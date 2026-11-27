// Package http implements the M1 tenant-only, non-streaming Bookstore transport.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/pangolin-framework/pangolin/examples/bookstore/domain"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/authn"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	response "github.com/pangolin-framework/pangolin/transport/http"
)

type BookService interface {
	Create(context.Context, string, string) (domain.Book, error)
	Get(context.Context, string) (domain.Book, error)
}
type Authenticator interface {
	Authenticate(context.Context, string) (context.Context, error)
}

// CredentialAuthenticator verifies an extracted credential. The trusted
// identity pipeline is the intended implementation.
type CredentialAuthenticator interface {
	AuthenticateCredential(context.Context, identity.Credential) (context.Context, error)
}
type Handler struct {
	Books BookService
	// Auth is the legacy Bearer-only entry point.
	Auth Authenticator
	// CredentialAuth verifies credentials extracted by Credentials. When it
	// is set, Credentials supplies pluggable request authentication; nil
	// defaults to exactly one Bearer credential.
	CredentialAuth CredentialAuthenticator
	Credentials    response.CredentialExtractor
	Scopes         scope.ScopeResolver
	Work           *runtime.WorkGroup
	Ready          func() bool
}

func problem(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": http.StatusText(status), "instance": r.URL.Path, "traceId": r.Header.Get("X-Request-ID"), "code": code})
}

// credentialProblem maps authentication failures to safe problem details.
// Failures are distinguishable but never expose credential or SQL content.
func credentialProblem(err error) (int, string) {
	switch {
	case errors.Is(err, response.ErrAmbiguousCredential):
		return 400, "AMBIGUOUS_CREDENTIAL"
	case errors.Is(err, response.ErrCookieUnsafeMethod):
		return 403, "COOKIE_UNSAFE_METHOD"
	case errors.Is(err, identity.ErrChallengeRequired):
		return 401, "AUTHENTICATION_CHALLENGE_REQUIRED"
	case errors.Is(err, identity.ErrPending):
		return 401, "AUTHENTICATION_PENDING"
	default:
		return 401, "UNAUTHENTICATED"
	}
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/readyz" {
		if h.Ready() {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		return
	}
	if !h.Ready() {
		problem(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	ctx, done, e := h.Work.Enter(r.Context())
	if e != nil {
		problem(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 5e9)
	defer cancel()
	switch {
	case h.CredentialAuth != nil && h.Auth != nil:
		// Fail closed on ambiguous wiring instead of guessing a path.
		problem(w, r, 500, "AUTH_MISCONFIGURED")
		return
	case h.CredentialAuth != nil:
		// Pluggable credential entry: extraction decides which credential is
		// present; the trusted pipeline installs identity. There is no
		// fallback between schemes.
		extract := h.Credentials
		if extract == nil {
			extract = response.OneCredential(response.BearerCredentials())
		}
		credential, present, extractErr := extract(r)
		if extractErr != nil {
			status, code := credentialProblem(extractErr)
			problem(w, r, status, code)
			return
		}
		if !present || credential.Value == "" {
			problem(w, r, 401, "UNAUTHENTICATED")
			return
		}
		var authErr error
		ctx, authErr = h.CredentialAuth.AuthenticateCredential(ctx, credential)
		if authErr != nil {
			status, code := credentialProblem(authErr)
			problem(w, r, status, code)
			return
		}
	case h.Auth != nil:
		// Legacy Bearer entry reuses the common extractor so malformed,
		// duplicate and non-Bearer Authorization values fail closed exactly
		// like the pluggable path.
		credential, present, extractErr := response.BearerCredentials()(r)
		if extractErr != nil || !present || credential.Value == "" {
			problem(w, r, 401, "UNAUTHENTICATED")
			return
		}
		var authErr error
		ctx, authErr = h.Auth.Authenticate(ctx, credential.Value)
		if authErr != nil {
			problem(w, r, 401, "UNAUTHENTICATED")
			return
		}
	default:
		problem(w, r, 401, "UNAUTHENTICATED")
		return
	}
	_, tenant, ok := authn.Identity(ctx)
	if !ok {
		problem(w, r, 401, "UNAUTHENTICATED")
		return
	}
	hint := r.Header.Get("X-Tenant-ID")
	if hint == "" {
		hint = tenant
	}
	if r.Header.Get("X-Scope-Kind") != "" && r.Header.Get("X-Scope-Kind") != "tenant" {
		problem(w, r, 400, "SCOPE_KIND_UNSUPPORTED")
		return
	}
	if r.Header.Get("X-Scope-ID") != "" && r.Header.Get("X-Scope-ID") != tenant {
		problem(w, r, 403, "PERMISSION_DENIED")
		return
	}
	ctx, e = h.Scopes.Bind(ctx, scope.ScopeRef{TenantID: hint, Kind: "tenant", ID: hint})
	if e != nil {
		if errors.Is(e, scope.ErrAuthorityUnavailable) {
			problem(w, r, 503, "SCOPE_AUTHORITY_UNAVAILABLE")
			return
		}
		problem(w, r, 403, "PERMISSION_DENIED")
		return
	}
	var book domain.Book
	// This transport owns the outer operation; a joined child must not send 2xx.
	if e := uow.Check(ctx, false); !errors.Is(e, uow.ErrRequired) {
		response.WriteError(w, r, uow.ErrBusy)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/books":
		var request struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if e = decoder.Decode(&request); e != nil {
			problem(w, r, 400, "MALFORMED_REQUEST")
			return
		}
		if e = decoder.Decode(new(any)); e != io.EOF {
			problem(w, r, 400, "MALFORMED_REQUEST")
			return
		}
		book, e = h.Books.Create(ctx, request.ID, request.Title)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/books/"):
		book, e = h.Books.Get(ctx, strings.TrimPrefix(r.URL.Path, "/books/"))
	default:
		problem(w, r, 404, "NOT_FOUND")
		return
	}
	if e != nil {
		status, code := 500, "INTERNAL"
		if errors.Is(e, domain.ErrNotFound) {
			status, code = 404, "NOT_FOUND"
		}
		if errors.Is(e, domain.ErrConflict) {
			status, code = 409, "CONFLICT"
		}
		if errors.Is(e, domain.ErrInvalidBook) {
			status, code = 422, "VALIDATION_FAILED"
		}
		// Application-level authorization failures keep their transport
		// semantics: a policy denial is 403 and an authority outage is 503.
		if errors.Is(e, scope.ErrDenied) {
			status, code = 403, "PERMISSION_DENIED"
		}
		if errors.Is(e, scope.ErrAuthorityUnavailable) {
			status, code = 503, "SCOPE_AUTHORITY_UNAVAILABLE"
		}
		var failure uow.Error
		if errors.As(e, &failure) && status == 500 {
			response.WriteError(w, r, e)
			return
		}
		problem(w, r, status, code)
		return
	}
	// Create returns only after a confirmed root Commit. Serialize afterwards.
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(book)
}
