//go:build integration

package m6

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/examples/bookstore/infrastructure"
	"github.com/cymondez/pangolin-framework/persistence/migration"
	"github.com/jackc/pgx/v5"
)

const (
	upgradeTenants        = 2
	upgradeBooksPerTenant = 30
	upgradeOrgScope       = "11111111-2222-4333-8444-555555555555"
)

// expectedUpgradeVersions is the complete pending set a previous (M1-era
// tenant-only) deployment receives on the v0.1.0 forward upgrade.
var expectedUpgradeVersions = []int64{3000001, 3000002, 3100001, 3100002, 3100003, 3100004, 3200001, 3200002, 3300001, 4000001, 4000002}

// TestM6ForwardUpgrade seeds a previous-version tenant-only database through
// the real fixture schema and app-role RLS sessions, then applies the actual
// pending migration set (framework modules, organization module and the real
// bookstore scope upgrade). It asserts owner preservation, no data loss,
// FORCE RLS with an exact policy set and no permissive union, the failed
// compatibility probe of the old API, and that the upgraded database serves
// organization-owned rows — all through fresh post-commit connections.
func TestM6ForwardUpgrade(t *testing.T) {
	d := openDrillDatabase(t, "upgrade")
	admin := d.adminPool(t)
	installBookstoreSQL(t, admin, "schema.sql")
	serverVersion := d.serverVersion(t)

	// Seed the previous deployment's business data under the app/worker roles,
	// inside their real transaction-local RLS sessions.
	seedSummary := map[string]any{}
	for tenantIndex, tenant := range []string{"tenant-a", "tenant-b"} {
		for i := 0; i < upgradeBooksPerTenant; i++ {
			id := fmt.Sprintf("upgrade-%s-%03d", tenant, i)
			softDeleted := i >= upgradeBooksPerTenant-5
			scopedSession(t, appDSNEnv, d.name, tenant, "tenant", tenant, true, func(tx pgx.Tx) error {
				if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.books(tenant_id,id,title) VALUES($1,$2,$3)", tenant, id, "M6 forward upgrade "+id); err != nil {
					return err
				}
				if softDeleted {
					if _, err := tx.Exec(context.Background(), "UPDATE pangolin.books SET deleted_at=now(),version=2 WHERE tenant_id=$1 AND id=$2", tenant, id); err != nil {
						return err
					}
				}
				if i < 3 {
					if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.outbox(event_id,tenant_id,envelope) VALUES($1,$2,$3)", "upgrade-"+id, tenant, []byte(fmt.Sprintf(`{"id":%q,"title":"drill"}`, id))); err != nil {
						return err
					}
				}
				return nil
			})
			scopedSession(t, workerDSNEnv, d.name, tenant, "tenant", tenant, true, func(tx pgx.Tx) error {
				if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.book_views(tenant_id,id,title,version) VALUES($1,$2,$3,1)", tenant, id, "view "+id); err != nil {
					return err
				}
				if i < 2 {
					if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.inbox(consumer_group,event_id,tenant_id,envelope,processed) VALUES('bookstore',$1,$2,$3,true)", "upgrade-"+id, tenant, []byte(fmt.Sprintf(`{"id":%q,"title":"drill"}`, id))); err != nil {
						return err
					}
				}
				return nil
			})
		}
		if tenantIndex == 0 {
			seedSummary["books_per_tenant"] = upgradeBooksPerTenant
			seedSummary["soft_deleted_per_tenant"] = 5
			seedSummary["outbox_rows_per_tenant"] = 3
			seedSummary["inbox_rows_per_tenant"] = 2
		}
	}

	// Previous-state evidence on fresh connections.
	preInventory := d.tableInventory(t)
	preProjection := booksProjection(t, d, "pangolin.books")
	prePolicies := d.policies(t, "pangolin", "books")
	if len(prePolicies) != 1 || prePolicies[0] != "tenant_books" {
		t.Fatalf("unexpected previous policy set on pangolin.books: %v", prePolicies)
	}
	preRLS := d.rlsInventory(t)
	if flags, ok := preRLS["pangolin.books"]; !ok || !flags[0] || !flags[1] {
		t.Fatalf("previous deployment lost FORCE RLS on books: %v", preRLS)
	}
	var preVersioned bool
	d.freshRow(t, "SELECT to_regclass('pangolin.goose_db_version') IS NOT NULL", func(row pgx.Row) error { return row.Scan(&preVersioned) })
	if preVersioned {
		t.Fatal("previous deployment must be unversioned for this drill")
	}

	// Apply the real pending migration set with the deployment runner:
	// every framework module, the organization module and the bookstore
	// scope upgrade (real embedded SQL), one deployment transaction order.
	sources, modules := moduleSources(t, true)
	sources["books"] = subMigrations(t, infrastructure.ScopeUpgrade)
	modules = append(modules, migration.ModuleMigrations{Module: "books", Directory: "books", DependsOn: []string{"scope"}})
	source := migration.JoinFS(sources)
	deployment := d.sqlDB(t)
	upgradeRunner, err := migration.NewPostgres(deployment, source, 0, currentDeploymentVersion)
	must(t, err)
	upgradeStarted := time.Now()
	must(t, upgradeRunner.Apply(context.Background(), modules))
	upgradeSeconds := time.Since(upgradeStarted).Seconds()
	compatibility, err := migration.NewPostgres(deployment, source, currentDeploymentVersion, currentDeploymentVersion)
	must(t, err)
	must(t, compatibility.CheckCompatibility(context.Background()))
	versions := d.appliedVersions(t)
	if !equalVersions(versions, expectedUpgradeVersions) {
		t.Fatalf("unexpected applied versions: %v", versions)
	}

	// Post-upgrade evidence on brand-new connections.
	postInventory := d.tableInventory(t)
	postProjection := booksProjection(t, d, "pangolin.scope_books")
	var booksGone bool
	d.freshRow(t, "SELECT to_regclass('pangolin.books') IS NULL", func(row pgx.Row) error { return row.Scan(&booksGone) })
	if !booksGone {
		t.Fatal("old pangolin.books still present after upgrade")
	}
	if _, exists := postInventory["pangolin.scope_books"]; !exists {
		t.Fatal("pangolin.scope_books missing after upgrade")
	}
	if postProjection.Count != preProjection.Count || postProjection.Checksum != preProjection.Checksum {
		t.Fatalf("forward upgrade lost or mutated business rows: %+v -> %+v", preProjection, postProjection)
	}
	for _, table := range []string{"pangolin.book_views", "pangolin.outbox", "pangolin.inbox"} {
		if preInventory[table] != postInventory[table] {
			t.Fatalf("unchanged table drifted across upgrade: %s %+v -> %+v", table, preInventory[table], postInventory[table])
		}
	}
	var ownerPreserved, ownerViolations int
	d.freshRow(t, "SELECT count(*) FROM pangolin.scope_books WHERE owner_scope_kind='tenant' AND owner_scope_id=tenant_id", func(row pgx.Row) error { return row.Scan(&ownerPreserved) })
	d.freshRow(t, "SELECT count(*) FROM pangolin.scope_books WHERE owner_scope_kind<>'tenant' OR owner_scope_id<>tenant_id", func(row pgx.Row) error { return row.Scan(&ownerViolations) })
	if ownerPreserved != upgradeTenants*upgradeBooksPerTenant || ownerViolations != 0 {
		t.Fatalf("owner backfill violated: preserved=%d violations=%d", ownerPreserved, ownerViolations)
	}
	var scopeCatalog []string
	d.freshRow(t, "SELECT COALESCE(array_agg(tenant_id||':'||scope_kind||':'||scope_id ORDER BY tenant_id||':'||scope_kind||':'||scope_id),'{}') FROM pangolin.resource_scopes", func(row pgx.Row) error { return row.Scan(&scopeCatalog) })
	if len(scopeCatalog) != upgradeTenants || scopeCatalog[0] != "tenant-a:tenant:tenant-a" || scopeCatalog[1] != "tenant-b:tenant:tenant-b" {
		t.Fatalf("upgrade guessed or lost owner scopes: %v", scopeCatalog)
	}
	postPolicies := d.policies(t, "pangolin", "scope_books")
	if len(postPolicies) != 1 || postPolicies[0] != "exact_owner" {
		t.Fatalf("permissive policy union risk: scope_books policies %v", postPolicies)
	}
	postRLS := d.rlsInventory(t)
	if flags, ok := postRLS["pangolin.scope_books"]; !ok || !flags[0] || !flags[1] {
		t.Fatalf("upgraded table lost FORCE RLS: %v", postRLS)
	}

	// App-role probes against the upgraded, exact-owner schema.
	probes := map[string]any{}
	var tenantARows, tenantBRows, crossTenantRows int
	scopedSession(t, appDSNEnv, d.name, "tenant-a", "tenant", "tenant-a", false, func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books").Scan(&tenantARows); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books WHERE tenant_id='tenant-b'").Scan(&crossTenantRows)
	})
	scopedSession(t, appDSNEnv, d.name, "tenant-b", "tenant", "tenant-b", false, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books").Scan(&tenantBRows)
	})
	if tenantARows != upgradeBooksPerTenant || tenantBRows != upgradeBooksPerTenant || crossTenantRows != 0 {
		t.Fatalf("RLS isolation after upgrade: a=%d b=%d cross=%d", tenantARows, tenantBRows, crossTenantRows)
	}
	probes["tenant_a_rows"], probes["tenant_b_rows"], probes["cross_tenant_rows_visible"] = tenantARows, tenantBRows, crossTenantRows

	// The upgraded schema serves organization-owned rows through the neutral
	// catalog; tenant scopes never see them.
	_, err = admin.Exec(context.Background(), "INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES('tenant-a','org.organization',$1) ON CONFLICT DO NOTHING", upgradeOrgScope)
	must(t, err)
	scopedSession(t, appDSNEnv, d.name, "tenant-a", "org.organization", upgradeOrgScope, true, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "INSERT INTO pangolin.scope_books(tenant_id,owner_scope_kind,owner_scope_id,id,title) VALUES('tenant-a','org.organization',$1,'org-owned-book','upgraded organization owner')", upgradeOrgScope)
		return err
	})
	var orgRows, tenantSeesOrgRows int
	scopedSession(t, appDSNEnv, d.name, "tenant-a", "tenant", "tenant-a", false, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books WHERE owner_scope_kind='org.organization'").Scan(&tenantSeesOrgRows)
	})
	scopedSession(t, appDSNEnv, d.name, "tenant-a", "org.organization", upgradeOrgScope, false, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books WHERE owner_scope_kind='org.organization'").Scan(&orgRows)
	})
	if orgRows != 1 || tenantSeesOrgRows != 0 {
		t.Fatalf("organization ownership isolation failed: org=%d tenant-sees-org=%d", orgRows, tenantSeesOrgRows)
	}
	probes["org_owner_insert_ok"] = true
	probes["org_scope_reads_own_rows"] = orgRows
	probes["tenant_scope_cannot_read_org_rows"] = tenantSeesOrgRows == 0

	// The old tenant-only API's compatibility probe must refuse the upgraded
	// schema instead of silently continuing.
	oldProbeFailed := false
	scopedSession(t, appDSNEnv, d.name, "tenant-a", "tenant", "tenant-a", false, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), "SELECT id FROM pangolin.books LIMIT 0"); err != nil {
			oldProbeFailed = true
		}
		return nil
	})
	if !oldProbeFailed {
		t.Fatal("old tenant-only API probe still accepted the upgraded schema")
	}
	probes["old_api_probe_failed"] = oldProbeFailed

	writeEvidence(t, "upgrade-drill.json", map[string]any{
		"database":       d.name,
		"server_version": serverVersion,
		"previous_deployment": map[string]any{
			"profile":            "tenant-only M1 fixture schema (examples/bookstore/infrastructure/sql/schema.sql)",
			"tables":             preInventory,
			"books_projection":   preProjection,
			"policies_on_books":  prePolicies,
			"force_rls_on_books": preRLS["pangolin.books"],
			"applied_versions":   []int64{},
			"seed":               seedSummary,
		},
		"upgrade": map[string]any{
			"modules":           moduleNames(modules),
			"versions_applied":  versions,
			"expected_versions": expectedUpgradeVersions,
			"seconds":           upgradeSeconds,
			"compatibility":     fmt.Sprintf("compatible@%d", currentDeploymentVersion),
		},
		"upgraded_deployment": map[string]any{
			"books_table_gone":         booksGone,
			"scope_books_exists":       true,
			"scope_books_projection":   postProjection,
			"projection_match":         postProjection.Count == preProjection.Count && postProjection.Checksum == preProjection.Checksum,
			"unchanged_tables":         map[string]tableState{"pangolin.book_views": postInventory["pangolin.book_views"], "pangolin.outbox": postInventory["pangolin.outbox"], "pangolin.inbox": postInventory["pangolin.inbox"]},
			"owner_preserved_rows":     ownerPreserved,
			"owner_violations":         ownerViolations,
			"resource_scopes":          scopeCatalog,
			"policies_on_scope_books":  postPolicies,
			"permissive_policy_union":  len(postPolicies) != 1,
			"force_rls_on_scope_books": postRLS["pangolin.scope_books"],
		},
		"post_upgrade_probes": probes,
	})
}

// booksProjection digests exactly the columns a previous-version business row
// carried, so pre- and post-upgrade comparisons catch loss or mutation
// without depending on newly added owner columns.
func booksProjection(t *testing.T, d *drillDatabase, table string) tableState {
	t.Helper()
	var state tableState
	d.freshRow(t, "SELECT count(*),COALESCE(md5(string_agg(id||'|'||tenant_id||'|'||title||'|'||version||'|'||COALESCE(deleted_at::text,''),'|' ORDER BY id)),md5('')) FROM "+table, func(row pgx.Row) error {
		return row.Scan(&state.Count, &state.Checksum)
	})
	return state
}

func equalVersions(actual, expected []int64) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return false
		}
	}
	return true
}

func moduleNames(modules []migration.ModuleMigrations) []string {
	names := make([]string, 0, len(modules))
	for _, module := range modules {
		names = append(names, module.Module)
	}
	return names
}
