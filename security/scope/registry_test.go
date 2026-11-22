package scope

import (
	"context"
	"errors"
	"testing"
)

type authority struct {
	kinds []string
	bad   bool
	err   error
}

func (p *authority) Kinds() []string { return p.kinds }
func (p *authority) Resolve(_ context.Context, principal PrincipalRef, ref ScopeRef) (ScopeBinding, error) {
	if p.err != nil {
		return ScopeBinding{}, p.err
	}
	b := ScopeBinding{ref, principal, "rev-1"}
	if p.bad {
		b.Ref.ID = "other"
	}
	return b, nil
}
func (p *authority) Authorize(context.Context, ScopeBinding, string) error { return p.err }
func identity(context.Context) (PrincipalRef, string, bool) {
	return PrincipalRef{"alice", "user"}, "tenant-a", true
}
func TestM2ScopeRegistry(t *testing.T) {
	ctx := context.Background()
	tenant := TenantScopeProvider{Identity: identity, Permission: func(context.Context, ScopeBinding, string) error { return nil }}
	org := &authority{kinds: []string{"org.organization", "org.enterprise"}}
	r, err := NewRegistry(identity, tenant, org)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []ScopeRef{{"tenant-a", "tenant", "tenant-a"}, {"tenant-a", "org.organization", "a"}, {"tenant-a", "org.organization", "b"}, {"tenant-a", "org.enterprise", "a"}} {
		bound, e := r.Bind(ctx, ref)
		if e != nil {
			t.Fatal(e)
		}
		b, ok := r.Current(bound)
		if !ok || b.Ref != ref {
			t.Fatal(b)
		}
		admitted, e := r.Admit(bound, b)
		if e != nil || admitted.Ref != ref {
			t.Fatal(e)
		}
		if e = r.Authorize(bound, "books.read"); e != nil {
			t.Fatal(e)
		}
	}
	for _, ref := range []ScopeRef{{}, {"tenant-a", "tenant", "other"}, {"tenant-a", "org.organization", ""}, {"tenant-a", "*", "a"}, {" tenant-a", "tenant", "tenant-a"}, {"tenant-b", "org.organization", "a"}} {
		if _, e := r.Bind(ctx, ref); e == nil {
			t.Fatal(ref)
		}
	}
	if _, e := r.Bind(nil, ScopeRef{}); e == nil {
		t.Fatal("nil")
	}
	if _, e := r.Bind(ctx, ScopeRef{"tenant-a", "missing", "a"}); !errors.Is(e, ErrKindUnsupported) {
		t.Fatal(e)
	}
	if r.Require("tenant", "org.organization") != nil || r.Require("missing") == nil {
		t.Fatal("require")
	}
	if r.Authorize(ctx, "x") == nil {
		t.Fatal("unbound authorize")
	}
	bound, _ := r.Bind(ctx, ScopeRef{"tenant-a", "org.organization", "a"})
	if r.Authorize(bound, "") == nil {
		t.Fatal("empty permission")
	}
	org.bad = true
	if _, e := r.Bind(ctx, ScopeRef{"tenant-a", "org.organization", "a"}); e == nil {
		t.Fatal("provider changed ref")
	}
	b, _ := Current(bound)
	if _, e := r.Admit(bound, b); e == nil {
		t.Fatal("admit changed ref")
	}
	org.bad = false
	org.err = ErrAuthorityUnavailable
	if _, e := r.Bind(ctx, b.Ref); !errors.Is(e, ErrAuthorityUnavailable) {
		t.Fatal(e)
	}
	if _, e := r.Admit(bound, b); !errors.Is(e, ErrAuthorityUnavailable) {
		t.Fatal(e)
	}
	if !errors.Is(r.Authorize(bound, "x"), ErrAuthorityUnavailable) {
		t.Fatal("stale allow")
	}
	if _, e := r.Admit(ctx, ScopeBinding{Ref: ScopeRef{Kind: "missing"}}); e == nil {
		t.Fatal("missing admission kind")
	}
	for _, providers := range [][]ScopeProvider{{nil}, {&authority{}}, {tenant, tenant}, {&authority{kinds: []string{""}}}, {&authority{kinds: []string{"*"}}}} {
		if _, e := NewRegistry(identity, providers...); e == nil {
			t.Fatal(providers)
		}
	}
	if _, e := NewRegistry(nil); e == nil {
		t.Fatal("nil identity")
	}
	badIdentity := func(context.Context) (PrincipalRef, string, bool) { return PrincipalRef{}, "", false }
	bad, _ := NewRegistry(badIdentity, tenant)
	if _, e := bad.Bind(ctx, ScopeRef{"tenant-a", "tenant", "tenant-a"}); e == nil {
		t.Fatal("untrusted")
	}
	for _, p := range []TenantScopeProvider{tenant, {}} {
		for _, ref := range []ScopeRef{{}, {"tenant-b", "tenant", "tenant-b"}, {"tenant-a", "org.organization", "a"}} {
			if _, e := p.Resolve(ctx, PrincipalRef{}, ref); e == nil {
				t.Fatal("tenant invalid")
			}
		}
		if p.Authorize(ctx, ScopeBinding{}, "") == nil {
			t.Fatal("permission")
		}
	}
	if _, e := tenant.Resolve(nil, PrincipalRef{}, ScopeRef{}); e == nil {
		t.Fatal("nil resolve")
	}
}
