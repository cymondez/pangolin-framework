// Package realtime routes authorized live notifications to exact resource owners.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/policy"
	"github.com/cymondez/pangolin-framework/security/scope"
)

var (
	ErrInvalid    = errors.New("REALTIME_INVALID")
	ErrClosed     = errors.New("REALTIME_CLOSED")
	ErrCapacity   = errors.New("REALTIME_CAPACITY")
	ErrMethod     = errors.New("REALTIME_METHOD_UNKNOWN")
	ErrCredential = errors.New("REALTIME_CREDENTIAL_INVALID")
)

// Authenticator must consult current credential and account authority on every
// call, then install the identity through the framework identity pipeline.
type Authenticator interface {
	AuthenticateCredential(context.Context, identity.Credential) (context.Context, error)
}

type Access struct {
	Permission string
	Resource   policy.ResourcePolicy
}

// Channel defines independent policies for each action. Target is the
// registered client method. Kinds lists the accepted, exact owner kinds.
type Channel struct {
	Name, Target                string
	Kinds                       []string
	Publish, Subscribe, Receive Access
}

type Destination struct {
	Scope   scope.ScopeRef
	Channel string
	// UserID restricts delivery to that user within the exact owner.
	UserID string
}

// Publisher is the application-facing contract. The current, authorized writable
// UOW must own the destination. The returned ID identifies the staged event.
type Publisher interface {
	Publish(context.Context, Destination, json.RawMessage) (string, error)
}

// Method is declared by trusted composition. Select returns an untrusted scope
// candidate for the current tenant. A nil Select selects the tenant owner.
type Method struct {
	Name   string
	Select func(tenant string, args []json.RawMessage) (scope.ScopeRef, error)
	Access Access
	Invoke func(context.Context, []json.RawMessage) (any, error)
}

type Options struct {
	Producer                                                               string
	Connect                                                                Access
	Channels                                                               []Channel
	Methods                                                                []Method
	MaxConnections, MaxSubscriptions, QueueSize, MaxPayload, MaxOperations int
	OperationTimeout                                                       time.Duration
}

type Subject struct {
	Tenant    string
	Principal scope.PrincipalRef
}
