//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/inbox"
	eventpg "github.com/cymondez/pangolin-framework/events/postgres"
	"github.com/cymondez/pangolin-framework/modules/organization"
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
	tenantpg "github.com/cymondez/pangolin-framework/tenancy/postgres"
)

func TestA91Replacement(t *testing.T) {
	g := governanceFixture(t)
	parent := g.newEnterprise(t)
	org := g.newOrg(t, &parent.Ref.ID)
	bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
	bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
	if _, err := g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(org.Ref, 1), Principal: bob, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	service := scope.PrincipalRef{Kind: "service", ID: "projector"}
	tenantContext(t, g.admin, g.app, g.tenant, service)
	_, err := g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.scope_grants(tenant_id,scope_kind,scope_id,principal_kind,principal_id,role)VALUES($1,$2,$3,'service','projector','projector')`, g.tenant, org.Ref.Kind, org.Ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.restrictions(tenant_id,scope_kind,scope_id,action) VALUES($1,$2,$3,'books.delete')`, g.tenant, org.Ref.Kind, org.Ref.ID); err != nil {
		t.Fatal(err)
	}
	queue, _ := eventpg.NewStore(g.worker, eventPolicy())
	replay := distributed.Envelope{ID: rand.Text(), Type: "organization.membership.changed", SchemaVersion: 1, OccurredAt: time.Now().UTC(), TenantID: g.tenant, ScopeKind: org.Ref.Kind, ScopeID: org.Ref.ID, CorrelationID: rand.Text(), Producer: "organization", Payload: []byte(`{"fact":"before-provider-switch"}`)}
	replayRaw, err := distributed.Encode(replay)
	if err != nil {
		t.Fatal(err)
	}
	originalBound := g.bind(t, g.ctx, org.Ref)
	if err = g.options.Manager.Within(originalBound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return queue.Append(c, replay, replayRaw) }); err != nil {
		t.Fatal(err)
	}
	source, err := g.store.Export(g.ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := organization.EncodeBundle(source)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := organization.DecodeBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	privateSchema := "replacement_" + strings.ToLower(rand.Text()[:8])
	installReplacementSchema(t, g, privateSchema)
	opts := g.options
	opts.PrivateSchema = privateSchema
	replacement, err := organization.NewStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	var restored organization.Bundle
	t.Run("provider-export-import", func(t *testing.T) {
		if err := replacement.Import(g.ctx, decoded); err != nil {
			t.Fatal(err)
		}
		restored, err = replacement.Export(g.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = organization.Reconcile(source, restored); err != nil {
			t.Fatal(err)
		}
	})
	provider, err := organization.NewProvider(replacement)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: tenantpg.Authority{Pool: g.app}, Permission: func(context.Context, scope.ScopeBinding, string) error { return nil }}, provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("stable-owner-id", func(t *testing.T) {
		bound, err := registry.Bind(g.ctx, org.Ref)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := scope.Current(bound)
		if b.Ref != org.Ref {
			t.Fatal(b)
		}
	})
	t.Run("membership-state-revision", func(t *testing.T) {
		bound, err := registry.Bind(bobCtx, org.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Authorize(bound, "books.read"); err != nil {
			t.Fatal(err)
		}
		if err = registry.Authorize(bound, "books.write"); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
		if len(restored.Memberships) != 2 || restored.Catalog[1].Version != source.Catalog[1].Version {
			t.Fatal(restored)
		}
	})
	t.Run("negative-authorization-parity", func(t *testing.T) {
		for _, r := range []*scope.Registry{g.registry, registry} {
			bound, e := r.Bind(bobCtx, org.Ref)
			if e != nil {
				t.Fatal(e)
			}
			if e = r.Authorize(bound, "books.delete"); !errors.Is(e, scope.ErrDenied) {
				t.Fatal(e)
			}
			if _, e = r.Bind(bobCtx, parent.Ref); e == nil {
				t.Fatal("parent privilege")
			}
		}
	})
	t.Run("preserved-event-replay", func(t *testing.T) {
		var before, after []byte
		if e := g.admin.QueryRow(context.Background(), `SELECT envelope FROM pangolin_events.outbox WHERE event_id=$1`, replay.ID).Scan(&before); e != nil {
			t.Fatal(e)
		}
		// Replay the persisted pre-switch envelope through the durable Inbox,
		// using the replacement authority's imported current service grant.
		serviceCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, service)
		persisted, e := distributed.Decode(before)
		if e != nil {
			t.Fatal(e)
		}
		group := "replacement-" + rand.Text()
		schemas, _ := distributed.NewRegistry(distributed.Schema{Type: replay.Type, Version: 1})
		receiver, e := inbox.NewReceiver(queue, schemas, group, func(context.Context, distributed.Envelope) (string, error) { return "organization", nil }, func(context.Context, error) { t.Fatal("unexpected envelope conflict") })
		if e != nil {
			t.Fatal(e)
		}
		if e = receiver.Receive(context.Background(), persisted, func(context.Context) error { return nil }); e != nil {
			t.Fatal(e)
		}
		worker, e := inbox.NewWorker(queue, manager(t, &postgres.Resource{Pool: g.worker}), group, func(_ context.Context, candidate inbox.Candidate) (context.Context, error) {
			bound, e := registry.Bind(serviceCtx, scope.ScopeRef{TenantID: candidate.Envelope.TenantID, Kind: candidate.Envelope.ScopeKind, ID: candidate.Envelope.ScopeID})
			if e != nil {
				return nil, e
			}
			return bound, registry.Authorize(bound, "events.project")
		}, func(c context.Context, event distributed.Envelope) error {
			if event.ID != replay.ID || event.ScopeID != org.Ref.ID {
				t.Fatal(event)
			}
			return insert(c, event.ID)
		}, 3, time.Millisecond)
		if e != nil {
			t.Fatal(e)
		}
		if processed, e := worker.Process(context.Background()); e != nil || !processed {
			t.Fatalf("%v cause=%v processed=%v", e, errors.Unwrap(e), processed)
		}
		if e = receiver.Receive(context.Background(), persisted, func(context.Context) error { return nil }); e != nil {
			t.Fatal(e)
		}
		if processed, e := worker.Process(context.Background()); e != nil || processed {
			t.Fatal("duplicate replay", e, processed)
		}
		if count(t, g.admin, replay.ID) != 1 {
			t.Fatal("replay lost or duplicated local effect")
		}
		if e := g.admin.QueryRow(context.Background(), `SELECT envelope FROM pangolin_events.outbox WHERE event_id=$1`, replay.ID).Scan(&after); e != nil {
			t.Fatal(e)
		}
		if string(before) != string(after) {
			t.Fatal("envelope modified by provider switch")
		}
	})
	t.Run("nonempty-import-rejected", func(t *testing.T) {
		if e := replacement.Import(g.ctx, source); !errors.Is(e, organization.ErrConflict) {
			t.Fatal(e)
		}
	})
}

func replacementStore(t *testing.T, g *governance) (*organization.Store, string) {
	t.Helper()
	name := "replacement_" + strings.ToLower(rand.Text()[:8])
	installReplacementSchema(t, g, name)
	options := g.options
	options.PrivateSchema = name
	store, e := organization.NewStore(options)
	if e != nil {
		t.Fatal(e)
	}
	return store, name
}
func installReplacementSchema(t *testing.T, g *governance, name string) {
	t.Helper()
	files, e := fs.ReadDir(organization.Migrations(), ".")
	if e != nil {
		t.Fatal(e)
	}
	for _, file := range files {
		raw, e := fs.ReadFile(organization.Migrations(), file.Name())
		if e != nil {
			t.Fatal(e)
		}
		sql := strings.ReplaceAll(strings.Split(string(raw), "-- +goose Down")[0], "pangolin_organization", name)
		if _, e = g.admin.Exec(context.Background(), sql); e != nil {
			t.Fatal(e)
		}
	}
}
func TestProviderImportFailureAtomicity(t *testing.T) {
	g := governanceFixture(t)
	e := g.newEnterprise(t)
	o := g.newOrg(t, &e.Ref.ID)
	if _, err := g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.scope_grants(tenant_id,scope_kind,scope_id,principal_kind,principal_id,role) VALUES($1,$2,$3,'service','worker','reader')`, g.tenant, o.Ref.Kind, o.Ref.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.restrictions VALUES($1,$2,$3,'books.delete')`, g.tenant, o.Ref.Kind, o.Ref.ID); err != nil {
		t.Fatal(err)
	}
	bundle, err := g.store.Export(g.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"pangolin.resource_scopes", "enterprises", "organizations", "organization_memberships", "enterprise_roles", "scope_grants", "restrictions", "owners"} {
		t.Run(table, func(t *testing.T) {
			replacement, schema := replacementStore(t, g)
			target := schema + "." + table
			if table == "pangolin.resource_scopes" {
				target = table
			}
			governanceFault(t, g, target)
			if err := replacement.Import(g.ctx, bundle); err == nil {
				t.Fatal("import ignored failure")
			}
			var n int
			if err := g.admin.QueryRow(context.Background(), `SELECT count(*) FROM `+schema+`.enterprises WHERE tenant_id=$1`, g.tenant).Scan(&n); err != nil || n != 0 {
				t.Fatal("partial import", err, n)
			}
		})
	}
	t.Run("neutral-catalog-mismatch", func(t *testing.T) {
		replacement, _ := replacementStore(t, g)
		raw, _ := organization.EncodeBundle(bundle)
		copy, _ := organization.DecodeBundle(raw)
		for i := range copy.Catalog {
			copy.Catalog[i].Version++
		}
		for i := range copy.Enterprises {
			copy.Enterprises[i].Version++
		}
		for i := range copy.Organizations {
			copy.Organizations[i].Version++
		}
		if err := replacement.Import(g.ctx, copy); !errors.Is(err, organization.ErrConflict) {
			t.Fatal(err)
		}
	})
	t.Run("required-effects-failure", func(t *testing.T) {
		replacement, _ := replacementStore(t, g)
		boom := errors.New("import audit unavailable")
		*g.effectsError = boom
		if err := replacement.Import(g.ctx, bundle); !errors.Is(err, boom) {
			t.Fatal(err)
		}
		*g.effectsError = nil
	})
}

func TestProviderExportConsistentFailure(t *testing.T) {
	g := governanceFixture(t)
	g.newOrg(t, nil)
	for _, table := range []string{"enterprises", "organizations", "organization_memberships", "enterprise_roles", "scope_grants", "restrictions", "owners"} {
		t.Run(table, func(t *testing.T) {
			if _, e := g.admin.Exec(context.Background(), `REVOKE SELECT ON pangolin_organization.`+table+` FROM pangolin_scope_authority`); e != nil {
				t.Fatal(e)
			}
			defer func() {
				if _, e := g.admin.Exec(context.Background(), `GRANT SELECT ON pangolin_organization.`+table+` TO pangolin_scope_authority`); e != nil {
					t.Error(e)
				}
			}()
			if _, e := g.store.Export(g.ctx); e == nil {
				t.Fatal("partial export accepted")
			}
		})
	}
}
