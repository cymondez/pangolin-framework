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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cymondez/pangolin-framework/kernel/module"
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
	Dir      string
	Out, Err io.Writer
	Command  func(context.Context, string, ...string) ([]byte, error)
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
		fmt.Fprintln(c.Out, "pangolin 0.1.0\nnew NAME --template modular-monolith [--with organization] [--realtime]\nmodule add NAME [--depends persistence,authorization]\nmigrate create NAME --module MODULE\nmigrate status\ndoctor\ngenerate openapi\nEach command accepts --help.")
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
	realtimeGroup := "no-realtime"
	if realtime {
		realtimeGroup = "realtime"
	}
	err = fs.WalkDir(assets, "assets", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(path, "assets/")
		if relative == "common/runtime-manifest.json.tmpl" {
			return nil
		}
		parts := strings.SplitN(relative, "/", 2)
		if len(parts) != 2 {
			return nil
		}
		group, destination := parts[0], strings.TrimSuffix(parts[1], ".tmpl")
		if group != "common" && group != "runtime" && group != profile && group != realtimeGroup {
			return nil
		}
		if group == "runtime" {
			if with == "" && strings.HasPrefix(destination, "modules/organization/") {
				return nil
			}
			if !realtime && strings.HasPrefix(destination, "modules/realtime/") {
				return nil
			}
			destination = "framework/" + destination
		}
		raw, err := fs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		if group != "runtime" {
			raw = []byte(strings.ReplaceAll(string(raw), "{{MODULE}}", "example.com/"+name))
		}
		return writeFile(filepath.Join(stage, filepath.FromSlash(destination)), raw)
	})
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
	raw := "package modules\nimport (\n\"github.com/cymondez/pangolin-framework/kernel/module\"\n" + imports.String() + ")\n" +
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
			fmt.Fprintln(c.Out, "module add NAME [--depends persistence,authorization]")
		}
		return err
	}
	var deps string
	f, err := c.flags("module add", rest, func(f *flag.FlagSet) { f.StringVar(&deps, "depends", "", "comma-separated registered dependencies") })
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
	if deps != "" {
		dependencies = strings.Split(deps, ",")
	}
	p.Modules[name] = dependencies
	if err = validateModules(p.Modules); err != nil {
		return err
	}
	if err = c.safeDirectory("modules"); err != nil {
		return err
	}
	target := filepath.Join(c.Dir, "modules", name)
	if _, err = os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return errors.New("module directory already exists")
	}
	var body strings.Builder
	importList := []string{"\"github.com/cymondez/pangolin-framework/kernel/module\""}
	var refs strings.Builder
	framework := false
	for _, d := range dependencies {
		if d == "realtime" && p.Realtime {
			importList = append(importList, `"github.com/cymondez/pangolin-framework/modules/realtime"`)
			refs.WriteString("module.Of[realtime.Module](),")
			continue
		}
		switch d {
		case "persistence", "authorization", "organization":
			// Framework capability names translate into the foundation Type
			// edge below; they are never silently omitted.
			framework = true
			continue
		case "bookstore":
			return errors.New("bookstore is the generated domain sample: user modules depend on foundation capabilities (persistence, authorization) or registered user modules")
		}
		if d == name || !namePattern.MatchString(d) {
			continue
		}
		importList = append(importList, fmt.Sprintf("\"example.com/%s/modules/%s\"", p.Name, d))
		refs.WriteString("module.Of[" + d + ".Module](),")
	}
	if framework {
		importList = append(importList, fmt.Sprintf("%q", "example.com/"+p.Name+"/foundation"))
	}
	fmt.Fprintf(&body, "package %s\nimport (\n%s\n)\ntype Module struct{}\n", name, strings.Join(importList, "\n"))
	fmt.Fprintf(&body, "func (m *Module) ModuleMetadata() module.Metadata { return module.Metadata{Name: %q, Version: %q} }\n", name, Version)
	if framework || refs.Len() > 0 {
		// Root-mode Type dependencies: direct user module edges plus the
		// foundation module that owns every framework contract. Framework
		// composition (HTTP, drain) stays in internal/app.
		fmt.Fprintf(&body, "func (m *Module) DependsOn() []module.Ref { return []module.Ref{")
		if framework {
			body.WriteString("module.Of[foundation.Module](),")
		}
		body.WriteString(refs.String())
		body.WriteString("} }\n")
	}
	body.WriteString("func (m *Module) PreConfigure(b module.RootBuilder) error { return b.SetDefault(\"" + name + ".installed\", \"true\") }\n")
	body.WriteString("// Configure registers your constructors. Framework services are typed\n")
	body.WriteString("// constructor dependencies owned by the foundation module, for example:\n")
	body.WriteString("//\n")
	body.WriteString("// func New" + strings.ToUpper(name[:1]) + name[1:] + "Service(units uow.UnitOfWorkManager, scopes foundation.Scopes) *New" + strings.ToUpper(name[:1]) + name[1:] + "Service {\n")
	body.WriteString("//\treturn &New" + strings.ToUpper(name[:1]) + name[1:] + "Service{units: units, scopes: scopes}\n")
	body.WriteString("// }\n")
	body.WriteString("//\n")
	body.WriteString("// return b.Services().Add(New" + strings.ToUpper(name[:1]) + name[1:] + "Service)\n")
	body.WriteString("func (m *Module) Configure(b module.RootBuilder) error { return nil }\n")
	body.WriteString("// Start and Stop are optional; Start resolves services via rt.Services().\n")
	raw, err := format.Source([]byte(body.String()))
	if err != nil {
		return err
	}
	if err = writeFile(filepath.Join(target, "module.go"), raw); err != nil {
		return err
	}
	if err = writeFile(filepath.Join(target, "README.md"), []byte("# "+name+"\n\nModule() declares root-mode module metadata and Type dependencies. Register constructors in Configure via b.Services().Add(...); resolve them in Start from rt.Services() with the exact concrete type. Framework services are typed constructor dependencies owned by the foundation module: declare --depends persistence/authorization (or organization) to add the foundation.Type edge and inject the published contracts (uow.UnitOfWorkManager, foundation.Scopes, foundation.Publisher, foundation.Idempotency). HTTP and drain composition stays in internal/app. Run go test ./... from the project root.\n")); err != nil {
		return err
	}
	if err = writeRegistry(c.Dir, p); err != nil {
		return err
	}
	return writeFile(filepath.Join(c.Dir, "pangolin.json"), jsonBytes(p))
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
	f, err := c.flags("doctor", args, func(*flag.FlagSet) {})
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
	for _, cmd := range [][]string{{"go", "version"}, {"docker", "compose", "version"}} {
		command := c.Command
		if command == nil {
			command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return exec.CommandContext(ctx, name, args...).Output()
			}
		}
		out, err := command(ctx, cmd[0], cmd[1:]...)
		if err != nil {
			return commandError{fmt.Sprintf("%s check failed", cmd[0]), err}
		}
		if cmd[0] == "go" && !strings.Contains(string(out), "go1.27.4 ") {
			return errors.New("Go 1.27.4 required by this template")
		}
		fmt.Fprint(c.Out, string(out))
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, "framework", "go.mod"))
	if err != nil || !strings.Contains(string(raw), "module github.com/cymondez/pangolin-framework") {
		return errors.New("bundled runtime missing")
	}
	var manifest map[string]string
	raw, err = os.ReadFile(filepath.Join(c.Dir, "runtime-manifest.json"))
	if err != nil || json.Unmarshal(raw, &manifest) != nil || len(manifest) == 0 {
		return errors.New("runtime manifest missing")
	}
	for path, expected := range manifest {
		if !strings.HasPrefix(path, "framework/") || strings.Contains(path, "..") || filepath.IsAbs(path) {
			return errors.New("invalid runtime manifest path")
		}
		raw, err = os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(path)))
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != expected {
			return errors.New("bundled runtime checksum mismatch")
		}
	}
	fmt.Fprintln(c.Out, "project", p.Name, "profile", p.Profile, "runtime", p.Version)
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
