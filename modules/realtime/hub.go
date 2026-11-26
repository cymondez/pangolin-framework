package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/cymondez/pangolin-framework/events/outbox"
	"github.com/cymondez/pangolin-framework/kernel/runtime"
	"github.com/cymondez/pangolin-framework/observability"
	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/policy"
	"github.com/cymondez/pangolin-framework/security/scope"
	"github.com/cymondez/pangolin-framework/tenancy"
)

// Hub is owned by the transport/composition. Application code uses Publisher.
type Hub struct {
	options    Options
	auth       Authenticator
	tenants    *tenancy.Resolver
	scopes     *scope.Registry
	outbox     *outbox.Publisher
	methods    map[string]Method
	channels   map[string]Channel
	work       *runtime.WorkGroup
	operations chan struct{}
	mu         sync.Mutex
	closing    bool
	sessions   map[*Session]struct{}
}

func (h *Hub) ConnectionLimit() int { return h.options.MaxConnections }

func New(options Options, auth Authenticator, tenants *tenancy.Resolver, scopes *scope.Registry, publisher *outbox.Publisher) (*Hub, error) {
	if auth == nil || tenants == nil || scopes == nil || options.Connect.Permission == "" ||
		options.MaxConnections <= 0 || options.MaxSubscriptions <= 0 || options.QueueSize <= 0 ||
		options.MaxPayload <= 0 || options.MaxPayload > 256<<10 || options.QueueSize > (8<<20)/options.MaxPayload ||
		options.MaxOperations <= 0 || options.OperationTimeout <= 0 || !name(options.Producer) {
		return nil, ErrInvalid
	}
	if err := scopes.Require("tenant"); err != nil {
		return nil, err
	}
	h := &Hub{
		options: options, auth: auth, tenants: tenants, scopes: scopes, outbox: publisher,
		methods: make(map[string]Method), channels: make(map[string]Channel),
		work: runtime.NewWorkGroup(), operations: make(chan struct{}, options.MaxOperations),
		sessions: make(map[*Session]struct{}),
	}
	for _, method := range options.Methods {
		key := strings.ToLower(method.Name)
		if !name(method.Name) || method.Invoke == nil || method.Access.Permission == "" ||
			key == "subscribe" || key == "unsubscribe" || h.methods[key].Invoke != nil {
			return nil, fmt.Errorf("%w: invalid or duplicate method", ErrInvalid)
		}
		h.methods[key] = method
	}
	for _, channel := range options.Channels {
		if !name(channel.Name) || !name(channel.Target) || len(channel.Kinds) == 0 ||
			channel.Publish.Permission == "" || channel.Subscribe.Permission == "" ||
			channel.Receive.Permission == "" || h.channels[channel.Name].Name != "" {
			return nil, fmt.Errorf("%w: invalid or duplicate channel", ErrInvalid)
		}
		if err := scopes.Require(channel.Kinds...); err != nil {
			return nil, err
		}
		channel.Kinds = slices.Clone(channel.Kinds)
		h.channels[channel.Name] = channel
	}
	return h, nil
}

func name(value string) bool {
	return value != "" && len(value) <= 128 && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r))
	}) < 0
}

func (h *Hub) authenticate(ctx context.Context, credential identity.Credential) (context.Context, Subject, error) {
	verified, err := h.auth.AuthenticateCredential(ctx, credential)
	if err != nil {
		if errors.Is(err, identity.ErrUnauthenticated) {
			err = errors.Join(ErrCredential, err)
		}
		return nil, Subject{}, err
	}
	verified, err = h.tenants.Resolve(verified, "", "")
	if err != nil {
		return nil, Subject{}, err
	}
	p, tenant, ok := tenancy.Identity(verified)
	if !ok || p.Kind != identity.PrincipalUser {
		return nil, Subject{}, ErrCredential
	}
	return verified, Subject{Tenant: tenant, Principal: p}, nil
}

// Authenticate establishes the subject for negotiation or upgrade.
func (h *Hub) Authenticate(ctx context.Context, credential identity.Credential) (Subject, error) {
	ctx, cancel := context.WithTimeout(ctx, h.options.OperationTimeout)
	defer cancel()
	verified, subject, err := h.authenticate(ctx, credential)
	if err == nil {
		_, err = h.bind(verified, tenantScope(subject.Tenant), h.options.Connect)
	}
	return subject, err
}

func tenantScope(tenant string) scope.ScopeRef {
	return scope.ScopeRef{TenantID: tenant, Kind: "tenant", ID: tenant}
}

func (h *Hub) bind(ctx context.Context, ref scope.ScopeRef, access Access) (context.Context, error) {
	bound, err := h.scopes.Bind(ctx, ref)
	if err != nil {
		return nil, err
	}
	if err := policy.Require(bound, h.scopes, access.Permission, access.Resource); err != nil {
		return nil, err
	}
	return bound, bound.Err()
}

// Open admits a verified connection. Transports must keep the credential
// private and pass the subject returned by Authenticate on the upgrade request.
func (h *Hub) Open(ctx context.Context, credential identity.Credential, expected Subject) (*Session, error) {
	subject, err := h.Authenticate(ctx, credential)
	if err != nil {
		return nil, err
	}
	if subject != expected {
		return nil, ErrCredential
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.closing {
		return nil, ErrClosed
	}
	if len(h.sessions) >= h.options.MaxConnections {
		return nil, ErrCapacity
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{hub: h, ctx: ctx, cancel: cancel, credential: credential, subject: subject,
		queue: make(chan Delivery, h.options.QueueSize), subscriptions: make(map[subscription]uint64)}
	h.sessions[s] = struct{}{}
	return s, nil
}

func (h *Hub) Quiesce(ctx context.Context) error {
	h.mu.Lock()
	h.closing = true
	h.mu.Unlock()
	return h.work.Quiesce(ctx)
}

func (h *Hub) Drain(ctx context.Context) error {
	err := h.work.Drain(ctx)
	h.Cancel()
	return err
}

func (h *Hub) Cancel() {
	_ = h.Quiesce(context.Background())
	h.work.Cancel()
	h.mu.Lock()
	sessions := maps.Clone(h.sessions)
	h.mu.Unlock()
	for s := range sessions {
		s.Close()
	}
}

type subscription struct {
	ref     scope.ScopeRef
	channel string
}

type Delivery struct {
	recipient    *Session
	subscription subscription
	revision     uint64
	event        Notification
}

// Session is a transport-owned handle. It stores a credential, never a bound
// operation context or a database executor.
type Session struct {
	hub           *Hub
	ctx           context.Context
	cancel        context.CancelFunc
	credential    identity.Credential
	subject       Subject
	queue         chan Delivery
	mu            sync.Mutex
	closeOnce     sync.Once
	subscriptions map[subscription]uint64
	nextRevision  uint64
}

func (s *Session) Context() context.Context { return s.ctx }

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.hub.mu.Lock()
		delete(s.hub.sessions, s)
		s.hub.mu.Unlock()
		s.mu.Lock()
		clear(s.subscriptions)
		s.mu.Unlock()
	})
}

func (s *Session) verify(ctx context.Context) (context.Context, error) {
	verified, subject, err := s.hub.authenticate(ctx, s.credential)
	if err != nil {
		if errors.Is(err, ErrCredential) || errors.Is(err, scope.ErrDenied) {
			s.Close()
		}
		return nil, err
	}
	if subject != s.subject {
		s.Close()
		return nil, ErrCredential
	}
	verified, err = s.hub.bind(verified, tenantScope(subject.Tenant), s.hub.options.Connect)
	if errors.Is(err, scope.ErrDenied) {
		s.Close()
	}
	return verified, err
}

func (h *Hub) enter(ctx context.Context) (context.Context, func(), error) {
	select {
	case h.operations <- struct{}{}:
	default:
		return nil, nil, ErrCapacity
	}
	ctx, done, err := h.work.Enter(ctx)
	if err != nil {
		<-h.operations
		return nil, nil, errors.Join(ErrClosed, err)
	}
	ctx, cancel := context.WithTimeout(ctx, h.options.OperationTimeout)
	return observability.WithOperation(ctx, ""), func() { cancel(); done(); <-h.operations }, nil
}

// Invoke admits one independent operation. The caller must pass a context
// derived from Context, with protocol cancellation if supported.
func (s *Session) Invoke(ctx context.Context, target string, args []json.RawMessage) (any, error) {
	if ctx == nil {
		return nil, ErrInvalid
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	operation, cancel := context.WithCancel(s.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer cancel()
	defer stop()
	ctx, done, err := s.hub.enter(operation)
	if err != nil {
		return nil, err
	}
	defer done()
	verified, err := s.verify(ctx)
	if err != nil {
		return nil, err
	}
	if key := strings.ToLower(target); key == "subscribe" || key == "unsubscribe" {
		return nil, s.changeSubscription(verified, args, key == "subscribe")
	}
	method, exists := s.hub.methods[strings.ToLower(target)]
	if !exists {
		return nil, ErrMethod
	}
	ref := tenantScope(s.subject.Tenant)
	if method.Select != nil {
		ref, err = method.Select(s.subject.Tenant, args)
		if err != nil {
			return nil, err
		}
	}
	bound, err := s.hub.bind(verified, ref, method.Access)
	if err != nil {
		return nil, err
	}
	return method.Invoke(bound, args)
}

func (s *Session) changeSubscription(ctx context.Context, args []json.RawMessage, join bool) error {
	var request struct {
		Tenant  string
		Kind    string
		ID      string
		Channel string
	}
	if len(args) != 1 {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(args[0]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return errors.Join(ErrInvalid, err)
	}
	ref := scope.ScopeRef{TenantID: request.Tenant, Kind: request.Kind, ID: request.ID}
	if ref.TenantID != s.subject.Tenant {
		return scope.ErrDenied
	}
	channel, ok := s.hub.channel(ref, request.Channel)
	if !ok {
		return scope.ErrDenied
	}
	if join {
		if _, err := s.hub.bind(ctx, ref, channel.Subscribe); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	key := subscription{ref, request.Channel}
	if !join {
		delete(s.subscriptions, key)
		return nil
	}
	if s.subscriptions[key] != 0 {
		return nil
	}
	if len(s.subscriptions) >= s.hub.options.MaxSubscriptions {
		return ErrCapacity
	}
	s.nextRevision++
	s.subscriptions[key] = s.nextRevision
	return nil
}

func (h *Hub) channel(ref scope.ScopeRef, name string) (Channel, bool) {
	channel, ok := h.channels[name]
	return channel, ok && scope.Validate(ref) == nil && slices.Contains(channel.Kinds, ref.Kind)
}
