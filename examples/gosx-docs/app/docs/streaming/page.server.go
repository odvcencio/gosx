package docs

import (
	"time"

	"github.com/odvcencio/gosx"
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
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
					{"href": "#streaming-response", "label": "Streaming Response"},
					{"href": "#use-cases", "label": "Use Cases"},
				},
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
