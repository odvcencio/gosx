# Scene3D timelines

Author finite node and camera sequences in Go and bind them to a motion program.
`Timeline.BindMotion` shares the existing playhead, scheduler and Go playback
controls with DOM, SVG, material and Selena shader animation. It requires no
application script or per-frame server updates.

```go
plan := scene.NewTimeline("place").At(0,
    scene.Tween{
        Node: "piece", Property: "y", From: 1, To: 0,
        Duration: 200 * time.Millisecond,
        Ease: motion.Ease{Kind: motion.EaseOutPow},
    },
    scene.Tween{
        Camera: true, Property: "y", From: 4, To: 2,
        Duration: 400 * time.Millisecond,
    },
).Then(scene.Tween{
    Node: "piece", Property: "scaleX", From: 1.1, To: 1,
    Duration: 100 * time.Millisecond,
})
program := motion.NewProgram("placement").WithTransport(motion.TransportOptions{
    Duration: plan.Duration(),
    Labels: map[string]time.Duration{"landed": 400 * time.Millisecond},
    Scenes: []string{"#board"}, // Also synchronize shader/animation clocks.
})
if err := plan.BindMotion(program, program.Playhead("clock"), "#board"); err != nil {
    return nil, err
}
body := ctx.Runtime().MotionScope(program,
    board, // Scene3D engine mounted as "board" inside this scope.
    server.MotionControl(server.MotionControlProps{
        Transport: program.ID, Action: server.MotionPlay,
    }, gosx.Text("Place piece")),
    server.MotionControl(server.MotionControlProps{
        Transport: program.ID, Action: server.MotionReverse,
    }, gosx.Text("Reverse")),
    server.MotionControl(server.MotionControlProps{
        Transport: program.ID, Action: server.MotionSeek, Label: "landed",
    }, gosx.Text("Landed")),
)
```

The complete Go/Selena example is [motion-shader](../examples/motion-shader/main.go).
Run `go run ./examples/motion-shader` and use its playback buttons. See
[shared motion](motion-transport.md) for pause, replay, labels and other bindings.

`At(offset, tweens...)` starts parallel tweens at an absolute offset.
`Then(tweens...)` starts them after the latest existing end time. `Duration()`
returns that end time. The first `From` holds before a property's start;
subsequent gaps hold the preceding `To`. A zero-duration tween cuts at its exact
start. At a shared boundary the new tween's `From` takes effect, even when it
differs from the previous `To`. Seeking backward reconstructs the same pose.
Values are absolute: rotations are radians, positions are scene units, and
camera `fov` is degrees.

Object properties are `x`, `y`, `z`, `rotationX`, `rotationY`, `rotationZ`,
`scaleX`, `scaleY`, `scaleZ`, and `opacity`. Camera properties are position,
rotation, and `fov`. Other properties retain their current values. Easing uses
the existing `motion.Ease` kinds, including power, cubic Bézier, steps, and back
curves. Rotation interpolates authored scalar angles; author the intended arc.

Plans require an ID, 1–1,024 tweens, finite values, and a total duration of at most
one day. Overlapping tweens on the same property are rejected. `BindMotion`
validates the complete resulting program before changing it and copies the plan.
Signal names are scoped under `<timeline ID>.motion`; conflicting names return
an error. Reuse one clock for several plans with distinct IDs. The input may be
any scalar motion signal; its units must be seconds since the plan's start.

The binding compiles to existing map, sampled-track, step and mix signals.
It does not start another clock or require `scene.Props.Timelines`. The normal
motion feature loader supplies the adapters. Long sequences use a balanced
selection tree. Reduced motion settles the final pose; scope disposal releases
the bindings. A paused scope retains ownership of its bound properties, so use
its controls to manage playback and avoid concurrently binding another source
to the same property. Imperative scene commands do not dispose a motion scope.

## Native sampling and imperative host playback

Native hosts can prepare a `scene.NewTimelinePlayer(plan)` once and call
`player.Sample(elapsedSeconds)` for compact scene commands. `plan.Sample` is the
one-shot convenience. Hosts own their clocks, cancellation, and reduced-motion
preferences. Sampling does not rebuild the scene or mutate the plan. Exact
endpoints are preserved even when interpolation would lose precision.

For existing imperative host integrations, `json.Marshal(plan)` emits a
validated wire plan with times in seconds. Set
`scene.Props{Timelines: scene.Bool(true)}` or `<Scene3D timelines={true}>` on
scenes receiving plans through the public Scene3D `playTimeline` bridge.
The optional `bootstrap-feature-scene3d-timeline.js` chunk loads on first play
from the advertised versioned URL. `Renderer.SetBootstrapFeatureScene3DTimelinePath`
accepts an asset prefix for embedded deployments.

This imperative API has its own playback lifecycle: each mounted scene has one
active timeline; replacement, disposal, cancellation and authoritative object,
camera, model or animation commands settle its `finished` result. Invalid
replacement leaves playback running. `pause`, `resume`, `seek` and `finish`
control that playback. Material, particle and post-effect commands preserve it.
These cancellation rules apply to imperative playback, not persistent motion
scope bindings. Keep game rules on the server.
