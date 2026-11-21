package distributed

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEnvelopeWireRoundTrip(t *testing.T) {
	e := Envelope{ID: "event", Type: "book.created", SchemaVersion: 1, OccurredAt: time.Now().UTC().Round(0), TenantID: "tenant-a", ScopeKind: "tenant", ScopeID: "tenant-a", CorrelationID: "operation", CausationID: "operation", Producer: "bookstore", Payload: json.RawMessage(`{"title":"book"}`)}
	raw, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"event_id", "event_type", "schema_version", "tenant_id", "scope_kind", "scope_id", "correlation_id", "causation_id", "producer", "payload", "occurred_at"} {
		if !strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatal(key)
		}
	}
	got, err := Decode(raw)
	if err != nil || !reflect.DeepEqual(got, e) {
		t.Fatal(got, err)
	}
	if _, err = Encode(Envelope{}); err == nil {
		t.Fatal("invalid envelope")
	}
	if _, err = Decode([]byte(`{`)); err == nil {
		t.Fatal("invalid JSON")
	}
	if _, err = Decode([]byte(`{}`)); err == nil {
		t.Fatal("missing fields")
	}
	if _, err = Decode([]byte(strings.Repeat(" ", 1<<20+1))); err == nil {
		t.Fatal("large")
	}
	e.Payload = json.RawMessage(`"` + strings.Repeat("x", 1<<20) + `"`)
	if _, err = Encode(e); err == nil {
		t.Fatal("large payload")
	}
}
