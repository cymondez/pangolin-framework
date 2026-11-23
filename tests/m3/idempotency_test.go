//go:build integration

package m3

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/idempotency"
	"github.com/cymondez/pangolin-framework/persistence/migration"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5/pgxpool"
)

type idemAuthority struct {
	mu      sync.Mutex
	revoked map[string]bool
}

func (a *idemAuthority) Kinds() []string                      { return []string{"tenant", "org.organization"} }
func idemGrant(p scope.PrincipalRef, r scope.ScopeRef) string { return fmt.Sprint(p, r) }
func (a *idemAuthority) Resolve(_ context.Context, p scope.PrincipalRef, r scope.ScopeRef) (scope.ScopeBinding, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.revoked[idemGrant(p, r)] {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	return scope.ScopeBinding{Ref: r, Principal: p, AuthorityRevision: "1"}, nil
}
func (a *idemAuthority) Authorize(ctx context.Context, b scope.ScopeBinding, _ string) error {
	_, err := a.Resolve(ctx, b.Principal, b.Ref)
	return err
}
func (a *idemAuthority) revoke(p scope.PrincipalRef, r scope.ScopeRef) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revoked[idemGrant(p, r)] = true
}

type idemFixture struct {
	db        *sql.DB
	app       *pgxpool.Pool
	m         uow.UnitOfWorkManager
	store     *idempotency.Store
	authority *idemAuthority
	ctx       context.Context
	tenant    string
}

func newIdemFixture(t *testing.T) *idemFixture {
	t.Helper()
	db, dsn := isolatedDB(t)
	source := migration.JoinFS(map[string]fs.FS{"scope": sub(migration.ScopeMigrations), "idempotency": sub(idempotency.Migrations)})
	runner, err := migration.NewPostgres(db, source, 3300001, 3300001)
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.Apply(context.Background(), []migration.ModuleMigrations{{Module: "scope", Directory: "scope"}, {Module: "idempotency", Directory: "idempotency", DependsOn: []string{"scope"}}}); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE public.idem_effects(tenant_id text NOT NULL,scope_kind text NOT NULL,scope_id text NOT NULL,id text NOT NULL,PRIMARY KEY(tenant_id,scope_kind,scope_id,id),FOREIGN KEY(tenant_id,scope_kind,scope_id) REFERENCES pangolin.resource_scopes(tenant_id,scope_kind,scope_id));
 ALTER TABLE public.idem_effects ENABLE ROW LEVEL SECURITY;ALTER TABLE public.idem_effects FORCE ROW LEVEL SECURITY;
 CREATE POLICY exact_owner ON public.idem_effects USING(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true) AND scope_kind=current_setting('app.scope_kind',true) AND scope_id=current_setting('app.scope_id',true));
 GRANT SELECT,INSERT ON public.idem_effects TO pangolin_app;`)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword("pangolin_app", "local-app-only")
	app, err := pgxpool.New(context.Background(), parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	f := &idemFixture{db: db, app: app, m: manager(t, &postgres.Resource{Pool: app}), authority: &idemAuthority{revoked: map[string]bool{}}, tenant: "tenant-" + rand.Text()}
	if _, err = db.Exec(`INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,'tenant',$1),($1,'org.organization','org-a'),($1,'org.organization','org-b')`, f.tenant); err != nil {
		t.Fatal(err)
	}
	plain := func(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }
	f.store, err = idempotency.New(idempotency.Config{MaxKeyBytes: 128, MaxBodyBytes: 1 << 20, RetryWindow: time.Hour, Retention: 24 * time.Hour, Protect: plain, Unprotect: plain}, func(ctx context.Context, op string) error {
		b, ok := scope.Current(ctx)
		if !ok {
			return scope.ErrDenied
		}
		return f.authority.Authorize(ctx, b, op)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.ctx = f.bind(t, scope.PrincipalRef{ID: "alice", Kind: "user"}, scope.ScopeRef{TenantID: f.tenant, Kind: "tenant", ID: f.tenant})
	return f
}
func (f *idemFixture) bind(t *testing.T, p scope.PrincipalRef, ref scope.ScopeRef) context.Context {
	t.Helper()
	identity := func(context.Context) (scope.PrincipalRef, string, bool) { return p, ref.TenantID, true }
	registry, err := scope.NewRegistry(identity, f.authority)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := registry.Bind(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
func (f *idemFixture) effect(ctx context.Context, id string) error {
	b, _ := scope.Current(ctx)
	ex, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `INSERT INTO public.idem_effects VALUES($1,$2,$3,$4)`, b.Ref.TenantID, b.Ref.Kind, b.Ref.ID, id)
	return err
}
func (f *idemFixture) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM public.idem_effects`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func (f *idemFixture) execute(ctx context.Context, key, body, id string, hook func()) error {
	return f.m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		_, _, err := f.store.Execute(c, "books.create", key, []byte(body), func(work context.Context) ([]byte, error) {
			if hook != nil {
				hook()
			}
			return []byte(`{"id":"` + id + `"}`), f.effect(work, id)
		})
		return err
	})
}
func (f *idemFixture) query(t *testing.T, ctx context.Context, key string) idempotency.Result {
	t.Helper()
	var result idempotency.Result
	err := f.m.Within(ctx, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
		var err error
		result, err = f.store.Query(c, "books.create", key)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (f *idemFixture) waitForWaiter(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		err := f.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO pangolin_idempotency.results%')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("idempotency waiter never reached unique-key lock")
		case <-tick.C:
		}
	}
}

func TestA60Idempotency(t *testing.T) {
	t.Run("same-key-same-body", func(t *testing.T) {
		f := newIdemFixture(t)
		key, id := rand.Text(), rand.Text()
		entered, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		results := make(chan error, 2)
		go func() {
			results <- f.execute(f.ctx, key, `{"title":"one","n":1}`, id, func() { calls.Add(1); close(entered); <-release })
		}()
		<-entered
		started := make(chan struct{})
		go func() {
			close(started)
			results <- f.execute(f.ctx, key, `{"n":1,"title":"one"}`, id, func() { calls.Add(1) })
		}()
		<-started
		f.waitForWaiter(t)
		close(release)
		for range 2 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != 1 || f.count(t) != 1 {
			t.Fatal(calls.Load(), f.count(t))
		}
	})
	t.Run("same-key-different-body", func(t *testing.T) {
		f := newIdemFixture(t)
		key := rand.Text()
		if err := f.execute(f.ctx, key, `{"v":1}`, "first", nil); err != nil {
			t.Fatal(err)
		}
		if err := f.execute(f.ctx, key, `{"v":2}`, "second", nil); !errors.Is(err, idempotency.ErrConflict) {
			t.Fatal(err)
		}
		if f.count(t) != 1 {
			t.Fatal("conflict side effect")
		}
	})
	t.Run("holder-rollback", func(t *testing.T) {
		f := newIdemFixture(t)
		key := rand.Text()
		entered, release := make(chan struct{}), make(chan struct{})
		results := make(chan error, 2)
		boom := errors.New("holder rolled back")
		go func() {
			results <- f.m.Within(f.ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				_, _, err := f.store.Execute(c, "books.create", key, []byte(`{}`), func(work context.Context) ([]byte, error) {
					if err := f.effect(work, "rolled-back"); err != nil {
						return nil, err
					}
					close(entered)
					<-release
					return nil, boom
				})
				return err
			})
		}()
		<-entered
		started := make(chan struct{})
		go func() { close(started); results <- f.execute(f.ctx, key, `{}`, "committed", nil) }()
		<-started
		f.waitForWaiter(t)
		close(release)
		first, second := <-results, <-results
		if (first == nil) == (second == nil) || !errors.Is(errors.Join(first, second), boom) || f.count(t) != 1 {
			t.Fatal(first, second, f.count(t))
		}
	})
	t.Run("wait-timeout", func(t *testing.T) {
		f := newIdemFixture(t)
		key := rand.Text()
		entered, release := make(chan struct{}), make(chan struct{})
		result := make(chan error, 1)
		go func() { result <- f.execute(f.ctx, key, `{}`, "holder", func() { close(entered); <-release }) }()
		<-entered
		ctx, cancel := context.WithTimeout(f.ctx, 60*time.Millisecond)
		err := f.execute(ctx, key, `{}`, "waiter", nil)
		cancel()
		close(release)
		if !errors.Is(err, idempotency.ErrInProgress) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if err = <-result; err != nil {
			t.Fatal(err)
		}
		if f.count(t) != 1 {
			t.Fatal("timed out waiter wrote")
		}
	})
	t.Run("caught-error-cannot-commit-pending", func(t *testing.T) {
		f := newIdemFixture(t)
		key := rand.Text()
		boom := errors.New("work failed")
		err := f.m.Within(f.ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			_, _, err := f.store.Execute(c, "books.create", key, []byte(`{}`), func(work context.Context) ([]byte, error) {
				if err := f.effect(work, "failure"); err != nil {
					return nil, err
				}
				return nil, boom
			})
			if !errors.Is(err, boom) {
				t.Fatal(err)
			}
			return nil
		})
		if !errors.Is(err, uow.ErrRollbackOnly) || f.count(t) != 0 || f.query(t, f.ctx, key).State != "unknown" {
			t.Fatal(err)
		}
	})
	t.Run("caught-panic-cannot-commit-pending", func(t *testing.T) {
		f := newIdemFixture(t)
		key := rand.Text()
		panicValue := errors.New("work panic")
		err := f.m.Within(f.ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			func() {
				defer func() {
					if recover() != panicValue {
						t.Fatal("panic changed")
					}
				}()
				_, _, _ = f.store.Execute(c, "books.create", key, []byte(`{}`), func(work context.Context) ([]byte, error) {
					if err := f.effect(work, "panic"); err != nil {
						return nil, err
					}
					panic(panicValue)
				})
			}()
			return nil
		})
		if !errors.Is(err, uow.ErrRollbackOnly) || f.count(t) != 0 || f.query(t, f.ctx, key).State != "unknown" {
			t.Fatal(err)
		}
	})
}

func TestA07ResultReplay(t *testing.T) {
	if os.Getenv("PANGOLIN_IDEMPOTENCY_CRASH_HELPER") == "1" {
		app, err := pgxpool.New(context.Background(), os.Getenv("PANGOLIN_IDEMPOTENCY_CRASH_DSN"))
		if err != nil {
			t.Fatal(err)
		}
		defer app.Close()
		tenant := os.Getenv("PANGOLIN_IDEMPOTENCY_CRASH_TENANT")
		f := &idemFixture{app: app, m: manager(t, &postgres.Resource{Pool: app}), tenant: tenant}
		identity := func(context.Context) (scope.PrincipalRef, string, bool) {
			return scope.PrincipalRef{ID: "alice", Kind: "user"}, tenant, true
		}
		ctx, err := scope.NewTenantResolver(identity).Bind(context.Background(), scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant})
		if err != nil {
			t.Fatal(err)
		}
		plain := func(raw []byte) ([]byte, error) { return raw, nil }
		f.store, err = idempotency.New(idempotency.Config{MaxKeyBytes: 128, MaxBodyBytes: 1 << 20, RetryWindow: time.Hour, Retention: 24 * time.Hour, Protect: plain, Unprotect: plain}, func(context.Context, string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if err = f.execute(ctx, os.Getenv("PANGOLIN_IDEMPOTENCY_CRASH_KEY"), `{}`, "saved", nil); err != nil {
			t.Fatal(err)
		}
		os.Exit(23) // process terminates after commit, before handing any result to caller
	}
	for _, name := range []string{"replay-success", "crash-after-commit-before-response"} {
		t.Run(name, func(t *testing.T) {
			f := newIdemFixture(t)
			key := rand.Text()
			var calls atomic.Int32
			if name == "crash-after-commit-before-response" {
				command := exec.Command(os.Args[0], "-test.run=^TestA07ResultReplay$")
				command.Env = append(os.Environ(), "PANGOLIN_IDEMPOTENCY_CRASH_HELPER=1", "PANGOLIN_IDEMPOTENCY_CRASH_DSN="+f.app.Config().ConnString(), "PANGOLIN_IDEMPOTENCY_CRASH_TENANT="+f.tenant, "PANGOLIN_IDEMPOTENCY_CRASH_KEY="+key)
				output, err := command.CombinedOutput()
				var exited *exec.ExitError
				if !errors.As(err, &exited) || exited.ExitCode() != 23 {
					t.Fatal("crash helper did not reach committed exit", err, string(output))
				}
				calls.Add(1)
			} else if err := f.execute(f.ctx, key, `{}`, "saved", func() { calls.Add(1) }); err != nil {
				t.Fatal(err)
			}
			if err := f.execute(f.ctx, key, `{}`, "different", func() { calls.Add(1) }); err != nil {
				t.Fatal(err)
			}
			result := f.query(t, f.ctx, key)
			if result.State != "succeeded" || string(result.Payload) != `{"id":"saved"}` || calls.Load() != 1 || f.count(t) != 1 {
				t.Fatal(result, calls.Load())
			}
		})
	}
}

func TestA61ResultQuery(t *testing.T) {
	f := newIdemFixture(t)
	key := rand.Text()
	if err := f.execute(f.ctx, key, `{}`, "saved", nil); err != nil {
		t.Fatal(err)
	}
	t.Run("authorized-success", func(t *testing.T) {
		if r := f.query(t, f.ctx, key); r.State != "succeeded" || string(r.Payload) != `{"id":"saved"}` {
			t.Fatal(r)
		}
	})
	t.Run("other-principal-denied", func(t *testing.T) {
		other := f.bind(t, scope.PrincipalRef{ID: "bob", Kind: "user"}, scope.ScopeRef{TenantID: f.tenant, Kind: "tenant", ID: f.tenant})
		if r := f.query(t, other, key); r.State != "unknown" || len(r.Payload) != 0 {
			t.Fatal(r)
		}
	})
	t.Run("pending-not-failed", func(t *testing.T) {
		pending := rand.Text()
		entered, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() { done <- f.execute(f.ctx, pending, `{}`, "pending", func() { close(entered); <-release }) }()
		<-entered
		result := f.query(t, f.ctx, pending)
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if result.State != "unknown" || len(result.Payload) != 0 {
			t.Fatal(result)
		}
	})
	t.Run("retention-boundary", func(t *testing.T) {
		boundary := rand.Text()
		if err := f.execute(f.ctx, boundary, `{}`, "boundary", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(`UPDATE pangolin_idempotency.results SET expires_at='2000-01-01' WHERE key=$1`, boundary); err != nil {
			t.Fatal(err)
		}
		if r := f.query(t, f.ctx, boundary); r.State != "expired" || len(r.Payload) != 0 {
			t.Fatal(r)
		}
		if err := f.execute(f.ctx, boundary, `{}`, "reexecuted", nil); !errors.Is(err, idempotency.ErrExpired) {
			t.Fatal(err)
		}
	})
	t.Run("expired-guarantee", func(t *testing.T) {
		expired := rand.Text()
		if err := f.execute(f.ctx, expired, `{}`, "expired-old", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(`UPDATE pangolin_idempotency.results SET expires_at='2000-01-01' WHERE key=$1`, expired); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(`DELETE FROM pangolin_idempotency.results WHERE expires_at<=now() AND key=$1`, expired); err != nil {
			t.Fatal(err)
		}
		if r := f.query(t, f.ctx, expired); r.State != "unknown" {
			t.Fatal(r)
		}
		if err := f.execute(f.ctx, expired, `{}`, "expired-new", nil); err != nil {
			t.Fatal(err)
		}
	})
}

func TestA81ScopeKeys(t *testing.T) {
	f := newIdemFixture(t)
	principal := scope.PrincipalRef{ID: "alice", Kind: "user"}
	refA := scope.ScopeRef{TenantID: f.tenant, Kind: "org.organization", ID: "org-a"}
	refB := scope.ScopeRef{TenantID: f.tenant, Kind: "org.organization", ID: "org-b"}
	a, b := f.bind(t, principal, refA), f.bind(t, principal, refB)
	key := rand.Text()
	var calls atomic.Int32
	t.Run("scope-specific-key", func(t *testing.T) {
		for _, ctx := range []context.Context{a, b} {
			if err := f.execute(ctx, key, `{}`, "same", func() { calls.Add(1) }); err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != 2 || f.count(t) != 2 {
			t.Fatal(calls.Load())
		}
	})
	t.Run("replay-same-scope", func(t *testing.T) {
		for _, ctx := range []context.Context{a, b} {
			if err := f.execute(ctx, key, `{}`, "no-repeat", func() { calls.Add(1) }); err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != 2 {
			t.Fatal(calls.Load())
		}
	})
	t.Run("cross-scope-result-denied", func(t *testing.T) {
		onlyA := rand.Text()
		if err := f.execute(a, onlyA, `{}`, "a-only", nil); err != nil {
			t.Fatal(err)
		}
		if r := f.query(t, b, onlyA); r.State != "unknown" || len(r.Payload) != 0 {
			t.Fatal(r)
		}
	})
	t.Run("revoked-replay-denied", func(t *testing.T) {
		f.authority.revoke(principal, refA)
		if err := f.execute(a, key, `{}`, "revoked", nil); !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
		err := f.m.Within(a, uow.Options{}, func(c context.Context) error { _, err := f.store.Query(c, "books.create", key); return err })
		if !errors.Is(err, scope.ErrDenied) {
			t.Fatal(err)
		}
	})
	t.Run("committed-effect-preserved", func(t *testing.T) {
		if f.count(t) != 3 {
			t.Fatal(f.count(t))
		}
		if r := f.query(t, b, key); r.State != "succeeded" {
			t.Fatal(r)
		}
	})
}

type idemCommitFaultResource struct {
	*postgres.Resource
	delivered bool
}

func (r idemCommitFaultResource) Begin(ctx context.Context, options uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	child, tx, err := r.Resource.Begin(ctx, options)
	if err != nil {
		return nil, nil, err
	}
	return child, idemCommitFault{Transaction: tx, delivered: r.delivered}, nil
}

type idemCommitFault struct {
	uow.Transaction
	delivered bool
}

func (t idemCommitFault) Commit(ctx context.Context) error {
	if t.delivered {
		if err := t.Transaction.Commit(ctx); err != nil {
			return err
		}
	}
	return errors.New("lost commit acknowledgement")
}
func TestIdempotencyUnknownReconciliation(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		name := "before-delivery"
		if delivered {
			name = "after-commit"
		}
		t.Run(name, func(t *testing.T) {
			f := newIdemFixture(t)
			fault := manager(t, idemCommitFaultResource{Resource: &postgres.Resource{Pool: f.app}, delivered: delivered})
			key := rand.Text()
			var calls atomic.Int32
			err := fault.Within(f.ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				_, _, err := f.store.Execute(c, "books.create", key, []byte(`{}`), func(work context.Context) ([]byte, error) {
					calls.Add(1)
					return []byte(`{"id":"unknown"}`), f.effect(work, "unknown")
				})
				return err
			})
			var failure uow.Error
			if !errors.Is(err, uow.ErrCommitUnknown) || !errors.As(err, &failure) || failure.Outcome() != uow.Unknown || calls.Load() != 1 {
				t.Fatal(err, calls.Load())
			}
			result := f.query(t, f.ctx, key)
			if delivered {
				if result.State != "succeeded" || f.count(t) != 1 {
					t.Fatal(result)
				}
			} else {
				if result.State != "unknown" || f.count(t) != 0 {
					t.Fatal(result)
				}
			}
			if err = f.execute(f.ctx, key, `{}`, "recovery", func() { calls.Add(1) }); err != nil {
				t.Fatal(err)
			}
			want := int32(2)
			if delivered {
				want = 1
			}
			if calls.Load() != want || f.count(t) != 1 {
				t.Fatal(calls.Load(), f.count(t))
			}
		})
	}
}

func TestIdempotencyResultFailures(t *testing.T) {
	f := newIdemFixture(t)
	plain := func(raw []byte) ([]byte, error) { return raw, nil }
	configure := func(t *testing.T, max int, protect, unprotect func([]byte) ([]byte, error)) {
		original := f.store
		t.Cleanup(func() { f.store = original })
		var err error
		f.store, err = idempotency.New(idempotency.Config{MaxKeyBytes: 128, MaxBodyBytes: max, RetryWindow: time.Hour, Retention: 24 * time.Hour, Protect: protect, Unprotect: unprotect}, func(context.Context, string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Run("required-result-protection-failure-rolls-back", func(t *testing.T) {
		failure := errors.New("result protector unavailable")
		configure(t, 1024, func([]byte) ([]byte, error) { return nil, failure }, plain)
		key := rand.Text()
		if err := f.execute(f.ctx, key, `{}`, "protected", nil); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if f.count(t) != 0 || f.query(t, f.ctx, key).State != "unknown" {
			t.Fatal("failed protection committed command")
		}
	})
	t.Run("oversized-result-rolls-back", func(t *testing.T) {
		configure(t, 4, plain, plain)
		key := rand.Text()
		if err := f.execute(f.ctx, key, `{}`, "too-large", nil); !errors.Is(err, uow.ErrOptionsInvalid) {
			t.Fatal(err)
		}
		if f.count(t) != 0 || f.query(t, f.ctx, key).State != "unknown" {
			t.Fatal("oversized result committed command")
		}
	})
	t.Run("unprotect-failure-preserves-success", func(t *testing.T) {
		key := rand.Text()
		if err := f.execute(f.ctx, key, `{}`, "unprotect", nil); err != nil {
			t.Fatal(err)
		}
		failure := errors.New("result decryptor unavailable")
		configure(t, 1024, plain, func([]byte) ([]byte, error) { return nil, failure })
		if err := f.execute(f.ctx, key, `{}`, "must-not-repeat", nil); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		err := f.m.Within(f.ctx, uow.Options{}, func(c context.Context) error { _, err := f.store.Query(c, "books.create", key); return err })
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if f.count(t) != 1 {
			t.Fatal("decryptor failure changed committed business effect")
		}
	})
	t.Run("incomplete-corrupt-record-is-not-success", func(t *testing.T) {
		key := rand.Text()
		hash, err := idempotency.CanonicalFingerprint([]byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.db.Exec(`INSERT INTO pangolin_idempotency.results(tenant_id,scope_kind,scope_id,principal_kind,principal_id,operation,key,fingerprint,expires_at) VALUES($1,'tenant',$1,'user','alice','books.create',$2,$3,now()+interval '1 hour')`, f.tenant, key, hash)
		if err != nil {
			t.Fatal(err)
		}
		if r := f.query(t, f.ctx, key); r.State != "in_progress" || len(r.Payload) != 0 {
			t.Fatal(r)
		}
		if err = f.execute(f.ctx, key, `{}`, "must-not-run", nil); !errors.Is(err, idempotency.ErrInProgress) {
			t.Fatal(err)
		}
	})
}
