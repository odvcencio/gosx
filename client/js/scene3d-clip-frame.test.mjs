// Sampled clip frames (GSP4): exact clip-time posing for rollback netcode.
//
// These tests sample a known three-node rig at known clip times and check the
// joint matrices, including after jumping backwards. The expected numbers are
// derived by hand in the comments, not by calling the code under test.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const transpile = (source) => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const read = (...parts) => fs.readFileSync(path.join(here, ...parts), "utf8");

function animationContext() {
  const sandbox = { console: { warn() {}, error() {}, log() {} }, Math, JSON, Number, Object, Array, Map, Set, WeakMap, Float32Array, Float64Array, isFinite };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  const context = vm.createContext(sandbox);
  vm.runInContext(read("bootstrap-src", "11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(transpile(read("..", "runtime", "scene3d", "animation.ts")), context, { filename: "animation.ts" });
  return { sandbox, context, api: sandbox.__gosx_scene3d_animation_api };
}

// Rig: node 0 "Armature" (scene root) > node 1 "hips" (joint 0, bind y = 1) >
// node 2 "arm" (joint 1, bind x = 1). Bind world: hips (0,1,0), arm (1,1,0).
// Inverse bind matrices are translations by minus those positions.
function rig() {
  const nodes = [
    { translation: [0, 0, 0], children: [1] },
    { translation: [0, 1, 0], children: [2] },
    { translation: [1, 0, 0] },
  ];
  const ibm = new Float32Array(32);
  ibm.set([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, -1, 0, 1], 0);
  ibm.set([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, -1, -1, 0, 1], 16);
  const skins = [{ joints: [1, 2], inverseBindMatrices: ibm }];
  const s = Math.SQRT1_2; // sin(45 deg) = cos(45 deg)
  const clips = () => [
    { // index 0: hips travel (0,1,0) -> (5,2,5) -> (0,1,0); arm rotates 0 -> 90 deg about Z
      name: "walk", duration: 2, channels: [
        { targetNode: 1, property: "translation", interpolation: "LINEAR", times: new Float32Array([0, 1, 2]), values: new Float32Array([0, 1, 0, 5, 2, 5, 0, 1, 0]) },
        { targetNode: 2, property: "rotation", interpolation: "LINEAR", times: new Float32Array([0, 2]), values: new Float32Array([0, 0, 0, 1, 0, 0, 1 * s, s]) },
      ],
    },
    { // index 1: STEP, arm y offset 0 -> 3 -> 0 with a key exactly at t=1
      name: "pop", duration: 2, channels: [
        { targetNode: 2, property: "translation", interpolation: "STEP", times: new Float32Array([0, 1, 2]), values: new Float32Array([1, 0, 0, 1, 3, 0, 1, 0, 0]) },
      ],
    },
  ];
  return { nodes, skins, clips };
}

function pose(api, options = {}) {
  const { nodes, skins, clips } = rig();
  const sampler = api.createClipSampler(clips(), { nodes, skins, rootNodes: [0], ...options });
  const output = new Map();
  const skin = { joints: skins[0].joints, inverseBindMatrices: skins[0].inverseBindMatrices };
  return {
    sampler,
    at(layers) {
      output.clear();
      sampler.sample(layers, layers.length, output);
      const world = api.buildNodeTransforms(nodes, output, null, [0]);
      return Float32Array.from(api.computeJointMatrices(skin, world));
    },
  };
}

const layer = (clip, time, weight = 1, loop = false) => typeof clip === "string"
  ? { name: clip, index: 0, time, weight, loop }
  : { name: "", index: clip, time, weight, loop };
const apply = (m, x, y, z) => [m[0] * x + m[4] * y + m[8] * z + m[12], m[1] * x + m[5] * y + m[9] * z + m[13], m[2] * x + m[6] * y + m[10] * z + m[14]];
const near = (actual, expected, label) => {
  assert.equal(actual.length, expected.length, label);
  actual.forEach((v, i) => assert.ok(Math.abs(v - expected[i]) < 1e-5, `${label}: [${i}] ${v} != ${expected[i]}`));
};
const joint = (matrices, i) => matrices.subarray(i * 16, i * 16 + 16);

test("sampling a known clip at a known time yields the hand-derived joint matrices", () => {
  const { api } = animationContext();
  const p = pose(api, { rootMotion: true });
  // t=0.5: hips lerp (0,1,0)->(5,2,5) at 0.5 = (2.5,1.5,2.5). joint0 = T(hips) * T(0,-1,0).
  // Rotation at t=0.5 is 22.5 degrees. Bind point of hips (0,1,0) maps to the hips position.
  const m = p.at([layer("walk", 0.5)]);
  near(apply(joint(m, 0), 0, 1, 0), [2.5, 1.5, 2.5], "hips bind point follows hips");
  // joint1 = T(hips) * T(1,0,0) * R(22.5 deg about Z) * T(-1,-1,0); point (2,1,0) -> (1,0,0) -> rotated.
  const a = (22.5 * Math.PI) / 180;
  near(apply(joint(m, 1), 2, 1, 0), [2.5 + 1 + Math.cos(a), 1.5 + Math.sin(a), 2.5], "arm tip");
});

test("at t=0 the pose equals the bind pose, and it is not the identity matrix", () => {
  const { api } = animationContext();
  const m = pose(api, { rootMotion: true }).at([layer("walk", 0)]);
  near(Array.from(joint(m, 0)), [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], "joint0 bind");
  near(Array.from(joint(m, 1)), [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], "joint1 bind");
});

test("jumping backwards and forwards reproduces the same pose bit for bit", () => {
  const { api } = animationContext();
  const p = pose(api, { rootMotion: true });
  const times = [0.5, 1.75, 0.25, 1.9, 0.5, 1.0, 1.75];
  const first = new Map();
  for (const t of times) {
    const m = p.at([layer("walk", t), layer("pop", t, 0.5)]);
    if (!first.has(t)) first.set(t, m);
    assert.deepEqual(m, first.get(t), `t=${t} must match its first sample`);
    // A fresh sampler that never saw any other time must agree exactly.
    assert.deepEqual(m, pose(api, { rootMotion: true }).at([layer("walk", t), layer("pop", t, 0.5)]), `t=${t} fresh`);
  }
});

test("a STEP key exactly on a boundary does not depend on the previous sample", () => {
  const { api } = animationContext();
  // Times are [0,1,2]; at t=1 the canonical key is the segment that ends at t=1 (value y=0 from key 0).
  const p = pose(api);
  p.at([layer("pop", 1.5)]); // leaves a cached index of 1
  const afterForward = p.at([layer("pop", 1)]);
  const fresh = pose(api).at([layer("pop", 1)]);
  assert.deepEqual(afterForward, fresh);
  assert.deepEqual(p.at([layer("pop", 1.5)]), pose(api).at([layer("pop", 1.5)]));
  // Past the key the arm is raised by 3: joint1 maps its bind tip (1,1,0) to (1,4,0).
  near(apply(joint(p.at([layer("pop", 1.5)]), 1), 1, 1, 0), [1, 4, 0], "raised");
});

test("time past the end wraps for loop layers and clamps for once layers", () => {
  const { api } = animationContext();
  const p = pose(api, { rootMotion: true });
  assert.deepEqual(p.at([layer("walk", 2.5, 1, true)]), p.at([layer("walk", 0.5)]), "loop 2.5 == 0.5");
  assert.deepEqual(p.at([layer("walk", 4.5, 1, true)]), p.at([layer("walk", 0.5)]), "loop 4.5 == 0.5");
  assert.deepEqual(p.at([layer("walk", 9, 1, false)]), p.at([layer("walk", 2)]), "once 9 == end");
  assert.deepEqual(p.at([layer("walk", 2, 1, true)]), p.at([layer("walk", 0)]), "loop at exactly the duration wraps to the start");
});

test("layers blend by relative weight; zero-weight and unknown layers add nothing", () => {
  const { api } = animationContext();
  const p = pose(api, { rootMotion: true });
  // At t=1 "walk" puts hips at (5,2,5); at t=0 hips are at (0,1,0).
  const equal = p.at([layer("walk", 1, 1), layer("walk", 0, 1)]);
  near(apply(joint(equal, 0), 0, 1, 0), [2.5, 1.5, 2.5], "equal weights");
  const quarter = p.at([layer("walk", 1, 1), layer("walk", 0, 3)]);
  near(apply(joint(quarter, 0), 0, 1, 0), [1.25, 1.25, 1.25], "1:3 weights");
  const scaled = p.at([layer("walk", 1, 0.25), layer("walk", 0, 0.75)]);
  assert.deepEqual(scaled.map(Math.fround), quarter.map(Math.fround), "weights are relative, not required to sum to one");
  assert.deepEqual(p.at([layer("walk", 1, 0), layer("walk", 0.5, 2)]), p.at([layer("walk", 0.5, 1)]), "weight 0 is skipped");
  assert.deepEqual(p.at([layer("nope", 1, 1), layer("walk", 0.5, 1)]), p.at([layer("walk", 0.5, 1)]), "unknown clip is skipped");
  assert.deepEqual(p.at([layer(0, 0.5), layer(1, 0.5, 0)]), p.at([layer("walk", 0.5)]), "index selects the same clip as name");
  assert.deepEqual(p.at([]), pose(api).at([]), "no layers poses the bind pose");
});

test("root motion is ignored by default and applied on request", () => {
  const { api } = animationContext();
  const off = pose(api).at([layer("walk", 1)]);
  // Hips at t=1 are (5,2,5) in the clip. Default keeps authored X/Z (0,0) and the clip's Y (2).
  near(apply(joint(off, 0), 0, 1, 0), [0, 2, 0], "horizontal motion dropped, height kept");
  const on = pose(api, { rootMotion: true }).at([layer("walk", 1)]);
  near(apply(joint(on, 0), 0, 1, 0), [5, 2, 5], "root motion applied");
});

test("the sampler never reads a clock and does not mutate clip data", () => {
  const { api } = animationContext();
  const { nodes, skins, clips } = rig();
  const clipList = clips();
  const before = JSON.stringify(clipList.map(c => c.channels.map(ch => Array.from(ch.values))));
  const sampler = api.createClipSampler(clipList, { nodes, skins });
  const realNow = Date.now;
  Date.now = () => assert.fail("sampled mode must not read the wall clock");
  try { sampler.sample([layer("walk", 0.7)], 1, new Map()); } finally { Date.now = realNow; }
  assert.equal(JSON.stringify(clipList.map(c => c.channels.map(ch => Array.from(ch.values)))), before);
});

// --- mount record plumbing (clip frame apply / clock advance) ----------------

function mountFixture() {
  const { sandbox, context, api } = animationContext();
  const source = read("..", "runtime", "scene3d", "mount-webgl.ts");
  const slice = (first, last) => source.slice(source.indexOf(`  function ${first}(`), source.indexOf(`  function ${last}(`));
  const code = [
    slice("sceneApplyModelSkinPose", "sceneOwns"),
    slice("sceneRegisterModelAnimationRecord", "sceneRegisterStaticModelLiveRecord"),
    slice("sceneHasActiveModelAnimations", "sceneModelRecordListensToEvent"),
  ].join("\n");
  assert.match(code, /function sceneAdvanceModelAnimations\(/);
  vm.runInContext(transpile(code), context);
  const { nodes, skins, clips } = rig();
  const state = { _modelAnimations: [], _modelSkins: [], _clipFrameLayers: new Map() };
  const record = {
    id: "p1", model: { id: "p1" }, nodes, rootNodes: [0], skins: skins.map(s => ({ ...s })),
    animatedTransforms: new Map(), rootTransform: null, animationApi: api, mixer: null, animation: "", poseDirty: false,
  };
  state._modelSkins.push(record);
  const configure = () => api.configureSampledRecord(state, record, clips(), { rootMotion: true });
  configure();
  vm.runInContext("sceneRegisterModelAnimationRecord", context)(state, record);
  const call = (name, ...args) => vm.runInContext(name, context)(...args);
  const applyFrame = (st, models) => api.applyClipFrame(st, models, vm.runInContext("sceneApplyModelSkinPose", context));
  return { state, record, call, sandbox, context, configure, applyFrame };
}

test("a sampled record in the animation list never advances with the client clock", () => {
  const { state, record, call, applyFrame } = mountFixture();
  assert.deepEqual(state._modelAnimations, [record], "registered so live transform patches can re-skin it");
  assert.equal(record.animationApi.isModelPlaying(record), false);
  assert.equal(call("sceneHasActiveModelAnimations", state), false, "a sampled model never asks for continuous rendering");
  const result = applyFrame(state, [{ id: "p1", layers: [layer("walk", 1)] }]);
  assert.equal(result.applied, true);
  const held = Float32Array.from(record.skins[0].jointMatrices);
  assert.ok(held.some((v, i) => Math.abs(v - [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1][i % 16]) > 1e-3), "pose is not the bind pose");
  for (let frame = 0; frame < 120; frame++) call("sceneAdvanceModelAnimations", state, 1 / 60, false);
  call("sceneAdvanceModelAnimations", state, 30, false);
  assert.deepEqual(Float32Array.from(record.skins[0].jointMatrices), held, "120 clock ticks and a 30 s tick changed nothing");
  // A live transform patch sets poseDirty; the re-skin keeps the same layers.
  record.poseDirty = true;
  call("sceneAdvanceModelAnimations", state, 5, false);
  assert.deepEqual(Float32Array.from(record.skins[0].jointMatrices), held);
});

test("rollback: re-sending an earlier frame restores exactly that pose", () => {
  const { state, record, call, applyFrame } = mountFixture();
  const send = (t) => { applyFrame(state, [{ id: "p1", layers: [layer("walk", t, 1, true), layer("pop", t, 0.25, true)] }]); return Float32Array.from(record.skins[0].jointMatrices); };
  const f10 = send(10 / 60);
  const f11 = send(11 / 60);
  const f12 = send(12 / 60);
  assert.notDeepEqual(f10, f12);
  assert.deepEqual(send(11 / 60), f11, "roll back to frame 11");
  assert.deepEqual(send(10 / 60), f10, "roll back to frame 10");
  assert.deepEqual(send(12 / 60), f12, "resimulate forward");
});

test("applyClipFrame rejects unknown models without writing, and remembers frames for pending hydration", () => {
  const { state, record, call, applyFrame } = mountFixture();
  const rejected = applyFrame(state, [{ id: "p1", layers: [layer("walk", 1)] }, { id: "ghost", layers: [] }]);
  assert.equal(rejected.applied, false);
  assert.equal(rejected.reason, "unknown-model");
  assert.equal(record.sampledLayerCount, 0, "no partial write");
  state._modelHydrationPromise = {};
  const pending = applyFrame(state, [{ id: "later", layers: [layer("walk", 1)] }]);
  assert.equal(pending.applied, true);
  assert.equal(pending.pending, true);
  assert.equal(state._clipFrameLayers.get("later").count, 1);
});

test("re-hydration re-poses from the last frame instead of the bind pose", () => {
  const { state, record, call, configure, applyFrame } = mountFixture();
  applyFrame(state, [{ id: "p1", layers: [layer("walk", 1)] }]);
  record.sampledLayers = []; record.sampledLayerCount = 0;
  configure();
  assert.equal(record.sampledLayerCount, 1);
  assert.equal(record.sampledLayers[0].time, 1);
});

// --- wire decoding -----------------------------------------------------------

const commandSource = transpile(read("..", "runtime", "scene3d", "command-runtime.ts"));
function bridge() {
  const context = { window: {}, document: {}, TextDecoder, Uint8Array, ArrayBuffer, DataView, Set, Map, Date, Promise, setTimeout, Object };
  vm.createContext(context);
  vm.runInContext(commandSource, context);
  return context.window.__gosx_scene3d_command_bridge;
}

test("decodeClipFrame reads names, indexes, times, weights and loop flags", () => {
  const b = bridge();
  const enc = new TextEncoder();
  const out = [];
  const u16 = (v) => out.push(v & 255, v >> 8);
  const f32 = (v) => { const d = new DataView(new ArrayBuffer(4)); d.setFloat32(0, v, true); out.push(...new Uint8Array(d.buffer)); };
  const id = (v) => { const c = enc.encode(v); u16(c.length); out.push(...c); };
  out.push(71, 83, 80, 52); u16(1); id("walk"); u16(2);
  id("p1"); out.push(2); u16(0); f32(1.5); f32(0.25); out.push(1); u16(0x8000 | 3); f32(0.5); f32(0.75); out.push(0);
  id("p2"); out.push(0);
  const models = b.decodeClipFrame(Uint8Array.from(out));
  assert.equal(models.length, 2);
  assert.deepEqual(JSON.parse(JSON.stringify(models[0])), { id: "p1", layers: [
    { name: "walk", index: 0, time: 1.5, weight: 0.25, loop: true },
    { name: "", index: 3, time: 0.5, weight: 0.75, loop: false },
  ] });
  assert.deepEqual(JSON.parse(JSON.stringify(models[1])), { id: "p2", layers: [] });
});

test("decodeClipFrame rejects malformed frames", () => {
  const b = bridge();
  const enc = new TextEncoder();
  const build = ({ names = ["walk"], layer = [0, 1, 1, 0], layerCount = 1, trailing = [] } = {}) => {
    const out = [];
    const u16 = (v) => out.push(v & 255, v >> 8);
    const f32 = (v) => { const d = new DataView(new ArrayBuffer(4)); d.setFloat32(0, v, true); out.push(...new Uint8Array(d.buffer)); };
    const id = (v) => { const c = enc.encode(v); u16(c.length); out.push(...c); };
    out.push(71, 83, 80, 52); u16(names.length); names.forEach(id); u16(1); id("p1"); out.push(layerCount);
    for (let i = 0; i < layerCount; i++) { u16(layer[0]); f32(layer[1]); f32(layer[2]); out.push(layer[3]); }
    out.push(...trailing);
    return Uint8Array.from(out);
  };
  assert.equal(b.decodeClipFrame(build()).length, 1);
  assert.throws(() => b.decodeClipFrame(build().subarray(0, -1)), /truncated/);
  assert.throws(() => b.decodeClipFrame(build({ trailing: [0] })), /trailing/);
  assert.throws(() => b.decodeClipFrame(build({ layer: [5, 1, 1, 0] })), /unknown/);
  assert.throws(() => b.decodeClipFrame(build({ layer: [0, NaN, 1, 0] })), /non-finite/);
  assert.throws(() => b.decodeClipFrame(build({ layer: [0, -1, 1, 0] })), /negative/);
  assert.throws(() => b.decodeClipFrame(build({ layer: [0, 1, -1, 0] })), /negative/);
  assert.throws(() => b.decodeClipFrame(build({ layer: [0, 1, 1, 2] })), /flags/);
  assert.throws(() => b.decodeClipFrame(build({ layerCount: 5 })), /too many/);
  const wrongMagic = build(); wrongMagic[3] = 51;
  assert.throws(() => b.decodeClipFrame(wrongMagic), /version/);
  assert.throws(() => b.decodeClipFrame("nope"), /ArrayBuffer/);
});

test("applyMountedClipFrame counts accepted frames, renders once, and reports rejections", () => {
  const b = bridge();
  const handle = {};
  const renders = [];
  const ok = b.applyMountedClipFrame({}, [], { applyClipFrame: () => ({ applied: true }) }, null, (r) => renders.push(r), handle);
  assert.deepEqual(JSON.parse(JSON.stringify(ok)), { applied: true, binary: true, pending: false });
  assert.deepEqual(renders, ["clip-frame"]);
  assert.equal(handle.__gosxClipFrameStats.accepted, 1);
  assert.throws(() => b.applyMountedClipFrame({}, [], { applyClipFrame: () => ({ applied: false, reason: "unknown-model", id: "ghost" }) }, null, () => assert.fail("no render"), handle), /unknown-model \(ghost\)/);
  assert.equal(handle.__gosxClipFrameStats.rejected["unknown-model"], 1);
  assert.throws(() => b.applyMountedClipFrame({}, [], undefined, null, () => assert.fail("no render"), handle), /renderer-not-ready/);
});

// Same bytes as clipFrameGoldenHex in scene/clip_frame_test.go.
test("decodeClipFrame reads the bytes the Go encoder writes", () => {
  const hex = "475350340100040077616c6b0200020070310200000000c03f0000803e0103800000003f0000403f000200703200";
  const models = bridge().decodeClipFrame(Uint8Array.from(Buffer.from(hex, "hex")));
  assert.deepEqual(JSON.parse(JSON.stringify(models)), [
    { id: "p1", layers: [
      { name: "walk", index: 0, time: 1.5, weight: 0.25, loop: true },
      { name: "", index: 3, time: 0.5, weight: 0.75, loop: false },
    ] },
    { id: "p2", layers: [] },
  ]);
});

// --- allocation-free steady state ---------------------------------------------

// A seeded generator keeps these tests reproducible.
function lcg(seed) { let x = seed >>> 0; return () => ((x = (Math.imul(x, 1664525) + 1013904223) >>> 0) / 4294967296); }

test("the cached key search returns exactly the index a scan from zero finds", () => {
  const { api, context } = animationContext();
  const seek = vm.runInContext("sceneAnimSeekChannel", context);
  const rnd = lcg(7);
  // Irregular times, including duplicates, so boundary and tie handling is exercised.
  const times = Float32Array.from([0, 0.1, 0.1, 0.25, 0.5, 0.5, 0.75, 1, 1.5, 2, 2.0625, 3]);
  const channel = { times, values: new Float32Array(times.length * 3), property: "translation", interpolation: "LINEAR" };
  const scan = (t) => { let i = 0; while (i < times.length - 1 && times[i + 1] < t) i++; return i; };
  let time = 1;
  for (let n = 0; n < 20000; n++) {
    const mode = rnd();
    if (mode < 0.5) time += (rnd() - 0.45) * 0.05;       // small steps both ways
    else if (mode < 0.7) time = times[Math.floor(rnd() * times.length)]; // exactly on a key
    else time = rnd() * 3.2;                              // arbitrary jump
    time = Math.min(3.2, Math.max(-0.1, time));
    seek(channel, time);
    if (time > times[0] && time < times[times.length - 1]) assert.equal(channel._lastIndex, scan(time), `t=${time}`);
  }
  void api;
});

function poseFixture(options = {}) {
  const { api, context } = animationContext();
  const { nodes, skins, clips } = rig();
  const clipList = clips();
  const skin = { joints: skins[0].joints, inverseBindMatrices: skins[0].inverseBindMatrices };
  const record = { rootNodes: [0], skins: [skin], rootTransform: options.root || null, sampledLayers: [], sampledLayerCount: 0 };
  const sampler = api.createClipSampler(clipList, { nodes, skins: [skin], rootNodes: [0], rootMotion: options.rootMotion === true });
  return { api, context, nodes, skin, record, sampler };
}

test("the allocation-free pose path is bit-equal to the general node-transform path", () => {
  const rootMatrix = Float32Array.from([0.9, 0, 0.1, 0, 0, 1.1, 0, 0, -0.1, 0, 0.9, 0, 3, -1, 2, 1]);
  const rnd = lcg(11);
  for (const rootMotion of [false, true]) for (const root of [null, rootMatrix]) {
    const fast = poseFixture({ rootMotion, root });
    const slow = poseFixture({ rootMotion, root });
    const output = new Map();
    for (let n = 0; n < 300; n++) {
      const count = Math.floor(rnd() * 5);
      const layers = Array.from({ length: count }, () => layer(rnd() < 0.5 ? "walk" : rnd() < 0.5 ? "pop" : 0, rnd() * 3, rnd() < 0.2 ? 0 : 0.1 + rnd() * 2, rnd() < 0.5));
      fast.record.sampledLayers = layers; fast.record.sampledLayerCount = layers.length;
      assert.equal(fast.sampler.pose(fast.record), true);
      output.clear();
      slow.sampler.sample(layers, layers.length, output);
      const world = slow.api.buildNodeTransforms(slow.nodes, output, root, [0]);
      const expected = slow.api.computeJointMatrices(slow.skin, world);
      assert.deepEqual(Array.from(fast.skin.jointMatrices), Array.from(expected), `rootMotion=${rootMotion} root=${!!root} frame ${n}`);
    }
  }
});

test("pose declines models with morph or rigid node-animation targets", () => {
  const f = poseFixture();
  f.record.morphTargets = [{}];
  assert.equal(f.sampler.pose(f.record), false);
  assert.equal(f.skin.jointMatrices, undefined, "nothing written");
});

// The probe runs in a child process with a 64 MB young generation, so no
// scavenge happens during the measurement and heapUsed growth is the number of
// bytes allocated. It applies 5000 two-model, four-layer frames (decode, sample,
// blend, node and joint matrices) and runs the general Map-based path as a
// control that must allocate, so a zero cannot come from a broken probe.
test("applying clip frames allocates (almost) nothing in steady state", () => {
  const probe = fileURLToPath(new URL("./scene3d-clip-frame-alloc.probe.mjs", import.meta.url));
  const out = execFileSync(process.execPath, ["--expose-gc", "--min-semi-space-size=64", "--max-semi-space-size=64", probe], { encoding: "utf8", timeout: 120000 });
  const { applyBytes, controlBytes, frames } = JSON.parse(out.trim().split("\n").pop());
  assert.equal(frames, 5000);
  assert.ok(controlBytes > 50 * 1024 * frames / 10, `probe sanity: the general path allocated only ${controlBytes} bytes`);
  // Budget: 1 KiB per frame (two models), against about 28 KiB for the general path.
  assert.ok(applyBytes < 1024 * frames, `steady-state apply allocated ${applyBytes} bytes over ${frames} frames (control: ${controlBytes})`);
});

test("the clip decoder interns strings and reuses its result objects", () => {
  const decode = bridge().createClipDecoder();
  const hex = "475350340100040077616c6b0200020070310200000000c03f0000803e0103800000003f0000403f000200703200";
  const bytes = Uint8Array.from(Buffer.from(hex, "hex"));
  const first = decode(bytes);
  const layers = first[0].layers;
  const id = first[0].id;
  const second = decode(bytes);
  assert.equal(second, first, "same models array");
  assert.equal(second[0].layers, layers, "same layers array");
  assert.equal(second[0].layers[0], layers[0], "same layer objects");
  assert.equal(second[0].id, id);
  assert.throws(() => decode(Uint8Array.from([71, 83, 80, 52, 0, 0, 2, 0, 2, 0, 112, 49, 0, 2, 0, 112, 49, 0])), /duplicate/);
});
