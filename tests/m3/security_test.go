//go:build integration

package m3

import (
	"context"
	"errors"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure"
	"github.com/pangolin-framework/pangolin/kernel/config"
	"github.com/pangolin-framework/pangolin/kernel/module"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/platform/audit"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
	tenantpg "github.com/pangolin-framework/pangolin/tenancy/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"io/fs"
	"net/url"
	"testing"
	"time"
)

func securityFixture(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	db, dsn := isolatedDB(t)
	source := migration.JoinFS(map[string]fs.FS{"tenant": sub(tenantpg.Migrations), "audit": sub(audit.Migrations), "scope": sub(migration.ScopeMigrations)})
	r, e := migration.NewPostgres(db, source, 3100001, 3100001)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Apply(context.Background(), []migration.ModuleMigrations{{Module: "tenant", Directory: "tenant"}, {Module: "audit", Directory: "audit"}, {Module: "scope", Directory: "scope"}}); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE SCHEMA pangolin_security_qa;
CREATE TABLE pangolin_security_qa.parents(tenant_id text,scope_kind text,scope_id text,id text,value text,deleted boolean DEFAULT false,PRIMARY KEY(tenant_id,scope_kind,scope_id,id),FOREIGN KEY(tenant_id,scope_kind,scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id));
CREATE TABLE pangolin_security_qa.children(tenant_id text,scope_kind text,scope_id text,id text,parent_id text,PRIMARY KEY(tenant_id,scope_kind,scope_id,id),FOREIGN KEY(tenant_id,scope_kind,scope_id,parent_id) REFERENCES pangolin_security_qa.parents(tenant_id,scope_kind,scope_id,id));
CREATE TABLE pangolin_security_qa.tenant_data(tenant_id text PRIMARY KEY,value text);
INSERT INTO pangolin_security_qa.tenant_data VALUES('a','private');
CREATE TABLE pangolin_security_qa.jobs(scope_kind text);
CREATE TABLE pangolin_security_qa.events(scope_kind text);
ALTER TABLE pangolin_security_qa.parents ENABLE ROW LEVEL SECURITY;ALTER TABLE pangolin_security_qa.parents FORCE ROW LEVEL SECURITY;
ALTER TABLE pangolin_security_qa.children ENABLE ROW LEVEL SECURITY;ALTER TABLE pangolin_security_qa.children FORCE ROW LEVEL SECURITY;
ALTER TABLE pangolin_security_qa.tenant_data ENABLE ROW LEVEL SECURITY;ALTER TABLE pangolin_security_qa.tenant_data FORCE ROW LEVEL SECURITY;
CREATE POLICY owner ON pangolin_security_qa.parents USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
CREATE POLICY owner ON pangolin_security_qa.children USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
CREATE POLICY tenant_only ON pangolin_security_qa.tenant_data USING(tenant_id=current_setting('app.tenant_id',true) AND current_setting('app.scope_kind',true)='tenant' AND current_setting('app.scope_id',true)=tenant_id);
GRANT USAGE ON SCHEMA pangolin_security_qa TO pangolin_app;
GRANT SELECT,INSERT,UPDATE ON pangolin_security_qa.parents,pangolin_security_qa.children,pangolin_security_qa.tenant_data TO pangolin_app;`)
	if e != nil {
		t.Fatal(e)
	}
	open := func(raw string) *pgxpool.Pool {
		p, e := pgxpool.New(context.Background(), raw)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Close)
		return p
	}
	admin := open(dsn)
	parsed, _ := url.Parse(dsn)
	parsed.User = url.UserPassword("pangolin_app", "local-app-only")
	return admin, open(parsed.String())
}

type neutralProvider struct{}

func (neutralProvider) Kinds() []string { return []string{"org.organization", "org.enterprise"} }
func (neutralProvider) Resolve(_ context.Context, p scope.PrincipalRef, r scope.ScopeRef) (scope.ScopeBinding, error) {
	return scope.ScopeBinding{Ref: r, Principal: p, AuthorityRevision: "trusted-test-grant"}, nil
}
func (neutralProvider) Authorize(context.Context, scope.ScopeBinding, string) error { return nil }
func scopedContexts(t *testing.T, admin, app *pgxpool.Pool) (context.Context, []context.Context, []scope.ScopeRef) {
	t.Helper()
	ctx, _ := tenantContext(t, admin, app, "a", scope.PrincipalRef{ID: "alice", Kind: "user"})
	a := tenantpg.Authority{Pool: app}
	r, _ := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: a}, neutralProvider{})
	refs := []scope.ScopeRef{{TenantID: "a", Kind: "org.organization", ID: "one"}, {TenantID: "a", Kind: "org.organization", ID: "two"}, {TenantID: "a", Kind: "org.enterprise", ID: "parent"}}
	var contexts []context.Context
	for _, ref := range refs {
		if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,$2,$3)`, ref.TenantID, ref.Kind, ref.ID); e != nil {
			t.Fatal(e)
		}
		c, e := r.Bind(ctx, ref)
		if e != nil {
			t.Fatal(e)
		}
		contexts = append(contexts, c)
	}
	return ctx, contexts, refs
}
func TestA77ScopeIsolation(t *testing.T) {
	admin, app := securityFixture(t)
	tenant, contexts, refs := scopedContexts(t, admin, app)
	m := manager(t, &postgres.Resource{Pool: app})
	if err := m.Within(contexts[0], uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, _ := postgres.Current(c, true)
		_, e := ex.Exec(c, `INSERT INTO pangolin_security_qa.parents VALUES('a','org.organization','one','book','private',false)`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	for i, c := range contexts {
		t.Run([]string{"missing-owner-filter", "cross-owner-read", "parent-cannot-read-child"}[i], func(t *testing.T) {
			if e := m.Within(c, uow.Options{}, func(work context.Context) error {
				ex, _ := postgres.Current(work, false)
				var n int
				e := ex.QueryRow(work, `SELECT count(*) FROM pangolin_security_qa.parents`).Scan(&n)
				want := 0
				if i == 0 {
					want = 1
				}
				if n != want {
					t.Fatal(n)
				}
				return e
			}); e != nil {
				t.Fatal(e)
			}
		})
	}
	t.Run("forged-owner", func(t *testing.T) {
		err := m.Within(contexts[1], uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			ex, _ := postgres.Current(work, true)
			_, e := ex.Exec(work, `INSERT INTO pangolin_security_qa.parents VALUES('a','org.organization','one','forged','x',false)`)
			return e
		})
		if err == nil {
			t.Fatal("forged owner accepted")
		}
	})
	t.Run("org-cannot-use-tenant-only", func(t *testing.T) {
		if err := m.Within(contexts[0], uow.Options{}, func(c context.Context) error { _, e := (infrastructure.Repository{}).Get(c, "book"); return e }); !errors.Is(err, uow.ErrScopeMismatch) {
			t.Fatal(err)
		}
		if err := m.Within(contexts[0], uow.Options{}, func(c context.Context) error {
			ex, _ := postgres.Current(c, false)
			var n int
			e := ex.QueryRow(c, `SELECT count(*) FROM pangolin_security_qa.tenant_data`).Scan(&n)
			if n != 0 {
				t.Fatal("tenant-only RLS bypass")
			}
			return e
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing-scope", func(t *testing.T) {
		if _, e := postgres.Current(context.Background(), false); !errors.Is(e, uow.ErrRequired) {
			t.Fatal(e)
		}
	})
	t.Run("pool-reuse", func(t *testing.T) {
		for _, c := range []context.Context{tenant, contexts[0], contexts[1], tenant} {
			if err := m.Within(c, uow.Options{}, func(work context.Context) error {
				b, _ := scope.Current(work)
				ex, _ := postgres.Current(work, false)
				var kind, id string
				if e := ex.QueryRow(work, `SELECT current_setting('app.scope_kind'),current_setting('app.scope_id')`).Scan(&kind, &id); e != nil {
					return e
				}
				if kind != b.Ref.Kind || id != b.Ref.ID {
					t.Fatal(kind, id)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		var n int
		if err := app.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_security_qa.parents`).Scan(&n); err != nil || n != 0 {
			t.Fatal("pool polluted", err, n)
		}
	})
	_ = refs
}
func TestA78OwnerConstraints(t *testing.T) {
	admin, app := securityFixture(t)
	_, contexts, _ := scopedContexts(t, admin, app)
	m := manager(t, &postgres.Resource{Pool: app})
	for _, tc := range []struct {
		name             string
		ctx              context.Context
		tenant, kind, id string
		good             bool
	}{{"same-owner-control", contexts[0], "a", "org.organization", "one", true}, {"cross-owner-fk", contexts[1], "a", "org.organization", "two", false}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.good {
				if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin_security_qa.parents VALUES('a','org.organization','one','parent','p',false)`); e != nil {
					t.Fatal(e)
				}
			}
			err := m.Within(tc.ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				ex, _ := postgres.Current(c, true)
				_, e := ex.Exec(c, `INSERT INTO pangolin_security_qa.children VALUES($1,$2,$3,$4,'parent')`, tc.tenant, tc.kind, tc.id, tc.name)
				return e
			})
			if tc.good && err != nil {
				t.Fatal(err)
			}
			if !tc.good {
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != "23503" {
					t.Fatal(err)
				}
			}
		})
	}
	t.Run("unknown-owner", func(t *testing.T) {
		_, e := admin.Exec(context.Background(), `INSERT INTO pangolin_security_qa.parents VALUES('a','org.organization','unknown','bad','p',false)`)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "23503" {
			t.Fatal(e)
		}
	})
	t.Run("cross-tenant-fk", func(t *testing.T) {
		_, e := admin.Exec(context.Background(), `INSERT INTO pangolin_security_qa.children VALUES('b','org.organization','one','bad','parent')`)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "23503" {
			t.Fatal(e)
		}
	})
	t.Run("neutral-catalog-dependency", func(t *testing.T) {
		var target string
		if e := admin.QueryRow(context.Background(), `SELECT confrelid::regclass::text FROM pg_constraint WHERE conrelid='pangolin_security_qa.parents'::regclass AND contype='f'`).Scan(&target); e != nil || target != "pangolin.resource_scopes" {
			t.Fatal(e, target)
		}
	})
}
func TestA92Downgrade(t *testing.T) {
	admin, _ := securityFixture(t)
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "admin", Kind: "user"}, "a", true
	}
	registry, _ := scope.NewRegistry(identity, scope.TenantScopeProvider{Identity: identity})
	for _, table := range []struct{ name, table, column string }{{"catalog-removal-blocked", "resource_scopes", "scope_kind"}, {"business-owner-removal-blocked", "parents", "scope_kind"}, {"pending-job-removal-blocked", "jobs", "scope_kind"}, {"pending-event-removal-blocked", "events", "scope_kind"}} {
		t.Run(table.name, func(t *testing.T) {
			schema := "pangolin_security_qa"
			if table.table == "resource_scopes" {
				schema = "pangolin"
				if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin.resource_scopes VALUES('a','org.organization','retained','archived',1)`); e != nil {
					t.Fatal(e)
				}
			} else if table.table == "parents" {
				if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin_security_qa.parents VALUES('a','org.organization','retained','retained','p',false)`); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := admin.Exec(context.Background(), `INSERT INTO `+pgx.Identifier{schema, table.table}.Sanitize()+` VALUES('org.organization')`); e != nil {
					t.Fatal(e)
				}
			}
			inventory := postgres.ScopeInventory{Pool: admin, Tables: []postgres.OwnerTable{{Schema: schema, Table: table.table, KindColumn: table.column}}}
			started := false
			h, e := runtime.New(runtime.Options{DrainTimeout: time.Second, CleanupTimeout: time.Second, StartupChecks: []func(context.Context, module.Runtime) error{func(c context.Context, _ module.Runtime) error { return registry.CheckInventory(c, inventory) }}}, []config.Source{{Name: "defaults", Values: map[string]string{"uow.default_timeout": "1s", "uow.cleanup_timeout": "1s", "uow.hook_timeout": "1s", "uow.max_requires_new_depth": "2"}}})
			if e != nil {
				t.Fatal(e)
			}
			e = h.Start(context.Background(), []module.Module{{Name: "api", Version: "0.1.0", Start: func(context.Context, module.Runtime) error { started = true; return nil }}})
			if !errors.Is(e, scope.ErrMigrationRequired) || h.Ready() || started {
				t.Fatal(e, started)
			}
		})
	}
	t.Run("required-capability-blocked", func(t *testing.T) {
		if !errors.Is(registry.Require("org.organization"), scope.ErrKindUnsupported) {
			t.Fatal("missing capability accepted")
		}
	})
	t.Run("no-tenant-downgrade", func(t *testing.T) {
		var kind, state string
		if e := admin.QueryRow(context.Background(), `SELECT scope_kind,state FROM pangolin.resource_scopes WHERE scope_id='retained'`).Scan(&kind, &state); e != nil || kind != "org.organization" || state != "archived" {
			t.Fatal(e, kind, state)
		}
	})
}
