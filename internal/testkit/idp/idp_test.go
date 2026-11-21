package idp

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRealSignatureRotationAndFixtureValidation(t *testing.T) {
	if _, err := New("https://production.example"); err == nil {
		t.Fatal("fixture accepted a production-style issuer")
	}
	s, err := New("http://127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response
	}
	for _, path := range []string{"/.well-known/openid-configuration", "/jwks.json", "/healthz"} {
		if got := request(http.MethodGet, path, ""); got.Code >= 300 {
			t.Fatalf("%s: %d", path, got.Code)
		}
	}
	for _, body := range []string{"{}", "{", `{"subject":"user","tenant_id":"tenant","admin":true}`, strings.Repeat("x", 5000)} {
		if got := request(http.MethodPost, "/token", body); got.Code != 400 {
			t.Fatalf("invalid request accepted: %d", got.Code)
		}
	}
	if got := request(http.MethodGet, "/token", ""); got.Code != 405 {
		t.Fatal(got.Code)
	}
	response := request(http.MethodPost, "/token", `{"subject":"alice","tenant_id":"tenant-a"}`)
	var result struct {
		Token string `json:"access_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(result.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT: %s", response.Body)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatal(err)
	}
	tampered := sha256.Sum256([]byte(parts[0] + "." + parts[1] + "x"))
	if err := rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, tampered[:], signature); err == nil {
		t.Fatal("tampering retained a valid signature")
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(claims, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["sub"] != "alice" || payload["tenant_id"] != "tenant-a" || payload["iss"] != s.issuer || payload["aud"] != "pangolin-bookstore" {
		t.Fatalf("wrong claims: %v", payload)
	}
	if payload["exp"].(float64)-payload["iat"].(float64) != 300 {
		t.Fatal("unbounded fixture token")
	}
	if got := request(http.MethodPost, "/rotate", ""); got.Code != 204 {
		t.Fatal(got.Code)
	}
	if s.kid != 2 || s.old == nil {
		t.Fatal("rotation failed")
	}
	if err := rsa.VerifyPKCS1v15(s.old, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatal("previous key lost during rotation")
	}
	var keys struct {
		Keys []map[string]string `json:"keys"`
	}
	if err := json.Unmarshal(request(http.MethodGet, "/jwks.json", "").Body.Bytes(), &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys.Keys) != 2 || keys.Keys[0]["kid"] != "2" || keys.Keys[1]["kid"] != "1" {
		t.Fatalf("wrong JWKS: %v", keys)
	}
}
