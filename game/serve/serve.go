//go:build !js || !wasm

// Package serve holds the server-side helpers of package game: driving a
// game.Runtime from a sim.Runner over a hub, and mounting a runtime's engine
// in a server-rendered page.
//
// They live here, not in game, so a browser (js/wasm) build of game links no
// hub, net/http, crypto/tls or GoSX compiler. Import serve from server code
// only.
package serve

import (
	"math"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/sim"
)

// game.Runtime is a sim.Simulation, and Runner.RestoreSnapshot sees why it
// refuses a snapshot.
var (
	_ sim.Simulation      = (*game.Runtime)(nil)
	_ sim.CheckedRestorer = (*game.Runtime)(nil)
)

// NewRunner wires runtime into the server-authoritative sim runner. A nil
// runtime gets game.New(game.Config{}). A zero opts.TickRate runs one tick per
// fixed step of the runtime.
func NewRunner(h *hub.Hub, runtime *game.Runtime, opts sim.Options) *sim.Runner {
	if runtime == nil {
		runtime = game.New(game.Config{})
	}
	if opts.TickRate <= 0 {
		opts.TickRate = int(math.Round(float64(time.Second) / float64(runtime.FixedStep())))
		if opts.TickRate <= 0 {
			opts.TickRate = 60
		}
	}
	return sim.New(h, runtime, opts)
}

// EngineMount is implemented by server.PageState and server.PageRuntime.
type EngineMount interface {
	Engine(engine.Config, gosx.Node) gosx.Node
}

// Mount registers the runtime's engine config with a page or runtime mount
// target and returns the node to render. A nil target returns fallback.
func Mount(runtime *game.Runtime, target EngineMount, fallback gosx.Node) gosx.Node {
	if target == nil {
		return fallback
	}
	return target.Engine(runtime.EngineConfig(), fallback)
}
