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
    sliceBetween(source, "function scenePBRSRGBChannelToLinear", "const SCENE_PBR_VERTEX_SOURCE"),
    sliceBetween(source, "function scenePBRDielectricF0", "function scenePBRCacheBaseUniforms"),
    sliceBetween(source, "function uploadCustomUniforms", "function uploadMaterial"),
    sliceBetween(source, "function uploadMaterial", "function applyBlendMode"),
    // Recording GL boundary: exactly the uniform setters uploadMaterial
    // issues, keyed by the slot's production uniform name.
    "function recordingGL() {" +
      "const floats = new Map();" +
      "return {" +
        "floats," +
        "uniform1f(loc, v) { floats.set(loc && loc.name, v); }," +
        "uniform2f(loc, a, b) { floats.set(loc && loc.name, [a, b]); }," +
        "uniform3f(loc, a, b, c) { floats.set(loc && loc.name, [a, b, c]); }," +
        "uniform4f(loc, a, b, c, d) { floats.set(loc && loc.name, [a, b, c, d]); }," +
        "uniform1i(loc, v) { floats.set(loc && loc.name, v); }," +
      "};" +
    "}",
    "function uniformSlots() {" +
      "const slots = { customUniforms: null };" +
      "for (const name of ['albedo', 'roughness', 'metalness', 'clearcoat', 'sheen'," +
        "'transmission', 'iridescence', 'anisotropy', 'specularF0', 'specularF90'," +
        "'emissive', 'opacity', 'unlit', 'hasAlbedoMap', 'hasNormalMap', 'hasRoughnessMap'," +
        "'hasMetalnessMap', 'hasEmissiveMap', 'hasOcclusionMap']) {" +
        "slots[name] = { name };" +
      "}" +
      "return slots;" +
    "}",
    "function scenePBRHDRIBLAvailable() { return false; }",
    "function scenePBRLoadTexture() { return null; }",
    "function scenePBRBindTexture() {}",
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

test("WebGL PBR shader consumes the uploaded factors in direct and IBL paths", () => {
  const source = readRuntimeSource("webgl.ts");
  // The dedicated uniforms are declared, cached and uploaded.
  assert.match(source, /uniform vec3 u_specularF0;/);
  assert.match(source, /uniform float u_specularF90;/);
  assert.match(source, /specularF0: gl\.getUniformLocation\(program, "u_specularF0"\),/);
  assert.match(source, /specularF90: gl\.getUniformLocation\(program, "u_specularF90"\),/);
  assert.match(source, /gl\.uniform3f\(uniforms\.specularF0, specularFactors\.f0\[0\], specularFactors\.f0\[1\], specularFactors\.f0\[2\]\);/);
  assert.match(source, /gl\.uniform1f\(uniforms\.specularF90, specularFactors\.f90\);/);
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

test("WebGPU materialUniformData packs finite effective specular factors", () => {
  const { source, context } = setupWebGPURenderer();
  // The material buffer grew for the aligned vec3f plus the F90 scalar; the
  // earlier 176-byte layout must be gone.
  assert.match(source, /var\s+_materialUniformBuf\s*=\s*new ArrayBuffer\(192\);/);
  assert.doesNotMatch(source, /var\s+_materialUniformBuf\s*=\s*new ArrayBuffer\(176\);/);

  const pack = (literal) => callIn(context,
    "materialUniformData(" + literal + ", false, null, null)");

  const def = pack("{}");
  assert.strictEqual(def.data.length, 48);
  for (let c = 0; c < 3; c++) assert.ok(close6(def.data[44 + c], 0.04));
  assert.strictEqual(def.data[47], 1);
  // Legacy slots keep their offsets: dielectric F0 at 40, zeroed alignment
  // padding at 41..43.
  assert.ok(close6(def.data[40], expectedDielectricF0(1.5)));
  assert.strictEqual(def.data[41], 0);
  assert.strictEqual(def.data[42], 0);
  assert.strictEqual(def.data[43], 0);

  // Explicit 0 intensity is valid and zeroes the lobe; black tints too.
  const zero = pack("{ specularIntensity: 0 }");
  for (let c = 0; c < 3; c++) assert.strictEqual(zero.data[44 + c], 0);
  assert.strictEqual(zero.data[47], 0);
  const black = pack("{ specularColor: [0, 0, 0] }");
  for (let c = 0; c < 3; c++) assert.strictEqual(black.data[44 + c], 0);
  assert.strictEqual(black.data[47], 1);

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
});

test("WebGPU WGSL consumes the uploaded factors in direct and IBL paths", () => {
  const source = readRuntimeSource("webgpu.ts");
  assert.match(source, /"    specularF0: vec3f,",/);
  assert.match(source, /"    specularF90: f32,",/);
  assert.match(source, /let specF0 = material\.specularF0( \* specIntensity)?;/);
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
  assert.match(source, /let specF0 = material\.specularF0 \* specIntensity;/);
  assert.match(source, /let specF90 = material\.specularF90 \* specIntensity;/);
  const specSample = source.match(/specIntensity = textureSample\(specularIntensityTex[^;]*;/);
  assert.ok(specSample && /\.a\s*;/.test(specSample[0]) && !/\.rgb/.test(specSample[0]),
    "specular-intensity sample reads the alpha channel only");
  assert.match(source, /\{ prop: "specularIntensityMap", descriptor: "specularIntensity", role: "specular-intensity", colorSpace: "linear", index: 41 \}/);
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
// for real against the real 192-byte shared buffer.
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
  assert.deepStrictEqual(bindings, [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14]);
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
  assert.strictEqual(desc.entries.length, 15);
  // The found entry objects were created inside the VM with foreign Object
  // prototypes; compare JSON roundtrips so only the values matter.
  assert.deepStrictEqual(JSON.parse(JSON.stringify(
    desc.entries.find((entry) => entry.binding === 13))),
    { binding: 13, visibility: 2, texture: { sampleType: "float" } });
  assert.deepStrictEqual(JSON.parse(JSON.stringify(
    desc.entries.find((entry) => entry.binding === 14))),
    { binding: 14, visibility: 2, sampler: {} });
});
