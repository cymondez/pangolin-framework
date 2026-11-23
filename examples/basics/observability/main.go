package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"time"

	"github.com/cymondez/pangolin-framework/observability"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	traces := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1))))
	meters := metric.NewMeterProvider()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = traces.Shutdown(ctx)
		_ = meters.Shutdown(ctx)
	}()
	var logs bytes.Buffer
	telemetry, err := observability.New(traces, meters, &logs, "my-app")
	if err != nil {
		panic(err)
	}
	ctx := telemetry.Context(observability.WithOperation(context.Background(), "demo-operation"))
	ctx, done := observability.Start(ctx, "use_case")
	continued := observability.Restore(context.Background(), observability.TraceParent(ctx))
	same := trace.SpanContextFromContext(ctx).TraceID() == trace.SpanContextFromContext(continued).TraceID()
	done(nil)
	fmt.Println("Trace correlated:", same)
	health := observability.Health{Ready: func() bool { return true }, Timeout: time.Second}
	for _, path := range []string{"/livez", "/readyz"} {
		result := httptest.NewRecorder()
		telemetry.HTTP(health).ServeHTTP(result, httptest.NewRequest("GET", path, nil))
		fmt.Printf("%s: %d\n", path, result.Code)
	}
	fmt.Println("JSON log recorded:", bytes.Contains(logs.Bytes(), []byte("demo-operation")))
}
