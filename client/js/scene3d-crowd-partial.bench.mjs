// A crowd on a rolling ship: full motion frames in world space against partial
// motion frames on a parent node. Main-thread cost in Node, with no GPU.
//
//   node client/js/scene3d-crowd-partial.bench.mjs [--sizes 2000,5000] [--ticks 60] [--share 0.05]
//
// It loads the shipped bundle (run `make build-bootstrap` after changing Scene3D
// sources) and runs the pipeline a page runs for a GPU-motion crowd:
//
//   every 6th frame (10 Hz tick)   decode a motion frame, apply it to the agents
//   every frame (60 Hz)            createSceneRenderBundle, prepareScene, and the
//                                  WebGL renderer's per-record preparation and
//                                  motion-stream upload, against a mock GL
//
// Two ways to keep 5,000 agents on a ship that rolls and moves:
//   full     Agents are in world space. The ship moved, so every tick re-sends
//            and re-applies the whole batch (GSP3), whatever changed.
//   partial  Agents are in the ship's space (InstancedGLBMesh.ParentNode). The
//            ship's pose is one matrix written per frame, and a tick sends only
//            the 5% of agents that changed, by slot (GSP5).
//
// What is timed: frame decode, applying the frame to the agents, and the
// per-frame bundle, planner and renderer preparation. What is not: building the
// frame bytes (the Go encoder's job), the app work of computing world-space
// transforms for the full path (which the partial path does not need), GPU time.
// Byte counts come from the same layout the Go encoders write (pinned by the
// golden tests in scene/motion_frame_partial_test.go).
//
// Node's JIT and the missing GPU make absolute numbers differ from a browser.
// Compare rows from one run on one machine.
//
// Median of five runs, Node v26.8.1, 60 ticks each, 5% of agents change per tick:
//   agents path    wire B/tick  decode+apply ms/tick  other ms/frame  avg ms/frame  GL upload B/tick
//   2000   full    204,912       9.93                  0.21            2.19          192,000
//   2000   partial 9,324         0.51                  0.23            0.43          188,928
//   5000   full    513,912       27.36                 1.55            6.55          480,000
//   5000   partial 23,274        1.21                  0.92            1.35          477,120
// "avg ms/frame" is the mean over all frames, ticks included. GL upload bytes
// barely fall because the renderer caps a stream upload at 32 calls and covers
// the span from the first to the last changed slot beyond that; 5% of agents
// scattered across the batch touch almost every part of the stream. Changed
// agents that sit together upload only their own ranges.
import { createRequire } from 'node:module';
import { performance } from 'node:perf_hooks';
import { fileURLToPath } from 'node:url';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';

const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..', '..');
const H = require(path.join(here, 'runtime-test-harness.js'));
const ts = createRequire(path.join(root, 'client/runtime/package.json'))('typescript');

function arg(name, fallback) {
  const index = process.argv.indexOf('--' + name);
  return index >= 0 && process.argv[index + 1] ? process.argv[index + 1] : fallback;
}
const sizes = arg('sizes', '2000,5000').split(',').map(Number);
const ticks = Number(arg('ticks', '60'));
const share = Number(arg('share', '0.05'));
const FRAMES_PER_TICK = 6;
const RUNS = 5;

const transpile = source => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const read = file => fs.readFileSync(path.join(root, file), 'utf8');
function section(source, first, last) {
  const start = source.indexOf(first);
  const end = source.indexOf(last, start + first.length);
  if (start < 0 || end < start) throw new Error(`missing source section: ${first}`);
  return source.slice(start, end);
}

const GLOBAL_NAMES = ['window', 'globalThis', 'document', 'console', 'navigator', 'performance', 'Date', 'Math', 'JSON',
  'requestAnimationFrame', 'cancelAnimationFrame', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval',
  'fetch', 'Image', 'ResizeObserver', 'IntersectionObserver', 'MutationObserver', 'getComputedStyle', 'matchMedia',
  'WebGL2RenderingContext', 'GPUBufferUsage', 'GPUTextureUsage', 'GPUShaderStage', 'CustomEvent', 'Event',
  'innerWidth', 'innerHeight', 'devicePixelRatio', 'screen'];

// Run a shipped chunk with ordinary globals, as a browser does.
async function loadEnvironment() {
  const env = H.createContext({});
  const c = env.context;
  const run = source => new Function(...GLOBAL_NAMES, source)(...GLOBAL_NAMES.map(key => key === 'globalThis' ? c : (c[key] !== undefined ? c[key] : globalThis[key])));
  run(H.bootstrapSource);
  await H.flushAsyncWork();
  run(read('client/js/bootstrap-feature-scene3d-animation.js'));
  await H.flushAsyncWork();
  return { api: c.__gosx_scene3d_api, window: c.window || c, anim: (c.window || c).__gosx_scene3d_animation_api || c.__gosx_scene3d_animation_api };
}

// The command bridge (frame decoding and the mounted apply) and the writer
// sceneUpdateRigidInstanceMotion, from their sources.
function loadFrameRuntime(window) {
  window.document = window.document || {};
  new Function('window', 'document', 'TextDecoder', 'Date', 'Promise', 'setTimeout', transpile(read('client/runtime/scene3d/command-runtime.ts')))(
    window, window.document, TextDecoder, Date, Promise, setTimeout);
  const webglMount = read('client/runtime/scene3d/mount-webgl.ts');
  const writer = section(webglMount, 'function sceneUpdateRigidInstanceMotion(', 'function sceneRigidMembershipModelID(');
  const updateRigidMotion = new Function('window', transpile(writer) + '\nreturn sceneUpdateRigidInstanceMotion;')(window);
  return { bridge: window.__gosx_scene3d_command_bridge, updateRigidMotion };
}

// The WebGL renderer's per-record preparation and motion-stream upload, against
// a GL mock that counts bytes. Same cut as scene3d-crowd-motion.bench.mjs.
function loadRenderer() {
  const webgl = read('client/runtime/scene3d/webgl.ts');
  const uploads = { bytes: 0, calls: 0 };
  let bufferID = 0;
  const gl = {
    ARRAY_BUFFER: 1, DYNAMIC_DRAW: 2,
    createBuffer: () => ++bufferID, bindBuffer() {}, deleteBuffer() {},
    bufferData(_target, data) { if (typeof data !== 'number') { uploads.bytes += data.byteLength; uploads.calls++; } },
    bufferSubData(_target, _offset, data) { uploads.bytes += data.byteLength; uploads.calls++; },
  };
  const context = vm.createContext({
    console, Float32Array, Map, WeakMap, Set, Math, Number, Array, gl,
    instancedStreamRecords: new Map(), instancedStreamEpoch: 1, pointsEntryBuffers: new Set(),
    sceneNumber: (value, fallback) => Number.isFinite(value) ? value : fallback,
    prepareCrowdAtlas() {}, SCENE_CROWD_MOTION_RECORD_FLOATS: 24,
    prebuiltRigidBatchRecords: new Map(), prebuiltRigidBatchEpoch: 1,
    meshColorVisibility: new Map(), rigidObjectBatches: new Map(),
  });
  vm.runInContext(transpile(section(webgl, 'function uploadInstancedStream(', 'function bindInstancedVertexAttribute(')), context);
  vm.runInContext(transpile(section(webgl, 'function boundsOutsideSidePlanes(', 'function prepareRigidMeshBatches(')), context);
  vm.runInContext(transpile(section(webgl, 'function prepareCrowdMotionRecords(', 'function bindCrowdMotionBatch(')), context);
  return { context, uploads };
}

// ---- wire layout (GSP3 / GSP5), as written by scene/motion_frame*.go ----

const encoder = new TextEncoder();
function writer() {
  const chunks = [];
  let view = new DataView(new ArrayBuffer(1 << 16)), offset = 0;
  const grow = n => {
    if (offset + n <= view.byteLength) return;
    chunks.push(new Uint8Array(view.buffer, 0, offset));
    view = new DataView(new ArrayBuffer(Math.max(1 << 16, n))); offset = 0;
  };
  return {
    u16(v) { grow(2); view.setUint16(offset, v, true); offset += 2; },
    u8(v) { grow(1); view.setUint8(offset, v); offset += 1; },
    f32(v) { grow(4); view.setFloat32(offset, v, true); offset += 4; },
    text(s) { const b = encoder.encode(s); this.u16(b.length); grow(b.length); new Uint8Array(view.buffer, offset, b.length).set(b); offset += b.length; },
    done() {
      chunks.push(new Uint8Array(view.buffer, 0, offset));
      const out = new Uint8Array(chunks.reduce((n, c) => n + c.length, 0));
      let at = 0;
      for (const c of chunks) { out.set(c, at); at += c.length; }
      return out;
    },
  };
}
function payload(w, k) {
  for (const v of [k.prev[0], k.prev[1], k.prev[2], 0, k.yaw, 0, 1, 1, 1, k.t0, k.next[0], k.next[1], k.next[2], 0, k.yaw, 0, 1, 1, 1, k.t1]) w.f32(v);
  w.u16(1); w.f32(k.t0); w.u8(1); w.f32(1);
}
function fullFrame(batchID, ids, keys) {
  const w = writer();
  for (const c of [71, 83, 80, 51]) w.u8(c);
  w.u16(1); w.text('Run'); w.u16(1); w.text(batchID); w.u16(ids.length);
  for (let i = 0; i < ids.length; i++) { w.text(ids[i]); payload(w, keys[i]); }
  return w.done();
}
function partialFrame(batchID, size, slots, keys) {
  const w = writer();
  for (const c of [71, 83, 80, 53]) w.u8(c);
  w.u16(1); w.text('Run'); w.u16(1); w.text(batchID); w.u16(size); w.u16(slots.length);
  for (const slot of slots) { w.u16(slot); payload(w, keys.get(slot)); }
  return w.done();
}

// ---- the scene ----

function cubeVertices() {
  const faces = [
    [[0, 0, 1], [1, 0, 0], [0, 1, 0]], [[0, 0, -1], [-1, 0, 0], [0, 1, 0]],
    [[1, 0, 0], [0, 0, -1], [0, 1, 0]], [[-1, 0, 0], [0, 0, 1], [0, 1, 0]],
    [[0, 1, 0], [1, 0, 0], [0, 0, -1]], [[0, -1, 0], [1, 0, 0], [0, 0, 1]],
  ];
  const positions = [], normals = [], uvs = [], tangents = [], joints = [], weights = [];
  for (const [n, u, v] of faces) {
    for (const [a, b] of [[-1, -1], [1, -1], [1, 1], [-1, -1], [1, 1], [-1, 1]]) {
      for (let k = 0; k < 3; k++) positions.push(n[k] * .5 + u[k] * a * .5 + v[k] * b * .5);
      normals.push(...n); uvs.push((a + 1) / 2, (b + 1) / 2); tangents.push(...u, 1);
      joints.push(0, 0, 0, 0); weights.push(1, 0, 0, 0);
    }
  }
  const f32 = values => new Float32Array(values);
  return { count: 36, positions: f32(positions), normals: f32(normals), uvs: f32(uvs), tangents: f32(tangents), joints: f32(joints), weights: f32(weights) };
}

// Column-major pose from a translation and Euler angles (R = Rz * Ry * Rx).
function pose(out, tx, ty, tz, rx, ry, rz) {
  const cx = Math.cos(rx), sx = Math.sin(rx), cy = Math.cos(ry), sy = Math.sin(ry), cz = Math.cos(rz), sz = Math.sin(rz);
  out.set([cy * cz, cy * sz, -sy, 0, sx * sy * cz - cx * sz, sx * sy * sz + cx * cz, sx * cy, 0,
    cx * sy * cz + sx * sz, cx * sy * sz - sx * cz, cx * cy, 0, tx, ty, tz, 1]);
  return out;
}
const apply = (m, x, y, z) => [m[0] * x + m[4] * y + m[8] * z + m[12], m[1] * x + m[5] * y + m[9] * z + m[13], m[2] * x + m[6] * y + m[10] * z + m[14]];
// The ship at time t: sails along -Z, rolls and pitches.
const shipPose = (out, t) => pose(out, 0, .4 * Math.sin(t * 1.3), -3 * t, .05 * Math.sin(t * 0.9), 0.02 * t, .09 * Math.sin(t * 1.1));

function median(values) {
  const sorted = values.slice().sort((a, b) => a - b);
  return sorted[sorted.length >> 1];
}

function makeScene(env, count, parented) {
  const { api, anim } = env;
  const proto = api.normalizeSceneObject({ id: 'proto', kind: 'mesh', vertices: cubeVertices(), materialKind: 'standard',
    color: '#c8b27a', roughness: .6, receiveShadow: true, pickable: false }, 0);
  proto.vertices.immutable = true;
  proto.vertices.revision = 0;
  const atlas = { id: 'bench-atlas', clips: new Map([['Run', { start: 1, segments: 30, duration: 1 }]]), missingClips: new Set() };
  const bounds = { minX: -.5, minY: -.5, minZ: -.5, maxX: .5, maxY: .5, maxZ: .5 };
  const ids = [], objects = [], locals = [];
  for (let i = 0; i < count; i++) {
    ids.push(`agent-${i}`);
    locals.push({ x: (i % 100) * .6 - 30, z: -Math.floor(i / 100) * .6, yaw: (i % 7) * .3 });
    const object = Object.assign({}, proto, {
      id: `crowd/agent-${i}/0`, x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
      spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0, driftSpeed: 0, driftPhase: 0,
      static: false, skin: null, vertices: proto.vertices,
      parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, locals[i].x, 0, locals[i].z, 1]),
    });
    object._crowdSkin = { atlas, bounds, rows: new Float32Array([1, 2, .5]), poseRows: null, batchID: 'crowd', rev: 0, group: null, parent: parented ? 'ship' : '' };
    objects.push(object);
  }
  const ship = Object.assign({}, proto, { id: 'ship/hull', x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
    spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0, static: false, skin: null, parentMatrix: new Float32Array(16) });
  pose(ship.parentMatrix, 0, 0, 0, 0, 0, 0);
  // What the mount's hydration cache holds: the writer resolves agents through it.
  const rigidInstances = new Map(), byID = new Map();
  ids.forEach((id, i) => {
    const staged = { objects: [objects[i]] };
    rigidInstances.set('k' + i, staged);
    byID.set('crowd/' + id, { key: 'k' + i, staged });
  });
  const state = {
    instancedGLBMeshes: [{ id: 'crowd', instances: ids.map(id => ({ id })) }],
    _hydratedModelRecords: { rigidInstances, rigidInstancesByID: byID },
    _crowdRenderer: { prepareCrowdMotionShaders: () => true },
  };
  return { ids, objects, locals, ship, state, atlas, anim };
}

function runScenario(env, runtime, renderer, count, kind) {
  const parented = kind === 'partial';
  const scene = makeScene(env, count, parented);
  const { api } = env;
  const list = parented ? [scene.ship, ...scene.objects] : scene.objects;
  const camera = { x: 0, y: 8, z: 30, fov: 60, near: .1, far: 600 };
  const viewport = { cssWidth: 1280, cssHeight: 720, pixelWidth: 1280, pixelHeight: 720, pixelRatio: 1 };
  const capabilities = { retainedGeometry: true, worldBakedAttributeViews: true, rigidImportedBatches: true, crowdBatches: true };
  const lights = [{ id: 'sun', kind: 'directional', directionX: .3, directionY: -1, directionZ: -.4, intensity: 1.2 }];
  const planes = [[1, 0, 0, 1e6], [-1, 0, 0, 1e6], [0, 0, 1, 1e6], [0, 0, -1, 1e6]];
  const P = new Float32Array(16);
  const changeCount = Math.max(1, Math.round(count * share));
  const frameBytes = [], tickMS = [], frameMS = [], uploadBytes = [];
  let prepared = null, seed = 12345;
  const rand = () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 4294967296; };
  const handle = {};

  // Agents walk along the deck; keys for tick n are the motion from n to n+1.
  const localKey = (i, n) => {
    const l = scene.locals[i];
    return { x0: l.x + n * .1 * (i % 3 - 1), x1: l.x + (n + 1) * .1 * (i % 3 - 1), z: l.z, yaw: l.yaw };
  };
  const frames = ticks * FRAMES_PER_TICK;
  const warm = 3 * FRAMES_PER_TICK;
  for (let frame = -warm; frame < frames; frame++) {
    const t = frame / 60;
    const tick = frame >= 0 ? Math.floor(frame / FRAMES_PER_TICK) : -1;
    const isTick = frame % FRAMES_PER_TICK === 0;
    const n = isTick ? Math.floor(frame / FRAMES_PER_TICK) + 1000 : 0;
    shipPose(P, t);
    let tickCost = 0;
    if (isTick) {
      // Which agents changed this tick. The first tick restates everyone.
      let bytes;
      const first = frame <= -warm;
      const t0 = t, t1 = t + FRAMES_PER_TICK / 60;
      if (kind === 'full' || first) {
        const keys = scene.ids.map((_id, i) => {
          const k = localKey(i, n);
          const world = !parented;
          const prev = world ? apply(P, k.x0, 0, k.z) : [k.x0, 0, k.z], next = world ? apply(P, k.x1, 0, k.z) : [k.x1, 0, k.z];
          return { prev, next, yaw: k.yaw, t0, t1 };
        });
        bytes = fullFrame('crowd', scene.ids, keys);
      } else {
        const picked = new Set();
        while (picked.size < changeCount) picked.add(Math.floor(rand() * count));
        const slots = Array.from(picked).sort((a, b) => a - b);
        const keys = new Map();
        for (const slot of slots) {
          const k = localKey(slot, n);
          keys.set(slot, { prev: [k.x0, 0, k.z], next: [k.x1, 0, k.z], yaw: k.yaw, t0, t1 });
        }
        bytes = partialFrame('crowd', count, slots, keys);
      }
      const begin = performance.now();
      const batches = runtime.bridge.decodeMotionFrame(bytes);
      runtime.bridge.applyMountedMotionFrame(scene.state, batches, runtime.updateRigidMotion, () => {}, handle);
      tickCost = performance.now() - begin;
      if (frame >= 0) { frameBytes.push(bytes.length); tickMS.push(tickCost); }
    }
    if (parented) scene.ship.parentMatrix.set(P);
    const uploadsBefore = renderer.uploads.bytes;
    const begin = performance.now();
    const bundle = api.createSceneRenderBundle(viewport.cssWidth, viewport.cssHeight, '#101820', Object.assign({}, camera),
      list, [], [], [], lights, {}, t, [], [], [], [], [], 0, false, capabilities);
    prepared = api.prepareScene(bundle, bundle.camera, viewport, prepared);
    renderer.context.prebuiltRigidBatchEpoch++;
    renderer.context.instancedStreamEpoch++;
    for (const record of bundle.meshObjects) {
      if (record._crowdBatch !== true) continue;
      renderer.context.prepareCrowdBatchRecord(record, planes);
      const batch = renderer.context.prebuiltRigidBatchRecords.get(record.id);
      renderer.context.crowdMotionStreamBuffer(batch);
    }
    const cost = performance.now() - begin;
    if (frame >= 0) {
      frameMS.push(cost + tickCost);
      if (isTick) uploadBytes.push(renderer.uploads.bytes - uploadsBefore);
    }
  }
  return {
    wire: median(frameBytes), tick: median(tickMS), frame: median(frameMS.filter((_v, i) => i % FRAMES_PER_TICK !== 0)),
    average: frameMS.reduce((a, b) => a + b, 0) / frameMS.length, upload: median(uploadBytes),
  };
}

const env = await loadEnvironment();
const runtime = loadFrameRuntime(env.window);
console.log(`node=${process.version} tick every ${FRAMES_PER_TICK} frames (10 Hz at 60 fps), ${ticks} ticks, ${share * 100}% of agents change per tick`);
console.log('agents | path | wire B/tick | decode+apply ms/tick | other ms/frame | avg ms/frame | GL upload B/tick');
for (const count of sizes) {
  const results = { full: [], partial: [] };
  for (let run = 0; run < RUNS; run++) {
    for (const kind of ['full', 'partial']) {
      if (global.gc) global.gc();
      results[kind].push(runScenario(env, runtime, loadRenderer(), count, kind));
    }
  }
  for (const kind of ['full', 'partial']) {
    const rows = results[kind];
    const pick = field => median(rows.map(row => row[field]));
    console.log(`${count} | ${kind} | ${pick('wire').toFixed(0)} | ${pick('tick').toFixed(2)} | ${pick('frame').toFixed(2)} | ${pick('average').toFixed(2)} | ${pick('upload').toFixed(0)}`);
  }
}
process.exit(0);
