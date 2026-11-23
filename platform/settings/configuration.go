package settings

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/cymondez/pangolin-framework/kernel/config"
)

// ConfigurationProvider uses ABP's exact Settings:+name lookup. Configuration
// text becomes a JSON string unless a converter is explicitly registered.
type ConfigurationProvider struct {
	base          Provider
	configuration config.Reader
	converters    map[string]func(string) (json.RawMessage, error)
}
type conversionError struct{ cause error }

func (e conversionError) Error() string { return "SETTING_CONFIGURATION_INVALID" }
func (e conversionError) Unwrap() error { return e.cause }

func WithConfiguration(base Provider, configuration config.Reader, converters map[string]func(string) (json.RawMessage, error)) (*ConfigurationProvider, error) {
	if base == nil || configuration == nil {
		return nil, ErrInvalid
	}
	p := &ConfigurationProvider{base: base, configuration: configuration, converters: map[string]func(string) (json.RawMessage, error){}}
	for key, convert := range converters {
		if key == "" || convert == nil {
			return nil, ErrInvalid
		}
		p.converters[key] = convert
	}
	return p, nil
}
func (p *ConfigurationProvider) Read(ctx context.Context, layers []Layer, key string) (Snapshot, error) {
	snapshot, err := p.base.Read(ctx, layers, key)
	if err != nil {
		return Snapshot{}, err
	}
	application := false
	for _, layer := range layers {
		if layer == (Layer{}) {
			application = true
			break
		}
	}
	if !application {
		return snapshot, nil
	}
	value, found := p.configuration.Lookup("Settings:" + key)
	stamp, _ := json.Marshal([]any{snapshot.Revision, found, value})
	snapshot.Revision = fmt.Sprintf("%x", sha256.Sum256(stamp))
	if !found {
		return snapshot, nil
	}
	var encoded json.RawMessage
	if convert := p.converters[key]; convert != nil {
		encoded, err = convert(value)
	} else {
		encoded, err = json.Marshal(value)
	}
	if err != nil {
		return Snapshot{}, conversionError{err}
	}
	if !json.Valid(encoded) {
		return Snapshot{}, ErrInvalid
	}
	snapshot.Values = append(snapshot.Values, append(json.RawMessage(nil), encoded...))
	return snapshot, nil
}
func (p *ConfigurationProvider) Put(ctx context.Context, layer Layer, key string, value json.RawMessage) (uint64, error) {
	w, ok := p.base.(interface {
		Put(context.Context, Layer, string, json.RawMessage) (uint64, error)
	})
	if !ok {
		return 0, ErrInvalid
	}
	return w.Put(ctx, layer, key, value)
}
