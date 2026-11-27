package scaffold

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func upgradeDigest(raw []byte) string {
	if raw == nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func TestClassifyThreeWay(t *testing.T) {
	oldHash := upgradeDigest([]byte("old"))
	newHash := upgradeDigest([]byte("new"))
	userHash := upgradeDigest([]byte("user"))
	tests := []struct {
		name                                  string
		baseExists, localExists, targetExists bool
		base, local, target                   string
		want                                  string
	}{
		{"unchanged", true, true, true, oldHash, oldHash, oldHash, "unchanged"},
		{"update", true, true, true, oldHash, oldHash, newHash, "update"},
		{"keep", true, true, true, oldHash, newHash, newHash, "keep"},
		{"add", false, false, true, "", "", newHash, "add"},
		{"remove", true, true, false, oldHash, oldHash, "", "remove"},
		{"conflict", true, true, true, oldHash, userHash, newHash, "conflict"},
		{"deleted-conflict", true, false, true, oldHash, "", newHash, "conflict"},
		{"missing", true, false, true, oldHash, "", oldHash, "missing"},
		{"removed-already", true, false, false, oldHash, "", "", "unchanged"},
		{"local-only-kept", false, true, false, "", userHash, "", "keep"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyThreeWay(tt.baseExists, tt.base, tt.localExists, tt.local, tt.targetExists, tt.target)
			if got != tt.want {
				t.Fatalf("classifyThreeWay() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpgradeClassificationHelpers(t *testing.T) {
	cause := errors.New("command failed")
	commandFailure := commandError{message: "upgrade blocked", cause: cause}
	if commandFailure.Error() != "upgrade blocked" || !errors.Is(commandFailure, cause) {
		t.Fatalf("command error lost its message or cause: %v", commandFailure)
	}

	for input, want := range map[string]string{
		"":              "Service",
		"catalog":       "Catalog",
		"order_service": "OrderService",
	} {
		if got := pascalName(input); got != want {
			t.Fatalf("pascalName(%q) = %q, want %q", input, got, want)
		}
	}

	for _, test := range []struct {
		path        string
		publicGo    bool
		migration   bool
		settingsCfg bool
	}{
		{"framework/application/service.go", true, false, false},
		{"framework/application/service_test.go", false, false, false},
		{"framework/_hidden.go", false, false, false},
		{"modules/bookstore/main.go", false, false, false},
		{"framework/persistence/migrations/5000000_books.sql", false, true, false},
		{"framework/platform/settings/configuration.go", true, false, true},
		{"framework/platform/features/features.go", true, false, true},
		{"framework/go.mod", false, false, true},
	} {
		if got := isPublicGoChange(test.path); got != test.publicGo {
			t.Fatalf("isPublicGoChange(%q) = %v, want %v", test.path, got, test.publicGo)
		}
		if got := isMigrationChange(test.path); got != test.migration {
			t.Fatalf("isMigrationChange(%q) = %v, want %v", test.path, got, test.migration)
		}
		if got := isSettingsConfigChange(test.path); got != test.settingsCfg {
			t.Fatalf("isSettingsConfigChange(%q) = %v, want %v", test.path, got, test.settingsCfg)
		}
	}
	if digestOrDash("") != "-" || digestOrDash("abc") != "abc" {
		t.Fatal("digest rendering changed")
	}
}

func TestSafeUpgradeRelativePath(t *testing.T) {
	for _, path := range []string{"framework/go.mod", "framework/a/b.go", "framework/platform/settings/configuration.go"} {
		if err := validateUpgradeRelativePath(path); err != nil {
			t.Fatalf("valid path %q rejected: %v", path, err)
		}
	}
	for _, path := range []string{"", "go.mod", "../framework/go.mod", "framework/../go.mod", "framework\\go.mod", "/tmp/go.mod", "framework/"} {
		if err := validateUpgradeRelativePath(path); err == nil {
			t.Fatalf("unsafe path %q accepted", path)
		}
	}
}

func TestUpgradePlanFreshNoWrite(t *testing.T) {
	dir := create(t, false)
	before := snapshotTree(t, dir)
	code, out := invoke(t, dir, "upgrade", "plan")
	if code != 0 {
		t.Fatalf("upgrade plan failed: %s", out)
	}
	for _, want := range []string{"current version:", "target version:", "api inventory:", "migrations:", "settings/config:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("upgrade plan missing %q: %s", want, out)
		}
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("upgrade plan wrote project files")
	}

	code, out = invoke(t, dir, "upgrade", "--dry-run")
	if code != 0 {
		t.Fatalf("upgrade dry-run failed: %s", out)
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("upgrade dry-run wrote project files")
	}
}

func TestUpgradeProfileMismatchNoWrite(t *testing.T) {
	cases := []struct {
		name           string
		baselineOrg    bool
		projectProfile string
	}{
		{name: "tenant baseline changed to organization", baselineOrg: false, projectProfile: "organization-enabled"},
		{name: "organization baseline changed to tenant", baselineOrg: true, projectProfile: "tenant-only"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := create(t, tt.baselineOrg)
			projectPath := filepath.Join(dir, "pangolin.json")
			raw, err := os.ReadFile(projectPath)
			if err != nil {
				t.Fatal(err)
			}
			var project Project
			if err := json.Unmarshal(raw, &project); err != nil {
				t.Fatal(err)
			}
			project.Profile = tt.projectProfile
			if err := os.WriteFile(projectPath, jsonBytes(project), 0644); err != nil {
				t.Fatal(err)
			}

			before := snapshotTree(t, dir)
			for _, args := range [][]string{{"upgrade", "plan"}, {"upgrade", "--dry-run"}, {"upgrade", "runtime"}, {"upgrade", "verify"}} {
				code, out := invoke(t, dir, args...)
				if code == 0 {
					t.Fatalf("%v accepted profile mismatch: %s", args, out)
				}
				if !strings.Contains(out, "profile switch") || !strings.Contains(out, "migration guide") {
					t.Fatalf("%v mismatch diagnosis missing migration guard: %s", args, out)
				}
				if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
					t.Fatalf("%v profile mismatch wrote project files", args)
				}
			}
		})
	}
}

func TestUpgradeRealtimeMismatchNoWrite(t *testing.T) {
	cases := []struct {
		name            string
		baselineOn      bool
		projectRealtime bool
	}{
		{name: "baseline realtime off changed on", baselineOn: false, projectRealtime: true},
		{name: "baseline realtime on changed off", baselineOn: true, projectRealtime: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			args := []string{"new", "bookstore", "--template", "modular-monolith"}
			if tt.baselineOn {
				args = append(args, "--realtime")
			}
			if code, out := invoke(t, dir, args...); code != 0 {
				t.Fatal(out)
			}
			dir = filepath.Join(dir, "bookstore")
			projectPath := filepath.Join(dir, "pangolin.json")
			raw, err := os.ReadFile(projectPath)
			if err != nil {
				t.Fatal(err)
			}
			var project Project
			if err := json.Unmarshal(raw, &project); err != nil {
				t.Fatal(err)
			}
			project.Realtime = tt.projectRealtime
			if err := os.WriteFile(projectPath, jsonBytes(project), 0644); err != nil {
				t.Fatal(err)
			}

			before := snapshotTree(t, dir)
			for _, args := range [][]string{{"upgrade", "plan"}, {"upgrade", "--dry-run"}, {"upgrade", "runtime"}, {"upgrade", "verify"}} {
				code, out := invoke(t, dir, args...)
				if code == 0 {
					t.Fatalf("%v accepted realtime mismatch: %s", args, out)
				}
				if !strings.Contains(out, "realtime capability switch") || !strings.Contains(out, "migration guide") {
					t.Fatalf("%v mismatch diagnosis missing migration guard: %s", args, out)
				}
				if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
					t.Fatalf("%v realtime mismatch wrote project files", args)
				}
			}
		})
	}
}

func TestUpgradePlanUpdateAndConflict(t *testing.T) {
	dir := create(t, false)
	manifestPath := filepath.Join(dir, "runtime-manifest.json")
	manifest := readUpgradeManifest(t, manifestPath)
	path := "framework/VERSION"
	targetRaw, err := fsReadAsset("assets/runtime/VERSION.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	oldRaw := []byte("0.0.9\n")
	localPath := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.WriteFile(localPath, oldRaw, 0644); err != nil {
		t.Fatal(err)
	}
	manifest[path] = upgradeDigest(oldRaw)
	writeUpgradeManifest(t, manifestPath, manifest)

	code, out := invoke(t, dir, "upgrade", "plan")
	if code != 0 {
		t.Fatalf("update plan failed: %s", out)
	}
	if !strings.Contains(out, path+" update") {
		t.Fatalf("missing update classification: %s", out)
	}

	userRaw := []byte("user-edited\n")
	if err := os.WriteFile(localPath, userRaw, 0644); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, dir)
	code, out = invoke(t, dir, "upgrade", "--dry-run")
	if code == 0 {
		t.Fatalf("dry-run accepted conflict: %s", out)
	}
	if !strings.Contains(out, path+" conflict") || !strings.Contains(out, "base=") || !strings.Contains(out, "local=") || !strings.Contains(out, "target=") {
		t.Fatalf("conflict output missing digests: %s", out)
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("dry-run conflict wrote project files")
	}
	_ = targetRaw
}

func TestUpgradeRuntimeCreatesBackup(t *testing.T) {
	dir := create(t, false)
	code, out := invoke(t, dir, "upgrade", "runtime")
	if code != 0 {
		t.Fatalf("upgrade runtime failed: %s", out)
	}
	if !strings.Contains(out, "backup:") || !strings.Contains(out, "rollback:") {
		t.Fatalf("runtime result missing backup/rollback: %s", out)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".pangolin-backup", "*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("timestamped backup missing: %v %v", matches, err)
	}
	for _, required := range []string{"framework", "runtime-manifest.json"} {
		if _, err := os.Stat(filepath.Join(matches[len(matches)-1], required)); err != nil {
			t.Fatalf("backup missing %s: %v", required, err)
		}
	}
}

func TestUpgradeRuntimeConflictNoWrite(t *testing.T) {
	dir := create(t, false)
	manifestPath := filepath.Join(dir, "runtime-manifest.json")
	manifest := readUpgradeManifest(t, manifestPath)
	path := "framework/VERSION"
	oldRaw := []byte("0.0.9\n")
	userRaw := []byte("user-edited\n")
	manifest[path] = upgradeDigest(oldRaw)
	writeUpgradeManifest(t, manifestPath, manifest)
	localPath := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.WriteFile(localPath, userRaw, 0644); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, dir)
	code, out := invoke(t, dir, "upgrade", "runtime")
	if code == 0 {
		t.Fatalf("runtime accepted conflict: %s", out)
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("runtime conflict wrote project files")
	}
}

func TestRestoreUpgradeBackupRestoresCommittedFiles(t *testing.T) {
	dir := t.TempDir()
	stage := filepath.Join(dir, ".pangolin-upgrade-stage")
	backup := filepath.Join(dir, ".pangolin-backup", "20261127T000000Z")
	for _, path := range []string{
		filepath.Join(dir, "framework"),
		filepath.Join(stage),
		filepath.Join(backup, "framework"),
	} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string][]byte{
		"framework/runtime.go":  []byte("new framework\n"),
		"runtime-manifest.json": []byte(`{"new":"manifest"}` + "\n"),
		"pangolin.json":         []byte(`{"version":"0.2.0"}` + "\n"),
		".pangolin-backup/20261127T000000Z/framework/runtime.go":  []byte("committed framework\n"),
		".pangolin-backup/20261127T000000Z/runtime-manifest.json": []byte(`{"old":"manifest"}` + "\n"),
		".pangolin-backup/20261127T000000Z/pangolin.json":         []byte(`{"version":"0.1.0"}` + "\n"),
	}
	for relative, raw := range files {
		path := filepath.Join(dir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := restoreUpgradeBackup(CLI{Dir: dir}, stage, backup); err != nil {
		t.Fatal(err)
	}
	for relative, backupRelative := range map[string]string{
		"framework/runtime.go":  ".pangolin-backup/20261127T000000Z/framework/runtime.go",
		"runtime-manifest.json": ".pangolin-backup/20261127T000000Z/runtime-manifest.json",
		"pangolin.json":         ".pangolin-backup/20261127T000000Z/pangolin.json",
	} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(files[backupRelative]) {
			t.Fatalf("%s was not restored from backup: %q", relative, got)
		}
	}
}

func TestUpgradeRuntimeUpdateThenVerify(t *testing.T) {
	dir := create(t, false)
	manifestPath := filepath.Join(dir, "runtime-manifest.json")
	manifest := readUpgradeManifest(t, manifestPath)
	path := "framework/VERSION"
	oldRaw := []byte("0.0.9\n")
	manifest[path] = upgradeDigest(oldRaw)
	writeUpgradeManifest(t, manifestPath, manifest)
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(path)), oldRaw, 0644); err != nil {
		t.Fatal(err)
	}

	if code, out := invoke(t, dir, "upgrade", "plan"); code != 0 || !strings.Contains(out, path+" update") {
		t.Fatalf("old project plan failed: code=%d out=%s", code, out)
	}
	if code, out := invoke(t, dir, "upgrade", "runtime"); code != 0 {
		t.Fatalf("old project runtime failed: %s", out)
	}
	before := snapshotTree(t, dir)
	verify := CLI{
		Dir: dir,
		Out: new(bytes.Buffer),
		Err: new(bytes.Buffer),
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "go" {
				return nil, errors.New("unexpected command")
			}
			if len(args) < 3 || args[1] != "-mod=readonly" {
				return nil, errors.New("verify command is not read-only")
			}
			return nil, nil
		},
	}
	if code := verify.Run(context.Background(), []string{"upgrade", "verify"}); code != 0 {
		t.Fatalf("post-runtime verify failed: %s", verify.Err)
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("post-runtime verify wrote project files")
	}
}

func TestUpgradeVerifyNoWriteAndTestFailureAction(t *testing.T) {
	dir := create(t, false)
	before := snapshotTree(t, dir)
	verify := CLI{
		Dir: dir,
		Out: new(bytes.Buffer),
		Err: new(bytes.Buffer),
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "go" {
				return nil, errors.New("unexpected command")
			}
			if len(args) < 3 || args[1] != "-mod=readonly" {
				return nil, errors.New("verify command is not read-only")
			}
			return nil, nil
		},
	}
	if code := verify.Run(context.Background(), []string{"upgrade", "verify"}); code != 0 {
		t.Fatalf("verify failed: %s", verify.Err)
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("verify wrote project files")
	}

	failed := CLI{
		Dir: dir,
		Out: new(bytes.Buffer),
		Err: new(bytes.Buffer),
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && len(args) > 0 && args[0] == "test" {
				return nil, errors.New("test injected failure")
			}
			return nil, nil
		},
	}
	if code := failed.Run(context.Background(), []string{"upgrade", "verify"}); code == 0 {
		t.Fatal("verify accepted injected test failure")
	}
	if !strings.Contains(failed.Err.(*bytes.Buffer).String(), "go test ./...") {
		t.Fatalf("verify test failure lacks actionable command: %s", failed.Err)
	}
}

func TestUpgradeVerifyRejectsStaleProjectVersion(t *testing.T) {
	dir := create(t, false)
	projectPath := filepath.Join(dir, "pangolin.json")
	raw, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	var project Project
	if err := json.Unmarshal(raw, &project); err != nil {
		t.Fatal(err)
	}
	project.Version = "0.0.9"
	if err := os.WriteFile(projectPath, jsonBytes(project), 0644); err != nil {
		t.Fatal(err)
	}
	verify := CLI{Dir: dir, Out: new(bytes.Buffer), Err: new(bytes.Buffer), Command: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }}
	if code := verify.Run(context.Background(), []string{"upgrade", "verify"}); code == 0 {
		t.Fatal("verify accepted stale pangolin.json version")
	}
	if !strings.Contains(verify.Err.(*bytes.Buffer).String(), "stale pangolin.json version") {
		t.Fatalf("verify stale-version diagnosis missing: %s", verify.Err)
	}
}

func TestUpgradeRuntimeLockAndCanceledContext(t *testing.T) {
	dir := create(t, false)
	before := snapshotTree(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".pangolin-module.lock"), []byte("held"), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := invoke(t, dir, "upgrade", "runtime"); code == 0 {
		t.Fatal("runtime accepted while project mutation lock was held")
	}
	if err := os.Remove(filepath.Join(dir, ".pangolin-module.lock")); err != nil {
		t.Fatal(err)
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("runtime lock refusal wrote project files")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := CLI{Dir: dir, Out: new(bytes.Buffer), Err: new(bytes.Buffer), Command: func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("canceled context ran a command")
	}}
	before = snapshotTree(t, dir)
	if code := cancelled.Run(ctx, []string{"upgrade", "verify"}); code == 0 {
		t.Fatal("verify accepted canceled context")
	}
	if got := snapshotTree(t, dir); !equalSnapshot(before, got) {
		t.Fatal("canceled upgrade verify wrote project files")
	}
}

func TestUpgradeRejectsInvalidManifestAndUnsafePath(t *testing.T) {
	dir := create(t, false)
	manifestPath := filepath.Join(dir, "runtime-manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"framework/../escape": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if code, out := invoke(t, dir, "upgrade", "plan"); code == 0 {
		t.Fatalf("invalid manifest accepted: %s", out)
	}

	manifest := map[string]string{"framework/../escape": strings.Repeat("a", 64)}
	writeUpgradeManifest(t, manifestPath, manifest)
	if code, out := invoke(t, dir, "upgrade", "runtime"); code == 0 {
		t.Fatalf("unsafe manifest accepted: %s", out)
	}
}

func fsReadAsset(path string) ([]byte, error) {
	return fs.ReadFile(assets, path)
}

func readUpgradeManifest(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]string{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeUpgradeManifest(t *testing.T, path string, manifest map[string]string) {
	t.Helper()
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(rel)] = upgradeDigest(raw)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func equalSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for path, digest := range a {
		if b[path] != digest {
			return false
		}
	}
	return true
}
