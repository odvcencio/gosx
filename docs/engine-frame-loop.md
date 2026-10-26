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

`PlayFrames`, `TransitionCamera` and `game/loop.Driver` already wait for frames,
so a loop written with them needs nothing more.
