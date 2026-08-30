"use strict";

const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
// Scene3D material IOR slice: numeric contract, normalization carry-through,
// profile cache key differentiation, named-material/model/instanced plumbing,
// planner signature invalidation, and the PBR shader uniform wiring for the
// authored normal-incidence dielectric Fresnel.
//
// Everything below executes production source text inside a VM sandbox: the
// scene primitives/utils/math/geometry/material fragments whole, the runtime
// core trimmed right before its "// Scene3D shared API" export block, and the
// scene planner whole — loaded in production chunk order, no extraction for
// anything inside those fragments. Nested runtime functions outside them
// (mount override plumbing, WebGL/WebGPU shader helpers) are sliced between
// explicit start and end markers taken from the current source: no
// next-declaration regex, no brace counting, and no reference-error recovery
// machinery. The WebGL case drives the production uploadMaterial through a
// recording GL boundary with stubs only for texture fetching and HDRI
// availability — a unit-boundary proof of the upload contract, not a native
// browser claim.

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

// The core's trailing export block reads engine-frame and IR helpers
// (cancelEngineFrame and friends) that only exist in the assembled
// monolith, so loading the file whole throws before any test runs.
// Everything these tests exercise sits above that marker; trim exactly
// there instead of faking the missing boot symbols.
function trimBeforeSharedApiExport(source) {
  const at = source.indexOf(SHARED_API_EXPORT_MARKER);
  assert.ok(at >= 0, "core '// Scene3D shared API' export marker located");
  return source.slice(0, at);
}

function runFragment(context, source, filename) {
  vm.runInContext(source, context, { filename });
}

// sliceBetween cuts one production function (or declaration run) out of its
// source between two exact markers from the current source. Nested/runtime
// functions live inside larger scopes, so the end marker is the explicit
// next declaration named in the source, never a heuristic.
function sliceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  assert.ok(start >= 0, "start marker located: " + startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(end > start, "end marker located after start: " + endMarker);
  return source.slice(start, end);
}

function indexOfMatch(source, pattern) {
  const match = pattern.exec(source);
  return match === null ? -1 : match.index;
}

// Fragments load whole, in production chunk order, exactly as the runtime
// assembles them. The planner loads whole too: its hash and CSS signature
// functions are exercised directly.
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
  runFragment(context, readBootstrapSource("15b-scene-planner.ts"), "15b-scene-planner.ts");
  return context;
}

function callIn(context, expression) {
  return vm.runInContext(expression, context, { filename: "scene-ior-expression.js" });
}

// Spec formula used only to compute expectations: F0 = ((ior-1)/(ior+1))^2,
// with ior 0 pinning the dielectric Fresnel to 1. The functions under test
// are the production implementations.
function expectedDielectricF0(ior) {
  return ior === 0 ? 1 : Math.fround(((ior - 1) / (ior + 1)) ** 2);
}

// --- numeric contract -------------------------------------------------------

test("sceneNormalizeMaterialIor enforces the KHR ior numeric contract", () => {
  const context = createSceneCoreContext();
  const normalize = (valueExpression, fallbackExpression) =>
    callIn(context, "sceneNormalizeMaterialIor(" + valueExpression + ", " + fallbackExpression + ")");

  // Missing, null, booleans and empty strings default; they are never
  // coerced into the zero compatibility mode.
  assert.strictEqual(normalize("undefined", "1.5"), 1.5);
  assert.strictEqual(normalize("null", "1.5"), 1.5);
  assert.strictEqual(normalize("false", "1.5"), 1.5);
  assert.strictEqual(normalize('""', "1.5"), 1.5);
  assert.strictEqual(normalize('"   "', "1.5"), 1.5);
  assert.strictEqual(normalize('"not-a-number"', "1.5"), 1.5);

  // Non-finite, negative and 0<ior<1 inputs default safely to 1.5.
  assert.strictEqual(normalize("NaN", "1.5"), 1.5);
  assert.strictEqual(normalize("Infinity", "1.5"), 1.5);
  assert.strictEqual(normalize("-Infinity", "1.5"), 1.5);
  assert.strictEqual(normalize("-1", "1.5"), 1.5);
  assert.strictEqual(normalize("-0.25", "1.5"), 1.5);
  assert.strictEqual(normalize("0.5", "1.5"), 1.5);
  assert.strictEqual(normalize("0.99", "1.5"), 1.5);

  // Explicit numeric zero is the glTF compatibility mode: preserved as-is,
  // never defaulted and never clamped to 1.
  assert.strictEqual(normalize("0", "1.5"), 0);

  // Finite ior >= 1 is valid without the legacy max=5 truncation.
  assert.strictEqual(normalize("1", "1.5"), 1);
  assert.strictEqual(normalize("1.33", "1.5"), 1.33);
  assert.strictEqual(normalize("1.5", "1.5"), 1.5);
  assert.strictEqual(normalize("2.42", "1.5"), 2.42);
  assert.strictEqual(normalize("6", "1.5"), 6);
  assert.strictEqual(normalize("42", "1.5"), 42);
  assert.strictEqual(normalize("Number.MAX_VALUE", "1.5"), Number.MAX_VALUE);

  // Numeric strings parse; CSS var strings ride the explicit-var machinery.
  assert.strictEqual(normalize('"1.33"', "1.5"), 1.33);
  assert.strictEqual(normalize('" var(--glass-ior) "', "1.5"), "var(--glass-ior)");

  // Invalid input falls back to the inherited value, including an inherited
  // explicit zero or CSS var.
  assert.strictEqual(normalize("0.5", "1.8"), 1.8);
  assert.strictEqual(normalize("undefined", "0"), 0);
  assert.strictEqual(normalize("undefined", '"var(--glass-ior)"'), "var(--glass-ior)");

  // The inherited fallback satisfies the same contract. sceneNumber would
  // coerce null, false and "" to 0 — silently enabling the glTF zero mode —
  // and pass negative or 0<ior<1 numbers straight through.
  assert.strictEqual(normalize("0.5", "null"), 1.5);
  assert.strictEqual(normalize("0.5", "false"), 1.5);
  assert.strictEqual(normalize("0.5", '""'), 1.5);
  assert.strictEqual(normalize("undefined", "-1"), 1.5);
  assert.strictEqual(normalize("undefined", "0.5"), 1.5);
  assert.strictEqual(normalize("undefined", "NaN"), 1.5);
  assert.strictEqual(normalize("undefined", "Infinity"), 1.5);
  // A valid inherited zero still carries through as the zero mode.
  assert.strictEqual(normalize("0.5", "0"), 0);
});

// --- normalization carry-through ---------------------------------------------

test("normalizeSceneObject carries authored ior through material normalization", () => {
  const context = createSceneCoreContext();

  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube" }, 0, null).ior'), 1.5);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", ior: 2.42 }, 0, null).ior'), 2.42);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", ior: 6 }, 0, null).ior'), 6);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", ior: 0 }, 0, null).ior'), 0);

  // Out-of-range or missing values default instead of passing through.
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", ior: 0.5 }, 0, null).ior'), 1.5);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", ior: -1 }, 0, null).ior'), 1.5);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", ior: null }, 0, null).ior'), 1.5);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", ior: "" }, 0, null).ior'), 1.5);

  // CSS var authoring survives normalization untouched.
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube", ior: " var(--glass-ior) " }, 0, null).ior'),
    "var(--glass-ior)");

  // Inline material objects are read through the shared material accessors.
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube", material: { ior: 1.33 } }, 0, null).ior'), 1.33);
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube", material: { ior: 0 } }, 0, null).ior'), 0);

  // Updates inherit the previous normalized object's ior when omitted.
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube" }, 0, { ior: 2.42 }).ior'), 2.42);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube" }, 0, { ior: 0 }).ior'), 0);
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube" }, 0, { ior: "var(--glass-ior)" }).ior'),
    "var(--glass-ior)");
});

test("named material resolution and record updates preserve ior", () => {
  const context = createSceneCoreContext();

  // sceneApplyNamedMaterialToObject copies the material's ior onto the
  // object (zero included) and keeps the object's value when unset.
  assert.strictEqual(callIn(context, "sceneApplyNamedMaterialToObject({ ior: 1.5 }, { ior: 1.33 }).ior"), 1.33);
  assert.strictEqual(callIn(context, "sceneApplyNamedMaterialToObject({ ior: 1.5 }, { ior: 0 }).ior"), 0);
  assert.strictEqual(callIn(context, "sceneApplyNamedMaterialToObject({ ior: 2.42 }, {}).ior"), 2.42);

  // The resolved object normalizes with the named material's ior.
  assert.strictEqual(
    callIn(context,
      'normalizeSceneObject(sceneApplyNamedMaterialToObject({ kind: "cube" }, { ior: 2.42 }), 0, null).ior'),
    2.42);

  // Named material records carry ior, and re-normalizing an update that
  // omits the field inherits it from the previous record.
  assert.strictEqual(
    callIn(context, 'normalizeSceneMaterialRecord({ name: "glass", ior: 1.33 }, 0, null).ior'), 1.33);
  assert.strictEqual(
    callIn(context,
      'normalizeSceneMaterialRecord({ name: "glass" }, 0, normalizeSceneMaterialRecord({ name: "glass", ior: 2.42 }, 0, null)).ior'),
    2.42);
});

test("normalizeSceneModel stores glTF model ior overrides on materialOverride", () => {
  const context = createSceneCoreContext();

  // Confirmed storage contract: authored ior lands on materialOverride.ior
  // at normalizeSceneModel's return, normalized (CSS var text trimmed).
  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", ior: 2.42 }, 0).materialOverride.ior'), 2.42);
  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", ior: 0 }, 0).materialOverride.ior'), 0);
  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", ior: " var(--glass-ior) " }, 0).materialOverride.ior'),
    "var(--glass-ior)");
  // A model without any override carries no materialOverride bag at all.
  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb" }, 0).materialOverride'), null);
});

test("instanced GLB batching carries authored ior and preserves genuinely absent ior", () => {
  const context = createSceneCoreContext();

  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb", ior: 2.42 }, 0, null).ior'), 2.42);
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb", ior: 0 }, 0, null).ior'), 0);
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb", ior: " var(--glass-ior) " }, 0, null).ior'),
    "var(--glass-ior)");

  // Omitted ior inherits the previous batch entry's value.
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, { ior: 1.33 }).ior'), 1.33);
  // Inherited fallbacks normalize under the same contract instead of
  // coercing null to the glTF zero mode.
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, { ior: null }).ior'), 1.5);
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, { ior: 0.5 }).ior'), 1.5);
  // A genuinely omitted ior stays omitted at this boundary: no key, no
  // defaulted 1.5, so downstream override plumbing cannot erase an authored
  // glTF value.
  const absent = callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, null)');
  assert.strictEqual(absent.ior, undefined);
  assert.strictEqual(Object.prototype.hasOwnProperty.call(absent, "ior"), false);
});

test("mount model override plumbing applies ior to raw and inline material", () => {
  const mountSource = readRuntimeSource("mount-webgl.ts");
  const context = createSceneCoreContext();
  runFragment(context, sliceBetween(mountSource,
    "function sceneModelMaterialOverrideSource",
    "function sceneApplyModelLOD"), "mount-override-extract.js");

  const applied = callIn(context,
    'sceneApplyMaterialOverride({ material: { ior: 1.33, color: "#ffffff" } }, { ior: 2.42 })');
  assert.strictEqual(applied.ior, 2.42);
  assert.strictEqual(applied.material.ior, 2.42);
  assert.strictEqual(applied.material.color, "#ffffff");

  const zero = callIn(context, 'sceneApplyMaterialOverride({ material: { ior: 1.33 } }, { ior: 0 })');
  assert.strictEqual(zero.ior, 0);
  assert.strictEqual(zero.material.ior, 0);

  const untouched = callIn(context, 'sceneApplyMaterialOverride({ material: { ior: 1.33 } }, {})');
  assert.strictEqual(untouched.material.ior, 1.33);

  // A model that sets nothing but ior still registers as an override source.
  assert.strictEqual(callIn(context, "sceneModelMaterialOverrideSource({ ior: 1.33 }) !== null"), true);
  assert.strictEqual(callIn(context, "sceneModelMaterialOverrideSource({}) === null"), true);
});

test("instanced GLB -> override chain preserves the raw glTF authored ior", () => {
  const mountSource = readRuntimeSource("mount-webgl.ts");
  const context = createSceneCoreContext();
  runFragment(context, sliceBetween(mountSource,
    "function sceneModelMaterialOverrideSource",
    "function sceneApplyModelLOD"), "mount-override-extract.js");

  // The chain runs the real instanced-GLB batch normalization (one default
  // instance merged into the raw entry) into the real batch-to-models
  // conversion and the real override application against a raw glTF record
  // with material.ior 2.42.
  const chain = (entryLiteral, fallbackLiteral) => callIn(context,
    "(() => {" +
    "const batch = normalizeSceneInstancedGLBMeshEntry(Object.assign(" + entryLiteral + ", { instances: [{}] }), 0, " + fallbackLiteral + ");" +
    "const models = sceneInstancedGLBMeshToModels(batch, 0);" +
    "if (!models || !models[0]) throw new Error('expected one model from instanced batch');" +
    "return sceneApplyMaterialOverride({ material: { ior: 2.42 } }, models[0]);" +
    "})()");

  // An omitted batch ior leaves the glTF asset's authored 2.42 untouched.
  assert.strictEqual(chain('{ src: "model.glb" }', "null").material.ior, 2.42);
  // An authored batch ior overrides the glTF value, glTF zero mode included.
  assert.strictEqual(chain('{ src: "model.glb", ior: 0 }', "null").material.ior, 0);
  assert.strictEqual(chain('{ src: "model.glb", ior: 1.33 }', "null").material.ior, 1.33);
  // An inherited batch ior is preserved through the chain.
  assert.strictEqual(chain('{ src: "model.glb" }', '{ ior: 1.33 }').material.ior, 1.33);
});

// --- cached keys and signatures ----------------------------------------------

test("material profile keys differentiate ior and keep the default compatible", () => {
  const context = createSceneCoreContext();
  const keyOf = (expr) => callIn(context, "sceneObjectMaterialProfile(" + expr + ").key");

  const base = keyOf('{ materialKind: "standard" }');
  // Omitted ior and explicit 1.5 collapse into one profile, preserving the
  // pre-IOR default (F0 0.04) compatibility.
  assert.strictEqual(keyOf('{ materialKind: "standard", ior: 1.5 }'), base);
  // A material differing only by ior never shares a cached profile.
  assert.notStrictEqual(keyOf('{ materialKind: "standard", ior: 1.33 }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", ior: 2.42 }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", ior: 0 }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", ior: 6 }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", ior: "var(--glass-ior)" }'), base);
  // Identical ior still dedupes to a single key, and CSS-var authoring keys
  // on the trimmed variable text.
  assert.strictEqual(keyOf('{ materialKind: "standard", ior: 2.42 }'), keyOf('{ materialKind: "standard", ior: 2.42 }'));
  assert.strictEqual(
    keyOf('{ materialKind: "standard", ior: " var(--glass-ior) " }'),
    keyOf('{ materialKind: "standard", ior: "var(--glass-ior)" }'));
  // Full precision: distinct high valid iors never quantize together.
  assert.notStrictEqual(
    keyOf('{ materialKind: "standard", ior: 42 }'),
    keyOf('{ materialKind: "standard", ior: 42.000001 }'));
});

test("planner material signatures hash the authored ior", () => {
  const context = createSceneCoreContext();

  // Production signature: scenePlannerHashMaterial(hash, material). The
  // planner is loaded whole, so the real hash chain runs directly.
  const signatureOf = (iorLiteral) => callIn(context,
    'scenePlannerHashMaterial(0, { key: "k", kind: "standard", ior: ' + iorLiteral + " })");
  const base = signatureOf("1.5");
  assert.notStrictEqual(signatureOf("1.33"), base);
  assert.notStrictEqual(signatureOf("2.42"), base);
  assert.notStrictEqual(signatureOf("0"), base);
  assert.notStrictEqual(signatureOf("6"), base);
  // Omitted and explicit 1.5 invalidate together.
  assert.strictEqual(signatureOf("1.5"), base);
  assert.strictEqual(signatureOf("undefined"), base);
});

test("sceneMaterialProfileKey treats raw invalid ior as the 1.5 default", () => {
  const context = createSceneCoreContext();
  const keyOf = (expr) => callIn(context, "sceneMaterialProfileKey({ ior: " + expr + " })");

  const base = keyOf("1.5");
  // Raw invalid values (undefined, null, false, empty) all land on the 1.5
  // shader default instead of colliding with an explicit 0 (which yields
  // F0 = 1).
  assert.strictEqual(keyOf("undefined"), base);
  assert.strictEqual(keyOf("null"), base);
  assert.strictEqual(keyOf("false"), base);
  assert.strictEqual(keyOf('""'), base);
  // Non-physical raw numbers (negative, sub-1, NaN, Infinity) also normalize
  // onto the 1.5 shader default.
  assert.strictEqual(keyOf("-0.5"), base);
  assert.strictEqual(keyOf("0.9"), base);
  assert.strictEqual(keyOf("NaN"), base);
  assert.strictEqual(keyOf("Infinity"), base);
  // An actual explicit 0 stays distinct from the default: explicit ior 0
  // yields F0 = 1, never the default's F0.
  assert.notStrictEqual(keyOf("0"), base);
  // CSS-var authoring keys on the trimmed variable text.
  assert.notStrictEqual(keyOf('"var(--glass-ior)"'), base);
  // Full precision: close high values never quantize together.
  assert.notStrictEqual(keyOf("42"), keyOf("42.000001"));
});

test("planner CSS input signature invalidates when authored ior changes", () => {
  const context = createSceneCoreContext();

  const signature = (edits) => callIn(context, "sceneCSSInputSignature(" + JSON.stringify({
    environment: null,
    materials: [{ id: "glass", ior: 1.5 }],
    lights: [],
    objects: [{ id: "orb", ior: 1.5 }],
    meshObjects: [{ id: "panel", ior: 1.5 }],
    points: [],
    instancedMeshes: [{ id: "bolts", ior: 1.5 }],
    labels: [],
    sprites: [],
    computeParticles: null,
    waterSystems: null,
    ...edits,
  }) + ")");

  const base = signature({});
  // The signature is deterministic, detects non-ior rewrites, and every
  // ior-bearing collection the planner resolves participates: named
  // materials, objects, model overlays (meshObjects) and instanced meshes.
  assert.strictEqual(signature({}), base);
  assert.notStrictEqual(signature({ materials: [{ id: "glass", opacity: 0.5 }] }), base);
  assert.notStrictEqual(signature({ materials: [{ id: "glass", ior: 2.42 }] }), base);
  assert.notStrictEqual(signature({ materials: [{ id: "glass", ior: 0 }] }), base);
  assert.notStrictEqual(signature({ objects: [{ id: "orb", ior: 2.42 }] }), base);
  assert.notStrictEqual(signature({ meshObjects: [{ id: "panel", ior: 2.42 }] }), base);
  assert.notStrictEqual(signature({ instancedMeshes: [{ id: "bolts", ior: 2.42 }] }), base);
});

// --- WebGL PBR ----------------------------------------------------------------

test("WebGL PBR shaders upload and consume the effective specular factors", () => {
  const source = readSceneRendererBackendSrc("webgl");

  // The dead u_dielectricF0 uniform is fully removed: declaration, cache
  // slot and upload. Shading reads only the effective uniforms.
  assert.doesNotMatch(source, /uniform float u_dielectricF0;/);
  assert.match(source, /vec3 specF0 = u_specularF0;/);
  assert.match(source, /vec3 F0 = mix\(specF0, albedo, metalness\);/);
  // The specular-intensity map multiplies both shared dielectric factors by
  // its alpha channel before the metallic mix; colour channels are never read.
  assert.match(source, /specF0 \*= specTex;/);
  assert.match(source, /specF90 \*= specTex;/);
  assert.match(source, /texture\(u_specularIntensityMap, v_uv\)\.a/);
  assert.doesNotMatch(source, /vec3 F0 = mix\(vec3\(0\.04\)/);
  assert.doesNotMatch(source, /dielectricF0:\s*gl\.getUniformLocation\(program,\s*"u_dielectricF0"\),/);
  assert.doesNotMatch(source, /gl\.uniform1f\(uniforms\.dielectricF0,/);
  // The live effective specular uniforms are cached and uploaded.
  assert.match(source, /specularF0: gl\.getUniformLocation\(program, "u_specularF0"\),/);
  assert.match(source, /specularF90: gl\.getUniformLocation\(program, "u_specularF90"\),/);
  assert.match(source, /gl\.uniform3f\(uniforms\.specularF0, specularFactors\.f0\[0\], specularFactors\.f0\[1\], specularFactors\.f0\[2\]\);/);
  assert.match(source, /gl\.uniform1f\(uniforms\.specularF90, specularFactors\.f90\);/);

  const context = createSceneCoreContext();
  runFragment(context, [
    sliceBetween(readBootstrapSource("15a1-scene-texture-budget.ts"),
      "var SCENE_TEXTURE_UNIT_MATERIALS", "var SCENE_TEXTURE_UNIT_FIRST_SHARED"),
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
    // Uniform slot objects remember their own names so the recorder can key
    // uploaded values; customUniforms stays null so that path early-returns.
    "function uniformSlots() {" +
      "const slots = { customUniforms: null };" +
      "for (const name of ['albedo', 'roughness', 'metalness', 'clearcoat', 'sheen'," +
        "'transmission', 'iridescence', 'anisotropy', 'specularF0', 'specularF90'," +
        "'emissive', 'opacity'," +
        "'unlit', 'hasAlbedoMap', 'hasNormalMap', 'hasRoughnessMap', 'hasMetalnessMap'," +
        "'hasEmissiveMap', 'hasOcclusionMap']) {" +
        "slots[name] = { name };" +
      "}" +
      "return slots;" +
    "}",
    // Narrow stubs for behavior outside this upload contract: no HDRI and no
    // texture ever loads, so every map reports unloaded.
    "function scenePBRHDRIBLAvailable() { return false; }",
    "function scenePBRLoadTexture() { return null; }",
    "function scenePBRBindTexture() {}",
  ].join("\n"), "webgl-upload-extract.js");

  const uploadWithIor = (iorLiteral) => callIn(context,
    "(() => { const gl = recordingGL(); const uniforms = uniformSlots();" +
    "uploadMaterial(gl, uniforms, { color: '#ffffff', ior: " + iorLiteral + " }, null);" +
    "return { f0: gl.floats.get('specularF0'), f90: gl.floats.get('specularF90') }; })()");
  const uploadedF0 = (iorLiteral) => uploadWithIor(iorLiteral).f0;
  const uploadedF90 = (iorLiteral) => uploadWithIor(iorLiteral).f90;

  // The production numeric helper implements the contract end to end.
  const f0 = (expression) => callIn(context, "scenePBRDielectricF0(" + expression + ")");
  const defaultF0 = ((1.5 - 1) / (1.5 + 1)) ** 2;
  const close = (actual, expected) => Math.abs(actual - expected) <= 1e-9;

  assert.ok(close(f0("1"), 0));
  assert.ok(close(f0("1.33"), ((1.33 - 1) / (1.33 + 1)) ** 2));
  assert.ok(close(f0("1.5"), defaultF0));
  assert.ok(close(f0("2.42"), ((2.42 - 1) / (2.42 + 1)) ** 2));
  assert.ok(close(f0("42"), ((42 - 1) / (42 + 1)) ** 2));
  // glTF compatibility mode: ior 0 pins the dielectric Fresnel to 1.
  assert.ok(close(f0("0"), 1));
  // 0<ior<1, negative and non-finite inputs default to 1.5 (F0 0.04).
  assert.ok(close(f0("0.5"), defaultF0));
  assert.ok(close(f0("-1"), defaultF0));
  assert.ok(close(f0("NaN"), defaultF0));
  assert.ok(close(f0("Infinity"), defaultF0));
  assert.ok(close(f0("undefined"), defaultF0));
  // null/false/empty strings are never coerced into the zero mode.
  assert.ok(close(f0("null"), defaultF0));
  assert.ok(close(f0("false"), defaultF0));
  assert.ok(close(f0('""'), defaultF0));
  // Unresolved CSS var residue defaults instead of zero-mode.
  assert.ok(close(f0('"var(--glass-ior)"'), defaultF0));
  // Huge finite input stays stable and the result fits float32.
  assert.ok(close(f0("1e300"), 1));
  const extreme = f0("1e300");
  assert.ok(Number.isFinite(extreme) && extreme >= 0 && extreme <= 1);

  // And the production uploadMaterial pushes those values through the real
  // uniform call for every contract branch: authored default, glTF zero
  // mode, bounds, above the legacy max=5, and invalid inputs.
  const unit = (actual, expected) => Math.abs(actual - expected) <= 1e-6;
  // The upload is the live vec3 effective F0: every channel must match.
  const uploadedChannels = (actual, expected) => {
    assert.ok(Array.isArray(actual) && actual.length === 3, "specularF0 uploaded as a vec3");
    for (let c = 0; c < 3; c++) {
      assert.ok(unit(actual[c], expected), "f0[" + c + "] = " + actual[c] + ", want " + expected);
    }
  };
  uploadedChannels(uploadedF0("undefined"), defaultF0);
  uploadedChannels(uploadedF0("1.5"), defaultF0);
  uploadedChannels(uploadedF0("0"), 1);
  uploadedChannels(uploadedF0("1"), 0);
  uploadedChannels(uploadedF0("1.33"), ((1.33 - 1) / (1.33 + 1)) ** 2);
  uploadedChannels(uploadedF0("2.42"), ((2.42 - 1) / (2.42 + 1)) ** 2);
  uploadedChannels(uploadedF0("6"), ((6 - 1) / (6 + 1)) ** 2);
  uploadedChannels(uploadedF0("42"), ((42 - 1) / (42 + 1)) ** 2);
  uploadedChannels(uploadedF0("0.5"), defaultF0);
  uploadedChannels(uploadedF0("-1"), defaultF0);
  uploadedChannels(uploadedF0("NaN"), defaultF0);
  uploadedChannels(uploadedF0('"var(--glass-ior)"'), defaultF0);
  uploadedChannels(uploadedF0("1e300"), 1);
  // Every branch uploads F90 = 1 (no authored specularIntensity).
  for (const iorLiteral of ["undefined", "1.5", "0", "1", "1.33", "2.42", "6", "42", "0.5", "-1", "NaN", '"var(--glass-ior)"', "1e300"]) {
    assert.ok(unit(uploadedF90(iorLiteral), 1), "specularF90 for ior " + iorLiteral);
  }
  // Neighbouring scalars still ride the same upload call.
  const scalarFor = (iorLiteral, name) => callIn(context,
    "(() => { const gl = recordingGL(); const uniforms = uniformSlots();" +
    "uploadMaterial(gl, uniforms, { color: '#ffffff', ior: " + iorLiteral + " }, null);" +
    "return gl.floats.get('" + name + "'); })()");
  assert.strictEqual(scalarFor("2.42", "roughness"), 0.5);
  assert.strictEqual(scalarFor("2.42", "opacity"), 1);
});

// --- WebGPU PBR ---------------------------------------------------------------

test("WebGPU material uniform packing carries the effective specular factors without moving existing slots", () => {
  const source = readSceneRendererBackendSrc("webgpu");

  // The WebGPU struct keeps its legacy trailing dielectricF0 scalar at f40
  // for layout compatibility, then packs the live effective specular F0 as
  // an aligned vec3f (floats 44..46) and F90 (float 47), padding the struct
  // to 208 bytes. Struct line matching allows the production aligned
  // whitespace.
  const structStart = indexOfMatch(source, /"struct MaterialUniforms \{"/);
  const matrixLine = indexOfMatch(source, /"\s+modelMatrix: mat4x4f,"/);
  const signsLine = indexOfMatch(source, /"\s+modelScaleSigns: vec4f,"/);
  const f0Line = indexOfMatch(source, /"\s+dielectricF0: f32,"/);
  const structClose = source.indexOf('"};",', structStart);
  assert.ok(structStart >= 0, "MaterialUniforms struct found");
  assert.ok(matrixLine > structStart && signsLine > matrixLine, "model transform slots preserved");
  assert.ok(f0Line > signsLine && f0Line < structClose, "dielectricF0 appended inside the struct");
  const specF0Line = indexOfMatch(source, /"\s+specularF0: vec3f,"/);
  const specF90Line = indexOfMatch(source, /"\s+specularF90: f32,"/);
  assert.ok(specF0Line > f0Line && specF90Line > specF0Line && specF90Line < structClose,
    "effective specular factors appended after dielectricF0");
  const specIntensityFlagLine = indexOfMatch(source, /"\s+hasSpecularIntensityMap: u32,"/);
  assert.ok(specIntensityFlagLine > f0Line && specIntensityFlagLine < specF0Line,
    "hasSpecularIntensityMap declared after dielectricF0 and before specularF0");
  for (const flag of ["hasAlbedoMap", "hasNormalMap", "hasRoughnessMap", "hasMetalnessMap", "hasEmissiveMap", "receiveShadow", "hasOcclusionMap"]) {
    const flagLine = indexOfMatch(source, new RegExp('"\\s+' + flag + ': u32,"'));
    assert.ok(flagLine > structStart && flagLine < matrixLine, flag + " texture-flag slot preserved");
  }
  assert.match(source, /var\s+_materialUniformBuf\s*=\s*new ArrayBuffer\(208\);/);
  assert.doesNotMatch(source, /var\s+_materialUniformBuf\s*=\s*new ArrayBuffer\(176\);/);
  // Fragment shader consumes the effective specular factors; the fixed 0.04
  // default is gone.
  // Production legitimately declares a mutable var: the color-texture slice
  // updates the shared specF0 before it is mixed with albedo by metalness.
  assert.match(source, /var specF0 = material\.specularF0( \* specIntensity)?;/);
  assert.match(source, /var F0 = mix\(specF0, albedo, metalness\);/);
  assert.doesNotMatch(source, /let F0 = mix\(vec3f\(0\.04\)/);

  // Execute the production packing function against the production buffer
  // declarations (whitespace-tolerant) with the real shared numeric/color
  // helpers — no hand copies of the buffer views or the math.
  const bufferDecls = (source.match(/var\s+_materialUniform\w+\s*=\s*[^;\n]+;/g) || []).join("\n");
  assert.match(bufferDecls, /var\s+_materialUniformBuf\s*=\s*new ArrayBuffer\(208\);/);
  assert.match(bufferDecls, /var\s+_materialUniformF\s*=/);
  assert.match(bufferDecls, /var\s+_materialUniformU\s*=/);
  const context = createSceneCoreContext();
  runFragment(context, [
    bufferDecls,
    sliceBetween(source, "function sceneWebGPUSRGBChannelToLinear", "var WGSL_COMMON_CONSTANTS"),
    sliceBetween(source, "function sceneWebGPUDielectricF0", "function materialUniformData"),
    sliceBetween(source, "function materialUniformData", "function wgpuCachedBindGroup"),
  ].join("\n"), "webgpu-material-extract.js");

  // IOR-only materials have white colour and intensity 1, so the effective
  // F0 equals the IOR F0 in all three channels and F90 is 1.
  const effectiveF0At = (materialLiteral, expected) => {
    const packed = callIn(context,
      "materialUniformData(" + materialLiteral + ", false, null, null)");
    for (let c = 0; c < 3; c++) {
      assert.ok(Math.abs(packed.data[44 + c] - expected) <= 1e-6,
        "specularF0[" + c + "] = " + packed.data[44 + c] + ", want " + expected);
    }
    assert.ok(Math.abs(packed.data[47] - 1) <= 1e-6, "specularF90 = " + packed.data[47] + ", want 1");
  };
  effectiveF0At("{}", expectedDielectricF0(1.5));
  effectiveF0At("{ ior: 1.33 }", expectedDielectricF0(1.33));
  effectiveF0At("{ ior: 2.42 }", expectedDielectricF0(2.42));
  effectiveF0At("{ ior: 42 }", expectedDielectricF0(42));
  effectiveF0At("{ ior: 0 }", 1);
  effectiveF0At("{ ior: 0.5 }", expectedDielectricF0(1.5));
  effectiveF0At("{ ior: -1 }", expectedDielectricF0(1.5));
  effectiveF0At("{ ior: 1e300 }", 1);

  const packed = callIn(context,
    'materialUniformData({ color: "#ffffff", roughness: 0.25, metalness: 0.5, ior: 2.42 }, true, null, null)');
  assert.strictEqual(packed.data.length, 52);
  // PBR scalars keep their slots.
  assert.ok(Math.abs(packed.data[3] - 0.25) <= 1e-6);
  assert.ok(Math.abs(packed.data[4] - 0.5) <= 1e-6);
  // Texture flag slots, receiveShadow and the occlusion flag stay put and
  // remain caller-owned.
  for (let index = 13; index <= 17; index += 1) {
    assert.strictEqual(packed.u[index], 0);
  }
  assert.strictEqual(packed.u[18], 1);
  assert.strictEqual(packed.u[19], 0);
  // Model matrix (identity fallback) and scale signs keep their slots.
  assert.strictEqual(packed.data[20], 1);
  assert.strictEqual(packed.data[21], 0);
  assert.strictEqual(packed.data[24], 0);
  assert.strictEqual(packed.data[25], 1);
  assert.strictEqual(packed.data[30], 1);
  assert.strictEqual(packed.data[33], 0);
  assert.strictEqual(packed.data[35], 1);
  assert.strictEqual(packed.data[36], 1);
  assert.strictEqual(packed.data[37], 1);
  assert.strictEqual(packed.data[38], 1);
  assert.strictEqual(packed.data[39], 0);
  // Trailing scalar plus deterministic padding, then the effective specular
  // factors at the vec3f-aligned slots: F0 = min(IOR F0 * white, 1) *
  // intensity and F90 = intensity 1.
  assert.ok(Math.abs(packed.data[40] - expectedDielectricF0(2.42)) <= 1e-6);
  assert.strictEqual(packed.data[41], 0);
  assert.strictEqual(packed.data[42], 0);
  assert.strictEqual(packed.data[43], 0);
  assert.ok(Math.abs(packed.data[44] - expectedDielectricF0(2.42)) <= 1e-6);
  assert.ok(Math.abs(packed.data[45] - expectedDielectricF0(2.42)) <= 1e-6);
  assert.ok(Math.abs(packed.data[46] - expectedDielectricF0(2.42)) <= 1e-6);
  assert.ok(Math.abs(packed.data[47] - 1) <= 1e-6);
  // Specular-color log coefficients at 48..50 and the neutral flag at u32 51.
  for (let c = 0; c < 3; c++) assert.ok(Number.isFinite(packed.data[48 + c]));
  assert.strictEqual(packed.u[51], 0);
});
