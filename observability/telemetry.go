// Package observability supplies explicitly injected OpenTelemetry instrumentation.
package observability

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var ErrConfig = errors.New("OBSERVABILITY_CONFIG_INVALID")
var safeID = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,160}$`)

type contextKey struct{}
type operationKey struct{}
type messageKey struct{}
type message struct{ kind, id string }
type Telemetry struct {
	tracer   trace.Tracer
	count    metric.Int64Counter
	latency  metric.Float64Histogram
	gauge    metric.Int64Gauge
	queueAge metric.Float64Gauge
	logger   *slog.Logger
	module   string
}

// The application owns SDK providers, exporters, sampling, retention and shutdown.
// Only fixed operation/result names are metric labels; owner IDs are never labels.
func New(tp trace.TracerProvider, mp metric.MeterProvider, writer io.Writer, module string) (*Telemetry, error) {
	if tp == nil || mp == nil || writer == nil || !safeID.MatchString(module) {
		return nil, ErrConfig
	}
	meter := mp.Meter("pangolin-framework")
	count, e1 := meter.Int64Counter("pangolin.operations")
	latency, e2 := meter.Float64Histogram("pangolin.duration", metric.WithUnit("s"))
	gauge, e3 := meter.Int64Gauge("pangolin.queue")
	queueAge, e4 := meter.Float64Gauge("pangolin.queue.oldest", metric.WithUnit("s"))
	if err := errors.Join(e1, e2, e3, e4); err != nil {
		return nil, err
	}
	return &Telemetry{tp.Tracer("pangolin-framework"), count, latency, gauge, queueAge, slog.New(slog.NewJSONHandler(writer, nil)), module}, nil
}
func WithMessage(ctx context.Context, kind, id string) context.Context {
	if (kind == "event" || kind == "job") && safeID.MatchString(id) {
		return context.WithValue(ctx, messageKey{}, message{kind, id})
	}
	return ctx
}
func Record(ctx context.Context, operation string) {
	defer func() { _ = recover() }()
	t, _ := ctx.Value(contextKey{}).(*Telemetry)
	if t != nil && operation == "inbox_duplicate" {
		t.count.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", operation)))
	}
}
func (t *Telemetry) Pool(ctx context.Context, acquired, maximum int32) {
	defer func() { _ = recover() }()
	t.gauge.Record(ctx, int64(acquired), metric.WithAttributes(attribute.String("queue", "database.pool"), attribute.String("state", "acquired")))
	t.gauge.Record(ctx, int64(maximum), metric.WithAttributes(attribute.String("queue", "database.pool"), attribute.String("state", "maximum")))
}
func (t *Telemetry) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, t)
}
func OperationID(ctx context.Context) string { id, _ := ctx.Value(operationKey{}).(string); return id }
func WithOperation(ctx context.Context, id string) context.Context {
	if !safeID.MatchString(id) {
		id = rand.Text()
	}
	return context.WithValue(ctx, operationKey{}, id)
}
func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}
func Restore(ctx context.Context, parent string) context.Context {
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": parent})
}

// Start allows only a finite instrumentation vocabulary and never records error text.
func Start(ctx context.Context, operation string) (context.Context, func(error)) {
	t, _ := ctx.Value(contextKey{}).(*Telemetry)
	if t == nil {
		return ctx, func(error) {}
	}
	switch operation {
	case "http", "use_case", "sql", "uow", "outbox", "consumer", "job":
	default:
		operation = "use_case"
	}
	started := time.Now()
	ctx, span := t.tracer.Start(ctx, operation)
	return ctx, func(err error) {
		defer func() { _ = recover() }() // Telemetry cannot rewrite a confirmed business outcome.
		result := "succeeded"
		if err != nil {
			result = "failed"
			span.SetStatus(codes.Error, "OPERATION_FAILED")
		}
		attrs := []attribute.KeyValue{attribute.String("module", t.module), attribute.String("operation", operation), attribute.String("result", result)}
		t.count.Add(ctx, 1, metric.WithAttributes(attrs...))
		t.latency.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
		fields := []any{"module", t.module, "code", result, "operation", operation, "trace_id", span.SpanContext().TraceID().String(), "operation_id", OperationID(ctx)}
		if msg, ok := ctx.Value(messageKey{}).(message); ok {
			fields = append(fields, msg.kind+"_id", msg.id)
			span.SetAttributes(attribute.String(msg.kind+"_id", msg.id))
		}
		if b, ok := scope.Current(ctx); ok {
			for name, value := range map[string]string{"tenant_id": b.Ref.TenantID, "scope_kind": b.Ref.Kind, "scope_id": b.Ref.ID, "principal_id": b.Principal.ID, "authority_revision": b.AuthorityRevision} {
				if safeID.MatchString(value) {
					fields = append(fields, name, value)
					span.SetAttributes(attribute.String(name, value))
				}
			}
		}
		t.logger.InfoContext(ctx, "operation completed", fields...)
		span.End()
	}
}
func (t *Telemetry) Job(ctx context.Context, result string, duration time.Duration) {
	defer func() { _ = recover() }()
	if result != "succeeded" {
		result = "failed"
	}
	t.count.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", "job"), attribute.String("result", result)))
	t.latency.Record(ctx, duration.Seconds(), metric.WithAttributes(attribute.String("operation", "job")))
}

// Queue records global aggregate values and emits fixed-cardinality alerts.
func (t *Telemetry) Queue(ctx context.Context, name string, pending, dead int64, oldest time.Duration, limit int64, age time.Duration) bool {
	defer func() { _ = recover() }()
	switch name {
	case "outbox", "inbox", "jobs":
	default:
		return false
	}
	t.gauge.Record(ctx, pending, metric.WithAttributes(attribute.String("queue", name), attribute.String("state", "pending")))
	t.gauge.Record(ctx, dead, metric.WithAttributes(attribute.String("queue", name), attribute.String("state", "dead")))
	t.queueAge.Record(ctx, oldest.Seconds(), metric.WithAttributes(attribute.String("queue", name)))
	alert := pending >= limit || oldest >= age || dead > 0
	if alert {
		t.logger.WarnContext(ctx, "queue alert", "module", t.module, "code", "QUEUE_PRESSURE", "queue", name, "pending", pending, "dead", dead, "oldest_seconds", oldest.Seconds())
	}
	return alert
}

type response struct {
	http.ResponseWriter
	status int
}

func (w *response) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *response) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (t *Telemetry) HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := t.Context(Restore(r.Context(), r.Header.Get("traceparent")))
		ctx = WithOperation(ctx, rand.Text())
		ctx, finish := Start(ctx, "http")
		rw := &response{ResponseWriter: w}
		rw.Header().Set("X-Request-ID", OperationID(ctx))
		defer func() {
			if p := recover(); p != nil {
				finish(errors.New("HTTP_PANIC"))
				panic(p)
			}
			var err error
			if rw.status >= 500 {
				err = errors.New("HTTP_FAILED")
			}
			finish(err)
		}()
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}

// Health is dependency-injected. Liveness never calls downstream services.
type Health struct {
	Ready    func() bool
	Required []func(context.Context) error
	Timeout  time.Duration
}

func (h Health) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/livez" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path != "/readyz" {
		http.NotFound(w, r)
		return
	}
	if h.Ready == nil || !h.Ready() || h.Timeout <= 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	for _, check := range h.Required {
		if check == nil || check(ctx) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
func (t *Telemetry) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	ctx, finish := Start(t.Context(ctx), "sql")
	return context.WithValue(ctx, queryKey{}, finish)
}

type queryKey struct{}

func (t *Telemetry) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if finish, ok := ctx.Value(queryKey{}).(func(error)); ok {
		finish(data.Err)
	}
}
