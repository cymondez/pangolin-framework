package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
)

// The cleanup regressions in this file use a barrier driver so the exact
// interleavings are deterministic: a query that blocks until its context is
// canceled, a completion that races an already-registered statement, the
// exact post-cleanup late-registration state, and a driver-held closer that
// keeps the cleanup job mid-close while the caller probes the cursor. The
// backend semantics themselves are proven by the real-driver suites and
// integration tests, never here.

var (
	barrierMu      sync.Mutex
	barrierQueries = map[string]*blockQuery{}
	holdQueries    = map[string]*holdQuery{}
)

// blockQuery describes one registered blocking statement.
type blockQuery struct {
	started  chan struct{} // closed when the driver call begins
	released chan struct{} // closed when the driver call returns
}

func registerBlock(name string) *blockQuery {
	q := &blockQuery{started: make(chan struct{}), released: make(chan struct{})}
	barrierMu.Lock()
	barrierQueries[name] = q
	barrierMu.Unlock()
	return q
}

// holdQuery describes one registered statement returning a live driver rows
// whose Close blocks until the test releases it.
type holdQuery struct {
	closeStarted chan struct{} // closed when the first driver Close begins
	released     chan struct{} // closed by the test to let the close return
	signaled     atomic.Bool
	closes       atomic.Int64 // driver Close calls observed

	mu       sync.Mutex
	queryCtx context.Context // statement context captured by the driver
}

func registerHold(name string) *holdQuery {
	q := &holdQuery{closeStarted: make(chan struct{}), released: make(chan struct{})}
	barrierMu.Lock()
	holdQueries[name] = q
	barrierMu.Unlock()
	return q
}

func (q *holdQuery) setQueryContext(ctx context.Context) {
	q.mu.Lock()
	q.queryCtx = ctx
	q.mu.Unlock()
}

func (q *holdQuery) queryContext() context.Context {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.queryCtx
}

// holdRows: Next reports EOF immediately and Close blocks on the release
// channel, pinning the session-owned cleanup job at the moment its close
// runs the cursor closer.
type holdRows struct{ q *holdQuery }

func (r *holdRows) Columns() []string { return []string{"v"} }

func (r *holdRows) Close() error {
	r.q.closes.Add(1)
	if r.q.signaled.CompareAndSwap(false, true) {
		close(r.q.closeStarted)
	}
	<-r.q.released
	return nil
}

func (r *holdRows) Next([]driver.Value) error { return io.EOF }

type barrierDriver struct{}

func (barrierDriver) Open(string) (driver.Conn, error) { return &barrierConn{}, nil }

type barrierConn struct{}

func (c *barrierConn) Prepare(query string) (driver.Stmt, error) {
	return &barrierStmt{query: query}, nil
}
func (c *barrierConn) Close() error              { return nil }
func (c *barrierConn) Begin() (driver.Tx, error) { return barrierTx{}, nil }

type barrierTx struct{}

func (barrierTx) Commit() error   { return nil }
func (barrierTx) Rollback() error { return nil }

// barrierStmt blocks in Query until its context is canceled, then returns
// the context error exactly like a real network driver would.
type barrierStmt struct {
	query string
}

func (s *barrierStmt) Close() error  { return nil }
func (s *barrierStmt) NumInput() int { return -1 }

func (s *barrierStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("exec unsupported")
}

func (s *barrierStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, errors.New("query without context unsupported")
}

func (s *barrierStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	barrierMu.Lock()
	q := barrierQueries[s.query]
	h := holdQueries[s.query]
	barrierMu.Unlock()
	if h != nil {
		// The driver sees the statement context the executor built; the
		// test later asserts the busy owner canceled exactly it.
		h.setQueryContext(ctx)
		return &holdRows{q: h}, nil
	}
	if q == nil {
		return nil, errors.New("unregistered barrier query: " + s.query)
	}
	close(q.started)
	select {
	case <-ctx.Done():
		close(q.released)
		return nil, ctx.Err()
	case <-q.released:
		return nil, errors.New("barrier released without cancel")
	}
}

func init() { sql.Register("database-barrier", barrierDriver{}) }

func barrierResource(t *testing.T, key string) *SQLResource {
	t.Helper()
	db, err := sql.Open("database-barrier", "barrier")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	r, err := NewSQLResource(db, SQLConfig{
		Backend:       "barrier",
		Key:           key,
		Capabilities:  Capabilities{Backend: "barrier"},
		Dialect:       QuestionDialect,
		Isolation:     func(uow.Isolation) (sql.IsolationLevel, error) { return sql.LevelDefault, nil },
		ReadOnly:      func(uow.AccessMode) bool { return false },
		CommitOutcome: func(error) uow.Outcome { return uow.Unknown },
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestCleanupCancelsInFlightQueryAndReusesPool is the barrier-controlled
// regression for a callback ending while a Query is in flight: the busy
// completion must fail busy, the session-owned cleanup must cancel the
// blocked driver call, discard the connection, and the pool must report
// no in-use connection afterwards — without races or duplicate cleanup.
func TestCleanupCancelsInFlightQueryAndReusesPool(t *testing.T) {
	const qname = "cleanup-inflight"
	q := registerBlock(qname)
	r := barrierResource(t, "barrier-cleanup")
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")

	var wg sync.WaitGroup
	var queryErr error
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The abandoned query blocks in the driver until the busy
			// cleanup cancels its statement context.
			_, queryErr = ex.Query(c, qname)
		}()
		// Wait until the driver call is truly in flight before the
		// callback ends, so the completion really races a live query.
		select {
		case <-q.started:
		case <-time.After(5 * time.Second):
			queryErr = errors.New("barrier query never started")
		}
		return nil
	})
	wg.Wait()

	// The busy completion refused to commit under the in-flight query.
	var failure uow.Error
	if !errors.As(err, &failure) {
		t.Fatalf("busy completion not a uow failure: %v", err)
	}
	if !errors.Is(err, uow.ErrBusy) {
		t.Fatalf("busy completion wrong: %v", err)
	}
	if failure.Outcome() == uow.Committed || failure.Outcome() == uow.CompletedNonTransactional {
		t.Fatalf("busy completion committed: %d", failure.Outcome())
	}
	// The blocked driver call was canceled and returned a context error.
	if queryErr == nil || !errors.Is(queryErr, context.Canceled) {
		t.Fatalf("in-flight query not canceled: %v", queryErr)
	}
	// The doomed connection was discarded, not returned for reuse; the
	// discard happens in the background job, so wait for it bounded.
	requireIdlePoolSoon(t, r)
}

// requireIdlePoolSoon waits bounded for the background cleanup job to
// finish discarding the doomed connection before asserting an idle pool.
func requireIdlePoolSoon(t *testing.T, r *SQLResource) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r.Pool().Stats().InUse == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	requireIdlePool(t, r)
}

// TestCleanupEndsStatementRegisteredWhileBusy is the barrier-controlled
// regression for a statement that registered its owner while still in
// flight: the completion fails busy, the session-owned cleanup force-ends
// the already-registered owner (canceling the blocked driver call), the
// bounded wait unblocks the abandoned statement, and the retried rollback
// still completes the doomed transaction — without races or duplicate
// cleanup. The late-registration state (registration after the cleanup
// completed) is tested separately by TestSetBusyAfterCleanupIsDoomedAndSelfCloses.
func TestCleanupEndsStatementRegisteredWhileBusy(t *testing.T) {
	const qname = "cleanup-late"
	q := registerBlock(qname)
	r := barrierResource(t, "barrier-late")
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")

	var s *session
	var wg sync.WaitGroup
	var queryErr error
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		s = c.Value(executorKey{}).(*session)
		ex, err := Current(c)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, queryErr = ex.Query(c, qname)
		}()
		select {
		case <-q.started:
		case <-time.After(5 * time.Second):
			queryErr = errors.New("barrier query never started")
		}
		// The direct session-level completion triggers the cleanup while
		// the blocked query holds mu; the bounded wait then releases the
		// abandoned statement, so the retried rollback completes the
		// doomed transaction. A second busy completion joins the same
		// bounded job instead of scheduling a second one.
		if err := s.Rollback(context.Background()); err != nil && !errors.Is(err, uow.ErrBusy) {
			t.Errorf("rollback while busy failed wrongly: %v", err)
		}
		if err := s.Commit(context.Background()); !errors.Is(err, uow.ErrTransactionRequired) && !errors.Is(err, sql.ErrTxDone) && !errors.Is(err, uow.ErrBusy) {
			t.Errorf("commit on finished session failed wrongly: %v", err)
		}
		return nil
	})
	wg.Wait()

	if err == nil {
		t.Fatal("late registration root reported success")
	}
	if queryErr == nil || !errors.Is(queryErr, context.Canceled) {
		t.Fatalf("late query not canceled: %v", queryErr)
	}
	requireIdlePoolSoon(t, r)
	// The single cleanup job closed exactly once: both barrier phases ran.
	select {
	case <-q.released:
	default:
		t.Fatal("cleanup job did not release the barrier")
	}
	// The persistent done channel stayed closed; a repeated completion
	// joins the finished job and returns immediately.
	done := s.scheduleCleanup()
	select {
	case <-done:
	default:
		t.Fatal("repeated cleanup did not join the finished job")
	}
}

// TestSetBusyAfterCleanupIsDoomedAndSelfCloses is the exact state check of
// the enter-to-setBusy gap: a statement holds the session lock but has not
// registered its owner yet, and the cleanup job already ran to completion.
// The late setBusy must report doomed and cancel the new owner's statement
// context synchronously, the upgrade of that taken owner must close its
// cursor exactly once, the doomed owner must never be registered as the
// busy owner, and repeated completions must return the same persistent
// closed done channel.
func TestSetBusyAfterCleanupIsDoomedAndSelfCloses(t *testing.T) {
	r := barrierResource(t, "barrier-setbusy-doomed")
	// The cleaned session matches what beginTransaction built: only the
	// transaction context and the busy bookkeeping exist; no driver
	// statement is reachable because the session lock stays held across
	// the whole cleanup.
	_, cancelTx := context.WithCancel(context.Background())
	defer cancelTx()
	s := &session{resource: r, cancelTx: cancelTx}

	// The simulated enter-to-setBusy gap: mu is held, no owner registered.
	s.mu.Lock()
	defer s.mu.Unlock()

	// The single cleanup job completes while the gap holds only mu.
	done := s.scheduleCleanup()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup job did not complete")
	}

	// A late registration is refused as doomed and cancels its statement
	// context synchronously through the owner close.
	stmtCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := &busyOwner{cancel: cancel}
	if doomed := s.setBusy(owner); !doomed {
		t.Fatal("late registration not reported doomed")
	}
	if err := stmtCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("late owner not canceled by setBusy: %v", err)
	}
	// The doomed owner was never registered as the session's busy owner.
	if busy := s.takeBusy(); busy != nil {
		t.Fatal("late owner registered on a cleaned session")
	}

	// The upgrade of the taken owner closes the cursor exactly once and
	// reports the owner as consumed.
	closes := 0
	if owner.upgrade(func() { closes++ }) {
		t.Fatal("taken owner upgraded as live")
	}
	if closes != 1 {
		t.Fatalf("cursor closer ran %d times, want 1", closes)
	}

	// Repeated completions return the same persistent, closed channel.
	if again := s.scheduleCleanup(); again != done {
		t.Fatal("repeated cleanup returned a different done channel")
	}
	select {
	case <-done:
	default:
		t.Fatal("repeated cleanup channel not closed")
	}
}

// TestCleanupWaitIsBoundedByDeadline proves a caller whose cleanup context
// is already expired does not block on the cleanup job at all, while the
// job itself still runs to completion in the background.
func TestCleanupWaitIsBoundedByDeadline(t *testing.T) {
	const qname = "cleanup-deadline"
	q := registerBlock(qname)
	r := barrierResource(t, "barrier-deadline")
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")

	var wg sync.WaitGroup
	var queryErr error
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, queryErr = ex.Query(c, qname)
		}()
		select {
		case <-q.started:
		case <-time.After(5 * time.Second):
			queryErr = errors.New("barrier query never started")
		}
		// An already-expired cleanup context must return immediately
		// while the blocked query still holds mu. The job itself keeps
		// running in the background; wg.Wait below proves it finished
		// before the pool is checked.
		expired, cancel := context.WithCancel(context.Background())
		cancel()
		s := c.Value(executorKey{}).(*session)
		start := time.Now()
		if err := s.Rollback(expired); !errors.Is(err, uow.ErrBusy) {
			t.Errorf("expired rollback not refused: %v", err)
		}
		if time.Since(start) > 2*time.Second {
			t.Errorf("expired cleanup wait blocked: %v", time.Since(start))
		}
		// A second completion joins the same finished job; the bounded
		// wait returns immediately and no second cleanup runs.
		if err := s.Rollback(expired); !errors.Is(err, uow.ErrBusy) && !errors.Is(err, sql.ErrTxDone) {
			t.Errorf("repeated rollback failed wrongly: %v", err)
		}
		return nil
	})
	wg.Wait()
	// The background job discarded the doomed connection only after the
	// canceled driver call returned; the pool must be idle now.
	requireIdlePoolSoon(t, r)
	if err == nil {
		t.Fatal("deadline root reported success")
	}
	if queryErr == nil || !errors.Is(queryErr, context.Canceled) {
		t.Fatalf("blocked query not canceled: %v", queryErr)
	}
	requireIdlePool(t, r)
}

// TestPooledBusyCursorCleanupReleasesSession covers the pooled read path:
// an abandoned pooled cursor keeps the session busy, the cleanup
// force-releases it, and the pooled statement's context is canceled so no
// pooled connection stays reserved.
func TestPooledBusyCursorCleanupReleasesSession(t *testing.T) {
	const qname = "cleanup-pooled"
	q := registerBlock(qname)
	r := barrierResource(t, "barrier-pooled")
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")

	var wg sync.WaitGroup
	var queryErr error
	err := m.Within(ctx, uow.Options{Mode: uow.ReadOnly, Transaction: uow.NonTransactional}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, queryErr = ex.Query(c, qname)
		}()
		select {
		case <-q.started:
		case <-time.After(5 * time.Second):
			queryErr = errors.New("barrier query never started")
		}
		s := c.Value(executorKey{}).(*session)
		// A pooled session has no transaction to complete; the busy
		// cursor keeps the session locked and the Release path is the
		// completion the manager calls. The session-level guards still
		// refuse a busy cursor.
		if err := s.Prepare(context.Background()); !errors.Is(err, uow.ErrBusy) {
			t.Errorf("pooled busy cursor not refused: %v", err)
		}
		return nil
	})
	wg.Wait()

	// The pooled read-only operation completed without writing; the
	// abandoned cursor was canceled through its statement context.
	if queryErr == nil || !errors.Is(queryErr, context.Canceled) {
		t.Fatalf("pooled cursor not canceled: %v", queryErr)
	}
	if err != nil {
		var failure uow.Error
		if !errors.As(err, &failure) {
			t.Fatalf("pooled failure not a uow failure: %v", err)
		}
		// A read-only pooled operation has nothing to roll back; a
		// completed non-transactional outcome is the only legal success.
		if failure.Outcome() != uow.CompletedNonTransactional && !strings.Contains(err.Error(), "UOW_") {
			t.Fatalf("pooled operation failed wrongly: %v", err)
		}
	}
	requireIdlePoolSoon(t, r)
}

// TestAbandonedCursorCloseAfterCleanupIsIdempotent exercises the dead
// cursor after the busy cleanup already consumed its owner: the cursor was
// abandoned, the completion force-released it, and only after Within
// returned does the caller still touch it. This is an idempotent close of
// an already-cleaned cursor — not a close/cleanup race, which is covered
// by TestCursorCloseAndErrRaceSessionCleanup — and it must stay a safe
// no-op: dead iteration, exhausted owner close, no commit and no pool
// leak, whatever the interleaving after the cleanup finishes.
func TestAbandonedCursorCloseAfterCleanupIsIdempotent(t *testing.T) {
	r := openSQLite(t, "cursor-race")
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")

	for i := 0; i < 20; i++ {
		var rows Rows
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			ex, err := Current(c)
			if err != nil {
				return err
			}
			rows, err = ex.Query(c, "SELECT 1 UNION SELECT 2")
			if err != nil {
				return err
			}
			// Intentionally abandon the cursor: the completion fails
			// busy and the cleanup force-releases it.
			return nil
		})
		var failure uow.Error
		if !errors.As(err, &failure) || !errors.Is(err, uow.ErrBusy) {
			t.Fatalf("abandoned cursor completion wrong: %v", err)
		}
		if failure.Outcome() == uow.Committed {
			t.Fatalf("abandoned cursor committed: %d", failure.Outcome())
		}
		// The dead cursor reports no further rows.
		if rows.Next() {
			t.Fatal("abandoned cursor still readable")
		}
		rows.Close()
		requireIdlePool(t, r)
	}
}

// TestCursorCloseAndErrRaceSessionCleanup drives the interleaving the
// post-return idempotence test cannot reach: the session-owned cleanup job
// is held mid-close inside the driver while the caller touches the cursor.
// A concurrent Err must observe the cursor without blocking, a concurrent
// Close must join the same once-guard instead of closing the driver rows
// again, and the job must finish with exactly one driver close, a canceled
// statement context, no commit under the active cursor and no leaked
// pooled connection. Channels replace every sleep: the held driver close
// starts before the caller proceeds and is released before the asserts.
func TestCursorCloseAndErrRaceSessionCleanup(t *testing.T) {
	const qname = "cleanup-cursor-race"
	h := registerHold(qname)
	r := barrierResource(t, "barrier-cursor-race")
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")

	var rows Rows
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		rows, err = ex.Query(c, qname)
		if err != nil {
			return err
		}
		// Intentionally abandon the live cursor: the completion fails
		// busy and the busy cleanup drives its owner close.
		return nil
	})

	// The busy completion scheduled the cleanup job, which is now blocked
	// inside the abandoned cursor's driver close.
	select {
	case <-h.closeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup never reached the driver cursor close")
	}
	// A concurrent Err must stay non-blocking on the in-progress close.
	errReturned := make(chan struct{})
	go func() {
		defer close(errReturned)
		_ = rows.Err()
	}()
	select {
	case <-errReturned:
	case <-time.After(5 * time.Second):
		t.Error("concurrent Err blocked behind the cleanup close")
	}
	// A concurrent Close joins the same once-guard: it becomes a no-op
	// once the cleanup-driven close returns, never closing the driver
	// rows a second time.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rows.Close()
	}()
	// Release the driver; the cleanup job then ends the owner exactly
	// once and discards the doomed connection.
	close(h.released)
	wg.Wait()

	// The busy completion refused to commit under the active cursor.
	var failure uow.Error
	if !errors.As(err, &failure) {
		t.Fatalf("busy completion not a uow failure: %v", err)
	}
	if !errors.Is(err, uow.ErrBusy) {
		t.Fatalf("busy completion wrong: %v", err)
	}
	if failure.Outcome() == uow.Committed || failure.Outcome() == uow.CompletedNonTransactional {
		t.Fatalf("busy completion committed: %d", failure.Outcome())
	}
	// The busy owner canceled exactly the statement context the driver
	// received.
	if cerr := h.queryContext().Err(); !errors.Is(cerr, context.Canceled) {
		t.Fatalf("abandoned cursor statement not canceled: %v", cerr)
	}
	// The driver cursor was closed exactly once overall.
	if closes := h.closes.Load(); closes != 1 {
		t.Fatalf("driver cursor close ran %d times, want 1", closes)
	}
	// The doomed connection was discarded and nothing stayed in use;
	// the discard happens in the cleanup job, so wait bounded.
	requireIdlePoolSoon(t, r)
}
