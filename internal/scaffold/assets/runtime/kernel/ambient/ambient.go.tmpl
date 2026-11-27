// Package ambient provides explicit, immutable Copy-on-Write ambient scope
// variables backed by context.Context.
//
// Each Var[T] instance owns a unique, unexported pointer key, eliminating
// string key collisions and allowing packages to keep their Var[T]
// package-private so external code cannot forge security or lifecycle state
// (such as tenant, principal, scope binding, or unit-of-work views).
//
// Nesting uses an immutable singly-linked frame chain stored inside
// context.Context. Pushing or clearing a value derives a new context without
// mutating any parent frame, sharing mutable counters, or using
// goroutine-local storage (GLS). Multiple goroutines can safely derive
// independent nested scopes from the same parent context concurrently.
package ambient

import "context"

// Slot is implemented exclusively by *Var[T]. It allows lifecycle boundaries
// such as uow.Detach to clear registered ambient variables without accepting
// arbitrary context-rewriting hooks.
type Slot interface {
	Clear(context.Context) context.Context
	ambientSlot()
}

// slotKey has non-zero size so every New[T]() allocates a distinct heap
// address that cannot be coalesced by the Go runtime.
type slotKey[T any] struct {
	_ byte
}

type frame[T any] struct {
	value   T
	present bool
	outer   *frame[T]
	depth   int
}

// Var is an explicit, concurrency-safe ambient scope variable bound to
// context.Context. Construct with New; the zero value is invalid for writes.
type Var[T any] struct {
	key *slotKey[T]
}

// New creates a Var[T] backed by a unique unexported pointer key.
func New[T any]() *Var[T] {
	return &Var[T]{key: &slotKey[T]{}}
}

func (v *Var[T]) ambientSlot() {}

func (v *Var[T]) requireInit() {
	if v == nil || v.key == nil {
		panic("ambient: uninitialized Var; construct with ambient.New")
	}
}

func (v *Var[T]) frame(ctx context.Context) *frame[T] {
	if v == nil || v.key == nil || ctx == nil {
		return nil
	}
	f, _ := ctx.Value(v.key).(*frame[T])
	return f
}

// Current returns the active value of v in ctx, if present.
func (v *Var[T]) Current(ctx context.Context) (T, bool) {
	f := v.frame(ctx)
	if f == nil || !f.present {
		var zero T
		return zero, false
	}
	return f.value, true
}

// Outer returns the enclosing active value immediately outside the current
// frame of v in ctx, if both the current and enclosing frames are present.
func (v *Var[T]) Outer(ctx context.Context) (T, bool) {
	f := v.frame(ctx)
	if f == nil || !f.present || f.outer == nil || !f.outer.present {
		var zero T
		return zero, false
	}
	return f.outer.value, true
}

// Depth returns the active nesting depth of v in ctx (0 when absent or cleared).
func (v *Var[T]) Depth(ctx context.Context) int {
	f := v.frame(ctx)
	if f == nil || !f.present {
		return 0
	}
	return f.depth
}

// With returns a derived context that pushes val onto v's immutable frame chain
// without mutating ctx or any enclosing frame. If ctx is nil, context.Background
// is used as the parent.
func (v *Var[T]) With(ctx context.Context, val T) context.Context {
	v.requireInit()
	if ctx == nil {
		ctx = context.Background()
	}
	outer := v.frame(ctx)
	next := &frame[T]{value: val, present: true, depth: 1}
	if outer != nil && outer.present {
		next.outer = outer
		next.depth = outer.depth + 1
	}
	return context.WithValue(ctx, v.key, next)
}

// Clear returns a derived context in which v has no active value (Current and
// Outer report false and Depth reports 0), without mutating ctx or any
// enclosing frame. Subsequent With calls on the returned context start a fresh
// chain at depth 1. If ctx is nil, context.Background is returned.
func (v *Var[T]) Clear(ctx context.Context) context.Context {
	v.requireInit()
	if ctx == nil {
		return context.Background()
	}
	f := v.frame(ctx)
	if f == nil || !f.present {
		return ctx
	}
	return context.WithValue(ctx, v.key, &frame[T]{present: false})
}

// Scope runs fn with a derived context carrying val for v. Because contexts
// are immutable, the enclosing context is automatically preserved when fn
// returns or panics. If fn is nil, Scope returns nil.
func (v *Var[T]) Scope(ctx context.Context, val T, fn func(context.Context) error) error {
	v.requireInit()
	if fn == nil {
		return nil
	}
	return fn(v.With(ctx, val))
}

// ScopeResult runs fn with a derived context carrying val for v and returns
// its typed result and error. If fn is nil, ScopeResult returns the zero value
// of R and nil.
func ScopeResult[T any, R any](v *Var[T], ctx context.Context, val T, fn func(context.Context) (R, error)) (R, error) {
	v.requireInit()
	if fn == nil {
		var zero R
		return zero, nil
	}
	return fn(v.With(ctx, val))
}
