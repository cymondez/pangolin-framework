package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type errTestBinder struct{ key string }

func (b errTestBinder) DatabaseKey() string                    { return b.key }
func (b errTestBinder) Bind(context.Context) (Executor, error) { return errTestExecutor{b.key}, nil }

type errTestExecutor struct{ key string }

func (e errTestExecutor) DatabaseKey() string { return e.key }
func (errTestExecutor) Exec(context.Context, string, ...any) (ExecResult, error) {
	return ExecResult{}, nil
}
func (errTestExecutor) QueryRowWrite(context.Context, string, ...any) Row { return nil }
func (errTestExecutor) QueryWrite(context.Context, string, ...any) (Rows, error) {
	return nil, nil
}
func (errTestExecutor) Query(context.Context, string, ...any) (Rows, error) { return nil, nil }
func (errTestExecutor) QueryRow(context.Context, string, ...any) Row        { return nil }

type errTestResource struct{ key string }

func (r errTestResource) DatabaseKey() string { return r.key }
func (r errTestResource) Begin(ctx context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return WithExecutor(ctx, errTestBinder{r.key}), errTestTx{}, nil
}

type errTestTx struct{}

func (errTestTx) Commit(context.Context) error   { return nil }
func (errTestTx) Rollback(context.Context) error { return nil }

func TestErrorHelpersAndCurrentWithScope(t *testing.T) {
	if !IsNotFound(ErrNoRows) || !IsNotFound(sql.ErrNoRows) || !IsNotFound(errors.Join(ErrNoRows, errors.New("miss"))) {
		t.Fatal("IsNotFound did not recognize ErrNoRows")
	}
	if IsNotFound(nil) || IsNotFound(errors.New("other")) {
		t.Fatal("IsNotFound false positive")
	}
	if !IsConflict(ErrConflict) || !IsConflict(errors.Join(ErrConflict, errors.New("dup"))) {
		t.Fatal("IsConflict did not recognize ErrConflict")
	}
	if IsConflict(nil) || IsConflict(ErrNoRows) {
		t.Fatal("IsConflict false positive")
	}

	// Outside UOW fails closed.
	if _, _, err := CurrentWithScope(context.Background()); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("CurrentWithScope outside UOW = %v", err)
	}

	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	ctx, err := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := uow.NewManager(errTestResource{key: "db:main"}, uow.DefaultOptions{
		DefaultTimeout:      time.Second,
		CleanupTimeout:      time.Second,
		HookTimeout:         time.Second,
		MaxRequiresNewDepth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
		ex, ref, err := CurrentWithScope(c)
		if err != nil {
			return err
		}
		if ex == nil || ex.DatabaseKey() != "db:main" {
			t.Fatalf("unexpected executor: %v", ex)
		}
		want := scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"}
		if ref != want {
			t.Fatalf("scope = %+v, want %+v", ref, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
