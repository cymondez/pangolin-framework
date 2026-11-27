package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type store struct {
	candidate                                                 *Candidate
	persistErr, candidateErr, lockErr, processedErr, retryErr error
	locked                                                    bool
	retries                                                   int
}

func (s *store) Persist(context.Context, string, distributed.Envelope, []byte, string) error {
	return s.persistErr
}
func (s *store) Candidate(context.Context, string) (*Candidate, error) {
	return s.candidate, s.candidateErr
}
func (s *store) Lock(context.Context, Candidate) (bool, error) { return s.locked, s.lockErr }
func (s *store) Processed(context.Context, Candidate) error    { return s.processedErr }
func (s *store) Retry(context.Context, Candidate, int, time.Duration) error {
	s.retries++
	return s.retryErr
}
func fact() distributed.Envelope {
	return distributed.Envelope{ID: "event", Type: "fact", SchemaVersion: 1, OccurredAt: time.Now(), TenantID: "t", ScopeKind: "tenant", ScopeID: "t", CorrelationID: "operation", Producer: "trusted", Payload: []byte(`{}`)}
}

type resource struct{}
type transaction struct{}

func (resource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return c, transaction{}, nil
}
func (transaction) Commit(context.Context) error   { return nil }
func (transaction) Rollback(context.Context) error { return nil }
func manager(t *testing.T) uow.UnitOfWorkManager {
	m, e := uow.NewManager(resource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func bound(t *testing.T, kind, tenant string) context.Context {
	r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "subject", Kind: kind}, tenant, true
	})
	c, e := r.Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestReceiverGuards(t *testing.T) {
	s := &store{}
	registry, _ := distributed.NewRegistry(distributed.Schema{Type: "fact", Version: 1})
	verify := func(context.Context, distributed.Envelope) (string, error) { return "trusted", nil }
	alert := func(context.Context, error) {}
	for _, tc := range []struct {
		s Store
		r *distributed.Registry
		g string
		v Verify
		a func(context.Context, error)
	}{{nil, registry, "g", verify, alert}, {s, nil, "g", verify, alert}, {s, registry, "", verify, alert}, {s, registry, "g", nil, alert}, {s, registry, "g", verify, nil}} {
		if _, e := NewReceiver(tc.s, tc.r, tc.g, tc.v, tc.a); e == nil {
			t.Fatal("bad receiver")
		}
	}
	r, _ := NewReceiver(s, registry, "g", verify, alert)
	if e := r.Receive(context.Background(), fact(), nil); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	ack := func(context.Context) error { return nil }
	if e := r.Receive(context.Background(), distributed.Envelope{}, ack); e == nil {
		t.Fatal("invalid event")
	}
	r.verify = func(context.Context, distributed.Envelope) (string, error) { return "other", nil }
	if e := r.Receive(context.Background(), fact(), ack); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	cause := errors.New("transport identity unavailable")
	r.verify = func(context.Context, distributed.Envelope) (string, error) { return "", cause }
	if e := r.Receive(context.Background(), fact(), ack); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	r.verify = verify
	s.persistErr = cause
	if e := r.Receive(context.Background(), fact(), ack); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	alerts := 0
	r.alert = func(context.Context, error) { alerts++ }
	s.persistErr = ErrContentConflict
	if e := r.Receive(context.Background(), fact(), ack); !errors.Is(e, ErrContentConflict) || alerts != 1 {
		t.Fatal(e, alerts)
	}
	s.persistErr = nil
	if e := r.Receive(context.Background(), fact(), func(context.Context) error { return cause }); !errors.Is(e, cause) {
		t.Fatal(e)
	}
}
func TestWorkerGuardsAndFailures(t *testing.T) {
	s := &store{}
	m := manager(t)
	bind := func(context.Context, Candidate) (context.Context, error) { return bound(t, "service", "t"), nil }
	handler := func(context.Context, distributed.Envelope) error { return nil }
	for _, tc := range []struct {
		s Store
		m uow.UnitOfWorkManager
		g string
		b func(context.Context, Candidate) (context.Context, error)
		h func(context.Context, distributed.Envelope) error
		n int
		d time.Duration
	}{{nil, m, "g", bind, handler, 1, time.Second}, {s, nil, "g", bind, handler, 1, time.Second}, {s, m, "", bind, handler, 1, time.Second}, {s, m, "g", nil, handler, 1, time.Second}, {s, m, "g", bind, nil, 1, time.Second}, {s, m, "g", bind, handler, 0, time.Second}, {s, m, "g", bind, handler, 1, 0}} {
		if _, e := NewWorker(tc.s, tc.m, tc.g, tc.b, tc.h, tc.n, tc.d); e == nil {
			t.Fatal("bad worker")
		}
	}
	w, _ := NewWorker(s, m, "g", bind, handler, 3, time.Millisecond)
	if ok, e := w.Process(context.Background()); e != nil || ok {
		t.Fatal(e, ok)
	}
	cause := errors.New("failure")
	s.candidateErr = cause
	if _, e := w.Process(context.Background()); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	s.candidateErr = nil
	s.candidate = &Candidate{Group: "g", ID: "event", Envelope: fact(), Version: 1}
	w.bind = func(context.Context, Candidate) (context.Context, error) { return nil, cause }
	if _, e := w.Process(context.Background()); !errors.Is(e, cause) || s.retries != 1 {
		t.Fatal(e)
	}
	for i, ctx := range []context.Context{context.Background(), bound(t, "user", "t"), bound(t, "service", "other")} {
		w.bind = func(context.Context, Candidate) (context.Context, error) { return ctx, nil }
		if _, e := w.Process(context.Background()); !errors.Is(e, scope.ErrDenied) || s.retries != i+2 {
			t.Fatal(e)
		}
	}
	w.bind = bind
	if ok, e := w.Process(context.Background()); e != nil || ok {
		t.Fatal(e, ok)
	}
	s.lockErr = cause
	if _, e := w.Process(context.Background()); !errors.Is(e, cause) || s.retries != 5 {
		t.Fatal(e)
	}
	s.lockErr = nil
	s.locked = true
	w.handler = func(context.Context, distributed.Envelope) error { return cause }
	if ok, e := w.Process(context.Background()); !errors.Is(e, cause) || ok || s.retries != 6 {
		t.Fatal(e, ok, s.retries)
	}
	w.handler = handler
	s.processedErr = cause
	if ok, e := w.Process(context.Background()); !errors.Is(e, cause) || ok || s.retries != 7 {
		t.Fatal(e, ok, s.retries)
	}
	s.processedErr = nil
	if ok, e := w.Process(context.Background()); e != nil || !ok {
		t.Fatal(e, ok)
	}
}
func TestExternalIdentity(t *testing.T) {
	for _, tc := range []struct {
		group     string
		e         distributed.Envelope
		operation string
	}{{"", fact(), "op"}, {"g", distributed.Envelope{}, "op"}, {"g", fact(), ""}} {
		if _, e := ExternalKey(tc.group, tc.e, tc.operation); e == nil {
			t.Fatal("incomplete external key")
		}
	}
}
