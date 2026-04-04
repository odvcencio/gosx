package docs

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

func init() {
	docsapp.RegisterStaticDocsPage(
		"Hubs & CRDT",
		"Real-time WebSocket rooms with conflict-free replicated data types.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				return map[string]any{
					"mode":        "",
					"title":       "Hubs & CRDT",
					"description": "Real-time WebSocket rooms with conflict-free replicated data types.",
					"tags":        []string{"hubs", "websocket", "crdt", "real-time", "sync"},
					"toc": []map[string]string{
						{"href": "#hub-rooms", "label": "Hub Rooms"},
						{"href": "#websocket-protocol", "label": "WebSocket Protocol"},
						{"href": "#crdt-documents", "label": "CRDT Documents"},
						{"href": "#merge-sync", "label": "Merge & Sync"},
						{"href": "#patches-hooks", "label": "Patches & Hooks"},
						{"href": "#use-cases", "label": "Use Cases"},
					},
				}, nil
			},
		},
	)
}
