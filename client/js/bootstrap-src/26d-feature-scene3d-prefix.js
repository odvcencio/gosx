// GoSX Scene3D Feature Chunk — loaded via <script defer>
//
// Contains the FULL scene pipeline: file 10 (scene-core) through file 20
// (scene-mount with the GoSXScene3D engine factory). This IIFE is
// self-contained — all scene functions are defined within it.
//
// A few functions from the runtime (00-textlayout.js, 10a-runtime-utils.js)
// are needed by the scene code. These are bridged from window.__gosx_runtime_api
// and duplicated declarations from the runtime-utils extraction.

(function() {
  "use strict";

  // Bridge runtime utilities that live in the runtime's IIFE scope.
  // These are exported by 00-textlayout.js to window.__gosx_runtime_api.
  var runtimeApi = window.__gosx_runtime_api || {};
  var setAttrValue = runtimeApi.setAttrValue || function() {};
  var setStyleValue = runtimeApi.setStyleValue || function() {};
  var gosxSubscribeSharedSignal = runtimeApi.gosxSubscribeSharedSignal || function() { return function() {}; };
  var setSharedSignalValue = runtimeApi.setSharedSignalValue || function() {};
  var gosxTextLayoutRevision = runtimeApi.gosxTextLayoutRevision || function() { return 0; };
  var normalizeTextLayoutOverflow = runtimeApi.normalizeTextLayoutOverflow || function() { return "ellipsis"; };
  var layoutBrowserText = runtimeApi.layoutBrowserText || function() { return null; };
  var applyTextLayoutPresentation = runtimeApi.applyTextLayoutPresentation || function() {};
  var onTextLayoutInvalidated = runtimeApi.onTextLayoutInvalidated || function() { return function() {}; };

  // --- file 10 (runtime-scene-core.js) is concatenated next, followed by files 11-20 ---
