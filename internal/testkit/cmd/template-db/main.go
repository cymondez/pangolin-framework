// Command template-db owns only named disposable M5 template databases.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	if len(os.Args) < 3 || !regexp.MustCompile(`^m5_template_[a-z0-9_]{1,48}$`).MatchString(os.Args[2]) || (len(os.Args) != 3 || os.Args[1] != "create" && os.Args[1] != "drop") && (len(os.Args) != 4 || os.Args[1] != "reconcile" || os.Args[3] != "tenant" && os.Args[3] != "organization") {
		fmt.Fprintln(os.Stderr, "create|drop m5_template_NAME required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, os.Getenv("PANGOLIN_M1_ADMIN_DSN"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "template fixture connection failed")
		os.Exit(1)
	}
	defer conn.Close(context.Background())
	if os.Args[1] == "reconcile" {
		config, err := pgx.ParseConfig(os.Getenv("PANGOLIN_M1_ADMIN_DSN"))
		if err != nil {
			panic(err)
		}
		config.Database = os.Args[2]
		db, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			panic(err)
		}
		defer db.Close(context.Background())
		expected := 2
		if os.Args[3] == "organization" {
			expected = 6
		}
		counts := map[string]int{}
		for _, table := range []string{"bookstore.books", "pangolin_audit.records", "pangolin_events.outbox", "pangolin_idempotency.results"} {
			var n int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
				panic(err)
			}
			counts[table] = n
			want := expected
			if table == "bookstore.books" {
				want = 3
				if os.Args[3] == "organization" {
					want = 5
				}
			}
			if n != want {
				panic(fmt.Sprintf("independent reconciliation %s: got %d want %d", table, n, want))
			}
		}
		var elevated, forced bool
		var linked int
		if err = db.QueryRow(ctx, `SELECT count(*) FROM pangolin_events.outbox o JOIN pangolin_audit.records a ON a.tenant_id=o.tenant_id AND a.scope_kind=o.scope_kind AND a.scope_id=o.scope_id AND a.trace=convert_from(o.envelope,'UTF8')::jsonb->>'correlation_id'`).Scan(&linked); err != nil {
			panic(err)
		}
		if linked != expected {
			panic("audit/event operation correlation missing")
		}
		var organizationSchema *string
		if err = db.QueryRow(ctx, "SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname='pangolin_app'").Scan(&elevated); err != nil {
			panic(err)
		}
		if err = db.QueryRow(ctx, "SELECT relrowsecurity AND relforcerowsecurity AND pg_get_userbyid(relowner)<>'pangolin_app' FROM pg_class WHERE oid='bookstore.books'::regclass").Scan(&forced); err != nil {
			panic(err)
		}
		if err = db.QueryRow(ctx, "SELECT to_regnamespace('pangolin_organization')::text").Scan(&organizationSchema); err != nil {
			panic(err)
		}
		if elevated || !forced || (organizationSchema != nil) != (os.Args[3] == "organization") {
			panic("generated profile/RLS privilege contract failed")
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"counts": counts, "force_rls": forced, "non_privileged_app": !elevated, "organization_schema": organizationSchema != nil})
		return
	}
	statement := "CREATE DATABASE " + pgx.Identifier{os.Args[2]}.Sanitize()
	if os.Args[1] == "drop" {
		statement = "DROP DATABASE " + pgx.Identifier{os.Args[2]}.Sanitize() + " WITH (FORCE)"
	}
	if _, err = conn.Exec(ctx, statement); err != nil {
		fmt.Fprintln(os.Stderr, "template fixture database operation failed:", err)
		os.Exit(1)
	}
}
