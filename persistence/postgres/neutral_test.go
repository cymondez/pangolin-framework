package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/database"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
)

// stubResource lets the unit tests drive a real Manager lifecycle without a
// database; the delegation success paths are exercised against real
// PostgreSQL by the integration contract tests.
type stubResource struct{}

func (stubResource) Begin(ctx context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return ctx, &stubTx{}, nil
}

type stubTx struct{}

func (*stubTx) Commit(context.Context) error   { return nil }
func (*stubTx) Rollback(context.Context) error { return nil }

func trustedContext(t *testing.T) context.Context {
	t.Helper()
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	ctx, err := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func stubManager(t *testing.T) uow.UnitOfWorkManager {
	t.Helper()
	m, err := uow.NewManager(stubResource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNeutralResourceCapabilitiesAndDialect(t *testing.T) {
	r := &Resource{Key: "contract"}
	caps := r.Capabilities()
	if caps.Backend != "postgres" || !caps.RowLevelSecurity || !caps.TransactionReadOnly {
		t.Fatalf("capabilities untruthful: %+v", caps)
	}
	for _, level := range []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Serializable} {
		if !caps.Supports(level) {
			t.Fatalf("supported level missing: %d", level)
		}
	}
	// Snapshots are detached: mutating the returned slice must not rewrite
	// the resource's admission contract.
	caps.Isolations[0] = uow.Isolation(99)
	if r.Capabilities().Supports(uow.ReadCommitted) != true {
		t.Fatal("capability snapshot is not detached")
	}
	if r.Dialect().Placeholder(1) != "$1" {
		t.Fatal("dialect wrong")
	}
	// A caller requiring RLS passes admission on this truthful contract.
	if err := database.Admit(r.Capabilities(), database.Requirements{RowLevelSecurity: true}); err != nil {
		t.Fatal(err)
	}
}

func TestNeutralViewLifetimeAndIdentityGuards(t *testing.T) {
	m := stubManager(t)
	ctx := trustedContext(t)
	binder := &neutralBinder{key: "postgres:contract"}

	// An executor bound outside a unit of work refuses every statement.
	outside, err := binder.Bind(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := outside.Exec(ctx, "SELECT 1"); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("unguarded exec accepted: %v", err)
	}
	if _, err := outside.Query(ctx, "SELECT 1"); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("unguarded query accepted: %v", err)
	}
	if err := outside.QueryRow(ctx, "SELECT 1").Scan(new(int)); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("unguarded row accepted: %v", err)
	}

	var captured database.Executor
	var nested context.Context
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		view, berr := binder.Bind(c)
		if berr != nil {
			return berr
		}
		captured = view
		// A nested Required context is a different context: the view
		// bound to the root must refuse to operate under it even though
		// that context is itself active. The refusal poisons the root,
		// which is the designed consequence of a guard violation.
		return m.Within(c, uow.Options{Mode: uow.ReadOnly}, func(child context.Context) error {
			nested = child
			if _, e := view.Exec(child, "SELECT 1"); !errors.Is(e, uow.ErrScopeMismatch) {
				return errors.New("view accepted a foreign active context")
			}
			if e := view.QueryRow(child, "SELECT 1").Scan(new(int)); !errors.Is(e, uow.ErrScopeMismatch) {
				return errors.New("row view accepted a foreign active context")
			}
			return nil
		})
	})
	// The identity violation marked the root rollback-only by design.
	if !errors.Is(err, uow.ErrRollbackOnly) {
		t.Fatalf("guard violation did not poison the root: %v", err)
	}
	_ = nested
	// The view's own lifetime ended with the root: it fails closed forever.
	if _, err := captured.Exec(ctx, "SELECT 1"); !errors.Is(err, uow.ErrClosed) {
		t.Fatalf("ended view usable: %v", err)
	}
	if _, err := captured.Query(ctx, "SELECT 1"); !errors.Is(err, uow.ErrClosed) {
		t.Fatalf("ended view readable: %v", err)
	}
	if err := captured.QueryRow(ctx, "SELECT 1").Scan(new(int)); !errors.Is(err, uow.ErrClosed) {
		t.Fatalf("ended row view scannable: %v", err)
	}
}

func TestNeutralViewDelegationRequiresPgxExecutor(t *testing.T) {
	m := stubManager(t)
	ctx := trustedContext(t)
	binder := &neutralBinder{key: "postgres:contract"}
	// A guarded view inside a real unit of work still cannot reach a pgx
	// executor this stub resource never installed: delegation must fail
	// closed as a resource mismatch, never fall back to a pool write. The
	// failure also poisons the root by design.
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		view, berr := binder.Bind(c)
		if berr != nil {
			return berr
		}
		if _, e := view.Exec(c, "SELECT 1"); !errors.Is(e, uow.ErrResourceMismatch) {
			return errors.New("delegate wrote without a guarded pgx executor")
		}
		if _, e := view.Query(c, "SELECT 1"); !errors.Is(e, uow.ErrResourceMismatch) {
			return errors.New("delegate read without a guarded pgx executor")
		}
		if e := view.QueryRow(c, "SELECT 1").Scan(new(int)); !errors.Is(e, uow.ErrResourceMismatch) {
			return errors.New("delegate row scanned without a guarded pgx executor")
		}
		return nil
	})
	if !errors.Is(err, uow.ErrRollbackOnly) {
		t.Fatalf("delegation without a guarded executor did not poison the root: %v", err)
	}
}

func TestFailedNeutralRowScan(t *testing.T) {
	r := failedNeutralRow{errors.New("refused")}
	if err := r.Scan(); err == nil {
		t.Fatal("failed row scanned")
	}
}

func TestNeutralInterfacesSatisfied(t *testing.T) {
	var _ database.Resource = (*Resource)(nil)
	var _ database.Binder = (*neutralBinder)(nil)
	var _ database.Executor = (*neutralView)(nil)
	if (&neutralBinder{key: "k"}).DatabaseKey() != "k" {
		t.Fatal("binder key wrong")
	}
	var v *neutralView
	v = &neutralView{key: "k"}
	if v.DatabaseKey() != "k" {
		t.Fatal("view key wrong")
	}
}
