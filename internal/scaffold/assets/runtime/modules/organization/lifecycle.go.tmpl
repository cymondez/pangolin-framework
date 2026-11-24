package organization

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
)

// JoinAsOwner is an explicit audited governance operation, never an implicit
// consequence of Enterprise ownership. It preserves the Org business owner ID.
func (s *Store) JoinAsOwner(ctx context.Context, c contracts.Command) error {
	if !commandValid(c) || c.Target.Kind != contracts.OrganizationKind {
		return scope.ErrInvalid
	}
	_, err := s.run(ctx, c.Target.TenantID, "enterprise.organization.join-as-owner", c.IdempotencyKey, c, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if err := lockTarget(work, c); err != nil {
			return nil, err
		}
		if err := s.activeTarget(work, c.Target); err != nil {
			return nil, err
		}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		var parent *string
		if err = ex.QueryRow(work, `SELECT enterprise_id FROM pangolin_organization.organizations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, c.Target.TenantID, c.Target.ID).Scan(&parent); err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, scope.ErrDenied
		}
		ref := scope.ScopeRef{TenantID: c.Target.TenantID, Kind: contracts.EnterpriseKind, ID: *parent}
		snapshot, err := s.snapshot(work, b.Principal, ref)
		if err != nil {
			return nil, err
		}
		if snapshot.Role != "owner" {
			return nil, scope.ErrDenied
		}
		if err = s.targetPermission(work, b, ref, "enterprise.organization.join-as-owner"); err != nil {
			return nil, err
		}
		if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.organization_memberships(tenant_id,organization_id,principal_kind,principal_id,role) VALUES($1,$2,$3,$4,'owner') ON CONFLICT(tenant_id,organization_id,principal_kind,principal_id) DO UPDATE SET role='owner',state='active',version=pangolin_organization.organization_memberships.version+1`, c.Target.TenantID, c.Target.ID, b.Principal.Kind, b.Principal.ID); err != nil {
			return nil, err
		}
		if err = owner(work, c.Target, b.Principal, true); err != nil {
			return nil, err
		}
		version, err := bump(work, c.Target)
		if err != nil {
			return nil, err
		}
		return struct{}{}, s.emit(work, b, c.Target, "organization.membership.changed", version, c.Target)
	})
	return err
}
func (s *Store) activeTarget(ctx context.Context, ref scope.ScopeRef) error {
	ex, err := executor(ctx, false)
	if err != nil {
		return err
	}
	var active bool
	err = ex.QueryRow(ctx, `SELECT c.state='active' AND o.state='active' AND EXISTS(SELECT 1 FROM pangolin_organization.owners own JOIN pangolin_security.tenant_memberships tm ON tm.tenant_id=own.tenant_id AND tm.principal_kind=own.principal_kind AND tm.principal_id=own.principal_id WHERE own.tenant_id=c.tenant_id AND own.scope_kind=c.scope_kind AND own.scope_id=c.scope_id AND own.state='active' AND tm.state='active') FROM pangolin.resource_scopes c JOIN pangolin_organization.organizations o ON o.tenant_id=c.tenant_id AND o.id=c.scope_id WHERE c.tenant_id=$1 AND c.scope_kind=$2 AND c.scope_id=$3`, ref.TenantID, ref.Kind, ref.ID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !active {
		return scope.ErrDenied
	}
	return nil
}

// Recover establishes a new effective owner after Tenant revocation. It requires
// dedicated Tenant recovery permission and records Audit/Outbox atomically.
func (s *Store) Recover(ctx context.Context, c contracts.Command, p scope.PrincipalRef) error {
	if !commandValid(c) || !validPrincipal(p) {
		return scope.ErrInvalid
	}
	_, err := s.run(ctx, c.Target.TenantID, "tenant.scope.recover", c.IdempotencyKey, recoveryInput{c, p}, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if err := s.tenantPermission(work, b, "tenant.scope.recover"); err != nil {
			return nil, err
		}
		if err := lockTarget(work, c); err != nil {
			return nil, err
		}
		if _, err := s.options.Tenants.Check(work, p, c.Target.TenantID); err != nil {
			return nil, err
		}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		var state string
		if err = ex.QueryRow(work, `SELECT state FROM pangolin.resource_scopes WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3`, c.Target.TenantID, c.Target.Kind, c.Target.ID).Scan(&state); err != nil {
			return nil, err
		}
		if state == "archived" {
			return nil, ErrConflict
		}
		table, column := "organization_memberships", "organization_id"
		if c.Target.Kind == contracts.EnterpriseKind {
			table, column = "enterprise_roles", "enterprise_id"
		}
		if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.`+table+`(tenant_id,`+column+`,principal_kind,principal_id,role) VALUES($1,$2,$3,$4,'owner') ON CONFLICT(tenant_id,`+column+`,principal_kind,principal_id) DO UPDATE SET role='owner',state='active',version=pangolin_organization.`+table+`.version+1`, c.Target.TenantID, c.Target.ID, p.Kind, p.ID); err != nil {
			return nil, err
		}
		if err = owner(work, c.Target, p, true); err != nil {
			return nil, err
		}
		if _, err = ex.Exec(work, `UPDATE pangolin.resource_scopes SET state='active' WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3 AND state<>'archived'`, c.Target.TenantID, c.Target.Kind, c.Target.ID); err != nil {
			return nil, err
		}
		scopeTable := "organizations"
		if c.Target.Kind == contracts.EnterpriseKind {
			scopeTable = "enterprises"
		}
		if _, err = ex.Exec(work, `UPDATE pangolin_organization.`+scopeTable+` SET state='active' WHERE tenant_id=$1 AND id=$2`, c.Target.TenantID, c.Target.ID); err != nil {
			return nil, err
		}
		version, err := bump(work, c.Target)
		if err != nil {
			return nil, err
		}
		return struct{}{}, s.emit(work, b, c.Target, "scope.active", version, p)
	})
	return err
}

type recoveryInput struct {
	Command   contracts.Command
	Principal scope.PrincipalRef
}

// Bundle is a versioned logical export, independent of private SQL table names.
// Deployment exports after Drain, imports through the replacement's adapter, and
// reconciles the neutral catalog plus pending event/job owner references.
type Bundle struct {
	Version       int                      `json:"version"`
	Enterprises   []contracts.Enterprise   `json:"enterprises"`
	Organizations []contracts.Organization `json:"organizations"`
	Memberships   []contracts.Membership   `json:"memberships"`
	Roles         []RoleRecord             `json:"roles"`
	Grants        []GrantRecord            `json:"grants"`
	Restrictions  []Restriction            `json:"restrictions"`
	Catalog       []CatalogRecord          `json:"catalog"`
	Owners        []OwnerRecord            `json:"owners"`
}
type CatalogRecord struct {
	Ref     scope.ScopeRef
	State   string
	Version int64
}
type OwnerRecord struct {
	Ref       scope.ScopeRef
	Principal scope.PrincipalRef
	State     string
}
type RoleRecord struct {
	Ref         scope.ScopeRef
	Principal   scope.PrincipalRef
	Role, State string
	Version     int64
}
type GrantRecord = RoleRecord
type Restriction struct {
	Ref    scope.ScopeRef
	Action string
}

// EncodeBundle validates the stable wire version; transfer tooling owns signing,
// access control and storage. Neither payload nor files grant runtime authority.
func EncodeBundle(bundle Bundle) ([]byte, error) {
	if bundle.Version != 1 {
		return nil, scope.ErrInvalid
	}
	return json.Marshal(bundle)
}
