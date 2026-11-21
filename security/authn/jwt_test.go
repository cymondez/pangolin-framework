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
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestJWTSignatureClaimsAndRotation(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	responseStatus, responseBody, transportErr := 200, "", error(nil)
	client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		if transportErr != nil {
			return nil, transportErr
		}
		return &http.Response{StatusCode: responseStatus, Body: io.NopCloser(strings.NewReader(responseBody))}, nil
	})}
	refresh := func() {
		raw, _ := json.Marshal(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "current", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		responseBody = string(raw)
	}
	refresh()
	claims := func() map[string]any {
		return map[string]any{"iss": "https://idp.example", "aud": "bookstore", "sub": "alice", "tenant_id": "tenant-a", "exp": time.Now().Add(time.Minute).Unix()}
	}
	sign := func(header any, payload any) string {
		h, _ := json.Marshal(header)
		p, _ := json.Marshal(payload)
		text := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
		hash := sha256.Sum256([]byte(text))
		sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if e != nil {
			t.Fatal(e)
		}
		return text + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	header := map[string]any{"alg": "RS256", "kid": "current"}
	v := NewValidator("https://idp.example/", "bookstore", "https://idp.example/jwks", client)
	token := sign(header, claims())
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
	if _, e = v.Authenticate(context.Background(), sign(header, array)); e != nil {
		t.Fatal(e)
	}
	for _, change := range []struct {
		key   string
		value any
	}{{"iss", "wrong"}, {"aud", "wrong"}, {"aud", 42}, {"sub", ""}, {"tenant_id", ""}, {"exp", 0}, {"nbf", time.Now().Add(time.Hour).Unix()}} {
		c := claims()
		c[change.key] = change.value
		if _, e = v.Authenticate(context.Background(), sign(header, c)); !errors.Is(e, ErrUnauthenticated) {
			t.Fatal(change, e)
		}
	}
	for _, token := range []string{"", strings.Repeat("x", 16385), "!.!.!", sign(map[string]string{"alg": "none", "kid": "current"}, claims()), sign(map[string]string{"alg": "RS256"}, claims()), sign(header, "bad-claims"), strings.TrimSuffix(token, "=") + "x", strings.Join(strings.Split(token, ".")[:2], ".") + ".!"} {
		if _, e = v.Authenticate(context.Background(), token); !errors.Is(e, ErrUnauthenticated) {
			t.Fatal("invalid token", e)
		}
	}
	if _, e = v.Authenticate(nil, token); e == nil {
		t.Fatal("nil context")
	}
	if _, e = NewValidator("", "", "", nil).Authenticate(context.Background(), token); e == nil {
		t.Fatal("unconfigured")
	}
	for _, body := range []string{"{", strings.Repeat(" ", 65537), `{"keys":[]}`, `{"keys":[{"kid":"current","alg":"RS256","use":"sig","kty":"RSA","n":"!","e":"AQAB"}]}`, `{"keys":[{"kid":"current","alg":"RS256","use":"sig","kty":"RSA","n":"AQ","e":"Ag"}]}`} {
		responseBody = body
		if _, e = v.Authenticate(context.Background(), token); e == nil {
			t.Fatal("invalid JWKS")
		}
	}
	refresh()
	responseStatus = 500
	if _, e = v.Authenticate(context.Background(), token); e == nil {
		t.Fatal("status")
	}
	responseStatus = 200
	transportErr = errors.New("offline")
	if _, e = v.Authenticate(context.Background(), token); e == nil {
		t.Fatal("offline")
	}
	transportErr = nil
	key, e = rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	refresh()
	if _, e = v.Authenticate(context.Background(), token); e == nil {
		t.Fatal("old signature")
	}
	if _, e = v.Authenticate(context.Background(), sign(header, claims())); e != nil {
		t.Fatal("rotated key", e)
	}
	if _, e = NewValidator("https://idp.example", "bookstore", ":invalid", client).Authenticate(context.Background(), token); e == nil {
		t.Fatal("JWKS URL")
	}
}
