'use strict';
// Regression coverage for the instanced-lifecycle slice of
// client/js/testdata/scene3d-material-ior-browser.cjs.
//
// SCOPE: this file exercises PROBE LOGIC ONLY — the Go fixture contract, the
// case-registration helper, the case-group filter, the GL instanced-shadow
// observer, the read-only state expression and the commands dispatch helper —
// using the real emitted fixture and minimal fakes inside a VM. It does NOT
// execute the browser probe, Chrome, WebGL/WebGPU or any rendering, and it
// asserts nothing about pixels. Real native multi-instance rendering evidence
// is produced only by the browser probe itself; these tests keep the probe
// source honest (registration order and refs, observer last-count semantics,
// fail-closed reads, dispatch acknowledgment and cleanup).
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const ROOT = path.join(__dirname, '..', '..');
const SRC = fs.readFileSync(path.join(ROOT, 'client', 'js', 'testdata',
  'scene3d-material-ior-browser.cjs'), 'utf8');

function slice(startMarker, endMarker) {
  const start = SRC.indexOf(startMarker);
  assert.ok(start >= 0, 'start marker present: ' + JSON.stringify(startMarker));
  const end = SRC.indexOf(endMarker, start);
  assert.ok(end > start, 'end marker after start: ' + JSON.stringify(endMarker));
  return SRC.slice(start, end);
}

let FIXTURE = null;
// Runs the real Go contract test for the fixture package (so main_test.go is
// covered by make test-js/CI) and then emits the real fixture via `go run`.
// Both calls are bounded by explicit timeouts.
function loadFixture() {
  if (FIXTURE) return FIXTURE;
  execFileSync('go', ['test', './client/js/testdata/instanced-shadow-typed-fixture'],
    { cwd: ROOT, timeout: 120000, stdio: 'pipe' });
  const out = execFileSync('go', ['run', './client/js/testdata/instanced-shadow-typed-fixture'],
    { cwd: ROOT, timeout: 120000, stdio: ['ignore', 'pipe', 'inherit'], encoding: 'utf8' });
  FIXTURE = JSON.parse(out);
  return FIXTURE;
}

// ---- Registration helper + case-group filter (real source slices) ----

function runRegistration(fixture, env, exits) {
  const markers = [
    'function registerInstancedLifecycleCases(',
    'registerInstancedLifecycleCases(INST_FIXTURE);',
    'CASES.forEach((c) => { byName[c.name] = c; });',
    'const CASE_GROUP_ENV = process.env.GOSX_IOR_CASE_GROUP;',
    'if (!Array.isArray(RUN_CASES) || RUN_CASES.length === 0) {',
  ];
  let at = 0;
  for (const m of markers) {
    const i = SRC.indexOf(m, at);
    assert.ok(i >= at, 'marker present and in order: ' + m);
    at = i;
  }
  const body = slice(markers[0], 'function propsFor(');
  const exitCodes = exits || [];
  const sandbox = vm.createContext({
    console,
    INST_FIXTURE: fixture,
    process: {
      env: env,
      exit(code) { exitCodes.push(code); throw new Error('probe-exit:' + code); },
    },
  });
  vm.runInContext('var CASES = []; var byName = {};', sandbox);
  const ret = vm.runInContext(body +
    '\n;({ CASES: CASES, byName: byName, RUN_CASES: RUN_CASES, CASE_GROUP: CASE_GROUP });',
    sandbox);
  return { CASES: ret.CASES, byName: ret.byName, RUN_CASES: ret.RUN_CASES,
    CASE_GROUP: ret.CASE_GROUP, exits: exitCodes };
}

const STATIC_COUNTS = { empty: 0, 'reference-left': 0, 'reference-right': 0,
  both: 2, left: 2, right: 2, 'moved-opaque': 2, moved: 2, one: 1 };
const STATIC_ORDER = ['empty', 'reference-left', 'reference-right', 'both',
  'left', 'right', 'moved-opaque', 'moved', 'one'];
// Exact per-scene reference mappings (constant, not conditional checks that
// would also pass if the references were deleted).
const SAME_REFS = { left: 'reference-left', one: 'reference-left',
  right: 'reference-right' };
const DIFFERS_REFS = { 'reference-left': 'empty', 'reference-right': 'empty',
  both: 'empty', moved: 'right' };
const SAME_FULL_REFS = { moved: 'moved-opaque' };
// Constant receiver-only ROI.
const CONSTANT_ROI = { x: 125, y: 99, width: 95, height: 35 };

test('registration helper registers exactly 20 unique ordered cases with valid refs', () => {
  const fx = loadFixture();
  const r = runRegistration(fx, {});
  assert.strictEqual(r.CASE_GROUP, 'all');
  assert.strictEqual(r.RUN_CASES, r.CASES);
  assert.strictEqual(r.CASES.length, 20);
  const names = r.CASES.map((c) => c.name);
  assert.strictEqual(new Set(names).size, 20, 'case names unique');
  // Exact new reference order per backend: nine static scenes (moved-opaque
  // before moved) then the lifecycle case.
  const EXPECTED_ORDER = ['empty', 'reference-left', 'reference-right', 'both',
    'left', 'right', 'moved-opaque', 'moved', 'one', 'lifecycle'];
  for (const webgpu of [false, true]) {
    const pfx = webgpu ? 'wg-instlife-' : 'gl-instlife-';
    const base = webgpu ? 10 : 0;
    EXPECTED_ORDER.forEach((scene, i) => {
      const c = r.CASES[base + i];
      assert.strictEqual(c.name, pfx + scene);
      assert.strictEqual(c.webgpu, webgpu);
      assert.strictEqual(c.instancedLifecycleGroup, true);
      assert.strictEqual(c.expectedInstancedCount,
        scene === 'lifecycle' ? 2 : STATIC_COUNTS[scene]);
      assert.strictEqual(r.byName[c.name], c, 'byName entry registered');
      if (scene !== 'lifecycle') {
        // Realm-neutral comparison: assert fields individually so the VM
        // object's cross-realm prototype does not affect the check.
        assert.strictEqual(c.shadowROI.x, CONSTANT_ROI.x,
          scene + ' shadowROI.x uses the constant receiver ROI');
        assert.strictEqual(c.shadowROI.y, CONSTANT_ROI.y,
          scene + ' shadowROI.y uses the constant receiver ROI');
        assert.strictEqual(c.shadowROI.width, CONSTANT_ROI.width,
          scene + ' shadowROI.width uses the constant receiver ROI');
        assert.strictEqual(c.shadowROI.height, CONSTANT_ROI.height,
          scene + ' shadowROI.height uses the constant receiver ROI');
      }
    });
    const life = r.CASES[base + 9];
    assert.strictEqual(life.name, pfx + 'lifecycle');
    assert.strictEqual(life.lifecycle, true);
    assert.strictEqual(life.lifecycleTransitions, fx.transitions,
      'live transitions come verbatim from the emitted fixture');
    assert.strictEqual(life.expectedInstancedCount, 2);
    // Exact same/differs/sameFull mappings per static scene.
    for (const scene of STATIC_ORDER) {
      const c = r.byName[pfx + scene];
      assert.ok(c, 'static case registered: ' + pfx + scene);
      assert.strictEqual(c.same,
        SAME_REFS[scene] === undefined ? undefined : pfx + SAME_REFS[scene],
        scene + ' exact same ref');
      assert.strictEqual(c.differs,
        DIFFERS_REFS[scene] === undefined ? undefined : pfx + DIFFERS_REFS[scene],
        scene + ' exact differs ref');
      assert.strictEqual(c.sameFull,
        SAME_FULL_REFS[scene] === undefined ? undefined : pfx + SAME_FULL_REFS[scene],
        scene + ' exact sameFull ref');
      if (c.differs) assert.strictEqual(c.minChanged, 50);
    }
    // References must be captured before their consumers by CASES index,
    // not merely present in byName.
    const idx = (n) => r.CASES.findIndex((x) => x.name === pfx + n);
    for (const [consumer, ref] of [['left', 'reference-left'],
      ['one', 'reference-left'], ['right', 'reference-right'],
      ['moved', 'right'], ['moved', 'moved-opaque'],
      ['reference-left', 'empty'], ['reference-right', 'empty'],
      ['both', 'empty']]) {
      assert.ok(idx(ref) >= 0 && idx(consumer) > idx(ref),
        pfx + ref + ' captured before ' + pfx + consumer);
    }
  }
});

test('case-group filter: default all, focused 20 with refs intact, unknown rejected', () => {
  const fx = loadFixture();
  const all = runRegistration(fx, {});
  assert.strictEqual(all.RUN_CASES, all.CASES);
  assert.deepStrictEqual(all.exits, []);

  const emptyEnv = runRegistration(fx, { GOSX_IOR_CASE_GROUP: '' });
  assert.strictEqual(emptyEnv.RUN_CASES, emptyEnv.CASES);
  assert.deepStrictEqual(emptyEnv.exits, []);

  const focused = runRegistration(fx, { GOSX_IOR_CASE_GROUP: 'instanced-lifecycle' });
  assert.notStrictEqual(focused.RUN_CASES, focused.CASES);
  assert.strictEqual(focused.RUN_CASES.length, 20);
  assert.ok(focused.RUN_CASES.every((c) => c.instancedLifecycleGroup === true));
  // A filtered group can never silently omit a same/differs reference target.
  const selected = new Set(focused.RUN_CASES.map((c) => c.name));
  for (const c of focused.RUN_CASES) {
    if (c.same) assert.ok(selected.has(c.same), 'same ref selected: ' + c.same);
    if (c.differs) assert.ok(selected.has(c.differs), 'differs ref selected: ' + c.differs);
  }

  const exits = [];
  assert.throws(() => runRegistration(fx, { GOSX_IOR_CASE_GROUP: 'bogus' }, exits),
    /probe-exit:2/);
  assert.deepStrictEqual(exits, [2]);
});

// ---- observeInstShadow (real function slice, fake native context) ----

let observeFn = null;
let observeSandbox = null;
function getObserve() {
  if (observeFn) return observeFn;
  observeSandbox = vm.createContext({ console });
  observeFn = vm.runInContext([
    'var window = {};',
    'function noteErr(arr, e) { arr.push(String((e && e.message) || e)); }',
    slice('  function observeInstShadow', 'if (W1) {'),
    ';observeInstShadow;',
  ].join('\n'), observeSandbox);
  return observeFn;
}

function fakeGL(opts) {
  opts = opts || {};
  const ctx = { CURRENT_PROGRAM: 7 };
  ctx.__origGetParameter = function () {
    if (opts.throwOnGetParameter) throw new Error('boom-getParameter');
    return opts.program === undefined ? this.CURRENT_PROGRAM : opts.program;
  };
  ctx.__lvlocs = { has: () => opts.lvHas !== false };
  ctx.__origGetAttribLocation = () => (opts.attrib === undefined ? 3 : opts.attrib);
  return ctx;
}

function freshIOR() {
  return { instShadowDraws: 0, lastInstShadowInstances: 0, obsErrors: [] };
}

test('observeInstShadow records the LAST qualified draw count, not the max', () => {
  const observe = getObserve();
  const ior = freshIOR();
  observeSandbox.window.__gosxIOR = ior;
  observe.call(fakeGL({}), 2);
  assert.strictEqual(ior.instShadowDraws, 1);
  assert.strictEqual(ior.lastInstShadowInstances, 2, 'after draw of 2');
  observe.call(fakeGL({}), 1);
  assert.strictEqual(ior.instShadowDraws, 2);
  assert.strictEqual(ior.lastInstShadowInstances, 1, 'after draw of 1');
  observe.call(fakeGL({}), 0);
  assert.strictEqual(ior.instShadowDraws, 3);
  assert.strictEqual(ior.lastInstShadowInstances, 0,
    'the most recent qualified draw (0) wins over the earlier max (2)');
  const ior2 = freshIOR();
  observeSandbox.window.__gosxIOR = ior2;
  observe.call(fakeGL({}), undefined);
  assert.strictEqual(ior2.instShadowDraws, 1);
  assert.strictEqual(ior2.lastInstShadowInstances, 0,
    'non-numeric counts normalize to 0');
});

test('observeInstShadow ignores unqualified programs/attribs and captures errors', () => {
  const observe = getObserve();
  const ior = freshIOR();
  observeSandbox.window.__gosxIOR = ior;
  observe.call(fakeGL({ program: null }), 2);
  observe.call(fakeGL({ lvHas: false }), 2);
  observe.call(fakeGL({ attrib: -1 }), 2);
  assert.strictEqual(ior.instShadowDraws, 0);
  assert.strictEqual(ior.lastInstShadowInstances, 0);
  assert.deepStrictEqual(ior.obsErrors, []);
  const iorErr = freshIOR();
  observeSandbox.window.__gosxIOR = iorErr;
  assert.doesNotThrow(() => observe.call(fakeGL({ throwOnGetParameter: true }), 2),
    'observation errors never break the native draw path');
  assert.strictEqual(iorErr.instShadowDraws, 0);
  assert.strictEqual(iorErr.obsErrors.length, 1);
  assert.match(iorErr.obsErrors[0], /boom-getParameter/);
});

// ---- INSTLIFE_STATE_READ (real expression, read-only fake mount) ----

let stateReadExpr = null;
function getStateRead() {
  if (stateReadExpr) return stateReadExpr;
  const sandbox = vm.createContext({ MOUNT: 'probe-mount' });
  stateReadExpr = vm.runInContext(
    slice('const INSTLIFE_STATE_READ', 'function instlifeDispatchExpr') +
    ';INSTLIFE_STATE_READ;', sandbox);
  return stateReadExpr;
}

function runStateRead(state) {
  const doc = {
    getElementById: (id) => (id === 'probe-mount'
      ? { __gosxScene3DState: state } : null),
  };
  return vm.runInContext(getStateRead(), vm.createContext({ document: doc }));
}

test('INSTLIFE_STATE_READ: exact counts/batches, read-only copies, fail-closed', () => {
  const state = { instancedMeshes: [{ count: 2,
    transforms: Array.from({ length: 32 }, (_, i) => i / 16),
    colors: ['rgba(1,2,3,0.5)', 'rgba(1,2,3,0.25)'] }] };
  const st = runStateRead(state);
  assert.ok(st, 'well-formed state reads successfully');
  assert.strictEqual(st.batches, 1);
  assert.strictEqual(st.instances, 2);
  assert.strictEqual(st.entries[0].count, 2);
  assert.deepStrictEqual(st.entries[0].transforms, state.instancedMeshes[0].transforms);
  assert.deepStrictEqual(st.entries[0].colors, state.instancedMeshes[0].colors);
  assert.notStrictEqual(st.entries[0].transforms, state.instancedMeshes[0].transforms,
    'transforms are copies; the production state object is never handed out');
  st.entries[0].transforms.push(99);
  assert.strictEqual(state.instancedMeshes[0].transforms.length, 32,
    'mutating the read result never touches production state');

  const empty = runStateRead({ instancedMeshes: [] });
  // Realm-neutral comparison: the VM result's entries array is a VM-array
  // even when spread, so assert fields and length instead of deepStrictEqual
  // against a host array.
  assert.strictEqual(empty.batches, 0, 'empty state has zero batches');
  assert.strictEqual(empty.instances, 0, 'empty state has zero instances');
  assert.strictEqual(Array.isArray(empty.entries), true,
    'empty state entries is an array');
  assert.strictEqual(empty.entries.length, 0, 'empty state entries is empty');

  assert.strictEqual(runStateRead({ instancedMeshes: [{ count: '2' }] }), null,
    'non-numeric count fails closed');
  assert.strictEqual(runStateRead({ instancedMeshes: [{ count: -1 }] }), null);
  assert.strictEqual(runStateRead({ instancedMeshes: [{}] }), null,
    'missing count fails closed');
  assert.strictEqual(
    runStateRead({ instancedMeshes: [{ count: 1, transforms: 'no' }] }), null,
    'non-array transforms fail closed');
  assert.strictEqual(runStateRead({}), null, 'missing instancedMeshes fails closed');
  assert.strictEqual(runStateRead(null), null, 'missing state fails closed');
});

// ---- instlifeDispatchExpr (real function, fake event target + timers) ----

function getDispatch(waitMs) {
  const sandbox = vm.createContext({ console });
  return vm.runInContext(
    'var MOUNT = "probe-mount"; var CASE_WAIT_MS = ' + Number(waitMs) + ';\n' +
    slice('function instlifeDispatchExpr', 'async function runInstancedLifecycleSteps') +
    ';instlifeDispatchExpr;', sandbox);
}

function makeTimers() {
  const t = { created: 0, cleared: 0 };
  t.setTimeout = (fn, ms) => { t.created += 1; return setTimeout(fn, ms); };
  t.clearTimeout = (id) => { t.cleared += 1; return clearTimeout(id); };
  return t;
}

function makeTarget(opts) {
  opts = opts || {};
  const listeners = {};
  const target = {
    removed: [], commandsDetail: null, ackDetail: null,
    addEventListener(type, h) { (listeners[type] = listeners[type] || []).push(h); },
    removeEventListener(type, h) {
      const a = listeners[type] || [];
      const i = a.indexOf(h);
      if (i >= 0) { a.splice(i, 1); target.removed.push(type); }
    },
    dispatchEvent(ev) {
      if (ev.type !== 'gosx:scene3d:commands') return true;
      if (opts.throwOnDispatch) throw new Error('dispatch-boom');
      target.commandsDetail = ev.detail;
      const cmds = ev.detail.commands || [];
      target.ackDetail = opts.ackDetail
        ? opts.ackDetail(ev.detail, cmds)
        : { revision: ev.detail.revision, commandCount: cmds.length };
      // Production contract: after applying commands the mount emits the
      // applied event carrying {revision, commandCount}.
      const applied = { type: 'gosx:scene3d:commands-applied', detail: target.ackDetail };
      (listeners['gosx:scene3d:commands-applied'] || []).slice().forEach((h) => h(applied));
      return true;
    },
    listenerCount(type) { return (listeners[type] || []).length; },
  };
  return target;
}

function evalDispatch(expr, target, timers) {
  const ctx = vm.createContext({
    document: { getElementById: () => target },
    CustomEvent: function CustomEvent(type, opts) {
      this.type = type;
      this.detail = opts && opts.detail;
    },
    setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout,
    JSON, Promise, Number,
  });
  return vm.runInContext(expr, ctx);
}

test('instlifeDispatch: exact revision/count ack resolves true with full cleanup', async () => {
  const dispatch = getDispatch(400);
  const target = makeTarget();
  const timers = makeTimers();
  const p = evalDispatch(dispatch([{ kind: 8 }], 3, 1), target, timers);
  assert.strictEqual(await p, true);
  assert.strictEqual(target.commandsDetail.revision, 3);
  assert.deepStrictEqual(target.commandsDetail.commands, [{ kind: 8 }]);
  assert.strictEqual(target.listenerCount('gosx:scene3d:commands-applied'), 0,
    'applied listener removed after success');
  assert.ok(target.removed.includes('gosx:scene3d:commands-applied'));
  assert.strictEqual(timers.created, 1);
  assert.strictEqual(timers.cleared, 1, 'wait timer cleared on success');
});

test('instlifeDispatch: wrong revision/count/string acks ignored until timeout', async () => {
  const dispatch = getDispatch(30);
  const acks = [
    (d, c) => ({ revision: d.revision + 1, commandCount: c.length }),
    (d, c) => ({ revision: d.revision, commandCount: c.length + 1 }),
    (d, c) => ({ revision: String(d.revision), commandCount: c.length }),
    (d, c) => ({ revision: d.revision, commandCount: String(c.length) }),
  ];
  for (const ack of acks) {
    const target = makeTarget({ ackDetail: ack });
    const timers = makeTimers();
    const p = evalDispatch(dispatch([{ kind: 8 }], 5, 1), target, timers);
    assert.strictEqual(await p, false, 'mismatched ack must wait out the timeout');
    assert.strictEqual(target.listenerCount('gosx:scene3d:commands-applied'), 0,
      'listener removed after timeout');
    assert.strictEqual(timers.cleared, 1);
  }
});

test('instlifeDispatch: dispatch throw fails fast with cleanup', async () => {
  const dispatch = getDispatch(3000);
  const target = makeTarget({ throwOnDispatch: true });
  const timers = makeTimers();
  const p = evalDispatch(dispatch([{ kind: 8 }], 1, 1), target, timers);
  assert.strictEqual(await p, false, 'dispatch failure resolves false immediately');
  assert.strictEqual(target.listenerCount('gosx:scene3d:commands-applied'), 0,
    'listener removed on dispatch failure');
  assert.strictEqual(timers.created, 1);
  assert.strictEqual(timers.cleared, 1, 'timer cleared synchronously on failure');
});

// ---- propsFor (real function slice): typed fixture arrays verbatim ----

function getPropsFor() {
  const sandbox = vm.createContext({ console });
  // A closing-brace marker stops at the first inner block end and yields
  // "Unexpected end of input"; slice through the NEXT top-level function
  // marker so the whole propsFor function is included.
  const start = SRC.indexOf('function propsFor(');
  assert.ok(start >= 0, 'propsFor present in source');
  const next = SRC.indexOf('\nfunction ', start + 1);
  assert.ok(next > start, 'next top-level function marker found after propsFor');
  const body = SRC.slice(start, next);
  return vm.runInContext('var W = 640; var H = 360;\n' + body + ';propsFor;', sandbox);
}

test('propsFor passes typed fixture scene arrays through VERBATIM', () => {
  const fx = loadFixture();
  const propsFor = getPropsFor();
  const gl = propsFor({
    webgpu: false, instancedLifecycleScene: 'one', expectedInstancedCount: 1,
    typedInstancedScene: fx.scenes.one,
  });
  assert.strictEqual(gl.forceWebGL, true);
  assert.strictEqual(gl.requireWebGL, true);
  assert.strictEqual(gl.preferWebGPU, false);
  assert.strictEqual(gl.instancedMeshes, fx.scenes.one.instancedMeshes,
    'instancedMeshes is the fixture array by reference');
  assert.deepStrictEqual(gl.objects, fx.scenes.one.objects);
  assert.deepStrictEqual(gl.lights, fx.scenes.one.lights);
  const wg = propsFor({
    webgpu: true, instancedLifecycleScene: 'both', expectedInstancedCount: 2,
    typedInstancedScene: fx.scenes.both,
  });
  assert.strictEqual(wg.forceWebGL, false);
  assert.strictEqual(wg.preferWebGPU, true);
  assert.strictEqual(wg.instancedMeshes, fx.scenes.both.instancedMeshes);
  assert.deepStrictEqual(wg.instancedMeshes, fx.scenes.both.instancedMeshes,
    'nested transforms/rotations/scales/colors carried through unchanged');
});

// ---- Emitted Go fixture contract (real go test + go run output) ----

test('typed instanced fixture passes its Go contract test and emits the expected JSON', () => {
  const fx = loadFixture();
  assert.strictEqual(fx.schema, 'gosx.instanced-shadow.fixture.v1');
  assert.deepStrictEqual(Object.keys(fx.scenes).sort(),
    ['both', 'empty', 'left', 'moved', 'moved-opaque', 'one',
      'reference-left', 'reference-right', 'right']);
  const chain = [['both', 'left'], ['left', 'right'], ['right', 'moved'],
    ['moved', 'one'], ['one', 'empty'], ['empty', 'both']];
  assert.strictEqual(fx.transitions.length, 6);
  chain.forEach(([from, to], i) => {
    const tr = fx.transitions[i];
    assert.strictEqual(tr.name, from + '-to-' + to);
    assert.strictEqual(tr.from, from);
    assert.strictEqual(tr.to, to);
    assert.strictEqual(Array.isArray(tr.commands), true);
    assert.strictEqual(tr.commands.length, 1,
      'exactly one strict kind-8 command per transition');
    assert.strictEqual(tr.commands[0].kind, 8);
  });
  const counts = { both: 2, left: 2, right: 2, 'moved-opaque': 2, moved: 2,
    one: 1, empty: 0 };
  for (const [scene, n] of Object.entries(counts)) {
    const im = fx.scenes[scene].instancedMeshes || [];
    assert.strictEqual(im.length, n > 0 ? 1 : 0, scene + ' batch count');
    if (n > 0) {
      assert.strictEqual(im[0].count, n, scene + ' instance count');
      assert.strictEqual(im[0].transforms.length, 16 * n, scene + ' transforms size');
      assert.strictEqual(im[0].colors.length, n, scene + ' colors size');
    }
  }
  for (const scene of ['reference-left', 'reference-right']) {
    assert.strictEqual((fx.scenes[scene].instancedMeshes || []).length, 0,
      scene + ' has no instanced meshes (unmasked reference path)');
  }

  // moved-opaque is the independent opaque CONTROL for moved: identical
  // typed transforms and material fields, instance colors at alpha 1, and
  // alphaCutoff OMITTED. It is not an oracle for new runtime output.
  const moved = (fx.scenes.moved.instancedMeshes || [])[0];
  const opaque = (fx.scenes['moved-opaque'].instancedMeshes || [])[0];
  assert.deepStrictEqual(opaque.transforms, moved.transforms,
    'moved-opaque typed transforms exactly equal moved');
  const strip = (im) => {
    const copy = { ...im };
    delete copy.colors;
    delete copy.alphaCutoff;
    return copy;
  };
  assert.deepStrictEqual(strip(opaque), strip(moved),
    'moved-opaque equals moved except instance colors and alphaCutoff');
  for (const c of opaque.colors) {
    assert.strictEqual(c, 'rgba(32,64,192,1)', 'instance color alpha is 1');
  }
  assert.strictEqual(opaque.alphaCutoff, undefined, 'alphaCutoff omitted');
  assert.strictEqual(moved.alphaCutoff, 0.5, 'moved keeps its 0.5 cutoff');
});

// ---- Shader-string regression: flat instanceColor in the real WebGL PBR
// sources (evaluated, not duplicated) ----

const WEBGL_TS = fs.readFileSync(path.join(ROOT, 'client', 'runtime', 'scene3d',
  'webgl.ts'), 'utf8');

function evalShaderConst(name) {
  const start = WEBGL_TS.indexOf('const ' + name + ' = [');
  assert.ok(start >= 0, name + ' declaration present in webgl.ts');
  const endMarker = '].join("\\n");';
  const end = WEBGL_TS.indexOf(endMarker, start);
  assert.ok(end > start, name + ' array terminated');
  return vm.runInContext(
    WEBGL_TS.slice(start, end + endMarker.length) + '\n;' + name + ';',
    vm.createContext({}));
}

test('WebGL PBR shaders: flat instanceColor across all producers and consumer', () => {
  const staticVS = evalShaderConst('SCENE_PBR_VERTEX_SOURCE');
  const instancedVS = evalShaderConst('SCENE_PBR_INSTANCED_VERTEX_SOURCE');
  const skinnedVS = evalShaderConst('SCENE_PBR_SKINNED_VERTEX_SOURCE');
  const frag = evalShaderConst('SCENE_PBR_FRAGMENT_SOURCE');

  for (const [name, vs] of [['static', staticVS], ['instanced', instancedVS],
      ['skinned', skinnedVS]]) {
    assert.strictEqual((vs.match(/out vec4 v_instanceColor;/g) || []).length, 1,
      name + ' vertex declares v_instanceColor exactly once');
    assert.ok(vs.includes('flat out vec4 v_instanceColor;'),
      name + ' vertex uses flat out v_instanceColor');
    assert.ok(!vs.includes('smooth out vec4 v_instanceColor;'),
      name + ' vertex must not use smooth instanceColor');
    // UV stays smooth; world position and normal stay smooth too.
    assert.ok(vs.includes('out vec2 v_uv;') && !vs.includes('flat out vec2 v_uv;'),
      name + ' vertex keeps smooth v_uv');
    assert.ok(vs.includes('out vec3 v_worldPosition;') &&
      !vs.includes('flat out vec3 v_worldPosition;'),
      name + ' vertex keeps smooth v_worldPosition');
    assert.ok(vs.includes('out vec3 v_normal;') &&
      !vs.includes('flat out vec3 v_normal;'),
      name + ' vertex keeps smooth v_normal');
  }
  assert.strictEqual((frag.match(/in vec4 v_instanceColor;/g) || []).length, 1,
    'fragment declares v_instanceColor exactly once');
  assert.ok(frag.includes('flat in vec4 v_instanceColor;'),
    'fragment uses flat in v_instanceColor');
  assert.ok(!frag.includes('smooth in vec4 v_instanceColor;'),
    'fragment must not use smooth instanceColor');
  assert.ok(frag.includes('in vec2 v_uv;') && !frag.includes('flat in vec2 v_uv;'),
    'fragment keeps smooth v_uv');
  // Strict cutoff predicate is preserved verbatim.
  assert.match(frag, /if \(masked && coverage < u_alphaCutoff\) \{/);
  assert.ok(frag.includes('discard;'), 'cutoff discard preserved');
});
