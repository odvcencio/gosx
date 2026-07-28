"use strict";
// Render backend selection and recovery: the backend registry, WebGPU
// preference, the stalled-frame watchdog, device loss and the lazy WebGL chunk.
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
  FakeWebGLContext,
  FakeElement,
  createContext,
  installManualRAF,
  flushSceneInitialFrameBoundary,
  installManualTimers,
  runScript,
  flushAsyncWork,
  scene3dWebGLSplitManifest,
  makeFakeGPUDevice,
  freshFeatureBundleSource,
} = require("./runtime-test-harness.js");

test("bootstrap registers and selects Scene3D backends through registry", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const registry = env.context.__gosx_scene3d_api.sceneBackendRegistry;
  assert.equal(typeof registry.register, "function");
  assert.ok(registry.list().some((entry) => entry.kind === "webgl"));
  assert.ok(registry.list().some((entry) => entry.kind === "canvas2d"));

  const custom = registry.register("foo", {
    capabilities: ["foo"],
    create: () => ({ kind: "foo", render() {}, dispose() {} }),
  });
  assert.equal(registry.select({ foo: true, canvas2d: false, canvas: false, webgl: false, webgpu: false }).kind, custom.kind);
  registry.dispose("foo");
  assert.equal(registry.list().some((entry) => entry.kind === "foo"), false);
  assert.equal(registry.select({ canvas: false, canvas2d: false, webgl: false, webgl2: false, webgpu: false }), null);
});

test("selective Scene3D bootstrap prefers WebGPU before first renderer selection", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-default";
  let requestedAdapterOptions = null;
  const env = createContext({
    elements: [mount],
    enableWebGPU: true,
    enableWebGL2: true,
    navigatorGPU: {
      requestAdapter: async (options) => {
        requestedAdapterOptions = options;
        return {
          requestDevice: async () => ({
            lost: new Promise(() => {}),
            features: new Set(["timestamp-query"]),
            limits: {
              maxTextureDimension2D: 4096,
              maxComputeWorkgroupSizeX: 128,
            },
          }),
        };
      },
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
      "/gosx/bootstrap-feature-scene3d-webgpu.js": {
        text: `
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function(canvas, options) {
              window.__gosx_scene3d_webgpu_options = options;
              return {
                kind: "webgpu",
                diagnostics: function() {
                  var presentation = options && options.presentation || {};
                  return {
                    requestedFeatures: ["timestamp-query", "shader-f16"],
                    requiredFeatures: ["shader-f16"],
                    deviceFeatures: ["timestamp-query"],
                    requiredLimits: {
                      maxTextureDimension2D: 4096,
                      maxComputeWorkgroupSizeX: 128
                    },
                    activeSampleCount: 4,
                    targetFormat: "rgba16float",
                    presentationAlphaMode: presentation.alphaMode,
                    presentationColorSpace: presentation.colorSpace,
                    presentationToneMappingMode: presentation.toneMappingMode,
                    powerPreference: options && options.powerPreference,
                    adapterLimits: {
                      maxTextureDimension2D: 8192,
                      maxBufferSize: 268435456,
                      maxComputeWorkgroupSizeX: 256,
                      maxComputeWorkgroupsPerDimension: 65535
                    },
                    deviceLimits: {
                      maxTextureDimension2D: 4096,
                      maxBufferSize: 134217728,
                      maxComputeWorkgroupSizeX: 128,
                      maxComputeWorkgroupsPerDimension: 32768
                    },
                    adapterInfo: { vendor: "gosx-test", architecture: "unit" }
                  };
                },
                render: function() {},
                dispose: function() {}
              };
            }
          };
          window.__gosx_scene3d_webgpu_loaded = true;
        `,
      },
    },
    manifest: {
      runtime: { path: "/gosx/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-webgpu-default",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-default",
          jsExport: "GoSXScene3D",
          requiredCapabilities: [
            "webgpu",
            "webgpu:limit:maxComputeWorkgroupSizeX>=128",
            "webgpu:device-limit:maxTextureDimension2D>=4096",
          ],
          props: {
            width: 360,
            height: 220,
            autoRotate: false,
            webgpuAlphaMode: "opaque",
            webgpuColorSpace: "display-p3",
            webgpuToneMapping: "extended",
            webgpuPowerPreference: "high-performance",
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

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "prefer");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu", JSON.stringify({
    fetchCalls: env.fetchCalls,
    hasWebGPUAPI: Boolean(env.context.__gosx_scene3d_webgpu_api),
    webgpuProbe: env.context.__gosx_scene3d_webgpu_probe && env.context.__gosx_scene3d_webgpu_probe(),
    backends: env.context.__gosx_scene3d_api.sceneBackendRegistry.list().map((entry) => entry.kind),
  }));
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-features"), "timestamp-query,shader-f16");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-required-features"), "shader-f16");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-device-features"), "timestamp-query");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-required-limits"), "maxComputeWorkgroupSizeX=128,maxTextureDimension2D=4096");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-sample-count"), "4");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-target-format"), "rgba16float");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-presentation-alpha-mode"), "opaque");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-presentation-color-space"), "display-p3");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-presentation-tone-mapping"), "extended");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-power-preference"), "high-performance");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-adapter-limits"), "maxBufferSize=268435456,maxComputeWorkgroupSizeX=256,maxComputeWorkgroupsPerDimension=65535,maxTextureDimension2D=8192");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-device-limits"), "maxBufferSize=134217728,maxComputeWorkgroupSizeX=128,maxComputeWorkgroupsPerDimension=32768,maxTextureDimension2D=4096");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-adapter-max-texture-2d"), "8192");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-device-max-texture-2d"), "4096");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-adapter-max-buffer-size"), "268435456");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-device-max-buffer-size"), "134217728");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-adapter-max-compute-workgroup-size-x"), "256");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-device-max-compute-workgroup-size-x"), "128");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-adapter-max-compute-workgroups-per-dimension"), "65535");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-device-max-compute-workgroups-per-dimension"), "32768");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-adapter"), "gosx-test unit");
  assert.equal(env.context.__gosx_scene3d_webgpu_options.presentation.alphaMode, "opaque");
  assert.equal(env.context.__gosx_scene3d_webgpu_options.presentation.colorSpace, "display-p3");
  assert.equal(env.context.__gosx_scene3d_webgpu_options.presentation.toneMappingMode, "extended");
  assert.equal(env.context.__gosx_scene3d_webgpu_options.powerPreference, "high-performance");
  assert.equal(requestedAdapterOptions.powerPreference, "high-performance");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgpu.js"),
    true,
  );
  assert.equal((mount.children[0].contextCalls || []).some((call) => call.kind === "webgl" || call.kind === "webgl2"), false);
});

test("Scene3D WebGPU render watchdog recreates stalled animated renderer", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-watchdog";
  let now = 0;
  let createCount = 0;
  let disposeCount = 0;
  let renderCount = 0;
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
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function() {
              createCount += 1;
              return {
                kind: "webgpu",
                diagnostics: function() { return { ready: true }; },
                render: function() { renderCount += 1; },
                dispose: function() { disposeCount += 1; }
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
          id: "gosx-engine-webgpu-watchdog",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-watchdog",
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
  env.context.createCount = createCount;
  env.context.disposeCount = disposeCount;
  env.context.renderCount = renderCount;
  Object.defineProperty(env.context, "createCount", {
    configurable: true,
    get: () => createCount,
    set: (value) => { createCount = value; },
  });
  Object.defineProperty(env.context, "disposeCount", {
    configurable: true,
    get: () => disposeCount,
    set: (value) => { disposeCount = value; },
  });
  Object.defineProperty(env.context, "renderCount", {
    configurable: true,
    get: () => renderCount,
    set: (value) => { renderCount = value; },
  });
  const timers = installManualTimers(env.context);
  const raf = installManualRAF(env.context);

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  timers.runDelay(0);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  raf.flush(48);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(createCount, 1);
  assert.equal(renderCount, 1);
  assert.equal(raf.count(), 1);

  now = 8000;
  assert.equal(timers.runInterval(2000), 1);
  now = 16000;
  assert.equal(timers.runInterval(2000), 1);

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-watchdog"), "recovering");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-watchdog-reason"), "webgpu-render-not-started");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-watchdog-recoveries"), "1");
  assert.equal(createCount, 2);
  assert.equal(disposeCount, 1);
});

test("Scene3D WebGPU device loss falls back to WebGL on a replacement canvas", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-device-lost";
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
          window.__testWebGPUDisposeCount = 0;
          window.__testWebGPURenderCount = 0;
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function(canvas) {
              window.__testWebGPUCreateCount += 1;
              canvas.__webgpuClaimed = true;
              return {
                kind: "webgpu",
                diagnostics: function() {
                  return {
                    ready: window.__testWebGPUDeviceLost !== true,
                    deviceLost: window.__testWebGPUDeviceLost === true
                  };
                },
                render: function() { window.__testWebGPURenderCount += 1; },
                dispose: function() { window.__testWebGPUDisposeCount += 1; }
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
          id: "gosx-engine-webgpu-device-lost",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-device-lost",
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
  env.context.__testWebGPUDeviceLost = false;
  const originalCreateElement = env.document.createElement.bind(env.document);
  env.document.createElement = function(tagName) {
    const element = originalCreateElement(tagName);
    if (String(tagName || "").toLowerCase() === "canvas") {
      const originalGetContext = element.getContext.bind(element);
      element.getContext = function(kind, options) {
        const contextKind = String(kind || "");
        if (
          this.__webgpuClaimed &&
          (contextKind === "2d" || contextKind === "webgl" || contextKind === "webgl2" || contextKind === "experimental-webgl")
        ) {
          this.contextCalls = this.contextCalls || [];
          this.contextCalls.push({ kind, options: options || null, blockedByWebGPU: true });
          return null;
        }
        return originalGetContext(kind, options);
      };
    }
    return element;
  };

  const timers = installManualTimers(env.context);
  const raf = installManualRAF(env.context);

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  env.context.__gosx_emit = (level, cat, msg, fields) => {
    events.push({ level, cat, msg, fields: fields || {} });
  };
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  timers.runDelay(0);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  raf.flush(48);
  await flushAsyncWork();

  const firstCanvas = mount.children[0];
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(firstCanvas.getAttribute("data-gosx-scene3d-canvas"), "true");
  assert.equal(env.context.__testWebGPUCreateCount, 1);
  assert.equal(env.context.__testWebGPURenderCount, 1);

  env.context.__testWebGPUDeviceLost = true;
  now = 4000;
  assert.equal(timers.runInterval(2000), 1);
  await flushAsyncWork();

  const replacementCanvas = mount.children[0];
  assert.notEqual(replacementCanvas, firstCanvas);
  assert.equal(firstCanvas.parentNode, null);
  assert.equal(replacementCanvas.getAttribute("data-gosx-scene3d-canvas"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgpu-device-lost");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-watchdog"), "recovering");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-watchdog-reason"), "webgpu-device-lost");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-watchdog-recoveries"), "1");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-watchdog-fallbacks"), "1");
  assert.equal(env.context.__testWebGPUDisposeCount, 1);
  assert.equal((firstCanvas.contextCalls || []).some((call) => call.blockedByWebGPU), false);
  assert.ok((replacementCanvas.contextCalls || []).some((call) => call.kind === "webgl2" || call.kind === "webgl"));
  assert.ok(replacementCanvas.children.some((child) => child.getAttribute("data-gosx-scene-node-layer") === "true"));
  assert.equal(events.some((event) => event.msg === "render-watchdog-recovery" && event.fields.reason === "webgpu-device-lost"), true);
  assert.equal(events.some((event) => event.msg === "renderer-canvas-replaced"), true);
  assert.equal(events.some((event) => event.msg === "renderer-swap" && event.fields.to === "webgl"), true);
  assert.equal(events.some((event) => event.msg === "renderer-fallback-unavailable"), false);
  // The WebGL renderer now ships as a lazily fetched chunk, so the ladder had
  // to fetch it during the fallback. Prove the fetch happened and that the
  // ladder re-entered and completed the swap on the second pass.
  assert.equal(
    env.fetchCalls.filter((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgl.js").length,
    1,
    "device loss must fetch the WebGL chunk exactly once",
  );
  assert.equal(events.some((event) => event.msg === "webgl-fallback-chunk-fetch"), true);
  assert.equal(events.some((event) => event.msg === "webgl-fallback-chunk-failed"), false);
  assert.equal(events.some((event) => event.msg === "webgl-fallback-chunk-unusable"), false);
});

test("Scene3D WebGL page fetches the lazy WebGL chunk and draws with WebGL", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-lazy";
  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource } },
    manifest: scene3dWebGLSplitManifest("scene-webgl-lazy", {}),
  });
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await flushAsyncWork();

  const webglFetches = env.fetchCalls.filter((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgl.js");
  assert.equal(webglFetches.length, 1, "a WebGL page must fetch the WebGL chunk exactly once");
  assert.ok(env.context.__gosx_scene3d_webgl_api, "the chunk must publish __gosx_scene3d_webgl_api");
  assert.equal(typeof env.context.__gosx_scene3d_webgl_api.createScenePBRRendererOrFallback, "function");
  assert.equal(typeof env.context.__gosx_scene3d_webgl_api.createSceneWaterRendererWebGL, "function");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.ok((mount.children[0].contextCalls || []).some((call) => call.kind === "webgl2" || call.kind === "webgl"));
});

test("Scene3D forceWebGL fetches the WebGL chunk and never fetches the WebGPU chunk", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-forced";
  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    enableWebGPU: true,
    navigatorGPU: {
      requestAdapter: async () => ({
        requestDevice: async () => ({ lost: new Promise(() => {}), features: new Set(), limits: {} }),
      }),
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource } },
    manifest: scene3dWebGLSplitManifest("scene-webgl-forced", { forceWebGL: true }),
  });
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await flushAsyncWork();

  assert.equal(
    env.fetchCalls.filter((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgl.js").length,
    1,
    "forceWebGL must force the WebGL fetch even where navigator.gpu exists",
  );
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgpu.js"),
    false,
    "forceWebGL must skip WebGPU",
  );
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
});

test("Scene3D WebGPU page does not fetch the WebGL chunk at mount", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-unused";
  const env = createContext({
    elements: [mount],
    enableWebGPU: true,
    enableWebGL2: true,
    navigatorGPU: {
      requestAdapter: async () => ({
        requestDevice: async () => ({ lost: new Promise(() => {}), features: new Set(), limits: {} }),
      }),
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/gosx/bootstrap-feature-scene3d-webgpu.js": {
        text: `
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function(canvas) {
              canvas.getContext("webgpu");
              return { kind: "webgpu", render: function() {}, dispose: function() {} };
            }
          };
        `,
      },
    },
    manifest: scene3dWebGLSplitManifest("scene-webgl-unused", { preferWebGPU: true }),
  });
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgl.js"),
    false,
    "this is the whole saving: a WebGPU page must not download the WebGL renderer",
  );
});

test("Scene3D falls through to canvas2d when the lazy WebGL chunk publishes no API", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-broken-chunk";
  const events = [];
  const env = createContext({
    elements: [mount],
    enableWebGL2: false,
    // An empty chunk body loads without error and publishes nothing, which is
    // the failure ensureWebGLFeatureLoaded rejects on.
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/gosx/bootstrap-feature-scene3d-webgl.js": { text: "" },
    },
    manifest: scene3dWebGLSplitManifest("scene-webgl-broken-chunk", {}),
  });
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  env.context.__gosx_emit = (level, cat, msg, fields) => {
    events.push({ level, cat, msg, fields: fields || {} });
  };
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await flushAsyncWork();

  assert.equal(
    env.fetchCalls.some((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgl.js"),
    true,
    "the mount must still try the chunk",
  );
  assert.equal(env.context.__gosx_scene3d_webgl_api, undefined);
  assert.equal(
    mount.getAttribute("data-gosx-scene3d-renderer"),
    "canvas",
    "an unreachable WebGL chunk must degrade to canvas2d, not to a dead renderer",
  );
});

test("Scene3D base chunk keeps the backend-agnostic PBR helpers eager", () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  const api = env.context.__gosx_scene3d_api;
  assert.ok(api, "base chunk must publish __gosx_scene3d_api");
  // 15b-scene-planner.js, 10-runtime-scene-core.js and the WebGPU chunk read
  // these. A WebGPU-only page never loads 16-scene-webgl.js, so a helper that
  // slipped back into the lazy chunk would leave one of them undefined here and
  // break WebGPU rendering with no test coverage short of a real GPU.
  for (const name of [
    "scenePBRViewMatrix",
    "scenePBRProjectionMatrix",
    "scenePBRProjectionMatrixForCamera",
    "sceneShadowLightSpaceMatrix",
    "sceneShadowComputeBounds",
    "scenePBRObjectRenderPass",
    "scenePBRDepthSort",
    "generateInstancedGeometry",
    "normalizeInstancedGeometryKind",
    "hashLightContent",
    "hashEnvironmentContent",
  ]) {
    assert.equal(typeof api[name], "function", `__gosx_scene3d_api.${name} must stay eager`);
  }
  for (const name of ["SCENE_POST_TONE_MAPPING", "SCENE_POST_FXAA", "SCENE_POST_CUSTOM_POST"]) {
    assert.equal(typeof api[name], "string", `__gosx_scene3d_api.${name} must stay eager`);
  }
  // Nothing WebGL-specific may leak into the eager chunk.
  assert.equal(env.context.__gosx_scene3d_webgl_api, undefined);
});

test("Scene3D spot lights get a usable default cone when the author omits Angle", () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  const api = env.context.__gosx_scene3d_api;

  // Go's setNumeric drops zero values, so an unset SpotLight.Angle arrives as
  // undefined. cos(0) admits no direction, so a 0 angle used to render nothing
  // on WebGL and WebGPU alike.
  const implicit = api.normalizeSceneLight({ kind: "spot", x: 0, y: 4, z: 0 }, 0, null);
  assert.equal(implicit.kind, "spot");
  assert.ok(implicit.angle > 0, "an omitted spot Angle must not collapse the cone");
  assert.ok(Math.abs(implicit.angle - Math.PI / 6) < 1e-9, "the default cone is 30 degrees");

  // An explicit zero is indistinguishable from unset over the wire, so it takes
  // the same default rather than rendering an invisible light.
  const explicitZero = api.normalizeSceneLight({ kind: "spot", angle: 0 }, 1, null);
  assert.ok(Math.abs(explicitZero.angle - Math.PI / 6) < 1e-9);

  // An authored angle passes through untouched, still clamped to [0, PI].
  const authored = api.normalizeSceneLight({ kind: "spot", angle: Math.PI / 3 }, 2, null);
  assert.ok(Math.abs(authored.angle - Math.PI / 3) < 1e-9);
  const overWide = api.normalizeSceneLight({ kind: "spot", angle: 12 }, 3, null);
  assert.ok(Math.abs(overWide.angle - Math.PI) < 1e-9);

  // Only spot lights read angle. Every other kind keeps 0.
  for (const kind of ["point", "directional", "ambient", "hemisphere", "rect-area"]) {
    assert.equal(api.normalizeSceneLight({ kind }, 4, null).angle, 0, `${kind} must keep angle 0`);
  }
});

test("16c-scene-shared-pbr.js stays free of WebGL context calls", () => {
  const shared = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16c-scene-shared-pbr.js"), "utf8");
  // Strip line comments so prose about WebGL cannot trip the scan.
  const code = shared.split("\n").filter((line) => !/^\s*\/\//.test(line)).join("\n");
  assert.doesNotMatch(code, /\bgl\s*\./, "16c must stay backend-agnostic; a gl. call means the WebGL split leaked back");
  assert.doesNotMatch(code, /WebGL2RenderingContext|createProgram|getContext/,
    "16c must not touch a rendering context");
});

// --- v0.33.2: persistent per-frame WebGPU validation/OOM errors (frames
// keep advancing, but every one is invalid — a stalled-frame-seq watchdog
// cannot see this) must demote (tear down post-FX, retry raw) first, and
// only escalate to a WebGL backend swap if raw rendering ALSO keeps
// erroring after demotion. ---
test("Scene3D WebGPU persistent frame errors demote post-FX first, then fall back to WebGL if raw rendering still errors", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-frame-error";
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
          window.__testWebGPUDisposeCount = 0;
          window.__testWebGPURenderCount = 0;
          window.__testWebGPUDemoteCount = 0;
          window.__testWebGPUFrameErrorStreak = 0;
          window.__testWebGPUPostFXDisabled = false;
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function(canvas) {
              window.__testWebGPUCreateCount += 1;
              canvas.__webgpuClaimed = true;
              return {
                kind: "webgpu",
                diagnostics: function() {
                  return {
                    ready: true,
                    frameErrorStreak: window.__testWebGPUFrameErrorStreak,
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
                render: function() { window.__testWebGPURenderCount += 1; },
                dispose: function() { window.__testWebGPUDisposeCount += 1; }
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
          id: "gosx-engine-webgpu-frame-error",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-frame-error",
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
  const originalCreateElement = env.document.createElement.bind(env.document);
  env.document.createElement = function(tagName) {
    const element = originalCreateElement(tagName);
    if (String(tagName || "").toLowerCase() === "canvas") {
      const originalGetContext = element.getContext.bind(element);
      element.getContext = function(kind, options) {
        const contextKind = String(kind || "");
        if (
          this.__webgpuClaimed &&
          (contextKind === "2d" || contextKind === "webgl" || contextKind === "webgl2" || contextKind === "experimental-webgl")
        ) {
          this.contextCalls = this.contextCalls || [];
          this.contextCalls.push({ kind, options: options || null, blockedByWebGPU: true });
          return null;
        }
        return originalGetContext(kind, options);
      };
    }
    return element;
  };

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

  const firstCanvas = mount.children[0];
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(env.context.__testWebGPUCreateCount, 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), null);

  // --- Step 1: persistent frame errors (well past the streak threshold) ->
  // DEMOTE. Renderer stays WebGPU; only post-FX gets torn down.
  env.context.__testWebGPUFrameErrorStreak = 40;
  now = 2000;
  assert.equal(timers.runInterval(2000), 1);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu", "demote must NOT swap the renderer");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), "true");
  assert.equal(env.context.__testWebGPUDemoteCount, 1);
  assert.equal(env.context.__testWebGPUCreateCount, 1, "demote must not recreate the renderer");
  assert.equal(mount.children[0], firstCanvas, "demote must not replace the canvas");
  assert.equal(events.some((event) => event.msg === "webgpu-postfx-demoted"), true);
  assert.equal(events.some((event) => event.msg === "webgpu-persistent-frame-error-fallback"), false);
  assert.equal(events.some((event) => event.msg === "renderer-swap"), false);

  // --- Step 2: raw rendering (post-FX already torn down) STILL errors
  // persistently -> FALLBACK to WebGL on a replacement canvas.
  now = 4000;
  assert.equal(timers.runInterval(2000), 1);
  await flushAsyncWork();

  const replacementCanvas = mount.children[0];
  assert.notEqual(replacementCanvas, firstCanvas, "fallback must replace the (WebGPU-tainted) canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgpu-persistent-frame-error");
  assert.equal(env.context.__testWebGPUDisposeCount, 1);
  assert.ok((replacementCanvas.contextCalls || []).some((call) => call.kind === "webgl2" || call.kind === "webgl"));
  assert.equal(events.some((event) => event.msg === "webgpu-persistent-frame-error-fallback"), true);
  assert.equal(events.some((event) => event.msg === "renderer-swap" && event.fields.to === "webgl"), true);
});

test("selective Scene3D bootstrap honors explicit WebGPU preference when WebGL is disabled", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-explicit";
  const env = createContext({
    elements: [mount],
    enableWebGPU: true,
    createWebGLContext: () => new FakeWebGLContext({
      vendor: "Mesa",
      renderer: "llvmpipe (LLVM 18.1.0, 256 bits)",
    }),
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
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function() {
              return {
                kind: "webgpu",
                diagnostics: function() {
                  return {};
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
          id: "gosx-engine-webgpu-explicit",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-explicit",
          jsExport: "GoSXScene3D",
          props: {
            width: 320,
            height: 180,
            preferWebGPU: true,
            preferWebGL: false,
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

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "disabled");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-preference"), "prefer");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgpu.js"),
    true,
  );
  assert.equal((mount.children[0].contextCalls || []).some((call) => call.kind === "webgl" || call.kind === "webgl2"), false);
});

test("selective Scene3D bootstrap uses WebGL when WebGPU cannot cover scene features", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-feature-gap";
  const env = createContext({
    elements: [mount],
    enableWebGPU: true,
    enableWebGL2: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
      "/gosx/bootstrap-feature-scene3d-webgpu.js": {
        text: `
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function(canvas) {
              canvas.getContext("webgpu");
              return {
                kind: "webgpu",
                render: function() {},
                dispose: function() {}
              };
            }
          };
          window.__gosx_scene3d_webgpu_loaded = true;
        `,
      },
    },
    manifest: {
      runtime: { path: "/gosx/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-webgpu-feature-gap",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-feature-gap",
          jsExport: "GoSXScene3D",
          props: {
            width: 360,
            height: 220,
            autoRotate: false,
            scene: {
              objects: [
                {
                  id: "wide-grid",
                  kind: "lines",
                  lineDash: true,
                  points: [
                    [0, 0, 0],
                    [1, 0, 0],
                  ],
                  segments: [[0, 1]],
                },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgpu-feature-gap");
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgpu.js"),
    false,
  );
  assert.equal((mount.children[0].contextCalls || []).some((call) => call.kind === "webgpu"), false);
  assert.equal((mount.children[0].contextCalls || []).some((call) => call.kind === "webgl" || call.kind === "webgl2"), true);
});

test("Scene3D WebGPU climbs back onto WebGPU after a device-lost fallback once the probe recovers, and actually renders through the new device", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-device-lost-recovery";
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
          window.__testWebGPUDeviceLost = false;
          // One entry per createRenderer() call, in creation order -- lets
          // this test prove render() calls land on the SPECIFIC new
          // instance created during recovery, not just that "some webgpu
          // renderer" rendered (which the OLD, still-broken instance could
          // also satisfy if the mount had merely flipped a flag instead of
          // actually swapping renderers).
          window.__testWebGPUInstances = [];
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function(canvas) {
              window.__testWebGPUCreateCount += 1;
              var idx = window.__testWebGPUInstances.length;
              window.__testWebGPUInstances.push({ renderCount: 0, disposed: false });
              canvas.__webgpuClaimed = true;
              return {
                kind: "webgpu",
                diagnostics: function() {
                  // Only the FIRST instance ever reports device loss --
                  // the recovered (second) instance is healthy, matching a
                  // real fresh device acquired after reprobe.
                  var lost = idx === 0 && window.__testWebGPUDeviceLost === true;
                  return {
                    ready: !lost,
                    deviceLost: lost,
                    deviceLostInfo: lost ? { reason: "destroyed", message: "Device was destroyed." } : null,
                    adapterInfo: { vendor: "test-vendor", architecture: "test-arch" },
                  };
                },
                render: function() { window.__testWebGPUInstances[idx].renderCount += 1; },
                dispose: function() { window.__testWebGPUInstances[idx].disposed = true; }
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
          id: "gosx-engine-webgpu-device-lost-recovery",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-device-lost-recovery",
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
  const originalCreateElement = env.document.createElement.bind(env.document);
  env.document.createElement = function(tagName) {
    const element = originalCreateElement(tagName);
    if (String(tagName || "").toLowerCase() === "canvas") {
      const originalGetContext = element.getContext.bind(element);
      element.getContext = function(kind, options) {
        const contextKind = String(kind || "");
        if (
          this.__webgpuClaimed &&
          (contextKind === "2d" || contextKind === "webgl" || contextKind === "webgl2" || contextKind === "experimental-webgl")
        ) {
          this.contextCalls = this.contextCalls || [];
          this.contextCalls.push({ kind, options: options || null, blockedByWebGPU: true });
          return null;
        }
        return originalGetContext(kind, options);
      };
    }
    return element;
  };

  const timers = installManualTimers(env.context);
  const raf = installManualRAF(env.context);

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  env.context.__gosx_emit = (level, cat, msg, fields) => {
    events.push({ level, cat, msg, fields: fields || {} });
  };
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  timers.runDelay(0);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  raf.flush(48);
  await flushAsyncWork();

  const firstCanvas = mount.children[0];
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu");
  assert.equal(env.context.__testWebGPUCreateCount, 1);
  assert.equal(env.context.__testWebGPUInstances[0].renderCount, 1);

  // --- Device lost -> the watchdog's forceFallback path swaps to WebGL,
  // exactly like the sibling non-recovery test above. ---
  env.context.__testWebGPUDeviceLost = true;
  now = 4000;
  assert.equal(timers.runInterval(2000), 1);
  await flushAsyncWork();

  const fallbackCanvas = mount.children[0];
  assert.notEqual(fallbackCanvas, firstCanvas);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgpu-device-lost");
  assert.equal(env.context.__testWebGPUInstances[0].disposed, true, "the OLD (broken) webgpu instance must be disposed");
  // Diagnostic surfacing: the loss reason must reach the DOM and the
  // render-watchdog-recovery telemetry event, not just live inside the
  // renderer's own diagnostics().
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-device-lost-reason"), "destroyed");
  const recoveryEvent = events.find((event) => event.msg === "render-watchdog-recovery" && event.fields.reason === "webgpu-device-lost");
  assert.ok(recoveryEvent, "render-watchdog-recovery must fire for the device-lost fallback");
  assert.equal(recoveryEvent.fields.deviceLostReason, "destroyed");
  assert.equal(recoveryEvent.fields.deviceLostMessage, "Device was destroyed.");
  assert.equal(recoveryEvent.fields.adapterInfo && recoveryEvent.fields.adapterInfo.vendor, "test-vendor");

  // --- The probe recovers: this is the exact production trigger
  // (16z-scene-webgpu-probe.js's sceneWebGPUDispatchProbeReady) for a
  // device re-acquired after a loss. Before this fix, handleSceneWebGPUProbeReady
  // silently ignored this because renderer.kind was "webgl". ---
  events.length = 0;
  env.context.dispatchEvent({ type: "gosx:scene3d:webgpu-probe-ready" });
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu", "the mount must climb back onto WebGPU once the probe recovers");
  assert.equal(env.context.__testWebGPUCreateCount, 2, "recovery must construct a genuinely NEW webgpu renderer instance");
  const recoveredCanvas = mount.children[0];
  assert.notEqual(recoveredCanvas, fallbackCanvas, "recovery must mount a fresh, untainted canvas -- the WebGL fallback canvas cannot host a WebGPU context");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgpu-postfx-demoted"), null);

  // Proof this is a REAL adoption, not a flag flip: the recovery swap
  // itself already rendered once immediately (renderLatestSceneBundle),
  // and the OLD, disposed instance must never render again.
  const recoveredRenderCountAfterSwap = env.context.__testWebGPUInstances[1].renderCount;
  assert.ok(recoveredRenderCountAfterSwap >= 1, "the RECOVERED renderer instance must actually receive render() calls, not just become the DOM-attribute value");
  assert.equal(env.context.__testWebGPUInstances[0].renderCount, 1, "the OLD, disposed instance must never render again (still just its one pre-loss frame)");

  // Drive a further real animation frame and confirm the continuous render
  // loop keeps driving THIS SAME recovered instance (proves adoption, not a
  // one-shot compensating render).
  raf.flush(16);
  await flushAsyncWork();
  assert.ok(env.context.__testWebGPUInstances[1].renderCount > recoveredRenderCountAfterSwap, "the render loop must keep driving the recovered instance on subsequent frames");
  assert.equal(env.context.__testWebGPUInstances[0].renderCount, 1, "the OLD, disposed instance must still never render again");

  assert.equal(events.some((event) => event.msg === "render-watchdog-recovery" && event.fields.reason === "webgpu-probe-recovered"), true);
});

test("Scene3D WebGPU device loss counts once (not twice) against the probe's reprobe backoff, and the loss reason survives a later successful reprobe", async () => {
  const fake1 = makeFakeGPUDevice();
  let resolveLost1 = null;
  fake1.device.lost = new Promise((resolve) => { resolveLost1 = resolve; });

  const fake2 = makeFakeGPUDevice();
  // fake2.device.lost intentionally left as makeFakeGPUDevice's default
  // (a promise that never resolves) -- this test only loses the FIRST device.

  let deviceRequests = 0;
  const adapter = {
    info: { vendor: "test-vendor" },
    requestDevice: async () => {
      deviceRequests += 1;
      return deviceRequests === 1 ? fake1.device : fake2.device;
    },
  };
  let adapterRequests = 0;
  const env = createContext({
    enableWebGPU: true,
    navigatorGPU: {
      requestAdapter: async () => {
        adapterRequests += 1;
        return adapter;
      },
      getPreferredCanvasFormat: () => "rgba8unorm",
    },
  });
  env.context.GPUBufferUsage = {
    MAP_READ: 0x1, MAP_WRITE: 0x2, COPY_SRC: 0x4, COPY_DST: 0x8,
    INDEX: 0x10, VERTEX: 0x20, UNIFORM: 0x40, STORAGE: 0x80,
    INDIRECT: 0x100, QUERY_RESOLVE: 0x200,
  };
  env.context.GPUTextureUsage = {
    COPY_SRC: 0x1, COPY_DST: 0x2, TEXTURE_BINDING: 0x4,
    STORAGE_BINDING: 0x8, RENDER_ATTACHMENT: 0x10,
  };
  env.context.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
  env.context.createImageBitmap = function(image) {
    return Promise.resolve({ __kind: "imageBitmap", width: image && image.width || 1, height: image && image.height || 1, close() {} });
  };

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();
  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true);
  assert.equal(adapterRequests, 1);
  assert.equal(deviceRequests, 1);

  runScript(freshFeatureBundleSource("scene3d-webgpu"), env.context, "bootstrap-feature-scene3d-webgpu.js");
  const api = env.context.__gosx_scene3d_webgpu_api;
  assert.ok(api && typeof api.createRenderer === "function");

  const mount = new FakeElement("div", null);
  const gpuCtx = {
    configure() {},
    getCurrentTexture() {
      return { createView() { return { __kind: "canvasTextureView" }; } };
    },
  };
  const canvas = {
    width: 64, height: 64, isConnected: true, childNodes: [], parentNode: mount,
    getBoundingClientRect() { return { width: 64, height: 64 }; },
    getContext(kind) { return kind === "webgpu" ? gpuCtx : null; },
  };
  const renderer = api.createRenderer(canvas, {});
  assert.ok(renderer, "createRenderer must succeed against fake1 (probe device)");

  const probeBefore = env.context.__gosx_scene3d_webgpu_probe();
  assert.equal(probeBefore.lostProbeCount, 0);

  // ONE device.lost event. Both 16z's watcher and 16a's renderer-local
  // handler are listening on this exact promise.
  resolveLost1({ reason: "destroyed", message: "Device was destroyed." });
  await flushAsyncWork();
  await flushAsyncWork();

  const probeAfterLoss = env.context.__gosx_scene3d_webgpu_probe();
  assert.equal(probeAfterLoss.lostProbeCount, 1, "one real device loss must count once against the reprobe backoff window, not twice");

  const rendererDiagAfterLoss = renderer.diagnostics();
  assert.equal(rendererDiagAfterLoss.deviceLost, true);
  assert.ok(rendererDiagAfterLoss.deviceLostInfo, "diagnostics().deviceLostInfo must be populated from the real device.lost resolution");
  assert.equal(rendererDiagAfterLoss.deviceLostInfo.reason, "destroyed");
  assert.equal(rendererDiagAfterLoss.deviceLostInfo.message, "Device was destroyed.");

  // The immediate reprobe (sceneWebGPUInvalidateProbe -> sceneWebGPUStartProbe)
  // succeeds against fake2 and clears the SHARED probe snapshot.
  assert.equal(await env.context.__gosx_scene3d_webgpu_probe_ready(), true, "the probe must reacquire a fresh device after the loss");
  const probeRecovered = env.context.__gosx_scene3d_webgpu_probe();
  assert.equal(probeRecovered.lost, null, "the SHARED probe snapshot clears on a successful reprobe");
  assert.equal(deviceRequests, 2);

  // The renderer's OWN lastDeviceLostInfo must survive that shared-snapshot
  // clear -- this renderer instance genuinely did lose its device, and that
  // fact does not become false just because a DIFFERENT device recovered.
  const rendererDiagAfterRecovery = renderer.diagnostics();
  assert.ok(rendererDiagAfterRecovery.deviceLostInfo, "the renderer's own loss detail must survive the shared probe snapshot clearing");
  assert.equal(rendererDiagAfterRecovery.deviceLostInfo.reason, "destroyed");
  assert.equal(probeAfterLoss.lostProbeCount, 1, "still one -- the reprobe/recovery cycle itself must not add another count");
});
