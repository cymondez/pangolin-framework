package nats

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/nats-io/nats.go"
	"testing"
	"time"
)

func TestBrokerRejectsInvalidConfigurationAndEnvelope(t *testing.T) {
	if _, e := Connect("nats://127.0.0.1:1", "", "subject"); e == nil {
		t.Fatal("missing stream")
	}
	if _, e := Connect("nats://127.0.0.1:1", "stream", ""); e == nil {
		t.Fatal("missing subject")
	}
	if _, e := Connect(":invalid", "stream", "subject"); e == nil {
		t.Fatal("invalid address")
	}
	b := &Broker{}
	if e := b.Send(context.Background(), distributed.Envelope{}); e == nil {
		t.Fatal("invalid event")
	}
}

type jetStreamStub struct {
	nats.JetStreamContext
	info                              *nats.StreamInfo
	infoErr, publishErr, subscribeErr error
	ack                               *nats.PubAck
}

func (s jetStreamStub) StreamInfo(string, ...nats.JSOpt) (*nats.StreamInfo, error) {
	return s.info, s.infoErr
}
func (s jetStreamStub) PublishMsg(*nats.Msg, ...nats.PubOpt) (*nats.PubAck, error) {
	return s.ack, s.publishErr
}
func (s jetStreamStub) AddStream(*nats.StreamConfig, ...nats.JSOpt) (*nats.StreamInfo, error) {
	return s.info, s.infoErr
}
func (s jetStreamStub) PullSubscribe(string, string, ...nats.SubOpt) (*nats.Subscription, error) {
	return nil, s.subscribeErr
}
func TestNATSDurableAcknowledgementGuards(t *testing.T) {
	event := distributed.Envelope{ID: "event", Type: "fact", SchemaVersion: 1, OccurredAt: time.Now(), TenantID: "t", ScopeKind: "tenant", ScopeID: "t", CorrelationID: "op", Producer: "producer", Payload: json.RawMessage(`{}`)}
	file := &nats.StreamInfo{Config: nats.StreamConfig{Storage: nats.FileStorage}}
	cause := errors.New("broker response lost")
	for _, stub := range []jetStreamStub{{infoErr: cause}, {}, {info: &nats.StreamInfo{Config: nats.StreamConfig{Storage: nats.MemoryStorage}}}, {info: file, publishErr: cause}, {info: file}, {info: file, ack: &nats.PubAck{Stream: "other", Sequence: 1}}, {info: file, ack: &nats.PubAck{Stream: "stream"}}} {
		b := &Broker{js: stub, stream: "stream", subject: "fact"}
		if e := b.Send(context.Background(), event); e == nil {
			t.Fatal("invalid durability confirmation accepted")
		}
	}
	b := &Broker{js: jetStreamStub{info: file, ack: &nats.PubAck{Stream: "stream", Sequence: 1}, subscribeErr: cause}, stream: "stream", subject: "fact"}
	if e := b.Send(context.Background(), event); e != nil {
		t.Fatal(e)
	}
	if e := b.Provision(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Subscribe(""); e == nil {
		t.Fatal("ephemeral consumer")
	}
	if _, e := b.Subscribe("durable"); !errors.Is(e, cause) {
		t.Fatal(e)
	}
}
