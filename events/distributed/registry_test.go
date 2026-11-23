package distributed

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestA16Registry(t *testing.T) {
	r, e := NewRegistry(Schema{Type: "book.created", Version: 1, Required: []string{"id"}})
	if e != nil {
		t.Fatal(e)
	}
	event := Envelope{ID: "e", Type: "book.created", SchemaVersion: 1, OccurredAt: time.Now(), TenantID: "t", ScopeKind: "tenant", ScopeID: "t", CorrelationID: "op", Producer: "bookstore", Payload: json.RawMessage(`{"id":"1"}`)}
	t.Run("old-schema", func(t *testing.T) {
		raw, e := r.Encode(event)
		if e != nil {
			t.Fatal(e)
		}
		got, e := r.Decode(raw)
		if e != nil || got.ID != event.ID {
			t.Fatal(e)
		}
	})
	t.Run("optional-field", func(t *testing.T) {
		event.Payload = json.RawMessage(`{"id":"1","optional":true}`)
		if _, e := r.Encode(event); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("unknown-schema", func(t *testing.T) {
		event.SchemaVersion = 2
		if _, e := r.Encode(event); !errors.Is(e, ErrSchemaUnsupported) {
			t.Fatal(e)
		}
		event.SchemaVersion = 1
	})
	t.Run("breaking-change", func(t *testing.T) {
		event.Payload = json.RawMessage(`{"new_id":"1"}`)
		if _, e := r.Encode(event); e == nil {
			t.Fatal("missing published id accepted")
		}
		if _, e := NewRegistry(Schema{Type: "x", Version: 1}, Schema{Type: "x", Version: 1}); e == nil {
			t.Fatal("schema replaced")
		}
	})
}

func TestRegistryValidation(t *testing.T) {
	for _, s := range []Schema{{}, {Type: "fact", Version: -1}, {Type: "fact", Version: 1, Required: []string{""}}} {
		if _, e := NewRegistry(s); e == nil {
			t.Fatal(s)
		}
	}
	var nilRegistry *Registry
	if _, e := nilRegistry.Decode([]byte(`{}`)); !errors.Is(e, ErrSchemaUnsupported) {
		t.Fatal(e)
	}
	custom := errors.New("typed payload rejected")
	r, _ := NewRegistry(Schema{Type: "fact", Version: 1, Validate: func(json.RawMessage) error { return custom }})
	e := Envelope{ID: "e", Type: "fact", SchemaVersion: 1, OccurredAt: time.Now(), TenantID: "t", ScopeKind: "tenant", ScopeID: "t", CorrelationID: "op", Producer: "producer", Payload: json.RawMessage(`{}`)}
	if _, err := r.Encode(e); !errors.Is(err, custom) {
		t.Fatal(err)
	}
	if _, err := r.Decode([]byte(`{`)); err == nil {
		t.Fatal("invalid json")
	}
	if _, err := r.Decode(make([]byte, (1<<20)+1)); err == nil {
		t.Fatal("size")
	}
	e.Payload = json.RawMessage(`[]`)
	if _, err := r.Encode(e); err == nil {
		t.Fatal("nonobject payload")
	}
}
