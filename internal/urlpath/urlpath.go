// Package urlpath resolves public paths without server dependencies.
package urlpath

import (
	"fmt"
	"path"
	"strings"
)

// Normalize accepts an unescaped, canonical root-relative path made of URL
// unreserved characters and slashes, with no trailing slash.
func Normalize(value string) (string, error) {
	if value == "" || value == "/" {
		return "", nil
	}
	if !strings.HasPrefix(value, "/") || strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/-._~", r))
	}) >= 0 || strings.HasPrefix(value, "//") || path.Clean(value) != strings.TrimSuffix(value, "/") {
		return "", fmt.Errorf("gosx: base path must be a canonical root-relative path")
	}
	return strings.TrimSuffix(value, "/"), nil
}

// URL converts an internal root-relative URL to its public URL. The prefix is
// always prepended, even when the internal path matches it. External and
// relative URLs retain their meaning. Query strings and fragments are preserved.
func URL(prefix, value string) string {
	if prefix == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.HasPrefix(value, "/\\") {
		return value
	}
	return prefix + value
}
