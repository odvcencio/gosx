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
  var gosxOwnDataArray = runtimeApi.ownDataArray;
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
  // gosxLowEndHardware is the single source of truth for the device-capability
  // gate (05-document-env.js). sceneCapabilityProfile in 20-scene-mount.js
  // calls it when the environment snapshot carries no lowEndHardware flag.
  // Treat a missing bridge as "not low end", which is the safe default.
  var gosxLowEndHardware = runtimeApi.gosxLowEndHardware || function() { return false; };

  // Runtime helpers this chunk used to duplicate. bootstrap-feature-scene3d.js
  // carried a full copy of 10-runtime-scene-utils.js — 42_000 source bytes the
  // Chromium Scene3D route downloaded twice, once inside bootstrap-runtime.js.
  // 10-runtime-scene-utils.js publishes these eight names on
  // window.__gosx_runtime_api, so this chunk reads them instead.
  //
  // The fallbacks below keep the chunk usable if a page somehow loads it
  // without the runtime bundle. They are deliberately conservative: capability
  // checks pass, signal queues drop, and frames still run.
  var browserCapabilitySupported = runtimeApi.browserCapabilitySupported || function() { return true; };
  // loadManifest and gosxApplyCurrentScriptNonce are read behind
  // `typeof X === "function"` guards (16z-scene-webgpu-probe.js,
  // 20-scene-mount.js). Leave them undefined when the runtime is absent so the
  // guards keep working.
  var loadManifest = runtimeApi.loadManifest;
  var gosxApplyCurrentScriptNonce = runtimeApi.gosxApplyCurrentScriptNonce;
  var cancelEngineFrame = runtimeApi.cancelEngineFrame || function(handle) {
    if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(handle);
    else clearTimeout(handle);
  };
  var engineFrame = runtimeApi.engineFrame || function(callback) {
    if (typeof window.requestAnimationFrame === "function") return window.requestAnimationFrame(callback);
    return setTimeout(function() { callback(Date.now()); }, 16);
  };
  var publishPointerSignals = runtimeApi.publishPointerSignals || function() {};
  var queueInputSignal = runtimeApi.queueInputSignal || function() {};
  var runtimeCapabilityStatus = runtimeApi.runtimeCapabilityStatus || function(entry) {
    return { ok: true, requested: [], required: [], supported: [], missing: [] };
  };
  var engineCapabilityStatus = runtimeApi.engineCapabilityStatus || runtimeCapabilityStatus;
  var sceneNumber = runtimeApi.sceneNumber || function(value, fallback) {
    var num = Number(value);
    return Number.isFinite(num) ? num : fallback;
  };

  // --- file 10 (runtime-scene-core.js) is concatenated next, followed by files 11-20 ---
