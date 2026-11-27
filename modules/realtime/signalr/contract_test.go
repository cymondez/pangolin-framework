package signalr

// Named M7 contract scenarios for the SignalR adapter (RT-001, RT-002), see
// spec §13. The official @microsoft/signalr client scenarios (official-invoke,
// official-error, official-push, heartbeat, reconnect, official-negotiate end
// to end) run against two live nodes with real services in
// examples/realtime/client/integration.test.mjs; the negative fixtures here use
// raw frames an official client never produces.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
)

type demoAuthority struct {
	revoked bool
}

func (*demoAuthority) Kind() string { return identity.BearerKind }
func (a *demoAuthority) Verify(ctx context.Context, token identity.Credential) (identity.Result, error) {
	if a.revoked {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{Status: identity.StatusAuthenticated, Method: "test",
		Principal: scope.PrincipalRef{Kind: identity.PrincipalUser, ID: "same-user"}, Tenant: token.Value}, ctx.Err()
}
func (*demoAuthority) Check(context.Context, scope.PrincipalRef, string) (string, error) {
	return "current", nil
}

const demoKeepAlive = 150 * time.Millisecond

func testListener(t *testing.T) *realtime.Hub {
	t.Helper()
	a := &demoAuthority{}
	auth, err := identity.NewPipeline(a)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := tenancy.NewResolver(identity.Identity, a)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{Authority: a,
		Permission: func(context.Context, scope.ScopeBinding, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	hub, err := realtime.New(realtime.Options{
		Producer: "test-realtime", Connect: realtime.Access{Permission: "connect"},
		Methods: []realtime.Method{
			{Name: "Echo", Access: realtime.Access{Permission: "echo"},
				Invoke: func(_ context.Context, args []json.RawMessage) (any, error) {
					var value string
					if len(args) != 1 || json.Unmarshal(args[0], &value) != nil {
						return nil, realtime.ErrInvalid
					}
					return value, nil
				}},
			{Name: "Wait", Access: realtime.Access{Permission: "wait"},
				Invoke: func(ctx context.Context, _ []json.RawMessage) (any, error) {
					if _, ok := scope.Current(ctx); !ok {
						return nil, scope.ErrDenied
					}
					<-ctx.Done()
					return nil, ctx.Err()
				}},
		},
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

func testEndpoint(t *testing.T) (base string, hub *realtime.Hub) {
	t.Helper()
	hub = testListener(t)
	endpoint, err := New(hub, Options{
		Path: "/rt", Origins: []string{"https://portal.example"}, AllowLoopbackCredentials: true,
		NegotiationLimit: 8, HandshakeConcurrency: 4, NegotiationTTL: 30 * time.Second,
		HandshakeTimeout: 2 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 2 * time.Second,
		KeepAlive: demoKeepAlive, MaxMessage: 512,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	return server.URL, hub
}

type negotiation struct {
	ConnectionToken string `json:"connectionToken"`
	ConnectionID    string `json:"connectionId"`
}

// barrierGateway forces both requests of the token-consume race through a
// controlled Gateway.Open barrier: neither upgrade leaves Open before both
// have arrived, so the admission decision is deterministic.
type barrierGateway struct {
	realtime.Gateway
	mu      sync.Mutex
	count   int
	goahead chan struct{}
}

func (b *barrierGateway) Open(ctx context.Context, credential identity.Credential, subject realtime.Subject) (*realtime.Session, error) {
	b.mu.Lock()
	b.count++
	second := b.count == 2
	b.mu.Unlock()
	if second {
		close(b.goahead)
	} else {
		select {
		case <-b.goahead:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.Gateway.Open(ctx, credential, subject)
}

func negotiateRequest(t *testing.T, base, actor, origin, version string) (*http.Response, *negotiation) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, base+"/rt/negotiate?negotiateVersion="+version, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+actor)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	var parsed *negotiation
	if response.StatusCode == http.StatusOK {
		parsed = &negotiation{}
		if err := json.NewDecoder(response.Body).Decode(parsed); err != nil {
			t.Fatal(err)
		}
	}
	return response, parsed
}

func dialUpgrade(t *testing.T, base, actor, token string) (*websocket.Conn, *http.Response) {
	t.Helper()
	header := http.Header{"Authorization": {"Bearer " + actor}}
	conn, response, err := websocket.Dial(t.Context(),
		strings.Replace(base, "http://", "ws://", 1)+"/rt?id="+token, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return conn, response
	}
	return conn, response
}

func expectDialStatus(t *testing.T, base, actor, token string, want int) {
	t.Helper()
	conn, response := dialUpgrade(t, base, actor, token)
	if response == nil || response.StatusCode != want {
		t.Fatalf("expected status %d, got %v", want, response)
	}
	if conn != nil {
		conn.CloseNow()
	}
}
func writeMessage(t *testing.T, conn *websocket.Conn, raw string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(raw+"\x1e")); err != nil {
		t.Fatal(err)
	}
}

func skipKeepAlive(raw []byte) bool { return bytes.HasPrefix(raw, []byte(`{"type":6}`)) }

type completionFrame struct {
	Type         int    `json:"type"`
	InvocationID string `json:"invocationId"`
	Error        string `json:"error"`
}

func readCompletion(t *testing.T, conn *websocket.Conn, invocationID string) completionFrame {
	t.Helper()
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		_, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read completion: %v", err)
		}
		raw := bytes.TrimSuffix(data, []byte{recordSeparator})
		if skipKeepAlive(raw) {
			continue
		}
		var frame completionFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("unreadable frame %q: %v", raw, err)
		}
		if frame.Type == 3 && frame.InvocationID == invocationID {
			return frame
		}
		t.Fatalf("unexpected completion %+v", frame)
	}
}

func handshake(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	writeMessage(t, conn, `{"protocol":"json","version":1}`)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("handshake reply: %v", err)
	}
	if string(bytes.TrimSuffix(data, []byte{recordSeparator})) != "{}" {
		t.Fatalf("unexpected handshake error %q", data)
	}
}

func TestA96RT001(t *testing.T) {
	base, hub := testEndpoint(t)
	t.Run("official-negotiate", func(t *testing.T) {
		response, parsed := negotiateRequest(t, base, "alpha", "", "1")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("negotiate rejected: %d", response.StatusCode)
		}
		if parsed.ConnectionID == "" || parsed.ConnectionToken == "" || parsed.ConnectionID == parsed.ConnectionToken {
			t.Fatalf("negotiation payload incomplete: %+v", parsed)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("negotiation response was cacheable")
		}
	})
	t.Run("token-binding", func(t *testing.T) {
		const second = "beta"
		_, parsed := negotiateRequest(t, base, "alpha", "", "1")
		conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
		if conn == nil {
			t.Fatal("matching credential was rejected")
		}
		handshake(t, conn)
		conn.Close(websocket.StatusNormalClosure, "")
		// The consumed token, even with a matching credential, is single-use.
		expectDialStatus(t, base, "alpha", parsed.ConnectionToken, http.StatusNotFound)
		// A forged ticket binding from another subject cannot take over.
		_, next := negotiateRequest(t, base, "alpha", "", "1")
		expectDialStatus(t, base, second, next.ConnectionToken, http.StatusNotFound)
		// An unknown token is not admitted.
		expectDialStatus(t, base, "alpha", "not-a-token", http.StatusNotFound)
	})
	t.Run("unsupported-version", func(t *testing.T) {
		response, _ := negotiateRequest(t, base, "alpha", "", "0")
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for negotiateVersion 0, got %d", response.StatusCode)
		}
	})
	t.Run("unsupported-transport", func(t *testing.T) {
		_, parsed := negotiateRequest(t, base, "alpha", "", "1")
		conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
		if conn == nil {
			t.Fatal("upgrade rejected")
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, []byte(`{"protocol":"messagepack","version":1}`+"\x1e")); err != nil {
			t.Fatal(err)
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		reply := string(bytes.TrimSuffix(data, []byte{recordSeparator}))
		if !strings.Contains(reply, "Only SignalR JSON Hub Protocol") {
			t.Fatalf("unsupported protocol not rejected: %s", reply)
		}
	})
	t.Run("origin-rejected", func(t *testing.T) {
		response, _ := negotiateRequest(t, base, "alpha", "https://untrusted.invalid", "1")
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("untrusted origin admitted: %d", response.StatusCode)
		}
		allowed, _ := negotiateRequest(t, base, "alpha", "https://portal.example", "1")
		if allowed.StatusCode != http.StatusOK {
			t.Fatalf("explicitly trusted origin rejected: %d", allowed.StatusCode)
		}
		if got := allowed.Header.Values("Access-Control-Allow-Origin"); !slices.Contains(got, "https://portal.example") {
			t.Fatalf("missing CORS echo: %v", got)
		}
	})
	t.Run("concurrent-consume", func(t *testing.T) {
		// The same negotiation token with a matching credential must admit
		// exactly one of two simultaneous upgrades. Both WebSocket requests
		// are forced to arrive at the controlled Gateway.Open barrier before
		// either consumes the ticket.
		blocked := &barrierGateway{Gateway: hub, goahead: make(chan struct{})}
		endpoint, err := New(blocked, Options{
			Path: "/rt", AllowLoopbackCredentials: true, NegotiationLimit: 8, HandshakeConcurrency: 4,
			NegotiationTTL: 30 * time.Second, HandshakeTimeout: 2 * time.Second, ReadTimeout: 10 * time.Second,
			WriteTimeout: 2 * time.Second, KeepAlive: demoKeepAlive, MaxMessage: 512,
		})
		if err != nil {
			t.Fatal(err)
		}
		barrierServer := httptest.NewServer(endpoint)
		t.Cleanup(barrierServer.Close)
		// The ticket belongs to the barrier endpoint instance.
		response, parsed := negotiateRequest(t, barrierServer.URL, "alpha", "", "1")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("barrier negotiate rejected: %d", response.StatusCode)
		}
		type upgrade struct {
			conn   *websocket.Conn
			status int
		}
		results := make(chan upgrade, 2)
		for i := 0; i < 2; i++ {
			go func() {
				conn, response := dialUpgrade(t, barrierServer.URL, "alpha", parsed.ConnectionToken)
				status := 0
				if response != nil {
					status = response.StatusCode
				}
				results <- upgrade{conn, status}
			}()
		}
		var admitted *websocket.Conn
		for i := 0; i < 2; i++ {
			resolve := <-results
			if resolve.conn != nil {
				if admitted != nil {
					t.Fatal("both simultaneous upgrades consumed one token")
				}
				admitted = resolve.conn
			} else if resolve.status != http.StatusNotFound {
				t.Fatalf("rejected upgrade returned %d, want 404", resolve.status)
			}
		}
		if admitted == nil {
			t.Fatal("exactly one upgrade must be admitted")
		}
		defer admitted.CloseNow()
		handshake(t, admitted)
		writeMessage(t, admitted, `{"type":1,"invocationId":"cc1","target":"Subscribe","arguments":[{"tenant":"alpha","kind":"tenant","id":"alpha","channel":"updates"}]}`)
		if frame := readCompletion(t, admitted, "cc1"); frame.Error != "" {
			t.Fatalf("subscription rejected: %s", frame.Error)
		}
		if err := hub.AcceptEvent(t.Context(), distributed.Envelope{
			Type: realtime.EventType, SchemaVersion: 1, Producer: "test-realtime", ID: "same-token",
			TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha",
			Payload: json.RawMessage(`{"channel":"updates","user_id":"same-user","data":42}`),
		}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		for {
			_, data, err := admitted.Read(ctx)
			if err != nil {
				t.Fatalf("notification missing: %v", err)
			}
			raw := bytes.TrimSuffix(data, []byte{recordSeparator})
			if skipKeepAlive(raw) {
				continue
			}
			if !strings.Contains(string(raw), `"target":"Notification"`) || !strings.Contains(string(raw), `"id":"same-token"`) {
				t.Fatalf("unexpected frame %s", raw)
			}
			break
		}
	})
}

func TestA97RT002(t *testing.T) {
	base, _ := testEndpoint(t)
	open := func(t *testing.T) *websocket.Conn {
		t.Helper()
		_, parsed := negotiateRequest(t, base, "alpha", "", "1")
		conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
		if conn == nil {
			t.Fatal("upgrade rejected")
		}
		handshake(t, conn)
		return conn
	}
	t.Run("invalid-method", func(t *testing.T) {
		conn := open(t)
		defer conn.CloseNow()
		writeMessage(t, conn, `{"type":1,"invocationId":"m1","target":"Nope","arguments":[]}`)
		if frame := readCompletion(t, conn, "m1"); frame.Error != "REALTIME_METHOD_UNKNOWN" {
			t.Fatalf("unexpected completion %+v", frame)
		}
	})
	t.Run("invalid-arguments", func(t *testing.T) {
		conn := open(t)
		defer conn.CloseNow()
		writeMessage(t, conn, `{"type":1,"invocationId":"m2","target":"Echo","arguments":[12]}`)
		if frame := readCompletion(t, conn, "m2"); frame.Error != "REALTIME_INVALID" {
			t.Fatalf("unexpected completion %+v", frame)
		}
		writeMessage(t, conn, `{"type":1,"invocationId":"m3","target":"Echo","arguments":["official"]}`)
		for {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			_, data, err := conn.Read(ctx)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			raw := bytes.TrimSuffix(data, []byte{recordSeparator})
			if skipKeepAlive(raw) {
				continue
			}
			if !strings.Contains(string(raw), `"result":"official"`) {
				t.Fatalf("unexpected completion %s", raw)
			}
			break
		}
	})
	t.Run("invalid-frame", func(t *testing.T) {
		conn := open(t)
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, []byte{0x00, 0x01, 0x02}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := conn.Read(ctx); err == nil {
			t.Fatal("invalid frame was tolerated")
		}
	})
	t.Run("oversize", func(t *testing.T) {
		conn := open(t)
		defer conn.CloseNow()
		payload := `"oversize:"` + strings.Repeat("x", 512)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText,
			[]byte(`{"type":1,"invocationId":"big","target":"Echo","arguments":[`+payload+`]}`+"\x1e")); err != nil {
			t.Fatal(err)
		}
		if _, _, err := conn.Read(ctx); err == nil {
			t.Fatal("oversize message was tolerated")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		conn := open(t)
		defer conn.CloseNow()
		writeMessage(t, conn, `{"type":1,"invocationId":"c1","target":"Wait","arguments":[]}`)
		time.Sleep(200 * time.Millisecond)
		writeMessage(t, conn, `{"type":5,"invocationId":"c1"}`)
		if frame := readCompletion(t, conn, "c1"); frame.Error == "" {
			t.Fatalf("canceled invocation completed without error: %+v", frame)
		}
	})
	t.Run("no-client-streaming", func(t *testing.T) {
		conn := open(t)
		defer conn.CloseNow()
		writeMessage(t, conn, `{"type":4,"invocationId":"s1","target":"Echo","arguments":[]}`)
		if frame := readCompletion(t, conn, "s1"); frame.Error != "REALTIME_STREAMING_UNSUPPORTED" {
			t.Fatalf("unexpected completion %+v", frame)
		}
	})
}
