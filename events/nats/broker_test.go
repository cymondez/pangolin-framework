package nats

import (
	"context"
	"github.com/cymondez/pangolin-framework/events/distributed"
	"testing"
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
