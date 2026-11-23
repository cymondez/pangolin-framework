package http

import (
	"context"
	"errors"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pipelineOptions(t *testing.T) PipelineOptions {
	t.Helper()
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a", true
	})
	m, e := uow.NewManager(&pipelineResource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 1})
	if e != nil {
		t.Fatal(e)
	}
	return PipelineOptions{Timeout: time.Second, MaxBodyBytes: 16, Manager: m, Authenticate: func(c context.Context, _ string) (context.Context, error) { return c, nil }, ResolveTenant: func(c context.Context, _ *http.Request) (context.Context, error) { return c, nil }, ResolveScope: func(c context.Context, _ *http.Request) (context.Context, error) {
		return resolver.Bind(c, scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"})
	}, Entry: func(context.Context) error { return nil }, Decode: func([]byte) (any, error) { return "decoded", nil }, Work: func(context.Context, any) (any, error) { return "success", nil }}
}
func TestPipelineFailureAndQueryBoundaries(t *testing.T) {
	for _, kind := range []string{"tenant-denied", "scope-denied", "resource-denied", "query", "query-error", "query-serialization", "panic", "missing-bearer", "body-read-error", "rejected-observer-panic"} {
		t.Run(kind, func(t *testing.T) {
			o := pipelineOptions(t)
			expected := 200
			notifications := 0
			o.Rejected = func(context.Context, error) { notifications++ }
			switch kind {
			case "tenant-denied":
				o.ResolveTenant = func(context.Context, *http.Request) (context.Context, error) { return nil, scope.ErrDenied }
				expected = 403
			case "scope-denied":
				o.ResolveScope = func(context.Context, *http.Request) (context.Context, error) {
					return nil, scope.ErrAuthorityUnavailable
				}
				expected = 503
			case "resource-denied":
				o.ResourcePolicy = func(context.Context, any) error { return scope.ErrDenied }
				expected = 403
			case "query":
				o.ReadOnly = true
			case "query-error":
				o.ReadOnly = true
				o.Work = func(context.Context, any) (any, error) { return nil, scope.ErrDenied }
				expected = 403
			case "query-serialization":
				o.ReadOnly = true
				o.Work = func(context.Context, any) (any, error) { return make(chan int), nil }
				expected = 500
			case "panic":
				o.Work = func(context.Context, any) (any, error) { panic("secret raw panic") }
				expected = 500
			case "missing-bearer":
				expected = 401
			case "body-read-error":
				expected = 400
			case "rejected-observer-panic":
				o.Entry = func(context.Context) error { return scope.ErrDenied }
				o.Rejected = func(context.Context, error) { panic("telemetry") }
				expected = 403
			}
			p, e := NewPipeline(o)
			if e != nil {
				t.Fatal(e)
			}
			r := httptest.NewRequest("POST", "/books", strings.NewReader("{}"))
			if kind != "missing-bearer" {
				r.Header.Set("Authorization", "Bearer valid")
			}
			if kind == "body-read-error" {
				r.Body = errorBody{}
			}
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			if w.Code != expected || strings.Contains(w.Body.String(), "secret") {
				t.Fatal(w.Code, w.Body.String())
			}
			if kind == "resource-denied" && notifications != 1 {
				t.Fatal("business rollback security event missing", notifications)
			}
		})
	}
}

type errorBody struct{}

func (errorBody) Read([]byte) (int, error) { return 0, errors.New("body unavailable") }
func (errorBody) Close() error             { return nil }
