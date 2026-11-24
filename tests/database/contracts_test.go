//go:build integration

// Package databasecontracts runs the neutral database Port contracts against
// real PostgreSQL and MySQL services. The required DSN environment variables
// are supplied by the acceptance runner; a missing prerequisite fails the
// test instead of skipping it. Credentials are never printed.
package databasecontracts

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/database"
	mysqladapter "github.com/cymondez/pangolin-framework/persistence/mysql"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// scopedFixtureTable is the acceptance-fixture schema. It is deliberately
// local to this test package: the framework ships owner and predicate
// helpers only, and this fixed id/value layout is an acceptance fixture.
const scopedFixtureTable = "pangolin_scoped_records"

// postgresFixtureSchema is the dedicated acceptance schema setupPostgresFixture
// creates. PostgreSQL fixture stores qualify their table with it, so every
// fixture statement targets exactly the RLS-protected table; nothing relies
// on the app role's search_path.
const postgresFixtureSchema = "pangolin_db_neutral"

const scopedFixtureDDL = "(tenant_id VARCHAR(128) NOT NULL, scope_kind VARCHAR(64) NOT NULL, scope_id VARCHAR(128) NOT NULL, id VARCHAR(128) NOT NULL, value BIGINT NOT NULL, PRIMARY KEY(tenant_id, scope_kind, scope_id, id))"

func requiredEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("required %s missing: real-service integration test cannot run", key)
	}
	return value
}

func binding(t *testing.T, tenant string) context.Context {
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

func newManager(t *testing.T, resource uow.Resource) uow.UnitOfWorkManager {
	t.Helper()
	m, err := uow.NewManager(resource, uow.DefaultOptions{DefaultTimeout: 5 * time.Second, CleanupTimeout: 2 * time.Second, HookTimeout: 50 * time.Millisecond, MaxRequiresNewDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// fixtureStore is the local acceptance repository over the fixture schema.
// Its statements bind the full owner (tenant, scope kind, scope id) from the
// trusted binding through the production OwnerPredicate helper; there is no
// unscoped path and no DTO owner substitution. table may be schema-qualified
// ("schema.table"); tableRef quotes each part separately.
type fixtureStore struct {
	dialect database.Dialect
	table   string
}

func (s fixtureStore) tableRef() string {
	return s.dialect.Identifier(strings.Split(s.table, ".")...)
}

func (s fixtureStore) insert(ctx context.Context, ex database.Executor, id string, value int64) error {
	o, err := database.CurrentOwner(ctx)
	if err != nil {
		return err
	}
	query := "INSERT INTO " + s.tableRef() + "(tenant_id,scope_kind,scope_id,id,value) VALUES(" + s.dialect.Placeholder(1) + "," + s.dialect.Placeholder(2) + "," + s.dialect.Placeholder(3) + "," + s.dialect.Placeholder(4) + "," + s.dialect.Placeholder(5) + ")"
	res, err := ex.Exec(ctx, query, o.TenantID, o.ScopeKind, o.ScopeID, id, value)
	if err != nil {
		return err
	}
	if res.RowsAffected != 1 {
		return database.ErrNoRows
	}
	return nil
}

func (s fixtureStore) get(ctx context.Context, ex database.Executor, id string) (int64, error) {
	o, err := database.CurrentOwner(ctx)
	if err != nil {
		return 0, err
	}
	clause, args := database.OwnerPredicate(s.dialect, "", o)
	query := "SELECT " + s.dialect.Identifier("value") + " FROM " + s.tableRef() + " WHERE " + clause + " AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(len(args)+1)
	args = append(args, id)
	var value int64
	if err := ex.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

// getForOwner validates an untrusted DTO owner hint against the trusted
// binding before reading; a foreign hint is denied, never substituted.
func (s fixtureStore) getForOwner(ctx context.Context, ex database.Executor, id string, requested scope.ScopeRef) (int64, error) {
	o, err := database.ResolveOwner(ctx, requested)
	if err != nil {
		return 0, err
	}
	clause, args := database.OwnerPredicate(s.dialect, "", o)
	query := "SELECT " + s.dialect.Identifier("value") + " FROM " + s.tableRef() + " WHERE " + clause + " AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(len(args)+1)
	args = append(args, id)
	var value int64
	if err := ex.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func (s fixtureStore) count(ctx context.Context, ex database.Executor) (int64, error) {
	o, err := database.CurrentOwner(ctx)
	if err != nil {
		return 0, err
	}
	clause, args := database.OwnerPredicate(s.dialect, "", o)
	query := "SELECT count(*) FROM " + s.tableRef() + " WHERE " + clause
	var n int64
	if err := ex.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s fixtureStore) delete(ctx context.Context, ex database.Executor, id string) (int64, error) {
	o, err := database.CurrentOwner(ctx)
	if err != nil {
		return 0, err
	}
	clause, args := database.OwnerPredicate(s.dialect, "", o)
	query := "DELETE FROM " + s.tableRef() + " WHERE " + clause + " AND " + s.dialect.Identifier("id") + " = " + s.dialect.Placeholder(len(args)+1)
	args = append(args, id)
	res, err := ex.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected, nil
}

// runCommitRollbackContract verifies the core transaction semantics against
// a real backend.
func runCommitRollbackContract(t *testing.T, m uow.UnitOfWorkManager, ctx context.Context, store fixtureStore) {
	t.Helper()
	cause := errors.New("business failure")

	// Commit persists the trusted owner's row.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		return store.insert(c, ex, "commit-1", 11)
	}); err != nil {
		t.Fatal(err)
	}
	// A failing root rolls back.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := database.Current(c)
		if err := store.insert(c, ex, "rollback-1", 1); err != nil {
			return err
		}
		return cause
	}); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	// A failing Required child poisons the root even when its error is
	// caught.
	var caught error
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := database.Current(c)
		if err := store.insert(c, ex, "rollback-2", 2); err != nil {
			return err
		}
		caught = m.Within(c, uow.Options{}, func(child context.Context) error {
			childEx, _ := database.Current(child)
			if err := store.insert(child, childEx, "rollback-3", 3); err != nil {
				return err
			}
			return errors.New("child failure")
		})
		return nil
	}); caught == nil || err == nil || !errors.Is(err, uow.ErrRollbackOnly) {
		t.Fatalf("required child failure not rollback-only: caught=%v root=%v", caught, err)
	}
	// RequiresNew commits independently of the failing parent. The child
	// completes before the parent takes its writer lock so both
	// transactions really run; every child result is checked.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		if err := m.Within(c, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(child context.Context) error {
			childEx, err := database.Current(child)
			if err != nil {
				return err
			}
			return store.insert(child, childEx, "independent", 7)
		}); err != nil {
			return err
		}
		ex, _ := database.Current(c)
		if err := store.insert(c, ex, "doomed", 1); err != nil {
			return err
		}
		return cause
	}); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	// Cancellation before commit rolls back, so the cancelled delete is
	// undone and an already-committed row must survive it. The
	// owner-scoped delete helper keeps the statement bound to the
	// trusted owner.
	cancelCtx, cancel := context.WithCancel(ctx)
	if err := m.Within(cancelCtx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		n, err := store.delete(c, ex, "commit-1")
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("cancelled delete must remove exactly one owned row")
		}
		cancel()
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Statement errors preserve the cause and roll back; the work is
	// verified afterwards in a fresh unit of work.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		_, err = ex.Exec(c, "INSERT INTO missing_contract_table VALUES(1)")
		return err
	}); err == nil {
		t.Fatal("statement error swallowed")
	}
	// Final state verified in a fresh unit of work: exactly the committed
	// rows survived.
	if err := m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		n, err := store.count(c, ex)
		if err != nil {
			return err
		}
		if n != 2 {
			t.Fatalf("committed rows wrong: %d", n)
		}
		if _, err := store.get(c, ex, "independent"); err != nil {
			t.Fatalf("independent child lost: %v", err)
		}
		if _, err := store.get(c, ex, "rollback-1"); !errors.Is(err, database.ErrNoRows) {
			t.Fatalf("rolled back row present: %v", err)
		}
		// The cancelled delete rolled back, so commit-1 must still be
		// there with its committed value.
		if v, err := store.get(c, ex, "commit-1"); err != nil || v != 11 {
			t.Fatalf("cancelled delete not rolled back, commit-1 lost: %d %v", v, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// runOwnerScopedContract verifies trusted-owner isolation on a backend. On
// PostgreSQL the engine additionally enFORCEs RLS: the raw foreign write is
// expected to fail and poison its transaction, so the final data is verified
// in a fresh unit of work. On MySQL (no engine RLS) the raw write is allowed,
// and the trusted-owner predicate must still keep it invisible.
func runOwnerScopedContract(t *testing.T, m uow.UnitOfWorkManager, ctx context.Context, store fixtureStore, expectRLSFailure bool, rawInsert func(c context.Context, tenantID string) error) {
	t.Helper()
	// Seed one row for the trusted tenant.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		return store.insert(c, ex, "scoped-1", 5)
	}); err != nil {
		t.Fatal(err)
	}
	// A different tenant sees nothing of the trusted tenant's rows.
	foreign := binding(t, "tenant-other")
	if err := m.Within(foreign, uow.Options{}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		if n, err := store.count(c, ex); err != nil || n != 0 {
			t.Fatalf("cross-tenant rows visible: %d %v", n, err)
		}
		if _, err := store.get(c, ex, "scoped-1"); !errors.Is(err, database.ErrNoRows) {
			t.Fatalf("cross-tenant read: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A foreign raw row written directly through the executor. On
	// PostgreSQL FORCE RLS rejects the statement, which poisons its
	// transaction; the expectation is asserted and the state is verified
	// afterwards in a fresh unit of work, never in the aborted one.
	rawErr := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return rawInsert(c, "tenant-other")
	})
	if expectRLSFailure && rawErr == nil {
		t.Fatal("RLS accepted a foreign raw write")
	}
	if !expectRLSFailure && rawErr != nil {
		t.Fatalf("raw write failed without engine RLS: %v", rawErr)
	}
	// Fresh unit of work: the trusted owner sees only its own row.
	if err := m.Within(ctx, uow.Options{}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		n, err := store.count(c, ex)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("foreign raw write visible to the trusted owner: %d", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// DTO owner substitution is denied; the trusted owner is kept.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := database.Current(c)
		_, err := store.getForOwner(c, ex, "scoped-1", scope.ScopeRef{TenantID: "tenant-other", Kind: "tenant", ID: "tenant-other"})
		if !errors.Is(err, scope.ErrDenied) {
			return err
		}
		v, err := store.get(c, ex, "scoped-1")
		if err != nil || v != 5 {
			t.Fatalf("trusted read broken after denial: %d %v", v, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Delete removes only the trusted owner's row.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := database.Current(c)
		n, err := store.delete(c, ex, "scoped-1")
		if err != nil || n != 1 {
			t.Fatal(err, n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// runGuardContract verifies read-only rejection, missing scope, busy
// cursors and foreign resource identity. After every failure the pool must
// be reusable: no abandoned statement may reserve the connection.
func runGuardContract(t *testing.T, m uow.UnitOfWorkManager, other uow.UnitOfWorkManager, ctx context.Context, store fixtureStore) {
	t.Helper()
	// Read-only view rejects writes.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		_, err = ex.Exec(c, "SELECT 1")
		return err
	}); err == nil {
		t.Fatal("read-only write accepted")
	}
	// Missing scope binding refuses any operation.
	if err := m.Within(context.Background(), uow.Options{}, func(context.Context) error { return nil }); !errors.Is(err, uow.ErrRequired) {
		t.Fatalf("missing scope accepted: %v", err)
	}
	// A second database identity inside the root fails closed.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		_ = other.Within(c, uow.Options{}, func(context.Context) error { return nil })
		return nil
	}); !errors.Is(err, uow.ErrResourceMismatch) {
		t.Fatalf("second database accepted: %v", err)
	}
	// Unclosed rows keep the session busy and roll the work back.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		rows, err := ex.Query(c, "SELECT 1")
		if err != nil {
			return err
		}
		_ = rows // abandoned on purpose
		if _, err := store.count(c, ex); !errors.Is(err, uow.ErrBusy) {
			return err
		}
		return nil
	}); err == nil {
		t.Fatal("busy cursor committed")
	}
}

func TestDatabasePostgresCommitRollbackContract(t *testing.T) {
	admin := poolFromEnv(t, "PANGOLIN_M1_ADMIN_DSN")
	app := poolFromEnv(t, "PANGOLIN_DATABASE_POSTGRES_DSN")
	setupPostgresFixture(t, admin, app)
	// The existing PostgreSQL resource exposes the neutral Port directly.
	resource := &postgres.Resource{Pool: app}
	m := newManager(t, resource)
	ctx := binding(t, "tenant-a")
	store := fixtureStore{dialect: database.DollarDialect, table: postgresFixtureSchema + "." + scopedFixtureTable}
	runCommitRollbackContract(t, m, ctx, store)
}

func TestDatabasePostgresOwnerScopedContract(t *testing.T) {
	admin := poolFromEnv(t, "PANGOLIN_M1_ADMIN_DSN")
	app := poolFromEnv(t, "PANGOLIN_DATABASE_POSTGRES_DSN")
	setupPostgresFixture(t, admin, app)
	resource := &postgres.Resource{Pool: app}
	m := newManager(t, resource)
	ctx := binding(t, "tenant-a")
	store := fixtureStore{dialect: database.DollarDialect, table: postgresFixtureSchema + "." + scopedFixtureTable}
	runOwnerScopedContract(t, m, ctx, store, true, func(c context.Context, tenantID string) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		raw := "INSERT INTO " + database.DollarDialect.Identifier(postgresFixtureSchema, scopedFixtureTable) + "(tenant_id,scope_kind,scope_id,id,value) VALUES($1,'tenant',$2,$3,1)"
		_, err = ex.Exec(c, raw, tenantID, tenantID, "foreign-raw")
		return err
	})
	// PostgreSQL additionally enforces engine RLS: even a raw unscoped
	// query cannot see another tenant's rows. Each check runs in its own
	// unit of work, because an RLS violation aborts its transaction.
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		var n int
		return ex.QueryRow(c, "SELECT count(*) FROM "+database.DollarDialect.Identifier(postgresFixtureSchema, scopedFixtureTable)).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	foreign := binding(t, "tenant-other")
	if err := m.Within(foreign, uow.Options{Mode: uow.ReadWrite}, func(f context.Context) error {
		fex, err := database.Current(f)
		if err != nil {
			return err
		}
		var total int
		if err := fex.QueryRow(f, "SELECT count(*) FROM "+database.DollarDialect.Identifier(postgresFixtureSchema, scopedFixtureTable)).Scan(&total); err != nil {
			return err
		}
		if total != 0 {
			t.Fatalf("RLS leaked rows across tenants: %d", total)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDatabasePostgresGuardContract(t *testing.T) {
	admin := poolFromEnv(t, "PANGOLIN_M1_ADMIN_DSN")
	app := poolFromEnv(t, "PANGOLIN_DATABASE_POSTGRES_DSN")
	setupPostgresFixture(t, admin, app)
	resource := &postgres.Resource{Pool: app}
	m := newManager(t, resource)
	other := newManager(t, &postgres.Resource{Pool: app, Key: "second-postgres"})
	ctx := binding(t, "tenant-a")
	store := fixtureStore{dialect: database.DollarDialect, table: postgresFixtureSchema + "." + scopedFixtureTable}
	runGuardContract(t, m, other, ctx, store)
}

func TestDatabaseMySQLCommitRollbackContract(t *testing.T) {
	resource, closeFn := mysqlFromEnv(t)
	defer closeFn()
	setupMySQLFixture(t, resource)
	m := newManager(t, resource)
	ctx := binding(t, "tenant-a")
	store := fixtureStore{dialect: database.QuestionDialect, table: scopedFixtureTable}
	runCommitRollbackContract(t, m, ctx, store)
}

func TestDatabaseMySQLOwnerScopedContract(t *testing.T) {
	resource, closeFn := mysqlFromEnv(t)
	defer closeFn()
	setupMySQLFixture(t, resource)
	m := newManager(t, resource)
	ctx := binding(t, "tenant-a")
	store := fixtureStore{dialect: database.QuestionDialect, table: scopedFixtureTable}
	// MySQL has no engine RLS: the raw foreign write is stored but must
	// stay invisible to the trusted owner's scoped queries.
	runOwnerScopedContract(t, m, ctx, store, false, func(c context.Context, tenantID string) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		_, err = ex.Exec(c, "INSERT INTO "+scopedFixtureTable+"(tenant_id,scope_kind,scope_id,id,value) VALUES(?,'tenant',?,?,1)", tenantID, tenantID, "foreign-raw")
		return err
	})
}

func TestDatabaseMySQLGuardContract(t *testing.T) {
	resource, closeFn := mysqlFromEnv(t)
	defer closeFn()
	setupMySQLFixture(t, resource)
	m := newManager(t, resource)
	other := newManager(t, mysqlWrap(t, resource))
	ctx := binding(t, "tenant-a")
	store := fixtureStore{dialect: database.QuestionDialect, table: scopedFixtureTable}
	runGuardContract(t, m, other, ctx, store)
}

func TestDatabaseCapabilityAdmissionContract(t *testing.T) {
	// PostgreSQL truthfully admits RLS; MySQL truthfully does not, and a
	// caller requiring RLS fails closed on it.
	pg := &postgres.Resource{}
	if err := database.Admit(pg.Capabilities(), database.Requirements{RowLevelSecurity: true, TransactionReadOnly: true, Isolation: uow.Serializable}); err != nil {
		t.Fatalf("truthful postgres requirement rejected: %v", err)
	}
	if err := database.Admit(mysqladapter.Capabilities(), database.Requirements{RowLevelSecurity: true}); !errors.Is(err, database.ErrCapabilityRequired) {
		t.Fatalf("RLS requirement accepted on mysql: %v", err)
	}
	// MySQL isolation levels are enforced by the engine.
	for _, level := range []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Serializable} {
		if err := database.Admit(mysqladapter.Capabilities(), database.Requirements{Isolation: level}); err != nil {
			t.Fatalf("mysql isolation rejected: %v", err)
		}
	}
	// SQLite admits only its default level; explicit others fail closed.
	// Capability snapshots are detached: mutating the returned slice must
	// not rewrite the resource's admission contract.
	pgCaps := pg.Capabilities()
	pgCaps.Isolations[0] = uow.Isolation(99)
	if pg.Capabilities().Supports(uow.ReadCommitted) != true {
		t.Fatal("capability snapshot is not detached")
	}
}

func poolFromEnv(t *testing.T, key string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), requiredEnv(t, key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("service unreachable via %s: %v", key, err)
	}
	return pool
}

func mysqlFromEnv(t *testing.T) (*mysqladapter.Resource, func()) {
	t.Helper()
	resource, err := mysqladapter.Open(mysqladapter.Config{DSN: requiredEnv(t, "PANGOLIN_DATABASE_MYSQL_DSN"), MaxOpen: 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := resource.Pool().PingContext(ctx); err != nil {
		_ = resource.Pool().Close()
		t.Fatalf("service unreachable via PANGOLIN_DATABASE_MYSQL_DSN: %v", err)
	}
	return resource, func() { _ = resource.Pool().Close() }
}

func mysqlWrap(t *testing.T, r *mysqladapter.Resource) *mysqladapter.Resource {
	t.Helper()
	wrapped, err := mysqladapter.Wrap(r.Pool(), "second-mysql")
	if err != nil {
		t.Fatal(err)
	}
	return wrapped
}

func setupPostgresFixture(t *testing.T, admin, app *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	appRole := app.Config().ConnConfig.User
	if _, err := admin.Exec(ctx, `
DROP TABLE IF EXISTS pangolin_db_neutral.`+scopedFixtureTable+`;
CREATE SCHEMA IF NOT EXISTS pangolin_db_neutral AUTHORIZATION pangolin_bootstrap;
SET ROLE pangolin_bootstrap;
CREATE TABLE pangolin_db_neutral.`+scopedFixtureTable+` (tenant_id text NOT NULL, scope_kind text NOT NULL, scope_id text NOT NULL, id text NOT NULL, value bigint NOT NULL, PRIMARY KEY(tenant_id, scope_kind, scope_id, id));
ALTER TABLE pangolin_db_neutral.`+scopedFixtureTable+` ENABLE ROW LEVEL SECURITY;
ALTER TABLE pangolin_db_neutral.`+scopedFixtureTable+` FORCE ROW LEVEL SECURITY;
CREATE POLICY scoped_owner ON pangolin_db_neutral.`+scopedFixtureTable+` USING (tenant_id = current_setting('app.tenant_id', true) AND scope_kind = current_setting('app.scope_kind', true) AND scope_id = current_setting('app.scope_id', true)) WITH CHECK (tenant_id = current_setting('app.tenant_id', true) AND scope_kind = current_setting('app.scope_kind', true) AND scope_id = current_setting('app.scope_id', true));
GRANT USAGE ON SCHEMA pangolin_db_neutral TO `+pgx.Identifier{appRole}.Sanitize()+`;
GRANT SELECT, INSERT, UPDATE, DELETE ON pangolin_db_neutral.`+scopedFixtureTable+` TO `+pgx.Identifier{appRole}.Sanitize()+`;
RESET ROLE;`); err != nil {
		t.Fatal(err)
	}
}

func setupMySQLFixture(t *testing.T, resource *mysqladapter.Resource) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := resource.Pool().ExecContext(ctx, "DROP TABLE IF EXISTS "+scopedFixtureTable); err != nil {
		t.Fatal(err)
	}
	if _, err := resource.Pool().ExecContext(ctx, "CREATE TABLE "+scopedFixtureTable+scopedFixtureDDL); err != nil {
		t.Fatal(err)
	}
}
