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
  const maxDefaultInstances = api.sceneGPUDrivenMaxInstances({});
  const maxDefaultCapacity = api.sceneGPUDrivenInstanceCapacity(maxDefaultInstances, {});
  assert.equal(maxDefaultCapacity, maxDefaultInstances);
  assert.ok(maxDefaultCapacity * 96 <= 128 * 1024 * 1024);
  assert.equal(api.sceneGPUDrivenInstanceCapacity(10, {}), 64);
  assert.equal(api.sceneGPUDrivenInstanceCapacity(42, { maxStorageBufferBindingSize: 4096 }), 42);
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


function transformsFor(count) {
  const out = [];
  for (let i = 0; i < count; i += 1) out.push(1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, (i % 10) - 5, 0, -Math.floor(i / 10), 1);
  return out;
}

function sceneState(api, gpuDriven, options) {
  const opts = options || {};
  const scene = {
    instancedMeshes: [
      { id: "crates", count: 30, kind: "box", width: 0.5, height: 0.5, depth: 0.5, transforms: transformsFor(30), castShadow: true, colors: new Array(30).fill("#ff8800") },
      { id: "orbs", count: 12, kind: "sphere", radius: 0.3, transforms: transformsFor(12) },
      { id: "glass", count: 4, kind: "box", transforms: transformsFor(4), opacity: 0.4 },
    ],
  };
  if (opts.light) scene.lights = [{ id: "sun", kind: "directional", castShadow: true, x: -1, y: -2, z: -1, intensity: 1 }];
  if (gpuDriven) scene.gpuDriven = gpuDriven;
  return api.createSceneState({ scene }, { tier: "full" });
}

function frameBundle(api, state) {
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 2, z: 12, fov: 60, near: 0.05, far: 200 },
    [], [], [], [], api.sceneStateLights(state), {}, 0, [], api.sceneStateInstancedMeshesWithMaterials(state), [], [], [], 0, false,
  );
  bundle.gpuDriven = state.gpuDriven;
  return bundle;
}

async function renderFrames(harness, api, state, frames) {
  for (let frame = 0; frame < frames; frame += 1) {
    harness.fake.state.renderPasses.length = 0;
    harness.fake.state.computePasses.length = 0;
    harness.renderer.render(frameBundle(api, state), { width: 64, height: 64 }, { nowMS: frame * 16, active: true });
    await flushAsyncWork();
  }
}

async function gpuDrivenHarness(options) {
  const opts = options || {};
  const harness = await createBoardWebGPUHarness({ fresh: true, configureDevice: opts.configureDevice, fakeDeviceOptions: Object.assign({ timestampQuery: true }, opts.device || {}) });
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  // The fake device drops buffer labels; keep them so tests can find buffers.
  const createBuffer = harness.fake.device.createBuffer;
  harness.fake.device.createBuffer = function(desc) {
    const buffer = createBuffer.call(this, desc);
    buffer.label = desc && desc.label;
    return buffer;
  };
  return { harness, api: harness.env.context.__gosx_scene3d_api };
}

const label = (buffer) => buffer && buffer.label;
const mainPasses = (fake) => fake.state.renderPasses.filter((pass) => pass.descriptor && pass.descriptor.colorAttachments && pass.descriptor.colorAttachments.length > 0);
const shadowPasses = (fake) => fake.state.renderPasses.filter((pass) => pass.descriptor && pass.descriptor.colorAttachments && pass.descriptor.colorAttachments.length === 0);
const cullPasses = (fake) => fake.state.computePasses.filter((pass) => pass.descriptor && pass.descriptor.label === "gosx-gpu-driven-cull");

test("gpu-driven: without the mode the renderer takes its classic path", async () => {
  const { harness, api } = await gpuDrivenHarness();
  await renderFrames(harness, api, sceneState(api, null), 3);
  assert.equal(cullPasses(harness.fake).length, 0);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-active"), "false");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-reason"), "off");
});

test("gpu-driven: frustum mode culls once and draws owned meshes indirectly", async () => {
  const { harness, api } = await gpuDrivenHarness();
  const state = sceneState(api, { occlusion: false, shadowCulling: true });
  await renderFrames(harness, api, state, 1);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-reason"), "warming", "pipelines resolve after the first frame");
  await renderFrames(harness, api, state, 2);
  const fake = harness.fake;
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-active"), "true");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-meshes"), "2", "the translucent mesh stays classic");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-instances"), "42");
  const culls = cullPasses(fake);
  assert.equal(culls.length, 1);
  assert.equal(culls[0].dispatches[0].workgroupCountX, 1);
  const main = mainPasses(fake)[0];
  const indirect = main.drawIndirects.filter((d) => label(d.buffer) === "gosx.gpu-driven.args");
  assert.deepEqual(indirect.map((d) => d.offset), [0, 64], "slot 0 of mesh 0 and mesh 1");
  assert.equal(indirect[0].pipeline.desc.label, "gosx-gpu-driven-pbr");
  const lists = main.vertexBuffers.filter((v) => v.slot === 4 && label(v.buffer) === "gosx.gpu-driven.visible");
  assert.deepEqual(lists.map((v) => [v.offset, v.size]), [[0, 120], [480, 48]]);
  assert.ok(!fake.state.buffers.some((b) => b.label === "gosx.cull.input"), "owned meshes never build a per-mesh cull system");
  assert.equal(main.draws.length >= 1, true, "the translucent mesh still draws directly");
});

test("gpu-driven: shadow lights cull casters and draw from their own slot", async () => {
  const { harness, api } = await gpuDrivenHarness();
  await renderFrames(harness, api, sceneState(api, {}, { light: true }), 3);
  const fake = harness.fake;
  const culls = cullPasses(fake);
  assert.equal(culls.length, 2, "camera + one shadow light");
  const shadow = shadowPasses(fake).find((pass) => pass.drawIndirects.length > 0);
  assert.ok(shadow, "the shadow pass draws owned casters indirectly");
  assert.deepEqual(shadow.drawIndirects.map((d) => d.offset), [32], "slot 2 of the one owned caster (orbs cast no shadow)");
  assert.equal(shadow.drawIndirects[0].pipeline.desc.label, "gosx-gpu-driven-shadow");
});

test("gpu-driven: frustum mode keeps render bundles replaying and uploads only on change", async () => {
  const { harness, api } = await gpuDrivenHarness({ device: { renderBundles: true } });
  const state = sceneState(api, {});
  await renderFrames(harness, api, state, 3);
  const writesBefore = harness.fake.state.writeBufferCalls.filter((c) => label(c.buffer) === "gosx.gpu-driven.instances").length;
  await renderFrames(harness, api, state, 3);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "replayed");
  const writesAfter = harness.fake.state.writeBufferCalls.filter((c) => label(c.buffer) === "gosx.gpu-driven.instances").length;
  assert.equal(writesAfter, writesBefore, "static transforms upload once");
  state.instancedMeshes[1].transforms = transformsFor(12).map((v, i) => (i % 16 === 13 ? v + 1 : v));
  await renderFrames(harness, api, state, 1);
  const changed = harness.fake.state.writeBufferCalls.filter((c) => label(c.buffer) === "gosx.gpu-driven.instances").slice(writesAfter);
  assert.equal(changed.length, 1, "one mesh changed, one range uploaded");
  assert.equal(changed[0].offset, 30 * 96);
});

test("gpu-driven: vertex shader derivations swap every instance input", async () => {
  const { harness, api } = await gpuDrivenHarness();
  await renderFrames(harness, api, sceneState(api, {}), 2);
  const code = (name) => harness.fake.state.shaderModules.find((m) => m.label === name).code;
  const pbr = code("gosx-gpu-driven-pbr-vert");
  assert.ok(pbr.includes("@location(4) gdSlot: u32,"));
  assert.ok(pbr.includes("@group(2) @binding(0) var<storage, read> gdInstances: array<GDInstance>;"));
  assert.ok(pbr.includes("let gdInstance = gdInstances[in.gdSlot];"));
  assert.ok(pbr.includes("out.instanceColor = gdInstance.color;"));
  assert.ok(!pbr.includes("instanceMatrix") && !pbr.includes("@location(8)"));
  const shadow = code("gosx-gpu-driven-shadow-vert");
  assert.ok(shadow.includes("@location(4) gdSlot: u32,"));
  assert.ok(shadow.includes("@group(1) @binding(0) var<storage, read> gdInstances: array<GDInstance>;"));
  assert.ok(shadow.includes("let model = gdInstances[in.gdSlot].model;"));
  assert.ok(!shadow.includes("instanceMatrix"));
  assert.throws(() => api.sceneGPUDrivenPBRVertexWGSL("fn main() {}"), /anchor missing: pbr inputs/);
});

test("gpu-driven: two-phase occlusion splits the main pass around a Hi-Z build", async () => {
  const { harness, api } = await gpuDrivenHarness({ device: { renderBundles: true } });
  await renderFrames(harness, api, sceneState(api, { occlusion: true }), 4);
  const fake = harness.fake;
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-occlusion"), "true");
  const passes = mainPasses(fake);
  assert.equal(passes.length, 2, "early + late");
  const late = passes[1];
  assert.equal(late.descriptor.label, "gosx-gpu-driven-late");
  assert.equal(late.descriptor.colorAttachments[0].loadOp, "load");
  assert.equal(late.descriptor.depthStencilAttachment.depthLoadOp, "load");
  assert.equal(passes[0].ended, true);
  assert.equal(passes[0].descriptor.timestampWrites.endOfPassWriteIndex, undefined, "the early pass only stamps the start");
  assert.equal(late.descriptor.timestampWrites.endOfPassWriteIndex, passes[0].descriptor.timestampWrites.beginningOfPassWriteIndex + 1, "the late pass stamps the end");
  assert.deepEqual(late.drawIndirects.map((d) => d.offset), [16, 80], "slot 1 of both owned meshes");
  const hiz = fake.state.computePasses.find((pass) => pass.descriptor && pass.descriptor.label === "gosx-gpu-driven-hiz");
  assert.ok(hiz, "Hi-Z pass ran");
  assert.equal(hiz.dispatches.length, 6, "seed + 5 downsample levels for 64x64");
  assert.equal(cullPasses(fake).length, 2, "early + late camera culls");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-reason"), "gpu-driven-occlusion");
});

for (const rejected of ["gosx-gpu-driven-cull", "gosx-gpu-driven-pbr"]) {
  test(`gpu-driven: scoped synchronous ${rejected} rejection keeps the classic renderer safe`, async () => {
    const warnings = [], stack = [], message = "Validation Error: rejected " + rejected;
    let attempts = 0;
    const { harness, api } = await gpuDrivenHarness({ configureDevice(device, state, env) {
      env.context.console = { warn: (...args) => warnings.push(args.join(" ")), error() {}, log() {} };
      device.createRenderPipelineAsync = device.createComputePipelineAsync = undefined;
      device.pushErrorScope = filter => stack.push({ filter, error: null });
      device.popErrorScope = () => Promise.resolve(stack.pop().error);
      for (const method of ["createRenderPipeline", "createComputePipeline"]) {
        const create = device[method];
        device[method] = descriptor => {
          assert.equal(stack.at(-1).filter, "validation");
          if (descriptor.label === rejected) {
            attempts++;
            stack.at(-1).error = { message };
          }
          return create(descriptor);
        };
      }
    } });
    await renderFrames(harness, api, sceneState(api, {}), 20);
    assert.equal(attempts, 1);
    assert.equal(warnings.length, 1);
    assert.ok(warnings[0].includes(message));
    assert.equal(stack.length, 0);
    assert.equal(harness.fake.state.renderPasses.some(pass => pass.pipelines.some(pipeline => pipeline.desc.label === rejected)), false);
    assert.equal(harness.fake.state.computePasses.some(pass => pass.pipelines.some(pipeline => pipeline.desc.label === rejected)), false);
    assert.ok(harness.fake.state.renderPasses.some(pass => pass.draws.length > 0 || pass.drawIndexeds.length > 0));
    assert.equal(harness.renderer.getFailureReason(), "");
    harness.renderer.dispose();
  });
}
