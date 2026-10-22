import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const {
  bootstrapRuntimeSource, bootstrapFeatureEnginesSource, createContext, runScript, flushAsyncWork,
  installManualRAF, installManualTimers, flushSceneInitialFrameBoundary, FakeElement, sharedSignalValue,
} = require("./runtime-test-harness.js");

const SCENE_CHUNK = "/gosx/bootstrap-feature-scene3d.js";
const PROPS_URL = "/gosx/scene3d/0123456789abcdef0123456789abcdef.json";
// Stand-in for the real scene chunk: registers the engine factory and signals
// availability the way 26d-feature-scene3d-suffix.ts does.
const STUB_SCENE_CHUNK = `(function(){
  window.__gosx_register_engine_factory("GoSXScene3D", function(ctx) {
    window.__stub_scene_mounts = (window.__stub_scene_mounts || []).concat([ctx.props]);
    return { dispose() {} };
  });
  window.__gosx_scene3d_available = true;
  if (typeof window.__gosx_scene3d_loaded === "function") window.__gosx_scene3d_loaded();
})();`;

function bootstrapTag() {
  const el = new FakeElement("script", null);
  el.setAttribute("data-gosx-script", "bootstrap");
  el.setAttribute("data-gosx-scene3d-url", SCENE_CHUNK);
  el.setAttribute("data-gosx-scene3d-webgl-url", "/gosx/bootstrap-feature-scene3d-webgl.js");
  el.setAttribute("data-gosx-scene3d-webgpu-url", "/gosx/bootstrap-feature-scene3d-webgpu.js");
  return el;
}
function contractElement() {
  const el = new FakeElement("script", null);
  el.id = "gosx-document";
  el.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  return el;
}
function deferredEntry(id, extra = {}) {
  return { id: "gosx-engine-" + id, component: "GoSXScene3D", kind: "surface", mountId: id, props: null, propsRef: PROPS_URL,
    defer: { until: "idle", preloads: [{ href: "/models/" + id + ".glb", as: "fetch" }] }, ...extra };
}
function page(entries, options = {}) {
  const mounts = entries.map((e) => { const m = new FakeElement("div", null); m.id = e.mountId; const poster = new FakeElement("img", null); poster.setAttribute("data-poster", "1"); m.appendChild(poster); return m; });
  const env = createContext({
    elements: [contractElement(), bootstrapTag(), ...mounts],
    enableWebGL2: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      [SCENE_CHUNK]: { text: STUB_SCENE_CHUNK },
      [PROPS_URL]: { text: JSON.stringify({ width: 10, height: 10, scene: { objects: [] } }) },
    },
    manifest: { engines: entries },
    ...options,
  });
  const raf = installManualRAF(env.context);
  const timers = installManualTimers(env.context);
  return { env, mounts, raf, timers, urls: () => env.fetchCalls.map((c) => String(c.url)) };
}
// Two frames, then the 250 ms requestIdleCallback fallback timer.
async function reachIdle(raf, timers) {
  await flushSceneInitialFrameBoundary(raf);
  timers.runDelay(250);
  await flushAsyncWork();
  await flushAsyncWork();
}

test("a deferred scene downloads no chunk and no props at bootstrap", async () => {
  const { env, urls } = page([deferredEntry("hero")]);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  assert.equal(env.context.__gosx.ready, true, "ready must not wait for the deferred scene");
  assert.ok(!urls().includes(SCENE_CHUNK) && !urls().includes(PROPS_URL), "no scene bytes before idle: " + urls().join(","));
});

test("a gated eager scene inserts the chunk tag itself once the gate holds", async () => {
  const { env, urls } = page([{ ...deferredEntry("eager"), defer: undefined, propsRef: undefined, props: { width: 10, height: 10, scene: { objects: [] } }, requiredCapabilities: ["full-motion"] }]);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork(); await flushAsyncWork();
  assert.ok(urls().includes(SCENE_CHUNK), "the runtime must insert the scene chunk tag");
  const tag = env.document.querySelector('script[data-gosx-script="feature-scene3d"]');
  assert.equal(tag.getAttribute("data-gosx-scene3d-webgl-url"), "/gosx/bootstrap-feature-scene3d-webgl.js", "sub-chunk URLs copied from the bootstrap tag");
  assert.equal((env.context.__stub_scene_mounts || []).length, 1);
});
