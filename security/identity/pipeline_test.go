package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pangolin-framework/pangolin/security/scope"
)

type fakeVerifier struct {
	kind   string
	result Result
	err    error
	calls  int
}

func (v *fakeVerifier) Kind() string { return v.kind }
func (v *fakeVerifier) Verify(_ context.Context, _ Credential) (Result, error) {
	v.calls++
	if v.err != nil {
		return Result{}, v.err
	}
	return v.result, nil
}

func authenticated(kind, principalKind, tenant, method string) Result {
	return Result{Status: StatusAuthenticated, Principal: scope.PrincipalRef{ID: "alice", Kind: principalKind}, Tenant: tenant, Method: method}
}

func TestPipelineConstruction(t *testing.T) {
	if _, err := NewPipeline(); err == nil {
		t.Fatal("empty pipeline accepted")
	}
	if _, err := NewPipeline(nil); err == nil {
		t.Fatal("nil verifier accepted")
	}
	for _, kind := range []string{"", "two words", "tab\tkind"} {
		v := &fakeVerifier{kind: kind}
		if _, err := NewPipeline(v); err == nil {
			t.Fatal("invalid kind accepted:", kind)
		}
	}
	first := &fakeVerifier{kind: SessionKind}
	second := &fakeVerifier{kind: SessionKind}
	if _, err := NewPipeline(first, second); err == nil {
		t.Fatal("duplicate kind accepted")
	}
	bearer := &fakeVerifier{kind: BearerKind, result: authenticated(BearerKind, PrincipalUser, "a", "jwt")}
	service := &fakeVerifier{kind: ServiceKind, result: authenticated(ServiceKind, PrincipalService, "a", MethodService)}
	p, err := NewPipeline(bearer, service)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || len(p.verifiers) != 2 {
		t.Fatal("pipeline not built")
	}
}

func TestPipelineDispatchWithoutFallback(t *testing.T) {
	bearer := &fakeVerifier{kind: BearerKind, result: authenticated(BearerKind, PrincipalUser, "tenant-a", "jwt")}
	session := &fakeVerifier{kind: SessionKind, result: authenticated(SessionKind, PrincipalUser, "tenant-a", "session")}
	p, err := NewPipeline(bearer, session)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := p.AuthenticateCredential(context.Background(), Credential{Kind: SessionKind, Value: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if session.calls != 1 || bearer.calls != 0 {
		t.Fatal("wrong verifier dispatched", session.calls, bearer.calls)
	}
	if _, _, ok := Identity(ctx); !ok {
		t.Fatal("identity not installed")
	}
	ctx, err = p.AuthenticateCredential(context.Background(), Credential{Kind: BearerKind, Value: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if bearer.calls != 1 || session.calls != 1 {
		t.Fatal("bearer not dispatched", bearer.calls)
	}
	// Unregistered kind fails closed without consulting any verifier.
	if _, err = p.AuthenticateCredential(context.Background(), Credential{Kind: "api_key", Value: "k"}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
	if bearer.calls != 1 || session.calls != 1 {
		t.Fatal("unregistered kind consulted a verifier")
	}
	// A failing credential is never retried against another scheme.
	rejected := errors.New("signature rejected")
	bearer.err = rejected
	if _, err = p.AuthenticateCredential(context.Background(), Credential{Kind: BearerKind, Value: "bad"}); !errors.Is(err, rejected) || bearer.calls != 2 || session.calls != 1 {
		t.Fatal("fallback attempted", err, session.calls)
	}
	// The published Bearer entry point routes to the bearer verifier.
	if _, err = p.Authenticate(context.Background(), "token"); !errors.Is(err, rejected) {
		t.Fatal("legacy entry did not route to bearer", err)
	}
	bearer.err = nil
	if _, err = p.Authenticate(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if bearer.calls != 4 {
		t.Fatal("bearer not called via legacy entry", bearer.calls)
	}
}

func TestPipelineCredentialBounds(t *testing.T) {
	session := &fakeVerifier{kind: SessionKind, result: authenticated(SessionKind, PrincipalUser, "a", "session")}
	p, _ := NewPipeline(session)
	for name, credential := range map[string]Credential{
		"empty-kind":   {Value: "v"},
		"empty-value":  {Kind: SessionKind},
		"oversized":    {Kind: SessionKind, Value: strings.Repeat("x", MaxCredentialBytes+1)},
		"unknown-kind": {Kind: "cookie", Value: "v"},
	} {
		if _, err := p.AuthenticateCredential(context.Background(), credential); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal(name, err)
		}
	}
	if session.calls != 0 {
		t.Fatal("bounded credential reached verifier")
	}
	var nilPipeline *Pipeline
	if _, err := nilPipeline.AuthenticateCredential(context.Background(), Credential{Kind: SessionKind, Value: "v"}); err == nil {
		t.Fatal("nil pipeline accepted")
	}
	if _, err := p.AuthenticateCredential(nil, Credential{Kind: SessionKind, Value: "v"}); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestPipelineValidatesAuthenticatedResults(t *testing.T) {
	for name, result := range map[string]Result{
		"zero-status":        {},
		"missing-id":         {Status: StatusAuthenticated, Principal: scope.PrincipalRef{Kind: PrincipalUser}, Tenant: "a", Method: "session"},
		"missing-tenant":     {Status: StatusAuthenticated, Principal: scope.PrincipalRef{ID: "u", Kind: PrincipalUser}, Method: "session"},
		"missing-method":     {Status: StatusAuthenticated, Principal: scope.PrincipalRef{ID: "u", Kind: PrincipalUser}, Tenant: "a"},
		"invalid-kind":       authenticated(SessionKind, "admin", "a", "session"),
		"challenge-attached": {Status: StatusAuthenticated, Principal: scope.PrincipalRef{ID: "u", Kind: PrincipalUser}, Tenant: "a", Method: "session", Challenge: &Challenge{ID: "c"}},
	} {
		verifier := &fakeVerifier{kind: SessionKind, result: result}
		p, _ := NewPipeline(verifier)
		if ctx, err := p.AuthenticateCredential(context.Background(), Credential{Kind: SessionKind, Value: "v"}); !errors.Is(err, ErrUnauthenticated) || ctx != nil {
			t.Fatal(name, err)
		}
		if _, _, ok := Identity(context.Background()); ok {
			t.Fatal("unauthenticated identity readable")
		}
	}
}

func TestChallengeAndPendingNeverInstall(t *testing.T) {
	session := &fakeVerifier{kind: SessionKind, result: authenticated(SessionKind, PrincipalUser, "tenant-a", "session")}
	p, _ := NewPipeline(session)
	base := context.Background()
	verified, err := p.AuthenticateCredential(base, Credential{Kind: SessionKind, Value: "valid"})
	if err != nil {
		t.Fatal(err)
	}
	if principal, tenant, ok := Identity(verified); !ok || principal.ID != "alice" || principal.Kind != PrincipalUser || tenant != "tenant-a" {
		t.Fatal(principal, tenant)
	}
	for name, result := range map[string]Result{
		"challenge":                 {Status: StatusChallenge, Principal: scope.PrincipalRef{ID: "alice", Kind: PrincipalUser}, Challenge: &Challenge{ID: "mfa-1", Reason: "otp", Methods: []string{"mfa.totp"}}},
		"pending":                   {Status: StatusPending, Challenge: &Challenge{ID: "device-1", Reason: "approval"}},
		"challenge-without-details": {Status: StatusChallenge},
		"pending-without-details":   {Status: StatusPending},
	} {
		session.result, session.err = result, nil
		attempt, err := p.AuthenticateCredential(verified, Credential{Kind: SessionKind, Value: "unfinished"})
		if attempt != nil {
			t.Fatal(name, "unfinished authentication returned a context")
		}
		switch name {
		case "challenge":
			var challenge *ChallengeError
			if !errors.Is(err, ErrChallengeRequired) || !errors.As(err, &challenge) || challenge.Challenge.ID != "mfa-1" || len(challenge.Challenge.Methods) != 1 {
				t.Fatal(name, err)
			}
		case "pending":
			var pending *PendingError
			if !errors.Is(err, ErrPending) || !errors.As(err, &pending) || pending.Challenge.ID != "device-1" {
				t.Fatal(name, err)
			}
		default:
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatal(name, err)
			}
		}
		// The previously verified identity is neither replaced nor exposed by
		// the failed attempt; the failed context cannot be used to bind scopes.
		principal, tenant, ok := Identity(verified)
		if !ok || principal.ID != "alice" || tenant != "tenant-a" {
			t.Fatal(name, "previous identity corrupted")
		}
		if resolverScope, bound := scope.Current(verified); bound || resolverScope != (scope.ScopeBinding{}) {
			t.Fatal(name, "unfinished authentication bound a scope")
		}
	}
}

func TestPipelineCancellation(t *testing.T) {
	session := &fakeVerifier{kind: SessionKind, result: authenticated(SessionKind, PrincipalUser, "a", "session")}
	p, _ := NewPipeline(session)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.AuthenticateCredential(cancelled, Credential{Kind: SessionKind, Value: "v"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if session.calls != 0 {
		t.Fatal("cancelled request reached verifier")
	}
}

func TestMethodMetadataImmutability(t *testing.T) {
	assurances := []string{"pwd", "mfa.totp"}
	session := &fakeVerifier{kind: SessionKind, result: Result{
		Status:     StatusAuthenticated,
		Principal:  scope.PrincipalRef{ID: "alice", Kind: PrincipalUser},
		Tenant:     "tenant-a",
		Method:     "session",
		Assurances: assurances,
	}}
	p, _ := NewPipeline(session)
	ctx, err := p.AuthenticateCredential(context.Background(), Credential{Kind: SessionKind, Value: "s"})
	if err != nil {
		t.Fatal(err)
	}
	method, reported, ok := Method(ctx)
	if !ok || method != "session" || len(reported) != 2 || reported[0] != "pwd" || reported[1] != "mfa.totp" {
		t.Fatal(method, reported)
	}
	// Mutating the verifier-owned and reader-returned slices cannot change the
	// installed metadata.
	assurances[0] = "tampered"
	reported[1] = "tampered"
	method, reported, _ = Method(ctx)
	if method != "session" || reported[0] != "pwd" || reported[1] != "mfa.totp" {
		t.Fatal("metadata mutated", reported)
	}
	if _, reported, ok := Method(context.Background()); ok || reported != nil {
		t.Fatal("unauthenticated metadata readable")
	}
	if _, _, ok := Method(nil); ok {
		t.Fatal("nil context metadata readable")
	}
}

func TestCancellationDuringVerifierNeverInstalls(t *testing.T) {
	inner := &fakeVerifier{kind: SessionKind, result: authenticated(SessionKind, PrincipalUser, "tenant-a", "session")}
	blocking := &blockingVerifier{inner: inner, entered: make(chan struct{}), release: make(chan struct{})}
	p, err := NewPipeline(blocking)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		ctx context.Context
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		authenticatedCtx, e := p.AuthenticateCredential(ctx, Credential{Kind: SessionKind, Value: "s"})
		done <- outcome{authenticatedCtx, e}
	}()
	<-blocking.entered
	cancel()
	// The verifier ignores cancellation and reports success afterwards.
	close(blocking.release)
	out := <-done
	if out.ctx != nil || !errors.Is(out.err, context.Canceled) {
		t.Fatal(out.ctx, out.err)
	}
	if _, _, ok := Identity(ctx); ok {
		t.Fatal("canceled verification installed identity")
	}
}

type blockingVerifier struct {
	inner   *fakeVerifier
	entered chan struct{}
	release chan struct{}
}

func (v *blockingVerifier) Kind() string { return SessionKind }
func (v *blockingVerifier) Verify(ctx context.Context, credential Credential) (Result, error) {
	close(v.entered)
	<-v.release
	return v.inner.Verify(ctx, credential)
}

func TestChallengeMetadataDetached(t *testing.T) {
	methods := []string{"mfa.totp"}
	session := &fakeVerifier{kind: SessionKind, result: Result{Status: StatusChallenge, Challenge: &Challenge{ID: "mfa-1", Methods: methods}}}
	p, _ := NewPipeline(session)
	_, err := p.AuthenticateCredential(context.Background(), Credential{Kind: SessionKind, Value: "s"})
	var challenge *ChallengeError
	if !errors.Is(err, ErrChallengeRequired) || !errors.As(err, &challenge) || challenge.Challenge.ID != "mfa-1" || len(challenge.Challenge.Methods) != 1 {
		t.Fatal(err)
	}
	// Later mutation of the verifier-owned metadata cannot change what the
	// caller already received.
	methods[0] = "tampered"
	session.result.Challenge.ID = "tampered"
	if challenge.Challenge.ID != "mfa-1" || challenge.Challenge.Methods[0] != "mfa.totp" {
		t.Fatal("challenge metadata mutated after report")
	}
}

func TestDistinctUserAndServiceIdentities(t *testing.T) {
	user := &fakeVerifier{kind: SessionKind, result: authenticated(SessionKind, PrincipalUser, "tenant-a", "session")}
	service := &fakeVerifier{kind: ServiceKind, result: Result{Status: StatusAuthenticated, Principal: scope.PrincipalRef{ID: "worker", Kind: PrincipalService}, Tenant: "tenant-a", Method: MethodService}}
	p, err := NewPipeline(user, service)
	if err != nil {
		t.Fatal(err)
	}
	userCtx, err := p.AuthenticateCredential(context.Background(), Credential{Kind: SessionKind, Value: "s"})
	if err != nil {
		t.Fatal(err)
	}
	serviceCtx, err := p.AuthenticateCredential(context.Background(), Credential{Kind: ServiceKind, Value: "mTLS"})
	if err != nil {
		t.Fatal(err)
	}
	userPrincipal, _, _ := Identity(userCtx)
	servicePrincipal, _, _ := Identity(serviceCtx)
	if userPrincipal.Kind != PrincipalUser || servicePrincipal.Kind != PrincipalService {
		t.Fatal("identity kinds not preserved", userPrincipal, servicePrincipal)
	}
	if userMethod, _, _ := Method(userCtx); userMethod != "session" {
		t.Fatal("user method not preserved")
	}
	if serviceMethod, _, _ := Method(serviceCtx); serviceMethod != MethodService {
		t.Fatal("service method not preserved")
	}
}
