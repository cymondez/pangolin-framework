// Package sqlite provides the real SQLite adapter on the neutral database
// Port. It uses modernc.org/sqlite (pure Go, CGo-free) and the shared guarded
// database/sql session. SQLite has no engine row-level security and no
// engine-enforced read-only transactions usable per operation; capabilities
// report that truthfully.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/cymondez/pangolin-framework/persistence/database"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	_ "modernc.org/sqlite"
)

// Capabilities is the truthful SQLite contract: no row-level security and
// no transaction-scoped read-only enforcement (the query_only PRAGMA is
// connection-scoped and would poison pooled connections). SQLite always
// runs serializable internally; the neutral Port admits only the default
// level so an explicit request is never silently downgraded.
func Capabilities() database.Capabilities {
	return database.Capabilities{
		Backend:             "sqlite",
		RowLevelSecurity:    false,
		TransactionReadOnly: false,
		Isolations:          []uow.Isolation{uow.IsolationDefault, uow.Serializable},
	}
}

// Config declares the SQLite database and pool bounds. Path is a plain file
// path or a driver DSN including parameters. MaxOpen bounds the pool; a
// memory database still needs a bounded pool large enough for any RequiresNew
// nesting, because every concurrent transaction holds its own connection.
// Memory selects a shared-cache in-memory database named after Path.
type Config struct {
	Path    string
	MaxOpen int
	// Key overrides the resource identity.
	Key string
	// Memory selects an in-memory database; Path is used as its name.
	Memory bool
}

// Resource is a real SQLite database resource on the neutral Port.
type Resource struct {
	resource *database.SQLResource
}

// withForeignKeys appends the foreign_keys pragma so every connection the
// pool ever opens enforces referential integrity. A DSN parameter applies
// per connection, which plain Exec on the first connection cannot provide.
// The pragma is appended after the caller's parameters on purpose: the
// adapter guarantees composite-owner referential integrity on all pooled
// connections, so a caller cannot silently weaken it.
func withForeignKeys(dsn string) string {
	suffix := "?_pragma=foreign_keys(1)"
	if strings.Contains(dsn, "?") {
		suffix = "&_pragma=foreign_keys(1)"
	}
	return dsn + suffix
}

// Open validates the configuration and opens the real pool.
func Open(c Config) (*Resource, error) {
	if c.Path == "" {
		return nil, uow.ErrOptionsInvalid
	}
	dsn := c.Path
	if c.Memory {
		dsn = "file:" + url.PathEscape(c.Path) + "?mode=memory&cache=shared"
	}
	dsn = withForeignKeys(dsn)
	opened, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if c.MaxOpen > 0 {
		opened.SetMaxOpenConns(c.MaxOpen)
	}
	inner, err := database.NewSQLResource(opened, database.SQLConfig{
		Backend:       "sqlite",
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

func (r *Resource) Begin(ctx context.Context, o uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return r.resource.Begin(ctx, o)
}
func (r *Resource) DatabaseKey() string                 { return r.resource.DatabaseKey() }
func (r *Resource) RequiresTransaction() bool           { return r.resource.RequiresTransaction() }
func (r *Resource) Capabilities() database.Capabilities { return r.resource.Capabilities() }
func (r *Resource) Dialect() database.Dialect           { return r.resource.Dialect() }

// Pool exposes the underlying *sql.DB for infrastructure operations that the
// neutral Port does not model (schema setup, backups). Business code uses
// the guarded executor.
func (r *Resource) Pool() *sql.DB { return r.resource.Pool() }

// Wrap adapts an already-opened *sql.DB, for example one assembled by
// another composition step. The caller remains responsible for closing it.
func Wrap(db *sql.DB, key string) (*Resource, error) {
	inner, err := database.NewSQLResource(db, database.SQLConfig{
		Backend:       "sqlite",
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

// isolation maps a requested level to the driver level. SQLite serializes
// internally, but the neutral Port never claims a specific level the
// caller requested; only the default level and the manager's configured
// default (Serializable for this adapter, matching SQLite's own engine
// behavior) are admitted. An explicit different request fails closed.
func isolation(level uow.Isolation) (sql.IsolationLevel, error) {
	switch level {
	case uow.IsolationDefault, uow.Serializable:
		return sql.LevelDefault, nil
	default:
		return sql.LevelDefault, fmt.Errorf("%w: %d", database.ErrIsolationUnsupported, level)
	}
}

// readOnly never requests engine read-only transactions. SQLite cannot
// enforce them per transaction without poisoning pooled connections with
// the connection-scoped query_only PRAGMA.
func readOnly(uow.AccessMode) bool { return false }

// commitOutcome classifies a failed SQLite COMMIT conservatively. The pinned
// modernc.org/sqlite v1.62.0 returns no driver error that proves whether a
// failed COMMIT reached the database file: the commit may have been applied
// before the failure was reported, so without proof the outcome stays
// Unknown. (The transaction is still cleaned up by database/sql once its
// context ends, but cleanup is not proof of the commit outcome.)
func commitOutcome(error) uow.Outcome { return uow.Unknown }

var _ database.Resource = (*Resource)(nil)
