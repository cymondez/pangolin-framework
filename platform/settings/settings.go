// Package settings resolves versioned settings through trusted scope ancestry.
// Standard layers are explicitly ordered Default < Config < Global < Tenant <
// Enterprise < Organization < User; Enterprise and Organization participate
// only for their trusted scope kinds.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/pangolin-framework/pangolin/platform/cache"
	"github.com/pangolin-framework/pangolin/security/policy"
	"github.com/pangolin-framework/pangolin/security/scope"
)

var ErrInvalid = errors.New("SETTING_INVALID")

// LayerName explicitly distinguishes the standard settings layers. The
// resolution precedence from highest to lowest is User > Organization >
// Enterprise > Tenant > Global > Config > Default. Enterprise and Organization
// participate only for their trusted scope kinds, so a tenant-only composition
// resolves exactly the five standard layers and never invents organization
// layers.
type LayerName string

const (
	LayerUser         LayerName = "user"
	LayerOrganization LayerName = "organization"
	LayerEnterprise   LayerName = "enterprise"
	LayerTenant       LayerName = "tenant"
	LayerGlobal       LayerName = "global"
	LayerConfig       LayerName = "config"
	LayerDefault      LayerName = "default"
)

// Layer is a provider address. User is always local to Ref, including tenant
// scope. The empty Layer (zero Ref and zero User) addresses the Global layer,
// the mutable composition-owned application-wide store; configuration is a
// separate standard layer below Global and never stores through this address.
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
// Result carries the winning value and every standard candidate in precedence
// order, highest priority first, ending with the declared default.
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
func ownLayerName(kind string) LayerName {
	switch kind {
	case "tenant":
		return LayerTenant
	case "org.enterprise":
		return LayerEnterprise
	case "org.organization":
		return LayerOrganization
	default:
		return LayerName(kind)
	}
}

// standardOrder is the exact layer sequence for one resolution: names list
// every standard layer, addrs lists the provider addresses that can store a
// value. Config and Default carry no address: configuration enters through a
// Provider (WithConfiguration) and the default lives in Definition.
type standardOrder struct {
	names []LayerName
	addrs []Layer
}

func (s *Service) order(ctx context.Context, b scope.ScopeBinding, inherit bool) (standardOrder, error) {
	o := standardOrder{
		names: []LayerName{LayerUser, ownLayerName(b.Ref.Kind)},
		addrs: []Layer{{Ref: b.Ref, User: b.Principal}, {Ref: b.Ref}},
	}
	if !inherit {
		o.names = append(o.names, LayerDefault)
		return o, nil
	}
	switch b.Ref.Kind {
	case "org.organization":
		if s.ancestry == nil {
			return standardOrder{}, scope.ErrAuthorityUnavailable
		}
		parent, ok, err := s.ancestry.Parent(ctx, b)
		if err != nil {
			return standardOrder{}, err
		}
		if ok {
			if scope.Validate(parent) != nil || parent.TenantID != b.Ref.TenantID || parent.Kind != "org.enterprise" {
				return standardOrder{}, scope.ErrDenied
			}
			o.names = append(o.names, LayerEnterprise)
			o.addrs = append(o.addrs, Layer{Ref: parent})
		}
	case "org.enterprise", "tenant":
	default:
		return standardOrder{}, scope.ErrKindUnsupported
	}
	if b.Ref.Kind != "tenant" {
		o.names = append(o.names, LayerTenant)
		o.addrs = append(o.addrs, Layer{Ref: scope.ScopeRef{TenantID: b.Ref.TenantID, Kind: "tenant", ID: b.Ref.TenantID}})
	}
	o.names = append(o.names, LayerGlobal, LayerConfig, LayerDefault)
	o.addrs = append(o.addrs, Layer{})
	return o, nil
}

// Layers returns the exact standard layers Resolve consults for key, ordered
// from highest to lowest priority. Tenant-only bindings resolve the five
// standard layers; enterprise and standalone-organization bindings resolve
// six; organizations under an enterprise resolve seven. Definitions without
// Inherit consult only the user and own-scope layers before Default. Config
// yields values only when a configuration source is wired through
// WithConfiguration.
func (s *Service) Layers(ctx context.Context, key string) ([]LayerName, error) {
	b, ok := scope.Current(ctx)
	if !ok {
		return nil, scope.ErrDenied
	}
	d, ok := s.definitions[key]
	if !ok {
		return nil, ErrInvalid
	}
	o, err := s.order(ctx, b, d.Inherit)
	if err != nil {
		return nil, err
	}
	return o.names, nil
}

// Resolve returns the highest-priority standard value for key, walking the
// explicit standard layers: the owner-local user layer, the own-scope layer,
// then for inheriting definitions the enterprise parent, tenant, Global and
// Config, and finally the declared default.
func (s *Service) Resolve(ctx context.Context, key string) (Result, error) {
	b, ok := scope.Current(ctx)
	if !ok {
		return Result{}, scope.ErrDenied
	}
	d, ok := s.definitions[key]
	if !ok {
		return Result{}, ErrInvalid
	}
	o, err := s.order(ctx, b, d.Inherit)
	if err != nil {
		return Result{}, err
	}
	layers := o.addrs
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
// The empty Layer stores the Global layer shared by every tenant; every other
// address stores its exact owner.
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
