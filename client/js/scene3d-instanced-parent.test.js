"use strict";

// Plain InstancedMesh batches that ride a parent node: the wire field is kept
// through normalization, and the render bundle composes the node's current
// world matrix into the instance transforms every build, so both renderers
// (which read bundle.instancedMeshes) draw the batch where the node is.

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { bootstrapSource, createContext, runScript, flushAsyncWork } = require("./runtime-test-harness.js");

// The bootstrap runs in a vm context whose Float32Array differs from this
// file's; build typed arrays in its realm, as a browser page would.
async function makeSceneApiEnv() {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  Float32 = vm.runInContext("Float32Array", env.context);
  return env.context.__gosx_scene3d_api;
}
let Float32 = Float32Array;

test("an instanced mesh keeps its parentNode through normalization and carries it across updates", async () => {
  const api = await makeSceneApiEnv();
  const entry = { id: "ship/windows", count: 1, kind: "box", transforms: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1] };
  const parented = api.normalizeSceneInstancedMeshEntry(Object.assign({ parentNode: " ship " }, entry), 0, null);
  assert.equal(parented.parentNode, "ship");
  assert.equal(api.normalizeSceneInstancedMeshEntry(entry, 0, null).parentNode, "");
  assert.equal(api.normalizeSceneInstancedMeshEntry(Object.assign({ parentNode: 7 }, entry), 0, null).parentNode, "");
  // A later update that omits the field keeps the parent; one that sets "" clears it.
  assert.equal(api.normalizeSceneInstancedMeshEntry(entry, 0, parented).parentNode, "ship");
  assert.equal(api.normalizeSceneInstancedMeshEntry(Object.assign({ parentNode: "" }, entry), 0, parented).parentNode, "");
});
