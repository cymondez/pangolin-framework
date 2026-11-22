package distributed

import (
	"encoding/json"
	"fmt"
	"time"
)

type wireEnvelope struct {
	ID            string          `json:"event_id"`
	Type          string          `json:"event_type"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	TenantID      string          `json:"tenant_id"`
	ScopeKind     string          `json:"scope_kind"`
	ScopeID       string          `json:"scope_id"`
	CorrelationID string          `json:"correlation_id"`
	CausationID   string          `json:"causation_id"`
	Producer      string          `json:"producer"`
	Payload       json.RawMessage `json:"payload"`
}

func validate(e Envelope) error {
	if e.ID == "" || e.Type == "" || e.SchemaVersion <= 0 || e.OccurredAt.IsZero() || e.TenantID == "" || e.ScopeKind == "" || e.ScopeID == "" || e.CorrelationID == "" || e.CausationID == "" || e.Producer == "" || !json.Valid(e.Payload) {
		return fmt.Errorf("invalid event envelope")
	}
	return nil
}

// Encode preserves the versioned snake_case wire envelope, bounded to 1 MiB.
func Encode(e Envelope) ([]byte, error) {
	if err := validate(e); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(wireEnvelope(e))
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("event too large")
	}
	return raw, err
}
func Decode(raw []byte) (Envelope, error) {
	var w wireEnvelope
	if len(raw) > 1<<20 {
		return Envelope{}, fmt.Errorf("event too large")
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return Envelope{}, err
	}
	e := Envelope(w)
	return e, validate(e)
}
