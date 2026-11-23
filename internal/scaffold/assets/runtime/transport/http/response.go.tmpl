// Package http provides transaction-aware non-streaming responses and short
// streaming operations. Authentication and scope binding precede these adapters.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/cymondez/pangolin-framework/application"
	"github.com/cymondez/pangolin-framework/persistence/idempotency"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/authn"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
)

// Problem exposes safe metadata without SQL, DSN, token or raw error text.
type Problem struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Detail      string `json:"detail"`
	Instance    string `json:"instance"`
	TraceID     string `json:"traceId"`
	Code        string `json:"code"`
	OperationID string `json:"operationId,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := 500, "INTERNAL"
	id, outcome := "", ""
	for _, entry := range []struct {
		err    error
		status int
	}{
		{authn.ErrUnauthenticated, 401}, {scope.ErrDenied, 403}, {scope.ErrInvalid, 400}, {scope.ErrRequired, 400}, {scope.ErrHintConflict, 400}, {scope.ErrKindUnsupported, 400}, {tenancy.ErrHintConflict, 400}, {scope.ErrAuthorityUnavailable, 503}, {ErrValidation, 400}, {ErrBodyTooLarge, 413},
	} {
		if errors.Is(err, entry.err) {
			status, code = entry.status, entry.err.Error()
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		status, code = 504, "TIMEOUT"
	}
	for _, denied := range []error{uow.ErrTenantMismatch, uow.ErrIdentityMismatch, uow.ErrScopeMismatch} {
		if errors.Is(err, denied) {
			status, code = 403, denied.Error()
		}
	}
	var failure uow.Error
	if errors.As(err, &failure) {
		id = failure.OperationID()
		if code == "INTERNAL" {
			code = failure.Code()
		}
		if failure.Outcome() == uow.Unknown {
			status, code, outcome = 503, failure.Code(), "unknown"
		}
	}
	var classified interface {
		HTTPStatus() int
		Code() string
	}
	if outcome != "unknown" && errors.As(err, &classified) {
		if s := classified.HTTPStatus(); s >= 400 && s <= 599 {
			status, code = s, classified.Code()
		}
	}
	if outcome != "unknown" {
		for _, sentinel := range []error{idempotency.ErrConflict, idempotency.ErrInProgress, idempotency.ErrExpired} {
			if errors.Is(err, sentinel) {
				status, code = 409, sentinel.Error()
			}
		}
	}
	writeProblem(w, r, status, code, id, outcome)
}

func writeResult(w http.ResponseWriter, r *http.Request, result any) {
	raw, err := marshalResponse(result)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
func marshalResponse(value any) (raw []byte, err error) {
	defer func() {
		if recover() != nil {
			raw = nil
			err = errors.New("response serialization failed")
		}
	}()
	return json.Marshal(value)
}
func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, id, outcome string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: http.StatusText(status), Instance: r.URL.Path, TraceID: r.Header.Get("X-Request-ID"), Code: code, OperationID: id, Outcome: outcome})
}

// Command owns a top-level write boundary. A surrounding UOW is rejected so a
// joined Required callback cannot accidentally send success before its root commits.
func Command(m uow.UnitOfWorkManager, work func(context.Context) (any, error)) http.Handler {
	return command(m, work, nil)
}
func command(m uow.UnitOfWorkManager, work func(context.Context) (any, error), rejected func(context.Context, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m == nil || work == nil {
			WriteError(w, r, uow.ErrOptionsInvalid)
			return
		}
		if _, ok := m.Current(r.Context()); ok {
			WriteError(w, r, uow.ErrBusy)
			return
		}
		var result any
		var id string
		err := application.Command(r.Context(), m, func(ctx context.Context) error {
			if s, ok := m.Current(ctx); ok {
				id = s.ID()
			}
			var e error
			result, e = work(ctx)
			return e
		})
		if err != nil {
			notifyRejected(rejected, r.Context(), err)
			WriteError(w, r, err)
			return
		}
		raw, err := marshalResponse(result)
		if err != nil {
			writeProblem(w, r, 500, "INTERNAL", id, "committed")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err = w.Write(raw); err != nil {
			slog.Error("HTTP response failed after commit", "operation_id", id, "outcome", "committed")
		}
	})
}

// StreamOperation runs one independent short query. Invoke it separately for
// each SSE/WebSocket operation; the connection context contains no DB executor.
func StreamOperation(ctx context.Context, m uow.UnitOfWorkManager, work func(context.Context) error) error {
	if m == nil {
		return uow.ErrOptionsInvalid
	}
	if _, ok := m.Current(ctx); ok {
		return uow.ErrBusy
	}
	return application.Query(ctx, m, work)
}
