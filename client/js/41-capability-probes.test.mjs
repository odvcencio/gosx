import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const {
  bootstrapRuntimeSource,
  bootstrapFeatureEnginesSource,
  createContext,
  runScript,
  flushAsyncWork,
  FakeElement,
} = require("./runtime-test-harness.js");

const PROBE_URL = "/gosx/bootstrap-feature-probe-fixture.js";
const PROBE_CHUNK = `(function(){
  window.__gosx_register_bootstrap_feature("probe-fixture", function(api) {
    api.registerCapabilityProbe("midi", function() { return true; });
    return {};
  });
})();`;

function contractElement() {
  const el = new FakeElement("script", null);
  el.id = "gosx-document";
  el.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  return el;
}

function midiEngine() {
  return { id: "midi-engine", component: "MidiFixture", kind: "surface", requiredCapabilities: ["midi"] };
}

function fixture(features) {
  const mount = new FakeElement("div", null);
  mount.id = "midi-root";
  const entry = { ...midiEngine(), mountId: "midi-root" };
  const env = createContext({
    elements: [contractElement(), mount],
    engineFactories: { MidiFixture() { return { dispose() {} }; } },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      [PROBE_URL]: { text: PROBE_CHUNK },
    },
    manifest: { engines: [entry], ...(features ? { features } : {}) },
  });
  return { env, mount };
}

test("a probe registered by a loaded feature enables the capability (case A)", async () => {
  const { env } = fixture(["probe-fixture"]);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  assert.ok(env.context.__gosx.engines.get("midi-engine"), "engine must mount once the probe says yes");
});

test("an unknown capability with no probe stays unsupported and says why (case B)", async () => {
  const { env, mount } = fixture(null);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  assert.equal(env.context.__gosx.engines.get("midi-engine"), undefined);
  const wrapper = mount.children[0];
  assert.equal(wrapper.getAttribute("data-gosx-engine-unsupported-reason"), "missing-capability");
  assert.equal(env.consoleLogs.error.filter((line) => line.includes("midi")).length, 1);
});

test("a no-probe miss is never cached: the probe added later wins (case C)", async () => {
  const { env, mount } = fixture(null);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  assert.equal(env.context.__gosx.engines.get("midi-engine"), undefined);

  const manifestEl = env.document.getElementById("gosx-manifest");
  const manifest = JSON.parse(manifestEl.textContent);
  manifest.features = ["probe-fixture"];
  manifestEl.textContent = JSON.stringify(manifest);
  // A soft navigation swaps in a new manifest element, which misses the parse
  // memo; dropping the memo is the same thing for this fixture.
  env.context.__gosx_manifest = undefined;
  mount.children.length = 0;
  await env.context.__gosx_dispose_page();
  await env.context.__gosx_bootstrap_page();
  await flushAsyncWork();
  assert.ok(env.context.__gosx.engines.get("midi-engine"), "the engine must mount after the probe loads");
});
