// Run with: nice -n 10 node client/js/scene3d-animation-graph.bench.mjs
//
// CPU cost of evaluating one animation graph frame for one character with a
// 60-joint humanoid rig (Mixamo-style names, fingers included): a base layer
// blending two locomotion clips, an additive upper-body layer, two foot IK
// chains with ground and pelvis drop, and a four-bone look-at. Every joint
// has rotation keys at 30 fps; the hips also translate. Reports per-frame
// mean/p50/p95/p99 in microseconds for evaluate() alone and for decode plus
// copy plus evaluate. GPU upload is outside the measurement.
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const read = (...p) => fs.readFileSync(path.join(here, ...p), "utf8");
const transpile = (s) => ts.transpileModule(s, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;

// Run in the main realm, like the shipped chunk in a page. A vm sandbox
// would route every global read (Math, performance) through the sandbox's
// property interceptors, which a browser global does not have, and would
// overstate the cost severalfold.
const sandbox = globalThis;
sandbox.window = sandbox;
sandbox.document = { getElementById: () => null };
const quiet = console.warn;
console.warn = () => {};
vm.runInThisContext(transpile([
  read("bootstrap-src", "26n-feature-scene3d-animation-graph-prefix.ts"),
  read("..", "runtime", "scene3d", "animation-rig.ts"),
  read("..", "runtime", "scene3d", "ik.ts"),
  read("..", "runtime", "scene3d", "animation-graph.ts"),
  read("bootstrap-src", "26n-feature-scene3d-animation-graph-suffix.ts"),
].join("\n")), { filename: "bootstrap-feature-scene3d-animation-graph.js" });
console.warn = quiet;
const api = sandbox.__gosx_scene3d_animation_graph_api;
const F32 = Float32Array;
const U8 = Uint8Array;

// --- 60-joint humanoid ----------------------------------------------------
const nodes = [{ name: "Armature", children: [] }];
function add(name, parent, t) {
  const index = nodes.length;
  nodes.push({ name: "mixamorig:" + name, translation: t, children: [] });
  nodes[parent].children.push(index);
  return index;
}
const hips = add("Hips", 0, [0, 1, 0]);
const spine = add("Spine", hips, [0, 0.1, 0]), spine1 = add("Spine1", spine, [0, 0.12, 0]), spine2 = add("Spine2", spine1, [0, 0.12, 0]);
const neck = add("Neck", spine2, [0, 0.15, 0]), head = add("Head", neck, [0, 0.1, 0.02]);
add("HeadTop_End", head, [0, 0.18, 0]); add("LeftEye", head, [0.03, 0.06, 0.08]); add("RightEye", head, [-0.03, 0.06, 0.08]); add("Jaw", head, [0, -0.03, 0.05]);
for (const [side, sx] of [["Left", 1], ["Right", -1]]) {
  const shoulder = add(side + "Shoulder", spine2, [0.05 * sx, 0.12, 0]);
  const arm = add(side + "Arm", shoulder, [0.12 * sx, 0, 0]), fore = add(side + "ForeArm", arm, [0.26 * sx, 0, 0]), hand = add(side + "Hand", fore, [0.24 * sx, 0, 0]);
  for (const [finger, z] of [["Thumb", 0.03], ["Index", 0.02], ["Middle", 0], ["Ring", -0.02], ["Pinky", -0.04]]) {
    let parent = hand;
    const segments = finger === "Thumb" || finger === "Index" ? 4 : 3; // Mixamo end bones on two fingers
    for (let k = 1; k <= segments; k++) parent = add(side + "Hand" + finger + k, parent, [0.03 * sx, 0, k === 1 ? z : 0]);
  }
  const up = add(side + "UpLeg", hips, [0.09 * sx, -0.05, 0]), leg = add(side + "Leg", up, [0, -0.43, 0.02]), foot = add(side + "Foot", leg, [0, -0.42, -0.02]);
  add(side + "ToeBase", foot, [0, -0.05, 0.12]);
}
const jointCount = nodes.length - 1;
const joints = Array.from({ length: jointCount }, (_, i) => i + 1);
const ibm = new F32(jointCount * 16);
for (let j = 0; j < jointCount; j++) ibm.set([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], j * 16);
const skins = [{ joints, inverseBindMatrices: ibm }];

function clip(name, duration, amplitude, seed) {
  const keys = Math.round(duration * 30) + 1;
  const channels = [];
  for (const node of joints) {
    const times = new F32(keys), values = new F32(keys * 4);
    for (let k = 0; k < keys; k++) {
      times[k] = (k / (keys - 1)) * duration;
      const a = Math.sin((k / (keys - 1)) * Math.PI * 2 + node * 0.3 + seed) * amplitude;
      values.set([Math.sin(a / 2), 0, 0, Math.cos(a / 2)], k * 4);
    }
    channels.push({ targetNode: node, property: "rotation", interpolation: "LINEAR", times, values });
  }
  const times = new F32(keys), values = new F32(keys * 3);
  for (let k = 0; k < keys; k++) { times[k] = (k / (keys - 1)) * duration; values.set([0, 1 + 0.03 * Math.sin(k), 0], k * 3); }
  channels.push({ targetNode: hips, property: "translation", interpolation: "LINEAR", times, values });
  return { name, duration, channels };
}
const clips = [clip("Walk", 1, 0.4, 0), clip("Run", 0.6, 0.7, 1), clip("Aim", 1, 0.2, 2), clip("Idle", 2, 0.05, 3)];

const rig = {
  v: 1, humanoid: "auto", bones: {},
  masks: [{ name: "upper", include: ["spine"], exclude: [], weights: {} }],
  layers: [{ name: "base", mask: -1, additive: false, refClip: "", refTime: 0 }, { name: "aim", mask: 0, additive: true, refClip: "", refTime: 0 }],
  ik: [
    { name: "leftFoot", root: "leftUpperLeg", mid: "leftLowerLeg", end: "leftFoot", foot: true, align: true },
    { name: "rightFoot", root: "rightUpperLeg", mid: "rightLowerLeg", end: "rightFoot", foot: true, align: true },
  ],
  lookAt: [{ name: "head", bones: ["spine", "chest", "neck", "head"], weights: [0.15, 0.2, 0.3, 0.35], axis: [0, 0, 1], pivot: "", maxAngle: 1.2 }],
  pelvis: { bone: "hips", maxDrop: 0.4 }, sources: [],
};
api.defineRig(9, JSON.stringify(rig));

// GSA2 frame bytes, as motion/animgraph.FrameEncoder writes them.
function frame(t) {
  const bytes = [];
  const u8 = (v) => bytes.push(v & 255), u16 = (v) => { u8(v); u8(v >> 8); }, u32 = (v) => { u16(v & 0xffff); u16(v >>> 16); };
  const f32 = (v) => { for (const x of new Uint8Array(new Float32Array([v]).buffer)) u8(x); };
  const str = (s) => { u16(s.length); for (const c of s) u8(c.charCodeAt(0)); };
  for (const c of "GSA2") u8(c.charCodeAt(0));
  u16(3); ["Walk", "Run", "Aim"].forEach(str);
  u16(1); str("hero"); u32(9); f32(1 / 60); u8(0);
  u8(2);
  f32(1); u8(2);
  u16(0); f32(t % 1); f32(0.5); u8(1);
  u16(1); f32((t % 1) * 0.6); f32(0.5); u8(1);
  f32(0.8); u8(1); u16(2); f32(t % 1); f32(1); u8(1);
  u8(2);
  for (const chain of [0, 1]) { u8(chain); u8(4); f32(1); f32(0.05 * Math.sin(t + chain)); f32(0); f32(1); f32(0); }
  u8(1); u8(0); f32(1); f32(Math.sin(t) * 3); f32(1.6); f32(4);
  return U8.from(bytes);
}

const record = { id: "hero", nodes, rootNodes: [0], skins, model: {}, rootTransform: null };
const model = api.createModel(record, clips);
const holder = api.holder({}, "hero");
const decode = api.createDecoder();
const frames = Array.from({ length: 240 }, (_, i) => frame(i / 60));

function stats(samples) {
  samples.sort((a, b) => a - b);
  const pick = (q) => samples[Math.min(samples.length - 1, Math.floor(q * samples.length))];
  const mean = samples.reduce((s, v) => s + v, 0) / samples.length;
  return `mean ${mean.toFixed(1).padStart(6)}  p50 ${pick(0.5).toFixed(1).padStart(6)}  p95 ${pick(0.95).toFixed(1).padStart(6)}  p99 ${pick(0.99).toFixed(1).padStart(6)} us`;
}

const N = Number(process.env.BENCH_FRAMES || 20000);
for (let i = 0; i < 2000; i++) { // warm up
  holder.data = api.copyModel(holder.data, decode(frames[i % frames.length]).models[0]); holder.serial++;
  api.evaluate(model, holder);
}
const evalOnly = [], full = [];
for (let i = 0; i < N; i++) {
  const bytes = frames[i % frames.length];
  const t0 = performance.now();
  holder.data = api.copyModel(holder.data, decode(bytes).models[0]); holder.serial++;
  const t1 = performance.now();
  api.evaluate(model, holder);
  const t2 = performance.now();
  evalOnly.push((t2 - t1) * 1000);
  full.push((t2 - t0) * 1000);
}
console.log(`${jointCount} joints, ${clips.length} clips x ${joints.length + 1} channels, 3 samples in 2 layers, 2 foot IK + 4-bone look-at, ${N} frames, ${frames[0].length} bytes/frame`);
console.log("evaluate        " + stats(evalOnly));
console.log("decode+evaluate " + stats(full));

// --- 22 characters per frame: full detail vs a level-of-detail mix ---------
// Each character is its own model on the same rig and clips, and moves every
// frame (a new root transform), as players on a field do. Full: all 22 run a
// full evaluation every frame. LOD mix (the DefaultLOD levels for a broadcast
// camera): 4 characters at level 0 (60 Hz, all joints), 8 at level 1 (30 Hz,
// no fingers/eyes/jaw), 10 at level 2 (20 Hz, base layer only, no IK, no
// toes or unmapped bones). Characters skipped in a frame are only re-skinned
// for their new position.
function teamFrame(level) {
  const bytes = [];
  const u8 = (v) => bytes.push(v & 255), u16 = (v) => { u8(v); u8(v >> 8); }, u32 = (v) => { u16(v & 0xffff); u16(v >>> 16); };
  const f32 = (v) => { for (const x of new Uint8Array(new Float32Array([v]).buffer)) u8(x); };
  const str = (s) => { u16(s.length); for (const c of s) u8(c.charCodeAt(0)); };
  for (const c of "GSA2") u8(c.charCodeAt(0));
  u16(3); ["Walk", "Run", "Aim"].forEach(str);
  u16(1); str("hero"); u32(9); f32(1 / 60); u8([0, 1, 2][level]);
  u8(level === 2 ? 1 : 2);
  f32(1); u8(2);
  u16(0); f32(0.3); f32(0.5); u8(1);
  u16(1); f32(0.18); f32(0.5); u8(1);
  if (level !== 2) { f32(0.8); u8(1); u16(2); f32(0.3); f32(1); u8(1); }
  if (level === 2) { u8(0); u8(0); }
  else {
    u8(2);
    for (const chain of [0, 1]) { u8(chain); u8(4); f32(1); f32(0.02 * chain); f32(0); f32(1); f32(0); }
    u8(1); u8(0); f32(1); f32(1); f32(1.6); f32(4);
  }
  return U8.from(bytes);
}
const team = [];
for (let i = 0; i < 22; i++) {
  const level = i < 4 ? 0 : i < 12 ? 1 : 2;
  const rec = { id: "p" + i, nodes, rootNodes: [0], skins: [{ joints, inverseBindMatrices: ibm }], model: {}, rootTransform: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, i, 0, 0, 1]) };
  const m = api.createModel(rec, clips);
  const h = api.holder({}, rec.id);
  const bytesFull = teamFrame(0), bytesLod = teamFrame(level);
  team.push({ m, h, rec, level, period: [1, 2, 3][level], phase: i % [1, 2, 3][level], bytesFull, bytesLod });
}
function runTeam(lod, frames) {
  const samples = [];
  for (let f = 0; f < frames; f++) {
    const t0 = performance.now();
    for (const p of team) {
      p.rec.rootTransform[12] += 0.01;
      const due = !lod || (f + p.phase) % p.period === 0;
      if (due) {
        p.h.data = api.copyModel(p.h.data, decode(lod ? p.bytesLod : p.bytesFull).models[0]);
        p.h.serial++;
      }
      api.evaluate(p.m, p.h);
    }
    samples.push((performance.now() - t0) * 1000);
  }
  return samples;
}
runTeam(false, 300); runTeam(true, 300);
console.log("22 characters, full detail every frame  " + stats(runTeam(false, 3000)));
console.log("22 characters, LOD mix (4/8/10)         " + stats(runTeam(true, 3000)));
