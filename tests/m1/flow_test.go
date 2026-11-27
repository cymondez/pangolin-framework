//go:build integration

package m1

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	broker "github.com/pangolin-framework/pangolin/events/nats"
	"github.com/pangolin-framework/pangolin/examples/bookstore/application"
	"github.com/pangolin-framework/pangolin/examples/bookstore/domain"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
	natsgo "github.com/nats-io/nats.go"
)

func pool(t *testing.T, key string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(key)
	if dsn == "" {
		t.Fatalf("required integration configuration %s missing", key)
	}
	p, e := pgxpool.New(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	if e = p.Ping(context.Background()); e != nil {
		t.Fatal(e)
	}
	return p
}
func binding(t *testing.T, tenant string) context.Context {
	t.Helper()
	r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, tenant, true
	})
	ctx, e := r.Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant})
	if e != nil {
		t.Fatal(e)
	}
	return ctx
}
func TestM1Flow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := pool(t, "PANGOLIN_M1_ADMIN_DSN")
	app := pool(t, "PANGOLIN_M1_APP_DSN")
	worker := pool(t, "PANGOLIN_M1_WORKER_DSN")
	var exists bool
	if e := admin.QueryRow(ctx, "SELECT to_regclass('pangolin.books') IS NOT NULL").Scan(&exists); e != nil {
		t.Fatal(e)
	}
	if !exists {
		raw, e := os.ReadFile(filepath.Join("..", "..", "examples", "bookstore", "infrastructure", "sql", "schema.sql"))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = admin.Exec(ctx, "SET ROLE pangolin_owner;"+string(raw)+";RESET ROLE;"); e != nil {
			t.Fatal(e)
		}
	}
	manager, e := uow.NewManager(&postgres.Resource{Pool: app}, uow.DefaultOptions{DefaultTimeout: 5 * time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 4})
	if e != nil {
		t.Fatal(e)
	}
	repo := infrastructure.Repository{}
	books := application.NewBooks(manager, repo, repo)
	tenantA := binding(t, "tenant-a")
	tenantB := binding(t, "tenant-b")
	id := rand.Text()
	t.Run("S2/confirmed-commit", func(t *testing.T) {
		book, e := books.Create(tenantA, id, "M1 transactions")
		if e != nil {
			t.Fatal(e)
		}
		var title string
		if e = admin.QueryRow(ctx, "SELECT title FROM pangolin.books WHERE tenant_id='tenant-a' AND id=$1", id).Scan(&title); e != nil || title != book.Title {
			t.Fatal(title, e)
		}
		var count int
		if e = admin.QueryRow(ctx, "SELECT count(*) FROM pangolin.outbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->>'id'=$1", id).Scan(&count); e != nil || count != 1 {
			t.Fatal(count, e)
		}
	})
	t.Run("S2/second-write-rollback", func(t *testing.T) {
		badID := rand.Text()
		sql := `CREATE OR REPLACE FUNCTION pangolin.m1_reject_event() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF convert_from(NEW.envelope,'UTF8')::jsonb->'payload'->>'id' = '` + badID + `' THEN RAISE EXCEPTION 'injected outbox failure'; END IF; RETURN NEW; END$$; CREATE TRIGGER m1_reject_event BEFORE INSERT ON pangolin.outbox FOR EACH ROW EXECUTE FUNCTION pangolin.m1_reject_event();`
		if _, e := admin.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
		defer admin.Exec(context.Background(), "DROP TRIGGER m1_reject_event ON pangolin.outbox;DROP FUNCTION pangolin.m1_reject_event();")
		if _, e := books.Create(tenantA, badID, "must rollback"); e == nil {
			t.Fatal("fault was not observed")
		}
		var count int
		if e := admin.QueryRow(ctx, "SELECT count(*) FROM pangolin.books WHERE id=$1", badID).Scan(&count); e != nil || count != 0 {
			t.Fatal(count, e)
		}
	})
	t.Run("S3/tenant-rls", func(t *testing.T) {
		if _, e := books.Get(tenantB, id); !errors.Is(e, domain.ErrNotFound) {
			t.Fatal(e)
		}
		if _, e := books.Create(tenantB, id, "other tenant"); e != nil {
			t.Fatal(e)
		}
		if _, e := repo.Get(context.Background(), id); e == nil {
			t.Fatal("missing scope accepted")
		}
		if e := manager.Within(tenantB, uow.Options{}, func(work context.Context) error {
			executor, e := postgres.Current(work, false)
			if e != nil {
				return e
			}
			var count int
			e = executor.QueryRow(work, "SELECT count(*) FROM pangolin.books WHERE id=$1", id).Scan(&count)
			if e == nil && count != 1 {
				t.Fatalf("RLS leaked: %d", count)
			}
			return e
		}); e != nil {
			t.Fatal(e)
		}
		var privileged bool
		if e := app.QueryRow(ctx, "SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user").Scan(&privileged); e != nil || privileged {
			t.Fatal(e, privileged)
		}
		if e := manager.Within(tenantA, uow.Options{}, func(work context.Context) error {
			return repo.Insert(work, domain.Book{ID: rand.Text(), Title: "illegal", Version: 1})
		}); e == nil {
			t.Fatal("read-only write")
		}
	})
	t.Run("S2/executor-guards", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(tenantA)
		cancel()
		if e := manager.Within(cancelled, uow.Options{}, func(context.Context) error { t.Fatal("cancelled Begin callback"); return nil }); e == nil {
			t.Fatal("cancelled Begin succeeded")
		}
		if _, _, e := (&postgres.Resource{}).Begin(context.Background(), uow.EffectiveOptions{}); e == nil {
			t.Fatal("missing binding")
		}
		var escaped *postgres.Executor
		var escapedContext context.Context
		if e := manager.Within(tenantA, uow.Options{Mode: uow.ReadWrite}, func(work context.Context) error {
			executor, e := postgres.Current(work, true)
			if e != nil {
				return e
			}
			escaped, escapedContext = executor, work
			other, cancel := context.WithCancel(work)
			defer cancel()
			if _, e = executor.Exec(other, "SELECT 1"); e == nil {
				t.Fatal("exec context")
			}
			if e = executor.QueryRow(other, "SELECT 1").Scan(new(int)); e == nil {
				t.Fatal("row context")
			}
			rows, queryErr := executor.Query(work, "SELECT 1")
			if queryErr != nil {
				return queryErr
			}
			rows.Close()
			row := executor.QueryRow(work, "SELECT 1")
			if _, e = executor.Exec(work, "SELECT 1"); e == nil {
				t.Fatal("concurrent exec")
			}
			if e = executor.QueryRow(work, "SELECT 1").Scan(new(int)); e == nil {
				t.Fatal("concurrent row")
			}
			return row.Scan(new(int))
		}); !errors.Is(e, uow.ErrRollbackOnly) {
			t.Fatal("caught executor violations must poison the root", e)
		}
		if _, e := escaped.Exec(escapedContext, "SELECT 1"); e == nil {
			t.Fatal("expired exec")
		}
		if e := escaped.QueryRow(escapedContext, "SELECT 1").Scan(new(int)); e == nil {
			t.Fatal("expired row")
		}
	})
	natsURL := os.Getenv("PANGOLIN_M1_NATS_URL")
	if natsURL == "" {
		t.Fatal("NATS integration configuration missing")
	}
	client, e := broker.Connect(natsURL, "PANGOLIN_M1", "pangolin.m1.book_created")
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	if e = client.Provision(ctx); e != nil {
		t.Fatal(e)
	}
	subscription, e := client.Subscribe("bookstore")
	if e != nil {
		t.Fatal(e)
	}
	replicator := &infrastructure.Replicator{Pool: worker, Broker: client, Tenants: map[string]bool{"tenant-a": true, "tenant-b": true}}
	t.Run("S3/broker-rejections", func(t *testing.T) {
		var raw []byte
		if e := admin.QueryRow(ctx, "SELECT envelope FROM pangolin.outbox LIMIT 1").Scan(&raw); e != nil {
			t.Fatal(e)
		}
		event, e := distributed.Decode(raw)
		if e != nil {
			t.Fatal(e)
		}
		missing, e := broker.Connect(natsURL, "PANGOLIN_M1_MISSING", "pangolin.m1.book_created")
		if e != nil {
			t.Fatal(e)
		}
		defer missing.Close()
		if e = missing.Send(ctx, event); e == nil {
			t.Fatal("unconfirmed routing accepted")
		}
		if _, e = missing.Subscribe("missing"); e == nil {
			t.Fatal("missing stream")
		}
		invalid, e := broker.Connect(natsURL, "PANGOLIN_M1_INVALID", "pangolin.m1.invalid")
		if e != nil {
			t.Fatal(e)
		}
		defer invalid.Close()
		if e = invalid.Provision(ctx); e != nil {
			t.Fatal(e)
		}
		sub, e := invalid.Subscribe("invalid")
		if e != nil {
			t.Fatal(e)
		}
		conn, e := natsgo.Connect(natsURL)
		if e != nil {
			t.Fatal(e)
		}
		defer conn.Close()
		js, e := conn.JetStream()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = js.Publish("pangolin.m1.invalid", []byte(`{"invalid":true}`)); e != nil {
			t.Fatal(e)
		}
		if _, e = sub.Next(ctx); e == nil {
			t.Fatal("invalid envelope accepted")
		}
	})
	t.Run("S3/outbox-inbox-read-model", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			sent, e := replicator.Dispatch(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if !sent {
				break
			}
		}
		for i := 0; i < 100; i++ {
			var count int
			_ = admin.QueryRow(ctx, "SELECT count(*) FROM pangolin.book_views WHERE tenant_id='tenant-a' AND id=$1", id).Scan(&count)
			if count == 1 {
				break
			}
			delivery, e := subscription.Next(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = replicator.Receive(ctx, delivery.Envelope); e != nil {
				t.Fatal(e)
			}
			if e = delivery.Ack(ctx); e != nil {
				t.Fatal(e)
			}
			for j := 0; j < 100; j++ {
				processed, err := replicator.Project(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !processed {
					break
				}
			}
		}
		var title string
		if e := admin.QueryRow(ctx, "SELECT title FROM pangolin.book_views WHERE tenant_id='tenant-a' AND id=$1", id).Scan(&title); e != nil || title != "M1 transactions" {
			t.Fatal(title, e)
		}
	})
	t.Run("S3/duplicate-inbox", func(t *testing.T) {
		raw, _ := json.Marshal(domain.Book{ID: rand.Text(), Title: "duplicate", Version: 1})
		event := distributed.Envelope{ID: rand.Text(), Type: "bookstore.book_created", SchemaVersion: 1, OccurredAt: time.Now().UTC(), TenantID: "tenant-a", ScopeKind: "tenant", ScopeID: "tenant-a", CorrelationID: id, CausationID: id, Producer: "bookstore", Payload: raw}
		if e := replicator.Receive(ctx, event); e != nil {
			t.Fatal(e)
		}
		if e := replicator.Receive(ctx, event); e != nil {
			t.Fatal(e)
		}
		event.Producer = "forged"
		if e := replicator.Receive(ctx, event); e == nil {
			t.Fatal("unauthorized producer")
		}
		event.Producer = "bookstore"
		event.Payload = []byte(`{"id":"conflict","title":"conflict","version":1}`)
		if e := replicator.Receive(ctx, event); e == nil || !strings.Contains(e.Error(), "INBOX_CONTENT_CONFLICT") {
			t.Fatal(e)
		}
	})
}
