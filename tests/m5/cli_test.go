//go:build integration

package m5

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/pangolin-framework/pangolin/internal/scaffold"
	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestA67Tools(t *testing.T) {
	t.Run("migrate-status", func(t *testing.T) {
		dsn := os.Getenv("PANGOLIN_M1_ADMIN_DSN")
		if dsn == "" {
			t.Fatal("PANGOLIN_M1_ADMIN_DSN required")
		}
		ctx := context.Background()
		root, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close(ctx)
		name := "m5_cli_" + rand.Text()
		if _, err = root.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := root.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
				t.Error(err)
			}
		}()
		config, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		config.Database = name
		db, err := sql.Open("pgx", config.ConnString())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		runner, err := migration.NewPostgres(db, fstest.MapFS{"status/1_status.sql": {Data: []byte("-- +goose Up\nCREATE TABLE status_contract(id text PRIMARY KEY);\n-- +goose Down\nSELECT 1;\n")}}, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err = runner.Apply(ctx, []migration.ModuleMigrations{{Module: "status", Directory: "status"}}); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PANGOLIN_MIGRATION_DSN", config.ConnString())
		dir := t.TempDir()
		var output bytes.Buffer
		c := scaffold.CLI{Dir: dir, Out: &output, Err: &output}
		if c.Run(context.Background(), []string{"new", "demo"}) != 0 {
			t.Fatal(output.String())
		}
		c.Dir = filepath.Join(dir, "demo")
		if c.Run(context.Background(), []string{"migrate", "status"}) != 0 || !bytes.Contains(output.Bytes(), []byte("applied=true")) {
			t.Fatal(output.String())
		}
	})
}
