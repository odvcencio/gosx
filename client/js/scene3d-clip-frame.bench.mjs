// Run with: nice -n 10 node --expose-gc client/js/scene3d-clip-frame.bench.mjs
// Measures the CPU cost of one sampled clip frame (GSP4) for two models with
// four clip layers each, 64 joints, 4 clips of 61 keys per channel (translation
// and rotation on every joint). Two paths on identical data:
//   apply   decode + sampler.pose (the shipped path: sample, blend, node and
//           joint matrices, no allocation)
//   general decode + Map-based sample + buildNodeTransforms + computeJointMatrices
//           (the first implementation, kept for models with morph targets)
// Reports per-frame mean/p50/p95/p99/max in ms, the longest 1% tail, and, when
// run with --expose-gc, heap growth over the run. GPU skinning and upload are
// outside this measurement.
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


const FRAMES = 6000, WARMUP = 6000;
const frames = Array.from({ length: 600 }, (_, i) => encode(i));
const decode = bridge.createClipDecoder();
const gc = globalThis.gc || null;

const slow = rigs.map((m) => { const skin = m.skin(); return { m, skin, sampler: api.createClipSampler(m.clips(), { nodes: m.nodes, skins: [skin], rootNodes: [0] }), out: new Map() }; });
function general(i) {
  const models = decode(frames[i % 600]);
  for (let k = 0; k < models.length; k++) {
    const s = slow[k];
    s.out.clear();
    s.sampler.sample(models[k].layers, models[k].layers.length, s.out);
    api.computeJointMatrices(s.skin, sandbox.__probe.buildNodeTransforms(s.m.nodes, s.out, null, [0]));
  }
}
function applied(i) { api.applyClipFrame(state, decode(frames[i % 600]), applyPose); }

function run(name, fn) {
  for (let i = 0; i < WARMUP; i++) fn(i);
  if (gc) gc();
  const heap0 = process.memoryUsage().heapUsed;
  const times = new Float64Array(FRAMES);
  for (let i = 0; i < FRAMES; i++) { const s = performance.now(); fn(i); times[i] = performance.now() - s; }
  const heap = process.memoryUsage().heapUsed - heap0;
  const sorted = Float64Array.from(times).sort();
  const q = (p) => sorted[Math.min(FRAMES - 1, Math.floor(FRAMES * p))];
  const mean = times.reduce((a, b) => a + b, 0) / FRAMES;
  const f = (v) => v.toFixed(3).padStart(7);
  console.log(`${name.padEnd(8)} mean ${f(mean)}  p50 ${f(q(0.5))}  p95 ${f(q(0.95))}  p99 ${f(q(0.99))}  max ${f(sorted[FRAMES - 1])} ms` + (gc ? `  heap ${(heap / FRAMES).toFixed(0)} B/frame` : ""));
}
console.log(`2 models x 4 layers, ${JOINTS} joints, ${FRAMES} frames, wire ${frames[0].length} bytes/frame`);
run("general", general);
run("apply", applied);
run("general", general);
run("apply", applied);
