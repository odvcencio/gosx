"use strict";

// Executable regression tests for the WebGL alpha-mask shadow pass. These
// execute the ACTUAL production fragments from webgl.ts inside a VM, sliced
// between exact checked markers — no copied implementation.

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const RUNTIME_DIR = path.join(__dirname, "..", "runtime", "scene3d");
const BOOTSTRAP_DIR = path.join(__dirname, "bootstrap-src");

function readRuntime(name) {
  return fs.readFileSync(path.join(RUNTIME_DIR, name), "utf8");
}

function readBootstrap(name) {
  return fs.readFileSync(path.join(BOOTSTRAP_DIR, name), "utf8");
}

function sliceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  assert.ok(start >= 0, "start marker located: " + startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(end > start, "end marker located after start: " + endMarker);
  return source.slice(start, end);
}

const WEBGL = readRuntime("webgl.ts");

// The whole alpha-mask shadow block: pass hash, renderSceneShadowPass,
// SCENE_SHADOW_UNMASKED, sceneShadowResolveMask, sceneShadowApplyMask.
const SHADOW_SOURCE = sliceBetween(WEBGL,
  "function sceneShadowPassHash",
  "// Compile the shadow depth shader program.");

// The shared texture cache key builders the shadow pass depends on.
const TEX_KEY_SOURCE = sliceBetween(WEBGL,
  "function scenePBRTextureDescriptor",
  "function scenePBRLoadTexture");

const SCENE_FRAGMENT_FILES = [
  "10-runtime-primitives.ts",
  "10-runtime-scene-utils.ts",
  "11-scene-math.ts",
  "13-scene-material.ts",
];

// Direct dependencies of the shadow source that live outside webgl.ts:
// sceneCSSVarReference (10-runtime-scene-core.ts) and sceneFiniteNumber
// (15a-scene-postfx-shared.ts). Slices end at the next actual function /
// section marker in each bootstrap file.
const SCENE_CORE_SOURCE = sliceBetween(readBootstrap("10-runtime-scene-core.ts"),
  "function sceneCSSVarReference",
  "function sceneWaterResetClock");

const SCENE_POSTFX_SOURCE = sliceBetween(readBootstrap("15a-scene-postfx-shared.ts"),
  "function sceneFiniteNumber",
  "// Render-truth telemetry");

function createContext() {
  const context = vm.createContext({
    console,
    window: {},
    Math, Number, Boolean, String, Array, Object, JSON, isFinite,
    Float32Array,
  });
  for (const name of SCENE_FRAGMENT_FILES) {
    vm.runInContext(readBootstrap(name), context, { filename: name });
  }
  vm.runInContext(SCENE_CORE_SOURCE, context,
    { filename: "10-runtime-scene-core.ts#sceneCSSVarReference" });
  vm.runInContext(SCENE_POSTFX_SOURCE, context,
    { filename: "15a-scene-postfx-shared.ts#sceneFiniteNumber" });
  vm.runInContext(TEX_KEY_SOURCE, context, { filename: "webgl.ts#texture-key" });
  vm.runInContext(SHADOW_SOURCE, context, { filename: "webgl.ts#shadow" });
  vm.runInContext(
    "globalThis.__textureKey = function (descriptor, url) {" +
    "  return scenePBRTextureCacheKey(" +
    "    scenePBRTextureDescriptor(descriptor, url, 'base-color', 'srgb'));" +
    "};",
    context, { filename: "webgl.ts#texture-key-helper" });
  return context;
}

// Shadow slot resource allocation block: createSceneShadowResources,
// createSceneShadowSlot and disposeShadowSlot, sliced from webgl.ts between
// exact checked markers.
const SHADOW_ALLOC_SOURCE = sliceBetween(WEBGL,
  "function createSceneShadowResources",
  "function computeShadowSlotCascadeMatrices");

const TEX_URI = "/tex/alb-white-a128.png";

function descriptorFor(uri) {
  return { uri, role: "base-color", colorSpace: "srgb" };
}

function makeShadowProgram() {
  return {
    program: { id: "shadow-program" },
    attributes: { position: 1, uv: 2 },
    uniforms: {
      lightViewProjection: "u_lightViewProjection",
      modelMatrix: "u_modelMatrix",
      albedoMap: "u_albedoMap",
      hasAlbedoMap: "u_hasAlbedoMap",
      opacity: "u_opacity",
      alphaCutoff: "u_alphaCutoff",
    },
  };
}

function makeShadowResources() {
  return { framebuffer: { id: "shadow-fbo" }, size: 512 };
}

// Minimal explicit fake allocation ports for the slot resource fragment:
// records create/delete/texImage2D calls, no permissive Proxy.
function createAllocGL() {
  const calls = [];
  const created = { fbos: [], textures: [] };
  let id = 0;
  return {
    calls,
    created,
    TEXTURE_2D: 0x0de1,
    FRAMEBUFFER: 0x8d40,
    DEPTH_ATTACHMENT: 0x8d00,
    DEPTH_COMPONENT24: 0x81a6,
    DEPTH_COMPONENT: 0x1902,
    UNSIGNED_INT: 0x1405,
    TEXTURE_MIN_FILTER: 0x2801,
    TEXTURE_MAG_FILTER: 0x2800,
    TEXTURE_WRAP_S: 0x2802,
    TEXTURE_WRAP_T: 0x2803,
    NEAREST: 0x2600,
    CLAMP_TO_EDGE: 0x812f,
    createFramebuffer() {
      const fbo = { id: "fbo" + (++id) };
      created.fbos.push(fbo);
      calls.push(["createFramebuffer", fbo]);
      return fbo;
    },
    createTexture() {
      const tex = { id: "tex" + (++id) };
      created.textures.push(tex);
      calls.push(["createTexture", tex]);
      return tex;
    },
    deleteFramebuffer(fbo) { calls.push(["deleteFramebuffer", fbo]); },
    deleteTexture(tex) { calls.push(["deleteTexture", tex]); },
    texImage2D(target, level, internalFormat, w, h, border, fmt, type, data) {
      calls.push(["texImage2D", target, level, internalFormat, w, h, border, fmt, type, data]);
    },
    texParameteri(target, pname, value) { calls.push(["texParameteri", target, pname, value]); },
    bindTexture(target, tex) { calls.push(["bindTexture", target, tex]); },
    bindFramebuffer(target, fbo) { calls.push(["bindFramebuffer", target, fbo]); },
    framebufferTexture2D(target, attachment, texTarget, tex, level) {
      calls.push(["framebufferTexture2D", target, attachment, texTarget, tex, level]);
    },
  };
}

function createAllocContext() {
  const context = createContext();
  vm.runInContext(SHADOW_ALLOC_SOURCE, context,
    { filename: "webgl.ts#shadow-alloc" });
  return context;
}

const LIGHT_MATRIX = Float32Array.from({ length: 16 }, (_, i) => i + 1);

// Recording fake GL: only the methods the production pass actually calls,
// no permissive Proxy.
function createRecordingGL() {
  const calls = [];
  const state = {
    activeTexture: 0x84c0,
    bindings: {},
    viewport: [0, 0, 1, 1],
    scissorEnabled: false,
    scissorBox: [0, 0, 0, 0],
    depthMask: true,
  };
  const gl = {
    calls,
    _state: state,
    ACTIVE_TEXTURE: 0x84e0,
    TEXTURE0: 0x84c0,
    TEXTURE_BINDING_2D: 0x8069,
    FRAMEBUFFER: 0x8d40,
    DEPTH_BUFFER_BIT: 0x100,
    DEPTH_TEST: 0xb71,
    BLEND: 0xbe2,
    CULL_FACE: 0xb44,
    LEQUAL: 0x203,
    FRONT: 0x404,
    BACK: 0x405,
    TRIANGLES: 4,
    UNSIGNED_INT: 0x1405,
    ARRAY_BUFFER: 0x8892,
    DYNAMIC_DRAW: 0x88e8,
    FLOAT: 0x1406,
    TEXTURE_2D: 0x0de1,
    VIEWPORT: 0x0ba2,
    SCISSOR_TEST: 0x0c11,
    SCISSOR_BOX: 0x0c10,
    DEPTH_WRITEMASK: 0x0d33,
    activeTexture(u) { calls.push(["activeTexture", u]); state.activeTexture = u; },
    getParameter(p) {
      if (p === 0x84e0) return state.activeTexture;
      if (p === 0x8069) return state.bindings[state.activeTexture] || null;
      if (p === gl.VIEWPORT) return state.viewport.slice();
      if (p === gl.SCISSOR_TEST) return state.scissorEnabled;
      if (p === gl.SCISSOR_BOX) return state.scissorBox.slice();
      if (p === gl.DEPTH_WRITEMASK) return state.depthMask;
      return null;
    },
    bindTexture(target, tex) {
      calls.push(["bindTexture", target, tex]);
      state.bindings[state.activeTexture] = tex;
    },
    bindFramebuffer(t, fbo) { calls.push(["bindFramebuffer", t, fbo]); },
    viewport(x, y, w, h) {
      calls.push(["viewport", x, y, w, h]);
      state.viewport = [x, y, w, h];
    },
    scissor(x, y, w, h) {
      calls.push(["scissor", x, y, w, h]);
      state.scissorBox = [x, y, w, h];
    },
    clearDepth(d) { calls.push(["clearDepth", d]); },
    clear(m) {
      calls.push(["clear", m]);
      // Snapshot the scissor/viewport/depth-writemask state AT CLEAR TIME:
      // the point atlas must clear exactly its tile with a writable mask.
      gl.clearSnapshots.push({
        mask: m,
        scissorEnabled: state.scissorEnabled,
        scissorBox: state.scissorBox.slice(),
        viewport: state.viewport.slice(),
        depthMask: state.depthMask,
      });
    },
    useProgram(p) { calls.push(["useProgram", p]); },
    uniformMatrix4fv(loc, transpose, m) {
      calls.push(["uniformMatrix4fv", loc, transpose, Array.from(m)]);
    },
    uniform1i(loc, v) { calls.push(["uniform1i", loc, v]); },
    uniform1f(loc, v) { calls.push(["uniform1f", loc, v]); },
    enable(cap) {
      calls.push(["enable", cap]);
      if (cap === gl.SCISSOR_TEST) state.scissorEnabled = true;
    },
    disable(cap) {
      calls.push(["disable", cap]);
      if (cap === gl.SCISSOR_TEST) state.scissorEnabled = false;
    },
    depthMask(v) {
      calls.push(["depthMask", v]);
      state.depthMask = v;
    },
    depthFunc(f) { calls.push(["depthFunc", f]); },
    cullFace(f) { calls.push(["cullFace", f]); },
    bindBuffer(t, b) { calls.push(["bindBuffer", t, b]); },
    bufferData(t, data, usage) {
      // Snapshot the typed array: production uploads subarray views of a
      // persistent scratch buffer, so recording the live view would let a
      // later draw overwrite an earlier call record.
      const snapshot = ArrayBuffer.isView(data) ? data.slice() : data;
      calls.push(["bufferData", t, snapshot, usage]);
    },
    enableVertexAttribArray(i) { calls.push(["enableVertexAttribArray", i]); },
    vertexAttribPointer(i, size, type, norm, stride, off) {
      calls.push(["vertexAttribPointer", i, size, type, norm, stride, off]);
    },
    disableVertexAttribArray(i) { calls.push(["disableVertexAttribArray", i]); },
    vertexAttrib2f(i, x, y) { calls.push(["vertexAttrib2f", i, x, y]); },
    drawArrays(mode, first, count) {
      // Record the unit-0 2D binding at draw time: the shadow program samples
      // unit 0 on every draw, so this must never be the attached depth
      // texture (framebuffer feedback).
      gl._drawTimeUnit0.push(
        Object.prototype.hasOwnProperty.call(state.bindings, gl.TEXTURE0)
          ? state.bindings[gl.TEXTURE0]
          : null
      );
      calls.push(["drawArrays", mode, first, count]);
    },
    drawElements(mode, count, type, off) {
      calls.push(["drawElements", mode, count, type, off]);
    },
  };
  gl._drawTimeUnit0 = [];
  gl.clearSnapshots = [];
  return gl;
}

function makeMaskedBundle() {
  const descriptor = descriptorFor(TEX_URI);
  return {
    descriptor,
    bundle: {
      meshObjects: [{ castShadow: true, vertexOffset: 0, vertexCount: 3, materialIndex: 0 }],
      materials: [{ alphaCutoff: 0.5, opacity: 0.5, textureDescriptors: { baseColor: descriptor } }],
      worldMeshPositions: Float32Array.from({ length: 9 }, (_, i) => i + 1),
      worldMeshUVs: Float32Array.from({ length: 6 }, (_, i) => 10 + i),
    },
  };
}

function makeOpaqueBundle() {
  return {
    meshObjects: [{ castShadow: true, vertexOffset: 1, vertexCount: 3, materialIndex: 0 }],
    materials: [{ opacity: 1, alphaCutoff: null }],
    worldMeshPositions: Float32Array.from({ length: 12 }, (_, i) => i + 1),
  };
}

// Point-atlas point pass resources: shared atlas framebuffer, 64px tiles,
// face index and light matrix so the production point-face gate fires.
function makePointFace(framebuffer, face) {
  return {
    framebuffer,
    size: 64,
    point: true,
    pointFace: face,
    lightMatrix: LIGHT_MATRIX,
  };
}

// Opaque bundle with an offscreen caster (viewCulled true — point faces must
// still draw it) plus a castShadow=false control that must never draw.
function makePointBundle() {
  const bundle = makeOpaqueBundle();
  bundle.meshObjects[0].viewCulled = true;
  bundle.meshObjects.push({
    castShadow: false,
    viewCulled: false,
    vertexOffset: 1,
    vertexCount: 3,
    materialIndex: 0,
  });
  return bundle;
}

function uniform1fValues(gl) {
  return Object.fromEntries(
    gl.calls.filter((c) => c[0] === "uniform1f").map((c) => [c[1], c[2]]));
}

test("sceneShadowResolveMask resolves materials[materialIndex] and ignores decoy obj.material", () => {
  const ctx = createContext();
  const resolve = ctx.sceneShadowResolveMask;
  assert.equal(typeof resolve, "function");
  const bundle = {
    materials: [
      { alphaCutoff: 0, opacity: 1 },
      { alphaCutoff: 0.5, opacity: 0.75, material: "decoy-on-material" },
    ],
  };
  const obj = { castShadow: true, materialIndex: 1, material: { alphaCutoff: null } };
  const before = JSON.stringify(obj);
  const mask = resolve(obj, bundle, new Map());
  assert.equal(mask.masked, true);
  assert.equal(mask.cutoff, 0.5);
  assert.equal(mask.opacity, 0.75);
  assert.equal(mask.hasAlbedoMap, false);
  assert.equal(mask.texture, null);
  assert.equal(JSON.stringify(obj), before, "source object not mutated");
});

test("sceneShadowResolveMask root cutoff 0 masks; disabled, invalid and negative unmask", () => {
  const ctx = createContext();
  const resolve = ctx.sceneShadowResolveMask;
  const zero = resolve({ materialIndex: 0 }, { materials: [{ alphaCutoff: 0 }] }, new Map());
  assert.equal(zero.masked, true, "cutoff 0 survives opacity 0 in the color pass");
  assert.equal(zero.cutoff, 0);
  assert.equal(zero.opacity, 1);
  for (const cutoff of [null, undefined, false, "", "abc", -1, -0.5, "  "]) {
    const m = resolve(
      { materialIndex: 0 }, { materials: [{ alphaCutoff: cutoff }] }, new Map());
    assert.equal(m.masked, false, "unmasked for cutoff " + String(cutoff));
    assert.equal(m.cutoff, -1);
    assert.equal(m.opacity, 1);
    assert.equal(m.hasAlbedoMap, false);
    assert.equal(m.texture, null);
  }
  assert.equal(resolve({ materialIndex: 0 }, { materials: [] }, new Map()).masked, false);
  assert.equal(resolve({ materialIndex: 0 }, {}, new Map()).masked, false);
});

test("sceneShadowResolveMask normalizes above-1 cutoffs to 2 and clamps fractional opacity", () => {
  const ctx = createContext();
  const resolve = ctx.sceneShadowResolveMask;
  const above = resolve({ materialIndex: 0 }, { materials: [{ alphaCutoff: 2.5 }] }, new Map());
  assert.equal(above.masked, true, "above-1 stays masked but discards everything");
  assert.equal(above.cutoff, 2);
  assert.equal(resolve({ materialIndex: 0 }, { materials: [{ alphaCutoff: 1.5, opacity: 1.5 }] }, new Map()).opacity, 1);
  assert.equal(resolve({ materialIndex: 0 }, { materials: [{ alphaCutoff: 1.5, opacity: -0.2 }] }, new Map()).opacity, 0);
  assert.equal(resolve({ materialIndex: 0 }, { materials: [{ alphaCutoff: 1.5, opacity: "0.25" }] }, new Map()).opacity, 0.25);
});

test("sceneShadowResolveMask texture readiness flips on the descriptor cache key only", () => {
  const ctx = createContext();
  const resolve = ctx.sceneShadowResolveMask;
  const descriptor = descriptorFor(TEX_URI);
  const bundle = {
    materials: [{ alphaCutoff: 0.5, textureDescriptors: { baseColor: descriptor } }],
  };
  const obj = { materialIndex: 0 };
  const texture = { id: "gl-tex" };
  const before = JSON.stringify(obj);
  const cache = new Map();
  let mask = resolve(obj, bundle, cache);
  assert.equal(mask.masked, true, "masked even before the texture is ready");
  assert.equal(mask.hasAlbedoMap, false);
  assert.equal(mask.texture, null);
  cache.set(ctx.__textureKey(descriptor, TEX_URI), { texture, loaded: true });
  mask = resolve(obj, bundle, cache);
  assert.equal(mask.hasAlbedoMap, true, "readiness flip is observed");
  assert.equal(mask.texture, texture, "texture identity is the actual record object");
  assert.equal(JSON.stringify(obj), before, "no source mutation");
  // A record stored under a different descriptor key (linear color space)
  // must never satisfy the srgb descriptor lookup.
  const wrongKey = ctx.__textureKey(
    { uri: TEX_URI, role: "base-color", colorSpace: "linear" }, TEX_URI);
  assert.notEqual(wrongKey, ctx.__textureKey(descriptor, TEX_URI));
  const wrongCache = new Map([[wrongKey, { texture, loaded: true }]]);
  mask = resolve(obj, bundle, wrongCache);
  assert.equal(mask.hasAlbedoMap, false);
  assert.equal(mask.texture, null);
});

test("sceneShadowResolveMask falls back to mat.texture, reads the cache via get only, never loads", () => {
  const ctx = createContext();
  const resolve = ctx.sceneShadowResolveMask;
  const texture = { id: "gl-tex" };
  const record = { texture, loaded: true };
  let gets = 0;
  const cache = {
    get(key) {
      gets++;
      return key === ctx.__textureKey(null, TEX_URI) ? record : null;
    },
  };
  const obj = { materialIndex: 0, material: "decoy" };
  const before = JSON.stringify(obj);
  const bundle = { materials: [{ alphaCutoff: 0.5, texture: TEX_URI }] };
  const mask = resolve(obj, bundle, cache);
  assert.equal(mask.masked, true);
  assert.equal(mask.hasAlbedoMap, true);
  assert.equal(mask.texture, texture);
  assert.equal(gets, 1, "exactly one cache lookup, no loader involvement");
  assert.equal(JSON.stringify(obj), before, "no source mutation");
});

test("masked soup casters upload interleaved position+UV (stride 20, UV offset 12) from source vertexOffset", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const program = makeShadowProgram();
  const resources = makeShadowResources();
  const shadowState = { buffer: { id: "shadow-vbo" } };
  const texture = { id: "mask-tex" };
  const { bundle, descriptor } = makeMaskedBundle();
  bundle.meshObjects[0].vertexOffset = 2;
  bundle.worldMeshPositions = Float32Array.from({ length: 15 }, (_, i) => i + 1);
  bundle.worldMeshUVs = Float32Array.from({ length: 10 }, (_, i) => 100 + i);
  const cache = new Map([[ctx.__textureKey(descriptor, TEX_URI), { texture, loaded: true }]]);

  ctx.renderSceneShadowPass(gl, program, resources, LIGHT_MATRIX, bundle, shadowState, null, cache);

  assert.deepEqual(gl.calls.find((c) => c[0] === "drawArrays"),
    ["drawArrays", gl.TRIANGLES, 0, 3]);
  const uploads = gl.calls.filter((c) => c[0] === "bufferData");
  assert.equal(uploads.length, 1);
  assert.equal(uploads[0][3], gl.DYNAMIC_DRAW);
  const data = uploads[0][2];
  assert.ok(data instanceof Float32Array);
  assert.equal(data.length, 15);
  // Exact interleave for source vertexOffset 2: per vertex, 3 positions then
  // 2 UVs — [7,8,9,104,105, 10,11,12,106,107, 13,14,15,108,109].
  assert.deepEqual(Array.from(data),
    [7, 8, 9, 104, 105, 10, 11, 12, 106, 107, 13, 14, 15, 108, 109],
    "per-vertex interleave: 3 positions then 2 UVs, sourced from vertexOffset");
  const pointers = gl.calls.filter((c) => c[0] === "vertexAttribPointer");
  assert.deepEqual(pointers[0], ["vertexAttribPointer", 1, 3, gl.FLOAT, false, 20, 0]);
  assert.deepEqual(pointers[1], ["vertexAttribPointer", 2, 2, gl.FLOAT, false, 20, 12]);
  assert.ok(gl.calls.some((c) => c[0] === "bindBuffer"
    && c[1] === gl.ARRAY_BUFFER && c[2] === shadowState.buffer),
    "draws from the persistent shadow buffer");
  const uniforms = uniform1fValues(gl);
  assert.equal(uniforms["u_opacity"], 0.5);
  assert.equal(uniforms["u_alphaCutoff"], 0.5);
  assert.equal(uniforms["u_hasAlbedoMap"], 1);
  assert.ok(gl.calls.some((c) => c[0] === "uniform1i"
    && c[1] === "u_albedoMap" && c[2] === 0));
  assert.ok(gl.calls.some((c) => c[0] === "bindTexture" && c[2] === texture),
    "mask texture bound during the pass");
});

test("unmasked casters keep the opaque path: stride 0, UV disabled and neutralized, cutoff -1", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const bundle = makeOpaqueBundle();
  const shadowState = { buffer: { id: "shadow-vbo" } };
  ctx.renderSceneShadowPass(gl, makeShadowProgram(), makeShadowResources(),
    LIGHT_MATRIX, bundle, shadowState, null, new Map());

  assert.deepEqual(gl.calls.find((c) => c[0] === "drawArrays"),
    ["drawArrays", gl.TRIANGLES, 0, 3]);
  const pointers = gl.calls.filter((c) => c[0] === "vertexAttribPointer");
  assert.deepEqual(pointers, [["vertexAttribPointer", 1, 3, gl.FLOAT, false, 0, 0]],
    "positions only, packed stride 0");
  assert.ok(gl.calls.some((c) => c[0] === "disableVertexAttribArray" && c[1] === 2),
    "uv attribute explicitly disabled");
  assert.deepEqual(gl.calls.find((c) => c[0] === "vertexAttrib2f"),
    ["vertexAttrib2f", 2, 0, 0], "uv neutralized to (0,0)");
  const uploads = gl.calls.filter((c) => c[0] === "bufferData");
  assert.equal(uploads.length, 1);
  assert.equal(uploads[0][2].length, 9);
  assert.deepEqual(Array.from(uploads[0][2]),
    Array.from(bundle.worldMeshPositions.subarray(3, 12)));
  const uniforms = uniform1fValues(gl);
  assert.equal(uniforms["u_opacity"], 1);
  assert.equal(uniforms["u_alphaCutoff"], -1);
  assert.equal(uniforms["u_hasAlbedoMap"], 0);
  assert.ok(!gl.calls.some((c) => c[0] === "bindTexture" && c[2]),
    "no texture bound for unmasked casters");
});

test("mixed masked-then-unmasked casters reset uniforms and neutralize the UV attribute between draws", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const descriptor = descriptorFor(TEX_URI);
  const texture = { id: "mask-tex" };
  const bundle = {
    meshObjects: [
      { castShadow: true, vertexOffset: 0, vertexCount: 3, materialIndex: 0 },
      { castShadow: true, vertexOffset: 3, vertexCount: 3, materialIndex: 1 },
    ],
    materials: [
      { alphaCutoff: 0.5, opacity: 0.5, textureDescriptors: { baseColor: descriptor } },
      { opacity: 1, alphaCutoff: null },
    ],
    worldMeshPositions: Float32Array.from({ length: 21 }, (_, i) => i + 1),
    worldMeshUVs: Float32Array.from({ length: 6 }, (_, i) => 100 + i),
  };
  const cache = new Map([[ctx.__textureKey(descriptor, TEX_URI), { texture, loaded: true }]]);

  ctx.renderSceneShadowPass(gl, makeShadowProgram(), makeShadowResources(),
    LIGHT_MATRIX, bundle, { buffer: { id: "shadow-vbo" } }, null, cache);

  const draws = gl.calls.filter((c) => c[0] === "drawArrays");
  assert.equal(draws.length, 2, "one draw per participating caster");
  assert.deepEqual(draws, [
    ["drawArrays", gl.TRIANGLES, 0, 3],
    ["drawArrays", gl.TRIANGLES, 0, 3],
  ]);
  const cutoffs = gl.calls
    .filter((c) => c[0] === "uniform1f" && c[1] === "u_alphaCutoff")
    .map((c) => c[2]);
  assert.deepEqual(cutoffs, [0.5, -1],
    "masked cutoff 0.5 then reset to -1 for the unmasked caster");
  const uploads = gl.calls.filter((c) => c[0] === "bufferData");
  assert.equal(uploads.length, 2);
  assert.equal(uploads[1][2].length, 9, "second draw uploads positions only");
  assert.deepEqual(Array.from(uploads[1][2]),
    Array.from(bundle.worldMeshPositions.subarray(9, 18)));
  const firstDrawAt = gl.calls.findIndex((c) => c[0] === "drawArrays");
  const afterFirst = gl.calls.slice(firstDrawAt + 1);
  assert.ok(afterFirst.some((c) => c[0] === "disableVertexAttribArray" && c[1] === 2),
    "uv attribute disabled after the masked draw");
  assert.deepEqual(afterFirst.find((c) => c[0] === "vertexAttrib2f"),
    ["vertexAttrib2f", 2, 0, 0], "uv neutralized after the masked draw");
  assert.ok(!afterFirst.some((c) => c[0] === "vertexAttribPointer" && c[2] === 2),
    "no UV vertexAttribPointer after the previous masked draw");
});

test("identical unmasked frames hit the pass cache and issue no GL writes", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const bundle = makeOpaqueBundle();
  const shadowState = { buffer: { id: "shadow-vbo" } };
  const args = [gl, makeShadowProgram(), makeShadowResources(),
    LIGHT_MATRIX, bundle, shadowState, null, new Map()];
  ctx.renderSceneShadowPass(...args);
  assert.equal(gl.calls.filter((c) => c[0] === "drawArrays").length, 1);
  const afterFirst = gl.calls.length;
  assert.ok(afterFirst > 0);
  ctx.renderSceneShadowPass(...args);
  assert.equal(gl.calls.length, afterFirst, "cached frame performs zero GL calls");
  // A hash-relevant change redraws.
  args[4] = makeOpaqueBundle();
  args[4].meshObjects[0].vertexCount = 6;
  args[4].worldMeshPositions = Float32Array.from({ length: 18 }, (_, i) => i + 1);
  ctx.renderSceneShadowPass(...args);
  assert.equal(gl.calls.filter((c) => c[0] === "drawArrays").length, 2);
});

test("point-face atlas clears only its tile and restores exact prior GL state", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const bundle = makePointBundle();
  const shadowState = { buffer: { id: "shadow-vbo" } };
  const program = makeShadowProgram();
  // Six persistent face records built once, all sharing a single atlas
  // framebuffer, mirroring the production slot allocator.
  const sharedFBO = { id: "atlas-fbo" };
  const pointFaces = Array.from({ length: 6 }, (_, face) =>
    makePointFace(sharedFBO, face));
  const savedTexture0 = { id: "unit0-color" };
  const savedTexture3 = { id: "unit3-color" };
  const args = [gl, program, null, LIGHT_MATRIX, bundle, shadowState,
    null, new Map(), undefined, undefined];

  // Each round visits faces 0-5 then repeats face 5 with identical inputs, so
  // the final repetition only draws if point faces bypass the pass cache.
  const faceOrder = [0, 1, 2, 3, 4, 5, 5];
  for (let round = 0; round < 2; round++) {
    for (const face of faceOrder) {
      const priorScissor = round === 1;
      gl._state.viewport = [2, 3, 300, 200];
      gl._state.scissorBox = [7, 11, 17, 19];
      gl._state.scissorEnabled = priorScissor;
      gl._state.depthMask = false;
      gl._state.bindings[gl.TEXTURE0] = savedTexture0;
      gl._state.bindings[gl.TEXTURE0 + 3] = savedTexture3;
      gl._state.activeTexture = gl.TEXTURE0 + 3;
      args[2] = pointFaces[face];
      const before = gl.calls.length;
      const clearsBefore = gl.clearSnapshots.length;
      ctx.renderSceneShadowPass(...args);
      const slice = gl.calls.slice(before);

      // Offscreen (viewCulled) caster draws exactly once per call even with
      // the same pass hash; the castShadow=false control never draws.
      assert.equal(slice.filter((c) => c[0] === "drawArrays").length, 1,
        "exactly one new draw per point-face call");
      assert.equal(gl.clearSnapshots.length, clearsBefore + 1,
        "exactly one new clear per point-face call");
      const snap = gl.clearSnapshots[gl.clearSnapshots.length - 1];
      const tx = (face % 3) * 64;
      const ty = Math.floor(face / 3) * 64;
      assert.equal(snap.mask, gl.DEPTH_BUFFER_BIT, "clear is DEPTH only");
      assert.equal(snap.depthMask, true, "clear happens with writable mask");
      assert.equal(snap.scissorEnabled, true);
      assert.deepEqual(snap.scissorBox, [tx, ty, 64, 64]);
      assert.deepEqual(snap.viewport, [tx, ty, 64, 64]);

      // Exact prior state restored: viewport, scissor box, scissor enable,
      // distinct unit-0 and unit-3 texture bindings and the incoming active
      // texture unit (TEXTURE0+3). The shadow pass intentionally leaves
      // depthMask true (the same contract as ordinary shadows); it restores
      // viewport/scissor/texture state, not the old depthMask.
      assert.deepEqual(gl._state.viewport, [2, 3, 300, 200]);
      assert.deepEqual(gl._state.scissorBox, [7, 11, 17, 19]);
      assert.equal(gl._state.scissorEnabled, priorScissor);
      assert.equal(gl._state.depthMask, true);
      assert.equal(gl._state.bindings[gl.TEXTURE0], savedTexture0);
      assert.equal(gl._state.bindings[gl.TEXTURE0 + 3], savedTexture3);
      assert.equal(gl._state.activeTexture, gl.TEXTURE0 + 3);
    }
  }

  // 14 calls, each with exactly one new clear and one draw; repeating face 5
  // proves point faces bypass the pass-hash cache. Flags never mutated.
  assert.equal(gl.clearSnapshots.length, 14);
  assert.equal(gl.calls.filter((c) => c[0] === "drawArrays").length, 14);
  assert.equal(bundle.meshObjects[0].castShadow, true);
  assert.equal(bundle.meshObjects[1].castShadow, false);
  assert.equal(bundle.meshObjects[0].viewCulled, true);
  assert.equal(bundle.meshObjects[1].viewCulled, false);
  assert.deepEqual(gl.calls[gl.calls.length - 1],
    ["bindFramebuffer", gl.FRAMEBUFFER, null]);
});

test("ordinary non-point pass still skips view-culled casters", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const bundle = makePointBundle();
  const shadowState = { buffer: { id: "shadow-vbo" } };
  ctx.renderSceneShadowPass(gl, makeShadowProgram(), makeShadowResources(),
    LIGHT_MATRIX, bundle, shadowState, null, new Map(), undefined, undefined);
  assert.equal(gl.calls.filter((c) => c[0] === "drawArrays").length, 0);
});

test("point atlas slot allocates ONE shared depth texture and disposes exactly once", () => {
  const ctx = createAllocContext();
  const gl = createAllocGL();
  const slot = ctx.createSceneShadowSlot(gl, 64, 1, true);
  assert.equal(slot.numCascades, 1);
  assert.equal(slot.point, true);
  assert.equal(slot.cascades.length, 1);
  assert.equal(slot.pointFaces.length, 6);
  assert.equal(gl.created.fbos.length, 1);
  assert.equal(gl.created.textures.length, 1);
  const uploads = gl.calls.filter((c) => c[0] === "texImage2D");
  assert.equal(uploads.length, 1);
  assert.equal(uploads[0][4], 192);
  assert.equal(uploads[0][5], 128);
  assert.equal(uploads[0][9], null);
  assert.equal(new Set(slot.pointFaces).size, 6);
  assert.deepEqual(Array.from(slot.pointFaces, (f) => f.pointFace),
    [0, 1, 2, 3, 4, 5]);
  for (const face of slot.pointFaces) {
    // Six non-owning face records alias the sole owner's resources.
    assert.equal(face.framebuffer, slot.cascades[0].framebuffer);
    assert.equal(face.depthTexture, slot.cascades[0].depthTexture);
    assert.equal(face.size, 64);
    assert.equal(face.point, true);
    assert.equal(face.cascadeIndex, 0);
  }
  ctx.disposeShadowSlot(gl, slot);
  assert.equal(gl.calls.filter((c) => c[0] === "deleteTexture").length, 1);
  assert.equal(gl.calls.filter((c) => c[0] === "deleteFramebuffer").length, 1);
});

test("ordinary slot allocates one 64x64 depth resource per cascade", () => {
  const ctx = createAllocContext();
  const gl = createAllocGL();
  const slot = ctx.createSceneShadowSlot(gl, 64, 2, false);
  assert.equal(slot.numCascades, 2);
  assert.equal(slot.cascades.length, 2);
  assert.equal(gl.created.textures.length, 2);
  assert.equal(gl.created.fbos.length, 2);
  const uploads = gl.calls.filter((c) => c[0] === "texImage2D");
  assert.equal(uploads.length, 2);
  for (const up of uploads) {
    assert.equal(up[4], 64);
    assert.equal(up[5], 64);
  }
  assert.notEqual(slot.cascades[0].depthTexture, slot.cascades[1].depthTexture);
  assert.notEqual(slot.cascades[0].framebuffer, slot.cascades[1].framebuffer);
  ctx.disposeShadowSlot(gl, slot);
  assert.equal(gl.calls.filter((c) => c[0] === "deleteTexture").length, 2);
  assert.equal(gl.calls.filter((c) => c[0] === "deleteFramebuffer").length, 2);
});

test("masked frames bypass the cache on readiness/opacity/cutoff edits, then recache when unmasked", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const texture = { id: "mask-tex" };
  const { bundle, descriptor } = makeMaskedBundle();
  const shadowState = { buffer: { id: "shadow-vbo" } };
  const cache = new Map();
  const args = [gl, makeShadowProgram(), makeShadowResources(),
    LIGHT_MATRIX, bundle, shadowState, null, cache];
  const draws = () => gl.calls.filter((c) => c[0] === "drawArrays").length;

  ctx.renderSceneShadowPass(...args);
  assert.equal(draws(), 1, "masked frame with no ready texture still draws");
  ctx.renderSceneShadowPass(...args);
  assert.equal(draws(), 2, "identical hash is ignored while masked");
  assert.equal(cache.size, 0, "the pass never mutates the texture cache: it stays empty");

  cache.set(ctx.__textureKey(descriptor, TEX_URI), { texture, loaded: true });
  ctx.renderSceneShadowPass(...args);
  assert.equal(draws(), 3, "readiness flip invalidates the shadow map");

  bundle.materials[0].opacity = 0.25;
  ctx.renderSceneShadowPass(...args);
  assert.equal(draws(), 4, "opacity edit redraws");

  bundle.materials[0].alphaCutoff = 0;
  ctx.renderSceneShadowPass(...args);
  assert.equal(draws(), 5, "cutoff edit redraws");

  bundle.materials[0] = { opacity: 1 };
  ctx.renderSceneShadowPass(...args);
  assert.equal(draws(), 6, "first unmasked frame after a masked frame redraws");
  ctx.renderSceneShadowPass(...args);
  assert.equal(draws(), 6, "identical unmasked frame is cached again");
});

test("the pass restores the active texture unit and unit-0 binding, including a null binding", () => {
  for (const saved of [{ id: "pbr-color" }, null]) {
    const ctx = createContext();
    const gl = createRecordingGL();
    // Valid active unit (TEXTURE0 + 3) with distinct unit-0 and unit-3
    // bindings, stored under the actual GL enum keys.
    const unit3 = { id: "pbr-unit3" };
    gl._state.activeTexture = gl.TEXTURE0 + 3;
    gl._state.bindings[gl.TEXTURE0] = saved;
    gl._state.bindings[gl.TEXTURE0 + 3] = unit3;
    const texture = { id: "mask-tex" };
    const { bundle, descriptor } = makeMaskedBundle();
    const cache = new Map([[ctx.__textureKey(descriptor, TEX_URI), { texture, loaded: true }]]);
    ctx.renderSceneShadowPass(gl, makeShadowProgram(), makeShadowResources(),
      LIGHT_MATRIX, bundle, { buffer: {} }, null, cache);

    const active = gl.calls.filter((c) => c[0] === "activeTexture").map((c) => c[1]);
    assert.deepEqual(active, [gl.TEXTURE0, gl.TEXTURE0 + 3],
      "activates unit 0 for sampling, then restores the previous unit");
    const binds = gl.calls.filter((c) => c[0] === "bindTexture").map((c) => c[2]);
    assert.equal(binds[0], null,
      "feedback-safe fallback binding precedes the mask bind (null for standalone shadowState without fallbackTexture)");
    assert.equal(binds[1], texture, "mask texture then bound on unit 0");
    assert.equal(binds[binds.length - 1], saved,
      "unit-0 binding restored by identity (including null)");
    assert.equal(gl._state.bindings[gl.TEXTURE0], saved,
      "unit-0 binding state restored");
    assert.equal(gl._state.bindings[gl.TEXTURE0 + 3], unit3,
      "unit-3 binding identity preserved untouched");
    assert.equal(gl._state.activeTexture, gl.TEXTURE0 + 3);
    assert.equal(gl.calls[gl.calls.length - 1][0], "bindFramebuffer");
  }
});

test("first draw never samples the attached depth texture: fallback bound at draw time, depth binding restored after", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const depthTexture = { id: "depth-tex" };
  // Reproduce the renderer precondition: createSceneShadowResources leaves
  // the depth texture bound on unit 0 when the shadow pass starts.
  gl._state.bindings[gl.TEXTURE0] = depthTexture;
  const fallback = { id: "fallback-tex" };

  // Opaque caster: hasAlbedoMap is 0 but the sampler uniform stays active on
  // unit 0, so the draw-time binding must already be the fallback.
  ctx.renderSceneShadowPass(gl, makeShadowProgram(),
    { framebuffer: { id: "shadow-fbo" }, size: 512, depthTexture },
    LIGHT_MATRIX, makeOpaqueBundle(),
    { buffer: {}, fallbackTexture: fallback }, null, new Map());
  assert.deepEqual(gl._drawTimeUnit0, [fallback],
    "opaque draw samples the fallback texture, never the depth texture");
  assert.equal(gl._state.bindings[gl.TEXTURE0], depthTexture,
    "initial depth-texture unit-0 binding restored after the pass");

  // Masked caster with a loaded texture: the real mask texture overrides the
  // fallback at draw time — and still never the depth texture.
  const { bundle, descriptor } = makeMaskedBundle();
  const texture = { id: "mask-tex" };
  const cache = new Map([[ctx.__textureKey(descriptor, TEX_URI), { texture, loaded: true }]]);
  ctx.renderSceneShadowPass(gl, makeShadowProgram(),
    { framebuffer: { id: "shadow-fbo-2" }, size: 512, depthTexture },
    LIGHT_MATRIX, bundle,
    { buffer: {}, fallbackTexture: fallback }, null, cache);
  // One recording GL spans both passes, so the feed accumulates: after the
  // second pass it must read [fallback, texture], not [texture] alone.
  assert.deepEqual(gl._drawTimeUnit0, [fallback, texture],
    "loaded masked caster overrides the fallback with its real texture");
  assert.notEqual(gl._drawTimeUnit0[0], depthTexture,
    "draw-time unit-0 binding is never the attached depth texture");
  assert.notEqual(gl._drawTimeUnit0[1], depthTexture,
    "loaded masked draw-time binding is never the attached depth texture");

  // Unready mask fallback: depth texture still bound on unit 0, empty cache —
  // the masked caster must draw sampling the fallback, never the depth tex.
  const { bundle: unreadyBundle } = makeMaskedBundle();
  ctx.renderSceneShadowPass(gl, makeShadowProgram(),
    { framebuffer: { id: "shadow-fbo-3" }, size: 512, depthTexture },
    LIGHT_MATRIX, unreadyBundle,
    { buffer: {}, fallbackTexture: fallback }, null, new Map());
  assert.deepEqual(gl._drawTimeUnit0, [fallback, texture, fallback],
    "unready masked caster with an empty cache samples the fallback at draw time");
  assert.notEqual(gl._drawTimeUnit0[2], depthTexture,
    "unready masked draw-time binding is never the attached depth texture");
  assert.equal(gl._state.bindings[gl.TEXTURE0], depthTexture,
    "depth-texture unit-0 binding restored after the masked pass");
  assert.equal(gl._state.activeTexture, gl.TEXTURE0,
    "active unit restored (it started on TEXTURE0)");
});

test("retained indexed casters route through the bind hook with drawElements and identity model matrix", () => {
  const ctx = createContext();
  const gl = createRecordingGL();
  const program = makeShadowProgram();
  const caster = {
    castShadow: true,
    viewCulled: false,
    directVertices: { id: "retained-geo" },
    retainedGeometry: true,
    modelMatrix: [2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 1],
  };
  const bundle = { meshObjects: [caster], materials: [] };
  const hooked = [];
  const bindIndexedCaster = (obj, prog) => { hooked.push([obj, prog]); return 6; };
  ctx.renderSceneShadowPass(gl, program, makeShadowResources(),
    LIGHT_MATRIX, bundle, { buffer: {} }, bindIndexedCaster, new Map());

  assert.deepEqual(hooked.map((h) => h[0]), [caster], "hook receives the caster");
  assert.equal(hooked[0][1], program, "hook receives the shadow program");
  assert.deepEqual(gl.calls.find((c) => c[0] === "drawElements"),
    ["drawElements", gl.TRIANGLES, 6, gl.UNSIGNED_INT, 0]);
  assert.equal(gl.calls.some((c) => c[0] === "drawArrays"), false,
    "retained casters never take the soup drawArrays path");
  const mm = gl.calls.filter((c) => c[0] === "uniformMatrix4fv" && c[1] === "u_modelMatrix");
  assert.ok(mm.length >= 1);
  // Call record shape: ["uniformMatrix4fv", location, transpose, matrix].
  assert.equal(mm[mm.length - 1][2], false, "model matrix uploaded without transpose");
  // The pass pre-seeds the identity model matrix; the bind hook standin only
  // records retained-draw behavior and does not exercise the real retained UV
  // binder inside the PBR renderer, so this is not a real GPU binder proof.
  assert.deepEqual(mm[mm.length - 1][3],
    [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

  // A hook that yields no indexes skips the draw entirely.
  const gl2 = createRecordingGL();
  ctx.renderSceneShadowPass(gl2, program, makeShadowResources(),
    LIGHT_MATRIX, bundle, { buffer: {} }, () => 0, new Map());
  assert.equal(gl2.calls.some((c) => c[0] === "drawElements"), false);
});
