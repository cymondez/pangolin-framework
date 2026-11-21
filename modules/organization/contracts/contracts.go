// Package contracts defines replaceable organization management ports.
// The organization module implementation is delivered in M2/M3, not M0.
package contracts

import (
	"context"

	"github.com/cymondez/pangolin-framework/security/scope"
)

const (
	EnterpriseKind   = "org.enterprise"
	OrganizationKind = "org.organization"
)

type PageRequest struct {
	Cursor string
	Limit  int
}
type Enterprise struct {
	Ref     scope.ScopeRef
	Slug    string
	State   string
	Version int64
}
type Organization struct {
	Ref          scope.ScopeRef
	EnterpriseID *string
	Slug         string
	State        string
	Version      int64
}
type Membership struct {
	TenantID       string
	OrganizationID string
	Principal      scope.PrincipalRef
	Role           string
	State          string
	Version        int64
}
type OrganizationPage struct {
	Items      []Organization
	NextCursor string
}
type Command struct {
	Target          scope.ScopeRef
	ExpectedVersion int64
	IdempotencyKey  string
}
type CreateEnterprise struct {
	TenantID       string
	Slug           string
	IdempotencyKey string
}
type CreateOrganization struct {
	TenantID       string
	EnterpriseID   *string
	Slug           string
	IdempotencyKey string
}
type SetMembership struct {
	Command
	Principal scope.PrincipalRef
	Role      string
}
type RemoveMembership struct {
	Command
	Principal scope.PrincipalRef
}
type SetEnterpriseRole struct {
	Command
	Principal scope.PrincipalRef
	Role      string
}
type AssignEnterprise struct {
	Command
	EnterpriseID *string
}

type Management interface {
	CreateEnterprise(context.Context, CreateEnterprise) (Enterprise, error)
	CreateOrganization(context.Context, CreateOrganization) (Organization, error)
	ListMyOrganizations(context.Context, PageRequest) (OrganizationPage, error)
	ListEnterpriseOrganizations(context.Context, scope.ScopeRef, PageRequest) (OrganizationPage, error)
	SetOrganizationMembership(context.Context, SetMembership) (Membership, error)
	RemoveOrganizationMembership(context.Context, RemoveMembership) error
	SetEnterpriseRole(context.Context, SetEnterpriseRole) error
	AssignEnterprise(context.Context, AssignEnterprise) (Organization, error)
	Suspend(context.Context, Command) error
	Resume(context.Context, Command) error
	Archive(context.Context, Command) error
}
