# Appendix C — Observations made while writing this spec (out of scope)

These were found while validating the spec. None is required for the
feature. Each entry says what was seen, how, and what to do about it. File
them as issues; do not fold them into the tasks.

## C1. Instanced meshes re-created GPU state every frame (fixed by G01)

Seen with the repo's own harness: on the mount path every frame allocated one
uniform buffer and one bind group per instanced mesh, and a static instanced
scene never replayed its render bundle. Cause: the render bundle is rebuilt
every frame with shallow copies of each mesh (00 §2), and the renderer cached
GPU objects on the copy. G01 keys the cache by mesh id. It is in this spec
because the GPU-driven CPU win depends on bundle replay.

## C2. Elio HEAD does not build against gosx HEAD (worked around by E0)

gosx requires `github.com/odvcencio/gotreesitter v0.50.1`; Elio pins
v0.47.0 and replaces `m31labs.dev/gosx => ../gosx`. Any `go` command in Elio
first fails with `updates to go.mod needed`; letting Go update selects
v0.50.1, under which Elio's parser rejects every `//` comment
(`syntax error near "// ..."`), even with a regenerated `parse/grammar.bin`.
E0 pins v0.47.0 with a `replace`. Report upstream: either the gotreesitter
regression or Elio's grammar needs a fix before Elio can follow gosx's pin.

## C3. Per-frame conversions on the classic instanced path

`instancedMeshTransformData` caches its `Float32Array` on the mesh object it
receives, and `instancedMeshColorData` does the same. Because that object is
the per-frame bundle copy, a mesh whose `transforms` is a plain `Array` (the
normal case: the normalizer stores `item.transforms.slice()`) is converted
again every frame, and CSS colour strings are parsed again every frame. The
render-bundle planner walks the draw path every frame to fingerprint it, so
this happens even when the bundle replays. For 40 000 instances that is
2.5 MB of `Float32Array` plus 40 000 colour parses per frame.

The GPU-driven host avoids it for the meshes it owns (it reads `transforms`
directly and converts colours only when the array identity changes), but
`buildInstancedDrawList` still calls `instancedMeshTransformData` for every
instanced mesh. Suggested fix: key both caches by the source array in a
renderer-scope `WeakMap` (source array → `{ count, data }`). Measure with the
G07 bench pair.

## C4. `Shadows.MaxPixels` does not reach the WebGPU renderer

`render()` reads `bundle.shadowMaxPixels`, but neither `createSceneState` nor
`createSceneRenderBundle` copies `scene.shadowMaxPixels` onto the bundle, so
the renderer always sees 0 and applies the default 1024² cap. An authored
`ShadowMaxPixels2048` therefore has no effect in the browser. Verify with a
scene that sets it and a directional light with `ShadowSize: 4096`; the
fix mirrors how G04 carries `gpuDriven`.

## C5. Directional shadow casters land outside WebGPU's depth range

In the G09 probe the light matrix passed to `renderShadowPass` for a
directional light (from `sceneShadowLightSpaceMatrix`, then
`sceneWebGPUShadowDepthMatrix`) put every caster at clip z ≈ −0.5. The light
view looks along +forward (view z positive for casters) while the orthographic
projection uses the GL convention (`-2 / (far - near)`), so z comes out
negative and the rasterizer clips every caster. In the probe scene, casting
on and off changed no pixels on the classic path. The GPU-driven light cull
agrees with the rasterizer (it culls what would be clipped), so the feature
keeps pixel parity either way.

This needs a focused check with a scene built to show a shadow, and a look at
how the WebGL path uses the same matrix, before anyone changes it.

## C6. The source video was not reachable from the build environment

YouTube returned a bot check and HTTP 429 for
<https://www.youtube.com/watch?v=LzivS_KzffY> and its transcript. The spec
implements the canonical GPU-driven design (README). If the video describes
something this spec lacks (for example meshlets or cluster culling), treat it
as future work below.

## C7. Test-harness gaps met while writing tests

- `makeFakeGPUDevice` drops `label` from `createBuffer` descriptors. The G06
  test wraps `createBuffer` to keep it; a harness change would help everyone.
- The fake device has no `createRenderPipelineAsync`, and
  `createComputePipelineAsync` does not record pipelines in
  `state.computePipelines`. The host falls back to the sync call inside a
  promise, so tests see pipelines one frame late (reason `warming`).
- The harness defines `GPUBufferUsage`, `GPUTextureUsage` and
  `GPUShaderStage` but not `GPUMapMode`; the host guards with `typeof`.

## C8. Future work for GPU-driven instancing

- Expose `MinPixelSize` and `MaxDistance` in `scene.GPUDriven`. The kernel
  already implements both (B6 `params.x`, `eye.w`); v1 writes 0.
- Own alpha and additive meshes (needs a GPU sort or per-mesh ordering).
- Own `InstancedGLBMesh` batches (needs indexed geometry and
  `drawIndexedIndirect`).
- Per-instance level of detail: pick a mesh per instance in the cull kernel.
- Keep render bundles on occlusion frames by recording one bundle per pass.
- Native renderer parity (`render/bundle`), so headless images and native
  apps use the same kernels through Elio's other backends.
- Picking for owned meshes currently rebuilds the classic transform buffer on
  demand; a pick pass that reads the host's instance buffer would avoid it.
