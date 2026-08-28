'use strict';
// Fixture-level tests for the alpha-mask ROUTING slice in
// scene3d-material-ior-browser.cjs. Executes the pure fixture+case source
// prefix, verifies the exact eight-case routing inventory per backend with
// control references and real props (no alphaMode), distinguishes input vs
// effective alpha cutoff, compiles the ACTUAL PRELOAD and READ strings, and
// evaluates the actual PRELOAD wrapper code with stub native APIs to check
// depth/blend observation, PBR qualification, strict forwarding and
// exception behavior. Everything here is scripted; none of it is native
// WebGL/WebGPU evidence.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const zlib = require('node:zlib');

const SRC = fs.readFileSync(
  path.join(__dirname, 'testdata', 'scene3d-material-ior-browser.cjs'), 'utf8');
const start = SRC.indexOf('function buildQuadGLB(');
const end = SRC.indexOf('const byName =');
assert.ok(start >= 0 && end > start, 'slice markers found');
const prefix = SRC.slice(start, end);
const sandbox = vm.createContext({
  Buffer, zlib, console, TextEncoder, TextDecoder,
  F0: (ior) => ((ior - 1) / (ior + 1)) ** 2,
  IBL_FIXTURE: { descriptor: {} },
});
const CASES = JSON.parse(JSON.stringify(
  vm.runInContext(prefix + '\n;CASES;', sandbox)));

const NAMES = ['opaque', 'mask-c5-f5', 'mask-c0-f0', 'discard-c5-f25',
  'alpha-opaque', 'alpha-mask-c5-f5', 'mask-disabled', 'mask-css-cutoff'];
// [expectedOpacity, inputCutoff, effectiveCutoff, depthWrite, blend,
//  expectedEmpty, same, differs, renderPass]
const TABLE = {
  'opaque': [1, undefined, -1, true, false, undefined, undefined, undefined, undefined],
  'mask-c5-f5': [0.5, 0.5, 0.5, true, false, undefined, 'opaque', undefined, undefined],
  'mask-c0-f0': [0, 0, 0, true, false, undefined, 'opaque', undefined, undefined],
  'discard-c5-f25': [0.25, 0.5, 0.5, true, false, true, undefined, 'opaque', undefined],
  'alpha-opaque': [1, undefined, -1, false, true, undefined, undefined, undefined, 'alpha'],
  'alpha-mask-c5-f5': [0.5, 0.5, 0.5, false, true, undefined, 'alpha-opaque', undefined, 'alpha'],
  'mask-disabled': [0.5, null, -1, false, true, undefined, undefined, undefined, undefined],
  'mask-css-cutoff': [0.5, 'var(--mask-cutoff, 0.5)', 0.5, true, false, undefined, 'opaque', undefined, undefined],
};
const routeCases = () => CASES.filter((c) => String(c.name).indexOf('-maskroute-') >= 0);

test('mask-route inventory: exactly 8 unique cases per backend', () => {
  const route = routeCases();
  assert.equal(route.length, NAMES.length * 2);
  assert.equal(new Set(route.map((c) => c.name)).size, route.length);
  for (const [pfx, wg] of [['gl-maskroute-', false], ['wg-maskroute-', true]]) {
    const sub = route.filter((c) => String(c.name).indexOf(pfx) === 0);
    assert.equal(sub.length, NAMES.length);
    assert.deepEqual(sub.map((c) => String(c.name).slice(pfx.length)).sort(), [...NAMES].sort());
    assert.ok(sub.every((c) => c.webgpu === wg));
  }
});

test('mask-route fixtures: single standard wireframe:false OBJ, real props only', () => {
  for (const c of routeCases()) {
    const n = c.name;
    assert.ok(c.obj, n + ': direct built-in obj');
    assert.equal(c.obj.materialKind, 'standard', n + ' materialKind');
    assert.equal(c.obj.wireframe, false, n + ' wireframe');
    assert.ok(c.obj.vertices, n + ' explicit quad vertices');
    assert.ok(!c.model && !c.instanced, n + ': no imported GLB/instancing');
    assert.equal(c.obj.color, '#3a7bd5', n + ' color');
    assert.equal(c.obj.roughness, 0.35, n + ' roughness');
    assert.equal(c.obj.metalness, 0, n + ' metalness');
    assert.equal(c.obj.ior, 1.5, n + ' ior');
    assert.equal(c.f0, 0.04, n + ' f0');
    assert.equal(c.f90, 1, n + ' f90');
    assert.ok(!('alphaMode' in c.obj), n + ': no alphaMode prop');
    assert.equal(typeof c.expectedDepthWrite, 'boolean', n + ' depthWrite');
    assert.equal(typeof c.expectedBlend, 'boolean', n + ' blend');
    assert.equal(typeof c.expectedOpacity, 'number', n + ' opacity');
  }
});

test('mask-route authored routing table exactly (input vs effective cutoff)', () => {
  for (const pfx of ['gl-maskroute-', 'wg-maskroute-']) {
    for (const nm of NAMES) {
      const c = CASES.find((x) => x.name === pfx + nm);
      assert.ok(c, pfx + nm + ' exists');
      const [op, cut, eff, dw, bl, empty, same, diff, pass] = TABLE[nm];
      assert.equal(c.expectedOpacity, op, pfx + nm + ' opacity');
      assert.equal(c.obj.alphaCutoff, cut, pfx + nm + ' input cutoff');
      assert.equal(c.expectedAlphaCutoff, eff, pfx + nm + ' effective cutoff');
      assert.equal(c.expectedDepthWrite, dw, pfx + nm + ' depthWrite');
      assert.equal(c.expectedBlend, bl, pfx + nm + ' blend');
      assert.equal(c.expectedEmpty === true, empty === true, pfx + nm + ' empty');
      assert.equal(c.same, same !== undefined ? pfx + same : undefined, pfx + nm + ' same');
      assert.equal(c.differs, diff !== undefined ? pfx + diff : undefined, pfx + nm + ' differs');
      assert.equal(c.obj.renderPass, pass, pfx + nm + ' renderPass');
      if (nm === 'mask-disabled') {
        assert.equal(c.obj.alphaCutoff, null, pfx + nm + ' explicit null cutoff');
      }
      if (nm === 'mask-css-cutoff') {
        assert.equal(typeof c.obj.alphaCutoff, 'string', pfx + nm + ' raw CSS string');
        assert.match(c.obj.alphaCutoff, /var\(--mask-cutoff,\s*0\.5\)/);
        assert.equal(c.expectedAlphaCutoff, 0.5, pfx + nm + ' effective numeric');
      }
    }
  }
});

// Extract the COMPLETE PRELOAD declaration (template literal through the
// assertClose marker), evaluate it in a throwaway VM so JS template escapes
// are resolved by the real engine, and use the resulting actual string.
const PRELOAD_DECL_END = SRC.indexOf('function assertClose');
assert.ok(PRELOAD_DECL_END > 0, 'assertClose marker found');
const PRELOAD_DECL = SRC.slice(
  SRC.indexOf('const PRELOAD = `'), PRELOAD_DECL_END);
assert.ok(PRELOAD_DECL.endsWith('\n'), 'PRELOAD declaration complete');
const PRELOAD = vm.runInContext(
  '(function(){' + PRELOAD_DECL + '; return PRELOAD;})()',
  vm.createContext({}));

// Extract the COMPLETE READ declaration (single-quoted concatenation through
// the trailing-comment marker), evaluate it in a throwaway VM with the MOUNT
// string injected, and use the resulting actual string.
const READ_DECL_END = SRC.indexOf('// Decode the actual screenshot');
assert.ok(READ_DECL_END > PRELOAD_DECL_END, 'READ marker after PRELOAD marker');
const READ_DECL = SRC.slice(
  SRC.indexOf("const READ = '"), READ_DECL_END);
assert.ok(READ_DECL.endsWith('\n'), 'READ declaration complete');
const READ = vm.runInContext(
  '(function(){var MOUNT = "mount";' + READ_DECL + '; return READ;})()',
  vm.createContext({}));

test('actual PRELOAD and READ strings compile', () => {
  assert.doesNotThrow(() => new Function(PRELOAD));
  assert.doesNotThrow(() => new Function('return (' + READ + ');'));
});

test('scripted PRELOAD: WebGL depth/blend observed via saved native APIs', () => {
  const window = {};
  function FakeCtx() {}
  const proto = FakeCtx.prototype;
  // The prototype-level getParameter/isEnabled throw: any use of the wrapped
  // (or unsaved) entry points in the observation path would fail loudly.
  proto.getParameter = function () { throw new Error('wrapped getParameter used'); };
  proto.isEnabled = function () { throw new Error('wrapped isEnabled used'); };
  proto.drawElements = function () { this.nativeDraws = (this.nativeDraws || 0) + 1; };
  proto.drawArrays = function () {};
  proto.drawArraysInstanced = function () {};
  proto.drawElementsInstanced = function () {};
  proto.getUniformLocation = function () { return null; };
  proto.getUniform = function () { return null; };
  proto.getProgramParameter = function () { return 0; };
  proto.getActiveUniform = function () { return null; };
  proto.activeTexture = function () {};
  proto.framebufferTexture2D = function () {};
  function FakeGL1() {}
  FakeGL1.prototype = {};
  const ctx = new FakeCtx();
  ctx.CURRENT_PROGRAM = 0x8B8D;
  // 0x0B72 is the real DEPTH_WRITEMASK; 0x0B71 is DEPTH_TEST.
  ctx.DEPTH_WRITEMASK = 0x0B72;
  ctx.BLEND = 0x0BE2;
  const program = { id: 1 };
  const loc = (kind) => ({ kind });
  let depthVal = true;
  let blendVal = false;
  const queriedParams = [];
  ctx.__origGetParameter = function (p) {
    queriedParams.push(p);
    if (p === ctx.CURRENT_PROGRAM) return program;
    if (p === ctx.DEPTH_WRITEMASK) return depthVal;
    throw new Error('unexpected getParameter ' + p);
  };
  ctx.__origIsEnabled = function (cap) {
    return cap === ctx.BLEND ? blendVal : false;
  };
  ctx.__origGetUniform = function (prog, l) {
    if (l.kind === 'f0') return [0.04, 0.04, 0.04];
    if (l.kind === 'f90') return 1;
    if (l.kind === 'op') return 0.5;
    if (l.kind === 'cut') return 0.5;
    return null;
  };
  ctx.__origGetProgramParameter = function () { return 1; };
  ctx.__origGetActiveUniform = function () { return null; };
  ctx.__sf0locs = new Map([[program, loc('f0')]]);
  ctx.__sf90locs = new Map([[program, loc('f90')]]);
  ctx.__oplocs = new Map([[program, loc('op')]]);
  ctx.__aclocs = new Map([[program, loc('cut')]]);
  ctx.__ulocs = new Map();
  ctx.__alblocs = new Map();
  ctx.__sibllocs = new Map();
  const sbx = vm.createContext({
    window, console, location: { pathname: '/case/other' },
    WebGLRenderingContext: FakeGL1, WebGL2RenderingContext: FakeCtx,
  });
  vm.runInContext(PRELOAD, sbx, { filename: 'preload.js' });
  const IOR = window.__gosxIOR;
  assert.ok(IOR, 'preload installed __gosxIOR');

  proto.drawElements.call(ctx, 4, 6, 5125, 0);
  assert.equal(IOR.draws, 1);
  assert.equal(IOR.pbrDraws, 1);
  // VM-realm arrays are not deepStrictEqual against host arrays; convert.
  assert.deepEqual(Array.from(IOR.lastDrawF0), [0.04, 0.04, 0.04]);
  assert.ok(queriedParams.indexOf(0x0B72) >= 0,
    'native DEPTH_WRITEMASK (0x0B72) was queried');
  assert.equal(IOR.lastDrawF90, 1);
  assert.equal(IOR.lastDrawOpacity, 0.5);
  assert.equal(IOR.lastDrawAlphaCutoff, 0.5);
  assert.equal(IOR.lastDrawDepthWrite, true);
  assert.equal(IOR.lastDrawBlend, false);
  assert.equal(ctx.nativeDraws, 1, 'strict forwarding: native draw ran');
  assert.equal(IOR.obsErrors.length, 0);

  // Non-boolean native results stay null; per-draw state is re-read.
  depthVal = 1;
  blendVal = true;
  proto.drawElements.call(ctx, 4, 6, 5125, 0);
  assert.equal(IOR.lastDrawDepthWrite, null);
  assert.equal(IOR.lastDrawBlend, true);
  assert.equal(IOR.draws, 2);

  // Native observation failure is recorded (bounded) and never defaults.
  ctx.__origGetParameter = function (p) {
    if (p === ctx.CURRENT_PROGRAM) return program;
    throw new Error('boom');
  };
  proto.drawElements.call(ctx, 4, 6, 5125, 0);
  assert.equal(IOR.lastDrawDepthWrite, null);
  assert.equal(IOR.lastDrawBlend, true);
  assert.ok(IOR.obsErrors.length >= 1 && IOR.obsErrors.length <= 16);

  // Missing saved isEnabled is recorded as null, never assumed. Assign an
  // own undefined property: deleting would silently fall back to the
  // prototype's saved method.
  ctx.__origIsEnabled = undefined;
  proto.drawElements.call(ctx, 4, 6, 5125, 0);
  assert.equal(IOR.lastDrawBlend, null);
  assert.equal(ctx.nativeDraws, 4, 'native draw always ran despite observation failures');
});

test('scripted PRELOAD: WebGPU PBR qualification and draw-route tracking', async () => {
  const window = {};
  function GPUDevice() {}
  GPUDevice.prototype.createRenderPipeline = function (d) { return { desc: d }; };
  GPUDevice.prototype.createRenderPipelineAsync = function (d) {
    return Promise.resolve({ desc: d });
  };
  function GPURenderPassEncoder() {}
  GPURenderPassEncoder.prototype.setPipeline = function () {};
  GPURenderPassEncoder.prototype.draw = function () {
    if (this.failNative) throw new Error('native draw failed');
    this.nativeDraws = (this.nativeDraws || 0) + 1;
  };
  GPURenderPassEncoder.prototype.drawIndexed = function () {};
  function GPURenderBundleEncoder() {}
  GPURenderBundleEncoder.prototype.setPipeline = function () {};
  GPURenderBundleEncoder.prototype.drawIndexed = function () {
    if (this.failNative) throw new Error('native draw failed');
    this.nativeDraws = (this.nativeDraws || 0) + 1;
  };
  const sbx = vm.createContext({
    window, console, GPUDevice, GPURenderPassEncoder, GPURenderBundleEncoder,
  });
  vm.runInContext(PRELOAD, sbx, { filename: 'preload.js' });
  const D = window.__gosxWGPUDraws;
  assert.ok(D, 'preload installed __gosxWGPUDraws');
  const pbrDesc = (depthWrite, blend) => ({
    label: 'gosx-pbr-' + (blend ? 'alpha' : 'opaque'),
    vertex: { module: {}, entryPoint: 'vertexMain' },
    fragment: { module: {}, entryPoint: 'fragmentMain',
      targets: [blend ? { format: 'rgba8unorm', blend: { color: {}, alpha: {} } }
        : { format: 'rgba8unorm' }] },
    depthStencil: { format: 'depth24plus', depthWriteEnabled: depthWrite,
      depthCompare: 'less-equal' },
  });
  const device = new GPUDevice();
  const enc = new GPURenderPassEncoder();
  const bundle = new GPURenderBundleEncoder();

  enc.setPipeline(device.createRenderPipeline(pbrDesc(true, false)));
  enc.draw(6);
  assert.equal(enc.nativeDraws, 1, 'native draw forwarded');
  assert.equal(D.observed, 1);
  assert.equal(D.lastDepthWrite, true);
  assert.equal(D.lastBlend, false);

  // Non-PBR pipelines never qualify: generic label, no color target.
  const enc2 = new GPURenderPassEncoder();
  enc2.setPipeline(device.createRenderPipeline({
    label: 'generic', vertex: { entryPoint: 'vertexMain' },
    fragment: { entryPoint: 'fragmentMain', targets: [{ format: 'rgba8unorm' }] },
    depthStencil: { depthWriteEnabled: false } }));
  enc2.draw(6);
  enc2.setPipeline(device.createRenderPipeline({
    label: 'gosx-pbr-shadow', vertex: { entryPoint: 'vertexMain' },
    fragment: { entryPoint: 'fragmentMain', targets: [] },
    depthStencil: { depthWriteEnabled: true } }));
  enc2.draw(6);
  assert.equal(D.observed, 1, 'non-PBR draws never counted');

  // Bundle encoders are tracked too; drawIndexed observed after native run.
  bundle.setPipeline(device.createRenderPipeline(pbrDesc(false, true)));
  bundle.drawIndexed(6);
  assert.equal(bundle.nativeDraws, 1);
  assert.equal(D.observed, 2);
  assert.equal(D.lastDepthWrite, false);
  assert.equal(D.lastBlend, true);

  // A failed native draw is never observed as a routed draw.
  const encFail = new GPURenderPassEncoder();
  encFail.setPipeline(device.createRenderPipeline(pbrDesc(true, false)));
  encFail.failNative = true;
  assert.throws(() => encFail.draw(6));
  assert.equal(D.observed, 2);

  // Async creation: the snapshot is taken BEFORE awaiting native creation,
  // so later descriptor mutation is not observed.
  const d = pbrDesc(true, false);
  const pr = device.createRenderPipelineAsync(d);
  d.depthStencil.depthWriteEnabled = false;
  d.fragment.targets[0].blend = { color: {}, alpha: {} };
  const p = await pr;
  enc.setPipeline(p);
  enc.draw(6);
  assert.equal(D.observed, 3);
  assert.equal(D.lastDepthWrite, true, 'pre-await snapshot preserved');
  assert.equal(D.lastBlend, false);
});

test('READ snapshot exposes routing state without raw GPU handles', () => {
  const el = { getAttribute: () => null };
  const sbx = vm.createContext({
    document: { getElementById: () => el },
    window: {
      __gosxIOR: { draws: 2, pbrDraws: 2, lastDrawF0: [0.04, 0.04, 0.04],
        lastDrawF90: 1, gl: 'webgl2', lastDrawOpacity: 0.5,
        lastDrawAlphaCutoff: 0.5, lastDrawDepthWrite: true, lastDrawBlend: false,
        lastDrawHasIBL: null, lastDrawHasSpecIntensityMap: null,
        lastDrawHasSpecColorMap: null, lastDrawHasAlbedoMap: null,
        lastDrawUnlit: null, programInfo: null, queriedUniforms: [],
        shadow: null, nativeCap: null, forcedCap: null, obsErrors: [] },
      __gosxWGPU: { materialUploads: 1, dumps: [], obsErrors: [] },
      __gosxWGPUDraws: { observed: 3, lastDepthWrite: false, lastBlend: true },
      __gosxWGPUReadEnvWords: () => null,
    },
  });
  const s = vm.runInContext('(' + READ + ')', sbx);
  assert.equal(s.ior.lastDrawDepthWrite, true);
  assert.equal(s.ior.lastDrawBlend, false);
  // VM-realm objects are not deepStrictEqual against host objects; unwrap
  // through JSON so values (and key sets) are still strictly checked.
  assert.deepEqual(JSON.parse(JSON.stringify(s.wgpu.wgdraws)),
    { observed: 3, lastDepthWrite: false, lastBlend: true });
  assert.deepEqual(Object.keys(s.wgpu.wgdraws).sort(),
    ['lastBlend', 'lastDepthWrite', 'observed']);
});

test('scripted PRELOAD: WebGPU env writeBuffer snapshot (68/80, offsets, SH tail, forwarding)', () => {
  const window = {};
  function GPUBuffer() {}
  function GPUTextureView() {}
  function GPUSampler() {}
  function GPUQueue() {}
  function GPUDevice() {}
  const nativeWriteCalls = [];
  const nativeResult = { marker: true };
  GPUQueue.prototype.writeBuffer = function () {
    nativeWriteCalls.push({ thisArg: this, args: Array.from(arguments) });
    return nativeResult;
  };
  GPUDevice.prototype.createBindGroup = function () { return { native: true }; };
  function GPURenderPassEncoder() {}
  GPURenderPassEncoder.prototype.setBindGroup = function () {};
  function GPURenderBundleEncoder() {}
  GPURenderBundleEncoder.prototype.setBindGroup = function () {};
  const sbx = vm.createContext({
    window, console, GPUQueue, GPUDevice, GPUBuffer, GPUTextureView, GPUSampler,
    GPURenderPassEncoder, GPURenderBundleEncoder,
  });
  vm.runInContext(PRELOAD, sbx, { filename: 'preload.js' });
  const W = window.__gosxWGPU;
  assert.ok(W, 'preload installed __gosxWGPU');
  assert.equal(typeof window.__gosxWGPUReadEnvWords, 'function');
  const readEnv = () => JSON.parse(JSON.stringify(window.__gosxWGPUReadEnvWords()));

  // Real binding-3 environment buffer, bound through a production-shaped
  // 15-entry frame bind group and setBindGroup(0, group).
  const envBuffer = new GPUBuffer();
  const otherBuffer = new GPUBuffer();
  const views = [new GPUTextureView(), new GPUTextureView(), new GPUTextureView()];
  const sampler = new GPUSampler();
  const entries = [];
  for (let i = 0; i < 15; i += 1) {
    if (i === 3) entries.push({ binding: i, resource: { buffer: envBuffer } });
    else if (i >= 9 && i <= 11) entries.push({ binding: i, resource: views[i - 9] });
    else if (i === 12) entries.push({ binding: i, resource: sampler });
    else entries.push({ binding: i, resource: { buffer: new GPUBuffer() } });
  }
  const device = new GPUDevice();
  const queue = new GPUQueue();
  const group = device.createBindGroup({ entries });
  new GPURenderPassEncoder().setBindGroup(0, group);
  assert.equal(readEnv(), null, 'no env words before any write');

  // Current production shape: exact 68-byte prefix (words 0..16) at
  // bufferOffset 0.
  const f = new Float32Array(17);
  const u = new Uint32Array(f.buffer);
  u[14] = 1; u[15] = 6; u[16] = 1;
  assert.equal(queue.writeBuffer(envBuffer, 0, f), nativeResult,
    'exact result forwarded through the wrapper');
  assert.deepEqual(readEnv(), { hasIBL: 1, mips: 6, hasEnvMap: 1 });

  // Legacy shape: full 80-byte prefix at bufferOffset 0.
  const f20 = new Float32Array(20);
  const u20 = new Uint32Array(f20.buffer);
  u20[14] = 0; u20[15] = 0; u20[16] = 1;
  queue.writeBuffer(envBuffer, 0, f20);
  assert.deepEqual(readEnv(), { hasIBL: 0, mips: 0, hasEnvMap: 1 });

  // Nonzero dataOffset/size: elements 4..20 of a larger source, still a
  // 68-byte prefix at bufferOffset 0.
  const big = new Float32Array(64);
  const bigU = new Uint32Array(big.buffer);
  bigU[4 + 14] = 1; bigU[4 + 15] = 3; bigU[4 + 16] = 0;
  queue.writeBuffer(envBuffer, 0, big, 4, 17);
  assert.deepEqual(readEnv(), { hasIBL: 1, mips: 3, hasEnvMap: 0 });

  // Nonzero view byteOffset (subarray) honors base+byteOff bounds.
  bigU[8 + 14] = 0; bigU[8 + 15] = 5; bigU[8 + 16] = 1;
  const sub = big.subarray(8, 25);
  queue.writeBuffer(envBuffer, 0, sub);
  assert.deepEqual(readEnv(), { hasIBL: 0, mips: 5, hasEnvMap: 1 });

  // Truncated write (64 bytes at offset 0) is never snapshotted.
  const short = new Float32Array(16);
  new Uint32Array(short.buffer)[14] = 7;
  queue.writeBuffer(envBuffer, 0, short);
  assert.deepEqual(readEnv(), { hasIBL: 0, mips: 5, hasEnvMap: 1 });

  // SH tail (uploadLights writes bufferOffset 68, 39 elements) never
  // overrides the env words, even with a 68-byte payload at offset 68.
  const tail = new Float32Array(39);
  new Uint32Array(tail.buffer)[0] = 1;
  queue.writeBuffer(envBuffer, 68, tail, 0, 39);
  queue.writeBuffer(envBuffer, 68, f);
  assert.deepEqual(readEnv(), { hasIBL: 0, mips: 5, hasEnvMap: 1 });

  // Unrelated buffers never affect the bound env buffer.
  queue.writeBuffer(otherBuffer, 0, f);
  assert.deepEqual(readEnv(), { hasIBL: 0, mips: 5, hasEnvMap: 1 });

  // Subsequent real env update is visible on the bound buffer.
  u[14] = 1; u[15] = 2; u[16] = 0;
  queue.writeBuffer(envBuffer, 0, f);
  assert.deepEqual(readEnv(), { hasIBL: 1, mips: 2, hasEnvMap: 0 });

  // Exact native arguments and this forwarding; only the 9 real writes ran.
  assert.equal(nativeWriteCalls.length, 9);
  assert.ok(nativeWriteCalls.every((c) => c.thisArg === queue));
  const a = nativeWriteCalls;
  assert.ok(a[0].args.length === 3 && a[0].args[0] === envBuffer &&
    a[0].args[1] === 0 && a[0].args[2] === f);
  assert.ok(a[2].args.length === 5 && a[2].args[0] === envBuffer &&
    a[2].args[1] === 0 && a[2].args[2] === big &&
    a[2].args[3] === 4 && a[2].args[4] === 17);
  assert.ok(a[5].args.length === 5 && a[5].args[1] === 68 &&
    a[5].args[3] === 0 && a[5].args[4] === 39);
  assert.ok(a[6].args.length === 3 && a[6].args[1] === 68 && a[6].args[2] === f);
  assert.ok(a[7].args.length === 3 && a[7].args[0] === otherBuffer);
  assert.ok(a[8].args.length === 3 && a[8].args[0] === envBuffer && a[8].args[2] === f);
  assert.equal(W.dumps.length, 0);
  assert.equal(W.materialUploads, 0);
  assert.equal(W.obsErrors.length, 0);
});
