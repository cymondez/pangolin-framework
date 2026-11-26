//go:build integration

package m7

// A102/no-client-ack: broker acceptance, node processing, socket write and
// client receipt are separate facts. A socket-failed subscriber must not be
// able to turn an unacked delivery into silent loss: the broker accepted the
// event, the node processed and acknowledged it, and no end-to-end
// acknowledgement is claimed.
//
// A102/node-restart: two real child processes share one stream. Killing one
// node must not disturb the survivor; a client reconnect requires a fresh
// negotiation and an explicit resubscription, and subscriptions are never
// silently restored.

import (
	"context"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

func TestA102NoClientAck(t *testing.T) {
	binary, adminDSN, appDSN, workerDSN, natsURL, key := demoEnvironment(t)
	const stream = "PANGOLIN_M7_NO_CLIENT_ACK"
	deleteStream(t, natsURL, stream)
	t.Cleanup(func() { deleteStream(t, natsURL, stream) })
	ensureProvisioned(t, binary, adminDSN, natsURL, stream)
	server := startDemoServer(t, binary, stream, map[string]string{
		"PANGOLIN_REALTIME_APP_DSN": appDSN, "PANGOLIN_REALTIME_WORKER_DSN": workerDSN,
	})
	_, js := brokerAdmin(t, natsURL)

	// Stage baseline: broker accepted, node processed, socket written and the
	// live client received the stable event.
	live := connectHub(t, server.base, key, "alice")
	live.subscribe(t, "s1", "alpha", "tenant", "alpha", "tenant")
	if frame := live.awaitCompletion(t, "s1", 10*time.Second); frame.Error != "" {
		t.Fatalf("subscription failed: %s`nSERVER LOG:`n%s", frame.Error, server.logs.String())
	}
	firstID := invokeNotify(t, live, "n1", "tenant", "", `{"value":"baseline"}`)
	live.awaitNotification(t, firstID, 30*time.Second)

	// A second, passive observer on the same node for the socket-failure
	// semantics of the filtered event.
	foreign := connectHub(t, server.base, key, "governor")
	foreign.subscribe(t, "s2", "alpha", "tenant", "alpha", "tenant")
	if frame := foreign.awaitCompletion(t, "s2", 10*time.Second); frame.Error != "" {
		t.Fatalf("subscription failed: %s`nSERVER LOG:`n%s", frame.Error, server.logs.String())
	}

	// The subscriber socket fails before the next event.
	live.abort()
	time.Sleep(300 * time.Millisecond)

	streamBefore := streamMessageCount(t, js, stream)
	// user-filtered event: its only subscriber socket is gone, so the node
	// accepts the event from the broker and processes it, and nothing reaches
	// a client.
	secondID := invokeNotify(t, foreign, "n2", "tenant", "alice", `{"value":"socket-failed"}`)
	thirdID := invokeNotify(t, foreign, "n3", "tenant", "governor", `{"value":"observer"}`)
	if secondID == "" || thirdID == "" {
		t.Fatal("missing produced event ids")
	}
	// The Outbox dispatch latency must not mask the broker acceptance proof:
	// the stream count is observed after the broker actually publishes.
	waitForStreamGrowth(t, js, stream, streamBefore, 2, 30*time.Second)
	// Broker accepted and the node acknowledged both deliveries: no
	// acknowledgement pending after the AckWait window, and no redelivery.
	deadline := time.Now().Add(9 * time.Second)
	for {
		quiet := true
		for _, info := range consumerInfos(t, js, stream) {
			if info.NumAckPending != 0 {
				quiet = false
				break
			}
		}
		if quiet {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("broker still has pending acknowledgements after the socket failure")
		}
		time.Sleep(500 * time.Millisecond)
	}
	// The observer's own targeted event is the barrier for the exclusion
	// window: once it arrived, the alice-targeted event has been processed or
	// was never delivered to this subscriber.
	foreign.awaitNotification(t, thirdID, 30*time.Second)
	foreign.assertAbsent(t, func(frame hubFrame) bool { return notifyWithID(frame, secondID) }, "filtered event")
	t.Logf("second=%s third=%s both acknowledged at node and broker", secondID, thirdID)
}

func streamMessageCount(t *testing.T, js natsgo.JetStreamContext, stream string) uint64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := js.StreamInfo(stream, natsgo.Context(ctx))
	if err != nil {
		t.Fatal(err)
	}
	return info.State.Msgs
}

func consumerInfos(t *testing.T, js natsgo.JetStreamContext, stream string) []*natsgo.ConsumerInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	infos := js.ConsumersInfo(stream, natsgo.Context(ctx))
	collected := make([]*natsgo.ConsumerInfo, 0, 4)
	for info := range infos {
		collected = append(collected, info)
	}
	if len(collected) == 0 {
		t.Fatal("the demo node consumer is missing from the stream")
	}
	return collected
}

func TestA102NodeRestart(t *testing.T) {
	binary, adminDSN, appDSN, workerDSN, natsURL, key := demoEnvironment(t)
	const stream = "PANGOLIN_M7_NODE_RESTART"
	deleteStream(t, natsURL, stream)
	t.Cleanup(func() { deleteStream(t, natsURL, stream) })
	ensureProvisioned(t, binary, adminDSN, natsURL, stream)
	overrides := map[string]string{
		"PANGOLIN_REALTIME_APP_DSN": appDSN, "PANGOLIN_REALTIME_WORKER_DSN": workerDSN,
	}
	nodeA := startDemoServer(t, binary, stream, overrides)
	nodeB := startDemoServer(t, binary, stream, overrides)

	first := connectHub(t, nodeA.base, key, "alice")
	first.subscribe(t, "s1", "alpha", "tenant", "alpha", "tenant")
	if frame := first.awaitCompletion(t, "s1", 10*time.Second); frame.Error != "" {
		t.Fatalf("subscription failed: %s`nSERVER LOG:`n%s", frame.Error, nodeA.logs.String())
	}
	// The survivor node runs the receiver too: the same subscription on the
	// far node receives the fan-out through the real broker.
	second := connectHub(t, nodeB.base, key, "governor")
	second.subscribe(t, "s2", "alpha", "tenant", "alpha", "tenant")
	if frame := second.awaitCompletion(t, "s2", 10*time.Second); frame.Error != "" {
		t.Fatalf("subscription failed: %s`nSERVER LOG:`n%s", frame.Error, nodeB.logs.String())
	}
	beforeKill := invokeNotify(t, second, "n1", "tenant", "", `{"value":"cross-node"}`)
	first.awaitNotification(t, beforeKill, 30*time.Second)
	second.awaitNotification(t, beforeKill, 30*time.Second)

	// Crash one node while the survivor keeps its socket and subscription.
	nodeA.kill(t)
	first.abort()
	time.Sleep(300 * time.Millisecond)
	duringKill := invokeNotify(t, second, "n2", "tenant", "", `{"value":"survivor"}`)
	second.awaitNotification(t, duringKill, 30*time.Second)

	// A reconnect to the survivor negotiates a fresh session: the old
	// subscription is never restored, only the explicit resubscribe is.
	reconnected := connectHub(t, nodeB.base, key, "governor")
	afterReconnect := invokeNotify(t, second, "n3", "tenant", "", `{"value":"no-restoration"}`)
	second.awaitNotification(t, afterReconnect, 30*time.Second)
	reconnected.assertAbsent(t, func(frame hubFrame) bool { return notifyWithID(frame, afterReconnect) }, "unrestored old subscription")
	reconnected.subscribe(t, "s3", "alpha", "tenant", "alpha", "tenant")
	if frame := reconnected.awaitCompletion(t, "s3", 10*time.Second); frame.Error != "" {
		t.Fatalf("resubscription failed: %s", frame.Error)
	}
	afterResubscribe := invokeNotify(t, second, "n4", "tenant", "", `{"value":"resubscribed"}`)
	second.awaitNotification(t, afterResubscribe, 30*time.Second)
	reconnected.awaitNotification(t, afterResubscribe, 30*time.Second)

	// Restarting a node: a fresh process on a fresh listener serves
	// negotiate, subscribe and fan-out again.
	restarted := startDemoServer(t, binary, stream, overrides)
	third := connectHub(t, restarted.base, key, "alice")
	third.subscribe(t, "s4", "alpha", "tenant", "alpha", "tenant")
	if frame := third.awaitCompletion(t, "s4", 10*time.Second); frame.Error != "" {
		t.Fatalf("subscription failed after the restart: %s", frame.Error)
	}
	afterRestart := invokeNotify(t, second, "n5", "tenant", "", `{"value":"restarted"}`)
	third.awaitNotification(t, afterRestart, 30*time.Second)
	second.awaitNotification(t, afterRestart, 30*time.Second)
	t.Logf("node restart and reconnect proven with events %s..%s", duringKill, afterRestart)
}
