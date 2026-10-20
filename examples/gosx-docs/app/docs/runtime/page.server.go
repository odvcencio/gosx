package docs

import (
	"m31labs.dev/gosx"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Runtime",
		"Managed navigation, fixed-target transfers, script roles, prefetch, telemetry, and page disposal.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					// This allowlist is display text, not an export asset reference.
					"navigationAssetPattern": gosx.RawHTML("&#47;gosx&#47;assets&#47;runtime&#47;navigation.*.js"),

					"sample001":   docsapp.DocSample("runtime/code-001.gosx.sample"),
					"sample002":   docsapp.DocSample("runtime/code-002.go.sample"),
					"sample003":   docsapp.DocSample("runtime/code-003.js.sample"),
					"sample004":   docsapp.DocSample("runtime/code-004.gosx.sample"),
					"sample005":   docsapp.DocSample("runtime/code-005.gosx.sample"),
					"sample006":   docsapp.DocSample("runtime/code-006.go.sample"),
					"sample007":   docsapp.DocSample("runtime/code-007.go.sample"),
					"sample008":   docsapp.DocSample("runtime/code-008.gosx.sample"),
					"sample009":   docsapp.DocSample("runtime/code-009.go.sample"),
					"sample010":   docsapp.DocSample("runtime/code-010.gosx.sample"),
					"sample011":   docsapp.DocSample("runtime/code-011.gosx.sample"),
					"sample012":   docsapp.DocSample("runtime/code-012.gosx.sample"),
					"sample013":   docsapp.DocSample("runtime/code-013.gosx.sample"),
					"sample014":   docsapp.DocSample("runtime/code-014.gosx.sample"),
					"sample015":   docsapp.DocSample("runtime/code-015.gosx.sample"),
					"sample016":   docsapp.DocSample("runtime/code-016.gosx.sample"),
					"sample017":   docsapp.DocSample("runtime/code-017.gosx.sample"),
					"sample018":   docsapp.DocSample("runtime/code-018.gosx.sample"),
					"sample019":   docsapp.DocSample("runtime/code-019.go.sample"),
					"sample020":   docsapp.DocSample("runtime/code-020.gosx.sample"),
					"sample021":   docsapp.DocSample("runtime/code-021.gosx.sample"),
					"sample022":   docsapp.DocSample("runtime/code-022.gosx.sample"),
					"sample023":   docsapp.DocSample("runtime/code-023.go.sample"),
					"sample024":   docsapp.DocSample("runtime/code-024.gosx.sample"),
					"sample025":   docsapp.DocSample("runtime/code-025.go.sample"),
					"sample026":   docsapp.DocSample("runtime/code-026.go.sample"),
					"sample027":   docsapp.DocSample("runtime/code-027.gosx.sample"),
					"sample028":   docsapp.DocSample("runtime/code-028.html.sample"),
					"sample029":   docsapp.DocSample("runtime/code-029.js.sample"),
					"mode":        "light",
					"title":       "Runtime",
					"description": "Managed navigation, fixed-target transfers, script roles, prefetch, telemetry, and page disposal.",
					"tags":        []string{"navigation", "transitions", "lifecycle", "prefetch", "revalidation", "heartbeat", "presence", "countdown", "reorder", "fixed-target transfer", "drag and drop", "filter", "search", "live regions", "text binding", "fragment refresh", "telemetry", "observability"},
					"toc": []map[string]string{
						{"href": "#client-navigation", "label": "Client navigation"},
						{"href": "#page-transitions", "label": "Transition ownership"},
						{"href": "#periodic-revalidation", "label": "Periodic revalidation"},
						{"href": "#visibility-heartbeat", "label": "Visibility-aware heartbeat"},
						{"href": "#declarative-countdown", "label": "Declarative countdown"},
						{"href": "#declarative-reorder", "label": "Declarative reorder"},
						{"href": "#declarative-transfer", "label": "Declarative fixed-target transfer"},
						{"href": "#live-bound-regions", "label": "Live-bound regions"},
						{"href": "#declarative-filter", "label": "Declarative list filter"},
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
