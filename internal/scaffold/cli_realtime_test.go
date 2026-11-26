package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedRealtimeProfiles(t *testing.T) {
	for _, profile := range []string{"tenant-only", "organization-enabled"} {
		t.Run(profile, func(t *testing.T) {
			dir := t.TempDir()
			args := []string{"new", "bookstore", "--template", "modular-monolith", "--realtime"}
			if profile == "organization-enabled" {
				args = append(args, "--with", "organization")
			}
			if code, out := invoke(t, dir, args...); code != 0 {
				t.Fatal(out)
			}
			project := filepath.Join(dir, "bookstore")
			if _, err := os.Stat(filepath.Join(project, "REALTIME.md")); err != nil {
				t.Fatal(err)
			}
			if code, out := invoke(t, project, "module", "add", "notices", "--depends", "realtime"); code != 0 {
				t.Fatal(out)
			}
			path := filepath.Join(project, "modules", "notices", "module.go")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wired := strings.Replace(string(raw), "Configure(b module.RootBuilder) error { return nil }",
				"Configure(b module.RootBuilder) error { return b.Services().Add(newNotices, module.Singleton()) }", 1)
			wired += "\ntype notices struct { publisher realtime.Publisher }\nfunc newNotices(publisher realtime.Publisher) *notices { return &notices{publisher} }\n"
			if err := os.WriteFile(path, []byte(wired), 0o644); err != nil {
				t.Fatal(err)
			}
			build := exec.CommandContext(t.Context(), "go", "-C", project, "build", "./...")
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("generated project build: %v\n%s", err, out)
			}
			if profile == "tenant-only" {
				deps := exec.CommandContext(t.Context(), "go", "-C", project, "list", "-deps", "./cmd/api")
				if out, err := deps.CombinedOutput(); err != nil || strings.Contains(string(out), "/modules/organization") {
					t.Fatalf("tenant-only dependencies: %v\n%s", err, out)
				}
			}
		})
	}
}
