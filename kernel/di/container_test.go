package di

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

var bg = context.Background()

func newRegistry(t *testing.T) Registry {
	t.Helper()
	r, e := NewRegistry(time.Second)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func value(key Token, l Lifetime, deps ...Token) Provider {
	return Provider{key, l, deps, func(context.Context, Resolver) (any, Cleanup, error) { v := 1; return &v, nil, nil }}
}
func mustProvide(t *testing.T, r Registry, p Provider) {
	t.Helper()
	if e := r.Provide(p); e != nil {
		t.Fatal(e)
	}
}
func seal(t *testing.T, r Registry) Container {
	t.Helper()
	c, e := r.Seal(bg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Close(bg) })
	return c
}
func TestA46(t *testing.T) {
	k := NewKey[*int]("service")
	for _, l := range []Lifetime{Singleton, Transient, Operation} {
		name := map[Lifetime]string{Singleton: "singleton", Transient: "transient", Operation: "operation"}[l]
		t.Run(name, func(t *testing.T) {
			r := newRegistry(t)
			mustProvide(t, r, value(k, l))
			c := seal(t, r)
			var resolver Resolver = c
			if l == Operation {
				if _, e := c.Resolve(bg, k); e == nil {
					t.Fatal("unscoped")
				}
				s, e := c.OpenScope(bg)
				if e != nil {
					t.Fatal(e)
				}
				defer s.Close(bg)
				resolver = s
			}
			a, e := resolver.Resolve(bg, k)
			if e != nil {
				t.Fatal(e)
			}
			b, e := resolver.Resolve(bg, k)
			if e != nil {
				t.Fatal(e)
			}
			if (a == b) != (l != Transient) {
				t.Fatal("lifetime")
			}
		})
	}
	for _, name := range []string{"missing-provider", "cycle", "captive-scope"} {
		t.Run(name, func(t *testing.T) {
			r := newRegistry(t)
			b := NewKey[*int]("b")
			mustProvide(t, r, value(k, Singleton, b))
			if name == "cycle" {
				mustProvide(t, r, value(b, Transient, k))
			}
			if name == "captive-scope" {
				c := NewKey[*int]("c")
				mustProvide(t, r, value(b, Transient, c))
				mustProvide(t, r, value(c, Operation))
			}
			if _, e := r.Seal(bg); e == nil {
				t.Fatal("invalid graph")
			}
		})
	}
	t.Run("duplicate-provider", func(t *testing.T) {
		r := newRegistry(t)
		mustProvide(t, r, value(k, Singleton))
		if r.Provide(value(k, Transient)) == nil {
			t.Fatal("duplicate")
		}
	})
	t.Run("explicit-replace", func(t *testing.T) {
		r := newRegistry(t)
		if r.Replace(value(k, Singleton), "reason") == nil {
			t.Fatal("missing")
		}
		mustProvide(t, r, value(k, Singleton))
		if r.Replace(value(k, Singleton), " ") == nil {
			t.Fatal("reason")
		}
		if e := r.Replace(value(k, Transient), "test replacement"); e != nil {
			t.Fatal(e)
		}
		if len(r.(*registry).replacements) != 1 {
			t.Fatal("replacement not recorded")
		}
		seal(t, r)
	})
	t.Run("sealed-registry", func(t *testing.T) {
		r := newRegistry(t)
		seal(t, r)
		if r.Provide(value(k, Singleton)) == nil || r.Replace(value(k, Singleton), "reason") == nil {
			t.Fatal("mutable")
		}
		if _, e := r.Seal(bg); e == nil {
			t.Fatal("sealed twice")
		}
	})
	t.Run("factory-failure", func(t *testing.T) {
		r := newRegistry(t)
		cause := errors.New("construct")
		cleanupErr := errors.New("cleanup")
		calls := 0
		mustProvide(t, r, Provider{k, Singleton, nil, func(context.Context, Resolver) (any, Cleanup, error) {
			return nil, func(ctx context.Context) error {
				calls++
				if ctx.Err() != nil {
					t.Fatal(ctx.Err())
				}
				return cleanupErr
			}, cause
		}})
		_, e := r.Seal(bg)
		if !errors.Is(e, cause) || !errors.Is(e, cleanupErr) || calls != 1 {
			t.Fatal(e, calls)
		}
	})
	t.Run("scoped-cleanup", func(t *testing.T) {
		r := newRegistry(t)
		order := []string{}
		dep := NewKey[*int]("dep")
		factory := func(name string, dependency Token) Factory {
			return func(ctx context.Context, res Resolver) (any, Cleanup, error) {
				if dependency != nil {
					if _, e := res.Resolve(ctx, dependency); e != nil {
						return nil, nil, e
					}
				}
				v := 1
				return &v, func(context.Context) error { order = append(order, name); return nil }, nil
			}
		}
		mustProvide(t, r, Provider{dep, Singleton, nil, factory("dep", nil)})
		mustProvide(t, r, Provider{k, Operation, []Token{dep}, factory("service", dep)})
		c := seal(t, r)
		s, _ := c.OpenScope(bg)
		if _, e := s.Resolve(bg, k); e != nil {
			t.Fatal(e)
		}
		cancelled, cancel := context.WithCancel(bg)
		cancel()
		if e := s.Close(cancelled); e != nil {
			t.Fatal(e)
		}
		_ = s.Close(bg)
		if _, e := s.Resolve(bg, k); e == nil {
			t.Fatal("closed")
		}
		_ = c.Close(bg)
		if !reflect.DeepEqual(order, []string{"service", "dep"}) {
			t.Fatal(order)
		}
	})
}
func TestContainerGuardsAndConcurrency(t *testing.T) {
	if _, e := NewRegistry(0); e == nil {
		t.Fatal("budget")
	}
	k := NewKey[*int]("a")
	r := newRegistry(t)
	for _, p := range []Provider{{nil, Singleton, nil, nil}, {NewKey[int](""), Singleton, nil, nil}, {k, Lifetime(99), nil, func(context.Context, Resolver) (any, Cleanup, error) { return nil, nil, nil }}, {k, Singleton, nil, nil}} {
		if r.Provide(p) == nil {
			t.Fatal("invalid provider")
		}
	}
	bad := newRegistry(t)
	mustProvide(t, bad, value(k, Singleton, nil))
	if _, e := bad.Seal(bg); e == nil {
		t.Fatal("nil dependency")
	}
	bad = newRegistry(t)
	d := NewKey[*int]("dep")
	mustProvide(t, bad, value(k, Transient, d, d))
	mustProvide(t, bad, value(d, Transient))
	if _, e := bad.Seal(bg); e == nil {
		t.Fatal("duplicate deps")
	}
	bad = newRegistry(t)
	mustProvide(t, bad, Provider{k, Singleton, nil, func(context.Context, Resolver) (any, Cleanup, error) { return "wrong", nil, nil }})
	if _, e := bad.Seal(bg); e == nil {
		t.Fatal("type mismatch")
	}
	var escaped Resolver
	mustProvide(t, r, Provider{k, Transient, nil, func(ctx context.Context, res Resolver) (any, Cleanup, error) {
		escaped = res
		if _, e := res.Resolve(ctx, d); e == nil {
			t.Fatal("undeclared")
		}
		v := 1
		return &v, nil, nil
	}})
	c := seal(t, r)
	for _, key := range []Token{nil, NewKey[int](""), d} {
		if _, e := c.Resolve(bg, key); e == nil {
			t.Fatal("invalid resolve")
		}
	}
	if _, e := c.Resolve(nil, k); e == nil {
		t.Fatal("nil context")
	}
	cancelled, cancel := context.WithCancel(bg)
	cancel()
	if _, e := c.Resolve(cancelled, k); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := c.OpenScope(nil); e == nil {
		t.Fatal("nil context")
	}
	if _, e := c.OpenScope(cancelled); e == nil {
		t.Fatal("cancelled")
	}
	if _, e := c.Resolve(bg, k); e != nil {
		t.Fatal(e)
	}
	if _, e := escaped.Resolve(bg, k); e == nil {
		t.Fatal("expired")
	}
	s, _ := c.OpenScope(bg)
	if _, e := s.Resolve(bg, nil); e == nil {
		t.Fatal("nil scope token")
	}
	_ = c.Close(bg)
	if _, e := s.Resolve(bg, k); e == nil {
		t.Fatal("container owns open scope")
	}
	if _, e := c.OpenScope(bg); e == nil {
		t.Fatal("closed")
	}
	if _, e := c.Resolve(bg, k); e == nil {
		t.Fatal("closed resolve")
	}
	r = newRegistry(t)
	mustProvide(t, r, value(k, Singleton))
	c = seal(t, r)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := c.OpenScope(bg)
			if e != nil {
				t.Error(e)
				return
			}
			defer s.Close(bg)
			if _, e = s.Resolve(bg, k); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
}

func TestSingletonPanicReleasesPreviouslyAcquiredResources(t *testing.T) {
	r := newRegistry(t)
	closed := 0
	first := NewKey[*int]("a")
	last := NewKey[*int]("z")
	mustProvide(t, r, Provider{first, Singleton, nil, func(context.Context, Resolver) (any, Cleanup, error) {
		v := 1
		return &v, func(context.Context) error { closed++; return nil }, nil
	}})
	mustProvide(t, r, Provider{last, Singleton, nil, func(context.Context, Resolver) (any, Cleanup, error) { panic("factory panic") }})
	defer func() {
		if recover() != "factory panic" || closed != 1 {
			t.Fatal("panic cleanup", closed)
		}
	}()
	_, _ = r.Seal(bg)
}
