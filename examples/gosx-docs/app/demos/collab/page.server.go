package collab

import (
	"encoding/json"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/route"
)

// Hub is exported for main.go to mount at /demos/collab/ws.
var Hub *hub.Hub
var doc *Doc

// roster tracks connected editors' stable identities and last known cursor
// positions for the presence + remote-cursor features. It is this demo's
// own bookkeeping — separate from hub.Hub's built-in Presence tracker.
var presenceRoster *roster

func init() {
	Hub = hub.New("collab")
	doc = NewDoc(defaultDocText)
	presenceRoster = newRoster()

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

	// cursor:update — client reports its caret/selection as a character
	// offset into the shared text. Relayed to ALL clients (including the
	// sender, which filters out its own ID) tagged with the sender's stable
	// identity so peers can render a labeled, colored caret marker.
	Hub.On("cursor:update", func(ctx *hub.Context) {
		var payload struct {
			Offset int `json:"offset"`
			SelEnd int `json:"selEnd"`
		}
		if err := json.Unmarshal(ctx.Data, &payload); err != nil {
			return
		}
		textLen := len(doc.State().Text)
		offset := clampInt(payload.Offset, 0, textLen)
		selEnd := clampInt(payload.SelEnd, 0, textLen)
		evt, ok := presenceRoster.updateCursor(ctx.Client.ID, offset, selEnd)
		if !ok {
			// Race with disconnect — client left before this arrived. Drop it.
			return
		}
		ctx.Hub.Broadcast("cursor:update", evt)
	})

	// join — fires automatically when a client connects (hub wires this).
	// Send the new client the current document state so they start in sync,
	// assign it a stable identity, hand it any already-connected peers'
	// cursors, and tell everyone (including this client) the new count.
	Hub.On("join", func(ctx *hub.Context) {
		state := doc.State()
		ctx.Hub.Send(ctx.Client.ID, "doc:update", state)

		identity := presenceRoster.join(ctx.Client.ID)
		ctx.Hub.Send(ctx.Client.ID, "presence:self", struct {
			ID string `json:"id"`
			Identity
		}{ID: ctx.Client.ID, Identity: identity})

		if cursors := presenceRoster.snapshot(); len(cursors) > 0 {
			ctx.Hub.Send(ctx.Client.ID, "cursor:roster", cursors)
		}

		ctx.Hub.Broadcast("presence:count", PresenceEvent{Count: presenceRoster.count()})
	})

	// leave — fires automatically when a client disconnects. Forget its
	// identity/cursor, tell peers to drop its caret marker immediately, and
	// broadcast the updated connected-editor count.
	Hub.On("leave", func(ctx *hub.Context) {
		presenceRoster.leave(ctx.Client.ID)
		ctx.Hub.Broadcast("cursor:leave", CursorLeaveEvent{ID: ctx.Client.ID})
		ctx.Hub.Broadcast("presence:count", PresenceEvent{Count: presenceRoster.count()})
	})

	docsapp.RegisterStaticDocsPage(
		"Collab Editor",
		"A deliberately simple last-write-wins document synchronized over one GoSX Hub.",
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
