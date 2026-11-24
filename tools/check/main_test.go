package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckCLI(t *testing.T) {
	root := filepath.Join("..", "..")
	var output bytes.Buffer
	if err := run([]string{"-root", root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "runtime acceptance pending") {
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
	write("go.mod", "module github.com/cymondez/pangolin-framework\n\ngo 1.27.0\n")
	write("kernel/check/f.go", "package check\nfunc F() int { return 1 }\n")
	if err := checkCoverage(root, "missing", io.Discard); err == nil {
		t.Fatal("missing exclusions accepted")
	}
	write("tests/coverage/exclusions.json", "{")
	if err := checkCoverage(root, "missing", io.Discard); err == nil {
		t.Fatal("corrupt exclusions accepted")
	}
	write("tests/coverage/exclusions.json", `{"github.com/cymondez/pangolin-framework/kernel/check/f.go":"hard to test"}`)
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
	write("profile.out", "mode: atomic\ngithub.com/cymondez/pangolin-framework/kernel/check/f.go:2.1,2.26 1 0\n")
	if err := checkCoverage(root, profile, io.Discard); err == nil {
		t.Fatal("zero coverage accepted")
	}
	write("profile.out", "mode: atomic\ngithub.com/cymondez/pangolin-framework/kernel/check/f.go:2.1,2.26 1 1\n")
	if err := checkCoverage(root, profile, failedWriter{}); err == nil {
		t.Fatal("write failure ignored")
	}
	if err := checkCoverage(root, profile, io.Discard); err != nil {
		t.Fatal(err)
	}
	write("other/f.go", "package other\nfunc F() int { return 1 }\n")
	write("profile.out", "mode: atomic\ngithub.com/cymondez/pangolin-framework/kernel/check/f.go:2.1,2.26 1 0\ngithub.com/cymondez/pangolin-framework/other/f.go:2.1,2.26 100 1\n")
	if err := checkCoverage(root, profile, io.Discard); err == nil {
		t.Fatal("critical package failed but aggregate masked it")
	}
	// Each new database backend package is critical on its own raw counts even
	// when the aggregate stays above 90%.
	rows := []string{
		"github.com/cymondez/pangolin-framework/kernel/check/f.go:2.1,2.26 1 1",
		"github.com/cymondez/pangolin-framework/other/f.go:2.1,2.26 100 1",
	}
	for _, critical := range []string{"persistence/database", "persistence/mysql", "persistence/sqlite"} {
		write(critical+"/f.go", "package "+filepath.Base(critical)+"\nfunc F() int { return 1 }\n")
		rows = append(rows, "github.com/cymondez/pangolin-framework/"+critical+"/f.go:2.1,2.26 1 0")
		write("profile.out", "mode: atomic\n"+strings.Join(rows, "\n")+"\n")
		if err := checkCoverage(root, profile, io.Discard); err == nil {
			t.Fatal("new critical database package failed but aggregate masked it")
		}
		rows[len(rows)-1] = "github.com/cymondez/pangolin-framework/" + critical + "/f.go:2.1,2.26 1 1"
	}
	write("profile.out", "mode: atomic\n"+strings.Join(rows, "\n")+"\n")
	if err := checkCoverage(root, profile, io.Discard); err != nil {
		t.Fatal(err)
	}
}
