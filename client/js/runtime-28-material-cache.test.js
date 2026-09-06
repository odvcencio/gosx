"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const h = require("./runtime-test-harness.js");
function setup() {
  const e = h.createContext({});
  h.runScript(h.bootstrapRuntimeSource, e.context, "bootstrap-runtime.js");
  const source = h.freshFeatureBundleSource("scene3d").replace(
    "window.__gosx_scene3d_available = true;",
    "window.__materialCacheTest = { profile: sceneObjectMaterialProfile, register: registerSceneMaterialProfile }; window.__gosx_scene3d_available = true;"
  );
  h.runScript(source, e.context, "bootstrap-feature-scene3d.js");
  return e.context.__materialCacheTest;
}
test("material profiles survive pose changes and invalidate scalar and nested edits", () => {
  const api = setup();
  const actor = { materialKind: "standard", color: "#abcdef", specularColor: [.2, .3, .4] };
  const first = api.profile(actor);
  for (let i = 0; i < 100; i++) {
    actor.x = i;
    assert.equal(api.profile(actor), first);
  }
  actor.specularColor[0] = .8;
  const changed = api.profile(actor);
  assert.notEqual(changed, first);
  assert.notEqual(changed.key, first.key);
  actor.color = "var(--actor-color)";
  const css = api.profile(actor);
  assert.notEqual(css, changed);
  assert.equal(css.color, "var(--actor-color)");
  delete actor.color;
  assert.notEqual(api.profile(actor), css);
});
test("material registration invalidates cached profiles and factories stay live", () => {
  const api = setup();
  const actor = { materialKind: "customcache" };
  api.register("customcache", { opacity: .6 });
  const first = api.profile(actor);
  api.register("customcache", { opacity: .9 });
  assert.notEqual(api.profile(actor), first);
  assert.equal(api.profile(actor).opacity, .9);
  let calls = 0;
  api.register("customcache", { shaderData: () => [++calls, 0, 1] });
  const one = api.profile(actor);
  const two = api.profile(actor);
  assert.notEqual(one, two);
  assert.notEqual(one.shaderData[0], two.shaderData[0]);
});
