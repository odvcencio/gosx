"use strict";
// Selective feature-bundle loading, browser capability gates, the engine
// factory contract, backend honesty gating and the video sync parity vector.
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
  bootstrapFeatureIslandsSource,
  bootstrapFeatureEnginesSource,
  bootstrapFeatureHubsSource,
  FakeTextNode,
  FakeElement,
  createContext,
  installManualRAF,
  makeFakeContext2D,
  runScript,
  flushAsyncWork,
  loadVideoSyncJSEngineFactory,
  readSceneMountSrc,
  bootstrapChunkSources,
  readBootstrapSrc,
  freshFeatureBundleSource,
} = require("./runtime-test-harness.js");

test("bootstrap decompresses compressedPositions for Scene3D points", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-decompress-root";

  // Create a compressedPositions payload matching Go's scalar quantization format.
  // 6 floats: [1.0, 2.0, 3.0, 4.0, 5.0, 6.0]
  // min=1.0, max=6.0, 2-bit quantization (4 levels: 0,1,2,3)
  // Indices: 0, floor((2-1)/(6-1)*3+0.5)=1, 1, 2, 2, 3
  // step = (6-1)/3 = 1.6667
  // Packed 2-bit: indices [0,1,1,2,2,3] → byte layout
  // byte 0: idx0(00) | idx1(01) | idx2(01) | idx3(10) = 0b10010100 = 0x94
  // byte 1: idx4(10) | idx5(11) | pad      | pad      = 0b00001110 = 0x0E
  const packed = Buffer.from([0x94, 0x0E]).toString("base64");

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-decompress",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-decompress-root",
          props: {
            width: 200,
            height: 200,
            background: "#000",
            camera: { x: 0, y: 0, z: 5, fov: 72 },
            scene: {
              points: [
                {
                  id: "compressed-cloud",
                  count: 2,
                  color: "#fff",
                  size: 2,
                  compressedPositions: [
                    {
                      packed: packed,
                      norm: 1.0,    // min value
                      maxVal: 6.0,  // max value
                      dim: 6,
                      bitWidth: 2,
                      count: 6,
                    },
                  ],
                },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  // The decompressor should have replaced compressedPositions with positions.
  // Verify the engine mounted and the scene rendered without errors.
  assert.equal(env.consoleLogs.error.length, 0, "expected no errors, got: " + JSON.stringify(env.consoleLogs.error));
  const engineState = env.context.__gosx.engines.get("gosx-engine-decompress");
  assert.ok(engineState, "expected engine to mount");
});

test("selective runtime loads islands feature and shared wasm only when islands are declared", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  wrapper.id = "gosx-island-runtime";
  componentRoot.appendChild(new FakeTextNode("0", null));
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
      "/gosx/bootstrap-feature-islands.js": { text: freshFeatureBundleSource("islands") },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-runtime",
          component: "Counter",
          props: { initial: 1 },
          programRef: "/counter.json",
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-islands.js"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-engines.js"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-hubs.js"), false);
  assert.equal(env.context.__gosx.islands.size, 1);
});

test("selective runtime ignores stale partial exports before hydrating islands", async () => {
  const wrapper = new FakeElement("div", null);
  wrapper.id = "gosx-island-stale";

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
      "/gosx/bootstrap-feature-islands.js": { text: bootstrapFeatureIslandsSource },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-stale",
          component: "Counter",
          props: { initial: 1 },
          programRef: "/counter.json",
        },
      ],
    },
  });
  env.context.__gosx_action = () => 0;

  const freshBootstrapRuntimeSource = readBootstrapSrc(
    ...bootstrapChunkSources("bootstrap-runtime.js").map((name) => name.replace(/^bootstrap-src\//, "")),
  );
  runScript(freshBootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), true);
  assert.equal(env.hydrateCalls.length, 1);
  assert.equal(env.context.__gosx.islands.size, 1);
  assert.deepEqual(env.consoleLogs.error, []);
});

test("selective island feature waits for hydrate export during cold runtime ready", async () => {
  const wrapper = new FakeElement("div", null);
  wrapper.id = "gosx-island-delayed-export";

  const hydrateCalls = [];
  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
      "/gosx/bootstrap-feature-islands.js": { text: freshFeatureBundleSource("islands") },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-delayed-export",
          component: "Counter",
          props: { initial: 1 },
          programRef: "/counter.json",
        },
      ],
    },
  });
  env.context.Go = function Go() {
    this.importObject = {};
    this.run = () => {
      const ready = env.context.__gosx_runtime_ready;
      if (typeof ready === "function") ready();
      setTimeout(() => {
        env.context.__gosx_hydrate = (...args) => {
          hydrateCalls.push(args);
          return null;
        };
      }, 12);
    };
  };

  const freshBootstrapRuntimeSource = readBootstrapSrc(
    ...bootstrapChunkSources("bootstrap-runtime.js").map((name) => name.replace(/^bootstrap-src\//, "")),
  );
  runScript(freshBootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await new Promise((resolve) => setTimeout(resolve, 30));
  await flushAsyncWork();

  assert.equal(hydrateCalls.length, 1);
  assert.equal(env.context.__gosx.islands.size, 1);
  assert.deepEqual(env.consoleLogs.error, []);
});

test("selective runtime loads islands feature for compute islands", async () => {
  const env = createContext({
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/fight-controller.json": { text: '{"name":"FightController"}' },
      "/gosx/bootstrap-feature-islands.js": { text: bootstrapFeatureIslandsSource },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      computeIslands: [
        {
          id: "gosx-compute-runtime",
          component: "FightController",
          props: { match: "abc" },
          programRef: "/fight-controller.json",
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.computeHydrateCalls.length, 1);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-islands.js"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-engines.js"), false);
  assert.equal(env.context.__gosx.computeIslands.size, 1);
});

test("selective runtime mounts native JS engines without loading the shared wasm runtime", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "engine-root";

  const env = createContext({
    elements: [mount],
    engineFactories: {
      Painter(context) {
        context.mount.setAttribute("data-mounted", "true");
        return {
          dispose() {
            context.mount.setAttribute("data-disposed", "true");
          },
        };
      },
    },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-runtime",
          component: "Painter",
          kind: "surface",
          mountId: "engine-root",
          jsExport: "Painter",
          props: { color: "#8de1ff" },
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-engines.js"), true);
  const enginesScript = env.document.head.children.find((child) =>
    child.tagName === "SCRIPT" && child.src === "/gosx/bootstrap-feature-engines.js"
  );
  assert.equal(enginesScript?.getAttribute("data-gosx-script"), "feature-engines");
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(mount.getAttribute("data-mounted"), "true");

  await env.context.__gosx_dispose_page();
  assert.equal(mount.getAttribute("data-disposed"), "true");
});

test("selective runtime ignores self-describing surfaces in old manifests", async () => {
  const surface = new FakeElement("canvas", null);
  surface.setAttribute("data-gosx-surface-kind", "canvas2d");

  const env = createContext({
    elements: [surface],
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
    },
    manifest: {},
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-engines.js"), false);
  assert.equal(env.hydrateCalls.length, 0);
});

test("selective runtime loads engines feature and shared wasm for declared self-describing surfaces", async () => {
  const surface = new FakeElement("canvas", null);
  surface.setAttribute("data-gosx-surface-kind", "canvas2d");
  surface.setAttribute("data-gosx-engine-component", "CanvasBoard");
  const ctx2d = makeFakeContext2D();
  surface.getContext = (kind) => kind === "2d" ? ctx2d : null;

  const env = createContext({
    elements: [surface],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      selfDescribingSurfaces: [
        { kind: "canvas2d", feature: "engines", runtime: "shared" },
        { kind: "canvas2d", feature: "engines", runtime: "shared" },
      ],
    },
  });

  env.context.__gosx_render_canvas = () => "";
  env.context.__gosx_tick_canvas = () => null;
  env.context.__gosx_canvas_set_backend = () => null;
  env.context.__gosx_canvas_event = () => null;
  env.context.__gosx_dispose_canvas = () => null;
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.filter((entry) => entry.url === "/runtime.wasm").length, 1);
  assert.equal(env.fetchCalls.filter((entry) => entry.url === "/gosx/bootstrap-feature-engines.js").length, 1);
  assert.equal(env.hydrateCalls.length, 1);
  assert.equal(env.hydrateCalls[0][0], "canvas2d");
  assert.equal(surface.hasAttribute("data-gosx-surface-id"), true);

  await env.context.__gosx_bootstrap_page({
    runtime: { path: "/runtime.wasm" },
    selfDescribingSurfaces: [{ kind: "canvas2d", feature: "engines", runtime: "shared" }],
  });
  await flushAsyncWork();
  assert.equal(env.hydrateCalls.length, 1);
});

// Regression test for the split feature-bundle build: bootstrap-feature-engines.js
// runs in its own IIFE (see 26b-feature-engines-prefix.ts), separate from the
// runtime bundle's closure. normalizeEngineRenderBundle (concatenated in from
// 30-tail.js's "engine mounting" section) normalizes the camera/label/html/
// surface fields of ANY runtime:"shared" engine's render bundle — not just
// GoSXScene3D's — via sceneRenderCamera, sceneLabelClassName,
// normalizeTextLayoutOverflow, normalizeSceneLabelCollision,
// normalizeSceneLabelWhiteSpace, normalizeSceneLabelAlign,
// normalizeSceneHTMLMode, normalizeSceneHTMLPointerEvents, and clamp01 — all
// of which live in 00-textlayout.ts / 10-runtime-scene-core.ts /
// 11-scene-math.ts, none of which bootstrap-feature-engines.js carries.
//
// Before the fix, a page whose ONLY shared-runtime engine is a non-Scene3D
// surface (so bootstrap-feature-scene3d.js never loads — manifestFeatureNames
// only requests "scene3d" for a GoSXScene3D component) threw
// "ReferenceError: sceneRenderCamera is not defined" (or one of the sibling
// normalizers) the first time that engine's render bundle carried a camera,
// label, or html entry. decodeEngineRenderBundle's try/catch silently
// swallowed it and returned null, dropping the entire render bundle every
// frame with no visible error to the app.
test("shared-runtime engine render bundle normalizes camera/labels/html/surfaces under the split runtime+engines-only bundles (no scene3d)", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "shared-surface-root";

  const renderedBundles = [];

  const env = createContext({
    elements: [mount],
    engineFactories: {
      // A custom (non-Scene3D) runtime:"shared" engine factory, mirroring how
      // a third-party //gosx:engine that opts into the shared WASM runtime
      // would drive its own render loop via ctx.runtime.renderFrame().
      TestSharedSurface(context) {
        context.mount.setAttribute("data-mounted", "true");
        const bundle = context.runtime.renderFrame(0, 320, 180);
        renderedBundles.push(bundle);
        return { dispose() {} };
      },
    },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-shared-surface",
          component: "TestSharedSurface",
          kind: "surface",
          mountId: "shared-surface-root",
          runtime: "shared",
          jsExport: "TestSharedSurface",
          props: { width: 320, height: 180 },
        },
      ],
    },
    onRenderEngine: () => JSON.stringify({
      camera: { kind: "orthographic", x: 1, y: 2, z: 3, zoom: 2 },
      labels: [
        {
          id: "lbl-1",
          text: "Hi",
          position: { x: 1, y: 2 },
          overflow: "ellipsis",
          collision: "allow",
          whiteSpace: "pre",
          textAlign: "left",
        },
      ],
      html: [
        {
          id: "html-1",
          target: "t1",
          mode: "texture",
          html: "<b>hi</b>",
          pointerEvents: "auto",
          opacity: 1.4, // out-of-range on purpose — exercises clamp01
        },
      ],
      positions: [0, 1, 2],
      colors: [1, 1, 1, 1],
      surfaces: [{ id: "surf-1", sourceKind: "video", textureKey: "tex-1" }],
    }),
  });

  const uncaughtErrors = [];
  env.context.addEventListener("error", (event) => {
    uncaughtErrors.push(event && event.error ? event.error : event);
  });

  // The split-bundle combo that reproduces the bug: the runtime chunk plus
  // the "engines" feature chunk ONLY. bootstrap-feature-scene3d.js never
  // loads (no GoSXScene3D entry in the manifest), so anything that used to
  // rely on Scene3D's chunk having already populated window.__gosx_runtime_api
  // must resolve on its own.
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.deepEqual(uncaughtErrors, [], "mounting the shared-runtime surface must not throw");
  assert.equal(env.consoleLogs.error.length, 0, "expected no console.error, got: " + JSON.stringify(env.consoleLogs.error));
  assert.equal(mount.getAttribute("data-mounted"), "true");
  assert.equal(renderedBundles.length, 1);

  const bundle = renderedBundles[0];
  assert.ok(bundle, "renderFrame must return a normalized bundle, not null");
  assert.equal(bundle.camera.kind, "orthographic");
  assert.equal(bundle.camera.zoom, 2);
  assert.equal(bundle.labels.length, 1);
  assert.equal(bundle.labels[0].overflow, "ellipsis");
  assert.equal(bundle.labels[0].collision, "allow");
  assert.equal(bundle.labels[0].whiteSpace, "pre");
  assert.equal(bundle.labels[0].textAlign, "left");
  assert.equal(bundle.html.length, 1);
  assert.equal(bundle.html[0].mode, "texture");
  assert.equal(bundle.html[0].pointerEvents, "auto");
  assert.equal(bundle.html[0].opacity, 1, "clamp01 must clamp opacity to 1");
  assert.equal(bundle.surfaces.length, 1);
  assert.equal(bundle.surfaces[0].sourceKind, "video");

  await env.context.__gosx_dispose_page();
});

test("selective runtime mounts builtin video sync without the hub feature chunk", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-runtime-root";
  let socket = null;

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
    },
    createWebSocket(url) {
      socket = {
        url,
        readyState: 1,
        sent: [],
        closeCalls: 0,
        send(payload) {
          this.sent.push(payload);
        },
        close() {
          this.closeCalls += 1;
          this.readyState = 3;
        },
      };
      return socket;
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-video",
          component: "SyncedVideo",
          kind: "video",
          mountId: "video-runtime-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            sync: "/api/theatre/ROOM01/ws",
            syncMode: "follow",
          },
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-engines.js"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-hubs.js"), false);
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(mount.firstChild && mount.firstChild.tagName, "VIDEO");
  assert.ok(socket, "expected video sync websocket to connect");
  assert.equal(socket.url, "ws://localhost:3000/api/theatre/ROOM01/ws");
  assert.equal(
    env.consoleLogs.error.some((entry) => entry.includes("failed to mount engine gosx-engine-video")),
    false,
  );
});

test("selective runtime video engines load HLS.js through the feature API", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-hls-runtime-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/gosx/hls.min.js": {
        text: `window.__hlsLoads = [];
window.Hls = function FakeHls() {
  this.attachMedia = function(video) { this.video = video; };
  this.loadSource = function(src) { window.__hlsLoads.push(src); };
  this.on = function() {};
  this.destroy = function() {};
};
window.Hls.isSupported = function() { return true; };
window.Hls.Events = {};`,
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-video-hls",
          component: "SyncedVideo",
          kind: "video",
          mountId: "video-hls-runtime-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.m3u8",
          },
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-engines.js"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/hls.min.js"), true);
  assert.deepEqual(Array.from(env.context.__hlsLoads || []), ["/media/promo.m3u8"]);
  assert.equal(env.context.__gosx.engines.size, 1);
  const mounted = env.context.__gosx.engines.get("gosx-engine-video-hls");
  assert.ok(mounted);
  assert.equal(
    mounted.handle.video.children.some((child) => child.tagName === "SOURCE" && String(child.getAttribute("src") || "").endsWith(".m3u8")),
    false,
  );
  assert.equal(
    env.consoleLogs.error.some((entry) => entry.includes("failed to mount engine gosx-engine-video-hls")),
    false,
  );
});

test("bootstrap blocks engines when required browser capabilities are missing", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "strict-engine-root";
  let factoryCalls = 0;

  const env = createContext({
    elements: [mount],
    engineFactories: {
      StrictRenderer() {
        factoryCalls += 1;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-strict",
          component: "StrictRenderer",
          kind: "surface",
          mountId: "strict-engine-root",
          props: {},
          capabilities: ["canvas", "webgl"],
          requiredCapabilities: ["webgl"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(factoryCalls, 0);
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(mount.getAttribute("data-gosx-engine-capability-state"), "unsupported");
  assert.equal(mount.getAttribute("data-gosx-engine-required-capabilities"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-engine-missing-capabilities"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-runtime-issue"), "capability");
  assert.equal(mount.getAttribute("data-gosx-fallback-active"), "unsupported");
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0].getAttribute("data-gosx-engine-unsupported"), "true");
  assert.ok(mount.children[0].textContent.includes("current browser"));

  const issues = env.context.__gosx.listIssues();
  assert.equal(issues.some((issue) => issue.scope === "engine" && issue.type === "capability" && issue.source === "gosx-engine-strict"), true);
});

test("bootstrap exposes required capability status to mounted engines", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "strict-ready-root";
  const captured = {};

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    engineFactories: {
      StrictReady(ctx) {
        captured.requiredCapabilities = ctx.requiredCapabilities.slice();
        captured.capabilityStatus = ctx.capabilityStatus;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-strict-ready",
          component: "StrictReady",
          kind: "surface",
          mountId: "strict-ready-root",
          props: {},
          capabilities: ["canvas", "webgl"],
          requiredCapabilities: ["webgl"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(mount.getAttribute("data-gosx-engine-capability-state"), "ready");
  assert.equal(mount.getAttribute("data-gosx-engine-supported-capabilities"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-engine-missing-capabilities"), null);
  assert.deepEqual(Array.from(captured.requiredCapabilities), ["webgl"]);
  assert.deepEqual(Array.from(captured.capabilityStatus.required), ["webgl"]);
  assert.deepEqual(Array.from(captured.capabilityStatus.missing), []);
  assert.deepEqual(Array.from(env.context.__gosx.engines.get("gosx-engine-strict-ready").requiredCapabilities), ["webgl"]);
});

test("bootstrap gates engines on negotiated WebGPU feature capabilities", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "webgpu-feature-root";
  const captured = {};
  const env = createContext({
    elements: [mount],
    enableWebGPU: true,
    webgpuAdapter: {
      features: new Set(["timestamp-query", "shader-f16"]),
      limits: {
        maxTextureDimension2D: 8192,
      },
      requestDevice: async (descriptor = {}) => ({
        lost: new Promise(() => {}),
        features: new Set(descriptor.requiredFeatures || []),
        limits: {
          maxTextureDimension2D: 4096,
        },
      }),
    },
    engineFactories: {
      WebGPUFeatureEngine(ctx) {
        captured.status = ctx.capabilityStatus;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgpu-feature",
          component: "WebGPUFeatureEngine",
          kind: "surface",
          mountId: "webgpu-feature-root",
          requiredCapabilities: ["webgpu", "webgpu:timestamp-query", "webgpu:limit:maxTextureDimension2D>=4096", "webgpu:adapter-limit:maxTextureDimension2D>=8192"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(mount.getAttribute("data-gosx-engine-capability-state"), "ready");
  assert.equal(mount.getAttribute("data-gosx-engine-supported-capabilities"), "webgpu webgpu:timestamp-query webgpu:limit:maxtexturedimension2d>=4096 webgpu:adapter-limit:maxtexturedimension2d>=8192");
  assert.deepEqual(Array.from(captured.status.required), ["webgpu", "webgpu:timestamp-query", "webgpu:limit:maxtexturedimension2d>=4096", "webgpu:adapter-limit:maxtexturedimension2d>=8192"]);
  assert.deepEqual(Array.from(captured.status.missing), []);
});

test("selective runtime connects hubs without loading the shared wasm runtime", async () => {
  const sockets = [];
  const fetchRoutes = {
    "/gosx/assets/runtime/bootstrap-feature-hubs.hashed.js": { text: bootstrapFeatureHubsSource },
  };
  const env = createContext({
    createWebSocket(url) {
      const socket = {
        url,
        closeCalled: false,
        close() {
          this.closeCalled = true;
        },
      };
      sockets.push(socket);
      return socket;
    },
    fetchRoutes,
    manifest: {
      hubs: [
        {
          id: "gosx-hub-runtime",
          name: "presence",
          path: "/gosx/hub/presence",
          bindings: [{ event: "snapshot", signal: "$presence" }],
        },
      ],
    },
  });
  const preload = env.document.createElement("link");
  preload.setAttribute("rel", "preload");
  preload.setAttribute("as", "script");
  preload.setAttribute("href", "/gosx/assets/runtime/bootstrap-feature-hubs.hashed.js");
  env.document.head.appendChild(preload);

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/assets/runtime/bootstrap-feature-hubs.hashed.js"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-hubs.js"), false);
  assert.equal(sockets.length, 1);
  assert.equal(String(sockets[0].url).includes("/gosx/hub/presence"), true);
});

test("engine factory context does not receive window or document", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scope-test-root";

  const capturedCtx = {};

  const env = createContext({
    elements: [mount],
    engineFactories: {
      ScopeTest(ctx) {
        capturedCtx.hasWindow = "window" in ctx;
        capturedCtx.hasDocument = "document" in ctx;
        capturedCtx.windowValue = ctx.window;
        capturedCtx.documentValue = ctx.document;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-scope",
          component: "ScopeTest",
          kind: "surface",
          mountId: "scope-test-root",
          capabilities: ["canvas"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(capturedCtx.hasWindow, false, "ctx must not expose window");
  assert.equal(capturedCtx.hasDocument, false, "ctx must not expose document");
  assert.equal(capturedCtx.windowValue, undefined, "ctx.window must be undefined");
  assert.equal(capturedCtx.documentValue, undefined, "ctx.document must be undefined");
});

test("engine factory context does not receive activateInputProviders", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "input-scope-root";

  const capturedCtx = {};

  const env = createContext({
    elements: [mount],
    engineFactories: {
      InputScopeTest(ctx) {
        capturedCtx.hasActivateInputProviders = "activateInputProviders" in ctx;
        capturedCtx.hasReleaseInputProviders = "releaseInputProviders" in ctx;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-input-scope",
          component: "InputScopeTest",
          kind: "surface",
          mountId: "input-scope-root",
          capabilities: [],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(capturedCtx.hasActivateInputProviders, false, "ctx must not expose activateInputProviders");
  assert.equal(capturedCtx.hasReleaseInputProviders, false, "ctx must not expose releaseInputProviders");
});

test("engine factory context does not receive activateInputProviders even with input capabilities", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "input-cap-root";

  const capturedCtx = {};

  const env = createContext({
    elements: [mount],
    engineFactories: {
      InputCapTest(ctx) {
        capturedCtx.hasActivateInputProviders = "activateInputProviders" in ctx;
        capturedCtx.capabilities = ctx.capabilities.slice();
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-input-cap",
          component: "InputCapTest",
          kind: "surface",
          mountId: "input-cap-root",
          capabilities: ["keyboard", "pointer", "gamepad"],
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(raf.count(), 1, "gamepad input provider should poll while the engine is mounted");
  assert.deepEqual(capturedCtx.capabilities, ["keyboard", "pointer", "gamepad"]);
  assert.equal(capturedCtx.hasActivateInputProviders, false, "ctx must not expose activateInputProviders even with input capabilities");
  await env.context.__gosx_dispose_page();
  assert.equal(raf.count(), 0, "page disposal should release the gamepad input provider RAF");
});

// chooseSceneBackend — backendCaps verdict tests
// The helper is exposed on window.__gosx_choose_scene_backend after running the
// main bootstrap script (which includes 20-scene-mount.js).

test("chooseSceneBackend selects webgl when backendCaps.capable==[webgl] despite preferWebGPU", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const choose = env.context.__gosx_choose_scene_backend;
  assert.ok(typeof choose === "function", "__gosx_choose_scene_backend must be exposed");

  const backendCaps = {
    capable: ["webgl"],
    degraded: {},
    reasons: [{ feature: "skinning", excludes: "webgpu" }],
  };
  const prefs = { preferWebGPU: true, requireWebGL: false, forceWebGL: false, preferCanvas: false };
  const availability = { webgpu: true, webgl: true };

  const result = choose(backendCaps, prefs, availability);

  assert.ok(result !== null, "result should not be null");
  assert.equal(result.backend, "webgl", "backend should be webgl (webgpu excluded by backendCaps)");
  assert.ok(result.fallbackReason.length > 0, "fallbackReason should be non-empty when downgraded from webgpu");
  assert.equal(result.fallbackReason, "skinning", "fallbackReason should be derived from the exclusion reason");
});

test("chooseSceneBackend selects webgpu and records degraded features when ibl is listed", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const choose = env.context.__gosx_choose_scene_backend;
  assert.ok(typeof choose === "function", "__gosx_choose_scene_backend must be exposed");

  const backendCaps = {
    capable: ["webgpu", "webgl"],
    degraded: { webgpu: ["ibl"] },
    reasons: [{ feature: "ibl", degrades: "webgpu" }],
  };
  const prefs = { preferWebGPU: true, requireWebGL: false, forceWebGL: false, preferCanvas: false };
  const availability = { webgpu: true, webgl: true };

  const result = choose(backendCaps, prefs, availability);

  assert.ok(result !== null, "result should not be null");
  assert.equal(result.backend, "webgpu", "backend should be webgpu (it is in capable[])");
  assert.equal(result.fallbackReason, "", "no fallbackReason when webgpu is selected");
  assert.deepEqual(result.degraded, ["ibl"], "ibl should be listed in degraded features");
});

test("chooseSceneBackend falls back to webgl when webgpu is capable but unavailable at runtime", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const choose = env.context.__gosx_choose_scene_backend;
  assert.ok(typeof choose === "function", "__gosx_choose_scene_backend must be exposed");

  const backendCaps = {
    capable: ["webgpu", "webgl"],
    degraded: {},
    reasons: [],
  };
  const prefs = { preferWebGPU: true, requireWebGL: false, forceWebGL: false, preferCanvas: false };
  const availability = { webgpu: false, webgl: true };

  const result = choose(backendCaps, prefs, availability);

  assert.ok(result !== null, "result should not be null");
  assert.equal(result.backend, "webgl", "backend should be webgl when webgpu is unavailable at runtime");
  assert.equal(result.fallbackReason, "webgpu-unavailable", "fallbackReason should be webgpu-unavailable");
});

test("chooseSceneBackend does not invent webgl for webgpu-only backendCaps", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const choose = env.context.__gosx_choose_scene_backend;
  assert.ok(typeof choose === "function", "__gosx_choose_scene_backend must be exposed");

  const backendCaps = {
    capable: ["webgpu"],
    degraded: {},
    reasons: [{ feature: "water-simulation", excludes: "webgl" }],
  };
  const prefs = { preferWebGPU: true, requireWebGL: false, forceWebGL: false, preferCanvas: false };
  const availability = { webgpu: false, webgl: true };

  const result = choose(backendCaps, prefs, availability);

  assert.ok(result !== null, "result should not be null");
  assert.equal(result.backend, null, "backend must stay unavailable when only webgpu is capable");
  assert.equal(result.fallbackReason, "no-capable-backend", "fallbackReason should report that no capable backend is available");
});

test("Scene3D renderer recovery respects backendCaps fallbacks", () => {
  const source = readSceneMountSrc();
  assert.match(source, /function restoreSceneWebGLRenderer\(reason\) \{[\s\S]*sceneBackendCapsAllowsKind\(sceneBackendCapsOf\(props\), "webgl"\)/);
  assert.match(source, /const allowWebGLFallback = sceneBackendCapsAllowsKind\(backendCaps, "webgl"\)/);
  assert.match(source, /const allowCanvasFallback = sceneBackendCapsAllowsKind\(backendCaps, "canvas2d"\)/);
  assert.match(source, /if \(!allowWebGLFallback && !allowCanvasFallback\) \{[\s\S]*renderer-fallback-disallowed/);
  assert.match(source, /if \(!allowCanvasFallback\) \{[\s\S]*renderer-canvas-fallback-disallowed/);
});

test("Scene3D fallbackSceneRenderer honors window.__gosx_scene3d_require_gpu without touching the WebGL fallback", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "mount.ts"), "utf8");
  // The gate is read once, right where allowCanvasFallback is computed, and
  // only that Canvas2D swap is disallowed -- the WebGL fallback attempt keeps
  // its own, unmodified allowWebGLFallback condition.
  assert.match(source, /const requireGPUOnly = typeof window !== "undefined" && window\.__gosx_scene3d_require_gpu === true;/);
  assert.match(source, /const allowCanvasFallback = sceneBackendCapsAllowsKind\(backendCaps, "canvas2d"\) && !requireGPUOnly;/);
  assert.match(source, /if \(!preferCanvasFallback && allowWebGLFallback\) \{/);
});

test("Scene3D WebGPU water debug gates isolate update and draw stages", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  assert.match(source, /new URLSearchParams\(window\.location\.search\)\.get\("gosx-water-debug"\)/);
  assert.match(source, /function sceneWebGPUWaterDebugSkipsUpdate\(mode\) \{[\s\S]*no-water[\s\S]*no-update/);
  assert.match(source, /function sceneWebGPUWaterDebugSkipsDraw\(mode\) \{[\s\S]*compute-only[\s\S]*no-draw/);
  assert.match(source, /sceneWebGPUWaterDebugSkipsUpdate\(waterDebugMode\)[\s\S]*updateWaterSystems\(\[\]/);
  assert.match(source, /hasWaterData && !sceneWebGPUWaterDebugSkipsDraw\(waterDebugMode\)/);
});

test("chooseSceneBackend returns null (backward-compat) when backendCaps is absent", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const choose = env.context.__gosx_choose_scene_backend;
  assert.ok(typeof choose === "function", "__gosx_choose_scene_backend must be exposed");

  const result = choose(null, { preferWebGPU: true }, { webgpu: true, webgl: true });
  assert.equal(result, null, "null backendCaps must return null so caller uses legacy path");
});

test("chooseSceneBackend honours forceWebGL override even when webgpu is in capable[]", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const choose = env.context.__gosx_choose_scene_backend;

  const backendCaps = { capable: ["webgpu", "webgl"], degraded: {}, reasons: [] };
  const prefs = { forceWebGL: true, requireWebGL: false, preferWebGPU: false, preferCanvas: false };
  const availability = { webgpu: true, webgl: true };

  const result = choose(backendCaps, prefs, availability);

  assert.ok(result !== null);
  assert.equal(result.backend, "webgl", "forceWebGL must override capable verdict");
  assert.equal(result.fallbackReason, "");
});

// read-path regression: sceneBackendCapsOf must extract backendCaps from props.scene
// This test proves that passing a props-shaped object (with backendCaps nested under
// props.scene) correctly routes to webgl via the skinning exclusion reason.
test("sceneBackendCapsOf extracts backendCaps from props.scene and honesty gate takes effect", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const choose = env.context.__gosx_choose_scene_backend;
  assert.ok(typeof choose === "function", "__gosx_choose_scene_backend must be exposed");

  const capsOf = env.context.__gosx_scene_backend_caps_of;
  assert.ok(typeof capsOf === "function", "__gosx_scene_backend_caps_of must be exposed");

  // props-shaped object: backendCaps nested under props.scene (as Go serializes it)
  const props = {
    preferWebGPU: true,
    scene: {
      backendCaps: {
        capable: ["webgl"],
        reasons: [{ feature: "skinning", excludes: "webgpu" }],
      },
    },
  };

  const extracted = capsOf(props);
  assert.ok(extracted !== null, "sceneBackendCapsOf must extract backendCaps from props.scene");
  assert.deepEqual(extracted.capable, ["webgl"], "extracted.capable should be [webgl]");

  const prefs = { preferWebGPU: true, requireWebGL: false, forceWebGL: false, preferCanvas: false };
  const availability = { webgpu: true, webgl: true };
  const result = choose(extracted, prefs, availability);

  assert.ok(result !== null, "chooseSceneBackend result must not be null");
  assert.equal(result.backend, "webgl", "backend must be webgl — skinning excludes webgpu");
  assert.equal(result.fallbackReason, "skinning", "fallbackReason must be skinning from reasons[]");
});

test("video sync JS fallback engine matches the Go golden parity vector", () => {
  const factory = loadVideoSyncJSEngineFactory();
  const golden = JSON.parse(
    fs.readFileSync(
      path.join(__dirname, "..", "videosync", "testdata", "parity_basic.json"),
      "utf8",
    ),
  );

  const engine = factory({});
  const got = [];
  for (const event of golden.events) {
    if (event.t === "ingest") {
      engine.ingest(
        event.serverTimeMs,
        event.position,
        event.playing,
        event.recvPerfMs == null ? 0 : event.recvPerfMs,
      );
    } else if (event.t === "rtt") {
      engine.rtt(event.rttMs);
    } else if (event.t === "tick") {
      // The golden ticks are all playing (paused=false); the engine derives
      // isPlaying internally from the last ingested heartbeat for projection.
      got.push(engine.tick(event.currentTime, event.perfNowMs, event.bufferedAhead, false));
    } else if (event.t === "playbackStart") {
      engine.onPlaybackStart(event.perfNowMs);
    }
  }

  const expected = golden.expected;
  assert.equal(
    got.length,
    expected.length,
    `expected ${expected.length} tick decisions, got ${got.length}`,
  );

  const EPS = 1e-3;
  for (let i = 0; i < expected.length; i += 1) {
    const e = expected[i];
    const g = got[i];
    const where = `tick ${i} (go reason="${e.reason}", js reason="${g.reason}")`;
    // Exact fields.
    assert.equal(g.kind, e.kind, `${where}: kind`);
    assert.equal(g.preloadPhase, e.preloadPhase, `${where}: preloadPhase`);
    assert.equal(Boolean(g.ready), Boolean(e.ready), `${where}: ready`);
    assert.equal(Boolean(g.stalled), Boolean(e.stalled), `${where}: stalled`);
    assert.equal(Boolean(g.resetRate), Boolean(e.resetRate), `${where}: resetRate`);
    // Near fields (1e-3).
    assert.ok(
      Math.abs(g.rate - e.rate) <= EPS,
      `${where}: rate expected ${e.rate} got ${g.rate}`,
    );
    assert.ok(
      Math.abs(g.seekTo - e.seekTo) <= EPS,
      `${where}: seekTo expected ${e.seekTo} got ${g.seekTo}`,
    );
    assert.ok(
      Math.abs(g.actualRate - e.actualRate) <= EPS,
      `${where}: actualRate expected ${e.actualRate} got ${g.actualRate}`,
    );
  }
});
