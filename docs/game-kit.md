# Game kit

The game kit is the layer above `game/*` that every browser game ends up writing by hand: input actions, a state flow, a loading gate, options, a camera, save slots and the screens that go with them. Each package is small and independent. Use any of them alone.

| Package | Does |
|---|---|
| `game/actions` | Named actions on keyboard, mouse, pointer lock, gamepad and touch. Stacked input contexts, rebinding, labels, local multiplayer. |
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

The kit screens build their HTML with package `m31labs.dev/gosx/markup`, not the root `gosx` package, so a browser build that uses them does not link the `.gsx` compiler. Build your own browser-side nodes with `markup` too:

```go
hud := markup.El("p", markup.Attrs(markup.Attr("class", "hud")), markup.Text("Round 2"))
kitui.Render(root, markup.Fragment(kitui.PauseMenu(kitui.PauseProps{}), hud))
```

Importing `m31labs.dev/gosx` in browser code links the compiler back in. A minimal program that renders the pause menu is 2.9 MB (633 KB with Brotli) with `markup` and 6.0 MB (1.25 MB with Brotli) with the root package, built with Go 1.26. `gosx.Node` is an alias of `markup.Node`, so server code can mix the two packages.

## Input actions

- A `Map` holds contexts. `Push("menu")` makes the menu the top context; an `Opaque` context hides the ones below. Inputs held when the stack changes are ignored until released, so the key that opens a menu does not press the menu's buttons.
- Call `Update(dt)` once per frame. `Update` and the query methods (`Pressed`, `Down`, `Value`, `Vec2`, `State`) do not allocate.
- A button pressed and released between two `Update` calls reads as pressed in the next frame, then released in the one after (the tap latch). Analog sticks do not latch.
- A held button let go and pressed again between two `Update` calls reads as pressed again. Two whole taps between two `Update` calls still read as one.
- Timing per action: `Tap`, `Hold`, `DoubleTap`. Read `Tapped`, `Holding`, `State().HoldStarted`, `DoubleTapped`.
- A press can end without the player letting go: the window loses the focus (`ReleaseAll`), the browser takes a touch away, or the action's context leaves the stack. The action then reads `Released` and `State().Canceled` together, and is not `Tapped`. Logic that acts on a release, such as a throw when the key comes up, skips a canceled one.
- When the browser uses a key itself, withdraw it at that moment with `Suppress(source)`: in a dialog's `cancel` event, `in.Suppress(actions.Key("Escape"))` keeps the Escape that closed the dialog from pausing the game as well, however many frames pass before the next `Update`. The next press counts.
- Keys aimed at a form control are skipped unless `BrowserConfig.KeepKey` says otherwise; `actions.KeepUnusedKeys` sends the game every key the control does not use itself (Escape from a menu, a letter from a slider).
- `BrowserConfig.StopRepeatClicks` keeps a held Enter or Space from pressing a control the focus moved to while the key was down: the press that opens a dialog does not go on to press the dialog's main button.
- `Rebind(ctx, action, slot, source, policy)` returns the conflicts and, under `ConflictReject`, changes nothing. `ConflictSwap` trades the sources; `ConflictUnbind` clears the other binding. `Capture()` then `TakeCaptured()` gives a "press a key" flow; Escape cancels. A slot number equal to the binding count adds a binding (up to `MaxBindings`); `RemoveSlot` deletes an added one.
- Pause: bind `actions.PauseBindings()` (Escape, gamepad Start) in gameplay and `actions.BackBindings()` in the menu, then call `flow.HandleInput(map, flow.Controls{})` each frame.
- `Export()` returns only the changed bindings; `Save`/`Load` use `game/storage`. Settings hold the same data in `Settings.Bindings`.
- Local multiplayer: `NewRouter(p1Map, p2Map)`, `Join(device)` on a first press from a new device, then `Feed` and `Update` on the router. Devices are `keyboard`, `mouse` and `pad:N`.
- `actions/bridge.Publisher` copies action state into a `game.Input` for systems that read it.

### Fixed-step simulation

A simulation that steps at a fixed rate runs on some frames and not others. `actions.NewTicks(m, actions.TickOptions{})` hears every edge the Map resolves and hands them to the next step, so each press and release reaches exactly one step, in order, and a step that runs twice in a frame sees them once:

```go
ticks := actions.NewTicks(in, actions.TickOptions{})
// each frame: in.Update(dt); then for each fixed step:
ticks.Tick()
if ticks.Pressed("jump") { /* ... */ }
if st := ticks.State("throw"); st.Released && !st.Canceled { bullet := !st.Tapped; _ = bullet }
```

- A tap between two steps reads `Pressed` and `Released` together, with the release's `Tapped` and `HeldFor`; `Edges()` lists them in order. `Down` and `Value` are the levels now.
- `Pulse(name)` sends a page command (a button on the panel) to the next step; read it with `Pulsed`. `Clear()` drops what waits, at a whistle or a dialog.
- `actions.Flick` turns a stick into one-shot directional presses, with a fire and a rearm line.

### Repeat and screen changes

- `ActionDef.Repeat{Delay, Interval}` makes a held button read `Repeated` again and again on the Map clock (at most one repeat per `Update`, so a slow frame does not burst). Without it `Repeated` equals `Pressed`.
- `Map.SuppressHeld()` withdraws everything held now until it is let go, as a context change does, for a screen change that is not a context change. `Map.Neutral(actions.Rest{})` reports whether every key, button and stick is at rest.

### Gamepad makers and glyphs

`actions.ParsePadID(id)` reads Chrome's, Firefox's and Safari's ids for a pad: vendor and product numbers, the maker, and the names its buttons wear (`PadXbox`, `PadPlayStation`, `PadNintendo`, or `PadGeneric` for a pad whose id names no console). `actions.ButtonGlyph(style, n)` is the short name and drawing hints for button `n` of the standard mapping ("A", "✕" with cap "cross" in the south slot, "ZR"). `PadPoller.PollStates(list, sink)` takes one poll read once, so several consumers share it; a pad that leaves releases what it held as canceled.

### The device in hand and prompts

`hand.NewTracker(hand.Rules{})` follows the last device used: tee every event into it (`actions.Tee(in, tracker)`), give it each poll with `Pads`, attach `hand.AttachPointers` for fingers and the mouse, and call `Frame()` once per frame. A stick takes the hand only when it is pushed across the line from inside it, so a resting or drifting stick never does; the mouse takes it only when `Rules` say so. `hand.Start` guesses the hand before the first use (a plugged pad, the remembered name, a touch-only screen). `prompt.Table` writes texts such as `"Snap ball · {act}"` for the hand: `Expand`, `Parts` and `ExpandGlyphs`, which draws each control as a `kbd.glyph` element with `data-kind`, `data-pad`, `data-cap` and `data-slot`.

### Gamepad focus on GoSX pages

`focus.Scope{Root: menu}` walks the page's buttons and menus with the D-pad: `Move(focus.Down)`, `Accept()` (a button clicks; a select steps to its next choice and a slider one step, each with one bubbling `change` event). The open `dialog[open]` holds the focus while it is up, and a roving row (`Row`, `Member`) is one stop that Left and Right move inside. `focus.Activate`, `StepMenu` and `StepSlider` work on a control that does not have the focus.

### Seats

`seats.New(seats.Config{NewMap: newMap})` runs local multiplayer seats: a fresh press of Enter or Space on the keyboard, or A on a pad, claims a seat; a pad that leaves (or a new id in its slot) makes its seat wait, and only a waiting seat can be claimed until it is filled again; every change disarms the seats, and a seat arms again on a frame its device rests through. Each frame: `SyncPads(list)`, feed the poller's and the keyboard's events through a `Tee` that includes the seats, then `Update(dt)`. Read gameplay from `Map(i)` (or `Ticks(i)`) only while `Armed(i)`.

### Touch

On a touch screen the page draws its own stick and buttons, and they are input sources like a key: `actions.TouchAxis("steer", false)`, `actions.TouchAxis("steer", true)` (y grows downward, as on a gamepad) and `actions.TouchButton("sprint")`. Mark the elements and attach once:

```html
<div data-gosx-stick="steer" data-gosx-stick-center="follow" data-gosx-stick-radius="56"></div>
<button data-gosx-hold="sprint">Sprint</button>
```

```go
touches, detach := actions.AttachTouch(in, actions.TouchConfig{
	Stick: actions.Stick{Radius: 56, Deadzone: .12, Floating: true, Follow: true},
	Allow: func(name string, stick bool) bool { return playing }, // false leaves the touch to the page
})
```

- Each finger holds one control, and several fingers hold several: one thumb on the stick, the other on a button. A pen and the primary mouse button count as fingers. A stick belongs to the first finger that takes it; a button stays down until every finger on it has gone.
- A stick's box is the area a finger can land in. `fixed` puts the centre in the middle of the box; `floating` puts it where the finger lands; `follow` also lets the centre trail a finger that goes past the rim. The deflection is a circle: the two axes together are never longer than 1.
- While a control is held its element carries `data-gosx-touch="on"`. A held stick also carries `--stick-cx` and `--stick-cy` (its centre, in pixels from the element's left and top) and `--stick-x` and `--stick-y` (-1 to 1), so a stylesheet draws the ring and the knob with no script.
- A lift ends the touch. `pointercancel`, a lost pointer capture, the window losing the focus and the page being hidden cancel it: a button's release then reads `Canceled`.
- Hiding an element does not take its finger away: the browser goes on sending that finger's moves to it. When a control goes away with a finger on it, call `touches.Drop(name)`; the finger must touch again to act.
- Give the elements `touch-action: none`, or the browser scrolls with the finger and cancels the touch.
- Act on the finger, not on `click`. While one finger is down the browser makes no click of a second finger's tap. A GoSX island does run a button's click handler itself when a touch lifts from the button, but only for the last finger that landed on a button, and a finger that lands anywhere afterwards makes it forget that one: press a button, then take the stick, and the button's tap is lost. Bind the button as a `TouchButton` and act on `Released` that is not `Canceled`. Then drop the click made of the same tap, or the button is pressed twice. The island's comes during the lift, before `AttachTouch` hears the lift, so `touches.Held(name)` is still true; the browser's comes after `TouchConfig.Ended`.
- `actions.Touches` is the tracker without the browser: `StickDown`, `Move`, `ButtonDown`, `Up`, `Cancel`. A test or another host feeds it directly.
- The pointer listeners and the page's markup were exercised in one game's browser checks, with touch emulation in Chrome. No phone or tablet was held.

## Rumble

`host.RumbleGamepad(index, host.Rumble{Duration: 160 * time.Millisecond, Strong: 0.8, Weak: 0.4})` shakes the gamepad in the browser's slot `index` (the number after `pad:` in an action device name). `Strong` is the heavy motor and `Weak` the light one, each from 0 to 1. The call returns at once, so it is safe in a frame callback, and a new effect replaces the one playing. It reports false when the slot is empty, the pad has no motors or the browser has no vibration support; `host.StopRumble(index)` stops the motors. Browsers play an effect only after the player has pressed something. Tested with a faked `navigator.getGamepads` (`game/host/rumble_js_test.go`); no real pad was held.

## Camera

`Director` runs the base source (a `Follow` or `Frame`) every frame and decides what is shown. `Take`, `GoTo` and `Play` blend from the pose on screen, so a handoff never pops. `Interrupt` ends an unlocked sequence when the player takes control. With reduced motion, blends become cuts and sequences follow `Sequence.Reduced` (skip, or play as cuts). Shake is trauma-based and is zero when `ShakeScale` returns 0. `Pose.Perspective` converts to `scene.PerspectiveCamera` with `TransitionMS` 0 so the renderer does not ease on top of the director.

## Saves

`SlotStore` writes each slot to two copies and always overwrites the one that does not hold the newest valid save. A copy carries a sequence number and a SHA-256 checksum, so a torn or corrupt write falls back to the previous save. A save from a newer build returns `ErrSaveTooNew` and is never overwritten. `Autosaver.Update(dt)` saves on an interval.

## Side select

`kitui.SideSelect` and `kitui.SideSelectPanel` are a controller-select screen for any two-team game. Each connected device (the keyboard, each gamepad) is an icon in one of three columns: side A, the middle (spectate), side B. A player slides the icon with the device's own input, readies up, and can edit an optional settings row under the icons.

```go
sides := kitui.NewSideSelect(kitui.SideConfig{
	SideNames: [2]string{"Home", "Away"},
	Fields: []kitui.Field{{ID: "quarter", Label: "Quarter length", Index: 1,
		Options: []kitui.Option{{Value: "2", Label: "2 min"}, {Value: "5", Label: "5 min"}}}},
	Router: router, PlayerSlots: [2][]int{{0, 2}, {1, 3}}, // feeds actions.Router device assignment
	OnStart:  func(r kitui.Result) { /* r.Sides, r.Spectators, r.Fields */ },
	OnRender: func() { kitui.Render(root, kitui.SideSelectPanel(kitui.SideSelectProps{Model: sides})) },
})
sides.ConnectKeyboard()
actions.Attach(sides, actions.BrowserConfig{}) // keyboard: arrows or WASD, Enter/Space, Escape
// each frame: sides.SyncPads(host.NavigatorSource{}); pads.Poll(host.NavigatorSource{}, sides)
kitui.Bind(body, kitui.Handlers{OnAction: func(n, v string) { sides.HandleAction(n, v) }}) // mouse on the settings row
```

- `SideSelect` is an `actions.Sink`, so it takes the place of an `actions.Map` behind `actions.Attach` and `actions.PadPoller`. Left/Right slide the icon (clamped at the edges), Confirm toggles ready (Enter, Space, pad A or Start), Cancel un-readies, then returns to the middle, then calls `OnBack`. Down moves that device's cursor into the settings row, where Left/Right change the field and Up/Down move between fields. The left stick works like the d-pad. A device that is unknown shows up on its first press.
- The match starts (`OnStart`) when every seat on a side is ready and both sides have a seat. `AllowEmptySide` allows one side to be empty for a computer opponent; `MaxPerSide` limits a side. Seats in the middle never hold up the start. A change that makes the screen not startable withdraws the start.
- With `Router` set, each local seat on a side is assigned to the next player index in `PlayerSlots`; seats in the middle are unassigned. Seat order decides who gets which slot.
- Online: pass remote players with `SetRemote([]kitui.Seat{...})` (the game supplies them). They show as dashed icons, cannot be moved from this screen, and slide when the data changes. `OnChange` reports each local change so the game can send it; `SetField` applies the host's choice; `SetFieldsReadOnly(true)` locks the settings row for guests; `Set` applies a server correction.
- The panel is plain HTML with data attributes (`data-device`, `data-slot`, `data-ready`), so it works in a static export. The icon that moved slides from its old column with a CSS animation (off under `prefers-reduced-motion`). Colors come from `--kit-*` variables, plus `--kit-side-a` and `--kit-side-b`. Screen readers get each icon's name, side and ready state in its `aria-label`, and changes in a live region.

`e2e/side_select_e2e_test.go` runs this in Chrome with the keyboard and two simulated gamepads (`navigator.getGamepads` stubbed): sliding, ready-up, router assignment, the settings row, the online variant.

## Browser tests

`e2e/game_kit_e2e_test.go` (build tag `e2e`) builds `e2e/testdata/kitbrowser` for js/wasm, serves it from a local server and drives it with Chrome:

```
go test -tags e2e -run TestGameKitBrowserAdapters ./e2e/
```

It covers keyboard, mouse buttons, wheel, the tap latch, blur release, pause on blur and tab hide, gamepad Start, the boot gate (stall, soft retry, one reload, failed, late ready), the watchdog script without WASM, and keyboard use and the Tab focus trap in the pause, settings and rebinding screens. Blur, tab hide and the gamepad are synthetic (window events and a stubbed `navigator.getGamepads`). Headless Chrome refuses pointer lock, so that test uses a `pointerLockElement` override.

## Limits

The event listeners allocate the Go strings that `syscall/js` creates for `event.code`; the per-frame path does not. After `kitui.Render` replaces a panel's HTML, keyboard focus inside it is lost.
