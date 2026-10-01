package docs

import (
	"m31labs.dev/gosx"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/textlayout"
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
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			var textExample gosx.Node = gosx.Text("")
			if ctx != nil {
				textExample = ctx.Runtime().TextBlock(server.TextBlockProps{
					Tag: "p", Text: "A Go-authored line plan stays readable before browser measurement.",
					Font: "400 16px Inter", Lang: "en", MaxWidth: 420, LineHeight: 24,
					MaxLines: 2, Overflow: textlayout.OverflowEllipsis,
				})
			}
			return route.FileTemplateBindings{Values: map[string]any{"textLayoutExample": textExample}}
		},
	})
}
