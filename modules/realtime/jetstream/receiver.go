// Package jetstream connects the realtime Hub to independent NATS consumers.
package jetstream

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/nats"
	"github.com/cymondez/pangolin-framework/modules/realtime"
	"github.com/cymondez/pangolin-framework/observability"
)

// Route sends realtime events to a dedicated stream and preserves the existing
// route for all other events in the shared Outbox.
type Route struct {
	Realtime distributed.BrokerPublisher
	Default  distributed.BrokerPublisher
}

func (r Route) Send(ctx context.Context, event distributed.Envelope) error {
	target := r.Default
	if event.Type == realtime.EventType {
		target = r.Realtime
	}
	if target == nil {
		return realtime.ErrInvalid
	}
	if err := target.Send(ctx, event); err != nil {
		return err
	}
	if event.Type == realtime.EventType {
		observability.Record(ctx, "realtime_broker_accepted")
	}
	return nil
}

type Receiver struct {
	sub      *nats.LiveSubscription
	hub      *realtime.Hub
	cancel   context.CancelFunc
	done     chan struct{}
	cleanup  sync.Once
	closeErr error
}

// Start creates the consumer before returning readiness. Only this receiver
// consumes its consumer identity. Broker connections are owned by composition.
func Start(ctx context.Context, broker *nats.Broker, hub *realtime.Hub, dedupCapacity int) (*Receiver, error) {
	if hub == nil || dedupCapacity <= 0 {
		return nil, realtime.ErrInvalid
	}
	sub, err := broker.SubscribeLive(ctx)
	if err != nil {
		return nil, err
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r := &Receiver{sub: sub, hub: hub, cancel: cancel, done: make(chan struct{})}
	go r.run(run, dedupCapacity)
	return r, nil
}

func (r *Receiver) run(ctx context.Context, capacity int) {
	defer close(r.done)
	ids := make([]string, capacity)
	recent := make(map[string]struct{}, capacity)
	cursor := 0
	for ctx.Err() == nil {
		fetch, finish := context.WithTimeout(ctx, 2*time.Second)
		item, err := r.sub.Next(fetch)
		finish()
		if err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		if item.Invalid {
			_ = item.Reject()
			continue
		}
		id := item.Envelope.ID
		if _, duplicate := recent[id]; !duplicate {
			err = r.hub.AcceptEvent(ctx, item.Envelope)
			if err != nil {
				if errors.Is(err, realtime.ErrInvalid) {
					_ = item.Reject()
				}
				continue
			}
			delete(recent, ids[cursor])
			recent[id] = struct{}{}
			ids[cursor] = id
			cursor = (cursor + 1) % len(ids)
		}
		ack, finish := context.WithTimeout(ctx, time.Second)
		if err := item.Ack(ack); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "realtime node acknowledgement failed")
		}
		finish()
	}
}

func (r *Receiver) Quiesce(context.Context) error { r.cancel(); return nil }
func (r *Receiver) Cancel()                       { r.cancel() }

func (r *Receiver) Drain(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Receiver) Close(ctx context.Context) error {
	r.cancel()
	err := r.Drain(ctx)
	r.cleanup.Do(func() { r.closeErr = r.sub.Close(ctx) })
	return errors.Join(err, r.closeErr)
}
