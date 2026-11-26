package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cymondez/pangolin-framework/events/distributed"
	"github.com/cymondez/pangolin-framework/events/outbox"
	"github.com/cymondez/pangolin-framework/modules/organization"
	"github.com/cymondez/pangolin-framework/modules/organization/contracts"
	"github.com/cymondez/pangolin-framework/modules/realtime"
	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
)

const (
	enterprise      = "11111111-1111-1111-1111-111111111111"
	enterpriseTwo   = "22222222-2222-2222-2222-222222222222"
	organizationOne = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	organizationTwo = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	demoProducer    = "realtime-demo"
)

type membership struct {
	principal scope.PrincipalRef
	ref       scope.ScopeRef
}

type fixtureAuthority struct {
	settings demoSettings
	mu       sync.RWMutex
	revoked  map[membership]bool
}

func newFixture(settings demoSettings) (*fixtureAuthority, error) {
	switch settings.profile {
	case "tenant-only", "organization-enabled", "replacement-contract":
		return &fixtureAuthority{settings: settings, revoked: make(map[membership]bool)}, nil
	default:
		return nil, errors.New("unknown realtime demo profile")
	}
}

func (a *fixtureAuthority) Kind() string { return identity.BearerKind }
func (a *fixtureAuthority) Verify(ctx context.Context, credential identity.Credential) (identity.Result, error) {
	key, actor, ok := strings.Cut(credential.Value, ":")
	tenant, user, actorOK := strings.Cut(actor, "/")
	principal := scope.PrincipalRef{ID: user, Kind: identity.PrincipalUser}
	if !ok || !actorOK || subtle.ConstantTimeCompare([]byte(key), []byte(a.settings.key)) != 1 ||
		(a.CheckIdentity(principal, tenant) != nil) {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{Status: identity.StatusAuthenticated, Principal: principal, Tenant: tenant, Method: "demo-bearer"}, ctx.Err()
}

func (a *fixtureAuthority) CheckIdentity(p scope.PrincipalRef, tenant string) error {
	if (tenant != "alpha" && tenant != "beta") || p.Kind != identity.PrincipalUser ||
		(p.ID != "alice" && p.ID != "governor" && p.ID != "bob") {
		return scope.ErrDenied
	}
	return nil
}

func (a *fixtureAuthority) Check(ctx context.Context, p scope.PrincipalRef, tenant string) (string, error) {
	if err := a.CheckIdentity(p, tenant); err != nil {
		return "", err
	}
	return "demo-tenant", ctx.Err()
}

func (a *fixtureAuthority) Resolve(ctx context.Context, p scope.PrincipalRef, ref scope.ScopeRef) (scope.ScopeBinding, error) {
	actual, tenant, ok := tenancy.Identity(ctx)
	if !ok || actual != p || tenant != ref.TenantID || !a.validOwner(ref) {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	member := false
	switch ref.Kind {
	case contracts.EnterpriseKind:
		member = p.ID == "governor" && ref.ID == enterprise || p.ID == "bob" && ref.ID == enterpriseTwo
	case contracts.OrganizationKind:
		member = p.ID == "alice" || p.ID == "bob" && ref.ID == organizationTwo
	}
	if !member || a.revoked[membership{p, ref}] {
		return scope.ScopeBinding{}, scope.ErrDenied
	}
	return scope.ScopeBinding{Ref: ref, Principal: p, AuthorityRevision: "demo-member"}, ctx.Err()
}

func (a *fixtureAuthority) Authorize(ctx context.Context, binding scope.ScopeBinding, permission string) error {
	actual, ok := scope.Current(ctx)
	if !ok || actual != binding || !demoPermission(permission) {
		return scope.ErrDenied
	}
	_, err := a.Resolve(ctx, binding.Principal, binding.Ref)
	return err
}

func demoPermission(permission string) bool {
	switch permission {
	case "connect", "query", "publish", "subscribe", "receive", "revoke-self":
		return true
	}
	return false
}

func (a *fixtureAuthority) validOwner(ref scope.ScopeRef) bool {
	if ref.TenantID != "alpha" && ref.TenantID != "beta" {
		return false
	}
	if ref.Kind == "tenant" {
		return ref.ID == ref.TenantID
	}
	if a.settings.profile == "tenant-only" {
		return false
	}
	switch ref.Kind {
	case contracts.EnterpriseKind:
		return ref.ID == enterprise || ref.ID == enterpriseTwo
	case contracts.OrganizationKind:
		return ref.ID == organizationOne || ref.ID == organizationTwo
	}
	return false
}

func (a *fixtureAuthority) authorizeEvent(ctx context.Context, event distributed.Envelope) error {
	if event.Producer != demoProducer || event.Type != realtime.EventType ||
		!a.validOwner(scope.ScopeRef{TenantID: event.TenantID, Kind: event.ScopeKind, ID: event.ScopeID}) {
		return scope.ErrDenied
	}
	return ctx.Err()
}

type replacement struct{ *fixtureAuthority }

func (replacement) Kinds() []string {
	return []string{contracts.EnterpriseKind, contracts.OrganizationKind}
}

func (a *fixtureAuthority) hub(publisher *outbox.Publisher, root *demoRoot) (*realtime.Hub, error) {
	auth, err := identity.NewPipeline(a)
	if err != nil {
		return nil, err
	}
	tenantResolver, err := tenancy.NewResolver(identity.Identity, a)
	if err != nil {
		return nil, err
	}
	providers := []scope.ScopeProvider{tenancy.Provider{Authority: a, Permission: func(ctx context.Context, _ scope.ScopeBinding, permission string) error {
		if !demoPermission(permission) {
			return scope.ErrDenied
		}
		return ctx.Err()
	}}}
	channel := func(name, kind string) realtime.Channel {
		return realtime.Channel{Name: name, Target: "Notification", Kinds: []string{kind},
			Publish: realtime.Access{Permission: "publish"}, Subscribe: realtime.Access{Permission: "subscribe"},
			Receive: realtime.Access{Permission: "receive"}}
	}
	channels := []realtime.Channel{channel("tenant", "tenant")}
	if a.settings.profile != "tenant-only" {
		if a.settings.profile == "organization-enabled" {
			provider, err := organization.NewProvider(a)
			if err != nil {
				return nil, err
			}
			providers = append(providers, provider)
		} else {
			providers = append(providers, replacement{a})
		}
		channels = append(channels, channel("enterprise", contracts.EnterpriseKind), channel("organization", contracts.OrganizationKind))
	}
	scopes, err := scope.NewRegistry(tenancy.Identity, providers...)
	if err != nil {
		return nil, err
	}
	return realtime.New(realtime.Options{
		Producer: demoProducer, Connect: realtime.Access{Permission: "connect"}, Channels: channels,
		MaxConnections: 128, MaxSubscriptions: 16, QueueSize: 32, MaxPayload: 8 << 10, MaxOperations: 32,
		OperationTimeout: 4 * time.Second,
		Methods: []realtime.Method{
			{Name: "Echo", Access: realtime.Access{Permission: "query"}, Invoke: func(ctx context.Context, args []json.RawMessage) (any, error) {
				return root.api.echo(ctx, args)
			}},
			{Name: "Notify", Select: selectDemoScope, Access: realtime.Access{Permission: "publish"}, Invoke: func(ctx context.Context, args []json.RawMessage) (any, error) {
				return root.api.notify(ctx, args)
			}},
			{Name: "RevokeMyMembership", Select: selectDemoScope, Access: realtime.Access{Permission: "revoke-self"},
				Invoke: a.revoke},
		},
	}, auth, tenantResolver, scopes, publisher)
}

func (a *fixtureAuthority) revoke(ctx context.Context, _ []json.RawMessage) (any, error) {
	binding, ok := scope.Current(ctx)
	if !ok || binding.Ref.Kind != contracts.OrganizationKind {
		return nil, scope.ErrDenied
	}
	a.mu.Lock()
	a.revoked[membership{binding.Principal, binding.Ref}] = true
	a.mu.Unlock()
	return true, nil
}
