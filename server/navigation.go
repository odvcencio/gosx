package server

import (
	"html"

	"m31labs.dev/gosx"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
)

// NavigationScriptAttr marks the inline script tag NavigationScript renders.
// PageState.Head uses it to detect that the navigation runtime is already in
// the head — from either App.EnableNavigation or a manual AddHead call — so
// it never injects the script twice.
const NavigationScriptAttr = "data-gosx-navigation"

// NavigationScript returns the inline GoSX page-navigation runtime. Its public
// navigate method soft-fetches only same-origin HTTP(S), hard-navigates safe
// cross-origin HTTP(S), and rejects other schemes.
func NavigationScript() gosx.Node {
	return NavigationScriptWithNonce("")
}

// NavigationScriptWithNonce returns the inline GoSX page-navigation runtime
// with a CSP nonce attribute attached. Passing an empty nonce is equivalent to
// NavigationScript.
func NavigationScriptWithNonce(nonce string) gosx.Node {
	return gosx.RawHTML(`<script ` + NavigationScriptAttr + `="true"` + nonceAttr(nonce) + `>` + runtimehost.NavigationRuntime + `</script>`)
}

func nonceAttr(nonce string) string {
	if nonce == "" {
		return ""
	}
	return ` nonce="` + html.EscapeString(nonce) + `"`
}

// Link renders an anchor tag opted into the GoSX page-navigation runtime.
func Link(href string, args ...any) gosx.Node {
	attrs := gosx.Attrs(
		gosx.Attr("href", href),
		gosx.BoolAttr(NavigationLinkAttr),
		gosx.Attr(NavigationLinkStateAttr, "idle"),
		gosx.Attr(NavigationLinkCurrentPolicyAttr, "auto"),
		gosx.Attr(NavigationLinkPrefetchStateAttr, "idle"),
	)
	attrs = append(attrs, gosx.ProgressiveEnhancementAttrs(gosx.ProgressiveEnhancementOptions{
		Kind:     "navigation",
		Layer:    "bootstrap",
		Fallback: "native-link",
	})...)
	prefixed := append([]any{
		attrs,
	}, args...)
	return gosx.El("a", prefixed...)
}

// Form renders a form tag opted into the GoSX navigation/runtime submission
// layer while preserving native HTML fallback behavior.
func Form(args ...any) gosx.Node {
	prefixed := append([]any{
		gosx.ManagedFormAttrs(gosx.ManagedFormOptions{
			State:    "idle",
			Layer:    "bootstrap",
			Fallback: "native-form",
		}),
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
