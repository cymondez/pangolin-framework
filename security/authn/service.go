package authn

import (
	"context"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// ServiceVerifier authenticates a deployment-selected mTLS/OAuth credential.
// It is infrastructure composition, never a request Principal DTO.
type ServiceVerifier interface {
	Verify(context.Context, string) (scope.PrincipalRef, string, error)
}
type ServiceAuthenticator struct{ verifier ServiceVerifier }

func NewServiceAuthenticator(verifier ServiceVerifier) (*ServiceAuthenticator, error) {
	if verifier == nil {
		return nil, ErrUnauthenticated
	}
	return &ServiceAuthenticator{verifier}, nil
}
func (a *ServiceAuthenticator) Authenticate(ctx context.Context, credential string) (context.Context, error) {
	if ctx == nil || credential == "" || a == nil || a.verifier == nil {
		return nil, ErrUnauthenticated
	}
	p, tenant, err := a.verifier.Verify(ctx, credential)
	if err != nil || p.Kind != "service" || p.ID == "" || tenant == "" {
		return nil, ErrUnauthenticated
	}
	return context.WithValue(ctx, identityKey{}, identity{p, tenant}), nil
}
