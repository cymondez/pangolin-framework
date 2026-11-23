package organization

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/persistence/idempotency"
	"github.com/cymondez/pangolin-framework/persistence/postgres"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound          = errors.New("RESOURCE_NOT_FOUND")
	ErrConflict          = errors.New("RESOURCE_CONFLICT")
	ErrLastOwner         = errors.New("ORGANIZATION_LAST_OWNER")
	ErrInUse             = errors.New("ORGANIZATION_IN_USE")
	ErrMigrationRequired = scope.ErrMigrationRequired
)

// TenantAccess is the neutral Tenant authority. It does not create organization users.
type TenantAccess interface {
	Check(context.Context, scope.PrincipalRef, string) (string, error)
}
type Permission interface {
	Check(context.Context, Snapshot, string) error
}
type PermissionFunc func(context.Context, Snapshot, string) error

func (f PermissionFunc) Check(c context.Context, s Snapshot, a string) error { return f(c, s, a) }

// Change belongs to the initiating governance operation's scope; Target is metadata.
// Effects MUST atomically save required Audit and an Outbox event via the supplied UOW context.
type Change struct {
	Action         string
	Actor          scope.PrincipalRef
	OperationScope scope.ScopeRef
	Target         scope.ScopeRef
	Revision       int64
	Payload        json.RawMessage
}
type Effects interface {
	Record(context.Context, Change) error
}
type EffectsFunc func(context.Context, Change) error

func (f EffectsFunc) Record(c context.Context, e Change) error { return f(c, e) }

type UseChecker interface {
	InUse(context.Context, scope.ScopeRef) (bool, error)
}
type UseCheckerFunc func(context.Context, scope.ScopeRef) (bool, error)

func (f UseCheckerFunc) InUse(c context.Context, r scope.ScopeRef) (bool, error) { return f(c, r) }

type StoreOptions struct {
	// AuthorityPool is a separate, metadata-read-only infrastructure connection.
	// Its role must not have any business-table bypass privileges.
	AuthorityPool *pgxpool.Pool
	Manager       uow.UnitOfWorkManager
	Identity      scope.IdentityReader
	Tenants       TenantAccess
	Permissions   Permission
	Effects       Effects
	UseChecker    UseChecker
	Idempotency   *idempotency.Store
	// PrivateSchema permits an independently deployed replacement to import the
	// logical Bundle into its own tables. It never changes the neutral catalog.
	PrivateSchema string
}
type Store struct {
	options    StoreOptions
	signingKey [32]byte
}

// Snapshot is an immutable admission snapshot, including the local Tenant projection.
// Permissions should use its role and revision; Features never create permission.
type Snapshot struct {
	Ref            scope.ScopeRef
	Principal      scope.PrincipalRef
	Role           string
	State          string
	Version        int64
	TenantRevision int64
	ParentID       string
	ParentVersion  int64
	ParentState    string
	Denied         []string
}

func NewStore(o StoreOptions) (*Store, error) {
	if o.AuthorityPool == nil || o.Manager == nil || o.Identity == nil || o.Tenants == nil || o.Permissions == nil || o.Effects == nil || o.UseChecker == nil || o.Idempotency == nil {
		return nil, scope.ErrInvalid
	}
	if o.PrivateSchema == "" {
		o.PrivateSchema = "pangolin_organization"
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(o.PrivateSchema) {
		return nil, scope.ErrInvalid
	}
	s := &Store{options: o}
	_, _ = rand.Read(s.signingKey[:])
	return s, nil
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Migrations() fs.FS { f, _ := fs.Sub(migrationFiles, "migrations"); return f }

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validSlug(s string) bool { return slugPattern.MatchString(s) }
func validPrincipal(p scope.PrincipalRef) bool {
	return p.ID != "" && strings.TrimSpace(p.ID) == p.ID && (p.Kind == "user" || p.Kind == "service")
}
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func pageLimit(n int) (int, error) {
	if n < 1 || n > 100 {
		return 0, scope.ErrInvalid
	}
	return n, nil
}
func encodeCursor(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func decodeCursor(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) > 128 || len(b) == 0 {
		return "", scope.ErrInvalid
	}
	return string(b), nil
}

func (s *Store) sign(snapshot Snapshot) string {
	raw, _ := json.Marshal(snapshot)
	mac := hmac.New(sha256.New, s.signingKey[:])
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Store) verify(b scope.ScopeBinding) (Snapshot, error) {
	parts := strings.Split(b.AuthorityRevision, ".")
	if len(parts) != 2 {
		return Snapshot{}, scope.ErrDenied
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil || len(raw) > 8192 {
		return Snapshot{}, scope.ErrDenied
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return Snapshot{}, scope.ErrDenied
	}
	mac := hmac.New(sha256.New, s.signingKey[:])
	_, _ = mac.Write(raw)
	var snapshot Snapshot
	if !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(raw, &snapshot) != nil || snapshot.Ref != b.Ref || snapshot.Principal != b.Principal {
		return Snapshot{}, scope.ErrDenied
	}
	return snapshot, nil
}

func (s *Store) Resolve(ctx context.Context, p scope.PrincipalRef, ref scope.ScopeRef) (scope.ScopeBinding, error) {
	snapshot, err := s.snapshot(ctx, p, ref)
	if err != nil {
		return scope.ScopeBinding{}, err
	}
	return scope.ScopeBinding{Ref: ref, Principal: p, AuthorityRevision: s.sign(snapshot)}, nil
}
func (s *Store) Authorize(ctx context.Context, b scope.ScopeBinding, action string) error {
	if ctx == nil || action == "" || scope.Validate(b.Ref) != nil || !validPrincipal(b.Principal) || s.options.Permissions == nil {
		return scope.ErrDenied
	}
	var snapshot Snapshot
	var err error
	if uow.Check(ctx, false) == nil {
		current, _ := scope.Current(ctx)
		if current != b {
			return scope.ErrDenied
		}
		snapshot, err = s.verify(b)
	} else {
		snapshot, err = s.snapshot(ctx, b.Principal, b.Ref)
	}
	if err != nil {
		return err
	}
	for _, a := range snapshot.Denied {
		if a == action || a == "*" {
			return scope.ErrDenied
		}
	}
	return s.options.Permissions.Check(ctx, snapshot, action)
}

// snapshot is one SQL statement: membership, parent, owners, restrictions and
// Tenant qualification all come from the same PostgreSQL visibility snapshot.
func (s *Store) snapshot(ctx context.Context, p scope.PrincipalRef, ref scope.ScopeRef) (Snapshot, error) {
	if ctx == nil || scope.Validate(ref) != nil || !validPrincipal(p) || (ref.Kind != contracts.OrganizationKind && ref.Kind != contracts.EnterpriseKind) || s.options.AuthorityPool == nil {
		return Snapshot{}, scope.ErrDenied
	}
	principal, tenant, ok := s.options.Identity(ctx)
	if !ok || principal != p || tenant != ref.TenantID {
		return Snapshot{}, scope.ErrDenied
	}
	var result Snapshot
	result.Ref = ref
	result.Principal = p
	var denied []byte
	err := s.options.AuthorityPool.QueryRow(ctx, s.sql(`SELECT c.state,c.version,tm.revision,
 COALESCE(o.enterprise_id,''),COALESCE(parent.version,0),COALESCE(parent.state,'active'),
 CASE WHEN $5='service' THEN g.role WHEN $2='org.organization' THEN m.role ELSE er.role END,
 COALESCE((SELECT jsonb_agg(DISTINCT d.action ORDER BY d.action) FROM pangolin_organization.restrictions d
 WHERE d.tenant_id=$1 AND ((d.scope_kind=$2 AND d.scope_id=$3) OR (d.scope_kind='org.enterprise' AND d.scope_id=o.enterprise_id) OR (d.scope_kind='tenant' AND d.scope_id=$1))), '[]'::jsonb)
 FROM pangolin.resource_scopes c
 JOIN pangolin_security.tenant_memberships tm ON tm.tenant_id=c.tenant_id AND tm.principal_kind=$5 AND tm.principal_id=$4 AND tm.state='active'
 LEFT JOIN pangolin_organization.organizations o ON c.scope_kind='org.organization' AND o.tenant_id=c.tenant_id AND o.id=c.scope_id
 LEFT JOIN pangolin_organization.enterprises parent ON parent.tenant_id=o.tenant_id AND parent.id=o.enterprise_id
 LEFT JOIN pangolin_organization.organization_memberships m ON m.tenant_id=c.tenant_id AND m.organization_id=c.scope_id AND m.principal_id=$4 AND m.principal_kind=$5 AND m.state='active'
 LEFT JOIN pangolin_organization.enterprise_roles er ON er.tenant_id=c.tenant_id AND er.enterprise_id=c.scope_id AND er.principal_id=$4 AND er.principal_kind=$5 AND er.state='active'
 LEFT JOIN pangolin_organization.scope_grants g ON g.tenant_id=c.tenant_id AND g.scope_kind=c.scope_kind AND g.scope_id=c.scope_id AND g.principal_id=$4 AND g.principal_kind=$5 AND g.state='active'
 WHERE c.tenant_id=$1 AND c.scope_kind=$2 AND c.scope_id=$3 AND c.state='active'
 AND (o.enterprise_id IS NULL OR parent.state='active')
 AND CASE WHEN $5='service' THEN g.role IS NOT NULL WHEN $2='org.organization' THEN m.role IS NOT NULL ELSE er.role IS NOT NULL END
 AND EXISTS (SELECT 1 FROM pangolin_organization.owners ow JOIN pangolin_security.tenant_memberships ot ON ot.tenant_id=ow.tenant_id AND ot.principal_kind=ow.principal_kind AND ot.principal_id=ow.principal_id AND ot.state='active'
 WHERE ow.tenant_id=c.tenant_id AND ow.scope_kind=c.scope_kind AND ow.scope_id=c.scope_id AND ow.state='active')`), ref.TenantID, ref.Kind, ref.ID, p.ID, p.Kind).Scan(&result.State, &result.Version, &result.TenantRevision, &result.ParentID, &result.ParentVersion, &result.ParentState, &result.Role, &denied)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, errors.Join(scope.ErrAuthorityUnavailable, err)
	}
	if err = json.Unmarshal(denied, &result.Denied); err != nil {
		return Snapshot{}, errors.Join(scope.ErrAuthorityUnavailable, err)
	}
	return result, nil
}

func (s *Store) actor(ctx context.Context, tenant string) (scope.ScopeBinding, error) {
	b, ok := scope.Current(ctx)
	if !ok || b.Ref.TenantID != tenant || !validPrincipal(b.Principal) {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	p, t, ok := s.options.Identity(ctx)
	if !ok || p != b.Principal || t != tenant {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	if _, err := s.options.Tenants.Check(ctx, p, tenant); err != nil {
		return scope.ScopeBinding{}, err
	}
	return b, nil
}
func (s *Store) tenantPermission(ctx context.Context, b scope.ScopeBinding, action string) error {
	if b.Ref.Kind != "tenant" || b.Ref.ID != b.Ref.TenantID {
		return scope.ErrDenied
	}
	return s.options.Permissions.Check(ctx, Snapshot{Ref: b.Ref, Principal: b.Principal, Role: "tenant", State: "active"}, action)
}
func (s *Store) targetPermission(ctx context.Context, b scope.ScopeBinding, target scope.ScopeRef, action string) error {
	if target.TenantID != b.Ref.TenantID {
		return ErrNotFound
	}
	snapshot, err := s.snapshot(ctx, b.Principal, target)
	if err != nil {
		return err
	}
	for _, a := range snapshot.Denied {
		if a == action || a == "*" {
			return scope.ErrDenied
		}
	}
	return s.options.Permissions.Check(ctx, snapshot, action)
}
func (s *Store) emit(ctx context.Context, b scope.ScopeBinding, target scope.ScopeRef, action string, version int64, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.options.Effects.Record(ctx, Change{Action: action, Actor: b.Principal, OperationScope: b.Ref, Target: target, Revision: version, Payload: raw})
}

type db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type privateSchemaKey struct{}

func (s *Store) sql(query string) string {
	return strings.ReplaceAll(query, "pangolin_organization.", pgx.Identifier{s.options.PrivateSchema}.Sanitize()+".")
}

type schemaExecutor struct {
	base   *postgres.Executor
	schema string
}

func (e schemaExecutor) sql(q string) string {
	return strings.ReplaceAll(q, "pangolin_organization.", pgx.Identifier{e.schema}.Sanitize()+".")
}
func (e schemaExecutor) Exec(c context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return e.base.Exec(c, e.sql(q), args...)
}
func (e schemaExecutor) QueryRow(c context.Context, q string, args ...any) pgx.Row {
	return e.base.QueryRow(c, e.sql(q), args...)
}
func executor(ctx context.Context, write bool) (db, error) {
	base, err := postgres.Current(ctx, write)
	if err != nil {
		return nil, err
	}
	schema, _ := ctx.Value(privateSchemaKey{}).(string)
	if schema == "" {
		schema = "pangolin_organization"
	}
	return schemaExecutor{base, schema}, nil
}
func commandValid(c contracts.Command) bool {
	return scope.Validate(c.Target) == nil && c.ExpectedVersion > 0 && c.IdempotencyKey != "" && len(c.IdempotencyKey) <= 128 && (c.Target.Kind == contracts.OrganizationKind || c.Target.Kind == contracts.EnterpriseKind)
}
