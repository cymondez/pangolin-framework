// Package http implements the M1 tenant-only, non-streaming Bookstore transport.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cymondez/pangolin-framework/examples/bookstore/domain"
	"github.com/cymondez/pangolin-framework/kernel/runtime"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/authn"
	"github.com/cymondez/pangolin-framework/security/scope"
	response "github.com/cymondez/pangolin-framework/transport/http"
)

type BookService interface {
	Create(context.Context, string, string) (domain.Book, error)
	Get(context.Context, string) (domain.Book, error)
}
type Authenticator interface {
	Authenticate(context.Context, string) (context.Context, error)
}
type Handler struct {
	Books  BookService
	Auth   Authenticator
	Scopes scope.ScopeResolver
	Work   *runtime.WorkGroup
	Ready  func() bool
}

func problem(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": http.StatusText(status), "instance": r.URL.Path, "traceId": r.Header.Get("X-Request-ID"), "code": code})
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
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || scheme != "Bearer" {
		problem(w, r, 401, "UNAUTHENTICATED")
		return
	}
	ctx, e = h.Auth.Authenticate(ctx, token)
	if e != nil {
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
