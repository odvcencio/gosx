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
  // Bumped raw 915_000 -> 1_130_000, gzip 249_000 -> 300_000, brotli 202_000 ->
  // 245_000: the scene3d/water system (16a WaterSystem WGSL passes + water shader
  // modules, landed in checkpoint f6c21364) is the bulk of this (~204K raw);
  // declarative interaction primitives 06-declarative-actions.js +
  // 07-declarative-regions.js add ~5K. Measured 1_119_654 / 295_924 / 239_403.
  // Trim when the water WIP finalizes its scene-module footprint.
  // Bumped raw 1_130_000 -> 1_180_000, gzip 300_000 -> 312_000, brotli 245_000
  // -> 252_000: water-demo Selena convergence. The hand-written jeantimex-water
  // WGSL/Elio shader trees were retired; every water pass (9 render + 5 compute,
  // incl. rounded pool) now emits from Selena and routes through the generic
  // descriptor-driven WGSL pipeline/bindgroup path (16a getSelenaComputePipeline,
  // state/grid handling + G1 array packing + post-kind path). This is net-added
  // over the retained SCENE_WATER_* builtin fallback tier; removing that fallback
  // once WebGPU water rendering is visually confirmed reclaims most of this.
  // Measured: 1_170_081 / 309_540 / 249_595 + sub-1% rounding headroom.
  //
  // Bumped raw 1_180_000 -> 1_181_000, gzip 312_000 -> 313_000, brotli
  // 252_000 -> 253_000: video-player-primitives (audio tracks, seekable/live
  // edge, quality levels, preference persistence, PiP + input lock) folded
  // into 30-tail.js. Measured: 1_180_338 / 312_544 / 252_009 + sub-1%
  // rounding headroom.
  //
  // Bumped raw 1_181_000 -> 1_184_000, gzip 313_000 -> 314_000: merge of
  // video-player-primitives with the scene3d gizmo/water/regions line —
  // both grew the bundle independently. Measured: 1_182_312 / 313_299 /
  // 252_641 + sub-1% rounding headroom.
  //
  // Bumped raw 1_184_000 -> 1_187_000, gzip 314_000 -> 315_000, brotli
  // 253_000 -> 254_000: E6 typed audio authoring surface (scene/audio.go's
  // Audio/AudioCue/SynthPatch) — arcadeAudio gains an ADSR envelope
  // (arcadeEnvelopeADSR) and a generic tone/sweep/noise patch player
  // (arcadePlayPatch), both exposed via window.__gosx.arcadeAudio, plus the
  // dedicated "audio" hub-event cue path in 20-scene-mount.js. Measured:
  // 1_185_499 / 314_215 / 253_293 + sub-1% rounding headroom.
  //
  // Bumped raw 1_187_000 -> 1_196_000, gzip 315_000 -> 317_000, brotli
  // 254_000 -> 256_000: WebGL GPU-skinned Selena materials
  // (scenePBRSelenaSkinAugmentVertex in 16-scene-webgl.js — renames compiled
  // position/normal attributes, injects the joint-blend skin matrix + model
  // matrix so custom materials draw skinned rigs) plus the gameplay post-FX
  // preset fast path and FXAA pass across 16-scene-webgl.js /
  // 16a-scene-webgpu.js / 30-tail.js, landed together with the E6 audio line
  // above. Measured: 1_192_618 / 315_900 / 254_712 + sub-1% rounding
  // headroom.
  // Bumped raw 1_196_000 -> 1_199_000, gzip 317_000 -> 318_000, brotli
  // 256_000 -> 256_500: water steady-state performance slice — cached WebGL
  // uniform locations, retained shadow/object-texture passes, and throttled
  // broad WebGPU DOM diagnostics. Measured: 1_197_095 / 317_299 / 255_972.
  // Fixed-rate water plus hysteretic adaptive profiles, nonblocking GPU timing,
  // and atomic quality resource swaps. Measured: 1_226_483 / 326_008 / 262_876.
  // Water fixed-clock event queues, timing failure state, and allocation
  // retry telemetry add ~3.7KB raw while compressed output remains in budget.
  // Bumped raw 1_234_000 -> 1_235_000, gzip 328_000 -> 329_000, brotli
  // 264_000 -> 265_000 for the generic runtime-surface
  // registry, which gives optional packages such as gosx/editor managed
  // mount/dispose, navigation remounting, and scoped browser bridges.
  // Bumped raw 1_242_000 -> 1_245_000, gzip 333_000 -> 334_000, brotli
  // 269_000 -> 270_000 for lifecycle telemetry and navigation coordination.
  // Bumped raw 1_245_000 -> 1_247_000 for the core request transport bridge
  // (CSRF defaults, response JSON helper, and surface abort propagation).
  // Bumped raw 1_247_000 -> 1_250_000 for surface latest-request
  // coordination and editor response helpers. Measured: 1_247_767 /
  // 331_948 / 267_500, plus rounding headroom.
  // Bumped raw 1_250_000 -> 1_253_000 for the shared runtime DOM replacement
  // lifecycle. Bumped raw 1_253_000 -> 1_257_000 for scoped motion/text-
  // layout lifecycle disposal. Measured: 1_253_630 / 333_374 / 268_521.
  // Bumped raw 1_257_000 -> 1_259_000 and gzip 334_000 -> 335_000 for the
  // scoped DOM/event bridge. Measured: 1_257_293 / 334_411 / 269_585.
  // Bumped raw to 1_262_000 and gzip to 336_000 for the lifecycle-aware DOM
  // transaction (`window.__gosx.dom.reconcile`). Measured: 1_259_473 /
  // 334_869 / 269_861. The service publication adds 9 bytes to gzip and
  // 330 bytes to Brotli in the regenerated monolith.
  // Bumped raw 1_262_000 -> 1_270_000 for the framework-owned scoped
  // scheduler (keyed debounce and animation-frame cancellation). Measured:
  // Bumped gzip 336_000 -> 338_000 and brotli 271_000 -> 272_000 for the
  // shared failure-reporting policy. Measured: 1_265_227 / 336_292 /
  // 271_136.
  // Bumped raw 1_270_000 -> 1_275_000, gzip 338_000 -> 339_000, and Brotli
  // 272_000 -> 273_000 after regenerating the aggregate runtime with the
  // sampled Selena water-state bridge and physical-normal/topology contract.
  // Measured: 1_272_545 / 338_205 / 272_527. Selective route budgets remain
  // the deployment gate and are unchanged below.
  // v0.31.6 combines the Go-WASM lifecycle already on main with the scoped
  // runtime-surface modules. Measured: 1_291_115 / 342_587 / 276_762.
  // Bumped raw 1_295_000 -> 1_296_000 for v0.31.15's subtype-aware duck RTT
  // scheduler and retained projection-matrix contract. Measured: 1_295_043 /
  // 343_699 / 277_538; compressed budgets retain their prior ceilings.
  { file: "bootstrap.js", raw: 1_296_000, gzip: 344_000, brotli: 278_000 },
  // Bumped raw 124_000 -> 126_000, gzip 34_000 -> 35_000, brotli 29_000 ->
  // 30_000 for the same generic region/action/stream contracts. Bumped raw
  // 126_000 -> 129_000 for the core request transport bridge. Bumped raw
  // 129_000 -> 131_000 for latest-request coordination and response helpers.
  // Bumped raw 131_000 -> 134_000 for the shared runtime DOM replacement
  // lifecycle. Bumped raw 134_000 -> 137_000 and gzip 35_000 -> 36_000 for
  // core keyed block reconciliation. Measured: 134_038 / 35_153 / 30_819.
  // Bumped brotli 31_000 -> 32_000 for the core transport scope that owns
  // latest-request cancellation and parent lifecycle abort composition.
  // Measured: 136_788 / 35_895 / 31_387.
  // Bumped raw 137_000 -> 140_000 and gzip 36_000 -> 37_000 for the scoped
  // DOM/event bridge. Measured: 138_522 / 36_319 / 31_767.
  // Bumped brotli 32_000 -> 33_000 for the public telemetry namespace.
  // Measured: 139_617 / 36_585 / 32_004.
  // Bumped raw to 144_000 and gzip to 38_000 for the shared surface failure
  // reporting policy. Measured: 142_195 / 37_102 / 32_459.
  // Bumped raw 144_000 -> 147_000 for the framework-owned scoped scheduler.
  // Bumped gzip 38_000 -> 39_000 and brotli 33_000 -> 34_000 for the shared
  // failure-reporting policy. Measured: 146_370 / 38_093 / 33_312.
  // Bumped raw 147_000 -> 148_000 for fragment-aware streaming replacement,
  // which routes marker swaps through the core DOM lifecycle. Measured:
  // 147_702 / 38_506 / 33_521.
  // Bumped raw 148_000 -> 149_000 for scoped portal ownership, which cleans
  // up body-mounted overlays with their owning surface. Measured:
  // 148_081 / 38_641 / 33_659.
  // Bumped raw 149_000 -> 150_000 for lifecycle-aware keyed stream updates.
  // Measured: 149_154 / 38_850 / 33_866.
  { file: "bootstrap-runtime.js", raw: 155_000, gzip: 41_000, brotli: 36_000 },
  // Bumped raw 102_000 -> 105_000 for the same transport bridge. Bumped raw
  // 105_000 -> 107_000 for latest-request coordination. Bumped raw
  // 107_000 -> 110_000 for the shared runtime DOM replacement lifecycle.
  // Bumped raw 110_000 -> 113_000, gzip 28_000 -> 30_000, brotli 25_000 ->
  // 27_000 for core keyed block reconciliation. Measured: 110_218 /
  // 28_476 / 25_131.
  // Bumped raw 113_000 -> 116_000 for the scoped DOM/event bridge. Measured:
  // 114_701 / 29_611 / 26_117.
  // Bumped raw to 120_000 and gzip to 31_000 for the shared surface failure
  // reporting policy. Measured: 118_374 / 30_396 / 26_878.
  // Bumped raw 120_000 -> 123_000 for the framework-owned scoped scheduler.
  // Bumped gzip 31_000 -> 32_000 for the shared failure-reporting policy.
  // Measured: 122_545 / 31_423 / 27_675.
  // Bumped raw 123_000 -> 125_000 for fragment-aware streaming replacement.
  // Measured: 123_877 / 31_803 / 27_965.
  // Bumped raw 125_000 -> 126_000 and brotli 28_000 -> 29_000 for scoped
  // portal ownership. Measured: 124_256 / 31_923 / 28_023.
  // Bumped raw 126_000 -> 127_000 and gzip 32_000 -> 33_000 for
  // lifecycle-aware keyed stream updates. Measured: 125_329 / 32_131 /
  // 28_268.
  { file: "bootstrap-lite.js", raw: 131_000, gzip: 34_000, brotli: 30_000 },
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
  // Bumped raw 556_000 -> 640_000, gzip 152_000 -> 175_000, brotli 125_000 ->
  // 144_000: scene3d/water system modules (checkpoint f6c21364) — WaterSystem
  // geometry/material/lighting in the shared scene core. Measured 629_079 /
  // 171_055 / 140_608. (No 06/07 here — base-only primitives.)
  // Bumped raw 640_000 -> 665_000, gzip 175_000 -> 182_000, brotli 144_000 ->
  // 149_000: water-demo Selena convergence (see bootstrap.js note). Measured:
  // 660_045 / 180_076 / 147_794 + sub-1% rounding headroom.
  //
  // Bumped raw 665_000 -> 667_000, brotli 149_000 -> 150_000: E6 typed audio
  // authoring surface's dedicated "audio" hub-event cue path in
  // 20-scene-mount.js (see bootstrap.js note). gzip headroom unchanged.
  // Measured: 665_662 / 181_802 / 149_179 + sub-1% rounding headroom.
  //
  // Bumped raw 667_000 -> 673_000, gzip 182_000 -> 184_000, brotli 150_000 ->
  // 151_000: WebGL GPU-skinned Selena materials + gameplay post preset/FXAA
  // (see bootstrap.js note). Measured: 670_458 / 183_192 / 150_191 + sub-1%
  // rounding headroom.
  // Bumped raw 673_000 -> 675_000, gzip 184_000 -> 185_000, brotli 151_000 ->
  // 152_000 for the WebGL water performance slice above. Measured:
  // 673_640 / 184_371 / 151_257.
  // Shared fixed clock plus WebGL adaptive resources/timer ring. Measured:
  // 689_945 / 189_731 / 155_635.
  // Bumped raw 693_000 -> 696_000, gzip 191_000 -> 192_000, and Brotli
  // 157_000 -> 157_500 for the typed water surface-resolution and linear-HDR
  // contracts. Measured: 695_570 / 191_539 / 157_108.
  // Bumped raw 700_000 -> 701_000 and gzip 192_000 -> 193_000 for the
  // target-aware water RTT scheduler and retained projection matrices.
  // Measured: 700_047 / 192_112 / 158_295.
  { file: "bootstrap-feature-scene3d.js", raw: 701_000, gzip: 193_000, brotli: 159_000 },
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
  // Bumped raw 181_200 -> 315_000, gzip 45_500 -> 73_000, brotli 39_000 ->
  // 62_000: the water WGSL pipelines in 16a-scene-webgpu.js (WaterSystem pool /
  // caustics / reflection / refraction passes, checkpoint f6c21364) — the bulk
  // of this surface. Measured 309_066 / 71_882 / 60_545.
  // Bumped raw 315_000 -> 331_000, gzip 73_000 -> 77_000, brotli 62_000 ->
  // 64_500: water-demo Selena convergence (see bootstrap.js note) — the
  // descriptor-driven WGSL water renderer lands here. Measured: 328_062 /
  // 76_090 / 63_619 + sub-1% rounding headroom.
  // Bumped raw 331_000 -> 333_000, gzip 77_000 -> 78_000, brotli 64_500 ->
  // 65_000 for exact in-memory frame proof plus 4 Hz broad DOM diagnostics.
  // Measured: 331_872 / 76_994 / 64_334.
  // WebGPU fixed ticks plus timestamp ring and atomic adaptive RTT swaps.
  // Measured: 344_693 / 80_230 / 67_009.
  // Bumped raw 349_000 -> 351_000, gzip 81_500 -> 82_000, and Brotli 68_000
  // -> 68_500 for sampled water-state mirrors and physical-cell normal uniforms. Measured:
  // 350_203 / 81_776 / 68_108.
  { file: "bootstrap-feature-scene3d-webgpu.js", raw: 354_000, gzip: 83_000, brotli: 69_000 },
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
  //
  // Bumped raw 76_000 -> 78_500, gzip 24_000 -> 24_500, brotli 21_300 -> 21_700
  // for the window.__gosx_runtime_api bridge (+ full local fallback
  // implementations) of sceneRenderCamera, sceneLabelClassName,
  // normalizeTextLayoutOverflow, normalizeSceneLabelCollision/WhiteSpace/Align,
  // normalizeSceneHTMLMode/PointerEvents, and clamp01 in
  // 26b-feature-engines-prefix.js — normalizeEngineRenderBundle (30-tail.js)
  // referenced these across the feature-bundle IIFE boundary with nothing
  // defining them, throwing ReferenceError and silently dropping every
  // runtime:"shared" engine's render bundle on split-bundle pages that never
  // load bootstrap-feature-scene3d.js. Measured: 77_893 / 24_123 / 21_494,
  // plus sub-1% rounding headroom.
  //
  // Bumped raw 78_500 -> 85_000, gzip 24_500 -> 26_000, brotli 21_700 ->
  // 23_300: video-player-primitives — audioTracks/audioTrack, seekable/
  // isLive/liveEdgeLag, qualityLevels/qualityLevel, opt-in localStorage
  // preference persistence, and PiP + input-lock command/signal wiring in
  // the video engine factory (30-tail.js), merged with the runtime_api
  // bridge line above. Measured: 84_129 / 25_836 / 23_000 + sub-1%
  // rounding headroom.
  //
  // Bumped raw 85_000 -> 86_000, gzip 26_000 -> 26_500: hidden-at-hydration
  // canvas2d backing-store recovery in _initEngineSurfaceCanvasSize
  // (26b-feature-engines-prefix.js) — _canvasDeclaredSize +
  // _measuredCanvasCSSBox walk up the ancestor chain for a real (>1px) box
  // when the canvas's own getBoundingClientRect() is still self-referentially
  // collapsed (hydrated while hidden, or a host CSS selector that no longer
  // matches after a DOM-nesting change), and additionally observe that
  // ancestor so a later layout change re-triggers the fix. Measured:
  // 85_024 / 26_115 / 23_259; brotli unchanged.
  // Bumped for the Go-WASM engine lifecycle described by the monolith budget
  // above. Measured: 94_446 / 28_907 / 25_685.
  { file: "bootstrap-feature-engines.js", raw: 95_000, gzip: 29_500, brotli: 26_000 },
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
    // Bumped raw 183_000 -> 190_000, gzip 52_200 -> 54_000, brotli 46_200 ->
    // 47_500: bootstrap-runtime.js now carries the declarative interaction
    // primitives 06-declarative-actions.js + 07-declarative-regions.js
    // (data-gosx-action / -submit-on / -set / -region). Measured 187_357 /
    // 53_135 / 46_874, plus sub-1% rounding headroom.
    // Bumped raw 190_000 -> 191_000, brotli 47_500 -> 47_800 for the
    // engines-prefix window.__gosx_runtime_api bridge (see the
    // bootstrap-feature-engines.js budget note above). gzip headroom
    // unchanged. Measured: 190_224 / 53_972 / 47_629, plus rounding headroom.
    // Bumped gzip 54_000 -> 54_200 for CSRF token attachment in
    // 06-declarative-actions.js's actionFetch (gosxCSRFToken() reads
    // <meta name="csrf-token">, attaches X-CSRF-Token on POST/PUT/PATCH/
    // DELETE — see session.Manager.Protect). Measured: 190_508 / 54_077 /
    // 47_753, plus rounding headroom.
    // Bumped raw 191_000 -> 198_000, gzip 54_200 -> 56_500, brotli 47_800 ->
    // 50_000: video-player-primitives folded into the engines surface,
    // merged with the runtime_api bridge + CSRF lines above. Measured:
    // 196_744 / 55_790 / 49_259, plus sub-1% rounding headroom.
    // Bumped raw 198_000 -> 211_000, gzip 56_500 -> 60_000, brotli 50_000 ->
    // 50_500 for the generic runtime-surface lifecycle registry carried by
    // bootstrap-runtime.js. The current measured total is 205_984 / 58_060 /
    // 51_177, plus rounding headroom.
    // Bumped raw 211_000 -> 213_000 for the same core request transport
    // bridge carried by bootstrap-runtime.js. Bumped raw 213_000 -> 217_000,
    // gzip 60_000 -> 62_000, brotli 53_000 -> 54_000 for the shared runtime
    // DOM replacement lifecycle. Bumped raw 217_000 -> 220_000 for core
    // keyed block reconciliation. Measured: 218_167 / 60_989 / 53_819.
    // Bumped raw 220_000 -> 222_000, gzip 62_000 -> 62_500, brotli 54_000 ->
    // 55_000 for the scoped core transport requester. Measured: 220_917 /
    // 61_731 / 54_387.
    // Bumped raw 222_000 -> 224_000 for the scoped DOM/event bridge. Measured:
    // 222_651 / 62_155 / 54_767.
    // Bumped brotli 55_000 -> 56_000 for the public telemetry namespace.
    // Measured: 223_746 / 62_421 / 55_004.
    // Bumped raw to 228_000 for the shared surface failure reporting policy.
    // Bumped raw to 231_000 for the scoped surface scheduler. Measured:
    // 228_459 / 62_938 / 55_459.
    // Bumped brotli 56_000 -> 57_000 for the shared failure-reporting policy.
    // Current measured total: 230_499 / 63_929 / 56_312.
    // Bumped raw 231_000 -> 234_000 and gzip 64_000 -> 65_000 for the
    // fragment-aware stream lifecycle shared by runtime and engines routes.
    // Measured: 231_831 / 64_376 / 56_521.
    // Combined v0.31.6 selective route: 248_331 / 68_808 / 60_591.
    raw: 250_000,
    gzip: 70_000,
    brotli: 62_000,
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
