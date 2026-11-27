package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
)

func TestBearerCredentialExtraction(t *testing.T) {
	extract := BearerCredentials()
	r := httptest.NewRequest("POST", "/books", nil)
	if credential, present, err := extract(r); present || err != nil {
		t.Fatal("absent header reported as credential", credential, err)
	}
	for name, header := range map[string]string{
		"other-scheme":  "Basic dXNlcg==",
		"empty-token":   "Bearer ",
		"bare-scheme":   "Bearer",
		"inner-space":   "Bearer to ken",
		"leading-space": " Bearer token",
		"unknown":       "Token abc",
	} {
		r := httptest.NewRequest("POST", "/books", nil)
		r.Header.Set("Authorization", header)
		if _, present, err := extract(r); present || err == nil || err.Error() != "UNAUTHENTICATED" {
			t.Fatal(name, "malformed Authorization treated as absent or accepted", present, err)
		}
	}
	duplicated := httptest.NewRequest("POST", "/books", nil)
	duplicated.Header.Add("Authorization", "Bearer one")
	duplicated.Header.Add("Authorization", "Bearer two")
	if _, present, err := extract(duplicated); present || err == nil {
		t.Fatal("duplicate Authorization accepted", err)
	}
	r = httptest.NewRequest("POST", "/books", nil)
	r.Header.Set("Authorization", "Bearer token")
	credential, present, err := extract(r)
	if err != nil || !present || credential.Kind != identity.BearerKind || credential.Value != "token" {
		t.Fatal(credential, present, err)
	}
	for _, scheme := range []string{"bearer", "BEARER", "BeArEr"} {
		r = httptest.NewRequest("POST", "/books", nil)
		r.Header.Set("Authorization", scheme+" token")
		if credential, present, err = extract(r); err != nil || !present || credential.Value != "token" {
			t.Fatal(scheme, "case-insensitive scheme rejected", err)
		}
	}
	if _, _, err = extract(nil); err == nil {
		t.Fatal("nil request accepted")
	}
}

func TestCookieCredentialExtraction(t *testing.T) {
	extract := CookieCredentials("pangolin_session")
	if _, present, err := extract(httptest.NewRequest("GET", "/books", nil)); present || err != nil {
		t.Fatal("absent cookie reported", err)
	}
	// Cookie absence precedes the unsafe-method refusal: a POST without the
	// session cookie stays usable through other credentials.
	if _, present, err := extract(httptest.NewRequest("POST", "/books", nil)); present || err != nil {
		t.Fatal("cookie-free POST refused", err)
	}
	r := httptest.NewRequest("GET", "/books", nil)
	r.AddCookie(&http.Cookie{Name: "pangolin_session", Value: "s-1"})
	credential, present, err := extract(r)
	if err != nil || !present || credential.Kind != identity.SessionKind || credential.Value != "s-1" {
		t.Fatal(credential, present, err)
	}
	for name, cookieText := range map[string]string{
		"empty":      "pangolin_session=",
		"separators": "pangolin_session=a,b",
		"whitespace": "pangolin_session=a b",
	} {
		r := httptest.NewRequest("GET", "/books", nil)
		r.Header.Set("Cookie", cookieText)
		if _, present, err = extract(r); present || err == nil || err.Error() != "UNAUTHENTICATED" {
			t.Fatal(name, "malformed cookie accepted", err)
		}
	}
	// Unsafe methods with a present cookie are refused as the explicit CSRF
	// boundary.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		r = httptest.NewRequest(method, "/books", nil)
		r.AddCookie(&http.Cookie{Name: "pangolin_session", Value: "s-1"})
		if _, present, err = extract(r); present || err != ErrCookieUnsafeMethod {
			t.Fatal(method, "unsafe cookie method accepted", err)
		}
	}
	for _, method := range []string{http.MethodHead, http.MethodOptions} {
		r = httptest.NewRequest(method, "/books", nil)
		r.AddCookie(&http.Cookie{Name: "pangolin_session", Value: "s-1"})
		if _, present, err = extract(r); err != nil || !present {
			t.Fatal(method, err, present)
		}
	}
	// Duplicate named cookies are ambiguous, not first-wins.
	duplicated := httptest.NewRequest("GET", "/books", nil)
	duplicated.Header.Set("Cookie", "pangolin_session=s-1; pangolin_session=s-2")
	if _, present, err = extract(duplicated); present || err != ErrAmbiguousCredential {
		t.Fatal("duplicate cookies accepted", err)
	}
	if _, _, err = CookieCredentials("")(httptest.NewRequest("GET", "/b", nil)); err == nil {
		t.Fatal("empty cookie name accepted")
	}
	if _, _, err = extract(nil); err == nil {
		t.Fatal("nil request accepted")
	}
}

func TestOneCredentialExtraction(t *testing.T) {
	cookie := CookieCredentials("pangolin_session")
	bearer := BearerCredentials()
	one := OneCredential(cookie, bearer)
	session := httptest.NewRequest("GET", "/books", nil)
	session.AddCookie(&http.Cookie{Name: "pangolin_session", Value: "s-1"})
	if credential, present, err := one(session); err != nil || !present || credential.Kind != identity.SessionKind {
		t.Fatal(credential, err)
	}
	api := httptest.NewRequest("GET", "/books", nil)
	api.Header.Set("Authorization", "Bearer token")
	if credential, present, err := one(api); err != nil || !present || credential.Kind != identity.BearerKind {
		t.Fatal(credential, err)
	}
	// Cookie-free POST keeps the Bearer credential usable.
	post := httptest.NewRequest("POST", "/books", nil)
	post.Header.Set("Authorization", "Bearer token")
	if credential, present, err := one(post); err != nil || !present || credential.Kind != identity.BearerKind {
		t.Fatal("cookie-free POST lost its Bearer credential", credential, err)
	}
	if _, present, err := one(httptest.NewRequest("GET", "/books", nil)); present || err != nil {
		t.Fatal("absent credentials reported", err)
	}
	ambiguous := httptest.NewRequest("GET", "/books", nil)
	ambiguous.AddCookie(&http.Cookie{Name: "pangolin_session", Value: "s-1"})
	ambiguous.Header.Set("Authorization", "Bearer token")
	if _, present, err := one(ambiguous); present || err != ErrAmbiguousCredential {
		t.Fatal("ambiguous credentials accepted", err)
	}
	// A present cookie on an unsafe method fails closed; the Bearer credential
	// is not consulted as a fallback.
	unsafe := httptest.NewRequest("POST", "/books", nil)
	unsafe.AddCookie(&http.Cookie{Name: "pangolin_session", Value: "s-1"})
	unsafe.Header.Set("Authorization", "Bearer token")
	if _, present, err := one(unsafe); present || err != ErrCookieUnsafeMethod {
		t.Fatal("unsafe cookie fell back to bearer", err)
	}
	if _, _, err := OneCredential(nil, bearer)(api); err == nil {
		t.Fatal("nil extractor accepted")
	}
}

type pluggableSessionVerifier struct {
	fail, challenge, pending bool
	calls                    int
}

func (v *pluggableSessionVerifier) Kind() string { return identity.SessionKind }
func (v *pluggableSessionVerifier) Verify(_ context.Context, c identity.Credential) (identity.Result, error) {
	v.calls++
	if c.Kind != identity.SessionKind {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	if v.fail {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	if v.challenge {
		return identity.Result{Status: identity.StatusChallenge, Challenge: &identity.Challenge{ID: "mfa-1", Methods: []string{"mfa.totp"}}}, nil
	}
	if v.pending {
		return identity.Result{Status: identity.StatusPending, Challenge: &identity.Challenge{ID: "device-1"}}, nil
	}
	if c.Value != "s-alice" {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{
		Status:     identity.StatusAuthenticated,
		Principal:  scope.PrincipalRef{ID: "alice", Kind: identity.PrincipalUser},
		Tenant:     "tenant-a",
		Method:     "session",
		Assurances: []string{"pwd"},
	}, nil
}

type pluggableBearerVerifier struct{ calls int }

func (v *pluggableBearerVerifier) Kind() string { return identity.BearerKind }
func (v *pluggableBearerVerifier) Verify(_ context.Context, c identity.Credential) (identity.Result, error) {
	v.calls++
	if c.Value != "token" {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{
		Status:    identity.StatusAuthenticated,
		Principal: scope.PrincipalRef{ID: "alice", Kind: identity.PrincipalUser},
		Tenant:    "tenant-a",
		Method:    identity.MethodJWT,
	}, nil
}

func pluggableOptions(t *testing.T, verifiers []identity.Verifier, credentials CredentialExtractor) (PipelineOptions, *[]string) {
	t.Helper()
	stages := &[]string{}
	pipeline, err := identity.NewPipeline(verifiers...)
	if err != nil {
		t.Fatal(err)
	}
	m, err := uow.NewManager(&pipelineResource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	o := PipelineOptions{Timeout: time.Second, MaxBodyBytes: 1 << 10, Manager: m, ReadOnly: true,
		AuthenticateCredential: pipeline.AuthenticateCredential, Credentials: credentials,
		ResolveTenant: func(c context.Context, _ *http.Request) (context.Context, error) {
			*stages = append(*stages, "tenant")
			return c, nil
		},
		ResolveScope: func(c context.Context, _ *http.Request) (context.Context, error) {
			*stages = append(*stages, "scope")
			// Bind against the identity the trusted pipeline installed.
			return scope.NewTenantResolver(identity.Identity).Bind(c, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
		},
		Entry: func(context.Context) error { *stages = append(*stages, "entry"); return nil },
		Decode: func([]byte) (any, error) {
			*stages = append(*stages, "decode")
			return struct{}{}, nil
		},
		ResourcePolicy: func(context.Context, any) error { *stages = append(*stages, "resource"); return nil },
		Work: func(c context.Context, _ any) (any, error) {
			*stages = append(*stages, "work")
			principal, tenant, ok := identity.Identity(c)
			if !ok || principal.ID != "alice" || principal.Kind != identity.PrincipalUser || tenant != "tenant-a" {
				t.Fatal("verified identity missing in work", principal, tenant)
			}
			return "result", nil
		},
	}
	return o, stages
}

func TestPipelinePluggableSessionCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, method, cookieValue, bearerHeader string
		bearer                                  bool
		fail, challenge, pending                bool
		want                                    int
		code                                    string
		sessionCalls, bearerCalls               int
	}{
		{name: "valid-session", method: http.MethodGet, cookieValue: "s-alice", want: 200, sessionCalls: 1},
		{name: "invalid-session", method: http.MethodGet, cookieValue: "s-alice", fail: true, want: 401, code: "UNAUTHENTICATED", sessionCalls: 1},
		{name: "unknown-session", method: http.MethodGet, cookieValue: "s-unknown", want: 401, code: "UNAUTHENTICATED", sessionCalls: 1},
		{name: "challenge-step-up", method: http.MethodGet, cookieValue: "s-alice", challenge: true, want: 401, code: "AUTHENTICATION_CHALLENGE_REQUIRED", sessionCalls: 1},
		{name: "pending-device", method: http.MethodGet, cookieValue: "s-alice", pending: true, want: 401, code: "AUTHENTICATION_PENDING", sessionCalls: 1},
		{name: "unsafe-post-refused", method: http.MethodPost, cookieValue: "s-alice", want: 403, code: "COOKIE_UNSAFE_METHOD"},
		{name: "ambiguous-credentials", method: http.MethodGet, cookieValue: "s-alice", bearer: true, want: 400, code: "AMBIGUOUS_CREDENTIAL"},
		{name: "unregistered-bearer-kind", method: http.MethodGet, bearer: true, want: 401, code: "UNAUTHENTICATED"},
		{name: "bearer-post-no-cookie", method: http.MethodPost, bearerHeader: "token", want: 200, bearerCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := &pluggableSessionVerifier{fail: tc.fail, challenge: tc.challenge, pending: tc.pending}
			verifiers := []identity.Verifier{session}
			bearer := &pluggableBearerVerifier{}
			if tc.bearerCalls > 0 {
				verifiers = append(verifiers, bearer)
			}
			o, stages := pluggableOptions(t, verifiers, OneCredential(CookieCredentials("pangolin_session"), BearerCredentials()))
			p, err := NewPipeline(o)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(tc.method, "/books", nil)
			if tc.cookieValue != "" {
				r.AddCookie(&http.Cookie{Name: "pangolin_session", Value: tc.cookieValue})
			}
			if tc.bearer {
				r.Header.Set("Authorization", "Bearer token")
			}
			if tc.bearerHeader != "" {
				r.Header.Set("Authorization", "Bearer "+tc.bearerHeader)
			}
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.code != "" && !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatal("problem code mismatch", w.Body.String())
			}
			if session.calls != tc.sessionCalls {
				t.Fatal("unexpected session verifier calls", session.calls, tc.sessionCalls)
			}
			if bearer.calls != tc.bearerCalls {
				t.Fatal("unexpected bearer verifier calls", bearer.calls, tc.bearerCalls)
			}
			if tc.want != 200 && len(*stages) != 0 {
				t.Fatal("pipeline continued after authentication failure", *stages)
			}
			if tc.want == 200 && strings.Join(*stages, ",") != "tenant,scope,entry,decode,resource,work" {
				t.Fatal("stage order", *stages)
			}
		})
	}
}

func TestPipelineAuthenticatorConfiguration(t *testing.T) {
	o, _ := pluggableOptions(t, []identity.Verifier{&pluggableSessionVerifier{}}, CookieCredentials("pangolin_session"))
	o.Authenticate = func(context.Context, string) (context.Context, error) { return nil, nil }
	if _, err := NewPipeline(o); err == nil {
		t.Fatal("both authenticators accepted")
	}
	o, _ = pluggableOptions(t, []identity.Verifier{&pluggableSessionVerifier{}}, CookieCredentials("pangolin_session"))
	o.AuthenticateCredential = nil
	if _, err := NewPipeline(o); err == nil {
		t.Fatal("no authenticator accepted")
	}
}

// The legacy token-string Authenticate is Bearer-only by contract: a session
// credential extracted by a custom extractor must never reach it.
func TestPipelineLegacyAuthenticateRejectsNonBearer(t *testing.T) {
	received := ""
	o, _ := pluggableOptions(t, []identity.Verifier{&pluggableSessionVerifier{}}, CookieCredentials("pangolin_session"))
	o.AuthenticateCredential = nil
	o.Authenticate = func(_ context.Context, token string) (context.Context, error) {
		received = token
		return nil, identity.ErrUnauthenticated
	}
	p, err := NewPipeline(o)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/books", nil)
	r.AddCookie(&http.Cookie{Name: "pangolin_session", Value: "s-alice"})
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 401 || received != "" {
		t.Fatal("non-bearer credential reached the legacy token authenticator", w.Code, received)
	}
	if !strings.Contains(w.Body.String(), `"code":"UNAUTHENTICATED"`) {
		t.Fatal(w.Body.String())
	}
}
