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

// Managed script roles consumed by the GoSX navigation/runtime layer.
const (
	ManagedScriptRoleWASMExec  = "wasm-exec"
	ManagedScriptRolePatch     = "patch"
	ManagedScriptRoleBootstrap = "bootstrap"
	ManagedScriptRoleLifecycle = "lifecycle"
	ManagedScriptRoleManaged   = "managed"
)

// ManagedScriptOptions configures GoSX runtime metadata attached to an
// externally loaded script asset.
type ManagedScriptOptions struct {
	Role string
}

// ManagedScript renders a script tag with GoSX runtime ownership metadata so
// the navigation layer can reload and sequence it across page transitions.
func ManagedScript(src string, opts ManagedScriptOptions, args ...any) gosx.Node {
	src = strings.TrimSpace(src)
	if src == "" {
		return gosx.Text("")
	}
	attrs := []any{
		gosx.Attrs(
			gosx.Attr("src", AssetURL(src)),
			gosx.Attr("data-gosx-script", normalizeManagedScriptRole(opts.Role)),
		),
	}
	attrs = append(attrs, args...)
	return gosx.El("script", attrs...)
}

// LifecycleScript renders an external script that is loaded before GoSX calls
// page lifecycle hooks during navigation and can chain onto bootstrap/dispose.
func LifecycleScript(src string, args ...any) gosx.Node {
	return ManagedScript(src, ManagedScriptOptions{Role: ManagedScriptRoleLifecycle}, args...)
}

// DocumentStylesheet renders a stylesheet link tag with GoSX document/CSS
// ownership metadata so the runtime can reason about it as part of the page
// contract.
func DocumentStylesheet(href string, opts StylesheetOptions, args ...any) gosx.Node {
	layer := normalizeCSSLayer(opts.Layer)
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = stylesheetSource(href)
	}
	attrs := []any{
		gosx.Attrs(
			gosx.Attr("rel", "stylesheet"),
			gosx.Attr("href", AssetURL(href)),
			gosx.Attr("data-gosx-css-layer", string(layer)),
			gosx.Attr("data-gosx-css-owner", NormalizeStylesheetOwner(layer, opts.Owner)),
			gosx.Attr("data-gosx-css-source", source),
		),
	}
	attrs = append(attrs, args...)
	return gosx.El("link", attrs...)
}

func normalizeManagedScriptRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case ManagedScriptRoleWASMExec:
		return ManagedScriptRoleWASMExec
	case ManagedScriptRolePatch:
		return ManagedScriptRolePatch
	case ManagedScriptRoleBootstrap:
		return ManagedScriptRoleBootstrap
	case ManagedScriptRoleLifecycle:
		return ManagedScriptRoleLifecycle
	case ManagedScriptRoleManaged:
		return ManagedScriptRoleManaged
	default:
		return ManagedScriptRoleManaged
	}
}
