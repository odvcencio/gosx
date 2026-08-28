"use strict";

// Stage 1 of alpha-masked mesh materials. These tests execute the actual
// production fragments inside a VM; they do not reimplement the production
// algorithms. This stage carries normalized alphaCutoff data only — no
// alpha-mask pass routing is advertised or asserted.

const { test } = require("node:test");
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

// The core's trailing export block reads engine-frame helpers that only
// exist in the assembled monolith; everything under test sits above that
// marker, so trim exactly there as the existing material tests do.
function trimBeforeSharedApiExport(source) {
  const at = source.indexOf(SHARED_API_EXPORT_MARKER);
  assert.ok(at >= 0, "core '// Scene3D shared API' export marker located");
  return source.slice(0, at);
}

// Extract the source region between two explicit markers so mount-webgl
// fragments can be evaluated without the engine-frame monolith tail.
function sliceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  assert.ok(start >= 0, "start marker located: " + startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(end > start, "end marker located after start: " + endMarker);
  return source.slice(start, end);
}

function runFragment(context, source, filename) {
  vm.runInContext(source, context, { filename });
}

const SCENE_FRAGMENT_FILES = [
  "10-runtime-primitives.ts",
  "10-runtime-scene-utils.ts",
  "11-scene-math.ts",
  "12-scene-geometry.ts",
  "13-scene-material.ts",
];

// Scene-side context: the material fragment plus its production
// dependencies, loaded whole in chunk order exactly as the runtime
// assembles them.
function createSceneCoreContext() {
  const context = vm.createContext({ console, window: {} });
  for (const name of SCENE_FRAGMENT_FILES) {
    runFragment(context, readBootstrapSource(name), name);
  }
  runFragment(context,
    trimBeforeSharedApiExport(readBootstrapSource("10-runtime-scene-core.ts")),
    "10-runtime-scene-core.ts");
  runFragment(context, readBootstrapSource("15b-scene-planner.ts"), "15b-scene-planner.ts");
  // Mount-side model hide-gate fragment: the actual production mount-webgl
  // model-hidden functions, sliced between stable function markers so no
  // engine-frame monolith tail is evaluated. They call only helpers already
  // present in the scene-core context loaded above.
  runFragment(context,
    sliceBetween(readRuntimeSource("mount-webgl.ts"),
      "function sceneModelMaxScale",
      "function sceneModelRotateDirection"),
    "mount-webgl.ts#scene-model-hidden");
  return context;
}

// Loader-side context: math + glTF only, matching the existing loader
// tests. glTF alpha-cutoff extraction must not depend on the material
// fragment, so the loader context deliberately omits it.
function createLoaderContext() {
  const warnings = [];
  const sandbox = {
    console: {
      warn: (...args) => warnings.push(args.join(" ")),
      error: () => {},
      log: () => {},
    },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    isFinite,
    Float32Array,
    Uint8Array,
    Uint16Array,
    Uint32Array,
    Int8Array,
    Int16Array,
    ArrayBuffer,
    DataView,
    TextDecoder,
    Error,
    URL,
    Blob: class {
      constructor(parts, options) {
        this.parts = parts;
        this.type = options && options.type;
      }
    },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.URL = { createObjectURL: () => "blob:fake" };

  const context = vm.createContext(sandbox);
  runFragment(context, readBootstrapSource("11-scene-math.ts"), "11-scene-math.ts");
  runFragment(context, readRuntimeSource("gltf.ts"), "gltf.ts");
  return { context, warnings };
}

function callIn(context, expression) {
  return vm.runInContext(expression, context, { filename: "scene3d-alpha-mask-expression.js" });
}

test("keyed material hashing keeps alphaCutoff states distinct", () => {
  const context = createSceneCoreContext();
  const res = callIn(context,
    "(function () {" +
    "  const h = (alphaCutoff) => scenePlannerHashMaterial(0, { key: 'k', kind: 'standard', alphaCutoff: alphaCutoff });" +
    "  return {" +
    "    omitted: h(undefined)," +
    "    disabled: h(null)," +
    "    zero: h(0)," +
    "    fine: h(0.3)," +
    "    finer: h(0.3000001)," +
    "    css: h('0.3')," +
    "    cssVar: h(' var(--cut) ')," +
    "    over: h(1.5)" +
    "  };" +
    "})()");

  // Omitted and explicit null both normalize to the disabled state.
  assert.equal(res.omitted, res.disabled);
  // Disabled stays distinct from an explicit 0.
  assert.notEqual(res.zero, res.disabled);
  assert.notEqual(res.zero, res.omitted);
  // Exact serialization keeps close cutoffs distinct even with material.key.
  assert.notEqual(res.fine, res.finer);
  assert.notEqual(res.fine, res.zero);
  assert.notEqual(res.fine, res.disabled);
  // A CSS-authored cutoff string is not the disabled state.
  assert.notEqual(res.css, res.disabled);
  // Actual CSS var strings and values above 1 remain distinct hashed states.
  assert.notEqual(res.cssVar, res.disabled);
  assert.notEqual(res.cssVar, res.fine);
  assert.notEqual(res.over, res.fine);
  assert.notEqual(res.over, res.disabled);
});

test("alphaCutoff CSS resolution and signature invalidation across collections", () => {
  const context = createSceneCoreContext();
  const res = callIn(context,
    "(function () {" +
    "  const bundle = {" +
    "    materials: [{ id: 'm', alphaCutoff: 'var(--ac, 0.25)' }, { id: 'n', alphaCutoff: 'var(--missing-ac)' }]," +
    "    objects: [{ id: 'o', alphaCutoff: 'var(--oc, 0)' }]," +
    "    meshObjects: [{ id: 'p', alphaCutoff: 'var(--pc, 0.5)' }]," +
    "    instancedMeshes: [{ id: 'b', alphaCutoff: 'var(--bc, 0.75)' }]" +
    "  };" +
    "  const before = JSON.stringify(bundle);" +
  "  const state = { source: bundle, out: bundle, dynamic: false, patches: [], resolvedVars: {}, varTransitions: [], prevResolved: null, prevTransitions: [] };" +
    "  const css = { mount: null, sentinels: null, styles: null, hasComputedStyle: false, revision: 0, transitionFrame: 0 };" +
    "  sceneCSSResolveExplicitVars(state, css);" +
  "  const empty = { environment: null, lights: [], points: [], labels: [], sprites: [] };" +
  "  const sigFor = (collection, id) => {" +
  "    const base = Object.assign({}, empty);" +
  "    base[collection] = [{ id: id, alphaCutoff: 0.3 }];" +
  "    const at = (v) => { base[collection][0].alphaCutoff = v; return sceneCSSInputSignature(base); };" +
  "    return { fine: at(0.3), finer: at(0.3000001), zero: at(0), disabled: at(null), omitted: at(undefined) };" +
  "  };" +
    "  return {" +
    "    before: before, after: JSON.stringify(bundle)," +
    "    m: state.out.materials[0], n: state.out.materials[1], o: state.out.objects[0]," +
    "    p: state.out.meshObjects[0], b: state.out.instancedMeshes[0], dynamic: state.dynamic," +
  "    mats: sigFor('materials', 'm'), objs: sigFor('objects', 'o')," +
  "    meshes: sigFor('meshObjects', 'p'), inst: sigFor('instancedMeshes', 'b')" +
    "  };" +
    "})()");

  // Var fallbacks resolve on all four collections, including an explicit 0.
  assert.strictEqual(res.m.alphaCutoff, 0.25);
  assert.strictEqual(res.o.alphaCutoff, 0);
  assert.strictEqual(res.p.alphaCutoff, 0.5);
  assert.strictEqual(res.b.alphaCutoff, 0.75);
  // An unmatched var with no fallback keeps the authored string untouched.
  assert.strictEqual(res.n.alphaCutoff, "var(--missing-ac)");
  assert.strictEqual(res.dynamic, true);
  // Resolution rewrote the out copy, never the source bundle.
  assert.equal(res.before, res.after);

  // Signatures invalidate with only one collection changed per comparison,
  // distinguishing close values, undefined, null and 0 in each collection.
  for (const group of [res.mats, res.objs, res.meshes, res.inst]) {
    assert.notEqual(group.fine, group.finer);
    assert.notEqual(group.disabled, group.omitted);
    assert.notEqual(group.zero, group.disabled);
    assert.notEqual(group.zero, group.omitted);
  }
});

// plain strips VM realm prototypes so node:assert can compare values.
function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

// --- scene-side numeric contract --------------------------------------------

test("sceneNormalizeMaterialAlphaCutoff normalizes cutoffs without coercing to 0", () => {
  const context = createSceneCoreContext();
  const normalize = (value, fallback) =>
    callIn(context, "sceneNormalizeMaterialAlphaCutoff(" + value + ", " + fallback + ")");

  // undefined inherits a validated fallback.
  assert.strictEqual(normalize("undefined", "0.25"), 0.25);
  assert.strictEqual(normalize("undefined", "'0.75'"), 0.75);
  assert.strictEqual(normalize("undefined", "null"), null);
  assert.strictEqual(normalize("undefined", "-1"), null);
  assert.strictEqual(normalize("undefined", "true"), null);
  assert.strictEqual(normalize("undefined", "undefined"), null);

  // Explicit null disables and never inherits.
  assert.strictEqual(normalize("null", "0.5"), null);

  // Finite numbers >= 0 are preserved, including 0 and values above 1
  // (no upper clamp).
  assert.strictEqual(normalize("0", "0.5"), 0);
  assert.strictEqual(normalize("1", "0.5"), 1);
  assert.strictEqual(normalize("1.5", "0.5"), 1.5);
  assert.strictEqual(normalize("100", "0.5"), 100);

  // Nonempty numeric strings are allowed; empty and invalid strings are not.
  assert.strictEqual(normalize("'0.33'", "0.5"), 0.33);
  assert.strictEqual(normalize("' 0.75 '", "0.5"), 0.75);
  assert.strictEqual(normalize("''", "0.25"), 0.25);
  assert.strictEqual(normalize("'abc'", "0.25"), 0.25);

  // Booleans, non-finite and negative values fall back safely.
  assert.strictEqual(normalize("true", "0.25"), 0.25);
  assert.strictEqual(normalize("false", "0.25"), 0.25);
  assert.strictEqual(normalize("NaN", "0.25"), 0.25);
  assert.strictEqual(normalize("Infinity", "0.25"), 0.25);
  assert.strictEqual(normalize("-Infinity", "0.25"), 0.25);
  assert.strictEqual(normalize("-0.1", "0.25"), 0.25);

  // CSS variable strings are trimmed and preserved via the shared machinery.
  assert.strictEqual(normalize("' var(--cutoff) '", "0.5"), "var(--cutoff)");
  assert.strictEqual(normalize("undefined", "' var(--fallback) '"), "var(--fallback)");
  assert.strictEqual(normalize("'bogus'", "' var(--fallback) '"), "var(--fallback)");
});

// --- profile and cache-key contract -----------------------------------------

test("sceneObjectMaterialProfile and sceneMaterialProfileKey carry distinct alpha cutoffs", () => {
  const context = createSceneCoreContext();
  const profile = (expr) => callIn(context, "sceneObjectMaterialProfile(" + expr + ")");
  const key = (expr) => callIn(context, "sceneMaterialProfileKey(sceneObjectMaterialProfile(" + expr + "))");

  assert.strictEqual(profile("{}").alphaCutoff, null);
  assert.strictEqual(profile("{alphaCutoff: null}").alphaCutoff, null);
  assert.strictEqual(profile("{alphaCutoff: 0}").alphaCutoff, 0);
  assert.strictEqual(profile("{alphaCutoff: 0.5}").alphaCutoff, 0.5);
  assert.strictEqual(profile("{alphaCutoff: 2}").alphaCutoff, 2);
  assert.strictEqual(profile("{alphaCutoff: '0.75'}").alphaCutoff, 0.75);
  assert.strictEqual(profile("{alphaCutoff: ' var(--cut) '}").alphaCutoff, "var(--cut)");
  assert.strictEqual(profile("{alphaCutoff: 'nope'}").alphaCutoff, null);

  // Disabled (null) never collides with an explicit 0 threshold.
  assert.notEqual(key("{}"), key("{alphaCutoff: 0}"));
  assert.equal(key("{alphaCutoff: null}"), key("{}"));

  // Full precision: no toFixed quantization on the cutoff slot.
  assert.notEqual(key("{alphaCutoff: 0.3}"), key("{alphaCutoff: 0.30000000000000004}"));

  // Numeric strings and numbers normalize onto the same cached material.
  assert.equal(key("{alphaCutoff: 0.5}"), key("{alphaCutoff: '0.5'}"));

  // CSS variables keep their trimmed form in the key.
  assert.ok(key("{alphaCutoff: ' var(--cut) '}").includes("var(--cut)"));
});

// --- glTF extraction ----------------------------------------------------------

function materialDoc(material) {
  return { asset: { version: "2.0" }, materials: [material] };
}

test("gltfExtractMaterial carries alphaCutoff on MASK only", () => {
  const { context } = createLoaderContext();
  const extract = (material) =>
    plain(callIn(context, "gltfExtractMaterial(" + JSON.stringify(materialDoc(material)) + ", 0, null)"));
  const hasCutoff = (record) => Object.prototype.hasOwnProperty.call(record, "alphaCutoff");

  const def = extract({ alphaMode: "MASK", pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0.4] } });
  assert.equal(def.alphaMode, "MASK");
  assert.equal(def.opacity, 0.4);
  assert.equal(def.alphaCutoff, 0.5);

  assert.equal(extract({ alphaMode: "MASK", alphaCutoff: 0, pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] } }).alphaCutoff, 0);
  assert.equal(extract({ alphaMode: "MASK", alphaCutoff: 1, pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] } }).alphaCutoff, 1);
  assert.equal(extract({ alphaMode: "MASK", alphaCutoff: 2.5, pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] } }).alphaCutoff, 2.5);

  // No numeric-string coercion in glTF; malformed authored values default.
  assert.equal(extract({ alphaMode: "MASK", alphaCutoff: "0.4", pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] } }).alphaCutoff, 0.5);
  assert.equal(extract({ alphaMode: "MASK", alphaCutoff: -1, pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] } }).alphaCutoff, 0.5);
  assert.equal(extract({ alphaMode: "MASK", alphaCutoff: null, pbrMetallicRoughness: { baseColorFactor: [1, 1, 1, 1] } }).alphaCutoff, 0.5);

  // Non-finite authored values (evaluated inside the VM so they survive
  // without JSON) also default.
  assert.equal(callIn(context,
    'gltfExtractMaterial({asset:{version:"2.0"},materials:[{alphaMode:"MASK",alphaCutoff:NaN,pbrMetallicRoughness:{baseColorFactor:[1,1,1,1]}}]}, 0, null).alphaCutoff'), 0.5);
  assert.equal(callIn(context,
    'gltfExtractMaterial({asset:{version:"2.0"},materials:[{alphaMode:"MASK",alphaCutoff:Infinity,pbrMetallicRoughness:{baseColorFactor:[1,1,1,1]}}]}, 0, null).alphaCutoff'), 0.5);

  // OPAQUE/BLEND/omitted never receive a cutoff, even when authored, and
  // factor opacity behavior is unchanged (OPAQUE pins to 1).
  const modes = ["OPAQUE", "BLEND", null];
  for (const mode of modes) {
    const material = mode === null
      ? { alphaCutoff: 0.3, pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0.25] } }
      : { alphaMode: mode, alphaCutoff: 0.3, pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0.25] } };
    const record = extract(material);
    assert.equal(hasCutoff(record), false, String(mode));
    if (mode === "OPAQUE") {
      assert.equal(record.opacity, 1);
    }
  }
});

test("gltfIsAlphaMaterial excludes MASK and preserves BLEND/unknown behavior", () => {
  const { context } = createLoaderContext();
  assert.equal(callIn(context, 'gltfIsAlphaMaterial({ alphaMode: "MASK", opacity: 0.4 })'), false);
  assert.equal(callIn(context, 'gltfIsAlphaMaterial({ alphaMode: "MASK", opacity: 1 })'), false);
  assert.equal(callIn(context, 'gltfIsAlphaMaterial({ alphaMode: "BLEND", opacity: 1 })'), true);
  assert.equal(callIn(context, 'gltfIsAlphaMaterial({ alphaMode: "OPAQUE", opacity: 0.5 })'), true);
  assert.equal(callIn(context, 'gltfIsAlphaMaterial({ alphaMode: "OPAQUE", opacity: 1 })'), false);
  assert.equal(callIn(context, 'gltfIsAlphaMaterial({ alphaMode: "CUSTOM", opacity: 0.5 })'), true);
  assert.equal(callIn(context, 'gltfIsAlphaMaterial({ alphaMode: "CUSTOM", opacity: 1 })'), false);
});

// One shared in-memory scene: points mode 0, LINE_STRIP mode 3, triangles
// mode 4 over a single 3-vertex noncollinear accessor.
function alphaSceneDoc(material) {
  return {
    asset: { version: "2.0" },
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{ primitives: [
      { attributes: { POSITION: 0 }, mode: 0, material: 0 },
      { attributes: { POSITION: 0 }, mode: 3, material: 0 },
      { attributes: { POSITION: 0 }, mode: 4, material: 0 },
    ] }],
    materials: [material],
    accessors: [{ bufferView: 0, componentType: 5126, count: 3, type: "VEC3", min: [0, 0, 0], max: [1, 1, 0] }],
    bufferViews: [{ buffer: 0, byteLength: 36 }],
    buffers: [{ byteLength: 36 }],
  };
}

test("MASK materials map to the opaque pass and never mutate the source doc", () => {
  const { context } = createLoaderContext();
  const doc = JSON.stringify(alphaSceneDoc({
    alphaMode: "MASK",
    alphaCutoff: 0.25,
    pbrMetallicRoughness: { baseColorFactor: [1, 0, 0, 0.4] },
  }));
  const res = plain(callIn(context,
    "(function () {" +
    "  var docObj = " + doc + ";" +
    "  var before = JSON.stringify(docObj);" +
    "  var scene = gltfExtractScene(docObj, new Float32Array([0,0,0, 1,0,0, 0,1,0]).buffer);" +
    "  return { before: before, after: JSON.stringify(docObj), scene: scene };" +
    "})()"));

  assert.equal(res.before, res.after, "gltfExtractScene mutated the source doc");

  const mesh = res.scene.objects.find((o) => o.kind === "gltf-mesh");
  const lines = res.scene.objects.find((o) => o.kind === "lines");
  assert.ok(mesh && lines);

  assert.equal(mesh.material.alphaCutoff, 0.25);
  assert.equal(mesh.material.opacity, 0.4);
  // Stage 1: MASK stays out of the alpha pass on every primitive class.
  assert.equal(mesh.renderPass, "opaque");
  assert.equal(lines.blendMode, "");
  assert.equal(lines.opacity, 0.4);
  assert.equal(res.scene.points[0].blendMode, "");
  assert.equal(res.scene.points[0].depthWrite, true);
});

test("mount model override plumbing applies alphaCutoff to raw and nested material", () => {
  const mountSource = readRuntimeSource("mount-webgl.ts");
  assert.ok(mountSource.includes("function sceneModelMaterialOverrideSource"));
  assert.ok(mountSource.includes("function sceneApplyModelLOD"));
  const context = createSceneCoreContext();
  runFragment(context, sliceBetween(mountSource,
    "function sceneModelMaterialOverrideSource",
    "function sceneApplyModelLOD"), "mount-override-extract.js");

  // Direct raw model: authored cutoffs are preserved, 0 and >1 included.
  const applied = callIn(context,
    'sceneApplyMaterialOverride({ material: { alphaCutoff: 0.4 } }, { alphaCutoff: 0.5 })');
  assert.strictEqual(applied.alphaCutoff, 0.5);
  assert.strictEqual(applied.material.alphaCutoff, 0.5);
  const zero = callIn(context,
    'sceneApplyMaterialOverride({ material: { alphaCutoff: 0.4 } }, { alphaCutoff: 0 })');
  assert.strictEqual(zero.alphaCutoff, 0);
  assert.strictEqual(zero.material.alphaCutoff, 0);
  const high = callIn(context,
    'sceneApplyMaterialOverride({ material: { alphaCutoff: 0.4 } }, { alphaCutoff: 1.5 })');
  assert.strictEqual(high.alphaCutoff, 1.5);
  assert.strictEqual(high.material.alphaCutoff, 1.5);
  // An explicit null clears the imported masking on both targets.
  const cleared = callIn(context,
    'sceneApplyMaterialOverride({ material: { alphaCutoff: 0.4 } }, { alphaCutoff: null })');
  assert.strictEqual(cleared.alphaCutoff, null);
  assert.strictEqual(cleared.material.alphaCutoff, null);
  // Absence in the model override leaves the imported asset masking alone.
  const untouched = callIn(context,
    'sceneApplyMaterialOverride({ material: { alphaCutoff: 0.4 } }, {})');
  assert.strictEqual(untouched.alphaCutoff, undefined);
  assert.strictEqual(untouched.material.alphaCutoff, 0.4);
  // Regression: an own { alphaCutoff: undefined } in the model override must
  // not erase the imported asset cutoff (fails before the source-side guard).
  const ownUndefined = callIn(context,
    'sceneApplyMaterialOverride({ material: { alphaCutoff: 0.4 } }, { alphaCutoff: undefined })');
  assert.strictEqual(ownUndefined.alphaCutoff, undefined);
  assert.strictEqual(ownUndefined.material.alphaCutoff, 0.4);
  // A normalized model carries the authored cutoff into its override bag.
  const normalized = callIn(context,
    'sceneApplyMaterialOverride({ material: { alphaCutoff: 0.4 } }, normalizeSceneModel({ alphaCutoff: "0.2" }, 0))');
  assert.strictEqual(normalized.material.alphaCutoff, 0.2);
});

test("instanced GLB -> override chain preserves alphaCutoff masking end to end", () => {
  const mountSource = readRuntimeSource("mount-webgl.ts");
  const context = createSceneCoreContext();
  runFragment(context, sliceBetween(mountSource,
    "function sceneModelMaterialOverrideSource",
    "function sceneApplyModelLOD"), "mount-override-extract.js");

  // The chain runs the real instanced-GLB batch normalization (one default
  // instance merged into the raw entry), the real batch-to-models conversion,
  // and the real override application against a raw glTF record whose
  // material.alphaCutoff is the asset-authored 0.4.
  const chain = (entryLiteral, fallbackLiteral) => callIn(context,
    "(() => {" +
    "const batch = normalizeSceneInstancedGLBMeshEntry(Object.assign(" + entryLiteral + ", { instances: [{}] }), 0, " + fallbackLiteral + ");" +
    "const models = sceneInstancedGLBMeshToModels(batch, 0);" +
    "if (!models || models.length !== 1 || !models[0]) throw new Error('expected exactly one model from instanced batch');" +
    "return sceneApplyMaterialOverride({ material: { alphaCutoff: 0.4 } }, models[0]);" +
    "})()");

  // An omitted batch cutoff leaves the glTF asset's authored 0.4 untouched.
  assert.strictEqual(chain('{ src: "model.glb" }', "null").material.alphaCutoff, 0.4);
  // An authored batch cutoff overrides the asset, zero and >1 included.
  assert.strictEqual(chain('{ src: "model.glb", alphaCutoff: 0 }', "null").material.alphaCutoff, 0);
  assert.strictEqual(chain('{ src: "model.glb", alphaCutoff: 1.5 }', "null").material.alphaCutoff, 1.5);
  // An inherited fallback cutoff flows through the chain.
  assert.strictEqual(chain('{ src: "model.glb" }', '{ alphaCutoff: 0.25 }').material.alphaCutoff, 0.25);
  // An own undefined batch cutoff falls back instead of erasing asset masking.
  assert.strictEqual(chain('{ src: "model.glb", alphaCutoff: undefined }', "null").material.alphaCutoff, 0.4);
  assert.strictEqual(chain('{ src: "model.glb", alphaCutoff: undefined }', '{ alphaCutoff: 0.25 }').material.alphaCutoff, 0.25);
  // An explicit null disables masking downstream.
  assert.strictEqual(chain('{ src: "model.glb", alphaCutoff: null }', '{ alphaCutoff: 0.25 }').material.alphaCutoff, null);

  // Own-field absence after undefined: batch and model override omit the key
  // entirely so imported asset masking can never be erased by plumbing.
  const absence = callIn(context,
    "(() => {" +
    "const batch = normalizeSceneInstancedGLBMeshEntry({ src: 'model.glb', alphaCutoff: undefined, instances: [{}] }, 0, null);" +
    "const model = normalizeSceneModel({ src: 'model.glb', alphaCutoff: undefined }, 0);" +
    "return {" +
    " batchHas: Object.prototype.hasOwnProperty.call(batch, 'alphaCutoff')," +
    " overrideHas: Boolean(model.materialOverride && Object.prototype.hasOwnProperty.call(model.materialOverride, 'alphaCutoff'))" +
    "};" +
    "})()");
  assert.strictEqual(absence.batchHas, false);
  assert.strictEqual(absence.overrideHas, false);

  // The nested material is a separate copy target: the override reaches both
  // the direct field and the nested material while the raw asset record
  // passed in stays untouched by the copy itself.
  // The nested material is a separate copy target: the production override
  // reaches both the direct field and the nested material while the raw
  // asset record passed in stays untouched by the copy itself.
  const nested = callIn(context,
    "(() => {" +
    "const raw = { alphaCutoff: 0.5, material: { alphaCutoff: 0.4 } };" +
    "const next = sceneApplyMaterialOverride(raw, { alphaCutoff: 0.25 });" +
    "return {" +
    " rawMaterialCutoff: raw.material.alphaCutoff," +
    " nextDirectCutoff: next.alphaCutoff," +
    " nextMaterialCutoff: next.material.alphaCutoff," +
    " sharesMaterial: next.material === raw.material," +
    " sharesRecord: next === raw" +
    "};" +
    "})()");
  assert.strictEqual(nested.nextDirectCutoff, 0.25);
  assert.strictEqual(nested.nextMaterialCutoff, 0.25);
  assert.strictEqual(nested.rawMaterialCutoff, 0.4, "raw asset material stays untouched by the copy");
  assert.strictEqual(nested.sharesMaterial, false, "nested material is a clone, not the raw record's");
});

// --- alphaCutoff normalization + named-material application -----------------

test("scene object/instanced normalizers flatten nested alphaCutoff with fallback carry-through", () => {
  const context = createSceneCoreContext();
  for (const name of ["normalizeSceneObject", "normalizeSceneInstancedMeshEntry"]) {
    const run = (item, fallback) => callIn(context, "(function () {" +
      " return " + name + "(" + JSON.stringify(item) + ", 0, " + JSON.stringify(fallback) + ");" +
      "})()");
    const fallback = { alphaCutoff: 0.4 };
    const nested = run({ material: { alphaCutoff: 0.25 } }, fallback);
    assert.equal(nested.alphaCutoff, 0.25, name + ": nested material value wins over fallback");
    assert.equal(run({ material: { alphaCutoff: 0.25 }, alphaCutoff: 0.9 }, fallback).alphaCutoff, 0.25,
      name + ": nested material value wins over the direct field");
    const cssString = run({ material: "var(--paint)", alphaCutoff: 0.7 }, fallback);
    assert.equal(cssString.alphaCutoff, 0.7, name + ": cutoff propagates alongside a CSS material string");
    const cssVar = run({ alphaCutoff: " var(--cut) " }, fallback);
    assert.ok(String(cssVar.alphaCutoff).includes("var(--cut)"),
      name + ": CSS var cutoff string preserved through normalization");
    assert.equal(run({ alphaCutoff: 0 }, fallback).alphaCutoff, 0, name + ": direct 0 preserved");
    assert.equal(run({ alphaCutoff: 1.5 }, fallback).alphaCutoff, 1.5, name + ": values above 1 preserved");
    assert.equal(run({ alphaCutoff: "nope" }, fallback).alphaCutoff, 0.4, name + ": invalid value uses fallback");
    assert.equal(run({}, fallback).alphaCutoff, 0.4, name + ": omitted value inherits fallback");
    // JSON round-tripping drops own-undefined fields, so call the VM with a
    // direct literal (index 0, fallback as third arg) to exercise undefined.
    const undefinedRun = callIn(context, "(function () { return " + name +
      "({ alphaCutoff: undefined }, 0, " + JSON.stringify(fallback) + "); })()");
    assert.equal(undefinedRun.alphaCutoff, 0.4, name + ": undefined inherits fallback");
    assert.equal(run({ alphaCutoff: null }, fallback).alphaCutoff, null, name + ": explicit null clears masking");
    assert.equal(run({ alphaCutoff: " 0.3 " }, fallback).alphaCutoff, 0.3, name + ": trimmed numeric string normalizes");
    assert.equal(run({ alphaCutoff: 0 }, {}).alphaCutoff, 0, name + ": direct 0 survives even with no fallback");
  }
});

test("material record normalization feeds named-material application", () => {
  const context = createSceneCoreContext();
  const record = (raw, fallback) => callIn(context, "(function () {" +
    " return normalizeSceneMaterialRecord(" + JSON.stringify(raw) + ", 0, " + JSON.stringify(fallback) + ");" +
    "})()");
  const apply = (raw, fallback, object) => callIn(context, "(function () {" +
    " return sceneApplyNamedMaterialToObject(" + JSON.stringify(object) + ", normalizeSceneMaterialRecord(" +
    JSON.stringify(raw) + ", 0, " + JSON.stringify(fallback) + "));" +
    "})()");
  const obj = { alphaCutoff: 0.4 };

  const omitted = record({}, undefined);
  assert.equal(Object.prototype.hasOwnProperty.call(omitted, "alphaCutoff"), false,
    "omitted cutoff with omitted fallback leaves no own alphaCutoff field");
  assert.equal(apply({}, undefined, obj).alphaCutoff, 0.4, "omitted record retains object 0.4");
  // JSON.stringify drops own undefined values, so the ownundefined cases are
  // exercised through direct VM expression literals instead.
  const ownUndefinedRecord = callIn(context,
    "normalizeSceneMaterialRecord({ alphaCutoff: undefined }, 0, undefined)");
  assert.equal(Object.prototype.hasOwnProperty.call(ownUndefinedRecord, "alphaCutoff"), false,
    "ownundefined record leaves no own alphaCutoff field");
  const ownUndefinedApply = callIn(context,
    "(function () { return sceneApplyNamedMaterialToObject({ alphaCutoff: 0.4 }," +
    " normalizeSceneMaterialRecord({ alphaCutoff: undefined }, 0, undefined)); })()");
  assert.equal(ownUndefinedApply.alphaCutoff, 0.4, "ownundefined record retains object 0.4");

  const cleared = record({ alphaCutoff: null }, { alphaCutoff: 0.4 });
  assert.equal(cleared.alphaCutoff, null, "explicit null keeps own field and clears masking");
  assert.equal(Object.prototype.hasOwnProperty.call(cleared, "alphaCutoff"), true);
  assert.equal(apply({ alphaCutoff: null }, { alphaCutoff: 0.4 }, obj).alphaCutoff, null,
    "explicit null clears through application");

  assert.equal(record({ alphaCutoff: 0 }, { alphaCutoff: 0.4 }).alphaCutoff, 0, "0 preserved over fallback");
  assert.equal(apply({ alphaCutoff: 0 }, { alphaCutoff: 0.4 }, obj).alphaCutoff, 0, "applied 0 preserved");
  assert.equal(record({ alphaCutoff: 1.5 }, { alphaCutoff: 0.4 }).alphaCutoff, 1.5, "values above 1 preserved");

  assert.equal(record({}, { alphaCutoff: 0.2 }).alphaCutoff, 0.2, "fallback cutoff inherited");
  assert.equal(record({ alphaCutoff: "nope" }, { alphaCutoff: 0.2 }).alphaCutoff, 0.2,
    "invalid raw value uses fallback");
  assert.equal(record({ alphaCutoff: " 0.3 " }, { alphaCutoff: 0.2 }).alphaCutoff, 0.3,
    "trimmed numeric string normalizes");

  const omittedKey = record({}, undefined).key;
  const zeroKey = record({ alphaCutoff: 0 }, undefined).key;
  assert.notEqual(omittedKey, zeroKey, "profile key distinguishes omitted cutoff from 0");
});

test("opacity-based invisibility cull spares enabled numeric alpha cutoffs", () => {
  const context = createSceneCoreContext();
  const invisible = (object, material) => callIn(context,
    "(function () { return sceneMeshObjectEffectivelyInvisible(" +
    JSON.stringify(object) + ", " + JSON.stringify(material) + "); })()");

  // cutoff 0 with opacity 0 must survive the opacity-based cull (equality
  // matters: 0 is a valid enabled cutoff).
  assert.equal(invisible({}, { opacity: 0, alphaCutoff: 0 }), false,
    "cutoff 0 + opacity 0 is not CPU-culled as invisible");
  // Small positive cutoff with tiny opacity also survives.
  assert.equal(invisible({}, { opacity: 0.00001, alphaCutoff: 0.01 }), false,
    "small positive cutoff + tiny opacity is not CPU-culled");

  // Non-numeric or non-enabled cutoff states retain the legacy cull.
  assert.equal(invisible({}, { opacity: 0 }), true, "absent cutoff + opacity 0 stays invisible");
  assert.equal(invisible({}, { opacity: 0, alphaCutoff: null }), true,
    "null cutoff + opacity 0 stays invisible");
  assert.equal(invisible({}, { opacity: 0, alphaCutoff: "nope" }), true,
    "invalid cutoff + opacity 0 stays invisible");
  assert.equal(invisible({}, { opacity: 0, alphaCutoff: " var(--cut) " }), true,
    "unresolved CSS var cutoff + opacity 0 stays invisible");

  // Other culling paths are untouched for masked materials.
  assert.equal(invisible({ visible: false }, { opacity: 1, alphaCutoff: 0.5 }), true,
    "visible:false still culls masked materials");
  assert.equal(invisible({ _modelHidden: true }, { opacity: 1, alphaCutoff: 0.5 }), true,
    "_modelHidden still culls masked materials");
  assert.equal(invisible({ scaleX: 0, scaleY: 0, scaleZ: 0 }, { opacity: 1, alphaCutoff: 0.5 }), true,
    "zero scale still culls masked materials");

  // Opaque unmasked materials behave exactly as before.
  assert.equal(invisible({}, { opacity: 1 }), false, "opacity 1 without cutoff stays visible");
});

// The mount-side model zero-opacity hide gate must mirror the core CPU-cull:
// a normalized object material carrying an enabled numeric alpha cutoff
// (0 included) keeps the object out of the _modelHidden draw gate even at
// model opacity 0 / fill factor 0, while absent or null cutoffs keep the
// legacy hide. Authored visible:false and zero-scale paths stay untouched.
test("scene model zero-opacity hide gate spares enabled numeric alpha cutoffs", () => {
  const context = createSceneCoreContext();
  const hidden = (model, object) => callIn(context,
    "(function () { return sceneModelEffectivelyHidden(" +
    JSON.stringify(model) + ", normalizeSceneObject(" +
    JSON.stringify(object) + ", 0, " + JSON.stringify(model) + ")); })()");
  const appliedHidden = (object, model) => callIn(context,
    "(function () { var o = normalizeSceneObject(" +
    JSON.stringify(object) + ", 0, " + JSON.stringify(model) + ");" +
    " sceneApplyModelObjectHiddenState(o, " + JSON.stringify(model) + ");" +
    " return o._modelHidden === true; })()");

  const zeroFactorModel = { opacity: 0 };
  // cutoff 0 with fill factor 0: the masked model stays out of the hide gate.
  assert.equal(hidden(zeroFactorModel, { material: { alphaCutoff: 0 } }), false,
    "cutoff 0 + factor 0 masked model is not effectively hidden");
  assert.equal(appliedHidden({ material: { alphaCutoff: 0 } }, zeroFactorModel), false,
    "cutoff 0 masked model draw gate stays open (_modelHidden false)");

  // Absent / null cutoff states retain the legacy zero-opacity hide.
  assert.equal(hidden(zeroFactorModel, {}), true,
    "absent cutoff + factor 0 stays effectively hidden");
  assert.equal(hidden(zeroFactorModel, { material: { alphaCutoff: null } }), true,
    "null cutoff + factor 0 stays effectively hidden");
  assert.equal(appliedHidden({}, zeroFactorModel), true,
    "absent cutoff draw gate closes (_modelHidden true)");

  // Other hide paths are untouched for masked models.
  assert.equal(
    hidden({ opacity: 0, visible: false }, { material: { alphaCutoff: 0 } }),
    true, "visible:false still hides masked models");
  assert.equal(
    hidden({ opacity: 0, scaleX: 0, scaleY: 0, scaleZ: 0 }, { material: { alphaCutoff: 0 } }),
    true, "zero scale still hides masked models");
});
