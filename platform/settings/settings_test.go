package settings

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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

func bindingFor(t *testing.T, tenant, kind, id string) context.Context {
	t.Helper()
	r, err := scope.NewRegistry(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "user", Kind: "user"}, tenant, true
	}, provider{kind, "1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := r.Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: kind, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

// The five standard layers are explicitly distinguished and resolved in the
// exact order Default < Config < Global < Tenant < User; Enterprise and
// Organization participate only for their trusted scope kinds.
func TestSettingsStandardLayers(t *testing.T) {
	c, _ := cache.NewMemory("app", "v", 64)
	tenant := contextFor(t, "tenant", "tenant")
	enterprise := contextFor(t, "org.enterprise", "enterprise")
	org := contextFor(t, "org.organization", "org")
	underEnterprise := parent{ref: scope.ScopeRef{TenantID: "tenant", Kind: "org.enterprise", ID: "enterprise"}}
	definitions := []Definition{{Key: "k", Default: json.RawMessage(`1`), Inherit: true}, {Key: "flat", Default: json.RawMessage(`0`)}}
	t.Run("tenant-only-five-standard-layers", func(t *testing.T) {
		s, _ := New(NewMemoryProvider(nil), c, nil, time.Minute, definitions...)
		got, err := s.Layers(tenant, "k")
		if err != nil || !slices.Equal(got, []LayerName{LayerUser, LayerTenant, LayerGlobal, LayerConfig, LayerDefault}) {
			t.Fatal(got, err)
		}
		if slices.Contains(got, LayerOrganization) || slices.Contains(got, LayerEnterprise) {
			t.Fatal("tenant-only must not invent organization layers", got)
		}
	})
	t.Run("enterprise-six-standard-layers", func(t *testing.T) {
		s, _ := New(NewMemoryProvider(nil), c, nil, time.Minute, definitions...)
		got, err := s.Layers(enterprise, "k")
		if err != nil || !slices.Equal(got, []LayerName{LayerUser, LayerEnterprise, LayerTenant, LayerGlobal, LayerConfig, LayerDefault}) {
			t.Fatal(got, err)
		}
	})
	t.Run("standalone-organization-six-standard-layers", func(t *testing.T) {
		s, _ := New(NewMemoryProvider(nil), c, parent{}, time.Minute, definitions...)
		got, err := s.Layers(org, "k")
		if err != nil || !slices.Equal(got, []LayerName{LayerUser, LayerOrganization, LayerTenant, LayerGlobal, LayerConfig, LayerDefault}) {
			t.Fatal(got, err)
		}
	})
	t.Run("organization-under-enterprise-seven-standard-layers", func(t *testing.T) {
		s, _ := New(NewMemoryProvider(nil), c, underEnterprise, time.Minute, definitions...)
		got, err := s.Layers(org, "k")
		if err != nil || !slices.Equal(got, []LayerName{LayerUser, LayerOrganization, LayerEnterprise, LayerTenant, LayerGlobal, LayerConfig, LayerDefault}) {
			t.Fatal(got, err)
		}
	})
	t.Run("no-inherit-own-scope-only", func(t *testing.T) {
		s, _ := New(NewMemoryProvider(nil), c, underEnterprise, time.Minute, definitions...)
		for _, tc := range []struct {
			ctx  context.Context
			want []LayerName
		}{
			{tenant, []LayerName{LayerUser, LayerTenant, LayerDefault}},
			{enterprise, []LayerName{LayerUser, LayerEnterprise, LayerDefault}},
			{org, []LayerName{LayerUser, LayerOrganization, LayerDefault}},
		} {
			got, err := s.Layers(tc.ctx, "flat")
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatal(got, err)
			}
		}
	})
	t.Run("errors-match-resolution", func(t *testing.T) {
		s, _ := New(NewMemoryProvider(nil), c, nil, time.Minute, definitions...)
		if _, err := s.Layers(context.Background(), "k"); !errors.Is(err, scope.ErrDenied) {
			t.Fatal("unbound context", err)
		}
		if _, err := s.Layers(tenant, "missing"); !errors.Is(err, ErrInvalid) {
			t.Fatal("unknown definition", err)
		}
		if _, err := s.Layers(org, "k"); !errors.Is(err, scope.ErrAuthorityUnavailable) {
			t.Fatal("organization without ancestry authority", err)
		}
		custom := contextFor(t, "book", "b")
		if _, err := s.Layers(custom, "k"); !errors.Is(err, scope.ErrKindUnsupported) {
			t.Fatal("unsupported kind", err)
		}
		untrusted, _ := New(NewMemoryProvider(nil), c, parent{ref: scope.ScopeRef{TenantID: "other", Kind: "org.enterprise", ID: "e"}}, time.Minute, definitions...)
		if _, err := untrusted.Layers(org, "k"); !errors.Is(err, scope.ErrDenied) {
			t.Fatal("cross-tenant ancestry", err)
		}
	})
}

func TestSettingsOwnerIsolation(t *testing.T) {
	a := bindingFor(t, "tenant-a", "tenant", "tenant-a")
	b := bindingFor(t, "tenant-b", "tenant", "tenant-b")
	ba, _ := scope.Current(a)
	bb, _ := scope.Current(b)
	p := NewMemoryProvider(nil)
	c, _ := cache.NewMemory("app", "v", 16)
	s, _ := New(p, c, nil, time.Minute, Definition{Key: "theme", Default: json.RawMessage(`"default"`), Inherit: true})
	if _, err := p.Put(a, Layer{Ref: bb.Ref}, "theme", json.RawMessage(`"tenant-b"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Put(b, Layer{Ref: bb.Ref, User: bb.Principal}, "theme", json.RawMessage(`"user-b"`)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Resolve(a, "theme")
	if err != nil || string(got.Value) != `"default"` {
		t.Fatal("tenant and user values leaked across tenants", got, err)
	}
	if _, err := p.Put(a, Layer{Ref: ba.Ref, User: ba.Principal}, "theme", json.RawMessage(`"user-a"`)); err != nil {
		t.Fatal(err)
	}
	got, err = s.Resolve(a, "theme")
	if err != nil || string(got.Value) != `"user-a"` {
		t.Fatal(got, err)
	}
	got, err = s.Resolve(b, "theme")
	if err != nil || string(got.Value) != `"user-b"` {
		t.Fatal("owner-local user isolation", got, err)
	}
	if _, err := p.Put(a, Layer{}, "theme", json.RawMessage(`"global"`)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ctx  context.Context
		want string
	}{{a, `"user-a"`}, {b, `"user-b"`}} {
		got, err = s.Resolve(tc.ctx, "theme")
		if err != nil || string(got.Value) != tc.want {
			t.Fatal("global must not outrank owner-local user values", got, err)
		}
	}
	third := bindingFor(t, "tenant-c", "tenant", "tenant-c")
	got, err = s.Resolve(third, "theme")
	if err != nil || string(got.Value) != `"global"` {
		t.Fatal("global is the application-wide layer under every tenant", got, err)
	}
}

type failingProvider struct{ err error }

func (f failingProvider) Read(context.Context, []Layer, string) (Snapshot, error) {
	return Snapshot{}, f.err
}

func TestSettingsSourceFailure(t *testing.T) {
	boom := errors.New("settings source offline")
	c, _ := cache.NewMemory("app", "v", 8)
	s, _ := New(failingProvider{err: boom}, c, nil, time.Minute, Definition{Key: "k", Default: json.RawMessage(`1`), Inherit: true})
	ctx := contextFor(t, "tenant", "tenant")
	for range 2 {
		if _, err := s.Resolve(ctx, "k"); !errors.Is(err, boom) {
			t.Fatal("source failure must surface the cause without negative caching", err)
		}
	}
}
