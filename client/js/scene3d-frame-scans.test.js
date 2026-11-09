"use strict";
// Per-frame scans that grow with the scene (docs/game-performance.md, "Where
// the render time goes"). Each test fails if a frame goes back to visiting the
// whole page manifest, every scene object or every crowd agent when nothing
// those scans read has changed. The crowd and focus tests run the built
// bootstrap (regenerate it with cmd/buildbootstrap after a source change).
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { bootstrapSource, createBoardWebGPUHarness, createContext, flushAsyncWork, runScript } = require("./runtime-test-harness.js");

// ---- Water shader sources (WebGPU) ------------------------------------------

const viewport = { width: 64, height: 64 };
const camera = { x: 0, y: 2, z: 12, fov: 60, near: 0.05, far: 200 };

function boxBundle(h) {
  const api = h.env.context.__gosx_scene3d_api;
  const objects = [api.normalizeSceneObject({ id: "box", kind: "box", color: "#88ccff", wireframe: false }, 0, null)];
  return api.createSceneRenderBundle(64, 64, "#000000", camera, objects, [], [], [], [], {}, 0, [], [], [], [], [], 0, false, {});
}

// A manifest script whose text the renderer can see but that declares no
// water shader source; every read is counted.
function countedScript(doc, id, text) {
  const script = doc.createElement("script");
  if (id) script.id = id;
  const reads = { count: 0 };
  Object.defineProperty(script, "textContent", { configurable: true, get() { reads.count += 1; return text; } });
  doc.head.appendChild(script);
  return reads;
}

test("WebGPU reads the page manifest for water shader sources once, not every frame, on a page without them", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const doc = h.env.context.document;
  // Large and holding both marker words, so a scan parses it.
  const text = JSON.stringify({ engines: [{ props: { scene: { waterSystems: [], note: "WGSL" } } }], pad: "x".repeat(1 << 20) });
  const manifest = countedScript(doc, "gosx-manifest", text);
  const frame = async (n) => {
    h.renderer.render(boxBundle(h), viewport, { nowMS: 16 * n, active: true });
    await flushAsyncWork();
  };
  try {
    await frame(1);
    const afterFirst = manifest.count;
    assert.ok(afterFirst > 0, "the first frame scans the manifest");
    for (let n = 2; n <= 6; n++) await frame(n);
    assert.equal(manifest.count, afterFirst, "frames 2 to 6 must not read the manifest text again");
    assert.equal(h.mount.__gosxScene3DWebGPUStats.waterManifestShaderSystems, 0);

    // A new script on the page invalidates the empty result; a source found
    // there is kept, as before.
    const pool = JSON.stringify({ engines: [{ props: { scene: { waterSystems: [{ id: "pool", causticsWGSL: "fn caustics() {}" }] } } }] });
    const extra = countedScript(doc, "", pool);
    await frame(7);
    assert.equal(extra.count, 1, "the added script is scanned once");
    assert.equal(h.mount.__gosxScene3DWebGPUStats.waterManifestShaderSystems, 1);
    const scanned = manifest.count + extra.count;
    for (let n = 8; n <= 10; n++) await frame(n);
    assert.equal(manifest.count + extra.count, scanned, "a found source is not rescanned");
    assert.equal(h.mount.__gosxScene3DWebGPUStats.waterManifestShaderSystems, 1);
  } finally { h.renderer.dispose(); }
});

// ---- Shared fixtures ---------------------------------------------------------

// The bootstrap runs in a vm context, whose Float32Array differs from this
// file's; the runtime tests typed arrays with instanceof, so build them there.
async function sceneAPI() {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  return { api: env.context.__gosx_scene3d_api, F32: vm.runInContext("Float32Array", env.context) };
}

// countedField makes object[name] a getter that counts its reads.
function countedField(object, name, value, reads) {
  Object.defineProperty(object, name, { configurable: true, enumerable: true, get() { reads.n += 1; return value; } });
  return object;
}

// ---- Focus proxies -----------------------------------------------------------

class FakeElement {
  constructor(tagName) { this.tagName = tagName; this.attributes = new Map(); this.style = {}; this.children = []; this.parentNode = null; }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.has(name) ? this.attributes.get(name) : null; }
  removeAttribute(name) { this.attributes.delete(name); }
  addEventListener() {}
  appendChild(child) { return this.insertBefore(child, null); }
  insertBefore(child, reference) {
    if (child.parentNode) child.parentNode.removeChild(child);
    const index = reference ? this.children.indexOf(reference) : -1;
    child.parentNode = this;
    if (index < 0) this.children.push(child); else this.children.splice(index, 0, child);
    return child;
  }
  removeChild(child) { this.children = this.children.filter((item) => item !== child); child.parentNode = null; return child; }
}

function focusRuntime() {
  const host = new FakeElement("div");
  const context = vm.createContext({ document: { createElement: (tag) => new FakeElement(tag) },
    sceneStateObjects: (state) => Array.from(state.objects.values()) });
  vm.runInContext(fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "mount-scene-focus.ts"), "utf8"), context,
    { filename: "mount-scene-focus.ts" });
  const focus = context.setupSceneNodeFocusProxies(host);
  return {
    sync: (state) => context.syncSceneNodeFocusProxies(focus, { meshObjects: [] }, state),
    ids: () => (host.children[0] ? host.children[0].children : []).map((el) => el.getAttribute("data-gosx-scene-node")),
  };
}

test("focus proxies read only the scene state's interactive objects each frame", async () => {
  const { api } = await sceneAPI();
  const state = api.createSceneState({});
  const reads = { n: 0 };
  for (let i = 0; i < 5000; i++) state.objects.set("fan-" + i, countedField({ id: "fan-" + i, label: "Fan" }, "interactive", false, reads));
  state.objects.set("door", { id: "door", interactive: true, label: "Door", interactiveOrder: 2 });
  const focus = focusRuntime();
  focus.sync(state);
  assert.deepEqual(focus.ids(), ["door"]);
  for (let frame = 0; frame < 3; frame++) {
    reads.n = 0;
    focus.sync(state);
    assert.equal(reads.n, 0, "a frame whose table did not change visits no non-interactive object");
  }
  // Interactive writes reach the next frame: an addition, then a removal.
  state.objects.set("gate", { id: "gate", interactive: true, label: "Gate", interactiveOrder: 1 });
  focus.sync(state);
  assert.deepEqual(focus.ids(), ["gate", "door"]);
  state.objects.delete("door");
  focus.sync(state);
  assert.deepEqual(focus.ids(), ["gate"]);
  // Replacing an interactive record with a plain one drops it too.
  state.objects.set("gate", { id: "gate", label: "Gate" });
  focus.sync(state);
  assert.deepEqual(focus.ids(), []);
});

// ---- Crowd partition ---------------------------------------------------------

function cubeVertices(F32) {
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
  return { count: 36, positions: new F32(positions), normals: new F32(normals), uvs: new F32(uvs), tangents: new F32(tangents),
    joints: new F32(joints), weights: new F32(weights), immutable: true, revision: 0 };
}

// Agents shaped like hydrated InstancedGLB crowd objects. reads counts reads
// of an agent's _crowdSkin: the partition reads it once for each agent it visits.
function crowd(fixture, batchID, count, reads, vertices) {
  const { api, F32 } = fixture;
  const proto = api.normalizeSceneObject({ id: "proto", kind: "mesh", vertices: vertices || cubeVertices(F32),
    materialKind: "standard", color: "#c8b27a", pickable: false }, 0);
  const atlas = { id: "atlas-" + batchID };
  const bounds = { minX: -.5, minY: -.5, minZ: -.5, maxX: .5, maxY: .5, maxZ: .5 };
  const agents = [];
  for (let i = 0; i < count; i++) {
    const agent = Object.assign({}, proto, {
      id: `${batchID}/agent-${i}/0`, x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0,
      spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0, driftSpeed: 0, driftPhase: 0,
      static: false, skin: null, parentMatrix: new F32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, i * 2, 0, -10, 1]),
    });
    countedField(agent, "_crowdSkin", { atlas, bounds, rows: new F32([1, 2, .5]), poseRows: null, batchID, rev: 0, group: null, parent: "" }, reads);
    agents.push(agent);
  }
  return agents;
}

const crowdCapabilities = { retainedGeometry: true, worldBakedAttributeViews: true, rigidImportedBatches: true, crowdBatches: true };

// build makes a frame's bundle the way the mount does: the scene state's
// objects, after a list transform when given one, with the state as hierarchy.
function build(api, state, transform) {
  const objects = api.sceneStateObjects(state);
  return api.createSceneRenderBundle(640, 360, "#000000", { x: 0, y: 2, z: 30, fov: 60, near: .1, far: 900 },
    transform ? transform(objects) : objects, [], [], [], [], {}, 0, [], [], [], [], [], 0, false, crowdCapabilities, state);
}

function batches(bundle) {
  const out = {};
  for (const record of bundle.meshObjects) {
    if (record._crowdBatch !== true) continue;
    out[record.crowdMembers[0].id.split("/")[0]] = { record, revision: record.crowdRevision, count: record.instanceCount, members: record.crowdMembers.slice() };
  }
  return out;
}

async function crowdScene(fans, players) {
  const fixture = await sceneAPI();
  const state = fixture.api.createSceneState({});
  const reads = { n: 0 };
  const groups = { fans: crowd(fixture, "fans", fans, reads), players: crowd(fixture, "players", players, reads) };
  for (const agent of groups.fans.concat(groups.players)) state.objects.set(agent.id, agent);
  return Object.assign(fixture, { state, reads, groups });
}

test("a frame whose crowd did not change visits no agent, after any list transform", async () => {
  const { api, state, reads } = await crowdScene(3000, 22);
  const first = batches(build(api, state));
  assert.ok(reads.n >= 3022, "the first frame walks every agent");
  assert.deepEqual(Object.keys(first).sort(), ["fans", "players"]);
  assert.equal(first.fans.count, 3000);
  // A list transform (named materials, quality groups, hierarchy views) hands
  // over a new array of the same objects.
  for (const transform of [null, (list) => list.map((object) => object), (list) => list.slice()]) {
    reads.n = 0;
    const next = batches(build(api, state, transform));
    assert.ok(reads.n <= 2, `an idle frame read ${reads.n} agents' pose records`);
    for (const name of ["fans", "players"]) {
      assert.equal(next[name].record, first[name].record, "the batch record is retained");
      assert.equal(next[name].revision, first[name].revision, "and its revision stands");
    }
  }
});

test("a touched agent re-walks only its own batch, and its pose reaches the record", async () => {
  const { api, state, reads, groups } = await crowdScene(3000, 22);
  const first = batches(build(api, state));
  const skin = groups.players[5]._crowdSkin;
  skin.rows.set([3, 4, .25]);
  skin.rev += 1;
  api.sceneCrowdBatchTouch(groups.players[5]);
  reads.n = 0;
  const next = batches(build(api, state));
  assert.ok(reads.n >= 22 && reads.n <= 24, `the frame read ${reads.n} pose records, want the 22 players`);
  assert.equal(next.fans.revision, first.fans.revision, "the fans did not change");
  assert.equal(next.players.revision, first.players.revision + 1, "the players changed once");
  assert.deepEqual(Array.from(next.players.record.crowdPoses.subarray(15, 18)), [3, 4, .25]);
  reads.n = 0;
  build(api, state);
  assert.ok(reads.n <= 2, "the touch is spent");
});

test("a table write or a touch on an agent that left its batch makes the next frame walk every agent", async () => {
  const { api, state, reads, groups } = await crowdScene(500, 22);
  build(api, state);
  // Replacing a record in place in the table keeps its position.
  const original = groups.fans[7], replacement = Object.assign({}, original);
  countedField(replacement, "_crowdSkin", Object.assign({}, original._crowdSkin, { group: null, rev: 0 }), reads);
  state.objects.set(replacement.id, replacement);
  reads.n = 0;
  let next = batches(build(api, state));
  assert.ok(reads.n >= 522, "a table write walks every agent");
  assert.equal(next.fans.members[7], replacement);
  // A hidden agent holds no slot, so a touch on it is untracked.
  state.objects.set(groups.players[2].id, Object.assign(Object.create(Object.getPrototypeOf(groups.players[2])), groups.players[2], { visible: false }));
  build(api, state);
  api.sceneCrowdBatchTouch(state.objects.get(groups.players[2].id));
  reads.n = 0;
  next = batches(build(api, state));
  assert.ok(reads.n >= 522, "an untracked touch walks every agent");
  assert.equal(next.players.count, 21);
});

test("two scenes whose batches share a group each draw their own agents", async () => {
  const fixture = await sceneAPI();
  const { api, F32 } = fixture;
  const vertices = cubeVertices(F32), reads = { n: 0 };
  const scenes = [0, 1].map(() => {
    const state = api.createSceneState({});
    const agents = crowd(fixture, "fans", 40, reads, vertices);
    for (const agent of agents) state.objects.set(agent.id, agent);
    return { state, agents: new Set(agents) };
  });
  const records = new Set();
  for (let frame = 0; frame < 4; frame++) {
    for (const scene of scenes) {
      const { fans } = batches(build(api, scene.state));
      records.add(fans.record);
      assert.equal(fans.count, 40);
      assert.ok(fans.members.every((member) => scene.agents.has(member)), `frame ${frame}: a batch drew another scene's agents`);
    }
  }
  assert.equal(records.size, 1, "the scenes share one group, so its last walk is not always this scene's");
});

test("a batch whose pose records the hierarchy derives is re-walked every frame, and only that batch", async () => {
  const { api, F32, state, reads } = await crowdScene(2000, 0);
  // hierarchy.ts marks the pose record of each agent view it builds this way.
  const riders = crowd({ api, F32 }, "riders", 12, reads);
  for (const rider of riders) { rider._crowdSkin.derived = true; state.objects.set(rider.id, rider); }
  const first = batches(build(api, state));
  for (let frame = 0; frame < 2; frame++) {
    reads.n = 0;
    const next = batches(build(api, state));
    assert.ok(reads.n >= 12 && reads.n <= 14, `the frame read ${reads.n} pose records, want the 12 riders`);
    assert.equal(next.fans.revision, first.fans.revision);
  }
  riders[4]._crowdSkin.rev += 1; // a view's pose changes without a touch
  assert.equal(batches(build(api, state)).riders.revision, first.riders.revision + 1);
});
