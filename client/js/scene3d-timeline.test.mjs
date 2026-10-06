import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import test from 'node:test';

const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8');
const fixture = JSON.parse(read('../../scene/testdata/timeline.json'));
const plain = value => JSON.parse(JSON.stringify(value));

function runtime(reduce = false) {
  let clock = 0, frameID = 0, alive = true;
  const frames = new Map(), listeners = new Set(), writes = [];
  const media = { matches: reduce, addEventListener(_name, fn) { listeners.add(fn); }, removeEventListener(_name, fn) { listeners.delete(fn); } };
  const body = { nodeType: 1, querySelectorAll() { return []; } };
  const document = { body, documentElement: body, scrollingElement: body, addEventListener() {}, querySelector() { return null; } };
  const window = { __gosx: {}, console, matchMedia: () => media, addEventListener() {},
    requestAnimationFrame(fn) { frames.set(++frameID, fn); return frameID; }, cancelAnimationFrame(id) { frames.delete(id); } };
  const context = vm.createContext({ window, document, console, performance: { now: () => clock }, Date, setTimeout, clearTimeout });
  vm.runInContext(read('bootstrap-src/06-motion-core.ts'), context);
  vm.runInContext(ts.transpileModule(read('../runtime/scene3d/timeline.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  const api = window.__gosx_scene3d_timeline_api;
  let current = null;
  return { api, window, context, writes, frames, listeners,
    play(spec = fixture, apply = commands => writes.push(plain(commands))) {
      current = api.play(spec, { alive: () => alive, apply, replace: () => current?.cancel('replaced') });
      return current;
    },
    frame(delta = 20) {
      clock += delta;
      const pending = [...frames.values()]; frames.clear();
      for (const callback of pending) callback(clock);
    },
    reduce() { media.matches = true; for (const fn of [...listeners]) fn(media); },
    dispose() { alive = false; current?.cancel('disposed'); },
  };
}

test('Go-authored wire fixture samples easing, parallel tracks, gaps and backward seeks', () => {
  const { api } = runtime();
  for (const [seconds, y, camera, scale] of [[-1, 1, 4, 1], [0.1, 0.25, 3.5, 1], [0.55, 0, 2, 1.1], [0.65, 0, 2, 1.05], [2, 0, 2, 1], [0.05, 0.5625, 3.75, 1]]) {
    const commands = plain(api.sample(fixture, seconds));
    assert.equal(commands.length, 2);
    assert.equal(commands[0].kind, 2); assert.equal(commands[0].objectId, 'piece');
    assert.equal(commands[1].kind, 5);
    assert.ok(Math.abs(commands[0].data.y - y) < 1e-12);
    assert.ok(Math.abs(commands[0].data.scaleX - scale) < 1e-12);
    assert.deepEqual(commands[1].data, { y: camera });
  }
});

test('playback shares the scheduler, pauses, seeks, completes and stops scheduling', async () => {
  const r = runtime(), player = r.play();
  r.frame(); r.frame(100);
  assert.ok(player.timeSeconds > 0);
  player.pause();
  const before = player.timeSeconds;
  r.frame(5000); assert.equal(player.timeSeconds, before);
  player.seek(0.55); r.frame();
  assert.equal(r.writes.at(-1)[0].data.scaleX, 1.1);
  player.seek(0.05); r.frame();
  assert.equal(r.writes.at(-1)[0].data.y, 0.5625);
  player.resume();
  for (let i = 0; i < 50; i++) { r.frame(); await Promise.resolve(); }
  assert.deepEqual(plain(await player.finished), { finished: true });
  assert.equal(r.frames.size, 0);
  assert.equal(r.writes.at(-1)[0].data.y, 0);
  assert.equal(r.listeners.size, 1); // The shared motion core owns the remaining listener.
});

test('reduced motion settles final values at mount and when the preference changes', async () => {
  for (const initiallyReduced of [false, true]) {
    const r = runtime(initiallyReduced), player = r.play();
    if (!initiallyReduced) { r.frame(); r.frame(); r.reduce(); }
    r.frame();
    assert.deepEqual(plain(await player.finished), { finished: true });
    assert.equal(r.writes.at(-1)[0].data.scaleX, 1);
    assert.equal(r.writes.at(-1)[1].data.y, 2);
    assert.equal(r.frames.size, 0);
  }
});

test('replacements and disposal suppress stale queued writes and release listeners', async () => {
  const r = runtime(), first = r.play();
  const second = r.play({ ...fixture, id: 'replace', tweens: [{ ...fixture.tweens[0], from: 2 }] });
  assert.deepEqual(plain(await first.finished), { finished: false, reason: 'replaced' });
  r.frame(); assert.equal(r.writes.length, 1); assert.equal(r.writes[0][0].data.y, 2);
  second.seek(0.1); r.dispose(); r.frame();
  assert.deepEqual(plain(await second.finished), { finished: false, reason: 'disposed' });
  assert.equal(r.writes.length, 1);
  assert.equal(r.frames.size, 0);
  assert.equal(r.listeners.size, 1);
});

test('invalid replacement is atomic and prepared playback owns its inputs', async () => {
  const r = runtime(), input = structuredClone(fixture), player = r.play(input);
  input.tweens[0].to = 99;
  for (const spec of [
    { ...fixture, version: 2 }, { ...fixture, tweens: [] },
    { ...fixture, tweens: [{ ...fixture.tweens[0], property: '__proto__' }] },
    { ...fixture, tweens: [{ ...fixture.tweens[0], from: NaN }] },
    { ...fixture, tweens: [{ ...fixture.tweens[0], duration: -1 }] },
    { ...fixture, tweens: [{ ...fixture.tweens[0], ease: { kind: 4, args: [2, 0, 1, 1] } }] },
    { ...fixture, tweens: [fixture.tweens[0], fixture.tweens[0]] },
  ]) assert.throws(() => r.play(spec));
  assert.throws(() => player.seek(Infinity));
  assert.throws(() => player.seek(1));
  player.finish(); r.frame();
  assert.deepEqual(plain(await player.finished), { finished: true });
  assert.equal(r.writes.at(-1)[0].data.y, 0);
});

test('application failures reject finished and release scheduler work', async () => {
  const r = runtime(), player = r.play(fixture, () => { throw new Error('disposed renderer'); });
  const rejection = assert.rejects(player.finished, /disposed renderer/);
  r.frame(); await rejection;
  r.frame(); assert.equal(r.frames.size, 0);
});

test('public bridge resolves a mounted timeline without applying command payloads', async () => {
  const r = runtime();
  vm.runInContext(ts.transpileModule(read('../runtime/scene3d/command-runtime.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, r.context);
  const result = { finished: Promise.resolve() };
  const handle = { __gosxScene3DCommandReady: true, applyCommands() { assert.fail('timeline dispatch is separate'); }, playTimeline(spec) { assert.equal(spec, fixture); return result; } };
  assert.equal(await r.window.__gosx_scene3d_command_bridge.playTimeline(handle, fixture), result);
});

test('shared render bundles receive node bindings and release the camera after settling', async () => {
  const r = runtime(), state = {}, poses = [], handle = {};
  vm.runInContext(ts.transpileModule(read('../runtime/scene3d/mount-controls.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, r.context);
  const playback = r.api.playMounted(fixture, state, handle, (_state, commands) => poses.push(plain(commands)), null, () => {}, () => {}, () => true);
  playback.pause(); playback.seek(0.1); r.frame();
  const bundle = { camera: { y: 4, z: 6 }, meshObjects: [{ id: 'piece', y: 1, x: 2 }] };
  const cameras = [];
  r.context.applyMotionBindingsToRuntimeBundle(bundle, state, { applyCamera: camera => cameras.push({ ...camera }) });
  assert.equal(bundle.meshObjects[0].y, 0.25);
  assert.equal(bundle.meshObjects[0].x, 2);
  assert.equal(bundle.camera.y, 3.5);
  assert.equal(cameras.length, 1);
  playback.finish(); r.frame(); await playback.finished;
  const next = { camera: { y: 8 }, meshObjects: [{ id: 'piece', y: 1 }] };
  r.context.applyMotionBindingsToRuntimeBundle(next, state, null);
  assert.equal(next.camera.y, 8, 'camera must be free for user control after completion');
  assert.equal(next.meshObjects[0].y, 0, 'settled pose must survive the shared runtime frame');
  r.api.clear(state, handle, [{ kind: 6 }]);
  assert.equal(state._gosxMotionRuntimeBindings.size, 1, 'particle updates preserve settled transforms');
  r.api.clear(state, handle, [{ kind: 2 }]);
  assert.equal(state._gosxMotionRuntimeBindings.size, 0, 'authoritative transforms supersede presentation');
});

test('demand-loaded adapters cancel paused playback on disposal and restore host methods', async () => {
  const r = runtime(), state = { camera: { y: 4 } };
  let alive = true, observerCallback, disconnected = false;
  r.context.MutationObserver = class {
    constructor(callback) { observerCallback = callback; }
    observe() {}
    disconnect() { disconnected = true; }
  };
  const mount = { __gosxScene3DState: state, isConnected: true };
  const apply = commands => r.writes.push(plain(commands));
  const dispose = () => { alive = false; };
  const handle = { applyCommands: apply, dispose, setCamera() {} };
  const playback = r.api.attach(fixture, mount, handle, () => alive);
  playback.pause(); playback.seek(0.1); r.frame();
  assert.equal(r.writes.length, 1, 'presentation must bypass the interruption adapter');
  mount.isConnected = false; observerCallback();
  assert.deepEqual(plain(await playback.finished), { finished: false, reason: 'disposed' });
  assert.equal(handle.applyCommands, apply);
  assert.equal(handle.dispose, dispose);
  assert.equal(disconnected, true);
});
