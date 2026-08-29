// Executes the REAL production frame source (webgpu.ts shadow-candidate
// admission block) inside a vm context with pure-JS stubs. No native GPU is
// claimed or exercised: `device` is a plain limits stub and every helper is a
// recording function. The point admission flag must be derived from the
// actually admitted candidates (never a copied .some/policy reimplementation).
var assert = require("assert/strict");
var fs = require("fs");
var path = require("path");
var vm = require("vm");
var test = require("node:test").test;

var prod = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
var START = "var lightArray = Array.isArray(bundle.lights)";
var MID = "var computeParticleRecords = updateComputeParticleSystems";
var END = "var elioSkinStats = updateElioSkinnedMeshes";
var startIdx = prod.indexOf(START);
var midIdx = prod.indexOf(MID, startIdx);
var endIdx = prod.indexOf(END, midIdx);
assert.ok(startIdx >= 0 && midIdx > startIdx && endIdx > midIdx, "markers present");
var src = prod.slice(startIdx, prod.indexOf("\n", endIdx));
// The block past the computeParticleRecords marker (morph/skin wiring) is
// included so the explicit flag passed to those calls can be observed.
assert.equal(src.indexOf(START), 0, "starts at lightArray marker");
assert.ok(src.indexOf(MID) > 0, "computeParticleRecords marker in range");
assert.ok(src.indexOf(END) > src.indexOf(MID), "ordered markers");
assert.ok(src.indexOf("includeOffscreenShadowCasters = false") < src.indexOf(MID),
  "flag derivation block precedes compute calls");

function makeSandbox(opts, bundle, device) {
  var calls = { sizePoint: [], sizeOther: [], bounds: [], cube: [],
    matrix: [], arena: [], morph: [], skin: [] };
  var sandbox = {
    bundle: bundle, device: device, encoder: {}, frameTimeSeconds: 0,
    sceneNumber: function (v, d) { return (typeof v === "number" && isFinite(v)) ? v : d; },
    resolvePointShadowSize: function (size) {
      calls.sizePoint.push(size);
      return opts.zeroPointSize ? 0 : 512;
    },
    resolveShadowSize: function (size) { calls.sizeOther.push(size); return size; },
    webGPUShadowComputeBounds: function (b, all) { calls.bounds.push(!!all); return { all: !!all }; },
    scenePointShadowFaceMatrices: function (light) {
      calls.cube.push(light.kind);
      return opts.invalidCube ? null : { faces: 6 };
    },
    sceneShadowLightSpaceMatrix: function (light) { calls.matrix.push(light.kind); return { ok: true }; },
    ensureShadowFrameBufferCapacity: function (slots) {
      calls.arena.push(slots);
      return !opts.arenaFailure;
    },
    updateComputeParticleSystems: function () { return Array.from(arguments); },
    updateComputedMorphMeshes: function (b, encoder, flag) { calls.morph.push(flag); return {}; },
    updateElioSkinnedMeshes: function (b, encoder, flag) { calls.skin.push(flag); return {}; }
  };
  sandbox.__calls = calls;
  return sandbox;
}

function run(lights, opts) {
  opts = opts || {};
  var sandbox = makeSandbox(opts, {
    lights: lights, meshObjects: [{}, {}, {}],
    computeParticles: [], shadowMaxPixels: 0
  }, { limits: { maxTextureDimension2D: 8192 } });
  vm.runInContext(src, vm.createContext(sandbox), { filename: "frame-admission.js" });
  return sandbox;
}

var dir = { kind: "directional", castShadow: true, shadowSize: 1024 };
var point = { kind: "point", castShadow: true, shadowSize: 1024 };

test("point light admitted in slot 0 wires flag", function () {
  var r = run([point]);
  assert.equal(r.includeOffscreenShadowCasters, true);
  assert.deepEqual(Array.from(r.__calls.cube), ["point"]);
  assert.deepEqual(Array.from(r.__calls.arena), [24]); // (3+1) slots x 6 passes
  assert.deepEqual(r.__calls.morph, [true]);
  assert.deepEqual(r.__calls.skin, [true]);
});

test("point in slot 1 after directional still wires flag", function () {
  var r = run([dir, point]);
  assert.equal(r.includeOffscreenShadowCasters, true);
  assert.deepEqual(Array.from(r.__calls.matrix), ["directional"]);
  assert.deepEqual(Array.from(r.__calls.cube), ["point"]);
  assert.deepEqual(Array.from(r.__calls.arena), [28]); // (3+1) x (1+6)
  assert.deepEqual(r.__calls.skin, [true]);
});

test("directional only leaves flag false", function () {
  var r = run([dir]);
  assert.equal(r.includeOffscreenShadowCasters, false);
  assert.equal(r.__calls.cube.length, 0);
  assert.deepEqual(r.__calls.morph, [false]);
  assert.deepEqual(r.__calls.skin, [false]);
});

test("no lights => false, no arena", function () {
  var r = run([]);
  assert.equal(r.includeOffscreenShadowCasters, false);
  assert.equal(r.__calls.arena.length, 0);
});

test("castShadow=false point never reaches size port", function () {
  var r = run([{ kind: "point", castShadow: false }]);
  assert.equal(r.includeOffscreenShadowCasters, false);
  assert.equal(r.__calls.sizePoint.length, 0);
  assert.equal(r.__calls.cube.length, 0);
  assert.equal(r.__calls.arena.length, 0);
});

test("zero-size point skipped before cube port", function () {
  var r = run([point], { zeroPointSize: true });
  assert.equal(r.includeOffscreenShadowCasters, false);
  assert.deepEqual(r.__calls.sizePoint, [1024]);
  assert.equal(r.__calls.cube.length, 0);
  assert.equal(r.__calls.arena.length, 0);
});

test("invalid point cube rejected at cube port", function () {
  var r = run([point], { invalidCube: true });
  assert.equal(r.includeOffscreenShadowCasters, false);
  assert.deepEqual(Array.from(r.__calls.cube), ["point"]);
  assert.equal(r.__calls.arena.length, 0);
});

test("two non-point lights crowd out the point", function () {
  var r = run([dir, dir, point]);
  assert.equal(r.includeOffscreenShadowCasters, false);
  assert.equal(r.__calls.matrix.length, 2);
  assert.equal(r.__calls.cube.length, 0);
  assert.deepEqual(Array.from(r.__calls.arena), [8]); // (3+1) x 2 passes
});

test("arena failure clears admitted candidates", function () {
  var r = run([point], { arenaFailure: true });
  assert.equal(r.includeOffscreenShadowCasters, false);
  assert.deepEqual(Array.from(r.__calls.arena), [24]);
  assert.deepEqual(Array.from(r.__calls.cube), ["point"]);
});
