"use strict";

// Regression tests for the instanced shadow pass and the shared shadow bounds
// helper. The ACTUAL production slices are executed in a VM: the shadow pass
// block from webgl.ts, the renderer's sceneInstancedColorBuffer, and
// sceneShadowComputeBounds from 16c-scene-shared-pbr.ts. GL is a recording
// mock (no permissive Proxy); the renderer-owned VBO cache and geometry
// resolver are injected as hooks, mirroring the real wiring.

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const RUNTIME_DIR = path.join(__dirname, "..", "runtime", "scene3d");
const BOOTSTRAP_DIR = path.join(__dirname, "bootstrap-src");

function read(name, dir) {
  return fs.readFileSync(path.join(dir || BOOTSTRAP_DIR, name), "utf8");
}

function sliceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  assert.ok(start >= 0, "start marker located: " + startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(end > start, "end marker located: " + endMarker);
  return source.slice(start, end);
}

const WEBGL = read("webgl.ts", RUNTIME_DIR);
const SHADOW_SOURCE = sliceBetween(WEBGL,
  "function sceneShadowPassHash",
  "// Compile the shadow depth shader program.");
const INSTANCED_COLOR_SOURCE = sliceBetween(WEBGL,
  "function sceneInstancedColorBuffer",
  "function drawInstancedMeshes");
const TEX_KEY_SOURCE = sliceBetween(WEBGL,
  "function scenePBRTextureDescriptor",
  "function scenePBRLoadTexture");
const BOUNDS_SOURCE = sliceBetween(read("16c-scene-shared-pbr.ts"),
  "function sceneShadowComputeBounds",
  "// Generate PBR vertex data");

const FRAGMENTS = [
  "10-runtime-primitives.ts",
  "10-runtime-scene-utils.ts",
  "11-scene-math.ts",
  "13-scene-material.ts",
];
const CORE = sliceBetween(read("10-runtime-scene-core.ts"),
  "function sceneCSSVarReference", "function sceneWaterResetClock");
const POSTFX = sliceBetween(read("15a-scene-postfx-shared.ts"),
  "function sceneFiniteNumber", "// Render-truth telemetry");
const SHADER_BUILDER = sliceBetween(WEBGL,
  "function sceneShadowFragmentSource",
  "// --- Points Shader Sources ---");

function createContext() {
  const context = vm.createContext({
    console, Math, Number, Boolean, String, Array, Object, JSON, isFinite,
    Float32Array,
  });
  for (const name of FRAGMENTS) {
    vm.runInContext(read(name), context, { filename: name });
  }
  vm.runInContext(CORE, context, { filename: "10-runtime-scene-core.ts#slice" });
  vm.runInContext(POSTFX, context, { filename: "15a-scene-postfx-shared.ts#slice" });
  vm.runInContext(TEX_KEY_SOURCE, context, { filename: "webgl.ts#tex-key" });
  vm.runInContext(SHADOW_SOURCE, context, { filename: "webgl.ts#shadow" });
  vm.runInContext(INSTANCED_COLOR_SOURCE, context, { filename: "webgl.ts#inst-color" });
  vm.runInContext(BOUNDS_SOURCE, context, { filename: "16c#bounds" });
  vm.runInContext(SHADER_BUILDER, context, { filename: "webgl.ts#shader-builder" });
  return context;
}

function createGL() {
  const calls = [];
  const gl = {
    calls,
    ACTIVE_TEXTURE: 0x84e0, TEXTURE0: 0x84c0, TEXTURE_BINDING_2D: 0x8069,
    FRAMEBUFFER: 0x8d40, DEPTH_BUFFER_BIT: 0x100, DEPTH_TEST: 0xb71,
    BLEND: 0xbe2, CULL_FACE: 0xb44, LEQUAL: 0x203, FRONT: 0x404, BACK: 0x405,
    TRIANGLES: 4, UNSIGNED_INT: 0x1405, ARRAY_BUFFER: 0x8892,
    STATIC_DRAW: 0x88e4, DYNAMIC_DRAW: 0x88e8, FLOAT: 0x1406,
    TEXTURE_2D: 0x0de1, MAX_VERTEX_ATTRIBS: 0x8869,
    _active: 0x84c0, _binding0: null,
    getParameter(p) {
      if (p === gl.MAX_VERTEX_ATTRIBS) return 16;
      if (p === gl.ACTIVE_TEXTURE) return gl._active;
      if (p === gl.TEXTURE_BINDING_2D) return gl._binding0;
      return null;
    },
    activeTexture(u) { calls.push(["activeTexture", u]); gl._active = u; },
    bindTexture(t, tex) {
      calls.push(["bindTexture", t, tex]);
      if (gl._active === gl.TEXTURE0) gl._binding0 = tex;
    },
    bindFramebuffer(t, f) { calls.push(["bindFramebuffer", t, f]); },
    viewport(x, y, w, h) { calls.push(["viewport", x, y, w, h]); },
    clearDepth(d) { calls.push(["clearDepth", d]); },
    clear(m) { calls.push(["clear", m]); },
    useProgram(p) { calls.push(["useProgram", p]); },
    uniformMatrix4fv(l, tr) { calls.push(["uniformMatrix4fv", l, tr]); },
    uniform1i(l, v) { calls.push(["uniform1i", l, v]); },
    uniform1f(l, v) { calls.push(["uniform1f", l, v]); },
    enable(c) { calls.push(["enable", c]); },
    disable(c) { calls.push(["disable", c]); },
    depthMask(v) { calls.push(["depthMask", v]); },
    depthFunc(f) { calls.push(["depthFunc", f]); },
    cullFace(f) { calls.push(["cullFace", f]); },
    bindBuffer(t, b) { calls.push(["bindBuffer", t, b]); },
    bufferData(t, data, usage) {
      calls.push(["bufferData", t, ArrayBuffer.isView(data) ? data.slice() : data, usage]);
    },
    enableVertexAttribArray(i) { calls.push(["enableVertexAttribArray", i]); },
    disableVertexAttribArray(i) { calls.push(["disableVertexAttribArray", i]); },
    vertexAttribPointer(i, s, ty, n, st, o) { calls.push(["vertexAttribPointer", i, s, ty, n, st, o]); },
    vertexAttribDivisor(i, d) { calls.push(["vertexAttribDivisor", i, d]); },
    vertexAttrib2f(i, x, y) { calls.push(["vertexAttrib2f", i, x, y]); },
    vertexAttrib4f(i, x, y, z, w) { calls.push(["vertexAttrib4f", i, x, y, z, w]); },
    drawArrays(m, f, c) { calls.push(["drawArrays", m, f, c]); },
    drawArraysInstanced(m, f, c, ic) { calls.push(["drawArraysInstanced", m, f, c, ic]); },
    drawElements(m, c, t, o) { calls.push(["drawElements", m, c, t, o]); },
    createBuffer() { return { id: "buf" + calls.length }; },
    deleteBuffer() {},
  };
  return gl;
}

function of(calls, name) {
  return calls.filter(function(c) { return c[0] === name; });
}

function hasCall(calls, expected) {
  return calls.some(function(c) {
    return c.length === expected.length &&
      expected.every(function(v, i) { return c[i] === v; });
  });
}

function makeEnv() {
  const gl = createGL();
  const context = createContext();
  const sceneInstancedColorBuffer = vm.runInContext("sceneInstancedColorBuffer", context);
  const computeBounds = vm.runInContext("sceneShadowComputeBounds", context);
  const renderPass = vm.runInContext(
    "(function() { return function __run(gl, program, resources, lightMatrix, bundle, shadowState, bindIndexedCaster, textureCache, resolveInstancedGeometry) {" +
    "  return renderSceneShadowPass(gl, program, resources, lightMatrix, bundle, shadowState, bindIndexedCaster, textureCache, resolveInstancedGeometry);" +
    "}; })()",
    context);

  const vboCache = new Map();
  const uploads = [];
  let vboSeq = 0;
  // Mirrors the renderer's ensureStaticArrayVBO: identity-keyed cache, one
  // STATIC_DRAW upload per new array, buffers owned by the shared cache.
  function ensureInstancedVBO(data) {
    let buf = vboCache.get(data);
    if (buf) return buf;
    buf = { id: "vbo" + (++vboSeq) };
    vboCache.set(data, buf);
    gl.bindBuffer(gl.ARRAY_BUFFER, buf);
    gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
    uploads.push({ buf, data: data.slice() });
    return buf;
  }

  const geometry = {
    positions: Float32Array.from([-0.5, -0.5, 0, 0.5, -0.5, 0, 0, 0.5, 0]),
    uvs: null,
    vertexCount: 3,
  };
  const bundle = {
    meshObjects: [],
    instancedMeshes: [],
    materials: [null],
    worldMeshPositions: new Float32Array(0),
  };
  const shadowState = {
    buffer: { id: "shadow-scratch" },
    fallbackTexture: null,
    ensureInstancedVBO,
    instanceColorData: sceneInstancedColorBuffer,
    instancedShadowProgram: {
      program: { id: "inst-shadow" },
      attributes: { position: 1, uv: 2, instanceMatrix: [5, 6, 7, 8], instanceColor: 9 },
      uniforms: { lightViewProjection: "ilvp", albedoMap: "iam", hasAlbedoMap: "iham", opacity: "iop", alphaCutoff: "iac" },
    },
  };
  const resources = { framebuffer: { id: "fbo" }, size: 256 };
  const staticProgram = {
    program: { id: "shadow" },
    attributes: { position: 0, uv: 1 },
    uniforms: { lightViewProjection: "lvp", albedoMap: "am", hasAlbedoMap: "ham", opacity: "op", alphaCutoff: "ac" },
  };
  const lightMatrix = new Float32Array(16).fill(0.5);
  function run(textureCache) {
    return renderPass(gl, staticProgram, resources, lightMatrix, bundle, shadowState,
      null, textureCache || new Map(), function() { return geometry; });
  }
  return { gl, calls: gl.calls, bundle, shadowState, geometry, run, computeBounds, context, uploads };
}

test("one instanced draw over two matrices with per-instance attribute setup", () => {
  const env = makeEnv();
  env.bundle.instancedMeshes = [{ castShadow: true, instanceCount: 2, transforms: new Float32Array(32) }];
  env.run();
  const draws = of(env.calls, "drawArraysInstanced");
  assert.equal(draws.length, 1);
  assert.deepEqual(draws[0].slice(1), [env.gl.TRIANGLES, 0, 3, 2]);
  for (let c = 0; c < 4; c++) {
    const ptr = env.calls.find(function(ca) {
      return ca[0] === "vertexAttribPointer" && ca[1] === 5 + c;
    });
    assert.deepEqual(ptr, ["vertexAttribPointer", 5 + c, 4, env.gl.FLOAT, false, 64, c * 16]);
  }
  for (let loc = 5; loc <= 9; loc++) {
    assert.ok(hasCall(env.calls, ["vertexAttribDivisor", loc, 0]), "divisor reset " + loc);
  }
});

test("instanced shadows draw full authored arrays despite viewCulled/cull config", () => {
  const env = makeEnv();
  env.bundle.instancedMeshes = [{
    castShadow: true, viewCulled: true, frustumCulled: true, culled: true,
    instanceCount: 2, transforms: new Float32Array(32),
  }];
  env.run();
  assert.equal(of(env.calls, "drawArraysInstanced").length, 1);
  assert.ok(env.uploads.some(function(u) { return u.data.length === 32; }),
    "full transform array uploaded, nothing frustum-compacted");
});

test("castShadow omitted or false skips instanced casters", () => {
  const env = makeEnv();
  env.bundle.instancedMeshes = [
    { instanceCount: 2, transforms: new Float32Array(32) },
    { castShadow: false, instanceCount: 2, transforms: new Float32Array(32) },
  ];
  env.run();
  assert.equal(of(env.calls, "drawArraysInstanced").length, 0);
});

test("instance count is floored and capped by available matrices", () => {
  const env = makeEnv();
  const cases = [
    { instanceCount: 2.9, transforms: new Float32Array(32), expected: 2 },
    { instanceCount: 9, transforms: new Float32Array(32), expected: 2 },
    { instanceCount: 0, transforms: new Float32Array(32), expected: 0 },
  ];
  for (const c of cases) {
    env.bundle.instancedMeshes = [Object.assign({ castShadow: true }, c)];
    env.calls.length = 0;
    env.run();
    const draws = of(env.calls, "drawArraysInstanced");
    assert.equal(draws.length, c.expected > 0 ? 1 : 0);
    if (draws.length) assert.equal(draws[0][4], c.expected);
  }
});

test("repeated instanced passes redraw; removal clears once then static hash skips", () => {
  const env = makeEnv();
  env.bundle.instancedMeshes = [{ castShadow: true, instanceCount: 2, transforms: new Float32Array(32) }];
  env.run();
  env.run();
  assert.equal(of(env.calls, "drawArraysInstanced").length, 2);

  env.bundle.instancedMeshes = [];
  env.calls.length = 0;
  env.run();
  assert.equal(of(env.calls, "drawArraysInstanced").length, 0);
  const clears = of(env.calls, "clear").length;
  assert.equal(clears, 1);
  env.run();
  assert.equal(of(env.calls, "clear").length, clears);
});

test("supplied instance colors use the shared cached array with divisor 1", () => {
  const env = makeEnv();
  const mesh = {
    castShadow: true, instanceCount: 2,
    transforms: new Float32Array(32),
    colors: Float32Array.from([0.25, 0.5, 0.75, 0.5, 1, 0, 0, 1]),
  };
  env.bundle.instancedMeshes = [mesh];
  env.run();
  const colorUploads = env.uploads.filter(function(u) { return u.data.length === 8; });
  assert.equal(colorUploads.length, 1);
  assert.deepEqual(Array.from(colorUploads[0].data), [0.25, 0.5, 0.75, 0.5, 1, 0, 0, 1]);
  assert.ok(ArrayBuffer.isView(mesh._cachedInstanceColors));
  assert.deepEqual(Array.from(mesh._cachedInstanceColors), [0.25, 0.5, 0.75, 0.5, 1, 0, 0, 1]);
  assert.ok(hasCall(env.calls, ["vertexAttribDivisor", 9, 1]));
  const ptr = env.calls.find(function(c) { return c[0] === "vertexAttribPointer" && c[1] === 9; });
  assert.deepEqual(ptr, ["vertexAttribPointer", 9, 4, env.gl.FLOAT, false, 0, 0]);

  const cached = mesh._cachedInstanceColors;
  const uploadCount = env.uploads.length;
  env.run();
  assert.strictEqual(mesh._cachedInstanceColors, cached);
  assert.equal(env.uploads.length, uploadCount);
});

test("missing instance colors disable the attribute with constant white", () => {
  const env = makeEnv();
  env.bundle.instancedMeshes = [{ castShadow: true, instanceCount: 2, transforms: new Float32Array(32) }];
  env.run();
  assert.ok(hasCall(env.calls, ["disableVertexAttribArray", 9]));
  assert.ok(hasCall(env.calls, ["vertexAttribDivisor", 9, 0]));
  assert.ok(hasCall(env.calls, ["vertexAttrib4f", 9, 1, 1, 1, 1]));
  assert.ok(!env.calls.some(function(c) { return c[0] === "vertexAttribPointer" && c[1] === 9; }));
});

test("bounds fold transformed instances, cap unused trailing matrices, reuse local AABB", () => {
  const env = makeEnv();
  const geom = env.geometry;
  const matrix = function(tx, s) {
    return [s, 0, 0, 0, 0, s, 0, 0, 0, 0, s, 0, tx, 0, 0, 1];
  };
  const imesh = {
    castShadow: true, instanceCount: 2,
    transforms: [].concat(matrix(10, 1), matrix(0, 2), matrix(100, 1)),
  };
  const bundle = { meshObjects: [], worldMeshPositions: new Float32Array(0), instancedMeshes: [imesh] };
  const bounds = env.computeBounds(bundle, function() { return geom; });
  assert.deepEqual(
    { minX: bounds.minX, minY: bounds.minY, minZ: bounds.minZ, maxX: bounds.maxX, maxY: bounds.maxY, maxZ: bounds.maxZ },
    { minX: -1, minY: -1, minZ: 0, maxX: 10.5, maxY: 1, maxZ: 0 });
  assert.deepEqual({ ...geom._gosxShadowLocalAABB },
    { minX: -0.5, minY: -0.5, minZ: 0, maxX: 0.5, maxY: 0.5, maxZ: 0 });
  const cachedAABB = geom._gosxShadowLocalAABB;
  env.computeBounds(bundle, function() { return geom; });
  assert.strictEqual(geom._gosxShadowLocalAABB, cachedAABB);

  const empty = env.computeBounds(
    Object.assign({}, bundle, { instancedMeshes: [Object.assign({}, imesh, { instanceCount: 0 })] }),
    function() { return geom; });
  assert.deepEqual({ ...empty }, { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 });
});

test("rotated, nonuniformly scaled, translated instance bounds are folded", () => {
  const env = makeEnv();
  const geom = {
    positions: Float32Array.from([-1, -2, -3, 1, 2, 3]),
    uvs: null,
    vertexCount: 2,
  };
  // Flat column-major 4x4: columns (0,2,0), (-3,0,0), (0,0,4), translation
  // (10,-4,5). Independent expected bounds computed by hand: extents
  // (|c1|·1 + |c2|·2 + |c3|·3) per axis around the translation.
  const imesh = {
    castShadow: true, instanceCount: 1,
    transforms: [0, 2, 0, 0, -3, 0, 0, 0, 0, 0, 4, 0, 10, -4, 5, 1],
  };
  const bundle = { meshObjects: [], worldMeshPositions: new Float32Array(0), instancedMeshes: [imesh] };
  const bounds = env.computeBounds(bundle, function() { return geom; });
  assert.deepEqual({ ...bounds },
    { minX: 4, minY: -6, minZ: -7, maxX: 16, maxY: -2, maxZ: 17 });
});

test("typed transform views are cached, stable across passes, rebuilt on replacement", () => {
  const env = makeEnv();
  const mesh = {
    castShadow: true, instanceCount: 2,
    transforms: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1,
                 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 5, 0, 0, 1],
  };
  env.bundle.instancedMeshes = [mesh];
  env.run();
  assert.ok(mesh._cachedTransforms instanceof Float32Array);
  const first = mesh._cachedTransforms;
  const transformUploads = function() {
    return env.uploads.filter(function(u) { return u.data.length === 32; }).length;
  };
  assert.equal(transformUploads(), 1);

  env.run();
  assert.strictEqual(mesh._cachedTransforms, first);
  assert.equal(transformUploads(), 1);

  // A replaced transform array arrives on a fresh normalized mesh without a
  // stale cache: a new typed view is derived, assigned and uploaded.
  const replacement = { castShadow: true, instanceCount: 2, transforms: new Float32Array(32) };
  replacement.transforms[12] = 3;
  env.bundle.instancedMeshes = [replacement];
  env.run();
  assert.ok(replacement._cachedTransforms instanceof Float32Array);
  assert.notStrictEqual(replacement._cachedTransforms, first);
  assert.equal(transformUploads(), 2);
});

test("masked instanced casters set mask uniforms, bind the cached texture, force redraws", () => {
  const env = makeEnv();
  const uri = "/tex/alb.png";
  const key = vm.runInContext(
    "scenePBRTextureCacheKey(scenePBRTextureDescriptor(" +
    "{ uri: '" + uri + "', role: 'base-color', colorSpace: 'srgb' }, '" + uri + "', 'base-color', 'srgb'))",
    env.context);
  const texture = { id: "albedo" };
  const textureCache = new Map([[key, { texture: texture, loaded: true }]]);
  env.bundle.instancedMeshes = [{
    castShadow: true, instanceCount: 1, materialIndex: 0,
    transforms: new Float32Array(16),
  }];
  env.bundle.materials = [{
    alphaCutoff: 0.5, opacity: 0.4,
    textureDescriptors: { baseColor: { uri: uri, role: "base-color", colorSpace: "srgb" } },
  }];

  env.run(textureCache);
  assert.ok(hasCall(env.calls, ["uniform1f", "iop", 0.4]));
  assert.ok(hasCall(env.calls, ["uniform1f", "iac", Math.fround(0.5)]));
  assert.ok(hasCall(env.calls, ["uniform1f", "iham", 1]));
  const bindIndex = env.calls.findIndex(function(c) { return c[0] === "bindTexture" && c[2] === texture; });
  const drawIndex = env.calls.findIndex(function(c) { return c[0] === "drawArraysInstanced"; });
  assert.ok(bindIndex >= 0 && bindIndex < drawIndex);

  env.bundle.instancedMeshes = [];
  env.bundle.materials = [null];
  env.calls.length = 0;
  env.run(textureCache);
  assert.equal(of(env.calls, "drawArraysInstanced").length, 0);
  const clears = of(env.calls, "clear").length;
  assert.equal(clears, 1);
  env.run(textureCache);
  assert.equal(of(env.calls, "clear").length, clears);
});

test("shared shadow fragment source: flat instanced alpha, strict cutoff, static output unchanged", () => {
  const context = createContext();
  const instanced = vm.runInContext("sceneShadowFragmentSource(true)", context);
  const staticSource = vm.runInContext("sceneShadowFragmentSource(false)", context);
  const constant = vm.runInContext("SCENE_SHADOW_FRAGMENT_SOURCE", context);
  assert.match(instanced, /flat in float v_instanceAlpha;/);
  assert.match(instanced, /alpha \*= v_instanceAlpha;/);
  assert.ok(instanced.includes("alpha < u_alphaCutoff"));
  assert.ok(!staticSource.includes("v_instanceAlpha"));
  assert.ok(staticSource.includes("alpha < u_alphaCutoff"));
  // Independent baseline: the exact original static shadow fragment shader,
  // compared against both the builder output and the module constant so the
  // unchanged static behavior is proven, not self-referential.
  const baseline = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_albedoMap;",
    "uniform float u_hasAlbedoMap;",
    "uniform float u_opacity;",
    "uniform float u_alphaCutoff;",
    "void main() {",
    "    float alpha = 1.0;",
    "    if (u_hasAlbedoMap > 0.5) {",
    "        alpha = texture(u_albedoMap, v_uv).a;",
    "    }",
    "    alpha *= u_opacity;",
    "    if (u_alphaCutoff >= 0.0 && alpha < u_alphaCutoff) {",
    "        discard;",
    "    }",
    "}",
  ].join("\n");
  assert.equal(staticSource, baseline);
  assert.equal(constant, baseline);
});
