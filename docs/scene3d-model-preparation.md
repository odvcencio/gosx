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
