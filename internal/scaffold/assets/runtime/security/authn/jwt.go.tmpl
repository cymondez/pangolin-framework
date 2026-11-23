// Package authn verifies the M1 RS256 identity slice against a trusted JWKS URL.
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
	"time"

	"github.com/cymondez/pangolin-framework/security/scope"
)

var ErrUnauthenticated = errors.New("UNAUTHENTICATED")

type identity struct {
	principal scope.PrincipalRef
	tenant    string
}
type identityKey struct{}

// Identity reads only verified identity installed by Validator.Authenticate.
func Identity(ctx context.Context) (scope.PrincipalRef, string, bool) {
	if ctx == nil {
		return scope.PrincipalRef{}, "", false
	}
	v, ok := ctx.Value(identityKey{}).(identity)
	return v.principal, v.tenant, ok
}

type Validator struct {
	issuer, audience, jwks string
	client                 *http.Client
}

func NewValidator(issuer, audience, jwks string, client *http.Client) *Validator {
	return &Validator{strings.TrimRight(issuer, "/"), audience, jwks, client}
}
func (v *Validator) Authenticate(ctx context.Context, token string) (context.Context, error) {
	if ctx == nil || len(token) > 16384 || v.client == nil || v.issuer == "" || v.audience == "" {
		return nil, ErrUnauthenticated
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrUnauthenticated
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
		return nil, ErrUnauthenticated
	}
	var claims struct {
		Issuer    string          `json:"iss"`
		Audience  json.RawMessage `json:"aud"`
		Subject   string          `json:"sub"`
		Tenant    string          `json:"tenant_id"`
		Expiry    int64           `json:"exp"`
		NotBefore int64           `json:"nbf"`
	}
	if decode(parts[1], &claims) != nil || claims.Issuer != v.issuer || claims.Subject == "" || claims.Tenant == "" || claims.Expiry <= time.Now().Unix() || claims.NotBefore > time.Now().Unix() {
		return nil, ErrUnauthenticated
	}
	var audiences []string
	var single string
	if json.Unmarshal(claims.Audience, &single) == nil {
		audiences = []string{single}
	} else if json.Unmarshal(claims.Audience, &audiences) != nil {
		return nil, ErrUnauthenticated
	}
	accepted := false
	for _, a := range audiences {
		accepted = accepted || a == v.audience
	}
	if !accepted {
		return nil, ErrUnauthenticated
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, v.jwks, nil)
	if e != nil {
		return nil, ErrUnauthenticated
	}
	response, e := v.client.Do(request)
	if e != nil {
		return nil, ErrUnauthenticated
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrUnauthenticated
	}
	var keys struct {
		Keys []struct{ Kty, Alg, Use, Kid, N, E string }
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if e != nil || len(raw) > 65536 || json.Unmarshal(raw, &keys) != nil {
		return nil, ErrUnauthenticated
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[2])
	if e != nil {
		return nil, ErrUnauthenticated
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
			return context.WithValue(ctx, identityKey{}, identity{scope.PrincipalRef{ID: claims.Subject, Kind: "user"}, claims.Tenant}), nil
		}
	}
	return nil, ErrUnauthenticated
}
