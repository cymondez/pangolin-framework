package nats

// Live JetStream suite: runs only when PANGOLIN_REALTIME_NATS_URL points at a
// real server (the shared pangolin-realtime-example fixture). The suite owns
// a dedicated stream and removes it afterwards.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/nats-io/nats.go"
)

func testBrokerURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("PANGOLIN_REALTIME_NATS_URL")
	if url == "" {
		t.Skip("PANGOLIN_REALTIME_NATS_URL not set")
	}
	return url
}

func liveEvent(id string) distributed.Envelope {
	return distributed.Envelope{ID: id, Type: "m3.fact", SchemaVersion: 1, OccurredAt: time.Now().UTC(),
		TenantID: "live", ScopeKind: "tenant", ScopeID: "live", CorrelationID: id, Producer: "live", Payload: []byte(`{"value":1}`)}
}

// provisionSmall applies the same shape as Provision with a smaller byte
// budget: the shared server's account already carries the fixture streams and
// its 256MiB file quota has no room for another 64MiB reservation.
type liveFixture struct {
	broker   *Broker
	leanConn *nats.Conn
	js       nats.JetStreamContext
	stream   string
	subject  string
}

func (f liveFixture) provisionSmall(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Best-effort removal of a residue stream left by a previously interrupted
	// run; only our own stream name is touched and a missing stream is fine.
	_ = f.js.DeleteStream(f.stream, nats.Context(ctx))
	_, err := f.js.AddStream(&nats.StreamConfig{Name: f.stream, Subjects: []string{f.subject},
		Storage: nats.FileStorage, MaxBytes: 4 << 20, MaxMsgSize: 1 << 20,
		MaxAge: 168 * time.Hour}, nats.Context(ctx))
	if err != nil {
		// A residue stream may exhaust the shared server's file quota; that is
		// fixture hygiene, not a regression in this suite.
		if strings.Contains(err.Error(), "insufficient storage resources") {
			t.Skipf("shared NATS fixture storage quota exhausted; reset the fixture streams: %v", err)
		}
		t.Fatal(err)
	}
	info, err := f.js.StreamInfo(f.stream, nats.Context(ctx))
	if err != nil || info == nil || info.Config.Storage != nats.FileStorage || info.Config.Retention != nats.LimitsPolicy {
		t.Fatalf("stream not provisioned with the required shape: %v %+v", err, info)
	}
}

func startFixture(t *testing.T) liveFixture {
	t.Helper()
	url := testBrokerURL(t)
	stream := "P3-COVERAGE-LIVE"
	subject := "events.live.p3"
	broker, err := ConnectLive(url, stream, subject)
	if err != nil {
		t.Skipf("broker unavailable: %v", err)
	}
	t.Cleanup(broker.Close)
	conn, e := nats.Connect(url, nats.Timeout(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if e != nil {
		conn.Close()
		t.Fatal(e)
	}
	js, e := conn.JetStream()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = js.DeleteStream(stream) })
	fixture := liveFixture{broker: broker, leanConn: conn, js: js, stream: stream, subject: subject}
	fixture.provisionSmall(t)
	return fixture
}

func TestDurableDeliveryAcknowledge(t *testing.T) {
	f := startFixture(t)
	f.provisionSmall(t)
	sub, err := f.broker.Subscribe("p3-durable-next")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.broker.Send(context.Background(), liveEvent("next-1")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	item, err := sub.Next(ctx)
	if err != nil {
		t.Fatalf("durable delivery failed: %v", err)
	}
	if item.Envelope.ID != "next-1" {
		t.Fatalf("unexpected delivery %+v", item)
	}
	if err := item.Ack(ctx); err != nil {
		t.Fatalf("durable acknowledgement failed: %v", err)
	}
	// A corrupt payload fails decoding instead of returning a half event.
	if err := f.leanConn.Publish(f.subject, []byte("not-an-envelope")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	fetch, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	if _, err := sub.Next(fetch); err == nil {
		t.Fatal("corrupt delivery decoded successfully")
	}
}

func TestLiveFanOutAckRejectAndClose(t *testing.T) {
	f := startFixture(t)
	f.provisionSmall(t)
	sub, err := f.broker.SubscribeLive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// A valid envelope is accepted and acknowledged.
	if err := f.broker.Send(context.Background(), liveEvent("live-1")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	item, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if item.Envelope.ID != "live-1" {
		t.Fatalf("unexpected live delivery %+v", item)
	}
	if err := item.Ack(ctx); err != nil {
		t.Fatalf("live acknowledgement failed: %v", err)
	}
	// A payload that fails envelope decoding marks the delivery invalid and
	// the fan-out must terminate it instead of redelivering forever.
	if err := f.leanConn.Publish(f.subject, []byte("garbage")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	fetch, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	poison, err := sub.Next(fetch)
	if err != nil {
		t.Fatalf("poison delivery missing: %v", err)
	}
	if !poison.Invalid {
		t.Fatal("corrupt delivery was accepted as valid")
	}
	if err := poison.Reject(); err != nil {
		t.Fatalf("poison termination failed: %v", err)
	}
	// Closing owns both the subscription and the consumer and is idempotent.
	if err := sub.Close(context.Background()); err != nil {
		t.Fatalf("live close failed: %v", err)
	}
	if err := sub.Close(context.Background()); err != nil {
		t.Fatalf("second live close failed: %v", err)
	}
	// Idempotent close must not swallow unrelated server errors either.
	if _, err := f.js.ConsumerInfo(f.stream, sub.name); !errors.Is(err, nats.ErrConsumerNotFound) {
		t.Fatalf("consumer survived close: %v", err)
	}
}

// A102/broker-redelivery: an acknowledgement lost at the transport boundary
// must redeliver the same stable envelope to the same node consumer per
// AckWait, and a late acknowledgement stops the broker redelivery. The proof
// uses the live JetStream delivery the node receiver consumes, never a mock.
func TestLiveAckWaitRedeliverySameConsumer(t *testing.T) {
	f := startFixture(t)
	f.provisionSmall(t)
	sub, err := f.broker.SubscribeLive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.broker.Send(context.Background(), liveEvent("redeliver-node")); err != nil {
		t.Fatal(err)
	}
	delivery, err := sub.Next(timeoutContext(t, 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Invalid || delivery.Envelope.ID != "redeliver-node" {
		t.Fatalf("unexpected first delivery %+v", delivery)
	}
	firstMeta, err := delivery.msg.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if firstMeta.NumDelivered != 1 {
		t.Fatalf("first delivery attempt %d", firstMeta.NumDelivered)
	}
	info, err := f.js.ConsumerInfo(f.stream, sub.name)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumAckPending != 1 || info.NumRedelivered != 0 {
		t.Fatalf("consumer before the ack loss: ack=%d redelivered=%d", info.NumAckPending, info.NumRedelivered)
	}
	// No acknowledgement is sent: the transport loses the ACK. The broker must
	// redeliver the same message to the same consumer after its AckWait.
	redelivery, err := sub.Next(timeoutContext(t, 14*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if redelivery.Invalid || redelivery.Envelope.ID != "redeliver-node" {
		t.Fatalf("redelivered envelope changed identity: %+v", redelivery.Envelope)
	}
	redeliveryMeta, err := redelivery.msg.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if redeliveryMeta.NumDelivered != 2 {
		t.Fatalf("redelivery attempt %d", redeliveryMeta.NumDelivered)
	}
	info, err = f.js.ConsumerInfo(f.stream, sub.name)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumRedelivered != 1 || info.NumAckPending != 1 {
		t.Fatalf("consumer after the redelivery: ack=%d redelivered=%d", info.NumAckPending, info.NumRedelivered)
	}
	// The late acknowledgement stops further redelivery for this consumer.
	if err := redelivery.Ack(timeoutContext(t, 2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := sub.Next(timeoutContext(t, 7*time.Second)); err == nil {
		t.Fatal("redelivery continued after the late acknowledgement")
	}
	info, err = f.js.ConsumerInfo(f.stream, sub.name)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumAckPending != 0 || info.NumPending != 0 {
		t.Fatalf("consumer after the late ack: ack=%d pending=%d", info.NumAckPending, info.NumPending)
	}
}

func timeoutContext(t *testing.T, wait time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	t.Cleanup(cancel)
	return ctx
}
