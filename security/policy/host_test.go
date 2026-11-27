package policy

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/security/scope"
	"testing"
)

func TestA58Host(t *testing.T) {
	allowed := false
	audits, queries := 0, 0
	h := Host{Identity: func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "admin", Kind: "user"}, "host", true
	}, Check: func(context.Context, scope.PrincipalRef, string, string) error {
		if !allowed {
			return scope.ErrDenied
		}
		return nil
	}, Audit: func(context.Context, scope.PrincipalRef, string, string) error { audits++; return nil }}
	run := func() error {
		return h.Run(context.Background(), "tenant-b", "host.inspect", func(c context.Context, tenant string) error {
			queries++
			if tenant != "tenant-b" {
				t.Fatal(tenant)
			}
			if _, ok := scope.Current(c); ok {
				t.Fatal("global bypass binding")
			}
			return nil
		})
	}
	t.Run("ordinary-override-denied", func(t *testing.T) {
		if !errors.Is(run(), scope.ErrDenied) || queries != 0 {
			t.Fatal("ordinary allowed")
		}
	})
	allowed = true
	t.Run("host-policy-allow", func(t *testing.T) {
		if err := run(); err != nil || queries != 1 {
			t.Fatal(err)
		}
	})
	t.Run("host-audit", func(t *testing.T) {
		if audits != 1 {
			t.Fatal(audits)
		}
	})
	t.Run("no-global-bypass", func(t *testing.T) {
		if err := h.Run(context.Background(), "*", "host.inspect", func(context.Context, string) error { return nil }); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
	})
}
