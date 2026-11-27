package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/database"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubPGResource struct{ key string }

func (r stubPGResource) DatabaseKey() string { return r.key }
func (r stubPGResource) Begin(ctx context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	tx := &transaction{key: r.key}
	ctx = executorVar.With(ctx, tx)
	ctx = uow.AttachSlot(ctx, executorVar)
	return ctx, &stubTx{}, nil
}

func TestPostgresCurrentWithScopeAndErrorMapping(t *testing.T) {
	ctx := trustedContext(t)
	if _, _, err := CurrentWithScope(ctx, false); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("outside UOW = %v", err)
	}

	m, err := uow.NewManager(stubPGResource{key: "pg:test"}, uow.DefaultOptions{
		DefaultTimeout:      time.Second,
		CleanupTimeout:      time.Second,
		HookTimeout:         time.Second,
		MaxRequiresNewDepth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	// ReadOnly allows write=false and rejects write=true.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
		ex, ref, err := CurrentWithScope(c, false)
		if err != nil {
			return err
		}
		want := scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"}
		if ex == nil || ref != want {
			t.Fatalf("got ex=%v ref=%+v", ex, ref)
		}
		if _, _, err := CurrentWithScope(c, true); !errors.Is(err, uow.ErrReadOnly) {
			t.Fatalf("expected ErrReadOnly, got %v", err)
		}
		return nil
	}); !errors.Is(err, uow.ErrRollbackOnly) {
		t.Fatalf("expected rollback-only after write denial, got %v", err)
	}

	// ReadWrite allows write=true.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, ref, err := CurrentWithScope(c, true)
		if err != nil {
			return err
		}
		if ex == nil || ref.TenantID != "tenant-a" || ref.Kind != "tenant" || ref.ID != "tenant-a" {
			t.Fatalf("unexpected write executor/scope: %v %+v", ex, ref)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Error mapping tests.
	if MapError(nil) != nil {
		t.Fatal("MapError(nil) must be nil")
	}
	noRows := MapError(fmt.Errorf("query book: %w", pgx.ErrNoRows))
	if !database.IsNotFound(noRows) || !errors.Is(noRows, pgx.ErrNoRows) || !errors.Is(noRows, database.ErrNoRows) {
		t.Fatalf("MapError(pgx.ErrNoRows) = %v", noRows)
	}

	pgDup := &pgconn.PgError{Code: "23505", ConstraintName: "books_pkey"}
	wrappedDup := fmt.Errorf("insert book: %w", pgDup)
	if !IsUniqueViolation(pgDup) || !IsUniqueViolation(wrappedDup) {
		t.Fatal("IsUniqueViolation missed 23505")
	}
	mappedDup := MapError(wrappedDup)
	if !database.IsConflict(mappedDup) || !errors.Is(mappedDup, database.ErrConflict) || !IsUniqueViolation(mappedDup) {
		t.Fatalf("MapError(23505) = %v", mappedDup)
	}
	var extracted *pgconn.PgError
	if !errors.As(mappedDup, &extracted) || extracted.ConstraintName != "books_pkey" {
		t.Fatalf("original PgError cause lost: %v", mappedDup)
	}
	// Idempotent mapping of already-mapped conflict.
	if MapError(mappedDup) != mappedDup {
		t.Fatal("already-mapped conflict should pass through")
	}

	otherPG := &pgconn.PgError{Code: "22P02"}
	if IsUniqueViolation(otherPG) || IsUniqueViolation(errors.New("plain")) {
		t.Fatal("IsUniqueViolation false positive")
	}
	if MapError(otherPG) != otherPG {
		t.Fatal("unrecognized PgError altered")
	}
}
