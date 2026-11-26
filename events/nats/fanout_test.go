package nats

// Structural coverage for SubscribeLive without a server: the guard errors
// are exercised through stub JetStream contexts. The live happy paths run in
// live_test.go when PANGOLIN_REALTIME_NATS_URL is set.

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
)

type liveStub struct {
	nats.JetStreamContext
	info                            *nats.StreamInfo
	infoErr, addErr, subErr, delErr error
	added, deleted                  bool
}

func (s *liveStub) StreamInfo(string, ...nats.JSOpt) (*nats.StreamInfo, error) {
	return s.info, s.infoErr
}
func (s *liveStub) AddConsumer(string, *nats.ConsumerConfig, ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	s.added = true
	return nil, s.addErr
}
func (s *liveStub) DeleteConsumer(string, string, ...nats.JSOpt) error {
	s.deleted = true
	return s.delErr
}
func (s *liveStub) PullSubscribe(string, string, ...nats.SubOpt) (*nats.Subscription, error) {
	return nil, s.subErr
}

func limitsInfo(retention nats.RetentionPolicy) *nats.StreamInfo {
	return &nats.StreamInfo{Config: nats.StreamConfig{Storage: nats.FileStorage, Retention: retention}}
}

func TestSubscribeLiveGuards(t *testing.T) {
	cause := errors.New("jetstream unavailable")
	cases := map[string]struct {
		subject string
		stub    *liveStub
	}{
		"wildcard-subject":   {"events.>", nil},
		"stream-info":        {"events.facts", &liveStub{infoErr: cause}},
		"interest-retention": {"events.facts", &liveStub{info: limitsInfo(nats.InterestPolicy)}},
		"consumer-refused":   {"events.facts", &liveStub{info: limitsInfo(nats.LimitsPolicy), addErr: cause}},
		"subscribe-refused":  {"events.facts", &liveStub{info: limitsInfo(nats.LimitsPolicy), subErr: cause}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			broker := &Broker{subject: tc.subject, stream: "stream"}
			if tc.stub != nil {
				broker.js = tc.stub
			}
			if _, err := broker.SubscribeLive(context.Background()); err == nil {
				t.Fatal("invalid live subscription accepted")
			}
		})
	}
	// A refused pull subscription cleans up the consumer it created.
	stub := &liveStub{info: limitsInfo(nats.LimitsPolicy), subErr: cause}
	broker := &Broker{js: stub, stream: "stream", subject: "events.facts"}
	if _, err := broker.SubscribeLive(context.Background()); err == nil {
		t.Fatal("refused subscription accepted")
	}
	if !stub.added || !stub.deleted {
		t.Fatalf("consumer cleanup missing: added=%t deleted=%t", stub.added, stub.deleted)
	}
	// The happy path allocates a named delivery consumer.
	stub = &liveStub{info: limitsInfo(nats.LimitsPolicy)}
	broker = &Broker{js: stub, stream: "stream", subject: "events.facts"}
	sub, err := broker.SubscribeLive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sub == nil || sub.name == "" {
		t.Fatal("live consumer not allocated")
	}
	// The stub has no server-side pull subscription: Close relays the
	// unsubscribe failure instead of masking it.
	if err := sub.Close(context.Background()); err == nil {
		t.Fatal("close masked the failed unsubscribe")
	}
}
