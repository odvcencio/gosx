"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

function sceneCore() {
  const context = vm.createContext({ window: {}, console });
  for (const name of ["10-runtime-primitives.ts", "10-runtime-scene-utils.ts",
    "11-scene-math.ts", "12-scene-geometry.ts", "13-scene-material.ts",
    "10-runtime-scene-core.ts", "15-scene-draw-plan.ts", "15b-scene-planner.ts"]) {
    let source = fs.readFileSync(path.join(__dirname, "bootstrap-src", name), "utf8");
    if (name === "10-runtime-scene-core.ts") source = source.slice(0, source.indexOf("// Scene3D shared API"));
    vm.runInContext(source, context, { filename: name });
  }
  return context;
}

function sourceSection(file, start, end, context) {
  const source = file.includes("\n") ? file : fs.readFileSync(path.join(__dirname, file), "utf8");
  const first = source.indexOf(start);
  const last = source.indexOf(end, first);
  assert.ok(first >= 0 && last > first, `section ${start} exists in ${file}`);
  vm.runInContext(source.slice(first, last), context, { filename: file });
  return context;
}

test("vertex normalization hits by immutable revision and misses after edits", () => {
  const api = sceneCore();
  const raw = { positions: [0, 0, 0, 1, 0, 0, 0, 1, 0], immutable: true, revision: 0 };
  const first = api.sceneNormalizeMeshVertexDataCached(raw);
  assert.equal(api.sceneNormalizeMeshVertexDataCached(raw), first);
  raw.positions[0] = 4;
  raw.revision = 1;
  const revised = api.sceneNormalizeMeshVertexDataCached(raw);
  assert.notEqual(revised, first);
  assert.equal(revised.positions[0], 4);
  assert.notEqual(api.sceneNormalizeMeshVertexDataCached({ ...raw }), revised);
  raw.immutable = false;
  raw.positions[0] = 8;
  assert.equal(api.sceneNormalizeMeshVertexDataCached(raw).positions[0], 8);
  raw.positions[0] = 9;
  assert.equal(api.sceneNormalizeMeshVertexDataCached(raw).positions[0], 9);
});

test("instanced transform cache follows the same ID and clears on replacement or remount", () => {
  const api = sceneCore();
  const state = { instancedMeshes: [] };
  api.applySceneInstancedMeshesCommand(state, { instancedMeshes: [{ id: "a", count: 1, transforms: Array(16).fill(1) }] });
  state.instancedMeshes[0]._cachedTransforms = new Float32Array(16);
  const cached = state.instancedMeshes[0]._cachedTransforms;
  api.applySceneInstancedMeshesCommand(state, { instancedMeshes: [{ id: "a", count: 1 }] });
  assert.equal(state.instancedMeshes[0]._cachedTransforms, cached);
  api.applySceneInstancedMeshesCommand(state, { instancedMeshes: [{ id: "a", count: 1, transforms: Array(16).fill(2) }] });
  assert.equal(state.instancedMeshes[0]._cachedTransforms, undefined);
  state.instancedMeshes[0]._cachedTransforms = cached;
  api.applySceneInstancedMeshesCommand(state, { instancedMeshes: [{ id: "b", count: 1 }] });
  assert.equal(state.instancedMeshes[0]._cachedTransforms, undefined);
  const remount = { instancedMeshes: [] };
  api.applySceneInstancedMeshesCommand(remount, { instancedMeshes: [{ id: "a", count: 1 }] });
  assert.equal(remount.instancedMeshes[0]._cachedTransforms, undefined);

  const reordered = { instancedMeshes: [] };
  api.applySceneInstancedMeshesCommand(reordered, { instancedMeshes: [{ id: "a", count: 1 }, { id: "b", count: 1 }] });
  reordered.instancedMeshes[0]._cachedTransforms = cached;
  api.applySceneInstancedMeshesCommand(reordered, { instancedMeshes: [{ id: "b", count: 1 }, { id: "a", count: 1 }] });
  assert.equal(reordered.instancedMeshes[1]._cachedTransforms, cached);
});

test("planner vertex scans hit by revision and invalidate on resize and material edits", () => {
  const api = sceneCore();
  const vertices = { count: 3, positions: new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]),
    normals: new Float32Array(9), uvs: new Float32Array(6), immutable: true, revision: 0 };
  const scans = () => vm.runInContext("scenePlannerTelemetryState.fullVertexHashScans", api);
  const initial = scans();
  const first = api.scenePlannerHashMeshVertices(123, vertices);
  assert.equal(scans(), initial + 1);
  assert.equal(api.scenePlannerHashMeshVertices(123, vertices), first);
  assert.equal(scans(), initial + 1);
  api.scenePlannerHashMeshVertices(123, { ...vertices });
  assert.equal(scans(), initial + 2);
  vertices.positions[0] = 2;
  vertices.revision = 1;
  assert.notEqual(api.scenePlannerHashMeshVertices(123, vertices), first);
  assert.equal(scans(), initial + 3);
  vertices.immutable = false;
  vertices.positions[0] = 3;
  api.scenePlannerHashMeshVertices(123, vertices);
  vertices.positions[0] = 4;
  api.scenePlannerHashMeshVertices(123, vertices);
  assert.equal(scans(), initial + 5);

  const bundle = { materials: [{ color: "#112233" }] };
  const camera = { x: 0, y: 0, z: 5 };
  const viewport = { cssWidth: 640, cssHeight: 360, pixelWidth: 640, pixelHeight: 360, pixelRatio: 1 };
  const signature = api.scenePreparedSignature(bundle, camera, viewport);
  assert.equal(api.scenePreparedSignature(bundle, camera, viewport), signature);
  assert.notEqual(api.scenePreparedSignature(bundle, camera, { ...viewport, cssWidth: 800, pixelWidth: 800 }), signature);
  bundle.materials[0].color = "#445566";
  assert.notEqual(api.scenePreparedSignature(bundle, camera, viewport), signature);
});

test("WebGL constant, color, and rigid key caches hit and invalidate", () => {
  let parses = 0;
  const api = vm.createContext({
    sceneColorRGBA: value => { parses += 1; return [value, 1, 1, 1]; },
  });
  sourceSection(readSceneRendererBackendSrc("webgl"), "  var sceneGLConstantCache = new WeakMap();",
    "  function scenePBRHDRIBLAvailable(gl)", api);
  let queries = 0;
  const gl = { getParameter: () => ++queries };
  assert.equal(api.sceneCachedGLParameter(gl, 1), 1);
  assert.equal(api.sceneCachedGLParameter(gl, 1), 1);
  assert.equal(queries, 1);
  api.sceneCachedGLParameter(gl, 2);
  assert.equal(queries, 2);
  api.sceneInvalidateGLConstantCache(gl); // Renderer disposal on context loss.
  assert.equal(api.sceneCachedGLParameter(gl, 1), 3);
  assert.equal(queries, 3);
  const restoredGL = { getParameter: () => ++queries };
  api.sceneCachedGLParameter(restoredGL, 1);
  assert.equal(queries, 4);

  assert.equal(api.sceneCachedInstancedColorRGBA("#112233", [1, 1, 1, 1]),
    api.sceneCachedInstancedColorRGBA("#112233", [1, 1, 1, 1]));
  assert.equal(parses, 1);
  api.sceneCachedInstancedColorRGBA("#445566", [1, 1, 1, 1]);
  assert.equal(parses, 2);

  const owner = Object.freeze({});
  const rigid = { resourceOwner: owner, materialIndex: 0, vertexCount: 12,
    geometryRevision: 0, receiveShadow: false, castShadow: false,
    depthWrite: true, doubleSided: false };
  const key = api.sceneRigidBatchKey(rigid, true);
  const record = vm.runInContext("sceneRigidBatchKeyCache.get(owner)", Object.assign(api, { owner }));
  assert.equal(api.sceneRigidBatchKey(rigid, true), key);
  assert.equal(vm.runInContext("sceneRigidBatchKeyCache.get(owner)", api), record);
  rigid.materialIndex = 1;
  assert.notEqual(api.sceneRigidBatchKey(rigid, true), key);
  assert.notEqual(vm.runInContext("sceneRigidBatchKeyCache.get(owner)", api), record);
  assert.notEqual(api.sceneRigidBatchKey(rigid, false), api.sceneRigidBatchKey(rigid, true));
  const mutableOwner = {};
  const mutableRigid = { ...rigid, resourceOwner: mutableOwner };
  const mutableKey = api.sceneRigidBatchKey(mutableRigid, true);
  assert.equal(api.sceneRigidBatchKey(mutableRigid, true), mutableKey);
  assert.equal(mutableOwner._rigidBatchKeyCache.key, mutableKey);
});

test("frame attributes skip unchanged values and publish on change and remount", () => {
  const writes = [];
  const makeMount = () => ({ setAttribute(name, value) { writes.push([name, value]); } });
  const mount = makeMount();
  const load = mount => sourceSection("../runtime/scene3d/mount.ts",
    "    const lastPublishedFrameAttrs = Object.create(null);",
    "    // syncMountedSceneGizmoHelpers", vm.createContext({ mount,
      setAttrValue: (target, name, value) => target.setAttribute(name, value) }));
  const first = load(mount);
  first.publishSceneFrameAttr("clock", "1");
  first.publishSceneFrameAttr("clock", "1");
  assert.equal(writes.length, 1);
  first.publishSceneFrameAttr("clock", "2");
  assert.equal(writes.length, 2);
  load(makeMount()).publishSceneFrameAttr("clock", "2");
  assert.equal(writes.length, 3);
});

test("WebGL particle attributes publish again when the mount changes", () => {
  const writes = [];
  const api = vm.createContext({
    lastPublishedComputeParticleAttrs: Object.create(null),
    lastPublishedComputeParticleMount: null,
  });
  sourceSection(readSceneRendererBackendSrc("webgl"),
    "    function publishWebGLComputeParticleStatAttr(mount, name, value) {",
    "    function publishWebGLComputeParticleDrawStats()", api);
  const mount = () => ({ setAttribute: (name, value) => writes.push([name, value]) });
  const a = mount();
  api.publishWebGLComputeParticleStatAttr(a, "draws", "0");
  api.publishWebGLComputeParticleStatAttr(a, "draws", "0");
  assert.equal(writes.length, 1);
  api.publishWebGLComputeParticleStatAttr(a, "draws", "1");
  assert.equal(writes.length, 2);
  api.publishWebGLComputeParticleStatAttr(mount(), "draws", "1");
  assert.equal(writes.length, 3);
});
