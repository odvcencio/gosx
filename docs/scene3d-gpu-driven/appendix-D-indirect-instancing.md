# Appendix D — `client/runtime/scene3d/indirect-instancing.ts` (verbatim)

This is the complete file task G05 creates. It was built and tested in a
scratch checkout of the baseline commit with tasks G01–G04 applied: the fake
device tests in G05 and G06 pass, and the real-WebGPU probe in G09 renders
pixel-identical frames to the classic path with zero validation errors.

- Lines: 1270
- SHA-256 of the file: `e90ab552fac1f0485cee90c11f54574fa1a99f449e3f15f02d7a569aa52e3cc0`

Copy it exactly. The fastest safe way is to copy the fenced block below into
the file and then check the hash:

```sh
sha256sum client/runtime/scene3d/indirect-instancing.ts
```

If the hash differs, diff your file against this block; do not "fix" the
block. The four `SCENE_GPU_DRIVEN_*_WGSL` arrays can also be regenerated with
the A8 one-liner from `client/js/testdata/elio/*.wgsl` (cull and downsample)
and from appendix A5 (the two seed shaders).

Why the file name has no "gpu" in it: `client/js/scene3d-renderer-source-set.js`
treats any `client/runtime/scene3d/*.ts` whose name contains `gpu-` or the
word `gpu` as an extracted WebGPU renderer source and rejects it unless it is
named `webgpu*.ts`. This file is not a renderer source; it ships in the
compute chunk. `indirect-instancing.ts` passes that check.

Map of the file:

| Section | What it holds | Spec reference |
|---|---|---|
| Header comment | purpose, slots, where the kernels come from | — |
| `SCENE_GPU_DRIVEN_CULL_WGSL` | Elio golden `gpudriven_cull.wgsl` | A3 |
| `SCENE_GPU_DRIVEN_HIZ_DOWNSAMPLE_WGSL` | Elio golden `gpudriven_hiz_downsample.wgsl` | A4 |
| `SCENE_GPU_DRIVEN_HIZ_SEED_WGSL`, `..._SEED_MS_WGSL` | hand-written seed shaders | A5 |
| `SCENE_GPU_DRIVEN_INSTANCE_STRUCT_WGSL`, derivations | vertex pulling | A6, A7 |
| Constants | record sizes, view stride, slot layout | B1–B7 |
| `sceneGPUDrivenConfig`, `sceneGPUDrivenMeshEligible` | mode + ownership rule | — |
| `sceneGPUDrivenHiZLevels`, `sceneGPUDrivenFrustumPlanes`, `sceneGPUDrivenPackView`, `sceneGPUDrivenMaxInstances` | pure helpers | B6, B9, B15 |
| `createSceneGPUDrivenHost` | the host: layouts, pipelines, buffers, uploads, culls, draws, occlusion split, readback, telemetry | B10–B14 |
| `Object.assign(window.__gosx_scene3d_api, …)` | exports | — |

Host methods (the contract `webgpu.ts` calls; wired by G06):

| Method | Called from | Returns |
|---|---|---|
| `beginFrame(bundle, encoder, frame)` | `render()`, before `updateInstancedCullSystems` | true when the host owns meshes this frame |
| `owns(mesh)` | `updateInstancedCullSystems` | skip the per-mesh cull system for owned meshes |
| `drawMesh(pass, mesh, depthWrite)` | `drawInstancedMeshes`, after group 1 is bound | true when it drew the mesh |
| `lightView(encoder, lightMatrix, lightSlot)` | shadow loop, before `renderShadowPass` | true when it culled that light |
| `drawShadowMesh(pass, mesh, lightSlot)` | `drawInstancedShadowMeshes` | true when it drew the casters |
| `splitsMainPass()` | render-bundle eligibility flags | true on an occlusion frame |
| `prepareMainPass(descriptor)` | before `encoder.beginRenderPass(mainPassDescriptor)` | strips resolve target + end timestamp on occlusion frames |
| `splitMainPass(encoder, pass, descriptor, frameBindGroup, materials, opaque)` | end of the opaque section of the direct branch | the pass to keep drawing into |
| `finishEncoding(encoder)` | before `device.queue.submit` | copies the args for telemetry |
| `endFrame(mount)` | after submit | stats object; publishes `data-gosx-scene3d-webgpu-gpu-driven-*` |
| `stats()`, `dispose()` | diagnostics, renderer dispose | — |

`frame` for `beginFrame`: `{ viewProjection, camera, width, height,
sampleCount, targetFormat, opaque }`. `opaque` is the renderer's
`buildInstancedDrawList(bundle, materials).opaque`.

Published attributes (all prefixed `data-gosx-scene3d-webgpu-gpu-driven-`):
`active`, `reason` (`off`, `warming`, `frustum`, `occlusion`,
`no-eligible-meshes`, `unsupported`, `pipeline-failed:<name>`,
`disposed`), `meshes`, `instances`, `occlusion`, `dispatches`,
`uploads`, `camera-visible`, `late-visible`, `shadow-casters`. The last
three come from an asynchronous readback, so they lag a frame or two.

## The file

```js
  // indirect-instancing.ts — GPU-driven instancing for the Scene3D WebGPU renderer.
  //
  // With bundle.gpuDriven set, the WebGPU renderer hands every eligible opaque
  // InstancedMesh to one host made here. The host keeps all their instance
  // records in one storage buffer, culls every instance of a view with one
  // compute dispatch, and draws each mesh with drawIndirect from a list of
  // survivor indices that the vertex shader resolves to a record.
  //
  // Views and slots: every owned mesh has four survivor lists and four
  // indirect-args entries. Slot 0 is the camera (early phase when occlusion is
  // on), slot 1 the camera late phase, slots 2 and 3 the two shadow lights.
  //
  // The cull and Hi-Z downsample kernels are Elio sources (odvcencio/elio,
  // stdlib/gpudriven). The embedded WGSL must equal the goldens in
  // client/js/testdata/elio/; scene3d-gpu-driven.test.js checks it. Byte
  // layouts: docs/scene3d-gpu-driven/appendix-B-layouts.md.

  var SCENE_GPU_DRIVEN_CULL_WGSL = [
    "struct GDInstance {",
    "  model : mat4x4<f32>,",
    "  color : vec4<f32>,",
    "  meshIndex : u32,",
    "  pickId : u32,",
    "  _pad0 : u32,",
    "  _pad1 : u32,",
    "};",
    "",
    "struct GDMesh {",
    "  sphere : vec4<f32>,",
    "  firstInstance : u32,",
    "  instanceCount : u32,",
    "  argsBase : u32,",
    "  listBase : u32,",
    "  castShadow : u32,",
    "  vertexCount : u32,",
    "  _pad0 : u32,",
    "  _pad1 : u32,",
    "};",
    "",
    "struct GDView {",
    "  viewProj : mat4x4<f32>,",
    "  planes : array<vec4<f32>, 6>,",
    "  viewport : vec4<f32>,",
    "  eye : vec4<f32>,",
    "  params : vec4<f32>,",
    "  control : vec4<u32>,",
    "  hzbLevels : array<vec4<u32>, 16>,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> gdView : GDView;",
    "@group(0) @binding(1) var<storage, read> gdInstances : array<GDInstance>;",
    "@group(0) @binding(2) var<storage, read> gdMeshes : array<GDMesh>;",
    "@group(0) @binding(3) var<storage, read_write> gdArgs : array<atomic<u32>>;",
    "@group(0) @binding(4) var<storage, read_write> gdVisible : array<u32>;",
    "@group(0) @binding(5) var<storage, read_write> gdVisibility : array<u32>;",
    "@group(0) @binding(6) var<storage, read> gdHzb : array<f32>;",
    "",
    "@compute @workgroup_size(64)",
    "fn cull(@builtin(global_invocation_id) gid : vec3<u32>) {",
    "  let i = gid.x;",
    "  if ((i >= gdView.control.x)) {",
    "    return;",
    "  }",
    "  let phase = gdView.control.z;",
    "  let slot = gdView.control.y;",
    "  let rec = gdInstances[i];",
    "  let gm = gdMeshes[rec.meshIndex];",
    "  let m = rec.model;",
    "  let ls = gm.sphere;",
    "  let center = ((((m[0].xyz * ls.x) + (m[1].xyz * ls.y)) + (m[2].xyz * ls.z)) + m[3].xyz);",
    "  let c0 = m[0].xyz;",
    "  let c1 = m[1].xyz;",
    "  let c2 = m[2].xyz;",
    "  let l0 = dot(c0, c0);",
    "  let l1 = dot(c1, c1);",
    "  let l2 = dot(c2, c2);",
    "  let d01 = dot(c0, c1);",
    "  let d02 = dot(c0, c2);",
    "  let d12 = dot(c1, c2);",
    "  var scale2 = ((l0 + l1) + l2);",
    "  if (((d01 * d01) <= ((0.00000001 * l0) * l1))) {",
    "    if (((d02 * d02) <= ((0.00000001 * l0) * l2))) {",
    "      if (((d12 * d12) <= ((0.00000001 * l1) * l2))) {",
    "        scale2 = max(l0, max(l1, l2));",
    "      }",
    "    }",
    "  }",
    "  let scale = sqrt(scale2);",
    "  var radius = ls.w;",
    "  if ((scale > 0.0)) {",
    "    radius = (ls.w * scale);",
    "  }",
    "  var keep = true;",
    "  if ((phase == 2u)) {",
    "    if ((gm.castShadow == 0u)) {",
    "      keep = false;",
    "    }",
    "  }",
    "  for (var p : i32 = 0; (p < 6); p = (p + 1)) {",
    "    let plane = gdView.planes[p];",
    "    if (((dot(plane.xyz, center) + plane.w) < -radius)) {",
    "      keep = false;",
    "      break;",
    "    }",
    "  }",
    "  if ((phase < 2u)) {",
    "    if (keep) {",
    "      if ((gdView.eye.w > 0.0)) {",
    "        if (((distance(center, gdView.eye.xyz) - radius) > gdView.eye.w)) {",
    "          keep = false;",
    "        }",
    "      }",
    "    }",
    "    var testRect = false;",
    "    if (keep) {",
    "      if ((gdView.params.x > 0.0)) {",
    "        testRect = true;",
    "      }",
    "      if ((phase == 1u)) {",
    "        if ((gdView.control.w > 0u)) {",
    "          testRect = true;",
    "        }",
    "      }",
    "    }",
    "    if (testRect) {",
    "      let vp = gdView.viewProj;",
    "      var behind = false;",
    "      var firstCorner = true;",
    "      var minX = 0.0;",
    "      var minY = 0.0;",
    "      var maxX = 0.0;",
    "      var maxY = 0.0;",
    "      var minZ = 0.0;",
    "      for (var c : u32 = 0u; (c < 8u); c = (c + 1u)) {",
    "        var sx = -1.0;",
    "        if (((c % 2u) == 1u)) {",
    "          sx = 1.0;",
    "        }",
    "        var sy = -1.0;",
    "        if ((((c / 2u) % 2u) == 1u)) {",
    "          sy = 1.0;",
    "        }",
    "        var sz = -1.0;",
    "        if ((((c / 4u) % 2u) == 1u)) {",
    "          sz = 1.0;",
    "        }",
    "        let clip = ((((vp[0] * (center.x + (sx * radius))) + (vp[1] * (center.y + (sy * radius)))) + (vp[2] * (center.z + (sz * radius)))) + vp[3]);",
    "        if ((clip.w <= 0.0001)) {",
    "          behind = true;",
    "        } else {",
    "          let nx = (clip.x / clip.w);",
    "          let ny = (clip.y / clip.w);",
    "          let nz = (clip.z / clip.w);",
    "          if (firstCorner) {",
    "            minX = nx;",
    "            maxX = nx;",
    "            minY = ny;",
    "            maxY = ny;",
    "            minZ = nz;",
    "            firstCorner = false;",
    "          } else {",
    "            minX = min(minX, nx);",
    "            maxX = max(maxX, nx);",
    "            minY = min(minY, ny);",
    "            maxY = max(maxY, ny);",
    "            minZ = min(minZ, nz);",
    "          }",
    "        }",
    "      }",
    "      if (!behind) {",
    "        let w = gdView.viewport.x;",
    "        let h = gdView.viewport.y;",
    "        let x0 = clamp((((minX * 0.5) + 0.5) * w), 0.0, w);",
    "        let x1 = clamp((((maxX * 0.5) + 0.5) * w), 0.0, w);",
    "        let y0 = clamp(((0.5 - (maxY * 0.5)) * h), 0.0, h);",
    "        let y1 = clamp(((0.5 - (minY * 0.5)) * h), 0.0, h);",
    "        let extent = max((x1 - x0), (y1 - y0));",
    "        if ((gdView.params.x > 0.0)) {",
    "          if ((extent < gdView.params.x)) {",
    "            keep = false;",
    "          }",
    "        }",
    "        if ((phase == 1u)) {",
    "          if ((gdView.control.w > 0u)) {",
    "            if (keep) {",
    "              var level = u32(max((ceil(log2(max(extent, 1.0))) - 1.0), 0.0));",
    "              if ((level >= gdView.control.w)) {",
    "                level = (gdView.control.w - 1u);",
    "              }",
    "              let lv = gdView.hzbLevels[level];",
    "              let texel = exp2((f32(level) + 1.0));",
    "              let tx0 = min(u32((x0 / texel)), (lv.y - 1u));",
    "              let tx1 = min(u32((x1 / texel)), (lv.y - 1u));",
    "              let ty0 = min(u32((y0 / texel)), (lv.z - 1u));",
    "              let ty1 = min(u32((y1 / texel)), (lv.z - 1u));",
    "              let z00 = gdHzb[((lv.x + (ty0 * lv.y)) + tx0)];",
    "              let z10 = gdHzb[((lv.x + (ty0 * lv.y)) + tx1)];",
    "              let z01 = gdHzb[((lv.x + (ty1 * lv.y)) + tx0)];",
    "              let z11 = gdHzb[((lv.x + (ty1 * lv.y)) + tx1)];",
    "              let farthest = max(max(z00, z10), max(z01, z11));",
    "              if ((minZ > farthest)) {",
    "                keep = false;",
    "              }",
    "            }",
    "          }",
    "        }",
    "      }",
    "    }",
    "  }",
    "  if ((phase == 1u)) {",
    "    let prev = gdVisibility[i];",
    "    if (keep) {",
    "      gdVisibility[i] = 1u;",
    "    } else {",
    "      gdVisibility[i] = 0u;",
    "    }",
    "    if ((prev == 1u)) {",
    "      keep = false;",
    "    }",
    "  }",
    "  if ((phase == 0u)) {",
    "    if ((gdView.control.w > 0u)) {",
    "      if ((gdVisibility[i] == 0u)) {",
    "        keep = false;",
    "      }",
    "    }",
    "  }",
    "  if (keep) {",
    "    let n = atomicAdd(&gdArgs[(((gm.argsBase + slot) * 4u) + 1u)], 1u);",
    "    gdVisible[((gm.listBase + (slot * gm.instanceCount)) + n)] = i;",
    "  }",
    "}",
  ].join("\n");

  var SCENE_GPU_DRIVEN_HIZ_DOWNSAMPLE_WGSL = [
    "struct HiZLevel {",
    "  srcOffset : u32,",
    "  srcWidth : u32,",
    "  srcHeight : u32,",
    "  dstOffset : u32,",
    "  dstWidth : u32,",
    "  dstHeight : u32,",
    "  _pad0 : u32,",
    "  _pad1 : u32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> gdLevel : HiZLevel;",
    "@group(0) @binding(1) var<storage, read_write> gdHzb : array<f32>;",
    "",
    "@compute @workgroup_size(64)",
    "fn downsample(@builtin(global_invocation_id) gid : vec3<u32>) {",
    "  let i = gid.x;",
    "  if ((i >= (gdLevel.dstWidth * gdLevel.dstHeight))) {",
    "    return;",
    "  }",
    "  let dx = (i % gdLevel.dstWidth);",
    "  let dy = (i / gdLevel.dstWidth);",
    "  let sx0 = (dx * 2u);",
    "  let sy0 = (dy * 2u);",
    "  let sx1 = min((sx0 + 1u), (gdLevel.srcWidth - 1u));",
    "  let sy1 = min((sy0 + 1u), (gdLevel.srcHeight - 1u));",
    "  let a = gdHzb[((gdLevel.srcOffset + (sy0 * gdLevel.srcWidth)) + sx0)];",
    "  let b = gdHzb[((gdLevel.srcOffset + (sy0 * gdLevel.srcWidth)) + sx1)];",
    "  let c = gdHzb[((gdLevel.srcOffset + (sy1 * gdLevel.srcWidth)) + sx0)];",
    "  let d = gdHzb[((gdLevel.srcOffset + (sy1 * gdLevel.srcWidth)) + sx1)];",
    "  gdHzb[(gdLevel.dstOffset + i)] = max(max(a, b), max(c, d));",
    "}",
  ].join("\n");

  var SCENE_GPU_DRIVEN_HIZ_SEED_WGSL = [
    "struct GDHiZSeed {",
    "  dstWidth: u32,",
    "  dstHeight: u32,",
    "  srcWidth: u32,",
    "  srcHeight: u32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> gdSeed: GDHiZSeed;",
    "@group(0) @binding(1) var gdDepth: texture_depth_2d;",
    "@group(0) @binding(2) var<storage, read_write> gdHzb: array<f32>;",
    "",
    "@compute @workgroup_size(8, 8)",
    "fn seed(@builtin(global_invocation_id) gid: vec3<u32>) {",
    "  if (gid.x >= gdSeed.dstWidth || gid.y >= gdSeed.dstHeight) { return; }",
    "  let sx0 = gid.x * 2u;",
    "  let sy0 = gid.y * 2u;",
    "  let sx1 = min(sx0 + 1u, gdSeed.srcWidth - 1u);",
    "  let sy1 = min(sy0 + 1u, gdSeed.srcHeight - 1u);",
    "  let a = textureLoad(gdDepth, vec2<u32>(sx0, sy0), 0);",
    "  let b = textureLoad(gdDepth, vec2<u32>(sx1, sy0), 0);",
    "  let c = textureLoad(gdDepth, vec2<u32>(sx0, sy1), 0);",
    "  let d = textureLoad(gdDepth, vec2<u32>(sx1, sy1), 0);",
    "  gdHzb[gid.y * gdSeed.dstWidth + gid.x] = max(max(a, b), max(c, d));",
    "}",
  ].join("\n");

  var SCENE_GPU_DRIVEN_HIZ_SEED_MS_WGSL = [
    "struct GDHiZSeed {",
    "  dstWidth: u32,",
    "  dstHeight: u32,",
    "  srcWidth: u32,",
    "  srcHeight: u32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> gdSeed: GDHiZSeed;",
    "@group(0) @binding(1) var gdDepth: texture_depth_multisampled_2d;",
    "@group(0) @binding(2) var<storage, read_write> gdHzb: array<f32>;",
    "",
    "fn gdPixelMax(p: vec2<u32>) -> f32 {",
    "  var d = 0.0;",
    "  let n = textureNumSamples(gdDepth);",
    "  for (var s = 0u; s < n; s = s + 1u) {",
    "    d = max(d, textureLoad(gdDepth, p, s));",
    "  }",
    "  return d;",
    "}",
    "",
    "@compute @workgroup_size(8, 8)",
    "fn seed(@builtin(global_invocation_id) gid: vec3<u32>) {",
    "  if (gid.x >= gdSeed.dstWidth || gid.y >= gdSeed.dstHeight) { return; }",
    "  let sx0 = gid.x * 2u;",
    "  let sy0 = gid.y * 2u;",
    "  let sx1 = min(sx0 + 1u, gdSeed.srcWidth - 1u);",
    "  let sy1 = min(sy0 + 1u, gdSeed.srcHeight - 1u);",
    "  let a = gdPixelMax(vec2<u32>(sx0, sy0));",
    "  let b = gdPixelMax(vec2<u32>(sx1, sy0));",
    "  let c = gdPixelMax(vec2<u32>(sx0, sy1));",
    "  let d = gdPixelMax(vec2<u32>(sx1, sy1));",
    "  gdHzb[gid.y * gdSeed.dstWidth + gid.x] = max(max(a, b), max(c, d));",
    "}",
  ].join("\n");

  var SCENE_GPU_DRIVEN_INSTANCE_STRUCT_WGSL = [
    "struct GDInstance {",
    "    model: mat4x4f,",
    "    color: vec4f,",
    "    meshIndex: u32,",
    "    pickId: u32,",
    "    _pad0: u32,",
    "    _pad1: u32,",
    "};",
  ].join("\n");

  var SCENE_GPU_DRIVEN_INSTANCE_BYTES = 96;
  var SCENE_GPU_DRIVEN_MESH_BYTES = 48;
  var SCENE_GPU_DRIVEN_VIEW_STRIDE = 512;
  var SCENE_GPU_DRIVEN_VIEW_BYTES = 480;
  var SCENE_GPU_DRIVEN_LEVEL_STRIDE = 256;
  var SCENE_GPU_DRIVEN_MAX_LEVELS = 16;
  // The instance-rate attribute that carries one survivor index per instance.
  var SCENE_GPU_DRIVEN_SLOT_LAYOUT = {
    arrayStride: 4,
    stepMode: "instance",
    attributes: [{ format: "uint32", offset: 0, shaderLocation: 4 }],
  };

  // sceneGPUDrivenReplace swaps one exact anchor and throws when it is
  // missing, so a renderer edit that moves an anchor fails loudly in tests
  // instead of shipping a shader that still reads the old instance inputs.
  function sceneGPUDrivenReplace(source, anchor, replacement, label) {
    if (typeof source !== "string" || source.indexOf(anchor) < 0) {
      throw new Error("gpu-driven: vertex shader anchor missing: " + label);
    }
    return source.replace(anchor, replacement);
  }

  function sceneGPUDrivenPBRVertexWGSL(base) {
    var s = sceneGPUDrivenReplace(base, [
      "    @location(4) instanceMatrix0: vec4f,",
      "    @location(5) instanceMatrix1: vec4f,",
      "    @location(6) instanceMatrix2: vec4f,",
      "    @location(7) instanceMatrix3: vec4f,",
      "    @location(8) instanceColor: vec4f,",
    ].join("\n"), "    @location(4) gdSlot: u32,", "pbr inputs");
    s = sceneGPUDrivenReplace(s, "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
      "@group(0) @binding(0) var<uniform> frame: FrameUniforms;\n" + SCENE_GPU_DRIVEN_INSTANCE_STRUCT_WGSL +
      "\n@group(2) @binding(0) var<storage, read> gdInstances: array<GDInstance>;", "pbr binding");
    s = sceneGPUDrivenReplace(s, "    let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
      "    let gdInstance = gdInstances[in.gdSlot];\n    let model = gdInstance.model;", "pbr model");
    s = sceneGPUDrivenReplace(s, "    out.instanceColor = in.instanceColor;",
      "    out.instanceColor = gdInstance.color;", "pbr color");
    return s;
  }

  function sceneGPUDrivenShadowVertexWGSL(base) {
    var s = sceneGPUDrivenReplace(base, [
      "    @location(4) instanceMatrix0: vec4f,",
      "    @location(5) instanceMatrix1: vec4f,",
      "    @location(6) instanceMatrix2: vec4f,",
      "    @location(7) instanceMatrix3: vec4f,",
    ].join("\n"), "    @location(4) gdSlot: u32,", "shadow inputs");
    s = sceneGPUDrivenReplace(s, "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;",
      "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;\n" + SCENE_GPU_DRIVEN_INSTANCE_STRUCT_WGSL +
      "\n@group(1) @binding(0) var<storage, read> gdInstances: array<GDInstance>;", "shadow binding");
    s = sceneGPUDrivenReplace(s, "    let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
      "    let model = gdInstances[in.gdSlot].model;", "shadow model");
    return s;
  }

  // sceneGPUDrivenConfig normalizes bundle.gpuDriven. Null means the scene
  // did not opt in.
  function sceneGPUDrivenConfig(raw) {
    if (!raw || typeof raw !== "object") return null;
    return { occlusion: raw.occlusion === true, shadowCulling: raw.shadowCulling !== false };
  }

  // sceneGPUDrivenMeshEligible reports whether the host may own one mesh of
  // the renderer's opaque instanced list. A mesh with an authored cull kernel
  // keeps it. The id keys every per-mesh record, because the render bundle
  // hands the renderer a fresh copy of each mesh every frame.
  function sceneGPUDrivenMeshEligible(mesh, count) {
    if (!mesh || typeof mesh.id !== "string" || mesh.id === "") return false;
    if (typeof mesh.cullKernelWGSL === "string" && mesh.cullKernelWGSL.trim() !== "") return false;
    if (!(count > 0)) return false;
    var transforms = mesh.transforms;
    return !!transforms && typeof transforms.length === "number" && transforms.length >= count * 16;
  }

  // sceneGPUDrivenHiZLevels lays out the Hi-Z pyramid for a depth target of
  // width x height: level 0 is the target reduced 2x2, each next level halves
  // again (rounding up) until 1x1 or 16 levels. Levels pack into one float
  // buffer, level k starting at levels[k].offset.
  function sceneGPUDrivenHiZLevels(width, height) {
    var levels = [];
    var w = Math.max(1, Math.ceil(Math.max(1, width) / 2));
    var h = Math.max(1, Math.ceil(Math.max(1, height) / 2));
    var offset = 0;
    for (;;) {
      levels.push({ offset: offset, width: w, height: h });
      offset += w * h;
      if ((w === 1 && h === 1) || levels.length === SCENE_GPU_DRIVEN_MAX_LEVELS) break;
      w = Math.ceil(w / 2);
      h = Math.ceil(h / 2);
    }
    return { levels: levels, total: offset };
  }

  // sceneGPUDrivenFrustumPlanes writes the six normalized planes of a
  // column-major WebGPU view-projection (clip z in [0, 1]) into
  // out[offset .. offset+23]: left, right, bottom, top, near, far. A point p
  // is inside a plane when dot(n, p) + d >= 0. Same rows as
  // extractFrustumPlanesJS in 11-scene-math.ts.
  function sceneGPUDrivenFrustumPlanes(m, out, offset) {
    var base = offset || 0;
    function put(p, x, y, z, w) {
      var len = Math.sqrt(x * x + y * y + z * z);
      var inv = len > 0 ? 1 / len : 1;
      out[base + p * 4] = x * inv;
      out[base + p * 4 + 1] = y * inv;
      out[base + p * 4 + 2] = z * inv;
      out[base + p * 4 + 3] = w * inv;
    }
    put(0, m[3] + m[0], m[7] + m[4], m[11] + m[8], m[15] + m[12]);
    put(1, m[3] - m[0], m[7] - m[4], m[11] - m[8], m[15] - m[12]);
    put(2, m[3] + m[1], m[7] + m[5], m[11] + m[9], m[15] + m[13]);
    put(3, m[3] - m[1], m[7] - m[5], m[11] - m[9], m[15] - m[13]);
    put(4, m[2], m[6], m[10], m[14]);
    put(5, m[3] - m[2], m[7] - m[6], m[11] - m[10], m[15] - m[14]);
    return out;
  }

  // sceneGPUDrivenPackView writes one GDView (480 bytes, appendix B6) at word
  // index base of f32 and u32, two views of one ArrayBuffer. view carries
  // viewProj, passAll, width, height, eye {x,y,z}, maxDistance, minPixel,
  // total, slot, phase and levels (null, or the Hi-Z level list to enable).
  function sceneGPUDrivenPackView(f32, u32, base, view) {
    var vp = view.viewProj;
    for (var k = 0; k < 16; k++) f32[base + k] = vp[k];
    if (view.passAll) {
      for (var p = 0; p < 6; p++) {
        f32[base + 16 + p * 4] = 0;
        f32[base + 17 + p * 4] = 0;
        f32[base + 18 + p * 4] = 0;
        f32[base + 19 + p * 4] = 1;
      }
    } else {
      sceneGPUDrivenFrustumPlanes(vp, f32, base + 16);
    }
    var width = view.width > 0 ? view.width : 1;
    var height = view.height > 0 ? view.height : 1;
    f32[base + 40] = width;
    f32[base + 41] = height;
    f32[base + 42] = 1 / width;
    f32[base + 43] = 1 / height;
    var eye = view.eye || null;
    f32[base + 44] = eye ? Number(eye.x) || 0 : 0;
    f32[base + 45] = eye ? Number(eye.y) || 0 : 0;
    f32[base + 46] = eye ? Number(eye.z) || 0 : 0;
    f32[base + 47] = view.maxDistance > 0 ? view.maxDistance : 0;
    f32[base + 48] = view.minPixel > 0 ? view.minPixel : 0;
    f32[base + 49] = 0;
    f32[base + 50] = 0;
    f32[base + 51] = 0;
    var levels = view.levels || null;
    u32[base + 52] = view.total >>> 0;
    u32[base + 53] = view.slot >>> 0;
    u32[base + 54] = view.phase >>> 0;
    u32[base + 55] = levels ? levels.length : 0;
    for (var l = 0; l < SCENE_GPU_DRIVEN_MAX_LEVELS; l++) {
      var level = levels && l < levels.length ? levels[l] : null;
      u32[base + 56 + l * 4] = level ? level.offset : 0;
      u32[base + 57 + l * 4] = level ? level.width : 0;
      u32[base + 58 + l * 4] = level ? level.height : 0;
      u32[base + 59 + l * 4] = 0;
    }
  }

  // sceneGPUDrivenMaxInstances is the most instances one frame may own: one
  // dispatch covers 65535 workgroups of 64, and the instance buffer must fit
  // one storage binding. A missing limit means the WebGPU default.
  function sceneGPUDrivenMaxInstances(limits) {
    var binding = limits && typeof limits.maxStorageBufferBindingSize === "number"
      ? limits.maxStorageBufferBindingSize
      : 134217728;
    return Math.min(65535 * 64, Math.floor(binding / SCENE_GPU_DRIVEN_INSTANCE_BYTES));
  }

  // createSceneGPUDrivenHost makes the per-renderer host. hooks carries the
  // renderer's own factories, shader sources and instanced-mesh helpers, so
  // the host builds pipelines that match the renderer's without reading its
  // private state. Every method returns false (or its input) when the host is
  // inactive this frame, and the renderer then takes its classic path.
  function createSceneGPUDrivenHost(device, hooks) {
    var disposed = false;
    var layouts = null;
    var compute = { cull: null, downsample: null, seed: null, seedMS: null };
    var computeStarted = { cull: false, downsample: false, seed: false, seedMS: false };
    var failed = "";
    var renderPipelines = new Map();
    var shaderModules = { pbrVertex: null, pbrFragment: null, shadowVertex: null, shadowFragment: null };
    var buffers = { instances: null, meshes: null, args: null, visible: null, visibility: null, views: null, hzb: null, hzbLevels: null, hzbSeed: null, readback: null };
    var capacityInstances = 0;
    var capacityMeshes = 0;
    var capacityHzbFloats = 0;
    var instanceStaging = null;
    var instanceF32 = null;
    var instanceU32 = null;
    var meshStaging = null;
    var meshF32 = null;
    var meshU32 = null;
    var argsTemplate = null;
    var viewStaging = new ArrayBuffer(4 * SCENE_GPU_DRIVEN_VIEW_STRIDE);
    var viewF32 = new Float32Array(viewStaging);
    var viewU32 = new Uint32Array(viewStaging);
    var cullGroups = [null, null, null, null];
    var instancesGroup = null;
    var levelGroups = [];
    var seedGroup = null;
    var seedGroupView = null;
    var records = new Map();
    var owned = [];
    var layoutKey = "";
    var total = 0;
    var frameSerial = 0;
    var active = false;
    var reason = "idle";
    var config = null;
    var cameraSlot = 0;
    var target = { width: 1, height: 1, sampleCount: 1, format: "" };
    var lightDispatched = [0, 0];
    var hiz = { width: 0, height: 0, levels: [], total: 0 };
    var occlusion = { thisFrame: false, lastFrame: false, prepared: false, split: false, resolveTarget: null, timestamps: null };
    var readback = { busy: false, pending: false, meshes: 0, camera: 0, late: 0, shadow: 0 };
    var counters = { dispatches: 0, uploads: 0, uploadedBytes: 0, draws: 0, shadowDraws: 0 };
    var published = "";

    function ensureLayouts() {
      if (layouts) return layouts;
      var C = GPUShaderStage.COMPUTE;
      function entry(binding, type) {
        return { binding: binding, visibility: C, buffer: { type: type } };
      }
      function seedLayout(label, multisampled) {
        return device.createBindGroupLayout({ label: label, entries: [
          entry(0, "uniform"),
          { binding: 1, visibility: C, texture: { sampleType: "depth", viewDimension: "2d", multisampled: multisampled } },
          entry(2, "storage"),
        ] });
      }
      layouts = {
        cull: device.createBindGroupLayout({ label: "gosx-gpu-driven-cull", entries: [
          entry(0, "uniform"), entry(1, "read-only-storage"), entry(2, "read-only-storage"),
          entry(3, "storage"), entry(4, "storage"), entry(5, "storage"), entry(6, "read-only-storage"),
        ] }),
        downsample: device.createBindGroupLayout({ label: "gosx-gpu-driven-hiz-downsample", entries: [
          entry(0, "uniform"), entry(1, "storage"),
        ] }),
        seed: seedLayout("gosx-gpu-driven-hiz-seed", false),
        seedMS: seedLayout("gosx-gpu-driven-hiz-seed-ms", true),
        instances: device.createBindGroupLayout({ label: "gosx-gpu-driven-instances", entries: [
          { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "read-only-storage" } },
        ] }),
        // Same factories as the renderer's own layouts, so the renderer's
        // frame, material and shadow bind groups are group-equivalent here.
        frame: hooks.createFrameBindGroupLayout(device),
        material: hooks.createMaterialBindGroupLayout(device),
        shadow: hooks.createShadowBindGroupLayout(device),
      };
      return layouts;
    }

    function fail(name, detail) {
      if (disposed) return;
      failed = name;
      console.warn("[gosx] gpu-driven: " + name + " pipeline rejected:", detail);
      if (typeof sceneReportPipelineFailure === "function") sceneReportPipelineFailure("gpu-driven", name, String(detail));
    }

    // settle waits for a pipeline promise, then for the module's compile
    // messages, and stores the pipeline only when both are clean.
    function settle(name, promise, modules, store) {
      promise.then(function(pipeline) {
        var check = typeof sceneShaderModuleError === "function" ? sceneShaderModuleError(modules) : Promise.resolve(null);
        return check.then(function(error) {
          if (disposed) return;
          if (error) fail(name, error);
          else store(pipeline);
        });
      }).catch(function(error) {
        fail(name, error && error.message ? error.message : error);
      });
    }

    function startCompute(name, label, code, entryPoint, layout) {
      if (computeStarted[name]) return;
      computeStarted[name] = true;
      var module = device.createShaderModule({ label: label, code: code });
      var descriptor = {
        label: label,
        layout: device.createPipelineLayout({ bindGroupLayouts: [layout] }),
        compute: { module: module, entryPoint: entryPoint },
      };
      var promise = typeof device.createComputePipelineAsync === "function"
        ? device.createComputePipelineAsync(descriptor)
        : Promise.resolve().then(function() { return device.createComputePipeline(descriptor); });
      settle(name, promise, [module], function(pipeline) { compute[name] = pipeline; });
    }

    function startRender(key, descriptor, modules) {
      renderPipelines.set(key, null);
      var promise = typeof device.createRenderPipelineAsync === "function"
        ? device.createRenderPipelineAsync(descriptor)
        : Promise.resolve().then(function() { return device.createRenderPipeline(descriptor); });
      settle(key, promise, modules, function(pipeline) { renderPipelines.set(key, pipeline); });
    }

    function pbrKey(depthWrite) {
      return "pbr|" + target.format + "|" + target.sampleCount + "|" + (depthWrite ? 1 : 0);
    }

    // ensurePBRPipeline starts the vertex-pulling PBR pipeline for the current
    // target and reports whether it is ready.
    function ensurePBRPipeline(depthWrite) {
      var key = pbrKey(depthWrite);
      if (renderPipelines.has(key)) return !!renderPipelines.get(key);
      var l = ensureLayouts();
      if (!shaderModules.pbrVertex) {
        shaderModules.pbrVertex = device.createShaderModule({ label: "gosx-gpu-driven-pbr-vert", code: sceneGPUDrivenPBRVertexWGSL(hooks.pbrInstancedVertexWGSL) });
        shaderModules.pbrFragment = device.createShaderModule({ label: "gosx-gpu-driven-pbr-frag", code: hooks.pbrFragmentWGSL });
      }
      startRender(key, {
        label: "gosx-gpu-driven-pbr",
        layout: device.createPipelineLayout({ bindGroupLayouts: [l.frame, l.material, l.instances] }),
        vertex: { module: shaderModules.pbrVertex, entryPoint: "vertexMain", buffers: hooks.pbrVertexLayout.concat([SCENE_GPU_DRIVEN_SLOT_LAYOUT]) },
        fragment: { module: shaderModules.pbrFragment, entryPoint: "fragmentMain", targets: [{ format: target.format, blend: hooks.blendState("opaque") }] },
        primitive: { topology: "triangle-list", cullMode: "none" },
        multisample: { count: target.sampleCount },
        depthStencil: { format: "depth24plus", depthWriteEnabled: depthWrite, depthCompare: "less-equal" },
      }, [shaderModules.pbrVertex, shaderModules.pbrFragment]);
      return false;
    }

    function ensureShadowPipeline() {
      if (renderPipelines.has("shadow")) return !!renderPipelines.get("shadow");
      var l = ensureLayouts();
      shaderModules.shadowVertex = device.createShaderModule({ label: "gosx-gpu-driven-shadow-vert", code: sceneGPUDrivenShadowVertexWGSL(hooks.shadowInstancedVertexWGSL) });
      shaderModules.shadowFragment = device.createShaderModule({ label: "gosx-gpu-driven-shadow-frag", code: hooks.shadowFragmentWGSL });
      startRender("shadow", {
        label: "gosx-gpu-driven-shadow",
        layout: device.createPipelineLayout({ bindGroupLayouts: [l.shadow, l.instances] }),
        vertex: { module: shaderModules.shadowVertex, entryPoint: "vertexMain", buffers: [hooks.shadowVertexLayout[0], SCENE_GPU_DRIVEN_SLOT_LAYOUT] },
        fragment: { module: shaderModules.shadowFragment, entryPoint: "fragmentMain", targets: [] },
        primitive: { topology: "triangle-list", cullMode: "none", frontFace: "ccw" },
        depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" },
      }, [shaderModules.shadowVertex, shaderModules.shadowFragment]);
      return false;
    }

    function replaceBuffer(name, size, usage) {
      if (buffers[name]) buffers[name].destroy();
      buffers[name] = device.createBuffer({ label: "gosx.gpu-driven." + name, size: size, usage: usage });
    }

    // ensureCapacity grows the shared buffers. Capacities never shrink. A
    // grown buffer drops every bind group that referenced the old one.
    function ensureCapacity(instances, meshes) {
      var U = GPUBufferUsage;
      var grew = false;
      if (instances > capacityInstances) {
        capacityInstances = Math.max(64, Math.ceil(instances * 1.25));
        replaceBuffer("instances", capacityInstances * SCENE_GPU_DRIVEN_INSTANCE_BYTES, U.STORAGE | U.COPY_DST);
        replaceBuffer("visible", capacityInstances * 16, U.STORAGE | U.VERTEX);
        replaceBuffer("visibility", capacityInstances * 4, U.STORAGE | U.COPY_DST);
        instanceStaging = new ArrayBuffer(capacityInstances * SCENE_GPU_DRIVEN_INSTANCE_BYTES);
        instanceF32 = new Float32Array(instanceStaging);
        instanceU32 = new Uint32Array(instanceStaging);
        grew = true;
      }
      if (meshes > capacityMeshes) {
        capacityMeshes = Math.max(4, Math.ceil(meshes * 1.25));
        replaceBuffer("meshes", capacityMeshes * SCENE_GPU_DRIVEN_MESH_BYTES, U.STORAGE | U.COPY_DST);
        replaceBuffer("args", capacityMeshes * 64, U.STORAGE | U.INDIRECT | U.COPY_DST | U.COPY_SRC);
        if (buffers.readback) buffers.readback.destroy();
        buffers.readback = null;
        meshStaging = new ArrayBuffer(capacityMeshes * SCENE_GPU_DRIVEN_MESH_BYTES);
        meshF32 = new Float32Array(meshStaging);
        meshU32 = new Uint32Array(meshStaging);
        argsTemplate = new Uint32Array(capacityMeshes * 16);
        grew = true;
      }
      if (!buffers.views) {
        replaceBuffer("views", 4 * SCENE_GPU_DRIVEN_VIEW_STRIDE, U.UNIFORM | U.COPY_DST);
        grew = true;
      }
      if (!buffers.hzb) {
        // The cull bind group always needs binding 6, even with occlusion off.
        replaceBuffer("hzb", 16, U.STORAGE);
        grew = true;
      }
      if (grew) {
        cullGroups = [null, null, null, null];
        instancesGroup = null;
        levelGroups = [];
        seedGroup = null;
        seedGroupView = null;
      }
      return grew;
    }

    function ensureBindGroups() {
      var l = ensureLayouts();
      for (var v = 0; v < 4; v++) {
        if (cullGroups[v]) continue;
        cullGroups[v] = device.createBindGroup({ label: "gosx-gpu-driven-cull-" + v, layout: l.cull, entries: [
          { binding: 0, resource: { buffer: buffers.views, offset: v * SCENE_GPU_DRIVEN_VIEW_STRIDE, size: SCENE_GPU_DRIVEN_VIEW_BYTES } },
          { binding: 1, resource: { buffer: buffers.instances } },
          { binding: 2, resource: { buffer: buffers.meshes } },
          { binding: 3, resource: { buffer: buffers.args } },
          { binding: 4, resource: { buffer: buffers.visible } },
          { binding: 5, resource: { buffer: buffers.visibility } },
          { binding: 6, resource: { buffer: buffers.hzb } },
        ] });
      }
      if (!instancesGroup) {
        instancesGroup = device.createBindGroup({ label: "gosx-gpu-driven-instances", layout: l.instances, entries: [
          { binding: 0, resource: { buffer: buffers.instances } },
        ] });
      }
    }

    // selectOwned picks this frame's owned meshes from the renderer's opaque
    // instanced list, in list order, and builds the layout key.
    function selectOwned(opaque, limit) {
      var list = [];
      var seen = new Set();
      var instances = 0;
      var key = "";
      for (var i = 0; i < opaque.length; i++) {
        var mesh = opaque[i];
        var count = hooks.instancedMeshCount(mesh);
        if (!sceneGPUDrivenMeshEligible(mesh, count) || seen.has(mesh.id)) continue;
        if (instances + count > limit) continue;
        var geom = hooks.getInstancedGeometry(mesh);
        if (!geom || !(geom.vertexCount > 0)) continue;
        seen.add(mesh.id);
        var radius = hooks.instancedCullRadius(mesh);
        var castShadow = mesh.castShadow ? 1 : 0;
        list.push({ mesh: mesh, count: count, geom: geom, radius: radius, castShadow: castShadow });
        instances += count;
        key += mesh.id + ":" + count + ":" + geom.vertexCount + ":" + radius + ":" + castShadow + ";";
      }
      return { list: list, instances: instances, key: key };
    }

    function writeMeshTable() {
      argsTemplate.fill(0);
      meshU32.fill(0);
      var first = 0;
      var listBase = 0;
      for (var m = 0; m < owned.length; m++) {
        var entry = owned[m];
        entry.index = m;
        entry.first = first;
        entry.argsBase = m * 4;
        entry.listBase = listBase;
        var w = m * 12;
        meshF32[w] = 0;
        meshF32[w + 1] = 0;
        meshF32[w + 2] = 0;
        meshF32[w + 3] = entry.radius;
        meshU32[w + 4] = first;
        meshU32[w + 5] = entry.count;
        meshU32[w + 6] = entry.argsBase;
        meshU32[w + 7] = listBase;
        meshU32[w + 8] = entry.castShadow;
        meshU32[w + 9] = entry.geom.vertexCount;
        for (var s = 0; s < 4; s++) argsTemplate[(m * 4 + s) * 4] = entry.geom.vertexCount;
        first += entry.count;
        listBase += entry.count * 4;
      }
      total = first;
      device.queue.writeBuffer(buffers.meshes, 0, meshStaging, 0, owned.length * SCENE_GPU_DRIVEN_MESH_BYTES);
    }

    // packInstances writes one mesh's records into the staging copy and
    // uploads that range. transforms may be a Float32Array or a plain Array.
    function packInstances(entry) {
      var mesh = entry.mesh;
      var transforms = mesh.transforms;
      var colors = hooks.instancedMeshColorData(mesh, entry.count);
      for (var j = 0; j < entry.count; j++) {
        var w = (entry.first + j) * 24;
        var t = j * 16;
        for (var k = 0; k < 16; k++) instanceF32[w + k] = transforms[t + k];
        instanceF32[w + 16] = colors ? colors[j * 4] : 1;
        instanceF32[w + 17] = colors ? colors[j * 4 + 1] : 1;
        instanceF32[w + 18] = colors ? colors[j * 4 + 2] : 1;
        instanceF32[w + 19] = colors ? colors[j * 4 + 3] : 1;
        instanceU32[w + 20] = entry.index;
        instanceU32[w + 21] = 0;
        instanceU32[w + 22] = 0;
        instanceU32[w + 23] = 0;
      }
      var bytes = entry.count * SCENE_GPU_DRIVEN_INSTANCE_BYTES;
      device.queue.writeBuffer(buffers.instances, entry.first * SCENE_GPU_DRIVEN_INSTANCE_BYTES, instanceStaging, entry.first * SCENE_GPU_DRIVEN_INSTANCE_BYTES, bytes);
      entry.transforms = mesh.transforms;
      entry.colors = mesh.colors;
      entry.revision = mesh._instanceStreamRevision || 0;
      counters.uploads += 1;
      counters.uploadedBytes += bytes;
    }

    function resetVisibility() {
      if (total <= 0) return;
      device.queue.writeBuffer(buffers.visibility, 0, new Uint32Array(total).fill(1));
    }

    function writeView(v, view) {
      sceneGPUDrivenPackView(viewF32, viewU32, v * (SCENE_GPU_DRIVEN_VIEW_STRIDE / 4), view);
      device.queue.writeBuffer(buffers.views, v * SCENE_GPU_DRIVEN_VIEW_STRIDE, viewStaging, v * SCENE_GPU_DRIVEN_VIEW_STRIDE, SCENE_GPU_DRIVEN_VIEW_BYTES);
    }

    function dispatchCull(encoder, v) {
      var pass = encoder.beginComputePass({ label: "gosx-gpu-driven-cull" });
      pass.setPipeline(compute.cull);
      pass.setBindGroup(0, cullGroups[v]);
      pass.dispatchWorkgroups(Math.ceil(total / 64));
      pass.end();
      counters.dispatches += 1;
    }

    // occlusionReady starts the Hi-Z pipelines on first use and reports
    // whether this frame can split the main pass.
    function occlusionReady() {
      if (!config.occlusion || failed === "downsample" || failed === "seed" || failed === "seedMS") return false;
      var l = ensureLayouts();
      startCompute("downsample", "gosx-gpu-driven-hiz-downsample", SCENE_GPU_DRIVEN_HIZ_DOWNSAMPLE_WGSL, "downsample", l.downsample);
      if (target.sampleCount > 1) {
        startCompute("seedMS", "gosx-gpu-driven-hiz-seed-ms", SCENE_GPU_DRIVEN_HIZ_SEED_MS_WGSL, "seed", l.seedMS);
      } else {
        startCompute("seed", "gosx-gpu-driven-hiz-seed", SCENE_GPU_DRIVEN_HIZ_SEED_WGSL, "seed", l.seed);
      }
      return !!compute.downsample && !!(target.sampleCount > 1 ? compute.seedMS : compute.seed);
    }

    // ensureHiZ sizes the pyramid for the depth target and uploads the level
    // table once per size.
    function ensureHiZ() {
      if (hiz.width === target.width && hiz.height === target.height) return;
      var layout = sceneGPUDrivenHiZLevels(target.width, target.height);
      hiz = { width: target.width, height: target.height, levels: layout.levels, total: layout.total };
      if (layout.total > capacityHzbFloats) {
        capacityHzbFloats = layout.total;
        replaceBuffer("hzb", Math.max(16, layout.total * 4), GPUBufferUsage.STORAGE);
        cullGroups = [null, null, null, null];
        seedGroup = null;
        seedGroupView = null;
      }
      if (!buffers.hzbLevels) replaceBuffer("hzbLevels", SCENE_GPU_DRIVEN_MAX_LEVELS * SCENE_GPU_DRIVEN_LEVEL_STRIDE, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST);
      if (!buffers.hzbSeed) replaceBuffer("hzbSeed", 16, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST);
      var table = new Uint32Array(SCENE_GPU_DRIVEN_MAX_LEVELS * SCENE_GPU_DRIVEN_LEVEL_STRIDE / 4);
      for (var k = 1; k < hiz.levels.length; k++) {
        var src = hiz.levels[k - 1];
        var dst = hiz.levels[k];
        table.set([src.offset, src.width, src.height, dst.offset, dst.width, dst.height, 0, 0], k * SCENE_GPU_DRIVEN_LEVEL_STRIDE / 4);
      }
      device.queue.writeBuffer(buffers.hzbLevels, 0, table);
      device.queue.writeBuffer(buffers.hzbSeed, 0, new Uint32Array([hiz.levels[0].width, hiz.levels[0].height, target.width, target.height]));
      levelGroups = [];
    }

    function buildHiZ(encoder, depthView) {
      var l = ensureLayouts();
      var multisampled = target.sampleCount > 1;
      if (!seedGroup || seedGroupView !== depthView) {
        seedGroup = device.createBindGroup({ label: "gosx-gpu-driven-hiz-seed", layout: multisampled ? l.seedMS : l.seed, entries: [
          { binding: 0, resource: { buffer: buffers.hzbSeed } },
          { binding: 1, resource: depthView },
          { binding: 2, resource: { buffer: buffers.hzb } },
        ] });
        seedGroupView = depthView;
      }
      var pass = encoder.beginComputePass({ label: "gosx-gpu-driven-hiz" });
      var level0 = hiz.levels[0];
      pass.setPipeline(multisampled ? compute.seedMS : compute.seed);
      pass.setBindGroup(0, seedGroup);
      pass.dispatchWorkgroups(Math.ceil(level0.width / 8), Math.ceil(level0.height / 8));
      pass.setPipeline(compute.downsample);
      for (var k = 1; k < hiz.levels.length; k++) {
        if (!levelGroups[k]) {
          levelGroups[k] = device.createBindGroup({ label: "gosx-gpu-driven-hiz-level-" + k, layout: l.downsample, entries: [
            { binding: 0, resource: { buffer: buffers.hzbLevels, offset: k * SCENE_GPU_DRIVEN_LEVEL_STRIDE, size: 32 } },
            { binding: 1, resource: { buffer: buffers.hzb } },
          ] });
        }
        pass.setBindGroup(0, levelGroups[k]);
        pass.dispatchWorkgroups(Math.ceil(hiz.levels[k].width * hiz.levels[k].height / 64));
      }
      pass.end();
      counters.dispatches += hiz.levels.length;
    }

    function deactivate(why) {
      active = false;
      reason = why;
      owned = [];
      occlusion.thisFrame = false;
      occlusion.lastFrame = false;
      return false;
    }

    function ownedEntry(mesh) {
      if (!active || !mesh || typeof mesh.id !== "string") return null;
      var entry = records.get(mesh.id);
      return entry && entry.serial === frameSerial ? entry : null;
    }

    function publish(mount) {
      if (!mount || typeof mount.setAttribute !== "function") return;
      var values = [
        ["active", active ? "true" : "false"],
        ["reason", reason],
        ["meshes", String(owned.length)],
        ["instances", String(active ? total : 0)],
        ["occlusion", occlusion.thisFrame ? "true" : "false"],
        ["dispatches", String(counters.dispatches)],
        ["uploads", String(counters.uploads)],
        ["camera-visible", String(readback.camera)],
        ["late-visible", String(readback.late)],
        ["shadow-casters", String(readback.shadow)],
      ];
      var signature = values.join(";");
      if (signature === published) return;
      published = signature;
      for (var i = 0; i < values.length; i++) {
        mount.setAttribute("data-gosx-scene3d-webgpu-gpu-driven-" + values[i][0], values[i][1]);
      }
    }

    return {
      // beginFrame decides whether the host runs this frame, uploads changed
      // records, resets the indirect args and culls the camera view. frame:
      // { viewProjection, camera, width, height, sampleCount, targetFormat,
      // opaque } where opaque is the renderer's opaque instanced list.
      beginFrame: function(bundle, encoder, frame) {
        frameSerial += 1;
        counters.dispatches = 0;
        counters.uploads = 0;
        counters.uploadedBytes = 0;
        counters.draws = 0;
        counters.shadowDraws = 0;
        cameraSlot = 0;
        occlusion.prepared = false;
        occlusion.split = false;
        occlusion.resolveTarget = null;
        occlusion.timestamps = null;
        config = sceneGPUDrivenConfig(bundle && bundle.gpuDriven);
        if (disposed) return deactivate("disposed");
        if (!config) return deactivate("off");
        if (failed) return deactivate("pipeline-failed:" + failed);
        if (!encoder || typeof encoder.beginComputePass !== "function") return deactivate("unsupported");
        var limits = device.limits || {};
        if (typeof limits.maxStorageBuffersInVertexStage === "number" && limits.maxStorageBuffersInVertexStage < 1) return deactivate("unsupported");
        target.width = Math.max(1, Math.floor(frame.width || 1));
        target.height = Math.max(1, Math.floor(frame.height || 1));
        target.sampleCount = Math.max(1, Math.floor(frame.sampleCount || 1));
        target.format = String(frame.targetFormat || "");
        var selection = selectOwned(Array.isArray(frame.opaque) ? frame.opaque : [], sceneGPUDrivenMaxInstances(limits));
        if (selection.list.length === 0) return deactivate("no-eligible-meshes");
        var l = ensureLayouts();
        startCompute("cull", "gosx-gpu-driven-cull", SCENE_GPU_DRIVEN_CULL_WGSL, "cull", l.cull);
        var shadowReady = ensureShadowPipeline();
        var pbrReady = ensurePBRPipeline(true);
        if (!compute.cull || !shadowReady || !pbrReady) return deactivate("warming");
        var grew = ensureCapacity(selection.instances, selection.list.length);
        var relayout = grew || selection.key !== layoutKey;
        owned = [];
        for (var i = 0; i < selection.list.length; i++) {
          var pick = selection.list[i];
          var entry = records.get(pick.mesh.id);
          if (!entry) {
            entry = { transforms: null, colors: null, revision: -1 };
            records.set(pick.mesh.id, entry);
          }
          entry.mesh = pick.mesh;
          entry.count = pick.count;
          entry.geom = pick.geom;
          entry.radius = pick.radius;
          entry.castShadow = pick.castShadow;
          entry.serial = frameSerial;
          owned.push(entry);
        }
        records.forEach(function(entry, id) {
          if (entry.serial !== frameSerial) records.delete(id);
        });
        if (relayout) {
          layoutKey = selection.key;
          writeMeshTable();
        }
        for (var m = 0; m < owned.length; m++) {
          var record = owned[m];
          var mesh = record.mesh;
          if (relayout || record.transforms !== mesh.transforms || record.colors !== mesh.colors ||
            record.revision !== (mesh._instanceStreamRevision || 0)) {
            packInstances(record);
          }
        }
        occlusion.thisFrame = occlusionReady();
        if (occlusion.thisFrame) ensureHiZ();
        if (relayout || (occlusion.thisFrame && !occlusion.lastFrame)) resetVisibility();
        occlusion.lastFrame = occlusion.thisFrame;
        ensureBindGroups();
        device.queue.writeBuffer(buffers.args, 0, argsTemplate, 0, owned.length * 16);
        var levels = occlusion.thisFrame ? hiz.levels : null;
        var cameraView = {
          viewProj: frame.viewProjection, width: target.width, height: target.height,
          eye: frame.camera || null, total: total, slot: 0, phase: 0, levels: levels,
        };
        writeView(0, cameraView);
        if (occlusion.thisFrame) {
          cameraView.slot = 1;
          cameraView.phase = 1;
          writeView(1, cameraView);
        }
        lightDispatched = [0, 0];
        active = true;
        reason = occlusion.thisFrame ? "occlusion" : "frustum";
        dispatchCull(encoder, 0);
        return true;
      },

      owns: function(mesh) {
        return !!ownedEntry(mesh);
      },

      // drawMesh draws one owned mesh's current camera list. The caller has
      // bound group 0 (frame) and group 1 (material).
      drawMesh: function(pass, mesh, depthWrite) {
        var entry = ownedEntry(mesh);
        if (!entry) return false;
        var pipeline = renderPipelines.get(pbrKey(depthWrite !== false));
        if (!pipeline) return false;
        var geom = entry.geom;
        pass.setPipeline(pipeline);
        pass.setBindGroup(2, instancesGroup);
        pass.setVertexBuffer(0, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedPositionBuffer", geom.positions));
        pass.setVertexBuffer(1, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedNormalBuffer", geom.normals));
        pass.setVertexBuffer(2, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedUVBuffer", geom.uvs));
        pass.setVertexBuffer(3, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedTangentBuffer", geom.tangents));
        pass.setVertexBuffer(4, buffers.visible, (entry.listBase + cameraSlot * entry.count) * 4, entry.count * 4);
        pass.drawIndirect(buffers.args, (entry.argsBase + cameraSlot) * 16);
        counters.draws += 1;
        return true;
      },

      // lightView culls every shadow caster for one shadow light. Call it
      // before the light's shadow pass. With shadow culling off the planes
      // accept everything, so casters still draw from one list.
      lightView: function(encoder, lightMatrix, lightSlot) {
        if (!active || !lightMatrix) return false;
        var light = lightSlot === 1 ? 1 : 0;
        var v = 2 + light;
        writeView(v, {
          viewProj: lightMatrix, passAll: !config.shadowCulling, width: target.width, height: target.height,
          eye: null, total: total, slot: v, phase: 2, levels: null,
        });
        dispatchCull(encoder, v);
        lightDispatched[light] = frameSerial;
        return true;
      },

      // drawShadowMesh draws one owned mesh's casters for a shadow light. The
      // caller has bound group 0 (the shadow uniform at its dynamic offset).
      drawShadowMesh: function(pass, mesh, lightSlot) {
        var entry = ownedEntry(mesh);
        var light = lightSlot === 1 ? 1 : 0;
        if (!entry || lightDispatched[light] !== frameSerial) return false;
        var pipeline = renderPipelines.get("shadow");
        if (!pipeline) return false;
        var slot = 2 + light;
        var geom = entry.geom;
        pass.setPipeline(pipeline);
        pass.setBindGroup(1, instancesGroup);
        pass.setVertexBuffer(0, hooks.ensureInstancedGeometryGPUBuffer(geom, "_gosxWGPUInstancedShadowPositionBuffer", geom.positions));
        pass.setVertexBuffer(1, buffers.visible, (entry.listBase + slot * entry.count) * 4, entry.count * 4);
        pass.drawIndirect(buffers.args, (entry.argsBase + slot) * 16);
        counters.shadowDraws += 1;
        return true;
      },

      // splitsMainPass reports whether this frame splits the main pass for
      // two-phase occlusion. Such a frame cannot use a render bundle.
      splitsMainPass: function() {
        return active && occlusion.thisFrame;
      },

      // prepareMainPass moves the resolve target and the end timestamp off the
      // main pass descriptor. The late pass puts them back, so a split frame
      // resolves once and times the whole main pass.
      prepareMainPass: function(descriptor) {
        if (!active || !occlusion.thisFrame || !descriptor) return false;
        var color = descriptor.colorAttachments && descriptor.colorAttachments[0];
        if (color && color.resolveTarget) {
          occlusion.resolveTarget = color.resolveTarget;
          delete color.resolveTarget;
        }
        var stamps = descriptor.timestampWrites;
        if (stamps && stamps.endOfPassWriteIndex !== undefined) {
          occlusion.timestamps = { querySet: stamps.querySet, endOfPassWriteIndex: stamps.endOfPassWriteIndex };
          descriptor.timestampWrites = { querySet: stamps.querySet, beginningOfPassWriteIndex: stamps.beginningOfPassWriteIndex };
        }
        occlusion.prepared = true;
        return true;
      },

      // splitMainPass ends the early pass, builds the Hi-Z pyramid from its
      // depth, culls the camera again against it, and opens the late pass
      // that loads color and depth. It draws the owned meshes' late lists
      // through hooks.drawInstancedMeshes and returns the pass the renderer
      // keeps drawing into. Without a split it returns pass unchanged.
      splitMainPass: function(encoder, pass, descriptor, frameBindGroup, materials, opaque) {
        if (!occlusion.prepared || occlusion.split) return pass;
        occlusion.split = true;
        var color = descriptor.colorAttachments[0];
        var depthView = descriptor.depthStencilAttachment.view;
        pass.end();
        buildHiZ(encoder, depthView);
        dispatchCull(encoder, 1);
        var colorAttachment = { view: color.view, loadOp: "load", storeOp: "store" };
        if (occlusion.resolveTarget) colorAttachment.resolveTarget = occlusion.resolveTarget;
        var late = {
          label: "gosx-gpu-driven-late",
          colorAttachments: [colorAttachment],
          depthStencilAttachment: { view: depthView, depthLoadOp: "load", depthStoreOp: "store" },
        };
        if (occlusion.timestamps) late.timestampWrites = occlusion.timestamps;
        var latePass = encoder.beginRenderPass(late);
        cameraSlot = 1;
        var lateMeshes = [];
        for (var i = 0; i < opaque.length; i++) {
          if (ownedEntry(opaque[i])) lateMeshes.push(opaque[i]);
        }
        if (lateMeshes.length > 0) {
          latePass.setBindGroup(0, frameBindGroup);
          hooks.drawInstancedMeshes(latePass, lateMeshes, materials, "opaque", true);
        }
        return latePass;
      },

      // finishEncoding copies the indirect args to the readback buffer once
      // per frame when no earlier copy is still mapping (telemetry only).
      finishEncoding: function(encoder) {
        if (!active || readback.busy || typeof encoder.copyBufferToBuffer !== "function") return false;
        if (!buffers.readback) {
          buffers.readback = device.createBuffer({ label: "gosx.gpu-driven.readback", size: capacityMeshes * 64, usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST });
        }
        if (typeof buffers.readback.mapAsync !== "function") return false;
        encoder.copyBufferToBuffer(buffers.args, 0, buffers.readback, 0, owned.length * 64);
        readback.pending = true;
        readback.meshes = owned.length;
        return true;
      },

      // endFrame runs after submit. It maps the readback copy, if one was
      // made, and publishes data-gosx-scene3d-webgpu-gpu-driven-* on mount.
      endFrame: function(mount) {
        if (readback.pending && !readback.busy) {
          readback.pending = false;
          readback.busy = true;
          var copy = buffers.readback;
          var meshes = readback.meshes;
          copy.mapAsync(typeof GPUMapMode !== "undefined" ? GPUMapMode.READ : 1).then(function() {
            var words = new Uint32Array(copy.getMappedRange(0, meshes * 64).slice(0));
            copy.unmap();
            var sums = [0, 0, 0, 0];
            for (var m = 0; m < meshes; m++) {
              for (var s = 0; s < 4; s++) sums[s] += words[(m * 4 + s) * 4 + 1];
            }
            readback.camera = sums[0] + sums[1];
            readback.late = sums[1];
            readback.shadow = sums[2] + sums[3];
            readback.busy = false;
          }).catch(function() {
            readback.busy = false;
          });
        }
        publish(mount);
        return this.stats();
      },

      stats: function() {
        return {
          gpuDrivenActive: active,
          gpuDrivenReason: reason,
          gpuDrivenMeshes: owned.length,
          gpuDrivenInstances: active ? total : 0,
          gpuDrivenOcclusion: active && occlusion.thisFrame,
          gpuDrivenDispatches: counters.dispatches,
          gpuDrivenUploads: counters.uploads,
          gpuDrivenUploadedBytes: counters.uploadedBytes,
          gpuDrivenDraws: counters.draws,
          gpuDrivenShadowDraws: counters.shadowDraws,
          gpuDrivenCameraVisible: readback.camera,
          gpuDrivenLateVisible: readback.late,
          gpuDrivenShadowCasters: readback.shadow,
        };
      },

      dispose: function() {
        disposed = true;
        active = false;
        var names = Object.keys(buffers);
        for (var i = 0; i < names.length; i++) {
          if (buffers[names[i]] && !readback.busy) buffers[names[i]].destroy();
          buffers[names[i]] = null;
        }
        records.clear();
        renderPipelines.clear();
        owned = [];
      },
    };
  }

  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    Object.assign(window.__gosx_scene3d_api, {
      createSceneGPUDrivenHost,
      sceneGPUDrivenConfig,
      sceneGPUDrivenMeshEligible,
      sceneGPUDrivenHiZLevels,
      sceneGPUDrivenFrustumPlanes,
      sceneGPUDrivenPackView,
      sceneGPUDrivenMaxInstances,
      sceneGPUDrivenPBRVertexWGSL,
      sceneGPUDrivenShadowVertexWGSL,
      SCENE_GPU_DRIVEN_CULL_WGSL,
      SCENE_GPU_DRIVEN_HIZ_DOWNSAMPLE_WGSL,
      SCENE_GPU_DRIVEN_HIZ_SEED_WGSL,
      SCENE_GPU_DRIVEN_HIZ_SEED_MS_WGSL,
    });
  }
```
