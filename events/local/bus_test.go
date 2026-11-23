package local

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type event int
type resource struct{ bus *Bus }
type transaction struct{ queue *Queue }

func (r resource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	c, q := r.bus.Attach(c)
	return c, transaction{q}, nil
}
func (t transaction) BeforeCommit(c context.Context) error { return t.queue.Drain(c, nil) }
func (transaction) Commit(context.Context) error           { return nil }
func (transaction) Rollback(context.Context) error         { return nil }
func bound(t *testing.T) context.Context {
	r := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "a", true
	})
	c, e := r.Bind(context.Background(), scope.ScopeRef{TenantID: "a", Kind: "tenant", ID: "a"})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func manager(t *testing.T, b *Bus) uow.UnitOfWorkManager {
	m, e := uow.NewManager(resource{b}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 2})
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestA55LocalEvents(t *testing.T) {
	t.Run("priority-registration-order", func(t *testing.T) {
		var got []int
		b, _ := NewBus(Limits{10, 3}, Handler{Event: event(0), Priority: 2, Handle: func(context.Context, any) error { got = append(got, 2); return nil }}, Handler{Event: event(0), Priority: 1, Handle: func(context.Context, any) error { got = append(got, 1); return nil }}, Handler{Event: event(0), Priority: 1, Handle: func(context.Context, any) error { got = append(got, 3); return nil }})
		if e := manager(t, b).Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return b.Enqueue(c, event(0)) }); e != nil || !reflect.DeepEqual(got, []int{1, 3, 2}) {
			t.Fatal(e, got)
		}
	})
	t.Run("fifo-new-events", func(t *testing.T) {
		var got []int
		var b *Bus
		b, _ = NewBus(Limits{10, 3}, Handler{Event: event(0), Handle: func(c context.Context, e any) error {
			n := int(e.(event))
			got = append(got, n)
			if n == 1 {
				return b.Enqueue(c, event(3))
			}
			return nil
		}})
		if e := manager(t, b).Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
			if e := b.Enqueue(c, event(1)); e != nil {
				return e
			}
			return b.Enqueue(c, event(2))
		}); e != nil || !reflect.DeepEqual(got, []int{1, 2, 3}) {
			t.Fatal(e, got)
		}
	})
	for _, tc := range []struct {
		name   string
		limits Limits
		want   error
	}{{"event-limit", Limits{2, 10}, ErrEventLimit}, {"depth-limit", Limits{10, 1}, ErrDepthLimit}, {"cyclic-chain", Limits{5, 10}, ErrEventLimit}} {
		t.Run(tc.name, func(t *testing.T) {
			var b *Bus
			b, _ = NewBus(tc.limits, Handler{Event: event(0), Handle: func(c context.Context, e any) error { return b.Enqueue(c, e) }})
			e := manager(t, b).Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return b.Enqueue(c, event(1)) })
			if !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
}

func TestLocalQueueGuards(t *testing.T) {
	for _, limits := range []Limits{{}, {1, 0}, {0, 1}} {
		if _, e := NewBus(limits); e == nil {
			t.Fatal("invalid limits")
		}
	}
	for _, h := range []Handler{{}, {Event: event(0)}} {
		if _, e := NewBus(Limits{2, 2}, h); e == nil {
			t.Fatal("invalid handler")
		}
	}
	b, _ := NewBus(Limits{2, 2})
	if e := b.Enqueue(context.Background(), event(0)); e == nil {
		t.Fatal("no UOW accepted")
	}
	m := manager(t, b)
	var captured context.Context
	if e := m.Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { captured = c; return b.Enqueue(c, event(0)) }); e != nil {
		t.Fatal(e)
	}
	if e := b.Enqueue(captured, event(0)); !errors.Is(e, uow.ErrClosed) {
		t.Fatal(e)
	}
	if e := m.Within(bound(t), uow.Options{}, func(c context.Context) error { return b.Enqueue(c, event(0)) }); !errors.Is(e, uow.ErrReadOnly) {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name string
		work func(context.Context) error
	}{{"nil-event", func(c context.Context) error { return b.Enqueue(c, nil) }}, {"queue-missing", func(c context.Context) error { other, _ := NewBus(Limits{1, 1}); return other.Enqueue(c, event(0)) }}} {
		t.Run(tc.name, func(t *testing.T) {
			if e := m.Within(bound(t), uow.Options{Mode: uow.ReadWrite}, tc.work); !errors.Is(e, ErrQueueRequired) {
				t.Fatal(e)
			}
		})
	}
	t.Run("depth-boundary", func(t *testing.T) {
		calls := 0
		var recursive *Bus
		recursive, _ = NewBus(Limits{3, 2}, Handler{Event: event(0), Handle: func(c context.Context, v any) error {
			calls++
			if v.(event) < 2 {
				return recursive.Enqueue(c, v.(event)+1)
			}
			return nil
		}})
		if e := manager(t, recursive).Within(bound(t), uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error { return recursive.Enqueue(c, event(0)) }); e != nil || calls != 3 {
			t.Fatal(e, calls)
		}
	})
	t.Run("flush-error", func(t *testing.T) {
		ctx, q := b.Attach(context.Background())
		q.events = []entry{{event: event(1)}}
		cause := errors.New("flush failed")
		if e := q.Drain(ctx, func(context.Context) error { return cause }); !errors.Is(e, cause) {
			t.Fatal(e)
		}
		if e := q.Drain(ctx, nil); e == nil {
			t.Fatal("double drain accepted")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ctx, q := b.Attach(context.Background())
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		if e := q.Drain(ctx, nil); !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	})
}
