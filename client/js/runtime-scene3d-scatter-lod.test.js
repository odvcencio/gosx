// Scatter batches (scene.Scatter) reach the render bundle only while the
// camera sits inside their distance band, and keep their wind sway.
const test = require("node:test");
const assert = require("node:assert/strict");
const { bootstrapSource, createContext, runScript, flushAsyncWork } = require("./runtime-test-harness.js");

function batch(id, level, min, max, wind) {
  return {
    id, kind: "box", count: 2, transforms: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 0, 0, 1],
    lodGroup: "veg/grass/0,0", lodLevel: level, lodMinDistance: min, lodMaxDistance: max, x: 10, y: 0, z: 0, wind,
  };
}

async function bundleAt(cameraX) {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const api = env.context.__gosx_scene3d_api;
  const meshes = [
    batch("near", 0, 0, 40, { sway: 0.8, height: 0.6 }),
    batch("far", 1, 40, 120),
    { id: "plain", kind: "box", count: 1, transforms: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1] },
  ].map((entry, index) => api.normalizeSceneInstancedMeshEntry(entry, index, null));
  const camera = api.sceneRenderCamera({ x: cameraX, y: 0, z: 0, fov: 60 });
  return api.createSceneRenderBundle(320, 180, "#000000", camera, [], [], [], [], [], {}, 0, [], meshes, [], [], [], 0);
}

test("scatter bands select one level by camera distance and drop the cell beyond the last band", async () => {
  const ids = async (x) => (await bundleAt(x)).instancedMeshes.map((mesh) => mesh.id).sort();
  assert.deepEqual(await ids(0), ["near", "plain"]);
  assert.deepEqual(await ids(-60), ["far", "plain"]);
  assert.deepEqual(await ids(-300), ["plain"]);
});

test("scatter batches keep their wind sway and band fields through normalization", async () => {
  const bundle = await bundleAt(0);
  const near = bundle.instancedMeshes.find((mesh) => mesh.id === "near");
  assert.deepEqual(near.wind, { sway: 0.8, height: 0.6 });
  assert.equal(near.lodMaxDistance, 40);
  assert.equal(near.x, 10);
});
