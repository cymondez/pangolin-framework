package signalr

// Protocol-level read loop and server push coverage; see the RT-001/RT-002
// mapping in tests/contract/m7.json.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
)

func flowListener(t *testing.T, extra []realtime.Method) (hub *realtime.Hub, base string) {
	t.Helper()
	methods := []realtime.Method{
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
	}
	methods = append(methods, extra...)
	authority := &demoAuthority{}
	auth, err := identity.NewPipeline(authority)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := tenancy.NewResolver(identity.Identity, authority)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := scope.NewRegistry(tenancy.Identity, tenancy.Provider{
		Authority: authority, Permission: func(context.Context, scope.ScopeBinding, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	hub, err = realtime.New(realtime.Options{
		Producer: "test-realtime", Connect: realtime.Access{Permission: "connect"},
		Methods: methods,
		Channels: []realtime.Channel{{Name: "updates", Target: "Notification", Kinds: []string{"tenant"},
			Publish:   realtime.Access{Permission: "publish"},
			Subscribe: realtime.Access{Permission: "subscribe"},
			Receive:   realtime.Access{Permission: "receive"}}},
		MaxConnections: 8, MaxSubscriptions: 4, QueueSize: 4, MaxPayload: 1024, MaxOperations: 4,
		OperationTimeout: time.Second,
	}, auth, resolver, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	endpoint, err := New(hub, Options{Path: "/rt", AllowLoopbackCredentials: true,
		NegotiationLimit: 8, HandshakeConcurrency: 4, NegotiationTTL: 30 * time.Second,
		HandshakeTimeout: 2 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 2 * time.Second,
		KeepAlive: demoKeepAlive, MaxMessage: 512})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	return hub, server.URL
}

// A connected client receives subscribed notifications and oversized results
// collapse into a single safe completion instead of an unreadable frame.
func TestServerPushAndOversizedResult(t *testing.T) {
	hub, base := flowListener(t, []realtime.Method{{Name: "Blob", Access: realtime.Access{Permission: "blob"},
		Invoke: func(context.Context, []json.RawMessage) (any, error) {
			return strings.Repeat("blob", 300), nil
		}}})
	_, parsed := negotiateRequest(t, base, "alpha", "", "1")
	conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
	if conn == nil {
		t.Fatal("upgrade rejected")
	}
	defer conn.CloseNow()
	handshake(t, conn)
	writeMessage(t, conn, `{"type":1,"invocationId":"sub","target":"Subscribe","arguments":[{"tenant":"alpha","kind":"tenant","id":"alpha","channel":"updates"}]}`)
	if frame := readCompletion(t, conn, "sub"); frame.Error != "" {
		t.Fatalf("subscription failed: %+v", frame)
	}
	err := hub.AcceptEvent(context.Background(), distributed.Envelope{
		Type: realtime.EventType, SchemaVersion: 1, Producer: "test-realtime", ID: "push-1",
		TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha",
		Payload: json.RawMessage(`{"channel":"updates","data":42}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The delivery must be serialized as an Invocation of the channel target.
	deadline := time.Now().Add(2 * time.Second)
	pushed := false
	for !pushed && time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		_, data, readErr := conn.Read(readCtx)
		cancel()
		if readErr != nil {
			break
		}
		raw := strings.TrimSuffix(string(data), "\x1e")
		if skipKeepAlive([]byte(raw)) {
			continue
		}
		var push struct {
			Type      int               `json:"type"`
			Target    string            `json:"target"`
			Result    json.RawMessage   `json:"result"`
			Arguments []json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal([]byte(raw), &push) == nil && push.Type == 1 && push.Target == "Notification" {
			pushed = true
			if !strings.Contains(string(push.Arguments[0]), `"data":42`) {
				t.Fatalf("pushed payload missing data: %s", raw)
			}
		}
	}
	if !pushed {
		t.Fatal("subscribed notification was not pushed")
	}
	// A result larger than the maximum message collapses into a safe error.
	writeMessage(t, conn, `{"type":1,"invocationId":"big","target":"Blob","arguments":[]}`)
	if frame := readCompletion(t, conn, "big"); frame.Error != "REALTIME_RESULT_TOO_LARGE" {
		t.Fatalf("oversize result not replaced: %+v", frame)
	}
}

func TestPanicHandlerReturnsSafeError(t *testing.T) {
	panicMethod := realtime.Method{Name: "Boom", Access: realtime.Access{Permission: "boom"},
		Invoke: func(context.Context, []json.RawMessage) (any, error) { panic("handler bug") }}
	_, base := flowListener(t, []realtime.Method{panicMethod})
	_, parsed := negotiateRequest(t, base, "alpha", "", "1")
	conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
	if conn == nil {
		t.Fatal("upgrade rejected")
	}
	defer conn.CloseNow()
	handshake(t, conn)
	writeMessage(t, conn, `{"type":1,"invocationId":"boom","target":"Boom","arguments":[]}`)
	if frame := readCompletion(t, conn, "boom"); frame.Error != "REALTIME_HANDLER_FAILED" {
		t.Fatalf("handler panic not contained: %+v", frame)
	}
}

// Frames the read loop cannot recover from must close the session without
// leaking the connection or the operation goroutine.
func TestReadLoopClosesOnUnrecoverableFrames(t *testing.T) {
	cases := map[string]struct {
		frame string
	}{
		"unknown-type":       {`{"type":9,"invocationId":"x"}`},
		"close-message":      {`{"type":7}`},
		"stream-no-identity": {`{"type":4,"target":"Echo","arguments":[]}`},
		"cancel-no-identity": {`{"type":5,"target":"Echo","arguments":[]}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, base := flowListener(t, nil)
			_, parsed := negotiateRequest(t, base, "alpha", "", "1")
			conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
			if conn == nil {
				t.Fatal("upgrade rejected")
			}
			defer conn.CloseNow()
			handshake(t, conn)
			writeMessage(t, conn, tc.frame)
			readCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			for {
				_, _, err := conn.Read(readCtx)
				if err != nil {
					if ctxErr(readCtx) != nil {
						t.Fatalf("connection stayed open after unrecoverable frame: %v", err)
					}
					return
				}
			}
		})
	}
}

func ctxErr(ctx context.Context) error { return ctx.Err() }

// Recoverable protocol violations still complete the invocation with the
// protocol error instead of silently dropping the request.
func TestProtocolViolationCompletesWithError(t *testing.T) {
	cases := map[string]string{
		"oversize-target":    `{"type":1,"invocationId":"long","target":"` + strings.Repeat("t", 130) + `","arguments":[]}`,
		"stream-ids":         `{"type":1,"invocationId":"s","target":"Echo","arguments":["x"],"streamIds":["s0"]}`,
		"missing-arguments":  `{"type":1,"invocationId":"m","target":"Echo"}`,
		"streaming-expected": `{"type":4,"invocationId":"s2","target":"Echo","arguments":[]}`,
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			_, base := flowListener(t, nil)
			_, parsed := negotiateRequest(t, base, "alpha", "", "1")
			conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
			if conn == nil {
				t.Fatal("upgrade rejected")
			}
			defer conn.CloseNow()
			handshake(t, conn)
			writeMessage(t, conn, frame)
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				readCtx, cancel := context.WithTimeout(t.Context(), time.Second)
				_, data, err := conn.Read(readCtx)
				cancel()
				if err != nil {
					t.Fatalf("connection closed instead of completing: %v", err)
				}
				raw := strings.TrimSuffix(string(data), "\x1e")
				if skipKeepAlive([]byte(raw)) {
					continue
				}
				var completed completionFrame
				if json.Unmarshal([]byte(raw), &completed) != nil {
					t.Fatalf("unexpected frame %s", raw)
				}
				if completed.Type == 3 && completed.Error != "" {
					if completed.Error != "REALTIME_PROTOCOL_INVALID" && completed.Error != "REALTIME_STREAMING_UNSUPPORTED" {
						t.Fatalf("unexpected completion %+v", completed)
					}
					return
				}
			}
			t.Fatal("no error completion arrived")
		})
	}
}

// A concurrent invocation while one operation is active is rejected with an
// explicit capacity completion, and a client repeating the active id closes
// the connection as a protocol error.
func TestConcurrentInvocationGuards(t *testing.T) {
	t.Run("capacity-completion", func(t *testing.T) {
		_, base := flowListener(t, nil)
		_, parsed := negotiateRequest(t, base, "alpha", "", "1")
		conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
		if conn == nil {
			t.Fatal("upgrade rejected")
		}
		defer conn.CloseNow()
		handshake(t, conn)
		writeMessage(t, conn, `{"type":1,"invocationId":"w","target":"Wait","arguments":[]}`)
		time.Sleep(200 * time.Millisecond)
		writeMessage(t, conn, `{"type":1,"invocationId":"second","target":"Echo","arguments":["x"]}`)
		if frame := readCompletion(t, conn, "second"); frame.Error != realtime_capacity_message {
			t.Fatalf("concurrent invocation admitted silently: %+v", frame)
		}
	})
	t.Run("duplicate-invocation-id", func(t *testing.T) {
		_, base := flowListener(t, nil)
		_, parsed := negotiateRequest(t, base, "alpha", "", "1")
		conn, _ := dialUpgrade(t, base, "alpha", parsed.ConnectionToken)
		if conn == nil {
			t.Fatal("upgrade rejected")
		}
		defer conn.CloseNow()
		handshake(t, conn)
		writeMessage(t, conn, `{"type":1,"invocationId":"d","target":"Wait","arguments":[]}`)
		time.Sleep(200 * time.Millisecond)
		writeMessage(t, conn, `{"type":1,"invocationId":"d","target":"Wait","arguments":[]}`)
		readCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		for {
			if _, _, err := conn.Read(readCtx); err != nil {
				if ctxErr(readCtx) != nil {
					t.Fatalf("duplicate invocation id kept the connection open: %v", err)
				}
				return
			}
		}
	})
}

var realtime_capacity_message = "REALTIME_CAPACITY"

var _ = errors.New
var _ = websocket.MessageText
