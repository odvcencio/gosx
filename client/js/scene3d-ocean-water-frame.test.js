"use strict";
// The drawn water's frame, for a host that moves a vessel's pose itself (Scene3D.OceanWater in Go; scene.OceanWaterFrame).
// recordOceanWater (mount-backend.ts) copies the ocean-fft glue's water() into the record a host reads; these tests drive the real
// glue on WebGPU and WebGL2 through a drift change and a pause, and the Gerstner fallback, and read the record the way a host does.
const test = require("node:test"), assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { createRequire } = require("node:module");
const ts = createRequire(path.join(__dirname, "..", "runtime", "package.json"))("typescript");
const { makeFakeGPUDevice } = require("./runtime-test-harness.js");
const { RecordingGL, loadSources, floatCaps, runnerContext } = require("./elio-graph-test-support.js");

const scene3d = path.join(__dirname, "..", "runtime", "scene3d");
const backend = fs.readFileSync(path.join(scene3d, "mount-backend.ts"), "utf8");
// recordSceneWaterFrame is what the mount calls after each drawn frame; it records the ocean first.
const recordSource = backend.slice(backend.indexOf("  function recordSceneWaterFrame("), backend.indexOf("  function sceneDebugBundleCounts("));
const CHUNK = ["ocean-fft-surface.ts", "ocean-fft-surface-gl.ts", "ocean-fft.ts"];

// Indices of the record: scene/ocean_water.go (OceanWaterIdx*).
const FRAME = 0, TIME = 1, X = 2, Z = 3, SPECTRAL = 4, SCALE = 5, DRIFT_X = 6, DRIFT_Z = 7, AT = 8;

function page(options = {}) {
  const context = runnerContext({ Float64Array, performance: { timeOrigin: 1e12, now: () => options.now ? options.now() : 0 }, GPUBufferUsage: { MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512 }, GPUTextureUsage: { COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } });
  if (options.chunk !== false) loadSources(context, CHUNK);
  vm.runInContext(ts.transpileModule(recordSource, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText + "\nthis.recordFrame = recordSceneWaterFrame;", context);
  return context;
}
const ocean = (extra) => Object.assign({ level: 0, spectrum: { windSpeed: 9 }, graphParams: { windSpeed: 9, foamThreshold: 0.3, driftX: 2, driftZ: -1 }, graphTween: 0 }, extra);
const bundle = (o) => ({ environment: { ocean: o } });
const close = (a, b, why) => assert.ok(Math.abs(a - b) < 1e-5, why + ": " + a + " vs " + b);

// A mount whose host asked for the record (what handle.oceanWater() does), and a renderer frame on WebGL2.
function webgl2(options) {
  const context = page(options), gl = new RecordingGL(), mount = { attrs: {}, setAttribute(k, v) { this.attrs[k] = v; } };
  const api = context.window.__gosx_scene3d_ocean_fft_gl_api, resources = {}, textureCache = { _sceneTextureEpoch: 0 };
  const record = new Float64Array(9); mount.__gosxOceanWater = record;
  // One rendered frame: what sceneOceanWebGLDraw and renderMountedFrame do, in that order.
  const draw = (o, time) => {
    const eng = o.spectrum ? api.run(resources, gl, { ocean: o, tier: "high", time, camera: { x: 0, y: 30, z: 0 }, textureCache, mount, caps: floatCaps }) : null;
    if (!eng && resources.fft) { resources.fft.dispose(); resources.fft = null; }
    context.recordFrame(mount, bundle(o), time);
    return eng;
  };
  return { context, gl, mount, record, draw, resources, fft: context.window.__gosx_scene3d_ocean_fft_api };
}

test("WebGL2: the record is the graph's water frame, follows a drift change, holds still on a pause, and counts frames", () => {
  const p = webgl2(), o = ocean();
  assert.equal(p.record[FRAME], 0, "no reading before the first drawn frame");
  p.draw(o, 100);
  assert.deepEqual([p.record[FRAME], p.record[SPECTRAL], p.record[X], p.record[TIME]], [1, 1, 0, 0], "first frame: dt is 0, the water has not moved");
  for (let i = 1; i <= 60; i++) p.draw(o, 100 + i / 60);
  const w = p.fft.water();
  close(p.record[X], 2, "driftX 2 m/s for one second"); close(p.record[Z], -1, "driftZ -1 m/s");
  close(p.record[TIME], 1, "wave time follows presentation time at timeScale 1");
  assert.deepEqual([p.record[X], p.record[Z], p.record[TIME]], [w.x, w.z, w.time], "exactly the graph's own values, not a recomputation");
  assert.deepEqual([p.record[SCALE], p.record[DRIFT_X], p.record[DRIFT_Z]], [1, 2, -1], "the rates carry a reading forward");
  assert.equal(p.record[FRAME], 61);
  assert.equal(p.record[AT], 1e12, "epoch milliseconds: timeOrigin plus now()");
  // The drift changes to (4, 3) and the timeScale to 2: the offset keeps what it had integrated and moves at the new rate.
  const o2 = ocean({ graphParams: { windSpeed: 9, foamThreshold: 0.3, driftX: 4, driftZ: 3, timeScale: 2 } });
  for (let i = 1; i <= 60; i++) p.draw(o2, 101 + i / 60);
  close(p.record[X], 2 + 4, "the old offset plus a second at 4 m/s"); close(p.record[Z], -1 + 3, "z likewise");
  close(p.record[TIME], 1 + 2, "a second at timeScale 2");
  assert.deepEqual([p.record[SCALE], p.record[DRIFT_X], p.record[DRIFT_Z]], [2, 4, 3]);
  // A pause: the scene clock stops, so the graph's dt is 0. The water frame holds still, and the frame count says a frame was drawn.
  const held = [p.record[X], p.record[Z], p.record[TIME]], count = p.record[FRAME];
  for (let i = 0; i < 5; i++) p.draw(o2, 102);
  assert.deepEqual([p.record[X], p.record[Z], p.record[TIME]], held, "a paused scene's water does not move");
  assert.equal(p.record[FRAME], count + 5);
});

test("WebGL2: a graph that cannot run reports the Gerstner fallback: no drift, the scene clock, Spectral false", () => {
  const p = webgl2(), o = ocean();
  const eng = p.draw(o, 10); p.draw(o, 11);
  assert.equal(p.record[SPECTRAL], 1);
  eng.fail("surface-compile"); // the renderer's route for a surface it could not build
  assert.equal(p.draw(o, 12), null, "the renderer now draws Gerstner");
  assert.deepEqual([p.record[SPECTRAL], p.record[X], p.record[Z], p.record[TIME], p.record[SCALE], p.record[DRIFT_X]], [0, 0, 0, 12, 1, 0]);
  assert.equal(p.fft.water(), null);
  p.draw(o, 12.5);
  assert.equal(p.record[TIME], 12.5);
});

test("a plain Gerstner ocean (no spectrum, no ocean-fft chunk) reports the scene clock", () => {
  const context = page({ chunk: false });
  const mount = { __gosxOceanWater: new Float64Array(9) };
  context.recordFrame(mount, bundle({ level: 0 }), 33.25);
  const r = mount.__gosxOceanWater;
  assert.deepEqual(Array.from(r.slice(0, 8)), [1, 33.25, 0, 0, 0, 1, 0, 0]);
});

test("nothing is recorded until a host asks, and not for a scene without an ocean", () => {
  const context = page({ chunk: false }), mount = {};
  context.recordFrame(mount, bundle({ level: 0 }), 1);
  assert.equal(mount.__gosxOceanWater, undefined, "the record is made by the handle's oceanWater(), not by the render path");
  mount.__gosxOceanWater = new Float64Array(9);
  context.recordFrame(mount, { environment: {} }, 1);
  context.recordFrame(mount, null, 1);
  assert.equal(mount.__gosxOceanWater[FRAME], 0);
});

test("latency: a reading taken before a frame is recorded is that of the frame before, a worker's sink gets each frame as it is recorded", () => {
  let now = 0;
  const p = webgl2({ now: () => now }), o = ocean(), sunk = [];
  p.mount.__gosxOceanWaterSink = (w) => sunk.push(Array.from(w));
  for (let i = 0; i <= 10; i++) { now = 1000 * i / 60; p.draw(o, 50 + i / 60); }
  // The host reads (to place the pose that the next render draws) after frame 10 and before frame 11 is recorded.
  const reading = Array.from(p.record);
  assert.equal(reading[FRAME], 11);
  now = 1000 * 11 / 60; p.draw(o, 50 + 11 / 60);
  assert.equal(p.record[FRAME] - reading[FRAME], 1, "the reading was one frame old when the next frame was drawn");
  close(p.record[TIME] - reading[TIME], 1 / 60, "one frame of wave time");
  close(p.record[X] - reading[X], 2 / 60, "one frame of drift");
  assert.equal(sunk.length, 12, "the sink is called once per recorded frame, in the same task");
  assert.deepEqual(sunk[10], reading);
  assert.ok(Math.abs(p.record[AT] - reading[AT] - 1000 / 60) < 1e-3, "AtMS lets a host measure the age in milliseconds");
});

// --- WebGPU ------------------------------------------------------------------------------------------------------------

function webgpu() {
  const context = page(), fake = makeFakeGPUDevice(), validation = new Map(), mount = { __gosxOceanWater: new Float64Array(9) };
  const api = context.window.__gosx_scene3d_ocean_fft_api, resources = {};
  const host = { device: fake.device, validation, guard: { disabled: new Set(), failures: [], fail(label, message) { this.failures.push({ label, message }); this.disabled.add("oceanFFT"); } }, camera: { x: 0, y: 30, z: 0 }, tier: "high",
    create(device, kind, descriptor) { const handle = { label: descriptor.label }; validation.set(handle, { ready: false, label: descriptor.label }); return handle; } };
  const settle = () => { for (const state of validation.values()) state.ready = true; };
  const draw = (o, time) => {
    api.frame(resources, Object.assign({}, host, { environment: { ocean: o }, time, encoder: fake.device.createCommandEncoder() }));
    context.recordFrame(mount, bundle(o), time);
  };
  return { context, mount, record: mount.__gosxOceanWater, draw, settle, api, resources };
}

test("WebGPU: the record is the graph's water frame through a drift change and a pause", () => {
  const p = webgpu(), o = ocean();
  p.draw(o, 100); p.settle();
  p.draw(o, 100);
  assert.equal(p.record[SPECTRAL], 1);
  for (let i = 1; i <= 60; i++) p.draw(o, 100 + i / 60);
  close(p.record[X], 2, "driftX 2 m/s for one second"); close(p.record[Z], -1, "driftZ"); close(p.record[TIME], 1, "wave time");
  const w = p.api.water();
  assert.deepEqual([p.record[X], p.record[Z], p.record[TIME]], [w.x, w.z, w.time]);
  const o2 = ocean({ graphParams: { windSpeed: 9, foamThreshold: 0.3, driftX: -3, driftZ: 0 } });
  for (let i = 1; i <= 30; i++) p.draw(o2, 101 + i / 60);
  close(p.record[X], 2 - 1.5, "half a second at -3 m/s"); close(p.record[Z], -1, "z stops");
  const held = [p.record[X], p.record[Z], p.record[TIME]];
  p.draw(o2, 101.5); p.draw(o2, 101.5);
  assert.deepEqual([p.record[X], p.record[Z], p.record[TIME]], held);
});

test("WebGPU: pipelines still validating draw the Gerstner sea, and the record says so until the graph runs", () => {
  const p = webgpu(), o = ocean();
  p.draw(o, 5);
  assert.deepEqual([p.record[SPECTRAL], p.record[X], p.record[TIME]], [0, 0, 5], "the first frame has no graph yet");
  p.settle(); p.draw(o, 6); p.draw(o, 6.5);
  assert.equal(p.record[SPECTRAL], 1);
  p.draw({ level: 0 }, 7);
  assert.equal(p.record[SPECTRAL], 0, "the spectrum is gone: the graph is torn down and the record says Gerstner");
  assert.equal(p.record[TIME], 7);
});

test("the water frame is one reused object: publishing a frame allocates nothing", () => {
  const p = webgl2(), o = ocean();
  p.draw(o, 1); p.draw(o, 2);
  const first = p.fft.water();
  p.draw(o, 3);
  assert.equal(p.fft.water(), first);
});

test("the command bridge makes the record on the mount once, keeps the sink, and has none for a handle without its mount", () => {
  const source = fs.readFileSync(path.join(scene3d, "command-runtime.ts"), "utf8");
  const text = source.slice(source.indexOf("  function oceanWater("), source.indexOf("  function whenReady("));
  const mount = {}, records = new Map([["ship", { handle: {}, mount }], ["bare", { handle: {}, mount: null }]]);
  const context = vm.createContext({ Float64Array, record: (target) => records.get(target) || null });
  vm.runInContext(ts.transpileModule(text, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText + "\nthis.oceanWater = oceanWater;", context);
  const first = context.oceanWater("ship");
  assert.equal(first.length, 9);
  assert.equal(context.oceanWater("ship"), first, "the same array each time");
  assert.equal(mount.__gosxOceanWater, first);
  const sink = () => {};
  context.oceanWater("ship", sink);
  assert.equal(mount.__gosxOceanWaterSink, sink);
  context.oceanWater("ship", null);
  assert.equal(mount.__gosxOceanWaterSink, null, "null stops the sink");
  context.oceanWater("ship");
  assert.equal(mount.__gosxOceanWaterSink, null, "a read does not touch the sink");
  assert.equal(context.oceanWater("bare"), null);
  assert.equal(context.oceanWater("missing"), null);
});
