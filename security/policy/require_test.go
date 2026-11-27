package policy

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/security/scope"
	"testing"
)

type authFunc func(context.Context, string) error

func (f authFunc) Authorize(c context.Context, s string) error { return f(c, s) }
func TestRequireAllPolicies(t *testing.T) {
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a", true
	})
	ctx, _ := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"})
	allow := authFunc(func(context.Context, string) error { return nil })
	deny := authFunc(func(context.Context, string) error { return scope.ErrDenied })
	if e := Require(context.Background(), allow, "read", nil); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if e := Require(ctx, deny, "read", nil); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if e := Require(ctx, allow, "read", nil); e != nil {
		t.Fatal(e)
	}
	if e := Require(ctx, allow, "read", func(context.Context) error { return scope.ErrDenied }); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if e := Require(ctx, allow, "", nil); !errors.Is(e, scope.ErrDenied) {
		t.Fatal("empty permission must deny", e)
	}
	offline := errors.New("authority offline")
	if e := Require(ctx, authFunc(func(context.Context, string) error { return offline }), "read", nil); !errors.Is(e, offline) {
		t.Fatal("ordinary permission must deny while preserving the authorization cause", e)
	}
	boom := errors.New("required host audit unavailable")
	h := Host{Identity: func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "admin", Kind: "user"}, "host", true
	}, Check: func(context.Context, scope.PrincipalRef, string, string) error { return nil }, Audit: func(context.Context, scope.PrincipalRef, string, string) error { return boom }}
	if e := h.Run(ctx, "b", "inspect", func(context.Context, string) error { t.Fatal("unaudited host query"); return nil }); !errors.Is(e, boom) {
		t.Fatal(e)
	}
	h.Identity = func(context.Context) (scope.PrincipalRef, string, bool) { return scope.PrincipalRef{}, "", false }
	if e := h.Run(ctx, "b", "inspect", func(context.Context, string) error { return nil }); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
}
