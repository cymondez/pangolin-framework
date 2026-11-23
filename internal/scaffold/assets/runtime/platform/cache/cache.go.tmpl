// Package cache provides a bounded, exact-owner memory cache.
package cache

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrInvalid = errors.New("CACHE_INVALID")

type Resource struct {
	Module, Kind, ID string
	Private          bool // Include the current principal and authority revision.
}

// Key derives security fields from a trusted binding, never from resource DTOs.
func Key(ctx context.Context, application, version string, r Resource) (string, error) {
	b, ok := scope.Current(ctx)
	if !ok {
		return "", scope.ErrDenied
	}
	parts := []string{application, version, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID, r.Module, r.Kind, r.ID}
	if r.Private {
		parts = append(parts, b.Principal.Kind, b.Principal.ID, b.AuthorityRevision)
	}
	for i, p := range parts {
		if strings.TrimSpace(p) != p || p == "" {
			return "", ErrInvalid
		}
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/"), nil
}

type Stats struct{ Hits, Misses, Loads, Coalesced, Invalidations, Evictions uint64 }
type Invalidation struct {
	Application, Version string
	Scope                scope.ScopeRef
	Resource             Resource
}
type Publisher interface {
	Publish(context.Context, Invalidation) error
}
type entry struct {
	value   []byte
	expires time.Time
}
type flight struct {
	done  chan struct{}
	value []byte
	err   error
	epoch uint64
}
type Memory struct {
	mu                   sync.Mutex
	application, version string
	capacity             int
	now                  func() time.Time
	entries              map[string]entry
	flights              map[string]*flight
	epoch                uint64
	stats                Stats
	publisher            Publisher
}

func NewMemory(application, version string, capacity int, publishers ...Publisher) (*Memory, error) {
	if strings.TrimSpace(application) != application || application == "" || strings.TrimSpace(version) != version || version == "" || capacity < 1 || len(publishers) > 1 || len(publishers) == 1 && publishers[0] == nil {
		return nil, ErrInvalid
	}
	m := &Memory{application: application, version: version, capacity: capacity, now: time.Now, entries: map[string]entry{}, flights: map[string]*flight{}}
	if len(publishers) == 1 {
		m.publisher = publishers[0]
	}
	return m, nil
}

func (m *Memory) Get(ctx context.Context, r Resource) ([]byte, bool, error) {
	k, err := Key(ctx, m.application, m.version, r)
	if err != nil {
		return nil, false, err
	}
	if err = ctx.Err(); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[k]
	if ok && m.now().Before(e.expires) {
		m.stats.Hits++
		return append([]byte(nil), e.value...), true, nil
	}
	delete(m.entries, k)
	m.stats.Misses++
	return nil, false, nil
}

func (m *Memory) put(k string, value []byte, ttl time.Duration) {
	if _, ok := m.entries[k]; !ok && len(m.entries) >= m.capacity {
		var victim string
		var earliest time.Time
		for key, e := range m.entries {
			if victim == "" || e.expires.Before(earliest) {
				victim, earliest = key, e.expires
			}
		}
		delete(m.entries, victim)
		m.stats.Evictions++
	}
	m.entries[k] = entry{append([]byte(nil), value...), m.now().Add(ttl)}
}

func (m *Memory) Set(ctx context.Context, r Resource, value []byte, ttl time.Duration) error {
	k, err := Key(ctx, m.application, m.version, r)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		return ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.epoch++
	m.put(k, value, ttl)
	return nil
}

// Delete fences in-flight fills as well as removing the current entry.
func (m *Memory) Delete(ctx context.Context, r Resource) error {
	k, err := Key(ctx, m.application, m.version, r)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	m.epoch++
	delete(m.entries, k)
	m.stats.Invalidations++
	m.mu.Unlock()
	if m.publisher != nil {
		b, _ := scope.Current(ctx)
		return m.publisher.Publish(ctx, Invalidation{Application: m.application, Version: m.version, Scope: b.Ref, Resource: r})
	}
	return nil
}

// Apply consumes an already authenticated invalidation in a reauthorized scope.
// It removes all principal/revision variants of that exact resource and never
// republishes. A publisher outage is bounded by the caller's source version/TTL.
func (m *Memory) Apply(ctx context.Context, event Invalidation) error {
	b, ok := scope.Current(ctx)
	if !ok || b.Ref != event.Scope || event.Application != m.application || event.Version != m.version {
		return scope.ErrDenied
	}
	r := event.Resource
	r.Private = false
	k, err := Key(ctx, m.application, m.version, r)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.epoch++
	for key := range m.entries {
		if key == k || strings.HasPrefix(key, k+"/") {
			delete(m.entries, key)
		}
	}
	m.stats.Invalidations++
	return nil
}

// Load coalesces misses. Waiters can cancel independently. A cancelled or failed
// loader never fills the cache; the caller owns the loader's deadline.
func (m *Memory) Load(ctx context.Context, r Resource, ttl time.Duration, load func(context.Context) ([]byte, error)) ([]byte, error) {
	if ttl <= 0 || load == nil {
		return nil, ErrInvalid
	}
	if value, ok, err := m.Get(ctx, r); err != nil || ok {
		return value, err
	}
	k, _ := Key(ctx, m.application, m.version, r)
	m.mu.Lock()
	// Another fill may have completed between Get and acquiring the lock.
	if e, ok := m.entries[k]; ok && m.now().Before(e.expires) {
		m.mu.Unlock()
		return append([]byte(nil), e.value...), nil
	}
	if f := m.flights[k]; f != nil {
		m.stats.Coalesced++
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
			return append([]byte(nil), f.value...), f.err
		}
	}
	f := &flight{done: make(chan struct{}), epoch: m.epoch}
	m.flights[k] = f
	m.stats.Loads++
	m.mu.Unlock()
	// Cleanup on panic releases waiters while preserving the loader's panic.
	defer func() {
		if p := recover(); p != nil {
			m.finish(k, f, nil, ErrInvalid, ttl)
			panic(p)
		}
	}()
	value, err := load(ctx)
	if err == nil {
		err = ctx.Err()
	}
	m.finish(k, f, value, err, ttl)
	return append([]byte(nil), value...), err
}
func (m *Memory) finish(k string, f *flight, value []byte, err error, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f.value, f.err = append([]byte(nil), value...), err
	if err == nil && f.epoch == m.epoch {
		m.put(k, value, ttl)
	}
	delete(m.flights, k)
	close(f.done)
}
func (m *Memory) Stats() Stats { m.mu.Lock(); defer m.mu.Unlock(); return m.stats }
