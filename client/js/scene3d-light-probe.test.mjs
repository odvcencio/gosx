// SH light-probe tests: sparse-coefficient validity, double-precision
// aggregation, hashing/snapshot invalidation, and the real WebGL
// scenePBRUploadLights against a recording GL stub.
//
// Everything under test runs the shipped sources in node:vm — the real
// 16c-scene-shared-pbr helpers, the real normalizer slice from
// 10-runtime-scene-core, and the real webgl.ts upload function. No
// duplicate implementations; the only prelude code is unrelated glue the
// sliced regions call but do not define themselves.

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

const coreSource = readSource("10-runtime-scene-core.ts");
const sharedPbrSource = readSource("16c-scene-shared-pbr.ts");
const webglSource = readSource("../runtime/scene3d/webgl.ts");
const webgpuSource = readSource("../runtime/scene3d/webgpu.ts");
const browserCheckerSource = fs.readFileSync(
  path.join(__dirname, "testdata", "scene3d-light-probe-browser.cjs"),
  "utf8",
);

function createContext() {
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math, JSON, Number, Object, Array, String, Boolean,
    Float32Array, Float64Array, Uint32Array, Uint8Array, Int32Array,
    ArrayBuffer, DataView, Map, Set, Error, Promise,
    isFinite, parseInt, parseFloat,
    performance: { now: () => 0 },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.__gosx_scene3d_api = {};
  const context = vm.createContext(sandbox);
  const prelude = `
    function sceneNumber(value, fallback) {
      var n = Number(value);
      return Number.isFinite(n) ? n : fallback;
    }
    function sceneBool(value, fallback) { return value == null ? fallback : !!value; }
    function sceneClamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }
    function sceneClampNumberOrCSSVar(value, fallback, lo, hi) {
      var n = Number(value);
      if (!Number.isFinite(n)) n = fallback;
      return Math.max(lo, Math.min(hi, n));
    }
    function sceneIsPlainObject(v) {
      return v != null && typeof v === "object" && !Array.isArray(v);
    }
    function sceneNormalizeLifecycle(item, current) {
      return { transition: item.transition, inState: item.inState, outState: item.outState, live: item.live };
    }
    function sceneCloneData(v) { return JSON.parse(JSON.stringify(v === undefined ? null : v)); }
  `;
  vm.runInContext(prelude, context, { filename: "prelude.js" });
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(sharedPbrSource, context, { filename: "16c-scene-shared-pbr.ts" });
  return context;
}

// The light normalizer region of 10-runtime-scene-core: from
// normalizeSceneLightKind through normalizeSceneLight (inclusive), which
// covers sceneDefaultLightIntensity and sceneSnapshotProbeCoefficients.
function loadNormalizer(context) {
  const kindStart = coreSource.indexOf("function normalizeSceneLightKind");
  assert.notEqual(kindStart, -1, "normalizeSceneLightKind moved; update this loader");
  const nextStart = coreSource.indexOf("function normalizeSceneLightKind", kindStart + 1);
  const start = nextStart === -1 ? kindStart : nextStart;
  const end = coreSource.indexOf("function normalizeSceneLabel(", start);
  assert.notEqual(end, -1, "normalizeSceneLabel moved; update this loader");
  return vm.runInContext(
    "(function() {\n" + coreSource.slice(start, end) + "\nreturn { normalizeSceneLight, sceneSnapshotProbeCoefficients };\n})()",
    context,
    { filename: "normalizer-slice.js" },
  );
}

// sceneApplyTransitionPatch (live restamp path) plus its trailing anchor.
function loadTransitionPatch(context) {
  const start = coreSource.indexOf("function sceneApplyTransitionPatch(");
  const end = coreSource.indexOf("function sceneInstantLiveBufferKeys(", start);
  assert.ok(start !== -1 && end !== -1, "sceneApplyTransitionPatch moved; update this loader");
  return vm.runInContext(
    "(function() {\n" + coreSource.slice(start, end) + "\nreturn sceneApplyTransitionPatch;\n})()",
    context,
    { filename: "transition-patch-slice.js" },
  );
}

// The real scenePBRLightsHash + scenePBRUploadLights from webgl.ts. The
// upload function ends after the fog-colour uniforms (the environment tail).
function loadWebGLUpload(context) {
  const start = webglSource.indexOf("function scenePBRLightsHash(");
  assert.notEqual(start, -1, "scenePBRLightsHash moved; update this loader");
  const fogAnchor = webglSource.indexOf("uniforms.fogColor, fogColorRGBA[0], fogColorRGBA[1], fogColorRGBA[2]);", start);
  assert.notEqual(fogAnchor, -1, "the fog tail moved; update this loader");
  const body = webglSource.slice(start, fogAnchor + "uniforms.fogColor, fogColorRGBA[0], fogColorRGBA[1], fogColorRGBA[2]);".length) + "\n  }";
  return vm.runInContext(
    "(function() {\n" + body + "\nreturn { scenePBRLightsHash, scenePBRUploadLights };\n})()",
    context,
    { filename: "webgl-upload-slice.js" },
  );
}

// --- Shared helpers (16c) ---------------------------------------------------

function helpers() {
  const context = createContext();
  return {
    context,
    valid: vm.runInContext("scenePBRProbeCoefficientsValid", context),
    aggregate: vm.runInContext("scenePBRProbeAggregate", context),
    hashLightContent: vm.runInContext("hashLightContent", context),
    AGGREGATE_MAX: vm.runInContext("SCENE_PBR_PROBE_AGGREGATE_MAX", context),
  };
}

function nineCoefs(fill) {
  const out = [];
  for (let i = 0; i < 9; i++) out.push(fill(i));
  return out;
}

test("sparse coefficients are valid: absent components mean zero", () => {
  const { valid } = helpers();
  assert.equal(valid(nineCoefs(() => ({}))), true, "all-zero sparse entries");
  assert.equal(valid(nineCoefs((i) => (i === 0 ? { x: 1 } : {}))), true, "one colored channel");
  assert.equal(valid(nineCoefs((i) => (i === 4 ? { x: undefined, y: 2, z: undefined } : {}))), true);
});

test("present invalid components are rejected", () => {
  const { valid } = helpers();
  assert.equal(valid(nineCoefs(() => ({ x: null }))), false, "null is present, not absent");
  assert.equal(valid(nineCoefs(() => ({ x: "1" }))), false, "strings are not numbers");
  assert.equal(valid(nineCoefs(() => ({ y: NaN }))), false);
  assert.equal(valid(nineCoefs(() => ({ z: Infinity }))), false);
  assert.equal(valid(nineCoefs(() => ({ x: 1e39 }))), false, "beyond float32 range");
  assert.equal(valid(nineCoefs(() => [1, 2, 3])), false, "array entries are not Vector3");
  assert.equal(valid(nineCoefs(() => null)), false);
  assert.equal(valid([{ x: 1 }]), false, "wrong length");
  assert.equal(valid("nope"), false);
});

test("nine all-zero sparse coefficients aggregate to valid black, not malformed", () => {
  const { aggregate } = helpers();
  const out = new Float32Array(27);
  const census = aggregate([{ kind: "light-probe", intensity: 2, coefficients: nineCoefs(() => ({})) }], 1, out);
  assert.equal(census.valid, true);
  assert.equal(census.malformed, 0);
  for (const v of out) assert.equal(v, 0);
});

test("the aggregate multiplies intensity exactly once and ignores Color", () => {
  const { aggregate } = helpers();
  const out = new Float32Array(27);
  const coefficients = nineCoefs((i) => ({ x: i + 1, y: 0, z: -(i + 1) }));
  const census = aggregate(
    [{ kind: "light-probe", color: "#ff0000", intensity: 3, coefficients }],
    1, out,
  );
  assert.equal(census.valid, true);
  for (let i = 0; i < 9; i++) {
    assert.equal(out[i * 3], 3 * (i + 1), "x is coefficient * intensity, once");
    assert.equal(out[i * 3 + 1], 0);
    assert.equal(out[i * 3 + 2], -3 * (i + 1));
  }
});

test("opposing very large valid coefficients at intensity 6 cancel exactly and stay finite", () => {
  const { aggregate, AGGREGATE_MAX } = helpers();
  // 3e38 is a valid float32 component; 6 * 3e38 overflows f32 but the f64
  // accumulator holds it, so two opposing probes cancel to exact zero.
  const big = 3e38;
  const out = new Float32Array(27);
  const census = aggregate(
    [
      { kind: "light-probe", intensity: 6, coefficients: nineCoefs(() => ({ x: big, y: big, z: big })) },
      { kind: "light-probe", intensity: 6, coefficients: nineCoefs(() => ({ x: -big, y: -big, z: -big })) },
    ],
    2, out,
  );
  assert.equal(census.valid, true);
  for (const v of out) {
    assert.equal(v, 0, "f64 accumulation must cancel +MAX/-MAX to exact zero");
  }
  // A single over-range probe clamps to the documented rendering bound.
  const single = new Float32Array(27);
  aggregate([{ kind: "light-probe", intensity: 6, coefficients: nineCoefs(() => ({ x: 1e30, y: 0, z: 0 })) }], 1, single);
  for (let i = 0; i < 9; i++) assert.equal(single[i * 3], Math.fround(AGGREGATE_MAX));
  for (const v of single) assert.ok(Number.isFinite(v), "the upload must never carry Infinity or NaN");
});

test("the shared aggregate honors the 256-light prefix and excludes light 257", () => {
  const { aggregate } = helpers();
  const lights = [];
  for (let i = 0; i < 257; i++) {
    lights.push({ kind: "light-probe", intensity: 1, coefficients: nineCoefs(() => ({ x: 1 })) });
  }
  const prefix = new Float32Array(27);
  const census = aggregate(lights, 256, prefix);
  assert.equal(census.valid, true);
  assert.equal(prefix[0], 256, "light 257 is outside the shared prefix");
  const all = new Float32Array(27);
  aggregate(lights, 257, all);
  assert.equal(all[0], 257, "an explicit larger count is honored");
});

test("a malformed probe cannot contaminate the aggregate, and the scratch is cleared between calls", () => {
  const { aggregate } = helpers();
  const out = new Float32Array(27);
  aggregate(
    [{ kind: "light-probe", intensity: 1, coefficients: nineCoefs(() => ({ x: 5, y: 5, z: 5 })) }],
    1, out,
  );
  // Second call: only a malformed probe. Reused scratch must come back zeroed.
  const census = aggregate(
    [
      { kind: "light-probe", intensity: 1, coefficients: [{ x: 1 }] },
      { kind: "ambient", intensity: 99 },
    ],
    2, out,
  );
  assert.equal(census.valid, false);
  assert.equal(census.malformed, 1);
  for (const v of out) assert.equal(v, 0);
});

test("hashing treats omitted zero and explicit zero identically", () => {
  const { hashLightContent } = helpers();
  const base = { kind: "light-probe", intensity: 0.5, coefficients: null };
  const sparse = { ...base, coefficients: nineCoefs((i) => (i === 2 ? { x: 1 } : {})) };
  const explicit = { ...base, coefficients: nineCoefs((i) => (i === 2 ? { x: 1, y: 0, z: 0 } : { x: 0, y: 0, z: 0 })) };
  assert.equal(hashLightContent(sparse), hashLightContent(explicit));
  // A coefficient change invalidates the hash even with unchanged scalars.
  const changed = { ...explicit, coefficients: nineCoefs((i) => (i === 2 ? { x: 2, y: 0, z: 0 } : { x: 0, y: 0, z: 0 })) };
  assert.notEqual(hashLightContent(explicit), hashLightContent(changed));
  // Malformed sets hash by length + marker, not by content.
  assert.equal(
    hashLightContent({ ...base, coefficients: [{ x: 1 }] }),
    hashLightContent({ ...base, coefficients: [{ y: 2 }] }),
  );
});

// --- Normalizer snapshot + live restamp -------------------------------------

test("the normalizer snapshots coefficient components so in-place edits cannot stale the hash", () => {
  const context = createContext();
  const { normalizeSceneLight } = loadNormalizer(context);
  const coefs = nineCoefs(() => ({ x: 1, y: 0, z: 0 }));
  const light = normalizeSceneLight({ kind: "light-probe", coefficients: coefs }, 0, null);
  assert.equal(light._lightHash, hashLightContentOf(light));
  coefs[0].x = 7; // author mutates their Vector3 in place
  const again = normalizeSceneLight(
    { kind: "light-probe", coefficients: coefs }, 0, light,
  );
  assert.equal(again._lightHash, hashLightContentOf(again));
  assert.notEqual(light._lightHash, again._lightHash, "the edit must invalidate the hash");
  assert.equal(light.coefficients[0].x, 1, "the earlier snapshot is untouched by the source mutation");

  function hashLightContentOf(l) {
    return vm.runInContext("hashLightContent", context)(l);
  }
});

test("snapshot copies vector objects and preserves malformed entries as-is", () => {
  const context = createContext();
  const { sceneSnapshotProbeCoefficients } = loadNormalizer(context);
  const sparse = [{ x: 1 }];
  const snap = sceneSnapshotProbeCoefficients(sparse);
  assert.notEqual(snap[0], sparse[0], "vector objects must be copied, not aliased");
  assert.equal(snap[0].x, 1, "present x is preserved");
  assert.equal(snap[0].y, undefined, "absent y is undefined (same zero semantics)");
  assert.equal(snap[0].z, undefined);
  const malformed = [null, [1, 2, 3], "nope"];
  const snap2 = sceneSnapshotProbeCoefficients(malformed);
  assert.equal(snap2[0], null);
  assert.equal(snap2[1], malformed[1], "arrays stay arrays, never valid sparse vectors");
  assert.equal(snap2[2], malformed[2]);
  assert.equal(sceneSnapshotProbeCoefficients("nope"), null);
});

test("a live transition patch re-stamps the light hash from mutated coefficients", () => {
  const context = createContext();
  const { normalizeSceneLight } = loadNormalizer(context);
  const sceneApplyTransitionPatch = loadTransitionPatch(context);
  const light = normalizeSceneLight(
    { kind: "light-probe", coefficients: nineCoefs(() => ({ x: 0, y: 0, z: 0 })) },
    0, null,
  );
  const before = light._lightHash;
  sceneApplyTransitionPatch(light, { coefficients: nineCoefs(() => ({ x: 3, y: 0, z: 0 })) });
  assert.notEqual(light._lightHash, before, "the restamp must reflect the patched coefficients");
});

// --- Real WebGL upload path -------------------------------------------------

function makeGL() {
  const calls = [];
  const rec = (method) => (...args) => calls.push({ method, args });
  return {
    calls,
    uniform1i: rec("uniform1i"),
    uniform1f: rec("uniform1f"),
    uniform3f: rec("uniform3f"),
    uniform3fv: rec("uniform3fv"),
  };
}

function makeUniforms() {
  const handle = (name) => name;
  const u = { lightCount: "lightCount", hasProbeSH: "hasProbeSH", probeSH: "probeSH" };
  for (const field of ["lightTypes", "lightPositions", "lightDirections", "lightColors", "lightIntensities",
    "lightRanges", "lightDecays", "lightAngles", "lightPenumbras", "lightGroundColors"]) {
    u[field] = [];
    for (let i = 0; i < 8; i++) u[field].push(`${field}[${i}]`);
  }
  u.ambientColor = handle("ambientColor");
  u.ambientIntensity = handle("ambientIntensity");
  u.skyColor = handle("skyColor");
  u.skyIntensity = handle("skyIntensity");
  u.groundColor = handle("groundColor");
  u.groundIntensity = handle("groundIntensity");
  u.hasFog = handle("hasFog");
  u.fogDensity = handle("fogDensity");
  u.fogColor = handle("fogColor");
  return u;
}

function uploadOnce(context, gl, uniforms, lights) {
  const { scenePBRUploadLights } = loadWebGLUpload(context);
  scenePBRUploadLights(gl, uniforms, lights, null);
}

function uploadedType(gl, uniforms, i) {
  const call = gl.calls.find((c) => c.method === "uniform1i" && c.args[0] === uniforms.lightTypes[i]);
  return call ? call.args[1] : undefined;
}

test("WebGL upload sends valid SH probes as type 6 and legacy/malformed as type 0", () => {
  const context = createContext();
  const gl = makeGL();
  const uniforms = makeUniforms();
  uploadOnce(context, gl, uniforms, [
    { kind: "light-probe", intensity: 1, coefficients: nineCoefs(() => ({ x: 1 })) },
    { kind: "light-probe", intensity: 1 },
    { kind: "light-probe", intensity: 1, coefficients: [{ x: 1 }] },
  ]);
  assert.equal(uploadedType(gl, uniforms, 0), 6);
  assert.equal(uploadedType(gl, uniforms, 1), 0, "legacy probe is flat ambient");
  assert.equal(uploadedType(gl, uniforms, 2), 0, "malformed probe falls back to ambient");
  const hasProbe = gl.calls.find((c) => c.args[0] === "hasProbeSH");
  assert.equal(hasProbe.args[1], 1, "a valid probe contributed, so the SH tail is live");
});

test("the WebGL upload hash is coefficient-sensitive and skips unchanged re-uploads", () => {
  const context = createContext();
  const gl = makeGL();
  const uniforms = makeUniforms();
  const lights = [{ kind: "light-probe", intensity: 1, coefficients: nineCoefs(() => ({ x: 1 })) }];
  uploadOnce(context, gl, uniforms, lights);
  const uploadsAfterFirst = gl.calls.filter((c) => c.args[0] === "hasProbeSH").length;
  assert.equal(uploadsAfterFirst, 1);
  // Identical content: the hash stamp short-circuits, no second upload.
  uploadOnce(context, gl, uniforms, lights.map((l) => ({ ...l })));
  assert.equal(gl.calls.filter((c) => c.args[0] === "hasProbeSH").length, 1);
  // Coefficient-only change: hash changes, upload happens.
  uploadOnce(context, gl, uniforms, [
    { kind: "light-probe", intensity: 1, coefficients: nineCoefs(() => ({ x: 2 })) },
  ]);
  assert.equal(gl.calls.filter((c) => c.args[0] === "hasProbeSH").length, 2);
});

test("scenePBRLightsHash hashes only the first 8 lights (WebGL cap prefix)", () => {
  const context = createContext();
  const { scenePBRLightsHash } = loadWebGLUpload(context);
  const make = (x) => ({ kind: "point", x });
  const eight = [0, 1, 2, 3, 4, 5, 6, 7].map(make);
  const nine = eight.concat([make(8)]);
  assert.equal(scenePBRLightsHash(eight, null), scenePBRLightsHash(nine, null),
    "light 9 is outside the uploaded prefix and must not affect the hash");
  const changed = eight.slice();
  changed[7] = make(99);
  assert.notEqual(scenePBRLightsHash(eight, null), scenePBRLightsHash(changed, null));
});

test("the WebGL aggregate covers only the first 8 lights numerically", () => {
  const context = createContext();
  const gl = makeGL();
  const uniforms = makeUniforms();
  const lights = [];
  for (let i = 0; i < 9; i++) {
    lights.push({ kind: "light-probe", intensity: 2, coefficients: nineCoefs(() => ({ x: 1 })) });
  }
  uploadOnce(context, gl, uniforms, lights);
  const sh = gl.calls.find((c) => c.method === "uniform3fv" && c.args[0] === uniforms.probeSH);
  assert.ok(sh, "the probe SH uniform must be uploaded");
  assert.equal(sh.args[1].length, 27);
  assert.equal(sh.args[1][0], 16, "8 accepted probes * intensity 2 * x 1");
  assert.equal(sh.args[1][1], 0);
  assert.equal(sh.args[1][2], 0);
});

test("the WebGL upload clears the aggregate when no valid probe remains", () => {
  const context = createContext();
  const gl = makeGL();
  const uniforms = makeUniforms();
  uploadOnce(context, gl, uniforms, [
    { kind: "light-probe", intensity: 2, coefficients: nineCoefs(() => ({ x: 1, y: 2, z: 3 })) },
  ]);
  uploadOnce(context, gl, uniforms, [{ kind: "ambient", intensity: 1 }]);
  const shCalls = gl.calls.filter((c) => c.method === "uniform3fv" && c.args[0] === uniforms.probeSH);
  assert.equal(shCalls.length, 2, "the ambient-only hash miss must re-upload");
  for (const v of shCalls[1].args[1]) assert.equal(v, 0, "removing the last probe clears the SH tail");
  const flags = gl.calls.filter((c) => c.method === "uniform1i" && c.args[0] === uniforms.hasProbeSH);
  assert.equal(flags[0].args[1], 1);
  assert.equal(flags[1].args[1], 0);
});

// --- WebGPU parity ----------------------------------------------------------

test("WebGPU light bytes stay at 112 and valid9 packs type 6 vs legacy 0", () => {
  const context = createContext();
  vm.runInContext(webgpuSource, context, { filename: "16a-scene-webgpu.js" });
  const api = context.__gosx_scene3d_api;
  assert.equal(api.SCENE_WEBGPU_LIGHT_BYTES, 112, "7 * vec4f = 112 bytes; unchanged by SH work");
  assert.equal(api.sceneWebGPULightTypeCode("light-probe"), 0);
  const out = new Float32Array(api.SCENE_WEBGPU_LIGHT_FLOATS);
  api.sceneWebGPUPackLights(
    [{ kind: "light-probe", intensity: 1, coefficients: nineCoefs(() => ({ x: 1 })) }],
    1, out, {},
  );
  assert.equal(out[3], 6, "valid SH probe takes code 6");
  const legacy = new Float32Array(api.SCENE_WEBGPU_LIGHT_FLOATS);
  api.sceneWebGPUPackLights([{ kind: "light-probe", intensity: 1 }], 1, legacy, {});
  assert.equal(legacy[3], 0, "legacy probe takes code 0");
});

test("the WebGPU capacity caps at 256 by doubling", () => {
  const context = createContext();
  vm.runInContext(webgpuSource, context, { filename: "16a-scene-webgpu.js" });
  const capacityFor = context.__gosx_scene3d_api.sceneWebGPULightCapacityFor;
  assert.equal(capacityFor(0), 8);
  assert.equal(capacityFor(200), 256);
});

test("the native checkStateCoeffs checker rejects malformed coefficient states", () => {
  // Extract the real checker from the browser probe source — no duplicate
  // implementation and no browser startup.
  const source = browserCheckerSource;
  const start = source.indexOf("function checkStateCoeffs");
  const end = source.indexOf("function validateCaseCompleteness");
  assert.ok(start >= 0 && end > start, "checkStateCoeffs moved in the browser probe");
  const context = createContext();
  vm.runInContext(source.slice(start, end), context,
    { filename: "scene3d-light-probe-browser.cjs" });
  const check = vm.runInContext("checkStateCoeffs", context);

  const ok = (r) => assert.equal(r.ok, true, r.why);
  const bad = (r) => assert.equal(r.ok, false);

  // Missing state and missing coefficients array.
  bad(check(null, [], "t"));
  bad(check({}, [], "t"));
  // Exact empty matches only an empty actual array; nine zero entries are
  // NOT a match for no coefficients.
  ok(check({ coefficients: [] }, [], "t"));
  bad(check({ coefficients: [{}, {}, {}, {}, {}, {}, {}, {}, {}] }, [], "t"));
  // Length must match exactly.
  bad(check({ coefficients: [] }, [{ x: 1 }], "t"));
  bad(check({ coefficients: [{}, {}] }, [{}, {}, {}], "t"));
  // Null and array entries are not coefficient objects and must not be
  // coerced into zeros.
  bad(check({ coefficients: [null] }, [{ x: 1 }], "t"));
  bad(check({ coefficients: [[1, 0, 0]] }, [{ x: 1 }], "t"));
  // A PRESENT non-numeric or non-finite component fails.
  bad(check({ coefficients: [{ x: NaN }] }, [{ x: 1 }], "t"));
  bad(check({ coefficients: [{ y: Infinity }] }, [{}], "t"));
  bad(check({ coefficients: [{ z: "1" }] }, [{}], "t"));
  // Sparse zeros: omitted components mean zero and are accepted.
  ok(check({ coefficients: [{}] }, [{}], "t"));
  ok(check({ coefficients: [{ x: 1, y: 0.5 }, {}, {}] },
    [{ x: 1, y: 0.5 }, { z: 0 }, {}], "t"));
  // Value mismatch still fails.
  bad(check({ coefficients: [{ x: 2 }] }, [{ x: 1 }], "t"));
});
