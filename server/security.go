package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// NoncePlaceholder marks the spot in a Content-Security-Policy value where GoSX
// writes the per-request nonce. Write it inside the quoted nonce source, as in
// "script-src 'self' 'nonce-{nonce}'".
const NoncePlaceholder = "{nonce}"

// SecurityPolicy configures the optional security response headers.
//
// The default policy restricts framing to the same origin. Script and other
// content restrictions are opt-in through EnableSecurityPolicy. Apps that
// permit embedding can supply an explicit frame-ancestors directive.
//
// A shared-cacheable response carries no nonce, because a shared cache would
// replay one client's nonce to the next client. GoSX drops the nonce from the
// body and sends SharedContentSecurityPolicy on those responses. Set that field
// to a policy that needs no nonce, such as one built from hashes or from
// 'strict-dynamic'. Leave it empty to let GoSX remove the nonce source from
// ContentSecurityPolicy, which then blocks the inline scripts on that page.
type SecurityPolicy struct {
	// FrameAncestors explicitly permits these CSP sources to embed the app.
	// Empty retains the existing default policy. Sources must be HTTP(S) origins
	// (optionally a wildcard subdomain), 'self', or 'none'.
	FrameAncestors []string

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
// Call it before Build. Passing a zero SecurityPolicy restores the default framing policy
// again and restores the default headers alone. Invalid frame ancestors or
// conflicting X-Frame-Options return an error without applying the policy.
func (a *App) EnableSecurityPolicy(policy SecurityPolicy) error {
	if a == nil {
		return nil
	}
	if err := validateFrameAncestors(policy); err != nil {
		return err
	}
	policy.FrameAncestors = append([]string(nil), policy.FrameAncestors...)
	a.securityPolicy = normalizeSecurityPolicy(policy)
	return nil
}

func normalizeSecurityPolicy(policy SecurityPolicy) SecurityPolicy {
	policy.ContentSecurityPolicy = strings.TrimSpace(policy.ContentSecurityPolicy)
	if len(policy.FrameAncestors) > 0 {
		directive := "frame-ancestors " + strings.Join(policy.FrameAncestors, " ")
		policy.ContentSecurityPolicy = withFrameAncestors(policy.ContentSecurityPolicy, directive)
		if strings.TrimSpace(policy.SharedContentSecurityPolicy) != "" {
			policy.SharedContentSecurityPolicy = withFrameAncestors(policy.SharedContentSecurityPolicy, directive)
		}
	}
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
	if policy.ContentSecurityPolicy == "" {
		policy.ContentSecurityPolicy = "frame-ancestors 'self'"
		if strings.EqualFold(strings.TrimSpace(policy.FrameOptions), "DENY") {
			policy.ContentSecurityPolicy = "frame-ancestors 'none'"
		}
	}
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
			if policy.ReportOnly {
				framing := "frame-ancestors 'self'"
				if len(policy.FrameAncestors) > 0 {
					framing = "frame-ancestors " + strings.Join(policy.FrameAncestors, " ")
				}
				if strings.EqualFold(policy.FrameOptions, "DENY") {
					framing = "frame-ancestors 'none'"
				}
				w.Header().Set("Content-Security-Policy", framing)
			}
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

func validateFrameAncestors(policy SecurityPolicy) error {
	if len(policy.FrameAncestors) == 0 {
		return nil
	}
	if strings.TrimSpace(policy.FrameOptions) != "" {
		return fmt.Errorf("gosx: frame ancestors cannot be combined with X-Frame-Options")
	}
	for _, source := range policy.FrameAncestors {
		if source == "'self'" {
			continue
		}
		if source == "'none'" && len(policy.FrameAncestors) == 1 {
			continue
		}
		u, err := url.Parse(source)
		if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (strings.ContainsAny(source, "\\;#") || strings.IndexFunc(source, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0) {
			return fmt.Errorf("gosx: invalid frame ancestor %q", source)
		}
		host := strings.TrimPrefix(u.Hostname(), "*.")
		if host == "" || strings.Contains(host, "*") {
			return fmt.Errorf("gosx: invalid frame ancestor %q", source)
		}
	}
	return nil
}

func withFrameAncestors(policy, directive string) string {
	var kept []string
	for _, part := range strings.Split(policy, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 && !strings.EqualFold(fields[0], "frame-ancestors") {
			kept = append(kept, strings.TrimSpace(part))
		}
	}
	return strings.Join(append(kept, directive), "; ")
}
