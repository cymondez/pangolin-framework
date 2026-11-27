package signalr

// A103/redaction: failure logs, traces and metric labels around the realtime
// adapter must never capture the bearer credential or notification payload
// values. Scope identifiers remain bounded by the shared observability
// mechanism; the secrets are absent everywhere.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/observability"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
)

func revokedListener(t *testing.T) *realtime.Hub {
	t.Helper()
	authority := &demoAuthority{revoked: true}
	auth, err := identity.NewPipeline(authority)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := tenancy.NewResolver(identity.Identity, authority)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: authority,
		Permission: func(context.Context, scope.ScopeBinding, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	hub, err := realtime.New(realtime.Options{
		Producer: "test-realtime", Connect: realtime.Access{Permission: "connect"},
		Channels: []realtime.Channel{{Name: "updates", Target: "Notification", Kinds: []string{"tenant"},
			Publish: realtime.Access{Permission: "publish"}, Subscribe: realtime.Access{Permission: "subscribe"},
			Receive: realtime.Access{Permission: "receive"}}},
		MaxConnections: 8, MaxSubscriptions: 4, QueueSize: 4, MaxPayload: 1024, MaxOperations: 4,
		OperationTimeout: time.Second,
	}, auth, resolver, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	return hub
}

var realtimeOperations = []string{"realtime_staged", "realtime_broker_accepted", "realtime_node_processed",
	"realtime_queue_full", "realtime_socket_written", "inbox_duplicate"}

func TestA103Redaction(t *testing.T) {
	const token = "secret-realtime-bearer-9812"
	const payloadSecret = "payload-secret-42"
	endpoint, err := New(revokedListener(t), Options{
		Path: "/rt", Origins: []string{"https://portal.example"}, AllowLoopbackCredentials: true,
		NegotiationLimit: 8, HandshakeConcurrency: 4, NegotiationTTL: 10 * time.Second,
		HandshakeTimeout: 2 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 2 * time.Second,
		KeepAlive: demoKeepAlive, MaxMessage: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	processor := tracetest.NewSpanRecorder()
	tracing := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	telemetry, err := observability.New(tracing, meters, &logs, "realtime-m7")
	if err != nil {
		t.Fatal(err)
	}
	handler := telemetry.HTTP(endpoint)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	request := httptest.NewRequest(http.MethodPost, server.URL+"/rt/negotiate?negotiateVersion=1", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("credential failure must be an authentication status, got %d", recorder.Code)
	}

	// The realtime counters fire while sensitive values sit in the context
	// path; the metrics must keep fixed-cardinality keys only.
	ctx := telemetry.Context(context.Background())
	for _, operation := range realtimeOperations {
		observability.Record(ctx, operation)
	}
	hub := revokedListener(t)
	_ = hub.AcceptEvent(ctx, distributed.Envelope{
		Type: realtime.EventType, SchemaVersion: 1, Producer: "test-realtime", ID: "redaction",
		TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha",
		Payload: json.RawMessage(`{"channel":"updates","data":"` + payloadSecret + `"}`),
	})

	forbidden := []string{token, payloadSecret}
	assertSecretFree := func(what, value string) {
		t.Helper()
		for _, secret := range forbidden {
			if strings.Contains(value, secret) {
				t.Fatalf("%s captured %s", what, secret)
			}
		}
	}

	assertSecretFree("negotiate response body", recorder.Body.String())
	collected := metricdata.ResourceMetrics{}
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			assertSecretFree("metric", metric.Name)
			counted, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range counted.DataPoints {
				seen++
				assertSecretFree("metric attribute value", point.Attributes.String())
			}
		}
	}
	if seen == 0 {
		t.Fatal("no realtime metric points were recorded")
	}
	assertSecretFree("operation log", logs.String())
	if !strings.Contains(logs.String(), `"operation":"http"`) {
		t.Fatalf("http operation log missing: %s", logs.String())
	}
	for _, span := range processor.Ended() {
		assertSecretFree("span", span.Name())
		for _, attribute := range span.Attributes() {
			assertSecretFree("span attribute", fmt.Sprintf("%s=%s", attribute.Key, attribute.Value.Emit()))
		}
	}
}
