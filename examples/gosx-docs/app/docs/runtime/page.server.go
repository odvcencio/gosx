package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Runtime",
		"Hydration bootstrap, page disposal, and streamed regions cooperate during client-side transitions.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"sceneDemo": map[string]any{
						"width":        720.0,
						"height":       420.0,
						"label":        "GoSX Scene3D runtime demo",
						"background":   "#08151f",
						"programRef":   "/api/runtime/scene-program",
						"capabilities": []string{"pointer", "keyboard"},
					},
				}, nil
			},
		},
	)
}
