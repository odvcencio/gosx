"use strict";
// The runner against the pass graphs Elio emits for the ocean (module.graphs). Skipped while the generated kernel module
// carries no graphs; once it does, every tier on both backends must build and run a frame on the test doubles.
const test = require("node:test"), assert = require("node:assert/strict");
const { RecordingGL, runnerContext, floatCaps } = require("./elio-graph-test-support.js");
const { makeFakeGPUDevice } = require("./runtime-test-harness.js");

const probe = runnerContext();
const graphs = probe.window.__gosx_scene3d_elio_ocean.graphs;
const skipFor = (backend) => graphs && graphs[backend + "/high"] ? false : "the generated Elio kernel module has no " + backend + " graphs yet";
const gpuGlobals = { GPUBufferUsage: { MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512 },
  GPUTextureUsage: { COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } };
const frame = (time) => ({ time, dt: 1 / 60, cameraX: 100 + time, cameraY: 30, cameraZ: -40 });

for (const tier of ["high", "medium", "low"]) {
  test("webgl2/" + tier + ": the emitted ocean graph builds, runs a frame on the double and exposes its exports", { skip: skipFor("webgl2") }, () => {
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

  test("webgpu/" + tier + ": the emitted ocean graph builds and encodes a frame once its pipelines validate", { skip: skipFor("webgpu") }, () => {
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

test("exportParams of the emitted graph are the wrapped water offsets, evaluated in float64", { skip: skipFor("webgl2") && skipFor("webgpu") }, () => {
  const backend = skipFor("webgl2") ? "webgpu" : "webgl2";
  const context = runnerContext(gpuGlobals), g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
  const fake = makeFakeGPUDevice(), validation = new Map();
  const host = backend === "webgl2" ? { gl: new RecordingGL(), caps: floatCaps }
    : { device: fake.device, validation, create(d, k, desc) { const h = { label: desc.label }; validation.set(h, { ready: true }); return h; } };
  const runner = g.create({ module, backend, tier: "high", host });
  runner.setValue("driftX", 3.3); runner.setValue("driftZ", -1.7);
  let time = 0;
  for (let i = 0; i < 600; i++) assert.equal(runner.run({ time: (time += 1 / 60), dt: 1 / 60, cameraX: 0, cameraY: 30, cameraZ: 0, encoder: backend === "webgpu" ? fake.device.createCommandEncoder() : undefined }), true);
  const exported = runner.exportParams();
  assert.deepEqual(Object.keys(exported), [...runner.graph.exportParams]);
  assert.deepEqual(Object.keys(exported).slice(0, 4), ["waterModX0", "waterModZ0", "waterModX1", "waterModZ1"]);
  const waterX = 3.3 * 10, waterZ = -1.7 * 10, wrap = (v, m) => v - Math.floor(v / m) * m;
  assert.ok(Math.abs(exported.waterModX0 - wrap(waterX, 1600)) < 1e-6);
  assert.ok(Math.abs(exported.waterModZ0 - wrap(waterZ, 1600)) < 1e-6);
  assert.ok(Math.abs(exported.waterModX3 - wrap(waterX, 5.3)) < 1e-6, "the smallest tile wraps often: " + exported.waterModX3);
  assert.ok(exported.waterModZ3 >= 0 && exported.waterModZ3 < 5.3, "mod is floored, so wrapped offsets are never negative");
});

test("the wake: its passes wait for wakeEnabled, the hull params drive its window, and the wake texture is exported (WebGPU)", { skip: skipFor("webgpu") }, () => {
  const context = runnerContext(gpuGlobals), g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
  const fake = makeFakeGPUDevice(), validation = new Map();
  const poses = { ship: { x: 120, z: -40, heading: Math.PI, speed: 9 } };
  const host = { device: fake.device, validation, nodePose: (id) => poses[id] || null, create(d, k, desc) { const h = { label: desc.label }; validation.set(h, { ready: true }); return h; } };
  const runner = g.create({ module, backend: "webgpu", tier: "high", host });
  assert.equal(runner.failed, null, String(runner.error && runner.error.stack));
  // The graph's hull params are value params the glue sets from the bound scene node (a graph may declare them as node sources instead).
  const hull = () => { for (const [name, field] of [["hullX", "x"], ["hullZ", "z"], ["hullHeading", "heading"], ["hullSpeed", "speed"]]) runner.setValue(name, poses.ship[field]); };
  hull();
  const labels = () => fake.state.computePasses.map((p) => p.descriptor.label.replace("gosx-elio-ocean-", ""));
  const step = (time) => assert.equal(runner.run({ time, dt: 1 / 60, cameraX: 100, cameraY: 30, cameraZ: -20, encoder: fake.device.createCommandEncoder() }), true);
  step(1);
  assert.ok(!labels().some((l) => /^wake\./.test(l)), "no wake pass while the wake is off");
  assert.equal(runner.value("hullX"), 120);
  assert.equal(runner.value("hullSpeed"), 9);
  const before = labels().length;
  runner.setValue("wakeEnabled", 1);
  step(1.02);
  const wake = labels().slice(before);
  for (const pass of ["wake.source", "wake.symbol", "wake.step"]) assert.ok(wake.includes(pass), pass + " runs once the wake is on: " + wake.filter((l) => /^wake\./.test(l)).join());
  assert.equal(wake.filter((l) => /^wake\.fft\d\.fft_(rows|cols)$/.test(l)).length, 4, "two inverse transforms, rows then columns");
  const exported = runner.export("wake");
  assert.ok(exported && exported.width === 256 && exported.levels === 9, "the wake texture: 256 squared with nine mips");
  const params = runner.exportParams();
  for (const name of ["wakeCamRelX", "wakeCamRelZ", "wakeOriginModX", "wakeOriginModZ", "wakeCell", "wakeOn"]) assert.ok(name in params, name + " is exported for the surface");
  assert.equal(params.wakeOn, 1);
  assert.equal(params.wakeCell, 4);
  // The window follows the hull in the water frame: a node 1 km away moves wakeCamRel with it.
  poses.ship = { x: 1120, z: -40, heading: Math.PI, speed: 9 };
  hull();
  step(1.04);
  assert.ok(Math.abs(runner.exportParams().wakeCamRelX - params.wakeCamRelX) > 500);
  runner.setValue("wakeEnabled", 0);
  const off = labels().length;
  step(1.06);
  assert.ok(!labels().slice(off).some((l) => /^wake\./.test(l)), "switching the wake off stops its passes");
});

test("the wake (WebGL2): the wake passes and their width copies run only while it is on, and the exported texture carries mips", { skip: skipFor("webgl2") }, () => {
  const context = runnerContext(), g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
  const gl = new RecordingGL();
  const poses = { ship: { x: 0, z: 0, heading: Math.PI, speed: 8 } };
  const runner = g.create({ module, backend: "webgl2", tier: "high", host: { gl, caps: floatCaps, textureCache: { _sceneTextureEpoch: 0 }, nodePose: (id) => poses[id] } });
  assert.equal(runner.failed, null, String(runner.error && runner.error.stack));
  const frame = (t) => ({ time: t, dt: 1 / 60, cameraX: 0, cameraY: 30, cameraZ: 0 });
  assert.equal(runner.run(frame(1)), true, String(runner.error && runner.error.stack));
  const off = gl.draws.length;
  runner.setValue("wakeEnabled", 1);
  assert.equal(runner.run(frame(1.02)), true, String(runner.error && runner.error.stack));
  assert.ok(gl.draws.length - off > 40, "a wake adds many gather passes: " + (gl.draws.length - off));
  const wake = runner.export("wake");
  assert.ok(wake && wake.width === 256 && wake.levels === 9);
  assert.ok(gl.mipmaps.includes(wake.tex), "its mips are generated");
});
