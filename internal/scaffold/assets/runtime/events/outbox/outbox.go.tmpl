// Package outbox provides transaction-staged publishing and fenced delivery.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/kernel/ambient"
	"github.com/cymondez/pangolin-framework/observability"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrUnavailable = errors.New("OUTBOX_UNAVAILABLE")
var ErrLeaseLost = errors.New("OUTBOX_LEASE_LOST")

type Policy struct {
	Lease, SendTimeout, RetryInitial, RetryMax time.Duration
	MaxAttempts                                int
	JitterFraction                             float64
}

func (p Policy) Validate() error {
	if p.Lease <= 0 || p.SendTimeout <= 0 || p.RetryInitial <= 0 || p.RetryMax < p.RetryInitial || p.MaxAttempts <= 0 || !(p.JitterFraction >= 0 && p.JitterFraction <= 1) {
		return fmt.Errorf("invalid outbox retry policy")
	}
	return nil
}
func (p Policy) Backoff(attempt int) time.Duration {
	d := p.RetryInitial
	for n := 1; n < attempt && d < p.RetryMax; n++ {
		if d > p.RetryMax/2 {
			d = p.RetryMax
			break
		}
		d *= 2
	}
	if d > p.RetryMax {
		d = p.RetryMax
	}
	if p.JitterFraction > 0 {
		d = time.Duration(float64(d) * (1 - p.JitterFraction + 2*p.JitterFraction*rand.Float64()))
	}
	if d > p.RetryMax {
		d = p.RetryMax
	}
	return d
}

type Claim struct {
	Envelope distributed.Envelope
	Token    string
	Attempts int
}
type Store interface {
	Append(context.Context, distributed.Envelope, []byte) error
	Claim(context.Context, string, Policy) (*Claim, error)
	Complete(context.Context, Claim, bool, time.Duration, string) error
}
type Publisher struct {
	registry *distributed.Registry
	store    Store
	queue    *ambient.Var[*Queue]
}

func NewPublisher(registry *distributed.Registry, store Store) (*Publisher, error) {
	if registry == nil || store == nil {
		return nil, ErrUnavailable
	}
	return &Publisher{registry: registry, store: store, queue: ambient.New[*Queue]()}, nil
}

type queued struct {
	envelope distributed.Envelope
	raw      []byte
}
type Queue struct {
	publisher *Publisher
	mu        sync.Mutex
	events    []queued
	closed    bool
}

func (p *Publisher) Attach(ctx context.Context) (context.Context, *Queue) {
	q := &Queue{publisher: p}
	if p == nil || p.queue == nil {
		return ctx, q
	}
	ctx = p.queue.With(ctx, q)
	ctx = uow.AttachSlot(ctx, p.queue)
	return ctx, q
}
func (p *Publisher) Publish(ctx context.Context, event any) error {
	if err := uow.Check(ctx, true); err != nil {
		return err
	}
	if p == nil || p.queue == nil {
		return uow.Fail(ctx, ErrUnavailable)
	}
	q, _ := p.queue.Current(ctx)
	if q == nil {
		return uow.Fail(ctx, ErrUnavailable)
	}
	e, ok := event.(distributed.Envelope)
	if !ok {
		return uow.Fail(ctx, distributed.ErrSchemaUnsupported)
	}
	if e.TraceParent == "" {
		e.TraceParent = observability.TraceParent(ctx)
	}
	b, _ := scope.Current(ctx)
	if e.TenantID != b.Ref.TenantID || e.ScopeKind != b.Ref.Kind || e.ScopeID != b.Ref.ID {
		return uow.Fail(ctx, scope.ErrDenied)
	}
	raw, err := p.registry.Encode(e)
	if err != nil {
		return uow.Fail(ctx, err)
	}
	// Copy the full wire envelope now so callers cannot mutate queued payloads.
	e.Payload = append(e.Payload[:0:0], e.Payload...)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return uow.Fail(ctx, uow.ErrClosed)
	}
	q.events = append(q.events, queued{e, raw})
	return nil
}
func (q *Queue) Flush(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return uow.ErrClosed
	}
	q.closed = true
	for _, e := range q.events {
		if err := q.publisher.store.Append(ctx, e.envelope, e.raw); err != nil {
			return uow.Fail(ctx, err)
		}
	}
	q.events = nil
	return nil
}

type Worker struct {
	store     Store
	broker    distributed.BrokerPublisher
	policy    Policy
	name      string
	authorize func(context.Context, distributed.Envelope) error
}

func NewWorker(store Store, broker distributed.BrokerPublisher, policy Policy, name string, authorize func(context.Context, distributed.Envelope) error) (*Worker, error) {
	if store == nil || broker == nil || name == "" || authorize == nil {
		return nil, ErrUnavailable
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Worker{store, broker, policy, name, authorize}, nil
}
func (w *Worker) Dispatch(ctx context.Context) (sent bool, dispatchErr error) {
	claim, err := w.store.Claim(ctx, w.name, w.policy)
	if err != nil || claim == nil {
		return false, err
	}
	ctx = observability.WithOperation(observability.Restore(ctx, claim.Envelope.TraceParent), claim.Envelope.CorrelationID)
	ctx = observability.WithMessage(ctx, "event", claim.Envelope.ID)
	ctx, finish := observability.Start(ctx, "outbox")
	defer func() { finish(dispatchErr) }()
	err = w.authorize(ctx, claim.Envelope)
	if err == nil {
		send, cancel := context.WithTimeout(ctx, w.policy.SendTimeout)
		err = w.broker.Send(send, claim.Envelope)
		cancel()
	}
	code := ""
	if err != nil {
		code = "DELIVERY_FAILED"
		if claim.Attempts >= w.policy.MaxAttempts {
			slog.ErrorContext(ctx, "outbox delivery dead", "event_id", claim.Envelope.ID, "attempts", claim.Attempts)
		}
	}
	finishErr := w.store.Complete(ctx, *claim, err == nil, w.policy.Backoff(claim.Attempts), code)
	return err == nil && finishErr == nil, errors.Join(err, finishErr)
}
