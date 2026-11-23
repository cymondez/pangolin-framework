package organization

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/persistence/uow"
	"github.com/cymondez/pangolin-framework/security/scope"
	"strings"
	"testing"
	"time"
)

func TestGovernanceInputs(t *testing.T) {
	if _, err := NewStore(StoreOptions{}); !errors.Is(err, scope.ErrInvalid) {
		t.Fatal(err)
	}
	for _, slug := range []string{"", " with-space", "A", "a/b", "a_"} {
		if validSlug(slug) {
			t.Fatal(slug)
		}
	}
	for _, slug := range []string{"a", "team-one", "team2"} {
		if !validSlug(slug) {
			t.Fatal(slug)
		}
	}
	if _, err := decodeCursor("!"); !errors.Is(err, scope.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := decodeCursor(encodeCursor("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := pageLimit(0); err == nil {
		t.Fatal("unbounded list")
	}
	s := &Store{}
	if err := s.Authorize(context.Background(), scope.ScopeBinding{}, "x"); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
}

func TestNoGovernanceWithoutUnitOfWork(t *testing.T) {
	ctx := context.Background()
	ref := scope.ScopeRef{TenantID: "tenant", Kind: contracts.OrganizationKind, ID: "11111111-1111-4111-8111-111111111111"}
	p := scope.PrincipalRef{Kind: "user", ID: "alice"}
	c := contracts.Command{Target: ref, ExpectedVersion: 1, IdempotencyKey: "key"}
	if err := lockTarget(ctx, c); !errors.Is(err, uow.ErrRequired) {
		t.Fatal(err)
	}
	if err := lockTarget(ctx, contracts.Command{}); !errors.Is(err, scope.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := bump(ctx, ref); !errors.Is(err, uow.ErrRequired) {
		t.Fatal(err)
	}
	if err := owner(ctx, ref, p, true); !errors.Is(err, uow.ErrRequired) {
		t.Fatal(err)
	}
	if err := preserveOwner(ctx, ref, p); !errors.Is(err, uow.ErrRequired) {
		t.Fatal(err)
	}
	s := &Store{}
	if _, err := s.run(ctx, "tenant", "action", "key", nil, nil); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if _, err := s.ListMyOrganizations(ctx, contracts.PageRequest{Limit: 1}); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if _, err := s.ListEnterpriseOrganizations(ctx, ref, contracts.PageRequest{Limit: 1}); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if _, err := s.Export(ctx); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if err := s.Import(ctx, Bundle{}); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if err := s.tenantPermission(ctx, scope.ScopeBinding{Ref: ref}, "action"); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if err := s.targetPermission(ctx, scope.ScopeBinding{}, ref, "action"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.snapshot(nil, p, ref); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
	if _, err := s.snapshot(ctx, p, scope.ScopeRef{}); !errors.Is(err, scope.ErrDenied) {
		t.Fatal(err)
	}
}

func TestMalformedGovernanceCommands(t *testing.T) {
	s := &Store{}
	c := context.Background()
	if _, e := s.CreateEnterprise(c, contracts.CreateEnterprise{Slug: "bad slug"}); e == nil {
		t.Fatal("slug")
	}
	if _, e := s.CreateOrganization(c, contracts.CreateOrganization{}); e == nil {
		t.Fatal("slug")
	}
	if _, e := s.SetOrganizationMembership(c, contracts.SetMembership{}); e == nil {
		t.Fatal("membership")
	}
	for _, e := range []error{s.RemoveOrganizationMembership(c, contracts.RemoveMembership{}), s.SetEnterpriseRole(c, contracts.SetEnterpriseRole{}), s.Suspend(c, contracts.Command{}), s.Resume(c, contracts.Command{}), s.Archive(c, contracts.Command{}), s.JoinAsOwner(c, contracts.Command{}), s.Recover(c, contracts.Command{}, scope.PrincipalRef{})} {
		if !errors.Is(e, scope.ErrInvalid) {
			t.Fatal(e)
		}
	}
	if _, e := s.AssignEnterprise(c, contracts.AssignEnterprise{}); !errors.Is(e, scope.ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := s.list(c, scope.ScopeBinding{}, contracts.PageRequest{Limit: 101}, nil); !errors.Is(e, scope.ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := s.list(c, scope.ScopeBinding{}, contracts.PageRequest{Limit: 1, Cursor: "!"}, nil); !errors.Is(e, scope.ErrInvalid) {
		t.Fatal(e)
	}
	for _, id := range []string{"not-a-uuid", "11111111-1111-4111-8111-zzzzzzzzzzzz"} {
		p, _ := NewProvider(authority{})
		if _, e := p.Resolve(c, scope.PrincipalRef{ID: "alice", Kind: "user"}, scope.ScopeRef{TenantID: "t", Kind: contracts.OrganizationKind, ID: id}); e == nil {
			t.Fatal(id)
		}
	}
}

func TestAdmissionTokenCannotBeForged(t *testing.T) {
	s := &Store{}
	b := scope.ScopeBinding{Ref: scope.ScopeRef{TenantID: "t", Kind: contracts.OrganizationKind, ID: "org"}, Principal: scope.PrincipalRef{ID: "alice", Kind: "user"}}
	for _, token := range []string{"", "!._", "e30.!", base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 8193))) + ".eA", "e30.eA"} {
		b.AuthorityRevision = token
		if _, e := s.verify(b); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(token, e)
		}
	}
	snapshot := Snapshot{Ref: b.Ref, Principal: b.Principal, Role: "owner"}
	b.AuthorityRevision = s.sign(snapshot)
	if _, e := s.verify(b); e != nil {
		t.Fatal(e)
	}
	b.Principal.ID = "bob"
	if _, e := s.verify(b); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if e := s.emit(context.Background(), scope.ScopeBinding{}, scope.ScopeRef{}, "x", 1, make(chan int)); e == nil {
		t.Fatal("unserializable effect")
	}
}

func TestBundleValidation(t *testing.T) {
	ref := scope.ScopeRef{TenantID: "t", Kind: contracts.OrganizationKind, ID: "11111111-1111-4111-8111-111111111111"}
	p := scope.PrincipalRef{Kind: "user", ID: "alice"}
	base := Bundle{Version: 1, Catalog: []CatalogRecord{{Ref: ref, State: "active", Version: 1}}, Organizations: []contracts.Organization{{Ref: ref, Slug: "org", State: "active", Version: 1}}, Memberships: []contracts.Membership{{TenantID: "t", OrganizationID: ref.ID, Principal: p, Role: "owner", State: "active", Version: 1}}}
	base.Owners = []OwnerRecord{{Ref: ref, Principal: p, State: "active"}}
	clone := func() Bundle { raw, _ := json.Marshal(base); var b Bundle; _ = json.Unmarshal(raw, &b); return b }
	for _, change := range []func(*Bundle){func(b *Bundle) { b.Version = 2 }, func(b *Bundle) { b.Catalog[0].Ref.TenantID = "other" }, func(b *Bundle) { b.Catalog = append(b.Catalog, b.Catalog[0]) }, func(b *Bundle) { b.Catalog[0].State = "bad" }, func(b *Bundle) { b.Organizations[0].Slug = "bad slug" }, func(b *Bundle) { b.Organizations[0].Version = 2 }, func(b *Bundle) { b.Organizations[0].EnterpriseID = func() *string { s := "missing"; return &s }() }, func(b *Bundle) { b.Organizations = nil }, func(b *Bundle) { b.Memberships[0].Role = "superuser" }, func(b *Bundle) {
		b.Roles = []RoleRecord{{Ref: ref, Principal: p, Role: "owner", State: "active", Version: 1}}
	}, func(b *Bundle) {
		b.Grants = []GrantRecord{{Ref: ref, Principal: p, Role: "worker", State: "active", Version: 1}}
	}, func(b *Bundle) { b.Owners = []OwnerRecord{{Ref: scope.ScopeRef{}, Principal: p, State: "active"}} }, func(b *Bundle) { b.Restrictions = []Restriction{{Ref: ref, Action: ""}} }} {
		b := clone()
		change(&b)
		if e := validateBundle(b, "t"); !errors.Is(e, scope.ErrInvalid) {
			t.Fatal(b, e)
		}
	}
	if e := validateBundle(base, "t"); e != nil {
		t.Fatal(e)
	}
	missing := clone()
	missing.Owners = nil
	if e := validateBundle(missing, "t"); !errors.Is(e, scope.ErrInvalid) {
		t.Fatal("missing owner projection", e)
	}
	phantom := clone()
	phantom.Owners[0].Principal.ID = "not-a-member"
	if e := validateBundle(phantom, "t"); !errors.Is(e, scope.ErrInvalid) {
		t.Fatal("phantom owner projection", e)
	}
	for _, raw := range [][]byte{[]byte("{"), []byte(`{"version":2}`)} {
		if _, e := DecodeBundle(raw); !errors.Is(e, scope.ErrInvalid) {
			t.Fatal(e)
		}
	}
	if _, e := EncodeBundle(Bundle{}); !errors.Is(e, scope.ErrInvalid) {
		t.Fatal(e)
	}
	if e := Reconcile(Bundle{}, base); e == nil {
		t.Fatal("bad source")
	}
	if e := Reconcile(base, Bundle{}); e == nil {
		t.Fatal("bad target")
	}
	different := clone()
	different.Organizations[0].Slug = "changed"
	if e := Reconcile(base, different); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
}

type tenantAccessFunc func(context.Context, scope.PrincipalRef, string) (string, error)

func (f tenantAccessFunc) Check(c context.Context, p scope.PrincipalRef, t string) (string, error) {
	return f(c, p, t)
}

type noSQLResource struct{}

func (noSQLResource) Begin(c context.Context, _ uow.EffectiveOptions) (context.Context, uow.Transaction, error) {
	return c, noSQLTransaction{}, nil
}

type noSQLTransaction struct{}

func (noSQLTransaction) Commit(context.Context) error   { return nil }
func (noSQLTransaction) Rollback(context.Context) error { return nil }
func TestTrustedGovernanceContextGuards(t *testing.T) {
	identity := func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "alice", Kind: "user"}, "tenant", true
	}
	resolver := scope.NewTenantResolver(identity)
	ctx, e := resolver.Bind(context.Background(), scope.ScopeRef{TenantID: "tenant", Kind: "tenant", ID: "tenant"})
	if e != nil {
		t.Fatal(e)
	}
	s := &Store{options: StoreOptions{Identity: identity, Tenants: tenantAccessFunc(func(context.Context, scope.PrincipalRef, string) (string, error) { return "1", nil })}}
	if _, e := s.run(ctx, "tenant", "action", "", nil, nil); !errors.Is(e, scope.ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := s.run(ctx, "tenant", "action", "key", make(chan int), nil); e == nil {
		t.Fatal("invalid command serialized")
	}
	s.options.Identity = func(context.Context) (scope.PrincipalRef, string, bool) {
		return scope.PrincipalRef{ID: "bob", Kind: "user"}, "tenant", true
	}
	if _, e := s.actor(ctx, "tenant"); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	s.options.Identity = identity
	s.options.Tenants = tenantAccessFunc(func(context.Context, scope.PrincipalRef, string) (string, error) { return "", scope.ErrDenied })
	if _, e := s.actor(ctx, "tenant"); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	s.options.Permissions = PermissionFunc(func(context.Context, Snapshot, string) error { return nil })
	m, _ := uow.NewManager(noSQLResource{}, uow.DefaultOptions{DefaultTimeout: time.Second, CleanupTimeout: time.Second, HookTimeout: time.Second, MaxRequiresNewDepth: 1})
	if e := m.Within(ctx, uow.Options{}, func(work context.Context) error {
		current, _ := scope.Current(work)
		other := current
		other.Ref.ID = "other"
		if e := s.Authorize(work, other, "books.read"); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
		if e := s.Authorize(work, current, "books.read"); !errors.Is(e, scope.ErrDenied) {
			t.Fatal(e)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	s.options.PrivateSchema = "invalid-schema"
	s.options.AuthorityPool = nil
	if _, e := s.snapshot(ctx, scope.PrincipalRef{Kind: "user", ID: "alice"}, scope.ScopeRef{TenantID: "tenant", Kind: contracts.OrganizationKind, ID: "id"}); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
	if e := s.authorizeInput(ctx, scope.ScopeBinding{}, "unknown", struct{}{}); !errors.Is(e, scope.ErrDenied) {
		t.Fatal(e)
	}
}
