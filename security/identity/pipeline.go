package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/cymondez/pangolin-framework/security/scope"
)

// Credential kinds select the registered verifier. Transport adapters own
// extraction; a credential value never carries parsed identity claims.
const (
	BearerKind  = "bearer"
	SessionKind = "session"
	ServiceKind = "service"
)

// Validated principal kinds. User and service identities stay distinct: a
// verifier for user credentials must not mint service principals.
const (
	PrincipalUser    = "user"
	PrincipalService = "service"
)

// Method identifiers for framework-provided verifiers. Custom verifiers use
// their own stable, non-secret method names.
const (
	MethodJWT     = "jwt"
	MethodService = "service"
)

// MaxCredentialBytes bounds any single credential value accepted by the
// pipeline regardless of protocol.
const MaxCredentialBytes = 16384

var (
	// ErrUnauthenticated is the fail-closed sentinel for unusable credentials.
	ErrUnauthenticated = errors.New("UNAUTHENTICATED")
	// ErrChallengeRequired reports an unfinished step-up flow (for example
	// MFA); no identity was installed.
	ErrChallengeRequired = errors.New("AUTHENTICATION_CHALLENGE_REQUIRED")
	// ErrPending reports verification awaiting out-of-band completion (for
	// example device login approval); no identity was installed.
	ErrPending = errors.New("AUTHENTICATION_PENDING")
)

// Credential is one extracted, unverified credential.
type Credential struct {
	Kind  string
	Value string
}

// Status of a verifier result. The zero value is never authenticated.
type Status int

const (
	StatusAuthenticated Status = iota + 1
	StatusChallenge
	StatusPending
)

// Challenge carries the non-secret continuation metadata of an unfinished
// authentication, such as which additional methods a step-up flow requires.
// Session state and login/redirect endpoints belong to the verifying module.
type Challenge struct {
	ID      string
	Reason  string
	Methods []string
}

// ChallengeError reports a challenge outcome without installing identity.
type ChallengeError struct{ Challenge Challenge }

func (e *ChallengeError) Error() string { return ErrChallengeRequired.Error() }
func (e *ChallengeError) Unwrap() error { return ErrChallengeRequired }

// PendingError reports an out-of-band pending outcome without installing
// identity.
type PendingError struct{ Challenge Challenge }

func (e *PendingError) Error() string { return ErrPending.Error() }
func (e *PendingError) Unwrap() error { return ErrPending }

// Result is the verifier's completed outcome. Verifiers return results; only
// the trusted pipeline installs authenticated identity.
type Result struct {
	Status     Status
	Principal  scope.PrincipalRef
	Tenant     string
	Method     string
	Assurances []string
	Challenge  *Challenge
}

// Verifier verifies one credential kind and returns an authentication result.
// Implementations are trusted, explicitly constructor-registered adapters
// (JWT, a user session store, an API-key registry, a future OIDC or device
// login module); they never install identity themselves. The pipeline keeps
// the Principal/Tenant/Scope authority rules: the returned tenant stays a
// hint that resolvers re-verify against current membership.
type Verifier interface {
	// Kind must match the Credential.Kind this verifier accepts.
	Kind() string
	Verify(context.Context, Credential) (Result, error)
}

// Pipeline is the trusted authentication orchestration: it dispatches one
// credential to the verifier registered for its kind, validates the returned
// result and installs identity under the private context key. There is no
// fallback between kinds: a credential for an unregistered or failing kind
// fails closed without trying another scheme.
type Pipeline struct {
	verifiers map[string]Verifier
}

// NewPipeline registers trusted verifiers explicitly. Duplicate, empty or
// invalid kinds are rejected at construction; registration order is
// irrelevant because dispatch is by credential kind.
func NewPipeline(verifiers ...Verifier) (*Pipeline, error) {
	p := &Pipeline{verifiers: make(map[string]Verifier, len(verifiers))}
	for _, verifier := range verifiers {
		if verifier == nil {
			return nil, ErrUnauthenticated
		}
		kind := verifier.Kind()
		if kind == "" || strings.ContainsAny(kind, " \t\r\n") || len(kind) > 64 {
			return nil, ErrUnauthenticated
		}
		if _, duplicate := p.verifiers[kind]; duplicate {
			return nil, ErrUnauthenticated
		}
		p.verifiers[kind] = verifier
	}
	if len(p.verifiers) == 0 {
		return nil, ErrUnauthenticated
	}
	return p, nil
}

// AuthenticateCredential verifies one credential and installs trusted
// identity. Verifier errors are propagated unchanged; challenge and pending
// outcomes return typed errors without installing identity, so unfinished
// flows cannot bind scopes or reuse a previous identity.
func (p *Pipeline) AuthenticateCredential(ctx context.Context, credential Credential) (context.Context, error) {
	if p == nil || ctx == nil {
		return nil, ErrUnauthenticated
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if credential.Kind == "" || credential.Value == "" || len(credential.Value) > MaxCredentialBytes {
		return nil, ErrUnauthenticated
	}
	verifier, ok := p.verifiers[credential.Kind]
	if !ok {
		return nil, ErrUnauthenticated
	}
	result, err := verifier.Verify(ctx, credential)
	if err != nil {
		return nil, err
	}
	// A verifier may ignore cancellation; canceled work never gains identity.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err = complete(result)
	if err != nil {
		return nil, err
	}
	return install(ctx, result)
}

// Authenticate keeps the published Bearer token entry point: it verifies one
// Authorization: Bearer value against the registered bearer verifier.
func (p *Pipeline) Authenticate(ctx context.Context, bearerToken string) (context.Context, error) {
	return p.AuthenticateCredential(ctx, Credential{Kind: BearerKind, Value: bearerToken})
}
