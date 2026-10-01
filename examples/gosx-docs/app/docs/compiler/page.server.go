package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Compiler", "How GoSX parses, validates, lowers, checks, and renders GSX source.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "light",
				"title":       "Compiler",
				"description": "How GoSX parses, validates, lowers, checks, and renders GSX source.",
				"tags":        []string{"compiler", "gsx", "strict", "ir", "tree-sitter"},
				"toc": []map[string]string{
					{"href": "#source-model", "label": "Source Model"},
					{"href": "#strict-validation", "label": "Strict Validation"},
					{"href": "#pipeline", "label": "Pipeline"},
					{"href": "#expressions", "label": "Expressions"},
					{"href": "#islands", "label": "Island Lowering"},
					{"href": "#commands", "label": "Commands"},
					{"href": "#lsp", "label": "LSP Boundary"},
				},
				"strictSample":   docsapp.DocSample("compiler/strictSample.gosx.sample"),
				"legacySample":   docsapp.DocSample("compiler/legacySample.gosx.sample"),
				"programSample":  docsapp.DocSample("compiler/programSample.go.sample"),
				"islandSample":   docsapp.DocSample("compiler/islandSample.gosx.sample"),
				"commandsSample": docsapp.DocSample("compiler/commandsSample.bash.sample"),
			}, nil
		},
	})
}
