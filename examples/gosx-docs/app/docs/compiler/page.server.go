package docs

import (
	docs "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docs.RegisterDocsPage("Compiler", "How GSX source compiles through tree-sitter parsing, IR lowering, and validation.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Compiler",
				"description": "How GSX source compiles through tree-sitter parsing, IR lowering, and validation.",
				"tags":        []string{"compiler", "gsx", "ir", "tree-sitter", "gotreesitter"},
				"toc": []map[string]string{
					{"href": "#gsx-syntax", "label": "GSX Syntax"},
					{"href": "#parsing", "label": "Parsing"},
					{"href": "#ir-lowering", "label": "IR Lowering"},
					{"href": "#validation", "label": "Validation"},
					{"href": "#expression-evaluation", "label": "Expression Evaluation"},
					{"href": "#island-compilation", "label": "Island Compilation"},
				},
			}, nil
		},
	})
}
