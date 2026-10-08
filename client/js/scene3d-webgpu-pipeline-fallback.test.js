"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  createBoardWebGPUHarness, flushAsyncWork, createContext, FakeElement, bundleMeshScene,
  waterPerfShapeEntry, waterSeedSelenaFixture, waterSurfaceSelenaFixture, waterCausticsSelenaFixture,
  installManualRAF, installManualTimers, runScript, bootstrapRuntimeSource,
  bootstrapFeatureScene3DSource, bootstrapFeatureScene3DWebGLSource, bootstrapFeatureScene3DWebGPUSource, bootstrapFeatureEnginesSource,
} = require("./runtime-test-harness.js");

const validationMessage = "RenderPipeline with 'gosx-post' label is invalid\nValidation Error in CommandEncoder::finish, label 'gosx-frame'\n" + "validation detail ".repeat(30);

async function failureHarness(pattern, mode = "scope", options = {}) {
  const events = [], warnings = [], stack = [], listeners = new Set();
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    ...options,
    configureDevice(device, state, env) {
      env.context.__gosx_emit = (level, category, message, detail) => events.push({ message, detail });
      env.context.console = { warn: message => warnings.push(message), error() {}, log() {} };
      device.pushErrorScope = (filter) => stack.push({ filter, error: null });
      device.popErrorScope = () => {
        const error = stack.pop().error;
        return error && options.deferValidation ? options.deferValidation(error) : Promise.resolve(error);
      };
      device.addEventListener = (kind, callback) => { if (kind === "uncapturederror") listeners.add(callback); };
      device.removeEventListener = (kind, callback) => listeners.delete(callback);
      for (const method of ["createRenderPipeline", "createComputePipeline"]) {
        const create = device[method];
        device[method] = (descriptor) => {
          assert.equal(stack.at(-1).filter, "validation", "every pipeline must have its own validation scope");
          if (pattern.test(descriptor.label)) {
            if (mode === "throw") throw new Error(validationMessage);
            stack.at(-1).error = { message: validationMessage };
          }
          return create(descriptor);
        };
      }
    },
  });
  harness.env.context.__gosx_emit = (level, category, message, detail) => events.push({ message, detail });
  harness.env.context.console = { warn: (message) => warnings.push(message), error() {}, log() {} };
  await flushAsyncWork();
  const scene = bundleMeshScene(harness.env.context.__gosx_scene3d_api,
    [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff", material: "m", wireframe: false }]);
  return { ...harness, events, warnings, listeners, stack, scene };
}

async function frames(h, count = 20) {
  for (let i = 0; i < count; i++) {
    assert.doesNotThrow(() => h.renderer.render(h.scene, { width: 64, height: 64 }, { nowMS: i * 17, active: true }));
    await flushAsyncWork();
  }
}

for (const [label, kind] of [
  ["toneMapping", "toneMapping"], ["bloomBright", "bloom"], ["contactShadows", "contactShadows"],
]) {
  test(`WebGPU rejects ${kind} once, keeps other effects and rendering, and publishes the exact error`, async () => {
    const h = await failureHarness(new RegExp("^gosx-post-" + label + "$"));
    h.scene.postEffects = [{ kind }, { kind: "fxaa" }];
    await frames(h);
    assert.equal(h.renderer.getFailureReason(), "");
    const diagnostic = h.renderer.diagnostics();
    assert.deepEqual(Array.from(diagnostic.disabledPipelinePasses), [kind]);
    assert.equal(diagnostic.pipelineFailures[0].message, validationMessage);
    assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 1);
    assert.equal(h.warnings.length, 1);
    assert.equal(h.events.find(event => event.message === "pipeline-failed").detail.error, validationMessage);
    assert.ok(h.fake.state.renderPasses.some(pass => pass.draws.some(draw => draw.pipeline && draw.pipeline.desc.label === "gosx-post-fxaa")));
    assert.equal(h.fake.state.renderPasses.some(pass => pass.pipelines.some(pipeline => pipeline.desc.label === "gosx-post-" + label)), false);
    assert.ok(h.fake.state.submitCount > 10, "raw scene and remaining effects must keep presenting frames");
    assert.equal(h.stack.length, 0, "aborted frames must pop the frame scope");
    h.renderer.dispose();
  });
}

for (const mode of ["scope", "throw"]) {
  test(`WebGPU core pipeline ${mode} failure stops encoding and requests WebGL2 once`, async () => {
    const h = await failureHarness(/^gosx-pbr-opaque$/, mode);
    await frames(h);
    assert.equal(h.renderer.getFailureReason(), "webgpu-pipeline-failed");
    assert.equal(h.renderer.diagnostics().pipelineCoreError, validationMessage);
    assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 1);
    assert.equal(h.events.find(event => event.message === "pipeline-failed").detail.action, "webgl2-fallback");
    assert.equal(h.warnings.length, 1);
    assert.equal(h.fake.state.renderPasses.some(pass => pass.pipelines.some(pipeline => /^gosx-pbr-opaque$/.test(pipeline.desc.label))), false);
    assert.equal(h.stack.length, 0);
    h.renderer.dispose();
    assert.equal(h.listeners.size, 0);
  });
}

test("WebGPU never binds pending pipelines and resumes after validation", async () => {
  const h = await failureHarness(/does-not-match/);
  h.renderer.render(h.scene, { width: 64, height: 64 });
  assert.equal(h.fake.state.renderPasses.some(pass => pass.pipelines.length > 0), false);
  assert.ok(h.fake.state.submitCount > 0, "valid work encoded before a pending pipeline must reach the GPU");
  assert.ok(h.fake.state.renderPasses.every(pass => pass.ended), "the suspended render pass must close before submission");
  await flushAsyncWork();
  await frames(h, 4);
  assert.ok(h.fake.state.renderPasses.some(pass => pass.draws.length > 0));
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 0);
  assert.equal(h.env.fetchCalls.filter(call => call.url.includes("pipeline-recovery")).length, 0);
  h.renderer.dispose();
});

test("a newly enabled post pass preserves water simulation commands while its pipelines validate", async () => {
  const h = await failureHarness(/does-not-match/);
  const api = h.env.context.__gosx_scene3d_api;
  h.scene.waterSystems = api.createSceneState({ scene: { waterSystems: [{ id: "pending-water", grid: 4, seedDrops: 1 }] } }).waterSystems;
  await frames(h, 8);
  const dispatches = label => h.fake.state.computePasses.flatMap(pass => pass.dispatches).filter(dispatch => dispatch.pipeline.desc.label === label).length;
  assert.equal(dispatches("gosx-water-seed-drops"), 1);
  const beforeSteps = dispatches("gosx-water-step"), beforeSubmits = h.fake.state.submitCount;
  h.scene.postEffects = [{ kind: "fxaa" }];
  h.renderer.render(h.scene, { width: 64, height: 64 }, { nowMS: 170, active: true });
  assert.ok(dispatches("gosx-water-step") > beforeSteps, "the live simulation advances before a newly required pipeline pauses the frame");
  assert.ok(h.fake.state.submitCount > beforeSubmits, "the advanced simulation must reach the GPU before retrying");
  assert.ok(h.fake.state.renderPasses.every(pass => pass.ended));
  await flushAsyncWork();
  for (let i = 0; i < 8; i++) {
    h.renderer.render(h.scene, { width: 64, height: 64 }, { nowMS: 187 + i * 17, active: true });
    await flushAsyncWork();
  }
  assert.equal(dispatches("gosx-water-seed-drops"), 1, "retries must preserve the simulation seed state");
  assert.ok(h.fake.state.renderPasses.some(pass => pass.draws.some(draw => draw.pipeline && draw.pipeline.desc.label === "gosx-post-fxaa")));
  assert.equal(h.renderer.getFailureReason(), "");
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 0);
  h.renderer.dispose();
});

test("uncaptured optional and unexpected core errors degrade once and detach on disposal", async () => {
  const h = await failureHarness(/does-not-match/);
  const listener = Array.from(h.listeners)[0];
  let prevented = 0;
  for (let i = 0; i < 5; i++) listener({ error: { message: validationMessage }, preventDefault() { prevented++; } });
  await flushAsyncWork();
  assert.deepEqual(Array.from(h.renderer.diagnostics().disabledPipelinePasses), ["post"]);
  await frames(h);
  assert.equal(h.renderer.getFailureReason(), "");
  assert.ok(h.fake.state.submitCount > 10);
  const unexpected = "Validation Error: unexpected attachment mismatch";
  for (let i = 0; i < 5; i++) listener({ error: { message: unexpected }, preventDefault() { prevented++; } });
  await flushAsyncWork();
  assert.equal(h.renderer.getFailureReason(), "webgpu-pipeline-failed");
  assert.equal(h.renderer.diagnostics().pipelineCoreError, unexpected);
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 2);
  assert.equal(h.warnings.length, 2);
  assert.equal(prevented, 10);
  h.renderer.dispose();
  assert.equal(h.listeners.size, 0);
  listener({ error: { message: "late error" } });
  assert.equal(h.warnings.length, 2);
});

test("the honesty gate swaps a core-failed WebGPU scene to WebGL2 on a replacement canvas", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "pipeline-fallback-scene";
  const events = [];
  let diagnostics = { ready: true, pipelineCoreError: "" };
  let disposed = 0;
  const env = createContext({
    elements: [mount], enableWebGPU: true, enableWebGL2: true,
    navigatorGPU: {
      requestAdapter: async () => ({ requestDevice: async () => ({ lost: new Promise(() => {}), features: new Set(), limits: {} }) }),
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    fetchRoutes: { "/gosx/bootstrap-feature-scene3d-webgl.js": { text: bootstrapFeatureScene3DWebGLSource }, "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource } },
    manifest: { runtime: { path: "/gosx/runtime.wasm" }, engines: [{
      id: "pipeline-fallback", component: "GoSXScene3D", kind: "surface", mountId: mount.id, jsExport: "GoSXScene3D",
      props: { width: 320, height: 180, preferWebGPU: true, autoRotate: true,
        scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] } },
    }] },
  });
  const timers = installManualTimers(env.context), raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  env.context.__gosx_emit = (level, category, message, detail) => events.push({ message, detail });
  env.context.__gosx_scene3d_webgpu_api = { createRenderer(canvas) {
    canvas.__webgpuClaimed = true;
    return { kind: "webgpu", render() {}, diagnostics: () => diagnostics, dispose() { disposed++; } };
  } };
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  timers.runDelay(0);
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  const firstCanvas = mount.children[0];
  diagnostics = { ready: true, pipelineCoreError: validationMessage };
  timers.runInterval(2000);
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgpu-pipeline-failed");
  assert.notEqual(mount.children[0], firstCanvas);
  assert.ok(mount.children[0].contextCalls.some(call => call.kind === "webgl2"));
  for (let i = 0; i < 5; i++) timers.runInterval(2000);
  assert.equal(disposed, 1);
  assert.equal(events.filter(event => event.message === "render-watchdog-recovery").length, 1);
});

test("an invalid core compute pipeline requests fallback without dispatching", async () => {
  const h = await failureHarness(/^gosx-water-step$/);
  await frames(h, 5);
  assert.equal(h.renderer.getFailureReason(), "webgpu-pipeline-failed");
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 1);
  assert.equal(h.renderer.diagnostics().pipelineFailures[0].label, "gosx-water-step");
  assert.equal(h.fake.state.computePasses.length, 0);
  h.renderer.dispose();
});

test("reflection capture rejection disables reflections and keeps the ocean rendering", async () => {
  const h = await failureHarness(/^gosx-reflection-capture$/);
  h.scene.environment = { sky: { mode: "physical" }, ocean: { reflections: { mode: "ssr+planar", resolution: 0.5, strength: 0.8 } } };
  await frames(h);
  assert.equal(h.renderer.getFailureReason(), "");
  assert.deepEqual(Array.from(h.renderer.diagnostics().disabledPipelinePasses), ["reflections"]);
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 1);
  assert.ok(h.fake.state.renderPasses.some(pass => pass.draws.some(draw => draw.pipeline && draw.pipeline.desc.label === "gosx-ocean")));
  assert.equal(h.fake.state.renderPasses.some(pass => pass.pipelines.some(pipeline => pipeline.desc.label === "gosx-reflection-capture")), false);
  h.renderer.dispose();
});

test("pipeline settlement wakes a static scene through its resource-ready event", async () => {
  const h = await failureHarness(/does-not-match/);
  const events = [];
  h.canvas.dispatchEvent = event => events.push(event.type);
  h.renderer.render(h.scene, { width: 64, height: 64 });
  await flushAsyncWork();
  assert.ok(events.includes("gosx:scene3d:resource-ready"));
  h.renderer.dispose();
});

test("the capability probe rejects asynchronously invalid canvas configuration", async () => {
  const error = "Validation Error: canvas format unsupported";
  const env = createContext({ enableWebGPU: true, navigatorGPU: {
    requestAdapter: async () => ({ requestDevice: async () => ({
      features: new Set(), limits: {}, lost: new Promise(() => {}),
      pushErrorScope(filter) { assert.equal(filter, "validation"); },
      popErrorScope() { return Promise.resolve({ message: error }); },
    }) }),
    getPreferredCanvasFormat: () => "rgba8unorm",
  } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  const probe = env.context.__gosx_scene3d_webgpu_probe();
  assert.equal(probe.ready, false);
  assert.match(probe.error, /canvas configure failed/);
  assert.ok(probe.error.includes(error));
});

test("lazy authored water pipelines preserve the initial seed across validation waits", async () => {
  const h = await failureHarness(/does-not-match/);
  const api = h.env.context.__gosx_scene3d_api;
  h.scene.waterSystems = api.createSceneState({ scene: { waterSystems: [Object.assign(waterPerfShapeEntry(false), { resolution: 16, surfaceResolution: 3, seedDrops: 1 })] } }).waterSystems;
  await frames(h, 30);
  const seeded = h.fake.state.computePasses.flatMap(pass => pass.dispatches).filter(dispatch => dispatch.pipeline.desc.label === "gosx-selena-compute-" + waterSeedSelenaFixture.layout.material);
  assert.equal(h.renderer.getFailureReason(), "", JSON.stringify(h.renderer.diagnostics().pipelineFailures));
  assert.equal(seeded.length, 1, "validation must settle before the initial seed is marked consumed");
  for (const fixture of [waterSurfaceSelenaFixture, waterCausticsSelenaFixture]) {
    assert.ok(h.fake.state.renderPasses.some(pass => pass.draws.concat(pass.drawIndexeds).some(draw => draw.pipeline && draw.pipeline.desc.fragment?.module?.code?.trim() === fixture.wgsl.trim())),
      fixture.layout.material + " must draw after validation settles");
  }
  assert.equal(h.renderer.getFailureReason(), "");
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 0);
  h.renderer.dispose();
});

for (const mode of ["scope", "throw"]) {
  test(`WebGPU ${mode} pipeline rejection reaches the mount's real WebGL2 fallback`, async () => {
    const h = await failureHarness(/^gosx-pbr-opaque$/, mode);
    h.renderer.dispose();
    const mount = new FakeElement("div", null);
    mount.id = "real-pipeline-fallback";
    const env = createContext({
      elements: [mount], enableWebGPU: true, enableWebGL2: true,
      navigatorGPU: {
        requestAdapter: async () => ({ requestDevice: async () => h.fake.device }),
        getPreferredCanvasFormat: () => "rgba8unorm",
      },
      fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource } },
      manifest: { runtime: { path: "/gosx/runtime.wasm" }, engines: [{
        id: "real-pipeline-fallback", component: "GoSXScene3D", kind: "surface", mountId: mount.id, jsExport: "GoSXScene3D",
        props: { width: 320, height: 180, preferWebGPU: true, autoRotate: true,
          scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] } },
      }] },
    });
    for (const key of ["GPUBufferUsage", "GPUTextureUsage", "GPUShaderStage"]) env.context[key] = h.env.context[key];
    const timers = installManualTimers(env.context), raf = installManualRAF(env.context);
    runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
    env.context.__gosx_emit = (level, category, message, detail) => h.events.push({ message, detail });
    runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
    runScript(bootstrapFeatureScene3DWebGPUSource, env.context, "bootstrap-feature-scene3d-webgpu.js");
    const factory = env.context.__gosx_scene3d_webgpu_api.createRenderer;
    let firstCanvas, activeRenderer;
    env.context.__gosx_scene3d_webgpu_api.createRenderer = (canvas, options) => { firstCanvas = canvas; activeRenderer = factory(canvas, options); return activeRenderer; };
    timers.runDelay(0);
    for (let frame = 0; frame < 20; frame++) {
      if (activeRenderer) activeRenderer.render(h.scene, { width: 320, height: 180 });
      raf.flush(frame * 17); await flushAsyncWork(); timers.runInterval(2000);
    }
    const handle = mount.__gosxScene3DHandle;
    try {
      assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
      assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgpu-pipeline-failed");
      assert.notEqual(mount.children[0], firstCanvas);
      assert.ok(mount.children[0].contextCalls.some(call => call.kind === "webgl2"));
      assert.equal(env.fetchCalls.filter(call => call.url.includes("pipeline-recovery")).length, 0);
      assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 1);
      assert.equal(h.fake.state.renderPasses.some(pass => pass.pipelines.some(pipeline => pipeline.desc.label === "gosx-pbr-opaque")), false);
    } finally { if (handle) handle.dispose(); }
  });
}

test("WebGPU recovery works without a separate recovery asset", async () => {
  const h = await failureHarness(/^gosx-pbr-opaque$/, "scope", {
    fetchRoutes: { "/gosx/bootstrap-feature-scene3d-pipeline-recovery.js": { text: "" } },
  });
  await frames(h);
  assert.equal(h.renderer.getFailureReason(), "webgpu-pipeline-failed");
  assert.equal(h.renderer.diagnostics().pipelineCoreError, validationMessage);
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 1);
  assert.equal(h.env.fetchCalls.filter(call => call.url.includes("pipeline-recovery")).length, 0);
  h.renderer.dispose();
});

test("core recovery remains available without the shared render-truth API", async () => {
  const h = await failureHarness(/^gosx-pbr-opaque$/);
  delete h.env.context.__gosx_scene3d_render_truth_api;
  await frames(h);
  assert.equal(h.renderer.getFailureReason(), "webgpu-pipeline-failed");
  assert.equal(h.renderer.diagnostics().pipelineCoreError, validationMessage);
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 1);
  h.renderer.dispose();
});

test("disposing before validation settles leaves the retired renderer silent", async () => {
  const pending = [];
  const h = await failureHarness(/^gosx-pbr-opaque$/, "scope", {
    deferValidation: error => new Promise(resolve => pending.push(() => resolve(error))),
  });
  h.renderer.render(h.scene, { width: 64, height: 64 });
  assert.ok(pending.length > 0, "a rejected pipeline must still be waiting for validation");
  assert.ok(h.renderer.diagnostics().pipelinePending > 0);
  h.renderer.dispose();
  for (const settle of pending) settle();
  await flushAsyncWork();
  assert.equal(h.events.filter(event => event.message === "pipeline-failed").length, 0);
  assert.equal(h.warnings.length, 0);
});
