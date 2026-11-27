package application

import (
	"context"
	"encoding/json"

	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/policy"
	"github.com/pangolin-framework/pangolin/security/scope"
)

// IdempotencyExecutor matches *idempotency.Store while keeping the application
// layer free of concrete database adapter imports.
type IdempotencyExecutor interface {
	Execute(ctx context.Context, operation, key string, body []byte, work func(context.Context) ([]byte, error)) ([]byte, bool, error)
}

// Runner is a single DI-injectable operation runner combining UOW admission,
// authorization policy checks and optional command idempotency.
type Runner struct {
	Manager     uow.UnitOfWorkManager
	Authorizer  policy.Authorizer
	Idempotency IdempotencyExecutor
}

// NewRunner creates a Runner with a required UnitOfWorkManager and optional
// Authorizer and IdempotencyExecutor. Attempting a permissioned or idempotent
// operation without the corresponding dependency fails closed.
func NewRunner(manager uow.UnitOfWorkManager, authorizer policy.Authorizer, idempotency IdempotencyExecutor) (*Runner, error) {
	if isNilInterface(manager) {
		return nil, uow.ErrOptionsInvalid
	}
	return &Runner{Manager: manager, Authorizer: authorizer, Idempotency: idempotency}, nil
}

// CommandSpec declares the authorization, UOW and optional idempotency policy
// of one command execution.
type CommandSpec struct {
	Permission     string
	ResourcePolicy policy.ResourcePolicy
	Options        *uow.Options
	IdempotencyOp  string
	IdempotencyKey string
	Fingerprint    []byte
}

// QuerySpec declares the authorization and UOW policy of one query execution.
type QuerySpec struct {
	Permission     string
	ResourcePolicy policy.ResourcePolicy
	Options        *uow.Options
}

// RunCommand executes work under a ReadWrite transactional unit of work
// (honoring compatible spec.Options overrides), enforcing entry/resource
// authorization and optional command idempotency with JSON replay.
// Any missing Authorizer for a Permission or missing IdempotencyExecutor for an
// idempotent spec fails closed.
func RunCommand[T any](ctx context.Context, r *Runner, spec CommandSpec, work func(context.Context) (T, error)) (T, error) {
	var zero T
	if r == nil || isNilInterface(r.Manager) || isNilInterface(ctx) || work == nil {
		return zero, uow.ErrOptionsInvalid
	}
	if spec.ResourcePolicy != nil && spec.Permission == "" {
		return zero, scope.ErrDenied
	}
	if spec.Permission != "" {
		if isNilInterface(r.Authorizer) {
			return zero, scope.ErrDenied
		}
		if err := policy.Require(ctx, r.Authorizer, spec.Permission, nil); err != nil {
			return zero, err
		}
	}
	useIdempotency := spec.IdempotencyOp != "" || spec.IdempotencyKey != "" || len(spec.Fingerprint) > 0
	if useIdempotency {
		if isNilInterface(r.Idempotency) || spec.IdempotencyOp == "" || spec.IdempotencyKey == "" {
			return zero, uow.ErrOptionsInvalid
		}
	}
	opts := uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}
	if spec.Options != nil {
		opts = *spec.Options
		if opts.Mode == uow.AccessDefault {
			opts.Mode = uow.ReadWrite
		}
		if opts.Transaction == uow.TransactionAuto {
			opts.Transaction = uow.Transactional
		}
		if opts.Mode != uow.ReadWrite || opts.Transaction != uow.Transactional {
			return zero, uow.ErrOptionsInvalid
		}
	}

	return executeResult(ctx, r.Manager, opts, func(c context.Context) (T, error) {
		var empty T
		if spec.Permission != "" && spec.ResourcePolicy != nil {
			if err := policy.Require(c, r.Authorizer, spec.Permission, spec.ResourcePolicy); err != nil {
				return empty, err
			}
		}
		if !useIdempotency {
			return work(c)
		}
		fingerprint := spec.Fingerprint
		if len(fingerprint) == 0 {
			fingerprint = []byte("{}")
		}
		var fresh T
		raw, replayed, err := r.Idempotency.Execute(c, spec.IdempotencyOp, spec.IdempotencyKey, fingerprint, func(ic context.Context) ([]byte, error) {
			val, workErr := work(ic)
			if workErr != nil {
				return nil, workErr
			}
			encoded, marshalErr := json.Marshal(val)
			if marshalErr != nil {
				return nil, marshalErr
			}
			fresh = val
			return encoded, nil
		})
		if err != nil {
			return empty, err
		}
		if !replayed {
			return fresh, nil
		}
		var out T
		if err := json.Unmarshal(raw, &out); err != nil {
			return empty, err
		}
		return out, nil
	})
}

// RunQuery executes work under a ReadOnly unit of work (honoring compatible
// spec.Options overrides) with fail-closed entry and optional resource policy checks.
func RunQuery[T any](ctx context.Context, r *Runner, spec QuerySpec, work func(context.Context) (T, error)) (T, error) {
	var zero T
	if r == nil || isNilInterface(r.Manager) || isNilInterface(ctx) || work == nil {
		return zero, uow.ErrOptionsInvalid
	}
	if spec.ResourcePolicy != nil && spec.Permission == "" {
		return zero, scope.ErrDenied
	}
	if spec.Permission != "" {
		if isNilInterface(r.Authorizer) {
			return zero, scope.ErrDenied
		}
		if err := policy.Require(ctx, r.Authorizer, spec.Permission, nil); err != nil {
			return zero, err
		}
	}
	opts := uow.Options{Mode: uow.ReadOnly}
	if spec.Options != nil {
		opts = *spec.Options
		if opts.Mode == uow.AccessDefault {
			opts.Mode = uow.ReadOnly
		}
		if opts.Mode != uow.ReadOnly {
			return zero, uow.ErrOptionsInvalid
		}
	}

	return executeResult(ctx, r.Manager, opts, func(c context.Context) (T, error) {
		var empty T
		if spec.Permission != "" && spec.ResourcePolicy != nil {
			if err := policy.Require(c, r.Authorizer, spec.Permission, spec.ResourcePolicy); err != nil {
				return empty, err
			}
		}
		return work(c)
	})
}
