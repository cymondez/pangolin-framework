package distributed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/cymondez/pangolin-framework/security/scope"
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
	TraceParent   string          `json:"traceparent,omitempty"`
}

func validate(e Envelope) error {
	if e.ID == "" || e.Type == "" || e.SchemaVersion <= 0 || e.OccurredAt.IsZero() || scope.Validate(scope.ScopeRef{TenantID: e.TenantID, Kind: e.ScopeKind, ID: e.ScopeID}) != nil || e.CorrelationID == "" || e.Producer == "" || !json.Valid(e.Payload) || !bytes.HasPrefix(bytes.TrimSpace(e.Payload), []byte("{")) {
		return fmt.Errorf("invalid event envelope")
	}
	return nil
}

// Encode preserves the versioned snake_case wire envelope, bounded to 1 MiB.
func Encode(e Envelope) ([]byte, error) {
	if err := validate(e); err != nil {
		return nil, err
	}
	// An absent cause is an explicit JSON null, never an invented operation.
	var raw []byte
	var err error
	if e.CausationID == "" {
		w := struct {
			wireEnvelope
			Cause any `json:"causation_id"`
		}{wireEnvelope: wireEnvelope(e)}
		raw, err = json.Marshal(w)
	} else {
		raw, err = json.Marshal(wireEnvelope(e))
	}
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
