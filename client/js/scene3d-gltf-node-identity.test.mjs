// Regression: gltfPrimitiveID collides when two nodes share one mesh.
//
// Production builds primitive IDs from (mesh.name | mesh index) + channel +
// primitive index + instance suffix. gltfWalkNode passes an empty suffix for
// ordinary nodes, so two nodes referencing the SAME mesh emit identical IDs
// for each primitive. Downstream, staged object/point IDs are stored in Maps
// (state.objects.set(object.id, ...)), so the second node's entries overwrite
// the first and the shared-mesh node disappears from the scene.
//
// Fixture: one named mesh "shared" with three primitives (TRIANGLES mode 4,
// POINTS mode 0, LINES mode 1), referenced by two root nodes at distinct
// translations. Expected today if IDs were correct: 4 objects, 2 points, all
// unique IDs, and per-node world-space positions. Baseline must FAIL the
// uniqueness assertions because both nodes currently emit
// "shared-prim-0" / "shared-points-1" / "shared-lines-2".

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  return fs.readFileSync(name.startsWith("../") ? path.join(__dirname, name) : path.join(srcDir, name), "utf8");
}

function createLoaderContext() {
  const warnings = [];
  const sandbox = {
    console: {
      warn: (...args) => warnings.push(args.join(" ")),
      error: () => {},
      log: () => {},
    },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    isFinite,
    Float32Array,
    Uint8Array,
    Uint16Array,
    Uint32Array,
    Int8Array,
    Int16Array,
    ArrayBuffer,
    DataView,
    TextDecoder,
    Error,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;

  const context = vm.createContext(sandbox);
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readSource("../runtime/scene3d/gltf.ts"), context, { filename: "gltf.ts" });
  return { context, sandbox, warnings };
}

function call(context, expression) {
  return vm.runInContext(expression, context);
}

// plain strips VM realm prototypes so node:assert can compare values.
function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

// extractScene binds the ACTUAL gltfDoc and binaryBuffer into the VM context,
// calls production exactly once, and returns realm-free records. Mesh objects
// expose production's vertices.positions / vertices.count; line objects
// expose production's points array of {x,y,z}; point entries expose
// entry.positions / entry.count. Everything is copied into host plain arrays.
function extractScene(context, gltfDoc, binaryBuffer) {
  context.gltfDoc = gltfDoc;
  context.binaryBuffer = binaryBuffer;
  const result = call(context, "gltfExtractScene(gltfDoc, binaryBuffer)");

  const objects = Array.from(result.objects || [], (o) => {
    if (o && o.vertices && o.vertices.positions) {
      // Mesh (TRIANGLES) object: positions live in o.vertices.positions.
      return {
        id: o.id,
        kind: "mesh",
        count: o.vertices.count,
        positions: Array.from(o.vertices.positions),
      };
    }
    if (o && Array.isArray(o.points)) {
      // Line object: o.points is an array of {x,y,z} records.
      const flat = [];
      for (const p of o.points) flat.push(p.x, p.y, p.z);
      return {
        id: o.id,
        kind: "line",
        count: o.points.length,
        positions: flat,
      };
    }
    return {
      id: o ? o.id : undefined,
      kind: "unknown",
      count: o ? o.count : 0,
      positions: o && o.positions ? Array.from(o.positions) : null,
    };
  });

  const points = Array.from(result.points || [], (p) => ({
    id: p.id,
    kind: "points",
    count: p.count,
    positions: p.positions ? Array.from(p.positions) : null,
  }));

  return { objects, points };
}

// --- Fixture ----------------------------------------------------------------

// One mesh, three primitives sharing the name "shared":
//   prim 0: TRIANGLES (mode 4), 3 verts
//   prim 1: POINTS (mode 0), 3 verts
//   prim 2: LINES (mode 1), 4 verts
// Authored (local) positions:
//   tri:   (0,0,0) (1,0,0) (0,1,0)
//   pts:   (2,0,0) (3,0,0) (4,0,0)
//   lines: (5,0,0) (6,0,0) (6,1,0) (5,1,0)
const LOCAL_POSITIONS = [
  0, 0, 0, 1, 0, 0, 0, 1, 0,
  2, 0, 0, 3, 0, 0, 4, 0, 0,
  5, 0, 0, 6, 0, 0, 6, 1, 0, 5, 1, 0,
];

function buildBinary() {
  const floats = Float32Array.from(LOCAL_POSITIONS);
  return floats.buffer;
}

function buildDoc(nodeTranslations, meshName) {
  return {
    asset: { version: "2.0" },
    scene: 0,
    scenes: [{ nodes: nodeTranslations.map((_, i) => i) }],
    nodes: nodeTranslations.map((t) => ({ mesh: 0, translation: t })),
    meshes: [
      {
        name: meshName,
        primitives: [
          { mode: 4, attributes: { POSITION: 0 } },
          { mode: 0, attributes: { POSITION: 1 } },
          { mode: 1, attributes: { POSITION: 2 } },
        ],
      },
    ],
    accessors: [
      { bufferView: 0, componentType: 5126, count: 3, type: "VEC3", min: [0, 0, 0], max: [1, 1, 0] },
      { bufferView: 1, componentType: 5126, count: 3, type: "VEC3", min: [2, 0, 0], max: [4, 0, 0] },
      { bufferView: 2, componentType: 5126, count: 4, type: "VEC3", min: [5, 0, 0], max: [6, 1, 0] },
    ],
    bufferViews: [
      { buffer: 0, byteOffset: 0, byteLength: 36 },
      { buffer: 0, byteOffset: 36, byteLength: 36 },
      { buffer: 0, byteOffset: 72, byteLength: 48 },
    ],
    buffers: [{ byteLength: 120 }],
  };
}

// --- Tests ------------------------------------------------------------------

test("shared mesh across two nodes emits unique object and point IDs", () => {
  const { context } = createLoaderContext();
  const binary = buildBinary();
  const doc = buildDoc([[10, 0, 0], [0, 10, 0]], "shared");
  const result = extractScene(context, doc, binary);

  assert.equal(result.objects.length, 4, "expected 2 tri + 2 line objects");
  assert.equal(result.points.length, 2, "expected 2 point entries");

  const objectIDs = result.objects.map((o) => o.id);
  assert.equal(new Set(objectIDs).size, objectIDs.length,
    "object IDs must be unique, got: " + objectIDs.join(", "));

  const pointIDs = result.points.map((p) => p.id);
  assert.equal(new Set(pointIDs).size, pointIDs.length,
    "point IDs must be unique, got: " + pointIDs.join(", "));
});

test("shared mesh nodes land at distinct world-space positions", () => {
  const { context } = createLoaderContext();
  const binary = buildBinary();
  const doc = buildDoc([[10, 0, 0], [0, 10, 0]], "shared");
  const result = extractScene(context, doc, binary);

  const firstVert = (entry) => entry.positions.slice(0, 3);
  const vecEq = (a, b) =>
    a.length === 3 && b.length === 3 &&
    a[0] === b[0] && a[1] === b[1] && a[2] === b[2];

  // TRI objects: first vertex (0,0,0) local -> (10,0,0) or (0,10,0) world.
  const triObjects = result.objects.filter((o) => vecEq(firstVert(o), [10, 0, 0]) || vecEq(firstVert(o), [0, 10, 0]));
  assert.equal(triObjects.length, 2, "both tri objects must be present at translated origins");
  assert.ok(vecEq(firstVert(triObjects[0]), [10, 0, 0]) !== vecEq(firstVert(triObjects[1]), [10, 0, 0]),
    "the two tri objects must sit at DIFFERENT nodes, not the same one twice");

  // LINE objects: first vertex (5,0,0) local -> (15,0,0) or (5,10,0) world.
  const lineFirsts = result.objects
    .filter((o) => !vecEq(firstVert(o), [10, 0, 0]) && !vecEq(firstVert(o), [0, 10, 0]))
    .map(firstVert);
  assert.equal(lineFirsts.length, 2);
  const hasLineA = lineFirsts.some((v) => vecEq(v, [15, 0, 0]));
  const hasLineB = lineFirsts.some((v) => vecEq(v, [5, 10, 0]));
  assert.ok(hasLineA && hasLineB, "line objects must come from both nodes");

  // POINT entries: first vertex (2,0,0) local -> (12,0,0) or (2,10,0) world.
  const pointFirsts = result.points.map(firstVert);
  assert.ok(pointFirsts.some((v) => vecEq(v, [12, 0, 0])), "node A points missing");
  assert.ok(pointFirsts.some((v) => vecEq(v, [2, 10, 0])), "node B points missing");
});

test("repeated extraction is deterministic and does not mutate the document or binary", () => {
  const { context } = createLoaderContext();
  const binary = buildBinary();
  const binarySnapshot = Array.from(new Uint8Array(binary));
  const doc = buildDoc([[10, 0, 0], [0, 10, 0]], "shared");
  const docSnapshot = plain(doc);

  const first = extractScene(context, doc, binary);
  const second = extractScene(context, doc, binary);

  assert.deepEqual(second, first, "repeated extraction must yield identical output");
  assert.deepEqual(plain(doc), docSnapshot, "gltf document must not be mutated");
  assert.deepEqual(Array.from(new Uint8Array(binary)), binarySnapshot, "binary buffer must not be mutated");
});

test("ordinary single-node fixture retains legacy primitive IDs (compatibility)", () => {
  const { context } = createLoaderContext();
  const binary = buildBinary();
  const doc = buildDoc([[0, 0, 0]], "shared");
  const result = extractScene(context, doc, binary);

  assert.equal(result.objects.length, 2);
  assert.equal(result.points.length, 1);

  const objectIDs = result.objects.map((o) => o.id).sort();
  assert.deepEqual(objectIDs, ["shared-lines-2", "shared-prim-0"]);
  assert.deepEqual(result.points.map((p) => p.id), ["shared-points-1"]);
});

// --- EXT_mesh_gpu_instancing on a reused mesh --------------------------------

// Same tri/points/lines mesh as the base fixture, now referenced by two nodes
// that each carry EXT_mesh_gpu_instancing with two TRANSLATION instances.
// Instance-local translations:
//   node A (translation [1,0,0]): [100,0,0], [200,0,0]
//   node B (translation [0,1,0]): [0,100,0], [0,200,0]
// The production id suffix "-inst-<n>" only distinguishes instances WITHIN a
// node, so two nodes reusing one mesh still collide per (primitive, instance).
function buildInstancedBinary() {
  // 30 floats of LOCAL_POSITIONS (120 bytes) + 2 nodes x 2 instances x VEC3.
  const floats = new Float32Array(42);
  floats.set(Float32Array.from(LOCAL_POSITIONS), 0);
  floats.set([100, 0, 0, 200, 0, 0], 30); // node A instance translations
  floats.set([0, 100, 0, 0, 200, 0], 36); // node B instance translations
  return floats.buffer;
}

function buildInstancedDoc() {
  return {
    asset: { version: "2.0" },
    extensionsUsed: ["EXT_mesh_gpu_instancing"],
    scene: 0,
    scenes: [{ nodes: [0, 1] }],
    nodes: [
      {
        mesh: 0,
        translation: [1, 0, 0],
        extensions: { EXT_mesh_gpu_instancing: { attributes: { TRANSLATION: 3 } } },
      },
      {
        mesh: 0,
        translation: [0, 1, 0],
        extensions: { EXT_mesh_gpu_instancing: { attributes: { TRANSLATION: 4 } } },
      },
    ],
    meshes: [
      {
        name: "shared",
        primitives: [
          { mode: 4, attributes: { POSITION: 0 } },
          { mode: 0, attributes: { POSITION: 1 } },
          { mode: 1, attributes: { POSITION: 2 } },
        ],
      },
    ],
    accessors: [
      { bufferView: 0, componentType: 5126, count: 3, type: "VEC3", min: [0, 0, 0], max: [1, 1, 0] },
      { bufferView: 1, componentType: 5126, count: 3, type: "VEC3", min: [2, 0, 0], max: [4, 0, 0] },
      { bufferView: 2, componentType: 5126, count: 4, type: "VEC3", min: [5, 0, 0], max: [6, 1, 0] },
      { bufferView: 3, componentType: 5126, count: 2, type: "VEC3", min: [100, 0, 0], max: [200, 0, 0] },
      { bufferView: 4, componentType: 5126, count: 2, type: "VEC3", min: [0, 100, 0], max: [0, 200, 0] },
    ],
    bufferViews: [
      { buffer: 0, byteOffset: 0, byteLength: 36 },
      { buffer: 0, byteOffset: 36, byteLength: 36 },
      { buffer: 0, byteOffset: 72, byteLength: 48 },
      { buffer: 0, byteOffset: 120, byteLength: 24 },
      { buffer: 0, byteOffset: 144, byteLength: 24 },
    ],
    buffers: [{ byteLength: 168 }],
  };
}

test("instanced reused mesh emits unique IDs per node, instance, and channel", () => {
  const { context } = createLoaderContext();
  const binary = buildInstancedBinary();
  const doc = buildInstancedDoc();
  const result = extractScene(context, doc, binary);

  // 2 nodes x 2 instances x (tri + line) objects; 2 x 2 point entries.
  assert.equal(result.objects.length, 8, "expected 8 objects (2 nodes x 2 instances x tri+line)");
  assert.equal(result.points.length, 4, "expected 4 point entries (2 nodes x 2 instances)");

  const objectIDs = result.objects.map((o) => o.id);
  assert.equal(new Set(objectIDs).size, objectIDs.length,
    "object IDs must be unique across both nodes and all instance copies, got: " + objectIDs.join(", "));

  const pointIDs = result.points.map((p) => p.id);
  assert.equal(new Set(pointIDs).size, pointIDs.length,
    "point IDs must be unique across both nodes and all instance copies, got: " + pointIDs.join(", "));

  const vecEq = (a, b) => a[0] === b[0] && a[1] === b[1] && a[2] === b[2];
  // TRI first vertex is (0,0,0) local; world = node translation + instance
  // translation: (1,0,0)+[100,0,0]=(101,0,0), etc.
  const triFirsts = result.objects
    .filter((o) => o.kind === "mesh")
    .map((o) => Array.from(o.positions.slice(0, 3)));
  assert.equal(triFirsts.length, 4);
  const triExpected = [[101, 0, 0], [201, 0, 0], [0, 101, 0], [0, 201, 0]];
  for (const expected of triExpected) {
    assert.ok(triFirsts.some((v) => vecEq(v, expected)),
      "missing tri instance at world position " + expected.join(","));
  }

  // POINT first vertex is (2,0,0) local.
  const pointFirsts = result.points.map((p) => Array.from(p.positions.slice(0, 3)));
  const pointExpected = [[103, 0, 0], [203, 0, 0], [2, 101, 0], [2, 201, 0]];
  for (const expected of pointExpected) {
    assert.ok(pointFirsts.some((v) => vecEq(v, expected)),
      "missing point instance at world position " + expected.join(","));
  }

  // LINE first vertex is (5,0,0) local.
  const lineFirsts = result.objects
    .filter((o) => o.kind === "line")
    .map((o) => Array.from(o.positions.slice(0, 3)));
  assert.equal(lineFirsts.length, 4);
  const lineExpected = [[106, 0, 0], [206, 0, 0], [5, 101, 0], [5, 201, 0]];
  for (const expected of lineExpected) {
    assert.ok(lineFirsts.some((v) => vecEq(v, expected)),
      "missing line instance at world position " + expected.join(","));
  }
});

test("instanced reused mesh extraction is deterministic", () => {
  const { context } = createLoaderContext();
  const binary = buildInstancedBinary();
  const doc = buildInstancedDoc();
  const first = extractScene(context, doc, binary);
  const second = extractScene(context, doc, binary);
  assert.deepEqual(second, first, "repeated instanced extraction must yield identical output");
});

// --- Point overlay on a reused mesh ------------------------------------------

// Base point entries: local positions (2,0,0) (3,0,0) (4,0,0).
// The overlay supplies genuinely DIFFERENT local positions
// (50,0,0) (60,0,0) (70,0,0) from its own binary, with matching accessor
// min/max, so a no-op apply (or a partial single-entry patch) cannot pass.
function buildPointsBinary() {
  return Float32Array.from([2, 0, 0, 3, 0, 0, 4, 0, 0]).buffer;
}

function buildOverlayBinary() {
  return Float32Array.from([50, 0, 0, 60, 0, 0, 70, 0, 0]).buffer;
}

function buildPointsDoc(translate, nodeExtras, overlayPositions) {
  const nodes = translate.map((t, i) => {
    const node = { mesh: 0, translation: t };
    if (nodeExtras) node.extras = nodeExtras(i);
    return node;
  });
  const positions = overlayPositions ? [50, 0, 0, 60, 0, 0, 70, 0, 0] : [2, 0, 0, 3, 0, 0, 4, 0, 0];
  return {
    asset: { version: "2.0" },
    scene: 0,
    scenes: [{ nodes: nodes.map((_, i) => i) }],
    nodes,
    meshes: [{ name: "shared", primitives: [{ mode: 0, attributes: { POSITION: 0 } }] }],
    accessors: [
      {
        bufferView: 0,
        componentType: 5126,
        count: 3,
        type: "VEC3",
        min: [positions[0], positions[1], positions[2]],
        max: [positions[6], positions[7], positions[8]],
      },
    ],
    bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: 36 }],
    buffers: [{ byteLength: 36 }],
  };
}

function collectOverlay(context, overlayDoc, binary) {
  context.overlayDoc = overlayDoc;
  context.binaryBuffer = binary;
  return call(context, "gltfCollectPointOverlay(overlayDoc, binaryBuffer)");
}

function applyOverlay(context, scene, overlay) {
  context.baseScene = scene;
  context.overlayPatch = overlay;
  call(context, "gltfApplyPointOverlay(baseScene, overlayPatch)");
}

test("point overlay replaces positions on both reused-mesh point entries", () => {
  const { context } = createLoaderContext();
  const baseBinary = buildPointsBinary();
  const doc = buildPointsDoc([[10, 0, 0], [0, 10, 0]], null, false);
  const docSnapshot = plain(doc);
  const binarySnapshot = Array.from(new Uint8Array(baseBinary));
  const base = extractScene(context, doc, baseBinary);

  assert.equal(base.points.length, 2, "base must extract one point entry per node");
  const baseIDs = base.points.map((p) => p.id);
  assert.equal(new Set(baseIDs).size, 2, "base point IDs must be unique per node, got: " + baseIDs.join(", "));
  const before = Object.fromEntries(base.points.map((p) => [p.id, Array.from(p.positions)]));

  const overlayBinary = buildOverlayBinary();
  const overlayBinarySnapshot = Array.from(new Uint8Array(overlayBinary));
  const overlayDoc = buildPointsDoc([[10, 0, 0], [0, 10, 0]], null, true);
  const overlayDocSnapshot = plain(overlayDoc);
  const overlay = collectOverlay(context, overlayDoc, overlayBinary);

  const overlayKeys = Object.keys(overlay);
  assert.equal(overlayKeys.length, 2,
    "matching overlay keyset must have exactly two entries, got: " + overlayKeys.join(", "));
  assert.deepEqual(overlayKeys.slice().sort(), baseIDs.slice().sort(),
    "overlay keys must match the base extraction point IDs one-to-one");

  const scene = { points: base.points.map((p) => ({ ...p, positions: p.positions.slice() })) };
  applyOverlay(context, scene, overlay);

  // Exact per-node overlay world positions: node A (translation [10,0,0]) gets
  // (60,0,0)(70,0,0)(80,0,0); node B (translation [0,10,0]) gets
  // (50,10,0)(60,10,0)(70,10,0).
  const nodeAPositions = [60, 0, 0, 70, 0, 0, 80, 0, 0];
  const nodeBPositions = [50, 10, 0, 60, 10, 0, 70, 10, 0];
  const matched = [];
  for (const entry of scene.points) {
    assert.equal(entry.count, 3, "entry " + entry.id + " count must be unchanged");
    assert.ok(entry._cachedPos === entry.positions,
      "entry " + entry.id + " must share the cached positions twin");
    assert.notDeepEqual(Array.from(entry.positions), before[entry.id],
      "entry " + entry.id + " positions must actually change under the overlay");
    if (JSON.stringify(Array.from(entry.positions)) === JSON.stringify(nodeAPositions)) {
      matched.push("A");
    } else if (JSON.stringify(Array.from(entry.positions)) === JSON.stringify(nodeBPositions)) {
      matched.push("B");
    } else {
      assert.fail("entry " + entry.id + " positions must equal one node's exact overlay world positions, got " +
        Array.from(entry.positions).join(","));
    }
  }
  assert.deepEqual(matched.sort(), ["A", "B"],
    "both entries must be patched, each with a DIFFERENT node's overlay positions");
  assert.deepEqual(scene.points.map((e) => e.id).sort(), baseIDs.slice().sort(),
    "entry IDs must be unchanged after patching");
  assert.equal(scene.points.length, 2, "entry count must be unchanged after patching");

  assert.deepEqual(plain(doc), docSnapshot, "base document must not be mutated");
  assert.deepEqual(Array.from(new Uint8Array(baseBinary)), binarySnapshot, "base binary must not be mutated");
  assert.deepEqual(plain(overlayDoc), overlayDocSnapshot, "overlay document must not be mutated");
  assert.deepEqual(Array.from(new Uint8Array(overlayBinary)), overlayBinarySnapshot,
    "overlay binary must not be mutated");
});

test("authored extras ids stay authoritative and the overlay patches both authored entries", () => {
  const { context } = createLoaderContext();
  const authored = (i) => ({ gosx: { id: "authored-pts-" + i }, scene3d: { id: "authored-pts-" + i } });
  const baseBinary = buildPointsBinary();
  const doc = buildPointsDoc([[10, 0, 0], [0, 10, 0]], authored, false);
  const base = extractScene(context, doc, baseBinary);

  // Distinct nodes carry distinct authored IDs; they pass through unchanged.
  assert.deepEqual(base.points.map((p) => p.id), ["authored-pts-0", "authored-pts-1"]);
  const before0 = Array.from(base.points[0].positions);
  const before1 = Array.from(base.points[1].positions);

  const overlay = collectOverlay(context, buildPointsDoc([[10, 0, 0], [0, 10, 0]], authored, true), buildOverlayBinary());
  const overlayKeys = Object.keys(overlay);
  assert.equal(overlayKeys.length, 2, "overlay must key both authored entries exactly");
  assert.deepEqual(overlayKeys.slice().sort(), ["authored-pts-0", "authored-pts-1"],
    "overlay must key by the same authored extras.id the base extraction used");

  const scene = { points: base.points.map((p) => ({ ...p, positions: p.positions.slice() })) };
  applyOverlay(context, scene, overlay);

  const byID = Object.fromEntries(scene.points.map((e) => [e.id, e]));
  assert.deepEqual(Array.from(byID["authored-pts-0"].positions), [60, 0, 0, 70, 0, 0, 80, 0, 0],
    "authored-pts-0 must carry node A's exact overlay world positions");
  assert.deepEqual(Array.from(byID["authored-pts-1"].positions), [50, 10, 0, 60, 10, 0, 70, 10, 0],
    "authored-pts-1 must carry node B's exact overlay world positions");
  assert.notDeepEqual(Array.from(byID["authored-pts-0"].positions), before0,
    "authored-pts-0 data must actually change under the overlay");
  assert.notDeepEqual(Array.from(byID["authored-pts-1"].positions), before1,
    "authored-pts-1 data must actually change under the overlay");
  assert.ok(byID["authored-pts-0"]._cachedPos === byID["authored-pts-0"].positions);
  assert.ok(byID["authored-pts-1"]._cachedPos === byID["authored-pts-1"].positions);
});
