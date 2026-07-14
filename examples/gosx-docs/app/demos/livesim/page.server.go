package livesim

import (
	"encoding/json"
	"hash/fnv"
	"time"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/examples/gosx-docs/app/demos/democtl"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/sim"
)

// Hub is the package-level hub that main.go mounts at /demos/livesim/ws.
var Hub *hub.Hub

// runner is the background sim runner. Leaked intentionally for this demo.
var runner *sim.Runner

// theGame is the package-level simulation instance. Kept as a var (rather
// than a local inside init) so the presence-viewer poller and the
// "presence:move" handler below can reach it without threading extra state
// through sim.Runner.
var theGame *game

// viewerPollInterval controls how often the connected-client count is
// re-sampled into theGame for the next broadcast. sim.Runner owns the single
// "join" hub handler (it sends the initial snapshot to new clients), so a
// short poll — rather than a second join hook — is how this demo keeps the
// "N viewers" HUD figure honest without touching sim.Runner.
const viewerPollInterval = 300 * time.Millisecond

func init() {
	Hub = hub.New("livesim")
	theGame = newGame()
	runner = sim.New(Hub, theGame, sim.Options{TickRate: 20})
	runner.RegisterHandlers()
	runner.Start()

	// presence:move — a client reports its pointer position; the hub
	// rebroadcasts it (tagged with a deterministic name/color derived from
	// the connection's client ID) so every other tab can render a ghost
	// cursor. Cosmetic only: never fed into the physics tick.
	Hub.On("presence:move", func(ctx *hub.Context) {
		var pos struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		}
		if err := json.Unmarshal(ctx.Data, &pos); err != nil {
			return
		}
		id := identityFor(ctx.Client.ID)
		ctx.Hub.Broadcast("presence:cursor", map[string]any{
			"id":    ctx.Client.ID,
			"x":     pos.X,
			"y":     pos.Y,
			"name":  id.Name,
			"color": id.Color,
		})
	})

	// leave — tell every remaining client to drop that ghost cursor
	// immediately, and refresh the viewer count without waiting on the poll
	// below.
	Hub.On("leave", func(ctx *hub.Context) {
		theGame.SetViewers(ctx.Hub.ClientCount())
		ctx.Hub.Broadcast("presence:leave", map[string]any{"id": ctx.Client.ID})
	})

	// sim.Runner already owns the "join" hub event (it sends the initial
	// sim:snapshot), so new-connection viewer-count updates come from this
	// poller instead of a second join hook. Leaked intentionally, same as
	// runner above — this is a package-lifetime background loop for a demo
	// process, not a per-request resource.
	go func() {
		ticker := time.NewTicker(viewerPollInterval)
		defer ticker.Stop()
		for range ticker.C {
			theGame.SetViewers(Hub.ClientCount())
		}
	}()

	docsapp.RegisterStaticDocsPage(
		"Live Sim",
		"Server-authoritative 2D physics sandbox streamed over a hub.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				ctx.Runtime().BindHub("livesim", "/demos/livesim/ws", []hydrate.HubBinding{})
				return map[string]any{
					"worldW":          worldWidth,
					"worldH":          worldHeight,
					"maxCircles":      maxCircles,
					"burstMinCircles": burstMinCircles,
					"burstMaxCircles": burstMaxCircles,
				}, nil
			},
		},
	)
}

// identityFor deterministically maps a hub client ID to a democtl Identity
// (name + accessible accent color) so the same connection always renders
// the same ghost-cursor label without any server-side session storage.
// democtl.Pick/PickChecked take a *rand.Rand seeded per call, which doesn't
// fit "same client ID always yields the same identity for the life of the
// connection" — so this hashes the client ID directly into democtl's
// exported name/color pools instead.
func identityFor(clientID string) democtl.Identity {
	names := democtl.NamePool()
	colors := democtl.ColorPool()
	if len(names) == 0 || len(colors) == 0 {
		return democtl.Identity{}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(clientID))
	sum := h.Sum32()
	return democtl.Identity{
		Name:  names[sum%uint32(len(names))],
		Color: colors[(sum/uint32(len(names)))%uint32(len(colors))],
	}
}
