package authn

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cymondez/pangolin-framework/security/identity"
)

// jwksFixture is a deterministic JWKS transport: every attempt is counted at
// entry, and optional gate channels coordinate single in-flight fetches for
// the coalescing and cancellation tests.
type jwksFixture struct {
	status  int
	err     error
	body    string
	fetches uint32
	entered chan struct{}
	release chan struct{}
}

func (f *jwksFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	atomic.AddUint32(&f.fetches, 1)
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

// serve swaps the current document between phases; phases run on one
// goroutine or after all prior goroutines joined, so this stays race-clean.
func (f *jwksFixture) serve(status int, body string) {
	f.status, f.err, f.body = status, nil, body
}

func (f *jwksFixture) attempts() uint32 { return atomic.LoadUint32(&f.fetches) }

type jwk map[string]any

func rsaJWK(kid string, key *rsa.PrivateKey) jwk {
	return jwk{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}
}

func jwksDocument(t *testing.T, keys ...jwk) string {
	t.Helper()
	raw, e := json.Marshal(map[string]any{"keys": keys})
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}

func mintRaw(t *testing.T, key *rsa.PrivateKey, header map[string]any, payload any) string {
	t.Helper()
	h, e := json.Marshal(header)
	if e != nil {
		t.Fatal(e)
	}
	p, e := json.Marshal(payload)
	if e != nil {
		t.Fatal(e)
	}
	text := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	hash := sha256.Sum256([]byte(text))
	signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if e != nil {
		t.Fatal(e)
	}
	return text + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func mint(t *testing.T, key *rsa.PrivateKey, kid string, payload any) string {
	t.Helper()
	return mintRaw(t, key, map[string]any{"alg": "RS256", "kid": kid}, payload)
}

// watchedCtx is test-only synchronization: it wraps a cancelable context and
// signals, exactly when refreshKeys evaluates its select (context Done), that
// this joiner has reached the in-flight wait. It lets tests release or
// cancel joiners deterministically and adds no production hook; direct Verify
// calls use it, bypassing the pipeline.
type watchedCtx struct {
	context.Context
	waiting chan struct{}
	done    <-chan struct{}
}

func watchContext(ctx context.Context) *watchedCtx {
	return &watchedCtx{Context: ctx, waiting: make(chan struct{}, 16), done: ctx.Done()}
}

func (w *watchedCtx) Done() <-chan struct{} {
	select {
	case w.waiting <- struct{}{}:
	default:
	}
	return w.done
}

// bearerClaims builds the shared valid claim set for one clock instant.
func bearerClaims(clock time.Time) map[string]any {
	return map[string]any{"iss": "https://idp.example", "aud": "bookstore",
		"sub": "alice", "tenant_id": "tenant-a",
		"exp": clock.Add(time.Hour).Unix(), "nbf": clock.Add(-time.Minute).Unix()}
}

// clockedValidator returns a validator with a manually advanced clock shared
// by claims, TTL and cooldown decisions; tests must not move the clock while
// verify goroutines are running.
func clockedValidator(t *testing.T, f *jwksFixture, policy KeyPolicy) (*Validator, *time.Time) {
	t.Helper()
	current := time.Unix(1000000000, 0)
	v := NewValidatorWithPolicy("https://idp.example", "bookstore", "https://idp.example/jwks", &http.Client{Transport: f}, policy)
	v.now = func() time.Time { return current }
	return v, &current
}

func newJWKSFixture(t *testing.T, keys ...jwk) (*jwksFixture, string) {
	t.Helper()
	fixture := &jwksFixture{status: http.StatusOK}
	document := jwksDocument(t, keys...)
	fixture.serve(http.StatusOK, document)
	return fixture, document
}

// The published test name is mapped by tests/contract/m3.json to the A12
// cases (valid, forged, issuer, audience, algorithm, expired, jwks-rotation);
// it keeps covering all of them under the bounded cache policy.
func TestJWTSignatureClaimsAndRotation(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture, _ := newJWKSFixture(t, rsaJWK("current", key))
	v := NewValidator("https://idp.example/", "bookstore", "https://idp.example/jwks", &http.Client{Transport: fixture})
	claims := func() map[string]any { return bearerClaims(time.Now()) }
	token := mint(t, key, "current", claims())
	ctx, e := v.Authenticate(context.Background(), token)
	if e != nil {
		t.Fatal(e)
	}
	principal, tenant, ok := Identity(ctx)
	if !ok || principal.ID != "alice" || tenant != "tenant-a" {
		t.Fatal(principal, tenant)
	}
	if _, _, ok := Identity(nil); ok {
		t.Fatal("nil identity")
	}
	if _, _, ok := Identity(context.Background()); ok {
		t.Fatal("forged identity")
	}
	array := claims()
	array["aud"] = []string{"other", "bookstore"}
	if _, e = v.Authenticate(context.Background(), mint(t, key, "current", array)); e != nil {
		t.Fatal(e)
	}
	for _, change := range []struct {
		key   string
		value any
	}{{"iss", "wrong"}, {"aud", "wrong"}, {"aud", 42}, {"sub", ""}, {"tenant_id", ""}, {"exp", 0}, {"nbf", time.Now().Add(time.Hour).Unix()}} {
		c := claims()
		c[change.key] = change.value
		if _, e = v.Authenticate(context.Background(), mint(t, key, "current", c)); !errors.Is(e, ErrUnauthenticated) {
			t.Fatal(change, e)
		}
	}
	for _, token := range []string{"", strings.Repeat("x", 16385), "!.!.!",
		mintRaw(t, key, map[string]any{"alg": "none", "kid": "current"}, claims()),
		mintRaw(t, key, map[string]any{"alg": "RS256"}, claims()),
		mint(t, key, "current", "bad-claims"), strings.TrimSuffix(token, "=") + "x",
		strings.Join(strings.Split(token, ".")[:2], ".") + ".!"} {
		if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
			t.Fatal("invalid token", e)
		}
	}
	// Precheck failures (malformed structure, header, claims) never reach
	// the key endpoint; a signature-invalid token that passes prechecks
	// still exercises the key path by design and is covered below and by
	// the unknown-kid TTL tests.
	if fixture.attempts() != 1 {
		t.Fatal("precheck fraud must not trigger JWKS fetches", fixture.attempts())
	}
	if _, e = v.Authenticate(nil, token); e == nil {
		t.Fatal("nil context")
	}
	if _, e = NewValidator("", "", "", nil).Authenticate(context.Background(), token); e == nil {
		t.Fatal("unconfigured")
	}
	// Malformed documents fail the attempt and enter cooldown: the immediate
	// retry stays rejected without another fetch, and the existing snapshot
	// is retained. Only a present array-valued keys member is authoritative
	// (withdrawal is covered by its dedicated test).
	for _, body := range []string{"{", strings.Repeat(" ", 65537), "{}", `{"keys":null}`, `{"keys":42}`, `{"keys":"x"}`, `{"keys":{}}`,
		`{"keys":[{"kid":"current","alg":"RS256","use":"sig","kty":"RSA","n":"!","e":"AQAB"}]}`,
		`{"keys":[{"kid":"current","alg":"RS256","use":"sig","kty":"RSA","n":"AQ","e":"Ag"}]}`} {
		source := &jwksFixture{}
		source.serve(http.StatusOK, body)
		broken := NewValidator("https://idp.example", "bookstore", "https://idp.example/jwks", &http.Client{Transport: source})
		for i := 0; i < 2; i++ {
			if _, e = broken.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
				t.Fatal("invalid JWKS", e)
			}
		}
		if source.attempts() != 1 {
			t.Fatal("a failing source must not be retried within cooldown", source.attempts())
		}
	}
	for _, failure := range []struct {
		status int
		err    error
	}{{http.StatusInternalServerError, nil}, {http.StatusOK, errors.New("offline")}} {
		source := &jwksFixture{}
		source.status, source.err = failure.status, failure.err
		down := NewValidator("https://idp.example", "bookstore", "https://idp.example/jwks", &http.Client{Transport: source})
		for i := 0; i < 2; i++ {
			if _, e = down.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
				t.Fatal("unavailable source", e)
			}
		}
		if source.attempts() != 1 {
			t.Fatal("an unavailable source must not be retried within cooldown", source.attempts())
		}
	}
	// Same-kid key change stays hidden until the TTL expires; the cached set
	// is trusted inside the TTL, and the old signature never verifies after
	// the refresh replaced it.
	rotation, _ := newJWKSFixture(t, rsaJWK("current", key))
	rotated, clock := clockedValidator(t, rotation, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second})
	oldToken := mint(t, key, "current", bearerClaims(*clock))
	if _, e = rotated.Authenticate(context.Background(), oldToken); e != nil {
		t.Fatal(e)
	}
	other, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	rotation.serve(http.StatusOK, jwksDocument(t, rsaJWK("current", other)))
	if _, e = rotated.Authenticate(context.Background(), oldToken); e != nil {
		t.Fatal("the fetched set must be trusted within the TTL", e)
	}
	if rotation.attempts() != 1 {
		t.Fatal("TTL-fresh rotations must not refetch", rotation.attempts())
	}
	*clock = clock.Add(2 * time.Minute)
	if _, e = rotated.Authenticate(context.Background(), oldToken); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("same-kid rotation must fail the old signature", e)
	}
	if rotation.attempts() != 2 {
		t.Fatal(rotation.attempts())
	}
	if _, e = rotated.Authenticate(context.Background(), mint(t, other, "current", bearerClaims(*clock))); e != nil {
		t.Fatal("rotated key must verify after refresh", e)
	}
	if rotation.attempts() != 2 {
		t.Fatal(rotation.attempts())
	}
	// Overlapping rotation: both kids of one document verify from one fetch.
	overlapping, _ := newJWKSFixture(t, rsaJWK("current", key), rsaJWK("next", other))
	overlap, clock := clockedValidator(t, overlapping, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second})
	first := mint(t, key, "current", bearerClaims(*clock))
	second := mint(t, other, "next", bearerClaims(*clock))
	for _, bearer := range []string{first, second, first} {
		if _, e = overlap.Authenticate(context.Background(), bearer); e != nil {
			t.Fatal("overlap rotation", e)
		}
	}
	if overlapping.attempts() != 1 {
		t.Fatal("overlap rotation must use one fetch", overlapping.attempts())
	}
	if _, e = NewValidator("https://idp.example", "bookstore", ":invalid", &http.Client{Transport: fixture}).Authenticate(context.Background(), token); e == nil {
		t.Fatal("JWKS URL")
	}
}

// A warm cache survives a malformed or key-less refresh: only a present,
// array-valued keys member is an authoritative document, so `{}`, null and
// wrong-type keys fail the attempt and preserve the old unexpired token;
// the explicit empty array remains the sole valid withdrawal.
func TestJWKSMalformedRefreshRetainsWarmCache(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	forge, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture, _ := newJWKSFixture(t, rsaJWK("current", key))
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second, FetchTimeout: time.Second})
	warm := mint(t, key, "current", bearerClaims(*clock))
	if _, e = v.Authenticate(context.Background(), warm); e != nil {
		t.Fatal(e)
	}
	spam := mint(t, forge, "attacker", bearerClaims(*clock))
	for i, body := range []string{"{}", `{"keys":null}`, `{"keys":42}`, `{"keys":"x"}`, `{"keys":{}}`} {
		fixture.serve(http.StatusOK, body)
		*clock = clock.Add(2 * time.Second)
		if _, e = v.Authenticate(context.Background(), spam); !errors.Is(e, ErrUnauthenticated) {
			t.Fatal(i, "malformed refresh must fail closed", e)
		}
		if want := uint32(i + 2); fixture.attempts() != want {
			t.Fatal(i, "each malformed refresh is one bounded fetch", fixture.attempts())
		}
		if _, e = v.Authenticate(context.Background(), warm); e != nil {
			t.Fatal(i, "old unexpired token must be preserved", e)
		}
		if fixture.attempts() != uint32(i+2) {
			t.Fatal(i, "retained snapshot must serve the known kid without fetch", fixture.attempts())
		}
	}
	// The explicit empty array installs an authoritative withdrawal.
	fixture.serve(http.StatusOK, `{"keys":[]}`)
	*clock = clock.Add(2 * time.Second)
	if _, e = v.Authenticate(context.Background(), spam); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("withdrawal", e)
	}
	if fixture.attempts() != 7 {
		t.Fatal(fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), warm); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("withdrawal must remove the previously valid kid", e)
	}
	if fixture.attempts() != 7 {
		t.Fatal(fixture.attempts())
	}
	fixture.serve(http.StatusOK, jwksDocument(t, rsaJWK("current", key)))
	*clock = clock.Add(2 * time.Second)
	if _, e = v.Authenticate(context.Background(), warm); e != nil {
		t.Fatal("restore", e)
	}
	if fixture.attempts() != 8 {
		t.Fatal(fixture.attempts())
	}
}

// A cancelled leader consumes exactly one cooldown window (measured from its
// started attempt), so a flood of new-kid verifications cannot raise the
// fetch rate; an independent client may retry once the cooldown passes.
func TestJWKSCancelledLeaderBoundsAttempts(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture := &jwksFixture{status: http.StatusOK, entered: make(chan struct{}, 8), release: make(chan struct{})}
	fixture.serve(http.StatusOK, jwksDocument(t, rsaJWK("k2", key)))
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: 5 * time.Second, FetchTimeout: 30 * time.Second})
	leader := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, err := v.Authenticate(ctx, mint(t, key, "k1", bearerClaims(*clock)))
		leader <- err
	}()
	<-fixture.entered
	cancel()
	if e = <-leader; e == nil {
		t.Fatal("cancelled leader must fail closed")
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	close(fixture.release)
	flood := mint(t, key, "k3", bearerClaims(*clock))
	for i := 0; i < 3; i++ {
		if _, e = v.Authenticate(context.Background(), flood); !errors.Is(e, ErrUnauthenticated) {
			t.Fatal(i, e)
		}
	}
	if fixture.attempts() != 1 {
		t.Fatal("cooldown must bound the cancelled-leader flood", fixture.attempts())
	}
	results := make(chan error, 4)
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Add(1)
		go func(n int) {
			defer group.Done()
			_, err := v.Authenticate(context.Background(), mint(t, key, "k"+string(rune('0'+n+4)), bearerClaims(*clock)))
			results <- err
		}(i)
	}
	group.Wait()
	close(results)
	for err := range results {
		if !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("flood", err)
		}
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	*clock = clock.Add(6 * time.Second)
	// An independent client is allowed one retry after the cooldown; the
	// refreshed set then serves it without further fetches.
	retry := mint(t, key, "k2", bearerClaims(*clock))
	if _, e = v.Authenticate(context.Background(), retry); e != nil {
		t.Fatal("cooldown retry", e)
	}
	if fixture.attempts() != 2 {
		t.Fatal(fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), retry); e != nil {
		t.Fatal(e)
	}
	if _, e = v.Authenticate(context.Background(), flood); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("the retry start must consume its own cooldown", e)
	}
	if fixture.attempts() != 2 {
		t.Fatal(fixture.attempts())
	}
}

// FetchTimeout bounds one stalled fetch even when the HTTP client carries no
// Timeout of its own; each started attempt consumes the cooldown either way.
func TestJWKSFetchTimeoutBoundsStalledEndpoint(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture := &jwksFixture{status: http.StatusOK, entered: make(chan struct{}, 8), release: make(chan struct{})}
	fixture.serve(http.StatusOK, jwksDocument(t, rsaJWK("current", key)))
	// clockedValidator uses &http.Client{Transport: fixture}: Timeout zero.
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: 5 * time.Second, FetchTimeout: 100 * time.Millisecond})
	token := mint(t, key, "current", bearerClaims(*clock))
	for i := 0; i < 2; i++ {
		if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
			t.Fatal("stalled endpoint must fail closed", i, e)
		}
	}
	if fixture.attempts() != 1 {
		t.Fatal("the timed-out start must consume the cooldown", fixture.attempts())
	}
	*clock = clock.Add(6 * time.Second)
	if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal(e)
	}
	if fixture.attempts() != 2 {
		t.Fatal("each cooldown window allows at most one bounded attempt", fixture.attempts())
	}
	close(fixture.release)
	*clock = clock.Add(6 * time.Second)
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal("release", e)
	}
	if fixture.attempts() != 3 {
		t.Fatal(fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	if fixture.attempts() != 3 {
		t.Fatal(fixture.attempts())
	}
}

// Context cancellation is checked before accepting a cached identity and
// before starting any fetch; a pre-cancelled Verify never triggers network
// work, whether the cache is warm or cold.
func TestJWKSPreCancelledContextNeverFetches(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture, _ := newJWKSFixture(t, rsaJWK("current", key))
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second, FetchTimeout: time.Second})
	token := mint(t, key, "current", bearerClaims(*clock))
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	credential := identity.Credential{Kind: identity.BearerKind, Value: token}
	if _, e = v.Verify(ctx, credential); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("cancelled cached verification must fail closed", e)
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	cold := &jwksFixture{}
	cold.serve(http.StatusOK, jwksDocument(t, rsaJWK("current", key)))
	freezing, coldClock := clockedValidator(t, cold, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second, FetchTimeout: time.Second})
	if _, e = freezing.Verify(ctx, identity.Credential{Kind: identity.BearerKind, Value: mint(t, key, "current", bearerClaims(*coldClock))}); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("pre-cancelled cold verify must not fetch", e)
	}
	if cold.attempts() != 0 {
		t.Fatal(cold.attempts())
	}
}

// A parseable JWKS with zero usable keys is an authoritative withdrawal: it
// replaces the cached set at the next authorized refresh so removed kids stop
// verifying instead of living until the old TTL; malformed documents retain
// the existing snapshot per policy.
func TestJWKSKeyWithdrawalReplacesCachedSet(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture, _ := newJWKSFixture(t, rsaJWK("current", key))
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second, FetchTimeout: time.Second})
	token := mint(t, key, "current", bearerClaims(*clock))
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	fixture.serve(http.StatusOK, `{"keys":[]}`)
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal("the unexpired snapshot is retained per policy", e)
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	*clock = clock.Add(2 * time.Minute)
	if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("withdrawal must reject the removed kid", e)
	}
	if fixture.attempts() != 2 {
		t.Fatal("withdrawal fetch expected", fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal(e)
	}
	if fixture.attempts() != 2 {
		t.Fatal("withdrawal cooldown must bound further fetches", fixture.attempts())
	}
	fixture.serve(http.StatusOK, jwksDocument(t, rsaJWK("current", key)))
	*clock = clock.Add(2 * time.Second)
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal("re-listed key verifies again", e)
	}
	if fixture.attempts() != 3 {
		t.Fatal(fixture.attempts())
	}
	// A malformed document is not authoritative: it fails the attempt and
	// keeps the existing snapshot; recovery works once it is fixed.
	fixture.serve(http.StatusOK, "{")
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	if fixture.attempts() != 3 {
		t.Fatal(fixture.attempts())
	}
	*clock = clock.Add(2 * time.Minute)
	if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("malformed refresh must fail closed", e)
	}
	if fixture.attempts() != 4 {
		t.Fatal(fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal(e)
	}
	if fixture.attempts() != 4 {
		t.Fatal(fixture.attempts())
	}
	fixture.serve(http.StatusOK, jwksDocument(t, rsaJWK("current", key)))
	*clock = clock.Add(2 * time.Second)
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal("restore", e)
	}
	if fixture.attempts() != 5 {
		t.Fatal(fixture.attempts())
	}
}

// Known kids are served from the cache: repeated verifications fetch once,
// and a source outage inside the TTL is invisible to authentication. The
// zero policy falls back to the published defaults.
func TestJWKSKnownKidServedFromCache(t *testing.T) {
	v := NewValidatorWithPolicy("https://idp.example", "bookstore", "https://idp.example/jwks", nil, KeyPolicy{})
	if v.policy != DefaultKeyPolicy() {
		t.Fatal(v.policy)
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture, _ := newJWKSFixture(t, rsaJWK("current", key))
	cached, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second})
	token := mint(t, key, "current", bearerClaims(*clock))
	for i := 0; i < 5; i++ {
		if _, e = cached.Authenticate(context.Background(), token); e != nil {
			t.Fatal(i, e)
		}
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	fixture.err = errors.New("offline")
	if _, e = cached.Authenticate(context.Background(), token); e != nil {
		t.Fatal("outage must be shielded within the TTL", e)
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
}

// Unknown kids are rate limited by the fetch cooldown: a burst of attacker
// kids reuses one refresh per cooldown window and never grows a kid map.
func TestJWKSUnknownKidBurstBoundedByCooldown(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	forge, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture, _ := newJWKSFixture(t, rsaJWK("current", key))
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: 5 * time.Second})
	token := mint(t, key, "current", bearerClaims(*clock))
	spam := mint(t, forge, "attacker", bearerClaims(*clock))
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if _, e = v.Authenticate(context.Background(), spam); !errors.Is(e, ErrUnauthenticated) {
			t.Fatal(i, e)
		}
		if fixture.attempts() != 1 {
			t.Fatal("cooldown must bound unknown-kid refresh", fixture.attempts())
		}
	}
	*clock = clock.Add(6 * time.Second)
	if _, e = v.Authenticate(context.Background(), spam); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal(e)
	}
	if fixture.attempts() != 2 {
		t.Fatal(fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 3)
	var group sync.WaitGroup
	for i := 0; i < 3; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := v.Authenticate(context.Background(), spam)
			results <- err
		}()
	}
	group.Wait()
	close(results)
	for err := range results {
		if !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("cooldown burst", err)
		}
	}
	if fixture.attempts() != 2 {
		t.Fatal("concurrent cooldown burst must not fetch", fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	if fixture.attempts() != 2 {
		t.Fatal(fixture.attempts())
	}
}

// Concurrent verifications of a cold cache share one in-flight fetch; every
// joiner is proven to have reached the refresh select before release via the
// watched test context, so the single-fetch assertion is deterministic.
func TestJWKSConcurrentRefreshCoalesces(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture := &jwksFixture{status: http.StatusOK, entered: make(chan struct{}, 4), release: make(chan struct{})}
	fixture.serve(http.StatusOK, jwksDocument(t, rsaJWK("current", key)))
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: 5 * time.Second})
	token := mint(t, key, "current", bearerClaims(*clock))
	credential := identity.Credential{Kind: identity.BearerKind, Value: token}
	initiator := make(chan error, 1)
	go func() {
		_, err := v.Verify(context.Background(), credential)
		initiator <- err
	}()
	<-fixture.entered
	verifiers := 8
	results := make(chan error, verifiers)
	watchers := make([]*watchedCtx, verifiers)
	var group sync.WaitGroup
	for i := 0; i < verifiers; i++ {
		watchers[i] = watchContext(context.Background())
	}
	for i := 0; i < verifiers; i++ {
		group.Add(1)
		go func(w *watchedCtx) {
			defer group.Done()
			_, err := v.Verify(w, credential)
			results <- err
		}(watchers[i])
	}
	// Each joiner has now evaluated the refresh select; none could have
	// observed a finished fetch, because the fixture is still gated.
	for _, w := range watchers {
		<-w.waiting
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	close(fixture.release)
	group.Wait()
	if e = <-initiator; e != nil {
		t.Fatal("initiator", e)
	}
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("joiner", err)
		}
	}
	if fixture.attempts() != 1 {
		t.Fatal("one in-flight fetch must serve every waiter", fixture.attempts())
	}
	// The installed set then serves further verifications without network.
	for i := 0; i < verifiers; i++ {
		if _, e = v.Verify(context.Background(), credential); e != nil {
			t.Fatal(i, e)
		}
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
}

// Waiting respects the caller's context: each cancelled waiter is proven to
// have joined the in-flight wait first, then fails closed without fetching,
// while an unaffected waiter still completes the single shared fetch.
func TestJWKSWaiterCancellation(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture := &jwksFixture{status: http.StatusOK, entered: make(chan struct{}, 4), release: make(chan struct{})}
	fixture.serve(http.StatusOK, jwksDocument(t, rsaJWK("current", key)))
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: 5 * time.Second})
	token := mint(t, key, "current", bearerClaims(*clock))
	credential := identity.Credential{Kind: identity.BearerKind, Value: token}
	initiator := make(chan error, 1)
	go func() {
		_, err := v.Verify(context.Background(), credential)
		initiator <- err
	}()
	<-fixture.entered
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	// The pipeline fails a pre-cancelled credential back before Verify; the
	// important property is fail-closed without a fetch either way.
	if _, e = v.Authenticate(cancelled, token); e == nil {
		t.Fatal("pre-cancelled waiter must fail closed")
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	waiting, stop := context.WithCancel(context.Background())
	waiter := watchContext(waiting)
	midway := make(chan error, 1)
	go func() {
		_, err := v.Verify(waiter, credential)
		midway <- err
	}()
	// Established: this waiter is inside the refresh select before cancel.
	<-waiter.waiting
	stop()
	if e = <-midway; e == nil {
		t.Fatal("joined waiter must fail closed after cancellation")
	}
	if fixture.attempts() != 1 {
		t.Fatal("cancelled waiters must not fetch", fixture.attempts())
	}
	passing := watchContext(context.Background())
	unaffected := make(chan error, 1)
	go func() {
		_, err := v.Verify(passing, credential)
		unaffected <- err
	}()
	<-passing.waiting
	close(fixture.release)
	if e = <-initiator; e != nil {
		t.Fatal("initiator", e)
	}
	if e = <-unaffected; e != nil {
		t.Fatal("unaffected waiter", e)
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
}

// An expired set fails closed while the source is unavailable, stops
// hammering it during cooldown, and recovers once the source is back.
func TestJWTExpiredSetFailsClosedOnSourceFailure(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	fixture, document := newJWKSFixture(t, rsaJWK("current", key))
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: 5 * time.Second})
	token := mint(t, key, "current", bearerClaims(*clock))
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	*clock = clock.Add(time.Minute - time.Second)
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal("the set must hold until the TTL", e)
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	fixture.err = errors.New("offline")
	*clock = clock.Add(2 * time.Second)
	if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("expired set plus unavailable source must fail closed", e)
	}
	if fixture.attempts() != 2 {
		t.Fatal(fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal(e)
	}
	if fixture.attempts() != 2 {
		t.Fatal("unavailable source must not be retried within cooldown", fixture.attempts())
	}
	fixture.serve(http.StatusOK, document)
	*clock = clock.Add(6 * time.Second)
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal("recovery", e)
	}
	if fixture.attempts() != 3 {
		t.Fatal(fixture.attempts())
	}
	if _, e = v.Authenticate(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	if fixture.attempts() != 3 {
		t.Fatal(fixture.attempts())
	}
}

// KeyPolicy bounds retention and document size; a kid truncated away by
// MaxKeys and an oversized document both fail closed instead of verifying.
func TestJWKSPolicyBounds(t *testing.T) {
	keys := make([]*rsa.PrivateKey, 5)
	kids := make([]jwk, len(keys))
	for i := range keys {
		var e error
		if keys[i], e = rsa.GenerateKey(rand.Reader, 2048); e != nil {
			t.Fatal(e)
		}
		kids[i] = rsaJWK("k"+string(rune('0'+i)), keys[i])
	}
	fixture, _ := newJWKSFixture(t, kids...)
	v, clock := clockedValidator(t, fixture, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second, MaxKeys: 3})
	first := mint(t, keys[0], "k0", bearerClaims(*clock))
	if _, e := v.Authenticate(context.Background(), first); e != nil {
		t.Fatal(e)
	}
	truncated := mint(t, keys[4], "k4", bearerClaims(*clock))
	if _, err := v.Authenticate(context.Background(), truncated); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("truncated kid must fail closed", err)
	}
	if fixture.attempts() != 1 {
		t.Fatal(fixture.attempts())
	}
	*clock = clock.Add(2 * time.Second)
	if _, err := v.Authenticate(context.Background(), truncated); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
	if fixture.attempts() != 2 {
		t.Fatal("refresh must keep the document capped", fixture.attempts())
	}
	if _, e := v.Authenticate(context.Background(), first); e != nil {
		t.Fatal(e)
	}
	if fixture.attempts() != 2 {
		t.Fatal(fixture.attempts())
	}
	// The document cap rejects oversized responses as failed attempts.
	one, document := newJWKSFixture(t, rsaJWK("solo", keys[0]))
	bound, clock := clockedValidator(t, one, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second, MaxResponseBytes: len(document) - 1})
	solo := mint(t, keys[0], "solo", bearerClaims(*clock))
	for i := 0; i < 2; i++ {
		var err error
		if _, err = bound.Authenticate(context.Background(), solo); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("oversized document", err)
		}
	}
	if one.attempts() != 1 {
		t.Fatal(one.attempts())
	}
	bound2, clock := clockedValidator(t, one, KeyPolicy{CacheTTL: time.Minute, FetchCooldown: time.Second, MaxResponseBytes: len(document) + 10})
	solo = mint(t, keys[0], "solo", bearerClaims(*clock))
	if _, err := bound2.Authenticate(context.Background(), solo); err != nil {
		t.Fatal(err)
	}
	if one.attempts() != 2 {
		t.Fatal(one.attempts())
	}
}
