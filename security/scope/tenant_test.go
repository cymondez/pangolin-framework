package scope

import (
	"context"
	"testing"
)

func TestTenantResolver(t *testing.T) {
	if _, ok := Current(nil); ok {
		t.Fatal("nil")
	}
	if _, ok := Current(context.Background()); ok {
		t.Fatal("unbound")
	}
	r := NewTenantResolver(nil)
	if _, e := r.Bind(nil, ScopeRef{}); e == nil {
		t.Fatal("nil identity")
	}
	r = NewTenantResolver(func(context.Context) (PrincipalRef, string, bool) {
		return PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	for _, ref := range []ScopeRef{{}, {"tenant-b", "tenant", "tenant-b"}, {"tenant-a", "org.organization", "org-a"}, {"tenant-a", "tenant", "wrong"}} {
		if _, e := r.Bind(context.Background(), ref); e == nil {
			t.Fatal(ref)
		}
	}
	ctx, e := r.Bind(context.Background(), ScopeRef{"tenant-a", "tenant", "tenant-a"})
	if e != nil {
		t.Fatal(e)
	}
	b, ok := r.Current(ctx)
	if !ok || b.Principal.ID != "alice" {
		t.Fatal(b)
	}
}
