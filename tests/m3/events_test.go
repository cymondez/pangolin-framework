//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/inbox"
	"github.com/pangolin-framework/pangolin/events/local"
	"github.com/pangolin-framework/pangolin/events/outbox"
	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	"github.com/pangolin-framework/pangolin/persistence/migration"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/platform/audit"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Each queue suite owns a database; it never mutates another test's candidates.
func eventFixture(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	db, dsn := isolatedDB(t)
	source, err := fs.Sub(eventpg.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migration.NewPostgres(db, migration.JoinFS(map[string]fs.FS{"events": source, "audit": sub(audit.Migrations)}), 3200002, 3200002)
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.Apply(context.Background(), []migration.ModuleMigrations{{Module: "audit", Directory: "audit"}, {Module: "events", Directory: "events"}}); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE SCHEMA pangolin_m3;CREATE TABLE pangolin_m3.records(tenant_id text,scope_kind text,scope_id text,id text PRIMARY KEY,value int DEFAULT 1);ALTER TABLE pangolin_m3.records ENABLE ROW LEVEL SECURITY;ALTER TABLE pangolin_m3.records FORCE ROW LEVEL SECURITY;CREATE POLICY owner ON pangolin_m3.records USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));GRANT USAGE ON SCHEMA pangolin_m3 TO pangolin_app,pangolin_worker;GRANT SELECT,INSERT,UPDATE ON pangolin_m3.records TO pangolin_app,pangolin_worker;`)
	if err != nil {
		t.Fatal(err)
	}
	open := func(raw string) *pgxpool.Pool {
		p, e := pgxpool.New(context.Background(), raw)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Close)
		return p
	}
	admin := open(dsn)
	role := func(key string) *pgxpool.Pool {
		u, e := url.Parse(os.Getenv(key))
		if e != nil {
			t.Fatal(e)
		}
		a, _ := url.Parse(dsn)
		u.Path = a.Path
		return open(u.String())
	}
	return admin, role("PANGOLIN_M1_APP_DSN"), role("PANGOLIN_M1_WORKER_DSN")
}
func eventContext(t *testing.T, tenant string, p scope.PrincipalRef) context.Context {
	t.Helper()
	r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) { return p, tenant, true })
	ctx, err := r.Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func eventPolicy() outbox.Policy {
	return outbox.Policy{Lease: 100 * time.Millisecond, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 8}
}
func envelope(ctx context.Context, id string) distributed.Envelope {
	b, _ := scope.Current(ctx)
	return distributed.Envelope{ID: id, Type: "m3.fact", SchemaVersion: 1, OccurredAt: time.Now().UTC(), TenantID: b.Ref.TenantID, ScopeKind: b.Ref.Kind, ScopeID: b.Ref.ID, CorrelationID: id, Producer: "m3", Payload: json.RawMessage(`{"value":1}`)}
}
func eventRegistry(t *testing.T) *distributed.Registry {
	t.Helper()
	r, e := distributed.NewRegistry(distributed.Schema{Type: "m3.fact", Version: 1, Required: []string{"value"}})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

type fact int

type failingOutboxStore struct {
	outbox.Store
	err error
}

func (s failingOutboxStore) Append(context.Context, distributed.Envelope, []byte) error { return s.err }

func TestA04AtomicCompletion(t *testing.T) {
	admin, app, worker := eventFixture(t)
	ctx := eventContext(t, "events-"+rand.Text(), scope.PrincipalRef{ID: "alice", Kind: "user"})
	store, err := eventpg.NewStore(worker, eventPolicy())
	if err != nil {
		t.Fatal(err)
	}
	publisher, _ := outbox.NewPublisher(eventRegistry(t), store)
	for _, kind := range []string{"handler-chain-commit", "handler-error", "outbox-error", "audit-error"} {
		t.Run(kind, func(t *testing.T) {
			id := rand.Text()
			testPublisher := publisher
			if kind == "outbox-error" {
				testPublisher, _ = outbox.NewPublisher(eventRegistry(t), failingOutboxStore{Store: store, err: errors.New("outbox storage failed")})
			}
			var bus *local.Bus
			bus, _ = local.NewBus(local.Limits{MaxEventsPerUOW: 8, MaxDispatchDepth: 3}, local.Handler{Event: fact(0), Handle: func(work context.Context, v any) error {
				if v.(fact) == 0 {
					if e := insert(work, id); e != nil {
						return e
					}
					return bus.Enqueue(work, fact(1))
				}
				if kind == "handler-error" {
					return errors.New("handler failed")
				}
				e := envelope(work, id)
				return testPublisher.Publish(work, e)
			}})
			before := func(work context.Context) error {
				if kind == "audit-error" {
					return errors.New("required audit failed")
				}
				return (audit.Recorder{}).Record(work, audit.Record{Action: "fact.created", Resource: id, Result: "success", Trace: id})
			}
			m := manager(t, &eventpg.Resource{Base: &postgres.Resource{Pool: app}, Local: bus, Publisher: testPublisher, Before: []func(context.Context) error{before}})
			err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return bus.Enqueue(c, fact(0)) })
			var n, a int
			if e := admin.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM pangolin_events.outbox WHERE event_id=$1),(SELECT count(*) FROM pangolin_audit.records WHERE resource=$1)`, id).Scan(&n, &a); e != nil {
				t.Fatal(e)
			}
			if kind == "handler-chain-commit" {
				if err != nil || count(t, admin, id) != 1 || n != 1 || a != 1 {
					t.Fatal(err, n, a)
				}
			} else if err == nil || count(t, admin, id) != 0 || n != 0 || a != 0 {
				t.Fatal(err, n, a)
			}
		})
	}
	t.Run("no-scope", func(t *testing.T) {
		if err := publisher.Publish(context.Background(), envelope(ctx, rand.Text())); err == nil {
			t.Fatal("published without UOW")
		}
	})
	t.Run("outbox-disabled", func(t *testing.T) {
		m := manager(t, &postgres.Resource{Pool: app})
		if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, envelope(c, rand.Text())) }); !errors.Is(err, outbox.ErrUnavailable) {
			t.Fatal(err)
		}
	})
}
func TestA11LeaseFencing(t *testing.T) {
	admin, _, worker := eventFixture(t)
	ctx := eventContext(t, "lease-"+rand.Text(), scope.PrincipalRef{ID: "alice", Kind: "user"})
	store, _ := eventpg.NewStore(worker, eventPolicy())
	e := envelope(ctx, rand.Text())
	raw, _ := distributed.Encode(e)
	if _, err := admin.Exec(context.Background(), `INSERT INTO pangolin_events.outbox(event_id,tenant_id,scope_kind,scope_id,envelope) VALUES($1,$2,$3,$4,$5)`, e.ID, e.TenantID, e.ScopeKind, e.ScopeID, raw); err != nil {
		t.Fatal(err)
	}
	var first, second *outbox.Claim
	var err error
	t.Run("lease-takeover", func(t *testing.T) {
		first, err = store.Claim(context.Background(), "first", eventPolicy())
		if err != nil || first == nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(context.Background(), `UPDATE pangolin_events.outbox SET locked_until=now()-interval '1 second' WHERE event_id=$1`, e.ID); err != nil {
			t.Fatal(err)
		}
		second, err = store.Claim(context.Background(), "second", eventPolicy())
		if err != nil || second == nil || second.Token == first.Token {
			t.Fatal(err)
		}
	})
	if first == nil || second == nil {
		t.Fatal("lease setup failed")
	}
	for _, tc := range []struct {
		name    string
		success bool
	}{{"stale-success-update", true}, {"stale-failure-update", false}} {
		t.Run(tc.name, func(t *testing.T) {
			if err = store.Complete(context.Background(), *first, tc.success, 0, ""); !errors.Is(err, outbox.ErrLeaseLost) {
				t.Fatal("stale token accepted", err)
			}
		})
	}
	if err = store.Complete(context.Background(), *second, true, 0, ""); err != nil {
		t.Fatal(err)
	}
}

func TestA55LocalRollback(t *testing.T) {
	admin, app, queue := eventFixture(t)
	ctx := eventContext(t, "a55", scope.PrincipalRef{ID: "alice", Kind: "user"})
	store, _ := eventpg.NewStore(queue, eventPolicy())
	publisher, _ := outbox.NewPublisher(eventRegistry(t), store)
	for _, tc := range []struct {
		name   string
		limits local.Limits
		want   error
	}{{"event-limit", local.Limits{MaxEventsPerUOW: 2, MaxDispatchDepth: 10}, local.ErrEventLimit}, {"depth-limit", local.Limits{MaxEventsPerUOW: 10, MaxDispatchDepth: 1}, local.ErrDepthLimit}, {"cyclic-chain", local.Limits{MaxEventsPerUOW: 3, MaxDispatchDepth: 10}, local.ErrEventLimit}} {
		t.Run(tc.name, func(t *testing.T) {
			id := rand.Text()
			var b *local.Bus
			b, _ = local.NewBus(tc.limits, local.Handler{Event: fact(0), Handle: func(c context.Context, e any) error { return b.Enqueue(c, e) }})
			m := manager(t, &eventpg.Resource{Base: &postgres.Resource{Pool: app}, Local: b, Publisher: publisher})
			err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				if e := insert(c, id); e != nil {
					return e
				}
				if e := publisher.Publish(c, envelope(c, id)); e != nil {
					return e
				}
				return b.Enqueue(c, fact(1))
			})
			var n int
			if e := admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.outbox WHERE event_id=$1`, id).Scan(&n); e != nil {
				t.Fatal(e)
			}
			if !errors.Is(err, tc.want) || n != 0 || count(t, admin, id) != 0 {
				t.Fatal(err, n)
			}
		})
	}
}
func TestA06InboxLock(t *testing.T) {
	t.Run("two-worker-barrier", testInboxBarrier)
}
func testInboxBarrier(t *testing.T) {
	admin, _, queue := eventFixture(t)
	tenant := "inbox-" + rand.Text()
	ctx := eventContext(t, tenant, scope.PrincipalRef{ID: "producer", Kind: "user"})
	serviceCtx := eventContext(t, tenant, scope.PrincipalRef{ID: "projector", Kind: "service"})
	store, _ := eventpg.NewStore(queue, eventPolicy())
	group := rand.Text()
	e := envelope(ctx, rand.Text())
	acknowledged := false
	alerts := 0
	receiver, _ := inbox.NewReceiver(store, eventRegistry(t), group, func(context.Context, distributed.Envelope) (string, error) { return "m3", nil }, func(context.Context, error) { alerts++ })
	ack := func(context.Context) error {
		var n int
		if er := admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.inbox WHERE event_id=$1`, e.ID).Scan(&n); er != nil {
			return er
		}
		if n != 1 {
			t.Fatal("ACK before persistence")
		}
		acknowledged = true
		return nil
	}
	if err := receiver.Receive(context.Background(), e, ack); err != nil || !acknowledged {
		t.Fatal(err)
	}
	if err := receiver.Receive(context.Background(), e, ack); err != nil {
		t.Fatal(err)
	}
	conflict := e
	conflict.Payload = json.RawMessage(`{"value":2}`)
	if err := receiver.Receive(context.Background(), conflict, func(context.Context) error { t.Fatal("conflict ACK"); return nil }); !errors.Is(err, inbox.ErrContentConflict) || alerts != 1 {
		t.Fatal(err, alerts)
	}
	m := manager(t, &postgres.Resource{Pool: queue})
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	bind := func(context.Context, inbox.Candidate) (context.Context, error) { return serviceCtx, nil }
	first, _ := inbox.NewWorker(store, m, group, bind, func(c context.Context, event distributed.Envelope) error {
		calls.Add(1)
		close(entered)
		<-release
		return insert(c, event.ID)
	}, 3, time.Millisecond)
	second, _ := inbox.NewWorker(store, m, group, bind, func(context.Context, distributed.Envelope) error { calls.Add(1); return nil }, 3, time.Millisecond)
	done := make(chan error, 1)
	go func() {
		ok, err := first.Process(context.Background())
		if err == nil && !ok {
			err = errors.New("first did not process")
		}
		done <- err
	}()
	<-entered
	if ok, err := second.Process(context.Background()); err != nil || ok {
		t.Fatal("locked worker processed", ok, err)
	}
	close(release)
	if err := <-done; err != nil || calls.Load() != 1 || count(t, admin, e.ID) != 1 {
		t.Fatal(err, calls.Load())
	}
	if ok, err := second.Process(context.Background()); err != nil || ok {
		t.Fatal("processed redelivered", ok, err)
	}
}

func TestA50Outbox(t *testing.T) {
	admin, app, queue := eventFixture(t)
	ctx := eventContext(t, "a50", scope.PrincipalRef{ID: "alice", Kind: "user"})
	store, _ := eventpg.NewStore(queue, eventPolicy())
	publisher, _ := outbox.NewPublisher(eventRegistry(t), store)
	m := manager(t, &eventpg.Resource{Base: &postgres.Resource{Pool: app}, Publisher: publisher})
	for _, name := range []string{"commit", "rollback"} {
		t.Run(name, func(t *testing.T) {
			id := rand.Text()
			cause := errors.New("roll back")
			err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				if err := insert(c, id); err != nil {
					return err
				}
				if err := publisher.Publish(c, envelope(c, id)); err != nil {
					return err
				}
				if name == "rollback" {
					return cause
				}
				return nil
			})
			var n int
			if e := admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.outbox WHERE event_id=$1`, id).Scan(&n); e != nil {
				t.Fatal(e)
			}
			if name == "commit" {
				if err != nil || n != 1 || count(t, admin, id) != 1 {
					t.Fatal(err, n)
				}
			} else if !errors.Is(err, cause) || n != 0 || count(t, admin, id) != 0 {
				t.Fatal(err, n)
			}
		})
	}
	t.Run("no-scope", func(t *testing.T) {
		if err := publisher.Publish(context.Background(), envelope(ctx, rand.Text())); err == nil {
			t.Fatal("accepted without root")
		}
	})
	t.Run("outbox-disabled", func(t *testing.T) {
		m := manager(t, &postgres.Resource{Pool: app})
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, envelope(c, rand.Text())) })
		if !errors.Is(err, outbox.ErrUnavailable) {
			t.Fatal(err)
		}
	})
}

func TestA51EnvelopeRecovery(t *testing.T) {
	_, app, queue := eventFixture(t)
	ctx := eventContext(t, "a51", scope.PrincipalRef{ID: "alice", Kind: "user"})
	store, _ := eventpg.NewStore(queue, eventPolicy())
	publisher, _ := outbox.NewPublisher(eventRegistry(t), store)
	m := manager(t, &eventpg.Resource{Base: &postgres.Resource{Pool: app}, Publisher: publisher})
	original := envelope(ctx, rand.Text())
	original.CausationID = "cause-1"
	original.OccurredAt = original.OccurredAt.Round(0)
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, original) }); err != nil {
		t.Fatal(err)
	}
	t.Run("producer-restart", func(t *testing.T) {
		store = nil
		restarted, e := eventpg.NewStore(queue, eventPolicy())
		if e != nil {
			t.Fatal(e)
		}
		claim, e := restarted.Claim(context.Background(), "restarted", eventPolicy())
		if e != nil || claim == nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(original, claim.Envelope) {
			t.Fatalf("envelope changed: %#v %#v", original, claim.Envelope)
		}
		store = restarted
		if e = store.Complete(context.Background(), *claim, true, 0, ""); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("full-envelope-replay", func(t *testing.T) {
		raw, e := distributed.Encode(original)
		if e != nil {
			t.Fatal(e)
		}
		restored, e := distributed.Decode(raw)
		if e != nil || !reflect.DeepEqual(original, restored) {
			t.Fatal(e, restored)
		}
	})
	t.Run("scope-preserved", func(t *testing.T) {
		for _, kind := range []string{"org.organization", "org.enterprise"} {
			a := &eventAuthority{kind: kind}
			r, _ := scope.NewRegistry(func(context.Context) (scope.PrincipalRef, string, bool) {
				return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a51", true
			}, a)
			bound, e := r.Bind(context.Background(), scope.ScopeRef{TenantID: "a51", Kind: kind, ID: rand.Text()})
			if e != nil {
				t.Fatal(e)
			}
			event := envelope(bound, rand.Text())
			if e = m.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, event) }); e != nil {
				t.Fatal(e)
			}
			claim, e := store.Claim(context.Background(), "owner-replay", eventPolicy())
			if e != nil || claim == nil || claim.Envelope.ScopeKind != kind || claim.Envelope.ScopeID != event.ScopeID {
				t.Fatal(e, claim)
			}
			if e = store.Complete(context.Background(), *claim, true, 0, ""); e != nil {
				t.Fatal(e)
			}
		}
	})
}

type failingReceiverStore struct {
	inbox.Store
	fail  error
	after bool
}

func (s failingReceiverStore) Persist(c context.Context, g string, e distributed.Envelope, raw []byte, p string) error {
	if !s.after {
		return s.fail
	}
	if err := s.Store.Persist(c, g, e, raw, p); err != nil {
		return err
	}
	return s.fail
}
func TestA53Receiver(t *testing.T) {
	admin, _, queue := eventFixture(t)
	ctx := eventContext(t, "a53", scope.PrincipalRef{ID: "producer", Kind: "user"})
	store, _ := eventpg.NewStore(queue, eventPolicy())
	reg := eventRegistry(t)
	for _, name := range []string{"persist-before-ack", "database-failure", "receiver-crash-before-persist", "receiver-crash-after-persist", "identical-duplicate", "conflicting-duplicate"} {
		t.Run(name, func(t *testing.T) {
			group, id := rand.Text(), rand.Text()
			event := envelope(ctx, id)
			var storage inbox.Store = store
			fault := errors.New("injected receiver exit")
			if name == "database-failure" || name == "receiver-crash-before-persist" {
				storage = failingReceiverStore{Store: store, fail: fault}
			}
			if name == "receiver-crash-after-persist" {
				storage = failingReceiverStore{Store: store, fail: fault, after: true}
			}
			alerts := 0
			r, e := inbox.NewReceiver(storage, reg, group, func(context.Context, distributed.Envelope) (string, error) { return "m3", nil }, func(context.Context, error) { alerts++ })
			if e != nil {
				t.Fatal(e)
			}
			acks := 0
			ack := func(context.Context) error {
				var n int
				if e := admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, group, id).Scan(&n); e != nil {
					return e
				}
				if n != 1 {
					t.Fatal("ACK before durable row")
				}
				acks++
				return nil
			}
			err := r.Receive(context.Background(), event, ack)
			if strings.Contains(name, "failure") || strings.Contains(name, "crash") {
				if !errors.Is(err, fault) || acks != 0 {
					t.Fatal(err, acks)
				}
				var n int
				if e := admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, group, id).Scan(&n); e != nil {
					t.Fatal(e)
				}
				want := 0
				if name == "receiver-crash-after-persist" {
					want = 1
				}
				if n != want {
					t.Fatal(n, want)
				}
				if want == 1 {
					restarted, _ := inbox.NewReceiver(store, reg, group, func(context.Context, distributed.Envelope) (string, error) { return "m3", nil }, func(context.Context, error) {})
					if e := restarted.Receive(context.Background(), event, ack); e != nil || acks != 1 {
						t.Fatal(e, acks)
					}
				}
				return
			}
			if err != nil || acks != 1 {
				t.Fatal(err, acks)
			}
			if name == "identical-duplicate" {
				if e := r.Receive(context.Background(), event, ack); e != nil || acks != 2 {
					t.Fatal(e, acks)
				}
			}
			if name == "conflicting-duplicate" {
				event.Payload = json.RawMessage(`{"value":99}`)
				if e := r.Receive(context.Background(), event, ack); !errors.Is(e, inbox.ErrContentConflict) || acks != 1 || alerts != 1 {
					t.Fatal(e, acks, alerts)
				}
			}
		})
	}
}

func TestA06InboxCrash(t *testing.T) {
	admin, _, queue := eventFixture(t)
	ctx := eventContext(t, "a06-crash", scope.PrincipalRef{ID: "producer", Kind: "user"})
	event := envelope(ctx, rand.Text())
	group := rand.Text()
	store, _ := eventpg.NewStore(queue, eventPolicy())
	raw, _ := distributed.Encode(event)
	if e := store.Persist(context.Background(), group, event, raw, "m3"); e != nil {
		t.Fatal(e)
	}
	t.Run("crash-before-commit", func(t *testing.T) {
		binary, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(binary, "-test.run=^TestM3InboxCrashChild$", "-test.v")
		cmd.Env = append(os.Environ(), "PANGOLIN_M3_CRASH_CHILD=1", "PANGOLIN_M3_CRASH_DSN="+queue.Config().ConnString(), "PANGOLIN_M3_CRASH_GROUP="+group)
		out, e := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(e, &exit) || exit.ExitCode() != 73 {
			t.Fatalf("child did not crash at handler: %s %v", out, e)
		}
		if count(t, admin, event.ID) != 0 {
			t.Fatal("uncommitted business survived process death")
		}
		var state string
		if e := admin.QueryRow(context.Background(), `SELECT state FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, group, event.ID).Scan(&state); e != nil || state != "pending" {
			t.Fatal(e, state)
		}
		t.Logf("child_exit=73 event=%s state=%s effect=0", event.ID, state)
	})
	t.Run("retry-after-crash", func(t *testing.T) {
		service := eventContext(t, event.TenantID, scope.PrincipalRef{ID: "projector", Kind: "service"})
		w, _ := inbox.NewWorker(store, manager(t, &postgres.Resource{Pool: queue}), group, func(context.Context, inbox.Candidate) (context.Context, error) { return service, nil }, func(c context.Context, e distributed.Envelope) error { return insert(c, e.ID) }, 3, time.Millisecond)
		ok, e := w.Process(context.Background())
		if e != nil || !ok || count(t, admin, event.ID) != 1 {
			t.Fatal(e, ok)
		}
		ok, e = w.Process(context.Background())
		if e != nil || ok {
			t.Fatal(e, ok)
		}
	})
}
func TestM3InboxCrashChild(t *testing.T) {
	if os.Getenv("PANGOLIN_M3_CRASH_CHILD") != "1" {
		return
	}
	q, e := pgxpool.New(context.Background(), os.Getenv("PANGOLIN_M3_CRASH_DSN"))
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	store, _ := eventpg.NewStore(q, eventPolicy())
	group := os.Getenv("PANGOLIN_M3_CRASH_GROUP")
	w, _ := inbox.NewWorker(store, manager(t, &postgres.Resource{Pool: q}), group, func(_ context.Context, c inbox.Candidate) (context.Context, error) {
		return eventContext(t, c.Envelope.TenantID, scope.PrincipalRef{ID: "projector", Kind: "service"}), nil
	}, func(c context.Context, e distributed.Envelope) error {
		if err := insert(c, e.ID); err != nil {
			return err
		}
		os.Exit(73)
		return nil
	}, 3, time.Millisecond)
	_, e = w.Process(context.Background())
	t.Fatal("child did not reach exit", e)
}

type eventAuthority struct {
	kind                string
	denied, unavailable bool
	revision            string
}

func (a *eventAuthority) Kinds() []string { return []string{a.kind} }
func (a *eventAuthority) Resolve(_ context.Context, p scope.PrincipalRef, r scope.ScopeRef) (scope.ScopeBinding, error) {
	if a.unavailable {
		return scope.ScopeBinding{}, scope.ErrAuthorityUnavailable
	}
	if a.denied {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	revision := a.revision
	if revision == "" {
		revision = "r1"
	}
	return scope.ScopeBinding{Ref: r, Principal: p, AuthorityRevision: revision}, nil
}
func (a *eventAuthority) Authorize(context.Context, scope.ScopeBinding, string) error {
	if a.denied {
		return scope.ErrDenied
	}
	return nil
}

func TestA54ExternalIdempotency(t *testing.T) {
	admin, _, queue := eventFixture(t)
	event := envelope(eventContext(t, "a54", scope.PrincipalRef{ID: "producer", Kind: "user"}), rand.Text())
	store, _ := eventpg.NewStore(queue, eventPolicy())
	group := rand.Text()
	raw, _ := distributed.Encode(event)
	if e := store.Persist(context.Background(), group, event, raw, "m3"); e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	effects := 0
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		mu.Lock()
		defer mu.Unlock()
		keys = append(keys, key)
		if key == "" {
			w.WriteHeader(400)
			return
		}
		if !seen[key] {
			seen[key] = true
			effects++
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	service := eventContext(t, event.TenantID, scope.PrincipalRef{ID: "projector", Kind: "service"})
	attempt := 0
	cause := errors.New("crash after external success")
	w, _ := inbox.NewWorker(store, manager(t, &postgres.Resource{Pool: queue}), group, func(context.Context, inbox.Candidate) (context.Context, error) { return service, nil }, func(c context.Context, e distributed.Envelope) error {
		key, err := inbox.ExternalKey(group, e, "payment.capture")
		if err != nil {
			return err
		}
		request, _ := http.NewRequestWithContext(c, "POST", server.URL, nil)
		request.Header.Set("Idempotency-Key", key)
		response, err := server.Client().Do(request)
		if err != nil {
			return err
		}
		response.Body.Close()
		attempt++
		if attempt == 1 {
			return cause
		}
		return insert(c, e.ID)
	}, 3, time.Millisecond)
	t.Run("external-success-local-crash", func(t *testing.T) {
		ok, err := w.Process(context.Background())
		if !errors.Is(err, cause) || ok || count(t, admin, event.ID) != 0 || effects != 1 {
			t.Fatal(err, ok, effects)
		}
	})
	// Move only this event's retry clock rather than relying on a scheduling sleep.
	if _, err := admin.Exec(context.Background(), `UPDATE pangolin_events.inbox SET next_attempt_at=now() WHERE consumer_group=$1 AND event_id=$2`, group, event.ID); err != nil {
		t.Fatal(err)
	}
	t.Run("stable-provider-key", func(t *testing.T) {
		ok, err := w.Process(context.Background())
		if err != nil || !ok || count(t, admin, event.ID) != 1 {
			t.Fatal(err, ok)
		}
		mu.Lock()
		defer mu.Unlock()
		if effects != 1 || len(keys) != 2 || keys[0] != keys[1] {
			t.Fatal(effects, keys)
		}
	})
	t.Run("no-exactly-once-claim", func(t *testing.T) {
		key, _ := inbox.ExternalKey(group, event, "payment.capture")
		other, _ := inbox.ExternalKey(group, event, "email.send")
		if key == other {
			t.Fatal("different external operations share identity")
		}
		for range 2 {
			response, err := server.Client().Post(server.URL, "application/json", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 400 {
				t.Fatal("fixture silently accepts nonidempotent request")
			}
		}
		t.Log("Inbox protects local DB effects; provider must implement Idempotency-Key for external effects")
	})
}

func TestA79ExecutionScope(t *testing.T) {
	admin, app, _ := eventFixture(t)
	principal := scope.PrincipalRef{ID: "service-current", Kind: "service"}
	a := &eventAuthority{kind: "org.organization"}
	identity := func(context.Context) (scope.PrincipalRef, string, bool) { return principal, "a79", true }
	r, _ := scope.NewRegistry(identity, a)
	ref := scope.ScopeRef{TenantID: "a79", Kind: "org.organization", ID: "org-stable"}
	calls := 0
	exec := distributed.Execution{Manager: manager(t, &postgres.Resource{Pool: app}), Identity: identity, Resolver: r, TrustedProducer: func(_ context.Context, p string) error {
		if p != "trusted" {
			return scope.ErrDenied
		}
		return nil
	}, Authorize: r.Authorize, Delegation: func(_ context.Context, p scope.PrincipalRef) error {
		if p.ID == "revoked-user" {
			return scope.ErrDenied
		}
		return nil
	}}
	task := distributed.Task{Scope: ref, Producer: "trusted", Permission: "fact.project"}
	work := func(c context.Context) error {
		b, _ := scope.Current(c)
		if b.Ref != ref || b.Principal != principal {
			t.Fatal(b)
		}
		calls++
		return insert(c, rand.Text())
	}
	t.Run("restart-replay", func(t *testing.T) {
		stored, e := json.Marshal(task)
		if e != nil {
			t.Fatal(e)
		}
		var restored distributed.Task
		if e = json.Unmarshal(stored, &restored); e != nil {
			t.Fatal(e)
		}
		if e = exec.Run(context.Background(), restored, work); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("scope-tamper", func(t *testing.T) {
		for _, bad := range []scope.ScopeRef{{TenantID: "other", Kind: ref.Kind, ID: ref.ID}, {TenantID: ref.TenantID, Kind: "missing", ID: ref.ID}} {
			forged := task
			forged.Scope = bad
			if e := exec.Run(context.Background(), forged, work); e == nil {
				t.Fatal("tampered task authorized")
			}
		}
	})
	t.Run("revoked-user", func(t *testing.T) {
		old := task
		old.DelegatedPrincipal = &scope.PrincipalRef{ID: "revoked-user", Kind: "user"}
		if e := exec.Run(context.Background(), old, work); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
	})
	t.Run("current-service-grant", func(t *testing.T) {
		if e := exec.Run(context.Background(), task, work); e != nil {
			t.Fatal(e)
		}
		if calls != 2 {
			t.Fatal(calls)
		}
		var n int
		if e := admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_m3.records`).Scan(&n); e != nil || n != 2 {
			t.Fatal(e, n)
		}
	})
	t.Run("missing-org-scope", func(t *testing.T) {
		old := task
		old.Scope.Kind = ""
		if e := exec.Run(context.Background(), old, work); e == nil {
			t.Fatal("missing org scope fell back")
		}
	})
	t.Run("unavailable-provider", func(t *testing.T) {
		a.unavailable = true
		defer func() { a.unavailable = false }()
		if e := exec.Run(context.Background(), task, work); !errors.Is(e, scope.ErrAuthorityUnavailable) {
			t.Fatal(e)
		}
	})
	t.Run("legacy-tenant-upcast", func(t *testing.T) {
		legacy := distributed.Envelope{ID: "legacy", Type: "tenant.fact", SchemaVersion: 1, OccurredAt: time.Now(), TenantID: "tenant-old", CorrelationID: "operation", Producer: "trusted", Payload: json.RawMessage(`{}`)}
		raw, _ := json.Marshal(map[string]any{"event_id": legacy.ID, "event_type": legacy.Type, "schema_version": 1, "occurred_at": legacy.OccurredAt, "tenant_id": legacy.TenantID, "correlation_id": legacy.CorrelationID, "producer": legacy.Producer, "payload": map[string]any{}})
		registry, _ := distributed.NewRegistry(distributed.Schema{Type: legacy.Type, Version: 1, LegacyTenant: true})
		decoded, e := registry.Decode(raw)
		if e != nil || decoded.ScopeKind != "tenant" || decoded.ScopeID != legacy.TenantID {
			t.Fatal(e, decoded)
		}
		org, _ := distributed.NewRegistry(distributed.Schema{Type: legacy.Type, Version: 1})
		if _, e = org.Decode(raw); e == nil {
			t.Fatal("unregistered legacy owner guessed")
		}
	})
}

func TestEventRetryMaintenance(t *testing.T) {
	admin, _, queue := eventFixture(t)
	ctx := eventContext(t, "maintenance", scope.PrincipalRef{ID: "operator", Kind: "service"})
	policy := eventPolicy()
	policy.MaxAttempts = 2
	store, _ := eventpg.NewStore(queue, policy)
	event := envelope(ctx, rand.Text())
	raw, _ := distributed.Encode(event)
	if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin_events.outbox(event_id,tenant_id,scope_kind,scope_id,envelope)VALUES($1,$2,$3,$4,$5)`, event.ID, event.TenantID, event.ScopeKind, event.ScopeID, raw); e != nil {
		t.Fatal(e)
	}
	t.Run("retry-dead-bounded", func(t *testing.T) {
		first, e := store.Claim(context.Background(), "worker", policy)
		if e != nil || first == nil {
			t.Fatal(e)
		}
		if e = store.Complete(context.Background(), *first, false, time.Hour, "DELIVERY_FAILED"); e != nil {
			t.Fatal(e)
		}
		if next, e := store.Claim(context.Background(), "worker", policy); e != nil || next != nil {
			t.Fatal(e, next)
		}
		if _, e = admin.Exec(context.Background(), `UPDATE pangolin_events.outbox SET next_attempt_at=now() WHERE event_id=$1`, event.ID); e != nil {
			t.Fatal(e)
		}
		second, e := store.Claim(context.Background(), "worker", policy)
		if e != nil || second == nil || second.Attempts != 2 {
			t.Fatal(e, second)
		}
		if e = store.Complete(context.Background(), *second, false, 0, "DELIVERY_FAILED"); e != nil {
			t.Fatal(e)
		}
		var state string
		if e = admin.QueryRow(context.Background(), `SELECT state FROM pangolin_events.outbox WHERE event_id=$1`, event.ID).Scan(&state); e != nil || state != "dead" {
			t.Fatal(e, state)
		}
	})
	allow := func(context.Context, eventpg.Replay) error { return nil }
	record := func(c context.Context, r eventpg.Replay) error {
		return (audit.Recorder{}).Record(c, audit.Record{Action: "event.replay", Resource: r.EventID, Result: "success", Trace: r.EventID})
	}
	m := manager(t, &postgres.Resource{Pool: queue})
	t.Run("audited-outbox-replay", func(t *testing.T) {
		replayer, e := eventpg.NewMaintenance(store, allow, record)
		if e != nil {
			t.Fatal(e)
		}
		if e = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return replayer.ReplayDead(c, "outbox", "", event.ID) }); e != nil {
			t.Fatal(e)
		}
		var state string
		var restored []byte
		var attempt, audits int
		if e = admin.QueryRow(context.Background(), `SELECT state,attempts,envelope FROM pangolin_events.outbox WHERE event_id=$1`, event.ID).Scan(&state, &attempt, &restored); e != nil || state != "pending" || attempt != 0 || string(restored) != string(raw) {
			t.Fatal(e, state, attempt)
		}
		if e = admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_audit.records WHERE resource=$1`, event.ID).Scan(&audits); e != nil || audits != 1 {
			t.Fatal(e, audits)
		}
	})
	group := rand.Text()
	event.ID = rand.Text()
	raw, _ = distributed.Encode(event)
	if e := store.Persist(context.Background(), group, event, raw, "m3"); e != nil {
		t.Fatal(e)
	}
	candidate, e := store.Candidate(context.Background(), group)
	if e != nil || candidate == nil {
		t.Fatal(e)
	}
	t.Run("fingerprint-rechecked-under-lock", func(t *testing.T) {
		forged := *candidate
		forged.Fingerprint = []byte("forged")
		if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { _, e := store.Lock(c, forged); return e }); !errors.Is(e, inbox.ErrContentConflict) {
			t.Fatal(e)
		}
	})
	t.Run("processed-owner-and-version-fenced", func(t *testing.T) {
		forged := *candidate
		forged.Envelope.ScopeID = "other"
		if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return store.Processed(c, forged) }); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
		forged = *candidate
		forged.Version++
		if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return store.Processed(c, forged) }); !errors.Is(e, inbox.ErrContentConflict) {
			t.Fatal(e)
		}
	})
	t.Run("retry-cannot-overwrite-processed", func(t *testing.T) {
		if e := store.Retry(context.Background(), *candidate, 2, 0); e != nil {
			t.Fatal(e)
		}
		stale := *candidate
		current, e := store.Candidate(context.Background(), group)
		if e != nil || current.Version <= stale.Version {
			t.Fatal(e)
		}
		if e = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			locked, e := store.Lock(c, *current)
			if e != nil {
				return e
			}
			if !locked {
				return errors.New("not locked")
			}
			return store.Processed(c, *current)
		}); e != nil {
			t.Fatal(e)
		}
		if e = store.Retry(context.Background(), stale, 1, 0); e != nil {
			t.Fatal(e)
		}
		var state string
		if e = admin.QueryRow(context.Background(), `SELECT state FROM pangolin_events.inbox WHERE event_id=$1 AND consumer_group=$2`, event.ID, group).Scan(&state); e != nil || state != "processed" {
			t.Fatal(e, state)
		}
	})
	t.Run("processed-replay-denied", func(t *testing.T) {
		replay, _ := eventpg.NewMaintenance(store, allow, record)
		if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return replay.ReplayDead(c, "inbox", group, event.ID) }); !errors.Is(e, eventpg.ErrReplayNotDead) {
			t.Fatal(e)
		}
	})
	t.Run("audit-failure-rolls-back-replay", func(t *testing.T) {
		if _, e := admin.Exec(context.Background(), `UPDATE pangolin_events.inbox SET state='dead' WHERE event_id=$1 AND consumer_group=$2`, event.ID, group); e != nil {
			t.Fatal(e)
		}
		cause := errors.New("audit unavailable")
		replay, _ := eventpg.NewMaintenance(store, allow, func(context.Context, eventpg.Replay) error { return cause })
		if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return replay.ReplayDead(c, "inbox", group, event.ID) }); !errors.Is(e, cause) {
			t.Fatal(e)
		}
		var state string
		if e := admin.QueryRow(context.Background(), `SELECT state FROM pangolin_events.inbox WHERE event_id=$1 AND consumer_group=$2`, event.ID, group).Scan(&state); e != nil || state != "dead" {
			t.Fatal(e, state)
		}
		replay, _ = eventpg.NewMaintenance(store, allow, record)
		if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return replay.ReplayDead(c, "inbox", group, event.ID) }); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("retention-protects-active-work", func(t *testing.T) {
		old := envelope(ctx, rand.Text())
		oldRaw, _ := distributed.Encode(old)
		if _, e := admin.Exec(context.Background(), `INSERT INTO pangolin_events.outbox(event_id,tenant_id,scope_kind,scope_id,envelope,state,sent_at)VALUES($1,$2,$3,$4,$5,'sent',now()-interval '10 hours')`, old.ID, old.TenantID, old.ScopeKind, old.ScopeID, oldRaw); e != nil {
			t.Fatal(e)
		}
		if _, e := admin.Exec(context.Background(), `UPDATE pangolin_events.inbox SET state='processed',processed_at=now()-interval '10 hours' WHERE event_id=$1 AND consumer_group=$2`, event.ID, group); e != nil {
			t.Fatal(e)
		}
		n, e := store.Purge(context.Background(), eventpg.Retention{Broker: time.Hour, ManualReplay: 2 * time.Hour, Inbox: 3 * time.Hour, OutboxSent: 3 * time.Hour})
		if e != nil || n != 2 {
			t.Fatal(e, n)
		}
		var remaining int
		if e = admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_events.outbox WHERE state='pending'`).Scan(&remaining); e != nil || remaining != 1 {
			t.Fatal(e, remaining)
		}
	})
}

func TestInboxScopeFailureBackoff(t *testing.T) {
	for _, phase := range []string{"bind", "admission"} {
		t.Run(phase, func(t *testing.T) {
			admin, _, queue := eventFixture(t)
			group := rand.Text()
			tenant := "scope-failure-" + phase
			service := eventContext(t, tenant, scope.PrincipalRef{ID: "projector", Kind: "service"})
			store, _ := eventpg.NewStore(queue, eventPolicy())
			denied := envelope(service, rand.Text())
			denied.ScopeKind = "org.organization"
			denied.ScopeID = "blocked"
			valid := envelope(service, rand.Text())
			for _, event := range []distributed.Envelope{denied, valid} {
				raw, _ := distributed.Encode(event)
				if e := store.Persist(context.Background(), group, event, raw, "m3"); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := admin.Exec(context.Background(), `UPDATE pangolin_events.inbox SET created_at=now()-interval '1 second' WHERE event_id=$1`, denied.ID); e != nil {
				t.Fatal(e)
			}
			a := &eventAuthority{kind: "org.organization"}
			r, _ := scope.NewRegistry(func(context.Context) (scope.PrincipalRef, string, bool) {
				return scope.PrincipalRef{ID: "projector", Kind: "service"}, tenant, true
			}, a)
			w, _ := inbox.NewWorker(store, manager(t, &postgres.Resource{Pool: queue}), group, func(c context.Context, candidate inbox.Candidate) (context.Context, error) {
				if candidate.ID != denied.ID {
					return service, nil
				}
				if phase == "bind" {
					return nil, scope.ErrAuthorityUnavailable
				}
				a.denied = false
				bound, e := r.Bind(c, scope.ScopeRef{TenantID: tenant, Kind: "org.organization", ID: "blocked"})
				a.denied = true
				return bound, e
			}, func(c context.Context, event distributed.Envelope) error { return insert(c, event.ID) }, 2, time.Hour)
			ok, e := w.Process(context.Background())
			if e == nil || ok {
				t.Fatal("unauthorized event processed", e, ok)
			}
			var attempts int
			var deferred bool
			var state string
			if e = admin.QueryRow(context.Background(), `SELECT attempts,next_attempt_at>now(),state FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, group, denied.ID).Scan(&attempts, &deferred, &state); e != nil || attempts != 1 || !deferred || state != "pending" {
				t.Fatalf("scope failure not deferred: err=%v attempts=%d delayed=%v state=%s", e, attempts, deferred, state)
			}
			if ok, e = w.Process(context.Background()); e != nil || !ok || count(t, admin, valid.ID) != 1 {
				t.Fatal("later authorized event starved", e, ok)
			}
			if _, e = admin.Exec(context.Background(), `UPDATE pangolin_events.inbox SET next_attempt_at=now() WHERE event_id=$1 AND consumer_group=$2`, denied.ID, group); e != nil {
				t.Fatal(e)
			}
			if ok, e = w.Process(context.Background()); e == nil || ok {
				t.Fatal("second unauthorized attempt processed", e, ok)
			}
			if e = admin.QueryRow(context.Background(), `SELECT attempts,state FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, group, denied.ID).Scan(&attempts, &state); e != nil || attempts != 2 || state != "dead" {
				t.Fatal("scope failure exceeded retry bound", e, attempts, state)
			}
		})
	}
	t.Run("concurrent-processed-fenced", func(t *testing.T) {
		admin, _, queue := eventFixture(t)
		group := rand.Text()
		service := eventContext(t, "concurrent-retry", scope.PrincipalRef{ID: "projector", Kind: "service"})
		event := envelope(service, rand.Text())
		raw, _ := distributed.Encode(event)
		store, _ := eventpg.NewStore(queue, eventPolicy())
		if e := store.Persist(context.Background(), group, event, raw, "m3"); e != nil {
			t.Fatal(e)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		m := manager(t, &postgres.Resource{Pool: queue})
		first, _ := inbox.NewWorker(store, m, group, func(context.Context, inbox.Candidate) (context.Context, error) {
			close(entered)
			<-release
			return nil, scope.ErrDenied
		}, func(context.Context, distributed.Envelope) error { t.Fatal("unauthorized handler"); return nil }, 2, time.Hour)
		result := make(chan error, 1)
		go func() { _, e := first.Process(context.Background()); result <- e }()
		<-entered
		second, _ := inbox.NewWorker(store, m, group, func(context.Context, inbox.Candidate) (context.Context, error) { return service, nil }, func(c context.Context, e distributed.Envelope) error { return insert(c, e.ID) }, 2, time.Hour)
		ok, e := second.Process(context.Background())
		close(release)
		firstErr := <-result
		if e != nil || !ok || !errors.Is(firstErr, scope.ErrDenied) {
			t.Fatal(e, ok, firstErr)
		}
		var state string
		var attempts int
		if e = admin.QueryRow(context.Background(), `SELECT state,attempts FROM pangolin_events.inbox WHERE consumer_group=$1 AND event_id=$2`, group, event.ID).Scan(&state, &attempts); e != nil || state != "processed" || attempts != 0 || count(t, admin, event.ID) != 1 {
			t.Fatal("late rejection overwrote completed work", e, state, attempts)
		}
	})
}

func TestExecutionAdmissionRevocation(t *testing.T) {
	for _, revoked := range []string{"permission", "delegation"} {
		t.Run(revoked, func(t *testing.T) {
			admin, app, _ := eventFixture(t)
			tenant := "execution-race"
			principal := scope.PrincipalRef{ID: "service", Kind: "service"}
			identity := func(context.Context) (scope.PrincipalRef, string, bool) { return principal, tenant, true }
			a := &eventAuthority{kind: "org.organization", revision: "before"}
			registry, _ := scope.NewRegistry(identity, a)
			ref := scope.ScopeRef{TenantID: tenant, Kind: a.kind, ID: "org"}
			checked, release := make(chan struct{}), make(chan struct{})
			revokedAtAdmission := false
			execution := distributed.Execution{Manager: manager(t, &postgres.Resource{Pool: app}), Identity: identity, Resolver: registry, TrustedProducer: func(context.Context, string) error { return nil }}
			execution.Authorize = func(c context.Context, _ string) error {
				if revoked == "permission" {
					if _, active := execution.Manager.Current(c); !active {
						close(checked)
						<-release
					} else {
						b, _ := scope.Current(c)
						if b.AuthorityRevision == "after" {
							return scope.ErrDenied
						}
					}
				}
				return nil
			}
			execution.Delegation = func(c context.Context, _ scope.PrincipalRef) error {
				if revoked == "delegation" {
					if _, active := execution.Manager.Current(c); !active {
						close(checked)
						<-release
					} else if revokedAtAdmission {
						return scope.ErrDenied
					}
				}
				return nil
			}
			task := distributed.Task{Scope: ref, Producer: "trusted", Permission: "facts.project", DelegatedPrincipal: &scope.PrincipalRef{ID: "delegated-user", Kind: "user"}}
			id := rand.Text()
			calls := 0
			result := make(chan error, 1)
			go func() {
				result <- execution.Run(context.Background(), task, func(c context.Context) error { calls++; return insert(c, id) })
			}()
			<-checked
			a.revision = "after"
			revokedAtAdmission = true
			close(release)
			err := <-result
			if !errors.Is(err, scope.ErrDenied) || calls != 0 || count(t, admin, id) != 0 {
				t.Fatalf("revocation after entry escaped admission: error=%v calls=%d", err, calls)
			}
		})
	}
}
