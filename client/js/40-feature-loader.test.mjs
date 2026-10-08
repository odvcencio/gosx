import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { bootstrapRuntimeSource, createContext, runScript, flushAsyncWork, FakeElement } = require("./runtime-test-harness.js");

const BRIDGE_URL = "/gosx/assets/runtime/bootstrap-feature-engine-bridge.abcd.js";
const chunk = (name, marker) => `(function(){
  window.__gosx_register_bootstrap_feature(${JSON.stringify(name)}, function(api) {
    window.${marker} = (window.${marker} || 0) + 1;
    window.${marker}_has_ensure = typeof api.ensureBootstrapFeature === "function";
    return { runtimeReady() { window.${marker}_ready = true; } };
  });
})();`;

function contractElement(assets) {
  const el = new FakeElement("script", null);
  el.id = "gosx-document";
  el.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true, ...assets } });
  return el;
}

test("manifest.features load from the derived bootstrapFeature<Name>Path contract key, once", async () => {
  const env = createContext({
    elements: [contractElement({ bootstrapFeatureEngineBridgePath: BRIDGE_URL })],
    fetchRoutes: { [BRIDGE_URL]: { text: chunk("engine-bridge", "__bridge_calls") } },
    manifest: { features: ["engine-bridge", "engine-bridge"], engines: [] },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  assert.equal(env.context.__bridge_calls, 1);
  assert.equal(env.context.__bridge_calls_ready, true);
  assert.equal(env.context.__bridge_calls_has_ensure, true);
  assert.deepEqual(env.fetchCalls.map((c) => String(c.url)), [BRIDGE_URL]);
  assert.equal(env.context.__gosx.ready, true);
});

test("a feature with no contract key falls back to the unhashed /gosx/ URL", async () => {
  const env = createContext({
    elements: [contractElement({})],
    fetchRoutes: { "/gosx/bootstrap-feature-painter.js": { text: chunk("painter", "__painter_calls") } },
    manifest: { features: ["painter"] },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  assert.equal(env.context.__painter_calls, 1);
});
