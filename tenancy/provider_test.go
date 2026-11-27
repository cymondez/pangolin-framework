package tenancy

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"testing"
	"time"
)

type providerResource struct{}

func (providerResource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return c, providerTx{}, nil
}

type providerTx struct{}

func (providerTx) Commit(context.Context) error   { return nil }
func (providerTx) Rollback(context.Context) error { return nil }
func TestProviderAuthorityAndSnapshot(t *testing.T) {
	active := true
	revision := "1"
	sourceError := error(nil)
	a := authorityFunc(func(context.Context, scope.PrincipalRef, string) (string, error) {
		if sourceError != nil {
			return "", sourceError
		}
		if !active {
			return "", scope.ErrDenied
		}
		return revision, nil
	})
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a", true
	}
	resolver, _ := NewResolver(identity, a)
	ctx, err := resolver.Resolve(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	p := Provider{Authority: a, Permission: func(context.Context, scope.ScopeBinding, string) error { return nil }}
	r, _ := scope.NewRegistry(Identity, p)
	ref := scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"}
	bound, err := r.Bind(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := scope.Current(bound)
	if len(p.Kinds()) != 1 || p.Authorize(bound, b, "read") != nil {
		t.Fatal("valid grant")
	}
	m, _ := uow.NewManager(providerResource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 1})
	if err = m.Within(bound, uow.Options{}, func(c context.Context) error {
		forged, _ := scope.Current(c)
		forged.AuthorityRevision = "admin"
		if e := p.Authorize(c, forged, "read"); !errors.Is(e, scope.ErrDenied) {
			t.Fatal("forged admitted binding authorized", e)
		}
		active = false
		return p.Authorize(c, b, "read")
	}); err != nil {
		t.Fatal("admitted snapshot lost", err)
	}
	if !errors.Is(p.Authorize(bound, b, "read"), scope.ErrDenied) {
		t.Fatal("stale entry allow")
	}
	active = true
	for _, provider := range []Provider{{}, p} {
		for _, ref := range []scope.ScopeRef{{}, {TenantID: "b", Kind: "tenant", ID: "b"}, {TenantID: "a", Kind: "org.organization", ID: "x"}} {
			if _, e := provider.Resolve(ctx, b.Principal, ref); e == nil {
				t.Fatal(ref)
			}
		}
		if provider.Authorize(ctx, b, "") == nil {
			t.Fatal("empty action")
		}
	}
	if p.Authorize(context.Background(), b, "read") == nil {
		t.Fatal("untrusted identity")
	}
	sourceError = scope.ErrAuthorityUnavailable
	if _, err = p.Resolve(ctx, b.Principal, ref); !errors.Is(err, scope.ErrAuthorityUnavailable) {
		t.Fatal(err)
	}
	sourceError = nil
	revision = ""
	if _, err = resolver.Resolve(context.Background(), "", ""); !errors.Is(err, scope.ErrAuthorityUnavailable) {
		t.Fatal(err)
	}
	if _, err = p.Resolve(ctx, b.Principal, ref); !errors.Is(err, scope.ErrAuthorityUnavailable) {
		t.Fatal(err)
	}
	if _, err = resolver.Resolve(nil, "", ""); err == nil {
		t.Fatal("nil context")
	}
	invalid, _ := NewResolver(func(context.Context) (scope.PrincipalRef, string, bool) { return scope.PrincipalRef{}, "", false }, a)
	if _, err = invalid.Resolve(context.Background(), "", ""); err == nil {
		t.Fatal("invalid identity")
	}
	sourceError = errors.New("authority offline")
	if _, err = resolver.Resolve(context.Background(), "", ""); !errors.Is(err, sourceError) {
		t.Fatal(err)
	}
}
