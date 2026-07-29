package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Diegetic Panels",
		"HTML and CSS rasterized onto real 3D surfaces — rotated, occluded and legible off-axis, with no npm build step.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"scene": HTMLSurfaceProgram(),
				}, nil
			},
		},
	)
}
