package uow

import (
	"context"
	"errors"
	"testing"
	"time"
)

type faultResource struct {
	tx    *faultTransaction
	begin func(context.Context) error
}

func (r faultResource) Begin(ctx context.Context, _ EffectiveOptions) (context.Context, Transaction, error) {
	if r.begin != nil {
		if e := r.begin(ctx); e != nil {
			return nil, nil, e
		}
	}
	return ctx, r.tx, nil
}

type faultTransaction struct {
	testTx
	flush    func(context.Context) error
	before   func(context.Context) error
	prepare  func(context.Context) error
	rollback func(context.Context) error
	commit   func(context.Context) error
	known    bool
	releases int
}

func (t *faultTransaction) Flush(ctx context.Context) error {
	if t.flush != nil {
		return t.flush(ctx)
	}
	return nil
}
func (t *faultTransaction) BeforeCommit(ctx context.Context) error {
	if t.before != nil {
		return t.before(ctx)
	}
	return nil
}
func (t *faultTransaction) Prepare(ctx context.Context) error {
	if t.prepare != nil {
		return t.prepare(ctx)
	}
	return nil
}
func (t *faultTransaction) Rollback(ctx context.Context) error {
	t.rollbacks++
	if t.rollback != nil {
		return t.rollback(ctx)
	}
	return t.rollbackErr
}
func (t *faultTransaction) Commit(ctx context.Context) error {
	t.commits++
	if t.commit != nil {
		return t.commit(ctx)
	}
	return t.commitErr
}
func (t *faultTransaction) CommitFailureOutcome(error) Outcome {
	if t.known {
		return RolledBack
	}
	return Unknown
}
func (t *faultTransaction) Release(context.Context) error { t.releases++; return nil }

func TestM2DeadlinesAndFlush(t *testing.T) {
	for _, phase := range []string{"begin", "flush", "handler", "prepare", "commit", "shorter-child", "longer-child"} {
		t.Run(phase, func(t *testing.T) {
			tx := &faultTransaction{}
			r := faultResource{tx: tx}
			block := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
			switch phase {
			case "begin":
				r.begin = block
			case "flush":
				tx.flush = block
			case "handler":
				tx.before = block
			case "prepare":
				tx.prepare = block
			case "commit":
				tx.commit = block
			}
			m, _ := NewManager(r, defaults())
			ctx, cancel := context.WithTimeout(testContext(t), 20*time.Millisecond)
			defer cancel()
			err := m.Within(ctx, Options{Mode: ReadWrite, Timeout: time.Second}, func(c context.Context) error {
				if phase == "shorter-child" || phase == "longer-child" {
					timeout := time.Second
					if phase == "shorter-child" {
						timeout = time.Millisecond
					}
					return m.Within(c, Options{Timeout: timeout}, func(child context.Context) error {
						parentDeadline, _ := c.Deadline()
						childDeadline, _ := child.Deadline()
						if childDeadline.After(parentDeadline) {
							t.Fatal("extended parent deadline")
						}
						<-child.Done()
						return child.Err()
					})
				}
				return nil
			})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			var f Error
			if !errors.As(err, &f) {
				t.Fatal(err)
			}
			want := RolledBack
			if phase == "begin" {
				want = NotStarted
			}
			if phase == "commit" {
				want = Unknown
			}
			if f.Outcome() != want {
				t.Fatal(f.Outcome(), want)
			}
		})
	}
	t.Run("savechanges-does-not-drain", func(t *testing.T) {
		tx := &faultTransaction{}
		flushes, handlers, completed := 0, 0, 0
		tx.flush = func(ctx context.Context) error { flushes++; return Check(ctx, true) }
		tx.before = func(ctx context.Context) error { handlers++; return Check(ctx, true) }
		m, _ := NewManager(faultResource{tx: tx}, defaults())
		e := m.Within(testContext(t), Options{Mode: ReadWrite}, func(c context.Context) error {
			s, _ := m.Current(c)
			_ = s.OnCompleted(func(context.Context, Completion) error { completed++; return nil })
			if e := s.SaveChanges(c); e != nil {
				return e
			}
			if flushes != 1 || handlers != 0 || completed != 0 || tx.commits != 0 {
				t.Fatal("SaveChanges completed root")
			}
			return nil
		})
		if e != nil || flushes != 2 || handlers != 1 || completed != 1 {
			t.Fatal(e, flushes, handlers, completed)
		}
	})
}

func TestM2FailureObservers(t *testing.T) {
	for _, mode := range []string{"success", "callback", "panic", "handler-panic", "unknown", "rollback-failure", "serialization", "deadlock", "cancel-after-commit", "prepare-failure", "flush-failure"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New(mode)
			tx := &faultTransaction{}
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			cleanup := false
			failed, disposed, calls := 0, 0, 0
			tx.rollback = func(c context.Context) error {
				if c.Err() != nil {
					t.Fatal("cleanup inherited cancel")
				}
				if _, ok := c.Deadline(); !ok {
					t.Fatal("cleanup unbounded")
				}
				cleanup = true
				if mode == "rollback-failure" {
					return errors.New("cleanup failure")
				}
				return nil
			}
			if mode == "unknown" || mode == "serialization" || mode == "deadlock" {
				tx.commitErr = cause
				tx.known = mode != "unknown"
			}
			if mode == "handler-panic" {
				tx.before = func(context.Context) error { panic(cause) }
			}
			if mode == "cancel-after-commit" {
				tx.commit = func(context.Context) error { cancel(); return nil }
			}
			if mode == "prepare-failure" {
				tx.prepare = func(context.Context) error { return cause }
			}
			if mode == "flush-failure" {
				tx.flush = func(context.Context) error { return cause }
			}
			m, _ := NewManager(faultResource{tx: tx}, defaults())
			var observed Completion
			invoke := func() error {
				return m.Within(ctx, Options{Mode: ReadWrite}, func(c context.Context) error {
					calls++
					s, _ := m.Current(c)
					_ = s.OnFailed(func(h context.Context, completion Completion) error {
						failed++
						observed = completion
						if _, ok := m.Current(h); ok {
							t.Fatal("observer retained executor")
						}
						return errors.New("observer")
					})
					_ = s.OnDisposed(func(context.Context, Completion) error { disposed++; return errors.New("observer") })
					switch mode {
					case "callback", "rollback-failure":
						cancel()
						return cause
					case "panic":
						panic(cause)
					}
					return nil
				})
			}
			var err error
			if mode == "panic" || mode == "handler-panic" {
				func() {
					defer func() {
						if recover() != cause {
							t.Fatal("panic changed")
						}
					}()
					err = invoke()
				}()
			} else {
				err = invoke()
			}
			success := mode == "success" || mode == "cancel-after-commit"
			if success {
				if err != nil || cleanup || failed != 0 || tx.commits != 1 || tx.releases != 1 {
					t.Fatal(err, tx)
				}
			} else {
				if failed != 1 || !cleanup {
					t.Fatal(failed, cleanup)
				}
				if mode == "unknown" || mode == "rollback-failure" {
					if observed.Outcome != Unknown {
						t.Fatal(observed)
					}
				} else if observed.Outcome != RolledBack {
					t.Fatal(observed)
				}
				if mode != "panic" && mode != "handler-panic" && !errors.Is(err, cause) {
					t.Fatal(err)
				}
			}
			if disposed != 1 || calls != 1 {
				t.Fatal(disposed, calls)
			}
		})
	}
}
