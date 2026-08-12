package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Routing", "File routes, layouts, dynamic parameters, loader modules, actions, and route configuration.", route.FileModuleOptions{
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
				"treeSample":   "app/\n├── layout.gsx\n├── page.gsx\n├── blog/\n│   ├── page.gsx\n│   └── [slug]/\n│       ├── page.gsx\n│       └── page.server.go\n└── docs/\n    └── [...path]/\n        └── page.gsx",
				"moduleSample": "package blog\n\nimport (\n\t\"log\"\n\n\t\"m31labs.dev/gosx/route\"\n)\n\nfunc init() {\n\tif err := route.RegisterFileModuleHere(route.FileModuleOptions{\n\t\tLoad: func(ctx *route.RouteContext, page route.FilePage) (any, error) {\n\t\t\treturn map[string]any{\n\t\t\t\t\"slug\":  ctx.Param(\"slug\"),\n\t\t\t\t\"query\": ctx.Query(\"q\"),\n\t\t\t}, nil\n\t\t},\n\t}); err != nil {\n\t\tlog.Fatal(err)\n\t}\n}",
				"pageSample":   "package blog\n\nfunc Page() Node {\n\treturn <article>\n\t\t<h1>{data.slug}</h1>\n\t\t<a href=\"/blog\" data-gosx-link=\"true\">All posts</a>\n\t</article>\n}",
				"configSample": "{\n  \"prerender\": false,\n  \"headers\": {\n    \"X-Content-Type-Options\": \"nosniff\"\n  },\n  \"cache\": {\n    \"public\": true,\n    \"maxAge\": \"1m\",\n    \"staleWhileRevalidate\": \"5m\"\n  },\n  \"cacheTags\": [\"docs\"]\n}",
			}, nil
		},
	})
}
