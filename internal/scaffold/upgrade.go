package scaffold

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

type UpgradeChange struct {
	Path           string
	Classification string
	BaseDigest     string
	LocalDigest    string
	TargetDigest   string
}

type UpgradePlan struct {
	CurrentVersion string
	TargetVersion  string
	Revision       string
	Changes        []UpgradeChange
}

func upgradeDigestBytes(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func classifyThreeWay(baseExists bool, base string, localExists bool, local string, targetExists bool, target string) string {
	if baseExists && targetExists {
		if !localExists {
			if base == target {
				return "missing"
			}
			return "conflict"
		}
		if base == local && local == target {
			return "unchanged"
		}
		if base == local && base != target {
			return "update"
		}
		if local == target && base != target {
			return "keep"
		}
		return "conflict"
	}
	if baseExists && !targetExists {
		if !localExists {
			return "unchanged"
		}
		if local == base {
			return "remove"
		}
		return "conflict"
	}
	if !baseExists && targetExists {
		if !localExists {
			return "add"
		}
		if local == target {
			return "unchanged"
		}
		return "conflict"
	}
	if localExists {
		return "keep"
	}
	return "unchanged"
}

func validateUpgradeRelativePath(relative string) error {
	if relative == "" || strings.Contains(relative, "\\") || filepath.IsAbs(relative) || pathpkg.IsAbs(relative) {
		return errors.New("unsafe upgrade path")
	}
	if strings.HasSuffix(relative, "/") {
		return errors.New("unsafe upgrade path")
	}
	if pathpkg.Clean(relative) != relative {
		return errors.New("unsafe upgrade path")
	}
	if !strings.HasPrefix(relative, "framework/") {
		return errors.New("unsafe upgrade path")
	}
	for _, part := range strings.Split(relative, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("unsafe upgrade path")
		}
	}
	return nil
}

func (c CLI) upgrade(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("upgrade action required: plan, --dry-run, runtime, or verify")
	}
	switch args[0] {
	case "plan":
		f, err := c.flags("upgrade plan", args[1:], func(*flag.FlagSet) {})
		if err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unsupported upgrade plan argument")
		}
		return c.upgradePlan(false)
	case "--dry-run":
		f, err := c.flags("upgrade --dry-run", args[1:], func(*flag.FlagSet) {})
		if err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unsupported upgrade dry-run argument")
		}
		return c.upgradePlan(true)
	case "runtime":
		f, err := c.flags("upgrade runtime", args[1:], func(*flag.FlagSet) {})
		if err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unsupported upgrade runtime argument")
		}
		return c.upgradeRuntime(ctx)
	case "verify":
		f, err := c.flags("upgrade verify", args[1:], func(*flag.FlagSet) {})
		if err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unsupported upgrade verify argument")
		}
		return c.upgradeVerify(ctx)
	default:
		return errors.New("unknown upgrade action; use pangolin upgrade --help")
	}
}

func (c CLI) readUpgradeProject() (Project, error) {
	var p Project
	path := filepath.Join(c.Dir, "pangolin.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return p, errors.New("regular pangolin.json required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return p, errors.New("run inside a pangolin project")
	}
	if json.Unmarshal(raw, &p) != nil || !projectPattern.MatchString(p.Name) || p.Profile != "tenant-only" && p.Profile != "organization-enabled" || p.Modules == nil {
		return p, errors.New("invalid pangolin.json")
	}
	if err = validateModules(p.Modules); err != nil {
		return p, err
	}
	return p, nil
}

func (c CLI) readUpgradeManifest() (map[string]string, error) {
	path := filepath.Join(c.Dir, "runtime-manifest.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("runtime manifest missing")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("runtime manifest missing")
	}
	manifest := map[string]string{}
	if json.Unmarshal(raw, &manifest) != nil || len(manifest) == 0 {
		return nil, errors.New("invalid runtime manifest")
	}
	for relative, digest := range manifest {
		if err := validateUpgradeRelativePath(relative); err != nil {
			return nil, fmt.Errorf("invalid runtime manifest path: %s", relative)
		}
		if len(digest) != 64 {
			return nil, fmt.Errorf("invalid runtime manifest checksum: %s", relative)
		}
		for _, char := range digest {
			if char < '0' || char > '9' {
				if char < 'a' || char > 'f' {
					return nil, fmt.Errorf("invalid runtime manifest checksum: %s", relative)
				}
			}
		}
	}
	return manifest, nil
}

func baselineUpgradeCapabilities(manifest map[string]string) (profile string, realtime bool) {
	profile = "tenant-only"
	for relative := range manifest {
		if strings.HasPrefix(relative, "framework/modules/organization/") {
			profile = "organization-enabled"
		}
		if strings.HasPrefix(relative, "framework/modules/realtime/") {
			realtime = true
		}
	}
	return profile, realtime
}

func validateUpgradeBaselineCapabilities(p Project, manifest map[string]string) error {
	baselineProfile, baselineRealtime := baselineUpgradeCapabilities(manifest)
	if p.Profile != baselineProfile {
		return fmt.Errorf("upgrade refused: profile switch is not an in-place upgrade (baseline %s, project %s); first follow the migration guide docs/site/content/data/migrations.md", baselineProfile, p.Profile)
	}
	if p.Realtime != baselineRealtime {
		return fmt.Errorf("upgrade refused: realtime capability switch is not an in-place upgrade (baseline %t, project %t); first follow the migration guide docs/site/content/reference/upgrading.md", baselineRealtime, p.Realtime)
	}
	return nil
}

func (c CLI) readLocalFramework() (map[string][]byte, error) {
	root := filepath.Join(c.Dir, "framework")
	info, err := os.Lstat(root)
	if err != nil {
		return nil, errors.New("bundled runtime missing")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("unsafe bundled runtime directory")
	}
	files := map[string][]byte{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("unsafe bundled runtime file")
		}
		rel, err := filepath.Rel(c.Dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := validateUpgradeRelativePath(rel); err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func embeddedProjectFiles(name, profile string, realtime bool) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(assets, "assets", func(assetPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(assetPath, "assets/")
		if relative == "common/runtime-manifest.json.tmpl" {
			return nil
		}
		parts := strings.SplitN(relative, "/", 2)
		if len(parts) != 2 {
			return nil
		}
		group, destination := parts[0], strings.TrimSuffix(parts[1], ".tmpl")
		matchesGroup := group == "common" || group == "runtime" || group == profile
		matchesGroup = matchesGroup || realtime && group == "realtime"
		matchesGroup = matchesGroup || !realtime && group == "no-realtime"
		if !matchesGroup {
			return nil
		}
		if group == "runtime" {
			if profile == "tenant-only" && strings.HasPrefix(destination, "modules/organization/") {
				return nil
			}
			if !realtime && strings.HasPrefix(destination, "modules/realtime/") {
				return nil
			}
			destination = "framework/" + destination
		}
		raw, err := fs.ReadFile(assets, assetPath)
		if err != nil {
			return err
		}
		if group != "runtime" {
			raw = []byte(strings.ReplaceAll(string(raw), "{{MODULE}}", "example.com/"+name))
		}
		files[destination] = raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func (c CLI) targetRuntimeAssets(p Project) (map[string][]byte, error) {
	projectFiles, err := embeddedProjectFiles(p.Name, p.Profile, p.Realtime)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for destination, raw := range projectFiles {
		if strings.HasPrefix(destination, "framework/") {
			if err := validateUpgradeRelativePath(destination); err != nil {
				return nil, err
			}
			files[destination] = raw
		}
	}
	return files, nil
}

func (c CLI) targetUpgradeManifest(p Project) (map[string][]byte, map[string]string, error) {
	files, err := c.targetRuntimeAssets(p)
	if err != nil {
		return nil, nil, err
	}
	manifest := map[string]string{}
	for relative, raw := range files {
		manifest[relative] = upgradeDigestBytes(raw)
	}
	return files, manifest, nil
}

func targetRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

func (c CLI) currentFrameworkVersion(p Project) string {
	raw, err := os.ReadFile(filepath.Join(c.Dir, "framework", "VERSION"))
	if err == nil {
		if version := strings.TrimSpace(string(raw)); version != "" {
			return version
		}
	}
	return p.Version
}

func (c CLI) buildUpgradePlan() (UpgradePlan, error) {
	p, err := c.readUpgradeProject()
	if err != nil {
		return UpgradePlan{}, err
	}
	base, err := c.readUpgradeManifest()
	if err != nil {
		return UpgradePlan{}, err
	}
	if err := validateUpgradeBaselineCapabilities(p, base); err != nil {
		return UpgradePlan{}, err
	}
	local, err := c.readLocalFramework()
	if err != nil {
		return UpgradePlan{}, err
	}
	targetFiles, targetManifest, err := c.targetUpgradeManifest(p)
	if err != nil {
		return UpgradePlan{}, err
	}

	paths := map[string]bool{}
	for relative := range base {
		if err := validateUpgradeRelativePath(relative); err != nil {
			return UpgradePlan{}, err
		}
		paths[relative] = true
	}
	for relative := range local {
		if err := validateUpgradeRelativePath(relative); err != nil {
			return UpgradePlan{}, err
		}
		paths[relative] = true
	}
	for relative := range targetManifest {
		if err := validateUpgradeRelativePath(relative); err != nil {
			return UpgradePlan{}, err
		}
		paths[relative] = true
	}

	ordered := make([]string, 0, len(paths))
	for relative := range paths {
		ordered = append(ordered, relative)
	}
	sort.Strings(ordered)
	plan := UpgradePlan{
		CurrentVersion: c.currentFrameworkVersion(p),
		TargetVersion:  Version,
		Revision:       targetRevision(),
		Changes:        make([]UpgradeChange, 0, len(ordered)),
	}
	for _, relative := range ordered {
		baseDigest, baseExists := base[relative]
		localRaw, localExists := local[relative]
		localDigest := ""
		if localExists {
			localDigest = upgradeDigestBytes(localRaw)
		}
		targetRaw, targetExists := targetFiles[relative]
		targetDigest := ""
		if targetExists {
			targetDigest = upgradeDigestBytes(targetRaw)
		}
		classification := classifyThreeWay(baseExists, baseDigest, localExists, localDigest, targetExists, targetDigest)
		plan.Changes = append(plan.Changes, UpgradeChange{
			Path:           relative,
			Classification: classification,
			BaseDigest:     baseDigest,
			LocalDigest:    localDigest,
			TargetDigest:   targetDigest,
		})
	}
	return plan, nil
}

func isUpgradeChanged(classification string) bool {
	switch classification {
	case "update", "add", "remove", "conflict", "missing":
		return true
	default:
		return false
	}
}

func isPublicGoChange(relative string) bool {
	if !strings.HasPrefix(relative, "framework/") || !strings.HasSuffix(relative, ".go") || strings.HasSuffix(relative, "_test.go") {
		return false
	}
	base := pathpkg.Base(relative)
	return !strings.HasPrefix(base, "_")
}

func isMigrationChange(relative string) bool {
	lower := strings.ToLower(relative)
	return strings.Contains(lower, "/migrations/") || strings.Contains(lower, "/migration/") || strings.HasSuffix(lower, ".sql")
}

func isSettingsConfigChange(relative string) bool {
	lower := strings.ToLower(relative)
	if strings.Contains(lower, "/platform/settings/") || strings.Contains(lower, "/platform/features/") {
		return true
	}
	base := pathpkg.Base(lower)
	return base == "config.go" || base == "configuration.go" || base == "go.mod" || strings.Contains(base, "config")
}

func (c CLI) printUpgradePlan(plan UpgradePlan) {
	fmt.Fprintf(c.Out, "current version: %s\n", plan.CurrentVersion)
	fmt.Fprintf(c.Out, "target version: %s\n", plan.TargetVersion)
	if plan.Revision != "" {
		fmt.Fprintf(c.Out, "target build revision: %s\n", plan.Revision)
	}
	conflicts, missing := 0, 0
	apiChanged, migrationsChanged, settingsChanged := 0, 0, 0
	for _, change := range plan.Changes {
		fmt.Fprintf(c.Out, "%s %s", change.Path, change.Classification)
		if change.Classification == "conflict" {
			fmt.Fprintf(c.Out, " base=%s local=%s target=%s", digestOrDash(change.BaseDigest), digestOrDash(change.LocalDigest), digestOrDash(change.TargetDigest))
		}
		fmt.Fprintln(c.Out)
		if change.Classification == "conflict" {
			conflicts++
		}
		if change.Classification == "missing" {
			missing++
		}
		if isUpgradeChanged(change.Classification) {
			if isPublicGoChange(change.Path) {
				apiChanged++
			}
			if isMigrationChange(change.Path) {
				migrationsChanged++
			}
			if isSettingsConfigChange(change.Path) {
				settingsChanged++
			}
		}
	}
	fmt.Fprintf(c.Out, "api inventory: %d public Go file(s) changed\n", apiChanged)
	fmt.Fprintf(c.Out, "migrations: %d file(s) changed\n", migrationsChanged)
	fmt.Fprintf(c.Out, "settings/config: %d file(s) changed\n", settingsChanged)
	if conflicts > 0 {
		fmt.Fprintf(c.Out, "conflicts: %d\n", conflicts)
	}
	if missing > 0 {
		fmt.Fprintf(c.Out, "missing: %d\n", missing)
	}
}

func digestOrDash(digest string) string {
	if digest == "" {
		return "-"
	}
	return digest
}

func (c CLI) upgradePlan(_ bool) error {
	plan, err := c.buildUpgradePlan()
	if err != nil {
		return err
	}
	c.printUpgradePlan(plan)
	for _, change := range plan.Changes {
		if change.Classification == "conflict" || change.Classification == "missing" {
			return errors.New("upgrade plan blocked: conflict or missing files")
		}
	}
	return nil
}

func copyUpgradeDirectory(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("unsafe copy source")
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("unsafe copy file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(dest, raw)
	})
}

func copyUpgradeFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("unsafe backup file")
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeFile(destination, raw)
}

func timestampBackupName(root string) (string, string, error) {
	base := time.Now().UTC().Format("20060102T150405Z")
	for suffix := 0; suffix < 1000; suffix++ {
		name := base
		if suffix > 0 {
			name = fmt.Sprintf("%s-%03d", base, suffix)
		}
		absolute := filepath.Join(root, ".pangolin-backup", name)
		if _, err := os.Lstat(absolute); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(absolute, 0755); err != nil {
				return "", "", err
			}
			return name, absolute, nil
		} else if err != nil {
			return "", "", err
		}
	}
	return "", "", errors.New("timestamped backup name unavailable")
}

func removeUpgradePath(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return nil
}

func restoreUpgradeBackup(c CLI, stage, backup string) error {
	var rollbackErrors []error
	frameworkOld := filepath.Join(stage, "old-framework")
	manifestOld := filepath.Join(stage, "old-manifest")
	pangolinOld := filepath.Join(stage, "old-pangolin")

	frameworkTarget := filepath.Join(c.Dir, "framework")
	if err := removeUpgradePath(frameworkTarget); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("remove framework during rollback: %w", err))
	}
	if _, err := os.Lstat(frameworkOld); err == nil {
		if err := os.Rename(frameworkOld, frameworkTarget); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore framework rename: %w", err))
			if copyErr := copyUpgradeDirectory(filepath.Join(backup, "framework"), frameworkTarget); copyErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore framework backup: %w", copyErr))
			}
		}
	} else if copyErr := copyUpgradeDirectory(filepath.Join(backup, "framework"), frameworkTarget); copyErr != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore framework backup: %w", copyErr))
	}

	manifestTarget := filepath.Join(c.Dir, "runtime-manifest.json")
	if err := os.Remove(manifestTarget); err != nil && !errors.Is(err, os.ErrNotExist) {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("remove manifest during rollback: %w", err))
	}
	if _, err := os.Lstat(manifestOld); err == nil {
		if err := os.Rename(manifestOld, manifestTarget); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore manifest rename: %w", err))
			if copyErr := copyUpgradeFile(filepath.Join(backup, "runtime-manifest.json"), manifestTarget); copyErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore manifest backup: %w", copyErr))
			}
		}
	} else if copyErr := copyUpgradeFile(filepath.Join(backup, "runtime-manifest.json"), manifestTarget); copyErr != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore manifest backup: %w", copyErr))
	}

	pangolinTarget := filepath.Join(c.Dir, "pangolin.json")
	if err := os.Remove(pangolinTarget); err != nil && !errors.Is(err, os.ErrNotExist) {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("remove pangolin.json during rollback: %w", err))
	}
	if _, err := os.Lstat(pangolinOld); err == nil {
		if err := os.Rename(pangolinOld, pangolinTarget); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore pangolin.json rename: %w", err))
			if copyErr := copyUpgradeFile(filepath.Join(backup, "pangolin.json"), pangolinTarget); copyErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore pangolin.json backup: %w", copyErr))
			}
		}
	} else if copyErr := copyUpgradeFile(filepath.Join(backup, "pangolin.json"), pangolinTarget); copyErr != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("restore pangolin.json backup: %w", copyErr))
	}
	return errors.Join(rollbackErrors...)
}

func equalUpgradePlan(a, b UpgradePlan) bool {
	if a.CurrentVersion != b.CurrentVersion || a.TargetVersion != b.TargetVersion || a.Revision != b.Revision || len(a.Changes) != len(b.Changes) {
		return false
	}
	for i := range a.Changes {
		ac, bc := a.Changes[i], b.Changes[i]
		if ac != bc {
			return false
		}
	}
	return true
}

func (c CLI) upgradeRuntime(ctx context.Context) (retErr error) {
	if ctx == nil {
		return errors.New("context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := acquireProjectModuleLock(c.Dir)
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := unlock(); unlockErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("release upgrade project lock: %w", unlockErr))
		}
	}()
	plan, err := c.buildUpgradePlan()
	if err != nil {
		return err
	}
	c.printUpgradePlan(plan)
	for _, change := range plan.Changes {
		if change.Classification == "conflict" || change.Classification == "missing" {
			return errors.New("upgrade runtime refused: conflict or missing files")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	p, err := c.readUpgradeProject()
	if err != nil {
		return err
	}
	targetFiles, targetManifest, err := c.targetUpgradeManifest(p)
	if err != nil {
		return err
	}
	local, err := c.readLocalFramework()
	if err != nil {
		return err
	}

	stage, err := os.MkdirTemp(c.Dir, ".pangolin-upgrade-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	backupName, backup, err := timestampBackupName(c.Dir)
	if err != nil {
		return err
	}
	if err := copyUpgradeDirectory(filepath.Join(c.Dir, "framework"), filepath.Join(backup, "framework")); err != nil {
		return fmt.Errorf("create upgrade backup: %w", err)
	}
	if err := copyUpgradeFile(filepath.Join(c.Dir, "runtime-manifest.json"), filepath.Join(backup, "runtime-manifest.json")); err != nil {
		return fmt.Errorf("create upgrade backup: %w", err)
	}
	if err := copyUpgradeFile(filepath.Join(c.Dir, "pangolin.json"), filepath.Join(backup, "pangolin.json")); err != nil {
		return fmt.Errorf("create upgrade backup: %w", err)
	}

	stagedFramework := filepath.Join(stage, "framework")
	if err := os.MkdirAll(stagedFramework, 0755); err != nil {
		return fmt.Errorf("stage upgrade: %w", err)
	}
	for _, relative := range sortedUpgradeKeys(targetFiles) {
		raw := targetFiles[relative]
		stageRelative := strings.TrimPrefix(relative, "framework/")
		stagedPath := filepath.Join(stagedFramework, filepath.FromSlash(stageRelative))
		if err := writeFile(stagedPath, raw); err != nil {
			return fmt.Errorf("stage upgrade: %w", err)
		}
	}
	for _, change := range plan.Changes {
		if change.Classification == "keep" {
			raw, exists := local[change.Path]
			if !exists {
				continue
			}
			stageRelative := strings.TrimPrefix(change.Path, "framework/")
			if err := writeFile(filepath.Join(stagedFramework, filepath.FromSlash(stageRelative)), raw); err != nil {
				return fmt.Errorf("stage upgrade keep file: %w", err)
			}
		}
	}

	stagedManifest := filepath.Join(stage, "runtime-manifest.json")
	if err := writeFile(stagedManifest, jsonBytes(sortedStringMap(targetManifest))); err != nil {
		return fmt.Errorf("stage upgrade manifest: %w", err)
	}
	stagedPangolin := filepath.Join(stage, "pangolin.json")
	p.Version = Version
	if err := writeFile(stagedPangolin, jsonBytes(p)); err != nil {
		return fmt.Errorf("stage upgrade project: %w", err)
	}

	for relative, expected := range targetManifest {
		rel, err := filepath.Rel(filepath.Join(c.Dir, "framework"), filepath.Join(c.Dir, filepath.FromSlash(relative)))
		if err != nil {
			return fmt.Errorf("verify staged upgrade: %w", err)
		}
		raw, err := os.ReadFile(filepath.Join(stagedFramework, rel))
		if err != nil || upgradeDigestBytes(raw) != expected {
			return fmt.Errorf("verify staged upgrade: checksum mismatch: %s", relative)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	latestPlan, err := c.buildUpgradePlan()
	if err != nil {
		return fmt.Errorf("revalidate upgrade before apply: %w", err)
	}
	if !equalUpgradePlan(plan, latestPlan) {
		return errors.New("upgrade runtime refused: project changed while planning; rerun upgrade plan")
	}

	frameworkOld := filepath.Join(stage, "old-framework")
	manifestOld := filepath.Join(stage, "old-manifest")
	pangolinOld := filepath.Join(stage, "old-pangolin")
	frameworkTarget := filepath.Join(c.Dir, "framework")
	manifestTarget := filepath.Join(c.Dir, "runtime-manifest.json")
	pangolinTarget := filepath.Join(c.Dir, "pangolin.json")

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(frameworkTarget, frameworkOld); err != nil {
		rollbackErr := restoreUpgradeBackup(c, stage, backup)
		primary := fmt.Errorf("apply upgrade framework: %w", err)
		if rollbackErr != nil {
			return errors.Join(primary, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return primary
	}
	if err := os.Rename(manifestTarget, manifestOld); err != nil {
		rollbackErr := restoreUpgradeBackup(c, stage, backup)
		primary := fmt.Errorf("apply upgrade manifest: %w", err)
		if rollbackErr != nil {
			return errors.Join(primary, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return primary
	}
	if err := os.Rename(pangolinTarget, pangolinOld); err != nil {
		rollbackErr := restoreUpgradeBackup(c, stage, backup)
		primary := fmt.Errorf("apply upgrade project: %w", err)
		if rollbackErr != nil {
			return errors.Join(primary, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return primary
	}
	if err := ctx.Err(); err != nil {
		rollbackErr := restoreUpgradeBackup(c, stage, backup)
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return err
	}
	if err := os.Rename(stagedFramework, frameworkTarget); err != nil {
		rollbackErr := restoreUpgradeBackup(c, stage, backup)
		primary := fmt.Errorf("apply staged framework: %w", err)
		if rollbackErr != nil {
			return errors.Join(primary, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return primary
	}
	if err := os.Rename(stagedManifest, manifestTarget); err != nil {
		rollbackErr := restoreUpgradeBackup(c, stage, backup)
		primary := fmt.Errorf("apply staged manifest: %w", err)
		if rollbackErr != nil {
			return errors.Join(primary, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return primary
	}
	if err := os.Rename(stagedPangolin, pangolinTarget); err != nil {
		rollbackErr := restoreUpgradeBackup(c, stage, backup)
		primary := fmt.Errorf("apply staged project: %w", err)
		if rollbackErr != nil {
			return errors.Join(primary, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return primary
	}

	for relative, expected := range targetManifest {
		raw, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(relative)))
		if err != nil || upgradeDigestBytes(raw) != expected {
			rollbackErr := restoreUpgradeBackup(c, stage, backup)
			primary := fmt.Errorf("verify installed upgrade: checksum mismatch: %s", relative)
			if rollbackErr != nil {
				return errors.Join(primary, fmt.Errorf("rollback: %w", rollbackErr))
			}
			return primary
		}
	}

	fmt.Fprintf(c.Out, "upgraded runtime to %s\n", Version)
	fmt.Fprintf(c.Out, "backup: .pangolin-backup/%s\n", backupName)
	fmt.Fprintf(c.Out, "rollback: Remove-Item -Recurse -Force .\\framework; Copy-Item -Recurse .\\.pangolin-backup\\%s\\framework .\\framework; Copy-Item .\\.pangolin-backup\\%s\\runtime-manifest.json .\\runtime-manifest.json; Copy-Item .\\.pangolin-backup\\%s\\pangolin.json .\\pangolin.json\n", backupName, backupName, backupName)
	return nil
}

func sortedUpgradeKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringMap(values map[string]string) map[string]string {
	return values
}

func (c CLI) upgradeVerify(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := c.readUpgradeProject()
	if err != nil {
		return err
	}
	base, err := c.readUpgradeManifest()
	if err != nil {
		return err
	}
	if err := validateUpgradeBaselineCapabilities(p, base); err != nil {
		return err
	}
	if p.Version != Version {
		return fmt.Errorf("upgrade verify: stale pangolin.json version %s; target %s", p.Version, Version)
	}
	local, err := c.readLocalFramework()
	if err != nil {
		return err
	}
	files, targetManifest, err := c.targetUpgradeManifest(p)
	if err != nil {
		return err
	}
	if len(base) != len(targetManifest) {
		return errors.New("upgrade verify: runtime manifest is not the target manifest")
	}
	for relative, digest := range targetManifest {
		if base[relative] != digest {
			return errors.New("upgrade verify: runtime manifest checksum mismatch")
		}
	}
	for relative, raw := range files {
		localRaw, exists := local[relative]
		if !exists || upgradeDigestBytes(localRaw) != upgradeDigestBytes(raw) {
			return fmt.Errorf("upgrade verify: local/target mismatch: %s", relative)
		}
	}
	for relative, digest := range base {
		raw, exists := local[relative]
		if !exists || upgradeDigestBytes(raw) != digest {
			return fmt.Errorf("upgrade verify: base current mismatch: %s", relative)
		}
	}
	if !regularProjectFile(filepath.Join(c.Dir, "go.mod")) || !regularProjectFile(filepath.Join(c.Dir, "modules", "registry.go")) {
		fmt.Fprintln(c.Out, "[FAIL] project files: go.mod or modules/registry.go missing")
		return errors.New("project files check failed")
	}
	fmt.Fprintln(c.Out, "[PASS] project files")
	fmt.Fprintln(c.Out, "[PASS] runtime manifest")

	buildCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := c.runCommand(buildCtx, "go", "build", "-mod=readonly", "./..."); err != nil {
		fmt.Fprintln(c.Out, "[FAIL] go build ./...: build failed")
		return errors.New("go build ./... failed; run: go build -mod=readonly ./...")
	}
	fmt.Fprintln(c.Out, "[PASS] go build ./...")

	testCtx, cancelTest := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelTest()
	if _, err := c.runCommand(testCtx, "go", "test", "-mod=readonly", "./..."); err != nil {
		fmt.Fprintln(c.Out, "[FAIL] go test ./...: test failed")
		return errors.New("go test ./... failed; run: go test -mod=readonly ./...")
	}
	fmt.Fprintln(c.Out, "[PASS] go test ./...")

	vetCtx, cancelVet := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelVet()
	if _, err := c.runCommand(vetCtx, "go", "vet", "-mod=readonly", "./..."); err != nil {
		fmt.Fprintln(c.Out, "[FAIL] go vet ./...: vet failed")
		return errors.New("go vet ./... failed; run: go vet -mod=readonly ./...")
	}
	fmt.Fprintln(c.Out, "[PASS] go vet ./...")
	fmt.Fprintln(c.Out, "[PASS] upgrade verify")
	return nil
}
