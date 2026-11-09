# Game kit

The game kit is the layer above `game/*` that every browser game ends up writing by hand: input actions, a state flow, a loading gate, options, a camera, save slots and the screens that go with them. Each package is small and independent. Use any of them alone.

| Package | Does |
|---|---|
| `game/actions` | Named actions on keyboard, mouse, pointer lock and gamepad. Stacked input contexts, rebinding, labels, local multiplayer. |
| `game/flow` | State stack (boot, title, lobby, play, pause, results) tied to `Runtime.SetPaused` and input contexts. |
| `game/boot` | Loading gate with progress, stall watchdog, retry budget and one automatic reload. `boot/bootpage` renders the WASM preload links and a watchdog script. |
| `game/settings` | Typed, versioned options: bus volumes, motion, camera shake, render scale, frame cap, quality, language, bindings. |
| `game/camera` | Follow and multi-target framing, trauma shake, bookmarks and sequences, one director with control handoff. |
| `game` (`slots.go`) | Two-copy save slots that survive a torn write, autosave, versioned migration. |
| `game/kitui` | Pause, settings, rebinding and loading screens as GoSX nodes styled by `--kit-*` CSS variables. |
| `game/replay` | Record, scrub and re-watch a deterministic game with free and broadcast cameras; see [game-replay.md](game-replay.md). |

## Example

This is `game/kitui/example_test.go`; it compiles and runs under `go test`.

```go
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
```

## Browser wiring (js/wasm)

```go
detach := actions.Attach(in, actions.BrowserConfig{PointerLockOnly: true}) // keyboard, mouse, wheel, blur
pads := actions.NewPadPoller()                                             // call pads.Poll(host.NavigatorSource{}, in) each frame
flow.AttachBrowser(fl)                                                     // pause on window blur and tab hide
settings.WatchSystemMotion(opts)                                           // prefers-reduced-motion
marker, key := boot.BrowserMarker()                                        // sessionStorage guard for the one auto-reload
gate := boot.New(boot.Config{Reload: boot.BrowserReload, Marker: marker, MarkerKey: key,
	OnChange: boot.DOMObserver(root)}, tasks...)
kitui.Bind(root, kitui.Handlers{OnAction: ..., OnSetting: func(k, v string) { kitui.ApplySetting(opts, k, v) }, OnRebind: ...})
```

Server side, put `kitui.Styles()`, `kitui.Script()`, `bootpage.Watchdog()` and `bootpage.PreloadLinks(manifest, "/gosx/assets", "game", bootpage.PreloadOptions{})` in the page head, and render `kitui.BootRoot(...)` around the canvas. The same markup works in a static export.

## Input actions

- A `Map` holds contexts. `Push("menu")` makes the menu the top context; an `Opaque` context hides the ones below. Inputs held when the stack changes are ignored until released, so the key that opens a menu does not press the menu's buttons.
- Call `Update(dt)` once per frame. `Update` and the query methods (`Pressed`, `Down`, `Value`, `Vec2`, `State`) do not allocate.
- A button pressed and released between two `Update` calls reads as pressed in the next frame, then released in the one after (the tap latch). Analog sticks do not latch.
- Timing per action: `Tap`, `Hold`, `DoubleTap`. Read `Tapped`, `Holding`, `State().HoldStarted`, `DoubleTapped`.
- `Rebind(ctx, action, slot, source, policy)` returns the conflicts and, under `ConflictReject`, changes nothing. `ConflictSwap` trades the sources; `ConflictUnbind` clears the other binding. `Capture()` then `TakeCaptured()` gives a "press a key" flow; Escape cancels. A slot number equal to the binding count adds a binding (up to `MaxBindings`); `RemoveSlot` deletes an added one.
- Pause: bind `actions.PauseBindings()` (Escape, gamepad Start) in gameplay and `actions.BackBindings()` in the menu, then call `flow.HandleInput(map, flow.Controls{})` each frame.
- `Export()` returns only the changed bindings; `Save`/`Load` use `game/storage`. Settings hold the same data in `Settings.Bindings`.
- Local multiplayer: `NewRouter(p1Map, p2Map)`, `Join(device)` on a first press from a new device, then `Feed` and `Update` on the router. Devices are `keyboard`, `mouse` and `pad:N`.
- `actions/bridge.Publisher` copies action state into a `game.Input` for systems that read it.

## Camera

`Director` runs the base source (a `Follow` or `Frame`) every frame and decides what is shown. `Take`, `GoTo` and `Play` blend from the pose on screen, so a handoff never pops. `Interrupt` ends an unlocked sequence when the player takes control. With reduced motion, blends become cuts and sequences follow `Sequence.Reduced` (skip, or play as cuts). Shake is trauma-based and is zero when `ShakeScale` returns 0. `Pose.Perspective` converts to `scene.PerspectiveCamera` with `TransitionMS` 0 so the renderer does not ease on top of the director.

## Saves

`SlotStore` writes each slot to two copies and always overwrites the one that does not hold the newest valid save. A copy carries a sequence number and a SHA-256 checksum, so a torn or corrupt write falls back to the previous save. A save from a newer build returns `ErrSaveTooNew` and is never overwritten. `Autosaver.Update(dt)` saves on an interval.

## Browser tests

`e2e/game_kit_e2e_test.go` (build tag `e2e`) builds `e2e/testdata/kitbrowser` for js/wasm, serves it from a local server and drives it with Chrome:

```
go test -tags e2e -run TestGameKitBrowserAdapters ./e2e/
```

It covers keyboard, mouse buttons, wheel, the tap latch, blur release, pause on blur and tab hide, gamepad Start, the boot gate (stall, soft retry, one reload, failed, late ready), the watchdog script without WASM, and keyboard use and the Tab focus trap in the pause, settings and rebinding screens. Blur, tab hide and the gamepad are synthetic (window events and a stubbed `navigator.getGamepads`). Headless Chrome refuses pointer lock, so that test uses a `pointerLockElement` override.

## Limits

The event listeners allocate the Go strings that `syscall/js` creates for `event.code`; the per-frame path does not. After `kitui.Render` replaces a panel's HTML, keyboard focus inside it is lost.
