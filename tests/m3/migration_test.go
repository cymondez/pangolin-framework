//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure"
	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/jackc/pgx/v5"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func isolatedDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	admin := pool(t, "PANGOLIN_M1_ADMIN_DSN")
	name := "m3_" + strings.ToLower(rand.Text())
	name = strings.ReplaceAll(name, "-", "_")
	if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(os.Getenv("PANGOLIN_M1_ADMIN_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	dsn.Path = "/" + name
	db, err := sql.Open("pgx", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	return db, dsn.String()
}
func TestA59Deployment(t *testing.T) {
	t.Run("empty-database", func(t *testing.T) {
		db, _ := isolatedDB(t)
		source := fstest.MapFS{"core/1_core.sql": {Data: []byte("-- +goose Up\nCREATE TABLE pangolin.applied(id integer PRIMARY KEY);INSERT INTO pangolin.applied VALUES(1);\n")}}
		runner, err := migration.NewPostgres(db, source, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err = runner.Apply(context.Background(), []migration.ModuleMigrations{{Module: "core", Directory: "core"}}); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = db.QueryRow("SELECT count(*) FROM pangolin.applied").Scan(&count); err != nil || count != 1 {
			t.Fatal(err, count)
		}
		if err = runner.CheckCompatibility(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("forward-upgrade", func(t *testing.T) {
		db, _ := isolatedDB(t)
		source := fstest.MapFS{"core/1_core.sql": {Data: []byte("-- +goose Up\nCREATE TABLE pangolin.applied(id integer PRIMARY KEY);INSERT INTO pangolin.applied VALUES(1);\n")}}
		runner, _ := migration.NewPostgres(db, source, 1, 1)
		modules := []migration.ModuleMigrations{{Module: "core", Directory: "core"}}
		if err := runner.Apply(context.Background(), modules); err != nil {
			t.Fatal(err)
		}
		source["core/2_next.sql"] = &fstest.MapFile{Data: []byte("-- +goose Up\nINSERT INTO pangolin.applied VALUES(2);\n")}
		next, _ := migration.NewPostgres(db, source, 2, 2)
		if err := next.Apply(context.Background(), modules); err != nil {
			t.Fatal(err)
		}
		if err := next.CheckCompatibility(context.Background()); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow("SELECT count(*) FROM pangolin.applied").Scan(&count); err != nil || count != 2 {
			t.Fatal(err, count)
		}
	})
	t.Run("api-does-not-migrate", func(t *testing.T) {
		db, _ := isolatedDB(t)
		runner, _ := migration.NewPostgres(db, fstest.MapFS{}, 1, 1)
		if err := runner.CheckCompatibility(context.Background()); !errors.Is(err, migration.ErrIncompatible) {
			t.Fatal(err)
		}
		var exists bool
		if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname='pangolin')").Scan(&exists); err != nil || exists {
			t.Fatal("API compatibility check created schema", err)
		}
	})
	t.Run("concurrent-deploy", func(t *testing.T) {
		db, _ := isolatedDB(t)
		source := fstest.MapFS{"core/1_core.sql": {Data: []byte("-- +goose Up\nCREATE TABLE pangolin.applied(id integer PRIMARY KEY);\nINSERT INTO pangolin.applied VALUES(1);\n")}}
		runner, err := migration.NewPostgres(db, source, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(runner.CheckCompatibility(context.Background()), migration.ErrIncompatible) {
			t.Fatal("empty schema compatible")
		}
		result := make(chan error, 2)
		var barrier sync.WaitGroup
		barrier.Add(2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			go func() {
				barrier.Done()
				<-start
				result <- runner.Apply(context.Background(), []migration.ModuleMigrations{{Module: "core", Directory: "core"}})
			}()
		}
		barrier.Wait()
		close(start)
		for i := 0; i < 2; i++ {
			if err = <-result; err != nil {
				t.Fatal(err)
			}
		}
		var n int
		if err = db.QueryRow("SELECT count(*) FROM pangolin.applied").Scan(&n); err != nil || n != 1 {
			t.Fatal(err, n)
		}
		if err = runner.CheckCompatibility(context.Background()); err != nil {
			t.Fatal(err)
		}
		incompatible, _ := migration.NewPostgres(db, source, 2, 2)
		if !errors.Is(incompatible.CheckCompatibility(context.Background()), migration.ErrIncompatible) {
			t.Fatal("incompatible accepted")
		}
		if err = db.QueryRow("SELECT count(*) FROM pangolin.applied").Scan(&n); err != nil || n != 1 {
			t.Fatal("compatibility mutated schema", err)
		}
	})
	t.Run("incompatible-schema", func(t *testing.T) {
		db, _ := isolatedDB(t)
		source := fstest.MapFS{"core/1_core.sql": {Data: []byte("-- +goose Up\nCREATE TABLE pangolin.applied(id integer PRIMARY KEY);\n")}}
		runner, _ := migration.NewPostgres(db, source, 1, 1)
		if err := runner.Apply(context.Background(), []migration.ModuleMigrations{{Module: "core", Directory: "core"}}); err != nil {
			t.Fatal(err)
		}
		incompatible, _ := migration.NewPostgres(db, source, 2, 2)
		if err := incompatible.CheckCompatibility(context.Background()); !errors.Is(err, migration.ErrIncompatible) {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO pangolin.goose_db_version(version_id,is_applied) VALUES(3,true)`); err != nil {
			t.Fatal(err)
		}
		if err := runner.CheckCompatibility(context.Background()); !errors.Is(err, migration.ErrIncompatible) {
			t.Fatal("newer schema accepted", err)
		}
	})
}
func TestA93ForwardUpgrade(t *testing.T) {
	for _, scenario := range []string{"tenant-owner-backfill", "interrupted-backfill", "interrupted-policy-switch", "forward-resume", "no-permissive-policy-union"} {
		t.Run(scenario, func(t *testing.T) {
			fault := "none"
			if scenario == "interrupted-backfill" || scenario == "forward-resume" {
				fault = "backfill"
			}
			if scenario == "interrupted-policy-switch" {
				fault = "policy"
			}
			db, _ := isolatedDB(t)
			_, err := db.Exec(`CREATE SCHEMA pangolin;CREATE TABLE pangolin.books(tenant_id text NOT NULL,id text NOT NULL,title text,PRIMARY KEY(tenant_id,id));INSERT INTO pangolin.books VALUES('a','same','a'),('b','same','b');ALTER TABLE pangolin.books ENABLE ROW LEVEL SECURITY;ALTER TABLE pangolin.books FORCE ROW LEVEL SECURITY;CREATE POLICY old_tenant ON pangolin.books USING(true);CREATE POLICY extra_permissive ON pangolin.books USING(true);`)
			if err != nil {
				t.Fatal(err)
			}
			upgrade, err := fs.ReadFile(infrastructure.ScopeUpgrade, "migrations/3100003_scope_upgrade.sql")
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := fs.ReadFile(migration.ScopeMigrations, "migrations/3100001_scope_catalog.sql")
			if err != nil {
				t.Fatal(err)
			}
			source := fstest.MapFS{"scope/3100001_scope.sql": {Data: catalog}, "books/3100003_books.sql": {Data: upgrade}}
			modules := []migration.ModuleMigrations{{Module: "scope", Directory: "scope"}, {Module: "books", Directory: "books", DependsOn: []string{"scope"}}}
			if fault != "none" {
				bad := string(upgrade)
				needle := "ALTER TABLE pangolin.books ALTER COLUMN owner_scope_kind SET NOT NULL;"
				if fault == "policy" {
					needle = "CREATE POLICY exact_owner ON pangolin.books"
				}
				bad = strings.Replace(bad, needle, "SELECT 1/0;\n"+needle, 1)
				source["books/3100003_books.sql"] = &fstest.MapFile{Data: []byte(bad)}
				failed, _ := migration.NewPostgres(db, source, 3100003, 3100003)
				if err = failed.Apply(context.Background(), modules); err == nil {
					t.Fatal("injected migration fault passed")
				}
				var columns int
				if err = db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema='pangolin' AND table_name='books' AND column_name='owner_scope_id'`).Scan(&columns); err != nil || columns != 0 {
					t.Fatal("partial migration visible", err, columns)
				}
				source["books/3100003_books.sql"] = &fstest.MapFile{Data: upgrade}
			}
			runner, _ := migration.NewPostgres(db, source, 3100003, 3100003)
			if err = runner.Apply(context.Background(), modules); err != nil {
				t.Fatal(err)
			}
			var valid, policies int
			if err = db.QueryRow(`SELECT (SELECT count(*) FROM pangolin.scope_books WHERE owner_scope_kind='tenant' AND owner_scope_id=tenant_id),(SELECT count(*) FROM pg_policies WHERE schemaname='pangolin' AND tablename='scope_books')`).Scan(&valid, &policies); err != nil || valid != 2 || policies != 1 {
				t.Fatal(err, valid, policies)
			}
		})
	}
}

func TestMigrationLockLifecycle(t *testing.T) {
	db, _ := isolatedDB(t)
	t.Run("same-session-and-cancelled-cleanup", func(t *testing.T) {
		first, err := (migration.PostgresLocker{DB: db}).Acquire(context.Background(), migration.LockKey)
		if err != nil {
			t.Fatal(err)
		}
		waiting, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		if _, err = (migration.PostgresLocker{DB: db}).Acquire(waiting, migration.LockKey); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		cancel()
		cancelled, stop := context.WithCancel(context.Background())
		stop()
		if err = first.Release(cancelled); err != nil {
			t.Fatal(err)
		}
		if err = first.Release(nil); err != nil {
			t.Fatal("double release", err)
		}
		next, err := (migration.PostgresLocker{DB: db}).Acquire(context.Background(), migration.LockKey)
		if err != nil {
			t.Fatal(err)
		}
		if err = next.Release(nil); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("broken-lock-session-is-discarded", func(t *testing.T) {
		held, err := (migration.PostgresLocker{DB: db}).Acquire(context.Background(), migration.LockKey)
		if err != nil {
			t.Fatal(err)
		}
		var pid int
		if err = db.QueryRow(`SELECT pid FROM pg_locks WHERE locktype='advisory' AND granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND ((classid::bigint<<32)|objid::bigint)=$1`, migration.LockKey).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		var killed bool
		if err = db.QueryRow(`SELECT pg_terminate_backend($1)`, pid).Scan(&killed); err != nil || !killed {
			t.Fatal(err, killed)
		}
		if err = held.Release(context.Background()); err == nil {
			t.Fatal("lost session reported release success")
		}
		next, err := (migration.PostgresLocker{DB: db}).Acquire(context.Background(), migration.LockKey)
		if err != nil {
			t.Fatal(err)
		}
		if err = next.Release(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMigrationFailureBoundaries(t *testing.T) {
	source := fstest.MapFS{"core/1_core.sql": {Data: []byte("-- +goose Up\nCREATE TABLE pangolin.applied(id int PRIMARY KEY);\n")}}
	modules := []migration.ModuleMigrations{{Module: "core", Directory: "core"}}
	t.Run("single-connection-pool-rejected", func(t *testing.T) {
		db, _ := isolatedDB(t)
		db.SetMaxOpenConns(1)
		runner, _ := migration.NewPostgres(db, source, 1, 1)
		if err := runner.Apply(context.Background(), modules); err == nil {
			t.Fatal("single connection deadlock accepted")
		}
	})
	t.Run("closed-pool", func(t *testing.T) {
		db, _ := isolatedDB(t)
		db.Close()
		runner, _ := migration.NewPostgres(db, source, 1, 1)
		if err := runner.Apply(context.Background(), modules); err == nil {
			t.Fatal("closed pool accepted")
		}
		if err := runner.CheckCompatibility(context.Background()); !errors.Is(err, migration.ErrIncompatible) {
			t.Fatal(err)
		}
		if _, err := (migration.PostgresLocker{DB: db}).Acquire(context.Background(), migration.LockKey); err == nil {
			t.Fatal("closed locker accepted")
		}
	})
	t.Run("version-table-failure", func(t *testing.T) {
		db, _ := isolatedDB(t)
		if _, err := db.Exec(`CREATE SCHEMA pangolin;CREATE TABLE pangolin.goose_db_version(wrong_column int)`); err != nil {
			t.Fatal(err)
		}
		runner, _ := migration.NewPostgres(db, source, 1, 1)
		if err := runner.CheckCompatibility(context.Background()); !errors.Is(err, migration.ErrIncompatible) {
			t.Fatal(err)
		}
	})
	t.Run("schema-permission-failure-releases-lock", func(t *testing.T) {
		db, dsn := isolatedDB(t)
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		parsed.User = url.UserPassword("pangolin_app", "local-app-only")
		app, err := sql.Open("pgx", parsed.String())
		if err != nil {
			t.Fatal(err)
		}
		defer app.Close()
		runner, _ := migration.NewPostgres(app, source, 1, 1)
		if err = runner.Apply(context.Background(), modules); err == nil {
			t.Fatal("application role migrated")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		owned, err := (migration.PostgresLocker{DB: db}).Acquire(ctx, migration.LockKey)
		if err != nil {
			t.Fatal("failed deployment retained lock", err)
		}
		if err = owned.Release(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
