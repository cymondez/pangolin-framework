package di

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type tokenID struct {
	name string
	typ  reflect.Type
}

func identity(t Token) (tokenID, error) {
	if t == nil || reflect.ValueOf(t).Kind() == reflect.Pointer && reflect.ValueOf(t).IsNil() {
		return tokenID{}, fmt.Errorf("nil DI token")
	}
	id := tokenID{t.Name(), t.Type()}
	if strings.TrimSpace(id.name) == "" || id.typ == nil {
		return id, fmt.Errorf("invalid DI token")
	}
	return id, nil
}

type registry struct {
	mu           sync.Mutex
	providers    map[tokenID]Provider
	sealed       bool
	timeout      time.Duration
	replacements []string
}

// NewRegistry requires a bounded cleanup budget shared by owned resources.
func NewRegistry(cleanupTimeout time.Duration) (Registry, error) {
	if cleanupTimeout <= 0 {
		return nil, fmt.Errorf("DI cleanup timeout must be positive")
	}
	return &registry{providers: map[tokenID]Provider{}, timeout: cleanupTimeout}, nil
}
func (r *registry) Provide(p Provider) error                { return r.put(p, "", false) }
func (r *registry) Replace(p Provider, reason string) error { return r.put(p, reason, true) }
func (r *registry) put(p Provider, reason string, replace bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return fmt.Errorf("DI registry is sealed")
	}
	id, e := identity(p.Key)
	if e != nil {
		return e
	}
	if p.Factory == nil || p.Lifetime > Operation {
		return fmt.Errorf("invalid provider %s", id.name)
	}
	_, exists := r.providers[id]
	if replace {
		if !exists || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("replacement requires an existing provider and reason: %s", id.name)
		}
		r.replacements = append(r.replacements, id.name+": "+reason)
	} else if exists {
		return fmt.Errorf("duplicate provider %s", id.name)
	}
	p.DependsOn = append([]Token(nil), p.DependsOn...)
	r.providers[id] = p
	return nil
}

func (r *registry) Seal(ctx context.Context) (Container, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed || ctx == nil {
		return nil, fmt.Errorf("DI registry sealed or context missing")
	}
	ids := make([]tokenID, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].name != ids[j].name {
			return ids[i].name < ids[j].name
		}
		return ids[i].typ.String() < ids[j].typ.String()
	})
	var visit func(tokenID, []string, map[tokenID]bool, bool) error
	visit = func(id tokenID, path []string, seen map[tokenID]bool, singleton bool) error {
		path = append(path, id.name)
		p, ok := r.providers[id]
		if !ok {
			return fmt.Errorf("missing provider: %s", strings.Join(path, " -> "))
		}
		if seen[id] {
			return fmt.Errorf("provider cycle: %s", strings.Join(path, " -> "))
		}
		if singleton && p.Lifetime == Operation {
			return fmt.Errorf("singleton captures operation: %s", strings.Join(path, " -> "))
		}
		seen[id] = true
		defer delete(seen, id)
		deps := map[tokenID]bool{}
		for _, dep := range p.DependsOn {
			d, e := identity(dep)
			if e != nil {
				return e
			}
			if deps[d] {
				return fmt.Errorf("duplicate dependency in %s", id.name)
			}
			deps[d] = true
			if e = visit(d, path, seen, singleton || p.Lifetime == Singleton); e != nil {
				return e
			}
		}
		return nil
	}
	for _, id := range ids {
		if e := visit(id, nil, map[tokenID]bool{}, false); e != nil {
			return nil, e
		}
	}
	r.sealed = true
	c := &container{providers: r.providers, singletons: map[tokenID]any{}, timeout: r.timeout, scopes: map[*operation]bool{}}
	defer func() {
		if p := recover(); p != nil {
			_ = c.Close(ctx)
			panic(p)
		}
	}()
	for _, id := range ids {
		if r.providers[id].Lifetime == Singleton {
			if _, e := c.resolve(ctx, id, nil); e != nil {
				return nil, errors.Join(e, c.Close(ctx))
			}
		}
	}
	return c, nil
}

type owned struct{ cleanup Cleanup }
type container struct {
	mu         sync.Mutex
	providers  map[tokenID]Provider
	singletons map[tokenID]any
	resources  []owned
	scopes     map[*operation]bool
	closed     bool
	timeout    time.Duration
}
type operation struct {
	container *container
	ctx       context.Context
	values    map[tokenID]any
	resources []owned
	closed    bool
}

// factoryResolver is deliberately valid only during a factory invocation.
type factoryResolver struct {
	container *container
	scope     *operation
	allowed   map[tokenID]bool
	active    bool
}

func (r *factoryResolver) Resolve(ctx context.Context, key Token) (any, error) {
	id, e := identity(key)
	if e != nil {
		return nil, e
	}
	if !r.active || !r.allowed[id] {
		return nil, fmt.Errorf("undeclared or expired factory dependency %s", id.name)
	}
	return r.container.resolve(ctx, id, r.scope)
}
func (c *container) Resolve(ctx context.Context, key Token) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, e := identity(key)
	if e != nil {
		return nil, e
	}
	return c.resolve(ctx, id, nil)
}
func (c *container) resolve(ctx context.Context, id tokenID, s *operation) (value any, err error) {
	if ctx == nil {
		return nil, fmt.Errorf("DI context is required")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if c.closed || s != nil && (s.closed || s.ctx.Err() != nil) {
		return nil, fmt.Errorf("DI scope is closed")
	}
	p, ok := c.providers[id]
	if !ok {
		return nil, fmt.Errorf("missing provider %s", id.name)
	}
	if p.Lifetime == Singleton {
		if v, ok := c.singletons[id]; ok {
			return v, nil
		}
	}
	if p.Lifetime == Operation {
		if s == nil {
			return nil, fmt.Errorf("operation provider requires scope: %s", id.name)
		}
		if v, ok := s.values[id]; ok {
			return v, nil
		}
	}
	r := &factoryResolver{container: c, scope: s, allowed: map[tokenID]bool{}, active: true}
	for _, d := range p.DependsOn {
		key, _ := identity(d)
		r.allowed[key] = true
	}
	var cleanup Cleanup
	defer func() {
		r.active = false
		if v := recover(); v != nil {
			panic(v)
		}
	}()
	value, cleanup, err = p.Factory(ctx, r)
	if err == nil && (value == nil || !reflect.TypeOf(value).AssignableTo(id.typ)) {
		err = fmt.Errorf("provider %s returned incompatible type", id.name)
	}
	if err != nil {
		if cleanup != nil {
			err = errors.Join(err, c.clean(ctx, []owned{{cleanup}}))
		}
		return nil, fmt.Errorf("factory %s: %w", id.name, err)
	}
	if cleanup != nil {
		if s != nil && p.Lifetime != Singleton {
			s.resources = append(s.resources, owned{cleanup})
		} else {
			c.resources = append(c.resources, owned{cleanup})
		}
	}
	if p.Lifetime == Singleton {
		c.singletons[id] = value
	}
	if p.Lifetime == Operation {
		s.values[id] = value
	}
	return value, nil
}
func (c *container) OpenScope(ctx context.Context) (Scope, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx == nil || c.closed {
		return nil, fmt.Errorf("DI context missing or container closed")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	s := &operation{container: c, ctx: ctx, values: map[tokenID]any{}}
	c.scopes[s] = true
	return s, nil
}
func (s *operation) Resolve(ctx context.Context, key Token) (any, error) {
	c := s.container
	c.mu.Lock()
	defer c.mu.Unlock()
	id, e := identity(key)
	if e != nil {
		return nil, e
	}
	return c.resolve(ctx, id, s)
}
func (c *container) clean(ctx context.Context, resources []owned) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.timeout)
	defer cancel()
	var err error
	for i := len(resources) - 1; i >= 0; i-- {
		err = errors.Join(err, resources[i].cleanup(cleanupCtx))
	}
	return err
}
func (s *operation) Close(ctx context.Context) error {
	c := s.container
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	delete(c.scopes, s)
	resources := s.resources
	s.resources = nil
	return c.clean(ctx, resources)
}
func (c *container) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	resources := append([]owned(nil), c.resources...)
	for s := range c.scopes {
		s.closed = true
		resources = append(resources, s.resources...)
		s.resources = nil
	}
	c.scopes = nil
	c.resources = nil
	return c.clean(ctx, resources)
}
