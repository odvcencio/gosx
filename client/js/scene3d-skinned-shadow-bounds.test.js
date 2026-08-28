"use strict";

// Regression tests: live GPU-skinned shadow casters must be folded into the
// shared shadow light-fit bounds (sceneShadowComputeBounds). The ACTUAL
// production slice of client/js/bootstrap-src/16c-scene-shared-pbr.ts — the
// same region existing VM slices run, from sceneShadowComputeBounds through
// the skinned influence helper — is executed in a VM. No copies of the code
// under test and no source-only regex assertions.
//
// skin.jointMatrices is ONE FLAT Float32Array of jointCount*16 floats, never
// an array of matrices. Every fixture below uses exactly that flat shape and
// mutates joints in place at jointIndex*16+12..14.

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const BOOTSTRAP_DIR = path.join(__dirname, "bootstrap-src");

const BOUNDS_SOURCE = (() => {
  const source = fs.readFileSync(path.join(BOOTSTRAP_DIR, "16c-scene-shared-pbr.ts"), "utf8");
  const start = source.indexOf("function sceneShadowComputeBounds");
  assert.ok(start >= 0, "sceneShadowComputeBounds located in 16c source");
  const end = source.indexOf("// Generate PBR vertex data", start);
  assert.ok(end > start, "Generate PBR vertex data marker located after it");
  return source.slice(start, end);
})();

function createContext() {
  const context = vm.createContext({
    console, Math, Number, Boolean, String, Array, Object, JSON, isFinite,
    Float32Array, Float64Array,
  });
  vm.runInContext(BOUNDS_SOURCE, context, { filename: "16c-scene-shared-pbr.ts#shadow-bounds" });
  return context;
}

function computeBounds(bundle) {
  const context = createContext();
  context.__bundle = bundle;
  return vm.runInContext("sceneShadowComputeBounds(__bundle)", context, {
    filename: "scene3d-skinned-shadow-bounds.test.js#sceneShadowComputeBounds",
  });
}

function identityMatrix() {
  return new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
}

function translationMatrix(x, y, z) {
  const m = new Float32Array(16);
  m[0] = 1; m[5] = 1; m[10] = 1; m[15] = 1;
  m[12] = x; m[13] = y; m[14] = z;
  return m;
}

// Flat palette: one Float32Array of jointCount*16 floats, identity per joint.
function flatPalette(jointCount) {
  const palette = new Float32Array(jointCount * 16);
  for (let j = 0; j < jointCount; j++) {
    const o = j * 16;
    palette[o] = 1;
    palette[o + 5] = 1;
    palette[o + 10] = 1;
    palette[o + 15] = 1;
  }
  return palette;
}

// Live-animation style in-place mutation at jointIndex*16+12..14.
function setJointTranslation(palette, jointIndex, x, y, z) {
  const o = jointIndex * 16;
  palette[o + 12] = x;
  palette[o + 13] = y;
  palette[o + 14] = z;
}

function makeVertices(positions, weightsPerVertex, jointIndicesPerVertex, extra) {
  const count = weightsPerVertex.length;
  const joints = new Float32Array(count * 4);
  const weights = new Float32Array(count * 4);
  for (let v = 0; v < count; v++) {
    for (let k = 0; k < 4; k++) {
      joints[v * 4 + k] = jointIndicesPerVertex[v][k];
      weights[v * 4 + k] = weightsPerVertex[v][k];
    }
  }
  return Object.assign({ positions, joints, weights, count }, extra || {});
}

function skinnedObject(vertices, palette, overrides) {
  return Object.assign({
    id: "skinned-caster",
    kind: "mesh",
    castShadow: true,
    receiveShadow: false,
    viewCulled: false,
    static: false,
    directVertices: true,
    skin: { jointMatrices: palette },
    vertices,
    // Deliberately bogus: the skinned path must derive bounds from the live
    // palette and model matrix, never from obj.bounds.
    bounds: { minX: -999, minY: -999, minZ: -999, maxX: 999, maxY: 999, maxZ: 999 },
    modelMatrix: identityMatrix(),
    vertexOffset: 0,
    vertexCount: vertices.count,
  }, overrides || {});
}

function makeBundle(meshObjects, worldMeshPositions) {
  return {
    worldMeshPositions: worldMeshPositions || new Float32Array(0),
    meshObjects,
    instancedMeshes: [],
  };
}

function assertBounds(actual, minX, minY, minZ, maxX, maxY, maxZ, label) {
  assert.equal(actual.minX, minX, label + ": minX");
  assert.equal(actual.minY, minY, label + ": minY");
  assert.equal(actual.minZ, minZ, label + ": minZ");
  assert.equal(actual.maxX, maxX, label + ": maxX");
  assert.equal(actual.maxY, maxY, label + ": maxY");
  assert.equal(actual.maxZ, maxZ, label + ": maxZ");
  for (const key of ["minX", "minY", "minZ", "maxX", "maxY", "maxZ"]) {
    assert.ok(Number.isFinite(actual[key]), label + ": " + key + " is finite");
  }
}

// Fallback bounds asserted field-by-field (never deepEqual across realms).
function assertFallbackBounds(actual, label) {
  assertBounds(actual, -10, -10, -10, 10, 10, 10, label);
}

const REST_VERTICES = () => makeVertices(
  new Float32Array([-1, 0, 0, 1, 2, 0, 0, -1, 3]),
  [[1, 0, 0, 0], [1, 0, 0, 0], [1, 0, 0, 0]],
  [[0, 0, 0, 0], [0, 0, 0, 0], [0, 0, 0, 0]]
);

test("single joint at identity yields exact local bounds", () => {
  const bounds = computeBounds(makeBundle([skinnedObject(REST_VERTICES(), flatPalette(1))]));
  assertBounds(bounds, -1, -1, 0, 1, 2, 3, "identity single joint");
});

test("translated joint: rest, posed via same-array palette mutation, back to rest", () => {
  const palette = flatPalette(1);
  const obj = skinnedObject(REST_VERTICES(), palette);

  let bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, -1, -1, 0, 1, 2, 3, "rest joint");

  // Mutate the SAME flat palette in place (live animation): posed bounds must
  // never be cached by palette identity.
  setJointTranslation(palette, 0, 5, -2, 1);
  bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, 4, -3, 1, 6, 0, 4, "posed joint");

  setJointTranslation(palette, 0, 0, 7, 0);
  bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, -1, 6, 0, 1, 9, 3, "re-posed joint");

  setJointTranslation(palette, 0, 0, 0, 0);
  bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, -1, -1, 0, 1, 2, 3, "back to rest");
});

test("nonidentity model matrix folds in and live model changes are honored", () => {
  const obj = skinnedObject(REST_VERTICES(), flatPalette(1), {
    modelMatrix: translationMatrix(10, 0, 0.5),
  });

  let bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, 9, -1, 0.5, 11, 2, 3.5, "translated model");

  obj.modelMatrix = identityMatrix();
  bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, -1, -1, 0, 1, 2, 3, "rest model");
});

test("multi-joint weighted vertices are conservatively contained", () => {
  // Slot k selects joint joints[k] with weight weights[k]: v1 selects
  // joint1 through weight slot 1 with joints[1] = 1.
  const vertices = makeVertices(
    new Float32Array([-1, 0, 0, 1, 0, 0, 0, 1, 0]),
    [[1, 0, 0, 0], [0, 1, 0, 0], [0.5, 0.5, 0, 0]],
    [[0, 0, 0, 0], [0, 1, 0, 0], [0, 1, 0, 0]]
  );
  const palette = flatPalette(2);
  setJointTranslation(palette, 1, 10, 0, 0);
  const bounds = computeBounds(makeBundle([skinnedObject(vertices, palette)]));

  // Exact conservative union: joint0 box x[-1,0], joint1 box x[10,11],
  // shared y[0,1] and z[0,0].
  assertBounds(bounds, -1, 0, 0, 11, 1, 0, "multi-joint union");

  // Every truly skinned vertex position lies inside the reported bound
  // (a weighted blend is a convex combination of per-joint transformed
  // positions, all of which the union contains).
  const skinnedPositions = [
    [-1, 0, 0], // v0: joint0 only
    [11, 0, 0], // v1: joint1 only
    [5, 1, 0],  // v2: 0.5 * joint0 + 0.5 * joint1
  ];
  for (const [x, y, z] of skinnedPositions) {
    assert.ok(x >= bounds.minX && x <= bounds.maxX, "x containment for " + x);
    assert.ok(y >= bounds.minY && y <= bounds.maxY, "y containment for " + y);
    assert.ok(z >= bounds.minZ && z <= bounds.maxZ, "z containment for " + z);
  }
});

test("zero weights never pull in irrelevant far joints", () => {
  const vertices = makeVertices(
    new Float32Array([0, 0, 0, 1, 0, 0, 500, 500, 500]),
    [[1, 0, 0, 0], [1, 0, 0, 0], [0, 0, 0, 0]],
    [[0, 0, 0, 0], [0, 0, 0, 0], [0, 0, 0, 0]]
  );
  const palette = flatPalette(2);
  setJointTranslation(palette, 0, 2, 0, 0);
  setJointTranslation(palette, 1, 1000, 0, 0);
  const bounds = computeBounds(makeBundle([skinnedObject(vertices, palette)]));
  assertBounds(bounds, 2, 0, 0, 3, 0, 0, "zero-weight far joint excluded");
});

test("castShadow false opts skinned casters out; empty scenes fall back", () => {
  assertFallbackBounds(
    computeBounds(makeBundle([
      skinnedObject(REST_VERTICES(), flatPalette(1), { castShadow: false }),
    ])),
    "castShadow false skipped"
  );

  assertFallbackBounds(computeBounds(makeBundle([])), "no objects");
  assertFallbackBounds(
    computeBounds({ worldMeshPositions: new Float32Array(0), instancedMeshes: [] }),
    "missing meshObjects key"
  );
});

test("nested matrix-array palettes are rejected, not misread as flat", () => {
  // length 1 -> jointCount = floor(1/16) = 0.
  const shortNested = skinnedObject(REST_VERTICES(), [identityMatrix()]);
  // length 16 of matrix objects -> jointCount 1, but element 0 is not a number.
  const sixteenNested = skinnedObject(REST_VERTICES(), new Array(16).fill(identityMatrix()));
  const bounds = computeBounds(makeBundle([shortNested, sixteenNested]));
  assertFallbackBounds(bounds, "nested palettes rejected");
});

test("hybrid skin + retainedGeometry uses live skinned bounds, not bind bounds", () => {
  const palette = flatPalette(1);
  setJointTranslation(palette, 0, 5, 0, 0);
  const obj = skinnedObject(REST_VERTICES(), palette, {
    retainedGeometry: true,
    bounds: { minX: 100, minY: 100, minZ: 100, maxX: 200, maxY: 200, maxZ: 200 },
  });
  const bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, 4, -1, 0, 6, 2, 3, "skinned precedence over retained bind bounds");
});

test("retained, soup and non-skinned direct casters keep their existing behavior", () => {
  const soupPositions = new Float32Array([-5, -5, -5, -4, -4, -4]);
  const skinnedVertices = makeVertices(
    new Float32Array([3, 3, 3, 4, 4, 4]),
    [[1, 0, 0, 0], [1, 0, 0, 0]],
    [[0, 0, 0, 0], [0, 0, 0, 0]]
  );
  const bounds = computeBounds(makeBundle([
    {
      id: "retained", castShadow: true, viewCulled: false,
      directVertices: true, retainedGeometry: true,
      bounds: { minX: 0, minY: 0, minZ: 0, maxX: 2, maxY: 2, maxZ: 2 },
    },
    { id: "soup", castShadow: true, viewCulled: false, vertexOffset: 0, vertexCount: 2 },
    skinnedObject(skinnedVertices, flatPalette(1)),
    {
      // directVertices without retainedGeometry and without skin: still skipped.
      id: "bare-direct", castShadow: true, viewCulled: false, directVertices: true,
      bounds: { minX: 100, minY: 100, minZ: 100, maxX: 200, maxY: 200, maxZ: 200 },
    },
  ], soupPositions));
  assertBounds(bounds, -5, -5, -5, 4, 4, 4, "retained + soup + skinned union");
});

test("retained-only and soup-only bounds are unchanged", () => {
  const retained = computeBounds(makeBundle([
    {
      castShadow: true, viewCulled: false, directVertices: true, retainedGeometry: true,
      bounds: { minX: 1, minY: 2, minZ: 3, maxX: 4, maxY: 5, maxZ: 6 },
    },
  ]));
  assertBounds(retained, 1, 2, 3, 4, 5, 6, "retained only");

  const soup = computeBounds(makeBundle([
    { castShadow: true, viewCulled: false, vertexOffset: 0, vertexCount: 2 },
  ], new Float32Array([1, 2, 3, 4, 5, 6])));
  assertBounds(soup, 1, 2, 3, 4, 5, 6, "soup only");
});

test("replacing the positions stream invalidates the influence cache", () => {
  const vertices = makeVertices(
    new Float32Array([0, 0, 0, 1, 1, 1]),
    [[1, 0, 0, 0], [1, 0, 0, 0]],
    [[0, 0, 0, 0], [0, 0, 0, 0]]
  );
  const obj = skinnedObject(vertices, flatPalette(1));

  let bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, 0, 0, 0, 1, 1, 1, "original stream");

  // Morph-style stream swap: new positions identity, same vertices object.
  vertices.positions = new Float32Array([0, 0, 0, 2, 2, 2]);
  bounds = computeBounds(makeBundle([obj]));
  assertBounds(bounds, 0, 0, 0, 2, 2, 2, "replaced stream");
});

test("same-array edits require an explicit revision bump to invalidate", () => {
  const positions = new Float32Array([0, 0, 0, 1, 1, 1]);
  const vertices = makeVertices(
    positions,
    [[1, 0, 0, 0], [1, 0, 0, 0]],
    [[0, 0, 0, 0], [0, 0, 0, 0]]
  );
  const obj = skinnedObject(vertices, flatPalette(1));

  assertBounds(computeBounds(makeBundle([obj])), 0, 0, 0, 1, 1, 1, "initial");

  // Same array identity, no revision: the cached influence boxes stay valid.
  // Only positions[3] changes, so y/z maxima remain 1.
  positions[3] = 2;
  assertBounds(computeBounds(makeBundle([obj])), 0, 0, 0, 1, 1, 1, "cached without revision");

  // vertices.revision bump invalidates: vertex1 is now (2, 1, 1).
  vertices.revision = 1;
  assertBounds(computeBounds(makeBundle([obj])), 0, 0, 0, 2, 1, 1, "vertices.revision invalidates");

  // object.geometryRevision takes precedence and invalidates again.
  positions[3] = 3;
  obj.geometryRevision = 7;
  assertBounds(computeBounds(makeBundle([obj])), 0, 0, 0, 3, 1, 1, "geometryRevision invalidates");
});

test("malformed skinned shapes are skipped instead of poisoning bounds", () => {
  const goodSoup = new Float32Array([0, 0, 0, 1, 1, 1]);
  const okVertices = () => makeVertices(
    new Float32Array([0, 0, 0, 1, 1, 1]),
    [[1, 0, 0, 0], [1, 0, 0, 0]],
    [[0, 0, 0, 0], [0, 0, 0, 0]]
  );
  const badPositions = okVertices();
  badPositions.positions = new Float32Array([NaN, 0, 0, 1, Infinity, 1]);
  const outOfRange = makeVertices(new Float32Array([8, 8, 8]), [[1, 0, 0, 0]], [[5, 0, 0, 0]]);
  const nanWeights = makeVertices(new Float32Array([8, 8, 8]), [[NaN, 0, 0, 0]], [[0, 0, 0, 0]]);
  const negativeWeights = makeVertices(new Float32Array([8, 8, 8]), [[-0.5, 0, 0, 0]], [[0, 0, 0, 0]]);
  const zeroWeights = makeVertices(new Float32Array([8, 8, 8]), [[0, 0, 0, 0]], [[0, 0, 0, 0]]);

  const bounds = computeBounds(makeBundle([
    { id: "soup", castShadow: true, viewCulled: false, vertexOffset: 0, vertexCount: 2 },
    skinnedObject(badPositions, flatPalette(1)),                       // non-finite positions
    skinnedObject(outOfRange, flatPalette(1)),                         // joint index >= jointCount
    skinnedObject(nanWeights, flatPalette(1)),                         // NaN weight
    skinnedObject(negativeWeights, flatPalette(1)),                    // negative weight
    skinnedObject(zeroWeights, flatPalette(1)),                        // all-zero weights
    skinnedObject(okVertices(), flatPalette(0)),                       // empty palette
    { id: "no-skin", castShadow: true, viewCulled: false, directVertices: true, vertices: okVertices(), vertexCount: 2 },
    skinnedObject(okVertices(), flatPalette(1), { modelMatrix: null }), // missing model matrix
  ], goodSoup));

  assertBounds(bounds, 0, 0, 0, 1, 1, 1, "only the soup survives malformed casters");
});

test("skeletons beyond the GL 64-joint uniform cap are not truncated", () => {
  // Flat Float32Array(72*16): 72 full 16-float joint matrices.
  const palette = flatPalette(72);
  setJointTranslation(palette, 70, 3, 0, 0);
  const vertices = makeVertices(
    new Float32Array([0, 0, 0, 1, 0, 0]),
    [[1, 0, 0, 0], [1, 0, 0, 0]],
    [[70, 0, 0, 0], [70, 0, 0, 0]]
  );
  const bounds = computeBounds(makeBundle([skinnedObject(vertices, palette)]));
  assertBounds(bounds, 3, 0, 0, 4, 0, 0, "joint 70 honored past the GL cap");
});
