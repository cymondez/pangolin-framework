// jwt.go holds the concrete RS256 JWT adapter: it verifies the M1 identity
// slice against a trusted JWKS URL. Parsing details stay here; the
// protocol-independent contract and trusted installation live in
// security/identity.
package authn

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/cymondez/pangolin-framework/security/identity"
	"github.com/cymondez/pangolin-framework/security/scope"
)

type Validator struct {
	issuer, audience, jwks string
	client                 *http.Client
	pipeline               *identity.Pipeline
}

func NewValidator(issuer, audience, jwks string, client *http.Client) *Validator {
	v := &Validator{issuer: strings.TrimRight(issuer, "/"), audience: audience, jwks: jwks, client: client}
	v.pipeline, _ = identity.NewPipeline(v)
	return v
}

// Kind implements identity.Verifier: bearer credentials route here.
func (v *Validator) Kind() string { return identity.BearerKind }

// Verify validates one RS256 bearer JWT and returns its result. It never
// installs identity; the trusted identity pipeline does that.
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
	var claims struct {
		Issuer    string          `json:"iss"`
		Audience  json.RawMessage `json:"aud"`
		Subject   string          `json:"sub"`
		Tenant    string          `json:"tenant_id"`
		Expiry    int64           `json:"exp"`
		NotBefore int64           `json:"nbf"`
		AMR       json.RawMessage `json:"amr"`
	}
	if decode(parts[1], &claims) != nil || claims.Issuer != v.issuer || claims.Subject == "" || claims.Tenant == "" || claims.Expiry <= time.Now().Unix() || claims.NotBefore > time.Now().Unix() {
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
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, v.jwks, nil)
	if e != nil {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	response, e := v.client.Do(request)
	if e != nil {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	var keys struct {
		Keys []struct{ Kty, Alg, Use, Kid, N, E string }
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if e != nil || len(raw) > 65536 || json.Unmarshal(raw, &keys) != nil {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[2])
	if e != nil {
		return identity.Result{}, identity.ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	for _, key := range keys.Keys {
		if key.Kid != header.Kid || key.Kty != "RSA" || key.Alg != "RS256" || key.Use != "sig" {
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
		if rsa.VerifyPKCS1v15(&rsa.PublicKey{N: modulus, E: int(exponent)}, crypto.SHA256, digest[:], signature) == nil {
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
