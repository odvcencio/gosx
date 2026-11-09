//go:build js && wasm

// Command replaybrowser is the page program for the replay viewer e2e test.
// It records a short match of the roomtest toy game, builds a Player, a camera
// Rig and the replayui Controller and Bar, and exposes window.replay so the
// test can step frames and read state. It holds no game logic of its own.
package main

import (
	"encoding/json"
	"strconv"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/camera"
	"m31labs.dev/gosx/game/kitui"
	"m31labs.dev/gosx/game/replay"
	"m31labs.dev/gosx/game/replay/replayui"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
	"m31labs.dev/gosx/sim/simcore"
)

const (
	ticks = 900
	hz    = 60
)

type snapshot struct {
	Tick, Start, End uint64
	Hash, Live       string // state hash now, and the hash live play had at this tick
	Match            bool
	Paused           bool
	Speed            float64
	Camera           string
	Resim            int
	Markers          int
	Bytes            int
	Pose             [3]float64
}

func hex(h uint64) string { return strconv.FormatUint(h, 16) }

func main() {
	g := js.Global()
	doc := g.Get("document")

	// Live play, recorded.
	live := roomtest.NewGame(3, 6, 11, hz)
	rec := replay.NewRecorder(live, replay.Config{Interval: 60, TickRate: hz})
	liveHash := []uint64{replay.Hash(live)}
	for i := 0; i < ticks; i++ {
		rec.Tick(map[string]simcore.Input{
			mode.PlayerID(i % 3): {Data: []byte{byte((i / 7) % 32)}},
		})
		liveHash = append(liveHash, replay.Hash(live))
		if i == 299 {
			rec.Mark("opening")
		}
		if i == 599 {
			rec.Mark("late")
		}
	}

	// Replay on a fresh game.
	game := roomtest.NewGame(3, 6, 11, hz)
	player, err := replay.NewPlayer(game, rec.Recording())
	if err != nil {
		g.Set("wasmError", err.Error())
		return
	}
	rig := replay.NewRig(replay.RigConfig{
		Subject: func(float64) (camera.Vec3, bool) {
			b := game.World.Bodies[0]
			return camera.Vec3{X: float64(b.X) / roomtest.Unit, Z: float64(b.Y) / roomtest.Unit}, true
		},
		Shots: map[string]camera.Pose{"high": {Position: camera.Vec3{Y: 40, Z: 1}}},
		Near:  0.1, Far: 500,
	})
	in := actions.NewMap()
	in.Define(replayui.Context())
	in.Push(replayui.ContextName)
	actions.Attach(in, actions.BrowserConfig{})
	var ctl *replayui.Controller
	ctl = replayui.New(replayui.Config{Player: player, Rig: rig, Input: in})

	bar := doc.Call("getElementById", "bar")
	lastKey := ""
	render := func(force bool) {
		v := ctl.View()
		key := strconv.FormatBool(v.Paused) + strconv.FormatFloat(v.Speed, 'f', -1, 64) + ctl.ActiveCamera()
		if force || key != lastKey {
			lastKey = key
			kitui.Render(bar, replayui.Bar(v))
			return
		}
		// Same controls: update the position without rebuilding the DOM, so a
		// drag on the scrubber and keyboard focus survive.
		if s := doc.Call("getElementById", "kit-replay-scrub"); s.Truthy() && !s.Equal(doc.Get("activeElement")) {
			s.Set("value", strconv.FormatUint(v.Tick, 10))
		}
	}
	render(true)
	kitui.Bind(doc.Get("body"), kitui.Handlers{
		OnAction:  func(name, value string) { ctl.Handle(name, value) },
		OnSetting: func(k, v string) { ctl.HandleSetting(k, v) },
	})

	state := func() string {
		n := player.Tick()
		p := rig.Pose()
		s := snapshot{
			Tick: n, Start: player.Start(), End: player.End(),
			Hash: hex(replay.Hash(game)), Live: hex(liveHash[n]),
			Paused: player.Paused(), Speed: player.Speed(), Camera: ctl.ActiveCamera(),
			Resim: player.LastResim(), Markers: len(player.Recording().Markers()), Bytes: player.Recording().Bytes(),
			Pose: [3]float64{p.Position.X, p.Position.Y, p.Position.Z},
		}
		s.Match = s.Hash == s.Live
		out, _ := json.Marshal(s)
		return string(out)
	}

	api := g.Get("Object").New()
	api.Set("frame", js.FuncOf(func(_ js.Value, args []js.Value) any {
		dt := time.Duration(args[0].Float() * float64(time.Millisecond))
		in.Update(dt)
		ctl.Update(dt)
		render(false)
		return state()
	}))
	api.Set("state", js.FuncOf(func(js.Value, []js.Value) any { return state() }))
	// seekBench seeks to n pseudo-random ticks and returns the mean milliseconds
	// per seek, measured with performance.now in the page.
	api.Set("seekBench", js.FuncOf(func(_ js.Value, args []js.Value) any {
		n := args[0].Int()
		perf := g.Get("performance")
		x := uint64(88172645463325252)
		t0 := perf.Call("now").Float()
		for i := 0; i < n; i++ {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
			_ = player.Seek(x % (ticks + 1))
		}
		return (perf.Call("now").Float() - t0) / float64(n)
	}))
	g.Set("replay", api)
	select {}
}
