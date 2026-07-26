package auth

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

var (
	// ErrOriginNotConfigured reports a flow that needs an absolute base URL but
	// has none. GoSX does not derive the base URL from request headers,
	// because a client controls the Host header.
	ErrOriginNotConfigured = errors.New("auth: absolute base url is not configured")

	// ErrOriginNotAllowed reports a request host that is absent from the
	// configured host allowlist.
	ErrOriginNotAllowed = errors.New("auth: request host is not allowed")
)

// ForwardedTrust decides when GoSX reads the X-Forwarded-Proto and
// X-Forwarded-Host headers. Both headers are client data unless a reverse
// proxy rewrites them, so GoSX ignores them until you opt in and name the
// proxies.
type ForwardedTrust struct {
	// Enabled reads the forwarded headers for a request that arrives from a
	// listed proxy.
	Enabled bool

	// Proxies lists the proxy addresses that may set the forwarded headers.
	// Give single IP addresses or CIDR blocks. An empty list trusts no peer,
	// so the forwarded headers stay ignored.
	Proxies []string
}

// originResolver turns a request into an absolute origin, such as
// "https://app.example". It prefers the configured base URL and falls back to
// the request only for a host on the allowlist.
type originResolver struct {
	baseURL      string
	allowedHosts []string
	trust        bool
	proxies      []netip.Prefix
}

// newOriginResolver validates the origin configuration once, at startup.
func newOriginResolver(baseURL string, allowedHosts []string, trust ForwardedTrust) (*originResolver, error) {
	resolver := &originResolver{}
	base := strings.TrimSpace(baseURL)
	if base != "" {
		normalized, err := normalizeBaseURL(base)
		if err != nil {
			return nil, err
		}
		resolver.baseURL = normalized
	}
	for _, host := range allowedHosts {
		normalized := normalizeAllowedHost(host)
		if normalized == "" {
			continue
		}
		resolver.allowedHosts = append(resolver.allowedHosts, normalized)
	}
	resolver.trust = trust.Enabled
	for _, proxy := range trust.Proxies {
		prefix, err := parseProxyPrefix(proxy)
		if err != nil {
			return nil, err
		}
		resolver.proxies = append(resolver.proxies, prefix)
	}
	if resolver.trust && len(resolver.proxies) == 0 {
		return nil, fmt.Errorf("auth: forwarded header trust requires at least one proxy address")
	}
	return resolver, nil
}

// baseConfigured reports whether an absolute base URL is present.
func (o *originResolver) baseConfigured() bool {
	return o != nil && o.baseURL != ""
}

// resolve returns the absolute origin for a request.
func (o *originResolver) resolve(r *http.Request) (string, error) {
	if o == nil {
		return "", ErrOriginNotConfigured
	}
	if o.baseURL != "" {
		return o.baseURL, nil
	}
	if len(o.allowedHosts) == 0 || r == nil {
		return "", ErrOriginNotConfigured
	}
	scheme, host := requestSchemeAndHost(r, o.trustsPeer(r))
	if host == "" {
		return "", ErrOriginNotConfigured
	}
	if !o.hostAllowed(host) {
		return "", fmt.Errorf("%w: %q", ErrOriginNotAllowed, host)
	}
	return scheme + "://" + host, nil
}

// trustsPeer reports whether the request arrives from a listed proxy.
func (o *originResolver) trustsPeer(r *http.Request) bool {
	if o == nil || !o.trust || r == nil {
		return false
	}
	addr, err := peerAddr(r.RemoteAddr)
	if err != nil {
		return false
	}
	for _, prefix := range o.proxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (o *originResolver) hostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	bare := host
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		bare = hostname
	}
	for _, allowed := range o.allowedHosts {
		if allowed == host || allowed == bare {
			return true
		}
	}
	return false
}

// requestSchemeAndHost reads the scheme and host of a request. It reads the
// forwarded headers only when trustForwarded is true.
func requestSchemeAndHost(r *http.Request, trustForwarded bool) (string, string) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if trustForwarded {
		if forwarded := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
			scheme = forwarded
		}
		if forwarded := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
			host = forwarded
		}
	}
	if scheme != "http" && scheme != "https" {
		return "", ""
	}
	if !validHostValue(host) {
		return "", ""
	}
	return scheme, strings.ToLower(host)
}

// firstForwardedValue reads the left-most value of a comma separated header.
func firstForwardedValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if index := strings.Index(value, ","); index >= 0 {
		value = value[:index]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

// validHostValue rejects a host that carries a path, a scheme, credentials, or
// a control character.
func validHostValue(host string) bool {
	if host == "" || len(host) > 253+6 {
		return false
	}
	for _, r := range host {
		switch {
		case r <= 0x20 || r == 0x7f:
			return false
		case r == '/' || r == '\\' || r == '@' || r == '?' || r == '#' || r == '%':
			return false
		}
	}
	return true
}

func normalizeBaseURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("auth: invalid base url %q: %w", value, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("auth: base url %q needs an http or https scheme", value)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("auth: base url %q needs a host", value)
	}
	origin := parsed.Scheme + "://" + parsed.Host
	path := strings.TrimRight(parsed.Path, "/")
	return origin + path, nil
}

func normalizeAllowedHost(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
			return parsed.Host
		}
	}
	return strings.TrimSuffix(value, "/")
}

func parseProxyPrefix(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Prefix{}, fmt.Errorf("auth: empty trusted proxy address")
	}
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("auth: invalid trusted proxy cidr %q: %w", value, err)
		}
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("auth: invalid trusted proxy address %q: %w", value, err)
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func peerAddr(remoteAddr string) (netip.Addr, error) {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return netip.Addr{}, fmt.Errorf("auth: empty remote address")
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}
	addr, err := netip.ParseAddr(remoteAddr)
	if err != nil {
		return netip.Addr{}, err
	}
	return addr.Unmap(), nil
}
