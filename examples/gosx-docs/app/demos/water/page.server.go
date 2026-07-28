package docs

import (
	"strings"

	"m31labs.dev/gosx"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Water",
		"Flagship GoSX Scene3D water with Elio simulation, Selena optics, depth-aware light transport, caustics, and object interaction.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				addWaterDemoPreloadHead(ctx)
				ctx.AddHead(server.LifecycleScript("/water-diag.js"))
				data, err := WaterDemoData()
				if err != nil {
					return nil, err
				}
				// Cost knobs and the named Hero/Balanced/Battery profiles resolve
				// from the URL (see diag.go). Balanced is the measured default;
				// every individual diagnostic query knob remains authoritative.
				for k, v := range WaterDiagConfig(ctx) {
					data["diag"+strings.ToUpper(k[:1])+k[1:]] = v
				}
				return data, nil
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
