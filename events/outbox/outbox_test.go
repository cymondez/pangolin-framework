package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type store struct {
	claim                            *Claim
	claimErr, appendErr, completeErr error
	appended                         int
	success                          bool
	completed                        int
}

func (s *store) Append(context.Context, distributed.Envelope, []byte) error {
	s.appended++
	return s.appendErr
}
func (s *store) Claim(context.Context, string, Policy) (*Claim, error) { return s.claim, s.claimErr }
func (s *store) Complete(_ context.Context, _ Claim, success bool, _ time.Duration, _ string) error {
	s.success = success
	s.completed++
	return s.completeErr
}

type broker struct {
	err   error
	calls int
}

func (b *broker) Send(context.Context, distributed.Envelope) error { b.calls++; return b.err }
func validPolicy() Policy {
	return Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: 5 * time.Millisecond, MaxAttempts: 2}
}

type resource struct {
	publisher *Publisher
	queue     **Queue
}
type transaction struct{ queue *Queue }

func (r resource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	c, q := r.publisher.Attach(c)
	if r.queue != nil {
		*r.queue = q
	}
	return c, transaction{q}, nil
}
func (t transaction) BeforeCommit(c context.Context) error { return t.queue.Flush(c) }
func (transaction) Commit(context.Context) error           { return nil }
func (transaction) Rollback(context.Context) error         { return nil }
func manager(t *testing.T, r resource) uow.UnitOfWorkManager {
	m, e := uow.NewManager(r, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func bound(t *testing.T) context.Context {
	r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "t", true
	})
	c, e := r.Bind(context.Background(), scope.ScopeRef{TenantID: "t", Kind: "tenant", ID: "t"})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func fact() distributed.Envelope {
	return distributed.Envelope{ID: "event", Type: "fact", SchemaVersion: 1, OccurredAt: time.Now(), TenantID: "t", ScopeKind: "tenant", ScopeID: "t", CorrelationID: "op", Producer: "producer", Payload: []byte(`{"n":1}`)}
}
func TestPublisherGuards(t *testing.T) {
	registry, _ := distributed.NewRegistry(distributed.Schema{Type: "fact", Version: 1})
	s := &store{}
	p, _ := NewPublisher(registry, s)
	for _, c := range []struct {
		r *distributed.Registry
		s Store
	}{{nil, s}, {registry, nil}} {
		if _, e := NewPublisher(c.r, c.s); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
	}
	if e := p.Publish(context.Background(), fact()); e == nil {
		t.Fatal("scope required")
	}
	for _, tc := range []struct {
		name      string
		event     any
		publisher *Publisher
		want      error
	}{{"wrong-type", 42, p, distributed.ErrSchemaUnsupported}, {"owner-tamper", func() distributed.Envelope { e := fact(); e.ScopeID = "other"; return e }(), p, scope.ErrDenied}, {"schema", func() distributed.Envelope { e := fact(); e.SchemaVersion = 2; return e }(), p, distributed.ErrSchemaUnsupported}, {"nil-publisher", fact(), nil, ErrUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			m := manager(t, resource{publisher: tc.publisher})
			if e := m.Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return tc.publisher.Publish(c, tc.event) }); !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
	var queue *Queue
	m := manager(t, resource{publisher: p, queue: &queue})
	if e := m.Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return p.Publish(c, fact()) }); e != nil || s.appended != 1 {
		t.Fatal(e, s.appended)
	}
	if e := queue.Flush(context.Background()); !errors.Is(e, uow.ErrClosed) {
		t.Fatal(e)
	}
	cause := errors.New("write failed")
	s.appendErr = cause
	if e := m.Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return p.Publish(c, fact()) }); !errors.Is(e, cause) {
		t.Fatal(e)
	}
}
func TestDeliveryPolicyAndWorker(t *testing.T) {
	if (Policy{}).Validate() == nil {
		t.Fatal("limits required")
	}
	p := validPolicy()
	p.JitterFraction = 2
	if p.Validate() == nil {
		t.Fatal("invalid jitter")
	}
	p.JitterFraction = .2
	for _, n := range []int{1, 2, 10} {
		delay := p.Backoff(n)
		if delay <= 0 || delay > p.RetryMax {
			t.Fatal(delay)
		}
	}
	p.RetryInitial = 3 * time.Millisecond
	if p.Backoff(4) > p.RetryMax {
		t.Fatal("backoff cap")
	}
	p = validPolicy()
	s := &store{}
	b := &broker{}
	allow := func(context.Context, distributed.Envelope) error { return nil }
	for _, tc := range []struct {
		s    Store
		b    distributed.BrokerPublisher
		p    Policy
		name string
		a    func(context.Context, distributed.Envelope) error
	}{{nil, b, p, "w", allow}, {s, nil, p, "w", allow}, {s, b, p, "", allow}, {s, b, p, "w", nil}, {s, b, Policy{}, "w", allow}} {
		if _, e := NewWorker(tc.s, tc.b, tc.p, tc.name, tc.a); e == nil {
			t.Fatal("invalid worker")
		}
	}
	w, _ := NewWorker(s, b, p, "worker", allow)
	if ok, e := w.Dispatch(context.Background()); e != nil || ok {
		t.Fatal(e, ok)
	}
	cause := errors.New("DB failed")
	s.claimErr = cause
	if _, e := w.Dispatch(context.Background()); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	s.claimErr = nil
	s.claim = &Claim{Envelope: fact(), Token: "token", Attempts: 2}
	if ok, e := w.Dispatch(context.Background()); e != nil || !ok || !s.success || b.calls != 1 {
		t.Fatal(e, ok)
	}
	b.err = cause
	if ok, e := w.Dispatch(context.Background()); !errors.Is(e, cause) || ok || s.success {
		t.Fatal(e, ok)
	}
	denied, _ := NewWorker(s, b, p, "worker", func(context.Context, distributed.Envelope) error { return scope.ErrDenied })
	before := b.calls
	if ok, e := denied.Dispatch(context.Background()); !errors.Is(e, scope.ErrDenied) || ok || b.calls != before {
		t.Fatal(e, ok)
	}
	b.err = nil
	s.completeErr = cause
	if ok, e := w.Dispatch(context.Background()); !errors.Is(e, cause) || ok {
		t.Fatal(e, ok)
	}
}
