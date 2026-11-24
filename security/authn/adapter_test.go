package authn

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// The identity.Pipeline dispatches by credential kind; the JWT adapter must
// refuse credentials of other kinds without parsing them.
func TestValidatorAdapterKindDispatch(t *testing.T) {
	v := NewValidator("https://idp.example", "bookstore", "https://idp.example/jwks.json", &http.Client{})
	if v.Kind() != identity.BearerKind {
		t.Fatal(v.Kind())
	}
	for _, kind := range []string{identity.SessionKind, identity.ServiceKind, ""} {
		if _, err := v.Verify(context.Background(), identity.Credential{Kind: kind, Value: "token"}); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatal(kind, err)
		}
	}
	if _, err := v.Verify(nil, identity.Credential{Kind: identity.BearerKind, Value: "t"}); err == nil {
		t.Fatal("nil context accepted")
	}
	var nilValidator *Validator
	if _, err := nilValidator.Verify(context.Background(), identity.Credential{Kind: identity.BearerKind, Value: "t"}); err == nil {
		t.Fatal("nil validator accepted")
	}
	if _, err := nilValidator.AuthenticateCredential(context.Background(), identity.Credential{Kind: identity.BearerKind, Value: "t"}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := nilValidator.Authenticate(context.Background(), "t"); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
}

func TestServiceAdapterKindDispatch(t *testing.T) {
	a, err := NewServiceAuthenticator(serviceVerify(func(context.Context, string) (scope.PrincipalRef, string, error) {
		return scope.PrincipalRef{ID: "worker", Kind: identity.PrincipalService}, "tenant-a", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind() != identity.ServiceKind {
		t.Fatal(a.Kind())
	}
	for _, kind := range []string{identity.BearerKind, identity.SessionKind, ""} {
		if _, err = a.Verify(context.Background(), identity.Credential{Kind: kind, Value: "c"}); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatal(kind, err)
		}
	}
	if _, err = a.Verify(nil, identity.Credential{Kind: identity.ServiceKind, Value: "c"}); err == nil {
		t.Fatal("nil context accepted")
	}
	if ctx, err := a.Authenticate(context.Background(), "credential"); err != nil || ctx == nil {
		t.Fatal(ctx, err)
	} else if _, _, ok := Identity(ctx); !ok {
		t.Fatal("service identity not installed")
	}
	var nilAuthenticator *ServiceAuthenticator
	if _, err = nilAuthenticator.Verify(context.Background(), identity.Credential{Kind: identity.ServiceKind, Value: "c"}); err == nil {
		t.Fatal("nil authenticator accepted")
	}
	if _, err = nilAuthenticator.AuthenticateCredential(context.Background(), identity.Credential{Kind: identity.ServiceKind, Value: "c"}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err = nilAuthenticator.Authenticate(context.Background(), "c"); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
}

// A service verifier minting a user principal keeps failing closed through
// the identity pipeline: kinds are validated after verification too.
func TestServiceAdapterCannotMintUserPrincipal(t *testing.T) {
	a, err := NewServiceAuthenticator(serviceVerify(func(context.Context, string) (scope.PrincipalRef, string, error) {
		return scope.PrincipalRef{ID: "user", Kind: identity.PrincipalUser}, "tenant-a", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Authenticate(context.Background(), "credential"); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
}

// The identity.Pipeline registered through the adapter only accepts bearer
// credentials; session credentials for an unregistered kind fail closed.
func TestValidatorPipelineRejectsForeignKinds(t *testing.T) {
	v := NewValidator("https://idp.example", "bookstore", "https://idp.example/jwks.json", &http.Client{})
	if _, err := v.AuthenticateCredential(context.Background(), identity.Credential{Kind: identity.SessionKind, Value: "s"}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, err := v.AuthenticateCredential(context.Background(), identity.Credential{Kind: identity.BearerKind, Value: strings.Repeat("x", identity.MaxCredentialBytes+1)}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal(err)
	}
}
