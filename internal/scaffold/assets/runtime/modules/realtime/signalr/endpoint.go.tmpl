// Package signalr serves the ASP.NET Core SignalR JSON Hub Protocol.
package signalr

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/pangolin-framework/pangolin/modules/realtime"
	"github.com/pangolin-framework/pangolin/security/identity"
	"github.com/pangolin-framework/pangolin/security/scope"
)

type Options struct {
	Path                     string
	Origins                  []string
	AllowLoopbackCredentials bool
	NegotiationLimit         int
	HandshakeConcurrency     int
	NegotiationTTL           time.Duration
	HandshakeTimeout         time.Duration
	ReadTimeout              time.Duration
	WriteTimeout             time.Duration
	KeepAlive                time.Duration
	MaxMessage               int
}

type ticket struct {
	subject realtime.Subject
	expires time.Time
}

type Endpoint struct {
	hub     realtime.Gateway
	options Options
	admit   chan struct{}
	wires   chan struct{}
	mu      sync.Mutex
	stopped bool
	tickets map[string]ticket
}

func New(hub realtime.Gateway, options Options) (*Endpoint, error) {
	if hub == nil || !strings.HasPrefix(options.Path, "/") || strings.HasSuffix(options.Path, "/") ||
		strings.ContainsAny(options.Path, "?#") || options.NegotiationLimit <= 0 || options.HandshakeConcurrency <= 0 ||
		options.NegotiationTTL <= 0 || options.HandshakeTimeout <= 0 || options.WriteTimeout <= 0 ||
		options.KeepAlive <= 0 || options.ReadTimeout <= options.KeepAlive || options.MaxMessage <= 0 || options.MaxMessage > 1<<20 {
		return nil, realtime.ErrInvalid
	}
	for _, value := range options.Origins {
		if _, err := parseOrigin(value); err != nil {
			return nil, err
		}
	}
	options.Origins = slices.Clone(options.Origins)
	return &Endpoint{hub: hub, options: options, admit: make(chan struct{}, options.HandshakeConcurrency),
		wires: make(chan struct{}, hub.ConnectionLimit()), tickets: make(map[string]ticket)}, nil
}

func parseOrigin(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, realtime.ErrInvalid
	}
	return u, nil
}

func (e *Endpoint) origin(w http.ResponseWriter, r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return r.Method != http.MethodOptions
	}
	if len(values) != 1 {
		return false
	}
	u, err := parseOrigin(values[0])
	if err != nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if !strings.EqualFold(u.Host, r.Host) || u.Scheme != scheme {
		if !slices.Contains(e.options.Origins, values[0]) {
			return false
		}
	}
	w.Header().Add("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", values[0])
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	return true
}

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	negotiate := r.URL.Path == e.options.Path+"/negotiate"
	if !negotiate && r.URL.Path != e.options.Path {
		http.NotFound(w, r)
		return
	}
	if !e.origin(w, r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.Method == http.MethodOptions && negotiate {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, X-SignalR-User-Agent")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	e.mu.Lock()
	stopped := e.stopped
	e.mu.Unlock()
	if stopped {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	method := http.MethodGet
	if negotiate {
		method = http.MethodPost
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if negotiate && !slices.Equal(query["negotiateVersion"], []string{"1"}) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !negotiate && (!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || len(query["id"]) != 1) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	credential, err := e.credential(r, query["access_token"], !negotiate)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	select {
	case e.admit <- struct{}{}:
	default:
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	if negotiate {
		defer func() { <-e.admit }()
		e.negotiate(w, r, credential)
		return
	}
	select {
	case e.wires <- struct{}{}:
	default:
		<-e.admit
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	defer func() { <-e.wires }()
	conn, session := e.upgrade(w, r, credential, query.Get("id"))
	<-e.admit
	if conn != nil {
		e.serve(conn, session)
	}
}

func (e *Endpoint) credential(r *http.Request, tokens []string, upgrade bool) (identity.Credential, error) {
	headers := r.Header.Values("Authorization")
	var value string
	switch {
	case len(headers) == 1 && len(tokens) == 0:
		scheme, token, ok := strings.Cut(headers[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return identity.Credential{}, realtime.ErrCredential
		}
		value = token
	case upgrade && len(headers) == 0 && len(tokens) == 1:
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if r.TLS == nil && !(e.options.AllowLoopbackCredentials && net.ParseIP(host).IsLoopback()) {
			return identity.Credential{}, realtime.ErrCredential
		}
		value = tokens[0]
	default:
		return identity.Credential{}, realtime.ErrCredential
	}
	if value == "" || len(value) > identity.MaxCredentialBytes || strings.ContainsAny(value, " \t\r\n,") {
		return identity.Credential{}, realtime.ErrCredential
	}
	return identity.Credential{Kind: identity.BearerKind, Value: value}, nil
}

func authStatus(err error) int {
	switch {
	case errors.Is(err, realtime.ErrCredential), errors.Is(err, identity.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, scope.ErrDenied):
		return http.StatusForbidden
	case errors.Is(err, realtime.ErrCapacity):
		return http.StatusTooManyRequests
	default:
		return http.StatusServiceUnavailable
	}
}

func (e *Endpoint) negotiate(w http.ResponseWriter, r *http.Request, credential identity.Credential) {
	subject, err := e.hub.Authenticate(r.Context(), credential)
	if err != nil {
		w.WriteHeader(authStatus(err))
		return
	}
	token, ok := e.newTicket(subject)
	if !ok {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"connectionId": rand.Text(), "connectionToken": token, "negotiateVersion": 1,
		"availableTransports": []map[string]any{{"transport": "WebSockets", "transferFormats": []string{"Text"}}},
	})
}

func (e *Endpoint) newTicket(subject realtime.Subject) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for token, t := range e.tickets {
		if !now.Before(t.expires) {
			delete(e.tickets, token)
		}
	}
	if e.stopped || len(e.tickets) >= e.options.NegotiationLimit {
		return "", false
	}
	token := rand.Text()
	e.tickets[token] = ticket{subject, now.Add(e.options.NegotiationTTL)}
	return token, true
}

func (e *Endpoint) upgrade(w http.ResponseWriter, r *http.Request, credential identity.Credential, token string) (*websocket.Conn, *realtime.Session) {
	e.mu.Lock()
	t, ok := e.tickets[token]
	e.mu.Unlock()
	if !ok || !time.Now().Before(t.expires) {
		w.WriteHeader(http.StatusNotFound)
		return nil, nil
	}
	session, err := e.hub.Open(r.Context(), credential, t.subject)
	if err != nil {
		status := authStatus(err)
		if errors.Is(err, realtime.ErrCredential) {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		return nil, nil
	}
	e.mu.Lock()
	current, ok := e.tickets[token]
	admitted := !e.stopped && ok && current == t && time.Now().Before(t.expires)
	if admitted {
		delete(e.tickets, token)
	}
	e.mu.Unlock()
	if !admitted {
		session.Close()
		w.WriteHeader(http.StatusNotFound)
		return nil, nil
	}
	// The exact Origin was already checked for both HTTP and WebSocket requests.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		session.Close()
		return nil, nil
	}
	conn.SetReadLimit(int64(e.options.MaxMessage))
	return conn, session
}

func (e *Endpoint) Quiesce(ctx context.Context) error {
	e.mu.Lock()
	e.stopped = true
	clear(e.tickets)
	e.mu.Unlock()
	return e.hub.Quiesce(ctx)
}

func (e *Endpoint) Drain(ctx context.Context) error { return e.hub.Drain(ctx) }
func (e *Endpoint) Cancel()                         { e.hub.Cancel() }
