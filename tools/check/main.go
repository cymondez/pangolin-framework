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
	"path/filepath"
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
	command := exec.Command("go", "list", "-json", "./...")
	command.Dir = root
	data, err := command.Output()
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
		for _, key := range []string{"kernel", "persistence/uow", "persistence/postgres", "persistence/database", "persistence/mysql", "persistence/sqlite", "security", "tenancy", "events", "platform/audit", "modules/organization"} {
			if (path == key || strings.HasPrefix(path, key+"/")) && !counts.Pass() {
				return fmt.Errorf("critical package %s is below 90%%", pkg)
			}
		}
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
