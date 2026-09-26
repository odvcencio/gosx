"use strict";

// GPU-driven instancing (the GPU-driven instancing spec): the embedded Elio
// kernels, the pure helpers in indirect-instancing.ts, and the renderer seam in
// the WebGPU renderer driven through the fake WebGPU device. Every renderer test builds
// a NEW bundle per frame, the way mount.ts does.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const { createBoardWebGPUHarness, flushAsyncWork } = require("./runtime-test-harness.js");

function golden(name) {
  return fs.readFileSync(path.join(__dirname, "testdata", "elio", name), "utf8").replace(/\n$/, "");
}

async function apiOnly() {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  return harness.env.context.__gosx_scene3d_api;
}

test("gpu-driven: embedded kernels equal the Elio goldens", async () => {
  const api = await apiOnly();
  assert.equal(api.SCENE_GPU_DRIVEN_CULL_WGSL, golden("gpudriven_cull.wgsl"));
  assert.equal(api.SCENE_GPU_DRIVEN_HIZ_DOWNSAMPLE_WGSL, golden("gpudriven_hiz_downsample.wgsl"));
  assert.match(api.SCENE_GPU_DRIVEN_HIZ_SEED_WGSL, /var gdDepth: texture_depth_2d;/);
  assert.match(api.SCENE_GPU_DRIVEN_HIZ_SEED_MS_WGSL, /var gdDepth: texture_depth_multisampled_2d;/);
});

test("gpu-driven: pure helpers match appendix B", async () => {
  const api = await apiOnly();
  const hiz = api.sceneGPUDrivenHiZLevels(320, 240);
  assert.equal(hiz.levels.length, 9);
  assert.equal(hiz.total, 25609);
  assert.deepEqual(JSON.parse(JSON.stringify(hiz.levels[4])), { offset: 25500, width: 10, height: 8 });
  assert.equal(api.sceneGPUDrivenHiZLevels(1, 1).levels.length, 1);
  assert.equal(api.sceneGPUDrivenMaxInstances({}), 1398101);
  assert.equal(api.sceneGPUDrivenMaxInstances({ maxStorageBufferBindingSize: 1 << 30 }), 4194240);
  assert.equal(api.sceneGPUDrivenConfig(null), null);
  assert.deepEqual(JSON.parse(JSON.stringify(api.sceneGPUDrivenConfig({}))), { occlusion: false, shadowCulling: true });
  assert.equal(api.sceneGPUDrivenMeshEligible({ id: "a", transforms: new Array(32) }, 2), true);
  assert.equal(api.sceneGPUDrivenMeshEligible({ id: "a", transforms: new Array(16) }, 2), false);
  assert.equal(api.sceneGPUDrivenMeshEligible({ id: "a", cullKernelWGSL: "fn k() {}", transforms: new Array(16) }, 1), false);
  assert.equal(api.sceneGPUDrivenMeshEligible({ transforms: new Array(16) }, 1), false);

  // Identity view-projection: planes are x = ±1, y = ±1, z = 0 and z = 1.
  const planes = api.sceneGPUDrivenFrustumPlanes([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], new Array(24), 0);
  assert.deepEqual(planes, [1, 0, 0, 1, -1, 0, 0, 1, 0, 1, 0, 1, 0, -1, 0, 1, 0, 0, 1, 0, 0, 0, -1, 1]);

  const buffer = new ArrayBuffer(512);
  const f32 = new Float32Array(buffer);
  const u32 = new Uint32Array(buffer);
  api.sceneGPUDrivenPackView(f32, u32, 0, {
    viewProj: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], width: 320, height: 240,
    eye: { x: 1, y: 2, z: 3 }, total: 7, slot: 1, phase: 1, levels: hiz.levels,
  });
  assert.deepEqual(Array.from(f32.slice(40, 48)), [320, 240, Math.fround(1 / 320), Math.fround(1 / 240), 1, 2, 3, 0]);
  assert.deepEqual(Array.from(u32.slice(52, 56)), [7, 1, 1, 9]);
  assert.deepEqual(Array.from(u32.slice(56 + 8 * 4, 56 + 9 * 4)), [25608, 1, 1, 0]);
  assert.deepEqual(Array.from(u32.slice(56 + 9 * 4, 56 + 10 * 4)), [0, 0, 0, 0]);
});
