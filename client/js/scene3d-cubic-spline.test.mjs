// CUBICSPLINE animation tests (runtime/scene3d/animation.ts) — glTF 2.0
// Appendix C playback.
//
// A CUBICSPLINE sampler stores three width-wide vectors per keyframe:
// [inTangent, value, outTangent]. The mixer used to treat those keys as
// LINEAR pairs, reading tangents as property values. These tests pin the
// Appendix C behavior: Hermite interpolation between PROPERTY values with
// derivatives scaled by the actual interval duration, clamping to the first
// and last PROPERTY values, componentwise cubic quaternions with a final
// normalization (never slerp, never sign-flipped controls), and unchanged
// LINEAR/STEP behavior.
//
// Expected values below are hand-computed explicit numbers (or independent
// arithmetic such as Math.sqrt ratios), never another copy of the production
// Hermite formula.
//
// The fragments declare plain top-level functions, so running them in a VM
// context publishes them as context globals, matching the existing unit
// style. 11-scene-math.ts supplies the shared scratch buffers.

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

function createContext() {
  const sandbox = {
    console: { warn: () => {}, error: () => {}, log: () => {} },
    Math, JSON, Number, Object, Array, Map, Set, Error, isFinite,
    Float32Array, Float64Array,
    Int8Array, Uint8Array, Int16Array, Uint16Array, Uint32Array,
    DataView, ArrayBuffer,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  return vm.createContext(sandbox);
}

function loadAnimationContext() {
  const context = createContext();
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readSource("../runtime/scene3d/animation.ts"), context, { filename: "animation.ts" });
  return context;
}

function loadGltfContext() {
  const context = createContext();
  // gltf.ts declares gltfExtension itself, and both gltf.ts and animation.ts
  // depend on the shared math helpers and scratch buffers from
  // 11-scene-math.ts, so load the real fragments in bundle order: math,
  // then gltf, then animation.
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readSource("../runtime/scene3d/gltf.ts"), context, { filename: "gltf.ts" });
  vm.runInContext(readSource("../runtime/scene3d/animation.ts"), context, { filename: "animation.ts" });
  return context;
}

function run(context, expression) {
  return JSON.parse(JSON.stringify(vm.runInContext(expression, context)));
}

function assertVecClose(actual, expected, epsilon = 1e-6) {
  assert.equal(actual.length, expected.length);
  for (let i = 0; i < expected.length; i++) {
    assert.ok(
      Math.abs(actual[i] - expected[i]) <= epsilon,
      `component ${i}: expected ${expected[i]}, got ${actual[i]}`
    );
  }
}

test("CUBICSPLINE translation Hermite-interpolates with interval-scaled tangents", () => {
  const context = loadAnimationContext();
  const value = run(context, `
    var channel = {
      property: "translation",
      componentCount: 3,
      interpolation: "CUBICSPLINE",
      times: new Float32Array([0.5, 2.5]),
      // key0: in [1,2,3], value [10,0,-4], out [6,0,-3]
      // key1: in [-3,2,1], value [4,8,2],  out [0,0,0]
      values: new Float32Array([
        1, 2, 3,   10, 0, -4,   6, 0, -3,
        -3, 2, 1,  4, 8, 2,     0, 0, 0,
      ]),
    };
    Array.from(sceneAnimInterpolateChannel(channel, 1.5));
  `);
  // Midpoint of a 2-second span: u = 0.5, dt = 2, so the Hermite basis is
  // h00 = 0.5, h10 = 0.125, h01 = 0.5, h11 = -0.125 and every tangent term
  // carries the factor dt:
  //   x: 0.5*10 + 0.125*(6*2) + 0.5*4 - 0.125*(-3*2)  = 9.25
  //   y: 0.5*0  + 0.125*(0*2) + 0.5*8 - 0.125*(2*2)   = 3.5
  //   z: 0.5*(-4) + 0.125*(-3*2) + 0.5*2 - 0.125*(1*2) = -2
  assertVecClose(value, [9.25, 3.5, -2]);
});

test("CUBICSPLINE scale Hermite-interpolates over a non-unit span", () => {
  const context = loadAnimationContext();
  const value = run(context, `
    var channel = {
      property: "scale",
      componentCount: 3,
      interpolation: "CUBICSPLINE",
      times: new Float32Array([1, 4]),
      // key0: in [1,1,1], value [2,4,8], out [3,6,-3]
      // key1: in [-2,0,2], value [4,0,2], out [0,0,0]
      values: new Float32Array([
        1, 1, 1,   2, 4, 8,   3, 6, -3,
        -2, 0, 2,  4, 0, 2,   0, 0, 0,
      ]),
    };
    Array.from(sceneAnimInterpolateChannel(channel, 2.5));
  `);
  // u = 0.5 over dt = 3: h00 = 0.5, h10 = 0.125, h01 = 0.5, h11 = -0.125
  //   x: 0.5*2 + 0.125*(3*3)  + 0.5*4 - 0.125*(-2*3) = 4.875
  //   y: 0.5*4 + 0.125*(6*3)  + 0.5*0 - 0.125*(0*3)  = 4.25
  //   z: 0.5*8 + 0.125*(-3*3) + 0.5*2 - 0.125*(2*3)  = 3.125
  assertVecClose(value, [4.875, 4.25, 3.125]);
});

test("CUBICSPLINE clamps to first and last PROPERTY values, not tangents", () => {
  const context = loadAnimationContext();
  const setup = `
    var channel = {
      property: "translation",
      componentCount: 3,
      interpolation: "CUBICSPLINE",
      times: new Float32Array([0.5, 2.5]),
      values: new Float32Array([
        1, 2, 3,   10, 0, -4,   6, 0, -3,
        -3, 2, 1,  4, 8, 2,     0, 0, 0,
      ]),
    };
  `;
  // Both clamps sit at a nonzero first-key time.
  const first = run(context, setup + " Array.from(sceneAnimInterpolateChannel(channel, 0));");
  assertVecClose(first, [10, 0, -4]);
  const last = run(context, setup + " Array.from(sceneAnimInterpolateChannel(channel, 9));");
  assertVecClose(last, [4, 8, 2]);
});

test("CUBICSPLINE morph weights interpolate at widths 1, 2, and 5", () => {
  const context = loadAnimationContext();

  const one = run(context, `
    var w1 = {
      property: "weights", componentCount: 1, interpolation: "CUBICSPLINE",
      times: new Float32Array([0, 1]),
      // key0: in 0, value 0, out 4 — key1: in 2, value 1, out 0
      values: new Float32Array([0, 0, 4, 2, 1, 0]),
    };
    Array.from(sceneAnimInterpolateChannel(w1, 0.25));
  `);
  // u = 0.25, dt = 1: h00 = 0.84375, h10 = 0.140625, h01 = 0.15625, h11 = -0.046875
  // 0.140625*4 + 0.15625*1 - 0.046875*2 = 0.625
  assertVecClose(one, [0.625]);

  const two = run(context, `
    var w2 = {
      property: "weights", componentCount: 2, interpolation: "CUBICSPLINE",
      times: new Float32Array([1, 3]),
      // key0: in [9,9], value [0,1], out [2,0]
      // key1: in [0,4], value [1,0], out [7,7]
      values: new Float32Array([9, 9, 0, 1, 2, 0, 0, 4, 1, 0, 7, 7]),
    };
    Array.from(sceneAnimInterpolateChannel(w2, 2));
  `);
  // u = 0.5 over dt = 2:
  //   c0: 0.5*0 + 0.125*(2*2) + 0.5*1 - 0.125*(0*2) = 1
  //   c1: 0.5*1 + 0.125*(0*2) + 0.5*0 - 0.125*(4*2) = -0.5
  assertVecClose(two, [1, -0.5]);

  const five = run(context, `
    var w5 = {
      property: "weights", componentCount: 5, interpolation: "CUBICSPLINE",
      times: new Float32Array([0.5, 1.5, 2.5]),
      values: new Float32Array([
        0,0,0,0,0,  1,2,3,4,5,  0,0,0,0,0,
        9,9,9,9,9,  2,3,4,5,6,  0,0,0,0,0,
        0,0,0,0,0,  0,0,0,0,0,  0,0,0,0,0,
      ]),
    };
    Array.from(sceneAnimInterpolateChannel(w5, 1.5));
  `);
  // Exactly on the middle key: the Hermite basis collapses to h01 = 1, so
  // the result is the middle key's PROPERTY value regardless of tangents.
  assertVecClose(five, [2, 3, 4, 5, 6]);
});

test("CUBICSPLINE walks cached segments forward and backward across unequal intervals", () => {
  const context = loadAnimationContext();
  // Key intervals 1, 3, 5. All tangents zero, so the Hermite reduces to the
  // smoothstep 3u^2 - 2u^3 between the PROPERTY values
  // v0=[0,0,0], v1=[6,3,0], v2=[6,9,0], v3=[0,9,12].
  const seq = run(context, `
    var channel = {
      property: "translation", componentCount: 3, interpolation: "CUBICSPLINE",
      times: new Float32Array([0, 1, 4, 9]),
      values: new Float32Array([
        0,0,0,  0,0,0,  0,0,0,
        0,0,0,  6,3,0,  0,0,0,
        0,0,0,  6,9,0,  0,0,0,
        0,0,0,  0,9,12, 0,0,0,
      ]),
    };
    var r1 = Array.from(sceneAnimInterpolateChannel(channel, 0.5));
    var r2 = Array.from(sceneAnimInterpolateChannel(channel, 2.5));
    var r3 = Array.from(sceneAnimInterpolateChannel(channel, 0.25));
    var r4 = Array.from(sceneAnimInterpolateChannel(channel, 6.5));
    [r1, r2, r3, r4];
  `);
  // t=0.5:  segment 0, u = 0.5,  smoothstep = 0.5     → [3, 1.5, 0]
  assertVecClose(seq[0], [3, 1.5, 0]);
  // t=2.5:  segment 1, u = 0.5,  smoothstep = 0.5     → [6, 6, 0]
  assertVecClose(seq[1], [6, 6, 0]);
  // t=0.25: backward seek, segment 0, u = 0.25,
  //         smoothstep = 0.1875 - 0.03125 = 0.15625   → [0.9375, 0.46875, 0]
  assertVecClose(seq[2], [0.9375, 0.46875, 0]);
  // t=6.5:  forward again, segment 2, u = 0.5, smoothstep = 0.5 → [3, 9, 6]
  assertVecClose(seq[3], [3, 9, 6]);
});

test("CUBICSPLINE reuses the per-channel scratch buffer", () => {
  const context = loadAnimationContext();
  const same = vm.runInContext(`
    var channel = {
      property: "weights", componentCount: 2, interpolation: "CUBICSPLINE",
      times: new Float32Array([0, 1]),
      values: new Float32Array(12),
    };
    var first = sceneAnimInterpolateChannel(channel, 0.25);
    var second = sceneAnimInterpolateChannel(channel, 0.75);
    first === second && first.length === 2;
  `, context);
  assert.equal(same, true);
});

test("CUBICSPLINE rotation interpolates componentwise and normalizes (not slerp)", () => {
  const context = loadAnimationContext();
  const value = run(context, `
    var channel = {
      property: "rotation", componentCount: 4, interpolation: "CUBICSPLINE",
      times: new Float32Array([0, 1]),
      // key0: in [0,0,0,0], value [0,0,0,1], out [0,4,0,0]
      // key1: in [0,0,0,0], value [0,1,0,0], out [0,0,0,0]
      values: new Float32Array([
        0,0,0,0,  0,0,0,1,  0,4,0,0,
        0,0,0,0,  0,1,0,0,  0,0,0,0,
      ]),
    };
    Array.from(sceneAnimInterpolateChannel(channel, 0.5));
  `);
  // Raw Hermite at u = 0.5, dt = 1:
  //   [0, 0.125*4 + 0.5*1, 0, 0.5*1] = [0, 1, 0, 0.5], norm = sqrt(1.25)
  // Normalized: [0, 1/sqrt(1.25), 0, 0.5/sqrt(1.25)] = [0, 2/sqrt(5), 0, 1/sqrt(5)].
  assertVecClose(value, [0, 2 / Math.sqrt(5), 0, 1 / Math.sqrt(5)]);
  // The slerp midpoint of q0 = [0,0,0,1] and q1 = [0,1,0,0] would be
  // [0, sin(pi/4), 0, cos(pi/4)] = [0, sqrt(0.5), 0, sqrt(0.5)]. The cubic
  // result is tangent-sensitive and therefore not slerp.
  assert.ok(Math.abs(value[1] - Math.SQRT1_2) > 0.1);
  assert.ok(Math.abs(value[3] - Math.SQRT1_2) > 0.1);

  // Raising the out-tangent of key0 to [0,8,0,0] moves the midpoint:
  //   raw = [0, 0.125*8 + 0.5, 0, 0.5] = [0, 1.5, 0, 0.5], norm = sqrt(2.5)
  const steep = run(context, `
    channel.values[9] = 8;
    Array.from(sceneAnimInterpolateChannel(channel, 0.5));
  `);
  assertVecClose(steep, [0, 1.5 / Math.sqrt(2.5), 0, 0.5 / Math.sqrt(2.5)]);
  assert.ok(Math.abs(steep[1] - value[1]) > 1e-3);
});

test("CUBICSPLINE rotation falls back to identity on a zero-norm result", () => {
  const context = loadAnimationContext();
  const value = run(context, `
    var channel = {
      property: "rotation", componentCount: 4, interpolation: "CUBICSPLINE",
      times: new Float32Array([0, 1]),
      // key0 value [1,0,0,0], key1 value [-1,0,0,0], all tangents zero:
      // the raw Hermite at u = 0.5 is [0,0,0,0].
      values: new Float32Array([
        0,0,0,0,  1,0,0,0,  0,0,0,0,
        0,0,0,0,  -1,0,0,0, 0,0,0,0,
      ]),
    };
    Array.from(sceneAnimInterpolateChannel(channel, 0.5));
  `);
  assertVecClose(value, [0, 0, 0, 1]);
  for (const component of value) {
    assert.ok(Number.isFinite(component));
  }
});

test("CUBICSPLINE rotation does not sign-flip controls on a negative-dot pair", () => {
  const context = loadAnimationContext();
  const value = run(context, `
    var channel = {
      property: "rotation", componentCount: 4, interpolation: "CUBICSPLINE",
      times: new Float32Array([0, 1]),
      // key0: in [0,0,0,0], value [0,0,0,1], out [0,1,0,0]
      // key1: in [0,0,0,0], value [0,sqrt(3)/2,0,-1/2], out [0,0,0,0]
      // dot(q0, q1) = -1/2 < 0, yet the raw Hermite result is nonzero, so
      // this exercises the normalization path, not the zero-norm fallback.
      values: new Float32Array([
        0,0,0,0,  0,0,0,1,  0,1,0,0,
        0,0,0,0,  0,0.8660254037844386,0,-0.5,  0,0,0,0,
      ]),
    };
    Array.from(sceneAnimInterpolateChannel(channel, 0.5));
  `);
  // Independent arithmetic at u = 0.5, dt = 1 (h00 = h01 = 0.5, h10 = 0.125,
  // h11 = -0.125), with NO sign flip of the negative-dot q1 controls:
  //   raw y = 0.125*1 + 0.5*(sqrt(3)/2)     = (1 + 2*sqrt(3))/8
  //   raw w = 0.5*1 + 0.5*(-1/2)            = 1/4
  //   norm^2 = ((1 + 2*sqrt(3))^2 + 4)/64   = (17 + 4*sqrt(3))/64
  const d = Math.sqrt(17 + 4 * Math.sqrt(3));
  assertVecClose(value, [0, (1 + 2 * Math.sqrt(3)) / d, 0, 2 / d]);
  // A slerp-style flip (negating q1 because dot < 0) would instead produce
  // raw [0, 0.125 - sqrt(3)/4, 0, 3/4], normalized ≈ [0, -0.3799, 0, 0.9250]
  // — clearly different from the un-flipped result asserted above.
  assert.ok(value[1] > 0.9);
  assert.ok(value[3] < 0.5);
});

test("CUBICSPLINE interpolation leaves the source arrays untouched", () => {
  const context = loadAnimationContext();
  const out = run(context, `
    var times = new Float32Array([0, 1]);
    var values = new Float32Array([
      0,0,0,  1,2,3,  4,5,6,
      0,0,0,  7,8,9,  0,0,0,
    ]);
    var channel = {
      property: "translation", componentCount: 3, interpolation: "CUBICSPLINE",
      times: times, values: values,
    };
    sceneAnimInterpolateChannel(channel, 0.5);
    ({
      timesCopy: Array.from(times),
      valuesCopy: Array.from(values),
      sameTimes: channel.times === times,
      sameValues: channel.values === values,
    });
  `);
  assert.deepEqual(out.timesCopy, [0, 1]);
  assert.deepEqual(out.valuesCopy, [0, 0, 0, 1, 2, 3, 4, 5, 6, 0, 0, 0, 7, 8, 9, 0, 0, 0]);
  assert.equal(out.sameTimes, true);
  assert.equal(out.sameValues, true);
});

test("LINEAR and STEP channels keep their previous behavior", () => {
  const context = loadAnimationContext();

  const linear = run(context, `
    var linear = {
      property: "translation", componentCount: 3, interpolation: "LINEAR",
      times: new Float32Array([0, 1]),
      values: new Float32Array([0, 0, 0, 2, 4, 6]),
    };
    Array.from(sceneAnimInterpolateChannel(linear, 0.25));
  `);
  assertVecClose(linear, [0.5, 1, 1.5]);

  const step = run(context, `
    var stepCh = {
      property: "translation", componentCount: 3, interpolation: "STEP",
      times: new Float32Array([0, 1]),
      values: new Float32Array([7, 8, 9, 1, 2, 3]),
    };
    Array.from(sceneAnimInterpolateChannel(stepCh, 0.75));
  `);
  assertVecClose(step, [7, 8, 9]);

  const slerp = run(context, `
    var rot = {
      property: "rotation", componentCount: 4, interpolation: "LINEAR",
      times: new Float32Array([0, 1]),
      values: new Float32Array([0, 0, 0, 1, 0, 1, 0, 0]),
    };
    Array.from(sceneAnimInterpolateChannel(rot, 0.5));
  `);
  // Quarter-turn midpoint: [0, sin(pi/4), 0, cos(pi/4)].
  assertVecClose(slerp, [0, Math.SQRT1_2, 0, Math.SQRT1_2]);
});

test("gltfExtractAnimations preserves CUBICSPLINE triplets and feeds the mixer", () => {
  const context = loadGltfContext();
  const out = run(context, `(() => {
    // One binary buffer, 44 floats: times3(3) + translation output(27)
    // + times2(2) + weights output(12).
    var data = new Float32Array(44);
    // Translation times: 0, 1, 2.
    data[0] = 0; data[1] = 1; data[2] = 2;
    // Translation output at float 3: 3 keys x [in(3), value(3), out(3)].
    // key1 value [1,2,3] at float 3 + 12; key2 value [2,4,6] at float 3 + 21.
    data[3 + 12] = 1; data[3 + 13] = 2; data[3 + 14] = 3;
    data[3 + 21] = 2; data[3 + 22] = 4; data[3 + 23] = 6;
    // Weights times at float 30: 0, 2.
    data[30] = 0; data[31] = 2;
    // Weights output at float 32: 2 keys x [in(2), value(2), out(2)].
    // key0 value [0,1]; key1 value [1,0].
    data[32 + 2] = 0; data[32 + 3] = 1;
    data[32 + 6 + 2] = 1; data[32 + 6 + 3] = 0;
    var buffer = data.buffer;

    var gltf = {
      bufferViews: [
        { buffer: 0, byteOffset: 0 },    // 0: translation times (3 floats)
        { buffer: 0, byteOffset: 12 },   // 1: translation output (9 vec3)
        { buffer: 0, byteOffset: 120 },  // 2: weights times (2 floats)
        { buffer: 0, byteOffset: 128 },  // 3: weights output (12 scalars)
      ],
      accessors: [
        { bufferView: 0, componentType: 5126, count: 3, type: "SCALAR" },
        { bufferView: 1, componentType: 5126, count: 9, type: "VEC3" },
        { bufferView: 2, componentType: 5126, count: 2, type: "SCALAR" },
        { bufferView: 3, componentType: 5126, count: 12, type: "SCALAR" },
      ],
      animations: [{
        name: "cubic",
        samplers: [
          { input: 0, output: 1, interpolation: "CUBICSPLINE" },
          { input: 2, output: 3, interpolation: "CUBICSPLINE" },
        ],
        channels: [
          { sampler: 0, target: { node: 7, path: "translation" } },
          { sampler: 1, target: { node: 7, path: "weights" } },
        ],
      }],
    };

    var clip = gltfExtractAnimations(gltf, buffer)[0];
    var trs = clip.channels[0];
    var weights = clip.channels[1];

    var mixer = createSceneAnimationMixer();
    mixer.addClip("cubic", clip);
    mixer.play("cubic", { fadeIn: 0, loop: false });
    var seenTranslation = null;
    var seenWeights = null;
    mixer.update(1, function (targetID, property, value) {
      if (property === "translation") seenTranslation = Array.from(value);
      if (property === "weights") seenWeights = Array.from(value);
    });

    return {
      duration: clip.duration,
      trsCount: trs.componentCount,
      weightsCount: weights.componentCount,
      trsInterpolation: trs.interpolation,
      weightsInterpolation: weights.interpolation,
      trsValuesLength: trs.values.length,
      weightsValuesLength: weights.values.length,
      key0InTangent: Array.from(trs.values.slice(0, 3)),
      key1Value: Array.from(trs.values.slice(12, 15)),
      key2Value: Array.from(trs.values.slice(21, 24)),
      weightKey0Value: Array.from(weights.values.slice(2, 4)),
      seenTranslation: seenTranslation,
      seenWeights: seenWeights,
    };
  })()`, context);

  // Extractor preservation: triplet layout and true component widths.
  assert.equal(out.duration, 2);
  assert.equal(out.trsCount, 3);
  assert.equal(out.weightsCount, 2);
  assert.equal(out.trsInterpolation, "CUBICSPLINE");
  assert.equal(out.weightsInterpolation, "CUBICSPLINE");
  assert.equal(out.trsValuesLength, 27);
  assert.equal(out.weightsValuesLength, 12);
  assert.deepEqual(out.key0InTangent, [0, 0, 0]);
  assert.deepEqual(out.key1Value, [1, 2, 3]);
  assert.deepEqual(out.key2Value, [2, 4, 6]);
  assert.deepEqual(out.weightKey0Value, [0, 1]);

  // Mixer playback at t = 1: exactly on the middle translation key
  // (h01 = 1 → [1,2,3]); weights halfway over the 2-second span with zero
  // tangents (0.5*[0,1] + 0.5*[1,0] = [0.5,0.5]).
  assert.deepEqual(out.seenTranslation, [1, 2, 3]);
  assertVecClose(out.seenWeights, [0.5, 0.5]);
});
