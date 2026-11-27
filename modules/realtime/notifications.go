package realtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/outbox"
	"github.com/pangolin-framework/pangolin/observability"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/policy"
	"github.com/pangolin-framework/pangolin/security/scope"
)

const EventType = "pangolin.realtime.notification"

type notificationPayload struct {
	Channel string          `json:"channel"`
	UserID  string          `json:"user_id,omitempty"`
	Data    json.RawMessage `json:"data"`
}

type Notification struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

func Schema() distributed.Schema {
	return distributed.Schema{Type: EventType, Version: 1, Required: []string{"channel", "data"},
		Validate: func(raw json.RawMessage) error {
			_, err := decodePayload(raw)
			return err
		}}
}

func decodePayload(raw json.RawMessage) (notificationPayload, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var p notificationPayload
	if err := decoder.Decode(&p); err != nil {
		return p, errors.Join(ErrInvalid, err)
	}
	if !json.Valid(raw) || !name(p.Channel) || len(p.UserID) > 256 || len(p.Data) == 0 {
		return p, ErrInvalid
	}
	return p, nil
}

func (h *Hub) Publish(ctx context.Context, destination Destination, data json.RawMessage) (string, error) {
	if err := uow.Check(ctx, true); err != nil {
		return "", err
	}
	binding, bound := scope.Current(ctx)
	channel, ok := h.channel(destination.Scope, destination.Channel)
	if !ok || !bound || binding.Ref != destination.Scope {
		return "", uow.Fail(ctx, scope.ErrDenied)
	}
	if len(data) > h.options.MaxPayload || !json.Valid(data) || len(destination.UserID) > 256 {
		return "", uow.Fail(ctx, ErrInvalid)
	}
	if err := policy.Require(ctx, h.scopes, channel.Publish.Permission, channel.Publish.Resource); err != nil {
		return "", uow.Fail(ctx, err)
	}
	if h.outbox == nil {
		return "", uow.Fail(ctx, outbox.ErrUnavailable)
	}
	payload, err := json.Marshal(notificationPayload{destination.Channel, destination.UserID, data})
	if err != nil {
		return "", uow.Fail(ctx, err)
	}
	id := rand.Text()
	correlation := observability.OperationID(ctx)
	if correlation == "" {
		correlation = id
	}
	ref := destination.Scope
	event := distributed.Envelope{ID: id, Type: EventType, SchemaVersion: 1, OccurredAt: time.Now().UTC(),
		TenantID: ref.TenantID, ScopeKind: ref.Kind, ScopeID: ref.ID,
		CorrelationID: correlation, Producer: h.options.Producer, Payload: payload}
	if err := h.outbox.Publish(ctx, event); err != nil {
		return "", err
	}
	observability.Record(ctx, "realtime_staged")
	return id, nil
}

// AcceptEvent is the trusted broker ingress. Its caller must authenticate the
// route with broker ACLs and a subscription to the dedicated realtime subject.
// Successful handoff does not indicate a socket write or client receipt.
func (h *Hub) AcceptEvent(ctx context.Context, event distributed.Envelope) error {
	if event.Type != EventType || event.SchemaVersion != 1 || event.Producer != h.options.Producer ||
		event.ID == "" || len(event.ID) > 128 || len(event.Payload) > (1<<20) {
		return ErrInvalid
	}
	p, err := decodePayload(event.Payload)
	if err != nil {
		return err
	}
	key := subscription{scope.ScopeRef{TenantID: event.TenantID, Kind: event.ScopeKind, ID: event.ScopeID}, p.Channel}
	if _, ok := h.channel(key.ref, key.channel); !ok || len(p.Data) > h.options.MaxPayload {
		return ErrInvalid
	}
	ctx, done, err := h.work.Enter(ctx)
	if err != nil {
		return errors.Join(ErrClosed, err)
	}
	defer done()
	h.mu.Lock()
	sessions := make([]*Session, 0, len(h.sessions))
	for s := range h.sessions {
		if s.subject.Tenant == key.ref.TenantID && (p.UserID == "" || s.subject.Principal.ID == p.UserID) {
			sessions = append(sessions, s)
		}
	}
	h.mu.Unlock()
	for _, s := range sessions {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.enqueue(key, Notification{ID: event.ID, Data: p.Data})
	}
	observability.Record(ctx, "realtime_node_processed")
	return nil
}

func (s *Session) enqueue(key subscription, event Notification) {
	s.mu.Lock()
	revision := s.subscriptions[key]
	if revision == 0 || s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	select {
	case s.queue <- Delivery{s, key, revision, event}:
		s.mu.Unlock()
	default:
		s.mu.Unlock()
		observability.Record(s.ctx, "realtime_queue_full")
		s.Close()
	}
}

func (s *Session) Pending() <-chan Delivery { return s.queue }

// WriteDelivery checks current identity, membership and receive policy at the
// final admission point. A withdrawn subscription cannot consume an old queue item.
func (s *Session) WriteDelivery(item Delivery, write func(context.Context, string, Notification) error) error {
	if item.recipient != s || write == nil {
		return ErrInvalid
	}
	ctx, done, err := s.hub.work.Enter(s.ctx)
	if err != nil {
		return errors.Join(ErrClosed, err)
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, s.hub.options.OperationTimeout)
	defer cancel()
	s.mu.Lock()
	revision := s.subscriptions[item.subscription]
	s.mu.Unlock()
	if revision == 0 || revision != item.revision {
		return scope.ErrDenied
	}
	verified, err := s.verify(ctx)
	if err != nil {
		return err
	}
	channel := s.hub.channels[item.subscription.channel]
	bound, err := s.hub.bind(verified, item.subscription.ref, channel.Receive)
	s.mu.Lock()
	if s.subscriptions[item.subscription] != item.revision {
		s.mu.Unlock()
		return scope.ErrDenied
	}
	if err != nil {
		if errors.Is(err, scope.ErrDenied) || errors.Is(err, scope.ErrKindUnsupported) {
			delete(s.subscriptions, item.subscription)
		}
		s.mu.Unlock()
		return err
	}
	// The lock serializes subscription removal with the admission decision.
	// Network writes run after admission without holding the subscription lock.
	s.mu.Unlock()
	if err := write(bound, channel.Target, item.event); err != nil {
		return err
	}
	observability.Record(ctx, "realtime_socket_written")
	return nil
}
