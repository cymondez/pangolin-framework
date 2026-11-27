// Package application provides explicit policies shared by HTTP, jobs and consumers.
package application

import (
	"context"
	"reflect"

	"github.com/cymondez/pangolin-framework/observability"
	"github.com/cymondez/pangolin-framework/persistence/uow"
)

// Command establishes the same write policy for every transport.
func Command(ctx context.Context, m uow.UnitOfWorkManager, callback func(context.Context) error) (err error) {
	if callback == nil {
		return uow.ErrOptionsInvalid
	}
	_, err = executeResult(ctx, m, uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}, func(work context.Context) (struct{}, error) {
		return struct{}{}, callback(work)
	})
	return err
}

// CommandResult establishes the write policy and returns the callback result only after confirmed completion.
func CommandResult[T any](ctx context.Context, m uow.UnitOfWorkManager, callback func(context.Context) (T, error)) (T, error) {
	var result T
	if callback == nil {
		return result, uow.ErrOptionsInvalid
	}
	return executeResult(ctx, m, uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}, callback)
}

// Query explicitly restricts data access; an RLS adapter opens a read transaction.
func Query(ctx context.Context, m uow.UnitOfWorkManager, callback func(context.Context) error) (err error) {
	if callback == nil {
		return uow.ErrOptionsInvalid
	}
	_, err = executeResult(ctx, m, uow.Options{Mode: uow.ReadOnly}, func(work context.Context) (struct{}, error) {
		return struct{}{}, callback(work)
	})
	return err
}

// QueryResult restricts data access and returns the callback result only after confirmed completion.
func QueryResult[T any](ctx context.Context, m uow.UnitOfWorkManager, callback func(context.Context) (T, error)) (T, error) {
	var result T
	if callback == nil {
		return result, uow.ErrOptionsInvalid
	}
	return executeResult(ctx, m, uow.Options{Mode: uow.ReadOnly}, callback)
}

func executeResult[T any](ctx context.Context, m uow.UnitOfWorkManager, options uow.Options, callback func(context.Context) (T, error)) (result T, err error) {
	if callback == nil || isNilInterface(ctx) || isNilInterface(m) {
		return result, uow.ErrOptionsInvalid
	}
	if ctx != nil {
		var finish func(error)
		ctx, finish = observability.Start(ctx, "use_case")
		defer func() { finish(err) }()
	}

	var callbackResult T
	err = m.Within(ctx, options, func(work context.Context) error {
		callbackResult, err = callback(work)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return callbackResult, nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
