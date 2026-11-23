//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/outbox"
	eventpg "github.com/cymondez/pangolin-framework/events/postgres"
	"github.com/cymondez/pangolin-framework/modules/organization"
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/persistence/idempotency"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/platform/audit"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
	tenantpg "github.com/cymondez/pangolin-framework/tenancy/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"strings"
	"testing"
	"time"
)

type governance struct {
	store              *organization.Store
	registry           *scope.Registry
	ctx                context.Context
	admin, app, worker *pgxpool.Pool
	tenant             string
	effectsError       *error
	options            organization.StoreOptions
	effectStage        *string
}

func governanceFixture(t *testing.T) *governance {
	t.Helper()
	db, dsn := isolatedDB(t)
	if err := installM3Database(db); err != nil {
		t.Fatal(err)
	}
	open := func(user, password string) *pgxpool.Pool {
		u, e := url.Parse(dsn)
		if e != nil {
			t.Fatal(e)
		}
		if user != "" {
			u.User = url.UserPassword(user, password)
		}
		p, e := pgxpool.New(context.Background(), u.String())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Close)
		return p
	}
	g := &governance{admin: open("", ""), app: open("pangolin_app", "local-app-only"), worker: open("pangolin_worker", "local-worker-only"), tenant: "governance-" + rand.Text()}
	g.ctx, _ = tenantContext(t, g.admin, g.app, g.tenant, scope.PrincipalRef{ID: "alice", Kind: "user"})
	schemas := []distributed.Schema{}
	for _, name := range []string{"enterprise.created", "organization.created", "organization.membership.changed", "organization.membership.removed", "enterprise.role.changed", "organization.transferred", "scope.suspended", "scope.active", "scope.archived", "organization.provider.imported"} {
		schemas = append(schemas, distributed.Schema{Type: name, Version: 1})
	}
	registry, err := distributed.NewRegistry(schemas...)
	if err != nil {
		t.Fatal(err)
	}
	queue, _ := eventpg.NewStore(g.worker, eventPolicy())
	publisher, _ := outbox.NewPublisher(registry, queue)
	m := manager(t, &eventpg.Resource{Base: &postgres.Resource{Pool: g.app}, Publisher: publisher})
	a := tenantpg.Authority{Pool: g.app}
	var injected error
	g.effectsError = &injected
	stage := ""
	g.effectStage = &stage
	effects := organization.EffectsFunc(func(c context.Context, change organization.Change) error {
		if injected != nil {
			return injected
		}
		if stage == "audit" {
			return errors.New("injected required audit failure")
		}
		id := rand.Text()
		if e := (audit.Recorder{}).Record(c, audit.Record{Action: change.Action, Resource: change.Target.ID, Result: "success", Trace: id}); e != nil {
			return e
		}
		if stage == "outbox" {
			return errors.New("injected outbox failure after audit")
		}
		payload, e := json.Marshal(change)
		if e != nil {
			return e
		}
		return publisher.Publish(c, distributed.Envelope{ID: id, Type: change.Action, SchemaVersion: 1, OccurredAt: time.Now().UTC(), TenantID: change.OperationScope.TenantID, ScopeKind: change.OperationScope.Kind, ScopeID: change.OperationScope.ID, CorrelationID: id, Producer: "organization", Payload: payload})
	})
	g.options = organization.StoreOptions{AuthorityPool: g.worker, Manager: m, Identity: tenancy.Identity, Tenants: a, Idempotency: orgIdempotency(t), Permissions: organization.PermissionFunc(func(_ context.Context, s organization.Snapshot, action string) error {
		if s.Role == "tenant" && (action == "tenant.enterprise.create" || action == "tenant.organization.create" || action == "tenant.scope.recover" || action == "tenant.organization.export" || action == "tenant.organization.import") {
			return nil
		}
		if s.Role == "owner" {
			return nil
		}
		if s.Role == "member" && action == "books.read" {
			return nil
		}
		if s.Role == "projector" && action == "events.project" {
			return nil
		}
		return scope.ErrDenied
	}), Effects: effects, UseChecker: organization.UseCheckerFunc(func(c context.Context, ref scope.ScopeRef) (bool, error) {
		var n int
		e := g.admin.QueryRow(c, `SELECT count(*) FROM pangolin_m3.records WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3`, ref.TenantID, ref.Kind, ref.ID).Scan(&n)
		return n > 0, e
	})}
	g.store, err = organization.NewStore(g.options)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := organization.NewProvider(g.store)
	if err != nil {
		t.Fatal(err)
	}
	g.registry, err = scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: a, Permission: func(context.Context, scope.ScopeBinding, string) error { return nil }}, provider)
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func orgIdempotency(t *testing.T) *idempotency.Store {
	t.Helper()
	s, e := idempotency.New(idempotency.Config{MaxKeyBytes: 128, MaxBodyBytes: 1 << 20, RetryWindow: 24 * time.Hour, Retention: 168 * time.Hour, Protect: func(b []byte) ([]byte, error) { return b, nil }, Unprotect: func(b []byte) ([]byte, error) { return b, nil }}, func(context.Context, string) error { return nil })
	if e != nil {
		t.Fatalf("%v cause=%v", e, errors.Unwrap(e))
	}
	return s
}
func slug() string { return strings.ToLower(strings.ReplaceAll(rand.Text()[:8], "_", "-")) }
func (g *governance) newOrg(t *testing.T, parent *string) contracts.Organization {
	t.Helper()
	o, e := g.store.CreateOrganization(g.ctx, contracts.CreateOrganization{TenantID: g.tenant, Slug: "org-" + slug(), EnterpriseID: parent, IdempotencyKey: rand.Text()})
	if e != nil {
		t.Fatalf("%v cause=%v", e, errors.Unwrap(e))
	}
	return o
}
func (g *governance) newEnterprise(t *testing.T) contracts.Enterprise {
	t.Helper()
	e, err := g.store.CreateEnterprise(g.ctx, contracts.CreateEnterprise{TenantID: g.tenant, Slug: "enterprise-" + slug(), IdempotencyKey: rand.Text()})
	if err != nil {
		t.Fatalf("%v cause=%v", err, errors.Unwrap(err))
	}
	return e
}
func (g *governance) command(ref scope.ScopeRef, v int64) contracts.Command {
	return contracts.Command{Target: ref, ExpectedVersion: v, IdempotencyKey: rand.Text()}
}
func TestA83Governance(t *testing.T) {
	g := governanceFixture(t)
	var standalone, child contracts.Organization
	var parent contracts.Enterprise
	t.Run("standalone", func(t *testing.T) {
		standalone = g.newOrg(t, nil)
		if standalone.EnterpriseID != nil {
			t.Fatal(standalone)
		}
	})
	t.Run("single-parent", func(t *testing.T) {
		parent = g.newEnterprise(t)
		child = g.newOrg(t, &parent.Ref.ID)
		if child.EnterpriseID == nil || *child.EnterpriseID != parent.Ref.ID {
			t.Fatal(child)
		}
	})
	t.Run("multiple-parent-rejected", func(t *testing.T) {
		second := g.newEnterprise(t)
		if _, e := g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.organizations(tenant_id,id,enterprise_id,slug)VALUES($1,$2,$3,'second-parent-copy')`, g.tenant, child.Ref.ID, second.Ref.ID); e == nil {
			t.Fatal("same Org acquired two parents")
		}
		var n int
		if e := g.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_organization.organizations WHERE tenant_id=$1 AND id=$2 AND enterprise_id=$3`, g.tenant, child.Ref.ID, parent.Ref.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal(e, n)
		}
	})
	t.Run("cross-tenant-rejected", func(t *testing.T) {
		other := governanceFixture(t)
		foreign := other.newEnterprise(t)
		if _, e := g.store.CreateOrganization(g.ctx, contracts.CreateOrganization{TenantID: g.tenant, Slug: "cross-tenant-org", EnterpriseID: &foreign.Ref.ID, IdempotencyKey: rand.Text()}); !errors.Is(e, organization.ErrNotFound) {
			t.Fatal(e)
		}
	})
	t.Run("owner-stable-on-transfer", func(t *testing.T) {
		moved, e := g.store.AssignEnterprise(g.ctx, contracts.AssignEnterprise{Command: g.command(standalone.Ref, 1), EnterpriseID: &parent.Ref.ID})
		if e != nil || moved.Ref != standalone.Ref || moved.Version != 2 {
			t.Fatal(e, moved)
		}
	})
	if e := g.store.Archive(g.ctx, g.command(parent.Ref, 1)); !errors.Is(e, organization.ErrInUse) {
		t.Fatal("nonempty parent archived", e)
	}
	if _, e := g.store.AssignEnterprise(g.ctx, contracts.AssignEnterprise{Command: g.command(child.Ref, 1), EnterpriseID: ptr("unowned-parent")}); e == nil {
		t.Fatal("unknown parent")
	}
	page, e := g.store.ListEnterpriseOrganizations(g.ctx, parent.Ref, contracts.PageRequest{Limit: 1})
	if e != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal(e, page)
	}
	second, e := g.store.ListEnterpriseOrganizations(g.ctx, parent.Ref, contracts.PageRequest{Limit: 1, Cursor: page.NextCursor})
	if e != nil || len(second.Items) != 1 || second.Items[0].Ref == page.Items[0].Ref {
		t.Fatal(e, second)
	}
}
func ptr(s string) *string { return &s }
func TestA84MembershipAndRevocation(t *testing.T) {
	g := governanceFixture(t)
	a, b := g.newOrg(t, nil), g.newOrg(t, nil)
	bob := scope.PrincipalRef{ID: "bob", Kind: "user"}
	bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
	var boundA, boundB context.Context
	t.Run("multi-organization-membership", func(t *testing.T) {
		g.add(t, a, bob, "owner", 1)
		g.add(t, b, bob, "member", 1)
		boundA = g.bind(t, bobCtx, a.Ref)
		boundB = g.bind(t, bobCtx, b.Ref)
	})
	t.Run("independent-roles", func(t *testing.T) {
		if e := g.registry.Authorize(boundA, "organization.membership.set"); e != nil {
			t.Fatal(e)
		}
		if e := g.registry.Authorize(boundB, "organization.membership.set"); !errors.Is(e, scope.ErrDenied) {
			t.Fatal("role spread", e)
		}
	})
	t.Run("scoped-revocation", func(t *testing.T) {
		if e := g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(a.Ref, 2), Principal: bob}); e != nil {
			t.Fatal(e)
		}
		m := manager(t, &postgres.Resource{Pool: g.app})
		if e := m.Within(boundA, uow.Options{}, func(context.Context) error { t.Fatal("revoked admission"); return nil }); e == nil {
			t.Fatal("stale binding")
		}
		if _, e := g.registry.Bind(bobCtx, b.Ref); e != nil {
			t.Fatal("other membership revoked", e)
		}
	})
	t.Run("tenant-membership-revoked", func(t *testing.T) {
		if _, e := g.admin.Exec(context.Background(), `UPDATE pangolin_security.tenant_memberships SET state='revoked',revision=revision+1 WHERE tenant_id=$1 AND principal_id='bob'`, g.tenant); e != nil {
			t.Fatal(e)
		}
		if _, e := g.registry.Bind(bobCtx, b.Ref); e == nil {
			t.Fatal("tenant revocation ignored")
		}
	})
}
func TestA87AtomicGovernance(t *testing.T) {
	g := governanceFixture(t)
	o := g.newOrg(t, nil)
	bob := scope.PrincipalRef{ID: "bob", Kind: "user"}
	tenantContext(t, g.admin, g.app, g.tenant, bob)
	boom := errors.New("effects unavailable")
	*g.effectsError = boom
	_, err := g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 1), Principal: bob, Role: "member"})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	var n int
	var revision int64
	if err = g.admin.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM pangolin_organization.organization_memberships WHERE tenant_id=$1 AND organization_id=$2 AND principal_id='bob'),(SELECT version FROM pangolin.resource_scopes WHERE tenant_id=$1 AND scope_kind=$3 AND scope_id=$2)`, g.tenant, o.Ref.ID, o.Ref.Kind).Scan(&n, &revision); err != nil || n != 0 || revision != 1 {
		t.Fatal(err, n, revision)
	}
	*g.effectsError = nil
	if _, err = g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 1), Principal: bob, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	var audits, events int
	if err = g.admin.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM pangolin_audit.records WHERE resource=$1),(SELECT count(*) FROM pangolin_events.outbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->'Target'->>'ID'=$1)`, o.Ref.ID).Scan(&audits, &events); err != nil {
		t.Fatal(err)
	}
	if audits != 2 || events != 2 {
		t.Fatal(audits, events)
	}
}
func TestA90LastOwner(t *testing.T) {
	g := governanceFixture(t)
	o := g.newOrg(t, nil)
	err := g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(o.Ref, 1), Principal: scope.PrincipalRef{ID: "alice", Kind: "user"}})
	if !errors.Is(err, organization.ErrLastOwner) {
		t.Fatal(err)
	}
	if _, err = g.admin.Exec(context.Background(), `UPDATE pangolin_security.tenant_memberships SET state='revoked',revision=revision+1 WHERE tenant_id=$1 AND principal_id='alice'`, g.tenant); err != nil {
		t.Fatal("tenant revocation blocked", err)
	}
	if _, err = g.registry.Bind(g.ctx, o.Ref); err == nil {
		t.Fatal("ownerless authorized")
	}
}

func (g *governance) bind(t *testing.T, ctx context.Context, ref scope.ScopeRef) context.Context {
	t.Helper()
	bound, e := g.registry.Bind(ctx, ref)
	if e != nil {
		t.Fatal(e)
	}
	return bound
}
func (g *governance) add(t *testing.T, o contracts.Organization, p scope.PrincipalRef, role string, version int64) {
	t.Helper()
	if _, e := g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, version), Principal: p, Role: role}); e != nil {
		t.Fatal(e)
	}
}
func TestA85GovernanceBoundary(t *testing.T) {
	g := governanceFixture(t)
	parent := g.newEnterprise(t)
	org := g.newOrg(t, &parent.Ref.ID)
	bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
	bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
	g.add(t, org, bob, "owner", 1)
	if e := g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(org.Ref, 2), Principal: scope.PrincipalRef{Kind: "user", ID: "alice"}}); e != nil {
		t.Fatal(e)
	}
	t.Run("enterprise-directory", func(t *testing.T) {
		p, e := g.store.ListEnterpriseOrganizations(g.ctx, parent.Ref, contracts.PageRequest{Limit: 20})
		if e != nil || len(p.Items) != 1 {
			t.Fatal(e, p)
		}
	})
	t.Run("governance-policy", func(t *testing.T) {
		bound := g.bind(t, g.ctx, parent.Ref)
		if e := g.registry.Authorize(bound, "enterprise.organizations.list"); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("enterprise-owner-no-org-content", func(t *testing.T) {
		if _, e := g.registry.Bind(g.ctx, org.Ref); e == nil {
			t.Fatal("enterprise owner read org content")
		}
	})
	t.Run("derived-member-no-org-permission", func(t *testing.T) {
		if _, e := g.registry.Bind(bobCtx, parent.Ref); e == nil {
			t.Fatal("derived membership created enterprise permission")
		}
	})
}

func TestA86RolePolicy(t *testing.T) {
	g := governanceFixture(t)
	parent := g.newEnterprise(t)
	org := g.newOrg(t, &parent.Ref.ID)
	bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
	bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
	g.add(t, org, bob, "member", 1)
	t.Run("role-policy-grid", func(t *testing.T) {
		for _, v := range []struct {
			ctx    context.Context
			action string
			allow  bool
		}{{g.ctx, "books.write", true}, {bobCtx, "books.read", true}, {bobCtx, "books.write", false}} {
			bound := g.bind(t, v.ctx, org.Ref)
			if e := g.registry.Authorize(bound, v.action); (e == nil) != v.allow {
				t.Fatal(v.action, e)
			}
		}
	})
	t.Run("explicit-deny", func(t *testing.T) {
		if _, e := g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.restrictions VALUES($1,$2,$3,'books.read')`, g.tenant, org.Ref.Kind, org.Ref.ID); e != nil {
			t.Fatal(e)
		}
		bound := g.bind(t, bobCtx, org.Ref)
		if e := g.registry.Authorize(bound, "books.read"); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
		if _, e := g.admin.Exec(context.Background(), `DELETE FROM pangolin_organization.restrictions WHERE tenant_id=$1`, g.tenant); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("parent-allow-no-membership", func(t *testing.T) {
		outsider := scope.PrincipalRef{Kind: "user", ID: "enterprise-owner"}
		outsiderCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, outsider)
		if e := g.store.SetEnterpriseRole(g.ctx, contracts.SetEnterpriseRole{Command: g.command(parent.Ref, 1), Principal: outsider, Role: "owner"}); e != nil {
			t.Fatal(e)
		}
		if _, e := g.registry.Bind(outsiderCtx, org.Ref); e == nil {
			t.Fatal("parent allowed missing member")
		}
	})
	t.Run("feature-no-grant", func(t *testing.T) {
		bound := g.bind(t, bobCtx, org.Ref)
		featureEnabled := true
		if !featureEnabled {
			t.Fatal("fixture")
		}
		if e := g.registry.Authorize(bound, "books.write"); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	})
	t.Run("unauthorized-self-elevation", func(t *testing.T) {
		_, e := g.store.SetOrganizationMembership(bobCtx, contracts.SetMembership{Command: g.command(org.Ref, 2), Principal: bob, Role: "owner"})
		if !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	})
	t.Run("audited-owner-join", func(t *testing.T) {
		if _, e := g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(org.Ref, 2), Principal: bob, Role: "owner"}); e != nil {
			t.Fatal(e)
		}
		if e := g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(org.Ref, 3), Principal: scope.PrincipalRef{Kind: "user", ID: "alice"}}); e != nil {
			t.Fatal(e)
		}
		if _, e := g.registry.Bind(g.ctx, org.Ref); e == nil {
			t.Fatal("implicit join")
		}
		if e := g.store.JoinAsOwner(g.ctx, g.command(org.Ref, 4)); e != nil {
			t.Fatal(e)
		}
		bound := g.bind(t, g.ctx, org.Ref)
		if e := g.registry.Authorize(bound, "books.write"); e != nil {
			t.Fatal(e)
		}
		var n int
		if e := g.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_audit.records WHERE resource=$1`, org.Ref.ID).Scan(&n); e != nil || n != 5 {
			t.Fatal(e, n)
		}
	})
	t.Run("tenant-restriction-only-tightens", func(t *testing.T) {
		if _, e := g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.restrictions VALUES($1,'tenant',$1,'books.read')`, g.tenant); e != nil {
			t.Fatal(e)
		}
		bound := g.bind(t, bobCtx, org.Ref)
		if e := g.registry.Authorize(bound, "books.read"); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	})
}

func TestA88AuthoritySnapshot(t *testing.T) {
	for _, variant := range []string{"revocation-between-bind-and-admission", "new-operation-after-revocation", "stale-jwt-cache", "admitted-operation-snapshot"} {
		t.Run(variant, func(t *testing.T) {
			g := governanceFixture(t)
			o := g.newOrg(t, nil)
			bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
			bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
			g.add(t, o, bob, "member", 1)
			bound := g.bind(t, bobCtx, o.Ref)
			m := manager(t, &postgres.Resource{Pool: g.app})
			revoke := func() {
				if e := g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(o.Ref, 2), Principal: bob}); e != nil {
					t.Fatal(e)
				}
			}
			if variant == "admitted-operation-snapshot" {
				if e := m.Within(bound, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
					revoke()
					if e := g.registry.Authorize(c, "books.read"); e != nil {
						return e
					}
					return m.Within(c, uow.Options{Propagation: uow.RequiresNew}, func(child context.Context) error { return g.registry.Authorize(child, "books.read") })
				}); e != nil {
					t.Fatal(e)
				}
			} else {
				revoke()
				if e := m.Within(bound, uow.Options{}, func(context.Context) error { t.Fatal("revoked callback"); return nil }); e == nil {
					t.Fatal("old snapshot admitted")
				}
			}
		})
	}
}

func TestA89Transfer(t *testing.T) {
	g := governanceFixture(t)
	first, second := g.newEnterprise(t), g.newEnterprise(t)
	o := g.newOrg(t, &first.Ref.ID)
	bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
	bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
	g.add(t, o, bob, "member", 1)
	t.Run("unauthorized-transfer", func(t *testing.T) {
		if _, e := g.store.AssignEnterprise(bobCtx, contracts.AssignEnterprise{Command: g.command(o.Ref, 2), EnterpriseID: &second.Ref.ID}); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	})
	t.Run("cross-tenant-transfer", func(t *testing.T) {
		other := governanceFixture(t)
		foreign := other.newEnterprise(t)
		if _, e := g.store.AssignEnterprise(g.ctx, contracts.AssignEnterprise{Command: g.command(o.Ref, 2), EnterpriseID: &foreign.Ref.ID}); !errors.Is(e, organization.ErrNotFound) {
			t.Fatal(e)
		}
	})
	t.Run("nonempty-parent-delete", func(t *testing.T) {
		if e := g.store.Archive(g.ctx, g.command(first.Ref, 1)); !errors.Is(e, organization.ErrInUse) {
			t.Fatal(e)
		}
	})
	before := g.bind(t, bobCtx, o.Ref)
	oldBinding, _ := scope.Current(before)
	t.Run("valid-same-tenant-transfer", func(t *testing.T) {
		moved, e := g.store.AssignEnterprise(g.ctx, contracts.AssignEnterprise{Command: g.command(o.Ref, 2), EnterpriseID: &second.Ref.ID})
		if e != nil || moved.Ref != o.Ref || moved.Version != 3 || moved.EnterpriseID == nil || *moved.EnterpriseID != second.Ref.ID {
			t.Fatal(e, moved)
		}
	})
	t.Run("stable-owner-and-new-ancestry", func(t *testing.T) {
		after := g.bind(t, bobCtx, o.Ref)
		current, _ := scope.Current(after)
		if current.Ref != oldBinding.Ref || current.AuthorityRevision == oldBinding.AuthorityRevision {
			t.Fatal(current, oldBinding)
		}
	})
}

func TestA90Recovery(t *testing.T) {
	g := governanceFixture(t)
	o := g.newOrg(t, nil)
	e := g.newEnterprise(t)
	t.Run("last-owner-removal-denied", func(t *testing.T) {
		if err := g.store.SetEnterpriseRole(g.ctx, contracts.SetEnterpriseRole{Command: g.command(e.Ref, 1), Principal: scope.PrincipalRef{Kind: "user", ID: "alice"}, Role: "manager"}); !errors.Is(err, organization.ErrLastOwner) {
			t.Fatal(err)
		}
	})
	recoverer := scope.PrincipalRef{Kind: "user", ID: "recoverer"}
	recoveryCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, recoverer)
	t.Run("tenant-revocation-allowed", func(t *testing.T) {
		if _, err := g.admin.Exec(context.Background(), `UPDATE pangolin_security.tenant_memberships SET state='revoked',revision=revision+1 WHERE tenant_id=$1 AND principal_id='alice'`, g.tenant); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ownerless-denied", func(t *testing.T) {
		if _, err := g.registry.Bind(recoveryCtx, o.Ref); err == nil {
			t.Fatal("ownerless access")
		}
	})
	t.Run("audited-recovery", func(t *testing.T) {
		if err := g.store.Recover(recoveryCtx, g.command(o.Ref, currentRevision(t, g, o.Ref)), recoverer); err != nil {
			t.Fatal(err)
		}
		bound := g.bind(t, recoveryCtx, o.Ref)
		if err := g.registry.Authorize(bound, "books.write"); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := g.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_audit.records WHERE resource=$1 AND action='scope.active'`, o.Ref.ID).Scan(&n); err != nil || n != 1 {
			t.Fatal(err, n)
		}
	})
}

func governanceFault(t *testing.T, g *governance, table string) {
	t.Helper()
	name := "fault_" + strings.ToLower(rand.Text()[:10])
	qualified := pgx.Identifier{"pangolin_m3", name}.Sanitize()
	parts := strings.Split(table, ".")
	identifier := pgx.Identifier(parts).Sanitize()
	sql := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.tenant_id='%s' THEN RAISE EXCEPTION 'injected governance stage'; END IF; RETURN NEW; END $$; CREATE TRIGGER %s BEFORE INSERT OR UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, qualified, g.tenant, pgx.Identifier{name}.Sanitize(), identifier, qualified)
	if _, e := g.admin.Exec(context.Background(), sql); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := g.admin.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER %s ON %s; DROP FUNCTION %s()`, pgx.Identifier{name}.Sanitize(), identifier, qualified)); e != nil {
			t.Error(e)
		}
	})
}
func TestA87WriteFaults(t *testing.T) {
	for _, tc := range []struct{ name, table string }{{"membership-write-failure", "pangolin_organization.organization_memberships"}, {"catalog-write-failure", "pangolin.resource_scopes"}, {"audit-write-failure", "pangolin_audit.records"}, {"outbox-write-failure", "pangolin_events.outbox"}} {
		t.Run(tc.name, func(t *testing.T) {
			g := governanceFixture(t)
			o := g.newOrg(t, nil)
			bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
			tenantContext(t, g.admin, g.app, g.tenant, bob)
			before, e := g.store.Export(g.ctx)
			if e != nil {
				t.Fatal(e)
			}
			governanceFault(t, g, tc.table)
			if _, e = g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 1), Principal: bob, Role: "member"}); e == nil {
				t.Fatal("fault did not fail")
			}
			after, e := g.store.Export(g.ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = organization.Reconcile(before, after); e != nil {
				t.Fatal("partially committed governance", e)
			}
		})
	}
	t.Run("atomic-success", TestA87AtomicGovernance)
	t.Run("revision-consistency", func(t *testing.T) {
		g := governanceFixture(t)
		o := g.newOrg(t, nil)
		bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
		tenantContext(t, g.admin, g.app, g.tenant, bob)
		g.add(t, o, bob, "member", 1)
		var catalog, private int64
		if e := g.admin.QueryRow(context.Background(), `SELECT c.version,o.version FROM pangolin.resource_scopes c JOIN pangolin_organization.organizations o ON o.tenant_id=c.tenant_id AND o.id=c.scope_id WHERE c.tenant_id=$1 AND c.scope_kind='org.organization' AND c.scope_id=$2`, g.tenant, o.Ref.ID).Scan(&catalog, &private); e != nil || catalog != 2 || catalog != private {
			t.Fatal(e, catalog, private)
		}
	})
}

func TestGovernanceMutationFaultMatrix(t *testing.T) {
	for _, op := range []string{"create-enterprise", "create-organization", "set-role", "set-member", "remove-member", "transfer", "suspend", "recover"} {
		tables := []string{"pangolin.resource_scopes", "pangolin_organization.owners", "pangolin_audit.records", "pangolin_events.outbox"}
		switch op {
		case "create-enterprise", "set-role":
			tables = append(tables, "pangolin_organization.enterprises", "pangolin_organization.enterprise_roles")
		case "create-organization", "set-member", "remove-member", "recover":
			tables = append(tables, "pangolin_organization.organizations", "pangolin_organization.organization_memberships")
		case "transfer", "suspend":
			tables = []string{"pangolin.resource_scopes", "pangolin_organization.organizations", "pangolin_audit.records", "pangolin_events.outbox"}
		}
		for _, table := range tables {
			t.Run(op+"/"+table, func(t *testing.T) {
				g := governanceFixture(t)
				o := g.newOrg(t, nil)
				parent := g.newEnterprise(t)
				bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
				tenantContext(t, g.admin, g.app, g.tenant, bob)
				g.add(t, o, bob, "owner", 1)
				before, e := g.store.Export(g.ctx)
				if e != nil {
					t.Fatal(e)
				}
				governanceFault(t, g, table)
				switch op {
				case "create-enterprise":
					_, e = g.store.CreateEnterprise(g.ctx, contracts.CreateEnterprise{TenantID: g.tenant, Slug: "new-enterprise", IdempotencyKey: rand.Text()})
				case "create-organization":
					_, e = g.store.CreateOrganization(g.ctx, contracts.CreateOrganization{TenantID: g.tenant, Slug: "new-org", IdempotencyKey: rand.Text()})
				case "set-role":
					e = g.store.SetEnterpriseRole(g.ctx, contracts.SetEnterpriseRole{Command: g.command(parent.Ref, 1), Principal: bob, Role: "manager"})
				case "set-member":
					_, e = g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 2), Principal: bob, Role: "member"})
				case "remove-member":
					e = g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(o.Ref, 2), Principal: bob})
				case "transfer":
					_, e = g.store.AssignEnterprise(g.ctx, contracts.AssignEnterprise{Command: g.command(o.Ref, 2), EnterpriseID: &parent.Ref.ID})
				case "suspend":
					e = g.store.Suspend(g.ctx, g.command(o.Ref, 2))
				case "recover":
					e = g.store.Recover(g.ctx, g.command(o.Ref, 2), bob)
				}
				if e == nil {
					t.Fatal("mutation ignored injected stage fault")
				}
				after, e := g.store.Export(g.ctx)
				if e != nil {
					t.Fatal(e)
				}
				if e = organization.Reconcile(before, after); e != nil {
					t.Fatal("partial governance mutation", e)
				}
			})
		}
	}
}

func TestGovernanceLifecycleAndReplay(t *testing.T) {
	g := governanceFixture(t)
	o := g.newOrg(t, nil)
	if p, e := g.store.ListMyOrganizations(g.ctx, contracts.PageRequest{Limit: 20}); e != nil || len(p.Items) != 1 {
		t.Fatal(e, p)
	}
	if e := g.store.Suspend(g.ctx, g.command(o.Ref, 1)); e != nil {
		t.Fatal(e)
	}
	if _, e := g.registry.Bind(g.ctx, o.Ref); e == nil {
		t.Fatal("suspended scope admission")
	}
	if e := g.store.Resume(g.ctx, g.command(o.Ref, 2)); e != nil {
		t.Fatal(e)
	}
	if _, e := g.registry.Bind(g.ctx, o.Ref); e != nil {
		t.Fatal(e)
	}
	if e := g.store.Archive(g.ctx, g.command(o.Ref, 3)); e != nil {
		t.Fatal(e)
	}
	if e := g.store.Recover(g.ctx, g.command(o.Ref, 4), scope.PrincipalRef{Kind: "user", ID: "alice"}); !errors.Is(e, organization.ErrConflict) {
		t.Fatal("archived recovered", e)
	}
	if e := g.store.Resume(g.ctx, g.command(o.Ref, 4)); !errors.Is(e, organization.ErrConflict) {
		t.Fatal("archived resumed", e)
	}
	other := g.newOrg(t, nil)
	bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
	bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
	g.add(t, other, bob, "owner", 1)
	command := contracts.SetMembership{Command: g.command(other.Ref, 2), Principal: scope.PrincipalRef{Kind: "user", ID: "alice"}, Role: "owner"}
	if _, e := g.store.SetOrganizationMembership(bobCtx, command); e != nil {
		t.Fatal(e)
	}
	if e := g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(other.Ref, 3), Principal: bob}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.store.SetOrganizationMembership(bobCtx, command); e == nil {
		t.Fatal("revoked governance result replay")
	}
}

func TestA81GovernanceReplay(t *testing.T) {
	g := governanceFixture(t)
	o := g.newOrg(t, nil)
	bob := scope.PrincipalRef{Kind: "user", ID: "bob"}
	bobCtx, _ := tenantContext(t, g.admin, g.app, g.tenant, bob)
	g.add(t, o, bob, "member", 1)
	bound := g.bind(t, bobCtx, o.Ref)
	m := manager(t, &postgres.Resource{Pool: g.app})
	store := orgIdempotency(t)
	key, id := rand.Text(), rand.Text()
	if e := m.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		_, _, e := store.Execute(c, "books.write", key, []byte(`{}`), func(w context.Context) ([]byte, error) { return []byte(`{"created":true}`), insert(w, id) })
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(o.Ref, 2), Principal: bob}); e != nil {
		t.Fatal(e)
	}
	for _, query := range []bool{false, true} {
		if e := m.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if query {
				_, e := store.Query(c, "books.write", key)
				return e
			}
			_, _, e := store.Execute(c, "books.write", key, []byte(`{}`), func(context.Context) ([]byte, error) { t.Fatal("revoked replay reran effect"); return nil, nil })
			return e
		}); e == nil {
			t.Fatal("revoked result read", query)
		}
	}
	if count(t, g.admin, id) != 1 {
		t.Fatal("original commit changed")
	}
	var n int
	if e := g.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_idempotency.results WHERE tenant_id=$1 AND scope_id=$2 AND key=$3 AND completed`, g.tenant, o.Ref.ID, key).Scan(&n); e != nil || n != 1 {
		t.Fatal(e, n)
	}
}

func TestGovernanceControlFailures(t *testing.T) {
	g := governanceFixture(t)
	o := g.newOrg(t, nil)
	parent := g.newEnterprise(t)
	alice := scope.PrincipalRef{ID: "alice", Kind: "user"}
	unknown := scope.PrincipalRef{ID: "unknown", Kind: "user"}
	for _, op := range []string{"set-member", "remove-member", "set-role", "transfer", "suspend", "recover", "join"} {
		t.Run("version-conflict/"+op, func(t *testing.T) {
			var e error
			switch op {
			case "set-member":
				_, e = g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 2), Principal: alice, Role: "owner"})
			case "remove-member":
				e = g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(o.Ref, 2), Principal: alice})
			case "set-role":
				e = g.store.SetEnterpriseRole(g.ctx, contracts.SetEnterpriseRole{Command: g.command(parent.Ref, 2), Principal: alice, Role: "owner"})
			case "transfer":
				_, e = g.store.AssignEnterprise(g.ctx, contracts.AssignEnterprise{Command: g.command(o.Ref, 2), EnterpriseID: &parent.Ref.ID})
			case "suspend":
				e = g.store.Suspend(g.ctx, g.command(o.Ref, 2))
			case "recover":
				e = g.store.Recover(g.ctx, g.command(o.Ref, 2), alice)
			case "join":
				child := g.newOrg(t, &parent.Ref.ID)
				e = g.store.JoinAsOwner(g.ctx, g.command(child.Ref, 2))
			}
			if !errors.Is(e, organization.ErrConflict) {
				t.Fatal(op, e)
			}
		})
	}
	t.Run("last-owner-downgrade-denied", func(t *testing.T) {
		if _, e := g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 1), Principal: alice, Role: "member"}); !errors.Is(e, organization.ErrLastOwner) {
			t.Fatal(e)
		}
	})
	t.Run("unknown-member-removal", func(t *testing.T) {
		if e := g.store.RemoveOrganizationMembership(g.ctx, contracts.RemoveMembership{Command: g.command(o.Ref, 1), Principal: unknown}); !errors.Is(e, organization.ErrNotFound) {
			t.Fatal(e)
		}
	})
	t.Run("non-tenant-member-refused", func(t *testing.T) {
		if _, e := g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 1), Principal: unknown, Role: "member"}); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
		if e := g.store.SetEnterpriseRole(g.ctx, contracts.SetEnterpriseRole{Command: g.command(parent.Ref, 1), Principal: unknown, Role: "manager"}); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
		if e := g.store.Recover(g.ctx, g.command(o.Ref, 1), unknown); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	})
	t.Run("standalone-join-refused", func(t *testing.T) {
		if e := g.store.JoinAsOwner(g.ctx, g.command(o.Ref, 1)); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	})
	t.Run("unknown-join-refused", func(t *testing.T) {
		ref := o.Ref
		ref.ID = "11111111-1111-4111-8111-111111111111"
		if e := g.store.JoinAsOwner(g.ctx, g.command(ref, 1)); !errors.Is(e, organization.ErrNotFound) {
			t.Fatal(e)
		}
	})
	t.Run("wrong-directory-kind", func(t *testing.T) {
		if _, e := g.store.ListEnterpriseOrganizations(g.ctx, o.Ref, contracts.PageRequest{Limit: 1}); !errors.Is(e, scope.ErrInvalid) {
			t.Fatal(e)
		}
	})
	t.Run("target-governance-deny", func(t *testing.T) {
		if _, e := g.admin.Exec(context.Background(), `INSERT INTO pangolin_organization.restrictions VALUES($1,$2,$3,'organization.membership.set')`, g.tenant, o.Ref.Kind, o.Ref.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := g.store.SetOrganizationMembership(g.ctx, contracts.SetMembership{Command: g.command(o.Ref, 1), Principal: alice, Role: "owner"}); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	})
	t.Run("enterprise-recovery", func(t *testing.T) {
		if e := g.store.Suspend(g.ctx, g.command(parent.Ref, 1)); e != nil {
			t.Fatal(e)
		}
		if e := g.store.Recover(g.ctx, g.command(parent.Ref, 2), alice); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("archive-busy-resource", func(t *testing.T) {
		busy := g.newOrg(t, nil)
		ctx := g.bind(t, g.ctx, busy.Ref)
		if e := g.options.Manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return insert(c, rand.Text()) }); e != nil {
			t.Fatal(e)
		}
		if e := g.store.Archive(g.ctx, g.command(busy.Ref, 1)); !errors.Is(e, organization.ErrInUse) {
			t.Fatal(e)
		}
	})
	t.Run("archive-inventory-failure", func(t *testing.T) {
		options := g.options
		boom := errors.New("inventory unavailable")
		options.UseChecker = organization.UseCheckerFunc(func(context.Context, scope.ScopeRef) (bool, error) { return false, boom })
		store, e := organization.NewStore(options)
		if e != nil {
			t.Fatal(e)
		}
		fresh := g.newOrg(t, nil)
		if e = store.Archive(g.ctx, g.command(fresh.Ref, 1)); !errors.Is(e, boom) {
			t.Fatal(e)
		}
	})
}
