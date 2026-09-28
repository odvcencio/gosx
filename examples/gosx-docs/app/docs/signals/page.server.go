package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

type LiveSignalsPropsData struct {
	Initial int
}

func init() {
	docsapp.RegisterDocsPage("Signals", "Go reactive values and the distinct signal subset compiled into browser islands.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":             "",
				"title":            "Signals",
				"description":      "Go reactive values and the distinct signal subset compiled into browser islands.",
				"tags":             []string{"signals", "derive", "watch", "batch", "islands"},
				"liveSignalsProps": LiveSignalsPropsData{Initial: 0},
				"toc": []map[string]string{
					{"href": "#go-signals", "label": "Go Signals"},
					{"href": "#equality", "label": "Equality"},
					{"href": "#derived", "label": "Derived Values"},
					{"href": "#effects", "label": "Effects"},
					{"href": "#batching", "label": "Batching"},
					{"href": "#island-signals", "label": "Island Signals"},
				},
				"basicSample":       docsapp.DocSample("signals/basicSample.go.sample"),
				"liveExampleSample": docsapp.DocSample("signals/liveExample.gosx.sample"),
				"equalSample":       docsapp.DocSample("signals/equalSample.go.sample"),
				"deriveSample":      docsapp.DocSample("signals/deriveSample.go.sample"),
				"watchSample":       docsapp.DocSample("signals/watchSample.go.sample"),
				"batchSample":       docsapp.DocSample("signals/batchSample.go.sample"),
				"islandSample":      docsapp.DocSample("signals/islandSample.gosx.sample"),
			}, nil
		},
	})
}
