"use strict";

// Stage 2 of alpha-masked mesh materials: built-in default mask routing.
// Same VM harness contract as scene3d-alpha-mask.test.js — the actual
// production fragments run unmodified; no reimplementation, no fakes.

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

function trimBeforeSharedApiExport(source) {
  const at = source.indexOf(SHARED_API_EXPORT_MARKER);
  assert.ok(at >= 0, "core export marker located");
  return source.slice(0, at);
}

const FRAGMENTS = [
  "10-runtime-primitives.ts",
  "10-runtime-scene-utils.ts",
  "11-scene-math.ts",
  "12-scene-geometry.ts",
  "13-scene-material.ts",
  "14-scene-lighting.ts",
  "15-scene-draw-plan.ts",
  "16c-scene-shared-pbr.ts",
];

const harnessWindow = {};

function createContext() {
  const context = vm.createContext({ console, window: harnessWindow });
  for (const name of FRAGMENTS) {
    vm.runInContext(readBootstrapSource(name), context, { filename: name });
  }
  vm.runInContext(
    trimBeforeSharedApiExport(readBootstrapSource("10-runtime-scene-core.ts")),
    context, { filename: "10-runtime-scene-core.ts" });
  vm.runInContext(readBootstrapSource("15b-scene-planner.ts"), context,
    { filename: "15b-scene-planner.ts" });
  return vm.runInContext("({\n" +
    "  normalizeSceneObject,\n" +
    "  normalizeSceneInstancedMeshEntry,\n" +
    "  normalizeSceneMaterialRecord,\n" +
    "  sceneObjectMaterialProfile,\n" +
    "  sceneApplyNamedMaterialToObject,\n" +
    "  sceneMaterialRenderPass,\n" +
    "  sceneWorldObjectRenderPass,\n" +
    "  scenePlannerObjectRenderPass,\n" +
    "  scenePBRObjectRenderPass,\n" +
    "  createSceneRenderBundle,\n" +
    "  sceneResolveCSSBundle,\n" +
    "  sceneResolveCSSBundleWithContext,\n" +
    "  prepareScene,\n" +
    "  applySceneObjectPatch,\n" +
    "  sceneObjectFromPayload,\n" +
    "  registerSceneMaterialProfile,\n" +
    "  unregisterSceneMaterialProfile\n" +
    "})", context, { filename: "collect-api.js" });
}

const api = createContext();

// Minimal scripted CSS computed-style source: it only supplies property
// VALUES for var() resolution; all routing stays in production code.
const cssProps = { "--mask-cutoff": "0.5" };
harnessWindow.getComputedStyle = function () {
  return {
    getPropertyValue: function (name) {
      return Object.prototype.hasOwnProperty.call(cssProps, name)
        ? String(cssProps[name])
        : "";
    },
  };
};
const CSS_MOUNT = { id: "scene-css-mount" };
const VIEWPORT = { width: 64, height: 64 };

function cssMaskVertexObject(alphaCutoff, extra) {
  return api.normalizeSceneObject(Object.assign({
    id: "css-mask-quad",
    kind: "mesh",
    materialKind: "standard",
    opacity: 0.5,
    wireframe: false,
    alphaCutoff,
    vertices: {
      count: 3,
      positions: [0, 0, 0, 1, 0, 0, 0, 1, 0],
    },
  }, extra || {}), 0);
}

function buildCssMaskBundle(objects, instancedMeshes, rendererCapabilities) {
  return api.createSceneRenderBundle(
    VIEWPORT.width, VIEWPORT.height, "#000000",
    {},
    objects, [], [], [], [], {}, 0,
    [], instancedMeshes || [], [], [], [], 0, false, rendererCapabilities || null);
}

function prepareCssMask(bundle, lastPrepared, revision) {
  return api.prepareScene(bundle, null, VIEWPORT, lastPrepared || null, {
    mount: CSS_MOUNT,
    revision,
  });
}

function profileOf(value) {
  return api.sceneObjectMaterialProfile(value);
}

function snapshotOf(value) {
  return JSON.stringify(value);
}

test("default mask routing: object/instance/material at opacity .5 cutoff .5 are opaque", () => {
  const geometryInputs = [
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: 0.5 },
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: "0.5" },
    { materialKind: "standard", kind: "box", opacity: 0, alphaCutoff: 0 },
  ];
  const materialInputs = [
    { materialKind: "standard", kind: "standard", opacity: 0.5, alphaCutoff: 0.5 },
    { materialKind: "standard", kind: "standard", opacity: 0.5, alphaCutoff: "0.5" },
    { materialKind: "standard", kind: "standard", opacity: 0, alphaCutoff: 0 },
  ];
  for (let i = 0; i < geometryInputs.length; i += 1) {
    const geo = geometryInputs[i];
    const mat = materialInputs[i];
    const obj = api.normalizeSceneObject(geo, 0);
    assert.equal(obj.blendMode, "opaque", JSON.stringify(geo));
    assert.equal(obj.renderPass, "opaque", JSON.stringify(geo));
    assert.equal(profileOf(obj).blendMode, "opaque", JSON.stringify(geo));
    assert.equal(profileOf(obj).renderPass, "opaque", JSON.stringify(geo));
    const inst = api.normalizeSceneInstancedMeshEntry(geo, 0);
    assert.equal(inst.blendMode, "opaque", JSON.stringify(geo));
    assert.equal(inst.renderPass, "opaque", JSON.stringify(geo));
    assert.equal(profileOf(inst).blendMode, "opaque", JSON.stringify(geo));
    assert.equal(profileOf(inst).renderPass, "opaque", JSON.stringify(geo));
    const rec = api.normalizeSceneMaterialRecord(mat, 0);
    assert.equal(rec.blendMode, "opaque", JSON.stringify(mat));
    assert.equal(rec.renderPass, "opaque", JSON.stringify(mat));
    assert.equal(profileOf(rec).blendMode, "opaque", JSON.stringify(mat));
    assert.equal(profileOf(rec).renderPass, "opaque", JSON.stringify(mat));
  }
});

test("sceneObjectMaterialProfile preserves masking defaults", () => {
  const profile = profileOf({ opacity: 0.5, alphaCutoff: 0.5 });
  assert.equal(profile.alphaCutoff, 0.5);
  assert.equal(profile.opacity, 0.5);
  assert.equal(profile.blendMode, "opaque");
  assert.equal(profile.renderPass, "opaque");
});

test("named material apply then profile agree", () => {
  const rec = api.normalizeSceneMaterialRecord(
    { id: "m1", kind: "standard", opacity: 0.5, alphaCutoff: 0.5 }, 0);
  assert.equal(profileOf(rec).renderPass, "opaque");
  const applied = api.sceneApplyNamedMaterialToObject({ kind: "box" }, rec);
  assert.equal(profileOf(applied).renderPass, "opaque");
  assert.equal(profileOf(applied).alphaCutoff, 0.5);
});

test("fallback re-evaluation on mask toggle uses stored computed defaults (object, instance, material)", () => {
  // Object: fallback is a previously normalized object.
  const objBase = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: null }, 0);
  const objMasked = api.normalizeSceneObject({ alphaCutoff: 0.5 }, 0, objBase);
  assert.equal(objMasked.alphaCutoff, 0.5);
  assert.equal(profileOf(objMasked).renderPass, "opaque");
  const objDisabled = api.normalizeSceneObject({ alphaCutoff: null }, 0, objMasked);
  assert.equal(objDisabled.alphaCutoff, null);
  assert.equal(profileOf(objDisabled).renderPass, "alpha");

  // Instance: fallback is a previously normalized instanced mesh entry.
  const instBase = api.normalizeSceneInstancedMeshEntry(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: null }, 0);
  const instMasked = api.normalizeSceneInstancedMeshEntry({ alphaCutoff: 0.5 }, 0, instBase);
  assert.equal(instMasked.alphaCutoff, 0.5);
  assert.equal(profileOf(instMasked).renderPass, "opaque");
  const instDisabled = api.normalizeSceneInstancedMeshEntry({ alphaCutoff: null }, 0, instMasked);
  assert.equal(instDisabled.alphaCutoff, null);
  assert.equal(profileOf(instDisabled).renderPass, "alpha");

  // Material record: fallback is a previously normalized record.
  const recBase = api.normalizeSceneMaterialRecord(
    { kind: "standard", opacity: 0.5, alphaCutoff: null }, 0);
  const recMasked = api.normalizeSceneMaterialRecord({ alphaCutoff: 0.5 }, 0, recBase);
  assert.equal(recMasked.alphaCutoff, 0.5);
  assert.equal(profileOf(recMasked).renderPass, "opaque");
  const recDisabled = api.normalizeSceneMaterialRecord({ alphaCutoff: null }, 0, recMasked);
  assert.equal(recDisabled.alphaCutoff, null);
  assert.equal(profileOf(recDisabled).renderPass, "alpha");

  // No mutation of any previously normalized fallback or fresh input.
  assert.equal(snapshotOf(objBase),
    snapshotOf(api.normalizeSceneObject(
      { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: null }, 0)));
  assert.equal(snapshotOf(recBase),
    snapshotOf(api.normalizeSceneMaterialRecord(
      { kind: "standard", opacity: 0.5, alphaCutoff: null }, 0)));
  const input = { alphaCutoff: 0.5 };
  const inputBefore = snapshotOf(input);
  api.normalizeSceneObject(input, 0, objDisabled);
  assert.equal(snapshotOf(input), inputBefore);
});

test("explicit renderPass/blendMode survive mask toggles; renderPass wins", () => {
  // Explicit choices directly on normalizer output.
  const explicit = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      renderPass: "alpha", blendMode: "alpha" }, 0);
  assert.equal(profileOf(explicit).renderPass, "alpha");
  assert.equal(profileOf(explicit).blendMode, "alpha");
  const additive = profileOf(api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      renderPass: "additive" }, 0));
  assert.equal(additive.renderPass, "additive");
  const blendOnly = profileOf(api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "additive" }, 0));
  assert.equal(blendOnly.renderPass, "additive");
  const precedence = api.scenePBRObjectRenderPass(
    { renderPass: "additive" }, { renderPass: "alpha", opacity: 0.5, alphaCutoff: 0.5 });
  assert.equal(precedence, "additive");

  // Blend alias toggles through normalized fallbacks.
  const blendBase = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: null }, 0);
  const blendAlias = api.normalizeSceneObject({ blendMode: "add" }, 0, blendBase);
  assert.equal(profileOf(blendAlias).blendMode, "additive");
  const blendTransparent = api.normalizeSceneObject(
    { blendMode: "transparent" }, 0, blendAlias);
  assert.equal(profileOf(blendTransparent).blendMode, "alpha");
  const blendCleared = api.normalizeSceneObject({ blendMode: null }, 0, blendTransparent);
  assert.equal(profileOf(blendCleared).blendMode, "opaque");

  // RenderPass alias toggles through normalized fallbacks.
  const passBase = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      renderPass: null }, 0);
  const passAdd = api.normalizeSceneObject({ renderPass: "add" }, 0, passBase);
  assert.equal(profileOf(passAdd).renderPass, "additive");
  const passTransparent = api.normalizeSceneObject(
    { renderPass: "transparent" }, 0, passAdd);
  assert.equal(profileOf(passTransparent).renderPass, "alpha");
  const passCleared = api.normalizeSceneObject({ renderPass: null }, 0, passTransparent);
  assert.equal(profileOf(passCleared).renderPass, "opaque");

  // Object renderPass precedence survives a fallback toggle.
  const precedenceMasked = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      renderPass: "additive" }, 0,
    api.normalizeSceneObject(
      { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
        renderPass: null }, 0));
  assert.equal(profileOf(precedenceMasked).renderPass, "additive");
  assert.equal(api.scenePBRObjectRenderPass(
    precedenceMasked, profileOf(precedenceMasked)), "additive");
  // Explicit choices also survive actual alphaCutoff null/.5 toggles.
  const explicitMasked = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "alpha", renderPass: "alpha" }, 0);
  const explicitDisabled = api.normalizeSceneObject(
    { alphaCutoff: null }, 0, explicitMasked);
  assert.equal(profileOf(explicitDisabled).blendMode, "alpha");
  assert.equal(profileOf(explicitDisabled).renderPass, "alpha");
  const explicitRemasked = api.normalizeSceneObject(
    { alphaCutoff: 0.5 }, 0, explicitDisabled);
  assert.equal(profileOf(explicitRemasked).blendMode, "alpha");
  assert.equal(profileOf(explicitRemasked).renderPass, "alpha");
});

test("no mutation of inputs or fallbacks", () => {
  const fallback = { opacity: 0.5, alphaCutoff: null };
  const input = { alphaCutoff: 0.5 };
  const before = JSON.stringify([input, fallback]);
  api.normalizeSceneObject(input, 0, fallback);
  assert.equal(JSON.stringify([input, fallback]), before);
});

test("invalid cutoffs remain unmasked", () => {
  for (const cutoff of [null, false, "", NaN, "abc"]) {
    const profile = profileOf({ materialKind: "standard", opacity: 0.5, alphaCutoff: cutoff });
    assert.equal(profile.renderPass, "alpha", String(cutoff));
  }
});

test("unresolved CSS cutoff stays unmasked; substitution re-evaluates on a normalized object", () => {
  const obj = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: "var(--cut)" }, 0);
  assert.equal(obj.alphaCutoff, "var(--cut)");
  assert.equal(profileOf(obj).renderPass, "alpha");
  const resolved = api.normalizeSceneObject({ alphaCutoff: 0.5 }, 0, obj);
  assert.equal(resolved.alphaCutoff, 0.5);
  assert.equal(profileOf(resolved).renderPass, "opaque");
  const cloned = profileOf(Object.assign({}, obj, { alphaCutoff: 0.5 }));
  assert.equal(cloned.renderPass, "opaque");
  assert.equal(cloned.blendMode, "opaque");
});

test("authored custom shaders keep legacy alpha default", () => {
  const selena = profileOf(
    { shaderBackend: "selena", opacity: 0.5, alphaCutoff: 0.5 });
  assert.equal(selena.renderPass, "alpha");
  const wgsl = profileOf(
    { materialKind: "custom", customFragmentWGSL: "fn f() -> vec4f { return vec4f(1.0); }",
      opacity: 0.5, alphaCutoff: 0.5 });
  assert.equal(wgsl.renderPass, "alpha");
  const fragment = profileOf(
    { materialKind: "custom", customFragment: "return vec4(1.0);",
      opacity: 0.5, alphaCutoff: 0.5 });
  assert.equal(fragment.renderPass, "alpha");
});

test("raw no-pass materials route opaque through all pass selectors", () => {
  const mat = { opacity: 0.5, alphaCutoff: 0.5 };
  assert.equal(api.sceneMaterialRenderPass(mat), "opaque");
  assert.equal(api.sceneWorldObjectRenderPass({}, mat), "opaque");
  assert.equal(api.scenePlannerObjectRenderPass({}, mat), "opaque");
  assert.equal(api.scenePBRObjectRenderPass({}, mat), "opaque");
  // Legacy raw fallback thresholds preserved: near-one unmarked opacity
  // with no raw pass still routes alpha in the planner and PBR selectors
  // (planner/PBR use < 1, unlike the generic material < 0.999 split).
  assert.equal(api.scenePlannerObjectRenderPass({}, { opacity: 0.9995 }), "alpha");
  assert.equal(api.scenePBRObjectRenderPass({}, { opacity: 0.9995 }), "alpha");
  assert.equal(api.sceneWorldObjectRenderPass({ renderPass: "alpha" }, mat), "alpha");
  assert.equal(api.scenePlannerObjectRenderPass({ renderPass: "opaque" }, mat), "opaque");
  assert.equal(api.scenePBRObjectRenderPass(null,
    { renderPass: "additive", opacity: 0.5, alphaCutoff: 0.5 }), "additive");
});

test("registered profile defaults apply unmasked and unregister cleanly", () => {
  const kind = "patch-alpha-profile-test";
  const registered = api.registerSceneMaterialProfile(kind,
    { blendMode: "alpha", renderPass: "alpha", opacity: 0.5 });
  assert.ok(registered && typeof registered === "object");
  assert.equal(registered.kind, kind);
  assert.equal(registered.blendMode, "alpha");
  assert.equal(registered.renderPass, "alpha");
  try {
    const obj = api.normalizeSceneObject({ materialKind: kind }, 0);
    assert.equal(obj.materialKind, kind);
    assert.equal(profileOf(obj).renderPass, "alpha");
    assert.equal(profileOf(obj).blendMode, "alpha");
    const rec = api.normalizeSceneMaterialRecord({ kind }, 0);
    assert.equal(rec.kind, kind);
    assert.equal(profileOf(rec).renderPass, "alpha");
    assert.equal(profileOf(rec).blendMode, "alpha");
    assert.equal(profileOf({ opacity: 0.5 }).renderPass, "alpha");
    assert.equal(profileOf({ opacity: 1 }).renderPass, "opaque");
  } finally {
    assert.equal(api.unregisterSceneMaterialProfile(kind), true);
  }
  const after = api.normalizeSceneObject({ materialKind: kind }, 0);
  assert.equal(after.materialKind, "flat");
});

test("explicit alpha blend selects alpha routing for raw, normalized, instanced, and named records", () => {
  const obj = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "alpha" }, 0);
  assert.equal(obj.blendMode, "alpha");
  assert.equal(obj.renderPass, "alpha");
  assert.equal(profileOf(obj).blendMode, "alpha");
  assert.equal(profileOf(obj).renderPass, "alpha");
  const inst = api.normalizeSceneInstancedMeshEntry(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "alpha" }, 0);
  assert.equal(inst.blendMode, "alpha");
  assert.equal(inst.renderPass, "alpha");
  const rec = api.normalizeSceneMaterialRecord(
    { kind: "standard", opacity: 0.5, alphaCutoff: 0.5, blendMode: "alpha" }, 0);
  assert.equal(rec.blendMode, "alpha");
  assert.equal(rec.renderPass, "alpha");
});

test("raw authored pass/blend values survive profiling and re-normalization", () => {
  const profile = profileOf({ opacity: 0.5, alphaCutoff: 0.5, renderPass: "alpha" });
  assert.equal(profile.renderPass, "alpha");
  const next = api.normalizeSceneObject({}, 0, {
    materialKind: "standard", opacity: 1, blendMode: "alpha", renderPass: "alpha" });
  assert.equal(next.blendMode, "alpha");
  assert.equal(next.renderPass, "alpha");
  assert.equal(profileOf(next).blendMode, "alpha");
  assert.equal(profileOf(next).renderPass, "alpha");
});

test("nested custom/Selena shader sources keep alpha default; explicit empty clears", () => {
  const nested = api.normalizeSceneObject(
    { material: { kind: "custom", shaderBackend: "selena",
      opacity: 0.5, alphaCutoff: 0.5 } }, 0);
  assert.equal(profileOf(nested).blendMode, "alpha");
  assert.equal(profileOf(nested).renderPass, "alpha");
  const nestedWgsl = api.normalizeSceneObject(
    { material: { kind: "custom",
      customFragmentWGSL: "fn f() -> vec4f { return vec4f(1.0); }",
      opacity: 0.5, alphaCutoff: 0.5 } }, 0);
  assert.equal(profileOf(nestedWgsl).renderPass, "alpha");
  const shaderBase = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", shaderBackend: "selena",
      opacity: 0.5, alphaCutoff: 0.5 }, 0);
  assert.equal(profileOf(shaderBase).renderPass, "alpha");
  const cleared = api.normalizeSceneObject({ shaderBackend: "" }, 0, shaderBase);
  assert.equal(profileOf(cleared).blendMode, "opaque");
  assert.equal(profileOf(cleared).renderPass, "opaque");
});

test("raw explicit alpha/additive blend routes through planner and PBR selectors", () => {
  const alpha = { opacity: 0.5, alphaCutoff: 0.5, blendMode: "alpha" };
  assert.equal(api.sceneMaterialRenderPass(alpha), "alpha");
  assert.equal(api.scenePlannerObjectRenderPass({}, alpha), "alpha");
  assert.equal(api.scenePBRObjectRenderPass({}, alpha), "alpha");
  const additive = { opacity: 0.5, alphaCutoff: 0.5, blendMode: "additive" };
  assert.equal(api.scenePlannerObjectRenderPass({}, additive), "additive");
  assert.equal(api.scenePBRObjectRenderPass({}, additive), "additive");
});

test("named material apply derives route provenance from the value source", () => {
  const masked = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: 0.5 }, 0);
  assert.equal(profileOf(masked).renderPass, "opaque");
  const explicit = api.sceneApplyNamedMaterialToObject(masked,
    { kind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "alpha", renderPass: "alpha" });
  assert.equal(explicit.blendMode, "alpha");
  assert.equal(explicit.renderPass, "alpha");
  assert.equal(profileOf(explicit).blendMode, "alpha");
  assert.equal(profileOf(explicit).renderPass, "alpha");
  const plain = api.normalizeSceneMaterialRecord({ id: "m-plain", kind: "standard" }, 0);
  const inherited = api.sceneApplyNamedMaterialToObject(masked, plain);
  assert.equal(inherited.alphaCutoff, 0.5);
  assert.equal(profileOf(inherited).renderPass, "opaque");
});

test("derived provenance persists when a normalized value is re-normalized as item", () => {
  // Object: a clone of a derived object stays computed on mask enable.
  const objBase = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: null }, 0);
  assert.equal(objBase._blendModeDerived, true);
  assert.equal(objBase._renderPassDerived, true);
  const objEnabled = api.normalizeSceneObject(
    Object.assign({}, objBase, { alphaCutoff: 0.5 }), 0);
  assert.equal(objEnabled.alphaCutoff, 0.5);
  assert.equal(objEnabled.blendMode, "opaque");
  assert.equal(objEnabled.renderPass, "opaque");
  assert.equal(objEnabled._blendModeDerived, true);
  assert.equal(objEnabled._renderPassDerived, true);
  const objMasked = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: 0.5 }, 0);
  const objDisabled = api.normalizeSceneObject(
    Object.assign({}, objMasked, { alphaCutoff: null }), 0);
  assert.equal(objDisabled.alphaCutoff, null);
  assert.equal(objDisabled.renderPass, "alpha");
  assert.equal(objDisabled._renderPassDerived, true);
  // Raw unmarked explicit routes stay authored through clones.
  const raw = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "alpha", renderPass: "alpha" }, 0);
  assert.equal(raw._blendModeDerived, false);
  assert.equal(raw._renderPassDerived, false);
  const rawClone = api.normalizeSceneObject(Object.assign({}, raw), 0);
  assert.equal(rawClone.blendMode, "alpha");
  assert.equal(rawClone.renderPass, "alpha");
  assert.equal(rawClone._blendModeDerived, false);
  assert.equal(rawClone._renderPassDerived, false);
  // Instanced entries honor the marker on the item too.
  const instBase = api.normalizeSceneInstancedMeshEntry(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: null }, 0);
  const instEnabled = api.normalizeSceneInstancedMeshEntry(
    Object.assign({}, instBase, { alphaCutoff: 0.5 }), 0);
  assert.equal(instEnabled.renderPass, "opaque");
  assert.equal(instEnabled._blendModeDerived, true);
  assert.equal(instEnabled._renderPassDerived, true);
  // Named records honor the marker on the item too.
  const recBase = api.normalizeSceneMaterialRecord(
    { kind: "standard", opacity: 0.5, alphaCutoff: null }, 0);
  const recEnabled = api.normalizeSceneMaterialRecord(
    Object.assign({}, recBase, { alphaCutoff: 0.5 }), 0);
  assert.equal(recEnabled.renderPass, "opaque");
  assert.equal(recEnabled._blendModeDerived, true);
  assert.equal(recEnabled._renderPassDerived, true);
  const recRaw = api.normalizeSceneMaterialRecord(
    { kind: "standard", renderPass: "alpha" }, 0);
  assert.equal(recRaw._renderPassDerived, false);
  const recClone = api.normalizeSceneMaterialRecord(Object.assign({}, recRaw), 0);
  assert.equal(recClone.renderPass, "alpha");
  assert.equal(recClone._renderPassDerived, false);
});

test("shader authorship is per-field: partial clears keep retained fields authored", () => {
  const wgsl = "fn f() -> vec4f { return vec4f(1.0); }";
  const base = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: 0.5,
      shaderBackend: "selena", customFragmentWGSL: wgsl }, 0);
  assert.equal(profileOf(base).renderPass, "alpha");
  // Clearing the backend alone retains the WGSL shader: still authored.
  const clearedBackend = api.normalizeSceneObject({ shaderBackend: "" }, 0, base);
  assert.equal(clearedBackend.shaderBackend, "");
  assert.equal(clearedBackend.customFragmentWGSL, wgsl);
  assert.equal(profileOf(clearedBackend).blendMode, "alpha");
  assert.equal(profileOf(clearedBackend).renderPass, "alpha");
  // Clearing all actual shader fields permits masked opaque defaults.
  const clearedAll = api.normalizeSceneObject(
    { customFragmentWGSL: "" }, 0, clearedBackend);
  assert.equal(clearedAll.customFragmentWGSL, "");
  assert.equal(profileOf(clearedAll).renderPass, "opaque");
  // Non-string inputs inherit the current value instead of clearing.
  const numericBackend = api.normalizeSceneObject({ shaderBackend: 7 }, 0, base);
  assert.equal(numericBackend.shaderBackend, "selena");
  assert.equal(profileOf(numericBackend).renderPass, "alpha");
  // Nested material fields take precedence over top-level item fields.
  const nestedWins = api.normalizeSceneObject(
    { material: { shaderBackend: "selena" }, shaderBackend: "" }, 0, clearedAll);
  assert.equal(nestedWins.shaderBackend, "selena");
  assert.equal(profileOf(nestedWins).renderPass, "alpha");
  // Direct top-level shader values author when no nested source carries one.
  const direct = api.normalizeSceneObject(
    { customFragment: "return vec4(1.0);" }, 0, clearedAll);
  assert.equal(profileOf(direct).renderPass, "alpha");
  // Instanced entries use the same per-field semantics.
  const instBase = api.normalizeSceneInstancedMeshEntry(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      customFragmentWGSL: wgsl }, 0);
  assert.equal(profileOf(instBase).renderPass, "alpha");
  const instCleared = api.normalizeSceneInstancedMeshEntry(
    { customFragmentWGSL: "" }, 0, instBase);
  assert.equal(profileOf(instCleared).renderPass, "opaque");
  // Named records are direct-only: clearing the backend keeps the WGSL.
  const recBase = api.normalizeSceneMaterialRecord(
    { kind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      shaderBackend: "selena", customFragmentWGSL: wgsl }, 0);
  const recCleared = api.normalizeSceneMaterialRecord(
    { shaderBackend: "" }, 0, recBase);
  assert.equal(recCleared.customFragmentWGSL, wgsl);
  assert.equal(profileOf(recCleared).renderPass, "alpha");
});

test("real CSS substitution re-routes derived object passes through all selectors", () => {
  const obj = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5,
      alphaCutoff: "var(--mask-cutoff, 0.5)" }, 0);
  assert.equal(obj.alphaCutoff, "var(--mask-cutoff, 0.5)");
  assert.equal(obj.renderPass, "alpha");
  assert.equal(obj._renderPassDerived, true);
  // Freshly evaluated material routing wins over the cached derived pass.
  const resolved = Object.assign({}, obj, { alphaCutoff: 0.5 });
  const profile = profileOf(resolved);
  assert.equal(profile.renderPass, "opaque");
  assert.equal(api.sceneWorldObjectRenderPass(resolved, profile), "opaque");
  assert.equal(api.scenePlannerObjectRenderPass(resolved, profile), "opaque");
  assert.equal(api.scenePBRObjectRenderPass(resolved, profile), "opaque");
  // Mask toggles through the same cloned route.
  const disabled = Object.assign({}, obj, { alphaCutoff: null });
  const disabledProfile = profileOf(disabled);
  assert.equal(disabledProfile.renderPass, "alpha");
  assert.equal(api.sceneWorldObjectRenderPass(disabled, disabledProfile), "alpha");
  assert.equal(api.scenePlannerObjectRenderPass(disabled, disabledProfile), "alpha");
  assert.equal(api.scenePBRObjectRenderPass(disabled, disabledProfile), "alpha");
  // Raw (unmarked) and explicit object passes keep precedence.
  assert.equal(api.sceneWorldObjectRenderPass({ renderPass: "alpha" }, profile), "alpha");
  assert.equal(api.scenePlannerObjectRenderPass(
    { renderPass: "additive" }, profile), "additive");
  assert.equal(api.scenePBRObjectRenderPass(
    { renderPass: "opaque", _renderPassDerived: false }, profile), "opaque");
  // Named material application with an inherited cutoff uses the effective
  // profile/defaults rather than the stale material-derived object pass.
  const plain = api.normalizeSceneMaterialRecord(
    { id: "m-css", kind: "standard", opacity: 0.5 }, 0);
  const applied = api.sceneApplyNamedMaterialToObject(resolved, plain);
  assert.equal(applied.alphaCutoff, 0.5);
  assert.equal(applied._renderPassDerived, true);
  assert.equal(profileOf(applied).renderPass, "opaque");
});

test("routed marker provenance follows the exact value source", () => {
  // Stale top-level derived markers must not override an explicit nested
  // route (nested material is the route source, and it carries no marker).
  const base = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: 0.5 }, 0);
  assert.equal(base._blendModeDerived, true);
  const nestedExplicit = api.normalizeSceneObject(
    Object.assign({}, base,
      { material: { blendMode: "alpha", renderPass: "alpha" } }), 0);
  assert.equal(nestedExplicit.blendMode, "alpha");
  assert.equal(nestedExplicit.renderPass, "alpha");
  // Top-level explicit routes must not be ignored because an unrelated
  // nested object carries derived markers without any route value.
  const topLevelExplicit = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "alpha", renderPass: "alpha",
      material: { _blendModeDerived: true, _renderPassDerived: true } }, 0);
  assert.equal(topLevelExplicit.blendMode, "alpha");
  assert.equal(topLevelExplicit.renderPass, "alpha");
  // A nested derived marker wins only when the nested source supplies the
  // route value.
  const nestedDerived = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      material: { blendMode: "alpha", _blendModeDerived: true } }, 0);
  assert.equal(nestedDerived.blendMode, "opaque");
  assert.equal(nestedDerived.renderPass, "opaque");
  // Instanced entries follow the same nested-first provenance.
  const instBase = api.normalizeSceneInstancedMeshEntry(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5 }, 0);
  const instNested = api.normalizeSceneInstancedMeshEntry(
    Object.assign({}, instBase,
      { material: { blendMode: "alpha", renderPass: "alpha" } }), 0);
  assert.equal(instNested.blendMode, "alpha");
  assert.equal(instNested.renderPass, "alpha");
  const instTop = api.normalizeSceneInstancedMeshEntry(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "alpha", renderPass: "alpha",
      material: { _blendModeDerived: true, _renderPassDerived: true } }, 0);
  assert.equal(instTop.blendMode, "alpha");
  assert.equal(instTop.renderPass, "alpha");
  // Named material records are direct-only: nested markers never leak in.
  const rec = api.normalizeSceneMaterialRecord(
    { kind: "standard", opacity: 0.5, alphaCutoff: 0.5, blendMode: "alpha",
      material: { _blendModeDerived: true, _renderPassDerived: true } }, 0);
  assert.equal(rec.blendMode, "alpha");
  assert.equal(rec.renderPass, "alpha");
  const recDerived = api.normalizeSceneMaterialRecord(
    { kind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      _blendModeDerived: true, _renderPassDerived: true,
      material: { blendMode: "alpha", renderPass: "alpha" } }, 0);
  assert.equal(recDerived.blendMode, "opaque");
  assert.equal(recDerived.renderPass, "opaque");
});

test("instanced clearing routes on the effective flattened shader fields", () => {
  const base = api.normalizeSceneInstancedMeshEntry(
    { material: { kind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      shaderBackend: "selena" } }, 0);
  assert.equal(profileOf(base).renderPass, "alpha");
  // Clearing the nested-origin backend via a direct empty field yields an
  // opaque route even though the nested source material still retains it.
  const clear = api.normalizeSceneInstancedMeshEntry({ shaderBackend: "" }, 0, base);
  assert.equal(clear.renderPass, "opaque");
  assert.equal(clear.shaderBackend, "");
  assert.equal(clear.material && clear.material.shaderBackend, "selena");
  const clearProfile = profileOf(clear);
  assert.equal(clearProfile.blendMode, "opaque");
  assert.equal(clearProfile.renderPass, "opaque");
  assert.equal(clearProfile.shaderBackend, "");
  // A retained effective fragment field after clearing the backend stays
  // authored: alpha routing.
  const frag = api.normalizeSceneInstancedMeshEntry(
    { customFragment: "return vec4(1.0);" }, 0, clear);
  assert.equal(frag.customFragment, "return vec4(1.0);");
  const fragProfile = profileOf(frag);
  assert.equal(fragProfile.blendMode, "alpha");
  assert.equal(fragProfile.renderPass, "alpha");
  // Clearing that field too permits masked opaque defaults again.
  const clearAll = api.normalizeSceneInstancedMeshEntry({ customFragment: "" }, 0, frag);
  assert.equal(clearAll.customFragment, "");
  assert.equal(clearAll.renderPass, "opaque");
  const clearAllProfile = profileOf(clearAll);
  assert.equal(clearAllProfile.blendMode, "opaque");
  assert.equal(clearAllProfile.renderPass, "opaque");
});

test("blend alias routing survives nested undefined blendMode blocks", () => {
  // A nested derived blend alias overrides a blocked (own undefined)
  // top-level blendMode.
  const nestedAlias = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "additive",
      material: { blendMode: undefined, blend: "alpha", _blendModeDerived: true } }, 0);
  assert.equal(nestedAlias.blendMode, "opaque");
  assert.equal(nestedAlias._blendModeDerived, true);
  // A nested own undefined blendMode blocks the top-level blendMode, which
  // then falls through to the aliases; the selected alias is derived, so the
  // resolved mode is opaque.
  const topLevelAlias = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      blendMode: "additive", blend: "alpha", _blendModeDerived: true,
      material: { blendMode: undefined } }, 0);
  assert.equal(topLevelAlias.blendMode, "opaque");
  assert.equal(topLevelAlias._blendModeDerived, true);
  // Raw authored alias control: no derived marker, stays an explicit alpha.
  const rawAlias = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5,
      material: { blend: "alpha" } }, 0);
  assert.equal(rawAlias.blendMode, "alpha");
  assert.equal(rawAlias._blendModeDerived, false);
});

test("real bundle flow: CSS-resolved derived mask material routes opaque everywhere", () => {
  const obj = cssMaskVertexObject("var(--mask-cutoff, 0.5)");
  assert.equal(obj.alphaCutoff, "var(--mask-cutoff, 0.5)");
  assert.equal(obj.renderPass, "alpha");
  assert.equal(obj._renderPassDerived, true);
  const bundle = buildCssMaskBundle([obj]);
  const material = bundle.materials[0];
  // Pre-resolution: raw CSS string preserved, cached derived alpha route.
  assert.equal(material.alphaCutoff, "var(--mask-cutoff, 0.5)");
  assert.equal(material.renderPass, "alpha");
  assert.equal(material._renderPassDerived, true);
  assert.equal(bundle.meshObjects[0]._renderPassDerived, true);
  assert.equal(bundle.meshObjects[0].renderPass, "alpha");
  assert.equal(api.scenePlannerObjectRenderPass(bundle.meshObjects[0], material), "alpha");
  const prepared = prepareCssMask(bundle, null, 1);
  // Numeric resolved cutoff and freshly evaluated routing.
  assert.equal(prepared.ir.materials[0].alphaCutoff, 0.5);
  assert.equal(api.sceneMaterialRenderPass(prepared.ir.materials[0]), "opaque");
  const record = prepared.ir.meshObjects[0];
  assert.equal(record._renderPassDerived, true);
  assert.equal(api.sceneWorldObjectRenderPass(record, prepared.ir.materials[0]), "opaque");
  assert.equal(api.scenePlannerObjectRenderPass(record, prepared.ir.materials[0]), "opaque");
  assert.equal(api.scenePBRObjectRenderPass(record, prepared.ir.materials[0]), "opaque");
  assert.ok(prepared.pbrPasses.opaque.indexOf(record) !== -1);
  assert.equal(prepared.pbrPasses.alpha.length, 0);
  assert.equal(prepared.pbrPasses.additive.length, 0);
});

test("real bundle flow: authored explicit alpha survives CSS cutoff resolution", () => {
  const obj = cssMaskVertexObject("var(--mask-cutoff, 0.5)",
    { blendMode: "alpha", renderPass: "alpha" });
  assert.equal(obj._renderPassDerived, false);
  const bundle = buildCssMaskBundle([obj]);
  assert.equal(bundle.materials[0]._renderPassDerived, false);
  assert.equal(bundle.meshObjects[0]._renderPassDerived, false);
  assert.equal(bundle.meshObjects[0].renderPass, "alpha");
  const prepared = prepareCssMask(bundle, null, 1);
  assert.equal(prepared.ir.materials[0].alphaCutoff, 0.5);
  assert.equal(prepared.ir.materials[0].renderPass, "alpha");
  assert.equal(api.sceneMaterialRenderPass(prepared.ir.materials[0]), "alpha");
  assert.ok(prepared.pbrPasses.alpha.indexOf(prepared.ir.meshObjects[0]) !== -1);
  assert.equal(prepared.pbrPasses.opaque.length, 0);
});

test("real bundle flow: CSS cache reuse and revision flips cannot restore stale routes", () => {
  const build = () => buildCssMaskBundle(
    [cssMaskVertexObject("var(--mask-cutoff, 0.5)")]);
  const first = prepareCssMask(build(), null, 1);
  assert.equal(first.ir.materials[0].alphaCutoff, 0.5);
  assert.equal(first.pbrPasses.alpha.length, 0);
  assert.ok(first.pbrPasses.opaque.length > 0);
  // Same revision: cached CSS resolution replays the same numeric
  // substitution and derived records re-route opaque.
  const cached = prepareCssMask(build(), first, 1);
  assert.equal(cached.ir.materials[0].alphaCutoff, 0.5);
  assert.equal(api.scenePlannerObjectRenderPass(
    cached.ir.meshObjects[0], cached.ir.materials[0]), "opaque");
  assert.equal(cached.pbrPasses.alpha.length, 0);
  assert.ok(cached.pbrPasses.opaque.length > 0);
  // Revision bump with the var resolving to junk unmaskes the derived
  // route back to alpha.
  cssProps["--mask-cutoff"] = "none";
  try {
    const flipped = prepareCssMask(build(), cached, 2);
    assert.equal(flipped.ir.materials[0].alphaCutoff, "none");
    assert.equal(flipped.pbrPasses.opaque.length, 0);
    assert.equal(flipped.pbrPasses.alpha.length, 1);
    // An otherwise-identical authored explicit alpha record under the same
    // revision keeps its authored route.
    const explicit = prepareCssMask(buildCssMaskBundle(
      [cssMaskVertexObject("var(--mask-cutoff, 0.5)",
        { id: "css-mask-explicit", blendMode: "alpha", renderPass: "alpha" })]),
      flipped, 2);
    assert.equal(explicit.pbrPasses.alpha.length, 1);
    assert.equal(explicit.pbrPasses.opaque.length, 0);
  } finally {
    cssProps["--mask-cutoff"] = "0.5";
  }
});

test("derived and authored otherwise-identical profiles keep distinct material identity", () => {
  const derived = api.sceneObjectMaterialProfile(cssMaskVertexObject(0.5));
  const authoredOpaque = api.sceneObjectMaterialProfile(
    cssMaskVertexObject(0.5, { blendMode: "opaque", renderPass: "opaque" }));
  assert.equal(derived.renderPass, "opaque");
  assert.equal(authoredOpaque.renderPass, "opaque");
  assert.equal(derived._renderPassDerived, true);
  assert.equal(authoredOpaque._renderPassDerived, false);
  assert.notEqual(derived.key, authoredOpaque.key);
  const derivedAlpha = api.sceneObjectMaterialProfile(
    cssMaskVertexObject("var(--mask-cutoff, 0.5)"));
  const authoredAlpha = api.sceneObjectMaterialProfile(
    cssMaskVertexObject("var(--mask-cutoff, 0.5)",
      { blendMode: "alpha", renderPass: "alpha" }));
  assert.equal(derivedAlpha.renderPass, "alpha");
  assert.equal(authoredAlpha.renderPass, "alpha");
  assert.notEqual(derivedAlpha.key, authoredAlpha.key);
});

test("raw CSS cutoff strings and authored values survive the real bundle handoff", () => {
  const input = { materialKind: "standard", kind: "mesh", id: "raw-keep",
    opacity: 0.5, alphaCutoff: "var(--mask-cutoff, 0.5)",
    vertices: { count: 3, positions: [0, 0, 0, 1, 0, 0, 0, 1, 0] } };
  const before = JSON.stringify(input);
  const obj = api.normalizeSceneObject(input, 0);
  assert.equal(JSON.stringify(input), before);
  assert.equal(obj.alphaCutoff, "var(--mask-cutoff, 0.5)");
  const bundle = buildCssMaskBundle([obj]);
  assert.equal(bundle.materials[0].alphaCutoff, "var(--mask-cutoff, 0.5)");
  assert.equal(bundle.materials[0].color, "#8de1ff");
});

test("instanced bundle entries preserve derived pass provenance from source", () => {
  const inst = api.normalizeSceneInstancedMeshEntry(
    { materialKind: "standard", opacity: 0.5,
      alphaCutoff: "var(--mask-cutoff, 0.5)" }, 0);
  assert.equal(inst._renderPassDerived, true);
  const bundle = buildCssMaskBundle([], [inst]);
  assert.equal(bundle.instancedMeshes[0]._renderPassDerived, true);
  assert.equal(bundle.instancedMeshes[0].renderPass, "alpha");
  assert.equal(bundle.instancedMeshes[0].materialIndex, 0);
  assert.equal(bundle.materials[0]._renderPassDerived, true);
});

test("raw no-route profiles convey derived defaults; recognized raw routes stay authored", () => {
  // Raw profile with no route value at all: computed defaults, not authored.
  // Before real CSS resolution the var() cutoff is derived alpha; both
  // markers are true and routing flips to opaque only after resolution.
  const derivedRaw = profileOf({ materialKind: "standard", opacity: 0.5,
    alphaCutoff: "var(--mask-cutoff, 0.5)" });
  assert.equal(derivedRaw.blendMode, "alpha");
  assert.equal(derivedRaw.renderPass, "alpha");
  assert.equal(derivedRaw._blendModeDerived, true);
  assert.equal(derivedRaw._renderPassDerived, true);
  // Recognized raw explicit route values remain authored.
  const authoredRaw = profileOf({ materialKind: "standard", opacity: 0.5,
    alphaCutoff: "var(--mask-cutoff, 0.5)",
    blendMode: "alpha", renderPass: "alpha" });
  assert.equal(authoredRaw.blendMode, "alpha");
  assert.equal(authoredRaw.renderPass, "alpha");
  assert.equal(authoredRaw._blendModeDerived, false);
  assert.equal(authoredRaw._renderPassDerived, false);
  assert.notEqual(derivedRaw.key, authoredRaw.key);
  // Raw profile aliases recognized by the shared profile alias maps stay
  // authored exactly like canonical route values on a masked material.
  const aliasRaw = profileOf({ materialKind: "standard", opacity: 0.5,
    alphaCutoff: "var(--mask-cutoff, 0.5)",
    blendMode: "transparent", renderPass: "transparent" });
  assert.equal(aliasRaw.blendMode, "alpha");
  assert.equal(aliasRaw.renderPass, "alpha");
  assert.equal(aliasRaw._blendModeDerived, false);
  assert.equal(aliasRaw._renderPassDerived, false);
  assert.notEqual(derivedRaw.key, aliasRaw.key);
  // Otherwise-identical authored-vs-derived key split on plain defaults.
  const derivedDefaults = profileOf({ opacity: 0.5, alphaCutoff: 0.5 });
  const authoredDefaults = profileOf({ opacity: 0.5, alphaCutoff: 0.5,
    blendMode: "opaque", renderPass: "opaque" });
  assert.equal(derivedDefaults._renderPassDerived, true);
  assert.equal(authoredDefaults._renderPassDerived, false);
  assert.notEqual(derivedDefaults.key, authoredDefaults.key);
});

test("retained-geometry records propagate derived object passes", () => {
  const obj = api.normalizeSceneObject({
    id: "retained-pass", kind: "mesh", materialKind: "standard",
    opacity: 0.5, alphaCutoff: "var(--mask-cutoff, 0.5)", wireframe: false,
    vertices: {
      count: 3, immutable: true, revision: 1,
      positions: [0, 0, 0, 1, 0, 0, 0, 1, 0],
      normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
      uvs: [0, 0, 1, 0, 0, 1],
      tangents: [0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1],
      indices: [0, 1, 2],
    },
  }, 0);
  assert.equal(obj.renderPass, "alpha");
  assert.equal(obj._renderPassDerived, true);
  const bundle = buildCssMaskBundle([obj], null, { retainedGeometry: true });
  const prepared = prepareCssMask(bundle, null, 1);
  const record = prepared.ir.meshObjects[0];
  assert.ok(record);
  assert.equal(record.retainedGeometry, true);
  assert.equal(record._renderPassDerived, true);
  assert.equal(api.scenePBRObjectRenderPass(record, prepared.ir.materials[0]), "opaque");
  assert.ok(prepared.pbrPasses.opaque.indexOf(record) !== -1);
  assert.equal(prepared.pbrPasses.alpha.length, 0);
});

test("same-revision prepareScene invalidates cached routes after authored override", () => {
  const firstBundle = buildCssMaskBundle(
    [cssMaskVertexObject("var(--mask-cutoff, 0.5)")]);
  const first = prepareCssMask(firstBundle, null, 1);
  assert.equal(first.pbrPasses.opaque.length, 1);
  assert.equal(first.pbrPasses.alpha.length, 0);
  // Same ID, same material values, same revision, same pre-resolution
  // "alpha" pass text; only the bundled record's derived marker flips, so
  // the cached route signature must invalidate to alpha.
  const secondBundle = buildCssMaskBundle(
    [cssMaskVertexObject("var(--mask-cutoff, 0.5)")]);
  assert.equal(secondBundle.meshObjects[0].id, firstBundle.meshObjects[0].id);
  secondBundle.meshObjects[0]._renderPassDerived = false;
  const second = prepareCssMask(secondBundle, first, 1);
  assert.equal(second.pbrPasses.alpha.length, 1);
  assert.equal(second.pbrPasses.opaque.length, 0);
});

function mkPatchState(obj) {
  return {
    objects: new Map([["x", obj]]),
    labels: new Map(),
    sprites: new Map(),
    html: new Map(),
  };
}

test("applySceneObjectPatch: explicit route over derived default stays authored", () => {
  const base = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: 0.5 }, 0);
  assert.equal(base.renderPass, "opaque");
  assert.equal(base._renderPassDerived, true);
  const baseBefore = snapshotOf(base);
  const state = mkPatchState(base);
  const patch = { renderPass: "alpha" };
  const patchBefore = JSON.stringify(patch);
  api.applySceneObjectPatch(state, "x", patch);
  assert.equal(JSON.stringify(patch), patchBefore);
  assert.equal(snapshotOf(base), baseBefore);
  let obj = state.objects.get("x");
  assert.equal(obj.renderPass, "alpha");
  assert.equal(obj._renderPassDerived, false);
  // Authored alpha survives cutoff null/.5 toggles.
  api.applySceneObjectPatch(state, "x", { alphaCutoff: null });
  obj = state.objects.get("x");
  assert.equal(obj.renderPass, "alpha");
  assert.equal(obj._renderPassDerived, false);
  api.applySceneObjectPatch(state, "x", { alphaCutoff: 0.5 });
  obj = state.objects.get("x");
  assert.equal(obj.renderPass, "alpha");
  assert.equal(obj._renderPassDerived, false);
  assert.equal(api.scenePBRObjectRenderPass(obj, profileOf(obj)), "alpha");
});

test("applySceneObjectPatch: explicit alpha over an already-alpha derived default", () => {
  const base = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: null }, 0);
  assert.equal(base.renderPass, "alpha");
  assert.equal(base._renderPassDerived, true);
  const state = mkPatchState(base);
  api.applySceneObjectPatch(state, "x", { renderPass: "alpha" });
  let obj = state.objects.get("x");
  assert.equal(obj.renderPass, "alpha");
  assert.equal(obj._renderPassDerived, false);
  // Enabling the mask keeps the authored alpha route.
  api.applySceneObjectPatch(state, "x", { alphaCutoff: 0.5 });
  obj = state.objects.get("x");
  assert.equal(obj.renderPass, "alpha");
  assert.equal(obj._renderPassDerived, false);
});

test("applySceneObjectPatch: unrelated patches keep derived routing; clear restores default", () => {
  const base = api.normalizeSceneObject(
    { materialKind: "standard", kind: "box", opacity: 0.5, alphaCutoff: null }, 0);
  assert.equal(base.renderPass, "alpha");
  assert.equal(base._renderPassDerived, true);
  const state = mkPatchState(base);
  // Unrelated transform patch retains derived markers and re-derives.
  api.applySceneObjectPatch(state, "x", { x: 1 });
  let obj = state.objects.get("x");
  assert.equal(obj.x, 1);
  assert.equal(obj._renderPassDerived, true);
  assert.equal(obj.renderPass, "alpha");
  // Cutoff-only patch re-evaluates the derived default.
  api.applySceneObjectPatch(state, "x", { alphaCutoff: 0.5 });
  obj = state.objects.get("x");
  assert.equal(obj._renderPassDerived, true);
  assert.equal(obj.renderPass, "opaque");
  // Blend alias patch stays authored; explicit clear restores the default.
  api.applySceneObjectPatch(state, "x", { blendMode: "add" });
  obj = state.objects.get("x");
  assert.equal(obj.blendMode, "additive");
  assert.equal(obj._blendModeDerived, false);
  api.applySceneObjectPatch(state, "x", { blendMode: null });
  obj = state.objects.get("x");
  assert.equal(obj.blendMode, "opaque");
  // Explicit renderPass clear re-derives the masked opaque default.
  api.applySceneObjectPatch(state, "x", { renderPass: null });
  obj = state.objects.get("x");
  assert.equal(obj.renderPass, "opaque");
  assert.equal(obj._renderPassDerived, true);
});

test("sceneObjectFromPayload: authored routes, nested sources, and retained metadata", () => {
  const base = api.normalizeSceneObject(
    { materialKind: "standard", opacity: 0.5, alphaCutoff: 0.5 }, 0);
  // Direct payload route is authored even over a stale derived marker.
  const direct = api.sceneObjectFromPayload("x", { props: { renderPass: "alpha" } }, base);
  assert.equal(direct.renderPass, "alpha");
  assert.equal(direct._renderPassDerived, false);
  // Same text as the previously derived value still becomes authored.
  const same = api.sceneObjectFromPayload("x", { props: { renderPass: "opaque" } }, base);
  assert.equal(same.renderPass, "opaque");
  assert.equal(same._renderPassDerived, false);
  // Nested authored route beats the stale top-level derived marker.
  const nested = api.sceneObjectFromPayload("x",
    { props: { material: { blendMode: "alpha", renderPass: "alpha" } } }, base);
  assert.equal(nested.blendMode, "alpha");
  assert.equal(nested.renderPass, "alpha");
  assert.equal(nested._renderPassDerived, false);
  assert.equal(nested._blendModeDerived, false);
  // Already-normalized props keep their own derived metadata.
  const derivedProps = Object.assign({}, base);
  assert.equal(derivedProps._renderPassDerived, true);
  const kept = api.sceneObjectFromPayload("x", { props: derivedProps }, base);
  assert.equal(kept.renderPass, "opaque");
  assert.equal(kept._renderPassDerived, true);
});
