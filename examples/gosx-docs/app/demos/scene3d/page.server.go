package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Geometry Zoo",
		"Interactive native 3D primitives rendered inside the routed GoSX application shell.",
		route.FileModuleOptions{
			Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
				return server.Metadata{
					Title: "Geometry Zoo | GoSX",
				}, nil
			},
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"scene": map[string]any{
						"width":        920.0,
						"height":       560.0,
						"label":        "GoSX Geometry Zoo",
						"background":   "#08151f",
						"programRef":   "/api/demos/scene-program",
						"capabilities": []string{"pointer", "keyboard"},
					},
					"traits": []map[string]string{
						{
							"kicker": "Pointer",
							"title":  "Pull the camera across the surface.",
							"body":   "The canvas follows live pointer position so the scene stays reactive on desktop and touch hardware.",
						},
						{
							"kicker": "Arrow keys",
							"title":  "Push the palette and motion system.",
							"body":   "Left and right rebalance the zoo while up tightens the camera and warms the materials.",
						},
						{
							"kicker": "Shared runtime",
							"title":  "The engine is part of the route, not a separate app.",
							"body":   "The same server-driven page owns the copy, metadata, links, and native 3D program endpoint.",
						},
					},
				}, nil
			},
		},
	)
}
