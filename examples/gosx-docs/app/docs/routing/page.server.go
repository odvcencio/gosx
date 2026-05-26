package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Routing", "File-based routing with nested layouts, dynamic params, and server-side data loading.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Routing",
				"description": "File-based routing with nested layouts, dynamic params, and server-side data loading.",
				"tags":        []string{"routes", "layouts", "params", "navigation"},
				"toc": []map[string]string{
					{"href": "#file-routes", "label": "File Routes"},
					{"href": "#dynamic-params", "label": "Dynamic Params"},
					{"href": "#layouts", "label": "Layouts"},
					{"href": "#data-loading", "label": "Data Loading"},
					{"href": "#redirects-and-rewrites", "label": "Redirects & Rewrites"},
					{"href": "#client-navigation", "label": "Client Navigation"},
				},
			}, nil
		},
	})
}
