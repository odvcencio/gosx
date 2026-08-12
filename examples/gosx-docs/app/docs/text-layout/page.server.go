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
				"blockSample":    "node := ctx.TextBlock(server.TextBlockProps{\n\tTag:        \"p\",\n\tText:       article.Summary,\n\tFont:       \"400 16px Inter\",\n\tLang:       \"en\",\n\tMaxWidth:   520,\n\tLineHeight: 24,\n\tMaxLines:   3,\n\tOverflow:   textlayout.OverflowEllipsis,\n})",
				"nativeSample":   "node := server.TextBlock(server.TextBlockProps{\n\tMode:       server.TextBlockModeNative,\n\tTag:        \"p\",\n\tText:       article.Summary,\n\tFont:       \"400 16px Inter\",\n\tMaxWidth:   520,\n\tLineHeight: 24,\n\tMaxLines:   3,\n\tOverflow:   textlayout.OverflowEllipsis,\n})",
				"lowLevelSample": "prepared := textlayout.Prepare(text, textlayout.PrepareOptions{\n\tWhiteSpace: textlayout.WhiteSpacePreWrap,\n\tTabSize:    4,\n})\nmeasured, err := textlayout.Measure(prepared, measurer, \"400 16px Inter\")\nif err != nil {\n\treturn err\n}\nresult := textlayout.Layout(measured, textlayout.LayoutOptions{\n\tMaxWidth:   520,\n\tLineHeight: 24,\n\tMaxLines:   3,\n\tOverflow:   textlayout.OverflowEllipsis,\n})",
			}, nil
		},
	})
}
