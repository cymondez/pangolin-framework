package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckCLI(t *testing.T) {
	root := filepath.Join("..", "..")
	var output bytes.Buffer
	if err := run([]string{"-root", root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "runtime evidence is checked by the M6 gate") {
		t.Fatal(output.String())
	}
	for _, args := range [][]string{{"-root", root, "-release"}, {"-bad-flag"}, {"unexpected"}, {"-root", t.TempDir()}, {"-root", root, "-coverage", "missing.out"}} {
		if err := run(args, &output); err == nil {
			t.Fatalf("invalid command passed: %v", args)
		}
	}
	previousArgs := os.Args
	os.Args = []string{"check", "-root", root}
	t.Cleanup(func() { os.Args = previousArgs })
	main()
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("write failure") }

func TestCoverageCLIRejectsIncompleteEvidence(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkCoverage(root, "missing", io.Discard); err == nil {
		t.Fatal("invalid module accepted")
	}
	write("go.mod", "module github.com/pangolin-framework/pangolin\n\ngo 1.27.0\n")
	write("kernel/check/f.go", "package check\nfunc F() int { return 1 }\n")
	if err := checkCoverage(root, "missing", io.Discard); err == nil {
		t.Fatal("missing exclusions accepted")
	}
	write("tests/coverage/exclusions.json", "{")
	if err := checkCoverage(root, "missing", io.Discard); err == nil {
		t.Fatal("corrupt exclusions accepted")
	}
	write("tests/coverage/exclusions.json", `{"github.com/pangolin-framework/pangolin/kernel/check/f.go":"hard to test"}`)
	if err := checkCoverage(root, "missing", io.Discard); err == nil {
		t.Fatal("invalid inventory accepted")
	}
	write("tests/coverage/exclusions.json", "{}")
	if err := checkCoverage(root, "missing", io.Discard); err == nil {
		t.Fatal("missing profile accepted")
	}
	profile := filepath.Join(root, "profile.out")
	write("profile.out", "mode: set\n")
	if err := checkCoverage(root, profile, io.Discard); err == nil {
		t.Fatal("invalid profile accepted")
	}
	write("profile.out", "mode: atomic\ngithub.com/pangolin-framework/pangolin/kernel/check/f.go:2.1,2.26 1 0\n")
	if err := checkCoverage(root, profile, io.Discard); err == nil {
		t.Fatal("zero coverage accepted")
	}
	write("profile.out", "mode: atomic\ngithub.com/pangolin-framework/pangolin/kernel/check/f.go:2.1,2.26 1 1\n")
	if err := checkCoverage(root, profile, failedWriter{}); err == nil {
		t.Fatal("write failure ignored")
	}
	if err := checkCoverage(root, profile, io.Discard); err != nil {
		t.Fatal(err)
	}
	write("other/f.go", "package other\nfunc F() int { return 1 }\n")
	write("profile.out", "mode: atomic\ngithub.com/pangolin-framework/pangolin/kernel/check/f.go:2.1,2.26 1 0\ngithub.com/pangolin-framework/pangolin/other/f.go:2.1,2.26 100 1\n")
	if err := checkCoverage(root, profile, io.Discard); err == nil {
		t.Fatal("critical package failed but aggregate masked it")
	}
	// Each new database backend package is critical on its own raw counts even
	// when the aggregate stays above 90%.
	rows := []string{
		"github.com/pangolin-framework/pangolin/kernel/check/f.go:2.1,2.26 1 1",
		"github.com/pangolin-framework/pangolin/other/f.go:2.1,2.26 100 1",
	}
	for _, critical := range []string{"persistence/database", "persistence/mysql", "persistence/sqlite"} {
		write(critical+"/f.go", "package "+filepath.Base(critical)+"\nfunc F() int { return 1 }\n")
		rows = append(rows, "github.com/pangolin-framework/pangolin/"+critical+"/f.go:2.1,2.26 1 0")
		write("profile.out", "mode: atomic\n"+strings.Join(rows, "\n")+"\n")
		if err := checkCoverage(root, profile, io.Discard); err == nil {
			t.Fatal("new critical database package failed but aggregate masked it")
		}
		rows[len(rows)-1] = "github.com/pangolin-framework/pangolin/" + critical + "/f.go:2.1,2.26 1 1"
	}
	write("profile.out", "mode: atomic\n"+strings.Join(rows, "\n")+"\n")
	if err := checkCoverage(root, profile, io.Discard); err != nil {
		t.Fatal(err)
	}
}

// A module fixture outside any git scope keeps the earlier `go list ./...`
// inventory semantics; no other git failure may fall back silently. Under a
// GOTMPDIR inside the repository go test redirects t.TempDir into the
// git-ignored .cache tree, so the fixture is git-ignored on disk even though
// git resolves an ancestor work tree — the scope test, not geography, binds
// the backend, and both backends must produce the same inventory here.
func TestTrackedPackageInventoryGitlessModuleKeepsLegacySemantics(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/pangolin-framework/pangolin\n\ngo 1.27.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "kernel", "check"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kernel", "check", "f.go"), []byte("package check\nfunc F() int { return 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if scoped, err := gitScope(root); err != nil {
		t.Fatal(err)
	} else if scoped {
		t.Fatalf("a git-ignored or ancestor-resolved temp module must not claim its own git scope")
	}
	data, err := trackedPackageInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ImportPath": "github.com/pangolin-framework/pangolin/kernel/check"`) {
		t.Fatalf("legacy inventory missed the module package: %s", data)
	}
	if strings.Contains(string(data), `"../`) || strings.Contains(string(data), `\..\`) {
		t.Fatalf("inventory escaped the module root: %s", data)
	}
}

// Inside a real git repository the tracked inventory excludes ignored
// directories and stays identical to the git ls-files package list.
func TestTrackedPackageInventoryRepositoryExcludesIgnoredDirectories(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", ".git")); err != nil {
		t.Skip("not a git checkout")
	}
	seed := func(root, name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// The seeded repository must own its own git scope; under a repo-internal
	// GOTMPDIR the default temp location is ignored, so build the fixture on
	// explicit disk outside any ignore rules of this repository.
	work := filepath.Join(os.TempDir(), "m7-toolcheck-repo-"+time.Now().Format("150405.000000000"))
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	seed(work, ".gitignore", "/artifacts/\n")
	seed(work, "go.mod", "module github.com/pangolin-framework/pangolin\n\ngo 1.27.0\n")
	seed(work, "kernel/check/f.go", "package check\nfunc F() int { return 1 }\n")
	seed(work, "artifacts/design-review/x_test.go", "package designreview\n")
	commands := [][]string{
		{"git", "init", "-q"},
		{"git", "-c", "core.quotepath=false", "add", "."},
	}
	for _, argv := range commands {
		command := exec.Command(argv[0], argv[1:]...)
		command.Dir = work
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git setup %v: %v: %s", argv, err, out)
		}
	}
	if scoped, err := gitScope(work); err != nil || !scoped {
		t.Fatalf("seeded repository must own its own git scope: scoped=%v err=%v", scoped, err)
	}
	data, err := trackedPackageInventory(work)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "artifacts/design-review") {
		t.Fatalf("ignored directory entered the tracked inventory: %s", data)
	}
	if !strings.Contains(string(data), `"ImportPath": "github.com/pangolin-framework/pangolin/kernel/check"`) {
		t.Fatalf("tracked package missing from the inventory: %s", data)
	}
}
