// GoSX Scene3D Animation Sub-Feature — loaded on-demand when a scene
// contains keyframe animations or skeletal clips.
//
// Pulled out of bootstrap-feature-scene3d.js so the large majority of
// scenes (static meshes, points, procedural geometry, data viz) don't
// have to parse ~16KB of keyframe interpolation / quaternion slerp /
// skeletal bone math they'll never execute.
//
// The main scene3d bundle's mount code lazy-fetches this chunk via
// ensureAnimationFeatureLoaded() the first time a GLTF asset with an
// animation clip is mounted, the first time a scene with animated
// skinned meshes mounts, or via an explicit request from consumer code
// that calls window.__gosx_scene3d_animation_api.createMixer().
//
// All dependencies on the main scene3d bundle (matrix math helpers in
// 11-scene-math.js) are resolved via globals that the main bundle
// publishes before this chunk runs.

(function() {
  "use strict";

  var sceneApi = window.__gosx_scene3d_api || {};
  var SCENE_IDENTITY_MAT4 = sceneApi.SCENE_IDENTITY_MAT4;
  var sceneMat4Multiply = sceneApi.sceneMat4Multiply;
  var sceneMat4MultiplyInto = sceneApi.sceneMat4MultiplyInto;
  var sceneTRSToMat4 = sceneApi.sceneTRSToMat4;
  var sceneTRSToMat4Into = sceneApi.sceneTRSToMat4Into;
  var _sceneMat4ScratchA = sceneApi._sceneMat4ScratchA || new Float32Array(16);
  var _sceneMat4ScratchB = sceneApi._sceneMat4ScratchB || new Float32Array(16);
  var _animScratch3 = sceneApi._animScratch3 || [0, 0, 0];
  var _animScratch4 = sceneApi._animScratch4 || [0, 0, 0, 0];
