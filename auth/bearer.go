package auth

import (
	"crypto/sha256"
	"net/http"
	"strings"
)

// BearerOptions configures RequireBearerToken.
type BearerOptions struct {
	// Realm is sent in the WWW-Authenticate challenge. An empty realm uses
	// "restricted".
	Realm string

	// HideWhenUnconfigured makes an empty configured token look like a missing
	// route. It is useful for optional internal endpoints: a deployment that
	// forgot to provide the token does not advertise that the endpoint exists.
	// A non-empty token always returns 401 for a missing, malformed, or wrong
	// credential.
	HideWhenUnconfigured bool
}

// RequireBearerToken returns middleware that protects a handler with one
// static bearer token.
//
// The configured token is hashed once when the middleware is created. Each
// request must contain exactly one Authorization header with a Bearer scheme
// and one RFC 6750 token68 credential; the scheme name is case-insensitive.
// The request credential is hashed and compared with the configured digest in
// constant time. Empty configuration never authenticates. When the token is
// empty, HideWhenUnconfigured can turn the response into a quiet 404;
// otherwise all rejected requests receive a 401 challenge.
//
// The middleware does not log or include credentials in responses. Applications
// should keep the configured token in a secret manager or environment variable
// and should construct this middleware once at startup rather than per request.
func RequireBearerToken(token string, opts BearerOptions) func(http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(token))
	configured := token != ""
	challenge := bearerChallenge(opts.Realm)

	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !configured && opts.HideWhenUnconfigured {
				http.NotFound(w, r)
				return
			}

			presented, ok := requestBearerToken(r)
			if ok {
				actual := sha256.Sum256([]byte(presented))
				if constantTimeBytesEqual(expected[:], actual[:]) {
					next.ServeHTTP(w, r)
					return
				}
			}

			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		})
	}
}

func requestBearerToken(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	return parseBearerAuthorization(values[0])
}

func parseBearerAuthorization(value string) (string, bool) {
	value = trimOWS(value)
	separator := strings.IndexAny(value, " \t")
	if separator <= 0 {
		return "", false
	}
	scheme := value[:separator]
	credential := trimOWS(value[separator:])
	if credential == "" || strings.IndexAny(credential, " \t") >= 0 || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	if !validBearerToken(credential) {
		return "", false
	}
	return credential, true
}

func validBearerToken(token string) bool {
	if token == "" {
		return false
	}
	hasData := false
	padding := false
	for i := 0; i < len(token); i++ {
		c := token[i]
		switch {
		case isBearerTokenChar(c):
			if padding {
				return false
			}
			hasData = true
		case c == '=':
			if !hasData {
				return false
			}
			padding = true
		default:
			return false
		}
	}
	return hasData
}

func isBearerTokenChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		strings.ContainsRune("-._~+/", rune(c))
}

func bearerChallenge(realm string) string {
	if realm == "" {
		realm = "restricted"
	}
	return `Bearer realm="` + escapeBearerRealm(realm) + `"`
}

func escapeBearerRealm(realm string) string {
	const hex = "0123456789ABCDEF"
	var escaped strings.Builder
	escaped.Grow(len(realm))
	for i := 0; i < len(realm); i++ {
		c := realm[i]
		switch {
		case c == '\\' || c == '"':
			escaped.WriteByte('\\')
			escaped.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			escaped.WriteByte('\\')
			escaped.WriteByte('x')
			escaped.WriteByte(hex[c>>4])
			escaped.WriteByte(hex[c&0x0f])
		default:
			escaped.WriteByte(c)
		}
	}
	return escaped.String()
}

func trimOWS(value string) string {
	return strings.Trim(value, " \t")
}
