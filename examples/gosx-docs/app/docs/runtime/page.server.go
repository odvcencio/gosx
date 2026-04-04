package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Runtime",
		"Client-side page transitions, lifecycle scripts, and navigation control.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"mode":        "light",
					"title":       "Runtime",
					"description": "Client-side page transitions, lifecycle scripts, and navigation control.",
					"tags":        []string{"navigation", "transitions", "lifecycle", "prefetch"},
					"toc": []map[string]string{
						{"href": "#client-navigation", "label": "Client Navigation"},
						{"href": "#page-transitions", "label": "Page Transitions"},
						{"href": "#lifecycle-scripts", "label": "Lifecycle Scripts"},
						{"href": "#prefetch", "label": "Prefetch"},
						{"href": "#disposal", "label": "Disposal"},
					},
				}, nil
			},
		},
	)
}
