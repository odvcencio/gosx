"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

const {
  bootstrapRuntimeSource,
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

function skinnedObject(overrides) {
  const vertices = {
    count: 3,
    positions: new Float32Array([-1, -1, 0, 1, -1, 0, 0, 1, 0]),
    normals: new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]),
    uvs: new Float32Array([0, 0, 1, 0, 0.5, 1]),
    joints: new Uint16Array(12),
    weights: new Float32Array([1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]),
  };
  return Object.assign({
    id: "skin-caster",
    kind: "mesh",
    materialKind: "standard",
    color: "#ff0000",
    receiveShadow: true,
    skin: { joints: [0] },
    vertices,
  }, overrides || {});
}

function renderBundle(api, object) {
  return api.createSceneRenderBundle(
    320, 180, "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [object], [], [], [], [], {}, 0, [], [], [], [], [], 0, false,
    { retainedGeometry: false },
  );
}

test("skinned bundle castShadow true follows the object flag", () => {
  const { env, api } = loadFreshSceneAPI();
  void vm.runInContext("Float32Array", env.context);
  const object = skinnedObject({ castShadow: true });
  const bundle = renderBundle(api, object);
  assert.equal(bundle.meshObjects.length, 1);
  const entry = bundle.meshObjects[0];
  assert.equal(entry.castShadow, true);
  assert.equal(entry.receiveShadow, true);
  assert.equal(entry.skin, object.skin);
  assert.equal(entry.vertices, object.vertices);
  assert.equal(entry.directVertices, true);
  assert.equal(typeof entry.materialIndex, "number");
  assert.equal(entry.modelMatrix.length, 16);
});

test("skinned bundle castShadow false when the object opts out", () => {
  const { api } = loadFreshSceneAPI();
  const bundle = renderBundle(api, skinnedObject({ castShadow: false }));
  assert.equal(bundle.meshObjects.length, 1);
  assert.equal(bundle.meshObjects[0].castShadow, false);
});

test("skinned bundle castShadow omitted defaults to false", () => {
  const { api } = loadFreshSceneAPI();
  const object = skinnedObject({});
  delete object.castShadow;
  const bundle = renderBundle(api, object);
  assert.equal(bundle.meshObjects.length, 1);
  assert.equal(bundle.meshObjects[0].castShadow, false);
});

test("skinned bundle record carries the computed geometryRevision", () => {
  const { api } = loadFreshSceneAPI();
  const object = skinnedObject({ castShadow: true, geometryRevision: 7 });
  const bundle = renderBundle(api, object);
  assert.equal(bundle.meshObjects.length, 1);
  assert.equal(
    bundle.meshObjects[0].geometryRevision,
    7,
    "geometryRevision propagated to the skinned bundle record",
  );
});
