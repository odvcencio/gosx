// Class audit: a legacy feature named in manifest.features on a page that has
// no entry that would normally trigger it. The loader must never wait on a load
// signal for a script the renderer did not emit.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const H = require("./runtime-test-harness.js");
const { bootstrapRuntimeSource, bootstrapFeatureEnginesSource, createContext, runScript, flushAsyncWork, FakeElement } = H;
const chunkSource = (name) => fs.readFileSync(path.join(here, `bootstrap-feature-${name}.js`), "utf8");

function page(feature) {
  const mount = new FakeElement("div", null);
  mount.id = "js-root";
  const contract = new FakeElement("script", null);
  contract.id = "gosx-document";
  contract.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  const env = createContext({
    elements: [contract, mount],
    engineFactories: { JSFixture() { return { dispose() {} }; } },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
      "/gosx/bootstrap-feature-islands.js": { text: chunkSource("islands") },
      "/gosx/bootstrap-feature-hubs.js": { text: chunkSource("hubs") },
      "/gosx/bootstrap-feature-controllers.js": { text: chunkSource("controllers") },
    },
    // An ordinary JavaScript surface engine: no Scene3D engine on the page.
    manifest: { engines: [{ id: "js-engine", component: "JSFixture", kind: "surface", mountId: "js-root" }], features: [feature] },
  });
  return env;
}

async function boot(feature) {
  const env = page(feature);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await new Promise((resolve) => setTimeout(resolve, 20));
  await flushAsyncWork();
  return env;
}

for (const feature of ["islands", "engines", "hubs", "controllers", "textlayout"]) {
  test(`explicit ${feature} on a page without its trigger still finishes initialization`, async () => {
    const env = await boot(feature);
    assert.equal(env.context.__gosx.ready, true, "runtime ready");
    assert.ok(env.context.__gosx.engines.get("js-engine"), "the page's own engine mounts");
  });
}

// scene3d is the one legacy name the loader waits on a signal for
// (__gosx_scene3d_loaded, from a script the renderer emits only for a
// GoSXScene3D engine). Go never lets it reach manifest.features:
// hydrate.RequireFeature rejects it and island's client manifest drops it. See
// hydrate/features_test.go and island/features_test.go.
