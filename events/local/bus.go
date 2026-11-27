package local

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/cymondez/pangolin-framework/kernel/ambient"
	"github.com/cymondez/pangolin-framework/persistence/uow"
)

var ErrEventLimit = errors.New("LOCAL_EVENT_LIMIT")
var ErrDepthLimit = errors.New("LOCAL_EVENT_DEPTH_LIMIT")
var ErrQueueRequired = errors.New("LOCAL_EVENT_UOW_REQUIRED")

type Limits struct{ MaxEventsPerUOW, MaxDispatchDepth int }
type Handler struct {
	Event    any
	Priority int
	Handle   func(context.Context, any) error
}
type Bus struct {
	limits   Limits
	handlers []Handler
	queue    *ambient.Var[*Queue]
}

// NewBus seals registrations. Event specifies an example value's exact Go type.
func NewBus(limits Limits, handlers ...Handler) (*Bus, error) {
	if limits.MaxEventsPerUOW <= 0 || limits.MaxDispatchDepth <= 0 {
		return nil, fmt.Errorf("positive local event limits required")
	}
	b := &Bus{limits: limits, handlers: append([]Handler(nil), handlers...), queue: ambient.New[*Queue]()}
	for _, h := range b.handlers {
		if h.Event == nil || h.Handle == nil {
			return nil, fmt.Errorf("invalid local handler")
		}
	}
	sort.SliceStable(b.handlers, func(i, j int) bool { return b.handlers[i].Priority < b.handlers[j].Priority })
	return b, nil
}

var depthVar = ambient.New[int]()

type entry struct {
	event any
	depth int
}
type Queue struct {
	bus      *Bus
	mu       sync.Mutex
	events   []entry
	total    int
	draining bool
	closed   bool
}

// Attach is for a Resource.Begin adapter. Each root owns a separate queue.
func (b *Bus) Attach(ctx context.Context) (context.Context, *Queue) {
	q := &Queue{bus: b}
	if b == nil || b.queue == nil {
		return ctx, q
	}
	ctx = b.queue.With(ctx, q)
	ctx = uow.AttachSlot(ctx, b.queue)
	ctx = uow.AttachSlot(ctx, depthVar)
	return ctx, q
}
func (b *Bus) Enqueue(ctx context.Context, event any) error {
	if err := uow.Check(ctx, true); err != nil {
		return err
	}
	if b == nil || b.queue == nil {
		return uow.Fail(ctx, ErrQueueRequired)
	}
	q, _ := b.queue.Current(ctx)
	if q == nil || event == nil {
		return uow.Fail(ctx, ErrQueueRequired)
	}
	depth := 0
	if n, ok := depthVar.Current(ctx); ok {
		depth = n + 1
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return uow.Fail(ctx, uow.ErrClosed)
	}
	if depth > b.limits.MaxDispatchDepth {
		return uow.Fail(ctx, ErrDepthLimit)
	}
	if q.total >= b.limits.MaxEventsPerUOW {
		return uow.Fail(ctx, ErrEventLimit)
	}
	q.total++
	q.events = append(q.events, entry{event, depth})
	return nil
}

// Drain iterates FIFO and flushes each event's mutations, including new events.
func (q *Queue) Drain(ctx context.Context, flush func(context.Context) error) error {
	q.mu.Lock()
	if q.draining || q.closed {
		q.mu.Unlock()
		return uow.Fail(ctx, uow.ErrBusy)
	}
	q.draining = true
	q.mu.Unlock()
	defer func() { q.mu.Lock(); q.draining = false; q.closed = true; q.events = nil; q.mu.Unlock() }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		q.mu.Lock()
		if len(q.events) == 0 {
			q.mu.Unlock()
			return nil
		}
		next := q.events[0]
		q.events = q.events[1:]
		q.mu.Unlock()
		handlerContext := depthVar.With(ctx, next.depth)
		for _, h := range q.bus.handlers {
			if reflect.TypeOf(h.Event) == reflect.TypeOf(next.event) {
				if err := h.Handle(handlerContext, next.event); err != nil {
					return uow.Fail(ctx, err)
				}
			}
		}
		if flush != nil {
			if err := flush(ctx); err != nil {
				return uow.Fail(ctx, err)
			}
		}
	}
}
