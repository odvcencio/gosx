# Scene3D timelines

Author a finite sequence in Go, then play it on a mounted scene without sending
per-frame updates from your server. A tabletop game can drop a piece, move the
camera, and settle the piece's scale in one plan.

Set `scene.Props{Timelines: scene.Bool(true)}` on a scene that will receive
timeline plans. This explicitly advertises the versioned playback chunk URL;
omitting the flag or setting it to false leaves the URL out of ordinary pages.

```go
import (
    "encoding/json"
    "time"

    "m31labs.dev/gosx/motion"
    "m31labs.dev/gosx/scene"
)

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
payload, err := json.Marshal(plan) // Validates; wire times are seconds.
```

Return `payload` in your existing authenticated action or hub response. After
applying any prerequisite scene commands, hand the decoded plan to the public
Scene3D bridge. The target may be a mount element, its stable ID, or its handle.

```js
await window.__gosx.scene3d.dispatchCommands(mount, response.commands);
const playback = await window.__gosx.scene3d.playTimeline(mount, response.timeline);
playback.pause();
playback.seek(0.25); // Seconds; may seek backward while playback is unfinished.
playback.resume();
const result = await playback.finished; // { finished: true }
```

`At(offset, tweens...)` starts parallel tweens at an absolute offset.
`Then(tweens...)` starts them after the latest existing end time. `Duration()`
returns that end time. The first `From` holds before a property's start;
subsequent gaps hold the preceding `To`. Values are absolute: author rotations
in radians, positions in scene units, and camera `fov` in degrees.

Object properties are `x`, `y`, `z`, `rotationX`, `rotationY`, `rotationZ`,
`scaleX`, `scaleY`, `scaleZ`, and `opacity`. Camera properties are position,
rotation, and `fov`. Other properties retain their current values. Easing uses
the existing `motion.Ease` kinds, including power, cubic Bézier, steps, and back
curves. Rotation interpolates authored scalar angles; author the intended arc.

Plans require an ID, 1–1,024 tweens, finite values, and a total duration of at most
one day. Overlapping tweens on the same property are rejected. A malformed
replacement leaves the existing playback running. Each mounted scene has one
active timeline; starting another cancels the previous one with
`{ finished: false, reason: "replaced" }`. `cancel()` holds the last applied
values and resolves with reason `"cancelled"`; disposal resolves with reason
`"disposed"`. `finish()` settles final values. `finished` rejects on an application
error. Treat timeline playback as presentation; keep game rules on the server.
Explicit object, camera, model, or animation commands through `dispatchCommands`
supersede presentation with reason `"commands"`. Material, particle, and post
effect updates keep the current timeline running.

Playback uses GoSX's shared read/evaluate/write/render scheduler. Pausing or
finishing stops its continuous work. Background gaps are capped to 100 ms per
frame. Reduced motion settles the final pose without tweening, including when
the preference changes during playback. Camera updates also synchronize the
built-in controls so the final camera survives the next interaction.
Both JavaScript scenes and shared Go/WASM render bundles receive the same
presentation bindings. Settled object values hold until an authoritative scene
command supersedes them; the settled camera returns to user control.

The implementation loads `bootstrap-feature-scene3d-timeline.js` on first play.
It is included in production manifests and size reports and uses the advertised,
versioned URL. `Renderer.SetBootstrapFeatureScene3DTimelinePath` accepts an asset
prefix for embedded deployments. Opted-in scenes advertise the URL without
loading or preloading the chunk. URL overrides preserve the explicit opt-in.

Native hosts can prepare a `scene.NewTimelinePlayer(plan)` once and call
`player.Sample(elapsedSeconds)` for compact scene commands. `plan.Sample` is the
one-shot convenience. Hosts own their clocks, cancellation, and reduced-motion
preferences. Sampling does not rebuild the scene or mutate the plan.
