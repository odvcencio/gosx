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

## Model texture delivery profiles

Set `Props.TextureDeliveryProfile` to `scene.TextureDeliveryLow`,
`scene.TextureDeliveryStandard`, `scene.TextureDeliveryHigh`, or
`scene.TextureDeliveryAuto` for one mounted scene. An empty profile preserves
the previous variant-selection behavior. `PrepareModels` and ordinary model
draws use the same mount scope, backend capabilities, and decoded-asset cache
key. The manifest must contain built, reachable KTX2 variants keyed by the
source image URI. For an embedded GLB image or `data:` image, use the GLB URL
path followed by `#image=N`, where `N` is its zero-based glTF image index.
`hydrate.Manifest.SetTextureVariants(assetpipe.BuildVariantManifest(report))`
publishes the built variant metadata, including role and color space.

Low and standard require a compatible built variant for each used material
image. They select backend-proven BC formats where available or a bounded
uncompressed KTX2 variant where supported. They fail preparation if none is
available, including after an upload error; they do not silently load the
authored full-resolution image. High and the empty legacy profile may retain
the authored image where no suitable variant exists. A shared linear ORM image
can serve metallic/roughness and occlusion when its packed variant preserves
the sampled channels. A shared sRGB base-color/emissive image uses the
base-color variant; transparent materials require a four-channel encoding
that preserves alpha. Other ambiguous role or color-space reuse is not
substituted.
Current material shaders do not reconstruct a normal's Z channel from BC5 RG,
so BC5 normal variants are rejected; provide a compatible full-RGB variant.

For a live quality change, include **every active model source** and call
`Surface.ApplyTextureProfile` from a goroutine. It prepares up to 256 selected
models in batches of 32, uploads their target maps, then rehydrates the scene.
The previous textured scene remains drawn until the new scene commits. Failure,
cancellation, a changed active-model set, disposal, or renderer replacement
returns an error and retains the committed profile. Concurrent profile changes
supersede earlier attempts. Ordinary model commands remain responsive and
restore the committed scope if they invalidate a staged change. Existing
cache-owned textures from an old profile may remain retained after a switch;
this API makes no total VRAM or eviction guarantee.

Public generic-fixture hardware verification of feature commit `3ffb3d7355`
on Chrome 155 used typed Go/WASM
`Surface` calls and real WebGL (D3D11) and WebGPU (NVIDIA Blackwell) uploads.
Both backends selected low BC1 sRGB base color and BC7 linear shared ORM, then
standard BC7 sRGB base color and BC7 ORM, each with seven mip levels. Held
target uploads left the previous rendered pixels unchanged; successful commit
changed the fixture's pixels. Upload failure, cancellation, and a model-command
race retained the old textured frame and profile. An embedded-image GLB also
switched correctly. Cache record counts increased after the switch, confirming
old-profile retention. A non-BC browser was unavailable for hardware proof;
portable KTX2 selection is covered by focused selector tests. The later
texture-retry merge and regenerated corpus passed focused Go, JS, TypeScript,
and generated-bundle checks; the screenshots do not attest that merged blob.
These fixture results do not establish game-art visual parity or total GPU
memory usage.

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

## Export from Blender

GoSX loads glTF 2.0 and GLB. Export with File > Export > glTF 2.0 and the
settings below. Retopologize and bake normals in Blender first; GoSX reads the
baked normal map and the exported vertex normals as they are.

| Blender setting | Value | Why |
|---|---|---|
| Format | glTF Binary (.glb) | One file per model. `.gltf` with separate files also works. |
| Mesh > Apply Modifiers | on | The loader never sees Blender modifiers. |
| Mesh > Normals, Tangents | on | Tangents come from your bake. Without them GoSX computes tangents from the UVs. |
| Mesh > Compression (Draco) | off | The loader rejects Draco. See "Compression". |
| Material > Images | Automatic | Embedded PNG or JPEG goes through `gosx assets build`. |
| Animation > Sampling, Always Sample | on | Keeps cubic-spline and constraint results exact. |
| Animation > Optimize Animation Size | on | Fewer keyframes load faster. `gosx assets build` also drops redundant keyframes. |

### UV sets

The renderer samples every texture of a material with one UV set. The loader
picks it from the material:

1. Each map's `texCoord` selects its set (`0` is `TEXCOORD_0`, `1` is
   `TEXCOORD_1`). A `KHR_texture_transform` `texCoord` overrides it.
2. If the base color is textured, its set wins. Otherwise the most-used set
   wins.
3. If the maps use different sets, every map samples the winning set and the
   console warns once per material:
   `reads maps from different UV sets`.

So a model whose base color, normal, and occlusion maps all use UV1 renders
correctly. A model with base color on UV0 and a baked AO or lightmap on UV1
does not: bake the AO into the base UV set, or into the packed
occlusion-roughness-metalness map on UV0. Mixed sets in one material need a
second UV stream in both renderers; that is not built yet.

### Joint limits

A skin may have up to 256 joints. WebGPU reads the joints from a storage
buffer. WebGL2 reads rigs up to 64 joints from a uniform array and larger rigs
from a float texture. A skin above 256 joints fails the load with
`glTF skin "<name>" has N joints; the maximum supported is 256`. Cap rigs at
128 joints or fewer for phones: every joint costs a matrix multiply per
skinned vertex. Instanced crowd batches still cap at 64 joints.

### Lights

`KHR_lights_punctual` lights import as scene lights: directional, point (with
`range`), and spot (with `innerConeAngle` and `outerConeAngle`). A light shines
down its node's -Z axis, so aim lights in Blender and export. Intensity is
passed through as authored (lux for directional, candela for point and spot)
and the scene clamps it to its 0 to 6 range. Blender exports sun strength and
point power in physical units, so expect to lower the Blender values or
re-tune intensity on the imported light.

### Compression

| Feature | Status |
|---|---|
| `KHR_mesh_quantization` | Supported. `gosx assets build` writes it. |
| `EXT_mesh_gpu_instancing` | Supported. |
| `KHR_texture_transform` | Supported, one transform per material (base color). |
| `EXT_meshopt_compression` | Rejected at load. Not supported yet. |
| Draco (`KHR_draco_mesh_compression`) | Rejected at load. |
| `KHR_texture_basisu` | Maps are dropped with a warning. Use the KTX2 files from `gosx assets build` through variants instead. |

## Build exports with `gosx assets build`

Run the build on every export. It reads each `.glb` and `.gltf` below the paths
you give, then writes to `--out`:

- `<name>.glb`: welded, reordered, and quantized, as a single file. A `.gltf`
  with external buffers becomes a GLB.
- `<name>.lod1.glb`, `<name>.lod2.glb`, ...: the LOD chain from quadric-error
  simplification.
- `<name>.textures/<image>.<tier>.<format>.ktx2`: KTX2 files with mip chains
  built in linear light, including BC7 (or BC5 for normal maps), for each
  embedded image.
- `assets.manifest.json`: per-model triangles, draw calls, joints, texture
  bytes, bounding radius, LOD switch distances, and texture variants.

```sh
gosx assets build --out public/assets \
  --max-triangles 30000 --max-draw-calls 40 --max-joints 128 --max-texture-mb 48 \
  art/exports
```

The command exits 1 when a model exceeds a budget. It still writes every file
and the manifest, so you can inspect the failure. `textureBytes` is the GPU
size of the images decoded to RGBA8 with mips, which is what the browser
uploads today. The KTX2 files are smaller on the GPU; the budget does not
count that saving until you serve them.

To list models and per-model budgets, pass a spec file:

```json
{
  "models": [
    {"src": "hero.glb", "name": "hero", "budget": {"maxTriangles": 60000}},
    {"src": "props/crate.glb"}
  ],
  "budget": {"maxTriangles": 20000, "maxJoints": 128},
  "lodRatios": [0.5, 0.25]
}
```

`gosx assets build art/assets.json` resolves `src` against the spec's
directory. The spec's top-level `budget` and `lodRatios` apply only when you pass no budget flags and no `--lods`.

### KTX2 textures in the GLB

Every built GLB (the main file and each LOD) lists its KTX2 files on the image
it came from, under the `GOSX_texture_variants` extension:

```json
"images": [{
  "bufferView": 4, "mimeType": "image/png",
  "extensions": {"GOSX_texture_variants": {"variants": [
    {"uri": "hero.textures/albedo-0.high.bc7-rgba-unorm-srgb.ktx2",
     "quality": "high", "format": "bc7-rgba-unorm-srgb",
     "requiredCapabilities": ["container:ktx2", "device-feature:texture-compression-bc", "..."]}
  ]}}
}]
```

The scene loader applies the same rule it uses for page-manifest texture
variants: a device that proved BC support loads the BC7 file, which takes a
quarter of the GPU memory. Any other device loads the original image, which
stays embedded in the GLB. URIs are relative to the GLB. No page manifest entry
is needed. Normal maps keep their original image, because the material shaders
read all three channels and BC5 stores two.

### LOD switch distances

Level 0 is the full model. For each reduced level the build writes the
distance at which it takes over: `scale * radius / sqrt(ratio)`, where
`radius` is the bounding radius of the model in world units, `ratio` is the
fraction of triangles that level kept, and `scale` is `--lod-distance-scale`
(default 4). This is a starting point, not a measured error bound. Tune the
scale until the swap is invisible in your game.

### Use the manifest in a scene

`scene.ModelLODGroup` turns a manifest entry into a `scene.LODGroup`:

```go
data, _ := os.ReadFile("public/assets/assets.manifest.json")
built, _ := manifest.Parse(data) // m31labs.dev/gosx/assetpipe/manifest
hero, _ := built.Model("hero")

group := scene.ModelLODGroup(hero, "/assets/", scene.Model{
    ID:         "hero",
    Position:   scene.Vector3{X: 2},
    CastShadow: true,
})
```

Add `group` to the scene like any other node. `manifest` imports the standard
library only, so a js/wasm game build can use it.

### What the build does not do yet

- It does not use `KHR_texture_basisu`. See "KTX2 textures in the GLB" below.
- It does not compress meshes with meshopt or Draco.
- It does not simplify primitives that carry morph targets or compression
  extensions, so such a model may have an empty LOD chain. The build prints a
  warning that names the reason.
