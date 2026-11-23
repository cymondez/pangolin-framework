//go:build integration

package m4

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/platform/jobs"
	"github.com/cymondez/pangolin-framework/security/scope"
)

func TestA64Jobs(t *testing.T) {
	t.Run("bounded-retry", func(t *testing.T) {
		f := newFixture(t, func(context.Context, jobs.Job) error { return errors.New("secret handler error") })
		j := f.job(t)
		f.enqueue(t, j)
		w := f.worker(t, nil)
		for i := 1; i <= f.policy.MaxAttempts; i++ {
			f.due(t, j.ID)
			ok, err := w.Process(context.Background())
			if ok || err == nil {
				t.Fatal(ok, err)
			}
		}
		state, n := f.state(t, j.ID)
		if state != "dead" || n != 3 {
			t.Fatal(state, n)
		}
		ok, err := w.Process(context.Background())
		must(t, err)
		if ok {
			t.Fatal("exhausted job executed")
		}
		var code string
		must(t, f.admin.QueryRow(context.Background(), `SELECT last_error FROM pangolin_jobs.jobs WHERE id=$1`, j.ID).Scan(&code))
		if code != "JOB_EXECUTION_FAILED" {
			t.Fatal(code)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		entered := make(chan struct{})
		f := newFixture(t, func(ctx context.Context, _ jobs.Job) error { close(entered); <-ctx.Done(); return ctx.Err() })
		j := f.job(t)
		f.enqueue(t, j)
		w := f.worker(t, nil)
		result := make(chan error, 1)
		go func() { _, err := w.Process(context.Background()); result <- err }()
		<-entered
		ctx := f.bind(t, context.Background(), f.tenant, j.Actor)
		must(t, f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Cancel(c, j.ID, "trace-cancel") }))
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("cancel not observed")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cancellation not bounded")
		}
		state, _ := f.state(t, j.ID)
		if state != "cancelled" {
			t.Fatal(state)
		}
	})
	t.Run("stale-lease", func(t *testing.T) {
		f := newFixture(t, nil)
		j := f.job(t)
		f.enqueue(t, j)
		ctx := context.Background()
		first, err := f.store.Claim(ctx, "one")
		must(t, err)
		_, err = f.admin.Exec(ctx, `UPDATE pangolin_jobs.jobs SET locked_until=now()-interval '1 microsecond' WHERE id=$1`, j.ID)
		must(t, err)
		second, err := f.store.Claim(ctx, "two")
		must(t, err)
		if second == nil || first.Token == second.Token {
			t.Fatal("lease not replaced")
		}
		for _, finish := range []func() error{func() error { return f.store.Finish(ctx, *first, true, 0, "") }, func() error { return f.store.Finish(ctx, *first, false, 0, "bad") }, func() error { return f.store.Release(ctx, *first) }} {
			if !errors.Is(finish(), jobs.ErrLeaseLost) {
				t.Fatal("stale token accepted")
			}
		}
		must(t, f.store.Finish(ctx, *second, true, 0, ""))
		state, n := f.state(t, j.ID)
		if state != "succeeded" || n != 2 {
			t.Fatal(state, n)
		}
	})
	t.Run("manual-replay-audit", func(t *testing.T) {
		f := newFixture(t, nil)
		j := f.job(t)
		f.enqueue(t, j)
		_, err := f.admin.Exec(context.Background(), `UPDATE pangolin_jobs.jobs SET state='dead' WHERE id=$1`, j.ID)
		must(t, err)
		ctx := f.bind(t, context.Background(), f.tenant, j.Actor)
		// An audit validation error must roll the state change back.
		err = f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Replay(c, j.ID, "invalid trace secret") })
		if err == nil {
			t.Fatal("invalid audit accepted")
		}
		state, _ := f.state(t, j.ID)
		if state != "dead" {
			t.Fatal(state)
		}
		must(t, f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Replay(c, j.ID, "trace-replay") }))
		var count int
		must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_audit.records WHERE action='jobs.replay' AND resource=$1`, j.ID).Scan(&count))
		if count != 1 {
			t.Fatal(count)
		}
		ok, err := f.worker(t, nil).Process(context.Background())
		must(t, err)
		if !ok {
			t.Fatal("replayed job not processed")
		}
	})
	t.Run("tenant-tamper", func(t *testing.T) {
		var called atomic.Int32
		f := newFixture(t, func(context.Context, jobs.Job) error { called.Add(1); return nil })
		j := f.job(t)
		ctx := f.bind(t, context.Background(), f.tenant, j.Actor)
		forged := j
		forged.Owner = scope.ScopeRef{TenantID: "other", Kind: "tenant", ID: "other"}
		err := f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, forged) })
		if !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
		f.enqueue(t, j)
		_, err = f.admin.Exec(context.Background(), `UPDATE pangolin_jobs.jobs SET document=convert_to(replace(convert_from(document,'UTF8'),'tenant-a','tenant-b'),'UTF8') WHERE id=$1`, j.ID)
		must(t, err)
		if _, err = f.worker(t, nil).Process(context.Background()); !errors.Is(err, jobs.ErrInvalid) {
			t.Fatal(err)
		}
		if called.Load() != 0 {
			t.Fatal("tampered job ran")
		}
		state, _ := f.state(t, j.ID)
		if state != "dead" {
			t.Fatal(state)
		}
	})
	t.Run("handler-idempotency", func(t *testing.T) {
		var f *fixture
		f = newFixture(t, func(ctx context.Context, j jobs.Job) error {
			_, err := f.queue.Exec(ctx, `INSERT INTO pangolin.job_effects(id) VALUES($1) ON CONFLICT DO NOTHING`, j.ID)
			return err
		})
		j := f.job(t)
		f.enqueue(t, j)
		// Simulate a crash after the external side effect, before local completion.
		first, err := f.store.Claim(context.Background(), "crashed")
		must(t, err)
		must(t, f.registry.Handle(context.Background(), first.Job))
		_, err = f.admin.Exec(context.Background(), `UPDATE pangolin_jobs.jobs SET locked_until=now()-interval '1 microsecond' WHERE id=$1`, j.ID)
		must(t, err)
		ok, err := f.worker(t, nil).Process(context.Background())
		must(t, err)
		if !ok {
			t.Fatal("retry failed")
		}
		var count int
		must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin.job_effects WHERE id=$1`, j.ID).Scan(&count))
		if count != 1 {
			t.Fatal(count)
		}
	})
}
func TestJobAdmissionAndAuthority(t *testing.T) {
	t.Run("global-capacity-concurrent-tenants", func(t *testing.T) {
		f := newFixture(t, nil)
		const total = 16
		start := make(chan struct{})
		var wg sync.WaitGroup
		var accepted atomic.Int32
		errs := make(chan error, total)
		for i := 0; i < total; i++ {
			j := f.job(t)
			if i%2 == 1 {
				j.Owner = scope.ScopeRef{TenantID: "tenant-b", Kind: "tenant", ID: "tenant-b"}
			}
			ctx := f.bind(t, context.Background(), j.Owner.TenantID, j.Actor)
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				err := f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return f.store.Enqueue(c, j) })
				if err == nil {
					accepted.Add(1)
				} else if !errors.Is(err, jobs.ErrCapacity) {
					errs <- err
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if int(accepted.Load()) != f.policy.MaxPending {
			t.Fatal(accepted.Load())
		}
		var count int
		must(t, f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pangolin_jobs.jobs`).Scan(&count))
		if count != f.policy.MaxPending {
			t.Fatal(count)
		}
	})
	t.Run("rollback-does-not-enqueue", func(t *testing.T) {
		f := newFixture(t, nil)
		j := f.job(t)
		ctx := f.bind(t, context.Background(), f.tenant, j.Actor)
		err := f.manager.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := f.store.Enqueue(c, j); e != nil {
				return e
			}
			return errors.New("rollback")
		})
		if err == nil {
			t.Fatal("rollback missing")
		}
		c, e := f.store.Claim(context.Background(), "w")
		must(t, e)
		if c != nil {
			t.Fatal("uncommitted work visible")
		}
	})
	t.Run("revoked-service", func(t *testing.T) {
		var called atomic.Int32
		f := newFixture(t, func(context.Context, jobs.Job) error { called.Add(1); return nil })
		j := f.job(t)
		f.enqueue(t, j)
		w := f.worker(t, nil)
		_, err := f.admin.Exec(context.Background(), `UPDATE pangolin_security.tenant_memberships SET state='revoked',revision=revision+1 WHERE principal_kind='service'`)
		must(t, err)
		if _, err = w.Process(context.Background()); err == nil {
			t.Fatal("revoked grant accepted")
		}
		if called.Load() != 0 {
			t.Fatal("side effect after revocation")
		}
	})
	t.Run("delegated-user", func(t *testing.T) {
		f := newFixture(t, nil)
		j := f.job(t)
		j.Delegated = true
		f.enqueue(t, j)
		w := f.worker(t, func(context.Context, jobs.Job) error { return scope.ErrDenied })
		if _, err := w.Process(context.Background()); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
	})
	t.Run("scheduled-and-expired", func(t *testing.T) {
		f := newFixture(t, nil)
		j := f.job(t)
		j.DueAt = time.Now().Add(time.Hour)
		j.Deadline = j.DueAt.Add(time.Hour)
		f.enqueue(t, j)
		w := f.worker(t, nil)
		ok, err := w.Process(context.Background())
		must(t, err)
		if ok {
			t.Fatal("schedule ran early")
		}
		_, err = f.admin.Exec(context.Background(), `UPDATE pangolin_jobs.jobs SET deadline=now()-interval '1 second',due_at=now()-interval '2 seconds'`)
		must(t, err)
		ok, err = w.Process(context.Background())
		must(t, err)
		if ok {
			t.Fatal("expired job ran")
		}
		state, _ := f.state(t, j.ID)
		if state != "dead" {
			t.Fatal(state)
		}
	})
}
