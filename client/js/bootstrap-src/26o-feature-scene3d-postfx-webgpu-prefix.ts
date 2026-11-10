// GoSX Scene3D WebGPU post-effects sub-feature, loaded on demand.
//
// Temporal anti-aliasing with its motion vectors (motion blur reads them too),
// depth of field and auto exposure for the WebGPU renderer. The page
// advertises this chunk (data-gosx-scene3d-postfx-webgpu-url) only when a
// scene declares a "taa", "dof" or "motionBlur" post effect or
// environment.autoExposure. The mount fetches it together with the WebGPU
// renderer chunk (or the monolith); without it the renderer's stand-ins skip
// those passes and "taa" falls back to FXAA.
//
// The modules read helpers that live in the renderer chunk. The renderer hands
// them over with its first call (postfxBind), so load order does not matter.
(function() {
  "use strict";
  var sceneApi = window.__gosx_scene3d_api;
  if (!sceneApi) {
    console.warn("[gosx] scene3d-postfx-webgpu chunk loaded without main scene3d bundle");
    return;
  }
  var sceneNumber = sceneApi.sceneNumber || function(v, d) { var n = Number(v); return Number.isFinite(n) ? n : d; };
  var sceneMat4MultiplyInto = sceneApi.sceneMat4MultiplyInto;
  var wgpuCreateValidatedPipeline, wgpuRequirePipeline, WGSL_FRAME_STRUCTS, WGSL_POINTS_SHADE, WGSL_POINTS_UNIFORM_FIELDS;
  function postfxBind(h) {
    if (!h) return;
    wgpuCreateValidatedPipeline = h.wgpuCreateValidatedPipeline; wgpuRequirePipeline = h.wgpuRequirePipeline;
    WGSL_FRAME_STRUCTS = h.WGSL_FRAME_STRUCTS; WGSL_POINTS_SHADE = h.WGSL_POINTS_SHADE; WGSL_POINTS_UNIFORM_FIELDS = h.WGSL_POINTS_UNIFORM_FIELDS;
  }
