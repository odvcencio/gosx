"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const rendererSource = readSceneRendererBackendSrc("webgl");

function fixture() {
  const source = rendererSource;
  const start = source.indexOf("function uploadInstancedStream(");
  const end = source.indexOf("function bindInstancedVertexAttribute(", start);
  assert.ok(start > 0 && end > start);
  const calls = [], storage = new Map();
  let bound, sequence = 0, arrayAllocations = 0;
  const context = {
    instancedStreamRecords: new Map(), pointsEntryBuffers: new Set(), instancedStreamEpoch: 1,
    Float32Array, Uint32Array: new Proxy(Uint32Array, { construct(target, args) { arrayAllocations++; return Reflect.construct(target, args); } }),
    gl: {
      ARRAY_BUFFER: 1, DYNAMIC_DRAW: 2,
      createBuffer: () => ({ id: ++sequence }),
      bindBuffer: (_target, buffer) => { bound = buffer; },
      bufferData: (_target, data) => {
        const bytes = typeof data === "number" ? new Uint8Array(data) : new Uint8Array(data.buffer, data.byteOffset, data.byteLength).slice();
        storage.set(bound, bytes); calls.push({ kind: "allocate", buffer: bound, bytes: bytes.length });
      },
      bufferSubData: (_target, offset, data) => {
        const bytes = new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
        storage.get(bound).set(bytes, offset); calls.push({ kind: "upload", buffer: bound, offset, bytes: bytes.length });
      },
      deleteBuffer: buffer => { storage.delete(buffer); calls.push({ kind: "delete", buffer }); },
    },
  };
  vm.runInNewContext(source.slice(start, end), context);
  const upload = (data, options = {}) => context.uploadInstancedStream({ id: options.id || "instances" }, 0, options.slot || "transforms", data, options.length, options.dirty);
  const equalGPU = (buffer, data) => assert.deepEqual(storage.get(buffer), new Uint8Array(data.buffer, data.byteOffset, data.byteLength));
  return { context, calls, storage, upload, equalGPU, allocations: () => arrayAllocations };
}

test("static instance streams skip uploads across frames and fresh equal arrays", () => {
  const h = fixture(), data = Float32Array.from({ length: 16000 }, (_v, i) => i % 17);
  const buffer = h.upload(data), mark = h.calls.length;
  for (let frame = 0; frame < 60; frame++) {
    h.context.instancedStreamEpoch++;
    assert.equal(h.upload(frame % 2 ? data : data.slice()), buffer);
  }
  assert.deepEqual(h.calls.slice(mark), [], "camera-only redraws must not re-upload static matrices");
  h.equalGPU(buffer, data);
});

test("in-place matrix and colour edits upload only their changed byte span", () => {
  const h = fixture(), matrices = new Float32Array(16000), colors = new Float32Array(4000).fill(1);
  const transformBuffer = h.upload(matrices), colorBuffer = h.upload(colors, { slot: "colors" });
  h.calls.length = 0;
  matrices[16 * 300 + 12] = 4;
  h.upload(matrices);
  colors[4 * 300 + 1] = 0;
  colors[4 * 300 + 2] = 0.5;
  h.upload(colors, { slot: "colors" });
  assert.deepEqual(h.calls.map(({ kind, offset, bytes }) => [kind, offset, bytes]), [["upload", (16 * 300 + 12) * 4, 4], ["upload", (4 * 300 + 1) * 4, 8]]);
  h.equalGPU(transformBuffer, matrices); h.equalGPU(colorBuffer, colors);
});

test("cached comparisons preserve signed zero and NaN payload bits", () => {
  const h = fixture(), data = new Float32Array(4), bits = new Uint32Array(data.buffer);
  bits.set([0x80000000, 0x7fc00001, 0x3f800000, 0]);
  const buffer = h.upload(data); h.calls.length = 0;
  h.upload(data);
  assert.equal(h.calls.length, 0, "an identical NaN payload is unchanged storage");
  bits[0] = 0; h.upload(data);
  bits[1] = 0x7fc00002; h.upload(data);
  assert.deepEqual(h.calls.map(({ offset, bytes }) => [offset, bytes]), [[0, 4], [4, 4]]);
  h.equalGPU(buffer, data);
});

test("active prefixes leave the tail untouched until it is needed", () => {
  const h = fixture(), data = Float32Array.from({ length: 16 }, (_v, i) => i + 1);
  const buffer = h.upload(data, { length: 4 }); h.calls.length = 0;
  data[8] = 99;
  h.upload(data, { length: 4 });
  assert.equal(h.calls.length, 0);
  assert.notDeepEqual(h.storage.get(buffer), new Uint8Array(data.buffer));
  h.upload(data, { length: 10 });
  assert.deepEqual(h.calls.map(({ offset, bytes }) => [offset, bytes]), [[32, 4]]);
  h.equalGPU(buffer, data);
});

test("explicit dirty ranges preserve partially initialized buffers and later full uploads", () => {
  const h = fixture(), data = Float32Array.from({ length: 16 }, (_v, i) => i + 1);
  const buffer = h.upload(data, { dirty: [4, 8] });
  assert.equal(h.calls[0].bytes, data.byteLength);
  assert.deepEqual(h.calls.slice(1).map(({ offset, bytes }) => [offset, bytes]), [[16, 16]]);
  h.upload(data);
  h.equalGPU(buffer, data);
  data[3] = 99; h.upload(data, { dirty: [3, 4] });
  data[8] = 55; h.upload(data);
  h.equalGPU(buffer, data);
});

test("resizing retains the GPU handle and uploads complete new storage", () => {
  const h = fixture(), first = new Float32Array(16).fill(1), smaller = new Float32Array(8).fill(2), larger = new Float32Array(32).fill(3);
  const buffer = h.upload(first);
  assert.equal(h.upload(smaller), buffer); h.equalGPU(buffer, smaller);
  assert.equal(h.upload(larger), buffer); h.equalGPU(buffer, larger);
  assert.equal(h.calls.filter(c => c.kind === "allocate").length, 3);
});

test("unchanged streams stay alive, absent streams retire and re-create correctly", () => {
  const h = fixture(), data = new Float32Array(16).fill(1);
  const first = h.upload(data);
  h.context.instancedStreamEpoch++;
  h.upload(data); h.context.retireInstancedStreams();
  assert.equal(h.storage.has(first), true);
  h.context.instancedStreamEpoch++; h.context.retireInstancedStreams();
  assert.equal(h.storage.has(first), false);
  assert.equal(h.context.instancedStreamRecords.size, 0);
  assert.equal(h.context.pointsEntryBuffers.size, 0);
  const second = h.upload(data);
  assert.notEqual(second, first); h.equalGPU(second, data);
});

test("stable input and in-place edits reuse owned comparison storage", () => {
  const h = fixture(), data = new Float32Array(16000);
  h.upload(data); const allocations = h.allocations();
  for (let frame = 0; frame < 60; frame++) {
    data[12] = frame;
    h.upload(data);
  }
  assert.equal(h.allocations(), allocations, "steady frames must not allocate comparison arrays");
});


test("equal views reuse comparison storage while offsets remain distinct", () => {
  const h = fixture(), data = new Float32Array(32).fill(1);
  data[24] = 2;
  const first = data.subarray(0, 16), second = data.subarray(16);
  const buffer = h.upload(first), allocated = h.allocations();
  for (let frame = 0; frame < 60; frame++) h.upload(data.subarray(0, 16));
  assert.equal(h.allocations(), allocated);
  h.upload(second); h.equalGPU(buffer, second);
});

test("length-tracking inputs refresh their views after backing storage grows", { skip: typeof ArrayBuffer.prototype.resize !== "function" }, () => {
  const h = fixture(), backing = new ArrayBuffer(64, { maxByteLength: 256 }), data = new Float32Array(backing);
  data.fill(1); const buffer = h.upload(data);
  backing.resize(128); data[20] = 2;
  h.upload(data); h.equalGPU(buffer, data); h.calls.length = 0;
  data[24] = 3; h.upload(data);
  assert.deepEqual(h.calls.map(({ offset, bytes }) => [offset, bytes]), [[96, 4]]);
  h.equalGPU(buffer, data);
  backing.resize(32); h.upload(data);
  data[5] = 4; h.upload(data); h.equalGPU(buffer, data);
});

test("other element widths retain the existing upload behavior", () => {
  const h = fixture(), data = new Uint16Array(8).fill(7), buffer = h.upload(data);
  data[3] = 9; h.upload(data); h.equalGPU(buffer, data);
});

function rendererFixture(count = 1000) {
  const { createWebGLRendererForPost } = require("./runtime-test-harness.js");
  const h = createWebGLRendererForPost({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", h.env.context);
  const transforms = new F32(count * 16), colors = new F32(count * 4).fill(1);
  for (let i = 0; i < count; i++) {
    for (const diagonal of [0, 5, 10, 15]) transforms[i * 16 + diagonal] = 1;
    transforms[i * 16 + 12] = i % 20 - 10;
    transforms[i * 16 + 14] = -Math.floor(i / 20);
  }
  const state = api.createSceneState({ scene: { instancedMeshes: [{
    id: "rendered-instances", count, kind: "box", width: .2, height: .2, depth: .2,
    transforms: Array.from(transforms), colors: Array.from(colors), color: "#ffffff", castShadow: false, receiveShadow: false,
  }] } }, { tier: "full" });
  const gl = h.canvas.getContext("webgl2"), calls = [], buffers = new Set();
  let bound;
  const bind = gl.bindBuffer.bind(gl), allocate = gl.bufferData.bind(gl), upload = gl.bufferSubData.bind(gl);
  gl.bindBuffer = (target, buffer) => { if (target === gl.ARRAY_BUFFER) bound = buffer; return bind(target, buffer); };
  gl.bufferData = (target, data, usage) => {
    if (target === gl.ARRAY_BUFFER && data && (data.byteLength === count * 64 || data.byteLength === count * 16)) {
      buffers.add(bound); calls.push({ kind: "allocate", buffer: bound, bytes: data.byteLength });
    }
    return allocate(target, data, usage);
  };
  gl.bufferSubData = (target, offset, data) => {
    if (target === gl.ARRAY_BUFFER && buffers.has(bound)) calls.push({ kind: "upload", buffer: bound, offset, bytes: data.byteLength });
    return upload(target, offset, data);
  };
  const render = frame => {
    const meshes = api.sceneStateInstancedMeshesWithMaterials(state);
    const bundle = api.createSceneRenderBundle(320, 180, "#000000",
      { x: frame * .01, y: 2, z: 12, fov: 60, near: .05, far: 200 },
      [], [], [], [], [], {}, frame / 60, [], meshes, [], [], [], 0, false);
    h.renderer.render(bundle, { cssWidth: 320, cssHeight: 180, width: 320, height: 180 });
    return bundle;
  };
  return { ...h, api, state, calls, buffers, render };
}

test("complete WebGL2 renderer draws camera-only frames without instance transfers", () => {
  const h = rendererFixture();
  const first = h.render(0);
  assert.equal(first.instancedMeshes.length, 1);
  assert.equal(h.buffers.size, 2, "the test must observe matrix and colour buffers");
  h.calls.length = 0;
  for (let frame = 1; frame <= 6; frame++) h.render(frame);
  assert.deepEqual(h.calls, [], "fresh bundle records must share retained instance streams");
  assert.ok(h.canvas.getContext("webgl2").ops.some(op => op[0] === "drawArraysInstanced" || op[0] === "drawElementsInstanced"));
  h.renderer.dispose();
});

test("complete WebGL2 renderer detects sparse edits without allocating another stream", () => {
  const h = rendererFixture(); h.render(0); h.calls.length = 0;
  const mesh = h.state.instancedMeshes[0];
  mesh.transforms[16 * 300 + 12] = 4;
  mesh.colors[4 * 300 + 1] = .5;
  h.render(1);
  assert.deepEqual(h.calls.map(({ kind, offset, bytes }) => [kind, offset, bytes]),
    [["upload", (16 * 300 + 12) * 4, 4], ["upload", (4 * 300 + 1) * 4, 4]]);
  assert.equal(h.buffers.size, 2);
  h.renderer.dispose();
});
