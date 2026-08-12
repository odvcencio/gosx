"use strict";
// The WebGPU adapter and device probe: optional-feature negotiation, retry
// after an empty or failed device request, and lost-device reacquisition.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapRuntimeSource,
  bootstrapFeatureEnginesSource,
  bootstrapFeatureScene3DSource,
  createContext,
  runScript,
  flushAsyncWork,
  readSceneMountSrc,
  freshFeatureBundleSource,
} = require("./runtime-test-harness.js");

test("Scene3D animation loop supports foreground frame caps", () => {
  const mount = readSceneMountSrc();

  assert.match(mount, /function sceneAnimationFrameIntervalMS\(\)/);
  assert.match(mount, /props && props\.frameIntervalMS/);
  assert.match(mount, /props && props\.maxFrameRate/);
  assert.match(mount, /props && props\.maxFPS/);
  assert.match(mount, /scheduleNextAnimationFrame\(\);\n\s+return;\n\s+\}/);
  assert.match(mount, /lastAnimationFrameAt = typeof now === "number" \? now : 0;/);
});

test("Scene3D resource readiness is canvas-scoped and detach-safe", () => {
  const mount = readSceneMountSrc();
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(mount, /target\.addEventListener\("gosx:scene3d:resource-ready", onSceneResourceReady\)/);
  assert.match(mount, /target\.removeEventListener\("gosx:scene3d:resource-ready", onSceneResourceReady\)/);
  assert.match(mount, /function onSceneResourceReady\(\)[\s\S]{0,120}!disposed[\s\S]{0,120}scheduleRender\("resource-ready"\)/);
  assert.match(webgl, /new CustomEvent\("gosx:scene3d:resource-ready"\)/);
  assert.match(webgpu, /new CustomEvent\("gosx:scene3d:resource-ready"\)/);
});

test("Scene3D instanced meshes are WebGPU-native", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const mount = readSceneMountSrc();

  assert.match(webgpu, /var WGSL_PBR_INSTANCED_VERTEX = \[/);
  assert.match(webgpu, /var WGSL_SHADOW_INSTANCED_VERTEX = \[/);
  assert.match(webgpu, /stepMode: "instance"[\s\S]*shaderLocation: 4[\s\S]*shaderLocation: 7/);
  assert.match(webgpu, /shaderLocation: 8/);
  assert.match(webgpu, /function wgpuCreatePBRInstancedPipeline/);
  assert.match(webgpu, /function drawInstancedMeshes\(pass, meshList, materials/);
  assert.match(webgpu, /pass\.draw\(geom\.vertexCount,\s*instanceCount\)/);
  assert.match(webgpu, /function getShadowInstancedPipeline/);
  assert.match(webgpu, /createMaterialBindGroup\(\s*mat,\s*receiveShadow,\s*materialOwner,\s*obj\.retainedGeometry \? webGPUObjectModelMatrix\(obj\) : null,\s*obj\.retainedGeometry \? obj\.modelScaleSigns : null\s*\)/);
  assert.doesNotMatch(webgpu, /bundle\.instancedMeshes[\s\S]{0,140}return false/);
  assert.doesNotMatch(mount, /instanced-meshes/);
});

test("Scene3D WebGPU PBR meshes do not cull double-sided GLB surfaces", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /function wgpuCreatePBRPipeline/);
  assert.match(webgpu, /label: "gosx-pbr-" \+ blendMode[\s\S]*primitive: \{ topology: "triangle-list", cullMode: "none" \}/);
  assert.match(webgpu, /function wgpuCreatePBRInstancedPipeline/);
  assert.match(webgpu, /label: "gosx-pbr-instanced-" \+ blendMode[\s\S]*primitive: \{ topology: "triangle-list", cullMode: "none" \}/);
  assert.doesNotMatch(webgpu, /label: "gosx-pbr-" \+ blendMode[\s\S]{0,900}cullMode: "back"/);
  assert.doesNotMatch(webgpu, /label: "gosx-pbr-instanced-" \+ blendMode[\s\S]{0,900}cullMode: "back"/);
});

test("Scene3D WebGPU Selena mesh pipeline honors obj.doubleSided (cullMode: none)", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // getSelenaPipeline's own default stays "back" (unchanged) when the
  // caller passes no cullMode option -- drawPBRObjects is the caller that
  // now conditions its options argument on obj.doubleSided.
  assert.match(webgpu, /var pipelineCullMode = options && typeof options\.cullMode === "string" && options\.cullMode \? options\.cullMode : "back";/);
  assert.match(webgpu, /var selenaPipelineOptions = obj\.doubleSided \? \{ cullMode: "none" \} : null;/);
  assert.match(webgpu, /getSelenaPipeline\(mat, blendMode, depthWrite, selenaPipelineOptions\)/);
  assert.match(webgpu, /getSelenaSkinnedPipeline\(mat, blendMode, depthWrite, selenaPipelineOptions\)/);
});

test("Scene3D world lines and textured surfaces are WebGPU-native", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const mount = readSceneMountSrc();

  assert.match(webgpu, /var WGSL_SCENE_WORLD_COLOR_VERTEX = \[/);
  assert.match(webgpu, /var WGSL_SCENE_CLIP_COLOR_VERTEX = \[/);
  assert.match(webgpu, /var WGSL_SURFACE_VERTEX = \[/);
  assert.match(webgpu, /var WGSL_THICK_LINE_VERTEX = \[/);
  assert.match(webgpu, /function wgpuCreateSceneColorPipeline/);
  assert.match(webgpu, /primitive: \{ topology: topology \}/);
  assert.match(webgpu, /function wgpuCreateSurfacePipeline/);
  assert.match(webgpu, /function wgpuCreateThickLinePipeline/);
  assert.match(webgpu, /function drawWorldLineEntries\(renderPass, entries, passName, frameBindGroup\)/);
  assert.match(webgpu, /function drawThickWorldLineEntries\(renderPass, record, passName, frameBindGroup\)/);
  assert.match(webgpu, /function drawScreenLines\(renderPass, bundle, frameBindGroup\)/);
  assert.match(webgpu, /function drawSurfaceEntries\(renderPass, bundle, materials, passName, frameBindGroup\)/);
  assert.match(webgpu, /buildSceneWorldDrawPlan\(bundle, worldDrawScratch\)/);
  assert.match(webgpu, /expandSceneThickLineIntoScratch\(/);
  assert.match(webgpu, /setIndexBuffer\(indexBuffer, "uint16"\)/);
  assert.match(webgpu, /getSceneColorPipeline\(entry\.space === "clip" \? "clip" : "world", topology, blend, depthWrite\)/);
  assert.doesNotMatch(webgpu, /Array\.isArray\(bundle && bundle\.surfaces\)[\s\S]{0,120}return false/);
  assert.doesNotMatch(webgpu, /Array\.isArray\(bundle && bundle\.lines\)[\s\S]{0,120}return false/);
  assert.doesNotMatch(mount, /sceneNumber\(entry\.lineWidth/);
  assert.doesNotMatch(mount, /source && source\.worldLineWidths/);
  assert.doesNotMatch(mount, /return "surfaces"/);
  assert.doesNotMatch(mount, /return "lines"/);
  assert.match(mount, /return "line-styles"/);
});

test("Scene3D WebGPU supports tiered MSAA render targets", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const mount = readSceneMountSrc();
  const probe = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16z-scene-webgpu-probe.js"), "utf8");

  assert.match(webgpu, /function createSceneWebGPURenderer\(canvas, options\)/);
  assert.match(webgpu, /function ensureMSAAColor\(width, height, sampleCount\)/);
  assert.match(webgpu, /sampleCount: sampleCount/);
  assert.match(webgpu, /multisample: \{ count: Math\.max\(1, Math\.floor\(sampleCount \|\| 1\)\) \}/);
  assert.match(webgpu, /activeSampleCount = sampleCount/);
  assert.match(webgpu, /mainColorAttachment\.resolveTarget = mainResolveView/);
  assert.match(webgpu, /function sceneWebGPUCanvasConfiguration\(\)/);
  assert.match(webgpu, /colorSpace: activePresentation\.colorSpace/);
  assert.match(webgpu, /config\.toneMapping = \{ mode: activePresentation\.toneMappingMode \}/);
  assert.match(mount, /function sceneWebGPUOptions\(props, capability\)/);
  assert.match(mount, /msaaSamples: requestedSamples > 1 \? 4 : \(antialias \? 4 : 1\)/);
  assert.match(mount, /powerPreference: sceneWebGPUPowerPreference/);
  assert.match(mount, /presentation: sceneWebGPUPresentationOptions\(props\)/);
  assert.match(probe, /sceneWebGPUProbeOptionsFromManifest/);
  assert.match(probe, /sceneWebGPURequiredFeaturesFromManifest/);
  assert.match(probe, /sceneWebGPURequiredLimitsFromManifest/);
  assert.match(probe, /descriptor\.requiredLimits = Object\.assign/);
  assert.match(probe, /requestAdapter\(adapterRequest\)/);
  assert.match(probe, /WEBGPU_LOST_REPROBE_BACKOFF_MS/);
  assert.match(probe, /function sceneWebGPURecordProbeLoss/);
  assert.match(probe, /device lost repeatedly; reprobe backed off/);
  assert.match(probe, /lostProbeBackoffUntil/);
});

test("Scene3D WebGPU probe negotiates optional features and exposes diagnostics", async () => {
  let requestedDescriptor = null;
  let requestedAdapterOptions = null;
  const deviceFeatures = new Set();
  const adapterLimits = {
    maxTextureDimension2D: 8192,
    maxComputeWorkgroupSizeX: 256,
    maxComputeWorkgroupsPerDimension: 65535,
  };
  const deviceLimits = {
    maxTextureDimension2D: 4096,
    maxComputeWorkgroupSizeX: 128,
  };
  const adapter = {
    features: new Set([
      "timestamp-query",
      "indirect-first-instance",
      "shader-f16",
      "texture-compression-bc",
      "texture-compression-bc-sliced-3d",
      "texture-compression-astc-sliced-3d",
      "subgroups",
      "subgroups-f16",
      "future-rendering-mode",
    ]),
    limits: adapterLimits,
    info: {
      vendor: "gosx-test",
      architecture: "test-gpu",
      subgroupMinSize: 4,
      subgroupMaxSize: 32,
    },
    requestDevice: async (descriptor = {}) => {
      requestedDescriptor = descriptor;
      for (const feature of descriptor.requiredFeatures || []) {
        deviceFeatures.add(feature);
      }
      return {
        lost: new Promise(() => {}),
        features: deviceFeatures,
        limits: deviceLimits,
      };
    },
  };
  const env = createContext({
    enableWebGPU: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
    },
    navigatorGPU: {
      requestAdapter: async (options) => {
        requestedAdapterOptions = options;
        return adapter;
      },
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgpu-probe",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "probe-scene",
          requiredCapabilities: [
            "webgpu",
            "webgpu:limit:maxComputeWorkgroupSizeX>=128",
            "webgpu:device-limit:maxTextureDimension2D>=4096",
            "webgpu:adapter-limit:maxTextureDimension2D>=8192",
            "webgpu-feature:future-rendering-mode",
          ],
          props: {
            webgpuPowerPreference: "high-performance",
            webgpuOptionalFeatures: true,
          },
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);

  assert.equal(requestedAdapterOptions.powerPreference, "high-performance");
  // Compare the SET, not the order. requestDevice reads requiredFeatures as an
  // unordered collection, so pinning the order tests the loop that builds the
  // list rather than the contract the browser honours. This assertion used to
  // pin the order and broke when the block-texture features moved to the front,
  // even though the requested set was byte-for-byte the same eight names.
  //
  // The set still matters exactly: a feature missing here cannot be added after
  // requestDevice, so a texture-compression feature absent from this list means
  // every block-compressed upload throws on a device whose adapter supports it.
  assert.deepEqual(
    Array.from(requestedDescriptor.requiredFeatures).slice().sort(),
    [
      "future-rendering-mode",
      "indirect-first-instance",
      "shader-f16",
      "subgroups",
      "subgroups-f16",
      "texture-compression-bc",
      "texture-compression-bc-sliced-3d",
      "timestamp-query",
    ],
  );
  assert.equal(requestedDescriptor.requiredLimits.maxComputeWorkgroupSizeX, 128);
  assert.equal(requestedDescriptor.requiredLimits.maxTextureDimension2D, 4096);
  const diagnostics = env.context.__gosx_scene3d_webgpu_diagnostics();
  assert.equal(diagnostics.ready, true);
  assert.equal(diagnostics.adapterAvailable, true);
  assert.equal(diagnostics.deviceAvailable, true);
  assert.ok(diagnostics.supportedFeatures.includes("texture-compression-astc-sliced-3d"));
  assert.equal(diagnostics.requestedFeatures.includes("texture-compression-astc-sliced-3d"), false);
  assert.ok(diagnostics.deviceFeatures.includes("timestamp-query"));
  assert.equal(diagnostics.adapterLimits.maxTextureDimension2D, 8192);
  assert.equal(diagnostics.deviceLimits.maxComputeWorkgroupSizeX, 128);
  assert.equal(diagnostics.adapterInfo.vendor, "gosx-test");
  assert.equal(diagnostics.adapterInfo.subgroupMaxSize, 32);
  assert.equal(diagnostics.probeOptions.powerPreference, "high-performance");
  assert.deepEqual(Array.from(diagnostics.requiredFeatures), ["future-rendering-mode"]);
  assert.equal(diagnostics.requiredLimits.maxComputeWorkgroupSizeX, 128);
  assert.equal(diagnostics.requiredLimits.maxTextureDimension2D, 4096);
  assert.equal(typeof env.context.__gosx_scene3d_api.sceneWebGPUDiagnostics, "function");
  // Regression guard: extractFrustumPlanesJS is hoisted into 11-scene-math.js
  // (base scene3d bundle) but USED by the separate scene3d-webgpu chunk's
  // instanced GPU cull. It MUST be exported on __gosx_scene3d_api so the webgpu
  // chunk's prefix can bridge it; otherwise the webgpu render path throws
  // "extractFrustumPlanesJS is not defined".
  assert.equal(typeof env.context.__gosx_scene3d_api.extractFrustumPlanesJS, "function");
  assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu:timestamp-query"), true);
  assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu-feature:future-rendering-mode"), true);
  assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu:texture-compression-astc-sliced-3d"), false);
  assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu:adapter-limit:maxTextureDimension2D>=8192"), true);
  assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu:device-limit:maxTextureDimension2D>=8192"), false);
  assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu:limit:maxComputeWorkgroupSizeX>=128"), true);
  assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu:limit:maxComputeWorkgroupSizeX>128"), false);
  assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu:missing-feature"), false);
});

test("Scene3D WebGPU probe keeps optional features opt-in for headless devices", async () => {
  let requestedDescriptor = null;
  const adapter = {
    features: new Set([
      "timestamp-query",
      "indirect-first-instance",
      "texture-compression-bc",
      "subgroups",
    ]),
    limits: {
      maxTextureDimension2D: 8192,
      maxComputeWorkgroupSizeX: 256,
    },
    requestDevice: async (descriptor) => {
      requestedDescriptor = descriptor;
      const requiredFeatures = descriptor && descriptor.requiredFeatures || [];
      return {
        lost: new Promise(() => {}),
        features: new Set(requiredFeatures),
        limits: {
          maxTextureDimension2D: 8192,
          maxComputeWorkgroupSizeX: 256,
        },
      };
    },
  };
  const env = createContext({
    enableWebGPU: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
    },
    navigatorGPU: {
      requestAdapter: async () => adapter,
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgpu-lean-probe",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "probe-scene",
          requiredCapabilities: ["webgpu"],
          props: {},
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);

  // The rule this test pins has TWO halves, and they differ by kind.
  //
  // A PERFORMANCE feature stays opt-in. timestamp-query, subgroups and
  // shader-f16 make an already-correct frame faster or better measured, so a
  // page that did not ask keeps a lean device and none are requested.
  //
  // A BLOCK-TEXTURE feature is NOT optional in the same sense. It is a
  // prerequisite for content the SERVER may ship: the asset pipeline emits
  // block-compressed KTX2 variants and the client selects one from the device
  // token set. A feature cannot be added after requestDevice, so a device
  // created without texture-compression-bc on a bc-capable adapter throws on
  // every block upload. Requesting a feature the adapter already reports costs
  // nothing when the page never uploads a block texture.
  //
  // So the descriptor now exists and carries exactly the compression features
  // the adapter offers, and nothing else.
  const requested = Array.from((requestedDescriptor && requestedDescriptor.requiredFeatures) || []);
  assert.deepEqual(requested, ["texture-compression-bc"],
    "a non-adaptive page must request the adapter's block-texture features and no performance features");

  const diagnostics = env.context.__gosx_scene3d_webgpu_diagnostics();
  assert.equal(diagnostics.ready, true);
  assert.ok(diagnostics.supportedFeatures.includes("timestamp-query"));
  // Array.from crosses the realm: diagnostics comes from the sandbox context,
  // so its arrays carry the sandbox Array.prototype and deepEqual reports
  // "same structure but not reference-equal" against a literal built here.
  const reported = Array.from(diagnostics.requestedFeatures);
  assert.deepEqual(reported, ["texture-compression-bc"]);
  // The performance features stay off, which is the half that must not regress.
  for (const perf of ["timestamp-query", "subgroups", "shader-f16", "indirect-first-instance"]) {
    assert.equal(reported.includes(perf), false,
      perf + " is a performance feature and must stay opt-in");
    assert.equal(env.context.__gosx_runtime_api.browserCapabilitySupported("webgpu:" + perf), false);
  }
});

test("Scene3D adaptive WebGPU requests only supported timestamp-query", async () => {
  let descriptor = null;
  const adapter = {
    features: new Set(["timestamp-query", "shader-f16", "subgroups"]),
    limits: {},
    requestDevice: async (next) => {
      descriptor = next || {};
      return { lost: new Promise(() => {}), features: new Set(descriptor.requiredFeatures || []), limits: {} };
    },
  };
  const env = createContext({
    enableWebGPU: true,
    fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource } },
    navigatorGPU: { requestAdapter: async () => adapter, getPreferredCanvasFormat: () => "rgba8unorm" },
    manifest: { engines: [{ component: "GoSXScene3D", props: { adaptiveQuality: { tier: "balanced" } } }] },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d-adaptive-probe.js");
  await flushAsyncWork();
  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);
  assert.deepEqual(Array.from(descriptor.requiredFeatures || []), ["timestamp-query"]);
  assert.deepEqual(Array.from(env.context.__gosx_scene3d_webgpu_diagnostics().requestedFeatures), ["timestamp-query"]);
});

test("Scene3D WebGPU probe retries empty device acquisition with a fresh adapter", async () => {
  let adapterRequests = 0;
  let deviceRequests = 0;
  const failingAdapter = {
    features: new Set(["timestamp-query"]),
    limits: {
      maxTextureDimension2D: 8192,
    },
    requestDevice: async (descriptor) => {
      deviceRequests++;
      assert.equal(descriptor, undefined);
      throw new Error("A valid external Instance reference no longer exists.");
    },
  };
  const retryAdapter = {
    features: new Set(["timestamp-query"]),
    limits: {
      maxTextureDimension2D: 8192,
    },
    info: {
      vendor: "retry-vendor",
    },
    requestDevice: async (descriptor) => {
      deviceRequests++;
      assert.equal(descriptor, undefined);
      return {
        lost: new Promise(() => {}),
        features: new Set(),
        limits: {
          maxTextureDimension2D: 8192,
        },
      };
    },
  };
  const env = createContext({
    enableWebGPU: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
    },
    navigatorGPU: {
      requestAdapter: async () => {
        adapterRequests++;
        return adapterRequests === 1 ? failingAdapter : retryAdapter;
      },
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgpu-retry-probe",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "probe-scene",
          requiredCapabilities: ["webgpu"],
          props: {},
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);
  assert.equal(adapterRequests, 2);
  assert.equal(deviceRequests, 2);
  const diagnostics = env.context.__gosx_scene3d_webgpu_diagnostics();
  assert.equal(diagnostics.ready, true);
  assert.equal(diagnostics.retryCount, 1);
  assert.match(diagnostics.warnings[0], /external Instance/);
  assert.equal(diagnostics.adapterInfo.vendor, "retry-vendor");
  assert.equal(diagnostics.requestedFeatures.length, 0);
  assert.equal(diagnostics.deviceFeatures.length, 0);
});

// v0.33.2: the fresh-adapter retry after a requestDevice failure (e.g. a
// memory-tight browser's "Not enough memory left") must request the bare
// device defaults, NOT repeat the manifest's requiredFeatures/requiredLimits
// — those are almost certainly unrelated to why the first request failed,
// and a memory-constrained browser is far more likely to grant a modest
// (no extra requirements) device than the exact same over-specified one.
test("Scene3D WebGPU probe retries a memory failure with a MINIMAL descriptor even when the first attempt required features/limits", async () => {
  let deviceRequests = 0;
  let firstDescriptor = "unset";
  let retryDescriptor = "unset";
  const adapterFeatures = new Set(["future-rendering-mode"]);
  const adapterLimits = { maxTextureDimension2D: 8192, maxComputeWorkgroupSizeX: 256 };
  const failingAdapter = {
    features: adapterFeatures,
    limits: adapterLimits,
    requestDevice: async (descriptor) => {
      deviceRequests++;
      firstDescriptor = descriptor;
      // Sanity: the first attempt DID carry the manifest's requirements —
      // this is the over-specified request a memory-tight browser rejects.
      throw new Error("Not enough memory left.");
    },
  };
  const retryAdapter = {
    features: adapterFeatures,
    limits: adapterLimits,
    info: { vendor: "retry-vendor" },
    requestDevice: async (descriptor) => {
      deviceRequests++;
      retryDescriptor = descriptor;
      return {
        lost: new Promise(() => {}),
        features: new Set(),
        limits: adapterLimits,
      };
    },
  };
  let adapterRequests = 0;
  const env = createContext({
    enableWebGPU: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
    },
    navigatorGPU: {
      requestAdapter: async () => {
        adapterRequests++;
        return adapterRequests === 1 ? failingAdapter : retryAdapter;
      },
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgpu-memory-retry-probe",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "probe-scene",
          requiredCapabilities: [
            "webgpu",
            "webgpu:limit:maxComputeWorkgroupSizeX>=128",
            "webgpu-feature:future-rendering-mode",
          ],
          props: {},
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);
  assert.equal(adapterRequests, 2, "must reacquire a fresh adapter after the memory failure");
  assert.equal(deviceRequests, 2);
  // The FIRST attempt is the over-specified one (proves the manifest's
  // requirements really were being requested, i.e. this test's premise
  // holds) — not undefined/minimal.
  assert.ok(firstDescriptor && Array.isArray(firstDescriptor.requiredFeatures) && firstDescriptor.requiredFeatures.length > 0,
    "first attempt must have requested the manifest's requiredFeatures");
  assert.ok(firstDescriptor.requiredLimits && Object.keys(firstDescriptor.requiredLimits).length > 0,
    "first attempt must have requested the manifest's requiredLimits");
  // The RETRY must be minimal: no requiredFeatures, no requiredLimits.
  assert.equal(retryDescriptor, undefined,
    "the fresh-adapter retry must request bare device defaults, not repeat the same requiredFeatures/requiredLimits");
  const diagnostics = env.context.__gosx_scene3d_webgpu_diagnostics();
  assert.equal(diagnostics.ready, true);
  assert.equal(diagnostics.retryCount, 1);
  assert.match(diagnostics.warnings[0], /Not enough memory left/);
  assert.equal(diagnostics.adapterInfo.vendor, "retry-vendor");
});

test("Scene3D WebGPU probe invalidates lost device and reacquires a fresh device", async () => {
  let adapterRequests = 0;
  let deviceRequests = 0;
  let resolveFirstLost = null;
  function makeDevice(name) {
    let resolveLost = null;
    const device = {
      name,
      lost: new Promise((resolve) => {
        resolveLost = resolve;
      }),
      features: new Set(),
      limits: {
        maxTextureDimension2D: 8192,
      },
    };
    if (name === "first") {
      resolveFirstLost = resolveLost;
    }
    return device;
  }
  function makeAdapter(name, vendor) {
    return {
      features: new Set(),
      limits: {
        maxTextureDimension2D: 8192,
      },
      info: {
        vendor,
      },
      requestDevice: async (descriptor) => {
        deviceRequests++;
        assert.equal(descriptor, undefined);
        return makeDevice(name);
      },
    };
  }
  const adapters = [
    makeAdapter("first", "initial-vendor"),
    makeAdapter("second", "recovered-vendor"),
  ];
  const env = createContext({
    enableWebGPU: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
    },
    navigatorGPU: {
      requestAdapter: async () => {
        adapterRequests++;
        return adapters[Math.min(adapterRequests - 1, adapters.length - 1)];
      },
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgpu-loss-reprobe",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "probe-scene",
          requiredCapabilities: ["webgpu"],
          props: {},
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);
  let probe = env.context.__gosx_scene3d_webgpu_probe();
  assert.equal(adapterRequests, 1);
  assert.equal(deviceRequests, 1);
  assert.equal(probe.ready, true);
  assert.equal(probe.device.name, "first");

  resolveFirstLost({ reason: "destroyed", message: "device gone" });
  await flushAsyncWork();

  const readyAfterLoss = await env.context.__gosx_scene3d_webgpu_probe_ready();
  probe = env.context.__gosx_scene3d_webgpu_probe();
  const diagnostics = env.context.__gosx_scene3d_webgpu_diagnostics();
  assert.equal(readyAfterLoss, true, "probe should reacquire WebGPU after the cached device is lost");
  assert.equal(adapterRequests, 2);
  assert.equal(deviceRequests, 2);
  assert.equal(probe.ready, true);
  assert.equal(probe.device.name, "second");
  assert.equal(probe.lost, null);
  assert.equal(diagnostics.adapterInfo.vendor, "recovered-vendor");
});

test("Scene3D WebGPU probe retries null adapter after lost-device reprobe", async () => {
  let now = 0;
  let adapterRequests = 0;
  let deviceRequests = 0;
  let resolveFirstLost = null;
  function makeDevice(name) {
    let resolveLost = null;
    const device = {
      name,
      lost: new Promise((resolve) => {
        resolveLost = resolve;
      }),
      features: new Set(),
      limits: {
        maxTextureDimension2D: 8192,
      },
    };
    if (name === "first") {
      resolveFirstLost = resolveLost;
    }
    return device;
  }
  function makeAdapter(name, vendor) {
    return {
      features: new Set(),
      limits: {
        maxTextureDimension2D: 8192,
      },
      info: {
        vendor,
      },
      requestDevice: async (descriptor) => {
        deviceRequests++;
        assert.equal(descriptor, undefined);
        return makeDevice(name);
      },
    };
  }
  const firstAdapter = makeAdapter("first", "initial-vendor");
  const recoveredAdapter = makeAdapter("recovered", "late-vendor");
  const env = createContext({
    enableWebGPU: true,
    performanceNow: () => now,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
    },
    navigatorGPU: {
      requestAdapter: async () => {
        adapterRequests++;
        if (adapterRequests === 1) {
          return firstAdapter;
        }
        if (adapterRequests === 2) {
          return null;
        }
        return recoveredAdapter;
      },
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgpu-late-reprobe",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "probe-scene",
          requiredCapabilities: ["webgpu"],
          props: {},
        },
      ],
    },
  });
  let probeReadyEvents = 0;
  env.context.addEventListener("gosx:scene3d:webgpu-probe-ready", () => {
    probeReadyEvents++;
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);
  assert.equal(adapterRequests, 1);
  assert.equal(deviceRequests, 1);

  resolveFirstLost({ reason: "destroyed", message: "device gone" });
  await flushAsyncWork();

  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), false);
  let diagnostics = env.context.__gosx_scene3d_webgpu_diagnostics();
  assert.equal(adapterRequests, 2);
  assert.equal(deviceRequests, 1);
  assert.equal(diagnostics.ready, false);
  assert.equal(diagnostics.error, "requestAdapter returned null");

  now = 1500;
  diagnostics = env.context.__gosx_scene3d_webgpu_diagnostics();
  assert.equal(diagnostics.ready, false);
  assert.equal(adapterRequests, 3);
  await flushAsyncWork();
  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);
  const probe = env.context.__gosx_scene3d_webgpu_probe();
  diagnostics = env.context.__gosx_scene3d_webgpu_diagnostics();
  assert.equal(deviceRequests, 2);
  assert.equal(probe.device.name, "recovered");
  assert.equal(probe.lost, null);
  assert.equal(diagnostics.adapterInfo.vendor, "late-vendor");
  assert.equal(probeReadyEvents, 1);
});
