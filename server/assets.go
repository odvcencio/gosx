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

// DocumentStylesheet renders a stylesheet link tag with GoSX document/CSS
// ownership metadata so the runtime can reason about it as part of the page
// contract.
func DocumentStylesheet(href string, opts StylesheetOptions, args ...any) gosx.Node {
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = stylesheetSource(href)
	}
	attrs := []any{
		gosx.Attrs(
			gosx.Attr("rel", "stylesheet"),
			gosx.Attr("href", AssetURL(href)),
			gosx.Attr("data-gosx-css-layer", string(normalizeCSSLayer(opts.Layer))),
			gosx.Attr("data-gosx-css-owner", stylesheetOwner(opts.Owner)),
			gosx.Attr("data-gosx-css-source", source),
		),
	}
	attrs = append(attrs, args...)
	return gosx.El("link", attrs...)
}
