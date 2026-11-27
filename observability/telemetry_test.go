package observability

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func telemetry(t *testing.T) (*Telemetry, *bytes.Buffer, *tracetest.SpanRecorder, *metric.ManualReader) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()); _ = mp.Shutdown(context.Background()) })
	out := &bytes.Buffer{}
	tm, err := New(tp, mp, out, "test")
	if err != nil {
		t.Fatal(err)
	}
	return tm, out, rec, reader
}
func TestA66Observability(t *testing.T) {
	t.Run("liveness", func(t *testing.T) {
		called := false
		h := Health{Ready: func() bool { return false }, Required: []func(context.Context) error{func(context.Context) error { called = true; return errors.New("down") }}, Timeout: time.Second}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/livez", nil))
		if w.Code != 204 || called {
			t.Fatal(w.Code, called)
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))
		if w.Code != 404 {
			t.Fatal(w.Code)
		}
	})
	t.Run("required-dependency-unready", func(t *testing.T) {
		for _, h := range []Health{{}, {Ready: func() bool { return false }, Timeout: time.Second}, {Ready: func() bool { return true }, Timeout: time.Second, Required: []func(context.Context) error{nil}}, {Ready: func() bool { return true }, Timeout: time.Second, Required: []func(context.Context) error{func(context.Context) error { return errors.New("down") }}}} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
			if w.Code != 503 {
				t.Fatal(w.Code)
			}
		}
	})
	t.Run("optional-broker-outage", func(t *testing.T) {
		h := Health{Ready: func() bool { return true }, Timeout: time.Second, Required: []func(context.Context) error{func(context.Context) error { return nil }}}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
		if w.Code != 204 {
			t.Fatal(w.Code)
		}
	})
	t.Run("alerting", func(t *testing.T) {
		tm, out, _, reader := telemetry(t)
		ctx := context.Background()
		if !tm.Queue(ctx, "outbox", 20, 1, time.Minute, 10, time.Second) || tm.Queue(ctx, "other", 100, 100, time.Hour, 1, time.Second) || tm.Queue(ctx, "jobs", 0, 0, 0, 1, time.Second) {
			t.Fatal("alert threshold")
		}
		if !strings.Contains(out.String(), "QUEUE_PRESSURE") {
			t.Fatal(out.String())
		}
		var data metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &data); err != nil || len(data.ScopeMetrics) == 0 {
			t.Fatal(err)
		}
		tm.Job(ctx, "bad", time.Second)
		tm.Job(ctx, "succeeded", time.Second)
	})
	t.Run("redaction", func(t *testing.T) {
		tm, out, rec, _ := telemetry(t)
		ctx := tm.Context(WithOperation(context.Background(), "operation-1"))
		ctx, end := Start(ctx, "sql")
		end(errors.New("password=secret SQL select private_payload"))
		if strings.Contains(out.String(), "password") || strings.Contains(out.String(), "private_payload") || !strings.Contains(out.String(), "operation-1") {
			t.Fatal(out.String())
		}
		spans := rec.Ended()
		if len(spans) != 1 || strings.Contains(spans[0].Status().Description, "secret") {
			t.Fatal(spans)
		}
		q := tm.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "secret"})
		tm.TraceQueryEnd(q, nil, pgx.TraceQueryEndData{Err: errors.New("private")})
		tm.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
		if strings.Contains(out.String(), "private") {
			t.Fatal(out.String())
		}
	})
}
func TestInstrumentationEdges(t *testing.T) {
	tm, _, rec, _ := telemetry(t)
	ctx, end := Start(context.Background(), "ignored")
	end(nil)
	if len(rec.Ended()) != 0 {
		t.Fatal("implicit global tracer")
	}
	ctx = tm.Context(WithOperation(ctx, "\nsecret"))
	if OperationID(ctx) == "" {
		t.Fatal("operation ID")
	}
	ctx, finish := Start(ctx, "unknown-operation")
	parent := TraceParent(ctx)
	if parent == "" {
		t.Fatal("missing parent")
	}
	restored := Restore(context.Background(), parent)
	if trace.SpanContextFromContext(restored).TraceID() != trace.SpanContextFromContext(ctx).TraceID() {
		t.Fatal("trace propagation")
	}
	finish(nil)
	for _, status := range []int{204, 503} {
		w := httptest.NewRecorder()
		tm.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if OperationID(r.Context()) == "" {
				t.Fatal("request ID")
			}
			w.WriteHeader(status)
			w.WriteHeader(400)
			_, _ = w.Write([]byte("ok"))
		})).ServeHTTP(w, httptest.NewRequest("GET", "/private?password=secret", nil))
		if w.Code != status {
			t.Fatal(w.Code)
		}
	}
	w := httptest.NewRecorder()
	tm.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if (&response{ResponseWriter: w}).Unwrap() != w {
		t.Fatal("unwrap")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic swallowed")
			}
		}()
		tm.HTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("private") })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	}()
	if _, err := New(nil, nil, nil, ""); err == nil {
		t.Fatal("invalid dependencies")
	}
}
