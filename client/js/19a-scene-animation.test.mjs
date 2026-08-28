// Animation mixer tests (19a-scene-animation.js) — keyframe channel widths.
//
// The mixer used to assume every non-rotation channel held three values. A glTF
// morph "weights" channel holds one value per morph target, so the mixer read
// past the keyframe and mixed the wrong numbers. These tests pin the width
// resolution and the interpolation at each width.
//
// Later tests also pin the browser-facing WASM transport (wasmClipJSON /
// wasmDecodePose): the weightCount clip-JSON key and the packed scalar weight
// records decoded alongside TRS writes.
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

test("cloned five-weight channel preserves componentCount and owns buffer copies", () => {
  const { context } = createMixerContext();
  vm.runInContext(readSource("10-runtime-scene-utils.ts"), context);
  vm.runInContext(readSource("../runtime/scene3d/mount-webgl.ts"), context);
  const out = run(context, `(() => {
    const times = new Float32Array([0, 1]);
    const values = new Float32Array(10);
    const channel = { targetID: 3, property: "weights", componentCount: 5, times, values };
    const copy = sceneCloneModelAnimations([{ name: "w5", duration: 2, channels: [channel] }])[0].channels[0];
    return { count: copy.componentCount, len: copy.values.length, sharedTimes: copy.times === times, sharedValues: copy.values === values };
  })()`);
  assert.equal(out.count, 5);
  assert.equal(out.len, 10);
  assert.equal(out.sharedTimes, false);
  assert.equal(out.sharedValues, false);
});

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

// ---------------------------------------------------------------------------
// WASM transport (wasmClipJSON / wasmDecodePose)
// ---------------------------------------------------------------------------

test("wasmClipJSON forwards the declared width as weightCount for weights channels", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var clip = {
      name: "blink5",
      duration: 2.5,
      channels: [
        {
          targetID: 6,
          property: "weights",
          componentCount: 5,
          interpolation: "CUBICSPLINE",
          times: new Float32Array([0, 1]),
          values: new Float32Array([
            0, 0, 0, 0, 0,   0.5, 0.25, -0.5, 0.75, 1,   0, 0, 0, 0, 0,
            0, 0, 0, 0, 0,  -0.5, 0.75, 0.25, 1, 0.5,   0, 0, 0, 0, 0
          ])
        },
        {
          targetNode: 6,
          property: "translation",
          interpolation: "LINEAR",
          times: new Float32Array([0, 1]),
          values: new Float32Array([0, 0, 0, 1, 2, 3])
        }
      ]
    };
    var json = JSON.parse(__gosx_scene3d_animation_api.wasmClipJSON(clip));
    return {
      duration: json.duration,
      channelCount: json.channels.length,
      weights: {
        node: json.channels[0].node,
        property: json.channels[0].property,
        interpolation: json.channels[0].interpolation,
        weightCount: json.channels[0].weightCount === undefined ? "absent" : json.channels[0].weightCount,
        times: json.channels[0].times,
        values: json.channels[0].values
      },
      trs: {
        node: json.channels[1].node,
        property: json.channels[1].property,
        weightCount: json.channels[1].weightCount === undefined ? "absent" : json.channels[1].weightCount,
        values: json.channels[1].values
      }
    };
  })()`);
  assert.equal(out.duration, 2.5);
  assert.equal(out.channelCount, 2);
  assert.equal(out.weights.node, 6);
  assert.equal(out.weights.property, "weights");
  assert.equal(out.weights.interpolation, "CUBICSPLINE");
  assert.equal(out.weights.weightCount, 5);
  assert.deepEqual(out.weights.times, [0, 1]);
  assert.deepEqual(out.weights.values, [
    0, 0, 0, 0, 0,   0.5, 0.25, -0.5, 0.75, 1,   0, 0, 0, 0, 0,
    0, 0, 0, 0, 0,  -0.5, 0.75, 0.25, 1, 0.5,   0, 0, 0, 0, 0
  ]);
  assert.equal(out.trs.node, 6);
  assert.equal(out.trs.property, "translation");
  assert.equal(out.trs.weightCount, "absent");
  assert.deepEqual(out.trs.values, [0, 0, 0, 1, 2, 3]);
});

test("wasmClipJSON omits weightCount when the width is missing or invalid and never mutates the clip", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var times = new Float32Array([0, 1]);
    var values = new Float32Array([0, 0, 0, 0, 0, 1, 1, 1, 1, 1]);
    var clip = {
      name: "bad",
      duration: 1,
      channels: [
        { targetID: 2, property: "weights", times: times, values: values },
        { targetID: 2, property: "weights", componentCount: 0, times: times, values: values },
        { targetID: 2, property: "weights", componentCount: -3, times: times, values: values },
        { targetID: 2, property: "weights", componentCount: 4.5, times: times, values: values },
        { targetID: 2, property: "weights", componentCount: "5", times: times, values: values }
      ]
    };
    var json = JSON.parse(__gosx_scene3d_animation_api.wasmClipJSON(clip));
    var flags = [];
    var nodes = [];
    var props = [];
    for (var i = 0; i < json.channels.length; i++) {
      flags.push(json.channels[i].weightCount === undefined ? "absent" : json.channels[i].weightCount);
      nodes.push(json.channels[i].node);
      props.push(json.channels[i].property);
    }
    return {
      flags: flags,
      nodes: nodes,
      props: props,
      callerUntouched:
        clip.channels[0].weightCount === undefined &&
        clip.channels[3].componentCount === 4.5 &&
        clip.channels[0].times === times &&
        clip.channels[0].values === values &&
        times.length === 2
    };
  })()`);
  assert.deepEqual(out.flags, ["absent", "absent", "absent", "absent", "absent"]);
  assert.deepEqual(out.nodes, [2, 2, 2, 2, 2]);
  assert.deepEqual(out.props, ["weights", "weights", "weights", "weights", "weights"]);
  assert.equal(out.callerUntouched, true);
});

test("wasmDecodePose decodes shuffled scalar weight records alongside TRS writes", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var f = new Float64Array([
      // node 3: five weights in shuffled order, values outside [0, 1]
      3, 1004, 0, 1.5,
      3, 1000, 0, -0.25,
      3, 1003, 0, 0.5,
      3, 1001, 0, 2,
      3, 1002, 0, 0,
      // node 7: quaternion (arity slot 4) then translation (legacy arity slot 3)
      7, 1, 4, 0, 0, 0, 1,
      7, 0, 3, 10, 20, 30,
      // node 9: scale only
      9, 2, 3, 1, 2, 3,
      // node 10: translation written with the ArityVec3 ordinal (2) — the
      // decoder must size TRS records from propID, not the arity slot.
      10, 0, 2, 40, 50, 60
    ]);
    var map = new Map();
    var writes = __gosx_scene3d_animation_api.wasmDecodePose(f, f.length, map);
    var n3 = map.get(3) || {};
    var n7 = map.get(7) || {};
    var n9 = map.get(9) || {};
    var n10 = map.get(10) || {};
    return {
      writes: writes,
      weights3: n3.weights || null,
      translation3: n3.translation === undefined ? "absent" : n3.translation,
      rotation3: n3.rotation === undefined ? "absent" : n3.rotation,
      scale3: n3.scale === undefined ? "absent" : n3.scale,
      rotation7: n7.rotation || null,
      translation7: n7.translation || null,
      weights7: n7.weights === undefined ? "absent" : n7.weights,
      scale9: n9.scale || null,
      weights9: n9.weights === undefined ? "absent" : n9.weights,
      translation10: n10.translation || null,
      weights10: n10.weights === undefined ? "absent" : n10.weights
    };
  })()`);
  assert.equal(out.writes, 9);
  assert.deepEqual(out.weights3, [-0.25, 2, 0, 0.5, 1.5]);
  assert.equal(out.translation3, "absent");
  assert.equal(out.rotation3, "absent");
  assert.equal(out.scale3, "absent");
  assert.deepEqual(out.rotation7, [0, 0, 0, 1]);
  assert.deepEqual(out.translation7, [10, 20, 30]);
  assert.equal(out.weights7, "absent");
  assert.deepEqual(out.scale9, [1, 2, 3]);
  assert.equal(out.weights9, "absent");
  assert.deepEqual(out.translation10, [40, 50, 60]);
  assert.equal(out.weights10, "absent");
});

test("wasmDecodePose is stable on repeat and merges later decodes into the same entries", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var api = __gosx_scene3d_animation_api;
    var fA = new Float64Array([
      3, 1000, 0, 0.5,
      3, 1002, 0, 2,
      3, 1001, 0, -1,
      3, 0, 3, 1, 2, 3
    ]);
    var map = new Map();
    var firstWrites = api.wasmDecodePose(fA, fA.length, map);
    var afterFirst = {
      weights: (map.get(3) || {}).weights || null,
      translation: (map.get(3) || {}).translation || null
    };
    var secondWrites = api.wasmDecodePose(fA, fA.length, map);
    var afterSecond = {
      weights: (map.get(3) || {}).weights || null,
      translation: (map.get(3) || {}).translation || null
    };
    var fB = new Float64Array([
      3, 1001, 0, 7,
      3, 1, 4, 0.5, 0.5, 0.5, 0.5
    ]);
    var thirdWrites = api.wasmDecodePose(fB, fB.length, map);
    var n3 = map.get(3) || {};
    return {
      firstWrites: firstWrites,
      secondWrites: secondWrites,
      thirdWrites: thirdWrites,
      afterFirst: afterFirst,
      afterSecond: afterSecond,
      mergedWeights: n3.weights || null,
      mergedTranslation: n3.translation || null,
      mergedRotation: n3.rotation || null
    };
  })()`);
  assert.equal(out.firstWrites, 4);
  assert.equal(out.secondWrites, 4);
  assert.equal(out.thirdWrites, 2);
  assert.deepEqual(out.afterFirst.weights, [0.5, -1, 2]);
  assert.deepEqual(out.afterFirst.translation, [1, 2, 3]);
  assert.deepEqual(out.afterSecond.weights, [0.5, -1, 2]);
  assert.deepEqual(out.afterSecond.translation, [1, 2, 3]);
  assert.deepEqual(out.mergedWeights, [0.5, 7, 2]);
  assert.deepEqual(out.mergedTranslation, [1, 2, 3]);
  assert.deepEqual(out.mergedRotation, [0.5, 0.5, 0.5, 0.5]);
});

test("wasmDecodePose leaves the packed buffer untouched and returns fresh arrays", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var f = new Float64Array([5, 0, 3, 1, 2, 3, 5, 1000, 0, -0.75]);
    var before = Array.from(f);
    var map = new Map();
    var writes = __gosx_scene3d_animation_api.wasmDecodePose(f, f.length, map);
    var n5 = map.get(5) || {};
    return {
      writes: writes,
      before: before,
      after: Array.from(f),
      translation: n5.translation || null,
      weights: n5.weights || null,
      weightsIsFreshArray: Array.isArray(n5.weights) && n5.weights !== f
    };
  })()`);
  assert.equal(out.writes, 2);
  assert.deepEqual(out.before, [5, 0, 3, 1, 2, 3, 5, 1000, 0, -0.75]);
  assert.deepEqual(out.after, out.before);
  assert.deepEqual(out.translation, [1, 2, 3]);
  assert.deepEqual(out.weights, [-0.75]);
  assert.equal(out.weightsIsFreshArray, true);
});

test("wasmDecodePose strides over unknown records and stops when the width is unknowable", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var f = new Float64Array([
      5, 0, 3, 1, 2, 3,        // translation — decoded
      11, 42, 1, 99, 98,       // unknown property, ArityVec2 stride → skipped whole
      6, 2, 3, 7, 8, 9,        // scale — alignment survives the skip
      12, 5, 77, 1, 2,         // unknown property, unknown arity 77 → stop
      6, 0, 3, 4, 5, 6         // must never decode
    ]);
    var map = new Map();
    var writes = __gosx_scene3d_animation_api.wasmDecodePose(f, f.length, map);
    var n5 = map.get(5) || {};
    var n6 = map.get(6) || {};
    return {
      writes: writes,
      translation5: n5.translation || null,
      scale6: n6.scale || null,
      translation6: n6.translation === undefined ? "absent" : n6.translation,
      hasNode11: map.has(11),
      hasNode12: map.has(12)
    };
  })()`);
  assert.equal(out.writes, 2);
  assert.deepEqual(out.translation5, [1, 2, 3]);
  assert.deepEqual(out.scale6, [7, 8, 9]);
  assert.equal(out.translation6, "absent");
  assert.equal(out.hasNode11, false);
  assert.equal(out.hasNode12, false);
});

test("wasmDecodePose defends truncation, lying counts and invalid headers", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var api = __gosx_scene3d_animation_api;
    var results = [];

    // Truncated vec3 record (missing z): nothing decoded, no entry created.
    var mapA = new Map();
    results.push({ writes: api.wasmDecodePose(new Float64Array([5, 0, 3, 1, 2]), 5, mapA), size: mapA.size });

    // Truncated weight record (missing value): nothing decoded.
    var mapB = new Map();
    results.push({ writes: api.wasmDecodePose(new Float64Array([3, 1000, 0]), 3, mapB), size: mapB.size });

    // count beyond the buffer clamps to f.length: both real records decode.
    var fC = new Float64Array([7, 1, 4, 0, 0, 0, 1, 7, 0, 3, 4, 5, 6]);
    var mapC = new Map();
    results.push({
      writes: api.wasmDecodePose(fC, 1000, mapC),
      rotation: (mapC.get(7) || {}).rotation || null,
      translation: (mapC.get(7) || {}).translation || null,
      size: mapC.size
    });

    // count short of the buffer: only whole records inside count decode.
    var mapD = new Map();
    results.push({
      writes: api.wasmDecodePose(fC, 9, mapD),
      rotation: (mapD.get(7) || {}).rotation || null,
      translation: (mapD.get(7) || {}).translation === undefined ? "absent" : "present",
      size: mapD.size
    });

    // NaN / fractional / negative / out-of-int32 headers never mint entries.
    var mapE = new Map();
    results.push({
      writes: api.wasmDecodePose(new Float64Array([
        NaN, 0, 3, 1, 2, 3,
        4.5, 1, 4, 0, 0, 0, 0,
        -9, 2, 3, 1, 1, 1,
        2147483648, 0, 3, 9, 9, 9,
        8, 2.5, 3, 7, 7, 7, 0, 0
      ]), 33, mapE),
      size: mapE.size
    });

    return results;
  })()`);
  assert.deepEqual(out[0], { writes: 0, size: 0 });
  assert.deepEqual(out[1], { writes: 0, size: 0 });
  assert.equal(out[2].writes, 2);
  assert.deepEqual(out[2].rotation, [0, 0, 0, 1]);
  assert.deepEqual(out[2].translation, [4, 5, 6]);
  assert.equal(out[2].size, 1);
  assert.equal(out[3].writes, 1);
  assert.deepEqual(out[3].rotation, [0, 0, 0, 1]);
  assert.equal(out[3].translation, "absent");
  assert.equal(out[3].size, 1);
  assert.deepEqual(out[4], { writes: 0, size: 0 });
});

test("wasmDecodePose handles wide weight vectors with no small cap and per-node arrays", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var n = 33;
    var parts = [];
    for (var k = 0; k < n; k++) {
      parts.push(12, 1000 + (n - 1 - k), 0, k - 10);  // node 12: reversed order
      parts.push(13, 1000 + k, 0, (k % 3) - 1);       // node 13: forward order
    }
    var f = new Float64Array(parts);
    var map = new Map();
    var writes = __gosx_scene3d_animation_api.wasmDecodePose(f, f.length, map);
    var a = (map.get(12) || {}).weights || [];
    var b = (map.get(13) || {}).weights || [];
    return {
      writes: writes,
      lenA: a.length,
      firstA: a[0],
      lastA: a[n - 1],
      lenB: b.length,
      firstB: b[0],
      lastB: b[n - 1],
      distinctArrays: a !== b
    };
  })()`);
  assert.equal(out.writes, 66);
  assert.equal(out.lenA, 33);
  assert.equal(out.firstA, 22);
  assert.equal(out.lastA, -10);
  assert.equal(out.lenB, 33);
  assert.equal(out.firstB, -1);
  assert.equal(out.lastB, 1);
  assert.equal(out.distinctArrays, true);
});

// ---------------------------------------------------------------------------
// sceneAnimWasmDecodePose: bounded sparse-weight handling.
//
// Weight slot IDs are untrusted int32s. The materializer used to allocate and
// loop across a header's sparse slot index, so the diagnostic packet
// [0, 2147483647, 0, 1] (weight slot 2147482647) spun until the VM execution
// timeout. Dense length now grows only by what a packet paid for — native
// emits every scalar track of a new or extended vector — so a slot beyond
// established length + received record count is dropped as a malformed
// sparse outlier, uncounted, without touching its valid siblings.

test("sparse int32-extreme weight ID cannot drive allocation or decode time", () => {
  const { context } = createMixerContext();
  const out = vm.runInContext(`(() => {
    const map = new Map();
    // [target 0, prop 2147483647 = 1000 + 2147482647, ArityScalar, 1]
    const writes = sceneAnimWasmDecodePose(new Float64Array([0, 2147483647, 0, 1]), 4, map);
    return { writes, size: map.size };
  })()`, context, { timeout: 1000 });
  assert.equal(out.writes, 0);
  assert.equal(out.size, 0);
});

test("sparse extreme weight ID is dropped without losing valid siblings", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    const map = new Map();
    const f = new Float64Array([
      0, 2147483647, 0, 9,  // weight slot 2147482647 — sparse outlier, dropped
      0, 1000, 0, 0.25,     // node 0 weight slot 0 — kept
      0, 1001, 0, 0.75,     // node 0 weight slot 1 — kept
      3, 1, 4, 0, 0, 0, 1   // node 3 rotation quaternion — kept
    ]);
    const writes = sceneAnimWasmDecodePose(f, f.length, map);
    const n0 = map.get(0);
    const n3 = map.get(3);
    return {
      writes,
      size: map.size,
      weights: n0 ? n0.weights : null,
      rotation: n3 ? n3.rotation : null
    };
  })()`);
  assert.equal(out.writes, 3);
  assert.equal(out.size, 2);
  assert.deepEqual(out.weights, [0.25, 0.75]);
  assert.deepEqual(out.rotation, [0, 0, 0, 1]);
});

test("weight update never mutates a default array shared between node entries", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    const shared = [0, 0, 0, 0, 0];
    const nodeA = { translation: [1, 2, 3], rotation: [0, 0, 0, 1], weights: shared };
    const nodeB = { translation: [4, 5, 6], weights: shared };
    const map = new Map();
    map.set(7, nodeA);
    map.set(8, nodeB);
    const writes = sceneAnimWasmDecodePose(new Float64Array([
      7, 0, 3, 9, 9, 9,   // node 7 translation
      7, 1002, 0, 0.5     // node 7 weight slot 2, inside established width 5
    ]), 10, map);
    return {
      writes,
      freshForA: nodeA.weights !== shared,
      stillSharedForB: nodeB.weights === shared,
      sharedContent: shared.slice(),
      aWeights: nodeA.weights.slice(),
      aTranslation: nodeA.translation,
      aRotation: nodeA.rotation,
      bTranslation: nodeB.translation
    };
  })()`);
  assert.equal(out.writes, 2);
  assert.equal(out.freshForA, true);
  assert.equal(out.stillSharedForB, true);
  assert.deepEqual(out.sharedContent, [0, 0, 0, 0, 0]);
  assert.deepEqual(out.aWeights, [0, 0, 0.5, 0, 0]);
  assert.deepEqual(out.aTranslation, [9, 9, 9]);
  assert.deepEqual(out.aRotation, [0, 0, 0, 1]);
  assert.deepEqual(out.bTranslation, [4, 5, 6]);
});

// Regression: wasmClipJSON weightCount width bound. A weights channel whose
// componentCount is the inclusive maximum (2147483647 - 1000 + 1 = 2147482648)
// must serialize its weightCount unchanged; max+1, an out-of-range finite
// integer (1e100), and NaN/+Infinity/-Infinity must all omit the key. Tiny
// times/values buffers keep this a serialization-only check, not a
// mixer-acceptance one. Well-formed translation/rotation/scale siblings must
// round-trip with node/property/times/values intact and no weightCount key,
// and the caller's clip object must be left unmutated.
test("wasmClipJSON serializes weightCount at the inclusive 2147482648 bound, omits it beyond or non-finite, and leaves TRS siblings and the caller untouched", () => {
  const { context } = createMixerContext();
  const out = run(context, `(() => {
    var times = new Float32Array([0, 1]);
    var trs3 = new Float32Array([0, 0, 0, 1, 1, 1]);
    var trs4 = new Float32Array([0, 0, 0, 1, 0, 0, 1, 0]);
    var weightValues = new Float32Array([0, 1]);
    var widths = [
      2147483647 - 1000 + 1,  // 2147482648: inclusive maximum, must serialize
      2147483647 - 1000 + 2,  // 2147482649: first value past the bound
      1e100,                  // finite integer, far past the bound
      0 / 0,                  // NaN
      1e400,                  // +Infinity
      -1e400                  // -Infinity
    ];
    var channels = [
      { targetID: 3, property: "translation", times: times, values: trs3 },
      { targetID: 4, property: "rotation", times: times, values: trs4 },
      { targetID: 5, property: "scale", times: times, values: trs3 }
    ];
    for (var w = 0; w < widths.length; w++) {
      channels.push({
        targetID: 2,
        property: "weights",
        componentCount: widths[w],
        times: times,
        values: weightValues
      });
    }
    var clip = { name: "boundary", duration: 1, channels: channels };
    var json = JSON.parse(__gosx_scene3d_animation_api.wasmClipJSON(clip));
    var flags = [];
    var nodes = [];
    var props = [];
    for (var i = 0; i < json.channels.length; i++) {
      flags.push(json.channels[i].weightCount === undefined ? "absent" : json.channels[i].weightCount);
      nodes.push(json.channels[i].node);
      props.push(json.channels[i].property);
    }
    var srcClean = clip.channels.length === 9;
    for (var j = 0; j < clip.channels.length; j++) {
      if (clip.channels[j].weightCount !== undefined) srcClean = false;
    }
    return {
      flags: flags,
      nodes: nodes,
      props: props,
      translationTimes: json.channels[0].times,
      translationValues: json.channels[0].values,
      rotationTimes: json.channels[1].times,
      rotationValues: json.channels[1].values,
      scaleTimes: json.channels[2].times,
      scaleValues: json.channels[2].values,
      callerUntouched:
        srcClean &&
        clip.name === "boundary" &&
        clip.duration === 1 &&
        clip.channels[0].targetID === 3 &&
        clip.channels[0].property === "translation" &&
        clip.channels[1].targetID === 4 &&
        clip.channels[1].property === "rotation" &&
        clip.channels[2].targetID === 5 &&
        clip.channels[2].property === "scale" &&
        clip.channels[3].property === "weights" &&
        clip.channels[3].componentCount === 2147482648 &&
        clip.channels[4].componentCount === 2147482649 &&
        clip.channels[5].componentCount === 1e100 &&
        clip.channels[6].componentCount !== clip.channels[6].componentCount &&
        clip.channels[7].componentCount === 1e400 &&
        clip.channels[8].componentCount === -1e400 &&
        clip.channels[0].times === times &&
        clip.channels[0].values === trs3 &&
        clip.channels[1].times === times &&
        clip.channels[1].values === trs4 &&
        clip.channels[3].times === times &&
        clip.channels[3].values === weightValues &&
        clip.channels[8].times === times &&
        clip.channels[8].values === weightValues &&
        times.length === 2 &&
        trs3.length === 6 &&
        trs4.length === 8 &&
        weightValues.length === 2 &&
        times[0] === 0 &&
        times[1] === 1 &&
        trs3[0] === 0 &&
        trs3[5] === 1 &&
        trs4[3] === 1 &&
        weightValues[0] === 0 &&
        weightValues[1] === 1
    };
  })()`);
  assert.deepEqual(out.flags, ["absent", "absent", "absent", 2147482648, "absent", "absent", "absent", "absent", "absent"]);
  assert.deepEqual(out.nodes, [3, 4, 5, 2, 2, 2, 2, 2, 2]);
  assert.deepEqual(out.props, ["translation", "rotation", "scale", "weights", "weights", "weights", "weights", "weights", "weights"]);
  assert.deepEqual(out.translationTimes, [0, 1]);
  assert.deepEqual(out.translationValues, [0, 0, 0, 1, 1, 1]);
  assert.deepEqual(out.rotationTimes, [0, 1]);
  assert.deepEqual(out.rotationValues, [0, 0, 0, 1, 0, 0, 1, 0]);
  assert.deepEqual(out.scaleTimes, [0, 1]);
  assert.deepEqual(out.scaleValues, [0, 0, 0, 1, 1, 1]);
  assert.equal(out.callerUntouched, true);
});
