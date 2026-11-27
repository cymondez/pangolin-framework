package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func invoke(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := (CLI{Dir: dir, Out: &out, Err: &out}).Run(context.Background(), args)
	return code, out.String()
}
func create(t *testing.T, org bool) string {
	t.Helper()
	dir := t.TempDir()
	args := []string{"new", "bookstore"}
	if org {
		args = append(args, "--with", "organization")
	}
	if code, out := invoke(t, dir, args...); code != 0 {
		t.Fatal(out)
	}
	return filepath.Join(dir, "bookstore")
}
func TestA67(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		dir := create(t, false)
		p, err := (CLI{Dir: dir}).project()
		if err != nil || p.Version != Version || p.Profile != "tenant-only" {
			t.Fatal(p, err)
		}
		if _, err = os.Stat(filepath.Join(dir, "framework", "modules", "organization")); !os.IsNotExist(err) {
			t.Fatal("optional implementation emitted")
		}
		root := t.TempDir()
		if code, out := invoke(t, root, "new", "bookstore-org", "--with", "organization"); code != 0 {
			t.Fatal(out)
		}
		p, err = (CLI{Dir: filepath.Join(root, "bookstore-org")}).project()
		if err != nil || p.Name != "bookstore-org" {
			t.Fatal(p, err)
		}
	})
	t.Run("module", func(t *testing.T) {
		dir := create(t, false)
		if code, out := invoke(t, dir, "module", "add", "catalog", "--depends", "persistence,authorization"); code != 0 {
			t.Fatal(out)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "modules", "registry.go"))
		if !strings.Contains(string(raw), "module.Of[m_catalog.Module]()") || !strings.Contains(string(raw), "type UserRoot struct{}") {
			t.Fatal("user module not registered in the root aggregator")
		}
		module, err := os.ReadFile(filepath.Join(dir, "modules", "catalog", "module.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(module), "module.RootBuilder") || !strings.Contains(string(module), "ModuleMetadata() module.Metadata") {
			t.Fatal("generated module is not a root-mode module")
		}
		// Framework dependency names translate into the foundation Type
		// edge; they are never silently omitted.
		if !strings.Contains(string(module), "module.Of[foundation.Module]()") || !strings.Contains(string(module), "example.com/bookstore/foundation") {
			t.Fatalf("framework dependency did not translate to the foundation Type edge: %s", module)
		}
		if _, err := os.Stat(filepath.Join(dir, "modules", "catalog", "README.md")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("migrate-create", func(t *testing.T) {
		dir := create(t, false)
		if code, out := invoke(t, dir, "migrate", "create", "add_books", "--module", "bookstore"); code != 0 {
			t.Fatal(out)
		}
		if code, out := invoke(t, dir, "migrate", "create", "add_authors", "--module", "bookstore"); code != 0 {
			t.Fatal(out)
		}
		entries, _ := os.ReadDir(filepath.Join(dir, "modules", "bookstore", "migrations"))
		if len(entries) != 2 || entries[0].Name() != "5000001_add_books.sql" || entries[1].Name() != "5000002_add_authors.sql" {
			t.Fatal(entries)
		}
	})
	t.Run("openapi", func(t *testing.T) {
		for _, org := range []bool{false, true} {
			dir := create(t, org)
			if code, out := invoke(t, dir, "generate", "openapi"); code != 0 {
				t.Fatal(out)
			}
			raw, _ := os.ReadFile(filepath.Join(dir, "openapi.json"))
			var doc struct {
				OpenAPI string
				Paths   map[string]any
			}
			if json.Unmarshal(raw, &doc) != nil || doc.OpenAPI != "3.1.0" || (doc.Paths["/organizations"] != nil) != org {
				t.Fatal("wrong OpenAPI")
			}
		}
	})
	t.Run("help", func(t *testing.T) {
		dir := t.TempDir()
		for _, args := range [][]string{nil, {"--help"}, {"help"}, {"--version"}, {"upgrade", "--help"}, {"upgrade", "plan", "--help"}, {"upgrade", "runtime", "--help"}, {"upgrade", "verify", "--help"}, {"new", "--help"}, {"new", "demo", "--help"}, {"module", "--help"}, {"module", "add", "--help"}, {"module", "add", "m", "--help"}, {"migrate", "--help"}, {"migrate", "create", "--help"}, {"migrate", "create", "m", "--help"}, {"migrate", "status", "--help"}, {"doctor", "--help"}, {"generate", "--help"}, {"generate", "openapi", "--help"}} {
			if code, out := invoke(t, dir, args...); code != 0 || out == "" {
				t.Fatal(args, code, out)
			}
		}
	})
	t.Run("invalid-arguments", func(t *testing.T) {
		dir := create(t, false)
		for _, args := range [][]string{{"unknown"}, {"upgrade"}, {"upgrade", "nope"}, {"upgrade", "plan", "extra"}, {"upgrade", "--dry-run", "extra"}, {"upgrade", "runtime", "extra"}, {"upgrade", "verify", "extra"}, {"new"}, {"new", "../escape"}, {"new", "x", "--template", "bad"}, {"new", "x", "--with", "enterprise"}, {"new", "x", "--unknown"}, {"new", "x", "extra"}, {"module", "add", "m", "extra"}, {"module", "add", "m", "--depends", "missing"}, {"module", "add", "m", "--depends", "m"}, {"module", "add", "m", "--depends", "bookstore"}, {"module", "add", "m", "--depends", "persistence,persistence"}, {"migrate", "create", "bad", "--module", "missing"}, {"migrate", "create", "bad", "--module", "bookstore", "extra"}, {"migrate", "status", "extra"}, {"doctor", "extra"}, {"generate", "openapi", "extra"}} {
			if code, out := invoke(t, dir, args...); code == 0 {
				t.Fatal(args, out)
			}
		}
		if code, _ := invoke(t, t.TempDir(), "doctor"); code == 0 {
			t.Fatal("no project")
		}
	})
	t.Run("existing-files", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "bookstore")
		if err := os.Mkdir(target, 0755); err != nil {
			t.Fatal(err)
		}
		sentinel := filepath.Join(target, "keep.txt")
		_ = os.WriteFile(sentinel, []byte("original"), 0644)
		if code, _ := invoke(t, root, "new", "bookstore"); code == 0 {
			t.Fatal("existing target accepted")
		}
		raw, _ := os.ReadFile(sentinel)
		if string(raw) != "original" {
			t.Fatal("existing data changed")
		}
		dir := create(t, false)
		_ = os.WriteFile(filepath.Join(dir, "openapi.json"), []byte("user-owned"), 0644)
		if code, _ := invoke(t, dir, "generate", "openapi"); code == 0 {
			t.Fatal("overwrote OpenAPI")
		}
		raw, _ = os.ReadFile(filepath.Join(dir, "openapi.json"))
		if string(raw) != "user-owned" {
			t.Fatal("OpenAPI corrupted")
		}
		_ = os.MkdirAll(filepath.Join(dir, "modules", "catalog"), 0755)
		if code, _ := invoke(t, dir, "module", "add", "catalog"); code == 0 {
			t.Fatal("existing module overwritten")
		}
	})
	t.Run("reentry", func(t *testing.T) {
		dir := create(t, false)
		for _, args := range [][]string{{"module", "add", "catalog"}, {"migrate", "create", "add_books", "--module", "catalog"}} {
			if code, out := invoke(t, dir, args...); code != 0 {
				t.Fatal(out)
			}
			if code, _ := invoke(t, dir, args...); code == 0 {
				t.Fatal("duplicate accepted")
			}
		}
		if code, out := invoke(t, dir, "generate", "openapi"); code != 0 {
			t.Fatal(out)
		}
		if code, out := invoke(t, dir, "generate", "openapi"); code != 0 {
			t.Fatal(out)
		}
	})
}
func TestA94(t *testing.T) {
	t.Run("default-tenant-template", func(t *testing.T) {
		dir := create(t, false)
		raw, _ := os.ReadFile(filepath.Join(dir, "foundation", "profile.go"))
		if strings.Contains(string(raw), "modules/organization") {
			t.Fatal("tenant imports organization")
		}
	})
	t.Run("explicit-organization-template", func(t *testing.T) {
		dir := create(t, true)
		raw, _ := os.ReadFile(filepath.Join(dir, "foundation", "profile.go"))
		if !strings.Contains(string(raw), "organization.NewStore") || !strings.Contains(string(raw), "orghttp.NewHandler") {
			t.Fatal("incomplete profile")
		}
	})
	t.Run("complete-module-migrations", func(t *testing.T) {
		dir := create(t, true)
		for _, file := range []string{"framework/modules/organization/migrations/3100002_organization.sql", "framework/modules/organization/migrations/3100004_owner_qualification.sql", "contracts/runtime_test.go", "internal/app/migrations/5000000_books.sql"} {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(file))); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("no-half-enterprise-template", func(t *testing.T) {
		if code, _ := invoke(t, t.TempDir(), "new", "bad", "--with", "enterprise"); code == 0 {
			t.Fatal("half profile")
		}
	})
}
func TestCLIInvalidProjectAndCancellation(t *testing.T) {
	dir := create(t, false)
	_ = os.WriteFile(filepath.Join(dir, "pangolin.json"), []byte(`{}`), 0644)
	if code, _ := invoke(t, dir, "module", "add", "m"); code == 0 {
		t.Fatal("corrupt project")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if (CLI{Dir: dir}).Run(ctx, []string{"--help"}) == 0 {
		t.Fatal("cancel ignored")
	}
	if (CLI{Dir: dir}).Run(nil, nil) == 0 {
		t.Fatal("nil context")
	}
	dir = create(t, false)
	t.Setenv("PANGOLIN_MIGRATION_DSN", "")
	if code, _ := invoke(t, dir, "migrate", "status"); code == 0 {
		t.Fatal("missing DSN")
	}
	dir = create(t, false)
	if err := os.WriteFile(filepath.Join(dir, "modules", "file"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if (CLI{Dir: dir}).safeDirectory(filepath.Join("modules", "file", "child")) == nil || (CLI{Dir: dir}).safeDirectory("../escape") == nil {
		t.Fatal("unsafe path")
	}
}

func TestA67Doctor(t *testing.T) {
	t.Run("doctor", func(t *testing.T) {
		dir := create(t, false)
		var out bytes.Buffer
		calls := 0
		c := CLI{Dir: dir, Out: &out, Err: &out, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls++
			if name != "go" || len(args) != 3 || args[0] != "build" || args[1] != "-mod=readonly" || args[2] != "./..." {
				return nil, errors.New("unexpected static command")
			}
			return nil, nil
		}}
		if err := c.doctor(context.Background(), nil); err != nil || calls != 1 {
			t.Fatal(err, calls)
		}
		if !strings.Contains(out.String(), "[PASS] project files") || !strings.Contains(out.String(), "[PASS] runtime manifest") || !strings.Contains(out.String(), "[PASS] go build ./...") {
			t.Fatal(out.String())
		}
		c.Command = func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("process unavailable")
		}
		if c.doctor(context.Background(), nil) == nil {
			t.Fatal("missing build tool ignored")
		}
	})
}
