package server

import (
	neturl "net/url"
	"path"
	"strings"

	"m31labs.dev/gosx"
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
	ManagedScriptRoleWASMExec           = "wasm-exec"
	ManagedScriptRoleStandardGoWASMExec = "standard-go-wasm-exec"
	ManagedScriptRolePatch              = "patch"
	ManagedScriptRoleBootstrap          = "bootstrap"
	ManagedScriptRoleLifecycle          = "lifecycle"
	ManagedScriptRoleManaged            = "managed"
)

// ManagedScriptOptions configures GoSX runtime metadata attached to an
// externally loaded script asset. GoSX always loads managed scripts through a
// real DOM script element; there is no fetch/eval mode because it cannot satisfy
// strict CSP and does not preserve normal browser script semantics.
type ManagedScriptOptions struct {
	Role           string
	Type           string
	Integrity      string
	CrossOrigin    string
	ReferrerPolicy string
}

// ManagedScript renders a script tag with GoSX runtime ownership metadata so
// the navigation layer can reload and sequence it across page transitions.
func ManagedScript(src string, opts ManagedScriptOptions, args ...any) gosx.Node {
	return managedScriptWithNonce(src, opts, "", args...)
}

func managedScriptWithNonce(src string, opts ManagedScriptOptions, nonce string, args ...any) gosx.Node {
	src = strings.TrimSpace(src)
	if src == "" {
		return gosx.Text("")
	}
	baseAttrs := gosx.Attrs(
		gosx.Attr("src", AssetURL(src)),
		gosx.Attr("data-gosx-script", normalizeManagedScriptRole(opts.Role)),
		gosx.Attr("type", normalizeManagedScriptType(opts.Type)),
		gosx.Attr("crossorigin", normalizeManagedScriptCrossOrigin(opts.CrossOrigin)),
		gosx.Attr("referrerpolicy", normalizeManagedScriptReferrerPolicy(opts.ReferrerPolicy)),
	)
	if integrity := strings.TrimSpace(opts.Integrity); integrity != "" {
		baseAttrs = append(baseAttrs, gosx.Attr("integrity", integrity))
	}
	if nonce != "" {
		// Keep the framework nonce in the first attribute list so a later
		// caller-supplied duplicate cannot replace the request-scoped value.
		baseAttrs = append(baseAttrs, gosx.Attr("nonce", nonce))
	}
	attrs := []any{baseAttrs}
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
	case ManagedScriptRoleStandardGoWASMExec:
		return ManagedScriptRoleStandardGoWASMExec
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

func normalizeManagedScriptType(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "text/javascript"
}

func normalizeManagedScriptCrossOrigin(value string) string {
	if value = strings.TrimSpace(strings.ToLower(value)); value != "" {
		return value
	}
	return "anonymous"
}

func normalizeManagedScriptReferrerPolicy(value string) string {
	if value = strings.TrimSpace(strings.ToLower(value)); value != "" {
		return value
	}
	return "no-referrer"
}
