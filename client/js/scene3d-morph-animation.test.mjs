"use strict";
// Animated glTF morph-weight deformation: regression coverage for the
// per-frame fold driven through the existing motion mixers. Source fragments
// load into ONE VM context; GLB bytes are built inside the VM so typed-array
// identity matches the loader's realm. No network, no GPU.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
// Tests live in client/js: bundled chunks in client/js/bootstrap-src, runtime
// scene3d modules in client/runtime/scene3d.
function readChunkSource(name) {
  return fs.readFileSync(path.join(__dirname, "bootstrap-src", name), "utf8");
}
function readRuntimeSource(name) {
  return fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", name), "utf8");
}

// One triangle, 5 morph targets: POSITION deltas on all targets, NORMAL and
// TANGENT deltas on TARGET 0 ONLY (absent channels must fold as zero — part
// of the contract under test). LINEAR weight channel over node 0 with
// keyframes at t=0 (all zero), t=0.5 and t=2 (final vector, flat tail so a
// finished mixer holds exactly-known weights). Options: indexed (default),
// withNormals/withTangents (default true), malformed (adds a short-delta
// POSITION target declaring the CORRECT count of 2 with a short NORMAL, a
// target naming a missing accessor, and a null target), a custom node (TRSA),
// and an optional translation channel for the mixed morph+TRS test.
// parent places a bare parent node above the mesh node: the weight channel
// retargets to the mesh node (index 1) and the translation channel animates
// the parent. instances attaches EXT_mesh_gpu_instancing TRANSLATION,
// ROTATION and SCALE buffers holding authored instance-local TRS (T*R*S).
const FIXTURE_SOURCE = [
  "function __morphFixture(opts) {",
  "  opts = opts || {};",
  "  var indexed = opts.indexed !== false;",
  "  var withNormals = opts.withNormals !== false;",
  "  var withTangents = opts.withTangents !== false;",
  "  var malformed = opts.malformed === true;",
  "  var translate = opts.translate || null;",
  "  var node = opts.node || { mesh: 0 };",
  "  var parent = opts.parent === true;",
  "  var instances = opts.instances || null;",
  "  var basePositions = [0,0,0, 1,0,0, 0,1,0];",
  "  var baseNormals = [0,0,1, 0,0,1, 0,0,1];",
  "  var baseTangents = [1,0,0,1, 1,0,0,1, 1,0,0,1];",
  "  var baseUVs = [0,0, 1,0, 0,1];",
  "  var targetPos = [",
  "    [0.5,0,0, 0,0.5,0, 0,0,0.5],",
  "    [0,0.25,0, 0.25,0,0, 0,0,0.25],",
  "    [0.1,0,0, 0,0.1,0, 0,0,0.1],",
  "    [-0.2,0,0, 0,-0.2,0, 0,0,-0.2],",
  "    [0.05,0.05,0.05, 0.05,0.05,0.05, 0.05,0.05,0.05]",
  "  ];",
  "  if (malformed) { targetPos.push([9,9,9, 9,9,9]); }",
  "  var targetNormal = [0,0,0.25, 0,0,0.25, 0,0,0.25];",
  "  var targetTangent = [0,1,0, 0,1,0, 0,1,0];",
  "  var floats = [];",
  "  var accessors = [];",
  "  function pushAccessor(values, type, count) {",
  "    var index = accessors.length;",
  "    accessors.push({ bufferView: 0, byteOffset: floats.length * 4, componentType: 5126, count: count, type: type });",
  "    floats = floats.concat(values);",
  "    return index;",
  "  }",
  "  var posAccessor = pushAccessor(basePositions, 'VEC3', 3);",
  "  var normalAccessor = withNormals ? pushAccessor(baseNormals, 'VEC3', 3) : -1;",
  "  var tangentAccessor = withTangents ? pushAccessor(baseTangents, 'VEC4', 3) : -1;",
  "  var uvAccessor = pushAccessor(baseUVs, 'VEC2', 3);",
  "  var instanceAttributes = null;",
  "  if (instances) {",
  "    var instanceTranslations = [];",
  "    var instanceRotations = [];",
  "    var instanceScales = [];",
  "    for (var inst = 0; inst < instances.length; inst++) {",
  "      var spec = instances[inst] || {};",
  "      var it = spec.t || [0, 0, 0];",
  "      var iq = spec.r || [0, 0, 0, 1];",
  "      var isv = spec.s || [1, 1, 1];",
  "      instanceTranslations.push(it[0], it[1], it[2]);",
  "      instanceRotations.push(iq[0], iq[1], iq[2], iq[3]);",
  "      instanceScales.push(isv[0], isv[1], isv[2]);",
  "    }",
  "    instanceAttributes = {",
  "      TRANSLATION: pushAccessor(instanceTranslations, 'VEC3', instances.length),",
  "      ROTATION: pushAccessor(instanceRotations, 'VEC4', instances.length),",
  "      SCALE: pushAccessor(instanceScales, 'VEC3', instances.length),",
  "    };",
  "  }",
  "  var targets = [];",
  "  for (var t = 0; t < targetPos.length; t++) {",
  "    // Malformed target 5 has only 2 vertices: the accessor count MUST say",
  "    // 2, otherwise a read runs off this stream into the next one.",
  "    var target = { POSITION: (malformed && t === 5) ? pushAccessor(targetPos[t], 'VEC3', 2) : pushAccessor(targetPos[t], 'VEC3', 3) };",
  "    if (withNormals && t === 0) { target.NORMAL = pushAccessor(targetNormal, 'VEC3', 3); }",
  "    if (malformed && withNormals && t === 5) { target.NORMAL = pushAccessor([0,0,1, 0,0,1], 'VEC3', 2); }",
  "    if (withTangents && t === 0) { target.TANGENT = pushAccessor(targetTangent, 'VEC3', 3); }",
  "    targets.push(target);",
  "  }",
  "  if (malformed) { targets.push({ POSITION: 999 }); targets.push(null); }",
  "  var timesAccessor = pushAccessor([0, 0.5, 2], 'SCALAR', 3);",
  "  var finalWeights = [];",
  "  for (var f = 0; f < targets.length; f++) { finalWeights.push(f < 5 ? [1, 0.5, -0.25, 2, 0.75][f] : 8); }",
  "  var weightValues = [];",
  "  for (var k = 0; k < 3; k++) {",
  "    for (var w = 0; w < finalWeights.length; w++) { weightValues.push(k === 0 ? 0 : finalWeights[w]); }",
  "  }",
  "  var valuesAccessor = pushAccessor(weightValues, 'SCALAR', weightValues.length);",
  "  var samplers = [{ input: timesAccessor, output: valuesAccessor, interpolation: 'LINEAR' }];",
  "  var channels = [{ sampler: 0, target: { node: parent ? 1 : 0, path: 'weights' } }];",
  "  if (translate) {",
  "    var translateValues = [translate[0][0], translate[0][1], translate[0][2], translate[1][0], translate[1][1], translate[1][2], translate[2][0], translate[2][1], translate[2][2]];",
  "    var translateAccessor = pushAccessor(translateValues, 'VEC3', 3);",
  "    samplers.push({ input: timesAccessor, output: translateAccessor, interpolation: 'LINEAR' });",
  "    channels.push({ sampler: 1, target: { node: 0, path: 'translation' } });",
  "  }",
  "  var indicesAccessor = -1;",
  "  if (indexed) {",
  "    indicesAccessor = accessors.length;",
  "    accessors.push({ bufferView: 0, byteOffset: floats.length * 4, componentType: 5123, count: 3, type: 'SCALAR' });",
  "  }",
  "  var primitive = { attributes: { POSITION: posAccessor, TEXCOORD_0: uvAccessor }, mode: 4, targets: targets };",
  "  if (normalAccessor >= 0) primitive.attributes.NORMAL = normalAccessor;",
  "  if (tangentAccessor >= 0) primitive.attributes.TANGENT = tangentAccessor;",
  "  if (indicesAccessor >= 0) primitive.indices = indicesAccessor;",
  "  if (instances) {",
  "    node.extensions = { EXT_mesh_gpu_instancing: { attributes: instanceAttributes } };",
  "  }",
  "  var doc = {",
  "    asset: { version: '2.0' },",
  "    scenes: [{ nodes: [0] }],",
  "    nodes: parent ? [{ children: [1] }, node] : [node],",
  "    meshes: [{ name: 'm', primitives: [primitive] }],",
  "    animations: [{ name: 'morph', channels: channels, samplers: samplers }],",
  "    bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: 0 }],",
  "    accessors: accessors",
  "  };",
  "  if (instances) { doc.extensionsUsed = ['EXT_mesh_gpu_instancing']; }",
  "  var floatArray = new Float32Array(floats);",
  "  var floatBytes = new Uint8Array(floatArray.buffer);",
  "  var indexBytes = indexed ? new Uint8Array(new Uint16Array([0, 1, 2]).buffer) : new Uint8Array(0);",
  "  var binLength = floatBytes.length + indexBytes.length;",
  "  var binPadded = (binLength + 3) & ~3;",
  "  var bin = new Uint8Array(binPadded);",
  "  bin.set(floatBytes, 0);",
  "  bin.set(indexBytes, floatBytes.length);",
  "  doc.bufferViews[0].byteLength = binPadded;",
  "  var jsonText = JSON.stringify(doc);",
  "  var jsonPadded = (jsonText.length + 3) & ~3;",
  "  var json = new Uint8Array(jsonPadded);",
  "  for (var i = 0; i < jsonText.length; i++) { json[i] = jsonText.charCodeAt(i) & 0xff; }",
  "  for (var p = jsonText.length; p < jsonPadded; p++) { json[p] = 0x20; }",
  "  var total = 12 + 8 + jsonPadded + 8 + binPadded;",
  "  var glb = new ArrayBuffer(total);",
  "  var head = new DataView(glb);",
  "  head.setUint32(0, 0x46546C67, true);",
  "  head.setUint32(4, 2, true);",
  "  head.setUint32(8, total, true);",
  "  // Chunk header order is [length][type]: JSON length at 12, type at 16.",
  "  head.setUint32(12, jsonPadded, true);",
  "  head.setUint32(16, 0x4E4F534A, true);",
  "  new Uint8Array(glb, 20, jsonPadded).set(json);",
  "  var binOffset = 20 + jsonPadded;",
  "  // BIN chunk: same order — length, then type.",
  "  head.setUint32(binOffset, binPadded, true);",
  "  head.setUint32(binOffset + 4, 0x004E4942, true);",
  "  new Uint8Array(glb, binOffset + 8, binPadded).set(bin);",
  "  return { doc: doc, bytes: glb, weightCount: targets.length, finalWeights: finalWeights };",
  "}",
].join("\n");

function createContextWithFetch() {
  const consoleLogs = { warn: [], error: [] };
  const routes = new Map();
  const sandbox = {
    console: {
      warn: (...args) => consoleLogs.warn.push(args.join(" ")),
      error: (...args) => consoleLogs.error.push(args.join(" ")),
      log: () => {},
    },
    Math, JSON, Number, Object, Array, String, Boolean, Date, Promise, Set, Map, WeakMap,
    isFinite, parseFloat, parseInt,
    ArrayBuffer, DataView, TextDecoder, TextEncoder, URL, Error, TypeError,
    Float32Array, Float64Array, Uint8Array, Uint16Array, Uint32Array, Int8Array, Int16Array, Int32Array,
    performance: { now: () => 0 },
    location: { href: "http://test.local/" },
    setTimeout, clearTimeout,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.self = sandbox;
  sandbox.fetch = function (url) {
    const requested = String(url);
    let route = routes.get(requested);
    if (!route) {
      for (const key of routes.keys()) {
        if (requested.endsWith(key)) { route = routes.get(key); break; }
      }
    }
    if (!route) {
      return Promise.resolve({
        ok: false, status: 404,
        arrayBuffer: () => Promise.resolve(new ArrayBuffer(0)),
        text: () => Promise.resolve(""),
      });
    }
    return Promise.resolve({
      ok: true, status: 200,
      arrayBuffer: () => Promise.resolve(route),
      text: () => Promise.resolve(""),
    });
  };
  sandbox.__morphRoutes = routes;
  const context = vm.createContext(sandbox);
  vm.runInContext(readChunkSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readRuntimeSource("gltf.ts"), context, { filename: "gltf.ts" });
  vm.runInContext(FIXTURE_SOURCE, context, { filename: "morph-fixture.js" });
  return { context, consoleLogs };
}

function createContextWithAnimation() {
  const env = createContextWithFetch();
  vm.runInContext(readRuntimeSource("animation.ts"), env.context, { filename: "animation.ts" });
  return env;
}

async function parseMorphAsset(context, options) {
  const fixture = vm.runInContext("__morphFixture(" + JSON.stringify(options || {}) + ")", context);
  context.__morphRoutes.set("/models/morph.glb", fixture.bytes);
  const api = vm.runInContext("window.__gosx_scene3d_gltf_api", context);
  const asset = await api.sceneLoadGLTFModel("/models/morph.glb");
  return { asset, fixture, api };
}

const BASE_TRIANGLES = [[0, 0, 0], [1, 0, 0], [0, 1, 0]];
const BASE_POSITIONS = [0, 0, 0, 1, 0, 0, 0, 1, 0];
const FINAL_WEIGHTS = [1, 0.5, -0.25, 2, 0.75];
// base + 1*t0 + 0.5*t1 - 0.25*t2 + 2*t3 + 0.75*t4, per vertex (hand-derived).
const FULL_FOLD = [
  [0.1125, 0.1625, 0.0375],
  [1.1625, 0.1125, 0.0375],
  [0.0375, 1.0375, 0.2375],
];
// Authored EXT_mesh_gpu_instancing TRS for the instanced fixtures. Both
// instances rotate 90 degrees around Z (noncommuting with the scale that
// follows) then translate: glTF instance-local = T * R * S, composed AFTER
// the animated node world and BEFORE the model transform:
//   instance 0: p -> (2 - y, 2x, z)   [T(2,0,0), Rz90, S(2,1,1)]
//   instance 1: p -> (-2y, x + 3, z)  [T(0,3,0), Rz90, S(1,2,1)]
const INSTANCE_Z_TURN = [0, 0, Math.SQRT1_2, Math.SQRT1_2];
const INSTANCE_TRS = [
  { t: [2, 0, 0], r: INSTANCE_Z_TURN, s: [2, 1, 1] },
  { t: [0, 3, 0], r: INSTANCE_Z_TURN, s: [1, 2, 1] },
];

function assertClose(actual, expected, label, tolerance) {
  const tol = tolerance == null ? 1e-5 : tolerance;
  assert.equal(actual.length, expected.length, label + " length");
  for (let i = 0; i < expected.length; i += 1) {
    assert.ok(
      Math.abs(Number(actual[i]) - expected[i]) < tol,
      label + "[" + i + "]: " + actual[i] + " ~ " + expected[i],
    );
  }
}

function assertTriangle(actual, expected, label, tolerance) {
  assert.equal(actual.length, 9, label + " vertex count");
  for (let v = 0; v < 3; v += 1) {
    assertClose(Array.from(actual.slice(v * 3, v * 3 + 3)), expected[v], label + " v" + v, tolerance);
  }
}
// Instance-local INPUT matrices may use the production TRS helper - only the
// expected coordinates in the instanced tests are hand-computed without it.
// Builds the per-instance fold entry exactly as the mount layer does: shared
// immutable meta, private live vertices copy, per-instance matrices.
// GPU-instanced objects carry their authored instance-local TRS in the
// loader-emitted meta.instanceMatrix, so entries need no extra argument; the
// fixture parser is the source of the instance offsets.
function makeEntry(object, modelMatrix, skinned, withLocal) {
  const meta = object._morphAnim;
  assert.ok(meta, "asset object carries private morph metadata");
  const source = object.vertices;
  const entry = {
    meta,
    vertices: {
      positions: new Float32Array(source.positions),
      normals: new Float32Array(source.normals),
      uvs: new Float32Array(source.uvs),
      tangents: new Float32Array(source.tangents),
      count: source.count,
    },
    skinned: Boolean(skinned),
    nodeMatrix: object.transform,
    modelMatrix: modelMatrix || null,
    modelLocalVertices: null,
    lastWeights: meta.defaults.slice(),
    lastFolded: null,
    lastNodeMatrix: null,
    lastModelMatrix: null,
  };
  if (withLocal) {
    entry.modelLocalVertices = {
      positions: new Float32Array(source.positions),
      normals: new Float32Array(source.normals),
      uvs: new Float32Array(source.uvs),
      tangents: new Float32Array(source.tangents),
      count: source.count,
    };
  }
  return entry;
}

function weightsMap(nodeIndex, weights) {
  return new Map([[nodeIndex, { weights }]]);
}

test("split-bundle suffix republishes applyMorphPose", () => {
  const suffixSource = readChunkSource("26f-feature-scene3d-gltf-suffix.ts");
  const flattened = suffixSource.replace(/\n/g, " ");
  assert.match(
    flattened,
    /__gosx_scene3d_gltf_api\s*=\s*\{[^}]*applyMorphPose\s*:/,
    "suffix API publication must include applyMorphPose (it overwrites the main publication)",
  );
});

test("parsed morph assets keep private immutable metadata, pristine streams, shallow shared clone container", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, {});
  const object = asset.objects[0];
  const meta = object._morphAnim;
  assert.ok(meta, "private _morphAnim metadata present");
  assert.equal(object.morphTargets, undefined, "no public morphTargets field");
  assert.equal(object.morphWeights, undefined, "no public morphWeights field");
  assert.equal(object.vertices.morphTargets, undefined);
  assert.equal(object.vertices.morphWeights, undefined);
  assert.equal(meta.nodeIndex, 0);
  assert.deepEqual(Array.from(meta.defaults), [0, 0, 0, 0, 0]);
  assert.equal(meta.targetPositions.length, 5);
  assert.equal(meta.targetNormals.length, 5);
  assert.equal(meta.targetTangents.length, 5);
  assertClose(Array.from(meta.basePositions), BASE_POSITIONS, "base positions");
  assert.notEqual(meta.basePositions, object.vertices.positions, "metadata base is a copy");
  assertTriangle(object.vertices.positions, BASE_TRIANGLES, "pristine instance source");
  assert.equal(object.transform.length, 16, "node matrix retained for the fold");
  // gltfSceneToModelAsset is a shallow asset container conversion, NOT a
  // live-instance clone: the shared instance and its pristine source arrays
  // are reused verbatim, while per-instance fold outputs stay distinct.
  const clone = api.gltfSceneToModelAsset(asset, "clone");
  assert.equal(clone.objects[0]._morphAnim, meta, "immutable source shared by clones");
  assert.equal(clone.objects[0], object, "shallow container conversion shares the live instance");
  assertTriangle(clone.objects[0].vertices.positions, BASE_TRIANGLES, "shared pristine streams unchanged");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("applyMorphPose folds known weights into instance vertices before the model transform", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, {});
  const modelMatrix = vm.runInContext("sceneTRSToMat4([10, 0, 0], [0, 0, 0, 1], [1, 1, 1])", env.context);
  const entry = makeEntry(asset.objects[0], modelMatrix, false);
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  assertTriangle(entry.vertices.positions, [
    [10.1125, 0.1625, 0.0375],
    [11.1625, 0.1125, 0.0375],
    [10.0375, 1.0375, 0.2375],
  ], "morphed + model-translated positions");
  assertClose(Array.from(entry.vertices.normals.slice(0, 3)), [0, 0, 1], "normals renormalized after transform");
  assertClose(Array.from(entry.vertices.tangents.slice(0, 4)), [Math.SQRT1_2, Math.SQRT1_2, 0, 1], "tangents");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("repeated identical samples are skipped and never accumulate", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, {});
  const entry = makeEntry(asset.objects[0], null, false);
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  const first = entry.vertices.positions;
  assertTriangle(Array.from(first), FULL_FOLD, "first fold");
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS.slice()));
  assert.equal(entry.vertices.positions, first, "identical sample skips the re-fold");
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  assert.equal(entry.vertices.positions, first, "same map skips again");
  assertTriangle(Array.from(entry.vertices.positions), FULL_FOLD, "no accumulation");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("two instances of one asset fold different samples from shared immutable metadata", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, {});
  const modelB = vm.runInContext("sceneTRSToMat4([100, 0, 0], [0, 0, 0, 1], [1, 1, 1])", env.context);
  const entryA = makeEntry(asset.objects[0], null, false);
  const entryB = makeEntry(asset.objects[0], modelB, false);
  assert.equal(entryA.meta, entryB.meta, "immutable source shared by clones");
  api.applyMorphPose([entryA], weightsMap(0, [1, 0, 0, 0, 0]));
  api.applyMorphPose([entryB], weightsMap(0, [0, 1, 0, 0, 0]));
  assertTriangle(Array.from(entryA.vertices.positions), [[0.5, 0, 0], [1, 0.5, 0], [0, 1, 0.5]], "instance A");
  assertTriangle(Array.from(entryB.vertices.positions), [[100, 0.25, 0], [101.25, 0, 0], [100, 1, 0.25]], "instance B");
  assertClose(Array.from(entryA.meta.basePositions), BASE_POSITIONS, "shared base untouched");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("nonidentity node and model transforms apply after the primitive-local fold", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, {
    node: { mesh: 0, translation: [5, 0, 0], scale: [2, 1, 1] },
  });
  const modelMatrix = vm.runInContext("sceneTRSToMat4([10, 0, 0], [0, 0, 0, 1], [1, 1, 1])", env.context);
  const entry = makeEntry(asset.objects[0], modelMatrix, false);
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  assertTriangle(entry.vertices.positions, [
    [15.225, 0.1625, 0.0375],
    [17.325, 0.1125, 0.0375],
    [15.075, 1.0375, 0.2375],
  ], "fold then node x model transform");
  assertClose(Array.from(entry.vertices.normals.slice(0, 3)), [0, 0, 1], "normal under nonuniform node scale");
  assertClose(
    Array.from(entry.vertices.tangents.slice(0, 4)),
    [2 / Math.sqrt(5), 1 / Math.sqrt(5), 0, 1],
    "tangent direction under nonuniform node scale",
  );
  assert.equal(env.consoleLogs.error.length, 0);
});

test("non-uniform scale transforms normals through the inverse-transpose matrix", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, { withNormals: false, withTangents: false });
  const entry = makeEntry(asset.objects[0], null, false);
  entry.nodeMatrix = vm.runInContext("sceneTRSToMat4([5, 0, 0], [0, 0, 0, 1], [2, 1, 1])", env.context);
  api.applyMorphPose([entry], weightsMap(0, [1, 0, 0, 0, 0]));
  assertTriangle(Array.from(entry.vertices.positions), [[6, 0, 0], [7, 0.5, 0], [5, 1, 0.5]], "fold then scale + translate");
  // Folded flat normal (1, -1, 3)/sqrt(11). The inverse-transpose of the
  // linear part diag(2,1,1) is diag(0.5,1,1), giving (1, -2, 6)/sqrt(41)
  // after renormalization — the plain upper-left 3x3 would yield
  // (2, -1, 3)/sqrt(14) instead.
  const invLen = Math.sqrt(41);
  assertClose(
    Array.from(entry.vertices.normals.slice(0, 3)),
    [1 / invLen, -2 / invLen, 6 / invLen],
    "inverse-transpose normal under non-uniform scale",
  );
  assert.equal(entry.vertices.tangents.length, 12, "tangents survive the transform");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("live model move re-applies with unchanged and changed weights; _modelLocalVertices keeps the node transform", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, {
    node: { mesh: 0, translation: [5, 0, 0], scale: [2, 1, 1] },
  });
  const entry = makeEntry(asset.objects[0], null, false, true);
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  // Node-only stage: model-local (asset-space) geometry keeps the node
  // transform — NOT primitive-local, NOT model-transformed.
  const nodeLocal = [
    [5.225, 0.1625, 0.0375],
    [7.325, 0.1125, 0.0375],
    [5.075, 1.0375, 0.2375],
  ];
  assertTriangle(Array.from(entry.modelLocalVertices.positions), nodeLocal, "model-local keeps the node transform");
  assertTriangle(Array.from(entry.vertices.positions), nodeLocal, "untransformed model: vertices match");

  // Live move with UNCHANGED weights: the refreshed model matrix must still
  // re-transform the cached primitive-local fold (no stale geometry).
  entry.modelMatrix = vm.runInContext("sceneTRSToMat4([10, 0, 0], [0, 0, 0, 1], [1, 1, 1])", env.context);
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  assertTriangle(Array.from(entry.vertices.positions), [
    [15.225, 0.1625, 0.0375],
    [17.325, 0.1125, 0.0375],
    [15.075, 1.0375, 0.2375],
  ], "model-only move with unchanged weights");
  assertTriangle(Array.from(entry.modelLocalVertices.positions), nodeLocal, "model-local unchanged by the model move");

  // CHANGED weights on the moved model: fresh fold + both transforms.
  api.applyMorphPose([entry], weightsMap(0, [1, 0, 0, 0, 0]));
  assertTriangle(Array.from(entry.vertices.positions), [
    [16, 0, 0],
    [17, 0.5, 0],
    [15, 1, 0.5],
  ], "changed weights after the move");
  assertTriangle(Array.from(entry.modelLocalVertices.positions), [
    [6, 0, 0],
    [7, 0.5, 0],
    [5, 1, 0.5],
  ], "model-local follows the new fold, node transform only");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("stopped and absent weight channels restore authored defaults, then replay re-folds", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, {});
  const entry = makeEntry(asset.objects[0], null, false);
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  assertTriangle(Array.from(entry.vertices.positions), FULL_FOLD, "weighted fold");
  api.applyMorphPose([entry], new Map());
  assertTriangle(Array.from(entry.vertices.positions), BASE_TRIANGLES, "defaults restored");
  assertClose(Array.from(entry.vertices.tangents.slice(0, 4)), [1, 0, 0, 1], "tangent restored");
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  assertTriangle(Array.from(entry.vertices.positions), FULL_FOLD, "replay re-folds");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("completed fade-out through the real mixer restores defaults, then replay re-folds", async () => {
  const env = createContextWithAnimation();
  const { asset, api } = await parseMorphAsset(env.context, {});
  const animationApi = vm.runInContext("window.__gosx_scene3d_animation_api", env.context);
  const clip = asset.animations[0];
  const mixer = animationApi.createMixer();
  mixer.addClip(clip.name, clip);
  mixer.play(clip.name, { loop: false, fadeIn: 0 });
  const collect = () => {
    const sampled = new Map();
    mixer.update(0.25, (targetNode, property, value) => {
      let slot = sampled.get(targetNode);
      if (!slot) { slot = {}; sampled.set(targetNode, slot); }
      slot[property] = Array.from(value);
    });
    return sampled;
  };
  const entry = makeEntry(asset.objects[0], null, false);
  // Drive onto the flat final tail (t = 1.0s, weights flat from 0.5s to 2s).
  for (let i = 0; i < 4; i += 1) api.applyMorphPose([entry], collect());
  assertTriangle(Array.from(entry.vertices.positions), FULL_FOLD, "final pose before stop");
  mixer.stop(clip.name, { fadeOut: 0.5 });
  // Ticks through and past the fade: once the fade completes the mixer no
  // longer samples the clip, so the pose carries no weights entry and the
  // fold must restore the authored defaults.
  let postFade = null;
  for (let i = 0; i < 6; i += 1) {
    postFade = collect();
    api.applyMorphPose([entry], postFade);
  }
  assert.equal(postFade.get(0) && postFade.get(0).weights, undefined, "weight channel gone after fade-out completes");
  assertTriangle(Array.from(entry.vertices.positions), BASE_TRIANGLES, "defaults restored after fade-out");
  mixer.play(clip.name, { loop: false, fadeIn: 0 });
  for (let i = 0; i < 4; i += 1) api.applyMorphPose([entry], collect());
  assertTriangle(Array.from(entry.vertices.positions), FULL_FOLD, "replay re-folds");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("malformed targets and >5 weights degrade safely", async () => {
  const env = createContextWithFetch();
  const { asset, fixture, api } = await parseMorphAsset(env.context, { malformed: true });
  const weights = fixture.finalWeights;
  assert.equal(weights.length, 8);
  // Skinned entry: primitive-local streams, no node/model transform applied.
  const entry = makeEntry(asset.objects[0], null, true);
  api.applyMorphPose([entry], weightsMap(0, weights));
  // Short POSITION target (count 2, weight 8) covers exactly corners 0-1;
  // corner 2 stays pristine. Missing/null targets contribute nothing.
  assertTriangle(Array.from(entry.vertices.positions), [
    [72.1125, 72.1625, 72.0375],
    [73.1625, 72.1125, 72.0375],
    [0.0375, 1.0375, 0.2375],
  ], "missing/short/null targets skip cleanly");
  assertClose(Array.from(entry.vertices.normals.slice(0, 3)), [0, 0, 9.25], "short normal deltas apply only in-range corners");
  assertClose(Array.from(entry.vertices.normals.slice(6, 9)), [0, 0, 1.25], "out-of-range corner untouched");
  assertClose(Array.from(entry.vertices.tangents.slice(0, 4)), [1, 1, 0, 1], "tangent w preserved");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("unindexed primitives pair deltas vertex-direct", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, { indexed: false });
  const entry = makeEntry(asset.objects[0], null, false);
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  assertTriangle(Array.from(entry.vertices.positions), FULL_FOLD, "unindexed fold");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("no authored normals: fallback flat normals regenerate from the folded surface", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, { withNormals: false, withTangents: false });
  const meta = asset.objects[0]._morphAnim;
  assert.equal(meta.baseNormals, null, "no base normals retained");
  const entry = makeEntry(asset.objects[0], null, false);
  api.applyMorphPose([entry], weightsMap(0, [1, 0, 0, 0, 0]));
  assertTriangle(Array.from(entry.vertices.positions), [[0.5, 0, 0], [1, 0.5, 0], [0, 1, 0.5]], "folded positions");
  // cross(v1-v0, v2-v0) of the folded triangle = (0.25, -0.25, 0.75).
  const flatScale = Math.sqrt(0.6875);
  assertClose(
    Array.from(entry.vertices.normals.slice(0, 3)),
    [0.25 / flatScale, -0.25 / flatScale, 0.75 / flatScale],
    "regenerated flat normal",
  );
  assert.equal(entry.vertices.tangents.length, 12, "tangents recomputed for the folded surface");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("skinned instances fold primitive-local, before skinning transforms", async () => {
  const env = createContextWithFetch();
  const { asset, api } = await parseMorphAsset(env.context, {});
  const entry = makeEntry(asset.objects[0], null, true);
  api.applyMorphPose([entry], weightsMap(0, FINAL_WEIGHTS));
  assertTriangle(Array.from(entry.vertices.positions), FULL_FOLD, "primitive-local fold");
  assertClose(Array.from(entry.vertices.normals.slice(0, 3)), [0, 0, 1.25], "raw folded local normal");
  assertClose(Array.from(entry.vertices.tangents.slice(0, 4)), [1, 1, 0, 1], "raw folded local tangent");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("real JS motion mixer samples the weight clip and drives the fold", async () => {
  const env = createContextWithAnimation();
  const { asset, api } = await parseMorphAsset(env.context, {});
  const animationApi = vm.runInContext("window.__gosx_scene3d_animation_api", env.context);
  const clip = asset.animations[0];
  assert.equal(clip.name, "morph");
  const mixer = animationApi.createMixer();
  mixer.addClip(clip.name, clip);
  mixer.play(clip.name, { loop: false, fadeIn: 0 });
  const sampled = new Map();
  mixer.update(1.0, function (targetNode, property, value) {
    sampled.set(targetNode, { [property]: Array.from(value) });
  });
  assertClose(Array.from(sampled.get(0).weights), FINAL_WEIGHTS, "mixer weight sample");
  const entry = makeEntry(asset.objects[0], null, false);
  api.applyMorphPose([entry], sampled);
  assertTriangle(Array.from(entry.vertices.positions), FULL_FOLD, "mixer-driven fold");
  assertClose(Array.from(entry.vertices.tangents.slice(0, 4)), [Math.SQRT1_2, Math.SQRT1_2, 0, 1], "tangents");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("mixed morph + TRS animation: animated node TRS folds once, model transform applied once", async () => {
  const env = createContextWithAnimation();
  const { asset, api } = await parseMorphAsset(env.context, { translate: [[0, 0, 0], [5, 0, 0], [5, 0, 0]] });
  const animationApi = vm.runInContext("window.__gosx_scene3d_animation_api", env.context);
  const clip = asset.animations[0];
  const mixer = animationApi.createMixer();
  mixer.addClip(clip.name, clip);
  mixer.play(clip.name, { loop: false, fadeIn: 0 });
  const sampled = new Map();
  mixer.update(1.0, function (targetNode, property, value) {
    let slot = sampled.get(targetNode);
    if (!slot) { slot = {}; sampled.set(targetNode, slot); }
    slot[property] = Array.from(value);
  });
  assertClose(Array.from(sampled.get(0).weights), FINAL_WEIGHTS, "sampled weights");
  assertClose(Array.from(sampled.get(0).translation), [5, 0, 0], "sampled translation");
  // Model-local node transforms: no root/model transform prepended.
  const localNodeTransforms = animationApi.buildNodeTransforms(asset.nodes, sampled, null, null);
  const modelMatrix = vm.runInContext("sceneTRSToMat4([10, 0, 0], [0, 0, 0, 1], [1, 1, 1])", env.context);
  const entry = makeEntry(asset.objects[0], modelMatrix, false, true);
  api.applyMorphPose([entry], sampled, localNodeTransforms);
  assertTriangle(Array.from(entry.vertices.positions), [
    [15.1125, 0.1625, 0.0375],
    [16.1625, 0.1125, 0.0375],
    [15.0375, 1.0375, 0.2375],
  ], "fold + animated node TRS + single model transform");
  assertTriangle(Array.from(entry.modelLocalVertices.positions), [
    [5.1125, 0.1625, 0.0375],
    [6.1625, 0.1125, 0.0375],
    [5.0375, 1.0375, 0.2375],
  ], "model-local carries the animated node TRS, not the model transform");
  assert.equal(env.consoleLogs.error.length, 0);
});

// ---------------------------------------------------------------------------
// EXT_mesh_gpu_instancing + morph weights: the live fold must compose the
// animated hierarchy node-world, then the authored instance-local TRS, around
// the folded vertex, and apply the model transform exactly once. The current
// implementation drops the animated node world for instanced entries, so each
// expected coordinate below fails until that composition is fixed. The two
// instance objects come out of the loader independently parsed, each with its
// own immutable meta.instanceMatrix; every expected coordinate is
// hand-computed without the production matrix helpers.
// ---------------------------------------------------------------------------
test("gpu-instanced fold composes animated node world, authored instance-local, fold, and one model transform", async () => {
  const env = createContextWithAnimation();
  const { asset, api } = await parseMorphAsset(env.context, { instances: INSTANCE_TRS });
  const meta = asset.objects[0]._morphAnim;
  assert.ok(meta.instanced, "loader parsed the gpu-instancing extension into the shared meta");
  const animationApi = vm.runInContext("window.__gosx_scene3d_animation_api", env.context);
  // Animated node world (5,0,0); model root (10,0,0) goes on top exactly once.
  const sampled = new Map([[0, { translation: [5, 0, 0], weights: FINAL_WEIGHTS }]]);
  const nodeTransforms = animationApi.buildNodeTransforms(asset.nodes, sampled, null, null);
  const modelMatrix = vm.runInContext("sceneTRSToMat4([10, 0, 0], [0, 0, 0, 1], [1, 1, 1])", env.context);
  const entryA = makeEntry(asset.objects[0], modelMatrix, false, true);
  const entryB = makeEntry(asset.objects[1], modelMatrix, false, false);
  assert.ok(entryA.meta.instanced, "instance 0 metadata carries the authored instance TRS");
  assert.ok(entryB.meta.instanced, "instance 1 metadata carries the authored instance TRS");
  assert.notEqual(entryA.meta, entryB.meta, "independently parsed instance objects have distinct metadata");
  assert.notEqual(entryA.vertices.positions, entryB.vertices.positions, "instances own distinct live vertex streams");
  api.applyMorphPose([entryA, entryB], sampled, nodeTransforms);
  // Instance 0, T(2,0,0)*Rz90*S(2,1,1): p -> (2 - y, 2x, z). On FULL_FOLD:
  //   v0 (0.1125, 0.1625, 0.0375) -> (1.8375, 0.225, 0.0375)
  //   v1 (1.1625, 0.1125, 0.0375) -> (1.8875, 2.325, 0.0375)
  //   v2 (0.0375, 1.0375, 0.2375) -> (0.9625, 0.075, 0.2375)
  // then +5 animated node world, then +10 model root, on x.
  assertTriangle(Array.from(entryA.vertices.positions), [
    [16.8375, 0.225, 0.0375],
    [16.8875, 2.325, 0.0375],
    [15.9625, 0.075, 0.2375],
  ], "instance 0: fold, instance-local, node world, one model transform");
  // Instance 1, T(0,3,0)*Rz90*S(1,2,1): p -> (-2y, x + 3, z):
  //   v0 -> (-0.325, 3.1125, 0.0375), v1 -> (-0.225, 4.1625, 0.0375),
  //   v2 -> (-2.075, 3.0375, 0.2375); then +15 on x.
  assertTriangle(Array.from(entryB.vertices.positions), [
    [14.675, 3.1125, 0.0375],
    [14.775, 4.1625, 0.0375],
    [12.925, 3.0375, 0.2375],
  ], "instance 1: distinct authored TRS, same composition");
  // Node-local stage keeps node world * instance-local * fold, no model root.
  assertTriangle(Array.from(entryA.modelLocalVertices.positions), [
    [6.8375, 0.225, 0.0375],
    [6.8875, 2.325, 0.0375],
    [5.9625, 0.075, 0.2375],
  ], "model-local carries node world + instance-local, not the model root");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("gpu-instanced fold follows an animated ancestor while the weights stay fixed", async () => {
  const env = createContextWithAnimation();
  const { asset, api } = await parseMorphAsset(env.context, { parent: true, instances: [INSTANCE_TRS[0]] });
  const animationApi = vm.runInContext("window.__gosx_scene3d_animation_api", env.context);
  const entry = makeEntry(asset.objects[0], null, false, true);
  // Fixed weights [1,0,0,0,0] in both poses; only the parent translation moves.
  const poseA = new Map([[0, { translation: [0, 0, 0] }], [1, { weights: [1, 0, 0, 0, 0] }]]);
  api.applyMorphPose([entry], poseA, animationApi.buildNodeTransforms(asset.nodes, poseA, null, null));
  // Hand check: fold+target0 -> (0.5,0,0) (1,0.5,0) (0,1,0.5); instance-local
  // p -> (2 - y, 2x, z) -> (2,1,0) (1.5,2,0) (1,0,0.5); parent world identity.
  assertTriangle(Array.from(entry.vertices.positions), [
    [2, 1, 0],
    [1.5, 2, 0],
    [1, 0, 0.5],
  ], "ancestor at identity");
  const poseB = new Map([[0, { translation: [7, 0, 0] }], [1, { weights: [1, 0, 0, 0, 0].slice() }]]);
  api.applyMorphPose([entry], poseB, animationApi.buildNodeTransforms(asset.nodes, poseB, null, null));
  // Same fold and instance-local, shifted only by the animated ancestor (+7x).
  assertTriangle(Array.from(entry.vertices.positions), [
    [9, 1, 0],
    [8.5, 2, 0],
    [8, 0, 0.5],
  ], "ancestor motion reaches the instanced fold");
  assertTriangle(Array.from(entry.modelLocalVertices.positions), [
    [9, 1, 0],
    [8.5, 2, 0],
    [8, 0, 0.5],
  ], "model-local follows the ancestor (no model root here)");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("gpu-instanced bare pose keeps the authored instance offset; repeats reuse streams; source immutable", async () => {
  const env = createContextWithAnimation();
  const { asset, api } = await parseMorphAsset(env.context, { instances: [INSTANCE_TRS[0]] });
  const object = asset.objects[0];
  const meta = object._morphAnim;
  const animationApi = vm.runInContext("window.__gosx_scene3d_animation_api", env.context);
  const entry = makeEntry(asset.objects[0], null, false, false);
  // The loader bakes the authored instance TRS into the parsed source stream,
  // so the pristine first-instance positions are the transformed base
  // triangle, not BASE_TRIANGLES. Snapshot before any live fold.
  const sourceSnapshot = Array.from(object.vertices.positions);
  const weighted = new Map([[0, { translation: [4, 0, 0], weights: FINAL_WEIGHTS }]]);
  api.applyMorphPose([entry], weighted, animationApi.buildNodeTransforms(asset.nodes, weighted, null, null));
  // Instance-local on FULL_FOLD -> (1.8375,0.225,0.0375) (1.8875,2.325,0.0375)
  // (0.9625,0.075,0.2375), then +4 animated node world on x.
  assertTriangle(Array.from(entry.vertices.positions), [
    [5.8375, 0.225, 0.0375],
    [5.8875, 2.325, 0.0375],
    [4.9625, 0.075, 0.2375],
  ], "weighted instanced fold under the animated node world");
  // Bare pose: weights fall back to the authored zero defaults, but the
  // authored instance-local offset and the node world still place the base:
  // p -> (2 - y, 2x, z) on the base triangle -> (2,0,0) (2,2,0) (1,0,0),
  // then +4 on x.
  const barePose = new Map([[0, { translation: [4, 0, 0] }]]);
  api.applyMorphPose([entry], new Map(), animationApi.buildNodeTransforms(asset.nodes, barePose, null, null));
  assertTriangle(Array.from(entry.vertices.positions), [
    [6, 0, 0],
    [6, 2, 0],
    [5, 0, 0],
  ], "full-map default-weight restoration");
  api.applyMorphPose([entry], barePose);
  const bare = entry.vertices.positions;
  assertTriangle(Array.from(bare), [
    [6, 0, 0],
    [6, 2, 0],
    [5, 0, 0],
  ], "bare pose keeps the authored instance offset + node world");
  assertClose(Array.from(entry.vertices.normals.slice(0, 3)), [0, 0, 1], "bare base normal survives the instance rotation");
  // Repeating the unchanged pose must reuse the per-instance stream as-is.
  api.applyMorphPose([entry], barePose);
  assert.equal(entry.vertices.positions, bare, "unchanged bare pose reuses the stream");
  assertTriangle(Array.from(entry.vertices.positions), [
    [6, 0, 0],
    [6, 2, 0],
    [5, 0, 0],
  ], "no drift after the repeated pose");
  // The shared loader source stays pristine regardless of instance folds.
  assertTriangle(sourceSnapshot, [
    [2, 0, 0],
    [2, 2, 0],
    [1, 0, 0],
  ], "pristine authored first-instance source: T(2,0,0)*Rz90*S(2,1,1) on the base triangle");
  assertTriangle(object.vertices.positions, [
    [2, 0, 0],
    [2, 2, 0],
    [1, 0, 0],
  ], "source instance stream untouched by live folds");
  assertClose(Array.from(meta.basePositions), BASE_POSITIONS, "metadata base untouched");
  // An empty pose (no map) must restore the authored/default instance pose,
  // applying the authored offset exactly once.
  api.applyMorphPose([entry], new Map());
  assertTriangle(Array.from(entry.vertices.positions), [
    [2, 0, 0],
    [2, 2, 0],
    [1, 0, 0],
  ], "empty pose restores the authored instance pose, offset applied once");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("real mixer drives parent TRS and weights into the gpu-instanced fold", async () => {
  const env = createContextWithAnimation();
  const { asset, api } = await parseMorphAsset(env.context, {
    parent: true,
    instances: [INSTANCE_TRS[0]],
    translate: [[0, 0, 0], [6, 0, 0], [6, 0, 0]],
  });
  const animationApi = vm.runInContext("window.__gosx_scene3d_animation_api", env.context);
  const clip = asset.animations[0];
  const mixer = animationApi.createMixer();
  mixer.addClip(clip.name, clip);
  mixer.play(clip.name, { loop: false, fadeIn: 0 });
  const sampled = new Map();
  mixer.update(1.0, function (targetNode, property, value) {
    let slot = sampled.get(targetNode);
    if (!slot) { slot = {}; sampled.set(targetNode, slot); }
    slot[property] = Array.from(value);
  });
  assertClose(Array.from(sampled.get(1).weights), FINAL_WEIGHTS, "weights sampled on the mesh node");
  assertClose(Array.from(sampled.get(0).translation), [6, 0, 0], "parent translation sampled");
  const nodeTransforms = animationApi.buildNodeTransforms(asset.nodes, sampled, null, null);
  const modelMatrix = vm.runInContext("sceneTRSToMat4([10, 0, 0], [0, 0, 0, 1], [1, 1, 1])", env.context);
  const entry = makeEntry(asset.objects[0], modelMatrix, false, false);
  api.applyMorphPose([entry], sampled, nodeTransforms);
  // Same hand composition as the direct test: instance-local on FULL_FOLD ->
  // (1.8375,0.225,0.0375) (1.8875,2.325,0.0375) (0.9625,0.075,0.2375), then
  // +6 animated parent, then +10 model root, on x.
  assertTriangle(Array.from(entry.vertices.positions), [
    [17.8375, 0.225, 0.0375],
    [17.8875, 2.325, 0.0375],
    [16.9625, 0.075, 0.2375],
  ], "mixer-driven ancestor and weights reach the instanced fold");
  assert.equal(env.consoleLogs.error.length, 0);
});
