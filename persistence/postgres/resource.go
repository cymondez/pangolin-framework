// Package postgres adapts the M1 UOW transaction resource to pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type executorKey struct{}
type Resource struct{ Pool *pgxpool.Pool }
type transaction struct {
	pgx.Tx
	mu sync.Mutex
}

func (t *transaction) Commit(ctx context.Context) error {
	if !t.mu.TryLock() {
		return fmt.Errorf("UOW_CONCURRENT_USE: unconsumed row")
	}
	defer t.mu.Unlock()
	return t.Tx.Commit(ctx)
}

func (t *transaction) Rollback(ctx context.Context) error {
	e := t.Tx.Rollback(ctx)
	if errors.Is(e, pgx.ErrTxClosed) {
		return nil
	}
	return e
}
func (r *Resource) Begin(ctx context.Context, options uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	b, ok := scope.Current(ctx)
	if !ok || b.Ref.Kind != "tenant" {
		return nil, nil, fmt.Errorf("trusted tenant required")
	}
	mode := pgx.ReadOnly
	if options.Mode == uow.ReadWrite {
		mode = pgx.ReadWrite
	}
	tx, e := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: mode})
	if e != nil {
		return nil, nil, e
	}
	if _, e = tx.Exec(ctx, "select set_config('pangolin.tenant_id',$1,true)", b.Ref.TenantID); e != nil {
		return nil, nil, errors.Join(e, tx.Rollback(context.WithoutCancel(ctx)))
	}
	t := &transaction{Tx: tx}
	return context.WithValue(ctx, executorKey{}, t), t, nil
}

// Executor exposes only bounded statement operations, never Begin/Commit/Pool.
// QueryOne keeps the resource occupied until Scan, including deferred row errors.
type Executor struct {
	tx    *transaction
	ctx   context.Context
	write bool
}

func Current(ctx context.Context, write bool) (*Executor, error) {
	if e := uow.Check(ctx, write); e != nil {
		return nil, e
	}
	tx, ok := ctx.Value(executorKey{}).(*transaction)
	if !ok {
		return nil, fmt.Errorf("UOW executor missing")
	}
	return &Executor{tx, ctx, write}, nil
}
func (e *Executor) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := uow.Check(e.ctx, true); err != nil {
		return pgconn.CommandTag{}, err
	}
	if ctx != e.ctx {
		return pgconn.CommandTag{}, fmt.Errorf("executor context mismatch")
	}
	if !e.tx.mu.TryLock() {
		return pgconn.CommandTag{}, fmt.Errorf("UOW_CONCURRENT_USE")
	}
	defer e.tx.mu.Unlock()
	return e.tx.Tx.Exec(ctx, sql, args...)
}

type row struct {
	pgx.Row
	unlock func()
}

func (r *row) Scan(dest ...any) error { defer r.unlock(); return r.Row.Scan(dest...) }

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }
func (e *Executor) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := uow.Check(e.ctx, false); err != nil {
		return errorRow{err}
	}
	if ctx != e.ctx {
		return errorRow{fmt.Errorf("executor context mismatch")}
	}
	if !e.tx.mu.TryLock() {
		return errorRow{fmt.Errorf("UOW_CONCURRENT_USE")}
	}
	return &row{e.tx.Tx.QueryRow(ctx, sql, args...), e.tx.mu.Unlock}
}

// Query exists for sqlc's DBTX contract; M1 uses only QueryRow and Exec.
func (e *Executor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("M1 multi-row queries unsupported")
}
