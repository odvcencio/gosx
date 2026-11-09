# Loops in a Go WASM engine: wait for a frame

A loop that runs on the page must wait for the browser's next animation frame
between iterations. Use `wasm.EachFrame`.

```go
go func() {
    _ = wasm.EachFrame(ctx, func(f wasm.Frame) error {
        // f.Time is the frame timestamp in ms. f.Delta is the ms since the
        // previous frame, and zero on the first frame and after a hidden page.
        return scene.SetCamera(ctx, nextCamera(f.Delta/1000))
    })
}()
```

`EachFrame` blocks, so start it from a goroutine, never from the `Factory`.
It returns when `ctx` is done (with `ctx.Err()`), when the step returns an
error, or with nil when the step returns `wasm.StopFrames`.

## Why a loop needs the wait

A Go WASM engine on the main thread shares one thread with the page. The
browser draws, handles clicks and runs timers only when Go is blocked on
something that ends in a browser event. A promise that is already resolved
does not count: the browser runs it before it goes back to its own work.

`Scene3D.Dispatch`, `SetCamera`, `SetOrbit` and `Live` each wait for a promise
that the Scene3D command bridge resolves at once. A loop made only of those
calls moves from one resolved promise to the next and the page never gets a
turn. It does not draw, it ignores input, and the tab freezes. An app that hit
this hid it with `time.Sleep(16 * time.Millisecond)`, which works but guesses
the frame time. The test `TestSceneCallLoopWithoutAFrameWaitStarvesThePage`
(`engine/wasm/frames_js_test.go`) reproduces the freeze: the browser runs no
task during 3,000 resolved-promise calls. `TestEachFrameLoopKeepsThePageResponsive`
shows the same calls inside `EachFrame`, with browser tasks running between every
two frames.

Plain Go work with nothing to wait on cannot be interrupted at all: the Go
scheduler does not return to the browser while any goroutine can run. Do a bounded
amount of work per frame, or move the work to a worker engine
(`Thread: engine.ThreadWorker`, see `engine-worker.md`).

## What `EachFrame` does for you

- Runs `step` at most once per visible frame, at the display's rate.
- Pauses while the page is hidden and resumes when it is shown. No frame
  request stays queued while hidden. The first frame after a hidden page has
  `Delta` 0, so a simulation does not take one long step.
- Stops cleanly: cancelling `ctx` cancels the pending frame request.
- In an engine worker it uses the worker's frame clock (60 Hz by default, see
  `engine-worker.md`), so the same loop runs on either thread.

## Other ways to wait for a frame

| Need | Use |
|---|---|
| Repeat once per frame | `wasm.EachFrame` |
| Wait for one frame, then continue | `wasm.NextFrame(ctx)` |
| A finite animation: sample commands over a duration | `Scene3D.PlayFrames` |
| Move the camera between two poses | `Scene3D.TransitionCamera` |
| A fixed-step simulation with interpolation | `game/loop.Driver` |
| A game that draws into a Scene3D every frame | `game/loop.Driver` on `host.NewSceneFrameSource` (below) |

`PlayFrames`, `TransitionCamera` and `game/loop.Driver` already wait for frames,
so a loop written with them needs nothing more.

## A game loop inside the Scene3D frame

A game on the page's main thread should not run its own
`requestAnimationFrame` next to the Scene3D renderer's. With two loops, the
poses a game writes in its frame are drawn one display frame later, or in the
wrong frame when the two drift apart. Let the renderer own the frame instead:

1. Create a `game.Runtime`.
2. Create a `host.SceneFrameSource` for the Scene3D mount's element id.
3. Drive the runtime with a `loop.Driver` on that source.
4. In the render callback, write this frame's poses through a
   `scene.StreamWriter` (the binary instance stream).

```go
rt := game.New(game.Config{
    FixedStep: time.Second / 60,
    CatchUp:   game.CatchUpLag, // netplay and replays: never drop a step
})
frames := host.NewSceneFrameSource("arena") // the Scene3D mount's id
stream := scene.NewStreamWriter("arena")
driver := loop.New(rt, frames)
driver.Start(func(alpha float64, frame game.Frame) {
    if !stream.Prepare() {
        return // the instance-stream codec is still loading
    }
    // Blend the previous and current simulation state by alpha, then
    // encode the instance transforms into one frame.
    _, _ = stream.Apply(scene.InstanceStreamFrame{
        BatchID: "units", Revision: frame.Index, Kind: scene.InstanceStreamTransform,
        Count: len(units), Data: interpolatedMatrices(alpha),
    })
})
defer driver.Stop()
```

Each display frame then runs in this order:

1. The renderer's `requestAnimationFrame` fires.
2. The renderer calls the source's frame hook with the frame time and the
   milliseconds since the last hook frame (0 on the first one, and after the
   mount could not draw because it was hidden or out of view).
3. The driver steps the runtime: `PhaseUpdate`, the fixed steps the clock
   allows, `PhaseLateUpdate` and `PhaseRender`.
4. The driver calls your render callback with the interpolation alpha. The
   poses you write there land in the renderer's retained buffers at once.
5. The driver hands the alpha to the source (`host.FrameAlphaSink`). The hook
   returns it to the renderer, which stores it as `bundle.simAlpha` and in
   `handle.getTelemetry().frameHook.alpha`. The built-in renderer does not
   blend streamed instances itself, so write already-interpolated poses.
6. The renderer builds the frame and draws it, with your poses.

While a hook is registered the mount draws every frame, at the pacing its
props select (see `FramePacing` in `scene.Props`), even when the scene clock is
paused or the page asks for reduced motion: the game pauses its own time with
`rt.SetPaused`. When the driver stops, the source unregisters and the mount goes
back to drawing only when something changes. The renderer's JavaScript API is
`handle.setFrameHook(fn)` on the mount's `__gosxScene3DHandle`; it returns a
function that removes the hook, and one hook replaces the previous one.

Until the mount's renderer is ready, after the mount is disposed, and in an
engine worker, `SceneFrameSource` steps on a plain `FrameSourceJS` instead, so
the game runs from its first frame. A worker engine's renderer is on the page,
out of reach of a synchronous call: its frames come from the worker frame clock,
and its streamed frames reach the page one message later.

### What one frame costs

Per-frame data takes binary paths. Avoid the JSON ones in a frame loop:

| Per-frame work | Use | Not |
|---|---|---|
| Instance transforms | `scene.StreamWriter.Apply` (one copy into a reused JS buffer, applied synchronously) | `Scene3D.Dispatch` (JSON, one promise per call) |
| Frame scheduling | `FrameSourceJS` and `SceneFrameSource` (one `js.Func` for the source's life, one browser request per frame) | a new `js.FuncOf` per frame |
| Gamepads | `host.NewGamepadReader()` (reused buffers; reads a pad again only when its timestamp changes) with `gamepad.Poller` | `host.NavigatorSource{}` (fresh slices every poll) |
| Worker input | `host.NewForwardedInput()` (one binary record per event through `__gosx.worker.onInputBytes`) | parsing the JSON of `onInput` |

`game.Runtime.Step` itself allocates nothing in steady state, and it runs the
scene builder only after `InvalidateScene` (see `game/README.md`).
