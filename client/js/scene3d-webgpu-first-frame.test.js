"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createBoardWebGPUHarness, flushAsyncWork, bundleMeshScene, bundleBoxes, deviceCallSnapshot } = require("./runtime-test-harness.js");

function firstScene(api, lit) {
  if (!lit) return bundleMeshScene(api, bundleBoxes(10, 1));
  const state = api.createSceneState({ scene: {
    materials: [{ id: "m", color: "#8de1ff", roughness: 0.4, metalness: 0.1 }, { id: "glass", color: "#ffffff", opacity: 0.5, blendMode: "alpha" }],
    objects: bundleBoxes(10, 1).map(o => Object.assign(o, { castShadow: true, receiveShadow: true })).concat([{ id: "g", kind: "box", width: 1, height: 1, depth: 1, x: 0, y: 1, z: 0, material: "glass", wireframe: false }]),
  } }, { tier: "full" });
  const bundle = api.createSceneRenderBundle(64, 64, "#000000", { x: 0, y: 0, z: 6, fov: 60, near: 0.05, far: 128 }, api.sceneStateObjectsWithMaterials(state), [], [], [], [], {}, 0, [], [], [], [], [], 0, false);
  bundle.lights = [{ kind: "directional", directionX: -1, directionY: -2, directionZ: -1, intensity: 1, castShadow: true, color: "#ffffff" }];
  bundle.postEffects = [{ kind: "bloom", threshold: 0.8, intensity: 0.5 }, { kind: "toneMapping" }];
  return bundle;
}

async function startup(lit, asyncPipelines = true, edit = () => {}, fakeDeviceOptions = {}) {
  const waiting = [], asyncLabels = [];
  const h = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions, configureDevice(device) {
    device.popErrorScope = () => new Promise(resolve => waiting.push(() => resolve(null)));
    if (asyncPipelines) for (const kind of ["Render", "Compute"]) {
      const create = device["create" + kind + "Pipeline"].bind(device);
      device["create" + kind + "PipelineAsync"] = desc => {
        asyncLabels.push(desc.label);
        const pipeline = create(desc);
        return new Promise(resolve => waiting.push(() => resolve(pipeline)));
      };
    }
  } });
  const context = h.canvas.getContext("webgpu"), acquire = context.getCurrentTexture;
  let acquired = 0, pending = 0, partial = 0;
  context.getCurrentTexture = () => { acquired++; return acquire(); };
  const construction = { ...deviceCallSnapshot(h.fake), buffers: h.fake.state.buffers.length };
  assert.equal(h.renderer.diagnostics().initialPipelinesReady, false, "initial readiness needs the prepared bundle");
  let scene = firstScene(h.env.context.__gosx_scene3d_api, lit);
  scene = edit(scene, h.env.context.__gosx_scene3d_api) || scene;
  let result;
  for (let i = 0; i < 20; i++) {
    const before = acquired;
    result = h.renderer.render(scene, { width: 64, height: 64 }, { nowMS: i * 16, active: true });
    if (result === "submitted") { assert.equal(h.renderer.diagnostics().initialPipelinesReady, true); break; }
    if (result === "pending") { assert.equal(h.renderer.diagnostics().initialPipelinesReady, false); pending++; partial += acquired - before; }
    while (waiting.length) waiting.shift()();
    await flushAsyncWork();
  }
  const counts = { ...deviceCallSnapshot(h.fake), buffers: h.fake.state.buffers.length, pending, acquired, partial, bundleEncodes: h.fake.state.renderBundleEncoders.length };
  h.renderer.dispose();
  return { result, counts, construction, asyncLabels };
}

for (const lit of [false, true]) test(`${lit ? "lit alpha shadows bloom and tone mapping" : "10 boxes"} prepares all first-frame pipelines in one pending render`, async t => {
  const evidence = await startup(lit);
  t.diagnostic(JSON.stringify(evidence));
  assert.equal(evidence.result, "submitted");
  assert.ok(evidence.counts.pending <= 1, `pending renders: ${evidence.counts.pending}`);
  assert.equal(evidence.counts.partial, 0, "pending renders must never acquire a canvas texture");
  assert.equal(evidence.counts.computePipelines, 0, "non-water rigid scenes create no compute pipelines");
  assert.equal(evidence.construction.renderPipelines, 0);
  assert.equal(evidence.construction.computePipelines, 0);
  assert.ok(evidence.asyncLabels.includes("gosx-pbr-opaque"));
});

test("synchronous fallback never presents a partial frame", async t => {
  const evidence = await startup(true, false);
  t.diagnostic(JSON.stringify(evidence));
  assert.equal(evidence.counts.partial, 0);
});

test("a non-water renderer compiles no water, skin or morph shaders", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    const scene = firstScene(h.env.context.__gosx_scene3d_api, false);
    for (let i = 0; i < 3; i++) { h.renderer.render(scene, { width: 64, height: 64 }); await flushAsyncWork(); }
    assert.deepEqual(h.fake.state.shaderModules.filter(m => /water|elio|morph/.test(m.label)).map(m => m.label), []);
    assert.equal(h.fake.state.computePipelines.length, 0);
  } finally { h.renderer.dispose(); }
});

for (const label of ["gosx-pbr-opaque", "gosx-post-toneMapping"]) test(`async rejection of ${label} follows pipeline recovery once`, async () => {
  const h = await createBoardWebGPUHarness({ fresh: true, configureDevice(device) {
    const create = device.createRenderPipeline.bind(device);
    device.createRenderPipelineAsync = desc => desc.label === label ? Promise.reject(new Error("async compile rejected")) : Promise.resolve(create(desc));
  } });
  try {
    const scene = firstScene(h.env.context.__gosx_scene3d_api, true);
    for (let i = 0; i < 6; i++) { h.renderer.render(scene, { width: 64, height: 64 }); await flushAsyncWork(); }
    const d = h.renderer.diagnostics();
    assert.equal(d.pipelineFailures.length, 1);
    assert.equal(d.pipelineFailures[0].label, label);
    assert.equal(d.pipelineFailures[0].message, "async compile rejected");
    assert.equal(d.pipelinePending, 0);
    if (label.includes("post")) {
      assert.deepEqual(Array.from(d.disabledPipelinePasses), ["toneMapping"]);
      assert.equal(h.renderer.getFailureReason(), "");
      assert.ok(h.fake.state.renderPasses.some(pass => pass.draws.length));
    } else assert.equal(h.renderer.getFailureReason(), "webgpu-pipeline-failed");
  } finally { h.renderer.dispose(); }
});

test("a pipeline discovered inside the main pass never acquires or submits the canvas", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const context = h.canvas.getContext("webgpu"), acquire = context.getCurrentTexture;
  let acquired = 0;
  context.getCurrentTexture = () => { acquired++; return acquire(); };
  try {
    const scene = firstScene(h.env.context.__gosx_scene3d_api, false);
    for (let i = 0; i < 3; i++) { h.renderer.render(scene, { width: 64, height: 64 }); await flushAsyncWork(); }
    scene.environment = { sky: { mode: "physical" } };
    const waiting = [];
    h.fake.device.popErrorScope = () => new Promise(resolve => waiting.push(resolve));
    const before = acquired, passes = h.fake.state.renderPasses.length;
    assert.equal(h.renderer.render(scene, { width: 64, height: 64 }), "pending");
    assert.equal(acquired, before);
    assert.equal(h.renderer.diagnostics().initialPipelinesReady, true, "initial readiness is a one-time milestone during later compilation");
    assert.equal(h.fake.state.renderPasses.length, passes, "the canvas main pass must not reach the raw encoder");
    for (const resolve of waiting) resolve(null);
    await flushAsyncWork();
    assert.equal(h.renderer.render(scene, { width: 64, height: 64 }), "submitted");
    assert.equal(acquired, before + 1);
  } finally { h.renderer.dispose(); }
});

test("reflection capture binds the acquired canvas view after deferred encoding", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    const create = h.fake.device.createRenderPipeline.bind(h.fake.device);
    h.fake.device.createRenderPipeline = desc => Object.assign(create(desc), { getBindGroupLayout: () => h.fake.device.createBindGroupLayout({ entries: [] }) });
    const group = h.fake.device.createBindGroup.bind(h.fake.device);
    h.fake.device.createBindGroup = desc => {
      for (const entry of desc.entries) if (entry.binding === 0 && !entry.resource.buffer && desc.entries[1]?.resource?.__kind === "textureView") {
        assert.ok(entry.resource.__kind, "native texture bindings must receive a real view");
      }
      return group(desc);
    };
    const scene = firstScene(h.env.context.__gosx_scene3d_api, false);
    scene.environment = { sky: { mode: "physical" }, ocean: { reflections: { mode: "ssr+planar", resolution: 0.5, strength: 0.8 } } };
    h.renderer.render(scene, { width: 64, height: 64 });
    assert.equal(h.renderer.getFailureReason(), "");
    const capture = h.fake.state.renderPasses.find(pass => pass.descriptor.label === "gosx-reflection-capture");
    assert.ok(capture);
    assert.equal(capture.bindGroups[0].group.desc.entries[0].resource.__kind, "canvasTextureView");
  } finally { h.renderer.dispose(); }
});

for (const variant of ["taa", "mip-bloom", "detail"]) test(`${variant} pipelines are ready after one pending render`, async () => {
  const evidence = await startup(true, true, scene => {
    if (variant === "taa") scene.postEffects.push({ kind: "taa" });
    if (variant === "mip-bloom") scene.postEffects[0].mode = "mip";
    if (variant === "detail") scene.materials[0].detail = { ground: { scale: 3 }, fadeStart: 5, fadeEnd: 12 };
  });
  assert.equal(evidence.result, "submitted");
  assert.ok(evidence.counts.pending <= 1, `pending renders: ${evidence.counts.pending}`);
  assert.equal(evidence.counts.partial, 0);
});


test("a reflected retained render bundle prepares only its drawn winding variant", async () => {
  const evidence = await startup(false, true, (_scene, api) => {
    const scene = bundleMeshScene(api, [{
    id: "reflected", kind: "box", wireframe: false, color: "#f6a44c", pickable: true,
    parentMatrix: [-2, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1],
    vertices: { positions: [-0.4, -0.4, 0, 0.4, -0.4, 0, 0, 0.4, 0], normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
      uvs: [0, 0, 1, 0, 0.5, 1], tangents: [1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1], indices: [0, 1, 2], count: 3, immutable: true, revision: 0 },
    }], { retainedGeometry: true });
    scene.meshObjects[0].modelMatrix = new Float32Array([-2, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
    assert.equal(scene.meshObjects[0].directVertices, true);
    return scene;
  }, { renderBundles: true });
  assert.equal(evidence.result, "submitted");
  assert.equal(evidence.counts.partial, 0, "a late unused default variant must not acquire the canvas");
  assert.equal(evidence.counts.pending, 1);
  assert.equal(evidence.counts.renderPipelines, 1);
  assert.ok(evidence.counts.bundleEncodes > 0, "the render-bundle path must run");
});
