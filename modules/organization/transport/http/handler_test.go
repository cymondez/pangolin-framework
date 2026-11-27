package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/modules/organization"
	"github.com/pangolin-framework/pangolin/modules/organization/contracts"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	transport "github.com/pangolin-framework/pangolin/transport/http"
)

type management struct {
	calls  int
	err    error
	tenant string
}

func (m *management) done() error { m.calls++; return m.err }
func (m *management) CreateEnterprise(_ context.Context, c contracts.CreateEnterprise) (contracts.Enterprise, error) {
	m.tenant = c.TenantID
	return contracts.Enterprise{}, m.done()
}
func (m *management) CreateOrganization(_ context.Context, c contracts.CreateOrganization) (contracts.Organization, error) {
	m.tenant = c.TenantID
	return contracts.Organization{}, m.done()
}
func (m *management) ListMyOrganizations(context.Context, contracts.PageRequest) (contracts.OrganizationPage, error) {
	return contracts.OrganizationPage{}, m.done()
}
func (m *management) ListEnterpriseOrganizations(context.Context, scope.ScopeRef, contracts.PageRequest) (contracts.OrganizationPage, error) {
	return contracts.OrganizationPage{}, m.done()
}
func (m *management) SetOrganizationMembership(context.Context, contracts.SetMembership) (contracts.Membership, error) {
	return contracts.Membership{}, m.done()
}
func (m *management) RemoveOrganizationMembership(context.Context, contracts.RemoveMembership) error {
	return m.done()
}
func (m *management) SetEnterpriseRole(context.Context, contracts.SetEnterpriseRole) error {
	return m.done()
}
func (m *management) AssignEnterprise(context.Context, contracts.AssignEnterprise) (contracts.Organization, error) {
	return contracts.Organization{}, m.done()
}
func (m *management) Suspend(context.Context, contracts.Command) error     { return m.done() }
func (m *management) Resume(context.Context, contracts.Command) error      { return m.done() }
func (m *management) Archive(context.Context, contracts.Command) error     { return m.done() }
func (m *management) JoinAsOwner(context.Context, contracts.Command) error { return m.done() }
func (m *management) Recover(context.Context, contracts.Command, scope.PrincipalRef) error {
	return m.done()
}

type resource struct{ begins int }

func (r *resource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	r.begins++
	return c, transaction{}, nil
}

type transaction struct{}

func (transaction) Commit(context.Context) error   { return nil }
func (transaction) Rollback(context.Context) error { return nil }
func fixture(t *testing.T) (Options, *management, *resource) {
	t.Helper()
	r := &resource{}
	m := &management{}
	manager, e := uow.NewManager(r, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if e != nil {
		t.Fatal(e)
	}
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	return Options{Management: m, Manager: manager, Timeout: time.Second, MaxBodyBytes: 512, Authenticate: func(c context.Context, _ string) (context.Context, error) { return c, nil }, ResolveTenant: func(c context.Context, _ *http.Request) (context.Context, error) { return c, nil }, ResolveScope: func(c context.Context, _ *http.Request) (context.Context, error) {
		return resolver.Bind(c, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	}, Authorize: func(context.Context, string) error { return nil }}, m, r
}
func serve(t *testing.T, o Options, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	h, e := NewHandler(o)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer verified")
	r.Header.Set("Idempotency-Key", key)
	h.ServeHTTP(w, r)
	return w
}
func TestControlPlaneRoutes(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/enterprises", `{"slug":"enterprise"}`}, {"POST", "/organizations", `{"slug":"org"}`},
		{"GET", "/organizations?limit=1", ""}, {"GET", "/enterprises/parent/organizations?limit=1", ""},
		{"PUT", "/organizations/org/memberships", `{"expected_version":1,"principal":{"ID":"bob","Kind":"user"},"role":"member"}`},
		{"DELETE", "/organizations/org/memberships", `{"expected_version":1}`}, {"PUT", "/enterprises/parent/roles", `{"expected_version":1}`},
		{"PUT", "/organizations/org/enterprise", `{"expected_version":1,"enterprise_id":"parent"}`},
		{"POST", "/organizations/org/join-as-owner", `{"expected_version":1}`},
		{"POST", "/scopes/org.organization/org/suspend", `{"expected_version":1}`}, {"POST", "/scopes/org.enterprise/parent/resume", `{"expected_version":1}`},
		{"POST", "/scopes/org.organization/org/archive", `{"expected_version":1}`}, {"POST", "/scopes/org.organization/org/recover", `{"expected_version":1}`},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			o, m, r := fixture(t)
			w := serve(t, o, route.method, route.path, route.body, "key")
			if w.Code != 200 || m.calls != 1 || r.begins != 1 {
				t.Fatal(w.Code, w.Body.String(), m.calls, r.begins)
			}
			if strings.HasPrefix(route.path, "/enterprises") && route.method == "POST" && m.tenant != "tenant-a" {
				t.Fatal(m.tenant)
			}
		})
	}
}
func TestControlPlaneRejectsBeforeTransaction(t *testing.T) {
	for _, tc := range []struct{ method, path, body, key string }{{"POST", "/organizations", `{"slug":"org","TenantID":"victim"}`, "key"}, {"POST", "/organizations", `{`, "key"}, {"POST", "/organizations", `{"slug":"org"}`, ""}, {"POST", "/organizations", `{}`, "key"}, {"PUT", "/organizations/org/memberships", `{}`, "key"}, {"GET", "/organizations?limit=0", "", ""}, {"GET", "/organizations?limit=101", "", ""}, {"GET", "/organizations?limit=1", "{}", ""}, {"POST", "/organizations", strings.Repeat("x", 513), "key"}} {
		o, m, r := fixture(t)
		w := serve(t, o, tc.method, tc.path, tc.body, tc.key)
		if w.Code < 400 || m.calls != 0 || r.begins != 0 {
			t.Fatal(w.Code, w.Body.String(), m.calls, r.begins)
		}
	}
	o, m, r := fixture(t)
	o.Authorize = func(context.Context, string) error { return scope.ErrDenied }
	w := serve(t, o, "POST", "/organizations", "{", "key")
	if w.Code != 403 || m.calls != 0 || r.begins != 0 {
		t.Fatal(w.Code)
	}
	if _, e := NewHandler(Options{}); e == nil {
		t.Fatal("missing composition")
	}
	o, _, _ = fixture(t)
	w = serve(t, o, "POST", "/scopes/unknown/org/suspend", `{"expected_version":1}`, "key")
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestControlPlaneSafeErrors(t *testing.T) {
	for _, sentinel := range []error{organization.ErrNotFound, organization.ErrConflict, organization.ErrLastOwner, organization.ErrInUse} {
		e := mapped(errors.Join(sentinel, errors.New("secret SQL")))
		var safe interface {
			HTTPStatus() int
			Code() string
		}
		if !errors.As(e, &safe) || safe.HTTPStatus() < 400 || strings.Contains(e.Error(), "secret") || !errors.Is(e, sentinel) {
			t.Fatal(e)
		}
	}
	if mapped(nil) != nil {
		t.Fatal("nil")
	}
	e := errors.New("other")
	if mapped(e) != e {
		t.Fatal("cause")
	}
	o, m, _ := fixture(t)
	m.err = organization.ErrNotFound
	w := serve(t, o, "GET", "/organizations?limit=1", "", "")
	if w.Code != 404 || strings.Contains(w.Body.String(), "SQL") {
		t.Fatal(w.Code, w.Body.String())
	}
}

// orgSessionVerifier is an external custom user Session verifier for the
// forwarding contract: it returns results and never installs identity.
type orgSessionVerifier struct{ calls int }

func (v *orgSessionVerifier) Kind() string { return identity.SessionKind }
func (v *orgSessionVerifier) Verify(_ context.Context, c identity.Credential) (identity.Result, error) {
	v.calls++
	if c.Kind != identity.SessionKind || c.Value != "s-alice" {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{
		Status:    identity.StatusAuthenticated,
		Principal: scope.PrincipalRef{ID: "alice", Kind: identity.PrincipalUser},
		Tenant:    "tenant-a",
		Method:    "session",
	}, nil
}

func serveRaw(t *testing.T, o Options, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	h, e := NewHandler(o)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// sessionOptions composes the control plane with a custom user Session
// credential: exactly the additive forwarding the extension ports provide.
func sessionOptions(t *testing.T) (Options, *management, *resource, *orgSessionVerifier) {
	t.Helper()
	o, m, r := fixture(t)
	verifier := &orgSessionVerifier{}
	pipeline, e := identity.NewPipeline(verifier)
	if e != nil {
		t.Fatal(e)
	}
	o.Authenticate = nil
	o.AuthenticateCredential = pipeline.AuthenticateCredential
	o.Credentials = transport.OneCredential(transport.CookieCredentials("pangolin_session"))
	// Bind against the identity the trusted pipeline installed so the
	// session-verified user actually reaches Governance.
	o.ResolveScope = func(c context.Context, _ *http.Request) (context.Context, error) {
		return scope.NewTenantResolver(identity.Identity).Bind(c, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	}
	return o, m, r, verifier
}

func sessionRequest(method, path, body, cookie string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Idempotency-Key", "key")
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: "pangolin_session", Value: cookie})
	}
	return r
}

func TestCredentialAuthenticationForwardingContract(t *testing.T) {
	t.Run("valid-session-reaches-governance", func(t *testing.T) {
		o, m, r, verifier := sessionOptions(t)
		w := serveRaw(t, o, sessionRequest("GET", "/organizations?limit=1", "", "s-alice"))
		if w.Code != 200 || m.calls != 1 || r.begins != 1 || verifier.calls != 1 {
			t.Fatal(w.Code, w.Body.String(), m.calls, r.begins, verifier.calls)
		}
	})
	t.Run("invalid-session-denied-before-governance", func(t *testing.T) {
		o, m, _, verifier := sessionOptions(t)
		w := serveRaw(t, o, sessionRequest("GET", "/organizations?limit=1", "", "s-unknown"))
		if w.Code != 401 || m.calls != 0 || verifier.calls != 1 {
			t.Fatal(w.Code, w.Body.String(), m.calls, verifier.calls)
		}
	})
	t.Run("unsafe-cookie-refused-before-governance", func(t *testing.T) {
		o, m, r, verifier := sessionOptions(t)
		w := serveRaw(t, o, sessionRequest("POST", "/organizations", `{"slug":"org"}`, "s-alice"))
		if w.Code != 403 || !strings.Contains(w.Body.String(), `"code":"COOKIE_UNSAFE_METHOD"`) {
			t.Fatal(w.Code, w.Body.String())
		}
		if m.calls != 0 || r.begins != 0 || verifier.calls != 0 {
			t.Fatal("unsafe cookie reached governance", m.calls, r.begins, verifier.calls)
		}
	})
	t.Run("missing-credential-unauthenticated", func(t *testing.T) {
		o, m, _, verifier := sessionOptions(t)
		w := serveRaw(t, o, sessionRequest("GET", "/organizations?limit=1", "", ""))
		if w.Code != 401 || m.calls != 0 || verifier.calls != 0 {
			t.Fatal(w.Code, w.Body.String(), m.calls, verifier.calls)
		}
	})
	t.Run("no-fallback-to-unregistered-kind", func(t *testing.T) {
		o, m, _, verifier := sessionOptions(t)
		request := sessionRequest("GET", "/organizations?limit=1", "", "")
		request.Header.Set("Authorization", "Bearer token")
		w := serveRaw(t, o, request)
		if w.Code != 401 || m.calls != 0 || verifier.calls != 0 {
			t.Fatal("bearer fallback attempted for session-only composition", w.Code, m.calls, verifier.calls)
		}
	})
	t.Run("legacy-mode-rejects-session-credential", func(t *testing.T) {
		o, m, _, _ := sessionOptions(t)
		o.AuthenticateCredential = nil
		o.Authenticate = func(context.Context, string) (context.Context, error) {
			t.Fatal("legacy token authenticator must not receive session credentials")
			return nil, nil
		}
		o.Credentials = transport.OneCredential(transport.CookieCredentials("pangolin_session"))
		w := serveRaw(t, o, sessionRequest("GET", "/organizations?limit=1", "", "s-alice"))
		if w.Code != 401 || m.calls != 0 {
			t.Fatal(w.Code, m.calls)
		}
	})
}

func TestHandlerAuthenticatorSelection(t *testing.T) {
	t.Run("both-rejected", func(t *testing.T) {
		o, _, _ := fixture(t)
		o.AuthenticateCredential = func(c context.Context, _ identity.Credential) (context.Context, error) { return c, nil }
		if _, e := NewHandler(o); e == nil {
			t.Fatal("both authenticators accepted")
		}
	})
	t.Run("neither-rejected", func(t *testing.T) {
		o, _, _ := fixture(t)
		o.Authenticate = nil
		if _, e := NewHandler(o); e == nil {
			t.Fatal("missing authenticator accepted")
		}
	})
	t.Run("credential-only-accepted", func(t *testing.T) {
		o, _, _ := fixture(t)
		o.Authenticate = nil
		o.AuthenticateCredential = func(c context.Context, _ identity.Credential) (context.Context, error) { return c, nil }
		if _, e := NewHandler(o); e != nil {
			t.Fatal(e)
		}
	})
}
