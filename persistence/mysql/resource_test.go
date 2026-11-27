package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/pangolin-framework/pangolin/persistence/database"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/go-sql-driver/mysql"
)

func TestCapabilitiesAreTruthful(t *testing.T) {
	c := Capabilities()
	if c.Backend != "mysql" {
		t.Fatalf("backend wrong: %s", c.Backend)
	}
	// MySQL has no engine row-level security; the adapter must never
	// promise it and a caller requiring it must fail closed.
	if c.RowLevelSecurity {
		t.Fatal("mysql cannot promise row-level security")
	}
	if err := database.Admit(c, database.Requirements{RowLevelSecurity: true}); !errors.Is(err, database.ErrCapabilityRequired) {
		t.Fatalf("RLS requirement accepted: %v", err)
	}
	// MySQL does enforce read-only transactions and isolation levels.
	if !c.TransactionReadOnly {
		t.Fatal("mysql does enforce READ ONLY transactions")
	}
	for _, level := range []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Serializable} {
		if !c.Supports(level) {
			t.Fatalf("supported level missing: %d", level)
		}
	}
	// Snapshots are detached: mutating a returned slice must not rewrite
	// the admission contract of the next snapshot.
	c.Isolations[0] = uow.Isolation(99)
	if !Capabilities().Supports(uow.ReadCommitted) {
		t.Fatal("capability snapshot is not detached")
	}
}

func TestOpenValidation(t *testing.T) {
	if _, err := Open(Config{}); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("empty configuration accepted: %v", err)
	}
	// sql.Open is lazy: a syntactically valid DSN opens without connecting,
	// so configuration assembly can be verified without credentials.
	r, err := Open(Config{DSN: "user:pass@tcp(127.0.0.1:1)/db", MaxOpen: 4, Key: "mysql-contract"})
	if err != nil {
		t.Fatal(err)
	}
	if r.DatabaseKey() != "mysql-contract" {
		t.Fatalf("key wrong: %s", r.DatabaseKey())
	}
	if r.RequiresTransaction() {
		t.Fatal("mysql does not require transactions for reads")
	}
	if r.Dialect().Placeholder(2) != "?" {
		t.Fatal("dialect wrong")
	}
	_ = r.Pool().Close()

	// A parsed configuration is accepted through the connector path.
	cfg := mysql.NewConfig()
	cfg.User = "user"
	cfg.Passwd = "pass"
	cfg.Net = "tcp"
	cfg.Addr = "127.0.0.1:1"
	cfg.DBName = "db"
	r2, err := Open(Config{DSNConfig: cfg})
	if err != nil {
		t.Fatal(err)
	}
	rc, want := r2.Capabilities(), Capabilities()
	if rc.Backend != want.Backend || rc.RowLevelSecurity != want.RowLevelSecurity || rc.TransactionReadOnly != want.TransactionReadOnly || !rc.Supports(uow.RepeatableRead) {
		t.Fatalf("resource capabilities differ from the package contract: %+v", rc)
	}
	_ = r2.Pool().Close()

	// A syntactically invalid DSN fails configuration assembly before any
	// connection is attempted.
	if _, err := Open(Config{DSN: "://bad-dsn"}); err == nil {
		t.Fatal("invalid DSN accepted")
	}
}

func TestIsolationAndReadOnlyPolicies(t *testing.T) {
	for level, want := range map[uow.Isolation]sql.IsolationLevel{
		uow.IsolationDefault: sql.LevelDefault,
		uow.ReadCommitted:    sql.LevelReadCommitted,
		uow.RepeatableRead:   sql.LevelRepeatableRead,
		uow.Serializable:     sql.LevelSerializable,
	} {
		got, err := isolation(level)
		if err != nil || got != want {
			t.Fatalf("isolation mapping wrong: %d -> %d (%v), want %d", level, got, err, want)
		}
	}
	if _, err := isolation(uow.Isolation(99)); !errors.Is(err, database.ErrIsolationUnsupported) {
		t.Fatalf("unknown isolation admitted: %v", err)
	}
	if !readOnly(uow.ReadOnly) {
		t.Fatal("read-only mode must request READ ONLY")
	}
	if readOnly(uow.ReadWrite) {
		t.Fatal("read-write mode must not request READ ONLY")
	}
	if commitOutcome(errors.New("wire cut")) != uow.Unknown {
		t.Fatal("mysql commit failures must stay unknown")
	}
}

func TestWrapRequiresValidPool(t *testing.T) {
	if _, err := Wrap(nil, "key"); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("nil pool accepted: %v", err)
	}
	// Reuse the mysql driver name for an unconnected pool; no server is
	// contacted until a statement runs.
	db, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := Wrap(db, "wrapped")
	if err != nil {
		t.Fatal(err)
	}
	if r.DatabaseKey() != "wrapped" {
		t.Fatalf("wrapped key wrong: %s", r.DatabaseKey())
	}
}

func TestBeginRejectsMissingScope(t *testing.T) {
	r, err := Open(Config{DSN: "user:pass@tcp(127.0.0.1:1)/db", Key: "mysql-begin"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Pool().Close() }()
	// No trusted scope binding: Begin must refuse before touching the
	// network at all.
	if _, _, err := r.Begin(context.Background(), uow.EffectiveOptions{Transactional: true, DatabaseKey: r.DatabaseKey()}); !errors.Is(err, uow.ErrResourceMismatch) {
		t.Fatalf("missing scope accepted: %v", err)
	}
}
