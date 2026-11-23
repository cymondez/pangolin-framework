// Package nats provides the M1 JetStream file-stream transport slice.
package nats

import (
	"context"
	"fmt"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/nats-io/nats.go"
)

type Broker struct {
	connection      *nats.Conn
	js              nats.JetStreamContext
	stream, subject string
}

func Connect(url, stream, subject string) (*Broker, error) {
	if stream == "" || subject == "" {
		return nil, fmt.Errorf("NATS stream and subject required")
	}
	connection, e := nats.Connect(url, nats.Timeout(3*time.Second), nats.NoReconnect())
	if e != nil {
		return nil, e
	}
	js, e := connection.JetStream()
	if e != nil {
		connection.Close()
		return nil, e
	}
	return &Broker{connection, js, stream, subject}, nil
}

// Provision is an explicit deployment/fixture operation, never API startup.
func (b *Broker) Provision(ctx context.Context) error {
	_, e := b.js.AddStream(&nats.StreamConfig{Name: b.stream, Subjects: []string{b.subject}, Storage: nats.FileStorage, MaxBytes: 64 << 20, MaxMsgSize: 1 << 20, MaxAge: 168 * time.Hour}, nats.Context(ctx))
	return e
}
func (b *Broker) Send(ctx context.Context, e distributed.Envelope) error {
	raw, err := distributed.Encode(e)
	if err != nil {
		return err
	}
	info, err := b.js.StreamInfo(b.stream, nats.Context(ctx))
	if err != nil {
		return err
	}
	if info == nil || info.Config.Storage != nats.FileStorage {
		return fmt.Errorf("JetStream file persistence required")
	}
	message := nats.NewMsg(b.subject)
	message.Data = raw
	message.Header.Set(nats.MsgIdHdr, e.ID)
	ack, err := b.js.PublishMsg(message, nats.Context(ctx), nats.ExpectStream(b.stream))
	if err != nil {
		return err
	}
	if ack == nil || ack.Stream != b.stream || ack.Sequence == 0 {
		return fmt.Errorf("JetStream durable acknowledgement missing")
	}
	return nil
}

type Delivery struct {
	Envelope distributed.Envelope
	message  *nats.Msg
}

func (d *Delivery) Ack(ctx context.Context) error { return d.message.AckSync(nats.Context(ctx)) }
func (b *Broker) Subscribe(durable string) (*Subscription, error) {
	if durable == "" {
		return nil, fmt.Errorf("durable consumer required")
	}
	s, e := b.js.PullSubscribe(b.subject, durable, nats.BindStream(b.stream), nats.ManualAck(), nats.AckExplicit(), nats.MaxDeliver(8), nats.AckWait(5*time.Second))
	if e != nil {
		return nil, e
	}
	return &Subscription{s}, nil
}

type Subscription struct{ subscription *nats.Subscription }

func (s *Subscription) Next(ctx context.Context) (*Delivery, error) {
	messages, e := s.subscription.Fetch(1, nats.Context(ctx))
	if e != nil {
		return nil, e
	}
	event, e := distributed.Decode(messages[0].Data)
	if e != nil {
		return nil, e
	}
	return &Delivery{event, messages[0]}, nil
}
func (b *Broker) Close() { b.connection.Close() }
