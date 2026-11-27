//go:build integration

package m2

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestM2ExecutorFailures(t *testing.T) {
	admin := pool(t, "PANGOLIN_M1_ADMIN_DSN")
	app := pool(t, "PANGOLIN_M1_APP_DSN")
	setup(t, admin)
	ctx := binding(t)
	m := manager(t, app)
	for _, isolation := range []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Serializable} {
		t.Run("isolation-"+time.Duration(isolation).String(), func(t *testing.T) {
			err := m.Within(ctx, uow.Options{Isolation: isolation}, func(c context.Context) error {
				ex, _ := postgres.Current(c, false)
				var actual string
				if e := ex.QueryRow(c, "SHOW transaction_isolation").Scan(&actual); e != nil {
					return e
				}
				if actual != map[uow.Isolation]string{uow.ReadCommitted: "read committed", uow.RepeatableRead: "repeatable read", uow.Serializable: "serializable"}[isolation] {
					t.Fatal(actual)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("rows-values-and-connection-hidden", func(t *testing.T) {
		if e := m.Within(ctx, uow.Options{}, func(c context.Context) error {
			ex, _ := postgres.Current(c, false)
			rows, e := ex.Query(c, "SELECT 7::int")
			if e != nil {
				return e
			}
			if rows.Conn() != nil {
				t.Fatal("cursor exposed connection")
			}
			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			values, e := rows.Values()
			if e != nil || len(values) != 1 {
				t.Fatal(values, e)
			}
			rows.Close()
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("unclosed-cursor-discards-connection", func(t *testing.T) {
		var escaped pgx.Rows
		id := rand.Text()
		e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := insert(c, id); e != nil {
				return e
			}
			ex, _ := postgres.Current(c, false)
			var e error
			escaped, e = ex.Query(c, "SELECT generate_series(1,10)")
			return e
		})
		if e == nil || !errors.Is(e, uow.ErrBusy) || count(t, admin, id) != 0 || app.Stat().AcquiredConns() != 0 {
			t.Fatal(e)
		}
		if escaped.Next() {
			t.Fatal("ended cursor readable")
		}
		escaped.Close()
	})
	t.Run("query-error-and-closed-executor", func(t *testing.T) {
		var ex *postgres.Executor
		var ended context.Context
		e := m.Within(ctx, uow.Options{}, func(c context.Context) error {
			ended = c
			ex, _ = postgres.Current(c, false)
			rows, e := ex.Query(c, "NOT VALID SQL")
			if rows != nil {
				rows.Close()
			}
			return e
		})
		if e == nil {
			t.Fatal("SQL accepted")
		}
		if _, e = ex.Query(ended, "SELECT 1"); !errors.Is(e, uow.ErrClosed) {
			t.Fatal(e)
		}
	})
	t.Run("row-scan-once", func(t *testing.T) {
		if e := m.Within(ctx, uow.Options{}, func(c context.Context) error {
			ex, _ := postgres.Current(c, false)
			row := ex.QueryRow(c, "SELECT 1")
			if e := row.Scan(new(int)); e != nil {
				return e
			}
			if !errors.Is(row.Scan(new(int)), uow.ErrClosed) {
				t.Fatal("row reusable")
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("row-cancel-before-scan", func(t *testing.T) {
		parent, cancel := context.WithCancel(ctx)
		defer cancel()
		e := m.Within(parent, uow.Options{}, func(c context.Context) error {
			ex, _ := postgres.Current(c, false)
			row := ex.QueryRow(c, "SELECT 1")
			cancel()
			return row.Scan(new(int))
		})
		if !errors.Is(e, context.Canceled) || app.Stat().AcquiredConns() != 0 {
			t.Fatal(e)
		}
	})
	t.Run("known-commit-rollback", func(t *testing.T) {
		_, e := admin.Exec(ctx, `SET ROLE pangolin_owner;CREATE TABLE IF NOT EXISTS pangolin_m2.deferred(tenant_id text,id text,FOREIGN KEY(tenant_id,id) REFERENCES pangolin_m2.records(tenant_id,id) DEFERRABLE INITIALLY DEFERRED);GRANT INSERT ON pangolin_m2.deferred TO pangolin_app;RESET ROLE;`)
		if e != nil {
			t.Fatal(e)
		}
		id := rand.Text()
		e = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := insert(c, id); e != nil {
				return e
			}
			ex, _ := postgres.Current(c, true)
			_, e := ex.Exec(c, "INSERT INTO pangolin_m2.deferred VALUES('tenant-a',$1)", rand.Text())
			return e
		})
		var f uow.Error
		if !errors.As(e, &f) || f.Outcome() != uow.RolledBack || count(t, admin, id) != 0 {
			t.Fatal(e)
		}
	})
	t.Run("begin-options-and-cancellation", func(t *testing.T) {
		r := &postgres.Resource{Pool: app}
		o := uow.EffectiveOptions{Transactional: true, DatabaseKey: r.DatabaseKey(), Isolation: 99}
		if _, _, e := r.Begin(ctx, o); !errors.Is(e, uow.ErrOptionsInvalid) {
			t.Fatal(e)
		}
		o.Isolation = uow.ReadCommitted
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, _, e := r.Begin(cancelled, o); e == nil {
			t.Fatal("cancelled acquire")
		}
	})
	t.Run("rls-configuration-failure-discards-connection", func(t *testing.T) {
		if _, e := admin.Exec(ctx, `CREATE OR REPLACE FUNCTION pangolin_m2.set_config(text,text,boolean) RETURNS text LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected RLS configuration failure'; END$$;`); e != nil {
			t.Fatal(e)
		}
		cfg, e := pgxpool.ParseConfig(os.Getenv("PANGOLIN_M1_APP_DSN"))
		if e != nil {
			t.Fatal(e)
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = "pangolin_m2,pg_catalog"
		p, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer p.Close()
		bad := manager(t, p)
		e = bad.Within(ctx, uow.Options{}, func(context.Context) error { t.Fatal("callback after failed RLS setup"); return nil })
		if e == nil || p.Stat().AcquiredConns() != 0 {
			t.Fatal(e, p.Stat())
		}
	})
}
