package kitui_test

import (
	"fmt"
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/boot"
	"m31labs.dev/gosx/game/camera"
	"m31labs.dev/gosx/game/flow"
	"m31labs.dev/gosx/game/kitui"
	"m31labs.dev/gosx/game/settings"
	"m31labs.dev/gosx/game/storage"
	"m31labs.dev/gosx/markup"
)

// Example wires the whole game kit: settings, bindings, loading gate, state
// flow, camera and the pause screen. The same code runs in the browser; there
// the frame function runs from the Scene3D frame callback.
func Example() {
	store := storage.NewDefault("mygame")
	opts := settings.NewManager(settings.Config{Store: store})
	opts.Load()
	in := actions.NewMap()
	in.Define(actions.Context{Name: "gameplay", Actions: []actions.ActionDef{
		{Name: "jump", Bindings: []actions.Binding{{Source: actions.Key("Space")}, {Source: actions.PadButton(0)}}},
		{Name: "pause", Bindings: actions.PauseBindings()}}})
	in.Define(actions.Context{Name: "menu", Opaque: true, Actions: []actions.ActionDef{
		{Name: "back", Bindings: actions.BackBindings()}}})
	in.Import(opts.Get().Bindings)
	rt := game.New(game.Config{})
	cam := camera.NewDirector(camera.DirectorConfig{ReducedMotion: opts.ReducedMotion, ShakeScale: opts.ShakeScale},
		&camera.Follow{Distance: 8, Pitch: 0.5, SmoothTime: 0.2})
	gate := boot.New(boot.Config{}, boot.Task{Name: "assets"})
	fl := flow.New(flow.Config{Pauser: rt, Input: in},
		flow.State{Name: flow.Boot}, flow.State{Name: flow.Play, Input: "gameplay", Pausable: true, PauseOnBlur: true},
		flow.State{Name: flow.Pause, Input: "menu", Pause: true})
	fl.Push(flow.Boot)
	frame := func(dt time.Duration) {
		in.Update(dt)
		gate.Update(dt)
		if gate.Ready() && fl.Is(flow.Boot) {
			fl.Replace(flow.Play)
		}
		fl.HandleInput(in, flow.Controls{}) // Escape or gamepad Start pauses and resumes
		cam.Update(dt.Seconds())
	}
	gate.Complete("assets")
	frame(16 * time.Millisecond)
	in.Feed(actions.Event{Device: actions.PadDevice(0), Source: actions.PadButton(actions.PadStart), Value: 1})
	frame(16 * time.Millisecond)
	fmt.Println(fl.Stack(), rt.Paused(), len(markup.RenderHTML(kitui.PauseMenu(kitui.PauseProps{}))) > 0)
	// Output: [play pause] true true
}
