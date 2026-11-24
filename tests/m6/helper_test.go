//go:build integration

// Package m6 hosts the executable A72 (REL-005) release drills. Every drill
// owns an isolated, uniquely named database that the test itself creates and
// drops — including failure paths — and writes machine-readable evidence for
// scripts/m6_drills.py to validate. The tests never touch shared fixture data
// and rely on the same PANGOLIN_M1_* connection variables as earlier suites.
package m6

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	eventpg "github.com/cymondez/pangolin-framework/events/postgres"
	"github.com/cymondez/pangolin-framework/modules/organization"
	"github.com/cymondez/pangolin-framework/persistence/idempotency"
	"github.com/cymondez/pangolin-framework/persistence/migration"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/platform/audit"
	jobpg "github.com/cymondez/pangolin-framework/platform/jobs/postgres"
	"github.com/cymondez/pangolin-framework/security/scope"
	tenantpg "github.com/cymondez/pangolin-framework/tenancy/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	adminDSNEnv  = "PANGOLIN_M1_ADMIN_DSN"
	appDSNEnv    = "PANGOLIN_M1_APP_DSN"
	workerDSNEnv = "PANGOLIN_M1_WORKER_DSN"
	natsURLEnv   = "PANGOLIN_M1_NATS_URL"
	issuerEnv    = "PANGOLIN_M1_ISSUER"
	evidenceEnv  = "PANGOLIN_M6_EVIDENCE_DIR"
)

// currentDeploymentVersion is the highest applied migration version of a
// complete v0.1.0 deployment (framework capacity migrations).
const currentDeploymentVersion int64 = 4000002

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func requireEnv(t *testing.T, name string) string {
	t.Helper()
	if value := os.Getenv(name); value != "" {
		return value
	}
	t.Fatalf("required environment variable %s missing", name)
	return ""
}

func uniqueName(prefix, label string) string {
	return prefix + "_" + label + "_" + strings.ToLower(rand.Text())
}

// moduleRoot is the repository/module root that hosts this test run; the
// clean-rebuild drill points the whole test at an exported clean copy.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	must(t, err)
	return root
}

func evidenceDir(t *testing.T) string {
	t.Helper()
	if directory := os.Getenv(evidenceEnv); directory != "" {
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(moduleRoot(t), directory)
		}
		must(t, os.MkdirAll(directory, 0o755))
		return directory
	}
	return t.TempDir()
}

func writeEvidence(t *testing.T, name string, data any) map[string]any {
	t.Helper()
	raw, err := json.MarshalIndent(data, "", "  ")
	must(t, err)
	must(t, os.WriteFile(filepath.Join(evidenceDir(t), name), raw, 0o600))
	encoded, _ := data.(map[string]any)
	return encoded
}

func dsnWithDatabase(t *testing.T, raw, database string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	must(t, err)
	parsed.Path = "/" + database
	return parsed.String()
}

// drillDatabase is an isolated database owned by exactly one drill test.
type drillDatabase struct {
	name     string
	adminDSN string
}

// openDrillDatabase creates a unique database and registers a guaranteed
// cleanup drop. Fixture roles receive CONNECT for realistic app/worker access.
func openDrillDatabase(t *testing.T, label string) *drillDatabase {
	t.Helper()
	admin := requireEnv(t, adminDSNEnv)
	ctx := context.Background()
	root, err := pgxpool.New(ctx, admin)
	must(t, err)
	name := uniqueName("m6", label)
	_, err = root.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	must(t, err)
	t.Cleanup(func() {
		_, err := root.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		root.Close()
		if err != nil {
			t.Errorf("cleanup drop of %s failed: %v", name, err)
		}
	})
	_, err = root.Exec(ctx, "GRANT CONNECT ON DATABASE "+pgx.Identifier{name}.Sanitize()+" TO pangolin_migrator, pangolin_app, pangolin_worker")
	must(t, err)
	return &drillDatabase{name: name, adminDSN: admin}
}

func (d *drillDatabase) pool(t *testing.T, dsnEnv string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsnWithDatabase(t, requireEnv(t, dsnEnv), d.name))
	must(t, err)
	t.Cleanup(pool.Close)
	must(t, pool.Ping(context.Background()))
	return pool
}

// sqlDB opens the deployment connection (admin role) used by the migration runner.
func (d *drillDatabase) sqlDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsnWithDatabase(t, d.adminDSN, d.name))
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	must(t, db.Ping())
	return db
}

func (d *drillDatabase) adminPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return d.pool(t, adminDSNEnv)
}

func subMigrations(t *testing.T, source fs.FS) fs.FS {
	t.Helper()
	sub, err := fs.Sub(source, "migrations")
	must(t, err)
	return sub
}

// moduleSources is the complete real v0.1.0 framework migration set.
func moduleSources(t *testing.T, withOrganization bool) (map[string]fs.FS, []migration.ModuleMigrations) {
	t.Helper()
	sources := map[string]fs.FS{
		"tenant":      subMigrations(t, tenantpg.Migrations),
		"audit":       subMigrations(t, audit.Migrations),
		"scope":       subMigrations(t, migration.ScopeMigrations),
		"events":      subMigrations(t, eventpg.Migrations),
		"idempotency": subMigrations(t, idempotency.Migrations),
		"jobs":        subMigrations(t, jobpg.Migrations),
	}
	capacity, err := fs.Sub(eventpg.CapacityMigrations, "capacity_migrations")
	must(t, err)
	sources["capacity"] = capacity
	modules := []migration.ModuleMigrations{
		{Module: "tenant", Directory: "tenant"},
		{Module: "audit", Directory: "audit", DependsOn: []string{"tenant"}},
		{Module: "scope", Directory: "scope"},
		{Module: "events", Directory: "events"},
		{Module: "idempotency", Directory: "idempotency"},
		{Module: "jobs", Directory: "jobs", DependsOn: []string{"scope"}},
		{Module: "capacity", Directory: "capacity", DependsOn: []string{"events"}},
	}
	if withOrganization {
		sources["organization"] = organization.Migrations()
		modules = append(modules, migration.ModuleMigrations{Module: "organization", Directory: "organization", DependsOn: []string{"scope", "tenant"}})
	}
	return sources, modules
}

// deployModules applies the real framework migration set and verifies the
// deployment reaches the current version. Migrations run through the same
// admin/deployment connection as every established fixture; app and worker
// roles connect afterwards with their reduced privileges and RLS exposure.
func deployModules(t *testing.T, db *sql.DB, withOrganization bool) {
	t.Helper()
	sources, modules := moduleSources(t, withOrganization)
	source := migration.JoinFS(sources)
	runner, err := migration.NewPostgres(db, source, 0, currentDeploymentVersion)
	must(t, err)
	must(t, runner.Apply(context.Background(), modules))
	compatibility, err := migration.NewPostgres(db, source, currentDeploymentVersion, currentDeploymentVersion)
	must(t, err)
	must(t, compatibility.CheckCompatibility(context.Background()))
}

// installBookstoreSQL installs one of the example Bookstore SQL fixture files
// (the deployment-owned bootstrap) under the pangolin_owner role.
func installBookstoreSQL(t *testing.T, admin *pgxpool.Pool, file string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "examples", "bookstore", "infrastructure", "sql", file))
	must(t, err)
	// New databases do not inherit the bootstrap database's schema grants.
	// Keep deployment ownership with the dedicated owner role before applying
	// the real Bookstore fixture under that role.
	_, err = admin.Exec(context.Background(), "CREATE SCHEMA IF NOT EXISTS pangolin AUTHORIZATION pangolin_owner; GRANT USAGE,CREATE ON SCHEMA pangolin TO pangolin_owner; GRANT USAGE ON SCHEMA pangolin TO pangolin_app,pangolin_worker; GRANT REFERENCES ON ALL TABLES IN SCHEMA pangolin TO pangolin_owner;")
	must(t, err)
	_, err = admin.Exec(context.Background(), "SET ROLE pangolin_owner;"+string(raw)+";RESET ROLE;")
	must(t, err)
}

// bindTenantScope installs a tenant scope binding without a request pipeline,
// matching the M1 integration fixture; the database still enforces RLS from
// the transaction-local settings the UOW applies.
func bindTenantScope(t *testing.T, tenant string, principal string) context.Context {
	t.Helper()
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: principal, Kind: "user"}, tenant, true
	}
	ctx, err := scope.NewTenantResolver(identity).Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant})
	must(t, err)
	return ctx
}

func drillManager(t *testing.T, pool *pgxpool.Pool) uow.UnitOfWorkManager {
	t.Helper()
	manager, err := uow.NewManager(&postgres.Resource{Pool: pool}, uow.DefaultOptions{DefaultTimeout: 5 * time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 4})
	must(t, err)
	return manager
}

// freshQuery runs one statement on a brand-new connection. Post-operation
// reconciliation must not reuse pooled writer connections.
func freshQuery(t *testing.T, dsn, query string, scan func(pgx.Row) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if err := scan(conn.QueryRow(ctx, query)); err != nil {
		t.Fatal(err)
	}
}

func (d *drillDatabase) freshRow(t *testing.T, query string, scan func(pgx.Row) error) {
	t.Helper()
	freshQuery(t, dsnWithDatabase(t, d.adminDSN, d.name), query, scan)
}

// scopedSession executes fn inside one transaction with the exact
// transaction-local settings the UOW installs, under the given fixture role.
// commit selects between a committed write session and a rolled-back probe.
func scopedSession(t *testing.T, dsnEnv, database, tenant, kind, id string, commit bool, fn func(pgx.Tx) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsnWithDatabase(t, requireEnv(t, dsnEnv), database))
	must(t, err)
	defer conn.Close(ctx)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	must(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('pangolin.tenant_id',$1,true),set_config('app.tenant_id',$1,true),set_config('app.scope_kind',$2,true),set_config('app.scope_id',$3,true)", tenant, kind, id)
	must(t, err)
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if commit {
		must(t, tx.Commit(ctx))
	} else {
		must(t, tx.Rollback(ctx))
	}
}

type tableState struct {
	Count    int64  `json:"count"`
	Checksum string `json:"checksum"`
}

// tableInventory counts and checksums every table of the drill database in one
// fresh connection. row_to_json rendering is stable on a fixed server and the
// aggregate ORDER BY makes the digest deterministic.
func (d *drillDatabase) tableInventory(t *testing.T) map[string]tableState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsnWithDatabase(t, d.adminDSN, d.name))
	must(t, err)
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind='r' AND n.nspname LIKE 'pangolin%' ORDER BY 1,2`)
	must(t, err)
	var names [][2]string
	for rows.Next() {
		var schema, table string
		must(t, rows.Scan(&schema, &table))
		names = append(names, [2]string{schema, table})
	}
	must(t, rows.Err())
	inventory := map[string]tableState{}
	for _, name := range names {
		var state tableState
		must(t, conn.QueryRow(ctx, "SELECT count(*),COALESCE(md5(string_agg(row_to_json(t)::text,'|' ORDER BY row_to_json(t)::text)),md5('')) FROM "+pgx.Identifier{name[0], name[1]}.Sanitize()+" t").Scan(&state.Count, &state.Checksum))
		inventory[name[0]+"."+name[1]] = state
	}
	return inventory
}

func (d *drillDatabase) appliedVersions(t *testing.T) []int64 {
	t.Helper()
	var versions []int64
	// Goose's initial version 0 marker is metadata, not an applied migration.
	d.freshRow(t, "SELECT COALESCE(array_agg(version_id ORDER BY version_id),'{}') FROM pangolin.goose_db_version WHERE is_applied AND version_id > 0", func(row pgx.Row) error {
		return row.Scan(&versions)
	})
	return versions
}

func (d *drillDatabase) serverVersion(t *testing.T) string {
	t.Helper()
	var version string
	d.freshRow(t, "SELECT current_setting('server_version')", func(row pgx.Row) error { return row.Scan(&version) })
	return version
}

// rlsInventory reports [row_level_security, force] for every owner-scoped
// table. Deployment metadata (goose_db_version) and the tenant authority
// membership catalog (pangolin_security.tenant_memberships, readable only by
// the dedicated authority role path per migration 3000001) are excluded; the
// exclusion and its remaining risk are recorded by the security review drill.
func (d *drillDatabase) rlsInventory(t *testing.T) map[string][2]bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsnWithDatabase(t, d.adminDSN, d.name))
	must(t, err)
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT n.nspname||'.'||c.relname,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind='r' AND n.nspname LIKE 'pangolin%' AND c.relname NOT IN ('goose_db_version','tenant_memberships') ORDER BY 1`)
	must(t, err)
	result := map[string][2]bool{}
	for rows.Next() {
		var name string
		var rls, force bool
		must(t, rows.Scan(&name, &rls, &force))
		result[name] = [2]bool{rls, force}
	}
	must(t, rows.Err())
	return result
}

func (d *drillDatabase) policies(t *testing.T, schema, table string) []string {
	t.Helper()
	var names []string
	d.freshRow(t, fmt.Sprintf("SELECT COALESCE(array_agg(policyname ORDER BY policyname),'{}') FROM pg_policies WHERE schemaname='%s' AND tablename='%s'", schema, table), func(row pgx.Row) error {
		return row.Scan(&names)
	})
	return names
}

// roleAttributes reads the fixture role posture that release review requires:
// no superuser, no BYPASSRLS, no role/database creation for app/worker.
func (d *drillDatabase) roleAttributes(t *testing.T) map[string]map[string]bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsnWithDatabase(t, d.adminDSN, d.name))
	must(t, err)
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT rolname,rolsuper,rolbypassrls,rolcreaterole,rolcreatedb FROM pg_roles WHERE rolname IN ('pangolin_app','pangolin_worker','pangolin_migrator') ORDER BY 1`)
	must(t, err)
	result := map[string]map[string]bool{}
	for rows.Next() {
		var name string
		var super, bypass, createRole, createDB bool
		must(t, rows.Scan(&name, &super, &bypass, &createRole, &createDB))
		result[name] = map[string]bool{"superuser": super, "bypassrls": bypass, "createrole": createRole, "createdb": createDB}
	}
	must(t, rows.Err())
	return result
}

// unownedTables counts business tables owned by an app/worker role; 0 is the
// required posture (owner roles own the schema).
func (d *drillDatabase) unownedTables(t *testing.T) int {
	t.Helper()
	var count int
	d.freshRow(t, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_roles r ON r.oid=c.relowner WHERE c.relkind='r' AND n.nspname LIKE 'pangolin%' AND r.rolname IN ('pangolin_app','pangolin_worker')`, func(row pgx.Row) error { return row.Scan(&count) })
	return count
}
