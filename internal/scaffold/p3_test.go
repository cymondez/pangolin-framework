package scaffold

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestP3ServicePresetBuildsCleanProject(t *testing.T) {
	dir := create(t, false)

	argsCases := [][]string{
		{"module", "add", "catalog", "--preset", "service", "--depends", "persistence,authorization"},
		{"module", "add", "prices", "--depends", "persistence,authorization", "--preset", "service"},
		{"module", "add", "orders", "--depends", "catalog", "--preset", "service"},
	}
	for _, args := range argsCases {
		if code, out := invoke(t, dir, args...); code != 0 {
			t.Fatalf("%v: %s", args, out)
		}
	}

	for _, name := range []string{"catalog", "prices", "orders"} {
		for _, relative := range []string{
			"module.go",
			"application/contracts.go",
			"application/service.go",
			"application/service_test.go",
			"README.md",
		} {
			if _, err := os.Stat(filepath.Join(dir, "modules", name, relative)); err != nil {
				t.Fatalf("%s preset missing %s: %v", name, relative, err)
			}
		}
	}

	for _, name := range []string{"catalog", "prices", "orders"} {
		moduleRaw, err := os.ReadFile(filepath.Join(dir, "modules", name, "module.go"))
		if err != nil {
			t.Fatal(err)
		}
		raw := string(moduleRaw)
		if !strings.Contains(raw, "module.AddSingleton") || !strings.Contains(raw, "NewService") {
			t.Fatalf("%s module lacks typed constructor registration:\n%s", name, raw)
		}
	}
	ordersRaw, err := os.ReadFile(filepath.Join(dir, "modules", "orders", "module.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ordersRaw), `m_catalog "example.com/bookstore/modules/catalog"`) || !strings.Contains(string(ordersRaw), `module.Of[m_catalog.Module]()`) {
		t.Fatalf("orders service preset misses registered user-module dependency:\n%s", ordersRaw)
	}

	build := exec.Command("go", "-C", dir, "test", "./...")
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("generated clean project module preset does not test:\n%s", out)
	}
}

func TestP3DefaultSkeletonAndCRUDUnsupported(t *testing.T) {
	dir := create(t, false)
	if code, out := invoke(t, dir, "module", "add", "catalog"); code != 0 {
		t.Fatal(out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "modules", "catalog", "module.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Configure(b module.RootBuilder) error") {
		t.Fatal("default skeleton changed")
	}
	if code, out := invoke(t, dir, "module", "add", "orders", "--depends", "catalog"); code != 0 {
		t.Fatal(out)
	}
	ordersRaw, err := os.ReadFile(filepath.Join(dir, "modules", "orders", "module.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ordersRaw), `m_catalog "example.com/bookstore/modules/catalog"`) || !strings.Contains(string(ordersRaw), `module.Of[m_catalog.Module]()`) {
		t.Fatalf("default skeleton user-module import is not aliased:\n%s", ordersRaw)
	}

	if code, out := invoke(t, dir, "module", "add", "customers", "--preset", "crud"); code == 0 || !strings.Contains(out, "unsupported preset") || !strings.Contains(out, "crud") {
		t.Fatalf("crud must be explicitly unsupported, got code=%d out=%s", code, out)
	}
	if _, err := os.Lstat(filepath.Join(dir, "modules", "customers")); !os.IsNotExist(err) {
		t.Fatal("unsupported preset left a module directory")
	}

	if err := os.WriteFile(filepath.Join(dir, ".pangolin-module.lock"), []byte("held"), 0644); err != nil {
		t.Fatal(err)
	}
	if code, out := invoke(t, dir, "module", "add", "locked", "--preset", "service"); code == 0 || !strings.Contains(out, "generation already in progress") {
		t.Fatalf("project generation lock not enforced: code=%d out=%s", code, out)
	}
	if _, err := os.Lstat(filepath.Join(dir, "modules", "locked")); !os.IsNotExist(err) {
		t.Fatal("lock accepted half a module")
	}
}

func TestP3RenameFallbackPreservesBothErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	primary := errors.New("primary rename failed")
	rollback := errors.New("rollback rename failed")
	originalRename := stagedRename
	calls := 0
	stagedRename = func(oldpath, newpath string) error {
		calls++
		if calls == 1 {
			return originalRename(oldpath, newpath)
		}
		if calls == 2 {
			return primary
		}
		return rollback
	}
	defer func() { stagedRename = originalRename }()

	_, _, err := replaceStagedRegularFile(path, []byte("replacement"))
	if err == nil || !errors.Is(err, primary) || !errors.Is(err, rollback) {
		t.Fatalf("fallback must preserve both errors: %v", err)
	}
}

func TestP3StaleRegistryRejectedBeforeCommit(t *testing.T) {
	dir := create(t, false)
	c := CLI{Dir: dir}
	p, err := c.project()
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(dir, "modules", "registry.go")
	projectPath := filepath.Join(dir, "pangolin.json")
	previousRegistry, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	previousProject, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	if err := os.Mkdir(filepath.Join(stage, "module"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "module", "module.go"), []byte("package stale\n"), 0644); err != nil {
		t.Fatal(err)
	}
	registryRaw, err := registryBytes(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "registry.go"), registryRaw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "pangolin.json"), previousProject, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, append(append([]byte{}, previousRegistry...), []byte("// stale\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	err = c.commitStagedModule(filepath.Join(dir, "modules", "stale"), stage, registryPath, projectPath, previousRegistry, previousProject)
	if err == nil || !strings.Contains(err.Error(), "registry changed during generation") {
		t.Fatalf("stale registry accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "modules", "stale")); !os.IsNotExist(err) {
		t.Fatal("stale commit left a module")
	}
}

func TestP3InvalidDependencyCandidates(t *testing.T) {
	dir := create(t, true)
	if code, out := invoke(t, dir, "module", "add", "catalog", "--depends", "missing"); code == 0 {
		t.Fatal("missing dependency accepted")
	} else {
		for _, want := range []string{"dependency path catalog -> missing: missing module", "valid candidates:", "persistence", "authorization", "organization"} {
			if !strings.Contains(out, want) {
				t.Fatalf("missing dependency diagnostics omit %q: %s", want, out)
			}
		}
		for _, unwanted := range []string{"catalog", "bookstore"} {
			if strings.Contains(out, "valid candidates: "+unwanted) {
				t.Fatalf("valid candidates include excluded dependency %q: %s", unwanted, out)
			}
		}
	}

	if code, out := invoke(t, dir, "module", "add", "catalog", "--depends", "persistence,persistence"); code == 0 || !strings.Contains(out, "duplicate dependency") {
		t.Fatalf("duplicate dependency classification changed: code=%d out=%s", code, out)
	}

	if code, out := invoke(t, dir, "module", "add", "catalog", "--depends", "catalog"); code == 0 || !strings.Contains(out, "module cycle:") || !strings.Contains(out, "valid candidates:") {
		t.Fatalf("self dependency cycle diagnostics changed: code=%d out=%s", code, out)
	}

	if code, out := invoke(t, dir, "module", "add", "catalog", "--depends", "bookstore"); code == 0 || !strings.Contains(out, "bookstore") {
		t.Fatalf("bookstore dependency accepted or message lost: code=%d out=%s", code, out)
	}
}

func TestP3DoctorStaticAndLive(t *testing.T) {
	dir := create(t, false)
	before := snapshotTree(t, dir)
	var out bytes.Buffer
	commandCalls := 0
	c := CLI{Dir: dir, Out: &out, Err: &out, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		commandCalls++
		if name != "go" || len(args) != 3 || args[0] != "build" || args[1] != "-mod=readonly" || args[2] != "./..." {
			return nil, errors.New("unexpected static command")
		}
		return nil, nil
	}}
	if err := c.doctor(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("static doctor wrote project files")
	}

	changedOut := bytes.Buffer{}
	changed := CLI{Dir: dir, Out: &changedOut, Err: &changedOut, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "go" || len(args) != 3 || args[0] != "build" || args[1] != "-mod=readonly" || args[2] != "./..." {
			return nil, errors.New("unexpected changed-module build command")
		}
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte("unexpected mutation\n"), 0644); err != nil {
			return nil, err
		}
		return nil, nil
	}}
	if err := changed.doctor(context.Background(), nil); err == nil {
		t.Fatal("static doctor accepted an unexpected go.sum mutation")
	}
	if !strings.Contains(changedOut.String(), "go.mod/go.sum changed during go build -mod=readonly ./...") {
		t.Fatalf("static doctor did not diagnose go.sum mutation: %s", changedOut.String())
	}

	for _, want := range []string{"[PASS] project files", "[PASS] runtime manifest", "[PASS] go build ./...", "[SKIP] database, requires --live", "[SKIP] migrations, requires --live"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("static doctor missing %q: %s", want, out.String())
		}
	}
	if commandCalls != 1 {
		t.Fatalf("static doctor made external/tool calls: %d", commandCalls)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/livez" && r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	t.Setenv("PANGOLIN_API_URL", server.URL)
	t.Setenv("PANGOLIN_DSN", "postgres://example.invalid/pangolin")
	t.Setenv("PANGOLIN_MIGRATION_DSN", "postgres://example.invalid/pangolin")

	liveOut := bytes.Buffer{}
	live := CLI{
		Dir: dir,
		Out: &liveOut,
		Err: &liveOut,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" {
				if len(args) == 3 && args[0] == "build" && args[1] == "-mod=readonly" && args[2] == "./..." {
					return nil, nil
				}
				return nil, errors.New("unexpected go command")
			}
			if name != "docker" || len(args) < 3 || strings.Join(args[:3], " ") != "compose ps --format" {
				return nil, errors.New("unexpected compose command")
			}
			return []byte(`[{"Service":"api","State":"running"}]`), nil
		},
		PingDatabase: func(context.Context, string) error { return nil },
		ReadMigrationLedger: func(context.Context, string) ([]MigrationLedgerEntry, error) {
			return []MigrationLedgerEntry{{Version: 5000002, Applied: true}}, nil
		},
	}
	if err := live.doctor(context.Background(), []string{"--live"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[PASS] compose state", "[PASS] database connection", "[PASS] migration ledger", "[PASS] API /livez", "[PASS] API /readyz"} {
		if !strings.Contains(liveOut.String(), want) {
			t.Fatalf("live doctor missing %q: %s", want, liveOut.String())
		}
	}

	failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer failureServer.Close()
	t.Setenv("PANGOLIN_API_URL", failureServer.URL)
	t.Setenv("PANGOLIN_DSN", "postgres://user:must-not-print@127.0.0.1/pangolin")
	t.Setenv("PANGOLIN_MIGRATION_DSN", "postgres://user:must-not-print@127.0.0.1/pangolin")
	failureOut := bytes.Buffer{}
	failure := CLI{
		Dir: dir,
		Out: &failureOut,
		Err: &failureOut,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" {
				return nil, nil
			}
			return nil, errors.New("compose unavailable")
		},
		PingDatabase: func(context.Context, string) error { return errors.New("database unavailable") },
		ReadMigrationLedger: func(context.Context, string) ([]MigrationLedgerEntry, error) {
			return nil, errors.New("ledger unavailable")
		},
	}
	if err := failure.doctor(context.Background(), []string{"--live"}); err == nil {
		t.Fatal("live doctor accepted failed dependencies")
	}
	for _, want := range []string{"[FAIL] compose state", "[FAIL] database connection", "[FAIL] migration ledger", "[FAIL] API /livez", "[FAIL] API /readyz"} {
		if !strings.Contains(failureOut.String(), want) {
			t.Fatalf("live doctor failure missing %q: %s", want, failureOut.String())
		}
	}
	if strings.Contains(failureOut.String(), "must-not-print") || strings.Contains(failureOut.String(), "postgres://") {
		t.Fatalf("live doctor leaked DSN: %s", failureOut.String())
	}

	if composeHasRunningAPI([]byte(`[{"Service":"api","State":"exited"}]`)) {
		t.Fatal("exited Compose API accepted")
	}
	if !composeHasRunningAPI([]byte("{\"Service\":\"db\",\"State\":\"running\"}\n{\"Service\":\"api\",\"State\":\"running\"}")) {
		t.Fatal("JSON-lines running Compose API rejected")
	}

	t.Setenv("PANGOLIN_API_URL", server.URL)
	t.Setenv("PANGOLIN_DSN", "postgres://example.invalid/pangolin")
	t.Setenv("PANGOLIN_MIGRATION_DSN", "postgres://example.invalid/pangolin")
	ledgerOut := bytes.Buffer{}
	ledgerDoctor := CLI{
		Dir: dir,
		Out: &ledgerOut,
		Err: &ledgerOut,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" {
				return nil, nil
			}
			return []byte(`[{"Service":"api","State":"running"}]`), nil
		},
		PingDatabase: func(context.Context, string) error { return nil },
		ReadMigrationLedger: func(context.Context, string) ([]MigrationLedgerEntry, error) {
			return []MigrationLedgerEntry{{Version: 5000001, Applied: false}}, nil
		},
	}
	if err := ledgerDoctor.doctor(context.Background(), []string{"--live"}); err == nil {
		t.Fatal("live doctor accepted a ledger with no applied migrations")
	}
	if !strings.Contains(ledgerOut.String(), "[FAIL] migration ledger") {
		t.Fatalf("false ledger not diagnosed: %s", ledgerOut.String())
	}
}
