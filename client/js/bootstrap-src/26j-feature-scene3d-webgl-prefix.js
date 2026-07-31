// GoSX Scene3D WebGL Sub-Feature — loaded on demand
//
// Pulled out of bootstrap-feature-scene3d.js so a WebGPU-capable browser
// never downloads the WebGL2 renderer it will never run. A Chromium Scene3D
// page used to fetch BOTH backends and draw with one of them.
//
// 20-scene-mount.js fetches this chunk in three cases:
//   1. The mount path resolves WebGL as the chosen backend. Safari and
//      Firefox on most platforms land here, and so does any page that sets
//      ForceWebGL, RequireWebGL or PreferWebGL.
//   2. A WebGPU page loses its GPU device, or hits a persistent frame error,
//      and the fallback ladder steps down to WebGL.
//   3. A water scene needs the WebGL2 water runtime.
// See ensureWebGLFeatureLoaded and settlePreferredWebGLBackend in
// 20-scene-mount.js.
//
// All cross-IIFE dependencies are bridged below, the same way
// 26e-feature-scene3d-webgpu-prefix.js bridges the WebGPU chunk. Two window
// namespaces carry them:
//   - window.__gosx_scene3d_api           (base scene3d chunk)
//
// This chunk always loads AFTER the base scene3d chunk, so that namespace is
// populated by the time this IIFE runs.
//
// Adding a new reference from 16-scene-webgl.js to a base symbol REQUIRES a
// new bridge line here. Without it the browser throws ReferenceError on the
// first WebGL frame, and no test in this repo can see that without a real
// GPU. Verify with the free-identifier scan described in
// 16c-scene-shared-pbr.js.

(function() {
  "use strict";

  // Bail early if the base scene3d chunk did not run. Someone loaded this
  // chunk in isolation, and none of the bridges below can resolve.
  if (!window.__gosx_scene3d_api) {
    console.warn("[gosx] scene3d-webgl chunk loaded without main scene3d bundle");
    return;
  }

  var sceneApi = window.__gosx_scene3d_api;

  // --- Primitives and scalar helpers (10-runtime-primitives.js,
  // 10-runtime-scene-core.js, 11-scene-math.js, 15a-scene-postfx-shared.js).
  var sceneBool = sceneApi.sceneBool || function(v, d) { return v == null ? d : !!v; };
  var sceneNumber = sceneApi.sceneNumber || function(v, d) { var n = Number(v); return Number.isFinite(n) ? n : d; };
  var clamp01 = sceneApi.clamp01 || function(v) { return Math.max(0, Math.min(1, Number(v) || 0)); };
  var sceneFiniteNumber = sceneApi.sceneFiniteNumber || function(v, d) {
    return typeof v === "number" && isFinite(v) ? v : d;
  };
  var sceneColorRGBA = sceneApi.sceneColorRGBA || function() { return [0, 0, 0, 1]; };
  var sceneMat4Multiply = sceneApi.sceneMat4Multiply;
  var sceneMat4MultiplyInto = sceneApi.sceneMat4MultiplyInto;
  var sceneRenderCamera = sceneApi.sceneRenderCamera || function(c) { return c; };
  var scenePointStyleCode = sceneApi.scenePointStyleCode || function() { return 0; };
  var sceneIsNumericTypedArray = sceneApi.sceneIsNumericTypedArray || function(value) {
    return value != null &&
      typeof value === "object" &&
      typeof value.length === "number" &&
      typeof ArrayBuffer !== "undefined" &&
      typeof ArrayBuffer.isView === "function" &&
      ArrayBuffer.isView(value) &&
      Object.prototype.toString.call(value) !== "[object DataView]";
  };
  var sceneCanvasAlpha = sceneApi.sceneCanvasAlpha || function() { return true; };
  var notifySceneTextureLoaded = sceneApi.notifySceneTextureLoaded;

  // --- Culling (11-scene-math.js). The WebGL2 CPU-cull fallback calls both.
  var extractFrustumPlanesJS = sceneApi.extractFrustumPlanesJS;
  var instancePassesCullTest = sceneApi.instancePassesCullTest;

  // --- Material identity (13-scene-material.js).
  var normalizeSceneMaterialKind = sceneApi.normalizeSceneMaterialKind;
  var sceneMaterialProfileKey = sceneApi.sceneMaterialProfileKey;

  // --- Draw planning (15b-scene-planner.js).
  var prepareScene = sceneApi.prepareScene || function(ir) { return { ir: ir, pbrPasses: null }; };
  var scenePreparedCommandSequence = sceneApi.scenePreparedCommandSequence || function() { return []; };
  var sceneCachedBuffer = sceneApi.sceneCachedBuffer;

  // --- Backend registry (15c-scene-backend-registry.js). The tail of
  // 16-scene-webgl.js registers the "webgl" backend on it when this chunk
  // finishes loading.
  var sceneBackendRegistry = sceneApi.sceneBackendRegistry;

  // --- Legacy vertex-color WebGL renderer. It no longer needs a bridge:
  // 16e-scene-webgl-legacy.js now ships in this chunk, so createSceneWebGLRenderer,
  // createSceneWebGLProgram, createSceneWebGLResources, disposeSceneWebGLRenderer
  // and renderSceneWebGLWorldBundle are lexical here. Re-declaring them as
  // `var x = sceneApi.x` would overwrite the hoisted function declarations with
  // undefined, because both live in this one IIFE scope.

  // --- Typed float coercion (10-runtime-scene-core.js). Mesh and animation
  // normalization call it on every backend, so it stayed in the base chunk
  // while the renderer that also calls it moved here.
  var sceneTypedFloatArray = sceneApi.sceneTypedFloatArray;

  // --- Camera helpers (10-runtime-scene-core.js) and draw planning
  // (15-scene-draw-plan.js, 13-scene-material.js). The legacy renderer in
  // 16e reads all of these; they had no consumer outside the base IIFE
  // until 16e moved into this chunk.
  var sceneCameraEquivalent = sceneApi.sceneCameraEquivalent;
  var sceneOrthographicBounds = sceneApi.sceneOrthographicBounds;
  var buildSceneWorldDrawPlan = sceneApi.buildSceneWorldDrawPlan;
  var createSceneWorldDrawScratch = sceneApi.createSceneWorldDrawScratch;
  var compareSceneWorldPassEntries = sceneApi.compareSceneWorldPassEntries;
  var sceneWorldObjectRenderPass = sceneApi.sceneWorldObjectRenderPass;
  var sceneWorldObjectRenderable = sceneApi.sceneWorldObjectRenderable;
  var sceneFallbackMaterialData = sceneApi.sceneFallbackMaterialData;
  var sceneMaterialEmissive = sceneApi.sceneMaterialEmissive;
  var sceneMaterialOpacity = sceneApi.sceneMaterialOpacity;
  var sceneMaterialShaderData = sceneApi.sceneMaterialShaderData;

  // --- Water clock (10-runtime-scene-core.js). The WebGL2 water runtime
  // shares the fixed clock with the WebGPU runtime.
  var sceneWaterAdvanceClock = sceneApi.sceneWaterAdvanceClock;
  var sceneWaterResetClock = sceneApi.sceneWaterResetClock;

  // --- Compute particles. WebGL drives the CPU particle path through the same
  // system objects WebGPU uses. 16b-scene-compute.js now ships in
  // bootstrap-feature-scene3d-compute.js, which the mount fetches only for a
  // scene that declares particles or instanced meshes, and which can land
  // after this chunk. Resolve at CALL time: a load-time snapshot would freeze
  // `undefined` and the first particle frame would throw.
  function createSceneParticleSystem(device, entry) {
    var api = window.__gosx_scene3d_api;
    if (!api || typeof api.createSceneParticleSystem !== "function") {
      return null;
    }
    return api.createSceneParticleSystem(device, entry);
  }

  function sceneComputeSystemSignature(entry) {
    var api = window.__gosx_scene3d_api;
    if (!api || typeof api.sceneComputeSystemSignature !== "function") {
      return "";
    }
    return api.sceneComputeSystemSignature(entry);
  }

  // --- Post-FX scalars (15a-scene-postfx-shared.js). The texture-unit table
  // and the Radiance HDR decoder are lexical in this chunk now: 15a1 and 16b-
  // scene-hdr.js ship here, beside the only renderer that reads them.
  var resolvePostFXFactor = sceneApi.resolvePostFXFactor || function() { return 1; };
  var resolveShadowSize = sceneApi.resolveShadowSize || function(s) { return s; };

  // --- Backend-agnostic PBR helpers (16c-scene-shared-pbr.js). These stayed
  // in the base chunk because 15b, 10-runtime-scene-core and the WebGPU chunk
  // read them too.
  var SCENE_POST_TONE_MAPPING = sceneApi.SCENE_POST_TONE_MAPPING || "toneMapping";
  var SCENE_POST_BLOOM = sceneApi.SCENE_POST_BLOOM || "bloom";
  var SCENE_POST_VIGNETTE = sceneApi.SCENE_POST_VIGNETTE || "vignette";
  var SCENE_POST_COLOR_GRADE = sceneApi.SCENE_POST_COLOR_GRADE || "colorGrade";
  var SCENE_POST_SSAO = sceneApi.SCENE_POST_SSAO || "ssao";
  var SCENE_POST_DOF = sceneApi.SCENE_POST_DOF || "dof";
  var SCENE_POST_CUSTOM_POST = sceneApi.SCENE_POST_CUSTOM_POST || "customPost";
  var SCENE_POST_FXAA = sceneApi.SCENE_POST_FXAA || "fxaa";
  var scenePBRViewMatrix = sceneApi.scenePBRViewMatrix;
  var scenePBRProjectionMatrixForCamera = sceneApi.scenePBRProjectionMatrixForCamera;
  var sceneShadowLightSpaceMatrix = sceneApi.sceneShadowLightSpaceMatrix;
  var sceneShadowComputeBounds = sceneApi.sceneShadowComputeBounds;
  var sceneShadowComputeCascadeSplits = sceneApi.sceneShadowComputeCascadeSplits;
  var sceneShadowFrustumSubCorners = sceneApi.sceneShadowFrustumSubCorners;
  var sceneInvertOrthonormalView = sceneApi.sceneInvertOrthonormalView;
  var sceneShadowFitLightSpaceOrtho = sceneApi.sceneShadowFitLightSpaceOrtho;
  var scenePBRObjectRenderPass = sceneApi.scenePBRObjectRenderPass;
  var scenePBRDepthSort = sceneApi.scenePBRDepthSort;
  var generateInstancedGeometry = sceneApi.generateInstancedGeometry;
  var normalizeInstancedGeometryKind = sceneApi.normalizeInstancedGeometryKind;
  var hashLightContent = sceneApi.hashLightContent;
  var hashEnvironmentContent = sceneApi.hashEnvironmentContent;

  // --- file 16 (scene-webgl.js) is concatenated next.
