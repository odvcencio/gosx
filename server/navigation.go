package server

import (
	_ "embed"
	"html"

	"m31labs.dev/gosx"
)

//go:embed navigation_runtime.js
var navigationRuntime string

// NavigationScript returns the inline GoSX page-navigation runtime.
//
// The emitted <script> tag has no nonce attribute, so it requires
// 'unsafe-inline' (or an equivalent hash source) in a script-src Content-
// Security-Policy. Callers enforcing a strict CSP with per-request nonces
// should use NavigationScriptWithNonce instead.
func NavigationScript() gosx.Node {
	return NavigationScriptWithNonce("")
}

// NavigationScriptWithNonce returns the inline GoSX page-navigation runtime
// with a CSP nonce attribute attached, so a downstream server can ship a
// Content-Security-Policy of `script-src 'self' 'nonce-<value>'` without
// 'unsafe-inline'. Passing an empty nonce is equivalent to NavigationScript
// (no nonce attribute is emitted). The nonce value is HTML-attribute-escaped
// before being written.
func NavigationScriptWithNonce(nonce string) gosx.Node {
	return gosx.RawHTML(`<script data-gosx-navigation="true"` + nonceAttr(nonce) + `>` + navigationRuntime + `</script>`)
}

// nonceAttr renders a leading-space-prefixed `nonce="..."` HTML attribute for
// a non-empty, HTML-attribute-escaped nonce value, or an empty string when
// nonce is empty (so the caller's markup is unchanged from the no-nonce
// form).
func nonceAttr(nonce string) string {
	if nonce == "" {
		return ""
	}
	return ` nonce="` + html.EscapeString(nonce) + `"`
}

// Link renders an anchor tag opted into the GoSX page-navigation runtime.
func Link(href string, args ...any) gosx.Node {
	prefixed := append([]any{
		gosx.Attrs(
			gosx.Attr("href", href),
			gosx.BoolAttr(NavigationLinkAttr),
			gosx.Attr(NavigationLinkStateAttr, "idle"),
			gosx.Attr(NavigationLinkCurrentPolicyAttr, "auto"),
			gosx.Attr(NavigationLinkPrefetchStateAttr, "idle"),
			gosx.Attr(NavigationEnhanceAttr, "navigation"),
			gosx.Attr(NavigationEnhanceLayerAttr, "bootstrap"),
			gosx.Attr(NavigationFallbackAttr, "native-link"),
		),
	}, args...)
	return gosx.El("a", prefixed...)
}

// Form renders a form tag opted into the GoSX navigation/runtime submission
// layer while preserving native HTML fallback behavior.
func Form(args ...any) gosx.Node {
	prefixed := append([]any{
		gosx.Attrs(
			gosx.BoolAttr(NavigationFormAttr),
			gosx.Attr(NavigationFormStateAttr, "idle"),
			gosx.Attr(NavigationEnhanceAttr, "form"),
			gosx.Attr(NavigationEnhanceLayerAttr, "bootstrap"),
			gosx.Attr(NavigationFallbackAttr, "native-form"),
		),
	}, args...)
	return gosx.El("form", prefixed...)
}

// HeadOutlet wraps head content in stable markers so the navigation runtime can
// replace managed head nodes during client-side page swaps.
func HeadOutlet(head gosx.Node) gosx.Node {
	return gosx.Fragment(
		gosx.RawHTML(`<meta name="gosx-head-start" content="">`),
		head,
		gosx.RawHTML(`<meta name="gosx-head-end" content="">`),
	)
}
