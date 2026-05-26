package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Text Layout", "Server-measured text with font-aware line breaking, ellipsis, and width constraints.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Text Layout",
				"description": "Server-measured text with font-aware line breaking, ellipsis, and width constraints.",
				"tags":        []string{"textblock", "typography", "measurement", "server-rendered"},
				"toc": []map[string]string{
					{"href": "#textblock", "label": "TextBlock"},
					{"href": "#font-metrics", "label": "Font Metrics"},
					{"href": "#width-constraints", "label": "Width Constraints"},
					{"href": "#ellipsis-clamping", "label": "Ellipsis & Clamping"},
					{"href": "#line-breaking", "label": "Line Breaking"},
					{"href": "#bootstrap-mode", "label": "Bootstrap Mode"},
				},
			}, nil
		},
	})
}
