package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Typed Component Proof",
		"A production-rendered GoSX route authored with the v0.39 strict typed component syntax.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"title":       "Typed Component Proof",
					"description": "A production-rendered route authored with the v0.39 strict typed component syntax.",
					"tags":        []string{"strict", "typed", "tsx", "dogfood"},
					"toc":         []map[string]string{},
				}, nil
			},
		},
	)
}
