package collab

import (
	"encoding/json"

	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/hub"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/route"
)

// Hub is exported for main.go to mount at /demos/collab/ws.
var Hub *hub.Hub
var doc *Doc

func init() {
	Hub = hub.New("collab")
	doc = NewDoc(defaultDocText)

	// doc:edit — client sends its current text + version; server applies LWW.
	Hub.On("doc:edit", func(ctx *hub.Context) {
		var payload struct {
			Text    string `json:"text"`
			Version uint64 `json:"version"`
		}
		if err := json.Unmarshal(ctx.Data, &payload); err != nil {
			// Malformed message — ignore; client will resync on next broadcast.
			return
		}
		state, _ := doc.Apply(payload.Text, payload.Version)
		// Broadcast resulting state to ALL clients (including the sender).
		// If the edit was rejected (stale), the sender gets the current state
		// and will re-sync automatically.
		ctx.Hub.Broadcast("doc:update", state)
	})

	// join — fires automatically when a client connects (hub wires this).
	// Send the new client the current document state so they start in sync.
	Hub.On("join", func(ctx *hub.Context) {
		state := doc.State()
		ctx.Hub.Send(ctx.Client.ID, "doc:update", state)
	})

	docsapp.RegisterStaticDocsPage(
		"Collab Editor",
		"LWW collaborative markdown editor over a single hub.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				ctx.Runtime().BindHub("collab", "/demos/collab/ws", []hydrate.HubBinding{})
				state := doc.State()
				return map[string]any{
					"initialText":    state.Text,
					"initialVersion": state.Version,
				}, nil
			},
		},
	)
}
