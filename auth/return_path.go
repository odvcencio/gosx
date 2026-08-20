package auth

import (
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxReturnPathBytes = 1024

// SafeReturnPath validates and canonicalizes an application-local destination
// carried through an authentication flow. A safe destination is a
// root-relative request URI with exactly one leading slash and, optionally, a
// query string. Decoded dot segments and redundant slashes are cleaned, and
// standard request escaping may change the supplied representation. The
// result is capped at 1,024 bytes to bound attacker-controlled ceremony state.
// The session's terminal Commit remains the authority on whether the complete
// encoded cookie fits.
//
// Callers must apply app-specific route authorization and allowlists to the
// returned canonical target. Same-origin validation is not route
// authorization.
//
// Invalid input returns ("", false). Callers deliberately choose their own
// fallback instead of this helper silently turning an unsafe value into "/".
func SafeReturnPath(raw string) (string, bool) {
	if raw == "" || len(raw) > maxReturnPathBytes || !utf8.ValidString(raw) {
		return "", false
	}
	if raw[0] != '/' || (len(raw) > 1 && raw[1] == '/') {
		return "", false
	}
	if strings.Contains(raw, "#") || hasUnsafeRawReturnPathRune(raw) {
		return "", false
	}

	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" {
		return "", false
	}
	// ParseRequestURI decodes escaped path bytes into Path. Checking the
	// decoded representation catches both encoded backslashes/controls and
	// an encoded second leading slash such as /%2Fevil.example.
	if parsed.Path == "" || parsed.Path[0] != '/' || strings.HasPrefix(parsed.Path, "//") ||
		!utf8.ValidString(parsed.Path) || hasUnsafeReturnPathRune(parsed.Path) {
		return "", false
	}

	decodedQuery, err := url.QueryUnescape(parsed.RawQuery)
	if err != nil || !utf8.ValidString(decodedQuery) || hasUnsafeReturnPathRune(decodedQuery) {
		return "", false
	}
	// Canonicalize the decoded path before any caller applies route policy.
	// net/http redirects and browsers clean dot segments and redundant
	// slashes; doing it here ensures the value an allowlist inspects is the
	// value navigation actually reaches. Rebuilding from Path also resolves
	// encoded unreserved bytes and internal encoded slashes consistently.
	cleanPath := path.Clean(parsed.Path)
	if cleanPath != "/" && strings.HasSuffix(parsed.Path, "/") {
		cleanPath += "/"
	}

	// URL.RequestURI escapes a non-ASCII path but preserves RawQuery as-is,
	// while net/http later hex-escapes non-ASCII bytes for a Location header.
	// Canonicalize those bytes here so the value we validate, count, store,
	// and return is the value a browser actually receives.
	canonical := url.URL{
		Path:       cleanPath,
		RawQuery:   escapeNonASCIIBytes(parsed.RawQuery),
		ForceQuery: parsed.ForceQuery,
	}

	target := canonical.RequestURI()
	if target == "" || len(target) > maxReturnPathBytes || !utf8.ValidString(target) ||
		target[0] != '/' || (len(target) > 1 && target[1] == '/') {
		return "", false
	}
	return target, true
}

// returnPathOr canonicalizes a caller-controlled return target and applies the
// caller's fallback when it is unsafe. Keeping fallback selection at the call
// site makes it explicit whether an invalid configured path or an invalid
// requested destination should resolve to the root or another reviewed local
// path.
func returnPathOr(raw, fallback string) string {
	if target, ok := SafeReturnPath(raw); ok {
		return target
	}
	return fallback
}

// requestedReturnPath preserves an optional explicit destination. An omitted
// destination remains omitted so a flow can use its configured success path;
// a supplied but invalid destination falls back to the root.
func requestedReturnPath(raw string) string {
	if raw == "" {
		return ""
	}
	return returnPathOr(raw, "/")
}

func hasUnsafeRawReturnPathRune(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == '\\'
	})
}

func hasUnsafeReturnPathRune(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool {
		return r == '\\' || unicode.IsControl(r)
	})
}

func escapeNonASCIIBytes(value string) string {
	first := -1
	for i := 0; i < len(value); i++ {
		if value[i] >= utf8.RuneSelf {
			first = i
			break
		}
	}
	if first == -1 {
		return value
	}

	const upperHex = "0123456789ABCDEF"
	var out strings.Builder
	out.Grow(len(value) + 2*(len(value)-first))
	out.WriteString(value[:first])
	for i := first; i < len(value); i++ {
		b := value[i]
		if b < utf8.RuneSelf {
			out.WriteByte(b)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(upperHex[b>>4])
		out.WriteByte(upperHex[b&0x0f])
	}
	return out.String()
}
