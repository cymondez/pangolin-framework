// Package migration defines deployment-only migration ports. API startup never invokes Runner.
package migration

import "context"

// LockKey is shared by all modules in this application's database.
const LockKey int64 = 0x70616e676f6c696e

type ModuleMigrations struct {
	Module    string
	DependsOn []string
	Directory string
}

// Lock owns an exclusive PostgreSQL session; Release never releases another session's lock.
type Lock interface {
	Release(context.Context) error
}
type Locker interface {
	Acquire(context.Context, int64) (Lock, error)
}
type Runner interface {
	Apply(context.Context, []ModuleMigrations) error
	CheckCompatibility(context.Context) error
}
