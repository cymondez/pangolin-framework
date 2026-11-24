package database

import (
	"context"
	"strings"

	"github.com/cymondez/pangolin-framework/security/scope"
)

// Owner is the exact trusted owner of scoped rows: tenant, scope kind and
// scope id. All three parts come from the trusted scope binding; a value
// object or DTO never supplies them.
type Owner struct {
	TenantID  string
	ScopeKind string
	ScopeID   string
}

// Ref returns the scope reference of this owner.
func (o Owner) Ref() scope.ScopeRef {
	return scope.ScopeRef{TenantID: o.TenantID, Kind: o.ScopeKind, ID: o.ScopeID}
}

// CurrentOwner binds the owner from the trusted scope binding only. It fails
// outside an authorized operation; a missing or invalid binding yields no
// owner.
func CurrentOwner(ctx context.Context) (Owner, error) {
	b, ok := scope.Current(ctx)
	if !ok {
		return Owner{}, scope.ErrDenied
	}
	if err := scope.Validate(b.Ref); err != nil {
		return Owner{}, err
	}
	return Owner{TenantID: b.Ref.TenantID, ScopeKind: b.Ref.Kind, ScopeID: b.Ref.ID}, nil
}

// ResolveOwner validates an untrusted, DTO-supplied owner hint against the
// trusted binding. An empty hint adopts the binding; any other owner is
// rejected, never substituted. DTO owner substitution is therefore a
// permission denial, not a way to read another owner's rows.
func ResolveOwner(ctx context.Context, requested scope.ScopeRef) (Owner, error) {
	o, err := CurrentOwner(ctx)
	if err != nil {
		return Owner{}, err
	}
	if err := scope.MatchOwner(ctx, requested); err != nil {
		return Owner{}, err
	}
	return o, nil
}

// OwnerPredicate renders the exact owner predicate "<alias>.tenant_id = ?
// AND <alias>.scope_kind = ? AND <alias>.scope_id = ?" for the given dialect
// and returns the bound arguments. The rendered placeholders always start at
// index 1; the clause is not offset for values a statement already binds, so
// a caller must put the owner clause first, pass the returned arguments
// before its own and number any further marker with
// Placeholder(len(args)+1). All three owner parts come from the trusted
// context, so a scoped statement can never run unscoped. Callers concatenate
// the clause into their own statement; the returned arguments must be passed
// as statement arguments in order.
func OwnerPredicate(d Dialect, alias string, o Owner) (clause string, args []any) {
	var sql strings.Builder
	prefix := ""
	if alias != "" {
		prefix = d.Identifier(alias) + "."
	}
	args = make([]any, 0, 3)
	for i, part := range []struct {
		column string
		value  string
	}{{"tenant_id", o.TenantID}, {"scope_kind", o.ScopeKind}, {"scope_id", o.ScopeID}} {
		if i > 0 {
			sql.WriteString(" AND ")
		}
		sql.WriteString(prefix)
		sql.WriteString(d.Identifier(part.column))
		sql.WriteString(" = ")
		sql.WriteString(d.Placeholder(len(args) + 1))
		args = append(args, part.value)
	}
	return sql.String(), args
}
