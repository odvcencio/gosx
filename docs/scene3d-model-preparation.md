# Prepare selected Scene3D models

`scene.Surface.PrepareModels` prepares selected assets for an existing mounted
renderer without adding scene nodes. Invoke it from a goroutine before admitting
actors whose first visible frame must include their geometry and material maps.
Enable `ModelPreparation` on the mounted scene to advertise this optional
capability. The preparation module downloads only when this API is invoked;
ordinary scenes do not advertise it or pay its wire cost.

```go
props.ModelPreparation = scene.Bool(true)

ctx, cancel := context.WithTimeout(parent, 30*time.Second)
defer cancel()

result, err := surface.PrepareModels(ctx, []scene.ModelPreparation{
    {ID: "pilot", Src: "/models/pilot.glb"},
    {ID: "scout", Src: "/models/scout.glb"},
})
```

The request must contain 1–32 models with distinct, nonempty IDs and nonempty
sources. The operation snapshots those values. IDs identify failures and do not
allocate actors or scene nodes.

Success means geometry and animation data have decoded in this renderer's
texture-variant scope and every distinct material-map record has completed
native GPU upload. `result.Models` counts selected models; `result.Textures`
counts distinct texture records. It does not guarantee GPU vertex buffers,
shader compilation, or a scene-generation commit. Observe the relevant command
acknowledgement separately when a committed scene is required.

For immutable rigid props, opt in per request with `RigidGeometry: true`.
This also prepares shared, model-local CPU primitive streams used by both WebGL
and WebGPU instantiation. Repeated preparation and the first visible instance
reuse those streams. It creates no scene members or draw calls. Skinned,
morph-animated, node-animated, and auxiliary-content models are unsupported and
return an error rather than freezing animation. Leave this flag unset for
animated actors. Prepared streams remain weakly owned by the decoded asset;
this option does not allocate GPU vertex buffers or compile shaders.

Model or map failure, context cancellation, `Surface.Dispose`, mount removal,
or renderer replacement returns an error. A canceled operation cleans up its
listeners and stops launching material maps. Shared model fetches remain
cache-owned and may finish after cancellation. Retry with a fresh request and
the current mounted surface; discard completions from an obsolete application
generation.

Keep the initial set small. Noncritical debris or effect assets can be prepared
after the first core scene commits. An unrelated model included in an atomic
scene transaction can still delay that transaction even when its hero models
have already prepared.

## Requested frame submissions

Model preparation fills caches, and `MountCommandsAppliedEvent` acknowledges
command acceptance. Neither event promises that the renderer submitted a frame
containing the new model: WebGL parallel shader compilation and WebGPU pipeline
validation can leave a queued render pending while the previous canvas remains.

Attach an optional `scene.MountFrameRequest` to the declaration's
`scene.MountCommandBatch`. `ModelIDs` names the actual live imported instance,
such as `tacet-lod/slot-0`, rather than an asset declaration or all prepared
full/LOD variants. Register `Surface.OnFrameSubmitted` before dispatching:

```go
cancel, err := surface.OnFrameSubmitted(func(receipt scene.MountFrameSubmission) {
    // Check your current phase, request ID and preparation generation here.
    // The receipt certifies a complete renderer submission, not GPU completion
    // or browser compositor presentation.
})
if err != nil { return err }
defer cancel()
err = surface.DispatchCommands(scene.MountCommandBatch{
    Revision: 12,
    Commands: actorDeclaration,
    FrameRequest: &scene.MountFrameRequest{
        ID: "deployment-3", ModelIDs: []string{"tacet-lod/slot-0"},
    },
})
```

The mount emits one `gosx:scene3d:frame-submitted` event after a complete frame
whose actual color draws include every current visible primitive of each
requested model instance. Absent membership, shader-pending, empty, failed,
partial, shadow-only and reflection-only draws never satisfy the request.
The receipt carries the requesting `revision`, the snapshot's
`commandRevision` (which can include later camera or pose commands), current
`hydrationGeneration`, `frameSerial`, renderer and drawn `objectIDs`.
The owner, command snapshot and hydration generation must remain current at
publication. A new request supersedes the previous request;
`Surface.CancelFrameRequest(id)` cancels only that exact ID. Disposal and
renderer replacement cancel pending requests. Applications must re-arm after
recovery or when their pending live full/LOD instance changes.

Receipts are opt-in: ordinary settled frames create no collector or receipt
event, do not scan model IDs, and do not read DOM telemetry. Shader/pipeline
readiness continues to schedule retries. An imported-model request fails closed
on a fallback that cannot actually color-draw that model; Canvas2D's line/point
representation is not acknowledged as an imported mesh. Validate actual
first-visible pixels separately when presentation matters.
