// shader-f16 tests for the WebGPU post chain (16a-scene-webgpu.js).
//
// The renderer negotiates shader-f16 through 16z and, until now, used it
// nowhere. Half precision cuts register pressure and lets a GPU issue two lanes
// per cycle, which matters most on the integrated and mobile parts.
//
// The claim these tests defend is narrow and checkable: f16 is applied only
// where the value range is bounded by the render target, and every f16 shader
// has an f32 twin for a device that never negotiated the feature.
//
// What runs here and what does not:
//   - The variant selection and the preamble run for real in a node:vm context.
//   - Every shader variant is compiled by naga when naga is on PATH, and also
//     lowered to SPIR-V and MSL, which catches a construct one backend rejects.
//   - No WebGPU adapter exists here, so nothing below reads a pixel. Whether
//     half precision is FASTER is not measured; only that it is valid, bounded
//     and reversible.

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
const webgpuSource = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

// loadShaderVariants evaluates the preamble helper and the two shader bodies out
// of the shipped source, then builds all four variants exactly as the renderer
// does. It lifts real text, not a copy.
function loadShaderVariants() {
  function grab(name) {
    const start = webgpuSource.indexOf("var " + name + " = [");
    assert.notEqual(start, -1, `${name} moved; update this loader`);
    const end = webgpuSource.indexOf("\n  ];", start);
    assert.notEqual(end, -1);
    return webgpuSource.slice(start, end + 5);
  }
  const preambleStart = webgpuSource.indexOf("function sceneWebGPUPostPrecisionPreamble");
  assert.notEqual(preambleStart, -1);
  const preamble = webgpuSource.slice(preambleStart, webgpuSource.indexOf("var WGSL_POST_BLUR_BODY"));
  const sandbox = {};
  vm.runInNewContext(
    [
      preamble,
      grab("WGSL_POST_BLUR_BODY"),
      grab("WGSL_POST_FXAA_BODY"),
      "out = {",
      "  blurF32: sceneWebGPUPostShaderSource(WGSL_POST_BLUR_BODY, false),",
      "  blurF16: sceneWebGPUPostShaderSource(WGSL_POST_BLUR_BODY, true),",
      "  fxaaF32: sceneWebGPUPostShaderSource(WGSL_POST_FXAA_BODY, false),",
      "  fxaaF16: sceneWebGPUPostShaderSource(WGSL_POST_FXAA_BODY, true),",
      "};",
    ].join("\n"),
    sandbox,
  );
  return sandbox.out;
}

const variants = loadShaderVariants();

// --- The variant is a preamble, not a second copy ----------------------------

test("the f32 and f16 variants differ only in the preamble", () => {
  for (const [f32Name, f16Name] of [["blurF32", "blurF16"], ["fxaaF32", "fxaaF16"]]) {
    const f32Body = variants[f32Name].split("\n").slice(3).join("\n");
    const f16Body = variants[f16Name].split("\n").slice(4).join("\n");
    assert.equal(f32Body, f16Body, `${f16Name} must reuse the ${f32Name} body verbatim`);
  }
});

test("the f16 variant enables the extension before anything else", () => {
  for (const name of ["blurF16", "fxaaF16"]) {
    const lines = variants[name].split("\n");
    assert.equal(lines[0], "enable f16;", "WGSL requires enable directives first");
    assert.equal(lines[1], "alias pf = f16;");
    assert.equal(lines[2], "alias pf3 = vec3h;");
  }
});

test("the f32 variant carries no f16 type anywhere", () => {
  for (const name of ["blurF32", "fxaaF32"]) {
    assert.ok(!variants[name].includes("f16"), `${name} must be free of f16`);
    assert.ok(!variants[name].includes("vec3h"), `${name} must be free of vec3h`);
    assert.match(variants[name], /alias pf = f32;/);
  }
});

// --- The bounded-range argument ----------------------------------------------

test("every post render target is an 8-bit canvas format, which bounds the range", () => {
  // This is the reason half precision is exact here rather than merely cheaper.
  // An f16 carries an 11-bit significand; the target stores 8 bits.
  const start = webgpuSource.indexOf("function ensureFBOs(");
  assert.notEqual(start, -1);
  const body = webgpuSource.slice(start, start + 1600);
  assert.match(body, /sceneTex = device\.createTexture\(\{ size: \[width, height, 1\], format: targetFormat/);
  assert.match(body, /auxTex = device\.createTexture\(\{ size: \[width, height, 1\], format: targetFormat/);
  const bloomStart = webgpuSource.indexOf("function ensureBloomPingPong(");
  const bloomBody = webgpuSource.slice(bloomStart, bloomStart + 900);
  assert.match(bloomBody, /pingPongA = device\.createTexture\(\{ size: \[w, h, 1\], format: targetFormat/);
  assert.match(bloomBody, /pingPongB = device\.createTexture\(\{ size: \[w, h, 1\], format: targetFormat/);
});

test("the blur weights sum to one, so the half-precision accumulator cannot overflow", () => {
  const weights = variants.blurF32.match(/alias|0\.227027|0\.1945946|0\.1216216|0\.054054|0\.016216/g) || [];
  assert.ok(weights.length > 0);
  const total = 0.227027 + 2 * (0.1945946 + 0.1216216 + 0.054054 + 0.016216);
  assert.ok(Math.abs(total - 1) < 1e-5, `the nine taps must sum to 1, got ${total}`);
});

test("the stages where half precision bites are still f32", () => {
  // Tone mapping multiplies by exposure before clamping, so it handles values
  // above 1 by design. SSAO and DOF reconstruct view-space depth. None of the
  // three may carry the f16 aliases.
  for (const name of ["WGSL_POST_TONEMAPPING_FRAGMENT", "WGSL_POST_SSAO_FRAGMENT", "WGSL_POST_DOF_FRAGMENT"]) {
    const start = webgpuSource.indexOf("var " + name + " = [");
    assert.notEqual(start, -1, `${name} must still exist`);
    const body = webgpuSource.slice(start, webgpuSource.indexOf("].join(\"\\n\");", start));
    assert.ok(!body.includes("pf3("), `${name} must stay f32`);
    assert.ok(!body.includes("enable f16"), `${name} must stay f32`);
  }
  // And the PBR fragment, where a GGX alpha squared at low roughness underflows
  // the smallest normal f16.
  assert.ok(!webgpuSource.includes("enable f16;\",\n    \"alias"), "the PBR shader must not enable f16");
});

// --- Variant selection ------------------------------------------------------

function loadPrecisionMode() {
  const startMark = "  function sceneWebGPUDeviceHasF16(device) {";
  const endMark = "  function wgpuCreatePostProcessor(";
  const start = webgpuSource.indexOf(startMark);
  assert.notEqual(start, -1, "the precision helpers moved; update this loader");
  const body = webgpuSource.slice(start, webgpuSource.indexOf(endMark, start));
  const sandbox = { window: {} };
  sandbox.window.window = sandbox.window;
  vm.runInNewContext(body + "\nout = { hasF16: sceneWebGPUDeviceHasF16, mode: sceneWebGPUPostPrecisionMode };", sandbox);
  return { api: sandbox.out, win: sandbox.window };
}

function deviceWith(features) {
  return { features: { has: (name) => features.includes(name) } };
}

test("a device that negotiated shader-f16 gets the half-precision chain", () => {
  const { api } = loadPrecisionMode();
  assert.equal(api.mode(deviceWith(["shader-f16"])), "f16");
});

test("a device without shader-f16 falls back honestly", () => {
  const { api } = loadPrecisionMode();
  assert.equal(api.mode(deviceWith(["timestamp-query"])), "f32");
  assert.equal(api.mode(deviceWith([])), "f32");
  assert.equal(api.mode(null), "f32");
  assert.equal(api.mode({}), "f32");
  // A device whose features set has no has() method must not be guessed at.
  assert.equal(api.mode({ features: ["shader-f16"] }), "f32");
});

test("a page can force f32 even on a device that supports f16", () => {
  const { api, win } = loadPrecisionMode();
  win.__gosx_scene3d_webgpu_post_f16 = false;
  assert.equal(api.mode(deviceWith(["shader-f16"])), "f32-forced");
});

test("the post processor resolves the variant once, not per frame", () => {
  const start = webgpuSource.indexOf("function wgpuCreatePostProcessor(");
  assert.notEqual(start, -1);
  const head = webgpuSource.slice(start, start + 500);
  assert.match(head, /var postPrecisionMode = sceneWebGPUPostPrecisionMode\(device\);/,
    "resolving per frame would call into the device features set every frame");
  assert.match(head, /var postUsesF16 = postPrecisionMode === "f16";/);
});

test("the precision mode reaches the diagnostics attributes", () => {
  assert.ok(webgpuSource.includes("data-gosx-scene3d-webgpu-post-precision"));
  assert.match(webgpuSource, /postPrecision: postPrecisionMode/);
});

// --- Compilation (skipped when naga is absent) -------------------------------

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
  "all four post variants validate, and lower to SPIR-V and MSL",
  { skip: hasNaga ? false : "naga is not on PATH" },
  () => {
    // Validation alone misses constructs a backend rejects. The light agent
    // found real issues by lowering; do the same for both precisions.
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-f16-wgsl-"));
    try {
      for (const [name, code] of Object.entries(variants)) {
        const file = path.join(dir, name + ".wgsl");
        fs.writeFileSync(file, code);
        execFileSync("naga", [file], { stdio: "pipe" });
        execFileSync("naga", [file, path.join(dir, name + ".spv")], { stdio: "pipe" });
        execFileSync("naga", [file, path.join(dir, name + ".metal")], { stdio: "pipe" });
      }
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  },
);
