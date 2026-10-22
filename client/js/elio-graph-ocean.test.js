"use strict";
// The runner against the pass graphs Elio emits for the ocean (module.graphs). Skipped while the generated kernel module
// carries no graphs; once it does, every tier on both backends must build and run a frame on the test doubles.
const test = require("node:test"), assert = require("node:assert/strict");
const { RecordingGL, runnerContext, floatCaps } = require("./elio-graph-test-support.js");
const { makeFakeGPUDevice } = require("./runtime-test-harness.js");

const probe = runnerContext();
const graphs = probe.window.__gosx_scene3d_elio_ocean.graphs;
const skip = graphs ? false : "the generated Elio kernel module has no graphs yet";
const gpuGlobals = { GPUBufferUsage: { MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512 },
  GPUTextureUsage: { COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } };
const frame = (time) => ({ time, dt: 1 / 60, cameraX: 100 + time, cameraY: 30, cameraZ: -40 });

for (const tier of ["high", "medium", "low"]) {
  test("webgl2/" + tier + ": the emitted ocean graph builds, runs a frame on the double and exposes its exports", { skip }, () => {
    const context = runnerContext(), g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
    const gl = new RecordingGL(), textureCache = { _sceneTextureEpoch: 0 };
    const runner = g.create({ module, backend: "webgl2", tier, host: { gl, caps: floatCaps, textureCache } });
    assert.equal(runner.failed, null, String(runner.error && runner.error.stack));
    assert.equal(runner.run(frame(1)), true, String(runner.error && runner.error.stack));
    const all = gl.draws.filter((d) => d.kind === "draw");
    assert.ok(all.length > 20, "passes ran: " + all.length);
    assert.ok(all.every((d) => Object.keys(d.attachments).length <= 2));
    for (const d of all) for (const i of d.inputs) for (const o of Object.values(d.attachments)) assert.notEqual(i, o, "never samples its target");
    for (const name of Object.keys(runner.graph.exports)) assert.ok(runner.export(name), name);
    assert.equal(runner.run(frame(1 + 1 / 60)), true);
    assert.ok(runner.run(frame(2)) && textureCache._sceneTextureEpoch >= 3);
    const first = all.length;
    assert.ok(gl.draws.filter((d) => d.kind === "draw").length > first, "later frames draw too");
    runner.dispose();
    for (const kind of Object.keys(gl.created)) assert.equal(gl.live(kind).length, 0, kind + " leaked");
  });

  test("webgpu/" + tier + ": the emitted ocean graph builds and encodes a frame once its pipelines validate", { skip }, () => {
    const context = runnerContext(gpuGlobals), g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
    const fake = makeFakeGPUDevice(), validation = new Map();
    const host = { device: fake.device, validation, create(device, kind, d) { const h = { label: d.label }; validation.set(h, { ready: false }); return h; } };
    const runner = g.create({ module, backend: "webgpu", tier, host });
    assert.equal(runner.failed, null, String(runner.error && runner.error.stack));
    assert.equal(runner.run(Object.assign(frame(1), { encoder: fake.device.createCommandEncoder() })), false, "pipelines pending");
    for (const v of validation.values()) v.ready = true;
    assert.equal(runner.run(Object.assign(frame(1), { encoder: fake.device.createCommandEncoder() })), true, String(runner.error && runner.error.stack));
    assert.ok(fake.state.computePasses.length > 10);
    assert.ok(fake.state.computePasses.every((p) => p.ended));
    assert.ok([...validation.keys()].every((h) => /^gosx-elio-ocean-/.test(h.label)));
    runner.dispose();
  });
}

test("exportParams of the emitted graph are evaluated and exposed", { skip }, () => {
  const context = runnerContext(), g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
  const runner = g.create({ module, backend: "webgl2", tier: "high", host: { gl: new RecordingGL(), caps: floatCaps } });
  runner.run(frame(1));
  const exported = runner.exportParams();
  assert.deepEqual(Object.keys(exported), runner.graph.exportParams || []);
});
