"use strict";
// scene.Props.WebGPURecovery: strike counter on the WebGPU render watchdog,
// the single reload request, and the published recovery state.
//
// The WebGPU chunk and renderer are fakes, the way
// runtime-09-scene-backend-selection.test.js builds them. `diag` is the mutable
// diagnostics object the fake renderer returns.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapFeatureEnginesSource,
  bootstrapFeatureScene3DSource,
  bootstrapRuntimeSource,
  FakeElement,
  createContext,
  installManualRAF,
  installManualTimers,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const RELOADS_KEY = "gosx:scene3d:webgpu-recovery-reloads";

async function mountWithRecovery(recovery, options = {}) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-recovery";
  let now = 0;
  const events = [];
  const env = createContext({
    elements: [mount],
    enableWebGPU: true,
    enableWebGL2: true,
    performanceNow: () => now,
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
          window.__testDiag = { ready: true, frameErrorStreak: 0, postFXDisabled: false, lastError: "" };
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function(canvas) {
              canvas.__webgpuClaimed = true;
              return {
                kind: "webgpu",
                diagnostics: function() { return window.__testDiag; },
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
      engines: [{
        id: "gosx-engine-webgpu-recovery",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: "scene-webgpu-recovery",
        jsExport: "GoSXScene3D",
        props: {
          width: 320,
          height: 180,
          preferWebGPU: true,
          autoRotate: true,
          webgpuRecovery: recovery,
          scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] },
        },
      }],
    },
  });
  // A claimed WebGPU canvas refuses 2D and WebGL contexts, as in runtime-09.
  const originalCreateElement = env.document.createElement.bind(env.document);
  env.document.createElement = function(tagName) {
    const element = originalCreateElement(tagName);
    if (String(tagName || "").toLowerCase() === "canvas") {
      const originalGetContext = element.getContext.bind(element);
      element.getContext = function(kind, contextOptions) {
        if (this.__webgpuClaimed && ["2d", "webgl", "webgl2", "experimental-webgl"].includes(String(kind || ""))) return null;
        return originalGetContext(kind, contextOptions);
      };
    }
    return element;
  };
  if (options.sessionStorage) {
    const store = options.sessionStorage;
    env.context.sessionStorage = {
      getItem: (key) => (Object.prototype.hasOwnProperty.call(store, key) ? store[key] : null),
      setItem: (key, value) => { store[key] = String(value); },
      removeItem: (key) => { delete store[key]; },
    };
  }
  if (options.forceWebGL) env.context.__gosx_scene3d_force_webgl = true;
  const timers = installManualTimers(env.context);
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  env.context.__gosx_emit = (level, cat, msg, fields) => { events.push({ level, cat, msg, fields: fields || {} }); };
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  timers.runDelay(0);
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();
  const check = async () => { now += 2000; timers.runInterval(2000); await flushAsyncWork(); };
  return { env, mount, timers, events, check, diag: env.context.__testDiag };
}

test("three consecutive erroring watchdog checks request a WebGL reload once and publish the state", async () => {
  const h = await mountWithRecovery({ strikes: 3, reload: true }, { sessionStorage: {} });
  const requests = [];
  h.env.context.__gosx_scene3d_request_webgl_recovery = (o) => { requests.push(o); return { forceWebGL: true, reload: !!o.reload }; };
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "armed");
  h.diag.frameErrorStreak = 4; h.diag.lastError = "Buffer with '' label is invalid";
  await h.check(); await h.check();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery-strikes"), "2");
  assert.equal(requests.length, 0);
  await h.check();
  assert.equal(JSON.stringify(requests), '[{"reload":true}]', "the request object crosses a vm realm, so compare as JSON");
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "reloading");
  assert.ok(h.events.some((e) => e.msg === "webgpu-recovery-reload" && e.fields.strikes === 3));
  await h.check();
  assert.equal(requests.length, 1, "never asks twice");
  assert.equal(h.env.context.sessionStorage.getItem(RELOADS_KEY), "1");
});

test("a clean check resets the strike count", async () => {
  const h = await mountWithRecovery({ strikes: 3, reload: true }, { sessionStorage: {} });
  h.diag.frameErrorStreak = 1; await h.check(); await h.check();
  h.diag.frameErrorStreak = 0; h.diag.lastError = ""; await h.check();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery-strikes"), null, "a clean check clears the attribute");
});

test("without reload the strikes force the in-page WebGL fallback once", async () => {
  const h = await mountWithRecovery({ strikes: 2 });
  h.diag.frameErrorStreak = 1; await h.check(); await h.check();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "fallback");
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-backend"), "webgl");
  assert.ok(h.events.some((e) => e.msg === "webgpu-recovery-fallback" && e.fields.strikes === 2));
});

test("a second reload in one tab session is refused and falls back in place", async () => {
  const h = await mountWithRecovery({ strikes: 1, reload: true }, { sessionStorage: { [RELOADS_KEY]: "1" } });
  const requests = [];
  h.env.context.__gosx_scene3d_request_webgl_recovery = (o) => requests.push(o);
  h.diag.frameErrorStreak = 1; await h.check();
  assert.equal(requests.length, 0);
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "fallback");
});

test("a WebGL-forced boot publishes webgl-forced and counts nothing", async () => {
  const h = await mountWithRecovery({ strikes: 3, reload: true }, { forceWebGL: true });
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "webgl-forced");
  await h.check();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery-strikes"), null);
});

test("a scene without webgpuRecovery publishes nothing", async () => {
  const h = await mountWithRecovery(undefined);
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), null);
  h.diag.frameErrorStreak = 1; await h.check(); await h.check(); await h.check();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery-strikes"), null);
});

for (const [label, patch] of [
  ["a lost device", { deviceLost: true }],
  ["a failed pipeline", { pipelineCoreError: "bad pipeline" }],
  ["a failed init", { initFailed: true }],
  ["a renderer that is not ready", { ready: false }],
]) {
  test("hard failure (" + label + ") counts as a strike and reloads at the threshold, never swaps first", async () => {
    const h = await mountWithRecovery({ strikes: 3, reload: true }, { sessionStorage: {} });
    const requests = [];
    h.env.context.__gosx_scene3d_request_webgl_recovery = (o) => { requests.push(o); return { forceWebGL: true, reload: true }; };
    Object.assign(h.diag, patch);
    await h.check(); await h.check();
    assert.equal(requests.length, 0, "no reload before the third strike");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "armed");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery-strikes"), "2");
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu", "no in-place swap while strikes accumulate");
    await h.check();
    assert.equal(requests.length, 1);
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "reloading");
  });
}

test("hard failure without reload swaps in place at once", async () => {
  const h = await mountWithRecovery({ strikes: 3 });
  h.diag.deviceLost = true;
  await h.check();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "fallback");
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
});

test("hard failure after the tab's one reload was used swaps in place at once", async () => {
  const h = await mountWithRecovery({ strikes: 3, reload: true }, { sessionStorage: { [RELOADS_KEY]: "1" } });
  const requests = [];
  h.env.context.__gosx_scene3d_request_webgl_recovery = (o) => { requests.push(o); };
  h.diag.pipelineCoreError = "bad";
  await h.check();
  assert.equal(requests.length, 0);
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "fallback");
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
});

test("when the bridge declines the reload the scene swaps in place instead of staying reloading", async () => {
  const h = await mountWithRecovery({ strikes: 1, reload: true }, { sessionStorage: {} });
  h.env.context.__gosx_scene3d_request_webgl_recovery = () => ({ forceWebGL: true, reload: false });
  h.diag.frameErrorStreak = 1;
  await h.check();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "fallback");
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
});

test("a storage that reads but cannot write never reloads and falls back in place", async () => {
  const h = await mountWithRecovery({ strikes: 1, reload: true }, {});
  h.env.context.sessionStorage = { getItem: () => null, setItem: () => {}, removeItem: () => {} };
  const requests = [];
  h.env.context.__gosx_scene3d_request_webgl_recovery = (o) => { requests.push(o); };
  h.diag.frameErrorStreak = 1; await h.check();
  assert.equal(requests.length, 0);
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "fallback");
});

test("a storage whose write throws never reloads", async () => {
  const h = await mountWithRecovery({ strikes: 1, reload: true }, {});
  h.env.context.sessionStorage = { getItem: () => null, setItem: () => { throw new Error("quota"); }, removeItem: () => {} };
  const requests = [];
  h.env.context.__gosx_scene3d_request_webgl_recovery = (o) => { requests.push(o); };
  h.diag.frameErrorStreak = 1; await h.check();
  assert.equal(requests.length, 0);
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-webgpu-recovery"), "fallback");
});
