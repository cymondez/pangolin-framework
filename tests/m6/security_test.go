//go:build integration

package m6

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cymondez/pangolin-framework/examples/bookstore/application"
	"github.com/cymondez/pangolin-framework/examples/bookstore/domain"
	"github.com/cymondez/pangolin-framework/examples/bookstore/infrastructure"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/jackc/pgx/v5"
)

const (
	securityTenantsPerScope = 25
	securityOrgRows         = 3
	securityOrgScope        = "99999999-8888-4777-8666-555555555555"
)

// injectionPayload is a hostile business value: SQL injection, quote
// escaping, backslashes and multi-byte text in one parameterized round trip.
const injectionPayload = `M6 drill '); DROP TABLE pangolin.scope_books; -- ❤️ "double" 'single' \u4e2d\u6587`

// TestM6SecurityDrill executes the release security review's database-level
// control set on an isolated organization-enabled deployment: fixture role
// posture, the FORCE RLS invariant, tenant and organization IDOR probes
// through real UOW sessions and parameterized application writes, and an
// injection round trip proving payloads store as data and never execute.
// The pipeline-layer controls (JWT, policy, transport redaction, cache,
// observability) are the curated executable tests the drill script runs and
// maps in docs/security/release-review.md.
func TestM6SecurityDrill(t *testing.T) {
	d := openDrillDatabase(t, "security")
	admin := d.adminPool(t)
	deployModules(t, d.sqlDB(t), true)
	installBookstoreSQL(t, admin, "schema.sql")
	installBookstoreSQL(t, admin, "scoped-schema.sql")
	serverVersion := d.serverVersion(t)

	// Fixture role posture: no superuser, no BYPASSRLS, no role/database
	// creation for the application roles, no business tables owned by them.
	roles := d.roleAttributes(t)
	if len(roles) != 3 {
		t.Fatalf("unexpected role posture set: %v", roles)
	}
	for role, attributes := range roles {
		if attributes["superuser"] || attributes["bypassrls"] {
			t.Fatalf("role %s is privileged: %v", role, attributes)
		}
		if (role == "pangolin_app" || role == "pangolin_worker") && (attributes["createrole"] || attributes["createdb"]) {
			t.Fatalf("application role %s can escalate: %v", role, attributes)
		}
	}
	if unowned := d.unownedTables(t); unowned != 0 {
		t.Fatalf("application roles own business tables: %d", unowned)
	}

	// FORCE RLS invariant across every owner-scoped table.
	rls := d.rlsInventory(t)
	violations := []string{}
	for table, flags := range rls {
		if !flags[0] || !flags[1] {
			violations = append(violations, table)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("tables without FORCE RLS: %v", violations)
	}
	var membershipsRLS bool
	d.freshRow(t, `SELECT c.relrowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='pangolin_security' AND c.relname='tenant_memberships'`, func(row pgx.Row) error { return row.Scan(&membershipsRLS) })

	// Seed the IDOR fixture: same business id in both tenants and in one
	// organization of tenant-a, through real app-role sessions.
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		_, err := admin.Exec(context.Background(), "INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,'tenant',$1) ON CONFLICT DO NOTHING", tenant)
		must(t, err)
		scopedSession(t, appDSNEnv, d.name, tenant, "tenant", tenant, true, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.scope_books(tenant_id,owner_scope_kind,owner_scope_id,id,title) VALUES($1,'tenant',$1,'shared-book',$2)", tenant, tenant+" shared"); err != nil {
				return err
			}
			for i := 0; i < securityTenantsPerScope-1; i++ {
				id := fmt.Sprintf("security-%s-%03d", tenant, i)
				if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.scope_books(tenant_id,owner_scope_kind,owner_scope_id,id,title) VALUES($1,'tenant',$1,$2,$3)", tenant, id, "M6 security "+id); err != nil {
					return err
				}
			}
			return nil
		})
	}
	_, err := admin.Exec(context.Background(), "INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES('tenant-a','org.organization',$1) ON CONFLICT DO NOTHING", securityOrgScope)
	must(t, err)
	scopedSession(t, appDSNEnv, d.name, "tenant-a", "org.organization", securityOrgScope, true, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.scope_books(tenant_id,owner_scope_kind,owner_scope_id,id,title) VALUES('tenant-a','org.organization',$1,'shared-book','organization shared')", securityOrgScope); err != nil {
			return err
		}
		for i := 0; i < securityOrgRows-1; i++ {
			id := fmt.Sprintf("security-org-%03d", i)
			if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.scope_books(tenant_id,owner_scope_kind,owner_scope_id,id,title) VALUES('tenant-a','org.organization',$1,$2,$3)", securityOrgScope, id, "M6 security org "+id); err != nil {
				return err
			}
		}
		return nil
	})

	// IDOR probes through the real framework path: scoped application on the
	// UOW with a tenant binding, plus raw SQL missing owner predicates.
	pool := d.pool(t, appDSNEnv)
	manager := drillManager(t, pool)
	scoped := application.NewScopedBooks(manager, infrastructure.ScopedRepository{}, infrastructure.ScopedRepository{})
	tenantA := bindTenantScope(t, "tenant-a", "alice")
	tenantB := bindTenantScope(t, "tenant-b", "bob")
	bookA, err := scoped.Get(tenantA, "shared-book")
	must(t, err)
	if bookA.Title != "tenant-a shared" {
		t.Fatalf("tenant-a read wrong owner row: %+v", bookA)
	}
	bookB, err := scoped.Get(tenantB, "shared-book")
	must(t, err)
	if bookB.Title != "tenant-b shared" {
		t.Fatalf("tenant-b read wrong owner row: %+v", bookB)
	}
	probe := func(t *testing.T, ctx context.Context) map[string]int64 {
		t.Helper()
		var visible, filterless, write, softDelete int64
		if err := manager.Within(ctx, uow.Options{}, func(work context.Context) error {
			executor, err := postgres.Current(work, false)
			if err != nil {
				return err
			}
			if err := executor.QueryRow(work, "SELECT count(*) FROM pangolin.scope_books").Scan(&visible); err != nil {
				return err
			}
			return executor.QueryRow(work, "SELECT count(*) FROM pangolin.scope_books WHERE tenant_id='tenant-b'").Scan(&filterless)
		}); err != nil {
			t.Fatal(err)
		}
		if err := manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			executor, err := postgres.Current(work, true)
			if err != nil {
				return err
			}
			tag, err := executor.Exec(work, "UPDATE pangolin.scope_books SET title='forged' WHERE tenant_id='tenant-b'")
			if err != nil {
				return err
			}
			write = tag.RowsAffected()
			tag, err = executor.Exec(work, "UPDATE pangolin.scope_books SET deleted_at=now() WHERE tenant_id='tenant-b'")
			if err != nil {
				return err
			}
			softDelete = tag.RowsAffected()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return map[string]int64{"visible": visible, "cross_tenant_filterless": filterless, "cross_tenant_write": write, "cross_tenant_soft_delete": softDelete}
	}
	tenantAProbe := probe(t, tenantA)
	if tenantAProbe["visible"] != securityTenantsPerScope || tenantAProbe["cross_tenant_filterless"] != 0 || tenantAProbe["cross_tenant_write"] != 0 || tenantAProbe["cross_tenant_soft_delete"] != 0 {
		t.Fatalf("tenant-a IDOR probe failed: %v", tenantAProbe)
	}

	// Organization ownership isolation at the database layer: the org scope
	// sees only its own rows; tenant scopes never see organization rows.
	var orgRows, tenantSeesOrgRows, otherTenantRows int
	scopedSession(t, appDSNEnv, d.name, "tenant-a", "org.organization", securityOrgScope, false, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books WHERE owner_scope_kind='org.organization'").Scan(&orgRows)
	})
	scopedSession(t, appDSNEnv, d.name, "tenant-a", "tenant", "tenant-a", false, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books WHERE owner_scope_kind='org.organization'").Scan(&tenantSeesOrgRows)
	})
	scopedSession(t, appDSNEnv, d.name, "tenant-b", "org.organization", securityOrgScope, false, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books").Scan(&otherTenantRows)
	})
	if orgRows != securityOrgRows || tenantSeesOrgRows != 0 || otherTenantRows != 0 {
		t.Fatalf("organization isolation failed: org=%d tenant-sees-org=%d other-tenant-org=%d", orgRows, tenantSeesOrgRows, otherTenantRows)
	}

	// Injection round trip through the real tenant-only application path.
	tenantBooks := application.NewBooks(manager, infrastructure.Repository{}, infrastructure.Repository{})
	if _, err := tenantBooks.Create(tenantA, "injection-probe-001", injectionPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := tenantBooks.Get(tenantA, "x' OR '1'='1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("hostile id did not resolve to not-found: %v", err)
	}
	var storedTitle string
	var outboxRows int
	d.freshRow(t, "SELECT title FROM pangolin.books WHERE tenant_id='tenant-a' AND id='injection-probe-001'", func(row pgx.Row) error { return row.Scan(&storedTitle) })
	d.freshRow(t, "SELECT count(*) FROM pangolin.outbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->>'id'='injection-probe-001'", func(row pgx.Row) error { return row.Scan(&outboxRows) })
	var scopeBooksIntact, booksIntact bool
	d.freshRow(t, "SELECT to_regclass('pangolin.scope_books') IS NOT NULL", func(row pgx.Row) error { return row.Scan(&scopeBooksIntact) })
	d.freshRow(t, "SELECT to_regclass('pangolin.books') IS NOT NULL", func(row pgx.Row) error { return row.Scan(&booksIntact) })
	if storedTitle != injectionPayload || !scopeBooksIntact || !booksIntact || outboxRows != 1 {
		t.Fatalf("injection round trip failed: stored=%q scope_books=%v books=%v outbox=%d", storedTitle, scopeBooksIntact, booksIntact, outboxRows)
	}

	writeEvidence(t, "security-drill.json", map[string]any{
		"database":       d.name,
		"server_version": serverVersion,
		"roles":          roles,
		"unowned_tables": 0,
		"rls":            rls,
		"rls_violations": violations,
		"tenant_membership_authority_metadata": map[string]any{
			"table":              "pangolin_security.tenant_memberships",
			"row_level_security": membershipsRLS,
			"note":               "authority metadata readable by the fixture authority roles per migration 3000001; reviewed with remaining risk in docs/security/release-review.md",
		},
		"idor": map[string]any{
			"same_id_tenant_a":         bookA.Title,
			"same_id_tenant_b":         bookB.Title,
			"tenant_a_visible_rows":    tenantAProbe["visible"],
			"cross_tenant_filterless":  tenantAProbe["cross_tenant_filterless"],
			"cross_tenant_write":       tenantAProbe["cross_tenant_write"],
			"cross_tenant_soft_delete": tenantAProbe["cross_tenant_soft_delete"],
			"org_scope_rows":           orgRows,
			"tenant_sees_org_rows":     tenantSeesOrgRows,
			"other_tenant_org_rows":    otherTenantRows,
		},
		"injection": map[string]any{
			"payload":             injectionPayload,
			"stored_exactly":      storedTitle == injectionPayload,
			"scope_books_intact":  scopeBooksIntact,
			"books_intact":        booksIntact,
			"outbox_rows":         outboxRows,
			"hostile_id_lookup":   "NOT_FOUND via parameterized predicate",
			"http_layer_controls": "transport/http unit tests run by the drill (see security-tests mapping)",
		},
	})
}
