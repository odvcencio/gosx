// GoSX Scene3D Feature Chunk — loaded via <script defer>
//
// Contains the FULL scene pipeline: file 10 (scene-core) through file 20
// (scene-mount with the GoSXScene3D engine factory). This IIFE is
// self-contained — all scene functions are defined within it.
//
// A few functions from the runtime (00-textlayout.js and the selective
// runtime utility extraction) are needed by the scene code. These are bridged
// from window.__gosx_runtime_api.

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
  // sceneLabelLayoutCacheLimit is declared as a const in 00-textlayout.js
  // (RUNTIME_UTILS scope, not exported via extraction elsewhere) and is
  // used by 20-scene-mount.js's layoutSceneLabel() to bound the per-label
  // layout cache. Without this bridge, any page mounting a Scene3D Label
  // node throws "ReferenceError: sceneLabelLayoutCacheLimit is not
  // defined" the first time a label lays out, because this bundle runs in
  // its own IIFE separate from the runtime bundle that defines it.
  var sceneLabelLayoutCacheLimit = runtimeApi.sceneLabelLayoutCacheLimit || 512;

  // --- file 10 (runtime-scene-core.js) is concatenated next, followed by files 11-20 ---
