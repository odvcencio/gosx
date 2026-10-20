package docs

import "m31labs.dev/gosx/hub"

// ExampleHub backs the live presence count on the Hubs guide.
var ExampleHub = hub.New("docs-guide-presence")

func init() {
	ExampleHub.MaxClients = 64
	ExampleHub.RequireOrigin = true
	broadcastCount := func(ctx *hub.Context) {
		ctx.Hub.Broadcast("presence", ctx.Hub.Presence().Count())
	}
	ExampleHub.On("join", broadcastCount)
	ExampleHub.On("leave", broadcastCount)
}
