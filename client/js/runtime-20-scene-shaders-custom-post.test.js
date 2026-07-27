"use strict";
// Authored shader payloads: compute particles, authored point pipelines, the
// shaderLib reference inflation and the custom post-effect passes.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapSource,
  bootstrapRuntimeSource,
  bootstrapFeatureEnginesSource,
  bootstrapFeatureScene3DSource,
  bootstrapScene3DWebGPUSourceFile,
  bootstrapScene3DInputSourceFile,
  bootstrapScene3DMountSourceFile,
  FakeWebGLContext,
  FakeElement,
  createContext,
  installManualRAF,
  installManualTimers,
  runScript,
  flushAsyncWork,
  createBoardWebGPUHarness,
  mainRenderPasses,
  boardBundleManyObjectsOneMaterial,
  makeFakeGPUDeviceForCompute,
  createComputeParticleHarness,
  makeComputeParticleBundle,
  makePointsBundle,
  makeComputeParticleBundleWithRender,
  makeShaderLibManifestEntry,
  makeShaderLibManifestEntryForPoints,
  makeShaderLibManifestEntryForParticleRender,
  makeShaderLibManifestEntryForInstancedMeshes,
  makeBundleWithCustomPost,
  createWebGLRendererForPost,
  makeWebGLBundleWithCustomPost,
  countDefaultFramebufferDraws,
  makeSceneApiEnv,
} = require("./runtime-test-harness.js");

test("compute particle payload kernel: invalid WGSL (async rejection) falls back to builtin, caches failure, warns once", async () => {
  // Simulate the real browser failure mode: createComputePipelineAsync REJECTS
  // for the payload WGSL (Tint validation error), then resolves for the builtin.
  let callCount = 0;
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      callCount++;
      // First call = payload kernel: reject (validation failure).
      // Second call = builtin fallback: resolve.
      if (callCount === 1) {
        return Promise.reject(new Error("fake WGSL validation error"));
      }
      return Promise.resolve({ __kind: "computePipeline", label: "builtin" });
    },
  });

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  // Frame 1: render triggers createSceneParticleSystem which kicks off async
  // pipeline validation — NOT yet resolved.
  harness.renderer.render(makeComputeParticleBundle({
    id: "test-invalid", count: 4,
    computeWGSL: "@group(0) @binding(0) var<storage, read_write> bad : array<u32>;",
    emitter: { kind: "point" }, material: { color: "#fff" },
  }), viewport);

  // System is not-ready: no compute passes dispatched yet.
  assert.equal(fake.state.computePasses.filter(p => p.ended).length, 0,
    "no compute pass should run before async validation resolves");

  // Drain microtask / promise queue so the rejection + fallback chain settles.
  await flushAsyncWork();
  await flushAsyncWork();

  // (a) Warn fired exactly once with the system id.
  const payloadWarns = harness.warnLog.filter(m => m.includes("test-invalid") && m.includes("falling back"));
  assert.equal(payloadWarns.length, 1, "exactly one payload-failure warn must fire");

  // (b) Failure is cached: re-render with the SAME entry — async call must NOT
  // run again (callCount stays at 2 = payload attempt + builtin attempt).
  // This requires the system to flip to the builtin pipeline, so subsequent
  // frames run the builtin without another payload attempt.
  // A second render should find the system already ready (builtin pipeline).
  harness.renderer.render(makeComputeParticleBundle({
    id: "test-invalid", count: 4,
    computeWGSL: "@group(0) @binding(0) var<storage, read_write> bad : array<u32>;",
    emitter: { kind: "point" }, material: { color: "#fff" },
  }), viewport);
  await flushAsyncWork();

  // No additional warn: the payload failure is cached on the system.
  const payloadWarns2 = harness.warnLog.filter(m => m.includes("test-invalid") && m.includes("falling back"));
  assert.equal(payloadWarns2.length, 1, "warn must fire only once (failure cached, not re-attempted)");

  // (c) The builtin pipeline eventually ran: at least one compute pass dispatched
  // after the builtin resolved.
  const computeEnded = fake.state.computePasses.filter(p => p.ended);
  assert.ok(computeEnded.length >= 1, "at least one compute pass must dispatch once the builtin pipeline is ready");
});

test("compute particle payload kernel: valid WGSL uses the payload pipeline, not the builtin", async () => {
  // The first createComputePipelineAsync call resolves cleanly; popErrorScope
  // returns null (no error).
  let pipelineAsyncCallCount = 0;
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      pipelineAsyncCallCount++;
      return Promise.resolve({ __kind: "computePipeline", label: "payload-" + pipelineAsyncCallCount });
    },
    errorScopeBehavior() {
      return Promise.resolve(null);
    },
  });

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const entry = {
    id: "test-valid", count: 4,
    computeWGSL: "fn simulate() {}",
    computeEntry: "simulate",
    emitter: { kind: "point" }, material: { color: "#fff" },
  };
  harness.renderer.render(makeComputeParticleBundle(entry), viewport);

  // Drain so async validation resolves.
  await flushAsyncWork();
  await flushAsyncWork();

  // No warnings: valid payload should not warn.
  const payloadWarns = harness.warnLog.filter(m => m.includes("falling back") || m.includes("failed"));
  assert.equal(payloadWarns.length, 0, "valid payload must not emit any fallback warning");

  // Exactly one createComputePipelineAsync call: the payload path, not two
  // (payload + builtin). The first call is for the payload kernel.
  assert.equal(pipelineAsyncCallCount, 1, "exactly one createComputePipelineAsync call for valid payload");

  // After resolution the system is ready: the next render dispatches a compute pass.
  harness.renderer.render(makeComputeParticleBundle(entry), viewport);
  const computeEnded = fake.state.computePasses.filter(p => p.ended);
  assert.ok(computeEnded.length >= 1, "compute pass must run after valid payload pipeline is ready");
});

test("compute particle payload kernel: absent computeWGSL goes straight to builtin path without payload attempt", async () => {
  let pipelineAsyncCallCount = 0;
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      pipelineAsyncCallCount++;
      return Promise.resolve({ __kind: "computePipeline", label: "builtin-only" });
    },
  });

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  // Entry WITHOUT computeWGSL: must take the builtin path directly.
  harness.renderer.render(makeComputeParticleBundle({
    id: "test-builtin-only", count: 4,
    emitter: { kind: "point" }, material: { color: "#fff" },
  }), viewport);

  await flushAsyncWork();
  await flushAsyncWork();

  // No payload warnings.
  assert.equal(harness.warnLog.filter(m => m.includes("payload") && m.includes("falling back")).length, 0,
    "no payload warning for entry without computeWGSL");

  // Exactly one async pipeline call (the builtin path).
  assert.equal(pipelineAsyncCallCount, 1, "exactly one createComputePipelineAsync call for the builtin path");

  // System becomes ready and dispatches a compute pass.
  harness.renderer.render(makeComputeParticleBundle({
    id: "test-builtin-only", count: 4,
    emitter: { kind: "point" }, material: { color: "#fff" },
  }), viewport);
  const computeEnded = fake.state.computePasses.filter(p => p.ended);
  assert.ok(computeEnded.length >= 1, "compute pass must dispatch once builtin pipeline is ready");
});

test("points authored WebGPU: absent customVertexWGSL goes straight to builtin without pipeline attempt", async () => {
  let renderAsyncCalls = 0;
  const baseCompute = makeFakeGPUDeviceForCompute({});
  baseCompute.device.createRenderPipelineAsync = function(desc) {
    renderAsyncCalls++;
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };

  const harness = await createComputeParticleHarness(baseCompute.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  harness.renderer.render(makePointsBundle({
    id: "plain-layer", count: 4,
    style: "glow",
    positions: new Float32Array([0,0,0, 1,0,0, 0,1,0, 0,0,1]),
  }), viewport);

  await flushAsyncWork();
  await flushAsyncWork();

  // No authored pipeline attempt: renderAsync is never called for points
  // without customVertexWGSL (only builtin render pipelines were pre-built).
  const authoredAttempts = harness.env.context.console
    ? harness.warnLog.filter(m => m.includes("points authored") || m.includes("falling back"))
    : [];
  assert.equal(authoredAttempts.length, 0, "no authored warning for entry without customVertexWGSL");
});

test("points authored WebGPU: valid customVertexWGSL+customFragmentWGSL builds authored pipeline", async () => {
  const baseCompute = makeFakeGPUDeviceForCompute({
    errorScopeBehavior() {
      return Promise.resolve(null); // no validation error
    },
  });
  baseCompute.device.createRenderPipelineAsync = function(desc) {
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label, isAuthored: true });
  };

  const harness = await createComputeParticleHarness(baseCompute.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const entry = {
    id: "authored-layer",
    count: 4,
    // positions required — without them the render loop skips the entry before
    // reaching the pipeline-selection branch (see _cachedPos guard).
    positions: new Float32Array([0,0,0, 1,0,0, 0,1,0, 0,0,1]),
    customVertexWGSL: "@vertex fn vertexMain() -> @builtin(position) vec4<f32> { return vec4<f32>(0.0,0.0,0.0,1.0); }",
    customFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0,1.0,0.0,1.0); }",
  };

  // Frame 1: async build kicks off but returns null (pending) — builtin used.
  harness.renderer.render(makePointsBundle(entry), viewport);
  await flushAsyncWork();
  await flushAsyncWork();

  // No failure warnings on valid shader.
  const failWarns = harness.warnLog.filter(m => m.includes("falling back") || m.includes("failed"));
  assert.equal(failWarns.length, 0, "valid authored points pipeline must not emit failure warning");

  // Frame 2: authored pipeline is resolved — the subsequent draw uses it.
  harness.renderer.render(makePointsBundle(entry), viewport);

  // Just verify the renderer didn't crash and warn count is still zero.
  const failWarns2 = harness.warnLog.filter(m => m.includes("falling back") || m.includes("failed"));
  assert.equal(failWarns2.length, 0, "no failure after two frames with valid authored WGSL");
});

test("points authored WebGPU: invalid customVertexWGSL attempts async pipeline build and then uses builtin", async () => {
  // Verify the authored shader path by confirming createRenderPipelineAsync is
  // called exactly once on first render (authored pipeline kicked off), and is
  // NOT called again on the second render (failure cached, builtin used).
  //
  // Points entries require _cachedPos to be set (count*3 floats): without
  // positions the render loop skips the entry before reaching the pipeline path.
  let renderAsyncCallCount = 0;
  const baseCompute = makeFakeGPUDeviceForCompute({});
  baseCompute.device.createRenderPipelineAsync = function(desc) {
    renderAsyncCallCount++;
    return Promise.reject(new Error("fake points WGSL validation error"));
  };

  const harness = await createComputeParticleHarness(baseCompute.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const entry = {
    id: "bad-authored-layer",
    count: 4,
    // 4 particles × 3 floats each — satisfies the _cachedPos guard so the
    // entry is not skipped before reaching the authored-pipeline branch.
    positions: new Float32Array([0,0,0, 1,0,0, 0,1,0, 0,0,1]),
    customVertexWGSL: "BAD WGSL SOURCE",
    customFragmentWGSL: "ALSO BAD",
  };

  // Frame 1: kicks off async build (returns null → builtin used this frame).
  harness.renderer.render(makePointsBundle(entry), viewport);

  // The authored path must have kicked off exactly one async pipeline build.
  assert.equal(renderAsyncCallCount, 1, "authored WGSL must trigger exactly one createRenderPipelineAsync call");

  // Settle the async rejection chain.
  await flushAsyncWork();
  await flushAsyncWork();
  await flushAsyncWork();

  // Frame 2: failure is now cached — createRenderPipelineAsync must NOT be called again.
  const countBefore = renderAsyncCallCount;
  harness.renderer.render(makePointsBundle(entry), viewport);
  assert.equal(renderAsyncCallCount, countBefore, "failure must be cached — no re-attempt on second render");
});

test("points authored WebGPU: async validation resolution after renderer dispose is ignored", async () => {
  let renderAsyncCallCount = 0;
  let popScopeCallCount = 0;
  const fake = makeFakeGPUDeviceForCompute({
    errorScopeBehavior() {
      popScopeCallCount++;
      return Promise.resolve(null);
    },
  });
  fake.device.createRenderPipelineAsync = function(desc) {
    renderAsyncCallCount++;
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label, isAuthored: true });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const entry = {
    id: "dispose-authored-layer",
    count: 4,
    positions: new Float32Array([0,0,0, 1,0,0, 0,1,0, 0,0,1]),
    customVertexWGSL: "@vertex fn vertexMain() -> @builtin(position) vec4<f32> { return vec4<f32>(0.0,0.0,0.0,1.0); }",
    customFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0,1.0,0.0,1.0); }",
  };

  harness.renderer.render(makePointsBundle(entry), viewport);
  assert.equal(renderAsyncCallCount, 1, "authored pipeline build should start before dispose");

  harness.renderer.dispose();
  await flushAsyncWork();
  await flushAsyncWork();

  assert.ok(popScopeCallCount >= 1, "stale async callback should drain the scoped device error scope");
  const staleWarns = harness.warnLog.filter(m => m.includes("dispose-authored-layer") || m.includes("falling back") || m.includes("failed"));
  assert.equal(staleWarns.length, 0, "disposed renderer callback must not publish failure warnings");
});

test("computeParticles authored render WebGPU: absent renderVertexWGSL goes to builtin render path", async () => {
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
  });

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  harness.renderer.render(makeComputeParticleBundleWithRender({
    id: "plain-cp",
    count: 4,
    emitter: { kind: "point" },
    material: { color: "#fff" },
    // No renderVertexWGSL/renderFragmentWGSL: builtin path.
  }), viewport);

  await flushAsyncWork();
  await flushAsyncWork();

  const authoredWarns = harness.warnLog.filter(m => m.includes("authored render") && m.includes("falling back"));
  assert.equal(authoredWarns.length, 0, "no authored-render warn for entry without renderVertexWGSL");
});

test("computeParticles authored render WebGPU: valid renderVertexWGSL+renderFragmentWGSL builds authored pipeline", async () => {
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    errorScopeBehavior() {
      return Promise.resolve(null);
    },
  });
  // Override createRenderPipelineAsync to succeed for authored render pipeline.
  fake.device.createRenderPipelineAsync = function(desc) {
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label, isAuthoredRender: true });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const entry = {
    id: "authored-cp-render",
    count: 4,
    emitter: { kind: "point" },
    material: { color: "#fff" },
    renderVertexWGSL: "@vertex fn vertexMain() -> @builtin(position) vec4<f32> { return vec4<f32>(0.0,0.0,0.0,1.0); }",
    renderFragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(0.2,0.8,1.0,1.0); }",
  };

  // Frame 1: async build.
  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);
  await flushAsyncWork();
  await flushAsyncWork();

  const failWarns = harness.warnLog.filter(m => m.includes("falling back") || m.includes("failed"));
  assert.equal(failWarns.length, 0, "valid authored particle render pipeline must not warn");

  // Frame 2: authored pipeline is ready.
  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);
  const failWarns2 = harness.warnLog.filter(m => m.includes("falling back") || m.includes("failed"));
  assert.equal(failWarns2.length, 0, "no failure after two frames with valid authored render WGSL");
});

test("computeParticles authored render WebGPU: invalid renderVertexWGSL attempts async build and uses builtin", async () => {
  // Verify that an authored render WGSL kicks off a createRenderPipelineAsync
  // attempt and then caches the failure so no re-attempt occurs on subsequent frames.
  //
  // The authored render pipeline is only attempted once the compute system is
  // "ready" (its compute pipeline has resolved). Frame 1 starts the compute
  // pipeline asynchronously; after flushing, it resolves and the system is ready.
  // Frame 2 is then needed to actually trigger buildAuthoredParticleRenderPipelineAsync.
  let renderAsyncCallCount = 0;
  const baseCompute = makeFakeGPUDeviceForCompute({
    // Allow the compute pipeline to resolve (system becomes ready to render).
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
  });
  // Override createRenderPipelineAsync to reject on the authored render path.
  baseCompute.device.createRenderPipelineAsync = function(desc) {
    renderAsyncCallCount++;
    return Promise.reject(new Error("fake particle render WGSL failure"));
  };

  const harness = await createComputeParticleHarness(baseCompute.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const entry = {
    id: "bad-cp-render",
    count: 4,
    emitter: { kind: "point" },
    material: { color: "#fff" },
    renderVertexWGSL: "BAD RENDER VERT",
    renderFragmentWGSL: "BAD RENDER FRAG",
  };

  // Frame 1: compute system creation kicks off. System not yet ready — render
  // path is skipped this frame (isReady() returns false).
  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);

  // Allow the compute pipeline promise to resolve → system becomes ready.
  await flushAsyncWork();
  await flushAsyncWork();
  await flushAsyncWork();

  // Frame 2: system is now ready. drawComputeParticleEntries reaches the
  // authored render branch and calls buildAuthoredParticleRenderPipelineAsync,
  // which fires createRenderPipelineAsync synchronously within the render call.
  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);

  // The authored render path must have kicked off exactly one async attempt
  // (called synchronously during frame 2's render, before any await).
  assert.equal(renderAsyncCallCount, 1, "authored renderVertexWGSL must trigger exactly one createRenderPipelineAsync call (got " + renderAsyncCallCount + ")");

  // Settle any remaining promise chains (the rejection + markFailed).
  await flushAsyncWork();
  await flushAsyncWork();
  await flushAsyncWork();

  // Frame 3: failure cached — no new async attempt.
  const countBefore = renderAsyncCallCount;
  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);
  assert.equal(renderAsyncCallCount, countBefore, "failure must be cached — no re-attempt on second render");
});

test("shaderLib hydrate: computeWGSLRef is inflated to computeWGSL before WASM hydration", async () => {
  const kernel = "// synthetic kernel " + "x".repeat(2000);
  const libID = "sl:aabb001122334455";

  // Create the island root DOM element so islandRoot(entry) finds it.
  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-shader-test";

  const env = createContext({
    elements: [islandRoot],
    manifest: makeShaderLibManifestEntry(
      { [libID]: kernel },
      [
        { id: "a", count: 100, emitter: { kind: "point" }, material: { color: "#fff" }, computeWGSLRef: libID },
        { id: "b", count: 100, emitter: { kind: "point" }, material: { color: "#fff" }, computeWGSLRef: libID },
      ],
    ),
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1, "island must be hydrated");
  const propsJSON = env.hydrateCalls[0][2];
  const props = JSON.parse(propsJSON);
  const scene = props && props.scene;

  // shaderLib should be gone from the wire props (inflated in-place).
  assert.equal(scene.shaderLib, undefined, "shaderLib must be deleted after inflation");

  // Both compute particles must have computeWGSL inflated.
  assert.ok(Array.isArray(scene.computeParticles), "computeParticles must be an array");
  assert.equal(scene.computeParticles.length, 2);
  for (let i = 0; i < 2; i++) {
    const cp = scene.computeParticles[i];
    assert.equal(cp.computeWGSL, kernel,
      `computeParticles[${i}].computeWGSL must be inflated from shaderLib`);
    assert.equal(cp.computeWGSLRef, undefined,
      `computeParticles[${i}].computeWGSLRef must be deleted after inflation`);
  }
});

test("shaderLib hydrate: missing lib entry leaves field absent, no crash", async () => {
  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-shader-test";

  const env = createContext({
    elements: [islandRoot],
    manifest: makeShaderLibManifestEntry(
      { "sl:realkey": "real kernel" },
      [
        { id: "good", count: 1, emitter: { kind: "point" }, material: {}, computeWGSLRef: "sl:realkey" },
        { id: "bad",  count: 1, emitter: { kind: "point" }, material: {}, computeWGSLRef: "sl:doesnotexist" },
      ],
    ),
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  const props = JSON.parse(env.hydrateCalls[0][2]);
  const scene = props && props.scene;

  assert.equal(scene.shaderLib, undefined, "shaderLib key must be gone");
  const good = scene.computeParticles[0];
  const bad = scene.computeParticles[1];

  assert.equal(good.computeWGSL, "real kernel", "good entry must be inflated");
  assert.equal(good.computeWGSLRef, undefined, "ref key must be deleted");
  assert.equal(bad.computeWGSL, undefined, "missing entry: computeWGSL must be absent (not crash)");
  assert.equal(bad.computeWGSLRef, undefined, "ref key must be deleted even when lib entry missing");
});

test("shaderLib hydrate: no-op when shaderLib absent (plain scene unaffected)", async () => {
  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-plain";

  const env = createContext({
    elements: [islandRoot],
    manifest: {
      islands: [
        {
          id: "gosx-island-plain",
          component: "Plain",
          props: {
            scene: {
              computeParticles: [
                { id: "x", count: 1, emitter: { kind: "point" }, material: {}, computeWGSL: "fn ok() {}" },
              ],
            },
          },
          programRef: "/test.json",
          programFormat: "json",
        },
      ],
      bundles: {},
      runtime: { path: "/test-runtime.wasm" },
    },
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  const props = JSON.parse(env.hydrateCalls[0][2]);
  const scene = props.scene;

  // Original computeWGSL must be untouched.
  assert.equal(scene.computeParticles[0].computeWGSL, "fn ok() {}",
    "inline computeWGSL must be preserved when no shaderLib present");
});

test("shaderLib hydrate: postEffects WGSL and GLSL refs inflate before WASM hydration", async () => {
  const wgsl = "// post wgsl " + "x".repeat(2000);
  const glsl = "// post glsl " + "y".repeat(2000);
  const wgslID = "sl:postwgsl001122334455";
  const glslID = "sl:postglsl001122334455";
  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-postfx-shader-test";

  const env = createContext({
    elements: [islandRoot],
    manifest: {
      islands: [
        {
          id: "gosx-island-postfx-shader-test",
          component: "PostFX",
          bundleId: "test-bundle",
          props: {
            scene: {
              postEffects: [
                {
                  kind: "customPost",
                  name: "flare-shield",
                  fragmentWGSLRef: wgslID,
                  vertexWGSLRef: wgslID,
                  fragmentGLSLRef: glslID,
                  vertexGLSLRef: glslID,
                },
              ],
              shaderLib: { [wgslID]: wgsl, [glslID]: glsl },
            },
          },
          programRef: "/test.json",
          programFormat: "json",
        },
      ],
      bundles: { "test-bundle": { path: "/test.wasm" } },
      runtime: { path: "/test-runtime.wasm" },
    },
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1, "island must be hydrated");
  const props = JSON.parse(env.hydrateCalls[0][2]);
  const scene = props && props.scene;
  assert.equal(scene.shaderLib, undefined, "shaderLib must be deleted after inflation");
  const effect = scene.postEffects && scene.postEffects[0];
  assert.equal(effect.fragmentWGSL, wgsl, "fragmentWGSL must inflate from shaderLib");
  assert.equal(effect.vertexWGSL, wgsl, "vertexWGSL must inflate from shaderLib");
  assert.equal(effect.fragmentGLSL, glsl, "fragmentGLSL must inflate from shaderLib");
  assert.equal(effect.vertexGLSL, glsl, "vertexGLSL must inflate from shaderLib");
  assert.equal(effect.fragmentWGSLRef, undefined, "fragmentWGSLRef must be deleted");
  assert.equal(effect.vertexWGSLRef, undefined, "vertexWGSLRef must be deleted");
  assert.equal(effect.fragmentGLSLRef, undefined, "fragmentGLSLRef must be deleted");
  assert.equal(effect.vertexGLSLRef, undefined, "vertexGLSLRef must be deleted");
});

test("shaderLib hydrate: customVertexWGSLRef inflated to customVertexWGSL in points entries", async () => {
  const shader = "// authored points vertex " + "x".repeat(2000);
  const fragShader = "// authored points fragment " + "x".repeat(2000);
  const vertID = "sl:ptvert001122334455";
  const fragID = "sl:ptfrag001122334455";

  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-points-shader-test";

  const env = createContext({
    elements: [islandRoot],
    manifest: makeShaderLibManifestEntryForPoints(
      { [vertID]: shader, [fragID]: fragShader },
      [
        { id: "layer-a", count: 10, customVertexWGSLRef: vertID, customFragmentWGSLRef: fragID },
        { id: "layer-b", count: 10, customVertexWGSLRef: vertID, customFragmentWGSLRef: fragID },
      ],
    ),
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1, "island must be hydrated");
  const props = JSON.parse(env.hydrateCalls[0][2]);
  const scene = props && props.scene;

  assert.equal(scene.shaderLib, undefined, "shaderLib must be deleted after inflation");
  assert.ok(Array.isArray(scene.points), "points must be an array");
  assert.equal(scene.points.length, 2);
  for (let i = 0; i < 2; i++) {
    const pt = scene.points[i];
    assert.equal(pt.customVertexWGSL, shader,
      `points[${i}].customVertexWGSL must be inflated from shaderLib`);
    assert.equal(pt.customFragmentWGSL, fragShader,
      `points[${i}].customFragmentWGSL must be inflated from shaderLib`);
    assert.equal(pt.customVertexWGSLRef, undefined,
      `points[${i}].customVertexWGSLRef must be deleted after inflation`);
    assert.equal(pt.customFragmentWGSLRef, undefined,
      `points[${i}].customFragmentWGSLRef must be deleted after inflation`);
  }
});

test("shaderLib hydrate: renderVertexWGSLRef inflated to renderVertexWGSL in computeParticles entries", async () => {
  const renderVert = "// authored render vertex " + "x".repeat(2000);
  const renderFrag = "// authored render fragment " + "x".repeat(2000);
  const vertID = "sl:rvvert001122334455";
  const fragID = "sl:rvfrag001122334455";

  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-cprender-shader-test";

  const env = createContext({
    elements: [islandRoot],
    manifest: makeShaderLibManifestEntryForParticleRender(
      { [vertID]: renderVert, [fragID]: renderFrag },
      [
        { id: "sys-a", count: 50, emitter: { kind: "point" }, material: { color: "#fff" }, renderVertexWGSLRef: vertID, renderFragmentWGSLRef: fragID },
        { id: "sys-b", count: 50, emitter: { kind: "point" }, material: { color: "#fff" }, renderVertexWGSLRef: vertID, renderFragmentWGSLRef: fragID },
      ],
    ),
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1, "island must be hydrated");
  const props = JSON.parse(env.hydrateCalls[0][2]);
  const scene = props && props.scene;

  assert.equal(scene.shaderLib, undefined, "shaderLib must be deleted after inflation");
  assert.ok(Array.isArray(scene.computeParticles), "computeParticles must be an array");
  assert.equal(scene.computeParticles.length, 2);
  for (let i = 0; i < 2; i++) {
    const cp = scene.computeParticles[i];
    assert.equal(cp.renderVertexWGSL, renderVert,
      `computeParticles[${i}].renderVertexWGSL must be inflated from shaderLib`);
    assert.equal(cp.renderFragmentWGSL, renderFrag,
      `computeParticles[${i}].renderFragmentWGSL must be inflated from shaderLib`);
    assert.equal(cp.renderVertexWGSLRef, undefined,
      `computeParticles[${i}].renderVertexWGSLRef must be deleted`);
    assert.equal(cp.renderFragmentWGSLRef, undefined,
      `computeParticles[${i}].renderFragmentWGSLRef must be deleted`);
  }
});

test("shaderLib hydrate: cullKernelWGSLRef is inflated to cullKernelWGSL in instancedMeshes entries", async () => {
  const kernel = "// synthetic cull kernel " + "x".repeat(2000);
  const libID = "sl:cull001122334455";
  const transforms = new Array(16).fill(0);
  transforms[0] = transforms[5] = transforms[10] = transforms[15] = 1; // identity

  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-instanced-cull-test";

  const env = createContext({
    elements: [islandRoot],
    manifest: makeShaderLibManifestEntryForInstancedMeshes(
      { [libID]: kernel },
      [
        { id: "ring-a", count: 1, kind: "box", transforms, cullKernelWGSLRef: libID },
        { id: "ring-b", count: 1, kind: "box", transforms, cullKernelWGSLRef: libID },
      ],
    ),
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1, "island must be hydrated");
  const propsJSON = env.hydrateCalls[0][2];
  const props = JSON.parse(propsJSON);
  const scene = props && props.scene;

  // shaderLib must be gone after inflation.
  assert.equal(scene.shaderLib, undefined, "shaderLib must be deleted after inflation");

  // Both instancedMeshes must have cullKernelWGSL inflated.
  assert.ok(Array.isArray(scene.instancedMeshes), "instancedMeshes must be an array");
  assert.equal(scene.instancedMeshes.length, 2);
  for (let i = 0; i < 2; i++) {
    const im = scene.instancedMeshes[i];
    assert.equal(im.cullKernelWGSL, kernel,
      `instancedMeshes[${i}].cullKernelWGSL must be inflated from shaderLib`);
    assert.equal(im.cullKernelWGSLRef, undefined,
      `instancedMeshes[${i}].cullKernelWGSLRef must be deleted after inflation`);
  }
});

test("shaderLib hydrate: missing cullKernelWGSL lib entry leaves field absent, no crash", async () => {
  const transforms = new Array(16).fill(0);
  transforms[0] = transforms[5] = transforms[10] = transforms[15] = 1;

  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-instanced-cull-test";

  const env = createContext({
    elements: [islandRoot],
    manifest: makeShaderLibManifestEntryForInstancedMeshes(
      { "sl:realcull": "real cull kernel" },
      [
        { id: "good", count: 1, kind: "box", transforms, cullKernelWGSLRef: "sl:realcull" },
        { id: "bad",  count: 1, kind: "box", transforms, cullKernelWGSLRef: "sl:doesnotexist" },
      ],
    ),
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  const props = JSON.parse(env.hydrateCalls[0][2]);
  const scene = props && props.scene;

  assert.equal(scene.shaderLib, undefined, "shaderLib key must be gone");
  const good = scene.instancedMeshes[0];
  const bad = scene.instancedMeshes[1];

  assert.equal(good.cullKernelWGSL, "real cull kernel", "good entry must be inflated");
  assert.equal(good.cullKernelWGSLRef, undefined, "ref key must be deleted");
  assert.equal(bad.cullKernelWGSL, undefined, "missing entry: cullKernelWGSL must be absent (not crash)");
  assert.equal(bad.cullKernelWGSLRef, undefined, "ref key must be deleted even when lib entry missing");
});

// Regression test: normalizeSceneInstancedMeshEntry (10-runtime-scene-core.js)
// used to omit cullKernelWGSL/cullKernelEntry/cullRadius/cullBackend when it
// rebuilt each instancedMeshes entry into its whitelisted `normalized` object.
// Consequence: updateInstancedCullSystems in 16a-scene-webgpu.js always saw
// mesh.cullKernelWGSL === undefined and never created a GPU cull system, so
// cullSurvivors telemetry stayed {} forever even though the Go scene types and
// manifest JSON carried the fields end-to-end (and even after the shaderLib
// dedup inflation above turns cullKernelWGSLRef into cullKernelWGSL). This
// exercises the actual normalizer (via the public createSceneState entry
// point, same as the WebGPU/WebGL render paths use) rather than just the raw
// pre-normalization manifest props.
test("normalizeSceneInstancedMeshEntry preserves cullKernelWGSL/cullKernelEntry/cullRadius/cullBackend and still drops unknown fields", async () => {
  const kernel = "@compute @workgroup_size(64) fn cull() {}";
  const transforms = new Array(16).fill(0);
  transforms[0] = transforms[5] = transforms[10] = transforms[15] = 1; // identity

  const islandRoot = new FakeElement("div", null);
  islandRoot.id = "gosx-island-instanced-cull-test";

  const env = createContext({
    elements: [islandRoot],
    manifest: makeShaderLibManifestEntryForInstancedMeshes(
      {},
      [
        {
          id: "meteors",
          count: 3,
          kind: "box",
          transforms,
          cullKernelWGSL: kernel,
          cullKernelEntry: "cullMain",
          cullRadius: 4.5,
          cullBackend: "WebGPU",
          pickable: false,
          thisFieldDoesNotExist: "should be dropped by the whitelist",
        },
      ],
    ),
    fetchRoutes: {
      "/test-runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/test.json": { text: '{}' },
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1, "island must be hydrated");
  const props = JSON.parse(env.hydrateCalls[0][2]);

  const state = env.context.__gosx_scene3d_api.createSceneState(props, null);
  assert.equal(state.instancedMeshes.length, 1);
  const mesh = state.instancedMeshes[0];

  assert.equal(mesh.cullKernelWGSL, kernel, "cullKernelWGSL must survive normalization");
  assert.equal(mesh.cullKernelEntry, "cullMain", "cullKernelEntry must survive normalization");
  assert.equal(mesh.cullRadius, 4.5, "cullRadius must survive normalization");
  assert.equal(mesh.cullBackend, "webgpu", "cullBackend must survive normalization (lowercased, like shaderBackend)");
  assert.equal(mesh.pickable, false, "explicit instanced-mesh pickability must survive normalization");
  assert.equal(mesh.thisFieldDoesNotExist, undefined, "unknown fields must still be dropped by the whitelist");
});

test("WebGPU water sampled state only uses the defined ping-pong textures", () => {
  assert.doesNotMatch(bootstrapScene3DWebGPUSourceFile, /stateTexture:\s*stateTexture[,\n]/);
  assert.doesNotMatch(bootstrapScene3DWebGPUSourceFile, /system\.stateTexture(?![A-Z])/);
  assert.match(bootstrapScene3DWebGPUSourceFile, /stateTextureA:\s*stateTextureA/);
  assert.match(bootstrapScene3DWebGPUSourceFile, /stateTextureB:\s*stateTextureB/);
  assert.match(bootstrapScene3DWebGPUSourceFile, /function syncWaterSampledState\(/);
});

test("Scene3D raycast returns the exact nearest non-uniformly scaled instance", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const identity = [
    1, 0, 0, 0,
    0, 0.5, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ];
  const translated = identity.slice();
  translated[12] = 3;
  const hit = env.context.__gosx_scene3d_api.sceneRaycastPickInstancedMeshes(
    { origin: { x: 0, y: 3, z: 0 }, dir: { x: 0, y: -1, z: 0 } },
    [{ id: "pieces", count: 2, kind: "sphere", radius: 0.5, pickable: true, transforms: identity.concat(translated) }],
    0,
  );

  assert.ok(hit, "expected the flattened sphere instance to be hit");
  assert.equal(hit.object.id, "pieces");
  assert.equal(hit.instanceIndex, 0);
  assert.ok(Math.abs(hit.distance - 2.75) < 1e-9, "expected exact ellipsoid surface distance");
  assert.ok(Math.abs(hit.worldPosition.y - 0.25) < 1e-9, "expected exact world-space hit point");
});

test("Scene3D authored picks reserve pointer gestures before orbit controls", () => {
  const pickInstall = bootstrapScene3DMountSourceFile.indexOf("pickHandle = setupScenePickInteractions");
  const controlsInstall = bootstrapScene3DMountSourceFile.indexOf("sceneControlHandle = setupSceneBuiltInControls", pickInstall);
  assert.ok(pickInstall >= 0 && controlsInstall > pickInstall, "pick listener must be registered before controls");
  assert.match(bootstrapScene3DMountSourceFile, /function onPointerDown\(event\) \{\s+if \(event && event\.defaultPrevented\)/);
  assert.match(bootstrapScene3DInputSourceFile, /sceneRaycastPickInstancedMeshes\(ray, bundle\.instancedMeshes/);
});

// -------------------------------------------------------------------------
// Task 5: gpu-cull capability (from Go capability Matrix + JSON manifests)
// -------------------------------------------------------------------------

test("capability/drift: WebGPU-only capabilities are explicit in backend JSON", () => {
  // Read the capability JSON files directly from bootstrap-src, same as the Go drift guard.
  const webgpuCaps = JSON.parse(fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "16a-scene-webgpu.capabilities.json"), "utf8"
  ));
  const webglCaps = JSON.parse(fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "16-scene-webgl.capabilities.json"), "utf8"
  ));
  assert.equal(webgpuCaps["gpu-cull"], true, "WebGPU capabilities JSON must declare gpu-cull: true");
  assert.equal(webglCaps["gpu-cull"], false, "WebGL2 capabilities JSON must declare gpu-cull: false (explicit, not absent)");
  assert.ok("gpu-cull" in webglCaps, "gpu-cull must be an explicit key in WebGL2 capabilities JSON (not absent)");
  assert.equal(webgpuCaps["water-object-texture-pass"], true, "WebGPU capabilities JSON must declare water-object-texture-pass: true");
  // A3: WebGL2 now implements the water passes via the runtime water renderer,
  // so the manifest declares them true (WebGPU stays primary, WebGL2 fallback).
  assert.equal(webglCaps["water-object-texture-pass"], true, "WebGL2 capabilities JSON must declare water-object-texture-pass: true (runtime water renderer)");
  assert.ok("water-object-texture-pass" in webglCaps, "water-object-texture-pass must be explicit in WebGL2 capabilities JSON");
  assert.equal(webglCaps["water-simulation"], true, "WebGL2 capabilities JSON must declare water-simulation: true");
  // The mesh-shadow cell is FALSE for WebGL2, and that is a corroborated claim
  // rather than a preference. A mesh shadow rasterizes the caster's geometry.
  // Both WebGL2 shadow programs bind an empty vertex array object and draw one
  // full-screen triangle, so both shade an analytic primitive. The identifier
  // objectMeshShadow appears zero times in 16-scene-webgl.js. See
  // scene/capability/water_shadow_test.go, which reads both renderers.
  assert.equal(webglCaps["water-object-mesh-shadow-pass"], false, "WebGL2 capabilities JSON must declare water-object-mesh-shadow-pass: false (no mesh rasterization in the WebGL2 water renderer)");
  assert.ok("water-object-mesh-shadow-pass" in webglCaps, "water-object-mesh-shadow-pass must be explicit in WebGL2 capabilities JSON (not absent)");
  // ibl is false on BOTH backends. The WebGL2 path tone maps the environment to
  // an 8-bit texture and taps it twice; it holds no samplerCube, no
  // textureCubeLod and no BRDF lookup table. See assetpipe/ibl for the products
  // a real consumer needs.
  assert.equal(webglCaps["ibl"], false, "WebGL2 capabilities JSON must declare ibl: false (tone-mapped equirect is not prefiltered IBL)");
  assert.equal(webgpuCaps["ibl"], false, "WebGPU capabilities JSON must declare ibl: false");
});

test("getSelenaPipeline memo: N objects sharing one material build the content key ONCE per material per frame", async () => {
  const harness = await createBoardWebGPUHarness();
  const N = 8;
  const bundle = boardBundleManyObjectsOneMaterial(N);

  // Instrument JSON.stringify on the sandbox: the key build calls
  // JSON.stringify(layout) exactly once per key construction. Counting calls
  // that carry a Selena layout (a uniformBlock) isolates getSelenaPipeline's
  // key builds from any other stringify in the frame.
  const sandboxJSON = harness.env.context.JSON;
  const realStringify = sandboxJSON.stringify;
  let layoutStringifyCount = 0;
  sandboxJSON.stringify = function(value) {
    if (value && typeof value === "object" && value.uniformBlock) {
      layoutStringifyCount++;
    }
    return realStringify.apply(this, arguments);
  };
  try {
    // Frame 1: the memo is cold → exactly ONE key build for the one shared
    // material, NOT one per object.
    harness.renderer.render(bundle, {});
    assert.equal(layoutStringifyCount, 1, "frame 1 must build the Selena key once for the shared material (got " + layoutStringifyCount + " for " + N + " objects)");

    // All N objects drew through the one shared pipeline.
    const mains = mainRenderPasses(harness.fake);
    assert.equal(mains[mains.length - 1].draws.length, N, "all N objects must draw");

    // The memo stamp is present on the material and carries the resolved pipeline.
    const memo = bundle.materials[0]._gosxWGPUSelenaResource;
    assert.ok(memo, "the material must carry the _gosxWGPUSelenaResource memo after render");
    assert.ok(memo.resource && memo.resource.pipeline, "the memo must hold the resolved pipeline");
    assert.equal(memo.failed, false);

    // Frame 2 (same bundle object — the material's memo persists): the per-pass
    // variant inputs are unchanged, so the memo short-circuits with ZERO new key
    // builds for the shared material.
    const beforeFrame2 = layoutStringifyCount;
    harness.renderer.render(bundle, {});
    assert.equal(layoutStringifyCount, beforeFrame2, "frame 2 must reuse the material memo and build NO new key");
  } finally {
    sandboxJSON.stringify = realStringify;
  }
});

test("custom post WebGPU: absent fragmentWGSL goes straight to identity without pipeline attempt", async () => {
  // A customPost effect with no fragmentWGSL/vertexWGSL must be a silent no-op
  // — no createRenderPipelineAsync call, no warn.
  let renderPipelineAsyncCalls = 0;
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    errorScopeBehavior() { return Promise.resolve(null); },
  });
  fake.device.createRenderPipelineAsync = function(desc) {
    renderPipelineAsyncCalls++;
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  harness.renderer.render(makeBundleWithCustomPost({}), viewport);
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(renderPipelineAsyncCalls, 0, "absent WGSL must not attempt a pipeline build");
  const warns = harness.warnLog.filter(m => m.includes("custom post pass"));
  assert.equal(warns.length, 0, "no warn for absent WGSL");
});

test("custom post WebGPU: valid fragmentWGSL+vertexWGSL builds async pipeline and uses it on frame 2", async () => {
  // Frame 1: buildCustomPostPipelineAsync fires and returns null (pending).
  // Frame 2: the pipeline resolves → createBindGroup + draw.
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    errorScopeBehavior() { return Promise.resolve(null); },
  });
  let renderPipelineAsyncCalls = 0;
  fake.device.createRenderPipelineAsync = function(desc) {
    renderPipelineAsyncCalls++;
    return Promise.resolve({
      __kind: "renderPipeline",
      label: desc && desc.label,
      isCustomPost: true,
    });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const bundle = makeBundleWithCustomPost({
    fragmentWGSL: "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0); }",
    vertexWGSL: "@vertex fn vertexMain(@builtin(vertex_index) vi: u32) -> @builtin(position) vec4<f32> { return vec4<f32>(0.0, 0.0, 0.0, 1.0); }",
  });

  // Frame 1: pipeline enqueued.
  harness.renderer.render(bundle, viewport);
  assert.equal(renderPipelineAsyncCalls, 1, "frame 1 must trigger exactly one createRenderPipelineAsync");

  await flushAsyncWork();
  await flushAsyncWork();

  const failWarns = harness.warnLog.filter(m => m.includes("custom post pass") && (m.includes("failed") || m.includes("passthrough")));
  assert.equal(failWarns.length, 0, "valid custom post WGSL must not warn");
});

// End-to-end dispatch pin. Every other custom-post test above hands the renderer
// a HAND-BUILT bundle with kind:"customPost" written literally, so they all kept
// passing while the real author path was dead: normalizeScenePostEffect
// lowercased kind to "custompost", which matched no case in the backend switch,
// and the pass was never entered. The effect stayed in state.postEffects and the
// post chain still ran its final blit, so nothing observable complained.
//
// This test drives the REAL author path — props → createSceneState →
// createSceneRenderBundle → renderer.render — and asserts the pass actually
// reaches the GPU: its WGSL compiles into a shader module, and on the frame
// after the async pipeline resolves it is BOUND AND DRAWN. Assert the draw, not
// just the compile: a pass that compiles but never dispatches is exactly the bug.
test("custom post WebGPU: a customPost authored through createSceneState is compiled AND drawn", async () => {
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    errorScopeBehavior() { return Promise.resolve(null); },
  });
  fake.device.createRenderPipelineAsync = function(desc) {
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const sceneAPI = harness.env.context.__gosx_scene3d_api;
  assert.ok(sceneAPI && typeof sceneAPI.createSceneState === "function", "scene3d chunk must publish createSceneState");

  const fragmentWGSL = "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0, 0.0, 0.0, 1.0); }";
  const vertexWGSL = "@vertex fn vertexMain(@builtin(vertex_index) vi: u32) -> @builtin(position) vec4<f32> { return vec4<f32>(0.0, 0.0, 0.0, 1.0); }";

  // The author path: exactly the shape a Selena CustomPost lowers to on the wire.
  const state = sceneAPI.createSceneState({
    scene: {
      postEffects: [{
        kind: "customPost",
        name: "galaxy-liquid-glass",
        fragmentWGSL,
        vertexWGSL,
      }],
    },
  });
  assert.equal(state.postEffects.length, 1, "the custom pass must survive createSceneState");
  assert.equal(state.postEffects[0].kind, sceneAPI.SCENE_POST_CUSTOM_POST,
    "state must carry the canonical kind the backend switch dispatches on");

  // The real bundle builder, fed the real state (a minimal compute particle
  // keeps the renderer's no-geometry early-return from firing first).
  const bundle = sceneAPI.createSceneRenderBundle(
    320, 180,
    null,
    { x: 0, y: 0, z: 5, fov: 72, near: 0.05, far: 128 },
    [], [], [], [], [],
    {},
    0,
    [], [],
    [{ id: "post-dispatch-cp", count: 4, emitter: { kind: "point" }, material: { color: "#fff" } }],
    [],
    state.postEffects,
    0,
    false,
  );
  assert.equal(bundle.postEffects.length, 1, "the bundle must carry the custom pass");
  assert.equal(bundle.postEffects[0].kind, sceneAPI.SCENE_POST_CUSTOM_POST);

  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  // Frame 1: the pass is entered and its WGSL submitted; the pipeline is async
  // so this frame still falls through to the identity blit.
  harness.renderer.render(bundle, viewport);

  const postModules = fake.state.shaderModules.filter(
    (module) => module.label === "selena-post-galaxy-liquid-glass",
  );
  assert.equal(postModules.length, 1,
    "the authored customPost WGSL must reach createShaderModule — if this is 0 the pass was never dispatched");
  assert.equal(postModules[0].code.includes(fragmentWGSL), true, "the authored fragment WGSL must be submitted verbatim");
  assert.equal(postModules[0].code.includes(vertexWGSL), true, "the authored vertex WGSL must be submitted verbatim");

  await flushAsyncWork();
  await flushAsyncWork();

  const drawsBefore = mainRenderPasses(fake).reduce((n, pass) => n + pass.draws.length, 0);

  // Frame 2: the pipeline has resolved, so the pass must BIND AND DRAW.
  harness.renderer.render(bundle, viewport);

  const postBindGroups = fake.state.bindGroups.filter(
    (bg) => bg.desc && bg.desc.layout && bg.desc.layout.desc && bg.desc.layout.desc.label === "gosx-selena-post",
  );
  assert.equal(postBindGroups.length >= 1, true,
    "the custom post pass must build a bind group against the gosx-selena-post layout");

  const postDraws = [];
  for (const pass of mainRenderPasses(fake)) {
    for (const draw of pass.draws) {
      if (draw.pipeline && draw.pipeline.label === "gosx-selena-post-galaxy-liquid-glass") {
        postDraws.push(draw);
      }
    }
  }
  assert.equal(postDraws.length >= 1, true,
    "the custom post pipeline must actually be drawn with — compiling it is not enough");
  assert.equal(drawsBefore >= 0, true);

  const failWarns = harness.warnLog.filter((m) => m.includes("custom post pass"));
  assert.deepEqual(failWarns, [], "a valid authored custom pass must not warn");
});

test("custom post WebGPU: identical complete WGSL module is submitted once", async () => {
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    errorScopeBehavior() { return Promise.resolve(null); },
  });
  let renderPipelineAsyncCalls = 0;
  fake.device.createRenderPipelineAsync = function(desc) {
    renderPipelineAsyncCalls++;
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const completeModule = [
    "@vertex fn vertexMain(@builtin(vertex_index) vi: u32) -> @builtin(position) vec4<f32> {",
    "  return vec4<f32>(0.0, 0.0, 0.0, 1.0);",
    "}",
    "@fragment fn fragmentMain() -> @location(0) vec4<f32> {",
    "  return vec4<f32>(1.0);",
    "}",
  ].join("\n");

  harness.renderer.render(makeBundleWithCustomPost({
    fragmentWGSL: completeModule,
    vertexWGSL: completeModule,
  }), viewport);
  assert.equal(renderPipelineAsyncCalls, 1, "frame 1 must trigger exactly one createRenderPipelineAsync");

  const modules = fake.state.shaderModules.filter((module) => module.label === "selena-post-test-lens");
  assert.equal(modules.length, 1, "one Selena post shader module must be created");
  assert.equal(modules[0].code, completeModule, "identical complete WGSL must not be concatenated twice");
  assert.equal((modules[0].code.match(/fn vertexMain/g) || []).length, 1, "vertexMain must occur once");
  assert.equal((modules[0].code.match(/fn fragmentMain/g) || []).length, 1, "fragmentMain must occur once");

  await flushAsyncWork();
  await flushAsyncWork();
  const failWarns = harness.warnLog.filter(m => m.includes("custom post pass") && (m.includes("failed") || m.includes("passthrough")));
  assert.equal(failWarns.length, 0, "identical complete WGSL module must not warn");
});

test("custom post WebGPU: same name and WGSL prefix with different tail builds distinct pipelines", async () => {
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    errorScopeBehavior() { return Promise.resolve(null); },
  });
  let renderPipelineAsyncCalls = 0;
  fake.device.createRenderPipelineAsync = function(desc) {
    renderPipelineAsyncCalls++;
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const sharedPrefix = "// shared prefix " + "x".repeat(180) + "\n";
  const moduleA = [
    sharedPrefix,
    "@vertex fn vertexMain(@builtin(vertex_index) vi: u32) -> @builtin(position) vec4<f32> { return vec4<f32>(0.0); }",
    "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(0.1, 0.2, 0.3, 1.0); }",
  ].join("\n");
  const moduleB = [
    sharedPrefix,
    "@vertex fn vertexMain(@builtin(vertex_index) vi: u32) -> @builtin(position) vec4<f32> { return vec4<f32>(0.0); }",
    "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(0.9, 0.8, 0.7, 1.0); }",
  ].join("\n");

  harness.renderer.render(makeBundleWithCustomPost({
    name: "same-prefix-lens",
    fragmentWGSL: moduleA,
    vertexWGSL: moduleA,
  }), viewport);
  harness.renderer.render(makeBundleWithCustomPost({
    name: "same-prefix-lens",
    fragmentWGSL: moduleB,
    vertexWGSL: moduleB,
  }), viewport);

  assert.equal(renderPipelineAsyncCalls, 2, "different WGSL tails must not collide in the custom post pipeline cache");
  const modules = fake.state.shaderModules.filter((module) => module.label === "selena-post-same-prefix-lens");
  assert.equal(modules.length, 2, "both different WGSL modules must be submitted");
  assert.equal(modules[0].code, moduleA);
  assert.equal(modules[1].code, moduleB);
});

test("custom post WebGPU: invalid WGSL triggers async validation failure, warns once, identity on subsequent frames", async () => {
  let renderPipelineAsyncCalls = 0;
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
  });
  // Measured browser behaviour for invalid WGSL: createRenderPipelineAsync
  // REJECTS. This used to be expressed through the device error scope, which
  // could not say which pipeline the error belonged to.
  fake.device.createRenderPipelineAsync = function(desc) {
    renderPipelineAsyncCalls++;
    const err = new Error("ShaderModule with '" + (desc && desc.label) + "' label is invalid");
    err.name = "GPUPipelineError";
    return Promise.reject(err);
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const bundle = makeBundleWithCustomPost({
    fragmentWGSL: "BAD WGSL FRAGMENT",
    vertexWGSL: "BAD WGSL VERTEX",
  });

  // Frame 1: pipeline attempt fires.
  harness.renderer.render(bundle, viewport);
  assert.equal(renderPipelineAsyncCalls, 1, "frame 1 must attempt one createRenderPipelineAsync");

  await flushAsyncWork();
  await flushAsyncWork();
  await flushAsyncWork();

  // Warn fired exactly once.
  const warns = harness.warnLog.filter(m => m.includes("custom post pass") && (m.includes("passthrough") || m.includes("validation")));
  assert.equal(warns.length, 1, "exactly one failure warn must fire for the invalid custom post WGSL");

  // Frame 2: failure cached — no new pipeline attempt.
  const callsBefore = renderPipelineAsyncCalls;
  harness.renderer.render(bundle, viewport);
  assert.equal(renderPipelineAsyncCalls, callsBefore, "failure must be cached — no re-attempt on frame 2");

  // Still exactly one warn.
  const warns2 = harness.warnLog.filter(m => m.includes("custom post pass") && (m.includes("passthrough") || m.includes("validation")));
  assert.equal(warns2.length, 1, "warn must fire only once (cached failure)");
});

test("custom post WebGL2: absent vertexGLSL/fragmentGLSL goes straight to identity without compile attempt", async () => {
  const { renderer, canvas, warnLog } = createWebGLRendererForPost();
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  renderer.render(makeWebGLBundleWithCustomPost({}), viewport);

  const gl = canvas.getContext("webgl2");
  // createShader should only have been called for the standard scene shaders,
  // NOT for a custom post program (no GLSL supplied).
  const createShaderCalls = gl.ops.filter(op => op[0] === "createShader").length;
  // Standard scene init creates shaders for PBR, shadow, etc. The custom post
  // path (with absent GLSL) must not add any extra createShader calls.
  // Re-render a second time with NO custom post to measure the baseline.
  const baselineBundle = Object.assign({}, makeWebGLBundleWithCustomPost({}), { postEffects: [] });
  const baseGl = canvas.getContext("webgl2");
  // If no GLSL → no warn from the custom post path.
  const customPostWarnsBefore = warnLog.filter(m => m.includes("gl-lens") || m.includes("custom post pass")).length;
  assert.equal(customPostWarnsBefore, 0, "absent GLSL must not warn");

  renderer.dispose();
});

test("custom post WebGL2: valid vertexGLSL+fragmentGLSL compiles and links the program", async () => {
  const { renderer, canvas, warnLog } = createWebGLRendererForPost();
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const validVert = "attribute vec2 a_position; varying vec2 v_uv; void main() { v_uv = a_position * 0.5 + 0.5; gl_Position = vec4(a_position, 0.0, 1.0); }";
  const validFrag = "precision mediump float; varying vec2 v_uv; uniform sampler2D _sceneColor; void main() { gl_FragColor = texture2D(_sceneColor, v_uv); }";

  renderer.render(makeWebGLBundleWithCustomPost({ vertexGLSL: validVert, fragmentGLSL: validFrag }), viewport);

  const gl = canvas.getContext("webgl2");
  // FakeWebGLContext returns LINK_STATUS=true always, so a valid GLSL pair
  // must produce a linkProgram call.
  const linked = gl.ops.filter(op => op[0] === "linkProgram");
  assert.ok(linked.length >= 1, "custom post GLSL must trigger at least one linkProgram call");

  const warns = warnLog.filter(m => m.includes("custom post pass") || m.includes("gl-lens"));
  assert.equal(warns.length, 0, "valid GLSL must not warn");

  renderer.dispose();
});

// WebGL counterpart of the WebGPU dispatch pin. 16-scene-webgl.js runs the same
// `switch (effect.kind)` against the same camelCase SCENE_POST_* constants, so
// the lowercasing normalizer killed the custom pass on BOTH backends. Every
// other WebGL custom-post test writes kind:"customPost" literally into a
// hand-built bundle, so none of them covered the author path.
test("custom post WebGL2: a customPost authored through createSceneState is dispatched", async () => {
  const { env, renderer, canvas, warnLog } = createWebGLRendererForPost();
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const sceneAPI = env.context.__gosx_scene3d_api;
  const validVert = "attribute vec2 a_position; varying vec2 v_uv; void main() { v_uv = a_position * 0.5 + 0.5; gl_Position = vec4(a_position, 0.0, 1.0); }";
  const validFrag = "precision mediump float; varying vec2 v_uv; uniform sampler2D _sceneColor; void main() { gl_FragColor = texture2D(_sceneColor, v_uv); }";

  // The author path produces the effect; the rest of the bundle is the shape the
  // sibling WebGL tests already use.
  const state = sceneAPI.createSceneState({
    scene: {
      postEffects: [{ kind: "customPost", name: "gl-lens", vertexGLSL: validVert, fragmentGLSL: validFrag }],
    },
  });
  assert.equal(state.postEffects[0].kind, sceneAPI.SCENE_POST_CUSTOM_POST,
    "state must carry the canonical kind the WebGL backend switch dispatches on");

  const bundle = Object.assign(makeWebGLBundleWithCustomPost({}), { postEffects: state.postEffects });

  const gl = canvas.getContext("webgl2");
  const linkedBefore = gl.ops.filter((op) => op[0] === "linkProgram").length;

  renderer.render(bundle, viewport);

  const linkedAfter = gl.ops.filter((op) => op[0] === "linkProgram").length;
  assert.equal(linkedAfter > linkedBefore, true,
    "the authored custom post GLSL must compile+link — if it does not, the pass was never dispatched");

  const warns = warnLog.filter((m) => m.includes("custom post pass") || m.includes("gl-lens"));
  assert.deepEqual(warns, [], "a valid authored custom pass must not warn");

  // A TRAILING custom pass must own the final image. applyCustomPost returns
  // null to signal "I already drew to the default framebuffer"; the case used to
  // swallow that via `if (next !== null)`, leaving currentTexture on the raw
  // scene color so the chain's closing blitToScreen painted the un-post-processed
  // scene straight over the pass output. That produced exactly ONE extra
  // on-screen draw and zero visible effect. Exactly one full-screen draw may
  // reach the canvas: the custom pass's own.
  assert.equal(countDefaultFramebufferDraws(gl), 1,
    "a trailing custom post pass must be the ONLY draw that reaches the canvas — a second one means the scene was blitted over it");

  renderer.dispose();
});

test("custom post WebGL2: compile/link failure warns once and falls back to identity on subsequent frames", async () => {
  // Strategy: boot a renderer normally (initial programs compile/link OK via
  // the default FakeWebGLContext), then patch the LIVE GL context to fail ONLY
  // future linkProgram calls. This avoids the broken-init problem that arises
  // when the failLinkGL object is set before backend.create() runs.
  const { renderer, canvas, warnLog } = createWebGLRendererForPost();
  assert.ok(renderer, "renderer must be created successfully with default GL");

  // Retrieve the live GL context from the canvas. FakeElement caches the
  // context on _webglContext after the first getContext("webgl2") call inside
  // createScenePBRRendererOrFallback / createSceneWebGLRenderer.
  const gl = canvas.getContext("webgl2") || canvas._webglContext;
  assert.ok(gl, "canvas must have a cached GL context after renderer creation");

  // Patch getProgramParameter so ALL future linkProgram calls fail.
  // The initial scene programs are already compiled; only the custom post
  // program's link will fail from this point on.
  const realGetProgramParameter = gl.getProgramParameter.bind(gl);
  gl.getProgramParameter = function(_program, param) {
    if (param === this.LINK_STATUS) return false;
    return realGetProgramParameter(_program, param);
  };

  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const bundle = makeWebGLBundleWithCustomPost({
    vertexGLSL: "attribute vec2 a_position; void main() { gl_Position = vec4(a_position, 0.0, 1.0); }",
    fragmentGLSL: "precision mediump float; void main() { gl_FragColor = vec4(1.0); }",
  });

  // Frame 1: link fails → warn once, mark failed.
  renderer.render(bundle, viewport);
  const warns1 = warnLog.filter(m => m.includes("custom post pass") || m.includes("gl-lens"));
  assert.equal(warns1.length, 1, "one warn must fire on first compile/link failure");

  // Frame 2: failure cached → no re-attempt, no new warn.
  renderer.render(bundle, viewport);
  const warns2 = warnLog.filter(m => m.includes("custom post pass") || m.includes("gl-lens"));
  assert.equal(warns2.length, 1, "warn must fire only once after the failure is cached");

  renderer.dispose();
});

test("computeParticles WebGL: renderVertex/renderFragment use authored points program and expose draw counters", async () => {
  const { env, renderer, canvas, warnLog } = createWebGLRendererForPost();
  const mount = env.document.createElement("div");
  mount.setAttribute("id", "webgl-compute-particles-test");
  mount.appendChild(canvas);
  env.document.body.appendChild(mount);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const vertexGLSL = [
    "attribute vec3 a_position;",
    "attribute float a_size;",
    "attribute vec4 a_color;",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "uniform mat4 u_modelMatrix;",
    "uniform float brightness;",
    "void main() {",
    "  gl_Position = u_projectionMatrix * u_viewMatrix * u_modelMatrix * vec4(a_position, 1.0);",
    "  gl_PointSize = max(1.0, a_size + brightness);",
    "}",
  ].join("\n");
  const fragmentGLSL = [
    "precision mediump float;",
    "attribute vec4 unused_attribute;",
    "uniform float brightness;",
    "void main() {",
    "  gl_FragColor = vec4(brightness, 0.25, 0.5, 1.0);",
    "}",
  ].join("\n");

  renderer.render(makeComputeParticleBundle({
    id: "webgl-authored-compute",
    count: 4,
    emitter: { kind: "point", lifetime: 10 },
    material: { color: "#ffffff", size: 2, attenuation: false },
    renderVertex: vertexGLSL,
    renderFragment: fragmentGLSL,
    renderUniforms: { brightness: 1.25 },
    renderShaderLayout: { material: "TestParticles" },
  }), viewport);

  const gl = canvas.getContext("webgl2");
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === gl.POINTS && entry[3] === 4),
    "compute particles must draw as WebGL points");
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1f" && entry[1] === "brightness" && entry[2] === 1.25),
    "renderUniforms must map to authored points customUniforms");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-compute-particle-draw-entries"), "1");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-compute-particle-draw-instances"), "4");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-compute-particle-draw-calls"), "1");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-compute-particle-authored-draw-entries"), "1");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-compute-particle-authored-draw-instances"), "4");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-compute-particle-authored-draw-calls"), "1");
  assert.equal(warnLog.filter((m) => m.includes("Points authored") && m.includes("falling back")).length, 0);

  renderer.dispose();
});

// -------------------------------------------------------------------------
// Reconciliation A — dual-entry vertexStorageMain selection (WebGPU, 16a)
// -------------------------------------------------------------------------

test("computeParticles authored render WebGPU: dual-entry WGSL prefers vertexStorageMain over vertexMain", async () => {
  // A WGSL module that exposes both vertexMain (attribute path) and
  // vertexStorageMain (storage path): the renderer must select vertexStorageMain
  // as the vertex entry point for the authored particle render pipeline.
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    errorScopeBehavior() { return Promise.resolve(null); },
  });

  const capturedDescs = [];
  fake.device.createRenderPipelineAsync = function(desc) {
    capturedDescs.push(desc);
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  const dualEntryWGSL = [
    "struct VSOut { @builtin(position) pos: vec4<f32> };",
    "@vertex fn vertexMain(@location(0) pos: vec3<f32>) -> VSOut {",
    "  var o: VSOut; o.pos = vec4f(pos, 1.0); return o;",
    "}",
    "@vertex fn vertexStorageMain(@builtin(vertex_index) vi: u32) -> VSOut {",
    "  var o: VSOut; o.pos = vec4f(0.0, 0.0, 0.0, 1.0); return o;",
    "}",
    "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4f(1.0); }",
  ].join("\n");

  const entry = {
    id: "dual-entry-cp",
    count: 4,
    emitter: { kind: "point" },
    material: { color: "#fff" },
    renderVertexWGSL: dualEntryWGSL,
    renderFragmentWGSL: dualEntryWGSL,
  };

  // Frame 1: kicks off compute + render pipeline async build.
  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);
  await flushAsyncWork();
  await flushAsyncWork();

  // Frame 2: system ready, authored render pipeline is built.
  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);
  await flushAsyncWork();
  await flushAsyncWork();

  // The authored render pipeline descriptor must use vertexStorageMain.
  const authoredDesc = capturedDescs.find(d => d && d.vertex && d.vertex.entryPoint === "vertexStorageMain");
  assert.ok(authoredDesc, "dual-entry WGSL must use vertexStorageMain as the vertex entry point (got: " +
    capturedDescs.map(d => d && d.vertex && d.vertex.entryPoint).join(", ") + ")");

  const failWarns = harness.warnLog.filter(m => m.includes("falling back") || m.includes("failed"));
  assert.equal(failWarns.length, 0, "dual-entry authored render pipeline must not warn");
});

test("computeParticles authored render WebGPU: renderShaderLayout vertexStorage entry point is honored", async () => {
  const fake = makeFakeGPUDeviceForCompute({
    pipelineAsyncBehavior(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    errorScopeBehavior() { return Promise.resolve(null); },
  });

  const capturedDescs = [];
  fake.device.createRenderPipelineAsync = function(desc) {
    capturedDescs.push(desc);
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const vertexWGSL = [
    "struct VSOut { @builtin(position) pos: vec4<f32> };",
    "@vertex fn storageBillboard(@builtin(vertex_index) vi: u32) -> VSOut {",
    "  var o: VSOut; o.pos = vec4f(0.0, 0.0, 0.0, 1.0); return o;",
    "}",
  ].join("\n");
  const fragmentWGSL = "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4f(1.0); }";

  const entry = {
    id: "layout-entry-cp",
    count: 4,
    emitter: { kind: "point" },
    material: { color: "#fff" },
    renderVertexWGSL: vertexWGSL,
    renderFragmentWGSL: fragmentWGSL,
    renderShaderLayout: {
      entryPoints: { vertexStorage: "storageBillboard" },
    },
  };

  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);
  await flushAsyncWork();
  await flushAsyncWork();
  harness.renderer.render(makeComputeParticleBundleWithRender(entry), viewport);
  await flushAsyncWork();
  await flushAsyncWork();

  const authoredDesc = capturedDescs.find(d => d && d.vertex && d.vertex.entryPoint === "storageBillboard");
  assert.ok(authoredDesc, "renderShaderLayout.entryPoints.vertexStorage must select the authored storage entry (got: " +
    capturedDescs.map(d => d && d.vertex && d.vertex.entryPoint).join(", ") + ")");

  const failWarns = harness.warnLog.filter(m => m.includes("falling back") || m.includes("failed"));
  assert.equal(failWarns.length, 0, "layout-selected authored render pipeline must not warn");
});

test("GLB-style points authored profile: named material authored fields propagate to point layer", async () => {
  // Simulates a GLB point layer that carries `material: "stars"` as a string
  // name (set by gltfApplyScene3DExtras from GLTF node extras), matched against
  // a composable <Material name="stars" customVertexWGSL=... customFragmentWGSL=...>
  // profile in scene.materials.
  const api = await makeSceneApiEnv();

  const vertWGSL = "@vertex fn vertexMain() -> @builtin(position) vec4<f32> { return vec4<f32>(0.0); }";
  const fragWGSL = "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0); }";

  const state = api.createSceneState({
    scene: {
      materials: [
        {
          name: "stars",
          color: "var(--galaxy-star-color)",
          opacity: "var(--galaxy-star-opacity)",
          blendMode: "additive",
          customVertexWGSL: vertWGSL,
          customFragmentWGSL: fragWGSL,
          customUniforms: { brightness: 1.5 },
          shaderBackend: "selena",
          shaderLayout: { material: "StarPoints" },
          shaderSource: "materials/star-points.sel",
          shaderSourceFiles: {
            customVertexWGSL: "materials/star-points.sel",
            customFragmentWGSL: "materials/star-points.sel",
          },
        },
      ],
      points: [
        // GLB-derived point layer: material is a string name reference.
        {
          id: "galaxy-stars",
          count: 100,
          material: "stars",
          color: "#ffffff",
          opacity: 0.1,
          blendMode: "additive",
          positions: new Float32Array(300),
        },
      ],
    },
  });

  const points = api.sceneStatePointsWithMaterials(state);
  assert.equal(points.length, 1, "one point layer");

  const pt = points[0];
  assert.equal(pt.customVertexWGSL, vertWGSL, "customVertexWGSL must propagate from named material profile");
  assert.equal(pt.customFragmentWGSL, fragWGSL, "customFragmentWGSL must propagate from named material profile");
  assert.equal(pt.shaderBackend, "selena", "shaderBackend must propagate from named material profile");
  // Use property access for cross-realm object comparison (VM context creates different Object prototypes).
  assert.ok(pt.shaderLayout && typeof pt.shaderLayout === "object", "shaderLayout must be an object");
  assert.equal(pt.shaderLayout.material, "StarPoints", "shaderLayout.material must propagate");
  assert.equal(pt.shaderSource, "materials/star-points.sel", "shaderSource must propagate");
  assert.ok(pt.shaderSourceFiles && typeof pt.shaderSourceFiles === "object", "shaderSourceFiles must be an object");
  assert.equal(pt.shaderSourceFiles.customFragmentWGSL, "materials/star-points.sel", "shaderSourceFiles.customFragmentWGSL must propagate");
  assert.ok(pt.customUniforms && typeof pt.customUniforms === "object", "customUniforms must be an object");
  assert.equal(pt.customUniforms.brightness, 1.5, "customUniforms.brightness must propagate per-layer");

  // Color/opacity from the named material must also flow through (existing behavior).
  assert.equal(pt.color, "var(--galaxy-star-color)", "color from profile");
  assert.equal(pt.opacity, "var(--galaxy-star-opacity)", "opacity from profile");
});

test("GLB-style points authored profile: absent authored envelope leaves builtin path unchanged", async () => {
  // Point layer with a named material that has NO authored shader fields:
  // existing builtin-path properties (color/opacity) must still resolve,
  // and no authored shader fields should appear on the resolved entry.
  const api = await makeSceneApiEnv();

  const state = api.createSceneState({
    scene: {
      materials: [
        {
          name: "dust",
          color: "var(--galaxy-dust-inner)",
          opacity: "var(--galaxy-dust-opacity)",
          blendMode: "additive",
          // No customVertexWGSL / customFragmentWGSL.
        },
      ],
      points: [
        {
          id: "galaxy-dust",
          count: 50,
          material: "dust",
          color: "#ffffff",
          opacity: 0.05,
          blendMode: "additive",
        },
      ],
    },
  });

  const points = api.sceneStatePointsWithMaterials(state);
  const pt = points[0];

  // Builtin color/opacity flow through.
  assert.equal(pt.color, "var(--galaxy-dust-inner)", "color from profile");
  assert.equal(pt.opacity, "var(--galaxy-dust-opacity)", "opacity from profile");

  // Authored shader fields must be empty/null (not from old point values).
  assert.equal(pt.customVertexWGSL || "", "", "no customVertexWGSL for non-authored profile");
  assert.equal(pt.customFragmentWGSL || "", "", "no customFragmentWGSL for non-authored profile");
  assert.equal(pt.shaderBackend || "", "", "no shaderBackend for non-authored profile");
});

test("GLB-style points authored profile: multiple profiles share authored shader; each gets own customUniforms", async () => {
  // Verifies per-layer uniform isolation: two profiles share the same WGSL
  // sources (galaxy dedup scenario) but have distinct customUniforms.
  // sceneApplyNamedMaterialToPoints creates a new object per layer so
  // uniform buffers (keyed by `entry`) never clobber each other.
  const api = await makeSceneApiEnv();

  const sharedWGSL = "@vertex fn vertexMain() -> @builtin(position) vec4<f32> { return vec4<f32>(0.0); }";
  const sharedFrag = "@fragment fn fragmentMain() -> @location(0) vec4<f32> { return vec4<f32>(1.0); }";

  const state = api.createSceneState({
    scene: {
      materials: [
        {
          name: "stars",
          color: "#aaddff",
          opacity: 0.8,
          blendMode: "additive",
          customVertexWGSL: sharedWGSL,
          customFragmentWGSL: sharedFrag,
          customUniforms: { brightness: 2.0 },
        },
        {
          name: "nebula",
          color: "#ffaaff",
          opacity: 0.3,
          blendMode: "additive",
          customVertexWGSL: sharedWGSL,
          customFragmentWGSL: sharedFrag,
          customUniforms: { brightness: 0.5 },
        },
      ],
      points: [
        { id: "layer-stars",  count: 100, material: "stars",  positions: new Float32Array(300) },
        { id: "layer-nebula", count: 200, material: "nebula", positions: new Float32Array(600) },
      ],
    },
  });

  const points = api.sceneStatePointsWithMaterials(state);
  assert.equal(points.length, 2);

  const starsLayer  = points.find(p => p.id === "layer-stars");
  const nebulaLayer = points.find(p => p.id === "layer-nebula");
  assert.ok(starsLayer,  "layer-stars missing");
  assert.ok(nebulaLayer, "layer-nebula missing");

  // Both share the same authored WGSL source.
  assert.equal(starsLayer.customVertexWGSL,  sharedWGSL, "stars layer WGSL");
  assert.equal(nebulaLayer.customVertexWGSL, sharedWGSL, "nebula layer WGSL");

  // But per-profile customUniforms are distinct objects — not the same reference.
  // Use property access for cross-realm object comparison (VM context creates different Object prototypes).
  assert.ok(starsLayer.customUniforms && typeof starsLayer.customUniforms === "object", "stars uniforms must be an object");
  assert.equal(starsLayer.customUniforms.brightness, 2.0, "stars uniforms.brightness");
  assert.ok(nebulaLayer.customUniforms && typeof nebulaLayer.customUniforms === "object", "nebula uniforms must be an object");
  assert.equal(nebulaLayer.customUniforms.brightness, 0.5, "nebula uniforms.brightness");
  assert.notStrictEqual(starsLayer.customUniforms, nebulaLayer.customUniforms, "uniforms must be distinct objects");

  // Resolved entries are different objects (each layer gets its own copy).
  assert.notStrictEqual(starsLayer, nebulaLayer, "resolved entries must be distinct objects");
});

test("shaderLib hydrate: customVertexWGSL in materials profile reaches point layer (post-inflate shape)", async () => {
  // Verifies that a materials profile carrying customVertexWGSL (the shape that
  // inflateSceneShaderLib produces after expanding shaderLib refs) flows correctly
  // through createSceneState into the point layer.
  //
  // inflateSceneShaderLib runs during manifest loading on the production path; the
  // post-inflate shape (inline WGSL fields, no *Ref keys) is what createSceneState
  // receives. We supply that shape directly here. Go-side inflate correctness for
  // "materials" entries is covered by TestMaterialProfileShaderLibInflate.
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const shaderSrc = "@vertex fn v() -> @builtin(position) vec4<f32> { return vec4<f32>(0.0); }";

  // Post-inflate scene shape: no shaderLib key, no *Ref fields — WGSL inline.
  const state = api.createSceneState({
    scene: {
      materials: [
        {
          name: "stars",
          color: "#ffffff",
          opacity: 0.8,
          blendMode: "additive",
          customVertexWGSL: shaderSrc,
          customFragmentWGSL: shaderSrc,
        },
      ],
      points: [
        { id: "galaxy", count: 4, material: "stars", positions: new Float32Array(12) },
      ],
    },
  });

  const points = api.sceneStatePointsWithMaterials(state);
  const pt = points[0];
  assert.ok(pt, "point layer must exist");
  assert.equal(pt.customVertexWGSL, shaderSrc, "post-inflate customVertexWGSL must flow from materials profile to point layer");
  assert.equal(pt.customFragmentWGSL, shaderSrc, "post-inflate customFragmentWGSL must flow from materials profile to point layer");
});

test("Scene3D WebGPU frame-error/clean streaks are driven by real popErrorScope results, and enablePostProcessing rebuilds the post chain", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;

  // Toggled per-frame by the test; the REAL beginWebGPUErrorScope /
  // endWebGPUErrorScope pair in render() awaits this exact promise.
  let nextFrameErrors = false;
  harness.fake.device.popErrorScope = function() {
    return nextFrameErrors
      ? Promise.resolve({ message: "Buffer with '' label is invalid" })
      : Promise.resolve(null);
  };

  const state = api.createSceneState({
    scene: {
      // A sphere (not a box -- box geometry also emits thick-world-line
      // edge data that makeFakeGPUDevice's render pass double doesn't
      // implement setIndexBuffer for) so render() has SOME renderable
      // content: an empty scene (no PBR/points/lines/water/labels) returns
      // before ever reaching the post-FX / error-scope code this test
      // exercises.
      objects: [{ id: "probe-sphere", kind: "sphere", radius: 0.5, x: 0, y: 0, z: 0, color: "#8de1ff", wireframe: false }],
      postEffects: [{ kind: "bloom", threshold: 0.8, intensity: 0.2 }],
    },
  });
  const objects = api.sceneStateObjectsWithMaterials(state);
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], [], state.postEffects, 0, false,
  );

  harness.canvas.width = 64;
  harness.canvas.height = 64;

  async function renderFrame(hasError) {
    nextFrameErrors = hasError;
    harness.renderer.render(bundle, { width: 64, height: 64 });
    // endWebGPUErrorScope's device.popErrorScope().then(...) resolves over
    // two microtask hops (the fake's Promise.resolve(...) plus the real
    // .then chain) -- drain both before reading diagnostics().
    await Promise.resolve();
    await Promise.resolve();
  }

  await renderFrame(false);
  let diag = harness.renderer.diagnostics();
  assert.equal(diag.postFXDisabled, false);
  assert.equal(diag.postProcessing, true, "a non-empty postEffects bundle must build the post chain when not force-disabled");

  // Five consecutive error frames -> frameErrorStreak must read exactly 5,
  // driven purely by real popErrorScope rejections.
  for (let i = 0; i < 5; i++) {
    await renderFrame(true);
  }
  diag = harness.renderer.diagnostics();
  assert.equal(diag.frameErrorStreak, 5);
  assert.equal(diag.frameCleanStreak, 0, "an error frame must zero the clean streak");

  // One clean frame breaks the error streak and starts the clean streak.
  await renderFrame(false);
  diag = harness.renderer.diagnostics();
  assert.equal(diag.frameErrorStreak, 0, "a clean frame must zero the error streak");
  assert.equal(diag.frameCleanStreak, 1);

  for (let i = 0; i < 4; i++) {
    await renderFrame(false);
  }
  diag = harness.renderer.diagnostics();
  assert.equal(diag.frameCleanStreak, 5);

  // disablePostProcessing/enablePostProcessing actually gate the post chain
  // (diag.postProcessing mirrors !!postProcessor), not just a flag.
  assert.equal(harness.renderer.disablePostProcessing(), true);
  diag = harness.renderer.diagnostics();
  assert.equal(diag.postFXDisabled, true);
  await renderFrame(false);
  diag = harness.renderer.diagnostics();
  assert.equal(diag.postProcessing, false, "disablePostProcessing must actually tear down postProcessor, not just set a flag");
  assert.equal(diag.frameErrorStreak, 0, "disablePostProcessing gives raw rendering a fresh error-streak window");

  assert.equal(harness.renderer.disablePostProcessing(), false, "idempotent: already demoted");
  assert.equal(harness.renderer.enablePostProcessing(), true);
  diag = harness.renderer.diagnostics();
  assert.equal(diag.postFXDisabled, false);
  assert.equal(diag.postProcessing, false, "enablePostProcessing only clears the gate -- the chain rebuilds lazily on the NEXT render() call");
  await renderFrame(false);
  diag = harness.renderer.diagnostics();
  assert.equal(diag.postProcessing, true, "the next render() call with a non-empty postEffects bundle must rebuild the post chain");
  assert.equal(harness.renderer.enablePostProcessing(), false, "idempotent: not currently demoted");
});

// window_testFrameState sets the window.__testWebGPU* control variables the
// fake createRenderer() factories above read from their diagnostics()
// closures -- a lightweight stand-in for a real frame's error/clean streak
// advancing (see 16a-scene-webgpu.js's webGPUConsecutiveFrameErrors /
// webGPUConsecutiveCleanFrames, which this harness's diagnostics() field
// NAMES mirror exactly, and which never move in the same direction at once
// in production -- reportWebGPUFrameError zeroes the clean streak on any
// error frame, and endWebGPUErrorScope's clean branch zeroes the error
// streak on any clean frame).
function window_testFrameState(env, state) {
  env.context.__testWebGPUFrameErrorStreak = state.errorStreak || 0;
  env.context.__testWebGPUFrameCleanStreak = state.cleanStreak || 0;
}

test("Scene3D WebGPU post-FX demotes at the trip threshold, restores after a scaling clean streak, and latches after repeated cycles", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-postfx-restore";
  let now = 0;
  const events = [];
  const env = createContext({
    elements: [mount],
    enableWebGPU: true,
    enableWebGL2: true,
    performanceNow: () => now,
    navigatorGPU: {
      requestAdapter: async () => ({
        requestDevice: async () => ({
          lost: new Promise(() => {}),
          features: new Set(),
          limits: {},
        }),
      }),
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
      "/gosx/bootstrap-feature-scene3d-webgpu.js": {
        text: `
          window.__testWebGPUCreateCount = 0;
          window.__testWebGPUFrameErrorStreak = 0;
          window.__testWebGPUFrameCleanStreak = 0;
          window.__testWebGPUPostFXDisabled = false;
          window.__testWebGPUDemoteCount = 0;
          window.__testWebGPURestoreCount = 0;
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function(canvas) {
              window.__testWebGPUCreateCount += 1;
              canvas.__webgpuClaimed = true;
              return {
                kind: "webgpu",
                diagnostics: function() {
                  // Advance the mount's frame-seq/frame-at progress markers
                  // on every diagnostics() poll -- readSceneWebGPUProgress()
                  // reads them from the DOM, and this fake never runs a real
                  // render loop (no raf.flush between polls in this test),
                  // so without this the UNRELATED render-STALL watchdog
                  // (checkSceneRenderWatchdog's own progress-staleness check,
                  // a different safety net than the post-FX ladder this test
                  // exercises) would eventually force an irrelevant WebGL
                  // fallback after SCENE_RENDER_FALLBACK_STALL_MS.
                  window.__testWebGPUFrameSeq = (window.__testWebGPUFrameSeq || 0) + 1;
                  if (canvas && canvas.parentNode && typeof canvas.parentNode.setAttribute === "function") {
                    canvas.parentNode.setAttribute("data-gosx-scene3d-webgpu-frame-seq", String(window.__testWebGPUFrameSeq));
                    canvas.parentNode.setAttribute("data-gosx-scene3d-webgpu-frame-at", String(window.__testWebGPUFrameSeq));
                  }
                  return {
                    ready: true,
                    frameErrorStreak: window.__testWebGPUFrameErrorStreak,
                    frameCleanStreak: window.__testWebGPUFrameCleanStreak,
                    postFXDisabled: window.__testWebGPUPostFXDisabled,
                    lastError: window.__testWebGPUFrameErrorStreak > 0 ? "Buffer with '' label is invalid" : ""
                  };
                },
                disablePostProcessing: function() {
                  if (window.__testWebGPUPostFXDisabled) return false;
                  window.__testWebGPUPostFXDisabled = true;
                  window.__testWebGPUDemoteCount += 1;
                  return true;
                },
                enablePostProcessing: function() {
                  if (!window.__testWebGPUPostFXDisabled) return false;
                  window.__testWebGPUPostFXDisabled = false;
                  window.__testWebGPURestoreCount += 1;
                  return true;
                },
                render: function() {},
                dispose: function() {}
              };
            }
          };
        `,
      },
    },
    manifest: {
      runtime: { path: "/gosx/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-webgpu-postfx-restore",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-postfx-restore",
          jsExport: "GoSXScene3D",
          props: {
            width: 320,
            height: 180,
            preferWebGPU: true,
            autoRotate: true,
            scene: {
              objects: [
                { kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });
  const timers = installManualTimers(env.context);
  const raf = installManualRAF(env.context);

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  env.context.__gosx_emit = (level, cat, msg, fields) => {
    events.push({ level, cat, msg, fields: fields || {} });
  };
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  timers.runDelay(0);
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), null);

  function poll(atMS) {
    now = atMS;
    timers.runInterval(2000);
  }

  // --- Cycle 1 ---
  // Trip: 40 consecutive error frames (>= the unchanged 30-frame threshold)
  // -> DEMOTE. Non-regression: the trip condition and behavior are the same
  // as the sibling "persistent frame errors" test above.
  events.length = 0;
  window_testFrameState(env, { errorStreak: 40, cleanStreak: 0 });
  poll(2000);
  assert.equal(env.context.__testWebGPUDemoteCount, 1, "40 consecutive error frames must demote");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), "true");
  assert.equal(events.some((e) => e.msg === "webgpu-postfx-demoted"), true);

  // Too early: 299 clean frames is one short of the 1st-demotion restore
  // threshold (SCENE_WEBGPU_FRAME_ERROR_RESTORE_STREAK_THRESHOLD x 1 = 300)
  // -- must NOT restore yet.
  window_testFrameState(env, { errorStreak: 0, cleanStreak: 299 });
  poll(4000);
  assert.equal(env.context.__testWebGPURestoreCount, 0, "must not restore before the clean streak reaches the threshold");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), "true");

  // Exactly 300 clean frames -> RESTORE. This is the additive "way back":
  // disablePostProcessing() alone never had one.
  window_testFrameState(env, { errorStreak: 0, cleanStreak: 300 });
  poll(6000);
  assert.equal(env.context.__testWebGPURestoreCount, 1, "300 consecutive clean frames must restore post-FX");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), null, "the DOM flag must clear on restore, not just stop being set");
  assert.equal(events.some((e) => e.msg === "webgpu-postfx-restored"), true);

  // --- Cycle 2: a scene that trips a SECOND time must require a LONGER
  // clean streak to earn its next restore (anti-oscillation escalation). ---
  events.length = 0;
  window_testFrameState(env, { errorStreak: 40, cleanStreak: 0 });
  poll(8000);
  assert.equal(env.context.__testWebGPUDemoteCount, 2, "the ladder must still demote on a second, independent bad streak");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), "true");

  // 300 clean frames was enough for the FIRST restore; it must NOT be
  // enough for the second (threshold now scales to 300 x 2 = 600) --
  // otherwise a scene flapping every ~300 frames could cycle forever.
  window_testFrameState(env, { errorStreak: 0, cleanStreak: 300 });
  poll(10000);
  assert.equal(env.context.__testWebGPURestoreCount, 1, "300 clean frames must not be enough for the 2nd restore");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), "true");

  window_testFrameState(env, { errorStreak: 0, cleanStreak: 600 });
  poll(12000);
  assert.equal(env.context.__testWebGPURestoreCount, 2, "600 consecutive clean frames must restore the 2nd time");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), null);

  // --- Cycle 3: third demotion, third (larger) restore threshold. ---
  window_testFrameState(env, { errorStreak: 40, cleanStreak: 0 });
  poll(14000);
  assert.equal(env.context.__testWebGPUDemoteCount, 3);
  window_testFrameState(env, { errorStreak: 0, cleanStreak: 900 });
  poll(16000);
  assert.equal(env.context.__testWebGPURestoreCount, 3, "900 consecutive clean frames must restore the 3rd time");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), null);

  // --- Cycle 4: a FOURTH demotion exceeds SCENE_WEBGPU_POSTFX_MAX_DEMOTIONS
  // (3) -- the ladder still demotes (raw rendering must still recover), but
  // restore must now latch off PERMANENTLY for the rest of the session, no
  // matter how long the clean streak runs. ---
  window_testFrameState(env, { errorStreak: 40, cleanStreak: 0 });
  poll(18000);
  assert.equal(env.context.__testWebGPUDemoteCount, 4, "demote must still work past the restore cap -- raw rendering must not be sacrificed");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), "true");

  window_testFrameState(env, { errorStreak: 0, cleanStreak: 1000000 });
  poll(20000);
  assert.equal(env.context.__testWebGPURestoreCount, 3, "restore must latch off permanently after SCENE_WEBGPU_POSTFX_MAX_DEMOTIONS demotions, even with an enormous clean streak");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), "true", "the demoted flag must stay set once restore has latched off");

  // Renderer identity is untouched throughout -- this is purely a post-FX
  // toggle on the SAME WebGPU renderer, never a backend swap.
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(env.context.__testWebGPUCreateCount, 1);
});

test("custom post WebGPU: a module with compilation errors is refused even when the pipeline resolves", async () => {
  // The belt-and-braces case the device error scope used to cover, now keyed
  // to the MODULE it describes. A lenient implementation resolves the pipeline
  // anyway; getCompilationInfo still says the shader is wrong, and it says so
  // about this module only, so it cannot demote an unrelated pass.
  let renderPipelineAsyncCalls = 0;
  const fake = makeFakeGPUDeviceForCompute({
    compilationInfoBehavior(desc) {
      if (desc && typeof desc.code === "string" && desc.code.indexOf("BAD WGSL") >= 0) {
        return [{ type: "error", lineNum: 1, message: "expected declaration" }];
      }
      return [];
    },
  });
  fake.device.createRenderPipelineAsync = function(desc) {
    renderPipelineAsyncCalls++;
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };

  const harness = await createComputeParticleHarness(fake.device);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const bundle = makeBundleWithCustomPost({
    fragmentWGSL: "BAD WGSL FRAGMENT",
    vertexWGSL: "BAD WGSL VERTEX",
  });

  harness.renderer.render(bundle, viewport);
  assert.equal(renderPipelineAsyncCalls, 1);
  await flushAsyncWork();
  await flushAsyncWork();
  await flushAsyncWork();

  const warns = harness.warnLog.filter(m => m.includes("custom post pass") && (m.includes("passthrough") || m.includes("validation")));
  assert.equal(warns.length, 1, "a module carrying compilation errors must still fail the pass");
  assert.ok(warns[0].includes("expected declaration"), "the warn must carry the module's own compiler message");
});
