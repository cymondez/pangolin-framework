// Package organization owns the optional Enterprise/Organization capability.
// Store implements the PostgreSQL authority and transactional Management Port.
package organization

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/cymondez/pangolin-framework/kernel/di"
	"github.com/cymondez/pangolin-framework/kernel/module"
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// Authority reads a consistent, trusted membership/governance snapshot.
// It is infrastructure composition, never populated from request DTOs.
type Authority interface {
	Resolve(context.Context, scope.PrincipalRef, scope.ScopeRef) (scope.ScopeBinding, error)
	Authorize(context.Context, scope.ScopeBinding, string) error
}
type Provider struct{ authority Authority }

func NewProvider(authority Authority) (*Provider, error) {
	if authority == nil {
		return nil, scope.ErrInvalid
	}
	return &Provider{authority}, nil
}
func (p *Provider) Kinds() []string {
	return []string{contracts.EnterpriseKind, contracts.OrganizationKind}
}
func (p *Provider) Resolve(ctx context.Context, principal scope.PrincipalRef, ref scope.ScopeRef) (scope.ScopeBinding, error) {
	if scope.Validate(ref) != nil || (ref.Kind != contracts.EnterpriseKind && ref.Kind != contracts.OrganizationKind) {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	if !validOrganizationID(ref.ID) {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	return p.authority.Resolve(ctx, principal, ref)
}
func validOrganizationID(id string) bool {
	compact := strings.ReplaceAll(id, "-", "")
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' || len(compact) != 32 {
		return false
	}
	_, err := hex.DecodeString(compact)
	return err == nil
}
func (p *Provider) Authorize(ctx context.Context, b scope.ScopeBinding, permission string) error {
	if permission == "" {
		return scope.ErrDenied
	}
	return p.authority.Authorize(ctx, b, permission)
}

// NewModule registers both kinds together; no standalone Enterprise module exists.
// A deployment chooses this module or its replacement, never two authorities.
func NewModule(identity scope.IdentityReader, authority Authority) (module.Module, error) {
	provider, err := NewProvider(authority)
	if err != nil {
		return module.Module{}, err
	}
	registry, err := scope.NewRegistry(identity, scope.TenantScopeProvider{Identity: identity}, provider)
	if err != nil {
		return module.Module{}, err
	}
	return module.Module{Name: "organization", Version: "0.1.0", RequiredScopeKinds: provider.Kinds(), Configure: func(b module.Builder) error {
		return b.Provide(di.Provider{Key: scope.RegistryKey, Lifetime: di.Singleton, Factory: func(context.Context, di.Resolver) (any, di.Cleanup, error) { return registry, nil, nil }})
	}}, nil
}
