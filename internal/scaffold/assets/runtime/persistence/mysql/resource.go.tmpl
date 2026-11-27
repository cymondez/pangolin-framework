// Package mysql provides the real MySQL adapter on the neutral database
// Port. It uses github.com/go-sql-driver/mysql and the shared guarded
// database/sql session. MySQL has no engine row-level security: capabilities
// report that truthfully and RLS-requiring callers fail closed.
package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pangolin-framework/pangolin/persistence/database"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/go-sql-driver/mysql"
)

// Capabilities is the truthful MySQL contract: no engine row-level security,
// enforced read-only transactions and the three standard isolation levels.
// MySQL ignores non-default isolation inside SAVEPOINT nesting; that is
// rejected by the manager before Begin.
func Capabilities() database.Capabilities {
	return database.Capabilities{
		Backend:             "mysql",
		RowLevelSecurity:    false,
		TransactionReadOnly: true,
		Isolations:          []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Serializable},
	}
}

// Config opens the real pool. DSN is a MySQL DSN like
// user:pass@tcp(host:port)/db; DSNConfig is the alternative for parsed
// configurations. MaxOpenConns bounds the pool; a zero value uses the
// driver default.
type Config struct {
	DSN       string
	DSNConfig *mysql.Config
	MaxOpen   int
	// Key overrides the resource identity.
	Key string
}

// Resource is a real MySQL database resource on the neutral Port.
type Resource struct {
	resource *database.SQLResource
}

// Open validates the configuration and opens the real pool.
func Open(c Config) (*Resource, error) {
	if c.DSN == "" && c.DSNConfig == nil {
		return nil, uow.ErrOptionsInvalid
	}
	var opened *sql.DB
	var err error
	if c.DSNConfig != nil {
		connector, cerr := mysql.NewConnector(c.DSNConfig)
		if cerr != nil {
			return nil, cerr
		}
		opened = sql.OpenDB(connector)
	} else {
		opened, err = sql.Open("mysql", c.DSN)
		if err != nil {
			return nil, err
		}
	}
	if c.MaxOpen > 0 {
		opened.SetMaxOpenConns(c.MaxOpen)
	}
	inner, err := database.NewSQLResource(opened, database.SQLConfig{
		Backend:       "mysql",
		Key:           c.Key,
		Capabilities:  Capabilities(),
		Dialect:       database.QuestionDialect,
		Isolation:     isolation,
		ReadOnly:      readOnly,
		CommitOutcome: commitOutcome,
	})
	if err != nil {
		_ = opened.Close()
		return nil, err
	}
	return &Resource{resource: inner}, nil
}

// Wrap adapts an already-opened *sql.DB, for example a pooled database
// owned by another composition step.
func Wrap(db *sql.DB, key string) (*Resource, error) {
	inner, err := database.NewSQLResource(db, database.SQLConfig{
		Backend:       "mysql",
		Key:           key,
		Capabilities:  Capabilities(),
		Dialect:       database.QuestionDialect,
		Isolation:     isolation,
		ReadOnly:      readOnly,
		CommitOutcome: commitOutcome,
	})
	if err != nil {
		return nil, err
	}
	return &Resource{resource: inner}, nil
}

func (r *Resource) Begin(ctx context.Context, o uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return r.resource.Begin(ctx, o)
}
func (r *Resource) DatabaseKey() string                 { return r.resource.DatabaseKey() }
func (r *Resource) RequiresTransaction() bool           { return r.resource.RequiresTransaction() }
func (r *Resource) Capabilities() database.Capabilities { return r.resource.Capabilities() }
func (r *Resource) Dialect() database.Dialect           { return r.resource.Dialect() }

// Pool exposes the underlying *sql.DB for infrastructure operations that the
// neutral Port does not model (schema setup by a migration tool, health
// checks). Business code uses the guarded executor.
func (r *Resource) Pool() *sql.DB { return r.resource.Pool() }

// Isolation maps a requested level to the MySQL driver level; the driver
// enforces it through SET TRANSACTION ISOLATION LEVEL. An explicit
// non-default request the backend cannot honor is rejected here, never
// silently downgraded.
func isolation(level uow.Isolation) (sql.IsolationLevel, error) {
	switch level {
	case uow.IsolationDefault:
		return sql.LevelDefault, nil
	case uow.ReadCommitted:
		return sql.LevelReadCommitted, nil
	case uow.RepeatableRead:
		return sql.LevelRepeatableRead, nil
	case uow.Serializable:
		return sql.LevelSerializable, nil
	default:
		return sql.LevelDefault, fmt.Errorf("%w: %d", database.ErrIsolationUnsupported, level)
	}
}

// readOnly reports whether a read-only transaction should be requested.
// MySQL enforces READ ONLY transactions at the engine level.
func readOnly(mode uow.AccessMode) bool {
	return mode == uow.ReadOnly
}

// commitOutcome classifies a failed MySQL COMMIT. The driver cannot prove
// from a wire failure whether the COMMIT reached the server, so every
// driver-level failure stays Unknown; only database/sql proof (context
// ended first, transaction already done) maps to RolledBack in the shared
// session.
func commitOutcome(error) uow.Outcome { return uow.Unknown }

var _ database.Resource = (*Resource)(nil)
