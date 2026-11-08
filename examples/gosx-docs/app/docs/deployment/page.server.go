package docs

import (
	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Deployment", "Build, export, and operate the staged GoSX deployment bundle.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return docs.LoadMarkdownDoc("deployment")
		},
	})
}
