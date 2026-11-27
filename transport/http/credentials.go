package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pangolin-framework/pangolin/security/identity"
)

var (
	// ErrAmbiguousCredential reports that more than one credential source
	// yielded a credential for one request. Ambiguity fails closed instead of
	// guessing which credential the caller meant.
	ErrAmbiguousCredential = errors.New("AMBIGUOUS_CREDENTIAL")
	// ErrCookieUnsafeMethod refuses cookie credentials for state-changing
	// methods. Browsers attach cookies cross-site, so cookie authentication
	// on unsafe methods requires an explicit anti-CSRF integration boundary
	// supplied by the session module that owns the cookie; generic core never
	// writes or accepts session cookies itself.
	ErrCookieUnsafeMethod = errors.New("COOKIE_UNSAFE_METHOD")
)

// CredentialExtractor reads one request credential. Extraction fails closed:
// an absent credential is reported as present=false, and an error rejects
// the request without trying another scheme. Implementations must not parse
// the credential value; verifiers own protocol details.
type CredentialExtractor func(*http.Request) (identity.Credential, bool, error)

// BearerCredentials extracts the Authorization: Bearer token. It is the
// default working profile and remains the only scheme unless a composition
// explicitly configures another extractor. A non-empty Authorization header
// that is not exactly one valid Bearer value is an error, not an absent
// credential, so an invalid Bearer scheme can never fall back to another
// extractor.
func BearerCredentials() CredentialExtractor {
	return func(r *http.Request) (identity.Credential, bool, error) {
		if r == nil {
			return identity.Credential{}, false, identity.ErrUnauthenticated
		}
		values := r.Header.Values("Authorization")
		if len(values) == 0 {
			return identity.Credential{}, false, nil
		}
		if len(values) > 1 {
			return identity.Credential{}, false, identity.ErrUnauthenticated
		}
		scheme, token, _ := strings.Cut(values[0], " ")
		if !strings.EqualFold(scheme, "Bearer") || token == "" || strings.TrimSpace(token) != token || strings.ContainsAny(token, " \t\r\n") {
			return identity.Credential{}, false, identity.ErrUnauthenticated
		}
		return identity.Credential{Kind: identity.BearerKind, Value: token}, true, nil
	}
}

// CookieCredentials extracts the named session cookie for safe requests.
// Cookie presence is checked before the unsafe-method refusal so that
// requests without the cookie keep working with other credentials; the
// refusal is the explicit CSRF integration boundary for requests that do
// carry the cookie. Duplicate named cookies are ambiguous and fail closed.
// A session module that supplies CSRF protection composes its own extractor.
func CookieCredentials(name string) CredentialExtractor {
	return func(r *http.Request) (identity.Credential, bool, error) {
		if r == nil {
			return identity.Credential{}, false, identity.ErrUnauthenticated
		}
		if name == "" {
			return identity.Credential{}, false, identity.ErrUnauthenticated
		}
		count, value := 0, ""
		for _, cookie := range r.Cookies() {
			if cookie.Name != name {
				continue
			}
			count++
			value = cookie.Value
		}
		if count == 0 {
			return identity.Credential{}, false, nil
		}
		if count > 1 {
			return identity.Credential{}, false, ErrAmbiguousCredential
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			return identity.Credential{}, false, ErrCookieUnsafeMethod
		}
		if value == "" || strings.ContainsAny(value, " \t\r\n,;") {
			return identity.Credential{}, false, identity.ErrUnauthenticated
		}
		return identity.Credential{Kind: identity.SessionKind, Value: value}, true, nil
	}
}

// OneCredential combines extractors with exactly-one semantics: at most one
// configured source may yield a credential for a request. Zero sources yield
// an absent credential; more than one is ambiguous and fails closed. An
// error from any extractor rejects the request immediately: a bad credential
// is never retried against another source.
func OneCredential(extractors ...CredentialExtractor) CredentialExtractor {
	return func(r *http.Request) (identity.Credential, bool, error) {
		found, present := identity.Credential{}, false
		for _, extract := range extractors {
			if extract == nil {
				return identity.Credential{}, false, identity.ErrUnauthenticated
			}
			credential, ok, err := extract(r)
			if err != nil {
				return identity.Credential{}, false, err
			}
			if !ok {
				continue
			}
			if present {
				return identity.Credential{}, false, ErrAmbiguousCredential
			}
			found, present = credential, true
		}
		return found, present, nil
	}
}
