# Scene3D crowds on a moving parent

Keep thousands of animated agents on a ship that moves. Two opt-in features
make this cheap: a partial motion frame that carries only the agents that
changed, and a parent node whose world matrix carries the whole crowd.

Both build on the GPU-motion crowd path: an `InstancedGLBMesh` of skinned
agents that share one primitive, driven by `MotionFrame`s. Existing crowds and
frames behave as before.

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
`ParentNode + "/"` (the parts of a model). The renderer reads its model matrix
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

- Only the WebGL crowd path follows a parent. Other backends ignore
  `ParentNode`; leave it empty there.
- A selected agent of a parented batch stays in its batch and draws no outline.
  An outline needs a separate record, which would sit in world space.
- A per-instance `ParentMatrix` is still rejected in motion frames. `ParentNode`
  is the per-batch replacement.
- `scene.Vessel` has one walk rectangle by default (`Walk.Surfaces` gets one
  moving surface sized from beam and length). Set `Vessel.Decks` for many decks
  and `Vessel.Cutaway` to cull the agents above a deck; see
  `docs/scene3d-powered-vessels.md`. Agents are not constrained to a deck by the
  renderer: keeping them on floors is simulation logic (`vessel.Layout`).

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
