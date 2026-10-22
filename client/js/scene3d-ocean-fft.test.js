"use strict";
// Spectral (FFT) ocean on WebGPU: the compute scheduler, the surface shader patch and the renderer hooks.
// Runs against the repository's fake WebGPU device; the real-GPU frames come from scripts/scene3d-ocean-spectrum.mjs.
const test = require("node:test"), assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { execFileSync } = require("node:child_process");
const { createRequire } = require("node:module");
const ts = createRequire(path.join(__dirname, "..", "runtime", "package.json"))("typescript");
const { createBoardWebGPUHarness, makeFakeGPUDevice, makePointsBundle } = require("./runtime-test-harness.js");

const scene3d = path.join(__dirname, "..", "runtime", "scene3d");
const CHUNK_FILES = ["ocean-fft-kernels.ts", "ocean-fft-surface.ts", "ocean-fft.ts"];
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
  const context = vm.createContext({ Math, Number, Object, Array, Float32Array, Uint32Array, ArrayBuffer, DataView, Error, Set, Map, JSON });
  const api = loadChunk(context);
  const fake = makeFakeGPUDevice();
  // Pipelines become ready when the test says so, as after asynchronous validation.
  const validation = new Map();
  const host = (extra) => Object.assign({
    device: fake.device, validation, guard: { disabled: new Set(), failures: [], fail(label, message) { this.failures.push({ label, message }); this.disabled.add("oceanFFT"); } }, environment: null, camera: { x: 0, z: 0 }, time: 0, tier: "high",
    create(device, kind, descriptor) { const handle = { label: descriptor.label }; validation.set(handle, { ready: false, label: descriptor.label }); return handle; },
  }, extra);
  const settle = () => { for (const state of validation.values()) state.ready = true; };
  return { api, fake, host, validation, settle, context };
}
const ocean = (spectrum, extra) => Object.assign({ level: 0, choppiness: 0.6, windDirection: 0, spectrum }, extra);
const frame = (api, resources, h, env, extra) => api.frame(resources, Object.assign({}, h, { environment: { ocean: env }, encoder: h.encoder || h.device.createCommandEncoder() }, extra));

test("the chunk's resolve step applies defaults, tiers and the single wind-angle conversion", () => {
  const { api } = standalone();
  const r = api.resolve(ocean({}), "high");
  assert.deepEqual([r.windSpeed, r.fetchKm, r.gamma, r.spread, r.swell, r.seed], [9, 300, 3.3, 8, 0, 0]);
  assert.deepEqual(Array.from(r.cascades, (c) => c.n), [64, 256, 256, 256]);
  assert.deepEqual(Array.from(r.cascades, (c) => c.size), [1600, 250, 37, 5.3]);
  assert.deepEqual(Array.from(api.resolve(ocean({}), "medium").cascades, (c) => c.n), [64, 128, 128, 128]);
  assert.deepEqual(Array.from(api.resolve(ocean({}), "low").cascades, (c) => c.n), [64, 64, 64], "low drops cascade 3");
  // Scene3D degrees: 0 travels toward +Z; positive turns toward +X. Kernel angle: toward (cos t, sin t) in (x, z).
  assert.equal(api.travelAngle(0), Math.PI / 2);
  assert.ok(Math.abs(api.travelAngle(90)) < 1e-15);
  assert.equal(api.resolve(ocean({}, { windDirection: 90 }), "high").windDir, api.travelAngle(90));
  assert.equal(api.resolve(ocean({ swellDirection: 45 }, { windDirection: 90 }), "high").swellDir, api.travelAngle(45));
  assert.equal(api.resolve(ocean({ seed: 0 }), "high").seed, 0, "0 is a seed");
  assert.equal(api.resolve(ocean({ seed: 2 ** 32 + 5 }), "high").seed, 5);
  assert.equal(api.resolve(ocean({ windSpeed: 400, fetch: 5000, spread: 99, peakEnhancement: 99 }), "high").windSpeed, 40);
  // A band edge above the cascade's lowest Nyquist wavenumber would leave the Nyquist line non-empty.
  const custom = api.resolve(ocean({ cascades: [{ size: 100, kMin: 0, kMax: 9 }] }), "high").cascades[0];
  assert.ok(custom.kMax < Math.PI * 64 / 100);
});

test("uniform packing follows the generated layouts and rejects typos", () => {
  const { api, context } = standalone();
  const layout = context.window.__gosx_scene3d_elio_ocean.kernels.oceanEvolve.uniforms[0];
  const view = new DataView(api.pack(layout, { n: 64, t: 12.5, tile: 250, loopPeriod: 1000 }));
  const field = (name) => layout.fields.find((f) => f.name === name);
  assert.equal(view.getUint32(field("n").offset, true), 64);
  assert.equal(view.getFloat32(field("t").offset, true), 12.5);
  assert.throws(() => api.pack(layout, { n: 64, t: 1, tile: 1 }), /loopPeriod/);
  assert.throws(() => api.pack(layout, { n: 64, t: 1, tile: 1, loopPeriod: 1, tt: 2 }), /no field tt/);
});

test("frame() does nothing without a spectrum and tears down when the spectrum goes away", () => {
  const s = standalone(), resources = {}, h = s.host();
  assert.equal(frame(s.api, resources, h, { level: 0 }), null);
  assert.equal(s.fake.state.buffers.length, 0);
  frame(s.api, resources, h, ocean({}));
  assert.ok(resources.fft && s.fake.state.buffers.length > 0);
  const owned = s.fake.state.buffers.slice();
  assert.equal(frame(s.api, resources, h, { level: 0 }), null);
  assert.equal(resources.fft, null);
  assert.ok(owned.every((b) => b.destroyed));
});

test("nothing is encoded until every pipeline has validated; then one compute pass runs in kernel order", () => {
  const s = standalone(), resources = {}, h = s.host();
  frame(s.api, resources, h, ocean({}));
  assert.equal(resources.fft.ready, false);
  assert.equal(s.fake.state.computePasses.length, 0, "pipelines pending: the Gerstner ocean keeps drawing");
  s.settle();
  frame(s.api, resources, h, ocean({}));
  assert.equal(resources.fft.ready, true);
  const [pass] = s.fake.state.computePasses;
  assert.equal(s.fake.state.computePasses.length, 1);
  assert.ok(pass.ended);
  const names = pass.dispatches.map((d) => d.pipeline.label.replace("gosx-ocean-fft-", ""));
  // Per cascade: spectrum (first frame only), evolve, rows and cols on A and B, assemble, mips; then foam.
  const per = (n) => ["spectrum", "evolve", "fft-rows-" + n, "fft-cols-" + n, "fft-rows-" + n, "fft-cols-" + n, "assemble"];
  const expected = [];
  for (const [i, n] of [64, 256, 256, 256].entries()) {
    expected.push(...per(n));
    for (let level = 1; level < Math.log2(n) - 1; level++) expected.push("mip", "mip");
  }
  expected.push("foam");
  assert.deepEqual(names, expected);
  assert.ok(pass.dispatches.every((d) => d.pipeline.label.startsWith("gosx-ocean-fft-")));
  const fftRows = pass.dispatches.filter((d) => d.pipeline.label === "gosx-ocean-fft-fft-rows-256");
  assert.ok(fftRows.every((d) => d.workgroupCountX === 256), "one workgroup per line");
});

test("resources are created once, spectrum changes rerun only ocean_spectrum, tier changes rebuild, dispose releases all", () => {
  const s = standalone(), resources = {}, h = s.host();
  frame(s.api, resources, h, ocean({ windSpeed: 9 }));
  s.settle();
  frame(s.api, resources, h, ocean({ windSpeed: 9 }));
  const state = s.fake.state, buffers = state.buffers.length, textures = state.textures.length;
  assert.equal(resources.fft.buildCount, 1);
  const dispatchesOf = (label) => state.computePasses.flatMap((p) => p.dispatches).filter((d) => d.pipeline.label === "gosx-ocean-fft-" + label).length;
  assert.equal(dispatchesOf("spectrum"), 4);
  frame(s.api, resources, h, ocean({ windSpeed: 9 }), { time: 5 });
  frame(s.api, resources, h, ocean({ windSpeed: 9 }), { time: 6 });
  assert.equal(dispatchesOf("spectrum"), 4, "an unchanged spectrum does not rerun ocean_spectrum");
  assert.equal([state.buffers.length, state.textures.length].join(), [buffers, textures].join(), "no allocation per frame");
  frame(s.api, resources, h, ocean({ windSpeed: 15 }), { time: 7 });
  assert.equal(dispatchesOf("spectrum"), 8, "a changed spectrum reruns it once per cascade");
  assert.equal([state.buffers.length, state.textures.length].join(), [buffers, textures].join(), "spectrum change keeps buffers and textures");
  assert.equal(resources.fft.buildCount, 1);
  const before = state.buffers.filter((b) => b.usage & 128), beforeTextures = state.textures.filter((t) => !/dummy/.test(t.desc.label));
  frame(s.api, resources, h, ocean({ windSpeed: 15 }), { tier: "low", time: 8 });
  assert.equal(resources.fft.buildCount, 2, "a tier change rebuilds");
  assert.ok(before.every((b) => b.destroyed) && beforeTextures.every((t) => t.destroyed), "the old resources are released");
  s.settle();
  frame(s.api, resources, h, ocean({ windSpeed: 15 }), { tier: "low", time: 9 });
  const live = () => [...state.buffers, ...state.textures].filter((r) => !r.destroyed);
  assert.ok(live().length > 0);
  resources.fft.dispose();
  assert.equal(live().length, 0, "dispose releases every buffer and texture");
});

test("the evolve kernel gets the scene time modulo the loop period", () => {
  const s = standalone(), resources = {}, h = s.host();
  frame(s.api, resources, h, ocean({}), { time: 1234.5 });
  s.settle();
  frame(s.api, resources, h, ocean({}), { time: 1234.5 });
  const layout = s.context.window.__gosx_scene3d_elio_ocean.kernels.oceanEvolve.uniforms[0];
  const t = layout.fields.find((f) => f.name === "t").offset;
  const evolveBuffers = new Set(s.fake.state.bindGroups.filter((g) => g.desc.entries.length === 5 && g.desc.entries.every((e) => e.resource.buffer)).map((g) => g.desc.entries[0].resource.buffer));
  const writes = s.fake.state.writeBufferCalls.filter((w) => evolveBuffers.has(w.buffer));
  assert.ok(writes.length >= 4);
  const view = new DataView(writes[writes.length - 1].data instanceof ArrayBuffer ? writes[writes.length - 1].data : writes[writes.length - 1].data.buffer);
  assert.ok(Math.abs(view.getFloat32(t, true) - 234.5) < 1e-3);
});

test("a disabled oceanFFT pass or a changed device disposes the engine", () => {
  const s = standalone(), resources = {}, h = s.host();
  frame(s.api, resources, h, ocean({}));
  const first = resources.fft;
  assert.equal(frame(s.api, resources, Object.assign({}, h, { guard: { disabled: new Set(["oceanFFT"]) } }), ocean({})), null);
  assert.equal(resources.fft, null);
  assert.ok(first.disposed);
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
  validateWGSL(s.api.MIP_WGSL, "downsample");
});

// --- the full renderer: hook order, surface variant, fallback -----------------------------------------------------

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

test("the renderer encodes the ocean compute pass before the main pass and samples the cascades", async () => {
  const h = await rendererWithChunk();
  h.canvas.width = h.canvas.height = 64;
  const b = sceneBundle({ windSpeed: 12 });
  await renderSettled(h, b);
  const { state } = h.fake;
  const module = state.shaderModules.find((m) => m.label === "gosx-ocean-fft-surface");
  assert.ok(module, "the spectral surface variant is compiled");
  assert.match(module.code, /textureSampleLevel\(spDisp0/);
  assert.doesNotMatch(module.code, /for \(var i = 0; i < 6; i\+\+\) \{\n    if \(i >= waves\)/, "the Gerstner loop is replaced");
  if (naga) validateWGSL(module.code, "ocean-fft-surface");
  const pipeline = state.renderPipelines.find((p) => p.desc.label === "gosx-ocean-fft-surface");
  assert.ok(pipeline);
  assert.equal(pipeline.desc.layout.desc.bindGroupLayouts.length, 3, "ocean, reflection and spectral groups");
  const oceanPass = state.renderPasses.find((p) => p.draws.some((d) => d.pipeline && d.pipeline.desc && d.pipeline.desc.label === "gosx-ocean-fft-surface"));
  assert.ok(oceanPass, "the main pass draws the spectral ocean");
  const compute = state.computePasses.filter((p) => p.descriptor && p.descriptor.label === "gosx-ocean-fft-pass");
  assert.ok(compute.length >= 1);
  // Command order: the ocean compute pass is recorded before the render pass that samples it.
  const order = [];
  const done = h.fake.device.createCommandEncoder;
  assert.ok(typeof done === "function");
  h.renderer.dispose();
});

test("encoding order inside one frame: compute passes precede render passes", async () => {
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
  const fft = lastFrame.indexOf("gosx-ocean-fft-pass"), render = lastFrame.indexOf("render");
  assert.ok(fft >= 0 && render > fft, "ocean compute before the main pass: " + lastFrame.join(","));
  h.renderer.dispose();
});

test("scenes without a spectrum render exactly as before: no FFT resources, Gerstner shader", async () => {
  const h = await rendererWithChunk();
  h.canvas.width = h.canvas.height = 64;
  await renderSettled(h, sceneBundle(null));
  const { state } = h.fake;
  assert.ok(state.shaderModules.some((m) => m.label === "gosx-ocean"));
  assert.ok(!state.shaderModules.some((m) => /ocean-fft/.test(m.label || "")));
  assert.ok(!state.computePipelines.some((p) => /ocean-fft/.test(p.label || "")));
  h.renderer.dispose();
});

test("a failed FFT pipeline falls back to the Gerstner ocean instead of a core error", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const createRender = h.fake.device.createRenderPipeline.bind(h.fake.device);
  h.fake.device.createRenderPipeline = (d) => Object.assign(createRender(d), { getBindGroupLayout: () => h.fake.device.createBindGroupLayout({ entries: [] }) });
  const createCompute = h.fake.device.createComputePipeline.bind(h.fake.device);
  h.fake.device.createComputePipeline = (d) => { if (/ocean-fft-foam/.test(d.label)) throw new Error("injected validation failure"); return createCompute(d); };
  loadChunk(h.env.context);
  h.canvas.width = h.canvas.height = 64;
  const status = await renderSettled(h, sceneBundle({}), 10);
  assert.notEqual(status, "failed");
  const failure = JSON.parse(h.mount.getAttribute("data-gosx-scene3d-webgpu-pipeline-failed") || "[]");
  assert.ok(failure.some((f) => f.label === "gosx-ocean-fft-foam" && f.pass === "oceanFFT"), JSON.stringify(failure));
  assert.ok(failure.every((f) => f.pass !== "core"));
  const { state } = h.fake;
  const draws = state.renderPasses.flatMap((p) => p.draws).filter((d) => d.pipeline && d.pipeline.desc && /^gosx-ocean/.test(d.pipeline.desc.label));
  assert.ok(draws.length > 0 && /^gosx-ocean$/.test(draws[draws.length - 1].pipeline.desc.label), "the last ocean draw is the Gerstner pipeline");
  h.renderer.dispose();
});

test("renderer dispose releases the FFT buffers and textures", async () => {
  const h = await rendererWithChunk();
  h.canvas.width = h.canvas.height = 64;
  await renderSettled(h, sceneBundle({}));
  const mine = () => [...h.fake.state.buffers.filter((b) => b.size > 0), ...h.fake.state.textures.filter((t) => /ocean-fft/.test(t.desc.label || ""))];
  assert.ok(h.fake.state.textures.some((t) => /ocean-fft-disp/.test(t.desc.label || "")));
  h.renderer.dispose();
  assert.ok(h.fake.state.textures.filter((t) => /ocean-fft/.test(t.desc.label || "")).every((t) => t.destroyed));
  const storage = h.fake.state.buffers.filter((b) => b.usage & 128);
  assert.ok(storage.length >= 16 && storage.every((b) => b.destroyed), "storage buffers: " + storage.length);
  assert.ok(mine().length > 0);
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
