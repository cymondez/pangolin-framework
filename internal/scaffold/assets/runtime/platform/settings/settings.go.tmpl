// Package settings resolves versioned settings through trusted scope ancestry.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/cymondez/pangolin-framework/platform/cache"
	"github.com/cymondez/pangolin-framework/security/policy"
	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrInvalid = errors.New("SETTING_INVALID")

// Layer is a provider address. User is always local to Ref, including tenant scope.
// Empty Ref is reserved for application configuration at the composition boundary.
type Layer struct {
	Ref  scope.ScopeRef
	User scope.PrincipalRef
}
type Definition struct {
	Key      string
	Default  json.RawMessage
	Inherit  bool
	Validate func(json.RawMessage) error
}
type Snapshot struct {
	Version  uint64
	Values   []json.RawMessage
	Revision string
}
type Provider interface {
	Read(context.Context, []Layer, string) (Snapshot, error)
}

// Ancestry is trusted infrastructure. It returns the current parent, never a DTO hint.
type Ancestry interface {
	Parent(context.Context, scope.ScopeBinding) (scope.ScopeRef, bool, error)
}
type Cache interface {
	Get(context.Context, cache.Resource) ([]byte, bool, error)
	Set(context.Context, cache.Resource, []byte, time.Duration) error
	Delete(context.Context, cache.Resource) error
}
type Result struct {
	Value      json.RawMessage
	Version    uint64
	Candidates []json.RawMessage
}
type Service struct {
	provider    Provider
	cache       Cache
	ancestry    Ancestry
	ttl         time.Duration
	definitions map[string]Definition
}

func New(provider Provider, c Cache, ancestry Ancestry, ttl time.Duration, definitions ...Definition) (*Service, error) {
	if provider == nil || c == nil || ttl <= 0 {
		return nil, ErrInvalid
	}
	s := &Service{provider: provider, cache: c, ancestry: ancestry, ttl: ttl, definitions: map[string]Definition{}}
	for _, d := range definitions {
		if d.Key == "" || !json.Valid(d.Default) || d.Validate != nil && d.Validate(d.Default) != nil {
			return nil, ErrInvalid
		}
		if _, ok := s.definitions[d.Key]; ok {
			return nil, ErrInvalid
		}
		d.Default = append(json.RawMessage(nil), d.Default...)
		s.definitions[d.Key] = d
	}
	return s, nil
}
func (s *Service) layers(ctx context.Context, b scope.ScopeBinding, inherit bool) ([]Layer, error) {
	layers := []Layer{{Ref: b.Ref, User: b.Principal}, {Ref: b.Ref}}
	if !inherit {
		return layers, nil
	}
	switch b.Ref.Kind {
	case "org.organization":
		if s.ancestry == nil {
			return nil, scope.ErrAuthorityUnavailable
		}
		parent, ok, err := s.ancestry.Parent(ctx, b)
		if err != nil {
			return nil, err
		}
		if ok {
			if scope.Validate(parent) != nil || parent.TenantID != b.Ref.TenantID || parent.Kind != "org.enterprise" {
				return nil, scope.ErrDenied
			}
			layers = append(layers, Layer{Ref: parent})
		}
	case "org.enterprise", "tenant":
	default:
		return nil, scope.ErrKindUnsupported
	}
	if b.Ref.Kind != "tenant" {
		layers = append(layers, Layer{Ref: scope.ScopeRef{TenantID: b.Ref.TenantID, Kind: "tenant", ID: b.Ref.TenantID}})
	}
	return append(layers, Layer{}), nil
}
func (s *Service) Resolve(ctx context.Context, key string) (Result, error) {
	b, ok := scope.Current(ctx)
	if !ok {
		return Result{}, scope.ErrDenied
	}
	d, ok := s.definitions[key]
	if !ok {
		return Result{}, ErrInvalid
	}
	layers, err := s.layers(ctx, b, d.Inherit)
	if err != nil {
		return Result{}, err
	}
	r := cache.Resource{Module: "settings", Kind: "value", ID: key, Private: true}
	raw, hit, cacheErr := s.cache.Get(ctx, r)
	// Provider snapshots are authoritative even if deletion/notification fails.
	// Never serve an unverified old cache value during an authority outage.
	snapshot, err := s.provider.Read(ctx, layers, key)
	if err != nil {
		return Result{}, err
	}
	if hit && cacheErr == nil {
		var cached struct {
			Result   Result
			Layers   []Layer
			Revision string
		}
		if json.Unmarshal(raw, &cached) == nil && cached.Result.Version == snapshot.Version && cached.Revision == snapshot.Revision && slices.Equal(cached.Layers, layers) {
			return cached.Result, nil
		}
	}
	result := Result{Version: snapshot.Version}
	for _, value := range snapshot.Values {
		if !json.Valid(value) || d.Validate != nil && d.Validate(value) != nil {
			return Result{}, ErrInvalid
		}
		result.Candidates = append(result.Candidates, append(json.RawMessage(nil), value...))
	}
	result.Candidates = append(result.Candidates, append(json.RawMessage(nil), d.Default...))
	result.Value = append(json.RawMessage(nil), result.Candidates[0]...)
	encoded, _ := json.Marshal(struct {
		Result   Result
		Layers   []Layer
		Revision string
	}{result, layers, snapshot.Revision})
	// Cache is optional acceleration; authoritative reads remain usable.
	_ = s.cache.Set(ctx, r, encoded, s.ttl)
	return result, nil
}

type Change struct {
	Layer   Layer
	Key     string
	Version uint64
}
type Publisher interface {
	Publish(context.Context, Change) error
}
type PublisherFunc func(context.Context, Change) error

func (f PublisherFunc) Publish(c context.Context, v Change) error { return f(c, v) }

// MemoryProvider is a composition-owned source. Put is an administrative Port,
// never exposed directly to request bodies. Version increments are atomic.
type MemoryProvider struct {
	mu        sync.RWMutex
	values    map[Layer]map[string]json.RawMessage
	version   uint64
	publisher Publisher
}

func NewMemoryProvider(p Publisher) *MemoryProvider {
	return &MemoryProvider{values: map[Layer]map[string]json.RawMessage{}, publisher: p}
}
func (p *MemoryProvider) Read(ctx context.Context, layers []Layer, key string) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := Snapshot{Version: p.version}
	for _, l := range layers {
		if v, ok := p.values[l][key]; ok {
			s.Values = append(s.Values, append(json.RawMessage(nil), v...))
		}
	}
	return s, nil
}

// Put persists the value before publishing. A notification error returns the new
// version and preserves its cause, so callers do not mistake it for a rollback.
func (p *MemoryProvider) Put(ctx context.Context, l Layer, key string, value json.RawMessage) (uint64, error) {
	if ctx == nil || key == "" || !json.Valid(value) || l.Ref != (scope.ScopeRef{}) && scope.Validate(l.Ref) != nil || l.User != (scope.PrincipalRef{}) && (l.User.ID == "" || l.Ref == (scope.ScopeRef{}) || l.User.Kind != "user" && l.User.Kind != "service") {
		return 0, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	p.mu.Lock()
	if p.values[l] == nil {
		p.values[l] = map[string]json.RawMessage{}
	}
	p.values[l][key] = append(json.RawMessage(nil), value...)
	p.version++
	version := p.version
	p.mu.Unlock()
	if p.publisher != nil {
		if err := p.publisher.Publish(ctx, Change{l, key, version}); err != nil {
			return version, fmt.Errorf("setting stored at version %d; invalidation failed: %w", version, err)
		}
	}
	return version, nil
}

// Update changes only the current exact scope. Broader/user-layer management
// uses a separately authorized application Port, never a caller-supplied owner.
func (s *Service) Update(ctx context.Context, a policy.Authorizer, key string, value json.RawMessage) (uint64, error) {
	if err := policy.Require(ctx, a, "settings.write", nil); err != nil {
		return 0, err
	}
	d, ok := s.definitions[key]
	if !ok || !json.Valid(value) || d.Validate != nil && d.Validate(value) != nil {
		return 0, ErrInvalid
	}
	w, ok := s.provider.(interface {
		Put(context.Context, Layer, string, json.RawMessage) (uint64, error)
	})
	if !ok {
		return 0, ErrInvalid
	}
	b, _ := scope.Current(ctx)
	version, err := w.Put(ctx, Layer{Ref: b.Ref}, key, value)
	if version > 0 {
		err = errors.Join(err, s.cache.Delete(ctx, cache.Resource{Module: "settings", Kind: "value", ID: key, Private: true}))
	}
	return version, err
}
