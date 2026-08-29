const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

const srcDir = path.join(__dirname, "bootstrap-src");

function readBootstrapSource(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

function sliceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  assert.ok(start >= 0, "start marker located: " + startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(end > start, "end marker located after start: " + endMarker);
  return source.slice(start, end);
}

function runFragment(context, source, filename) {
  vm.runInContext(source, context, { filename });
}

// Point cube builder, verbatim from 16c-scene-shared-pbr.ts.
function extractPointShadowSource() {
  return sliceBetween(readBootstrapSource("16c-scene-shared-pbr.ts"),
    "function scenePointShadowFaceMatrices",
    "function sceneSpotShadowLightSpaceMatrix");
}

// Point atlas size resolver, verbatim from 15a-scene-postfx-shared.ts.
function extractPointShadowSizeSource() {
  return sliceBetween(readBootstrapSource("15a-scene-postfx-shared.ts"),
    "function resolvePointShadowSize",
    "function sceneFiniteNumber");
}

function createContext() {
  const context = vm.createContext({
    console,
    Math,
    Number,
    isFinite,
    Float32Array,
  });
  for (const name of ["10-runtime-scene-utils.ts", "11-scene-math.ts"]) {
    runFragment(context, readBootstrapSource(name), name);
  }
  runFragment(context, extractPointShadowSource(), "16c-scene-shared-pbr.ts#point-cube");
  runFragment(context, extractPointShadowSizeSource(), "15a-scene-postfx-shared.ts#point-size");
  return context;
}

function callFaces(context, light, bounds) {
  context.__light = light;
  context.__bounds = bounds;
  return vm.runInContext("scenePointShadowFaceMatrices(__light, __bounds)",
    context, { filename: "point-matrix" });
}

function callSize(context, requested, cap, maxDim) {
  context.__requested = requested;
  context.__cap = cap;
  context.__maxDim = maxDim;
  return vm.runInContext("resolvePointShadowSize(__requested, __cap, __maxDim)",
    context, { filename: "point-size" });
}

// Plain JS matrix * point with explicit column-major indices.
function transformPoint(matrix, x, y, z) {
  return [
    matrix[0] * x + matrix[4] * y + matrix[8] * z + matrix[12],
    matrix[1] * x + matrix[5] * y + matrix[9] * z + matrix[13],
    matrix[2] * x + matrix[6] * y + matrix[10] * z + matrix[14],
    matrix[3] * x + matrix[7] * y + matrix[11] * z + matrix[15],
  ];
}

const TOL = 1e-6;

// Fixed cube face order +X, -X, +Y, -Y, +Z, -Z.
const FACES = [
  [1, 0, 0], [-1, 0, 0],
  [0, 1, 0], [0, -1, 0],
  [0, 0, 1], [0, 0, -1],
];

// Explicit right/up bases per face (right = forward cross up; up = +Y
// on X/Z faces, +Z on vertical faces). Hand-derived, NOT computed via
// cross() so the test stays independent of production conventions.
const RIGHTS = [
  [0, 0, 1], [0, 0, -1],
  [1, 0, 0], [-1, 0, 0],
  [-1, 0, 0], [1, 0, 0],
];
const UPS = [
  [0, 1, 0], [0, 1, 0],
  [0, 0, 1], [0, 0, 1],
  [0, 1, 0], [0, 1, 0],
];

// Light at the origin inside asymmetric bounds: far = max axis span = 6,
// near = clamp(6 * 0.001, 0.01, 0.1) = 0.01 — hand-derived constants.
const LIGHT = {
  kind: "point", id: "cube", x: 0, y: 0, z: 0,
  range: 0, castShadow: true, shadowBias: 0.005, shadowSize: 1024,
};
const BOUNDS = { minX: -4, minY: -3, minZ: -2, maxX: 2, maxY: 5, maxZ: 6 };
const NEAR = 0.01;
const FAR = 6;

test("six faces share one near/far and project forward axes with positive w", () => {
  const context = createContext();
  const result = callFaces(context, LIGHT, BOUNDS);
  assert.ok(result, "valid point light builds a cube");
  assert.equal(result.matrices.length, 6);
  assert.deepEqual(Array.from(result.position), [0, 0, 0]);
  assert.ok(Math.abs(result.near - NEAR) < TOL, "near = " + result.near);
  assert.ok(Math.abs(result.far - FAR) < TOL, "far = " + result.far);

  for (let i = 0; i < 6; i++) {
    const m = result.matrices[i];
    assert.equal(m.length, 16);
    for (const v of m) assert.ok(Number.isFinite(v), "all entries finite");
    const f = FACES[i];

    // A point in front of this face gets w = forward distance.
    const front = transformPoint(m, f[0] * 2, f[1] * 2, f[2] * 2);
    assert.ok(Math.abs(front[3] - 2) < TOL, "front w = forward distance");
    assert.ok(front[3] > 0, "front w positive");

    // The eye itself maps to w = 0; the opposite direction is behind.
    const eye = transformPoint(m, 0, 0, 0);
    assert.ok(Math.abs(eye[3]) < TOL, "eye w = 0");
    const behind = transformPoint(m, -f[0] * 2, -f[1] * 2, -f[2] * 2);
    assert.ok(Math.abs(behind[3] + 2) < TOL, "opposite w = -distance");
    assert.ok(behind[3] < 0, "opposite direction w negative");

    // The one common near/far pair maps clip depth -1/+1 on every face.
    const nearPt = transformPoint(m, f[0] * NEAR, f[1] * NEAR, f[2] * NEAR);
    assert.ok(Math.abs(nearPt[2] / nearPt[3] + 1) < TOL, "near maps to -1");
    const farPt = transformPoint(m, f[0] * FAR, f[1] * FAR, f[2] * FAR);
    assert.ok(Math.abs(farPt[2] / farPt[3] - 1) < TOL, "far maps to +1");

    // Two points on one forward ray share projected XY and order in depth.
    const a = transformPoint(m, f[0] * 1, f[1] * 1, f[2] * 1);
    const b = transformPoint(m, f[0] * 4, f[1] * 4, f[2] * 4);
    assert.ok(Math.abs(a[0] / a[3] - b[0] / b[3]) < TOL, "shared x on ray");
    assert.ok(Math.abs(a[1] / a[3] - b[1] / b[3]) < TOL, "shared y on ray");
    assert.ok(a[3] < b[3], "depth orders along the ray");
  }

  // Pure forward axes project to the face center.
  for (let i = 0; i < 6; i++) {
    const f = FACES[i];
    const m = Array.from(result.matrices[i]);
    const center = transformPoint(m, f[0] * 3, f[1] * 3, f[2] * 3);
    assert.ok(Math.abs(center[0] / center[3]) < TOL, "face " + i + " forward axis centers x");
    assert.ok(Math.abs(center[1] / center[3]) < TOL, "face " + i + " forward axis centers y");
  }
});

test("off-axis face orientation matches explicit expected right/up axes", () => {
  const context = createContext();
  const result = callFaces(context, LIGHT, BOUNDS);
  for (let i = 0; i < 6; i++) {
    const f = FACES[i];
    const r = RIGHTS[i];
    const u = UPS[i];
    const m = Array.from(result.matrices[i]);
    // Pure right/up vectors lie on the eye plane (w = 0), so combine
    // them with 2 * forward: w = 2 and NDC offsets are +/- 0.25.
    const atRight = transformPoint(m,
      2 * f[0] + 0.5 * r[0], 2 * f[1] + 0.5 * r[1], 2 * f[2] + 0.5 * r[2]);
    const atLeft = transformPoint(m,
      2 * f[0] - 0.5 * r[0], 2 * f[1] - 0.5 * r[1], 2 * f[2] - 0.5 * r[2]);
    const atUp = transformPoint(m,
      2 * f[0] + 0.5 * u[0], 2 * f[1] + 0.5 * u[1], 2 * f[2] + 0.5 * u[2]);
    assert.ok(Math.abs(atRight[3] - 2) < TOL, "face " + i + " right probe w = 2");
    assert.ok(Math.abs(atRight[0] / atRight[3] - 0.25) < TOL,
      "face " + i + " right axis projects NDC x +0.25");
    assert.ok(Math.abs(atLeft[0] / atLeft[3] + 0.25) < TOL,
      "face " + i + " opposite right axis projects NDC x -0.25");
    assert.ok(Math.abs(atUp[1] / atUp[3] - 0.25) < TOL,
      "face " + i + " up axis projects NDC y +0.25");
  }
});

test("translated light keeps eye/near/far semantics and off-axis projections on all faces", () => {
  const context = createContext();
  const light = { ...LIGHT, x: 2, y: -3, z: 5 };
  const bounds = { minX: -2, minY: -6, minZ: 3, maxX: 4, maxY: 2, maxZ: 11 };
  const result = callFaces(context, light, bounds);
  assert.ok(result, "translated point light builds a cube");
  assert.deepEqual(Array.from(result.position), [2, -3, 5]);
  assert.ok(Math.abs(result.near - NEAR) < TOL, "translated near unchanged");
  assert.ok(Math.abs(result.far - FAR) < TOL, "translated far unchanged");
  for (let i = 0; i < 6; i++) {
    const f = FACES[i];
    const r = RIGHTS[i];
    const u = UPS[i];
    const m = Array.from(result.matrices[i]);
    const lx = 2, ly = -3, lz = 5;
    const eye = transformPoint(m, lx, ly, lz);
    assert.ok(Math.abs(eye[3]) < TOL, "face " + i + " eye w = 0");
    const nearPt = transformPoint(m,
      lx + f[0] * NEAR, ly + f[1] * NEAR, lz + f[2] * NEAR);
    // A translated origin loses float32 precision when the matrix rows are
    // applied in JS (translation-cancellation); the independently measured
    // worst-case near NDC error across all six faces at light (2,-3,5),
    // near 0.01 is 1.49e-5, so allow 1e-4 here. All other geometry stays at TOL.
    assert.ok(Math.abs(nearPt[2] / nearPt[3] + 1) < 1e-4, "face " + i + " near maps to -1");
    const farPt = transformPoint(m,
      lx + f[0] * FAR, ly + f[1] * FAR, lz + f[2] * FAR);
    assert.ok(Math.abs(farPt[2] / farPt[3] - 1) < TOL, "face " + i + " far maps to +1");
    const obliqueA = transformPoint(m,
      lx + 1 * f[0] + 0.25 * r[0], ly + 1 * f[1] + 0.25 * r[1], lz + 1 * f[2] + 0.25 * r[2]);
    const obliqueB = transformPoint(m,
      lx + 4 * f[0] + 1 * r[0], ly + 4 * f[1] + 1 * r[1], lz + 4 * f[2] + 1 * r[2]);
    assert.ok(Math.abs(obliqueA[0] / obliqueA[3] - obliqueB[0] / obliqueB[3]) < TOL,
      "face " + i + " oblique ray shares x");
    assert.ok(Math.abs(obliqueA[1] / obliqueA[3] - obliqueB[1] / obliqueB[3]) < TOL,
      "face " + i + " oblique ray shares y");
    assert.ok(obliqueA[3] < obliqueB[3], "face " + i + " oblique w orders");
    assert.ok(obliqueA[2] / obliqueA[3] < obliqueB[2] / obliqueB[3],
      "face " + i + " oblique NDC z/w depth orders");
    const atRight = transformPoint(m,
      lx + 2 * f[0] + 0.5 * r[0], ly + 2 * f[1] + 0.5 * r[1], lz + 2 * f[2] + 0.5 * r[2]);
    assert.ok(Math.abs(atRight[0] / atRight[3] - 0.25) < TOL,
      "face " + i + " translated right projects +0.25");
    const atUp = transformPoint(m,
      lx + 2 * f[0] + 0.5 * u[0], ly + 2 * f[1] + 0.5 * u[1], lz + 2 * f[2] + 0.5 * u[2]);
    assert.ok(Math.abs(atUp[1] / atUp[3] - 0.25) < TOL,
      "face " + i + " translated up projects +0.25");
  }
});

test("inputs are never mutated and face matrices never alias", () => {
  const context = createContext();
  const light = { ...LIGHT };
  const bounds = { ...BOUNDS };
  const result = callFaces(context, light, bounds);
  assert.deepEqual(light, LIGHT, "light unmutated");
  assert.deepEqual(bounds, BOUNDS, "bounds unmutated");
  assert.equal(new Set(result.matrices).size, 6, "six distinct matrices");
  const snapshots = result.matrices.map((m) => Array.from(m));
  result.matrices[0][0] = 12345;
  for (let i = 1; i < 6; i++) {
    assert.equal(result.matrices[i][0], snapshots[i][0], "matrix " + i + " unaffected");
  }
});

test("malformed positions are rejected", () => {
  const context = createContext();
  assert.equal(callFaces(context, { ...LIGHT, x: NaN }, BOUNDS), null);
  assert.equal(callFaces(context, { ...LIGHT, y: Infinity }, BOUNDS), null);
  assert.equal(callFaces(context, { ...LIGHT, z: -Infinity }, BOUNDS), null);
  assert.equal(callFaces(context, { x: 0, y: 0 }, BOUNDS), null);
});

test("finite positive Range caps the far plane", () => {
  const context = createContext();
  const wide = callFaces(context, { ...LIGHT, range: 5 }, { minX: -100, minY: -100, minZ: -100, maxX: 100, maxY: 100, maxZ: 100 });
  assert.ok(wide, "capped cube builds");
  assert.ok(Math.abs(wide.far - 5) < TOL, "far capped to range");
  assert.ok(Math.abs(wide.near - 0.01) < TOL, "near clamps to floor");

  // Tiny range: clamp collapses past the far plane, so near falls back
  // to far * 0.5 and stays strictly inside the frustum.
  const tiny = callFaces(context, { ...LIGHT, range: 0.004 }, { minX: -100, minY: -100, minZ: -100, maxX: 100, maxY: 100, maxZ: 100 });
  assert.ok(tiny, "tiny-range cube builds");
  assert.ok(Math.abs(tiny.far - 0.004) < TOL, "far = tiny range");
  assert.ok(Math.abs(tiny.near - 0.002) < TOL, "near falls back to far/2");
  for (const m of tiny.matrices) {
    for (const v of m) assert.ok(Number.isFinite(v), "tiny-range entries finite");
  }
});

test("huge extents are rejected, tiny coordinates stay finite", () => {
  const context = createContext();
  const hugeBounds = { minX: -1e308, minY: -1e308, minZ: -1e308, maxX: 1e308, maxY: 1e308, maxZ: 1e308 };
  assert.equal(callFaces(context, { ...LIGHT, x: 1e308, y: 0, z: 0 }, hugeBounds), null,
    "overflowing extent rejects the cube");
  const tinyResult = callFaces(context, { ...LIGHT, x: 5e-324, y: 0, z: 0 }, null);
  assert.ok(tinyResult, "subnormal position with no bounds still builds");
  assert.ok(Math.abs(tinyResult.far - 10) < TOL, "absent bounds fallback far = 10");
  for (const m of tinyResult.matrices) {
    for (const v of m) assert.ok(Number.isFinite(v), "tiny-coordinate entries finite");
  }
});

test("float32-unrepresentable projections reject, safe tiny positions build", () => {
  const context = createContext();
  // A null light has no position; reject instead of reading undefined fields.
  assert.equal(callFaces(context, null, BOUNDS), null, "null light rejects");
  // range 1e-50 underflows float32 when clamped into the near/far uniforms,
  // so the pair could never be uploaded; reject like other float32 hazards.
  assert.equal(
    callFaces(context, { ...LIGHT, range: 1e-50 }, BOUNDS),
    null,
    "range underflowing float32 rejects the cube");
  // far = 2e300 is finite in float64 but overflows float32; the pair
  // could never be uploaded as receiver uniforms, so reject.
  assert.equal(
    callFaces(context, { ...LIGHT, x: 1e300 },
      { minX: -1e300, minY: -1e300, minZ: -1e300, maxX: 1e300, maxY: 1e300, maxZ: 1e300 }),
    null,
    "far beyond float32 rejects the cube");
  // A subnormal position with ordinary projection is still safe.
  const tinyResult = callFaces(context, { ...LIGHT, x: 5e-324 }, null);
  assert.ok(tinyResult, "subnormal position with ordinary projection builds");
  assert.ok(Math.abs(tinyResult.far - 10) < TOL, "fallback far = 10");
  assert.ok(tinyResult.near > 0 && tinyResult.near < tinyResult.far,
    "near stays positive and strictly below far");
});

test("atlas size honors total pixel budget and device limits", () => {
  const context = createContext();
  // Default cap 1048576 => per-face budget floor(sqrt(cap/6)) = 418.
  assert.equal(callSize(context, 256, undefined, undefined), 256, "small request passes");
  assert.equal(callSize(context, 4096, undefined, undefined), 418, "default cap scales down");
  assert.equal(callSize(context, 100, undefined, undefined), 256, "renderer clamp lifts to 256");
  assert.equal(callSize(context, NaN, undefined, undefined), 418, "non-finite request defaults");
  assert.equal(callSize(context, 0, undefined, undefined), 256, "finite zero request clamps like renderer");
  assert.equal(callSize(context, -512, undefined, undefined), 256, "finite negative request clamps like renderer");
  // Explicit cap 6 * 512^2 => 512 per face.
  assert.equal(callSize(context, 2048, 6 * 512 * 512, undefined), 512, "explicit cap honored");
  // Device dimension limit: atlas width 3*S must fit.
  assert.equal(callSize(context, 2048, undefined, 3072), 418, "budget binds before device");
  assert.equal(callSize(context, 2048, undefined, 600), 200, "device limit floor(dim/3) binds");
  // Positive caps are never silently exceeded.
  assert.equal(callSize(context, 1024, 5, undefined), 0, "cap under 6 pixels disables");
  assert.equal(callSize(context, 1024, undefined, 2), 0, "dimension under 3 disables");
  // By the established contract a non-positive pixel cap means "no explicit
  // cap", so it resolves to the default 1048576 (same as undefined), while a
  // finite non-positive device dimension still disables outright.
  assert.equal(callSize(context, 1024, 0, undefined), 418, "zero cap defaults to budget cap");
  assert.equal(callSize(context, 1024, -1, undefined), 418, "negative cap defaults to budget cap");
  assert.equal(callSize(context, 1024, undefined, 0), 0, "zero device dimension disables");
  assert.equal(callSize(context, 1024, undefined, -6), 0, "negative device dimension disables");
  // Minimum viable cap yields the smallest face, and every resolved
  // size obeys the total pixel budget 6*S*S <= cap.
  assert.equal(callSize(context, 4096, 6, undefined), 1, "six-pixel cap yields S = 1");
  for (const cap of [6, 100, 6 * 511 * 511, 6 * 512 * 512]) {
    const s = callSize(context, 4096, cap, undefined);
    assert.ok(s >= 1, "cap " + cap + " yields a positive face size");
    assert.ok(6 * s * s <= cap, "cap " + cap + " budget holds: 6*" + s + "^2 <= " + cap);
  }
});
