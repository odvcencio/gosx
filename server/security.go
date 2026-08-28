package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

// NoncePlaceholder marks the spot in a Content-Security-Policy value where GoSX
// writes the per-request nonce. Write it inside the quoted nonce source, as in
// "script-src 'self' 'nonce-{nonce}'".
const NoncePlaceholder = "{nonce}"

// SecurityPolicy configures the optional security response headers.
//
// The policy is opt-in. A default Content-Security-Policy would block the
// inline scripts that existing apps already ship, so New leaves it off and
// EnableSecurityPolicy turns it on.
//
// A shared-cacheable response carries no nonce, because a shared cache would
// replay one client's nonce to the next client. GoSX drops the nonce from the
// body and sends SharedContentSecurityPolicy on those responses. Set that field
// to a policy that needs no nonce, such as one built from hashes or from
// 'strict-dynamic'. Leave it empty to let GoSX remove the nonce source from
// ContentSecurityPolicy, which then blocks the inline scripts on that page.
type SecurityPolicy struct {
	// ContentSecurityPolicy is the policy value. GoSX replaces every
	// NoncePlaceholder with a fresh nonce and attaches the same nonce to the
	// script elements it emits.
	ContentSecurityPolicy string

	// SharedContentSecurityPolicy replaces ContentSecurityPolicy on a
	// shared-cacheable response.
	SharedContentSecurityPolicy string

	// ReportOnly sends Content-Security-Policy-Report-Only in place of
	// Content-Security-Policy.
	ReportOnly bool

	// FrameOptions sets X-Frame-Options, for example "DENY" or "SAMEORIGIN".
	FrameOptions string

	// StrictTransportSecurity sets Strict-Transport-Security, for example
	// "max-age=63072000; includeSubDomains". Send it only over HTTPS.
	StrictTransportSecurity string

	// ReferrerPolicy overrides the default "strict-origin-when-cross-origin".
	ReferrerPolicy string

	// PermissionsPolicy sets Permissions-Policy.
	PermissionsPolicy string

	// NonceBytes sets how many random bytes back each nonce. Values below 16
	// become 16.
	NonceBytes int
}

const defaultNonceBytes = 16

type securityPolicyState struct {
	nonce        string
	policy       string
	sharedPolicy string
	headerName   string
}

const securityPolicyContextKey contextKey = "gosx.security_policy"

func withSecurityPolicyState(ctx context.Context, state securityPolicyState) context.Context {
	return context.WithValue(ctx, securityPolicyContextKey, state)
}

// EnableSecurityPolicy turns on the optional security response headers,
// including a Content-Security-Policy with a generated per-request nonce.
//
// Call it before Build. Passing a zero SecurityPolicy turns the feature off
// again and restores the default headers alone.
func (a *App) EnableSecurityPolicy(policy SecurityPolicy) {
	if a == nil {
		return
	}
	a.securityPolicy = normalizeSecurityPolicy(policy)
}

func normalizeSecurityPolicy(policy SecurityPolicy) SecurityPolicy {
	policy.ContentSecurityPolicy = strings.TrimSpace(policy.ContentSecurityPolicy)
	policy.SharedContentSecurityPolicy = strings.TrimSpace(policy.SharedContentSecurityPolicy)
	policy.FrameOptions = strings.TrimSpace(policy.FrameOptions)
	policy.StrictTransportSecurity = strings.TrimSpace(policy.StrictTransportSecurity)
	policy.ReferrerPolicy = strings.TrimSpace(policy.ReferrerPolicy)
	policy.PermissionsPolicy = strings.TrimSpace(policy.PermissionsPolicy)
	if policy.NonceBytes < defaultNonceBytes {
		policy.NonceBytes = defaultNonceBytes
	}
	if policy.SharedContentSecurityPolicy == "" && policy.ContentSecurityPolicy != "" {
		policy.SharedContentSecurityPolicy = removeNonceSources(policy.ContentSecurityPolicy)
		if policy.SharedContentSecurityPolicy == "" {
			// A shared response cannot carry a request nonce. If removing the
			// nonce leaves no usable policy, fail closed instead of emitting a
			// header that browsers will ignore or pretending the cached body has
			// a nonce it does not carry.
			policy.SharedContentSecurityPolicy = "default-src 'none'"
		}
	}
	return policy
}

// removeNonceSources strips every quoted nonce source from a policy value while
// retaining the directive grammar. strings.Fields alone is not sufficient:
// CSP uses semicolons as directive boundaries, and collapsing the whole value
// can accidentally turn the first source of the next directive into a source
// of the previous one when callers omit whitespace around a semicolon.
func removeNonceSources(policy string) string {
	trimmed := strings.TrimSpace(policy)
	if trimmed == "" {
		return ""
	}
	trailingSemicolon := strings.HasSuffix(trimmed, ";")
	directives := strings.Split(trimmed, ";")
	kept := make([]string, 0, len(directives))
	for _, directive := range directives {
		fields := strings.Fields(directive)
		if len(fields) == 0 {
			continue
		}
		filtered := fields[:0]
		removedNonce := false
		for _, field := range fields {
			if strings.HasPrefix(strings.ToLower(field), "'nonce-") {
				removedNonce = true
				continue
			}
			filtered = append(filtered, field)
		}
		// A source directive containing only its name after nonce removal must
		// remain present and fail closed. Dropping it would make script-src fall
		// back to a potentially broader default-src on the shared response.
		if removedNonce && len(filtered) <= 1 {
			filtered = append(filtered, "'none'")
		}
		kept = append(kept, strings.Join(filtered, " "))
	}
	result := strings.Join(kept, "; ")
	if trailingSemicolon && result != "" {
		result += ";"
	}
	return result
}

// securityHeaders resolves the configured policy when Build wraps the handler.
// Resolving there keeps a request from reading a field that EnableSecurityPolicy
// may still write.
func (a *App) securityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return securityHeadersMiddleware(a.securityPolicy)(next)
	}
}

func securityHeadersMiddleware(policy SecurityPolicy) Middleware {
	referrer := policy.ReferrerPolicy
	if referrer == "" {
		referrer = "strict-origin-when-cross-origin"
	}
	headerName := "Content-Security-Policy"
	if policy.ReportOnly {
		headerName = "Content-Security-Policy-Report-Only"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", referrer)
			if policy.FrameOptions != "" {
				w.Header().Set("X-Frame-Options", policy.FrameOptions)
			}
			if policy.StrictTransportSecurity != "" {
				w.Header().Set("Strict-Transport-Security", policy.StrictTransportSecurity)
			}
			if policy.PermissionsPolicy != "" {
				w.Header().Set("Permissions-Policy", policy.PermissionsPolicy)
			}
			if policy.ContentSecurityPolicy == "" {
				next.ServeHTTP(w, r)
				return
			}

			state := securityPolicyState{
				policy:       policy.ContentSecurityPolicy,
				sharedPolicy: policy.SharedContentSecurityPolicy,
				headerName:   headerName,
			}
			if strings.Contains(policy.ContentSecurityPolicy, NoncePlaceholder) {
				state.nonce = generateNonce(policy.NonceBytes)
				state.policy = strings.ReplaceAll(policy.ContentSecurityPolicy, NoncePlaceholder, state.nonce)
			}
			w.Header().Set(headerName, state.policy)
			next.ServeHTTP(w, r.WithContext(withSecurityPolicyState(r.Context(), state)))
		})
	}
}

// generateNonce returns a fresh base64 nonce. crypto/rand.Read never fails on
// the platforms Go supports, so a read error is fatal by design: continuing
// with a predictable nonce would defeat the policy.
func generateNonce(size int) string {
	if size < defaultNonceBytes {
		size = defaultNonceBytes
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		panic("gosx: cannot read random bytes for a Content-Security-Policy nonce: " + err.Error())
	}
	return base64.RawStdEncoding.EncodeToString(buf)
}

// RequestNonce returns the Content-Security-Policy nonce that GoSX generated for
// the request, or an empty string when no policy is active.
func RequestNonce(r *http.Request) string {
	if r == nil {
		return ""
	}
	state, ok := r.Context().Value(securityPolicyContextKey).(securityPolicyState)
	if !ok {
		return ""
	}
	return state.nonce
}

// applySharedCacheSecurityHeaders swaps in the nonce-free policy for a
// shared-cacheable response. The body carries no nonce on those responses, so a
// nonce source in the header would describe scripts that do not exist and the
// header itself could not be cached.
func applySharedCacheSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	if w == nil || r == nil {
		return
	}
	state, ok := r.Context().Value(securityPolicyContextKey).(securityPolicyState)
	if !ok || state.nonce == "" || state.headerName == "" {
		return
	}
	if state.sharedPolicy == "" {
		w.Header().Set(state.headerName, "default-src 'none'")
		return
	}
	w.Header().Set(state.headerName, state.sharedPolicy)
}
