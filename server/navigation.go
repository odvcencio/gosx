package server

import (
	_ "embed"

	"github.com/odvcencio/gosx"
)

//go:embed navigation_runtime.js
var navigationRuntime string

// NavigationScript returns the inline GoSX page-navigation runtime.
func NavigationScript() gosx.Node {
	return gosx.RawHTML(`<script data-gosx-navigation="true">` + navigationRuntime + `</script>`)
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
