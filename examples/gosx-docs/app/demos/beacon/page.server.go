package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage("Blackglass Beacon — Eclipse Protocol", "An asset-free, bounded Scene3D tower that demonstrates a cinematic Go-authored scene with truthful backend fallback.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{"scene": BlackglassBeaconProgram()}, nil
		},
	})
}
