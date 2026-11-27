package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/pangolin-framework/pangolin/persistence/database"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
)

func TestCapabilitiesAreTruthful(t *testing.T) {
	c := Capabilities()
	if c.Backend != "sqlite" {
		t.Fatalf("backend wrong: %s", c.Backend)
	}
	if c.RowLevelSecurity {
		t.Fatal("sqlite cannot promise row-level security")
	}
	if c.TransactionReadOnly {
		t.Fatal("sqlite cannot promise engine read-only transactions per operation")
	}
	for _, level := range []uow.Isolation{uow.IsolationDefault, uow.Serializable} {
		if !c.Supports(level) {
			t.Fatalf("supported level missing: %d", level)
		}
	}
	if c.Supports(uow.ReadCommitted) || c.Supports(uow.RepeatableRead) {
		t.Fatal("sqlite must not claim the levels it cannot enforce")
	}
	// Snapshots are detached: mutating a returned slice must not rewrite
	// the admission contract of the next snapshot.
	c.Isolations[0] = uow.Isolation(99)
	if !Capabilities().Supports(uow.Serializable) {
		t.Fatal("capability snapshot is not detached")
	}
	// A caller requiring RLS fails closed on this adapter.
	if err := database.Admit(c, database.Requirements{RowLevelSecurity: true}); !errors.Is(err, database.ErrCapabilityRequired) {
		t.Fatalf("RLS requirement accepted: %v", err)
	}
}

func TestOpenValidation(t *testing.T) {
	if _, err := Open(Config{}); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("empty path accepted: %v", err)
	}
	// A shared-cache memory database admits a bounded pool of any size:
	// concurrent RequiresNew transactions each hold their own connection.
	r, err := Open(Config{Path: "memory-db", Memory: true, MaxOpen: 2})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pool().Stats().MaxOpenConnections != 2 {
		t.Fatal("memory pool not bounded to the configured size")
	}
	_ = r.Pool().Close()
	// Without an explicit bound the caller owns the pool size.
	unbounded, err := Open(Config{Path: "memory-db-2", Memory: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = unbounded.Pool().Close()
}

func TestIsolationAndReadOnlyPolicies(t *testing.T) {
	for _, level := range []uow.Isolation{uow.IsolationDefault, uow.Serializable} {
		if _, err := isolation(level); err != nil {
			t.Fatalf("admitted level rejected: %d", level)
		}
	}
	for _, level := range []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Isolation(99)} {
		if _, err := isolation(level); !errors.Is(err, database.ErrIsolationUnsupported) {
			t.Fatalf("unsupported level admitted: %d", level)
		}
	}
	for _, mode := range []uow.AccessMode{uow.ReadOnly, uow.ReadWrite} {
		if readOnly(mode) {
			t.Fatalf("sqlite must not request engine read-only transactions: %d", mode)
		}
	}
	if commitOutcome(errors.New("anything")) != uow.Unknown {
		t.Fatal("sqlite commit failures must stay unknown")
	}
}

func TestRealSQLiteAdapterSmoke(t *testing.T) {
	r, err := Open(Config{Path: t.TempDir() + "\\adapter.db", MaxOpen: 2, Key: "sqlite-contract"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Pool().Close() }()
	if r.DatabaseKey() != "sqlite-contract" {
		t.Fatalf("key wrong: %s", r.DatabaseKey())
	}
	if r.RequiresTransaction() {
		t.Fatal("sqlite does not require transactions for reads")
	}
	if r.Dialect().Placeholder(1) != "?" {
		t.Fatal("dialect wrong")
	}
	// The resource reports the same truthful capabilities as the package.
	rc, want := r.Capabilities(), Capabilities()
	if rc.Backend != want.Backend || rc.RowLevelSecurity != want.RowLevelSecurity || rc.TransactionReadOnly != want.TransactionReadOnly || !rc.Supports(uow.Serializable) {
		t.Fatalf("resource capabilities differ from the package contract: %+v", rc)
	}
	if _, err := r.Pool().Exec("CREATE TABLE smoke(id TEXT PRIMARY KEY, value INTEGER NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	m, err := uow.NewManager(r, uow.DefaultOptions{DefaultTimeout: 3 * time.Second, CleanupTimeout: time.Second, HookTimeout: 50 * time.Millisecond, MaxRequiresNewDepth: 2, DefaultIsolation: uow.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	resolver := scope.NewTenantResolver(func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant-a", true
	})
	ctx, err := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite}, func(c context.Context) error {
		ex, err := database.Current(c)
		if err != nil {
			return err
		}
		res, err := ex.Exec(c, "INSERT INTO smoke(id,value) VALUES(?,?)", "a", 1)
		if err != nil {
			return err
		}
		if res.RowsAffected != 1 {
			t.Fatalf("rows affected wrong: %d", res.RowsAffected)
		}
		var v int
		if err := ex.QueryRow(c, "SELECT value FROM smoke WHERE id = ?", "a").Scan(&v); err != nil || v != 1 {
			return errors.Join(err, errors.New("value wrong"))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// An explicit unsupported isolation must not be silently downgraded.
	err = m.Within(ctx, uow.Options{Mode: uow.ReadWrite, Isolation: uow.ReadCommitted}, func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("unsupported isolation silently accepted")
	}
}

func TestWrapRequiresValidPool(t *testing.T) {
	if _, err := Wrap(nil, "key"); !errors.Is(err, uow.ErrOptionsInvalid) {
		t.Fatalf("nil pool accepted: %v", err)
	}
	db, err := sql.Open("sqlite", t.TempDir()+"\\wrap.db")
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

// TestForeignKeysOnEveryPooledConnection proves the adapter enables the
// foreign_keys PRAGMA on every connection the pool opens, not only the
// first, and that a composite-owner foreign key really rejects rows whose
// owner parts do not match a parent row.
func TestForeignKeysOnEveryPooledConnection(t *testing.T) {
	r, err := Open(Config{Path: t.TempDir() + "\\fk.db", MaxOpen: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Pool().Close() }()
	ctx := context.Background()
	if _, err := r.Pool().ExecContext(ctx, "CREATE TABLE owner_parent(tenant_id TEXT NOT NULL, scope_kind TEXT NOT NULL, scope_id TEXT NOT NULL, id TEXT NOT NULL, PRIMARY KEY(tenant_id, scope_kind, scope_id, id))"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Pool().ExecContext(ctx, "CREATE TABLE owner_child(tenant_id TEXT NOT NULL, scope_kind TEXT NOT NULL, scope_id TEXT NOT NULL, id TEXT NOT NULL, value INTEGER NOT NULL, PRIMARY KEY(tenant_id, scope_kind, scope_id, id), FOREIGN KEY(tenant_id, scope_kind, scope_id, id) REFERENCES owner_parent(tenant_id, scope_kind, scope_id, id))"); err != nil {
		t.Fatal(err)
	}
	// One parent row per pooled connection so each can commit its own
	// valid child row without colliding on the composite primary key.
	parent, err := r.Pool().PrepareContext(ctx, "INSERT INTO owner_parent VALUES('tenant-a','tenant','tenant-a',?)")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	for i := range 2 {
		if _, err := parent.ExecContext(ctx, "parent-"+fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	// Hold two connections of the bounded pool simultaneously; each must
	// enforce the composite-owner foreign key. A row for another owner
	// with no matching parent must be rejected on every connection.
	conns := make([]*sql.Conn, 2)
	for i := range conns {
		c, err := r.Pool().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conns[i] = c
	}
	for i, c := range conns {
		tx, err := c.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO owner_child VALUES('tenant-other','tenant','tenant-other','parent-1',1)")
		_ = tx.Rollback()
		if err == nil {
			t.Fatalf("connection %d accepted a composite-owner foreign key violation", i)
		}
		// A matching owner is accepted on the same connection.
		tx2, err := c.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx2.ExecContext(ctx, "INSERT INTO owner_child VALUES('tenant-a','tenant','tenant-a',?,?)", "parent-"+fmt.Sprint(i), i)
		if cerr := tx2.Commit(); cerr != nil {
			t.Fatal(cerr)
		}
		if err != nil {
			t.Fatalf("connection %d rejected a valid owner row: %v", i, err)
		}
	}
}
