//go:build integration

package m2

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
)

type stagedResource struct {
	*postgres.Resource
	handler func(context.Context) error
}

func (r stagedResource) Begin(ctx context.Context, o uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	c, tx, e := r.Resource.Begin(ctx, o)
	if e != nil {
		return nil, nil, e
	}
	return c, stagedTransaction{Transaction: tx, handler: r.handler}, nil
}

type stagedTransaction struct {
	uow.Transaction
	handler func(context.Context) error
}

func (t stagedTransaction) BeforeCommit(ctx context.Context) error { return t.handler(ctx) }
func (t stagedTransaction) Prepare(ctx context.Context) error {
	return t.Transaction.(interface{ Prepare(context.Context) error }).Prepare(ctx)
}
func (t stagedTransaction) Release(ctx context.Context) error {
	return t.Transaction.(interface{ Release(context.Context) error }).Release(ctx)
}
func (t stagedTransaction) CommitFailureOutcome(e error) uow.Outcome {
	return t.Transaction.(interface{ CommitFailureOutcome(error) uow.Outcome }).CommitFailureOutcome(e)
}

func TestM2BeforeCommitFaults(t *testing.T) {
	admin := pool(t, "PANGOLIN_M1_ADMIN_DSN")
	app := pool(t, "PANGOLIN_M1_APP_DSN")
	setup(t, admin)
	panicValue := errors.New("handler panic")
	for _, mode := range []string{"panic", "deadline", "serialization", "deadlock", "explicit-upper-retry"} {
		t.Run(mode, func(t *testing.T) {
			id := rand.Text()
			calls, stages := 0, 0
			handler := func(ctx context.Context) error {
				stages++
				switch mode {
				case "panic":
					panic(panicValue)
				case "deadline":
					<-ctx.Done()
					return ctx.Err()
				default:
					if mode == "explicit-upper-retry" && stages == 2 {
						return nil
					}
					code := "40001"
					if mode == "deadlock" {
						code = "40P01"
					}
					ex, e := postgres.Current(ctx, true)
					if e != nil {
						return e
					}
					_, e = ex.Exec(ctx, "DO $$ BEGIN RAISE EXCEPTION 'injected failure' USING ERRCODE = '"+code+"'; END $$")
					return e
				}
			}
			m, e := uow.NewManager(stagedResource{&postgres.Resource{Pool: app}, handler}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
			if e != nil {
				t.Fatal(e)
			}
			invoke := func() error {
				return m.Within(binding(t), uow.Options{Mode: uow.ReadWrite, Timeout: 30 * time.Millisecond}, func(ctx context.Context) error { calls++; return insert(ctx, id) })
			}
			if mode == "panic" {
				func() {
					defer func() {
						if recover() != panicValue {
							t.Fatal("handler panic changed")
						}
					}()
					_ = invoke()
				}()
			} else {
				e = invoke()
				var f uow.Error
				if !errors.As(e, &f) || f.Outcome() != uow.RolledBack {
					t.Fatal(e)
				}
				if mode == "deadline" && !errors.Is(e, context.DeadlineExceeded) {
					t.Fatal(e)
				}
			}
			if calls != 1 || count(t, admin, id) != 0 || app.Stat().AcquiredConns() != 0 {
				t.Fatal("failed stage leaked or retried", calls)
			}
			if mode == "explicit-upper-retry" {
				if e = invoke(); e != nil || calls != 2 || count(t, admin, id) != 2 {
					t.Fatal(e, calls)
				}
			}
		})
	}
}
