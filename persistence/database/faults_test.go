package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// faultDriver is a registered stub driver for failure-path tests only; the
// backend semantics themselves are proven by the real-driver suites and
// integration tests, never by this stub.
var faultCommitErr, faultRollbackErr, faultBeginErr error

type faultDriver struct{}

func (faultDriver) Open(string) (driver.Conn, error) { return &faultConn{}, nil }

type faultConn struct{}

func (c *faultConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *faultConn) Close() error { return nil }
func (c *faultConn) Begin() (driver.Tx, error) {
	if faultBeginErr != nil {
		return nil, faultBeginErr
	}
	return faultTx{}, nil
}

type faultTx struct{}

func (faultTx) Commit() error   { return faultCommitErr }
func (faultTx) Rollback() error { return faultRollbackErr }

func init() { sql.Register("database-fault", faultDriver{}) }

func faultResource(t *testing.T, key string, commitOutcome func(error) uow.Outcome) *SQLResource {
	t.Helper()
	db, err := sql.Open("database-fault", "fault")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	r, err := NewSQLResource(db, SQLConfig{
		Backend:       "fault",
		Key:           key,
		Capabilities:  Capabilities{Backend: "fault"},
		Dialect:       QuestionDialect,
		Isolation:     func(uow.Isolation) (sql.IsolationLevel, error) { return sql.LevelDefault, nil },
		ReadOnly:      func(uow.AccessMode) bool { return false },
		CommitOutcome: commitOutcome,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func faultDefaults() uow.DefaultOptions {
	return uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2}
}

func TestSessionCommitFailureStaysUnknown(t *testing.T) {
	faultCommitErr = errors.New("wire cut during COMMIT")
	defer func() { faultCommitErr = nil }()
	r := faultResource(t, "fault", nil)
	m, err := uow.NewManager(r, faultDefaults())
	if err != nil {
		t.Fatal(err)
	}
	ctx := trustedContext(t, "tenant-a")
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("commit failure swallowed")
	}
	var f uow.Error
	if !errors.As(err, &f) || f.Outcome() != uow.Unknown {
		t.Fatalf("commit failure outcome not unknown: %v", err)
	}
	if !errors.Is(err, uow.ErrCommitUnknown) {
		t.Fatalf("commit failure not UOW_COMMIT_UNKNOWN: %v", err)
	}
}

func TestSessionRollbackFailureStaysUnknown(t *testing.T) {
	faultRollbackErr = errors.New("rollback refused")
	defer func() { faultRollbackErr = nil }()
	r := faultResource(t, "fault", nil)
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")
	cause := errors.New("business failure")
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(context.Context) error { return cause })
	if !errors.Is(err, cause) {
		t.Fatalf("cause lost: %v", err)
	}
	var f uow.Error
	if !errors.As(err, &f) || f.Outcome() != uow.Unknown {
		t.Fatalf("rollback failure outcome not unknown: %v", err)
	}
	// The cleanup failure is reported, never rewritten into a success or a
	// confirmed rollback.
	if !errors.Is(err, uow.ErrBusy) && f.Code() != "UOW_CLEANUP_FAILED" && !errors.Is(err, faultRollbackErr) {
		t.Fatalf("cleanup failure lost: %v", err)
	}
}

func TestSessionPooledCompletionGuards(t *testing.T) {
	r := faultResource(t, "fault", nil)
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")
	var s *session
	err := m.Within(ctx, uow.Options{Mode: uow.ReadOnly, Transaction: uow.NonTransactional}, func(c context.Context) error {
		s = c.Value(executorKey{}).(*session)
		if !s.pooled {
			t.Fatal("read-only non-transactional op is not pooled")
		}
		// Prepare refuses while a statement lock is held.
		s.mu.Lock()
		if err := s.Prepare(context.Background()); !errors.Is(err, uow.ErrBusy) {
			s.mu.Unlock()
			return err
		}
		s.mu.Unlock()
		return s.Prepare(context.Background())
	})
	if err != nil {
		t.Fatal(err)
	}
	// A pooled session has no transaction: completing one is refused.
	if err := s.Commit(context.Background()); !errors.Is(err, uow.ErrTransactionRequired) {
		t.Fatalf("pooled commit accepted: %v", err)
	}
	if err := s.Rollback(context.Background()); !errors.Is(err, uow.ErrTransactionRequired) {
		t.Fatalf("pooled rollback accepted: %v", err)
	}
	if err := s.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionCommitAndRollbackRefuseWhileBusy(t *testing.T) {
	r := faultResource(t, "fault", nil)
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		s := c.Value(executorKey{}).(*session)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.Commit(context.Background()); !errors.Is(err, uow.ErrBusy) {
			return err
		}
		if err := s.Rollback(context.Background()); !errors.Is(err, uow.ErrBusy) {
			return err
		}
		if err := s.Prepare(context.Background()); !errors.Is(err, uow.ErrBusy) {
			return err
		}
		return nil
	})
	// The direct busy completions above ran the session cleanup, which
	// correctly dooms this transaction; the root must fail and roll back,
	// never report success.
	if err == nil {
		t.Fatal("busy completion reported success")
	}
	var failure uow.Error
	if !errors.As(err, &failure) {
		t.Fatalf("busy completion not a uow failure: %v", err)
	}
	if failure.Outcome() == uow.Committed || failure.Outcome() == uow.CompletedNonTransactional {
		t.Fatalf("doomed busy transaction completed: %d", failure.Outcome())
	}
}

func TestCommitFailureOutcomeClassification(t *testing.T) {
	r := faultResource(t, "fault", nil)
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")
	var s *session
	_ = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		s = c.Value(executorKey{}).(*session)
		return nil
	})
	if got := s.CommitFailureOutcome(sql.ErrTxDone); got != uow.RolledBack {
		t.Fatalf("ErrTxDone outcome: %d", got)
	}
	// Context errors after a commit attempt prove nothing: the server may
	// have committed before the acknowledgment was lost, so they stay
	// Unknown without backend proof.
	if got := s.CommitFailureOutcome(context.Canceled); got != uow.Unknown {
		t.Fatalf("canceled outcome: %d", got)
	}
	if got := s.CommitFailureOutcome(context.DeadlineExceeded); got != uow.Unknown {
		t.Fatalf("deadline outcome: %d", got)
	}
	if got := s.CommitFailureOutcome(errors.New("wire cut")); got != uow.Unknown {
		t.Fatalf("wire failure outcome: %d", got)
	}
	// A backend hook may prove known rollback failures.
	provable, err := NewSQLResource(r.Pool(), SQLConfig{
		Backend:       "provable",
		Key:           "provable",
		Capabilities:  Capabilities{Backend: "provable"},
		Dialect:       QuestionDialect,
		Isolation:     func(uow.Isolation) (sql.IsolationLevel, error) { return sql.LevelDefault, nil },
		CommitOutcome: func(error) uow.Outcome { return uow.RolledBack },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := (&session{resource: provable}).CommitFailureOutcome(errors.New("known")); got != uow.RolledBack {
		t.Fatalf("hook outcome: %d", got)
	}
}

func TestNewSQLResourceValidation(t *testing.T) {
	valid := SQLConfig{
		Backend:      "fault",
		Capabilities: Capabilities{Backend: "fault"},
		Dialect:      QuestionDialect,
		Isolation:    func(uow.Isolation) (sql.IsolationLevel, error) { return sql.LevelDefault, nil },
	}
	if _, err := NewSQLResource(nil, valid); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("nil pool accepted: %v", err)
	}
	db, err := sql.Open("database-fault", "x")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for name, mutate := range map[string]func(*SQLConfig){
		"missing-backend":   func(c *SQLConfig) { c.Backend = "" },
		"missing-isolation": func(c *SQLConfig) { c.Isolation = nil },
		"missing-dialect":   func(c *SQLConfig) { c.Dialect = Dialect{} },
		"caps-mismatch":     func(c *SQLConfig) { c.Capabilities = Capabilities{Backend: "other"} },
	} {
		c := valid
		mutate(&c)
		if _, err := NewSQLResource(db, c); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if _, err := NewSQLResource(db, valid); err != nil {
		t.Fatal(err)
	}
	// A zero ReadOnly is allowed and defaults to never requesting it.
	if _, err := NewSQLResource(db, SQLConfig{
		Backend:      "fault",
		Capabilities: Capabilities{Backend: "fault"},
		Dialect:      QuestionDialect,
		Isolation:    valid.Isolation,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBeginGuards(t *testing.T) {
	r := faultResource(t, "fault", nil)
	ctx := trustedContext(t, "tenant-a")
	// Missing scope binding.
	if _, _, err := r.Begin(context.Background(), uow.EffectiveOptions{Transactional: true, DatabaseKey: r.DatabaseKey()}); !errors.Is(err, uow.ErrResourceMismatch) {
		t.Fatalf("missing scope accepted: %v", err)
	}
	// Wrong database identity.
	if _, _, err := r.Begin(ctx, uow.EffectiveOptions{Transactional: true, DatabaseKey: "other"}); !errors.Is(err, uow.ErrResourceMismatch) {
		t.Fatalf("wrong key accepted: %v", err)
	}
	// Invalid scope reference: the trusted resolver rejects the mismatched
	// tenant ID, so no binding can ever be fabricated for Begin.
	_, berr := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "x", Kind: "user"}, "tenant-a", true
	}).Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "different"})
	if berr == nil {
		t.Fatal("invalid scope reference accepted by resolver")
	}
	// Rejected isolation fails before any connection is taken.
	strict, serr := NewSQLResource(r.Pool(), SQLConfig{
		Backend:      "strict",
		Key:          "strict",
		Capabilities: Capabilities{Backend: "strict"},
		Dialect:      QuestionDialect,
		Isolation:    func(uow.Isolation) (sql.IsolationLevel, error) { return sql.LevelDefault, databaseErrIsolation },
	})
	if serr != nil {
		t.Fatal(serr)
	}
	if _, _, err := strict.Begin(ctx, uow.EffectiveOptions{Transactional: true, DatabaseKey: "strict", Isolation: uow.Serializable}); !errors.Is(err, databaseErrIsolation) {
		t.Fatalf("isolation downgrade accepted: %v", err)
	}
}

var databaseErrIsolation = errors.New("strict isolation refused")

func TestCurrentGuards(t *testing.T) {
	if _, err := Current(context.Background()); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("outside uow accepted: %v", err)
	}
	r := faultResource(t, "fault", nil)
	m, _ := uow.NewManager(r, faultDefaults())
	ctx := trustedContext(t, "tenant-a")
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		if _, err := Current(c); err != nil {
			return err
		}
		// A foreign executor value is a resource mismatch and poisons the
		// root: the callback reports it and the manager surfaces it.
		foreign := context.WithValue(c, executorKey{}, "not a binder")
		if _, err := Current(foreign); !errors.Is(err, uow.ErrResourceMismatch) {
			return errors.New("foreign binder accepted")
		}
		return nil
	})
	if !errors.Is(err, uow.ErrRollbackOnly) {
		t.Fatalf("guard violation did not poison the root: %v", err)
	}
}

func TestBeginFailurePaths(t *testing.T) {
	ctx := trustedContext(t, "tenant-a")
	// A canceled context fails Begin before taking a connection.
	r := faultResource(t, "fault", nil)
	m, _ := uow.NewManager(r, faultDefaults())
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	err := m.Within(canceled, uow.Options{Mode: uow.ReadWrite}, func(context.Context) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled begin accepted: %v", err)
	}
	// A driver-level BEGIN failure surfaces as UOW_BEGIN_FAILED without
	// leaking the operation.
	faultBeginErr = errors.New("begin refused")
	defer func() { faultBeginErr = nil }()
	r2 := faultResource(t, "fault-begin", nil)
	m2, _ := uow.NewManager(r2, faultDefaults())
	err = m2.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("begin failure swallowed")
	}
	var f uow.Error
	if !errors.As(err, &f) || f.Outcome() != uow.NotStarted {
		t.Fatalf("begin failure outcome wrong: %v", err)
	}
}

func TestFailedRowScan(t *testing.T) {
	r := failedRow{errors.New("view refused")}
	if err := r.Scan(); err == nil {
		t.Fatal("failed row scanned")
	}
}
