// Inline wrapper for the text-layout engine in the monolithic bootstrap.js.
//
// The engine runs in its own IIFE in every bundle, so one code path serves both
// the inline copy and the lazily fetched bootstrap-feature-textlayout.js chunk.
// The engine reads its shared helpers from window.__gosx_runtime_api, which
// 00-textlayout.js assigns as its last statement.

(function() {
  "use strict";

