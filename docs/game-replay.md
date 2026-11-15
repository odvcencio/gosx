# Game replay

`game/replay` records a deterministic game cheaply and plays it back in the browser: scrub to any moment, pause, step, slow down to 0.25x or speed up to 4x, and watch from a free or broadcast camera. `game/replay/replayui` adds the controls as kit components. Everything is generic: you bring a deterministic `sim.Simulation` (a `game.Runtime` works) and a few lines of camera setup.

## How it works

A `Recorder` stores each tick's inputs and, every `Interval` ticks, a keyframe snapshot of the simulation. A `Player` seeks to tick N by restoring the nearest keyframe at or before N and re-simulating the recorded inputs forward. Seeking backward is the same operation, so reverse needs no extra state. The cost of a seek is at most `Interval` simulated ticks.

Tick N means the state after N ticks since recording began (plus `Config.StartTick`).

### Your game must be deterministic

The state after tick N must depend only on a snapshot at an earlier tick and the inputs since. Put all game state in the snapshot. Do not read the wall clock, range over a map in a way that changes state (use `sim.EachInput`), or use unseeded random numbers. Floating-point math is repeatable on one architecture, but only integer or carefully controlled math is repeatable across arm64 and amd64; `room/roomtest` uses fixed-point integers for this reason. The test below shows how to check your game.

## Record

```go
g := newMyGame()                                  // any sim.Simulation
rec := replay.NewRecorder(g, replay.Config{
	Interval: 120,       // ticks between keyframes (default 120)
	MaxBytes: 32 << 20,  // memory bound (default 64 MiB)
	TickRate: 60,        // stored with clips so playback runs in real time
})
// In your fixed-step loop, call rec.Tick(inputs) instead of g.Tick(inputs).
// To keep calling g.Tick yourself, call rec.Record(inputs) just before it.
rec.Mark("goal")         // label the current tick
recording := rec.Recording() // immutable view, safe while recording continues
```

When the recording would pass `MaxBytes`, the recorder drops its oldest keyframe segment, so it always holds the most recent play. `Dropped()` counts the ticks lost and `Start()` is the oldest tick still seekable.

## Play

```go
p, err := replay.NewPlayer(freshGame, recording) // a new game built the same way
p.Seek(1800)             // any tick in [p.Start(), p.End()]
p.StepForward(1)
p.StepBack(1)            // by seek
p.SetSpeed(0.25)         // clamped to 0.25..4
p.Resume()
p.Advance(frameDelta)    // each frame: runs whole ticks, carries the remainder
```

Each frame, read the game state through your normal code and render it through the Scene3D pipeline. `Player.Alpha()` is the fraction between the previous and current tick. `replay.Interpolator` blends the last two samples of whatever you draw, and snaps after a seek so no object glides across the map:

```go
var interp replay.Interpolator
interp.Update(p, func(dst []float64) []float64 { return append(dst, x, y, z) })
values := interp.At(p.Alpha(), buf) // write these to the pose stream
```

## Highlights and clips

`rec.Mark(label)` returns a `Marker`. `recording.Highlight(marker, before, after)` returns a clip of the ticks around it, with the keyframe it needs, and `Marshal()` turns it into bytes that `replay.Unmarshal` reads back anywhere (a file, a URL, a save). The format is versioned, ends with a checksum, and is rejected if damaged. A clip plays in a fresh game with `NewPlayer`, so sharing a highlight is sharing a few kilobytes.

## Cameras

`replay.NewRig` is a camera with three modes that you can switch at any time without a pop:

| Mode | Behaviour |
|---|---|
| Broadcast | A `camera.Director` follows `RigConfig.Subject` (or a base source you supply, such as a `*camera.Frame`) and cuts or blends to named `Shots`. Use `Director()` to play your own sequences and shake. |
| Orbit | Circle a target: `Look`, `Zoom`, `Move` (pan). |
| Fly | Free spectator camera: `Look`, `Move`. |

Feed `Rig.Update(wallDT, player.Alpha())` wall time, not playback time, so the camera stays live while the replay is paused. `Rig.Camera()` returns a `scene.PerspectiveCamera`. After a seek, call `Rig.Snap()` so the follow camera does not fly across the map (`replayui.Controller` does this for you).

## Controls

`replayui.Controller` turns input into player and camera commands, and `replayui.Bar` renders the scrubber, play and step buttons, speed picker, camera picker and marker list in the look of `game/kitui`.

```go
in.Define(replayui.Context())         // hotkeys and gamepad bindings; rebindable
in.Push(replayui.ContextName)
ctl := replayui.New(replayui.Config{Player: p, Rig: rig, Input: in})

// Page head: kitui.Styles(), replayui.Styles(), kitui.Script().
kitui.Bind(root, kitui.Handlers{
	OnAction:  func(name, value string) { ctl.Handle(name, value) },
	OnSetting: func(key, value string) { ctl.HandleSetting(key, value) },
})
kitui.Render(barRoot, replayui.Bar(ctl.View())) // when ctl.View() changes

frame := func(dt time.Duration) {
	in.Update(dt)
	f := ctl.Update(dt) // f.Camera, f.Alpha, f.Ticks, f.Seeked
	// ... sample state, interpolate, draw with f.Camera
}
```

| Control | Keyboard | Gamepad |
|---|---|---|
| Play / pause | Space, K | A |
| Step back / forward | `,` / `.` | d-pad left / right |
| Scrub (hold) | Left / Right, J / L | triggers |
| Speed down / up | `-` / `=` | d-pad down / up |
| Camera next / previous | C / V | Y |
| Broadcast / orbit / fly | F | X |
| Previous / next marker | `[` / `]` | shoulder buttons |
| Restart | Home | |
| Free camera move, look, zoom | WASD, Q/E, mouse (pointer lock), wheel | left stick, right stick |

A marker jump lands `MarkerLead` seconds (default 2) before the marker. The bar is not modal and traps no focus; it is plain buttons, a range input and a list, so Tab, Enter and the arrow keys work without extra script. Re-rendering the bar replaces its HTML and loses keyboard focus inside it (the same limit as the other kit screens), so re-render on button presses and update only the scrubber value and time text each frame.

## Measured

On `room/roomtest` (16 bodies, 60 Hz, 4 seats sending input every tick). Machine: Intel Core Ultra 9 285, Go 1.26, linux/amd64.

| Measure | Result |
|---|---|
| One minute of play, keyframe every 120 ticks | 77,840 bytes (30 keyframes of 332 bytes; inputs are the rest) |
| Seek, keyframe every 30 / 120 / 600 ticks (random tick, mean) | 26 / 102 / 475 microseconds |
| Seek in Chrome on Apple M4 Pro (wasm), keyframe every 60, 900 ticks | 0.087 ms mean over 300 seeks |
| Record one tick | 1.3 microseconds |
| Replay machinery allocations | 0 per step or seek (`TestPlaybackDoesNotAllocate`, measured with a null simulation) |

Your game's snapshot size and tick cost dominate these numbers; run `go test -bench . ./game/replay` with your own game to size the interval. Memory scales with snapshot size over `Interval`, and seek latency with `Interval` times your tick cost.

## Tests

```
go test ./game/replay/...                                   # state hash at tick N equals live play
go test -bench . -run xxx ./game/replay                     # seek, record and play benchmarks
tailnet-run --host oscar-mbp <worktree> go test ./game/replay/...   # arm64
tailnet-run --host buildbox <worktree> go test ./game/replay/...    # amd64
mac-gpu-run <worktree> go test -tags e2e -timeout 5m -run TestReplayViewerBrowser ./e2e/
```

`TestSeekMatchesLiveState` seeks to hundreds of ticks and compares the state hash with the hash live play had at each. `TestGoldenAcrossArchitectures` pins fixed hashes recorded on amd64 and passes on arm64. `TestReplayViewerBrowser` scrubs a wasm replay in Chrome with real mouse and keyboard input and checks the same hashes.

## Limits

- Only the simulation is replayed. Visual effects, audio and UI that read wall time are not; derive them from the tick.
- The player restores snapshots into the game you give it, so build the game the same way (config, seats, content) as the recorded one.
- A clip does not carry the game version. Replaying it on a build whose rules changed gives different states without an error; store your own version string next to the clip and refuse mismatches.
- Reverse playback is repeated seeking, so it is cheap for stepping and scrubbing but not for long continuous reverse play at 4x on a heavy simulation.
