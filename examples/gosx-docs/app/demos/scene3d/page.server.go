package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
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
