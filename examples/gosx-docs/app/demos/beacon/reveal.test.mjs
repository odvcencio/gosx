import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
const source = fs.readFileSync(new URL("../../../public/blackglass-content.js", import.meta.url), "utf8");
function fixture() {
  const bytes = {}, fps = {}, scene = {}, events = {}, documentEvents = {};
  const root = { dataset: {}, isConnected: true, querySelector: selector => selector.includes('"bytes"') ? bytes : selector.includes('"fps"') ? fps : scene,
    addEventListener: (name, fn) => { events[name] = fn; } };
  let now = 10, tick, stopped = false;
  let timing = { submitAtMS: 0, frameIntervalMS: 0 };
  const document = { readyState: "complete", hidden: false, querySelector: () => root,
    addEventListener: (name, fn) => { documentEvents[name] = fn; } };
  const window = { __gosx_scene3d_debug_registry: new Map([["scene", { mount: scene, snapshot: () => ({ frameTiming: timing }) }]]),
    addEventListener: (name, fn) => { events[name] = fn; } };
  const performance = { now: () => now, getEntriesByType: kind => kind === "navigation" ? [{ name: "page", transferSize: 1024 }] :
    [{ name: "asset", transferSize: 2048 }, { name: "cached", transferSize: 0, encodedBodySize: 4096 }, { name: "blob:normal", transferSize: 8192 }] };
  vm.runInNewContext(source, { document, window, performance, setInterval: fn => { tick = fn; return 1; }, clearInterval: () => { stopped = true; } });
  return { root, document, bytes, fps, scene, events, advance(ms, interval = 1000 / 30) { now += ms; timing = { submitAtMS: now, frameIntervalMS: interval }; tick(); },
    idle(ms) { now += ms; tick(); }, stopped: () => stopped };
}
test("reveal waits six seconds after the first rendered frame", () => {
  const f = fixture();
  f.idle(10000);
  assert.equal(f.root.dataset.revealed, undefined);
  f.advance(1);
  f.advance(5999);
  assert.equal(f.root.dataset.revealed, undefined);
  f.advance(1);
  assert.equal(f.root.dataset.revealed, "true");
});
test("caption measures frame intervals and actual transfers, including cached zeroes", () => {
  const f = fixture(); f.advance(239);
  assert.equal(f.bytes.textContent, "3"); assert.equal(f.fps.textContent, "30");
});
for (const event of ["pointerdown", "keydown", "wheel", "scroll"]) test(`${event} reveals the controls immediately`, () => {
  const f = fixture(); f.events[event](); assert.equal(f.root.dataset.revealed, "true");
});
test("stalled rendering reports zero instead of the configured frame cap", () => {
  const f = fixture(); f.advance(239); f.idle(1001); assert.equal(f.fps.textContent, "0");
});
test("hidden pages discard old cadence and detached pages stop polling", () => {
  const f = fixture(); f.advance(239); f.document.hidden = true; f.idle(5000);
  f.document.hidden = false; f.advance(239, 1000 / 60); assert.equal(f.fps.textContent, "60");
  f.root.isConnected = false; f.idle(239); assert.equal(f.stopped(), true);
});
test("pagehide stops the sampler", () => { const f = fixture(); f.events.pagehide(); assert.equal(f.stopped(), true); });

test("WebGPU caption counts submitted frames rather than sampling its configured cap", () => {
 const f = fixture();
 f.scene.__gosxScene3DWebGPUStats = { frameSeq: 100 }; f.advance(200, 1000 / 30);
 f.scene.__gosxScene3DWebGPUStats.frameSeq += 12; f.advance(200, 1000 / 30);
 assert.equal(f.fps.textContent, "60");
});
