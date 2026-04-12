// GoSX Scene3D Feature Chunk — loaded via <script defer>
//
// Contains the FULL scene pipeline: file 10 (scene-core with normalizers,
// state builders, bundle builders) through file 20 (scene-mount with the
// GoSXScene3D engine factory). This IIFE is self-contained — all scene
// functions are defined within it via the concatenated source files.
//
// A few infrastructure utilities (engineFrame, sceneNumber, clearChildren,
// etc.) are duplicated between this chunk and the runtime's 10a-runtime-utils.
// That's intentional: each IIFE has its own copy. The runtime uses them for
// input providers and feature API; the scene chunk uses them for rendering.

(function() {
  "use strict";

  // setSharedSignalValue is defined in 26-runtime-tail.js (the runtime).
  // The scene core references it for input signal flushing. Bridge it
  // from the global that the runtime exposes.
  var setSharedSignalValue = window.__gosx_set_shared_signal_value || function() {};

  // --- file 10 (runtime-scene-core.js) is concatenated next ---
