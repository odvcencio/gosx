// Regression: glTF TRIANGLE_STRIP (mode 5) and TRIANGLE_FAN (mode 6)
// primitives disappear because extraction only handled mode 4.
//
// Fixture strategy: load the real math + gltf runtime into one VM context,
// bind actual fixture docs/buffers, and call gltfExtractScene exactly once
// per extraction. The per-channel corner map is additionally asserted
// through one direct gltfExtractMeshPrimitive call, because scene objects
// deliberately omit joints/weights unless a skin is bound. Expected values
// are written as explicit corner indices / corner positions straight from
// the triangulation contract — never derived by re-implementing the
// production algorithm:
//   STRIP: window i over source corner list s emits (s[i], s[i+1], s[i+2])
//     on even i and (s[i+2], s[i+1], s[i]) on odd i. That is the canonical
//     legal order: source [0,1,2,3,4] -> corners [0,1,2, 3,2,1, 2,3,4].
//   FAN:   window i emits (s[0], s[i+1], s[i+2]) anchored on the first
//     source corner: [0,1,2, 0,2,3].
// Degenerate strip windows are RETAINED (never dropped; parity follows the
// window position, so [0,1,2,1,3] -> [0,1,2, 1,2,1, 2,1,3]). Uint32 indices
// must survive untruncated, and every stream (positions, normals, uvs,
// tangents, joints, weights, static + animated morph targets) must follow
// the same corner-to-source-vertex map.

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
  const sandbox = {
    console: { warn: () => {}, error: () => {}, log: () => {} },
    Math, JSON, Number, Object, Array, String, Boolean, isFinite,
    Float32Array, Uint8Array, Uint16Array, Uint32Array, Int8Array, Int16Array,
    ArrayBuffer, DataView, TextDecoder, Error,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  const context = vm.createContext(sandbox);
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readSource("../runtime/scene3d/gltf.ts"), context, { filename: "gltf.ts" });
  return context;
}

// --- canonical triangulation contracts --------------------------------------

// Unindexed strip over source vertices [0,1,2,3,4]: even window
// (s[i], s[i+1], s[i+2]); odd window (s[i+2], s[i+1], s[i]).
const STRIP_5_CORNERS = [0, 1, 2, 3, 2, 1, 2, 3, 4];
// Indexed strip with index list [0,1,2,3].
const STRIP_4_CORNERS = [0, 1, 2, 3, 2, 1];
// Indexed strip [0,1,2,1,3]: the middle window (1,2,1) is degenerate but
// RETAINED; parity follows the window position, so window 2 stays even.
const DEGENERATE_STRIP_CORNERS = [0, 1, 2, 1, 2, 1, 2, 1, 3];
// Uint32 strip [70000,70001,70002,70003].
const STRIP_UINT32_CORNERS = [70000, 70001, 70002, 70003, 70002, 70001];
// Fans anchor every window on the first source corner.
const FAN_4_CORNERS = [0, 1, 2, 0, 2, 3];
const FAN_INDEXED_CORNERS = [0, 2, 3, 0, 3, 1];

// glTF accessor element widths and component layouts.
const TYPE_WIDTH = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4 };
const COMPONENT_BPE = { 5121: 1, 5123: 2, 5125: 4, 5126: 4 };
const COMPONENT_CTORS = {
  5121: Uint8Array,
  5123: Uint16Array,
  5125: Uint32Array,
  5126: Float32Array,
};

// Binary builder: each entry becomes one bufferView + accessor.
// accessor.count counts ELEMENTS (data.length / type width) while
// bufferView.byteLength stays based on the raw scalar component count —
// mixing the two up makes accessor reads run past the bufferView.
function makeBinary(entries) {
  let offset = 0;
  const metas = [];
  for (const e of entries) {
    const bpe = COMPONENT_BPE[e.ctype];
    const width = TYPE_WIDTH[e.type] || 1;
    const pad = (4 - (offset % 4)) % 4;
    offset += pad;
    metas.push({
      offset,
      bpe,
      width,
      scalarCount: e.data.length,
      elementCount: e.data.length / width,
    });
    offset += e.data.length * bpe;
  }
  const buffer = new ArrayBuffer(Math.max(offset, 4));
  const accessors = [];
  const bufferViews = [];
  entries.forEach((e, i) => {
    const m = metas[i];
    new COMPONENT_CTORS[e.ctype](buffer, m.offset, m.scalarCount).set(e.data);
    bufferViews.push({ buffer: 0, byteOffset: m.offset, byteLength: m.scalarCount * m.bpe });
    const accessor = { bufferView: i, componentType: e.ctype, count: m.elementCount, type: e.type };
    if (m.scalarCount > 0) {
      // glTF 2.0 requires valid min/max on POSITION; supplying accurate
      // component-wise bounds on every stream keeps every fixture valid.
      const min = [];
      const max = [];
      for (let k = 0; k < m.width; k++) {
        let lo = Infinity;
        let hi = -Infinity;
        for (let j = k; j < e.data.length; j += m.width) {
          if (e.data[j] < lo) lo = e.data[j];
          if (e.data[j] > hi) hi = e.data[j];
        }
        min.push(lo);
        max.push(hi);
      }
      accessor.min = min;
      accessor.max = max;
    }
    accessors.push(accessor);
  });
  return { buffer, bufferViews, accessors, buffers: [{ byteLength: buffer.byteLength }] };
}

// Spec-strip vertex layout: non-collinear zig-zag so every window is a real
// triangle and flat-normal orientation is decidable.
const STRIP_POS = [0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0, 0, 2, 0];
const FAN_POS = [0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0];

// spec: { mode, positions, indices:{data,type}, uvs, normals, tangents,
//         joints, weights, deltas, meshWeights, animateWeights }
function buildFixture(spec) {
  const entries = [{ ctype: 5126, type: "VEC3", data: spec.positions }];
  let next = 1;
  const add = (data, ctype, type) => { entries.push({ ctype, type, data }); return next++; };
  const posAcc = 0;
  const idxAcc = spec.indices ? add(spec.indices.data, spec.indices.type, "SCALAR") : null;
  const uvAcc = spec.uvs ? add(spec.uvs, 5126, "VEC2") : null;
  const normalAcc = spec.normals ? add(spec.normals, 5126, "VEC3") : null;
  const tangentAcc = spec.tangents ? add(spec.tangents, 5126, "VEC4") : null;
  // JOINTS_0 carries joint indices: glTF 2.0 requires an unsigned integer
  // component type (UNSIGNED_SHORT here) — never FLOAT.
  const jointAcc = spec.joints ? add(spec.joints, 5123, "VEC4") : null;
  const weightAcc = spec.weights ? add(spec.weights, 5126, "VEC4") : null;
  const deltaAcc = spec.deltas ? add(spec.deltas, 5126, "VEC3") : null;
  let animTimeAcc = null;
  let animValAcc = null;
  if (spec.animateWeights) {
    animTimeAcc = add(spec.animateWeights[0], 5126, "SCALAR");
    animValAcc = add(spec.animateWeights[1], 5126, "SCALAR");
  }
  const bin = makeBinary(entries);
  const attributes = { POSITION: posAcc };
  if (uvAcc != null) attributes.TEXCOORD_0 = uvAcc;
  if (normalAcc != null) attributes.NORMAL = normalAcc;
  if (tangentAcc != null) attributes.TANGENT = tangentAcc;
  if (jointAcc != null) attributes.JOINTS_0 = jointAcc;
  if (weightAcc != null) attributes.WEIGHTS_0 = weightAcc;
  const prim = { mode: spec.mode, attributes };
  if (idxAcc != null) prim.indices = idxAcc;
  if (deltaAcc != null) prim.targets = [{ POSITION: deltaAcc }];
  const mesh = { name: "m", primitives: [prim] };
  if (spec.meshWeights) mesh.weights = spec.meshWeights;
  const doc = {
    asset: { version: "2.0" },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [mesh],
    accessors: bin.accessors,
    bufferViews: bin.bufferViews,
    buffers: bin.buffers,
  };
  if (animTimeAcc != null) {
    doc.animations = [{
      channels: [{ sampler: 0, target: { node: 0, path: "weights" } }],
      samplers: [{ input: animTimeAcc, output: animValAcc, interpolation: "LINEAR" }],
    }];
  }
  return { doc, binary: bin.buffer };
}

// Line entries publish renderer points (flat xyz numbers, [x,y,z] tuples or
// {x,y,z} records depending on the internal representation); normalize all
// of them to flat coordinates so the assertion is about real positions.
function flattenLinePoints(points) {
  if (!points || typeof points.length !== "number") return null;
  const out = [];
  for (let i = 0; i < points.length; i++) {
    const p = points[i];
    if (typeof p === "number") {
      out.push(p);
    } else if (p && typeof p === "object") {
      if (Array.isArray(p)) out.push(p[0], p[1], p[2]);
      else out.push(p.x, p.y, p.z);
    }
  }
  return out;
}

function extractScene(context, doc, binary) {
  context.gltfDoc = doc;
  context.binaryBuffer = binary;
  const result = vm.runInContext("gltfExtractScene(gltfDoc, binaryBuffer)", context);
  const objects = Array.from(result.objects || [], (o) => {
    if (o.kind === "lines") {
      const coords = flattenLinePoints(o.points);
      return {
        id: o.id,
        kind: "lines",
        count: coords ? coords.length / 3 : 0,
        positions: coords,
      };
    }
    return {
      id: o.id,
      kind: o.kind,
      count: o.vertices ? o.vertices.count : 0,
      positions: o.vertices ? Array.from(o.vertices.positions) : null,
      normals: o.vertices && o.vertices.normals ? Array.from(o.vertices.normals) : null,
      uvs: o.vertices && o.vertices.uvs ? Array.from(o.vertices.uvs) : null,
      tangents: o.vertices && o.vertices.tangents ? Array.from(o.vertices.tangents) : null,
      joints: o.vertices && o.vertices.joints ? Array.from(o.vertices.joints) : null,
      weights: o.vertices && o.vertices.weights ? Array.from(o.vertices.weights) : null,
      morph: o._morphAnim ? {
        vertexCount: o._morphAnim.vertexCount,
        defaults: Array.from(o._morphAnim.defaults),
        basePositions: Array.from(o._morphAnim.basePositions),
        targetPositions: Array.from(o._morphAnim.targetPositions, (t) => (t ? Array.from(t) : null)),
        targetNormals: Array.from(o._morphAnim.targetNormals, (t) => (t ? Array.from(t) : null)),
      } : null,
    };
  });
  const points = Array.from(result.points || [], (p) => ({
    id: p.id,
    count: p.count,
    positions: p.positions ? Array.from(p.positions) : null,
  }));
  return { objects, points };
}

function meshObjects(result) {
  return result.objects.filter((o) => o.kind === "gltf-mesh");
}

function assertCorners(object, sourceVertices, cornerIndices) {
  assert.equal(object.count, cornerIndices.length, "triangle corner count");
  const expected = [];
  for (const c of cornerIndices) expected.push(sourceVertices[c * 3], sourceVertices[c * 3 + 1], sourceVertices[c * 3 + 2]);
  assert.deepEqual(object.positions, expected, "corner positions must follow the canonical triangulation");
}

// Expand one authored per-vertex stream through the corner map.
function expandByCornerMap(authored, width, cornerMap) {
  const out = [];
  for (const s of cornerMap) {
    for (let k = 0; k < width; k++) out.push(authored[s * width + k]);
  }
  return out;
}

function assertFinite(values, label) {
  for (const v of values) assert.ok(Number.isFinite(v), label + " must stay finite");
}

function assertFaceNormals(object, cornerIndices, expected) {
  const normals = object.normals;
  assert.ok(normals, "fallback flat normals must be generated");
  assert.equal(normals.length, cornerIndices.length * 3);
  for (let c = 0; c < cornerIndices.length; c++) {
    for (let k = 0; k < 3; k++) {
      const i = c * 3 + k;
      assert.ok(Math.abs(normals[i] - expected[k]) < 1e-6,
        "normal of corner " + c + " expected ~" + expected[k] + " got " + normals[i]);
    }
  }
}

test("mode 4 TRIANGLES indexed behavior is unchanged", () => {
  const context = createLoaderContext();
  const quad = [0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0];
  const { doc, binary } = buildFixture({ mode: 4, positions: quad, indices: { data: [0, 1, 2, 0, 2, 3], type: 5123 } });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1);
  assertCorners(meshes[0], quad, [0, 1, 2, 0, 2, 3]);
});

test("mode 4 TRIANGLES unindexed behavior is unchanged", () => {
  const context = createLoaderContext();
  const tri = [0, 0, 0, 1, 0, 0, 0, 1, 0];
  const { doc, binary } = buildFixture({ mode: 4, positions: tri });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1);
  assertCorners(meshes[0], tri, [0, 1, 2]);
});

test("unindexed TRIANGLE_STRIP emits canonical windows with consistent facing", () => {
  const context = createLoaderContext();
  const { doc, binary } = buildFixture({ mode: 5, positions: STRIP_POS });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1, "mode 5 must produce a mesh object");
  // Canonical windows over [0,1,2,3,4]: (0,1,2), (3,2,1), (2,3,4).
  assertCorners(meshes[0], STRIP_POS, STRIP_5_CORNERS);
  assertFaceNormals(meshes[0], STRIP_5_CORNERS, [0, 0, 1]);
});

test("unindexed TRIANGLE_FAN anchors every triangle on the first vertex", () => {
  const context = createLoaderContext();
  const { doc, binary } = buildFixture({ mode: 6, positions: FAN_POS });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1, "mode 6 must produce a mesh object");
  // Windows: (0,1,2), (0,2,3) — all anchored at vertex 0.
  assertCorners(meshes[0], FAN_POS, FAN_4_CORNERS);
  assertFaceNormals(meshes[0], FAN_4_CORNERS, [0, 0, 1]);
});

test("indexed TRIANGLE_FAN follows the index list, anchored on index[0]", () => {
  const context = createLoaderContext();
  const { doc, binary } = buildFixture({
    mode: 6, positions: FAN_POS, indices: { data: [0, 2, 3, 1], type: 5123 },
  });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1);
  // Windows: (0,2,3), (0,3,1).
  assertCorners(meshes[0], FAN_POS, FAN_INDEXED_CORNERS);
});

test("Uint32 strip indices above 65535 are not truncated", () => {
  const context = createLoaderContext();
  const vertexCount = 70010;
  const positions = new Float32Array(vertexCount * 3); // mostly zeros
  const mark = (v, x, y) => { positions[v * 3] = x; positions[v * 3 + 1] = y; };
  mark(70000, 1, 0); mark(70001, 0, 1); mark(70002, 2, 0); mark(70003, 0, 2);
  const { doc, binary } = buildFixture({
    mode: 5, positions,
    indices: { data: [70000, 70001, 70002, 70003], type: 5125 },
  });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1);
  // Canonical windows: (70000,70001,70002), (70003,70002,70001). A Uint16
  // truncation would collapse 70000 -> 4464 (a zero vertex) and fail here.
  assertCorners(meshes[0], positions, STRIP_UINT32_CORNERS);
});

test("fewer than 3 input corners yield zero triangles without dropping the valid primitive", () => {
  const context = createLoaderContext();
  const tri = [0, 0, 0, 1, 0, 0, 0, 1, 0];
  const short = [5, 0, 0, 6, 0, 0];
  const entries = [
    { ctype: 5126, type: "VEC3", data: tri },
    { ctype: 5126, type: "VEC3", data: short },
  ];
  const bin = makeBinary(entries);
  const doc = {
    asset: { version: "2.0" }, scene: 0, scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{
      name: "m",
      primitives: [
        { mode: 4, attributes: { POSITION: 0 } },
        { mode: 5, attributes: { POSITION: 1 } },
        { mode: 6, attributes: { POSITION: 1 } },
      ],
    }],
    accessors: bin.accessors, bufferViews: bin.bufferViews, buffers: bin.buffers,
  };
  const result = extractScene(context, doc, bin.buffer);
  const meshes = meshObjects(result);
  // A count-0 record for the short strip/fan is acceptable — the caller may
  // retain it. Assert on contributed geometry instead of record deletion.
  let totalCorners = 0;
  for (const m of meshes) {
    totalCorners += m.count;
    assertFinite(m.positions || [], "positions");
    assertFinite(m.normals || [], "normals");
    assertFinite(m.tangents || [], "tangents");
  }
  assert.equal(totalCorners, 3, "short strip and fan outputs must contribute zero triangles");
  const valid = meshes.filter((m) => m.count > 0);
  assert.equal(valid.length, 1, "exactly the valid mode 4 triangle carries geometry");
  assert.deepEqual(valid[0].positions, [0, 0, 0, 1, 0, 0, 0, 1, 0]);
});

test("degenerate strip windows are retained with window-position parity", () => {
  const context = createLoaderContext();
  const verts = STRIP_POS.slice(0, 12); // 4 authored vertices
  const { doc, binary } = buildFixture({
    mode: 5, positions: verts,
    indices: { data: [0, 1, 2, 1, 3], type: 5123 },
  });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1);
  // Three windows over [0,1,2,1,3], none dropped: (0,1,2), the retained
  // degenerate (1,2,1), then (2,1,3) — window 2 is even because parity
  // follows the window position, not the emitted-triangle count.
  assertCorners(meshes[0], verts, DEGENERATE_STRIP_CORNERS);
});

test("primitive extraction maps every channel through the strip corner map", () => {
  const context = createLoaderContext();
  const verts = STRIP_POS.slice(0, 12); // 4 authored vertices
  const uvs = [0, 0, 0.25, 1, 0.5, 0, 0.75, 1];
  const normals = [0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1];
  const tangents = [1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1];
  const joints = [0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0];
  const weights = [1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0];
  const { doc, binary } = buildFixture({
    mode: 5, positions: verts, uvs, normals, tangents, joints, weights,
    indices: { data: [0, 1, 2, 3], type: 5123 },
  });
  context.gltfDoc = doc;
  context.binaryBuffer = binary;
  // Scene objects omit joints/weights without a bound skin, so the
  // per-channel mapping is asserted on the actual primitive extraction.
  const geometry = vm.runInContext(
    "gltfExtractMeshPrimitive(gltfDoc, gltfDoc.meshes[0].primitives[0], binaryBuffer, null, null, false, 0, gltfDoc.nodes[0])",
    context
  );
  const cornerMap = STRIP_4_CORNERS; // [0,1,2, 3,2,1]
  assert.equal(geometry.count, 6, "two strip windows -> six corners");
  const channelChecks = [
    ["positions", verts, 3],
    ["normals", normals, 3],
    ["uvs", uvs, 2],
    ["tangents", tangents, 4],
    // Six corners require 24 joint/weight values, expanded from four
    // authored VEC4 vertices through the same corner map.
    ["joints", joints, 4],
    ["weights", weights, 4],
  ];
  for (const [key, authored, width] of channelChecks) {
    assert.deepEqual(
      Array.from(geometry[key]),
      expandByCornerMap(authored, width, cornerMap),
      key + " must follow the strip corner map"
    );
  }
  assert.equal(geometry.morphMeta, null, "no targets -> no morph metadata");

  // Without a skin the scene object deliberately omits joints/weights while
  // still carrying the other channels (skin policy asserted, not invented).
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1, "scene extraction must not silently skip the strip");
  assert.deepEqual(meshes[0].uvs, expandByCornerMap(uvs, 2, cornerMap));
  assert.equal(meshes[0].joints, null, "no skin -> scene object omits joints");
  assert.equal(meshes[0].weights, null, "no skin -> scene object omits weights");
});

test("static morph deltas fold onto triangulated strip corners", () => {
  const context = createLoaderContext();
  const verts = STRIP_POS.slice(0, 12);
  const deltas = [0, 0, 0, 0, 10, 0, 0, 0, 0, 0, 0, 0]; // vertex 1 moves +10y
  const { doc, binary } = buildFixture({
    mode: 5, positions: verts, deltas, meshWeights: [1],
    indices: { data: [0, 1, 2, 3], type: 5123 },
  });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1);
  // Corner sources [0,1,2, 3,2,1]: corners 1 and 5 carry vertex 1's delta.
  const expected = [
    0, 0, 0, 1, 10, 0, 0, 1, 0,
    1, 1, 0, 0, 1, 0, 1, 10, 0,
  ];
  assert.equal(meshes[0].count, 6);
  assert.deepEqual(meshes[0].positions, expected);
  assert.equal(meshes[0].morph, null, "static-only primitives leak no morph metadata");
});

test("animated morph metadata maps target deltas through the corner map", () => {
  const context = createLoaderContext();
  const verts = STRIP_POS.slice(0, 12);
  const deltas = [0, 0, 0, 0, 10, 0, 0, 0, 0, 0, 0, 0];
  const { doc, binary } = buildFixture({
    mode: 5, positions: verts, deltas, meshWeights: [1],
    indices: { data: [0, 1, 2, 3], type: 5123 },
    animateWeights: [[0, 1], [1, 1]],
  });
  const result = extractScene(context, doc, binary);
  const meshes = meshObjects(result);
  assert.equal(meshes.length, 1);
  const meta = meshes[0].morph;
  assert.ok(meta, "weight-animated strip must carry morph metadata");
  assert.equal(meta.vertexCount, 6, "metadata vertex count is the corner count");
  assert.deepEqual(meta.defaults, [1]);
  // Pristine base expanded through corner sources [0,1,2, 3,2,1].
  assert.deepEqual(meta.basePositions, [
    0, 0, 0, 1, 0, 0, 0, 1, 0,
    1, 1, 0, 0, 1, 0, 1, 0, 0,
  ]);
  // Vertex 1's delta lands on corners 1 and 5.
  assert.deepEqual(meta.targetPositions[0], [
    0, 0, 0, 0, 10, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 10, 0,
  ]);
  assert.deepEqual(meta.targetNormals, [null], "no NORMAL target authored");
  // The baked scene positions are the statically folded strip.
  assert.equal(meshes[0].count, 6);
  assert.deepEqual(meshes[0].positions, [
    0, 0, 0, 1, 10, 0, 0, 1, 0,
    1, 1, 0, 0, 1, 0, 1, 10, 0,
  ]);
});

test("points and lines primitives are unaffected by the strip/fan change", () => {
  const context = createLoaderContext();
  const pointData = [2, 0, 0, 3, 0, 0, 4, 0, 0];
  const lineData = [5, 0, 0, 6, 0, 0, 6, 1, 0, 5, 1, 0];
  const entries = [
    { ctype: 5126, type: "VEC3", data: [0, 0, 0, 1, 0, 0, 0, 1, 0] },
    { ctype: 5126, type: "VEC3", data: pointData },
    { ctype: 5126, type: "VEC3", data: lineData },
  ];
  const bin = makeBinary(entries);
  const doc = {
    asset: { version: "2.0" }, scene: 0, scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{
      name: "m",
      primitives: [
        { mode: 0, attributes: { POSITION: 1 } },
        { mode: 1, attributes: { POSITION: 2 } },
      ],
    }],
    accessors: bin.accessors, bufferViews: bin.bufferViews, buffers: bin.buffers,
  };
  const result = extractScene(context, doc, bin.buffer);
  assert.equal(meshObjects(result).length, 0, "points/lines never become mesh objects");
  // The line primitive must produce an actual line entry with real positions.
  const lines = result.objects.filter((o) => o.kind === "lines");
  assert.equal(lines.length, 1, "mode 1 must produce a line entry");
  assert.ok(lines[0].id, "line entry carries its id");
  assert.equal(lines[0].count, 4, "line entry exposes its vertex count");
  assert.deepEqual(lines[0].positions, lineData, "line positions survive identity node transform");
  assert.equal(result.points.length, 1);
  assert.equal(result.points[0].count, 3);
  assert.deepEqual(result.points[0].positions, pointData, "point positions survive identity node transform");
});

test("extraction is deterministic and does not mutate the document or binary", () => {
  const context = createLoaderContext();
  const { doc, binary } = buildFixture({
    mode: 5, positions: STRIP_POS, meshWeights: [0.5],
    indices: { data: [0, 1, 2, 3, 4], type: 5123 },
    deltas: [0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
  });
  const binarySnapshot = Array.from(new Uint8Array(binary));
  const docSnapshot = JSON.parse(JSON.stringify(doc));
  const first = extractScene(context, doc, binary);
  // Guard against a silently skipped primitive: assert real output before
  // comparing determinism.
  const meshes = meshObjects(first);
  assert.equal(meshes.length, 1, "strip primitive must produce a mesh object");
  assert.equal(meshes[0].count, 9, "canonical triangulation of [0,1,2,3,4]");
  assert.deepEqual(meshes[0].positions, [
    0, 0.5, 0, 1, 0, 0, 0, 1, 0,
    1, 1, 0, 0, 1, 0, 1, 0, 0,
    0, 1, 0, 1, 1, 0, 0, 2, 0,
  ], "vertex 0's delta folds at authored weight 0.5 onto its corners");
  const second = extractScene(context, doc, binary);
  assert.deepEqual(second, first, "repeated extraction must be identical");
  assert.deepEqual(JSON.parse(JSON.stringify(doc)), docSnapshot, "document untouched");
  assert.deepEqual(Array.from(new Uint8Array(binary)), binarySnapshot, "binary untouched");
});
