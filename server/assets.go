package server

import (
	neturl "net/url"
	"path"
	"strings"

	"github.com/odvcencio/gosx"
)

// AssetURL returns a root-relative public asset URL for local assets while
// leaving absolute/external URLs untouched.
func AssetURL(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	if strings.HasPrefix(src, "data:") || strings.HasPrefix(src, "//") {
		return src
	}
	if parsed, err := neturl.Parse(src); err == nil && (parsed.Scheme != "" || parsed.Host != "") {
		return src
	}
	clean := path.Clean("/" + strings.TrimLeft(src, "/"))
	if clean == "." {
		return "/"
	}
	return clean
}

// Stylesheet renders a stylesheet link tag for a public asset or external URL.
func Stylesheet(href string, args ...any) gosx.Node {
	attrs := []any{
		gosx.Attrs(
			gosx.Attr("rel", "stylesheet"),
			gosx.Attr("href", AssetURL(href)),
		),
	}
	attrs = append(attrs, args...)
	return gosx.El("link", attrs...)
}
