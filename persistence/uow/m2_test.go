package uow

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type sequenceResource struct {
	transactions []*testTx
	options      []EffectiveOptions
	key          string
	rls          bool
}

func (r *sequenceResource) DatabaseKey() string       { return r.key }
func (r *sequenceResource) RequiresTransaction() bool { return r.rls }
func (r *sequenceResource) Begin(ctx context.Context, o EffectiveOptions) (context.Context, Transaction, error) {
	r.options = append(r.options, o)
	tx := &testTx{}
	r.transactions = append(r.transactions, tx)
	return ctx, tx, nil
}

func TestM2Options(t *testing.T) {
	for _, c := range []struct {
		name      string
		options   Options
		rls       bool
		mode      AccessMode
		tx        bool
		isolation Isolation
		invalid   bool
	}{
		{"zero", Options{}, false, ReadOnly, false, IsolationDefault, false},
		{"rls-read", Options{}, true, ReadOnly, true, ReadCommitted, false},
		{"command", Options{Mode: ReadWrite}, false, ReadWrite, true, ReadCommitted, false},
		{"repeatable", Options{Transaction: Transactional, Isolation: RepeatableRead}, false, ReadOnly, true, RepeatableRead, false},
		{"serializable", Options{Mode: ReadWrite, Isolation: Serializable}, false, ReadWrite, true, Serializable, false},
		{"nontransactional-write", Options{Mode: ReadWrite, Transaction: NonTransactional}, false, 0, false, 0, true},
		{"nontransactional-rls", Options{Transaction: NonTransactional}, true, 0, false, 0, true},
		{"nontransactional-isolation", Options{Transaction: NonTransactional, Isolation: ReadCommitted}, false, 0, false, 0, true},
		{"mode", Options{Mode: 99}, false, 0, false, 0, true},
		{"transaction", Options{Transaction: 99}, false, 0, false, 0, true},
		{"propagation", Options{Propagation: 99}, false, 0, false, 0, true},
		{"isolation", Options{Isolation: 99}, false, 0, false, 0, true},
		{"timeout", Options{Timeout: -1}, false, 0, false, 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &sequenceResource{key: "db-a", rls: c.rls}
			m, err := NewManager(r, defaults())
			if err != nil {
				t.Fatal(err)
			}
			called := false
			err = m.Within(testContext(t), c.options, func(ctx context.Context) error {
				called = true
				s, ok := m.Current(ctx)
				if !ok {
					t.Fatal("missing scope")
				}
				o := s.Options()
				if o.Mode != c.mode || o.Transactional != c.tx || o.Isolation != c.isolation || o.DatabaseKey != "db-a" {
					t.Fatalf("options %+v", o)
				}
				return nil
			})
			if c.invalid {
				if !errors.Is(err, ErrOptionsInvalid) || called || len(r.options) != 0 {
					t.Fatal(err, called, r.options)
				}
				return
			}
			if err != nil || !called {
				t.Fatal(err)
			}
			if !c.tx && r.transactions[0].commits != 0 {
				t.Fatal("nontransactional commit")
			}
		})
	}
}

func TestM2RequiredMatrix(t *testing.T) {
	for _, parent := range []Options{{Mode: ReadWrite}, {Mode: ReadOnly, Transaction: Transactional}, {Mode: ReadOnly, Transaction: NonTransactional}} {
		for _, child := range []Options{{}, {Mode: ReadOnly}, {Mode: ReadWrite}, {Transaction: Transactional}, {Transaction: NonTransactional}, {Isolation: Serializable}} {
			t.Run(string(rune('A'+parent.Mode))+"-"+reflect.TypeOf(child).Name()+time.Duration(child.Transaction*10+TransactionMode(child.Mode)).String()+time.Duration(child.Isolation).String(), func(t *testing.T) {
				r := &sequenceResource{key: "db"}
				m, _ := NewManager(r, defaults())
				var escaped context.Context
				conflict := parent.Mode == ReadOnly && child.Mode == ReadWrite || parent.Transaction == NonTransactional && child.Transaction == Transactional || parent.Transaction != NonTransactional && child.Transaction == NonTransactional || child.Isolation == Serializable
				err := m.Within(testContext(t), parent, func(ctx context.Context) error {
					s, _ := m.Current(ctx)
					e := m.Within(ctx, child, func(c context.Context) error {
						escaped = c
						cs, _ := m.Current(c)
						if cs.ID() != s.ID() {
							t.Fatal("different Required root")
						}
						return nil
					})
					if conflict {
						if !errors.Is(e, ErrOptionsConflict) && !(parent.Transaction == NonTransactional && child.Isolation != IsolationDefault && errors.Is(e, ErrOptionsInvalid)) {
							t.Fatal(e)
						}
					} else if e != nil {
						t.Fatal(e)
					}
					if escaped != nil {
						if _, ok := m.Current(escaped); ok {
							t.Fatal("child escaped")
						}
						if !errors.Is(Check(escaped, false), ErrClosed) {
							t.Fatal("closed child")
						}
					}
					if _, ok := m.Current(ctx); !ok {
						t.Fatal("parent ended")
					}
					return nil
				})
				if conflict {
					if !errors.Is(err, ErrRollbackOnly) || r.transactions[0].commits != 0 {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if len(r.options) != 1 {
					t.Fatal("Required began twice")
				}
			})
		}
	}
}

func TestM2RequiresNew(t *testing.T) {
	for _, mode := range []string{"parent-failure", "caught-child", "propagated-child", "depth", "resource"} {
		t.Run(mode, func(t *testing.T) {
			r := &sequenceResource{key: "db"}
			d := defaults()
			d.MaxRequiresNewDepth = 1
			m, _ := NewManager(r, d)
			cause := errors.New("business")
			err := m.Within(testContext(t), Options{Mode: ReadWrite}, func(parent context.Context) error {
				ps, _ := m.Current(parent)
				e := m.Within(parent, Options{Mode: ReadWrite, Propagation: RequiresNew}, func(child context.Context) error {
					cs, _ := m.Current(child)
					if ps.ID() == cs.ID() {
						t.Fatal("same root")
					}
					if _, ok := m.Current(parent); ok {
						t.Fatal("suspended current")
					}
					if !errors.Is(Check(parent, false), ErrSuspended) {
						t.Fatal(Check(parent, false))
					}
					if mode == "depth" {
						if e := m.Within(child, Options{Propagation: RequiresNew}, func(context.Context) error { t.Fatal("depth callback"); return nil }); !errors.Is(e, ErrDepthExceeded) {
							t.Fatal(e)
						}
					}
					if mode == "caught-child" || mode == "propagated-child" {
						return cause
					}
					return nil
				})
				if e := Check(parent, true); e != nil {
					t.Fatal("parent did not resume", e)
				}
				if mode == "resource" {
					other, _ := NewManager(&sequenceResource{key: "other"}, d)
					_ = other.Within(parent, Options{}, func(context.Context) error { t.Fatal("second resource"); return nil })
				}
				if mode == "parent-failure" {
					return cause
				}
				if mode == "propagated-child" {
					return e
				}
				return nil
			})
			if len(r.options) != 2 {
				t.Fatal(len(r.options))
			}
			parentCommit := mode == "caught-child" || mode == "depth"
			childCommit := mode != "caught-child" && mode != "propagated-child"
			if (r.transactions[0].commits == 1) != parentCommit || (r.transactions[1].commits == 1) != childCommit {
				t.Fatal(r.transactions)
			}
			if parentCommit != (err == nil) {
				t.Fatal(err)
			}
		})
	}
}

func TestM2HookIsolation(t *testing.T) {
	tx := &testTx{}
	d := defaults()
	d.HookTimeout = 5 * time.Millisecond
	m, _ := NewManager(testResource{tx: tx}, d)
	var order []int
	err := m.Within(testContext(t), Options{Mode: ReadWrite}, func(ctx context.Context) error {
		s, _ := m.Current(ctx)
		for i := 0; i < 4; i++ {
			_ = s.OnCompleted(func(h context.Context, c Completion) error {
				order = append(order, i)
				if c.Outcome != Committed {
					t.Fatal(c)
				}
				if _, ok := m.Current(h); ok {
					t.Fatal("hook retained UOW")
				}
				if i == 0 {
					return errors.New("observer")
				}
				if i == 1 {
					panic("observer panic")
				}
				if i == 2 {
					<-h.Done()
					return h.Err()
				}
				return nil
			})
		}
		return s.OnDisposed(func(context.Context, Completion) error { order = append(order, 4); return nil })
	})
	if err != nil || !reflect.DeepEqual(order, []int{0, 1, 2, 3, 4}) || tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatal(err, order, tx)
	}
}

func TestM2ViewGuards(t *testing.T) {
	tx := &testTx{}
	m, _ := NewManager(testResource{tx: tx}, defaults())
	ctx := testContext(t)
	var child context.Context
	var s Scope
	if err := m.Within(ctx, Options{Mode: ReadWrite}, func(root context.Context) error {
		s, _ = m.Current(root)
		if CheckResource(root, "second", false) == nil {
			t.Fatal("second database")
		}
		if s.SaveChanges(ctx) == nil {
			t.Fatal("wrong SaveChanges context")
		}
		return m.Within(root, Options{Mode: ReadOnly}, func(c context.Context) error { child = c; return nil })
	}); !errors.Is(err, ErrRollbackOnly) {
		t.Fatal(err)
	}
	if err := m.Within(child, Options{}, func(context.Context) error { t.Fatal("closed callback"); return nil }); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if s.SaveChanges(child) == nil {
		t.Fatal("ended flush")
	}
	if CheckResource(context.Background(), "db", false) == nil {
		t.Fatal("unscoped resource")
	}
	if Fail(context.Background(), nil) != nil || Fail(context.Background(), ErrBusy) != ErrBusy {
		t.Fatal("unscoped adapter failure")
	}
	for _, code := range []error{ErrOptionsInvalid, ErrOptionsConflict, ErrRequired, ErrReadOnly, ErrTransactionRequired, ErrTenantMismatch, ErrIdentityMismatch, ErrScopeMismatch, ErrResourceMismatch, ErrClosed, ErrSuspended, ErrBusy, ErrDepthExceeded, ErrRollbackOnly, ErrCommitUnknown} {
		e := fail(code, "op", Unknown, code)
		var classified Error
		if !errors.Is(e, code) || !errors.As(e, &classified) || classified.Code() != code.Error() || errorCode(code) != code {
			t.Fatal(e)
		}
	}
}

func TestM2NonTransactional(t *testing.T) {
	r := &sequenceResource{key: "no-rls"}
	m, _ := NewManager(r, defaults())
	ctx := testContext(t)
	var ended context.Context
	if err := m.Within(ctx, Options{Transaction: NonTransactional}, func(c context.Context) error {
		ended = c
		if e := Check(c, false); e != nil {
			return e
		}
		if e := Check(c, true); !errors.Is(e, ErrReadOnly) {
			t.Fatal(e)
		}
		return nil
	}); !errors.Is(err, ErrRollbackOnly) {
		t.Fatal(err)
	}
	if Check(ended, false) == nil || Check(ctx, true) == nil {
		t.Fatal("unscoped write")
	}
	if r.transactions[0].commits != 0 || r.transactions[0].rollbacks != 0 {
		t.Fatal("nontransactional transaction control")
	}
}

func TestM2RequestIsolation(t *testing.T) {
	r := &sequenceResource{key: "db", rls: true}
	m, _ := NewManager(r, defaults())
	first, second := testContext(t), testContext(t)
	if err := m.Within(first, Options{}, func(a context.Context) error {
		sa, _ := m.Current(a)
		return m.Within(second, Options{}, func(b context.Context) error {
			sb, _ := m.Current(b)
			if sa.ID() == sb.ID() {
				t.Fatal("requests shared scope")
			}
			if _, ok := m.Current(a); !ok {
				t.Fatal("unrelated request suspended")
			}
			return nil
		})
	}); err != nil || len(r.options) != 2 {
		t.Fatal(err)
	}
}

func TestM2HookCollections(t *testing.T) {
	r := &sequenceResource{key: "db", rls: true}
	m, _ := NewManager(r, defaults())
	var order []int
	if e := m.Within(testContext(t), Options{Mode: ReadWrite}, func(p context.Context) error {
		s, _ := m.Current(p)
		_ = s.OnCompleted(func(context.Context, Completion) error { order = append(order, 0); return nil })
		if e := m.Within(p, Options{}, func(c context.Context) error {
			s, _ := m.Current(c)
			return s.OnCompleted(func(context.Context, Completion) error { order = append(order, 1); return nil })
		}); e != nil {
			return e
		}
		if len(order) != 0 {
			t.Fatal("Required hook ran early")
		}
		return m.Within(p, Options{Propagation: RequiresNew}, func(c context.Context) error {
			s, _ := m.Current(c)
			return s.OnCompleted(func(context.Context, Completion) error { order = append(order, 2); return nil })
		})
	}); e != nil || !reflect.DeepEqual(order, []int{2, 0, 1}) {
		t.Fatal(e, order)
	}
}
