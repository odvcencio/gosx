"use strict";

// Typed AlphaCutoff fixture test: the real Go scene.Props -> SceneIR
// pipeline emits the fixture (alpha-mask-typed-fixture), and the real
// production VM fragments lower and diff it. Same VM harness contract as
// scene3d-alpha-mask-routing.test.js — no reimplementation, no fakes.
// Every expectation below is an authored constant from the fixture
// contract; nothing is derived from implementation output.

const { test } = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const REPO_ROOT = path.join(__dirname, "..", "..");

// Authored fixture contract constants.
const FIXTURE_SCHEMA = "gosx.alpha-cutoff.fixture.v1";
const FIXTURE_SCENE_KEYS = ["disabled", "discard", "mask", "opaque", "zero"];
const FIXTURE_TRANSITION_NAMES =
  ["absent-to-zero", "mask-to-absent", "mask-to-disabled"];
const OBJECT_ID = "typed-mask";

// The Go fixture is the only source of scene data. A missing or invalid
// fixture is fatal: no test runs without it.
const fixture = JSON.parse(execFileSync(
  "go",
  ["run", "./client/js/testdata/alpha-mask-typed-fixture"],
  { cwd: REPO_ROOT, encoding: "utf8", timeout: 60000 }));

// Authored per-scene constants: exact raw opacity, exact raw alphaCutoff
// presence/value, exact NORMALIZED cutoff, and the exact authored render
// pass. These are contract constants, never derived from runtime values.
// Note: discard routes opaque (the cutoff changes pixels, NOT routing).
const SCENES = {
  opaque: {
    opacity: 1,
    rawCutoff: { present: false },
    normalizedCutoff: null,
    pass: "opaque",
  },
  mask: {
    opacity: 0.5,
    rawCutoff: { present: true, value: 0.5 },
    normalizedCutoff: 0.5,
    pass: "opaque",
  },
  zero: {
    opacity: 0,
    rawCutoff: { present: true, value: 0 },
    normalizedCutoff: 0,
    pass: "opaque",
  },
  disabled: {
    opacity: 0.5,
    rawCutoff: { present: true, value: null },
    normalizedCutoff: null,
    pass: "alpha",
  },
  discard: {
    opacity: 1,
    rawCutoff: { present: true, value: 2 },
    normalizedCutoff: 2,
    pass: "opaque",
  },
};

// Authored per-transition constants for the FINAL lowered object.
const TRANSITIONS = {
  "absent-to-zero": { opacity: 0, cutoff: 0, pass: "opaque" },
  "mask-to-disabled": { opacity: 0.5, cutoff: null, pass: "alpha" },
  "mask-to-absent": { opacity: 1, cutoff: null, pass: "opaque" },
};

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
    "  sceneObjectMaterialProfile,\n" +
    "  sceneMaterialRenderPass,\n" +
    "  applySceneCommands\n" +
    "})", context, { filename: "collect-api.js" });
}

const api = createContext();

test("fixture shape: exact schema, exact five scene keys, exact three transition names", () => {
  assert.equal(typeof fixture, "object");
  assert.notEqual(fixture, null);
  assert.equal(fixture.schema, FIXTURE_SCHEMA);
  assert.deepEqual(Object.keys(fixture.scenes).sort(), FIXTURE_SCENE_KEYS);
  assert.ok(Array.isArray(fixture.transitions), "transitions array present");
  assert.deepEqual(fixture.transitions.map((t) => t.name).sort(),
    FIXTURE_TRANSITION_NAMES);
});

test("every fixture scene ships exactly one shared typed-mask object", () => {
  for (const name of FIXTURE_SCENE_KEYS) {
    const scene = fixture.scenes[name];
    assert.ok(Array.isArray(scene.objects), name + ": objects array present");
    assert.equal(scene.objects.length, 1, name + ": exactly one object");
    assert.equal(scene.objects[0].id, OBJECT_ID, name + ": object id");
  }
});

// ---- RAW Go JSON assertions (presence AND value, kept separate from the
// normalized-object assertions below).

function assertRawOpacity(raw, expected, label) {
  assert.ok(Object.prototype.hasOwnProperty.call(raw, "opacity"),
    label + ": raw opacity key present");
  assert.strictEqual(typeof raw.opacity, "number",
    label + ": raw opacity strictly numeric");
  assert.strictEqual(raw.opacity, expected, label + ": exact raw opacity");
}

function assertRawCutoff(raw, spec, label) {
  const has = Object.prototype.hasOwnProperty.call(raw, "alphaCutoff");
  assert.strictEqual(has, spec.present,
    label + ": raw alphaCutoff presence exactly " + spec.present);
  if (!spec.present) {
    // Absent: no key AND no value.
    assert.strictEqual(raw.alphaCutoff, undefined,
      label + ": raw alphaCutoff undefined when absent");
  } else if (spec.value === null) {
    // Explicit null: key present AND strict null.
    assert.strictEqual(raw.alphaCutoff, null,
      label + ": raw alphaCutoff exactly null");
  } else {
    // Number: key present, type number, exact value.
    assert.strictEqual(typeof raw.alphaCutoff, "number",
      label + ": raw alphaCutoff strictly numeric");
    assert.strictEqual(raw.alphaCutoff, spec.value,
      label + ": exact raw alphaCutoff " + spec.value);
  }
}

// ---- NORMALIZED object assertions.

function assertNormalizedOpacity(normalized, expected, label) {
  assert.strictEqual(typeof normalized.opacity, "number",
    label + ": normalized opacity strictly numeric");
  assert.strictEqual(normalized.opacity, expected,
    label + ": exact normalized opacity");
}

function assertNormalizedCutoff(normalized, expected, label) {
  if (expected === null) {
    // Missing and disabled BOTH normalize to strict null (never undefined).
    assert.notStrictEqual(normalized.alphaCutoff, undefined,
      label + ": normalized alphaCutoff is null, not undefined");
    assert.strictEqual(normalized.alphaCutoff, null,
      label + ": normalized alphaCutoff exactly null");
  } else {
    assert.notStrictEqual(normalized.alphaCutoff, undefined,
      label + ": normalized alphaCutoff present");
    assert.notStrictEqual(normalized.alphaCutoff, null,
      label + ": normalized alphaCutoff not null");
    assert.strictEqual(typeof normalized.alphaCutoff, "number",
      label + ": normalized alphaCutoff strictly numeric");
    assert.strictEqual(normalized.alphaCutoff, expected,
      label + ": exact normalized alphaCutoff " + expected);
  }
}

// sceneMaterialRenderPass is always consulted through the material profile,
// never on the raw normalized object.
function profilePass(normalized) {
  return api.sceneMaterialRenderPass(api.sceneObjectMaterialProfile(normalized));
}

for (const name of FIXTURE_SCENE_KEYS) {
  const spec = SCENES[name];
  assert.ok(spec, "authored expectation exists for scene " + name);
  test("scene " + name + ": raw Go JSON opacity/cutoff match authored constants", () => {
    const raw = fixture.scenes[name].objects[0];
    assertRawOpacity(raw, spec.opacity, name);
    assertRawCutoff(raw, spec.rawCutoff, name);
  });
  test("scene " + name + ": normalized object and pass match authored constants", () => {
    const raw = fixture.scenes[name].objects[0];
    const normalized = api.normalizeSceneObject(raw, 0);
    assertNormalizedOpacity(normalized, spec.opacity, name);
    assertNormalizedCutoff(normalized, spec.normalizedCutoff, name);
    assert.strictEqual(profilePass(normalized), spec.pass,
      name + ": pass is exactly " + spec.pass);
  });
}

// Map-backed scene state seeded with the transition's own initial object
// (transition.initial.objects[0]) plus the empty label/sprite/html/light
// maps the production command applier expects.
function newState(initialNormalized) {
  return {
    objects: new Map([[String(initialNormalized.id), initialNormalized]]),
    labels: new Map(),
    sprites: new Map(),
    html: new Map(),
    lights: new Map(),
  };
}

function loweredFinalObject(transition) {
  const spec = TRANSITIONS[transition.name];
  assert.ok(spec, "authored expectation exists for " + transition.name);
  const initial =
    api.normalizeSceneObject(transition.initial.objects[0], 0);
  const state = newState(initial);
  assert.ok(Array.isArray(transition.commands) && transition.commands.length > 0,
    transition.name + ": commands nonempty");
  const result = api.applySceneCommands(state, transition.commands);
  assert.ok(!(result && typeof result.then === "function"),
    transition.name + ": diff commands apply synchronously");
  assert.equal(state.objects.size, 1,
    transition.name + ": exactly one object after commands");
  return state.objects.values().next().value;
}

for (const transition of fixture.transitions) {
  test("transition " + transition.name + " lowers to authored constants via applySceneCommands", () => {
    const spec = TRANSITIONS[transition.name];
    const final = loweredFinalObject(transition);
    assertNormalizedOpacity(final, spec.opacity, transition.name);
    assertNormalizedCutoff(final, spec.cutoff, transition.name);
    assert.strictEqual(profilePass(final), spec.pass,
      transition.name + ": final pass is exactly " + spec.pass);
  });
}
