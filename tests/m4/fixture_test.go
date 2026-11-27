//go:build integration

package m4

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/platform/audit"
	"github.com/pangolin-framework/pangolin/platform/jobs"
	jobpg "github.com/pangolin-framework/pangolin/platform/jobs/postgres"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
	tenantpg "github.com/pangolin-framework/pangolin/tenancy/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func writeEvidence(t *testing.T, name string, data any) {
	t.Helper()
	directory := os.Getenv("PANGOLIN_M4_METRICS_DIR")
	if directory == "" {
		directory = t.TempDir()
	}
	must(t, os.MkdirAll(directory, 0755))
	raw, err := json.MarshalIndent(data, "", "  ")
	must(t, err)
	must(t, os.WriteFile(filepath.Join(directory, name), raw, 0600))
}

type fixture struct {
	admin, app, queue *pgxpool.Pool
	manager           uow.UnitOfWorkManager
	registry          *jobs.Registry
	policy            jobs.Policy
	store             *jobpg.Store
	tenant            string
}

func policy() jobs.Policy {
	return jobs.Policy{Lease: 3 * time.Second, ExecutionTimeout: time.Second, CleanupTimeout: time.Second, PollInterval: 10 * time.Millisecond, RetryInitial: time.Millisecond, RetryMax: 10 * time.Millisecond, MaxAttempts: 3, Concurrency: 2, BatchSize: 2, MaxPending: 8, Retention: time.Hour}
}
func sub(f fs.FS) fs.FS {
	v, e := fs.Sub(f, "migrations")
	if e != nil {
		panic(e)
	}
	return v
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func newFixture(t *testing.T, handler func(context.Context, jobs.Job) error) *fixture {
	t.Helper()
	ctx := context.Background()
	rootDSN := os.Getenv("PANGOLIN_M1_ADMIN_DSN")
	if rootDSN == "" {
		t.Fatal("PANGOLIN_M1_ADMIN_DSN required")
	}
	root, err := pgxpool.New(ctx, rootDSN)
	must(t, err)
	name := "m4_" + rand.Text()
	_, err = root.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	must(t, err)
	t.Cleanup(func() {
		_, e := root.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		if e != nil {
			t.Error(e)
		}
		root.Close()
	})
	open := func(raw string) *pgxpool.Pool {
		u, e := url.Parse(raw)
		must(t, e)
		u.Path = "/" + name
		p, e := pgxpool.New(ctx, u.String())
		must(t, e)
		t.Cleanup(p.Close)
		return p
	}
	f := &fixture{admin: open(rootDSN), app: open(os.Getenv("PANGOLIN_M1_APP_DSN")), queue: open(os.Getenv("PANGOLIN_M1_WORKER_DSN")), policy: policy(), tenant: "tenant-a"}
	db, err := sql.Open("pgx", f.admin.Config().ConnString())
	must(t, err)
	defer db.Close()
	capacity, err := fs.Sub(eventpg.CapacityMigrations, "capacity_migrations")
	must(t, err)
	sources := migration.JoinFS(map[string]fs.FS{"tenant": sub(tenantpg.Migrations), "scope": sub(migration.ScopeMigrations), "audit": sub(audit.Migrations), "events": sub(eventpg.Migrations), "jobs": sub(jobpg.Migrations), "capacity": capacity})
	runner, err := migration.NewPostgres(db, sources, 4000002, 4100003)
	must(t, err)
	must(t, runner.Apply(ctx, []migration.ModuleMigrations{{Module: "tenant", Directory: "tenant"}, {Module: "scope", Directory: "scope"}, {Module: "audit", Directory: "audit"}, {Module: "events", Directory: "events"}, {Module: "jobs", Directory: "jobs", DependsOn: []string{"scope"}}, {Module: "capacity", Directory: "capacity", DependsOn: []string{"events"}}}))
	_, err = f.admin.Exec(ctx, `CREATE TABLE pangolin.job_effects(id text PRIMARY KEY,value int NOT NULL DEFAULT 1);GRANT SELECT,INSERT ON pangolin.job_effects TO pangolin_worker;`)
	must(t, err)
	// Deployment provisioning keeps the jobs permit pool consistent with the
	// fixture policy; event tests provision their own capacity per case.
	_, err = f.admin.Exec(ctx, `SELECT pangolin_jobs.provision_permits($1,'resize')`, f.policy.MaxPending)
	must(t, err)
	if handler == nil {
		handler = func(context.Context, jobs.Job) error { return nil }
	}
	f.registry, err = jobs.NewRegistry(1024, jobs.Definition{Type: "test.work", Version: 1, Validate: func(raw json.RawMessage) error {
		var p struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(raw, &p) != nil || p.Value == "" {
			return jobs.ErrInvalid
		}
		return nil
	}, Handle: handler})
	must(t, err)
	f.manager, err = uow.NewManager(&postgres.Resource{Pool: f.app}, uow.DefaultOptions{DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	must(t, err)
	f.store, err = jobpg.NewStore(f.queue, f.registry, f.policy, func(context.Context, string) error { return nil })
	must(t, err)
	return f
}
func (f *fixture) bind(t *testing.T, parent context.Context, tenant string, principal scope.PrincipalRef) context.Context {
	t.Helper()
	_, err := f.admin.Exec(parent, `INSERT INTO pangolin_security.tenant_memberships VALUES($1,$2,$3,'active',1) ON CONFLICT DO NOTHING`, tenant, principal.Kind, principal.ID)
	must(t, err)
	_, err = f.admin.Exec(parent, `INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,'tenant',$1) ON CONFLICT DO NOTHING`, tenant)
	must(t, err)
	ctx, err := f.resolve(parent, scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant}, principal)
	must(t, err)
	return ctx
}
func (f *fixture) resolve(ctx context.Context, ref scope.ScopeRef, p scope.PrincipalRef) (context.Context, error) {
	authority := tenantpg.Authority{Pool: f.app}
	tr, _ := tenancy.NewResolver(func(context.Context) (scope.PrincipalRef, string, bool) { return p, ref.TenantID, true }, authority)
	ctx, err := tr.Resolve(ctx, ref.TenantID, "")
	if err != nil {
		return nil, err
	}
	r, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: authority, Permission: func(context.Context, scope.ScopeBinding, string) error { return nil }})
	if err != nil {
		return nil, err
	}
	return r.Bind(ctx, ref)
}
func (f *fixture) job(t *testing.T) jobs.Job {
	ctx := f.bind(t, context.Background(), f.tenant, scope.PrincipalRef{Kind: "user", ID: "alice"})
	b, _ := scope.Current(ctx)
	return jobs.Job{ID: rand.Text(), Type: "test.work", Version: 1, Owner: b.Ref, Actor: b.Principal, Payload: json.RawMessage(`{"value":"private-secret"}`), DueAt: time.Now().Add(-time.Second), Deadline: time.Now().Add(time.Minute)}
}
func (f *fixture) enqueue(t *testing.T, j jobs.Job) {
	t.Helper()
	ctx := f.bind(t, context.Background(), j.Owner.TenantID, j.Actor)
	must(t, f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, j) }))
}
func (f *fixture) worker(t *testing.T, actor jobs.CheckActor) *jobs.Worker {
	t.Helper()
	f.bind(t, context.Background(), f.tenant, scope.PrincipalRef{Kind: "service", ID: "worker"})
	if actor == nil {
		actor = func(context.Context, jobs.Job) error { return nil }
	}
	w, err := jobs.NewWorker(f.store, f.registry, f.policy, "worker", func(c context.Context, j jobs.Job) (context.Context, error) {
		return f.resolve(c, j.Owner, scope.PrincipalRef{Kind: "service", ID: "worker"})
	}, actor, nil)
	must(t, err)
	return w
}
func (f *fixture) state(t *testing.T, id string) (string, int) {
	t.Helper()
	var state string
	var n int
	must(t, f.admin.QueryRow(context.Background(), `SELECT state,attempts FROM pangolin_jobs.jobs WHERE id=$1`, id).Scan(&state, &n))
	return state, n
}
func (f *fixture) due(t *testing.T, id string) {
	t.Helper()
	_, err := f.admin.Exec(context.Background(), `UPDATE pangolin_jobs.jobs SET due_at=now()-interval '1 second' WHERE id=$1`, id)
	must(t, err)
}
func provisionQueue(t *testing.T, f *fixture, queue string, capacity int, action string) {
	t.Helper()
	_, err := f.admin.Exec(context.Background(), `SELECT pangolin_events.provision_queue_permits($1,$2,$3)`, queue, capacity, action)
	must(t, err)
}
func provisionJobs(t *testing.T, f *fixture, capacity int, action string) {
	t.Helper()
	_, err := f.admin.Exec(context.Background(), `SELECT pangolin_jobs.provision_permits($1,$2)`, capacity, action)
	must(t, err)
}
