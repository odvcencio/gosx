"use strict";
// The shadow a scene draws with no softness set is the same on WebGL and
// WebGPU: the same four taps, centred, with y running the same way, and a
// shadow map of the same size. Each test pins one of those.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

function read(file) { return fs.readFileSync(path.join(__dirname, file), "utf8"); }

// kernel reads the four-entry table named name from shader text: the numbers of
// each vec2(x, y), in order.
function kernel(text, name) {
  const at = text.indexOf(name);
  assert.ok(at >= 0, name + " is missing");
  const end = text.indexOf(");", at);
  const body = text.slice(at, end);
  const taps = [...body.matchAll(/vec2f?\(\s*(-?\d*\.?\d+)\s*,\s*(-?\d*\.?\d+)\s*\)/g)].map((m) => [Number(m[1]), Number(m[2])]);
  return taps;
}

// The WebGPU source set's text: the WGSL is in string literals, and kernel reads it as it reads GLSL.
function wgslSource() { return readSceneRendererBackendSrc("webgpu"); }

test("both renderers' no-softness shadow takes four taps whose offsets sum to zero", () => {
  const wgsl = kernel(wgslSource(), "kShadowHard4");
  const glsl = kernel(readSceneRendererBackendSrc("webgl"), "kShadowHard4");
  assert.equal(wgsl.length, 4);
  assert.equal(glsl.length, 4);
  for (const taps of [wgsl, glsl]) {
    const sum = taps.reduce((a, t) => [a[0] + t[0], a[1] + t[1]], [0, 0]);
    assert.ok(Math.abs(sum[0]) < 1e-9 && Math.abs(sum[1]) < 1e-9, "taps are centred: sum " + sum);
  }
});

test("the WebGL taps are the WebGPU taps with y turned over, so both land on the same texel", () => {
  // WebGPU reads v = 0.5 - ndc.y * 0.5 and WebGL reads t = ndc.y * 0.5 + 0.5.
  const wgsl = kernel(wgslSource(), "kShadowHard4");
  const glsl = kernel(readSceneRendererBackendSrc("webgl"), "kShadowHard4");
  assert.deepEqual(glsl.map((t) => [t[0], -t[1]]), wgsl);
});

test("the soft path keeps WebGPU's eight-tap disk", () => {
  assert.equal(kernel(wgslSource(), "kShadowDisk8").length, 8);
});

test("the shadow map's size is one function of the request, the cap and the cascade count", () => {
  const source = read("bootstrap-src/15a-scene-postfx-shared.ts");
  const start = source.indexOf("function resolveShadowSize");
  const context = vm.createContext({});
  vm.runInContext(source.slice(start, source.indexOf("function sceneFiniteNumber")) + "\nglobalThis.size = resolveShadowSize;", context);
  const size = context.size;
  assert.equal(size(1536, 2048 * 2048, 1), 1536);
  assert.equal(size(1536, 2048 * 2048, 2), 1448, "the cap covers every cascade layer of the light");
  assert.equal(size(1536, 2048 * 2048, 4), 1024);
  assert.equal(size(1024, 0, 2), 1024, "no cap given: the 1024 square default, not divided");
  assert.equal(size(2048, 0, 1), 1024);
  assert.equal(size(512, 2048 * 2048), 512);
  for (const [backend, pattern] of [["webgl", /resolveShadowSize\([^;]*[Cc]ascades/], ["webgpu", /resolveShadowSize\([^;]*cascades/]]) {
    assert.match(readSceneRendererBackendSrc(backend), pattern, backend + " sizes its shadow map with the cascade count");
  }
});

// --- WebGL: the comparison WebGPU's linear sampler makes, done by hand -----

const { createContext } = require("./runtime-test-harness.js");
const ts = require("node:module").createRequire(path.join(__dirname, "../runtime/package.json"))("typescript");

test("the no-softness path takes four taps of a hand-made 2 by 2 comparison and no sampler of its own", () => {
  const source = readSceneRendererBackendSrc("webgl");
  const start = source.indexOf('"float shadowFactor(');
  const body = source.slice(start, source.indexOf("Cascaded-shadow dispatchers", start));
  const hard = body.slice(body.indexOf("softness <= 0.0001"), body.indexOf("Blocker search"));
  assert.equal(kernel(source, "kShadowHard4").length, 4);
  assert.match(hard, /for \(int i = 0; i < 4; i\+\+\)/, "the loop runs once for each of the four taps");
  assert.equal((hard.match(/shadowTap\(/g) || []).length, 1, "one tap in the loop body");
  assert.doesNotMatch(hard, /texture\(/, "the hard path uses the raw sampler's texelFetch only");
  const tap = source.slice(source.indexOf('"float shadowTap('), source.indexOf('"float shadowFactor('));
  assert.equal((tap.match(/shadowLit\(shadowMap, t/g) || []).length, 4, "each tap reads the 2 by 2 texels around it: 4 fetches, 16 in all");
  assert.match(source, /reference < texelFetch\(shadowMap/, "lit when the reference is nearer, as WebGPU's \"less\"");
  assert.match(tap, /clamp\(uv, vec2\(0\.0\), vec2\(1\.0\)\)/, "taps clamp to the map as WebGPU's address mode does");
  assert.match(tap, /- 0\.5/, "the footprint is the one a linear filter uses: floor of uv * size - 0.5");
  assert.match(hard, /clamp\(receiverDepth - bias, 0\.0, 1\.0\)/, "the reference is WebGPU's: receiver depth less the bias, clamped to 0..1");
});

// samplerCount counts the sampler uniforms a GLSL source declares once its
// "#if GOSX_HDR_IBL" blocks are resolved for the value the source defines.
function samplerCount(source) {
  const on = /#define GOSX_HDR_IBL 1/.test(source);
  let skipping = [], count = 0;
  for (const line of source.split("\n")) {
    const t = line.trim();
    if (t.startsWith("#if GOSX_HDR_IBL")) { skipping.push(!on); continue; }
    if (t.startsWith("#else")) { skipping[skipping.length - 1] = !skipping[skipping.length - 1]; continue; }
    if (t.startsWith("#endif")) { skipping.pop(); continue; }
    if (skipping.some(Boolean)) continue;
    const m = /^uniform\s+(?:(?:highp|mediump|lowp)\s+)?sampler\w*\s+\w+(?:\[(\d+)\])?\s*;/.exec(t);
    if (m) count += m[1] ? Number(m[1]) : 1;
  }
  return count;
}

test("the WebGL PBR fragment program declares at most 16 samplers, the minimum WebGL2 guarantees", () => {
  // A program links only if its samplers (not its distinct units) fit MAX_TEXTURE_IMAGE_UNITS, and WebGL2
  // guarantees 16. A 17th sampler links on a 32-unit desktop GPU and fails on a 16-unit one (Apple GPUs report
  // 16), where the renderer falls back to webgl-shader-failed and draws no shadow. So count them here.
  const env = createContext({});
  for (const file of ["10-runtime-primitives.ts", "10-runtime-scene-utils.ts", "11-scene-math.ts", "13-scene-material.ts", "16c1-scene-detail.ts"]) {
    vm.runInContext(read("bootstrap-src/" + file), env.context);
  }
  const core = read("bootstrap-src/10-runtime-scene-core.ts");
  vm.runInContext(core.slice(0, core.indexOf("// Scene3D shared API")), env.context);
  vm.runInContext(ts.transpileModule(readSceneRendererBackendSrc("webgl"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, env.context);
  const c = env.context;
  const base = vm.runInContext("SCENE_PBR_FRAGMENT_SOURCE", c);
  const gl16 = { MAX_TEXTURE_IMAGE_UNITS: 16, getParameter: () => 16 };
  const variants = { "base": base, "with detail": c.sceneWebGLDetailFragment(base, true) };
  for (const [name, source] of Object.entries(variants)) {
    const resolved = c.scenePBRFragmentSourceForContext(gl16, source);
    const n = samplerCount(resolved);
    assert.ok(n >= 12, name + ": the counter found only " + n + " samplers, so it is not reading the program");
    assert.ok(n <= 16, name + " declares " + n + " samplers on a 16-unit device: a program links only if its samplers fit MAX_TEXTURE_IMAGE_UNITS, and 16 is the minimum WebGL2 guarantees");
  }
  assert.equal(samplerCount(c.scenePBRFragmentSourceForContext(gl16, base)), 16, "the base program is full: adding a sampler needs another to go");
});
