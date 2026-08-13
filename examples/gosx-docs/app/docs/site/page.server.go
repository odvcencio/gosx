package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"How This Site Works",
		"The GoSX routes, runtime surfaces, source, and deployment behind this documentation site.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"title":       "How This Site Works",
					"description": "The routes, runtime surfaces, source, and deployment behind this documentation site.",
					"tags":        []string{"dogfood", "architecture", "deployment", "operations"},
					"toc": []map[string]string{
						{"href": "#request-path", "label": "Request Path"},
						{"href": "#proof-map", "label": "Proof Map"},
						{"href": "#deployment", "label": "What Is Live"},
						{"href": "#limits", "label": "Deliberate Limits"},
					},
				}, nil
			},
		},
	)
}
