import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const budgets = [
  // bootstrap.js raw bumped 806_000 -> 812_000 for 28-video-sync-fallback.js
  // (parity-locked JS drift engine on the brain-absent video path). gzip/brotli
  // headroom unchanged.
  // Bumped raw 812_000 -> 814_000 and gzip 222_000 -> 223_000 for Scene3D's
  // Selena shader descriptor transport (`shaderBackend` + `shaderLayout`).
  //
  // Bumped raw 814_000 -> 827_000, gzip 223_000 -> 226_000, brotli 182_000 ->
  // 184_000 for Scene3D's Selena shader executor: WebGL full-GLSL program
  // routing plus WebGPU descriptor-packed WGSL pipelines. This turns the prior
  // descriptor transport into executable shader backend support.
  //
  // Bumped raw 827_000 -> 835_000, gzip 226_000 -> 229_000, brotli 184_000 ->
  // 186_000 for WebGPU skeletal skinning: Elio stdlib.Skin compute kernel,
  // storage-buffer packing, per-frame bone palette upload, and computed vertex
  // buffer routing through PBR/shadow draws.
  //
  // Bumped raw 835_000 -> 837_000 for static GLB live model records and
  // transform reprojection, used by baked computed meshes without skinning.
  //
  // Bumped raw 837_000 -> 861_000, gzip 229_000 -> 235_000, brotli 186_000 ->
  // 191_000 — catch-up while re-arming this gate. `make test-js` (and so CI)
  // globbed only *.test.js and never ran this .mjs file, so breaches landed
  // silently: c75e7ee (WebGPU pipeline refresh) shipped bundles already over
  // the very budgets it bumped (raw 856_454 vs 837_000), then 6d4e3dc (Selena
  // custom materials on skinned meshes) added ~1KB more with no bump. The M1
  // ortho-2D board work adds the last ~1.5KB (ortho-2D camera branch in 16a,
  // board-bundle adapter, Selena BoardFill attach). New budgets = current
  // measured size (858_908 / 233_921 / 189_980) plus the customary sub-1%
  // rounding headroom; the Makefile glob now includes *.test.mjs.
  //
  // Bumped raw 861_000 -> 862_000, brotli 191_000 -> 191_500: M4 galaxy
  // compute-particle payload kernel seam (16b-scene-compute.js payload path +
  // schema typedef updates). Measured: 861_059 / 234_524 / 190_508.
  //
  // Bumped raw 862_000 -> 864_000, gzip 235_000 -> 236_000, brotli 191_500 ->
  // 192_000: S1 galaxy triad shaderLib dedup — inflateManifestShaderLibs +
  // inflateSceneShaderLib + SHADER_LIB_FIELDS registry in 10-runtime-scene-core
  // + inflateManifestShaderLibs call in 30-tail.js. Measured: 863_126 /
  // 235_185 / 191_669 + rounding headroom.
  //
  // Bumped raw 864_000 -> 871_000, brotli 192_000 -> 193_000: S2+S3 galaxy
  // authored-shader rungs — Points.Material + ComputeParticles.RenderMaterial
  // plumbing (16a pointsAuthoredVertexPipelineLayout / pointsAuthoredStorage
  // PipelineLayout + buildAuthoredPoints/ParticleRender async pipelines;
  // 16-webgl ensurePointsAuthoredGLProgram; 10-scene-core SHADER_LIB_FIELDS
  // extensions; 30-tail.js inflate updates). Measured: 870_772 / 236_528 /
  // 192_052 + rounding headroom.
  //
  // Bumped raw 871_000 -> 876_000, gzip 237_000 -> 239_000, brotli 193_000 ->
  // 194_000: custom post-effect end-to-end — Selena kind:"post" payload kind
  // in 16a (buildCustomPostPipelineAsync, getSelenaPostBGL, getDepthSampler,
  // ensureCustomPostUniformBuffer, SCENE_POST_CUSTOM_POST case in apply loop)
  // and in 16-scene-webgl (createSceneCustomPostProgram, applyCustomPost, case
  // SCENE_POST_CUSTOM_POST in PBR post chain). Measured: 875_599 / 237_894 /
  // 193_228 + rounding headroom.
  //
  // Bumped raw 876_000 -> 877_000: S4 GLB-point authored-profile — extend
  // sceneApplyNamedMaterialToPoints to propagate customVertexWGSL /
  // customFragmentWGSL / customVertex / customFragment / customUniforms /
  // shaderBackend / shaderLayout from named material profile to GLB-derived
  // point layers; SHADER_LIB_FIELDS registry extended for "materials" collection.
  // Measured: 876_657 / 238_033 / 193_169 + rounding headroom.
  //
  // Bumped raw 877_000 -> 878_000: authored draw ownership diagnostics for
  // WebGPU points and compute-particle render paths. Measured: 877_796 /
  // 238_214 / 193_184; compressed budgets still have headroom.
  //
  // Bumped raw 878_000 -> 880_000: ComputeParticles normalization now preserves
  // computeWGSL plus authored render shader fields through createSceneState.
  // Measured: 879_213 / 238_474 / 193_640; compressed budgets still fit.
  //
  // Bumped raw 880_000 -> 881_000: WebGPU async validation lifecycle guards
  // capture the scoped GPUDevice and ignore stale callbacks after renderer
  // dispose/device loss. Measured: 880_492 / 238_803 / 193_620.
  //
  // Bumped raw 881_000 -> 884_000, gzip 239_000 -> 240_000, brotli 194_000 ->
  // 195_000: Scene3D foreground frame-cap props and animation-chain throttle.
  // Measured: 882_542 / 239_397 / 194_102.
  //
  // Bumped raw 884_000 -> 888_000, gzip 240_000 -> 242_000, brotli 195_000 ->
  // 196_500: Scene3D WebGPU device-loss fallback now swaps to a fresh canvas,
  // rebinds canvas interaction/context listeners, and probes WebGL before 2D.
  // Measured: 886_997 / 240_771 / 195_495.
  //
  // Bumped raw 888_000 -> 894_000, gzip 242_000 -> 243_500, brotli 196_500 ->
  // 197_500: M1 GPU-text slice 2 — the canvas-board LABEL glyph pass in 16a-
  // scene-webgpu.js (BoardText Selena material WGSL + layout, per-font coverage
  // atlas rasterized via OffscreenCanvas, drawBoardLabels per-glyph quad layout,
  // hasLabelData gate). 16a ships in BOTH this bundle and the webgpu feature
  // chunk. Measured: 892_436 / 242_881 / 196_941 + rounding headroom.
  //
  // Bumped raw 894_000 -> 895_000: Slice 1 browser-gpu-cull plumbing —
  // SHADER_LIB_FIELDS entry for instancedMeshes/cullKernelWGSL in
  // 10-runtime-scene-core.js; cull-field validators in 15-scene-ir-schema-strict.js;
  // gpu-cull capability in 16a-scene-webgpu.capabilities.json and
  // 16-scene-webgl.capabilities.json. Measured: 894_095 / 243_280 / 197_165.
  //
  // Bumped raw 895_000 -> 903_000, gzip 244_000 -> 245_500, brotli 198_000 ->
  // 199_500: Slice 2 browser-gpu-cull framework — WGSL_PBR_INSTANCED_CULL_VERTEX
  // shader + WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT + wgpuCreatePBRInstancedCull
  // Pipeline + getPBRInstancedCullPipeline + extractFrustumPlanesJS +
  // updateInstancedCullSystems dispatch hook + indirect-draw branch in
  // drawInstancedMeshes + createSceneInstancedCullSystem in 16b (16a+16b).
  // Measured: 902_343 / 244_995 / 198_759 + rounding headroom.
  //
  // Bumped raw 903_000 -> 905_000: Slice 3 WebGL2 CPU-cull fallback —
  // extractFrustumPlanesJS + instancePassesCullTest hoisted to 11-scene-math.js
  // (shared); CPU cull path (hasCullConfig gate + survivor compaction +
  // dynamic VBO upload) added to drawInstancedMeshes in 16-scene-webgl.js.
  // Measured: 903_824 / 245_457 / 198_989 + rounding headroom.
  //
  // Bumped raw 905_000 -> 906_500, gzip 246_000 -> 246_500: P2.4b unified-motion
  // WASM apply seam — applyWasmMotionFrame in 20-scene-mount.js (lazy
  // __gosx_motion_load/refs once, per-frame __gosx_motion_tick + grow/re-tick,
  // packed-float decode loop mapping position/scale/quat-rotation to
  // SET_TRANSFORM commands via applySceneCommands) plus sceneQuatToEulerXYZ in
  // 11-scene-math.js. Flag-gated on window.__gosx_motion_wasm (inert when unset).
  // Measured: 905_554 / 246_098 / 199_456 + rounding headroom.
  //
  // Bumped raw 906_500 -> 911_000, gzip 246_500 -> 247_500, brotli 199_500 ->
  // 201_000: P4-M3 unified-motion WASM mixer bridge for glTF MODEL animation —
  // sceneAnimWasmClipJSON + sceneAnimWasmDecodePose in 19a-scene-animation.js
  // (clip→JSON serialization + packed-pose [targetID,propID,arity,comps] decode
  // into animatedTransforms) and the 20-scene-mount.js routers
  // (sceneModelWasmMixerActive / sceneAdvanceWasmModelMixer grow-and-retick out
  // buffer / sceneModelRecordPlay|Stop|WasPlaying / wasmMixer create+add_clip in
  // scenePrepareModelSkinPlayback / sceneDestroyModelWasmMixers on teardown).
  // Skinning (buildNodeTransforms / computeJointMatrices) is unchanged. Flag-
  // gated on window.__gosx_motion_wasm (JS mixer default; inert when unset).
  // Measured: 908_723 / 246_935 / 200_138 + rounding headroom.
  //
  // Bumped raw 911_000 -> 915_000, gzip 247_500 -> 249_000, brotli 201_000 ->
  // 202_000: integration merge of perf/scene3d-slices + main's GPU-cull
  // telemetry seam (16b survivor readback, 16a throttled dispatch, 20-scene-mount
  // __gosx_scene3d_telemetry) on top of the unified-motion WASM seams above —
  // the merged bundle carries both. Measured: 913_071 / 248_325 / 201_362 +
  // rounding headroom.
  { file: "bootstrap.js", raw: 915_000, gzip: 249_000, brotli: 202_000 },
  { file: "bootstrap-runtime.js", raw: 120_000, gzip: 33_000, brotli: 30_000 },
  { file: "bootstrap-lite.js", raw: 100_000, gzip: 27_000, brotli: 24_000 },
  // Bumped raw 510_000 -> 512_000 for the WebGL Selena executor. Bumped gzip
  // 140_000 -> 140_500 for static GLB live model records and transform
  // reprojection used by baked computed meshes.
  //
  // Bumped raw 512_000 -> 525_000, gzip 140_500 -> 144_000, brotli 116_000 ->
  // 119_000: same gate-rot catch-up as bootstrap.js above — c75e7ee landed
  // this chunk at raw 522_576, over the 512_000 budget it set, while make
  // test-js skipped this file; the M1 ortho-2D board work adds ~0.9KB on top
  // (measured now: 523_496 / 143_391 / 118_066, plus rounding headroom).
  //
  // Bumped raw 525_000 -> 526_000: async WebGPU validation for compute particle
  // payload kernels (pushErrorScope/popErrorScope + createComputePipelineAsync
  // replacing the old synchronous try/catch path in 16b-scene-compute.js).
  // Measured: 525_173 / 143_759 / 118_429, plus rounding headroom.
  //
  // Bumped raw 526_000 -> 528_000: S1 galaxy triad shaderLib dedup —
  // inflateManifestShaderLibs / inflateSceneShaderLib / SHADER_LIB_FIELDS
  // included in the scene3d feature chunk. Measured: 526_888 / 143_947 /
  // 118_583 + rounding headroom.
  //
  // Bumped raw 528_000 -> 531_000, gzip 144_500 -> 145_500, brotli 119_500 ->
  // 120_000: S2+S3 galaxy authored-shader rungs — Points authored GL program
  // (ensurePointsAuthoredGLProgram, applyPointsAuthoredCustomUniforms in 16-
  // scene-webgl.js) and SHADER_LIB_FIELDS registry extensions in 10-runtime-
  // scene-core.js. Measured: 530_221 / 144_963 / 119_289 + rounding headroom.
  //
  // Bumped raw 531_000 -> 533_000, gzip 145_500 -> 146_000: custom post-effect
  // WebGL2 path (createSceneCustomPostProgram, applyCustomPost, SCENE_POST_
  // CUSTOM_POST = "customPost" constant, case in PBR post chain). Measured:
  // 532_176 / 145_525 / 119_692 + rounding headroom.
  //
  // Bumped raw 533_000 -> 534_000: S4 GLB-point authored-profile — extend
  // sceneApplyNamedMaterialToPoints to carry authored-shader envelope + extend
  // SHADER_LIB_FIELDS for "materials" collection. Measured: 533_232 / 145_655 /
  // 119_826 + rounding headroom.
  //
  // Bumped brotli 120_000 -> 120_500: ComputeParticles normalization now keeps
  // computeWGSL and authored render fields. Measured: 533_791 / 145_799 / 120_081.
  //
  // Bumped raw 534_000 -> 535_000: compute-particle async validation lifecycle
  // guard prevents disposed systems from publishing late pipelines. Measured:
  // 534_310 / 145_945 / 120_003.
  //
  // Bumped raw 535_000 -> 537_000, gzip 146_000 -> 147_000, brotli 120_500 ->
  // 121_000: Scene3D foreground frame-cap props and animation-chain throttle.
  // Measured: 536_361 / 146_509 / 120_436.
  //
  // Bumped raw 537_000 -> 542_000, gzip 147_000 -> 149_000, brotli 121_000 ->
  // 122_500: Scene3D WebGPU device-loss fallback now swaps to a fresh canvas,
  // rebinds canvas interaction/context listeners, and probes WebGL before 2D.
  // Measured: 540_677 / 147_715 / 121_554.
  //
  // Bumped raw 542_000 -> 546_000, gzip 149_000 -> 149_500, brotli 122_500 ->
  // 123_000: Slice 2 browser-gpu-cull — createSceneInstancedCullSystem + exports
  // in 16b-scene-compute.js (the scene3d feature chunk includes 16b).
  // Measured: 545_325 / 148_714 / 122_349 + rounding headroom.
  //
  // Bumped raw 546_000 -> 548_500: Slice 3 WebGL2 CPU-cull fallback —
  // instancePassesCullTest in 11-scene-math.js + CPU cull path in
  // 16-scene-webgl.js (drawInstancedMeshes: hasCullConfig gate, survivor
  // compaction, dynamic VBO upload). Measured: 547_225 / 149_287 / 122_821.
  //
  // Bumped raw 548_500 -> 550_000, brotli 123_000 -> 124_000: P2.4b unified-motion
  // WASM apply seam — applyWasmMotionFrame (lazy motionProgram base64 load via
  // sceneBase64Decode + __gosx_motion_load/refs, per-frame __gosx_motion_tick with
  // grow-and-re-tick on truncation, packed LE-float64 decode loop mapping
  // position/scale/quat-rotation to SET_TRANSFORM commands through
  // applySceneCommands) in 20-scene-mount.js + sceneQuatToEulerXYZ in
  // 11-scene-math.js. Flag-gated on window.__gosx_motion_wasm; inert when unset.
  // Measured: 548_880 / 149_960 / 123_499 + rounding headroom.
  //
  // Bumped raw 550_000 -> 552_500: P4-M3 unified-motion WASM mixer bridge for
  // glTF MODEL animation — 20-scene-mount.js routers (sceneModelWasmMixerActive,
  // sceneAdvanceWasmModelMixer grow-and-retick out buffer, sceneModelRecordPlay|
  // Stop|WasPlaying, wasmMixer create+add_clip in scenePrepareModelSkinPlayback,
  // sceneDestroyModelWasmMixers on teardown) calling sceneAnimWasmClipJSON /
  // sceneAnimWasmDecodePose from the animation chunk. Skinning unchanged. Flag-
  // gated on window.__gosx_motion_wasm; inert when unset. gzip/brotli still fit.
  // Measured: 551_222 / 150_493 / 123_833 + rounding headroom.
  //
  // Bumped raw 552_500 -> 553_500, gzip 150_500 -> 151_500: C3 unified-motion
  // WASM MATERIAL-UNIFORM apply seam — applyWasmMaterialMotionFrame (lazy
  // materialMotionProgram base64 load via sceneBase64Decode +
  // __gosx_motion_load/refs, per-frame __gosx_motion_tick with grow-and-retick,
  // packed LE-float64 decode mapping arity-enum width to material customUniforms
  // writes via sceneResolveMaterialUniforms) in 20-scene-mount.js +
  // sceneResolveMaterialUniforms in 10-runtime-scene-core.js + a live
  // __gosxScene3DState mount handle. Flag-gated on window.__gosx_motion_wasm;
  // inert when unset. brotli still fits. Measured: 552_885 / 150_875 / 123_944
  // + rounding headroom.
  //
  // Bumped raw 553_500 -> 556_000, gzip 151_500 -> 152_000, brotli 124_000 ->
  // 125_000: integration merge — main's GPU-cull telemetry seam folded into the
  // scene3d chunk alongside the unified-motion seams above. Measured: 554_885 /
  // 151_570 / 124_729 + rounding headroom.
  { file: "bootstrap-feature-scene3d.js", raw: 556_000, gzip: 152_000, brotli: 125_000 },
  // Bumped raw 130_000 -> 135_000, gzip 32_000 -> 33_500, brotli 28_000 ->
  // 29_000 for the WebGPU Selena executor. Bumped raw 135_000 -> 143_000,
  // gzip 33_500 -> 36_000, brotli 29_000 -> 31_000 for Elio compute skinning
  // and GPU-deformed PBR vertex buffers.
  //
  // Bumped raw 143_000 -> 155_000, gzip 36_000 -> 38_500, brotli 31_000 ->
  // 33_500: gate-rot catch-up — c75e7ee landed this chunk at raw 152_242,
  // over the 143_000 budget it set in the same commit; 6d4e3dc (Selena custom
  // materials on skinned meshes) added ~1KB more unbumped; and the M1 ortho-2D
  // board work (camera branch + board adapter + BoardFill attach) adds ~0.6KB
  // (measured now: 153_827 / 38_175 / 33_084, plus rounding headroom).
  //
  // Bumped raw 155_000 -> 156_000, gzip 38_500 -> 39_000: M4 galaxy payload
  // kernel seam in 16b-scene-compute.js. Measured: 155_313 / 38_593 / 33_405.
  //
  // Bumped raw 156_000 -> 160_000, gzip 39_000 -> 40_000, brotli 33_500 ->
  // 35_000: S2+S3 galaxy authored-shader rungs — pointsAuthoredUserUniformBGL,
  // pointsAuthoredVertexPipelineLayout, pointsAuthoredStoragePipelineLayout,
  // buildAuthoredPointsVertexPipelineAsync, buildAuthoredParticleRenderPipeline
  // Async, ensurePointsAuthoredUserUniformBuffer in 16a-scene-webgpu.js.
  // Measured: 159_799 / 39_354 / 34_047 + rounding headroom.
  //
  // Bumped raw 160_000 -> 164_000, gzip 40_000 -> 41_000: custom post-effect
  // WebGPU path (buildCustomPostPipelineAsync, getSelenaPostBGL, getDepthSampler,
  // ensureCustomPostUniformBuffer, SCENE_POST_CUSTOM_POST case in apply loop +
  // SCENE_POST_CUSTOM_POST constant bridged via 26e prefix). Measured:
  // 162_911 / 40_201 / 34_738 + rounding headroom.
  //
  // Bumped raw 164_000 -> 165_000: authored draw ownership diagnostics for
  // WebGPU points and compute-particle render paths. Measured: 164_048 /
  // 40_382 / 34_816; compressed budgets still have headroom.
  //
  // Bumped raw 165_000 -> 167_000, gzip 41_000 -> 41_500, brotli 35_000 ->
  // 35_500: scoped GPUDevice popErrorScope guards for custom post, authored
  // points, and authored particle render async callbacks. Measured:
  // 166_165 / 40_940 / 35_214.
  //
  // Bumped raw 167_000 -> 173_000, gzip 41_500 -> 43_500, brotli 35_500 ->
  // 37_500: M1 GPU-text slice 2 — the canvas-board LABEL glyph pass (BoardText
  // Selena material WGSL + layout, per-font coverage atlas via OffscreenCanvas,
  // drawBoardLabels per-glyph quad layout reusing getSelenaPipeline, hasLabelData
  // gate) in 16a-scene-webgpu.js. Measured: 171_595 / 43_048 / 36_971 + rounding
  // headroom.
  //
  // Bumped raw 173_000 -> 181_000, gzip 43_500 -> 45_500, brotli 37_500 ->
  // 39_000: Slice 2 browser-gpu-cull — WGSL_PBR_INSTANCED_CULL_VERTEX +
  // WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT + wgpuCreatePBRInstancedCullPipeline +
  // getPBRInstancedCullPipeline + createSceneInstancedCullSystem in 16b +
  // updateInstancedCullSystems + extractFrustumPlanesJS + indirect-draw branch
  // in drawInstancedMeshes. Measured: 180_246 / 44_932 / 38_599 + rounding headroom.
  // Bumped raw 181_000 -> 181_200: cull telemetry poll/readback reorder (Bug 1) +
  // unlit derivation from kind/materialKind in materialUniformData (Bug 2).
  // Measured: 181_038 + headroom.
  { file: "bootstrap-feature-scene3d-webgpu.js", raw: 181_200, gzip: 45_500, brotli: 39_000 },
  { file: "bootstrap-feature-scene3d-gltf.js", raw: 22_000, gzip: 8_000, brotli: 7_000 },
  { file: "bootstrap-feature-scene3d-animation.js", raw: 8_000, gzip: 4_000, brotli: 4_000 },
  // bootstrap-feature-engines.js carries the video factory, so it now also
  // carries 28-video-sync-fallback.js (the JS drift engine): raw 52_000 ->
  // 58_000, gzip 16_000 -> 18_500, brotli 14_500 -> 16_500.
  //
  // Bumped again for the canvas2d paint loop (26b1-canvas2d-painter.js + the
  // _startCanvasSurfaceRAF render loop in 26b-feature-engines-prefix.js):
  // raw 58_000 -> 62_000, gzip 18_500 -> 20_000, brotli 16_500 -> 18_000.
  //
  // Bumped raw 62_000 -> 64_000 for the canvas2d INTERACTION loop
  // (_bridgeCanvasBoardEvents in 26b-feature-engines-prefix.js: drag-to-pan,
  // wheel-to-zoom, click-to-pick dispatching to __gosx_canvas_event). Compressed
  // headroom unchanged — gzip/brotli stay well under their budgets.
  //
  // Bumped raw 64_000 -> 66_000 (gzip 20_000 -> 21_000, brotli 18_000 -> 19_000)
  // for the canvas2d MARQUEE + KEYBOARD-NAV loop (shift-drag marquee overlay +
  // CANVAS_EVENT_MARQUEE, arrow-key CANVAS_EVENT_NAV, Escape-to-clear in
  // _bridgeCanvasBoardEvents) — interaction parity with the DOM site-map board.
  //
  // Bumped raw 66_000 -> 66_500 for video engine resilience: bitmap WebVTT cue
  // placement, muted autoplay fallback, and fatal HLS network/media recovery.
  // Direct compressed budgets still hold.
  //
  // Bumped raw 66_500 -> 70_000, gzip 21_000 -> 22_000, brotli 19_000 -> 19_700
  // for the DOM label overlay (26b2-canvas-board-labels.js): __gosx_canvas_board_labels_sync
  // and __gosx_canvas_board_labels_dispose. Measured: 69_141 / 21_901 / 19_540,
  // plus sub-1% rounding headroom.
  //
  // Bumped raw 70_000 -> 73_000, gzip 22_000 -> 23_000, brotli 19_700 -> 20_500
  // for the M1 slice-4 WebGPU backend routing in 26b-feature-engines-prefix.js
  // (_startCanvasSurfaceWebGPURAF + the probe/factory/fallback/dispose helpers
  // that route a canvas2d surface to the 16a WebGPU renderer behind the
  // data-gosx-canvas-backend flag). Measured: 71_680 / 22_600 / 20_186, plus
  // sub-1% rounding headroom.
  //
  // Bumped raw 73_000 -> 76_000, gzip 23_000 -> 24_000, brotli 20_500 -> 21_300
  // for CanvasBoard WebGPU HTML overlays in 26b2-canvas-board-labels.js:
  // keyed RenderBundle.HTML reconciliation, pointer-event handling, and
  // focus-preserving editable DOM sync. Measured: 75_180 / 23_446 / 20_888,
  // plus sub-1% rounding headroom.
  { file: "bootstrap-feature-engines.js", raw: 76_000, gzip: 24_000, brotli: 21_300 },
  { file: "bootstrap-feature-hubs.js", raw: 40_000, gzip: 14_000, brotli: 13_000 },
  { file: "bootstrap-feature-islands.js", raw: 10_000, gzip: 4_000, brotli: 4_000 },
];

const routeBudgets = [
  {
    name: "video selective runtime",
    files: ["bootstrap-runtime.js", "bootstrap-feature-engines.js"],
    // raw bumped 160_000 -> 164_000 for 28-video-sync-fallback.js folded into
    // the engines surface. Bumped again 164_000 -> 167_000 (gzip 46_000 ->
    // 48_000) for the canvas2d paint loop folded into the engines surface.
    // Bumped raw 167_000 -> 169_000 (brotli 42_000 -> 43_000) for the canvas2d
    // interaction loop (_bridgeCanvasBoardEvents); gzip headroom unchanged.
    // Bumped raw 169_000 -> 171_000 (gzip 48_000 -> 49_000) for the canvas2d
    // marquee + keyboard-nav loop (shift-drag marquee, arrow-key nav, Escape).
    // Bumped brotli 43_000 -> 43_100 for the video resilience additions above.
    // Bumped raw 171_000 -> 175_000, gzip 49_000 -> 50_000, brotli 43_100 -> 44_100
    // for the DOM label overlay (26b2-canvas-board-labels.js). Measured:
    // 173_758 / 49_610 / 43_879, plus sub-1% rounding headroom.
    // Bumped raw 175_000 -> 178_000, gzip 50_000 -> 51_000, brotli 44_100 -> 45_000
    // for the M1 slice-4 WebGPU backend routing folded into the engines surface
    // (_startCanvasSurfaceWebGPURAF + probe/fallback/dispose). Measured:
    // 176_297 / 50_309 / 44_525, plus sub-1% rounding headroom.
    //
    // Bumped raw 178_000 -> 179_000: S4 GLB-point authored-profile — bootstrap-
    // runtime.js carries the SHADER_LIB_FIELDS extension for "materials"
    // collection. Measured: 178_189 / 50_768 / 44_938, plus rounding headroom.
    // Bumped raw 179_000 -> 183_000, gzip 51_000 -> 52_200, brotli 45_000 ->
    // 46_200 for the CanvasBoard WebGPU HTML overlay helper folded into the
    // engines surface. Measured: 181_639 / 51_556 / 45_591, plus rounding
    // headroom.
    raw: 183_000,
    gzip: 52_200,
    brotli: 46_200,
  },
];

function fileSize(relativePath) {
  const absolute = path.join(__dirname, relativePath);
  return fs.statSync(absolute).size;
}

test("generated bootstrap bundles stay within runtime size budgets", () => {
  for (const budget of budgets) {
    const raw = fileSize(budget.file);
    const gzip = fileSize(`${budget.file}.gz`);
    const brotli = fileSize(`${budget.file}.br`);

    assert.ok(raw <= budget.raw, `${budget.file} raw size ${raw} exceeds budget ${budget.raw}`);
    assert.ok(gzip <= budget.gzip, `${budget.file}.gz size ${gzip} exceeds budget ${budget.gzip}`);
    assert.ok(brotli <= budget.brotli, `${budget.file}.br size ${brotli} exceeds budget ${budget.brotli}`);
    assert.ok(gzip < raw, `${budget.file}.gz should be smaller than raw JS`);
    assert.ok(brotli <= gzip, `${budget.file}.br should be no larger than gzip`);
  }
});

test("selective runtime route surfaces stay within first-load budgets", () => {
  const monolithRaw = fileSize("bootstrap.js");
  for (const budget of routeBudgets) {
    const raw = budget.files.reduce((sum, file) => sum + fileSize(file), 0);
    const gzip = budget.files.reduce((sum, file) => sum + fileSize(`${file}.gz`), 0);
    const brotli = budget.files.reduce((sum, file) => sum + fileSize(`${file}.br`), 0);

    assert.ok(raw <= budget.raw, `${budget.name} raw size ${raw} exceeds budget ${budget.raw}`);
    assert.ok(gzip <= budget.gzip, `${budget.name}.gz size ${gzip} exceeds budget ${budget.gzip}`);
    assert.ok(brotli <= budget.brotli, `${budget.name}.br size ${brotli} exceeds budget ${budget.brotli}`);
    assert.ok(raw < monolithRaw * 0.25, `${budget.name} should stay below 25% of legacy monolith raw size`);
  }
});

// Regression guard for the mobile-GPU throttle bug (spore
// spore.2026-06-24.claude-opus.verify-artifact-and-all-copies): the device-
// capability predicate must stay a single source of truth (gosxLowEndHardware)
// and require BOTH low memory AND few cores. It previously lived inline in two
// files joined by OR; since mobile Chrome reports deviceMemory<=4 near-
// universally, that OR throttled capable phones to the low-power GPU.
test("device-capability gate stays DRY (gosxLowEndHardware, AND-form, no inlined OR drift)", () => {
  const srcDir = path.join(__dirname, "bootstrap-src");
  const files = fs.readdirSync(srcDir).filter((f) => f.endsWith(".js"));
  let definitions = 0;
  let references = 0;
  for (const f of files) {
    const body = fs.readFileSync(path.join(srcDir, f), "utf8");
    assert.ok(
      !/deviceMemory\s*<=\s*4\s*\)\s*\|\|\s*\(?\s*hardwareConcurrency/.test(body) &&
        !/hardwareConcurrency\s*<=\s*4\s*\)\s*\|\|\s*\(?\s*deviceMemory/.test(body),
      `inlined OR-form device-capability predicate found in ${f}; derive it from gosxLowEndHardware instead`,
    );
    definitions += (body.match(/function\s+gosxLowEndHardware\s*\(/g) || []).length;
    references += (body.match(/gosxLowEndHardware\s*\(/g) || []).length;
  }
  assert.equal(definitions, 1, "gosxLowEndHardware must be defined exactly once (single source of truth)");
  assert.ok(references >= 3, "gosxLowEndHardware should back both lowPower and constrainedHardware");
  const env = fs.readFileSync(path.join(srcDir, "05-document-env.js"), "utf8");
  assert.ok(
    /function\s+gosxLowEndHardware[\s\S]{0,200}<=\s*4\s*\)\s*&&\s*\(/.test(env),
    "gosxLowEndHardware must AND the memory and core checks (not OR)",
  );
});
