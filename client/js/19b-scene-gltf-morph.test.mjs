// glTF morph-target tests — static folding inside primitive extraction.
//
// These tests follow the same pattern as 19-scene-gltf.test.mjs: bootstrap
// fragments load into ONE VM context and the tests call the loader functions
// directly. Every fixture is a small JSON glTF whose binary chunk is built
// inside the VM, so typed-array identity matches the loader's realm. No
// network and no GPU involved.
//
// Scope of these tests (static morph-target checkpoint):
//   - primitive.targets POSITION/NORMAL/TANGENT deltas folded into fresh,
//     owned primitive-local streams during gltfExtractMeshPrimitive
//   - index-map application so delta vertex indices[v] feeds corner v
//   - node default weights overriding mesh default weights (resolved before
//     primitive extraction)
//   - additive weighted-delta accumulation across multiple targets
//   - short, long, zero, and non-finite authored weight lists
//   - malformed target accessors and short delta accessors degrade safely
//   - incomplete trailing destination vertices are never folded nor copied
//   - weighted deltas folded BEFORE any node/world transform (Khronos glTF
//     2.0 ordering), proven under a non-uniform node scale where a
//     transform-deltas-separately order produced wrong normals/tangents
//   - morphed position/normal/tangent data with tangent w preserved
//   - UV data untouched by morph handling; inputs never mutated
//   - static morph metadata never leaks onto extracted geometry or objects

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
// the flat per-corner streams exercise index-map application. Two morph
// targets:
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

function morphDoc(meshWeights, nodeTransforms) {
  var nodes = [
    { mesh: 0 },
    { mesh: 0, weights: meshWeights.node },
  ];
  if (nodeTransforms) {
    for (var i = 0; i < nodeTransforms.length && i < nodes.length; i++) {
      Object.assign(nodes[i], nodeTransforms[i] || {});
    }
  }
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
    nodes: nodes,
    scenes: [{ nodes: [0, 1] }],
  };
}

// Builds the binary chunk and document inside the VM and stashes them on the
// sandbox global so several tests can share one extraction.
function loadFixture(context, meshWeights, nodeTransforms) {
  call(context, `
    var floats = ${JSON.stringify(FIXTURE_FLOATS)};
    var indices = [1, 2, 0];
    var buffer = new ArrayBuffer(floats.length * 4 + 8);
    new Float32Array(buffer, 0, floats.length).set(floats);
    new Uint16Array(buffer, floats.length * 4, 3).set(indices);
    var morphDoc = ${JSON.stringify(morphDoc(meshWeights, nodeTransforms))};
    var morphScene = null;
  `);
}

// Extracted flat corners after the [1, 2, 0] index fan-out: corner k carries
// source vertex indices[k].
const FLAT_TARGET0_POSITIONS = [0, 0.5, 0, 0, 0, 0.5, 0.5, 0, 0];

// --- primitive-level fold ------------------------------------------------------

test("indexed morph targets fold into owned primitive-local streams through the index map", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [0.5, 0.25], node: null });
  const result = plain(call(context, `
    var geometry = gltfExtractMeshPrimitive(
      morphDoc, morphDoc.meshes[0].primitives[0], buffer, null, [0.5, 0.25]);
    ({
      count: geometry.count,
      positions: Array.from(geometry.positions),
      normals: Array.from(geometry.normals),
      tangents: Array.from(geometry.tangents),
      uvs: Array.from(geometry.uvs),
      ownedStreams: geometry.positions.buffer !== buffer
        && geometry.normals.buffer !== buffer
        && geometry.tangents.buffer !== buffer,
      noMetadata: !Object.prototype.hasOwnProperty.call(geometry, "morphTargets")
        && !Object.prototype.hasOwnProperty.call(geometry, "morphWeights"),
      sourceUntouched: Array.from(new Float32Array(buffer, 0, floats.length))
        .every(function(v, i) { return v === floats[i]; }),
    });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  assert.equal(result.count, 3);
  // Additive bake with mesh defaults [0.5, 0.25]: base corner + 0.5*target0 +
  // 0.25*target1 per expanded corner. Corners descend from indices [1, 2, 0].
  assert.deepEqual(round6(result.positions), [
    1, 0.3125, 0,
    0, 1, 0.3125,
    0.25, 0.0625, 0,
  ]);
  // Primitive extraction deliberately exposes additive primitive-local
  // normal sums before later world-transform normalization: raw fold is
  // base + 0.5*target0 + 0.25*target1 = [0.125, 0, 1.125].
  assert.deepEqual(round6(result.normals), [
    0.125, 0, 1.125,
    0.125, 0, 1.125,
    0.125, 0, 1.125,
  ]);
  // Tangent xyz shifts by 0.5*(0,1,0); authored w=1 survives untouched.
  assert.deepEqual(round6(result.tangents), [
    1, 0.5, 0, 1,
    1, 0.5, 0, 1,
    1, 0.5, 0, 1,
  ]);
  // UV corners are untouched by morph folding and still fan out via indices.
  assert.deepEqual(result.uvs, [1, 0, 0, 1, 0, 0]);

  // The fold hands back fresh copies, not views over the shared GLB buffer,
  // and reading targets never mutates the binary payload itself.
  assert.equal(result.ownedStreams, true);
  assert.equal(result.noMetadata, true);
  assert.equal(result.sourceUntouched, true);
});

test("unindexed primitives fold owned target copies without an index map", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [1, 0], node: null });
  const result = plain(call(context, `
    var unindexed = Object.assign({}, morphDoc.meshes[0].primitives[0], { indices: undefined });
    var geometry = gltfExtractMeshPrimitive(morphDoc, unindexed, buffer, null, [1, 0]);
    ({
      positions: Array.from(geometry.positions),
      normals: Array.from(geometry.normals),
      owned: geometry.positions.buffer !== buffer,
    });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  // Vertex v pairs directly with target vertex v at weight 1:
  // base + flat target0 = [0.5,0,0], [1,0.5,0], [0,1,0.5].
  assert.deepEqual(round6(result.positions), [
    0.5, 0, 0,
    1, 0.5, 0,
    0, 1, 0.5,
  ]);
  // Base normal (0,0,1) plus target0's (0,0,0.25) per vertex.
  assert.deepEqual(round6(result.normals), [
    0, 0, 1.25,
    0, 0, 1.25,
    0, 0, 1.25,
  ]);
  // Even on the unindexed path the folded stream detaches from the GLB view.
  assert.equal(result.owned, true);
});

// --- node-over-mesh default weights -------------------------------------------

test("node weights override mesh defaults on baked object vertices", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [0.5, 0.25], node: [1, 0] });
  const result = plain(call(context, `
    var scene = gltfExtractScene(morphDoc, buffer);
    ({
      objectCount: scene.objects.length,
      meshDefaultPositions: Array.from(scene.objects[0].vertices.positions),
      nodeOverridePositions: Array.from(scene.objects[1].vertices.positions),
      leakedMetadata: scene.objects.some(function(o) {
        return Object.prototype.hasOwnProperty.call(o.vertices, "morphTargets")
          || Object.prototype.hasOwnProperty.call(o.vertices, "morphWeights");
      }),
    });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  assert.equal(result.objectCount, 2);
  // Both nodes sit under an identity transform here, so baked object-space
  // positions equal primitive-local base + weighted deltas exactly.
  // A plain node keeps the mesh's own default weights [0.5, 0.25].
  assert.deepEqual(round6(result.meshDefaultPositions), [
    1, 0.3125, 0,
    0, 1, 0.3125,
    0.25, 0.0625, 0,
  ]);
  // A node instantiating the mesh overrides them wholesale with [1, 0]
  // (glTF 2.0).
  assert.deepEqual(round6(result.nodeOverridePositions), [
    1, 0.5, 0,
    0, 1, 0.5,
    0.5, 0, 0,
  ]);
  // Static morph metadata is fully consumed during extraction: nothing rides
  // on the extracted objects' vertices.
  assert.equal(result.leakedMetadata, false);

  // Short authored lists leave trailing targets at zero rather than leaking;
  // extra node entries are truncated to the target count.
  const partial = plain((() => {
    const { context: ctx } = createLoaderContext();
    loadFixture(ctx, { mesh: [0.75], node: [0.5, 0.5, 0.125] });
    return JSON.parse(JSON.stringify(call(ctx, `
      var scene = gltfExtractScene(morphDoc, buffer);
      ({
        paddedMesh: Array.from(scene.objects[0].vertices.positions),
        truncatedNode: Array.from(scene.objects[1].vertices.positions),
      });
    `)));
  })());
  const t0Flat = FLAT_TARGET0_POSITIONS;
  assert.deepEqual(round6(partial.paddedMesh), [
    1 + 0.75 * t0Flat[0], 0 + 0.75 * t0Flat[1], 0 + 0.75 * t0Flat[2],
    0 + 0.75 * t0Flat[3], 1 + 0.75 * t0Flat[4], 0 + 0.75 * t0Flat[5],
    0 + 0.75 * t0Flat[6], 0 + 0.75 * t0Flat[7], 0 + 0.75 * t0Flat[8],
  ]);
  // Target 1 flat corner positions: c0=(0,0.25,0), c1=(0,0,0.25), c2=(0,0.25,0).
  assert.deepEqual(round6(partial.truncatedNode), [
    1 + 0.5 * t0Flat[0] + 0.5 * 0,     0 + 0.5 * t0Flat[1] + 0.5 * 0.25, 0 + 0.5 * t0Flat[2] + 0.5 * 0,
    0 + 0.5 * t0Flat[3] + 0.5 * 0,     1 + 0.5 * t0Flat[4] + 0.5 * 0,    0 + 0.5 * t0Flat[5] + 0.5 * 0.25,
    0 + 0.5 * t0Flat[6] + 0.5 * 0,     0 + 0.5 * t0Flat[7] + 0.5 * 0.25, 0 + 0.5 * t0Flat[8] + 0.5 * 0,
  ]);
});

test("zero and non-finite weights are skipped without touching their channels", () => {
  // Zero-weight target 1 ([1, 0]) applies only target 0 — already exercised by
  // the node-override case above. Here both entries are non-finite: nothing
  // applies, and the pure base surface survives untouched.
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [], node: null });
  const result = plain(call(context, `
    morphDoc.meshes[0].weights = [Infinity, NaN];
    var scene = gltfExtractScene(morphDoc, buffer);
    ({
      positions: Array.from(scene.objects[0].vertices.positions),
      normals: Array.from(scene.objects[0].vertices.normals),
      uvs: Array.from(scene.objects[0].vertices.uvs),
    });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  assert.deepEqual(round6(result.positions), [
    1, 0, 0,
    0, 1, 0,
    0, 0, 0,
  ]);
  assert.deepEqual(round6(result.normals), [
    0, 0, 1,
    0, 0, 1,
    0, 0, 1,
  ]);
  // Corner order still descends from indices [1, 2, 0].
  assert.deepEqual(result.uvs, [1, 0, 0, 1, 0, 0]);
});

test("a finite entry next to a non-finite one applies positionally", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [], node: null });
  const result = plain(call(context, `
    // Target 0 is rejected as non-finite; target 1 applies at weight 1.
    morphDoc.meshes[0].weights = [NaN, 1];
    var scene = gltfExtractScene(morphDoc, buffer);
    ({ positions: Array.from(scene.objects[0].vertices.positions) });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  // base + flat target1: c0=(0,0.25,0), c1=(0,0,0.25), c2=(0,0.25,0).
  assert.deepEqual(round6(result.positions), [
    1, 0.25, 0,
    0, 1, 0.25,
    0, 0.25, 0,
  ]);
});

// --- full pipeline at extraction level ----------------------------------------

test("default weights fold onto morphed position/normal/tangent data per node", () => {
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [0.5, 0.25], node: [1, 0] });
  // Morph targets and weights are consumed during extraction: the object
  // vertices arrive already baked with multiple targets accumulating
  // additively per node.
  const result = plain(call(context, `
    var scene = gltfExtractScene(morphDoc, buffer);
    function read(vertices) {
      return {
        positions: Array.from(vertices.positions),
        normals: Array.from(vertices.normals),
        tangents: Array.from(vertices.tangents),
        uvs: Array.from(vertices.uvs),
        leakedMetadata: Object.prototype.hasOwnProperty.call(vertices, "morphTargets")
          || Object.prototype.hasOwnProperty.call(vertices, "morphWeights"),
      };
    }
    ({ meshDefault: read(scene.objects[0].vertices), nodeOverride: read(scene.objects[1].vertices) });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  // Mesh-default weights [0.5, 0.25]: both nodes sit under an identity
  // transform, so object-space data equals the primitive-local bake.
  assert.deepEqual(round6(result.meshDefault.positions), [
    1, 0.3125, 0,
    0, 1, 0.3125,
    0.25, 0.0625, 0,
  ]);
  // Baked normals are normalized after folding: raw sum is [0.125, 0, 1.125].
  const nLen = Math.sqrt(0.125 ** 2 + 1.125 ** 2);
  assert.deepEqual(round6(result.meshDefault.normals), [
    round6([0.125 / nLen])[0], 0, round6([1.125 / nLen])[0],
    round6([0.125 / nLen])[0], 0, round6([1.125 / nLen])[0],
    round6([0.125 / nLen])[0], 0, round6([1.125 / nLen])[0],
  ]);
  // Tangent xyz follows the linear transform and is then independently
  // Gram-Schmidt orthogonalized against the inverse-transpose normal.
  const normal = [1 / Math.sqrt(82), 0, 9 / Math.sqrt(82)];
  const dot = normal[0];
  const tangent = [1 - normal[0] * dot, 0.5, -normal[2] * dot];
  const tLen = Math.hypot(...tangent);
  assert.deepEqual(round6(result.meshDefault.tangents), [
    ...round6(tangent.map((v) => v / tLen)), 1,
    ...round6(tangent.map((v) => v / tLen)), 1,
    ...round6(tangent.map((v) => v / tLen)), 1,
  ]);

  // Node override [1, 0]: only target 0 applies, at full weight.
  assert.deepEqual(round6(result.nodeOverride.positions), [
    1, 0.5, 0,
    0, 1, 0.5,
    0.5, 0, 0,
  ]);
  // Raw baked normal [0, 0, 1.25] normalizes back to +Z.
  assert.deepEqual(round6(result.nodeOverride.normals), [
    0, 0, 1,
    0, 0, 1,
    0, 0, 1,
  ]);
  // Raw baked tangent xyz [1, 1, 0] normalizes; w stays 1.
  assert.deepEqual(round6(result.nodeOverride.tangents), [
    round6([Math.SQRT1_2])[0], round6([Math.SQRT1_2])[0], 0, 1,
    round6([Math.SQRT1_2])[0], round6([Math.SQRT1_2])[0], 0, 1,
    round6([Math.SQRT1_2])[0], round6([Math.SQRT1_2])[0], 0, 1,
  ]);

  // Both nodes keep their original UV corners: morph handling never touches
  // texture coordinates.
  const expectedUvs = [1, 0, 0, 1, 0, 0];
  assert.deepEqual(result.meshDefault.uvs, expectedUvs);
  assert.deepEqual(result.nodeOverride.uvs, expectedUvs);

  // Index-driven corner order is preserved end to end: the first baked corner
  // descends from source vertex 1, not vertex 0.
  assert.deepEqual(round6(result.meshDefault.positions.slice(0, 3)), [1, 0.3125, 0]);

  // Static morph metadata is consumed during extraction: nothing rides on
  // either node's vertices.
  assert.equal(result.meshDefault.leakedMetadata, false);
  assert.equal(result.nodeOverride.leakedMetadata, false);
});

// --- pre-transform bake under a non-uniform node scale ------------------------

test("non-uniform node scale transforms primitive-local baked morph data", () => {
  const { context } = createLoaderContext();
  // Scale only the mesh-default node; extraction runs exactly once through
  // gltfExtractScene, so any correct result must come from morphing BEFORE
  // the world transform (Khronos ordering).
  loadFixture(context, { mesh: [0.5, 0.25], node: [1, 0] }, [{ scale: [2, 3, 4] }]);
  const result = plain(call(context, `
    var scene = gltfExtractScene(morphDoc, buffer);
    var vertices = scene.objects[0].vertices;
    ({
      positions: Array.from(vertices.positions),
      normals: Array.from(vertices.normals),
      tangents: Array.from(vertices.tangents),
      leakedMetadata: Object.prototype.hasOwnProperty.call(vertices, "morphTargets")
        || Object.prototype.hasOwnProperty.call(vertices, "morphWeights"),
    });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  // Primitive-local baked positions (identity case above) scaled by [2,3,4]:
  // [1,0.3125,0] -> [2,0.9375,0]; [0,1,0.3125] -> [0,3,1.25];
  // [0.25,0.0625,0] -> [0.5,0.1875,0]. Morphing happened before scaling.
  assert.deepEqual(round6(result.positions), [
    2, 0.9375, 0,
    0, 3, 1.25,
    0.5, 0.1875, 0,
  ]);

  // Normals go through the inverse-transpose of the diagonal scale, i.e.
  // component-wise division, then renormalize: normalize([0.125/2, 0/3,
  // 1.125/4]). Transforming deltas separately would fold [0.125, 0, 1.125]
  // AFTER normalization or skip the inverse-transpose and produce a different
  // direction here.
  const nl = Math.sqrt((0.125 / 2) ** 2 + (1.125 / 4) ** 2);
  assert.deepEqual(round6(result.normals), [
    round6([(0.125 / 2) / nl])[0], 0, round6([(1.125 / 4) / nl])[0],
    round6([(0.125 / 2) / nl])[0], 0, round6([(1.125 / 4) / nl])[0],
    round6([(0.125 / 2) / nl])[0], 0, round6([(1.125 / 4) / nl])[0],
  ]);

  // Tangents take the forward linear transform, then Gram-Schmidt against
  // the exact normal. This independent oracle would fail if the old plain
  // mat3-normal path or a missing orthogonalization were restored.
  const n = [2 / Math.sqrt(85), 0, 9 / Math.sqrt(85)];
  const rawT = [2, 1.5, 0];
  const ndot = n[0] * rawT[0];
  const orthoT = [rawT[0] - n[0] * ndot, rawT[1], -n[2] * ndot];
  const orthoLen = Math.hypot(...orthoT);
  const expectedT = round6(orthoT.map((v) => v / orthoLen));
  assert.deepEqual(round6(result.tangents), [
    ...expectedT, 1,
    ...expectedT, 1,
    ...expectedT, 1,
  ]);

  // The bake payload was consumed during extraction even under a transform.
  assert.equal(result.leakedMetadata, false);
});

// --- malformed accessors and length bounds ------------------------------------

test("malformed target accessors and short delta lengths stay safe", () => {
  // Target 0's channels are malformed (skipped below); target 1 carries the
  // short POSITION accessor that must degrade safely.
  const { context } = createLoaderContext();
  loadFixture(context, { mesh: [0, 1], node: null });
  const result = plain(call(context, `
    // Target 0 names an accessor that does not exist: its channels are
    // skipped instead of poisoning the streams. Target 1's POSITION points
    // at a deliberately short two-vertex accessor; its NORMAL stays valid.
    morphDoc.meshes[0].primitives[0].targets[0] = { POSITION: 99, NORMAL: 99, TANGENT: 99 };
    morphDoc.accessors.push({
      bufferView: 0, byteOffset: 144, componentType: 5126, count: 2, type: "VEC3",
    });
    morphDoc.meshes[0].primitives[0].targets[1].POSITION = 10;
    var scene = gltfExtractScene(morphDoc, buffer);
    var positions = Array.from(scene.objects[0].vertices.positions);
    var normals = Array.from(scene.objects[0].vertices.normals);
    ({
      positions: positions,
      allFinite: positions.every(function(v) { return isFinite(v); })
        && normals.every(function(v) { return isFinite(v); }),
    });
  `));
  const round6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);

  // Accessor 10 holds two vertices over TARGET0_POSITIONS bytes: v0=(0.5,0,0),
  // v1=(0,0.5,0). With indices [1,2,0]: corner 0 reads d=1 (+0,0.5,0),
  // corner 1 reads d=2 which is past the short accessor and is LEFT UNTOUCHED,
  // corner 2 reads d=0 (+0.5,0,0).
  assert.equal(result.allFinite, true);
  assert.deepEqual(round6(result.positions), [
    1, 0.5, 0,
    0, 1, 0,
    0.5, 0, 0,
  ]);
});

// --- incomplete destination vertices ------------------------------------------

// dstVertices is floor(values.length / stride), so a trailing base fragment
// narrower than one full three-float vertex is never folded. With zero
// complete vertices the fold cannot write a single corner: no stream copy is
// allocated and the shared GLB view is handed back by identity. With one
// complete vertex plus a one-float fragment, only that vertex folds and the
// fragment float survives into the owned copy.
test("incomplete trailing destination vertices stay untouched and uncopied", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `
    var gltf = {
      accessors: [{ bufferView: 0, byteOffset: 0, componentType: 5126, count: 2, type: "VEC3" }],
      bufferViews: [{ byteOffset: 0 }],
    };
    var primitive = { targets: [{ POSITION: 0 }] };
    var deltas = new Float32Array([10, 20, 30, 40, 50, 60]);
    var fragmentOnly = new Float32Array([1, 2]);
    var oneVertexPlusFragment = new Float32Array([1, 2, 3, 4]);
    var fragmentStreams = gltfFoldMorphTargets(
      gltf, primitive, deltas.buffer, null, fragmentOnly, null, null, [1]);
    var foldedStreams = gltfFoldMorphTargets(
      gltf, primitive, deltas.buffer, null, oneVertexPlusFragment, null, null, [1]);
    ({
      fragmentSameObject: fragmentStreams[0] === fragmentOnly,
      fragment: Array.from(fragmentStreams[0]),
      fragmentInputUntouched: Array.from(fragmentOnly),
      foldedSameObject: foldedStreams[0] === oneVertexPlusFragment,
      folded: Array.from(foldedStreams[0]),
      foldedInputUntouched: Array.from(oneVertexPlusFragment),
    });
  `));
  assert.equal(result.fragmentSameObject, true, "no complete destination vertex means no copy");
  assert.deepEqual(result.fragment, [1, 2], "a two-float fragment cannot hold a stride, so nothing folds");
  assert.deepEqual(result.fragmentInputUntouched, [1, 2], "the shared GLB bytes stay untouched");
  assert.equal(result.foldedSameObject, false, "one writable corner detaches the stream from the shared view");
  assert.deepEqual(result.folded, [11, 22, 33, 4], "only the first complete vertex folds; the fragment float survives");
  assert.deepEqual(result.foldedInputUntouched, [1, 2, 3, 4], "the original shared view stays untouched");
});

// --- fallback normals describe the morphed surface ---------------------------
//
// Regression guard: with no authored NORMAL the loader generates flat
// fallback normals, and it used to generate them from the BASE positions
// before gltfFoldMorphTargets ran. A POSITION-only morph target therefore
// deformed the triangle while every corner kept the undeformed face
// normal, and computed tangents inherited that stale basis. The fix
// generates the fallback after the fold, from the final primitive-local
// positions.
//
// The expected normals below are derived independently from the expected
// deformed corners with plain cross-product/normalize math in this test
// realm; the production gltfGenerateFlatNormals helper is never called.

// Loads the shared fixture, then strips the authored NORMAL/TANGENT
// attribute references and keeps POSITION-only targets, so extraction
// must fall back to generated normals.
function loadPositionOnlyMorphFixture(context, weights) {
  loadFixture(context, { mesh: weights, node: null });
  call(context, `
    delete morphDoc.meshes[0].primitives[0].attributes.NORMAL;
    delete morphDoc.meshes[0].primitives[0].attributes.TANGENT;
    morphDoc.meshes[0].primitives[0].targets = [{ POSITION: 5 }, { POSITION: 8 }];
  `);
}

// Independent face normal of the triangle (a, b, c), computed here rather
// than through any production helper.
function faceNormalFromCorners(a, b, c) {
  const e1 = [b[0] - a[0], b[1] - a[1], b[2] - a[2]];
  const e2 = [c[0] - a[0], c[1] - a[1], c[2] - a[2]];
  const n = [
    e1[1] * e2[2] - e1[2] * e2[1],
    e1[2] * e2[0] - e1[0] * e2[2],
    e1[0] * e2[1] - e1[1] * e2[0],
  ];
  const len = Math.sqrt(n[0] * n[0] + n[1] * n[1] + n[2] * n[2]);
  return [n[0] / len, n[1] / len, n[2] / len];
}

const morphRound6 = (arr) => arr.map((v) => Math.round(v * 1e6) / 1e6);
// Face normal of the undeformed fixture triangle.
const UNDEFORMED_NORMAL = [0, 0, 1];

// --- no base NORMAL ignores authored and morph tangents (glTF 2.0) ----------
//
// The spec requires calculated flat normals when NORMAL is absent and says
// authored tangents and morph TANGENT displacement must be ignored. This
// fixture restores TANGENT streams (base VEC4 and morph VEC3 delta) on the
// POSITION-only fixture, pointing at a sentinel direction that cannot
// describe the triangle, and proves extraction computes its tangent basis
// from the deformed geometry instead of forwarding the sentinels.

// Reuses loadPositionOnlyMorphFixture (which strips NORMAL and TANGENT),
// then re-adds deliberately incompatible sentinel TANGENT streams.
function loadNoNormalWithTangentSentinels(context, weights) {
  loadPositionOnlyMorphFixture(context, weights);
  call(context, `
    morphDoc.meshes[0].primitives[0].attributes.TANGENT = 2;
    morphDoc.meshes[0].primitives[0].targets[0].TANGENT = 7;
    // Base VEC4 TANGENT lives at byte 72; morph VEC3 delta at byte 216.
    new Float32Array(buffer, 72, 12).set(
      [0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1]);
    new Float32Array(buffer, 216, 9).set(
      [0, 0, -1, 0, 0, -1, 0, 0, -1]);
  `);
}

test("no authored NORMAL: authored and morph tangents are ignored, basis is computed", () => {
  const cases = [
    { name: "indexed", unindexed: false,
      corners: [[1, 0.5, 0], [0, 1, 0.5], [0.5, 0, 0]] },
    { name: "unindexed", unindexed: true,
      corners: [[0.5, 0, 0], [1, 0.5, 0], [0, 1, 0.5]] },
  ];
  for (const { name, unindexed, corners } of cases) {
    const { context } = createLoaderContext();
    loadNoNormalWithTangentSentinels(context, [1, 0]);
    if (unindexed) {
      call(context, "delete morphDoc.meshes[0].primitives[0].indices;");
    }
    const result = plain(call(context, `
      var geometry = gltfExtractMeshPrimitive(
        morphDoc, morphDoc.meshes[0].primitives[0], buffer, null, [1, 0]);
      ({
        positions: Array.from(geometry.positions),
        normals: Array.from(geometry.normals),
        tangents: Array.from(geometry.tangents),
        sourceTangents: Array.from(new Float32Array(buffer, 72, 12)),
      });
    `));

    const expectedNormal = faceNormalFromCorners(...corners);
    for (let k = 0; k < 3; k++) {
      assert.deepStrictEqual(
        morphRound6(result.positions.slice(k * 3, k * 3 + 3)),
        morphRound6(corners[k]), `${name}: deformed corner`);
      const n = result.normals.slice(k * 3, k * 3 + 3);
      assert.deepStrictEqual(morphRound6(n), morphRound6(expectedNormal),
        `${name}: flat normal from deformed surface`);
      const t = result.tangents.slice(k * 4, k * 4 + 4);
      assert.strictEqual(t[3], 1, `${name}: tangent handedness`);
      const len = Math.hypot(t[0], t[1], t[2]);
      assert.ok(Math.abs(len - 1) < 1e-6, `${name}: tangent is unit`);
      const dot = t[0] * n[0] + t[1] * n[1] + t[2] * n[2];
      assert.ok(Math.abs(dot) < 1e-6, `${name}: tangent orthogonal to normal`);
      // The [0,0,1] sentinel direction must not leak into the output.
      assert.ok(
        Math.abs(t[0]) + Math.abs(t[1]) + Math.abs(t[2] - 1) > 0.5,
        `${name}: sentinel tangent leaked`);
    }
    // The shared GLB buffer's authored TANGENT stream was never rewritten.
    assert.deepStrictEqual(
      morphRound6(result.sourceTangents),
      [0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1],
      `${name}: source tangent stream unchanged`);
  }
});

test("indexed POSITION-only morphs regenerate fallback normals from the deformed surface", () => {
  const { context } = createLoaderContext();
  loadPositionOnlyMorphFixture(context, [1, 0]);
  const result = plain(call(context, `
    var geometry = gltfExtractMeshPrimitive(
      morphDoc, morphDoc.meshes[0].primitives[0], buffer, null, [1, 0]);
    ({
      positions: Array.from(geometry.positions),
      normals: Array.from(geometry.normals),
      noMetadata: !Object.prototype.hasOwnProperty.call(geometry, "morphTargets")
        && !Object.prototype.hasOwnProperty.call(geometry, "morphWeights"),
      sourceUntouched: Array.from(new Float32Array(buffer, 0, floats.length))
        .every(function(v, i) { return v === floats[i]; }),
    });
  `));

  // Weight [1, 0] folds only target 0's POSITION deltas. Base vertices
  // (0,0,0), (1,0,0), (0,1,0) become (0.5,0,0), (1,0.5,0), (0,1,0.5), and
  // the [1, 2, 0] index fan-out orders the corners v1, v2, v0.
  const deformedCorners = [
    [1, 0.5, 0],
    [0, 1, 0.5],
    [0.5, 0, 0],
  ];
  const expectedNormal = faceNormalFromCorners(
    deformedCorners[0], deformedCorners[1], deformedCorners[2]);

  assert.deepEqual(morphRound6(result.positions), morphRound6(deformedCorners.flat()));
  assert.equal(result.noMetadata, true);
  assert.equal(result.sourceUntouched, true);

  // Every corner carries the deformed face normal, which differs from the
  // undeformed (0, 0, 1) the pre-fold fallback used to hand back.
  for (let v = 0; v < 3; v++) {
    const corner = morphRound6(result.normals.slice(v * 3, v * 3 + 3));
    assert.deepEqual(corner, morphRound6(expectedNormal), `corner ${v} matches the deformed face normal`);
    assert.notDeepEqual(corner, UNDEFORMED_NORMAL, `corner ${v} differs from the undeformed normal`);
  }
});

test("unindexed POSITION-only morphs regenerate fallback normals from the deformed surface", () => {
  const { context } = createLoaderContext();
  loadPositionOnlyMorphFixture(context, [1, 0]);
  const result = plain(call(context, `
    var unindexed = Object.assign({}, morphDoc.meshes[0].primitives[0], { indices: undefined });
    var geometry = gltfExtractMeshPrimitive(morphDoc, unindexed, buffer, null, [1, 0]);
    ({
      positions: Array.from(geometry.positions),
      normals: Array.from(geometry.normals),
    });
  `));

  // Vertex v pairs directly with target vertex v: (0,0,0), (1,0,0), (0,1,0)
  // become (0.5,0,0), (1,0.5,0), (0,1,0.5). This is the same deformed
  // triangle as the indexed case up to a cyclic corner shift, so the
  // independent cross product lands on the same face normal.
  const deformedCorners = [
    [0.5, 0, 0],
    [1, 0.5, 0],
    [0, 1, 0.5],
  ];
  const expectedNormal = faceNormalFromCorners(
    deformedCorners[0], deformedCorners[1], deformedCorners[2]);

  assert.deepEqual(morphRound6(result.positions), morphRound6(deformedCorners.flat()));

  for (let v = 0; v < 3; v++) {
    const corner = morphRound6(result.normals.slice(v * 3, v * 3 + 3));
    assert.deepEqual(corner, morphRound6(expectedNormal), `corner ${v} matches the deformed face normal`);
    assert.notDeepEqual(corner, UNDEFORMED_NORMAL, `corner ${v} differs from the undeformed normal`);
  }
});

test("zero-weight POSITION-only morphs keep the base fallback normals and the shared source", () => {
  const { context } = createLoaderContext();
  loadPositionOnlyMorphFixture(context, [0, 0]);
  const result = plain(call(context, `
    var geometry = gltfExtractMeshPrimitive(
      morphDoc, morphDoc.meshes[0].primitives[0], buffer, null, [0, 0]);
    ({
      positions: Array.from(geometry.positions),
      normals: Array.from(geometry.normals),
      ownedStreams: geometry.positions.buffer !== buffer
        && geometry.normals.buffer !== buffer,
      sourceUntouched: Array.from(new Float32Array(buffer, 0, floats.length))
        .every(function(v, i) { return v === floats[i]; }),
    });
  `));

  // No effective fold: the expanded base corners (1,0,0), (0,1,0), (0,0,0)
  // keep the undeformed face normal on every corner, the fallback streams
  // are owned copies, and the shared GLB bytes are never written.
  const baseNormal = faceNormalFromCorners([1, 0, 0], [0, 1, 0], [0, 0, 0]);
  assert.deepEqual(morphRound6(baseNormal), [0, 0, 1]);
  assert.deepEqual(morphRound6(result.positions), [1, 0, 0, 0, 1, 0, 0, 0, 0]);
  assert.deepEqual(morphRound6(result.normals), [0, 0, 1, 0, 0, 1, 0, 0, 1]);
  assert.equal(result.ownedStreams, true);
  assert.equal(result.sourceUntouched, true);
});

test("reflected shear reverses complete triangles and preserves the tangent frame", () => {
  const { context } = createLoaderContext();
  const result = plain(call(context, `(() => {
    const positions = new Float32Array([0,0,0, 0,1,0, -1,0,1]);
    const normals = new Float32Array([1,0,1, 1,0,1, 1,0,1]);
    const tangents = new Float32Array([0,1,0,1, 0,1,0,1, 0,1,0,1]);
    const uvs = new Float32Array([0,0, 1,0, 0,1]);
    // Rows [-2,1,0; 0,3,0; 0,0,1]: non-orthogonal columns and det -6.
    const matrix = new Float64Array([-2,0,0,0, 1,3,0,0, 0,0,1,0, 0,0,0,1]);
    const out = gltfTransformMorphedStreams({positions,normals,tangents,uvs}, matrix);
    return {positions:Array.from(out.positions), normals:Array.from(out.normals), tangents:Array.from(out.tangents), uvs:Array.from(out.uvs)};
  })()`));
  assert.deepEqual(result.positions, [0, 0, 0, 2, 0, 1, 1, 3, 0]);
  assert.deepEqual(result.uvs, [0, 0, 0, 1, 1, 0]);
  const n = [-3 / Math.sqrt(46), 1 / Math.sqrt(46), 6 / Math.sqrt(46)];
  const t = [1 / Math.sqrt(10), 3 / Math.sqrt(10), 0];
  for (let vertex = 0; vertex < 3; vertex++) {
    assert.deepEqual(morphRound6(result.normals.slice(vertex * 3, vertex * 3 + 3)), morphRound6(n));
    assert.deepEqual(morphRound6(result.tangents.slice(vertex * 4, vertex * 4 + 3)), morphRound6(t));
    assert.equal(result.tangents[vertex * 4 + 3], -1);
  }
  const edge1 = result.positions.slice(3, 6);
  const edge2 = result.positions.slice(6, 9);
  const face = [
    edge1[1] * edge2[2] - edge1[2] * edge2[1],
    edge1[2] * edge2[0] - edge1[0] * edge2[2],
    edge1[0] * edge2[1] - edge1[1] * edge2[0],
  ];
  assert.ok(face[0] * n[0] + face[1] * n[1] + face[2] * n[2] > 0, "reflected face stays front-facing");
});
