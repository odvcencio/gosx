"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const {
  bootstrapRuntimeSource, createContext, freshFeatureBundleSource, runScript,
} = require("./runtime-test-harness.js");

function harness() {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  return { api: env.context.__gosx_scene3d_api, F32: vm.runInContext("Float32Array", env.context) };
}

function fit(api, bundle) {
  return JSON.parse(JSON.stringify(api.sceneShadowComputeBounds(bundle)));
}

function scan(api, bundle) {
  return fit(api, { ...bundle, meshObjects: bundle.meshObjects.map(o => ({ ...o, _worldBakedBounds: false })) });
}

function mesh(F32, overrides = {}) {
  return {
    id: "baked", kind: "mesh", visible: true, materialKind: "standard", wireframe: false,
    x: 1 / 3, y: -1 / 7, z: 1 / 11, rotationX: 0, rotationY: 0.31, rotationZ: 0,
    scale: 1, scaleX: 2, scaleY: 0.7, scaleZ: 1.3,
    spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0, driftSpeed: 0, driftPhase: 0,
    customVertex: "void main() { gl_Position = vec4(a_position, 1.0); }",
    customFragment: "void main() { gl_FragColor = vec4(1.0); }",
    vertices: {
      positions: new F32([-1, -2, 0.3, 1, -2, 0.3, 0, 3, -0.7]), count: 3,
      normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1]),
      uvs: new F32([0, 0, 1, 0, 0.5, 1]),
      tangents: new F32([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]),
      immutable: true, revision: 0, dynamic: false,
    },
    ...overrides,
  };
}

function draw(api, objects) {
  return api.createSceneRenderBundle(800, 600, "#000", { x: 0, y: 3, z: 9, fov: 72, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], [], [], 0, false, { retainedGeometry: true });
}

test("baked shadow fits match the Float32 scan without reading vertices on cold, warm and moved frames", () => {
  const { api, F32 } = harness();
  const object = mesh(F32);
  for (let frame = 0; frame < 4; frame++) {
    if (frame === 2) { object.x += 0.000001; object.scaleX = -2; }
    const bundle = draw(api, [object]);
    const expected = scan(api, bundle);
    assert.notEqual(bundle.meshObjects[0].bounds.minX, expected.minX, "the double extrema require Float32 rounding");
    let reads = 0;
    bundle.worldMeshPositions = new Proxy(bundle.worldMeshPositions, {
      get(target, key) { if (/^\d+$/.test(String(key))) reads++; return Reflect.get(target, key, target); },
    });
    assert.deepEqual(fit(api, bundle), expected);
    assert.equal(reads, 0, `frame ${frame} avoids the triangle scan`);
  }
});

test("baked fits preserve indexed triangle selection, culling, receivers and retained caster semantics", () => {
  const { api, F32 } = harness();
  const object = mesh(F32, { castShadow: false, receiveShadow: true });
  // Normalization accepts wire JSON and creates the renderer realm's indices.
  const indexed = api.normalizeSceneObject({ ...object, vertices: {
    ...object.vertices, positions: [-1, -2, 0.3, 1, -2, 0.3, 0, 3, -0.7, 999, 999, 999],
    indices: [2, 1, 0], count: 4,
  } }, 0);
  const retained = mesh(F32, { id: "retained", customVertex: "", customFragment: "", x: -4, castShadow: true });
  const ignored = mesh(F32, { id: "ignored", customVertex: "", customFragment: "", x: -100, castShadow: false });
  const culled = mesh(F32, { id: "culled", x: 100, viewCulled: true });
  const bundle = draw(api, [indexed, retained, ignored, culled]);
  assert.ok(bundle.meshObjects.some(o => o.retainedGeometry && o.castShadow));
  assert.deepEqual(fit(api, bundle), scan(api, bundle));
  assert.ok(fit(api, bundle).maxX < 10, "unused vertices and culled meshes do not expand the fit");
});

test("manual, invalid and signed-zero bounds retain the legacy vertex scan", () => {
  const { api, F32 } = harness();
  const positions = new F32([1, 2, 3, -4, 5, -6, 7, -8, 9]);
  const base = { vertexOffset: 0, vertexCount: 3, bounds: { minX: -4, minY: -8, minZ: -6, maxX: 7, maxY: 5, maxZ: 9 } };
  for (const obj of [
    { ...base, bounds: { minX: -999, minY: -999, minZ: -999, maxX: 999, maxY: 999, maxZ: 999 } },
    { ...base, _worldBakedBounds: true, bounds: null },
    { ...base, _worldBakedBounds: true, bounds: { ...base.bounds, minX: NaN } },
    { ...base, _worldBakedBounds: true, bounds: { ...base.bounds, maxX: 1e40 } },
    { ...base, _worldBakedBounds: true, bounds: { ...base.bounds, minX: -0 } },
  ]) {
    const bundle = { worldMeshPositions: positions, meshObjects: [obj] };
    assert.deepEqual(fit(api, bundle), scan(api, bundle));
  }
  assert.deepEqual(fit(api, { meshObjects: [] }),
    { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 });
});
