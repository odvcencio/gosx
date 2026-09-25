"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { FakeWebGLContext, FakeElement, createContext, runScript, bootstrapSource, flushAsyncWork } = require("./runtime-test-harness.js");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

function setup() {
  const source = readSceneRendererBackendSrc("webgl");
  const context = vm.createContext({ console, window: {} });
  const run = (code) => vm.runInContext(code, context);
  const slice = (text, start, end) => text.slice(text.indexOf(start), text.indexOf(end, text.indexOf(start)));
  run("function sceneNumber(v, d) { return Number.isFinite(Number(v)) ? Number(v) : d; } var sceneFiniteNumber = sceneNumber;");
  run(slice(fs.readFileSync(path.join(__dirname, "bootstrap-src/15a1-scene-texture-budget.ts"), "utf8"),
    "var SCENE_TEXTURE_UNIT_MATERIALS", "function sceneTextureMipBytes"));
  run(slice(source, "var sceneGLConstantCache", "function scenePBRHDRIBLAvailable"));
  run(slice(source, "function createSceneShadowSlot", "// Compute per-cascade light-space matrices"));
  run(slice(source, "var _scenePBRCascadeMatScratch", "var SCENE_IBL_BRDF_MODEL"));
  run("function scenePBRBindTexture(gl, unit, texture, target) {gl.activeTexture(gl.TEXTURE0 + unit);gl.bindTexture(target, texture);}");
  return context;
}

class ArrayGL extends FakeWebGLContext {
  constructor(maxUnits = 16) {
    super(); this.maxUnits = maxUnits; this.units = new Map(); this.activeUnit = 0;
  }
  getParameter(name) { return name === this.MAX_TEXTURE_IMAGE_UNITS ? this.maxUnits : super.getParameter(name); }
  activeTexture(unit) { this.activeUnit = unit - this.TEXTURE0; super.activeTexture(unit); }
  bindTexture(target, texture) { this.units.set(`${this.activeUnit}:${target}`, texture); super.bindTexture(target, texture); }
}
function uniforms(gl) {
  const result = {};
  for (let s = 0; s < 2; s++) for (const name of ["shadowMap", "hasShadow", "shadowBias", "shadowSoftness", "shadowLightIndex", "shadowCascades", "lightSpaceMatrices", "shadowCascadeSplits"]) {
    result[name + s] = gl.getUniformLocation({}, "u_" + name + s);
  }
  return result;
}
function slot(api, gl, n, seed) {
  const s = api.createSceneShadowSlot(gl, 256, n);
  s.cascades.forEach((c, i) => { c.lightMatrix = Array(16).fill(seed + i); c.splitFar = (i + 1) * 100 / n; });
  return s;
}
function last(gl, op, name) { return gl.ops.filter(v => v[0] === op && v[1] === name).at(-1); }

test("eight cascades share two depth arrays and fit IBL on the minimum WebGL2 device", () => {
  const api = setup(), gl = new ArrayGL();
  const slots = [slot(api, gl, 4, 100), slot(api, gl, 4, 200)];
  assert.equal(gl.ops.filter(v => v[0] === "texImage3D").length, 2);
  const attachments = gl.ops.filter(v => v[0] === "framebufferTextureLayer");
  assert.equal(attachments.length, 8);
  assert.deepEqual(attachments.map(v => v[5]), [0, 1, 2, 3, 0, 1, 2, 3]);
  assert.equal(new Set(attachments.map(v => v[3])).size, 2);
  const layout = api.scenePBRTextureLayoutForFrame(slots, [0, 1], { ibl: {} }, 16);
  assert.deepEqual(Array.from(layout.shadows), [8, 9]);
  assert.deepEqual({ ...layout.ibl }, { irradiance: 10, radiance: 11, brdfLUT: 12 });
  assert.equal(layout.warnings.length, 0);
  for (const max of [16, 32]) for (const env of [null, { envMap: "sky.hdr" }, { ibl: {} }]) {
    assert.deepEqual(Array.from(api.scenePBRNegotiateShadowCascades([4, 4], env, max)), [4, 4]);
  }
  api.scenePBRUploadShadowUniforms(gl, uniforms(gl), slots, [0, 1], [{}, {}], { ibl: {} }, new Map());
  assert.equal(gl.units.get(`8:${gl.TEXTURE_2D_ARRAY}`), slots[0].depthTexture);
  assert.equal(gl.units.get(`9:${gl.TEXTURE_2D_ARRAY}`), slots[1].depthTexture);
  assert.equal(last(gl, "uniform1i", "u_shadowMap0")[2], 8);
  assert.equal(last(gl, "uniform1i", "u_shadowMap1")[2], 9);
  assert.equal(last(gl, "uniform1i", "u_shadowCascades0")[2], 4);
  assert.equal(last(gl, "uniform1i", "u_shadowCascades1")[2], 4);
});

test("unshadowed scenes bind complete array placeholders away from 2D and cube samplers", () => {
  const api = setup(), gl = new ArrayGL(), cache = new Map(), u = uniforms(gl);
  api.scenePBRUploadShadowUniforms(gl, u, [null, null], [-1, -1], [], {}, cache);
  const placeholder = gl.units.get(`8:${gl.TEXTURE_2D_ARRAY}`);
  assert.ok(placeholder);
  assert.equal(gl.units.get(`9:${gl.TEXTURE_2D_ARRAY}`), placeholder);
  assert.equal(cache.size, 1);
  assert.equal(last(gl, "uniform1i", "u_hasShadow0")[2], 0);
  assert.equal(last(gl, "uniform1i", "u_shadowLightIndex1")[2], -1);
  api.scenePBRUploadShadowUniforms(gl, u, [null, null], [-1, -1], [], {}, cache);
  assert.equal(gl.ops.filter(v => v[0] === "texImage3D").length, 1);
});

test("removed lights cannot retain enabled shadows or overwrite the other light", () => {
  const api = setup(), gl = new ArrayGL(), cache = new Map(), u = uniforms(gl);
  const slots = [slot(api, gl, 4, 100), slot(api, gl, 1, 200)];
  api.scenePBRUploadShadowUniforms(gl, u, slots, [0, 1], [{}, {}], {}, cache);
  api.scenePBRUploadShadowUniforms(gl, u, [slots[0], null], [0, -1], [{}], {}, cache);
  assert.equal(last(gl, "uniform1i", "u_hasShadow0")[2], 1);
  assert.equal(last(gl, "uniform1i", "u_hasShadow1")[2], 0);
  assert.equal(last(gl, "uniform1i", "u_shadowCascades1")[2], 0);
  assert.equal(gl.units.get(`8:${gl.TEXTURE_2D_ARRAY}`), slots[0].depthTexture);
});

test("a depth array is freed once when its cascade count or size changes", () => {
  const api = setup(), gl = new ArrayGL();
  const old = slot(api, gl, 4, 100);
  api.disposeShadowSlot(gl, old);
  assert.equal(gl.ops.filter(v => v[0] === "deleteTexture").length, 1);
  assert.equal(gl.ops.filter(v => v[0] === "deleteFramebuffer").length, 4);
  const next = api.createSceneShadowSlot(gl, 512, 2);
  assert.notEqual(next.depthTexture, old.depthTexture);
  assert.equal(next.cascades.length, 2);
  assert.equal(next.size, 512);
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
      if (target === this.TEXTURE_2D_ARRAY && this._activeUnit >= 0) {
        this.unitTextures.set(this._activeUnit, texture && texture.id);
      }
      super.bindTexture(target, texture);
    }
    bindFramebuffer(target, framebuffer) {
      if (target === this.FRAMEBUFFER) this._currentFramebuffer = framebuffer || null;
      super.bindFramebuffer(target, framebuffer);
    }
    framebufferTextureLayer(target, attachment, texture, level, layer) {
      if (attachment === this.DEPTH_ATTACHMENT && this._currentFramebuffer) {
        this.depthTargets.set(this._currentFramebuffer.id, texture.id + ":" + layer);
      }
      super.framebufferTextureLayer(target, attachment, texture, level, layer);
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
            const unit = values["u_shadowMap" + slot];
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
        if (texId.startsWith(texture.id + ":")) this.depthTargets.delete(fbId);
      }
      for (const id of this.shadowDrawMatrices.keys()) if (id.startsWith(texture.id + ":")) this.shadowDrawMatrices.delete(id);
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
      const textureID = snap.unitTextures[snap.samplerUnits[slot + "_" + c]] + ":" + c;
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

for (const units of [16, 32]) test(units + "-unit WebGL renders all eight cascades with an environment map", async () => {
  const { mount, gl, env } = await mountTwoCascadeShadowScene(units);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(gl.ops.filter(op => op[0] === "framebufferTextureLayer").length, 8);
  assert.equal(gl.ops.filter(op => op[0] === "texImage3D").length, 3, "two shadow arrays and one placeholder");
  assert.equal(last(gl, "uniform1i", "u_shadowMap0")[2], 8);
  assert.equal(last(gl, "uniform1i", "u_shadowMap1")[2], 9);
  assert.equal(last(gl, "uniform1i", "u_envMap")[2], 12);
  const snap = assertDrawTimeShadowEvidence(gl, 8);
  assert.deepEqual(snap.cascades, [4, 4]);
  assert.deepEqual(snap.hasShadow, [1, 1]);
  assert.deepEqual(snap.lightIndices, [0, 1]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("spot shadow shares the cascade budget without invalid lights consuming authored-order slots", async () => {
  const TrackingContext = trackingContextClass(16);
  const mount = new FakeElement("div", null);
  mount.id = "scene-spot-shadow-root";
  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    createWebGL2Context: () => new TrackingContext(),
    manifest: {
      engines: [{
        id: "gosx-engine-spot-shadow",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: "scene-spot-shadow-root",
        props: {
          width: 320,
          height: 180,
          camera: { x: 0, y: 0, z: 6, near: 0.1, far: 100, fov: 72 },
          environment: { envMap: "/hdri/studio.png", envIntensity: 1 },
          scene: {
            lights: [
              { id: "wide-a", kind: "spot", castShadow: true, x: -1, y: 3, z: 1,
                directionX: 0, directionY: -1, directionZ: -0.2, angle: 1.8, shadowSize: 256 },
              { id: "wide-b", kind: "spot", castShadow: true, x: 1, y: 3, z: 1,
                directionX: 0, directionY: -1, directionZ: -0.2, angle: 2.2, shadowSize: 256 },
              { id: "spot", kind: "spot", castShadow: true, x: 0, y: 3, z: 1,
                directionX: 0, directionY: -1, directionZ: -0.2, angle: 0.5,
                range: 8, shadowSize: 256, shadowBias: 0.005 },
              { id: "sun", kind: "directional", castShadow: true,
                directionX: 0.2, directionY: -1, directionZ: -0.35,
                shadowCascades: 4, shadowSize: 256, shadowSoftness: 0.05 },
            ],
            objects: [JSON.parse(JSON.stringify(SCENE_TRIANGLE))],
          },
        },
      }],
    },
  });
  env.context.WebGL2RenderingContext = TrackingContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl2");
  let snap = assertDrawTimeShadowEvidence(gl, 5);
  assert.deepEqual(snap.lightIndices, [2, 3]);
  assert.deepEqual(snap.cascades, [1, 4]);
  assert.deepEqual(snap.hasShadow, [1, 1]);

  const handle = env.context.__gosx.engines.get("gosx-engine-spot-shadow").handle;
  await handle.applyCommands([{ kind: 1, objectId: "spot" }]);
  await flushAsyncWork();
  await flushAsyncWork();
  snap = assertDrawTimeShadowEvidence(gl, 4);
  assert.deepEqual(snap.lightIndices, [2, -1]);
  assert.deepEqual(snap.cascades, [4, 0]);

  await handle.applyCommands([{ kind: 1, objectId: "sun" }]);
  await flushAsyncWork();
  await flushAsyncWork();
  snap = assertDrawTimeShadowEvidence(gl, 0);
  assert.deepEqual(snap.hasShadow, [0, 0]);
  assert.deepEqual(snap.lightIndices, [-1, -1]);

  env.context.__gosx_dispose_engine("gosx-engine-spot-shadow");
  assert.equal(gl.depthTargets.size, 0, "explicit disposal releases every shadow framebuffer/texture pair");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("32-unit WebGL keeps its shadow budget and releases removed lights", async () => {
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
  // Authored light state is only a request; budget negotiation never rewrites it.
  assert.deepEqual(authoredCascades(), [4, 4]);

  // Baseline: 32 units sustain 4+4 cascades (8 live depth resources).
  assert.equal(liveDepthCount(), 8);
  let prevSequence = gl.pbrDrawSnapshot.sequence;
  assertDrawTimeShadowEvidence(gl, 8);

  let snap = assertDrawTimeShadowEvidence(gl, 8);
  assert.deepEqual(snap.cascades, [4, 4]);
  assert.deepEqual(snap.hasShadow, [1, 1]);
  assert.deepEqual(snap.lightIndices, [0, 1]);

  // Rendering does not mutate authored shadowCascades.
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
