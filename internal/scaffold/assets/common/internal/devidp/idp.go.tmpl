// Package idp is an intentionally unauthenticated, local-development OIDC fixture.
// It issues real RSA signatures but must never be deployed as a production IdP.
package idp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	issuer string
	mu     sync.RWMutex
	key    *rsa.PrivateKey
	old    *rsa.PublicKey
	kid    int
}

func New(issuer string) (*Server, error) {
	if !strings.HasPrefix(issuer, "http://") {
		return nil, fmt.Errorf("test issuer must be an explicit local HTTP URL")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Server{issuer: strings.TrimRight(issuer, "/"), key: key, kid: 1}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"issuer": s.issuer, "jwks_uri": s.issuer + "/jwks.json", "token_endpoint": s.issuer + "/token", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("GET /jwks.json", s.jwks)
	mux.HandleFunc("POST /token", s.token)
	mux.HandleFunc("POST /rotate", s.rotate)
	return mux
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func jwk(key *rsa.PublicKey, kid int) map[string]string {
	return map[string]string{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": fmt.Sprint(kid), "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}
}

func (s *Server) jwks(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := []map[string]string{jwk(&s.key.PublicKey, s.kid)}
	if s.old != nil {
		keys = append(keys, jwk(s.old, s.kid-1))
	}
	writeJSON(w, map[string]any{"keys": keys})
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Subject  string `json:"subject"`
		TenantID string `json:"tenant_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Subject == "" || request.TenantID == "" {
		http.Error(w, "subject and tenant_id are required", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": fmt.Sprint(s.kid)})
	now := time.Now().Unix()
	claims, _ := json.Marshal(map[string]any{"iss": s.issuer, "aud": "pangolin-bookstore", "sub": request.Subject, "tenant_id": request.TenantID, "iat": now, "exp": now + 300})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		http.Error(w, "signing failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"access_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), "token_type": "Bearer", "expires_in": 300})
}

func (s *Server) rotate(w http.ResponseWriter, r *http.Request) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		http.Error(w, "key generation failed", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.old, s.key, s.kid = &s.key.PublicKey, key, s.kid+1
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
