package telemetry

import (
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Validation is pure: parsing addresses does not resolve names or open files.
// Token-file contents and Unix ownership are checked once by listener startup.
func validateListenerOptions(o ListenOptions) error {
	for _, credential := range []Credential{o.Metrics, o.Admin} {
		if credential.Token != "" && credential.TokenFile != "" {
			return invalid("credential", "duplicate_source")
		}
		if credential.Token != "" && (len(credential.Token) < 32 || len(credential.Token) > 2048 || !validCredentialToken(credential.Token)) {
			return invalid("credential", "token")
		}
		if len(credential.TokenFile) > 4096 || strings.ContainsRune(credential.TokenFile, 0) {
			return invalid("credential", "file")
		}
	}
	if o.Metrics.Token != "" && o.Metrics.Token == o.Admin.Token || o.Metrics.TokenFile != "" && o.Admin.TokenFile != "" && filepath.Clean(o.Metrics.TokenFile) == filepath.Clean(o.Admin.TokenFile) {
		return invalid("credential", "shared_credential")
	}
	if o.Addr == "" || strings.EqualFold(o.Addr, "off") {
		return nil
	}
	if len(o.Addr) > 4096 || strings.ContainsRune(o.Addr, 0) {
		return invalid("listen", "address")
	}
	if strings.HasPrefix(o.Addr, "unix:") {
		path := strings.TrimPrefix(o.Addr, "unix:")
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
			return invalid("listen", "unix_path")
		}
		capacity := unixSocketPathCapacity()
		if capacity == 0 {
			return invalid("listen", "unsupported")
		}
		if len(path) >= capacity { // One byte is reserved for the terminating NUL.
			return invalid("listen", "unix_path")
		}
		return nil
	}
	host, portText, err := net.SplitHostPort(o.Addr)
	if err != nil || portText == "" {
		return invalid("listen", "address")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil { // Literal IPs only, including IPv6; no DNS or wildcard aliases.
		return invalid("listen", "address")
	}
	for _, c := range portText {
		if c < '0' || c > '9' {
			return invalid("listen", "port")
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port > 65535 || port == 0 && !ip.IsLoopback() {
		return invalid("listen", "port")
	}
	if !ip.IsLoopback() && o.Metrics.Token == "" && o.Metrics.TokenFile == "" && !o.DangerouslyAllowUnauthenticatedMetricsOnNonLoopback {
		return errors.Join(ErrInsecureListener, invalid("listen", "auth_required"))
	}
	return nil
}

// Match the platform sockaddr_un path array without importing native syscall
// structures into the portable options package.
func unixSocketPathCapacity() int {
	switch runtime.GOOS {
	case "linux", "android", "solaris", "illumos":
		return 108
	case "darwin", "ios", "freebsd", "openbsd", "netbsd", "dragonfly":
		return 104
	case "aix":
		return 1023
	default:
		return 0
	}
}

// This only validates configuration grammar. HTTP authentication uses the
// existing auth.RequireBearerToken middleware and its digest comparison.
func validCredentialToken(token string) bool {
	data, padding := false, false
	for i := range token {
		c := token[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+/", rune(c)):
			if padding {
				return false
			}
			data = true
		case c == '=':
			if !data {
				return false
			}
			padding = true
		default:
			return false
		}
	}
	return data
}
