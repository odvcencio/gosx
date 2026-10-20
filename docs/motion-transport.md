# Shared motion and shader surfaces

`motion.Program.WithTransport` gives semantic HTML, scene objects, cameras,
material uniforms and Selena shader time one seekable playhead. Author the
program in Go, render it with `ctx.Runtime().MotionScope`, and use
`server.MotionControl` for playback buttons. Application JavaScript is not
required. The optional `motion-transport` chunk uses GoSX's existing scheduler;
ordinary motion, video and static pages do not load it.

Run the complete example:

```sh
go run ./examples/motion-shader
```

The example uses one `.sel` material as a flat background and a torus. Its
staggered lettering, swaying form, camera and warming material share time.
Keyboard and touch controls can pause, reverse, replay and seek to the bloom.

## Author a playhead

```go
program := motion.NewProgram("intro").WithTransport(motion.TransportOptions{
    Duration: 8 * time.Second,
    Labels: map[string]time.Duration{"crest": 3500 * time.Millisecond},
    Scenes: []string{"#background", "#sculpture"},
})
clock := program.Playhead("clock") // absolute seconds
gain := program.Curve("gain", clock, []motion.CurveStop{
    {At: 0, Value: .3}, {At: 4, Value: .9}, {At: 8, Value: .5},
}, true)
program.BindStyle(gain, ".title", "opacity", "")
program.BindMaterialUniform(gain, "#background", "shader-shape", "gain")

body := ctx.Runtime().MotionScope(program,
    content,
    server.MotionControl(server.MotionControlProps{
        Transport: "intro", Action: server.MotionPlay,
    }, gosx.Text("Play")),
    server.MotionControl(server.MotionControlProps{
        Transport: "intro", Action: server.MotionSeek, Label: "crest",
    }, gosx.Text("The crest")),
)
```

Use the existing `BindSceneNode`, `BindCamera`, `BindStyle` and
`BindMaterialUniform` methods. Curves sample absolute time; they do not
accumulate deltas when seeking. Transport duration is at most one day. Labels
must lie inside it, with at most 128 labels and 64 scene selectors per scope.
Scene selectors refer to mounts already present inside the scope; deferred
engine hydration inherits the current clock when the handle appears.

The default is paused at zero. Set `AutoPlay: true` for automatic playback.
Play advances forward from the current position. Reverse plays backward and
starts at the end when currently at zero. Replay returns to zero while
preserving pause intent. The transport sleeps at its endpoints and while the
document is hidden; returning to a tab does not advance by the hidden time.

Reduced motion settles automatic playback at the end. Bound Selena scene
clocks stay at zero so shader animation remains still. Semantic HTML remains
readable without enhancement; playback buttons enable once their scope mounts.

## Compose expressive keyframes

`Program.SampleTrack` connects the existing native `motion.Track` and `Key`
types to any scalar input and samples scalar, vector, color or quaternion values. With a playhead, key times are absolute seconds;
with a scroll signal they are scroll progress. The optional `motion-track`
chunk reuses the shared signal graph, easing functions and lifecycle. It owns
no clock and adds no bytes to the base runtime. Scene node, material and pin
bindings also load its optional adapters, which apply values before geometry
baking and material interning. Static scenes retain their existing budgets.

```go
rise, err := program.SampleTrack("heading-rise", clock, motion.Track{
    Ease: motion.Ease{Kind: motion.EaseOutBack, Args: []float64{.8}},
    Keys: []motion.Key{
        {T: .2, Value: motion.ScalarV(32)},
        {T: 1.4, Value: motion.ScalarV(0)},
        {T: 4, Value: motion.ScalarV(0)}, // hold, then leave
        {T: 5, Value: motion.ScalarV(-12)},
    },
})
// Handle err before rendering the scope.
program.BindStyle(rise, ".heading", "transform.y", "px")
```

An outgoing key's `Ease` overrides the track's default. Linear, power, cubic
Bezier, back and stepped easing use the same math as native `motion.Eval`.
Back easing retains its overshoot. `InterpStep` holds the previous key until
the exact next key boundary. Values hold outside the key range and reverse
seeks reproduce the same value; no elapsed-time integration is involved.

Use the existing `ExpandStagger` to delay copies before sampling them:

```go
tracks := motion.ExpandStagger(template, []int{0, 1, 2}, motion.StaggerSpec{
    From: motion.FromCenter,
    Delay: .14, // seconds with a playhead, otherwise the input's units
})
for i, track := range tracks {
    ref, err := program.SampleTrack(fmt.Sprintf("line-%d", i), clock, track)
    if err != nil {
        return err
    }
    program.BindStyle(ref, selectors[i], "transform.y", "px")
}
```

The same distributions support first, last, center, a chosen index and grids.
Bind several independent tracks to overlap cues, or use `Map`, `Clamp` and
`Mix` to compose their outputs. Scene transforms, material uniforms, cameras
and SVG/scene-transition progress can all consume the resulting scalar signal.
Progress bindings clamp to `[0,1]`; use direct scalar property bindings when
overshoot is desired. `server.MotionControl` and transport capture settlement
continue to work without application JavaScript.

Each track requires 2–4,096 finite, strictly increasing keys with one consistent
arity. Generators return an error; their native motion IR remains available.
Quaternion keys must be unit length and use shortest-arc
slerp, including the normalized-linear fast path for nearly parallel poses.
`InterpSlerp` is accepted only for quaternion tracks. Existing linear scalar
wire data remains unchanged. `Target`, `TargetID` and `PropID`
are not used by this bridge: the ordinary binding methods select the target.
Reduced motion holds each track's final key and restores the current input
position when the preference is lifted.

### Follow a curved path

`InterpCubicSpline` accepts the native `Key.InTangent` and `Key.OutTangent`.
Tangents express change per input unit: units per second for a playhead, or per
unit of scroll progress for `ScrollProgress`. Each segment scales its outgoing
and incoming tangents by its duration before sampling the Hermite curve.

```go
departure := motion.Vec3V(0, .3, 0)
arrival := motion.Vec3V(0, -.3, 0)
path, err := program.SampleTrack("camera-path", clock, motion.Track{
    Interp: motion.InterpCubicSpline,
    Keys: []motion.Key{
        {T: 0, Value: motion.Vec3V(-1, 0, 3), OutTangent: &departure},
        {T: 4, Value: motion.Vec3V(1, 0, 3), InTangent: &arrival},
    },
})
// Handle err before binding.
program.BindCamera(path, "#sculpture", "position")
```

Use matching incoming and outgoing tangents at an interior key for continuous
velocity. Tangents must have the same arity as their key values and finite
components. Complete spline segments ignore easing, matching native `Eval`;
a segment missing either required tangent falls back to eased interpolation.
Quaternion splines interpolate components and normalize the result, rather
than flipping signs for shortest-arc slerp. Quaternion tangents are derivatives
and do not need unit length. A near-zero result uses the identity quaternion.
Scalar, vector and color splines can overshoot; choose tangents accordingly.

### Sample a complete pose or color

Use the existing value constructors instead of separate scalar tracks for every
component. One prepared sample finds the segment once and writes into a reusable
buffer; the graph owns snapshots when notifying subscribers.

```go
position, err := program.SampleTrack("form-position", clock, motion.Track{
    Keys: []motion.Key{
        {T: 0, Value: motion.Vec3V(-.4, -.2, 0)},
        {T: 5, Value: motion.Vec3V(.4, .3, -.1)},
    },
})
// Handle err before binding.
program.BindSceneNode(position, "#sculpture", "form", "position")

rotation, err := program.SampleTrack("form-rotation", clock, motion.Track{
    Keys: []motion.Key{
        {T: 0, Value: motion.QuatV(motion.QuatFromEuler(.2, 0, 0))},
        {T: 5, Value: motion.QuatV(motion.QuatFromEuler(.4, 2, .1))},
    },
})
// Handle err before binding.
program.BindSceneNode(rotation, "#sculpture", "form", "rotation")

tint, err := program.SampleTrack("tint", clock, motion.Track{
    Keys: []motion.Key{
        {T: 0, Value: motion.ColorV(.34, .81, .76, 1)},
        {T: 5, Value: motion.ColorV(.96, .58, .34, 1)},
    },
})
// Handle err before binding.
program.BindStyle(tint, ".caption", "color", "")
program.BindMaterialUniform(tint, "#sculpture", "form", "glow")
```

`position` and `scale` bindings take Vec3 tracks; `rotation` takes quaternion
tracks. Cameras accept Vec3 positions and quaternion rotations. Scene rotation
reuses the renderer's quaternion-to-Euler conversion. Material uniforms accept
scalar, Vec2, Vec3 and Vec4 values, including colors. Color style bindings use
`color`, `background` or `backgroundColor`. CSS custom properties receive raw,
space-separated channels, with the requested unit on each component.

Vectors and colors interpolate their supplied channels without a color-space
conversion. Style colors clamp RGBA channels to `[0,1]`; shader uniforms retain
HDR values and easing overshoot. `Mix` supports matching vector/color arities;
quaternion mixing and passing vector values through scalar operators return an
error. Binding shapes are checked before serializing a program, and cyclic graphs or dependency paths longer than
1,024 edges are rejected. Reduced motion retains an independent
final pose and restores the current playhead when the preference is lifted.

## Apply a shader to either dimension

```go
material, _, err := scene.CompileSelenaMaterial(source, scene.SelenaMaterialOptions{})
// Handle err, then select the same compiled material for either surface.
surface, err := (scene.ShaderSurface{
    Material: material,
    Shape: scene.ShaderPlane, // ShaderSphere, ShaderBox or ShaderTorus
    MeshID: "shader-shape",
}).Scene()
// Handle err. Compose normal Scene3D features before mounting if needed.
config := surface.EngineConfig()
config.MountID = "background"
node := ctx.Engine(config, gosx.Text("A quiet patterned background."))
```

The surface reuses Scene3D's capability handling, WebGPU/WebGL fallbacks and
Selena compilation. Defaults cap rendering at 30 FPS, DPR 1.5 and two million
pixels, with adaptive quality enabled. A plane uses explicit orthographic
bounds so its UV domain fills portrait and ultrawide viewports. Set the mount's
layout through the normal engine/CSS contract. 3D shapes use perspective;
`Interactive: true` enables their existing orbit behavior.

Keep selectable text, forms and controls in GSX HTML. A shader can sit behind
or alongside them. Existing `CompileSelenaPost` and `CustomPost` process rendered
scene textures. DOM-region tracking supplies geometry, not captured HTML pixels.

## Integrations and capture

Framework packages such as slides can obtain a mounted scope through
`window.__gosx.motion.transport(id)`, or create a transport with `createTransport`.
The runtime control and capture positions use milliseconds; Go curves and
Scene3D shader clocks use seconds. `attach({sample, settled, present})` joins
another absolute sampler without introducing a separate frame loop. Optional
`settled` hooks await producers; `present` hooks then await fresh renderer
submissions, including changes that leave the clock at the same time.

After pausing or seeking, `settled()` flushes queued writes, waits for each
bound scene to submit the requested clock revision, and allows presentation
boundaries before capture. It follows deferred hydration and reduced-motion
policy changes, and rejects a concurrent playback/seek command or a bounded
timeout. Bound scenes must be visible for renderer submission; offscreen scenes
can remain parked until they re-enter view. This verifies submission, not GPU
completion or frame rate.

The shared clock drives declarative Selena materials, custom post effects and
authored point shaders on both browser backends. Renderer telemetry and
stateful water/particle compute retain wall time. A playhead does not reconstruct
simulation history, event-driven glTF state, audio or network events; those
systems need their own deterministic replay/checkpoints.

## Prepared 3D scene transitions

Use `scene.CommandTransition` when a pose includes geometry, colors, camera
parameters or several scene command types. GoSX validates and prepares the
endpoints once, then samples absolute progress without rebuilding command maps
or comparing serialized JSON on every frame.

```go
progress := program.Map("pose", clock, 0, 8, 0, 1)
err := program.BindSceneTransition(progress, "#sculpture", scene.CommandTransition{
    From: []scene.Command{
        scene.SetTransformCommand("form", scene.TransformPatch{
            X: -.6, ScaleX: 1, ScaleY: 1, ScaleZ: 1,
        }),
        scene.SetCameraCommand(scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 10), FOV: 50}),
    },
    To: []scene.Command{
        scene.SetTransformCommand("form", scene.TransformPatch{
            X: .6, RotationY: 3.14159, ScaleX: 1, ScaleY: 1, ScaleZ: 1,
        }),
        scene.SetCameraCommand(scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 8.5), FOV: 45}),
    },
})
// Handle err before rendering ctx.Runtime().MotionScope(program, ...).
```

The binding requests the optional `motion-scene` feature and adds its mount to
an attached transport's scene clocks. It retains the latest pose during deferred
hydration, coalesces pending command submissions and participates in
`transport.settled()` before renderer presentation. It owns no separate frame
loop. Avoid binding the same property with both a transition and a scalar scene
binding; independent material uniforms can share the same playhead.

Matching numeric fields, equal-length arrays and six-digit hex `color` values
interpolate. Object identity, names, topology/index arrays and geometry kinds
change discretely at the destination. Incompatible object kinds retain the
source until the endpoint. Omitted commands retain their other endpoint; use
explicit `RemoveObject` commands to hide actors. Zero transform/point coordinates
and unit scales are restored explicitly on rewind. Camera commands use immediate
poses so renderer camera easing cannot fight the shared clock. Reduced motion
shows the exact destination pose.

Each pose supports up to 10,000 commands, with a 4 MiB wire plan, 32 levels of
value nesting and a 200,000-node input budget. Nonfinite values and unsafe object
keys are rejected. These are declarative poses; stateful simulations still need
replay/checkpoints. Native Go consumers can call `transition.Prepare()` once and
`player.Sample(progress)` repeatedly; returned native data is independently
owned. The browser uses immutable prepared snapshots and reuses unchanged poses.

Framework integrations use `motion.prepareSceneTransition(from, to)` and
`motion.bindSceneTransition(mount, plan)`. The binding exposes `sample(progress)`,
`setTransition(plan)`, `settled()` and `dispose()`. The existing Scene3D command
bridge owns asset resolution, readiness, cancellation and ordered application.
Disposal aborts pending work and leaves the last applied pose in place.
Applications author the typed Go construct rather than a browser adapter.

## SVG diagrams on the same playhead

```go
p := motion.NewProgram("diagram").WithTransport(motion.TransportOptions{
    Duration: 2 * time.Second,
})
progress := p.Map("progress", p.Playhead("clock"), 0, 2, 0, 1)
p.BindSVGMorph(progress, "#before", "#after")
// Render both SVG snapshots inside ctx.Runtime().MotionScope(p, ...).
```

The optional `motion-svg` feature samples SVG geometry from server-rendered
snapshots. Hide the source snapshot in a `hidden` HTML container and keep the
destination readable without JavaScript. Match actors using `data-morph-id`;
SVG without actor keys can morph as one group. The framework compiles geometry
once, then applies absolute progress, so seek and reverse reproduce exact poses.
Use a `Curve` signal for typed easing or bind pointer, hover or scroll progress.

Compatible numeric attributes, transforms and paths interpolate. Different path
commands, SVG arcs, missing attributes and unmatched actors retain valid
destination geometry and fade in. This deliberately avoids guessing arbitrary
path topology or interpolating arc flags. Reduced motion always shows the exact
destination. Disposal restores destination geometry and original inline opacity.
No animation loop or layout reads run per sample, and pages without SVG morphs
do not request this feature.

Run `go run ./examples/motion-svg` for a complete Go-authored app with semantic
playback buttons. Framework adapters can use `motion.createSVGMorph(fromSVG,
toSVG, {easing: "ease-in-out"})` and call `sample(progress)` / `dispose()`;
GoSX-slides uses this boundary for storyboard navigation. Applications use the
Go binding rather than authoring browser scripts.

## Repeated native spring sampling

For repeated absolute-time samples in Go, prepare a spring once:

```go
spring := motion.Spring{Mass: 1, Stiffness: 400, Damping: 2}
samples := spring.Prepare(0, 1)
value := samples.Value(playheadSeconds)
settleTime := samples.Duration()
```

`PreparedSpring` retains immutable position/velocity checkpoints from the same
240 Hz Euler integrator as `Spring.Value`. Each sample replays at most 31 steps
and preserves the original float bits, including reverse seeks and step
boundaries. Sampling allocates nothing and supports concurrent readers.
Preparation runs at most 2,400 steps once and uses at most 1,216 bytes of
checkpoint tables per spring. A one-off sample can use `Spring.Value` directly.

`motion.Runtime.Load` automatically prepares decoded spring-generator tracks.
Identical physics and endpoints share a table, with signed-zero distinctions
preserved. Each decoded program admits at most 64 KiB of checkpoint tables;
additional unique springs use the uncached evaluator. Reduced motion keeps the
same final value. These optimizations apply to Go and its WASM motion runtime;
they do not change the browser's interruptible reactive spring integrator.

Non-finite physics use the uncached evaluator. Spring time handling is bounded:
NaN and negative times hold the starting value, positive infinity holds the
target, and replay never exceeds the ten-second duration cap even if invalid
physics produce a NaN settle time.
