package nats

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/nats-io/nats.go"
)

// LiveDelivery belongs to an independent consumer for one live node.
type LiveDelivery struct {
	Envelope distributed.Envelope
	Invalid  bool
	msg      *nats.Msg
}

func (d LiveDelivery) Ack(ctx context.Context) error { return d.msg.AckSync(nats.Context(ctx)) }
func (d LiveDelivery) Reject() error                 { return d.msg.Term() }

type LiveSubscription struct {
	broker *Broker
	name   string
	sub    *nats.Subscription
	once   sync.Once
	err    error
}

// SubscribeLive allocates a distinct DeliverNew consumer per server process.
// InactiveThreshold bounds the lifetime of consumers left by crashed processes.
func (b *Broker) SubscribeLive(ctx context.Context) (*LiveSubscription, error) {
	if b == nil || strings.ContainsAny(b.subject, "*> \t\r\n") {
		return nil, errors.New("live fan-out requires a literal subject")
	}
	info, err := b.js.StreamInfo(b.stream, nats.Context(ctx))
	if err != nil {
		return nil, err
	}
	if info.Config.Storage != nats.FileStorage || info.Config.Retention != nats.LimitsPolicy {
		return nil, fmt.Errorf("live fan-out requires a JetStream file stream with limits retention")
	}
	name := "realtime-" + rand.Text()
	_, err = b.js.AddConsumer(b.stream, &nats.ConsumerConfig{
		Durable: name, FilterSubject: b.subject, DeliverPolicy: nats.DeliverNewPolicy, AckPolicy: nats.AckExplicitPolicy,
		AckWait: 5 * time.Second, MaxDeliver: 8, MaxAckPending: 1, InactiveThreshold: 10 * time.Minute,
	}, nats.Context(ctx))
	if err != nil {
		return nil, err
	}
	sub, err := b.js.PullSubscribe(b.subject, name, nats.Bind(b.stream, name))
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		return nil, errors.Join(err, b.js.DeleteConsumer(b.stream, name, nats.Context(cleanup)))
	}
	return &LiveSubscription{broker: b, name: name, sub: sub}, nil
}

func (s *LiveSubscription) Next(ctx context.Context) (LiveDelivery, error) {
	messages, err := s.sub.Fetch(1, nats.Context(ctx))
	if err != nil {
		return LiveDelivery{}, err
	}
	if len(messages) != 1 {
		return LiveDelivery{}, errors.New("empty JetStream delivery")
	}
	msg := messages[0]
	event, err := distributed.Decode(msg.Data)
	return LiveDelivery{Envelope: event, Invalid: err != nil || msg.Subject != s.broker.subject, msg: msg}, nil
}

// Close owns the subscription and the consumer. The connection remains owned
// by the caller that opened the Broker.
func (s *LiveSubscription) Close(ctx context.Context) error {
	s.once.Do(func() {
		unsubscribe := s.sub.Unsubscribe()
		remove := s.broker.js.DeleteConsumer(s.broker.stream, s.name, nats.Context(ctx))
		if errors.Is(remove, nats.ErrConsumerNotFound) {
			remove = nil
		}
		s.err = errors.Join(unsubscribe, remove)
	})
	return s.err
}
