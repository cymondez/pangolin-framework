package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cymondez/pangolin-framework/examples/bookstore/domain"
	"github.com/cymondez/pangolin-framework/internal/testkit/idp"
	"github.com/cymondez/pangolin-framework/kernel/runtime"
	"github.com/cymondez/pangolin-framework/security/authn"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type trip func(*http.Request) (*http.Response, error)

func (f trip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type booksStub struct {
	err              error
	calls            int
	entered, release chan struct{}
}

func (s *booksStub) Create(context.Context, string, string) (domain.Book, error) {
	s.calls++
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	return domain.Book{ID: "book", Title: "title", Version: 1}, s.err
}
func (s *booksStub) Get(context.Context, string) (domain.Book, error) {
	s.calls++
	return domain.Book{ID: "book", Title: "title", Version: 1}, s.err
}
func fixture(t *testing.T) (*Handler, string, *booksStub) {
	t.Helper()
	provider, e := idp.New("http://idp.example")
	if e != nil {
		t.Fatal(e)
	}
	route := provider.Handler()
	client := &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		route.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	w := httptest.NewRecorder()
	route.ServeHTTP(w, httptest.NewRequest("POST", "http://idp.example/token", strings.NewReader(`{"subject":"alice","tenant_id":"tenant-a"}`)))
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &token); e != nil {
		t.Fatal(e)
	}
	books := &booksStub{}
	return &Handler{Books: books, Auth: authn.NewValidator("http://idp.example", "pangolin-bookstore", "http://idp.example/jwks.json", client), Scopes: scope.NewTenantResolver(authn.Identity), Work: runtime.NewWorkGroup(), Ready: func() bool { return true }}, token.AccessToken, books
}
func TestCommitBeforeSuccessAndRejections(t *testing.T) {
	h, token, books := fixture(t)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		header                   map[string]string
	}{
		{"ready", "GET", "/readyz", "", 204, nil},
		{"missing-auth", "POST", "/books", `{"title":"book"}`, 401, map[string]string{"Authorization": ""}},
		{"invalid-auth", "POST", "/books", `{"title":"book"}`, 401, map[string]string{"Authorization": "Bearer invalid"}},
		{"tenant-forgery", "POST", "/books", `{"title":"book"}`, 403, map[string]string{"X-Tenant-ID": "tenant-b"}},
		{"org-hint", "POST", "/books", `{"title":"book"}`, 403, map[string]string{"X-Scope-Kind": "org.organization"}},
		{"bad-scope-id", "POST", "/books", `{"title":"book"}`, 403, map[string]string{"X-Scope-ID": "tenant-b"}},
		{"bad-json", "POST", "/books", `{`, 400, nil},
		{"unknown-field", "POST", "/books", `{"owner":"other","title":"book"}`, 400, nil},
		{"trailing-json", "POST", "/books", `{"title":"book"}{}`, 400, nil},
		{"unknown-route", "POST", "/missing", `{}`, 404, nil},
		{"create", "POST", "/books", `{"title":"book"}`, 201, nil},
		{"get", "GET", "/books/book", "", 200, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+token)
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.status >= 400 && w.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatal("problem details")
			}
		})
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{domain.ErrInvalidBook, 422}, {domain.ErrConflict, 409}, {domain.ErrNotFound, 404}, {io.ErrUnexpectedEOF, 500}} {
		books.err = tc.err
		req := httptest.NewRequest("POST", "/books", strings.NewReader(`{"title":"book"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "unexpected EOF") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	books.err = nil
	books.entered = make(chan struct{})
	books.release = make(chan struct{})
	req := httptest.NewRequest("POST", "/books", strings.NewReader(`{"title":"book"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := &writeBarrier{writes: make(chan struct{}, 1), header: http.Header{}}
	finished := make(chan struct{})
	go func() { h.ServeHTTP(w, req); close(finished) }()
	<-books.entered
	select {
	case <-w.writes:
		t.Fatal("response before application completion")
	default:
	}
	close(books.release)
	<-finished
	select {
	case <-w.writes:
	default:
		t.Fatal("no success response")
	}
	_ = h.Work.Quiesce(context.Background())
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest("GET", "/books/book", nil))
	if w2.Code != 503 {
		t.Fatal(w2.Code)
	}
	h.Ready = func() bool { return false }
	w2 = httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest("GET", "/readyz", nil))
	if w2.Code != 503 {
		t.Fatal(w2.Code)
	}
}

type writeBarrier struct {
	header http.Header
	writes chan struct{}
}

func (w *writeBarrier) Header() http.Header { return w.header }
func (w *writeBarrier) WriteHeader(int) {
	select {
	case w.writes <- struct{}{}:
	default:
	}
}
func (w *writeBarrier) Write(p []byte) (int, error) { w.WriteHeader(200); return len(p), nil }
