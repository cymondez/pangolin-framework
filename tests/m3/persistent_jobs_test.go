//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/modules/organization"
	"github.com/cymondez/pangolin-framework/persistence/migration"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/platform/jobs"
	jobpg "github.com/cymondez/pangolin-framework/platform/jobs/postgres"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
	tenantpg "github.com/cymondez/pangolin-framework/tenancy/postgres"
)

// Extends the original A79/A91 governance fixtures with the delivered persistent
// worker, so provider replacement is checked against a real queued Job document.
func TestM4PersistentOrganizationJobs(t *testing.T) {
	g := governanceFixture(t)
	org := g.newOrg(t, nil)
	service := scope.PrincipalRef{Kind: "service", ID: "projector"}
	tenantContext(t, g.admin, g.app, g.tenant, service)
	_, err := g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.scope_grants(tenant_id,scope_kind,scope_id,principal_kind,principal_id,role) VALUES($1,$2,$3,'service','projector','projector')`, g.tenant, org.Ref.Kind, org.Ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", g.admin.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runner, err := migration.NewPostgres(db, sub(jobpg.Migrations), 4000001, 4000001)
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.Apply(context.Background(), []migration.ModuleMigrations{{Module: "jobs", Directory: "."}}); err != nil {
		t.Fatal(err)
	}
	registry, err := jobs.NewRegistry(1024, jobs.Definition{Type: "project", Version: 1, Validate: func(json.RawMessage) error { return nil }, Handle: func(c context.Context, j jobs.Job) error {
		return g.options.Manager.Within(c, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			ex, e := postgres.Current(work, true)
			if e != nil {
				return e
			}
			_, e = ex.Exec(work, `INSERT INTO pangolin_m3.records(tenant_id,scope_kind,scope_id,id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, j.Owner.TenantID, j.Owner.Kind, j.Owner.ID, j.ID)
			return e
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	policy := jobs.Policy{Lease: 5 * time.Second, ExecutionTimeout: 2 * time.Second, CleanupTimeout: time.Second, PollInterval: time.Millisecond, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 3, Concurrency: 1, BatchSize: 1, MaxPending: 10, Retention: time.Hour}
	store, err := jobpg.NewStore(g.worker, registry, policy, func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	bound := g.bind(t, g.ctx, org.Ref)
	binding, _ := scope.Current(bound)
	makeJob := func() jobs.Job {
		return jobs.Job{ID: rand.Text(), Type: "project", Version: 1, Owner: org.Ref, Actor: binding.Principal, Payload: json.RawMessage(`{}`), DueAt: time.Now().Add(-time.Second), Deadline: time.Now().Add(time.Minute)}
	}
	first, second, revoked := makeJob(), makeJob(), makeJob()
	enqueue := func(j jobs.Job) {
		if e := g.options.Manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return store.Enqueue(c, j) }); e != nil {
			t.Fatal(e)
		}
	}
	run := func(provider *scope.Registry) (bool, error) {
		w, e := jobs.NewWorker(store, registry, policy, "organization-worker", func(c context.Context, j jobs.Job) (context.Context, error) {
			tr, _ := tenancy.NewResolver(func(context.Context) (scope.PrincipalRef, string, bool) { return service, g.tenant, true }, tenantpg.Authority{Pool: g.app})
			c, e := tr.Resolve(c, g.tenant, "")
			if e != nil {
				return nil, e
			}
			c, e = provider.Bind(c, j.Owner)
			if e != nil {
				return nil, e
			}
			return c, provider.Authorize(c, "events.project")
		}, func(context.Context, jobs.Job) error { return nil }, nil)
		if e != nil {
			return false, e
		}
		return w.Process(context.Background())
	}
	t.Run("organization-enabled", func(t *testing.T) {
		enqueue(first)
		ok, e := run(g.registry)
		if e != nil || !ok {
			t.Fatal(ok, e)
		}
	})
	enqueue(second)
	enqueue(revoked)
	source, err := g.store.Export(g.ctx)
	if err != nil {
		t.Fatal(err)
	}
	replacement, schema := replacementStore(t, g)
	if err = replacement.Import(g.ctx, source); err != nil {
		t.Fatal(err)
	}
	provider, err := organization.NewProvider(replacement)
	if err != nil {
		t.Fatal(err)
	}
	replacementRegistry, err := scope.NewRegistry(tenancy.Identity, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("replacement-contract", func(t *testing.T) {
		ok, e := run(replacementRegistry)
		if e != nil || !ok {
			t.Fatal(ok, e)
		}
		if count(t, g.admin, second.ID) != 1 {
			t.Fatal("owner/result changed after replacement")
		}
	})
	t.Run("current-grant-revoked", func(t *testing.T) {
		_, e := g.admin.Exec(context.Background(), `UPDATE `+schema+`.scope_grants SET state='revoked',version=version+1 WHERE principal_id='projector'`)
		if e != nil {
			t.Fatal(e)
		}
		ok, e := run(replacementRegistry)
		if e == nil || ok {
			t.Fatal("revoked job executed", ok, e)
		}
		if count(t, g.admin, revoked.ID) != 0 {
			t.Fatal("side effect after revocation")
		}
	})
}
