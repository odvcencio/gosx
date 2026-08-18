package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Runtime",
		"Managed navigation, script roles, prefetch, telemetry, and page disposal.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"mode":        "light",
					"title":       "Runtime",
					"description": "Managed navigation, script roles, prefetch, telemetry, and page disposal.",
					"tags":        []string{"navigation", "transitions", "lifecycle", "prefetch", "revalidation", "countdown", "reorder", "drag and drop", "live regions", "text binding", "fragment refresh", "telemetry", "observability"},
					"toc": []map[string]string{
						{"href": "#client-navigation", "label": "Client navigation"},
						{"href": "#page-transitions", "label": "Transition ownership"},
						{"href": "#periodic-revalidation", "label": "Periodic revalidation"},
						{"href": "#declarative-countdown", "label": "Declarative countdown"},
						{"href": "#declarative-reorder", "label": "Declarative reorder"},
						{"href": "#live-bound-regions", "label": "Live-bound regions"},
						{"href": "#lifecycle-scripts", "label": "Managed scripts"},
						{"href": "#prefetch", "label": "Prefetch"},
						{"href": "#runtime-telemetry", "label": "Telemetry"},
						{"href": "#disposal", "label": "Disposal"},
					},
				}, nil
			},
		},
	)
}
