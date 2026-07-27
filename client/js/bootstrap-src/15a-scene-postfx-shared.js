// Shared Scene3D post-processing scalars used by the scene core and by both
// renderers. The concatenated bootstrap.js places this module before
// 16-scene-webgl.js and 16a-scene-webgpu.js, so these functions are in scope
// when either renderer runs.
//
// The texture-unit table, the IBL profile maths and the shared texture-memory
// budget left this file for 15a1-scene-texture-budget.js. Only
// 16-scene-webgl.js reads them, and that renderer ships in the lazily fetched
// WebGL chunk, so a WebGPU page used to pay for allocation tables it can never
// reach.

// Resolve the effective scaling factor for the postfx offscreen pipeline.
// Maps the `maxPixels` cap from the IR bundle to a scale factor in (0, 1].
//
//   undefined / null / zero / negative → default 1080p cap (2_073_600)
//   positive → explicit cap
//   canvasPixels <= cap → factor of 1.0 (no scaling)
//   canvasPixels > cap → factor of sqrt(cap / canvasPixels)
//
// See scene.PostFX.MaxPixels in the Go API for the source of this value.
function resolvePostFXFactor(maxPixels, canvasPixels) {
  var cap = (typeof maxPixels === "number" && maxPixels > 0)
    ? maxPixels
    : 2073600; // PostFXMaxPixels1080p default
  if (canvasPixels <= cap) return 1;
  return Math.sqrt(cap / canvasPixels);
}

// Resolve the scaled shadow map size given the light's requested size
// and the scene's shadow pixel cap. Mirrors the resolvePostFXFactor
// pattern but operates on a single shadow map's pixel count rather than
// a canvas area.
//
//   shadowMaxPixels undefined/null/zero/negative → default 1024² cap
//   shadowMaxPixels positive → explicit cap
//   requestedSize² <= cap → return requestedSize unchanged
//   requestedSize² > cap → scale down uniformly
function resolveShadowSize(requestedSize, shadowMaxPixels) {
  var size = Math.max(1, requestedSize | 0);
  var cap = (typeof shadowMaxPixels === "number" && shadowMaxPixels > 0)
    ? shadowMaxPixels
    : 1048576; // ShadowMaxPixels1024 default
  var requestedPixels = size * size;
  if (requestedPixels <= cap) return size;
  var factor = Math.sqrt(cap / requestedPixels);
  return Math.max(1, Math.floor(size * factor));
}

function sceneFiniteNumber(value, fallback) {
  return typeof value === "number" && isFinite(value) ? value : fallback;
}
