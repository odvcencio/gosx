package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage("Geometry Zoo", "Cinematic PBR showcase with three-point lighting, ACES tonemapping, and bloom — all declared in Go.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"scene": GeometryZooProgram(),
			}, nil
		},
	})
}
