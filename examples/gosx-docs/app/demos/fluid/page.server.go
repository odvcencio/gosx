package fluid

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/hub"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/route"
)

// Hub is the package-level hub that main.go mounts at /demos/fluid/ws.
var Hub *hub.Hub

// sim is the background field simulation. Leaked intentionally for this demo.
var sim *Sim

func init() {
	Hub = hub.New("fluid")
	sim = NewSim(Hub)
	sim.Start()

	docsapp.RegisterStaticDocsPage(
		"Fluid",
		"Server advects a 3D velocity field and streams quantized deltas to the browser.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				ctx.Runtime().BindHub("fluid", "/demos/fluid/ws", []hydrate.HubBinding{})
				return map[string]any{
					"worldW":   800,
					"worldH":   500,
					"bitWidth": bitWidth,
					"gridN":    gridN,
				}, nil
			},
		},
	)
}
