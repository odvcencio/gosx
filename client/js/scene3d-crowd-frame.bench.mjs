// Per-frame main-thread cost of an animated crowd: render bundle build plus
// planner, in Node, with no GL or GPU.
//
//   node client/js/scene3d-crowd-frame.bench.mjs [--mode motion|pose] [--sizes 500,2000,5000] [--frames 200]
//
// It loads the shipped bootstrap.js (run `GOWORK=off make build-bootstrap`
// after changing Scene3D sources) and calls the same two functions the mount
// calls every rendered frame: createSceneRenderBundle, then prepareScene with
// the previous result. Each agent is shaped like a hydrated InstancedGLB crowd
// object: a shared immutable skinned primitive, a per-agent parentMatrix, a
// per-agent _crowdSkin pose record and, in motion mode, a per-agent
// _crowdMotion record (the GSP3 path).
//
// Three cases per crowd size, reported as median ms per frame:
//   a  nothing changes
//   b  the camera moves
//   c  a motion frame arrives: every agent's motion record changes
// For c the timer covers only bundle build and planner. Applying the frame to
// the agents (sceneUpdateRigidInstanceMotion, one record write per agent) is
// not timed. The input list is the mount's flat object array; building it
// (Array.from over the state map) is not timed either.
//
// Node's JIT, the fake globals and the missing GPU make absolute numbers
// differ from a browser. Compare runs from the same machine.
//
// Baseline on origin/main 0ae3f5174 (Node v26.8.1, motion mode, median
// per-frame total of bundle build + planner over 200 frames; run-to-run spread
// is about 10% at 5,000 agents):
//   agents   a nothing changes   b camera moves   c motion frame
//   500      2.8 ms              2.4 ms           2.6 ms
//   2000     14.8 ms             12.5 ms          13.1 ms
//   5000     26 ms               27 ms            25 ms
import { createRequire } from 'node:module';
import { performance } from 'node:perf_hooks';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const H = require(path.join(here, 'runtime-test-harness.js'));

function arg(name, fallback) {
  const index = process.argv.indexOf('--' + name);
  return index >= 0 && process.argv[index + 1] ? process.argv[index + 1] : fallback;
}
const mode = arg('mode', 'motion');
const sizes = arg('sizes', '500,2000,5000').split(',').map(Number);
const frames = Number(arg('frames', '200'));
const warmup = 40;

// Run bootstrap.js with ordinary globals, as a browser does. A contextified VM
// global makes numeric work several times slower and would swamp the result.
async function loadAPI() {
  const env = H.createContext({});
  const names = ['window', 'globalThis', 'document', 'console', 'navigator', 'performance', 'Date', 'Math', 'JSON',
    'requestAnimationFrame', 'cancelAnimationFrame', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval',
    'fetch', 'Image', 'ResizeObserver', 'IntersectionObserver', 'MutationObserver', 'getComputedStyle', 'matchMedia',
    'WebGL2RenderingContext', 'GPUBufferUsage', 'GPUTextureUsage', 'GPUShaderStage', 'CustomEvent', 'Event',
    'innerWidth', 'innerHeight', 'devicePixelRatio', 'screen'];
  const c = env.context;
  new Function(...names, H.bootstrapSource)(...names.map(key => key === 'globalThis' ? c : (c[key] !== undefined ? c[key] : globalThis[key])));
  await H.flushAsyncWork();
  return c.__gosx_scene3d_api;
}

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
  return { count: 36, positions: f32(positions), normals: f32(normals), uvs: f32(uvs), tangents: f32(tangents),
    joints: f32(joints), weights: f32(weights) };
}

function makeCrowd(api, count) {
  const shared = cubeVertices();
  const proto = api.normalizeSceneObject({ id: 'proto', kind: 'mesh', vertices: shared, materialKind: 'standard',
    color: '#c8b27a', roughness: .6, receiveShadow: true, pickable: false }, 0);
  proto.vertices.immutable = true;
  proto.vertices.revision = 0;
  const atlas = { id: 'bench-atlas' };
  const bounds = { minX: -.5, minY: -.5, minZ: -.5, maxX: .5, maxY: .5, maxZ: .5 };
  const batch = { id: 'bench-batch', revision: 0 };
  const objects = [];
  for (let i = 0; i < count; i++) {
    const object = Object.assign({}, proto, {
      id: `bench-batch/agent-${i}/0`, x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
      spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0, driftSpeed: 0, driftPhase: 0,
      static: false, skin: null, vertices: proto.vertices,
      parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, (i % 100) * .6 - 30, 0, -Math.floor(i / 100) * .6, 1]),
    });
    object._crowdSkin = { atlas, bounds, rows: new Float32Array([1, 2, .5]), poseRows: null, batch };
    if (mode === 'motion') {
      object._crowdMotion = { atlas, clipTable: { count: 1 }, record: new Float32Array(24), dirty: true,
        bounds: { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 } };
    }
    objects.push(object);
  }
  return { objects, batch };
}

// The write sceneUpdateRigidInstanceMotion and the pose path make, then the
// dirty signal the bundle builder reads. The signal is absent on builds that
// predate batch records.
function applyMotionFrame(api, crowd, tick) {
  for (const object of crowd.objects) {
    if (object._crowdMotion) {
      object._crowdMotion.record[0] = tick * .1;
      object._crowdMotion.dirty = true;
      object._crowdMotion.bounds.maxX = 1 + tick * .1;
    } else {
      object._crowdSkin.rows[2] = (tick % 10) / 10;
    }
  }
  if (typeof api.sceneCrowdBatchTouch === 'function') api.sceneCrowdBatchTouch(crowd.batch);
}

function median(values) {
  const sorted = values.slice().sort((a, b) => a - b);
  return sorted[sorted.length >> 1];
}

function runCase(api, count, kind) {
  const crowd = makeCrowd(api, count);
  const camera = { x: 0, y: 6, z: 24, fov: 60, near: .1, far: 600 };
  const viewport = { cssWidth: 1280, cssHeight: 720, pixelWidth: 1280, pixelHeight: 720, pixelRatio: 1 };
  const capabilities = { retainedGeometry: true, worldBakedAttributeViews: true, rigidImportedBatches: true, crowdBatches: true };
  const lights = [{ id: 'sun', kind: 'directional', directionX: .3, directionY: -1, directionZ: -.4, intensity: 1.2 }];
  let prepared = null;
  const bundleMS = [], plannerMS = [];
  for (let frame = -warmup; frame < frames; frame++) {
    if (kind === 'b') camera.x = Math.sin(frame * .05) * 4;
    if (kind === 'c') applyMotionFrame(api, crowd, frame);
    const t0 = performance.now();
    const bundle = api.createSceneRenderBundle(viewport.cssWidth, viewport.cssHeight, '#101820', Object.assign({}, camera),
      crowd.objects, [], [], [], lights, {}, frame / 60, [], [], [], [], [], 0, false, capabilities);
    const t1 = performance.now();
    prepared = api.prepareScene(bundle, bundle.camera, viewport, prepared);
    const t2 = performance.now();
    if (frame >= 0) { bundleMS.push(t1 - t0); plannerMS.push(t2 - t1); }
  }
  return { bundle: median(bundleMS), planner: median(plannerMS), total: median(bundleMS.map((v, i) => v + plannerMS[i])) };
}

const api = await loadAPI();
console.log(`mode=${mode} frames=${frames} node=${process.version}`);
console.log('agents | case | bundle ms | planner ms | total ms');
for (const count of sizes) {
  for (const [kind, label] of [['a', 'a nothing changes'], ['b', 'b camera moves'], ['c', 'c motion frame']]) {
    if (global.gc) global.gc();
    const result = runCase(api, count, kind);
    console.log(`${count} | ${label} | ${result.bundle.toFixed(2)} | ${result.planner.toFixed(2)} | ${result.total.toFixed(2)}`);
  }
}
process.exit(0);
