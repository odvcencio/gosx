// GoSX Scene3D compute sub-feature — fetched on demand.
//
// 16b-scene-compute.js holds the WGSL particle simulation, the CPU particle
// fallback, the particle force registry and the GPU instanced-cull system. It
// cost every Scene3D page 30_189 raw / 8_772 gzip / 7_409 brotli minified
// bytes, and a scene with one cube and one directional light runs none of it.
//
// The base chunk fetches this chunk when the scene declares a compute particle
// system, or an instanced mesh that can carry a GPU cull kernel. See
// ensureComputeFeatureLoaded and settleSceneComputeFeature in
// 20b-scene-mount-webgl-chunk.js.
//
// The suffix assigns every published symbol onto window.__gosx_scene3d_api,
// which is the object the WebGL and WebGPU renderers already read for the
// cull path. The two names those renderers used to snapshot at load time,
// createSceneParticleSystem and sceneComputeSystemSignature, now resolve
// through that object at call time, because this chunk lands after them.

(function() {
  "use strict";

  // Bail out when the base scene3d chunk did not run. Every bridge below
  // resolves through its API object, so none of them can work alone.
  if (!window.__gosx_scene3d_api) {
    console.warn("[gosx] scene3d-compute chunk loaded without main scene3d bundle");
    return;
  }

  var sceneApi = window.__gosx_scene3d_api;

  // --- Scalars and colour (10-runtime-primitives.js, 11-scene-math.js).
  var sceneNumber = sceneApi.sceneNumber || function(value, fallback) {
    var num = Number(value);
    return Number.isFinite(num) ? num : fallback;
  };
  var sceneBool = sceneApi.sceneBool || function(value, fallback) {
    return value == null ? fallback : !!value;
  };
  var clamp01 = sceneApi.clamp01 || function(value) {
    return Math.max(0, Math.min(1, Number(value) || 0));
  };
  var sceneColorRGBA = sceneApi.sceneColorRGBA || function() { return [1, 1, 1, 1]; };

  // --- file 16b (scene-compute.js) is concatenated next ---
