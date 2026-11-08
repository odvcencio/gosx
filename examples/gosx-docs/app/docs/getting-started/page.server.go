package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Getting Started", "Set up a GoSX project from scratch in under a minute.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return docs.LoadMarkdownDoc("getting-started")
		},
	})
}
