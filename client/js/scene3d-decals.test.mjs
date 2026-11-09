import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import test from 'node:test';

const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8');

let lastWindow = null;
function load(files = ['depthfx', 'decal-data', 'decal-gpu', 'decal-gl']) {
  const window = lastWindow = { console };
  const context = vm.createContext({ window, console, Float32Array, Int32Array, Uint32Array, Float64Array, Math, Object, Array, Map, WeakMap, Promise, GPUBufferUsage: { UNIFORM: 1, COPY_DST: 2, STORAGE: 4 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2 }, GPUTextureUsage: { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 } });
  context.fetch = () => Promise.reject(new Error('no network in unit test'));
  for (const name of files) {
    vm.runInContext(ts.transpileModule(read('../runtime/scene3d/' + name + '.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  }
  return window.__gosx_scene3d_depthfx_api;
}

const REC = 20;
function record({ pos = [0, 0, 0], opacity = 1, q = [0, 0, 0, 1], size = [1, 1, 1], frame = 0, tint = [1, 1, 1, 1], layer = 0, seq = 0, material = 0 } = {}) {
  return [...pos, opacity, ...q, ...size, frame, ...tint, layer, seq, material, 0];
}
const material = (extra = {}) => ({ name: 'm', albedo: '', columns: 1, rows: 1, roughness: 0.8, edgeFade: 0.1, angleFadeStart: 65, angleFadeEnd: 88, normalStrength: 1, ...extra });
const identity = () => Float32Array.from([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

// Orthographic view of a 20x20x20 box centered on the origin: everything in it is visible.
function orthoVP() {
  const m = new Float32Array(16);
  m[0] = 0.1; m[5] = 0.1; m[10] = -0.1; m[15] = 1; // GL clip: z in [-1, 1]
  return m;
}
function orthoVPZeroOne() {
  const m = new Float32Array(16);
  m[0] = 0.1; m[5] = 0.1; m[10] = -0.05; m[14] = 0.5; m[15] = 1;
  return m;
}

test('matrix invert round trips and rejects singular matrices', () => {
  const fx = load(['depthfx']);
  const m = Float32Array.from([2, 0, 0, 0, 0, 3, 0, 0, 0, 0, 4, 0, 5, 6, 7, 1]);
  const inv = new Float32Array(16);
  assert.equal(fx.invert(inv, m), true);
  const out = new Float32Array(16);
  fx.mul(out, m, inv);
  for (let i = 0; i < 16; i++) assert.ok(Math.abs(out[i] - identity()[i]) < 1e-5, `entry ${i}: ${out[i]}`);
  assert.equal(fx.invert(new Float32Array(16), new Float32Array(16)), false);
});

test('frustum planes accept both clip ranges and reject boxes outside', () => {
  const fx = load(['depthfx']);
  const planes = new Float32Array(24);
  fx.frustumPlanes(planes, orthoVP(), false);
  assert.equal(fx.sphereVisible(planes, 0, 0, 0, 1), true);
  assert.equal(fx.sphereVisible(planes, 30, 0, 0, 1), false);
  assert.equal(fx.sphereVisible(planes, 0, 0, 25, 1), false);
  fx.frustumPlanes(planes, orthoVPZeroOne(), true);
  assert.equal(fx.sphereVisible(planes, 0, 0, 0, 1), true);
  assert.equal(fx.sphereVisible(planes, 30, 0, 0, 1), false);
});

test('lightSummary reads ambient and the first directional light in linear space', () => {
  const fx = load(['depthfx']);
  const ambient = new Float32Array(3), dir = new Float32Array(3), color = new Float32Array(3);
  const sun = fx.lightSummary({
    environment: { ambientColor: '#ffffff', ambientIntensity: 0.25 },
    lights: [{ kind: 'ambient', color: '#ffffff', intensity: 0.25 }, { kind: 'directional', color: '#ffffff', intensity: 2, directionX: 0, directionY: -2, directionZ: 0 }, { kind: 'directional', color: '#ff0000', intensity: 9, directionX: 1, directionY: 0, directionZ: 0 }],
  }, ambient, dir, color);
  assert.equal(sun, true);
  assert.deepEqual([...ambient].map(v => +v.toFixed(3)), [0.5, 0.5, 0.5]);
  assert.deepEqual([...dir], [0, -1, 0]);
  assert.deepEqual([...color].map(v => +v.toFixed(3)), [2, 2, 2]);
  assert.equal(fx.lightSummary({}, ambient, dir, color), false);
  assert.deepEqual([...dir], [0, -1, 0]);
});

test('decal instances place the box and invert it exactly', () => {
  const fx = load(['depthfx', 'decal-data']);
  const set = fx.createDecalSet();
  // Facing +X: rotate local +Z onto +X (90 degrees about Y).
  const s = Math.SQRT1_2;
  const rec = record({ pos: [3, 1, -2], q: [0, s, 0, s], size: [4, 2, 0.5], frame: 3 });
  assert.equal(set.update({ materials: [material({ columns: 2, rows: 2 })], records: rec }), true);
  assert.equal(set.prepare(orthoVP(), false, new Float32Array([0, 0, 0])), 1);
  const inst = set.instances;
  // Local +Z corner (0, 0, 0.5) maps along world +X by half the depth.
  const model = inst.subarray(0, 16), inv = inst.subarray(16, 32);
  const world = [model[8] * 0.5 + model[12], model[9] * 0.5 + model[13], model[10] * 0.5 + model[14]];
  assert.ok(Math.abs(world[0] - 3.25) < 1e-5 && Math.abs(world[1] - 1) < 1e-5 && Math.abs(world[2] + 2) < 1e-5, String(world));
  // inverse * world point returns the local point.
  const local = [0, 1, 2].map(r => inv[r] * world[0] + inv[4 + r] * world[1] + inv[8 + r] * world[2] + inv[12 + r]);
  assert.ok(Math.abs(local[0]) < 1e-5 && Math.abs(local[1]) < 1e-5 && Math.abs(local[2] - 0.5) < 1e-5, local.join());
  // Frame 3 of a 2x2 atlas: column 1, row 1.
  assert.deepEqual([...inst.subarray(32, 36)], [0.5, 0.5, 0.5, 0.5]);
});

test('decals sort by layer, keep source order within a layer, and group material runs', () => {
  const fx = load(['depthfx', 'decal-data']);
  const set = fx.createDecalSet();
  const records = [
    record({ layer: 1, material: 0, pos: [1, 0, 0] }),
    record({ layer: 0, material: 1, pos: [2, 0, 0] }),
    record({ layer: 0, material: 1, pos: [3, 0, 0] }),
    record({ layer: 0, material: 0, pos: [4, 0, 0] }),
  ].flat();
  set.update({ materials: [material(), material({ name: 'b' })], records });
  assert.equal(set.prepare(orthoVP(), false, new Float32Array(3)), 4);
  const xs = [0, 1, 2, 3].map(i => set.instances[i * 40 + 12]);
  assert.deepEqual(xs, [2, 3, 4, 1], 'layer 0 first, in source order, then layer 1');
  assert.equal(set.runs, 2, 'adjacent decals of one material share a run');
  assert.deepEqual([0, 1].map(i => [set.runMaterial[i], set.runFirst[i], set.runLength[i]]), [[1, 0, 2], [0, 2, 2]]);
});

test('culling drops decals beyond maxDistance and outside the frustum', () => {
  const fx = load(['depthfx', 'decal-data']);
  const set = fx.createDecalSet();
  const records = [
    record({ pos: [0, 0, 0] }),
    record({ pos: [50, 0, 0] }),   // outside the 10-unit half-extent frustum
    record({ pos: [0, 5, 0] }),    // visible but beyond maxDistance 4 from the eye
  ].flat();
  set.update({ materials: [material()], records, maxDistance: 4 });
  assert.equal(set.prepare(orthoVP(), false, new Float32Array([0, 0, 0])), 1);
});

test('pool streams join the static set and rebuild only when their revision changes', () => {
  const fx = load(['depthfx', 'decal-data']);
  const set = fx.createDecalSet();
  const data = new Float32Array(REC * 4);
  data.set(record({ pos: [1, 0, 0] }), 0);
  const store = { data, count: 1, revision: 1 };
  const effect = { materials: [material()], records: record({ pos: [-1, 0, 0] }), pools: ['hits'] };
  window_pools(fx, { hits: store });
  assert.equal(set.update(effect), true);
  assert.equal(set.prepare(orthoVP(), false, new Float32Array(3)), 2);
  assert.equal(set.update(effect), false, 'no change');
  store.count = 2; data.set(record({ pos: [2, 0, 0] }), REC); store.revision = 2;
  assert.equal(set.update(effect), true);
  assert.equal(set.prepare(orthoVP(), false, new Float32Array(3)), 3);
  // Same view and same set: prepare is a no-op and keeps the result.
  assert.equal(set.prepare(orthoVP(), false, new Float32Array(3)), 3);
});

function window_pools(_fx, pools) {
  // The decal data module reads window.__gosx_scene3d_decal_pools from its own window.
  lastWindow.__gosx_scene3d_decal_pools = pools;
}

test('material uniforms encode fade cosines, blend mode and map flags', () => {
  const fx = load(['depthfx', 'decal-data']);
  const set = fx.createDecalSet();
  set.update({ materials: [material({ blend: 'multiply', normalMap: '/n.png', emissiveMap: '/e.png', emissiveColor: '#ffffff', emissiveIntensity: 2, color: '#ffffff', angleFadeStart: 60, angleFadeEnd: 80, edgeFade: 0.3 })], records: [] });
  const u = set.materials[0].uniforms;
  assert.ok(Math.abs(u[0] - 0.5) < 1e-6 && Math.abs(u[1] - Math.cos(80 * Math.PI / 180)) < 1e-6);
  assert.ok(Math.abs(u[2] - 0.3) < 1e-6);
  assert.equal(u[5], 2);
  assert.equal(u[6], 1);
  assert.equal(u[11], 1);
  assert.equal(u[15], 1);
  assert.equal(set.materials[0].multiply, true);
});

test('records with an unknown material index are skipped', () => {
  const fx = load(['depthfx', 'decal-data']);
  const set = fx.createDecalSet();
  set.update({ materials: [material()], records: [record({ material: 3 }), record({ material: 0 })].flat() });
  assert.equal(set.prepare(orthoVP(), false, new Float32Array(3)), 1);
});

// A recording stand-in for a WebGL2 context.
function mockGL() {
  const calls = [];
  const gl = new Proxy({}, {
    get(target, name) {
      if (name in target) return target[name];
      if (typeof name === 'symbol') return undefined;
      if (/^[A-Z0-9_]+$/.test(name)) return name;
      return (...args) => {
        calls.push([name, ...args]);
        if (name === 'getShaderParameter' || name === 'getProgramParameter') return true;
        if (name.startsWith('create')) return { kind: name };
        return undefined;
      };
    },
  });
  return { gl, calls };
}

test('WebGL pass draws one instanced batch per material run with the matching blend', () => {
  const fx = load();
  const { gl, calls } = mockGL();
  const state = { gl };
  const effect = { kind: 'decals', materials: [material(), material({ name: 'b', blend: 'multiply' })], records: [record({ material: 0 }), record({ material: 1 }), record({ material: 1 })].flat() };
  const frame = { viewProj: orthoVP(), camera: { x: 0, y: 0, z: 0 }, lights: [], environment: {} };
  assert.equal(fx.webglRun(state, effect, 0, { tex: 'in' }, { fbo: 'f', colorTex: 'out' }, 64, 64, { tex: 'depth' }, frame), true);
  const draws = calls.filter(c => c[0] === 'drawArraysInstanced');
  assert.deepEqual(draws.map(c => c[4]), [1, 2]);
  const blends = calls.filter(c => c[0] === 'blendFuncSeparate').map(c => c[1]);
  assert.deepEqual(blends, ['ONE', 'DST_COLOR']);
  const offsets = calls.filter(c => c[0] === 'vertexAttribPointer' && c[1] === 0).map(c => c[6]);
  assert.deepEqual(offsets, [0, 160]);
  // The pass restores the state later passes rely on.
  assert.equal(calls.filter(c => c[0] === 'disable' && c[1] === 'BLEND').length >= 2, true);
  assert.equal(calls.filter(c => c[0] === 'bindVertexArray').pop()[1], null);
  fx.dispose(state);
});

test('WebGL pass is a no-op without decals, depth or a view-projection', () => {
  const fx = load();
  const { gl, calls } = mockGL();
  const state = { gl };
  const frame = { viewProj: orthoVP(), camera: {}, lights: [] };
  assert.equal(fx.webglRun(state, { kind: 'decals', materials: [material()], records: [] }, 0, {}, null, 8, 8, {}, frame), false);
  assert.equal(fx.webglRun(state, { kind: 'decals', materials: [material()], records: record() }, 0, {}, null, 8, 8, null, frame), false);
  assert.equal(fx.webglRun(state, { kind: 'decals', materials: [material()], records: record() }, 0, {}, null, 8, 8, {}, {}), false);
  assert.equal(calls.filter(c => c[0] === 'drawArraysInstanced').length, 0);
  assert.equal(fx.webglRun(state, { kind: 'unknown' }, 1, {}, null, 8, 8, {}, frame), false);
});

function mockDevice() {
  const log = [];
  const resource = (kind, extra = {}) => ({ kind, destroy() {}, createView() { return { view: kind }; }, ...extra });
  const pass = {
    setPipeline: p => log.push(['pipeline', p.label]), setBindGroup: (i, g) => log.push(['group', i]),
    draw: (...a) => log.push(['draw', ...a]), end: () => log.push(['end']),
  };
  const device = {
    queue: { writeBuffer: (...a) => log.push(['writeBuffer', a[1], a[4]]), writeTexture() {}, copyExternalImageToTexture() {}, submit() {} },
    createSampler: () => resource('sampler'), createBuffer: d => resource('buffer', d), createTexture: d => resource('texture', d),
    createShaderModule: d => resource('module', d), createBindGroupLayout: () => resource('bgl'), createPipelineLayout: () => resource('pl'),
    createBindGroup: () => resource('group'),
    createRenderPipeline: d => ({ label: d.label, blend: d.fragment.targets[0].blend, cull: d.primitive.cullMode }),
    createCommandEncoder: () => ({ beginRenderPass: d => { log.push(['pass', d.label]); return pass; }, finish: () => ({}) }),
  };
  return { device, log };
}

test('WebGPU pass blits the input, then draws each material run from its first instance', () => {
  const fx = load();
  const { device, log } = mockDevice();
  const state = { device };
  const effect = { kind: 'decals', materials: [material(), material({ name: 'b', blend: 'multiply' })], records: [record({ material: 0 }), record({ material: 0 }), record({ material: 1 })].flat() };
  const frame = { viewProj: orthoVPZeroOne(), camera: { x: 0, y: 0, z: 0 }, lights: [], environment: {} };
  const encoder = device.createCommandEncoder();
  const input = { id: 'in' }, output = { id: 'out' };
  assert.equal(fx.webgpuRun(state, effect, 2, encoder, input, output, 'rgba16float', { id: 'depth' }, 64, 64, frame), true);
  const draws = log.filter(e => e[0] === 'draw');
  assert.deepEqual(draws, [['draw', 3], ['draw', 36, 2, 0, 0], ['draw', 36, 1, 0, 2]]);
  const pipelines = log.filter(e => e[0] === 'pipeline').map(e => e[1]);
  assert.deepEqual(pipelines, ['gosx-decal-blit:rgba16float', 'gosx-decals:rgba16float|lit', 'gosx-decals:rgba16float|mul']);
  fx.dispose(state);
});

test('WebGPU pass leaves the output alone when nothing is visible', () => {
  const fx = load();
  const { device, log } = mockDevice();
  const state = { device };
  const frame = { viewProj: orthoVPZeroOne(), camera: {}, lights: [] };
  const effect = { kind: 'decals', materials: [material()], records: record({ pos: [99, 0, 0] }) };
  assert.equal(fx.webgpuRun(state, effect, 0, device.createCommandEncoder(), {}, {}, 'rgba16float', {}, 8, 8, frame), false);
  assert.equal(log.filter(e => e[0] === 'pass').length, 0);
});

test('a slot rebuilds its pass when the effect kind changes and dispose clears state', () => {
  const fx = load();
  let created = 0, disposed = 0;
  fx.webgpu.decals = () => { created++; return { apply: () => true, dispose: () => { disposed++; } }; };
  fx.webgpu.motionBlur = () => { created++; return { apply: () => true, dispose: () => { disposed++; } }; };
  const state = { device: {} };
  fx.webgpuRun(state, { kind: 'decals' }, 0, 0, 0, 0, '', 0, 1, 1, {});
  fx.webgpuRun(state, { kind: 'decals' }, 0, 0, 0, 0, '', 0, 1, 1, {});
  assert.equal(created, 1);
  fx.webgpuRun(state, { kind: 'motionBlur' }, 0, 0, 0, 0, '', 0, 1, 1, {});
  assert.deepEqual([created, disposed], [2, 1]);
  fx.dispose(state);
  assert.equal(disposed, 2);
  assert.equal(fx.handles('decals') && fx.handles('motionBlur') && !fx.handles('bloom'), true);
});

test('steady-state decal preparation does not allocate typed arrays', () => {
  const fx = load(['depthfx', 'decal-data']);
  const set = fx.createDecalSet();
  const records = [];
  for (let i = 0; i < 200; i++) records.push(record({ pos: [i % 10 - 5, 0, 0], layer: i % 3 }));
  const effect = { materials: [material()], records: records.flat() };
  set.update(effect);
  const eye = new Float32Array(3);
  const vp = orthoVP();
  set.prepare(vp, false, eye);
  const before = set.instances;
  vp[0] = 0.099; // a new view forces a cull pass
  set.prepare(vp, false, eye);
  assert.equal(set.instances, before, 'culled instance buffer is reused');
});
