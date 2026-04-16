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

var SCENE_TEXTURE_UNIT_MATERIALS = {
  albedo: 0,
  normal: 1,
  roughness: 2,
  metalness: 3,
  emissive: 4,
};
var SCENE_TEXTURE_UNIT_FIRST_SHARED = 5;
var SCENE_TEXTURE_UNIT_DEFAULT_MAX = 16;
var SCENE_TEXTURE_BUDGET_DEFAULT_BYTES = 26 * 1024 * 1024;

function sceneFiniteNumber(value, fallback) {
  return typeof value === "number" && isFinite(value) ? value : fallback;
}

function sceneClampInteger(value, min, max) {
  var next = Math.floor(sceneFiniteNumber(value, min));
  if (next < min) return min;
  if (next > max) return max;
  return next;
}

// Allocates Scene3D texture units from one shared table:
//   0-4: material maps
//   5..N: shadow maps / CSM cascades
//   N+1..N+3: future IBL irradiance/radiance/BRDF textures
//
// Keeping this centralized prevents shadow cascades and IBL from silently
// binding different samplers to the same texture unit.
function sceneAllocateTextureUnits(options) {
  var opts = options || {};
  var requestedMaxUnits = Object.prototype.hasOwnProperty.call(opts, "maxUnits")
    ? opts.maxUnits
    : SCENE_TEXTURE_UNIT_DEFAULT_MAX;
  var maxUnits = sceneClampInteger(requestedMaxUnits, SCENE_TEXTURE_UNIT_FIRST_SHARED, 64);
  var shadowCount = sceneClampInteger(opts.shadowCount || 0, 0, 32);
  var needsIBL = Boolean(opts.ibl || opts.envMap);
  var iblUnitCount = needsIBL ? 3 : 0;
  var warnings = [];
  var availableShared = Math.max(0, maxUnits - SCENE_TEXTURE_UNIT_FIRST_SHARED);
  var availableForShadows = Math.max(0, availableShared - iblUnitCount);
  if (shadowCount > availableForShadows) {
    warnings.push("shadow texture units reduced from " + shadowCount + " to " + availableForShadows);
    shadowCount = availableForShadows;
  }

  var units = {
    material: {
      albedo: SCENE_TEXTURE_UNIT_MATERIALS.albedo,
      normal: SCENE_TEXTURE_UNIT_MATERIALS.normal,
      roughness: SCENE_TEXTURE_UNIT_MATERIALS.roughness,
      metalness: SCENE_TEXTURE_UNIT_MATERIALS.metalness,
      emissive: SCENE_TEXTURE_UNIT_MATERIALS.emissive,
    },
    shadows: [],
    ibl: null,
    maxUnits: maxUnits,
    warnings: warnings,
  };

  var unit = SCENE_TEXTURE_UNIT_FIRST_SHARED;
  for (var i = 0; i < shadowCount; i++) {
    units.shadows.push(unit++);
  }

  if (needsIBL) {
    if (unit + 2 < maxUnits) {
      units.ibl = {
        irradiance: unit,
        radiance: unit + 1,
        brdfLUT: unit + 2,
      };
    } else {
      warnings.push("IBL disabled because texture units are exhausted");
    }
  }

  return units;
}

function sceneTextureMipBytes(baseSize, mipLevels, faceCount, bytesPerPixel) {
  var total = 0;
  var size = Math.max(1, Math.floor(baseSize || 1));
  var levels = Math.max(1, Math.floor(mipLevels || 1));
  var faces = Math.max(1, Math.floor(faceCount || 1));
  var bpp = Math.max(1, Math.floor(bytesPerPixel || 1));
  for (var level = 0; level < levels; level++) {
    var mipSize = Math.max(1, Math.floor(size / Math.pow(2, level)));
    total += mipSize * mipSize * faces * bpp;
  }
  return total;
}

function sceneNormalizeIBLProfile(profile) {
  var source = profile || {};
  return {
    sourceFaceSize: sceneClampInteger(source.sourceFaceSize || 512, 16, 4096),
    irradianceFaceSize: sceneClampInteger(source.irradianceFaceSize || 32, 8, 512),
    radianceFaceSize: sceneClampInteger(source.radianceFaceSize || 128, 16, 1024),
    radianceMipLevels: sceneClampInteger(source.radianceMipLevels || 5, 1, 12),
    brdfSize: sceneClampInteger(source.brdfSize || 512, 16, 2048),
    bytesPerPixel: sceneClampInteger(source.bytesPerPixel || 8, 1, 16),
    brdfBytesPerPixel: sceneClampInteger(source.brdfBytesPerPixel || 4, 1, 16),
  };
}

function sceneEstimateIBLTextureBytes(profile) {
  var p = sceneNormalizeIBLProfile(profile);
  return sceneTextureMipBytes(p.sourceFaceSize, 1, 6, p.bytesPerPixel)
    + sceneTextureMipBytes(p.irradianceFaceSize, 1, 6, p.bytesPerPixel)
    + sceneTextureMipBytes(p.radianceFaceSize, p.radianceMipLevels, 6, p.bytesPerPixel)
    + sceneTextureMipBytes(p.brdfSize, 1, 1, p.brdfBytesPerPixel);
}

function sceneEstimateShadowTextureBytes(shadowSize, shadowCount) {
  var size = Math.max(1, Math.floor(shadowSize || 1));
  var count = Math.max(0, Math.floor(shadowCount || 0));
  return size * size * 4 * count;
}

function sceneDownscaleIBLProfile(profile) {
  var p = sceneNormalizeIBLProfile(profile);
  var next = {
    sourceFaceSize: Math.max(128, Math.floor(p.sourceFaceSize / 2)),
    irradianceFaceSize: Math.max(16, Math.floor(p.irradianceFaceSize / 2)),
    radianceFaceSize: Math.max(64, Math.floor(p.radianceFaceSize / 2)),
    radianceMipLevels: p.radianceMipLevels,
    brdfSize: Math.max(256, Math.floor(p.brdfSize / 2)),
    bytesPerPixel: p.bytesPerPixel,
    brdfBytesPerPixel: p.brdfBytesPerPixel,
  };
  var changed = next.sourceFaceSize !== p.sourceFaceSize
    || next.irradianceFaceSize !== p.irradianceFaceSize
    || next.radianceFaceSize !== p.radianceFaceSize
    || next.brdfSize !== p.brdfSize;
  return changed ? next : p;
}

// Applies the shared Scene3D texture-memory budget across shadows and IBL.
// The desktop IBL profile is reduced before shadow maps because existing
// cast/receive-shadow scenes should keep their declared behavior.
function sceneResolveTextureMemoryBudget(options) {
  var opts = options || {};
  var maxBytes = Math.max(1, Math.floor(sceneFiniteNumber(opts.maxBytes, SCENE_TEXTURE_BUDGET_DEFAULT_BYTES)));
  var shadowCount = sceneClampInteger(opts.shadowCount || 0, 0, 32);
  var shadowSize = Math.max(1, Math.floor(sceneFiniteNumber(opts.shadowSize, 1024)));
  var iblEnabled = Boolean(opts.ibl || opts.envMap);
  var iblProfile = sceneNormalizeIBLProfile(opts.iblProfile);
  var warnings = [];
  var iblBytes = iblEnabled ? sceneEstimateIBLTextureBytes(iblProfile) : 0;
  var shadowBytes = sceneEstimateShadowTextureBytes(shadowSize, shadowCount);

  while (iblEnabled && shadowBytes + iblBytes > maxBytes) {
    var nextProfile = sceneDownscaleIBLProfile(iblProfile);
    var nextBytes = sceneEstimateIBLTextureBytes(nextProfile);
    if (nextBytes >= iblBytes) break;
    iblProfile = nextProfile;
    iblBytes = nextBytes;
    warnings.push("IBL profile downscaled for shared texture budget");
  }

  if (shadowBytes + iblBytes > maxBytes && shadowCount > 0) {
    var remaining = Math.max(1, maxBytes - iblBytes);
    var cappedShadowSize = Math.max(1, Math.floor(Math.sqrt(remaining / Math.max(1, shadowCount * 4))));
    if (cappedShadowSize < shadowSize) {
      shadowSize = cappedShadowSize;
      shadowBytes = sceneEstimateShadowTextureBytes(shadowSize, shadowCount);
      warnings.push("shadow map size downscaled for shared texture budget");
    }
  }

  if (iblEnabled && shadowBytes + iblBytes > maxBytes) {
    iblEnabled = false;
    iblBytes = 0;
    warnings.push("IBL disabled for shared texture budget");
  }

  return {
    maxBytes: maxBytes,
    shadowCount: shadowCount,
    shadowSize: shadowSize,
    shadowBytes: shadowBytes,
    ibl: iblEnabled,
    iblProfile: iblEnabled ? iblProfile : null,
    iblBytes: iblBytes,
    totalBytes: shadowBytes + iblBytes,
    warnings: warnings,
  };
}

// Chooses the IBL precompute render-target path. WebGL2 half-float color
// attachments are preferred; when unavailable, mobile GPUs get a graceful
// LDR/no-IBL fallback instead of a failed scene mount.
function sceneResolveIBLRenderTargetMode(gl, options) {
  var opts = options || {};
  var ext = null;
  if (gl && typeof gl.getExtension === "function") {
    ext = gl.getExtension("EXT_color_buffer_half_float") || gl.getExtension("EXT_color_buffer_float");
  }
  if (ext) {
    return {
      mode: "half-float",
      extension: ext,
      profile: sceneNormalizeIBLProfile(opts.lowPower ? {
        sourceFaceSize: 256,
        irradianceFaceSize: 16,
        radianceFaceSize: 64,
        brdfSize: 256,
      } : opts.profile),
      reason: "",
    };
  }
  if (opts.allowLDRFallback !== false) {
    return {
      mode: "ldr-fallback",
      extension: null,
      profile: sceneNormalizeIBLProfile({
        sourceFaceSize: opts.lowPower ? 128 : 256,
        irradianceFaceSize: 16,
        radianceFaceSize: 64,
        brdfSize: 256,
        bytesPerPixel: 4,
        brdfBytesPerPixel: 4,
      }),
      reason: "half-float-render-target-unavailable",
    };
  }
  return {
    mode: "disabled",
    extension: null,
    profile: null,
    reason: "half-float-render-target-unavailable",
  };
}

if (typeof window !== "undefined") {
  window.__gosx_scene3d_resource_api = Object.assign(window.__gosx_scene3d_resource_api || {}, {
    allocateTextureUnits: sceneAllocateTextureUnits,
    estimateIBLTextureBytes: sceneEstimateIBLTextureBytes,
    estimateShadowTextureBytes: sceneEstimateShadowTextureBytes,
    resolveTextureMemoryBudget: sceneResolveTextureMemoryBudget,
    resolveIBLRenderTargetMode: sceneResolveIBLRenderTargetMode,
  });
}
