// Command check validates the M0 contract inventory. It does not certify runtime behavior.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cymondez/pangolin-framework/internal/quality"
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
	const module = "github.com/cymondez/pangolin-framework/"
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

// gitRepository reports whether root belongs to a git work tree, preserving
// the historical behavior for modules genuinely outside a repository while
// failing closed on any other git resolution error (missing binary,
// permissions, corrupted repository).
func gitRepository(root string) (bool, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return false, fmt.Errorf("git is required to derive the tracked package inventory: %w", err)
	}
	command := exec.Command("git", "-c", "core.quotepath=false", "rev-parse", "--show-toplevel")
	command.Dir = root
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return true, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 128 &&
		strings.Contains(stderr.String(), "not a git repository") {
		return false, nil
	}
	return false, fmt.Errorf("resolving the source repository failed (no silent fallback): %v: %s", err, strings.TrimSpace(stderr.String()))
}

// trackedPackageInventory reproduces the scripts/m0.py Gate package list: the
// Go packages derived from `git ls-files --cached --others --exclude-standard
// -- *.go`, so ignored generated/bug-reproducer directories under artifacts/
// are excluded while every tracked package stays in the inventory. A module
// genuinely outside a git repository (temporary fixtures, plain checkouts)
// keeps the earlier `go list ./...` inventory semantics; no other error falls
// back silently.
func trackedPackageInventory(root string) ([]byte, error) {
	repository, err := gitRepository(root)
	if err != nil {
		return nil, err
	}
	if !repository {
		command := exec.Command("go", "list", "-json", "./...")
		command.Dir = root
		return command.Output()
	}
	command := exec.Command("git", "-c", "core.quotepath=false", "ls-files", "--cached", "--others", "--exclude-standard", "--", "*.go")
	command.Dir = root
	listing, err := command.Output()
	if err != nil {
		return nil, err
	}
	seed := map[string]bool{}
	for _, name := range strings.Split(strings.ReplaceAll(string(listing), "\r\n", "\n"), "\n") {
		name = filepath.ToSlash(name)
		if name == "" {
			continue
		}
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
		case pkg.ImportPath == "github.com/cymondez/pangolin-framework":
			inventory = append(inventory, ".")
		case strings.HasPrefix(pkg.ImportPath, "github.com/cymondez/pangolin-framework/"):
			inventory = append(inventory, "./"+strings.TrimPrefix(pkg.ImportPath, "github.com/cymondez/pangolin-framework/"))
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
