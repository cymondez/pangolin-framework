// service.go keeps the published service credential verifier and adapts it to
// the identity contract: service credentials keep their distinct validated
// service principal kind.
package authn

import (
	"context"

	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
)

// ServiceVerifier authenticates a deployment-selected mTLS/OAuth credential.
// It is infrastructure composition, never a request Principal DTO.
type ServiceVerifier interface {
	Verify(context.Context, string) (scope.PrincipalRef, string, error)
}
type ServiceAuthenticator struct {
	verifier ServiceVerifier
	pipeline *identity.Pipeline
}

func NewServiceAuthenticator(verifier ServiceVerifier) (*ServiceAuthenticator, error) {
	if verifier == nil {
		return nil, identity.ErrUnauthenticated
	}
	a := &ServiceAuthenticator{verifier: verifier}
	a.pipeline, _ = identity.NewPipeline(a)
	return a, nil
}

// Kind implements identity.Verifier: service credentials route here.
func (a *ServiceAuthenticator) Kind() string { return identity.ServiceKind }

// Verify checks one service credential and returns its result without
// installing it.
func (a *ServiceAuthenticator) Verify(ctx context.Context, credential identity.Credential) (identity.Result, error) {
	if ctx == nil || a == nil || a.verifier == nil || credential.Kind != identity.ServiceKind || credential.Value == "" {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	p, tenant, err := a.verifier.Verify(ctx, credential.Value)
	if err != nil || p.Kind != identity.PrincipalService || p.ID == "" || tenant == "" {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{
		Status:    identity.StatusAuthenticated,
		Principal: p,
		Tenant:    tenant,
		Method:    identity.MethodService,
	}, nil
}

// Authenticate keeps the published service credential entry point. The
// trusted identity pipeline validates the result and installs identity.
func (a *ServiceAuthenticator) Authenticate(ctx context.Context, credential string) (context.Context, error) {
	return a.AuthenticateCredential(ctx, identity.Credential{Kind: identity.ServiceKind, Value: credential})
}

// AuthenticateCredential verifies one extracted service credential.
func (a *ServiceAuthenticator) AuthenticateCredential(ctx context.Context, credential identity.Credential) (context.Context, error) {
	if a == nil || a.pipeline == nil {
		return nil, identity.ErrUnauthenticated
	}
	return a.pipeline.AuthenticateCredential(ctx, credential)
}
