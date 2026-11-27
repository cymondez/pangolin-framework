//go:build integration

package m3

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"sync"
	"testing"
	"time"

	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	"github.com/pangolin-framework/pangolin/modules/organization"
	"github.com/pangolin-framework/pangolin/persistence/idempotency"
	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/platform/audit"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
	tenantpg "github.com/pangolin-framework/pangolin/tenancy/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var initialized sync.Once
var setupError error

func sub(f fs.FS) fs.FS {
	v, e := fs.Sub(f, "migrations")
	if e != nil {
		panic(e)
	}
	return v
}
func pool(t *testing.T, key string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(key)
	if dsn == "" {
		t.Fatalf("required %s missing", key)
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	if err = p.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return p
}
func prepare(t *testing.T) {
	t.Helper()
	initialized.Do(func() {
		db, err := sql.Open("pgx", os.Getenv("PANGOLIN_M1_ADMIN_DSN"))
		if err != nil {
			setupError = err
			return
		}
		defer db.Close()
		setupError = installM3Database(db)
	})
	if setupError != nil {
		t.Fatal(setupError)
	}
}
func installM3Database(db *sql.DB) error {
	source := migration.JoinFS(map[string]fs.FS{"tenant": sub(tenantpg.Migrations), "audit": sub(audit.Migrations), "scope": sub(migration.ScopeMigrations), "organization": organization.Migrations(), "events": sub(eventpg.Migrations), "idempotency": sub(idempotency.Migrations)})
	runner, err := migration.NewPostgres(db, source, 3300001, 3300001)
	if err != nil {
		return err
	}
	modules := []migration.ModuleMigrations{{Module: "tenant", Directory: "tenant"}, {Module: "audit", Directory: "audit", DependsOn: []string{"tenant"}}, {Module: "scope", Directory: "scope"}, {Module: "organization", Directory: "organization", DependsOn: []string{"scope", "tenant"}}, {Module: "events", Directory: "events"}, {Module: "idempotency", Directory: "idempotency"}}
	if err = runner.Apply(context.Background(), modules); err != nil {
		return err
	}
	_, err = db.Exec(`CREATE SCHEMA IF NOT EXISTS pangolin_m3;
CREATE TABLE IF NOT EXISTS pangolin_m3.records(tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,id text NOT NULL,value int NOT NULL DEFAULT 1,PRIMARY KEY(tenant_id,scope_kind,scope_id,id),FOREIGN KEY(tenant_id,scope_kind,scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id));
ALTER TABLE pangolin_m3.records ENABLE ROW LEVEL SECURITY;ALTER TABLE pangolin_m3.records FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS exact_owner ON pangolin_m3.records;
CREATE POLICY exact_owner ON pangolin_m3.records USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
GRANT USAGE ON SCHEMA pangolin_m3 TO pangolin_app,pangolin_worker;
GRANT SELECT,INSERT,UPDATE ON pangolin_m3.records TO pangolin_app,pangolin_worker;`)
	return err
}
func manager(t *testing.T, r uow.Resource) uow.UnitOfWorkManager {
	t.Helper()
	m, e := uow.NewManager(r, uow.DefaultOptions{DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second, HookTimeout: 100 * time.Millisecond, MaxRequiresNewDepth: 3})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func tenantContext(t *testing.T, admin, authority *pgxpool.Pool, tenant string, p scope.PrincipalRef) (context.Context, *scope.Registry) {
	t.Helper()
	_, err := admin.Exec(context.Background(), `INSERT INTO pangolin_security.tenant_memberships VALUES($1,$2,$3,'active',1) ON CONFLICT(tenant_id,principal_kind,principal_id) DO NOTHING`, tenant, p.Kind, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(context.Background(), `INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,'tenant',$1) ON CONFLICT DO NOTHING`, tenant); err != nil {
		t.Fatal(err)
	}
	identity := func(context.Context) (scope.PrincipalRef, string, bool) { return p, tenant, true }
	a := tenantpg.Authority{Pool: authority}
	tr, _ := tenancy.NewResolver(identity, a)
	ctx, err := tr.Resolve(context.Background(), tenant, "")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: a, Permission: func(context.Context, scope.ScopeBinding, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = registry.Bind(ctx, scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, registry
}
func insert(ctx context.Context, id string) error {
	b, _ := scope.Current(ctx)
	ex, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `INSERT INTO pangolin_m3.records(tenant_id,scope_kind,scope_id,id) VALUES($1,$2,$3,$4)`, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID, id)
	return err
}
func count(t *testing.T, admin *pgxpool.Pool, id string) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_m3.records WHERE id=$1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
