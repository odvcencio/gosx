// GoSX Scene3D WebGL2 post-effects sub-feature, loaded on demand.
//
// Temporal anti-aliasing with its motion-vector pass, depth of field and auto
// exposure for the WebGL2 renderer. The page advertises this chunk
// (data-gosx-scene3d-postfx-webgl-url) only when a scene declares a "taa",
// "dof" or "motionBlur" post effect or environment.autoExposure. The mount
// fetches it together with the WebGL renderer chunk (or the monolith); without
// it the renderer's stand-ins skip those passes and "taa" falls back to FXAA.
//
// The modules read helpers that live in the renderer chunk. The renderer hands
// them over with its first call (postfxBind), so load order does not matter.
(function() {
  "use strict";
  var sceneApi = window.__gosx_scene3d_api;
  if (!sceneApi) {
    console.warn("[gosx] scene3d-postfx-webgl chunk loaded without main scene3d bundle");
    return;
  }
  var sceneNumber = sceneApi.sceneNumber || function(v, d) { var n = Number(v); return Number.isFinite(n) ? n : d; };
  var sceneMat4MultiplyInto = sceneApi.sceneMat4MultiplyInto;
  var SCENE_POST_CUSTOM_POST = sceneApi.SCENE_POST_CUSTOM_POST || "customPost";
  var SCENE_POST_VIEW_POSITION_GLSL, createSceneFullscreenQuad, createScenePostFBO, createScenePostProgram, disposeScenePostFBO,
    drawSceneFullscreenQuad, objectIsSkinned, sceneInvertOrthonormalView, scenePBRCreateProgramInfo, scenePBRProgramReady,
    scenePBRRenderTruth, sceneWaterUniformLocation;
  function postfxBind(h) {
    if (!h) return;
    SCENE_POST_VIEW_POSITION_GLSL = h.SCENE_POST_VIEW_POSITION_GLSL; createSceneFullscreenQuad = h.createSceneFullscreenQuad;
    createScenePostFBO = h.createScenePostFBO; createScenePostProgram = h.createScenePostProgram; disposeScenePostFBO = h.disposeScenePostFBO;
    drawSceneFullscreenQuad = h.drawSceneFullscreenQuad; objectIsSkinned = h.objectIsSkinned; sceneInvertOrthonormalView = h.sceneInvertOrthonormalView;
    scenePBRCreateProgramInfo = h.scenePBRCreateProgramInfo; scenePBRProgramReady = h.scenePBRProgramReady;
    scenePBRRenderTruth = h.scenePBRRenderTruth; sceneWaterUniformLocation = h.sceneWaterUniformLocation;
  }
