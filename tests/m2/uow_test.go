//go:build integration

package m2

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/examples/bookstore/domain"
	"github.com/pangolin-framework/pangolin/examples/bookstore/infrastructure"
	"github.com/pangolin-framework/pangolin/persistence/postgres"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	transport "github.com/pangolin-framework/pangolin/transport/http"
	"github.com/jackc/pgx/v5/pgxpool"
)

func pool(t *testing.T, key string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(key)
	if dsn == "" {
		t.Fatalf("required %s missing", key)
	}
	p, e := pgxpool.New(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	if e = p.Ping(context.Background()); e != nil {
		t.Fatal(e)
	}
	return p
}
func binding(t *testing.T) context.Context {
	t.Helper()
	r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	ctx, e := r.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if e != nil {
		t.Fatal(e)
	}
	return ctx
}
func manager(t *testing.T, p *pgxpool.Pool) uow.UnitOfWorkManager {
	t.Helper()
	m, e := uow.NewManager(&postgres.Resource{Pool: p}, uow.DefaultOptions{DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second, HookTimeout: 20 * time.Millisecond, MaxRequiresNewDepth: 2})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func insert(ctx context.Context, id string) error {
	e, err := postgres.Current(ctx, true)
	if err != nil {
		return err
	}
	s, _ := scope.Current(ctx)
	_, err = e.Exec(ctx, "INSERT INTO pangolin_m2.records(tenant_id,id,value) VALUES($1,$2,1)", s.Ref.TenantID, id)
	if err != nil {
		return err
	}
	_, err = e.Exec(ctx, "INSERT INTO pangolin_m2.events(tenant_id,id) VALUES($1,$2)", s.Ref.TenantID, id)
	return err
}
func count(t *testing.T, p *pgxpool.Pool, id string) int {
	t.Helper()
	var n int
	if e := p.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM pangolin_m2.records WHERE id=$1)+(SELECT count(*) FROM pangolin_m2.events WHERE id=$1)", id).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func setup(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	_, e := p.Exec(context.Background(), `CREATE SCHEMA IF NOT EXISTS pangolin_m2 AUTHORIZATION pangolin_owner;
SET ROLE pangolin_owner;
CREATE TABLE IF NOT EXISTS pangolin_m2.records(tenant_id text NOT NULL,id text NOT NULL,value int NOT NULL,PRIMARY KEY(tenant_id,id));
CREATE TABLE IF NOT EXISTS pangolin_m2.events(tenant_id text NOT NULL,id text NOT NULL,PRIMARY KEY(tenant_id,id));
CREATE TABLE IF NOT EXISTS pangolin_m2.results(tenant_id text NOT NULL,key text NOT NULL,operation_id text NOT NULL,PRIMARY KEY(tenant_id,key));
GRANT USAGE ON SCHEMA pangolin_m2 TO pangolin_app;
GRANT SELECT,INSERT,UPDATE ON pangolin_m2.records,pangolin_m2.events,pangolin_m2.results TO pangolin_app;
RESET ROLE;`)
	if e != nil {
		t.Fatal(e)
	}
}

func TestM2Transactions(t *testing.T) {
	admin := pool(t, "PANGOLIN_M1_ADMIN_DSN")
	app := pool(t, "PANGOLIN_M1_APP_DSN")
	setup(t, admin)
	m := manager(t, app)
	ctx := binding(t)
	cause := errors.New("business failure")
	t.Run("A03-required-rollback", func(t *testing.T) {
		a, b := rand.Text(), rand.Text()
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(p context.Context) error {
			if e := insert(p, a); e != nil {
				return e
			}
			if e := m.Within(p, uow.Options{}, func(c context.Context) error { return insert(c, b) }); e != nil {
				return e
			}
			if count(t, admin, a) != 0 || count(t, admin, b) != 0 {
				t.Fatal("uncommitted visible")
			}
			return cause
		})
		if !errors.Is(err, cause) || count(t, admin, a) != 0 || count(t, admin, b) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("A05-explicit-rollback", func(t *testing.T) {
		id := rand.Text()
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := insert(c, id); e != nil {
				return e
			}
			s, _ := m.Current(c)
			return s.SetRollbackOnly(cause)
		})
		var f uow.Error
		if !errors.As(err, &f) || f.Outcome() != uow.RolledBack || !errors.Is(err, cause) || count(t, admin, id) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("A23-independent-suspended-resumed", func(t *testing.T) {
		var pID, cID uint64
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(p context.Context) error {
			pe, e := postgres.Current(p, false)
			if e != nil {
				return e
			}
			if e = pe.QueryRow(p, "SELECT txid_current()").Scan(&pID); e != nil {
				return e
			}
			e = m.Within(p, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(c context.Context) error {
				var during uint64
				if e := pe.QueryRow(p, "SELECT txid_current()").Scan(&during); e != nil || during != pID {
					t.Fatalf("parent unusable during RequiresNew: tx=%d want=%d err=%v", during, pID, e)
				}
				ce, e := postgres.Current(c, false)
				if e != nil {
					return e
				}
				return ce.QueryRow(c, "SELECT txid_current()").Scan(&cID)
			})
			if e != nil {
				return e
			}
			var resumed uint64
			if e = pe.QueryRow(p, "SELECT txid_current()").Scan(&resumed); e != nil {
				return e
			}
			if pID != resumed || pID == cID {
				t.Fatal(pID, cID, resumed)
			}
			t.Logf("parent_tx=%d child_tx=%d resumed_tx=%d", pID, cID, resumed)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	for _, mode := range []string{"A24-child-commit-parent-rollback", "A25-caught-child", "A25-propagated-child"} {
		t.Run(mode, func(t *testing.T) {
			a, b := rand.Text(), rand.Text()
			err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(p context.Context) error {
				if e := insert(p, a); e != nil {
					return e
				}
				e := m.Within(p, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew}, func(c context.Context) error {
					if e := insert(c, b); e != nil {
						return e
					}
					if strings.Contains(mode, "A25") {
						return cause
					}
					return nil
				})
				if strings.Contains(mode, "parent-rollback") {
					return cause
				}
				if strings.Contains(mode, "propagated") {
					return e
				}
				return nil
			})
			wantA, wantB := 0, 0
			if strings.Contains(mode, "caught") {
				wantA = 2
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("missing failure")
			}
			if strings.Contains(mode, "A24") {
				wantB = 2
			}
			if count(t, admin, a) != wantA || count(t, admin, b) != wantB {
				t.Fatal("wrong independent outcomes")
			}
		})
	}
	t.Run("A26-pool-exhaustion", func(t *testing.T) {
		cfg, e := pgxpool.ParseConfig(os.Getenv("PANGOLIN_M1_APP_DSN"))
		if e != nil {
			t.Fatal(e)
		}
		cfg.MaxConns = 1
		p, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer p.Close()
		one := manager(t, p)
		err := one.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			e := one.Within(c, uow.Options{Propagation: uow.RequiresNew, Timeout: 30 * time.Millisecond}, func(context.Context) error { t.Fatal("acquire should time out"); return nil })
			if !errors.Is(e, context.DeadlineExceeded) {
				t.Fatal(e)
			}
			return nil
		})
		if err != nil || p.Stat().AcquiredConns() != 0 {
			t.Fatal(err, p.Stat())
		}
	})
	t.Run("A26-parent-lock-wait", func(t *testing.T) {
		id := rand.Text()
		if _, e := admin.Exec(ctx, "INSERT INTO pangolin_m2.records VALUES('tenant-a',$1,1)", id); e != nil {
			t.Fatal(e)
		}
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(p context.Context) error {
			pe, _ := postgres.Current(p, true)
			if _, e := pe.Exec(p, "UPDATE pangolin_m2.records SET value=2 WHERE id=$1", id); e != nil {
				return e
			}
			e := m.Within(p, uow.Options{Mode: uow.ReadWrite, Propagation: uow.RequiresNew, Timeout: 30 * time.Millisecond}, func(c context.Context) error {
				ce, _ := postgres.Current(c, true)
				_, e := ce.Exec(c, "UPDATE pangolin_m2.records SET value=3 WHERE id=$1", id)
				return e
			})
			if !errors.Is(e, context.DeadlineExceeded) {
				t.Fatal(e)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("A28-sql-deadline", func(t *testing.T) {
		err := m.Within(ctx, uow.Options{Timeout: 20 * time.Millisecond}, func(c context.Context) error {
			e, _ := postgres.Current(c, false)
			return e.QueryRow(c, "SELECT pg_sleep(1)").Scan(new(any))
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
	t.Run("A29-cancel-before-commit", func(t *testing.T) {
		id := rand.Text()
		c, cancel := context.WithCancel(ctx)
		err := m.Within(c, uow.Options{Mode: uow.ReadWrite}, func(w context.Context) error {
			if e := insert(w, id); e != nil {
				return e
			}
			cancel()
			return nil
		})
		if !errors.Is(err, context.Canceled) || count(t, admin, id) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("A30-open-rows-busy", func(t *testing.T) {
		id := rand.Text()
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := insert(c, id); e != nil {
				return e
			}
			e, _ := postgres.Current(c, false)
			rows, er := e.Query(c, "SELECT generate_series(1,3)")
			if er != nil {
				return er
			}
			other := make(chan error, 1)
			go func() { other <- e.QueryRow(c, "SELECT 1").Scan(new(int)) }()
			if er = <-other; !errors.Is(er, uow.ErrBusy) {
				t.Fatal(er)
			}
			for rows.Next() {
				var n int
				if er = rows.Scan(&n); er != nil {
					t.Fatal(er)
				}
			}
			rows.Close()
			return rows.Err()
		})
		if !errors.Is(err, uow.ErrRollbackOnly) || count(t, admin, id) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("A30-delayed-rows-error", func(t *testing.T) {
		id := rand.Text()
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := insert(c, id); e != nil {
				return e
			}
			e, _ := postgres.Current(c, false)
			rows, er := e.Query(c, "SELECT 1/(n-2) FROM generate_series(1,3) n")
			if er != nil {
				return er
			}
			for rows.Next() {
				var n int
				_ = rows.Scan(&n)
			}
			rows.Close()
			if rows.Err() == nil {
				t.Fatal("missing delayed error")
			}
			return nil
		})
		if err == nil || count(t, admin, id) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("A31-savechanges-rollback", func(t *testing.T) {
		id := rand.Text()
		completed := false
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := insert(c, id); e != nil {
				return e
			}
			s, _ := m.Current(c)
			_ = s.OnCompleted(func(context.Context, uow.Completion) error { completed = true; return nil })
			if e := s.SaveChanges(c); e != nil {
				return e
			}
			if completed || count(t, admin, id) != 0 {
				t.Fatal("flush completed early")
			}
			return cause
		})
		if !errors.Is(err, cause) || completed || count(t, admin, id) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("A35-callback-panic", func(t *testing.T) {
		id := rand.Text()
		func() {
			defer func() {
				if recover() != cause {
					t.Fatal("lost panic")
				}
			}()
			_ = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				if e := insert(c, id); e != nil {
					return e
				}
				panic(cause)
			})
		}()
		if count(t, admin, id) != 0 || app.Stat().AcquiredConns() != 0 {
			t.Fatal("panic leak")
		}
	})
	t.Run("A43-readonly-child", func(t *testing.T) {
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(p context.Context) error {
			_ = m.Within(p, uow.Options{Mode: uow.ReadOnly}, func(c context.Context) error {
				e, er := postgres.Current(c, false)
				if er != nil {
					return er
				}
				if er = e.QueryRow(c, "SELECT 1").Scan(new(int)); er != nil {
					return er
				}
				return insert(c, rand.Text())
			})
			return nil
		})
		if !errors.Is(err, uow.ErrReadOnly) || !errors.Is(err, uow.ErrRollbackOnly) {
			t.Fatal(err)
		}
	})
	t.Run("A41-resource-mismatch", func(t *testing.T) {
		other, e := uow.NewManager(&postgres.Resource{Pool: app, Key: "second-db"}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
		if e != nil {
			t.Fatal(e)
		}
		id := rand.Text()
		err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(p context.Context) error {
			if e := insert(p, id); e != nil {
				return e
			}
			_ = other.Within(p, uow.Options{}, func(context.Context) error { t.Fatal("second DB callback"); return nil })
			return nil
		})
		if !errors.Is(err, uow.ErrResourceMismatch) || count(t, admin, id) != 0 {
			t.Fatal(err)
		}
	})
}

func TestM2CommitUnknown(t *testing.T) {
	admin := pool(t, "PANGOLIN_M1_ADMIN_DSN")
	setup(t, admin)
	for _, after := range []bool{false, true} {
		name := "before-delivery"
		if after {
			name = "after-db-commit"
		}
		t.Run(name, func(t *testing.T) {
			dsn, e := url.Parse(os.Getenv("PANGOLIN_M1_APP_DSN"))
			if e != nil {
				t.Fatal(e)
			}
			address, stop := commitProxy(t, dsn.Host, after)
			defer stop()
			dsn.Host = address
			p, e := pgxpool.New(context.Background(), dsn.String())
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			m := manager(t, p)
			id := rand.Text()
			calls := 0
			var op string
			err := m.Within(binding(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				calls++
				s, _ := m.Current(c)
				op = s.ID()
				if e := insert(c, id); e != nil {
					return e
				}
				ex, _ := postgres.Current(c, true)
				_, e := ex.Exec(c, "INSERT INTO pangolin_m2.results VALUES('tenant-a',$1,$2)", id, op)
				return e
			})
			var f uow.Error
			if !errors.Is(err, uow.ErrCommitUnknown) || !errors.As(err, &f) || f.Outcome() != uow.Unknown || calls != 1 || f.OperationID() != op {
				t.Fatal(err, calls)
			}
			var stored string
			e = admin.QueryRow(context.Background(), "SELECT operation_id FROM pangolin_m2.results WHERE key=$1", id).Scan(&stored)
			if after {
				if e != nil || stored != op || count(t, admin, id) != 2 {
					t.Fatal(e, stored, op)
				}
			} else if e == nil || count(t, admin, id) != 0 {
				t.Fatal("COMMIT delivered before cut")
			}
			t.Logf("fault=%s operation=%s outcome=unknown reconciled=%v", name, op, after)
			// The same wire fault through the public HTTP adapter must never send 2xx.
			httpID := rand.Text()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/write", nil).WithContext(binding(t))
			transport.Command(m, func(c context.Context) (any, error) { return map[string]string{"id": httpID}, insert(c, httpID) }).ServeHTTP(recorder, request)
			if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), `"outcome":"unknown"`) {
				t.Fatal(recorder.Code, recorder.Body.String())
			}
			want := 0
			if after {
				want = 2
			}
			if count(t, admin, httpID) != want {
				t.Fatal("HTTP response disagrees with independently reconciled database")
			}
		})
	}
}

func TestA10VersionConflict(t *testing.T) {
	admin := pool(t, "PANGOLIN_M1_ADMIN_DSN")
	app := pool(t, "PANGOLIN_M1_APP_DSN")
	ctx := binding(t)
	m := manager(t, app)
	repo := infrastructure.Repository{}
	id := rand.Text()
	if err := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		return repo.Insert(c, domain.Book{ID: id, Title: "version one", Version: 1})
	}); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"first", "second"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
				b, e := repo.Get(c, id)
				if e != nil {
					return e
				}
				ready <- struct{}{}
				<-start
				b.Title = title
				return repo.Update(c, b, b.Version)
			})
		}()
	}
	<-ready
	<-ready
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, domain.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	var version int
	if e := admin.QueryRow(ctx, "SELECT version FROM pangolin.books WHERE id=$1", id).Scan(&version); e != nil || success != 1 || conflict != 1 || version != 2 {
		t.Fatal(e, success, conflict, version)
	}
	if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return repo.Delete(c, id, 1) }); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	if e := m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return repo.Delete(c, id, 2) }); e != nil {
		t.Fatal(e)
	}
	if e := m.Within(ctx, uow.Options{}, func(c context.Context) error { _, e := repo.Get(c, id); return e }); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
}
