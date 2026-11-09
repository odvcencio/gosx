import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import test from 'node:test';

const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8');

function load(files = ['depthfx', 'motion-blur', 'motion-blur-wgpu', 'motion-blur-gl']) {
  const window = { console };
  const context = vm.createContext({ window, console, Float32Array, Int32Array, Math, Object, Array, Map, WeakMap, performance: { now: () => clock }, Date,
    GPUBufferUsage: { UNIFORM: 1, COPY_DST: 2, STORAGE: 4 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2 }, GPUTextureUsage: { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 } });
  for (const name of files) {
    vm.runInContext(ts.transpileModule(read('../runtime/scene3d/' + name + '.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  }
  return window.__gosx_scene3d_depthfx_api;
}
let clock = 0;

// A perspective view-projection looking down -Z from (x, 0, 5), in WebGPU clip range (z 0..1) or GL (-1..1).
function perspective(x = 0, zeroToOne = true, yaw = 0) {
  const near = 0.1, far = 100, f = 1 / Math.tan(Math.PI / 6), aspect = 16 / 9;
  const proj = new Float32Array(16);
  proj[0] = f / aspect; proj[5] = f; proj[11] = -1;
  if (zeroToOne) { proj[10] = far / (near - far); proj[14] = near * far / (near - far); }
  else { proj[10] = (far + near) / (near - far); proj[14] = 2 * far * near / (near - far); }
  const c = Math.cos(yaw), s = Math.sin(yaw);
  // view = rotateY(yaw) * translate(-x, 0, -5)
  const view = Float32Array.from([c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, -x * c - 5 * s, 0, x * s - 5 * c, 1]);
  const out = new Float32Array(16);
  for (let col = 0; col < 4; col++) for (let row = 0; row < 4; row++) {
    let sum = 0;
    for (let k = 0; k < 4; k++) sum += proj[k * 4 + row] * view[col * 4 + k];
    out[col * 4 + row] = sum;
  }
  return out;
}

test('plan reads the preset, clamps to the tile, and follows the quality tier', () => {
  const fx = load(['depthfx', 'motion-blur']);
  let plan = fx.motionBlurPlan({ quality: 'high', strength: 0.75, maxBlur: 100 }, {}, 1080);
  assert.deepEqual([plan.skip, plan.samples, plan.tile, plan.maxBlur, plan.strength], [false, 16, 16, 16, 0.75]);
  plan = fx.motionBlurPlan({}, {}, 1080);
  assert.deepEqual([plan.samples, plan.tile, plan.maxBlur, plan.strength, plan.quality], [10, 24, 24, 0.5, 'medium']);
  plan = fx.motionBlurPlan({ quality: 'low', maxBlur: 8 }, {}, 540);
  assert.equal(plan.maxBlur, 4, 'blur limit scales with canvas height');
  const frame = tier => ({ meta: { qualityEnabled: true, qualityTier: tier } });
  assert.equal(fx.motionBlurPlan({ quality: 'high' }, frame('survival'), 1080).skip, true);
  assert.equal(fx.motionBlurPlan({ quality: 'high' }, frame('low'), 1080).skip, true);
  plan = fx.motionBlurPlan({ quality: 'high' }, frame('balanced'), 1080);
  assert.deepEqual([plan.skip, plan.quality, plan.samples], [false, 'low', 6]);
  plan = fx.motionBlurPlan({ quality: 'high' }, frame('full'), 1080);
  assert.deepEqual([plan.skip, plan.quality], [false, 'high']);
  assert.equal(fx.motionBlurPlan({ quality: 'ultra' }, {}, 1080).quality, 'medium', 'unknown quality uses the default');
});

test('history needs a previous frame, resets after a gap, and after a camera cut', () => {
  const fx = load(['depthfx', 'motion-blur']);
  const h = fx.createMotionHistory();
  const eye = new Float32Array([0, 0, 5]);
  const a = perspective(0), b = perspective(0.5);
  assert.equal(h.begin(0, eye, a), false, 'first frame has no history');
  assert.equal(h.begin(16, eye, b), true);
  assert.deepEqual([...h.prev], [...a], 'the previous matrix is the last frame\'s');
  assert.equal(h.begin(416, eye, a), false, 'a 400 ms gap drops history');
  assert.equal(h.begin(432, new Float32Array([40, 0, 5]), b), false, 'a 40 unit jump is a cut');
  assert.equal(h.begin(448, new Float32Array([40, 0, 5]), a), true, 'history resumes after the cut');
  h.reset();
  assert.equal(h.begin(464, eye, a), false);
});

test('a second render inside one animation frame keeps the previous matrix', () => {
  const fx = load(['depthfx', 'motion-blur']);
  const h = fx.createMotionHistory();
  const eye = new Float32Array([0, 0, 5]);
  const a = perspective(0), b = perspective(0.5);
  h.begin(0, eye, a);
  assert.equal(h.begin(16, eye, b), true);
  assert.equal(h.begin(17, eye, b), true, 'the repeat render still blurs');
  assert.deepEqual([...h.prev], [...a], 'and uses the same previous matrix');
  assert.equal(h.begin(33, eye, perspective(1)), true);
  assert.deepEqual([...h.prev], [...b], 'the next real frame moves history on');
});

test('eye and forward come out of the inverse view-projection', () => {
  const fx = load(['depthfx', 'motion-blur']);
  const inv = new Float32Array(16);
  assert.equal(fx.invert(inv, perspective(2, true, 0)), true);
  const eye = new Float32Array(3), forward = new Float32Array(3);
  fx.eyeAndForward(inv, 0, 1, eye, forward);
  assert.ok(Math.abs(forward[2] + 1) < 1e-4 && Math.abs(forward[0]) < 1e-4, forward.join());
  assert.ok(Math.abs(eye[0] - 2) < 1e-3 && eye[2] < 5 && eye[2] > 4.8, eye.join());
  fx.invert(inv, perspective(2, false, 0));
  fx.eyeAndForward(inv, -1, 1, eye, forward);
  assert.ok(Math.abs(forward[2] + 1) < 1e-4 && Math.abs(eye[0] - 2) < 1e-3, 'GL clip range gives the same camera');
});

function mockGL({ floatTargets = true } = {}) {
  const calls = [];
  const gl = new Proxy({}, {
    get(target, name) {
      if (name in target) return target[name];
      if (typeof name === 'symbol') return undefined;
      if (name === 'getExtension') return () => (floatTargets ? {} : null);
      if (name === 'checkFramebufferStatus') return () => 'FRAMEBUFFER_COMPLETE';
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

test('WebGL blur skips the first frame, then runs four passes per frame and restores state', () => {
  const fx = load();
  const { gl, calls } = mockGL();
  const state = { gl };
  const effect = { kind: 'motionBlur', quality: 'medium', strength: 0.5, maxBlur: 32 };
  const frame = x => ({ viewProj: perspective(x, false), camera: {}, lights: [] });
  clock = 1000;
  const input = { c: 1 }, target = { fbo: 'f', colorTex: { t: 'out' } };
  assert.equal(fx.webglRun(state, effect, 0, input, target, 320, 180, { d: 1 }, frame(0)), input, 'no previous frame yet');
  assert.equal(calls.filter(c => c[0] === 'drawArrays').length, 0);
  clock = 1016;
  assert.equal(fx.webglRun(state, effect, 0, input, target, 320, 180, { d: 1 }, frame(0.1)), target.colorTex);
  assert.equal(calls.filter(c => c[0] === 'drawArrays').length, 4, 'velocity, tile max, neighbor max, gather');
  const viewports = calls.filter(c => c[0] === 'viewport').map(c => [c[3], c[4]]);
  assert.deepEqual(viewports, [[320, 180], [14, 8], [14, 8], [320, 180]], 'tile targets are ceil(size / 24)');
  assert.equal(calls.filter(c => c[0] === 'bindVertexArray').pop()[1], null);
  fx.dispose(state);
});

test('WebGL blur needs float color targets and a depth texture', () => {
  const fx = load();
  const effect = { kind: 'motionBlur' };
  const frame = { viewProj: perspective(0, false), camera: {} };
  clock = 2000;
  let m = mockGL({ floatTargets: false });
  let state = { gl: m.gl };
  const input = { c: 1 };
  fx.webglRun(state, effect, 0, input, null, 64, 64, {}, frame);
  clock = 2016;
  assert.equal(fx.webglRun(state, effect, 0, input, null, 64, 64, {}, frame), input);
  m = mockGL();
  state = { gl: m.gl };
  clock = 3000;
  fx.webglRun(state, effect, 0, {}, null, 64, 64, {}, frame);
  clock = 3016;
  assert.equal(fx.webglRun(state, effect, 0, input, null, 64, 64, null, frame), input, 'no depth texture');
});

function mockDevice() {
  const log = [];
  const resource = (kind, extra = {}) => ({ kind, destroy() {}, createView() { return { view: kind }; }, ...extra });
  const pass = { setPipeline: p => log.push(['pipeline', p.label]), setBindGroup: () => {}, draw: n => log.push(['draw', n]), end: () => {} };
  const device = {
    queue: { writeBuffer: (_b, _o, data) => log.push(['write', data.length]) },
    createBuffer: d => resource('buffer', d), createTexture: d => resource('texture', d), createShaderModule: d => resource('module', d),
    createBindGroupLayout: () => resource('bgl'), createPipelineLayout: () => resource('pl'), createBindGroup: d => resource('group', d),
    createRenderPipeline: d => ({ label: d.label, targets: d.fragment.targets }),
    createCommandEncoder: () => ({ beginRenderPass: d => { log.push(['pass', d.label]); return pass; } }),
  };
  return { device, log };
}

test('WebGPU blur runs four passes and feeds mover velocity when the renderer supplies it', () => {
  const fx = load();
  const { device, log } = mockDevice();
  const state = { device };
  const effect = { kind: 'motionBlur', quality: 'high' };
  const encoder = device.createCommandEncoder();
  const frame = (x, motion) => ({ viewProj: perspective(x, true), camera: {}, lights: [], motion });
  clock = 5000;
  const input = { id: 'in' }, output = { id: 'out' };
  assert.equal(fx.webgpuRun(state, effect, 1, encoder, input, output, 'rgba16float', {}, 640, 360, frame(0), null), input);
  clock = 5016;
  const velocityView = { v: 1 };
  assert.equal(fx.webgpuRun(state, effect, 1, encoder, input, output, 'rgba16float', {}, 640, 360, frame(0.1, { velocityView }), null), output);
  const passes = log.filter(e => e[0] === 'pass').map(e => e[1]);
  assert.deepEqual(passes, ['gosx-motion-velocity', 'gosx-motion-tilemax', 'gosx-motion-neighbor', 'gosx-motion-gather']);
  const targets = log.filter(e => e[0] === 'pipeline').map(e => e[1]);
  assert.equal(targets.length, 4);
  fx.dispose(state);
});

test('WebGPU blur does nothing on the survival tier but keeps its history', () => {
  const fx = load();
  const { device, log } = mockDevice();
  const state = { device };
  const encoder = device.createCommandEncoder();
  const tiered = { viewProj: perspective(0, true), camera: {}, meta: { qualityEnabled: true, qualityTier: 'survival' } };
  clock = 7000;
  const input = { id: 'in' }, output = { id: 'out' };
  fx.webgpuRun(state, { kind: 'motionBlur' }, 0, encoder, input, output, 'rgba16float', {}, 64, 64, tiered, null);
  clock = 7016;
  assert.equal(fx.webgpuRun(state, { kind: 'motionBlur' }, 0, encoder, input, output, 'rgba16float', {}, 64, 64, tiered, null), input);
  assert.equal(log.filter(e => e[0] === 'pass').length, 0);
  // Back on the full tier the very next frame blurs: history kept advancing.
  clock = 7032;
  const full = { viewProj: perspective(0.1, true), camera: {} };
  assert.equal(fx.webgpuRun(state, { kind: 'motionBlur' }, 0, encoder, input, output, 'rgba16float', {}, 64, 64, full, null), output);
});
