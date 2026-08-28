"use strict";
// Scene3D authored specular render slice: the KHR specularIntensity /
// specularColor factors must actually shade, not just normalize. This file
// drives the production WebGL uploadMaterial and the production WebGPU
// materialUniformData through recording GPU boundaries, and pins the
// production shader strings that consume the uploaded factors.
//
// Effective dielectric F0 = min(IOR F0 * linear color, 1) * intensity,
// F90 = intensity, clamp BEFORE intensity. The diffuse weight is the scalar
// (1 - maxRGB(dielectric Fresnel)) * (1 - metalness), never an inverse RGB
// tint, and the fully-metal branch must be independent of the dielectric
// settings.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const srcDir = path.join(__dirname, "bootstrap-src");
const SHARED_API_EXPORT_MARKER = "// Scene3D shared API";

function readBootstrapSource(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

function readRuntimeSource(name) {
  return fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", name), "utf8");
}

function trimBeforeSharedApiExport(source) {
  const at = source.indexOf(SHARED_API_EXPORT_MARKER);
  assert.ok(at >= 0, "core '// Scene3D shared API' export marker located");
  return source.slice(0, at);
}

function runFragment(context, source, filename) {
  vm.runInContext(source, context, { filename });
}

function sliceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  assert.ok(start >= 0, "start marker located: " + startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(end > start, "end marker located after start: " + endMarker);
  return source.slice(start, end);
}

const SCENE_FRAGMENT_FILES = [
  "10-runtime-primitives.ts",
  "10-runtime-scene-utils.ts",
  "11-scene-math.ts",
  "12-scene-geometry.ts",
  "13-scene-material.ts",
];

function createSceneCoreContext() {
  const context = vm.createContext({ console, window: {} });
  for (const name of SCENE_FRAGMENT_FILES) {
    runFragment(context, readBootstrapSource(name), name);
  }
  runFragment(context,
    trimBeforeSharedApiExport(readBootstrapSource("10-runtime-scene-core.ts")),
    "10-runtime-scene-core.ts");
  return context;
}

function callIn(context, expression) {
  return vm.runInContext(expression, context, { filename: "scene-specular-expression.js" });
}

const close6 = (actual, expected) => Math.abs(actual - expected) <= 1e-6;

// Spec formula used only to compute expectations: F0 = ((ior-1)/(ior+1))^2,
// with ior 0 pinning the dielectric Fresnel to 1. The functions under test
// are the production implementations.
function expectedDielectricF0(ior) {
  return ior === 0 ? 1 : Math.fround(((ior - 1) / (ior + 1)) ** 2);
}

// --- WebGL -------------------------------------------------------------------

function setupWebGLRenderer() {
  const source = readRuntimeSource("webgl.ts");
  const context = createSceneCoreContext();
  runFragment(context, [
    "function sceneFiniteNumber(value, fallback) { const n = Number(value); return Number.isFinite(n) ? n : fallback; }",
    sliceBetween(readBootstrapSource("15a1-scene-texture-budget.ts"),
      "var SCENE_TEXTURE_UNIT_MATERIALS", "function sceneTextureMipBytes"),
    sliceBetween(source, "function scenePBRSRGBChannelToLinear", "const SCENE_PBR_VERTEX_SOURCE"),
    sliceBetween(source, "function scenePBRDielectricF0", "function scenePBRCacheBaseUniforms"),
    sliceBetween(source, "function scenePBRHDRIBLAvailable", "function scenePBRFragmentSourceForContext"),
    sliceBetween(source, "function scenePBRMaxTextureUnits", "function scenePBRSlotCascadeCount"),
    sliceBetween(source, "function scenePBRSlotCascadeCount", "function scenePBRTextureLayoutForFrame"),
    sliceBetween(source, "function scenePBRTextureLayoutForFrame", "// Upload cascaded-shadow uniforms"),
    sliceBetween(source, "function uploadCustomUniforms", "function uploadMaterial"),
    sliceBetween(source, "function uploadMaterial", "function applyBlendMode"),
    [
      "function recordingGL() {",
      "  const floats = new Map();",
      "  const ints = new Map();",
      "  const binds = new Map();",
      "  let activeUnit = -1;",
      "  return {",
      "    floats, ints, binds,",
      "    TEXTURE0: 0, TEXTURE_2D: 1, TEXTURE_CUBE_MAP: 2,",
      "    uniform1f(loc, v) { floats.set(loc && loc.name, v); },",
      "    uniform2f(loc, a, b) { floats.set(loc && loc.name, [a, b]); },",
      "    uniform3f(loc, a, b, c) { floats.set(loc && loc.name, [a, b, c]); },",
      "    uniform4f(loc, a, b, c, d) { floats.set(loc && loc.name, [a, b, c, d]); },",
      "    uniform1i(loc, v) { ints.set(loc && loc.name, v); },",
      "    activeTexture(unit) { activeUnit = unit; },",
      "    bindTexture(target, texture) { binds.set(activeUnit, { target, texture }); },",
      "  };",
      "}",
      "function uniformSlots() {",
      "  const slots = { customUniforms: null };",
      "  const names = ['albedo', 'roughness', 'metalness', 'clearcoat', 'sheen',",
      "    'transmission', 'iridescence', 'anisotropy', 'specularF0', 'specularF90',",
      "    'specularColorLog',",
    "    'emissive', 'opacity', 'unlit', 'alphaCutoff',",
      "    'albedoMap', 'normalMap', 'roughnessMap', 'metalnessMap', 'occlusionMap',",
      "    'emissiveMap', 'specularIntensityMap',",
      "    'specularColorMap',",
      "    'hasAlbedoMap', 'hasNormalMap', 'hasRoughnessMap', 'hasMetalnessMap',",
      "    'hasOcclusionMap', 'hasEmissiveMap', 'hasSpecularIntensityMap',",
      "    'hasSpecularColorMap'];",
      "  for (const name of names) slots[name] = { name };",
      "  return slots;",
      "}",
      "const __textureStates = new Map();",
      "const __textureLoads = [];",
      "const __textureRecords = new Map();",
      "function setTextureState(url, state) { __textureStates.set(url, state); }",
      "function textureLoads() { return __textureLoads; }",
      "function textureRecords() { return __textureRecords; }",
      "function scenePBRLoadTexture(gl, url, cache, descriptor, role, colorSpace) {",
      "  let entry = __textureRecords.get(url);",
      "  if (!entry) {",
      "    entry = { url: url, texture: { gosxTestName: url } };",
      "    __textureRecords.set(url, entry);",
      "  }",
      "  const load = { url: url, role: role, colorSpace: colorSpace,",
      "    descriptor: descriptor === undefined ? null : descriptor, texture: entry.texture };",
      "  __textureLoads.push(load);",
      "  const state = __textureStates.get(url) || 'missing';",
      "  if (state === 'missing') return null;",
      "  const record = { texture: entry.texture, target: gl.TEXTURE_2D,",
      "    loaded: state === 'loaded', failed: state === 'failed' };",
      "  entry.record = record;",
      "  return record;",
      "}",
      sliceBetween(source, "function scenePBRBindTexture", "function scenePBRDielectricF0"),
      "function glWithUnits(count) {",
      "  return { MAX_TEXTURE_IMAGE_UNITS: 34930,",
      "    getParameter: function(p) { if (p !== 34930) throw new Error('unexpected query'); return count; } };",
      "}",
    ].join("\n"),
  ].join("\n"), "webgl-specular-extract.js");
  return { source, context };
}

function webglUpload(context, literal) {
  return callIn(context,
    "(() => { const gl = recordingGL(); const uniforms = uniformSlots();" +
    "uploadMaterial(gl, uniforms, " + literal + ", null);" +
    "return { f0: gl.floats.get('specularF0'), f90: gl.floats.get('specularF90') }; })()");
}

test("WebGL uploadMaterial uploads the effective specular factors", () => {
  const { context } = setupWebGLRenderer();
  const upload = (literal) => webglUpload(context, literal);
  const f0Is = (result, expected) => {
    assert.ok(Array.isArray(result.f0) && result.f0.length === 3, "specularF0 uploaded as a vec3");
    for (let c = 0; c < 3; c++) {
      assert.ok(close6(result.f0[c], expected[c]),
        "f0[" + c + "] = " + result.f0[c] + ", want " + expected[c]);
    }
  };

  // Valid defaults: omitted intensity = 1, omitted color = white, so the
  // effective F0 is exactly the legacy IOR F0 and F90 is 1.
  const def = upload("{}");
  f0Is(def, [0.04, 0.04, 0.04]);
  assert.strictEqual(def.f90, 1);

  // An explicit 0 intensity is valid and zeroes the whole lobe.
  const zero = upload("{ specularIntensity: 0 }");
  f0Is(zero, [0, 0, 0]);
  assert.strictEqual(zero.f90, 0);

  // A black tint zeroes F0 while the intensity stays 1: the lobe is not
  // gone, F90 still produces a grazing response. Only intensity 0 removes
  // the lobe entirely.
  const black = upload("{ specularColor: [0, 0, 0] }");
  f0Is(black, [0, 0, 0]);
  assert.strictEqual(black.f90, 1);

  // A tint scales the clamped IOR F0 per channel.
  f0Is(upload("{ specularColor: [4, 1, 1] }"), [0.16, 0.04, 0.04]);

  // Clamp BEFORE intensity: min(0.04 * 100, 1) = 1, then * 0.5 = 0.5. The
  // wrong order (intensity inside the min) would upload 1 in red.
  const clampOrder = upload("{ specularColor: [100, 1, 1], specularIntensity: 0.5 }");
  f0Is(clampOrder, [0.5, 0.02, 0.02]);
  assert.strictEqual(clampOrder.f90, 0.5);

  // IOR interaction: ior 0 maps the base F0 to 1, so the tint passes through
  // the clamp unattenuated; ior 1 maps the base F0 to 0; a finite ior scales.
  f0Is(upload("{ ior: 0, specularColor: [0.5, 0.5, 0.5] }"), [0.5, 0.5, 0.5]);
  f0Is(upload("{ ior: 1, specularColor: [3, 3, 3] }"), [0, 0, 0]);
  const ior2 = Math.pow((2 - 1) / (2 + 1), 2);
  f0Is(upload("{ ior: 2, specularColor: [3, 3, 3] }"),
    [Math.min(ior2 * 3, 1), Math.min(ior2 * 3, 1), Math.min(ior2 * 3, 1)]);

  // Invalid inputs fall back: intensity outside [0, 1] or non-finite means 1;
  // a color that is not exactly three finite non-negative components means
  // white. Nothing uploads NaN or Infinity.
  const nan = upload("{ specularIntensity: NaN }");
  f0Is(nan, [0.04, 0.04, 0.04]);
  assert.strictEqual(nan.f90, 1);
  const over = upload("{ specularIntensity: 2 }");
  assert.strictEqual(over.f90, 1);
  const negative = upload("{ specularIntensity: -0.5 }");
  assert.strictEqual(negative.f90, 1);
  const inf = upload("{ specularIntensity: Infinity }");
  assert.strictEqual(inf.f90, 1);
  for (const badColor of ["[1, 2]", "[-1, 0, 0]", "[Infinity, 1, 1]", '[1, 2, NaN]', '"junk"', "[0, 0, 0, 0]"]) {
    const bad = upload("{ specularColor: " + badColor + " }");
    f0Is(bad, [0.04, 0.04, 0.04]);
    assert.ok(Number.isFinite(bad.f90));
  }

  // Bounded finite uploads at the extremes: a float64-huge tint clamps to 1
  // before the intensity, and an IOR of 1 + Number.EPSILON yields a tiny but
  // finite base F0.
  const huge = upload("{ specularColor: [Number.MAX_VALUE, 1, 1] }");
  f0Is(huge, [1, 0.04, 0.04]);
  assert.ok(Number.isFinite(huge.f90));
  const epsIor = upload("{ ior: 1 + Number.EPSILON }");
  const epsF0 = Math.pow(Number.EPSILON / (2 + Number.EPSILON), 2);
  f0Is(epsIor, [epsF0, epsF0, epsF0]);
  assert.strictEqual(epsIor.f90, 1);

  // Number.MAX_VALUE IOR: (MAX-1)/(MAX+1) collapses to exactly 1 in float64
  // (MAX-1 === MAX and MAX+1 === MAX), so the base F0 is 1 in every channel.
  const maxIor = upload("{ ior: Number.MAX_VALUE }");
  f0Is(maxIor, [1, 1, 1]);
  assert.strictEqual(maxIor.f90, 1);

  // Combined extreme: a 1+eps base F0 with a float64-huge tint. The clamp
  // runs in float64 BEFORE any f32 packing, so red clamps to exactly 1 and
  // the untouched channels stay at the tiny finite base F0 instead of
  // underflowing.
  const combined = upload("{ ior: 1 + Number.EPSILON, specularColor: [Number.MAX_VALUE, 1, 1] }");
  f0Is(combined, [1, epsF0, epsF0]);
  assert.strictEqual(combined.f90, 1);

  // Neighbouring scalars still ride the same upload call.
  const untouched = upload("{ ior: 2.42, specularColor: [1, 1, 1] }");
  assert.strictEqual(untouched.f90, 1);
});

test("WebGL specular-intensity map is neutral when missing, pending or failed", () => {
  const { context } = setupWebGLRenderer();
  const upload = (literal) => callIn(context,
    "(() => { const gl = recordingGL(); const uniforms = uniformSlots();" +
    "uploadMaterial(gl, uniforms, " + literal + ", null);" +
    "return { gl, uniforms }; })()");

  // Missing: no flag, no sampler unit, no bind, fast path stays ready.
  let result = upload("{}");
  assert.equal(result.gl.ints.get("hasSpecularIntensityMap"), 0);
  assert.equal(result.gl.ints.get("specularIntensityMap"), undefined);
  assert.equal(result.gl.binds.size, 0);
  assert.equal(result.uniforms._lastMaterialTexturesReady, true);

  // Pending: neutral now, but the material fast-path must be blocked so a
  // later frame can still observe the settled texture.
  callIn(context, "setTextureState('pending.png', 'pending')");
  result = upload("{ specularIntensityMap: 'pending.png' }");
  assert.equal(result.gl.ints.get("hasSpecularIntensityMap"), 0);
  assert.equal(result.gl.binds.size, 0);
  assert.equal(result.uniforms._lastMaterialTexturesReady, false);
  const pendingRecord = callIn(context, "textureRecords().get('pending.png').record");
  assert.equal(pendingRecord.loaded, false);
  assert.equal(pendingRecord.failed, false);
  assert.strictEqual(pendingRecord.texture,
    callIn(context, "textureRecords().get('pending.png').texture"));

  // Failed: neutral, and it must not block the ready cache.
  callIn(context, "setTextureState('failed.png', 'failed')");
  result = upload("{ specularIntensityMap: 'failed.png' }");
  assert.equal(result.gl.ints.get("hasSpecularIntensityMap"), 0);
  assert.equal(result.gl.binds.size, 0);
  assert.equal(result.uniforms._lastMaterialTexturesReady, true);
  const failedRecord = callIn(context, "textureRecords().get('failed.png').record");
  assert.equal(failedRecord.loaded, false);
  assert.equal(failedRecord.failed, true);
});

test("WebGL specular-intensity map binds its reserved material unit with the exact texture", () => {
  const { context } = setupWebGLRenderer();
  const specUnit = callIn(context, "SCENE_TEXTURE_UNIT_MATERIALS.specularIntensity");
  assert.equal(specUnit, 6);
  callIn(context, "setTextureState('spec.png', 'loaded')");
  const result = callIn(context,
    "(() => { const gl = recordingGL(); const uniforms = uniformSlots();" +
    "uploadMaterial(gl, uniforms, { specularIntensityMap: 'spec.png' }, null);" +
    "return gl; })()");
  assert.equal(result.ints.get("hasSpecularIntensityMap"), 1);
  assert.equal(result.ints.get("specularIntensityMap"), specUnit);
  const bind = result.binds.get(specUnit);
  assert.ok(bind, "specular intensity texture bound on its own unit");
  assert.equal(bind.target, result.TEXTURE_2D);
  assert.equal(bind.texture.gosxTestName, "spec.png");
  assert.strictEqual(bind.texture,
    callIn(context, "textureRecords().get('spec.png').texture"));
  // Loaded through the production role-aware path as linear data.
  const load = callIn(context, "textureLoads()[textureLoads().length - 1]");
  assert.equal(load.url, "spec.png");
  assert.equal(load.role, "specular-intensity");
  assert.equal(load.colorSpace, "linear");
});

test("WebGL specular-intensity descriptor wins over the legacy map path", () => {
  const { context } = setupWebGLRenderer();
  callIn(context, "var specDescriptor = { uri: 'descriptor.png' };");
  callIn(context, "setTextureState('descriptor.png', 'loaded')");
  let gl = callIn(context,
    "(() => { const g = recordingGL(); uploadMaterial(g, uniformSlots()," +
    " { specularIntensityMap: 'legacy.png', textureDescriptors: { specularIntensity: specDescriptor } }, null);" +
    "return g; })()");
  const descriptorLoad = callIn(context, "textureLoads()[textureLoads().length - 1]");
  assert.equal(descriptorLoad.url, "descriptor.png");
  assert.strictEqual(descriptorLoad.descriptor, callIn(context, "specDescriptor"));
  assert.equal(descriptorLoad.role, "specular-intensity");
  assert.equal(descriptorLoad.colorSpace, "linear");
  const descriptorTexture = callIn(context, "textureRecords().get('descriptor.png').texture");
  assert.strictEqual(gl.binds.get(6).texture, descriptorTexture);

  callIn(context, "setTextureState('legacy.png', 'loaded')");
  gl = callIn(context,
    "(() => { const g = recordingGL(); uploadMaterial(g, uniformSlots()," +
    " { specularIntensityMap: 'legacy.png' }, null); return g; })()");
  const legacyLoad = callIn(context, "textureLoads()[textureLoads().length - 1]");
  assert.equal(legacyLoad.role, "specular-intensity");
  assert.equal(legacyLoad.colorSpace, "linear");
  const legacyTexture = callIn(context, "textureRecords().get('legacy.png').texture");
  assert.strictEqual(gl.binds.get(6).texture, legacyTexture);
  assert.equal(gl.ints.get("specularIntensityMap"), 6);
});

test("WebGL pending specular-intensity map blocks the fast path and a late load takes effect", () => {
  const { context } = setupWebGLRenderer();
  callIn(context,
    "var persistMaterial = { specularIntensityMap: 'late.png' }; var persistUniforms = uniformSlots();");
  const uploadPersisted = () => callIn(context,
    "(() => { const gl = recordingGL(); uploadMaterial(gl, persistUniforms, persistMaterial, null);" +
    "return gl; })()");

  callIn(context, "setTextureState('late.png', 'pending')");
  let gl = uploadPersisted();
  assert.equal(callIn(context, "persistUniforms._lastMaterialTexturesReady"), false);
  assert.equal(gl.ints.get("hasSpecularIntensityMap"), 0);
  assert.equal(gl.binds.size, 0);

  // Repeating the same material still re-issues the readiness work while
  // the texture is pending.
  uploadPersisted();
  assert.equal(callIn(context, "persistUniforms._lastMaterialTexturesReady"), false);

  callIn(context, "setTextureState('late.png', 'loaded')");
  gl = uploadPersisted();
  assert.equal(callIn(context, "persistUniforms._lastMaterialTexturesReady"), true);
  assert.equal(gl.ints.get("hasSpecularIntensityMap"), 1);
  assert.equal(gl.ints.get("specularIntensityMap"), 6);
  assert.strictEqual(gl.binds.get(6).texture,
    callIn(context, "textureRecords().get('late.png').texture"));

  // Same-material ready caching: the next upload of the identical reference
  // short-circuits and issues no uniform or bind work.
  gl = uploadPersisted();
  assert.equal(callIn(context, "persistUniforms._lastMaterialTexturesReady"), true);
  assert.equal(gl.ints.size, 0);
  assert.equal(gl.binds.size, 0);
});

test("WebGL uploadMaterial normalizes alphaCutoff through the shared normalizer", () => {
  const { context } = setupWebGLRenderer();
  const cutoffOf = (literal) => callIn(context,
    "(() => { const gl = recordingGL(); uploadMaterial(gl, uniformSlots(), { alphaCutoff: " + literal + " }, null);" +
    "return gl.floats.get('alphaCutoff'); })()");
  // Disabled: missing, null, false, negative, non-finite, non-numeric text.
  assert.equal(cutoffOf("undefined"), -1);
  assert.equal(cutoffOf("null"), -1);
  assert.equal(cutoffOf("false"), -1);
  assert.equal(cutoffOf("-0.5"), -1);
  assert.equal(cutoffOf("NaN"), -1);
  assert.equal(cutoffOf("Infinity"), -1);
  assert.equal(cutoffOf("'var(--cut)'"), -1);
  assert.equal(cutoffOf("'nope'"), -1);
  // Enabled: numeric strings and numbers pack exactly via fround.
  assert.ok(close6(cutoffOf("'.5'"), 0.5));
  assert.ok(close6(cutoffOf("0.5"), 0.5));
  assert.equal(cutoffOf("0"), 0);
  assert.equal(cutoffOf("1"), 1);
  // Above 1 packs the float32-exact sentinel 2.
  assert.equal(cutoffOf("1.5"), 2);
  assert.equal(cutoffOf("Number.MAX_VALUE"), 2);
});

test("WebGL alphaCutoff resets to disabled on the same uniforms after a masked material", () => {
  const { context } = setupWebGLRenderer();
  callIn(context, "var seqUniforms = uniformSlots();");
  const cutoffOf = (literal) => callIn(context,
    "(() => { const gl = recordingGL(); uploadMaterial(gl, seqUniforms, " + literal + ", null);" +
    "return gl.floats.get('alphaCutoff'); })()");
  assert.ok(close6(cutoffOf("{ alphaCutoff: 0.5 }"), 0.5));
  // Different material identities on the same uniform slots: the disabled
  // sentinel must come back, not the previous material's cutoff.
  assert.equal(cutoffOf("{}"), -1);
  assert.equal(cutoffOf("{ alphaCutoff: null }"), -1);
  assert.ok(close6(cutoffOf("{ alphaCutoff: '0.25' }"), 0.25));
});

test("WebGL pending albedo texture keeps alphaCutoff and updates hasAlbedoMap on load", () => {
  const { context } = setupWebGLRenderer();
  callIn(context,
    "var maskMaterial = { alphaCutoff: 0.25, texture: 'late-albedo.png' };" +
    "var maskUniforms = uniformSlots();");
  const uploadMasked = () => callIn(context,
    "(() => { const gl = recordingGL(); uploadMaterial(gl, maskUniforms, maskMaterial, null); return gl; })()");

  callIn(context, "setTextureState('late-albedo.png', 'pending')");
  let gl = uploadMasked();
  assert.ok(close6(gl.floats.get("alphaCutoff"), 0.25));
  assert.equal(gl.ints.get("hasAlbedoMap"), 0);

  callIn(context, "setTextureState('late-albedo.png', 'loaded')");
  gl = uploadMasked();
  // Same material identity, so the cutoff must survive the texture load and
  // the albedo map must flip on without losing the masked state.
  assert.ok(close6(gl.floats.get("alphaCutoff"), 0.25));
  assert.equal(gl.ints.get("hasAlbedoMap"), 1);
  assert.equal(gl.ints.get("albedoMap"), 0);
  assert.strictEqual(gl.binds.get(0).texture,
    callIn(context, "textureRecords().get('late-albedo.png').texture"));
});

test("WebGL alpha-mask shader wiring: coverage discard precedes unlit, alpha forced only at output", () => {
  const source = readRuntimeSource("webgl.ts");
  assert.match(source, /alphaCutoff: gl\.getUniformLocation\(program, "u_alphaCutoff"\),/);
  assert.match(source, /gl\.uniform1f\(uniforms\.alphaCutoff, cutoffValue\);/);
  const coverageAt = source.indexOf('"    if (masked && coverage < u_alphaCutoff) {",');
  const discardAt = source.indexOf('"        discard;",');
  const unlitAt = source.indexOf('"    if (u_unlit) {",');
  assert.ok(coverageAt >= 0 && discardAt > coverageAt, "coverage discard block present");
  assert.ok(unlitAt > discardAt, "coverage discard runs before the unlit branch");
  // Both branches force masked output alpha to 1, and neither clobbers the
  // authored opacity handed to gosxApplyCustomFragment.
  const forcedAlpha = "masked ? 1.0 : opacity * v_instanceColor.a";
  assert.equal(source.split(forcedAlpha).length - 1, 2, "both shader branches force masked alpha at output");
  assert.doesNotMatch(source, /"        if \(masked\) \{",\s*\n\s*"            opacity = 1\.0;",/);
  assert.doesNotMatch(source, /"    if \(masked\) \{",\s*\n\s*"        opacity = 1\.0;",/);
  assert.match(source,
    /"    float opacity = u_opacity;",\s*\n\s*"    gosxApplyCustomFragment\(color, opacity, N, v_worldPosition, v_uv\);",/);
});

test("sceneNormalizeMaterialAlphaCutoff bridges from the base chunk into the WebGL chunk", () => {
  // Chunk 1: the base scene3d bundle. 13-scene-material.ts defines the
  // production normalizer; the real 16d fragment must publish it.
  const baseContext = createSceneCoreContext();
  runFragment(baseContext, "window.__gosx_scene3d_api = {};", "init-api.js");
  runFragment(baseContext, readBootstrapSource("16d-scene-webgl-bridge.ts"), "16d-scene-webgl-bridge.ts");
  const bridgeApi = callIn(baseContext, "window.__gosx_scene3d_api");
  assert.strictEqual(bridgeApi.sceneNormalizeMaterialAlphaCutoff,
    callIn(baseContext, "sceneNormalizeMaterialAlphaCutoff"));
  assert.equal(callIn(baseContext,
    "window.__gosx_scene3d_api.sceneNormalizeMaterialAlphaCutoff(0, null)"), 0);
  assert.ok(close6(callIn(baseContext,
    "window.__gosx_scene3d_api.sceneNormalizeMaterialAlphaCutoff('.5', null)"), 0.5));

  // Chunk 2: a different VM holding only the bridged API. The real lazy
  // prefix is an open IIFE; close it here and probe the lexical alias.
  const webglContext = vm.createContext({
    console,
    window: { __gosx_scene3d_api: bridgeApi },
  });
  runFragment(webglContext, [
    readBootstrapSource("26j-feature-scene3d-webgl-prefix.ts"),
    "globalThis.__alphaCutoffProbe = {",
    "  fn: sceneNormalizeMaterialAlphaCutoff,",
    "  zero: sceneNormalizeMaterialAlphaCutoff(0, null),",
    "  half: sceneNormalizeMaterialAlphaCutoff('.5', null),",
    "  missing: sceneNormalizeMaterialAlphaCutoff(null, null),",
    "};",
    "})();",
  ].join("\n"), "26j-scene-webgl-prefix-probe.js");
  const probe = vm.runInContext("globalThis.__alphaCutoffProbe", webglContext);
  assert.equal(typeof probe.fn, "function", "lazy prefix resolved the bridged normalizer");
  assert.strictEqual(probe.zero, 0);
  assert.ok(close6(probe.half, 0.5));
  assert.strictEqual(probe.missing, null);
});

test("WebGL texture-unit allocator reserves the specular material slots", () => {
  const { context } = setupWebGLRenderer();
  const layout = callIn(context,
    "sceneAllocateTextureUnits({ shadowCount: 2, ibl: true, maxUnits: 16 })");
  assert.equal(layout.material.specularIntensity, 6);
  assert.equal(layout.material.specularColor, 7);
  assert.deepEqual(Array.from(layout.shadows), [8, 9]);
  assert.deepEqual({ ...layout.ibl }, { irradiance: 10, radiance: 11, brdfLUT: 12 });
  // Every returned slot is distinct: no material map collides with another
  // material map, a shadow cascade or an IBL unit.
  const materialUnits = Object.values(layout.material);
  assert.equal(new Set(materialUnits).size, materialUnits.length);
  assert.equal(new Set([...materialUnits, ...layout.shadows,
    layout.ibl.irradiance, layout.ibl.radiance, layout.ibl.brdfLUT]).size,
    materialUnits.length + layout.shadows.length + 3);
});

test("WebGL guarded max-texture-unit query falls back conservatively", () => {
  const { context } = setupWebGLRenderer();
  assert.equal(callIn(context, "scenePBRMaxTextureUnits(glWithUnits(32))"), 32);
  assert.equal(callIn(context, "scenePBRMaxTextureUnits(glWithUnits(16))"), 16);
  assert.equal(callIn(context, "scenePBRMaxTextureUnits({})"), 16);
  assert.equal(callIn(context, "scenePBRMaxTextureUnits(null)"), 16);
  assert.equal(callIn(context,
    "scenePBRMaxTextureUnits({ MAX_TEXTURE_IMAGE_UNITS: 34930, getParameter() { throw new Error('lost'); } })"), 16);
});

test("WebGL frame layout threads real GL unit limits", () => {
  const { context } = setupWebGLRenderer();
  const call = (maxUnits) => callIn(context,
    "scenePBRTextureLayoutForFrame(" +
    "[{ numCascades: 4, cascades: [{}, {}, {}, {}] }, { numCascades: 4, cascades: [{}, {}, {}, {}] }], [0, 1], " +
    "{ ibl: { radiance: {}, irradiance: {}, brdfLUT: {} } }, " + maxUnits + ")");
  // A 32-unit GL retains 8 cascades plus the three IBL units.
  const wide = call(32);
  assert.deepEqual(Array.from(wide.shadows), [8, 9, 10, 11, 12, 13, 14, 15]);
  assert.deepEqual({ ...wide.ibl }, { irradiance: 16, radiance: 17, brdfLUT: 18 });
  assert.equal(wide.warnings.length, 0);
  // A 16-unit GL keeps the supported non-HDR path with a boundary warning.
  const tight = call(16);
  // A 16-unit GL keeps the supported non-HDR path with a boundary warning:
  // only 5 shadow slots fit after the 8 material units, leaving 3 for IBL.
  assert.deepEqual(Array.from(tight.shadows), [8, 9, 10, 11, 12]);
  assert.deepEqual({ ...tight.ibl }, { irradiance: 13, radiance: 14, brdfLUT: 15 });
  assert.equal(tight.warnings.length > 0, true);
});

test("WebGL HDR IBL guard needs 20 sampler units", () => {
  const { context } = setupWebGLRenderer();
  const source = readRuntimeSource("webgl.ts");
  assert.match(source, /fragment-texture-units<20/);
  assert.doesNotMatch(source, /fragment-texture-units<19/);
  assert.equal(callIn(context, "scenePBRHDRIBLAvailable(glWithUnits(16))"), false);
  assert.equal(callIn(context, "scenePBRHDRIBLAvailable(glWithUnits(19))"), false);
  assert.equal(callIn(context, "scenePBRHDRIBLAvailable(glWithUnits(20))"), true);
  assert.equal(callIn(context, "scenePBRHDRIBLAvailable(glWithUnits(32))"), true);
});

test("WebGL PBR shader consumes the uploaded factors in direct and IBL paths", () => {
  const source = readRuntimeSource("webgl.ts");
  // The dedicated uniforms are declared, cached and uploaded.
  assert.match(source, /uniform vec3 u_specularF0;/);
  assert.match(source, /uniform float u_specularF90;/);
  assert.match(source, /specularF0: gl\.getUniformLocation\(program, "u_specularF0"\),/);
  assert.match(source, /specularF90: gl\.getUniformLocation\(program, "u_specularF90"\),/);
  assert.match(source, /gl\.uniform3f\(uniforms\.specularF0, specularFactors\.f0\[0\], specularFactors\.f0\[1\], specularFactors\.f0\[2\]\);/);
  assert.match(source, /gl\.uniform1f\(uniforms\.specularF90, specularFactors\.f90\);/);
  // Specular-intensity texture: declared, cached, loaded via the role-aware
  // table entry and sampled on the alpha channel only.
  assert.match(source, /uniform sampler2D u_specularIntensityMap;/);
  assert.match(source, /uniform bool u_hasSpecularIntensityMap;/);
  assert.match(source, /specularIntensityMap: gl\.getUniformLocation\(program, "u_specularIntensityMap"\),/);
  assert.match(source, /hasSpecularIntensityMap: gl\.getUniformLocation\(program, "u_hasSpecularIntensityMap"\),/);
  assert.match(source, /descriptor: "specularIntensity", role: "specular-intensity"/);
  assert.match(source, /unit: SCENE_TEXTURE_UNIT_MATERIALS\.specularIntensity\s*\}/);
  assert.match(source, /float specTex = texture\(u_specularIntensityMap, v_uv\)\.a;/);
  assert.match(source, /specF0 \*= specTex;/);
  assert.match(source, /specF90 \*= specTex;/);
  assert.doesNotMatch(source, /texture\(u_specularIntensityMap, v_uv\)\.rgb/);
  // Direct Schlick carries F90: an omitted intensity must scale the lobe.
  assert.match(source, /vec3 fresnelSchlick\(float cosTheta, vec3 F0, float F90\) \{/);
  assert.match(source, /return F0 \+ \(vec3\(F90\) - F0\) \* pow/);
  assert.match(source, /vec3 F = fresnelSchlick\(max\(dot\(H, V\), 0\.0\), F0, F90\);/);
  // The implicit-F90 Schlick form must not come back anywhere.
  assert.doesNotMatch(source, /return F0 \+ \(1\.0 - F0\) \* pow/);
  // Scalar dielectric diffuse, never the inverse RGB tint.
  assert.match(source, /float kD = \(1\.0 - max\(Fdiel\.x, max\(Fdiel\.y, Fdiel\.z\)\)\) \* \(1\.0 - metalness\);/);
  assert.doesNotMatch(source, /vec3 kD = \(vec3\(1\.0\) - F\) \* \(1\.0 - metalness\);/);
  // Split-sum IBL weights brdf.y by the mixed F90.
  assert.match(source, /vec3\(F90\) \* brdf\.y/);
  assert.doesNotMatch(source, /prefiltered \* \(F0 \* brdf\.x \+ brdf\.y\)/);
  // Roughness-adjusted environment Fresnel honours F90.
  assert.match(source, /vec3 fresnelSchlickRoughness\(float cosTheta, vec3 F0, float F90, float roughness\) \{/);
  // Fully-metal branch keeps dielectric settings out of a metal.
  // The source is quoted GLSL, so match the quoted lines.
  assert.match(source, /"    if \(metalness >= 1\.0\) \{",\s*\n\s*"        F0 = albedo;",\s*\n\s*"        F90 = 1\.0;",/);
  // The effective-F0 clamp happens before the intensity, never inside it.
  const helper = sliceBetween(source, "function scenePBRSpecularFactors", "  // Cache the base uniform locations");
  assert.match(helper, /Math\.min\(iorF0 \* color\[0\], 1\) \* intensity/);
  assert.doesNotMatch(helper, /Math\.min\(iorF0 \* intensity/);
  assert.doesNotMatch(helper, /color\[0\] \* intensity, 1\)/);
  // The alpha multiply sits between the uniform initializers and the
  // metallic mix.
  const multiplyAt = source.indexOf('"        specF0 *= specTex;",');
  const mixAt = source.indexOf('"    vec3 F0 = mix(specF0, albedo, metalness);",');
  assert.ok(multiplyAt >= 0 && mixAt > multiplyAt, "alpha multiply precedes the metallic mix");
});

test("WebGL specular-color log coefficients stay finite at the extremes", () => {
  const { context } = setupWebGLRenderer();
  const logs = (literal) => callIn(context, "scenePBRSpecularColorLogs(" + literal + ")");
  const defaultLog = Math.log2(0.04);

  // Omitted colour falls back to white: the log is exactly log2(IOR F0).
  const def = logs("{}");
  for (let c = 0; c < 3; c++) {
    assert.ok(Number.isFinite(def[c]), "default log coefficient finite");
    assert.ok(Math.abs(def[c] - defaultLog) <= 1e-6);
  }

  // Exact-zero channels (black tint or ior 1) use the finite -1e30 sentinel.
  const black = logs("{ specularColor: [0, 0, 0] }");
  for (let c = 0; c < 3; c++) assert.strictEqual(black[c], -1e30);
  const iorOne = logs("{ ior: 1, specularColor: [3, 3, 3] }");
  for (let c = 0; c < 3; c++) assert.strictEqual(iorOne[c], -1e30);

  // tinyIOR * hugeColor keeps a huge but finite positive log: no ceiling.
  const extreme = logs("{ ior: 1 + Number.EPSILON, specularColor: [Number.MAX_VALUE, Number.MAX_VALUE, Number.MAX_VALUE] }");
  for (let c = 0; c < 3; c++) {
    assert.ok(Number.isFinite(extreme[c]) && extreme[c] > 0, "extreme log stays finite and positive");
  }

  // Invalid colour arrays (wrong shape, negative, non-finite) fall back to
  // white and never produce NaN or Infinity.
  for (const bad of ["[1, 2]", "[-1, 0, 0]", "[Infinity, 1, 1]", '"junk"']) {
    const invalid = logs("{ specularColor: " + bad + " }");
    for (let c = 0; c < 3; c++) {
      assert.ok(Number.isFinite(invalid[c]));
      assert.ok(Math.abs(invalid[c] - defaultLog) <= 1e-6);
    }
  }
});

test("WebGL uploadMaterial uploads finite float32 specular-color log coefficients", () => {
  const { context } = setupWebGLRenderer();
  const upload = (literal) => callIn(context,
    "(() => { const gl = recordingGL(); const uniforms = uniformSlots();" +
    "uploadMaterial(gl, uniforms, " + literal + ", null);" +
    "return gl.floats.get('specularColorLog'); })()");
  const expect = (literal) => callIn(context, "scenePBRSpecularColorLogs(" + literal + ")");
  const checkVec3 = (logs, label) => {
    assert.ok(Array.isArray(logs) && logs.length === 3, label + " uploaded as a vec3");
    for (let c = 0; c < 3; c++) assert.ok(Number.isFinite(logs[c]), label + " log coefficient finite");
  };
  const defaultLog = Math.log2(0.04);

  // Default (omitted colour): exactly log2(IOR F0) on every channel.
  const def = upload("{}");
  checkVec3(def, "default");
  for (let c = 0; c < 3; c++) assert.ok(Math.abs(def[c] - defaultLog) <= 1e-6);

  // Invalid colour arrays fall back to white through the real upload path.
  for (const bad of ["[1, 2]", "[-1, 0, 0]", "[Infinity, 1, 1]", '"junk"']) {
    const invalid = upload("{ specularColor: " + bad + " }");
    checkVec3(invalid, "invalid " + bad);
    for (let c = 0; c < 3; c++) assert.ok(Math.abs(invalid[c] - defaultLog) <= 1e-6);
  }

  // The zero sentinel survives the upload as a finite number.
  const black = upload("{ specularColor: [0, 0, 0] }");
  checkVec3(black, "black sentinel");
  for (let c = 0; c < 3; c++) assert.strictEqual(black[c], expect("{ specularColor: [0, 0, 0] }")[c]);

  // tinyIOR * MAX_VALUE colour: used channels get the huge positive log,
  // the unused (small) channel keeps the tinyIOR log, and everything the
  // recorder captured is float32-representable where the magnitude allows.
  const literal = "{ ior: 1 + Number.EPSILON, specularColor: [Number.MAX_VALUE, Number.MAX_VALUE, 1] }";
  const extreme = upload(literal);
  const expectedExtreme = expect(literal);
  checkVec3(extreme, "extreme");
  for (let c = 0; c < 3; c++) {
    assert.strictEqual(extreme[c], expectedExtreme[c], "channel " + c + " matches the helper");
    if (Math.abs(extreme[c]) < 1e29) {
      assert.strictEqual(extreme[c], Math.fround(extreme[c]), "channel " + c + " float32-representable");
    }
  }
  assert.ok(extreme[0] > 0 && extreme[1] > 0, "MAX_VALUE channels yield a positive log");
  assert.ok(extreme[2] < 0 && Number.isFinite(extreme[2]), "unused channel retains the finite tinyIOR log");
});

test("WebGL specular-color map stays neutral when missing, pending or failed", () => {
  const { context } = setupWebGLRenderer();
  callIn(context, "setTextureState('pending-color.png', 'pending')");
  callIn(context, "setTextureState('failed-color.png', 'failed')");
  const upload = (literal) => callIn(context,
    "(() => { const gl = recordingGL(); const uniforms = uniformSlots();" +
    "uploadMaterial(gl, uniforms, " + literal + ", null);" +
    "return { gl, uniforms }; })()");

  // Missing: no flag, no sampler unit, no bind, fast path stays ready.
  let result = upload("{}");
  assert.equal(result.gl.ints.get("hasSpecularColorMap"), 0);
  assert.equal(result.gl.ints.get("specularColorMap"), undefined);
  assert.equal(result.gl.binds.size, 0);
  assert.equal(result.uniforms._lastMaterialTexturesReady, true);

  // Pending: neutral now, fast path blocked so a late load takes effect.
  result = upload("{ specularColorMap: 'pending-color.png' }");
  assert.equal(result.gl.ints.get("hasSpecularColorMap"), 0);
  assert.equal(result.gl.binds.get(7), undefined);
  assert.equal(result.uniforms._lastMaterialTexturesReady, false);

  // Failed: neutral, and it must not block the ready cache.
  result = upload("{ specularColorMap: 'failed-color.png' }");
  assert.equal(result.gl.ints.get("hasSpecularColorMap"), 0);
  assert.equal(result.gl.binds.get(7), undefined);
  assert.equal(result.uniforms._lastMaterialTexturesReady, true);
});

test("WebGL specular-color map binds unit 7 as sRGB with descriptor priority", () => {
  const { context } = setupWebGLRenderer();
  const colorUnit = callIn(context, "SCENE_TEXTURE_UNIT_MATERIALS.specularColor");
  assert.equal(colorUnit, 7);

  callIn(context, "setTextureState('color.png', 'loaded')");
  let gl = callIn(context,
    "(() => { const g = recordingGL(); uploadMaterial(g, uniformSlots()," +
    " { specularColorMap: 'color.png' }, null); return g; })()");
  assert.equal(gl.ints.get("hasSpecularColorMap"), 1);
  assert.equal(gl.ints.get("specularColorMap"), colorUnit);
  const bind = gl.binds.get(colorUnit);
  assert.ok(bind, "specular colour texture bound on its own unit");
  assert.equal(bind.target, gl.TEXTURE_2D);
  assert.strictEqual(bind.texture,
    callIn(context, "textureRecords().get('color.png').texture"));
  let load = callIn(context, "textureLoads()[textureLoads().length - 1]");
  assert.equal(load.url, "color.png");
  assert.equal(load.role, "specular-color");
  assert.equal(load.colorSpace, "srgb");

  // The descriptor wins over the legacy prop.
  callIn(context, "var colorDescriptor = { uri: 'color-descriptor.png' };");
  callIn(context, "setTextureState('color-descriptor.png', 'loaded')");
  gl = callIn(context,
    "(() => { const g = recordingGL(); uploadMaterial(g, uniformSlots()," +
    " { specularColorMap: 'color.png', textureDescriptors: { specularColor: colorDescriptor } }, null);" +
    "return g; })()");
  load = callIn(context, "textureLoads()[textureLoads().length - 1]");
  assert.equal(load.url, "color-descriptor.png");
  assert.strictEqual(load.descriptor, callIn(context, "colorDescriptor"));
  assert.equal(load.role, "specular-color");
  assert.equal(load.colorSpace, "srgb");
  assert.strictEqual(gl.binds.get(colorUnit).texture,
    callIn(context, "textureRecords().get('color-descriptor.png').texture"));
});

test("WebGL paired intensity and color maps bind distinct units without collisions", () => {
  const { context } = setupWebGLRenderer();
  callIn(context, "setTextureState('pair-i.png', 'loaded')");
  callIn(context, "setTextureState('pair-c.png', 'loaded')");
  const gl = callIn(context,
    "(() => { const g = recordingGL(); uploadMaterial(g, uniformSlots()," +
    " { specularIntensityMap: 'pair-i.png', specularColorMap: 'pair-c.png' }, null); return g; })()");
  assert.equal(gl.ints.get("specularIntensityMap"), 6);
  assert.equal(gl.ints.get("specularColorMap"), 7);
  assert.equal(gl.binds.get(6).texture.gosxTestName, "pair-i.png");
  assert.equal(gl.binds.get(7).texture.gosxTestName, "pair-c.png");
  assert.equal(gl.binds.size, 2);
});

test("WebGL specular-color late load takes effect and the loaded cache short-circuits", () => {
  const { context } = setupWebGLRenderer();
  callIn(context,
    "var colorMaterial = { specularColorMap: 'late-color.png' }; var colorUniforms = uniformSlots();");
  const uploadPersisted = () => callIn(context,
    "(() => { const gl = recordingGL(); uploadMaterial(gl, colorUniforms, colorMaterial, null);" +
    "return gl; })()");

  callIn(context, "setTextureState('late-color.png', 'pending')");
  let gl = uploadPersisted();
  assert.equal(callIn(context, "colorUniforms._lastMaterialTexturesReady"), false);
  assert.equal(gl.ints.get("hasSpecularColorMap"), 0);
  assert.equal(gl.binds.size, 0);

  callIn(context, "setTextureState('late-color.png', 'loaded')");
  gl = uploadPersisted();
  assert.equal(callIn(context, "colorUniforms._lastMaterialTexturesReady"), true);
  assert.equal(gl.ints.get("hasSpecularColorMap"), 1);
  assert.equal(gl.ints.get("specularColorMap"), 7);
  assert.strictEqual(gl.binds.get(7).texture,
    callIn(context, "textureRecords().get('late-color.png').texture"));

  // Same-material ready caching: the identical reference short-circuits.
  gl = uploadPersisted();
  assert.equal(gl.ints.size, 0);
  assert.equal(gl.binds.size, 0);
});

test("WebGL PBR shader samples the specular-color map RGB-only before the metallic mix", () => {
  const source = readRuntimeSource("webgl.ts");
  assert.match(source, /uniform sampler2D u_specularColorMap;/);
  assert.match(source, /uniform bool u_hasSpecularColorMap;/);
  assert.match(source, /uniform vec3 u_specularColorLog;/);
  assert.match(source, /specularColorMap: gl\.getUniformLocation\(program, "u_specularColorMap"\),/);
  assert.match(source, /hasSpecularColorMap: gl\.getUniformLocation\(program, "u_hasSpecularColorMap"\),/);
  assert.match(source, /specularColorLog: gl\.getUniformLocation\(program, "u_specularColorLog"\),/);
  assert.match(source, /descriptor: "specularColor", role: "specular-color"/);
  assert.match(source, /unit: SCENE_TEXTURE_UNIT_MATERIALS\.specularColor\s*\}/);
  assert.match(source,
    /gl\.uniform3f\(uniforms\.specularColorLog, specularColorLogs\[0\], specularColorLogs\[1\], specularColorLogs\[2\]\);/);
  // Linear RGB only; alpha is never read.
  const colorSample = source.match(/texture\(u_specularColorMap, v_uv\)[^;]*;/);
  assert.ok(colorSample && /\.rgb\s*;/.test(colorSample[0]) && !/\.a\s*;/.test(colorSample[0]),
    "specular-color sample reads RGB only");
  // Per-channel reconstruction with the finite sentinel guard and the
  // clamp-to-0-before-exp2 product scaled by the combined F90.
  assert.match(source, /texColor\.r > 0\.0 && u_specularColorLog\.r > -1e29/);
  assert.match(source,
    /exp2\(min\(u_specularColorLog\.r \+ log2\(texColor\.r\), 0\.0\)\) \* specF90/);
  // The colour-textured F0 lands in specF0 BEFORE the metallic mix, and the
  // block never reassigns specF90.
  const colorPos = source.indexOf('"    if (u_hasSpecularColorMap) {",');
  const assignPos = source.indexOf('"        specF0 = texF0;",', colorPos);
  const mixPos = source.indexOf('"    vec3 F0 = mix(specF0, albedo, metalness);",');
  assert.ok(colorPos >= 0 && assignPos > colorPos && assignPos < mixPos,
    "colour-textured specF0 is assigned before the metallic mix");
  assert.doesNotMatch(source.slice(colorPos, mixPos), /specF90\s*=/);
  // The fully-metal branch after the mix is untouched by the colour texture.
  assert.match(source, /"    if \(metalness >= 1\.0\) \{",\s*\n\s*"        F0 = albedo;",\s*\n\s*"        F90 = 1\.0;",/);
});

// --- WebGPU ------------------------------------------------------------------

function setupWebGPURenderer() {
  const source = readRuntimeSource("webgpu.ts");
  const bufferDecls = (source.match(/var\s+_materialUniform\w+\s*=\s*[^;\n]+;/g) || []).join("\n");
  const context = createSceneCoreContext();
  runFragment(context, [
    bufferDecls,
    sliceBetween(source, "function sceneWebGPUSRGBChannelToLinear", "var WGSL_COMMON_CONSTANTS"),
    sliceBetween(source, "function sceneWebGPUDielectricF0", "function materialUniformData"),
    sliceBetween(source, "function materialUniformData", "function wgpuCachedBindGroup"),
  ].join("\n"), "webgpu-specular-extract.js");
  return { source, context };
}

test("sceneNormalizeMaterialAlphaCutoff bridges from the base chunk into the WebGPU chunk", () => {
  // Chunk 1: the base scene3d bundle publishing the normalizer via 16d.
  const baseContext = createSceneCoreContext();
  runFragment(baseContext, "window.__gosx_scene3d_api = {};", "init-api.js");
  runFragment(baseContext, readBootstrapSource("16d-scene-webgl-bridge.ts"), "16d-scene-webgl-bridge.ts");
  const bridgeApi = callIn(baseContext, "window.__gosx_scene3d_api");
  assert.strictEqual(bridgeApi.sceneNormalizeMaterialAlphaCutoff,
    callIn(baseContext, "sceneNormalizeMaterialAlphaCutoff"));
  assert.equal(callIn(baseContext,
    "window.__gosx_scene3d_api.sceneNormalizeMaterialAlphaCutoff(0, null)"), 0);

  // Chunk 2: a different VM holding only the bridged API. The real lazy 26e
  // WebGPU prefix is an open IIFE; close it here and probe the lexical
  // alias materialUniformData resolves in the concatenated runtime.
  const webgpuContext = vm.createContext({
    console,
    window: { __gosx_scene3d_api: bridgeApi },
    navigator: { gpu: undefined },
  });
  runFragment(webgpuContext, [
    readBootstrapSource("26e-feature-scene3d-webgpu-prefix.ts"),
    "globalThis.__alphaCutoffProbe = {",
    "  fn: sceneNormalizeMaterialAlphaCutoff,",
    "  zero: sceneNormalizeMaterialAlphaCutoff(0, null),",
    "  half: sceneNormalizeMaterialAlphaCutoff('.5', null),",
    "  missing: sceneNormalizeMaterialAlphaCutoff(null, null),",
    "};",
    "})();",
  ].join("\n"), "26e-scene-webgpu-prefix-probe.js");
  const probe = vm.runInContext("globalThis.__alphaCutoffProbe", webgpuContext);
  assert.equal(typeof probe.fn, "function", "lazy WebGPU prefix resolved the bridged normalizer");
  assert.strictEqual(probe.zero, 0);
  assert.ok(close6(probe.half, 0.5));
  assert.strictEqual(probe.missing, null);
});

test("WebGPU materialUniformData normalizes alphaCutoff into slot 42", () => {
  const { context } = setupWebGPURenderer();
  const pack = (literal) => callIn(context,
    "materialUniformData(" + literal + ", false, null, null)");
  const cutoff = (literal) => pack("{ alphaCutoff: " + literal + " }").data[42];
  for (const bad of ["undefined", "null", "-0.25", "NaN", "Infinity", "false",
    "'oops'", "'var(--accent)'"]) {
    assert.strictEqual(cutoff(bad), -1, "alphaCutoff " + bad + " normalizes to -1");
  }
  assert.strictEqual(cutoff("0"), 0);
  assert.ok(close6(cutoff("'0.25'"), 0.25));
  assert.ok(close6(cutoff("'.5'"), 0.5));
  assert.strictEqual(cutoff("1"), 1);
  // Above 1 clamps to the finite sentinel 2, matching the WebGL renderer.
  assert.strictEqual(cutoff("Number.MAX_VALUE"), 2);
});

test("WebGPU material scratch buffer resets and neighbor slots stay put", () => {
  const { context } = setupWebGPURenderer();
  const pack = (literal) => callIn(context,
    "materialUniformData(" + literal + ", false, null, null)");
  const dirty = pack("{ alphaCutoff: 0.5, specularIntensity: 0.75 }");
  assert.ok(close6(dirty.data[42], 0.5));
  const clean = pack("{}");
  assert.strictEqual(clean.data[42], -1, "stale cutoff must not survive a repack");
  assert.strictEqual(clean.data[41], 0);
  assert.strictEqual(clean.data[43], 0);
  assert.strictEqual(clean.data.length, 52);
  for (let c = 0; c < 3; c++) assert.ok(close6(clean.data[44 + c], 0.04));
  assert.strictEqual(clean.data[47], 1);
  assert.strictEqual(clean.u[51], 0);
  const edge = pack("{ alphaCutoff: 1 }");
  assert.strictEqual(edge.data[41], 0);
  assert.strictEqual(edge.data[43], 0);
  for (let c = 0; c < 3; c++) assert.ok(close6(edge.data[44 + c], 0.04));
  const expectedLog = Math.log2(0.04);
  for (let c = 0; c < 3; c++) {
    assert.ok(Number.isFinite(edge.data[48 + c]));
    assert.ok(Math.abs(edge.data[48 + c] - expectedLog) <= 1e-6);
  }
  assert.strictEqual(edge.data.buffer.byteLength, 208);
});

test("WebGPU fragment shaders pin coverage discard and corrected alpha selects", () => {
  const { source } = setupWebGPURenderer();
  assert.match(source, /texAlpha = texAlbedo\.a;/);
  assert.ok((source.match(/coverage < cutoff/g) || []).length >= 3,
    "strictly-less discard in unlit, main, and water paths");
  assert.doesNotMatch(source, /coverage <= cutoff/);
  assert.strictEqual(
    (source.match(/select\(finalOpacity, 1\.0, alphaEnabled\)/g) || []).length, 2,
    "unlit and main survivors emit full opacity");
  assert.match(source, /select\(unmaskedOpacity, 1\.0, alphaEnabled\)/);
  assert.doesNotMatch(source, /select\(\w+, coverage, alphaEnabled\)/);
});

test("WebGPU instanceColor and masked shadow alpha varyings are flat (primitive-constant, no interpolation drift)", () => {
  // Every producer writes a primitive constant (ordinary vec4f(1), cull
  // vec4f(1), instanced per-instance constants), and every consumer (PBR,
  // water, masked shadow) reads it back. Perspective-correct interpolation of
  // a constant 1 can round just below 1, dropping opacity * alpha strictly
  // under the cutoff and discarding real fragments. @interpolate(flat) is
  // exact: provoking-vertex pass-through, no epsilon, no clamping. Output and
  // input structs must declare matching modes for the pipelines to link.
  const { source } = setupWebGPURenderer();
  assert.strictEqual(
    (source.match(/@location\(5\) @interpolate\(flat\) instanceColor: vec4f,/g) || []).length,
    5,
    "ordinary/instanced/cull vertex outputs + PBR/water fragment inputs all flat");
  assert.strictEqual(
    (source.match(/@location\(5\) instanceColor: vec4f,/g) || []).length, 0,
    "no smooth instanceColor declaration remains on either side");
  assert.strictEqual(
    (source.match(/@location\(1\) @interpolate\(flat\) alpha: f32,/g) || []).length,
    3,
    "masked shadow alpha: non-instanced + instanced vertex outputs and fragment input all flat");
  assert.strictEqual(
    (source.match(/@location\(1\) alpha: f32,/g) || []).length, 0,
    "no smooth masked-shadow alpha declaration remains");
  // Genuinely per-vertex varyings keep smooth (default) interpolation.
  assert.doesNotMatch(source,
    /@interpolate\(flat\)[^\n]*(uv|normal|worldPos|tangent|bitangent)/,
    "uv/normal/worldPos/tangent/bitangent must stay perspective-correct");
});

test("WebGPU materialUniformData packs finite effective specular factors", () => {
  const { source, context } = setupWebGPURenderer();
  // The material buffer grew for the aligned vec3f plus the F90 scalar; the
  // earlier 176-byte layout must be gone.
  assert.match(source, /var\s+_materialUniformBuf\s*=\s*new ArrayBuffer\(208\);/);
  assert.doesNotMatch(source, /var\s+_materialUniformBuf\s*=\s*new ArrayBuffer\(192\);/);
  assert.doesNotMatch(source, /var\s+_materialUniformBuf\s*=\s*new ArrayBuffer\(176\);/);

  const pack = (literal) => callIn(context,
    "materialUniformData(" + literal + ", false, null, null)");

  const def = pack("{}");
  assert.strictEqual(def.data.length, 52);
  for (let c = 0; c < 3; c++) assert.ok(close6(def.data[44 + c], 0.04));
  assert.strictEqual(def.data[47], 1);
  // Finite pre-clamp log coefficients at 48..50, neutral loaded-color flag at 51.
  const defaultLog = Math.log2(0.04);
  for (let c = 0; c < 3; c++) {
    assert.ok(Number.isFinite(def.data[48 + c]), "log coefficient finite");
    assert.ok(Math.abs(def.data[48 + c] - defaultLog) <= 1e-6);
  }
  assert.strictEqual(def.u[51], 0);
  // Legacy slots keep their offsets: dielectric F0 at 40, the specular
  // flag word at 41, the normalized alpha cutoff at 42 (-1 when unset),
  // and padding at 43.
  assert.ok(close6(def.data[40], expectedDielectricF0(1.5)));
  assert.strictEqual(def.data[41], 0);
  assert.strictEqual(def.data[42], -1);
  assert.strictEqual(def.data[43], 0);

  // Explicit 0 intensity is valid and zeroes the lobe; black tints too.
  const zero = pack("{ specularIntensity: 0 }");
  for (let c = 0; c < 3; c++) assert.strictEqual(zero.data[44 + c], 0);
  assert.strictEqual(zero.data[47], 0);
  const black = pack("{ specularColor: [0, 0, 0] }");
  for (let c = 0; c < 3; c++) assert.strictEqual(black.data[44 + c], 0);
  assert.strictEqual(black.data[47], 1);
  // Exact-zero channels use the finite -1e30 sentinel.
  // The sentinel is stored through the f32 buffer view, so it compares as
  // the packed Math.fround(-1e30) value, not the float64 -1e30.
  for (let c = 0; c < 3; c++) assert.strictEqual(black.data[48 + c], Math.fround(-1e30));

  // Clamp BEFORE intensity: min(0.04 * 100, 1) * 0.5 = 0.5 in red; the wrong
  // order would pack 1 in red.
  const clampOrder = pack("{ specularColor: [100, 1, 1], specularIntensity: 0.5 }");
  assert.ok(close6(clampOrder.data[44], 0.5));
  assert.ok(close6(clampOrder.data[45], 0.02));
  assert.ok(close6(clampOrder.data[46], 0.02));
  assert.strictEqual(clampOrder.data[47], 0.5);

  // IOR interaction: ior 0 maps the base F0 to 1; ior 1 maps it to 0.
  const iorZero = pack("{ ior: 0, specularColor: [0.5, 0.5, 0.5] }");
  for (let c = 0; c < 3; c++) assert.ok(close6(iorZero.data[44 + c], 0.5));
  const iorOne = pack("{ ior: 1, specularColor: [3, 3, 3] }");
  for (let c = 0; c < 3; c++) assert.strictEqual(iorOne.data[44 + c], 0);
  for (let c = 0; c < 3; c++) assert.strictEqual(iorOne.data[48 + c], Math.fround(-1e30));

  // Invalid inputs fall back to intensity 1 and white, never NaN/Infinity.
  const invalid = pack("{ specularIntensity: NaN, specularColor: [-1, 0, 0] }");
  for (let c = 0; c < 3; c++) assert.ok(close6(invalid.data[44 + c], 0.04));
  assert.strictEqual(invalid.data[47], 1);
  const badShape = pack("{ specularColor: [1, 2] }");
  for (let c = 0; c < 3; c++) assert.ok(close6(badShape.data[44 + c], 0.04));

  // Bounded finite uploads at the extremes.
  const huge = pack("{ specularColor: [Number.MAX_VALUE, 1, 1] }");
  for (let c = 0; c < 3; c++) assert.ok(Number.isFinite(huge.data[44 + c]));
  assert.ok(close6(huge.data[44], 1));
  assert.ok(close6(huge.data[45], 0.04));
  assert.ok(Number.isFinite(huge.data[48]) && huge.data[48] > 0, "huge tint keeps a finite positive log");
  const epsIor = pack("{ ior: 1 + Number.EPSILON }");
  const epsF0 = Math.pow(Number.EPSILON / (2 + Number.EPSILON), 2);
  for (let c = 0; c < 3; c++) {
    assert.ok(Number.isFinite(epsIor.data[44 + c]));
    assert.ok(close6(epsIor.data[44 + c], epsF0));
  }
  assert.strictEqual(epsIor.data[47], 1);

  // Number.MAX_VALUE IOR packs an F0 of exactly 1 in every channel.
  const maxIor = pack("{ ior: Number.MAX_VALUE }");
  for (let c = 0; c < 3; c++) assert.ok(close6(maxIor.data[44 + c], 1));
  assert.strictEqual(maxIor.data[47], 1);

  // Combined extreme: the float64 clamp lands red on exactly 1 and never
  // underflows the tiny eps base F0 on the untouched channels.
  const combined = pack("{ ior: 1 + Number.EPSILON, specularColor: [Number.MAX_VALUE, 1, 1] }");
  assert.ok(close6(combined.data[44], 1));
  assert.ok(close6(combined.data[45], epsF0));
  assert.ok(close6(combined.data[46], epsF0));
  assert.strictEqual(combined.data[47], 1);
  // IOR 1+EPS with MAX_VALUE colour: the log sum is huge but finite; no 1e32
  // ceiling is applied (a ceiling would corrupt the reconstruction below).
  assert.ok(Number.isFinite(combined.data[48]) && combined.data[48] > 0);
});

test("WebGPU WGSL consumes the uploaded factors in direct and IBL paths", () => {
  const source = readRuntimeSource("webgpu.ts");
  assert.match(source, /"    specularF0: vec3f,",/);
  assert.match(source, /"    specularF90: f32,",/);
  // Production uses a mutable var because the color-texture slice updates
  // specF0 before the metallic mix; the guards below cover the diffuse and
  // environment consumers and the before-mix assignment.
  assert.match(source, /var specF0 = material\.specularF0( \* specIntensity)?;/);
  assert.match(source, /var F0 = mix\(specF0, albedo, metalness\);/);
  assert.match(source, /var F90 = mix\(specF90, 1\.0, metalness\);/);
  // Direct Schlick carries F90: an omitted intensity must scale the lobe.
  assert.match(source, /fn fresnelSchlick\(cosTheta: f32, F0: vec3f, F90: f32\) -> vec3f \{/);
  assert.match(source, /return F0 \+ \(vec3f\(F90\) - F0\) \* pow/);
  assert.match(source, /let F = fresnelSchlick\(max\(dot\(H, V\), 0\.0\), F0, F90\);/);
  // The implicit-F90 Schlick form must not come back anywhere.
  assert.doesNotMatch(source, /return F0 \+ \(1\.0 - F0\) \* pow/);
  // Scalar dielectric diffuse, never the inverse RGB tint.
  assert.match(source, /let kD = \(1\.0 - max\(Fdiel\.x, max\(Fdiel\.y, Fdiel\.z\)\)\) \* \(1\.0 - metalness\);/);
  assert.doesNotMatch(source, /let kD = \(vec3f\(1\.0\) - F\) \* \(1\.0 - metalness\);/);
  // Split-sum IBL weights brdf.y by the mixed F90.
  assert.match(source, /vec3f\(F90\) \* brdf\.y/);
  assert.doesNotMatch(source, /prefiltered \* \(F0 \* brdf\.x \+ brdf\.y\)/);
  // Roughness-adjusted environment Fresnel honours F90.
  assert.match(source, /fn fresnelSchlickRoughness\(cosTheta: f32, F0: vec3f, F90: f32, roughness: f32\) -> vec3f \{/);
  // The rect-area approximate specular path takes the mixed factors too.
  assert.match(source, /fn rectAreaLightRadiance\(light: Light, P: vec3f, N: vec3f, V: vec3f, albedo: vec3f, roughness: f32, metalness: f32, F0: vec3f, F90: f32, NoV: f32\)/);
  assert.match(source, /rectAreaLightRadiance\(light, in\.worldPos, N, V, albedo, roughness, metalness, F0, F90, NoV\)/);
  // Fully-metal branch keeps dielectric settings out of a metal.
  assert.match(source, /"    if \(metalness >= 1\.0\) \{",\s*\n\s*"        F0 = albedo;",\s*\n\s*"        F90 = 1\.0;",/);
  // Specular-intensity texture slice: group1 bindings 13/14 exist, the flag
  // reuses the alignment word, and the linear ALPHA channel multiplies BOTH
  // shared factors BEFORE the metallic mix.
  assert.match(source, /"@group\(1\) @binding\(13\) var specularIntensityTex: texture_2d<f32>;"/);
  assert.match(source, /"@group\(1\) @binding\(14\) var specularIntensitySamp: sampler;"/);
  assert.match(source, /"    hasSpecularIntensityMap: u32,",/);
  assert.match(source, /var specF0 = material\.specularF0 \* specIntensity;/);
  assert.match(source, /let specF90 = material\.specularF90 \* specIntensity;/);
  const specSample = source.match(/specIntensity = textureSample\(specularIntensityTex[^;]*;/);
  assert.ok(specSample && /\.a\s*;/.test(specSample[0]) && !/\.rgb/.test(specSample[0]),
    "specular-intensity sample reads the alpha channel only");
  assert.match(source, /\{ prop: "specularIntensityMap", descriptor: "specularIntensity", role: "specular-intensity", colorSpace: "linear", index: 41 \}/);
  // Specular-color slice: bindings 15/16, RGB-only sampling, loaded-color
  // flag at u32 51, exact-white/zero/sentinel guards and the
  // clamp-to-0-before-exp2 reconstruction scaled by the combined F90.
  assert.match(source, /"@group\(1\) @binding\(15\) var specularColorTex: texture_2d<f32>;"/);
  assert.match(source, /"@group\(1\) @binding\(16\) var specularColorSamp: sampler;"/);
  assert.match(source, /"    hasSpecularColorMap: u32,",/);
  assert.match(source, /\{ prop: "specularColorMap", descriptor: "specularColor", role: "specular-color", colorSpace: "srgb", index: 51 \}/);
  const colorSample = source.match(/textureSample\(specularColorTex[^;]*;/);
  assert.ok(colorSample && /\.rgb\s*;/.test(colorSample[0]) && !/\.a\s*;/.test(colorSample[0]),
    "specular-color sample reads RGB only");
  assert.match(source, /texColor\.r == 1\.0/);
  assert.match(source, /texColor\.r > 0\.0 && material\.specularColorLog\.r > -1e29/);
  assert.match(source, /exp2\(min\(material\.specularColorLog\.r \+ log2\(texColor\.r\), 0\.0\)\) \* specF90/);
  const colorMapPos = source.indexOf('if (material.hasSpecularColorMap != 0u) {');
  const sharedAssignPos = source.indexOf('specF0 = texF0;', colorMapPos);
  const mixPos = source.indexOf('var F0 = mix(specF0, albedo, metalness);');
  assert.ok(colorMapPos >= 0 && sharedAssignPos > colorMapPos && sharedAssignPos < mixPos,
    "shared specF0 is assigned before the F0 mix");
  assert.doesNotMatch(source.slice(colorMapPos, mixPos), /specF90\s*=/);
  // The effective-F0 clamp happens before the intensity, never inside it.
  const helper = sliceBetween(source, "function sceneWebGPUSpecularFactors", "    function materialUniformData");
  assert.match(helper, /Math\.min\(iorF0 \* color\[0\], 1\) \* intensity/);
  assert.doesNotMatch(helper, /Math\.min\(iorF0 \* intensity/);
  assert.doesNotMatch(helper, /color\[0\] \* intensity, 1\)/);
});

// --- WebGPU recording-boundary material binding ------------------------------

function setupWebGPUMaterialBinding() {
  const source = readRuntimeSource("webgpu.ts");
  const context = createSceneCoreContext();
  const bufferDecls = (source.match(/var\s+_materialUniform\w+\s*=\s*[^;\n]+;/g) || []).join("\n");
  runFragment(context, [
    bufferDecls,
    sliceBetween(source, "function sceneWebGPUSRGBChannelToLinear", "var WGSL_COMMON_CONSTANTS"),
    sliceBetween(source, "function sceneWebGPUDielectricF0", "function materialUniformData"),
    sliceBetween(source, "function materialUniformData", "function wgpuCachedBindGroup"),
    sliceBetween(source, "function wgpuCachedBindGroup", "function createMaterialBindGroup"),
    sliceBetween(source, "function createMaterialBindGroup", "    // _frameBindGroupCache memoizes"),
  ].join("\n"), "webgpu-material-bind-extract.js");
  return { source, context };
}

// Stubs only the GPU resource and texture-loader boundaries; the production
// materialUniformData, createMaterialBindGroup and bind-group cache execute
// for real against the real 208-byte shared buffer.
function makeGPUHarness(context, textureStates) {
  const calls = { loads: [], bindGroups: [], buffers: [] };
  context.GPUBufferUsage = { UNIFORM: 0x40, COPY_DST: 0x8 };
  context.placeholderView = { __view: "placeholder" };
  context.linearSampler = { __sampler: "linear" };
  context.textureCache = {};
  context.defaultMaterialOwner = {};
  context.materialBindGroupLayout = { __layout: "material" };
  context.wgpuLoadTexture = function (device, url, cache, descriptor, role, colorSpace) {
    calls.loads.push({ url, role, colorSpace });
    const state = textureStates[url];
    if (!state || !state.loaded) {
      return state ? { loaded: false, pending: !!state.pending, failed: !!state.failed } : null;
    }
    if (!state.view) state.view = { __view: url };
    return { loaded: true, view: state.view };
  };
  context.wgpuCachedTrackedBuffer = function (owner, slot) {
    if (!owner[slot]) {
      const buffer = { __buffer: slot };
      owner[slot] = buffer;
      calls.buffers.push(buffer);
    }
    return owner[slot];
  };
  context.device = {
    createBindGroup: (desc) => {
      calls.bindGroups.push(desc);
      return { __bg: calls.bindGroups.length };
    },
  };
  // Capture the shared uniform views the production function mutates; a
  // second materialUniformData call would re-zero the flags.
  const realPack = context.materialUniformData;
  context.__lastUniform = null;
  context.materialUniformData = function (...args) {
    const packed = realPack(...args);
    context.__lastUniform = packed;
    return packed;
  };
  return calls;
}

function lastBoundResource(calls, binding) {
  const last = calls.bindGroups[calls.bindGroups.length - 1];
  assert.ok(last, "a bind group was created");
  const entry = last.entries.find((candidate) => candidate.binding === binding);
  assert.ok(entry, "binding " + binding + " present in the created bind group");
  return entry.resource;
}

test("WebGPU specular-intensity map stays neutral while missing, pending or failed", () => {
  const { context } = setupWebGPUMaterialBinding();
  const calls = makeGPUHarness(context, {
    "pending.png": { pending: true },
    "failed.png": { failed: true },
  });
  context.owner = {};

  // No map at all: flag 0, placeholder bound.
  context.material = {};
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[41], 0);
  assert.strictEqual(lastBoundResource(calls, 13), context.placeholderView);

  // Pending load: pending flag consumed, hasSpecularIntensityMap stays 0 and
  // the neutral placeholder is bound.
  context.material = { textureDescriptors: { specularIntensity: { uri: "pending.png" } } };
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[41], 0);
  assert.strictEqual(lastBoundResource(calls, 13), context.placeholderView);
  assert.strictEqual(calls.loads[calls.loads.length - 1].role, "specular-intensity");
  assert.strictEqual(calls.loads[calls.loads.length - 1].colorSpace, "linear");

  // Failed load: identical neutral treatment.
  context.material = { textureDescriptors: { specularIntensity: { uri: "failed.png" } } };
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[41], 0);
  assert.strictEqual(lastBoundResource(calls, 13), context.placeholderView);
});

test("WebGPU specular-intensity loaded map binds the real view and reuses the bind group", () => {
  const { context } = setupWebGPUMaterialBinding();
  const view = { __view: "spec-intensity" };
  const calls = makeGPUHarness(context, { "spec.png": { loaded: true, view } });
  context.material = { textureDescriptors: { specularIntensity: { uri: "spec.png" } } };
  context.owner = {};

  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[41], 1);
  assert.strictEqual(lastBoundResource(calls, 13), view);
  assert.strictEqual(lastBoundResource(calls, 14), context.linearSampler);
  const load = calls.loads[calls.loads.length - 1];
  assert.strictEqual(load.url, "spec.png");
  assert.strictEqual(load.role, "specular-intensity");
  assert.strictEqual(load.colorSpace, "linear");

  // Same view again: the bind group is reused, not recreated.
  const created = calls.bindGroups.length;
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(calls.bindGroups.length, created);
  // The created bind group carries exactly the group1 uniform/texture/sampler
  // pairs, including the new specular-intensity texture and sampler.
  // entries is a VM-created array; Array.from copies it into a host array so
  // deepStrictEqual compares primitive numbers rather than foreign prototypes.
  const bindings = Array.from(calls.bindGroups[0].entries, (entry) => entry.binding);
  assert.deepStrictEqual(bindings, [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]);
});

test("WebGPU specular-intensity late load and new view invalidate the cached bind group", () => {
  const { context } = setupWebGPUMaterialBinding();
  const state = { loaded: false };
  const calls = makeGPUHarness(context, { "late.png": state });
  context.material = { textureDescriptors: { specularIntensity: { uri: "late.png" } } };
  context.owner = {};

  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[41], 0);
  assert.strictEqual(calls.bindGroups.length, 1);

  // Late load: the cache must invalidate and bind the actual view.
  const lateView = { __view: "late-loaded" };
  state.loaded = true;
  state.view = lateView;
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[41], 1);
  assert.strictEqual(calls.bindGroups.length, 2);
  assert.strictEqual(lastBoundResource(calls, 13), lateView);

  // Same view: reuse, no new bind group.
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(calls.bindGroups.length, 2);

  // A different view for the same URL invalidates again.
  const otherView = { __view: "late-loaded-2" };
  state.view = otherView;
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(calls.bindGroups.length, 3);
  assert.strictEqual(lastBoundResource(calls, 13), otherView);
});

test("WebGPU material bind group layout declares specular-intensity texture and sampler at 13/14", () => {
  const source = readRuntimeSource("webgpu.ts");
  const context = createSceneCoreContext();
  context.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2 };
  runFragment(context,
    sliceBetween(source, "function wgpuCreateMaterialBindGroupLayout", "function wgpuCreatePointsBindGroupLayout"),
    "webgpu-layout-extract.js");
  context.device = { createBindGroupLayout: (desc) => desc };
  const desc = callIn(context, "wgpuCreateMaterialBindGroupLayout(device)");
  assert.strictEqual(desc.entries.length, 17);
  // The found entry objects were created inside the VM with foreign Object
  // prototypes; compare JSON roundtrips so only the values matter.
  assert.deepStrictEqual(JSON.parse(JSON.stringify(
    desc.entries.find((entry) => entry.binding === 13))),
    { binding: 13, visibility: 2, texture: { sampleType: "float" } });
  assert.deepStrictEqual(JSON.parse(JSON.stringify(
    desc.entries.find((entry) => entry.binding === 14))),
    { binding: 14, visibility: 2, sampler: {} });
  assert.deepStrictEqual(JSON.parse(JSON.stringify(
    desc.entries.find((entry) => entry.binding === 15))),
    { binding: 15, visibility: 2, texture: { sampleType: "float" } });
  assert.deepStrictEqual(JSON.parse(JSON.stringify(
    desc.entries.find((entry) => entry.binding === 16))),
    { binding: 16, visibility: 2, sampler: {} });
});

test("WebGPU packed log coefficients reconstruct clamped reflectance numerically", () => {
  const { context } = setupWebGPURenderer();
  const pack = (literal) => callIn(context, "materialUniformData(" + literal + ", false, null, null)");
  const TOL = 1e-6;

  // JS reconstruction of the production WGSL expression
  // exp2(min(logCoef + log2(texel), 0)) * specF90 for a positive texel.
  // Math.fround models the f32 shader math; this validates the PACKED f32
  // coefficients and is NOT real GPU execution (browser tests follow).
  const reconstruct = (texel, logCoef, f90) => {
    if (texel === 1) return null; // exact white takes the specF0 branch
    if (!(texel > 0) || logCoef <= -1e29) return 0;
    const sum = Math.fround(logCoef + Math.fround(Math.log2(texel)));
    // JavaScript has no Math.exp2; 2 ** sum is the actual exponentiation
    // operation matching the WGSL exp2, with the same fround sequence.
    return Math.fround(Math.fround(2 ** Math.min(sum, 0)) * Math.fround(f90));
  };

  const def = pack("{}");
  // Fractional 0.1 texel: unclamped 0.04 * 0.1.
  const frac = reconstruct(0.1, def.data[48], def.data[47]);
  assert.ok(Math.abs(frac - 0.04 * 0.1) <= TOL, "0.1 texel reconstructs 0.004, got " + frac);
  // Exact white retains the old effective F0 (the specF0 branch value).
  assert.ok(close6(def.data[44], 0.04));
  // Sentinel reconstructs to exact zero for any positive texel.
  assert.strictEqual(reconstruct(0.5, -1e30, 1), 0);

  // Intensity 0.5 with a 128/255 linear intensity-map alpha: F90 combines
  // both before scaling the color-textured F0.
  const half = pack("{ specularIntensity: 0.5 }");
  const alpha = 128 / 255;
  const halfFrac = reconstruct(0.1, half.data[48], Math.fround(half.data[47] * Math.fround(alpha)));
  assert.ok(Math.abs(halfFrac - 0.04 * 0.1 * (0.5 * alpha)) <= TOL,
    "intensity-map alpha scales the color-textured F0, got " + halfFrac);

  // Real counterexample: IOR 1+EPS with a Number.MAX_VALUE colour keeps a
  // huge finite log (the rejected 1e32 ceiling returned 0.1232595 here).
  const extreme = pack("{ ior: 1 + Number.EPSILON, specularColor: [Number.MAX_VALUE, Number.MAX_VALUE, Number.MAX_VALUE] }");
  assert.ok(Number.isFinite(extreme.data[48]) && extreme.data[48] > 0);
  assert.ok(Math.abs(reconstruct(0.1, extreme.data[48], extreme.data[47]) - 1) <= TOL,
    "0.1 texel with a huge coefficient clamps to 1 before intensity");
  // Zero texel with the same huge coefficient stays exactly 0, never 1.
  assert.strictEqual(reconstruct(0, extreme.data[48], extreme.data[47]), 0);
});

test("WebGPU specular-color map binds at 15/16 with srgb role and flags 41/51 independent", () => {
  const { context } = setupWebGPUMaterialBinding();
  const iView = { __view: "intensity" };
  const cView = { __view: "color" };
  const calls = makeGPUHarness(context, {
    "i.png": { loaded: true, view: iView },
    "c.png": { loaded: true, view: cView },
    "pending-c.png": { pending: true },
    "failed-c.png": { failed: true },
  });
  context.owner = {};

  // Both maps together: independent flags and views.
  context.material = { textureDescriptors: {
    specularIntensity: { uri: "i.png" }, specularColor: { uri: "c.png" } } };
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[41], 1);
  assert.strictEqual(context.__lastUniform.u[51], 1);
  assert.strictEqual(lastBoundResource(calls, 13), iView);
  assert.strictEqual(lastBoundResource(calls, 15), cView);
  assert.strictEqual(lastBoundResource(calls, 16), context.linearSampler);
  const bothLoads = calls.loads.slice(-2);
  assert.ok(bothLoads.some((l) => l.role === "specular-intensity" && l.colorSpace === "linear"));
  assert.ok(bothLoads.some((l) => l.role === "specular-color" && l.colorSpace === "srgb"));

  // Descriptor URI wins over the legacy prop.
  context.material = { specularColorMap: "legacy.png",
    textureDescriptors: { specularColor: { uri: "c.png" } } };
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  const lastLoad = calls.loads[calls.loads.length - 1];
  assert.strictEqual(lastLoad.url, "c.png");
  assert.strictEqual(lastLoad.role, "specular-color");
  assert.strictEqual(lastLoad.colorSpace, "srgb");
  assert.strictEqual(context.__lastUniform.u[51], 1);

  // Legacy prop alone works.
  context.material = { specularColorMap: "c.png" };
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[51], 1);
  assert.strictEqual(lastBoundResource(calls, 15), cView);

  // Missing / pending / failed stay neutral: flag 0, placeholder view at 15.
  context.material = {};
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[51], 0);
  assert.strictEqual(lastBoundResource(calls, 15), context.placeholderView);
  context.material = { textureDescriptors: { specularColor: { uri: "pending-c.png" } } };
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[51], 0);
  context.material = { textureDescriptors: { specularColor: { uri: "failed-c.png" } } };
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[51], 0);
  assert.strictEqual(lastBoundResource(calls, 15), context.placeholderView);
});

test("WebGPU specular-color late load and new view invalidate the cached bind group", () => {
  const { context } = setupWebGPUMaterialBinding();
  const state = { loaded: false };
  const calls = makeGPUHarness(context, { "late-c.png": state });
  context.material = { textureDescriptors: { specularColor: { uri: "late-c.png" } } };
  context.owner = {};

  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[51], 0);
  assert.strictEqual(calls.bindGroups.length, 1);

  const lateView = { __view: "late-color" };
  state.loaded = true;
  state.view = lateView;
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(context.__lastUniform.u[51], 1);
  assert.strictEqual(calls.bindGroups.length, 2);
  assert.strictEqual(lastBoundResource(calls, 15), lateView);

  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(calls.bindGroups.length, 2);

  state.view = { __view: "late-color-2" };
  callIn(context, "createMaterialBindGroup(material, false, owner, null, null)");
  assert.strictEqual(calls.bindGroups.length, 3);
  assert.strictEqual(lastBoundResource(calls, 15), state.view);
});
