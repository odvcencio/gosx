"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const {
  createBoardWebGPUHarness,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const sourceRoot = path.join(__dirname, "bootstrap-src");
const runtimeRoot = path.join(__dirname, "..", "runtime", "scene3d");

function read(file) {
  return fs.readFileSync(file, "utf8");
}

function between(source, startMarker, endMarker) {
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(start >= 0 && end > start, "source markers exist: " + startMarker + " -> " + endMarker);
  return source.slice(start, end);
}

function matrixContext() {
  const context = vm.createContext({ console, Math, Number, isFinite, Float32Array });
  for (const name of ["10-runtime-scene-utils.ts", "11-scene-math.ts"]) {
    vm.runInContext(read(path.join(sourceRoot, name)), context, { filename: name });
  }
  const shared = read(path.join(sourceRoot, "16c-scene-shared-pbr.ts"));
  vm.runInContext(between(shared, "function sceneSpotShadowLightSpaceMatrix", "// Compute the AABB of all objects in the bundle."), context);
  return context;
}

function build(context, light, bounds) {
  context.light = light;
  context.bounds = bounds;
  return vm.runInContext("sceneShadowLightSpaceMatrix(light, bounds)", context);
}

function project(matrix, point) {
  const [x, y, z] = point;
  return [
    matrix[0] * x + matrix[4] * y + matrix[8] * z + matrix[12],
    matrix[1] * x + matrix[5] * y + matrix[9] * z + matrix[13],
    matrix[2] * x + matrix[6] * y + matrix[10] * z + matrix[14],
    matrix[3] * x + matrix[7] * y + matrix[11] * z + matrix[15],
  ];
}

const bounds = { minX: -1, minY: -5, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 };
const spot = {
  kind: "spot", x: 0, y: 3, z: 0,
  directionX: 0, directionY: -1, directionZ: 0,
  angle: Math.PI / 6, range: 0, castShadow: true,
};

test("spot shadow perspective maps near/far depth and preserves positive forward w", () => {
  const matrix = build(matrixContext(), spot, bounds);
  assert.ok(matrix);
  const near = project(matrix, [0, 2.99, 0]);
  const far = project(matrix, [0, -5, 0]);
  const middle = project(matrix, [0, 1, 0]);
  assert.ok(Math.abs(near[2] / near[3] + 1) < 1e-4);
  assert.ok(Math.abs(far[2] / far[3] - 1) < 1e-4);
  assert.ok(Math.abs(middle[3] - 2) < 1e-4);
  assert.equal(project(matrix, [0, 3, 0])[3], 0);
  assert.ok(project(matrix, [0, 4, 0])[3] < 0);
});

test("spot shadow projection is stable and fail-closed at the one-map boundary", () => {
  const context = matrixContext();
  for (const patch of [
    { angle: Math.PI / 2 },
    { angle: Math.PI },
    { directionX: 0, directionY: 0, directionZ: 0 },
    { directionX: Infinity },
    { x: NaN },
  ]) {
    assert.equal(build(context, Object.assign({}, spot, patch), bounds), null);
  }
  const huge = build(context, Object.assign({}, spot, { directionY: -1e200 }), bounds);
  const tiny = build(context, Object.assign({}, spot, { directionY: -1e-200 }), bounds);
  assert.deepEqual(Array.from(huge), Array.from(tiny));
  const ranged = build(context, Object.assign({}, spot, { range: 4 }), bounds);
  const farAtRange = project(ranged, [0, -1, 0]);
  assert.ok(Math.abs(farAtRange[2] / farAtRange[3] - 1) < 1e-4);
});

test("WebGL and WebGPU receivers reject invalid perspective coordinates before sampling", () => {
  const webgl = read(path.join(runtimeRoot, "webgl.ts"));
  const webgpu = read(path.join(runtimeRoot, "webgpu.ts"));
  const glGuard = webgl.indexOf('"    if (lightSpacePos.w <= 0.0) return 1.0;"');
  const glSample = webgl.indexOf('"        float depth = texture(shadowMap, projCoords.xy).r;"');
  assert.ok(glGuard >= 0 && glGuard < glSample);
  assert.match(webgl, /u_receiveShadow && \(lightType == 1 \|\| lightType == 3\)/);

  const gpuGuard = webgpu.indexOf('"    if (lightSpacePos.w <= 0.0) { return 1.0; }"');
  const gpuSample = webgpu.indexOf("textureSampleCompareLevel(shadowMap0");
  assert.ok(gpuGuard >= 0 && gpuGuard < gpuSample);
  assert.match(webgpu, /lightType == 1u \|\| lightType == 3u/);
});

test("WebGPU shadow depth conversion maps GL near/far to 0/1 without mutating input", () => {
  const context = matrixContext();
  const raw = build(context, spot, bounds);
  const webgpu = read(path.join(runtimeRoot, "webgpu.ts"));
  vm.runInContext(between(webgpu, "function sceneWebGPUShadowDepthMatrix", "function createSceneWebGPURenderer"), context);
  context.raw = raw;
  const converted = vm.runInContext("sceneWebGPUShadowDepthMatrix(raw)", context);
  assert.notEqual(converted, raw);
  const near = project(converted, [0, 2.99, 0]);
  const far = project(converted, [0, -5, 0]);
  assert.ok(Math.abs(near[2] / near[3]) < 1e-4);
  assert.ok(Math.abs(far[2] / far[3] - 1) < 1e-4);
  assert.ok(Math.abs(project(raw, [0, 2.99, 0])[2] / project(raw, [0, 2.99, 0])[3] + 1) < 1e-4);
});

test("alpha-masked materials have a documented shared closed-silhouette caster limit", () => {
  const webgl = read(path.join(runtimeRoot, "webgl.ts"));
  const webgpu = read(path.join(runtimeRoot, "webgpu.ts"));
  const glShadow = between(webgl, "const SCENE_SHADOW_VERTEX_SOURCE", "// Create a framebuffer with a depth-only texture");
  const gpuShadow = between(webgpu, "var WGSL_SHADOW_VERTEX", "var WGSL_SCENE_COLOR_FRAGMENT");
  for (const [backend, source] of [["WebGL", glShadow], ["WebGPU", gpuShadow]]) {
    assert.doesNotMatch(source, /alphaCutoff|baseColorMap|albedoMap|textureSample|texture\(/,
      backend + " shadow caster path must stay explicitly depth/geometry-only until cutout support is added");
  }
});

function spotShadowBundle(api, lights) {
  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#ffffff", roughness: 1 }],
      objects: [{ id: "caster", kind: "box", width: 1, height: 1, depth: 1,
        material: "m", castShadow: true, receiveShadow: true }],
      lights,
    },
  }, { tier: "full" });
  return api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], api.sceneStateLights(state), {},
    0, [], [], [], [], [], 0, false,
  );
}

test("WebGPU spot shadows render in deterministic slots and retire on removal and device loss", async () => {
  let resolveLost;
  const lost = new Promise((resolve) => { resolveLost = resolve; });
  const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: { lost } });
  const api = harness.env.context.__gosx_scene3d_api;
  const liveState = api.createSceneState({ scene: { lights: [{
    id: "live-spot", kind: "spot", x: 0, y: 3, z: 1,
    directionX: 0, directionY: -1, directionZ: 0,
    angle: 0.4, range: 6, castShadow: false,
  }] } }, { tier: "full" });
  const initialLiveHash = api.sceneStateLights(liveState)[0]._lightHash;
  api.applySceneCommands(liveState, [{
    kind: 4,
    objectId: "live-spot",
    data: {
      directionZ: -0.25, angle: 0.55, range: 9, castShadow: true,
      shadowBias: -0.002, shadowSize: 512, shadowCascades: 1, shadowSoftness: 1.5,
    },
  }]);
  const liveSpot = api.sceneStateLights(liveState)[0];
  assert.deepEqual(
    [liveSpot.directionZ, liveSpot.angle, liveSpot.range, liveSpot.castShadow,
      liveSpot.shadowBias, liveSpot.shadowSize, liveSpot.shadowCascades, liveSpot.shadowSoftness],
    [-0.25, 0.55, 9, true, -0.002, 512, 1, 1.5],
  );
  assert.notEqual(liveSpot._lightHash, initialLiveHash, "live spot projection patch restamps renderer identity");
  const lights = [
    { id: "wide-a", kind: "spot", castShadow: true, x: -1, y: 3, z: 1,
      directionX: 0, directionY: -1, directionZ: 0, angle: 1.8, shadowSize: 256 },
    { id: "wide-b", kind: "spot", castShadow: true, x: 1, y: 3, z: 1,
      directionX: 0, directionY: -1, directionZ: 0, angle: 2.2, shadowSize: 256 },
    { id: "spot", kind: "spot", castShadow: true, x: 0, y: 3, z: 1,
      directionX: 0, directionY: -1, directionZ: -0.2, angle: 0.5, range: 8, shadowSize: 256 },
    { id: "sun", kind: "directional", castShadow: true,
      directionX: 0.2, directionY: -1, directionZ: -0.35, shadowSize: 256 },
  ];
  const passStart = harness.fake.state.renderPasses.length;
  harness.renderer.render(spotShadowBundle(api, lights), { width: 64, height: 64 }, { nowMS: 0, active: true });
  const shadowPasses = harness.fake.state.renderPasses.slice(passStart).filter(
    (pass) => pass.descriptor && Array.isArray(pass.descriptor.colorAttachments) && pass.descriptor.colorAttachments.length === 0,
  );
  assert.equal(shadowPasses.length, 2);
  const shadowTextureIDs = shadowPasses.map((pass) => pass.descriptor.depthStencilAttachment.view.textureId);
  assert.equal(new Set(shadowTextureIDs).size, 2);

  const uniformWrite = harness.fake.state.writeBufferCalls.filter(
    (call) => call.data && call.data.length === 40,
  ).at(-1);
  assert.ok(uniformWrite);
  const words = new Int32Array(uniformWrite.data.buffer, uniformWrite.data.byteOffset, uniformWrite.data.length);
  assert.deepEqual([words[36], words[37]], [2, 3]);

  harness.renderer.render(spotShadowBundle(api, []), { width: 64, height: 64 }, { nowMS: 17, active: true });
  for (const id of shadowTextureIDs) {
    assert.equal(harness.fake.state.textures.find((texture) => texture.id === id).destroyed, true,
      "removed light retires shadow texture " + id);
  }

  const beforeRecreate = harness.fake.state.textures.length;
  harness.renderer.render(spotShadowBundle(api, [lights[2]]), { width: 64, height: 64 }, { nowMS: 34, active: true });
  const recreated = harness.fake.state.textures.slice(beforeRecreate).filter(
    (texture) => texture.desc && texture.desc.format === "depth24plus" && texture.desc.size[0] === 256,
  );
  assert.equal(recreated.length, 1);
  assert.notEqual(recreated[0].destroyed, true);

  resolveLost({ reason: "destroyed", message: "spot-shadow test loss" });
  await flushAsyncWork();
  await flushAsyncWork();
  assert.equal(recreated[0].destroyed, true, "device loss shares renderer disposal and releases spot shadow texture");
});
