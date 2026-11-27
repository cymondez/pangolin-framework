package authn

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/security/scope"
	"testing"
)

type serviceVerify func(context.Context, string) (scope.PrincipalRef, string, error)

func (f serviceVerify) Verify(c context.Context, s string) (scope.PrincipalRef, string, error) {
	return f(c, s)
}
func TestServiceCredentials(t *testing.T) {
	if _, e := NewServiceAuthenticator(nil); e == nil {
		t.Fatal("nil verifier")
	}
	for _, tc := range []struct {
		name   string
		p      scope.PrincipalRef
		tenant string
		err    error
		allow  bool
	}{
		{"valid-service", scope.PrincipalRef{ID: "worker", Kind: "service"}, "a", nil, true},
		{"forged-user", scope.PrincipalRef{ID: "user", Kind: "user"}, "a", nil, false},
		{"empty-tenant", scope.PrincipalRef{ID: "worker", Kind: "service"}, "", nil, false},
		{"empty-id", scope.PrincipalRef{Kind: "service"}, "a", nil, false},
		{"invalid-credential", scope.PrincipalRef{ID: "worker", Kind: "service"}, "a", errors.New("invalid"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := NewServiceAuthenticator(serviceVerify(func(context.Context, string) (scope.PrincipalRef, string, error) { return tc.p, tc.tenant, tc.err }))
			ctx, e := a.Authenticate(context.Background(), "credential")
			if (e == nil) != tc.allow {
				t.Fatal(e)
			}
			if e == nil {
				p, tenant, ok := Identity(ctx)
				if !ok || p != tc.p || tenant != tc.tenant {
					t.Fatal(p, tenant)
				}
			}
			if _, e = a.Authenticate(nil, "credential"); e == nil {
				t.Fatal("nilctx")
			}
			if _, e = a.Authenticate(context.Background(), ""); e == nil {
				t.Fatal("empty credential")
			}
		})
	}
	var a *ServiceAuthenticator
	if _, e := a.Authenticate(context.Background(), "x"); e == nil {
		t.Fatal("nil auth")
	}
}
