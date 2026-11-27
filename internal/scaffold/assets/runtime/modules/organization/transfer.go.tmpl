package organization

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/pangolin-framework/pangolin/modules/organization/contracts"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/scope"
	"github.com/jackc/pgx/v5"
)

func validState(s string) bool {
	return s == "active" || s == "suspended" || s == "archived" || s == "ownerless"
}
func validateBundle(b Bundle, tenant string) error {
	if b.Version != 1 || tenant == "" {
		return scope.ErrInvalid
	}
	refs := map[scope.ScopeRef]CatalogRecord{}
	for _, c := range b.Catalog {
		if scope.Validate(c.Ref) != nil || !validOrganizationID(c.Ref.ID) || c.Ref.TenantID != tenant || !validState(c.State) || c.Version < 1 || (c.Ref.Kind != contracts.EnterpriseKind && c.Ref.Kind != contracts.OrganizationKind) {
			return scope.ErrInvalid
		}
		if _, ok := refs[c.Ref]; ok {
			return scope.ErrInvalid
		}
		refs[c.Ref] = c
	}
	seen := map[scope.ScopeRef]bool{}
	for _, e := range b.Enterprises {
		c, ok := refs[e.Ref]
		if !ok || e.Ref.Kind != contracts.EnterpriseKind || !validSlug(e.Slug) || e.State != c.State || e.Version != c.Version || seen[e.Ref] {
			return scope.ErrInvalid
		}
		seen[e.Ref] = true
	}
	for _, o := range b.Organizations {
		c, ok := refs[o.Ref]
		if !ok || o.Ref.Kind != contracts.OrganizationKind || !validSlug(o.Slug) || o.State != c.State || o.Version != c.Version || seen[o.Ref] {
			return scope.ErrInvalid
		}
		seen[o.Ref] = true
		if o.EnterpriseID != nil {
			if _, ok = refs[scope.ScopeRef{TenantID: tenant, Kind: contracts.EnterpriseKind, ID: *o.EnterpriseID}]; !ok {
				return scope.ErrInvalid
			}
		}
	}
	if len(seen) != len(refs) {
		return scope.ErrInvalid
	}
	for _, m := range b.Memberships {
		if _, ok := refs[scope.ScopeRef{TenantID: m.TenantID, Kind: contracts.OrganizationKind, ID: m.OrganizationID}]; !ok || !validPrincipal(m.Principal) || (m.Role != "owner" && m.Role != "member") || (m.State != "active" && m.State != "revoked") || m.Version < 1 {
			return scope.ErrInvalid
		}
	}
	for _, r := range b.Roles {
		if _, ok := refs[r.Ref]; !ok || r.Ref.Kind != contracts.EnterpriseKind || !validPrincipal(r.Principal) || (r.Role != "owner" && r.Role != "manager") || (r.State != "active" && r.State != "revoked") || r.Version < 1 {
			return scope.ErrInvalid
		}
	}
	for _, g := range b.Grants {
		if _, ok := refs[g.Ref]; !ok || g.Principal.Kind != "service" || !validPrincipal(g.Principal) || g.Role == "" || (g.State != "active" && g.State != "revoked") || g.Version < 1 {
			return scope.ErrInvalid
		}
	}
	type ownerKey struct {
		Ref       scope.ScopeRef
		Principal scope.PrincipalRef
	}
	expectedOwners := map[ownerKey]bool{}
	for _, m := range b.Memberships {
		if m.Role == "owner" && m.State == "active" {
			expectedOwners[ownerKey{scope.ScopeRef{TenantID: m.TenantID, Kind: contracts.OrganizationKind, ID: m.OrganizationID}, m.Principal}] = true
		}
	}
	for _, r := range b.Roles {
		if r.Role == "owner" && r.State == "active" {
			expectedOwners[ownerKey{r.Ref, r.Principal}] = true
		}
	}
	for _, o := range b.Owners {
		if _, ok := refs[o.Ref]; !ok || !validPrincipal(o.Principal) || (o.State != "active" && o.State != "revoked") {
			return scope.ErrInvalid
		}
		if o.State == "active" {
			key := ownerKey{o.Ref, o.Principal}
			if !expectedOwners[key] {
				return scope.ErrInvalid
			}
			delete(expectedOwners, key)
		}
	}
	if len(expectedOwners) != 0 {
		return scope.ErrInvalid
	}
	for _, r := range b.Restrictions {
		_, ok := refs[r.Ref]
		if r.Ref == (scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant}) {
			ok = true
		}
		if !ok || r.Action == "" {
			return scope.ErrInvalid
		}
	}
	return nil
}

// DecodeBundle accepts only the versioned logical migration schema. Callers
// authenticate and protect the file separately; it is never an authority token.
func DecodeBundle(raw []byte) (Bundle, error) {
	var b Bundle
	if len(raw) > 16<<20 || json.Unmarshal(raw, &b) != nil || b.Version != 1 {
		return b, scope.ErrInvalid
	}
	return b, nil
}

// Export is a deployment operation after Drain. All rows come from one bounded
// repeatable-read snapshot. It includes revoked roles and tombstones.
func (s *Store) Export(ctx context.Context) (Bundle, error) {
	result := Bundle{Version: 1}
	b, err := s.actorFromContext(ctx)
	if err != nil {
		return result, err
	}
	if err = s.tenantPermission(ctx, b, "tenant.organization.export"); err != nil {
		return result, err
	}
	tx, err := s.options.AuthorityPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(clean)
	}()
	read := func(query string, scan func(pgx.Rows) error) error {
		rows, e := tx.Query(ctx, s.sql(query), b.Ref.TenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			if e = scan(rows); e != nil {
				return e
			}
		}
		return rows.Err()
	}
	err = read(`SELECT tenant_id,id,slug,state,version FROM pangolin_organization.enterprises WHERE tenant_id=$1 ORDER BY id`, func(r pgx.Rows) error {
		v := contracts.Enterprise{}
		v.Ref.Kind = contracts.EnterpriseKind
		e := r.Scan(&v.Ref.TenantID, &v.Ref.ID, &v.Slug, &v.State, &v.Version)
		result.Enterprises = append(result.Enterprises, v)
		return e
	})
	if err != nil {
		return result, err
	}
	err = read(`SELECT tenant_id,id,enterprise_id,slug,state,version FROM pangolin_organization.organizations WHERE tenant_id=$1 ORDER BY id`, func(r pgx.Rows) error {
		v := contracts.Organization{}
		v.Ref.Kind = contracts.OrganizationKind
		e := r.Scan(&v.Ref.TenantID, &v.Ref.ID, &v.EnterpriseID, &v.Slug, &v.State, &v.Version)
		result.Organizations = append(result.Organizations, v)
		return e
	})
	if err != nil {
		return result, err
	}
	err = read(`SELECT tenant_id,organization_id,principal_kind,principal_id,role,state,version FROM pangolin_organization.organization_memberships WHERE tenant_id=$1 ORDER BY organization_id,principal_kind,principal_id`, func(r pgx.Rows) error {
		v := contracts.Membership{}
		e := r.Scan(&v.TenantID, &v.OrganizationID, &v.Principal.Kind, &v.Principal.ID, &v.Role, &v.State, &v.Version)
		result.Memberships = append(result.Memberships, v)
		return e
	})
	if err != nil {
		return result, err
	}
	err = read(`SELECT tenant_id,enterprise_id,principal_kind,principal_id,role,state,version FROM pangolin_organization.enterprise_roles WHERE tenant_id=$1 ORDER BY enterprise_id,principal_kind,principal_id`, func(r pgx.Rows) error {
		v := RoleRecord{}
		v.Ref.Kind = contracts.EnterpriseKind
		e := r.Scan(&v.Ref.TenantID, &v.Ref.ID, &v.Principal.Kind, &v.Principal.ID, &v.Role, &v.State, &v.Version)
		result.Roles = append(result.Roles, v)
		return e
	})
	if err != nil {
		return result, err
	}
	err = read(`SELECT tenant_id,scope_kind,scope_id,principal_kind,principal_id,role,state,version FROM pangolin_organization.scope_grants WHERE tenant_id=$1 ORDER BY scope_kind,scope_id,principal_id`, func(r pgx.Rows) error {
		v := GrantRecord{}
		e := r.Scan(&v.Ref.TenantID, &v.Ref.Kind, &v.Ref.ID, &v.Principal.Kind, &v.Principal.ID, &v.Role, &v.State, &v.Version)
		result.Grants = append(result.Grants, v)
		return e
	})
	if err != nil {
		return result, err
	}
	err = read(`SELECT tenant_id,scope_kind,scope_id,action FROM pangolin_organization.restrictions WHERE tenant_id=$1 ORDER BY scope_kind,scope_id,action`, func(r pgx.Rows) error {
		v := Restriction{}
		e := r.Scan(&v.Ref.TenantID, &v.Ref.Kind, &v.Ref.ID, &v.Action)
		result.Restrictions = append(result.Restrictions, v)
		return e
	})
	if err != nil {
		return result, err
	}
	err = read(`SELECT tenant_id,scope_kind,scope_id,principal_kind,principal_id,state FROM pangolin_organization.owners WHERE tenant_id=$1 ORDER BY scope_kind,scope_id,principal_kind,principal_id`, func(r pgx.Rows) error {
		v := OwnerRecord{}
		e := r.Scan(&v.Ref.TenantID, &v.Ref.Kind, &v.Ref.ID, &v.Principal.Kind, &v.Principal.ID, &v.State)
		result.Owners = append(result.Owners, v)
		return e
	})
	if err != nil {
		return result, err
	}
	err = read(`SELECT tenant_id,scope_kind,scope_id,state,version FROM pangolin.resource_scopes WHERE tenant_id=$1 AND scope_kind IN('org.enterprise','org.organization') ORDER BY scope_kind,scope_id`, func(r pgx.Rows) error {
		v := CatalogRecord{}
		e := r.Scan(&v.Ref.TenantID, &v.Ref.Kind, &v.Ref.ID, &v.State, &v.Version)
		result.Catalog = append(result.Catalog, v)
		return e
	})
	if err != nil {
		return result, err
	}
	if err = validateBundle(result, b.Ref.TenantID); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// Import restores a drained deployment into an empty private schema. Existing
// neutral IDs must match exactly; no owner or revision is reassigned. Failure
// rolls back every imported row together with the required Audit/Outbox record.
func (s *Store) Import(ctx context.Context, bundle Bundle) error {
	b, err := s.actorFromContext(ctx)
	if err != nil {
		return err
	}
	if err = validateBundle(bundle, b.Ref.TenantID); err != nil {
		return err
	}
	ctx = privateSchemaVar.With(ctx, s.options.PrivateSchema)
	return s.options.Manager.Within(ctx, uow.Options{Mode: uow.ReadWrite, Transaction: uow.Transactional}, func(work context.Context) error {
		b, err := s.actorFromContext(work)
		if err != nil {
			return err
		}
		if err = s.tenantPermission(work, b, "tenant.organization.import"); err != nil {
			return err
		}
		ex, err := executor(work, true)
		if err != nil {
			return err
		}
		var n int
		if err = ex.QueryRow(work, `SELECT (SELECT count(*) FROM pangolin_organization.enterprises WHERE tenant_id=$1)+(SELECT count(*) FROM pangolin_organization.organizations WHERE tenant_id=$1)`, b.Ref.TenantID).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
		for _, c := range bundle.Catalog {
			if _, err = ex.Exec(work, `INSERT INTO pangolin.resource_scopes(tenant_id,scope_kind,scope_id,state,version) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, c.Ref.TenantID, c.Ref.Kind, c.Ref.ID, c.State, c.Version); err != nil {
				return err
			}
			var state string
			var version int64
			if err = ex.QueryRow(work, `SELECT state,version FROM pangolin.resource_scopes WHERE tenant_id=$1 AND scope_kind=$2 AND scope_id=$3 FOR UPDATE`, c.Ref.TenantID, c.Ref.Kind, c.Ref.ID).Scan(&state, &version); err != nil {
				return err
			}
			if state != c.State || version != c.Version {
				return ErrConflict
			}
		}
		for _, e := range bundle.Enterprises {
			if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.enterprises(tenant_id,id,slug,state,version) VALUES($1,$2,$3,$4,$5)`, e.Ref.TenantID, e.Ref.ID, e.Slug, e.State, e.Version); err != nil {
				return err
			}
		}
		for _, o := range bundle.Organizations {
			if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.organizations(tenant_id,id,enterprise_id,slug,state,version) VALUES($1,$2,$3,$4,$5,$6)`, o.Ref.TenantID, o.Ref.ID, o.EnterpriseID, o.Slug, o.State, o.Version); err != nil {
				return err
			}
		}
		for _, m := range bundle.Memberships {
			if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.organization_memberships(tenant_id,organization_id,principal_kind,principal_id,role,state,version) VALUES($1,$2,$3,$4,$5,$6,$7)`, m.TenantID, m.OrganizationID, m.Principal.Kind, m.Principal.ID, m.Role, m.State, m.Version); err != nil {
				return err
			}
		}
		for _, r := range bundle.Roles {
			if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.enterprise_roles(tenant_id,enterprise_id,principal_kind,principal_id,role,state,version) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.Ref.TenantID, r.Ref.ID, r.Principal.Kind, r.Principal.ID, r.Role, r.State, r.Version); err != nil {
				return err
			}
		}
		for _, r := range bundle.Grants {
			if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.scope_grants(tenant_id,scope_kind,scope_id,principal_kind,principal_id,role,state,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.Ref.TenantID, r.Ref.Kind, r.Ref.ID, r.Principal.Kind, r.Principal.ID, r.Role, r.State, r.Version); err != nil {
				return err
			}
		}
		for _, r := range bundle.Restrictions {
			if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.restrictions(tenant_id,scope_kind,scope_id,action) VALUES($1,$2,$3,$4)`, r.Ref.TenantID, r.Ref.Kind, r.Ref.ID, r.Action); err != nil {
				return err
			}
		}
		for _, o := range bundle.Owners {
			if _, err = ex.Exec(work, `INSERT INTO pangolin_organization.owners(tenant_id,scope_kind,scope_id,principal_kind,principal_id,state) VALUES($1,$2,$3,$4,$5,$6)`, o.Ref.TenantID, o.Ref.Kind, o.Ref.ID, o.Principal.Kind, o.Principal.ID, o.State); err != nil {
				return err
			}
		}
		return s.emit(work, b, b.Ref, "organization.provider.imported", 1, struct {
			Version int
			Count   int
		}{bundle.Version, len(bundle.Catalog)})
	})
}

// Reconcile compares a replacement's logical data, including governance state
// and stable IDs, with its drained source. Pending work remains in neutral
// event/job stores and is checked by the deployment inventory Port.
func Reconcile(source, restored Bundle) error {
	a, err := EncodeBundle(source)
	if err != nil {
		return err
	}
	b, err := EncodeBundle(restored)
	if err != nil {
		return err
	}
	if string(a) != string(b) {
		return errors.Join(ErrConflict, errors.New("organization migration reconciliation failed"))
	}
	return nil
}
