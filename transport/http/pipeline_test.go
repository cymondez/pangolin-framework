package http

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/authn"
	"github.com/pangolin-framework/pangolin/security/scope"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestA48Pipeline(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		auth, deny bool
		want       int
		stages     string
	}{
		{"invalid-identity", "{}", false, false, 401, "auth"},
		{"entry-denied", "bad", true, true, 403, "auth,tenant,scope,entry"},
		{"malformed-dto", "bad", true, false, 400, "auth,tenant,scope,entry,decode"},
		{"oversized-body", "12345678901", true, false, 413, "auth,tenant,scope,entry"},
		{"valid-request", "{}", true, false, 200, "auth,tenant,scope,entry,decode,resource,work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stages := []string{}
			identity := func(context.Context) (scope.PrincipalRef, string, bool) {
				return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a", true
			}
			resolver := scope.NewTenantResolver(identity)
			// Existing response test resource provides a real Manager completion boundary.
			m, _ := uow.NewManager(&pipelineResource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
			p, err := NewPipeline(PipelineOptions{Timeout: time.Second, MaxBodyBytes: 10, Manager: m,
				Authenticate: func(c context.Context, _ string) (context.Context, error) {
					stages = append(stages, "auth")
					if !tc.auth {
						return nil, authn.ErrUnauthenticated
					}
					return c, nil
				},
				ResolveTenant: func(c context.Context, _ *http.Request) (context.Context, error) {
					stages = append(stages, "tenant")
					return c, nil
				},
				ResolveScope: func(c context.Context, _ *http.Request) (context.Context, error) {
					stages = append(stages, "scope")
					return resolver.Bind(c, scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"})
				},
				Entry: func(context.Context) error {
					stages = append(stages, "entry")
					if tc.deny {
						return scope.ErrDenied
					}
					return nil
				},
				Decode: func(raw []byte) (any, error) {
					stages = append(stages, "decode")
					if string(raw) != "{}" {
						return nil, ErrValidation
					}
					return struct{}{}, nil
				},
				ResourcePolicy: func(context.Context, any) error { stages = append(stages, "resource"); return nil },
				Work:           func(context.Context, any) (any, error) { stages = append(stages, "work"); return struct{}{}, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/books", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer valid")
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			if w.Code != tc.want || strings.Join(stages, ",") != tc.stages {
				t.Fatal(w.Code, w.Body.String(), stages)
			}
			if tc.want != 200 && !strings.Contains(w.Body.String(), `"traceId":`) {
				t.Fatal("missing problem details")
			}
		})
	}
	if _, err := NewPipeline(PipelineOptions{}); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatal(err)
	}
}

type pipelineResource struct{}

func (*pipelineResource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return c, pipelineTransaction{}, nil
}

type pipelineTransaction struct{}

func (pipelineTransaction) Commit(context.Context) error   { return nil }
func (pipelineTransaction) Rollback(context.Context) error { return nil }
