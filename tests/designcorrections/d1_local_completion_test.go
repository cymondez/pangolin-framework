//go:build integration

package designcorrections

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/application"
	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/local"
	"github.com/pangolin-framework/pangolin/events/outbox"
	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type localFact struct{ ID string }

type d1Fixture struct {
	admin, app, worker *pgxpool.Pool
	manager            uow.UnitOfWorkManager
	bus                *local.Bus
	publisher          *outbox.Publisher
	ctx                context.Context
}

func d1Pool(t *testing.T, raw string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return p
}

func d1FixtureFor(t *testing.T, handler func(context.Context, any) error) *d1Fixture {
	t.Helper()
	rootDSN := os.Getenv("PANGOLIN_M1_ADMIN_DSN")
	if rootDSN == "" {
		t.Fatal("PANGOLIN_M1_ADMIN_DSN is required")
	}
	root := d1Pool(t, rootDSN)
	name := "design_d1_" + strings.ReplaceAll(strings.ToLower(rand.Text()), "-", "_")
	if _, err := root.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := root.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated database: %v", err)
		}
	})
	open := func(key string) *pgxpool.Pool {
		u, err := url.Parse(os.Getenv(key))
		if err != nil {
			t.Fatal(err)
		}
		if u.Scheme == "" {
			t.Fatalf("%s is required", key)
		}
		u.Path = "/" + name
		return d1Pool(t, u.String())
	}
	admin, app, worker := open("PANGOLIN_M1_ADMIN_DSN"), open("PANGOLIN_M1_APP_DSN"), open("PANGOLIN_M1_WORKER_DSN")
	db, err := sql.Open("pgx", admin.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	eventMigrations, err := fsSub(eventpg.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	source := migration.JoinFS(map[string]fs.FS{"events": eventMigrations})
	runner, err := migration.NewPostgres(db, source, 3200001, 3200001)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(context.Background(), []migration.ModuleMigrations{{Module: "events", Directory: "events"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), `CREATE SCHEMA pangolin_design_d1;
CREATE TABLE pangolin_design_d1.records(id text PRIMARY KEY, value text NOT NULL);
GRANT USAGE ON SCHEMA pangolin_design_d1 TO pangolin_app;
GRANT SELECT,INSERT ON pangolin_design_d1.records TO pangolin_app;`); err != nil {
		t.Fatal(err)
	}
	registry, err := distributed.NewRegistry(distributed.Schema{Type: "designcorrections.fact", Version: 1, Required: []string{"value"}})
	if err != nil {
		t.Fatal(err)
	}
	policy := outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 3}
	store, err := eventpg.NewStore(worker, policy)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := outbox.NewPublisher(registry, store)
	if err != nil {
		t.Fatal(err)
	}
	bus, err := local.NewBus(local.Limits{MaxEventsPerUOW: 16, MaxDispatchDepth: 4}, local.Handler{Event: localFact{}, Handle: handler})
	if err != nil {
		t.Fatal(err)
	}
	resource := &eventpg.Resource{Base: &postgres.Resource{Pool: app}, Local: bus, Publisher: publisher}
	manager, err := uow.NewManager(resource, uow.DefaultOptions{DefaultTimeout: 5 * time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "design-review", Kind: "service"}, "design-review", true
	})
	ctx, err := r.Bind(context.Background(), scope.ScopeRef{TenantID: "design-review", Kind: "tenant", ID: "design-review"})
	if err != nil {
		t.Fatal(err)
	}
	return &d1Fixture{admin: admin, app: app, worker: worker, manager: manager, bus: bus, publisher: publisher, ctx: ctx}
}

// fsSub keeps this suite independent of other integration-test packages.
func fsSub(f fs.FS, name string) (fs.FS, error) { return fs.Sub(f, name) }

func d1Write(ctx context.Context, id, value string) error {
	ex, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, "INSERT INTO pangolin_design_d1.records(id,value) VALUES($1,$2)", id, value)
	return err
}

func d1Envelope(ctx context.Context, id string) distributed.Envelope {
	b, _ := scope.Current(ctx)
	return distributed.Envelope{ID: id, Type: "designcorrections.fact", SchemaVersion: 1, OccurredAt: time.Now().UTC(), TenantID: b.Ref.TenantID, ScopeKind: b.Ref.Kind, ScopeID: b.Ref.ID, CorrelationID: id, Producer: "designcorrections", Payload: []byte(`{"value":"` + id + `"}`)}
}

func d1Counts(t *testing.T, f *d1Fixture, ids ...string) (int, int) {
	t.Helper()
	var records, events int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM pangolin_design_d1.records WHERE id=ANY($1)", ids).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM pangolin_events.outbox WHERE event_id=ANY($1)", ids).Scan(&events); err != nil {
		t.Fatal(err)
	}
	return records, events
}

func TestD1LocalCompletionApplicationPortsCommitTogether(t *testing.T) {
	rootID, childID, eventID := "root-"+rand.Text(), "child-"+rand.Text(), "outbox-"+rand.Text()
	var f *d1Fixture
	var seen []string
	f = d1FixtureFor(t, func(ctx context.Context, value any) error {
		id := value.(localFact).ID
		seen = append(seen, id)
		if id != "initial" {
			return nil
		}
		if err := application.Command(ctx, f.manager, func(work context.Context) error {
			if err := d1Write(work, childID, "from-command"); err != nil {
				return err
			}
			return f.bus.Enqueue(work, localFact{ID: "followup"})
		}); err != nil {
			return err
		}
		if err := application.Query(ctx, f.manager, func(work context.Context) error {
			ex, err := postgres.Current(work, false)
			if err != nil {
				return err
			}
			var value string
			return ex.QueryRow(work, "SELECT value FROM pangolin_design_d1.records WHERE id=$1", childID).Scan(&value)
		}); err != nil {
			return err
		}
		return f.publisher.Publish(ctx, d1Envelope(ctx, eventID))
	})
	if err := application.Command(f.ctx, f.manager, func(ctx context.Context) error {
		if err := d1Write(ctx, rootID, "root"); err != nil {
			return err
		}
		return f.bus.Enqueue(ctx, localFact{ID: "initial"})
	}); err != nil {
		t.Fatalf("Application Command with controlled completion reentry: %v", err)
	}
	var rootValue, childValue string
	if err := f.admin.QueryRow(context.Background(), "SELECT value FROM pangolin_design_d1.records WHERE id=$1", rootID).Scan(&rootValue); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT value FROM pangolin_design_d1.records WHERE id=$1", childID).Scan(&childValue); err != nil {
		t.Fatal(err)
	}
	var outboxCount int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM pangolin_events.outbox WHERE event_id=$1", eventID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if rootValue != "root" || childValue != "from-command" || outboxCount != 1 || strings.Join(seen, ",") != "initial,followup" {
		t.Fatalf("independent committed state: root=%q child=%q outbox=%d local-order=%v", rootValue, childValue, outboxCount, seen)
	}
}

func TestD1SwallowedCompletionRequiredErrorRollsBackRoot(t *testing.T) {
	rootID, childID, eventID := "root-"+rand.Text(), "child-"+rand.Text(), "outbox-"+rand.Text()
	cause := errors.New("deliberate child failure")
	var f *d1Fixture
	f = d1FixtureFor(t, func(ctx context.Context, value any) error {
		if err := application.Command(ctx, f.manager, func(work context.Context) error {
			if err := d1Write(work, childID, "must-rollback"); err != nil {
				return err
			}
			return cause
		}); err == nil {
			return fmt.Errorf("nested Required Command unexpectedly succeeded")
		}
		return f.publisher.Publish(ctx, d1Envelope(ctx, eventID))
	})
	err := application.Command(f.ctx, f.manager, func(ctx context.Context) error {
		if err := d1Write(ctx, rootID, "must-rollback"); err != nil {
			return err
		}
		return f.bus.Enqueue(ctx, localFact{ID: "failing"})
	})
	if err == nil {
		t.Fatal("root succeeded after Local Event swallowed a failed Required Application Command")
	}
	if records, events := d1Counts(t, f, rootID, childID, eventID); records != 0 || events != 0 {
		t.Fatalf("independent connection observed partial commit: records=%d outbox=%d", records, events)
	}
}

func TestD1OrdinaryContextCannotReenterDuringCompletion(t *testing.T) {
	id := "ordinary-" + rand.Text()
	var f *d1Fixture
	var callbackContext context.Context
	f = d1FixtureFor(t, func(completionContext context.Context, value any) error {
		err := application.Command(callbackContext, f.manager, func(context.Context) error { return nil })
		if !errors.Is(err, uow.ErrBusy) {
			return fmt.Errorf("ordinary context reentry error = %v, want UOW_BUSY", err)
		}
		return nil
	})
	err := application.Command(f.ctx, f.manager, func(ctx context.Context) error {
		callbackContext = ctx
		if err := d1Write(ctx, id, "must-rollback"); err != nil {
			return err
		}
		return f.bus.Enqueue(ctx, localFact{ID: "ordinary-context"})
	})
	if err == nil {
		t.Fatal("root succeeded after ordinary context reentry during completion")
	}
	if records, _ := d1Counts(t, f, id); records != 0 {
		t.Fatalf("expected no records, observed %d", records)
	}
}
