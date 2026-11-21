package uow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cymondez/pangolin-framework/security/scope"
)

// Transaction is owned exclusively by Manager, never exposed to application code.
type Transaction interface {
	Commit(context.Context) error
	Rollback(context.Context) error
}

// Resource installs its private executor in the returned callback context.
type Resource interface {
	Begin(context.Context, EffectiveOptions) (context.Context, Transaction, error)
}
type manager struct {
	resource Resource
	defaults DefaultOptions
}
type scopeKey struct{}
type operation struct {
	mu       sync.Mutex
	id       string
	options  EffectiveOptions
	binding  scope.ScopeBinding
	active   bool
	rollback error
	hooks    [3][]func(context.Context, Completion) error
}
type view struct {
	root    *operation
	options EffectiveOptions
	active  bool
}
type failure struct {
	code, id string
	outcome  Outcome
	cause    error
}

func (e *failure) Error() string       { return e.code + ": " + e.id }
func (e *failure) Code() string        { return e.code }
func (e *failure) ScopeID() string     { return e.id }
func (e *failure) OperationID() string { return e.id }
func (e *failure) Outcome() Outcome    { return e.outcome }
func (e *failure) Unwrap() error       { return e.cause }

// NewManager implements the M1 Required transactional slice. RequiresNew and
// non-default isolation fail explicitly until the M2 options matrix is delivered.
func NewManager(resource Resource, defaults DefaultOptions) (UnitOfWorkManager, error) {
	if resource == nil || defaults.DefaultTimeout <= 0 || defaults.CleanupTimeout <= 0 || defaults.HookTimeout <= 0 || defaults.MaxRequiresNewDepth <= 0 {
		return nil, fmt.Errorf("invalid UOW settings")
	}
	return &manager{resource, defaults}, nil
}
func currentView(ctx context.Context) *view {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(scopeKey{}).(*view)
	return v
}
func (m *manager) Current(ctx context.Context) (Scope, bool) {
	v := currentView(ctx)
	if v == nil {
		return nil, false
	}
	v.root.mu.Lock()
	defer v.root.mu.Unlock()
	return v, v.active && v.root.active
}

// Check is used by resource adapters before accessing a callback's executor.
func Check(ctx context.Context, write bool) error {
	v := currentView(ctx)
	if v == nil {
		return &failure{code: "UOW_SCOPE_REQUIRED"}
	}
	r := v.root
	r.mu.Lock()
	defer r.mu.Unlock()
	fail := func(code string, cause error) error { return &failure{code, r.id, NotStarted, cause} }
	if !v.active || !r.active {
		return fail("UOW_SCOPE_ENDED", nil)
	}
	b, ok := scope.Current(ctx)
	if !ok || b != r.binding {
		r.rollback = errors.New("binding mismatch")
		return fail("UOW_SCOPE_MISMATCH", r.rollback)
	}
	if e := ctx.Err(); e != nil {
		r.rollback = e
		return fail("UOW_CANCELLED", e)
	}
	if write && v.options.Mode != ReadWrite {
		r.rollback = errors.New("read only")
		return fail("UOW_READ_ONLY", r.rollback)
	}
	return nil
}
func (m *manager) Within(ctx context.Context, options Options, callback func(context.Context) error) (err error) {
	parent := currentView(ctx)
	invalid := func(cause error) error {
		if parent != nil {
			_ = parent.SetRollbackOnly(cause)
		}
		return &failure{code: "UOW_OPTIONS_INVALID", cause: cause}
	}
	if ctx == nil || callback == nil || options.Timeout < 0 || options.Mode > ReadWrite || options.Transaction > NonTransactional || options.Propagation != Required || options.Isolation > ReadCommitted || options.Transaction == NonTransactional {
		return invalid(errors.New("unsupported M1 options"))
	}
	b, ok := scope.Current(ctx)
	if !ok {
		return &failure{code: "UOW_SCOPE_REQUIRED"}
	}
	mode := options.Mode
	if mode == AccessDefault {
		mode = ReadOnly
		if parent != nil {
			mode = parent.options.Mode
		}
	}
	if parent != nil {
		if e := Check(ctx, mode == ReadWrite); e != nil {
			return e
		}
		timeout := time.Until(parent.options.Deadline)
		if options.Timeout > 0 && options.Timeout < timeout {
			timeout = options.Timeout
		}
		child, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		v := &view{root: parent.root, options: parent.options, active: true}
		v.options.Mode = mode
		v.options.Deadline, _ = child.Deadline()
		child = context.WithValue(child, scopeKey{}, v)
		defer func() {
			v.root.mu.Lock()
			v.active = false
			v.root.mu.Unlock()
			if p := recover(); p != nil {
				_ = parent.SetRollbackOnly(fmt.Errorf("child panic"))
				panic(p)
			}
			if err != nil {
				_ = parent.SetRollbackOnly(err)
			}
		}()
		err = callback(child)
		if err == nil {
			err = child.Err()
		}
		return err
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = m.defaults.DefaultTimeout
	}
	work, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	id := rand.Text()
	effective := EffectiveOptions{Mode: mode, Transactional: true, Isolation: ReadCommitted, ResourceScopeKind: b.Ref.Kind, ResourceScopeID: b.Ref.ID}
	effective.Deadline, _ = work.Deadline()
	r := &operation{id: id, options: effective, binding: b, active: true}
	v := &view{r, effective, true}
	work = context.WithValue(work, scopeKey{}, v)
	work, tx, beginErr := m.resource.Begin(work, effective)
	if beginErr != nil {
		r.active = false
		return &failure{"UOW_BEGIN_FAILED", id, NotStarted, beginErr}
	}
	outcome := NotStarted
	defer func() {
		p := recover()
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), m.defaults.CleanupTimeout)
		if outcome != Committed {
			rollbackErr := tx.Rollback(cleanup)
			err = errors.Join(err, rollbackErr)
			if outcome != Unknown {
				outcome = RolledBack
			}
		}
		done()
		r.mu.Lock()
		r.active = false
		v.active = false
		r.mu.Unlock()
		completion := Completion{id, id, outcome, err}
		for i, hooks := range r.hooks {
			if i == 0 && outcome != Committed || i == 1 && outcome == Committed {
				continue
			}
			for _, hook := range hooks {
				hookCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), m.defaults.HookTimeout)
				err = errors.Join(err, hook(hookCtx, completion))
				stop()
			}
		}
		if p != nil {
			panic(p)
		}
		if err != nil {
			code := "UOW_ROLLED_BACK"
			if outcome == Unknown {
				code = "UOW_COMMIT_UNKNOWN"
			}
			if outcome == Committed {
				code = "UOW_HOOK_FAILED"
			}
			err = &failure{code, id, outcome, err}
		}
	}()
	if err = callback(work); err != nil {
		return err
	}
	if err = work.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	err = r.rollback
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if err = tx.Commit(work); err != nil {
		outcome = Unknown
		return err
	}
	outcome = Committed
	return nil
}
func (v *view) ID() string                            { return v.root.id }
func (v *view) Options() EffectiveOptions             { return v.options }
func (v *view) SaveChanges(ctx context.Context) error { return Check(ctx, false) }
func (v *view) SetRollbackOnly(cause error) error {
	r := v.root
	r.mu.Lock()
	defer r.mu.Unlock()
	if !v.active || !r.active {
		return &failure{code: "UOW_SCOPE_ENDED", id: r.id}
	}
	if cause == nil {
		cause = errors.New("rollback only")
	}
	r.rollback = errors.Join(r.rollback, cause)
	return nil
}
func (v *view) addHook(index int, f func(context.Context, Completion) error) error {
	r := v.root
	r.mu.Lock()
	defer r.mu.Unlock()
	if f == nil {
		return &failure{code: "UOW_OPTIONS_INVALID", id: r.id}
	}
	if !v.active || !r.active {
		return &failure{code: "UOW_SCOPE_ENDED", id: r.id}
	}
	r.hooks[index] = append(r.hooks[index], f)
	return nil
}
func (v *view) OnCompleted(f func(context.Context, Completion) error) error { return v.addHook(0, f) }
func (v *view) OnFailed(f func(context.Context, Completion) error) error    { return v.addHook(1, f) }
func (v *view) OnDisposed(f func(context.Context, Completion) error) error  { return v.addHook(2, f) }
