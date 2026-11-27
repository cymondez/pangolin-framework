// Package scaffold implements the version-bound pangolin developer CLI.
package scaffold

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pangolin-framework/pangolin/kernel/module"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const Version = "0.1.0"

type commandError struct {
	message string
	cause   error
}

func (e commandError) Error() string { return e.message }
func (e commandError) Unwrap() error { return e.cause }

//go:embed all:assets
var assets embed.FS

type Project struct {
	Name     string              `json:"name"`
	Version  string              `json:"version"`
	Profile  string              `json:"profile"`
	Realtime bool                `json:"realtime,omitempty"`
	Modules  map[string][]string `json:"modules"`
}
type CLI struct {
	Dir                 string
	Out, Err            io.Writer
	Command             func(context.Context, string, ...string) ([]byte, error)
	HTTPClient          HTTPDoer
	PingDatabase        func(context.Context, string) error
	ReadMigrationLedger func(context.Context, string) ([]MigrationLedgerEntry, error)
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var projectPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

func (c CLI) Run(ctx context.Context, args []string) int {
	if c.Out == nil {
		c.Out = io.Discard
	}
	if c.Err == nil {
		c.Err = io.Discard
	}
	err := c.run(ctx, args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(c.Err, err)
		return 1
	}
	return 0
}
func (c CLI) flags(name string, args []string, setup func(*flag.FlagSet)) (*flag.FlagSet, error) {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(c.Out)
	setup(f)
	err := f.Parse(args)
	return f, err
}
func (c CLI) run(ctx context.Context, args []string) error {
	if ctx == nil {
		return errors.New("context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(c.Out, "pangolin 0.1.0\nnew NAME --template modular-monolith [--with organization] [--realtime]\nmodule add NAME [--depends persistence,authorization] [--preset service|crud]\nmigrate create NAME --module MODULE\nmigrate status\ndoctor [--live]\ngenerate openapi\nupgrade plan\nupgrade --dry-run\nupgrade runtime\nupgrade verify\nEach command accepts --help.")
		return nil
	}
	if args[0] == "--version" && len(args) == 1 {
		fmt.Fprintln(c.Out, Version)
		return nil
	}
	switch args[0] {
	case "new":
		return c.newProject(args[1:])
	case "module":
		if len(args) > 1 && args[1] == "add" {
			return c.addModule(args[2:])
		}
	case "migrate":
		if len(args) > 1 {
			switch args[1] {
			case "create":
				return c.createMigration(args[2:])
			case "status":
				return c.status(ctx, args[2:])
			}
		}
	case "doctor":
		return c.doctor(ctx, args[1:])
	case "generate":
		if len(args) > 1 && args[1] == "openapi" {
			return c.openapi(args[2:])
		}
	case "upgrade":
		if len(args) > 1 && args[1] == "--help" {
			fmt.Fprintln(c.Out, "upgrade plan\nupgrade --dry-run\nupgrade runtime\nupgrade verify")
			return nil
		}
		return c.upgrade(ctx, args[1:])
	}
	if len(args) == 2 && args[1] == "--help" {
		fmt.Fprintln(c.Out, "Use pangolin --help for supported subcommands.")
		return nil
	}
	return errors.New("unknown command; use pangolin --help")
}
func splitName(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New("name required")
	}
	if args[0] == "--help" {
		return "", nil, flag.ErrHelp
	}
	if !namePattern.MatchString(args[0]) || token.Lookup(args[0]).IsKeyword() || args[0] == "main" {
		return "", nil, errors.New("name must be lowercase letters, digits or underscore, starting with a letter")
	}
	return args[0], args[1:], nil
}
func splitProjectName(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New("project name required")
	}
	if args[0] == "--help" {
		return "", nil, flag.ErrHelp
	}
	if !projectPattern.MatchString(args[0]) {
		return "", nil, errors.New("project name must start with a lowercase letter and contain lowercase letters, digits, underscore or hyphen")
	}
	return args[0], args[1:], nil
}
func (c CLI) project() (Project, error) {
	var p Project
	info, err := os.Lstat(filepath.Join(c.Dir, "pangolin.json"))
	if err != nil || !info.Mode().IsRegular() {
		return p, errors.New("regular pangolin.json required")
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, "pangolin.json"))
	if err != nil {
		return p, errors.New("run inside a pangolin project")
	}
	if json.Unmarshal(raw, &p) != nil || !projectPattern.MatchString(p.Name) || p.Version != Version || p.Profile != "tenant-only" && p.Profile != "organization-enabled" || p.Modules == nil {
		return p, errors.New("invalid or incompatible pangolin.json")
	}
	if err = validateModules(p.Modules); err != nil {
		return p, err
	}
	return p, nil
}
func validateModules(modules map[string][]string) error {
	r := module.NewRegistry()
	for name, deps := range modules {
		if !namePattern.MatchString(name) {
			return errors.New("invalid module name")
		}
		m := module.Module{Name: name, Version: Version}
		for _, d := range deps {
			m.DependsOn = append(m.DependsOn, module.Dependency{Name: d, VersionConstraint: "=" + Version})
		}
		if err := r.Register(m); err != nil {
			return err
		}
	}
	_, err := r.Plan()
	return err
}
func jsonBytes(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
func writeFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}
func exclusive(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	return errors.Join(err, f.Close())
}

// safeDirectory rejects symlinked ancestors before writing command-owned files.
func (c CLI) safeDirectory(relative string) error {
	current := c.Dir
	for _, part := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if part == "." {
			continue
		}
		if part == ".." {
			return errors.New("path escapes project")
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("unsafe project directory")
		}
	}
	return nil
}
func (c CLI) newProject(args []string) error {
	name, rest, err := splitProjectName(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(c.Out, "new NAME --template modular-monolith [--with organization] [--realtime]")
		}
		return err
	}
	var template, with string
	var realtime bool
	f, err := c.flags("new", rest, func(f *flag.FlagSet) {
		f.StringVar(&template, "template", "modular-monolith", "project template")
		f.StringVar(&with, "with", "", "optional organization module")
		f.BoolVar(&realtime, "realtime", false, "include the optional SignalR endpoint")
	})
	if err != nil {
		return err
	}
	if f.NArg() != 0 || template != "modular-monolith" || with != "" && with != "organization" {
		return errors.New("unsupported template or module option")
	}
	target := filepath.Join(c.Dir, name)
	if _, err = os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return errors.New("target already exists; no files changed")
	}
	stage, err := os.MkdirTemp(c.Dir, ".pangolin-new-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	profile := "tenant-only"
	if with == "organization" {
		profile = "organization-enabled"
	}
	projectFiles, err := embeddedProjectFiles(name, profile, realtime)
	if err != nil {
		return err
	}
	orderedDestinations := make([]string, 0, len(projectFiles))
	for destination := range projectFiles {
		orderedDestinations = append(orderedDestinations, destination)
	}
	sort.Strings(orderedDestinations)
	for _, destination := range orderedDestinations {
		if err := writeFile(filepath.Join(stage, filepath.FromSlash(destination)), projectFiles[destination]); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	manifest := map[string]string{}
	if err = filepath.WalkDir(filepath.Join(stage, "framework"), func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		relative, e := filepath.Rel(stage, path)
		if e != nil {
			return e
		}
		manifest[filepath.ToSlash(relative)] = fmt.Sprintf("%x", sha256.Sum256(raw))
		return nil
	}); err != nil {
		return err
	}
	if err = writeFile(filepath.Join(stage, "runtime-manifest.json"), jsonBytes(manifest)); err != nil {
		return err
	}
	p := Project{Name: name, Version: Version, Profile: profile, Realtime: realtime, Modules: map[string][]string{"persistence": {}, "authorization": {}, "bookstore": {"persistence", "authorization"}}}
	if with != "" {
		p.Modules["organization"] = []string{"persistence", "authorization"}
	}
	if realtime {
		p.Modules["realtime"] = []string{"authorization"}
	}
	if err = writeFile(filepath.Join(stage, "pangolin.json"), jsonBytes(p)); err != nil {
		return err
	}
	if err = writeRegistry(stage, p); err != nil {
		return err
	}
	if err = os.Rename(stage, target); err != nil {
		return err
	}
	fmt.Fprintln(c.Out, "Created", name, "profile", profile, "runtime", Version)
	return nil
}

// registryBytes emits the version-bound generated modules root file in the
// root-mode form: an exported UserRoot aggregator whose DependsOn declares
// every user module Type, plus ForRoot() returning the instances handed to
// StartRoot. Framework composition (pools, registry, HTTP) stays in the app
// template; user modules depend only on each other and their own services.
func registryBytes(p Project) ([]byte, error) {
	names := make([]string, 0, len(p.Modules))
	for name := range p.Modules {
		names = append(names, name)
	}
	sort.Strings(names)
	var imports, refs strings.Builder
	for _, name := range names {
		if name == "persistence" || name == "authorization" || name == "bookstore" || name == "organization" || (name == "realtime" && p.Realtime) {
			continue
		}
		fmt.Fprintf(&imports, "m_%s %q\n", name, "example.com/"+p.Name+"/modules/"+name)
		fmt.Fprintf(&refs, "module.Of[m_%s.Module](),\n", name)
	}
	raw := "package modules\nimport (\n\"github.com/pangolin-framework/pangolin/kernel/module\"\n" + imports.String() + ")\n" +
		"\n// UserRoot aggregates every user module as one typed root dependency;\n" +
		"// the loader plans and dedupes reachable Types exactly once.\n" +
		"type UserRoot struct{}\n" +
		"func (UserRoot) ModuleMetadata() module.Metadata { return module.Metadata{Name: \"user-modules\"} }\n" +
		"func (UserRoot) DependsOn() []module.Ref { return []module.Ref{\n" + refs.String() + "} }\n" +
		"\n// ForRoot returns the user root instances handed to runtime.StartRoot.\n" +
		"func ForRoot() []any { return []any{&UserRoot{}} }\n"
	out, err := format.Source([]byte(raw))
	if err != nil {
		return nil, err
	}
	return out, nil
}
func writeRegistry(root string, p Project) error {
	raw, err := registryBytes(p)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(root, "modules", "registry.go"), raw)
}
func (c CLI) addModule(args []string) error {
	name, rest, err := splitName(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(c.Out, "module add NAME [--depends persistence,authorization] [--preset service|crud]")
		}
		return err
	}
	var deps, preset string
	f, err := c.flags("module add", rest, func(f *flag.FlagSet) {
		f.StringVar(&deps, "depends", "", "comma-separated registered dependencies")
		f.StringVar(&preset, "preset", "", "module generation preset: service (crud is unsupported)")
	})
	if err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	preset = strings.TrimSpace(strings.ToLower(preset))
	if preset != "" && preset != "service" {
		return fmt.Errorf("unsupported preset %q: only %q is supported; no files changed", preset, "service")
	}
	unlock, err := acquireProjectModuleLock(c.Dir)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()
	p, err := c.project()
	if err != nil {
		return err
	}
	if _, ok := p.Modules[name]; ok {
		return errors.New("module already registered")
	}
	previousRegistry, err := registryBytes(p)
	if err != nil {
		return err
	}
	registryPath := filepath.Join(c.Dir, "modules", "registry.go")
	info, err := os.Lstat(registryPath)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("regular module registry required")
	}
	currentRegistry, err := os.ReadFile(registryPath)
	if err != nil || string(currentRegistry) != string(previousRegistry) {
		return errors.New("module registry was edited; existing files preserved")
	}
	dependencies := []string{}
	if strings.TrimSpace(deps) != "" {
		for _, dependency := range strings.Split(deps, ",") {
			dependencies = append(dependencies, strings.TrimSpace(dependency))
		}
	}
	if err = validatePresetDependencies(p, name, dependencies); err != nil {
		return err
	}
	p.Modules[name] = dependencies
	if err = validateModules(p.Modules); err != nil {
		return fmt.Errorf("%w; valid candidates: %s", err, strings.Join(validModuleCandidates(p, name), ", "))
	}
	if err = c.safeDirectory("modules"); err != nil {
		return err
	}
	target := filepath.Join(c.Dir, "modules", name)
	if _, err = os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return errors.New("module directory already exists")
	}

	previousRegistry, err = os.ReadFile(registryPath)
	if err != nil {
		return errors.New("module registry changed; existing files preserved")
	}
	previousProject, err := os.ReadFile(filepath.Join(c.Dir, "pangolin.json"))
	if err != nil {
		return errors.New("pangolin.json changed; existing files preserved")
	}

	stage, err := os.MkdirTemp(c.Dir, ".pangolin-module-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = c.writePresetModule(stage, name, preset, p, dependencies); err != nil {
		return err
	}

	temporaryRegistry, err := registryBytes(p)
	if err != nil {
		return err
	}
	if err = writeFile(filepath.Join(stage, "registry.go"), temporaryRegistry); err != nil {
		return err
	}
	if err = writeFile(filepath.Join(stage, "pangolin.json"), jsonBytes(p)); err != nil {
		return err
	}
	if err = c.commitStagedModule(target, stage, registryPath, filepath.Join(c.Dir, "pangolin.json"), previousRegistry, previousProject); err != nil {
		return fmt.Errorf("module generation failed: %w", err)
	}
	fmt.Fprintln(c.Out, "Generated", name, "preset", func() string {
		if preset == "" {
			return "skeleton"
		}
		return preset
	}())
	return nil
}
func (c CLI) createMigration(args []string) error {
	name, rest, err := splitName(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(c.Out, "migrate create NAME --module MODULE")
		}
		return err
	}
	var owner string
	f, err := c.flags("migrate create", rest, func(f *flag.FlagSet) { f.StringVar(&owner, "module", "", "registered module") })
	if err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	p, err := c.project()
	if err != nil {
		return err
	}
	if _, ok := p.Modules[owner]; !ok || !namePattern.MatchString(owner) {
		return errors.New("registered --module required")
	}
	if err = c.safeDirectory(filepath.Join("modules", owner, "migrations")); err != nil {
		return err
	}
	dir := filepath.Join(c.Dir, "modules", owner, "migrations")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_"+name+".sql") {
			return errors.New("migration already exists")
		}
	}
	// A project-wide monotonic sequence keeps module files globally unique.
	var maximum int64 = 5000000
	err = filepath.WalkDir(filepath.Join(c.Dir, "modules"), func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sql") {
			var n int64
			if _, e = fmt.Sscanf(filepath.Base(path), "%d_", &n); e == nil && n > maximum {
				maximum = n
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return exclusive(filepath.Join(dir, fmt.Sprintf("%d_%s.sql", maximum+1, name)), []byte("-- +goose Up\n-- Add forward-only SQL, owner constraints, grants and FORCE RLS here.\n\n-- +goose Down\nSELECT 1;\n"))
}
func (c CLI) status(ctx context.Context, args []string) error {
	f, err := c.flags("migrate status", args, func(*flag.FlagSet) {})
	if err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if _, err = c.project(); err != nil {
		return err
	}
	dsn := os.Getenv("PANGOLIN_MIGRATION_DSN")
	if dsn == "" {
		return errors.New("PANGOLIN_MIGRATION_DSN required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return commandError{"invalid migration connection", err}
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT version_id,is_applied FROM pangolin.goose_db_version ORDER BY id")
	if err != nil {
		return commandError{"migration status unavailable; apply deployment migrations first", err}
	}
	defer rows.Close()
	for rows.Next() {
		var version int64
		var applied bool
		if err = rows.Scan(&version, &applied); err != nil {
			return err
		}
		fmt.Fprintf(c.Out, "%d applied=%t\n", version, applied)
	}
	return rows.Err()
}
func (c CLI) doctor(ctx context.Context, args []string) error {
	var live bool
	f, err := c.flags("doctor", args, func(f *flag.FlagSet) {
		f.BoolVar(&live, "live", false, "check Compose, database, migration ledger and API readiness")
	})
	if err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	_, err = c.doctorStatic(ctx)
	if err != nil {
		return err
	}
	if live {
		if err = c.doctorLive(ctx); err != nil {
			return errors.New("doctor --live completed with failures")
		}
		fmt.Fprintln(c.Out, "[PASS] doctor --live")
		return nil
	}
	fmt.Fprintln(c.Out, "[SKIP] database, requires --live")
	fmt.Fprintln(c.Out, "[SKIP] migrations, requires --live")
	return nil
}
func (c CLI) openapi(args []string) error {
	f, err := c.flags("generate openapi", args, func(*flag.FlagSet) {})
	if err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	p, err := c.project()
	if err != nil {
		return err
	}
	raw, err := fs.ReadFile(assets, "assets/"+p.Profile+"/openapi.json.tmpl")
	if err != nil {
		return err
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return errors.New("invalid OpenAPI template")
	}
	target := filepath.Join(c.Dir, "openapi.json")
	previous, err := os.ReadFile(target)
	if err == nil {
		if string(previous) == string(raw) {
			return nil
		}
		return errors.New("openapi.json differs; existing file preserved")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return exclusive(target, raw)
}
