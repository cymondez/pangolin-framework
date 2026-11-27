package jetstream

// Coverage for the realtime routing and receiver. The live suite runs against
// a real JetStream server when PANGOLIN_REALTIME_NATS_URL is set (the shared
// pangolin-realtime-example fixture); a dedicated stream is owned and removed
// by the suite.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/nats"
	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/pangolin-framework/pangolin/tenancy"
)

type countingPublisher struct {
	realtimeEvents atomic.Int32
	defaultEvents  atomic.Int32
}

func (p *countingPublisher) Send(_ context.Context, event distributed.Envelope) error {
	if event.Type == "pangolin.realtime.notification" {
		p.realtimeEvents.Add(1)
	} else {
		p.defaultEvents.Add(1)
	}
	return nil
}

type failingPublisher struct{ err error }

func (p failingPublisher) Send(context.Context, distributed.Envelope) error { return p.err }

func TestRouteSend(t *testing.T) {
	realtimePublisher, defaultPublisher := &countingPublisher{}, &countingPublisher{}
	route := Route{Realtime: realtimePublisher, Default: defaultPublisher}
	if err := route.Send(context.Background(), distributed.Envelope{Type: realtime.EventType, ID: "rt"}); err != nil {
		t.Fatal(err)
	}
	if err := route.Send(context.Background(), distributed.Envelope{Type: "some.other", ID: "d"}); err != nil {
		t.Fatal(err)
	}
	if realtimePublisher.realtimeEvents.Load() != 1 || defaultPublisher.defaultEvents.Load() != 1 {
		t.Fatalf("unexpected routing: realtime=%d default=%d", realtimePublisher.realtimeEvents.Load(), defaultPublisher.defaultEvents.Load())
	}
	var missing Route
	if err := missing.Send(context.Background(), distributed.Envelope{Type: realtime.EventType}); !errors.Is(err, realtime.ErrInvalid) {
		t.Fatalf("missing target accepted: %v", err)
	}
	publishErr := errors.New("broker publish failed")
	failed := Route{Realtime: failingPublisher{err: publishErr}}
	if err := failed.Send(context.Background(), distributed.Envelope{Type: realtime.EventType}); !errors.Is(err, publishErr) {
		t.Fatalf("publisher failure was replaced: %v", err)
	}
}

func TestReceiverLifecycleWithoutBroker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	receiver := &Receiver{cancel: cancel, done: done}
	if err := receiver.Quiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("quiesce did not cancel the receiver")
	}

	receiver.cancel = func() {}
	receiver.Cancel()
	close(done)
	if err := receiver.Drain(context.Background()); err != nil {
		t.Fatalf("completed receiver did not drain: %v", err)
	}

	waiting := &Receiver{done: make(chan struct{})}
	drainCtx, cancelDrain := context.WithCancel(context.Background())
	cancelDrain()
	if err := waiting.Drain(drainCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled drain returned %v", err)
	}

	closedCtx, closeCancel := context.WithCancel(context.Background())
	closedDone := make(chan struct{})
	close(closedDone)
	closed := &Receiver{cancel: closeCancel, done: closedDone}
	closed.cleanup.Do(func() {})
	if err := closed.Close(context.Background()); err != nil {
		t.Fatalf("idempotent close failed: %v", err)
	}
	if closedCtx.Err() != context.Canceled {
		t.Fatal("close did not cancel the receiver")
	}
}

func TestStartGuards(t *testing.T) {
	if _, err := Start(context.Background(), nil, nil, 1); !errors.Is(err, realtime.ErrInvalid) {
		t.Fatalf("nil hub accepted: %v", err)
	}
	hub := testHub(t)
	// A zero dedup capacity is refused before any broker call.
	if _, err := Start(context.Background(), nil, hub, 0); !errors.Is(err, realtime.ErrInvalid) {
		t.Fatalf("zero dedup capacity accepted: %v", err)
	}
	// A zero-capacity hub and a missing broker are both refused before any
	// consumer is allocated; broker wiring is composition's responsibility
	// and is covered by the live suite below.
	if _, err := Start(context.Background(), nil, hub, 1); err == nil {
		t.Fatal("nil broker accepted")
	}
}

type receiverAuthority struct{ revoked atomic.Bool }

func (*receiverAuthority) Kind() string { return identity.BearerKind }

func (a *receiverAuthority) Verify(ctx context.Context, credential identity.Credential) (identity.Result, error) {
	if a.revoked.Load() {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{Status: identity.StatusAuthenticated, Method: "test",
		Principal: scope.PrincipalRef{Kind: identity.PrincipalUser, ID: "same-user"}, Tenant: credential.Value}, ctx.Err()
}

func (*receiverAuthority) Check(ctx context.Context, _ scope.PrincipalRef, _ string) (string, error) {
	return "current", ctx.Err()
}

func testHub(t *testing.T) *realtime.Hub {
	t.Helper()
	authority := &receiverAuthority{}
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
		Channels: []realtime.Channel{{Name: "updates", Target: "Update", Kinds: []string{"tenant"},
			Publish:   realtime.Access{Permission: "publish"},
			Subscribe: realtime.Access{Permission: "subscribe"},
			Receive:   realtime.Access{Permission: "receive"}}},
		MaxConnections: 4, MaxSubscriptions: 2, QueueSize: 1, MaxPayload: 1024, MaxOperations: 2,
		OperationTimeout: time.Second,
	}, auth, resolver, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Cancel)
	return hub
}

func brokerURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("PANGOLIN_REALTIME_NATS_URL")
	if url == "" {
		t.Skip("PANGOLIN_REALTIME_NATS_URL not set")
	}
	return url
}

type receiverFixture struct {
	broker  *nats.Broker
	hub     *realtime.Hub
	js      natsgo.JetStreamContext
	conn    *natsgo.Conn
	stream  string
	subject string
}

func startReceiverFixture(t *testing.T) receiverFixture {
	t.Helper()
	url := brokerURL(t)
	stream := "P3-COVERAGE-REALTIME"
	subject := "events.realtime.p3"
	broker, err := nats.ConnectLive(url, stream, subject)
	if err != nil {
		t.Skipf("broker unavailable: %v", err)
	}
	t.Cleanup(broker.Close)
	conn, e := natsgo.Connect(url, natsgo.Timeout(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	js, e := conn.JetStream()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = js.DeleteStream(stream) })
	// Provision with the same file-storage shape as the demo stream but a
	// smaller byte budget: the shared server's account quota already carries
	// the fixture streams, so another 64MiB reservation is refused.
	provisionCtx, provisionCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer provisionCancel()
	// Best-effort removal of a residue stream left by a previously interrupted
	// run; only our own stream name is touched and a missing stream is fine.
	_ = js.DeleteStream(stream, natsgo.Context(provisionCtx))
	if _, e = js.AddStream(&natsgo.StreamConfig{Name: stream, Subjects: []string{subject},
		Storage: natsgo.FileStorage, MaxBytes: 4 << 20, MaxMsgSize: 1 << 20, MaxAge: 168 * time.Hour},
		natsgo.Context(provisionCtx)); e != nil {
		// A residue stream may exhaust the shared server's file quota; that is
		// fixture hygiene, not a regression in this suite.
		if strings.Contains(e.Error(), "insufficient storage resources") {
			t.Skipf("shared NATS fixture storage quota exhausted; reset the fixture streams: %v", e)
		}
		t.Fatal(e)
	}
	info, e := js.StreamInfo(stream, natsgo.Context(provisionCtx))
	if e != nil || info == nil || info.Config.Storage != natsgo.FileStorage || info.Config.Retention != natsgo.LimitsPolicy {
		t.Fatalf("stream not provisioned with the required shape: %v %+v", e, info)
	}
	hub := testHub(t)
	return receiverFixture{broker: broker, hub: hub, js: js, conn: conn, stream: stream, subject: subject}
}

func realtimeEnvelope(id string) distributed.Envelope {
	return distributed.Envelope{ID: id, Type: realtime.EventType, SchemaVersion: 1, OccurredAt: time.Now().UTC(),
		TenantID: "alpha", ScopeKind: "tenant", ScopeID: "alpha", CorrelationID: id, Producer: "test-realtime",
		Payload: json.RawMessage(`{"channel":"updates","user_id":"same-user","data":42}`)}
}

// waitSessionDelivery consumes the next delivery and re-admits it through
// WriteDelivery, asserting it carries the expected event id.
func waitSessionDelivery(t *testing.T, session *realtime.Session, id string) {
	t.Helper()
	select {
	case item := <-session.Pending():
		written := ""
		err := session.WriteDelivery(item, func(_ context.Context, target string, notification realtime.Notification) error {
			if target == "" {
				return errors.New("empty target")
			}
			written = notification.ID
			return nil
		})
		if err != nil {
			t.Fatalf("delivery not admitted: %v", err)
		}
		if written != id {
			t.Fatalf("unexpected delivery %q != %q", written, id)
		}
		return
	case <-time.After(3 * time.Second):
		t.Fatalf("delivery %q never reached the session", id)
	}
}

func assertNoDelivery(t *testing.T, session *realtime.Session, note string) {
	t.Helper()
	select {
	case <-session.Pending():
		t.Fatalf("%s: unexpected delivery arrived", note)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestReceiverFanOutLifecycle(t *testing.T) {
	fixture := startReceiverFixture(t)
	credentials := identity.Credential{Kind: identity.BearerKind, Value: "alpha"}
	subject, err := fixture.hub.Authenticate(context.Background(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	session, err := fixture.hub.Open(context.Background(), credentials, subject)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	raw, err := json.Marshal(map[string]string{"tenant": "alpha", "kind": "tenant", "id": "alpha", "channel": "updates"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Invoke(session.Context(), "Subscribe", []json.RawMessage{raw}); err != nil {
		t.Fatal(err)
	}
	receiver, err := Start(context.Background(), fixture.broker, fixture.hub, 2)
	if err != nil {
		t.Fatal(err)
	}
	// A valid envelope is accepted exactly once: the duplicate is skipped
	// while the dedup window holds the id.
	if err := fixture.broker.Send(context.Background(), realtimeEnvelope("dup-1")); err != nil {
		t.Fatal(err)
	}
	if err := fixture.broker.Send(context.Background(), realtimeEnvelope("dup-1")); err != nil {
		t.Fatal(err)
	}
	waitSessionDelivery(t, session, "dup-1")
	assertNoDelivery(t, session, "duplicate")
	// An envelope that fails hub validation is rejected and never reaches a
	// session.
	foreign := realtimeEnvelope("foreign-producer")
	foreign.Producer = "other"
	if err := fixture.broker.Send(context.Background(), foreign); err != nil {
		t.Fatal(err)
	}
	// A payload that fails envelope decoding is rejected on the broker side.
	if err := fixture.conn.Publish(fixture.subject, []byte("garbage")); err != nil {
		t.Fatal(err)
	}
	assertNoDelivery(t, session, "invalid")
	// A new dedup cycle accepts a fresh id after the window rotates.
	if err := fixture.broker.Send(context.Background(), realtimeEnvelope("after-rotate")); err != nil {
		t.Fatal(err)
	}
	waitSessionDelivery(t, session, "after-rotate")
	if err := receiver.Quiesce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := receiver.Drain(context.Background()); err != nil {
		t.Fatalf("drained receiver reported an error: %v", err)
	}
	if err := receiver.Close(context.Background()); err != nil {
		t.Fatalf("receiver close failed: %v", err)
	}
	if err := receiver.Close(context.Background()); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}

func TestReceiverDrainDeadline(t *testing.T) {
	fixture := startReceiverFixture(t)
	receiver, err := Start(context.Background(), fixture.broker, fixture.hub, 1)
	if err != nil {
		t.Fatal(err)
	}
	// A cancelled drain context surfaces its own cancellation instead of
	// pretending the receiver stopped.
	drainCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := receiver.Drain(drainCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled drain returned %v", err)
	}
	receiver.Cancel()
	if err := receiver.Close(context.Background()); err != nil {
		t.Fatalf("receiver close failed: %v", err)
	}
}

// A102/no-client-ack at the node boundary: the receiver acknowledges a
// processed event even when the subscriber never reads its queue, and a hub
// that cannot accept events withholds the transport acknowledgement so the
// broker redelivers the same message to the same node consumer per AckWait.
func TestReceiverNoClientAckAndRedelivery(t *testing.T) {
	fixture := startReceiverFixture(t)
	credentials := identity.Credential{Kind: identity.BearerKind, Value: "alpha"}
	subject, err := fixture.hub.Authenticate(context.Background(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	session, err := fixture.hub.Open(context.Background(), credentials, subject)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	raw, err := json.Marshal(map[string]string{"tenant": "alpha", "kind": "tenant", "id": "alpha", "channel": "updates"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Invoke(session.Context(), "Subscribe", []json.RawMessage{raw}); err != nil {
		t.Fatal(err)
	}
	receiver, err := Start(context.Background(), fixture.broker, fixture.hub, 4)
	if err != nil {
		t.Fatal(err)
	}
	consumerName := onlyRealtimeConsumer(t, fixture)
	// (1) The event is queued to a subscriber that intentionally does not
	// consume during the probe window; the broker acknowledgement still lands.
	if err := fixture.broker.Send(context.Background(), realtimeEnvelope("no-client-ack")); err != nil {
		t.Fatal(err)
	}
	waitConsumerAckPending(t, fixture, consumerName, 0, 6*time.Second, "processed event")
	processed := ""
	select {
	case item := <-session.Pending():
		err := session.WriteDelivery(item, func(_ context.Context, _ string, notification realtime.Notification) error {
			processed = notification.ID
			return nil
		})
		if err != nil {
			t.Fatalf("queued event not admitted: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("processed event missing from the client queue")
	}
	if processed != "no-client-ack" {
		t.Fatalf("unexpected queued event %q", processed)
	}
	// (2) The hub can no longer accept events; the non-ErrInvalid failure
	// withholds the acknowledgement, so the broker redelivers the message to
	// the same node consumer per its AckWait.
	fixture.hub.Cancel()
	if err := fixture.broker.Send(context.Background(), realtimeEnvelope("lost-ack")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(12 * time.Second)
	for {
		info, err := fixture.js.ConsumerInfo(fixture.stream, consumerName)
		if err != nil {
			t.Fatal(err)
		}
		if info.NumRedelivered >= 1 && info.NumAckPending == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("broker never redelivered the unacked message: %+v", info)
		}
		time.Sleep(250 * time.Millisecond)
	}
	// The consumer is owned by this receiver: closing removes it, leaving the
	// unacked message in the stream for the next deployment, never consumed.
	if err := receiver.Close(context.Background()); err != nil {
		t.Fatalf("receiver close failed: %v", err)
	}
	if _, err := fixture.js.ConsumerInfo(fixture.stream, consumerName); err == nil {
		t.Fatal("node consumer survived the receiver close")
	}
}

// onlyRealtimeConsumer resolves this node's per-process consumer identity.
func onlyRealtimeConsumer(t *testing.T, fixture receiverFixture) string {
	t.Helper()
	for start := time.Now(); time.Since(start) < 5*time.Second; {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		infos := fixture.js.ConsumersInfo(fixture.stream, natsgo.Context(ctx))
		consumers := make([]*natsgo.ConsumerInfo, 0)
		for info := range infos {
			consumers = append(consumers, info)
		}
		cancel()
		if len(consumers) == 1 && strings.HasPrefix(consumers[0].Name, "realtime-") {
			return consumers[0].Name
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the node consumer was not identifiable")
	return ""
}

func waitConsumerAckPending(t *testing.T, fixture receiverFixture, name string, want int, limit time.Duration, note string) {
	t.Helper()
	for start := time.Now(); time.Since(start) < limit; {
		info, err := fixture.js.ConsumerInfo(fixture.stream, name)
		if err != nil {
			t.Fatal(err)
		}
		if info.NumAckPending == want && info.NumRedelivered == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s: broker acknowledgement did not settle at %d", note, want)
}
