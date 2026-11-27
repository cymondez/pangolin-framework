// Package tenancy resolves untrusted selectors against current tenant authority.
package tenancy

import (
	"context"
	"errors"

	"github.com/cymondez/pangolin-framework/kernel/ambient"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrHintConflict = errors.New("TENANT_HINT_CONFLICT")

// Authority is a current, trusted local membership projection. JWT claims alone
// are never sufficient. A non-empty revision denotes a consistent active grant.
type Authority interface {
	Check(context.Context, scope.PrincipalRef, string) (string, error)
}
type identity struct {
	principal        scope.PrincipalRef
	tenant, revision string
}

var identityVar = ambient.New[identity]()

type Resolver struct {
	identity  scope.IdentityReader
	authority Authority
}

func NewResolver(identity scope.IdentityReader, authority Authority) (*Resolver, error) {
	if identity == nil || authority == nil {
		return nil, scope.ErrInvalid
	}
	return &Resolver{identity, authority}, nil
}
func Identity(ctx context.Context) (scope.PrincipalRef, string, bool) {
	v, ok := identityVar.Current(ctx)
	return v.principal, v.tenant, ok
}

// Resolve verifies route/header candidates and current membership before binding.
func (r *Resolver) Resolve(ctx context.Context, route, header string) (context.Context, error) {
	if ctx == nil {
		return nil, scope.ErrDenied
	}
	p, tenant, ok := r.identity(ctx)
	if !ok || p.ID == "" || (p.Kind != "user" && p.Kind != "service") || tenant == "" {
		return nil, scope.ErrDenied
	}
	if route != "" && header != "" && route != header {
		return nil, ErrHintConflict
	}
	for _, hint := range []string{route, header} {
		if hint != "" && hint != tenant {
			return nil, scope.ErrDenied
		}
	}
	rev, err := r.authority.Check(ctx, p, tenant)
	if err != nil {
		return nil, err
	}
	if rev == "" {
		return nil, scope.ErrAuthorityUnavailable
	}
	return identityVar.With(ctx, identity{p, tenant, rev}), nil
}

// Provider revalidates tenant membership at binding and at root admission.
// Permission is mandatory for actions; it cannot grant another tenant's access.
type Provider struct {
	Authority  Authority
	Permission func(context.Context, scope.ScopeBinding, string) error
}

func (Provider) Kinds() []string { return []string{"tenant"} }
func (p Provider) Resolve(ctx context.Context, principal scope.PrincipalRef, ref scope.ScopeRef) (scope.ScopeBinding, error) {
	actual, tenant, ok := Identity(ctx)
	if !ok || actual != principal || tenant != ref.TenantID || ref.Kind != "tenant" || scope.Validate(ref) != nil || p.Authority == nil {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	rev, err := p.Authority.Check(ctx, principal, tenant)
	if err != nil {
		return scope.ScopeBinding{}, err
	}
	if rev == "" {
		return scope.ScopeBinding{}, scope.ErrAuthorityUnavailable
	}
	return scope.ScopeBinding{Ref: ref, Principal: principal, AuthorityRevision: rev}, nil
}
func (p Provider) Authorize(ctx context.Context, b scope.ScopeBinding, permission string) error {
	if permission == "" || p.Permission == nil {
		return scope.ErrDenied
	}
	actual, tenant, ok := Identity(ctx)
	if !ok || actual != b.Principal || tenant != b.Ref.TenantID {
		return scope.ErrDenied
	}
	current, bound := scope.Current(ctx)
	if !bound || current != b || b.Ref.Kind != "tenant" || scope.Validate(b.Ref) != nil {
		return scope.ErrDenied
	}
	if uow.Check(ctx, false) != nil {
		if latest, err := p.Resolve(ctx, b.Principal, b.Ref); err != nil {
			return err
		} else if latest != b {
			return scope.ErrDenied
		}
	}
	return p.Permission(ctx, b, permission)
}
