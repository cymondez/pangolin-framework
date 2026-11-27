// Root-mode operation scopes. The restricted lifecycle resolver deliberately
// opens and closes no ephemeral scope itself, so Operation-timed services are
// unreachable from module Start/Stop hooks unless the module opens an explicit
// scope of its own. OpenScope turns that resolver into a module-owned
// di.Scope: the underlying container scope is created here, every Resolve
// keeps exactly the sealed composition's immutable module/direct-contract-owner
// visibility, and Close owns the inner scope's cleanup. The root runtime, the
// composition engine and the raw container are never exposed, and the owner
// binding is fixed at creation.
package module

import (
	"context"
	"fmt"

	"github.com/cymondez/pangolin-framework/kernel/di"
)

// OpenScope opens one operation scope in root mode for the module that owns
// resolver: pass the restricted lifecycle resolver received through
// Runtime.Services() in a Start/Stop hook (or returned by
// Composition.LifecycleResolver(owner)). The returned di.Scope resolves
// Operation-timed services with exactly the same immutable visibility as the
// lifecycle resolver — the owner module's own registrations (including its
// owner-private interface aliases) plus direct dependencies' exported tokens —
// and never allows a different owner binding or a foreign private alias.
//
// The underlying scope is the sealed container's own OpenScope implementation;
// the returned wrapper keeps holding it. Close runs the inner scope's bounded
// cleanup once and is idempotent; cancellation and closure of the context
// passed here are handled by the existing inner scope semantics.
//
// Errors: a nil context, a nil resolver, a typed-nil lifecycle resolver or any
// other resolver implementation (including the raw container or a delegating
// wrapper) is rejected; container failures (closed container, canceled
// context) surface unchanged from the inner OpenScope.
func OpenScope(ctx context.Context, resolver di.Resolver) (di.Scope, error) {
	if contextIsNil(ctx) {
		return nil, fmt.Errorf("module OpenScope requires a context")
	}
	if resolver == nil {
		return nil, fmt.Errorf("module OpenScope requires a restricted lifecycle resolver")
	}
	lr, ok := resolver.(*lifecycleResolver)
	if !ok || lr == nil {
		return nil, fmt.Errorf("module OpenScope accepts only the restricted lifecycle resolver of a sealed composition; unsupported resolver %T", resolver)
	}
	return lr.openOperationScope(ctx)
}

// openOperationScope validates the same post-seal state as a Resolve call,
// creates the underlying container scope and wraps it. The composition lock is
// released before the container is consulted so the wrapper never holds the
// engine lock across scope creation or later resolves.
func (r *lifecycleResolver) openOperationScope(ctx context.Context) (di.Scope, error) {
	r.c.mu.Lock()
	if _, ok := r.c.modules[r.owner]; !ok {
		r.c.mu.Unlock()
		return nil, fmt.Errorf("composition has no module %s", r.owner)
	}
	if !r.c.sealed || r.c.container == nil {
		r.c.mu.Unlock()
		return nil, fmt.Errorf("composition is not sealed")
	}
	container := r.c.container
	r.c.mu.Unlock()
	inner, err := container.OpenScope(ctx)
	if err != nil {
		return nil, err
	}
	return &moduleScope{c: r.c, owner: r.owner, inner: inner}, nil
}

// moduleScope is the deliberately unexported wrapper handed out by OpenScope.
// It pins the owner for the lifetime of the scope: Resolve re-checks the same
// immutable module/direct-contract-owner visibility as the lifecycleResolver
// before delegating to the inner container scope, so no caller can switch
// owners, reach an engine handle or recover the raw container through it.
// Resources registered by factories live in the inner scope and are cleaned up
// by inner Close while the wrapper only passes the call through.
type moduleScope struct {
	c     *composition
	owner string
	inner di.Scope
}

func (s *moduleScope) Resolve(ctx context.Context, key di.Token) (any, error) {
	id, err := keyOf(key)
	if err != nil {
		return nil, err
	}
	if contextIsNil(ctx) {
		return nil, fmt.Errorf("DI context is required")
	}
	s.c.mu.Lock()
	if _, ok := s.c.modules[s.owner]; !ok {
		s.c.mu.Unlock()
		return nil, fmt.Errorf("composition has no module %s", s.owner)
	}
	if !s.c.sealed || s.c.container == nil {
		s.c.mu.Unlock()
		return nil, fmt.Errorf("composition is not sealed")
	}
	inner := s.inner
	// Same owner-private rule as the lifecycle resolver: the pinned owner
	// resolves its own private interface parameters through the engine alias
	// table or, for a unique lifecycle-only implementation, through the
	// dynamic own-provider selection; foreign private aliases and alias
	// identities stay invisible.
	tok, handled, err := s.c.restrictedResolveTokenLocked(s.owner, key, id)
	if err != nil {
		s.c.mu.Unlock()
		return nil, err
	}
	if !handled {
		if vErr := s.c.visibilityLocked(s.owner, id); vErr != nil {
			s.c.mu.Unlock()
			return nil, vErr
		}
	}
	s.c.mu.Unlock()
	return inner.Resolve(ctx, tok)
}

func (s *moduleScope) Close(ctx context.Context) error {
	return s.inner.Close(ctx)
}
