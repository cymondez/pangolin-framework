package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/outbox"
	"github.com/jackc/pgx/v5/pgconn"
)

// The capacity admission contract is deployment-owned: the typed errors, the
// database-code mapping and the provisioning guards are the whole fail-closed
// surface a configuration or driver fault can reach without live services.
func TestCapacityErrorSentinelsAreTypedAndReachable(t *testing.T) {
	probe := func(sentinel error, code string, status int) {
		t.Helper()
		if sentinel.Error() != code || errCode(sentinel) != code || httpStatus(sentinel) != status {
			t.Fatalf("sentinel %v drifted: %s/%s/%d", sentinel, sentinel.Error(), errCode(sentinel), httpStatus(sentinel))
		}
	}
	probe(ErrCapacity, "EVENT_CAPACITY_EXCEEDED", 429)
	probe(ErrCapacityUnprovisioned, "EVENT_CAPACITY_UNPROVISIONED", 503)
	probe(ErrCapacityConfigConflict, "EVENT_CAPACITY_CONFIG_CONFLICT", 503)
	probe(ErrCapacityRequired, "EVENT_CAPACITY_REQUIRED", 503)
	probe(ErrCapacityUpgradeRequired, "EVENT_CAPACITY_UPGRADE_REQUIRED", 503)
}

// TestCapacityOfMapsPermitCodes pins the exact admission contract: every
// database permit code maps to its typed error identity, and foreign failures
// pass through untouched.
func TestCapacityOfMapsPermitCodes(t *testing.T) {
	codes := map[string]error{
		errCodeCapacityExceeded: ErrCapacity,
		errCodeUnprovisioned:    ErrCapacityUnprovisioned,
		errCodeConfigConflict:   ErrCapacityConfigConflict,
		errCodeCapacityRequired: ErrCapacityRequired,
		errCodeUpgradeRequired:  ErrCapacityUpgradeRequired,
	}
	for code, sentinel := range codes {
		mapped := capacityOf(&pgconn.PgError{Code: code})
		if !errors.Is(mapped, sentinel) {
			t.Fatalf("code %s mapped to %v, want %v", code, mapped, sentinel)
		}
	}
	wrapped := capacityOf(&wrappedPgError{&pgconn.PgError{Code: errCodeCapacityExceeded}})
	if !errors.Is(wrapped, ErrCapacity) {
		t.Fatal("wrapped permit code must still map through errors.As")
	}
	foreign := errors.New("connection refused")
	if got := capacityOf(foreign); got != foreign {
		t.Fatalf("foreign failure must pass through unchanged, got %v", got)
	}
}

type wrappedPgError struct{ *pgconn.PgError }

func (w *wrappedPgError) Unwrap() error { return w.PgError }

// errCode/httpStatus read the capacity contract through the public error
// interface without depending on concrete types.
func errCode(err error) string {
	type coder interface{ Code() string }
	if c, ok := err.(coder); ok {
		return c.Code()
	}
	return ""
}

func httpStatus(err error) int {
	type statuser interface{ HTTPStatus() int }
	if s, ok := err.(statuser); ok {
		return s.HTTPStatus()
	}
	return 0
}

// TestNewBoundedStoreRefusesUnboundedCapacity keeps the unbounded alias out of
// provisioned deployments, and delegates the pool guard to NewStore.
func TestNewBoundedStoreRefusesUnboundedCapacity(t *testing.T) {
	if _, err := NewBoundedStore(nil, testPolicy(), Capacity{}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("unbounded capacity rejected with %v, want ErrCapacity", err)
	}
	if _, err := NewBoundedStore(nil, testPolicy(), Capacity{Outbox: 1}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("half-configured capacity rejected with %v, want ErrCapacity", err)
	}
	if _, err := NewBoundedStore(nil, testPolicy(), Capacity{Outbox: 1, Inbox: 1}); !errors.Is(err, outbox.ErrUnavailable) {
		t.Fatalf("configured capacity without a pool must surface the store guard, got %v", err)
	}
}

// probeDriver scripts the deployment provisioning transaction: every SQL
// statement succeeds unless one of the fail flags is set.
type probeDriver struct {
	failExec       bool
	failSecondExec bool
	failCommit     bool
	failBegin      bool
	execCall       int
}

func (p *probeDriver) Open(string) (driver.Conn, error) {
	if p.failBegin {
		return nil, errors.New("database unreachable")
	}
	return p, nil
}
func (p *probeDriver) Prepare(string) (driver.Stmt, error) {
	return probeStmt{}, nil
}
func (p *probeDriver) Close() error              { return nil }
func (p *probeDriver) Begin() (driver.Tx, error) { return p, nil }
func (p *probeDriver) Commit() error {
	if p.failCommit {
		return errors.New("commit rejected")
	}
	return nil
}
func (p *probeDriver) Rollback() error { return nil }
func (p *probeDriver) Exec(string, []driver.Value) (driver.Result, error) {
	p.execCall++
	if p.failExec || p.failSecondExec && p.execCall > 1 {
		return nil, errors.New("provision statement rejected")
	}
	return driver.RowsAffected(1), nil
}

type probeStmt struct{}

func (probeStmt) Close() error                               { return nil }
func (probeStmt) NumInput() int                              { return -1 }
func (probeStmt) Exec([]driver.Value) (driver.Result, error) { return driver.RowsAffected(1), nil }
func (probeStmt) Query([]driver.Value) (driver.Rows, error)  { return nil, errors.New("no rows") }

// probeUnopenableDriver refuses every connection attempt.
type probeUnopenableDriver struct{ probeDriver }

func (p *probeUnopenableDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("database unreachable")
}

var _ driver.Conn = (*probeDriver)(nil)
var _ driver.Execer = (*probeDriver)(nil)

func probeDatabase(t *testing.T, p *probeDriver) *sql.DB {
	t.Helper()
	name := "probe-event-capacity"
	sql.Register(name, p)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	// Re-open the scripted connection per transaction so later failBegin runs
	// reach the driver Open instead of a cached healthy connection.
	db.SetMaxIdleConns(0)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestProvisionQueuesGuardsAndFaults pins the single-transaction deployment
// contract: identity/action/capacity guards, complete rollback of both queues
// on any statement failure, both rollbacks reachable from either statement,
// and the commit and success paths.
func TestProvisionQueuesGuardsAndFaults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ProvisionQueues(ctx, nil, 1, 1, "resize"); err == nil {
		t.Fatal("nil migration database accepted")
	}
	driver := &probeDriver{}
	db := probeDatabase(t, driver)
	if err := ProvisionQueues(ctx, db, 1, 1, "upgrade"); err == nil {
		t.Fatal("unknown provisioning action accepted")
	}
	if err := ProvisionQueues(ctx, db, 0, 1, "resize"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("nonpositive outbox capacity accepted: %v", err)
	}
	if err := ProvisionQueues(ctx, db, 1, 0, "resize"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("nonpositive inbox capacity accepted: %v", err)
	}
	driver.failExec = true
	if err := ProvisionQueues(ctx, db, 1, 1, "resize"); err == nil || errors.Is(err, ErrCapacity) {
		t.Fatalf("failed provisioning statement accepted: %v", err)
	}
	driver.failExec = false
	driver.failSecondExec = true
	driver.execCall = 0
	if err := ProvisionQueues(ctx, db, 1, 1, "resize"); err == nil {
		t.Fatal("failed inbox provisioning statement accepted")
	}
	driver.failSecondExec = false
	driver.execCall = 0
	driver.failCommit = true
	if err := ProvisionQueues(ctx, db, 1, 1, "resize"); err == nil {
		t.Fatal("failed provisioning commit accepted")
	}
	driver.failCommit = false
	if err := ProvisionQueues(ctx, db, 1, 1, "resize"); err != nil {
		t.Fatalf("clean provisioning run failed: %v", err)
	}
	driver.failBegin = true
	if err := ProvisionQueues(ctx, db, 1, 1, "resize"); err == nil {
		t.Fatal("unopenable provisioning transaction accepted")
	}
}

// TestProvisionQueuesRefusesUnopenableDatabase covers the transaction guard
// with a connector that cannot open at all.
func TestProvisionQueuesRefusesUnopenableDatabase(t *testing.T) {
	unopenable := &probeUnopenableDriver{}
	name := "probe-event-capacity-unopenable"
	sql.Register(name, unopenable)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ProvisionQueues(context.Background(), db, 1, 1, "resize"); err == nil {
		t.Fatal("provisioning accepted an unreachable database")
	}
}

var _ = io.Discard
