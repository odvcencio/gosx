package server

import (
	"net/http"
	"net/netip"
	"strings"

	"m31labs.dev/gosx/internal/telemetryerr"
)

const (
	maxTrustedProxyPrefixes = 64
	maxForwardedBytes       = 2048
	maxForwardedHops        = 16
)

// TrustedProxyPolicy declares which genuine peers may supply a client address.
// Proxies contains at most 64 literal IP addresses or CIDRs. Empty trusts nobody;
// policies covering every IPv4 or IPv6 address are rejected. Header defaults to
// X-Forwarded-For, limited to 16 bare IP addresses and 2048 bytes. An explicit
// alternative must contain a single literal address. Repeated header lines and
// malformed values fall back to the peer. RFC 7239 Forwarded is unsupported and
// ignored, including when X-Forwarded-For is also present.
type TrustedProxyPolicy struct {
	Proxies []string
	Header  string
}

type compiledProxyPolicy struct {
	prefixes []netip.Prefix
	header   string
}

type requestMetadata struct {
	id           string
	peer, client netip.Addr
}

// SetTrustedProxies compiles and copies the policy before the first Build. It
// does not change the origin, host, scheme, or CSRF policies owned by other
// packages. An invalid replacement leaves the previous policy intact.
func (a *App) SetTrustedProxies(policy TrustedProxyPolicy) error {
	if a == nil {
		return shutdownConfigError(&telemetryerr.ConfigError{Field: "app", Code: "required"})
	}
	a.shutdown.mu.Lock()
	defer a.shutdown.mu.Unlock()
	if !a.ConfigurationOpen() {
		return shutdownConfigError(telemetryerr.ErrAfterBuild)
	}
	compiled, err := compileProxyPolicy(policy)
	if err != nil {
		return err
	}
	a.trustedProxies = compiled
	return nil
}

func compileProxyPolicy(policy TrustedProxyPolicy) (*compiledProxyPolicy, error) {
	invalid := func(code string) error {
		return shutdownConfigError(&telemetryerr.ConfigError{Field: "trusted_proxies", Code: code})
	}
	if len(policy.Proxies) > maxTrustedProxyPrefixes {
		return nil, invalid("prefix_cap")
	}
	header := policy.Header
	if header == "" {
		header = "X-Forwarded-For"
	}
	if !validAddressHeader(header) || strings.EqualFold(header, "Forwarded") {
		return nil, invalid("header_invalid")
	}
	compiled := &compiledProxyPolicy{header: strings.Clone(http.CanonicalHeaderKey(header))}
	for _, text := range policy.Proxies {
		text = strings.TrimSpace(text)
		if len(text) > 128 {
			return nil, invalid("prefix_invalid")
		}
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			addr, parseErr := netip.ParseAddr(text)
			if parseErr != nil || addr.Zone() != "" {
				return nil, invalid("prefix_invalid")
			}
			addr = addr.Unmap()
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return nil, invalid("prefix_invalid")
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		compiled.prefixes = append(compiled.prefixes, prefix.Masked())
	}
	for _, root := range []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")} {
		if proxyPrefixesCover(compiled.prefixes, root) {
			return nil, invalid("all_addresses")
		}
	}
	return compiled, nil
}

func validAddressHeader(header string) bool {
	if len(header) == 0 || len(header) > 128 {
		return false
	}
	for i := range len(header) {
		c := header[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

// Detect union coverage too, so splitting /0 into smaller prefixes does not
// bypass the all-address restriction. Recursion visits only declared subtrees.
func proxyPrefixesCover(prefixes []netip.Prefix, root netip.Prefix) bool {
	overlaps := false
	for _, prefix := range prefixes {
		if prefix.Addr().BitLen() != root.Addr().BitLen() {
			continue
		}
		if prefix.Bits() <= root.Bits() && prefix.Contains(root.Addr()) {
			return true
		}
		overlaps = overlaps || root.Contains(prefix.Addr())
	}
	if !overlaps || root.Bits() == root.Addr().BitLen() {
		return false
	}
	bit := root.Bits()
	left := netip.PrefixFrom(root.Addr(), bit+1)
	var right netip.Prefix
	if root.Addr().Is4() {
		raw := root.Addr().As4()
		raw[bit/8] |= 1 << (7 - bit%8)
		right = netip.PrefixFrom(netip.AddrFrom4(raw), bit+1)
	} else {
		raw := root.Addr().As16()
		raw[bit/8] |= 1 << (7 - bit%8)
		right = netip.PrefixFrom(netip.AddrFrom16(raw), bit+1)
	}
	return proxyPrefixesCover(prefixes, left) && proxyPrefixesCover(prefixes, right)
}

// ClientIP returns the normalized address cached by the App request metadata
// owner. Bare handlers resolve only the genuine peer. Invalid peers return the
// zero address, allowing callers to use one fixed denial bucket. This transient
// value must not be used as a metric label or persisted identifier.
func ClientIP(r *http.Request) netip.Addr {
	if r == nil {
		return netip.Addr{}
	}
	if metadata := cachedRequestMetadata(r); metadata != nil {
		return metadata.client
	}
	return requestPeer(r.RemoteAddr)
}

// The observer wrapper precedes middleware and shares its observation state.
// Publish the same metadata pointer there so completion observers see the one
// cached resolution even though their request precedes the middleware copy.
func cachedRequestMetadata(r *http.Request) *requestMetadata {
	if metadata, ok := r.Context().Value(requestIDContextKey).(*requestMetadata); ok {
		return metadata
	}
	if state, ok := r.Context().Value(requestObservationContextKey).(*requestObservation); ok {
		return state.metadata
	}
	return nil
}

func requestPeer(remote string) netip.Addr {
	if len(remote) > 128 {
		return netip.Addr{}
	}
	addr, err := netip.ParseAddr(remote)
	if err != nil {
		withPort, parseErr := netip.ParseAddrPort(remote)
		if parseErr != nil {
			return netip.Addr{}
		}
		addr = withPort.Addr()
	}
	if addr.Zone() != "" {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func (policy *compiledProxyPolicy) trusts(addr netip.Addr) bool {
	if policy != nil {
		for _, prefix := range policy.prefixes {
			if prefix.Contains(addr) {
				return true
			}
		}
	}
	return false
}

func (policy *compiledProxyPolicy) resolve(peer netip.Addr, header http.Header) netip.Addr {
	if !peer.IsValid() || !policy.trusts(peer) {
		return peer
	}
	values := header.Values(policy.header)
	if len(values) != 1 || len(values[0]) > maxForwardedBytes {
		return peer
	}
	var hops [maxForwardedHops]netip.Addr
	count, text := 0, values[0]
	for {
		part, rest, more := strings.Cut(text, ",")
		if count == len(hops) || more && policy.header != "X-Forwarded-For" {
			return peer
		}
		addr, err := netip.ParseAddr(strings.Trim(part, " \t"))
		if err != nil || addr.Zone() != "" {
			return peer
		}
		hops[count] = addr.Unmap()
		count++
		if !more {
			break
		}
		text = rest
	}
	for i := count - 1; i >= 0; i-- {
		if !policy.trusts(hops[i]) || i == 0 {
			return hops[i]
		}
	}
	return peer
}
