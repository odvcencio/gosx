# Appendix B — Byte layouts, buffers, bind groups, descriptors

All numbers are fixed. "Word" means a 4-byte lane. `F` is a `Float32Array` and
`U` a `Uint32Array`, both viewing the same `ArrayBuffer`.

## B1. Instance record `GDInstance` — 96 bytes (24 words), one per instance

Instances are packed mesh by mesh, in owned-mesh order. The record for global
instance `g` starts at byte `g * 96`.

| Words | Type | Field | Value written by the host |
|---|---|---|---|
| 0–15 | f32 | `model` (column-major mat4) | `transforms[j*16 .. j*16+15]` of the mesh's instance `j` |
| 16–19 | f32 | `color` (rgba) | `instancedMeshColorData(mesh, count)[j*4 .. j*4+3]` (white when no colours) |
| 20 | u32 | `meshIndex` | owned-mesh index `m` |
| 21 | u32 | `pickId` | 0 (reserved) |
| 22–23 | u32 | `_pad0`, `_pad1` | 0 |

## B2. Mesh record `GDMesh` — 48 bytes (12 words), one per owned mesh

| Words | Type | Field | Value |
|---|---|---|---|
| 0–3 | f32 | `sphere` | `(0, 0, 0, webGPUInstancedCullRadius(mesh))` |
| 4 | u32 | `firstInstance` | prefix sum of earlier meshes' counts |
| 5 | u32 | `instanceCount` | `instancedMeshCount(mesh)` |
| 6 | u32 | `argsBase` | `m * 4` (in args entries, not bytes) |
| 7 | u32 | `listBase` | prefix sum of `count * 4` over earlier meshes (in u32 elements) |
| 8 | u32 | `castShadow` | `mesh.castShadow ? 1 : 0` |
| 9 | u32 | `vertexCount` | `getInstancedGeometry(mesh).vertexCount` |
| 10–11 | u32 | pads | 0 |

## B3. Indirect draw args — 16 bytes per entry, 4 entries per owned mesh

Entry for mesh `m`, slot `s` starts at byte `(m * 4 + s) * 16` and holds
`[vertexCount, instanceCount, firstVertex = 0, firstInstance = 0]`. Every frame
the host rewrites the whole buffer with `queue.writeBuffer` from a template:
`[vertexCount_m, 0, 0, 0]` for all four slots of every mesh. `firstInstance` is
always 0, so the optional `indirect-first-instance` feature is never needed.

## B4. Visible lists — `u32` per entry

Mesh `m` owns `4 * count_m` elements starting at `listBase_m`. Slot `s` uses
elements `[listBase_m + s*count_m, listBase_m + (s+1)*count_m)`. For a draw:

- vertex-buffer byte offset = `(listBase_m + s * count_m) * 4`
- vertex-buffer byte size = `count_m * 4`
- indirect byte offset = `(m * 4 + s) * 16`

Worked example: two meshes with counts 3 and 5.
`firstInstance = [0, 3]`, `argsBase = [0, 4]`, `listBase = [0, 12]`,
visible buffer = `(3+5)*4` elements = 128 bytes, args = 2*4*16 = 128 bytes.
Mesh 1, slot 1: list byte offset `(12 + 5) * 4 = 68`, size `20`; args offset
`(4 + 1) * 16 = 80`.

## B5. Visibility — `u32` per instance (global index)

1 = drawn visible last frame. The whole buffer is written to 1s whenever the
layout changes (new mesh set, count, geometry or radius). That is
conservative: the next frame draws everything early.

## B6. View uniform `GDView` — 480 bytes used, 512-byte stride, 4 views

The views buffer is 2048 bytes. View `v` lives at byte `v * 512`: 0 = camera
early/single, 1 = camera late, 2 = light 0, 3 = light 1. Word index `k` below
is relative to the view's start (`F[base + k]`, where `base = v * 128`).

| Words | Type | Field | Camera views (0, 1) | Light views (2, 3) |
|---|---|---|---|---|
| 0–15 | f32 | `viewProj` | `scratchSelenaViewProjection` | the shadow `lightMatrix` passed to `renderShadowPass` |
| 16–39 | f32 | `planes[6]` (nx, ny, nz, d) | `sceneGPUDrivenFrustumPlanes(viewProj)` | same function on the light matrix |
| 40–43 | f32 | `viewport` | `(width, height, 1/width, 1/height)` of the main depth target | same values (unused) |
| 44–47 | f32 | `eye` | `(cam.x, cam.y, cam.z, maxDistance)` | `(0, 0, 0, 0)` |
| 48–51 | f32 | `params` | `(minPixelSize, 0, 0, 0)` | `(0, 0, 0, 0)` |
| 52 | u32 | `control.x` | total owned instances | same |
| 53 | u32 | `control.y` = slot | 0 (early) / 1 (late) | 2 / 3 |
| 54 | u32 | `control.z` = phase | 0 / 1 | 2 |
| 55 | u32 | `control.w` = Hi-Z level count | level count when occlusion is on, else 0 | 0 |
| 56–119 | u32 | `hzbLevels[16]` (offset, width, height, 0) | B9 levels, zero-filled past the last | zero |

In v1 `maxDistance` and `minPixelSize` are always 0. The kernel supports them;
exposing them is future work (see appendix C).

## B7. Hi-Z level uniform — 32 bytes (8 u32) at byte `k * 256`, for `k = 1..levels-1`

`[srcOffset, srcWidth, srcHeight, dstOffset, dstWidth, dstHeight, 0, 0]`, where
`src` is level `k-1` and `dst` is level `k` (B9). The buffer is `16 * 256 = 4096`
bytes. The bind group entry uses `{ buffer, offset: k * 256, size: 32 }`.

## B8. Hi-Z seed uniform — 16 bytes (4 u32)

`[level0Width, level0Height, depthWidth, depthHeight]`.

## B9. Hi-Z level math

```
level 0: width = ceil(W/2), height = ceil(H/2), offset = 0
level k: width = ceil(prevWidth/2), height = ceil(prevHeight/2), offset = sum of earlier sizes
stop after the level that is 1×1, or after 16 levels
```

`W×H` is the main depth target size (`scaledW × scaledH`). Example 320×240 →
9 levels: 160×120, 80×60, 40×30, 20×15, 10×8, 5×4, 3×2, 2×1, 1×1; total
25 609 floats. Reference implementation:

```js
  function sceneGPUDrivenHiZLevels(width, height) {
    var levels = [];
    var w = Math.max(1, Math.ceil(Math.max(1, width) / 2));
    var h = Math.max(1, Math.ceil(Math.max(1, height) / 2));
    var offset = 0;
    for (;;) {
      levels.push({ offset: offset, width: w, height: h });
      offset += w * h;
      if ((w === 1 && h === 1) || levels.length === 16) break;
      w = Math.ceil(w / 2);
      h = Math.ceil(h / 2);
    }
    return { levels: levels, total: offset };
  }
```

## B10. Buffers owned by the host

| Name | Label | Size (bytes) | Usage flags |
|---|---|---|---|
| instances | `gosx.gpu-driven.instances` | `capacityInstances * 96` | `STORAGE \| COPY_DST` |
| meshes | `gosx.gpu-driven.meshes` | `capacityMeshes * 48` | `STORAGE \| COPY_DST` |
| args | `gosx.gpu-driven.args` | `capacityMeshes * 64` | `STORAGE \| INDIRECT \| COPY_DST \| COPY_SRC` |
| visible | `gosx.gpu-driven.visible` | `capacityInstances * 16` | `STORAGE \| VERTEX` |
| visibility | `gosx.gpu-driven.visibility` | `capacityInstances * 4` | `STORAGE \| COPY_DST` |
| views | `gosx.gpu-driven.views` | 2048 | `UNIFORM \| COPY_DST` |
| hzb | `gosx.gpu-driven.hzb` | `max(16, totalFloats * 4)` | `STORAGE` |
| hzbLevels | `gosx.gpu-driven.hzb-levels` | 4096 | `UNIFORM \| COPY_DST` |
| hzbSeed | `gosx.gpu-driven.hzb-seed` | 16 | `UNIFORM \| COPY_DST` |
| readback | `gosx.gpu-driven.readback` | same as args | `MAP_READ \| COPY_DST` (created lazily by `finishEncoding`) |

Capacities grow, never shrink:
`capacityInstances = max(64, ceil(needed * 1.25))` and
`capacityMeshes = max(4, ceil(meshes * 1.25))`. `hzb` is created with 16 bytes
when occlusion has not been used yet, because the cull bind group always needs
binding 6.

## B11. Bind group layouts

Cull (`label: "gosx-gpu-driven-cull"`), all `visibility: GPUShaderStage.COMPUTE`:

| Binding | Entry | Resource |
|---|---|---|
| 0 | `{ buffer: { type: "uniform" } }` | `{ buffer: views, offset: v * 512, size: 480 }` |
| 1 | `{ buffer: { type: "read-only-storage" } }` | `{ buffer: instances }` |
| 2 | `{ buffer: { type: "read-only-storage" } }` | `{ buffer: meshes }` |
| 3 | `{ buffer: { type: "storage" } }` | `{ buffer: args }` |
| 4 | `{ buffer: { type: "storage" } }` | `{ buffer: visible }` |
| 5 | `{ buffer: { type: "storage" } }` | `{ buffer: visibility }` |
| 6 | `{ buffer: { type: "read-only-storage" } }` | `{ buffer: hzb }` |

Four cull bind groups exist, one per view `v = 0..3`. They are rebuilt whenever
any bound buffer is reallocated.

Hi-Z downsample (`label: "gosx-gpu-driven-hiz-downsample"`): binding 0
`{ buffer: { type: "uniform" } }`; binding 1 `{ buffer: { type: "storage" } }`.
One bind group per level `k >= 1`: `[{ buffer: hzbLevels, offset: k*256,
size: 32 }, { buffer: hzb }]`.

Hi-Z seed: see A5 (`label: "gosx-gpu-driven-hiz-seed"` and
`"gosx-gpu-driven-hiz-seed-ms"`).

Instances for drawing (`label: "gosx-gpu-driven-instances"`): binding 0
`{ visibility: GPUShaderStage.VERTEX, buffer: { type: "read-only-storage" } }`,
resource `{ buffer: instances }`. The same bind group serves as group 2 of
the PBR pipeline and group 1 of the shadow pipeline. That works because both
pipeline layouts reference this same layout object.

## B12. Pipelines

Compute (all `createComputePipelineAsync`):

| Label | Layout | Module | Entry |
|---|---|---|---|
| `gosx-gpu-driven-cull` | `[cullBGL]` | `SCENE_GPU_DRIVEN_CULL_WGSL` | `cull` |
| `gosx-gpu-driven-hiz-downsample` | `[downsampleBGL]` | `SCENE_GPU_DRIVEN_HIZ_DOWNSAMPLE_WGSL` | `downsample` |
| `gosx-gpu-driven-hiz-seed` | `[seedBGL]` | `SCENE_GPU_DRIVEN_HIZ_SEED_WGSL` | `seed` |
| `gosx-gpu-driven-hiz-seed-ms` | `[seedMSBGL]` | `SCENE_GPU_DRIVEN_HIZ_SEED_MS_WGSL` | `seed` |

Render pipelines are created with `createRenderPipelineAsync` (or, when the
device lacks it, `createRenderPipeline` inside a resolved promise). The host
owns nothing until every pipeline it needs has resolved; until then
`beginFrame` returns false with reason `warming`, and the classic path draws.

| Label | Cache key | Bind group layouts | Vertex buffers | Fragment | Other state |
|---|---|---|---|---|---|
| `gosx-gpu-driven-pbr` | `"pbr|" + targetFormat + "|" + sampleCount + "|" + (depthWrite ? 1 : 0)` | `[frame, material, instances]` | `hooks.pbrVertexLayout` (locations 0–3) + the slot layout below | `hooks.pbrFragmentWGSL`, entry `fragmentMain`, target `{ format: targetFormat, blend: hooks.blendState("opaque") }` | `triangle-list`, `cullMode: "none"`, `multisample.count = sampleCount`, depth `depth24plus`, `less-equal`, write = depthWrite |
| `gosx-gpu-driven-shadow` | `"shadow"` | `[shadow, instances]` | `hooks.shadowVertexLayout[0]` + the slot layout | `hooks.shadowFragmentWGSL`, entry `fragmentMain`, `targets: []` | `triangle-list`, `cullMode: "none"`, `frontFace: "ccw"`, depth `depth24plus`, `less-equal`, write true |

Slot layout (location 4, one `u32` survivor index per instance):
`{ arrayStride: 4, stepMode: "instance", attributes: [{ format: "uint32", offset: 0, shaderLocation: 4 }] }`.

The `frame`, `material` and `shadow` layouts are **the host's own objects**,
made by calling the renderer's module-scope factories
(`wgpuCreateFrameBindGroupLayout`, `wgpuCreateMaterialBindGroupLayout`,
`wgpuCreateShadowBindGroupLayout`) passed in `hooks`. WebGPU treats layouts
built from identical descriptors as group-equivalent, so the renderer's own
frame, material and shadow bind groups bind to the host's pipelines. The host
never reads the renderer's `var x = null` closure variables, because each
such read would add a noImplicitAny diagnostic to `webgpu.ts` (00 §7). The
real-WebGPU probe (G09) proved this binding works on Chromium.

The two pipelines mirror the renderer's `wgpuCreatePBRInstancedPipeline` and
`wgpuCreateShadowInstancedPipeline`. Only the instance inputs differ.

## B13. Draw sequences (exact call order)

Camera draw of owned mesh `m`, slot `s` (0 early/single, 1 late). The caller
has already bound group 0 (frame) and group 1 (material):

```js
pass.setPipeline(pbrPipeline(targetFormat, sampleCount, depthWrite));
pass.setBindGroup(2, instancesBindGroup);
pass.setVertexBuffer(0, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedPositionBuffer", geom.positions));
pass.setVertexBuffer(1, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedNormalBuffer", geom.normals));
pass.setVertexBuffer(2, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedUVBuffer", geom.uvs));
pass.setVertexBuffer(3, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedTangentBuffer", geom.tangents));
pass.setVertexBuffer(4, visible, (listBase + s * count) * 4, count * 4);
pass.drawIndirect(args, (argsBase + s) * 16);
```

Shadow draw of owned mesh `m` for light slot `L` (0 or 1), list slot `2 + L`.
The caller has already bound group 0 (shadow uniform at its dynamic offset):

```js
pass.setPipeline(shadowPipeline());
pass.setBindGroup(1, instancesBindGroup);
pass.setVertexBuffer(0, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedShadowPositionBuffer", geom.positions));
pass.setVertexBuffer(1, visible, (listBase + (2 + L) * count) * 4, count * 4);
pass.drawIndirect(args, (argsBase + 2 + L) * 16);
```

The slot names are the ones the classic instanced path already uses, so
geometry GPU buffers are shared, not duplicated.

## B14. Dispatch sizes

- Cull: `dispatchWorkgroups(Math.ceil(totalInstances / 64))`, one compute pass
  per view, label `gosx-gpu-driven-cull`.
- Hi-Z: one compute pass, label `gosx-gpu-driven-hiz`. Seed
  `dispatchWorkgroups(ceil(w0 / 8), ceil(h0 / 8))`, then each level `k >= 1`
  `dispatchWorkgroups(ceil(wk * hk / 64))`.

## B15. Limits

- Max owned instances per frame:
  `min(4194240, floor(maxStorageBufferBindingSize / 96))`, where 4194240 is
  `65535 * 64`, the 1-D dispatch limit. When `device.limits` lacks a value,
  use the WebGPU default (`maxStorageBufferBindingSize` = 134217728, so
  1398101 instances).
- The cull kernel binds 6 storage buffers (default
  `maxStorageBuffersPerShaderStage` is 8). The vertex stage binds 1. When
  `device.limits.maxStorageBuffersInVertexStage` is a number below 1, the host
  must stay inactive (reason `unsupported`).
