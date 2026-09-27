package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Text Layout", "Approximate server text layout with optional browser metric refinement.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Text Layout",
				"description": "Approximate server text layout with optional browser metric refinement.",
				"tags":        []string{"textblock", "line-breaking", "measurement", "bootstrap"},
				"toc": []map[string]string{
					{"href": "#textblock", "label": "TextBlock"},
					{"href": "#modes", "label": "Modes"},
					{"href": "#measurement", "label": "Measurement"},
					{"href": "#constraints", "label": "Constraints"},
					{"href": "#whitespace", "label": "Whitespace"},
					{"href": "#low-level", "label": "Low-level API"},
				},
				"blockSample":    docsapp.DocSample("text-layout/blockSample.go.sample"),
				"nativeSample":   docsapp.DocSample("text-layout/nativeSample.go.sample"),
				"lowLevelSample": docsapp.DocSample("text-layout/lowLevelSample.go.sample"),
			}, nil
		},
	})
}
