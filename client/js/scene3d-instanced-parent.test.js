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
  assert.equal(api.normalizeSceneInstancedMeshEntry(entry, 0, null).parentNode, undefined);
  assert.equal(api.normalizeSceneInstancedMeshEntry(Object.assign({ parentNode: 7 }, entry), 0, null).parentNode, undefined);
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

test("named material wrappers retain the parent's composed stream across frames and recolors", async () => {
 const api=await makeSceneApiEnv();
 const mesh=windows(api,{parentNode:"ship",material:"glass"});
 const state={instancedMeshes:[mesh],materials:[{name:"glass",kind:"standard",color:"#ffffff"}]};
 const hull=shipPart(api,"ship/hull",translation(50,0,0));
 const build=()=>bundleOf(api,[hull],api.sceneStateInstancedMeshesWithMaterials(state)).instancedMeshes[0];
 const first=build(),second=build();
 assert.equal(first.transforms,second.transforms,"a fresh live-material view must keep its underlying stream owner");
 assert.equal(first._instanceStreamRevision,second._instanceStreamRevision);
 state.materials[0].color="#ff0000";
 const recolored=build();assert.equal(recolored.transforms,second.transforms,"material edits do not change parent transforms");
 mesh.transforms[12]=7;
 const edited=build();assert.equal(edited.transforms[12],57);assert.notEqual(edited.transforms,recolored.transforms);
});

test("streamed count/color snapshots preserve parent-space picking and re-declaration lifetimes", async () => {
  const fs = require("node:fs"), path = require("node:path"), { execFileSync } = require("node:child_process");
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js"); await flushAsyncWork();
  runScript(fs.readFileSync(path.join(__dirname, '../runtime/scene3d/instance-stream.ts'), 'utf8'), env.context, 'instance-stream.ts');
  const api = env.context.__gosx_scene3d_api;
  const fixtures = JSON.parse(execFileSync(process.env.GOSX_GO || 'go', ['run', './client/js/testdata/instance-stream-go-writer-fixture'], {cwd:path.join(__dirname, '../..'),encoding:'utf8'}));
  const state = api.createSceneState({scene:{instancedMeshes:[{id:'tinted',count:0,kind:'box',size:.5,parentNode:'carrier',transforms:[],pickable:true}]}},{tier:'full'});
  const pose = [1,0,0,0,0,1,0,0,0,0,1,0,10,0,0,1];
  const carrier = api.normalizeSceneObject({id:'carrier',kind:'cube'},0); carrier.parentMatrix = pose;
  for (const name of ['tinted-1','tinted-2','tinted-3','tinted-4','tinted-5']) {
    const raw = Buffer.from(fixtures.find(f=>f.name===name).bytes,'base64');
    const bytes = vm.runInContext(`new Uint8Array(${JSON.stringify([...raw])})`, env.context);
    assert.equal(env.context.__gosx_scene3d_instance_stream_apply(state,bytes,()=>{},null).applied,true);
    const bundle = bundleOf(api,[carrier],state.instancedMeshes), entry = bundle.instancedMeshes[0];
    assert.equal(entry.count,state.instancedMeshes[0].count);
    if (entry.count) {
      const center = {x:entry.transforms[12],y:entry.transforms[13],z:entry.transforms[14]};
      assert.equal(center.x,10);
      const hit = api.sceneRaycastPickInstancedMeshes({origin:{...center,z:10},dir:{x:0,y:0,z:-1}},bundle.instancedMeshes,0);
      assert.ok(hit, 'pick must follow streamed position under parent transform');
      const miss = api.sceneRaycastPickInstancedMeshes({origin:{x:0,y:center.y,z:10},dir:{x:0,y:0,z:-1}},bundle.instancedMeshes,0);
      assert.equal(miss,null,'old local position is not pickable in world space');
    } else assert.equal(api.sceneRaycastPickInstancedMeshes({origin:{x:10,y:1,z:10},dir:{x:0,y:0,z:-1}},bundle.instancedMeshes,0),null);
  }
  api.applySceneCommands(state,[{kind:8,data:{instancedMeshes:[]}}]);
  assert.equal(state.instancedMeshes.length,0);
  api.applySceneCommands(state,[{kind:8,data:{instancedMeshes:[{id:'tinted',count:0,kind:'sphere',transforms:[]}]}}]);
  const first = fixtures.find(f=>f.name==='tinted-1');
  const bytes = vm.runInContext(`new Uint8Array(${JSON.stringify([...Buffer.from(first.bytes,'base64')])})`, env.context);
  assert.equal(env.context.__gosx_scene3d_instance_stream_apply(state,bytes,()=>{},null).applied,true,'recreated declaration accepts a new stream lifetime');
  assert.equal(state.instancedMeshes[0].kind,'sphere');
});
