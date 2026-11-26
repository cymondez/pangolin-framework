// jwt.go holds the concrete RS256 JWT adapter: it verifies the M1 identity
// slice against a trusted JWKS URL through a bounded key cache. Parsing
// details stay here; the protocol-independent contract and trusted
// installation live in security/identity.
package authn

import (
	"context"
	"crypto"
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
	"time"

	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
)

// KeyPolicy bounds one Validator's JWKS fetching, caching and key retention.
// Non-positive fields fall back to DefaultKeyPolicy; every bound exists so a
// hostile kid stream or broken key endpoint cannot consume unbounded work.
type KeyPolicy struct {
	// CacheTTL trusts one fetched key set for this long. Within the TTL no
	// network fetch happens, so endpoint outages do not affect authentication.
	CacheTTL time.Duration
	// FetchCooldown is the minimum spacing between fetch attempts, measured
	// from each started attempt and consumed regardless of its outcome or
	// caller cancellation; during cooldown unknown-kid and expired-set
	// refreshes fail closed without network.
	FetchCooldown time.Duration
	// FetchTimeout bounds every single fetch through its own context
	// deadline, so source stalls stay bounded even when the HTTP client is
	// assembled with Timeout zero.
	FetchTimeout time.Duration
	// MaxKeys caps the number of usable keys kept from one document
	// (earliest document order wins; later entries, and any kid truncated
	// away, fail closed).
	MaxKeys int
	// MaxResponseBytes caps the accepted JWKS document size.
	MaxResponseBytes int
}

// DefaultKeyPolicy returns the published bounded caching policy.
func DefaultKeyPolicy() KeyPolicy {
	return KeyPolicy{
		CacheTTL:         5 * time.Minute,
		FetchCooldown:    5 * time.Second,
		FetchTimeout:     5 * time.Second,
		MaxKeys:          32,
		MaxResponseBytes: 65536,
	}
}

func (p KeyPolicy) normalized() KeyPolicy {
	fallback := DefaultKeyPolicy()
	if p.CacheTTL <= 0 {
		p.CacheTTL = fallback.CacheTTL
	}
	if p.FetchCooldown <= 0 {
		p.FetchCooldown = fallback.FetchCooldown
	}
	if p.FetchTimeout <= 0 {
		p.FetchTimeout = fallback.FetchTimeout
	}
	if p.MaxKeys <= 0 {
		p.MaxKeys = fallback.MaxKeys
	}
	if p.MaxResponseBytes <= 0 {
		p.MaxResponseBytes = fallback.MaxResponseBytes
	}
	return p
}

type rsaKey struct {
	kid string
	pub *rsa.PublicKey
}

var (
	errJWKSFetch    = errors.New("jwks fetch failed")
	errJWKSDocument = errors.New("jwks document unusable")
)

type Validator struct {
	issuer, audience, jwks string
	client                 *http.Client
	pipeline               *identity.Pipeline
	policy                 KeyPolicy

	keysMu        sync.Mutex
	keys          []rsaKey
	keysFetchedAt time.Time
	keysNextFetch time.Time
	fetching      chan struct{}

	// now is the clock used for claims, TTL and cooldown decisions; it is
	// injectable for deterministic tests and shared by every path.
	now func() time.Time
}

// NewValidator keeps the published constructor with DefaultKeyPolicy.
func NewValidator(issuer, audience, jwks string, client *http.Client) *Validator {
	return NewValidatorWithPolicy(issuer, audience, jwks, client, DefaultKeyPolicy())
}

// NewValidatorWithPolicy builds the validator with an explicit bounded JWKS
// cache policy; zero or negative fields fall back to the defaults.
func NewValidatorWithPolicy(issuer, audience, jwks string, client *http.Client, policy KeyPolicy) *Validator {
	v := &Validator{
		issuer:   strings.TrimRight(issuer, "/"),
		audience: audience,
		jwks:     jwks,
		client:   client,
		policy:   policy.normalized(),
		now:      func() time.Time { return time.Now() },
	}
	v.pipeline, _ = identity.NewPipeline(v)
	return v
}

// Kind implements identity.Verifier: bearer credentials route here.
func (v *Validator) Kind() string { return identity.BearerKind }

// Verify validates one RS256 bearer JWT and returns its result. It never
// installs identity; the trusted identity pipeline does that. The JWKS is
// cached under KeyPolicy: known kids verify without network inside the TTL,
// unknown kids and expired sets refresh single-flight with a cooldown, and
// every unavailable-source phase fails closed.
func (v *Validator) Verify(ctx context.Context, credential identity.Credential) (identity.Result, error) {
	if ctx == nil || v == nil || credential.Kind != identity.BearerKind || len(credential.Value) > identity.MaxCredentialBytes || v.client == nil || v.issuer == "" || v.audience == "" {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	token := credential.Value
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	decode := func(s string, out any) error {
		raw, e := base64.RawURLEncoding.DecodeString(s)
		if e != nil {
			return e
		}
		return json.Unmarshal(raw, out)
	}
	var header struct{ Alg, Kid string }
	if decode(parts[0], &header) != nil || header.Alg != "RS256" || header.Kid == "" {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	at := v.now().Unix()
	var claims struct {
		Issuer    string          `json:"iss"`
		Audience  json.RawMessage `json:"aud"`
		Subject   string          `json:"sub"`
		Tenant    string          `json:"tenant_id"`
		Expiry    int64           `json:"exp"`
		NotBefore int64           `json:"nbf"`
		AMR       json.RawMessage `json:"amr"`
	}
	if decode(parts[1], &claims) != nil || claims.Issuer != v.issuer || claims.Subject == "" || claims.Tenant == "" || claims.Expiry <= at || claims.NotBefore > at {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	var audiences []string
	var single string
	if json.Unmarshal(claims.Audience, &single) == nil {
		audiences = []string{single}
	} else if json.Unmarshal(claims.Audience, &audiences) != nil {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	accepted := false
	for _, a := range audiences {
		accepted = accepted || a == v.audience
	}
	if !accepted {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[2])
	if e != nil {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	// One refresh per verification at most: after it, a missing kid fails
	// closed instead of looping into further fetch attempts. Cancellation is
	// re-checked before accepting a cached identity, after refresh waiting,
	// and after the signature path so a cancelled operation never succeeds.
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return identity.Result{}, identity.ErrUnauthenticated
		}
		keys, ok := v.matchingKeys(header.Kid)
		if !ok {
			if attempt == 1 || !v.refreshKeys(ctx) {
				return identity.Result{}, identity.ErrUnauthenticated
			}
			continue
		}
		for _, key := range keys {
			if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil {
				if ctx.Err() != nil {
					return identity.Result{}, identity.ErrUnauthenticated
				}
				// The amr claim, when present as a string array, is preserved as
				// immutable, non-secret assurance metadata for future policies.
				assurances := []string(nil)
				if len(claims.AMR) > 0 {
					var reported []string
					if json.Unmarshal(claims.AMR, &reported) == nil {
						assurances = reported
					}
				}
				return identity.Result{
					Status:     identity.StatusAuthenticated,
					Principal:  scope.PrincipalRef{ID: claims.Subject, Kind: identity.PrincipalUser},
					Tenant:     claims.Tenant,
					Method:     identity.MethodJWT,
					Assurances: assurances,
				}, nil
			}
		}
		return identity.Result{}, identity.ErrUnauthenticated
	}
	return identity.Result{}, identity.ErrUnauthenticated
}

// matchingKeys returns the usable cached keys for kid while the cached set is
// still trusted. An expired set never satisfies a lookup, so an expired set
// plus an unavailable endpoint fails closed through the refresh path.
func (v *Validator) matchingKeys(kid string) ([]*rsa.PublicKey, bool) {
	v.keysMu.Lock()
	defer v.keysMu.Unlock()
	if len(v.keys) == 0 || v.now().Sub(v.keysFetchedAt) >= v.policy.CacheTTL {
		return nil, false
	}
	var keys []*rsa.PublicKey
	for i := range v.keys {
		if v.keys[i].kid == kid {
			keys = append(keys, v.keys[i].pub)
		}
	}
	return keys, len(keys) > 0
}

// refreshKeys obtains a fresher key set when one is not already in flight and
// the cooldown allows it. Concurrent verifiers join the single in-flight
// fetch and select on their own context; the fetch itself runs outside the
// state mutex under its own FetchTimeout. Every started attempt consumes the
// cooldown measured from its start. It returns whether the caller should
// retry the lookup: waiters whose context ended, and callers refused by the
// cooldown, must fail closed.
func (v *Validator) refreshKeys(ctx context.Context) bool {
	if ctx.Err() != nil {
		// A pre-cancelled operation must not start a fetch, and thus must
		// not consume the cooldown scheduled for a real attempt.
		return false
	}
	v.keysMu.Lock()
	if v.fetching != nil {
		done := v.fetching
		v.keysMu.Unlock()
		select {
		case <-done:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if v.now().Before(v.keysNextFetch) {
		v.keysMu.Unlock()
		return false
	}
	started := v.now()
	// Every started attempt consumes the cooldown before any I/O, measured
	// from its start: cancelled, timed out, failed and successful attempts
	// count alike, so a hostile stream of cancellations cannot raise the
	// fetch rate; an independent caller may retry once the cooldown passes.
	v.keysNextFetch = started.Add(v.policy.FetchCooldown)
	done := make(chan struct{})
	v.fetching = done
	v.keysMu.Unlock()

	fetchCtx, cancel := context.WithTimeout(ctx, v.policy.FetchTimeout)
	keys, err := v.fetchKeys(fetchCtx)
	cancel()
	fetchedAt := v.now()
	v.keysMu.Lock()
	v.fetching = nil
	// An authoritative document (any error-free response whose keys member
	// is a present array, including an empty one for withdrawal) installs
	// here; a cancelled, timed out, failed or malformed fetch never alters
	// the existing key set.
	if err == nil && keys != nil {
		v.keys = keys
		v.keysFetchedAt = fetchedAt
	}
	close(done)
	v.keysMu.Unlock()
	return true
}

// fetchKeys performs one bounded JWKS fetch without holding the state mutex.
// Transport errors, non-200, oversized and unparsable documents fail the
// attempt so the caller retains its existing snapshot. Only a document with
// a present, array-valued keys member is authoritative: it returns every
// usable key, including an empty list for an explicit withdrawal.
func (v *Validator) fetchKeys(ctx context.Context) ([]rsaKey, error) {
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, v.jwks, nil)
	if e != nil {
		return nil, errJWKSFetch
	}
	response, e := v.client.Do(request)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errJWKSFetch
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, int64(v.policy.MaxResponseBytes)+1))
	if e != nil {
		return nil, e
	}
	if len(raw) > v.policy.MaxResponseBytes {
		return nil, errJWKSDocument
	}
	var document struct {
		Keys json.RawMessage `json:"keys"`
	}
	if json.Unmarshal(raw, &document) != nil {
		return nil, errJWKSDocument
	}
	// Only a present, array-valued keys member is an authoritative document:
	// a missing or null keys, or any other type, is malformed and fails the
	// attempt so the existing unexpired snapshot is retained; an explicit
	// empty array remains a valid withdrawal.
	trimmed := strings.TrimSpace(string(document.Keys))
	if document.Keys == nil || trimmed == "null" || !strings.HasPrefix(trimmed, "[") {
		return nil, errJWKSDocument
	}
	var entries []struct{ Kty, Alg, Use, Kid, N, E string }
	if json.Unmarshal(document.Keys, &entries) != nil {
		return nil, errJWKSDocument
	}
	keys := make([]rsaKey, 0, min(len(entries), v.policy.MaxKeys))
	for _, key := range entries {
		if key.Kty != "RSA" || key.Alg != "RS256" || key.Use != "sig" {
			continue
		}
		n, ne := base64.RawURLEncoding.DecodeString(key.N)
		ex, ee := base64.RawURLEncoding.DecodeString(key.E)
		if ne != nil || ee != nil || len(ex) > 4 {
			continue
		}
		modulus := new(big.Int).SetBytes(n)
		exponent := new(big.Int).SetBytes(ex).Int64()
		if modulus.BitLen() < 2048 || exponent < 3 || exponent%2 == 0 {
			continue
		}
		keys = append(keys, rsaKey{kid: key.Kid, pub: &rsa.PublicKey{N: modulus, E: int(exponent)}})
		if len(keys) == v.policy.MaxKeys {
			break
		}
	}
	return keys, nil
}

// Authenticate keeps the published Bearer token entry point. The trusted
// identity pipeline validates the result and installs identity.
func (v *Validator) Authenticate(ctx context.Context, token string) (context.Context, error) {
	return v.AuthenticateCredential(ctx, identity.Credential{Kind: identity.BearerKind, Value: token})
}

// AuthenticateCredential verifies one extracted credential.
func (v *Validator) AuthenticateCredential(ctx context.Context, credential identity.Credential) (context.Context, error) {
	if v == nil || v.pipeline == nil {
		return nil, identity.ErrUnauthenticated
	}
	return v.pipeline.AuthenticateCredential(ctx, credential)
}
