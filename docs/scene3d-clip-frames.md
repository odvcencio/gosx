# Scene3D sampled clip frames

Pose a skinned model from clip times you supply, with no client clock. Use this
when your simulation owns time, for example a rollback netcode game: after a
rollback you send the corrected frame's clip times and the model shows exactly
that pose.

The default Scene3D model playback advances clip time with the browser's frame
clock. Sampled mode replaces that for the models you opt in. The same layers
always give the same pose, after a jump backwards or forwards.

## Declare a sampled model

```go
scene.Model{
    ID:          "fighter-1", // required: frames address models by ID
    Src:         "/models/fighter.glb",
    ClipSampled: true,
}
```

A sampled model shows its bind pose until the first frame arrives. Its
`Animation`, `Loop`, `AnimationSpeed` and `AnimationSeq` fields do nothing, and
a mixer clip never starts. A live `x`, `y`, `z`, rotation or scale patch (for
example from `scene.Live`) still moves the model and keeps the sampled pose.

## Send a frame

Each frame lists, per model, up to four layers. A layer is a clip, an exact
clip time in seconds, a weight and a loop flag.

```go
frame := scene.ClipFrame{Models: []scene.ClipModel{
    {ID: "fighter-1", Layers: []scene.ClipLayer{
        {Name: "walk", Time: float64(f.Tick) / 60, Weight: 1, Loop: true},
        {Name: "hit", Time: hitFrames / 60, Weight: 0.4},
    }},
    {ID: "fighter-2", Layers: []scene.ClipLayer{{Name: "idle", Time: t2, Weight: 1, Loop: true}}},
}}
err := scene.DispatchClipFrame("scene", frame) // js/wasm build
```

`DispatchClipFrame` encodes into a reused buffer and calls
`window.__gosx.scene3d.dispatchClipFrame(target, bytes)`. On other builds,
call `scene.EncodeClipFrame(frame)` (or reuse a `scene.ClipFrameEncoder`) and
send the bytes yourself. From JavaScript, call
`window.__gosx.scene3d.dispatchClipFrame(target, bytes)` with a `Uint8Array`.
It returns a Promise that resolves to `{ applied: true, binary: true }` and
rejects on a malformed frame or an unknown model ID. A rejection also raises
the `gosx:scene3d:clip-frame-error` event, and counters appear on the mount
handle as `__gosxClipFrameStats`.

A frame is complete state for the models it names. Each frame replaces that
model's earlier layers; nothing accumulates. A model the frame does not name
keeps its last pose. An empty `Layers` slice poses the bind pose.

## Layer rules

| Field | Rule |
|---|---|
| `Name` | glTF animation name. When empty, `Index` is used. |
| `Index` | Zero-based glTF animation index, 0 to 32767. Only read when `Name` is empty. |
| `Time` | Clip time in seconds, finite and not negative. Stored as float32. |
| `Weight` | Relative blend weight, finite and not negative. Weights need not sum to one. A weight of 0 skips the layer. |
| `Loop` | What happens when `Time` is past the clip's end. |

**Time past the end.** A looping layer wraps: the sampled time is `Time` modulo
the clip duration, so a time equal to the duration shows the first key. A layer
with `Loop` false clamps: it holds the clip's last key. A clip with zero
duration always shows its first key. The rule is per layer, because glTF clips
carry no loop mode of their own.

**Blending.** Layers blend in slice order, per animated property of each node,
weighted by `Weight`. Translation and scale blend linearly; rotation blends by
spherical interpolation. A node that only one layer animates takes that layer's
value, whatever its weight. A node no layer animates keeps its authored pose.
Weights are relative per node, as in the existing mixer. Blend order matters
only for rotations with three or more layers; it is fixed by slice order.

**Unknown clips.** A layer whose clip does not exist is skipped, with one
console warning per name. An unknown model ID rejects the whole frame and
changes nothing, except while models are still loading: then the frame is
remembered and applied once the model mounts.

## Root motion

The default is that the simulation owns position, so clip root motion is
ignored. For each root node, the sampled horizontal translation (X and Z) is
replaced by the node's authored value. A root node is a glTF scene root, or the
topmost joint of a skin. The vertical (Y) translation stays, so crouches and
jumps in the hips still animate. Rotation and scale are never changed.

If your rig has no dedicated root bone above the hips, the hips are the root
joint and their X and Z travel is dropped too. The pose then stays centered on
the model's position, which is what a fighting game wants. Set
`RootMotion: true` on the model to apply the clip's translation unchanged.

## Wire format (GSP4)

Little-endian. `EncodeClipFrame` writes it and `decodeClipFrame` reads it.

```
"GSP4"
u16 name count, then each clip name: u16 length + UTF-8 bytes
u16 model count, then per model:
    u16 length + UTF-8 model ID
    u8  layer count (0 to 4)
    per layer (11 bytes): u16 clip ref, f32 time, f32 weight, u8 flags
```

Clip ref bit 15 clear: a zero-based position in the name list. Bit 15 set: the
low 15 bits are a zero-based glTF animation index. Flags bit 0 is `Loop`; other
bits must be zero. Two models with four layers each, using two clip names, take
about 130 bytes. The decoder checks the whole frame before any model changes.

## Rendering

The browser samples the clips on the CPU and writes joint matrices, as the
mixer does. Both the WebGL and the WebGPU renderers then skin on the GPU from
those matrices. A sampled model never asks for continuous rendering: a frame or
a transform patch schedules one render.

In steady state a frame allocates almost nothing, so it adds no garbage
collection pressure: decoding, sampling, blending, node transforms and joint
matrices reuse buffers sized when the model mounts. Two fighters with four
layers each measured about 0.3 KB per frame, against about 10 KB per frame for
the general mixer path. A sampled model that also has morph targets or rigid
node animation takes the general path and allocates per frame. The `Dispatch`
call itself still creates one Promise per frame.

The Node benchmark `client/js/scene3d-clip-frame.bench.mjs` (run it with
`node --expose-gc`) measures both paths for two 64-joint models with four
layers each, and reports per-frame mean, p50, p95, p99 and heap growth.
