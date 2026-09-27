package docs

import (
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage("Hubs & CRDT", "WebSocket coordination, presence, fanout, and binary CRDT synchronization.", route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return map[string]any{
				"mode":        "",
				"title":       "Hubs & CRDT",
				"description": "WebSocket coordination, presence, fanout, and binary CRDT synchronization.",
				"tags":        []string{"hubs", "websocket", "presence", "crdt", "sync"},
				"toc": []map[string]string{
					{"href": "#hub-model", "label": "Hub Model"},
					{"href": "#events", "label": "Events"},
					{"href": "#delivery", "label": "Delivery"},
					{"href": "#documents", "label": "CRDT Documents"},
					{"href": "#sync", "label": "Sync"},
					{"href": "#security", "label": "Security"},
				},
				"hubSample":        docsapp.DocSample("hubs/hubSample.go.sample"),
				"lifecycleSample":  docsapp.DocSample("hubs/lifecycleSample.go.sample"),
				"crdtSample":       docsapp.DocSample("hubs/crdtSample.go.sample"),
				"manualSyncSample": docsapp.DocSample("hubs/manualSyncSample.go.sample"),
				"hubSyncSample":    docsapp.DocSample("hubs/hubSyncSample.go.sample"),
			}, nil
		},
	})
}
