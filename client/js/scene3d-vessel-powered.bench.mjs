// A powered liner with 14 decks and thousands of parented agents, driven by Go.
// Main-thread cost in Node, with no GPU.
//
//   node client/js/scene3d-vessel-powered.bench.mjs [--sizes 2900] [--ticks 60] [--share 0.05]
//
// The ship's poses come from client/js/testdata/liner-bench-trace.json, written
// by vessel/bench_test.go: 80 poses at 10 Hz of the 290 m, 14-deck liner under
// 0.8 power in a storm, a long turn to port included. They are the numbers the
// Go model produced, byte for byte, and Go tests pin them on amd64, js/wasm and
// arm64 (see vessel/testdata/parity-liner-bench.json).
//
// The bench loads the shipped bundle (run `make build-bootstrap` after changing
// Scene3D sources) and runs what a page runs for this scene:
//
//   every 6th frame (10 Hz tick)   decode a GSV1 pose frame and hand it to the
//                                  external-pose channel; decode a GSP5 partial
//                                  motion frame and apply it to the agents
//   every frame (60 Hz)            blend the pose, update the deck planes and the
//                                  cutaway planes, write the ship's matrix, ask the
//                                  deck surface for eight walker heights, then
//                                  createSceneRenderBundle, prepareScene and the
//                                  WebGL renderer's per-record preparation and
//                                  motion-stream upload, against a mock GL
//
// "cutaway" rows run the same frames with the cutaway to deck 8 switched on and
// fading the decks above. The renderer clips on the GPU, so the CPU cost should
// match; the bench shows that it does. What is not timed: building the frame bytes
// (the Go encoders' job), GPU time.
//
// Node's JIT and the missing GPU make absolute numbers differ from a browser.
// Compare rows from one run on one machine.
//
// Median of five runs, Node v26.8.1, 60 ticks each, 5% of 2,900 agents change per tick:
//   cutaway  wire B/tick  decode+apply ms/tick  ship+decks ms/frame  other ms/frame  avg ms/frame
//   off      13,571       0.73                  0.030                0.40            0.66
//   on       13,571       0.68                  0.032                0.39            0.62
// Wire bytes per tick are a 62-byte pose frame plus a partial motion frame of
// 13,509 bytes (the Go encoder writes 13,514). "worst" is the frame that
// restates all 2,900 agents (7 to 8 ms), once at start-up. The ship, the deck
// planes and eight walker height queries cost about 0.03 ms per frame.
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
const { readSceneRendererBackendSrc } = require(path.join(here, 'scene3d-renderer-source-set.js'));

function arg(name, fallback) {
  const index = process.argv.indexOf('--' + name);
  return index >= 0 && process.argv[index + 1] ? process.argv[index + 1] : fallback;
}
const sizes = arg('sizes', '2900').split(',').map(Number);
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
  const window = c.window || c;
  // The vessel modules are plain JavaScript; run them from source, as the Node tests do.
  const vesselContext = { window, TextDecoder, DataView, Uint8Array, Float32Array, Float64Array, Math, Number, Map, WeakMap, Error, Date, performance };
  for (const name of ['vessel-physics', 'vessel-model', 'vessel-decks', 'vessel-cutaway', 'vessel-external']) {
    vm.runInNewContext(read(`client/runtime/scene3d/${name}.ts`), vesselContext);
  }
  return { api: c.__gosx_scene3d_api, window, anim: window.__gosx_scene3d_animation_api || c.__gosx_scene3d_animation_api };
}

// The command bridge (frame decoding and the mounted apply) and the writer
// sceneUpdateRigidInstanceMotion, from their sources.
function loadFrameRuntime(window) {
  window.document = window.document || {};
  new Function('window', 'document', 'TextDecoder', 'Date', 'Promise', 'setTimeout', transpile(read('client/runtime/scene3d/command-runtime.ts')))(
    window, window.document, TextDecoder, Date, Promise, setTimeout);
  const webglMount = fs.readFileSync(path.join(root, 'client', 'runtime', 'scene3d', 'mount-webgl.ts'), 'utf8');
  const writer = section(webglMount, 'function sceneUpdateRigidInstanceMotion(', 'function sceneRigidMembershipModelID(');
  const updateRigidMotion = new Function('window', transpile(writer) + '\nreturn sceneUpdateRigidInstanceMotion;')(window);
  return { bridge: window.__gosx_scene3d_command_bridge, updateRigidMotion };
}

// The WebGL renderer's per-record preparation and motion-stream upload, against
// a GL mock that counts bytes. Same cut as scene3d-crowd-motion.bench.mjs.
function loadRenderer() {
  const webgl = readSceneRendererBackendSrc('webgl');
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


const trace = JSON.parse(read('client/js/testdata/liner-bench-trace.json'));
const fixtureDecks = JSON.parse(read('client/js/testdata/vessel-decks-liner.json')).decks;
const DECK_COUNT = fixtureDecks.length;
const CUTAWAY_DECK = 8;

// A GSV1 pose frame, as scene.EncodeVesselPoseFrame writes it.
function poseFrame(id, row, snap) {
  const bytes = encoder.encode(id), out = new DataView(new ArrayBuffer(6 + 51 + bytes.length));
  new Uint8Array(out.buffer).set([71, 83, 86, 49]);
  out.setUint16(4, 1, true); out.setUint8(6, snap ? 1 : 0); out.setUint16(7, bytes.length, true);
  new Uint8Array(out.buffer).set(bytes, 9);
  let at = 9 + bytes.length;
  for (let k = 0; k < 4; k++, at += 8) out.setFloat64(at, row[k], true);
  for (let k = 4; k < 8; k++, at += 4) out.setFloat32(at, row[k], true);
  return new Uint8Array(out.buffer);
}

function median(values) {
  const sorted = values.slice().sort((a, b) => a - b);
  return sorted[sorted.length >> 1];
}

function makeScene(env, count) {
  const { api, anim, window } = env;
  const physics = window.__gosx_scene3d_vessel_physics;
  const proto = api.normalizeSceneObject({ id: 'proto', kind: 'mesh', vertices: cubeVertices(), materialKind: 'standard',
    color: '#c8b27a', roughness: .6, receiveShadow: true, pickable: false }, 0);
  proto.vertices.immutable = true;
  proto.vertices.revision = 0;
  const atlas = { id: 'bench-atlas', clips: new Map([['Run', { start: 1, segments: 30, duration: 1 }]]), missingClips: new Set() };
  const bounds = { minX: -.5, minY: -.5, minZ: -.5, maxX: .5, maxY: .5, maxZ: .5 };
  const decks = window.__gosx_scene3d_vessel_decks.create(fixtureDecks, physics);
  decks.update({ x: 0, y: 0, z: 0, heading: 0, pitch: 0, roll: 0 });
  // Agents spread over the 14 decks at deterministic spots on walkable floor.
  let seed = 7;
  const rand = () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 4294967296; };
  const ids = [], objects = [], locals = [];
  for (let i = 0; i < count; i++) {
    const deck = i % DECK_COUNT, y = fixtureDecks[deck].y;
    let x, z;
    do { x = (rand() - .5) * 34; z = (rand() - .5) * 280; } while (decks.deckAt({ x, y: y + .1, z }) !== deck);
    ids.push(`agent-${i}`);
    locals.push({ x, y, z, yaw: (i % 7) * .3 });
    const object = Object.assign({}, proto, {
      id: `crowd/agent-${i}/0`, x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
      spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0, driftSpeed: 0, driftPhase: 0,
      static: false, skin: null, vertices: proto.vertices,
      parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, y, z, 1]),
    });
    object._crowdSkin = { atlas, bounds, rows: new Float32Array([1, 2, .5]), poseRows: null, batchID: 'crowd', rev: 0, group: null, parent: 'ship' };
    objects.push(object);
  }
  const ship = Object.assign({}, proto, { id: 'ship/hull', x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
    spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0, static: false, skin: null, parentMatrix: new Float32Array(16) });
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
  return { ids, objects, locals, ship, state, atlas, anim, decks, physics };
}

function runScenario(env, runtime, renderer, count, cutaway) {
  const scene = makeScene(env, count);
  const { api, window } = env;
  const external = window.__gosx_scene3d_vessel_external, cutawayApi = window.__gosx_scene3d_vessel_cutaway_api;
  const mount = { id: 'bench', attrs: {}, getAttribute(k) { return this.attrs[k] ?? null; }, setAttribute(k, v) { this.attrs[k] = String(v); } };
  const cw = cutawayApi.create(scene.decks, mount, null, cutaway ? { enabled: true, deck: CUTAWAY_DECK, above: 'fade' } : undefined);
  const channel = external.create(null);
  const shipState = { x: 0, y: 0, z: 0, heading: 0, pitch: 0, roll: 0, speed: 0 }, shown = {};
  const list = [scene.ship, ...scene.objects];
  const camera = { x: 0, y: 80, z: 320, fov: 60, near: .1, far: 900 };
  const viewport = { cssWidth: 1280, cssHeight: 720, pixelWidth: 1280, pixelHeight: 720, pixelRatio: 1 };
  const capabilities = { retainedGeometry: true, worldBakedAttributeViews: true, rigidImportedBatches: true, crowdBatches: true };
  const lights = [{ id: 'sun', kind: 'directional', directionX: .3, directionY: -1, directionZ: -.4, intensity: 1.2 }];
  const planes = [[1, 0, 0, 1e6], [-1, 0, 0, 1e6], [0, 0, 1, 1e6], [0, 0, -1, 1e6]];
  const changeCount = Math.max(1, Math.round(count * share));
  const frameBytes = [], tickMS = [], frameMS = [], shipMS = [], uploadBytes = [];
  let prepared = null, seed = 12345;
  const rand = () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 4294967296; };
  const handle = {};
  const walkers = Array.from({ length: 8 }, (_v, i) => ({ x: (i - 4) * 3, z: (i % 3 - 1) * 60, deck: (i * 5) % DECK_COUNT }));

  // Agents walk the decks; keys for tick n are the motion from n to n+1.
  const localKey = (i, n) => {
    const l = scene.locals[i];
    return { x0: l.x + n * .1 * (i % 3 - 1), x1: l.x + (n + 1) * .1 * (i % 3 - 1), y: l.y, z: l.z, yaw: l.yaw };
  };
  const frames = ticks * FRAMES_PER_TICK;
  const warm = 3 * FRAMES_PER_TICK;
  let clock = 0;
  external.now = () => clock;
  for (let frame = -warm; frame < frames; frame++) {
    const t = frame / 60;
    clock = t;
    const isTick = frame % FRAMES_PER_TICK === 0;
    const n = isTick ? Math.floor(frame / FRAMES_PER_TICK) : 0;
    let tickCost = 0, bytesThisTick = 0;
    if (isTick) {
      const first = frame <= -warm;
      const t0 = t, t1 = t + FRAMES_PER_TICK / 60;
      let bytes;
      if (first) {
        const keys = scene.ids.map((_id, i) => {
          const k = localKey(i, n + 1000);
          return { prev: [k.x0, k.y, k.z], next: [k.x1, k.y, k.z], yaw: k.yaw, t0, t1 };
        });
        bytes = fullFrame('crowd', scene.ids, keys);
      } else {
        const picked = new Set();
        while (picked.size < changeCount) picked.add(Math.floor(rand() * count));
        const slots = Array.from(picked).sort((a, b) => a - b);
        const keys = new Map();
        for (const slot of slots) {
          const k = localKey(slot, n + 1000);
          keys.set(slot, { prev: [k.x0, k.y, k.z], next: [k.x1, k.y, k.z], yaw: k.yaw, t0, t1 });
        }
        bytes = partialFrame('crowd', count, slots, keys);
      }
      const poseBytes = poseFrame('liner', trace.poses[(n + warm / FRAMES_PER_TICK) % trace.poses.length], first);
      const begin = performance.now();
      for (const sample of external.decode(poseBytes)) channel.push(sample, clock);
      const batches = runtime.bridge.decodeMotionFrame(bytes);
      runtime.bridge.applyMountedMotionFrame(scene.state, batches, runtime.updateRigidMotion, () => {}, handle);
      tickCost = performance.now() - begin;
      bytesThisTick = poseBytes.length + (first ? 0 : bytes.length);
      if (frame >= 0) { frameBytes.push(bytesThisTick); tickMS.push(tickCost); }
    }
    // What the vessel mount does each frame for a Go-driven ship.
    const shipBegin = performance.now();
    const p = channel.pose(clock, shown);
    if (p) Object.assign(shipState, p);
    scene.decks.update(shipState);
    cw.update(scene.decks);
    env.window.__gosx_scene3d_vessel_model.matrix(shipState, scene.physics, scene.ship.parentMatrix);
    cw.touch(scene.ship);
    let sink = 0;
    for (const w of walkers) {
      const feet = scene.physics.localPoint(shipState, w.x, fixtureDecks[w.deck].y, w.z);
      sink += scene.decks.height(feet.x, feet.z, feet.y);
    }
    const shipCost = performance.now() - shipBegin;
    if (!Number.isFinite(sink)) throw new Error('deck heights are not finite');
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
      frameMS.push(cost + shipCost + tickCost);
      shipMS.push(shipCost);
      if (isTick) uploadBytes.push(renderer.uploads.bytes - uploadsBefore);
    }
  }
  return {
    wire: median(frameBytes), tick: median(tickMS), ship: median(shipMS), frame: median(frameMS.filter((_v, i) => i % FRAMES_PER_TICK !== 0)),
    average: frameMS.reduce((a, b) => a + b, 0) / frameMS.length, worst: Math.max(...frameMS), upload: median(uploadBytes),
    roll: Math.max(...trace.poses.map(row => Math.abs(row[6]))) * 180 / Math.PI,
  };
}

const env = await loadEnvironment();
const runtime = loadFrameRuntime(env.window);
console.log(`node=${process.version} tick every ${FRAMES_PER_TICK} frames (10 Hz at 60 fps), ${ticks} ticks, ${share * 100}% of agents change per tick`);
console.log(`liner: ${DECK_COUNT} decks, Go poses max roll ${Math.max(...trace.poses.map(row => Math.abs(row[6]))) * 180 / Math.PI | 0}.${Math.round(Math.max(...trace.poses.map(row => Math.abs(row[6]))) * 18000 / Math.PI) % 100} deg, state digest ${trace.stateDigest}`);
console.log(`Go encoders: pose frame ${trace.poseFrameBytes} B, partial frame ${trace.partialFrameBytes} B for ${trace.changed} of ${trace.agents} agents, cutaway command ${trace.cutawayJsonBytes} B`);
console.log('agents | cutaway | wire B/tick | decode+apply ms/tick | ship+decks ms/frame | other ms/frame | avg ms/frame | worst ms/frame | GL upload B/tick');
for (const count of sizes) {
  const results = { off: [], on: [] };
  for (let run = 0; run < RUNS; run++) {
    for (const kind of ['off', 'on']) {
      if (global.gc) global.gc();
      results[kind].push(runScenario(env, runtime, loadRenderer(), count, kind === 'on'));
    }
  }
  for (const kind of ['off', 'on']) {
    const rows = results[kind];
    const pick = field => median(rows.map(row => row[field]));
    console.log(`${count} | ${kind} | ${pick('wire').toFixed(0)} | ${pick('tick').toFixed(2)} | ${pick('ship').toFixed(3)} | ${pick('frame').toFixed(2)} | ${pick('average').toFixed(2)} | ${pick('worst').toFixed(2)} | ${pick('upload').toFixed(0)}`);
  }
}
process.exit(0);
