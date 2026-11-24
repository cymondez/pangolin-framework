package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

func postgresCapabilities() Capabilities {
	return Capabilities{
		Backend:             "postgres",
		RowLevelSecurity:    true,
		TransactionReadOnly: true,
		Isolations:          []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Serializable},
	}
}
func mysqlCapabilities() Capabilities {
	return Capabilities{
		Backend:             "mysql",
		RowLevelSecurity:    false,
		TransactionReadOnly: true,
		Isolations:          []uow.Isolation{uow.ReadCommitted, uow.RepeatableRead, uow.Serializable},
	}
}
func sqliteCapabilitiesForAdmission() Capabilities {
	return Capabilities{
		Backend:             "sqlite",
		RowLevelSecurity:    false,
		TransactionReadOnly: false,
		Isolations:          []uow.Isolation{uow.IsolationDefault, uow.Serializable},
	}
}

func TestAdmissionFailClosed(t *testing.T) {
	// A caller requiring RLS passes on PostgreSQL.
	if err := Admit(postgresCapabilities(), Requirements{RowLevelSecurity: true}); err != nil {
		t.Fatal(err)
	}
	// The same requirement fails closed on MySQL and SQLite: no silent
	// promise of equivalent shared-schema isolation.
	for name, caps := range map[string]Capabilities{"mysql": mysqlCapabilities(), "sqlite": sqliteCapabilitiesForAdmission()} {
		err := Admit(caps, Requirements{RowLevelSecurity: true})
		if !errors.Is(err, ErrCapabilityRequired) {
			t.Fatalf("RLS requirement accepted on %s: %v", name, err)
		}
	}
	// Engine read-only transactions fail closed on SQLite.
	err := Admit(sqliteCapabilitiesForAdmission(), Requirements{TransactionReadOnly: true})
	if !errors.Is(err, ErrCapabilityRequired) {
		t.Fatalf("read-only transaction accepted on sqlite: %v", err)
	}
	if s := err.Error(); !strings.Contains(s, "transaction-read-only") {
		t.Fatalf("missing capability not named: %s", s)
	}
	// An isolation level the backend cannot enforce fails closed.
	err = Admit(sqliteCapabilitiesForAdmission(), Requirements{Isolation: uow.ReadCommitted})
	if !errors.Is(err, ErrCapabilityRequired) {
		t.Fatalf("unsupported isolation accepted: %v", err)
	}
	if s := err.Error(); !strings.Contains(s, "read-committed") {
		t.Fatalf("isolation not named: %s", s)
	}
	// Supported combinations pass.
	if err := Admit(mysqlCapabilities(), Requirements{Isolation: uow.RepeatableRead}); err != nil {
		t.Fatal(err)
	}
	if err := Admit(sqliteCapabilitiesForAdmission(), Requirements{}); err != nil {
		t.Fatal(err)
	}
	// Multiple missing capabilities are all reported.
	err = Admit(sqliteCapabilitiesForAdmission(), Requirements{RowLevelSecurity: true, TransactionReadOnly: true})
	if err == nil || !strings.Contains(err.Error(), "row-level-security") || !strings.Contains(err.Error(), "transaction-read-only") {
		t.Fatalf("partial capability report: %v", err)
	}
}

func TestCapabilitySupports(t *testing.T) {
	if postgresCapabilities().Supports(uow.Serializable) != true {
		t.Fatal("postgres serializable unsupported")
	}
	if postgresCapabilities().Supports(uow.Isolation(99)) {
		t.Fatal("unknown level supported")
	}
	if !sqliteCapabilitiesForAdmission().Supports(uow.Serializable) {
		t.Fatal("sqlite default unsupported")
	}
}

func TestIsolationNameCoverage(t *testing.T) {
	// Every admitted isolation level is reported by name in the failure
	// message, so operators see exactly which requirement failed closed.
	for level, want := range map[uow.Isolation]string{
		uow.ReadCommitted:    "read-committed",
		uow.RepeatableRead:   "repeatable-read",
		uow.Serializable:     "serializable",
		uow.IsolationDefault: "default",
	} {
		err := Admit(Capabilities{Backend: "none", Isolations: nil}, Requirements{Isolation: level})
		if level == uow.IsolationDefault {
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("level %d not named in %v", level, err)
		}
	}
}

func TestDialects(t *testing.T) {
	if DollarDialect.Placeholder(3) != "$3" {
		t.Fatal("dollar placeholder wrong")
	}
	if QuestionDialect.Placeholder(7) != "?" {
		t.Fatal("question placeholder wrong")
	}
	if id := DollarDialect.Identifier("schema", "table"); id != `"schema"."table"` {
		t.Fatalf("dollar identifier wrong: %s", id)
	}
	if id := QuestionDialect.Identifier("sc`hema", "table"); id != "`sc``hema`.`table`" {
		t.Fatalf("question identifier wrong: %s", id)
	}
}

func TestOwnerBinding(t *testing.T) {
	// Outside an authorized operation there is no owner at all.
	if _, err := CurrentOwner(context.Background()); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("unbound owner accepted: %v", err)
	}
	// ResolveOwner validates a hint against the trusted binding.
	ctx := trustedContext(t, "tenant-a")
	o, err := CurrentOwner(ctx)
	if err != nil || o != (Owner{TenantID: "tenant-a", ScopeKind: "tenant", ScopeID: "tenant-a"}) {
		t.Fatalf("trusted owner wrong: %+v %v", o, err)
	}
	if o.Ref() != (scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"}) {
		t.Fatalf("owner ref wrong: %+v", o.Ref())
	}
	// An empty hint adopts the trusted owner.
	adopted, err := ResolveOwner(ctx, scope.ScopeRef{})
	if err != nil || adopted != o {
		t.Fatalf("empty hint wrong: %+v %v", adopted, err)
	}
	// A matching hint is allowed.
	same, err := ResolveOwner(ctx, scope.ScopeRef{TenantID: "tenant-a", Kind: "tenant", ID: "tenant-a"})
	if err != nil || same != o {
		t.Fatalf("matching hint wrong: %+v %v", same, err)
	}
	// A foreign hint is denied, never substituted.
	if _, err := ResolveOwner(ctx, scope.ScopeRef{TenantID: "tenant-b", Kind: "tenant", ID: "tenant-b"}); !errors.Is(err, scope.ErrDenied) {
		t.Fatalf("foreign hint accepted: %v", err)
	}
}

func TestOwnerPredicateRendersExactOwnerClause(t *testing.T) {
	ctx := trustedContext(t, "tenant-a")
	o, err := CurrentOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The production predicate helper renders the full three-part owner
	// comparison with dialect placeholders and binds the trusted values in
	// order; callers concatenate it into their own statements.
	for _, tc := range []struct {
		name    string
		dialect Dialect
		alias   string
		want    string
	}{
		{"dollar", DollarDialect, "", `"tenant_id" = $1 AND "scope_kind" = $2 AND "scope_id" = $3`},
		{"dollar-alias", DollarDialect, "r", `"r"."tenant_id" = $1 AND "r"."scope_kind" = $2 AND "r"."scope_id" = $3`},
		{"question", QuestionDialect, "", "`tenant_id` = ? AND `scope_kind` = ? AND `scope_id` = ?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clause, args := OwnerPredicate(tc.dialect, tc.alias, o)
			if clause != tc.want {
				t.Fatalf("clause wrong: %s", clause)
			}
			if len(args) != 3 || args[0] != "tenant-a" || args[1] != "tenant" || args[2] != "tenant-a" {
				t.Fatalf("owner args wrong: %v", args)
			}
		})
	}
}
