package scaffold

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type MigrationLedgerEntry struct {
	Version int64
	Applied bool
}

func validModuleCandidates(p Project, self string) []string {
	seen := map[string]bool{}
	candidates := make([]string, 0, len(p.Modules)+4)
	add := func(name string) {
		if name == "" || name == self || name == "bookstore" || seen[name] {
			return
		}
		seen[name] = true
		candidates = append(candidates, name)
	}
	add("persistence")
	add("authorization")
	if p.Profile == "organization-enabled" {
		add("organization")
	}
	if p.Realtime {
		add("realtime")
	}
	for name := range p.Modules {
		if name != "persistence" && name != "authorization" && name != "organization" && name != "realtime" {
			add(name)
		}
	}
	sort.Strings(candidates)
	return candidates
}

func validatePresetDependencies(p Project, self string, dependencies []string) error {
	valid := map[string]bool{}
	for _, candidate := range validModuleCandidates(p, self) {
		valid[candidate] = true
	}
	seen := map[string]bool{}
	for _, raw := range dependencies {
		dependency := strings.TrimSpace(raw)
		if dependency == "" {
			return fmt.Errorf("invalid empty dependency for module %s; valid candidates: %s", self, strings.Join(validModuleCandidates(p, self), ", "))
		}
		if dependency == "bookstore" {
			return fmt.Errorf("dependency path %s -> bookstore: bookstore is the generated sample and cannot be used as a user module dependency; valid candidates: %s", self, strings.Join(validModuleCandidates(p, self), ", "))
		}
		if seen[dependency] {
			return fmt.Errorf("dependency path %s -> %s: duplicate dependency; valid candidates: %s", self, dependency, strings.Join(validModuleCandidates(p, self), ", "))
		}
		seen[dependency] = true
		if dependency == self {
			continue
		}
		if !valid[dependency] {
			return fmt.Errorf("dependency path %s -> %s: missing module; valid candidates: %s", self, dependency, strings.Join(validModuleCandidates(p, self), ", "))
		}
	}
	return nil
}

func pascalName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' })
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	if len(parts) == 0 {
		return "Service"
	}
	return strings.Join(parts, "")
}

func formatGeneratedGo(relative string, raw []byte) ([]byte, error) {
	if strings.HasSuffix(relative, ".go") {
		formatted, err := format.Source(raw)
		if err != nil {
			return nil, fmt.Errorf("format generated %s: %w", relative, err)
		}
		return formatted, nil
	}
	return raw, nil
}

func (c CLI) writePresetModule(stage, name, preset string, p Project, dependencies []string) error {
	target := filepath.Join(stage, "module")
	if preset == "service" {
		var imports strings.Builder
		if len(dependencies) > 0 {
			for _, dependency := range dependencies {
				if dependency == "realtime" && p.Realtime {
					continue
				}
			}
		}
		imports.WriteString("\t\"example.com/" + p.Name + "/foundation\"\n")
		imports.WriteString("\t\"example.com/" + p.Name + "/modules/" + name + "/application\"\n")
		for _, dependency := range dependencies {
			if dependency == "realtime" && p.Realtime {
				imports.WriteString("\t\"github.com/cymondez/pangolin-framework/modules/realtime\"\n")
				continue
			}
			if dependency != "persistence" && dependency != "authorization" && dependency != "organization" && dependency != name && namePattern.MatchString(dependency) {
				imports.WriteString("\tm_" + dependency + " \"example.com/" + p.Name + "/modules/" + dependency + "\"\n")
			}
		}

		var moduleRefs strings.Builder
		moduleRefs.WriteString("module.Of[foundation.Module]()")
		for _, dependency := range dependencies {
			if dependency == "realtime" && p.Realtime {
				moduleRefs.WriteString(", module.Of[realtime.Module]()")
				continue
			}
			if dependency == "persistence" || dependency == "authorization" || dependency == "organization" {
				continue
			}
			if dependency != name && namePattern.MatchString(dependency) {
				moduleRefs.WriteString(", module.Of[m_" + dependency + ".Module]()")
			}
		}

		moduleGo := fmt.Sprintf(`package %s

import (
	"context"
%s	"github.com/cymondez/pangolin-framework/kernel/module"
)

type Module struct{}

func (m *Module) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: %q, Version: %q}
}

func (m *Module) DependsOn() []module.Ref {
	return []module.Ref{%s}
}

func (m *Module) PreConfigure(b module.RootBuilder) error {
	return b.SetDefault(%q, "true")
}

func (m *Module) Configure(b module.RootBuilder) error {
	return module.AddSingleton(b.Services(), application.NewService)
}

func (m *Module) Start(ctx context.Context, rt module.Runtime) error {
	_, err := module.Resolve[*application.Service](ctx, rt.Services())
	return err
}

var _ application.Contract = (*application.Service)(nil)
`, name, imports.String(), name, Version, moduleRefs.String(), name+".installed")
		moduleGoBytes, err := format.Source([]byte(moduleGo))
		if err != nil {
			return fmt.Errorf("format generated module: %w", err)
		}
		if err = writeFile(filepath.Join(target, "module.go"), moduleGoBytes); err != nil {
			return err
		}

		contractGo := fmt.Sprintf(`package application

import (
	"context"
	"errors"
)

var ErrUnavailable = errors.New(%q)

type Status struct {
	Name  string
	Ready bool
}

type Contract interface {
	Status(context.Context) (Status, error)
}
`, strings.ToUpper(name)+"_UNAVAILABLE")
		contractBytes, err := formatGeneratedGo("contracts.go", []byte(contractGo))
		if err != nil {
			return err
		}
		if err = writeFile(filepath.Join(target, "application", "contracts.go"), contractBytes); err != nil {
			return err
		}

		serviceGo := fmt.Sprintf(`package application

import (
	"context"
	"errors"

	frameworkapplication "github.com/cymondez/pangolin-framework/application"
	"github.com/cymondez/pangolin-framework/persistence/uow"

	"example.com/%s/foundation"
)

type Service struct {
	units  uow.UnitOfWorkManager
	scopes foundation.ScopeAccess
}

func NewService(units uow.UnitOfWorkManager, scopes foundation.ScopeAccess) *Service {
	return &Service{units: units, scopes: scopes}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	if ctx == nil {
		return Status{}, errors.Join(ErrUnavailable, errors.New("context required"))
	}
	if err := ctx.Err(); err != nil {
		return Status{}, errors.Join(ErrUnavailable, err)
	}
	if s == nil || s.units == nil || s.scopes == nil {
		return Status{}, ErrUnavailable
	}
	return frameworkapplication.QueryResult(ctx, s.units, func(work context.Context) (Status, error) {
		if work == nil {
			return Status{}, errors.Join(ErrUnavailable, errors.New("work context required"))
		}
		if err := work.Err(); err != nil {
			return Status{}, errors.Join(ErrUnavailable, err)
		}
		return Status{Name: %q, Ready: true}, nil
	})
}
`, p.Name, name)
		serviceBytes, err := formatGeneratedGo("service.go", []byte(serviceGo))
		if err != nil {
			return err
		}
		if err = writeFile(filepath.Join(target, "application", "service.go"), serviceBytes); err != nil {
			return err
		}

		serviceTest := fmt.Sprintf(`package application

import (
	"context"
	"errors"
	"testing"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"

	"example.com/%s/foundation"
)

type testManager struct{}

func (m *testManager) Within(ctx context.Context, _ uow.Options, callback func(context.Context) error) error {
	return callback(ctx)
}

func (m *testManager) Current(context.Context) (uow.Scope, bool) { return nil, false }

type testScopes struct{}

func (testScopes) Bind(ctx context.Context, _ scope.ScopeRef) (context.Context, error) { return ctx, nil }
func (testScopes) Authorize(context.Context, string) error { return nil }
func (testScopes) Current(context.Context) (scope.ScopeBinding, bool) { return scope.ScopeBinding{}, false }
func (testScopes) Admit(_ context.Context, binding scope.ScopeBinding) (scope.ScopeBinding, error) { return binding, nil }

var _ foundation.ScopeAccess = testScopes{}

func TestServiceStatus(t *testing.T) {
	service := NewService(&testManager{}, testScopes{})
	status, err := service.Status(context.Background())
	if err != nil || !status.Ready || status.Name != %q {
		t.Fatalf("status=%%+v err=%%v", status, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = service.Status(canceled); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("canceled status: %%v", err)
	}

	if _, err = (&Service{}).Status(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable status: %%v", err)
	}
}
`, p.Name, name)
		serviceTestBytes, err := formatGeneratedGo("service_test.go", []byte(serviceTest))
		if err != nil {
			return err
		}
		if err = writeFile(filepath.Join(target, "application", "service_test.go"), serviceTestBytes); err != nil {
			return err
		}

		readme := fmt.Sprintf("# %s service preset\n\n"+
			"This module is a vertical slice generated by `pangolin module add %s --preset service`.\n\n"+
			"- `ModuleMetadata` and `DependsOn` register the module and foundation Type edge.\n"+
			"- `application.Contract` defines the public use-case boundary.\n"+
			"- `application.Service` uses typed constructor injection for `uow.UnitOfWorkManager` and `foundation.ScopeAccess`.\n"+
			"- `Status` opens an explicit read-only application policy and returns `ErrUnavailable` for invalid or unavailable dependencies.\n"+
			"- `Configure` registers `NewService` through `module.AddSingleton`.\n\n"+
			"Run the first test from the project root:\n\n```powershell\ngo test ./...\n```\n\n"+
			"A clean generated project should compile and pass before you replace the status implementation with your own use case.\n", name, name)
		if err = writeFile(filepath.Join(target, "README.md"), []byte(readme)); err != nil {
			return err
		}
		return nil
	}

	// The no-preset path intentionally remains the existing small skeleton.
	var body strings.Builder
	importList := []string{"\"github.com/cymondez/pangolin-framework/kernel/module\""}
	var refs strings.Builder
	framework := false
	for _, dependency := range dependencies {
		if dependency == "realtime" && p.Realtime {
			importList = append(importList, `"github.com/cymondez/pangolin-framework/modules/realtime"`)
			refs.WriteString("module.Of[realtime.Module](),")
			continue
		}
		switch dependency {
		case "persistence", "authorization", "organization":
			framework = true
		case "bookstore":
			continue
		default:
			if dependency != name && namePattern.MatchString(dependency) {
				importList = append(importList, fmt.Sprintf("m_%s \"example.com/%s/modules/%s\"", dependency, p.Name, dependency))
				refs.WriteString("module.Of[m_" + dependency + ".Module](),")
			}
		}
	}
	if framework {
		importList = append(importList, fmt.Sprintf("%q", "example.com/"+p.Name+"/foundation"))
	}
	fmt.Fprintf(&body, "package %s\nimport (\n%s\n)", name, strings.Join(importList, "\n"))
	fmt.Fprintf(&body, "\ntype Module struct{}\nfunc (m *Module) ModuleMetadata() module.Metadata { return module.Metadata{Name: %q, Version: %q} }\n", name, Version)
	if framework || refs.Len() > 0 {
		body.WriteString("func (m *Module) DependsOn() []module.Ref { return []module.Ref{")
		if framework {
			body.WriteString("module.Of[foundation.Module](),")
		}
		body.WriteString(refs.String())
		body.WriteString("} }\n")
	}
	body.WriteString("func (m *Module) PreConfigure(b module.RootBuilder) error { return b.SetDefault(\"" + name + ".installed\", \"true\") }\n")
	body.WriteString("func (m *Module) Configure(b module.RootBuilder) error { return nil }\n")
	moduleBytes, err := format.Source([]byte(body.String()))
	if err != nil {
		return err
	}
	if err = writeFile(filepath.Join(target, "module.go"), moduleBytes); err != nil {
		return err
	}
	readme := "# " + name + "\n\nDefault skeleton only. Run `pangolin module add " + name + " --preset service` for a buildable vertical slice. Run `go test ./...` from the project root.\n"
	return writeFile(filepath.Join(target, "README.md"), []byte(readme))
}

var stagedRename = os.Rename

func replaceStagedRegularFile(path string, raw []byte) (restore func() error, commit func(), err error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, errors.New("regular target required")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pangolin-stage-*")
	if err != nil {
		return nil, nil, err
	}
	tmpName := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return nil, nil, err
	}
	if _, err = tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return nil, nil, err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, nil, err
	}
	if err = tmp.Close(); err != nil {
		return nil, nil, err
	}

	backup, err := os.CreateTemp(dir, ".pangolin-backup-*")
	if err != nil {
		return nil, nil, err
	}
	backupName := backup.Name()
	if err = backup.Close(); err != nil {
		return nil, nil, err
	}
	if err = os.Remove(backupName); err != nil {
		return nil, nil, err
	}
	if err = stagedRename(path, backupName); err != nil {
		return nil, nil, err
	}
	if err = stagedRename(tmpName, path); err != nil {
		rollbackErr := stagedRename(backupName, path)
		return nil, nil, errors.Join(err, rollbackErr)
	}
	cleanupTmp = false
	restore = func() error {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return stagedRename(backupName, path)
	}
	commit = func() { _ = os.Remove(backupName) }
	return restore, commit, nil
}

func acquireProjectModuleLock(dir string) (func() error, error) {
	path := filepath.Join(dir, ".pangolin-module.lock")
	lock, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errors.New("module generation already in progress; project files preserved")
		}
		return nil, err
	}
	if err = lock.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return func() error { return os.Remove(path) }, nil
}

func (c CLI) commitStagedModule(target, stage, registryPath, pangolinPath string, previousRegistry, previousProject []byte) error {
	registryRaw, err := os.ReadFile(filepath.Join(stage, "registry.go"))
	if err != nil {
		_ = os.RemoveAll(target)
		return err
	}
	projectRaw, err := os.ReadFile(filepath.Join(stage, "pangolin.json"))
	if err != nil {
		return err
	}
	currentRegistry, err := os.ReadFile(registryPath)
	if err != nil || !regularProjectFile(registryPath) || string(currentRegistry) != string(previousRegistry) {
		return errors.New("module registry changed during generation; existing files preserved")
	}
	currentProject, err := os.ReadFile(pangolinPath)
	if err != nil || !regularProjectFile(pangolinPath) || string(currentProject) != string(previousProject) {
		return errors.New("pangolin.json changed during generation; existing files preserved")
	}
	moduleStage := filepath.Join(stage, "module")
	if err := os.Rename(moduleStage, target); err != nil {
		return err
	}

	restoreRegistry, commitRegistry, err := replaceStagedRegularFile(registryPath, registryRaw)
	if err != nil {
		rollbackErr := os.RemoveAll(target)
		return errors.Join(err, rollbackErr)
	}
	_, commitProject, err := replaceStagedRegularFile(pangolinPath, projectRaw)
	if err != nil {
		var rollbackErrors []error
		if rollbackErr := restoreRegistry(); rollbackErr != nil {
			rollbackErrors = append(rollbackErrors, rollbackErr)
		}
		if rollbackErr := os.RemoveAll(target); rollbackErr != nil {
			rollbackErrors = append(rollbackErrors, rollbackErr)
		}
		rollbackErrors = append(rollbackErrors, err)
		return errors.Join(rollbackErrors...)
	}
	commitRegistry()
	commitProject()
	return nil
}

func (c CLI) runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := c.Command
	if command != nil {
		return command(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = c.Dir
	return cmd.Output()
}

func regularProjectFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func (c CLI) checkRuntimeManifest(p Project) error {
	if !regularProjectFile(filepath.Join(c.Dir, "framework", "go.mod")) {
		return errors.New("bundled runtime missing")
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, "framework", "go.mod"))
	if err != nil || !strings.Contains(string(raw), "module github.com/cymondez/pangolin-framework") {
		return errors.New("bundled runtime missing")
	}
	manifestRaw, err := os.ReadFile(filepath.Join(c.Dir, "runtime-manifest.json"))
	if err != nil {
		return errors.New("runtime manifest missing")
	}
	var manifest map[string]string
	if json.Unmarshal(manifestRaw, &manifest) != nil || len(manifest) == 0 {
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
	_ = p
	return nil
}

func snapshotDoctorModuleFiles(dir string) (map[string]string, error) {
	snapshot := map[string]string{}
	for _, name := range []string{"go.mod", "go.sum"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			snapshot[name] = ""
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", name, err)
		}
		snapshot[name] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	return snapshot, nil
}

func doctorModuleFilesChanged(before, after map[string]string) bool {
	for _, name := range []string{"go.mod", "go.sum"} {
		if before[name] != after[name] {
			return true
		}
	}
	return false
}

func (c CLI) doctorStatic(ctx context.Context) (Project, error) {
	p, err := c.project()
	if err != nil {
		return p, err
	}
	if !regularProjectFile(filepath.Join(c.Dir, "go.mod")) || !regularProjectFile(filepath.Join(c.Dir, "modules", "registry.go")) {
		fmt.Fprintln(c.Out, "[FAIL] project files: go.mod or modules/registry.go missing")
		return p, errors.New("project files check failed")
	}
	fmt.Fprintln(c.Out, "[PASS] project files")
	if err = c.checkRuntimeManifest(p); err != nil {
		fmt.Fprintf(c.Out, "[FAIL] runtime manifest: %v\n", err)
		return p, err
	}
	fmt.Fprintln(c.Out, "[PASS] runtime manifest")

	moduleBefore, err := snapshotDoctorModuleFiles(c.Dir)
	if err != nil {
		fmt.Fprintf(c.Out, "[FAIL] go.mod/go.sum snapshot: %v\n", err)
		return p, err
	}
	buildCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err = c.runCommand(buildCtx, "go", "build", "-mod=readonly", "./..."); err != nil {
		moduleAfter, snapshotErr := snapshotDoctorModuleFiles(c.Dir)
		if snapshotErr == nil && doctorModuleFilesChanged(moduleBefore, moduleAfter) {
			fmt.Fprintln(c.Out, "[FAIL] go.mod/go.sum changed during go build -mod=readonly ./...")
			return p, errors.New("go build changed go.mod/go.sum")
		}
		fmt.Fprintln(c.Out, "[FAIL] go build ./...: build failed")
		return p, errors.New("go build ./... failed")
	}
	moduleAfter, err := snapshotDoctorModuleFiles(c.Dir)
	if err != nil {
		fmt.Fprintf(c.Out, "[FAIL] go.mod/go.sum snapshot: %v\n", err)
		return p, err
	}
	if doctorModuleFilesChanged(moduleBefore, moduleAfter) {
		fmt.Fprintln(c.Out, "[FAIL] go.mod/go.sum changed during go build -mod=readonly ./...")
		return p, errors.New("go build changed go.mod/go.sum")
	}
	fmt.Fprintln(c.Out, "[PASS] go build ./...")
	return p, nil
}

func (c CLI) pingDatabase(ctx context.Context, dsn string) error {
	if c.PingDatabase != nil {
		return c.PingDatabase(ctx, dsn)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.PingContext(pingCtx)
}

func (c CLI) readMigrationLedger(ctx context.Context, dsn string) ([]MigrationLedgerEntry, error) {
	if c.ReadMigrationLedger != nil {
		return c.ReadMigrationLedger(ctx, dsn)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ledgerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ledgerCtx, "SELECT version_id,is_applied FROM pangolin.goose_db_version ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []MigrationLedgerEntry{}
	for rows.Next() {
		var entry MigrationLedgerEntry
		if err = rows.Scan(&entry.Version, &entry.Applied); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func composeString(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := record[key].(string); ok {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	for key, value := range record {
		stringValue, ok := value.(string)
		if !ok {
			continue
		}
		lowerKey := strings.ToLower(key)
		for _, expected := range keys {
			if lowerKey == strings.ToLower(expected) {
				return strings.ToLower(strings.TrimSpace(stringValue))
			}
		}
	}
	return ""
}

func composeHasRunningAPI(raw []byte) bool {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return false
	}
	check := func(record map[string]any) bool {
		service := composeString(record, "Service", "service", "Name", "name")
		state := composeString(record, "State", "state", "Status", "status")
		isAPI := service == "api" || strings.HasSuffix(service, "_api") || strings.HasSuffix(service, "-api")
		return isAPI && strings.Contains(state, "running")
	}
	var records []map[string]any
	if json.Unmarshal([]byte(text), &records) == nil {
		for _, record := range records {
			if check(record) {
				return true
			}
		}
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			break
		}
		if check(record) {
			return true
		}
	}
	return false
}

func (c CLI) doctorLive(ctx context.Context) error {
	var failures []error
	composeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	out, err := c.runCommand(composeCtx, "docker", "compose", "ps", "--format", "json")
	cancel()
	composeText := strings.ToLower(strings.TrimSpace(string(out)))
	runningCompose := composeHasRunningAPI(out)
	if err != nil || composeText == "" || !runningCompose {
		fmt.Fprintln(c.Out, "[FAIL] compose state: Docker Compose is unavailable or no running project service returned")
		failures = append(failures, errors.New("compose state failed"))
	} else {
		fmt.Fprintln(c.Out, "[PASS] compose state")
	}

	dsn := os.Getenv("PANGOLIN_DSN")
	if dsn == "" {
		fmt.Fprintln(c.Out, "[FAIL] database connection: PANGOLIN_DSN required")
		failures = append(failures, errors.New("database DSN missing"))
	} else {
		databaseCtx, databaseCancel := context.WithTimeout(ctx, 5*time.Second)
		err = c.pingDatabase(databaseCtx, dsn)
		databaseCancel()
		if err != nil {
			fmt.Fprintln(c.Out, "[FAIL] database connection: PANGOLIN_DSN connection failed")
			failures = append(failures, errors.New("database connection failed"))
		} else {
			fmt.Fprintln(c.Out, "[PASS] database connection")
		}
	}

	migrationDSN := os.Getenv("PANGOLIN_MIGRATION_DSN")
	if migrationDSN == "" {
		fmt.Fprintln(c.Out, "[FAIL] migration ledger: PANGOLIN_MIGRATION_DSN required")
		failures = append(failures, errors.New("migration DSN missing"))
	} else {
		ledgerCtx, ledgerCancel := context.WithTimeout(ctx, 5*time.Second)
		entries, readErr := c.readMigrationLedger(ledgerCtx, migrationDSN)
		ledgerCancel()
		applied := false
		for _, entry := range entries {
			if entry.Applied {
				applied = true
				break
			}
		}
		if readErr != nil || !applied {
			fmt.Fprintln(c.Out, "[FAIL] migration ledger: Goose ledger unavailable or no migration is applied")
			failures = append(failures, errors.New("migration ledger failed"))
		} else {
			fmt.Fprintln(c.Out, "[PASS] migration ledger")
		}
	}

	baseURL := strings.TrimSpace(os.Getenv("PANGOLIN_API_URL"))
	if baseURL == "" {
		port := strings.TrimSpace(os.Getenv("PANGOLIN_API_PORT"))
		if port == "" {
			port = "18281"
		}
		baseURL = "http://127.0.0.1:" + port
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	for _, path := range []string{"/livez", "/readyz"} {
		requestCtx, requestCancel := context.WithTimeout(ctx, 3*time.Second)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+path, nil)
		if err != nil {
			requestCancel()
			fmt.Fprintf(c.Out, "[FAIL] API %s: invalid API URL\n", path)
			failures = append(failures, fmt.Errorf("API %s failed", path))
			continue
		}
		response, err := client.Do(request)
		requestCancel()
		if err != nil {
			fmt.Fprintf(c.Out, "[FAIL] API %s: request timed out or could not connect\n", path)
			failures = append(failures, fmt.Errorf("API %s failed", path))
			continue
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			fmt.Fprintf(c.Out, "[FAIL] API %s: HTTP %d\n", path, response.StatusCode)
			failures = append(failures, fmt.Errorf("API %s failed", path))
			continue
		}
		fmt.Fprintf(c.Out, "[PASS] API %s\n", path)
	}

	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}
