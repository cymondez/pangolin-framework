// Package identity defines the protocol-independent identity/authentication
// contract, the trusted orchestration that completes and installs verified
// identity, and the private context storage read by resolvers.
//
// Verifiers are trusted, explicitly registered adapters: they verify one
// credential kind and return a Result. Only the pipeline in this package
// validates the result and installs identity; HTTP bodies, DTOs and custom
// verifiers cannot install a principal. Unfinished challenge or pending
// outcomes never install a usable authenticated principal.
package identity

import (
	"context"

	"github.com/cymondez/pangolin-framework/kernel/ambient"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type identity struct {
	principal  scope.PrincipalRef
	tenant     string
	method     string
	assurances []string
}

var identityVar = ambient.New[identity]()

// Identity reads only verified identity installed by this package's trusted
// pipeline. The tenant is a credential hint; authorization still happens in
// the tenant/scope resolvers.
func Identity(ctx context.Context) (scope.PrincipalRef, string, bool) {
	v, ok := value(ctx)
	if !ok {
		return scope.PrincipalRef{}, "", false
	}
	return v.principal, v.tenant, true
}

// Method reads the immutable, non-secret authentication metadata: how the
// principal authenticated (for example "jwt" or "session") and the assurance
// factors the verifier reported. Raw tokens and secrets are never stored.
func Method(ctx context.Context) (string, []string, bool) {
	v, ok := value(ctx)
	if !ok {
		return "", nil, false
	}
	return v.method, append([]string(nil), v.assurances...), true
}

func value(ctx context.Context) (identity, bool) {
	return identityVar.Current(ctx)
}

// install stores a completed, validated result under the private context key.
// Installed metadata is copied so later mutation cannot change it.
func install(ctx context.Context, result Result) (context.Context, error) {
	if ctx == nil {
		return nil, ErrUnauthenticated
	}
	return identityVar.With(ctx, identity{
		principal:  result.Principal,
		tenant:     result.Tenant,
		method:     result.Method,
		assurances: append([]string(nil), result.Assurances...),
	}), nil
}

// complete validates a verifier result. Authenticated results must carry a
// valid user or service principal; challenge and pending results report the
// unfinished outcome and never install identity. Challenge metadata is
// detached from the verifier-owned value before being reported.
func complete(result Result) (Result, error) {
	switch result.Status {
	case StatusAuthenticated:
		if result.Challenge != nil || result.Principal.ID == "" || result.Tenant == "" || result.Method == "" ||
			(result.Principal.Kind != PrincipalUser && result.Principal.Kind != PrincipalService) {
			return Result{}, ErrUnauthenticated
		}
		return result, nil
	case StatusChallenge:
		if result.Challenge == nil {
			return Result{}, ErrUnauthenticated
		}
		return Result{Status: result.Status, Challenge: detach(result.Challenge)}, &ChallengeError{Challenge: *detach(result.Challenge)}
	case StatusPending:
		if result.Challenge == nil {
			return Result{}, ErrUnauthenticated
		}
		return Result{Status: result.Status, Challenge: detach(result.Challenge)}, &PendingError{Challenge: *detach(result.Challenge)}
	}
	return Result{}, ErrUnauthenticated
}

// detach copies challenge metadata so later mutation of the verifier-owned
// value cannot change what callers already received.
func detach(c *Challenge) *Challenge {
	return &Challenge{ID: c.ID, Reason: c.Reason, Methods: append([]string(nil), c.Methods...)}
}
