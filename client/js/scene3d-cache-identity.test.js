"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const { bootstrapRuntimeSource, createContext, freshFeatureBundleSource, runScript } = require("./runtime-test-harness.js");

function sceneAPI() {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "fresh-scene3d.js");
  return env.context.__gosx_scene3d_api;
}

test("depth cache equal-sum bounds movement changes the consumed cull result", () => {
  const api = sceneAPI();
  const owner = {};
  const camera = { x: 0, y: 0, z: 5, near: 0.1, far: 10 };
  const bounds = { minX: -1, maxX: 1, minY: -1, maxY: 1, minZ: -1, maxZ: 1 };
  assert.equal(api.sceneBoundsViewCulled(bounds, camera, owner), false);
  const initial = api.sceneBoundsDepthMetrics(bounds, camera, owner);
  assert.equal(api.sceneBoundsDepthMetrics({ ...bounds }, { ...camera }, owner), initial, "static inputs reuse the result");
  bounds.minX += 10; bounds.maxX += 10;
  bounds.minZ -= 10; bounds.maxZ -= 10;
  assert.equal(api.sceneBoundsViewCulled(bounds, camera, owner), true, "far-plane culling must consume updated depth");
  const moved = api.sceneBoundsDepthMetrics(bounds, camera, owner);
  assert.equal(moved.near, 14);
  assert.equal(moved.far, 16);
  camera.far = 20;
  assert.equal(api.sceneBoundsViewCulled(bounds, camera, owner), false, "near/far remain live culling inputs");
  assert.equal(api.sceneBoundsDepthMetrics(bounds, camera, owner), moved, "projection-only edits need no depth recomputation");
});

test("depth cache compares camera eye and rotations independently", () => {
  const api = sceneAPI();
  const bounds = { minX: -1, maxX: 1, minY: -1, maxY: 1, minZ: -1, maxZ: 1 };
  for (const [initial, changed] of [
    [{ x: 0, y: 0, z: 5 }, { x: -10, y: 0, z: 15 }],
    [{ x: 0, y: 0, z: 5, rotationX: 0, rotationY: 0 }, { x: 0, y: 0, z: 5, rotationX: Math.PI / 2, rotationY: -Math.PI / 2 }],
  ]) {
    const owner = {};
    api.sceneBoundsDepthMetrics(bounds, initial, owner);
    const camera = { ...changed, near: 0.1, far: 10 };
    const expected = api.sceneBoundsDepthMetrics(bounds, camera);
    const actual = api.sceneBoundsDepthMetrics(bounds, camera, owner);
    assert.equal(actual.near, expected.near);
    assert.equal(actual.far, expected.far);
    assert.equal(api.sceneBoundsViewCulled(bounds, camera, owner), api.sceneBoundsViewCulled(bounds, camera));
  }
});

function identity() { return new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]); }

function shadowFixture() {
  // Execute the production pass and its cache helpers. GL records actual
  // clear/draw submissions; it is not a rasterizer or performance oracle.
  const source = readSceneRendererBackendSrc("webgl");
  const start = source.indexOf("  function sceneShadowPass");
  const end = source.indexOf("  // Compile the shadow depth shader program.", start);
  assert.ok(start >= 0 && end > start);
  const context = vm.createContext({ Float32Array, Uint32Array, Number, Math,
    sceneFiniteNumber: (x, fallback) => Number.isFinite(Number(x)) ? Number(x) : fallback,
    sceneAffineDeterminant: () => 1,
  });
  vm.runInContext(source.slice(start, end), context);
  const calls = [];
  let framebufferComplete = true;
  const gl = new Proxy({}, { get: (_, name) => {
    if (name === "checkFramebufferStatus") return () => framebufferComplete ? "FRAMEBUFFER_COMPLETE" : "FRAMEBUFFER_INCOMPLETE_ATTACHMENT";
    if (/^[A-Z_]+$/.test(name)) return name;
    return (...args) => { calls.push([name, ...args]); };
  } });
  const vertices = {
    immutable: true, revision: 0, count: 4,
    positions: new Float32Array([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0]),
    indices: new Uint32Array([0, 1, 2, 0, 2, 3]),
  };
  const caster = { castShadow: true, viewCulled: false, directVertices: true, retainedGeometry: true,
    vertices, geometryRevision: 0, vertexOffset: 0, vertexCount: 4, depthNear: 4, depthFar: 6, modelMatrix: identity() };
  const resources = { framebuffer: {}, depthTexture: {}, size: 256, cascadeIndex: 0, splitNear: 0.1, splitFar: 10 };
  const program = { program: {}, attributes: { position: 0 }, uniforms: { lightViewProjection: {}, modelMatrix: {} } };
  const state = { buffer: {} };
  const bundle = { meshObjects: [caster], worldMeshPositions: new Float32Array(18) };
  let failBind = false;
  const binder = (object) => {
    calls.push(["bindCaster", Array.from(object.vertices.positions), Array.from(object.vertices.indices || [])]);
    return failBind ? null : { indexed: !!object.vertices.indices, count: object.vertices.indices ? object.vertices.indices.length : object.vertexCount };
  };
  const light = identity();
  const draw = () => context.renderSceneShadowPass(gl, program, resources, light, bundle, state, binder);
  const counts = () => ({ clear: calls.filter(c => c[0] === "clear").length, draw: calls.filter(c => c[0] === "drawElements" || c[0] === "drawArrays").length });
  return { caster, resources, program, state, bundle, light, calls, draw, counts,
    failBind: value => { failBind = value; }, complete: value => { framebufferComplete = value; } };
}

test("WebGL retained static shadow pass reuses its completed clear/draw", () => {
  const f = shadowFixture();
  f.draw();
  assert.deepEqual(f.counts(), { clear: 1, draw: 1 });
  f.caster.modelMatrix = new Float32Array(f.caster.modelMatrix);
  f.draw();
  assert.deepEqual(f.counts(), { clear: 1, draw: 1 }, "equal matrix values need no redraw");
});

test("WebGL shadow pass redraws equal-sum model and light matrix edits", () => {
  for (const target of ["model", "light"]) {
    const f = shadowFixture(); f.draw();
    const matrix = target === "model" ? f.caster.modelMatrix : f.light;
    matrix[12] += 1; matrix[13] -= 1;
    f.draw();
    assert.deepEqual(f.counts(), { clear: 2, draw: 2 }, target + " transform changed despite equal sum");
    f.draw();
    assert.deepEqual(f.counts(), { clear: 2, draw: 2 });
  }
});

test("WebGL shadow pass redraws revisions, same-AABB topology, geometry replacements and cascade inputs", () => {
  const changes = [
    f => { f.caster.vertices.indices.set([0, 1, 3, 1, 2, 3]); f.caster.geometryRevision++; },
    f => { f.caster.vertices.positions[3] = 0.5; f.caster.geometryRevision++; },
    f => { f.caster.vertices = { ...f.caster.vertices }; },
    f => { f.caster.vertices.positions = new Float32Array(f.caster.vertices.positions); },
    f => { f.caster.vertices.indices = new Uint32Array([0, 1, 3, 1, 2, 3]); },
    f => { f.caster.vertexCount--; },
    f => { f.resources.cascadeIndex++; },
    f => { f.resources.splitNear += 1; f.resources.splitFar -= 1; },
    f => { f.resources.size *= 2; },
    f => { f.resources.framebuffer = {}; },
    f => { f.resources.depthTexture = {}; },
    f => { f.program.program = {}; },
  ];
  for (const [index, change] of changes.entries()) {
    const f = shadowFixture(); f.draw(); change(f); f.draw();
    assert.deepEqual(f.counts(), { clear: 2, draw: 2 }, "change " + index);
    if (index < 2) {
      const binds = f.calls.filter(c => c[0] === "bindCaster");
      assert.notDeepEqual(binds[0], binds[1], "same-AABB edit reaches the submitted geometry");
    }
  }
});

test("WebGL shadow pass never caches mutable, unversioned or soup geometry", () => {
  for (const change of [
    f => { f.caster.vertices.immutable = false; },
    f => { f.caster.vertices.dynamic = true; },
    f => { f.caster.geometryRevision = null; },
    f => { f.caster.directVertices = false; f.caster.retainedGeometry = false; },
  ]) {
    const f = shadowFixture(); change(f); f.draw();
    f.caster.vertices.positions[3] = 0.5; // same AABB and vertex count
    f.bundle.worldMeshPositions[3] = 0.5;
    f.draw();
    assert.deepEqual(f.counts(), { clear: 2, draw: 2 });
  }
});

test("WebGL failed retained binding cannot publish a reusable shadow pass", () => {
  const f = shadowFixture();
  f.failBind(true); f.draw();
  assert.deepEqual(f.counts(), { clear: 1, draw: 0 });
  f.failBind(false); f.draw();
  assert.deepEqual(f.counts(), { clear: 2, draw: 1 });
  f.draw();
  assert.deepEqual(f.counts(), { clear: 2, draw: 1 });
});

test("WebGL invalid program or incomplete framebuffer cannot cache an unrendered pass", () => {
  const f = shadowFixture();
  f.complete(false); f.draw();
  assert.deepEqual(f.counts(), { clear: 0, draw: 0 });
  f.complete(true); f.draw();
  assert.deepEqual(f.counts(), { clear: 1, draw: 1 });
  const program = f.program.program;
  f.program.program = null; f.draw();
  f.program.program = program; f.draw();
  assert.deepEqual(f.counts(), { clear: 2, draw: 2 });
});

test("WebGL shadow caster eligibility changes clear stale content but otherwise stay unchanged", () => {
  const f = shadowFixture(); f.draw();
  f.caster.castShadow = false; f.draw();
  assert.deepEqual(f.counts(), { clear: 2, draw: 1 });
  f.draw(); assert.deepEqual(f.counts(), { clear: 2, draw: 1 });
  f.caster.castShadow = true; f.caster.viewCulled = true; f.draw();
  assert.deepEqual(f.counts(), { clear: 2, draw: 1 }, "camera-cull eligibility intentionally remains unchanged in G1a");
  f.caster.viewCulled = false; f.draw();
  assert.deepEqual(f.counts(), { clear: 3, draw: 2 });
});
