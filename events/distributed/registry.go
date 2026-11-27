package distributed

import (
	"encoding/json"
	"errors"
	"fmt"
)

var ErrSchemaUnsupported = errors.New("EVENT_SCHEMA_UNSUPPORTED")

type Schema struct {
	Type     string
	Version  int
	Required []string
	Validate func(json.RawMessage) error
	// LegacyTenant permits only an explicitly registered old tenant-only format.
	LegacyTenant bool
}
type schemaKey struct {
	kind    string
	version int
}
type Registry struct{ schemas map[schemaKey]Schema }

func NewRegistry(schemas ...Schema) (*Registry, error) {
	r := &Registry{schemas: map[schemaKey]Schema{}}
	for _, s := range schemas {
		key := schemaKey{s.Type, s.Version}
		if s.Type == "" || s.Version <= 0 {
			return nil, ErrSchemaUnsupported
		}
		if _, exists := r.schemas[key]; exists {
			return nil, fmt.Errorf("duplicate immutable schema: %w", ErrSchemaUnsupported)
		}
		s.Required = append([]string(nil), s.Required...)
		for _, field := range s.Required {
			if field == "" {
				return nil, ErrSchemaUnsupported
			}
		}
		r.schemas[key] = s
	}
	return r, nil
}
func (r *Registry) check(e Envelope) error {
	if r == nil {
		return ErrSchemaUnsupported
	}
	s, ok := r.schemas[schemaKey{e.Type, e.SchemaVersion}]
	if !ok {
		return ErrSchemaUnsupported
	}
	if err := validate(e); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &object); err != nil {
		return err
	}
	for _, field := range s.Required {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("published field %s missing", field)
		}
	}
	if s.Validate != nil {
		return s.Validate(e.Payload)
	}
	return nil
}
func (r *Registry) Encode(e Envelope) ([]byte, error) {
	if err := r.check(e); err != nil {
		return nil, err
	}
	return Encode(e)
}
func (r *Registry) Decode(raw []byte) (Envelope, error) {
	if len(raw) > 1<<20 {
		return Envelope{}, fmt.Errorf("event too large")
	}
	var w wireEnvelope
	if err := json.Unmarshal(raw, &w); err != nil {
		return Envelope{}, err
	}
	e := Envelope(w)
	if r != nil {
		if s, ok := r.schemas[schemaKey{e.Type, e.SchemaVersion}]; ok && s.LegacyTenant && e.ScopeKind == "" && e.ScopeID == "" {
			e.ScopeKind = "tenant"
			e.ScopeID = e.TenantID
		}
	}
	return e, r.check(e)
}
