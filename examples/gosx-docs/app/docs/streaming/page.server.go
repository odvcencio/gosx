package docs

import (
	"time"

	"m31labs.dev/gosx"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	docsapp.RegisterDocsPage("Streaming", "Deferred regions with visible fallbacks and streamed content replacement.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Streaming",
				"description": "Deferred regions with visible fallbacks and streamed content replacement.",
				"tags":        []string{"streaming", "deferred", "ssr", "progressive"},
				"toc": []map[string]string{
					{"href": "#deferred-regions", "label": "Deferred Regions"},
					{"href": "#fallback-content", "label": "Fallback Content"},
					{"href": "#completion-order", "label": "Completion Order"},
					{"href": "#errors-and-csp", "label": "Errors & CSP"},
					{"href": "#caching", "label": "Caching"},
				},
				"deferSample":   "return gosx.El(\"main\",\n\tctx.Defer(\n\t\tgosx.El(\"p\", gosx.Text(\"Loading activity...\")),\n\t\tfunc() (gosx.Node, error) {\n\t\t\titems, err := loadActivity(ctx.Request.Context())\n\t\t\tif err != nil {\n\t\t\t\treturn nil, err\n\t\t\t}\n\t\t\treturn ActivityList(items), nil\n\t\t},\n\t),\n)",
				"optionsSample": "ctx.SuspenseWithOptions(server.DeferredOptions{\n\tID:       \"account-summary\",\n\tTag:      \"section\",\n\tClass:    \"summary-shell\",\n\tBoundary: \"component\",\n}, fallback, resolve)",
			}, nil
		},
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			var demoRegion gosx.Node = gosx.Text("")
			if ctx != nil {
				demoRegion = ctx.DeferWithOptions(server.DeferredOptions{
					Class: "streaming-demo-region",
				}, gosx.El("div", gosx.Attrs(gosx.Attr("class", "demo-well")),
					gosx.El("p", gosx.Attrs(gosx.Attr("class", "streaming-fallback")),
						gosx.Text("Fallback: the shell flushed immediately. This region is resolving..."),
					),
				), func() (gosx.Node, error) {
					time.Sleep(200 * time.Millisecond)
					return gosx.El("div", gosx.Attrs(gosx.Attr("class", "demo-well")),
						gosx.El("strong", gosx.Text("Resolved: ")),
						gosx.Text("this card streamed in after the initial HTML shell."),
					), nil
				})
			}
			return route.FileTemplateBindings{
				Values: map[string]any{
					"streamDemo": demoRegion,
				},
			}
		},
	})
}
