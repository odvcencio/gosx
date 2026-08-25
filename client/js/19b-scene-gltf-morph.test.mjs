// glTF morph-target tests — primitive.targets extraction and static baking.
//
// These tests follow the same pattern as 19-scene-gltf.test.mjs: bootstrap
// fragments load into ONE VM context and the tests call the loader functions
// directly. Every fixture is a small JSON glTF whose binary chunk is built
// inside the VM, so typed-array identity matches the loader's realm. No
// network and no GPU involved.
//
// Scope of these tests (first morph-target checkpoint):
//   - primitive.targets extraction for POSITION, NORMAL, and TANGENT
//   - index-map expansion so target vertex v matches expanded base vertex v
//   - node default weights overriding mesh default weights
//   - additive weighted-delta accumulation across multiple targets
//   - morphed position/normal/tangent data with tangent w preserved
//   - UV data untouched by morph handling; inputs never mutated

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
  return { context };
}

// call runs one expression inside the VM and returns a realm-free result.
function call(context, expression) {
  return vm.runInContext(expression, context);
}

// plain deep-copies a value out of the loader's VM realm into ordinary
// host-realm objects so assert.deepEqual compares like with like. Every value
// handed here is JSON-safe by construction: the VM expressions flatten typed
// arrays with Array.from before returning them.
function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

// --- Fixture ----------------------------------------------------------------
//
// One triangle, three base vertices, referenced through indices [1, 2, 0] so
// the flat per-corner streams exercise index-map expansion. Two morph targets:
//
//   target 0: POSITION/NORMAL/TANGENT deltas (distinct per vertex)
//   target 1: POSITION/NORMAL deltas only (no tangent channel)
//
// Buffer layout (one bufferView over everything):
//   bytes   0-143  base floats: 3x POSITION, 3x NORMAL, 3x TANGENT(vec4),
//                  3x UV = 9 + 9 + 12 + 6 = 36 floats
//   bytes 144-323  target floats: t0 pos(9) nrm(9) tan(9), t1 pos(9) nrm(9);
//                  morph TANGENT deltas are VEC3 like every other delta
//   bytes 324-329  indices [1, 2, 0] as UINT16

const BASE_POSITIONS = [0, 0, 0, 1, 0, 0, 0, 1, 0];
const BASE_NORMALS = [0, 0, 1, 0, 0, 1, 0, 0, 1];
const BASE_TANGENTS = [1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1];
const BASE_UVS = [0, 0, 1, 0, 0, 1];
const TARGET0_POSITIONS = [0.5, 0, 0, 0, 0.5, 0, 0, 0, 0.5];
const TARGET0_NORMALS = [0, 0, 0.25, 0, 0, 0.25, 0, 0, 0.25];
// Three xyz direction deltas per vertex; there is no authored w component.
const TARGET0_TANGENTS = [0, 1, 0, 0, 1, 0, 0, 1, 0];
const TARGET1_POSITIONS = [0, 0.25, 0, 0, 0.25, 0, 0, 0, 0.25];
const TARGET1_NORMALS = [0.5, 0, 0, 0.5, 0, 0, 0.5, 0, 0];

// Float payload in accessor order; offsets below are derived from it.
const FIXTURE_FLOATS = [
  ...BASE_POSITIONS,      // accessor 0 POSITION
  ...BASE_NORMALS,        // accessor 1 NORMAL
  ...BASE_TANGENTS,       // accessor 2 TANGENT
  ...BASE_UVS,            // accessor 3 TEXCOORD_0
  ...TARGET0_POSITIONS,   // accessor 5 (byte 144)
  ...TARGET0_NORMALS,     // accessor 6 (byte 180)
  ...TARGET0_TANGENTS,    // accessor 7 (byte 216, VEC3)
  ...TARGET1_POSITIONS,   // accessor 8 (byte 252)
  ...TARGET1_NORMALS,     // accessor 9 (byte 288)
];

// Indices sit right after the float payload inside the same bufferView.
const INDICES_BYTE_OFFSET = FIXTURE_FLOATS.length * 4;

function vec3(byteOffset) {
  return { bufferView: 0, byteOffset, componentType: 5126, count: 3, type: "VEC3" };
}

function morphDoc(meshWeights) {
  return {
    asset: { version: "2.0" },
    bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: FIXTURE_FLOATS.length * 4 + 8 }],
    accessors: [
      vec3(0),
      vec3(36),
      { bufferView: 0, byteOffset: 72, componentType: 5126, count: 3, type: "VEC4" },
      { bufferView: 0, byteOffset: 120, componentType: 5126, count: 3, type: "VEC2" },
      { bufferView: 0, byteOffset: INDICES_BYTE_OFFSET, componentType: 5123, count: 3, type: "SCALAR" },
      vec3(144),
      vec3(180),
      { bufferView: 0, byteOffset: 216, componentType: 5126, count: 3, type: "VEC3" },
      vec3(252),
      vec3(288),
    ],
    meshes: [{
      name: "tri",
      weights: meshWeights.mesh,
      primitives: [{
        attributes: { POSITION: 0, NORMAL: 1, TANGENT: 2, TEXCOORD_0: 3 },
        indices: 4,
        mode: 4,
        targets: [
          { POSITION: 5, NORMAL: 6, TANGENT: 7 },
          { POSITION: 8, NORMAL: 9 },
        ],
      }],
    }],
    nodes: [
      { mesh: 0 },
      { mesh: 0, weights: meshWeights.node },
    ],
    scenes: [{ nodes: [0, 1] }],
  };
}

// Builds the binary chunk and document inside the VM and stashes them on the
// sandbox global so several tests can share one extraction.
function loadFixture(context, meshWeights) {
  call(context, `
    var floats = ${JSON.stringify(FIXTURE_FLOATS)};
    var indices = [1, 2, 0];
    var buffer = new ArrayBuffer(floats.length * 4 + 8);
    new Float32Array(buffer, 0, floats.length).set(floats);
    new Uint16Array(buffer, floats.length * 4, 3).set(indices);
    var morphDoc = ${JSON.stringify(morphDoc(meshWeights))};
    var morphScene = null;
  `);
}

// Extracted flat corners after the [1, 2, 0] index fan-out: corner k carries
// source vertex indices[k].
const FLAT_TARGET0_POSITIONS = [0, 0.5, 0, 0, 0, 0.5, 0.5, 0, 0];

// --- primitive.targets extraction -------------------------------------------

test("primitive targets extract for POSITION, NORMAL, and TANGENT through the index map", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [0.5, 0.25], node: [1, 0] });
  const result = plain(call(context, `
    var geometry = gltfExtractMeshPrimitive(morphDoc, morphDoc.meshes[0].primitives[0], buffer, null);
    ({
      count: geometry.count,
      morphCount: geometry.morphTargets ? geometry.morphTargets.length : 0,
      t0Positions: Array.from(geometry.morphTargets[0].positions),
      t0NormalsLength: geometry.morphTargets[0].normals.length,
      t0TangentsLength: geometry.morphTargets[0].tangents.length,
      hasT1Tangents: Boolean(geometry.morphTargets[1].tangents),
      t1NormalsLength: geometry.morphTargets[1].normals.length,
      basePositions: Array.from(geometry.positions),
      baseUvs: Array.from(geometry.uvs),
    });
  `));

  assert.equal(result.count, 3);
  assert.equal(result.morphCount, 2);
  // Fan-out through indices [1, 2, 0]: each corner gets its own vertex delta.
  assert.deepEqual(result.t0Positions, FLAT_TARGET0_POSITIONS);
  assert.equal(result.t0NormalsLength, 9);
  // Morph TANGENT deltas are VEC3: three components per expanded corner.
  assert.equal(result.t0TangentsLength, 9);
  assert.equal(result.hasT1Tangents, false);
  assert.equal(result.t1NormalsLength, 9);

  // Reading targets leaves every base stream untouched.
  assert.deepEqual(result.basePositions, [1, 0, 0, 0, 1, 0, 0, 0, 0]);
  assert.deepEqual(result.baseUvs, [1, 0, 0, 1, 0, 0]);
});

test("unindexed primitives extract owned copies of their targets", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [0.5, 0.25], node: [1, 0] });
  const result = plain(call(context, `
    var unindexed = Object.assign({}, morphDoc.meshes[0].primitives[0], { indices: undefined });
    var geometry = gltfExtractMeshPrimitive(morphDoc, unindexed, buffer, null);
    ({
      positions: Array.from(geometry.positions),
      t0Positions: Array.from(geometry.morphTargets[0].positions),
    });
  `));
  assert.deepEqual(result.positions, BASE_POSITIONS);
  assert.deepEqual(result.t0Positions, TARGET0_POSITIONS);
});

// --- default weights ---------------------------------------------------------

test("node weights override mesh defaults on extracted objects", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [0.5, 0.25], node: [1, 0] });
  const result = plain(call(context, `
    var scene = gltfExtractScene(morphDoc, buffer);
    ({
      meshDefaults: Array.from(scene.objects[0].vertices.morphWeights),
      nodeOverride: Array.from(scene.objects[1].vertices.morphWeights),
      targetCounts: scene.objects.map(function(o) { return o.vertices.morphTargets.length; }),
      objectCount: scene.objects.length,
    });
  `));

  assert.equal(result.objectCount, 2);
  // A plain node keeps the mesh's own default weights.
  assert.deepEqual(result.meshDefaults, [0.5, 0.25]);
  // A node instantiating the mesh overrides them wholesale (glTF 2.0).
  assert.deepEqual(result.nodeOverride, [1, 0]);
  assert.deepEqual(result.targetCounts, [2, 2]);

  // Short authored lists leave trailing targets at zero rather than leaking.
  const partial = plain((() => {
    const { context: ctx } = createLoaderContext();
    loadFixture(ctx, { mesh: [0.75], node: [0.5, 0.5, 0.125] });
    return JSON.parse(JSON.stringify(call(ctx, `
      var scene = gltfExtractScene(morphDoc, buffer);
      ({
        paddedMesh: Array.from(scene.objects[0].vertices.morphWeights),
        truncatedNode: Array.from(scene.objects[1].vertices.morphWeights),
      });
    `)));
  })());
  assert.deepEqual(partial.paddedMesh, [0.75, 0]);
  assert.deepEqual(partial.truncatedNode, [0.5, 0.5]);
});

// --- additive weighted-delta application -------------------------------------

test("multiple targets accumulate additively with weighted deltas", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    var positions = new Float32Array([1, 0, 0, 0, 1, 0, 0, 0, 0]);
    var normals = new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]);
    var tangents = new Float32Array([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]);
    var targets = [
      {
        positions: new Float32Array(${JSON.stringify(FLAT_TARGET0_POSITIONS)}),
        normals: new Float32Array([0, 0, 0.25, 0, 0, 0.25, 0, 0, 0.25]),
        tangents: new Float32Array([0, 1, 0, 0, 1, 0, 0, 1, 0]),
      },
      {
        positions: new Float32Array([0, 0.25, 0, 0, 0.25, 0, 0, 0.25, 0]),
        normals: new Float32Array([0.5, 0, 0, 0.5, 0, 0, 0.5, 0, 0]),
      },
    ];
    var weights = [0.5, 0.25];
    var baked = gltfApplyMorphWeights(positions, normals, tangents, targets, weights);
    ({
      positions: Array.from(baked.positions),
      normals: Array.from(baked.normals),
      tangents: Array.from(baked.tangents),
      inputUntouched: Array.from(positions)[0] === 1 && Array.from(normals)[2] === 1,
      freshBuffers: baked.positions !== positions && baked.normals !== normals && baked.tangents !== tangents,
    });
  `));

  // position + 0.5*target0 + 0.25*target1 per corner.
  assert.deepEqual(result.positions.map((v) => Math.round(v * 1e6) / 1e6), [
    1, 0.3125, 0,
    0, 1.0625, 0.25,
    0.25, 0.0625, 0,
  ]);
  // normal + 0.5*(0,0,0.25) + 0.25*(0.5,0,0) per corner.
  assert.deepEqual(result.normals.map((v) => Math.round(v * 1e6) / 1e6), [
    0.125, 0, 1.125,
    0.125, 0, 1.125,
    0.125, 0, 1.125,
  ]);
  // Tangent xyz shifts by 0.5*(0,1,0); each base w (1) survives untouched.
  assert.deepEqual(result.tangents.map((v) => Math.round(v * 1e6) / 1e6), [
    1, 0.5, 0, 1,
    1, 0.5, 0, 1,
    1, 0.5, 0, 1,
  ]);
  assert.equal(result.inputUntouched, true);
  assert.equal(result.freshBuffers, true);
});

test("zero-weight targets are skipped entirely", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    var positions = new Float32Array([1, 2, 3]);
    var targets = [{ positions: new Float32Array([10, -10, 4]) }, { positions: new Float32Array([100, 100, 100]) }];
    var baked = gltfApplyMorphWeights(positions, null, null, targets, [0, 1]);
    ({ out: Array.from(baked.positions), hasNormals: Boolean(baked.normals), hasTangents: Boolean(baked.tangents) });
  `));
  assert.deepEqual(result.out, [101, 102, 103]);
  assert.equal(result.hasNormals, false);
  assert.equal(result.hasTangents, false);
});

// --- full pipeline at extraction level ----------------------------------------

test("default weights fold onto morphed position/normal/tangent data per node", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [0.5, 0.25], node: [1, 0] });
  const result = plain(call(context, `
    var scene = gltfExtractScene(morphDoc, buffer);
    function bake(vertices) {
      var out = gltfApplyMorphWeights(
        vertices.positions, vertices.normals, vertices.tangents,
        vertices.morphTargets, vertices.morphWeights);
      return {
        positions: Array.from(out.positions),
        normals: Array.from(out.normals),
        tangents: Array.from(out.tangents),
        uvs: Array.from(vertices.uvs),
      };
    }
    ({ meshDefault: bake(scene.objects[0].vertices), nodeOverride: bake(scene.objects[1].vertices) });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  // Mesh-default weights [0.5, 0.25]: both nodes sit under an identity
  // transform, so object-space deltas match primitive-local ones.
  assert.deepEqual(round6(result.meshDefault.positions), [
    1, 0.3125, 0,
    0, 1, 0.3125,
    0.25, 0.0625, 0,
  ]);
  assert.deepEqual(round6(result.meshDefault.normals), [
    0.125, 0, 1.125,
    0.125, 0, 1.125,
    0.125, 0, 1.125,
  ]);
  assert.deepEqual(round6(result.meshDefault.tangents), [
    1, 0.5, 0, 1,
    1, 0.5, 0, 1,
    1, 0.5, 0, 1,
  ]);

  // Node override [1, 0]: only target 0 applies, at full weight.
  assert.deepEqual(round6(result.nodeOverride.positions), [
    1, 0.5, 0,
    0, 1, 0.5,
    0.5, 0, 0,
  ]);
  assert.deepEqual(round6(result.nodeOverride.normals), [
    0, 0, 1.25,
    0, 0, 1.25,
    0, 0, 1.25,
  ]);
  assert.deepEqual(round6(result.nodeOverride.tangents), [
    1, 1, 0, 1,
    1, 1, 0, 1,
    1, 1, 0, 1,
  ]);

  // Both nodes keep their original UV corners: morph handling never touches
  // texture coordinates.
  const expectedUvs = [1, 0, 0, 1, 0, 0];
  assert.deepEqual(result.meshDefault.uvs, expectedUvs);
  assert.deepEqual(result.nodeOverride.uvs, expectedUvs);

  // Index-driven corner order is preserved end to end: the first baked corner
  // descends from source vertex 1, not vertex 0.
  assert.deepEqual(round6(result.meshDefault.positions.slice(0, 3)), [1, 0.3125, 0]);
});
