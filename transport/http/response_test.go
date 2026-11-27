package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type resource struct{ tx *transaction }

func (r resource) Begin(ctx context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return ctx, r.tx, nil
}

type transaction struct {
	committed bool
	commitErr error
	rollbacks int
	cancel    context.CancelFunc
}

func (t *transaction) Commit(context.Context) error {
	if t.commitErr != nil {
		return t.commitErr
	}
	t.committed = true
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}
func (t *transaction) Rollback(context.Context) error { t.rollbacks++; return nil }

type writer struct {
	*httptest.ResponseRecorder
	tx     *transaction
	t      *testing.T
	broken bool
}

func (w writer) WriteHeader(code int) {
	if code >= 200 && code < 300 && !w.tx.committed {
		w.t.Fatal("2xx before commit")
	}
	w.ResponseRecorder.WriteHeader(code)
}
func (w writer) Write(b []byte) (int, error) {
	if w.broken {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(b)
}
func bound(t *testing.T) context.Context {
	t.Helper()
	r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant", true
	})
	ctx, e := r.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant", Kind: "tenant", ID: "tenant"})
	if e != nil {
		t.Fatal(e)
	}
	return ctx
}
func newManager(t *testing.T, tx *transaction) uow.UnitOfWorkManager {
	t.Helper()
	m, e := uow.NewManager(resource{tx}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestA14(t *testing.T) {
	for _, mode := range []string{"success-after-commit", "precommit-error", "explicit-rollback", "unknown-no-2xx", "serialize-after-commit", "response-lost-after-commit", "cancel-after-commit"} {
		t.Run(mode, func(t *testing.T) {
			tx := &transaction{}
			m := newManager(t, tx)
			ctx := bound(t)
			if mode == "cancel-after-commit" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				tx.cancel = cancel
			}
			if mode == "unknown-no-2xx" {
				tx.commitErr = errors.New("secret DSN SQL token")
			}
			h := Command(m, func(c context.Context) (any, error) {
				switch mode {
				case "precommit-error":
					return nil, errors.New("secret DSN SQL token")
				case "explicit-rollback":
					s, _ := m.Current(c)
					_ = s.SetRollbackOnly(nil)
				case "serialize-after-commit":
					return make(chan int), nil
				}
				return map[string]string{"result": "ok"}, nil
			})
			r := httptest.NewRequest(http.MethodPost, "/command", nil).WithContext(ctx)
			rr := httptest.NewRecorder()
			h.ServeHTTP(writer{rr, tx, t, mode == "response-lost-after-commit"}, r)
			want := 200
			if mode == "precommit-error" || mode == "explicit-rollback" || mode == "serialize-after-commit" {
				want = 500
			}
			if mode == "unknown-no-2xx" {
				want = 503
			}
			if rr.Code != want || strings.Contains(rr.Body.String(), "secret") {
				t.Fatal(rr.Code, rr.Body.String())
			}
			if mode == "unknown-no-2xx" && (!strings.Contains(rr.Body.String(), `"outcome":"unknown"`) || strings.Contains(rr.Body.String(), "retryable")) {
				t.Fatal(rr.Body.String())
			}
			if mode == "serialize-after-commit" && !strings.Contains(rr.Body.String(), `"outcome":"committed"`) {
				t.Fatal(rr.Body.String())
			}
			if tx.committed && tx.rollbacks != 0 {
				t.Fatal("rolled back committed transaction")
			}
		})
	}
}
func TestA42HTTPMapping(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/command", nil)
	for _, c := range []struct {
		err    error
		status int
	}{{context.DeadlineExceeded, 504}, {uow.ErrTenantMismatch, 403}, {uow.ErrIdentityMismatch, 403}, {uow.ErrScopeMismatch, 403}, {uow.ErrOptionsInvalid, 500}} {
		w := httptest.NewRecorder()
		WriteError(w, r, c.err)
		if w.Code != c.status {
			t.Fatal(w.Code, c)
		}
	}
	m := newManager(t, &transaction{})
	if StreamOperation(bound(t), nil, nil) == nil {
		t.Fatal("nil stream manager")
	}
	if e := StreamOperation(bound(t), m, func(context.Context) error { return nil }); e != nil {
		t.Fatal(e)
	}
	for _, h := range []http.Handler{Command(nil, func(context.Context) (any, error) { return nil, nil }), Command(m, nil)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 500 {
			t.Fatal(w.Code)
		}
	}
	if e := m.Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(ctx context.Context) error {
		w := httptest.NewRecorder()
		Command(m, func(context.Context) (any, error) { t.Fatal("joined command"); return nil, nil }).ServeHTTP(w, r.WithContext(ctx))
		if w.Code != 500 {
			t.Fatal(w.Code)
		}
		if !errors.Is(StreamOperation(ctx, m, func(context.Context) error { return nil }), uow.ErrBusy) {
			t.Fatal("stream joined root")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
