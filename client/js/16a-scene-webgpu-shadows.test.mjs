// Shadow-path tests for the WebGPU renderer (16a-scene-webgpu.js).
//
// Cluster-B shadow parity moved the WebGL2 PSSM cascade fit into
// 16c-scene-shared-pbr.js (see runtime-04-scene-materials-lighting.test.js
// for the pinned-splits/ortho-fit table tests that cover the shared math
// itself) and gave WebGPU a texture_depth_2d_array shadow slot with the same
// cascade selection and PCSS filtering constants WebGL2 uses. These tests
// pin the WGSL side of that port: a constant or comparison drifting between
// the two backends fails here instead of only showing up as an eyeball
// difference in a screenshot.
//
// What runs here and what does not:
//   - Every assertion below is a string/structural check against the shipped
//     WGSL source text.
//   - The WGSL is also compiled through naga when naga is on PATH, proving
//     it is syntactically and semantically valid. That does NOT prove the
//     image is right.
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

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

const webgpuSource = readSource("16a-scene-webgpu.js");
const webglSource = readSource("16-scene-webgl.js");

// --- ShadowUniforms / ShadowSlot struct --------------------------------------

test("ShadowUniforms carries a ShadowSlot array with per-cascade matrices and splits", () => {
  const structStart = webgpuSource.indexOf('"struct ShadowSlot {",');
  assert.notEqual(structStart, -1, "ShadowSlot struct must exist");
  const structEnd = webgpuSource.indexOf('"};",', structStart);
  const body = webgpuSource.slice(structStart, structEnd);
  assert.match(body, /lightSpaceMatrices: array<mat4x4f, 4>,/);
  assert.match(body, /cascadeSplits: vec4f,/);
  assert.match(body, /cascadeCount: u32,/);
  assert.match(body, /hasShadow: u32,/);
  assert.match(body, /lightIndex: i32,/);
  assert.match(body, /bias: f32,/);
  assert.match(body, /softness: f32,/);

  const uniformsStart = webgpuSource.indexOf('"struct ShadowUniforms {",');
  assert.notEqual(uniformsStart, -1, "ShadowUniforms struct must exist");
  const uniformsEnd = webgpuSource.indexOf('"};",', uniformsStart);
  const uniformsBody = webgpuSource.slice(uniformsStart, uniformsEnd);
  assert.match(uniformsBody, /slots: array<ShadowSlot, 2>,/);
});

// --- Bindings -----------------------------------------------------------------

test("shadow map bindings declare texture_depth_2d_array, not texture_depth_2d", () => {
  assert.match(webgpuSource, /@group\(0\) @binding\(4\) var shadowMap0: texture_depth_2d_array;/);
  assert.match(webgpuSource, /@group\(0\) @binding\(6\) var shadowMap1: texture_depth_2d_array;/);
  assert.match(webgpuSource, /@group\(0\) @binding\(5\) var shadowSampler0: sampler_comparison;/);
  assert.match(webgpuSource, /@group\(0\) @binding\(7\) var shadowSampler1: sampler_comparison;/);
});

test("the frame bind group layout declares 2d-array view dimension for both shadow slots", () => {
  const start = webgpuSource.indexOf("function wgpuCreateFrameBindGroupLayout(device)");
  assert.notEqual(start, -1);
  const end = webgpuSource.indexOf("function wgpuCreateMaterialBindGroupLayout", start);
  const body = webgpuSource.slice(start, end);
  const shadowEntries = body.match(/binding: [46], visibility: GPUShaderStage\.FRAGMENT, texture: \{ sampleType: "depth", viewDimension: "2d-array" \}/g) || [];
  assert.equal(shadowEntries.length, 2, "both shadow texture bindings (4 and 6) must declare viewDimension 2d-array");
});

// --- Cascade selection ---------------------------------------------------------

test("shadowCascadeIndex selects a cascade with the same three comparisons WebGL2 uses", () => {
  const start = webgpuSource.indexOf('"fn shadowCascadeIndex(slot: ShadowSlot, viewDepth: f32) -> u32 {",');
  assert.notEqual(start, -1);
  const end = webgpuSource.indexOf('"}",', start);
  const body = webgpuSource.slice(start, end);
  assert.match(body, /slot\.cascadeCount >= 2u && viewDepth >= slot\.cascadeSplits\.x/);
  assert.match(body, /slot\.cascadeCount >= 3u && viewDepth >= slot\.cascadeSplits\.y/);
  assert.match(body, /slot\.cascadeCount >= 4u && viewDepth >= slot\.cascadeSplits\.z/);

  // The WebGL2 dispatcher makes the same three comparisons over its own
  // per-slot split arrays.
  assert.match(webglSource, /u_shadowCascades0 >= 2 && viewDepth >= u_shadowCascadeSplits0\[0\]/);
  assert.match(webglSource, /u_shadowCascades0 >= 3 && viewDepth >= u_shadowCascadeSplits0\[1\]/);
  assert.match(webglSource, /u_shadowCascades0 >= 4 && viewDepth >= u_shadowCascadeSplits0\[2\]/);
});

test("shadowViewDepth is computed once per fragment from frame.viewMatrix, matching WebGL2's u_viewMatrix term", () => {
  assert.match(webgpuSource, /let shadowViewDepth = -\(frame\.viewMatrix \* vec4f\(in\.worldPos, 1\.0\)\)\.z;/);
  assert.match(webglSource, /float viewDepth = -\(u_viewMatrix \* vec4\(v_worldPosition, 1\.0\)\)\.z;/);
});

// --- PCSS constants -------------------------------------------------------------

test("shadowFactorCascade reuses WebGL2's exact PCSS blocker and filter-radius constants", () => {
  const start = webgpuSource.indexOf('"fn shadowFactorCascade(');
  assert.notEqual(start, -1);
  const end = webgpuSource.indexOf('"fn distributionGGX', start);
  const body = webgpuSource.slice(start, end);

  assert.match(body, /slot\.softness \* 32\.0/, "blocker search radius must use the WebGL2 * 32.0 constant");
  assert.match(body, /penumbra \* 128\.0/, "penumbra-to-filter-radius scale must use the WebGL2 * 128.0 constant");
  assert.match(body, /slot\.softness \* 96\.0/, "filter radius ceiling must use the WebGL2 * 96.0 constant");
  assert.match(body, /avgBlockerDepth\) \* slot\.softness \/ max\(avgBlockerDepth, 1e-4\)/, "penumbra estimate must match WebGL2's formula");

  // WebGL2's shadowFactor carries the same three constants.
  assert.match(webglSource, /softness \* 32\.0/);
  assert.match(webglSource, /penumbra \* 128\.0/);
  assert.match(webglSource, /softness \* 96\.0/);
  assert.match(webglSource, /avgBlockerDepth\) \* softness \/ max\(avgBlockerDepth, 1e-4\)/);
});

test("shadowFactorCascade keeps the shipped 4-tap hard-shadow path at softness 0 (a deliberate WebGPU/WebGL2 delta)", () => {
  const start = webgpuSource.indexOf('"fn shadowFactorCascade(');
  const end = webgpuSource.indexOf('"fn distributionGGX', start);
  const body = webgpuSource.slice(start, end);
  assert.match(body, /slot\.softness <= 0\.0001/);
  assert.match(body, /for \(var i = 0u; i < 4u; i = i \+ 1u\) \{/);
});

test("the blocker search reads raw depth via textureLoad; the final filter reads through textureSampleCompareLevel", () => {
  const start = webgpuSource.indexOf('"fn shadowFactorCascade(');
  const end = webgpuSource.indexOf('"fn distributionGGX', start);
  const body = webgpuSource.slice(start, end);
  assert.match(body, /textureLoad\(tex, texel, layer, 0\)/, "the blocker search must read unfiltered depth");
  assert.match(body, /textureSampleCompareLevel\(tex, samp, sampleUV, layer, refDepth\)/, "the final PCF taps must use hardware comparison");
});

test("kPoissonDisk8 matches WebGL2's kPoissonDisk8 constants exactly", () => {
  const gpuStart = webgpuSource.indexOf('"const kPoissonDisk8 = array<vec2f, 8>(",');
  assert.notEqual(gpuStart, -1);
  const gpuEnd = webgpuSource.indexOf('");",', gpuStart);
  const gpuBody = webgpuSource.slice(gpuStart, gpuEnd);
  const gpuTaps = (gpuBody.match(/-?\d\.\d+/g) || []).map(Number);
  assert.equal(gpuTaps.length, 16, "expected 8 taps x 2 components");

  const glStart = webglSource.indexOf("const vec2 kPoissonDisk8[8] = vec2[](");
  assert.notEqual(glStart, -1);
  const glEnd = webglSource.indexOf(');",', glStart);
  const glBody = webglSource.slice(glStart, glEnd);
  const glTaps = (glBody.match(/-?\d\.\d+/g) || []).map(Number);
  assert.equal(glTaps.length, 16);

  for (let i = 0; i < 16; i++) {
    assert.ok(Math.abs(gpuTaps[i] - glTaps[i]) < 1e-6, `tap component ${i}: WebGPU ${gpuTaps[i]} vs WebGL2 ${glTaps[i]}`);
  }
});

// --- Shading loop gating --------------------------------------------------------

test("the light loop gates shadowFactorCascade the same way it gated shadowFactor0/1", () => {
  assert.match(webgpuSource, /shadow\.slots\[0\]\.hasShadow != 0u && i32\(i\) == shadow\.slots\[0\]\.lightIndex/);
  assert.match(webgpuSource, /shadowFactorCascade\(shadowMap0, shadowSampler0, 0u, shadowViewDepth, in\.worldPos\)/);
  assert.match(webgpuSource, /shadow\.slots\[1\]\.hasShadow != 0u && i32\(i\) == shadow\.slots\[1\]\.lightIndex/);
  assert.match(webgpuSource, /shadowFactorCascade\(shadowMap1, shadowSampler1, 1u, shadowViewDepth, in\.worldPos\)/);
});

// --- CPU-side cascade orchestration ---------------------------------------------

test("wgpuComputeShadowCascadeMatrices calls the shared PSSM helpers moved to 16c-scene-shared-pbr.js", () => {
  const start = webgpuSource.indexOf("function wgpuComputeShadowCascadeMatrices(");
  assert.notEqual(start, -1);
  const end = webgpuSource.indexOf("\n  }\n", start);
  const body = webgpuSource.slice(start, end);
  assert.match(body, /sceneShadowComputeCascadeSplits\(/);
  assert.match(body, /sceneShadowFrustumSubCorners\(/);
  assert.match(body, /sceneShadowFitLightSpaceOrtho\(/);
  assert.match(body, /sceneShadowLightSpaceMatrix\(light, sceneBounds\)/, "numCascades <= 1 must fall back to the whole-scene fit");
});

test("wgpuCreateShadowMap allocates a depth array texture with one render-target view per cascade layer", () => {
  const start = webgpuSource.indexOf("function wgpuCreateShadowMap(device, size, layerCount)");
  assert.notEqual(start, -1);
  const end = webgpuSource.indexOf("\n  }\n", start);
  const body = webgpuSource.slice(start, end);
  assert.match(body, /size: \[size, size, n\]/);
  assert.match(body, /dimension: "2d", baseArrayLayer: i, arrayLayerCount: 1/);
  assert.match(body, /dimension: "2d-array", baseArrayLayer: 0, arrayLayerCount: n/);
});

test("each cascade depth pass writes and reads its own uniform buffer (no shared-buffer aliasing across passes in one frame)", () => {
  const start = webgpuSource.indexOf("function renderShadowPass(encoder, lightMatrix, bundle, shadowResource, pbrBuffers, cascadeIndex, passBufferIndex)");
  assert.notEqual(start, -1);
  const end = webgpuSource.indexOf("\n    }\n", start);
  const body = webgpuSource.slice(start, end);
  assert.match(body, /shadowPassUniformBuffers\[passBufferIndex % SCENE_WEBGPU_SHADOW_PASS_BUFFER_COUNT\]/);
  assert.match(body, /device\.queue\.writeBuffer\(passBuffer, 0, lightMatrix\)/);
});

// --- WGSL compile (skipped when naga is absent) ---------------------------------

function createContext() {
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math, JSON, Number, Object, Array, String, Boolean,
    Float32Array, Uint32Array, Uint8Array, Int32Array, ArrayBuffer, DataView,
    Promise, Error, Map, Set, parseInt, parseFloat, isFinite,
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
  vm.runInContext(readSource("11-scene-math.js"), context, { filename: "11-scene-math.js" });
  vm.runInContext(readSource("17-scene-input.js"), context, { filename: "17-scene-input.js" });
  vm.runInContext(webgpuSource, context, { filename: "16a-scene-webgpu.js" });
  return { context, sandbox };
}

function nagaAvailable() {
  try {
    execFileSync("naga", ["--version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

const hasNaga = nagaAvailable();

function compileCheck(name, symbol) {
  test(
    `the ${name} WGSL compiles`,
    { skip: hasNaga ? false : "naga is not on PATH" },
    () => {
      const { context } = createContext();
      const code = vm.runInContext(symbol, context);
      assert.equal(typeof code, "string");
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-wgsl-"));
      const file = path.join(dir, symbol + ".wgsl");
      fs.writeFileSync(file, code);
      try {
        execFileSync("naga", [file], { stdio: "pipe" });
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    },
  );
}

compileCheck("PBR fragment (shadow cascade sampling)", "WGSL_PBR_FRAGMENT");
compileCheck("shadow depth-pass vertex", "WGSL_SHADOW_VERTEX");
compileCheck("shadow depth-pass instanced vertex", "WGSL_SHADOW_INSTANCED_VERTEX");
