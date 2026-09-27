package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterDocsPage("Islands", "Explicit interactive regions compiled for the shared GoSX browser VM.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "",
				"title":       "Islands",
				"description": "Explicit interactive regions compiled for the shared GoSX browser VM.",
				"tags":        []string{"islands", "signals", "handlers", "hydration", "vm"},
				"toc": []map[string]string{
					{"href": "#island-model", "label": "Island Model"},
					{"href": "#authoring", "label": "Authoring"},
					{"href": "#composition", "label": "Composition"},
					{"href": "#vm-subset", "label": "VM Subset"},
					{"href": "#shared-signals", "label": "Shared Signals"},
					{"href": "#program-assets", "label": "Program Assets"},
					{"href": "#choosing", "label": "Choosing Islands"},
				},
				"counterSample":     docsapp.DocSample("islands/counterSample.gosx.sample"),
				"compositionSample": docsapp.DocSample("islands/compositionSample.gosx.sample"),
				"sharedSample":      docsapp.DocSample("islands/sharedSample.gosx.sample"),
				"assetSample":       docsapp.DocSample("islands/assetSample.go.sample"),
			}, nil
		},
	})
}
