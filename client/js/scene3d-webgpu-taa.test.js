"use strict";
// WebGPU temporal anti-aliasing: jitter, history lifetime, motion vectors for
// moving meshes, the particle and water reactive masks, and the FXAA fallback.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const ts = require("../runtime/node_modules/typescript");
const { createBoardWebGPUHarness, makePointsBundle, flushAsyncWork } = require("./runtime-test-harness.js");

function extract(file, name) {
  const source = fs.readFileSync(path.join(__dirname, file), "utf8");
  const start = source.indexOf("function " + name + "(");
  return source.slice(start, source.indexOf("\n  }\n", start) + 4);
}

function loadTAA() {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "taa-wgpu.ts"), "utf8");
  const context = { GPUTextureUsage: { RENDER_ATTACHMENT: 16, TEXTURE_BINDING: 4 }, GPUBufferUsage: { UNIFORM: 64, COPY_DST: 8, VERTEX: 32 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2 }, Float32Array, Math, JSON, Map, Set };
  vm.createContext(context);
  vm.runInContext([
    "function sceneNumber(v, f) { const n = Number(v); return Number.isFinite(n) ? n : f; }",
    "function wgpuCreateValidatedPipeline(device, kind, desc) { return { desc }; }",
    "function wgpuRequirePipeline(p) { return p; }",
    "var WGSL_FRAME_STRUCTS = '', WGSL_POINTS_UNIFORM_FIELDS = '', WGSL_POINTS_SHADE = '';",
    extract("bootstrap-src/11-scene-math.ts", "sceneMat4MultiplyInto"),
    ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText,
    "globalThis.taa = { wgpuCreateTemporalAA, sceneWebGPUTAAJitter, sceneWebGPUInvertMat4, sceneWebGPUTAAWaterRegions };",
  ].join("\n"), context);
  return context.taa;
}

const api = loadTAA();

function stubDevice() {
  const writes = [];
  const object = (kind) => (desc) => ({ kind, desc, size: desc.size, createView: () => ({ kind: kind + "-view" }), destroy() { this.destroyed = true; } });
  return {
    writes,
    createTexture: object("texture"), createBuffer: object("buffer"), createBindGroup: object("group"),
    createBindGroupLayout: object("layout"), createPipelineLayout: object("pipelineLayout"), createShaderModule: object("module"),
    queue: { writeBuffer: (buffer, offset, data) => writes.push({ buffer, offset, length: data.length }) },
  };
}

function stubPass(log) {
  return { setPipeline() {}, setBindGroup() {}, setVertexBuffer(slot, buffer, offset, size) { log.push(["vertex", slot, buffer, offset, size]); },
    draw(n, i) { log.push(["draw", n, i]); }, drawIndexed(n) { log.push(["drawIndexed", n]); }, end() {} };
}

function stubEncoder(log) {
  return { beginRenderPass: (desc) => { log.push(["pass", desc.label]); return stubPass(log); }, copyBufferToBuffer: (...args) => log.push(["copy", ...args.slice(1)]) };
}

const identity = () => new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

test("jitter moves the image by the returned uv offset and leaves depth alone", () => {
  const projection = new Float32Array(32);
  projection.set([1.2, 0, 0, 0, 0, 1.6, 0, 0, 0, 0, -0.5, -1, 0, 0, -0.05, 0], 16);
  const before = projection.slice();
  const [ux, uy] = api.sceneWebGPUTAAJitter(projection, 16, 320, 180, 0);
  // A point straight ahead at view z = -4: clip w = 4.
  const clipBefore = [before[16 + 8] * -4 + before[16 + 12], before[16 + 9] * -4 + before[16 + 13], 4];
  const clipAfter = [projection[16 + 8] * -4 + projection[16 + 12], projection[16 + 9] * -4 + projection[16 + 13], 4];
  const uvShift = [(clipAfter[0] - clipBefore[0]) / 4 * 0.5, -(clipAfter[1] - clipBefore[1]) / 4 * 0.5];
  assert.ok(Math.abs(uvShift[0] - ux) < 1e-9 && Math.abs(uvShift[1] - uy) < 1e-9);
  assert.ok(Math.abs(ux) <= 0.5 / 320 && Math.abs(uy) <= 0.5 / 180, "jitter stays inside one pixel");
  assert.deepEqual(Array.from(projection.slice(16 + 2, 16 + 3)), Array.from(before.slice(16 + 2, 16 + 3)), "depth row unchanged");
});

test("the matrix inverse round-trips a view-projection", () => {
  const m = new Float32Array([1.2, 0.1, 0, 0, -0.1, 1.6, 0.2, 0, 0, 0.3, -0.5, -1, 0.4, -0.2, -0.05, 0.1]);
  const inv = new Float32Array(16);
  assert.equal(api.sceneWebGPUInvertMat4(m, inv), true);
  const product = new Float32Array(16);
  for (let c = 0; c < 4; c++) for (let r = 0; r < 4; r++) for (let k = 0; k < 4; k++) product[c * 4 + r] += m[k * 4 + r] * inv[c * 4 + k];
  for (let i = 0; i < 16; i++) assert.ok(Math.abs(product[i] - (i % 5 === 0 ? 1 : 0)) < 1e-4, `entry ${i}: ${product[i]}`);
});

test("water regions pack the ocean band and up to four pools", () => {
  const out = new Float32Array(24);
  api.sceneWebGPUTAAWaterRegions({ environment: { ocean: { level: 2, waveHeight: 1 } }, waterSystems: [{ poolWidth: 3, poolLength: 4, poolHeight: 1 }] }, out, 0);
  assert.deepEqual(Array.from(out.slice(0, 3)), [2, 0.75, 1]);
  assert.deepEqual(Array.from(out.slice(4, 8)).map((v) => Math.round(v * 100) / 100), [3, 4, 0.22, 1]);
  assert.equal(out[11], 0, "absent pools are off");
});

function frame(view, extra = {}) {
  const projection = new Float32Array(32);
  projection.set([1.2, 0, 0, 0, 0, 1.6, 0, 0, 0, 0, -0.5, -1, 0, 0, -0.05, 0], 16);
  return Object.assign({ postProcessing: true, perspective: true, width: 320, height: 180, samples: 1, view, projection, projectionOffset: 16, viewProjection: identity(), now: 0 }, extra);
}

test("a moving retained mesh and a skinned mesh draw motion vectors; still meshes do not", () => {
  const device = stubDevice(), log = [];
  const skin = { count: 900, outputBuffer: { size: 960 * 40 } }, positions = { tag: "positions" };
  const temporal = api.wgpuCreateTemporalAA(() => ({
    device, linearSampler: {}, materialGroup: () => ({}), previousSkin: (obj) => obj._gosxTAAPreviousSkin || null,
    skinRecord: (obj) => obj.skinned ? skin : null,
    bindPositions: (pass, slot) => { pass.setVertexBuffer(slot, positions); return true; }, bindIndices: () => 36,
  }));
  const moving = { id: "crate", retainedGeometry: true, modelMatrix: identity(), vertexCount: 24 };
  const still = { id: "wall", retainedGeometry: true, modelMatrix: identity(), vertexCount: 24 };
  const hero = { id: "hero", skinned: true, retainedGeometry: true, modelMatrix: identity(), vertexCount: 900 };
  const effects = [{ kind: "taa" }];
  const resolveFrame = { colorView: {}, colorFormat: "rgba16float", depthView: {}, samples: 1, objects: [moving, still, hero], particles: [], frameGroup: {}, bundle: {} };
  for (let i = 0; i < 3; i++) {
    log.length = 0;
    temporal.beginFrame(effects, frame(identity(), { now: i * 16 }));
    temporal.capturePreviousSkins(stubEncoder(log), resolveFrame.objects);
    if (i === 2) moving.modelMatrix[12] += 0.25;
    const chain = temporal.resolve(stubEncoder(log), effects, resolveFrame);
    if (i === 0) assert.deepEqual(chain, effects, "the first frame keeps the FXAA fallback");
    if (i > 0) assert.deepEqual(chain, [], "a frame with history drops taa from the post chain");
  }
  assert.equal(temporal.stats().taaMovers, 2, "the moved crate and the skinned hero draw; the wall reprojects by camera");
  assert.deepEqual(log.filter((entry) => entry[0] === "copy").map((entry) => entry[4]), [900 * 12], "only the drawn positions are copied for the next frame");
  assert.ok(log.some((entry) => entry[0] === "vertex" && entry[1] === 1 && entry[2] === hero._gosxTAAPreviousSkin && entry[3] === 0 && entry[4] === 900 * 12), "the hero's previous positions feed slot 1");
  assert.deepEqual(log.filter((entry) => entry[0] === "pass").map((entry) => entry[1]), ["gosx-taa-velocity", "gosx-taa-resolve", "gosx-taa-copy"]);
});

test("a skinned mesh keeps one previous-skin copy on its owner while its draw record is rebuilt each frame", () => {
  const made = [];
  const sized = () => {
    const device = stubDevice(), create = device.createBuffer;
    device.createBuffer = (desc) => { const buffer = create(desc); buffer.size = desc.size; made.push(buffer); return buffer; };
    return device;
  };
  let device = sized();
  const skin = { count: 900, outputBuffer: { size: 960 * 40 } }, owner = { id: "runner" };
  const temporal = api.wgpuCreateTemporalAA(() => ({
    device, linearSampler: {}, materialGroup: () => ({}), previousSkin: (obj) => (obj.resourceOwner || obj)._gosxTAAPreviousSkin || null,
    skinRecord: (obj) => obj.skinned ? skin : null, bindPositions: () => true, bindIndices: () => 0,
  }));
  const effects = [{ kind: "taa" }];
  const copies = () => made.filter((buffer) => buffer.desc && buffer.desc.label === "gosx-taa-previous-skin");
  const step = (i, vertexCount = skin.count, viewCulled = false) => {
    // The scene hands the renderer a new draw record for the same owner every frame.
    const record = { id: "runner", skinned: true, resourceOwner: owner, modelMatrix: identity(), vertexCount, viewCulled };
    const log = [], encoder = stubEncoder(log);
    temporal.beginFrame(effects, frame(identity(), { now: i * 16 }));
    temporal.capturePreviousSkins(encoder, [record]);
    skin.count = vertexCount;
    temporal.resolve(encoder, effects, { colorView: {}, colorFormat: "rgba16float", depthView: {}, samples: 1, objects: [record], particles: [], frameGroup: {}, bundle: {} });
    return log;
  };
  for (let i = 0; i < 4; i++) step(i);
  assert.equal(copies().length, 1, "one previous-skin buffer for the owner, not one per frame");
  assert.strictEqual(owner._gosxTAAPreviousSkin, copies()[0]);

  const resized = step(4, 933);
  assert.ok(resized.some((entry) => entry[0] === "vertex" && entry[1] === 1 && entry[2] === skin.outputBuffer && entry[4] === 933 * 12), "resized geometry uses current positions for missing history");
  step(5);
  assert.equal(copies().length, 2);
  assert.equal(copies()[1].size, 933 * 12);
  assert.equal(copies()[0].destroyed, true, "the replaced history buffer is released");
  assert.ok(!step(6, 933, true).some((entry) => entry[0] === "copy"), "culled meshes skip the history copy");

  device = sized();
  step(7);
  assert.equal(copies().length, 3, "a new device gets its own previous-skin buffer");
  assert.strictEqual(owner._gosxTAAPreviousSkin, copies()[2]);
});

test("a resized skin output survives until the encoding frame has submitted", async () => {
  const passes = "../runtime/scene3d/webgpu-passes.ts";
  const functions = extract(passes, "webGPUElioEnsureOutputBuffer");
  const context = {
    GPUBufferUsage: { STORAGE: 128, VERTEX: 32, COPY_DST: 8, COPY_SRC: 4 }, queueMicrotask,
    wgpuTrackedBufferSize: (buffer) => buffer.size,
    wgpuCreateTrackedBuffer(mesh, device, usage, size) {
      const buffer = device.createBuffer({ usage, size });
      mesh.pointsEntryGPUBuffers.add(buffer);
      return buffer;
    },
  };
  vm.createContext(context);
  vm.runInContext(ts.transpileModule(functions, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);

  const device = stubDevice(), mesh = { pointsEntryGPUBuffers: new Set() }, record = {};
  const copySource = context.webGPUElioEnsureOutputBuffer(mesh, device, record, 64);
  const grown = context.webGPUElioEnsureOutputBuffer(mesh, device, record, 128);
  assert.notStrictEqual(grown, copySource);
  assert.ok(!copySource.destroyed, "the frame can still submit a copy from the old output");
  await new Promise(queueMicrotask);
  assert.equal(copySource.destroyed, true);
  assert.ok(!mesh.pointsEntryGPUBuffers.has(copySource));
  assert.ok(!grown.destroyed && mesh.pointsEntryGPUBuffers.has(grown));
});

test("history resets on a camera cut, a resize and a long gap", () => {
  const device = stubDevice();
  const temporal = api.wgpuCreateTemporalAA(() => ({ device, linearSampler: {}, materialGroup: () => ({}), previousSkin: () => null, skinRecord: () => null, bindPositions: () => true, bindIndices: () => 0 }));
  const effects = [{ kind: "taa" }];
  const resolveFrame = { colorView: {}, colorFormat: "rgba16float", depthView: {}, samples: 1, objects: [], particles: [], frameGroup: {}, bundle: {} };
  const step = (view, extra) => { const valid = temporal.beginFrame(effects, frame(view, extra)); temporal.resolve(stubEncoder([]), effects, resolveFrame); return valid; };
  step(identity(), { now: 0 });
  assert.equal(step(identity(), { now: 16 }), true, "a steady camera keeps history");
  const moved = identity(); moved[12] = -5;
  assert.equal(step(moved, { now: 32 }), false, "a 5-unit jump is a cut");
  assert.equal(step(moved, { now: 48 }), true);
  assert.equal(step(moved, { now: 64, width: 640 }), false, "resize resets");
  assert.equal(step(moved, { now: 80, width: 640 }), true);
  assert.equal(step(moved, { now: 400, width: 640 }), false, "a 320 ms gap resets");
  assert.equal(temporal.stats().taaResets, 3);
});

test("WebGPU renders the temporal resolve once history exists and falls back to FXAA before", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
    bundle.postEffects = [{ kind: "toneMapping" }, { kind: "taa", historyWeight: 0.9 }];
    bundle.computeParticles = [{ id: "dust", count: 8, emitter: { kind: "sphere", radius: 1, lifetime: 2 }, material: { blendMode: "additive", size: 0.2 } }];
    const labels = [];
    for (let i = 0; i < 4; i++) {
      const start = h.fake.state.renderPasses.length;
      h.renderer.render(bundle, { width: 320, height: 180 }, { nowMS: 1000 + i * 16 });
      await flushAsyncWork();
      labels.push(h.fake.state.renderPasses.slice(start).map((pass) => pass.descriptor && pass.descriptor.label).filter(Boolean));
    }
    const resolved = labels.findIndex((list) => list.includes("gosx-taa-resolve"));
    assert.ok(resolved >= 0, "a resolve pass runs: " + JSON.stringify(labels));
    const stats = h.mount.__gosxScene3DWebGPUStats;
    assert.equal(stats.taaActive, 1);
    assert.equal(stats.taaHistoryValid, 1);
    const module = h.fake.state.shaderModules.find((m) => m.label === "taa-resolve");
    assert.match(module.code, /toYCoCg/);
    assert.match(module.code, /reactive \* 0\.85/);
    assert.ok(h.fake.state.writeBufferCalls.some((call) => call.offset === 64 && call.data && call.data.length >= 16), "the jittered projection is re-uploaded");
  } finally { h.renderer.dispose(); }
});
