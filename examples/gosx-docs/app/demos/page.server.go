package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Demos",
		"A tour of GoSX capabilities — servers, islands, real-time, simulation, and 3D.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"demos": Demos(),
				}, nil
			},
		},
	)
}
