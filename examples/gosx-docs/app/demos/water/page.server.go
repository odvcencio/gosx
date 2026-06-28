package docs

import (
	"m31labs.dev/gosx"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Water Lab",
		"GoSX Scene3D water system with Elio simulation, Selena material hooks, caustics, reflections, and object interaction.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				addWaterDemoPreloadHead(ctx)
				return WaterDemoData()
			},
		},
	)
}

func addWaterDemoPreloadHead(ctx *route.RouteContext) {
	if ctx == nil {
		return
	}
	for _, href := range []string{
		"/water/tiles.jpg",
		"/water/xpos.jpg",
		"/water/xneg.jpg",
		"/water/ypos.jpg",
		"/water/zpos.jpg",
		"/water/zneg.jpg",
	} {
		ctx.AddHead(gosx.El("link", gosx.Attrs(
			gosx.Attr("rel", "preload"),
			gosx.Attr("as", "image"),
			gosx.Attr("href", href),
			gosx.Attr("crossorigin", "anonymous"),
		)))
	}
}
