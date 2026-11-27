package http

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/cymondez/pangolin-framework/kernel/runtime"
)

// ServerParticipant bridges an *http.Server, a runtime.WorkGroup and optional
// extra runtime.Participant instances into the host's graceful shutdown order:
// admission stops first, keep-alives are disabled, then the HTTP server and
// nested participants drain under the shared shutdown deadline.
type ServerParticipant struct {
	work   *runtime.WorkGroup
	mu     sync.Mutex
	server *http.Server
	extras []runtime.Participant
}

// NewServerParticipant creates a ServerParticipant backed by work (a fresh
// WorkGroup is allocated when work is nil) plus any non-nil extra participants.
func NewServerParticipant(work *runtime.WorkGroup, extra ...runtime.Participant) *ServerParticipant {
	if work == nil {
		work = runtime.NewWorkGroup()
	}
	p := &ServerParticipant{work: work}
	p.Add(extra...)
	return p
}

// Work returns the underlying admission WorkGroup.
func (p *ServerParticipant) Work() *runtime.WorkGroup {
	if p == nil {
		return nil
	}
	return p.work
}

// Attach binds the HTTP server drained by this participant.
func (p *ServerParticipant) Attach(server *http.Server) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.server = server
	p.mu.Unlock()
}

// Add registers additional runtime participants coordinated with this server.
func (p *ServerParticipant) Add(participants ...runtime.Participant) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, participant := range participants {
		if participant != nil && !isNilInterface(participant) {
			p.extras = append(p.extras, participant)
		}
	}
}

// Wrap guards next with WorkGroup admission tracking so active HTTP operations
// block Drain until they finish releasing their resources.
func (p *ServerParticipant) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p == nil || p.work == nil || next == nil {
			WriteError(w, r, errors.New("INTERNAL"))
			return
		}
		ctx, done, err := p.work.Enter(r.Context())
		if err != nil {
			WriteError(w, r, err)
			return
		}
		defer done()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (p *ServerParticipant) snapshot() (*http.Server, []runtime.Participant) {
	p.mu.Lock()
	defer p.mu.Unlock()
	extras := append([]runtime.Participant(nil), p.extras...)
	return p.server, extras
}

// Quiesce disables HTTP keep-alives, closes new WorkGroup admission and
// quiesces all nested participants.
func (p *ServerParticipant) Quiesce(ctx context.Context) error {
	if p == nil {
		return nil
	}
	server, extras := p.snapshot()
	if server != nil {
		server.SetKeepAlivesEnabled(false)
	}
	var err error
	if p.work != nil {
		err = p.work.Quiesce(ctx)
	}
	for _, extra := range extras {
		err = errors.Join(err, extra.Quiesce(ctx))
	}
	return err
}

// Drain shuts down the attached HTTP server and drains all nested participants
// and tracked WorkGroup operations within ctx.
func (p *ServerParticipant) Drain(ctx context.Context) error {
	if p == nil {
		return nil
	}
	server, extras := p.snapshot()
	var err error
	for _, extra := range extras {
		err = errors.Join(err, extra.Drain(ctx))
	}
	if server != nil {
		err = errors.Join(err, server.Shutdown(ctx))
	}
	if p.work != nil {
		err = errors.Join(err, p.work.Drain(ctx))
	}
	return err
}

// Cancel cancels all active WorkGroup operations and nested participants.
func (p *ServerParticipant) Cancel() {
	if p == nil {
		return
	}
	_, extras := p.snapshot()
	if p.work != nil {
		p.work.Cancel()
	}
	for _, extra := range extras {
		extra.Cancel()
	}
}

var _ runtime.Participant = (*ServerParticipant)(nil)
