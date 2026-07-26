// GoSX text-layout feature chunk — fetched on demand.
//
// The runtime asks for this chunk when the document holds a
// data-gosx-text-layout element, or when the manifest mounts a Scene3D engine,
// because Scene3D labels lay out through layoutBrowserText. A page with neither
// never downloads it.
//
// Registration follows the same convention as islands, engines, hubs and
// controllers: window.__gosx_register_bootstrap_feature(name, factory).
// bootstrap-lite.js carries no feature host, so the suffix runs the engine
// straight away in that case.

(function() {
  "use strict";

  function runTextLayoutEngine() {

