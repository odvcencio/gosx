import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import test from 'node:test';

const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8');
const fixture = JSON.parse(read('../../scene/testdata/particle_burst.json'));
const clone = value => JSON.parse(JSON.stringify(value));

function runtime(reduce = false) {
  let clock = 0, frameID = 0;
  const frames = new Map(), listeners = new Set(), writes = [];
  const media = { matches: reduce, addEventListener(_event, fn) { listeners.add(fn); }, removeEventListener(_event, fn) { listeners.delete(fn); } };
  const body = { nodeType: 1, querySelectorAll() { return []; } };
  const document = { body, documentElement: body, scrollingElement: body, addEventListener() {}, querySelector() { return null; } };
  const window = { __gosx: {}, console, matchMedia: () => media, addEventListener() {},
    requestAnimationFrame(fn) { frames.set(++frameID, fn); return frameID; }, cancelAnimationFrame(id) { frames.delete(id); } };
  const context = vm.createContext({ window, document, console, performance: { now: () => clock }, Date, setTimeout, clearTimeout });
  vm.runInContext(read('bootstrap-src/06-motion-core.ts'), context);
  vm.runInContext(ts.transpileModule(read('../runtime/scene3d/particle-burst.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  const baselineListeners = listeners.size;
  const state = { points: [{ id: 'stars' }], computeParticles: [{ id: 'smoke', count: 2 }], waterSystems: [{ id: 'water' }] };
  const mount = { isConnected: true, __gosxScene3DState: state };
  const handle = { applyCommands(commands) {
    writes.push(clone(commands));
    for (const command of commands) if (command.kind === 6) Object.assign(state, command.data);
  }, dispose() { mount.isConnected = false; } };
  mount.__gosxScene3DHandle = handle;
  const api = window.__gosx_scene3d_particle_burst_api;
  return { api, window, document, context, state, mount, handle, frames, writes, listeners, baselineListeners,
    play(plan = fixture) { return api.attach(plan, mount, handle, () => mount.__gosxScene3DHandle === handle); },
    frame(delta = 20) { clock += delta; const pending = [...frames.values()]; frames.clear(); for (const fn of pending) fn(clock); },
    reduce() { media.matches = true; for (const fn of [...listeners]) fn(media); },
  };
}

test('Go-authored burst waits for contact, preserves layers, fades and expires without continuous work', async () => {
  const r = runtime(), effect = r.play();
  r.frame(); r.frame(100);
  assert.equal(r.state.computeParticles.length, 1);
  r.frame(100);
  assert.equal(r.state.computeParticles.length, 2);
  assert.equal(r.state.computeParticles[1].emitter.once, true);
  assert.equal(r.state.computeParticles[1].material.opacityEnd, 0);
  for (let i = 0; i < 8; i++) r.frame(100);
  assert.deepEqual(clone(await effect.finished), { finished: true, suppressed: false });
  assert.deepEqual(clone(r.state), { points: [{ id: 'stars' }], computeParticles: [{ id: 'smoke', count: 2 }], waterSystems: [{ id: 'water' }] });
  const count = r.writes.length; r.frame(); r.frame(); assert.equal(r.writes.length, count);
  assert.equal(r.listeners.size, r.baselineListeners);
});

test('repeating an ID replaces playback with fresh renderer identity and resolves coalesced cleanup', async () => {
  const r = runtime(), plan = { ...clone(fixture), delay: 0, duration: fixture.duration - fixture.delay };
  const first = r.play(plan); r.frame();
  const oldID = r.state.computeParticles[1].id;
  const second = r.play(plan); r.frame();
  assert.deepEqual(clone(await first.finished), { finished: false, reason: 'replaced' });
  assert.notEqual(r.state.computeParticles[1].id, oldID);
  second.cancel(); r.frame();
  assert.equal((await second.finished).reason, 'cancelled');
  assert.equal(r.state.computeParticles.length, 1);
});

test('concurrent bursts share cleanup writes and enforce bounded mount capacity', async () => {
  const r = runtime(), effects = [];
  for (let i = 0; i < 16; i++) { const plan = clone(fixture); plan.id = 'impact-' + i; plan.particles.id = 'gosx-burst/' + plan.id; effects.push(r.play(plan)); }
  const extra = clone(fixture); extra.id = 'extra'; extra.particles.id = 'gosx-burst/extra';
  assert.throws(() => r.play(extra), /capacity/);
  r.frame(); for (let i = 0; i < 10; i++) r.frame(100);
  assert.equal((await Promise.all(effects.map(effect => effect.finished))).length, 16);
  assert.equal(r.state.computeParticles.length, 1);
});

test('authoritative particles supersede pending writes and disposal restores adapters', async () => {
  const r = runtime(), originalApply = r.handle.applyCommands, originalDispose = r.handle.dispose;
  const effect = r.play();
  r.handle.applyCommands([{ kind: 6, data: { points: [], computeParticles: [{ id: 'new' }], waterSystems: [] } }]);
  r.frame(); assert.equal((await effect.finished).reason, 'commands');
  assert.deepEqual(clone(r.state.computeParticles), [{ id: 'new' }]);
  const pending = r.play(); r.handle.dispose(); r.frame();
  assert.equal((await pending.finished).reason, 'disposed');
  assert.equal(r.handle.applyCommands, originalApply); assert.equal(r.handle.dispose, originalDispose);
});

test('initial and changed reduced motion suppress decorative effects and remove listeners', async () => {
  for (const initial of [false, true]) {
    const r = runtime(initial), effect = r.play();
    r.frame(); if (!initial) { r.frame(100); r.frame(100); assert.equal(r.state.computeParticles.length, 2); r.reduce(); }
    r.frame(); assert.deepEqual(clone(await effect.finished), { finished: true, suppressed: true });
    assert.equal(r.state.computeParticles.length, 1); assert.equal(r.listeners.size, r.baselineListeners);
  }
});

test('malformed replacements leave playback intact and caller mutation cannot affect a burst', async () => {
  const r = runtime(), plan = clone(fixture), effect = r.play(plan);
  plan.particles.count = 10000;
  for (const change of [p => p.duration = 0, p => p.particles.count = 0, p => p.particles.emitter.once = false, p => p.particles.material.size = NaN]) {
    const bad = clone(fixture); change(bad); assert.throws(() => r.play(bad));
  }
  r.frame(); r.frame(100); r.frame(100); assert.equal(r.state.computeParticles[1].count, 6);
  effect.cancel(); r.frame(); await effect.finished;
});

test('synchronous and asynchronous application failures reject finished and stop playback', async () => {
  for (const asyncFailure of [false, true]) {
    const r = runtime(); r.handle.applyCommands = () => { if (asyncFailure) return Promise.reject(new Error('render failed')); throw new Error('render failed'); };
    const effect = r.play(); const rejected = assert.rejects(effect.finished, /render failed/);
    r.frame(); r.frame(100); r.frame(100); await rejected;
    assert.equal(r.listeners.size, r.baselineListeners);
  }
});

test('unsupported rendering backends reject before allocating an effect', () => {
  const r = runtime(); r.mount.getAttribute = () => 'canvas';
  assert.throws(() => r.play(), /require WebGL or WebGPU/);
  assert.equal(r.state.computeParticles.length, 1);
  assert.equal(r.writes.length, 0);
});

test('timeline and burst adapters compose in either order and restore the original handle', async () => {
  const timeline = JSON.parse(read('../../scene/testdata/timeline.json'));
  for (const timelineFirst of [true, false]) {
    const r = runtime(), originalApply = r.handle.applyCommands, originalDispose = r.handle.dispose;
    r.state.camera = {};
    r.handle.setCamera = () => {};
    vm.runInContext(ts.transpileModule(read('../runtime/scene3d/timeline.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, r.context);
    const playTimeline = () => r.window.__gosx_scene3d_timeline_api.attach(timeline, r.mount, r.handle, () => r.mount.__gosxScene3DHandle === r.handle);
    const first = timelineFirst ? playTimeline() : r.play();
    const second = timelineFirst ? r.play() : playTimeline();
    const motion = timelineFirst ? first : second, particles = timelineFirst ? second : first;
    let particleResult;
    particles.finished.then(result => { particleResult = result; });
    motion.pause(); motion.seek(.1);
    r.frame(); r.frame(100); r.frame(100); await Promise.resolve();
    assert.equal(particleResult, undefined, 'timeline presentation must not cancel burst ownership');
    assert.equal(r.state.computeParticles.length, 2);
    assert.ok(r.writes.some(commands => commands.some(command => command.kind === 2)), 'timeline must still publish transforms');
    r.handle.applyCommands([{ kind: 2, objectId: 'piece', data: { x: 4 } }]);
    assert.equal((await motion.finished).reason, 'commands');
    assert.equal(particleResult, undefined, 'authoritative transforms must not cancel particles');
    r.handle.dispose(); r.frame();
    assert.equal((await particles.finished).reason, 'disposed');
    assert.equal(r.handle.applyCommands, originalApply);
    assert.equal(r.handle.dispose, originalDispose);
    assert.equal(r.listeners.size, r.baselineListeners);
  }
});

test('burst bridge rejects non-finite readiness timeouts before scheduling', async () => {
  const r = runtime();
  vm.runInContext(ts.transpileModule(read('../runtime/scene3d/command-runtime.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, r.context);
  r.context.setTimeout = () => assert.fail('invalid timeouts must not schedule work');
  for (const timeoutMS of [NaN, Infinity, -Infinity, 'later']) {
    await assert.rejects(r.window.__gosx_scene3d_command_bridge.burstParticles('missing', fixture, { timeoutMS }), /timeout must be finite/);
  }
});
