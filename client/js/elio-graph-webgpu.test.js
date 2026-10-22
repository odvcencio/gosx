"use strict";
// The WebGPU executor of the Elio graph runner, on the repository's fake WebGPU device. Graphs are hand-built to the
// contract over kernels in the generated module.
const test = require("node:test"), assert = require("node:assert/strict");
const { runnerContext, testGraph } = require("./elio-graph-test-support.js");
const { makeFakeGPUDevice } = require("./runtime-test-harness.js");

function setup(graphEdit, deviceExtra) {
  const context = runnerContext({
    GPUBufferUsage: { MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512 },
    GPUTextureUsage: { COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 },
  });
  const g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
  const fake = makeFakeGPUDevice();
  let lose;
  fake.device.lost = new Promise((resolve) => { lose = resolve; });
  Object.assign(fake.device, deviceExtra);
  const graph = testGraph("webgpu");
  if (graphEdit) graphEdit(graph);
  // Pipelines become ready when the test says so, as after the renderer's asynchronous validation.
  const validation = new Map(), failures = [];
  const host = { device: fake.device, validation, create(device, kind, descriptor) { const handle = { label: descriptor.label, kind }; validation.set(handle, { ready: false }); return handle; } };
  const runner = g.create({ module, backend: "webgpu", graph, host, onFail: (reason) => failures.push(reason) });
  const settle = () => { for (const v of validation.values()) v.ready = true; };
  let time = 10;
  const encoder = () => fake.device.createCommandEncoder();
  const step = (e = encoder(), extra) => runner.run(Object.assign({ time: (time += 1 / 60), dt: 1 / 60, cameraX: 0, cameraY: 30, cameraZ: 0, encoder: e }, extra));
  return { fake, runner, graph, validation, settle, step, encoder, failures, lose, host, g, module };
}
const dispatchLabels = (pass) => pass.dispatches.map((d) => d.pipeline.label);

test("nothing is encoded until every pipeline has validated; then each kernel pass is its own compute pass, in graph order", () => {
  const s = setup();
  assert.equal(s.runner.failed, null);
  assert.equal(s.step(), false);
  assert.equal(s.fake.state.computePasses.length, 0, "pipelines pending");
  assert.equal(s.runner.ready, false);
  s.settle();
  assert.equal(s.step(), true);
  assert.equal(s.runner.ready, true);
  const passes = s.fake.state.computePasses;
  assert.equal(passes.length, 4, "evolve, fft rows, fft cols, assemble: one compute pass per kernel pass");
  assert.ok(passes.every((p) => p.ended));
  const labels = passes.map((p) => dispatchLabels(p)[0]);
  assert.deepEqual(labels, ["gosx-elio-test-ocean_evolve", "gosx-elio-test-fft_compute_64", "gosx-elio-test-fft_compute_64", "gosx-elio-test-ocean_assemble"]);
  assert.deepEqual(passes.map((p) => p.dispatches[0].workgroupCountX), [64, 64, 64, 64]);
});

test("pipelines are created once per kernel and entry through the validated-pipeline host call, labelled gosx-elio-<graph>-<kernel>", () => {
  const s = setup();
  const labels = [...s.validation.keys()].map((h) => h.label).sort();
  assert.deepEqual(labels, ["gosx-elio-test-fft_compute_64", "gosx-elio-test-fft_compute_64", "gosx-elio-test-ocean_assemble", "gosx-elio-test-ocean_evolve"]);
  assert.ok([...s.validation.keys()].every((h) => h.kind === "compute"));
  s.settle(); s.step(); s.step();
  assert.equal(s.validation.size, 4, "no pipeline per frame");
});

test("changed: passes run on the first frame and when their parameter moves; uniforms carry the frame's parameter values", () => {
  const s = setup();
  s.settle();
  s.step(undefined, { time: 1234.5 });
  const evolveWrites = () => s.fake.state.writeBufferCalls.filter((w) => w.buffer.size === 32 && new DataView(w.data.buffer || w.data, w.data.byteOffset || 0).getUint32(0, true) === 64);
  assert.ok(evolveWrites().length >= 1);
  const labelsOf = () => s.fake.state.computePasses.map((p) => dispatchLabels(p)[0].replace("gosx-elio-test-", ""));
  assert.equal(labelsOf().filter((l) => l === "ocean_evolve").length, 1);
  s.step();
  assert.equal(labelsOf().filter((l) => l === "ocean_evolve").length, 1, "windSpeed did not change");
  s.runner.setValue("windSpeed", 20);
  s.step(); s.step();
  assert.equal(labelsOf().filter((l) => l === "ocean_evolve").length, 2, "the tween's first frame holds the value; the second moves it");
  const first = s.fake.state.writeBufferCalls.find((w) => w.buffer.size === 32);
  const view = new DataView(first.data.buffer || first.data, first.data.byteOffset || 0);
  const t = s.module.kernels.oceanEvolve.uniforms[0].fields.find((f) => f.name === "t").offset;
  assert.ok(Math.abs(view.getFloat32(t, true) - 234.5) < 1e-3, "t = time mod 1000, packed with the WGSL layout");
});

test("textures and buffers follow the graph: sizes, formats, mips, and per-level views bound to storage textures", () => {
  const s = setup();
  const { state } = s.fake;
  const disp = state.textures.find((t) => t.desc.label === "gosx-elio-test-disp");
  assert.deepEqual([disp.desc.format, disp.desc.mipLevelCount, ...disp.desc.size.slice(0, 2)], ["rgba16float", 5, 64, 64]);
  const a0 = state.buffers.find((b) => b.size === 4096 * 16);
  assert.ok(a0, "a vec4 buffer holds count x 16 bytes");
  assert.ok(state.buffers.some((b) => b.size === 4096 * 4), "an f32 buffer holds count x 4 bytes");
  const exported = s.runner.export("disp0");
  assert.equal(exported.texture, disp);
  assert.deepEqual([exported.width, exported.height, exported.levels], [64, 64, 5]);
  assert.equal(s.runner.export("nope"), null);
});

test("frameSwaps exchange resources after each frame; each swapped variant builds its bind group once", () => {
  const s = setup((g) => { g.frameSwaps = [["disp", "slope"]]; });
  s.settle();
  const before = s.fake.state.bindGroups.length;
  s.step(); s.step(); s.step(); s.step();
  const created = s.fake.state.bindGroups.length - before;
  assert.ok(created > 0 && created <= 12, "bind groups are memoised per swap state, got " + created);
  const disp = s.fake.state.textures.find((t) => t.desc.label === "gosx-elio-test-disp"), slope = s.fake.state.textures.find((t) => t.desc.label === "gosx-elio-test-slope");
  assert.equal(s.runner.export("disp0").texture, disp, "an even number of frames swaps back");
  s.step();
  assert.equal(s.runner.export("disp0").texture, slope);
});

test("a frame without an encoder disables the graph and releases its resources", () => {
  const warn = console.warn; console.warn = () => {};
  try {
    const s = setup();
    s.settle();
    assert.equal(s.runner.run({ time: 1, dt: 0.1 }), false, "no encoder");
    assert.equal(s.runner.failed, "error");
    assert.match(s.runner.error.message, /frame.encoder/);
    assert.deepEqual(s.failures, ["error"]);
    assert.ok(s.fake.state.buffers.every((b) => b.destroyed), "a disabled graph releases its buffers");
    assert.ok(s.fake.state.textures.every((t) => t.destroyed));
  } finally { console.warn = warn; }
});

test("a pipeline that fails to build in the host surfaces as a disabled graph", () => {
  const warn = console.warn; console.warn = () => {};
  try {
    const context = runnerContext({ GPUBufferUsage: { STORAGE: 128, COPY_DST: 8, UNIFORM: 64 }, GPUTextureUsage: { STORAGE_BINDING: 8, TEXTURE_BINDING: 4 }, GPUShaderStage: { COMPUTE: 4 } });
    const g = context.window.__gosx_elio_graph, fake = makeFakeGPUDevice();
    const r = g.create({ module: context.window.__gosx_scene3d_elio_ocean, backend: "webgpu", graph: testGraph("webgpu"),
      host: { device: fake.device, create() { throw new Error("validation rejected"); } } });
    assert.equal(r.failed, "error");
    assert.match(r.error.message, /validation rejected/);
  } finally { console.warn = warn; }
});

test("dispose, and device loss, release every buffer and texture; a disposed runner does nothing", async () => {
  const s = setup();
  s.settle();
  s.step();
  const live = () => [...s.fake.state.buffers, ...s.fake.state.textures].filter((r) => !r.destroyed);
  assert.ok(live().length > 0);
  s.runner.dispose();
  assert.equal(live().length, 0);
  assert.equal(s.step(), false);
  const lost = setup();
  lost.settle(); lost.step();
  lost.lose({ reason: "destroyed" });
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal([...lost.fake.state.buffers, ...lost.fake.state.textures].filter((r) => !r.destroyed).length, 0, "device loss disposes");
  assert.equal(lost.step(), false);
});

test("repeat passes become separate compute passes, each with its own uniform block holding its index", () => {
  const s = setup((g) => {
    g.passes = [{ type: "repeat", name: "stages", count: 4, index: "stage", swap: [["a0", "a1"]], body: [
      { name: "evolve", type: "kernel", kernel: "ocean_evolve", entry: "ocean_evolve", when: "always", dispatch: [64, 1, 1],
        bindings: [{ name: "h0", resource: "h0" }, { name: "omega", resource: "omega" }, { name: "outA", resource: "a0" }, { name: "outB", resource: "b0" }],
        uniforms: [{ name: "ev", values: { n: 64, t: "param:stage", tile: 250, loopPeriod: 1000 } }] }] }];
  });
  s.settle();
  s.step();
  assert.equal(s.fake.state.computePasses.length, 4);
  const writes = s.fake.state.writeBufferCalls.filter((w) => w.buffer.size === 32);
  assert.equal(new Set(writes.map((w) => w.buffer)).size, 4, "one uniform buffer per iteration, so queued writes cannot overwrite each other");
  const t = s.module.kernels.oceanEvolve.uniforms[0].fields.find((f) => f.name === "t").offset;
  assert.deepEqual(writes.map((w) => new DataView(w.data.buffer || w.data, w.data.byteOffset || 0).getFloat32(t, true)), [0, 1, 2, 3]);
});

test("a fed buffer is uploaded from its parameter each frame, and a buffer export reaches the surface or a readback", () => {
  const s = setup((g) => {
    g.params.push({ name: "probes", type: "vec4<f32>[]", count: 2, source: { kind: "value", value: [1, 2, 3, 4, 5, 6, 7, 8] } });
    g.resources[0].feed = "probes";
    g.exports.probeBuffer = "h0";
  });
  s.settle();
  s.step(); s.step();
  const fed = s.fake.state.writeBufferCalls.filter((w) => w.buffer.size === 4096 * 16);
  assert.equal(fed.length, 2);
  assert.equal(fed[0].data.length, 8);
  const exported = s.runner.export("probeBuffer");
  assert.equal(exported.buffer.size, 4096 * 16);
});
