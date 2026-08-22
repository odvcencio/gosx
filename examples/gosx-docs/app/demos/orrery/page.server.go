package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage("Lodestar Meridian", "A clockwork star-system engine whose ignition, procession, and transit choreography is declared entirely as typed GoSX Scene3D animation data.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{"scene": LodestarMeridianProgram()}, nil
		},
	})
}
