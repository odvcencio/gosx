"use strict";
// Selena scene lighting block: the per-frame engine inputs both renderers
// supply to Selena mesh materials (lights, shadow cascades, fog), packed as
// Selena's docs/engine-inputs.md#scene-lighting describes.
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const path = require("node:path");
const {readSceneRendererBackendSrc} = require("./scene3d-renderer-source-set.js");

function fn(source, name) {
  const start = source.indexOf("function " + name + "(");
  assert.ok(start >= 0, name);
  let depth = 0;
  for (let i = source.indexOf("{", start); i < source.length; i++) {
    if (source[i] === "{") depth++;
    if (source[i] === "}" && --depth === 0) return source.slice(start, i + 1);
  }
  throw new Error("unclosed " + name);
}

function decl(source, name) {
  const start = source.indexOf("var " + name + " = ");
  assert.ok(start >= 0, name);
  return source.slice(start, source.indexOf(";\n", start) + 1);
}

const shared = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16c2-scene-selena-lighting.ts"), "utf8");
const lightingFunctions = ["sceneSelenaLightingRecord", "sceneSelenaLightKind", "sceneSelenaPackLight", "sceneSelenaClipToShadowUV",
  "sceneSelenaFillShadowSlot", "sceneSelenaFillLighting", "sceneSelenaLightingValue", "sceneSelenaShadowMapSlot",
  "sceneSelenaFrameLighting", "sceneSelenaWGPUTextureLayout"];

function lightingContext(extra = {}) {
  const context = vm.createContext(Object.assign({
    Float32Array, Int32Array, Math, Object, Array, String,
    sceneNumber: (v, d) => (Number.isFinite(Number(v)) && v !== null && v !== "" ? Number(v) : d),
    sceneColorRGBA: (value, fallback) => Array.isArray(value) ? value : fallback,
  }, extra));
  vm.runInContext(["SCENE_SELENA_LIGHT_CAPACITY", "SCENE_SELENA_CASCADE_CAPACITY", "SCENE_SELENA_FAR_SPLIT", "SCENE_SELENA_LIGHTING_NAMES"]
    .map((n) => decl(shared, n)).concat(lightingFunctions.map((n) => fn(shared, n))).join("\n"), context);
  return context;
}

// Objects built inside the VM have another realm's prototypes.
const plain = (v) => JSON.parse(JSON.stringify(v));
const close = (a, b, msg) => assert.ok(Math.abs(a - b) < 1e-5, `${msg}: ${a} != ${b}`);

test("lights pack in the Selena layout with the key light, ambient, fog and camera", () => {
  const ctx = lightingContext();
  const view = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, -1, -2, -3, 1]);
  const lights = [
    {kind: "rect-area", intensity: 9},
    {kind: "directional", directionX: 0, directionY: -2, directionZ: 0, color: [1, 0.5, 0.25, 1], intensity: 2},
    {kind: "point", x: 1, y: 2, z: 3, color: [0, 1, 0, 1], intensity: 3, range: 6},
    {kind: "spot", x: 0, y: 5, z: 0, directionY: -1, angle: 0.6, penumbra: 0.5, decay: 1.5},
    {kind: "hemisphere", color: [0.4, 0.6, 1, 1], groundColor: [0.2, 0.1, 0, 1], intensity: 0.5},
    {kind: "light-probe", color: [0.1, 0.1, 0.1, 1]},
  ];
  const env = {ambientColor: [1, 0.5, 0, 1], ambientIntensity: 0.5, fogColor: [0.3, 0.4, 0.5, 1], fogDensity: 0.02};
  const out = ctx.sceneSelenaFillLighting(null, {cam: {x: 1, y: 2, z: 3}, view, lights, environment: env, clip: "gl"});
  assert.equal(out.lightCount, 5, "rect-area is skipped; the light probe folds into ambient (kind 0)");
  assert.deepEqual(Array.from(out.cameraPos), [1, 2, 3]);
  assert.deepEqual(Array.from(out.cameraForward).map((v) => v + 0), [0, 0, -1]);
  assert.deepEqual(Array.from(out.ambient), [0.5, 0.25, 0]);
  assert.deepEqual(Array.from(out.fog), [0.3, 0.4, 0.5, 0.02].map(Math.fround));
  assert.deepEqual(Array.from(out.sunDir).map((v) => v + 0), [0, 1, 0], "sunDir points toward the first directional light");
  assert.deepEqual(Array.from(out.sunColor), [2, 1, 0.5]);
  const L = (i, k) => Array.from(out.lights.slice(i * 16 + k * 4, i * 16 + k * 4 + 4));
  assert.deepEqual(L(0, 0), [0, 0, 0, 1]);
  assert.deepEqual(L(0, 1), [0, -2, 0, 0]);
  assert.deepEqual(L(1, 0), [1, 2, 3, 2]);
  assert.deepEqual(L(1, 1)[3], 6, "range rides in [4i+1].w");
  assert.deepEqual(L(1, 2), [0, 3, 0, 2], "color times intensity, default decay 2");
  close(L(2, 3)[0], Math.cos(0.6), "spot outer cone");
  close(L(2, 3)[1], Math.cos(0.3), "spot inner cone");
  assert.equal(L(2, 2)[3], 1.5);
  assert.deepEqual(L(3, 3).slice(0, 3).map((v) => +v.toFixed(5)), [0.1, 0.05, 0], "hemisphere ground color times intensity");
  assert.equal(L(4, 0)[3], 0);
  for (let i = 0; i < 5; i++) assert.equal(L(i, 3)[3], -1, `light ${i} casts no shadow`);
});

test("the light capacity is eight, and fill allocates nothing after the first frame", () => {
  const ctx = lightingContext();
  const lights = Array.from({length: 12}, (_, i) => ({kind: "point", x: i}));
  const holder = {};
  const first = ctx.sceneSelenaFrameLighting(holder, {}, null, {lights}, null, null, null, "gl");
  assert.equal(first.lightCount, 8);
  const lightsArray = first.lights, input = holder._gosxSelenaLightingInput;
  const second = ctx.sceneSelenaFrameLighting(holder, {}, null, {lights: lights.slice(0, 2)}, null, null, null, "gl");
  assert.equal(second, first);
  assert.equal(second.lights, lightsArray);
  assert.equal(holder._gosxSelenaLightingInput, input);
  assert.equal(second.lightCount, 2);
  assert.equal(second.lights[2 * 16 + 3], 0, "stale lights are cleared");
});

// The contract: after the renderer's own clip-to-texture mapping, uvz.xy is a
// texture coordinate and uvz.z compares with stored depth. These references are
// the renderers' own shader math: WebGL shadowFactor (projCoords * 0.5 + 0.5)
// and WebGPU shadowProjectedCoords (x * 0.5 + 0.5, 0.5 - y * 0.5, z).
function project(m, p) {
  const out = [0, 0, 0, 0];
  for (let r = 0; r < 4; r++) out[r] = m[r] * p[0] + m[4 + r] * p[1] + m[8 + r] * p[2] + m[12 + r];
  return out;
}

test("shadow matrices carry each renderer's clip-to-texture mapping", () => {
  const ctx = lightingContext();
  // A perspective-like matrix so w is not 1 (spot lights use one).
  const light = new Float32Array([0.8, 0.1, 0.05, 0.02, -0.2, 0.9, 0.1, 0.03, 0.1, 0.2, 0.7, 0.4, 0.3, -0.1, 0.2, 1.2]);
  const points = [[0.2, 0.4, -0.3], [-1, 0.5, 2], [0.7, -0.6, 0.1]];
  const out = ctx.sceneSelenaLightingRecord();
  for (const clip of ["gl", "gpu"]) {
    ctx.sceneSelenaClipToShadowUV(out.shadowMatrices0, 0, light, clip);
    for (const p of points) {
      const s = project(out.shadowMatrices0, p);
      const c = project(light, p);
      const ndc = [c[0] / c[3], c[1] / c[3], c[2] / c[3]];
      const want = clip === "gl"
        ? [ndc[0] * 0.5 + 0.5, ndc[1] * 0.5 + 0.5, ndc[2] * 0.5 + 0.5]
        : [ndc[0] * 0.5 + 0.5, 0.5 - ndc[1] * 0.5, ndc[2]];
      for (let k = 0; k < 3; k++) close(s[k] / s[3], want[k], `${clip} uvz[${k}] at ${p}`);
    }
  }
});

test("WebGL cascades and WebGPU single maps fill both shadow slots", () => {
  const ctx = lightingContext();
  const ident = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
  const lights = [{kind: "point"}, {kind: "directional", shadowBias: 0.002, shadowSoftness: 0.5}, {kind: "spot", angle: 0.5}];
  const glSlot = {numCascades: 3, size: 2048, depthTexture: {}, cascades: [
    {lightMatrix: ident, splitFar: 8}, {lightMatrix: ident, splitFar: 30}, {lightMatrix: ident, splitFar: 90}]};
  const out = ctx.sceneSelenaFillLighting(null, {lights, slots: [glSlot, null], lightIndices: [1, -1], clip: "gl"});
  assert.deepEqual(Array.from(out.shadow0).map((v) => +v.toFixed(6)), [3, 0.002, +(5 / 2048).toFixed(6), 1]);
  assert.deepEqual(Array.from(out.shadowSplits0.slice(0, 2)), [8, 30], "far split of every cascade but the last");
  assert.ok(out.shadowSplits0[2] > 1e37, "the last cascade reaches to infinity");
  assert.equal(out.shadowMatrices0[16 * 2 + 12], 0.5, "cascade 2 has the gl mapping");
  assert.equal(out.shadowMatrices0[16 * 3 + 15], 0, "unused cascades are zero");
  assert.equal(out.lights[1 * 16 + 15], 0, "the casting light records its slot");
  assert.equal(out.shadowMaps[0], glSlot);
  assert.deepEqual(Array.from(out.shadow1), [0, 0.005, 1 / 1024, -1].map(Math.fround), "empty slot");
  assert.equal(out.shadowMaps[1], null);

  // WebGPU: one map per slot, matrix passed alongside; a spot can cast.
  const gpuSlot = {texture: {}, view: {}, size: 1024};
  const gpu = ctx.sceneSelenaFillLighting(null, {lights, slots: [gpuSlot, gpuSlot], lightIndices: [2, 1], matrices: [ident, ident], clip: "gpu"});
  assert.equal(gpu.shadow0[0], 1);
  assert.equal(gpu.shadow0[3], 2, "packed index of the spot light");
  assert.equal(gpu.shadow1[3], 1);
  assert.equal(gpu.shadowMatrices0[5], -0.5, "gpu mapping flips y");
  assert.equal(gpu.shadowMatrices0[10], 1, "gpu mapping keeps z");
  assert.equal(gpu.lights[2 * 16 + 15], 0);
  assert.equal(gpu.lights[1 * 16 + 15], 1);

  // A shadow light that was not packed (rect-area, or past capacity) leaves its slot empty.
  const skipped = ctx.sceneSelenaFillLighting(null, {lights: [{kind: "rect-area"}], slots: [gpuSlot], lightIndices: [0], matrices: [ident], clip: "gpu"});
  assert.equal(skipped.shadow0[0], 0);
  assert.equal(skipped.shadowMaps[0], null);
});

test("only engine context fields read the lighting record", () => {
  const ctx = lightingContext();
  const lighting = ctx.sceneSelenaLightingRecord();
  assert.equal(ctx.sceneSelenaLightingValue(lighting, {name: "lights", class: "context"}), lighting.lights);
  assert.equal(ctx.sceneSelenaLightingValue(lighting, {name: "lightCount", class: "context"}), 0);
  assert.equal(ctx.sceneSelenaLightingValue(lighting, {name: "lights"}), undefined, "an author param named lights keeps its value");
  assert.equal(ctx.sceneSelenaLightingValue(lighting, {name: "shadowMaps", class: "context"}), undefined, "renderer state is never a uniform");
  assert.equal(ctx.sceneSelenaLightingValue(lighting, {name: "resolution", class: "context"}), undefined, "host-custom context passes through");
  assert.equal(ctx.sceneSelenaLightingValue(null, {name: "fog", class: "context"}), undefined);
  lighting.shadowMaps[1] = {id: 1};
  assert.equal(ctx.sceneSelenaShadowMapSlot(lighting, {engine: "shadowMap1"}), lighting.shadowMaps[1]);
  assert.equal(ctx.sceneSelenaShadowMapSlot(lighting, {engine: "shadowMap0"}), null);
  assert.equal(ctx.sceneSelenaShadowMapSlot(null, {engine: "shadowMap0"}), null);
  assert.equal(ctx.sceneSelenaShadowMapSlot(lighting, {engine: "opaqueDepth"}), undefined);
  assert.deepEqual(plain(ctx.sceneSelenaWGPUTextureLayout({dimension: "depth2d-array"})), {sampleType: "depth", viewDimension: "2d-array"});
  assert.deepEqual(plain(ctx.sceneSelenaWGPUTextureLayout({dimension: "depth2d"})), {sampleType: "depth", viewDimension: "2d"});
  assert.deepEqual(plain(ctx.sceneSelenaWGPUTextureLayout({dimension: "cube"})), {sampleType: "float", viewDimension: "cube"});
  assert.deepEqual(plain(ctx.sceneSelenaWGPUTextureLayout({})), {sampleType: "float", viewDimension: "2d"});
});

test("WebGL uploads array fields whole and binds shadow maps as depth arrays", () => {
  const source = readSceneRendererBackendSrc("webgl");
  const calls = [];
  const ctx = lightingContext({
    textureCache: {_sceneTextureEpoch: 0, _gosxSelenaLighting: null},
    sceneSelenaTextureDescriptors: (l) => l.textures,
    scenePBRBindTexture: (gl, unit, texture, target) => calls.push({unit, texture, target}),
    scenePBRPlaceholderShadow: () => "placeholder-array",
    scenePBRPlaceholderCube: () => ({texture: "cube"}),
    scenePBRLoadTexture: () => null,
  });
  vm.runInContext(["sceneSelenaFloatCount", "sceneSelenaUploadScalar", "sceneSelenaUploadUniform", "sceneWebGLSelenaEnvironmentSlot",
    "sceneSelenaTextureURL", "sceneWebGLBindSelenaTextures"].map((n) => fn(source, n)).join("\n"), ctx);
  const uploads = [];
  const gl = {TEXTURE_2D: 3553, TEXTURE_2D_ARRAY: 35866, TEXTURE_CUBE_MAP: 34067,
    uniform4fv: (loc, data) => uploads.push(["4fv", Array.from(data)]), uniform1fv: (loc, data) => uploads.push(["1fv", Array.from(data)]),
    uniform4f: () => uploads.push(["4f"]), uniform1i: () => {}, getUniformLocation: () => ({})};
  ctx.sceneSelenaUploadUniform(gl, {}, "vec4", new Float32Array(128).fill(2), 32);
  assert.equal(uploads[0][0], "4fv");
  assert.equal(uploads[0][1].length, 128);
  ctx.sceneSelenaUploadUniform(gl, {}, "float", [1, 2], 3);
  assert.deepEqual(uploads[1], ["1fv", [1, 2, 0]], "short JS arrays pad with zeros");
  ctx.sceneSelenaUploadUniform(gl, {}, "vec4", [1, 2, 3, 4]);
  assert.deepEqual(uploads[2], ["4f"], "single values keep their path");

  const info = {program: {}, layout: {textures: [{name: "shadowMap0", engine: "shadowMap0", dimension: "depth2d-array", gl: {unit: 3}}]}};
  ctx.sceneWebGLBindSelenaTextures(gl, info, {}, ctx.textureCache, "placeholder-2d");
  assert.deepEqual(calls.at(-1), {unit: 3, texture: "placeholder-array", target: gl.TEXTURE_2D_ARRAY}, "an empty slot binds the lit placeholder array");
  const slot = {depthTexture: "slot-depth"};
  ctx.textureCache._gosxSelenaLighting = {shadowMaps: [slot, null]};
  ctx.sceneWebGLBindSelenaTextures(gl, info, {}, ctx.textureCache, "placeholder-2d");
  assert.deepEqual(calls.at(-1), {unit: 3, texture: "slot-depth", target: gl.TEXTURE_2D_ARRAY});
});

test("WebGPU resolves lighting uniforms and shadow-map array views", () => {
  const uniforms = readSceneRendererBackendSrc("webgpu");
  const created = [];
  const ctx = lightingContext({GPUTextureUsage: {TEXTURE_BINDING: 4}, sceneAffineNormalMatrix: () => [1, 0, 0, 0, 1, 0, 0, 0, 1]});
  vm.runInContext("var selenaIdentityMatrix4 = new Float32Array(16);\n" + ["sceneSelenaFloatCount", "sceneSelenaUniformDefault",
    "sceneSelenaRenderContextUniformValue", "sceneSelenaMaterialValue", "webGPUObjectModelMatrix", "webGPUSelenaObjectModelMatrix",
    "sceneSelenaUniformValue", "sceneSelenaShadowMapView"].map((n) => fn(uniforms, n)).join("\n"), ctx);
  const frame = {time: 1, cameraProximity: 0, _gosxSelenaLighting: ctx.sceneSelenaLightingRecord()};
  frame._gosxSelenaLighting.sunDir.set([0, 0, 1]);
  const field = (name, extra = {}) => Object.assign({name, class: "context", type: "vec3"}, extra);
  assert.equal(ctx.sceneSelenaUniformValue({}, {}, field("sunDir"), null, null, frame), frame._gosxSelenaLighting.sunDir,
    "WebGPU now supplies the key light like WebGL");
  assert.equal(ctx.sceneSelenaUniformValue({}, {}, field("cameraPos"), null, {uniforms: {cameraPos: [9, 9, 9]}}, frame)[0], 9,
    "a render context still wins");
  assert.equal(ctx.sceneSelenaUniformValue({customUniforms: {lights: 7}}, {}, {name: "lights", type: "vec4"}, null, null, frame), 7,
    "author params named like engine inputs keep their values");

  const device = {createTexture: (desc) => { created.push(desc); return {createView: (v) => ({placeholder: true, v})}; }};
  const context = {device, frame};
  const tex = {engine: "shadowMap0"};
  const placeholder = ctx.sceneSelenaShadowMapView(context, tex);
  assert.equal(placeholder.v.dimension, "2d-array");
  assert.equal(ctx.sceneSelenaShadowMapView(context, tex), placeholder, "the placeholder is created once");
  assert.equal(created.length, 1);
  assert.equal(created[0].format, "depth32float");
  const views = [];
  frame._gosxSelenaLighting.shadowMaps[0] = {texture: {createView: (v) => { views.push(v); return {slot: true}; }}};
  const view = ctx.sceneSelenaShadowMapView(context, tex);
  assert.ok(view.slot);
  assert.equal(ctx.sceneSelenaShadowMapView(context, tex), view, "the slot's array view is cached on the slot");
  assert.deepEqual(plain(views), [{dimension: "2d-array"}]);
  assert.equal(ctx.sceneSelenaShadowMapView(context, {name: "albedo"}), null);
});

test("renderer chunks bridge the shared lighting helpers", () => {
  const prefixes = {
    webgl: fs.readFileSync(path.join(__dirname, "bootstrap-src", "26j-feature-scene3d-webgl-prefix.ts"), "utf8"),
    webgpu: fs.readFileSync(path.join(__dirname, "bootstrap-src", "26e-feature-scene3d-webgpu-prefix.ts"), "utf8"),
  };
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");
  const used = {
    webgl: ["sceneSelenaFrameLighting", "sceneSelenaLightingValue", "sceneSelenaShadowMapSlot"],
    webgpu: ["sceneSelenaFrameLighting", "sceneSelenaLightingValue", "sceneSelenaShadowMapSlot", "sceneSelenaWGPUTextureLayout"],
  };
  for (const [backend, names] of Object.entries(used)) {
    const renderer = readSceneRendererBackendSrc(backend);
    for (const name of names) {
      assert.ok(renderer.includes(name + "("), `${backend} renderer calls ${name}`);
      assert.ok(prefixes[backend].includes(`var ${name} = sceneApi.${name};`), `${backend} prefix bridges ${name}`);
      assert.ok(core.includes(`${name}: typeof ${name} === "function" ? ${name} : undefined`), `core API exports ${name}`);
    }
  }
});
