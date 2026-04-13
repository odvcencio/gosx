package livesim

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/hub"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/sim"
)

// Hub is the package-level hub that main.go mounts at /demos/livesim/ws.
var Hub *hub.Hub

// runner is the background sim runner. Leaked intentionally for this demo.
var runner *sim.Runner

func init() {
	Hub = hub.New("livesim")
	game := newGame()
	runner = sim.New(Hub, game, sim.Options{TickRate: 20})
	runner.RegisterHandlers()
	runner.Start()

	docsapp.RegisterStaticDocsPage(
		"Live Sim",
		"Server-authoritative 2D physics sandbox streamed over a hub.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				ctx.Runtime().BindHub("livesim", "/demos/livesim/ws", []hydrate.HubBinding{})
				return map[string]any{
					"worldW": worldWidth,
					"worldH": worldHeight,
				}, nil
			},
		},
	)
}
