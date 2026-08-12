"use strict";
// GPU and CPU instance culling, render bundles, indirect draws, GPU pass
// timing and the Scene3D telemetry accessor.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const {
  createContext,
  flushAsyncWork,
  makeFakeGPUDevice,
  readSceneMountSrc,
  createBoardWebGPUHarness,
  makeFakeGPUDeviceForCompute,
  MINIMAL_CULL_WGSL,
  createCullSystemHarness,
  bundleMeshScene,
  bundleBoxes,
  bundleInstancedMesh,
  loadCullFunctions,
} = require("./runtime-test-harness.js");

// -------------------------------------------------------------------------
// Task 1: source-string — new layout constant + pipeline accessor exist in 16a
// -------------------------------------------------------------------------
test("gpu-cull T1: WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT and cull pipeline accessor exist in 16a source", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // Layout constant: 80-byte stride, locations 4-8, uint32x4 at loc 8.
  assert.match(webgpu, /var WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT/,
    "WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT constant must exist");
  assert.match(webgpu, /arrayStride:\s*80[\s\S]{0,200}stepMode:\s*"instance"/,
    "cull layout must have 80-byte stride and instance stepMode");
  assert.match(webgpu, /format:\s*"uint32x4"[\s\S]{0,60}shaderLocation:\s*8/,
    "cull layout must use uint32x4 at shaderLocation 8 (pickData)");

  // Cull vertex shader variant: pickData vec4u at location 8.
  assert.match(webgpu, /var WGSL_PBR_INSTANCED_CULL_VERTEX/,
    "WGSL_PBR_INSTANCED_CULL_VERTEX shader must exist");
  assert.match(webgpu, /pickData:\s*vec4u/,
    "cull vertex shader must declare pickData: vec4u at location 8");

  // Pipeline factory and accessor.
  assert.match(webgpu, /function wgpuCreatePBRInstancedCullPipeline/,
    "wgpuCreatePBRInstancedCullPipeline factory must exist");
  assert.match(webgpu, /function getPBRInstancedCullPipeline/,
    "getPBRInstancedCullPipeline accessor must exist");
  assert.match(webgpu, /WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT/,
    "cull pipeline must reference cull layout");

  // No shadow variant for cull (D2: shadows stay draw-all).
  assert.doesNotMatch(webgpu, /wgpuCreateShadow.*[Cc]ull|[Cc]ull.*shadow/,
    "no shadow cull pipeline must exist (shadows stay draw-all)");
});

test("mesh bundle cull keeps authored zero-opacity shaders and culls plain zero-opacity meshes", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  const vertices = {
    positions: [
      -0.5, -0.5, 0,
       0.5, -0.5, 0,
       0.0,  0.5, 0,
    ],
    normals: [
      0, 0, 1,
      0, 0, 1,
      0, 0, 1,
    ],
    uvs: [
      0, 0,
      1, 0,
      0.5, 1,
    ],
    count: 3,
  };
  const state = api.createSceneState({
    scene: {
      objects: [
        {
          id: "selena-zero-opacity",
          kind: "mesh",
          vertices,
          opacity: 0,
          shaderBackend: "selena",
        },
        {
          id: "custom-zero-opacity",
          kind: "mesh",
          vertices,
          opacity: 0,
          customFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4f(1.0, 0.0, 0.0, 1.0); }",
        },
        {
          id: "plain-zero-opacity",
          kind: "mesh",
          vertices,
          opacity: 0,
        },
      ],
    },
  }, { tier: "full" });

  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], [], {}, 0, [], [], [], [], [], 0, false,
  );

  assert.deepEqual(Array.from(bundle.meshObjects, (object) => object.id), [
    "selena-zero-opacity",
    "custom-zero-opacity",
  ]);
  assert.equal(bundle.materials[bundle.meshObjects[0].materialIndex].opacity, 0);
  assert.equal(bundle.materials[bundle.meshObjects[0].materialIndex].shaderBackend, "selena");
  assert.equal(bundle.materials[bundle.meshObjects[1].materialIndex].opacity, 0);
  assert.equal(bundle.meshObjects.some((object) => object.id === "plain-zero-opacity"), false);
});

// -------------------------------------------------------------------------
// Task 2: createSceneInstancedCullSystem — buffer sizes and usage flags
// -------------------------------------------------------------------------
test("gpu-cull T2a-count: cull sizes buffers from `count` when `instanceCount` is absent", async () => {
  const { device } = makeFakeGPUDevice();
  const harness = await createCullSystemHarness(device);
  const api = harness.api;
  // Regression guard: real instanced meshes (e.g. the galaxy meteor ring)
  // serialize the count under `count` (legacyProps), NOT `instanceCount`. The
  // cull MUST resolve count→ or it sizes for 1 (capacity 32) and drawIndirect
  // renders only ~32 degenerate zero-matrix instances → an invisible ring.
  const mesh = { id: "meteors", count: 100, cullKernelWGSL: MINIMAL_CULL_WGSL };
  const sys = api.createSceneInstancedCullSystem(device, mesh);
  // capacity = max(32, 100 + floor(100/4)) = 125 → buffers 125*80 bytes.
  const expectedBufBytes = 125 * 80;
  assert.equal(sys.inputBuf.size, expectedBufBytes,
    "inputBuf must be sized from `count` (125*80=10000), not the instanceCount-missing fallback (32*80=2560)");
  assert.equal(sys.outputBuf.size, expectedBufBytes,
    "outputBuf must be sized from `count`");
});

test("gpu-cull T2a: createSceneInstancedCullSystem creates buffers with correct sizes and usage flags", async () => {
  const { device, state } = makeFakeGPUDevice();
  const harness = await createCullSystemHarness(device);
  const api = harness.api;

  assert.ok(typeof api.createSceneInstancedCullSystem === "function",
    "createSceneInstancedCullSystem must be exported to __gosx_scene3d_api");

  const mesh = { id: "meteors", instanceCount: 4, cullKernelWGSL: MINIMAL_CULL_WGSL };
  api.createSceneInstancedCullSystem(device, mesh);

  // Buffer sizes: capacity = max(32, 4 + 1) = 32 instances (minimum 32).
  const expectedCap = 32;
  const expectedBufBytes = expectedCap * 80; // 80B per InstanceRecord

  const buffers = state.writeBufferCalls.map(c => c.buffer)
    .concat(/* we inspect created buffers via their sizes */[]);

  // Inspect device's created buffers (all go through device.createBuffer).
  // We can look at the buffers on the device state by re-checking what was
  // created: makeFakeGPUDevice records createBuffer calls indirectly via size.
  // Count by size:
  const allBuffers = []; // re-read from state by tracing createBuffer.
  // Instead, call the system directly and check returned fields.
  const sys = api.createSceneInstancedCullSystem(device, mesh);
  assert.ok(sys, "createSceneInstancedCullSystem must return a system object");
  assert.equal(typeof sys.isReady, "function", "system must have isReady()");
  assert.equal(typeof sys.update, "function", "system must have update()");
  assert.equal(typeof sys.dispose, "function", "system must have dispose()");
  assert.ok(sys.inputBuf, "system must expose inputBuf");
  assert.ok(sys.outputBuf, "system must expose outputBuf");
  assert.ok(sys.drawArgsBuf, "system must expose drawArgsBuf");
  assert.ok(sys.cullUniformBuf, "system must expose cullUniformBuf");
  assert.equal(sys.inputBuf.size,  expectedBufBytes, "inputBuf size must be capacity*80");
  assert.equal(sys.outputBuf.size, expectedBufBytes, "outputBuf size must be capacity*80");
  assert.equal(sys.drawArgsBuf.size, 16,            "drawArgsBuf size must be 16 bytes (4*u32)");
  assert.equal(sys.cullUniformBuf.size, 112,        "cullUniformBuf size must be 112 bytes");

  // Usage flags: INDIRECT must be on drawArgsBuf; VERTEX must be on outputBuf.
  const INDIRECT = 0x100;
  const VERTEX   = 0x20;
  const STORAGE  = 0x80;
  assert.ok(sys.drawArgsBuf.usage & INDIRECT,
    "drawArgsBuf must have INDIRECT usage (for drawIndirect)");
  assert.ok(sys.outputBuf.usage & VERTEX,
    "outputBuf must have VERTEX usage (bound as vertex in main pass)");
  assert.ok(sys.outputBuf.usage & STORAGE,
    "outputBuf must have STORAGE usage (compute write)");
  assert.ok(sys.drawArgsBuf.usage & STORAGE,
    "drawArgsBuf must have STORAGE usage (atomicAdd in shader)");
});

// -------------------------------------------------------------------------
// Per-pass GPU timing through the STANDARD timestampWrites API.
//
// encoder.writeTimestamp, which the frame timer uses, is not part of the
// WebGPU standard and Chromium removed it. timestampWrites on the render-pass
// descriptor is the standard path, and it also yields a time per pass.
// -------------------------------------------------------------------------

test("gpu pass timing: the shadow and main passes carry timestampWrites", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { timestampQuery: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#8de1ff" }],
      lights: [{ id: "sun", kind: "directional", directionX: 0, directionY: -1, directionZ: -0.2, castShadow: true }],
      objects: bundleBoxes(2, 1),
    },
  }, { tier: "full" });
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], api.sceneStateLights(state), {}, 0, [], [], [], [], [], 0, false,
  );

  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });

  const stamped = harness.fake.state.renderPasses.filter((p) => p.descriptor && p.descriptor.timestampWrites);
  assert.ok(stamped.length >= 2, "the shadow pass and the main pass must both be stamped");
  const indices = stamped.map((p) => [
    p.descriptor.timestampWrites.beginningOfPassWriteIndex,
    p.descriptor.timestampWrites.endOfPassWriteIndex,
  ]);
  // Four stamps per ring entry: shadow begin/end at base+0/1, main begin/end at
  // base+2/3. The base depends on which ring slot the frame took.
  const base = indices[0][0];
  assert.equal(base % 4, 0, "the shadow pair must start on a ring boundary");
  assert.deepEqual(indices.slice(0, 2), [[base, base + 1], [base + 2, base + 3]]);
  const querySets = new Set(stamped.map((p) => p.descriptor.timestampWrites.querySet));
  assert.equal(querySets.size, 1, "both passes must write into one query set");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-timing"), "pending");
});

test("gpu pass timing: a second shadow pass is not stamped over the first", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { timestampQuery: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  // Two directional casters open two shadow passes. The ring holds one pair, so
  // stamping both would report the second and hide the first.
  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#8de1ff" }],
      lights: [
        { id: "sun", kind: "directional", directionX: 0, directionY: -1, directionZ: -0.2, castShadow: true },
        { id: "fill", kind: "directional", directionX: 1, directionY: -1, directionZ: 0, castShadow: true },
      ],
      objects: bundleBoxes(2, 1),
    },
  }, { tier: "full" });
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], api.sceneStateLights(state), {}, 0, [], [], [], [], [], 0, false,
  );

  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
  const stamped = harness.fake.state.renderPasses.filter((p) => p.descriptor && p.descriptor.timestampWrites);
  const shadowStamps = stamped.filter((p) => p.descriptor.timestampWrites.beginningOfPassWriteIndex % 4 === 0);
  assert.equal(shadowStamps.length, 1, "exactly one shadow pass may be stamped per frame");
});

test("gpu pass timing: the resolved stamps become per-pass milliseconds", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { timestampQuery: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#8de1ff" }],
      lights: [{ id: "sun", kind: "directional", directionX: 0, directionY: -1, directionZ: -0.2, castShadow: true }],
      objects: bundleBoxes(2, 1),
    },
  }, { tier: "full" });
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], api.sceneStateLights(state), {}, 0, [], [], [], [], [], 0, false,
  );

  // The readback is deliberately delayed by two frames, so drive several frames
  // and drain the async work between them.
  for (let frame = 0; frame < 5; frame += 1) {
    harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: frame * 17, active: true });
    await flushAsyncWork();
  }

  const mount = harness.mount;
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-timing"), "measured");
  // The fake resolves [0, 1e6, 1.5e6, 4e6] ns with a 1 ns period.
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-shadow-ms"), "1.000");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-main-ms"), "2.500");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-scene-ms"), "4.000");
});

test("gpu pass timing: a device without timestamp-query reports the timer as unavailable", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  harness.renderer.render(bundleMeshScene(api, bundleBoxes(1, 1)), { width: 64, height: 64 }, { nowMS: 0, active: true });
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-timing"), "timer-unavailable");
  const stamped = harness.fake.state.renderPasses.filter((p) => p.descriptor && p.descriptor.timestampWrites);
  assert.equal(stamped.length, 0, "no pass may carry timestampWrites without the feature");
});

test("gpu pass timing: the pass timer feeds the shared sample when writeTimestamp is absent", async () => {
  // This is the Chromium case: timestamp-query is present, encoder.writeTimestamp
  // is not. Before the pass timer the page got no GPU time at all.
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { timestampQuery: true, writeTimestamp: false },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#8de1ff" }],
      lights: [{ id: "sun", kind: "directional", directionX: 0, directionY: -1, directionZ: -0.2, castShadow: true }],
      objects: bundleBoxes(2, 1),
    },
  }, { tier: "full" });
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], api.sceneStateLights(state), {}, 0, [], [], [], [], [], 0, false,
  );

  for (let frame = 0; frame < 5; frame += 1) {
    harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: frame * 17, active: true });
    await flushAsyncWork();
  }
  const mount = harness.mount;
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-gpu-pass-timing"), "measured");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-gpu-timing"), "measured-pass",
    "the whole-scene span must stand in for the missing frame timer");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-gpu-ms"), "4.000");
});

test("render bundles: a static mesh scene encodes once and replays after", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { renderBundles: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  const bundle = bundleMeshScene(api, bundleBoxes(3, 1));
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });

  const mount = harness.mount;
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "encoded",
    "the first frame must encode a bundle");
  assert.equal(harness.fake.state.renderBundles.length, 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-reason"), "");
  const draws = Number(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-draws"));
  assert.ok(draws >= 3, "all three boxes must be inside the bundle, got " + draws);

  // The bundle encoder, not the main pass, carried the draws.
  const bundleEncoder = harness.fake.state.renderBundleEncoders[0];
  assert.equal(bundleEncoder.draws.length, draws);
  const mainPass = harness.fake.state.renderPasses[harness.fake.state.renderPasses.length - 1];
  assert.equal(mainPass.executedBundles.length, 1, "the main pass must replay the bundle");

  for (let frame = 1; frame <= 3; frame += 1) {
    harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: frame * 17, active: true });
    assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "replayed",
      "frame " + frame + " must replay, not re-encode");
  }
  assert.equal(harness.fake.state.renderBundles.length, 1, "four frames, one encode");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-encodes"), "1");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-replays"), "3");
});

test("render bundles: adding an object re-encodes instead of replaying a stale bundle", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { renderBundles: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  const mount = harness.mount;

  harness.renderer.render(bundleMeshScene(api, bundleBoxes(2, 1)), { width: 64, height: 64 }, { nowMS: 0, active: true });
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "encoded");
  const firstDraws = Number(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-draws"));

  // A third box is a new draw. Replaying the two-box bundle would drop it from
  // the image with no error anywhere.
  harness.renderer.render(bundleMeshScene(api, bundleBoxes(3, 1)), { width: 64, height: 64 }, { nowMS: 17, active: true });
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "encoded",
    "a changed draw set must re-encode");
  assert.equal(harness.fake.state.renderBundles.length, 2);
  assert.ok(
    Number(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-draws")) > firstDraws,
    "the new bundle must carry the extra draw",
  );

  // Removing it again must also re-encode rather than replay the three-box
  // bundle, which would draw a box that is no longer in the scene.
  harness.renderer.render(bundleMeshScene(api, bundleBoxes(2, 1)), { width: 64, height: 64 }, { nowMS: 34, active: true });
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "encoded");
  assert.equal(harness.fake.state.renderBundles.length, 3);
});

test("render bundles: a scene the bundled set excludes keeps the direct path", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { renderBundles: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  // Points draw after the meshes in the same render pass, and the bundled set
  // does not carry them, so the frame must not bundle at all.
  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#8de1ff" }],
      objects: bundleBoxes(2, 1),
    },
  }, { tier: "full" });
  const withMaterials = api.sceneStateObjectsWithMaterials(state);
  const pointBundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    withMaterials, [], [], [], [], {}, 0,
    [{ id: "stars", positions: [0, 0, 0, 1, 1, 1], size: 2, color: "#ffffff" }],
    [], [], [], [], 0, false,
  );

  harness.renderer.render(pointBundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "direct");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-reason"), "points");
  assert.equal(harness.fake.state.renderBundles.length, 0, "no bundle may be built");
  // And the draws must still reach the main pass exactly once.
  const mainPass = harness.fake.state.renderPasses[harness.fake.state.renderPasses.length - 1];
  assert.ok(mainPass.draws.length > 0, "the direct path must still draw");
});

test("render bundles: a page can force the direct path", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { renderBundles: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.env.context.__gosx_scene3d_webgpu_render_bundles = false;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  harness.renderer.render(bundleMeshScene(api, bundleBoxes(2, 1)), { width: 64, height: 64 }, { nowMS: 0, active: true });
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "direct");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-reason"), "disabled");
  assert.equal(harness.fake.state.renderBundles.length, 0);
});

test("render bundles: a device without createRenderBundleEncoder still renders", async () => {
  // The fake device omits the method unless a test asks for it, which is the
  // honest degrade: an implementation without render bundles draws directly.
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  harness.renderer.render(bundleMeshScene(api, bundleBoxes(2, 1)), { width: 64, height: 64 }, { nowMS: 0, active: true });
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "direct");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-reason"), "disabled");
  const mainPass = harness.fake.state.renderPasses[harness.fake.state.renderPasses.length - 1];
  assert.ok(mainPass.draws.length > 0);
});

test("gpu-driven draw: a large instanced mesh culls on the GPU and draws indirectly", async () => {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { renderBundles: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  // No authored cullKernelWGSL. Before the built-in kernel this mesh drew every
  // instance every frame with the CPU rebuilding the draw list.
  const mesh = bundleInstancedMesh(512, 1);
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 40, fov: 60, near: 0.05, far: 256 },
    [], [], [], [], [], {}, 0, [], [mesh], [], [], [], 0, false,
  );

  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
  await flushAsyncWork();
  await flushAsyncWork();
  const mount = harness.mount;
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-cull-builtin-systems"), "1",
    "the mesh must have taken the renderer's own cull kernel");

  // Second frame: the pipeline has resolved, so the draw must go indirect.
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 17, active: true });
  const indirect = harness.fake.state.renderBundleEncoders
    .concat(harness.fake.state.renderPasses)
    .reduce((total, pass) => total + pass.drawIndirects.length, 0);
  assert.ok(indirect >= 1, "the compacted survivors must be drawn with drawIndirect");
  assert.ok(Number(mount.getAttribute("data-gosx-scene3d-webgpu-cull-dispatches")) >= 1);

  // Third frame with the same transforms and the same camera: the whole cull
  // dispatch must be skipped, and the bundle must replay. The GPU still owns the
  // instance count, so the image is right without any CPU work at all.
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 34, active: true });
  assert.ok(
    Number(mount.getAttribute("data-gosx-scene3d-webgpu-cull-skipped-dispatches")) >= 1,
    "a static instanced scene must stop re-dispatching the cull",
  );
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "replayed");
});

test("gpu-driven draw: a small instanced mesh stays on the draw-all path", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  // Below the threshold one compute dispatch plus one indirect draw costs more
  // than drawing every instance.
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 40, fov: 60, near: 0.05, far: 256 },
    [], [], [], [], [], {}, 0, [], [bundleInstancedMesh(8, 1)], [], [], [], 0, false,
  );
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
  await flushAsyncWork();
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-cull-builtin-systems"), "0");
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 17, active: true });
  const mainPass = harness.fake.state.renderPasses[harness.fake.state.renderPasses.length - 1];
  assert.equal(mainPass.drawIndirects.length, 0, "a small mesh must draw directly");
  assert.ok(mainPass.draws.some((d) => d.instanceCount === 8), "all eight instances must draw");
});

test("gpu-driven draw: per-instance colours keep a mesh off the cull path", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  // The compacted 80-byte record's last vec4 is pick data, not colour, so the
  // cull vertex shader hands the fragment shader white. A coloured mesh routed
  // through it would lose its colours, which is a silent wrong image.
  const mesh = bundleInstancedMesh(512, 1);
  mesh.colors = new Array(512 * 4).fill(0.5);
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 40, fov: 60, near: 0.05, far: 256 },
    [], [], [], [], [], {}, 0, [], [mesh], [], [], [], 0, false,
  );
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
  await flushAsyncWork();
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-cull-builtin-systems"), "0",
    "a coloured instanced mesh must not be culled by the built-in kernel");
});

test("gpu-driven draw: a page can turn the built-in cull off", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.env.context.__gosx_scene3d_webgpu_builtin_cull = false;
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 40, fov: 60, near: 0.05, far: 256 },
    [], [], [], [], [], {}, 0, [], [bundleInstancedMesh(512, 1)], [], [], [], 0, false,
  );
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
  await flushAsyncWork();
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-cull-builtin-systems"), "0");
});

test("render bundles: the frame bind group survives across frames", async () => {
  // The bundle can only replay because the frame bind group is memoized. Prove
  // the memo directly: a second frame must not build another one.
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { renderBundles: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  const bundle = bundleMeshScene(api, bundleBoxes(2, 1));

  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
  const afterFirst = harness.fake.state.bindGroups.length;
  harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 17, active: true });
  assert.equal(
    harness.fake.state.bindGroups.length,
    afterFirst,
    "a static frame must create no new bind group at all",
  );
});

// T2b changed contract. An absent cullKernelWGSL used to return null, which
// meant a mesh only ever culled on the GPU when the author wrote a kernel. The
// renderer now owns a kernel of its own, so an absent authored kernel builds a
// system that runs the built-in one. The eligibility gate that decides WHICH
// meshes take that path lives in 16a (webGPUBuiltinCullEligible); this factory
// builds whatever it is asked for.
test("gpu-cull T2b: absent cullKernelWGSL → built-in kernel system", async () => {
  const { device } = makeFakeGPUDevice();
  const harness = await createCullSystemHarness(device);
  const api = harness.api;

  const sys = api.createSceneInstancedCullSystem(device, { id: "no-kernel", instanceCount: 4 });
  assert.ok(sys, "absent cullKernelWGSL must fall back to the renderer's own kernel");
  assert.equal(sys.usesBuiltinKernel, true);
  const authored = api.createSceneInstancedCullSystem(device,
    { id: "authored", instanceCount: 4, cullKernelWGSL: MINIMAL_CULL_WGSL });
  assert.equal(authored.usesBuiltinKernel, false, "an authored kernel must win");
});

// The defect this pins: a constant radius drops an instance that its transform
// scales up, so the instance vanishes while it is plainly on screen. The built-in
// kernel must scale the radius per thread, exactly as cullWGSL does in
// render/bundle/cull.go. Removing any of the three length() calls fails this.
test("gpu-cull T2b2: the built-in kernel scales the radius per instance", async () => {
  const { device } = makeFakeGPUDevice();
  const harness = await createCullSystemHarness(device);
  const wgsl = harness.api.SCENE_INSTANCED_CULL_BUILTIN_WGSL;
  void device;
  assert.equal(typeof wgsl, "string");
  assert.match(
    wgsl,
    /let scale = max\(length\(m\[0\]\.xyz\), max\(length\(m\[1\]\.xyz\), length\(m\[2\]\.xyz\)\)\);/,
    "the kernel must take the largest of the three transform column lengths",
  );
  assert.match(wgsl, /if \(scale > 0\.0\) \{ radius = radius \* scale; \}/,
    "the radius must be scaled, and a degenerate transform must keep the base radius");
  assert.match(wgsl, /if \(d < -radius\) \{ return; \}/,
    "the plane test must use the scaled radius");
  // The input buffer's capacity runs past the live instance count and WebGPU
  // zero-initializes a buffer, so a thread past the count would compact a
  // zero-matrix record. It draws nothing, but it inflates the survivor count.
  assert.match(
    wgsl,
    /if \(index >= min\(cull\.instanceCount, arrayLength\(&src\)\)\) \{ return; \}/,
    "the thread guard must bound on the live instance count, not on the capacity",
  );
});

test("gpu-cull T2b2b: the uniform carries the live instance count", async () => {
  const fake = makeFakeGPUDeviceForCompute({});
  const harness = await createCullSystemHarness(fake.device);
  const api = harness.api;

  const sys = api.createSceneInstancedCullSystem(fake.device, { id: "bounded", instanceCount: 500 });
  await flushAsyncWork();
  await flushAsyncWork();
  assert.ok(sys.isReady());
  const encoder = fake.device.createCommandEncoder();
  fake.state.writeBufferCalls.length = 0;
  sys.update(fake.device, encoder, Array.from({ length: 6 }, () => [0, 0, 1, 10]), 36, new Float32Array(500 * 20), 500, {});
  const uniform = fake.state.writeBufferCalls.filter((w) => w.buffer === sys.cullUniformBuf).pop();
  assert.ok(uniform, "the uniform must be uploaded");
  const words = new Uint32Array(uniform.data);
  assert.equal(words[24], 36, "byte 96 is the vertex count");
  assert.equal(words[26], 500, "byte 104 carries the live instance count");
  assert.equal(words[27], 0, "byte 108 stays padding");
  // Capacity runs past the count, which is exactly why the guard is needed.
  assert.ok(sys.capacity > 500, `capacity ${sys.capacity} must exceed the count`);
});

test("gpu-cull T2b3: the CPU oracle matches the kernel's scale term", async () => {
  const { device } = makeFakeGPUDevice();
  const harness = await createCullSystemHarness(device);
  const api = harness.api;

  // Column-major mat4 with non-uniform scale 2, 3, 4.
  const transforms = new Float32Array([
    2, 0, 0, 0,
    0, 3, 0, 0,
    0, 0, 4, 0,
    5, 6, 7, 1,
  ]);
  assert.equal(api.sceneInstanceColumnScale(transforms, 0), 4, "the largest column wins");
  assert.equal(api.sceneInstanceCullRadius(1.5, transforms, 0), 6, "radius scales by the largest column");
  // A degenerate transform must keep the base radius rather than collapse.
  assert.equal(api.sceneInstanceCullRadius(1.5, new Float32Array(16), 0), 1.5);

  const two = new Float32Array(32);
  two.set(transforms, 0);
  two.set([9, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], 16);
  assert.equal(api.sceneInstancedMaxTransformScale(two, 2), 9, "the bound covers every instance");
  assert.equal(api.sceneInstancedMaxTransformScale(new Float32Array(16), 1), 1,
    "an all-zero transform must not report a zero bound");
});

test("gpu-cull T2b4: an authored kernel gets the conservative radius bound", async () => {
  const fake = makeFakeGPUDeviceForCompute({});
  const harness = await createCullSystemHarness(fake.device);
  const api = harness.api;

  const planes = Array.from({ length: 6 }, () => [0, 0, 1, 10]);
  const encoder = fake.device.createCommandEncoder();

  // An authored kernel may ignore per-instance scale entirely, so the JS side
  // inflates the uniform radius by the largest instance scale. Over-including is
  // safe; dropping a visible instance is not.
  const authored = api.createSceneInstancedCullSystem(fake.device,
    { id: "authored", instanceCount: 1, cullRadius: 2, cullKernelWGSL: MINIMAL_CULL_WGSL });
  await flushAsyncWork();
  await flushAsyncWork();
  assert.ok(authored.isReady());
  fake.state.writeBufferCalls.length = 0;
  authored.update(fake.device, encoder, planes, 36, new Float32Array(20), 1, { maxInstanceScale: 5 });
  const authoredUniform = fake.state.writeBufferCalls.filter((w) => w.buffer === authored.cullUniformBuf).pop();
  assert.ok(authoredUniform, "the uniform must be uploaded");
  assert.equal(new Float32Array(authoredUniform.data)[25], 10, "2 * 5");

  // The built-in kernel scales per thread, so inflating the uniform too would
  // double-count and weaken the cull.
  const builtin = api.createSceneInstancedCullSystem(fake.device,
    { id: "builtin", instanceCount: 1, cullRadius: 2 });
  await flushAsyncWork();
  await flushAsyncWork();
  assert.ok(builtin.isReady());
  fake.state.writeBufferCalls.length = 0;
  builtin.update(fake.device, encoder, planes, 36, new Float32Array(20), 1, { maxInstanceScale: 5 });
  const builtinUniform = fake.state.writeBufferCalls.filter((w) => w.buffer === builtin.cullUniformBuf).pop();
  assert.equal(new Float32Array(builtinUniform.data)[25], 2, "the kernel owns the scale term");
});

test("gpu-cull T2b5: a repeated transform fingerprint skips the whole dispatch", async () => {
  const fake = makeFakeGPUDeviceForCompute({});
  const harness = await createCullSystemHarness(fake.device);
  const api = harness.api;

  const planes = Array.from({ length: 6 }, () => [0, 0, 1, 10]);
  const sys = api.createSceneInstancedCullSystem(fake.device,
    { id: "static", instanceCount: 2, cullKernelWGSL: MINIMAL_CULL_WGSL });
  await flushAsyncWork();
  await flushAsyncWork();
  assert.ok(sys.isReady());

  const records = new Float32Array(40);
  const encoder = fake.device.createCommandEncoder();

  fake.state.writeBufferCalls.length = 0;
  fake.state.computePasses.length = 0;
  assert.equal(sys.update(fake.device, encoder, planes, 36, records, 2, { transformFingerprint: "abc" }), true);
  assert.equal(fake.state.computePasses.length, 1, "the first frame must dispatch");
  assert.ok(fake.state.writeBufferCalls.length >= 3, "reset, records and uniform all upload on the first frame");

  fake.state.writeBufferCalls.length = 0;
  fake.state.computePasses.length = 0;
  assert.equal(sys.update(fake.device, encoder, planes, 36, null, 2, { transformFingerprint: "abc" }), false);
  assert.equal(fake.state.computePasses.length, 0, "an unchanged scene must not dispatch again");
  assert.equal(fake.state.writeBufferCalls.length, 0, "and must not re-upload anything");
  assert.equal(sys.skippedDispatchCount, 1);

  // A moved camera changes the planes, so the survivor set changes and the
  // dispatch must come back even though the transforms did not move.
  const moved = [[0, 0, 1, 20], [0, 0, 1, 10], [0, 0, 1, 10], [0, 0, 1, 10], [0, 0, 1, 10], [0, 0, 1, 10]];
  fake.state.computePasses.length = 0;
  assert.equal(sys.update(fake.device, encoder, moved, 36, null, 2, { transformFingerprint: "abc" }), true);
  assert.equal(fake.state.computePasses.length, 1);

  // A null fingerprint means the caller cannot vouch for the contents, so every
  // frame must do the work.
  fake.state.computePasses.length = 0;
  assert.equal(sys.update(fake.device, encoder, moved, 36, null, 2, {}), true);
  assert.equal(fake.state.computePasses.length, 1);
});

test("gpu-cull T2b6: the transform fingerprint changes with the data", async () => {
  const { device } = makeFakeGPUDevice();
  const harness = await createCullSystemHarness(device);
  const fp = harness.api.sceneInstanceTransformFingerprint;

  const a = new Float32Array(32);
  a[0] = 1;
  const b = new Float32Array(32);
  b[0] = 1;
  assert.equal(fp(a, 2), fp(b, 2), "equal contents fold to the same value");
  b[17] = 0.5;
  assert.notEqual(fp(a, 2), fp(b, 2), "one changed float must change the fold");
  // A change beyond the requested count must not register.
  const c = new Float32Array(32);
  c[0] = 1;
  c[16] = 99;
  assert.equal(fp(a, 1), fp(c, 1), "the fold covers exactly count instances");
  // A plain Array must fold identically to the typed array with the same values.
  assert.equal(fp(a, 2), fp(Array.from(a), 2), "a plain Array must fold the same way");
});

test("gpu-cull T2c: zero/missing cullRadius defaults to non-zero (never literal 0)", async () => {
  const { device } = makeFakeGPUDevice();
  const harness = await createCullSystemHarness(device);
  const api = harness.api;

  const sys = api.createSceneInstancedCullSystem(device,
    { id: "zero-radius", instanceCount: 4, cullKernelWGSL: MINIMAL_CULL_WGSL, cullRadius: 0 });
  assert.ok(sys, "system must be created even with cullRadius: 0");
  assert.ok(sys.cullRadius > 0, "cullRadius must be defaulted to >0 (never literal 0)");
});

// -------------------------------------------------------------------------
// Task 2d: per-frame drawArgs reset (Risk #1 guard — the most critical test)
// -------------------------------------------------------------------------
test("gpu-cull T2d: writeBuffer(drawArgsBuf, [vertexCount,0,0,0]) occurs EVERY frame before dispatch", async () => {
  // Use the controllable async device so the pipeline resolves immediately.
  const fake = makeFakeGPUDeviceForCompute({});
  const harness = await createCullSystemHarness(fake.device);
  const api = harness.api;

  const mesh = { id: "m", instanceCount: 4, cullKernelWGSL: MINIMAL_CULL_WGSL, cullRadius: 1.5 };
  const sys = api.createSceneInstancedCullSystem(fake.device, mesh);
  assert.ok(sys, "system must be created");

  // Drain async pipeline creation.
  await flushAsyncWork();
  await flushAsyncWork();
  assert.ok(sys.isReady(), "system must be ready after async pipeline resolves");

  const encoder = fake.device.createCommandEncoder();
  const planes = [
    [0, 0, 1, 10], [0, 0, -1, 10],
    [0, 1, 0, 10], [0, -1, 0, 10],
    [1, 0, 0, 10], [-1, 0, 0, 10],
  ];
  const instanceRecords = new Float32Array(4 * 20); // 4 instances * 80B / 4B

  // Frame 1: call update.
  fake.state.writeBufferCalls.length = 0;
  sys.update(fake.device, encoder, planes, 6, instanceRecords, 4);

  const resetCallsF1 = fake.state.writeBufferCalls.filter(c =>
    c.buffer === sys.drawArgsBuf
  );
  assert.ok(resetCallsF1.length >= 1, "frame 1: writeBuffer to drawArgsBuf must happen");

  // Check the reset value: first u32 = vertexCount=6, rest = 0.
  const resetData = resetCallsF1[0].data;
  // Note: use ArrayBuffer check not instanceof Uint32Array (cross-VM context issue).
  assert.ok(resetData && (resetData instanceof Uint32Array || Object.prototype.toString.call(resetData) === "[object Uint32Array]" || resetData.constructor && resetData.constructor.name === "Uint32Array"),
    "drawArgs reset must be a Uint32Array (or Uint32Array-like)");
  assert.equal(resetData[0], 6, "drawArgs[0] (vertexCount) must be reset to 6");
  assert.equal(resetData[1], 0, "drawArgs[1] (instanceCount) must be reset to 0");
  assert.equal(resetData[2], 0, "drawArgs[2] (firstVertex) must be reset to 0");
  assert.equal(resetData[3], 0, "drawArgs[3] (firstInstance) must be reset to 0");

  // Frame 2: update again — drawArgsBuf must be written again.
  fake.state.writeBufferCalls.length = 0;
  sys.update(fake.device, encoder, planes, 6, instanceRecords, 4);
  const resetCallsF2 = fake.state.writeBufferCalls.filter(c =>
    c.buffer === sys.drawArgsBuf
  );
  assert.ok(resetCallsF2.length >= 1, "frame 2: drawArgsBuf MUST be written again (per-frame reset, accumulation guard)");
  assert.equal(resetCallsF2[0].data[1], 0, "frame 2: instanceCount must be 0 before dispatch");
});

test("gpu-cull T2e: compute pass is dispatched when system is ready", async () => {
  const fake = makeFakeGPUDeviceForCompute({});
  const harness = await createCullSystemHarness(fake.device);
  const api = harness.api;

  const sys = api.createSceneInstancedCullSystem(fake.device,
    { id: "m", instanceCount: 8, cullKernelWGSL: MINIMAL_CULL_WGSL });
  assert.ok(sys);
  await flushAsyncWork();
  await flushAsyncWork();
  assert.ok(sys.isReady(), "system must be ready");

  const encoder = fake.device.createCommandEncoder();
  const planes = Array.from({ length: 6 }, () => [0, 0, 0, 1]);
  sys.update(fake.device, encoder, planes, 6, new Float32Array(8 * 20), 8);

  // A compute pass must have been dispatched.
  assert.ok(fake.state.computePasses.length > 0, "at least one compute pass must be dispatched");
  const lastPass = fake.state.computePasses[fake.state.computePasses.length - 1];
  assert.ok(lastPass.ended, "compute pass must be ended");
});

// -------------------------------------------------------------------------
// Task 3: extractFrustumPlanesJS — golden test vs. native Go vectors
// -------------------------------------------------------------------------
test("gpu-cull T3: extractFrustumPlanesJS source exists in 11-scene-math.js (shared) and produces 6 normalized planes", () => {
  // extractFrustumPlanesJS was hoisted from 16a to 11-scene-math.js (Slice 3)
  // so both the WebGPU renderer (16a) and the WebGL2 renderer (16) share one
  // implementation with no divergence.
  const math = fs.readFileSync(path.join(__dirname, "bootstrap-src", "11-scene-math.js"), "utf8");
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // Function lives in the shared math module.
  assert.match(math, /function extractFrustumPlanesJS\(vp\)/,
    "extractFrustumPlanesJS must be defined in 11-scene-math.js (shared)");
  assert.match(math, /near.*R2|R2.*near/,
    "near plane formula must reference R2 (Gribb-Hartmann near=R2)");
  assert.match(math, /addRow.*r3.*r0|left.*R3\+R0/,
    "left plane must be R3+R0 (Gribb-Hartmann)");
  assert.match(math, /function instancePassesCullTest\(/,
    "instancePassesCullTest must be defined in 11-scene-math.js");

  // 16a must NOT redefine it (hoisted away) — it only carries the comment pointer.
  assert.doesNotMatch(webgpu, /function extractFrustumPlanesJS\(vp\)/,
    "extractFrustumPlanesJS must NOT be redefined in 16a (hoisted to 11)");

  // 16a still has the dispatch hook.
  assert.match(webgpu, /updateInstancedCullSystems\(/,
    "updateInstancedCullSystems dispatch hook must exist in 16a");
});

// Golden test: compare JS extractFrustumPlanesJS output against the planes
// that native Go extractFrustumPlanes produces for the same VP matrix.
// VP fixture: an identity matrix (degenerate frustum, planes are axis-aligned).
// Source is read from the UN-MINIFIED bootstrap-src file (the built bundle
// minifies function names so they can't be extracted by name).
test("gpu-cull T3-golden: extractFrustumPlanesJS matches native Go cull.go output for identity VP", () => {
  // An identity matrix VP. Gribb-Hartmann on identity:
  // row0=[1,0,0,0], row1=[0,1,0,0], row2=[0,0,1,0], row3=[0,0,0,1]
  //
  // left   = r3+r0 = [1,0,0,1]  → xyz mag = sqrt(1²+0+0) = 1 → [1,0,0,1]  (no change)
  // right  = r3-r0 = [-1,0,0,1] → xyz mag = 1               → [-1,0,0,1]
  // bottom = r3+r1 = [0,1,0,1]  → xyz mag = 1               → [0,1,0,1]
  // top    = r3-r1 = [0,-1,0,1] → xyz mag = 1               → [0,-1,0,1]
  // near   = r2    = [0,0,1,0]  → xyz mag = 1               → [0,0,1,0]
  // far    = r3-r2 = [0,0,-1,1] → xyz mag = 1               → [0,0,-1,1]
  //
  // The identity VP represents an orthographic projection where all frustum
  // plane normals are already unit-length (axis-aligned); normalization is
  // a no-op.  The native Go extractFrustumPlanes produces identical output.
  // This golden test documents the parity.
  //
  // Slice 3: extractFrustumPlanesJS was hoisted to 11-scene-math.js.
  // NOTE: built files (bootstrap-feature-scene3d-webgpu.js) are minified by
  // esbuild and function names are mangled, so we extract from the SOURCE file.
  const mathSrc = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "11-scene-math.js"), "utf8");

  const match = mathSrc.match(/function extractFrustumPlanesJS\(vp\)\s*\{([\s\S]*?)\n  \}/);
  assert.ok(match, "extractFrustumPlanesJS must be extractable from 11-scene-math.js source (check indentation)");

  const fnSrc = "function extractFrustumPlanesJS(vp) {" + match[1] + "\n  }";
  const extractFn = new Function("return (" + fnSrc + ")")();

  // Identity VP (column-major Float32Array).
  const identity = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);
  const planes = extractFn(identity);

  assert.ok(Array.isArray(planes) && planes.length === 6, "must return 6 planes");

  function approxEq(a, b, eps) { return Math.abs(a - b) < (eps || 1e-5); }
  function planeOk(p, nx, ny, nz, d, label) {
    assert.ok(approxEq(p[0], nx) && approxEq(p[1], ny) && approxEq(p[2], nz) && approxEq(p[3], d),
      label + ": got [" + p.join(",") + "] expected [" + [nx,ny,nz,d].join(",") + "]");
  }

  // Identity VP produces axis-aligned frustum planes (already unit normals).
  planeOk(planes[0],  1,  0,  0, 1, "left");    // r3+r0 = [1,0,0,1], xyz-mag=1
  planeOk(planes[1], -1,  0,  0, 1, "right");   // r3-r0 = [-1,0,0,1]
  planeOk(planes[2],  0,  1,  0, 1, "bottom");  // r3+r1 = [0,1,0,1]
  planeOk(planes[3],  0, -1,  0, 1, "top");     // r3-r1 = [0,-1,0,1]
  planeOk(planes[4],  0,  0,  1, 0, "near");    // r2    = [0,0,1,0], xyz-mag=1
  planeOk(planes[5],  0,  0, -1, 1, "far");     // r3-r2 = [0,0,-1,1], xyz-mag=1
});

test("gpu-cull T4a: no cullKernelWGSL → draw-all (direct pass.draw, not drawIndirect)", () => {
  // Source-level structural test: drawInstancedMeshes must contain an else-branch
  // that calls pass.draw (not drawIndirect) when no cullKernelWGSL or cull record.
  //
  // This is a source assertion because the full render pipeline needs the WebGL
  // renderer module (generateInstancedGeometry) which is not loaded in the WebGPU
  // test harness.  The behavioral contract is guaranteed by the source structure
  // and proven at runtime by T2e (compute dispatch) + T5-drawIndirect (fake records).
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // drawInstancedMeshes must contain a branch with pass.draw for draw-all.
  assert.match(webgpu, /pass\.draw\(geom\.vertexCount,\s*instanceCount\)/,
    "drawInstancedMeshes must have draw-all branch: pass.draw(geom.vertexCount, instanceCount)");

  // The else branch must emit pass.draw (not drawIndirect) when no cull system.
  assert.match(webgpu, /else\s*\{[^}]*pass\.draw\(geom\.vertexCount/s,
    "draw-all must be in the else branch of the cull ready check");

  // The draw-indirect call must be in the if-branch (ready cull path).
  assert.match(webgpu, /if\s*\(cullSys\s*&&\s*cullSys\.isReady\(\)\)/,
    "drawIndirect path must be gated by cullSys.isReady()");

  // Ensure the indirect draw call exists inside the if-branch.
  assert.match(webgpu, /pass\.drawIndirect\(cullSys\.drawArgsBuf,\s*0\)/,
    "drawIndirect must use cullSys.drawArgsBuf, offset 0");
});

test("gpu-cull T4b: cullKernelWGSL present but system not-ready → draw-all fallback (D3 design decision)", () => {
  // Source-level test: the not-ready fallback falls through to pass.draw.
  // D3: not-ready → draw-all (NOT skip).  The else-branch handles this because
  // cullSys.isReady() returns false when the async pipeline is pending.
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // The draw path gating — if cull system NOT ready, falls to else (draw-all).
  // Confirmed by: the if-guard is isReady(), so not-ready → else → pass.draw.
  assert.match(webgpu, /cullSys\.isReady\(\)/,
    "draw path must be gated by isReady() — not-ready falls to draw-all (else)");

  // The else branch must NOT have a return or skip (D3: never invisible).
  // Extract the drawInstancedMeshes function body and verify the else branch
  // calls pass.draw rather than returning early.
  const fnMatch = webgpu.match(/function drawInstancedMeshes\([^)]*\)\s*\{([\s\S]*?)\n    \}/);
  assert.ok(fnMatch, "drawInstancedMeshes function must be present");
  const body = fnMatch[1];

  // The else branch has pass.draw, not a return/skip.
  assert.ok(body.includes("pass.draw(geom.vertexCount"),
    "D3: drawInstancedMeshes must call pass.draw in else-branch (draw-all, not skip)");
  // The if-branch has pass.drawIndirect.
  assert.ok(body.includes("pass.drawIndirect(cullSys.drawArgsBuf"),
    "ready cull path must call pass.drawIndirect");
});

// -------------------------------------------------------------------------
// Task 5: capability gating
// -------------------------------------------------------------------------
test("gpu-cull T5: 16a source structure — dispatch hook calls updateInstancedCullSystems in frame loop", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // The dispatch hook must call updateInstancedCullSystems.
  assert.match(webgpu, /updateInstancedCullSystems\(bundle\.instancedMeshes,\s*encoder,\s*scratchSelenaViewProjection\)/,
    "frame loop must call updateInstancedCullSystems with instancedMeshes, encoder, and scratchSelenaViewProjection");

  // The frame loop must call it AFTER uploadFrameUniforms (so VP is ready) and
  // BEFORE the shadow and main render passes.
  const uploadPos = webgpu.indexOf("var cam = uploadFrameUniforms(");
  const cullPos   = webgpu.indexOf("updateInstancedCullSystems(bundle.instancedMeshes");
  // Use the call site, not the function definition — the definition appears first
  // in the file but runs only when invoked.  The call passes `shadowSlots[slot]`
  // which is unique to the call site; the definition uses parameter names.
  const shadowPos = webgpu.indexOf("renderShadowPass(encoder, lightMatrix, bundle, shadowSlots");
  // The main color pass is identified by `var mainPass = encoder.beginRenderPass(`
  // to avoid matching earlier shadow/utility beginRenderPass calls.
  const mainPassPos = webgpu.indexOf("var mainPass = encoder.beginRenderPass(");
  assert.ok(uploadPos > 0 && cullPos > 0 && shadowPos > 0 && mainPassPos > 0,
    "all frame-loop landmarks must exist");
  assert.ok(cullPos > uploadPos,
    "updateInstancedCullSystems must run AFTER uploadFrameUniforms (VP is ready)");
  assert.ok(cullPos < shadowPos,
    "updateInstancedCullSystems must run BEFORE shadow pass");
  assert.ok(cullPos < mainPassPos,
    "updateInstancedCullSystems must run BEFORE main render pass");
});

test("gpu-cull T5-source: 16b exports createSceneInstancedCullSystem via __gosx_scene3d_api", () => {
  const compute = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "compute.ts"), "utf8");

  assert.match(compute, /function createSceneInstancedCullSystem/,
    "createSceneInstancedCullSystem must be defined in 16b");
  assert.match(compute, /createSceneInstancedCullSystem,/,
    "must be exported in Object.assign to __gosx_scene3d_api");
  assert.match(compute, /cullSystemSignature,/,
    "cullSystemSignature must be exported");
  assert.match(compute, /INSTANCED_CULL_RECORD_STRIDE.*80|80.*INSTANCED_CULL_RECORD_STRIDE/,
    "must define 80-byte record stride constant");
  assert.match(compute, /INSTANCED_CULL_UNIFORM_SIZE.*112|112.*INSTANCED_CULL_UNIFORM_SIZE/,
    "must define 112-byte uniform size constant");
});

test("gpu-cull T5-drawIndirect: makePass records drawIndirect calls (harness extension)", async () => {
  // Verify that the fake pass now records drawIndirect.
  const { device, state } = makeFakeGPUDevice();
  const encoder = device.createCommandEncoder();
  const renderPass = encoder.beginRenderPass({ colorAttachments: [{ view: {} }] });
  const fakeBuffer = { __kind: "buffer", size: 16 };

  renderPass.drawIndirect(fakeBuffer, 0);
  renderPass.end();

  assert.equal(state.renderPasses.length, 1, "render pass must be recorded");
  const pass = state.renderPasses[0];
  assert.equal(pass.drawIndirects.length, 1, "drawIndirect must be recorded in the fake pass");
  assert.equal(pass.drawIndirects[0].buffer, fakeBuffer, "drawIndirect must record the correct buffer");
  assert.equal(pass.drawIndirects[0].offset, 0, "drawIndirect must record offset=0");
});

test("gpu-cull T5-capability: gpu-cull is true in WebGPU capabilities and false in WebGL2", () => {
  const webgpuCaps = JSON.parse(fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "16a-scene-webgpu.capabilities.json"), "utf8"));
  const webglCaps = JSON.parse(fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "16-scene-webgl.capabilities.json"), "utf8"));
  assert.equal(webgpuCaps["gpu-cull"], true, "WebGPU must declare gpu-cull: true");
  assert.equal(webglCaps["gpu-cull"], false, "WebGL2 must declare gpu-cull: false");
});

// -------------------------------------------------------------------------
// S3 T1: instancePassesCullTest — survivor parity vs hand-computed reference
// -------------------------------------------------------------------------
// Scenario: identity VP (axis-aligned frustum, all 6 planes as computed by
// T3-golden).  Place 4 instances:
//   i0: translation (0,0,0)    — inside (center of frustum)
//   i1: translation (2,0,0)    — outside (right plane: d = -1*2+1 = -1 < -radius=0)
//                                Wait — for right plane [−1,0,0,1]: d = −1*2 + 0 + 0 + 1 = −1 < 0
//                                With radius=0 default (use 2.0): d = −1 < −2.0 → false — inside
//                                Actually d=−1; −r = −2.0 → d ≥ −r → inside!
//   We need to move further: translation (5,0,0):
//     right plane [-1,0,0,1]: d = -1*5 + 1 = -4; -radius = -2 → -4 < -2 → CULLED
//   i2: translation (-5,0,0):
//     left plane [1,0,0,1]: d = 1*(-5)+1 = -4 < -2 → CULLED
//   i3: translation (0,0,5):
//     far plane [0,0,-1,1]: d = -5+1 = -4 < -2 → CULLED
//
// So with identity VP and radius 2.0:
//   i0 at (0,0,0)   → VISIBLE
//   i1 at (5,0,0)   → CULLED (right)
//   i2 at (-5,0,0)  → CULLED (left)
//   i3 at (0,0,5)   → CULLED (far)
//   i4 at (0,5,0)   → CULLED (top: [0,-1,0,1]: d = -5+1=-4<-2)
//   i5 at (0,0,-3)  → near plane [0,0,1,0]: d=0*(-3)+0 = -3 < -2 → CULLED?
//                     Actually d = plane[0]*cx + plane[1]*cy + plane[2]*cz + plane[3]
//                               = 0*0 + 0*0 + 1*(-3) + 0 = -3 < -2 → CULLED (behind near)
// Expected survivors: only i0.

test("cpu-cull S3-T1: instancePassesCullTest survivor parity vs hand-computed reference", () => {
  const { extractFrustumPlanesJS, instancePassesCullTest } = loadCullFunctions();

  const identity = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);
  const planes = extractFrustumPlanesJS(identity);
  assert.ok(planes && planes.length === 6, "must have 6 planes");

  // Build transforms for 6 instances (col-major mat4, 16 floats each).
  // Translation is at indices 12,13,14 of each 16-float block.
  function makeTranslationMat4(x, y, z) {
    return [
      1, 0, 0, 0,  // col 0
      0, 1, 0, 0,  // col 1
      0, 0, 1, 0,  // col 2
      x, y, z, 1,  // col 3 — translation
    ];
  }

  const instances = [
    makeTranslationMat4(0, 0, 0),   // i0: center — VISIBLE
    makeTranslationMat4(5, 0, 0),   // i1: far right — CULLED
    makeTranslationMat4(-5, 0, 0),  // i2: far left  — CULLED
    makeTranslationMat4(0, 0, 5),   // i3: far back  — CULLED
    makeTranslationMat4(0, 5, 0),   // i4: far up    — CULLED
    makeTranslationMat4(0, 0, -3),  // i5: behind near — CULLED
  ];
  const transforms = new Float32Array(instances.flat());
  const radius = 2.0;

  const results = instances.map((_, idx) => instancePassesCullTest(transforms, idx, planes, radius));

  // Verify hand-computed expectations.
  assert.equal(results[0], true,  "i0 at origin must be VISIBLE");
  assert.equal(results[1], false, "i1 at (5,0,0) must be CULLED (right plane)");
  assert.equal(results[2], false, "i2 at (-5,0,0) must be CULLED (left plane)");
  assert.equal(results[3], false, "i3 at (0,0,5) must be CULLED (far plane)");
  assert.equal(results[4], false, "i4 at (0,5,0) must be CULLED (top plane)");
  assert.equal(results[5], false, "i5 at (0,0,-3) must be CULLED (near plane)");

  // Collect survivors.
  const survivors = instances
    .map((_, idx) => idx)
    .filter(idx => instancePassesCullTest(transforms, idx, planes, radius));
  assert.deepEqual(survivors, [0], "only i0 at origin must survive the cull");
});

// -------------------------------------------------------------------------
// S3 T2: instancePassesCullTest — radius sensitivity
// -------------------------------------------------------------------------
test("cpu-cull S3-T2: radius controls cull boundary (larger radius keeps more instances)", () => {
  const { extractFrustumPlanesJS, instancePassesCullTest } = loadCullFunctions();
  const identity = new Float32Array([
    1,0,0,0, 0,1,0,0, 0,0,1,0, 0,0,0,1,
  ]);
  const planes = extractFrustumPlanesJS(identity);

  // Instance at (3,0,0): right plane [-1,0,0,1]: d = -3+1 = -2.
  // radius=1.5 → -2 < -1.5 → CULLED.
  // radius=3   → -2 >= -3  → VISIBLE.
  function makeTf(x, y, z) {
    const t = new Float32Array(16);
    t[0]=1; t[5]=1; t[10]=1; t[15]=1; // identity mat
    t[12]=x; t[13]=y; t[14]=z;
    return t;
  }
  const tf3 = makeTf(3, 0, 0);

  assert.equal(instancePassesCullTest(tf3, 0, planes, 1.5), false,
    "radius 1.5: instance at (3,0,0) must be CULLED");
  assert.equal(instancePassesCullTest(tf3, 0, planes, 3.0), true,
    "radius 3.0: instance at (3,0,0) must be VISIBLE (sphere overlaps right plane)");
});

// -------------------------------------------------------------------------
// S3 T3: absent/zero cullRadius defaults to 2.0 (matches compute cull system)
// -------------------------------------------------------------------------
test("cpu-cull S3-T3: absent or zero cullRadius defaults to 2.0 (matches GPU-cull system default)", () => {
  const { extractFrustumPlanesJS, instancePassesCullTest } = loadCullFunctions();
  const identity = new Float32Array([
    1,0,0,0, 0,1,0,0, 0,0,1,0, 0,0,0,1,
  ]);
  const planes = extractFrustumPlanesJS(identity);

  // At (2.5,0,0): right plane d = -2.5+1 = -1.5; -default_radius = -2.0; -1.5 >= -2 → VISIBLE.
  // At (3.5,0,0): right plane d = -3.5+1 = -2.5; -2.5 < -2.0 → CULLED.
  function makeTf(x) {
    const t = new Float32Array(16);
    t[0]=1; t[5]=1; t[10]=1; t[15]=1;
    t[12]=x;
    return t;
  }

  // undefined radius → default 2.0
  assert.equal(instancePassesCullTest(makeTf(2.5), 0, planes, undefined), true,
    "undefined radius → default 2.0 → (2.5,0,0) is VISIBLE");
  assert.equal(instancePassesCullTest(makeTf(3.5), 0, planes, undefined), false,
    "undefined radius → default 2.0 → (3.5,0,0) is CULLED");
  // 0 radius → default 2.0
  assert.equal(instancePassesCullTest(makeTf(2.5), 0, planes, 0), true,
    "zero radius → default 2.0 → (2.5,0,0) is VISIBLE");
  assert.equal(instancePassesCullTest(makeTf(3.5), 0, planes, 0), false,
    "zero radius → default 2.0 → (3.5,0,0) is CULLED");
});

// -------------------------------------------------------------------------
// S3 T4: WebGL2 source — CPU cull path present in 16-scene-webgl.js
// -------------------------------------------------------------------------
test("cpu-cull S3-T4: 16-scene-webgl.js contains CPU cull path with correct structure", () => {
  const webgl = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");

  // The cull config check must be present.
  assert.match(webgl, /hasCullConfig/,
    "drawInstancedMeshes must have hasCullConfig gate");
  assert.match(webgl, /cullKernelWGSL/,
    "hasCullConfig must check cullKernelWGSL");

  // Must call the shared frustum + test functions.
  assert.match(webgl, /extractFrustumPlanesJS\(scratchSelenaViewProjection\)/,
    "CPU cull must call extractFrustumPlanesJS(scratchSelenaViewProjection)");
  assert.match(webgl, /instancePassesCullTest\(/,
    "CPU cull must call instancePassesCullTest");

  // Must compact into scratch buffers and upload via bufferData.
  assert.match(webgl, /_cpuCullScratchTransforms/,
    "must use _cpuCullScratchTransforms scratch buffer");
  assert.match(webgl, /gl\.bufferData\(gl\.ARRAY_BUFFER.*DYNAMIC_DRAW/,
    "must upload compacted data via bufferData with DYNAMIC_DRAW");

  // The draw call must use validated vertex and instance counts.
  assert.match(webgl, /gl\.drawArraysInstanced\(gl\.TRIANGLES,\s*0,\s*drawVertexCount,\s*instanceCount\)/,
    "drawArraysInstanced must use validated vertex and instance counts");

  // No-cull-config path: must still draw all instances (no early return before draw).
  assert.match(webgl, /var hasCullConfig.*\n.*var instanceColorData/s,
    "instanceColorData must be fetched before the cull branch (draw-all path)");
});

// -------------------------------------------------------------------------
// S3 T5: WebGL2 capability gating — no-cull-config → draw-all (source check)
// -------------------------------------------------------------------------
test("cpu-cull S3-T5: no cullKernelWGSL on WebGL2 → draw-all (unchanged instanceCount)", () => {
  const webgl = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");

  // The hasCullConfig branch is conditional — draw-all when false.
  assert.match(webgl, /if\s*\(hasCullConfig\)\s*\{/,
    "CPU cull must be inside if (hasCullConfig) block");

  // instanceColorData must be declared BEFORE the cull branch so the draw-all
  // path uses the original data directly.
  const cidPos = webgl.indexOf("var instanceColorData = sceneInstancedColorBuffer(mesh, instanceCount)");
  const cullPos = webgl.indexOf("if (hasCullConfig)");
  assert.ok(cidPos > 0, "instanceColorData declaration must exist");
  assert.ok(cullPos > 0, "hasCullConfig branch must exist");
  assert.ok(cidPos < cullPos, "instanceColorData must be declared BEFORE the hasCullConfig branch");
});

test("cpu-cull S3-T5b: WebGL instanced draw validates attribute capacity before drawArraysInstanced", () => {
  const webgl = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");

  assert.match(webgl, /function sceneInstancedAttributeCapacity\(data, components\)/,
    "instanced path must measure typed-array capacity");
  assert.match(webgl, /function bindInstancedVertexAttribute\(location, data, components, fallback\)/,
    "instanced path must bind per-vertex attributes through a capacity-aware helper");
  assert.match(webgl, /gl\.disableVertexAttribArray\(location\);\s*\n\s*gl\.vertexAttrib4f\(location/,
    "short optional streams must use a constant attribute instead of a zero-length VBO");
  assert.match(webgl, /var positionCapacity = bindInstancedVertexAttribute\(ip\.attributes\.position, geom\.positions, 3, \[0, 0, 0\]\);/,
    "required position stream must be validated");
  assert.match(webgl, /var drawVertexCount = Math\.min\(geom\.vertexCount, positionCapacity\);/,
    "draw vertex count must not exceed supplied position vertices");
  assert.match(webgl, /instanceCount = Math\.min\(instanceCount, sceneInstancedAttributeCapacity\(transformData, 16\)\);/,
    "instance count must not exceed supplied transform matrices");
  assert.match(webgl, /if \(mesh\._cachedInstanceColors && mesh\._cachedInstanceColors\.length >= count \* 4\)/,
    "cached instance colors must be revalidated against the requested count");
});

test("cpu-cull S3-T5c: WebGL instanced draw disables stale unowned vertex attributes", () => {
  const webgl = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");

  assert.match(webgl, /function sceneDisableUnownedVertexAttribArrays\(allowed\)/,
    "instanced path must clear stale global vertex attributes");
  assert.match(webgl, /sceneDisableUnownedVertexAttribArrays\(instancedAllowedAttribs\);/,
    "instanced path must clear stale attributes before drawArraysInstanced");

  const fnMatch = webgl.match(/function sceneDisableUnownedVertexAttribArrays\(allowed\) \{[\s\S]*?\n    \}/);
  assert.ok(fnMatch, "reset helper source must be extractable");
  const ops = [];
  const context = {
    gl: {
      MAX_VERTEX_ATTRIBS: 8,
      getParameter(param) {
        assert.equal(param, 8);
        return 8;
      },
      vertexAttribDivisor(location, divisor) {
        ops.push(["divisor", location, divisor]);
      },
      disableVertexAttribArray(location) {
        ops.push(["disable", location]);
      },
    },
    sceneNumber(value, fallback) {
      const n = Number(value);
      return Number.isFinite(n) ? n : fallback;
    },
  };
  vm.runInNewContext(fnMatch[0] + "\nsceneDisableUnownedVertexAttribArrays({0:true,4:true,5:true,6:true,7:true});", context);

  assert.deepEqual(
    ops.filter((op) => op[0] === "disable").map((op) => op[1]),
    [1, 2, 3],
    "stale zero-capacity attributes outside the instanced pass ownership set must be disabled");
  assert.ok(ops.some((op) => op[0] === "divisor" && op[1] === 1 && op[2] === 0),
    "stale attributes must also have divisors reset");
});

// -------------------------------------------------------------------------
// S3 T6: WebGPU path untouched — extractFrustumPlanesJS not redefined in 16a
// -------------------------------------------------------------------------
test("cpu-cull S3-T6: WebGPU path (16a) does NOT redefine extractFrustumPlanesJS (uses shared 11)", () => {
  const webgpu = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // Must not redefine the function (it was hoisted to 11).
  assert.doesNotMatch(webgpu, /function extractFrustumPlanesJS\(vp\)/,
    "16a must not redefine extractFrustumPlanesJS (hoisted to 11-scene-math.js)");

  // The GPU cull path must remain intact: updateInstancedCullSystems + drawIndirect.
  assert.match(webgpu, /updateInstancedCullSystems\(/,
    "GPU cull dispatch hook must still exist in 16a");
  assert.match(webgpu, /pass\.drawIndirect\(/,
    "GPU drawIndirect call must still exist in 16a");
  assert.match(webgpu, /cullSys\.isReady\(\)/,
    "GPU cull readiness gate must still exist in 16a");
});

// -------------------------------------------------------------------------
// S3 T7: 11-scene-math.js exports both shared cull functions
// -------------------------------------------------------------------------
test("cpu-cull S3-T7: 11-scene-math.js defines extractFrustumPlanesJS and instancePassesCullTest", () => {
  const math = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "11-scene-math.js"), "utf8");

  assert.match(math, /function extractFrustumPlanesJS\(vp\)/,
    "extractFrustumPlanesJS must be defined in 11-scene-math.js");
  assert.match(math, /function instancePassesCullTest\(transforms, instanceIndex, planes, radius\)/,
    "instancePassesCullTest must be defined in 11-scene-math.js");

  // instancePassesCullTest must match cull.go: cull when d < -radius.
  assert.match(math, /if\s*\(d\s*<\s*-r\)\s*return false/,
    "half-space test must cull when d < -r (matches cull.go)");

  // Default radius must be 2.0 (matches 16b-scene-compute.js).
  assert.match(math, /radius.*>.*0.*\?.*radius.*:.*2\.0|2\.0.*default/,
    "absent/zero radius must default to 2.0");
});

// -------------------------------------------------------------------------
// Telemetry T1: __gosx_scene3d_telemetry aggregates mount data attributes
// -------------------------------------------------------------------------
test("telemetry T1: __gosx_scene3d_telemetry aggregates scene mount data attributes", () => {
  // Build a minimal fake DOM element that exposes getAttribute/setAttribute.
  const attrs = {};
  const mount = {
    getAttribute(name) { return Object.prototype.hasOwnProperty.call(attrs, name) ? attrs[name] : null; },
    setAttribute(name, value) { attrs[name] = String(value); },
    removeAttribute(name) { delete attrs[name]; },
  };

  // Set the data attributes that telemetry reads.
  mount.setAttribute("data-gosx-scene3d-backend", "webgpu");
  mount.setAttribute("data-gosx-scene3d-ready", "true");
  mount.setAttribute("data-gosx-scene3d-mounted", "true");
  mount.setAttribute("data-gosx-scene3d-in-viewport", "true");
  mount.setAttribute("data-gosx-scene3d-capability-tier", "high");
  mount.setAttribute("data-gosx-scene3d-pixel-ratio", "2");
  mount.setAttribute("data-gosx-scene3d-quality-frame-ms", "16.7");
  mount.setAttribute("data-gosx-scene3d-quality-dpr-cap", "1.5");
  mount.setAttribute("data-gosx-scene3d-quality-postfx-suppressed", "false");
  mount.setAttribute("data-gosx-scene3d-adaptive-quality", "ok");
  mount.setAttribute("data-gosx-scene3d-render-loop-reason", "requestAnimationFrame");
  mount.setAttribute("data-gosx-scene3d-dropped", "0");
  mount.setAttribute("data-gosx-scene3d-device-memory", "8");
  mount.setAttribute("data-gosx-scene3d-hardware-concurrency", "12");
  mount.setAttribute("data-gosx-scene3d-cull-survivors",
    JSON.stringify({"cull-lab-left-culled": {"instanceCount": 108, "survivors": 72}}));
  mount.__gosxScene3DHandle = {
    getTelemetry() {
      return {
        camera: { x: 1, y: 2, z: 3 },
        orbit: { yaw: 0.25, pitch: -0.5, radius: 4 },
        selectionID: "sphere",
        lastPick: { entityID: "sphere", triangleIndex: 12 },
        rendererStats: { waterSimulationTickSeq: 42 },
      };
    },
  };

  // Run the telemetry function extracted from 20-scene-mount.js source in an
  // isolated vm context. We extract the function body and wrap it so it assigns
  // window.__gosx_scene3d_telemetry on a minimal window-like context.
  const mountSrc = readSceneMountSrc();
  // Extract the function definition from the source text.
  const fnStart = mountSrc.indexOf("window.__gosx_scene3d_telemetry = function sceneTelemSnapshot");
  assert.ok(fnStart >= 0, "could not find __gosx_scene3d_telemetry in 20-scene-mount.js");
  const fnEnd = mountSrc.indexOf("\n  window.__gosx_register_engine_factory", fnStart);
  assert.ok(fnEnd >= 0, "could not find end of telemetry function");
  const fnSource = mountSrc.slice(fnStart, fnEnd);

  const ctx = vm.createContext({
    window: {},
    document: {
      querySelector(sel) { if (sel === "[data-gosx-scene3d-mounted]") return mount; return null; },
    },
    JSON,
    parseFloat,
  });
  // Wire window.__gosx_scene3d_webgpu_diagnostics to undefined (not available).
  vm.runInContext(fnSource + "\n", ctx);

  const snap = ctx.window.__gosx_scene3d_telemetry(mount);
  assert.ok(snap !== null, "telemetry snapshot must not be null");
  assert.equal(snap.backend, "webgpu", "backend must be webgpu");
  assert.equal(snap.ready, true, "ready must be true");
  assert.equal(snap.mounted, true, "mounted must be true");
  assert.equal(snap.inViewport, true, "inViewport must be true");
  assert.equal(snap.capabilityTier, "high", "capabilityTier must be high");
  assert.equal(snap.pixelRatio, 2, "pixelRatio must be 2");
  assert.ok(Math.abs(snap.qualityFrameMs - 16.7) < 0.01, "qualityFrameMs must be ~16.7");
  assert.equal(snap.qualityDprCap, 1.5, "qualityDprCap must be 1.5");
  assert.equal(snap.qualityPostfxSuppressed, false, "qualityPostfxSuppressed must be false");
  assert.equal(snap.adaptiveQuality, "ok", "adaptiveQuality must be ok");
  assert.equal(snap.dropped, "0", "dropped must be '0'");
  assert.equal(snap.deviceMemory, 8, "deviceMemory must be 8");
  assert.equal(snap.hardwareConcurrency, 12, "hardwareConcurrency must be 12");
  // cull-survivors JSON should be parsed into an object.
  assert.ok(snap.cullSurvivors !== null, "cullSurvivors must not be null");
  assert.ok(typeof snap.cullSurvivors === "object", "cullSurvivors must be an object");
  assert.equal(snap.cullSurvivors["cull-lab-left-culled"].instanceCount, 108);
  assert.equal(snap.cullSurvivors["cull-lab-left-culled"].survivors, 72);
  assert.equal(snap.camera.x, 1, "interactive telemetry must expose the live camera");
  assert.equal(snap.orbit.yaw, 0.25, "interactive telemetry must expose orbit state");
  assert.equal(snap.selectionID, "sphere", "interactive telemetry must expose selection state");
  assert.equal(snap.lastPick.triangleIndex, 12, "interactive telemetry must expose the exact last pick");
  assert.equal(snap.rendererStats.waterSimulationTickSeq, 42, "interactive telemetry must expose renderer evidence");
  // webgpu diagnostics: absent (no diagnostics fn registered in this env).
  assert.equal(snap.webgpu, null, "webgpu diagnostics must be null when unavailable");
});

// -------------------------------------------------------------------------
// Telemetry T2: __gosx_scene3d_telemetry with null arg auto-finds mounted scene
// -------------------------------------------------------------------------
test("telemetry T2: __gosx_scene3d_telemetry(null) returns null when no mounted scene", () => {
  const mountSrc = readSceneMountSrc();
  const fnStart = mountSrc.indexOf("window.__gosx_scene3d_telemetry = function sceneTelemSnapshot");
  const fnEnd = mountSrc.indexOf("\n  window.__gosx_register_engine_factory", fnStart);
  const fnSource = mountSrc.slice(fnStart, fnEnd);

  const ctx = vm.createContext({
    window: {},
    document: {
      querySelector() { return null; },
    },
    JSON,
    parseFloat,
  });
  vm.runInContext(fnSource + "\n", ctx);

  const snap = ctx.window.__gosx_scene3d_telemetry(null);
  assert.equal(snap, null, "must return null when no mounted scene found");
});

test("telemetry T2a: explicit page scope exposes every registered mount and standard diagnostics", () => {
  function fakeMount(id, backend) {
    const attrs = {
      "data-gosx-scene3d-backend": backend,
      "data-gosx-scene3d-renderer": backend,
      "data-gosx-scene3d-ready": "true",
      "data-gosx-scene3d-mounted": "true",
    };
    return {
      id,
      getAttribute(name) {
        return Object.prototype.hasOwnProperty.call(attrs, name) ? attrs[name] : null;
      },
    };
  }

  const first = fakeMount("first-scene", "webgpu");
  const second = fakeMount("second-scene", "webgl");
  const registry = new Map([
    ["first-engine", {
      mount: first,
      snapshot() {
        return {
          id: "first-scene",
          mountID: "first-scene",
          engineID: "first-engine",
          component: "scene3d",
          diagnostics: [{severity: "info", code: "scene.backend.selected", backend: "webgpu"}],
          rendererDiagnostics: {ready: true, backend: "webgpu"},
        };
      },
    }],
    ["second-engine", {
      mount: second,
      snapshot() {
        return {
          id: "second-scene",
          mountID: "second-scene",
          engineID: "second-engine",
          component: "scene3d",
          diagnostics: [{severity: "warn", code: "scene.backend.fallback", backend: "webgl"}],
          rendererDiagnostics: {ready: true, backend: "webgl"},
        };
      },
    }],
  ]);
  const mountSrc = readSceneMountSrc();
  const fnStart = mountSrc.indexOf("window.__gosx_scene3d_telemetry = function sceneTelemSnapshot");
  const fnEnd = mountSrc.indexOf("\n  window.__gosx_register_engine_factory", fnStart);
  let webgpuProbeCalls = 0;
  const ctx = vm.createContext({
    window: {
      __gosx_scene3d_debug_registry: registry,
      __gosx_scene3d_webgpu_diagnostics() {
        webgpuProbeCalls += 1;
        return {
          ready: true,
          adapterAvailable: true,
          deviceAvailable: true,
          deviceFeatures: ["timestamp-query"],
        };
      },
    },
    document: {
      querySelector() { return first; },
      querySelectorAll() { return [first, second]; },
    },
    JSON,
    parseFloat,
  });
  vm.runInContext(mountSrc.slice(fnStart, fnEnd) + "\n", ctx);

  const page = ctx.window.__gosx_scene3d_telemetry({scope: "page"});
  assert.equal(webgpuProbeCalls, 1, "one page snapshot must read page-global WebGPU capability exactly once");
  assert.equal(page.scope, "page");
  assert.equal(page.mountCount, 2);
  assert.equal(page.mounts.length, 2);
  assert.equal(page.mounts[0].engineID, "first-engine");
  assert.equal(page.mounts[1].engineID, "second-engine");
  assert.equal(page.mounts[0].rendererDiagnostics.backend, "webgpu");
  assert.equal(page.mounts[1].diagnostics[0].code, "scene.backend.fallback");
  assert.equal(page.diagnostics.length, 2);
  assert.equal(page.diagnostics[1].mountID, "second-scene");
  assert.equal(page.diagnostics[1].engineID, "second-engine");
  assert.equal(page.pageCapabilities.webgpu.ready, true);

  const explicit = ctx.window.__gosx_scene3d_telemetry({scope: "mount", mount: second});
  assert.equal(webgpuProbeCalls, 2, "an explicit mount snapshot probes page capability once");
  assert.equal(explicit.scope, "mount");
  assert.equal(explicit.mountID, "second-scene");
  assert.equal(explicit.backend, "webgl");
  assert.equal(explicit.renderer, "webgl");
  assert.equal(explicit.rendererDiagnostics.backend, "webgl",
    "renderer-specific truth must remain WebGL even when the page-level WebGPU probe is ready");
  assert.equal(explicit.webgpuProbeScope, "page");
  assert.equal(explicit.pageCapabilities.webgpu.ready, true);
  assert.equal(explicit.webgpu.ready, true, "legacy webgpu alias remains page-scoped probe evidence");

  const legacy = ctx.window.__gosx_scene3d_telemetry(null);
  assert.equal(webgpuProbeCalls, 3, "a legacy mount snapshot probes page capability once");
  assert.equal(legacy.mountID, "first-scene", "legacy null must still select the first mounted scene");
});

test("telemetry T2b: strict typed parsing surfaces invalid values and malformed JSON", () => {
  const attrs = {
    "data-gosx-scene3d-ready": "yes",
    "data-gosx-scene3d-mounted": "true",
    "data-gosx-scene3d-in-viewport": "1",
    "data-gosx-scene3d-pixel-ratio": "16ms",
    "data-gosx-scene3d-quality-frame-ms": "Infinity",
    "data-gosx-scene3d-cull-survivors": "{",
  };
  const mount = {
    id: "invalid-scene",
    getAttribute(name) {
      return Object.prototype.hasOwnProperty.call(attrs, name) ? attrs[name] : null;
    },
  };
  const mountSrc = readSceneMountSrc();
  const fnStart = mountSrc.indexOf("window.__gosx_scene3d_telemetry = function sceneTelemSnapshot");
  const fnEnd = mountSrc.indexOf("\n  window.__gosx_register_engine_factory", fnStart);
  const ctx = vm.createContext({
    window: {},
    document: {querySelector() { return mount; }},
    JSON,
    parseFloat,
  });
  vm.runInContext(mountSrc.slice(fnStart, fnEnd) + "\n", ctx);

  const snap = ctx.window.__gosx_scene3d_telemetry(mount);
  assert.equal(snap.ready, null);
  assert.equal(snap.mounted, true);
  assert.equal(snap.inViewport, null);
  assert.equal(snap.pixelRatio, null);
  assert.equal(snap.qualityFrameMs, null);
  assert.equal(snap.cullSurvivors, null);
  assert.equal(snap.diagnostics.filter((entry) => entry.code === "scene.telemetry.invalid_attribute").length, 4);
  assert.equal(snap.diagnostics.filter((entry) => entry.code === "scene.telemetry.parse_error").length, 1);
  const pixelError = snap.diagnostics.find((entry) => entry.data.attribute === "data-gosx-scene3d-pixel-ratio");
  assert.equal(pixelError.data.value, "16ms");
  assert.equal(pixelError.data.expected, "finite-number");

  attrs["data-gosx-scene3d-cull-survivors"] = "[]";
  const wrongShape = ctx.window.__gosx_scene3d_telemetry(mount);
  assert.equal(wrongShape.cullSurvivors, null);
  assert.ok(wrongShape.diagnostics.some((entry) =>
    entry.code === "scene.telemetry.invalid_attribute"
      && entry.data.attribute === "data-gosx-scene3d-cull-survivors"
      && entry.data.expected === "json-object"));
});

test("telemetry T2c: missing attributes stay quiet while producer failures are contained", () => {
  const quietMount = {
    getAttribute() { return null; },
  };
  const failingMount = {
    id: "failing-scene",
    getAttribute() { return null; },
    __gosxScene3DHandle: {
      getTelemetry() { throw new Error("handle failed"); },
    },
  };
  const registry = new Map([["failing-engine", {
    mount: failingMount,
    snapshot() { throw new Error("debug failed"); },
  }]]);
  const mountSrc = readSceneMountSrc();
  const fnStart = mountSrc.indexOf("window.__gosx_scene3d_telemetry = function sceneTelemSnapshot");
  const fnEnd = mountSrc.indexOf("\n  window.__gosx_register_engine_factory", fnStart);
  let webgpuProbeCalls = 0;
  let webgpuProbeFails = true;
  const ctx = vm.createContext({
    window: {
      __gosx_scene3d_debug_registry: registry,
      __gosx_scene3d_webgpu_diagnostics() {
        webgpuProbeCalls += 1;
        if (webgpuProbeFails) throw new Error("webgpu failed");
        return {
          ready: true,
          adapterAvailable: true,
          deviceAvailable: true,
          deviceFeatures: [],
        };
      },
    },
    document: {
      querySelector() { return null; },
      querySelectorAll() { return []; },
    },
    JSON,
    parseFloat,
  });
  vm.runInContext(mountSrc.slice(fnStart, fnEnd) + "\n", ctx);

  const quiet = ctx.window.__gosx_scene3d_telemetry(quietMount);
  assert.equal(webgpuProbeCalls, 1, "direct mount scope probes once");
  assert.equal(quiet.diagnostics.length, 1,
    "only the page-global WebGPU producer failure should be reported; missing attributes are not invalid");
  assert.equal(quiet.diagnostics[0].data.producer, "webgpu-diagnostics");

  const failing = ctx.window.__gosx_scene3d_telemetry(failingMount);
  assert.equal(webgpuProbeCalls, 2, "each direct mount snapshot probes once");
  const producers = failing.diagnostics
    .filter((entry) => entry.code === "scene.telemetry.snapshot_failed")
    .map((entry) => entry.data.producer)
    .sort();
  assert.deepEqual([...producers], ["debug-surface", "mount-handle", "webgpu-diagnostics"]);
  assert.equal(failing.camera, null);

  registry.clear();
  webgpuProbeFails = false;
  const page = ctx.window.__gosx_scene3d_telemetry({scope: "page"});
  assert.equal(webgpuProbeCalls, 3, "an empty page still probes page-global capability exactly once");
  assert.equal(page.scope, "page");
  assert.equal(page.mountCount, 0);
  assert.equal(page.mounts.length, 0);
  assert.equal(page.diagnostics.length, 0);
  assert.equal(page.pageCapabilities.webgpu.ready, true,
    "an empty page still returns page-global WebGPU capability evidence");

  webgpuProbeFails = true;
  const failedPage = ctx.window.__gosx_scene3d_telemetry({scope: "page"});
  assert.equal(webgpuProbeCalls, 4, "a failed page snapshot still invokes the probe only once");
  assert.equal(failedPage.mountCount, 0);
  assert.equal(failedPage.pageCapabilities.webgpu, null);
  assert.equal(failedPage.diagnostics.length, 1,
    "a page-global probe failure must appear exactly once at page scope");
  assert.equal(failedPage.diagnostics[0].code, "scene.telemetry.snapshot_failed");
  assert.equal(failedPage.diagnostics[0].data.producer, "webgpu-diagnostics");

  assert.equal(ctx.window.__gosx_scene3d_telemetry(null), null,
    "legacy null still returns null when no mounted scene exists");
  assert.equal(webgpuProbeCalls, 4, "a missing legacy mount must not invoke the page probe");
});

// -------------------------------------------------------------------------
// Telemetry T3: cull readback wiring — 16b exposes requestSurvivorReadback + pollSurvivors
// -------------------------------------------------------------------------
test("telemetry T3: 16b-scene-compute.js exposes requestSurvivorReadback and pollSurvivors on cull system", () => {
  const compute = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "compute.ts"), "utf8");

  assert.match(compute, /requestSurvivorReadback/,
    "cull system must expose requestSurvivorReadback");
  assert.match(compute, /pollSurvivors/,
    "cull system must expose pollSurvivors");
  assert.match(compute, /COPY_SRC/,
    "drawArgsBuf must include COPY_SRC usage flag");
  assert.match(compute, /stagingBuf/,
    "must create a staging buffer for readback");
  assert.match(compute, /stagingMapping/,
    "must guard overlapping mapAsync calls with a mapping flag");
  assert.match(compute, /lastSurvivors/,
    "cull system must store lastSurvivors");
  assert.match(compute, /copyBufferToBuffer/,
    "requestSurvivorReadback must call copyBufferToBuffer");
});

// -------------------------------------------------------------------------
// Telemetry T4: cull telemetry gate — 16a gates readback on cull_telemetry flag
// -------------------------------------------------------------------------
test("telemetry T4: 16a-scene-webgpu.js gates survivor readback on __gosx_scene3d_cull_telemetry", () => {
  const webgpu = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /__gosx_scene3d_cull_telemetry/,
    "16a must reference __gosx_scene3d_cull_telemetry gate");
  assert.match(webgpu, /requestSurvivorReadback/,
    "16a must call requestSurvivorReadback on cull systems");
  assert.match(webgpu, /pollSurvivors/,
    "16a must call pollSurvivors on cull systems");
  assert.match(webgpu, /data-gosx-scene3d-cull-survivors/,
    "16a must write data-gosx-scene3d-cull-survivors attribute");
  assert.match(webgpu, /cullTelemetryFrameCount/,
    "16a must throttle readback with a frame counter");
  assert.match(webgpu, /lastCullSurvivors/,
    "16a must store lastCullSurvivors for publishWebGPUFrameStats");
});

// -------------------------------------------------------------------------
// Telemetry T5: __gosx_scene3d_telemetry exposed in 20-scene-mount.js source
// -------------------------------------------------------------------------
test("telemetry T5: 20-scene-mount.js defines __gosx_scene3d_telemetry", () => {
  const mount = readSceneMountSrc();

  assert.match(mount, /__gosx_scene3d_telemetry/,
    "20-scene-mount must define window.__gosx_scene3d_telemetry");
  assert.match(mount, /cull-survivors/,
    "telemetry function must reference cull-survivors attribute");
  assert.match(mount, /cullSurvivors/,
    "telemetry snapshot must include cullSurvivors field");
  assert.match(mount, /data-gosx-scene3d-backend/,
    "telemetry must read backend attribute");
});

// -------------------------------------------------------------------------
// Telemetry T6: poll/readback order — pollSurvivors must come BEFORE
// requestSurvivorReadback in the telemetry block so mapAsync reads data from
// the prior frame's submitted copy, not a not-yet-submitted copy.
// -------------------------------------------------------------------------
test("telemetry T6: 16a pollSurvivors is called BEFORE requestSurvivorReadback in telemetry block", () => {
  const webgpu = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // Use the LAST occurrence of "return instancedCullSystems;" as the block boundary
  // (there is an earlier early-return for the empty-meshes case that must be excluded).
  const gcEnd = webgpu.lastIndexOf("return instancedCullSystems;");
  assert.ok(gcEnd > 0, "must find return instancedCullSystems");
  const telemetryRegion = webgpu.slice(0, gcEnd);

  // Use lastIndexOf so we find the occurrences inside the telemetry block (not
  // earlier occurrences in comments or unrelated code blocks).
  const pollIdx = telemetryRegion.lastIndexOf("pollSurvivors");
  const readbackIdx = telemetryRegion.lastIndexOf("requestSurvivorReadback");
  assert.ok(pollIdx > 0, "pollSurvivors must appear in telemetry region");
  assert.ok(readbackIdx > 0, "requestSurvivorReadback must appear in telemetry region");
  assert.ok(
    pollIdx < readbackIdx,
    "pollSurvivors must be called BEFORE requestSurvivorReadback in the telemetry block " +
    "(polling reads data from prior submitted copy; readback encodes copy for current frame)"
  );
});

// -------------------------------------------------------------------------
// Telemetry T7: lastSurvivors null init — 16b initialises lastSurvivors to null
// so the snapshot can distinguish "never polled" (null) from "0 survivors" (0).
// -------------------------------------------------------------------------
test("telemetry T7: 16b initialises lastSurvivors to null (not 0)", () => {
  const compute = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "compute.ts"), "utf8");

  assert.match(compute, /lastSurvivors:\s*null/,
    "lastSurvivors must initialise to null so the HUD can distinguish pending from 0 survivors");
});

// -------------------------------------------------------------------------
// Bug-2 / Color T1: materialUniformData derives unlit from kind:"flat" /
// materialKind:"flat" — FlatMaterial must render unlit even when the material
// object comes from bundle.materials[] (which carries kind but never sets unlit).
// -------------------------------------------------------------------------
test("color T1: 16a materialUniformData sets unlit=1 for kind:flat and materialKind:flat", () => {
  const webgpu = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // The unlit uniform slot must be derived from mat.kind OR mat.materialKind,
  // not only from mat.unlit.  Regression guard: before the fix u[12] was set
  // only from mat.unlit, which is absent on normalizeSceneMaterialRecord objects
  // (they carry kind:"flat" but never set unlit:true) — causing FlatMaterial
  // instanced meshes to render via the PBR lighting path with no ambient, giving
  // near-black output.
  assert.match(
    webgpu,
    /u\[12\].*mat\.kind\s*===\s*["']flat["']/,
    "u[12] (unlit uniform) must be derived from mat.kind === 'flat'"
  );
  assert.match(
    webgpu,
    /u\[12\].*mat\.materialKind\s*===\s*["']flat["']/,
    "u[12] (unlit uniform) must be derived from mat.materialKind === 'flat'"
  );
});
