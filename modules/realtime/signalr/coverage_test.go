package signalr

// Coverage tests for endpoint admission errors and lifecycle drains, see the
// RT-002 mapping in tests/contract/m7.json.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type stubGateway struct {
	authenticateErr error
	openErr         error
	drainErr        error
}

func (g *stubGateway) Authenticate(context.Context, identity.Credential) (realtime.Subject, error) {
	return realtime.Subject{Tenant: "alpha"}, g.authenticateErr
}

func (g *stubGateway) Open(context.Context, identity.Credential, realtime.Subject) (*realtime.Session, error) {
	return nil, g.openErr
}

func (*stubGateway) ConnectionLimit() int { return 4 }
func (*stubGateway) Quiesce(context.Context) error {
	return nil
}
func (g *stubGateway) Drain(context.Context) error { return g.drainErr }
func (*stubGateway) Cancel()                       {}

func stubOptions() Options {
	return Options{Path: "/rt", NegotiationLimit: 4, HandshakeConcurrency: 4,
		NegotiationTTL: time.Second, HandshakeTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second,
		WriteTimeout: time.Second, KeepAlive: time.Second, MaxMessage: 1 << 10}
}

func serveProbe(endpoint *Endpoint, path, actor string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://example.com"+path, nil)
	request.Header.Set("Authorization", "Bearer "+actor)
	request.Header.Set("Origin", "http://example.com")
	recorder := httptest.NewRecorder()
	endpoint.ServeHTTP(recorder, request)
	return recorder
}

func testEndpointWithMaxMessage(t *testing.T, maxMessage int) (base string) {
	t.Helper()
	options := Options{Path: "/rt", Origins: []string{"https://portal.example"}, AllowLoopbackCredentials: true,
		NegotiationLimit: 8, HandshakeConcurrency: 4, NegotiationTTL: 30 * time.Second,
		HandshakeTimeout: 2 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 2 * time.Second,
		KeepAlive: demoKeepAlive, MaxMessage: maxMessage}
	endpoint, err := New(testListener(t), options)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	return server.URL
}

func TestNegotiateMapsAdmissionErrors(t *testing.T) {
	cases := map[string]struct {
		authenticate error
		status       int
	}{
		"denied":      {scope.ErrDenied, http.StatusForbidden},
		"capacity":    {realtime.ErrCapacity, http.StatusTooManyRequests},
		"unavailable": {realtime.ErrClosed, http.StatusServiceUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			endpoint, err := New(&stubGateway{authenticateErr: tc.authenticate}, stubOptions())
			if err != nil {
				t.Fatal(err)
			}
			recorder := serveProbe(endpoint, "/rt/negotiate?negotiateVersion=1", "alpha")
			if recorder.Code != tc.status {
				t.Fatalf("expected status %d, got %d", tc.status, recorder.Code)
			}
		})
	}
}

func TestNegotiationLimitAndLifecycleDrains(t *testing.T) {
	gateway := &stubGateway{}
	endpoint, err := New(gateway, Options{Path: "/rt", NegotiationLimit: 1, HandshakeConcurrency: 4,
		NegotiationTTL: time.Second, HandshakeTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second,
		WriteTimeout: time.Second, KeepAlive: time.Second, MaxMessage: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	recorder := serveProbe(endpoint, "/rt/negotiate?negotiateVersion=1", "alpha")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected first negotiation to admit, got %d", recorder.Code)
	}
	recorder = serveProbe(endpoint, "/rt/negotiate?negotiateVersion=1", "alpha")
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected negotiation limit rejection 429, got %d", recorder.Code)
	}
	if err := endpoint.Quiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
	recorder = serveProbe(endpoint, "/rt/negotiate?negotiateVersion=1", "alpha")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("stopped endpoint admitted negotiation with %d", recorder.Code)
	}
	if err := endpoint.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	endpoint.Cancel()
}

// A write over the configured maximum message size must be rejected before
// any frame is framed or sent.
func TestWriteRejectsOverCapacityFrame(t *testing.T) {
	options := stubOptions()
	options.MaxMessage = 16
	endpoint, err := New(&stubGateway{}, options)
	if err != nil {
		t.Fatal(err)
	}
	// A framed reply over the maximum is refused before any frame is sent.
	if err := endpoint.write(nil, context.Background(), []byte(strings.Repeat("x", 64))); !errors.Is(err, realtime.ErrCapacity) {
		t.Fatalf("oversize write accepted: %v", err)
	}
}

// A peer (or completion channel) that cannot consume responses must not
// turn them into waiting goroutines: sendCompletion refuses to block.
func TestSendCompletionNeverBlocks(t *testing.T) {
	out := make(chan completion, 1)
	out <- completion{InvocationID: "stale"}
	if sendCompletion(context.Background(), out, completion{InvocationID: "next"}) {
		t.Fatal("full completion channel blocked the sender")
	}
	// A completed operation cannot wait on a channel nobody drains anymore:
	// with a full channel and a cancelled session the arm must be false.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sendCompletion(ctx, out, completion{InvocationID: "closed"}) {
		t.Fatal("cancelled operation completion accepted")
	}
	// A ready channel still wins the select when the context is alive.
	free := make(chan completion, 4)
	if !sendCompletion(context.Background(), free, completion{InvocationID: "open"}) {
		t.Fatal("healthy completion refused")
	}
}

func TestHandshakeOverCapacityDropsConnection(t *testing.T) {
	base := testEndpointWithMaxMessage(t, 16)
	_, parsed := negotiateRequest(t, base, "alpha", "", "1")
	conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
	if conn == nil {
		t.Fatal("upgrade rejected")
	}
	defer conn.CloseNow()
	writeMessage(t, conn, `{"protocol":"messagepack","version":1}`)
	readCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if _, _, err := conn.Read(readCtx); err == nil {
		t.Fatal("oversize handshake reply was accepted by the client")
	}
}
