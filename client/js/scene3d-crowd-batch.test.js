"use strict";

// Crowd batch records: one bundle record per crowd batch, retained across
// frames, so per-frame bundle and planner work stops scaling with agent count.
// The tests run the built bootstrap (GOWORK=off make build-bootstrap) through
// the Scene3D API: createSceneRenderBundle, prepareScene and the ray picker.

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const ts = require("../runtime/node_modules/typescript");
const { bootstrapSource, createContext, runScript, flushAsyncWork } = require("./runtime-test-harness.js");
const fs = require("node:fs");
const path = require("node:path");

const gltfSource = ts.transpileModule(fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "gltf.ts"), "utf8"), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

// The bootstrap runs in a vm context, whose Float32Array differs from this
// file's. The runtime tests typed arrays with instanceof, so build them in its
// realm, as a browser page would.
async function makeSceneApiEnv() {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const api = env.context.__gosx_scene3d_api;
  Float32 = vm.runInContext("Float32Array", env.context);
  return api;
}
let Float32 = Float32Array;

const viewport = { cssWidth: 640, cssHeight: 360, pixelWidth: 640, pixelHeight: 360, pixelRatio: 1 };
const capabilities = { retainedGeometry: true, worldBakedAttributeViews: true, rigidImportedBatches: true, crowdBatches: true };

function cubeVertices() {
  const faces = [
    [[0, 0, 1], [1, 0, 0], [0, 1, 0]], [[0, 0, -1], [-1, 0, 0], [0, 1, 0]],
    [[1, 0, 0], [0, 0, -1], [0, 1, 0]], [[-1, 0, 0], [0, 0, 1], [0, 1, 0]],
    [[0, 1, 0], [1, 0, 0], [0, 0, -1]], [[0, -1, 0], [1, 0, 0], [0, 0, 1]],
  ];
  const positions = [], normals = [], uvs = [], tangents = [], joints = [], weights = [];
  for (const [n, u, v] of faces) {
    for (const [a, b] of [[-1, -1], [1, -1], [1, 1], [-1, -1], [1, 1], [-1, 1]]) {
      for (let k = 0; k < 3; k++) positions.push(n[k] * .5 + u[k] * a * .5 + v[k] * b * .5);
      normals.push(...n); uvs.push((a + 1) / 2, (b + 1) / 2); tangents.push(...u, 1);
      joints.push(0, 0, 0, 0); weights.push(1, 0, 0, 0);
    }
  }
  const f32 = values => new Float32(values);
  return { count: 36, positions: f32(positions), normals: f32(normals), uvs: f32(uvs), tangents: f32(tangents), joints: f32(joints), weights: f32(weights) };
}

function matrixAt(x, z) {
  return new Float32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, 0, z, 1]);
}

// Agents shaped like hydrated InstancedGLB crowd objects.
function makeCrowd(api, count, options = {}) {
  const proto = api.normalizeSceneObject(Object.assign({ id: "proto", kind: "mesh", vertices: cubeVertices(),
    materialKind: "standard", color: "#c8b27a", pickable: false }, options.appearance), 0);
  proto.vertices.immutable = true;
  proto.vertices.revision = 0;
  const atlas = { id: "atlas" };
  const bounds = { minX: -.5, minY: -.5, minZ: -.5, maxX: .5, maxY: .5, maxZ: .5 };
  const objects = [];
  for (let i = 0; i < count; i++) {
    const object = Object.assign({}, proto, {
      id: `crowd/agent-${i}/0`, x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
      spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0, driftSpeed: 0, driftPhase: 0,
      static: false, skin: null, parentMatrix: matrixAt(i * 2, -10),
    });
    object._crowdSkin = { atlas, bounds, rows: new Float32([1, 2, .5]), poseRows: null, batchID: "crowd", rev: 0, group: null, parent: options.parent || "" };
    if (options.motion) {
      object._crowdMotion = { atlas, clipTable: { count: 1 }, record: new Float32(24), dirty: false,
        bounds: { minX: i * 2 - 1, minY: -1, minZ: -11, maxX: i * 2 + 1, maxY: 1, maxZ: -9 } };
    }
    objects.push(object);
  }
  return objects;
}

function build(api, objects, camera = { x: 0, y: 2, z: 10, fov: 60, near: .1, far: 500 }, caps = capabilities) {
  return api.createSceneRenderBundle(640, 360, "#000000", camera, objects, [], [], [], [], {}, 0,
    [], [], [], [], [], 0, false, caps);
}

function batchRecords(bundle) {
  return bundle.meshObjects.filter(record => record._crowdBatch === true);
}

test("a crowd of N agents becomes one bundle record when the renderer accepts crowd batches", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 50);
  const bundle = build(api, objects);
  assert.equal(bundle.meshObjects.length, 1);
  const [record] = batchRecords(bundle);
  assert.equal(record.instanceCount, 50);
  assert.equal(record.crowdMembers.length, 50);
  assert.equal(record.instanceMatrices.length, 50);
  assert.equal(record.renderPass, "opaque");
  assert.equal(record.crowdMotion, false);
  assert.equal(bundle.retainedMeshObjectCount, 50);
  assert.equal(bundle.retainedMeshVertexCount, 50 * 36);
  assert.deepEqual(Object.assign({}, record.bounds), { minX: -.5, minY: -.5, minZ: -10.5, maxX: 98.5, maxY: .5, maxZ: -9.5 });

  const legacy = build(api, objects, undefined, { ...capabilities, crowdBatches: false });
  assert.equal(legacy.meshObjects.length, 50, "without the capability each agent keeps its own record");
  assert.equal(batchRecords(legacy).length, 0);
});

test("a camera move keeps the record and revision, and the planner reuses its plan", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 200, { motion: true });
  const first = build(api, objects, { x: 0, y: 2, z: 10, fov: 60, near: .1, far: 500 });
  const prepared = api.prepareScene(first, first.camera, viewport, null);
  const [record] = batchRecords(first);
  const revision = record.crowdRevision;
  const shape = record.crowdShape;

  const moved = build(api, objects, { x: 5, y: 3, z: 12, rotationY: .2, fov: 70, near: .1, far: 500 });
  const [movedRecord] = batchRecords(moved);
  assert.equal(movedRecord, record, "the batch record is retained, not rebuilt");
  assert.equal(movedRecord.crowdRevision, revision);
  assert.equal(movedRecord.crowdShape, shape);
  assert.notEqual(movedRecord.depthCenter, undefined);
  const next = api.prepareScene(moved, moved.camera, { ...viewport, cssWidth: 800, pixelWidth: 800 }, prepared);
  assert.equal(next, prepared, "camera and viewport changes do not rebuild the prepared plan");
  assert.equal(next.ir, moved, "the cached plan follows the current bundle");
  assert.equal(next.pbrPasses.opaque[0], record);
  assert.equal(next.camera, moved.camera);
});

test("a motion frame bumps the revision once and does not rebuild the planner", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 200, { motion: true });
  const first = build(api, objects);
  const prepared = api.prepareScene(first, first.camera, viewport, null);
  const [record] = batchRecords(first);
  const revision = record.crowdRevision;

  for (const object of objects) { object._crowdMotion.record[0] += 1; object._crowdMotion.dirty = true; }
  const second = build(api, objects);
  assert.equal(batchRecords(second)[0], record);
  assert.equal(record.crowdRevision, revision + 1);
  assert.equal(api.prepareScene(second, second.camera, viewport, prepared), prepared);

  // The renderer clears dirty after it uploads. Nothing changed since.
  for (const object of objects) object._crowdMotion.dirty = false;
  build(api, objects);
  assert.equal(record.crowdRevision, revision + 1);

  // Only some agents move: still one bump.
  for (const object of objects.slice(0, 10)) object._crowdMotion.dirty = true;
  build(api, objects);
  assert.equal(record.crowdRevision, revision + 2);
});

test("a pose revision bump counts as a change and the record bounds follow the matrices", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 20);
  const first = build(api, objects);
  const [record] = batchRecords(first);
  const revision = record.crowdRevision;
  build(api, objects);
  assert.equal(record.crowdRevision, revision, "no change, no bump");

  objects[3]._crowdSkin.rows[2] = .75;
  objects[3]._crowdSkin.rev += 1;
  build(api, objects);
  assert.equal(record.crowdRevision, revision + 1);
  assert.equal(record.crowdShape, batchRecords(first)[0].crowdShape);

  objects[0].parentMatrix = matrixAt(-50, -10);
  build(api, objects);
  assert.equal(record.crowdRevision, revision + 2);
  assert.equal(record.bounds.minX, -50.5, "union bounds track the new matrix");
});

test("a structural change rebuilds the record and the planner", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 30);
  const first = build(api, objects);
  const prepared = api.prepareScene(first, first.camera, viewport, null);
  const [record] = batchRecords(first);
  const shape = record.crowdShape;
  const revision = record.crowdRevision;

  const smaller = build(api, objects.slice(0, 29));
  assert.equal(batchRecords(smaller)[0], record);
  assert.equal(record.instanceCount, 29);
  assert.equal(record.crowdMembers.length, 29);
  assert.ok(record.crowdShape > shape);
  assert.ok(record.crowdRevision > revision);
  const rebuilt = api.prepareScene(smaller, smaller.camera, viewport, prepared);
  assert.notEqual(rebuilt, prepared, "a different agent count rebuilds the plan");
  assert.equal(rebuilt.rebuilds, prepared.rebuilds + 1);

  const replaced = objects.slice(0, 29);
  replaced[7] = Object.assign({}, replaced[7], { _crowdSkin: { ...replaced[7]._crowdSkin } });
  const afterSwap = record.crowdShape;
  build(api, replaced);
  assert.ok(record.crowdShape > afterSwap, "a different pose record in a slot reshapes the batch");
});

test("agents that cannot ride a shared matrix stay on their own records", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 10);
  objects[2].selected = true;
  objects[4].x = 3;
  objects[6].visible = false;
  const bundle = build(api, objects);
  const [record] = batchRecords(bundle);
  assert.equal(record.instanceCount, 7, "10 agents minus a selected one, an offset one and an invisible one");
  const own = bundle.meshObjects.filter(entry => !entry._crowdBatch).map(entry => entry.id).sort();
  assert.deepEqual(Array.from(own), ["crowd/agent-2/0", "crowd/agent-4/0"]);
});

test("an agent whose appearance differs from its batch stays on its own record", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 6);
  // Hydration normalizes every agent separately, so equal array fields are distinct copies.
  for (const object of objects) object.specularColor = Array.from(object.specularColor || [1, 1, 1]);
  objects[3].color = "#ff0000";
  const bundle = build(api, objects);
  const [record] = batchRecords(bundle);
  assert.equal(record.instanceCount, 5);
  assert.deepEqual(Array.from(bundle.meshObjects.filter(entry => !entry._crowdBatch), entry => entry.id), ["crowd/agent-3/0"]);
});

test("a translucent crowd keeps one record per agent so each sorts by depth", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 6, { appearance: { opacity: .5, blendMode: "alpha" } });
  const bundle = build(api, objects);
  assert.equal(batchRecords(bundle).length, 0);
  assert.equal(bundle.meshObjects.length, 6);
  assert.ok(bundle.meshObjects.every(record => record.renderPass === "alpha"));
});

test("translucent objects re-sort by depth when the camera moves even though the plan is reused", async () => {
  const api = await makeSceneApiEnv();
  const glass = (id, z) => {
    const object = api.normalizeSceneObject({ id, kind: "mesh", vertices: cubeVertices(), z, wireframe: false, opacity: .5, blendMode: "alpha", color: "#88ccff" }, 0);
    object.vertices.immutable = true;
    object.vertices.revision = 0;
    return object;
  };
  const objects = [glass("near", -5), glass("far", -20)];
  const front = build(api, objects, { x: 0, y: 0, z: 0, fov: 60, near: .1, far: 500 });
  const prepared = api.prepareScene(front, front.camera, viewport, null);
  assert.deepEqual(Array.from(prepared.pbrPasses.alpha, record => record.id), ["far", "near"], "far draws first");

  const behind = build(api, objects, { x: 0, y: 0, z: -30, rotationY: Math.PI, fov: 60, near: .1, far: 500 });
  const reused = api.prepareScene(behind, behind.camera, viewport, prepared);
  assert.equal(reused, prepared, "the plan is reused");
  assert.deepEqual(Array.from(reused.pbrPasses.alpha, record => record.id), ["near", "far"], "the new camera flips the order");
});

test("a ray finds the agent inside a crowd batch", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 5, { appearance: { pickable: undefined } });
  for (const object of objects) delete object.pickable;
  // Make agents big enough to pass the pick extent heuristic.
  for (const object of objects) object._crowdSkin.bounds = { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 };
  const bundle = build(api, objects, { x: 4, y: 0, z: 5, fov: 60, near: .1, far: 500 });
  assert.equal(batchRecords(bundle).length, 1);
  const [record] = batchRecords(bundle);
  const records = record.crowdPickRecords();
  assert.equal(records.length, 5);
  assert.deepEqual(Array.from(records, entry => entry.id), objects.map(object => object.id));
  const hit = api.sceneRaycastPick(320, 180, 640, 360, bundle.camera, bundle);
  assert.ok(hit, "the center ray hits an agent");
  assert.equal(hit.object.id, "crowd/agent-2/0");
});

test("crowd batches sit after ordinary objects and leave them untouched", async () => {
  const api = await makeSceneApiEnv();
  const box = api.normalizeSceneObject({ id: "ground", kind: "mesh", vertices: cubeVertices(), y: -1 }, 0);
  box.vertices.immutable = true;
  box.vertices.revision = 0;
  const crowd = makeCrowd(api, 4);
  const bundle = build(api, [crowd[0], crowd[1], box, crowd[2], crowd[3]]);
  assert.equal(bundle.meshObjects.length, 2);
  assert.equal(bundle.meshObjects[0].id, "ground");
  assert.equal(bundle.meshObjects[1]._crowdBatch, true);
  assert.equal(bundle.meshObjects[1].instanceCount, 4);
});

test("a group that stops appearing is dropped and a later crowd starts clean", async () => {
  const api = await makeSceneApiEnv();
  const first = makeCrowd(api, 3);
  const [record] = batchRecords(build(api, first));
  for (let i = 0; i < 12; i++) build(api, []);
  const again = batchRecords(build(api, first));
  assert.equal(again.length, 1);
  assert.notEqual(again[0], record, "the stale group was released");
  assert.equal(again[0].instanceCount, 3);
});

test("a partial motion frame hands the renderer the changed slot ranges", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 200, { motion: true });
  const first = build(api, objects);
  const [record] = batchRecords(first);
  assert.equal(record.crowdDirtyRanges, null, "the first revision restates every slot");
  for (const object of objects) object._crowdMotion.dirty = false;

  for (const slot of [3, 4, 5, 90, 150, 151]) objects[slot]._crowdMotion.dirty = true;
  build(api, objects);
  assert.deepEqual(Array.from(record.crowdDirtyRanges), [3, 6, 90, 91, 150, 152]);
  assert.equal(record.crowdDirtyBase, record.crowdRevision - 1);

  // The renderer has not cleared the flags, so the next bundle repeats them.
  const before = record.crowdRevision;
  build(api, objects);
  assert.equal(record.crowdRevision, before + 1);
  assert.deepEqual(Array.from(record.crowdDirtyRanges), [3, 6, 90, 91, 150, 152]);

  // A bundle with no change keeps the revision and the ranges that describe it.
  for (const object of objects) object._crowdMotion.dirty = false;
  const quiet = record.crowdRevision;
  build(api, objects);
  assert.equal(record.crowdRevision, quiet);
  assert.deepEqual(Array.from(record.crowdDirtyRanges), [3, 6, 90, 91, 150, 152]);

  // A membership change restates every slot again.
  objects.splice(10, 1);
  build(api, objects);
  assert.equal(record.crowdDirtyRanges, null);
});

test("scattered changes collapse into a bounded range list", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 600, { motion: true });
  const [record] = batchRecords(build(api, objects));
  for (const object of objects) object._crowdMotion.dirty = false;
  for (let slot = 0; slot < 600; slot += 2) objects[slot]._crowdMotion.dirty = true;
  build(api, objects);
  assert.ok(record.crowdDirtyRanges.length <= 256);
  assert.equal(record.crowdDirtyRanges[0], 0);
  assert.equal(record.crowdDirtyRanges[record.crowdDirtyRanges.length - 1], 599);
});

// A node the way the vessel presents itself: parts named "<node>/<part>" that
// share one parentMatrix, which the vessel rewrites in place every frame.
function makeShip(api, pose) {
  const part = api.normalizeSceneObject({ id: "ship/hull", kind: "mesh", vertices: cubeVertices(), materialKind: "standard", color: "#884422", pickable: false }, 0);
  Object.assign(part, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, parentMatrix: pose, static: false });
  return part;
}

function yawPose(yaw, x, y, z) {
  const c = Math.cos(yaw), s = Math.sin(yaw);
  return new Float32([c, 0, -s, 0, 0, 1, 0, 0, s, 0, c, 0, x, y, z, 1]);
}

test("a parented crowd carries the node's world matrix and follows it without a new revision", async () => {
  const api = await makeSceneApiEnv();
  const pose = yawPose(0, 100, 5, 0);
  const objects = [makeShip(api, pose), ...makeCrowd(api, 40, { motion: true, parent: "ship" })];
  const first = build(api, objects);
  const [record] = batchRecords(first);
  assert.ok(record.crowdParentMatrix, "the record carries a batch matrix");
  assert.ok(Array.from(pose).every((v, i) => record.crowdParentMatrix[i] === v), "the record carries the node's matrix values");
  assert.equal(first.meshObjects.filter(r => r.id === "ship/hull").length, 1, "the node itself is an ordinary record");
  const revision = record.crowdRevision;
  const localMinX = record.bounds.minX - 100;

  // The vessel mutates its pose in place every frame; no agent changes.
  pose.set(yawPose(0, 130, 6, 20));
  build(api, objects);
  assert.equal(record.crowdRevision, revision, "moving the parent is not an agent change");
  assert.equal(record.crowdParentMatrix[12], 130);
  assert.equal(record.crowdParentMatrix[14], 20);
  assert.ok(Math.abs(record.bounds.minX - (130 + localMinX)) < 1e-4, "culling bounds follow the parent: " + record.bounds.minX);
  assert.ok(record.bounds.minZ > 20 - 12 && record.bounds.maxZ < 20 + 12 && record.bounds.minY > 5 - 2);
});

test("a parent that turns the crowd rotates its culling bounds", async () => {
  const api = await makeSceneApiEnv();
  const pose = yawPose(Math.PI / 2, 0, 0, 0);
  const objects = [makeShip(api, pose), ...makeCrowd(api, 10, { motion: true, parent: "ship" })];
  const [record] = batchRecords(build(api, objects));
  // Agents lie along +X (x = 2i, up to 19) at z = -10; a quarter turn about Y
  // sends local X to world -Z and local Z to world X.
  assert.ok(record.bounds.minX > -12 && record.bounds.maxX < -8, JSON.stringify(record.bounds));
  assert.ok(record.bounds.minZ < -15 && record.bounds.maxZ < 2, JSON.stringify(record.bounds));
});

test("a crowd draws in world space until its parent exists, and again if it goes away", async () => {
  const api = await makeSceneApiEnv();
  const crowd = makeCrowd(api, 12, { parent: "ship" });
  const [record] = batchRecords(build(api, crowd));
  assert.equal(record.crowdParentMatrix, null);
  const worldMinX = record.bounds.minX;
  const pose = yawPose(0, 50, 0, 0);
  const ship = makeShip(api, pose);
  build(api, [ship, ...crowd]);
  assert.equal(record.crowdParentMatrix[12], 50);
  assert.ok(Math.abs(record.bounds.minX - (worldMinX + 50)) < 1e-4);
  build(api, crowd);
  assert.equal(record.crowdParentMatrix, null);
  assert.ok(Math.abs(record.bounds.minX - worldMinX) < 1e-4, "bounds return to world space");
});

test("a parent name matches a node or its parts, not a longer name", async () => {
  const api = await makeSceneApiEnv();
  const crowd = makeCrowd(api, 4, { parent: "ship" });
  const other = makeShip(api, yawPose(0, 9, 0, 0));
  other.id = "ship-two/hull";
  const [record] = batchRecords(build(api, [other, ...crowd]));
  assert.equal(record.crowdParentMatrix, null);
  const exact = makeShip(api, yawPose(0, 7, 0, 0));
  exact.id = "ship";
  assert.equal(batchRecords(build(api, [other, exact, ...crowd]))[0].crowdParentMatrix[12], 7);
});

test("a crowd named under its parent's id never resolves one of its own agents as the parent", async () => {
  const api = await makeSceneApiEnv();
  const pose = yawPose(0, 70, 0, 0);
  // Batch "ship/riders" rides node "ship": its agent ids start with "ship/", the node's part prefix.
  const crowd = makeCrowd(api, 4, { parent: "ship" });
  crowd.forEach((object, i) => {
    object.id = `ship/riders/agent-${i}/0`;
    object._crowdSkin.batchID = "ship/riders";
    object.specularColor = Array.from(object.specularColor || [1, 1, 1]);
  });
  // An agent that differs from its batch draws on its own record, which lands in the list the parent search reads.
  crowd[1].color = "#ff0000";
  crowd[1].parentMatrix = matrixAt(500, 500);
  const ship = makeShip(api, pose);
  const [record] = batchRecords(build(api, [...crowd, ship]));
  assert.equal(record.instanceCount, 3);
  assert.equal(record.crowdParentMatrix[12], 70, "the batch rides the ship's pose, not its stray agent at x=500");
  // With no ship in the scene the crowd must draw in world space, not ride its own agent.
  const alone = batchRecords(build(api, crowd))[0];
  assert.equal(alone.crowdParentMatrix, null);
});

test("agents with different parents do not share a record", async () => {
  const api = await makeSceneApiEnv();
  const a = makeCrowd(api, 4, { parent: "ship" });
  const b = makeCrowd(api, 4, { parent: "dock" });
  for (const object of b) { object._crowdSkin.batchID = "crowd-b"; object.id = object.id.replace("crowd/", "crowd-b/"); }
  const bundle = build(api, [makeShip(api, yawPose(0, 1, 0, 0)), ...a, ...b]);
  assert.equal(batchRecords(bundle).length, 2);
});

test("a ray hits an agent where its parent has carried it", async () => {
  const api = await makeSceneApiEnv();
  const pose = yawPose(0, 0, 0, 0);
  const crowd = makeCrowd(api, 1, { parent: "ship", appearance: { pickable: undefined } });
  for (const object of crowd) { delete object.pickable; object._crowdSkin.bounds = { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 }; }
  const objects = [makeShip(api, pose), ...crowd];
  const camera = { x: 0, y: 0, z: 10, fov: 60, near: .1, far: 500 };
  // The lone agent sits at (0, 0, -10) in the ship's space.
  let bundle = build(api, objects, camera);
  assert.ok(api.sceneRaycastPick(320, 180, 640, 360, bundle.camera, bundle), "ray finds the agent at the ship's origin");
  // The ship sails 60 m to the side: the centre ray no longer hits, a ray at the new spot does.
  pose.set(yawPose(0, 60, 0, 0));
  bundle = build(api, objects, camera);
  assert.equal(api.sceneRaycastPick(320, 180, 640, 360, bundle.camera, bundle), null);
  const moved = Object.assign({}, camera, { x: 60 });
  bundle = build(api, objects, moved);
  const hit = api.sceneRaycastPick(320, 180, 640, 360, bundle.camera, bundle);
  assert.ok(hit && hit.object.id === "crowd/agent-0/0", "ray finds the agent where the ship carried it");
});

test("motion agents are picked at their latest motion key, through the parent", async () => {
  const api = await makeSceneApiEnv();
  const pose = yawPose(0, 40, 0, 0);
  const crowd = makeCrowd(api, 1, { motion: true, parent: "ship", appearance: { pickable: undefined } });
  const [agent] = crowd;
  delete agent.pickable;
  agent._crowdSkin.bounds = { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 };
  // Motion key: from (0,0,-10) to (4,0,-10) over t in [0, 1].
  const record = agent._crowdMotion.record;
  record.set([0, 0, -10, 0, 0, 0, 1, 1, 1, 0, 4, 0, -10, 0, 0, 0, 1, 1, 1, 1]);
  agent._crowdMotion.bounds = { minX: -1, minY: -1, minZ: -11, maxX: 5, maxY: 1, maxZ: -9 };
  agent._crowdMotion.dirty = true;
  const objects = [makeShip(api, pose), agent];
  const bundle = api.createSceneRenderBundle(640, 360, "#000000", { x: 44, y: 0, z: 10, fov: 60, near: .1, far: 500 }, objects,
    [], [], [], [], {}, 2, [], [], [], [], [], 0, false, capabilities);
  const [batch] = batchRecords(bundle);
  const [pick] = batch.crowdPickRecords();
  assert.ok(Math.abs(pick.modelMatrix[12] - 44) < 1e-4, "40 (ship) + 4 (motion key after t=1): " + pick.modelMatrix[12]);
  assert.ok(pick.bounds.minX > 38 && pick.bounds.maxX < 46);
});

test("a crowd parented on today's vessel rides its JS-computed pose", async () => {
  const api = await makeSceneApiEnv();
  // The real vessel authority, as the vessel mount runs it: physics state in,
  // pose matrix out, written into every part's parentMatrix.
  const root = path.resolve(__dirname, "../..");
  const vessel = vm.createContext({ window: {}, Float32Array, Uint8Array, Uint16Array, Uint32Array, Int8Array, Int16Array, ArrayBuffer, DataView, TextDecoder });
  for (const source of ["client/js/bootstrap-src/11-scene-math.ts", "client/runtime/scene3d/gltf.ts", "client/runtime/scene3d/vessel-navigation.ts",
    "client/runtime/scene3d/vessel-physics.ts", "client/runtime/scene3d/vessel-model.ts"]) {
    vm.runInContext(source.endsWith("/gltf.ts") ? gltfSource : fs.readFileSync(path.join(root, source), "utf8"), vessel);
  }
  const physics = vessel.window.__gosx_scene3d_vessel_physics, modelAPI = vessel.window.__gosx_scene3d_vessel_model;
  const hull = makeShip(api, null);
  hull.id = "ship-high/tarred-hull";
  const mast = makeShip(api, null);
  mast.id = "ship-high/mast-1";
  const sceneState = { objects: new Map([[hull.id, hull], [mast.id, mast]]) };
  const config = { nodeId: "ship-high", lods: [] };
  const state = physics.create(config, {}, {});
  const model = modelAPI.create(sceneState, config, physics);

  const crowd = makeCrowd(api, 30, { parent: "ship-high" });
  const objects = [hull, mast, ...crowd];
  for (const frame of [0, 1, 2]) {
    Object.assign(state, { x: 200 + frame * 3, y: 0.4 * frame, z: -50 + frame, heading: 0.5 + frame * 0.1, roll: 0.05 * frame, pitch: -0.02 * frame });
    model.update(state, { x: 0, y: 0, z: 0 }, 1, 1);
    const bundle = build(api, objects);
    const [record] = batchRecords(bundle);
    const pose = hull.parentMatrix;
    assert.ok(pose === mast.parentMatrix && pose === model.pose, "the vessel shares one pose matrix across its parts");
    assert.ok(Array.from(pose).every((v, i) => record.crowdParentMatrix[i] === v), "frame " + frame + ": the batch matrix is the vessel pose");
    // Spot check: the agent at ship-local (2i, 0, -10) lands where the vessel says that deck point is.
    const local = physics.localPoint(state, 6, 0, -10);
    const [pick] = record.crowdPickRecords();
    const agent3 = record.crowdPickRecords()[3];
    assert.ok(Math.abs(agent3.modelMatrix[12] - local.x) < 1e-3 && Math.abs(agent3.modelMatrix[13] - local.y) < 1e-3 && Math.abs(agent3.modelMatrix[14] - local.z) < 1e-3,
      "pick matrix puts agent 3 at the vessel's world point for its deck position");
    assert.ok(pick);
  }
});

test("an InstancedGLB batch keeps its parentNode through normalization", async () => {
  const api = await makeSceneApiEnv();
  const batch = api.normalizeSceneInstancedGLBMeshEntry({ id: "riders", src: "/p.glb", parentNode: " ship ", instances: [{ id: "a" }] }, 0, null);
  assert.equal(batch.parentNode, "ship");
  assert.equal(api.normalizeSceneInstancedGLBMeshEntry({ id: "walkers", src: "/p.glb", instances: [{ id: "a" }] }, 0, null).parentNode, "");
  assert.equal(api.normalizeSceneInstancedGLBMeshEntry({ id: "x", src: "/p.glb", parentNode: 7, instances: [] }, 0, null).parentNode, "");
});

test("an InstancedGLB batch keeps its palette and each instance's tint index through normalization", async () => {
  const api = await makeSceneApiEnv();
  const batch = api.normalizeSceneInstancedGLBMeshEntry({ id: "shirts", src: "/p.glb", palette: [" #d8a373 ", "#3a5a8c", 7],
    instances: [{ id: "a", tint: 2 }, { id: "b" }, { id: "c", tint: 1.9 }, { id: "d", tint: -3 }, { id: "e", tint: "x" }] }, 0, null);
  assert.deepEqual(Array.from(batch.palette), ["#d8a373", "#3a5a8c", ""]);
  assert.deepEqual(batch.instances.map(instance => instance.tint), [2, undefined, 1, undefined, undefined]);
  assert.equal(api.normalizeSceneInstancedGLBMeshEntry({ id: "plain", src: "/p.glb", instances: [{ id: "a" }] }, 0, null).palette, undefined);
  // An update that omits the palette keeps the mounted one.
  const kept = api.normalizeSceneInstancedGLBMeshEntry({ id: "shirts", src: "/p.glb", instances: [{ id: "a", tint: 1 }] }, 0, batch);
  assert.deepEqual(Array.from(kept.palette), ["#d8a373", "#3a5a8c", ""]);
});

test("a selected agent leaves its batch, unless the batch rides a parent", async () => {
  const api = await makeSceneApiEnv();
  const free = makeCrowd(api, 6);
  free[2].selected = true;
  assert.equal(build(api, free).meshObjects.length, 2, "five batched agents and the selected one on its own record");
  const parented = makeCrowd(api, 6, { parent: "ship" });
  parented[2].selected = true;
  const bundle = build(api, [makeShip(api, yawPose(0, 5, 0, 0)), ...parented]);
  assert.equal(batchRecords(bundle).length, 1);
  assert.equal(batchRecords(bundle)[0].instanceCount, 6, "a record outside the ship would sit in world space");
});

test("crowd agents with different planar-mirror participation never share a batch record", async () => {
  const api = await makeSceneApiEnv();
  const objects = makeCrowd(api, 4);
  objects[0].planarReflection = false;
  objects[1].planarReflection = false;
  const split = build(api, objects), records = batchRecords(split);
  assert.equal(records.length, 2, "two cohorts: the agents that opt out and the agents that do not");
  assert.deepEqual(Array.from(records, record => [record.planarReflection, record.instanceCount]).sort(), [[false, 2], [true, 2]]);
  assert.deepEqual(Array.from(api.sceneReflectOpaqueList(split.meshObjects, split.materials), record => record.planarReflection), [true],
    "the mirror draws the participating cohort only");
  // The agents rejoin one record when they agree again. A patch replaces an agent's record, so the test does too: a batch
  // checks an agent's flags when the agent takes its slot, not while the same object keeps it.
  const agreed = objects.map((object, index) => index < 2 ? Object.assign({}, object, { planarReflection: true }) : object);
  const joined = batchRecords(build(api, agreed));
  assert.deepEqual(Array.from(joined, record => [record.planarReflection, record.instanceCount]), [[true, 4]]);
  // A crowd that never said anything participates.
  assert.deepEqual(Array.from(batchRecords(build(api, makeCrowd(api, 3))), record => [record.planarReflection, record.instanceCount]), [[true, 3]]);
});
