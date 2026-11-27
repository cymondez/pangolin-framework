package realtime

// Schema and ingress payload validation coverage (notifications.go), see spec
// §6/§13 and tests/contract/m7.json for the scenario mapping.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNotificationSchemaContract(t *testing.T) {
	schema := Schema()
	if schema.Type != EventType || schema.Version != 1 {
		t.Fatalf("unexpected schema identity %+v", schema)
	}
	if len(schema.Required) != 2 || schema.Required[0] != "channel" || schema.Required[1] != "data" {
		t.Fatalf("unexpected schema fields %v", schema.Required)
	}
	for name, raw := range map[string]json.RawMessage{
		"valid":         json.RawMessage(`{"channel":"updates","data":42}`),
		"optional-user": json.RawMessage(`{"channel":"updates","user_id":"same-user","data":42}`),
	} {
		if err := schema.Validate(raw); err != nil {
			t.Fatalf("%s payload rejected: %v", name, err)
		}
	}
	for name, raw := range map[string]json.RawMessage{
		"unknown-field":  json.RawMessage(`{"channel":"updates","data":42,"extra":1}`),
		"malformed":      json.RawMessage(`{"channel":"updates","data":`),
		"missing-data":   json.RawMessage(`{"channel":"updates"}`),
		"bad-channel":    json.RawMessage(`{"channel":"updates channel","data":42}`),
		"empty-channel":  json.RawMessage(`{"channel":"","data":42}`),
		"long-user":      json.RawMessage(`{"channel":"updates","user_id":"` + strings.Repeat("u", 257) + `","data":42}`),
		"non-json-value": json.RawMessage(`[]`),
	} {
		err := schema.Validate(raw)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s payload accepted: %v", name, err)
		}
	}
}
