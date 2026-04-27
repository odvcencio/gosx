// GoSX Scene3D WebGPU Sub-Feature — loaded via <script defer>
//
// Pulled out of bootstrap-feature-scene3d.js so pages that only use the
// WebGL renderer (Safari, Firefox on most platforms, any page with
// ForceWebGL) don't have to parse ~108KB of WebGPU code they'll never run.
//
// This sub-feature is loaded by the Go renderer ONLY when navigator.gpu
// is likely to exist (see island renderer emit). It publishes its
// factory to window.__gosx_scene3d_webgpu_api, which the main scene3d
// bundle's stub probe picks up on the next render tick.
//
// All cross-IIFE dependencies are bridged via:
//   - window.__gosx_scene3d_api  (set by main scene3d bundle)
//   - window.__gosx_runtime_api  (set by runtime bundle)
//
// Because this chunk loads AFTER the main scene3d bundle, both APIs are
// guaranteed to be populated by the time this IIFE runs.

(function() {
  "use strict";

  // Bail early if the main scene3d bundle didn't run (e.g., someone
  // loaded this chunk in isolation).
  if (!window.__gosx_scene3d_api) {
    console.warn("[gosx] scene3d-webgpu chunk loaded without main scene3d bundle");
    return;
  }

  var sceneApi = window.__gosx_scene3d_api;
  var runtimeApi = window.__gosx_runtime_api || {};

  // --- Scene math / geometry / material helpers used by 16a + 16b.
  // Pulled from window.__gosx_scene3d_api (main bundle export). If any
  // of these are undefined the webgpu renderer will fail at runtime
  // and fall back to webgl — that's the intended failure mode.
  var sceneBool = sceneApi.sceneBool || function(v, d) { return v == null ? d : !!v; };
  var sceneNumber = sceneApi.sceneNumber || function(v, d) { var n = Number(v); return Number.isFinite(n) ? n : d; };
  var clamp01 = sceneApi.clamp01 || function(v) { return Math.max(0, Math.min(1, Number(v) || 0)); };
  var SCENE_POST_TONE_MAPPING = sceneApi.SCENE_POST_TONE_MAPPING || "toneMapping";
  var SCENE_POST_BLOOM = sceneApi.SCENE_POST_BLOOM || "bloom";
  var SCENE_POST_VIGNETTE = sceneApi.SCENE_POST_VIGNETTE || "vignette";
  var SCENE_POST_COLOR_GRADE = sceneApi.SCENE_POST_COLOR_GRADE || "colorGrade";
  var SCENE_POST_SSAO = sceneApi.SCENE_POST_SSAO || "ssao";
  var SCENE_POST_DOF = sceneApi.SCENE_POST_DOF || "dof";
  var sceneColorRGBA = sceneApi.sceneColorRGBA || function() { return [0, 0, 0, 1]; };
  var scenePointStyleCode = sceneApi.scenePointStyleCode || function() { return 0; };
  var sceneRenderCamera = sceneApi.sceneRenderCamera || function(c) { return c; };
  var scenePBRDepthSort = sceneApi.scenePBRDepthSort;
  var scenePBRObjectRenderPass = sceneApi.scenePBRObjectRenderPass;
  var prepareScene = sceneApi.prepareScene || function(ir) { return { ir: ir, pbrPasses: null }; };
  var scenePreparedCommandSequence = sceneApi.scenePreparedCommandSequence || function() { return []; };
  var sceneCachedBuffer = sceneApi.sceneCachedBuffer;
  var scenePBRProjectionMatrix = sceneApi.scenePBRProjectionMatrix;
  var scenePBRProjectionMatrixForCamera = sceneApi.scenePBRProjectionMatrixForCamera;
  var scenePBRViewMatrix = sceneApi.scenePBRViewMatrix;
  var sceneShadowLightSpaceMatrix = sceneApi.sceneShadowLightSpaceMatrix;
  var sceneShadowComputeBounds = sceneApi.sceneShadowComputeBounds;
  var resolvePostFXFactor = sceneApi.resolvePostFXFactor || function() { return 1; };
  var resolveShadowSize = sceneApi.resolveShadowSize || function(s) { return s; };
  // createSceneParticleSystem + sceneComputeSystemSignature are defined
  // by 16b-scene-compute.js concatenated into this same IIFE below —
  // no bridge needed.

  // Adapter + device probe result shared with the main bundle. The
  // probe is asynchronous: when this chunk first runs, the probe may
  // still be pending. We re-call the function each time we need the
  // result so sceneWebGPUAvailable reflects the current probe state,
  // not a snapshot taken at chunk-load time.
  //
  // The probe owns the lifecycle of both the adapter AND the device
  // (see 16z-scene-webgpu-probe.js). The renderer in 16a reuses the
  // probed device instead of requesting its own, which is what lets
  // the factory be synchronous and guarantees we never taint the
  // canvas with a WebGPU context for a device that doesn't actually
  // work.
  function _externalProbe() {
    if (typeof window.__gosx_scene3d_webgpu_probe === "function") {
      return window.__gosx_scene3d_webgpu_probe();
    }
    return { adapter: null, device: null, ready: false };
  }

  // --- file 16a (scene-webgpu.js) is concatenated next, followed by 16b.
