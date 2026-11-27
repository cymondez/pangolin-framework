//go:build integration

package m6

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/jackc/pgx/v5"
)

// pgClient locates real pg_dump/pg_restore binaries. Host mode uses the
// runner's client tools (version-checked); container mode shells into the
// already-running fixture postgres service through the Docker project — the
// fallback for runners without host tools. When neither is available the drill
// fails closed; docs/runbooks/release.md documents the host-run procedure and
// provenance the release gate must then validate.
type pgClient struct {
	mode      string
	binDir    string
	container []string
}

func resolvePgClient(t *testing.T) *pgClient {
	t.Helper()
	mode := os.Getenv("PANGOLIN_M6_PG_MODE")
	container := []string{}
	if raw := os.Getenv("PANGOLIN_M6_PG_CONTAINER_ARGV"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &container); err != nil || len(container) == 0 {
			t.Fatalf("invalid PANGOLIN_M6_PG_CONTAINER_ARGV: %v", err)
		}
	}
	if mode == "" {
		if _, err := exec.LookPath("pg_dump"); err == nil {
			if _, err := exec.LookPath("pg_restore"); err == nil {
				mode = "host"
			}
		}
		if mode == "" && len(container) > 0 {
			mode = "container"
		}
		if mode == "" {
			// A single operator-provided compose project name is enough for
			// bare `go test` runs (the cumulative gate); it must name the
			// inventoried fixture project, never an unrelated one.
			if project := os.Getenv("PANGOLIN_M6_COMPOSE_PROJECT"); project != "" {
				if !composeProjectPattern.MatchString(project) {
					t.Fatalf("PANGOLIN_M6_COMPOSE_PROJECT must be a pangolin-m6-* project: %s", project)
				}
				composeFile := os.Getenv("PANGOLIN_M6_COMPOSE_FILE")
				if composeFile == "" {
					composeFile = "deploy/compose/m1.yaml"
				}
				if !filepath.IsAbs(composeFile) {
					composeFile = filepath.Join(moduleRoot(t), composeFile)
				}
				container = []string{"docker", "compose", "-p", project, "-f", composeFile, "exec", "-T", "postgres"}
				mode = "container"
			}
		}
	}
	if mode == "host" {
		return &pgClient{mode: mode, binDir: os.Getenv("PANGOLIN_M6_PG_BIN_DIR")}
	}
	if mode == "container" {
		if len(container) == 0 {
			t.Fatal("container pg client requires PANGOLIN_M6_PG_CONTAINER_ARGV")
		}
		return &pgClient{mode: mode, container: container}
	}
	t.Fatal("no pg client tools available: install host pg_dump/pg_restore (>= server major) or set PANGOLIN_M6_PG_CONTAINER_ARGV/PANGOLIN_M6_COMPOSE_PROJECT; see docs/runbooks/release.md for the host-run report procedure")
	return nil
}

var composeProjectPattern = regexp.MustCompile(`^pangolin-m6-[a-z0-9-]+$`)

func (c *pgClient) binary(t *testing.T, program string) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	if c.binDir != "" {
		return filepath.Join(c.binDir, program+suffix)
	}
	path, err := exec.LookPath(program)
	if err != nil {
		t.Fatalf("%s not found on PATH: %v", program, err)
	}
	return path
}

func (c *pgClient) command(t *testing.T, program string, args []string) []string {
	t.Helper()
	if c.mode == "host" {
		return append([]string{c.binary(t, program)}, args...)
	}
	return append(append([]string{}, c.container...), append([]string{program}, args...)...)
}

// dsn points the pg client at the given database of the fixture server. In
// container mode the client runs inside the postgres service, so the DSN host
// becomes the container loopback.
func (c *pgClient) dsn(t *testing.T, raw, database string) string {
	t.Helper()
	parsed, err := url.Parse(dsnWithDatabase(t, raw, database))
	must(t, err)
	if c.mode == "container" {
		parsed.Host = "127.0.0.1:5432"
	}
	return parsed.String()
}

func (c *pgClient) run(t *testing.T, name string, argv []string, stdin, stdout *os.File) (float64, int) {
	t.Helper()
	log, err := os.Create(filepath.Join(evidenceDir(t), "pg-"+name+".log"))
	must(t, err)
	defer func() { _ = log.Close() }()
	command := exec.Command(argv[0], argv[1:]...)
	command.Env = os.Environ()
	if stdin != nil {
		command.Stdin = stdin
	}
	if stdout != nil {
		command.Stdout = stdout
	}
	command.Stderr = log
	started := time.Now()
	runErr := command.Run()
	seconds := time.Since(started).Seconds()
	exitCode := 0
	if runErr != nil {
		exitCode = -1
		if command.ProcessState != nil {
			exitCode = command.ProcessState.ExitCode()
		}
		raw, _ := os.ReadFile(log.Name())
		t.Fatalf("pg command %s failed (exit %d): %v\n%s", name, exitCode, runErr, raw)
	}
	return seconds, exitCode
}

func (c *pgClient) version(t *testing.T, program string) string {
	t.Helper()
	argv := c.command(t, program, []string{"--version"})
	var output strings.Builder
	log, err := os.Create(filepath.Join(evidenceDir(t), "pg-"+program+"-version.log"))
	must(t, err)
	defer func() { _ = log.Close() }()
	command := exec.Command(argv[0], argv[1:]...)
	command.Stdout = &output
	command.Stderr = log
	if err := command.Run(); err != nil {
		t.Fatalf("%s --version failed: %v", program, err)
	}
	return strings.TrimSpace(output.String())
}

// checkMajor fails closed when the pg client major version is older than the
// server major version (pg_dump/pg_restore refuse newer-server dumps anyway).
func (c *pgClient) checkMajor(t *testing.T, version, serverVersion string) {
	t.Helper()
	clientMajor, ok := majorOf(version)
	if !ok {
		t.Fatalf("cannot parse pg client version %q", version)
	}
	serverMajor, ok := majorOf(serverVersion)
	if !ok {
		t.Fatalf("cannot parse server version %q", serverVersion)
	}
	if clientMajor < serverMajor {
		t.Fatalf("pg client major %d is older than server major %d", clientMajor, serverMajor)
	}
}

func majorOf(version string) (int, bool) {
	rest := version
	if index := strings.LastIndex(rest, ") "); index >= 0 {
		rest = rest[index+2:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, false
	}
	major, _, found := strings.Cut(fields[0], ".")
	if !found && major == "" {
		return 0, false
	}
	value := 0
	for _, digit := range major {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		value = value*10 + int(digit-'0')
	}
	return value, len(major) > 0
}

const backupOrgScope = "aaaaaaa1-bbbb-4ccc-8ddd-eeeeeeeeeeee"

// TestM6BackupRestore exercises the real release backup and restore path on a
// seeded organization-enabled deployment: pg_dump -Fc of the unique source
// database, pg_restore into a new database, then a fresh-connection
// comparison of every table (owner/keys/counts/checksums), RLS policies and
// flags, role posture, and an app-role probe on the restored database, with
// measured RPO window and RTO.
func TestM6BackupRestore(t *testing.T) {
	client := resolvePgClient(t)
	source := openDrillDatabase(t, "backup")
	admin := source.adminPool(t)
	deployModules(t, source.sqlDB(t), true)
	installBookstoreSQL(t, admin, "scoped-schema.sql")
	serverVersion := source.serverVersion(t)
	dumpVersion := client.version(t, "pg_dump")
	restoreVersion := client.version(t, "pg_restore")
	client.checkMajor(t, dumpVersion, serverVersion)
	client.checkMajor(t, restoreVersion, serverVersion)

	// Seed deterministic business and governance data through real role
	// sessions and the governance control path.
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		_, err := admin.Exec(context.Background(), "INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,'tenant',$1) ON CONFLICT DO NOTHING", tenant)
		must(t, err)
		for i := 0; i < 25; i++ {
			id := fmt.Sprintf("backup-%s-%03d", tenant, i)
			scopedSession(t, appDSNEnv, source.name, tenant, "tenant", tenant, true, func(tx pgx.Tx) error {
				_, err := tx.Exec(context.Background(), "INSERT INTO pangolin.scope_books(tenant_id,owner_scope_kind,owner_scope_id,id,title) VALUES($1,'tenant',$1,$2,$3)", tenant, id, "M6 backup "+id)
				return err
			})
		}
		scopedSession(t, appDSNEnv, source.name, tenant, "tenant", tenant, true, func(tx pgx.Tx) error {
			for i := 0; i < 4; i++ {
				id := fmt.Sprintf("backup-outbox-%s-%d", tenant, i)
				if _, err := tx.Exec(context.Background(), "INSERT INTO pangolin.scope_outbox(event_id,tenant_id,scope_kind,scope_id,envelope) VALUES($1,$2,'tenant',$2,$3)", id, tenant, []byte(fmt.Sprintf(`{"id":%q}`, id))); err != nil {
					return err
				}
			}
			return nil
		})
	}
	_, err := admin.Exec(context.Background(), "INSERT INTO pangolin_security.tenant_memberships VALUES('tenant-a','user','alice','active',1) ON CONFLICT DO NOTHING")
	must(t, err)
	_, err = admin.Exec(context.Background(), "INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES('tenant-a','org.organization',$1) ON CONFLICT DO NOTHING", backupOrgScope)
	must(t, err)
	_, err = admin.Exec(context.Background(), "INSERT INTO pangolin_organization.enterprises(tenant_id,id,slug) VALUES('tenant-a','backup-enterprise','backup-enterprise')")
	must(t, err)
	_, err = admin.Exec(context.Background(), "INSERT INTO pangolin_organization.organizations(tenant_id,id,enterprise_id,slug) VALUES('tenant-a',$1,'backup-enterprise','backup-org')", backupOrgScope)
	must(t, err)
	_, err = admin.Exec(context.Background(), "INSERT INTO pangolin_organization.organization_memberships(tenant_id,organization_id,principal_kind,principal_id,role) VALUES('tenant-a',$1,'user','alice','owner')", backupOrgScope)
	must(t, err)
	_, err = admin.Exec(context.Background(), "INSERT INTO pangolin_organization.owners(tenant_id,scope_kind,scope_id,principal_kind,principal_id) VALUES('tenant-a','org.organization',$1,'user','alice')", backupOrgScope)
	must(t, err)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("backup-org-%03d", i)
		scopedSession(t, appDSNEnv, source.name, "tenant-a", "org.organization", backupOrgScope, true, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), "INSERT INTO pangolin.scope_books(tenant_id,owner_scope_kind,owner_scope_id,id,title) VALUES('tenant-a','org.organization',$1,$2,$3)", backupOrgScope, id, "M6 backup org "+id)
			return err
		})
	}

	var lastWriteAt time.Time
	source.freshRow(t, "SELECT now()", func(row pgx.Row) error { return row.Scan(&lastWriteAt) })
	sourceInventory := source.tableInventory(t)
	sourceRLS := source.rlsInventory(t)
	sourcePolicies := source.policies(t, "pangolin", "scope_books")
	sourceRoles := source.roleAttributes(t)
	sourceUnowned := source.unownedTables(t)
	if sourceUnowned != 0 {
		t.Fatalf("seeded deployment has app/worker-owned tables: %d", sourceUnowned)
	}

	// The real backup: pg_dump -Fc streamed into the evidence directory.
	dumpPath := filepath.Join(evidenceDir(t), "backup.dump")
	dumpFile, err := os.Create(dumpPath)
	must(t, err)
	dumpStarted := time.Now()
	dumpSeconds, dumpExit := client.run(t, "dump", client.command(t, "pg_dump", []string{"--format=custom", "--dbname=" + client.dsn(t, source.adminDSN, source.name)}), nil, dumpFile)
	must(t, dumpFile.Close())
	rpoWindow := dumpStarted.Sub(lastWriteAt).Seconds()
	dumpStats, err := os.Stat(dumpPath)
	must(t, err)
	if dumpStats.Size() == 0 {
		t.Fatal("pg_dump produced an empty archive")
	}

	target := openDrillDatabase(t, "restore")
	restoreStdin, err := os.Open(dumpPath)
	must(t, err)
	defer func() { _ = restoreStdin.Close() }()
	restoreStarted := time.Now()
	restoreSeconds, restoreExit := client.run(t, "restore", client.command(t, "pg_restore", []string{"--dbname=" + client.dsn(t, source.adminDSN, target.name), "--exit-on-error"}), restoreStdin, nil)

	// Post-restore verification happens only on brand-new connections to the
	// restored database: full table equality (owner/keys/counts/checksums),
	// policy/flag restoration, role posture and a live app-role probe.
	restoredInventory := target.tableInventory(t)
	mismatches := []string{}
	for table, state := range sourceInventory {
		restored, ok := restoredInventory[table]
		switch {
		case !ok:
			mismatches = append(mismatches, table+" missing after restore")
		case restored != state:
			mismatches = append(mismatches, fmt.Sprintf("%s: source %+v restored %+v", table, state, restored))
		}
	}
	for table := range restoredInventory {
		if _, ok := sourceInventory[table]; !ok {
			mismatches = append(mismatches, table+" unexpected after restore")
		}
	}
	if len(mismatches) > 0 {
		t.Fatalf("restore comparison failed:\n%s", strings.Join(mismatches, "\n"))
	}
	restoredRLS := target.rlsInventory(t)
	if len(restoredRLS) != len(sourceRLS) {
		t.Fatalf("restored RLS table set differs: %d vs %d", len(restoredRLS), len(sourceRLS))
	}
	for table, flags := range sourceRLS {
		if restoredRLS[table] != flags {
			t.Fatalf("restored RLS flags differ on %s: %v vs %v", table, flags, restoredRLS[table])
		}
	}
	restoredPolicies := target.policies(t, "pangolin", "scope_books")
	if strings.Join(restoredPolicies, ",") != strings.Join(sourcePolicies, ",") {
		t.Fatalf("restored policy set differs: %v vs %v", restoredPolicies, sourcePolicies)
	}
	restoredRoles := target.roleAttributes(t)
	if fmt.Sprint(restoredRoles) != fmt.Sprint(sourceRoles) {
		t.Fatalf("restored role posture differs: %v vs %v", restoredRoles, sourceRoles)
	}
	restoredUnowned := target.unownedTables(t)
	if restoredUnowned != 0 {
		t.Fatalf("restored deployment has app/worker-owned tables: %d", restoredUnowned)
	}
	var tenantARows int
	scopedSession(t, appDSNEnv, target.name, "tenant-a", "tenant", "tenant-a", false, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pangolin.scope_books").Scan(&tenantARows)
	})
	if tenantARows != 25 {
		t.Fatalf("restored app-role probe sees %d tenant-a rows, want 25", tenantARows)
	}
	compatibilitySource, _ := moduleSources(t, true)
	compatibility, err := migration.NewPostgres(target.sqlDB(t), migration.JoinFS(compatibilitySource), currentDeploymentVersion, currentDeploymentVersion)
	must(t, err)
	must(t, compatibility.CheckCompatibility(context.Background()))
	versionsRestored := target.appliedVersions(t)
	if len(versionsRestored) == 0 || versionsRestored[len(versionsRestored)-1] != currentDeploymentVersion {
		t.Fatalf("restored migration version history incomplete: %v", versionsRestored)
	}
	rto := time.Since(restoreStarted).Seconds()

	writeEvidence(t, "backup-restore.json", map[string]any{
		"database":         source.name,
		"restore_database": target.name,
		"server_version":   serverVersion,
		"pg": map[string]any{
			"mode":               client.mode,
			"pg_dump_version":    dumpVersion,
			"pg_restore_version": restoreVersion,
			"dump_argv":          client.command(t, "pg_dump", []string{"--format=custom", "--dbname=" + client.dsn(t, source.adminDSN, source.name)}),
			"restore_argv":       client.command(t, "pg_restore", []string{"--dbname=" + client.dsn(t, source.adminDSN, target.name), "--exit-on-error"}),
			"dump_exit":          dumpExit,
			"restore_exit":       restoreExit,
			"dump_seconds":       dumpSeconds,
			"dump_bytes":         dumpStats.Size(),
			"dump_sha256":        fileSha256(t, dumpPath),
		},
		"seed": map[string]any{
			"tenants":                 []string{"tenant-a", "tenant-b"},
			"scope_books_per_tenant":  25,
			"org_owned_scope_books":   10,
			"scope_outbox_per_tenant": 4,
			"governance":              "enterprise, organization, owner membership, org scope catalog row",
		},
		"source": map[string]any{
			"tables":                  sourceInventory,
			"rls":                     sourceRLS,
			"policies_on_scope_books": sourcePolicies,
			"roles":                   sourceRoles,
			"unowned_tables":          sourceUnowned,
		},
		"restore": map[string]any{
			"tables_compared":        len(sourceInventory),
			"mismatches":             mismatches,
			"rls_flags_match":        true,
			"policies_match":         true,
			"roles_match":            true,
			"app_role_tenant_a_rows": tenantARows,
			"compatibility":          fmt.Sprintf("compatible@%d", currentDeploymentVersion),
			"versions":               versionsRestored,
		},
		"rpo": map[string]any{
			"window_seconds": rpoWindow,
			"rows_missing":   0,
			"method":         "wall clock from last committed seed write to pg_dump snapshot start; a consistent pg_dump snapshot means zero committed rows can be missing; verified by full table equality",
		},
		"rto": map[string]any{
			"restore_seconds": restoreSeconds,
			"verify_seconds":  rto - restoreSeconds,
			"total_seconds":   rto,
			"method":          "wall clock from restore start to fresh-connection verified equality of every restored table",
		},
	})
}
