// Package inbox separates durable reception from transactional consumption.
package inbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/observability"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrContentConflict = errors.New("INBOX_CONTENT_CONFLICT")
var ErrUnavailable = errors.New("INBOX_UNAVAILABLE")

type Candidate struct {
	Group, ID, Producer string
	Envelope            distributed.Envelope
	Version             int64
	Fingerprint         []byte
}
type Store interface {
	Persist(context.Context, string, distributed.Envelope, []byte, string) error
	Candidate(context.Context, string) (*Candidate, error)
	Lock(context.Context, Candidate) (bool, error)
	Processed(context.Context, Candidate) error
	Retry(context.Context, Candidate, int, time.Duration) error
}

// Verify must authenticate the Broker session/route independently of Producer.
type Verify func(context.Context, distributed.Envelope) (string, error)
type Receiver struct {
	store    Store
	registry *distributed.Registry
	group    string
	verify   Verify
	alert    func(context.Context, error)
}

func NewReceiver(store Store, registry *distributed.Registry, group string, verify Verify, alert func(context.Context, error)) (*Receiver, error) {
	if store == nil || registry == nil || group == "" || verify == nil || alert == nil {
		return nil, ErrUnavailable
	}
	return &Receiver{store, registry, group, verify, alert}, nil
}
func (r *Receiver) Receive(ctx context.Context, event distributed.Envelope, ack func(context.Context) error) error {
	if ack == nil {
		return ErrUnavailable
	}
	raw, err := r.registry.Encode(event)
	if err != nil {
		return err
	}
	producer, err := r.verify(ctx, event)
	if err != nil {
		return err
	}
	if producer == "" || producer != event.Producer {
		return scope.ErrDenied
	}
	if err = r.store.Persist(ctx, r.group, event, raw, producer); err != nil {
		if errors.Is(err, ErrContentConflict) {
			r.alert(ctx, ErrContentConflict)
		}
		return err
	}
	return ack(ctx)
}

type Worker struct {
	store    Store
	manager  uow.UnitOfWorkManager
	group    string
	bind     func(context.Context, Candidate) (context.Context, error)
	handler  func(context.Context, distributed.Envelope) error
	attempts int
	retry    time.Duration
}

func NewWorker(store Store, manager uow.UnitOfWorkManager, group string, bind func(context.Context, Candidate) (context.Context, error), handler func(context.Context, distributed.Envelope) error, maxAttempts int, retry time.Duration) (*Worker, error) {
	if store == nil || manager == nil || group == "" || bind == nil || handler == nil || maxAttempts <= 0 || retry <= 0 {
		return nil, ErrUnavailable
	}
	return &Worker{store, manager, group, bind, handler, maxAttempts, retry}, nil
}
func (w *Worker) Process(ctx context.Context) (processed bool, err error) {
	if ctx == nil {
		return false, ErrUnavailable
	}
	candidate, err := w.store.Candidate(ctx, w.group)
	if err != nil || candidate == nil {
		return false, err
	}
	ctx = observability.WithOperation(observability.Restore(ctx, candidate.Envelope.TraceParent), candidate.Envelope.CorrelationID)
	ctx = observability.WithMessage(ctx, "event", candidate.Envelope.ID)
	ctx, finish := observability.Start(ctx, "consumer")
	defer func() { finish(err) }()
	bound, err := w.bind(ctx, *candidate)
	if err != nil {
		slog.ErrorContext(ctx, "inbox scope authorization rejected", "event_id", candidate.ID, "consumer_group", w.group)
		return false, w.retryFailure(ctx, *candidate, err)
	}
	b, ok := scope.Current(bound)
	if !ok || b.Principal.Kind != "service" || b.Ref != (scope.ScopeRef{TenantID: candidate.Envelope.TenantID, Kind: candidate.Envelope.ScopeKind, ID: candidate.Envelope.ScopeID}) {
		slog.ErrorContext(ctx, "inbox scope binding mismatch", "event_id", candidate.ID, "consumer_group", w.group)
		return false, w.retryFailure(ctx, *candidate, scope.ErrDenied)
	}
	err = w.manager.Within(bound, uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}, func(work context.Context) error {
		locked, err := w.store.Lock(work, *candidate)
		if err != nil || !locked {
			return err
		}
		if err = w.handler(work, candidate.Envelope); err != nil {
			return err
		}
		if err = w.store.Processed(work, *candidate); err != nil {
			return err
		}
		processed = true
		return nil
	})
	if err != nil {
		processed = false
		var failure uow.Error
		if errors.As(err, &failure) && (failure.Outcome() == uow.RolledBack || failure.Outcome() == uow.NotStarted) {
			err = w.retryFailure(ctx, *candidate, err)
		}
	}
	return processed, err
}
func (w *Worker) retryFailure(ctx context.Context, candidate Candidate, cause error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	return errors.Join(cause, w.store.Retry(cleanup, candidate, w.attempts, w.retry))
}

// ExternalKey is stable across crashes. The external provider must honor it.
// Inbox alone provides no exactly-once guarantee for external side effects.
func ExternalKey(group string, event distributed.Envelope, operation string) (string, error) {
	if group == "" || event.ID == "" || operation == "" {
		return "", fmt.Errorf("external idempotency identity required")
	}
	hash := sha256.Sum256([]byte(group + "\x00" + event.ID + "\x00" + event.TenantID + "\x00" + event.ScopeKind + "\x00" + event.ScopeID + "\x00" + operation))
	return hex.EncodeToString(hash[:]), nil
}
