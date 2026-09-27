package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Your first GoSX app", "Build one GoSX app in four steps with server data, an island, a hub, and a typed Scene3D.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Your first GoSX app",
				"description": "Build one GoSX app in four steps with server data, an island, a hub, and a typed Scene3D.",
				"tags":        []string{"tutorial", "four steps", "about 5 minutes"},
				"toc": []map[string]string{
					{"href": "#step-server-data", "label": "Server data"},
					{"href": "#step-island", "label": "Island"},
					{"href": "#step-hub", "label": "Hub"},
					{"href": "#step-scene3d", "label": "Scene3D"},
				},
			}, nil
		},
	})
}
