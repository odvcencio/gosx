"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const ts = require("../runtime/node_modules/typescript");

function loadSection(start, end, globals = {}) {
  const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
  const source = readSceneRendererBackendSrc("webgpu");
  const first = source.indexOf(start), last = source.indexOf(end, first);
  assert.ok(first >= 0 && last > first);
  const code = ts.transpileModule(source.slice(first, last), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
  const context = vm.createContext(globals);
  vm.runInContext(code, context);
  return context;
}

function encoderFixture() {
  const calls = [];
  const pass = { setPipeline(...args) { calls.push(["pipeline", ...args]); }, draw(...args) { calls.push(["draw", ...args]); }, end() { calls.push(["end"]); } };
  const encoder = { beginRenderPass() { return pass; }, finish() { return calls; } };
  const guard = { pending: 0, framePipelinesReady: false };
  return { calls, pass, encoder, guard };
}

test("ready frame pipelines use the native encoder and pass without recording", () => {
  const api = loadSection("  function wgpuReplayFrameCommands", "  function wgpuGuardDevice");
  const h = encoderFixture(); h.guard.framePipelinesReady = true;
  const encoder = api.wgpuTrackFrameEncoder(h.encoder, h.guard);
  assert.equal(encoder, h.encoder);
  assert.equal(encoder.beginRenderPass({ colorAttachments: [] }), h.pass);
});

test("pass wrappers are cached and preserve missing optional methods", () => {
  const api = loadSection("  function wgpuReplayFrameCommands", "  function wgpuGuardDevice");
  const h = encoderFixture();
  const pass = api.wgpuTrackFrameEncoder(h.encoder, h.guard).beginRenderPass({ colorAttachments: [] });
  assert.equal(pass.draw, pass.draw);
  assert.equal(pass.executeBundles, undefined);
  assert.equal(pass.setScissorRect, undefined);
});

test("pipeline preparation hits across orbit and resize and invalidates material variants", () => {
  let pbrCalls = 0;
  const api = loadSection("  function prepareBundlePipelines", "  function supportsBundle", {
    getSelenaPipeline: () => null,
    getObjectSelenaPipeline: () => null, getPBRPipeline: () => { pbrCalls++; return {}; }, getShadowPipeline: () => ({}),
    pbrVariant: (_ps, _gr, _dev, material) => Boolean(material && material.detail),
    sceneAffineDeterminant: () => 1, sceneWebGPUObjectReflected: () => false,
    sceneTransmissionDepthWrite: (_object, _material, depth) => depth,
    webGPUObjectIsSkinned: () => false, pipelineGuard: { pending: 0 },
  });
  const s = { initialPipelinesPrepared: false, preparedPipelineKey: "", pipeState: { targetFormat: "rgba8unorm", activeSampleCount: 1, selectiveBloom: false, pbrShadowsActive: false }, gpuRes: {}, meshState: {}, telemetryState: {} }, device = {};
  const bundle = { materials: [{}], lights: [], instancedMeshes: [], waterSystems: [] };
  const prepared = { signature: "camera-one", materialsHash: "material-one", shadowPassHash: "shadow-one", pipelineVariantHash: "opaque-ccw", pbrPasses: { opaque: [{ materialIndex: 0 }], alpha: [], additive: [] } };
  api.prepareBundlePipelines(s, device, bundle, prepared, [], false);
  const first = pbrCalls;
  api.prepareBundlePipelines(s, device, bundle, { ...prepared, signature: "orbit-and-resize" }, [], false);
  assert.equal(pbrCalls, first, "camera and viewport changes must not walk every draw again");
  api.prepareBundlePipelines(s, device, bundle, { ...prepared, materialsHash: "material-two" }, [], false);
  assert.ok(pbrCalls > first);
});


test("deferred canvas replay preserves optional methods and snapshots dynamic offsets", () => {
  const h = encoderFixture(), sentinel = {}, view = {}, offsets = new Uint32Array([16]);
  h.pass.setBindGroup = (...args) => h.calls.push(["group", ...args]);
  let acquired = 0, descriptor;
  h.encoder.beginRenderPass = desc => { descriptor = desc; return h.pass; };
  h.guard.canvasView = sentinel;
  h.guard.canvasContext = { getCurrentTexture: () => { acquired++; return { createView: () => view }; } };
  const api = loadSection("  function wgpuReplayFrameCommands", "  function wgpuGuardDevice", { GPURenderPassEncoder: { prototype: h.pass } });
  const encoder = api.wgpuTrackFrameEncoder(h.encoder, h.guard);
  const pass = encoder.beginRenderPass({ colorAttachments: [{ view: sentinel }] });
  assert.equal(pass.executeBundles, undefined);
  assert.equal(pass.setScissorRect, undefined);
  assert.equal(pass.draw, pass.draw);
  pass.setBindGroup(0, {}, offsets); offsets[0] = 32;
  pass.draw(3); pass.end();
  assert.equal(acquired, 0); assert.equal(h.calls.length, 0);
  encoder.finish();
  assert.equal(acquired, 1); assert.equal(descriptor.colorAttachments[0].view, view);
  assert.equal(h.calls[0][3][0], 16);
  assert.deepEqual(h.calls.slice(1), [["draw", 3], ["end"]]);
});

test("preparation selects drawn reflection, Selena, skin+morph and instanced cull variants", () => {
  const calls = [];
  const pipeState = { targetFormat: "rgba8unorm", activeSampleCount: 1, selectiveBloom: false, pbrShadowsActive: false }, gpuRes = {}, device = {}, getSelenaPipeline = () => null;
  const api = loadSection("  function prepareBundlePipelines", "  function supportsBundle", {
    getSelenaPipeline,
    getObjectSelenaPipeline: (_getSelena, _object, material) => material.selena ? {} : null,
    getPBRPipeline: (_ps, _dev, ...args) => calls.push(["pbr", ...args]), getShadowPipeline: (...args) => calls.push(["shadow", ...args]),
    pbrVariant: (_ps, _gr, _dev, material) => Boolean(material && material.detail),
    sceneWebGPUObjectReflected: object => !!object.directVertices && object.negative,
    sceneTransmissionDepthWrite: (object, _material, depth) => object.depthWrite ?? depth,
    webGPUObjectIsSkinned: object => !!object.skin,
    ensureElioSkinPipeline: () => calls.push(["skin"]), ensureComputedMorphPipeline: () => calls.push(["morph"]),
    instancedMeshMaterial: (mesh, materials) => materials[mesh.materialIndex], scenePBRObjectRenderPass: () => "alpha",
    instancedMeshUsesCull: mesh => !!mesh.cull,
    getPBRInstancedPipeline: (_ps, _dev, ...args) => calls.push(["instanced", ...args]), getPBRInstancedCullPipeline: (_ps, _dev, ...args) => calls.push(["cull", ...args]),
    getShadowInstancedPipeline: () => calls.push(["shadow-instanced"]), pipelineGuard: { pending: 0 },
    wgpuShadowCaster: light => !!light.castShadow,
  });
  const s = { initialPipelinesPrepared: false, preparedPipelineKey: "", pipeState, gpuRes, meshState: {}, telemetryState: {} };
  const bundle = { materials: [{}, { selena: true }], lights: [{ castShadow: true }], instancedMeshes: [{ materialIndex: 0, cull: true, depthWrite: true }] };
  const prepared = { pipelineVariantHash: "variants", materialsHash: "materials", shadowPassHash: "shadow", pbrPasses: {
    opaque: [{ materialIndex: 0, negative: true, castShadow: true }, { materialIndex: 1, skin: {}, computedMorph: {} }],
    alpha: [{ materialIndex: 0, negative: true, directVertices: true }], additive: [],
  } };
  api.prepareBundlePipelines(s, device, bundle, prepared, [], false);
  assert.deepEqual(calls.filter(c => c[0] === "pbr"), [["pbr", "opaque", true, "ccw", false], ["pbr", "alpha", false, "cw", false]]);
  assert.ok(calls.some(c => c[0] === "skin")); assert.ok(calls.some(c => c[0] === "morph"));
  assert.ok(calls.some(c => c[0] === "cull" && c[2] === true));
  const first = calls.length;
  bundle.instancedMeshes[0].depthWrite = false;
  api.prepareBundlePipelines(s, device, bundle, prepared, [], false);
  assert.ok(calls.length > first, "instanced variant changes invalidate preparation at the same mesh count");
});

test("fake async pipeline compilation settles and rejects in a microtask", async () => {
  const { makeFakeGPUDevice, makeFakeGPUDeviceForPoints } = require("./runtime-test-harness.js");
  const fake = makeFakeGPUDevice();
  let compiled = false;
  fake.device.createComputePipeline = () => { compiled = true; return {}; };
  const pending = fake.device.createComputePipelineAsync({});
  assert.ok(pending instanceof Promise); assert.equal(compiled, false);
  await pending; assert.equal(compiled, true);
  fake.device.createComputePipeline = () => { throw new Error("invalid compute"); };
  await assert.rejects(fake.device.createComputePipelineAsync({}), /invalid compute/);
  const points = makeFakeGPUDeviceForPoints();
  points.device.createRenderPipeline = () => { throw new Error("invalid render"); };
  await assert.rejects(points.device.createRenderPipelineAsync({ label: "gosx-pbr-opaque" }), /invalid render/);
});

test("the skin input buffer is exactly the packed size, so a player whose gear shrinks does not read his normals from the wrong region", () => {
  // The skin kernel takes its vertex count from arrayLength(&verts) and puts the
  // normal and tangent regions at count and 2 * count; the draw reads them at the
  // padded count it packed. A buffer left larger after the count shrank (gear
  // taken off in a substitution) made the two disagree: flat, blocky shading.
  const written = [];
  const tracked = new Set();
  const meshState = { pointsEntryGPUBuffers: tracked };
  const stream = (count, tuple) => new Float32Array(count * tuple);
  const vertices = (count) => ({ count, positions: stream(count, 3), joints: stream(count, 4), weights: stream(count, 4), normals: stream(count, 3), tangents: stream(count, 4) });
  const api = loadSection("  function webGPUElioSkinVertexBuffer", "  function webGPUElioEnsureOutputBuffer", {
    GPUBufferUsage: { STORAGE: 1, COPY_DST: 2 },
    webGPUDirectAttribute: (obj, key) => obj.vertices[key],
    webGPUDefaultAttributeData: (_state, obj, key) => obj.vertices[key],
    wgpuSameItems: (a, b) => a.length === b.length && a.every((item, i) => item === b[i]),
    wgpuPackElioSkinVertices: (_key, _count, padded) => new Uint8Array(padded * 72),
    wgpuTrackedBufferSize: (buffer) => buffer.size,
    wgpuCreateTrackedBuffer: (state, _device, _usage, size) => { const buffer = { size, destroyed: false, destroy() { this.destroyed = true; } }; state.pointsEntryGPUBuffers.add(buffer); return buffer; },
  });
  const device = { queue: { writeBuffer: (buffer, _at, bytes) => written.push([buffer.size, bytes.byteLength]) } };
  const object = { vertices: vertices(100) };
  const first = api.webGPUElioSkinVertexBuffer(meshState, device, object, 100, 128, 5);
  assert.equal(first.size, 128 * 72);
  object.vertices = Object.assign(vertices(60), { _gosxWGPUElioSkinVertexData: object.vertices._gosxWGPUElioSkinVertexData });
  const smaller = api.webGPUElioSkinVertexBuffer(meshState, device, object, 60, 64, 5);
  assert.equal(smaller.size, 64 * 72, "the buffer is the new packed size, not the old larger one");
  assert.equal(first.destroyed, true, "the larger buffer is released");
  assert.ok(!tracked.has(first));
  assert.deepEqual(written.at(-1), [64 * 72, 64 * 72]);
});
