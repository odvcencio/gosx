package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Routing", "File routes, layouts, dynamic parameters, loader modules, actions, and route configuration.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Routing",
				"description": "File routes, layouts, dynamic parameters, loader modules, actions, and route configuration.",
				"tags":        []string{"routes", "layouts", "params", "loaders", "navigation"},
				"toc": []map[string]string{
					{"href": "#file-routes", "label": "File Routes"},
					{"href": "#params", "label": "Parameters"},
					{"href": "#layouts", "label": "Layouts"},
					{"href": "#modules", "label": "Server Modules"},
					{"href": "#configuration", "label": "Configuration"},
					{"href": "#navigation", "label": "Navigation"},
				},
				"treeSample":   docsapp.DocSample("routing/treeSample.text.sample"),
				"moduleSample": docsapp.DocSample("routing/moduleSample.go.sample"),
				"pageSample":   docsapp.DocSample("routing/pageSample.gosx.sample"),
				"configSample": docsapp.DocSample("routing/configSample.json.sample"),
			}, nil
		},
	})
}
