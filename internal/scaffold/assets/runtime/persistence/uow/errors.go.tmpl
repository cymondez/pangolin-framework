package uow

import (
	"context"
	"errors"
)

var (
	ErrOptionsInvalid      = errors.New("UOW_OPTIONS_INVALID")
	ErrOptionsConflict     = errors.New("UOW_OPTIONS_CONFLICT")
	ErrRequired            = errors.New("UOW_REQUIRED")
	ErrReadOnly            = errors.New("UOW_READ_ONLY")
	ErrTransactionRequired = errors.New("UOW_TRANSACTION_REQUIRED")
	ErrTenantMismatch      = errors.New("UOW_TENANT_MISMATCH")
	ErrIdentityMismatch    = errors.New("UOW_IDENTITY_MISMATCH")
	ErrScopeMismatch       = errors.New("UOW_SCOPE_MISMATCH")
	ErrResourceMismatch    = errors.New("UOW_RESOURCE_MISMATCH")
	ErrClosed              = errors.New("UOW_CLOSED")
	ErrSuspended           = errors.New("UOW_SUSPENDED")
	ErrBusy                = errors.New("UOW_BUSY")
	ErrDepthExceeded       = errors.New("UOW_DEPTH_EXCEEDED")
	ErrRollbackOnly        = errors.New("UOW_ROLLBACK_ONLY")
	ErrCommitUnknown       = errors.New("UOW_COMMIT_UNKNOWN")
)

type failure struct {
	code    error
	id      string
	outcome Outcome
	cause   error
}

func (e *failure) Error() string        { return e.code.Error() + ": " + e.id }
func (e *failure) Code() string         { return e.code.Error() }
func (e *failure) ScopeID() string      { return e.id }
func (e *failure) OperationID() string  { return e.id }
func (e *failure) Outcome() Outcome     { return e.outcome }
func (e *failure) Unwrap() error        { return e.cause }
func (e *failure) Is(target error) bool { return target != nil && errors.Is(e.code, target) }
func fail(code error, id string, outcome Outcome, cause error) error {
	return &failure{code, id, outcome, cause}
}

func errorCode(err error) error {
	for _, code := range []error{ErrOptionsInvalid, ErrOptionsConflict, ErrRequired, ErrReadOnly, ErrTransactionRequired, ErrTenantMismatch, ErrIdentityMismatch, ErrScopeMismatch, ErrResourceMismatch, ErrClosed, ErrSuspended, ErrBusy, ErrDepthExceeded, ErrRollbackOnly, ErrCommitUnknown} {
		if errors.Is(err, code) {
			return code
		}
	}
	return errors.New("UOW_ROLLED_BACK")
}

// CheckResource refuses a second database identity without a pool fallback.
func CheckResource(ctx context.Context, key string, write bool) error {
	if err := Check(ctx, write); err != nil {
		return err
	}
	v := currentView(ctx)
	if key != v.options.DatabaseKey {
		return Fail(ctx, fail(ErrResourceMismatch, v.root.id, NotStarted, nil))
	}
	return nil
}

// Fail preserves an adapter error and marks the active root rollback-only.
func Fail(ctx context.Context, cause error) error {
	if cause == nil {
		return nil
	}
	v := currentView(ctx)
	if v == nil {
		return cause
	}
	v.root.mu.Lock()
	defer v.root.mu.Unlock()
	if v.active && v.root.active && !v.root.suspended {
		v.root.rollback = errors.Join(v.root.rollback, cause)
	}
	return cause
}
