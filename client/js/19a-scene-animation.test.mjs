// Animation mixer tests (19a-scene-animation.js) — keyframe channel widths.
//
// The mixer used to assume every non-rotation channel held three values. A glTF
// morph "weights" channel holds one value per morph target, so the mixer read
// past the keyframe and mixed the wrong numbers. These tests pin the width
// resolution and the interpolation at each width.
//
// The fragment declares plain top-level functions, so running it in a VM
// context publishes them as context globals. 11-scene-math.ts supplies the
// shared scratch buffers and the matrix helpers the fragment expects.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  const sourcePath = name.startsWith("../") ? path.join(__dirname, name) : path.join(srcDir, name);
  return fs.readFileSync(sourcePath, "utf8");
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

test("sceneAnimChannelWidth reads the declared component count", () => {
  const { context } = createMixerContext();
  assert.equal(vm.runInContext(`sceneAnimChannelWidth({ property: "weights", componentCount: 7 })`, context), 7);
});

test("sceneAnimChannelWidth falls back to the TRS widths", () => {
  const { context } = createMixerContext();
  assert.equal(vm.runInContext(`sceneAnimChannelWidth({ property: "rotation" })`, context), 4);
  assert.equal(vm.runInContext(`sceneAnimChannelWidth({ property: "translation" })`, context), 3);
  assert.equal(vm.runInContext(`sceneAnimChannelWidth({ property: "scale" })`, context), 3);
});

test("a translation channel still interpolates three values", () => {
  const { context } = createMixerContext();
  const value = run(context, `
    var channel = {
      property: "translation",
      componentCount: 3,
      times: new Float32Array([0, 1]),
      values: new Float32Array([0, 0, 0, 2, 4, 6]),
      interpolation: "LINEAR"
    };
    Array.from(sceneAnimInterpolateChannel(channel, 0.5)).slice(0, 3);
  `);
  assert.deepEqual(value, [1, 2, 3]);
});

test("a morph weights channel interpolates every target weight", () => {
  const { context } = createMixerContext();
  const value = run(context, `
    var channel = {
      property: "weights",
      componentCount: 5,
      times: new Float32Array([0, 1]),
      values: new Float32Array([0, 0, 0, 0, 0, 1, 0.5, 0.25, 0.125, 0]),
      interpolation: "LINEAR"
    };
    Array.from(sceneAnimInterpolateChannel(channel, 0.5));
  `);
  assert.deepEqual(value, [0.5, 0.25, 0.125, 0.0625, 0]);
});

test("a morph weights channel clamps before the first and after the last key", () => {
  const { context } = createMixerContext();
  const before = run(context, `
    var channel = {
      property: "weights",
      componentCount: 4,
      times: new Float32Array([1, 2]),
      values: new Float32Array([1, 2, 3, 4, 5, 6, 7, 8]),
      interpolation: "LINEAR"
    };
    Array.from(sceneAnimInterpolateChannel(channel, 0));
  `);
  assert.deepEqual(before, [1, 2, 3, 4]);

  const after = run(context, `
    var channel = {
      property: "weights",
      componentCount: 4,
      times: new Float32Array([1, 2]),
      values: new Float32Array([1, 2, 3, 4, 5, 6, 7, 8]),
      interpolation: "LINEAR"
    };
    Array.from(sceneAnimInterpolateChannel(channel, 9));
  `);
  assert.deepEqual(after, [5, 6, 7, 8]);
});

test("a wide channel reuses one cached scratch buffer per channel", () => {
  const { context } = createMixerContext();
  const sameBuffer = vm.runInContext(`
    var channel = {
      property: "weights",
      componentCount: 6,
      times: new Float32Array([0, 1]),
      values: new Float32Array(12),
      interpolation: "LINEAR"
    };
    var first = sceneAnimInterpolateChannel(channel, 0.25);
    var second = sceneAnimInterpolateChannel(channel, 0.75);
    first === second && first.length === 6;
  `, context);
  assert.equal(sameBuffer, true);
});

test("the mixer blends a weights channel at its true width", () => {
  const { context } = createMixerContext();
  const applied = run(context, `
    var mixer = createSceneAnimationMixer();
    mixer.addClip("blink", {
      duration: 1,
      channels: [{
        targetID: 3,
        property: "weights",
        componentCount: 4,
        times: new Float32Array([0, 1]),
        values: new Float32Array([0, 0, 0, 0, 1, 1, 1, 1]),
        interpolation: "LINEAR"
      }]
    });
    mixer.play("blink", { fadeIn: 0, loop: false, weight: 1 });
    var seen = [];
    mixer.update(0.5, function(targetID, property, value) {
      seen.push({ targetID: targetID, property: property, value: value.slice() });
    });
    seen;
  `);
  assert.equal(applied.length, 1);
  assert.equal(applied[0].targetID, 3);
  assert.equal(applied[0].property, "weights");
  assert.deepEqual(applied[0].value, [0.5, 0.5, 0.5, 0.5]);
});

test("a rotation channel still slerps four values", () => {
  const { context } = createMixerContext();
  const value = run(context, `
    var channel = {
      property: "rotation",
      componentCount: 4,
      times: new Float32Array([0, 1]),
      values: new Float32Array([0, 0, 0, 1, 0, 0, 0, 1]),
      interpolation: "LINEAR"
    };
    Array.from(sceneAnimInterpolateChannel(channel, 0.5));
  `);
  assert.deepEqual(value, [0, 0, 0, 1]);
});
