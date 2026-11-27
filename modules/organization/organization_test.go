package organization

import (
	"context"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/di"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type authority struct{}
type replacementProvider struct{ authority }

func (replacementProvider) Kinds() []string { return []string{"org.organization", "org.enterprise"} }

func (authority) Resolve(_ context.Context, p scope.PrincipalRef, r scope.ScopeRef) (scope.ScopeBinding, error) {
	return scope.ScopeBinding{Ref: r, Principal: p, AuthorityRevision: "one"}, nil
}
func (authority) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }
func identity(context.Context) (scope.PrincipalRef, string, bool) {
	return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant", true
}
func TestM2Assembly(t *testing.T) {
	for _, profile := range []string{"tenant-only", "organization-enabled", "replacement", "missing", "duplicate"} {
		t.Run(profile, func(t *testing.T) {
			h, e := runtime.New(runtime.Options{DrainTimeout: time.Second, CleanupTimeout: time.Second}, []config.Source{{Name: "defaults", Values: map[string]string{"uow.default_timeout": "1s", "uow.cleanup_timeout": "1s", "uow.hook_timeout": "1s", "uow.max_requires_new_depth": "2"}}})
			if e != nil {
				t.Fatal(e)
			}
			mods := []module.Module{}
			app := module.Module{Name: "app", Version: "0.1.0"}
			if profile == "organization-enabled" || profile == "duplicate" {
				m, e := NewModule(identity, authority{})
				if e != nil {
					t.Fatal(e)
				}
				mods = append(mods, m)
				app.RequiredScopeKinds = []string{"org.organization", "org.enterprise"}
			}
			if profile == "tenant-only" || profile == "replacement" || profile == "duplicate" {
				providers := []scope.ScopeProvider{scope.TenantScopeProvider{Identity: identity}}
				if profile == "replacement" {
					providers = append(providers, replacementProvider{})
					app.RequiredScopeKinds = []string{"org.organization"}
				}
				r, e := scope.NewRegistry(identity, providers...)
				if e != nil {
					t.Fatal(e)
				}
				mods = append(mods, module.Module{Name: "scope-provider", Version: "0.1.0", Configure: func(b module.Builder) error {
					return b.Provide(di.Provider{Key: scope.RegistryKey, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return r, nil, nil }})
				}})
			}
			if profile == "missing" {
				app.RequiredScopeKinds = []string{"org.organization"}
			}
			mods = append(mods, app)
			err := h.Start(context.Background(), mods)
			fail := profile == "missing" || profile == "duplicate"
			if (err != nil) != fail || h.Ready() == fail {
				t.Fatal(err, h.Ready())
			}
			if !fail {
				if e = h.Stop(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
	if _, e := NewProvider(nil); e == nil {
		t.Fatal("nil authority")
	}
	if _, e := NewModule(nil, authority{}); e == nil {
		t.Fatal("nil identity")
	}
	if _, e := NewModule(identity, nil); e == nil {
		t.Fatal("nil authority module")
	}
	p, _ := NewProvider(authority{})
	if _, e := p.Resolve(context.Background(), scope.PrincipalRef{}, scope.ScopeRef{}); e == nil {
		t.Fatal("invalid scope")
	}
	if _, e := p.Resolve(context.Background(), scope.PrincipalRef{}, scope.ScopeRef{TenantID: "tenant", Kind: "tenant", ID: "tenant"}); e == nil {
		t.Fatal("wrong kind")
	}
	if p.Authorize(context.Background(), scope.ScopeBinding{}, "") == nil {
		t.Fatal("empty permission")
	}
	if p.Authorize(context.Background(), scope.ScopeBinding{}, "books.read") != nil {
		t.Fatal("permission")
	}
	if _, e := p.Resolve(context.Background(), scope.PrincipalRef{ID: "alice"}, scope.ScopeRef{TenantID: "tenant", Kind: "org.organization", ID: "11111111-1111-4111-8111-111111111111"}); e != nil {
		t.Fatal(e)
	}
}
