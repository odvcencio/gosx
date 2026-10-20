package docs

import (
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

// AddScenePosterPreload starts fetching a route's WebP scene poster before
// the browser reaches the poster element in the rendered body.
func AddScenePosterPreload(ctx *route.RouteContext, href string) {
	if ctx == nil {
		return
	}
	href = strings.TrimSpace(href)
	if href == "" {
		return
	}
	ctx.AddHead(gosx.El("link", gosx.Attrs(
		gosx.Attr("rel", "preload"),
		gosx.Attr("as", "image"),
		gosx.Attr("type", "image/webp"),
		gosx.Attr("href", href),
		gosx.Attr("fetchpriority", "high"),
	)))
}
