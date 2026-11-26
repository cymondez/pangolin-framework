//go:build integration

package m4

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	broker "github.com/cymondez/pangolin-framework/events/nats"
	"github.com/cymondez/pangolin-framework/events/outbox"
	eventpg "github.com/cymondez/pangolin-framework/events/postgres"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/platform/audit"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/nats-io/nats.go"
)

func barrier(t *testing.T, action string) {
	t.Helper()
	directory := os.Getenv("PANGOLIN_M3_BROKER_RESTART_DIR")
	if directory == "" {
		t.Fatal("host broker fixture required")
	}
	id := rand.Text()
	must(t, os.WriteFile(filepath.Join(directory, id+".request"), []byte(action), 0600))
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(filepath.Join(directory, id+".done"))
		if err == nil {
			if string(raw) != "ok" {
				t.Fatal("broker fixture failed")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("broker fixture timeout")
}
func natsFixture(t *testing.T) (*broker.Broker, string, string) {
	t.Helper()
	id := rand.Text()
	stream, subject := "M4_"+id, "m4."+id
	client, err := broker.Connect(os.Getenv("PANGOLIN_M1_NATS_URL"), stream, subject)
	must(t, err)
	must(t, client.Provision(context.Background()))
	t.Cleanup(client.Close)
	t.Cleanup(func() {
		nc, err := nats.Connect(os.Getenv("PANGOLIN_M1_NATS_URL"))
		if err != nil {
			t.Error(err)
			return
		}
		defer nc.Close()
		js, err := nc.JetStream()
		if err == nil {
			err = js.DeleteStream(stream)
		}
		if err != nil {
			t.Error(err)
		}
	})
	return client, stream, subject
}
func envelope(ref scope.ScopeRef) distributed.Envelope {
	return distributed.Envelope{ID: rand.Text(), Type: "m4.fact", SchemaVersion: 1, OccurredAt: time.Now(), TenantID: ref.TenantID, ScopeKind: ref.Kind, ScopeID: ref.ID, CorrelationID: rand.Text(), Producer: "m4-service", Payload: json.RawMessage(`{"value":"private-secret"}`)}
}
func eventManager(t *testing.T, f *fixture, store *eventpg.Store) (uow.UnitOfWorkManager, *outbox.Publisher) {
	t.Helper()
	registry, err := distributed.NewRegistry(distributed.Schema{Type: "m4.fact", Version: 1, Required: []string{"value"}})
	must(t, err)
	publisher, err := outbox.NewPublisher(registry, store)
	must(t, err)
	manager, err := uow.NewManager(&eventpg.Resource{Base: &postgres.Resource{Pool: f.app}, Publisher: publisher}, uow.DefaultOptions{DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	must(t, err)
	return manager, publisher
}
func TestA63Resilience(t *testing.T) {
	f := newFixture(t, nil)
	began := time.Now()
	var curve []map[string]any
	ctx := context.Background()
	p := outbox.Policy{Lease: time.Second, SendTimeout: 100 * time.Millisecond, RetryInitial: time.Millisecond, RetryMax: 5 * time.Millisecond, MaxAttempts: 3}
	store, err := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 4, Inbox: 4})
	must(t, err)
	provisionQueue(t, f, "outbox", 4, "resize")
	provisionQueue(t, f, "inbox", 4, "resize")
	sample := func(phase string) {
		st, e := store.Stats(ctx, "outbox")
		must(t, e)
		var attempts int
		must(t, f.admin.QueryRow(ctx, `SELECT COALESCE(sum(attempts),0) FROM pangolin_events.outbox`).Scan(&attempts))
		curve = append(curve, map[string]any{"phase": phase, "elapsed_ms": time.Since(began).Milliseconds(), "pending": st.Pending, "processing": st.Processing, "dead": st.Dead, "oldest_ms": st.Oldest.Milliseconds(), "attempts": attempts})
	}
	manager, publisher := eventManager(t, f, store)
	var events []distributed.Envelope
	for i := 0; i < 4; i++ {
		tenant := "tenant-a"
		if i%2 == 1 {
			tenant = "tenant-b"
		}
		bound := f.bind(t, ctx, tenant, scope.PrincipalRef{Kind: "service", ID: "producer"})
		b, _ := scope.Current(bound)
		e := envelope(b.Ref)
		events = append(events, e)
		must(t, manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, e) }))
	}
	client, stream, subject := natsFixture(t)
	worker, err := outbox.NewWorker(store, client, p, "outbox", func(c context.Context, e distributed.Envelope) error {
		_, err := f.resolve(c, scope.ScopeRef{TenantID: e.TenantID, Kind: e.ScopeKind, ID: e.ScopeID}, scope.PrincipalRef{Kind: "service", ID: "producer"})
		return err
	})
	must(t, err)
	stopped := false
	t.Cleanup(func() {
		if stopped {
			barrier(t, "START_NATS")
		}
	})
	t.Run("broker-offline", func(t *testing.T) {
		barrier(t, "STOP_NATS")
		stopped = true
		ok, err := worker.Dispatch(ctx)
		if ok || err == nil {
			t.Fatal("offline broker confirmed")
		}
		st, err := store.Stats(ctx, "outbox")
		must(t, err)
		if st.Pending+st.Processing != 4 {
			t.Fatal(st)
		}
		sample("offline")
	})
	t.Run("capacity-limit", func(t *testing.T) {
		bound := f.bind(t, ctx, "tenant-a", scope.PrincipalRef{Kind: "service", ID: "producer"})
		b, _ := scope.Current(bound)
		err := manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, envelope(b.Ref)) })
		if !errors.Is(err, eventpg.ErrCapacity) {
			t.Fatal(err)
		}
	})
	t.Run("retry-limit", func(t *testing.T) {
		for i := 0; i < 12; i++ {
			_, err := f.admin.Exec(ctx, `UPDATE pangolin_events.outbox SET next_attempt_at=now()-interval '1 second'`)
			must(t, err)
			_, _ = worker.Dispatch(ctx)
			sample("retry")
		}
		st, err := store.Stats(ctx, "outbox")
		must(t, err)
		if st.Dead != 4 || st.Pending+st.Processing != 0 {
			t.Fatal(st)
		}
	})
	t.Run("dead-replay", func(t *testing.T) {
		replayManager, err := uow.NewManager(&postgres.Resource{Pool: f.queue}, uow.DefaultOptions{DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
		must(t, err)
		maint, err := eventpg.NewMaintenance(store, func(context.Context, eventpg.Replay) error { return nil }, func(c context.Context, r eventpg.Replay) error {
			return (audit.Recorder{}).Record(c, audit.Record{Action: "events.replay", Resource: r.EventID, Result: "accepted", Trace: "trace-recovery"})
		})
		must(t, err)
		for _, e := range events {
			bound := f.bind(t, ctx, e.TenantID, scope.PrincipalRef{Kind: "user", ID: "operator"})
			must(t, replayManager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return maint.ReplayDead(c, "outbox", "", e.ID) }))
		}
		var count int
		must(t, f.admin.QueryRow(ctx, `SELECT count(*) FROM pangolin_audit.records WHERE action='events.replay'`).Scan(&count))
		if count != 4 {
			t.Fatal(count)
		}
	})
	t.Run("recovery-drain", func(t *testing.T) {
		started := time.Now()
		barrier(t, "START_NATS")
		stopped = false
		recovered, err := broker.Connect(os.Getenv("PANGOLIN_M1_NATS_URL"), stream, subject)
		must(t, err)
		defer recovered.Close()
		worker, err = outbox.NewWorker(store, recovered, p, "recovered", func(context.Context, distributed.Envelope) error { return nil })
		must(t, err)
		for range events {
			ok, err := worker.Dispatch(ctx)
			must(t, err)
			if !ok {
				t.Fatal("not drained")
			}
		}
		st, err := store.Stats(ctx, "outbox")
		must(t, err)
		if st.Pending+st.Processing+st.Dead != 0 {
			t.Fatal(st)
		}
		var sent int
		must(t, f.admin.QueryRow(ctx, `SELECT count(*) FROM pangolin_events.outbox WHERE state='sent'`).Scan(&sent))
		if sent != 4 {
			t.Fatal(sent)
		}
		t.Logf("recovery_rto_ms=%d reconciled=%d", time.Since(started).Milliseconds(), sent)
		sample("recovered")
		writeEvidence(t, "resilience.json", map[string]any{"sent": sent, "lost": 0, "recovery_rto_ms": time.Since(started).Milliseconds(), "curve": curve})
	})
	t.Run("tenant-load-competition", func(t *testing.T) {
		var owners int
		must(t, f.admin.QueryRow(ctx, `SELECT count(DISTINCT tenant_id) FROM pangolin_events.outbox WHERE state='sent'`).Scan(&owners))
		if owners != 2 {
			t.Fatal("tenant lost work", owners)
		}
	})
}
