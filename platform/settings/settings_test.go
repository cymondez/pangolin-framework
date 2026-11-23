package settings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/platform/cache"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type provider struct{ kind, revision string }

func (p provider) Kinds() []string { return []string{p.kind} }
func (p provider) Resolve(_ context.Context, principal scope.PrincipalRef, ref scope.ScopeRef) (scope.ScopeBinding, error) {
	return scope.ScopeBinding{Ref: ref, Principal: principal, AuthorityRevision: p.revision}, nil
}
func (provider) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }
func contextFor(t *testing.T, kind, id string) context.Context {
	t.Helper()
	r, _ := scope.NewRegistry(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "user", Kind: "user"}, "tenant", true
	}, provider{kind, "1"})
	ctx, err := r.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant", Kind: kind, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

type parent struct {
	ref scope.ScopeRef
	err error
}

func (p parent) Parent(context.Context, scope.ScopeBinding) (scope.ScopeRef, bool, error) {
	return p.ref, p.ref.ID != "", p.err
}

type brokenCache struct{ *cache.Memory }

func (b brokenCache) Delete(context.Context, cache.Resource) error {
	return errors.New("delete unavailable")
}

type allow bool

func (a allow) Authorize(context.Context, string) error {
	if a {
		return nil
	}
	return scope.ErrDenied
}
func TestA65Settings(t *testing.T) {
	t.Run("setting-precedence", func(t *testing.T) {
		ctx := contextFor(t, "tenant", "tenant")
		b, _ := scope.Current(ctx)
		p := NewMemoryProvider(nil)
		c, _ := cache.NewMemory("app", "v", 8)
		s, _ := New(p, c, nil, time.Minute, Definition{Key: "color", Default: json.RawMessage(`"default"`), Inherit: true})
		layers := []Layer{{}, {Ref: b.Ref}, {Ref: b.Ref, User: b.Principal}}
		for i, l := range layers {
			value := []string{`"application"`, `"tenant"`, `"user"`}[i]
			_, err := p.Put(ctx, l, "color", json.RawMessage(value))
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Resolve(ctx, "color")
			if err != nil || string(got.Value) != value {
				t.Fatal(got, err)
			}
		}
	})
	t.Run("invalidation-failure", func(t *testing.T) {
		ctx := contextFor(t, "tenant", "tenant")
		p := NewMemoryProvider(PublisherFunc(func(context.Context, Change) error { return errors.New("notification unavailable") }))
		c, _ := cache.NewMemory("app", "v", 8)
		s, _ := New(p, brokenCache{c}, nil, time.Hour, Definition{Key: "limit", Default: json.RawMessage(`1`), Inherit: true})
		_, _ = s.Resolve(ctx, "limit")
		v, err := s.Update(ctx, allow(true), "limit", json.RawMessage(`2`))
		if v == 0 || err == nil {
			t.Fatal("must preserve committed version and error")
		}
		got, err := s.Resolve(ctx, "limit")
		if err != nil || string(got.Value) != "2" || got.Version != v {
			t.Fatal(got, err)
		}
		if _, err = s.Update(ctx, allow(false), "limit", json.RawMessage(`3`)); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
	})
}
func TestA80Settings(t *testing.T) {
	t.Run("setting-inheritance", func(t *testing.T) {
		ctx := contextFor(t, "org.organization", "org")
		b, _ := scope.Current(ctx)
		enterprise := scope.ScopeRef{TenantID: "tenant", Kind: "org.enterprise", ID: "enterprise"}
		p := NewMemoryProvider(nil)
		c, _ := cache.NewMemory("app", "v", 16)
		s, _ := New(p, c, parent{ref: enterprise}, time.Minute, Definition{Key: "color", Default: json.RawMessage(`"default"`), Inherit: true}, Definition{Key: "local", Default: json.RawMessage(`0`)})
		layers := []Layer{{}, {Ref: scope.ScopeRef{TenantID: "tenant", Kind: "tenant", ID: "tenant"}}, {Ref: enterprise}, {Ref: b.Ref}, {Ref: b.Ref, User: b.Principal}}
		for i, l := range layers {
			value := json.RawMessage([]byte(`"layer` + string(rune('0'+i)) + `"`))
			_, _ = p.Put(ctx, l, "color", value)
			got, err := s.Resolve(ctx, "color")
			if err != nil || string(got.Value) != string(value) {
				t.Fatal(i, got, err)
			}
		}
		_, _ = p.Put(ctx, Layer{}, "local", json.RawMessage(`100`))
		got, _ := s.Resolve(ctx, "local")
		if string(got.Value) != "0" {
			t.Fatal("undeclared inheritance")
		}
		other := contextFor(t, "org.organization", "other")
		got, _ = s.Resolve(other, "color")
		if string(got.Value) != `"layer2"` {
			t.Fatal("user leaked across scopes", got)
		}
		standalone, _ := New(p, c, parent{}, time.Minute, Definition{Key: "color", Default: json.RawMessage(`"default"`), Inherit: true})
		got, _ = standalone.Resolve(other, "color")
		if string(got.Value) != `"layer1"` {
			t.Fatal("standalone inherited enterprise")
		}
		enterpriseCtx := contextFor(t, "org.enterprise", "enterprise")
		got, _ = s.Resolve(enterpriseCtx, "color")
		if string(got.Value) != `"layer2"` {
			t.Fatal(got)
		}
	})
}
func TestSettingsErrors(t *testing.T) {
	p := NewMemoryProvider(nil)
	c, _ := cache.NewMemory("app", "v", 8)
	d := Definition{Key: "k", Default: json.RawMessage(`1`), Inherit: true}
	if _, err := New(nil, c, nil, time.Second, d); err == nil {
		t.Fatal("config")
	}
	if _, err := New(p, c, nil, time.Second, d, d); err == nil {
		t.Fatal("duplicate")
	}
	if _, err := New(p, c, nil, time.Second, Definition{Key: "k", Default: json.RawMessage(`bad`)}); err == nil {
		t.Fatal("default")
	}
	s, _ := New(p, c, nil, time.Second, d)
	if _, err := s.Resolve(context.Background(), "k"); err == nil {
		t.Fatal("unbound")
	}
	ctx := contextFor(t, "tenant", "tenant")
	if _, err := s.Resolve(ctx, "unknown"); err == nil {
		t.Fatal("unknown")
	}
	org := contextFor(t, "org.organization", "org")
	if _, err := s.Resolve(org, "k"); err == nil {
		t.Fatal("missing ancestry")
	}
	for _, ancestry := range []parent{{err: errors.New("offline")}, {ref: scope.ScopeRef{TenantID: "other", Kind: "org.enterprise", ID: "e"}}} {
		s, _ := New(p, c, ancestry, time.Second, d)
		if _, err := s.Resolve(org, "k"); err == nil {
			t.Fatal("untrusted ancestry")
		}
	}
	if _, err := p.Put(ctx, Layer{}, "", json.RawMessage(`1`)); err == nil {
		t.Fatal("invalid put")
	}
	if _, err := s.Update(ctx, allow(true), "k", json.RawMessage(`bad`)); err == nil {
		t.Fatal("invalid update")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Read(cancelled, nil, "k"); err == nil {
		t.Fatal("cancel")
	}
	if _, err := p.Put(cancelled, Layer{}, "k", json.RawMessage(`1`)); err == nil {
		t.Fatal("cancel")
	}
}
