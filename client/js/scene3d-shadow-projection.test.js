"use strict";

// Shadow projection defect fix. These tests execute the actual production
// shared light-space matrix builder (extracted as a small fragment from the
// shared PBR bootstrap) and the actual WebGPU depth conversion helper inside
// a VM, using explicit geometry points/corners. No oracle reimplementation of
// the production algorithms is used.

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const srcDir = path.join(__dirname, "bootstrap-src");

function readBootstrapSource(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

function readRuntimeSource(name) {
  return fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", name), "utf8");
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

// Extract the ACTUAL sceneShadowLightSpaceMatrix from the shared PBR
// bootstrap (not the whole core, which pulls missing dependencies).
function extractLightSpaceMatrixSource() {
  return sliceBetween(readBootstrapSource("16c-scene-shared-pbr.ts"),
    "function sceneShadowLightSpaceMatrix",
    "// Compute the AABB of all objects in the bundle.");
}

function createContext() {
  const context = vm.createContext({
    console,
    Math,
    Number,
    isFinite,
    Float32Array,
  });
  // sceneNumber and sceneMat4Multiply come from the real scene utility/math
  // fragments; the light-space builder itself comes from the shared PBR file.
  for (const name of ["10-runtime-scene-utils.ts", "11-scene-math.ts"]) {
    runFragment(context, readBootstrapSource(name), name);
  }
  runFragment(context, extractLightSpaceMatrixSource(),
    "16c-scene-shared-pbr.ts#sceneShadowLightSpaceMatrix");
  return context;
}

// Extract the actual production WebGPU depth conversion helper (and the
// actual WGSL shadowProjectedCoords text) from the runtime source.
function extractDepthConversionHelper() {
  const source = readRuntimeSource("webgpu.ts");
  const start = source.indexOf("function sceneWebGPUShadowDepthMatrix");
  assert.ok(start >= 0, "sceneWebGPUShadowDepthMatrix located in webgpu.ts");
  const end = source.indexOf("\n    }", start);
  assert.ok(end > start, "end of sceneWebGPUShadowDepthMatrix located");
  return source.slice(start, end + "\n    }".length);
}

function extractShadowProjectedCoordsWGSL() {
  return sliceBetween(readRuntimeSource("webgpu.ts"),
    "fn shadowProjectedCoords", "fn shadowFactor0");
}

// Plain JS matrix * point diagnostic with explicit column-major indices; the
// matrix is passed in directly, never via a side-channel context reference.
function transformPoint(context, matrix, x, y, z) {
  const m = JSON.stringify(Array.from(matrix));
  return vm.runInContext(
    "(function(m, x, y, z) { return [" +
    "m[0]*x + m[4]*y + m[8]*z + m[12]," +
    "m[1]*x + m[5]*y + m[9]*z + m[13]," +
    "m[2]*x + m[6]*y + m[10]*z + m[14]," +
    "m[3]*x + m[7]*y + m[11]*z + m[15]" +
    "]; })(" + m + ", " + x + ", " + y + ", " + z + ")",
    context, { filename: "transform-point" });
}

const TOL = 1e-4;

// Native baseline geometry: directional light (0.7, -0.7, -1) over the AABB
// [-1.5,-1.1,-0.55]..[1.5,1.1,0.575], with an explicit caster and receiver
// on the same light ray.
const BASE_LIGHT = { directionX: 0.7, directionY: -0.7, directionZ: -1 };
const BASE_BOUNDS = { minX: -1.5, minY: -1.1, minZ: -0.55, maxX: 1.5, maxY: 1.1, maxZ: 0.575 };
const CASTER = [-0.55, 0.35, 0.5];
const RECEIVER = [0.115, -0.315, -0.45];

test("shared light-space matrix projects caster, receiver and all 8 AABB corners inside clip", () => {
  const context = createContext();
  context.BASE_LIGHT = BASE_LIGHT;
  context.BASE_BOUNDS = BASE_BOUNDS;
  const matrix = vm.runInContext(
    "sceneShadowLightSpaceMatrix(BASE_LIGHT, BASE_BOUNDS)",
    context, { filename: "shared-matrix" });
  assert.equal(matrix.length, 16);

  const corners = [];
  for (const x of [BASE_BOUNDS.minX, BASE_BOUNDS.maxX]) {
    for (const y of [BASE_BOUNDS.minY, BASE_BOUNDS.maxY]) {
      for (const z of [BASE_BOUNDS.minZ, BASE_BOUNDS.maxZ]) {
        corners.push(transformPoint(context, matrix, x, y, z));
      }
    }
  }
  assert.equal(corners.length, 8, "all 8 bounds angles projected");
  for (const p of corners.concat([
    transformPoint(context, matrix, CASTER[0], CASTER[1], CASTER[2]),
    transformPoint(context, matrix, RECEIVER[0], RECEIVER[1], RECEIVER[2]),
  ])) {
    assert.ok(p[3] > 0, "clip w positive");
    for (let i = 0; i < 3; i++) {
      assert.ok(Number.isFinite(p[i]), "clip component " + i + " finite");
      assert.ok(p[i] >= -1 - TOL && p[i] <= 1 + TOL,
        "clip component " + i + " inside [-1,1], got " + p[i]);
    }
  }
});

test("receiver behind caster on the same light ray gets greater depth and identical XY", () => {
  const context = createContext();
  context.BASE_LIGHT = BASE_LIGHT;
  context.BASE_BOUNDS = BASE_BOUNDS;
  const matrix = vm.runInContext(
    "sceneShadowLightSpaceMatrix(BASE_LIGHT, BASE_BOUNDS)",
    context, { filename: "shared-matrix" });

  const caster = transformPoint(context, matrix, CASTER[0], CASTER[1], CASTER[2]);
  const receiver = transformPoint(context, matrix, RECEIVER[0], RECEIVER[1], RECEIVER[2]);

  assert.ok(Number.isFinite(caster[2]) && Number.isFinite(receiver[2]),
    "depth ordering values finite");
  assert.ok(Math.abs(caster[3] - 1) < TOL, "orthographic w is 1 for caster");
  assert.ok(receiver[2] > caster[2] + TOL,
    "receiver depth " + receiver[2] + " greater than caster depth " + caster[2]);
  assert.ok(Math.abs(receiver[0] - caster[0]) < TOL &&
            Math.abs(receiver[1] - caster[1]) < TOL,
     "same light ray yields identical light-space XY");
});

test("zero direction falls back to finite matrix; vertical direction up choice stays finite", () => {
  const context = createContext();
  context.BASE_BOUNDS = BASE_BOUNDS;
  context.ZERO_DIR = { directionX: 0, directionY: 0, directionZ: 0 };
  context.VERTICAL_DIR = { directionX: 0, directionY: -1, directionZ: 0 };
  const zeroMatrix = vm.runInContext(
    "sceneShadowLightSpaceMatrix(ZERO_DIR, BASE_BOUNDS)",
    context, { filename: "zero-dir" });
  assert.equal(zeroMatrix.length, 16);
  for (const v of zeroMatrix) assert.ok(Number.isFinite(v), "zero-direction fallback finite");

  const verticalMatrix = vm.runInContext(
    "sceneShadowLightSpaceMatrix(VERTICAL_DIR, BASE_BOUNDS)",
    context, { filename: "vertical-dir" });
  for (const v of verticalMatrix) assert.ok(Number.isFinite(v), "vertical-direction matrix finite");
});

test("WebGPU depth conversion maps clip -1 to 0 and +1 to 1 via the actual converted matrix", () => {
  const context = createContext();
  runFragment(context, extractDepthConversionHelper(), "webgpu.ts#depth-conversion");

  // GL identity: clip z == input z, w == 1.
  const identity = new Float32Array(16);
  identity[0] = identity[5] = identity[10] = identity[15] = 1;
  context.identityRef = identity;
  const converted = vm.runInContext("sceneWebGPUShadowDepthMatrix(identityRef)",
    context, { filename: "converted-identity" });

  const nearPoint = transformPoint(context, converted, 0, 0, -1);
  const farPoint = transformPoint(context, converted, 0, 0, 1);
  assert.ok(Math.abs(nearPoint[2] - 0) < TOL,
    "clip z -1 maps to depth 0, got " + nearPoint[2]);
  assert.ok(Math.abs(nearPoint[3] - 1) < TOL,
    "clip w stays 1 at the near point, got " + nearPoint[3]);
  assert.ok(Math.abs(farPoint[2] - 1) < TOL,
    "clip z +1 maps to depth 1, got " + farPoint[2]);
  assert.ok(Math.abs(farPoint[3] - 1) < TOL,
    "clip w stays 1 at the far point, got " + farPoint[3]);

  // Non-constant w row: (3,0) = 0.5 makes w depend on x. With x=2, z=1 the
  // source clip values are z=1, w=2, so the converted matrix must produce
  // clip z 1.5, w 2, and NDC depth 0.75 — explicit expected constants, not a
  // re-derivation of the production formula.
  const wRow = new Float32Array(16);
  wRow[0] = wRow[5] = wRow[10] = wRow[15] = 1;
  wRow[3] = 0.5;
  context.wRowRef = wRow;
  const convertedW = vm.runInContext("sceneWebGPUShadowDepthMatrix(wRowRef)",
    context, { filename: "converted-wrow" });
  const p = transformPoint(context, convertedW, 2, 0, 1);
  assert.ok(Math.abs(p[2] - 1.5) < TOL,
    "converted clip z is 1.5, got " + p[2]);
  assert.ok(Math.abs(p[3] - 2) < TOL,
    "clip w is 2, got " + p[3]);
  assert.ok(Math.abs(p[2] / p[3] - 0.75) < TOL,
    "NDC depth is 0.75, got " + (p[2] / p[3]));
});

test("WebGPU depth conversion does not mutate its source and returns independent matrices", () => {
  const context = createContext();
  runFragment(context, extractDepthConversionHelper(), "webgpu.ts#depth-conversion");

  const source = new Float32Array(16);
  for (let i = 0; i < 16; i++) source[i] = i + 1;
  const snapshot = Float32Array.from(source);
  context.srcRef = source;
  const a = vm.runInContext("sceneWebGPUShadowDepthMatrix(srcRef)",
    context, { filename: "conv-a" });
  for (let i = 0; i < 16; i++) assert.equal(source[i], snapshot[i], "source unmutated at " + i);

  const b = vm.runInContext("sceneWebGPUShadowDepthMatrix(srcRef)",
    context, { filename: "conv-b" });
  assert.notEqual(a, b, "fresh buffer per call, no aliasing");
  a[2] = 999;
  assert.notEqual(b[2], 999, "mutating one result leaves the other intact");
});

test("WGSL shadowProjectedCoords maps XY to flipped UV and keeps Z direct", () => {
  const wgsl = extractShadowProjectedCoordsWGSL();
  assert.ok(wgsl.includes(
    "return vec3f(projCoords3.x * 0.5 + 0.5, 0.5 - projCoords3.y * 0.5, projCoords3.z);"),
    "single UV mapping with texture Y flip and direct Z");
  assert.ok(!wgsl.includes("projCoords3 * 0.5 + 0.5"),
    "no uniform GL-style 0.5+0.5 remap of all components");
});
