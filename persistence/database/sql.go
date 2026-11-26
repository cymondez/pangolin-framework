package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// SQLConfig declares the backend-specific behavior of SQLResource. Fields
// describe what the backend truthfully supports; unsupported options fail
// closed instead of being downgraded.
type SQLConfig struct {
	// Backend names the adapter for capability reports.
	Backend string
	// Key overrides the resource identity; empty derives it from the pool.
	Key string
	// Capabilities is the truthful admission contract of the backend.
	Capabilities Capabilities
	// Dialect renders placeholders and identifiers.
	Dialect Dialect
	// Isolation maps a requested level to the driver level, rejecting the
	// ones the backend cannot enforce. It must not silently downgrade.
	Isolation func(uow.Isolation) (sql.IsolationLevel, error)
	// ReadOnly reports whether the engine enforces read-only transactions
	// for the requested access mode. Backends without such enforcement
	// report false and never request it from the driver.
	ReadOnly func(uow.AccessMode) bool
	// CommitOutcome classifies a failed COMMIT. Only errors proving the
	// transaction was not committed may map to uow.RolledBack; everything
	// else stays uow.Unknown.
	CommitOutcome func(error) uow.Outcome
}

// SQLResource is a database/sql-backed Resource for real database engines.
// Each transaction owns a dedicated connection, so a failed or abandoned
// completion never poisons the pool with a connection still inside a
// transaction.
type SQLResource struct {
	pool          *sql.DB
	key           string
	capabilities  Capabilities
	dialect       Dialect
	isolation     func(uow.Isolation) (sql.IsolationLevel, error)
	readOnly      func(uow.AccessMode) bool
	commitOutcome func(error) uow.Outcome
}

// NewSQLResource validates the configuration before any operation starts.
func NewSQLResource(pool *sql.DB, c SQLConfig) (*SQLResource, error) {
	if pool == nil {
		return nil, uow.ErrOptionsInvalid
	}
	if c.Backend == "" || c.Isolation == nil || c.Dialect.Placeholder == nil || c.Dialect.Identifier == nil || c.Capabilities.Backend != c.Backend {
		return nil, uow.ErrOptionsInvalid
	}
	readOnly := c.ReadOnly
	if readOnly == nil {
		readOnly = func(uow.AccessMode) bool { return false }
	}
	key := c.Key
	if key == "" {
		key = fmt.Sprintf("%s:%p", c.Backend, pool)
	}
	// Detach the capability slice: a caller mutating its configuration
	// after construction must never rewrite the resource's admission
	// contract.
	caps := c.Capabilities
	caps.Isolations = append([]uow.Isolation(nil), c.Capabilities.Isolations...)
	return &SQLResource{
		pool:          pool,
		key:           key,
		capabilities:  caps,
		dialect:       c.Dialect,
		isolation:     c.Isolation,
		readOnly:      readOnly,
		commitOutcome: c.CommitOutcome,
	}, nil
}

func (r *SQLResource) DatabaseKey() string       { return r.key }
func (r *SQLResource) RequiresTransaction() bool { return false }

// Capabilities returns a detached snapshot: callers can hold it without
// affecting the resource's admission contract.
func (r *SQLResource) Capabilities() Capabilities {
	caps := r.capabilities
	caps.Isolations = append([]uow.Isolation(nil), r.capabilities.Isolations...)
	return caps
}
func (r *SQLResource) Dialect() Dialect { return r.dialect }

// Pool exposes the underlying *sql.DB for infrastructure operations that
// the neutral Port does not model (schema setup by a migration tool, health
// checks). Business code uses the guarded executor.
func (r *SQLResource) Pool() *sql.DB { return r.pool }

func (r *SQLResource) beginTransaction(ctx context.Context, o uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	level, err := r.isolation(o.Isolation)
	if err != nil {
		return nil, nil, err
	}
	opts := &sql.TxOptions{Isolation: level, ReadOnly: r.readOnly(o.Mode)}
	// The session owns a cancellable child context for the SQL
	// transaction: finish ends it deterministically, and the operation
	// context's cancellation still rolls the transaction back through
	// database/sql's own watcher.
	txCtx, cancelTx := context.WithCancel(ctx)
	conn, err := r.pool.Conn(txCtx)
	if err != nil {
		cancelTx()
		return nil, nil, err
	}
	tx, err := conn.BeginTx(txCtx, opts)
	if err != nil {
		cancelTx()
		_ = conn.Close()
		return nil, nil, err
	}
	s := &session{resource: r, tx: tx, conn: conn, cancelTx: cancelTx}
	return WithExecutor(ctx, s), s, nil
}

// Begin installs the session binder. Transactional operations own a
// dedicated connection; non-transactional reads run pooled statements and
// can never write.
func (r *SQLResource) Begin(ctx context.Context, o uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	b, ok := scope.Current(ctx)
	if !ok || o.DatabaseKey != r.DatabaseKey() {
		return nil, nil, uow.ErrResourceMismatch
	}
	if err := scope.Validate(b.Ref); err != nil {
		return nil, nil, err
	}
	if !o.Transactional {
		s := &session{resource: r, pooled: true}
		return WithExecutor(ctx, s), s, nil
	}
	return r.beginTransaction(ctx, o)
}

// session is one unit of work's exclusive state; it carries no operation
// context. The mutex enforces one statement at a time; an unclosed cursor
// keeps it locked and the completion fails busy instead of committing under
// an active read. bind returns a fresh view per Current call, so a
// Required child context shares the session while its view stays bound to
// the child, exactly like postgres.Current.
type session struct {
	resource *SQLResource
	mu       sync.Mutex
	pooled   bool
	tx       *sql.Tx
	// conn is immutable after beginTransaction; close/discard of the
	// acquired connection happens exactly once under finishOnce.
	conn       *sql.Conn
	cancelTx   context.CancelFunc
	finishOnce sync.Once

	// busyMu guards the busy-owner bookkeeping and the closing flag. The
	// owner's close is invoked without busyMu held, so an arbitrary
	// cleanup path can never deadlock against the statement lock itself.
	busyMu sync.Mutex
	// busy is the registered owner of the statement currently holding
	// mu: either an owned cancel func or a Rows/Row closer. It is nil
	// while the session is idle or already cleaned up.
	busy *busyOwner
	// closing marks a session whose cleanup already ran: a statement
	// registering after closing ends itself immediately, so no late
	// registration can leave a query live on a doomed connection.
	closing bool

	// cleanupDone is created exactly once per session and never replaced:
	// the first busy completion closes it when the single job finishes,
	// and every caller, including the creator, waits on it bounded by
	// the cleanup context.
	cleanupOnce sync.Once
	cleanupDone chan struct{}
}

func (s *session) DatabaseKey() string { return s.resource.key }

// Bind returns a fresh executor view bound to ctx. The session is looked up
// from ctx itself, so a view is never rebound to another session and an
// ended view fails closed on its first use.
func (s *session) Bind(ctx context.Context) (Executor, error) {
	return &view{session: s, work: ctx}, nil
}

// finish ends the dedicated connection exactly once: the transaction is
// ended synchronously FIRST so database/sql's context watcher can no longer
// roll it back and close/discard the *sql.Conn wrapper behind our back, then
// the transaction context is canceled, and only after that the connection is
// discarded (never returned for reuse) or released. An already-ended
// transaction reports ErrTxDone and leaves the connection untouched, which
// also makes this path cheap after a normal Commit or Rollback. The
// watcher-driven close races the wrapper's own closemu handshake (Raw can
// otherwise observe done=false and dc=nil between the load and the read
// lock), so ending the transaction inside this Once is what makes the
// teardown single-owner. finish is idempotent and safe to call from any
// goroutine.
func (s *session) finish(discard bool) {
	s.finishOnce.Do(func() {
		if s.tx != nil {
			_ = s.tx.Rollback()
		}
		if s.cancelTx != nil {
			s.cancelTx()
		}
		if s.conn == nil {
			return
		}
		if discard {
			// database/sql returns pooled connections without clearing a
			// failed transaction, so reuse would corrupt the next
			// operation. Raw with ErrBadConn makes the pool drop it.
			_ = s.conn.Raw(func(any) error { return driver.ErrBadConn })
		} else {
			_ = s.conn.Close()
		}
	})
}

// busyOwner is the cleanup token of one statement for its whole lifecycle.
// cancel aborts the in-flight driver call; upgrade swaps in the cursor or
// row closer once the statement produced one. Both phases are guarded so a
// cleanup racing the hand-off still ends the statement exactly once.
type busyOwner struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	closer func()
	taken  bool
}

func (o *busyOwner) close() {
	o.mu.Lock()
	o.taken = true
	cancel, closer := o.cancel, o.closer
	o.cancel, o.closer = nil, nil
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if closer != nil {
		closer()
	}
}

// upgrade installs the cursor or row closer of a completed statement. It
// reports whether the owner is still registered: when a cleanup already
// consumed it, the caller must release the statement itself.
func (o *busyOwner) upgrade(closer func()) bool {
	o.mu.Lock()
	if o.taken {
		o.mu.Unlock()
		closer()
		return false
	}
	o.closer = closer
	o.mu.Unlock()
	return true
}

// end deregisters the cancel-only owner of a finished statement. When a
// cleanup already consumed it, the cancel it invoked is exactly the abort
// the statement observed, so nothing further is needed.
func (o *busyOwner) end() {
	o.mu.Lock()
	o.taken = true
	cancel := o.cancel
	o.cancel = nil
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// scheduleCleanup runs the single session-owned cleanup job exactly once
// per session and returns its persistent done channel. The job marks the
// session closing, closes the statement owner found at that point (its
// cancel unblocks the in-flight driver call and its closer releases the
// transaction's read lock), then discards the dedicated connection via
// finish: the busy path cannot prove the transaction ended cleanly, so the
// connection is never returned for reuse. The synchronous transaction end
// inside finish must see no reader anymore, which is why the owner closes
// before finish, and why the transaction context is canceled by finish
// itself (after the Transaction's CAS) instead of here: canceling early
// would let database/sql's context watcher close the *sql.Conn wrapper
// concurrently with the teardown. A statement registering after closing was
// marked ends itself immediately.
func (s *session) scheduleCleanup() <-chan struct{} {
	s.cleanupOnce.Do(func() {
		s.cleanupDone = make(chan struct{})
		go func() {
			s.busyMu.Lock()
			s.closing = true
			s.busyMu.Unlock()
			if owner := s.takeBusy(); owner != nil {
				owner.close()
			}
			s.finish(true)
			close(s.cleanupDone)
		}()
	})
	return s.cleanupDone
}

// waitCleanup bounds every cleanup caller, including the creator, by ctx:
// the job always runs to completion in the background, but a caller whose
// cleanup context expires proceeds without waiting further.
func waitCleanup(ctx context.Context, done <-chan struct{}) {
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// setBusy registers the owner of the statement that currently holds mu. If
// the session's cleanup already ran, the late statement ends itself and
// registration reports doomed, closing the gap between enter acquiring mu
// and this registration: no late query can stay live on a finished
// connection. The statement still runs its driver call and observes the
// cancellation through the same owner token; a doomed Exec reports the
// context error, and the Query paths upgrade into a dead cursor/row.
func (s *session) setBusy(o *busyOwner) (doomed bool) {
	s.busyMu.Lock()
	if s.closing {
		s.busyMu.Unlock()
		o.close()
		return true
	}
	s.busy = o
	s.busyMu.Unlock()
	return false
}

// takeBusy consumes the registered busy owner without invoking it under
// busyMu.
func (s *session) takeBusy() *busyOwner {
	s.busyMu.Lock()
	o := s.busy
	s.busy = nil
	s.busyMu.Unlock()
	return o
}

// clearBusy unregisters a busy owner when it is still the registered one;
// after a cleanup took ownership the closer stays consumed so the job is
// never run twice.
func (s *session) clearBusy(o *busyOwner) {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	if s.busy == o {
		s.busy = nil
	}
}

// Prepare refuses to complete an operation that still holds a cursor.
func (s *session) Prepare(context.Context) error {
	if !s.mu.TryLock() {
		return uow.ErrBusy
	}
	s.mu.Unlock()
	return nil
}

func (s *session) Commit(ctx context.Context) error {
	if s.pooled || s.tx == nil {
		return uow.ErrTransactionRequired
	}
	if !s.mu.TryLock() {
		// A cursor or in-flight statement still owns the connection;
		// completing under it is refused. The single session-owned
		// cleanup bounds the wait, so an abandoned statement cannot
		// reserve the pooled connection forever.
		waitCleanup(ctx, s.scheduleCleanup())
		return uow.ErrBusy
	}
	defer s.mu.Unlock()
	err := s.tx.Commit()
	if err != nil {
		s.finish(true)
		return err
	}
	s.finish(false)
	return nil
}

// CommitFailureOutcome classifies COMMIT failures conservatively. Only
// sql.ErrTxDone proves the transaction was already ended without this
// COMMIT; a context error after a commit attempt cannot prove anything,
// because the server may have committed before the acknowledgment was
// lost. Without proof the outcome stays Unknown.
func (s *session) CommitFailureOutcome(err error) uow.Outcome {
	if errors.Is(err, sql.ErrTxDone) {
		return uow.RolledBack
	}
	if s.resource.commitOutcome != nil {
		return s.resource.commitOutcome(err)
	}
	return uow.Unknown
}

func (s *session) Rollback(ctx context.Context) error {
	if s.pooled || s.tx == nil {
		return uow.ErrTransactionRequired
	}
	if !s.mu.TryLock() {
		// A cursor or in-flight statement still owns the connection. The
		// single session-owned cleanup closes it and discards the
		// connection; the caller's cursor or row is dead afterwards,
		// matching the hijacked connection of the PostgreSQL adapter.
		waitCleanup(ctx, s.scheduleCleanup())
		// The bounded wait above may have released the abandoned
		// statement already; retry once so a normal cleanup does not
		// surface busy. A statement still holding mu stays strictly
		// rejected.
		if !s.mu.TryLock() {
			return uow.ErrBusy
		}
	}
	defer s.mu.Unlock()
	err := s.tx.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		err = nil
	}
	if err != nil {
		s.finish(true)
		return err
	}
	s.finish(false)
	return nil
}

// Release is a no-op: transactional connections are finished by Commit and
// Rollback, and the busy path is cleaned up when the work context ends.
func (s *session) Release(context.Context) error { return nil }

// view is one executor binding of a session to a specific operation context.
// It preserves the postgres semantics: using it with a different context is
// rejected, and once its context ends every use fails closed.
type view struct {
	session *session
	work    context.Context
}

func (v *view) DatabaseKey() string { return v.session.resource.key }

func (v *view) enter(ctx context.Context, write bool) error {
	if err := uow.Check(v.work, write); err != nil {
		return err
	}
	if ctx != v.work {
		return uow.Fail(v.work, uow.ErrScopeMismatch)
	}
	if !v.session.mu.TryLock() {
		return uow.Fail(v.work, uow.ErrBusy)
	}
	return nil
}

func (v *view) record(err error) error {
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		uow.Fail(v.work, err)
	}
	return err
}

// Exec is a write and therefore requires a read-write transaction; the
// UOW Check in enter has already refused pooled non-transactional views.
func (v *view) Exec(ctx context.Context, query string, args ...any) (ExecResult, error) {
	if err := v.enter(ctx, true); err != nil {
		return ExecResult{}, err
	}
	defer v.session.mu.Unlock()
	// The busy owner makes a busy cleanup cancel this in-flight statement
	// instead of waiting for it; registration happens under busyMu after
	// mu is held, so the owner is always the live one.
	stmtCtx, cancel := context.WithCancel(ctx)
	owner := &busyOwner{cancel: cancel}
	doomed := v.session.setBusy(owner)
	res, err := v.session.tx.ExecContext(stmtCtx, query, args...)
	owner.end()
	v.session.clearBusy(owner)
	if err != nil {
		return ExecResult{}, v.record(err)
	}
	if doomed {
		// A late registration after the session's cleanup ran: the
		// statement was canceled through its owner, and its result
		// cannot be trusted on a finished connection.
		return ExecResult{}, v.record(context.Canceled)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ExecResult{}, v.record(err)
	}
	insertID, err := res.LastInsertId()
	if err != nil {
		return ExecResult{}, v.record(err)
	}
	return ExecResult{RowsAffected: affected, LastInsertID: insertID}, nil
}

func (v *view) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	return v.query(ctx, false, query, args...)
}

// QueryWrite executes a write-returning cursor after the write guard.
func (v *view) QueryWrite(ctx context.Context, query string, args ...any) (Rows, error) {
	return v.query(ctx, true, query, args...)
}

func (v *view) query(ctx context.Context, write bool, query string, args ...any) (Rows, error) {
	if err := v.enter(ctx, write); err != nil {
		return nil, err
	}
	// One owner spans the whole statement: cancel aborts the in-flight
	// driver call and the returned rows stay bound to that context until
	// the cursor closes, so a cleanup racing either phase still ends the
	// statement exactly once.
	stmtCtx, cancel := context.WithCancel(ctx)
	owner := &busyOwner{cancel: cancel}
	v.session.setBusy(owner)
	var rows *sql.Rows
	var err error
	if v.session.pooled || v.session.tx == nil {
		rows, err = v.session.resource.pool.QueryContext(stmtCtx, query, args...)
	} else {
		rows, err = v.session.tx.QueryContext(stmtCtx, query, args...)
	}
	if err != nil {
		owner.end()
		v.session.clearBusy(owner)
		v.session.mu.Unlock()
		return nil, v.record(err)
	}
	c := &cursor{rows: rows, binding: v, owner: owner}
	if !owner.upgrade(c.Close) {
		// A cleanup already consumed the owner and the upgrade path
		// closed the cursor, releasing the session. The returned cursor
		// is dead; the doomed operation reports it through Err.
		return c, nil
	}
	// A late registration after the session's cleanup ran still leaves
	// the cursor's statement context canceled by setBusy, so iteration
	// fails immediately; the session lock stays held until Close.
	return c, nil
}

func (v *view) QueryRow(ctx context.Context, query string, args ...any) Row {
	return v.queryRow(ctx, false, query, args...)
}

// QueryRowWrite executes a write-returning statement after the write guard.
func (v *view) QueryRowWrite(ctx context.Context, query string, args ...any) Row {
	return v.queryRow(ctx, true, query, args...)
}

func (v *view) queryRow(ctx context.Context, write bool, query string, args ...any) Row {
	if err := v.enter(ctx, write); err != nil {
		return failedRow{err}
	}
	stmtCtx, cancel := context.WithCancel(ctx)
	owner := &busyOwner{cancel: cancel}
	v.session.setBusy(owner)
	var row *sql.Row
	if v.session.pooled || v.session.tx == nil {
		row = v.session.resource.pool.QueryRowContext(stmtCtx, query, args...)
	} else {
		row = v.session.tx.QueryRowContext(stmtCtx, query, args...)
	}
	r := &singleRow{row: row, binding: v, owner: owner}
	if !owner.upgrade(r.forceRelease) {
		// The cleanup already force-released the row and the session.
		return r
	}
	return r
}

// cursor wraps database/sql rows so the session stays locked until the
// cursor closes, exactly like the PostgreSQL executor.
type cursor struct {
	rows    *sql.Rows
	binding *view
	owner   *busyOwner
	once    sync.Once
	// err is stored atomically: a cleanup-invoked Close can race the
	// caller's Err, and the store must be visible without ordering
	// assumptions on the caller's side.
	err atomic.Pointer[error]
}

func (c *cursor) Close() {
	c.once.Do(func() {
		// Read the true driver outcome BEFORE ending the owner: ending
		// cancels the statement context, and Rows.awaitDone would then
		// close the rows asynchronously with a false context error.
		c.rows.Close()
		if err := c.rows.Err(); err != nil {
			c.err.Store(&err)
		}
		_ = c.binding.record(c.rows.Err())
		// Ending the owner now cancels the statement context harmlessly;
		// the rows are already closed by the explicit Close above.
		// Unregister before unlocking: a busy cleanup that races this
		// close finds the owner consumed, and once.Do makes a
		// cleanup-invoked close a no-op.
		c.owner.end()
		c.binding.session.clearBusy(c.owner)
		c.binding.session.mu.Unlock()
	})
}

func (c *cursor) Next() bool {
	if uow.Check(c.binding.work, false) != nil {
		c.Close()
		return false
	}
	ok := c.rows.Next()
	if !ok {
		c.Close()
	}
	return ok
}

func (c *cursor) Scan(dest ...any) error { return c.binding.record(c.rows.Scan(dest...)) }
func (c *cursor) Err() error {
	if v := c.err.Load(); v != nil {
		return *v
	}
	return nil
}

// singleRow scans at most once and releases the session on the first Scan.
// A row that is never scanned keeps the unit of work busy.
type singleRow struct {
	row     *sql.Row
	binding *view
	owner   *busyOwner
	once    sync.Once
	mu      sync.Mutex
	scanned bool
}

func (r *singleRow) Scan(dest ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scanned {
		return uow.ErrClosed
	}
	r.scanned = true
	defer r.once.Do(r.binding.session.mu.Unlock)
	if err := uow.Check(r.binding.work, false); err != nil {
		// The view ended first; the cleanup path owns the statement.
		r.owner.end()
		r.binding.session.clearBusy(r.owner)
		return err
	}
	err := r.binding.record(r.row.Scan(dest...))
	// End the owner only AFTER Scan: ending cancels the statement context,
	// and *sql.Row.awaitDone would otherwise close the rows with a false
	// context error before the scan could read the real outcome.
	r.owner.end()
	r.binding.session.clearBusy(r.owner)
	return err
}

// forceRelease consumes the row from the session's busy cleanup path so
// the driver statement cannot pin the pooled connection forever. The row
// is dead afterwards: a later Scan reports closed.
func (r *singleRow) forceRelease() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scanned {
		return
	}
	r.scanned = true
	r.owner.end()
	r.binding.session.clearBusy(r.owner)
	_ = r.row.Scan() // *sql.Row.Scan always closes its driver rows first.
	r.once.Do(r.binding.session.mu.Unlock)
}

type failedRow struct{ err error }

func (r failedRow) Scan(...any) error { return r.err }
