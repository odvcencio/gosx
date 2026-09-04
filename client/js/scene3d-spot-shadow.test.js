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

// Match the renderer's shader-visible f32 inputs and left-associated
// multiply/add chain. Matrix entries are already f32, while vertex attributes
// and every arithmetic result are rounded explicitly here.
function projectShaderF32(matrix, point) {
  const x = Math.fround(point[0]);
  const y = Math.fround(point[1]);
  const z = Math.fround(point[2]);
  function component(row) {
    let value = Math.fround(Math.fround(matrix[row]) * x);
    value = Math.fround(value + Math.fround(Math.fround(matrix[4 + row]) * y));
    value = Math.fround(value + Math.fround(Math.fround(matrix[8 + row]) * z));
    return Math.fround(value + Math.fround(matrix[12 + row]));
  }
  return [component(0), component(1), component(2), component(3)];
}

function webGPUShadowMatrix(context, raw) {
  if (typeof context.sceneWebGPUShadowDepthMatrix !== "function") {
    const webgpu = read(path.join(runtimeRoot, "webgpu.ts"));
    vm.runInContext(between(webgpu, "function sceneWebGPUShadowDepthMatrix", "function createSceneWebGPURenderer"), context);
  }
  context.raw = raw;
  return vm.runInContext("sceneWebGPUShadowDepthMatrix(raw)", context);
}

function deterministicRandom(seed) {
  let state = seed >>> 0;
  return function next() {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    return state / 0x100000000;
  };
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

test("spot far padding conservatively contains 10k shader-f32 boundary corners", () => {
  const context = matrixContext();
  const random = deterministicRandom(0x0552296);
  for (let sample = 0; sample < 10000; sample++) {
    const scale = Math.pow(10, random() * 4 - 2);
    const cx = (random() - 0.5) * 64 * scale;
    const cy = (random() - 0.5) * 64 * scale;
    const cz = (random() - 0.5) * 64 * scale;
    const ex = (0.25 + random() * 4) * scale;
    const ey = (0.25 + random() * 4) * scale;
    const ez = (0.25 + random() * 4) * scale;
    const sampleBounds = {
      minX: cx - ex, maxX: cx + ex,
      minY: cy - ey, maxY: cy + ey,
      minZ: cz - ez, maxZ: cz + ez,
    };
    const px = cx + (random() - 0.5) * 12 * scale;
    const py = cy + (random() - 0.5) * 12 * scale;
    const pz = cz + (random() - 0.5) * 12 * scale;
    let dx = cx - px + (random() - 0.5) * 0.25 * scale;
    let dy = cy - py + (random() - 0.5) * 0.25 * scale;
    let dz = cz - pz + (random() - 0.5) * 0.25 * scale;
    const directionLength = Math.hypot(dx, dy, dz);
    dx /= directionLength;
    dy /= directionLength;
    dz /= directionLength;

    let furthestDepth = -Infinity;
    let furthestCorner = null;
    for (let corner = 0; corner < 8; corner++) {
      const point = [
        (corner & 1) ? sampleBounds.maxX : sampleBounds.minX,
        (corner & 2) ? sampleBounds.maxY : sampleBounds.minY,
        (corner & 4) ? sampleBounds.maxZ : sampleBounds.minZ,
      ];
      const depth = (point[0] - px) * dx + (point[1] - py) * dy + (point[2] - pz) * dz;
      if (depth > furthestDepth) {
        furthestDepth = depth;
        furthestCorner = point;
      }
    }

    const raw = build(context, {
      kind: "spot", x: px, y: py, z: pz,
      directionX: dx, directionY: dy, directionZ: dz,
      angle: 0.05 + random() * 1.45, range: 0, castShadow: true,
    }, sampleBounds);
    assert.ok(raw, "sample " + sample + " remains numerically projectable");
    const converted = webGPUShadowMatrix(context, raw);
    for (const [backend, matrix] of [["WebGL", raw], ["WebGPU", converted]]) {
      const clip = projectShaderF32(matrix, furthestCorner);
      assert.ok(Number.isFinite(clip[2]) && Number.isFinite(clip[3]) && clip[3] > 0,
        backend + " sample " + sample + " has finite positive clip w");
      assert.ok(clip[2] / clip[3] <= 1,
        backend + " sample " + sample + " boundary escaped clip Z: " + clip[2] / clip[3]);
    }
  }
});

test("authored range boundary stays inside bounded f32 projection padding", () => {
  const context = matrixContext();
  const raw = build(context, Object.assign({}, spot, { range: 4 }), bounds);
  assert.ok(raw);
  const converted = webGPUShadowMatrix(context, raw);
  const boundary = [0, -1, 0];
  const outside = [0, -5, 0];
  for (const [backend, matrix] of [["WebGL", raw], ["WebGPU", converted]]) {
    const atRange = projectShaderF32(matrix, boundary);
    assert.ok(atRange[2] / atRange[3] <= 1,
      backend + " authored range boundary remains covered");
    const twiceRange = projectShaderF32(matrix, outside);
    assert.ok(twiceRange[2] / twiceRange[3] > 1,
      backend + " padding remains bounded rather than doubling the authored range");
  }
});

test("huge transverse bounds fail closed before shader-f32 cancellation", () => {
  const diagonal = Math.SQRT1_2;
  const axisDepth = 10;
  const transverse = 9e8;
  const x = diagonal * (axisDepth + transverse);
  const y = diagonal * (axisDepth - transverse);
  const projectedDepth = x * diagonal + y * diagonal;
  const projectedTransverse = x * diagonal - y * diagonal;
  assert.ok(Math.abs(projectedDepth - axisDepth) < 1e-6);
  assert.ok(Math.abs(projectedTransverse - transverse) < 1e-6);
  assert.ok(Math.atan2(projectedTransverse, projectedDepth) < Math.PI / 2 - 1e-8,
    "the double-precision point is inside the authored cone");
  const degenerateBounds = { minX: x, maxX: x, minY: y, maxY: y, minZ: 0, maxZ: 0 };
  const matrix = build(matrixContext(), {
    kind: "spot", x: 0, y: 0, z: 0,
    directionX: diagonal, directionY: diagonal, directionZ: 0,
    angle: Math.PI / 2 - 1e-8, range: 0, castShadow: true,
  }, degenerateBounds);
  assert.equal(matrix, null,
    "vertex coordinate magnitude makes the bounded f32 guard fail closed");
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
  const converted = webGPUShadowMatrix(context, raw);
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
