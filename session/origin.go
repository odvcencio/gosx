package session

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// ForwardedTrust allows listed reverse proxies to supply the public host and
// scheme. Enable it only when those proxies replace forwarded headers.
type ForwardedTrust struct {
	Enabled bool
	Proxies []string // IP addresses or CIDR blocks.
}

type originPolicy struct {
	protection *http.CrossOriginProtection
	trusted    map[string]bool
	hosts      map[string]bool
	proxies    []netip.Prefix
}

func newOriginPolicy(opts Options) (*originPolicy, error) {
	p := &originPolicy{
		protection: http.NewCrossOriginProtection(),
		trusted:    make(map[string]bool),
		hosts:      make(map[string]bool),
	}
	for _, origin := range opts.TrustedOrigins {
		if !validOrigin(origin) {
			return nil, fmt.Errorf("session: invalid trusted origin %q", origin)
		}
		if err := p.protection.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
		p.trusted[origin] = true
	}
	for _, host := range opts.AllowedHosts {
		if !validOrigin("https://" + host) {
			return nil, fmt.Errorf("session: invalid allowed host %q", host)
		}
		p.hosts[strings.ToLower(host)] = true
	}
	if opts.ForwardedTrust.Enabled {
		if len(opts.ForwardedTrust.Proxies) == 0 || len(p.hosts) == 0 {
			return nil, fmt.Errorf("session: forwarded trust requires proxy addresses and allowed hosts")
		}
		for _, value := range opts.ForwardedTrust.Proxies {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				addr, addrErr := netip.ParseAddr(value)
				if addrErr != nil {
					return nil, fmt.Errorf("session: invalid trusted proxy %q", value)
				}
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
			p.proxies = append(p.proxies, prefix.Masked())
		}
	}
	return p, nil
}

// check uses the standard browser origin guard, then checks Origin as well:
// contradictory headers and HTTP-to-HTTPS origin changes fail closed. Missing
// browser headers are not evidence of same origin; Protect requires a token.
func (p *originPolicy) check(r *http.Request) (bool, error) {
	scheme, host := "http", r.Host
	if r.TLS != nil {
		scheme = "https"
	}
	if p.trustsPeer(r.RemoteAddr) {
		if value := r.Header.Get("X-Forwarded-Host"); value != "" {
			host = firstForwardedValue(value)
		}
		if value := r.Header.Get("X-Forwarded-Proto"); value != "" {
			scheme = firstForwardedValue(value)
		}
	}
	if !validOrigin(scheme+"://"+host) || (len(p.hosts) > 0 && !p.hosts[strings.ToLower(host)]) {
		return false, fmt.Errorf("session: request origin is not allowed")
	}
	checked := r
	if host != r.Host {
		checked = r.Clone(r.Context())
		checked.Host = host
	}
	if err := p.protection.Check(checked); err != nil {
		return false, err
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		if !validOrigin(origin) || (!p.trusted[origin] && !strings.EqualFold(origin, scheme+"://"+host)) {
			return false, fmt.Errorf("session: browser origin is not allowed")
		}
		return true, nil
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin", nil
}

func validOrigin(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") &&
		u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" &&
		!u.ForceQuery && u.Fragment == "" && !strings.HasSuffix(value, "#")
}

func (p *originPolicy) trustsPeer(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, prefix := range p.proxies {
		if prefix.Contains(addr.Unmap()) {
			return true
		}
	}
	return false
}

func firstForwardedValue(value string) string {
	first, _, _ := strings.Cut(value, ",")
	return strings.TrimSpace(first)
}
