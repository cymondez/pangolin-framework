// Command check validates the M0 contract inventory. It does not certify runtime behavior.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pangolin-framework/pangolin/internal/quality"
)

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(out)
	root := flags.String("root", ".", "repository root")
	release := flags.Bool("release", false, "reject static-only release certification; use the same-commit M6 evidence gate")
	profile := flags.String("coverage", "", "single atomic profile from this source/platform/build")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if err := quality.CheckRepository(*root, *release); err != nil {
		return err
	}
	if *profile != "" {
		if err := checkCoverage(*root, *profile, out); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out, "M0 inventory valid: 95 requirements, 95 planned case groups, 95 unique deliveries; runtime evidence is checked by the M6 gate")
	return err
}

func checkCoverage(root, profile string, out io.Writer) error {
	data, err := trackedPackageInventory(root)
	if err != nil {
		return err
	}
	exclusionData, err := os.ReadFile(filepath.Join(root, "tests", "coverage", "exclusions.json"))
	if err != nil {
		return err
	}
	var exclusions map[string]string
	if err := json.Unmarshal(exclusionData, &exclusions); err != nil {
		return err
	}
	files, err := quality.SourceInventory(bytes.NewReader(data), exclusions)
	if err != nil {
		return err
	}
	input, err := os.Open(profile)
	if err != nil {
		return err
	}
	defer input.Close()
	coverage, err := quality.CountCoverage([]io.Reader{input}, files)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(coverage); err != nil {
		return err
	}
	if !coverage.Overall.Pass() {
		return fmt.Errorf("overall statement coverage is below 90%%")
	}
	const module = "github.com/pangolin-framework/pangolin/"
	for pkg, counts := range coverage.Packages {
		path := strings.TrimPrefix(pkg, module)
		for _, key := range []string{"kernel", "persistence/uow", "persistence/postgres", "persistence/database", "persistence/mysql", "persistence/sqlite", "security", "tenancy", "events", "platform/audit", "modules/organization", "modules/realtime"} {
			if (path == key || strings.HasPrefix(path, key+"/")) && !counts.Pass() {
				return fmt.Errorf("critical package %s is below 90%%", pkg)
			}
		}
	}
	return nil
}

// gitScope answers whether root's Go inventory must come from git: root must
// be inside a git work tree (rev-parse) AND not itself git-ignored — a test
// fixture placed under an ignored path such as .cache/go-tmp resolves git up
// the ancestor chain but has no git scope of its own, and its files can never
// be enumerated by ls-files. Any other git failure fails closed (no silent
// fallback).
func gitScope(root string) (bool, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return false, fmt.Errorf("git is required to derive the tracked package inventory: %w", err)
	}
	rev := exec.Command("git", "-c", "core.quotepath=false", "rev-parse", "--show-toplevel")
	rev.Dir = root
	var revErr bytes.Buffer
	rev.Stderr = &revErr
	if err := rev.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 128 &&
			strings.Contains(revErr.String(), "not a git repository") {
			return false, nil
		}
		return false, fmt.Errorf("resolving the source repository failed (no silent fallback): %v: %s", err, strings.TrimSpace(revErr.String()))
	}
	check := exec.Command("git", "-c", "core.quotepath=false", "check-ignore", "-q", ".")
	check.Dir = root
	err := check.Run()
	var ignored bool
	switch {
	case err == nil:
		ignored = true
	case errors.Is(err, exec.ErrNotFound):
		return false, fmt.Errorf("git is required to derive the tracked package inventory: %w", err)
	default:
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return false, fmt.Errorf("checking the git ignore state of the inventory root failed (no silent fallback): %v", err)
		}
		if exitError.ExitCode() != 1 {
			return false, fmt.Errorf("checking the git ignore state of the inventory root failed (no silent fallback): unexpected exit %d", exitError.ExitCode())
		}
		ignored = false
	}
	return !ignored, nil
}

// gitFiles returns the tracked and untracked-but-unignored Go files of root's
// git scope. A root genuinely outside any repository — or itself ignored —
// has no git scope; the caller falls back to the historical `go list ./...`
// inventory. No other failure falls back silently.
func gitFiles(root string) ([]string, error) {
	scoped, err := gitScope(root)
	if err != nil {
		return nil, err
	}
	if !scoped {
		return nil, errGitless
	}
	command := exec.Command("git", "-c", "core.quotepath=false", "ls-files", "--cached", "--others", "--exclude-standard", "--", "*.go")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, name := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		name = filepath.ToSlash(name)
		if name != "" && !escapesRoot(name) {
			names = append(names, name)
		}
	}
	return names, nil
}

// escapesRoot reports whether a git-provided relative name leaves the
// inventory root (../ walk-up or absolute paths).
func escapesRoot(name string) bool {
	if name == "" || path.IsAbs(name) || filepath.IsAbs(filepath.FromSlash(name)) {
		return true
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// errGitless marks a root outside any git repository.
var errGitless = errors.New("root outside any git repository")

// trackedPackageInventory reproduces the scripts/m0.py Gate package list: the
// Go packages derived from `git ls-files --cached --others --exclude-standard
// -- *.go`, so ignored generated/bug-reproducer directories under artifacts/
// are excluded while every tracked package stays in the inventory. A module
// genuinely outside a git repository (temporary fixtures, plain checkouts)
// keeps the earlier `go list ./...` inventory semantics; no other error falls
// back silently.
func trackedPackageInventory(root string) ([]byte, error) {
	listing, err := gitFiles(root)
	if errors.Is(err, errGitless) {
		command := exec.Command("go", "list", "-json", "./...")
		command.Dir = root
		return command.Output()
	}
	if err != nil {
		return nil, err
	}
	// A test/temp module placed under a GOTMPDIR that itself lives inside a
	// repository (the gate's m0.go_environment default) resolves git up the
	// ancestor chain; names outside the inventory root (the repo's own files
	// with ../ prefixes) are already dropped by gitFiles, and the nested
	// module boundary still applies to what remains.
	seed := map[string]bool{}
	for _, name := range listing {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			continue
		}
		// Nested modules (a go.mod inside the tree, e.g. examples/basics) are
		// separate module units the historical `./...` patterns never crossed.
		parts := strings.Split(name, "/")
		nested := false
		for index := range parts[:len(parts)-1] {
			if _, err := os.Stat(filepath.Join(root, filepath.Join(parts[:index+1]...), "go.mod")); err == nil {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		seed[path.Dir(name)] = true
	}
	if len(seed) == 0 {
		return nil, fmt.Errorf("tracked go package inventory is empty")
	}
	dirs := make([]string, 0, len(seed))
	for dir := range seed {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for index := range dirs {
		if dirs[index] == "." {
			dirs[index] = "."
			continue
		}
		dirs[index] = "./" + dirs[index]
	}
	// scripts/m0.py semantics: directories whose tracked Go files are fully
	// excluded by build constraints (integration-tagged tests/mN suites) only
	// run under -tags=integration; the default-constraint coverage inventory
	// matches the historical `./...` selection without the ignored dirs.
	list, err := exec.Command("go", append([]string{"list", "-e", "-json"}, dirs...)...).Output()
	if err != nil {
		return nil, err
	}
	decoder, entries := json.NewDecoder(bytes.NewReader(list)), 0
	var inventory []string
	for {
		var pkg struct {
			ImportPath string
			Dir        string
			Error      *struct{ Err string }
		}
		if err := decoder.Decode(&pkg); err != nil {
			break
		}
		entries++
		if pkg.Error != nil && strings.Contains(pkg.Error.Err, "build constraints exclude") {
			continue
		}
		switch {
		case pkg.ImportPath == "github.com/pangolin-framework/pangolin":
			inventory = append(inventory, ".")
		case strings.HasPrefix(pkg.ImportPath, "github.com/pangolin-framework/pangolin/"):
			inventory = append(inventory, "./"+strings.TrimPrefix(pkg.ImportPath, "github.com/pangolin-framework/pangolin/"))
		default:
			inventory = append(inventory, pkg.ImportPath)
		}
	}
	if entries == 0 || len(inventory) == 0 {
		return nil, fmt.Errorf("default go package inventory is empty")
	}
	sort.Strings(inventory)
	listCommand := exec.Command("go", append([]string{"list", "-json"}, inventory...)...)
	listCommand.Dir = root
	return listCommand.Output()
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
