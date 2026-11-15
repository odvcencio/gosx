// GoSX Scene3D Selena lighting sub-feature, loaded on demand.
//
// The page advertises this chunk (data-gosx-scene3d-selena-lighting-url) only
// when one of its Selena materials reads the scene lighting block: lights,
// shadow slots and cascades, fog, or the camera forward vector. The WebGL and
// WebGPU chunks request it on first use (see the selena shims in 26j/26e) and
// draw without it until it lands. The full monolith carries 16c2 inline.
//
// 16c2 reads two scene-core helpers; bridge them from the base chunk.
(function() {
  "use strict";
  var sceneApi = window.__gosx_scene3d_api;
  if (!sceneApi) {
    console.warn("[gosx] scene3d-selena-lighting chunk loaded without main scene3d bundle");
    return;
  }
  var sceneNumber = sceneApi.sceneNumber || function(v, d) { var n = Number(v); return Number.isFinite(n) ? n : d; };
  var sceneColorRGBA = sceneApi.sceneColorRGBA || function(_value, fallback) { return fallback || [0, 0, 0, 1]; };
