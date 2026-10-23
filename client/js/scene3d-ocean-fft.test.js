"use strict";
// Spectral (FFT) ocean on WebGPU: the glue between the scene, the Elio graph runner and the surface shader, and the renderer hooks.
// Runs against the repository's fake WebGPU device; the real-GPU frames come from scripts/scene3d-ocean-spectrum.mjs.
const test = require("node:test"), assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { execFileSync } = require("node:child_process");
const { createRequire } = require("node:module");
const ts = createRequire(path.join(__dirname, "..", "runtime", "package.json"))("typescript");
const { createBoardWebGPUHarness, makeFakeGPUDevice, makePointsBundle } = require("./runtime-test-harness.js");

const scene3d = path.join(__dirname, "..", "runtime", "scene3d");
const CHUNK_FILES = ["ocean-fft-kernels.ts", "elio-graph.ts", "elio-graph-gather.ts", "elio-graph-compute.ts", "ocean-fft-surface.ts", "ocean-fft-surface-gl.ts", "ocean-fft.ts"];
const transpile = (file) => ts.transpileModule(fs.readFileSync(path.join(scene3d, file), "utf8"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;

function loadChunk(context) {
  context.GPUBufferUsage ||= { MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512 };
  context.GPUTextureUsage ||= { COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16 };
  context.GPUShaderStage ||= { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
  context.window ||= context;
  for (const file of CHUNK_FILES) vm.runInContext(transpile(file), context, { filename: file });
  return context.window.__gosx_scene3d_ocean_fft_api;
}

function standalone() {
  const context = vm.createContext({ Math, Number, Object, Array, Float32Array, Uint32Array, Uint8Array, ArrayBuffer, DataView, Error, Set, Map, JSON, Promise, console });
  const api = loadChunk(context);
  const fake = makeFakeGPUDevice();
  // Pipelines become ready when the test says so, as after asynchronous validation.
  const validation = new Map();
  const host = (extra) => Object.assign({
    device: fake.device, validation, guard: { disabled: new Set(), failures: [], fail(label, message) { this.failures.push({ label, message }); this.disabled.add(/^gosx-elio-/.test(label) ? "elioGraph" : "oceanFFT"); } },
    environment: null, camera: { x: 0, y: 30, z: 0 }, time: 0, tier: "high",
    create(device, kind, descriptor) { const handle = { label: descriptor.label }; validation.set(handle, { ready: false, label: descriptor.label }); return handle; },
  }, extra);
  const settle = () => { for (const state of validation.values()) state.ready = true; };
  return { api, fake, host, validation, settle, context, graphs: context.window.__gosx_scene3d_elio_ocean.graphs, G: context.window.__gosx_elio_graph };
}
const ocean = (extra) => Object.assign({ level: 0, choppiness: 0.6, windDirection: 0, spectrum: { windSpeed: 9 } }, extra);
const frame = (s, resources, h, env, extra) => s.api.frame(resources, Object.assign({}, h, { environment: { ocean: env }, encoder: h.encoder || h.device.createCommandEncoder() }, extra));
// The graph's pipelines are created by the first frame and become ready afterwards, as after asynchronous validation.
const warm = (s, resources, h, env, extra) => { frame(s, resources, h, env, extra); s.settle(); return frame(s, resources, h, env, extra); };
const surfaceWrites = (s) => s.fake.state.writeBufferCalls.filter((w) => w.buffer.size === 160);
const lastSurface = (s) => { const w = surfaceWrites(s).pop(); return w ? Array.from(new Float32Array(w.data.buffer || w.data, w.data.byteOffset || 0, 40)) : null; };
const labels = (pass) => pass.dispatches.map((d) => d.pipeline.label);

test("frame() does nothing without a spectrum and tears down when the spectrum goes away", () => {
  const s = standalone(), resources = {}, h = s.host();
  assert.equal(frame(s, resources, h, { level: 0 }), null);
  assert.equal(s.fake.state.buffers.length, 0);
  frame(s, resources, h, ocean());
  assert.ok(resources.fft && s.fake.state.buffers.length > 0);
  const owned = [...s.fake.state.buffers, ...s.fake.state.textures];
  assert.equal(frame(s, resources, h, { level: 0 }), null);
  assert.equal(resources.fft, null);
  assert.ok(owned.every((r) => r.destroyed), "every buffer and texture is released");
  assert.equal(s.api.water(), null, "no water frame once the graph is gone");
});

test("nothing is encoded until every pipeline has validated; then the graph's kernel passes run in graph order in the frame encoder", () => {
  const s = standalone(), resources = {}, h = s.host();
  frame(s, resources, h, ocean());
  assert.equal(resources.fft.ready, false);
  assert.equal(s.fake.state.computePasses.length, 0, "pipelines pending: the Gerstner ocean keeps drawing");
  s.settle();
  frame(s, resources, h, ocean());
  assert.equal(resources.fft.ready, true);
  const passes = s.fake.state.computePasses;
  assert.ok(passes.length > 40 && passes.every((p) => p.ended));
  assert.ok(passes.every((p) => /^gosx-elio-ocean-/.test(labels(p)[0])), "labelled gosx-elio-<graph>-<kernel>");
  const kernels = passes.map((p) => labels(p)[0].replace("gosx-elio-ocean-", ""));
  const expected = Array.from(s.G.expand(s.graphs["webgpu/high"])).filter((p) => p.type === "kernel").map((p) => p.kernel);
  assert.deepEqual(kernels, expected, "one compute pass per kernel pass, in order (every changed: pass runs on the first frame)");
  assert.ok(kernels.indexOf("ocean_spectrum") < kernels.indexOf("ocean_evolve") && kernels.lastIndexOf("ocean_assemble") < kernels.indexOf("ocean_foam"));
  const before = passes.length;
  frame(s, resources, h, ocean(), { time: 1 / 60 });
  const second = s.fake.state.computePasses.slice(before).map((p) => labels(p)[0].replace("gosx-elio-ocean-", ""));
  assert.ok(!second.includes("ocean_spectrum"), "an unchanged spectrum does not rerun ocean_spectrum");
  assert.ok(second.includes("ocean_evolve") && second.includes("ocean_foam"));
});

test("graphParams reach the graph's uniforms; graphTween overrides the graph's tween; the app's setting wins", () => {
  const s = standalone(), resources = {}, h = s.host();
  s.settle();
  const spectrumSeed = () => {
    const fields = s.context.window.__gosx_scene3d_elio_ocean.kernels.oceanSpectrum.uniforms[0].fields;
    const seed = fields.find((f) => f.name === "seed").offset, wind = fields.find((f) => f.name === "windSpeed").offset;
    const writes = s.fake.state.writeBufferCalls.filter((w) => w.buffer.size === 64 && new DataView(w.data.buffer || w.data, w.data.byteOffset || 0).getUint32(0, true) === 64);
    const v = new DataView(writes[writes.length - 1].data.buffer || writes[writes.length - 1].data, writes[writes.length - 1].data.byteOffset || 0);
    return [v.getUint32(seed, true), v.getFloat32(wind, true)];
  };
  warm(s, resources, h, ocean({ graphParams: { seed: 7, windSpeed: 9, lambda: 0.6, foamThreshold: 0.25 }, graphTween: 10 }), { time: 100 });
  assert.deepEqual(spectrumSeed(), [7, 9]);
  const runner = resources.fft.runner;
  assert.equal(runner.value("foamThreshold"), 0.25);
  // The sea moves to 29 m/s over graphTween = 10 s of presentation time, not the graph's 20 s.
  const to = ocean({ graphParams: { seed: 7, windSpeed: 29, lambda: 0.6, foamThreshold: 0.25 }, graphTween: 10 });
  frame(s, resources, h, to, { time: 101 });
  assert.equal(runner.value("windSpeed"), 9, "the tween clock starts on the first frame after the update");
  frame(s, resources, h, to, { time: 106 });
  assert.ok(Math.abs(runner.value("windSpeed") - 19) < 1e-9, "half way after 5 of 10 seconds: " + runner.value("windSpeed"));
  frame(s, resources, h, to, { time: 111 });
  assert.equal(runner.value("windSpeed"), 29);
  assert.equal(spectrumSeed()[1], 29, "the spectrum pass reran with the arriving value");
  // graphTween 0: a weather change that must not blend.
  const snap = ocean({ graphParams: { seed: 7, windSpeed: 5, lambda: 0.6, foamThreshold: 0.25 }, graphTween: 0 });
  frame(s, resources, h, snap, { time: 112 });
  frame(s, resources, h, snap, { time: 112.1 });
  assert.equal(runner.value("windSpeed"), 5);
});

test("the low tier draws three cascades; the surface uniform carries tile, band centre, wrapped water offsets and the foam window", () => {
  const s = standalone(), resources = {}, h = s.host({ tier: "low" });
  const env = ocean({ graphParams: { windSpeed: 12, driftX: 3.3, driftZ: -1.7, foamThreshold: 0.4 }, graphTween: 0 });
  warm(s, resources, h, env, { time: 100 });
  for (let i = 1; i < 61; i++) frame(s, resources, h, env, { time: 100 + i / 60 });
  const d = lastSurface(s);
  const graph = s.graphs["webgpu/low"], info = s.api.describe(graph);
  assert.equal(info.cascades.length, 3);
  assert.deepEqual([d[3], d[7], d[11], d[15]], [1, 1, 1, 0], "three cascades enabled, the fourth bound to the dummy texture");
  assert.deepEqual([d[0], d[4], d[8]], Array.from(info.cascades, (c) => c.tile));
  assert.ok(Math.abs(d[1] - Math.sqrt(info.cascades[0].kMin * info.cascades[0].kMax)) < 1e-6);
  // One second of drift at 3.3 and -1.7 m/s: the offsets wrapped by each tile (floored), in float64 until the upload.
  const wx = 3.3 * 1, wz = -1.7 * 1, wrap = (v, m) => v - Math.floor(v / m) * m;
  info.cascades.forEach((c, i) => {
    assert.ok(Math.abs(d[24 + i * 4] - wrap(wx, c.tile)) < 1e-4, `cascade ${i} x: ${d[24 + i * 4]} vs ${wrap(wx, c.tile)}`);
    assert.ok(Math.abs(d[25 + i * 4] - wrap(wz, c.tile)) < 1e-4);
  });
  assert.ok(d[25] >= 0 && d[25] < info.cascades[0].tile, "wrapped offsets are never negative");
  assert.ok(Math.abs(d[16] - 1 / (info.foam.cell * info.foam.width)) < 1e-9);
  assert.equal(Math.fround(d[18]), Math.fround(0.4), "the foam threshold is the graph parameter, not a JS constant");
  const water = s.api.water();
  assert.ok(Math.abs(water.x - 3.3) < 1e-6 && Math.abs(water.z + 1.7) < 1e-6 && water.time > 0.9 && water.time < 1.1, JSON.stringify(water));
  const high = standalone(), r2 = {}, h2 = high.host();
  warm(high, r2, h2, env, { time: 1 }); frame(high, r2, h2, env, { time: 1.02 });
  assert.deepEqual([lastSurface(high)[3], lastSurface(high)[15]], [1, 1], "the high tier enables all four");
});

test("a graph that fails after its first frame stops the ocean: not ready, the guard hears of it, and the Gerstner ocean takes over", () => {
  const warn = console.warn; console.warn = () => {};
  try {
    const s = standalone(), resources = {}, h = s.host();
    warm(s, resources, h, ocean(), { time: 1 });
    assert.equal(resources.fft.ready, true);
    const device = h.device, begin = device.createCommandEncoder.bind(device);
    const encoder = begin();
    encoder.beginComputePass = () => { throw new Error("device error"); };
    frame(s, resources, h, ocean(), { time: 2, encoder });
    assert.equal(resources.fft.ready, false);
    assert.equal(resources.fft.group, null);
    assert.ok(h.guard.failures.some((f) => /^gosx-elio-ocean/.test(f.label)), JSON.stringify(h.guard.failures));
    assert.equal(frame(s, resources, h, ocean(), { time: 3 }), null, "the guard disabled the graph pass");
    assert.equal(resources.fft, null);
  } finally { console.warn = warn; }
});

// --- the probe: points in, a ring of three staging buffers, readback out ---------------------------------------------

// A fake device whose staging buffers can be mapped, and whose copyBufferToBuffer writes a pattern the test controls.
function probeRig() {
  const s = standalone();
  const fake = makeFakeGPUDevice({ timestampQuery: true });
  s.fake = fake; s.host = ((orig) => (extra) => orig(Object.assign({ device: fake.device }, extra)))(s.host);
  s.copies = [];
  s.pattern = (count) => { const a = new Float32Array(64 * 4); for (let i = 0; i < count; i++) { a[i * 4] = 0.5 + i; a[i * 4 + 1] = 0.1; a[i * 4 + 2] = -0.2; a[i * 4 + 3] = 3; } return a; };
  s.heights = s.pattern(4);
  const create = fake.device.createCommandEncoder.bind(fake.device);
  fake.device.createCommandEncoder = () => {
    const encoder = create();
    encoder.copyBufferToBuffer = (source, so, destination, doff, size) => { if (size !== 64 * 16) return; s.copies.push({ source, destination, size }); new Float32Array(destination._backing).set(s.heights); }; // the graph's own timestamp copies are 16 bytes
    return encoder;
  };
  return s;
}
const settled = () => new Promise((resolve) => setTimeout(resolve, 0));

test("probe: the points a vessel asks about reach the graph relative to their centroid, and the output is copied into a staging buffer", async () => {
  const s = probeRig(), resources = {}, h = s.host({ now: () => 1000 }), probe = s.api.probe;
  const env = ocean({ level: 2, graphParams: { windSpeed: 9 } });
  warm(s, resources, h, env, { time: 1 });
  probe.begin();
  const pts = [[100, 40], [104, 40], [102, 38], [102, 42]];
  for (const [x, z] of pts) assert.equal(probe.sample(x, z, 2, 1000), null, "no readback yet: the caller uses the swell waves");
  frame(s, resources, h, env, { time: 1.02 });
  const runner = resources.fft.runner;
  assert.equal(runner.value("probeCount"), 4);
  assert.deepEqual([runner.value("probeOriginX"), runner.value("probeOriginZ")], [102, 40]);
  assert.equal(s.copies.length, 1, "one copy of the probe output per frame");
  assert.equal(s.copies[0].size, 64 * 16);
  assert.ok(s.copies[0].source.size >= 64 * 16, "the graph's probeOut buffer");
  assert.equal(s.fake.state.writeBufferCalls.filter((w) => w.buffer.size === 64 * 16).length > 0, true, "probePoints are uploaded");
  const fed = s.fake.state.writeBufferCalls.filter((w) => w.buffer.size === 64 * 16).pop();
  assert.deepEqual(Array.from(new Float32Array(fed.data.buffer || fed.data, fed.data.byteOffset || 0, 4)), [-2, 0, 0, 0], "point 0 relative to the centroid (102, 40)");
});

test("probe: a copy is polled and mapped on a later frame, never awaited; the answer is level + height, extrapolated with the velocity of the two latest readbacks, and stale after 250 ms", async () => {
  const s = probeRig(), resources = {}; let now = 1000;
  const h = s.host({ now: () => now }), probe = s.api.probe;
  const env = ocean({ level: 2, graphParams: {} });
  warm(s, resources, h, env, { time: 1 });
  const askAll = () => { probe.begin(); return [[100, 40], [104, 40], [102, 38], [102, 42]].map(([x, z]) => probe.sample(x, z, 2, now)); };
  askAll(); frame(s, resources, h, env, { time: 1.02 });
  assert.equal(askAll()[0], null, "the copy has not been mapped yet");
  frame(s, resources, h, env, { time: 1.04 });
  await settled();
  let a = askAll();
  assert.ok(a[0], "after the map resolved the readback answers");
  assert.ok(Math.abs(a[0].y - (2 + 0.5)) < 1e-6 && Math.abs(a[1].y - (2 + 1.5)) < 1e-6, "level + height: " + a[0].y);
  assert.deepEqual([a[0].normal.x.toFixed(2), a[0].normal.z.toFixed(2)], ["0.10", "-0.20"]);
  assert.ok(Math.abs(Math.hypot(a[0].normal.x, a[0].normal.y, a[0].normal.z) - 1) < 1e-6);
  assert.equal(a[0].velocity.y, 0, "one readback gives no velocity");
  // A second readback 100 ms later with the water 0.4 m higher gives 4 m/s; 50 ms after it the answer leads it by 0.2 m.
  now = 1100; s.heights = s.pattern(4).map((v, i) => i % 4 === 0 && i < 16 ? v + 0.4 : v);
  frame(s, resources, h, env, { time: 1.06 }); frame(s, resources, h, env, { time: 1.08 });
  await settled();
  now = 1150;
  a = askAll();
  assert.ok(Math.abs(a[0].velocity.y - 4) < 1e-6, "velocity from two timestamped samples: " + a[0].velocity.y);
  assert.ok(Math.abs(a[0].y - (2 + 0.9 + 0.2)) < 1e-6, "extrapolated: " + a[0].y);
  now = 1100 + 251;
  assert.equal(askAll()[0], null, "after 250 ms the swell waves take over");
  // A vessel that jumped far from the sampled points gets no answer from a readback made elsewhere.
  now = 1150; probe.begin();
  assert.equal(probe.sample(5000, 5000, 2, now), null);
});

test("probe: the ring holds three staging buffers; with all three in flight the frame skips its copy; dispose releases them", async () => {
  const s = probeRig(), resources = {}, h = s.host({ now: () => 5 }), probe = s.api.probe;
  const env = ocean({ graphParams: {} });
  const stuck = [];
  const create = s.fake.device.createBuffer.bind(s.fake.device);
  s.fake.device.createBuffer = (d) => { const b = create(d); if ((d.usage & 1) && d.size === 64 * 16) b.mapAsync = () => new Promise(() => stuck.push(b)); return b; };
  warm(s, resources, h, env, { time: 1 });
  for (let i = 0; i < 6; i++) { probe.begin(); probe.sample(1, 1, 0, 5); frame(s, resources, h, env, { time: 1.02 + i * 0.02 }); }
  assert.equal(s.copies.length, 3, "a copy per free slot, then none: " + s.copies.length);
  assert.equal(new Set(s.copies.map((c) => c.destination)).size, 3);
  assert.equal(stuck.length, 3, "every copy was polled for mapping on a later frame");
  const staging = s.fake.state.buffers.filter((b) => (b.usage & 1) && b.size === 64 * 16);
  assert.equal(staging.length, 3);
  resources.fft.dispose();
  assert.ok(staging.every((b) => b.destroyed));
  assert.equal(probe.sample(1, 1, 0, 5), null, "a disposed graph leaves no stale answers");
});

test("sampler: CPU swell waves are evaluated in the water frame at the graph's wave time, and probed() falls back to them", () => {
  const s = standalone(), resources = {}, h = s.host();
  const calls = [], query = { sample: (q, x, z, t) => { calls.push([x, z, t]); return { x: x + 0.5, y: 1, z: z - 0.25 }; } };
  let r = s.api.sampler(query, { data: [3] }, 42).sample(10, 20);
  assert.deepEqual(calls.pop(), [10, 20, 42], "before the graph runs: the plain query at the scene's water clock");
  assert.deepEqual([r.x, r.z], [10.5, 19.75]);
  warm(s, resources, h, ocean({ graphParams: { driftX: 3.3, driftZ: -1.7 }, graphTween: 0 }), { time: 100 });
  for (let i = 1; i <= 60; i++) frame(s, resources, h, ocean({ graphParams: { driftX: 3.3, driftZ: -1.7 }, graphTween: 0 }), { time: 100 + i / 60 });
  const w = s.api.water();
  const sm = s.api.sampler(query, { data: [3] }, 42);
  r = sm.sample(10, 20);
  const c = calls.pop();
  assert.ok(Math.abs(c[0] - (10 - w.x)) < 1e-9 && Math.abs(c[1] - (20 - w.z)) < 1e-9 && c[2] === w.time, "sampled at (position - origin) at the graph's wave time: " + JSON.stringify(c));
  assert.ok(Math.abs(r.x - (10.5)) < 1e-9 && Math.abs(r.z - 19.75) < 1e-9, "the displaced position is back in world coordinates");
  assert.equal(sm.probed(10, 20).y, 1, "no probe answer: the swell waves");
  sm.begin();
  const gone = frame(s, resources, h, { level: 0 });
  assert.equal(gone, null);
  assert.equal(s.api.sampler(query, { data: [3] }, 7).sample(1, 2).x, 1.5);
  assert.deepEqual(calls.pop(), [1, 2, 7], "after the graph is gone the query is plain again");
});

test("timing(): the WebGPU graph's compute time is reported separately, and absent without timestamp-query", async () => {
  const plain = standalone(), r1 = {}, h1 = plain.host();
  warm(plain, r1, h1, ocean());
  assert.equal(plain.api.timing(), null, "no timestamp-query on the default fake device");
  const s = probeRig(), resources = {}, h = s.host();
  assert.equal(s.api.timing(), null, "no graph yet");
  warm(s, resources, h, ocean(), { time: 1 });
  assert.ok(s.api.timing(), "a graph on a device with timestamp-query has timing");
  assert.equal(s.api.timing().ms, null, "before the first readback");
  const rig = s.fake.state.computePasses.map((p) => p.descriptor.timestampWrites).filter(Boolean);
  assert.ok(rig.length >= 2, "the graph's compute passes carry the stamps");
  resources.fft.dispose();
  assert.equal(s.api.timing(), null, "gone with the graph");
});

test("graphNodes bind the graph's logical nodes to scene nodes; poses come from the node registry the vessel publishes", () => {
  const s = standalone();
  const G = s.G, module = s.context.window.__gosx_scene3d_elio_ocean;
  const graph = { name: "t", backend: "webgpu", tier: "high", version: 1, resources: [], passes: [], frameSwaps: [], exports: {},
    params: ["x", "z", "heading", "speed"].map((f) => ({ name: "hull_" + f, type: "f32", source: { kind: "node", node: "hull", field: f } })) };
  const runner = G.create({ module, backend: "webgpu", graph, host: { device: s.fake.device, create() { return {}; }, nodePose: s.api.nodePose } });
  assert.equal(runner.failed, null);
  const poses = s.context.window.__gosx_scene3d_node_poses = { ship: { x: 100, z: -40, heading: 2, speed: 7.5 } };
  s.api.feed(runner, { graphNodes: { hull: "ship" } });
  runner.run({ time: 1, dt: 0.016, encoder: s.fake.device.createCommandEncoder() });
  assert.deepEqual([runner.value("hull_x"), runner.value("hull_z"), runner.value("hull_heading"), runner.value("hull_speed")], [100, -40, 2, 7.5]);
  assert.equal(s.api.nodePose("nobody"), null);
  delete poses.ship;
  assert.equal(s.api.nodePose("ship"), null, "a node that stopped publishing is absent");
});

test("a hull's speed for the wake is its speed through the water: the node's velocity minus the water's drift", () => {
  const s = standalone();
  const graph = { name: "t", backend: "webgpu", tier: "high", version: 1, resources: [], passes: [], frameSwaps: [], exports: {},
    params: [{ name: "driftX", type: "f32", source: { kind: "value", value: 0 } }, { name: "driftZ", type: "f32", source: { kind: "value", value: 0 } },
      { name: "hullSpeed", type: "f32", source: { kind: "node", node: "hull", field: "speed" } }] };
  const module = s.context.window.__gosx_scene3d_elio_ocean;
  let runner;
  const host = { device: s.fake.device, create() { return {}; }, nodePose: (id) => s.api.waterRelative(s.api.nodePose(id), () => runner) };
  runner = s.G.create({ module, backend: "webgpu", graph, host });
  const poses = s.context.window.__gosx_scene3d_node_poses = {};
  const speedFor = (pose, drift) => {
    poses.ship = pose;
    s.api.feed(runner, { graphNodes: { hull: "ship" }, graphParams: { driftX: drift[0], driftZ: drift[1] }, graphTween: 0 });
    runner.run({ time: speedFor.t = (speedFor.t || 0) + 1, dt: 1, encoder: s.fake.device.createCommandEncoder() });
    runner.run({ time: speedFor.t += 1, dt: 1, encoder: s.fake.device.createCommandEncoder() });
    return runner.value("hullSpeed");
  };
  assert.ok(Math.abs(speedFor({ x: 0, z: 0, heading: 0, speed: 0 }, [0, 8.2]) - 8.2) < 1e-9, "a ship at rest in drifting water moves through it at the drift speed");
  assert.ok(Math.abs(speedFor({ x: 0, z: 0, heading: 0, speed: 5 }, [0, 3]) - 2) < 1e-9, "5 m/s ahead (+z) with 3 m/s of current the same way: 2 m/s through the water");
  assert.ok(Math.abs(speedFor({ x: 0, z: 0, heading: Math.PI / 2, speed: 6 }, [0, 0]) - 6) < 1e-9, "no drift: the node's own speed");
  assert.equal(s.api.waterRelative(null, () => runner), null);
});

function nagaPath() {
  for (const candidate of ["naga", path.join(process.env.HOME || "", ".cargo", "bin", "naga")]) {
    try { execFileSync(candidate, ["--version"], { stdio: "ignore" }); return candidate; } catch (_e) {}
  }
  return "";
}
const naga = nagaPath();
function validateWGSL(code, name) {
  const dir = fs.mkdtempSync(path.join(require("node:os").tmpdir(), "ocean-fft-"));
  const file = path.join(dir, name + ".wgsl");
  fs.writeFileSync(file, code);
  try { execFileSync(naga, [file], { stdio: "pipe" }); } finally { fs.rmSync(dir, { recursive: true, force: true }); }
}

test("every Elio kernel and the downsample pass the WGSL validator", { skip: naga ? false : "naga is not available" }, () => {
  const s = standalone();
  const kernels = s.context.window.__gosx_scene3d_elio_ocean.kernels;
  for (const name of ["oceanSpectrum", "oceanEvolve", "oceanAssemble", "oceanMip", "oceanFoam", "oceanProbe", "fftCompute64", "fftCompute128", "fftCompute256"]) validateWGSL(kernels[name].wgsl, name);
});

function sceneBundle(spectrum) {
  const b = makePointsBundle(null);
  b.points = [];
  b.environment.sky = { mode: "physical" };
  b.environment.ocean = { level: 0, extent: 4000, choppiness: 0.6, windDirection: 20, reflections: { mode: "ssr+planar", resolution: 0.5, strength: 0.8 } };
  if (spectrum) b.environment.ocean.spectrum = spectrum;
  b.camera.y = 30; b.camera.z = 0;
  return b;
}
async function rendererWithChunk(fresh = true) {
  const h = await createBoardWebGPUHarness({ fresh });
  const create = h.fake.device.createRenderPipeline.bind(h.fake.device);
  h.fake.device.createRenderPipeline = (d) => Object.assign(create(d), { getBindGroupLayout: () => h.fake.device.createBindGroupLayout({ entries: [] }) });
  loadChunk(h.env.context);
  return h;
}
async function renderSettled(h, bundle, frames = 6) {
  let status = "";
  for (let i = 0; i < frames; i++) {
    status = h.renderer.render(bundle, { width: 64, height: 64 }, { sceneTime: 3 + i });
    await new Promise((resolve) => setTimeout(resolve, 0));
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  return status;
}


test("the renderer encodes the ocean graph before the main pass and samples the cascades", async () => {
  const h = await rendererWithChunk();
  h.canvas.width = h.canvas.height = 64;
  const b = sceneBundle({ windSpeed: 12 });
  await renderSettled(h, b);
  const { state } = h.fake;
  const module = state.shaderModules.find((m) => m.label === "gosx-ocean-fft-surface");
  assert.ok(module, "the spectral surface variant is compiled");
  assert.match(module.code, /textureSampleLevel\(spDisp0/);
  assert.match(module.code, /sp\.w\[0\]\.xy/, "each cascade is sampled at its own wrapped water offset");
  assert.doesNotMatch(module.code, /for \(var i = 0; i < 6; i\+\+\) \{\n    if \(i >= waves\)/, "the Gerstner loop is replaced");
  if (naga) validateWGSL(module.code, "ocean-fft-surface");
  const pipeline = state.renderPipelines.find((p) => p.desc.label === "gosx-ocean-fft-surface");
  assert.ok(pipeline);
  assert.equal(pipeline.desc.layout.desc.bindGroupLayouts.length, 3, "ocean, reflection and spectral groups");
  assert.ok(state.renderPasses.find((p) => p.draws.some((d) => d.pipeline && d.pipeline.desc && d.pipeline.desc.label === "gosx-ocean-fft-surface")), "the main pass draws the spectral ocean");
  assert.ok(state.computePasses.some((p) => p.descriptor && /^gosx-elio-ocean-/.test(p.descriptor.label)));
  h.renderer.dispose();
});

test("encoding order inside one frame: the graph's compute passes precede the render passes", async () => {
  const h = await rendererWithChunk();
  h.canvas.width = h.canvas.height = 64;
  const events = [];
  const create = h.fake.device.createCommandEncoder.bind(h.fake.device);
  h.fake.device.createCommandEncoder = (...args) => {
    const encoder = create(...args), render = encoder.beginRenderPass.bind(encoder), compute = encoder.beginComputePass.bind(encoder);
    encoder.beginRenderPass = (d) => { events.push("render"); return render(d); };
    encoder.beginComputePass = (d) => { events.push((d && d.label) || "compute"); return compute(d); };
    encoder.finish = () => { events.push("finish"); return { __kind: "commandBuffer" }; };
    return encoder;
  };
  await renderSettled(h, sceneBundle({}), 8);
  const lastFrame = events.slice(events.lastIndexOf("finish", events.length - 2) + 1);
  const graph = lastFrame.findIndex((e) => /^gosx-elio-ocean-/.test(e)), render = lastFrame.indexOf("render");
  assert.ok(graph >= 0 && render > graph, "graph passes before the main pass: " + lastFrame.slice(0, 6).join(","));
  h.renderer.dispose();
});

test("scenes without a spectrum render exactly as before: no graph resources, Gerstner shader", async () => {
  const h = await rendererWithChunk();
  h.canvas.width = h.canvas.height = 64;
  await renderSettled(h, sceneBundle(null));
  const { state } = h.fake;
  assert.ok(state.shaderModules.some((m) => m.label === "gosx-ocean"));
  assert.ok(!state.shaderModules.some((m) => /ocean-fft|elio/.test(m.label || "")));
  assert.ok(!state.computePipelines.some((p) => /ocean-fft|elio/.test(p.label || "")));
  h.renderer.dispose();
});

test("a rejected graph pipeline disables the graph pass and the ocean falls back to Gerstner waves, not a core error", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const createRender = h.fake.device.createRenderPipeline.bind(h.fake.device);
  h.fake.device.createRenderPipeline = (d) => Object.assign(createRender(d), { getBindGroupLayout: () => h.fake.device.createBindGroupLayout({ entries: [] }) });
  const createCompute = h.fake.device.createComputePipeline.bind(h.fake.device);
  h.fake.device.createComputePipeline = (d) => { if (/gosx-elio-ocean-ocean_foam/.test(d.label)) throw new Error("injected validation failure"); return createCompute(d); };
  loadChunk(h.env.context);
  h.canvas.width = h.canvas.height = 64;
  const status = await renderSettled(h, sceneBundle({}), 10);
  assert.notEqual(status, "failed");
  const failure = JSON.parse(h.mount.getAttribute("data-gosx-scene3d-webgpu-pipeline-failed") || "[]");
  assert.ok(failure.some((f) => f.label === "gosx-elio-ocean-ocean_foam" && f.pass === "elioGraph"), JSON.stringify(failure));
  assert.ok(failure.every((f) => f.pass !== "core"));
  const draws = h.fake.state.renderPasses.flatMap((p) => p.draws).filter((d) => d.pipeline && d.pipeline.desc && /^gosx-ocean/.test(d.pipeline.desc.label));
  assert.ok(draws.length > 0 && /^gosx-ocean$/.test(draws[draws.length - 1].pipeline.desc.label), "the last ocean draw is the Gerstner pipeline");
  h.renderer.dispose();
});

test("renderer dispose releases the graph's buffers and textures", async () => {
  const h = await rendererWithChunk();
  h.canvas.width = h.canvas.height = 64;
  await renderSettled(h, sceneBundle({}));
  assert.ok(h.fake.state.textures.some((t) => /gosx-elio-ocean-dispTex0/.test(t.desc.label || "")));
  h.renderer.dispose();
  assert.ok(h.fake.state.textures.filter((t) => /gosx-elio-ocean|ocean-fft/.test(t.desc.label || "")).every((t) => t.destroyed));
  assert.ok(h.fake.state.buffers.filter((b) => b.usage & 128).every((b) => b.destroyed), "storage buffers");
});

test("the surface patch fails loudly when a base-shader anchor disappears", () => {
  const { context } = standalone();
  assert.throws(() => context.window.__gosx_scene3d_ocean_fft_surface.wgsl("struct X {};", 1), /anchor not found/);
});

test("a spectral shader that cannot be built falls back to the Gerstner ocean and reports through the pass guard", async () => {
  const h = await rendererWithChunk();
  h.canvas.width = h.canvas.height = 64;
  h.env.context.window.__gosx_scene3d_ocean_fft_surface.wgsl = () => { throw new Error("anchor not found: injected"); };
  const status = await renderSettled(h, sceneBundle({}), 10);
  assert.notEqual(status, "failed");
  const failure = JSON.parse(h.mount.getAttribute("data-gosx-scene3d-webgpu-pipeline-failed") || "[]");
  assert.ok(failure.some((f) => f.label === "gosx-ocean-fft-surface" && f.pass === "oceanFFT" && /anchor not found/.test(f.message)), JSON.stringify(failure));
  const draws = h.fake.state.renderPasses.flatMap((p) => p.draws).filter((d) => d.pipeline && d.pipeline.desc && /^gosx-ocean/.test(d.pipeline.desc.label));
  assert.ok(draws.length > 0 && /^gosx-ocean$/.test(draws[draws.length - 1].pipeline.desc.label));
  h.renderer.dispose();
});

test("the shipped (packed) WebGPU bundle still accepts the spectral surface patch", async () => {
  const h = await rendererWithChunk(false);
  h.canvas.width = h.canvas.height = 64;
  const status = await renderSettled(h, sceneBundle({ windSpeed: 9 }), 8);
  assert.notEqual(status, "failed");
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-pipeline-failed"), null);
  const module = h.fake.state.shaderModules.find((m) => m.label === "gosx-ocean-fft-surface");
  assert.ok(module, "built bundle: spectral surface compiled");
  if (naga) validateWGSL(module.code, "ocean-fft-surface-shipped");
  h.renderer.dispose();
});

// --- the Go contract: normalized SceneIR in, graph parameters out --------------------------------------------------

test("the browser carries the spectrum, swell waves, graphParams and graphTween from the normalized scene unchanged", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const raw = { spectrum: { windSpeed: 12, fetch: 100, peakEnhancement: 2, spread: 4, swell: 0.3, swellDirection: 15, seed: 0 }, swellWaves: [{ dx: 0, dz: 1, k: 0.1, omega: 1, amplitude: 0.1, phase: 0 }],
    masks: ["hull", 3], graphParams: { windSpeed: 12, seed: 0, bogus: "x" }, graphTween: 5, graphNodes: { hull: "ship", bad: 3, empty: "" } };
  const state = h.env.context.__gosx_scene3d_api.createSceneState({ scene: { environment: { ocean: raw } } });
  const ocean = state.environment.ocean;
  assert.equal(ocean.spectrum.windSpeed, 12);
  assert.equal(ocean.spectrum.seed, 0);
  assert.equal(ocean.swellWaves.length, 1);
  assert.deepEqual(Array.from(ocean.masks), ["hull"]);
  assert.deepEqual(JSON.parse(JSON.stringify(ocean.graphParams)), { windSpeed: 12, seed: 0 }, "non-numeric values are dropped");
  assert.equal(ocean.graphTween, 5);
  assert.deepEqual(JSON.parse(JSON.stringify(ocean.graphNodes)), { hull: "ship" }, "only node ids that are non-empty strings");
  h.renderer.dispose();
});

test("Go's graph parameters are exactly the graph's value parameters, and its swell waves share the graph's wind direction and loop time", () => {
  const s = standalone();
  const graph = s.graphs["webgpu/high"];
  const golden = JSON.parse(fs.readFileSync(path.join(__dirname, "..", "..", "scene", "testdata", "ocean-graph-params.json"), "utf8"));
  const valuesOf = (g) => Array.from(g.params).filter((p) => p.source.kind === "value" && !/\[\]$/.test(p.type)).map((p) => p.name).sort();
  // Go's parameters are the graph's value parameters. The wake's own tuning parameters (wakeCell, wakeDepth, ...) stay at the graph's defaults,
  // and the hull pose comes from a scene node, so the graph may have value parameters Go does not set, but only wake and hull ones; and
  // before the wake is in the generated module Go's three wake parameters have no counterpart yet.
  const graphValues = valuesOf(graph), hasWake = graphValues.includes("wakeEnabled"), WAKE = ["wakeEnabled", "hullLength", "hullBeam"];
  for (const entry of golden) {
    const goKeys = Object.keys(entry.graphParams).sort();
    const known = hasWake ? graphValues : graphValues.concat(WAKE).sort();
    assert.deepEqual(goKeys.filter((k) => !known.includes(k)), [], entry.preset + ": every Go parameter names a graph value parameter");
    assert.deepEqual(graphValues.filter((k) => !goKeys.includes(k) && !/^(wake|hull)/.test(k)), [], entry.preset + ": only wake and hull parameters keep the graph's defaults");
    for (const name of valuesOf(s.graphs["webgl2/high"])) assert.ok(name in entry.graphParams || /^(wake|hull|probe)/.test(name), "webgl2 parameter " + name);
  }
  const loop = graph.params.find((p) => p.name === "loopPeriod").source.value;
  const cases = JSON.parse(fs.readFileSync(path.join(__dirname, "..", "..", "scene", "testdata", "ocean-spectrum-query-parity.json"), "utf8"));
  const omega0 = 2 * Math.PI / loop;
  let checked = 0;
  for (const c of cases) {
    const waves = c.ocean.swellWaves || [];
    // The Go swell waves quantize omega to whole cycles per loop, exactly as ocean_spectrum does, so the CPU query and the GPU agree at every time.
    for (const w of waves) { const cycles = w.omega / omega0; assert.ok(Math.abs(cycles - Math.round(cycles)) < 1e-6, `omega ${w.omega} is not a whole number of cycles per ${loop} s`); }
    const sp = c.ocean.spectrum || {};
    if (!waves.length || (sp.swell || 0) > 0.2 || sp.swellDirection) continue;
    // The graph turns windDirDeg into the kernels' travel angle (windDir); the strongest Go wave must travel that way.
    const params = s.G.createParams(graph.params);
    params.set("windDirDeg", c.ocean.windDirection || 0);
    params.evaluate({ time: 0, dt: 0 });
    const wanted = params.current.windDir;
    const top = waves.slice().sort((a, b) => b.amplitude - a.amplitude)[0];
    const got = Math.atan2(top.dz, top.dx);
    assert.ok(Math.abs(Math.atan2(Math.sin(got - wanted), Math.cos(got - wanted))) < 0.9, `strongest wave travels at ${got.toFixed(2)} rad, the graph's wind angle is ${wanted.toFixed(2)} (windDirection ${c.ocean.windDirection})`);
    checked++;
  }
  assert.ok(checked >= 3, "enough fixture cases carry a pure wind sea: " + checked);
});

test("an environment update that adds a spectrum loads the FFT chunk once; one that removes it releases the GPU resources", async () => {
  const { FakeElement, createContext, installManualRAF, runScript, flushAsyncWork, bootstrapSource } = require("./runtime-test-harness.js");
  const chunk = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-ocean-fft.js"), "utf8");
  const mount = new FakeElement("div", null);
  mount.id = "ocean-update"; mount.setAttribute("data-gosx-engine", "GoSXScene3D");
  const camera = { x: 0, y: 20, z: 0, rotationX: -0.1, rotationY: 0, fov: 60, near: 0.1, far: 2000 };
  const props = { camera, scene: { camera, objects: [], environment: { ocean: { level: 0 } } } };
  const env = createContext({ elements: [mount], enableWebGL: true, disableCanvas2D: true, performanceNow: () => 0, fetchRoutes: { "/ocean-fft.js": { text: chunk } },
    manifest: { engines: [{ id: "ocean-update", component: "GoSXScene3D", kind: "surface", mountId: mount.id, props }] } });
  const script = env.document.createElement("script");
  script.setAttribute("data-gosx-script", "feature-scene3d");
  script.setAttribute("data-gosx-scene3d-ocean-fft-url", "/ocean-fft.js");
  env.document.head.appendChild(script);
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  env.document.dispatchEvent({ type: "DOMContentLoaded" });
  for (let i = 0; i < 8; i++) { await flushAsyncWork(); raf.flush(i * 17); }
  const handle = env.context.__gosx.engines.get("ocean-update").handle;
  const fetched = () => (env.fetchCalls || []).filter((c) => /ocean-fft\.js/.test(String(c.url || c))).length;
  assert.equal(fetched(), 0, "a scene without a spectrum never fetches the chunk");
  assert.equal(env.context.__gosx_scene3d_ocean_fft_api, undefined);
  await handle.applyCommands([{ kind: 13, data: { environment: { ocean: { level: 0, spectrum: { windSpeed: 10 } } }, shape: "sceneIR" } }]);
  for (let i = 0; i < 8; i++) { await flushAsyncWork(); raf.flush(200 + i * 17); }
  assert.equal(fetched(), 1, "adding a spectrum fetches the chunk");
  assert.ok(env.context.__gosx_scene3d_ocean_fft_api, "and publishes the API");
  await handle.applyCommands([{ kind: 13, data: { environment: { ocean: { level: 0, spectrum: { windSpeed: 12 } } }, shape: "sceneIR" } }]);
  for (let i = 0; i < 4; i++) { await flushAsyncWork(); raf.flush(400 + i * 17); }
  assert.equal(fetched(), 1, "a later update does not fetch again");
  handle.dispose();
});

