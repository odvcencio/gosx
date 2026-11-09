# Scene3D graphics recovery and adaptive quality

A browser can take the GPU away from a page at any time: a driver update, a
GPU process crash, a laptop switching graphics chips, or memory pressure. GoSX
rebuilds the renderer and its GPU resources in place, without a page reload.
Your game only needs to pause and resume its own state.

## Pause play while the GPU resets

```go
surface := scene.NewSurface("arena")
cancel, err := surface.OnGraphicsReset(func(reset scene.GraphicsReset) {
    switch reset.Phase {
    case scene.GraphicsResetLost:
        game.Pause("graphics")
    case scene.GraphicsResetRestored:
        game.Resume("graphics")
    }
})
if err != nil {
    // Host builds and worker-hosted surfaces cannot observe the mount.
}
defer cancel()
```

Each reset produces one `lost` event and, once a renderer draws again, one
`restored` event. `Backend` names the renderer after the transition, `Reason`
says why (`webgl-context-lost`, `webgpu-device-lost`, ...), and `Count` is the
number of losses the mount has seen. Page scripts can listen for the same
`gosx:scene3d:graphics-reset` DOM event on the mount element.

| Loss | What the mount does |
|---|---|
| WebGL context lost | Shows a stand-in, then rebuilds WebGL when the browser restores the context (or after a bounded retry). |
| WebGPU device lost | Falls back to WebGL at once so the scene keeps drawing, then returns to WebGPU when the browser grants a new device. |

A mount returns to WebGPU at most twice in five minutes. A third loss in that
window keeps it on WebGL until it remounts, so a failing GPU cannot loop.

## Adaptive quality

Adaptive quality is on by default. It lowers resolution, post effects, or the
quality ladder rung when frames miss their budget, and raises them again after
sustained clean frames. These rules keep it from reacting to the wrong thing:

- A gap of 250 ms or more between frames (a tab switch, a garbage-collection
  pause, a shader compile, a context restore) is not a slow frame. The timing
  window starts over, and quality returns once the stall clears.
- The budget is never shorter than the display's refresh interval. A 30, 48 or
  50 Hz display does not count every frame as a miss. The interval is measured
  only from frames with little CPU work, so a CPU stall cannot raise its own
  budget, and a refresh-rate change is followed within a few dozen frames.
  Intervals that match no common refresh rate (for example a steady 25 ms on a
  60 Hz display) still count as misses.
- If quality drops again right after it was raised, the next raise waits twice
  as long, up to 64 times the base window. One clean window at the requested
  quality resets the wait.
- A renderer swap, including a graphics reset, starts a fresh timing window.
