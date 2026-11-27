// Package distributed defines the integration envelope and publisher ports.
package distributed

import (
	"context"
	"encoding/json"
	"time"
)

type Envelope struct {
	ID            string
	Type          string
	SchemaVersion int
	OccurredAt    time.Time
	TenantID      string
	ScopeKind     string
	ScopeID       string
	CorrelationID string
	CausationID   string
	Producer      string
	Payload       json.RawMessage
	TraceParent   string
}
type IntegrationEventPublisher interface {
	Publish(context.Context, any) error
}
type BrokerPublisher interface {
	Send(context.Context, Envelope) error
}
