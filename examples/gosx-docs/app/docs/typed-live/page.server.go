package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Typed Component Proof",
		"A production-rendered GoSX route authored with a strict typed component.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"title":       "Typed Component Proof",
					"description": "A production-rendered route authored with a strict typed component.",
					"tags":        []string{"strict", "typed", "tsx", "dogfood"},
					"toc":         []map[string]string{},
				}, nil
			},
		},
	)
}
