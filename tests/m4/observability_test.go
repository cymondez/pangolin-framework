//go:build integration

package m4

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/inbox"
	"github.com/cymondez/pangolin-framework/events/outbox"
	eventpg "github.com/cymondez/pangolin-framework/events/postgres"
	"github.com/cymondez/pangolin-framework/observability"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestA66TraceCorrelation(t *testing.T) {
	t.Run("trace-correlation", func(t *testing.T) {
		f := newFixture(t, nil)
		ctx := context.Background()
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		defer tp.Shutdown(ctx)
		mp := metric.NewMeterProvider()
		defer mp.Shutdown(ctx)
		logs := &bytes.Buffer{}
		tm, err := observability.New(tp, mp, logs, "m4")
		must(t, err)
		conf := f.app.Config().Copy()
		conf.ConnConfig.Tracer = tm
		traced, err := pgxpool.NewWithConfig(ctx, conf)
		must(t, err)
		t.Cleanup(traced.Close)
		f.app = traced
		p := outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 3}
		store, err := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 10, Inbox: 10})
		must(t, err)
		manager, publisher := eventManager(t, f, store)
		var fact distributed.Envelope
		var traceID trace.TraceID
		handler := tm.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bound := f.bind(t, r.Context(), f.tenant, scope.PrincipalRef{Kind: "service", ID: "producer"})
			b, _ := scope.Current(bound)
			fact = envelope(b.Ref)
			fact.CorrelationID = observability.OperationID(bound)
			traceID = trace.SpanContextFromContext(bound).TraceID()
			bound, end := observability.Start(bound, "use_case")
			err := manager.Within(bound, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return publisher.Publish(c, fact) })
			end(err)
			must(t, err)
			w.WriteHeader(201)
		}))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("POST", "/books?secret=private-secret", strings.NewReader("private-secret")))
		if response.Code != 201 {
			t.Fatal(response.Code)
		}
		client, _, _ := natsFixture(t)
		worker, err := outbox.NewWorker(store, client, p, "dispatcher", func(context.Context, distributed.Envelope) error { return nil })
		must(t, err)
		ok, err := worker.Dispatch(tm.Context(ctx))
		must(t, err)
		if !ok {
			t.Fatal("not dispatched")
		}
		subscription, err := client.Subscribe("m4-trace")
		must(t, err)
		receive, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		delivery, err := subscription.Next(receive)
		must(t, err)
		if delivery.Envelope.TraceParent == "" || delivery.Envelope.ID != fact.ID {
			t.Fatal("correlation not persisted")
		}
		registry, err := distributed.NewRegistry(distributed.Schema{Type: "m4.fact", Version: 1, Required: []string{"value"}})
		must(t, err)
		receiver, err := inbox.NewReceiver(store, registry, "trace", func(context.Context, distributed.Envelope) (string, error) { return "m4-service", nil }, func(context.Context, error) {})
		must(t, err)
		must(t, receiver.Receive(ctx, delivery.Envelope, delivery.Ack))
		consumerManager, err := uow.NewManager(&postgres.Resource{Pool: f.queue}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
		must(t, err)
		consumer, err := inbox.NewWorker(store, consumerManager, "trace", func(c context.Context, can inbox.Candidate) (context.Context, error) {
			return f.resolve(c, scope.ScopeRef{TenantID: can.Envelope.TenantID, Kind: can.Envelope.ScopeKind, ID: can.Envelope.ScopeID}, scope.PrincipalRef{Kind: "service", ID: "producer"})
		}, func(c context.Context, e distributed.Envelope) error {
			ex, err := postgres.Current(c, true)
			if err != nil {
				return err
			}
			_, err = ex.Exec(c, `INSERT INTO pangolin.job_effects(id) VALUES($1)`, e.ID)
			return err
		}, 3, time.Millisecond)
		must(t, err)
		ok, err = consumer.Process(tm.Context(ctx))
		must(t, err)
		if !ok {
			t.Fatal("not consumed")
		}
		names := map[string]bool{}
		for _, span := range rec.Ended() {
			if span.SpanContext().TraceID() == traceID {
				names[span.Name()] = true
			}
		}
		for _, name := range []string{"http", "use_case", "sql", "uow", "outbox", "consumer"} {
			if !names[name] {
				t.Errorf("missing correlated %s span", name)
			}
		}
		if strings.Contains(logs.String(), "private-secret") || strings.Contains(logs.String(), "INSERT") || !strings.Contains(logs.String(), fact.CorrelationID) {
			t.Fatal(logs.String())
		}
	})
}
