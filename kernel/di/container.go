package di

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	lifeCtx, cancel := context.WithCancel(context.Background())
	c := &container{providers: r.providers, singletons: map[tokenID]any{}, singletonFlights: map[tokenID]*flight{}, timeout: r.timeout, scopes: map[*operation]bool{}, lifeCtx: lifeCtx, lifeCancel: cancel, activeZero: closedSignal(), closeDone: make(chan struct{})}
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
type flight struct {
	done    chan struct{}
	value   any
	err     error
	waiters int
}
type container struct {
	mu               sync.Mutex
	providers        map[tokenID]Provider
	singletons       map[tokenID]any
	singletonFlights map[tokenID]*flight
	resources        []owned
	scopes           map[*operation]bool
	closed           bool
	active           int
	activeZero       chan struct{}
	closeDone        chan struct{}
	closeErr         error
	lifeCtx          context.Context
	lifeCancel       context.CancelFunc
	timeout          time.Duration
}
type operation struct {
	container  *container
	ctx        context.Context
	lifeCtx    context.Context
	cancel     context.CancelFunc
	values     map[tokenID]any
	flights    map[tokenID]*flight
	resources  []owned
	closed     bool
	active     int
	activeZero chan struct{}
	closeDone  chan struct{}
	closeErr   error
}

// factoryResolver is deliberately valid only during a factory invocation.
type factoryResolver struct {
	container *container
	scope     *operation
	allowed   map[tokenID]bool
	active    atomic.Bool
}

func (r *factoryResolver) Resolve(ctx context.Context, key Token) (any, error) {
	id, e := identity(key)
	if e != nil {
		return nil, e
	}
	if !r.active.Load() || !r.allowed[id] {
		return nil, fmt.Errorf("undeclared or expired factory dependency %s", id.name)
	}
	return r.container.resolve(ctx, id, r.scope)
}
func (c *container) Resolve(ctx context.Context, key Token) (any, error) {
	id, e := identity(key)
	if e != nil {
		return nil, e
	}
	if e := c.enter(nil); e != nil {
		return nil, e
	}
	defer c.leave(nil)
	return c.resolve(ctx, id, nil)
}
func (c *container) resolve(ctx context.Context, id tokenID, s *operation) (value any, err error) {
	if ctx == nil {
		return nil, fmt.Errorf("DI context is required")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	c.mu.Lock()
	if c.closed || s != nil && (s.closed || s.ctx.Err() != nil) {
		c.mu.Unlock()
		return nil, fmt.Errorf("DI scope is closed")
	}
	p, ok := c.providers[id]
	if !ok {
		c.mu.Unlock()
		return nil, fmt.Errorf("missing provider %s", id.name)
	}
	if p.Lifetime == Singleton {
		if v, ok := c.singletons[id]; ok {
			c.mu.Unlock()
			return v, nil
		}
	}
	if p.Lifetime == Operation {
		if s == nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("operation provider requires scope: %s", id.name)
		}
		if v, ok := s.values[id]; ok {
			c.mu.Unlock()
			return v, nil
		}
	}
	var flights map[tokenID]*flight
	if p.Lifetime == Singleton {
		flights = c.singletonFlights
	} else if p.Lifetime == Operation {
		flights = s.flights
	}
	if f := flights[id]; f != nil {
		f.waiters++
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			c.mu.Lock()
			f.waiters--
			c.mu.Unlock()
			return nil, ctx.Err()
		case <-c.lifeCtx.Done():
			c.mu.Lock()
			f.waiters--
			c.mu.Unlock()
			return nil, fmt.Errorf("DI container is closing")
		case <-scopeDone(s):
			c.mu.Lock()
			f.waiters--
			c.mu.Unlock()
			return nil, fmt.Errorf("DI scope is closing")
		case <-f.done:
			c.mu.Lock()
			f.waiters--
			value, err := f.value, f.err
			c.mu.Unlock()
			return value, err
		}
	}
	var f *flight
	if flights != nil {
		f = &flight{done: make(chan struct{})}
		flights[id] = f
	}
	c.mu.Unlock()
	r := &factoryResolver{container: c, scope: s, allowed: map[tokenID]bool{}}
	r.active.Store(true)
	for _, d := range p.DependsOn {
		key, _ := identity(d)
		r.allowed[key] = true
	}
	var cleanup Cleanup
	defer r.active.Store(false)
	factoryCtx, cancel := context.WithCancel(ctx)
	stopContainer := context.AfterFunc(c.lifeCtx, cancel)
	stopScope := func() bool { return true }
	if s != nil {
		stopScope = context.AfterFunc(s.lifeCtx, cancel)
	}
	defer func() { stopContainer(); stopScope(); cancel() }()
	func() {
		defer func() {
			if panicValue := recover(); panicValue != nil {
				r.active.Store(false)
				if f != nil {
					c.finishFlight(id, s, f, nil, fmt.Errorf("factory %s panicked", id.name))
				}
				panic(panicValue)
			}
		}()
		value, cleanup, err = p.Factory(factoryCtx, r)
		r.active.Store(false)
	}()
	if err == nil && (value == nil || !reflect.TypeOf(value).AssignableTo(id.typ)) {
		err = fmt.Errorf("provider %s returned incompatible type", id.name)
	}
	if err != nil {
		if cleanup != nil {
			err = errors.Join(err, c.clean(ctx, []owned{{cleanup}}))
		}
		err = fmt.Errorf("factory %s: %w", id.name, err)
		if f != nil {
			c.finishFlight(id, s, f, nil, err)
		}
		return nil, err
	}
	var closeErr error
	c.mu.Lock()
	if c.closed || s != nil && s.closed {
		closeErr = fmt.Errorf("DI container or scope closed during factory %s", id.name)
	} else {
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
	}
	c.mu.Unlock()
	if closeErr != nil {
		if cleanup != nil {
			closeErr = errors.Join(closeErr, c.clean(ctx, []owned{{cleanup}}))
		}
		if f != nil {
			c.finishFlight(id, s, f, nil, closeErr)
		}
		return nil, closeErr
	}
	if f != nil {
		c.finishFlight(id, s, f, value, nil)
	}
	return value, nil
}

func (c *container) finishFlight(id tokenID, s *operation, f *flight, value any, err error) {
	c.mu.Lock()
	f.value, f.err = value, err
	if c.providers[id].Lifetime == Operation && s != nil && s.flights[id] == f {
		delete(s.flights, id)
	}
	if c.providers[id].Lifetime == Singleton && c.singletonFlights[id] == f {
		delete(c.singletonFlights, id)
	}
	close(f.done)
	c.mu.Unlock()
}

func closedSignal() chan struct{} { ch := make(chan struct{}); close(ch); return ch }
func (c *container) enter(s *operation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || s != nil && s.closed {
		return fmt.Errorf("DI container or scope is closed")
	}
	if c.active == 0 {
		c.activeZero = make(chan struct{})
	}
	c.active++
	if s != nil {
		if s.active == 0 {
			s.activeZero = make(chan struct{})
		}
		s.active++
	}
	return nil
}
func (c *container) leave(s *operation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active--
	if c.active == 0 {
		close(c.activeZero)
	}
	if s != nil {
		s.active--
		if s.active == 0 {
			close(s.activeZero)
		}
	}
}
func scopeDone(s *operation) <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.lifeCtx.Done()
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
	lifeCtx, cancel := context.WithCancel(ctx)
	s := &operation{container: c, ctx: ctx, lifeCtx: lifeCtx, cancel: cancel, values: map[tokenID]any{}, flights: map[tokenID]*flight{}, activeZero: closedSignal(), closeDone: make(chan struct{})}
	c.scopes[s] = true
	return s, nil
}
func (s *operation) Resolve(ctx context.Context, key Token) (any, error) {
	c := s.container
	id, e := identity(key)
	if e != nil {
		return nil, e
	}
	if e := c.enter(s); e != nil {
		return nil, e
	}
	defer c.leave(s)
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
	if s.closed {
		done := s.closeDone
		c.mu.Unlock()
		return waitClose(ctx, done, c.timeout, func() error { c.mu.Lock(); defer c.mu.Unlock(); return s.closeErr })
	}
	s.closed = true
	s.cancel()
	wait := s.activeZero
	c.mu.Unlock()
	go func() {
		<-wait
		c.mu.Lock()
		resources := s.resources
		s.resources = nil
		c.mu.Unlock()
		err := c.clean(context.Background(), resources)
		c.mu.Lock()
		delete(c.scopes, s)
		s.closeErr = err
		close(s.closeDone)
		c.mu.Unlock()
	}()
	return waitClose(ctx, s.closeDone, c.timeout, func() error { c.mu.Lock(); defer c.mu.Unlock(); return s.closeErr })
}
func (c *container) Close(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		done := c.closeDone
		c.mu.Unlock()
		return waitClose(ctx, done, c.timeout, func() error { c.mu.Lock(); defer c.mu.Unlock(); return c.closeErr })
	}
	c.closed = true
	c.lifeCancel()
	wait := c.activeZero
	scopes := make([]*operation, 0, len(c.scopes))
	for s := range c.scopes {
		scopes = append(scopes, s)
	}
	c.mu.Unlock()
	go func() {
		<-wait
		var err error
		for _, s := range scopes {
			_ = s.Close(context.Background())
			<-s.closeDone
			c.mu.Lock()
			scopeErr := s.closeErr
			c.mu.Unlock()
			err = errors.Join(err, scopeErr)
		}
		c.mu.Lock()
		resources := append([]owned(nil), c.resources...)
		c.resources = nil
		c.mu.Unlock()
		err = errors.Join(err, c.clean(context.Background(), resources))
		c.mu.Lock()
		c.closeErr = err
		close(c.closeDone)
		c.mu.Unlock()
	}()
	return waitClose(ctx, c.closeDone, c.timeout, func() error { c.mu.Lock(); defer c.mu.Unlock(); return c.closeErr })
}

func waitClose(ctx context.Context, done <-chan struct{}, timeout time.Duration, result func() error) error {
	base := context.Background()
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			var cancel context.CancelFunc
			base, cancel = context.WithDeadline(base, deadline)
			defer cancel()
		}
	}
	waitCtx, cancel := context.WithTimeout(base, timeout)
	defer cancel()
	select {
	case <-done:
		return result()
	case <-waitCtx.Done():
		return waitCtx.Err()
	}
}
