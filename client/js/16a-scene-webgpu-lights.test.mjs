// Light-path tests for the WebGPU renderer (16a-scene-webgpu.js).
//
// GoSX declares seven light kinds and lowers all seven in Go. Until this
// suite existed the WebGPU renderer honoured two: SpotLight,
// HemisphereLight, RectAreaLight and LightProbe all reached the shader as
// point lights, so authors got a silently wrong image on the primary
// backend. These tests pin the type mapping, the storage layout, the
// rect-area basis, the light capacity, and the author diagnostics.
//
// What runs here and what does not:
//   - The pure helpers run for real, in a node:vm context that loads the
//     shipped sources the way the bundle concatenates them.
//   - The WGSL is checked structurally, plus a full compile through naga
//     when naga is on PATH. That proves the shader is valid; it does NOT
//     prove the image is right.
//   - No WebGPU adapter exists in a headless environment, so no test here
//     reads a rendered pixel.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import rendererSourceSet from "./scene3d-renderer-source-set.js";

const { readSceneRendererBackendSrc } = rendererSourceSet;

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  return fs.readFileSync(name.startsWith("../") ? path.join(__dirname, name) : path.join(srcDir, name), "utf8");
}

const webgpuSource = readSceneRendererBackendSrc("webgpu");
const webglSource = readSceneRendererBackendSrc("webgl");

// --- Bundle-shaped VM context ----------------------------------------------

function createContext() {
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    Float32Array,
    Uint32Array,
    Uint8Array,
    Int32Array,
    ArrayBuffer,
    DataView,
    Promise,
    Error,
    Map,
    Set,
    parseInt,
    parseFloat,
    isFinite,
    performance: { now: () => 0 },
    GPUBufferUsage: { UNIFORM: 1, COPY_DST: 2, MAP_READ: 4, VERTEX: 8, STORAGE: 16, COPY_SRC: 32, INDIRECT: 64 },
    GPUTextureUsage: { RENDER_ATTACHMENT: 1, COPY_SRC: 2, TEXTURE_BINDING: 4, COPY_DST: 8, STORAGE_BINDING: 16 },
    GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 },
    GPUMapMode: { READ: 1, WRITE: 2 },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.__gosx_scene3d_api = {};
  sandbox.setTimeout = (fn) => { fn(); return 0; };
  sandbox.clearTimeout = () => {};

  const context = vm.createContext(sandbox);
  const prelude = `
    function sceneNumber(value, fallback) {
      var n = Number(value);
      return Number.isFinite(n) ? n : fallback;
    }
    function sceneBool(value, fallback) { return value == null ? fallback : !!value; }
    function clamp01(v) { return Math.max(0, Math.min(1, Number(v) || 0)); }
    function normalizeSceneKind(value) {
      return typeof value === "string" ? value.trim().toLowerCase() : "box";
    }
    function normalizeSceneCameraKind(value, fallback) { return fallback; }
    function sceneRenderCamera(camera) { return camera; }
    function sceneOrthographicBounds() { return { left: -3, right: 3, top: 3, bottom: -3 }; }
    function queueInputSignal() {}
    function sceneProjectPoint() { return null; }
  `;
  vm.runInContext(prelude, context, { filename: "prelude.js" });
  vm.runInContext(readSource("11-scene-math.ts"), context, { filename: "11-scene-math.ts" });
  vm.runInContext(readSource("17-scene-input.ts"), context, { filename: "17-scene-input.ts" });
  vm.runInContext(webgpuSource, context, { filename: "16a-scene-webgpu.js" });
  return { context, sandbox };
}

function api() {
  return createContext().sandbox.__gosx_scene3d_api;
}

// --- Type mapping -----------------------------------------------------------

// Every kind GoSX lowers. Keep in step with the Kind values in
// scene/scene.go: lowerAmbientLight through lowerLightProbe.
const ALL_KINDS = [
  "ambient",
  "directional",
  "point",
  "spot",
  "hemisphere",
  "rect-area",
  "light-probe",
];

// webglTypeCodes parses the kind-to-code mapping out of the WebGL2 renderer,
// so the parity claim below rests on shipped source and not on a copy.
function webglTypeCodes() {
  const anchor = webglSource.indexOf('var lightType = 2; // default: point');
  assert.notEqual(anchor, -1, "the WebGL kind mapping moved; update this parser");
  const block = webglSource.slice(anchor, webglSource.indexOf("gl.uniform1i(uniforms.lightTypes[i]", anchor));
  // WebGL never names "point": code 2 is its declared default, which the
  // anchor line above pins.
  const codes = { "": 2, point: 2 };
  const pattern = /kind === "([a-z-]+)"\)\s*\{\s*lightType = (\d)/g;
  let match = pattern.exec(block);
  while (match) {
    codes[match[1]] = Number(match[2]);
    match = pattern.exec(block);
  }
  return codes;
}

test("sceneWebGPULightTypeCode gives every GoSX light kind its own shading path", () => {
  const scene = api();
  const got = {};
  for (const kind of ALL_KINDS) got[kind] = scene.sceneWebGPULightTypeCode(kind);
  assert.deepEqual(got, {
    ambient: 0,
    directional: 1,
    point: 2,
    spot: 3,
    hemisphere: 4,
    "rect-area": 5,
    // A probe has no position, so ambient is the honest fold. Point would
    // invent a distance falloff the author never asked for.
    "light-probe": 0,
  });
});

test("no light kind falls through to the point-light default any more", () => {
  const scene = api();
  // The default exists for unknown strings only. Before this work spot,
  // hemisphere, rect-area and light-probe all landed on it.
  assert.equal(scene.sceneWebGPULightTypeCode("nonsense"), 2);
  for (const kind of ALL_KINDS) {
    if (kind === "point") continue;
    assert.notEqual(
      scene.sceneWebGPULightTypeCode(kind),
      2,
      `${kind} must not render as a point light`,
    );
  }
});

test("codes 0 through 4 match the WebGL2 renderer, and only rect-area diverges", () => {
  const scene = api();
  const webgl = webglTypeCodes();
  for (const kind of ALL_KINDS) {
    const gpu = scene.sceneWebGPULightTypeCode(kind);
    const gl = webgl[kind];
    assert.equal(typeof gl, "number", `WebGL has no mapping for ${kind}`);
    if (kind === "rect-area") {
      // Documented divergence: WebGL2 draws a rect-area light as a point
      // light (code 2); WebGPU shades the rectangle (code 5).
      assert.equal(gpu, 5);
      assert.equal(gl, 2);
      continue;
    }
    assert.equal(gpu, gl, `${kind} must carry the same type code on both backends`);
  }
});

// --- Storage layout ---------------------------------------------------------

function packOne(light) {
  const scene = api();
  const out = new Float32Array(scene.SCENE_WEBGPU_LIGHT_FLOATS);
  const census = scene.sceneWebGPUPackLights([light], 1, out, {});
  return { out, census, scene };
}

// f32 rounds an authored f64 to the float the storage buffer holds.
function f32(value) {
  return new Float32Array([value])[0];
}

test("the packed stride matches the WGSL Light struct", () => {
  const scene = api();
  const structStart = webgpuSource.indexOf('"struct Light {",');
  assert.notEqual(structStart, -1);
  const structEnd = webgpuSource.indexOf('"};",', structStart);
  const body = webgpuSource.slice(structStart, structEnd);
  const fields = body.match(/vec4f,/g) || [];
  assert.equal(
    fields.length * 4,
    scene.SCENE_WEBGPU_LIGHT_FLOATS,
    "SCENE_WEBGPU_LIGHT_FLOATS must equal 4 floats per vec4f field",
  );
});

test("a point light packs position, colour, range and decay", () => {
  const { out } = packOne({
    kind: "point", x: 1, y: 2, z: 3, color: "#ff8000",
    intensity: 2.5, range: 12, decay: 1.5,
  });
  assert.deepEqual(Array.from(out.slice(0, 4)), [1, 2, 3, 2]);
  assert.equal(out[7], 2.5, "intensity rides in direction.w");
  assert.ok(Math.abs(out[8] - 1) < 1e-6);
  assert.ok(Math.abs(out[9] - 0x80 / 255) < 1e-6);
  assert.equal(out[10], 0);
  assert.equal(out[11], 12, "range rides in color.a");
  assert.equal(out[12], 1.5, "decay rides in params.x");
});

test("a spot light packs its direction, cone angle and penumbra", () => {
  const { out, census } = packOne({
    kind: "spot", x: 0, y: 5, z: 0,
    directionX: 0, directionY: -1, directionZ: 0,
    angle: 0.6, penumbra: 0.25, range: 20, decay: 2, intensity: 3,
  });
  assert.equal(out[3], 3, "type code 3 is spot");
  assert.deepEqual(Array.from(out.slice(4, 8)), [0, -1, 0, 3]);
  assert.equal(out[15], f32(0.6), "the cone angle rides in params.w");
  assert.equal(out[19], f32(0.25), "the penumbra rides in groundPenumbra.a");
  assert.equal(census.spot, 1);
  assert.equal(census.spotEmptyCone, 0);
});

test("a spot penumbra outside 0..1 clamps instead of inverting the cone", () => {
  assert.equal(packOne({ kind: "spot", angle: 1, penumbra: 4 }).out[19], 1);
  assert.equal(packOne({ kind: "spot", angle: 1, penumbra: -3 }).out[19], 0);
});

test("a hemisphere light packs sky colour and ground colour separately", () => {
  // The IR carries the sky colour in `color` (lowerHemisphereLight writes
  // SkyColor there) and the ground colour in `groundColor`.
  const { out } = packOne({
    kind: "hemisphere", color: "#0000ff", groundColor: "#00ff00", intensity: 1.25,
  });
  assert.equal(out[3], 4, "type code 4 is hemisphere");
  assert.deepEqual(Array.from(out.slice(8, 11)), [0, 0, 1], "sky colour in color.rgb");
  assert.deepEqual(Array.from(out.slice(16, 19)), [0, 1, 0], "ground colour in groundPenumbra.rgb");
  assert.equal(out[7], 1.25);
});

test("a light probe packs as an ambient term and never as a point light", () => {
  const { out, census } = packOne({
    kind: "light-probe", color: "#ffffff", intensity: 0.7,
    coefficients: [{ x: 1, y: 1, z: 1 }],
  });
  assert.equal(out[3], 0, "a probe shades as ambient");
  assert.equal(out[11], 0, "a probe carries no range");
  assert.equal(census.lightProbe, 1);
  assert.equal(census.lightProbeWithCoefficients, 1, "ignored coefficients must be counted");
});

test("non-rect lights zero the area edge vectors", () => {
  const { out } = packOne({ kind: "directional", directionX: 1, directionY: -1 });
  assert.deepEqual(Array.from(out.slice(20, 28)), [0, 0, 0, 0, 0, 0, 0, 0]);
});

test("packing stops at the requested count and leaves later slots untouched", () => {
  const scene = api();
  const out = new Float32Array(scene.SCENE_WEBGPU_LIGHT_FLOATS * 3).fill(-7);
  const census = scene.sceneWebGPUPackLights(
    [{ kind: "ambient" }, { kind: "point" }, { kind: "spot", angle: 1 }],
    2,
    out,
    {},
  );
  assert.equal(census.count, 2);
  assert.equal(out[scene.SCENE_WEBGPU_LIGHT_FLOATS * 2], -7, "slot 2 must stay untouched");
});

test("packing never reads past the array the caller passed", () => {
  const scene = api();
  const out = new Float32Array(scene.SCENE_WEBGPU_LIGHT_FLOATS * 4);
  const census = scene.sceneWebGPUPackLights([{ kind: "ambient" }], 4, out, {});
  assert.equal(census.count, 1);
});

// --- Rect-area basis --------------------------------------------------------

function dot3(a, b) { return a[0] * b[0] + a[1] * b[1] + a[2] * b[2]; }
function cross3(a, b) {
  return [
    a[1] * b[2] - a[2] * b[1],
    a[2] * b[0] - a[0] * b[2],
    a[0] * b[1] - a[1] * b[0],
  ];
}
function len3(a) { return Math.sqrt(dot3(a, a)); }

function rectBasis(light) {
  const scene = api();
  const out = new Float32Array(6);
  scene.sceneWebGPURectAreaBasis(light, out);
  return { halfW: Array.from(out.slice(0, 3)), halfH: Array.from(out.slice(3, 6)) };
}

test("the rect-area edge vectors carry half the authored width and height", () => {
  const { halfW, halfH } = rectBasis({
    kind: "rect-area", width: 4, height: 6,
    directionX: 0, directionY: 0, directionZ: -1,
  });
  assert.ok(Math.abs(len3(halfW) - 2) < 1e-6, `half width ${len3(halfW)}`);
  assert.ok(Math.abs(len3(halfH) - 3) < 1e-6, `half height ${len3(halfH)}`);
});

test("the rect-area edge vectors are orthogonal to each other and to the direction", () => {
  for (const dir of [
    [0, -1, 0],
    [0, 0, -1],
    [1, 0, 0],
    [0.4, -0.7, 0.6],
    [0, 0, 1],
  ]) {
    const { halfW, halfH } = rectBasis({
      kind: "rect-area", width: 2, height: 2,
      directionX: dir[0], directionY: dir[1], directionZ: dir[2],
    });
    assert.ok(Math.abs(dot3(halfW, halfH)) < 1e-6, `edges not orthogonal for ${dir}`);
    assert.ok(Math.abs(dot3(halfW, dir)) < 1e-6, `half width not in the plane for ${dir}`);
    assert.ok(Math.abs(dot3(halfH, dir)) < 1e-6, `half height not in the plane for ${dir}`);
  }
});

// The WGSL form factor returns 0 when dot(cross(rect1-rect0, rect3-rect0),
// P - rect0) < 0. Working that through, the lit side is the side that
// cross(halfWidth, halfHeight) points AWAY from. A GoSX light shines the way
// its Direction points, so cross(halfWidth, halfHeight) must be antiparallel
// to Direction. Get this backwards and a rect-area light lights the wall
// behind it and leaves the room dark.
test("cross(halfWidth, halfHeight) opposes the emission direction", () => {
  for (const dir of [
    [0, -1, 0],
    [0, 0, -1],
    [1, 0, 0],
    [0.4, -0.7, 0.6],
  ]) {
    const { halfW, halfH } = rectBasis({
      kind: "rect-area", width: 3, height: 5,
      directionX: dir[0], directionY: dir[1], directionZ: dir[2],
    });
    const n = cross3(halfW, halfH);
    const dirLen = len3(dir);
    const cosine = dot3(n, dir) / (len3(n) * dirLen);
    assert.ok(cosine < -0.999, `cross(halfW, halfH) must oppose ${dir}; cosine ${cosine}`);
  }
});

test("a zero direction falls back to straight down instead of collapsing", () => {
  const { halfW, halfH } = rectBasis({
    kind: "rect-area", width: 2, height: 2,
    directionX: 0, directionY: 0, directionZ: 0,
  });
  const n = cross3(halfW, halfH);
  assert.ok(len3(n) > 0.9, "the basis must stay non-degenerate");
  assert.ok(dot3(n, [0, -1, 0]) < -0.9, "the fallback direction is -Y");
});

test("a rect-area light packs its basis into the area fields", () => {
  const { out, census } = packOne({
    kind: "rect-area", x: 0, y: 3, z: 0, width: 2, height: 2,
    directionX: 0, directionY: -1, directionZ: 0, color: "#ffffff", intensity: 4,
  });
  assert.equal(out[3], 5, "type code 5 is rect-area");
  const halfW = Array.from(out.slice(20, 23));
  const halfH = Array.from(out.slice(24, 27));
  assert.ok(len3(halfW) > 0.9 && len3(halfW) < 1.1);
  assert.ok(len3(halfH) > 0.9 && len3(halfH) < 1.1);
  assert.equal(out[23], 0, "the padding lane stays zero");
  assert.equal(out[27], 0, "the padding lane stays zero");
  assert.equal(census.rectArea, 1);
});

// --- Light capacity ---------------------------------------------------------

test("the light capacity grows by doubling and stops at the hard cap", () => {
  const scene = api();
  const capacityFor = scene.sceneWebGPULightCapacityFor;
  assert.equal(capacityFor(0), 8);
  assert.equal(capacityFor(8), 8, "eight lights still fit the starting buffer");
  assert.equal(capacityFor(9), 16, "the ninth light must not be dropped");
  assert.equal(capacityFor(17), 32);
  assert.equal(capacityFor(200), 256);
  assert.equal(capacityFor(5000), scene.SCENE_WEBGPU_LIGHT_CAPACITY_MAX);
});

test("the shader bounds the light loop with arrayLength, not a fixed cap", () => {
  assert.match(
    webgpuSource,
    /let lightCount = min\(frame\.lightCount, arrayLength\(&lights\)\);/,
    "the loop must bound itself against the storage buffer the frame sized",
  );
  assert.ok(
    !/MAX_LIGHTS/.test(webgpuSource),
    "no compile-time light cap may remain in the WGSL",
  );
});

test("uploadLights grows the storage buffer before it writes", () => {
  const start = webgpuSource.indexOf("function ensureLightCapacity(");
  const end = webgpuSource.indexOf("function uploadFogUniforms(");
  assert.notEqual(start, -1, "ensureLightCapacity must exist");
  assert.notEqual(end, -1);
  const body = webgpuSource.slice(start, end);
  const grow = body.indexOf("ensureLightCapacity(sceneWebGPULightCapacityFor(lightArray.length))");
  const pack = body.indexOf("sceneWebGPUPackLights(");
  const write = body.indexOf("writeBuffer(\n          lightStorageBuffer");
  assert.ok(grow !== -1, "uploadLights must resize before it counts");
  assert.ok(pack > grow, "packing must follow the resize");
  assert.ok(write > pack, "the upload must follow the packing");
  assert.match(body, /_retiredLightBuffers\.push\(lightStorageBuffer\)/,
    "a replaced buffer must be retired, not destroyed while commands may still read it");
});

// --- Author diagnostics -----------------------------------------------------

// issueCodes lifts the sandbox realm's result into a host array so
// node:assert/strict can compare it against a literal from this file.
function issueCodes(census, requested, capacity) {
  const scene = api();
  return Array.from(scene.sceneWebGPULightIssues(census, requested, capacity), (i) => i.code);
}

const emptyCensus = {
  count: 0, spot: 0, spotEmptyCone: 0, hemisphere: 0,
  rectArea: 0, lightProbe: 0, lightProbeWithCoefficients: 0,
};

test("a clean scene reports nothing", () => {
  assert.deepEqual(issueCodes({ ...emptyCensus, count: 4, spot: 2, hemisphere: 1 }, 4, 8), []);
});

test("exceeding the light cap reports instead of dropping lights in silence", () => {
  const scene = api();
  const issues = scene.sceneWebGPULightIssues({ ...emptyCensus, count: 256 }, 300, 256);
  assert.deepEqual(Array.from(issues, (i) => i.code), ["light-cap"]);
  assert.match(issues[0].message, /300 lights/);
  assert.match(issues[0].message, /first 256/);
});

test("a spot light with a zero cone angle reports its empty cone", () => {
  // normalizeSceneLight defaults angle to 0, and cos(0) makes the cone
  // collapse on BOTH backends. Widening the cone here would break parity, so
  // the renderer tells the author instead.
  const codes = issueCodes({ ...emptyCensus, count: 1, spot: 1, spotEmptyCone: 1 }, 1, 8);
  assert.deepEqual(codes, ["spot-empty-cone"]);
});

test("a rect-area light reports its approximate specular lobe", () => {
  const scene = api();
  const issues = scene.sceneWebGPULightIssues({ ...emptyCensus, count: 1, rectArea: 1 }, 1, 8);
  assert.deepEqual(Array.from(issues, (i) => i.code), ["rect-area-specular"]);
  assert.match(issues[0].message, /LTC/, "the message must name what is missing");
});

test("a light probe reports its ignored spherical-harmonic coefficients", () => {
  const codes = issueCodes(
    { ...emptyCensus, count: 1, lightProbe: 1, lightProbeWithCoefficients: 1 },
    1, 8,
  );
  assert.deepEqual(codes, ["light-probe-sh"]);
});

test("a probe without coefficients reports nothing", () => {
  const codes = issueCodes({ ...emptyCensus, count: 1, lightProbe: 1 }, 1, 8);
  assert.deepEqual(codes, []);
});

test("the renderer reports each lighting issue once, not once per frame", () => {
  const start = webgpuSource.indexOf("function reportLightIssuesOnce(");
  assert.notEqual(start, -1);
  const body = webgpuSource.slice(start, start + 900);
  assert.match(body, /_lightIssuesReported\[key\]/);
  assert.match(webgpuSource, /reportLightIssuesOnce\(sceneWebGPULightIssues\(census, lightArray\.length, _lightCapacity\)\)/);
  assert.match(webgpuSource, /window\.__gosx\.reportIssue\(record\)/);
});

// --- uploadLights, executed ------------------------------------------------
//
// The three functions below are the shipped renderer text, lifted out of
// 16a-scene-webgpu.js and evaluated in the sandbox with the closure variables
// the renderer gives them. the runtime suite uses the same technique for the
// cull helpers. This runs the real growth, retirement, packing and upload
// logic against a recording fake device. It does NOT compile WGSL or draw.

function loadUploadLights() {
  const { context, sandbox } = createContext();
  const startMark = "    // ensureLightCapacity grows the light storage buffer";
  const endMark = "      return census;\n    }";
  const start = webgpuSource.indexOf(startMark);
  assert.notEqual(start, -1, "the uploadLights block moved; update this loader");
  const end = webgpuSource.indexOf(endMark, start);
  assert.notEqual(end, -1, "uploadLights must end with `return census;`");
  const body = webgpuSource.slice(start, end + endMark.length);

  const writes = [];
  const created = [];
  const issues = [];
  function makeBuffer(desc) {
    const buffer = {
      label: desc.label || "",
      size: desc.size || 0,
      usage: desc.usage || 0,
      destroyed: false,
      destroy() { buffer.destroyed = true; },
    };
    created.push(buffer);
    return buffer;
  }
  const device = {
    createBuffer: makeBuffer,
    queue: {
      writeBuffer(buffer, offset, data, dataOffset, size) {
        writes.push({
          label: buffer.label,
          bufferOffset: offset,
          dataOffset: dataOffset === undefined ? 0 : dataOffset,
          size: size === undefined ? data.length : size,
          words: data instanceof Uint32Array ? Array.from(data) : null,
          floats: data instanceof Float32Array ? Array.from(data) : null,
        });
      },
    },
  };
  sandbox.__gosx = { reportIssue(record) { issues.push(record); } };
  sandbox.__device = device;
  sandbox.__frameUniformBuffer = { label: "gosx-frame" };

  const harness = vm.runInContext(
    [
      "(function(device, frameUniformBuffer) {",
      "  var lightStorageBuffer = device.createBuffer({",
      "    label: \"gosx-lights\",",
      "    size: SCENE_WEBGPU_LIGHT_CAPACITY_MIN * SCENE_WEBGPU_LIGHT_BYTES,",
      "    usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,",
      "  });",
      "  var _lightCapacity = SCENE_WEBGPU_LIGHT_CAPACITY_MIN;",
      "  var _lightCountBuf = new Uint32Array(1);",
      "  var _lightDataF = new Float32Array(_lightCapacity * SCENE_WEBGPU_LIGHT_FLOATS);",
      "  var _lightColorCache = {};",
      "  var _retiredLightBuffers = [];",
      "  var _lightIssuesReported = Object.create(null);",
      body,
      "  return {",
      "    uploadLights: uploadLights,",
      "    buffer: function() { return lightStorageBuffer; },",
      "    capacity: function() { return _lightCapacity; },",
      "    retired: function() { return _retiredLightBuffers; },",
      "    data: function() { return _lightDataF; },",
      "  };",
      "})",
    ].join("\n"),
    context,
    { filename: "uploadLights-harness.js" },
  )(device, sandbox.__frameUniformBuffer);

  return { harness, writes, created, issues, scene: sandbox.__gosx_scene3d_api };
}

test("uploadLights writes lightCount into the frame uniform at byte 140", () => {
  const { harness, writes } = loadUploadLights();
  harness.uploadLights([{ kind: "ambient" }, { kind: "spot", angle: 1 }, { kind: "hemisphere" }]);
  const countWrite = writes.find((w) => w.label === "gosx-frame");
  assert.ok(countWrite, "lightCount must reach the frame uniform buffer");
  assert.equal(countWrite.bufferOffset, 140, "lightCount sits at byte 140 of FrameUniforms");
  assert.deepEqual(countWrite.words, [3]);
});

test("uploadLights uploads exactly the used prefix of the light buffer", () => {
  const { harness, writes, scene } = loadUploadLights();
  harness.uploadLights([{ kind: "point" }, { kind: "point" }]);
  const upload = writes.find((w) => w.label === "gosx-lights");
  assert.ok(upload, "the light storage buffer must be written");
  assert.equal(upload.bufferOffset, 0);
  assert.equal(upload.dataOffset, 0);
  assert.equal(
    upload.size,
    2 * scene.SCENE_WEBGPU_LIGHT_FLOATS,
    "the size argument counts ELEMENTS of the Float32Array, not bytes",
  );
  // The size must land on a 4-byte multiple, or WebGPU rejects the write.
  assert.equal((upload.size * 4) % 4, 0);
});

test("uploadLights skips the storage write when a scene has no lights", () => {
  const { harness, writes } = loadUploadLights();
  const census = harness.uploadLights([]);
  assert.equal(census.count, 0);
  assert.equal(writes.filter((w) => w.label === "gosx-lights").length, 0);
  const countWrite = writes.find((w) => w.label === "gosx-frame");
  assert.deepEqual(countWrite.words, [0]);
});

test("uploadLights tolerates a missing or malformed light list", () => {
  const { harness } = loadUploadLights();
  for (const input of [undefined, null, 0, "nope", {}]) {
    assert.equal(harness.uploadLights(input).count, 0);
  }
});

// The regression this whole task exists for: the ninth light used to vanish.
test("uploadLights keeps the ninth light by growing the storage buffer", () => {
  const { harness, writes, created, scene } = loadUploadLights();
  const lights = [];
  for (let i = 0; i < 9; i += 1) lights.push({ kind: "point", x: i });
  const census = harness.uploadLights(lights);

  assert.equal(census.count, 9, "all nine lights must reach the shader");
  assert.equal(harness.capacity(), 16, "the buffer must have doubled");
  const grown = created[created.length - 1];
  assert.equal(grown.label, "gosx-lights");
  assert.equal(grown.size, 16 * scene.SCENE_WEBGPU_LIGHT_BYTES);
  assert.equal(harness.buffer(), grown, "later frames must bind the grown buffer");

  const upload = writes.find((w) => w.label === "gosx-lights");
  assert.equal(upload.size, 9 * scene.SCENE_WEBGPU_LIGHT_FLOATS);
  // Light 8 is the one the old cap dropped. Its position must be in the data.
  assert.equal(harness.data()[8 * scene.SCENE_WEBGPU_LIGHT_FLOATS], 8);
});

test("uploadLights retires a replaced buffer instead of destroying it mid-flight", () => {
  const { harness } = loadUploadLights();
  const first = harness.buffer();
  const lights = [];
  for (let i = 0; i < 20; i += 1) lights.push({ kind: "point" });
  harness.uploadLights(lights);
  assert.equal(harness.capacity(), 32);
  const retired = harness.retired();
  assert.equal(retired.length, 1);
  assert.equal(retired[0], first);
  assert.equal(
    first.destroyed,
    false,
    "destroying the old buffer now would fault the previous frame's submitted commands",
  );
});

test("uploadLights stops growing at the hard cap and reports the drop", () => {
  const { harness, issues, scene } = loadUploadLights();
  const lights = [];
  for (let i = 0; i < 400; i += 1) lights.push({ kind: "point" });
  const census = harness.uploadLights(lights);
  assert.equal(census.count, scene.SCENE_WEBGPU_LIGHT_CAPACITY_MAX);
  assert.equal(harness.capacity(), scene.SCENE_WEBGPU_LIGHT_CAPACITY_MAX);
  const capIssue = issues.find((i) => i.code === "light-cap");
  assert.ok(capIssue, "dropping lights must reach the author");
  assert.equal(capIssue.scope, "scene3d");
  assert.equal(capIssue.type, "lighting");
  assert.equal(capIssue.source, "webgpu");
  assert.match(capIssue.message, /400/);
});

test("uploadLights reports each lighting issue once across frames", () => {
  const { harness, issues } = loadUploadLights();
  const lights = [{ kind: "rect-area", width: 2, height: 2 }, { kind: "spot", angle: 0 }];
  for (let frame = 0; frame < 5; frame += 1) harness.uploadLights(lights);
  const codes = issues.map((i) => i.code).sort();
  assert.deepEqual(codes, ["rect-area-specular", "spot-empty-cone"]);
});

test("uploadLights does not grow when the light count falls back", () => {
  const { harness, created } = loadUploadLights();
  const many = [];
  for (let i = 0; i < 12; i += 1) many.push({ kind: "point" });
  harness.uploadLights(many);
  const afterGrowth = created.length;
  harness.uploadLights([{ kind: "point" }]);
  assert.equal(created.length, afterGrowth, "shrinking must not reallocate");
  assert.equal(harness.capacity(), 16);
});

// --- WGSL shading paths -----------------------------------------------------

test("the fragment shader carries a branch for every light type it declares", () => {
  const anchor = webgpuSource.indexOf('"    let lightCount = min(frame.lightCount, arrayLength(&lights));",');
  assert.notEqual(anchor, -1);
  const body = webgpuSource.slice(anchor, webgpuSource.indexOf('"    // Environment hemisphere lighting.",', anchor));
  assert.match(body, /lightType == 0u/, "ambient and probe");
  assert.match(body, /lightType == 1u/, "directional");
  assert.match(body, /lightType == 4u/, "hemisphere");
  assert.match(body, /lightType == 5u/, "rect-area");
  assert.match(body, /lightType == 3u/, "spot");
  assert.match(body, /spotConeAttenuation\(L, light\.direction\.xyz, light\.params\.w, light\.groundPenumbra\.a\)/);
  assert.match(body, /mix\(light\.groundPenumbra\.rgb, lightColor, hBlend\)/);
  assert.match(body, /rectAreaLightRadiance\(light, in\.worldPos, N, V, albedo, roughness, metalness, F0, F90, NoV\)/);
});

test("the WGSL spot cone reproduces the WebGL2 cone, term for term", () => {
  // Port, do not invent. The two shaders must compute the same cone.
  const gpuStart = webgpuSource.indexOf('"fn spotConeAttenuation(');
  assert.notEqual(gpuStart, -1);
  const gpu = webgpuSource.slice(gpuStart, webgpuSource.indexOf('"}",', gpuStart));
  assert.match(gpu, /let outerCos = cos\(angle\);/);
  assert.match(gpu, /let innerCos = cos\(angle \* \(1\.0 - penumbra\)\);/);
  assert.match(gpu, /clamp\(\(cosAngle - outerCos\) \/ max\(innerCos - outerCos, 0\.001\), 0\.0, 1\.0\)/);

  // The same three lines in the WebGL fragment shader.
  assert.match(webglSource, /float outerCos = cos\(u_lightAngles\[i\]\);/);
  assert.match(webglSource, /float innerCos = cos\(u_lightAngles\[i\] \* \(1\.0 - u_lightPenumbras\[i\]\)\);/);
  assert.match(webglSource, /clamp\(\(cosAngle - outerCos\) \/ max\(innerCos - outerCos, 0\.001\), 0\.0, 1\.0\)/);
});

test("the WGSL hemisphere blend reproduces the WebGL2 blend", () => {
  assert.match(webgpuSource, /let hBlend = N\.y \* 0\.5 \+ 0\.5;/);
  assert.match(webglSource, /float hBlend = N\.y \* 0\.5 \+ 0\.5;/);
  assert.match(webglSource, /vec3 hemiColor = mix\(u_lightGroundColors\[i\], lightColor, hBlend\);/);
});

test("the rect-area diffuse term is the three.js polygon form factor", () => {
  // three.js evaluates the RectAreaLight diffuse with LTC_Evaluate and an
  // identity matrix, which needs no lookup table. These are its constants.
  assert.match(webgpuSource, /0\.8543985 \+ \(0\.4965155 \+ 0\.0145206 \* y\) \* y/);
  assert.match(webgpuSource, /3\.4175940 \+ \(4\.1616724 \+ y\) \* y/);
  assert.match(webgpuSource, /fn ltcClippedSphereFormFactor\(f: vec3f\) -> f32/);
  assert.match(webgpuSource, /max\(\(l \* l \+ f\.z\) \/ \(l \+ 1\.0\), 0\.0\)/);
  // And the honest label on the specular half.
  assert.match(webgpuSource, /fn rectAreaRepresentativePoint\(/);
});

// --- WGSL compile (skipped when naga is absent) -----------------------------

function nagaAvailable() {
  try {
    execFileSync("naga", ["--version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

const hasNaga = nagaAvailable();

test(
  "the PBR fragment WGSL compiles",
  { skip: hasNaga ? false : "naga is not on PATH" },
  () => {
    // Recreate the shader exactly as the renderer hands it to
    // createShaderModule, then run the reference WGSL validator over it.
    // This catches a malformed struct or a bad builtin call. It does NOT
    // check the image; no adapter exists here.
    const { context } = createContext();
    const code = vm.runInContext("WGSL_PBR_FRAGMENT", context);
    assert.equal(typeof code, "string");
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-wgsl-"));
    const file = path.join(dir, "pbr-fragment.wgsl");
    fs.writeFileSync(file, code);
    try {
      execFileSync("naga", [file], { stdio: "pipe" });
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  },
);
