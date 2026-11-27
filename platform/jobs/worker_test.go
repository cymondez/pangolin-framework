package jobs

import (
	"context"
	"encoding/json"
	"github.com/pangolin-framework/pangolin/security/scope"
	"testing"
	"time"
)

type fakeStore struct {
	claim                                      *Claim
	claimErr, finishErr, cancelErr, releaseErr error
	cancelled                                  bool
	released                                   bool
}

func (s *fakeStore) Claim(context.Context, string) (*Claim, error) { return s.claim, s.claimErr }
func (s *fakeStore) Finish(context.Context, Claim, bool, time.Duration, string) error {
	return s.finishErr
}
func (s *fakeStore) Cancelled(context.Context, Claim) (bool, error) { return s.cancelled, s.cancelErr }
func (s *fakeStore) Release(context.Context, Claim) error           { s.released = true; return s.releaseErr }
func workerPolicy() Policy {
	return Policy{Lease: time.Second, ExecutionTimeout: 100 * time.Millisecond, CleanupTimeout: 100 * time.Millisecond, PollInterval: time.Millisecond, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 3, Concurrency: 1, BatchSize: 1, MaxPending: 10, Retention: time.Hour}
}
func TestWorkerFailures(t *testing.T) {
	valid := Job{ID: "j", Type: "test", Version: 1, Owner: scope.ScopeRef{TenantID: "t", Kind: "tenant", ID: "t"}, Actor: scope.PrincipalRef{ID: "user", Kind: "user"}, Payload: json.RawMessage(`{}`), DueAt: time.Now(), Deadline: time.Now().Add(time.Minute)}
	bind := func(ctx context.Context, j Job) (context.Context, error) {
		r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
			return scope.PrincipalRef{ID: "worker", Kind: "service"}, j.Owner.TenantID, true
		})
		return r.Bind(ctx, j.Owner)
	}
	actor := func(context.Context, Job) error { return nil }
	for _, name := range []string{"empty", "claim-error", "bad-schema", "bind-error", "wrong-binding", "delegation", "cancelled", "cancel-error", "panic", "finish-error", "shutdown-release"} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{claim: &Claim{Job: valid, Token: "token", Attempts: 1}}
			binder := bind
			check := actor
			handler := func(context.Context, Job) error { return nil }
			ctx := context.Background()
			switch name {
			case "empty":
				store.claim = nil
			case "claim-error":
				store.claimErr = ErrInvalid
			case "bad-schema":
				store.claim.Job.Version = 2
			case "bind-error":
				binder = func(context.Context, Job) (context.Context, error) { return nil, scope.ErrDenied }
			case "wrong-binding":
				binder = func(ctx context.Context, _ Job) (context.Context, error) { return ctx, nil }
			case "delegation":
				store.claim.Job.Delegated = true
				check = func(context.Context, Job) error { return scope.ErrDenied }
			case "cancelled":
				store.cancelled = true
			case "cancel-error":
				store.cancelErr = ErrLeaseLost
			case "panic":
				handler = func(context.Context, Job) error { panic("secret") }
			case "finish-error":
				store.finishErr = ErrLeaseLost
			case "shutdown-release":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				handler = func(context.Context, Job) error { cancel(); return nil }
			}
			registry, e := NewRegistry(32, Definition{Type: "test", Version: 1, Validate: func(json.RawMessage) error { return nil }, Handle: handler})
			if e != nil {
				t.Fatal(e)
			}
			w, e := NewWorker(store, registry, workerPolicy(), "w", binder, check, nil)
			if e != nil {
				t.Fatal(e)
			}
			ok, e := w.Process(ctx)
			if name == "empty" {
				if ok || e != nil {
					t.Fatal(ok, e)
				}
			} else if ok || e == nil {
				t.Fatal("failure accepted", ok, e)
			}
			if name == "shutdown-release" && !store.released {
				t.Fatal("lease not released")
			}
			if e = w.Quiesce(context.Background()); e != nil {
				t.Fatal(e)
			}
			w.Cancel()
			if e = w.Drain(context.Background()); e != nil {
				t.Fatal(e)
			}
			if _, e = w.Process(context.Background()); e == nil {
				t.Fatal("admitted after quiesce")
			}
		})
	}
	registry, _ := NewRegistry(32, Definition{Type: "test", Version: 1, Validate: func(json.RawMessage) error { return nil }, Handle: func(context.Context, Job) error { return nil }})
	for _, build := range []func() (*Worker, error){func() (*Worker, error) { return NewWorker(nil, registry, workerPolicy(), "w", bind, actor, nil) }, func() (*Worker, error) { return NewWorker(&fakeStore{}, registry, Policy{}, "w", bind, actor, nil) }} {
		if _, e := build(); e == nil {
			t.Fatal("invalid worker")
		}
	}
}
