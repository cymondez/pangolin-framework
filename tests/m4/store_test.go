//go:build integration

package m4

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/events/distributed"
	"github.com/pangolin-framework/pangolin/events/outbox"
	eventpg "github.com/pangolin-framework/pangolin/events/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/platform/jobs"
	jobpg "github.com/pangolin-framework/pangolin/platform/jobs/postgres"
	"github.com/pangolin-framework/pangolin/security/scope"
)

func TestJobStoreBoundaries(t *testing.T) {
	f := newFixture(t, nil)
	j := f.job(t)
	ctx := f.bind(t, context.Background(), f.tenant, j.Actor)
	if _, e := jobpg.NewStore(nil, f.registry, f.policy, func(context.Context, string) error { return nil }); e == nil {
		t.Fatal("nil pool")
	}
	if _, e := jobpg.NewStore(f.queue, f.registry, jobs.Policy{}, func(context.Context, string) error { return nil }); e == nil {
		t.Fatal("invalid policy")
	}
	if e := f.store.Enqueue(ctx, j); e == nil {
		t.Fatal("enqueue without UOW")
	}
	invalid := j
	invalid.Version = 2
	if e := f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { _ = f.store.Enqueue(c, invalid); return nil }); e == nil {
		t.Fatal("swallowed enqueue error committed")
	}
	f.enqueue(t, j)
	// Two independent claimers cross one barrier. Exactly one obtains this job.
	start := make(chan struct{})
	claims := make(chan *jobs.Claim, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c, e := f.store.Claim(context.Background(), "competitor")
			if e != nil {
				errs <- e
			}
			claims <- c
		}()
	}
	close(start)
	wg.Wait()
	close(claims)
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	var held *jobs.Claim
	for c := range claims {
		if c != nil {
			if held != nil {
				t.Fatal("double claim")
			}
			held = c
		}
	}
	if held == nil {
		t.Fatal("no claim")
	}
	must(t, f.store.Release(context.Background(), *held))
	state, _ := f.state(t, j.ID)
	if state != "pending" {
		t.Fatal(state)
	}
	other := f.bind(t, context.Background(), "tenant-b", j.Actor)
	e := f.manager.Within(other, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Cancel(c, j.ID, "trace") })
	if e == nil {
		t.Fatal("cross owner cancellation")
	}
	e = f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Replay(c, j.ID, "trace") })
	if !errors.Is(e, jobs.ErrInvalid) {
		t.Fatal(e)
	}
	must(t, f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Cancel(c, j.ID, "trace") }))
	state, _ = f.state(t, j.ID)
	if state != "cancelled" {
		t.Fatal(state)
	}
	_, e = f.admin.Exec(context.Background(), `UPDATE pangolin_jobs.jobs SET finished_at=now()-interval '2 hours'`)
	must(t, e)
	n, e := f.store.Purge(context.Background())
	must(t, e)
	if n != 1 {
		t.Fatal(n)
	}
	denied, e := jobpg.NewStore(f.queue, f.registry, f.policy, func(context.Context, string) error { return scope.ErrDenied })
	must(t, e)
	e = f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return denied.Enqueue(c, j) })
	if !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if _, e = f.store.Claim(context.Background(), ""); e == nil {
		t.Fatal("empty worker")
	}
}
func TestEventCapacity(t *testing.T) {
	f := newFixture(t, nil)
	p := outbox.Policy{Lease: time.Second, SendTimeout: time.Second, RetryInitial: time.Millisecond, RetryMax: time.Second, MaxAttempts: 2}
	if _, e := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{}); e == nil {
		t.Fatal("unbounded capacity")
	}
	store, e := eventpg.NewBoundedStore(f.queue, p, eventpg.Capacity{Outbox: 1, Inbox: 1})
	must(t, e)
	provisionQueue(t, f, "outbox", 1, "resize")
	provisionQueue(t, f, "inbox", 1, "resize")
	if _, e = store.Stats(context.Background(), "invalid"); e == nil {
		t.Fatal("invalid queue")
	}
	fact := envelope(scope.ScopeRef{TenantID: "t", Kind: "tenant", ID: "t"})
	raw, e := distributed.Encode(fact)
	must(t, e)
	must(t, store.Persist(context.Background(), "group", fact, raw, fact.Producer))
	must(t, store.Persist(context.Background(), "group", fact, raw, fact.Producer))
	second := fact
	second.ID = "other"
	encoded, e := distributed.Encode(second)
	must(t, e)
	if e = store.Persist(context.Background(), "group", second, encoded, second.Producer); !errors.Is(e, eventpg.ErrCapacity) {
		t.Fatal(e)
	}
	st, e := store.Stats(context.Background(), "inbox")
	must(t, e)
	if st.Pending != 1 {
		t.Fatal(st)
	}
}
