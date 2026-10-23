# Scene3D crowds on a moving parent

Keep thousands of animated agents on a ship that moves. Two opt-in features
make this cheap: a partial motion frame that carries only the agents that
changed, and a parent node whose world matrix carries the whole crowd.

Both build on the GPU-motion crowd path: an `InstancedGLBMesh` of skinned
agents that share one primitive, driven by `MotionFrame`s. The WebGL and WebGPU
renderers draw it; see [Crowds on WebGPU](#crowds-on-webgpu). Existing crowds
and frames behave as before.

## Send only the agents that changed

A full `MotionFrame` (GSP3) restates every agent in a batch, by ID. A
`PartialMotionFrame` (GSP5) names each agent by its slot, its position in the
batch's `Instances` list, and lists only the agents that changed.

```go
// First frame, and after any membership change: the whole batch.
full := scene.MotionBatch{ID: "passengers", Instances: all}
scene.DispatchMotionFrame("scene", scene.MotionFrame{Batches: []scene.MotionBatch{full}}, nil)

// Each 10 Hz tick: the agents that moved.
partial := full.Partial(changedSlots) // slots in ascending order
scene.DispatchPartialMotionFrame("scene",
    scene.PartialMotionFrame{Batches: []scene.PartialMotionBatch{partial}}, nil)
```

An agent a partial frame leaves out keeps its last motion key and animation, and
the GPU keeps interpolating it. Rules:

- Send a full frame first. An agent that has never had a motion frame draws in
  its declared pose until it gets one.
- `Size` must equal the batch's mounted instance count. The client rejects a
  frame built before a membership change with `membership-changed`. Send a full
  frame after you add, remove or reorder agents.
- Slots must ascend strictly and stay below `Size`. A batch holds at most 65,535
  agents.
- Partial frames use the same dispatch call and the same `fallbackCommands`
  options as full frames. From JavaScript, pass the bytes of
  `scene.EncodePartialMotionFrame` to `window.__gosx.scene3d.dispatchMotionFrame`.
- The browser keeps one pending frame per mount. Two pending partial frames merge
  (the newer entry wins per slot), so no changed agent is lost. A full frame never
  replaces a pending partial frame, and a partial frame never replaces a pending
  full frame: both run, in order.

On the wire a changed agent takes 93 bytes (slot, both motion keys, animation
state), against 93 plus the ID length in a full frame.

## Ride a parent node

Set `ParentNode` on the batch. Instance positions, rotations and motion frames
are then relative to that node, and the renderer applies the node's current
world matrix once per frame.

```go
scene.InstancedGLBMesh{
    ID: "passengers", Src: "/models/passenger.glb",
    ParentNode: vessel.NodeID, // or any Model ID or object ID
    Instances:  agents,        // positions in the ship's own frame
}
```

The node is any scene object whose ID equals `ParentNode`, or starts with
`ParentNode + "/"` (the parts of a model). An object with exactly that ID wins
over a part. Crowd agents never count as the node, so a batch may carry an ID
under its parent's ("ship/riders" on node "ship"). The renderer reads its model matrix
every frame, so a pose computed in the browser needs no message: the vessel
writes its pose into its parts' matrix each frame and a parented crowd follows.
While no such object exists, the batch draws in world space.

Agents sit in the node's frame. For a `scene.Vessel` that is the model's own
frame: metres, bow toward -Z, waterline at Y=0, deck at `DeckHeight`.

What follows the parent: the colour and shadow passes, whole-batch culling and
depth sorting (the batch's bounds move with the parent), the shadow-map cache,
and pointer picking. Motion agents are picked at their latest motion key, not at
the pose they were declared with.

Limits:

- The WebGL and WebGPU crowd paths follow a parent. Canvas and headless
  rendering ignore `ParentNode`; leave it empty there.
- A selected agent of a parented batch stays in its batch and draws no outline.
  An outline needs a separate record, which would sit in world space.
- A per-instance `ParentMatrix` is still rejected in motion frames. `ParentNode`
  is the per-batch replacement.
- `scene.Vessel` has one walk rectangle by default (`Walk.Surfaces` gets one
  moving surface sized from beam and length). Set `Vessel.Decks` for many decks
  and `Vessel.Cutaway` to cull the agents above a deck; see
  `docs/scene3d-powered-vessels.md`. Agents are not constrained to a deck by the
  renderer: keeping them on floors is simulation logic (`vessel.Layout`).

## Colour one agent at a time

Set `MeshInstance.Color` (a CSS hex or `rgb()` colour) and one batch can draw a
crowd whose agents differ only by colour. A body part that comes in five colours
is then one batch, not five.

```go
scene.InstancedGLBMesh{
    ID: "shirts", Src: "/models/shirt.glb",
    Material:  scene.StandardMaterial{Color: "#ffffff"}, // white shows the instance colour as written
    Instances: []scene.MeshInstance{
        {ID: "a", Color: "#d8a373"},
        {ID: "b", Color: "#3a5a8c"},
        {ID: "c"}, // no Color: drawn in the batch colour
    },
}
```

The instance colour multiplies the batch material's colour and texture in linear
light, so a white material shows the colour you wrote and a coloured material
darkens it. On the wire the batch lists each distinct colour once (`palette`) and
each instance carries a 1-based index (`tint`). For 1,000 agents that adds about
9 bytes per coloured agent (`,"tint":3`), against 18 for a colour string on every
agent. A batch with no colours keeps its old wire form. A recolour reaches the
renderer as an ordinary pose patch, uploads one small stream and does not
rebuild the batch.

Limits:

- Only the opaque crowd path tints: WebGL (pose and motion modes) and WebGPU.
  A translucent crowd, which draws one record per agent for depth sorting, and
  the Canvas fallback ignore `Color`.
- An agent that leaves its batch because its transform is not a plain matrix or
  its appearance differs still draws on the crowd shader, and keeps its colour.
- Vertex colours are not read. The PBR mesh path of both renderers has no
  vertex-colour attribute, for batches or for ordinary meshes.

## Cost

`client/js/scene3d-crowd-partial.bench.mjs` measures the main thread for 5,000
agents on a rolling ship, 5% of them changing per 10 Hz tick (Node, no GPU,
median of five runs):

| Path | Wire bytes per tick | Decode and apply per tick | Mean per frame (60 Hz) |
|---|---|---|---|
| Full frames, world space | 513,912 | 27.4 ms | 6.55 ms |
| Partial frames, parent node | 23,274 | 1.2 ms | 1.35 ms |

The Go encoder writes 513,917 and 23,279 bytes for the same shapes. GPU upload
bytes barely fall: the renderer caps a stream upload at 32 calls and covers the
span between the first and last changed slot beyond that, so 5% of agents
scattered through a batch touch almost all of it. Agents that change together
upload only their own ranges.

## Wire format (GSP5)

Little-endian.

```
"GSP5"
u16 clip count, then each clip name: u16 length + UTF-8 bytes
u16 batch count, then per batch:
    u16 length + UTF-8 batch ID
    u16 size             instances in the whole batch
    u16 changed count
    per changed agent (93 bytes):
        u16 slot         strictly ascending, below size
        20 x f32         prev x y z, rotation x y z, scale x y z, tPrev;
                         then the same nine fields for next, then tNext
        u16 clip index   0 is no clip, then the name list
        f32 clip start time, u8 loop (0 or 1), f32 playback rate
```

The decoder checks the whole frame before any agent changes.

## Crowds on WebGPU

A scene that sets `PreferWebGPU` draws its crowds on the WebGPU renderer with
the features above: palette-skinned poses, `MotionFrame` motion interpolated on
the GPU, `PartialMotionFrame` updates, `ParentNode`, and shadows. Nothing
changes in the authoring API. The planner builds the same batch record for both
backends, and a scene that forces WebGL keeps drawing it there.

How it differs from WebGL: the renderer reads each agent's record from a storage
buffer, 16 floats and 3 pose rows per agent in pose mode, 24 floats in motion
mode, and draws with a list of agent indices. The joint-pose atlas is an
`rgba32float` texture. A partial frame writes one buffer range per run of
changed slots (at most 32 writes, then one range that covers them), and a
moving parent writes one 624-byte uniform. The renderer needs vertex-stage
storage buffers; on a device without them hydration falls back to the ordinary
skinned path, as it does when the atlas is too large.

### Cull agents on the GPU

`scene.GPUDriven` (the opt-in for GPU-driven instancing) now covers crowds.

```go
scene.Props{
    GPUDriven: &scene.GPUDriven{Occlusion: true},
    // ...
}
```

One compute dispatch per batch and view tests every agent's bounding sphere
against the camera frustum, and against each shadow light's frustum. The sphere
comes from the palette bounds, which already cover every pose, and the shader
repeats the motion interpolation, so the test sees the same position the draw
does. Survivors go into a list that an indirect draw reads. The CPU does no
per-agent work: the cull reads the records on the GPU.

With `Occlusion: true` the main pass splits in two, as it does for instanced
meshes:

1. The early pass draws the walls, decks and every agent that was visible last
   frame.
2. A Hi-Z pyramid (the farthest depth over each block of pixels) is built from
   that depth.
3. The late pass draws the agents that were hidden last frame and now pass the
   frustum and pyramid tests. Every agent's visibility is stored for the next
   frame.

The pyramid comes from the current frame's depth, not the last frame's, because
a ship that heaves and rolls and a camera that follows it would make a
reprojected pyramid hide visible agents. It costs no extra geometry pass: the
decks are drawn once, in the early pass. A test that cannot prove an agent hidden
draws it, so culling leaves every visible agent in the frame (see Cost for the
one case where it removes a stray pixel).

Costs: one compute dispatch per batch and view, and with `Occlusion` one extra
render pass, one pyramid build (a seed pass and one dispatch per level) and one
more dispatch. When a scene uses both GPU-driven instancing and crowds with
`Occlusion`, each builds its own pyramid; the second pass sees the first one's
depth.

### Cost

`scripts/scene3d-crowd-webgpu-bench.mjs` runs a stand-in for a cruise ship: six
decks, the lower four behind a front wall, a crowd spread over them in motion
mode, the whole ship heaving and rolling every frame, and a partial motion frame
for 5% of the agents every sixth frame. The table is from headless Chromium on
a development host with no usable GPU: WebGPU runs on SwiftShader (a
software Vulkan), WebGL on SwiftShader GL, a 640x360 canvas, 120 frames per run.
Main-thread times are the planner (`createSceneRenderBundle`, which walks every
agent) plus the renderer's `render()` call, and vary by about a millisecond from
run to run because the software rasterizer shares the CPU.

| Agents | Mode | Main thread ms (mean / p99) | Frustum survivors | Drawn after Hi-Z | Software raster ms/frame |
|---|---|---|---|---|---|
| 5,000 | WebGPU, no culling | 1.31 / 4.10 | 5,000 | 5,000 | 359 |
| 5,000 | WebGPU, `GPUDriven{}` | 1.86 / 5.40 | 4,860 | 4,860 | 384 |
| 5,000 | WebGPU, `Occlusion: true` | 2.46 / 10.40 | | 2,232 | 174 |
| 5,000 | WebGL | 0.92 / 2.20 | | | |
| 7,800 | WebGPU, no culling | 2.35 / 5.30 | 7,800 | 7,800 | 533 |
| 7,800 | WebGPU, `GPUDriven{}` | 2.85 / 9.70 | 7,580 | 7,580 | 451 |
| 7,800 | WebGPU, `Occlusion: true` | 2.35 / 7.60 | | 3,458 | 283 |
| 7,800 | WebGL | 0.90 / 2.10 | | | |

Culling does no per-agent CPU work. It adds a few compute and render passes,
which cost up to about a millisecond of main-thread time per frame in these runs
(the 7,800-agent Hi-Z run shows none). Hi-Z draws 44% to 45% of the agents here,
because the two open decks hold a third of them and a coarse pyramid cannot prove
the rest hidden near the wall's top edge. The last column is CPU rasterization,
not GPU time: this host has no usable GPU, so the table measures the work saved
(about half the vertices), not a GPU speed-up. Run the script on a machine with a
GPU and a headed browser (`... present`) for GPU times; it also prints the
renderer's timestamp-query time when the device offers it.

A frame with Hi-Z differs from the unculled frame by 26 to 30 of 230,400 pixels in
this scene. Each is a single pixel where an agent about a metre behind the wall
shows through it when drawn; WebGL on SwiftShader leaks the same pixels, so the
leak is in the unculled draw, and culling removes the agent that causes it. A
plain frustum cull changes no pixel.

### Limits on WebGPU

- Crowd agents are not GPU pick targets. The pick pass skips them.
- A crowd material draws without detail layers. Water and ocean reflection
  passes, and the water object textures, skip crowds.
- An agent that leaves its batch (a different appearance, or selected) draws as
  a batch of one: it skins correctly but is not culled.
- A batch uses at most 32 clips and one atlas of at most 32 MiB across the
  renderer, as on WebGL.
- A crowd is not recorded into a render bundle, so a scene with a crowd
  re-encodes its main pass every frame.
