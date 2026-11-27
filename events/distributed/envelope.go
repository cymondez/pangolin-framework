package distributed

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// EnvelopeOption customizes fields of an Envelope created by NewEnvelope.
type EnvelopeOption func(*Envelope)

// WithEventID overrides the generated event ID.
func WithEventID(id string) EnvelopeOption { return func(e *Envelope) { e.ID = id } }

// WithOccurredAt overrides the generated UTC event timestamp.
func WithOccurredAt(at time.Time) EnvelopeOption {
	return func(e *Envelope) { e.OccurredAt = at.UTC() }
}

// WithCorrelationID overrides the default UOW-derived correlation ID.
func WithCorrelationID(id string) EnvelopeOption { return func(e *Envelope) { e.CorrelationID = id } }

// WithCausationID overrides the default UOW-derived causation ID.
func WithCausationID(id string) EnvelopeOption { return func(e *Envelope) { e.CausationID = id } }

// WithTraceParent sets an explicit W3C traceparent header on the envelope.
func WithTraceParent(traceParent string) EnvelopeOption {
	return func(e *Envelope) { e.TraceParent = traceParent }
}

// NewEnvelope builds a validated integration Envelope from ctx:
//   - TenantID, ScopeKind, ScopeID are populated exclusively from scope.Current(ctx).
//   - ID defaults to rand.Text(); OccurredAt defaults to time.Now().UTC().
//   - CorrelationID and CausationID default to uow.Current(ctx).ID() when active.
//   - payload may be json.RawMessage / []byte (copied defensively) or any JSON
//     object value marshaled with json.Marshal.
func NewEnvelope(ctx context.Context, producer, eventType string, schemaVersion int, payload any, opts ...EnvelopeOption) (Envelope, error) {
	if ctx == nil {
		return Envelope{}, scope.ErrDenied
	}
	b, ok := scope.Current(ctx)
	if !ok {
		return Envelope{}, scope.ErrDenied
	}
	var raw json.RawMessage
	switch v := payload.(type) {
	case json.RawMessage:
		raw = append(json.RawMessage(nil), v...)
	case []byte:
		raw = append(json.RawMessage(nil), v...)
	default:
		encoded, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, err
		}
		raw = encoded
	}
	opID := ""
	if s, active := uow.Current(ctx); active {
		opID = s.ID()
	}
	e := Envelope{
		ID:            rand.Text(),
		Type:          eventType,
		SchemaVersion: schemaVersion,
		OccurredAt:    time.Now().UTC(),
		TenantID:      b.Ref.TenantID,
		ScopeKind:     b.Ref.Kind,
		ScopeID:       b.Ref.ID,
		CorrelationID: opID,
		CausationID:   opID,
		Producer:      producer,
		Payload:       raw,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&e)
		}
	}
	if err := validate(e); err != nil {
		return Envelope{}, err
	}
	return e, nil
}
