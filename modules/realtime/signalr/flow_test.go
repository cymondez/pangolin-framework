package signalr

// Additional coverage for endpoint validation, credential parsing and the
// ServeHTTP routing guards; see the RT-002/RT-009 mapping in
// tests/contract/m7.json.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/modules/realtime"
)

func TestNewRejectsInvalidOptions(t *testing.T) {
	valid := Options{Path: "/rt", NegotiationLimit: 4, HandshakeConcurrency: 4,
		NegotiationTTL: time.Second, HandshakeTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second,
		WriteTimeout: time.Second, KeepAlive: time.Second, MaxMessage: 1 << 10,
		Origins: []string{"https://portal.example"}}
	cases := map[string]func(*Options){
		"nil-gateway":        nil,
		"relative-path":      func(o *Options) { o.Path = "rt" },
		"trailing-slash":     func(o *Options) { o.Path = "/rt/" },
		"path-with-query":    func(o *Options) { o.Path = "/rt?" },
		"zero-negotiations":  func(o *Options) { o.NegotiationLimit = 0 },
		"zero-concurrency":   func(o *Options) { o.HandshakeConcurrency = 0 },
		"zero-ttl":           func(o *Options) { o.NegotiationTTL = 0 },
		"zero-handshake":     func(o *Options) { o.HandshakeTimeout = 0 },
		"zero-write":         func(o *Options) { o.WriteTimeout = 0 },
		"zero-keepalive":     func(o *Options) { o.KeepAlive = 0 },
		"read-under-keep":    func(o *Options) { o.ReadTimeout = time.Second },
		"zero-message":       func(o *Options) { o.MaxMessage = 0 },
		"oversize-message":   func(o *Options) { o.MaxMessage = 1<<20 + 1 },
		"origin-with-path":   func(o *Options) { o.Origins = []string{"https://portal.example/path"} },
		"origin-with-query":  func(o *Options) { o.Origins = []string{"https://portal.example/?x=1"} },
		"origin-with-user":   func(o *Options) { o.Origins = []string{"https://user@portal.example"} },
		"origin-with-frag":   func(o *Options) { o.Origins = []string{"https://portal.example#f"} },
		"origin-ftp":         func(o *Options) { o.Origins = []string{"ftp://portal.example"} },
		"origin-no-hostname": func(o *Options) { o.Origins = []string{"x:"} },
	}
	gateway := &stubGateway{}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			options := valid
			if mutate == nil {
				if _, err := New(nil, options); err == nil {
					t.Fatal("nil gateway accepted")
				}
				return
			}
			mutate(&options)
			endpoint, err := New(gateway, options)
			if endpoint != nil {
				endpoint.Cancel()
			}
			if err == nil {
				t.Fatal("invalid option was accepted")
			}
		})
	}
}

func TestParseOriginRejectsInvalidForms(t *testing.T) {
	for _, value := range []string{"://x", "https:/portal.example", "https://portal.example/path", "https://user@portal.example"} {
		if _, err := parseOrigin(value); !errors.Is(err, realtime.ErrInvalid) {
			t.Fatalf("origin %q accepted: %v", value, err)
		}
	}
}

func TestCredentialParsing(t *testing.T) {
	options := stubOptions()
	options.AllowLoopbackCredentials = true
	endpoint, err := New(&stubGateway{}, options)
	if err != nil {
		t.Fatal(err)
	}
	request := func(remote, method, value string) *http.Request {
		r := httptest.NewRequest(method, "http://example.com/rt?id=ticket", nil)
		r.RemoteAddr = remote
		_ = value
		return r
	}
	// Bearer header credentials parse into the shared credential kind.
	bearer := request("192.0.2.1:1234", http.MethodGet, "")
	bearer.Header.Set("Authorization", "Bearer alpha")
	if _, err := endpoint.credential(bearer, nil, false); err != nil {
		t.Fatalf("bearer credential rejected: %v", err)
	}
	// A wrong scheme is refused outright.
	rejected := request("192.0.2.1:1234", http.MethodGet, "")
	rejected.Header.Set("Authorization", "Basic zzz")
	if _, err := endpoint.credential(rejected, nil, false); !errors.Is(err, realtime.ErrCredential) {
		t.Fatalf("basic credential accepted: %v", err)
	}
	// Upgrade credentials from the query parameter require a loopback (or TLS)
	// peer address; a non-loopback HTTP request cannot carry them.
	if _, err := endpoint.credential(request("192.0.2.1:1234", http.MethodGet, "alpha"), []string{"alpha"}, true); !errors.Is(err, realtime.ErrCredential) {
		t.Fatalf("non-loopback query credential accepted: %v", err)
	}
	if _, err := endpoint.credential(request("127.0.0.1:1234", http.MethodGet, "alpha"), []string{"alpha"}, true); err != nil {
		t.Fatalf("loopback query credential rejected: %v", err)
	}
	// The query credential must stay short, clean and unambiguous.
	for _, token := range []string{"has space", "with,comma", "tab\there"} {
		if _, err := endpoint.credential(request("127.0.0.1:1234", http.MethodGet, token), []string{token}, true); !errors.Is(err, realtime.ErrCredential) {
			t.Fatalf("dirty credential accepted: %q", token)
		}
	}
	if _, err := endpoint.credential(request("127.0.0.1:1234", http.MethodGet, ""), []string{"a", "b"}, true); !errors.Is(err, realtime.ErrCredential) {
		t.Fatalf("ambiguous credential accepted: %v", err)
	}
}

func TestServeHTTPRoutingGuards(t *testing.T) {
	gateway := &stubGateway{}
	endpoint, err := New(gateway, stubOptions())
	if err != nil {
		t.Fatal(err)
	}
	serve := func(method, path string, headers map[string]string) int {
		request := httptest.NewRequest(method, "http://example.com"+path, nil)
		request.Header.Set("Origin", "http://example.com")
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		recorder := httptest.NewRecorder()
		endpoint.ServeHTTP(recorder, request)
		return recorder.Code
	}
	if code := serve(http.MethodGet, "/rt/other", nil); code != http.StatusNotFound {
		t.Fatalf("unknown path accepted: %d", code)
	}
	// A preflight OPTIONS echoes the allowed methods for the negotiate path.
	request := httptest.NewRequest(http.MethodOptions, "http://example.com/rt/negotiate", nil)
	request.Header.Set("Origin", "http://example.com")
	recorder := httptest.NewRecorder()
	endpoint.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("unexpected preflight response %d %v", recorder.Code, recorder.Header())
	}
	// Method mismatches announce the allowed method.
	if code := serve(http.MethodGet, "/rt/negotiate?negotiateVersion=1", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("negotiate accepted GET: %d", code)
	}
	// Unparseable negotiate queries are rejected outright.
	if code := serve(http.MethodPost, "/rt/negotiate?negotiateVersion=%zz",
		map[string]string{"Authorization": "Bearer alpha"}); code != http.StatusBadRequest {
		t.Fatalf("malformed query accepted: %d", code)
	}
	// Upgrade requests need the websocket upgrade header and a ticket id.
	if code := serve(http.MethodGet, "/rt?id=ticket",
		map[string]string{"Authorization": "Bearer alpha"}); code != http.StatusBadRequest {
		t.Fatalf("non-websocket upgrade accepted: %d", code)
	}
	// Negotiations without a credential are unauthorized.
	if code := serve(http.MethodPost, "/rt/negotiate?negotiateVersion=1", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous negotiation accepted: %d", code)
	}
	// A full admission slot refuses further negotiations.
	for {
		select {
		case endpoint.admit <- struct{}{}:
			continue
		default:
		}
		break
	}
	if code := serve(http.MethodPost, "/rt/negotiate?negotiateVersion=1",
		map[string]string{"Authorization": "Bearer alpha"}); code != http.StatusTooManyRequests {
		t.Fatalf("over-committed negotiation accepted: %d", code)
	}
	<-endpoint.admit
	// A full wire budget refuses further upgrades.
	for i := 0; i < gateway.ConnectionLimit(); i++ {
		endpoint.wires <- struct{}{}
	}
	if code := serve(http.MethodGet, "/rt?id=ticket",
		map[string]string{"Upgrade": "websocket", "Authorization": "Bearer alpha"}); code != http.StatusTooManyRequests {
		t.Fatalf("over-committed upgrade accepted: %d", code)
	}
	for i := 0; i < gateway.ConnectionLimit(); i++ {
		<-endpoint.wires
	}
}
