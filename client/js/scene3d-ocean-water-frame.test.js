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

// Indices of the record: the oceanWater* constants in engine/wasm/scene_js.go.
const SEQ = 0, TIME = 1, X = 2, Z = 3, SPECTRAL = 4, SCALE = 5, DRIFT_X = 6, DRIFT_Z = 7, AT = 8, GEN = 9;

function page(options = {}) {
  const context = runnerContext({ Float64Array, performance: { timeOrigin: 1e12, now: () => options.now ? options.now() : 0 }, GPUBufferUsage: { MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512 }, GPUTextureUsage: { COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } });
  if (options.chunk !== false) loadSources(context, CHUNK);
  context.ensureSceneGatedFeatureLoaded = () => Promise.resolve({});
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
  const record = new Float64Array(340); mount.__gosxOceanWater = record;
  // One rendered frame: what sceneOceanWebGLDraw and renderMountedFrame do, in that order.
  const draw = (o, time) => {
    const eng = o.spectrum ? api.run(resources, gl, { ocean: o, tier: "high", time, camera: { x: 0, y: 30, z: 0 }, textureCache, mount, caps: floatCaps }) : null;
    if (!eng && resources.fft) { resources.fft.dispose(); resources.fft = null; }
    context.recordFrame(mount, bundle(o), time, "submitted");
    return eng;
  };
  return { context, gl, mount, record, draw, resources, fft: context.window.__gosx_scene3d_ocean_fft_api };
}

test("WebGL2: the record is the graph's water frame, follows a drift change, holds still on a pause, and counts frames", () => {
  const p = webgl2(), o = ocean();
  assert.equal(p.record[SEQ], 0, "no reading before the first drawn frame");
  p.draw(o, 100);
  assert.deepEqual([p.record[SEQ], p.record[SPECTRAL], p.record[X], p.record[TIME]], [1, 1, 0, 0], "first frame: dt is 0, the water has not moved");
  for (let i = 1; i <= 60; i++) p.draw(o, 100 + i / 60);
  const w = p.fft.water();
  close(p.record[X], 2, "driftX 2 m/s for one second"); close(p.record[Z], -1, "driftZ -1 m/s");
  close(p.record[TIME], 1, "wave time follows presentation time at timeScale 1");
  assert.deepEqual([p.record[X], p.record[Z], p.record[TIME]], [w.x, w.z, w.time], "exactly the graph's own values, not a recomputation");
  assert.deepEqual([p.record[SCALE], p.record[DRIFT_X], p.record[DRIFT_Z]], [1, 2, -1], "the rates carry a reading forward");
  assert.equal(p.record[SEQ], 61);
  assert.equal(p.record[AT], 1e12, "epoch milliseconds: timeOrigin plus now()");
  // The drift changes to (4, 3) and the timeScale to 2: the offset keeps what it had integrated and moves at the new rate.
  const o2 = ocean({ graphParams: { windSpeed: 9, foamThreshold: 0.3, driftX: 4, driftZ: 3, timeScale: 2 } });
  for (let i = 1; i <= 60; i++) p.draw(o2, 101 + i / 60);
  close(p.record[X], 2 + 4, "the old offset plus a second at 4 m/s"); close(p.record[Z], -1 + 3, "z likewise");
  close(p.record[TIME], 1 + 2, "a second at timeScale 2");
  assert.deepEqual([p.record[SCALE], p.record[DRIFT_X], p.record[DRIFT_Z]], [2, 4, 3]);
  // A pause: the scene clock stops, so the graph's dt is 0. The water frame holds still, and the frame count says a frame was drawn.
  const held = [p.record[X], p.record[Z], p.record[TIME]], count = p.record[SEQ];
  for (let i = 0; i < 5; i++) p.draw(o2, 102);
  assert.deepEqual([p.record[X], p.record[Z], p.record[TIME]], held, "a paused scene's water does not move");
  assert.equal(p.record[SEQ], count + 5);
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
  const mount = { __gosxOceanWater: new Float64Array(340) };
  context.recordFrame(mount, bundle({ level: 0 }), 33.25, "submitted");
  const r = mount.__gosxOceanWater;
  assert.deepEqual(Array.from(r.slice(0, 8)), [1, 33.25, 0, 0, 0, 1, 0, 0]);
});

test("nothing is recorded until a host asks, and not for a scene without an ocean", () => {
  const context = page({ chunk: false }), mount = {};
  context.recordFrame(mount, bundle({ level: 0 }), 1, "submitted");
  assert.equal(mount.__gosxOceanWater, undefined, "the record is made by the handle's oceanWater(), not by the render path");
  mount.__gosxOceanWater = new Float64Array(340);
  context.recordFrame(mount, { environment: {} }, 1, "submitted");
  context.recordFrame(mount, null, 1, "submitted");
  assert.equal(mount.__gosxOceanWater[SEQ], 0);
});

test("latency: a reading taken before a frame is recorded is that of the frame before, a worker's sink gets each frame as it is recorded", () => {
  let now = 0;
  const p = webgl2({ now: () => now }), o = ocean(), sunk = [];
  p.mount.__gosxOceanWaterSinks = new Set([(w) => sunk.push(Array.from(w))]);
  for (let i = 0; i <= 10; i++) { now = 1000 * i / 60; p.draw(o, 50 + i / 60); }
  // The host reads (to place the pose that the next render draws) after frame 10 and before frame 11 is recorded.
  const reading = Array.from(p.record);
  assert.equal(reading[SEQ], 11);
  now = 1000 * 11 / 60; p.draw(o, 50 + 11 / 60);
  assert.equal(p.record[SEQ] - reading[SEQ], 1, "the reading was one frame old when the next frame was drawn");
  close(p.record[TIME] - reading[TIME], 1 / 60, "one frame of wave time");
  close(p.record[X] - reading[X], 2 / 60, "one frame of drift");
  assert.equal(sunk.length, 12, "the sink is called once per recorded frame, in the same task");
  assert.deepEqual(sunk[10], reading);
  assert.ok(Math.abs(p.record[AT] - reading[AT] - 1000 / 60) < 1e-3, "AtMS lets a host measure the age in milliseconds");
});

// --- WebGPU ------------------------------------------------------------------------------------------------------------

function webgpu() {
  const context = page(), fake = makeFakeGPUDevice(), validation = new Map(), mount = { __gosxOceanWater: new Float64Array(340) };
  const api = context.window.__gosx_scene3d_ocean_fft_api, resources = {};
  const host = { device: fake.device, validation, guard: { disabled: new Set(), failures: [], fail(label, message) { this.failures.push({ label, message }); this.disabled.add("oceanFFT"); } }, camera: { x: 0, y: 30, z: 0 }, tier: "high",
    create(device, kind, descriptor) { const handle = { label: descriptor.label }; validation.set(handle, { ready: false, label: descriptor.label }); return handle; } };
  const settle = () => { for (const state of validation.values()) state.ready = true; };
  const draw = (o, time) => {
    api.frame(resources, Object.assign({}, host, { environment: { ocean: o }, time, encoder: fake.device.createCommandEncoder() }));
    context.recordFrame(mount, bundle(o), time, "submitted");
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

test("the command bridge makes the record on the mount once, keeps each sink in a set, and has none for a handle without its mount", () => {
  const source = fs.readFileSync(path.join(scene3d, "command-runtime.ts"), "utf8");
  const text = source.slice(source.indexOf("  function oceanWater("), source.indexOf("  function whenReady("));
  const mount = {}, records = new Map([["ship", { handle: {}, mount }], ["bare", { handle: {}, mount: null }]]);
  const context = vm.createContext({ Float64Array, Set, record: (target) => records.get(target) || null });
  vm.runInContext(ts.transpileModule(text, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText + "\nthis.oceanWater = oceanWater;", context);
  const first = context.oceanWater("ship");
  assert.equal(first.length, 340);
  assert.equal(context.oceanWater("ship"), first, "the same array each time");
  assert.equal(mount.__gosxOceanWater, first);
  assert.equal(mount.__gosxOceanWaterSinks, undefined, "a read adds no sink");
  const a = () => {}, b = () => {};
  context.oceanWater("ship", a); context.oceanWater("ship", b); context.oceanWater("ship", a);
  assert.deepEqual([...mount.__gosxOceanWaterSinks], [a, b], "two engines watching one mount keep both sinks");
  context.oceanWater("ship", a, true);
  assert.deepEqual([...mount.__gosxOceanWaterSinks], [b], "stopping one leaves the other");
  mount.__gosxOceanWater[10] = 5; // a worker host asked for a probe
  context.oceanWater("ship", a, true);
  assert.equal(mount.__gosxOceanWater[10], 5, "another sink is still watching");
  context.oceanWater("ship", b, true);
  assert.equal(mount.__gosxOceanWater[10], 0, "the last sink gone: no host is left, so the page stops probing");
  assert.equal(context.oceanWater("bare"), null);
  assert.equal(context.oceanWater("missing"), null);
});

test("only frames the renderer submitted are recorded", () => {
  const p = webgl2();
  p.draw(ocean(), 1);
  const seq = p.record[SEQ], at = p.record[AT];
  for (const outcome of ["empty", "skipped", undefined]) p.context.recordFrame(p.mount, bundle(ocean()), 2, outcome);
  assert.equal(p.record[SEQ], seq, "a frame that was not submitted does not count");
  assert.equal(p.record[AT], at);
});

test("the generation changes when the graph is rebuilt, and Time restarts with it", () => {
  const p = webgl2(), o = ocean();
  p.draw(o, 10); p.draw(o, 11);
  const gen = p.record[GEN];
  assert.ok(gen > 0, "a spectral reading carries its graph's generation");
  const t = p.record[TIME];
  p.draw(o, 12);
  assert.equal(p.record[GEN], gen, "the same graph keeps its generation");
  p.resources.fft.dispose(); p.resources.fft = null; // what a lost context or a tier change does
  p.draw(o, 13); p.draw(o, 14);
  assert.notEqual(p.record[GEN], gen);
  assert.ok(p.record[TIME] < t + 2, "the new graph's clock restarted");
  p.draw({ level: 0 }, 15);
  assert.equal(p.record[GEN], 0, "the Gerstner sea has no graph");
});

test("WebGL2: a wave clock follows the scene clock across frames the renderer did not draw", () => {
  const p = webgl2(), o = ocean();
  p.draw(o, 100);
  p.draw(o, 101); // a second of scene time passed with no ocean frame, as in the first frames of a backend
  close(p.record[TIME], 1, "wave time is not capped to one long frame");
  close(p.record[X], 2, "nor is the drift offset");
  p.draw(o, 101.016);
  close(p.record[TIME], 1.016, "normal frames add their own delta");
});

// --- the real WebGL renderer's ocean draw ----------------------------------------------------------------------------------

const { createWebGLRendererForPost, makePointsBundle } = require("./runtime-test-harness.js");
// The renderer's own sceneOceanWebGLDraw runs against its fake context; the graph glue is the real one, driven by the recording
// WebGL2 double (the fake context cannot run gather passes). The wrapper only hands the renderer an engine whose bind does nothing.
function realDrawRenderer() {
  const h = createWebGLRendererForPost({ fresh: true });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2"), enabled = new Set([gl.CULL_FACE]);
  gl.isEnabled = (c) => enabled.has(c); gl.enable = (c) => enabled.add(c); gl.disable = (c) => enabled.delete(c);
  for (const [name, fn] of Object.entries({ createSampler: () => ({}), deleteSampler() {}, samplerParameteri() {}, bindSampler() {}, uniform4fv() {}, uniform1i() {}, blendFuncSeparate() {}, uniformMatrix4fv() {} })) if (!gl[name]) gl[name] = fn;
  const context = h.env.context;
  context.window = context.window || context;
  for (const name of ["Float32Array", "Float64Array", "Uint32Array", "Uint8Array", "ArrayBuffer", "DataView", "Set", "Map", "Promise", "Error"]) if (!context[name]) context[name] = globalThis[name];
  context.performance = context.performance || { timeOrigin: 1e12, now: () => 0 };
  loadSources(context, ["ocean-fft-kernels.ts", "elio-graph.ts", "elio-graph-gather.ts", "elio-graph-compute.ts", ...CHUNK]);
  const recording = new RecordingGL(), real = context.window.__gosx_scene3d_ocean_fft_gl_api;
  context.window.__gosx_scene3d_ocean_fft_gl_api = { run: (resources, g, host) => {
    const eng = real.run(resources, recording, Object.assign({}, host, { caps: floatCaps }));
    return eng ? { bind() {}, fail: (r) => eng.fail(r), dispose: () => eng.dispose() } : null;
  } };
  vm.runInContext(ts.transpileModule(recordSource, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText + "\nthis.recordFrame = recordSceneWaterFrame;", context);
  return { h, mount, context, fft: context.window.__gosx_scene3d_ocean_fft_api };
}
function seaBundle(spectrum) {
  const b = makePointsBundle(null);
  b.points = [];
  b.environment.sky = { mode: "physical" };
  b.environment.ocean = Object.assign({ level: 0, windDirection: 0, waveHeight: 0.8, waveLength: 18, choppiness: 0.6, speed: 1, deepColor: "#03141f", shallowColor: "#1f6f78", scatterColor: "#2fa58f", foamColor: "#e9eef0", clarity: 4, roughness: 0.06, foam: 0.6, surf: 0.5, extent: 4000, bathymetry: null }, spectrum ? { spectrum, graphParams: { windSpeed: 9, driftX: 2, driftZ: -1 }, graphTween: 0 } : {});
  b.camera.y = 30; b.camera.z = 0;
  return b;
}

test("the real WebGL ocean draw: removing the spectrum clears the water frame, so the record reports Gerstner", () => {
  const r = realDrawRenderer(), record = new Float64Array(340);
  r.mount.__gosxOceanWater = record;
  const frame = (bundle, time) => { r.h.renderer.render(bundle, { width: 320, height: 180 }, { sceneTime: time }); r.context.recordFrame(r.mount, bundle, time, "submitted"); };
  frame(seaBundle({ windSpeed: 9 }), 1); frame(seaBundle({ windSpeed: 9 }), 1.5);
  assert.equal(record[SPECTRAL], 1, "a spectral frame is drawn");
  assert.ok(r.fft.water(), "the graph published its water");
  frame(seaBundle(null), 2);
  assert.equal(r.fft.water(), null, "the renderer tore the graph down and the frame went with it");
  assert.deepEqual([record[SPECTRAL], record[X], record[Z], record[GEN]], [0, 0, 0, 0]);
  assert.equal(record[TIME], 2, "Gerstner runs on the scene clock");
  r.h.renderer.dispose();
});

test("the real WebGL renderer's teardown (a lost context) clears the water frame", () => {
  const r = realDrawRenderer();
  r.h.renderer.render(seaBundle({ windSpeed: 9 }), { width: 320, height: 180 }, { sceneTime: 1 });
  assert.ok(r.fft.water());
  r.h.renderer.dispose();
  assert.equal(r.fft.water(), null);
});

// --- the host probe: drawn heights at host-given points (Scene3D.OceanProbe in Go) --------------------------------------------------

const COUNT = 10, P_SEQ = 11, P_GEN = 12, P_FLAGS = 13, P_N = 14, P_LAT = 15, P_AT = 16, P_TICK = 17, PTS = 18, OUT = 146, REQ = 338, ECHO = 339;
const VALID = 1, GERSTNER = 2, UNAVAILABLE = 4;
const askAt = (record, points) => { record[COUNT] = points.length; record[REQ]++; points.forEach(([x, z], i) => { record[PTS + 2 * i] = x; record[PTS + 2 * i + 1] = z; }); };
// What the shaders write: the height with the ocean level already in it (base is the level plus the swell's height).
const pattern = (n, base = 0.5) => { const a = new Float32Array(64 * 4); for (let i = 0; i < n; i++) { a[i * 4] = base + i; a[i * 4 + 1] = 0.1; a[i * 4 + 2] = -0.2; a[i * 4 + 3] = 3; } return a; };
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const probeShaders = (gl) => gl.created.program.filter((x) => x.shaders.some((sh) => /uniform vec4 pts\[/.test(sh.source || "")));

// WebGPU: the fake device's copyBufferToBuffer of the probe output (64 records of 16 bytes) writes a pattern the test controls.
function gpuProbe(options = {}) {
  const context = page(), fake = makeFakeGPUDevice({ timestampQuery: true }), validation = new Map(), mount = { __gosxOceanWater: new Float64Array(340) };
  const api = context.window.__gosx_scene3d_ocean_fft_api, resources = {}, p = { copies: [], heights: pattern(8), mount, record: mount.__gosxOceanWater, api, resources, context, fake, failures: [] };
  const create = fake.device.createCommandEncoder.bind(fake.device);
  fake.device.createCommandEncoder = () => {
    const encoder = create();
    encoder.copyBufferToBuffer = (source, so, destination, doff, size) => { if (size !== 64 * 16) return; p.copies.push({ source, destination, size }); new Float32Array(destination._backing).set(p.heights); };
    return encoder;
  };
  const host = { device: fake.device, validation, guard: { disabled: new Set(), failures: p.failures, fail(label, message) { this.failures.push({ label, message }); this.disabled.add("oceanFFT"); } }, camera: { x: 0, y: 30, z: 0 }, tier: "high",
    create(device, kind, descriptor) { const handle = { label: descriptor.label }; validation.set(handle, { ready: false, label: descriptor.label }); return handle; } };
  p.settle = () => { for (const state of validation.values()) state.ready = true; };
  p.update = (o, time) => api.frame(resources, Object.assign({}, host, { environment: { ocean: o }, time, encoder: fake.device.createCommandEncoder() }));
  p.draw = (o, time, outcome = "submitted") => { p.update(o, time); context.recordFrame(mount, bundle(o), time, outcome); };
  p.draw(ocean({ level: 2 }), 1); p.settle(); p.draw(ocean({ level: 2 }), 1.02);
  return p;
}

test("probe, WebGPU: a host that asks gets the shader's heights as written (level not added again), for the frame they were issued in, 1 frame after the issue", async () => {
  const p = gpuProbe(), o = ocean({ level: 2 }), r = p.record;
  p.heights = pattern(8, 2.5); // the pass wrote level 2 + height 0.5
  askAt(r, [[100, 40], [104, 40], [102, 38], [102, 42]]);
  p.draw(o, 1.03); // the first ask builds the pass; its pipeline is ready a moment later
  assert.equal(p.copies.length, 0, "nothing is issued until the pipeline is ready");
  await settle();
  const seq0 = r[SEQ];
  p.draw(o, 1.04); // the issue: the copy is encoded; the record then tags it with the frame's Seq and starts mapping it
  const issued = r[SEQ];
  assert.equal(issued, seq0 + 1);
  assert.equal(p.copies.length, 1, "one copy");
  assert.equal(r[P_TICK], 0, "no reading yet");
  await settle(); // the GPU finished and the map resolved
  p.draw(o, 1.06); // this frame's record delivers it
  assert.equal(r[P_TICK], 1, "one reading");
  assert.deepEqual([r[P_SEQ], r[P_N], r[P_FLAGS]], [issued, 4, VALID], "tagged with the frame it was issued in");
  assert.equal(r[P_GEN], r[9], "the generation of the graph that drew it");
  assert.equal(r[P_LAT], r[SEQ] - issued);
  assert.equal(r[P_LAT], 1, "issued in frame N, published in the record of frame N+1");
  assert.equal(r[ECHO], r[REQ], "the reading names the request whose points it answers");
  close(r[OUT], 2.5, "exactly what the shader wrote: level 2 + height 0.5"); close(r[OUT + 3], 3.5, "level + 1.5");
  close(r[OUT + 1], 0.1, "normal x"); close(r[OUT + 2], -0.2, "normal z");
  p.draw(o, 1.08);
  assert.equal(r[P_TICK], 1, "a reading is written once, not on every frame");
});

test("probe, WebGPU: a reading names the request it answers, even after the host moved on to new points", async () => {
  const p = gpuProbe(), o = ocean({ level: 0 }), r = p.record;
  askAt(r, [[1, 1]]); p.draw(o, 1.03); await settle();
  const first = r[REQ];
  p.draw(o, 1.04); askAt(r, [[2, 2]]); // the host moved its points after the issue
  await settle(); p.draw(o, 1.06);
  assert.equal(r[ECHO], first, "the reading is for the first request");
  assert.equal(r[REQ], first + 1);
});

test("probe, WebGPU: nothing is issued while no host asks, and stopping stops the readback", async () => {
  const p = gpuProbe(), o = ocean({ level: 2 });
  for (let i = 0; i < 5; i++) p.draw(o, 1.04 + i * 0.02);
  await settle();
  assert.equal(p.copies.length, 0, "no probe copy without a host that asked");
  assert.equal(p.resources.fft.runner.value("probeCount"), 0, "no points fed to the graph");
  askAt(p.record, [[1, 1]]); p.draw(o, 1.2); await settle(); p.draw(o, 1.21);
  assert.equal(p.copies.length, 1);
  p.record[COUNT] = 0; p.draw(o, 1.22); p.draw(o, 1.24);
  assert.equal(p.copies.length, 1, "count 0 ends the readback");
});

test("probe, WebGPU: the pass samples the same textures as the surface, and a GoSX vessel's own probe is untouched", async () => {
  const p = gpuProbe(), o = ocean({ level: 0 });
  p.api.probe.begin(); p.api.probe.sample(5, 5, 0, 5); // a GoSX vessel asks through the graph's own kernel
  askAt(p.record, [[1, 2], [3, 4]]);
  p.draw(o, 1.03); await settle();
  const before = p.copies.length;
  p.draw(o, 1.04);
  assert.equal(p.resources.fft.runner.value("probeCount"), 1, "the vessel's point goes to the graph's probe");
  assert.equal(p.copies.length - before, 2, "the vessel's readback and the host's are separate copies");
  const wgsl = (p.fake.state.shaderModules || []).map((m) => m.code || (m.descriptor && m.descriptor.code) || "").find((c) => /fn main\(@builtin/.test(c)) || "";
  assert.match(wgsl, /textureSampleLevel\(d0, smp, uv, 0.0\)/);
  assert.match(wgsl, /textureSampleLevel\(s3, smp, uv, 0.0\)/, "all four cascades' displacement and slope textures");
  assert.doesNotMatch(wgsl, /var e = ev\(q\)/, "no wasted first evaluation");
});

test("probe, WebGPU: the bind group is made once, and again only when a texture changes", async () => {
  const p = gpuProbe(), o = ocean({ level: 0 });
  let groups = 0; const make = p.fake.device.createBindGroup.bind(p.fake.device);
  p.fake.device.createBindGroup = (d) => { if (/probe/.test(d.label || "")) groups++; return make(d); };
  askAt(p.record, [[1, 2]]); p.draw(o, 1.03); await settle();
  for (let i = 0; i < 4; i++) { p.draw(o, 1.04 + i * 0.02); await settle(); }
  assert.equal(groups, 1);
});

test("probe, WebGPU: a pass that cannot be built tells the host once that it cannot answer, and leaves the ocean alone", async () => {
  const p = gpuProbe(), o = ocean({ level: 0 });
  p.fake.device.createComputePipeline = (d) => { if (/gosx-probe-ocean/.test(d.label || "")) throw new Error("pipeline rejected"); return {}; };
  askAt(p.record, [[1, 2]]);
  p.draw(o, 1.03); await settle(); p.draw(o, 1.04);
  assert.deepEqual([p.record[P_FLAGS], p.record[P_N], p.record[P_TICK]], [UNAVAILABLE, 0, 1]);
  p.draw(o, 1.06);
  assert.equal(p.record[P_TICK], 1, "once per reason");
  assert.deepEqual(p.failures, [], "the renderer's pass guard heard nothing: the spectral ocean is still drawn");
  assert.ok(p.resources.fft && !p.resources.fft.disposed);
  assert.doesNotMatch("gosx-probe-ocean", /^gosx-ocean-fft-/, "the pipeline's label cannot match the ocean's own guard");
});

test("probe, WebGPU: a probe issued in a frame the renderer did not submit is never delivered, and its staging buffer is never mapped", async () => {
  const p = gpuProbe(), o = ocean({ level: 0 }), r = p.record;
  askAt(r, [[1, 1]]); p.draw(o, 1.03); await settle();
  p.draw(o, 1.04); await settle(); p.draw(o, 1.06);
  assert.equal(r[P_TICK], 1);
  r[COUNT] = 0; await settle(); p.draw(o, 1.07); await settle(); p.draw(o, 1.08); // everything issued so far finishes and is delivered
  const ticks = r[P_TICK], seq = r[P_SEQ];
  let maps = 0; for (const b of p.fake.state.buffers) { const m = b.mapAsync && b.mapAsync.bind(b); if (m && (b.usage & 1) && b.size === 64 * 16) b.mapAsync = (...a) => { maps++; return m(...a); }; }
  askAt(r, [[2, 2]]); p.update(o, 1.1); // issued, but the frame was not submitted: no record
  r[COUNT] = 0;
  await settle(); p.draw(o, 1.12); await settle(); p.draw(o, 1.14);
  assert.equal(r[P_TICK], ticks, "no new reading");
  assert.equal(r[P_SEQ], seq, "and nothing is re-tagged with a later frame's Seq");
  assert.equal(maps, 0, "the unsubmitted copy's buffer was never mapped");
});

test("probe: a torn-down graph forgets its probe: a later graph starts clean and never answers a stale request", async () => {
  const p = gpuProbe(), o = ocean({ level: 0 }), r = p.record;
  askAt(r, [[1, 1]]); p.draw(o, 1.03); await settle(); p.draw(o, 1.04); // issued, not yet delivered
  const oldGen = r[9], ticks = r[P_TICK];
  p.resources.fft.dispose(); p.resources.fft = null;
  p.update(o, 1.06); p.update(o, 1.08); // a new graph runs with no record: nothing is read back for a host that is no longer there
  assert.equal(p.copies.length, 1, "no probe issued without a record");
  await settle(); p.draw(o, 1.1);
  assert.equal(r[P_TICK], ticks, "the old graph's pending read was dropped with it");
  assert.notEqual(r[9], oldGen, "the new graph has its own generation");
});

// WebGL2: a recording double with a pixel buffer and a fence the test controls.
class ProbeGL extends RecordingGL {
  constructor() {
    super();
    Object.assign(this, { SYNC_GPU_COMMANDS_COMPLETE: 0x9117, ALREADY_SIGNALED: 0x911A, CONDITION_SATISFIED: 0x911C, TIMEOUT_EXPIRED: 0x911B, WAIT_FAILED: 0x911D, STREAM_READ: 0x88E1 });
    this.fences = []; this.reads = []; this.signaled = true; this.status = null; this.pattern = pattern(8); this.waits = 0; this.levels = [];
  }
  fenceSync() { const f = { id: this.fences.length, deleted: false }; this.fences.push(f); return f; }
  clientWaitSync() { this.waits++; return this.status !== null ? this.status : this.signaled ? this.ALREADY_SIGNALED : this.TIMEOUT_EXPIRED; }
  deleteSync(f) { f.deleted = true; }
  flush() {}
  getBufferSubData(target, offset, dst) { this.reads.push(dst); dst.set(this.pattern); }
  uniform1f(loc, v) { if (loc && loc.name === "level") this.levels.push(v); }
}
function probeWebGL() {
  const context = page(), gl = new ProbeGL(), mount = { attrs: {}, setAttribute(k, v) { this.attrs[k] = v; } };
  const api = context.window.__gosx_scene3d_ocean_fft_gl_api, resources = {}, textureCache = { _sceneTextureEpoch: 0 };
  const record = new Float64Array(340); mount.__gosxOceanWater = record;
  const p = { context, gl, mount, record, resources, fft: context.window.__gosx_scene3d_ocean_fft_api };
  p.update = (o, time) => o.spectrum ? api.run(resources, gl, { ocean: o, tier: "high", time, camera: { x: 0, y: 30, z: 0 }, textureCache, mount, caps: floatCaps }) : null;
  p.draw = (o, time, outcome = "submitted") => {
    const eng = p.update(o, time);
    if (!eng && resources.fft) { resources.fft.dispose(); resources.fft = null; }
    context.recordFrame(mount, bundle(o), time, outcome);
    return eng;
  };
  p.probeDraws = () => gl.draws.filter((d) => d.kind === "draw" && d.program && d.program.shaders.some((sh) => /uniform vec4 pts\[/.test(sh.source || "")));
  return p;
}

test("probe, WebGL2: a host that asks gets the shader's heights as written through a float target, a pixel buffer and a fence that is never waited on, 1 frame after the issue", () => {
  const p = probeWebGL(), o = ocean({ level: 2 }), r = p.record;
  p.gl.pattern = pattern(8, 2.5);
  p.draw(o, 1); p.draw(o, 1.02);
  askAt(r, [[100, 40], [104, 40], [102, 38], [102, 42]]);
  p.draw(o, 1.04); // the issue
  const issued = r[SEQ];
  assert.equal(p.probeDraws().length, 1, "one probe pass");
  assert.equal(p.gl.fences.length, 1);
  assert.equal(p.gl.ops.filter((x) => x[0] === "readPixels").length, 1);
  assert.deepEqual(p.gl.levels, [2], "the shader adds the ocean level; the page does not");
  const sources = p.probeDraws()[0].program.shaders.map((x) => x.source).join("\n");
  assert.match(sources, /precision highp sampler2D;/);
  assert.match(sources, /textureLod\(d0/); assert.match(sources, /textureLod\(s3/, "all four cascades' displacement and slope textures");
  assert.equal(p.probeDraws()[0].viewport[2], 4, "one texel per point");
  assert.equal(p.probeDraws()[0].fbo.attachments[0].internal, p.gl.RGBA32F, "a float target");
  assert.ok(probeShaders(p.gl).length === 1 && probeShaders(p.gl)[0].shaders.every((x) => x.deleted), "the shader objects are deleted once the program is linked");
  askAt(r, []); // one read
  assert.equal(r[P_TICK], 0, "nothing yet in the frame that issued it");
  p.draw(o, 1.06); // its update polls the fence, finds it signalled, and reads the buffer; its record delivers the reading
  assert.equal(r[P_TICK], 1);
  assert.deepEqual([r[P_SEQ], r[P_N], r[P_FLAGS]], [issued, 4, VALID]);
  assert.equal(r[P_LAT], r[SEQ] - issued);
  assert.equal(r[P_LAT], 1, "issued in frame N, published in the record of frame N+1");
  close(r[OUT], 2.5, "exactly what the shader wrote"); close(r[OUT + 3], 3.5, "level + 1.5"); close(r[OUT + 1], 0.1, "normal x");
  assert.ok(p.gl.fences.every((f) => f.deleted), "fences are released");
});

test("probe, WebGL2: a fence that has not signalled is never waited on; the frame goes on and the reading arrives when it signals", () => {
  const p = probeWebGL(), o = ocean({ level: 0 }), r = p.record;
  p.draw(o, 1); p.draw(o, 1.02);
  p.gl.signaled = false; askAt(r, [[1, 2]]);
  p.draw(o, 1.04); const issued = r[SEQ]; askAt(r, []);
  for (let i = 0; i < 4; i++) p.draw(o, 1.06 + i * 0.02);
  assert.equal(r[P_TICK], 0, "no reading while the fence is pending");
  assert.ok(p.gl.waits >= 4 && p.gl.reads.length === 0, "polled with a zero timeout each frame, nothing read");
  p.gl.signaled = true; p.draw(o, 1.2);
  assert.equal(r[P_TICK], 1);
  assert.equal(r[P_SEQ], issued);
  assert.equal(r[P_LAT], r[SEQ] - issued, "the age says how late it is: 5 frames here");
  assert.equal(r[P_LAT], 5);
});

test("probe, WebGL2: no pass, fence or readPixels is issued while no host asks, and none after it stops", () => {
  const p = probeWebGL(), o = ocean({ level: 0 });
  for (let i = 0; i < 5; i++) p.draw(o, 1 + i * 0.02);
  assert.equal(p.probeDraws().length, 0);
  assert.equal(p.gl.fences.length, 0);
  assert.equal(p.gl.ops.filter((x) => x[0] === "readPixels").length, 0);
  assert.equal(probeShaders(p.gl).length, 0, "the probe program is not even built");
  askAt(p.record, [[1, 1]]); p.draw(o, 1.2);
  assert.equal(p.probeDraws().length, 1);
  p.record[COUNT] = 0; p.draw(o, 1.22); p.draw(o, 1.24);
  assert.equal(p.probeDraws().length, 1);
});

test("probe, WebGL2: the draw leaves the GL state as the renderer had it, and a failed build is reported once as unavailable and leaves no program behind", () => {
  const p = probeWebGL(), o = ocean({ level: 0 });
  p.draw(o, 1); p.draw(o, 1.02);
  const before = [p.gl.drawFB, p.gl.currentProgram, p.gl.viewportRect.join(), p.gl.enabled.has(p.gl.BLEND)];
  askAt(p.record, [[1, 1]]);
  p.draw(o, 1.04);
  assert.deepEqual([p.gl.drawFB, p.gl.currentProgram, p.gl.viewportRect.join(), p.gl.enabled.has(p.gl.BLEND)], before, "framebuffer, program, viewport and blend are restored");
  const q = probeWebGL();
  q.gl.rejectShader = (src) => /uniform vec4 pts\[/.test(src);
  q.draw(o, 1); q.draw(o, 1.02);
  askAt(q.record, [[1, 1]]);
  q.draw(o, 1.04);
  assert.deepEqual([q.record[P_FLAGS], q.record[P_N], q.record[P_TICK]], [UNAVAILABLE, 0, 1]);
  q.draw(o, 1.06);
  assert.equal(q.record[P_TICK], 1, "told once");
  const last = q.gl.created.program.at(-1);
  assert.ok(last.deleted, "the failed build's program is deleted");
  assert.ok(q.gl.created.shader.slice(-2).every((sh) => sh.deleted), "and so are its shaders");
});

test("probe, WebGL2: a failed fence is released and reported as unavailable", () => {
  const p = probeWebGL(), o = ocean({ level: 0 }), r = p.record;
  p.draw(o, 1); p.draw(o, 1.02);
  askAt(r, [[1, 1]]); p.draw(o, 1.04);
  p.gl.status = p.gl.WAIT_FAILED; p.draw(o, 1.06);
  assert.equal(r[P_FLAGS], UNAVAILABLE);
  assert.ok(p.gl.fences.every((f) => f.deleted), "the fence is deleted");
});

test("probe, WebGL2: a failure, a pending read and the host's record do not outlive the graph", () => {
  const p = probeWebGL(), o = ocean({ level: 0 }), r = p.record;
  p.draw(o, 1); p.draw(o, 1.02);
  p.gl.status = p.gl.WAIT_FAILED; askAt(r, [[1, 1]]); p.draw(o, 1.04); p.draw(o, 1.06);
  assert.equal(r[P_FLAGS], UNAVAILABLE);
  p.resources.fft.dispose(); p.resources.fft = null; p.gl.status = null;
  const draws = p.probeDraws().length;
  p.update(o, 1.08); p.update(o, 1.1); // the new graph runs with no record: nothing is asked of the GPU for a host that is gone
  assert.equal(p.probeDraws().length, draws, "the record is forgotten with the graph");
  askAt(r, [[1, 1]]); r[P_TICK] = 0; p.draw(o, 1.12); p.draw(o, 1.14); p.draw(o, 1.16);
  assert.equal(r[P_FLAGS], VALID, "the old failure did not stick to the new graph");
});

test("probe, WebGL2: a probe issued in a frame the renderer did not submit is dropped", () => {
  const p = probeWebGL(), o = ocean({ level: 0 }), r = p.record;
  p.draw(o, 1); p.draw(o, 1.02);
  askAt(r, [[1, 1]]); p.draw(o, 1.04, "empty"); // issued, not submitted
  askAt(r, []);
  p.draw(o, 1.06); p.draw(o, 1.08);
  assert.equal(r[P_TICK], 0, "never delivered, and never tagged with the next frame's Seq");
});

test("probe, Gerstner: the CPU query answers in the same frame, exact, with the Gerstner flag, the tier the frame draws, and the level once", () => {
  const context = page({ chunk: false });
  loadSources(context, ["ocean-waves.ts", "ocean-query.ts"]);
  const q = context.window.__gosx_scene3d_ocean_query;
  const mount = { __gosxOceanWater: new Float64Array(340) }, o = { level: 1, waveHeight: 0.8, waveLength: 18, choppiness: 0.6, speed: 1, surf: 0.5 };
  askAt(mount.__gosxOceanWater, [[10, 20], [-5, 7]]);
  context.recordFrame(mount, bundle(o), 33.25, "submitted");
  const r = mount.__gosxOceanWater;
  assert.deepEqual([r[P_SEQ], r[P_N], r[P_FLAGS], r[P_LAT], r[9]], [1, 2, VALID + GERSTNER, 0, 0]);
  assert.equal(r[ECHO], r[REQ]);
  const want = q.sample(q.create(o, "high", null, true), 10, 20, 33.25);
  assert.equal(r[OUT], want.y, "the level is in the height once"); assert.equal(r[OUT + 1], want.normal.x); assert.equal(r[OUT + 2], want.normal.z);
  context.sceneLowHardware = () => true; // the renderer draws four waves
  context.recordFrame(mount, bundle(o), 33.25, "submitted");
  const low = q.sample(q.create(o, "low", null, true), 10, 20, 33.25);
  assert.equal(r[OUT], low.y, "the low tier's four waves");
  assert.notEqual(low.y, want.y);
  askAt(r, []);
  r[P_TICK] = 0; context.recordFrame(mount, bundle(o), 34, "submitted");
  assert.equal(r[P_TICK], 0, "no points, no work");
});

test("probe: a spectral reading and a Gerstner one at the same level agree on what 'level' means", () => {
  // The spectral heights are what the shaders write, with the level in them; the Gerstner heights come from a query whose base is the level.
  const context = page({ chunk: false });
  loadSources(context, ["ocean-waves.ts", "ocean-query.ts"]);
  const q = context.window.__gosx_scene3d_ocean_query, flat = q.sample(q.create({ level: 2, waveHeight: 0, waveLength: 18, choppiness: 0, speed: 1, surf: 0 }, "high", null, true), 5, 5, 0);
  assert.ok(Math.abs(flat.y - 2) < 0.2, "a calm Gerstner sea at level 2 reads about 2, not 4: " + flat.y);
});
