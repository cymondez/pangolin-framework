// Package http_test proves extensible authentication with an external
// test-package adapter: a custom user Session verifier and a real signed-JWT
// Bearer validator both install identity through the trusted identity
// pipeline and reach Bookstore -> Tenant -> Scope -> application policy.
package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cymondez/pangolin-framework/examples/bookstore/domain"
	bookstorehttp "github.com/cymondez/pangolin-framework/examples/bookstore/transport/http"
	"github.com/cymondez/pangolin-framework/internal/testkit/idp"
	"github.com/cymondez/pangolin-framework/kernel/runtime"
	"github.com/cymondez/pangolin-framework/security/authn"
	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/policy"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
	frameworkhttp "github.com/cymondez/pangolin-framework/transport/http"
)

// userSessions is an external, custom user Session verifier: it owns its
// session state, returns verification results and never installs identity.
// Session values map to a current user and tenant; tenant stays a hint that
// the resolvers re-verify.
type userSessions struct {
	users     map[string]string
	tenants   map[string]string
	challenge map[string]bool
	pending   map[string]bool
	calls     int
}

func (s *userSessions) Kind() string { return identity.SessionKind }
func (s *userSessions) Verify(_ context.Context, c identity.Credential) (identity.Result, error) {
	s.calls++
	if c.Kind != identity.SessionKind {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	subject, ok := s.users[c.Value]
	if !ok {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	if s.challenge[c.Value] {
		return identity.Result{Status: identity.StatusChallenge, Challenge: &identity.Challenge{ID: "mfa-1", Reason: "step-up", Methods: []string{"mfa.totp"}}}, nil
	}
	if s.pending[c.Value] {
		return identity.Result{Status: identity.StatusPending, Challenge: &identity.Challenge{ID: "device-1", Reason: "approval"}}, nil
	}
	return identity.Result{
		Status:     identity.StatusAuthenticated,
		Principal:  scope.PrincipalRef{ID: subject, Kind: identity.PrincipalUser},
		Tenant:     s.tenants[c.Value],
		Method:     "session",
		Assurances: []string{"pwd"},
	}, nil
}

// userBooks records the application policy context: the trusted identity and
// bound scope every call actually observed.
type userBooks struct {
	calls      int
	principals []scope.PrincipalRef
}

func (b *userBooks) Create(ctx context.Context, id, title string) (domain.Book, error) {
	b.calls++
	principal, tenant, ok := identity.Identity(ctx)
	if !ok {
		return domain.Book{}, errors.New("identity missing in application")
	}
	b.principals = append(b.principals, principal)
	binding, bound := scope.Current(ctx)
	if !bound || binding.Principal != principal || binding.Ref.Kind != "tenant" || binding.Ref.TenantID != tenant || principal.Kind != identity.PrincipalUser {
		return domain.Book{}, errors.New("scope binding missing or forged in application")
	}
	return domain.Book{ID: id, Title: title, Version: 1}, nil
}
func (b *userBooks) Get(ctx context.Context, id string) (domain.Book, error) {
	return b.Create(ctx, id, "title")
}

// dualHandler composes the Bookstore with both the external session verifier
// and the real signed-JWT validator through one trusted pipeline.
func dualHandler(t *testing.T) (*bookstorehttp.Handler, *userSessions, *userBooks, string) {
	t.Helper()
	provider, err := idp.New("http://idp.example")
	if err != nil {
		t.Fatal(err)
	}
	route := provider.Handler()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		route.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	w := httptest.NewRecorder()
	route.ServeHTTP(w, httptest.NewRequest("POST", "http://idp.example/token", strings.NewReader(`{"subject":"alice","tenant_id":"tenant-a"}`)))
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	sessions := &userSessions{
		users:     map[string]string{"s-alice": "alice", "s-bob": "bob", "s-mfa": "alice", "s-device": "alice"},
		tenants:   map[string]string{"s-alice": "tenant-a", "s-bob": "tenant-b", "s-mfa": "tenant-a", "s-device": "tenant-a"},
		challenge: map[string]bool{"s-mfa": true},
		pending:   map[string]bool{"s-device": true},
	}
	validator := authn.NewValidator("http://idp.example", "pangolin-bookstore", "http://idp.example/jwks.json", client)
	pipeline, err := identity.NewPipeline(sessions, validator)
	if err != nil {
		t.Fatal(err)
	}
	books := &userBooks{}
	handler := &bookstorehttp.Handler{
		Books:          books,
		CredentialAuth: pipeline,
		Credentials:    frameworkhttp.OneCredential(frameworkhttp.CookieCredentials("pangolin_session"), frameworkhttp.BearerCredentials()),
		Scopes:         scope.NewTenantResolver(identity.Identity),
		Work:           runtime.NewWorkGroup(),
		Ready:          func() bool { return true },
	}
	return handler, sessions, books, token.AccessToken
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExternalUserSessionReachesBookstore(t *testing.T) {
	handler, sessions, books, jwt := dualHandler(t)
	for _, tc := range []struct {
		name, method, path, body, cookie, bearer string
		headers                                  map[string]string
		want                                     int
		code                                     string
	}{
		{name: "session-get", method: http.MethodGet, path: "/books/book", cookie: "s-alice", want: 200},
		{name: "tenant-hint-forgery", method: http.MethodGet, path: "/books/book", cookie: "s-alice", headers: map[string]string{"X-Tenant-ID": "tenant-b"}, want: 403, code: "PERMISSION_DENIED"},
		{name: "scope-id-forgery", method: http.MethodGet, path: "/books/book", cookie: "s-alice", headers: map[string]string{"X-Scope-ID": "tenant-b"}, want: 403, code: "PERMISSION_DENIED"},
		{name: "scope-kind-unsupported", method: http.MethodGet, path: "/books/book", cookie: "s-alice", headers: map[string]string{"X-Scope-Kind": "org.organization"}, want: 400, code: "SCOPE_KIND_UNSUPPORTED"},
		{name: "session-post-unsafe-refused", method: http.MethodPost, path: "/books", body: `{"id":"b","title":"t"}`, cookie: "s-alice", want: 403, code: "COOKIE_UNSAFE_METHOD"},
		{name: "jwt-post-no-cookie", method: http.MethodPost, path: "/books", body: `{"id":"b","title":"t"}`, bearer: jwt, want: 201},
		{name: "jwt-get", method: http.MethodGet, path: "/books/book", bearer: jwt, want: 200},
		{name: "invalid-session", method: http.MethodGet, path: "/books/book", cookie: "s-unknown", want: 401, code: "UNAUTHENTICATED"},
		{name: "challenge-step-up", method: http.MethodGet, path: "/books/book", cookie: "s-mfa", want: 401, code: "AUTHENTICATION_CHALLENGE_REQUIRED"},
		{name: "pending-device", method: http.MethodGet, path: "/books/book", cookie: "s-device", want: 401, code: "AUTHENTICATION_PENDING"},
		{name: "ambiguous-credentials", method: http.MethodGet, path: "/books/book", cookie: "s-alice", bearer: jwt, want: 400, code: "AMBIGUOUS_CREDENTIAL"},
		{name: "missing-credentials", method: http.MethodGet, path: "/books/book", want: 401, code: "UNAUTHENTICATED"},
		{name: "cross-tenant-session-blocked", method: http.MethodGet, path: "/books/book", cookie: "s-bob", headers: map[string]string{"X-Tenant-ID": "tenant-a"}, want: 403, code: "PERMISSION_DENIED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := books.calls
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "pangolin_session", Value: tc.cookie})
			}
			if tc.bearer != "" {
				r.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.code != "" && !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatal("problem code mismatch", w.Body.String())
			}
			reached := books.calls - before
			if tc.want >= 400 && reached != 0 {
				t.Fatal("application reached after failure", reached)
			}
			if tc.want < 400 && reached != 1 {
				t.Fatal("application not reached exactly once", reached)
			}
		})
	}
	if sessions.calls == 0 {
		t.Fatal("external session verifier never consulted")
	}
	// Every successful application call observed a distinct user principal,
	// never a service identity.
	for _, principal := range books.principals {
		if principal.Kind != identity.PrincipalUser || principal.ID != "alice" {
			t.Fatal("user identity not preserved", principal)
		}
	}
}

func TestExternalSessionVerifierIsTrustedRegistration(t *testing.T) {
	_, sessions, _, _ := dualHandler(t)
	if _, err := identity.NewPipeline(sessions); err != nil {
		t.Fatal(err)
	}
	duplicate := &userSessions{}
	if _, err := identity.NewPipeline(sessions, duplicate); err == nil {
		t.Fatal("duplicate session kind accepted")
	}
	if _, err := identity.NewPipeline(); err == nil {
		t.Fatal("empty registration accepted")
	}
}

func TestHandlerWithoutAuthenticators(t *testing.T) {
	handler, _, _, _ := dualHandler(t)
	handler.CredentialAuth = nil
	handler.Auth = nil
	r := httptest.NewRequest(http.MethodGet, "/books/book", nil)
	r.Header.Set("Authorization", "Bearer anything")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 || !strings.Contains(w.Body.String(), `"code":"UNAUTHENTICATED"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestHandlerRejectsBothAuthenticators(t *testing.T) {
	handler, _, books, _ := dualHandler(t)
	handler.Auth = authenticatorFunc(func(context.Context, string) (context.Context, error) {
		return nil, errors.New("misconfigured entry must never be called")
	})
	before := books.calls
	r := httptest.NewRequest(http.MethodGet, "/books/book", nil)
	r.Header.Set("Authorization", "Bearer anything")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"code":"AUTH_MISCONFIGURED"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if books.calls != before {
		t.Fatal("application reached through misconfigured authentication")
	}
}

type authenticatorFunc func(context.Context, string) (context.Context, error)

func (f authenticatorFunc) Authenticate(ctx context.Context, token string) (context.Context, error) {
	return f(ctx, token)
}

// trustedAuthority is the CURRENT tenant membership projection for the
// policy test: membership can be revoked per request and the source can
// fail like a real authority outage.
type trustedAuthority struct {
	revoked map[string]bool
	outage  bool
}

func (a *trustedAuthority) Check(_ context.Context, p scope.PrincipalRef, tenant string) (string, error) {
	if a.outage {
		return "", fmt.Errorf("authority offline: %w", scope.ErrAuthorityUnavailable)
	}
	if a.revoked[p.ID+"/"+tenant] {
		return "", nil
	}
	switch {
	case p.ID == "alice" && tenant == "tenant-a":
		return "rev-alice", nil
	case p.ID == "carol" && tenant == "tenant-a":
		return "rev-carol", nil
	}
	return "", scope.ErrDenied
}

// authorityScope resolves the tenant against the current trusted authority
// and then binds through the provider-backed registry, the same trust chain
// the generated application uses.
type authorityScopeResolver struct {
	tenants  *tenancy.Resolver
	registry *scope.Registry
}

func (a *authorityScopeResolver) Bind(ctx context.Context, ref scope.ScopeRef) (context.Context, error) {
	resolved, err := a.tenants.Resolve(ctx, "", ref.TenantID)
	if err != nil {
		return nil, err
	}
	return a.registry.Bind(resolved, ref)
}
func (a *authorityScopeResolver) Current(ctx context.Context) (scope.ScopeBinding, bool) {
	return scope.Current(ctx)
}

// policyBooks is the application boundary: it executes the real use-case
// authorization (policy.Require) before completing any work, the same way a
// sensitive use case does. attempts counts handler dispatches; completed
// counts use-cases that actually passed policy.
type policyBooks struct {
	registry  *scope.Registry
	attempts  int
	completed int
}

func (b *policyBooks) Create(ctx context.Context, id, title string) (domain.Book, error) {
	b.attempts++
	if err := policy.Require(ctx, b.registry, "books.write", nil); err != nil {
		return domain.Book{}, err
	}
	b.completed++
	return domain.Book{ID: id, Title: title, Version: 1}, nil
}
func (b *policyBooks) Get(ctx context.Context, id string) (domain.Book, error) {
	b.attempts++
	if err := policy.Require(ctx, b.registry, "books.read", nil); err != nil {
		return domain.Book{}, err
	}
	b.completed++
	return domain.Book{ID: id, Title: "title", Version: 1}, nil
}

// authorityComposition wires the custom user Session verifier through the
// trusted identity pipeline into the current-authority tenant resolution,
// the provider-backed scope registry and real policy evaluation.
type authorityComposition struct {
	handler  *bookstorehttp.Handler
	books    *policyBooks
	tenants  *tenancy.Resolver
	registry *scope.Registry
	pipeline *identity.Pipeline
}

func authorityCompositionFor(t *testing.T, authority *trustedAuthority) *authorityComposition {
	t.Helper()
	sessions := &userSessions{
		users:   map[string]string{"s-alice": "alice", "s-carol": "carol"},
		tenants: map[string]string{"s-alice": "tenant-a", "s-carol": "tenant-a"},
	}
	pipeline, err := identity.NewPipeline(sessions)
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := tenancy.NewResolver(identity.Identity, authority)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
		Authority: authority,
		Permission: func(_ context.Context, b scope.ScopeBinding, action string) error {
			if action != "books.read" && action != "books.write" {
				return scope.ErrDenied
			}
			rules := policy.Rules{Tenant: policy.Allow}
			if b.Principal.ID == "carol" {
				// Explicit user deny wins over the tenant-level allow.
				rules.User = policy.Deny
			}
			if !policy.Allowed(rules) {
				return scope.ErrDenied
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	books := &policyBooks{registry: registry}
	handler := &bookstorehttp.Handler{
		Books:          books,
		CredentialAuth: pipeline,
		Credentials:    frameworkhttp.OneCredential(frameworkhttp.CookieCredentials("pangolin_session")),
		Scopes:         &authorityScopeResolver{tenants: tenants, registry: registry},
		Work:           runtime.NewWorkGroup(),
		Ready:          func() bool { return true },
	}
	return &authorityComposition{handler: handler, books: books, tenants: tenants, registry: registry, pipeline: pipeline}
}

func sessionRequest(value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/books/book", nil)
	r.AddCookie(&http.Cookie{Name: "pangolin_session", Value: value})
	return r
}

// TestExternalSessionCurrentAuthorityAndPolicy proves a custom user Session
// identity reaches the use-case only through CURRENT tenant authority and
// real policy evaluation: explicit deny, revocation and authority outage all
// prevent the use-case.
func TestExternalSessionCurrentAuthorityAndPolicy(t *testing.T) {
	authority := &trustedAuthority{revoked: map[string]bool{}}
	composed := authorityCompositionFor(t, authority)

	// Current member with an allowing policy: the use-case completes through
	// the full Bookstore -> Tenant -> Scope -> policy chain.
	w := httptest.NewRecorder()
	composed.handler.ServeHTTP(w, sessionRequest("s-alice"))
	if w.Code != 200 || composed.books.completed != 1 {
		t.Fatal(w.Code, w.Body.String(), composed.books.completed)
	}

	// Explicit deny: carol is a current member of the same tenant, but the
	// real policy evaluation denies her user decision first.
	w = httptest.NewRecorder()
	composed.handler.ServeHTTP(w, sessionRequest("s-carol"))
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"code":"PERMISSION_DENIED"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if composed.books.attempts != 2 || composed.books.completed != 1 {
		t.Fatal("denied use-case must not complete", composed.books.attempts, composed.books.completed)
	}

	// Revocation between requests: the current authority re-check at tenant
	// resolution prevents the use-case before the application is reached.
	authority.revoked["alice/tenant-a"] = true
	w = httptest.NewRecorder()
	composed.handler.ServeHTTP(w, sessionRequest("s-alice"))
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"SCOPE_AUTHORITY_UNAVAILABLE"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if composed.books.attempts != 2 {
		t.Fatal("revoked request reached the application", composed.books.attempts)
	}

	// Authority outage: access fails closed with no fallback.
	authority.outage = true
	w = httptest.NewRecorder()
	composed.handler.ServeHTTP(w, sessionRequest("s-alice"))
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"SCOPE_AUTHORITY_UNAVAILABLE"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if composed.books.attempts != 2 {
		t.Fatal("outage request reached the application", composed.books.attempts)
	}
	authority.outage = false

	// A previously admitted binding cannot survive revocation either:
	// policy.Require re-checks the CURRENT authority at the use-case.
	delete(authority.revoked, "alice/tenant-a")
	authenticated, err := composed.pipeline.AuthenticateCredential(context.Background(), identity.Credential{Kind: identity.SessionKind, Value: "s-alice"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := composed.tenants.Resolve(authenticated, "", "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := composed.registry.Bind(resolved, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	authority.revoked["alice/tenant-a"] = true
	if err = policy.Require(bound, composed.registry, "books.write", nil); !errors.Is(err, scope.ErrAuthorityUnavailable) {
		t.Fatal("stale admitted binding survived revocation", err)
	}
	if err = policy.Require(bound, composed.registry, "books.write", func(context.Context) error {
		t.Fatal("resource policy evaluated after authorization denial")
		return nil
	}); !errors.Is(err, scope.ErrAuthorityUnavailable) {
		t.Fatal(err)
	}
}
