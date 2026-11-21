// Package uow defines the frozen §5 ports. No UnitOfWorkManager implementation exists in M0.
package uow

import (
	"context"
	"time"
)

type AccessMode uint8

const (
	AccessDefault AccessMode = iota
	ReadOnly
	ReadWrite
)

type TransactionMode uint8

const (
	TransactionAuto TransactionMode = iota
	Transactional
	NonTransactional
)

type Propagation uint8

const (
	Required Propagation = iota
	RequiresNew
)

type Isolation uint8

const (
	IsolationDefault Isolation = iota
	ReadCommitted
	RepeatableRead
	Serializable
)

type Options struct {
	Mode        AccessMode
	Transaction TransactionMode
	Propagation Propagation
	Isolation   Isolation
	Timeout     time.Duration
}
type DefaultOptions struct {
	DefaultTimeout      time.Duration
	CleanupTimeout      time.Duration
	HookTimeout         time.Duration
	MaxRequiresNewDepth int
}
type EffectiveOptions struct {
	Mode              AccessMode
	Transactional     bool
	Isolation         Isolation
	Deadline          time.Time
	DatabaseKey       string
	ResourceScopeKind string
	ResourceScopeID   string
}
type Outcome uint8

const (
	NotStarted Outcome = iota
	Committed
	CompletedNonTransactional
	RolledBack
	Unknown
)

type Completion struct {
	ScopeID     string
	OperationID string
	Outcome     Outcome
	Err         error
}
type Scope interface {
	ID() string
	Options() EffectiveOptions
	SaveChanges(context.Context) error
	SetRollbackOnly(error) error
	OnCompleted(func(context.Context, Completion) error) error
	OnFailed(func(context.Context, Completion) error) error
	OnDisposed(func(context.Context, Completion) error) error
}
type UnitOfWorkManager interface {
	Within(context.Context, Options, func(context.Context) error) error
	Current(context.Context) (Scope, bool)
}

// Error is the errors.As view of a UOW failure; causes remain traversable.
type Error interface {
	error
	Code() string
	ScopeID() string
	OperationID() string
	Outcome() Outcome
	Unwrap() error
}
