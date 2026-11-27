package outbox

import (
	"context"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/persistence/uow"
)

// EventSpec describes an integration event staged via Publisher.PublishJSON.
type EventSpec struct {
	Producer      string
	Type          string
	SchemaVersion int
	Options       []distributed.EnvelopeOption
}

// PublishJSON builds a context-bound Envelope (owner from scope.Current,
// default correlation/causation from uow.Current, generated ID and UTC
// timestamp, JSON-marshaled payload) and stages it in the active write UOW
// through Publisher.Publish, preserving full Registry schema validation,
// owner verification and traceparent injection.
func (p *Publisher) PublishJSON(ctx context.Context, spec EventSpec, payload any) error {
	if err := uow.Check(ctx, true); err != nil {
		return err
	}
	if p == nil || p.queue == nil {
		return uow.Fail(ctx, ErrUnavailable)
	}
	envelope, err := distributed.NewEnvelope(ctx, spec.Producer, spec.Type, spec.SchemaVersion, payload, spec.Options...)
	if err != nil {
		return uow.Fail(ctx, err)
	}
	return p.Publish(ctx, envelope)
}

// ProducerPublisher binds a default producer name to a Publisher for concise,
// one-line context-aware publishing inside write UOWs.
type ProducerPublisher struct {
	publisher *Publisher
	producer  string
}

// ForProducer returns a ProducerPublisher bound to producer.
func (p *Publisher) ForProducer(producer string) *ProducerPublisher {
	return &ProducerPublisher{publisher: p, producer: producer}
}

// PublishJSON stages a context-bound JSON event under the bound producer.
func (pp *ProducerPublisher) PublishJSON(ctx context.Context, eventType string, schemaVersion int, payload any, opts ...distributed.EnvelopeOption) error {
	if pp == nil || pp.publisher == nil {
		if err := uow.Check(ctx, true); err != nil {
			return err
		}
		return uow.Fail(ctx, ErrUnavailable)
	}
	return pp.publisher.PublishJSON(ctx, EventSpec{
		Producer:      pp.producer,
		Type:          eventType,
		SchemaVersion: schemaVersion,
		Options:       opts,
	}, payload)
}
