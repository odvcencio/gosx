# 00 — Context every task relies on

Read this whole file before any task. It records facts about the codebase that
were verified at the baseline commit. When a later task says "see 00 §N", it
means a section here.

## §1 Files and their roles

| Path | Role |
|---|---|
| `client/runtime/scene3d/webgpu.ts` | WebGPU renderer (19 125 lines, JS syntax). Factory `createSceneWebGPURenderer(canvas, options)`; per-frame `render(bundle, viewport, frameMeta)`. Ships in `bootstrap-feature-scene3d-webgpu.js` **and** the monolith `bootstrap.js`. |
| `client/runtime/scene3d/compute.ts` | Compute chunk: particle systems and the per-mesh GPU cull `createSceneInstancedCullSystem`. Ships in `bootstrap-feature-scene3d-compute.js` and `bootstrap.js`. |
| `client/runtime/scene3d/indirect-instancing.ts` | NEW (G05). The GPU-driven host. Ships right after `compute.ts` in the compute chunk and in `bootstrap.js`. Full text: appendix D. |
| `client/js/bootstrap-src/26k-feature-scene3d-compute-prefix.ts` / `-suffix.ts` | Wrap the compute chunk in one IIFE. The prefix defines `sceneNumber`, `sceneBool`, `clamp01` and `sceneColorRGBA` for the chunk. |
| `client/runtime/scene3d/mount.ts` | Mount loop. Rebuilds the render bundle every frame and calls `renderer.render(...)`. |
| `client/js/bootstrap-src/10-runtime-scene-core.ts` | `createSceneState`, `createSceneRenderBundle`, and instanced-mesh normalisation. |
| `client/runtime/scene3d/instance-stream.ts` | Binary per-frame instance-transform fast path (lazy chunk). |
| `cmd/buildbootstrap/main.go` | Chunk table: which sources go in which bundle. `make build-bootstrap` runs it. |
| `client/js/runtime-test-harness.js` | Fake WebGPU device and renderer harnesses for Node tests. |
| `client/js/runtime-21-scene-gpu-cull-bundles.test.js` | Existing GPU cull / render-bundle tests. New renderer tests go in a new file (see G06). |
| `scene/scene.go`, `scene/scene_ir.go`, `scene/diff.go` | Go authoring types, IR lowering, diff policy. |
| `scene/schema/schema.json`, `scene/schema/validate.go` | IR JSON schema and Go validator. |
| `client/js/testdata/scene3d-renderer-architecture.json` | Renderer governance: line ceilings, per-function complexity exceptions, symbol metrics. |
| `client/js/bootstrap-size.test.mjs` | Byte budgets per bundle and per page route. |
| `client/runtime/scene3d/noimplicitany-baseline.json` | Per-file noImplicitAny error counts (ratchet). |

## §2 The render bundle is rebuilt every frame

`mount.ts` calls `createSceneRenderBundle(...)` once per frame. Each
`bundle.instancedMeshes[i]` is a **new shallow copy** of the scene-state
entry: `Object.assign({}, sceneMesh, { materialIndex, materialKind, renderPass,
_renderPassDerived })` in `appendSceneInstancedMeshesToBundle`. When a named
material applies, `sceneStateInstancedMeshesWithMaterials` also returns a
fresh copy. Therefore:

- Never cache a GPU object on the bundle mesh object. Key per-mesh GPU state by
  `mesh.id` (a string) in a renderer- or host-scoped `Map`.
- References copied from the scene-state entry **are** stable across frames:
  `mesh.transforms`, `mesh.colors` and `mesh._cachedTransforms`, when present.
  Use their identity for change detection.
- Unit tests often reuse one bundle object across frames, which hides this.
  The G01 test builds a new bundle per frame, as the mount does.

## §3 Instanced mesh fields available at draw time

`bundle.instancedMeshes[i]` carries: `id` (string); `count` (or
`instanceCount`); `kind` (`box`, `sphere`, `cylinder`, `cone`, `torus`,
`torusknot`, `plane`) plus that kind's dimensions; `transforms` (16 floats per
instance, column-major, as a `Float32Array` or plain `Array`); `colors`
(optional: CSS strings, or 3 or 4 floats per instance); `castShadow`;
`receiveShadow`; `viewCulled`; `cullKernelWGSL` (optional, authored);
`materialIndex`; `renderPass`.

Renderer helpers inside `createSceneWebGPURenderer` (all in `webgpu.ts`):

| Helper | Returns |
|---|---|
| `instancedMeshCount(mesh)` | `instanceCount`, else `count`, else 0 |
| `instancedMeshTransformData(mesh, count)` | `Float32Array` with ≥ `count*16` floats, or `null` |
| `instancedMeshColorData(mesh, count)` | `Float32Array(count*4)`; white when no colours |
| `getInstancedGeometry(mesh)` | `{ positions, normals, uvs, tangents, vertexCount }`; a non-indexed triangle list, cached per geometry key |
| `ensureInstancedGeometryGPUBuffer(geom, slotName, data)` | cached `GPUBuffer` (VERTEX) |
| `webGPUInstancedCullRadius(mesh)` | local bounding-sphere radius about the origin, padded 5% |
| `buildInstancedDrawList(bundle, materials)` | `{ opaque, alpha, additive }` mesh arrays, `viewCulled` removed |
| `drawInstancedMeshes(pass, meshList, materials, blendMode, depthWrite)` | draws a pass list (direct pass or bundle encoder) |
| `drawInstancedShadowMeshes(pass, bundle)` | draws every shadow-casting instance into the current shadow pass |
| `createMaterialBindGroup(material, receiveShadow, cacheOwner, modelMatrix)` | material bind group (group 1), memoised on `cacheOwner` |

Module-scope names in `webgpu.ts` used by this spec: `wgpuBlendState(mode)`,
`WGPU_PBR_VERTEX_LAYOUT` (4 buffers, locations 0–3),
`WGPU_SHADOW_VERTEX_LAYOUT` (1 buffer, location 0),
`WGSL_PBR_INSTANCED_VERTEX`, `WGSL_SHADOW_INSTANCED_VERTEX`.
Renderer-scope names: `device`, `targetFormat`, `activeSampleCount`,
`frameBindGroupLayout` (group 0 of PBR pipelines), `materialBindGroupLayout`
(group 1), `shadowBindGroupLayout` (group 0 of shadow pipelines: a uniform with
a dynamic offset, `minBindingSize: 64`), `pbrFragmentModule`,
`shadowFragmentModule`, `scratchSelenaViewProjection`, `canvas`.

## §4 Conventions the kernels depend on

- Matrices are column-major: element `(row r, col c)` is at index `c*4 + r`.
- `scratchSelenaViewProjection` is `remappedProjection × view`. It is exactly
  the matrix the vertex shaders apply (`frame.projMatrix * frame.viewMatrix`).
  Its clip-space z is in [0, 1] (WebGPU). `uploadFrameUniforms` builds it.
- Main depth: `depth24plus`, cleared to `1.0`, `depthCompare: "less-equal"`.
  Nearer is smaller. A Hi-Z texel stores the **max** (farthest) depth.
- NDC y points up and framebuffer rows go down:
  `pixelY = (0.5 - 0.5 * ndcY) * height`.
- The main render target is `scaledW × scaledH`: equal to the canvas size
  without post-FX, and the post-FX target size with it. MSAA is 1 or 4
  (`resolveWebGPUSampleCount`). The main depth texture comes from
  `ensureMainDepth(w, h, sampleCount)`, or from the post-FX target when post-FX
  is on and `sampleCount == 1`. That one already has `TEXTURE_BINDING`.
- `device.queue.writeBuffer` calls execute before the frame's single command
  buffer. A buffer region may be written at most once per frame if different
  passes must see different values. Give each view its own uniform region.

## §5 Frame order inside `render()` at the baseline

1. `uploadFrameUniforms(...)` → `var cam`, `scratchSelenaViewProjection`.
2. `var encoder = device.createCommandEncoder({ label: "gosx-frame" });`
3. Compute updates: particles, morphs, skinning, water, then
   `updateInstancedCullSystems(bundle.instancedMeshes, encoder, scratchSelenaViewProjection);`
4. Shadow loop: up to 2 lights; for each, `renderShadowPass(encoder, lightMatrix, bundle, { view: ..., lightSlot: slot }, pbrSceneBuffers);`.
   Inside it, the shadow bind group (group 0, light matrix at a dynamic offset)
   is bound before `drawInstancedShadowMeshes(pass, bundle);`.
5. Main target selection (MSAA / post-FX / canvas), `mainPassDescriptor`,
   timestamp writes, then `var mainPass = encoder.beginRenderPass(mainPassDescriptor);`.
6. Render-bundle decision (`sceneWebGPUBundleIneligibleReason({...})`), then
   either `executeBundles` or the direct branch. The direct branch draws opaque
   → water → alpha → additive, then labels, screen lines and points.
7. `mainPass.end();`, pick pass, post-FX, `endGPUFrameTiming`,
   `endGPUPassTimingFrame`, `device.queue.submit([...])`, sweeps,
   `publishWebGPUFrameStats(frameStats);`.

## §6 Chunks and loading

- `bootstrap-feature-scene3d-compute.js` holds prefix 26k + `compute.ts` +
  suffix 26k. The mount fetches it when the scene declares an instanced mesh or
  a compute particle system. Its symbols are assigned onto
  `window.__gosx_scene3d_api`, and the renderer reads them **at call time**.
- The Node harness option `createBoardWebGPUHarness({ fresh: true })` builds
  chunks by concatenating the raw source files listed in
  `client/js/bootstrap-src/chunks.json`, **without** transpiling. So:
  (a) runtime sources must be plain JS syntax; and (b) after adding a file to a
  chunk in `cmd/buildbootstrap/main.go`, run `make build-bootstrap`, so that
  `chunks.json` lists it, before any fresh-harness test can see it.
- Non-fresh harnesses read the committed bundles. Rebuild them
  (`make build-bootstrap`) before running JS tests after any runtime edit.

## §7 Governance ratchets (all enforced in CI)

1. **Line ceilings**: `files["../runtime/scene3d/webgpu.ts"]` = 19125 and
   `sourceSets.webgpu.maxLines` / `maxFileLines` in
   `client/js/testdata/scene3d-renderer-architecture.json`. `webgpu.ts` is
   exactly at its ceiling, so any net line added fails
   `client/js/scene3d-renderer-architecture.test.js`. `mount.ts` = 3859
   (3 lines of room at baseline).
2. **Per-function complexity**: every function in `webgpu.ts` must be within
   200 lines, cyclomatic 40, cognitive 60, nesting 4, and 8 parameters. The
   alternative is an exception keyed `name@line:col` in `functionExceptions`.
   Inserting lines shifts every key below the insertion point.
   `createSceneWebGPURenderer` also has a `symbols` ceiling. The Go test
   `cmd/buildbootstrap/scene3d_renderer_architecture_test.go` prints the new
   names and values.
3. **Governance line count**: `governanceBudgetRevision.actualGovernanceLinesAtReview`
   must **equal** the summed physical lines of five governance files, including
   the JSON itself.
4. **Byte budgets**: `client/js/bootstrap-size.test.mjs`. The route
   "Scene3D Chromium route (WebGPU, with labels)" has about 228 gzip bytes of
   room, so any growth of `webgpu.ts` or the base chunk needs a budget update.
   The compute chunk budget has about 1 KB raw of room.
5. **noImplicitAny ratchet**: `npm run typecheck` inside `client/runtime` runs
   `typecheck:scene3d` (zero errors required) and the ratchet. Baselines:
   `webgpu.ts` 1744, `mount.ts` 335, `compute.ts` 110. Behaviour verified with
   TypeScript 5.9.3:
   - `function f(a = null)` and `function f(a = undefined)` → TS7006
     (still counted).
   - `function f(a = "")`, `(a = 0)`, `(a = false)` → typed, no error.
   - `var x = null;` read inside another function → TS7034 at the declaration
     plus TS7005 at every use.
   - Calling a method on an `any` value (`window.__gosx_scene3d_api...`,
     `someMap.get(k)`) → no error.
   - Calling a zero-parameter function with arguments → TS2554, unless the
     receiver is `any`.
   - The ratchet silently passes when `node_modules` is missing. Always run
     `npm ci` first (01-preflight).
6. **Test pins in `webgpu.ts`**: see README rules 6–7.
7. **Capability matrix**: do not add a Matrix row. The GPU-driven path is
   performance-only.

The procedure for updating 1–4 lives in G10. Tasks that touch the governed
files run it at their end.

## §8 The fake WebGPU device (`makeFakeGPUDevice` in `runtime-test-harness.js`)

- It records `state.renderPasses`, `state.computePasses`,
  `state.renderPipelines`, `state.computePipelines`, `state.shaderModules`,
  `state.bindGroups`, `state.buffers`, `state.textures`,
  `state.writeBufferCalls`, `state.renderBundleEncoders` and
  `state.renderBundles`.
- A pass records `draws`, `drawIndirects` (`{buffer, offset, pipeline}`),
  `dispatches` (`{workgroupCountX, Y, Z, pipeline}`), `vertexBuffers`
  (`{slot, buffer, offset, size}`), `bindGroups` (`{slot, group,
  dynamicOffsets}`), `pipelines`, `descriptor` and `executedBundles`.
- `device.limits` is `{}` (or `{ timestampPeriod: 1 }`). Treat missing limits as
  WebGPU defaults.
- `createComputePipelineAsync` resolves immediately. Shader modules have no
  `getCompilationInfo` unless you use `makeFakeGPUDeviceForCompute`.
- The encoder has **no** `clearBuffer` and **no** `copyTextureToBuffer`.
  `copyBufferToBuffer` exists by default. Buffers only get `mapAsync` when
  `timestampQuery: true`. Use `queue.writeBuffer` to reset buffers, and guard
  `mapAsync` with `typeof buffer.mapAsync === "function"`.
- Render pipelines keep their descriptor at `pipeline.desc` (so `desc.label` is
  readable). Bind group layouts keep theirs at `layout.desc`.
- Harness: `createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: { renderBundles: true } })`
  returns `{ env, renderer, fake, mount, canvas }`. `env.context` is the
  sandbox `window`; `env.context.__gosx_scene3d_api` is the scene API; `fake`
  is `{ device, state }`; `mount.getAttribute(name)` reads published data
  attributes.

## §9 Test commands

| Purpose | Command (from repo root unless noted) |
|---|---|
| Rebuild bundles | `make build-bootstrap` |
| Bundles in sync | `cd cmd/buildbootstrap && GOWORK=off go run -tags 'grammar_subset grammar_subset_typescript' . --check` |
| Buildbootstrap Go tests (architecture ratchets) | `cd cmd/buildbootstrap && GOWORK=off go test -count=1 -tags 'grammar_subset grammar_subset_typescript' ./...` |
| One JS test file | `node --test client/js/<file>` (add `--test-name-pattern='...'` to filter) |
| All JS tests | `node --test client/js/*.test.js client/js/*.test.mjs client/runtime/*/*.test.js` |
| Typecheck + ratchet | `cd client/runtime && npm run typecheck` |
| Scene Go tests | `go test ./scene/... ./render/bundle ./scene/capability` |
| Docs app tests | `go test ./examples/gosx-docs/...` |
| Whitespace | `git diff --check` |
