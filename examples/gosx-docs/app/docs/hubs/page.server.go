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
				"hubSample":        "room := hub.New(\"chat\")\nroom.MaxClients = 200\nroom.Latch(\"room-state\")\n\nroom.On(\"message\", func(ctx *hub.Context) {\n\tvar payload map[string]string\n\tif err := json.Unmarshal(ctx.Data, &payload); err != nil {\n\t\treturn\n\t}\n\tctx.Hub.Broadcast(\"message\", map[string]any{\n\t\t\"clientId\": ctx.Client.ID,\n\t\t\"text\":     payload[\"text\"],\n\t})\n})\n\nmux.Handle(\"/ws/chat\", room)",
				"lifecycleSample":  "room.On(\"join\", func(ctx *hub.Context) {\n\tctx.Hub.Broadcast(\"presence\", ctx.Hub.Presence().List())\n})\nroom.On(\"leave\", func(ctx *hub.Context) {\n\tctx.Hub.Broadcast(\"presence\", ctx.Hub.Presence().List())\n})",
				"crdtSample":       "doc := crdt.NewDoc()\nif err := doc.Put(crdt.Root, \"title\", crdt.StringValue(\"Draft\")); err != nil {\n\treturn err\n}\nif _, err := doc.Commit(\"set title\"); err != nil {\n\treturn err\n}\n\nvalue, _, err := doc.Get(crdt.Root, \"title\")\nif err != nil {\n\treturn err\n}\nfmt.Println(value.Str)",
				"manualSyncSample": "leftState := crdtsync.NewState()\nrightState := crdtsync.NewState()\n\nif msg, ok := left.GenerateSyncMessage(leftState); ok {\n\tif err := right.ReceiveSyncMessage(rightState, msg); err != nil {\n\t\treturn err\n\t}\n}",
				"hubSyncSample":    "room.SyncDoc(\"workspace\", doc)\nroom.SetBinaryAuthorizer(func(client *hub.Client, name string) bool {\n\trole, ok := client.Metadata(\"role\")\n\treturn ok && name == \"workspace\" && role == \"editor\"\n})",
			}, nil
		},
	})
}
