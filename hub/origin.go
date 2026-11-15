package hub

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// OriginAllowlist is an Origin policy for WebSocket clients served from other
// origins, such as a static game client hosted on a CDN or inside a game
// portal's iframe. Install it on a hub with
//
//	allow, err := hub.NewOriginAllowlist("https://play.example.com", "https://*.example.net")
//	h.CheckOrigin = allow.Check
//
// Check accepts a request when its Origin is the hub's own origin (the default
// rule), when it matches an entry, or when it has no Origin at all. Browsers
// always send Origin on a WebSocket upgrade, so an absent Origin means a
// non-browser client; set Hub.RequireOrigin to refuse those too.
//
// An entry is one of:
//
//   - an exact origin, "https://play.example.com" or "http://localhost:8080";
//   - a subdomain pattern, "https://*.example.com", which matches any host that
//     ends in ".example.com" (not "example.com" itself) with the same scheme
//     and port;
//   - "null", the Origin a sandboxed iframe without allow-same-origin sends.
//     Allowing it admits every such iframe, from any site, so enable it only
//     when a host requires it.
//
// Scheme and host compare case-insensitively, and a default port (80 for http,
// 443 for https) matches the same origin written without one. An allowlist is
// immutable and safe for concurrent use.
type OriginAllowlist struct {
	exact    map[string]struct{}
	patterns []originPattern
	null     bool
}

type originPattern struct {
	scheme string
	suffix string // ".example.com"
	port   string // normalized; empty for the scheme default
}

// ErrInvalidOrigin reports an allowlist entry that is not an origin.
var ErrInvalidOrigin = errors.New("hub: invalid origin")

// NewOriginAllowlist parses origins into an allowlist. It returns an error
// wrapping ErrInvalidOrigin for an entry that is not an http or https origin,
// carries a path, query or user info, or uses "*" anywhere except as the whole
// first host label.
func NewOriginAllowlist(origins ...string) (*OriginAllowlist, error) {
	a := &OriginAllowlist{exact: make(map[string]struct{}, len(origins))}
	for _, raw := range origins {
		entry := strings.TrimSpace(raw)
		if entry == "null" {
			a.null = true
			continue
		}
		scheme, host, port, err := splitOrigin(entry)
		if err != nil {
			return nil, fmt.Errorf("%w %q: %v", ErrInvalidOrigin, raw, err)
		}
		if strings.HasPrefix(host, "*.") {
			suffix := host[1:]
			if strings.Contains(suffix, "*") || len(suffix) < 2 {
				return nil, fmt.Errorf("%w %q: wildcard must be the whole first label", ErrInvalidOrigin, raw)
			}
			a.patterns = append(a.patterns, originPattern{scheme: scheme, suffix: suffix, port: port})
			continue
		}
		if strings.Contains(host, "*") {
			return nil, fmt.Errorf("%w %q: wildcard must be the whole first label", ErrInvalidOrigin, raw)
		}
		a.exact[joinOrigin(scheme, host, port)] = struct{}{}
	}
	return a, nil
}

// MustOriginAllowlist is NewOriginAllowlist for fixed configuration; it panics
// on an invalid entry.
func MustOriginAllowlist(origins ...string) *OriginAllowlist {
	a, err := NewOriginAllowlist(origins...)
	if err != nil {
		panic(err)
	}
	return a
}

// Allowed reports whether origin, the value of an Origin header, matches an
// entry. It does not apply the same-origin rule; Check does.
func (a *OriginAllowlist) Allowed(origin string) bool {
	if a == nil {
		return false
	}
	if origin == "null" {
		return a.null
	}
	scheme, host, port, err := splitOrigin(origin)
	if err != nil || strings.Contains(host, "*") {
		return false
	}
	if _, ok := a.exact[joinOrigin(scheme, host, port)]; ok {
		return true
	}
	for _, p := range a.patterns {
		if p.scheme == scheme && p.port == port && len(host) > len(p.suffix) && strings.HasSuffix(host, p.suffix) {
			return true
		}
	}
	return false
}

// Check is a Hub.CheckOrigin policy: it accepts a request with no Origin, a
// same-origin request, and a request whose Origin is Allowed.
func (a *OriginAllowlist) Check(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" || sameOrigin(r, false) {
		return true
	}
	return a.Allowed(origin)
}

func splitOrigin(origin string) (scheme, host, port string, err error) {
	if origin == "" {
		return "", "", "", errors.New("empty")
	}
	// url.Parse rejects "*" in a host, so swap a leading wildcard label for a
	// placeholder label while parsing.
	const placeholder = "wildcard-placeholder-label"
	parsed := origin
	wildcard := false
	if i := strings.Index(origin, "://*."); i >= 0 {
		parsed = origin[:i+3] + placeholder + origin[i+4:]
		wildcard = true
	}
	u, perr := url.Parse(parsed)
	if perr != nil {
		return "", "", "", perr
	}
	scheme = strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", "", errors.New("scheme must be http or https")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", "", "", errors.New("an origin has no user info, path, query or fragment")
	}
	host = strings.ToLower(u.Hostname())
	if host == "" {
		return "", "", "", errors.New("missing host")
	}
	if wildcard {
		host = "*" + strings.TrimPrefix(host, placeholder)
	}
	port = u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	return scheme, host, port, nil
}

func joinOrigin(scheme, host, port string) string {
	if strings.Contains(host, ":") { // IPv6 literal
		host = "[" + host + "]"
	}
	if port == "" {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}
