import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const source = '(function(){' + readFileSync(new URL('./hero-gpu.generated.js', import.meta.url), 'utf8') + readFileSync(new URL('./hero-loader.js', import.meta.url), 'utf8') + '})();';
function browser({ reduced = false, saveData = false, effectiveType = '4g', software = false, webgl = true, adapter, idle = true, complete = false } = {}) {
  const events = new Map(), frames = [], idles = [], timers = new Map(), requests = [];
  let nextTimer = 0;
  const listeners = () => ({ addEventListener(name, fn) { events.set(this.tag + name, fn); }, removeEventListener(name, fn) { if (events.get(this.tag + name) === fn) events.delete(this.tag + name); } });
  const media = { ...listeners(), tag: 'media', matches: reduced };
  const connection = { ...listeners(), tag: 'connection', saveData, effectiveType };
  const hero = { dataset: {}, isConnected: true, append(frame) { requests.push(frame); } };
  const template = { parentElement: hero, innerHTML: '<script src="/scene.js"></script>' };
  const gl = { VENDOR: 1, RENDERER: 2, getParameter() { return software ? 'SwiftShader' : 'NVIDIA'; }, getExtension() { return null; } };
  const document = { ...listeners(), tag: 'document', readyState: complete ? 'complete' : 'loading', querySelector() { return template; }, createElement(tag) { if (tag === 'canvas') return { getContext() { return webgl ? gl : null; } }; return { ...listeners(), tag, setAttribute() {}, remove() { requests.splice(requests.indexOf(this), 1); }, classList: { add() {} } }; } };
  const window = { ...listeners(), tag: 'window', requestIdleCallback: idle ? fn => { idles.push(fn); return 1; } : undefined, cancelIdleCallback() {} };
  const context = { window, document, navigator: { connection, gpu: { requestAdapter: async () => adapter } }, matchMedia: () => media, requestAnimationFrame(fn) { frames.push(fn); return 1; }, cancelAnimationFrame() {}, setTimeout(fn, ms) { timers.set(++nextTimer, { fn, ms }); return nextTimer; }, clearTimeout(id) { timers.delete(id); }, requestIdleCallback: window.requestIdleCallback };
  vm.runInNewContext(source, context);
  const paint = () => frames.shift()?.();
  const settle = async () => { for (let i = 0; i < 4; i++) await Promise.resolve(); };
  const upgrade = async () => { events.get('windowload')?.(); paint(); paint(); if (idle) idles.shift()?.(); else [...timers.values()].find(t => t.ms === 200)?.fn(); await settle(); };
  return { requests, hero, media, connection, events, frames, idles, timers, paint, upgrade, settle };
}

test('load, two paint boundaries, and idle precede scene requests', async () => {
  const b = browser();
  assert.equal(b.requests.length, 0);
  b.events.get('windowload')();
  b.paint(); assert.equal(b.idles.length, 0);
  b.paint(); assert.equal(b.requests.length, 0);
  b.idles.shift()(); await b.settle();
  assert.equal(b.requests.length, 1);
  assert.equal(b.hero.dataset.heroState, 'loading');
});
for (const opts of [{ reduced: true }, { saveData: true }, ...['slow-2g', '2g', '3g'].map(effectiveType => ({ effectiveType })), { software: true }, { webgl: false }, { webgl: false, adapter: { isFallbackAdapter: true } }, { webgl: false, adapter: { info: { description: 'llvmpipe' } } }]) {
  test(`still stays for ${JSON.stringify(opts)}`, async () => {
    const b = browser(opts); await b.upgrade();
    assert.equal(b.requests.length, 0);
    assert.equal(b.hero.dataset.heroState, 'still');
  });
}
test('a hardware WebGPU adapter can upgrade without WebGL2', async () => {
  const b = browser({ webgl: false, adapter: { isFallbackAdapter: false, info: { vendor: 'nvidia' } } });
  await b.upgrade(); assert.equal(b.requests.length, 1);
});
test('setTimeout fallback still waits for paint', async () => {
  const b = browser({ idle: false, complete: true });
  assert.equal(b.timers.size, 0);
  b.paint(); b.paint();
  assert.equal(b.requests.length, 0);
  [...b.timers.values()].find(t => t.ms === 200).fn(); await b.settle();
  assert.equal(b.requests.length, 1);
});
test('motion changes, route removal, and probe timeout cancel upgrades', async () => {
  const b = browser();
  b.events.get('windowload')(); b.paint(); b.paint();
  b.hero.isConnected = false;
  b.idles.shift()(); await b.settle(); assert.equal(b.requests.length, 0);
  const c = browser(); await c.upgrade();
  c.media.matches = true; c.events.get('mediachange')(); assert.equal(c.requests.length, 0);
});
test('fade waits for the runtime to publish an actual hardware frame', async () => {
  const b = browser(); await b.upgrade();
  const frame = b.requests[0];
  frame.contentWindow = {};
  let visible = false;
  frame.classList.add = () => { visible = true; };
  b.events.get('windowmessage')({ source: {}, data: 'gosx-home-hero-ready' });
  assert.equal(visible, false);
  b.events.get('windowmessage')({ source: frame.contentWindow, data: 'other' });
  assert.equal(visible, false);
  b.events.get('windowmessage')({ source: frame.contentWindow, data: 'gosx-home-hero-ready' });
  assert.equal(visible, true);
  assert.equal(b.hero.dataset.heroState, 'live');
});

test('a driver probe that times out cannot mount after it settles', async () => {
  const b = browser();
  b.events.get('windowload')(); b.paint(); b.paint();
  b.idles.shift()();
  [...b.timers.values()].find(t => t.ms === 15000).fn();
  await b.settle();
  assert.equal(b.requests.length, 0);
});
