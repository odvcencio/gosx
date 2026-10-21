// Run with: nice -n 10 node client/js/scene3d-clip-frame.bench.mjs
// Measures the CPU cost of one sampled clip frame (GSP4) for two models with
// four clip layers each: wire decode, keyframe sampling and blending, node
// world transforms, and joint matrix computation. The rig has 64 joints in a
// chain-with-branches layout; each clip animates translation and rotation on
// every joint with 61 keys (2 s at 30 Hz). GPU skinning and upload time are
// outside this measurement.
import fs from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const ts = createRequire(path.join(root, 'client/runtime/package.json'))('typescript');
const read = (file) => fs.readFileSync(path.join(root, file), 'utf8');
const transpile = (source) => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;

const sandbox = { console, Math, JSON, Number, Object, Array, Map, Set, WeakMap, Float32Array, Float64Array, isFinite, Uint8Array, ArrayBuffer, DataView, TextDecoder, Promise, setTimeout, Date };
sandbox.window = sandbox; sandbox.globalThis = sandbox; sandbox.document = {};
const context = vm.createContext(sandbox);
vm.runInContext(read('client/js/bootstrap-src/11-scene-math.ts'), context);
vm.runInContext(transpile(read('client/runtime/scene3d/animation.ts')), context);
vm.runInContext(transpile(read('client/runtime/scene3d/command-runtime.ts')), context);
const api = sandbox.__gosx_scene3d_animation_api;
const bridge = sandbox.__gosx_scene3d_command_bridge;

const JOINTS = 64, KEYS = 61, CLIPS = ['idle', 'walk', 'hit', 'block'];
function buildModel(seed) {
  const nodes = [{ translation: [0, 0, 0], children: [1] }];
  for (let j = 0; j < JOINTS; j++) {
    const parent = j === 0 ? 0 : 1 + Math.floor((j - 1) / 2);
    const node = { translation: [0.1, 0.2, 0], children: [] };
    nodes.push(node);
    nodes[parent].children.push(j + 1);
  }
  const ibm = new Float32Array(JOINTS * 16);
  for (let j = 0; j < JOINTS; j++) ibm.set([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], j * 16);
  const clips = CLIPS.map((name, c) => ({
    name, duration: 2,
    channels: Array.from({ length: JOINTS * 2 }, (_, k) => {
      const joint = k >> 1, rotation = k & 1;
      const times = Float32Array.from({ length: KEYS }, (_, i) => i / 30);
      const width = rotation ? 4 : 3;
      const values = new Float32Array(KEYS * width);
      for (let i = 0; i < KEYS; i++) {
        const a = (i + joint + c + seed) * 0.05;
        if (rotation) values.set([0, 0, Math.sin(a / 2), Math.cos(a / 2)], i * 4); else values.set([Math.sin(a), 1 + Math.cos(a), 0], i * 3);
      }
      return { targetNode: joint + 1, property: rotation ? 'rotation' : 'translation', interpolation: 'LINEAR', times, values };
    }),
  }));
  const skin = { joints: Array.from({ length: JOINTS }, (_, j) => j + 1), inverseBindMatrices: ibm };
  return { nodes, clips, skin };
}

const models = [buildModel(0), buildModel(7)].map((m, i) => ({
  ...m, id: `p${i + 1}`, output: new Map(),
  sampler: api.createClipSampler(m.clips, { nodes: m.nodes, skins: [m.skin], rootNodes: [0] }),
}));

function encode(frame) {
  const enc = new TextEncoder(), out = [];
  const u16 = (v) => out.push(v & 255, v >> 8);
  const f32 = (v) => { const d = new DataView(new ArrayBuffer(4)); d.setFloat32(0, v, true); out.push(...new Uint8Array(d.buffer)); };
  const id = (v) => { const c = enc.encode(v); u16(c.length); out.push(...c); };
  out.push(71, 83, 80, 52); u16(CLIPS.length); CLIPS.forEach(id); u16(models.length);
  models.forEach((m) => { id(m.id); out.push(4); CLIPS.forEach((_, c) => { u16(c); f32((frame / 60 + c * 0.37) % 6); f32(1 / (c + 1)); out.push(1); }); });
  return Uint8Array.from(out);
}

const FRAMES = 3000, WARMUP = 300;
const frames = Array.from({ length: FRAMES + WARMUP }, (_, i) => encode(i));
function applyFrame(bytes, phase) {
  let t = phase ? performance.now() : 0;
  const decoded = bridge.decodeClipFrame(bytes);
  const t1 = phase ? performance.now() : 0;
  if (phase) phase.decode += t1 - t;
  for (let i = 0; i < decoded.length; i++) {
    const m = models[i];
    m.output.clear();
    const a = phase ? performance.now() : 0;
    m.sampler.sample(decoded[i].layers, decoded[i].layers.length, m.output);
    const b = phase ? performance.now() : 0;
    const world = api.buildNodeTransforms(m.nodes, m.output, null, [0]);
    api.computeJointMatrices(m.skin, world);
    if (phase) { phase.sample += b - a; phase.skin += performance.now() - b; }
  }
}
for (let i = 0; i < WARMUP; i++) applyFrame(frames[i], null);
const phase = { decode: 0, sample: 0, skin: 0 };
const per = [];
for (let i = WARMUP; i < FRAMES + WARMUP; i++) {
  const s = performance.now();
  applyFrame(frames[i], phase);
  per.push(performance.now() - s);
}
per.sort((a, b) => a - b);
const mean = per.reduce((a, b) => a + b, 0) / per.length;
const fmt = (v) => v.toFixed(3);
console.log(`2 models x 4 layers, ${JOINTS} joints, ${CLIPS.length} clips, ${KEYS} keys/channel, ${FRAMES} frames`);
console.log(`per frame ms: mean ${fmt(mean)}  p50 ${fmt(per[per.length >> 1])}  p95 ${fmt(per[Math.floor(per.length * 0.95)])}  p99 ${fmt(per[Math.floor(per.length * 0.99)])}  max ${fmt(per[per.length - 1])}`);
console.log(`phase mean ms: decode ${fmt(phase.decode / FRAMES)}  sample+blend ${fmt(phase.sample / FRAMES)}  node+joint matrices ${fmt(phase.skin / FRAMES)}`);
console.log(`wire bytes per frame: ${frames[0].length}`);
