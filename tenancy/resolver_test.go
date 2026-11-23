package tenancy

import (
	"context"
	"errors"
	"github.com/cymondez/pangolin-framework/security/scope"
	"testing"
)

type authorityFunc func(context.Context, scope.PrincipalRef, string) (string, error)

func (f authorityFunc) Check(c context.Context, p scope.PrincipalRef, t string) (string, error) {
	return f(c, p, t)
}
func TestA08TenantHints(t *testing.T) {
	id := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a", true
	}
	active := true
	r, _ := NewResolver(id, authorityFunc(func(_ context.Context, _ scope.PrincipalRef, tenant string) (string, error) {
		if !active || tenant != "a" {
			return "", scope.ErrDenied
		}
		return "1", nil
	}))
	for _, tc := range []struct {
		name, route, header string
		denied              bool
	}{{"valid-hint", "a", "a", false}, {"http-tenant-hint", "", "b", true}, {"job-tenant-tamper", "b", "", true}, {"event-tenant-tamper", "", "b", true}, {"conflict", "a", "b", true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := r.Resolve(context.Background(), tc.route, tc.header)
			if (err != nil) != tc.denied {
				t.Fatal(err)
			}
			if err == nil {
				p, tenant, ok := Identity(ctx)
				if !ok || tenant != "a" || p.ID != "alice" {
					t.Fatal(p, tenant)
				}
			}
		})
	}
	active = false
	if _, err := r.Resolve(context.Background(), "", ""); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if _, _, ok := Identity(context.Background()); ok {
		t.Fatal("forged context")
	}
	if _, _, ok := Identity(nil); ok {
		t.Fatal("nil context")
	}
	if _, err := NewResolver(nil, nil); err == nil {
		t.Fatal("nil dependencies")
	}
}
