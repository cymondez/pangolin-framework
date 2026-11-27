package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	_ "modernc.org/sqlite"
)

// scopedFixture is the local test-fixture schema of the acceptance store
// used to exercise the neutral Port on SQLite. It is deliberately NOT
// production API: the framework ships owner and predicate helpers, while
// this fixed id/value layout is an acceptance fixture.
const scopedFixture = "CREATE TABLE IF NOT EXISTS scoped_records(tenant_id TEXT NOT NULL, scope_kind TEXT NOT NULL, scope_id TEXT NOT NULL, id TEXT NOT NULL, value INTEGER NOT NULL, PRIMARY KEY(tenant_id, scope_kind, scope_id, id))"

// fixtureStore is the local fixture repository over the fixture schema. Its
// statements bind the full owner (tenant, scope kind, scope id) from the
// trusted binding through the production OwnerPredicate helper; there is no
// unscoped path and no DTO owner substitution.
type fixtureStore struct {
	dialect Dialect
	table   string
}

func (s fixtureStore) Insert(ctx context.Context, ex Executor, id string, value int64) error {
	o, err := CurrentOwner(ctx)
	if err != nil {
		return err
	}
	query := "INSERT INTO " + s.dialect.Identifier(s.table) + "(tenant_id,scope_kind,scope_id,id,value) VALUES(" + s.dialect.Placeholder(1) + "," + s.dialect.Placeholder(2) + "," + s.dialect.Placeholder(3) + "," + s.dialect.Placeholder(4) + "," + s.dialect.Placeholder(5) + ")"
	res, err := ex.Exec(ctx, query, o.TenantID, o.ScopeKind, o.ScopeID, id, value)
	if err != nil {
		return err
	}
	if res.RowsAffected != 1 {
		return ErrNoRows
	}
	return nil
}

func (s fixtureStore) Get(ctx context.Context, ex Executor, id string) (int64, error) {
	o, err := CurrentOwner(ctx)
	if err != nil {
		return 0, err
	}
	clause, args := OwnerPredicate(s.dialect, "", o)
	query := "SELECT " + s.dialect.Identifier("value") + " FROM " + s.dialect.Identifier(s.table) + " WHERE " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(1) + " AND " + clause
	var value int64
	if err := ex.QueryRow(ctx, query, append([]any{id}, args...)...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

// GetForOwner validates an untrusted DTO owner hint against the trusted
// binding before reading.
func (s fixtureStore) GetForOwner(ctx context.Context, ex Executor, id string, requested scope.ScopeRef) (int64, error) {
	o, err := ResolveOwner(ctx, requested)
	if err != nil {
		return 0, err
	}
	clause, args := OwnerPredicate(s.dialect, "", o)
	query := "SELECT " + s.dialect.Identifier("value") + " FROM " + s.dialect.Identifier(s.table) + " WHERE " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(1) + " AND " + clause
	var value int64
	if err := ex.QueryRow(ctx, query, append([]any{id}, args...)...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func (s fixtureStore) Count(ctx context.Context, ex Executor) (int64, error) {
	o, err := CurrentOwner(ctx)
	if err != nil {
		return 0, err
	}
	clause, args := OwnerPredicate(s.dialect, "", o)
	query := "SELECT count(*) FROM " + s.dialect.Identifier(s.table) + " WHERE " + clause
	var n int64
	if err := ex.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s fixtureStore) Delete(ctx context.Context, ex Executor, id string) (int64, error) {
	o, err := CurrentOwner(ctx)
	if err != nil {
		return 0, err
	}
	clause, args := OwnerPredicate(s.dialect, "", o)
	query := "DELETE FROM " + s.dialect.Identifier(s.table) + " WHERE " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(1) + " AND " + clause
	res, err := ex.Exec(ctx, query, append([]any{id}, args...)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected, nil
}

// requireIdlePool asserts every explicitly acquired connection was returned:
// the pool holds no in-use connection after a completed operation, which
// proves abandoned cursors, unscanned rows and cancellations do not leak.
func requireIdlePool(t *testing.T, r *SQLResource) {
	t.Helper()
	stats := r.Pool().Stats()
	if stats.InUse != 0 {
		t.Fatalf("pool connection leak: %d in use", stats.InUse)
	}
}

// openSQLite opens a real on-disk SQLite database through the neutral Port.
// WAL keeps concurrent readers legal so RequiresNew semantics can be tested
// against real locks; busy_timeout bounds lock waits deterministically.
func openSQLite(t *testing.T, key string) *SQLResource {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"\\"+key+".db?_journal_mode=WAL&_busy_timeout=300&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Four connections let a root plus two RequiresNew children hold real
	// transactions at once, so depth limits are tested instead of pool waits.
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	r, err := NewSQLResource(db, SQLConfig{
		Backend:      "sqlite",
		Key:          key,
		Capabilities: sqliteTestCapabilities(),
		Dialect:      QuestionDialect,
		Isolation: func(level uow.Isolation) (sql.IsolationLevel, error) {
			// SQLite admits the manager default and nothing else; any
			// other explicit request fails closed.
			switch level {
			case uow.IsolationDefault, uow.Serializable:
				return sql.LevelDefault, nil
			default:
				return sql.LevelDefault, errors.New("unsupported isolation")
			}
		},
		ReadOnly:      func(uow.AccessMode) bool { return false },
		CommitOutcome: func(error) uow.Outcome { return uow.Unknown },
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// sqliteTestCapabilities mirrors the sqlite adapter's truthful contract.
func sqliteTestCapabilities() Capabilities {
	return Capabilities{
		Backend:             "sqlite",
		RowLevelSecurity:    false,
		TransactionReadOnly: false,
		Isolations:          []uow.Isolation{uow.IsolationDefault, uow.Serializable},
	}
}

func newManager(t *testing.T, r *SQLResource) uow.UnitOfWorkManager {
	t.Helper()
	m, err := uow.NewManager(r, uow.DefaultOptions{DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second, HookTimeout: 20 * time.Millisecond, MaxRequiresNewDepth: 2, DefaultIsolation: uow.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func trustedContext(t *testing.T, tenant string) context.Context {
	t.Helper()
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, tenant, true
	})
	ctx, err := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func setupFixture(t *testing.T, r *SQLResource) {
	t.Helper()
	if _, err := r.Pool().Exec(scopedFixture); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteCommitRollbackAndOwnerScoping(t *testing.T) {
	r := openSQLite(t, "core")
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")
	store := fixtureStore{dialect: QuestionDialect, table: "scoped_records"}

	// Commit persists rows owned by the trusted binding.
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		return store.Insert(c, ex, "row-1", 10)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		n, err := store.Count(c, ex)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("committed row invisible: %d", n)
		}
		v, err := store.Get(c, ex, "row-1")
		if err != nil || v != 10 {
			t.Fatalf("committed value wrong: %d %v", v, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Business failure rolls everything back.
	cause := errors.New("business failure")
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := Current(c)
		if err := store.Insert(c, ex, "row-2", 20); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatal(err)
	}
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("rollback leaked rows: %d", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Another tenant sees none of tenant-a's rows: the owner predicate
	// includes tenant, so a cross-tenant read finds nothing.
	err = m.Within(trustedContext(t, "tenant-b"), uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("cross-tenant read: %d", n)
		}
		if _, err := store.Get(c, ex, "row-1"); !errors.Is(err, ErrNoRows) {
			t.Fatalf("cross-tenant get: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A DTO owner substitution attempt is denied; the trusted owner is kept.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := Current(c)
		_, err := store.GetForOwner(c, ex, "row-1", scope.ScopeRef{TenantID: "tenant-b", Kind: "tenant", ID: "tenant-b"})
		if !errors.Is(err, scope.ErrDenied) {
			return err
		}
		v, err := store.Get(c, ex, "row-1")
		if err != nil || v != 10 {
			t.Fatalf("trusted read broken after denial: %d %v", v, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A cross-tenant raw write through the executor stays visible only to
	// whoever queries it: the trusted store cannot address it.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := Current(c)
		_, err := ex.Exec(c, "INSERT INTO scoped_records(tenant_id,scope_kind,scope_id,id,value) VALUES('tenant-b','tenant','tenant-b','foreign',1)")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("foreign write leaked into trusted owner view: %d", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Delete removes only the trusted owner's row.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Delete(c, ex, "row-1")
		if err != nil || n != 1 {
			t.Fatal(err, n)
		}
		n, err = store.Delete(c, ex, "row-1")
		if err != nil || n != 0 {
			t.Fatal(err, n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteRequiredChildViews(t *testing.T) {
	r := openSQLite(t, "required")
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")
	store := fixtureStore{dialect: QuestionDialect, table: "scoped_records"}

	// A Required child obtains its own executor view bound to the child
	// context and shares the root transaction; uncommitted writes are
	// visible inside the same transaction before commit.
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		if err := store.Insert(c, ex, "parent", 1); err != nil {
			return err
		}
		return m.Within(c, uow.Options{}, func(child context.Context) error {
			childEx, err := Current(child)
			if err != nil {
				return err
			}
			// The root's uncommitted row is visible in the same transaction.
			var seen int
			if err := childEx.QueryRow(child, "SELECT count(*) FROM scoped_records").Scan(&seen); err != nil {
				return err
			}
			if seen != 1 {
				t.Fatalf("same-transaction visibility broken: %d", seen)
			}
			return store.Insert(child, childEx, "child", 2)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil || n != 2 {
			t.Fatalf("required child work not committed: %d %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A failed Required child marks the root rollback-only even when its
	// error is caught; nothing commits.
	var caught error
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := Current(c)
		if err := store.Insert(c, ex, "r1", 1); err != nil {
			return err
		}
		caught = m.Within(c, uow.Options{}, func(child context.Context) error {
			childEx, _ := Current(child)
			if err := store.Insert(child, childEx, "r2", 2); err != nil {
				return err
			}
			return errors.New("child failure")
		})
		return nil
	})
	if caught == nil || err == nil || !errors.Is(err, uow.ErrRollbackOnly) {
		t.Fatalf("child failure not rollback-only: caught=%v root=%v", caught, err)
	}
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil || n != 2 {
			t.Fatalf("rollback-only root committed: %d %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A child view dies with its context: reusing it after the callback
	// fails closed.
	var escaped Executor
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return m.Within(c, uow.Options{}, func(child context.Context) error {
			ex, err := Current(child)
			escaped = ex
			return err
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := escaped.QueryRow(context.Background(), "SELECT 1").Scan(new(int)); !errors.Is(err, uow.ErrClosed) && !errors.Is(err, uow.ErrRequired) && !errors.Is(err, uow.ErrScopeMismatch) {
		t.Fatalf("ended child view usable: %v", err)
	}
	// A read-only child of a read-write root cannot write.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		_ = m.Within(c, uow.Options{Mode: uow.ReadOnly}, func(child context.Context) error {
			childEx, _ := Current(child)
			_, err := childEx.Exec(child, "INSERT INTO scoped_records(tenant_id,scope_kind,scope_id,id,value) VALUES('tenant-a','tenant','tenant-a','bad',1)")
			return err
		})
		return nil
	})
	if !errors.Is(err, uow.ErrReadOnly) && !errors.Is(err, uow.ErrTransactionRequired) {
		t.Fatalf("read-only child wrote: %v", err)
	}
}

func TestSQLiteRequiresNewSemantics(t *testing.T) {
	r := openSQLite(t, "requiresnew")
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")
	store := fixtureStore{dialect: QuestionDialect, table: "scoped_records"}

	// RequiresNew opens an independent connection/transaction without mutating
	// or suspending the parent view. Each view remains usable with its own
	// context and connection.
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		parentEx, _ := Current(c)
		err := m.Within(c, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(child context.Context) error {
			var parentProbe int
			if err := parentEx.QueryRow(c, "SELECT 1").Scan(&parentProbe); err != nil || parentProbe != 1 {
				t.Fatalf("parent view unusable during RequiresNew: %d %v", parentProbe, err)
			}
			childEx, err := Current(child)
			if err != nil {
				return err
			}
			return store.Insert(child, childEx, "inner", 2)
		})
		if err != nil {
			return err
		}
		// The parent view continues after the child completes and sees the
		// child's committed row (a new statement starts a new snapshot).
		var seen int
		if err := parentEx.QueryRow(c, "SELECT count(*) FROM scoped_records").Scan(&seen); err != nil {
			return err
		}
		if seen != 1 {
			t.Fatalf("child commit not visible to parent: %d", seen)
		}
		return store.Insert(c, parentEx, "outer", 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Mixing a child context into a parent executor view fails closed with
	// ErrScopeMismatch and poisons the parent root, while the child commits
	// independently.
	mixErr := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		parentEx, _ := Current(c)
		return m.Within(c, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(child context.Context) error {
			if err := parentEx.QueryRow(child, "SELECT 1").Scan(new(int)); !errors.Is(err, uow.ErrScopeMismatch) {
				t.Fatalf("parent view accepted child context: %v", err)
			}
			return nil
		})
	})
	if !errors.Is(mixErr, uow.ErrRollbackOnly) {
		t.Fatalf("mixed view context did not poison parent: %v", mixErr)
	}
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil || n != 2 {
			t.Fatalf("requiresnew outcomes wrong: %d %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A parent rollback does not undo the independently committed child.
	// The child commits before the parent takes the write lock, so both
	// transactions really run against the real engine.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		if err := m.Within(c, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(child context.Context) error {
			childEx, _ := Current(child)
			return store.Insert(child, childEx, "kept", 5)
		}); err != nil {
			return err
		}
		ex, err := Current(c)
		if err != nil {
			return err
		}
		if err := store.Insert(c, ex, "doomed", 1); err != nil {
			return err
		}
		return errors.New("parent fails after child")
	})
	if err == nil {
		t.Fatal("missing parent failure")
	}
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		if _, err := store.Get(c, ex, "kept"); err != nil {
			t.Fatalf("independent child lost: %v", err)
		}
		if _, err := store.Get(c, ex, "doomed"); !errors.Is(err, ErrNoRows) {
			t.Fatalf("failed parent committed: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// SQLite is a single-writer engine: a child write while the parent
	// holds the write lock fails closed with a real lock error instead of
	// silently succeeding. This is a truthful backend boundary.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := Current(c)
		if err := store.Insert(c, ex, "lock-holder", 1); err != nil {
			return err
		}
		child := m.Within(c, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(child context.Context) error {
			childEx, _ := Current(child)
			return store.Insert(child, childEx, "lock-waiter", 1)
		})
		if child == nil {
			t.Fatal("child write under parent write lock silently succeeded")
		}
		var chain strings.Builder
		for un := child; un != nil; un = errors.Unwrap(un) {
			chain.WriteString(un.Error())
			chain.WriteString(";")
		}
		lowered := strings.ToLower(chain.String())
		if !strings.Contains(lowered, "locked") && !strings.Contains(lowered, "busy") {
			t.Fatalf("child write under parent write lock did not fail closed: %s", chain.String())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil || n != 4 {
			t.Fatalf("parent commit after lock conflict wrong: %d %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Depth limit is enforced before any work runs.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return m.Within(c, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(child context.Context) error {
			return m.Within(child, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(grand context.Context) error {
				return m.Within(grand, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(context.Context) error {
					return nil
				})
			})
		})
	})
	if !errors.Is(err, uow.ErrDepthExceeded) {
		t.Fatalf("depth limit not enforced: %v", err)
	}
}

func TestSQLiteGuardsAndCancellation(t *testing.T) {
	r := openSQLite(t, "guards")
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")
	store := fixtureStore{dialect: QuestionDialect, table: "scoped_records"}

	// Read-only view rejects writes: a pooled read view has no
	// transaction, so a write is refused before reaching the driver.
	err := m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		_, err := ex.Exec(c, "INSERT INTO scoped_records(tenant_id,scope_kind,scope_id,id,value) VALUES('tenant-a','tenant','tenant-a','bad',1)")
		return err
	})
	if !errors.Is(err, uow.ErrTransactionRequired) && !errors.Is(err, uow.ErrReadOnly) {
		t.Fatalf("read-only write accepted: %v", err)
	}

	// Missing scope binding refuses any operation.
	err = m.Within(context.Background(), uow.Options{}, func(context.Context) error { return nil })
	if !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("missing scope accepted: %v", err)
	}

	// Expired/foreign scope binding inside an operation is refused.
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		foreign, ferr := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
			return scope.PrincipalRef{ID: "mallory", Kind: "user"}, "tenant-b", true
		}).Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-b", Kind: "tenant", ID: "tenant-b"})
		if ferr != nil {
			return ferr
		}
		if _, gerr := Current(foreign); gerr != nil {
			return gerr
		}
		return errors.New("foreign binding accepted")
	})
	if err == nil {
		t.Fatal("foreign binding accepted")
	}

	// Wrong resource key fails closed inside a foreign root.
	other, oerr := NewSQLResource(r.Pool(), SQLConfig{
		Backend:      "sqlite",
		Key:          "other-key",
		Capabilities: sqliteTestCapabilities(),
		Dialect:      QuestionDialect,
		Isolation:    func(uow.Isolation) (sql.IsolationLevel, error) { return sql.LevelDefault, nil },
	})
	if oerr != nil {
		t.Fatal(oerr)
	}
	otherManager, _ := uow.NewManager(other, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2, DefaultIsolation: uow.Serializable})
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		_ = otherManager.Within(c, uow.Options{}, func(context.Context) error { return nil })
		return nil
	})
	if !errors.Is(err, uow.ErrResourceMismatch) {
		t.Fatalf("second database accepted: %v", err)
	}

	// Unclosed rows keep the session busy; the completion refuses to
	// commit under an active cursor, force-releases the abandoned cursor
	// and rolls the work back.
	var abandoned Rows
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		rows, err := ex.Query(c, "SELECT 1 UNION SELECT 2")
		if err != nil {
			return err
		}
		abandoned = rows // intentionally abandoned cursor
		if _, err := ex.Exec(c, "INSERT INTO scoped_records(tenant_id,scope_kind,scope_id,id,value) VALUES('tenant-a','tenant','tenant-a','leak',1)"); !errors.Is(err, uow.ErrBusy) {
			return err
		}
		return nil
	})
	var failure uow.Error
	if !errors.As(err, &failure) {
		t.Fatalf("busy completion not a uow failure: %v", err)
	}
	if !errors.Is(err, uow.ErrRollbackOnly) || !errors.Is(err, uow.ErrBusy) {
		t.Fatalf("busy completion wrong: %v", err)
	}
	if failure.Outcome() != uow.RolledBack {
		t.Fatalf("busy completion outcome not rolled back: %d", failure.Outcome())
	}
	if abandoned.Next() {
		t.Fatal("abandoned cursor still readable")
	}
	// The connection held by the abandoned cursor was released: the pool
	// reports no in-use connection and the next operation reuses it.
	requireIdlePool(t, r)
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil || n != 0 {
			t.Fatalf("busy cursor leaked commit: %d %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// An abandoned single-row query keeps the session busy exactly like a
	// cursor; the completion force-releases it and rolls the work back,
	// and the leaked row is dead afterwards.
	var leaked Row
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		leaked = ex.QueryRow(c, "SELECT count(*) FROM scoped_records")
		if _, err := ex.Exec(c, "INSERT INTO scoped_records(tenant_id,scope_kind,scope_id,id,value) VALUES('tenant-a','tenant','tenant-a','leak-row',1)"); !errors.Is(err, uow.ErrBusy) {
			return err
		}
		return nil
	})
	if !errors.As(err, &failure) || !errors.Is(err, uow.ErrRollbackOnly) {
		t.Fatalf("leaked row completion wrong: %v", err)
	}
	if failure.Outcome() != uow.RolledBack {
		t.Fatalf("leaked row outcome not rolled back: %d", failure.Outcome())
	}
	if err := leaked.Scan(new(int)); !errors.Is(err, uow.ErrClosed) {
		t.Fatalf("leaked row still scannable: %v", err)
	}
	// The connection of the leaked-row operation was released back to the
	// pool; the next operations reuse it instead of exhausting the pool.
	requireIdlePool(t, r)

	// Cancellation before commit rolls back.
	cancelCtx, cancel := context.WithCancel(ctx)
	err = m.Within(cancelCtx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := Current(c)
		if _, err := ex.Exec(c, "INSERT INTO scoped_records(tenant_id,scope_kind,scope_id,id,value) VALUES('tenant-a','tenant','tenant-a','cancelled',1)"); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not surfaced: %v", err)
	}
	// database/sql ends the canceled transaction and the session returns
	// the reserved connection; nothing keeps the SQLite file locked.
	requireIdlePool(t, r)
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, _ := Current(c)
		n, err := store.Count(c, ex)
		if err != nil || n != 0 {
			t.Fatalf("cancelled work committed: %d %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Statement errors mark the operation rollback-only, preserve the
	// cause and roll the work back.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		_, err = ex.Exec(c, "INSERT INTO not_a_table VALUES(1)")
		return err
	})
	if err == nil {
		t.Fatal("statement error swallowed")
	}
	var causeText strings.Builder
	for un := err; un != nil; un = errors.Unwrap(un) {
		causeText.WriteString(un.Error())
		causeText.WriteString(";")
	}
	if !strings.Contains(causeText.String(), "not_a_table") {
		t.Fatalf("statement error cause lost: %s", causeText.String())
	}
	if !errors.As(err, &failure) {
		t.Fatalf("statement error not a uow failure: %v", err)
	}
	if failure.Outcome() != uow.RolledBack {
		t.Fatalf("statement error outcome wrong: %d", failure.Outcome())
	}

	// A delayed row failure surfaces through Scan: SQLite returns NULL for
	// 1/0 instead of raising a server-side error, so the delayed failure
	// is the conversion error of the poisoned row.
	err = m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		rows, err := ex.Query(c, "SELECT 1/(n-2) FROM (SELECT 1 AS n UNION SELECT 2 UNION SELECT 3)")
		if err != nil {
			return err
		}
		defer rows.Close()
		var scanFailures int
		for rows.Next() {
			var v int
			if err := rows.Scan(&v); err != nil {
				scanFailures++
			}
		}
		if scanFailures == 0 {
			return errors.New("missing delayed row error")
		}
		return rows.Err()
	})
	if err == nil {
		t.Fatal("delayed row error swallowed")
	}
}

func TestSQLiteRowAndCursorViews(t *testing.T) {
	r := openSQLite(t, "views")
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")

	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		// Each Current call returns a fresh view; two views of the same
		// session cannot run statements concurrently.
		first, err := Current(c)
		if err != nil {
			return err
		}
		second, _ := Current(c)
		if first == second {
			t.Fatal("views not fresh")
		}
		row := first.QueryRow(c, "SELECT 42")
		if _, err := second.Query(c, "SELECT 1"); !errors.Is(err, uow.ErrBusy) {
			t.Fatalf("concurrent statement accepted: %v", err)
		}
		var v int
		if err := row.Scan(&v); err != nil || v != 42 {
			return err
		}
		if !errors.Is(row.Scan(&v), uow.ErrClosed) {
			t.Fatal("row scanned twice")
		}
		// A view rejects a different context; the refusal also marks the
		// operation rollback-only, like every guard violation.
		other := context.WithValue(c, struct{ k int }{1}, "x")
		if _, err := second.Query(other, "SELECT 1"); !errors.Is(err, uow.ErrScopeMismatch) {
			t.Fatalf("foreign context accepted: %v", err)
		}
		// Values iterate and cursors report no error when clean.
		rows, err := second.Query(c, "SELECT 7")
		if err != nil {
			return err
		}
		ok := rows.Next()
		if !ok {
			t.Fatal("no rows")
		}
		var n int
		if err := rows.Scan(&n); err != nil || n != 7 {
			t.Fatal(n, err)
		}
		rows.Close()
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
		// Close is idempotent.
		rows.Close()
		// A statement error is returned, not deferred.
		if _, err := second.Query(c, "NOT VALID SQL"); err == nil {
			t.Fatal("invalid SQL accepted")
		}
		return nil
	})
	// The scope mismatch correctly poisoned the root: guard violations
	// mark the operation rollback-only.
	if !errors.Is(err, uow.ErrRollbackOnly) {
		t.Fatalf("scope mismatch did not poison the root: %v", err)
	}
}

func TestSQLiteSessionIdentityAndCapabilities(t *testing.T) {
	r := openSQLite(t, "identity")
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")

	// The resource reports truthful capabilities and its dialect.
	if r.Capabilities().Backend != "sqlite" || r.Capabilities().RowLevelSecurity {
		t.Fatalf("capabilities untruthful: %+v", r.Capabilities())
	}
	if r.Dialect().Placeholder(1) != "?" {
		t.Fatal("dialect wrong")
	}
	if r.RequiresTransaction() {
		t.Fatal("sqlite must not require transactions for reads")
	}
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		// The executor view reports the same database identity as the
		// resource, so CheckResource validated the right key.
		if ex.DatabaseKey() != r.DatabaseKey() {
			t.Fatalf("view key differs: %s != %s", ex.DatabaseKey(), r.DatabaseKey())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteExplicitWriteReturningGuards(t *testing.T) {
	r := openSQLite(t, "write-returning")
	m := newManager(t, r)
	if _, err := r.Pool().Exec(`CREATE TABLE returned_values(id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	ctx := trustedContext(t, "tenant-a")
	insertReturning := `INSERT INTO returned_values(value) VALUES(?) RETURNING id`
	count := func() int {
		var n int
		if err := r.Pool().QueryRow(`SELECT count(*) FROM returned_values`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	t.Run("readonly-root-rejects-before-sql", func(t *testing.T) {
		err := m.Within(ctx, uow.Options{}, func(work context.Context) error {
			ex, err := Current(work)
			if err != nil {
				return err
			}
			return ex.QueryRowWrite(work, insertReturning, "root-forbidden").Scan(new(int64))
		})
		if !errors.Is(err, uow.ErrReadOnly) || count() != 0 {
			t.Fatalf("err=%v count=%d", err, count())
		}
	})
	t.Run("readonly-root-rejects-multirow-before-sql", func(t *testing.T) {
		err := m.Within(ctx, uow.Options{}, func(work context.Context) error {
			ex, err := Current(work)
			if err != nil {
				return err
			}
			_, err = ex.QueryWrite(work, `INSERT INTO returned_values(value) VALUES(?),(?) RETURNING id`, "multi-forbidden-a", "multi-forbidden-b")
			return err
		})
		if !errors.Is(err, uow.ErrReadOnly) || count() != 0 {
			t.Fatalf("err=%v count=%d", err, count())
		}
	})
	t.Run("readonly-required-child-rejects-before-sql", func(t *testing.T) {
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			return m.Within(work, uow.Options{Mode: uow.ReadOnly}, func(child context.Context) error {
				ex, err := Current(child)
				if err != nil {
					return err
				}
				return ex.QueryRowWrite(child, insertReturning, "child-forbidden").Scan(new(int64))
			})
		})
		if !errors.Is(err, uow.ErrReadOnly) || count() != 0 {
			t.Fatalf("err=%v count=%d", err, count())
		}
	})
	t.Run("readwrite-returning-commits", func(t *testing.T) {
		var id int64
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			ex, err := Current(work)
			if err != nil {
				return err
			}
			return ex.QueryRowWrite(work, insertReturning, "committed").Scan(&id)
		})
		if err != nil || id == 0 || count() != 1 {
			t.Fatalf("err=%v id=%d count=%d", err, id, count())
		}
	})
	t.Run("readwrite-returning-rolls-back", func(t *testing.T) {
		cause := errors.New("force rollback after RETURNING")
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			ex, err := Current(work)
			if err != nil {
				return err
			}
			if err := ex.QueryRowWrite(work, insertReturning, "rolled-back").Scan(new(int64)); err != nil {
				return err
			}
			return cause
		})
		if !errors.Is(err, cause) || count() != 1 {
			t.Fatalf("err=%v count=%d", err, count())
		}
	})
	t.Run("readwrite-multirow-returning-commits", func(t *testing.T) {
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			ex, err := Current(work)
			if err != nil {
				return err
			}
			rows, err := ex.QueryWrite(work, `INSERT INTO returned_values(value) VALUES(?),(?) RETURNING id`, "multi-a", "multi-b")
			if err != nil {
				return err
			}
			defer rows.Close()
			seen := 0
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					return err
				}
				seen++
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if seen != 2 {
				return errors.New("write-returning row count mismatch")
			}
			return nil
		})
		if err != nil || count() != 3 {
			t.Fatalf("err=%v count=%d", err, count())
		}
	})
}

func TestSQLitePooledReads(t *testing.T) {
	r := openSQLite(t, "pooled")
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")
	store := fixtureStore{dialect: QuestionDialect, table: "scoped_records"}

	// Without an RLS requirement, read-only operations run non-
	// transactional pooled statements; writes still demand a transaction.
	err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := Current(c)
		return store.Insert(c, ex, "seed", 3)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Within(ctx, uow.Options{Mode: uow.ReadOnly, Transaction: uow.NonTransactional}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		v, err := store.Get(c, ex, "seed")
		if err != nil || v != 3 {
			t.Fatalf("pooled read wrong: %d %v", v, err)
		}
		rows, err := ex.Query(c, "SELECT id FROM scoped_records")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			if id != "seed" {
				t.Fatalf("pooled cursor wrong: %s", id)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteConcurrentRequiresNewAndDetach(t *testing.T) {
	r := openSQLite(t, "concurrent-requiresnew")
	r.Pool().SetMaxOpenConns(8)
	setupFixture(t, r)
	m := newManager(t, r)
	ctx := trustedContext(t, "tenant-a")
	store := fixtureStore{dialect: QuestionDialect, table: "scoped_records"}

	// Seed rows inside a committed write UOW.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		for i := range 6 {
			if err := store.Insert(c, ex, fmt.Sprintf("rec-%d", i), int64(i+10)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Multiple goroutines concurrently execute RequiresNew under a single
	// active parent UOW sharing the same stateless repository (store), while
	// the parent also uses its own connection.
	const workers = 6
	err := m.Within(ctx, uow.Options{Mode: uow.ReadOnly, Transaction: uow.Transactional}, func(parent context.Context) error {
		parentEx, err := Current(parent)
		if err != nil {
			return err
		}
		ready := make(chan struct{}, workers)
		release := make(chan struct{})
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for i := range workers {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				e := m.Within(parent, uow.Options{Mode: uow.ReadOnly, Transaction: uow.Transactional, Propagation: uow.RequiresNew}, func(child context.Context) error {
					childEx, err := Current(child)
					if err != nil {
						return err
					}
					ready <- struct{}{}
					<-release
					got, err := store.Get(child, childEx, fmt.Sprintf("rec-%d", idx))
					if err != nil {
						return err
					}
					if got != int64(idx+10) {
						return fmt.Errorf("worker %d got %d, want %d", idx, got, idx+10)
					}
					return nil
				})
				if e != nil {
					errs <- e
				}
			}(i)
		}
		for range workers {
			<-ready
		}
		// Parent remains usable on its own connection while children hold theirs.
		n, err := store.Count(parent, parentEx)
		if err != nil || n != workers {
			close(release)
			return fmt.Errorf("parent count = (%d, %v)", n, err)
		}
		close(release)
		wg.Wait()
		close(errs)
		for e := range errs {
			return e
		}

		// 2. uow.Detach clears both UOW scope and database.executorVar while
		// preserving ScopeBinding so parallel goroutines can start fresh
		// top-level UOWs cleanly.
		detached := uow.Detach(parent)
		if _, ok := executorVar.Current(detached); ok {
			return errors.New("Detach did not clear database executorVar")
		}
		if _, err := Current(detached); !errors.Is(err, uow.ErrRequired) {
			return fmt.Errorf("Current(detached) = %v, want ErrRequired", err)
		}
		detachErrs := make(chan error, workers)
		for i := range workers {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				e := m.Within(detached, uow.Options{Mode: uow.ReadOnly, Transaction: uow.Transactional}, func(top context.Context) error {
					topEx, err := Current(top)
					if err != nil {
						return err
					}
					got, err := store.Get(top, topEx, fmt.Sprintf("rec-%d", idx))
					if err != nil || got != int64(idx+10) {
						return fmt.Errorf("detached worker %d = (%d, %v)", idx, got, err)
					}
					return nil
				})
				if e != nil {
					detachErrs <- e
				}
			}(i)
		}
		wg.Wait()
		close(detachErrs)
		for e := range detachErrs {
			return e
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Single-connection overlap protection stays enforced when sharing the
	// SAME UOW without RequiresNew/Detach.
	busyErr := m.Within(ctx, uow.Options{Mode: uow.ReadOnly, Transaction: uow.Transactional}, func(c context.Context) error {
		ex, err := Current(c)
		if err != nil {
			return err
		}
		rows, err := ex.Query(c, "SELECT id FROM scoped_records")
		if err != nil {
			return err
		}
		defer rows.Close()
		other := make(chan error, 1)
		go func() {
			other <- ex.QueryRow(c, "SELECT 1").Scan(new(int))
		}()
		if e := <-other; !errors.Is(e, uow.ErrBusy) {
			return fmt.Errorf("same-UOW concurrent SQL = %v, want ErrBusy", e)
		}
		return nil
	})
	if !errors.Is(busyErr, uow.ErrRollbackOnly) {
		t.Fatalf("same-UOW busy did not poison root: %v", busyErr)
	}
}
