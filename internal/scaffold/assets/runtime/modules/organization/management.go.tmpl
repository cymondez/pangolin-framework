package organization

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
)

var _ contracts.Management = (*Store)(nil)

func (s *Store) run(ctx context.Context, tenant, action, key string, input any, fn func(context.Context, scope.ScopeBinding) (any, error)) ([]byte, error) {
	if _, err := s.actor(ctx, tenant); err != nil {
		return nil, err
	}
	if key == "" || len(key) > 128 {
		return nil, scope.ErrInvalid
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var raw []byte
	ctx = context.WithValue(ctx, privateSchemaKey{}, s.options.PrivateSchema)
	err = s.options.Manager.Within(ctx, uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}, func(work context.Context) error {
		b, err := s.actor(work, tenant)
		if err != nil {
			return err
		}
		// Replaying a committed result is a new authorized operation. The
		// callback below is skipped on replay, so action authorization must run
		// before consulting the idempotency record.
		if err = s.authorizeInput(work, b, action, input); err != nil {
			return err
		}
		raw, _, err = s.options.Idempotency.Execute(work, action, key, body, func(c context.Context) ([]byte, error) {
			v, e := fn(c, b)
			if e != nil {
				return nil, e
			}
			return json.Marshal(v)
		})
		return err
	})
	return raw, err
}

func (s *Store) authorizeInput(ctx context.Context, b scope.ScopeBinding, action string, input any) error {
	switch c := input.(type) {
	case contracts.CreateEnterprise:
		return s.tenantPermission(ctx, b, action)
	case contracts.CreateOrganization:
		if c.EnterpriseID == nil {
			return s.tenantPermission(ctx, b, "tenant.organization.create")
		}
		return s.targetPermission(ctx, b, scope.ScopeRef{TenantID: c.TenantID, Kind: contracts.EnterpriseKind, ID: *c.EnterpriseID}, "enterprise.organization.create")
	case contracts.SetMembership:
		return s.targetPermission(ctx, b, c.Target, action)
	case contracts.RemoveMembership:
		return s.targetPermission(ctx, b, c.Target, action)
	case contracts.SetEnterpriseRole:
		return s.targetPermission(ctx, b, c.Target, action)
	case contracts.AssignEnterprise:
		if err := s.targetPermission(ctx, b, c.Target, action); err != nil {
			return err
		}
		if c.EnterpriseID != nil {
			return s.targetPermission(ctx, b, scope.ScopeRef{TenantID: c.Target.TenantID, Kind: contracts.EnterpriseKind, ID: *c.EnterpriseID}, "enterprise.organization.attach")
		}
		return nil
	case recoveryInput:
		return s.tenantPermission(ctx, b, "tenant.scope.recover")
	case contracts.Command:
		if action == "scope.active" {
			return s.tenantPermission(ctx, b, "tenant.scope.recover")
		}
		if action == "enterprise.organization.join-as-owner" {
			if err := s.activeTarget(ctx, c.Target); err != nil {
				return err
			}
			var parent *string
			ex, err := executor(ctx, false)
			if err != nil {
				return err
			}
			if err = ex.QueryRow(ctx, `SELECT enterprise_id FROM pangolin_organization.organizations WHERE tenant_id=$1 AND id=$2`, c.Target.TenantID, c.Target.ID).Scan(&parent); errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			if parent == nil {
				return scope.ErrDenied
			}
			ref := scope.ScopeRef{TenantID: c.Target.TenantID, Kind: contracts.EnterpriseKind, ID: *parent}
			snapshot, err := s.snapshot(ctx, b.Principal, ref)
			if err != nil {
				return err
			}
			if snapshot.Role != "owner" {
				return scope.ErrDenied
			}
			return s.targetPermission(ctx, b, ref, action)
		}
		return s.targetPermission(ctx, b, c.Target, action)
	default:
		return scope.ErrDenied
	}
}
func lockTarget(ctx context.Context, c contracts.Command) error {
	if !commandValid(c) {
		return scope.ErrInvalid
	}
	ex, err := executor(ctx, true)
	if err != nil {
		return err
	}
	var version int64
	err = ex.QueryRow(ctx, `SELECT version FROM pangolin.resource_scopes WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3 FOR UPDATE`, c.Target.TenantID, c.Target.Kind, c.Target.ID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if version != c.ExpectedVersion {
		return ErrConflict
	}
	return nil
}
func bump(ctx context.Context, target scope.ScopeRef) (int64, error) {
	ex, err := executor(ctx, true)
	if err != nil {
		return 0, err
	}
	var version int64
	err = ex.QueryRow(ctx, `UPDATE pangolin.resource_scopes SET version=version+1 WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3 RETURNING version`, target.TenantID, target.Kind, target.ID).Scan(&version)
	if err != nil {
		return 0, err
	}
	table := "organizations"
	if target.Kind == contracts.EnterpriseKind {
		table = "enterprises"
	}
	_, err = ex.Exec(ctx, `UPDATE pangolin_organization.`+table+` SET version=$3 WHERE tenant_id=$1 AND id=$2`, target.TenantID, target.ID, version)
	return version, err
}
func owner(ctx context.Context, target scope.ScopeRef, p scope.PrincipalRef, active bool) error {
	state := "revoked"
	if active {
		state = "active"
	}
	ex, err := executor(ctx, true)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `INSERT INTO pangolin_organization.owners(tenant_id,scope_kind,scope_id,principal_kind,principal_id,state) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,scope_kind,scope_id,principal_kind,principal_id) DO UPDATE SET state=EXCLUDED.state`, target.TenantID, target.Kind, target.ID, p.Kind, p.ID, state)
	return err
}
func preserveOwner(ctx context.Context, target scope.ScopeRef, p scope.PrincipalRef) error {
	ex, err := executor(ctx, false)
	if err != nil {
		return err
	}
	var owns bool
	var count int
	err = ex.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pangolin_organization.owners WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3 AND principal_kind=$4 AND principal_id=$5 AND state='active'),(SELECT count(*) FROM pangolin_organization.owners o JOIN pangolin_security.tenant_memberships t USING(tenant_id,principal_kind,principal_id) WHERE o.tenant_id=$1 AND o.scope_kind=$2 AND o.scope_id=$3 AND o.state='active' AND t.state='active')`, target.TenantID, target.Kind, target.ID, p.Kind, p.ID).Scan(&owns, &count)
	if err != nil {
		return err
	}
	if owns && count <= 1 {
		return ErrLastOwner
	}
	return nil
}
func (s *Store) CreateEnterprise(ctx context.Context, c contracts.CreateEnterprise) (contracts.Enterprise, error) {
	var result contracts.Enterprise
	if !validSlug(c.Slug) {
		return result, scope.ErrInvalid
	}
	raw, err := s.run(ctx, c.TenantID, "tenant.enterprise.create", c.IdempotencyKey, c, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if err := s.tenantPermission(work, b, "tenant.enterprise.create"); err != nil {
			return nil, err
		}
		ref := scope.ScopeRef{TenantID: c.TenantID, Kind: contracts.EnterpriseKind, ID: newID()}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		if _, err = ex.Exec(work, `INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,$2,$3)`, ref.TenantID, ref.Kind, ref.ID); err != nil {
			return nil, err
		}
		if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.enterprises(tenant_id,id,slug) VALUES($1,$2,$3)`, ref.TenantID, ref.ID, c.Slug); err != nil {
			return nil, err
		}
		if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.enterprise_roles(tenant_id,enterprise_id,principal_kind,principal_id,role) VALUES($1,$2,$3,$4,'owner')`, ref.TenantID, ref.ID, b.Principal.Kind, b.Principal.ID); err != nil {
			return nil, err
		}
		if err = owner(work, ref, b.Principal, true); err != nil {
			return nil, err
		}
		v := contracts.Enterprise{Ref: ref, Slug: c.Slug, State: "active", Version: 1}
		return v, s.emit(work, b, ref, "enterprise.created", 1, v)
	})
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
func (s *Store) CreateOrganization(ctx context.Context, c contracts.CreateOrganization) (contracts.Organization, error) {
	var result contracts.Organization
	if !validSlug(c.Slug) {
		return result, scope.ErrInvalid
	}
	raw, err := s.run(ctx, c.TenantID, "tenant.organization.create", c.IdempotencyKey, c, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if c.EnterpriseID == nil {
			if err := s.tenantPermission(work, b, "tenant.organization.create"); err != nil {
				return nil, err
			}
		} else {
			if err := s.targetPermission(work, b, scope.ScopeRef{TenantID: c.TenantID, Kind: contracts.EnterpriseKind, ID: *c.EnterpriseID}, "enterprise.organization.create"); err != nil {
				return nil, err
			}
		}
		ref := scope.ScopeRef{TenantID: c.TenantID, Kind: contracts.OrganizationKind, ID: newID()}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		if _, err = ex.Exec(work, `INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id) VALUES($1,$2,$3)`, ref.TenantID, ref.Kind, ref.ID); err != nil {
			return nil, err
		}
		if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.organizations(tenant_id,id,slug,enterprise_id) VALUES($1,$2,$3,$4)`, ref.TenantID, ref.ID, c.Slug, c.EnterpriseID); err != nil {
			return nil, err
		}
		if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.organization_memberships(tenant_id,organization_id,principal_kind,principal_id,role) VALUES($1,$2,$3,$4,'owner')`, ref.TenantID, ref.ID, b.Principal.Kind, b.Principal.ID); err != nil {
			return nil, err
		}
		if err = owner(work, ref, b.Principal, true); err != nil {
			return nil, err
		}
		v := contracts.Organization{Ref: ref, EnterpriseID: c.EnterpriseID, Slug: c.Slug, State: "active", Version: 1}
		return v, s.emit(work, b, ref, "organization.created", 1, v)
	})
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
func (s *Store) SetOrganizationMembership(ctx context.Context, c contracts.SetMembership) (contracts.Membership, error) {
	var result contracts.Membership
	if !commandValid(c.Command) || c.Target.Kind != contracts.OrganizationKind || !validPrincipal(c.Principal) || (c.Role != "owner" && c.Role != "member") {
		return result, scope.ErrInvalid
	}
	raw, err := s.run(ctx, c.Target.TenantID, "organization.membership.set", c.IdempotencyKey, c, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if err := lockTarget(work, c.Command); err != nil {
			return nil, err
		}
		if err := s.targetPermission(work, b, c.Target, "organization.membership.set"); err != nil {
			return nil, err
		}
		if _, err := s.options.Tenants.Check(work, c.Principal, c.Target.TenantID); err != nil {
			return nil, err
		}
		if c.Role != "owner" {
			if err := preserveOwner(work, c.Target, c.Principal); err != nil {
				return nil, err
			}
		}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		var version int64
		err = ex.QueryRow(work, `INSERT INTO pangolin_organization.organization_memberships(tenant_id,organization_id,principal_kind,principal_id,role) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,organization_id,principal_kind,principal_id) DO UPDATE SET role=EXCLUDED.role,state='active',version=pangolin_organization.organization_memberships.version+1 RETURNING version`, c.Target.TenantID, c.Target.ID, c.Principal.Kind, c.Principal.ID, c.Role).Scan(&version)
		if err != nil {
			return nil, err
		}
		if err = owner(work, c.Target, c.Principal, c.Role == "owner"); err != nil {
			return nil, err
		}
		revision, err := bump(work, c.Target)
		if err != nil {
			return nil, err
		}
		v := contracts.Membership{TenantID: c.Target.TenantID, OrganizationID: c.Target.ID, Principal: c.Principal, Role: c.Role, State: "active", Version: version}
		return v, s.emit(work, b, c.Target, "organization.membership.changed", revision, v)
	})
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
func (s *Store) RemoveOrganizationMembership(ctx context.Context, c contracts.RemoveMembership) error {
	if !commandValid(c.Command) || c.Target.Kind != contracts.OrganizationKind || !validPrincipal(c.Principal) {
		return scope.ErrInvalid
	}
	_, err := s.run(ctx, c.Target.TenantID, "organization.membership.remove", c.IdempotencyKey, c, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if err := lockTarget(work, c.Command); err != nil {
			return nil, err
		}
		if err := s.targetPermission(work, b, c.Target, "organization.membership.remove"); err != nil {
			return nil, err
		}
		if err := preserveOwner(work, c.Target, c.Principal); err != nil {
			return nil, err
		}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		tag, err := ex.Exec(work, `UPDATE pangolin_organization.organization_memberships SET state='revoked',version=version+1 WHERE tenant_id=$1 AND organization_id=$2 AND principal_kind=$3 AND principal_id=$4 AND state='active'`, c.Target.TenantID, c.Target.ID, c.Principal.Kind, c.Principal.ID)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return nil, ErrNotFound
		}
		if err = owner(work, c.Target, c.Principal, false); err != nil {
			return nil, err
		}
		revision, err := bump(work, c.Target)
		if err != nil {
			return nil, err
		}
		return struct{}{}, s.emit(work, b, c.Target, "organization.membership.removed", revision, c.Principal)
	})
	return err
}
func (s *Store) SetEnterpriseRole(ctx context.Context, c contracts.SetEnterpriseRole) error {
	if !commandValid(c.Command) || c.Target.Kind != contracts.EnterpriseKind || !validPrincipal(c.Principal) || (c.Role != "owner" && c.Role != "manager") {
		return scope.ErrInvalid
	}
	_, err := s.run(ctx, c.Target.TenantID, "enterprise.role.set", c.IdempotencyKey, c, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if err := lockTarget(work, c.Command); err != nil {
			return nil, err
		}
		if err := s.targetPermission(work, b, c.Target, "enterprise.role.set"); err != nil {
			return nil, err
		}
		if _, err := s.options.Tenants.Check(work, c.Principal, c.Target.TenantID); err != nil {
			return nil, err
		}
		if c.Role != "owner" {
			if err := preserveOwner(work, c.Target, c.Principal); err != nil {
				return nil, err
			}
		}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		_, err = ex.Exec(work, `INSERT INTO pangolin_organization.enterprise_roles(tenant_id,enterprise_id,principal_kind,principal_id,role) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,enterprise_id,principal_kind,principal_id) DO UPDATE SET role=EXCLUDED.role,state='active',version=pangolin_organization.enterprise_roles.version+1`, c.Target.TenantID, c.Target.ID, c.Principal.Kind, c.Principal.ID, c.Role)
		if err != nil {
			return nil, err
		}
		if err = owner(work, c.Target, c.Principal, c.Role == "owner"); err != nil {
			return nil, err
		}
		revision, err := bump(work, c.Target)
		if err != nil {
			return nil, err
		}
		return struct{}{}, s.emit(work, b, c.Target, "enterprise.role.changed", revision, c)
	})
	return err
}
func (s *Store) AssignEnterprise(ctx context.Context, c contracts.AssignEnterprise) (contracts.Organization, error) {
	var result contracts.Organization
	if !commandValid(c.Command) || c.Target.Kind != contracts.OrganizationKind {
		return result, scope.ErrInvalid
	}
	raw, err := s.run(ctx, c.Target.TenantID, "organization.transfer", c.IdempotencyKey, c, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if err := lockTarget(work, c.Command); err != nil {
			return nil, err
		}
		if err := s.targetPermission(work, b, c.Target, "organization.transfer"); err != nil {
			return nil, err
		}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		var old *string
		var slug, state string
		if err = ex.QueryRow(work, `SELECT enterprise_id,slug,state FROM pangolin_organization.organizations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, c.Target.TenantID, c.Target.ID).Scan(&old, &slug, &state); err != nil {
			return nil, err
		}
		for _, parent := range []struct {
			id     *string
			action string
		}{{old, "enterprise.organization.detach"}, {c.EnterpriseID, "enterprise.organization.attach"}} {
			if parent.id != nil {
				if err = s.targetPermission(work, b, scope.ScopeRef{TenantID: c.Target.TenantID, Kind: contracts.EnterpriseKind, ID: *parent.id}, parent.action); err != nil {
					return nil, err
				}
			}
		}
		if _, err = ex.Exec(work, `UPDATE pangolin_organization.organizations SET enterprise_id=$3 WHERE tenant_id=$1 AND id=$2`, c.Target.TenantID, c.Target.ID, c.EnterpriseID); err != nil {
			return nil, err
		}
		revision, err := bump(work, c.Target)
		if err != nil {
			return nil, err
		}
		v := contracts.Organization{Ref: c.Target, EnterpriseID: c.EnterpriseID, Slug: slug, State: state, Version: revision}
		return v, s.emit(work, b, c.Target, "organization.transferred", revision, v)
	})
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
func (s *Store) state(ctx context.Context, c contracts.Command, state string) error {
	if !commandValid(c) {
		return scope.ErrInvalid
	}
	_, err := s.run(ctx, c.Target.TenantID, "scope."+state, c.IdempotencyKey, c, func(work context.Context, b scope.ScopeBinding) (any, error) {
		if err := lockTarget(work, c); err != nil {
			return nil, err
		}
		if state == "active" {
			if err := s.tenantPermission(work, b, "tenant.scope.recover"); err != nil {
				return nil, err
			}
		} else if err := s.targetPermission(work, b, c.Target, "scope."+state); err != nil {
			return nil, err
		}
		ex, err := executor(work, true)
		if err != nil {
			return nil, err
		}
		if state == "active" {
			var previous string
			if err = ex.QueryRow(work, `SELECT state FROM pangolin.resource_scopes WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3`, c.Target.TenantID, c.Target.Kind, c.Target.ID).Scan(&previous); err != nil {
				return nil, err
			}
			if previous == "archived" || previous == "ownerless" {
				return nil, ErrConflict
			}
		}
		if state == "archived" {
			used, err := s.options.UseChecker.InUse(work, c.Target)
			if err != nil {
				return nil, err
			}
			if used {
				return nil, ErrInUse
			}
			if c.Target.Kind == contracts.EnterpriseKind {
				var n int
				if err = ex.QueryRow(work, `SELECT count(*) FROM pangolin_organization.organizations WHERE tenant_id=$1 AND enterprise_id=$2`, c.Target.TenantID, c.Target.ID).Scan(&n); err != nil {
					return nil, err
				}
				if n > 0 {
					return nil, ErrInUse
				}
			}
		}
		_, err = ex.Exec(work, `UPDATE pangolin.resource_scopes SET state=$4 WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3`, c.Target.TenantID, c.Target.Kind, c.Target.ID, state)
		if err != nil {
			return nil, err
		}
		table := "organizations"
		if c.Target.Kind == contracts.EnterpriseKind {
			table = "enterprises"
		}
		if _, err = ex.Exec(work, `UPDATE pangolin_organization.`+table+` SET state=$3 WHERE tenant_id=$1 AND id=$2`, c.Target.TenantID, c.Target.ID, state); err != nil {
			return nil, err
		}
		revision, err := bump(work, c.Target)
		if err != nil {
			return nil, err
		}
		return struct{}{}, s.emit(work, b, c.Target, "scope."+state, revision, c.Target)
	})
	return err
}
func (s *Store) Suspend(c context.Context, v contracts.Command) error {
	return s.state(c, v, "suspended")
}
func (s *Store) Resume(c context.Context, v contracts.Command) error { return s.state(c, v, "active") }
func (s *Store) Archive(c context.Context, v contracts.Command) error {
	return s.state(c, v, "archived")
}
func (s *Store) ListMyOrganizations(ctx context.Context, page contracts.PageRequest) (contracts.OrganizationPage, error) {
	b, err := s.actorFromContext(ctx)
	if err != nil {
		return contracts.OrganizationPage{}, err
	}
	return s.list(ctx, b, page, nil)
}
func (s *Store) actorFromContext(ctx context.Context) (scope.ScopeBinding, error) {
	b, ok := scope.Current(ctx)
	if !ok {
		return b, scope.ErrDenied
	}
	return s.actor(ctx, b.Ref.TenantID)
}
func (s *Store) ListEnterpriseOrganizations(ctx context.Context, ref scope.ScopeRef, page contracts.PageRequest) (contracts.OrganizationPage, error) {
	b, err := s.actor(ctx, ref.TenantID)
	if err != nil {
		return contracts.OrganizationPage{}, err
	}
	if ref.Kind != contracts.EnterpriseKind {
		return contracts.OrganizationPage{}, scope.ErrInvalid
	}
	if err = s.targetPermission(ctx, b, ref, "enterprise.organizations.list"); err != nil {
		return contracts.OrganizationPage{}, err
	}
	return s.list(ctx, b, page, &ref.ID)
}
func (s *Store) list(ctx context.Context, b scope.ScopeBinding, page contracts.PageRequest, parent *string) (contracts.OrganizationPage, error) {
	var result contracts.OrganizationPage
	limit, err := pageLimit(page.Limit)
	if err != nil {
		return result, err
	}
	cursor, err := decodeCursor(page.Cursor)
	if err != nil {
		return result, err
	}
	// This explicitly authorized metadata Port never grants a business owner union.
	rows, err := s.options.AuthorityPool.Query(ctx, s.sql(`SELECT o.id,o.enterprise_id,o.slug,o.state,o.version FROM pangolin_organization.organizations o WHERE o.tenant_id=$1 AND o.id>$2 AND (($5::text IS NOT NULL AND o.enterprise_id=$5) OR ($5::text IS NULL AND EXISTS(SELECT 1 FROM pangolin_organization.organization_memberships m WHERE m.tenant_id=o.tenant_id AND m.organization_id=o.id AND m.principal_kind=$3 AND m.principal_id=$4 AND m.state='active'))) ORDER BY o.id LIMIT $6`), b.Ref.TenantID, cursor, b.Principal.Kind, b.Principal.ID, parent, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var o contracts.Organization
		o.Ref.TenantID = b.Ref.TenantID
		o.Ref.Kind = contracts.OrganizationKind
		if err = rows.Scan(&o.Ref.ID, &o.EnterpriseID, &o.Slug, &o.State, &o.Version); err != nil {
			return result, err
		}
		result.Items = append(result.Items, o)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = encodeCursor(result.Items[limit-1].Ref.ID)
	}
	return result, nil
}
