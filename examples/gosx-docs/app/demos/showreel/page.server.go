package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	demos "m31labs.dev/gosx/examples/gosx-docs/app/demos"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage("Orbital sculpture", "Turn a compact Scene3D composition built from typed Go scene data.", route.FileModuleOptions{
		Load: func(_ *route.RouteContext, _ route.FilePage) (any, error) {
			return map[string]any{"scene": demos.DemoShowreelProgram()}, nil
		},
	})
}
