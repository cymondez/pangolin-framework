// Package postgres supplies guarded pgx executors owned by a UOW Manager.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/pangolin-framework/pangolin/kernel/ambient"
	"github.com/pangolin-framework/pangolin/persistence/database"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var executorVar = ambient.New[*transaction]()

// Resource is a single database resource. Key defaults to the pool's identity.
// Transaction-local RLS requires transactions for reads as well as writes.
type Resource struct {
	Pool *pgxpool.Pool
	Key  string
}

func (r *Resource) DatabaseKey() string {
	if r.Key != "" {
		return r.Key
	}
	return fmt.Sprintf("postgres:%p", r.Pool)
}
func (r *Resource) RequiresTransaction() bool { return true }

type transaction struct {
	tx      pgx.Tx
	conn    *pgxpool.Conn
	mu      sync.Mutex
	release sync.Once
	key     string
}

func (t *transaction) Prepare(context.Context) error {
	if !t.mu.TryLock() {
		return uow.ErrBusy
	}
	t.mu.Unlock()
	return nil
}
func (t *transaction) Commit(ctx context.Context) error {
	if !t.mu.TryLock() {
		return uow.ErrBusy
	}
	defer t.mu.Unlock()
	return t.tx.Commit(ctx)
}
func (t *transaction) CommitFailureOutcome(err error) uow.Outcome {
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrTxCommitRollback) || errors.As(err, &pg) {
		return uow.RolledBack
	}
	return uow.Unknown
}
func (t *transaction) Release(context.Context) error { t.release.Do(t.conn.Release); return nil }
func (t *transaction) Rollback(ctx context.Context) error {
	if !t.mu.TryLock() {
		// A cursor or in-flight query cannot be reused. Hijack removes it from the
		// pool; closing net.Conn is safe while a network operation is blocked.
		t.release.Do(func() { c := t.conn.Hijack(); _ = c.PgConn().Conn().Close() })
		return uow.ErrBusy
	}
	defer t.mu.Unlock()
	err := t.tx.Rollback(ctx)
	if errors.Is(err, pgx.ErrTxClosed) {
		err = nil
	}
	if err != nil {
		t.release.Do(func() { c := t.conn.Hijack(); _ = c.PgConn().Conn().Close() })
	} else {
		_ = t.Release(ctx)
	}
	return err
}
func (r *Resource) Begin(ctx context.Context, o uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	b, ok := scope.Current(ctx)
	if !ok || r.Pool == nil || !o.Transactional || o.DatabaseKey != r.DatabaseKey() {
		return nil, nil, uow.ErrResourceMismatch
	}
	if err := scope.Validate(b.Ref); err != nil {
		return nil, nil, err
	}
	mode := pgx.ReadOnly
	if o.Mode == uow.ReadWrite {
		mode = pgx.ReadWrite
	}
	isolation := pgx.ReadCommitted
	switch o.Isolation {
	case uow.ReadCommitted:
	case uow.RepeatableRead:
		isolation = pgx.RepeatableRead
	case uow.Serializable:
		isolation = pgx.Serializable
	default:
		return nil, nil, uow.ErrOptionsInvalid
	}
	conn, err := r.Pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation, AccessMode: mode})
	if err != nil {
		conn.Release()
		return nil, nil, err
	}
	t := &transaction{tx: tx, conn: conn, key: r.DatabaseKey()}
	// Keep the original tenant-only variable for the shipped M1 schema.
	_, err = tx.Exec(ctx, `SELECT set_config('pangolin.tenant_id',$1,true),set_config('app.tenant_id',$1,true),set_config('app.scope_kind',$2,true),set_config('app.scope_id',$3,true)`, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID)
	if err != nil {
		t.release.Do(func() { c := conn.Hijack(); _ = c.PgConn().Conn().Close() })
		return nil, nil, err
	}
	// Install the driver-neutral Port view next to the guarded pgx
	// executor. Both share the same operation, database identity and
	// transaction; wrappers (events/postgres.Resource) delegating to this
	// Begin expose the neutral Port automatically.
	ctx = database.WithExecutor(ctx, &neutralBinder{key: r.DatabaseKey()})
	ctx = executorVar.With(ctx, t)
	ctx = uow.AttachSlot(ctx, executorVar)
	return ctx, t, nil
}

// Executor never exposes Begin, Commit, Rollback or the underlying connection.
// A write-returning query must obtain Current(ctx,true), including QueryRow.
type Executor struct {
	tx    *transaction
	ctx   context.Context
	write bool
}

func Current(ctx context.Context, write bool) (*Executor, error) {
	if err := uow.Check(ctx, write); err != nil {
		return nil, err
	}
	tx, ok := executorVar.Current(ctx)
	if !ok || tx == nil {
		return nil, uow.Fail(ctx, uow.ErrResourceMismatch)
	}
	if err := uow.CheckResource(ctx, tx.key, write); err != nil {
		return nil, err
	}
	return &Executor{tx, ctx, write}, nil
}
func (e *Executor) enter(ctx context.Context, write bool) error {
	if err := uow.Check(e.ctx, write); err != nil {
		return err
	}
	if ctx != e.ctx {
		return uow.Fail(e.ctx, uow.ErrScopeMismatch)
	}
	if !e.tx.mu.TryLock() {
		return uow.Fail(e.ctx, uow.ErrBusy)
	}
	return nil
}
func (e *Executor) record(err error) error {
	// A lookup miss is a normal domain result, not a failed SQL statement.
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		uow.Fail(e.ctx, err)
	}
	return err
}
func (e *Executor) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := e.enter(ctx, true); err != nil {
		return pgconn.CommandTag{}, err
	}
	defer e.tx.mu.Unlock()
	tag, err := e.tx.tx.Exec(ctx, sql, args...)
	return tag, e.record(err)
}

type row struct {
	pgx.Row
	executor *Executor
	once     sync.Once
	scanned  bool
	mu       sync.Mutex
}

func (r *row) Scan(dest ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scanned {
		return uow.ErrClosed
	}
	r.scanned = true
	defer r.once.Do(r.executor.tx.mu.Unlock)
	if err := uow.Check(r.executor.ctx, r.executor.write); err != nil {
		return err
	}
	return r.executor.record(r.Row.Scan(dest...))
}

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }
func (e *Executor) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := e.enter(ctx, e.write); err != nil {
		return errorRow{err}
	}
	return &row{Row: e.tx.tx.QueryRow(ctx, sql, args...), executor: e}
}

type rows struct {
	pgx.Rows
	executor *Executor
	once     sync.Once
}

// pgx.Rows includes Conn; hide it so a cursor cannot bypass the UOW owner.
func (r *rows) Conn() *pgx.Conn { return nil }

func (r *rows) Close() {
	r.once.Do(func() { r.Rows.Close(); r.executor.record(r.Rows.Err()); r.executor.tx.mu.Unlock() })
}
func (r *rows) Next() bool {
	if err := uow.Check(r.executor.ctx, r.executor.write); err != nil {
		r.Close()
		return false
	}
	ok := r.Rows.Next()
	if !ok {
		r.Close()
	}
	return ok
}
func (r *rows) Scan(dest ...any) error { return r.executor.record(r.Rows.Scan(dest...)) }
func (r *rows) Values() ([]any, error) { v, err := r.Rows.Values(); return v, r.executor.record(err) }
func (e *Executor) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := e.enter(ctx, e.write); err != nil {
		return nil, err
	}
	result, err := e.tx.tx.Query(ctx, sql, args...)
	if err != nil {
		e.tx.mu.Unlock()
		return nil, e.record(err)
	}
	return &rows{Rows: result, executor: e}, nil
}
