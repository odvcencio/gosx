// GoSX Scene3D GLTF Sub-Feature — loaded on-demand by the main scene3d
// bundle the FIRST time a page calls loadSceneModelAsset() with a .glb
// or .gltf URL.
//
// Pulled out of bootstrap-feature-scene3d.js so pages that use only
// programmatic geometry (points, lines, spheres, etc.) don't have to
// parse ~30KB of GLTF parsing code they'll never execute. Every galaxy,
// particle system, data-viz, or CSS-driven scene is one of these.
//
// Loading strategy: 20-scene-mount.js's ensureGLTFFeatureLoaded() helper
// detects a model-bearing scene, dynamically inserts this script tag,
// waits for the onload handler, and then calls into the API below. Once
// loaded the chunk is cached in window.__gosx_scene3d_gltf_api and stays
// resident for the rest of the session.
//
// All cross-IIFE dependencies are bridged via globals so this IIFE can
// run without importing anything from the main scene3d bundle.

(function() {
  "use strict";
