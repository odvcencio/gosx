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

// ---------------------------------------------------------------------------
// Render-truth telemetry
// ---------------------------------------------------------------------------
//
// Every pre-existing Scene3D counter answers "what did the author ASK for" or
// "what did the planner PUT IN THE BUNDLE". None of them answer "what actually
// reached the framebuffer". That gap has cost real weeks:
//
//   * A Selena customPost "liquid glass" pass was tuned across three sessions
//     while producing zero pixels. data-gosx-scene3d-postfx said "on",
//     data-gosx-scene3d-webgpu-post-effects said "1", and both were TRUE and
//     both were USELESS -- the effect was in the chain, its pipeline never
//     finished compiling, and the switch case fell through to an identity
//     passthrough every frame. Only a forced-red probe found it.
//   * Three mesh planes drew nothing for two weeks while
//     data-gosx-scene3d-webgpu-mesh-objects read "3" (a bundle count that
//     includes CPU-frustum-culled objects). v0.35.9 added mesh-draw-calls /
//     mesh-view-culled to close that specific hole; this module generalises
//     the same SUBMITTED-vs-DREW split to the post chain, to points, and to
//     the reserved auto-uniforms.
//
// The contract of everything below is: it reports an OBSERVED GPU action, or
// the specific reason no action happened. It never reports configuration.
//
// Cost control: sceneRenderTruthEnabled() gates ALL of it. When the
// diagnostics tier is off, renderers must not build chain records, must not
// format strings, and must not touch the DOM -- callers check the flag first.
// The flag is read once per frame at most, from a plain global.

var SCENE_RENDER_TRUTH_MAX_CHAIN = 32;

// Pipeline states, ordered from "cannot draw" to "drew".
//   missing  -- no shader source / no program object was ever requested
//   pending  -- async pipeline compilation still in flight (WebGPU only)
//   failed   -- compile / link / validation rejected the shader
//   ok       -- a usable pipeline object exists
// "ok" plus dispatched=0 is the signature of the liquid-glass bug class:
// a healthy pipeline that no pass ever bound.
var SCENE_RENDER_TRUTH_PIPELINE_MISSING = "missing";
var SCENE_RENDER_TRUTH_PIPELINE_PENDING = "pending";
var SCENE_RENDER_TRUTH_PIPELINE_FAILED = "failed";
var SCENE_RENDER_TRUTH_PIPELINE_OK = "ok";

// sceneRenderTruthEnabled reports whether the diagnostics tier is active.
// Deliberately reuses the flags the WebGPU verbose-telemetry publisher
// already honours so operators have ONE switch to think about, plus a
// dedicated __gosx_scene3d_render_truth flag for probes that want render
// truth without the full per-frame perf trace.
function sceneRenderTruthEnabled() {
  if (typeof window === "undefined" || !window) return false;
  if (window.__gosx_scene3d_render_truth === true) return true;
  if (window.__gosx_scene3d_webgpu_telemetry === true) return true;
  if (window.__gosx_scene3d_cull_telemetry === true) return true;
  var config = window.__gosx_telemetry_config;
  if (config && typeof config === "object") {
    if (config.scene3dDiagnostics === true) return true;
    if (config.scene3dPerfTelemetry === true) return true;
    if (config.scene3dRenderTruth === true) return true;
  }
  return false;
}

// sceneRenderTruthToken strips the separators used by the encoded chain so a
// hostile or merely careless effect name cannot forge extra chain entries.
function sceneRenderTruthToken(value) {
  return String(value == null ? "" : value).replace(/[|:@]+/g, "-").slice(0, 48);
}

// sceneRenderTruthChain builds one record per AUTHORED post effect. Records
// start life as "missing / not dispatched" so an effect whose switch case
// never runs (an unknown kind, a lowercase-mismatched kind -- the exact
// customPost defect) is visible as a dead entry rather than as silence.
function sceneRenderTruthChain(effects) {
  var list = Array.isArray(effects) ? effects : [];
  var out = [];
  var limit = Math.min(list.length, SCENE_RENDER_TRUTH_MAX_CHAIN);
  for (var i = 0; i < limit; i++) {
    var effect = list[i] || {};
    out.push({
      kind: sceneRenderTruthToken(effect.kind || "unknown"),
      name: sceneRenderTruthToken(effect.name || ""),
      pipeline: SCENE_RENDER_TRUTH_PIPELINE_MISSING,
      dispatched: 0,
    });
  }
  return out;
}

// sceneRenderTruthMark records what happened to one chain slot. pipeline is
// one of the SCENE_RENDER_TRUTH_PIPELINE_* constants; dispatched counts the
// fullscreen passes the effect actually issued this frame (bloom issues four,
// so the count is informative, not just a boolean).
function sceneRenderTruthMark(chain, index, pipeline, dispatched) {
  if (!Array.isArray(chain)) return;
  var record = chain[index];
  if (!record) return;
  if (pipeline) record.pipeline = pipeline;
  var count = Math.max(0, Math.floor(Number(dispatched) || 0));
  if (count > 0) record.dispatched += count;
}

// sceneRenderTruthEncodeChain formats the chain for a DOM attribute:
//   "0:bloom:ok:4|1:custompost@liquidGlass:pending:0"
// index:kind[@name]:pipelineState:dispatchCount, pipe-separated.
// One glance answers "which effect is dead and why".
function sceneRenderTruthEncodeChain(chain) {
  if (!Array.isArray(chain) || chain.length === 0) return "";
  var parts = [];
  for (var i = 0; i < chain.length; i++) {
    var record = chain[i];
    if (!record) continue;
    var label = record.name ? record.kind + "@" + record.name : record.kind;
    parts.push(i + ":" + label + ":" + record.pipeline + ":" + record.dispatched);
  }
  return parts.join("|");
}

// sceneRenderTruthChainCounts reduces the chain to the two numbers a gate
// asserts on: how many effects were authored, and how many actually drew.
// dead = authored - dispatched is the alarm.
function sceneRenderTruthChainCounts(chain) {
  var authored = Array.isArray(chain) ? chain.length : 0;
  var dispatched = 0;
  var failed = 0;
  var pending = 0;
  for (var i = 0; i < authored; i++) {
    var record = chain[i];
    if (!record) continue;
    if (record.dispatched > 0) dispatched++;
    if (record.pipeline === SCENE_RENDER_TRUTH_PIPELINE_FAILED) failed++;
    if (record.pipeline === SCENE_RENDER_TRUTH_PIPELINE_PENDING) pending++;
  }
  return {
    authored: authored,
    dispatched: dispatched,
    dead: Math.max(0, authored - dispatched),
    failed: failed,
    pending: pending,
  };
}

// Previous published reserved-time value, per mount, so "is the clock moving"
// is answerable from a SINGLE sample instead of forcing every consumer to
// poll twice. The `time` uniform stuck at 0 read healthy on every existing
// attribute; this makes it a one-glance fact.
var sceneRenderTruthTimeHistory = (typeof WeakMap === "function") ? new WeakMap() : null;

function sceneRenderTruthTimeAdvancing(mount, seconds) {
  var value = Number(seconds);
  if (!isFinite(value)) return 0;
  if (!sceneRenderTruthTimeHistory || !mount) return value > 0 ? 1 : 0;
  var previous = sceneRenderTruthTimeHistory.get(mount);
  sceneRenderTruthTimeHistory.set(mount, value);
  if (typeof previous !== "number") return value > 0 ? 1 : 0;
  return value > previous ? 1 : 0;
}

// ---------------------------------------------------------------------------
// Implementation identity
// ---------------------------------------------------------------------------
//
// "The browser supports WebGPU" hides TWO independent implementations with two
// independent sets of compiler bugs:
//
//   Edge / Chrome  -> Dawn,  which translates WGSL through Tint
//   Firefox        -> wgpu,  which translates WGSL through naga
//
// On Windows both then emit HLSL for D3D12, so identical WGSL passes through
// two different translators before a single pixel exists. Selena validates its
// emitted WGSL with naga -- Firefox's compiler. A shader can therefore pass the
// authoring-time gate and still hit a Tint bug in Edge, which is exactly where
// the white parallelogram and the pink homepage were observed. Recording which
// implementation ran is the difference between "the shader is wrong" and "this
// browser's compiler disagreed", and those two conclusions send people down
// opposite paths.
//
// Detection is heuristic by necessity -- no web API reports "Dawn" or "wgpu".
// The signal used, in order: GPUAdapterInfo.description/vendor strings (Dawn
// populates these far more richly), then the user agent. Reported honestly as
// "dawn" / "wgpu" / "unknown" so a consumer can tell a guess from a fact via
// the accompanying engine + adapter strings.
function sceneRenderTruthBrowserEngine() {
  if (typeof navigator === "undefined" || !navigator) return "";
  var ua = String(navigator.userAgent || "");
  if (/\bFirefox\/|\bGecko\//.test(ua) && ua.indexOf("like Gecko") < 0) return "gecko";
  if (/\bEdg\//.test(ua)) return "blink-edge";
  if (/\bChrome\/|\bChromium\//.test(ua)) return "blink";
  if (/\bSafari\//.test(ua)) return "webkit";
  return "";
}

function sceneRenderTruthImplementation(adapterInfo) {
  var engine = sceneRenderTruthBrowserEngine();
  if (engine === "gecko") return "wgpu";
  if (engine === "blink" || engine === "blink-edge") return "dawn";
  var info = adapterInfo || {};
  var blob = String(info.description || "") + " " + String(info.vendor || "") + " " + String(info.architecture || "");
  if (/\bdawn\b/i.test(blob)) return "dawn";
  if (/\bwgpu\b|\bnaga\b/i.test(blob)) return "wgpu";
  if (engine === "webkit") return "webkit-webgpu";
  return "unknown";
}

function sceneRenderTruthAdapterLabel(adapterInfo) {
  var info = adapterInfo || {};
  var parts = [];
  if (info.vendor) parts.push(String(info.vendor));
  if (info.architecture) parts.push(String(info.architecture));
  if (info.device) parts.push(String(info.device));
  if (info.description) parts.push(String(info.description));
  return parts.join(" ").trim().slice(0, 240);
}

// ---------------------------------------------------------------------------
// Event journal
// ---------------------------------------------------------------------------
//
// A final state cannot describe a device that mounts healthy and dies eight
// seconds later. Firefox does exactly that under GPU-memory pressure: the
// engine acquires a device, the scene mounts, then `device.lost` resolves and
// the page silently continues on a different backend. Anything that samples
// once sees a consistent, wrong story.
//
// The journal is an ordered, bounded, timestamped list of the transitions that
// change what is drawable: backend selection, fallback, device loss,
// uncaptured GPU errors, shader compilation complaints, and latched
// host-page decisions. It answers "what happened, in what order" rather than
// "what is true now".
var SCENE_RENDER_TRUTH_MAX_EVENTS = 64;
var sceneRenderTruthEvents = [];

function sceneRenderTruthNow() {
  if (typeof performance !== "undefined" && performance && typeof performance.now === "function") {
    return Math.round(performance.now());
  }
  return Date.now();
}

// sceneRenderTruthRecord appends one journal entry. Always callable, even when
// the diagnostics tier is off: these are RARE events (backend swaps, device
// loss, shader failures), not per-frame work, and losing the one event that
// explains a production incident to save a few bytes is a bad trade. The ring
// is capped so a pathological loop cannot grow it without bound.
function sceneRenderTruthRecord(kind, detail) {
  var entry = {
    at: sceneRenderTruthNow(),
    kind: sceneRenderTruthToken(kind || "event"),
    detail: String(detail == null ? "" : detail).slice(0, 240),
  };
  sceneRenderTruthEvents.push(entry);
  if (sceneRenderTruthEvents.length > SCENE_RENDER_TRUTH_MAX_EVENTS) {
    sceneRenderTruthEvents.shift();
  }
  return entry;
}

function sceneRenderTruthEventLog() {
  return sceneRenderTruthEvents.slice();
}

function sceneRenderTruthEncodeEvents() {
  var parts = [];
  for (var i = 0; i < sceneRenderTruthEvents.length; i++) {
    var entry = sceneRenderTruthEvents[i];
    parts.push(entry.at + ":" + entry.kind + (entry.detail ? ":" + entry.detail.replace(/[|]+/g, "/") : ""));
  }
  return parts.join("|");
}

// ---------------------------------------------------------------------------
// Latched host-page decisions
// ---------------------------------------------------------------------------
//
// Host pages latch decisions from a backend observation and then never re-arm
// -- m31labs.dev's post-FX WebGL guard sets postFXWebGLGuardSettled=true the
// first time it sees WebGPU and keeps the full four-pass chain running even
// after the device dies and the page falls to WebGL. That is invisible in any
// per-frame counter: every counter still reports a healthy four-pass chain.
//
// A latch registered here records the backend that was current WHEN it
// settled. Comparing that against the live backend turns "guard latched on a
// backend that no longer exists" from an inference into a published fact.
var sceneRenderTruthLatches = {};

function sceneRenderTruthLatch(name, backend, settled) {
  var key = sceneRenderTruthToken(name || "latch");
  var record = {
    settled: settled !== false,
    backend: sceneRenderTruthToken(backend || ""),
    at: sceneRenderTruthNow(),
  };
  sceneRenderTruthLatches[key] = record;
  sceneRenderTruthRecord("latch", key + "=" + (record.settled ? "settled" : "armed") + " on " + (record.backend || "?"));
  return record;
}

// sceneRenderTruthEncodeLatches formats "name:settled:backendAtSettle:stale".
// stale=1 means the live backend differs from the one the latch settled on --
// the bug signature described above.
function sceneRenderTruthEncodeLatches(liveBackend) {
  var live = sceneRenderTruthToken(liveBackend || "");
  var parts = [];
  for (var key in sceneRenderTruthLatches) {
    if (!Object.prototype.hasOwnProperty.call(sceneRenderTruthLatches, key)) continue;
    var record = sceneRenderTruthLatches[key];
    var stale = (record.settled && record.backend && live && record.backend !== live) ? 1 : 0;
    parts.push(key + ":" + (record.settled ? 1 : 0) + ":" + (record.backend || "-") + ":" + stale);
  }
  return parts.join("|");
}

function sceneRenderTruthStaleLatchCount(liveBackend) {
  var live = sceneRenderTruthToken(liveBackend || "");
  var count = 0;
  for (var key in sceneRenderTruthLatches) {
    if (!Object.prototype.hasOwnProperty.call(sceneRenderTruthLatches, key)) continue;
    var record = sceneRenderTruthLatches[key];
    if (record.settled && record.backend && live && record.backend !== live) count++;
  }
  return count;
}

// ---------------------------------------------------------------------------
// Shader compilation diagnostics
// ---------------------------------------------------------------------------
//
// getCompilationInfo() is the browser's own verdict on a shader module. It is
// the only place a Tint-versus-naga disagreement surfaces as TEXT rather than
// as wrong pixels. Recording it means a shader that Selena validated with naga
// and that Tint then rejected shows up as a message in the dump instead of a
// white parallelogram nobody can explain.
//
// Async and fire-and-forget: never blocks pipeline creation. Gated on the
// diagnostics tier because it costs a promise per shader module.
var sceneRenderTruthShaderMessages = 0;
var sceneRenderTruthShaderErrors = 0;

function sceneRenderTruthCaptureShaderInfo(module, label) {
  if (!sceneRenderTruthEnabled()) return;
  if (!module || typeof module.getCompilationInfo !== "function") return;
  var tag = sceneRenderTruthToken(label || "shader");
  try {
    var pending = module.getCompilationInfo();
    if (!pending || typeof pending.then !== "function") return;
    pending.then(function(info) {
      var messages = (info && info.messages) ? info.messages : [];
      for (var i = 0; i < messages.length; i++) {
        var message = messages[i] || {};
        var type = String(message.type || "info");
        if (type === "info") continue;
        sceneRenderTruthShaderMessages++;
        if (type === "error") sceneRenderTruthShaderErrors++;
        sceneRenderTruthRecord(
          "shader-" + type,
          tag + " L" + (message.lineNum || 0) + ": " + String(message.message || "").slice(0, 160)
        );
      }
    }).catch(function() {});
  } catch (_err) {
    // getCompilationInfo is optional in older implementations.
  }
}

function sceneRenderTruthShaderCounts() {
  return { messages: sceneRenderTruthShaderMessages, errors: sceneRenderTruthShaderErrors };
}

// sceneRenderTruthPublish stamps the backend-neutral attribute surface. Both
// renderers write the SAME attribute names, so a probe, a deploy gate or a
// human reading the DOM never has to branch on which backend won.
//
// truth fields (all optional):
//   backend           string  renderer kind that actually ran this frame
//   postChain         array   sceneRenderTruthChain records
//   meshSubmitted     number  mesh objects handed to the draw list
//   meshDrawn         number  pass.draw() calls actually issued for meshes
//   meshViewCulled    number  mesh objects dropped by the CPU frustum cull
//   meshUndrawable    number  mesh objects dropped for degenerate geometry
//   pointsSubmitted   number  point entries in the bundle
//   pointsDrawn       number  point entries that issued a draw
//   pointInstancesSubmitted / pointInstancesDrawn  number
//   uniformTime       number  the reserved `time` auto-uniform, as fed to shaders
function sceneRenderTruthPublish(mount, truth) {
  if (!mount || typeof mount.setAttribute !== "function" || !truth) return;
  var counts = sceneRenderTruthChainCounts(truth.postChain);
  var set = function(name, value) {
    mount.setAttribute("data-gosx-scene3d-render-" + name, String(value));
  };
  set("backend", truth.backend || "");
  set("post-chain", sceneRenderTruthEncodeChain(truth.postChain));
  set("post-authored", counts.authored);
  set("post-dispatched", counts.dispatched);
  set("post-dead", counts.dead);
  set("post-failed", counts.failed);
  set("post-pending", counts.pending);
  set("mesh-submitted", Math.max(0, Math.floor(truth.meshSubmitted || 0)));
  set("mesh-drawn", Math.max(0, Math.floor(truth.meshDrawn || 0)));
  set("mesh-view-culled", Math.max(0, Math.floor(truth.meshViewCulled || 0)));
  set("mesh-undrawable", Math.max(0, Math.floor(truth.meshUndrawable || 0)));
  set("points-submitted", Math.max(0, Math.floor(truth.pointsSubmitted || 0)));
  set("points-drawn", Math.max(0, Math.floor(truth.pointsDrawn || 0)));
  set("point-instances-submitted", Math.max(0, Math.floor(truth.pointInstancesSubmitted || 0)));
  set("point-instances-drawn", Math.max(0, Math.floor(truth.pointInstancesDrawn || 0)));
  var seconds = Number(truth.uniformTime);
  if (!isFinite(seconds)) seconds = 0;
  set("uniform-time", seconds.toFixed(3));
  set("uniform-time-advancing", sceneRenderTruthTimeAdvancing(mount, seconds));
  if (truth.adapterInfo || truth.implementation) {
    set("implementation", truth.implementation || sceneRenderTruthImplementation(truth.adapterInfo));
    set("engine", sceneRenderTruthBrowserEngine());
    set("adapter", sceneRenderTruthAdapterLabel(truth.adapterInfo));
  }
  var shaders = sceneRenderTruthShaderCounts();
  set("shader-messages", shaders.messages);
  set("shader-errors", shaders.errors);
  set("events", sceneRenderTruthEncodeEvents());
  set("latches", sceneRenderTruthEncodeLatches(truth.backend));
  set("stale-latches", sceneRenderTruthStaleLatchCount(truth.backend));
}

if (typeof window !== "undefined") {
  window.__gosx_scene3d_resource_api = Object.assign(window.__gosx_scene3d_resource_api || {}, {
    allocateTextureUnits: sceneAllocateTextureUnits,
    estimateIBLTextureBytes: sceneEstimateIBLTextureBytes,
    estimateShadowTextureBytes: sceneEstimateShadowTextureBytes,
    resolveTextureMemoryBudget: sceneResolveTextureMemoryBudget,
    resolveIBLRenderTargetMode: sceneResolveIBLRenderTargetMode,
  });
  // Bridged on window (not only on __gosx_scene3d_api) because
  // bootstrap-feature-scene3d-webgpu.js is a SEPARATE <script> whose IIFE
  // does not include 15a; it reads these off the global.
  window.__gosx_scene3d_render_truth_api = Object.assign(window.__gosx_scene3d_render_truth_api || {}, {
    enabled: sceneRenderTruthEnabled,
    chain: sceneRenderTruthChain,
    mark: sceneRenderTruthMark,
    encodeChain: sceneRenderTruthEncodeChain,
    chainCounts: sceneRenderTruthChainCounts,
    publish: sceneRenderTruthPublish,
    record: sceneRenderTruthRecord,
    events: sceneRenderTruthEventLog,
    encodeEvents: sceneRenderTruthEncodeEvents,
    latch: sceneRenderTruthLatch,
    encodeLatches: sceneRenderTruthEncodeLatches,
    staleLatches: sceneRenderTruthStaleLatchCount,
    captureShaderInfo: sceneRenderTruthCaptureShaderInfo,
    shaderCounts: sceneRenderTruthShaderCounts,
    implementation: sceneRenderTruthImplementation,
    browserEngine: sceneRenderTruthBrowserEngine,
    adapterLabel: sceneRenderTruthAdapterLabel,
    PIPELINE_MISSING: SCENE_RENDER_TRUTH_PIPELINE_MISSING,
    PIPELINE_PENDING: SCENE_RENDER_TRUTH_PIPELINE_PENDING,
    PIPELINE_FAILED: SCENE_RENDER_TRUTH_PIPELINE_FAILED,
    PIPELINE_OK: SCENE_RENDER_TRUTH_PIPELINE_OK,
  });
}
