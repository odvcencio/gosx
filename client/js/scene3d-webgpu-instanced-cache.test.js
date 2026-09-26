"use strict";

// Instanced meshes reach the WebGPU renderer as a fresh shallow copy every
// frame (appendSceneInstancedMeshesToBundle). Their GPU caches must survive
// that copy: no buffer or bind group per frame, and a static scene must
// replay its render bundle. These tests build a NEW bundle per frame the way
// mount.ts does; reusing one bundle object would hide the defect.

const test = require("node:test");
const assert = require("node:assert/strict");

const { createBoardWebGPUHarness, flushAsyncWork } = require("./runtime-test-harness.js");

function instancedSceneState(api, id, count) {
  const transforms = [];
  for (let i = 0; i < count; i += 1) {
    transforms.push(1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, (i % 20) - 10, 0, -Math.floor(i / 20), 1);
  }
  return api.createSceneState({
    scene: {
      instancedMeshes: [{ id, count, kind: "box", width: 0.2, height: 0.2, depth: 0.2, transforms, color: "#44aa44" }],
    },
  }, { tier: "full" });
}

function frameBundle(api, instancedMeshes) {
  return api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 2, z: 12, fov: 60, near: 0.05, far: 200 },
    [], [], [], [], [], {}, 0, [], instancedMeshes, [], [], [], 0, false,
  );
}

async function renderFrames(harness, api, state, frames, startFrame) {
  const samples = [];
  for (let frame = 0; frame < frames; frame += 1) {
    const meshes = state ? api.sceneStateInstancedMeshesWithMaterials(state) : [];
    harness.renderer.render(frameBundle(api, meshes), { width: 64, height: 64 }, { nowMS: (startFrame + frame) * 16, active: true });
    await flushAsyncWork();
    samples.push({
      buffers: harness.fake.state.buffers.length,
      bindGroups: harness.fake.state.bindGroups.length,
      bundle: harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"),
    });
  }
  return samples;
}

for (const count of [300, 8]) {
  test(`instanced cache: per-frame bundle copies reuse GPU state (count ${count})`, async () => {
    const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: { renderBundles: true } });
    const api = harness.env.context.__gosx_scene3d_api;
    harness.canvas.width = 64;
    harness.canvas.height = 64;
    const samples = await renderFrames(harness, api, instancedSceneState(api, "grass", count), 6, 0);
    const detail = JSON.stringify(samples);
    assert.equal(samples[5].buffers, samples[2].buffers, "a GPU buffer was created per frame: " + detail);
    assert.equal(samples[5].bindGroups, samples[2].bindGroups, "a bind group was created per frame: " + detail);
    assert.equal(samples[5].bundle, "replayed", "a static instanced scene must replay its render bundle: " + detail);
  });
}

test("instanced cache: owners of meshes gone for 120 frames are released", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: { renderBundles: true } });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  await renderFrames(harness, api, instancedSceneState(api, "rocks", 8), 3, 0);
  const destroyedBefore = harness.fake.state.buffers.filter((buffer) => buffer.destroyed).length;
  await renderFrames(harness, api, null, 122, 3);
  const destroyedAfter = harness.fake.state.buffers.filter((buffer) => buffer.destroyed).length;
  // The mesh owned a transform, a colour and a material uniform buffer.
  assert.ok(destroyedAfter - destroyedBefore >= 3, `expected the three owner buffers to be destroyed, got ${destroyedAfter - destroyedBefore}`);
});
