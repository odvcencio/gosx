"use strict";
// Scene3D cascaded-shadow texture-unit budget regressions.
//
// These tests drive the PRODUCTION webgl.ts upload/render path and assert on
// actual per-unit bound-texture identity, matrix arrays and split counts:
//   - partial budget on a 16-unit GL with an environment map: two authored
//     4-cascade lights must never let slot 1's later cascades overwrite the
//     texture actually bound for slot 0 (or slot 1 cascade 0's own unit);
//   - zero-capacity slots disable has/index and never steal another slot's
//     texture;
//   - full budget (no environment) preserves 4+4 cascades;
//   - absent slots reset enables;
//   - end-to-end mount/render at reported 16- and 32-unit caps.
//
// All GL tracking happens in this file (a recording wrapper and a subclass of
// the harness FakeWebGLContext); the shared fake class is not modified.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const {
  bootstrapSource,
  FakeWebGLContext,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const srcDir = path.join(__dirname, "bootstrap-src");

function readBootstrapSource(name) {
  return fs.readFileSync(path.join(srcDir, name), "utf8");
}

function readRuntimeSource(name) {
  return fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", name), "utf8");
}

function sliceBetween(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  assert.ok(start >= 0, "start marker located: " + startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(end > start, "end marker located after start: " + endMarker);
  return source.slice(start, end);
}

function runFragment(context, source, filename) {
  vm.runInContext(source, context, { filename });
}

function callIn(context, expression) {
  return vm.runInContext(expression, context, { filename: "scene3d-shadow-budget-expression.js" });
}

// --- direct upload path (production functions inside a vm sandbox) ---------

function setupUploadContext() {
  const source = readRuntimeSource("webgl.ts");
  const context = vm.createContext({ console, window: {} });
  runFragment(context,
    "function sceneFiniteNumber(value, fallback) { const n = Number(value); return Number.isFinite(n) ? n : fallback; }" +
    "function sceneNumber(value, fallback) { const n = Number(value); return Number.isFinite(n) ? n : fallback; }",
    "shadow-budget-helpers.js");
  // Shared texture-unit allocator (production).
  runFragment(context,
    sliceBetween(readBootstrapSource("15a1-scene-texture-budget.ts"),
      "var SCENE_TEXTURE_UNIT_MATERIALS", "function sceneTextureMipBytes"),
    "15a1-scene-texture-budget.ts");
  // Scratch cascade buffers + guarded max-unit query + layout + upload path
  // (production slices; scenePBRBindTexture is injected below because the
  // recording GL in this file tracks binding identity itself).
  runFragment(context,
    sliceBetween(source, "var _scenePBRCascadeMatScratch", "function scenePBRMaxTextureUnits"),
    "webgl-scratch.ts");
  runFragment(context,
    sliceBetween(source, "function scenePBRMaxTextureUnits", "function scenePBRSlotCascadeCount"),
    "webgl-units.ts");
  runFragment(context,
    sliceBetween(source, "function scenePBRSlotCascadeCount", "function scenePBRTextureLayoutForFrame"),
    "webgl-counts.ts");
  runFragment(context,
    sliceBetween(source, "function scenePBRTextureLayoutForFrame", "// Upload cascaded-shadow uniforms"),
    "webgl-layout.ts");
  runFragment(context,
    sliceBetween(source, "// Upload cascaded-shadow uniforms", "var SCENE_IBL_BRDF_MODEL"),
    "webgl-upload.ts");
  runFragment(context,
    "function scenePBRBindTexture(gl, unit, texture, target) {" +
    "  gl.activeTexture(gl.TEXTURE0 + (unit | 0));" +
    "  gl.bindTexture(target === undefined ? gl.TEXTURE_2D : target, texture);" +
    "}",
    "shadow-budget-bind-helper.js");
  return { context };
}

// Recording GL: tracks which texture OBJECT is resident on each unit, plus a
// full op log, so tests can assert binding identity (not just sampler ints).
function createRecordingGL(maxUnits) {
  const gl = {
    TEXTURE0: 0x84C0,
    TEXTURE_2D: 0x0DE1,
    TEXTURE_CUBE_MAP: 0x8513,
    MAX_TEXTURE_IMAGE_UNITS: 0x8872,
    _maxUnits: maxUnits,
    _activeUnit: -1,
    ops: [],
    unitTextures: new Map(), // unit -> texture object currently bound (2D)
    getParameter(param) {
      return param === gl.MAX_TEXTURE_IMAGE_UNITS ? gl._maxUnits : null;
    },
    activeTexture(unit) {
      gl._activeUnit = unit - gl.TEXTURE0;
      gl.ops.push(["activeTexture", unit]);
    },
    bindTexture(target, texture) {
      if (target === gl.TEXTURE_2D) {
        gl.unitTextures.set(gl._activeUnit, texture);
      }
      gl.ops.push(["bindTexture", gl._activeUnit, texture && texture.id]);
    },
    uniform1i(loc, v) { gl.ops.push(["uniform1i", loc && loc.name, v]); },
    uniform1f(loc, v) { gl.ops.push(["uniform1f", loc && loc.name, v]); },
    uniform1fv(loc, v) { gl.ops.push(["uniform1fv", loc && loc.name, v ? Array.from(v) : null]); },
    uniformMatrix4fv(loc, transpose, v) { gl.ops.push(["uniformMatrix4fv", loc && loc.name, v ? Array.from(v) : null]); },
  };
  return gl;
}

function makeUniforms() {
  const names = [];
  for (let s = 0; s < 2; s++) {
    names.push("hasShadow" + s, "shadowBias" + s, "shadowSoftness" + s,
      "shadowLightIndex" + s, "shadowCascades" + s,
      "lightSpaceMatrices" + s, "shadowCascadeSplits" + s);
    for (let c = 0; c < 4; c++) names.push("shadowMap" + s + "_" + c);
  }
  const uniforms = {};
  for (const name of names) uniforms[name] = { name: "u_" + name };
  return uniforms;
}

// Deterministic slot fixture: matrix[0] = (seed+1)*100+c so each cascade's
// matrix is identifiable inside the uploaded 4x16 float array; splitFar
// always ends at 100 (the camera far plane).
function makeSlot(numCascades, seed) {
  const cascades = [];
  for (let c = 0; c < numCascades; c++) {
    const matrix = new Array(16).fill(0);
    matrix[0] = (seed + 1) * 100 + c;
    cascades.push({
      depthTexture: { id: "tex-s" + seed + "-c" + c },
      lightMatrix: matrix,
      splitNear: (c * 100) / numCascades,
      splitFar: Math.round(((c + 1) * 100) / numCascades),
    });
  }
  return { size: 256, numCascades, cascades };
}

function uploadInContext(context, gl, uniforms, slots, indices, lights, environment) {
  context.__gl = gl;
  context.__uniforms = uniforms;
  context.__slots = slots;
  context.__indices = indices;
  context.__lights = lights;
  context.__env = environment;
  callIn(context,
    "scenePBRUploadShadowUniforms(__gl, __uniforms, __slots, __indices, __lights, __env)");
}

function lastOp(gl, opName, locName) {
  for (let i = gl.ops.length - 1; i >= 0; i--) {
    const op = gl.ops[i];
    if (op[0] === opName && op[1] === locName) return op;
  }
  return null;
}

const lastInt = (gl, name) => { const op = lastOp(gl, "uniform1i", name); return op ? op[2] : undefined; };
const lastFv = (gl, name) => { const op = lastOp(gl, "uniform1fv", name); return op ? op[2] : null; };
const lastMat = (gl, name) => { const op = lastOp(gl, "uniformMatrix4fv", name); return op ? op[2] : null; };
const bindOps = (gl) => gl.ops.filter((op) => op[0] === "bindTexture");
const activationsOn = (gl, unit) =>
  gl.ops.filter((op) => op[0] === "activeTexture" && op[1] === gl.TEXTURE0 + unit).length;

test("WebGL texture layout gives two 4-cascade lights plus IBL exactly 5 shared units on a 16-unit GL", () => {
  const { context } = setupUploadContext();
  context.__slots = [makeSlot(4, 0), makeSlot(4, 1)];
  const layout = callIn(context,
    "scenePBRTextureLayoutForFrame(__slots, [0, 1], { envMap: 'studio.png' }, 16)");
  assert.deepEqual(Array.from(layout.shadows), [8, 9, 10, 11, 12]);
  assert.deepEqual({ ...layout.ibl }, { irradiance: 13, radiance: 14, brdfLUT: 15 });
  assert.equal(layout.warnings.length > 0, true);
});

test("WebGL shadow upload binds each cascade to a unique unit under a partial 16-unit budget", () => {
  const { context } = setupUploadContext();
  const gl = createRecordingGL(16);
  const slots = [makeSlot(4, 0), makeSlot(1, 1)];
  const uniforms = makeUniforms();
  const lights = [{ shadowBias: 0.005, shadowSoftness: 0.02 }, { shadowBias: 0.005, shadowSoftness: 0.02 }];
  uploadInContext(context, gl, uniforms, slots, [0, 1], lights, { envMap: "studio.png" });

  // Slot 0 keeps units 8..11; slot 1's first cascade owns unit 12. The old
  // implementation rebound slot 1's later cascades (fallback to a shared
  // unit) over already-bound cascade textures.
  assert.equal(gl.unitTextures.get(8).id, "tex-s0-c0");
  assert.equal(gl.unitTextures.get(9).id, "tex-s0-c1");
  assert.equal(gl.unitTextures.get(10).id, "tex-s0-c2");
  assert.equal(gl.unitTextures.get(11).id, "tex-s0-c3");
  assert.equal(gl.unitTextures.get(12).id, "tex-s1-c0");
  assert.equal(bindOps(gl).length, 5);
  assert.equal(new Set(bindOps(gl).map((op) => op[2])).size, 5);
  for (const unit of [8, 9, 10, 11, 12]) {
    assert.equal(activationsOn(gl, unit), 1, "unit " + unit + " activated exactly once");
  }

  // Sampler uniforms: active cascades on their own units, unused slot-1
  // samplers aliased to slot 1's own unit 12 (same already-bound texture).
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt(gl, "u_shadowMap0_" + c)), [8, 9, 10, 11]);
  assert.equal(lastInt(gl, "u_shadowMap1_0"), 12);
  assert.equal(lastInt(gl, "u_shadowMap1_1"), 12);
  assert.equal(lastInt(gl, "u_shadowMap1_2"), 12);
  assert.equal(lastInt(gl, "u_shadowMap1_3"), 12);

  // Counts, enables, matrices and splits follow the actual slot data.
  assert.equal(lastInt(gl, "u_shadowCascades0"), 4);
  assert.equal(lastInt(gl, "u_shadowCascades1"), 1);
  assert.equal(lastInt(gl, "u_hasShadow0"), 1);
  assert.equal(lastInt(gl, "u_hasShadow1"), 1);
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt(gl, "u_shadowLightIndex" + (c < 2 ? 0 : 1))), [0, 0, 1, 1]);
  const mat0 = lastMat(gl, "u_lightSpaceMatrices0");
  assert.equal(mat0[0], 100);
  assert.equal(mat0[16], 101);
  assert.equal(mat0[48], 103);
  const mat1 = lastMat(gl, "u_lightSpaceMatrices1");
  assert.equal(mat1[0], 200);
  assert.equal(mat1[16], 200);
  const splits0 = lastFv(gl, "u_shadowCascadeSplits0");
  assert.equal(splits0.length, 4);
  assert.equal(splits0[3], 100); // last active split reaches the camera far plane
  const splits1 = lastFv(gl, "u_shadowCascadeSplits1");
  assert.deepEqual(Array.from(splits1), [100, Infinity, Infinity, Infinity]);
});

test("WebGL shadow upload preserves 4+4 cascades when the shared budget has room (no environment)", () => {
  const { context } = setupUploadContext();
  const gl = createRecordingGL(16);
  const slots = [makeSlot(4, 0), makeSlot(4, 1)];
  const uniforms = makeUniforms();
  const lights = [{}, {}];
  uploadInContext(context, gl, uniforms, slots, [0, 1], lights, null);

  const units = [8, 9, 10, 11, 12, 13, 14, 15];
  assert.equal(bindOps(gl).length, 8);
  assert.equal(new Set(units.map((u) => gl.unitTextures.get(u).id)).size, 8);
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt(gl, "u_shadowMap0_" + c)), [8, 9, 10, 11]);
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt(gl, "u_shadowMap1_" + c)), [12, 13, 14, 15]);
  assert.equal(lastInt(gl, "u_shadowCascades0"), 4);
  assert.equal(lastInt(gl, "u_shadowCascades1"), 4);
  assert.equal(lastInt(gl, "u_hasShadow0"), 1);
  assert.equal(lastInt(gl, "u_hasShadow1"), 1);
  assert.equal(lastFv(gl, "u_shadowCascadeSplits1")[3], 100);
  assert.equal(lastMat(gl, "u_lightSpaceMatrices1")[0], 200);
});

test("zero-capacity shadow slot disables has/index without stealing another slot's texture", () => {
  const { context } = setupUploadContext();
  const gl = createRecordingGL(16);
  const uniforms = makeUniforms();
  const lights = [{}, {}];
  // Only four units exist and all of them belong to slot 0: slot 1's base
  // offset points past the end, i.e. zero capacity for slot 1.
  context.__gl = gl;
  context.__uniforms = uniforms;
  context.__slot0 = makeSlot(4, 0);
  context.__slot1 = makeSlot(4, 1);
  context.__units = [8, 9, 10, 11];
  context.__lights = lights;
  callIn(context, "uploadCascadedSlot(__gl, __uniforms, 0, __slot0, 0, __lights, __units, 0)");
  callIn(context, "uploadCascadedSlot(__gl, __uniforms, 1, __slot1, 1, __lights, __units, 4)");

  assert.equal(bindOps(gl).length, 4); // only slot 0's cascades were bound
  assert.equal(gl.unitTextures.get(8).id, "tex-s0-c0"); // never overwritten
  assert.equal(activationsOn(gl, 8), 1); // no re-bind into slot 0's unit
  assert.equal(lastInt(gl, "u_hasShadow0"), 1);
  assert.equal(lastInt(gl, "u_hasShadow1"), 0);
  assert.equal(lastInt(gl, "u_shadowLightIndex1"), -1);
  assert.equal(lastInt(gl, "u_shadowCascades1"), 0);
  // Unused samplers reset to material unit 0 without a bind.
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt(gl, "u_shadowMap1_" + c)), [0, 0, 0, 0]);
});

test("exhausted shared budget disables both shadow slots and binds nothing", () => {
  const { context } = setupUploadContext();
  const gl = createRecordingGL(8); // only the 8 material units fit
  const uniforms = makeUniforms();
  const slots = [makeSlot(4, 0), makeSlot(4, 1)];
  uploadInContext(context, gl, uniforms, slots, [0, 1], [{}, {}], { envMap: "studio.png" });
  assert.equal(bindOps(gl).length, 0);
  assert.equal(gl.unitTextures.size, 0);
  assert.equal(lastInt(gl, "u_hasShadow0"), 0);
  assert.equal(lastInt(gl, "u_hasShadow1"), 0);
  assert.equal(lastInt(gl, "u_shadowLightIndex0"), -1);
  assert.equal(lastInt(gl, "u_shadowLightIndex1"), -1);
});

test("absent shadow slot resets enable/index without extra texture binds", () => {
  const { context } = setupUploadContext();
  const gl = createRecordingGL(16);
  const uniforms = makeUniforms();
  const slots = [makeSlot(4, 0), null];
  uploadInContext(context, gl, uniforms, slots, [0, -1], [{}], null);
  assert.equal(bindOps(gl).length, 4);
  assert.equal(lastInt(gl, "u_hasShadow1"), 0);
  assert.equal(lastInt(gl, "u_shadowLightIndex1"), -1);
  assert.equal(lastInt(gl, "u_shadowCascades1"), 0);
  assert.equal(lastInt(gl, "u_shadowMap1_0"), 0); // reset, no bind
  assert.equal(activationsOn(gl, 8), 1);
});

test("cascade negotiation shrinks and restores with environment and unit budget", () => {
  const { context } = setupUploadContext();
  // New-helper coverage (isolated from the negative controls above, which
  // exercise only pre-existing names and fail against the unfixed source).
  const at16WithEnv = callIn(context, "scenePBRNegotiateShadowCascades([4, 4], { envMap: 'x' }, 16)");
  assert.deepEqual([...at16WithEnv], [4, 1]);
  const at16NoEnv = callIn(context, "scenePBRNegotiateShadowCascades([4, 4], null, 16)");
  assert.deepEqual([...at16NoEnv], [4, 4]);
  const at32WithEnv = callIn(context, "scenePBRNegotiateShadowCascades([4, 4], { envMap: 'x' }, 32)");
  assert.deepEqual([...at32WithEnv], [4, 4]);
  const at8WithEnv = callIn(context, "scenePBRNegotiateShadowCascades([4, 4], { envMap: 'x' }, 8)");
  assert.deepEqual([...at8WithEnv], [0, 0]);
  const at12WithEnv = callIn(context, "scenePBRNegotiateShadowCascades([4, 4], { envMap: 'x' }, 12)");
  assert.deepEqual([...at12WithEnv], [1, 0]); // fairness: earlier light wins the lone unit
  const at13WithEnv = callIn(context, "scenePBRNegotiateShadowCascades([4, 4], { envMap: 'x' }, 13)");
  assert.deepEqual([...at13WithEnv], [1, 1]); // one cascade per light before priority fill
});

test("stale 4+4 slots under a 5-unit partial budget disable slot 1 entirely", () => {
  const { context } = setupUploadContext();
  const gl = createRecordingGL(16);
  const uniforms = makeUniforms();
  const slots = [makeSlot(4, 0), makeSlot(4, 1)]; // stale over-capacity slot 1
  uploadInContext(context, gl, uniforms, slots, [0, 1], [{}, {}], { envMap: "studio.png" });

  assert.equal(bindOps(gl).length, 4); // only slot 0's cascades bound
  assert.equal(gl.unitTextures.get(12), undefined); // nothing bound into unit 12
  assert.equal(gl.unitTextures.get(8).id, "tex-s0-c0"); // slot 0 textures untouched
  assert.equal(lastInt(gl, "u_hasShadow1"), 0);
  assert.equal(lastInt(gl, "u_shadowLightIndex1"), -1);
  assert.equal(lastInt(gl, "u_shadowCascades1"), 0);
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt(gl, "u_shadowMap1_" + c)), [0, 0, 0, 0]);
});

// --- end-to-end mount/render via the shared harness ------------------------

const SCENE_TRIANGLE = {
  id: "shadow-triangle",
  kind: "gltf-mesh",
  materialKind: "pbr",
  castShadow: true,
  receiveShadow: true,
  vertices: {
    count: 3,
    positions: [0, 1, 0, -1, -1, 0, 1, -1, 0],
    normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
    uvs: [0.5, 1, 0, 0, 1, 0],
  },
};

// Tracking subclass of the shared fake: reports a configured
// MAX_TEXTURE_IMAGE_UNITS and records DRAW-TIME shadow evidence (current
// framebuffer/depth attachment, current program uniform values, and actual
// draws) so tests can assert what the GPU really sampled without touching
// the shared class. All methods forward to super.
function trackingContextClass(maxUnits) {
  return class extends FakeWebGLContext {
    constructor() {
      super();
      this.MAX_TEXTURE_IMAGE_UNITS = 0x8872;
      this._maxUnits = maxUnits;
      this._activeUnit = -1;
      this.unitTextures = new Map();
      // Framebuffer id -> depth-attachment texture id.
      this.depthTargets = new Map();
      // Depth texture id -> lightViewProjection matrix uploaded at its draw.
      this.shadowDrawMatrices = new Map();
      // Uniform snapshot of the most recent actual PBR draw.
      this.pbrDrawSnapshot = null;
      // Monotonic counter distinguishing successive recorded draws.
      this.drawCounter = 0;
    }
    getParameter(param) {
      if (param === this.MAX_TEXTURE_IMAGE_UNITS) return this._maxUnits;
      return super.getParameter(param);
    }
    activeTexture(unit) {
      this._activeUnit = unit - this.TEXTURE0;
      super.activeTexture(unit);
    }
    bindTexture(target, texture) {
      if (target === this.TEXTURE_2D && this._activeUnit >= 0) {
        this.unitTextures.set(this._activeUnit, texture && texture.id);
      }
      super.bindTexture(target, texture);
    }
    bindFramebuffer(target, framebuffer) {
      if (target === this.FRAMEBUFFER) this._currentFramebuffer = framebuffer || null;
      super.bindFramebuffer(target, framebuffer);
    }
    framebufferTexture2D(target, attachment, textarget, texture, level) {
      const depthAttachment = this.DEPTH_ATTACHMENT != null ? this.DEPTH_ATTACHMENT : 0x8d00;
      if (attachment === depthAttachment && this._currentFramebuffer) {
        this.depthTargets.set(this._currentFramebuffer.id, texture && texture.id);
      }
      super.framebufferTexture2D(target, attachment, textarget, texture, level);
    }
    _recordUniform(name, value) {
      const program = this._activeProgram;
      if (!program || !name) return;
      if (!program._uniformValues) program._uniformValues = {};
      program._uniformValues[name] = (ArrayBuffer.isView(value) || Array.isArray(value))
        ? Float32Array.from(value)
        : value;
    }
    uniform1f(location, value) {
      this._recordUniform(location && location.name, value);
      super.uniform1f(location, value);
    }
    uniform1i(location, value) {
      this._recordUniform(location && location.name, value);
      super.uniform1i(location, value);
    }
    uniform1fv(location, value) {
      this._recordUniform(location && location.name, value);
      super.uniform1fv(location, value);
    }
    uniformMatrix4fv(location, transpose, value) {
      this._recordUniform(location && location.name, value);
      super.uniformMatrix4fv(location, transpose, value);
    }
    _recordDraw() {
      const program = this._activeProgram;
      if (!program) return;
      this.drawCounter++;
      const values = program._uniformValues || {};
      if (program === this.programMatching("u_lightViewProjection")) {
        const textureID = this._currentFramebuffer && this.depthTargets.get(this._currentFramebuffer.id);
        if (textureID != null) {
          this.shadowDrawMatrices.set(textureID, Float32Array.from(values.u_lightViewProjection));
        }
      } else if (program === this.programMatching("u_specularF0")) {
        const snapshot = {
          sequence: this.drawCounter,
          samplerUnits: {},
          cascades: [values.u_shadowCascades0, values.u_shadowCascades1],
          hasShadow: [values.u_hasShadow0, values.u_hasShadow1],
          lightIndices: [values.u_shadowLightIndex0, values.u_shadowLightIndex1],
          splits: [values.u_shadowCascadeSplits0 || null, values.u_shadowCascadeSplits1 || null],
          matrices: [values.u_lightSpaceMatrices0 || null, values.u_lightSpaceMatrices1 || null],
          unitTextures: Object.fromEntries(this.unitTextures),
        };
        for (const slot of [0, 1]) {
          for (let c = 0; c < 4; c++) {
            const unit = values["u_shadowMap" + slot + "_" + c];
            if (typeof unit === "number") snapshot.samplerUnits[slot + "_" + c] = unit;
          }
        }
        this.pbrDrawSnapshot = snapshot;
      }
    }
    drawArrays(mode, first, count) {
      this._recordDraw();
      super.drawArrays(mode, first, count);
    }
    drawElements(mode, count, type, offset) {
      this._recordDraw();
      super.drawElements(mode, count, type, offset);
    }
    deleteFramebuffer(framebuffer) {
      const opCountBefore = this.ops.length;
      super.deleteFramebuffer(framebuffer);
      if (framebuffer && this.ops.length > opCountBefore) {
        this.depthTargets.delete(framebuffer.id);
      }
    }
    deleteTexture(texture) {
      const opCountBefore = this.ops.length;
      super.deleteTexture(texture);
      if (!texture || this.ops.length === opCountBefore) return;
      for (const [fbId, texId] of Array.from(this.depthTargets)) {
        if (texId === texture.id) this.depthTargets.delete(fbId);
      }
      this.shadowDrawMatrices.delete(texture.id);
    }
  };
}

// Draw-time shadow evidence: the recorded PBR draw sampled distinct, actually
// rendered depth targets whose uploaded cascade matrices match the shadow-pass
// matrices recorded for each texture. Returns the PBR snapshot. A missing
// snapshot is a failure, never a silent success.
function assertDrawTimeShadowEvidence(gl, expectedDepthResources) {
  const snap = gl.pbrDrawSnapshot;
  assert.ok(snap, "observed an actual PBR draw with shadow uniforms");
  assert.equal(gl.shadowDrawMatrices.size, expectedDepthResources);
  const seen = new Set();
  for (const slot of [0, 1]) {
    const cascades = snap.cascades[slot];
    const matrices = snap.matrices[slot];
    const splits = snap.splits[slot];
    if (!snap.hasShadow[slot]) {
      assert.equal(cascades, 0, "disabled slot " + slot + " negotiates zero cascades");
      assert.equal(snap.hasShadow[slot], 0, "disabled slot " + slot + " hasShadow is 0");
      assert.equal(snap.lightIndices[slot], -1, "disabled slot " + slot + " light index is -1");
      continue;
    }
    assert.ok(matrices && splits, "slot " + slot + " matrices/splits uploaded");
    assert.equal(splits.length, 4);
    assert.equal(splits[cascades - 1], 100, "last active split of slot " + slot + " reaches camera far");
    for (let c = 0; c < cascades; c++) {
      const textureID = snap.unitTextures[snap.samplerUnits[slot + "_" + c]];
      assert.ok(textureID != null, "slot " + slot + " cascade " + c + " texture bound at draw time");
      assert.ok(!seen.has(textureID), "slot " + slot + " cascade " + c + " texture is distinct");
      seen.add(textureID);
      assert.ok(gl.shadowDrawMatrices.has(textureID), "depth texture " + textureID + " rendered a shadow pass");
      assert.deepEqual(
        Array.from(matrices.slice(c * 16, c * 16 + 16)),
        Array.from(gl.shadowDrawMatrices.get(textureID)),
        "cascade matrix for " + slot + "_" + c + " matches its depth draw");
    }
  }
  return snap;
}

async function mountTwoCascadeShadowScene(maxUnits) {
  const TrackingContext = trackingContextClass(maxUnits);
  const mount = new FakeElement("div", null);
  mount.id = "scene-shadow-budget-root";
  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    createWebGL2Context: () => new TrackingContext(),
    manifest: {
      engines: [
        {
          id: "gosx-engine-shadow-budget",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-shadow-budget-root",
          props: {
            width: 320,
            height: 180,
            camera: { x: 0, y: 0, z: 6, near: 0.1, far: 100, fov: 72 },
            environment: { envMap: "/hdri/studio.png", envIntensity: 1 },
            scene: {
              lights: [
                {
                  id: "sun-a", kind: "directional", castShadow: true,
                  shadowCascades: 4, shadowSize: 256, shadowSoftness: 0.05,
                  directionX: 0.2, directionY: -1, directionZ: -0.35,
                },
                {
                  id: "sun-b", kind: "directional", castShadow: true,
                  shadowCascades: 4, shadowSize: 256, shadowSoftness: 0.05,
                  directionX: -0.3, directionY: -1, directionZ: 0.2,
                },
              ],
              objects: [JSON.parse(JSON.stringify(SCENE_TRIANGLE))],
            },
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = TrackingContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();
  const gl = mount.children[0].getContext("webgl2");
  return { mount, gl, env };
}

test("16-unit WebGL negotiates 4+1 cascades for two CSM lights with an environment map", async () => {
  const { mount, gl, env } = await mountTwoCascadeShadowScene(16);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");

  // Resource counts follow effective counts: 4 cascades + 1 cascade = 5
  // shadow framebuffers/depth attachments; both lights are retained.
  assert.equal(gl.ops.filter((op) => op[0] === "createFramebuffer").length, 5);
  assert.equal(gl.ops.filter((op) => op[0] === "framebufferTexture2D").length, 5);
  const lastInt = (name) => { const op = lastOp(gl, "uniform1i", name); return op ? op[2] : undefined; };
  assert.equal(lastInt("u_shadowCascades0"), 4);
  assert.equal(lastInt("u_shadowCascades1"), 1);
  assert.equal(lastInt("u_hasShadow0"), 1);
  assert.equal(lastInt("u_hasShadow1"), 1);
  assert.equal(lastInt("u_shadowLightIndex0"), 0);
  assert.equal(lastInt("u_shadowLightIndex1"), 1);

  // Units: slot 0 owns 8..11, slot 1 owns 12, IBL keeps 13..15.
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt("u_shadowMap0_" + c)), [8, 9, 10, 11]);
  assert.equal(lastInt("u_shadowMap1_0"), 12);
  assert.equal(lastInt("u_shadowMap1_1"), 12);
  assert.equal(lastInt("u_shadowMap1_2"), 12);
  assert.equal(lastInt("u_shadowMap1_3"), 12);
  assert.equal(lastInt("u_envMap"), 13);

  // Draw-time evidence: the actual PBR draw sampled five distinct, rendered
  // depth targets whose uploaded cascade matrices match the shadow passes.
  const snap = assertDrawTimeShadowEvidence(gl, 5);
  assert.deepEqual(snap.cascades, [4, 1]);
  assert.deepEqual(snap.hasShadow, [1, 1]);
  assert.deepEqual(snap.lightIndices, [0, 1]);

  assert.equal(env.consoleLogs.error.length, 0);
});

test("32-unit WebGL retains 4+4 cascades for two CSM lights with an environment map", async () => {
  const { mount, gl, env } = await mountTwoCascadeShadowScene(32);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(gl.ops.filter((op) => op[0] === "createFramebuffer").length, 8);
  assert.equal(gl.ops.filter((op) => op[0] === "framebufferTexture2D").length, 8);
  const lastInt = (name) => { const op = lastOp(gl, "uniform1i", name); return op ? op[2] : undefined; };
  assert.equal(lastInt("u_shadowCascades0"), 4);
  assert.equal(lastInt("u_shadowCascades1"), 4);
  assert.equal(lastInt("u_hasShadow0"), 1);
  assert.equal(lastInt("u_hasShadow1"), 1);
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt("u_shadowMap0_" + c)), [8, 9, 10, 11]);
  assert.deepEqual([0, 1, 2, 3].map((c) => lastInt("u_shadowMap1_" + c)), [12, 13, 14, 15]);
  assert.equal(lastInt("u_envMap"), 16);
  const snap = assertDrawTimeShadowEvidence(gl, 8);
  assert.deepEqual(snap.cascades, [4, 4]);
  assert.deepEqual(snap.hasShadow, [1, 1]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("32-unit WebGL renegotiates shadow budget across cap changes and light removal", async () => {
  const { mount, gl, env } = await mountTwoCascadeShadowScene(32);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const handle = env.context.__gosx.engines.get("gosx-engine-shadow-budget").handle;
  const stateLights = mount.__gosxScene3DState.lights;
  const liveDepthCount = () => new Set(gl.depthTargets.values()).size;
  const authoredCascades = () =>
    Array.from(stateLights.values())
      .filter((light) => light && light.castShadow)
      .map((light) => light.shadowCascades)
      .sort((a, b) => a - b);
  const renegotiate = async () => {
    await handle.applyCommands([]);
    await flushAsyncWork();
    await flushAsyncWork();
  };

  // Authored light state is only a request; budget negotiation never rewrites it.
  assert.deepEqual(authoredCascades(), [4, 4]);

  // Baseline: 32 units sustain 4+4 cascades (8 live depth resources).
  assert.equal(liveDepthCount(), 8);
  let prevSequence = gl.pbrDrawSnapshot.sequence;
  assertDrawTimeShadowEvidence(gl, 8);

  // Cap 32 -> 16: slot 1 drops to 1 cascade (8 -> 5 live depth resources).
  gl._maxUnits = 16;
  await renegotiate();
  assert.ok(gl.pbrDrawSnapshot.sequence > prevSequence, "new PBR draw after cap drop to 16");
  assert.equal(liveDepthCount(), 5);
  let snap = assertDrawTimeShadowEvidence(gl, 5);
  assert.deepEqual(snap.cascades, [4, 1]);
  assert.deepEqual(snap.hasShadow, [1, 1]);
  assert.deepEqual(snap.lightIndices, [0, 1]);

  // Cap 16 -> 8: neither light fits the shared budget (5 -> 0 live depths).
  prevSequence = gl.pbrDrawSnapshot.sequence;
  gl._maxUnits = 8;
  await renegotiate();
  assert.ok(gl.pbrDrawSnapshot.sequence > prevSequence, "new PBR draw after cap drop to 8");
  assert.equal(liveDepthCount(), 0);
  snap = assertDrawTimeShadowEvidence(gl, 0);
  assert.deepEqual(snap.hasShadow, [0, 0]);
  assert.deepEqual(snap.lightIndices, [-1, -1]);

  // Cap 8 -> 32: full 4+4 restoration (0 -> 8 live depth resources), with
  // matrix-to-depth and final split=100 checks on live slots.
  prevSequence = gl.pbrDrawSnapshot.sequence;
  gl._maxUnits = 32;
  await renegotiate();
  assert.ok(gl.pbrDrawSnapshot.sequence > prevSequence, "new PBR draw after cap restoration");
  assert.equal(liveDepthCount(), 8);
  snap = assertDrawTimeShadowEvidence(gl, 8);
  assert.deepEqual(snap.cascades, [4, 4]);
  assert.deepEqual(snap.hasShadow, [1, 1]);
  assert.deepEqual(snap.lightIndices, [0, 1]);

  // Restoration still did not mutate authored shadowCascades.
  assert.deepEqual(authoredCascades(), [4, 4]);

  // Removing sun-b via a scene command disposes only its slot: 4 live depths,
  // slot 1 disabled, slot 0 untouched.
  prevSequence = gl.pbrDrawSnapshot.sequence;
  await handle.applyCommands([{ kind: 1, objectId: "sun-b" }]);
  await flushAsyncWork();
  await flushAsyncWork();
  assert.ok(gl.pbrDrawSnapshot.sequence > prevSequence, "new PBR draw after light removal");
  assert.equal(liveDepthCount(), 4);
  snap = assertDrawTimeShadowEvidence(gl, 4);
  assert.deepEqual(snap.cascades, [4, 0]);
  assert.deepEqual(snap.hasShadow, [1, 0]);
  assert.deepEqual(snap.lightIndices, [0, -1]);
  assert.deepEqual(authoredCascades(), [4]);

  assert.equal(env.consoleLogs.error.length, 0);
});
