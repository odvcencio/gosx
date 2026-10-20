"use strict";
// Scene3D material specular slice: numeric contracts for specularIntensity
// and specularColor (LINEAR RGB), normalization carry-through, profile cache
// key differentiation, named-material/model/instanced plumbing, planner
// signature invalidation, and explicit CSS var resolution.
//
// Mirrors the IOR harness: production source fragments execute inside a VM
// sandbox in production chunk order. No parser or normalizer is faked and no
// copied expected algorithm exists — every function under test is the
// production implementation.

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
  runFragment(context, readBootstrapSource("15b-scene-planner.ts"), "15b-scene-planner.ts");
  return context;
}

function callIn(context, expression) {
  return vm.runInContext(expression, context, { filename: "scene-specular-expression.js" });
}

// Arrays returned from the VM realm carry that realm's Array prototype, so
// every cross-realm array comparison goes through Array.from first.
function color(context, valueExpression, fallbackExpression) {
  return Array.from(callIn(context,
    "sceneNormalizeMaterialSpecularColor(" + valueExpression + ", " + fallbackExpression + ")"));
}

// --- numeric contract -------------------------------------------------------

test("sceneNormalizeMaterialSpecularIntensity enforces the specular numeric contract", () => {
  const context = createSceneCoreContext();
  const normalize = (valueExpression, fallbackExpression) =>
    callIn(context, "sceneNormalizeMaterialSpecularIntensity(" + valueExpression + ", " + fallbackExpression + ")");

  // Missing, null, booleans and empty strings default to 1; never coerced.
  assert.strictEqual(normalize("undefined", "1"), 1);
  assert.strictEqual(normalize("null", "1"), 1);
  assert.strictEqual(normalize("false", "1"), 1);
  assert.strictEqual(normalize('""', "1"), 1);
  assert.strictEqual(normalize('"   "', "1"), 1);
  assert.strictEqual(normalize('"not-a-number"', "1"), 1);

  // Non-finite and out-of-range inputs default to 1.
  assert.strictEqual(normalize("NaN", "1"), 1);
  assert.strictEqual(normalize("Infinity", "1"), 1);
  assert.strictEqual(normalize("-Infinity", "1"), 1);
  assert.strictEqual(normalize("-0.25", "1"), 1);
  assert.strictEqual(normalize("1.5", "1"), 1);
  assert.strictEqual(normalize("2", "1"), 1);

  // Explicit numeric 0 is valid and preserved. The full [0, 1] range is
  // accepted; numeric strings parse; CSS var strings come back trimmed.
  assert.strictEqual(normalize("0", "1"), 0);
  assert.strictEqual(normalize("0.25", "1"), 0.25);
  assert.strictEqual(normalize("1", "1"), 1);
  assert.strictEqual(normalize('"0.5"', "1"), 0.5);
  assert.strictEqual(normalize('" var(--spec-intensity) "', "1"), "var(--spec-intensity)");

  // Invalid input falls back to a valid inherited value, including an
  // inherited explicit zero or CSS var.
  assert.strictEqual(normalize("5", "0.25"), 0.25);
  assert.strictEqual(normalize("undefined", "0"), 0);
  assert.strictEqual(normalize("undefined", '"var(--spec-intensity)"'), "var(--spec-intensity)");
  assert.strictEqual(normalize("-1", "0"), 0);

  // The inherited fallback satisfies the same contract; the hard default
  // of 1 is always valid.
  assert.strictEqual(normalize("5", "null"), 1);
  assert.strictEqual(normalize("5", "false"), 1);
  assert.strictEqual(normalize("5", '""'), 1);
  assert.strictEqual(normalize("undefined", "NaN"), 1);
  assert.strictEqual(normalize("undefined", "-1"), 1);
  assert.strictEqual(normalize("undefined", "2"), 1);
});

test("sceneNormalizeMaterialSpecularColor enforces the specular color contract", () => {
  const context = createSceneCoreContext();

  // Missing, null, booleans, empty strings, junk strings, wrong shapes and
  // fully invalid triples default to LINEAR white [1, 1, 1].
  assert.deepStrictEqual(color(context, "undefined", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "null", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "false", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, '""', "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, '"junk"', "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "{}", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "[1, 2]", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "[NaN, NaN, NaN]", "null"), [1, 1, 1]);

  // Wrong arity and non-array shapes are invalid wholes: a four-component
  // array, a bare numeric string ("123" would index characters) and plain
  // array-like objects never reach per-component parsing.
  assert.deepStrictEqual(color(context, "[1, 2, 3, 4]", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, '"123"', "null"), [1, 1, 1]);
  assert.deepStrictEqual(
    color(context, "{ length: 3, 0: 0.25, 1: 0.5, 2: 0.75 }", "null"), [1, 1, 1]);

  // One bad component invalidates the whole triple: no per-channel repair.
  assert.deepStrictEqual(color(context, "[0.5, NaN, 1]", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "[0.5, -1, 1]", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "[0.5, null, 1]", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "[0.5, , 1]", "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "[0.5, NaN, 1]", "[0.25, 0.5, 0.75]"), [0.25, 0.5, 0.75]);

  // Explicit black is valid and preserved; HDR components above 1 are
  // allowed; numeric component strings parse under the scalar convention;
  // typed arrays are accepted.
  assert.deepStrictEqual(color(context, "[0, 0, 0]", "null"), [0, 0, 0]);
  assert.deepStrictEqual(color(context, "[0.5, 1, 2.5]", "null"), [0.5, 1, 2.5]);
  assert.deepStrictEqual(color(context, '["0.25", "1", "0"]', "null"), [0.25, 1, 0]);
  assert.deepStrictEqual(
    color(context, "new Float32Array([0.25, 0.5, 1])", "null"), [0.25, 0.5, 1]);

  // Numeric CSS-style triples are explicit LINEAR-space colors: whitespace
  // or comma separated, exactly three numeric tokens, no gamma conversion.
  // Single numbers and wrong token counts are rejected.
  assert.deepStrictEqual(color(context, '"0.5 0.5 0.5"', "null"), [0.5, 0.5, 0.5]);
  assert.deepStrictEqual(color(context, '"0.25,0.5, 0.75"', "null"), [0.25, 0.5, 0.75]);
  assert.deepStrictEqual(color(context, '"0.5 0.5"', "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, '"0.5 0.5 0.5 0.5"', "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, '"0.5 x 0.5"', "null"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, '"-0.5 0.5 0.5"', "null"), [1, 1, 1]);

  // CSS var strings ride the explicit-var machinery, trimmed.
  assert.strictEqual(
    callIn(context, 'sceneNormalizeMaterialSpecularColor(" var(--spec-color) ", null)'),
    "var(--spec-color)");

  // An invalid triple falls back to a valid inherited color as a whole:
  // a valid fallback triple or CSS triple text, never per-channel mixing.
  assert.deepStrictEqual(color(context, "[NaN, NaN, NaN]", "[0.25, 0.5, 0.75]"), [0.25, 0.5, 0.75]);
  assert.deepStrictEqual(color(context, "null", "[0.25, 0.5, 0.75]"), [0.25, 0.5, 0.75]);
  assert.deepStrictEqual(color(context, "[NaN, -1, 0.5]", "[0.25, 0.5, 0.75]"), [0.25, 0.5, 0.75]);
  assert.deepStrictEqual(color(context, "null", "[NaN, -1, 0.5]"), [1, 1, 1]);
  assert.deepStrictEqual(color(context, "null", '"0.25 0.5 0.75"'), [0.25, 0.5, 0.75]);
  assert.strictEqual(
    callIn(context, 'sceneNormalizeMaterialSpecularColor(null, " var(--spec-color) ")'),
    "var(--spec-color)");

  // Snapshot semantics: mutating the author input array can never mutate
  // the normalized value or a cached profile's stored tint.
  assert.strictEqual(callIn(context,
    '(() => { const input = [0.1, 0.2, 0.3]; const c = sceneNormalizeMaterialSpecularColor(input, null);' +
    ' input[0] = 9; input[1] = 9; input[2] = 9;' +
    ' return c[0] + "," + c[1] + "," + c[2] + "|" + (c !== input); })()'),
    "0.1,0.2,0.3|true");
  assert.strictEqual(callIn(context,
    '(() => { const input = [0.1, 0.2, 0.3]; const profile = sceneObjectMaterialProfile({ materialKind: "standard", specularColor: input });' +
    " input[2] = 9;" +
    ' return profile.specularColor[0] + "," + profile.specularColor[1] + "," + profile.specularColor[2]; })()'),
    "0.1,0.2,0.3");
});

// --- normalization carry-through ---------------------------------------------

test("normalizeSceneObject carries specular intensity and color through material normalization", () => {
  const context = createSceneCoreContext();

  // Defaults.
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube" }, 0, null).specularIntensity'), 1);
  assert.deepStrictEqual(Array.from(callIn(context, 'normalizeSceneObject({ kind: "cube" }, 0, null).specularColor')), [1, 1, 1]);

  // Authored values, explicit zero and black included.
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", specularIntensity: 0 }, 0, null).specularIntensity'), 0);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", specularIntensity: 0.25 }, 0, null).specularIntensity'), 0.25);
  assert.deepStrictEqual(Array.from(callIn(context, 'normalizeSceneObject({ kind: "cube", specularColor: [0, 0, 0] }, 0, null).specularColor')), [0, 0, 0]);
  assert.deepStrictEqual(Array.from(callIn(context, 'normalizeSceneObject({ kind: "cube", specularColor: [0.2, 0.4, 1.7] }, 0, null).specularColor')), [0.2, 0.4, 1.7]);

  // Invalid values default instead of passing through.
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", specularIntensity: 1.5 }, 0, null).specularIntensity'), 1);
  assert.strictEqual(callIn(context, 'normalizeSceneObject({ kind: "cube", specularIntensity: null }, 0, null).specularIntensity'), 1);
  assert.deepStrictEqual(Array.from(callIn(context, 'normalizeSceneObject({ kind: "cube", specularColor: "junk" }, 0, null).specularColor')), [1, 1, 1]);
  // Invalid wholes default instead of per-channel repair; numeric CSS
  // triple text is a valid LINEAR color.
  assert.deepStrictEqual(Array.from(callIn(context, 'normalizeSceneObject({ kind: "cube", specularColor: [0.5, -1, 1] }, 0, null).specularColor')), [1, 1, 1]);
  assert.deepStrictEqual(Array.from(callIn(context, 'normalizeSceneObject({ kind: "cube", specularColor: [1, 2, 3, 4] }, 0, null).specularColor')), [1, 1, 1]);
  assert.deepStrictEqual(Array.from(callIn(context, 'normalizeSceneObject({ kind: "cube", specularColor: "0.25 0.5 0.75" }, 0, null).specularColor')), [0.25, 0.5, 0.75]);

  // CSS var authoring survives normalization untouched.
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube", specularIntensity: " var(--si) " }, 0, null).specularIntensity'),
    "var(--si)");
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube", specularColor: " var(--sc) " }, 0, null).specularColor'),
    "var(--sc)");

  // Inline material objects are read through the shared material accessors.
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube", material: { specularIntensity: 0 } }, 0, null).specularIntensity'), 0);
  assert.deepStrictEqual(Array.from(callIn(context,
    'normalizeSceneObject({ kind: "cube", material: { specularColor: [0, 1, 0] } }, 0, null).specularColor')), [0, 1, 0]);

  // Partial updates inherit the previous normalized values when omitted,
  // including zero and black.
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube" }, 0, { specularIntensity: 0 }).specularIntensity'), 0);
  assert.deepStrictEqual(Array.from(callIn(context,
    'normalizeSceneObject({ kind: "cube" }, 0, { specularColor: [0, 0.5, 0] }).specularColor')), [0, 0.5, 0]);
  // An invalid authored value falls back to a valid inherited zero/black.
  assert.strictEqual(
    callIn(context, 'normalizeSceneObject({ kind: "cube", specularIntensity: 2 }, 0, { specularIntensity: 0 }).specularIntensity'), 0);
  assert.deepStrictEqual(Array.from(callIn(context,
    'normalizeSceneObject({ kind: "cube", specularColor: "junk" }, 0, { specularColor: [0, 0, 0] }).specularColor')), [0, 0, 0]);
});

test("normalizeSceneModel stores specular overrides and omission stays absent", () => {
  const context = createSceneCoreContext();

  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", specularIntensity: 0.5 }, 0).materialOverride.specularIntensity'), 0.5);
  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", specularIntensity: 0 }, 0).materialOverride.specularIntensity'), 0);
  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", specularIntensity: " var(--si) " }, 0).materialOverride.specularIntensity'),
    "var(--si)");
  assert.deepStrictEqual(
    Array.from(callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", specularColor: [0, 0, 0] }, 0).materialOverride.specularColor')),
    [0, 0, 0]);
  assert.deepStrictEqual(
    Array.from(callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", specularColor: [1, 0.5, 2] }, 0).materialOverride.specularColor')),
    [1, 0.5, 2]);
  // Explicit specular factors alone register an override bag.
  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb", specularIntensity: 0 }, 0).materialOverride !== null'), true);
  // A model without any override carries no materialOverride bag at all.
  assert.strictEqual(
    callIn(context, 'normalizeSceneModel({ id: "glb", src: "model.glb" }, 0).materialOverride'), null);
});

test("instanced GLB batching carries specular factors and preserves genuine omission", () => {
  const context = createSceneCoreContext();

  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb", specularIntensity: 0.5 }, 0, null).specularIntensity'), 0.5);
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb", specularIntensity: 0 }, 0, null).specularIntensity'), 0);
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb", specularIntensity: " var(--si) " }, 0, null).specularIntensity'),
    "var(--si)");
  assert.deepStrictEqual(
    Array.from(callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb", specularColor: [0, 0, 0] }, 0, null).specularColor')),
    [0, 0, 0]);

  // Omitted factors inherit the previous batch entry's values.
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, { specularIntensity: 0.25 }).specularIntensity'), 0.25);
  assert.deepStrictEqual(
    Array.from(callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, { specularColor: [0, 1, 0] }).specularColor')),
    [0, 1, 0]);
  // Inherited fallbacks normalize under the same contract instead of
  // coercing null into a wrong value.
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, { specularIntensity: null }).specularIntensity'), 1);
  assert.strictEqual(
    callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, { specularIntensity: 2 }).specularIntensity'), 1);
  assert.deepStrictEqual(
    Array.from(callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, { specularColor: null }).specularColor')),
    [1, 1, 1]);

  // Genuinely omitted factors stay omitted at this boundary: no key and no
  // defaulted value, so downstream override plumbing cannot erase an
  // asset-authored specular factor.
  const absent = callIn(context, 'normalizeSceneInstancedGLBMeshEntry({ src: "model.glb" }, 0, null)');
  assert.strictEqual(absent.specularIntensity, undefined);
  assert.strictEqual(Object.prototype.hasOwnProperty.call(absent, "specularIntensity"), false);
  assert.strictEqual(absent.specularColor, undefined);
  assert.strictEqual(Object.prototype.hasOwnProperty.call(absent, "specularColor"), false);
});

test("mount model override plumbing applies specular factors to raw and inline material", () => {
  const mountSource = readRuntimeSource("mount-webgl.ts");
  const context = createSceneCoreContext();
  runFragment(context, sliceBetween(mountSource,
    "function sceneModelMaterialOverrideSource",
    "function sceneApplyModelLOD"), "mount-override-extract.js");

  const applied = callIn(context,
    'sceneApplyMaterialOverride({ material: { specularIntensity: 1, specularColor: [1, 1, 1], color: "#ffffff" } }, { specularIntensity: 0, specularColor: [0, 0, 0] })');
  assert.strictEqual(applied.specularIntensity, 0);
  assert.deepStrictEqual(Array.from(applied.specularColor), [0, 0, 0]);
  assert.strictEqual(applied.material.specularIntensity, 0);
  assert.deepStrictEqual(Array.from(applied.material.specularColor), [0, 0, 0]);
  assert.strictEqual(applied.material.color, "#ffffff");

  const untouched = callIn(context,
    'sceneApplyMaterialOverride({ material: { specularIntensity: 0.75, specularColor: [0.9, 0.8, 0.7] } }, {})');
  assert.strictEqual(untouched.material.specularIntensity, 0.75);
  assert.deepStrictEqual(Array.from(untouched.material.specularColor), [0.9, 0.8, 0.7]);

  // The override RGB is snapshotted per target: mutating the author array
  // afterwards, or the direct override color, cannot alias through to the
  // inline material copy (or vice versa).
  const aliasing = callIn(context,
    '(() => { const overrideRGB = [0.1, 0.2, 0.3];' +
    ' const applied = sceneApplyMaterialOverride(' +
    '   { specularColor: [1, 1, 1], material: { specularColor: [1, 1, 1], color: "#ffffff" } },' +
    '   { specularColor: overrideRGB });' +
    ' overrideRGB[0] = 9;' +
    ' applied.specularColor[1] = 9;' +
    ' return applied.specularColor.join(",") + "|" + applied.material.specularColor.join(",") + "|" +' +
    '   (applied.specularColor !== applied.material.specularColor) + "|" + applied.material.color; })()');
  assert.strictEqual(aliasing, "0.1,9,0.3|0.1,0.2,0.3|true|#ffffff");

  // Typed-array override RGB gets the same per-target snapshot treatment:
  // mutating the author Float32Array, or one copied target, cannot alias
  // through to the other copy.
  const typedAliasing = callIn(context,
    '(() => { const overrideRGB = new Float32Array([0.25, 0.5, 0.75]);' +
    ' const applied = sceneApplyMaterialOverride(' +
    '   { specularColor: new Float32Array([1, 1, 1]), material: { specularColor: new Float32Array([1, 1, 1]), color: "#ffffff" } },' +
    '   { specularColor: overrideRGB });' +
    ' overrideRGB[0] = 9;' +
    ' applied.specularColor[1] = 9;' +
    ' return applied.specularColor.join(",") + "|" + applied.material.specularColor.join(",") + "|" +' +
    '   (applied.specularColor !== applied.material.specularColor) + "|" + applied.material.color; })()');
  assert.strictEqual(typedAliasing, "0.25,9,0.75|0.25,0.5,0.75|true|#ffffff");

  // A model that sets nothing but specular factors still registers as an
  // override source.
  assert.strictEqual(callIn(context, "sceneModelMaterialOverrideSource({ specularIntensity: 0 }) !== null"), true);
  assert.strictEqual(callIn(context, "sceneModelMaterialOverrideSource({ specularColor: [0, 0, 0] }) !== null"), true);
  assert.strictEqual(callIn(context, "sceneModelMaterialOverrideSource({}) === null"), true);
});

test("instanced GLB -> override chain preserves asset-authored specular factors", () => {
  const mountSource = readRuntimeSource("mount-webgl.ts");
  const context = createSceneCoreContext();
  runFragment(context, sliceBetween(mountSource,
    "function sceneModelMaterialOverrideSource",
    "function sceneApplyModelLOD"), "mount-override-extract.js");

  // The chain runs the real instanced-GLB batch normalization, the real
  // batch-to-models conversion and the real override application against a
  // raw glTF record with authored specular factors.
  const chain = (entryLiteral, fallbackLiteral) => callIn(context,
    "(() => {" +
    "const batch = normalizeSceneInstancedGLBMeshEntry(Object.assign(" + entryLiteral + ", { instances: [{}] }), 0, " + fallbackLiteral + ");" +
    "const models = sceneInstancedGLBMeshToModels(batch, 0);" +
    "if (!models || !models[0]) throw new Error('expected one model from instanced batch');" +
    "return sceneApplyMaterialOverride({ material: { specularIntensity: 0.75, specularColor: [0.9, 0.8, 0.7] } }, models[0]);" +
    "})()");

  // An omitted batch factor leaves the glTF asset's authored values untouched.
  const omitted = chain('{ src: "model.glb" }', "null");
  assert.strictEqual(omitted.material.specularIntensity, 0.75);
  assert.deepStrictEqual(Array.from(omitted.material.specularColor), [0.9, 0.8, 0.7]);
  // Authored batch factors override the glTF values, zero/black included.
  assert.strictEqual(chain('{ src: "model.glb", specularIntensity: 0 }', "null").material.specularIntensity, 0);
  assert.deepStrictEqual(
    Array.from(chain('{ src: "model.glb", specularColor: [0, 0, 0] }', "null").material.specularColor),
    [0, 0, 0]);
  assert.strictEqual(chain('{ src: "model.glb", specularIntensity: 0.25 }', "null").material.specularIntensity, 0.25);
  // Inherited batch factors are preserved through the chain.
  assert.strictEqual(chain('{ src: "model.glb" }', '{ specularIntensity: 0.25 }').material.specularIntensity, 0.25);
});

test("named material resolution, variants and partial updates preserve specular", () => {
  const context = createSceneCoreContext();

  // sceneApplyNamedMaterialToObject copies the material's specular factors
  // onto the object (zero/black included) and keeps the object's values
  // when unset.
  assert.strictEqual(callIn(context, "sceneApplyNamedMaterialToObject({ specularIntensity: 1 }, { specularIntensity: 0.25 }).specularIntensity"), 0.25);
  assert.strictEqual(callIn(context, "sceneApplyNamedMaterialToObject({ specularIntensity: 1 }, { specularIntensity: 0 }).specularIntensity"), 0);
  assert.deepStrictEqual(
    Array.from(callIn(context, "sceneApplyNamedMaterialToObject({ specularColor: [1, 1, 1] }, { specularColor: [0, 0, 0] }).specularColor")),
    [0, 0, 0]);
  assert.strictEqual(callIn(context, "sceneApplyNamedMaterialToObject({ specularIntensity: 0.5 }, {}).specularIntensity"), 0.5);

  // The resolved object normalizes with the named material's factors.
  assert.strictEqual(
    callIn(context,
      'normalizeSceneObject(sceneApplyNamedMaterialToObject({ kind: "cube" }, { specularIntensity: 0, specularColor: [0, 0, 0] }), 0, null).specularIntensity'),
    0);
  assert.deepStrictEqual(
    Array.from(callIn(context,
      'normalizeSceneObject(sceneApplyNamedMaterialToObject({ kind: "cube" }, { specularIntensity: 0, specularColor: [0, 0, 0] }), 0, null).specularColor')),
    [0, 0, 0]);

  // Named material records carry specular factors, and re-normalizing an
  // update that omits the fields inherits them (zero/black included).
  const recordExpression = 'normalizeSceneMaterialRecord({ name: "tint", specularIntensity: 0.25, specularColor: [0.1, 0.2, 0.3] }, 0, null)';
  assert.strictEqual(callIn(context, recordExpression + ".specularIntensity"), 0.25);
  assert.deepStrictEqual(Array.from(callIn(context, recordExpression + ".specularColor")), [0.1, 0.2, 0.3]);
  const inherited = (fallbackExpression) => callIn(context,
    "normalizeSceneMaterialRecord({ name: \"tint\" }, 0, " + fallbackExpression + ")");
  assert.strictEqual(inherited(recordExpression).specularIntensity, 0.25);
  assert.deepStrictEqual(Array.from(inherited(recordExpression).specularColor), [0.1, 0.2, 0.3]);
  const zeroRecord = 'normalizeSceneMaterialRecord({ name: "flat", specularIntensity: 0, specularColor: [0, 0, 0] }, 0, null)';
  assert.strictEqual(callIn(context, 'normalizeSceneMaterialRecord({ name: "flat" }, 0, ' + zeroRecord + ").specularIntensity"), 0);
  assert.deepStrictEqual(
    Array.from(callIn(context, 'normalizeSceneMaterialRecord({ name: "flat" }, 0, ' + zeroRecord + ").specularColor")),
    [0, 0, 0]);

  // Variant tagging still propagates alongside specular factors.
  assert.strictEqual(
    callIn(context, 'normalizeSceneMaterialRecord({ name: "t", _variantKey: "v2", specularIntensity: 0.5 }, 0, null).variantKey'),
    "v2");
});

// --- cached keys and signatures ----------------------------------------------

test("material profile keys differentiate specular intensity and each color component", () => {
  const context = createSceneCoreContext();
  const keyOf = (expr) => callIn(context, "sceneObjectMaterialProfile(" + expr + ").key");

  const base = keyOf('{ materialKind: "standard" }');
  // Omitted factors and explicit defaults collapse into one profile.
  assert.strictEqual(keyOf('{ materialKind: "standard", specularIntensity: 1, specularColor: [1, 1, 1] }'), base);
  // A material differing only by a specular factor never shares a cached
  // profile — each RGB component and the intensity individually matter.
  assert.notStrictEqual(keyOf('{ materialKind: "standard", specularIntensity: 0.5 }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", specularIntensity: 0 }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", specularColor: [0, 1, 1] }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", specularColor: [1, 0, 1] }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", specularColor: [1, 1, 0] }'), base);
  assert.notStrictEqual(keyOf('{ materialKind: "standard", specularColor: [0, 0, 0] }'), base);
  assert.notStrictEqual(
    keyOf('{ materialKind: "standard", specularColor: [0.25, 0.5, 0.75] }'),
    keyOf('{ materialKind: "standard", specularColor: [0.26, 0.5, 0.75] }'));
  assert.notStrictEqual(
    keyOf('{ materialKind: "standard", specularColor: [0.25, 0.5, 0.75] }'),
    keyOf('{ materialKind: "standard", specularColor: [0.25, 0.5, 0.76] }'));
  // Full precision: close intensities and close color components never
  // quantize together.
  assert.notStrictEqual(
    keyOf('{ materialKind: "standard", specularIntensity: 0.5 }'),
    keyOf('{ materialKind: "standard", specularIntensity: 0.500001 }'));
  assert.notStrictEqual(
    keyOf('{ materialKind: "standard", specularColor: [1, 1, 1] }'),
    keyOf('{ materialKind: "standard", specularColor: [1, 1, 1.0000001] }'));
  // Identical values dedupe; CSS-var authoring keys on the trimmed text.
  assert.strictEqual(
    keyOf('{ materialKind: "standard", specularIntensity: 0.25 }'),
    keyOf('{ materialKind: "standard", specularIntensity: 0.25 }'));
  assert.strictEqual(
    keyOf('{ materialKind: "standard", specularIntensity: " var(--si) " }'),
    keyOf('{ materialKind: "standard", specularIntensity: "var(--si)" }'));
  assert.strictEqual(
    keyOf('{ materialKind: "standard", specularColor: " var(--sc) " }'),
    keyOf('{ materialKind: "standard", specularColor: "var(--sc)" }'));
  // Raw invalid values normalize onto the default key instead of colliding
  // with explicit zero/black.
  assert.strictEqual(keyOf('{ materialKind: "standard", specularIntensity: 5 }'), base);
  assert.strictEqual(keyOf('{ materialKind: "standard", specularIntensity: null }'), base);
  assert.strictEqual(keyOf('{ materialKind: "standard", specularColor: "junk" }'), base);
  assert.strictEqual(keyOf('{ materialKind: "standard", specularColor: [1, 2] }'), base);
  assert.strictEqual(keyOf('{ materialKind: "standard", specularColor: [1, 2, 3, 4] }'), base);
  assert.strictEqual(keyOf('{ materialKind: "standard", specularColor: [0.5, NaN, 1] }'), base);
  assert.strictEqual(keyOf('{ materialKind: "standard", specularColor: "123" }'), base);
  // Numeric CSS triple text consumes identically to the explicit array.
  assert.strictEqual(
    keyOf('{ materialKind: "standard", specularColor: "0.25 0.5 0.75" }'),
    keyOf('{ materialKind: "standard", specularColor: [0.25, 0.5, 0.75] }'));
});

test("planner material signatures hash specular intensity and color", () => {
  const context = createSceneCoreContext();

  // Production signature: scenePlannerHashMaterial(hash, material). The
  // planner is loaded whole, so the real hash chain runs directly.
  const signatureOf = (suffix) => callIn(context,
    'scenePlannerHashMaterial(0, { key: "k", kind: "standard"' + suffix + " })");
  const base = signatureOf("");
  // Omitted and explicit defaults invalidate together.
  assert.strictEqual(signatureOf(", specularIntensity: 1, specularColor: [1, 1, 1]"), base);
  // Intensity and each RGB component individually invalidate.
  assert.notStrictEqual(signatureOf(", specularIntensity: 0.5"), base);
  assert.notStrictEqual(signatureOf(", specularIntensity: 0"), base);
  assert.notStrictEqual(signatureOf(", specularColor: [0, 1, 1]"), base);
  assert.notStrictEqual(signatureOf(", specularColor: [1, 0, 1]"), base);
  assert.notStrictEqual(signatureOf(", specularColor: [1, 1, 0]"), base);
  assert.notStrictEqual(signatureOf(", specularColor: [0, 0, 0]"), base);
  assert.notStrictEqual(signatureOf(", specularColor: [0.25, 0.5, 0.75]"), base);
  // Full precision: the *1000 hash quantization must not collapse close
  // factors, even when the material's stable key is fixed.
  assert.notStrictEqual(signatureOf(", specularIntensity: 0.5"), signatureOf(", specularIntensity: 0.500001"));
  assert.notStrictEqual(signatureOf(", specularColor: [1, 1, 1]"), signatureOf(", specularColor: [1, 1, 1.0000001]"));
});

test("planner CSS input signature invalidates when authored specular factors change", () => {
  const context = createSceneCoreContext();

  const signature = (edits) => callIn(context, "sceneCSSInputSignature(" + JSON.stringify({
    environment: null,
    materials: [{ id: "m", specularIntensity: 1, specularColor: [1, 1, 1] }],
    lights: [],
    objects: [{ id: "o", specularIntensity: 1, specularColor: [1, 1, 1] }],
    meshObjects: [{ id: "p", specularIntensity: 1, specularColor: [1, 1, 1] }],
    points: [],
    instancedMeshes: [{ id: "b", specularIntensity: 1, specularColor: [1, 1, 1] }],
    labels: [],
    sprites: [],
    computeParticles: null,
    waterSystems: null,
    ...edits,
  }) + ")");

  const base = signature({});
  // The signature is deterministic and detects non-specular rewrites.
  assert.strictEqual(signature({}), base);
  assert.notStrictEqual(signature({ materials: [{ id: "m", opacity: 0.5 }] }), base);
  // Every specular-bearing collection the planner resolves participates:
  // named materials, objects, model overlays (meshObjects), instanced
  // meshes — intensity and each RGB component — and CSS var strings.
  assert.notStrictEqual(signature({ materials: [{ id: "m", specularIntensity: 0.5 }] }), base);
  assert.notStrictEqual(signature({ materials: [{ id: "m", specularIntensity: 0 }] }), base);
  assert.notStrictEqual(signature({ materials: [{ id: "m", specularIntensity: "var(--si)" }] }), base);
  assert.notStrictEqual(signature({ materials: [{ id: "m", specularColor: [0, 1, 1] }] }), base);
  assert.notStrictEqual(signature({ materials: [{ id: "m", specularColor: [0, 0, 0] }] }), base);
  assert.notStrictEqual(signature({ objects: [{ id: "o", specularIntensity: 0 }] }), base);
  assert.notStrictEqual(signature({ objects: [{ id: "o", specularColor: [1, 0, 1] }] }), base);
  assert.notStrictEqual(signature({ meshObjects: [{ id: "p", specularIntensity: 0.25 }] }), base);
  assert.notStrictEqual(signature({ meshObjects: [{ id: "p", specularColor: [1, 1, 0] }] }), base);
  assert.notStrictEqual(signature({ instancedMeshes: [{ id: "b", specularIntensity: 0.5 }] }), base);
  assert.notStrictEqual(signature({ instancedMeshes: [{ id: "b", specularColor: [0.1, 0.2, 0.3] }] }), base);
  // Full precision invalidation across every specular-bearing collection.
  assert.notStrictEqual(
    signature({ materials: [{ id: "m", specularIntensity: 0.5 }] }),
    signature({ materials: [{ id: "m", specularIntensity: 0.500001 }] }));
  assert.notStrictEqual(
    signature({ objects: [{ id: "o", specularColor: [1, 1, 1] }] }),
    signature({ objects: [{ id: "o", specularColor: [1, 1, 1.0000001] }] }));
  assert.notStrictEqual(
    signature({ meshObjects: [{ id: "p", specularColor: [1, 1, 1] }] }),
    signature({ meshObjects: [{ id: "p", specularColor: [1, 1, 1.0000001] }] }));
  assert.notStrictEqual(
    signature({ instancedMeshes: [{ id: "b", specularIntensity: 1 }] }),
    signature({ instancedMeshes: [{ id: "b", specularIntensity: 1.0000001 }] }));
});

test("explicit CSS var resolution resolves specular factors across all collections", () => {
  const context = createSceneCoreContext();

  // Real production resolution path: sceneCSSResolveExplicitVars over a
  // planner state whose source is a valid render bundle (production states
  // always carry the IR as source), with no computed style available so
  // var fallbacks apply. All four specular-bearing collections resolve.
  const result = callIn(context,
    "(() => {" +
    "const bundle = {" +
    "  materials: [" +
    "   { id: 'm', specularIntensity: 'var(--si, 0.25)', specularColor: 'var(--sc, 0.5 0.5 0.5)' }," +
    "   { id: 'n', specularIntensity: 'var(--missing-si)' }" +
    "  ]," +
    "  objects: [{ id: 'o', specularIntensity: 'var(--oi, 0)' }]," +
    "  meshObjects: [{ id: 'p', specularColor: 'var(--pc, 0.1 0.2 0.3)' }]," +
    "  instancedMeshes: [{ id: 'b', specularIntensity: 'var(--bi, 0.75)', specularColor: 'var(--bc, 1, 0.5, 0.25)' }]" +
    "};" +
    "const state = {" +
    " source: bundle," +
    " out: bundle," +
    " dynamic: false, patches: [], resolvedVars: {}, varTransitions: [], prevResolved: null, prevTransitions: []" +
    "};" +
    "const css = { mount: null, sentinels: null, styles: null, hasComputedStyle: false, revision: 0, transitionFrame: 0 };" +
    "sceneCSSResolveExplicitVars(state, css);" +
    "return { m: state.out.materials[0], n: state.out.materials[1], o: state.out.objects[0]," +
    " p: state.out.meshObjects[0], b: state.out.instancedMeshes[0], dynamic: state.dynamic };" +
    "})()");

  // Positive cases: var fallbacks resolve and coerce numerically; explicit
  // zero stays explicit zero.
  assert.strictEqual(result.m.specularIntensity, 0.25);
  assert.strictEqual(result.o.specularIntensity, 0);
  assert.strictEqual(result.b.specularIntensity, 0.75);
  // Resolved tint text becomes a real LINEAR triple on every collection,
  // not leftover CSS text.
  assert.deepStrictEqual(Array.from(result.m.specularColor), [0.5, 0.5, 0.5]);
  assert.deepStrictEqual(Array.from(result.p.specularColor), [0.1, 0.2, 0.3]);
  assert.deepStrictEqual(Array.from(result.b.specularColor), [1, 0.5, 0.25]);
  // Fallback case: an unmatched var with no fallback keeps the authored
  // var string untouched rather than coercing it.
  assert.strictEqual(result.n.specularIntensity, "var(--missing-si)");
  assert.strictEqual(result.dynamic, true);

  // The resolved tint is consumed by the real downstream normalizer: its
  // material profile matches the explicit triple's profile and differs
  // from the default.
  const profileKeyOf = (expression) => callIn(context,
    "sceneObjectMaterialProfile({ materialKind: 'standard', specularColor: " + expression + " }).key");
  const resolvedProfileKey = profileKeyOf(JSON.stringify(result.m.specularColor));
  assert.strictEqual(resolvedProfileKey, profileKeyOf("[0.5, 0.5, 0.5]"));
  assert.notStrictEqual(resolvedProfileKey, profileKeyOf("null"));
});
