"use strict";

// Regression tests for WebGL GPU-skinned shadow casting. These execute the
// ACTUAL production fragments from webgl.ts inside a VM, sliced between exact
// checked markers — no copied renderer math, no permissive proxies. The
// skinned shadow binder executed here IS the production
// bindScenePBRDirectSkinnedShadowCaster / bindScenePBRDirectAttribute /
// bindScenePBRDirectIndexBuffer / scenePBRDirectAttribute source, wired to
// recording low-level cache/buffer ports; its control flow, attribute
// choice/defaults and index/nonindexed decisions all run real source. The
// tests pin GL-observed behavior (bindings, uploads, draw-time state), not
// native/GPU evidence — that scope is intentional and honest.

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
  assert.ok(end > start, "end marker located after start: " + endMarker);
  return source.slice(start, end);
}

const WEBGL = read("webgl.ts", RUNTIME_DIR);
const SHADOW_SOURCE = sliceBetween(WEBGL,
  "function sceneShadowPassHash",
  "// Compile the shadow depth shader program.");
// CREATORS_SOURCE legitimately contains BOTH the static/instanced factories
// and the skinned factory, so absence-of-instanced-fragment checks must be
// scoped to the skinned factory slice alone. The skinned factory is sliced
// directly from WEBGL through its following marker (re-slicing the already
// shortened CREATORS_SOURCE would drop the end marker and fail).
const SKINNED_CREATOR_SOURCE = sliceBetween(WEBGL,
  "function createSceneShadowSkinnedProgram",
  "// --- End shadow depth program factories ---");
const SKINNED_VS_SOURCE = sliceBetween(WEBGL,
  "const SCENE_SHADOW_SKINNED_VERTEX_SOURCE",
  '].join("\\n");');
const SHADER_BUILDER = sliceBetween(WEBGL,
  "function sceneShadowFragmentSource",
  "// --- Points Shader Sources ---");
const TEX_KEY_SOURCE = sliceBetween(WEBGL,
  "function scenePBRTextureDescriptor",
  "function scenePBRLoadTexture");
// The real direct-attribute/index/skinned-shadow binder chain, from
// scenePBRDirectAttribute through bindScenePBRDirectSkinnedShadowCaster.
const BINDER_SOURCE = sliceBetween(WEBGL,
  "function scenePBRDirectAttribute",
  "function ensurePointsProgram");
// The real skinned shadow program disposal branch from dispose().
const SKINNED_DISPOSAL_SOURCE = sliceBetween(WEBGL,
  "if (shadowState.skinnedShadowProgram) {",
  "textureCache._gosxGeneration.disposed = true;");

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

function createContext() {
  const context = vm.createContext({
    console, Math, Number, Boolean, String, Array, Object, JSON, isFinite,
    Float32Array, Uint32Array, Set, Map,
  });
  for (const name of FRAGMENTS) {
    vm.runInContext(read(name), context, { filename: name });
  }
  vm.runInContext(CORE, context, { filename: "10-runtime-scene-core.ts#slice" });
  vm.runInContext(POSTFX, context, { filename: "15a-scene-postfx-shared.ts#slice" });
  vm.runInContext(TEX_KEY_SOURCE, context, { filename: "webgl.ts#tex-key" });
  vm.runInContext(SHADOW_SOURCE, context, { filename: "webgl.ts#shadow" });
  vm.runInContext(SHADER_BUILDER, context, { filename: "webgl.ts#shader-builder" });
  return context;
}

// createBinderContext loads the REAL production binder chain into a VM whose
// closure ports are recording fakes: the GL is the same recording fake the
// shadow pass sees, and the direct-attribute cache / epoch / retained stats /
// shared buffer handles are test-owned containers. Control flow, attribute
// choice, defaults, UV neutralization and index/nonindexed decisions all
// execute actual production source.
function createBinderContext(gl) {
  const context = createContext();
  context.__binderGL = gl;
  context.__cache = new Map();
  context.__epoch = 0;
  context.__stats = {
    rebuilds: 0, revisionInvalidations: 0, misses: 0, allocations: 0,
    liveBytes: 0, uploadCalls: 0, uploadBytes: 0, hits: 0, retirements: 0,
  };
  context.__entryBuffers = new Set();
  context.__positionBuffer = { id: "positionBuffer" };
  context.__uvBuffer = { id: "uvBuffer" };
  context.__jointsBuffer = { id: "jointsBuffer" };
  context.__weightsBuffer = { id: "weightsBuffer" };
  vm.runInContext(`
    var gl = __binderGL;
    var directMeshAttributeCache = __cache;
    var directMeshAttributeEpoch = __epoch;
    var retainedMeshBufferStats = __stats;
    var pointsEntryBuffers = __entryBuffers;
    var positionBuffer = __positionBuffer;
    var uvBuffer = __uvBuffer;
    var jointsBuffer = __jointsBuffer;
    var weightsBuffer = __weightsBuffer;
  `, context, { filename: "test-binder-env" });
  vm.runInContext(BINDER_SOURCE, context, { filename: "webgl.ts#binder" });
  return context;
}

function makeProductionSkinnedHook(binderContext) {
  const bind = vm.runInContext("bindScenePBRDirectSkinnedShadowCaster", binderContext);
  return function (obj, program, bundle) {
    return bind(obj, program, bundle);
  };
}

function createGL() {
  const calls = [];
  const gl = {
    calls,
    ACTIVE_TEXTURE: 0x84e0, TEXTURE0: 0x84c0, TEXTURE_BINDING_2D: 0x8069,
    FRAMEBUFFER: 0x8d40, DEPTH_BUFFER_BIT: 0x100, DEPTH_TEST: 0xb71,
    BLEND: 0xbe2, CULL_FACE: 0xb44, LEQUAL: 0x203, FRONT: 0x404, BACK: 0x405,
    TRIANGLES: 4, UNSIGNED_INT: 0x1405, ARRAY_BUFFER: 0x8892,
    ELEMENT_ARRAY_BUFFER: 0x8893, DYNAMIC_DRAW: 0x88e8, STATIC_DRAW: 0x88e4,
    FLOAT: 0x1406, TEXTURE_2D: 0x0de1,
    MAX_VERTEX_ATTRIBS: 0x8869, VERTEX_SHADER: 0x8b31, FRAGMENT_SHADER: 0x8b30,
    _active: 0x84c0, _binding0: null,
    _program: null,
    _enabled: new Array(16).fill(false),
    _divisors: new Array(16).fill(0),
    getParameter(p) {
      if (p === gl.MAX_VERTEX_ATTRIBS) return 16;
      if (p === gl.ACTIVE_TEXTURE) return gl._active;
      if (p === gl.TEXTURE_BINDING_2D) return gl._binding0;
      return null;
    },
    getAttribLocation(p, name) {
      gl._attribCounter = (gl._attribCounter || 0) + 1;
      return gl._attribCounter;
    },
    getUniformLocation(p, name) { return name; },
    activeTexture(u) { calls.push(["activeTexture", u]); gl._active = u; },
    bindTexture(t, tex) {
      calls.push(["bindTexture", t, tex]);
      if (gl._active === gl.TEXTURE0) gl._binding0 = tex;
    },
    bindFramebuffer(t, f) { calls.push(["bindFramebuffer", t, f]); },
    viewport(x, y, w, h) { calls.push(["viewport", x, y, w, h]); },
    clearDepth(d) { calls.push(["clearDepth", d]); },
    clear(m) { calls.push(["clear", m]); },
    useProgram(p) { calls.push(["useProgram", p]); gl._program = p; },
    uniformMatrix4fv(l, tr, m) {
      calls.push(["uniformMatrix4fv", l, tr, m ? Array.from(m) : null]);
    },
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
    enableVertexAttribArray(i) {
      calls.push(["enableVertexAttribArray", i]); gl._enabled[i] = true;
    },
    disableVertexAttribArray(i) {
      calls.push(["disableVertexAttribArray", i]); gl._enabled[i] = false;
    },
    vertexAttribPointer(i, s, ty, n, st, o) { calls.push(["vertexAttribPointer", i, s, ty, n, st, o]); },
    vertexAttribDivisor(i, d) { calls.push(["vertexAttribDivisor", i, d]); gl._divisors[i] = d; },
    vertexAttrib2f(i, x, y) { calls.push(["vertexAttrib2f", i, x, y]); },
    vertexAttrib4f(i, x, y, z, w) { calls.push(["vertexAttrib4f", i, x, y, z, w]); },
    drawArrays(m, f, c) {
      calls.push(["drawArrays", m, f, c]); gl._snapshotState();
    },
    drawElements(m, c, t, o) {
      calls.push(["drawElements", m, c, t, o]); gl._snapshotState();
    },
    drawArraysInstanced(m, f, c, ic) {
      calls.push(["drawArraysInstanced", m, f, c, ic]); gl._snapshotState();
    },
    createBuffer() {
      gl._bufferCounter = (gl._bufferCounter || 0) + 1;
      return { id: "buf" + gl._bufferCounter };
    },
    deleteBuffer(b) { calls.push(["deleteBuffer", b]); },
    deleteShader(s) { calls.push(["deleteShader", s]); },
    deleteProgram(p) { calls.push(["deleteProgram", p]); },
    _snapshotState() {
      const call = calls[calls.length - 1];
      call.__state = {
        enabled: gl._enabled.slice(),
        divisors: gl._divisors.slice(),
        program: gl._program,
      };
    },
  };
  return gl;
}

function findDraw(gl, kind) {
  return gl.calls.find(function (c) { return c[0] === kind && c.__state; });
}

function makeStaticShadowProgram() {
  return {
    program: { id: "static-shadow-program" },
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

function makeSkinnedShadowProgram() {
  return {
    program: { id: "skinned-shadow-program" },
    attributes: { position: 1, uv: 2, joints: 3, weights: 4 },
    uniforms: {
      lightViewProjection: "u_lightViewProjection",
      modelMatrix: "u_modelMatrix",
      jointMatrices: "u_jointMatrices",
      hasSkin: "u_hasSkin",
      albedoMap: "u_albedoMap",
      hasAlbedoMap: "u_hasAlbedoMap",
      opacity: "u_opacity",
      alphaCutoff: "u_alphaCutoff",
    },
  };
}

const LIGHT_MATRIX = Float32Array.from({ length: 16 }, (_, i) => i + 1);

function makeSkinnedCaster(overrides) {
  const caster = {
    castShadow: true,
    viewCulled: false,
    directVertices: true,
    vertexOffset: 0,
    vertexCount: 3,
    modelMatrix: Float32Array.from([2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 1]),
    vertices: {
      positions: Float32Array.from({ length: 9 }, (_, i) => i + 1),
      uvs: Float32Array.from({ length: 6 }, () => 0.5),
      joints: Float32Array.from([0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0]),
      weights: Float32Array.from([1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]),
      indices: Uint32Array.from([0, 1, 2, 0, 1, 2]),
    },
    skin: { jointMatrices: Float32Array.from({ length: 64 * 16 }, (_, i) => (i % 7) + 1) },
  };
  return Object.assign(caster, overrides || {});
}

function makeBundle(casters) {
  return { meshObjects: casters, materials: [] };
}

function makeResources() {
  return { framebuffer: { id: "shadow-fbo" }, size: 512 };
}

function indexOfCall(calls, expected) {
  return calls.findIndex(function (c) {
    return c.length === expected.length &&
      expected.every(function (v, i) { return c[i] === v; });
  });
}

function runShadowPass(context, gl, args) {
  vm.runInContext("renderSceneShadowPass", context).apply(null, args);
}

test("skinned shadow vertex source composes skin palette before model, 64 joints, hasSkin gate", () => {
  assert.ok(SKINNED_VS_SOURCE.includes("uniform mat4 u_jointMatrices[64];"));
  assert.ok(SKINNED_VS_SOURCE.includes("uniform bool u_hasSkin;"));
  assert.ok(SKINNED_VS_SOURCE.includes("in vec4 a_joints;"));
  assert.ok(SKINNED_VS_SOURCE.includes("in vec4 a_weights;"));
  const skinApply = SKINNED_VS_SOURCE.indexOf("pos = skinMatrix * pos;");
  const modelApply = SKINNED_VS_SOURCE.indexOf("u_modelMatrix * pos");
  const skinSum = SKINNED_VS_SOURCE.indexOf("a_weights.x * u_jointMatrices[int(a_joints.x)]");
  assert.ok(skinSum >= 0, "weighted 4-joint palette sum present");
  assert.ok(skinApply >= 0 && modelApply > skinApply,
    "skin matrix applied before the model matrix, matching the color stage");
  assert.ok(SKINNED_VS_SOURCE.includes("u_lightViewProjection * (u_modelMatrix * pos)"));
});

test("skinned program compiles the exact shared static fragment, no instanced fragment, queries skin bindings", () => {
  const context = createContext();
  const sharedFragment = vm.runInContext("SCENE_SHADOW_FRAGMENT_SOURCE", context);
  const sharedVertex = vm.runInContext("SCENE_SHADOW_SKINNED_VERTEX_SOURCE", context);
  // Scoped to the actual skinned factory only: CREATORS_SOURCE legitimately
  // contains the instanced factory too, so it is never scanned here.
  assert.ok(SKINNED_CREATOR_SOURCE.includes("SCENE_SHADOW_FRAGMENT_SOURCE"),
    "skinned program compiles the shared static fragment constant");
  assert.ok(!SKINNED_CREATOR_SOURCE.includes("sceneShadowFragmentSource("),
    "no instanced alpha fragment builder leaks into the skinned depth program");

  vm.runInContext(`
    globalThis.__compiled = [];
    globalThis.__queriedAttribs = [];
    globalThis.__queriedUniforms = [];
    globalThis.scenePBRCompileShader = function (gl, type, source) {
      globalThis.__compiled.push({ type: type, source: source });
      return { id: "shader" + globalThis.__compiled.length };
    };
    globalThis.scenePBRLinkProgram = function (gl, vs, fs, label) {
      return { id: "program", label: label };
    };
  `, context, { filename: "test-stubs" });
  let attribCounter = 0;
  let uniformCounter = 0;
  context.__gl = {
    VERTEX_SHADER: 0x8b31,
    FRAGMENT_SHADER: 0x8b30,
    deleteShader() {},
    getAttribLocation(p, name) { context.__queriedAttribs.push(name); return attribCounter++; },
    getUniformLocation(p, name) { context.__queriedUniforms.push(name); return uniformCounter++; },
  };
  vm.runInContext(SKINNED_CREATOR_SOURCE, context, { filename: "webgl.ts#skinned-creator" });
  const info = vm.runInContext("createSceneShadowSkinnedProgram(__gl)", context);
  assert.equal(info.program.id, "program");
  assert.ok(info.vertexShader && info.fragmentShader,
    "handle exposes exactly the shader/program fields the dispose path deletes");
  // Exact shader identity — evaluated from production source, no copies, no
  // OR fallbacks.
  const frag = context.__compiled.find(function (c) { return c.type === 0x8b30; });
  assert.equal(frag.source, sharedFragment,
    "compiled fragment is byte-identical to the shared static mask constant");
  const vert = context.__compiled.find(function (c) { return c.type === 0x8b31; });
  assert.equal(vert.source, sharedVertex,
    "compiled vertex source is byte-identical to the skinned vertex constant");
  for (const name of ["a_position", "a_uv", "a_joints", "a_weights"]) {
    assert.ok(context.__queriedAttribs.includes(name), "attrib queried: " + name);
  }
  for (const name of ["u_lightViewProjection", "u_modelMatrix", "u_jointMatrices[0]",
    "u_hasSkin", "u_albedoMap", "u_hasAlbedoMap", "u_opacity", "u_alphaCutoff"]) {
    assert.ok(context.__queriedUniforms.includes(name),
      "uniform queried consistently with the PBR stage: " + name);
  }
  assert.equal(info.attributes.joints, 2);
  assert.equal(info.attributes.weights, 3);
  assert.ok(info.uniforms.jointMatrices && info.uniforms.hasSkin);
});

test("skinned indexed caster: real binder pins position/joints/weights/UV bindings, palette, hasSkin, model, draw state", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const program = makeStaticShadowProgram();
  const skinned = makeSkinnedShadowProgram();
  const caster = makeSkinnedCaster();
  const shadowState = { buffer: { id: "shadow-vbo" }, skinnedShadowProgram: skinned };
  runShadowPass(context, gl, [gl, program, makeResources(), LIGHT_MATRIX,
    makeBundle([caster]), shadowState, null, new Map(), null,
    makeProductionSkinnedHook(context)]);

  const draw = findDraw(gl, "drawElements");
  assert.ok(draw, "indexed skinned caster draws drawElements");
  assert.deepEqual(draw.slice(1), [gl.TRIANGLES, 6, gl.UNSIGNED_INT, 0]);
  assert.equal(gl.calls.some(function (c) { return c[0] === "drawArrays"; }), false,
    "indexed caster never takes the drawArrays path");

  const drawIdx = gl.calls.indexOf(draw);
  // Real production bindings, in order, before the draw.
  const pointerFor = function (loc, size) {
    return gl.calls.findIndex(function (c, i) {
      return i < drawIdx && c[0] === "vertexAttribPointer" && c[1] === loc && c[2] === size;
    });
  };
  assert.ok(pointerFor(1, 3) >= 0, "a_position bound, vec3");
  assert.ok(pointerFor(2, 2) >= 0, "a_uv bound, vec2");
  assert.ok(pointerFor(3, 4) >= 0, "a_joints bound, vec4");
  assert.ok(pointerFor(4, 4) >= 0, "a_weights bound, vec4");
  for (const loc of [1, 2, 3, 4]) {
    assert.ok(gl.calls.findIndex(function (c, i) {
      return i < drawIdx && c[0] === "enableVertexAttribArray" && c[1] === loc;
    }) >= 0, "attrib array enabled before draw: " + loc);
  }
  const uploaded = function (array) {
    return gl.calls.some(function (c) {
      // bufferData records as [name, target, data, usage]; verify the exact
      // typed data, target and usage at their real positions.
      return c[0] === "bufferData" &&
        c[1] === gl.ARRAY_BUFFER &&
        c[2] instanceof Float32Array &&
        c[3] === gl.DYNAMIC_DRAW &&
        JSON.stringify(Array.from(c[2])) === JSON.stringify(Array.from(array));
    });
  };
  assert.ok(uploaded(caster.vertices.positions), "position stream uploaded");
  assert.ok(uploaded(caster.vertices.joints), "joint stream uploaded");
  assert.ok(uploaded(caster.vertices.weights), "weight stream uploaded");
  assert.ok(uploaded(caster.vertices.uvs), "uv stream uploaded");

  // Draw-time GL state snapshot: skinned program active, exactly the skinned
  // attribs enabled, every divisor zero.
  const state = draw.__state;
  assert.equal(state.program, skinned.program, "skinned shadow program active at the draw");
  for (let i = 0; i < 16; i++) {
    assert.equal(state.divisors[i], 0, "divisor zero at draw for attrib " + i);
    const expected = i >= 1 && i <= 4;
    assert.equal(state.enabled[i], expected, "enabled state at draw for attrib " + i);
  }

  assert.ok(gl.calls.some(function (c) {
    return c[0] === "uniform1i" && c[1] === skinned.uniforms.hasSkin && c[2] === 1;
  }), "u_hasSkin set to 1");
  const jointUploads = gl.calls.filter(function (c) {
    return c[0] === "uniformMatrix4fv" && c[1] === skinned.uniforms.jointMatrices;
  });
  assert.equal(jointUploads.length, 1);
  assert.equal(jointUploads[0][2], false);
  assert.equal(jointUploads[0][3].length, 1024, "full 64-joint palette uploaded");
  assert.deepEqual(jointUploads[0][3], Array.from(caster.skin.jointMatrices));
  const modelUploads = gl.calls.filter(function (c) {
    return c[0] === "uniformMatrix4fv" && c[1] === skinned.uniforms.modelMatrix &&
      JSON.stringify(c[3]) === JSON.stringify(Array.from(caster.modelMatrix));
  });
  assert.equal(modelUploads.length, 1, "caster model matrix uploaded for the skinned draw");
  const uniforms1f = Object.fromEntries(
    gl.calls.filter(function (c) { return c[0] === "uniform1f"; }).map(function (c) { return [c[1], c[2]]; }));
  assert.equal(uniforms1f[skinned.uniforms.opacity], 1);
  assert.equal(uniforms1f[skinned.uniforms.alphaCutoff], -1);
  assert.equal(uniforms1f[skinned.uniforms.hasAlbedoMap], 0);

  // Program restored to the static shadow program's actual GL program.
  assert.ok(gl.calls.some(function (c, i) {
    return i > drawIdx && c[0] === "useProgram" && c[1] === program.program;
  }), "static shadow program reactivated after skinned draws");
});

test("skinned nonindexed caster: real binder takes drawArrays over vertexCount", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const caster = makeSkinnedCaster();
  delete caster.vertices.indices;
  const skinned = makeSkinnedShadowProgram();
  const shadowState = { buffer: {}, skinnedShadowProgram: skinned };
  runShadowPass(context, gl, [gl, makeStaticShadowProgram(), makeResources(), LIGHT_MATRIX,
    makeBundle([caster]), shadowState, null, new Map(), null,
    makeProductionSkinnedHook(context)]);
  const draw = findDraw(gl, "drawArrays");
  assert.ok(draw, "nonindexed skinned caster draws drawArrays");
  assert.deepEqual(draw.slice(1, 5), [gl.TRIANGLES, 0, 3],
    "positional draw arguments match exactly (state snapshot compared separately)");
  assert.equal(gl.calls.some(function (c) { return c[0] === "drawElements"; }), false,
    "nonindexed caster never takes the drawElements path");
  const state = draw.__state;
  assert.equal(state.program, skinned.program, "skinned shadow program active at the draw");
  for (let i = 0; i < 16; i++) {
    assert.equal(state.divisors[i], 0, "divisor zero at draw for attrib " + i);
    const expected = i >= 1 && i <= 4;
    assert.equal(state.enabled[i], expected, "enabled state at draw for attrib " + i);
  }
});

test("missing UVs are neutralized by the real binder, not left stale", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const caster = makeSkinnedCaster();
  delete caster.vertices.uvs;
  const skinned = makeSkinnedShadowProgram();
  const shadowState = { buffer: {}, skinnedShadowProgram: skinned };
  runShadowPass(context, gl, [gl, makeStaticShadowProgram(), makeResources(), LIGHT_MATRIX,
    makeBundle([caster]), shadowState, null, new Map(), null,
    makeProductionSkinnedHook(context)]);
  // The fixture keeps its indices, so the real binder takes the indexed path.
  const draw = findDraw(gl, "drawElements");
  const drawIdx = gl.calls.indexOf(draw);
  assert.ok(drawIdx >= 0, "indexed skinned draw happens with missing UVs");
  const disIdx = gl.calls.findIndex(function (c, i) {
    return i < drawIdx && c[0] === "disableVertexAttribArray" && c[1] === 2;
  });
  const neutralIdx = gl.calls.findIndex(function (c, i) {
    return i < drawIdx && c[0] === "vertexAttrib2f" && c[1] === 2 && c[2] === 0 && c[3] === 0;
  });
  assert.ok(disIdx >= 0, "uv attrib array disabled when UVs are missing");
  assert.ok(neutralIdx >= 0, "uv attrib neutralized to (0,0) when UVs are missing");
  assert.equal(draw.__state.enabled[2], false, "uv array disabled at draw time");
});

test("skinned casters bypass the pass cache; in-place pose change updates the actually uploaded palette; removal flushes; static caching resumes", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const program = makeStaticShadowProgram();
  const skinned = makeSkinnedShadowProgram();
  const resources = makeResources();
  const shadowState = { buffer: {}, skinnedShadowProgram: skinned };
  // Intentional nonindexed fixture: drawArrays is the counted draw.
  const caster = makeSkinnedCaster();
  delete caster.vertices.indices;
  const run = function (casters) {
    runShadowPass(context, gl, [gl, program, resources, LIGHT_MATRIX,
      makeBundle(casters), shadowState, null, new Map(), null,
      makeProductionSkinnedHook(context)]);
  };
  const draws = function () {
    return gl.calls.filter(function (c) { return c[0] === "drawArrays"; }).length;
  };
  const jointUploads = function () {
    return gl.calls.filter(function (c) {
      return c[0] === "uniformMatrix4fv" && c[1] === skinned.uniforms.jointMatrices;
    });
  };

  run([caster]);
  assert.equal(draws(), 1, "first skinned pass draws");
  run([caster]);
  assert.equal(draws(), 2, "identical hash is ignored while a skinned caster participates");
  // In-place pose change: same arrays, same bounds, mutated palette. The
  // actually uploaded uniform value must reflect the mutation.
  caster.skin.jointMatrices[0] = 42;
  run([caster]);
  assert.equal(draws(), 3, "in-place joint palette change redraws");
  const uploads = jointUploads();
  assert.ok(uploads.length >= 3);
  assert.equal(uploads[uploads.length - 1][3][0], 42,
    "the uploaded palette value is the mutated one, read from the real upload call");
  const clearsBefore = gl.calls.filter(function (c) { return c[0] === "clear"; }).length;
  run([]);
  assert.equal(draws(), 3, "removal draws nothing");
  assert.equal(gl.calls.filter(function (c) { return c[0] === "clear"; }).length, clearsBefore + 1,
    "depth map cleared after the last skinned caster is removed");
  const callsBefore = gl.calls.length;
  run([]);
  assert.equal(gl.calls.length, callsBefore, "identical static-only frame is cached again");
});

test("static-only scenes keep the hash cache even with the skinned wiring present", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const resources = makeResources();
  const shadowState = { buffer: {}, skinnedShadowProgram: makeSkinnedShadowProgram() };
  const staticCaster = {
    castShadow: true, viewCulled: false,
    vertexOffset: 0, vertexCount: 3, materialIndex: 0,
  };
  const bundle = makeBundle([staticCaster]);
  bundle.materials = [{ opacity: 1, alphaCutoff: null }];
  bundle.worldMeshPositions = Float32Array.from({ length: 9 }, function (_, i) { return i + 1; });
  const args = [gl, makeStaticShadowProgram(), resources, LIGHT_MATRIX, bundle,
    shadowState, null, new Map(), null, null];
  runShadowPass(context, gl, args);
  assert.equal(gl.calls.filter(function (c) { return c[0] === "drawArrays"; }).length, 1);
  const afterFirst = gl.calls.length;
  runShadowPass(context, gl, args);
  assert.equal(gl.calls.length, afterFirst, "static frame stays fully cached");
});

test("a null or missing skinned hook skips skinned casters without breaking the pass", () => {
  const context = createBinderContext(createGL());
  const caster = makeSkinnedCaster();
  const shadowState = { buffer: {}, skinnedShadowProgram: makeSkinnedShadowProgram() };

  const gl1 = createGL();
  runShadowPass(context, gl1, [gl1, makeStaticShadowProgram(), makeResources(), LIGHT_MATRIX,
    makeBundle([caster]), shadowState, null, new Map(), null,
    function () { return null; }]);
  assert.equal(gl1.calls.some(function (c) { return c[0] === "drawArrays" || c[0] === "drawElements"; }), false,
    "hook returning null skips the draw");
  // useProgram receives the actual GL program, not the wrapper object.
  assert.ok(gl1.calls.some(function (c) {
    return c[0] === "useProgram" && c[1] && c[1].id === "static-shadow-program";
  }), "program state restored even with zero skinned draws");

  const gl2 = createGL();
  runShadowPass(context, gl2, [gl2, makeStaticShadowProgram(), makeResources(), LIGHT_MATRIX,
    makeBundle([caster]), shadowState, null, new Map(), null]);
  assert.equal(gl2.calls.some(function (c) { return c[0] === "drawArrays" || c[0] === "drawElements"; }), false,
    "missing hook (9-arg legacy callers) skips skinned casters gracefully");
});

test("stale instanced/PBR attribute state is neutralized BEFORE the first skinned draw; reset always runs, even when the binder returns null", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const program = makeStaticShadowProgram();
  const skinned = makeSkinnedShadowProgram();
  const shadowState = { buffer: {}, skinnedShadowProgram: skinned };
  // Simulate leftover instanced/PBR state: a divisor-1 array and a foreign
  // enabled array. The direct-attribute binder only enables/points; the pass
  // must reset divisors (including on reused skinned locations) and disable
  // foreign arrays before any skinned draw.
  gl._divisors[3] = 1;
  gl._enabled[3] = true;
  gl._enabled[12] = true;
  runShadowPass(context, gl, [gl, program, makeResources(), LIGHT_MATRIX,
    makeBundle([makeSkinnedCaster()]), shadowState, null, new Map(), null,
    makeProductionSkinnedHook(context)]);
  const draw = findDraw(gl, "drawElements");
  assert.ok(draw, "skinned draw happened");
  const state = draw.__state;
  assert.equal(state.divisors[3], 0, "divisor on a reused skinned location zeroed before the draw");
  assert.equal(state.enabled[12], false, "foreign enabled array disabled before the draw");
  assert.equal(state.enabled[3], true, "skinned joints array enabled at the draw");
  const drawIdx = gl.calls.indexOf(draw);
  for (let i = 0; i < 16; i++) {
    assert.ok(gl.calls.findIndex(function (c, idx) {
      return idx > drawIdx && c[0] === "vertexAttribDivisor" && c[1] === i && c[2] === 0;
    }) > drawIdx, "divisor zeroed after skinned draw for attrib " + i);
    assert.ok(gl.calls.findIndex(function (c, idx) {
      return idx > drawIdx && c[0] === "disableVertexAttribArray" && c[1] === i;
    }) > drawIdx, "attrib array disabled after skinned draw for attrib " + i);
  }
  assert.ok(gl.calls.findIndex(function (c, idx) {
    return idx > drawIdx && c[0] === "useProgram" && c[1] === program.program;
  }) > drawIdx, "static shadow program restored after the reset");

  // Binder partially binds then returns null (zero vertexCount): the cleanup
  // must still run — attribute state never survives the skinned section.
  const gl2 = createGL();
  const context2 = createBinderContext(gl2);
  gl2._divisors[5] = 1;
  gl2._enabled[5] = true;
  runShadowPass(context2, gl2, [gl2, program, makeResources(), LIGHT_MATRIX,
    makeBundle([makeSkinnedCaster({ vertexCount: 0 })]),
    { buffer: {}, skinnedShadowProgram: skinned }, null, new Map(), null,
    makeProductionSkinnedHook(context2)]);
  assert.equal(gl2.calls.some(function (c) {
    return c[0] === "drawArrays" || c[0] === "drawElements";
  }), false, "binder null means no draw");
  assert.ok(gl2.calls.some(function (c) {
    return c[0] === "vertexAttribDivisor" && c[1] === 5 && c[2] === 0;
  }), "cleanup runs even when the binder never produced a draw");
  assert.ok(gl2.calls.some(function (c) {
    return c[0] === "useProgram" && c[1] === program.program;
  }), "program restored when the binder returned null");
});

test("missing and zero-length joint palettes never draw with u_hasSkin=1 and never fall back to bind pose; fixing the palette draws", () => {
  const invalidPalettes = [
    ["missing palette", {}],
    ["zero-length palette", { jointMatrices: new Float32Array(0) }],
  ];
  for (const [label, invalidSkin] of invalidPalettes) {
    const gl = createGL();
    const context = createBinderContext(gl);
    const program = makeStaticShadowProgram();
    const skinned = makeSkinnedShadowProgram();
    const resources = makeResources();
    const shadowState = { buffer: {}, skinnedShadowProgram: skinned };
    const caster = makeSkinnedCaster();
    caster.skin = invalidSkin; // participates (skin + joints/weights) but palette unusable
    const args = [gl, program, resources, LIGHT_MATRIX,
      makeBundle([caster]), shadowState, null, new Map(), null,
      makeProductionSkinnedHook(context)];
    runShadowPass(context, gl, args);
    assert.equal(gl.calls.some(function (c) {
      return c[0] === "drawArrays" || c[0] === "drawElements";
    }), false, label + ": no draw at all — no stale skinned draw, no bind-pose fallback");
    assert.equal(gl.calls.some(function (c) {
      return c[0] === "uniform1i" && c[1] === skinned.uniforms.hasSkin && c[2] === 1;
    }), false, label + ": u_hasSkin never set to 1 without a usable palette");
    assert.equal(gl.calls.filter(function (c) { return c[0] === "clear"; }).length, 1,
      label + ": the pass still redraws, flushing any old skinned shadow");
    caster.skin = { jointMatrices: Float32Array.from({ length: 64 * 16 }, (_, i) => (i % 5) + 1) };
    runShadowPass(context, gl, args);
    const draw = findDraw(gl, "drawElements");
    assert.ok(draw, label + ": with a valid palette the skinned draw happens");
    assert.ok(gl.calls.some(function (c) {
      return c[0] === "uniform1i" && c[1] === skinned.uniforms.hasSkin && c[2] === 1;
    }), label + ": u_hasSkin set to 1 only for a valid palette");
  }
});

test("masked skinned caster binds its texture with opacity/cutoff uniforms, then the unmasked skinned caster resets to opaque", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const program = makeStaticShadowProgram();
  const skinned = makeSkinnedShadowProgram();
  const shadowState = { buffer: {}, skinnedShadowProgram: skinned };
  const texture = { id: "skinned-mask-tex" };
  // The production resolve path computes the descriptor cache key from the
  // actual VM functions; only the cache lookup is faked, always returning the
  // loaded texture record.
  let gets = 0;
  const textureCache = {
    get() {
      gets += 1;
      return { texture: texture, loaded: true };
    },
  };
  const masked = makeSkinnedCaster({ materialIndex: 0 });
  const unmasked = makeSkinnedCaster({ materialIndex: 1 });
  const bundle = makeBundle([masked, unmasked]);
  bundle.materials = [
    { alphaCutoff: 0.5, opacity: 0.5, textureDescriptors: { baseColor: { uri: "skinned-mask.png" } } },
    { opacity: 1, alphaCutoff: null },
  ];
  runShadowPass(context, gl, [gl, program, makeResources(), LIGHT_MATRIX,
    bundle, shadowState, null, textureCache, null,
    makeProductionSkinnedHook(context)]);

  assert.ok(gets > 0, "mask resolution read the texture cache");
  const draws = gl.calls.filter(function (c) { return c[0] === "drawElements"; });
  assert.equal(draws.length, 2, "both skinned casters draw");
  const maskBind = gl.calls.find(function (c) {
    return c[0] === "bindTexture" && c[2] === texture;
  });
  assert.ok(maskBind, "mask texture bound during the pass");
  assert.ok(gl.calls.indexOf(maskBind) < gl.calls.indexOf(draws[0]),
    "mask texture bound before the masked skinned draw");
  const uniform1fValues = function (name) {
    return gl.calls.filter(function (c) {
      return c[0] === "uniform1f" && c[1] === name;
    }).map(function (c) { return c[2]; });
  };
  assert.deepEqual(uniform1fValues(skinned.uniforms.opacity), [0.5, 1],
    "masked opacity then opaque reset for the unmasked skinned caster");
  assert.deepEqual(uniform1fValues(skinned.uniforms.alphaCutoff), [0.5, -1],
    "masked cutoff then -1 reset");
  assert.deepEqual(uniform1fValues(skinned.uniforms.hasAlbedoMap), [1, 0],
    "hasAlbedoMap 1 with a loaded texture, then 0 for the unmasked caster");
});

test("retained static casters are not double-drawn when they carry skin data", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const program = makeStaticShadowProgram();
  const skinned = makeSkinnedShadowProgram();
  const shadowState = { buffer: {}, skinnedShadowProgram: skinned };
  const caster = makeSkinnedCaster({ retainedGeometry: true });
  const retainedHookCalls = [];
  runShadowPass(context, gl, [gl, program, makeResources(), LIGHT_MATRIX,
    makeBundle([caster]), shadowState,
    function (obj, prog) { retainedHookCalls.push([obj, prog]); return 6; },
    new Map(), null, makeProductionSkinnedHook(context)]);
  assert.equal(retainedHookCalls.length, 0,
    "retained bind-pose binder never runs for skinned casters");
  const draw = findDraw(gl, "drawElements");
  assert.ok(draw, "exactly one draw: the skinned section's deformed draw");
  assert.equal(draw.__state.program, skinned.program,
    "the single draw went through the skinned program");
  assert.equal(gl.calls.filter(function (c) {
    return c[0] === "drawElements" || c[0] === "drawArrays";
  }).length, 1, "exactly one total draw in the frame");
});

test("mixed skinned/instanced frame: programs and divisor state transition correctly through real draw snapshots", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  const program = makeStaticShadowProgram();
  const skinned = makeSkinnedShadowProgram();
  const instanced = {
    program: { id: "instanced-shadow-program" },
    attributes: { position: 5, uv: 6, instanceMatrix: [7, 8, 9, 10], instanceColor: 11 },
    uniforms: { lightViewProjection: "u_lvp", albedoMap: "u_albedoMap" },
  };
  const shadowState = {
    buffer: {},
    skinnedShadowProgram: skinned,
    instancedShadowProgram: instanced,
    ensureInstancedVBO: function () { return { id: "instanced-vbo" }; },
  };
  const caster = makeSkinnedCaster();
  const bundle = makeBundle([caster]);
  bundle.instancedMeshes = [{
    castShadow: true,
    instanceCount: 1,
    transforms: Float32Array.from({ length: 16 }, function (_, i) { return i + 1; }),
  }];
  const resolveInstancedGeometry = function () {
    return { positions: Float32Array.from({ length: 9 }, function (_, i) { return i + 1; }), vertexCount: 3 };
  };
  // Leftover divisor on a location the skinned section will reuse.
  gl._divisors[3] = 1;
  runShadowPass(context, gl, [gl, program, makeResources(), LIGHT_MATRIX,
    bundle, shadowState, null, new Map(), resolveInstancedGeometry,
    makeProductionSkinnedHook(context)]);
  const skinnedDraw = findDraw(gl, "drawElements");
  const instancedDraw = findDraw(gl, "drawArraysInstanced");
  assert.ok(skinnedDraw && instancedDraw, "both skinned and instanced draws happened");
  assert.ok(gl.calls.indexOf(skinnedDraw) < gl.calls.indexOf(instancedDraw),
    "skinned section runs before the instanced section");
  assert.equal(skinnedDraw.__state.program, skinned.program);
  assert.equal(skinnedDraw.__state.divisors[3], 0,
    "stale divisor on a skinned location zeroed before the skinned draw");
  for (let i = 0; i < 16; i++) {
    assert.equal(skinnedDraw.__state.divisors[i], 0, "skinned draw divisor zero: " + i);
  }
  assert.equal(instancedDraw.__state.program, instanced.program);
  for (const loc of instanced.attributes.instanceMatrix) {
    assert.equal(instancedDraw.__state.divisors[loc], 1,
      "instanced matrix divisor is 1 at the instanced draw: " + loc);
  }
  for (const loc of instanced.attributes.instanceMatrix) {
    assert.ok(gl.calls.some(function (c) {
      return c[0] === "vertexAttribDivisor" && c[1] === loc && c[2] === 0 &&
        gl.calls.indexOf(c) > gl.calls.indexOf(instancedDraw);
    }), "instanced divisor reset after the instanced draw: " + loc);
  }
  // The static program is restored when the skinned section ends — BEFORE the
  // instanced section. The instanced path intentionally leaves its own program
  // bound (the renderer rebinds the color program after the pass), so the
  // actual contract is: static restored after skinned, the instanced program
  // active at the instanced draw, and fully neutral divisors/arrays after.
  assert.ok(gl.calls.some(function (c) {
    return c[0] === "useProgram" && c[1] === program.program &&
      gl.calls.indexOf(c) > gl.calls.indexOf(skinnedDraw) &&
      gl.calls.indexOf(c) < gl.calls.indexOf(instancedDraw);
  }), "static shadow program restored after the skinned section, before instanced draws");
  const lastInstanced = gl.calls.filter(function (c) { return c[0] === "drawArraysInstanced"; }).pop();
  const afterInstanced = gl.calls.slice(gl.calls.indexOf(lastInstanced) + 1);
  for (let i = 0; i < 16; i++) {
    assert.ok(afterInstanced.some(function (c) {
      return c[0] === "vertexAttribDivisor" && c[1] === i && c[2] === 0;
    }), "divisor neutral after the instanced section for attrib " + i);
    assert.ok(afterInstanced.some(function (c) {
      return c[0] === "disableVertexAttribArray" && c[1] === i;
    }), "attrib array disabled after the instanced section for attrib " + i);
  }
});

test("lazy skinned shadow program is created once and reused across frames", () => {
  const gl = createGL();
  const context = createBinderContext(gl);
  // The binder VM only contains the pass + binder chain; evaluate the ACTUAL
  // skinned factory so the lazy creation path inside the pass runs real
  // production code. The shader source constants it reads are already defined
  // in the VM: createContext evaluates SHADER_BUILDER, which includes the
  // full SCENE_SHADOW_SKINNED_VERTEX_SOURCE constant. SKINNED_VS_SOURCE is a
  // deliberately incomplete slice (it excludes the closing ].join) used only
  // for text assertions, so it must never be evaluated here.
  vm.runInContext(SKINNED_CREATOR_SOURCE, context, { filename: "webgl.ts#skinned-creator-lazy" });
  vm.runInContext(`
    var __compiledShaders = 0;
    var __linkedPrograms = 0;
    scenePBRCompileShader = function (gl2, type, source) {
      __compiledShaders += 1;
      return { id: "s" + __compiledShaders };
    };
    scenePBRLinkProgram = function (gl2, vs, fs, label) {
      __linkedPrograms += 1;
      return { id: "p" + __linkedPrograms };
    };
  `, context, { filename: "test-lazy-stubs" });
  const program = makeStaticShadowProgram();
  const shadowState = { buffer: {} };
  const run = function () {
    runShadowPass(context, gl, [gl, program, makeResources(), LIGHT_MATRIX,
      makeBundle([makeSkinnedCaster()]), shadowState, null, new Map(), null,
      makeProductionSkinnedHook(context)]);
  };
  run();
  const firstDraw = findDraw(gl, "drawElements");
  assert.ok(firstDraw, "first frame creates and uses the lazy skinned program");
  assert.equal(vm.runInContext("__linkedPrograms", context), 1, "program linked exactly once");
  const lazyProgram = shadowState.skinnedShadowProgram;
  assert.ok(lazyProgram && lazyProgram.program, "lazy program cached on shadowState");
  run();
  const draws = gl.calls.filter(function (c) { return c[0] === "drawElements" && c.__state; });
  assert.equal(draws.length, 2, "second frame draws again");
  assert.equal(vm.runInContext("__linkedPrograms", context), 1,
    "lazy program reused once — no relink on the second frame");
  assert.equal(draws[1].__state.program, draws[0].__state.program,
    "both frames drew with the same cached GL program");
});

test("actual production disposal branch deletes the skinned program's shaders and program", () => {
  const gl = createGL();
  const context = createContext();
  context.__gl = gl;
  context.__shadowState = {
    skinnedShadowProgram: {
      program: { id: "skinned-program" },
      vertexShader: { id: "skinned-vs" },
      fragmentShader: { id: "skinned-fs" },
    },
  };
  vm.runInContext("var gl = __gl; var shadowState = __shadowState;", context,
    { filename: "test-disposal-env" });
  vm.runInContext(SKINNED_DISPOSAL_SOURCE, context, { filename: "webgl.ts#skinned-disposal" });
  assert.ok(gl.calls.some(function (c) {
    return c[0] === "deleteShader" && c[1] && c[1].id === "skinned-vs";
  }), "vertex shader deleted by the real disposal branch");
  assert.ok(gl.calls.some(function (c) {
    return c[0] === "deleteShader" && c[1] && c[1].id === "skinned-fs";
  }), "fragment shader deleted by the real disposal branch");
  assert.ok(gl.calls.some(function (c) {
    return c[0] === "deleteProgram" && c[1] && c[1].id === "skinned-program";
  }), "program deleted by the real disposal branch");
  assert.equal(context.__shadowState.skinnedShadowProgram, null,
    "skinned program reference cleared by the real disposal branch");
});
