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

const identity = () => new Float32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

function yawRollPose(yaw, roll, x, y, z) {
  const cy = Math.cos(yaw), sy = Math.sin(yaw), cr = Math.cos(roll), sr = Math.sin(roll);
  // Column-major: yaw about Y, then roll about Z, translation last.
  const yawM = [cy, 0, -sy, 0, 0, 1, 0, 0, sy, 0, cy, 0, 0, 0, 0, 1];
  const rollM = [cr, sr, 0, 0, -sr, cr, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
  const out = new Float32(16);
  for (let c = 0; c < 4; c++) for (let r = 0; r < 4; r++) {
    let sum = 0;
    for (let k = 0; k < 4; k++) sum += yawM[k * 4 + r] * rollM[c * 4 + k];
    out[c * 4 + r] = sum;
  }
  out[12] = x; out[13] = y; out[14] = z;
  return out;
}

function apply(m, x, y, z) {
  return [m[0] * x + m[4] * y + m[8] * z + m[12], m[1] * x + m[5] * y + m[9] * z + m[13], m[2] * x + m[6] * y + m[10] * z + m[14]];
}

function translation(x, y, z) {
  const m = identity();
  m[12] = x; m[13] = y; m[14] = z;
  return m;
}

function shipPart(api, id, pose) {
  const part = api.normalizeSceneObject({ id, kind: "cube", size: 1 }, 0);
  Object.assign(part, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, parentMatrix: pose, static: false });
  return part;
}

function bundleOf(api, objects, instanced, time = 0) {
  return api.createSceneRenderBundle(640, 360, "#000000", { x: 0, y: 2, z: 10, fov: 60, near: .1, far: 500 }, objects,
    [], [], [], [], {}, time, [], instanced, [], [], [], 0, false, {});
}

function windows(api, extra = {}) {
  // Two instances in the ship's own frame: (1, 0, 2) and (-3, 4, 5).
  const transforms = [...translation(1, 0, 2), ...translation(-3, 4, 5)];
  return api.normalizeSceneInstancedMeshEntry(Object.assign({ id: "ship/windows", count: 2, kind: "box", size: 1, transforms }, extra), 0, null);
}

function assertNear(actual, expected, message) {
  for (let i = 0; i < 3; i++) assert.ok(Math.abs(actual[i] - expected[i]) < 1e-4, `${message}: ${Array.from(actual)} vs ${Array.from(expected)}`);
}

test("an instanced mesh with a parentNode follows the node's pose frame by frame", async () => {
  const api = await makeSceneApiEnv();
  const pose = yawRollPose(0.4, 0.1, 100, 3, -20);
  const hull = shipPart(api, "ship/hull", pose);
  const mesh = windows(api, { parentNode: "ship" });
  let bundle = bundleOf(api, [hull], [mesh]);
  let [entry] = bundle.instancedMeshes;
  assert.equal(entry.transforms.length, 32);
  assertNear(apply(entry.transforms.subarray(0, 16), 0, 0, 0), apply(pose, 1, 0, 2), "frame 0, instance 0");
  assertNear(apply(entry.transforms.subarray(16, 32), 0, 0, 0), apply(pose, -3, 4, 5), "frame 0, instance 1");
  assert.equal(entry._cachedTransforms, entry.transforms, "both renderers read _cachedTransforms first");

  // The vessel mutates its pose in place; the next bundle carries the batch with it.
  pose.set(yawRollPose(-0.3, -0.12, 140, 3.4, -10));
  bundle = bundleOf(api, [hull], [mesh], 1);
  [entry] = bundle.instancedMeshes;
  assertNear(apply(entry.transforms.subarray(0, 16), 0, 0, 0), apply(pose, 1, 0, 2), "frame 1, instance 0");
  assertNear(apply(entry.transforms.subarray(16, 32), 0, 0, 0), apply(pose, -3, 4, 5), "frame 1, instance 1");
  // The source record keeps its ship-local transforms for the next frame.
  assert.deepEqual(Array.from(mesh.transforms.slice(12, 15)), [1, 0, 2]);
});

test("an instanced mesh draws in world space until its node exists, and again when it goes", async () => {
  const api = await makeSceneApiEnv();
  const mesh = windows(api, { parentNode: "ship" });
  const hull = shipPart(api, "ship/hull", translation(50, 0, 0));
  assert.equal(bundleOf(api, [], [mesh]).instancedMeshes[0].transforms[12], 1);
  assert.equal(bundleOf(api, [hull], [mesh]).instancedMeshes[0].transforms[12], 51);
  assert.equal(bundleOf(api, [], [mesh]).instancedMeshes[0].transforms[12], 1);
  const other = shipPart(api, "ship-two/hull", translation(9, 0, 0));
  assert.equal(bundleOf(api, [other], [mesh]).instancedMeshes[0].transforms[12], 1, "a longer name is not the node");
});

test("an instanced mesh without a parentNode keeps its transforms untouched", async () => {
  const api = await makeSceneApiEnv();
  const mesh = windows(api);
  const hull = shipPart(api, "ship/hull", translation(50, 0, 0));
  const [entry] = bundleOf(api, [hull], [mesh]).instancedMeshes;
  assert.equal(entry.transforms, mesh.transforms);
  assert.equal(entry.transforms[12], 1);
});

test("a parented instanced mesh named under its node's id does not parent itself", async () => {
  const api = await makeSceneApiEnv();
  const mesh = windows(api, { parentNode: "ship" });
  // Objects hydrated under the mesh's own ID, and a crowd agent under the node, are not the node.
  const own = shipPart(api, "ship/windows/0", translation(900, 0, 0));
  const agent = shipPart(api, "ship/riders/a/0", translation(700, 0, 0));
  agent._crowdSkin = { batchID: "ship/riders", parent: "ship" };
  const hull = shipPart(api, "ship/hull", translation(50, 0, 0));
  const [entry] = bundleOf(api, [own, agent, hull], [mesh]).instancedMeshes;
  assert.equal(entry.transforms[12], 51);
});

test("a parented instanced mesh keeps its array while the node is still and takes a new array and revision when it moves", async () => {
  const api = await makeSceneApiEnv();
  const mesh = windows(api, { parentNode: "ship" });
  const pose = translation(50, 0, 0);
  const hull = shipPart(api, "ship/hull", pose);
  const first = bundleOf(api, [hull], [mesh]).instancedMeshes[0];
  const still = bundleOf(api, [hull], [mesh]).instancedMeshes[0];
  assert.equal(still.transforms, first.transforms, "an unmoved node leaves the array alone");
  assert.equal(still._instanceStreamRevision, first._instanceStreamRevision);
  pose.set(translation(60, 0, 0));
  const moved = bundleOf(api, [hull], [mesh]).instancedMeshes[0];
  assert.notEqual(moved.transforms, first.transforms, "a moved node hands the renderers a new array");
  assert.ok(moved._instanceStreamRevision > first._instanceStreamRevision, "and a new revision, which the GPU-driven path reads");
  assert.equal(moved.transforms[12], 61);
  // A local array changed in place counts as a change too.
  mesh.transforms[12] = 5;
  const edited = bundleOf(api, [hull], [mesh]).instancedMeshes[0];
  assert.equal(edited.transforms[12], 65);
  assert.ok(edited._instanceStreamRevision > moved._instanceStreamRevision);
  // Flipping between two poses never reports a stale array as current.
  pose.set(translation(50, 0, 0));
  const back = bundleOf(api, [hull], [mesh]).instancedMeshes[0];
  assert.equal(back.transforms[12], 55);
  assert.ok(back._instanceStreamRevision > edited._instanceStreamRevision);
});
