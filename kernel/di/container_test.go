package di

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
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

func TestDIUnrelatedWorkDuringBlockedOperationFactory(t *testing.T) {
	service, singleton := NewKey[*int]("slow-operation"), NewKey[*int]("ready-singleton")
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := releaseOnce(release)
	defer unblock()
	var calls int32
	r := newRegistry(t)
	mustProvide(t, r, Provider{service, Operation, nil, func(context.Context, Resolver) (any, Cleanup, error) {
		atomic.AddInt32(&calls, 1)
		close(entered)
		<-release
		v := 2
		return &v, nil, nil
	}})
	mustProvide(t, r, value(singleton, Singleton))
	c := seal(t, r)
	s, err := c.OpenScope(bg)
	if err != nil {
		t.Fatal(err)
	}
	resolved := make(chan error, 1)
	go func() { _, e := s.Resolve(bg, service); resolved <- e }()
	<-entered
	assertDICompletes(t, func() error { _, e := c.Resolve(bg, singleton); return e })
	var another Scope
	assertDICompletes(t, func() error { var e error; another, e = c.OpenScope(bg); return e })
	unblock()
	if err := <-resolved; err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("factory calls = %d", calls)
	}
	_ = another.Close(bg)
	_ = s.Close(bg)
	_ = c.Close(bg)
}

func TestDIUnrelatedWorkDuringBlockedCleanup(t *testing.T) {
	service, singleton := NewKey[*int]("operation-with-cleanup"), NewKey[*int]("ready-singleton")
	cleanupEntered, cleanupRelease := make(chan struct{}), make(chan struct{})
	unblockCleanup := releaseOnce(cleanupRelease)
	defer unblockCleanup()
	r := newRegistry(t)
	mustProvide(t, r, Provider{service, Operation, nil, func(context.Context, Resolver) (any, Cleanup, error) {
		v := 1
		return &v, func(context.Context) error { close(cleanupEntered); <-cleanupRelease; return nil }, nil
	}})
	mustProvide(t, r, value(singleton, Singleton))
	c := seal(t, r)
	s, _ := c.OpenScope(bg)
	if _, err := s.Resolve(bg, service); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close(bg) }()
	<-cleanupEntered
	assertDICompletes(t, func() error { _, e := c.Resolve(bg, singleton); return e })
	var another Scope
	assertDICompletes(t, func() error { var e error; another, e = c.OpenScope(bg); return e })
	unblockCleanup()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	_ = another.Close(bg)
	_ = c.Close(bg)
}

func TestDIContainerCloseWaitsForInProgressScopeCleanup(t *testing.T) {
	service, singleton := NewKey[*int]("scope-cleanup"), NewKey[*int]("singleton-cleanup-order")
	cleanupEntered, cleanupRelease, singletonCleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := releaseOnce(cleanupRelease)
	defer unblock()
	r := newRegistry(t)
	mustProvide(t, r, Provider{service, Operation, nil, func(context.Context, Resolver) (any, Cleanup, error) {
		v := 1
		return &v, func(context.Context) error { close(cleanupEntered); <-cleanupRelease; return nil }, nil
	}})
	mustProvide(t, r, Provider{singleton, Singleton, nil, func(context.Context, Resolver) (any, Cleanup, error) {
		v := 2
		return &v, func(context.Context) error { close(singletonCleaned); return nil }, nil
	}})
	c := seal(t, r)
	s, _ := c.OpenScope(bg)
	if _, err := s.Resolve(bg, service); err != nil {
		t.Fatal(err)
	}
	scopeClosed := make(chan error, 1)
	go func() { scopeClosed <- s.Close(bg) }()
	<-cleanupEntered
	containerClosed := make(chan error, 1)
	go func() { containerClosed <- c.Close(bg) }()
	waitForDI(t, func() bool {
		concrete := c.(*container)
		concrete.mu.Lock()
		defer concrete.mu.Unlock()
		return concrete.closed
	})
	select {
	case <-singletonCleaned:
		t.Fatal("singleton cleanup started before scope cleanup finished")
	case <-time.After(40 * time.Millisecond):
	}
	unblock()
	if err := <-scopeClosed; err != nil {
		t.Fatal(err)
	}
	if err := <-containerClosed; err != nil {
		t.Fatal(err)
	}
	select {
	case <-singletonCleaned:
	default:
		t.Fatal("singleton cleanup was not run")
	}
}

func TestDIOperationWaiterCanCancelAndConstructionIsUnique(t *testing.T) {
	service := NewKey[*int]("single-operation-construction")
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := releaseOnce(release)
	defer unblock()
	var calls int32
	r := newRegistry(t)
	mustProvide(t, r, Provider{service, Operation, nil, func(context.Context, Resolver) (any, Cleanup, error) {
		atomic.AddInt32(&calls, 1)
		close(entered)
		<-release
		v := 1
		return &v, nil, nil
	}})
	c := seal(t, r)
	s, _ := c.OpenScope(bg)
	first := make(chan error, 1)
	go func() { _, e := s.Resolve(bg, service); first <- e }()
	<-entered
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if _, err := s.Resolve(ctx, service); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error = %v", err)
	}
	second := make(chan any, 1)
	secondErr := make(chan error, 1)
	go func() { v, e := s.Resolve(bg, service); second <- v; secondErr <- e }()
	id, _ := identity(service)
	waitForDI(t, func() bool {
		concrete := s.(*operation)
		concrete.container.mu.Lock()
		defer concrete.container.mu.Unlock()
		f := concrete.flights[id]
		return f != nil && f.waiters == 1
	})
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	value, err := <-second, <-secondErr
	if err != nil || value == nil {
		t.Fatalf("same-key waiter: value=%v error=%v", value, err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("factory calls = %d", calls)
	}
	_ = s.Close(bg)
	_ = c.Close(bg)
}

func TestDICloseRacingFactoryCleansLateResourceOnce(t *testing.T) {
	service := NewKey[*int]("late-operation")
	entered, release, cleanupEntered, cleanupRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := releaseOnce(release)
	defer unblock()
	unblockCleanup := releaseOnce(cleanupRelease)
	defer unblockCleanup()
	var cleanups int32
	r := newRegistry(t)
	mustProvide(t, r, Provider{service, Operation, nil, func(context.Context, Resolver) (any, Cleanup, error) {
		close(entered)
		<-release
		v := 1
		return &v, func(context.Context) error {
			atomic.AddInt32(&cleanups, 1)
			close(cleanupEntered)
			<-cleanupRelease
			return nil
		}, nil
	}})
	c := seal(t, r)
	s, _ := c.OpenScope(bg)
	resolved := make(chan error, 1)
	go func() { _, e := s.Resolve(bg, service); resolved <- e }()
	<-entered
	close1, close2 := make(chan error, 1), make(chan error, 1)
	go func() { close1 <- c.Close(bg) }()
	go func() { close2 <- c.Close(bg) }()
	waitForDI(t, func() bool {
		concrete := c.(*container)
		concrete.mu.Lock()
		defer concrete.mu.Unlock()
		return concrete.closed
	})
	unblock()
	<-cleanupEntered
	if atomic.LoadInt32(&cleanups) != 1 {
		t.Fatalf("cleanup calls = %d", cleanups)
	}
	unblockCleanup()
	if err := <-resolved; err == nil {
		t.Fatal("factory result escaped a closing container")
	}
	if err := <-close1; err != nil {
		t.Fatal(err)
	}
	if err := <-close2; err != nil {
		t.Fatal(err)
	}
	_ = s.Close(bg)
}

func TestDICloseWaitIsBoundedWhileLateFactoryRemainsOwned(t *testing.T) {
	service := NewKey[*int]("factory-ignoring-cancellation")
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := releaseOnce(release)
	defer unblock()
	var cleanups int32
	r, err := NewRegistry(40 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Provide(Provider{service, Operation, nil, func(context.Context, Resolver) (any, Cleanup, error) {
		close(entered)
		<-release
		v := 1
		return &v, func(context.Context) error { atomic.AddInt32(&cleanups, 1); return nil }, nil
	}}); err != nil {
		t.Fatal(err)
	}
	c, err := r.Seal(bg)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := c.OpenScope(bg)
	resolved := make(chan error, 1)
	go func() { _, e := s.Resolve(bg, service); resolved <- e }()
	<-entered
	started := time.Now()
	if err := c.Close(bg); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("Close exceeded configured bound: %s", elapsed)
	}
	unblock()
	if err := <-resolved; err == nil {
		t.Fatal("late result escaped closed container")
	}
	waitForDI(t, func() bool { return atomic.LoadInt32(&cleanups) == 1 })
	if err := c.Close(bg); err != nil {
		t.Fatal(err)
	}
	_ = s.Close(bg)
}

func releaseOnce(ch chan struct{}) func() {
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

func waitForDI(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for DI state transition")
		}
		time.Sleep(time.Millisecond)
	}
}

func assertDICompletes(t *testing.T, call func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("unrelated DI operation blocked behind callback")
	}
}
