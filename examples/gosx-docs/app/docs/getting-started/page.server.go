package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Getting Started", "GoSX is a Go framework for server-rendered pages, interactive islands, realtime hubs, and typed 3D scenes.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Getting Started",
				"description": "GoSX is a Go framework for server-rendered pages, interactive islands, realtime hubs, and typed 3D scenes.",
				"tags":        []string{"quickstart", "init", "Go 1.26+"},
				"toc": []map[string]string{
					{"href": "#quickstart-heading", "label": "Start"},
					{"href": "#prerequisites", "label": "Prerequisites"},
					{"href": "#timing", "label": "Measured time"},
					{"href": "#troubleshooting", "label": "Troubleshooting"},
					{"href": "#project-files", "label": "Project files"},
				},
			}, nil
		},
	})
}
