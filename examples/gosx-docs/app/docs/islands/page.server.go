package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Islands", "Reactive DOM regions powered by a Go expression VM with shared signals.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "",
				"title":       "Islands",
				"description": "Reactive DOM regions powered by a Go expression VM with shared signals.",
				"tags":        []string{"islands", "reactivity", "signals", "hydration"},
				"toc": []map[string]string{
					{"href": "#what-are-islands", "label": "What Are Islands"},
					{"href": "#island-programs", "label": "Island Programs"},
					{"href": "#expression-vm", "label": "Expression VM"},
					{"href": "#shared-signals", "label": "Shared Signals"},
					{"href": "#cross-island-sync", "label": "Cross-Island Sync"},
					{"href": "#hydration", "label": "Hydration"},
				},
			}, nil
		},
	})
}
