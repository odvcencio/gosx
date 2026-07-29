package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage("Blackglass Coast", "A Studio-authored volcanic cove bound to GoSX Scene3D water, gameplay anchors, and performance telemetry.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{"scene": BlackglassBeaconProgram()}, nil
		},
	})
}
