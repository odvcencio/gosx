package docs

import (
	"time"

	"github.com/odvcencio/gosx"
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func init() {
	docsapp.RegisterDocsPage(
		"Streaming",
		"Deferred regions flush fallback HTML first, then stream the resolved node into place.",
		route.FileModuleOptions{
			Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
				region := gosx.Text("")
				if ctx != nil {
					region = ctx.DeferWithOptions(server.DeferredOptions{
						Class: "card",
					}, gosx.El("div",
						gosx.El("strong", gosx.Text("Loading region")),
						gosx.El("p", gosx.Text("The server has already flushed this fallback while the deferred resolver finishes.")),
					), func() (gosx.Node, error) {
						time.Sleep(180 * time.Millisecond)
						return gosx.El("div", gosx.Attrs(gosx.Attr("class", "card")),
							gosx.El("strong", gosx.Text("Resolved region")),
							gosx.El("p", gosx.Text("This card streamed after the initial HTML shell and replaced the fallback slot in-place.")),
						), nil
					})
				}
				return route.FileTemplateBindings{
					Values: map[string]any{
						"streamRegion": region,
					},
				}
			},
		},
	)
}
