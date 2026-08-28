// Scene3D node-matrix tests — authored glTF node.matrix through animation
// traversal.
//
// sceneAnimBuildNodeTransforms used to rebuild every local transform from
// TRS, so a legal glTF node carrying an authored 4x4 matrix (with no
// animated TRS) was silently reset to identity defaults, dropping its
// transform through parent chains during morph playback. A weights-only
// morph pose is not a TRS override, so the authored matrix must survive,
// while explicit TRS animation keeps overriding node TRS, and repeated
// rebuilds must never alias or mutate the source matrix.
//
// The fragment declares plain top-level functions, so running it in a VM
// context publishes them as context globals. 11-scene-math.ts supplies the
// matrix helpers the fragment expects.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  return fs.readFileSync(name.startsWith("../") ? path.join(__dirname, name) : path.join(srcDir, name), "utf8");
}

function createMixerContext() {
  const sandbox = {
    console: { warn: () => {}, error: () => {}, log: () => {} },
    Math,
    JSON,
    Number,
    Object,
    Array,
    Map,
    Set,
    Float32Array,
    Float64Array,
    isFinite,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  const context = vm.createContext(sandbox);
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readSource("../runtime/scene3d/animation.ts"), context, { filename: "animation.ts" });
  return { context, sandbox };
}

function run(context, expression) {
  return JSON.parse(JSON.stringify(vm.runInContext(expression, context)));
}

test("authored node matrix survives a weights-only morph pose", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    const node = { matrix: [2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 3, 5, 7, 1] };
    const pose = new Map([[0, { weights: [0.5] }]]);
    const map = sceneAnimBuildNodeTransforms([node], pose, null, [0]);
    const m = map.get(0);
    return {
      translation: Array.from(m.slice(12, 15)),
      scale: [m[0], m[5], m[10]],
      bottomRight: m[15],
      isFreshFloat32Array: Object.prototype.toString.call(m) === "[object Float32Array]",
    };
  })()`);
  assert.deepEqual(out.translation, [3, 5, 7]);
  assert.deepEqual(out.scale, [2, 2, 2]);
  assert.equal(out.bottomRight, 1);
  assert.equal(out.isFreshFloat32Array, true);
});

test("parent authored matrix carries child TRS through the hierarchy", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    const parent = { matrix: [2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 10, 0, -4, 1], children: [1] };
    const child = { translation: [0, 2, 0] };
    const pose = new Map([[0, { weights: [0.25] }]]);
    const map = sceneAnimBuildNodeTransforms([parent, child], pose, null, [0]);
    return {
      parentTranslation: Array.from(map.get(0).slice(12, 15)),
      parentScale: [map.get(0)[0], map.get(0)[5], map.get(0)[10]],
      childTranslation: Array.from(map.get(1).slice(12, 15)),
      childScale: [map.get(1)[0], map.get(1)[5], map.get(1)[10]],
    };
  })()`);
  assert.deepEqual(out.parentTranslation, [10, 0, -4]);
  assert.deepEqual(out.parentScale, [2, 2, 2]);
  assert.deepEqual(out.childTranslation, [10, 4, -4]);
  assert.deepEqual(out.childScale, [2, 2, 2]);
});

test("root model transform is applied exactly once through the chain", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    const root = { matrix: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 1, 2, 3, 1], children: [1] };
    const child = { translation: [1, 1, 1] };
    const rootTransform = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 100, 200, 300, 1]);
    const map = sceneAnimBuildNodeTransforms([root, child], null, rootTransform, null);
    return {
      rootTranslation: Array.from(map.get(0).slice(12, 15)),
      childTranslation: Array.from(map.get(1).slice(12, 15)),
      rootTransformTranslation: Array.from(rootTransform.slice(12, 15)),
    };
  })()`);
  assert.deepEqual(out.rootTranslation, [101, 202, 303]);
  assert.deepEqual(out.childTranslation, [102, 203, 304]);
  assert.deepEqual(out.rootTransformTranslation, [100, 200, 300]);
});

test("explicit TRS animation still overrides node TRS", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    const overridden = { translation: [9, 9, 9], scale: [5, 5, 5] };
    const partial = { translation: [4, 5, 6], scale: [2, 2, 2] };
    const weightsOnly = { translation: [3, 2, 1] };
    const pose = new Map([
      [0, { translation: [1, 2, 3], scale: [2, 3, 4] }],
      [1, { translation: [7, 8, 9] }],
      [2, { weights: [1] }],
    ]);
    const map = sceneAnimBuildNodeTransforms([overridden, partial, weightsOnly], pose, null, [0, 1, 2]);
    const a = map.get(0);
    const b = map.get(1);
    const c = map.get(2);
    return {
      aTranslation: Array.from(a.slice(12, 15)),
      aScale: [a[0], a[5], a[10]],
      bTranslation: Array.from(b.slice(12, 15)),
      bScale: [b[0], b[5], b[10]],
      cTranslation: Array.from(c.slice(12, 15)),
      cScale: [c[0], c[5], c[10]],
    };
  })()`);
  assert.deepEqual(out.aTranslation, [1, 2, 3]);
  assert.deepEqual(out.aScale, [2, 3, 4]);
  assert.deepEqual(out.bTranslation, [7, 8, 9]);
  assert.deepEqual(out.bScale, [2, 2, 2]);
  assert.deepEqual(out.cTranslation, [3, 2, 1]);
  assert.deepEqual(out.cScale, [1, 1, 1]);
});

test("repeated rebuilds never mutate or alias the source node matrix", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    const matrix = [0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1, 0, 4, 5, 6, 1];
    const node = { matrix };
    const rootTransform = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 10, 0, 0, 1]);
    const original = Array.from(matrix);
    const first = sceneAnimBuildNodeTransforms([node], null, rootTransform, [0]);
    const firstSnapshot = Array.from(first.get(0));
    first.get(0)[12] = 999;
    const second = sceneAnimBuildNodeTransforms([node], null, rootTransform, [0]);
    const secondSnapshot = Array.from(second.get(0));
    return {
      original,
      firstSnapshot,
      secondSnapshot,
      matrixAfterMutation: Array.from(matrix),
      rootTransformTranslation: Array.from(rootTransform.slice(12, 15)),
    };
  })()`);
  const expectedWorld = [0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1, 0, 14, 5, 6, 1];
  assert.deepEqual(out.firstSnapshot, expectedWorld);
  assert.deepEqual(out.secondSnapshot, expectedWorld);
  assert.deepEqual(out.matrixAfterMutation, out.original);
  assert.deepEqual(out.rootTransformTranslation, [10, 0, 0]);
});
