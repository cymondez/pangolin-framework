//go:build integration

package m7

// A104/legacy-off: the existing no-realtime generated runtime entry must
// really start and serve HTTP for both generated profiles. The scaffold CLI
// generates both Bookstore profiles without --realtime into a runner
// directory, the migration entry seeds the pinned PostgreSQL, and the API
// binary serves its health contract while no /hub realtime path exists.
// The proof reuses the same build/test/start flow scripts/m5_templates.py runs.

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cymondez/pangolin-framework/internal/scaffold"
)

// legacyDatabase isolates one generated-runtime run in its own database of
// the pinned PostgreSQL instance: the profile's migration history must stay
// contiguous and apart from the realtime demo migrations.
func legacyDatabase(t *testing.T, adminDSN, appDSN, workerDSN, issuer, name string) map[string]string {
	t.Helper()
	const legacyDB = "pangolin_m7_legacy_"
	target, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), target.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("legacy database unreachable: %v", err)
	}
	database := legacyDB + name
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)`, database).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	quoted := `"` + database + `"`
	if exists {
		if _, err := pool.Exec(context.Background(), `DROP DATABASE `+quoted+` WITH (FORCE)`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(context.Background(), `CREATE DATABASE `+quoted); err != nil {
		t.Fatal(err)
	}
	legacyDSN := func(dsn string) string {
		t.Helper()
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		parsed.Path = "/" + database
		return parsed.String()
	}
	return map[string]string{
		"PANGOLIN_MIGRATION_DSN": legacyDSN(adminDSN),
		"PANGOLIN_DSN":           legacyDSN(appDSN),
		"PANGOLIN_AUTHORITY_DSN": legacyDSN(workerDSN),
		"PANGOLIN_ISSUER":        issuer,
		"PANGOLIN_JWKS_URL":      issuer + "/jwks.json",
	}
}

func buildGenerated(t *testing.T, project, target, output string) {
	t.Helper()
	var logs bytes.Buffer
	cmd := exec.Command("go", "-C", project, "build", "-o", output, target)
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Run(); err != nil {
		t.Fatalf("generated %s build failed: %v\n%s", target, err, logs.String())
	}
}

func runGeneratedMigration(t *testing.T, project string, environment map[string]string) {
	t.Helper()
	var logs bytes.Buffer
	cmd := exec.Command("go", "-C", project, "run", "./cmd/migrate", "--seed")
	cmd.Env = overrideEnv(t, environment)
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Run(); err != nil {
		t.Fatalf("generated migration failed: %v\n%s", err, logs.String())
	}
}

func TestA104LegacyOff(t *testing.T) {
	adminDSN := envRequired(t, "PANGOLIN_REALTIME_ADMIN_DSN")
	appDSN := envRequired(t, "PANGOLIN_REALTIME_APP_DSN")
	workerDSN := envRequired(t, "PANGOLIN_REALTIME_WORKER_DSN")
	issuer := envRequired(t, "PANGOLIN_M1_ISSUER")
	profiles := []struct {
		name  string
		label string
		args  []string
	}{
		{"legacy-tenant", "tenant-only", []string{"new", "legacy-tenant", "--template", "modular-monolith"}},
		{"legacy-org", "organization-enabled", []string{"new", "legacy-org", "--template", "modular-monolith", "--with", "organization"}},
	}
	for _, profile := range profiles {
		profile := profile
		t.Run(profile.label, func(t *testing.T) {
			// The generated migration sets differ per profile, so each run gets
			// its own database in the pinned PostgreSQL instance.
			environment := legacyDatabase(t, adminDSN, appDSN, workerDSN, issuer, profile.label)
			dir := t.TempDir()
			var output bytes.Buffer
			cli := scaffold.CLI{Dir: dir, Out: &output, Err: &output}
			if code := cli.Run(context.Background(), profile.args); code != 0 {
				t.Fatalf("scaffold generate failed: %s", output.String())
			}
			project := filepath.Join(dir, profile.name)
			if _, err := os.Stat(filepath.Join(project, "cmd", "api")); err != nil {
				t.Fatal(err)
			}
			// The m5 build/test/start flow wires one module through the CLI;
			// the module depends on no services and no realtime module.
			var wired bytes.Buffer
			moduleCLI := scaffold.CLI{Dir: project, Out: &wired, Err: &wired}
			if code := moduleCLI.Run(context.Background(), []string{"module", "add", "notices"}); code != 0 {
				t.Fatalf("module add failed: %s", wired.String())
			}
			binary := filepath.Join(dir, "api.exe")
			buildGenerated(t, project, "./cmd/api", binary)
			runGeneratedMigration(t, project, environment)
			// The generated runtime starts without any realtime module
			// assembly: the legacy entry owns the process contract.
			port := freeLoopbackPort(t)
			environment["PANGOLIN_LISTEN"] = "127.0.0.1:" + port
			cmd := exec.Command(binary)
			cmd.Dir = project
			cmd.Env = overrideEnv(t, environment)
			var logs bytes.Buffer
			cmd.Stdout = &logs
			cmd.Stderr = &logs
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			t.Cleanup(func() {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
			})
			base := "http://127.0.0.1:" + port
			client := &http.Client{Timeout: 2 * time.Second}
			started := false
			for start := time.Now(); time.Since(start) < 30*time.Second; time.Sleep(250 * time.Millisecond) {
				response, err := client.Get(base + "/livez")
				if err == nil {
					response.Body.Close()
					if response.StatusCode == http.StatusNoContent {
						started = true
						break
					}
				}
				select {
				case err := <-done:
					t.Fatalf("generated api exited before readiness: %v\n%s", err, logs.String())
				default:
				}
			}
			if !started {
				t.Fatalf("legacy entry never reported liveness\n%s", logs.String())
			}
			// Without the realtime module assembly there is no /hub path at all.
			response, err := http.Post(base+"/hub/negotiate?negotiateVersion=1", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("realtime path served without the module: %d", response.StatusCode)
			}
			// The legacy entry must keep reporting liveness on demand.
			response, err = client.Get(base + "/livez")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("liveness degraded after startup: %d", response.StatusCode)
			}
			t.Logf("legacy entry %s served http without realtime on %s", profile.label, base)
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
}
