//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	broker "github.com/cymondez/pangolin-framework/events/nats"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/nats-io/nats.go"
)

func brokerFixture(t *testing.T, storage nats.StorageType, maxMessages int64) (*broker.Broker, nats.JetStreamContext, string, string) {
	t.Helper()
	address := os.Getenv("PANGOLIN_M1_NATS_URL")
	if address == "" {
		t.Fatal("PANGOLIN_M1_NATS_URL required")
	}
	nc, err := nats.Connect(address, nats.Timeout(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := nc.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	id := rand.Text()
	stream := "M3_" + id
	subject := "m3." + id + ".fact"
	_, err = js.AddStream(&nats.StreamConfig{Name: stream, Subjects: []string{subject}, Storage: storage, MaxMsgs: maxMessages, MaxMsgSize: 1 << 20, Discard: nats.DiscardNew, MaxAge: 168 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := js.DeleteStream(stream, nats.Context(ctx)); err != nil {
			t.Error(err)
		}
	})
	b, err := broker.Connect(address, stream, subject)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b, js, stream, subject
}
func brokerEnvelope(t *testing.T) distributed.Envelope {
	return envelope(eventContext(t, "m3-broker", scopeService), rand.Text())
}

var scopeService = scope.PrincipalRef{ID: "publisher", Kind: "service"}

func TestA52Broker(t *testing.T) {
	t.Run("durable-ack", func(t *testing.T) {
		b, js, stream, _ := brokerFixture(t, nats.FileStorage, -1)
		event := brokerEnvelope(t)
		if e := b.Send(context.Background(), event); e != nil {
			t.Fatal(e)
		}
		info, e := js.StreamInfo(stream)
		if e != nil || info.Config.Storage != nats.FileStorage || info.State.Msgs != 1 {
			t.Fatal(e, info)
		}
		s, e := b.Subscribe("m3_durable")
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		d, e := s.Next(ctx)
		if e != nil || d.Envelope.ID != event.ID {
			t.Fatal(e)
		}
		if e = d.Ack(ctx); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("unroutable", func(t *testing.T) {
		_, _, stream, subject := brokerFixture(t, nats.FileStorage, -1)
		b, e := broker.Connect(os.Getenv("PANGOLIN_M1_NATS_URL"), stream, subject+".missing")
		if e != nil {
			t.Fatal(e)
		}
		defer b.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if e = b.Send(ctx, brokerEnvelope(t)); e == nil {
			t.Fatal("unrouted message confirmed")
		}
	})
	t.Run("rejected", func(t *testing.T) {
		b, _, _, _ := brokerFixture(t, nats.FileStorage, 1)
		if e := b.Send(context.Background(), brokerEnvelope(t)); e != nil {
			t.Fatal(e)
		}
		if e := b.Send(context.Background(), brokerEnvelope(t)); e == nil {
			t.Fatal("discard-new reject marked successful")
		}
	})
	t.Run("timed-out", func(t *testing.T) {
		b, _, _, _ := brokerFixture(t, nats.FileStorage, -1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if e := b.Send(ctx, brokerEnvelope(t)); e == nil {
			t.Fatal("cancelled send confirmed")
		}
	})
	t.Run("memory-stream-rejected", func(t *testing.T) {
		b, _, _, _ := brokerFixture(t, nats.MemoryStorage, -1)
		if e := b.Send(context.Background(), brokerEnvelope(t)); e == nil {
			t.Fatal("memory stream called durable")
		}
	})
	t.Run("broker-restart", func(t *testing.T) {
		b, _, stream, _ := brokerFixture(t, nats.FileStorage, -1)
		event := brokerEnvelope(t)
		if e := b.Send(context.Background(), event); e != nil {
			t.Fatal(e)
		}
		if _, e := b.Subscribe("restart_durable"); e != nil {
			t.Fatal(e)
		}
		b.Close()
		requestBrokerRestart(t)
		var restarted *broker.Broker
		var err error
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			restarted, err = broker.Connect(os.Getenv("PANGOLIN_M1_NATS_URL"), stream, "m3."+strings.TrimPrefix(stream, "M3_")+".fact")
			if err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			t.Fatal(err)
		}
		defer restarted.Close()
		s, e := restarted.Subscribe("restart_durable")
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		delivery, e := s.Next(ctx)
		if e != nil || delivery.Envelope.ID != event.ID {
			t.Fatal("file message lost after broker restart", e)
		}
		if e = delivery.Ack(ctx); e != nil {
			t.Fatal(e)
		}
		t.Logf("stream=%s event=%s survived restart", stream, event.ID)
	})
}

func requestBrokerRestart(t *testing.T) {
	t.Helper()
	dir := os.Getenv("PANGOLIN_M3_BROKER_RESTART_DIR")
	if dir == "" {
		t.Fatal("restart acceptance requires PANGOLIN_M3_BROKER_RESTART_DIR and host fixture controller")
	}
	id := rand.Text()
	request := filepath.Join(dir, id+".request")
	done := filepath.Join(dir, id+".done")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(request, []byte("RESTART_NATS"), 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("broker restart controller did not complete within 45s")
		case <-tick.C:
			result, e := os.ReadFile(done)
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				t.Fatal(e)
			}
			if strings.TrimSpace(string(result)) != "ok" {
				t.Fatalf("broker restart failed: %s", result)
			}
			return
		}
	}
}
