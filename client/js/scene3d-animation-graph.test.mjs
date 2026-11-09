// Animation graph chunk (GSA1 frames from motion/animgraph): decoding, layer
// blending with masks and additive layers, root motion, IK, humanoid bone
// mapping, retargeting, and the dispatch path through the mount's clip-frame
// hook. Expected values are derived by hand in comments, not by calling the
// code under test.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const transpile = (source) => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const read = (...parts) => fs.readFileSync(path.join(here, ...parts), "utf8");

export function loadChunk(extra = {}) {
  const sandbox = { console: { warn() {}, error() {}, log() {} }, TextDecoder, performance, ...extra };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.document = { getElementById: () => null };
  const context = vm.createContext(sandbox);
  const source = [
    read("bootstrap-src", "26n-feature-scene3d-animation-graph-prefix.ts"),
    read("..", "runtime", "scene3d", "animation-rig.ts"),
    read("..", "runtime", "scene3d", "ik.ts"),
    read("..", "runtime", "scene3d", "animation-graph.ts"),
    read("bootstrap-src", "26n-feature-scene3d-animation-graph-suffix.ts"),
  ].join("\n");
  vm.runInContext(transpile(source), context, { filename: "bootstrap-feature-scene3d-animation-graph.js" });
  return { sandbox, context, api: sandbox.__gosx_scene3d_animation_graph_api };
}

// --- GSA1 encoder mirroring motion/animgraph.FrameEncoder ---------------------
export function encodeFrame(models) {
  const names = [];
  for (const m of models) for (const l of m.layers || []) for (const s of l.samples) if (!names.includes(s.name)) names.push(s.name);
  const bytes = [];
  const u8 = (v) => bytes.push(v & 255);
  const u16 = (v) => { u8(v); u8(v >> 8); };
  const u32 = (v) => { u16(v & 0xffff); u16(v >>> 16); };
  const f32 = (v) => { const b = new Uint8Array(new Float32Array([v]).buffer); for (const x of b) u8(x); };
  const str = (s) => { const b = Buffer.from(s, "utf8"); u16(b.length); for (const x of b) u8(x); };
  for (const c of "GSA2") u8(c.charCodeAt(0));
  u16(names.length); names.forEach(str);
  u16(models.length);
  for (const m of models) {
    str(m.id); u32(m.rigHash || 0); f32(m.dt || 0); u8(m.detail || 0);
    u8((m.layers || []).length);
    for (const l of m.layers || []) {
      f32(l.weight); u8(l.samples.length);
      for (const s of l.samples) { u16(names.indexOf(s.name)); f32(s.time); f32(s.weight); u8(s.loop ? 1 : 0); }
    }
    u8((m.ik || []).length);
    for (const k of m.ik || []) {
      const flags = (k.target ? 1 : 0) | (k.pole ? 2 : 0) | (k.ground ? 4 : 0) | (k.bone ? 8 : 0);
      u8(k.chain); u8(flags); f32(k.weight);
      if (k.target) k.target.forEach(f32);
      if (k.pole) k.pole.forEach(f32);
      if (k.ground) { f32(k.ground.height); (k.ground.normal || [0, 1, 0]).forEach(f32); }
      if (k.bone) { u16(k.bone.model); u8(k.bone.index); (k.bone.offset || [0, 0, 0]).forEach(f32); }
    }
    u8((m.look || []).length);
    for (const k of m.look || []) { u8(k.chain); f32(k.weight); k.target.forEach(f32); }
  }
  return Uint8Array.from(bytes);
}

// --- A small humanoid: hips at y=1, legs of two 0.45 m bones, feet at y=0.1 --
// Names are Mixamo style without the prefix (as Meshy exports them).
//   0 Armature            (scene root)
//   1 Hips      (0,1,0)
//   2 LeftUpLeg (0.1,0,0)   3 LeftLeg (0,-0.45,0.03)   4 LeftFoot (0,-0.45,-0.03)
//   5 RightUpLeg(-0.1,0,0)  6 RightLeg(0,-0.45,0.03)   7 RightFoot(0,-0.45,-0.03)
//   8 Spine     (0,0.1,0)   9 Head (0,0.5,0)   10 LeftEye (under Head)
//   11 Spine_twist (unmapped leaf under Spine)   12 LeftToeBase (under LeftFoot)
// The knees sit 3 cm forward, so each leg has a bend plane.
export function humanoid() {
  const nodes = [
    { name: "Armature", children: [1] },
    { name: "Hips", translation: [0, 1, 0], children: [2, 5, 8] },
    { name: "LeftUpLeg", translation: [0.1, 0, 0], children: [3] },
    { name: "LeftLeg", translation: [0, -0.45, 0.03], children: [4] },
    { name: "LeftFoot", translation: [0, -0.45, -0.03] },
    { name: "RightUpLeg", translation: [-0.1, 0, 0], children: [6] },
    { name: "RightLeg", translation: [0, -0.45, 0.03], children: [7] },
    { name: "RightFoot", translation: [0, -0.45, -0.03] },
    { name: "Spine", translation: [0, 0.1, 0], children: [9, 11] },
    { name: "Head", translation: [0, 0.5, 0], children: [10] },
    { name: "LeftEye", translation: [0.03, 0.1, 0.08] },
    { name: "Spine_twist", translation: [0, 0.2, 0] },
  ];
  nodes[4].children = [12];
  nodes.push({ name: "LeftToeBase", translation: [0, -0.05, 0.12] });
  const joints = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12];
  const skins = [{ joints, inverseBindMatrices: new Float32Array(joints.length * 16).map((_, i) => (i % 16 % 5 === 0 ? 1 : 0)) }];
  return { nodes, skins };
}

const s45 = Math.sin(Math.PI / 8), c45 = Math.cos(Math.PI / 8); // half-angle of 45 degrees
// Clips: "Bob" moves the hips (0,1,0) -> (2,1.2,3) over 1 s and turns the
// spine 0 -> 45 degrees about Y; "Lean" turns the spine 45 degrees about X.
export function clips() {
  return [
    { name: "Rig|Bob", duration: 1, channels: [
      { targetNode: 1, property: "translation", interpolation: "LINEAR", times: new Float32Array([0, 1]), values: new Float32Array([0, 1, 0, 2, 1.2, 3]) },
      { targetNode: 8, property: "rotation", interpolation: "LINEAR", times: new Float32Array([0, 1]), values: new Float32Array([0, 0, 0, 1, 0, s45, 0, c45]) },
    ] },
    { name: "Lean", duration: 1, channels: [
      { targetNode: 8, property: "rotation", interpolation: "LINEAR", times: new Float32Array([0, 1]), values: new Float32Array([0, 0, 0, 1, s45, 0, 0, c45]) },
    ] },
    // Detail joints: the eye, the twist bone and the toe turn 45 degrees
    // about X; the spine slides 0.2 forward (a non-root translation).
    { name: "Detail", duration: 1, channels: [10, 11, 12].map((node) => (
      { targetNode: node, property: "rotation", interpolation: "STEP", times: new Float32Array([0]), values: new Float32Array([s45, 0, 0, c45]) }
    )).concat([{ targetNode: 8, property: "translation", interpolation: "STEP", times: new Float32Array([0]), values: new Float32Array([0, 0.1, 0.2]) }]) },
  ];
}

export const RIG = {
  v: 1, humanoid: "auto", bones: {},
  masks: [{ name: "upper", include: ["spine"], exclude: [], weights: {} }],
  layers: [{ name: "base", mask: -1, additive: false, refClip: "", refTime: 0 }, { name: "lean", mask: 0, additive: true, refClip: "", refTime: 0 }],
  ik: [
    { name: "leftFoot", root: "leftUpperLeg", mid: "leftLowerLeg", end: "leftFoot", foot: true, align: true },
    { name: "rightFoot", root: "rightUpperLeg", mid: "rightLowerLeg", end: "rightFoot", foot: true, align: false },
  ],
  lookAt: [{ name: "head", bones: ["spine", "head"], weights: [0.5, 0.5], axis: [0, 0, 1], pivot: "", maxAngle: 1.2 }],
  pelvis: { bone: "hips", maxDrop: 0.4 }, sources: [],
};

function setup(api, frameModel, { rig = RIG, record: overrides = {} } = {}) {
  const { nodes, skins } = humanoid();
  const record = { id: "hero", nodes, rootNodes: [0], skins, model: {}, rootTransform: null, ...overrides };
  api.defineRig(7, JSON.stringify(rig));
  const m = api.createModel(record, clips());
  const state = {};
  const holder = api.holder(state, "hero");
  const decode = api.createDecoder();
  const frame = decode(encodeFrame([{ id: "hero", rigHash: 7, ...frameModel }]));
  holder.data = api.copyModel(holder.data, frame.models[0]);
  holder.serial++;
  return { m, holder, record, decode, step(next) { holder.data = api.copyModel(holder.data, decode(encodeFrame([{ id: "hero", rigHash: 7, ...next }])).models[0]); holder.serial++; api.evaluate(m, holder); } };
}

const pos = (m, node) => [m.world[node * 16 + 12], m.world[node * 16 + 13], m.world[node * 16 + 14]];
const dist = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);
function near(actual, expected, tol, label) {
  const a = Array.isArray(actual) || ArrayBuffer.isView(actual) ? Array.from(actual) : [actual];
  const e = Array.isArray(expected) ? expected : [expected];
  assert.equal(a.length, e.length, label);
  for (let i = 0; i < a.length; i++) assert.ok(Math.abs(a[i] - e[i]) <= tol, `${label}[${i}]: ${a[i]} vs ${e[i]}`);
}

const base = (samples, extra = {}) => ({ layers: [{ weight: 1, samples }], ...extra });

test("decoder reads the Go encoder's golden frame", () => {
  const { api } = loadChunk();
  const hex = fs.readFileSync(path.join(here, "..", "..", "motion", "animgraph", "testdata", "frame.golden.hex"), "utf8").trim();
  const frame = api.createDecoder()(Uint8Array.from(hex.match(/../g).map((h) => parseInt(h, 16))));
  // The Go fixture names "rival" (joint detail 1) first, then "hero".
  assert.equal(frame.count, 2);
  assert.equal(frame.models[0].id, "rival");
  assert.equal(frame.models[0].detail, 1);
  const m = frame.models[1];
  assert.equal(m.id, "hero");
  // dt is the time stepped since the hero was last sent: 15 steps of 1/60 s.
  near(m.dt, 0.25, 1e-7, "dt");
  assert.equal(m.layerCount, 2);
  // speed 2.7 is halfway between Walk (1.4) and Run (4): 0.5 / 0.5.
  assert.equal(m.layers[0].count, 2);
  assert.equal(m.layers[0].samples[0].name, "Walk");
  assert.equal(m.layers[0].samples[1].name, "Run");
  near([m.layers[0].samples[0].weight, m.layers[0].samples[1].weight], [0.5, 0.5], 1e-6, "blend weights");
  assert.equal(m.layers[0].samples[0].loop, true);
  assert.equal(m.layers[1].samples[0].name, "Idle");
  // leftFoot ground (chain 0, flags 4), leftHand on the rival's right
  // shoulder (chain 2, flags 8), rightHand target+pole (chain 3, flags 3).
  assert.equal(m.ikCount, 3);
  assert.deepEqual([m.ik[0].chain, m.ik[0].flags, m.ik[0].height], [0, 4, 0.25]);
  assert.deepEqual([m.ik[1].chain, m.ik[1].flags, m.ik[1].weight, m.ik[1].other], [2, 8, 0.75, 0]);
  assert.equal(api.bones[m.ik[1].bone], "rightShoulder", "Go HumanoidBones and the browser bone table agree");
  near(m.ik[1].offset, [0, 0.05, 0.1], 1e-7, "bone offset");
  assert.deepEqual([m.ik[2].chain, m.ik[2].flags, m.ik[2].weight], [3, 3, 0.5]);
  assert.deepEqual(Array.from(m.ik[2].target), [0.5, 1.25, 0.75]);
  assert.deepEqual(Array.from(m.ik[2].pole), [0.5, 1, -1]);
  assert.equal(m.lookCount, 1);
  assert.deepEqual(Array.from(m.look[0].target), [0, 1.5, 4]);
});

test("decoder rejects malformed frames", () => {
  const { api } = loadChunk();
  const decode = api.createDecoder();
  const good = encodeFrame([{ id: "a", ...base([{ name: "Bob", time: 0.5, weight: 1, loop: false }]) }]);
  decode(good);
  assert.throws(() => decode(good.subarray(0, good.length - 1)), /truncated/);
  assert.throws(() => decode(Uint8Array.from([...good, 0])), /trailing/);
  const bad = good.slice(); bad[3] = 49; // "GSA1", the previous layout
  assert.throws(() => decode(bad), /version/);
  assert.throws(() => decode(encodeFrame([{ id: "a" }, { id: "a" }])), /duplicate/);
  assert.throws(() => decode(encodeFrame([{ id: "a", layers: Array.from({ length: 9 }, () => ({ weight: 1, samples: [] })) }])), /too many animation graph layers/);
  assert.throws(() => decode(encodeFrame([{ id: "a", ...base([{ name: "Bob", time: -1, weight: 1 }]) }])), /invalid animation graph sample/);
});

test("single sample poses the rig and resolves Blender action names", () => {
  const { api } = loadChunk();
  // "Bob" matches the clip named "Rig|Bob". At 0.5 s the hips sit at
  // (1, 1.1, 1.5); root motion is off, so X and Z return to the authored 0.
  const { m, holder } = setup(api, base([{ name: "Bob", time: 0.5, weight: 1, loop: false }]));
  api.evaluate(m, holder);
  near(pos(m, 1), [0, 1.1, 0], 1e-6, "hips");
  // Spine turned 22.5 degrees about Y: the head offset (0,0.5,0) is on the axis, unchanged.
  near(pos(m, 9), [0, 1.7, 0], 1e-6, "head");
  near(holder.feedback.subarray(0, 3), [7, 2, 1], 0, "feedback header");
});

test("root motion keeps clip X/Z translation when the model opts in", () => {
  const { api } = loadChunk();
  const { m, holder } = setup(api, base([{ name: "Bob", time: 0.5, weight: 1 }]), { record: { model: { rootMotion: true } } });
  api.evaluate(m, holder);
  near(pos(m, 1), [1, 1.1, 1.5], 1e-6, "hips with root motion");
});

test("samples blend by weight within a layer", () => {
  const { api } = loadChunk();
  // Bob at t=0 (hips y 1) and t=1 (hips y 1.2), weights 1 and 3: y = 1 + 0.2 * 3/4.
  const { m, holder } = setup(api, base([{ name: "Bob", time: 0, weight: 1 }, { name: "Bob", time: 1, weight: 3 }]));
  api.evaluate(m, holder);
  near(pos(m, 1)[1], 1.15, 1e-6, "hips y");
});

test("an additive layer inside its mask adds rotation to the base pose", () => {
  const { api } = loadChunk();
  // Base: Bob at 1 s turns the spine 45 degrees about Y. Layer 1 (additive,
  // mask = spine subtree) adds Lean at 1 s: 45 degrees about X relative to
  // Lean's first frame (identity). Spine local = Ry(45) * Rx(45), so the
  // head offset (0, 0.5, 0) becomes Ry(45) Rx(45) (0,0.5,0) =
  // Ry(45) (0, 0.3536, 0.3536) = (0.25, 0.3536, 0.25).
  const frame = { layers: [
    { weight: 1, samples: [{ name: "Bob", time: 1, weight: 1 }] },
    { weight: 1, samples: [{ name: "Lean", time: 1, weight: 1 }] },
  ] };
  const { m, holder } = setup(api, frame);
  api.evaluate(m, holder);
  const spine = pos(m, 8), head = pos(m, 9);
  near([head[0] - spine[0], head[1] - spine[1], head[2] - spine[2]], [0.25, 0.5 * Math.SQRT1_2, 0.25], 1e-5, "head offset");
  // Half layer weight: 22.5 degrees about X on top of the base.
  const half = setup(api, { layers: [frame.layers[0], { weight: 0.5, samples: frame.layers[1].samples }] });
  api.evaluate(half.m, half.holder);
  const h = pos(half.m, 9), s = pos(half.m, 8);
  const lean = Math.PI / 8, y = 0.5 * Math.cos(lean), z = 0.5 * Math.sin(lean);
  near([h[0] - s[0], h[1] - s[1], h[2] - s[2]], [z * Math.SQRT1_2, y, z * Math.SQRT1_2], 1e-5, "half lean");
});

test("an override layer only touches bones inside its mask", () => {
  const { api } = loadChunk();
  const rig = { ...RIG, layers: [RIG.layers[0], { ...RIG.layers[1], additive: false }] };
  // Layer 1 plays Bob (hips + spine) but its mask is the spine subtree: the
  // spine turns, the hips keep the authored height 1 (base layer is empty).
  const { m, holder } = setup(api, { layers: [{ weight: 1, samples: [] }, { weight: 1, samples: [{ name: "Bob", time: 1, weight: 1 }] }] }, { rig });
  api.evaluate(m, holder);
  near(pos(m, 1), [0, 1, 0], 1e-6, "hips untouched");
  assert.ok(m.flags[8] & 2, "spine rotation applied");
  assert.equal(m.flags[1], 0, "hips not animated");
});

test("two-bone IK reaches the target, keeps bone lengths, and bends toward the pole", () => {
  const { api } = loadChunk();
  const { m, holder } = setup(api, base([]));
  api.evaluate(m, holder);
  const l1 = dist(pos(m, 2), pos(m, 3)), l2 = dist(pos(m, 3), pos(m, 4));
  // Pull the left foot up and forward: reachable (distance 0.6 < 0.9).
  api.solveTwoBone(m, 2, 3, 4, 0.1, 0.55, 0.3, null, 1);
  near(pos(m, 4), [0.1, 0.55, 0.3], 1e-5, "foot at target");
  near(dist(pos(m, 2), pos(m, 3)), l1, 1e-5, "thigh length");
  near(dist(pos(m, 3), pos(m, 4)), l2, 1e-5, "shin length");
  assert.ok(pos(m, 3)[2] > 0.3, "knee bends forward, as animated");
  // A pole behind the leg flips the knee backward.
  const second = setup(api, base([]));
  api.evaluate(second.m, second.holder);
  api.solveTwoBone(second.m, 2, 3, 4, 0.1, 0.55, 0.3, new Float64Array([0.1, 0.6, -2]), 1);
  near(pos(second.m, 4), [0.1, 0.55, 0.3], 1e-5, "foot at target with pole");
  assert.ok(pos(second.m, 3)[2] < 0.1, "knee bends toward the pole");
  // Out of reach: the chain points straight at the target.
  const third = setup(api, base([]));
  api.evaluate(third.m, third.holder);
  api.solveTwoBone(third.m, 2, 3, 4, 0.1, 1, 5, null, 1);
  const hip = pos(third.m, 2), foot = pos(third.m, 4);
  near(dist(hip, foot), l1 + l2, 1e-3, "fully extended");
  near((foot[2] - hip[2]) / dist(hip, foot), 1, 1e-3, "pointing at the target");
});

test("foot placement lifts a foot onto higher ground and drops the pelvis for lower ground", () => {
  const { api } = loadChunk();
  // Left foot over a 0.2 m step. The animated foot is 0.1 m above the
  // origin plane, so it lands at 0.3; the right foot keeps 0.1.
  const up = setup(api, base([], { ik: [{ chain: 0, weight: 1, ground: { height: 0.2 } }] }));
  api.evaluate(up.m, up.holder);
  near(pos(up.m, 4), [0.1, 0.3, 0], 1e-5, "left foot on the step");
  near(pos(up.m, 7), [-0.1, 0.1, 0], 1e-6, "right foot unchanged");
  near(pos(up.m, 1), [0, 1, 0], 1e-6, "pelvis unchanged");
  const fb = up.holder.feedback;
  near(fb.subarray(3, 9), [0.1, 0.1, 0, 0.1, 0.3, 0], 1e-5, "left feedback: animated then solved");
  // Right foot over a 0.1 m dip: the first frame snaps the pelvis down 0.1,
  // the right leg reaches 0.0, and the left foot stays at 0.1.
  const down = setup(api, base([], { ik: [{ chain: 0, weight: 1, ground: { height: 0 } }, { chain: 1, weight: 1, ground: { height: -0.1 } }] }));
  api.evaluate(down.m, down.holder);
  near(pos(down.m, 1), [0, 0.9, 0], 1e-6, "pelvis dropped");
  near(pos(down.m, 7), [-0.1, 0, 0], 1e-5, "right foot in the dip");
  near(pos(down.m, 4), [0.1, 0.1, 0], 1e-5, "left foot stays planted");
  // Later frames ease: with dt = 1/60 and a 0.06 s time constant, the pelvis
  // moves 1 - exp(-0.2778) = 24.25% of the way back toward 0.
  down.step(base([], { dt: 1 / 60, ik: [{ chain: 0, weight: 1, ground: { height: 0 } }, { chain: 1, weight: 1, ground: { height: 0 } }] }));
  near(pos(down.m, 1)[1], 0.9 + 0.1 * (1 - Math.exp(-(1 / 60) / 0.06)), 1e-6, "pelvis eases back");
});

test("foot alignment tilts the foot to the ground normal", () => {
  const { api } = loadChunk();
  // A 30 degree slope rising toward +Z: normal (0, cos30, -sin30). The foot's
  // up axis (column 1 of its world matrix) turns to that normal.
  const n = [0, Math.cos(Math.PI / 6), -Math.sin(Math.PI / 6)];
  const { m, holder } = setup(api, base([], { ik: [{ chain: 0, weight: 1, ground: { height: 0, normal: n } }] }));
  api.evaluate(m, holder);
  const w = m.world, o = 4 * 16;
  near([w[o + 4], w[o + 5], w[o + 6]], n, 1e-5, "foot up axis");
});

test("look-at turns the head toward a target within the angle limit", () => {
  const { api } = loadChunk();
  // Target 45 degrees to the right (+X, +Z) at head height: under the 1.2
  // rad limit, so the head forward (+Z) ends on the target direction.
  const right = setup(api, base([], { look: [{ chain: 0, weight: 1, target: [5, 1.6, 5] }] }));
  api.evaluate(right.m, right.holder);
  const fb = right.holder.feedback, at = 3 + 9 * 2;
  near(fb.subarray(at + 3, at + 6), [Math.SQRT1_2, 0, Math.SQRT1_2], 1e-3, "forward faces the target");
  // Directly behind is 180 degrees: clamped to 1.2 rad from forward.
  const behind = setup(api, base([], { look: [{ chain: 0, weight: 1, target: [0.001, 1.6, -5] }] }));
  api.evaluate(behind.m, behind.holder);
  const f = behind.holder.feedback.subarray(at + 3, at + 6);
  near(Math.acos(f[2]), 1.2, 1e-3, "clamped angle");
});

test("humanoid mapping reads common naming schemes", () => {
  const { api } = loadChunk();
  const chain = (names) => names.map((name, i) => ({ name, children: i + 1 < names.length ? [i + 1] : [] }));
  // Each case: a parent chain of names and the canonical bones expected in order.
  const cases = {
    mixamo: [["mixamorig:Hips", "mixamorig:Spine", "mixamorig:Spine1", "mixamorig:Spine2", "mixamorig:Neck", "mixamorig:Head", "mixamorig:HeadTop_End"],
      ["hips", "spine", "chest", "upperChest", "neck", "head", null]],
    mixamoArm: [["mixamorig:LeftShoulder", "mixamorig:LeftArm", "mixamorig:LeftForeArm", "mixamorig:LeftHand", "mixamorig:LeftHandIndex1", "mixamorig:LeftHandIndex2", "mixamorig:LeftHandIndex4"],
      ["leftShoulder", "leftUpperArm", "leftLowerArm", "leftHand", "leftIndexProximal", "leftIndexIntermediate", null]],
    mixamoLeg: [["Hips", "RightUpLeg", "RightLeg", "RightFoot", "RightToeBase", "RightToe_End"], ["hips", "rightUpperLeg", "rightLowerLeg", "rightFoot", "rightToes", null]],
    rigifyDEF: [["DEF-spine", "DEF-spine.001", "DEF-spine.002", "DEF-spine.003", "DEF-spine.004", "DEF-spine.005", "DEF-spine.006"],
      ["hips", "spine", "chest", "upperChest", "neck", null, "head"]],
    rigifyLeg: [["DEF-thigh.L", "DEF-thigh.L.001", "DEF-shin.L", "DEF-shin.L.001", "DEF-foot.L", "DEF-toe.L"], ["leftUpperLeg", null, "leftLowerLeg", null, "leftFoot", "leftToes"]],
    rigifyHand: [["DEF-hand.R", "DEF-thumb.01.R", "DEF-thumb.02.R", "DEF-thumb.03.R"], ["rightHand", "rightThumbMetacarpal", "rightThumbProximal", "rightThumbDistal"]],
    unreal: [["pelvis", "spine_01", "spine_02", "spine_03", "clavicle_l", "upperarm_l", "lowerarm_l", "hand_l", "thumb_01_l"],
      ["hips", "spine", "chest", "upperChest", "leftShoulder", "leftUpperArm", "leftLowerArm", "leftHand", "leftThumbMetacarpal"]],
    unrealLeg: [["thigh_r", "calf_r", "foot_r", "ball_r"], ["rightUpperLeg", "rightLowerLeg", "rightFoot", "rightToes"]],
    biped: [["Bip01 Pelvis", "Bip01 Spine", "Bip01 L Thigh", "Bip01 L Calf", "Bip01 L Foot", "Bip01 L Toe0"], ["hips", "spine", "leftUpperLeg", "leftLowerLeg", "leftFoot", "leftToes"]],
    blender: [["pelvis", "spine", "chest", "neck", "head", "upper_arm.L", "forearm.L", "hand.L"], ["hips", "spine", "chest", "neck", "head", "leftUpperArm", "leftLowerArm", "leftHand"]],
    shortCamel: [["root", "torso", "lLeg", "lShin", "lFoot"], [null, "spine", "leftUpperLeg", "leftLowerLeg", "leftFoot"]],
    controls: [["MCH-thigh_ik.L", "thigh_fk.L", "ORG-thigh.L"], [null, null, "leftUpperLeg"]],
  };
  for (const [label, [names, want]] of Object.entries(cases)) {
    const nodes = chain(names);
    const map = api.mapHumanoid(nodes, [], "auto", null);
    const got = names.map((_, i) => (map.nodeBone[i] >= 0 ? api.bones[map.nodeBone[i]] : null));
    assert.deepEqual(got, want, label);
  }
  assert.equal(api.mapHumanoid(chain(cases.mixamo[0]), [], "auto", null).scheme, "mixamo");
  assert.equal(api.mapHumanoid(chain(cases.rigifyDEF[0]), [], "auto", null).scheme, "rigify");
  assert.equal(api.mapHumanoid(chain(cases.unreal[0]), [], "auto", null).scheme, "unreal");
  // Overrides win; "none" maps only overrides.
  const overridden = api.mapHumanoid(chain(cases.shortCamel[0]), [], "none", { hips: "root" });
  assert.equal(overridden.bone.get("hips"), 0);
  assert.equal(overridden.bone.has("spine"), false);
});

// Build a two-bone arm whose bones carry arbitrary rest rotations (as a
// Blender bone roll does) while the joints sit at the same places as an
// unrotated rig. Retargeting must give the same world motion on both.
function rolledArm(roll) {
  // Unrotated rig: Hips (0,1,0) > LeftArm (0.2,0.4,0) > LeftForeArm +0.3 x > LeftHand +0.25 x.
  const q = (ax, ay, az, angle) => { const s = Math.sin(angle / 2), l = Math.hypot(ax, ay, az); return [ax / l * s, ay / l * s, az / l * s, Math.cos(angle / 2)]; };
  const mul = (a, b) => [a[3] * b[0] + a[0] * b[3] + a[1] * b[2] - a[2] * b[1], a[3] * b[1] - a[0] * b[2] + a[1] * b[3] + a[2] * b[0], a[3] * b[2] + a[0] * b[1] - a[1] * b[0] + a[2] * b[3], a[3] * b[3] - a[0] * b[0] - a[1] * b[1] - a[2] * b[2]];
  const conj = (a) => [-a[0], -a[1], -a[2], a[3]];
  const rot = (a, v) => { const p = mul(mul(a, [v[0], v[1], v[2], 0]), conj(a)); return [p[0], p[1], p[2]]; };
  const rolls = roll ? [q(0, 1, 0, 0.7), q(1, 0, 0, -1.1), q(0, 0, 1, 2.0), q(1, 1, 0, 0.4)] : [[0, 0, 0, 1], [0, 0, 0, 1], [0, 0, 0, 1], [0, 0, 0, 1]];
  // World rest rotation of bone i is rolls[i]; local = conj(parent world) * world.
  const offsets = [[0, 1, 0], [0.2, 0.4, 0], [0.3, 0, 0], [0.25, 0, 0]];
  const names = roll ? ["DEF-spine", "DEF-upper_arm.L", "DEF-forearm.L", "DEF-hand.L"] : ["mixamorig:Hips", "mixamorig:LeftArm", "mixamorig:LeftForeArm", "mixamorig:LeftHand"];
  const nodes = names.map((name, i) => {
    const parentWorld = i > 0 ? rolls[i - 1] : [0, 0, 0, 1];
    return { name, rotation: mul(conj(parentWorld), rolls[i]), translation: rot(conj(parentWorld), offsets[i]), children: i < 3 ? [i + 1] : [] };
  });
  if (roll) nodes[0].scale = [1, 1, 1];
  return { nodes, rolls, mul, conj, q };
}

test("retargeting transfers rest-relative rotation between rigs with different bone axes", () => {
  const { api } = loadChunk();
  const source = rolledArm(false), target = rolledArm(true);
  // Source clip: the upper arm turns 60 degrees about world Z at t=1 (its rest
  // rotation is identity, so local = world delta).
  const turn = source.q(0, 0, 1, Math.PI / 3);
  const sourceClips = [{ name: "Raise", duration: 1, channels: [
    { targetNode: 1, property: "rotation", interpolation: "LINEAR", times: new Float32Array([0, 1]), values: new Float32Array([0, 0, 0, 1, ...turn]) },
    { targetNode: 0, property: "translation", interpolation: "LINEAR", times: new Float32Array([0, 1]), values: new Float32Array([0, 1, 0, 0, 1.5, 0]) },
  ] }];
  const sMap = api.mapHumanoid(source.nodes, [], "auto", null), tMap = api.mapHumanoid(target.nodes, [], "auto", null);
  const retargeted = api.retargetClips({ nodes: source.nodes, animations: sourceClips }, sMap, { nodes: target.nodes }, tMap, "mx_");
  assert.equal(retargeted[0].name, "mx_Raise");
  const record = (nodes) => ({ id: "x", nodes, rootNodes: [0], skins: [], model: { rootMotion: true }, rootTransform: null });
  const evalAt = (nodes, clipList, time) => {
    const m = api.createModel(record(nodes), clipList);
    const holder = api.holder({}, "x");
    holder.data = api.copyModel(holder.data, api.createDecoder()(encodeFrame([{ id: "x", ...base([{ name: clipList[0].name, time, weight: 1 }]) }])).models[0]);
    api.evaluate(m, holder);
    return m;
  };
  const a = evalAt(source.nodes, sourceClips, 1), b = evalAt(target.nodes, retargeted, 1);
  for (const node of [1, 2, 3]) near(pos(b, node), pos(a, node), 1e-5, `joint ${node} world position`);
  // The hand has moved: 60 degrees about Z lifts the forearm off the X axis.
  assert.ok(pos(a, 3)[1] - pos(a, 1)[1] > 0.4, "the arm raised");
  near(pos(b, 0), [0, 1.5, 0], 1e-5, "hips translation transfers (same hip height)");
});

test("dispatch routes frames through the mount's clip-frame hook and reports feedback", () => {
  // A real command runtime, a stand-in animation API, and a mount handle that
  // forwards applyClipFrame exactly as mount.ts does.
  const { sandbox, api } = loadChunk();
  const sampled = [];
  sandbox.__gosx_scene3d_animation_api = {
    configureSampledRecord(state, record) { record.clipSampler = { pose() { sampled.push("default"); return true; }, sample(_l, _c, out) { return out; } }; },
    applyClipFrame() { return { applied: true, pending: false }; },
  };
  const commandSource = transpile(read("..", "runtime", "scene3d", "command-runtime.ts"));
  vm.runInContext(commandSource, vm.createContext(sandbox));
  const { nodes, skins } = humanoid();
  const record = { id: "hero", nodes, rootNodes: [0], skins, model: {}, rootTransform: null };
  const state = { _modelSkins: [record] };
  sandbox.__gosx_scene3d_animation_api.configureSampledRecord(state, record, clips(), {});
  const posed = [];
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands() {},
    applyClipFrame(models) {
      return sandbox.__gosx_scene3d_command_bridge.applyMountedClipFrame(state, models, sandbox.__gosx_scene3d_animation_api, (rec) => { posed.push(rec.id); rec.clipSampler.pose(rec); }, () => {}, handle);
    },
  };
  sandbox.__gosx.engines = new Map([["scene", { handle }]]);
  const scene3d = sandbox.__gosx.scene3d;
  assert.equal(scene3d.defineAnimationRig(7, JSON.stringify(RIG)), true);
  // The record predates the chunk's capture of its clips, and no glTF loader is
  // present, so the first frame falls back to the default sampler.
  const bytes = encodeFrame([{ id: "hero", rigHash: 7, ...base([{ name: "Bob", time: 0.5, weight: 1 }], { ik: [{ chain: 0, weight: 1, ground: { height: 0.2 } }] }) }]);
  record.__animGraphClips = clips();
  const result = scene3d.dispatchAnimationFrame("scene", bytes);
  assert.deepEqual({ ...result }, { applied: true, pending: false });
  assert.deepEqual(posed, ["hero"]);
  assert.equal(record.clipSampler.__animGraph, true);
  assert.ok(ArrayBuffer.isView(skins[0].jointMatrices) && skins[0].jointMatrices.length === 12 * 16, "joint matrices written");
  const out = new Float64Array(64);
  const count = scene3d.animationFeedback("scene", "hero", out);
  assert.equal(count, 3 + 9 * 2 + 6 + 4);
  assert.equal(out[0], 7);
  // Left foot solved at 0.1 + 0.2 above the origin plane; hips at 1.1 (Bob at 0.5 s).
  near(out.subarray(6, 9), [0.1, 0.4, 0], 1e-5, "solved left foot");
  // Unknown model IDs are rejected without touching the mount.
  const unknown = scene3d.dispatchAnimationFrame("scene", encodeFrame([{ id: "nobody", ...base([]) }]));
  assert.equal(unknown.applied, false);
  assert.match(unknown.reason, /unknown-model/);
  // Plain GSP4 clip frames still reach the original hook.
  assert.deepEqual(sandbox.__gosx_scene3d_animation_api.applyClipFrame(state, [], () => {}), { applied: true, pending: false });
  // An unknown target is not ready.
  assert.equal(scene3d.dispatchAnimationFrame("missing", bytes).reason, "not-ready");
});

test("the animation API hook captures clips for records created after the chunk loads", () => {
  const { sandbox } = loadChunk();
  const captured = [];
  sandbox.__gosx_scene3d_animation_api = { configureSampledRecord(_s, record) { record.clipSampler = { pose() { return true; }, sample() {} }; captured.push(record.id); }, applyClipFrame() {} };
  const record = { id: "late" };
  const list = clips();
  sandbox.__gosx_scene3d_animation_api.configureSampledRecord({}, record, list, {});
  assert.deepEqual(captured, ["late"]);
  assert.equal(record.__animGraphClips, list);
});

test("a rig defined after the first frame binds on the next frame", () => {
  const { api } = loadChunk();
  const { nodes, skins } = humanoid();
  const record = { id: "hero", nodes, rootNodes: [0], skins, model: {}, rootTransform: null };
  const m = api.createModel(record, clips());
  const holder = api.holder({}, "hero");
  const decode = api.createDecoder();
  const frame = encodeFrame([{ id: "hero", rigHash: 99, ...base([], { ik: [{ chain: 0, weight: 1, ground: { height: 0.2 } }] }) }]);
  holder.data = api.copyModel(holder.data, decode(frame).models[0]);
  holder.serial++;
  api.evaluate(m, holder);
  assert.equal(holder.feedback[0], 0, "no rig yet: feedback reports rig 0");
  near(pos(m, 4), [0.1, 0.1, 0], 1e-6, "no IK without a rig");
  api.defineRig(99, JSON.stringify(RIG));
  holder.data = api.copyModel(holder.data, decode(frame).models[0]);
  holder.serial++;
  api.evaluate(m, holder);
  assert.equal(holder.feedback[0], 99, "rig bound");
  near(pos(m, 4), [0.1, 0.3, 0], 1e-5, "foot IK applies once the rig is known");
});

test("joint detail fades fingers, eyes and twist bones out without snapping", () => {
  const { api } = loadChunk();
  const play = (detail, dt = 0.1) => base([{ name: "Detail", time: 0, weight: 1, loop: true }], { detail, dt });
  const { m, holder, step } = setup(api, play(0));
  api.evaluate(m, holder);
  // Classes: the eye is class 1, the twist bone and the toe class 2.
  assert.deepEqual([m.nodeClass[10], m.nodeClass[11], m.nodeClass[12], m.nodeClass[8]], [1, 2, 2, 0]);
  const angleX = (node) => 2 * Math.atan2(m.r[node * 4], m.r[node * 4 + 3]);
  near([angleX(10), angleX(11), angleX(12)], [Math.PI / 4, Math.PI / 4, Math.PI / 4], 1e-6, "all detail joints animate at detail 0");
  // Detail 1: the eye (class 1) eases out by 0.1 / 0.25 per 0.1 s frame.
  step(play(1));
  near(angleX(10), 0.6 * Math.PI / 4, 1e-3, "eye at 60% after one frame");
  near(angleX(11), Math.PI / 4, 1e-6, "class 2 untouched at detail 1");
  step(play(1)); step(play(1));
  near(angleX(10), 0, 1e-9, "eye at rest after 0.3 s");
  assert.equal(m.flags[10], 0, "a faded joint is not sampled");
  // Detail 2: class 2 fades too; coming back up fades in the same way.
  step(play(2)); step(play(2)); step(play(2));
  near([angleX(11), angleX(12)], [0, 0], 1e-9, "twist and toe at rest");
  step(play(0));
  near(angleX(10), 0.4 * Math.PI / 4, 1e-3, "eye fades back in");
});

test("a frame without new data re-skins the moved model and keeps the pose", () => {
  const { api } = loadChunk();
  const { m, holder, record } = setup(api, base([{ name: "Bob", time: 0.5, weight: 1 }], { ik: [{ chain: 0, weight: 1, ground: { height: 0.2 } }] }));
  api.evaluate(m, holder);
  const footY = pos(m, 4)[1], spineRot = Array.from(m.r.subarray(32, 36));
  const footJoint = 3; // LeftFoot is joint 3 of the skin; its inverse bind is identity
  const before = Array.from(record.skins[0].jointMatrices.subarray(footJoint * 16 + 12, footJoint * 16 + 15));
  near(before, pos(m, 4), 1e-6, "joint matrix places the foot");
  // The model moves 2 m along X (a live transform patch); no new frame.
  record.rootTransform = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 0, 0, 1]);
  api.evaluate(m, holder);
  const after = Array.from(record.skins[0].jointMatrices.subarray(footJoint * 16 + 12, footJoint * 16 + 15));
  near(after, [before[0] + 2, before[1], before[2]], 1e-5, "foot joint carried by the move, IK result kept");
  near(m.r.subarray(32, 36), spineRot, 0, "local pose untouched");
  // A forced evaluation re-solves IK against the moved model: still the
  // step height, because ground height is world space and the model did
  // not move vertically.
  api.evaluate(m, holder, true);
  near(pos(m, 4), [2.1, footY, 0], 1e-5, "forced re-evaluation agrees");
  // Without IK, a rotated, scaled and raised placement re-skins to exactly
  // what a full pass computes.
  const plain = setup(api, base([{ name: "Bob", time: 0.5, weight: 1 }]));
  api.evaluate(plain.m, plain.holder);
  plain.record.rootTransform = new Float32Array([0, 0, -1.5, 0, 0, 1.5, 0, 0, 1.5, 0, 0, 0, -3, 0.5, 1, 1]);
  api.evaluate(plain.m, plain.holder);
  const cheap = Array.from(plain.record.skins[0].jointMatrices);
  api.evaluate(plain.m, plain.holder, true);
  near(cheap, Array.from(plain.record.skins[0].jointMatrices), 2e-5, "re-skin equals a full evaluation");
});

test("IK reaches a bone of another model posed in the same frame", () => {
  const { sandbox, api } = loadChunk();
  sandbox.__gosx_scene3d_animation_api = { configureSampledRecord(_s, record) { record.clipSampler = { pose() { return true; }, sample() {} }; }, applyClipFrame() { return { applied: true }; } };
  vm.runInContext(transpile(read("..", "runtime", "scene3d", "command-runtime.ts")), vm.createContext(sandbox));
  const make = (id, x) => {
    const { nodes, skins } = humanoid();
    const record = { id, nodes, rootNodes: [0], skins, model: {}, rootTransform: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, 0, 0, 1]) };
    sandbox.__gosx_scene3d_animation_api.configureSampledRecord(state, record, clips(), {});
    return record;
  };
  const state = {};
  state._modelSkins = [make("a", 0), make("b", 0.3)];
  const handle = {
    __gosxScene3DCommandReady: true, applyCommands() {},
    applyClipFrame(models) { return sandbox.__gosx_scene3d_command_bridge.applyMountedClipFrame(state, models, sandbox.__gosx_scene3d_animation_api, (rec) => rec.clipSampler.pose(rec), () => {}, handle); },
  };
  sandbox.__gosx.engines = new Map([["scene", { handle }]]);
  const scene3d = sandbox.__gosx.scene3d;
  scene3d.defineAnimationRig(7, JSON.stringify(RIG));
  // b's left leg reaches a's spine, 10 cm in front of it. b comes first in
  // the frame; the chunk still poses a first.
  const spine = api.bones.indexOf("spine");
  const frame = encodeFrame([
    { id: "b", rigHash: 7, ...base([], { ik: [{ chain: 0, weight: 1, bone: { model: 1, index: spine, offset: [0, 0, 0.1] } }] }) },
    { id: "a", rigHash: 7, ...base([]) },
  ]);
  assert.equal(scene3d.dispatchAnimationFrame("scene", frame).applied, true);
  const out = new Float64Array(64);
  scene3d.animationFeedback("scene", "b", out);
  // a's spine sits at (0, 1.1, 0); the target is 0.1 in front: (0, 1.1, 0.1).
  near(out.subarray(9, 12), [0, 1.1, 0.1], 1e-6, "resolved target");
  near(out.subarray(6, 9), [0, 1.1, 0.1], 1e-5, "b's foot reached it");
  // A target model outside the frame is rejected by the decoder.
  assert.match(scene3d.dispatchAnimationFrame("scene", encodeFrame([{ id: "b", rigHash: 7, ...base([], { ik: [{ chain: 0, weight: 1, bone: { model: 3, index: spine } }] }) }])).reason, /target model/);
});

test("bone scale enlarges a bone and carries its children", () => {
  const { api } = loadChunk();
  const { m, holder } = setup(api, base([]), { rig: { ...RIG, boneScale: { head: 2 } } });
  api.evaluate(m, holder);
  const w = m.world, o = 9 * 16;
  near(Math.hypot(w[o], w[o + 1], w[o + 2]), 2, 1e-6, "head scale");
  // The eye's offset (0.03, 0.1, 0.08) from the head doubles.
  const head = pos(m, 9), eye = pos(m, 10);
  near([eye[0] - head[0], eye[1] - head[1], eye[2] - head[2]], [0.06, 0.2, 0.16], 1e-6, "eye carried");
});

test("keep proportions drops non-root translation keys from the model's clips", () => {
  const { api } = loadChunk();
  const play = base([{ name: "Detail", time: 0, weight: 1 }]);
  const plain = setup(api, play);
  api.evaluate(plain.m, plain.holder);
  near(pos(plain.m, 8), [0, 1.1, 0.2], 1e-6, "the clip moves the spine");
  const kept = setup(api, play, { rig: { ...RIG, keepProportions: true } });
  api.evaluate(kept.m, kept.holder);
  near(pos(kept.m, 8), [0, 1.1, 0], 1e-6, "the rig keeps its own spine offset");
});

test("retargeting keeps the target's bone lengths (mascot proportions)", () => {
  const { api } = loadChunk();
  const source = rolledArm(false), target = rolledArm(false);
  // A mascot: the same bones, with a forearm 1.6 times as long and the
  // hips 0.7 times as high.
  target.nodes[2].translation = [0.48, 0, 0];
  target.nodes[0].translation = [0, 0.7, 0];
  const raise = source.q(0, 0, 1, Math.PI / 3);
  const clips = [{ name: "Raise", duration: 1, channels: [
    { targetNode: 1, property: "rotation", interpolation: "LINEAR", times: new Float32Array([0, 1]), values: new Float32Array([0, 0, 0, 1, ...raise]) },
    { targetNode: 2, property: "translation", interpolation: "LINEAR", times: new Float32Array([0, 1]), values: new Float32Array([0.3, 0, 0, 0.3, 0, 0]) },
    { targetNode: 0, property: "translation", interpolation: "LINEAR", times: new Float32Array([0, 1]), values: new Float32Array([0, 1, 0, 0, 1.2, 0]) },
  ] }];
  const sMap = api.mapHumanoid(source.nodes, [], "auto", null), tMap = api.mapHumanoid(target.nodes, [], "auto", null);
  const retargeted = api.retargetClips({ nodes: source.nodes, animations: clips }, sMap, { nodes: target.nodes }, tMap, "");
  const record = { id: "x", nodes: target.nodes, rootNodes: [0], skins: [], model: { rootMotion: true }, rootTransform: null };
  const m = api.createModel(record, retargeted);
  const holder = api.holder({}, "x");
  holder.data = api.copyModel(holder.data, api.createDecoder()(encodeFrame([{ id: "x", ...base([{ name: "Raise", time: 1, weight: 1 }]) }])).models[0]);
  api.evaluate(m, holder);
  near(dist(pos(m, 1), pos(m, 2)), 0.48, 1e-6, "the mascot's forearm keeps its length");
  // The hips rise 0.2 m on the source rig (hip height 1); scaled by 0.7.
  near(pos(m, 0)[1], 0.7 + 0.2 * 0.7, 1e-6, "hips offset scaled by hip height");
  // The arm still turns 60 degrees: forearm direction (cos 60, sin 60, 0).
  const a = pos(m, 1), b = pos(m, 2), len = dist(a, b);
  near([(b[0] - a[0]) / len, (b[1] - a[1]) / len], [0.5, Math.sqrt(3) / 2], 1e-6, "direction carried");
});
