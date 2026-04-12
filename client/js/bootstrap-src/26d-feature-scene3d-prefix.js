// GoSX Scene3D Feature Chunk — loaded async via <script async>
// Wraps files 11-20 (scene-math through scene-mount) in a self-contained
// IIFE that destructures shared utilities from window.__gosx_scene3d_api,
// which the runtime (10-runtime-scene-core.js) exports before this loads.
//
// This file is the PREFIX — build-bootstrap.mjs concatenates files 11-20
// after it, then closes with 26d-feature-scene3d-suffix.js.

(function() {
  "use strict";

  var api = window.__gosx_scene3d_api;
  if (!api) {
    console.error("[gosx] scene3d feature loaded before runtime — __gosx_scene3d_api missing");
    return;
  }

  // Destructure shared utilities so files 11-20 reference them by their
  // original closure names. Zero source changes needed in those files.
  var appendSceneObjectToBundle = api.appendSceneObjectToBundle;
  var appendSceneSurfaceToBundle = api.appendSceneSurfaceToBundle;
  var applySceneCommands = api.applySceneCommands;
  var cancelEngineFrame = api.cancelEngineFrame;
  var clearChildren = api.clearChildren;
  var createSceneRenderBundle = api.createSceneRenderBundle;
  var createSceneState = api.createSceneState;
  var createSceneWebGLRenderer = api.createSceneWebGLRenderer;
  var engineFrame = api.engineFrame;
  var normalizeSceneEnvironment = api.normalizeSceneEnvironment;
  var normalizeSceneLabel = api.normalizeSceneLabel;
  var normalizeSceneLabelAlign = api.normalizeSceneLabelAlign;
  var normalizeSceneLabelCollision = api.normalizeSceneLabelCollision;
  var normalizeSceneLabelWhiteSpace = api.normalizeSceneLabelWhiteSpace;
  var normalizeSceneLight = api.normalizeSceneLight;
  var normalizeSceneObject = api.normalizeSceneObject;
  var normalizeSceneSprite = api.normalizeSceneSprite;
  var normalizeSceneSpriteFit = api.normalizeSceneSpriteFit;
  var publishPointerSignals = api.publishPointerSignals;
  var queueInputSignal = api.queueInputSignal;
  var sceneAdvanceTransitions = api.sceneAdvanceTransitions;
  var sceneApplyLiveEvent = api.sceneApplyLiveEvent;
  var sceneBool = api.sceneBool;
  var sceneBoundsDepthMetrics = api.sceneBoundsDepthMetrics;
  var sceneBoundsViewCulled = api.sceneBoundsViewCulled;
  var sceneBundleNeedsThickLines = api.sceneBundleNeedsThickLines;
  var sceneCameraEquivalent = api.sceneCameraEquivalent;
  var sceneHasActiveTransitions = api.sceneHasActiveTransitions;
  var sceneLabelAnimated = api.sceneLabelAnimated;
  var sceneMeshMaterialArray = api.sceneMeshMaterialArray;
  var sceneModels = api.sceneModels;
  var sceneNormalizeDirection = api.sceneNormalizeDirection;
  var sceneNowMilliseconds = api.sceneNowMilliseconds;
  var sceneNumber = api.sceneNumber;
  var sceneObjectAnimated = api.sceneObjectAnimated;
  var scenePointStyleCode = api.scenePointStyleCode;
  var scenePrimeInitialTransitions = api.scenePrimeInitialTransitions;
  var sceneProps = api.sceneProps;
  var sceneRenderCamera = api.sceneRenderCamera;
  var sceneResolveLightingEnvironment = api.sceneResolveLightingEnvironment;
  var sceneSpriteAnimated = api.sceneSpriteAnimated;
  var sceneStateLabels = api.sceneStateLabels;
  var sceneStateLights = api.sceneStateLights;
  var sceneStateObjects = api.sceneStateObjects;
  var sceneStateSprites = api.sceneStateSprites;
  var sceneTypedFloatArray = api.sceneTypedFloatArray;
  var translateScenePointInto = api.translateScenePointInto;

  // --- files 11 through 20 are concatenated below by build-bootstrap.mjs ---
