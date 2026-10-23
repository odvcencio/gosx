"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { createWebGLRendererForPost, createBoardWebGPUHarness, flushAsyncWork } = require("./runtime-test-harness.js");

function scene(api, count = 1000) {
  const transforms = Array(count * 16).fill(0);
  for (let i = 0; i < count; i++) {
    for (const diagonal of [0, 5, 10, 15]) transforms[i * 16 + diagonal] = 1;
    transforms[i * 16 + 12] = i % 20 - 10;
    transforms[i * 16 + 14] = -Math.floor(i / 20);
  }
  return api.createSceneState({ scene: { instancedMeshes: [{ id: "fleet", count, kind: "box", width: .2, height: .2, depth: .2, transforms, colors: Array(count * 4).fill(1), color: "#ffffff" }] } }, { tier: "full" });
}

function bundle(api, state, frame) {
  return api.createSceneRenderBundle(320, 180, "#000000", { x: frame * .01, y: 2, z: 12, fov: 60, near: .05, far: 200 },
    [], [], [], [], [], {}, frame / 60, [], api.sceneStateInstancedMeshesWithMaterials(state), [], [], [], 0, false);
}

test("WebGL preparation retains matrix and color storage across fresh mutable records", () => {
  const h = createWebGLRendererForPost({ fresh: true }), api = h.env.context.__gosx_scene3d_api;
  const state = scene(api), transforms = new Set(), colors = new Set();
  for (let frame = 0; frame < 20; frame++) {
    const mesh = state.instancedMeshes[0];
    mesh.transforms[12] = frame;
    mesh.colors[1] = frame / 20;
    if (frame % 2) { mesh.transforms = mesh.transforms.slice(); mesh.colors = mesh.colors.slice(); }
    const b = bundle(api, state, frame);
    h.renderer.render(b, { cssWidth: 320, cssHeight: 180, width: 320, height: 180 });
    const prepared = b.instancedMeshes[0];
    assert.equal(prepared._cachedTransforms[12], Math.fround(frame));
    assert.equal(prepared._cachedInstanceColors[1], Math.fround(frame / 20));
    transforms.add(prepared._cachedTransforms.buffer); colors.add(prepared._cachedInstanceColors.buffer);
  }
  assert.equal(transforms.size, 1, "matrix backing storage must survive wrapper and source-array replacement");
  assert.equal(colors.size, 1, "color backing storage must survive wrapper and source-array replacement");
  assert.equal(state.instancedMeshes[0]._cachedTransforms, undefined, "SceneIR must not acquire renderer caches");
  h.renderer.dispose();
});

test("WebGPU preparation retains matrix storage while changed frames reach the renderer", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true }), api = h.env.context.__gosx_scene3d_api;
  const state = scene(api), transforms = new Set(); h.canvas.width = 320; h.canvas.height = 180;
  for (let frame = 0; frame < 8; frame++) {
    state.instancedMeshes[0].transforms[12] = frame;
    const b = bundle(api, state, frame);
    h.renderer.render(b, { width: 320, height: 180 }, { nowMS: frame * 16, active: true });
    await flushAsyncWork();
    const data = b.instancedMeshes[0]._cachedTransforms;
    if (data) { assert.equal(data[12], frame); transforms.add(data.buffer); }
  }
  assert.equal(transforms.size, 1, "WebGPU must reuse prepared matrices across per-frame copies");
  h.renderer.dispose();
});

let helpers;
function preparation() {
  if (!helpers) {
    const h = createWebGLRendererForPost({ fresh: true });
    helpers = { h, api: h.env.context.__gosx_scene3d_api, F32: vm.runInContext("Float32Array", h.env.context) };
  }
  return helpers;
}
test.after(() => { if (helpers) helpers.h.renderer.dispose(); });

test("prepared transforms retain coercion, signed zero and mutable typed-view semantics", () => {
  const { api, F32 } = preparation(), owner = {}, source = [1, -0, NaN, .1];
  const first = api.scenePrepareInstancedTransforms(owner, source);
  assert.deepEqual(Array.from(first), Array.from(new F32(source)));
  source[0] = 3; source[3] = .2;
  assert.equal(api.scenePrepareInstancedTransforms(owner, source), first);
  assert.deepEqual(Array.from(first), Array.from(new F32(source)));
  const backing = new F32(20), view = backing.subarray(4, 20);
  assert.equal(api.scenePrepareInstancedTransforms(owner, view), view);
  backing[4] = 9; assert.equal(api.scenePrepareInstancedTransforms(owner, view)[0], 9);
  assert.equal(api.scenePrepareInstancedTransforms(owner, null), null);
});

test("prepared colors handle palettes, mutable RGBA/RGB, count and representation changes", () => {
  const { api, F32 } = preparation(), owner = {}, palette = ["#ff0000", "#00ff00"];
  const first = api.scenePrepareInstancedColors(owner, palette, 3);
  assert.deepEqual(Array.from(first), [1,0,0,1, 0,1,0,1, 0,1,0,1]);
  assert.equal(api.scenePrepareInstancedColors(owner, palette.slice(), 3), first);
  palette[0] = "#0000ff";
  assert.equal(api.scenePrepareInstancedColors(owner, palette, 3), first);
  assert.deepEqual(Array.from(first).slice(0,4), [0,0,1,1]);
  const rgba = [1, -0, NaN, .5, 0, 1, 0, 1];
  const data = api.scenePrepareInstancedColors(owner, rgba, 2);
  assert.deepEqual(Array.from(data), Array.from(new F32(rgba)));
  rgba[1] = 0; assert.equal(api.scenePrepareInstancedColors(owner, rgba, 2), data);
  assert.equal(Object.is(data[1], -0), false);
  const rgb = [1,0,0, 0,0,1];
  assert.deepEqual(Array.from(api.scenePrepareInstancedColors(owner, rgb, 2)), [1,0,0,1, 0,0,1,1]);
  assert.deepEqual(Array.from(api.scenePrepareInstancedColors(owner, rgb, 1)), rgb);
  const packed = new F32([.5,0,0,1]);
  assert.equal(api.scenePrepareInstancedColors(owner, packed, 1), packed);
});

test("different owners cannot overwrite each other's prepared data", () => {
  const { api } = preparation(), a = {}, b = {}, source = [1,0,0, 0,0,1];
  const rgba = api.scenePrepareInstancedColors(a, source, 1);
  const rgb = api.scenePrepareInstancedColors(b, source, 2);
  assert.notEqual(rgba.buffer, rgb.buffer);
  assert.deepEqual(Array.from(rgba), source);
  assert.deepEqual(Array.from(rgb), [1,0,0,1, 0,0,1,1]);
  const transforms = api.scenePrepareInstancedTransforms(a, [1,2,3,4]);
  assert.notEqual(api.scenePrepareInstancedTransforms(b, [9,8,7,6]).buffer, transforms.buffer);
  assert.deepEqual(Array.from(transforms), [1,2,3,4]);
});

test("backend white fallback is retained, cleared and rebuilt without stale palette values", () => {
  const { api } = preparation(), owner = {};
  assert.equal(api.scenePrepareInstancedColors(owner, [], 2), null);
  const white = api.scenePrepareInstancedColors(owner, [], 2, true);
  assert.deepEqual(Array.from(white), Array(8).fill(1));
  assert.equal(api.scenePrepareInstancedColors(owner, null, 2, true), white);
  api.scenePrepareInstancedColors(owner, ["#ff0000"], 2, true);
  assert.deepEqual(Array.from(api.scenePrepareInstancedColors(owner, [], 2, true)), Array(8).fill(1));
  assert.equal(api.scenePrepareInstancedColors(owner, [], 0, true), null);
  assert.equal(api.scenePrepareInstancedColors(owner, [], 2), null);
});


test("prepared storage resizes with live data and does not retain a removed batch", () => {
  const { api: helper } = preparation(), owner = {};
  const small = helper.scenePrepareInstancedTransforms(owner, [1,2,3,4]);
  const grown = helper.scenePrepareInstancedTransforms(owner, [1,2,3,4,5,6,7,8]);
  assert.notEqual(grown.buffer, small.buffer);
  assert.deepEqual(Array.from(grown), [1,2,3,4,5,6,7,8]);
  const shrunk = helper.scenePrepareInstancedTransforms(owner, [9,8,7,6]);
  assert.equal(shrunk.length, 4); assert.notEqual(shrunk.buffer, grown.buffer);

  const h = createWebGLRendererForPost({ fresh: true }), api = h.env.context.__gosx_scene3d_api;
  const state = scene(api, 8), first = bundle(api, state, 0);
  const viewport = { cssWidth:320,cssHeight:180,width:320,height:180 };
  h.renderer.render(first, viewport);
  const data = first.instancedMeshes[0]._cachedTransforms;
  const meshes = state.instancedMeshes; state.instancedMeshes = [];
  const beforeRemoval = h.canvas.getContext("webgl2").ops.length;
  h.renderer.render(bundle(api, state, 1), viewport);
  assert.equal(h.canvas.getContext("webgl2").ops.slice(beforeRemoval).filter(op => op[0] === "deleteBuffer").length, 2, "an empty frame must free matrix and color GPU streams");
  state.instancedMeshes = meshes;
  const returned = bundle(api, state, 2); h.renderer.render(returned, viewport);
  assert.notEqual(returned.instancedMeshes[0]._cachedTransforms.buffer, data.buffer, "retirement must end CPU preparation ownership too");
  h.renderer.dispose();
});

test("WebGPU batch retirement ends CPU preparation ownership", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true }), api = h.env.context.__gosx_scene3d_api;
  h.canvas.width = 320; h.canvas.height = 180;
  const state = scene(api, 8), viewport = { width:320,height:180 };
  let prior;
  for (let i = 0; i < 3; i++) {
    const b = bundle(api, state, i); h.renderer.render(b, viewport, { nowMS:i*16,active:true });
    await flushAsyncWork(); prior = b.instancedMeshes[0]._cachedTransforms || prior;
  }
  assert.ok(prior);
  const meshes = state.instancedMeshes; state.instancedMeshes = [];
  for (let i = 3; i < 126; i++) {
    h.renderer.render(bundle(api, state, i), viewport, { nowMS:i*16,active:true }); await flushAsyncWork();
  }
  state.instancedMeshes = meshes;
  const returned = bundle(api, state, 126); h.renderer.render(returned, viewport, { nowMS:2016,active:true });
  await flushAsyncWork();
  assert.notEqual(returned.instancedMeshes[0]._cachedTransforms.buffer, prior.buffer);
  h.renderer.dispose();
});

test("coercible numeric colors are refreshed even when their object identity is stable", () => {
  const { api } = preparation(), owner = {}; let value = .25;
  const coercible = { valueOf: () => value }, colors = [coercible, 0, 0, 1];
  const first = api.scenePrepareInstancedColors(owner, colors, 1);
  assert.equal(first[0], .25); value = .75;
  assert.equal(api.scenePrepareInstancedColors(owner, colors, 1), first);
  assert.equal(first[0], .75);
});


test("palette counts resize prepared storage without holding peak capacity", () => {
  const { api } = preparation(), owner = {}, palette = ["#00ff00"];
  const a = api.scenePrepareInstancedColors(owner, palette, 1000);
  const b = api.scenePrepareInstancedColors(owner, palette, 2);
  assert.equal(a.length, 4000); assert.equal(b.length, 8);
  assert.notEqual(a.buffer, b.buffer);
  assert.deepEqual(Array.from(b), [0,1,0,1,0,1,0,1]);
  const c = api.scenePrepareInstancedColors(owner, palette, 5);
  assert.equal(c.length, 20); assert.notEqual(c.buffer, b.buffer);
});

test("numeric NaN color payloads retain constructor-equivalent bits after mutation", () => {
  const { api, F32 } = preparation(), owner = {}, source = new Float64Array([0,0,0,1]);
  const bits = new BigUint64Array(source.buffer);
  bits[0] = 0x7ff8000020000000n;
  const a = api.scenePrepareInstancedColors(owner, source, 1);
  assert.deepEqual(Array.from(new Uint32Array(a.buffer)), Array.from(new Uint32Array(new F32(source).buffer)));
  bits[0] = 0x7ff8000040000000n;
  const b = api.scenePrepareInstancedColors(owner, source, 1);
  assert.equal(a, b);
  assert.deepEqual(Array.from(new Uint32Array(b.buffer)), Array.from(new Uint32Array(new F32(source).buffer)));
});


test("a conversion failure cannot poison a previously cached palette", () => {
  const { api } = preparation(), owner = {}, palette = ["#ff0000"];
  api.scenePrepareInstancedColors(owner, palette, 1);
  const invalid = [0, 1, { valueOf() { throw new Error("transient numeric conversion"); } }, 1];
  assert.throws(() => api.scenePrepareInstancedColors(owner, invalid, 1), /transient numeric conversion/);
  assert.deepEqual(Array.from(api.scenePrepareInstancedColors(owner, palette, 1)), [1,0,0,1]);
});
