// Package database defines the driver-neutral data access Port shared by the
// PostgreSQL, MySQL and SQLite adapters. Signatures expose only standard
// library, UOW and scope types; pgx, MySQL and SQLite driver types never leak.
// Backends without engine-level guarantees (for example PostgreSQL row-level
// security) must fail closed through Capabilities admission.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cymondez/pangolin-framework/persistence/uow"
)

// Capabilities is the truthful admission contract of one backend. It reports
// what the database engine itself enforces. The UOW guard layer (read-only
// views, transaction requirements, busy checks) is additional on every backend
// and is never advertised here.
type Capabilities struct {
	// Backend identifies the adapter, for example "postgres".
	Backend string
	// RowLevelSecurity reports engine-enforced tenant/owner isolation as
	// provided by FORCE RLS on PostgreSQL. MySQL and SQLite report false.
	RowLevelSecurity bool
	// TransactionReadOnly reports engine-enforced read-only transactions
	// (BEGIN READ ONLY / START TRANSACTION READ ONLY). SQLite reports
	// false; its query_only PRAGMA is connection scoped and would poison
	// pooled connections.
	TransactionReadOnly bool
	// Isolations lists every isolation level the backend can honor. An
	// empty list means only the default level is admitted.
	Isolations []uow.Isolation
}

// Supports reports whether the backend can enforce the requested level.
func (c Capabilities) Supports(level uow.Isolation) bool {
	for _, supported := range c.Isolations {
		if supported == level {
			return true
		}
	}
	return false
}

var (
	// ErrCapabilityRequired fails closed when a requirement has no truthful
	// support on the requested backend.
	ErrCapabilityRequired = errors.New("DATABASE_CAPABILITY_REQUIRED")
	// ErrIsolationUnsupported fails closed when a backend cannot enforce a
	// requested isolation level instead of silently downgrading it.
	ErrIsolationUnsupported = errors.New("DATABASE_ISOLATION_UNSUPPORTED")
)

// Requirements declares what a caller needs from a backend before it is
// allowed to run. Admit rejects unsupported combinations; a backend never
// silently promises an engine guarantee it does not have.
type Requirements struct {
	RowLevelSecurity    bool
	TransactionReadOnly bool
	Isolation           uow.Isolation
}

// Admit validates requirements against a truthful capability set and fails
// closed on the first unsupported one.
func Admit(c Capabilities, r Requirements) error {
	var missing []string
	if r.RowLevelSecurity && !c.RowLevelSecurity {
		missing = append(missing, "row-level-security")
	}
	if r.TransactionReadOnly && !c.TransactionReadOnly {
		missing = append(missing, "transaction-read-only")
	}
	if r.Isolation != uow.IsolationDefault && !c.Supports(r.Isolation) {
		missing = append(missing, "isolation-"+isolationName(r.Isolation))
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: backend %s lacks %s", ErrCapabilityRequired, c.Backend, strings.Join(missing, ","))
	}
	return nil
}

func isolationName(level uow.Isolation) string {
	switch level {
	case uow.ReadCommitted:
		return "read-committed"
	case uow.RepeatableRead:
		return "repeatable-read"
	case uow.Serializable:
		return "serializable"
	default:
		return "default"
	}
}

// Dialect carries the SQL generation differences between backends. It is
// data, not a feature claim; capabilities live in Capabilities.
type Dialect struct {
	// Placeholder renders the n-th (1-based) argument marker.
	Placeholder func(n int) string
	// Identifier quotes an identifier chain, for example schema.table.
	Identifier func(parts ...string) string
}

var (
	// DollarDialect renders $N placeholders and standard quoted identifiers
	// (PostgreSQL).
	DollarDialect = Dialect{
		Placeholder: func(n int) string { return fmt.Sprintf("$%d", n) },
		Identifier: func(parts ...string) string {
			quoted := make([]string, len(parts))
			for i, part := range parts {
				quoted[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
			}
			return strings.Join(quoted, ".")
		},
	}
	// QuestionDialect renders ? placeholders and backtick identifiers
	// (MySQL, and SQLite which accepts the same quoting).
	QuestionDialect = Dialect{
		Placeholder: func(int) string { return "?" },
		Identifier: func(parts ...string) string {
			quoted := make([]string, len(parts))
			for i, part := range parts {
				quoted[i] = "`" + strings.ReplaceAll(part, "`", "``") + "`"
			}
			return strings.Join(quoted, ".")
		},
	}
)

// Resource is the driver-neutral UOW resource Port. Begin installs the
// guarded Executor in the returned context; only the Manager completes the
// returned transaction.
type Resource interface {
	Begin(context.Context, uow.EffectiveOptions) (context.Context, uow.Transaction, error)
	// DatabaseKey identifies the single database of this resource.
	DatabaseKey() string
	// RequiresTransaction reports whether reads also need transactions,
	// which is true only for transaction-local RLS backends.
	RequiresTransaction() bool
	// Capabilities returns the truthful admission contract.
	Capabilities() Capabilities
	// Dialect returns the SQL generation rules of this backend.
	Dialect() Dialect
}

// ExecResult is the neutral statement result.
type ExecResult struct {
	RowsAffected int64
	LastInsertID int64
}

// Row scans at most one row. Scan may be called only once; a row that is
// never scanned keeps the unit of work busy and the transaction rolls back.
type Row interface {
	Scan(dest ...any) error
}

// Rows iterates a guarded cursor. Close is mandatory; unclosed cursors keep
// the unit of work busy and the transaction rolls back. Err reports the
// cursor's failure once iteration ended.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

// Executor never exposes Begin, Commit, Rollback or raw connections. Exec, QueryRowWrite, and
// QueryWrite carry explicit write intent and require a read-write
// transaction. Query and QueryRow are for read SQL by caller contract. This
// intent API is not an arbitrary-SQL sandbox: a caller can still misuse a
// read method with a mutating statement, and not every engine enforces
// transaction-level read-only mode.
type Executor interface {
	// DatabaseKey is the resource identity this executor is bound to.
	DatabaseKey() string
	// Exec runs a write statement and requires a read-write transaction.
	Exec(ctx context.Context, query string, args ...any) (ExecResult, error)
	// QueryRowWrite runs a write statement that returns one row (for example
	// INSERT ... RETURNING). It is guarded as a write before SQL is sent.
	QueryRowWrite(ctx context.Context, query string, args ...any) Row
	// QueryWrite runs a write statement that returns rows and is guarded as a
	// write before SQL is sent.
	QueryWrite(ctx context.Context, query string, args ...any) (Rows, error)
	// Query runs a read cursor. The caller must use read SQL; this is not an
	// engine-enforced SQL classifier.
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	// QueryRow runs a read single-row statement; it reports a missing
	// row as sql.ErrNoRows on Scan, not as a returned error.
	QueryRow(ctx context.Context, query string, args ...any) Row
}

// Binder is implemented by values a Resource installs under the executor key.
// Bind returns a fresh executor view bound to the caller's operation context,
// exactly like postgres.Current: a Required child context inherits the same
// session and receives its own view, and an ended view fails closed forever.
// A view is never rebound to a different session.
type Binder interface {
	DatabaseKey() string
	Bind(context.Context) (Executor, error)
}

type executorKey struct{}

// WithExecutor installs the session-owned binder of the active resource in
// the operation context. Only Resource implementations call it.
func WithExecutor(ctx context.Context, b Binder) context.Context {
	return context.WithValue(ctx, executorKey{}, b)
}

// Current returns a guarded executor view of the current unit of work,
// bound to ctx. It fails outside an active unit of work, on the wrong
// resource and after the work context has ended.
func Current(ctx context.Context) (Executor, error) {
	if err := uow.Check(ctx, false); err != nil {
		return nil, err
	}
	b, ok := ctx.Value(executorKey{}).(Binder)
	if !ok {
		return nil, uow.Fail(ctx, uow.ErrResourceMismatch)
	}
	if err := uow.CheckResource(ctx, b.DatabaseKey(), false); err != nil {
		return nil, err
	}
	return b.Bind(ctx)
}

// ErrNoRows is the neutral sentinel for a missing row result. Adapters map
// their driver's sentinel onto it; it is compatible with sql.ErrNoRows.
var ErrNoRows = sql.ErrNoRows
