package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Signals", "Fine-grained reactive state with dependency tracking, batching, and computed values.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "",
				"title":       "Signals",
				"description": "Fine-grained reactive state with dependency tracking, batching, and computed values.",
				"tags":        []string{"signals", "reactivity", "computed", "effects", "batch"},
				"toc": []map[string]string{
					{"href": "#signal-basics", "label": "Signal Basics"},
					{"href": "#computed-values", "label": "Computed Values"},
					{"href": "#effects", "label": "Effects"},
					{"href": "#dependency-tracking", "label": "Dependency Tracking"},
					{"href": "#batch-updates", "label": "Batch Updates"},
					{"href": "#signal-store", "label": "Signal Store"},
				},
			}, nil
		},
	})
}
