// Package jobs defines versioned, at-least-once background work and bounded workers.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pangolin-framework/pangolin/events/outbox"
	"github.com/pangolin-framework/pangolin/kernel/runtime"
	"github.com/pangolin-framework/pangolin/observability"
	"github.com/pangolin-framework/pangolin/security/scope"
)

var (
	ErrInvalid           = errors.New("JOB_INVALID")
	ErrUnsupported       = errors.New("JOB_SCHEMA_UNSUPPORTED")
	ErrCapacity    error = capacityError{}
	ErrLeaseLost         = errors.New("JOB_LEASE_LOST")
	ErrCancelled         = errors.New("JOB_CANCELLED")
)

type capacityError struct{}

func (capacityError) Error() string   { return "JOB_CAPACITY_EXCEEDED" }
func (capacityError) Code() string    { return "JOB_CAPACITY_EXCEEDED" }
func (capacityError) HTTPStatus() int { return 429 }

// Job has immutable identity/owner/attribution. Payload never supplies authority.
type Job struct {
	ID          string             `json:"id"`
	Type        string             `json:"type"`
	Version     int                `json:"schema_version"`
	Owner       scope.ScopeRef     `json:"owner"`
	Actor       scope.PrincipalRef `json:"actor"`
	Delegated   bool               `json:"delegated"`
	Payload     json.RawMessage    `json:"payload"`
	DueAt       time.Time          `json:"due_at"`
	Deadline    time.Time          `json:"deadline"`
	OperationID string             `json:"operation_id,omitempty"`
	TraceParent string             `json:"traceparent,omitempty"`
}
type Definition struct {
	Type     string
	Version  int
	Validate func(json.RawMessage) error
	Handle   func(context.Context, Job) error
}
type key struct {
	name    string
	version int
}
type Registry struct {
	maxBytes    int
	definitions map[key]Definition
}

func NewRegistry(maxBytes int, definitions ...Definition) (*Registry, error) {
	if maxBytes <= 0 || len(definitions) == 0 {
		return nil, ErrInvalid
	}
	r := &Registry{maxBytes: maxBytes, definitions: map[key]Definition{}}
	for _, d := range definitions {
		k := key{d.Type, d.Version}
		if d.Type == "" || d.Version <= 0 || d.Validate == nil || d.Handle == nil {
			return nil, ErrInvalid
		}
		if _, exists := r.definitions[k]; exists {
			return nil, ErrInvalid
		}
		r.definitions[k] = d
	}
	return r, nil
}
func (r *Registry) Validate(j Job) error {
	if r == nil || j.ID == "" || len(j.ID) > 160 || scope.Validate(j.Owner) != nil || j.Actor.ID == "" || (j.Actor.Kind != "user" && j.Actor.Kind != "service") || j.Delegated && j.Actor.Kind != "user" || j.DueAt.IsZero() || !j.Deadline.After(j.DueAt) || len(j.Payload) > r.maxBytes || !json.Valid(j.Payload) {
		return ErrInvalid
	}
	d, ok := r.definitions[key{j.Type, j.Version}]
	if !ok {
		return ErrUnsupported
	}
	if err := d.Validate(j.Payload); err != nil {
		return errors.Join(ErrInvalid, err)
	}
	return nil
}
func (r *Registry) Handle(ctx context.Context, j Job) error {
	if err := r.Validate(j); err != nil {
		return err
	}
	return r.definitions[key{j.Type, j.Version}].Handle(ctx, j)
}

type Policy struct {
	Lease, ExecutionTimeout, CleanupTimeout, PollInterval time.Duration
	RetryInitial, RetryMax, Retention                     time.Duration
	MaxAttempts, Concurrency, BatchSize, MaxPending       int
	JitterFraction                                        float64
}

func (p Policy) Validate() error {
	if p.Lease <= p.ExecutionTimeout || p.CleanupTimeout <= 0 || p.Lease-p.ExecutionTimeout <= p.CleanupTimeout || p.ExecutionTimeout <= 0 || p.PollInterval <= 0 || p.Concurrency <= 0 || p.BatchSize <= 0 || p.BatchSize < p.Concurrency || p.MaxPending <= 0 || p.Retention <= 0 {
		return ErrInvalid
	}
	return p.retry().Validate()
}
func (p Policy) retry() outbox.Policy {
	return outbox.Policy{Lease: p.Lease, SendTimeout: p.ExecutionTimeout, RetryInitial: p.RetryInitial, RetryMax: p.RetryMax, MaxAttempts: p.MaxAttempts, JitterFraction: p.JitterFraction}
}
func (p Policy) Backoff(attempt int) time.Duration { return p.retry().Backoff(attempt) }

type Claim struct {
	Job      Job
	Token    string
	Attempts int
}

// Store is an infrastructure queue port. Administrative calls must authorize first.
type Store interface {
	Claim(context.Context, string) (*Claim, error)
	Finish(context.Context, Claim, bool, time.Duration, string) error
	Cancelled(context.Context, Claim) (bool, error)
	Release(context.Context, Claim) error
}

// Bind must build a fresh current service identity and reauthorize the exact owner.
// CheckActor revalidates current user authority for delegated jobs only.
type Bind func(context.Context, Job) (context.Context, error)
type CheckActor func(context.Context, Job) error
type Observer interface {
	Job(context.Context, string, time.Duration)
}
type Worker struct {
	store    Store
	registry *Registry
	policy   Policy
	name     string
	bind     Bind
	actor    CheckActor
	observer Observer
	group    *runtime.WorkGroup
	stop     chan struct{}
	once     sync.Once
	start    sync.Once
	slots    chan struct{}
}

func NewWorker(store Store, registry *Registry, policy Policy, name string, bind Bind, actor CheckActor, observer Observer) (*Worker, error) {
	if store == nil || registry == nil || name == "" || bind == nil || actor == nil {
		return nil, ErrInvalid
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Worker{store: store, registry: registry, policy: policy, name: name, bind: bind, actor: actor, observer: observer, group: runtime.NewWorkGroup(), stop: make(chan struct{}), slots: make(chan struct{}, policy.Concurrency)}, nil
}

// Process owns a single claim until handler termination and token-fenced cleanup.
// The caller supplies a worker context, never an HTTP operation context.
func (w *Worker) Process(ctx context.Context) (processed bool, err error) {
	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	default:
		return false, ErrCapacity
	}
	work, done, err := w.group.Enter(ctx)
	if err != nil {
		return false, err
	}
	defer done()
	claim, err := w.store.Claim(work, w.name)
	if err != nil || claim == nil {
		return false, err
	}
	started := time.Now()
	defer func() {
		defer func() { _ = recover() }()
		if w.observer != nil {
			result := "failed"
			if processed {
				result = "succeeded"
			}
			w.observer.Job(work, result, time.Since(started))
		}
	}()
	execute, cancel := context.WithTimeout(work, w.policy.ExecutionTimeout)
	defer cancel()
	if until := time.Until(claim.Job.Deadline); until < w.policy.ExecutionTimeout {
		var stop context.CancelFunc
		execute, stop = context.WithDeadline(execute, claim.Job.Deadline)
		defer stop()
	}
	err = w.registry.Validate(claim.Job)
	var bound context.Context
	if err == nil {
		bound, err = w.bind(execute, claim.Job)
	}
	if err == nil {
		binding, ok := scope.Current(bound)
		if !ok || binding.Ref != claim.Job.Owner || binding.Principal.Kind != "service" {
			err = scope.ErrDenied
		}
	}
	if err == nil && claim.Job.Delegated {
		err = w.actor(bound, claim.Job)
	}
	if err == nil {
		bound = observability.WithOperation(observability.Restore(bound, claim.Job.TraceParent), claim.Job.OperationID)
		bound = observability.WithMessage(bound, "job", claim.Job.ID)
		var finish func(error)
		bound, finish = observability.Start(bound, "job")
		defer func() { finish(err) }()
		err = w.execute(bound, *claim)
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(work), w.policy.CleanupTimeout)
	defer stop()
	// Release only after execution has returned. Cancellation may have committed work;
	// retries use the same Job ID and require handler idempotency.
	if work.Err() != nil {
		return false, errors.Join(err, work.Err(), w.store.Release(cleanup, *claim))
	}
	code := ""
	if err != nil {
		code = "JOB_EXECUTION_FAILED"
	}
	finish := w.store.Finish(cleanup, *claim, err == nil, w.policy.Backoff(claim.Attempts), code)
	return err == nil && finish == nil, errors.Join(err, finish)
}
func (w *Worker) execute(ctx context.Context, c Claim) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorDone := make(chan struct{})
	monitorExited := make(chan struct{})
	go func() {
		defer close(monitorExited)
		ticker := time.NewTicker(w.policy.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-monitorDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				stopped, e := w.store.Cancelled(ctx, c)
				if e != nil || stopped {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		close(monitorDone)
		<-monitorExited
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: handler panic", ErrInvalid)
		}
	}()
	stopped, err := w.store.Cancelled(ctx, c)
	if err != nil {
		return err
	}
	if stopped {
		return ErrCancelled
	}
	err = w.registry.Handle(ctx, c.Job)
	return errors.Join(err, ctx.Err())
}

// Start creates exactly Concurrency loops; there is no unbounded in-memory queue.
func (w *Worker) Start() {
	w.start.Do(func() {
		for i := 0; i < w.policy.Concurrency; i++ {
			go func() {
				for {
					select {
					case <-w.stop:
						return
					default:
					}
					_, _ = w.Process(context.Background())
					timer := time.NewTimer(w.policy.PollInterval)
					select {
					case <-w.stop:
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}()
		}
	})
}
func (w *Worker) Quiesce(ctx context.Context) error {
	w.once.Do(func() { close(w.stop) })
	return w.group.Quiesce(ctx)
}
func (w *Worker) Drain(ctx context.Context) error { return w.group.Drain(ctx) }
func (w *Worker) Cancel()                         { w.group.Cancel() }
