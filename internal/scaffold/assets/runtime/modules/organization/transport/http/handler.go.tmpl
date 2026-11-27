// Package http adapts the public organization Management Port to bounded HTTP
// commands. Authentication, Tenant/Scope selection and entry permission precede
// decoding; management retains its own authorization and transaction policy.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/pangolin-framework/pangolin/modules/organization"
	"github.com/pangolin-framework/pangolin/modules/organization/contracts"
	"github.com/pangolin-framework/pangolin/persistence/uow"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
	transport "github.com/pangolin-framework/pangolin/transport/http"
)

type Governance interface {
	contracts.Management
	JoinAsOwner(context.Context, contracts.Command) error
	Recover(context.Context, contracts.Command, scope.PrincipalRef) error
}
type Options struct {
	Management   Governance
	Manager      uow.UnitOfWorkManager
	Timeout      time.Duration
	MaxBodyBytes int64
	// Authenticate is the legacy Bearer token entry point.
	Authenticate func(context.Context, string) (context.Context, error)
	// AuthenticateCredential verifies a full extracted credential; use it with
	// Credentials for pluggable request authentication. Exactly one of the
	// two authenticators must be configured.
	AuthenticateCredential func(context.Context, identity.Credential) (context.Context, error)
	// Credentials is the request credential extractor forwarded to the common
	// pipeline; nil keeps the default Bearer extraction.
	Credentials   transport.CredentialExtractor
	ResolveTenant func(context.Context, *http.Request) (context.Context, error)
	ResolveScope  func(context.Context, *http.Request) (context.Context, error)
	Authorize     func(context.Context, string) error
	Rejected      func(context.Context, error)
}
type request struct {
	Slug            string             `json:"slug"`
	EnterpriseID    *string            `json:"enterprise_id"`
	ExpectedVersion int64              `json:"expected_version"`
	Principal       scope.PrincipalRef `json:"principal"`
	Role            string             `json:"role"`
}
type safeError struct {
	cause  error
	code   string
	status int
}

func (e safeError) Error() string   { return e.code }
func (e safeError) Unwrap() error   { return e.cause }
func (e safeError) Code() string    { return e.code }
func (e safeError) HTTPStatus() int { return e.status }
func mapped(err error) error {
	if err == nil {
		return nil
	}
	for _, entry := range []struct {
		sentinel error
		status   int
	}{{organization.ErrNotFound, 404}, {organization.ErrConflict, 409}, {organization.ErrLastOwner, 409}, {organization.ErrInUse, 409}} {
		if errors.Is(err, entry.sentinel) {
			return safeError{cause: err, code: entry.sentinel.Error(), status: entry.status}
		}
	}
	return err
}

// NewHandler registers only the optional organization control plane. The caller
// owns all pools, services and HTTP server shutdown; this adapter owns none.
func NewHandler(o Options) (http.Handler, error) {
	if o.Management == nil || o.Manager == nil || o.Timeout <= 0 || o.MaxBodyBytes <= 0 || (o.Authenticate == nil) == (o.AuthenticateCredential == nil) || o.ResolveTenant == nil || o.ResolveScope == nil || o.Authorize == nil {
		return nil, scope.ErrInvalid
	}
	mux := http.NewServeMux()
	for _, route := range []struct {
		pattern, action string
		read            bool
	}{
		{"POST /enterprises", "tenant.enterprise.create", false},
		{"POST /organizations", "organization.create", false},
		{"GET /organizations", "organization.directory", true},
		{"GET /enterprises/{id}/organizations", "enterprise.organizations.list", true},
		{"PUT /organizations/{id}/memberships", "organization.membership.set", false},
		{"DELETE /organizations/{id}/memberships", "organization.membership.remove", false},
		{"PUT /enterprises/{id}/roles", "enterprise.role.set", false},
		{"PUT /organizations/{id}/enterprise", "organization.transfer", false},
		{"POST /organizations/{id}/join-as-owner", "enterprise.organization.join-as-owner", false},
		{"POST /scopes/{kind}/{id}/suspend", "scope.suspended", false},
		{"POST /scopes/{kind}/{id}/resume", "scope.active", false},
		{"POST /scopes/{kind}/{id}/archive", "scope.archived", false},
		{"POST /scopes/{kind}/{id}/recover", "tenant.scope.recover", false},
	} {
		mux.HandleFunc(route.pattern, func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			p, _ := transport.NewPipeline(transport.PipelineOptions{Timeout: o.Timeout, MaxBodyBytes: o.MaxBodyBytes, Manager: o.Manager, ReadOnly: route.read, Authenticate: o.Authenticate, AuthenticateCredential: o.AuthenticateCredential, Credentials: o.Credentials, ResolveTenant: o.ResolveTenant, ResolveScope: o.ResolveScope, Rejected: o.Rejected,
				Entry: func(c context.Context) error { return mapped(o.Authorize(c, route.action)) },
				Decode: func(raw []byte) (any, error) {
					if kind := r.PathValue("kind"); kind != "" && kind != contracts.OrganizationKind && kind != contracts.EnterpriseKind {
						return nil, scope.ErrInvalid
					}
					if route.read {
						if len(bytes.TrimSpace(raw)) != 0 {
							return nil, scope.ErrInvalid
						}
						limit, e := strconv.Atoi(r.URL.Query().Get("limit"))
						if e != nil || limit < 1 || limit > 100 {
							return nil, scope.ErrInvalid
						}
						return contracts.PageRequest{Limit: limit, Cursor: r.URL.Query().Get("cursor")}, nil
					}
					if key == "" || len(key) > 128 {
						return nil, scope.ErrInvalid
					}
					var dto request
					decoder := json.NewDecoder(bytes.NewReader(raw))
					decoder.DisallowUnknownFields()
					if decoder.Decode(&dto) != nil || !json.Valid(raw) {
						return nil, scope.ErrInvalid
					}
					if route.action == "tenant.enterprise.create" || route.action == "organization.create" {
						if dto.Slug == "" {
							return nil, scope.ErrInvalid
						}
					} else if dto.ExpectedVersion < 1 {
						return nil, scope.ErrInvalid
					}
					return dto, nil
				},
				Work: func(c context.Context, input any) (any, error) {
					b, ok := scope.Current(c)
					if !ok {
						return nil, scope.ErrDenied
					}
					target := scope.ScopeRef{TenantID: b.Ref.TenantID, Kind: contracts.OrganizationKind, ID: r.PathValue("id")}
					if route.action == "enterprise.role.set" || route.action == "enterprise.organizations.list" {
						target.Kind = contracts.EnterpriseKind
					}
					if kind := r.PathValue("kind"); kind != "" {
						target.Kind = kind
					}
					var result any
					var err error
					if route.read {
						page := input.(contracts.PageRequest)
						if route.action == "organization.directory" {
							result, err = o.Management.ListMyOrganizations(c, page)
						} else {
							result, err = o.Management.ListEnterpriseOrganizations(c, target, page)
						}
						return result, mapped(err)
					}
					dto := input.(request)
					command := contracts.Command{Target: target, ExpectedVersion: dto.ExpectedVersion, IdempotencyKey: key}
					switch route.action {
					case "tenant.enterprise.create":
						result, err = o.Management.CreateEnterprise(c, contracts.CreateEnterprise{TenantID: b.Ref.TenantID, Slug: dto.Slug, IdempotencyKey: key})
					case "organization.create":
						result, err = o.Management.CreateOrganization(c, contracts.CreateOrganization{TenantID: b.Ref.TenantID, Slug: dto.Slug, EnterpriseID: dto.EnterpriseID, IdempotencyKey: key})
					case "organization.membership.set":
						result, err = o.Management.SetOrganizationMembership(c, contracts.SetMembership{Command: command, Principal: dto.Principal, Role: dto.Role})
					case "organization.membership.remove":
						err = o.Management.RemoveOrganizationMembership(c, contracts.RemoveMembership{Command: command, Principal: dto.Principal})
					case "enterprise.role.set":
						err = o.Management.SetEnterpriseRole(c, contracts.SetEnterpriseRole{Command: command, Principal: dto.Principal, Role: dto.Role})
					case "organization.transfer":
						result, err = o.Management.AssignEnterprise(c, contracts.AssignEnterprise{Command: command, EnterpriseID: dto.EnterpriseID})
					case "enterprise.organization.join-as-owner":
						err = o.Management.JoinAsOwner(c, command)
					case "scope.suspended":
						err = o.Management.Suspend(c, command)
					case "scope.active":
						err = o.Management.Resume(c, command)
					case "scope.archived":
						err = o.Management.Archive(c, command)
					case "tenant.scope.recover":
						err = o.Management.Recover(c, command, dto.Principal)
					}
					if result == nil {
						result = struct{}{}
					}
					return result, mapped(err)
				},
			})
			p.ServeHTTP(w, r)
		})
	}
	return mux, nil
}
