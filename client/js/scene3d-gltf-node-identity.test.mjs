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
