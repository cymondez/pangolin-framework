package uow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/cymondez/pangolin-framework/kernel/ambient"
	"github.com/cymondez/pangolin-framework/observability"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// Transaction is owned exclusively by Manager, never exposed to application code.
type Transaction interface {
	Commit(context.Context) error
	Rollback(context.Context) error
}

// Resource installs its private executor in the callback context. Optional
// DatabaseKey and RequiresTransaction methods declare resource capabilities.
type Resource interface {
	Begin(context.Context, EffectiveOptions) (context.Context, Transaction, error)
}
type manager struct {
	resource Resource
	defaults DefaultOptions
	key      string
	rls      bool
}

var (
	scopeVar      = ambient.New[*view]()
	completingVar = ambient.New[*operation]()
	slotsVar      = ambient.New[[]ambient.Slot]()
)

type operation struct {
	mu                 sync.Mutex
	id                 string
	options            EffectiveOptions
	binding            scope.ScopeBinding
	active, completing bool
	depth              int
	rollback           error
	flush              func(context.Context) error
	hooks              [3][]func(context.Context, Completion) error
}
type view struct {
	root    *operation
	options EffectiveOptions
	active  bool
}

// AttachSlot registers a UOW-scoped ambient variable (such as a database
// executor or transaction-bound event queue) on ctx so Detach and RequiresNew
// clear it automatically without mutating any enclosing context.
func AttachSlot(ctx context.Context, slot ambient.Slot) context.Context {
	if ctx == nil || slot == nil {
		return ctx
	}
	prev, _ := slotsVar.Current(ctx)
	for _, existing := range prev {
		if existing == slot {
			return ctx
		}
	}
	next := make([]ambient.Slot, len(prev)+1)
	copy(next, prev)
	next[len(prev)] = slot
	return slotsVar.With(ctx, next)
}

func clearSlots(ctx context.Context) context.Context {
	if slots, ok := slotsVar.Current(ctx); ok {
		for _, slot := range slots {
			ctx = slot.Clear(ctx)
		}
		ctx = slotsVar.Clear(ctx)
	}
	return ctx
}

// Detach returns a derived context with the current unit-of-work view,
// completion marker, and all UOW-scoped resource slots (database/postgres
// executors, local/outbox event queues) cleared via ambient.Var.Clear.
// Verified identity, tenant, ScopeBinding, observability metadata, and
// deadlines are preserved so a goroutine can safely start a fresh top-level
// Within operation (which re-runs scope.Admit) without leaking parent UOW
// state.
func Detach(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	ctx = clearSlots(ctx)
	ctx = completingVar.Clear(ctx)
	ctx = scopeVar.Clear(ctx)
	return ctx
}

func isCompleting(ctx context.Context, r *operation) bool {
	cur, ok := completingVar.Current(ctx)
	return ok && cur == r
}

// NewManager validates mandatory limits before any operation can start.
func NewManager(resource Resource, defaults DefaultOptions) (UnitOfWorkManager, error) {
	if resource == nil || defaults.DefaultTimeout <= 0 || defaults.CleanupTimeout <= 0 || defaults.HookTimeout <= 0 || defaults.MaxRequiresNewDepth <= 0 || defaults.DefaultIsolation > Serializable {
		return nil, fail(ErrOptionsInvalid, "", NotStarted, nil)
	}
	m := &manager{resource: resource, defaults: defaults, key: "resource:" + rand.Text(), rls: true}
	if p, ok := resource.(interface{ DatabaseKey() string }); ok {
		m.key = p.DatabaseKey()
		if m.key == "" {
			return nil, fail(ErrOptionsInvalid, "", NotStarted, nil)
		}
	}
	if p, ok := resource.(interface{ RequiresTransaction() bool }); ok {
		m.rls = p.RequiresTransaction()
	}
	return m, nil
}
func currentView(ctx context.Context) *view {
	v, _ := scopeVar.Current(ctx)
	return v
}

// Current returns the active unit-of-work scope view carried by ctx, if any.
func Current(ctx context.Context) (Scope, bool) {
	v := currentView(ctx)
	if v == nil {
		return nil, false
	}
	v.root.mu.Lock()
	defer v.root.mu.Unlock()
	if !v.active || !v.root.active || (v.root.completing && !isCompleting(ctx, v.root)) {
		return nil, false
	}
	return v, true
}
func (m *manager) Current(ctx context.Context) (Scope, bool) { return Current(ctx) }
func bindingError(b, want scope.ScopeBinding) error {
	if b.Ref.TenantID != want.Ref.TenantID {
		return ErrTenantMismatch
	}
	if b.Principal != want.Principal {
		return ErrIdentityMismatch
	}
	if b.Ref != want.Ref || b.AuthorityRevision != want.AuthorityRevision {
		return ErrScopeMismatch
	}
	return nil
}

// Check validates lifecycle, immutable identity, deadline and access restrictions.
func Check(ctx context.Context, write bool) error {
	v := currentView(ctx)
	if v == nil {
		return fail(ErrRequired, "", NotStarted, nil)
	}
	r := v.root
	r.mu.Lock()
	defer r.mu.Unlock()
	reject := func(code, cause error, poison bool) error {
		if poison {
			r.rollback = errors.Join(r.rollback, code, cause)
		}
		return fail(code, r.id, NotStarted, cause)
	}
	if !v.active || !r.active {
		return reject(ErrClosed, nil, false)
	}
	if r.completing && !isCompleting(ctx, r) {
		return reject(ErrBusy, nil, true)
	}
	b, ok := scope.Current(ctx)
	if !ok {
		return reject(ErrScopeMismatch, nil, true)
	}
	if e := bindingError(b, r.binding); e != nil {
		return reject(e, nil, true)
	}
	if e := ctx.Err(); e != nil {
		return reject(e, e, true)
	}
	if write && v.options.Mode != ReadWrite {
		return reject(ErrReadOnly, nil, true)
	}
	if write && !v.options.Transactional {
		return reject(ErrTransactionRequired, nil, true)
	}
	return nil
}
func valid(o Options) bool {
	return o.Timeout >= 0 && o.Mode <= ReadWrite && o.Transaction <= NonTransactional && o.Propagation <= RequiresNew && o.Isolation <= Serializable
}
func (m *manager) Within(ctx context.Context, o Options, callback func(context.Context) error) (err error) {
	if ctx != nil {
		var finish func(error)
		ctx, finish = observability.Start(ctx, "uow")
		defer func() { finish(err) }()
	}
	p := currentView(ctx)
	completionJoin := false
	if p != nil && isCompleting(ctx, p.root) {
		// A Resource completion hook (Local Event drain/flush) may call the
		// normal Application Command/Query boundary. This marker is private
		// to Manager and is only installed on its controlled completion path.
		completionJoin = true
	}
	reject := func(code, cause error) error {
		id := ""
		if p != nil {
			id = p.root.id
			if o.Propagation == Required {
				markRollback(p, errors.Join(code, cause))
			}
		}
		return fail(code, id, NotStarted, cause)
	}
	if ctx == nil || callback == nil || !valid(o) {
		return reject(ErrOptionsInvalid, nil)
	}
	b, ok := scope.Current(ctx)
	if !ok {
		return reject(ErrRequired, nil)
	}
	if p != nil {
		p.root.mu.Lock()
		active := p.active && p.root.active
		completing := p.root.completing
		p.root.mu.Unlock()
		if !active {
			return reject(ErrClosed, nil)
		}
		if completing && !(completionJoin && o.Propagation == Required) {
			return reject(ErrBusy, nil)
		}
		if e := bindingError(b, p.root.binding); e != nil {
			return reject(e, nil)
		}
		if o.Propagation == Required {
			return m.join(ctx, p, o, callback)
		}
		if p.root.depth >= m.defaults.MaxRequiresNewDepth {
			return reject(ErrDepthExceeded, nil)
		}
	} else {
		admitted, admissionErr := scope.Admit(ctx)
		if admissionErr != nil {
			return reject(admissionErr, admissionErr)
		}
		ctx = admitted
		b, _ = scope.Current(ctx)
	}
	mode := o.Mode
	if mode == AccessDefault {
		mode = ReadOnly
	}
	transactional := o.Transaction == Transactional || o.Transaction == TransactionAuto && (mode == ReadWrite || m.rls)
	if !transactional && (mode == ReadWrite || m.rls || o.Isolation != IsolationDefault) {
		return reject(ErrOptionsInvalid, nil)
	}
	isolation := o.Isolation
	if transactional && isolation == IsolationDefault {
		isolation = m.defaults.DefaultIsolation
		if isolation == IsolationDefault {
			isolation = ReadCommitted
		}
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = m.defaults.DefaultTimeout
	}
	work, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if p != nil {
		var stop context.CancelFunc
		work, stop = context.WithDeadline(work, p.options.Deadline)
		defer stop()
	}
	if e := work.Err(); e != nil {
		return reject(e, e)
	}
	id := rand.Text()
	effective := EffectiveOptions{Mode: mode, Transactional: transactional, Isolation: isolation, DatabaseKey: m.key, ResourceScopeKind: b.Ref.Kind, ResourceScopeID: b.Ref.ID}
	effective.Deadline, _ = work.Deadline()
	r := &operation{id: id, options: effective, binding: b, active: true}
	if p != nil {
		r.depth = p.root.depth + 1
	}
	v := &view{r, effective, true}
	// Isolate the new root from any parent resource slots (executors, event
	// queues) and completion markers before Begin installs its own slots.
	// Parent contexts are immutable and remain completely unaffected.
	work = clearSlots(work)
	work = completingVar.Clear(work)
	work = scopeVar.With(work, v)
	work, tx, beginErr := m.resource.Begin(work, effective)
	if beginErr != nil {
		r.active = false
		v.active = false
		return fail(errors.New("UOW_BEGIN_FAILED"), id, NotStarted, beginErr)
	}
	if work == nil || tx == nil {
		r.active = false
		v.active = false
		return fail(ErrOptionsInvalid, id, NotStarted, errors.New("invalid resource result"))
	}
	if flusher, ok := tx.(interface{ Flush(context.Context) error }); ok {
		r.flush = flusher.Flush
	}
	outcome := NotStarted
	commitAttempted := false
	defer func() {
		panicValue := recover()
		r.mu.Lock()
		r.completing = true
		r.mu.Unlock()
		if panicValue != nil {
			err = errors.Join(err, fmt.Errorf("UOW callback panic: %v", panicValue))
		}
		cleanup, done := context.WithTimeout(context.WithoutCancel(work), m.defaults.CleanupTimeout)
		if outcome != Committed && outcome != CompletedNonTransactional {
			var cleanupErr error
			if effective.Transactional {
				cleanupErr = tx.Rollback(cleanup)
			} else if c, ok := tx.(interface{ Release(context.Context) error }); ok {
				cleanupErr = c.Release(cleanup)
			}
			err = errors.Join(err, cleanupErr)
			if outcome != Unknown {
				if cleanupErr != nil && effective.Transactional {
					outcome = Unknown
				} else {
					outcome = RolledBack
				}
			}
		} else if c, ok := tx.(interface{ Release(context.Context) error }); ok {
			if e := c.Release(cleanup); e != nil {
				slog.Error("UOW resource release failed", "operation_id", id, "outcome", outcome)
			}
		}
		done()
		r.mu.Lock()
		r.active = false
		v.active = false
		hooks := r.hooks
		r.mu.Unlock()
		if err != nil {
			code := errorCode(err)
			if errors.Is(err, ErrRollbackOnly) {
				code = ErrRollbackOnly
			}
			if outcome == Unknown {
				if commitAttempted {
					code = ErrCommitUnknown
				} else {
					code = errors.New("UOW_CLEANUP_FAILED")
				}
			}
			err = fail(code, id, outcome, err)
		}
		completion := Completion{id, id, outcome, err}
		for i, fs := range hooks {
			success := outcome == Committed || outcome == CompletedNonTransactional
			if i == 0 && !success || i == 1 && success {
				continue
			}
			for _, f := range fs {
				m.observe(f, completion)
			}
		}
		if panicValue != nil {
			panic(panicValue)
		}
	}()
	if err = callback(work); err != nil {
		return err
	}
	if err = work.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	rollback := r.rollback
	r.completing = true
	r.mu.Unlock()
	if rollback != nil {
		return fail(ErrRollbackOnly, id, NotStarted, rollback)
	}
	// Only this private completion context can use the executor while completing.
	completionContext := completingVar.With(work, r)
	if r.flush != nil {
		if err = r.flush(completionContext); err != nil {
			return err
		}
	}
	if stages, ok := tx.(interface{ BeforeCommit(context.Context) error }); ok {
		if err = stages.BeforeCommit(completionContext); err != nil {
			return err
		}
	}
	if f, ok := tx.(interface{ Prepare(context.Context) error }); ok {
		if err = f.Prepare(completionContext); err != nil {
			return err
		}
	}
	r.mu.Lock()
	rollback = r.rollback
	r.mu.Unlock()
	if rollback != nil {
		return fail(ErrRollbackOnly, id, NotStarted, rollback)
	}
	if err = work.Err(); err != nil {
		return err
	}
	if !effective.Transactional {
		outcome = CompletedNonTransactional
		return nil
	}
	commitAttempted = true
	if err = tx.Commit(work); err != nil {
		outcome = Unknown
		if c, ok := tx.(interface{ CommitFailureOutcome(error) Outcome }); ok && c.CommitFailureOutcome(err) == RolledBack {
			outcome = RolledBack
		}
		return err
	}
	outcome = Committed
	return nil
}

// markRollback records failure while the root is active, including during
// controlled completion. SetRollbackOnly intentionally remains unavailable
// to application code once completion has started.
func markRollback(v *view, cause error) {
	if cause == nil {
		cause = ErrRollbackOnly
	}
	r := v.root
	r.mu.Lock()
	if v.active && r.active {
		r.rollback = errors.Join(r.rollback, cause)
	}
	r.mu.Unlock()
}
func (m *manager) join(ctx context.Context, p *view, o Options, callback func(context.Context) error) (err error) {
	conflict := func(code error) error { markRollback(p, code); return fail(code, p.root.id, NotStarted, nil) }
	if m.key != p.options.DatabaseKey {
		return conflict(ErrResourceMismatch)
	}
	if !p.options.Transactional && o.Isolation != IsolationDefault {
		return conflict(ErrOptionsInvalid)
	}
	if e := Check(ctx, false); e != nil {
		return e
	}
	mode := o.Mode
	if mode == AccessDefault {
		mode = p.options.Mode
	}
	if mode == ReadWrite && p.options.Mode != ReadWrite || o.Transaction == Transactional && !p.options.Transactional || o.Transaction == NonTransactional && p.options.Transactional || o.Isolation != IsolationDefault && o.Isolation != p.options.Isolation {
		return conflict(ErrOptionsConflict)
	}
	child, cancel := context.WithDeadline(ctx, p.options.Deadline)
	defer cancel()
	if o.Timeout > 0 {
		var stop context.CancelFunc
		child, stop = context.WithTimeout(child, o.Timeout)
		defer stop()
	}
	v := &view{root: p.root, options: p.options, active: true}
	v.options.Mode = mode
	v.options.Deadline, _ = child.Deadline()
	child = scopeVar.With(child, v)
	defer func() {
		pv := recover()
		p.root.mu.Lock()
		v.active = false
		if pv != nil {
			p.root.rollback = errors.Join(p.root.rollback, errors.New("Required callback panic"))
		}
		if err != nil {
			p.root.rollback = errors.Join(p.root.rollback, err)
		}
		p.root.mu.Unlock()
		if pv != nil {
			panic(pv)
		}
	}()
	err = callback(child)
	if err == nil {
		err = child.Err()
	}
	return err
}
func (m *manager) observe(f func(context.Context, Completion) error, c Completion) {
	ctx, stop := context.WithTimeout(context.Background(), m.defaults.HookTimeout)
	defer stop()
	defer func() {
		if recover() != nil {
			slog.Error("UOW observer panicked", "operation_id", c.OperationID, "outcome", c.Outcome)
		}
	}()
	if err := f(ctx, c); err != nil {
		slog.Error("UOW observer failed", "operation_id", c.OperationID, "outcome", c.Outcome)
	}
}
func (v *view) ID() string                { return v.root.id }
func (v *view) Options() EffectiveOptions { return v.options }
func (v *view) SaveChanges(ctx context.Context) error {
	if currentView(ctx) != v {
		return fail(ErrClosed, v.root.id, NotStarted, nil)
	}
	if e := Check(ctx, false); e != nil {
		return e
	}
	v.root.mu.Lock()
	rollback, flush := v.root.rollback, v.root.flush
	v.root.mu.Unlock()
	if rollback != nil {
		return fail(ErrRollbackOnly, v.root.id, NotStarted, rollback)
	}
	if flush != nil {
		return Fail(ctx, flush(ctx))
	}
	return nil
}
func (v *view) SetRollbackOnly(cause error) error {
	r := v.root
	r.mu.Lock()
	defer r.mu.Unlock()
	if !v.active || !r.active {
		return fail(ErrClosed, r.id, NotStarted, nil)
	}
	if r.completing {
		return fail(ErrBusy, r.id, NotStarted, nil)
	}
	if cause == nil {
		cause = ErrRollbackOnly
	}
	r.rollback = errors.Join(r.rollback, cause)
	return nil
}
func (v *view) addHook(i int, f func(context.Context, Completion) error) error {
	r := v.root
	r.mu.Lock()
	defer r.mu.Unlock()
	if f == nil {
		return fail(ErrOptionsInvalid, r.id, NotStarted, nil)
	}
	if !v.active || !r.active {
		return fail(ErrClosed, r.id, NotStarted, nil)
	}
	if r.completing {
		return fail(ErrBusy, r.id, NotStarted, nil)
	}
	r.hooks[i] = append(r.hooks[i], f)
	return nil
}
func (v *view) OnCompleted(f func(context.Context, Completion) error) error { return v.addHook(0, f) }
func (v *view) OnFailed(f func(context.Context, Completion) error) error    { return v.addHook(1, f) }
func (v *view) OnDisposed(f func(context.Context, Completion) error) error  { return v.addHook(2, f) }
