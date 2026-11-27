package scope

import (
	"context"
	"errors"
	"testing"
)

type inventoryFunc func(context.Context) ([]string, error)

func (f inventoryFunc) RequiredKinds(c context.Context) ([]string, error) { return f(c) }
func TestScopeAdmissionGuards(t *testing.T) {
	ctx := context.Background()
	if _, e := Admit(nil); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	ctx = bindingVar.With(ctx, ScopeBinding{Ref: ScopeRef{"a", "tenant", "a"}, Principal: PrincipalRef{"alice", "user"}, AuthorityRevision: "1"})
	if _, e := Admit(ctx); !errors.Is(e, ErrAuthorityUnavailable) {
		t.Fatal(e)
	}
	if MatchOwner(context.Background(), ScopeRef{}) != ErrDenied {
		t.Fatal("unbound DTO")
	}
	for _, p := range []PrincipalRef{{}, {"alice", "invalid"}} {
		r := NewTenantResolver(func(context.Context) (PrincipalRef, string, bool) { return p, "a", true })
		if _, e := r.Bind(context.Background(), ScopeRef{"a", "tenant", "a"}); e == nil {
			t.Fatal(p)
		}
	}
	r, _ := NewRegistry(identity, TenantScopeProvider{Identity: identity})
	if r.CheckInventory(nil, nil) != ErrAuthorityUnavailable || r.CheckInventory(context.Background(), nil) != ErrAuthorityUnavailable {
		t.Fatal("inventory required")
	}
	if e := r.CheckInventory(context.Background(), inventoryFunc(func(context.Context) ([]string, error) { return []string{"org.organization"}, nil })); !errors.Is(e, ErrMigrationRequired) {
		t.Fatal(e)
	}
	if e := r.CheckInventory(context.Background(), inventoryFunc(func(context.Context) ([]string, error) { return nil, errors.New("projection offline") })); !errors.Is(e, ErrAuthorityUnavailable) {
		t.Fatal(e)
	}
	if e := r.CheckInventory(context.Background(), inventoryFunc(func(context.Context) ([]string, error) { return []string{"tenant"}, nil })); e != nil {
		t.Fatal(e)
	}
	for _, ref := range []ScopeRef{{}, {"a", "tenant", "x"}, {"a", " ", "x"}} {
		if _, e := Select("a", ref, ScopeRef{}, true); e == nil {
			t.Fatal(ref)
		}
	}
	b, err := Select("a", ScopeRef{}, ScopeRef{"a", "tenant", "a"}, false)
	if err != nil || b.ID != "a" {
		t.Fatal(b, err)
	}
}
