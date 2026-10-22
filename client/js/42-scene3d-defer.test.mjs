import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const {
  bootstrapRuntimeSource, bootstrapFeatureEnginesSource, createContext, runScript, flushAsyncWork,
  installManualRAF, installManualTimers, flushSceneInitialFrameBoundary, FakeElement, sharedSignalValue,
} = require("./runtime-test-harness.js");

const DEFER_CHUNK = "/gosx/bootstrap-feature-scene3d-defer.js";
const deferChunkSource = fs.readFileSync(path.join(path.dirname(new URL(import.meta.url).pathname), "bootstrap-feature-scene3d-defer.js"), "utf8");
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
      [DEFER_CHUNK]: { text: deferChunkSource },
      [SCENE_CHUNK]: { text: STUB_SCENE_CHUNK },
      [PROPS_URL]: { text: JSON.stringify({ width: 10, height: 10, scene: { objects: [] } }) },
    },
    manifest: { engines: entries, features: ["scene3d-defer"] },
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
  const { env, urls } = page([{ ...deferredEntry("eager"), defer: { until: "now" }, propsRef: undefined, props: { width: 10, height: 10, scene: { objects: [] } }, requiredCapabilities: ["full-motion"] }]);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork(); await flushAsyncWork();
  assert.ok(urls().includes(SCENE_CHUNK), "the runtime must insert the scene chunk tag");
  const tag = env.document.querySelector('script[data-gosx-script="feature-scene3d"]');
  assert.equal(tag.getAttribute("data-gosx-scene3d-webgl-url"), "/gosx/bootstrap-feature-scene3d-webgl.js", "sub-chunk URLs copied from the bootstrap tag");
  assert.equal((env.context.__stub_scene_mounts || []).length, 1);
});

test("after two frames and idle the scene preloads, fetches its props, loads the chunk and mounts", async () => {
  const { env, mounts, raf, timers, urls } = page([deferredEntry("hero")]);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  assert.equal(mounts[0].getAttribute("data-gosx-scene3d-defer-state"), "scheduled");
  await reachIdle(raf, timers);
  const hrefs = Array.from(env.document.head.children).filter((n) => n.tagName === "LINK").map((n) => n.getAttribute("href"));
  assert.ok(hrefs.includes(SCENE_CHUNK) && hrefs.includes("/gosx/bootstrap-feature-scene3d-webgl.js") && hrefs.includes("/models/hero.glb"), "preloads: " + hrefs.join(","));
  assert.ok(!hrefs.includes("/gosx/bootstrap-feature-scene3d-webgpu.js"), "no WebGPU chunk hint without navigator.gpu");
  assert.ok(urls().includes(PROPS_URL) && urls().includes(SCENE_CHUNK));
  assert.equal(env.context.__stub_scene_mounts.length, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(env.context.__stub_scene_mounts[0].scene)), { objects: [] });
  assert.equal(mounts[0].getAttribute("data-gosx-scene3d-defer-state"), "mounted");
});

test("two deferred scenes on one page both mount, each fetching its own props once", async () => {
  const second = deferredEntry("field", { propsRef: PROPS_URL.replace("0123", "ffff") });
  const { env, raf, timers, urls } = page([deferredEntry("hero"), second], { fetchRoutes: {
    [DEFER_CHUNK]: { text: deferChunkSource },
    "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
    [SCENE_CHUNK]: { text: STUB_SCENE_CHUNK },
    [PROPS_URL]: { text: JSON.stringify({ scene: { objects: [] } }) },
    [second.propsRef]: { text: JSON.stringify({ scene: { objects: [], points: [] } }) },
  } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await reachIdle(raf, timers);
  assert.equal(env.context.__stub_scene_mounts.length, 2);
  assert.equal(urls().filter((u) => u === PROPS_URL).length, 1);
  assert.equal(urls().filter((u) => u === second.propsRef).length, 1);
  assert.equal(urls().filter((u) => u === SCENE_CHUNK).length, 1, "the chunk loads once for both scenes");
});

test("disposing the page before idle cancels the deferred load", async () => {
  const { env, raf, timers, urls } = page([deferredEntry("hero")]);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await env.context.__gosx_dispose_page(new Set());
  await reachIdle(raf, timers);
  assert.ok(!urls().includes(PROPS_URL) && !urls().includes(SCENE_CHUNK), "nothing may load for a disposed page: " + urls().join(","));
});

test("a failed gate keeps the poster, downloads nothing and publishes renderer none", async () => {
  const { env, mounts, raf, timers, urls } = page([deferredEntry("hero", { requiredCapabilities: ["full-motion", "hardware-gpu"] })], { prefersReducedMotion: true });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await reachIdle(raf, timers);
  assert.ok(!urls().includes(PROPS_URL) && !urls().includes(SCENE_CHUNK), "a failed gate must fetch nothing: " + urls().join(","));
  assert.equal(mounts[0].getAttribute("data-gosx-scene3d-defer-state"), "gated");
  assert.ok(mounts[0].children.some((c) => c.getAttribute && c.getAttribute("data-poster") === "1"), "the poster stays");
  assert.equal(mounts[0].getAttribute("data-gosx-engine-capability-state"), "unsupported");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer"), "none");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer-reason"), "full-motion");
  assert.deepEqual(JSON.parse(JSON.stringify(sharedSignalValue(env, "$gosx.renderer"))), { renderer: "none", reason: "full-motion", source: "gosx-engine-hero" });
  assert.equal(env.consoleLogs.warn.filter((l) => l.includes("full-motion")).length, 1);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("a matching media variant fetches its own props instead of the base", async () => {
  const SMALL_URL = PROPS_URL.replace("0123", "5555");
  for (const [matches, want, other] of [[true, SMALL_URL, PROPS_URL], [false, PROPS_URL, SMALL_URL]]) {
    const entry = deferredEntry("hero", { defer: { until: "idle", variants: [{ media: "(max-width: 767px)", propsRef: SMALL_URL }] } });
    const { env, raf, timers, urls } = page([entry], {
      matchMedia: { "(max-width: 767px)": matches },
      fetchRoutes: {
        [DEFER_CHUNK]: { text: deferChunkSource },
        "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
        [SCENE_CHUNK]: { text: STUB_SCENE_CHUNK },
        [PROPS_URL]: { text: JSON.stringify({ scene: { objects: [] } }) },
        [SMALL_URL]: { text: JSON.stringify({ width: 320, scene: { objects: [] } }) },
      },
    });
    runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
    await flushAsyncWork();
    await reachIdle(raf, timers);
    assert.ok(urls().includes(want), "media=" + matches + " must fetch " + want + ": " + urls().join(","));
    assert.ok(!urls().includes(other), "media=" + matches + " must not fetch " + other);
  }
});
