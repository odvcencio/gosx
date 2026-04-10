// Shared Scene3D post-processing helpers used by both the WebGL and
// WebGPU renderers. The concatenated bootstrap.js places this module
// before 16-scene-webgl.js and 16a-scene-webgpu.js, so these functions
// are in scope when either renderer runs.

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
