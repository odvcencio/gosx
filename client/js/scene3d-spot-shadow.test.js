"use strict";

// Authored SpotLight shadow slice. These tests execute the REAL production
// shared matrix builder (the spot dispatch inside sceneShadowLightSpaceMatrix
// plus the perspective spot builder), the REAL WebGPU depth conversion
// helper, and real WGSL/GLSL receiver source — inside a VM. No oracle
// reimplementation of the production algorithms is used.
//
// This is a SOURCE CONTRACT suite, not native pixel proof; browser proof is
// a separate follow-up. The production candidate-collection / render-path
// regression (invalid spots must not consume slots) runs against the real
// recording-GL mount harness in scene3d-shadow-budget.test.js.

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

// Spot builder + directional orthographic builder, verbatim.
function extractShadowMatrixSource() {
  return sliceBetween(readBootstrapSource("16c-scene-shared-pbr.ts"),
    "function sceneSpotShadowLightSpaceMatrix",
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
  for (const name of ["10-runtime-scene-utils.ts", "11-scene-math.ts"]) {
    runFragment(context, readBootstrapSource(name), name);
  }
  runFragment(context, extractShadowMatrixSource(),
    "16c-scene-shared-pbr.ts#spot+directional");
  return context;
}

function callBuild(context, light, bounds) {
  context.__light = light;
  context.__bounds = bounds;
  return vm.runInContext("sceneShadowLightSpaceMatrix(__light, __bounds)",
    context, { filename: "spot-matrix" });
}

// Plain JS matrix * point with explicit column-major indices.
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

// Authored spot at (0,3,0) pointing straight down, 30-degree outer
// half-angle (radians). The bounds corner (0,-5,0) lies 8 units along the
// light direction, so the geometry far plane is exactly 8; the documented
// near strategy gives near = clamp(8*0.001, 0.01, 0.1) = 0.01 — explicit
// constants derived by hand, not by calling the production helper.
const SPOT = {
  kind: "spot", id: "key-spot",
  x: 0, y: 3, z: 0,
  directionX: 0, directionY: -1, directionZ: 0,
  angle: Math.PI / 6, penumbra: 0, range: 0,
  castShadow: true, shadowBias: 0.005, shadowSize: 512,
};
const BOUNDS = { minX: -1, minY: -5, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 };
const SPOT_NEAR = 0.01;
const SPOT_FAR = 8;

test("spot perspective maps near/far planes to clip depth -1/+1 with w = forward depth", () => {
  const context = createContext();
  const m = callBuild(context, SPOT, BOUNDS);
  assert.ok(m, "valid spot builds a matrix");
  assert.equal(m.length, 16);
  for (const v of m) assert.ok(Number.isFinite(v), "all entries finite");

  const near = transformPoint(context, m, 0, 3 - SPOT_NEAR, 0);
  const far = transformPoint(context, m, 0, 3 - SPOT_FAR, 0);
  const mid = transformPoint(context, m, 0, 1, 0); // depth 2

  assert.ok(Math.abs(near[2] / near[3] - (-1)) < TOL,
    "near plane depth -1, got " + near[2] / near[3]);
  assert.ok(Math.abs(far[2] / far[3] - 1) < TOL,
    "far plane depth +1, got " + far[2] / far[3]);
  // w equals positive forward distance for in-cone points; the eye itself
  // has w = 0, never w > 0.
  assert.ok(Math.abs(mid[3] - 2) < TOL, "midpoint w is forward depth 2, got " + mid[3]);
  assert.ok(Math.abs(near[3] - SPOT_NEAR) < TOL, "near point w equals its distance");
  assert.ok(mid[2] / mid[3] < 1 - TOL && mid[2] / mid[3] > -1 + TOL,
    "in-cone depth strictly inside clip");
});

test("a caster and receiver on one forward ray share projected XY and order in depth", () => {
  const context = createContext();
  const m = callBuild(context, SPOT, BOUNDS);
  // Both points lie on the ray eye + t*(0.4, -1.6, -0.3); t = 1 and 2.25.
  const caster = transformPoint(context, m, 0.4, 1.4, -0.3);
  const receiver = transformPoint(context, m, 0.9, -0.6, -0.675);
  assert.ok(caster[3] > 0 && receiver[3] > 0, "both in front: w positive");
  assert.ok(Math.abs(receiver[0] / receiver[3] - caster[0] / caster[3]) < TOL,
    "same ray shares projected x");
  assert.ok(Math.abs(receiver[1] / receiver[3] - caster[1] / caster[3]) < TOL,
    "same ray shares projected y");
  assert.ok(receiver[2] / receiver[3] > caster[2] / caster[3] + TOL,
    "receiver behind caster is deeper");
});

test("a point at the eye projects to w=0 and behind the eye to w<0 (receiver rejects)", () => {
  const context = createContext();
  const m = callBuild(context, SPOT, BOUNDS);
  const eye = transformPoint(context, m, 0, 3, 0);
  assert.ok(Math.abs(eye[3]) < TOL, "eye w is 0, got " + eye[3]);
  const behind = transformPoint(context, m, 0, 4, 0);
  assert.ok(behind[3] < 0, "behind-eye w is negative, got " + behind[3]);
});

test("vertical spot cone scale is -cot(angle): forward x up = (-1,0,0) makes m[0] negative", () => {
  const context = createContext();
  const base = callBuild(context, SPOT, BOUNDS); // direction (0,-1,0)
  // forward (0,-1,0) cross up (0,0,1) = (-1,0,0), so the X row coefficient
  // is NEGATIVE cot(angle), not +cot(angle).
  assert.ok(Math.abs(base[0] + 1 / Math.tan(Math.PI / 6)) < TOL,
    "vertical spot m[0] is -cot(30deg), got " + base[0]);
  const wider = callBuild(context, { ...SPOT, angle: Math.PI / 4 }, BOUNDS);
  assert.ok(Math.abs(wider[0] + 1) < TOL,
    "45-degree cone m[0] is -1, got " + wider[0]);
});

test("direction updates re-aim the projection: an axis point stays centered", () => {
  const context = createContext();
  const mz = callBuild(context, { ...SPOT, directionX: 0, directionY: 0, directionZ: -1 }, BOUNDS);
  const ahead = transformPoint(context, mz, 0, 3, -2);
  assert.ok(ahead[3] > 0, "axis point in front");
  assert.ok(Math.abs(ahead[0] / ahead[3]) < TOL && Math.abs(ahead[1] / ahead[3]) < TOL,
    "axis point projects to center");
});

test("position update shifts a forward receiver laterally; only an axial move moves the eye", () => {
  const context = createContext();
  // Lateral move (x=2): a receiver under the OLD eye is still a forward
  // point of the moved light (w = 2 > 0, NOT behind). The cone scales the
  // lateral offset by cot(angle), so its projected x/w is
  // cot(30deg) * 2 / 2 = sqrt(3), independent of scene bounds; a receiver
  // under the NEW eye stays centered.
  const moved = callBuild(context, { ...SPOT, x: 2 }, BOUNDS);
  const underNewEye = transformPoint(context, moved, 2, 1, 0);
  const underOldEye = transformPoint(context, moved, 0, 1, 0);
  assert.ok(underNewEye[3] > 0 && underOldEye[3] > 0, "both receivers in front");
  assert.ok(Math.abs(underNewEye[0] / underNewEye[3]) < TOL,
    "receiver under the new eye centered");
  const cotHalfAngle = 1 / Math.tan(Math.PI / 6); // sqrt(3)
  assert.ok(Math.abs(underOldEye[0] / underOldEye[3] - cotHalfAngle) < TOL,
    "receiver under the old eye shifted by cot(angle)*2/2 = sqrt(3), got " +
    underOldEye[0] / underOldEye[3]);
  // Axial move (1 unit along the direction): the old eye IS behind now.
  const axial = callBuild(context, { ...SPOT, y: 2 }, BOUNDS);
  const oldEye = transformPoint(context, axial, 0, 3, 0);
  assert.ok(oldEye[3] < 0, "axial move puts the old eye behind, w = " + oldEye[3]);
  // And the sideways move did NOT put the old eye behind (w stays 0).
  const sidewaysOldEye = transformPoint(context, moved, 0, 3, 0);
  assert.ok(Math.abs(sidewaysOldEye[3]) < TOL,
    "sideways move leaves the old eye at w = 0, got " + sidewaysOldEye[3]);
});

test("finite positive Range bounds the far plane; nonpositive Range uses scene geometry", () => {
  const context = createContext();
  const bounded = callBuild(context, { ...SPOT, range: 3 }, BOUNDS);
  const atRange = transformPoint(context, bounded, 0, 0, 0);   // depth 3 = Range
  const inside = transformPoint(context, bounded, 0, 1.5, 0);  // depth 1.5
  assert.ok(Math.abs(atRange[2] / atRange[3] - 1) < TOL,
    "Range clamps the far plane, got " + atRange[2] / atRange[3]);
  assert.ok(inside[2] / inside[3] < 1 - TOL, "closer point strictly inside");

  const unbounded = callBuild(context, SPOT, BOUNDS); // Range 0
  const geometryFar = transformPoint(context, unbounded, 0, 3 - SPOT_FAR, 0);
  assert.ok(Math.abs(geometryFar[2] / geometryFar[3] - 1) < TOL,
    "unbounded far comes from scene bounds (8)");
  const beyond = transformPoint(context, unbounded, 0, -6, 0); // depth 9
  assert.ok(beyond[2] / beyond[3] > 1, "beyond geometry far clips");
});

test("very large and very small finite directions project identically to their normalized form", () => {
  const context = createContext();
  const normInv = 1 / 3; // normalized (1,2,-2)/3
  const expected = callBuild(context, { ...SPOT,
    directionX: normInv, directionY: 2 * normInv, directionZ: -2 * normInv }, BOUNDS);
  assert.ok(expected, "normalized direction builds");
  const huge = callBuild(context, { ...SPOT,
    directionX: 1e200, directionY: 2e200, directionZ: -2e200 }, BOUNDS);
  const tiny = callBuild(context, { ...SPOT,
    directionX: 1e-200, directionY: 2e-200, directionZ: -2e-200 }, BOUNDS);
  assert.ok(huge && tiny, "scaled finite directions are not overflow/underflow-rejected");
  for (const variant of [huge, tiny]) {
    for (let i = 0; i < 16; i++) {
      assert.ok(Math.abs(variant[i] - expected[i]) < 1e-5,
        "scaled direction equivalent at entry " + i);
    }
  }
});

test("zero and non-finite directions return null without re-aiming; vertical stays finite", () => {
  const context = createContext();
  const vertical = callBuild(context, SPOT, BOUNDS); // direction (0,-1,0)
  for (const v of vertical) assert.ok(Number.isFinite(v));
  assert.equal(callBuild(context, { ...SPOT, directionX: 0, directionY: 0, directionZ: 0 }, BOUNDS),
    null, "zero direction is never silently re-aimed");
  assert.equal(callBuild(context, { ...SPOT, directionX: NaN }, BOUNDS), null);
  assert.equal(callBuild(context, { ...SPOT, directionZ: Infinity }, BOUNDS), null);
  assert.equal(callBuild(context, { ...SPOT, x: Infinity }, BOUNDS), null);
});

test("empty/invalid cones return null; near-90-degree cones still project", () => {
  const context = createContext();
  assert.equal(callBuild(context, { ...SPOT, angle: 0 }, BOUNDS), null,
    "Angle=0 invents no cone");
  assert.equal(callBuild(context, { ...SPOT, angle: -0.3 }, BOUNDS), null);
  assert.equal(callBuild(context, { ...SPOT, angle: NaN }, BOUNDS), null);
  assert.equal(callBuild(context, { ...SPOT, angle: Math.PI / 2 }, BOUNDS), null,
    "half-angle 90 degrees makes tan(fov/2) diverge: one perspective map cannot cover it");
  assert.equal(callBuild(context, { ...SPOT, angle: 2 }, BOUNDS), null);
  // Conventional usable range: up to just under 90 degrees.
  const steep = callBuild(context, { ...SPOT, angle: 1.5533 }, BOUNDS); // 89 degrees
  assert.ok(steep, "89-degree cone still projects");
  for (const v of steep) assert.ok(Number.isFinite(v));
});

test("a receiver on the farthest scene corner is not falsely excluded by rounding", () => {
  const context = createContext();
  const m = callBuild(context, SPOT, BOUNDS); // far plane 8 from corner (0,-5,0)
  for (const p of [[0, -5, 0], [1, -5, 1]]) {
    const corner = transformPoint(context, m, p[0], p[1], p[2]);
    assert.ok(corner[3] > 0, "far corner in front");
    const ndcZ = corner[2] / corner[3];
    assert.ok(ndcZ <= 1.0001 && ndcZ >= 1 - TOL,
      "farthest corner reaches the far plane within rounding slack, got " + ndcZ);
  }
});

test("geometry inside the documented near plane is outside the volume (lit), not claimed covered", () => {
  const context = createContext();
  const m = callBuild(context, SPOT, BOUNDS); // near = clamp(8*0.001, 0.01, 0.1) = 0.01
  const tooClose = transformPoint(context, m, 0, 3 - 0.005, 0);
  assert.ok(tooClose[3] > 0, "still in front of the eye");
  assert.ok(tooClose[2] / tooClose[3] < -1,
    "inside the near plane projects beyond clip -1 and is rejected, got " +
    tooClose[2] / tooClose[3]);
});

test("matrix building does not mutate inputs and returns independent buffers", () => {
  const context = createContext();
  const light = { ...SPOT };
  const lightSnapshot = JSON.stringify(light);
  const boundsSnapshot = JSON.stringify(BOUNDS);
  const a = callBuild(context, light, BOUNDS);
  assert.equal(JSON.stringify(light), lightSnapshot, "authored light untouched");
  assert.equal(JSON.stringify(BOUNDS), boundsSnapshot, "bounds untouched");
  const b = callBuild(context, light, BOUNDS);
  assert.notEqual(a, b, "fresh buffer per call, no aliasing");
  a[0] = 1234;
  assert.notEqual(b[0], 1234, "mutating one result leaves the other intact");
});

test("directional lights keep the orthographic path: w=1 fit unchanged", () => {
  const context = createContext();
  const dir = { directionX: 0.7, directionY: -0.7, directionZ: -1 };
  const bounds = { minX: -1.5, minY: -1.1, minZ: -0.55, maxX: 1.5, maxY: 1.1, maxZ: 0.575 };
  const m = callBuild(context, dir, bounds);
  assert.ok(m, "directional still builds");
  const p = transformPoint(context, m, -0.55, 0.35, 0.5);
  assert.ok(Math.abs(p[3] - 1) < TOL, "orthographic w stays 1, got " + p[3]);
  assert.ok(Number.isFinite(p[2]), "depth finite");
});

test("a malformed spot projects to null — the documented no-slot signal", () => {
  const context = createContext();
  assert.equal(callBuild(context, { ...SPOT, angle: 0 }, BOUNDS), null);
  assert.equal(callBuild(context, { ...SPOT, directionX: 0, directionY: 0, directionZ: 0 }, BOUNDS), null);
  // The builder is stateless: a valid spot still builds after invalid tries.
  assert.ok(callBuild(context, SPOT, BOUNDS), "no poisoned state");
});

function extractDepthConversionHelper() {
  const source = readRuntimeSource("webgpu.ts");
  const start = source.indexOf("function sceneWebGPUShadowDepthMatrix");
  assert.ok(start >= 0, "sceneWebGPUShadowDepthMatrix located in webgpu.ts");
  const end = source.indexOf("\n    }", start);
  assert.ok(end > start, "end of sceneWebGPUShadowDepthMatrix located");
  return source.slice(start, end + "\n    }".length);
}

test("WebGPU depth remap of a spot perspective matrix reaches 0/1 depth with a nontrivial w row", () => {
  const context = createContext();
  runFragment(context, extractDepthConversionHelper(), "webgpu.ts#depth-conversion");
  const m = callBuild(context, SPOT, BOUNDS);
  context.__spot = m;
  const converted = vm.runInContext("sceneWebGPUShadowDepthMatrix(__spot)",
    context, { filename: "convert-spot" });
  const near = transformPoint(context, converted, 0, 3 - SPOT_NEAR, 0);
  const far = transformPoint(context, converted, 0, 3 - SPOT_FAR, 0);
  assert.ok(Math.abs(near[2] / near[3]) < TOL, "remapped near depth 0, got " + near[2] / near[3]);
  assert.ok(Math.abs(far[2] / far[3] - 1) < TOL, "remapped far depth 1, got " + far[2] / far[3]);
  assert.ok(Math.abs(near[3] - SPOT_NEAR) < TOL,
    "perspective w preserved through the remap (nontrivial w row), got " + near[3]);
});

// Source contracts only (NOT native pixel proof; browser proof is a
// follow-up): the WGSL receivers must guard w<=0 and out-of-volume BEFORE
// any compare sample, in both slots.
test("WGSL receivers guard w<=0 and out-of-volume before any compare sample", () => {
  const source = readRuntimeSource("webgpu.ts");
  for (const slot of [0, 1]) {
    const fn = sliceBetween(source, "fn shadowFactor" + slot + "(", "fn distributionGGX");
    assert.ok(fn.includes("if (lightSpacePos.w <= 0.0)"),
      "slot " + slot + " rejects w<=0 before the divide");
    assert.ok(fn.includes("if (!inside)"),
      "slot " + slot + " rejects outside the clip volume");
    assert.ok(fn.indexOf("textureSampleCompareLevel") > fn.indexOf("if (!inside)"),
      "slot " + slot + " samples only after both guards");
    assert.ok(!fn.includes("select(1.0, shadowVal / 4.0, inside)"),
      "slot " + slot + " no longer samples then discards");
  }
});

test("both backends apply shadows to spot and point lights (types 1, 2 and 3) via the slot index", () => {
  const glsl = readRuntimeSource("webgl.ts");
  const glslCondition = glsl.match(/u_receiveShadow && \(([^)]*)\)/);
  assert.ok(glslCondition, "GL receiveShadow gating condition located");
  assert.ok(["1", "2", "3"].every((t) => glslCondition[1].includes("lightType == " + t)),
    "GL receiveShadow requires light types 1, 2 and 3");
  const wgsl = readRuntimeSource("webgpu.ts");
  const wgslCondition = wgsl.match(/material\.receiveShadow != 0u && \(([^)]*)\)/);
  assert.ok(wgslCondition, "WebGPU receiveShadow gating condition located");
  assert.ok(["1u", "2u", "3u"].every((t) => wgslCondition[1].includes("lightType == " + t)),
    "WebGPU receiveShadow requires light types 1, 2 and 3");
});
