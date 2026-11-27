package runtime

import (
	"context"
	"fmt"
	"sync"
)

// WorkGroup tracks admitted HTTP operations or worker claims across shutdown.
// An owner calls done only after releasing its transaction/lease resources.
type WorkGroup struct {
	mu     sync.Mutex
	closed bool
	next   uint64
	active map[uint64]context.CancelFunc
	empty  chan struct{}
}

func NewWorkGroup() *WorkGroup {
	ch := make(chan struct{})
	close(ch)
	return &WorkGroup{active: map[uint64]context.CancelFunc{}, empty: ch}
}
func (g *WorkGroup) Enter(ctx context.Context) (context.Context, func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ctx == nil || g.closed {
		return nil, nil, fmt.Errorf("work admission closed")
	}
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	if len(g.active) == 0 {
		g.empty = make(chan struct{})
	}
	g.next++
	id := g.next
	work, cancel := context.WithCancel(ctx)
	g.active[id] = cancel
	var once sync.Once
	done := func() {
		once.Do(func() {
			cancel()
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(g.active, id)
			if len(g.active) == 0 {
				close(g.empty)
			}
		})
	}
	return work, done, nil
}
func (g *WorkGroup) Quiesce(context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	return nil
}
func (g *WorkGroup) Drain(ctx context.Context) error {
	g.mu.Lock()
	empty := g.empty
	g.mu.Unlock()
	select {
	case <-empty:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (g *WorkGroup) Cancel() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, cancel := range g.active {
		cancel()
	}
}
