package collections

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

type CollectionPropsData struct{ Initial string }

func init() {
	docsapp.RegisterDocsPage("Island Collections", "A GoSX collection editor built with Go append, typed signals, and the shared island VM.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode": "", "title": "Island Collections",
				"description":      "Grow and clear a slice through the shared GoSX island VM.",
				"tags":             []string{"islands", "signals", "collections"},
				"collectionProps":  CollectionPropsData{Initial: "GoSX"},
				"collectionSample": docsapp.DocSample("islands/collectionSample.gosx.sample"),
				"toc":              []map[string]string{{"href": "#collection-example", "label": "Working Collection"}, {"href": "#collection-source", "label": "Source"}},
			}, nil
		},
	})
}
