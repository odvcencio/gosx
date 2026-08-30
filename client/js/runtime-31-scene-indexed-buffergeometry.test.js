"use strict";

const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

// Indexed BufferGeometry end-to-end slice: unique vertex streams plus an
// authored triangle index stream survive Scene3D bundle construction, both the
// direct and the retained object path, malformed streams fail closed, and the
// shared CPU pick walks the authored triangle order. Backend draw-call wiring
// (WebGL2 drawElements / WebGPU drawIndexed over uint32 element buffers) is
// pinned at the source-contract level, mirroring runtime-10's source asserts.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const {
  bootstrapRuntimeSource,
  createBoardWebGPUHarness,
  createContext,
  freshFeatureBundleSource,
  runScript,
} = require("./runtime-test-harness.js");

function loadFreshSceneAPI() {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  return { env, api: env.context.__gosx_scene3d_api };
}

function renderBundle(api, object, timeSeconds, waterSystems, retainedGeometry, lights) {
  const objects = Array.isArray(object) ? object : [object];
  return api.createSceneRenderBundle(
    320,
    180,
    "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    objects,
    [],
    [],
    [],
    lights || [],
    {},
    timeSeconds || 0,
    [],
    [],
    [],
    waterSystems || [],
    [],
    0,
    false,
    { retainedGeometry: retainedGeometry !== false },
  );
}

// A two-triangle quad lowered the way scene.BufferGeometry now ships it: FOUR
// unique vertices plus SIX authored indices instead of six expanded corners.
function indexedQuad(overrides, Float32Ctor, Uint32Ctor) {
  const F32 = Float32Ctor || Float32Array;
  const U32 = Uint32Ctor || Uint32Array;
  const vertices = {
    count: 4,
    positions: new F32([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0]),
    normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]),
    uvs: new F32([0, 0, 1, 0, 1, 1, 0, 1]),
    tangents: new F32([
      1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1,
      1, 0, 0, 1,
    ]),
    indices: new U32([0, 1, 2, 0, 2, 3]),
    immutable: true,
    revision: 0,
    dynamic: false,
  };
  return Object.assign({
    id: "indexed-quad",
    kind: "gltf-mesh",
    scaleX: 1,
    scaleY: 1,
    scaleZ: 1,
    materialKind: "standard",
    color: "#8de1ff",
    wireframe: false,
    castShadow: false,
    receiveShadow: true,
    vertices,
  }, overrides || {});
}

// The historical flat six-corner soup shape, for unindexed controls.
function soupQuad(overrides, Float32Ctor, Uint32Ctor) {
  const F32 = Float32Ctor || Float32Array;
  const base = indexedQuad(overrides, F32, Uint32Ctor);
  const vertices = Object.assign({}, base.vertices);
  delete vertices.indices;
  vertices.count = 6;
  vertices.positions = new F32([
    -1, -1, 0, 1, -1, 0, 1, 1, 0,
    -1, -1, 0, 1, 1, 0, -1, 1, 0,
  ]);
  vertices.normals = new F32(18);
  vertices.uvs = new F32([0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1]);
  vertices.tangents = new F32(24);
  for (let i = 0; i < 6; i += 1) {
    vertices.normals[i * 3 + 2] = 1;
    vertices.tangents[i * 4] = 1;
    vertices.tangents[i * 4 + 3] = 1;
  }
  return Object.assign(base, { vertices });
}

test("Scene3D keeps an indexed quad indexed through direct and retained bundle paths", () => {
  const { env, api } = loadFreshSceneAPI();
  const F32 = vm.runInContext("Float32Array", env.context);
  const U32 = vm.runInContext("Uint32Array", env.context);
  const object = api.normalizeSceneObject(indexedQuad({}, F32, U32), 0, null);
  const bundle = renderBundle(api, object, 0);

  assert.equal(bundle.meshObjects.length, 1);
  const record = bundle.meshObjects[0];
  assert.equal(record.directVertices, true);
  assert.equal(record.retainedGeometry, true, "an immutable revisioned quad is retention-eligible");
  assert.equal(record.vertexCount, 4, "count is the unique position vertex count");

  const vertices = record.vertices;
  assert.ok(vertices.indices instanceof Uint32Array, "indices normalize once to a Uint32Array");
  assert.deepEqual(Array.from(vertices.indices), [0, 1, 2, 0, 2, 3]);
  assert.equal(vertices.positions.length, 12, "positions stay unique, never expanded to soup");
  assert.equal(vertices.normals.length, 12);
  assert.equal(vertices.uvs.length, 8);

  assert.equal(bundle.retainedMeshObjectCount, 1);
  assert.equal(bundle.retainedMeshVertexCount, 4);
  assert.equal(bundle.worldBakedMeshObjectCount, 0);
  assert.equal(bundle.worldMeshPositions.length, 0, "no per-frame CPU expansion");
});

test("Scene3D fails closed on malformed index streams instead of drawing a partial mesh", () => {
  const { env, api } = loadFreshSceneAPI();
  const F32 = vm.runInContext("Float32Array", env.context);
  const U32 = vm.runInContext("Uint32Array", env.context);
  const cases = [
    ["negative", new F32([0, -1, 2, 0, 2, 3])],
    ["out-of-range", new U32([0, 1, 9, 0, 2, 3])],
    ["non-triangle", new U32([0, 1, 2, 3])],
  ];
  for (const [name, indices] of cases) {
    for (const immutable of [true, false]) {
      const raw = indexedQuad({}, F32, U32);
      raw.vertices = Object.assign({}, raw.vertices, {
        indices,
        immutable,
        revision: immutable ? 0 : null,
      });
      const normalized = api.normalizeSceneObject(raw, 0, null);
      const bundle = renderBundle(api, normalized, 0);
      const route = immutable ? "retained" : "baked";
      assert.equal(bundle.meshObjects.length, 0, `${name}/${route}: nothing may be serialized or drawn`);
      assert.equal(bundle.retainedMeshObjectCount, 0, `${name}/${route}`);
      assert.equal(bundle.worldBakedMeshObjectCount, 0, `${name}/${route}`);
      assert.equal(bundle.worldMeshPositions.length, 0, `${name}/${route}`);
    }
  }
});

test("Scene3D unindexed geometry keeps its historical lowering", () => {
  const { env, api } = loadFreshSceneAPI();
  const F32 = vm.runInContext("Float32Array", env.context);
  const U32 = vm.runInContext("Uint32Array", env.context);

  // Immutable revisionless-of-nothing control: retention still applies without
  // indices, and the record simply carries no index stream.
  const plain = api.normalizeSceneObject(soupQuad({}, F32, U32), 0, null);
  const retainedBundle = renderBundle(api, plain, 0);
  assert.equal(retainedBundle.retainedMeshObjectCount, 1);
  assert.equal(retainedBundle.meshObjects[0].vertexCount, 6);
  const vertices = retainedBundle.meshObjects[0].vertices;
  assert.equal(vertices.indices, null, "normalized unindexed geometry grows no index stream");
  assert.equal(retainedBundle.retainedMeshVertexCount, 6);

  // Mutable geometry takes the CPU bake path and stays a flat soup.
  const mutableRaw = soupQuad({ id: "mutable" }, F32, U32);
  mutableRaw.vertices = Object.assign({}, mutableRaw.vertices, { immutable: false, revision: null });
  const mutable = api.normalizeSceneObject(mutableRaw, 0, null);
  const bakedBundle = renderBundle(api, mutable, 0);
  assert.equal(bakedBundle.retainedMeshObjectCount, 0);
  assert.equal(bakedBundle.worldBakedMeshObjectCount, 1);
  assert.equal(bakedBundle.meshObjects[0].vertexCount, 6);
  assert.equal(bakedBundle.worldMeshPositions.length, 18);
});

test("Scene3D shadow semantics: indexed casters retain, unindexed casters still bake", () => {
  const { env, api } = loadFreshSceneAPI();
  const F32 = vm.runInContext("Float32Array", env.context);
  const U32 = vm.runInContext("Uint32Array", env.context);

  const caster = api.normalizeSceneObject(indexedQuad({ id: "caster", castShadow: true }, F32, U32), 0, null);
  const casterBundle = renderBundle(api, caster, 0);
  assert.equal(casterBundle.retainedMeshObjectCount, 1, "indexed casters opt out of the bake");
  assert.equal(casterBundle.meshObjects[0].castShadow, true);

  const soupCaster = api.normalizeSceneObject(soupQuad({ id: "soup-caster", castShadow: true }, F32, U32), 1, null);
  const soupBundle = renderBundle(api, soupCaster, 0);
  assert.equal(soupBundle.retainedMeshObjectCount, 0, "unindexed casters keep the baked path");
  assert.equal(soupBundle.worldBakedMeshObjectCount, 1);
  assert.equal(soupBundle.worldMeshPositions.length, 18);
});

function frameUint32IndexBindings(fake, startPass) {
  return fake.state.renderPasses.slice(startPass).flatMap((pass) =>
    pass.indexBuffers.filter((binding) => binding.format === "uint32")
  );
}

test("WebGPU reuses, revises, and retires retained uint32 index buffers", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  harness.env.context.__gosx_scene3d_webgpu_render_bundles = false;
  const api = harness.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", harness.env.context);
  const U32 = vm.runInContext("Uint32Array", harness.env.context);
  const viewport = { cssWidth: 640, cssHeight: 480, pixelWidth: 640, pixelHeight: 480, pixelRatio: 1 };
  const object = api.normalizeSceneObject(indexedQuad({}, F32, U32), 0, null);

  let passStart = harness.fake.state.renderPasses.length;
  harness.renderer.render(renderBundle(api, object, 0), viewport);
  let bindings = frameUint32IndexBindings(harness.fake, passStart);
  assert.ok(bindings.length > 0, "the first retained draw must bind a uint32 index buffer");
  const firstBuffer = bindings[bindings.length - 1].buffer;
  const firstStats = Object.assign({}, harness.renderer.diagnostics().retainedGeometry);
  const firstIndexedDraw = harness.fake.state.renderPasses.slice(passStart)
    .flatMap((pass) => pass.drawIndexeds)
    .find((draw) => draw.indexCount === 6);
  assert.ok(firstIndexedDraw, "the indexed quad must issue drawIndexed(6)");

  passStart = harness.fake.state.renderPasses.length;
  harness.renderer.render(renderBundle(api, object, 1), viewport);
  bindings = frameUint32IndexBindings(harness.fake, passStart);
  assert.equal(bindings[bindings.length - 1].buffer, firstBuffer, "unchanged revision reuses the index buffer");
  const secondStats = Object.assign({}, harness.renderer.diagnostics().retainedGeometry);
  assert.equal(secondStats.uploadCalls, firstStats.uploadCalls, "unchanged revision performs no retained uploads");
  assert.ok(secondStats.hits > firstStats.hits, "unchanged revision records retained-buffer hits");

  object.vertices.indices = new U32([0, 2, 1, 0, 3, 2]);
  object.vertices.revision = 1;
  passStart = harness.fake.state.renderPasses.length;
  harness.renderer.render(renderBundle(api, object, 2), viewport);
  bindings = frameUint32IndexBindings(harness.fake, passStart);
  const revisedBuffer = bindings[bindings.length - 1].buffer;
  assert.notEqual(revisedBuffer, firstBuffer, "revision change rebuilds the index buffer");
  assert.equal(firstBuffer.destroyed, true, "revision change destroys the replaced index buffer");
  assert.equal(harness.renderer.diagnostics().retainedGeometry.revisionInvalidations, 1);

  const replacement = api.normalizeSceneObject(indexedQuad({ id: "replacement" }, F32, U32), 1, null);
  passStart = harness.fake.state.renderPasses.length;
  harness.renderer.render(renderBundle(api, replacement, 3), viewport);
  bindings = frameUint32IndexBindings(harness.fake, passStart);
  const replacementBuffer = bindings[bindings.length - 1].buffer;
  assert.equal(revisedBuffer.destroyed, true, "epoch sweep destroys an index buffer whose object disappeared");
  assert.notEqual(replacementBuffer, revisedBuffer);

  harness.renderer.render(renderBundle(api, replacement, 4, [], false), viewport);
  assert.equal(replacementBuffer.destroyed, true, "leaving retained mode retires the index buffer");
  assert.equal(harness.renderer.diagnostics().retainedGeometry.cacheEntries, 0);
  harness.renderer.dispose();
});

test("WebGPU indexed shadow casters bind distinct aligned matrix slots", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  harness.env.context.__gosx_scene3d_webgpu_render_bundles = false;
  const api = harness.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", harness.env.context);
  const U32 = vm.runInContext("Uint32Array", harness.env.context);
  const viewport = { cssWidth: 640, cssHeight: 480, pixelWidth: 640, pixelHeight: 480, pixelRatio: 1 };
  const left = api.normalizeSceneObject(indexedQuad({ id: "left", x: -1, castShadow: true }, F32, U32), 0, null);
  const right = api.normalizeSceneObject(indexedQuad({ id: "right", x: 1, castShadow: true }, F32, U32), 1, null);
  const light = api.normalizeSceneLight({
    id: "sun",
    kind: "directional",
    x: 4,
    y: 6,
    z: 8,
    intensity: 1,
    castShadow: true,
  }, 0, null);

  harness.renderer.render(renderBundle(api, [left, right], 0, [], true, [light]), viewport);
  const shadowPass = harness.fake.state.renderPasses.find((pass) =>
    pass.drawIndexeds.filter((draw) => draw.indexCount === 6).length >= 2
  );
  assert.ok(shadowPass, "both indexed casters must draw into a shadow pass");
  const casterOffsets = shadowPass.bindGroups
    .flatMap((binding) => binding.dynamicOffsets || [])
    .filter((offset) => offset > 0);
  assert.equal(new Set(casterOffsets).size, 2, "each caster receives an immutable uniform slot");
  assert.ok(casterOffsets.every((offset) => offset % 256 === 0), "dynamic offsets respect WebGPU alignment");

  const shadowBuffer = shadowPass.bindGroups[0].group.desc.entries[0].resource.buffer;
  const casterWrites = harness.fake.state.writeBufferCalls.filter((call) =>
    call.buffer === shadowBuffer && casterOffsets.includes(call.offset)
  );
  assert.equal(casterWrites.length, 2, "both per-caster matrices are uploaded before submit");
  assert.notDeepEqual(
    Array.from(casterWrites[0].data),
    Array.from(casterWrites[1].data),
    "different model transforms keep different light-space matrices",
  );
  harness.renderer.dispose();
});

// --- Shared CPU pick against an indexed direct mesh -------------------------

// Minimal faithful prelude copied from 16a-scene-webgpu-pick.test.mjs: the
// helpers 17-scene-input.ts expects from earlier bundle chunks.
function createPickContext() {
  const srcDir = path.join(__dirname, "bootstrap-src");
  function readSource(name) {
    return fs.readFileSync(path.join(srcDir, name), "utf8");
  }
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math, JSON, Number, Object, Array, String, Boolean,
    Float32Array, Uint32Array, Uint8Array, ArrayBuffer, DataView, Promise, Error,
    performance: { now: () => 0 },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.setTimeout = (fn) => { fn(); return 0; };
  sandbox.clearTimeout = () => {};
  const context = vm.createContext(sandbox);
  const prelude = `
    function sceneNumber(value, fallback) {
      var n = Number(value);
      return Number.isFinite(n) ? n : fallback;
    }
    function sceneBool(value, fallback) { return value == null ? fallback : !!value; }
    function normalizeSceneCameraKind(value, fallback) {
      var text = typeof value === "string" ? value.trim().toLowerCase() : "";
      return text === "orthographic" ? "orthographic" : fallback;
    }
    function normalizeSceneKind(value) {
      return typeof value === "string" ? value.trim().toLowerCase() : "box";
    }
    function sceneRenderCamera(camera) {
      return {
        kind: normalizeSceneCameraKind(camera && camera.kind, "perspective"),
        x: sceneNumber(camera && camera.x, 0),
        y: sceneNumber(camera && camera.y, 0),
        z: sceneNumber(camera && camera.z, 6),
        rotationX: sceneNumber(camera && camera.rotationX, 0),
        rotationY: sceneNumber(camera && camera.rotationY, 0),
        rotationZ: sceneNumber(camera && camera.rotationZ, 0),
        fov: sceneNumber(camera && camera.fov, 75),
        left: sceneNumber(camera && camera.left, 0),
        right: sceneNumber(camera && camera.right, 0),
        top: sceneNumber(camera && camera.top, 0),
        bottom: sceneNumber(camera && camera.bottom, 0),
        zoom: Math.max(0.0001, sceneNumber(camera && camera.zoom, 1)),
        near: sceneNumber(camera && camera.near, 0.05),
        far: sceneNumber(camera && camera.far, 128),
      };
    }
    function sceneOrthographicBounds(camera, width, height) {
      var cam = sceneRenderCamera(camera);
      var aspect = Math.max(0.0001, sceneNumber(width, 1) / Math.max(1, sceneNumber(height, 1)));
      var left = cam.left, right = cam.right, top = cam.top, bottom = cam.bottom;
      if (Math.abs(right - left) <= 0.000001 || Math.abs(top - bottom) <= 0.000001) {
        var hh = 3, hw = 3 * aspect;
        left = -hw; right = hw; top = hh; bottom = -hh;
      }
      var zoom = Math.max(0.0001, cam.zoom);
      var cx = (left + right) * 0.5, cy = (top + bottom) * 0.5;
      var halfW = Math.max(0.000001, Math.abs(right - left) * 0.5 / zoom);
      var halfH = Math.max(0.000001, Math.abs(top - bottom) * 0.5 / zoom);
      return { left: cx - halfW, right: cx + halfW, top: cy + halfH, bottom: cy - halfH };
    }
    function queueInputSignal() {}
    function sceneProjectPoint() { return null; }
    function clamp01(v) { return Math.max(0, Math.min(1, Number(v) || 0)); }
  `;
  vm.runInContext(prelude, context, { filename: "prelude.js" });
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readSource("17-scene-input.ts"), context, { filename: "17-scene-input.ts" });
  return context;
}

function indexedPickBundle(Float32Ctor, Uint32Ctor) {
  const F32 = Float32Ctor || Float32Array;
  const U32 = Uint32Ctor || Uint32Array;
  return {
    camera: { x: 0, y: 0, z: 6, fov: 75 },
    worldMeshPositions: new F32(0),
    worldMeshUVs: new F32(0),
    meshObjects: [{
      id: "quad",
      kind: "box",
      pickable: true,
      directVertices: true,
      vertexOffset: 0,
      vertexCount: 4,
      bounds: { minX: -1, minY: -1, minZ: 0, maxX: 1, maxY: 1, maxZ: 0 },
      vertices: {
        count: 4,
        positions: new F32([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0]),
        normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]),
        uvs: new F32([0, 0, 1, 0, 1, 1, 0, 1]),
        indices: new U32([0, 1, 2, 0, 2, 3]),
      },
    }],
    instancedMeshes: [],
    objects: [],
  };
}

test("exact CPU picking walks the authored triangle order of an indexed quad", () => {
  const context = createPickContext();
  context.__bundle = indexedPickBundle(
    vm.runInContext("Float32Array", context),
    vm.runInContext("Uint32Array", context),
  );

  // Right of centre lands in authored triangle 0; left of centre in triangle 1.
  // Both probes sit at the vertical centre so the answer never depends on the
  // screen-Y direction convention.
  const rightHit = vm.runInContext(
    "sceneRaycastPick(220, 150, 400, 300, __bundle.camera, __bundle)",
    context,
  );
  assert.ok(rightHit, "the right probe must hit the indexed quad");
  assert.equal(rightHit.primitiveIndex, 0, "right of the diagonal is authored triangle 0");
  assert.equal(rightHit.triangleIndex, 0);
  assert.ok(Math.abs(rightHit.uv.y - 0.5) <= 1e-6, `interpolated V = ${rightHit.uv.y}, want 0.5`);
  assert.ok(rightHit.uv.x > 0 && rightHit.uv.x < 1, "interpolated U stays inside the quad");

  const leftHit = vm.runInContext(
    "sceneRaycastPick(180, 150, 400, 300, __bundle.camera, __bundle)",
    context,
  );
  assert.ok(leftHit, "the left probe must hit the indexed quad");
  assert.equal(leftHit.primitiveIndex, 1, "left of the diagonal is authored triangle 1");
  assert.equal(leftHit.triangleIndex, 1);
  assert.ok(Math.abs(leftHit.uv.y - 0.5) <= 1e-6, `interpolated V = ${leftHit.uv.y}, want 0.5`);
  assert.ok(leftHit.uv.x > 0 && leftHit.uv.x < 1, "interpolated U stays inside the quad");
});

test("exact CPU picking misses off the indexed quad", () => {
  const context = createPickContext();
  context.__bundle = indexedPickBundle(
    vm.runInContext("Float32Array", context),
    vm.runInContext("Uint32Array", context),
  );
  const miss = vm.runInContext(
    "sceneRaycastPick(20, 20, 400, 300, __bundle.camera, __bundle)",
    context,
  );
  assert.equal(miss, null, "a ray outside the quad's bounds must report no hit");
});

// --- Source-level backend contract ------------------------------------------

test("backends wire uint32 index buffers and indexed draws for indexed direct meshes", () => {
  const webgl = readSceneRendererBackendSrc("webgl");
  assert.match(webgl, /function bindScenePBRDirectIndexBuffer\(/);
  assert.match(webgl, /gl\.ELEMENT_ARRAY_BUFFER/);
  assert.match(webgl, /gl\.drawElements\(gl\.TRIANGLES, selenaIndexCount, gl\.UNSIGNED_INT, 0\)/);
  assert.match(webgl, /gl\.drawElements\(gl\.TRIANGLES, directIndexCount, gl\.UNSIGNED_INT, 0\)/);
  assert.match(webgl, /gl\.drawElements\(gl\.TRIANGLES, casterIndexCount, gl\.UNSIGNED_INT, 0\)/);
  assert.match(webgl, /uniform mat4 u_modelMatrix;/, "shadow depth shader transforms model-space casters");

  const webgpu = readSceneRendererBackendSrc("webgpu");
  assert.match(webgpu, /function webGPUBindRetainedMeshIndexBuffer\(/);
  assert.match(webgpu, /pass\.setIndexBuffer\(record\.buffer, "uint32"\)/);
  assert.match(webgpu, /pass\.drawIndexed\(pbrIndexCount\)/);
  assert.match(webgpu, /pass\.drawIndexed\(casterIndexCount\)/);
  assert.match(webgpu, /pass\.drawIndexed\(skinnedShadowIndexCount\)/);
  assert.match(webgpu, /pass\.drawIndexed\(morphShadowIndexCount\)/);
  assert.match(webgpu, /pass\.drawIndexed\(selenaSkinIndexCount\)/);
  assert.match(webgpu, /pass\.drawIndexed\(skinnedPBRIndexCount\)/);

  const mountWebGL = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "mount-webgl.ts"), "utf8");
  assert.match(mountWebGL, /function sceneCloneModelMeshIndices\(indices\)/);
  assert.equal(
    (mountWebGL.match(/indices: sceneCloneModelMeshIndices\(vertices\.indices\)/g) || []).length,
    3,
    "skinned, transformed, and model-local clones must all preserve topology",
  );
});
