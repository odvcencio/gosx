// Allocation probe for the sampled clip-frame apply path. Not a test file:
// scene3d-clip-frame.test.mjs runs it in a child process with a large young
// generation (no scavenge during the measurement) and --expose-gc, so heapUsed
// growth equals the bytes allocated. Prints one JSON line:
//   { applyBytes, controlBytes, frames }
// applyBytes: decode + apply (sample, blend, node and joint matrices) for two
// models x four layers. controlBytes: the general Map-based path, which must
// allocate, so a zero result cannot come from a broken probe.
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const read = (...p) => fs.readFileSync(path.join(here, ...p), "utf8");
const transpile = (s) => ts.transpileModule(s, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;

// Wrap the sources the way the shipped bundle is wrapped (one function scope),
// not as vm globals. Do not inject host builtins into the context: typed arrays
// from another realm defeat V8's element-access optimizations and make the code
// allocate a heap number per read, which a browser page does not do.
const sandbox = { console: { warn() {}, error() {}, log() {} }, TextDecoder };
sandbox.window = sandbox; sandbox.globalThis = sandbox; sandbox.document = {};
const context = vm.createContext(sandbox);
vm.runInContext("(function(){\n" + read("bootstrap-src", "11-scene-math.ts") + "\n" + transpile(process.env.CLIP_ANIM_PATH ? fs.readFileSync(process.env.CLIP_ANIM_PATH, "utf8") : read("..", "runtime", "scene3d", "animation.ts")) +
  "\nwindow.__probe = { buildNodeTransforms: sceneAnimBuildNodeTransforms, interp: sceneAnimInterpolateChannel, seek: sceneAnimSeekChannel, st: sceneAnimationSampleTime, ev: sceneClipEvalChannel, bl: sceneClipBlendChannel, bs: sceneClipBuildPoseState, sl64: sceneClipSlerpF64, sl: sceneClipSlerp, trs: sceneTRSToMat4Into, mul: sceneMat4MultiplyInto };})()", context);
const Float32Array = vm.runInContext("Float32Array", context); // typed arrays must come from the same realm as the code that reads them
const Uint8Array = vm.runInContext("Uint8Array", context);
vm.runInContext(transpile(read("..", "runtime", "scene3d", "command-runtime.ts")), context);
const api = sandbox.__gosx_scene3d_animation_api;
const bridge = sandbox.__gosx_scene3d_command_bridge;

const JOINTS = 64, KEYS = 61, CLIPS = ["idle", "walk", "hit", "block"];
function model(seed) {
  const nodes = [{ translation: [0, 0, 0], children: [1] }];
  for (let j = 0; j < JOINTS; j++) {
    const parent = j === 0 ? 0 : 1 + Math.floor((j - 1) / 2);
    nodes.push({ translation: [0.1, 0.2, 0], children: [] });
    nodes[parent].children.push(j + 1);
  }
  const ibm = new Float32Array(JOINTS * 16);
  for (let j = 0; j < JOINTS; j++) ibm.set([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], j * 16);
  const clips = () => CLIPS.map((name, c) => ({
    name, duration: 2,
    channels: Array.from({ length: JOINTS * 2 }, (_, k) => {
      const joint = k >> 1, rotation = k & 1, width = rotation ? 4 : 3;
      const times = Float32Array.from({ length: KEYS }, (_, i) => i / 30);
      const values = new Float32Array(KEYS * width);
      for (let i = 0; i < KEYS; i++) {
        const a = (i + joint + c + seed) * 0.05;
        if (rotation) values.set([0, 0, Math.sin(a / 2), Math.cos(a / 2)], i * 4); else values.set([Math.sin(a), 1 + Math.cos(a), 0], i * 3);
      }
      return { targetNode: joint + 1, property: rotation ? "rotation" : "translation", interpolation: "LINEAR", times, values };
    }),
  }));
  return { nodes, clips, skin: () => ({ joints: Array.from({ length: JOINTS }, (_, j) => j + 1), inverseBindMatrices: ibm }) };
}

const state = { _modelSkins: [], _modelAnimations: [], _clipFrameLayers: new Map() };
const rigs = [model(0), model(7)];
const records = rigs.map((m, i) => {
  const record = { id: "p" + (i + 1), nodes: m.nodes, rootNodes: [0], skins: [m.skin()], rootTransform: null, animationApi: api, animatedTransforms: new Map() };
  api.configureSampledRecord(state, record, m.clips(), { rootMotion: false });
  state._modelSkins.push(record);
  return record;
});
const applyPose = (record) => record.clipSampler.pose(record);

function encode(frame) {
  const enc = new TextEncoder(), out = [];
  const u16 = (v) => out.push(v & 255, v >> 8);
  const f32 = (v) => { const d = new DataView(new ArrayBuffer(4)); d.setFloat32(0, v, true); out.push(...new Uint8Array(d.buffer)); };
  const id = (v) => { const c = enc.encode(v); u16(c.length); out.push(...c); };
  out.push(71, 83, 80, 52); u16(CLIPS.length); CLIPS.forEach(id); u16(2);
  ["p1", "p2"].forEach((who, w) => { id(who); out.push(4); CLIPS.forEach((_, c) => { u16(c); f32((frame / 60 + c * 0.37 + w) % 6); f32(1 / (c + 1)); out.push(1); }); });
  return Uint8Array.from(out);
}

const FRAMES = Number(process.env.CLIP_PROBE_FRAMES || 5000), WARMUP = Number(process.env.CLIP_PROBE_WARMUP || 20000);
const frames = Array.from({ length: 600 }, (_, i) => encode(i));
const decode = bridge.createClipDecoder();
const apply = (i) => { const r = api.applyClipFrame(state, decode(frames[i % 600]), applyPose); if (!r.applied) throw new Error(r.reason); };
const gc = globalThis.gc || (() => {});

for (let i = 0; i < WARMUP; i++) apply(i);
gc();
let before = process.memoryUsage().heapUsed;
for (let i = 0; i < FRAMES; i++) apply(i);
const applyBytes = process.memoryUsage().heapUsed - before;

// Control: the general path (Map pose + node transform map) on the same data.
const slow = rigs[0], slowSkin = slow.skin(), slowSampler = api.createClipSampler(slow.clips(), { nodes: slow.nodes, skins: [slowSkin], rootNodes: [0] });
const out = new Map();
const layers = [0, 1, 2, 3].map((c) => ({ name: CLIPS[c], index: 0, time: 0, weight: 1 / (c + 1), loop: true }));
const control = (i) => {
  for (let c = 0; c < 4; c++) layers[c].time = (i / 60 + c * 0.37) % 6;
  out.clear();
  slowSampler.sample(layers, 4, out);
  api.computeJointMatrices(slowSkin, sandbox.__probe.buildNodeTransforms(slow.nodes, out, null, [0]));
};
for (let i = 0; i < 3000; i++) control(i);
gc();
before = process.memoryUsage().heapUsed;
for (let i = 0; i < FRAMES; i++) control(i);
const controlBytes = process.memoryUsage().heapUsed - before;
console.log(JSON.stringify({ applyBytes, controlBytes, frames: FRAMES }));
if (process.env.CLIP_PROBE_PARTS) {
  const measure = (fn) => { for (let i = 0; i < 20000; i++) fn(i); gc(); const b = process.memoryUsage().heapUsed; for (let i = 0; i < FRAMES; i++) fn(i); return process.memoryUsage().heapUsed - b; };
  const models = decode(frames[0]);
  console.log("decode only", measure((i) => decode(frames[i % 600])));
  console.log("pose only", measure(() => { records[0].clipSampler.pose(records[0]); records[1].clipSampler.pose(records[1]); }));
}
if (process.env.CLIP_PROBE_PARTS) {
  const measure = (fn) => { for (let i = 0; i < 20000; i++) fn(i); gc(); const b = process.memoryUsage().heapUsed; for (let i = 0; i < FRAMES; i++) fn(i); return process.memoryUsage().heapUsed - b; };
  const clipsx = rigs[0].clips()[0].channels; const tch = clipsx[0], rch = clipsx[1];
  const P = sandbox.__probe;
  const ts_ = [0.31, 0.77, 1.2, 1.55];
  console.log("seek only T", measure((i) => P.seek(tch, ts_[i & 3])));
  console.log("interp T", measure((i) => P.interp(tch, ts_[i & 3])));
  console.log("interp R", measure((i) => P.interp(rch, ts_[i & 3])));
  console.log("sampletime", measure((i) => P.st(ts_[i & 3] + 4, 2, true)));
}
if (process.env.CLIP_PROBE_PARTS) {
  const measure = (fn) => { for (let i = 0; i < 20000; i++) fn(i); gc(); const b = process.memoryUsage().heapUsed; for (let i = 0; i < FRAMES; i++) fn(i); return process.memoryUsage().heapUsed - b; };
  const ch = rigs[0].clips()[0].channels; const P = sandbox.__probe;
  const buf = new Float64Array(4), o = new Float64Array(4), tS = new Float64Array(3), rS = new Float64Array([0,0,0,1]), sS = new Float64Array([1,1,1]); const M = new Float32Array(16), M2 = new Float32Array(16);
  const ts_ = [0.31, 0.77, 1.2, 1.55];
  console.log("evalT", measure((i) => { buf[0] = ts_[i & 3]; P.ev(ch[0], buf, o); }));
  console.log("evalR", measure((i) => { buf[0] = ts_[i & 3]; P.ev(ch[1], buf, o); }));
  console.log("slerp", measure((i) => { buf[1] = ts_[i & 3] / 2; P.sl(o, ch[1].values, 0, ch[1].values, 4, buf, 1); }));
  console.log("trs", measure(() => P.trs(M, tS, rS, sS)));
  console.log("mul", measure(() => P.mul(M, M2, M2)));
  console.log("empty", measure((i) => { buf[0] = ts_[i & 3]; }));
}
if (process.env.CLIP_PROBE_PARTS) {
  const measure = (fn) => { for (let i = 0; i < 20000; i++) fn(i); gc(); const b = process.memoryUsage().heapUsed; for (let i = 0; i < FRAMES; i++) fn(i); return process.memoryUsage().heapUsed - b; };
  const r = records[0]; const saved = r.sampledLayerCount;
  for (const n of [0, 1, 2, 4]) { r.sampledLayerCount = n; console.log("pose layers", n, measure(() => r.clipSampler.pose(r))); }
  r.sampledLayerCount = saved;
}
if (process.env.CLIP_PROBE_PARTS) {
  const measure = (fn) => { for (let i = 0; i < 200000; i++) fn(i); gc(); const b = process.memoryUsage().heapUsed; for (let i = 0; i < FRAMES; i++) fn(i); return process.memoryUsage().heapUsed - b; };
  const P = sandbox.__probe; const rig0 = rigs[0];
  const st = P.bs(rig0.nodes, [0], [rig0.skin()]);
  st.v.set([0, 0, 0.3, 0.9]); st.buf[3] = 0.5; st.flags[5] = 2; st.wt[5*3+1] = 1; st.r.set([0,0,0,1], 5*4);
  console.log("blendR", measure(() => { P.bl(st, 5, 1); }));
  st.flags[6] = 1; st.wt[6*3] = 1;
  console.log("blendT", measure(() => { P.bl(st, 6, 0); }));
  const q = new Float64Array(4), a = new Float64Array([0,0,0,1]), b = new Float64Array([0,0,0.6,0.8]);
  st.buf[2] = 0.3;
  console.log("sl64", measure(() => { P.sl64(q, a, 0, b, 0, st.buf, 2); }));
}
