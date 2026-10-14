"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const path = require("node:path");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const { createBoardWebGPUHarness, makeFakeGPUDevice, flushAsyncWork, deviceCallSnapshot, deviceCallDelta } = require("./runtime-test-harness.js");
const viewport = { width: 64, height: 64 };
const camera = { x: 0, y: 2, z: 12, fov: 60, near: 0.05, far: 200 };
const lights = [0, 1].map(i => ({ kind: "directional", castShadow: true, directionX: i ? 1 : -1, directionY: -2, directionZ: -1 }));
async function draw(h, bundle) {
  const before = deviceCallSnapshot(h.fake);
  const buffers = h.fake.state.buffers.length;
  const start = h.fake.state.renderPasses.length;
  const result = h.renderer.render(bundle, viewport, { nowMS: 16, active: true });
  await flushAsyncWork();
  return { ...deviceCallDelta(before, deviceCallSnapshot(h.fake)), result, buffers: h.fake.state.buffers.length - buffers,
    pending: result === "pending" ? 1 : 0, passes: h.fake.state.renderPasses.slice(start) };
}
function bakedBundle(h, count, shadow = false, retained = false, mixed = false) {
  const api = h.env.context.__gosx_scene3d_api;
  const objects = Array.from({ length: count }, (_, i) => api.normalizeSceneObject({ id: "box" + i, kind: "box", x: i * 0.1, color: "#88ccff", wireframe: false, castShadow: shadow }, i, null));
  if (mixed) objects[1] = api.normalizeSceneObject({ id: "retained", kind: "gltf-mesh", wireframe: false,
    vertices: { positions: [-1,-1,0, 1,-1,0, 0,1,0], normals: [0,0,1, 0,0,1, 0,0,1], uvs: [0,0, 1,0, .5,1], count: 3, immutable: true, revision: 0 } }, 1, null);
  return api.createSceneRenderBundle(64, 64, "#000000", camera, objects, [], [], [], shadow ? lights : [], {}, 0, [], [], [], [], [], 0, false, { retainedGeometry: retained });
}
test("WebGPU static coloured instances upload zero bytes; edits upload once across shadow and main passes", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  h.env.context.__gosx_scene3d_webgpu_builtin_cull = false;
  const F32 = vm.runInContext("Float32Array", h.env.context);
  const transforms = new F32(1000 * 16);
  for (let i = 0; i < 1000; i++) { const b = i * 16; transforms[b] = transforms[b + 5] = transforms[b + 10] = transforms[b + 15] = 1; }
  const colors = Array(1000).fill("#88ccff");
  const frame = () => ({ camera, environment: {}, materials: [{ color: "#ffffff" }], lights,
    instancedMeshes: [{ id: "crowd", kind: "box", count: 1000, transforms, colors, materialIndex: 0, castShadow: true }] });
  try {
    await draw(h, frame());
    const start = h.fake.state.writeBufferCalls.length;
    const cost = await draw(h, frame());
    const large = () => h.fake.state.writeBufferCalls.slice(start).filter(c => c.data.byteLength >= 16000);
    console.log("static coloured 1000 with two shadows", JSON.stringify({ ...cost, passes: undefined }), "instance bytes", large().reduce((s, c) => s + c.data.byteLength, 0));
    assert.equal(large().length, 0);
    assert.equal(cost.buffers, 0);
    transforms[12] = 3;
    const moveStart = h.fake.state.writeBufferCalls.length;
    await draw(h, frame());
    const moved = h.fake.state.writeBufferCalls.slice(moveStart).filter(c => c.data.byteLength >= 16000);
    assert.deepEqual(moved.map(c => c.data.byteLength), [64000]);
    assert.equal(moved[0].data[12], 3);
    colors[4] = "#ff0000";
    const colorStart = h.fake.state.writeBufferCalls.length;
    await draw(h, frame());
    const recolored = h.fake.state.writeBufferCalls.slice(colorStart).filter(c => c.data.byteLength >= 16000);
    assert.deepEqual(recolored.map(c => c.data.byteLength), [16000]);
    assert.equal(recolored[0].data[16], 1);
    assert.equal(recolored[0].data[17], 0);
  } finally { h.renderer.dispose(); }
});
test("WebGPU world-baked meshes bind four shared buffers and draw distinct first vertices", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const draws = [];
  const original = h.fake.device.createCommandEncoder.bind(h.fake.device);
  h.fake.device.createCommandEncoder = (...args) => {
    const encoder = original(...args);
    const begin = encoder.beginRenderPass.bind(encoder);
    encoder.beginRenderPass = desc => {
      const pass = begin(desc), draw = pass.draw.bind(pass);
      pass.draw = (...values) => { draws.push(values); draw(...values); };
      return pass;
    };
    return encoder;
  };
  try {
    const bundle = bakedBundle(h, 100);
    await draw(h, bundle);
    draws.length = 0;
    const cost = await draw(h, bakedBundle(h, 100));
    const bindings = cost.passes.reduce((n, p) => n + p.vertexBuffers.length, 0);
    console.log("100 baked meshes", JSON.stringify({ ...cost, passes: undefined, vertexBindings: bindings }));
    assert.equal(bindings, 4);
    assert.deepEqual(draws.map(args => args[2]), Array.from(bundle.meshObjects, obj => obj.vertexOffset));
  } finally { h.renderer.dispose(); }
});
test("WebGPU shadow bind groups survive frames and rebuild when the arena grows", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    await draw(h, bakedBundle(h, 2, true));
    const cost = await draw(h, bakedBundle(h, 2, true));
    console.log("two shadow lights", JSON.stringify({ ...cost, passes: undefined }));
    const groups = cost.passes.filter(p => p.descriptor.colorAttachments.length === 0).map(p => p.bindGroups[0].group);
    assert.equal(groups.length, 2);
    assert.equal(groups[0], groups[1], "light offsets share the same frame bind group");
    assert.equal(cost.bindGroups, 0);
    const grown = await draw(h, bakedBundle(h, 40, true));
    const newGroups = grown.passes.filter(p => p.descriptor.colorAttachments.length === 0).map(p => p.bindGroups[0].group);
    assert.ok(newGroups.every((g, i) => g !== groups[i]));
    assert.ok(newGroups.every(g => !g.desc.entries[0].resource.buffer.destroyed));
  } finally { h.renderer.dispose(); }
});

for (const builtinCull of [false, true]) for (const retired of ["scratch", "transferred"]) {
  test(`WebGPU ${builtinCull ? "indirect" : "draw-all"} colours use the current equal-content array after the old array is ${retired}`, async () => {
    const h = await createBoardWebGPUHarness({ fresh: true });
    h.env.context.__gosx_scene3d_webgpu_builtin_cull = builtinCull;
    const F32 = vm.runInContext("Float32Array", h.env.context), count = 1000;
    const transforms = new F32(count * 16), old = new F32(count * 4).fill(1);
    for (let i = 0; i < count; i++) transforms[i * 16] = transforms[i * 16 + 5] = transforms[i * 16 + 10] = transforms[i * 16 + 15] = 1;
    let colors = old, revision = 0;
    const frame = () => ({ camera, environment: {}, materials: [{ color: "#ffffff" }], lights, gpuDriven: builtinCull ? {} : null,
      instancedMeshes: [{ id: "retired-colours", kind: "box", count, transforms, colors, _instanceStreamRevision: revision, materialIndex: 0, castShadow: true }] });
    try {
      for (let warm = 0; warm < 3; warm++) await draw(h, frame());
      colors = old.slice();
      if (retired === "scratch") old.fill(0);
      else structuredClone(old.buffer, { transfer: [old.buffer] });
      for (let turn = 0; turn < 2; turn++) {
        transforms[12] = turn + 1; // Force indirect repacking even though current colours match.
        revision++;
        const start = h.fake.state.writeBufferCalls.length;
        const cost = await draw(h, frame());
        assert.notEqual(cost.result, "failed");
        const writes = h.fake.state.writeBufferCalls.slice(start);
        assert.equal(writes.filter(w => w.data.length === count * 4).length, 0, "unchanged RGBA needs no upload");
        if (builtinCull) {
          const packed = writes.find(w => !ArrayBuffer.isView(w.data) && w.data.byteLength >= count * 96);
          assert.ok(packed, "moved indirect instances must repack");
          const values = new Float32Array(packed.data);
          assert.deepEqual(Array.from(values.slice(16, 20)), [1, 1, 1, 1]);
        }
      }
    } finally { h.renderer.dispose(); }
  });
}

function uploadHelperHarness() {
  const source = readSceneRendererBackendSrc("webgpu");
  const planner = fs.readFileSync(path.join(__dirname, "bootstrap-src/15b-scene-planner.ts"), "utf8");
  function take(text, name) {
    const start = text.indexOf("function " + name + "(");
    assert.ok(start >= 0, name);
    const next = /\n\s*function [\w$]+\(/.exec(text.slice(start + 1));
    return text.slice(start, next ? start + 1 + next.index : text.length);
  }
  const fake = makeFakeGPUDevice({}), context = vm.createContext({ device: fake.device, GPUBufferUsage: { VERTEX: 32, COPY_DST: 8 },
    pointsEntryGPUBuffers: new Set(), pbrSceneAttributeCache: {}, instancedCacheOwnerEpoch: 1, scans: 0 });
  vm.runInContext([...["wgpuArrayContentsMatch", "wgpuAlignUp", "wgpuCreateBuffer", "wgpuCreateTrackedBuffer", "wgpuTrackedBufferSize", "wgpuUploadTrackedBuffer", "wgpuStablePBRAttributeBuffer", "wgpuCachedTrackedBuffer"].map(n => take(source, n)),
    take(planner, "sceneCachedBuffer"), take(planner, "sceneCachedSlotBuffer"),
    "var match = wgpuArrayContentsMatch; wgpuArrayContentsMatch = function(a,b) { scans++; return match(a,b); };"].join("\n"), context);
  return { fake, context, F32: vm.runInContext("Float32Array", context) };
}

test("WebGPU upload snapshots scan once per frame and identity, reuse storage, and upload changed bytes", () => {
  const { fake, context: c, F32 } = uploadHelperHarness(), owner = {}, values = new F32(16000);
  const upload = data => c.wgpuStablePBRAttributeBuffer("positions", data, owner, "positionsB");
  upload(values);
  const snapshot = owner.positions;
  upload(values); upload(values); upload(values);
  assert.equal(c.scans, 1, "main, shadows and bundle recording share one content scan");
  assert.equal(fake.state.writeBufferCalls.length, 1);
  c.instancedCacheOwnerEpoch++;
  values[12] = 3;
  upload(values); upload(values); upload(values);
  assert.equal(c.scans, 2);
  assert.equal(owner.positions, snapshot, "same-size changes reuse the owned snapshot");
  assert.equal(fake.state.writeBufferCalls.length, 2, "same snapshot identity still uploads changed bytes");
  assert.equal(fake.state.writeBufferCalls.at(-1).data[12], 3);
  const equal = values.slice();
  upload(equal);
  assert.equal(c.scans, 3, "a different data identity is checked in the same frame");
  assert.equal(fake.state.writeBufferCalls.length, 2);
  c.instancedCacheOwnerEpoch++;
  equal[12] = 4; upload(equal);
  assert.equal(fake.state.writeBufferCalls.at(-1).data[12], 4);
  const grown = new F32(32000); grown.set(equal); upload(grown);
  assert.notEqual(owner.positions, snapshot);
  assert.equal(fake.state.writeBufferCalls.at(-1).data.length, grown.length);
  c.pointsEntryGPUBuffers.clear(); upload(grown);
  assert.equal(fake.state.writeBufferCalls.length, 5, "a stale renderer buffer is recreated even in the cached frame");
});

test("WebGPU typed content comparison preserves NaN payloads, signed zero and view offsets", () => {
  const { context: c } = uploadHelperHarness();
  vm.runInContext("var a = new Float32Array(3), b = new Float32Array(3); new Uint32Array(a.buffer).set([0x7fc00001, 0, 7]); new Uint32Array(b.buffer).set([0x7fc00001, 0x80000000, 7]);", c);
  assert.equal(c.wgpuArrayContentsMatch(c.a.subarray(0, 1), c.b.subarray(0, 1)), true);
  assert.equal(c.wgpuArrayContentsMatch(c.a.subarray(1, 2), c.b.subarray(1, 2)), false);
  assert.equal(c.wgpuArrayContentsMatch(c.a.subarray(2), c.b.subarray(2)), true);
  vm.runInContext("new Uint32Array(b.buffer)[0] = 0x7fc00002", c);
  assert.equal(c.wgpuArrayContentsMatch(c.a.subarray(0, 1), c.b.subarray(0, 1)), false);
  assert.equal(c.wgpuArrayContentsMatch(["red"], ["red"]), true);
});

test("WebGPU main world draws clamp negative and fractional vertex offsets", async () => {
  const firstVertices = [];
  const h = await createBoardWebGPUHarness({ fresh: true, configureDevice(device) {
    const create = device.createCommandEncoder.bind(device);
    device.createCommandEncoder = (...args) => {
      const encoder = create(...args), begin = encoder.beginRenderPass.bind(encoder);
      encoder.beginRenderPass = desc => {
        const pass = begin(desc), draw = pass.draw.bind(pass);
        pass.draw = (...values) => { firstVertices.push(values[2] || 0); return draw(...values); };
        return pass;
      };
      return encoder;
    };
  } });
  try {
    for (const offset of [-0.5, 0.5]) {
      const bundle = bakedBundle(h, 2);
      bundle.meshObjects[0].vertexOffset = offset;
      firstVertices.length = 0;
      const cost = await draw(h, bundle);
      assert.notEqual(cost.result, "failed");
      assert.equal(firstVertices[0], 0);
    }
  } finally { h.renderer.dispose(); }
});

test("WebGPU baked buffers rebind after a retained mesh changes vertex slots", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    const bundle = bakedBundle(h, 3);
    bundle.meshObjects[1] = bakedBundle(h, 3, false, true, true).meshObjects[1];
    assert.equal(bundle.meshObjects[1].retainedGeometry, true);
    // Explicit depth-write overrides preserve this interleaving under the
    // shared planner's pipeline/material grouping.
    for (const object of bundle.meshObjects) object.depthWrite = true;
    await draw(h, bundle);
    const cost = await draw(h, bundle);
    const main = cost.passes.find(p => p.descriptor.colorAttachments.length > 0);
    assert.equal(main.vertexBuffers.length, 12);
    for (let slot = 0; slot < 4; slot++) {
      assert.equal(main.vertexBuffers[slot].buffer, main.vertexBuffers[8 + slot].buffer);
      assert.notEqual(main.vertexBuffers[slot].buffer, main.vertexBuffers[4 + slot].buffer);
      assert.equal(main.vertexBuffers[8 + slot].offset, 0);
    }
  } finally { h.renderer.dispose(); }
});
