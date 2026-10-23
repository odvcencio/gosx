import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { bootstrapRuntimeSource, bootstrapFeatureEnginesSource, createContext, runScript, flushAsyncWork, installManualRAF, installManualTimers, flushSceneInitialFrameBoundary, FakeElement, FakeMutationObserver, sharedSignalValue } = require("./runtime-test-harness.js");

const dir = path.dirname(new URL(import.meta.url).pathname);
const read = (name) => fs.readFileSync(path.join(dir, name), "utf8");

function contract() {
  const el = new FakeElement("script", null);
  el.id = "gosx-document";
  el.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  return el;
}

test("a deferred scene publishes the renderer its mount settles on", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "hero";
  const observers = [];
  const tag = new FakeElement("script", null);
  tag.setAttribute("data-gosx-script", "bootstrap");
  const env = createContext({
    elements: [contract(), tag, mount],
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/gosx/bootstrap-feature-scene3d-defer.js": { text: read("bootstrap-feature-scene3d-defer.js") },
    },
    manifest: { features: ["scene3d-defer"], engines: [{ id: "gosx-engine-hero", component: "GoSXScene3D", kind: "surface", mountId: "hero", props: { scene: {} }, defer: { until: "now" } }] },
  });
  env.context.__gosx_scene3d_available = true; // the scene chunk is already on the page
  env.context.MutationObserver = class extends FakeMutationObserver { constructor(cb) { super(cb); observers.push(this); } };
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork(); await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-defer-state"), "mounted");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer"), null, "nothing is published before the scene picks a backend");
  mount.setAttribute("data-gosx-scene3d-backend", "webgl");
  observers.forEach((o) => o.trigger());
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer"), "webgl2");
  const value = sharedSignalValue(env, "$gosx.renderer");
  assert.equal(JSON.parse(JSON.stringify(value)).source, "gosx-engine-hero");
});

test("a page with no scene gets its renderer from the renderer-probe feature", async () => {
  const env = createContext({
    elements: [contract()], enableWebGL2: true, renderer: "ANGLE (Apple M2)",
    fetchRoutes: {
      "/gosx/bootstrap-feature-renderer-probe.js": { text: read("bootstrap-feature-renderer-probe.js") },
      "/gosx/bootstrap-feature-scene3d-defer.js": { text: read("bootstrap-feature-scene3d-defer.js") },
    },
    manifest: { features: ["renderer-probe"] },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  for (let i = 0; i < 4; i++) await flushAsyncWork();
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer"), "webgl2");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer-reason"), "hardware-gpu");
});
