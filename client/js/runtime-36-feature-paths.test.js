"use strict";
// Feature chunk URLs come from the document contract.
//
// On 2026-09-29 a Scene3D page went black in Firefox, Chrome and Edge. The
// scene's accessible "label" string matched the runtime's label test, so the
// runtime asked for the text-layout chunk. The document contract carried no
// URL for that chunk, the loader fell back to an unhashed /gosx/ URL that the
// server did not serve, and the failed load rejected every manifest feature,
// scene included. These tests pin the three fixes.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapRuntimeSource,
  bootstrapFeatureEnginesSource,
  bootstrapFeatureTextLayoutSource,
  createContext,
  runScript,
  flushAsyncWork,
  FakeElement,
} = require("./runtime-test-harness.js");

const ENGINES_URL = "/gosx/assets/runtime/bootstrap-feature-engines.7b56.js";
const TEXTLAYOUT_URL = "/gosx/assets/runtime/bootstrap-feature-textlayout.c072.js";

function sceneContext(props, routes) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-root";
  const contract = new FakeElement("script", null);
  contract.id = "gosx-document";
  contract.textContent = JSON.stringify({
    version: 1,
    assets: {
      bootstrapMode: "full",
      manifest: true,
      bootstrapFeatureEnginesPath: ENGINES_URL,
      bootstrapFeatureTextLayoutPath: TEXTLAYOUT_URL,
      engines: 1,
    },
  });
  const env = createContext({
    elements: [contract, mount],
    engineFactories: {
      GoSXScene3D(ctx) {
        ctx.mount.setAttribute("data-mounted", "true");
        return { dispose() {} };
      },
    },
    fetchRoutes: routes,
    manifest: {
      engines: [{
        id: "gosx-engine-scene",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: "scene-root",
        props,
      }],
    },
  });
  // The scene3d feature arrives by its own script tag; mark it present.
  env.context.__gosx_scene3d_available = true;
  return { env, mount };
}

function fetchedURLs(env) {
  return env.fetchCalls.map((call) => String(call.url));
}

test("a scene label loads the text-layout chunk from the contract's hashed URL", async () => {
  const { env, mount } = sceneContext(
    { label: "A beach", labels: [{ id: "sign", text: "Blackglass" }] },
    {
      [ENGINES_URL]: { text: bootstrapFeatureEnginesSource },
      [TEXTLAYOUT_URL]: { text: bootstrapFeatureTextLayoutSource },
    },
  );
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  const urls = fetchedURLs(env);
  assert.ok(urls.includes(TEXTLAYOUT_URL), "expected the hashed text-layout URL, got " + JSON.stringify(urls));
  assert.ok(!urls.includes("/gosx/bootstrap-feature-textlayout.js"), "must not fall back to the unhashed URL");
  assert.equal(mount.getAttribute("data-mounted"), "true");
  assert.deepEqual(env.consoleLogs.error, []);
});

test("a scene's accessible label string does not fetch the text-layout chunk", async () => {
  const { env, mount } = sceneContext(
    { label: "Blackglass Beach at golden hour", ariaLabel: "A beach" },
    { [ENGINES_URL]: { text: bootstrapFeatureEnginesSource } },
  );
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  const urls = fetchedURLs(env);
  assert.ok(!urls.some((url) => url.includes("bootstrap-feature-textlayout")), "unexpected text-layout fetch: " + JSON.stringify(urls));
  assert.equal(mount.getAttribute("data-mounted"), "true");
  assert.deepEqual(env.consoleLogs.error, []);
});

test("a text-layout chunk that fails to load does not stop the scene from mounting", async () => {
  const { env, mount } = sceneContext(
    { labels: [{ id: "sign", text: "Blackglass" }] },
    // No route for the text-layout chunk: the load fails.
    { [ENGINES_URL]: { text: bootstrapFeatureEnginesSource } },
  );
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.ok(fetchedURLs(env).includes(TEXTLAYOUT_URL));
  assert.equal(mount.getAttribute("data-mounted"), "true", "the scene must mount without text layout");
  assert.ok(
    env.consoleLogs.warn.some((entry) => String(entry).includes("[gosx] textlayout:")),
    "expected a warning, got " + JSON.stringify(env.consoleLogs.warn),
  );
});
