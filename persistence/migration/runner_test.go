package migration

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMigrationPlan(t *testing.T) {
	source := fstest.MapFS{
		"core/1_core.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")},
		"org/2_org.sql":   {Data: []byte("-- +goose Up\nSELECT 2;\n")},
	}
	plan, err := collect(source, []ModuleMigrations{{Module: "org", DependsOn: []string{"core"}, Directory: "org"}, {Module: "core", Directory: "core"}})
	if err != nil || len(plan.files) != 2 {
		t.Fatal(err)
	}
	if err = fstest.TestFS(plan, "1_core.sql", "2_org.sql"); err != nil {
		t.Fatal(err)
	}
	for _, modules := range [][]ModuleMigrations{
		{{Module: "core", Directory: "core"}, {Module: "core", Directory: "core"}},
		{{Module: "org", Directory: "org", DependsOn: []string{"missing"}}},
		{{Module: "org", Directory: "org", DependsOn: []string{"core"}}, {Module: "core", Directory: "core", DependsOn: []string{"org"}}},
		{{Module: "", Directory: "core"}},
		{{Module: "core", Directory: "missing"}},
	} {
		if _, err := collect(source, modules); err == nil {
			t.Fatal("invalid migration plan accepted", modules)
		}
	}
}

func TestMigrationSourcesAndRejections(t *testing.T) {
	source := fstest.MapFS{"core/2_core.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}, "org/1_org.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}, "empty/readme.md": {Data: []byte("no migrations")}, "bad/name.sql": {}, "duplicate/2_other.sql": {}}
	for _, modules := range [][]ModuleMigrations{
		nil, {{Module: "core"}}, {{Directory: "core"}}, {{Module: "empty", Directory: "empty"}}, {{Module: "bad", Directory: "bad"}}, {{Module: "core", Directory: "core"}, {Module: "other", Directory: "duplicate"}}, {{Module: "core", Directory: "core"}, {Module: "org", Directory: "org", DependsOn: []string{"core"}}},
	} {
		if _, err := collect(source, modules); err == nil {
			t.Fatal(modules)
		}
	}
	source["zero/0_zero.sql"] = &fstest.MapFile{}
	if _, err := collect(source, []ModuleMigrations{{Module: "zero", Directory: "zero"}}); err == nil {
		t.Fatal("zero version")
	}
	mounts := map[string]fs.FS{"one": fstest.MapFS{"1_first.sql": {Data: []byte("one")}}, "two": fstest.MapFS{"2_second.sql": {Data: []byte("two")}}}
	joined := JoinFS(mounts)
	delete(mounts, "one")
	if err := fstest.TestFS(joined, "one/1_first.sql", "two/2_second.sql"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../one", "absent", "absent/file"} {
		if _, err := joined.Open(name); err == nil {
			t.Fatal(name)
		}
	}
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "core"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "core", "1_core.sql"), []byte("-- +goose Up\nSELECT 1;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := collect(Filesystem(directory), []ModuleMigrations{{Module: "core", Directory: "core"}}); err != nil {
		t.Fatal(err)
	}
	db := new(sql.DB)
	for _, config := range []struct {
		db       *sql.DB
		source   fs.FS
		min, max int64
	}{{nil, source, 0, 1}, {db, nil, 0, 1}, {db, source, -1, 1}, {db, source, 2, 1}} {
		if _, err := NewPostgres(config.db, config.source, config.min, config.max); err == nil {
			t.Fatal("invalid runner configuration")
		}
	}
	runner, _ := NewPostgres(db, source, 0, 1)
	if err := runner.Apply(nil, nil); err == nil {
		t.Fatal("nil apply context")
	}
	if err := runner.Apply(context.Background(), nil); err == nil {
		t.Fatal("empty apply modules")
	}
	if err := runner.CheckCompatibility(nil); !errors.Is(err, ErrIncompatible) {
		t.Fatal(err)
	}
	for _, locker := range []PostgresLocker{{}, {DB: db}} {
		if _, err := locker.Acquire(nil, LockKey); err == nil {
			t.Fatal("invalid acquire context")
		}
	}
	if _, err := (PostgresLocker{}).Acquire(context.Background(), LockKey); err == nil {
		t.Fatal("nil database")
	}
}
