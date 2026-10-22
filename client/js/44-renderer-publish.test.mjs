import test from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { bootstrapSource, bootstrapRuntimeSource, createContext, runScript, flushAsyncWork, installManualRAF, flushSceneInitialFrameBoundary, FakeElement, sharedSignalValue } = require("./runtime-test-harness.js");

test("a WebGL scene mount publishes renderer webgl2 on the document", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene";
  const env = createContext({
    elements: [mount], enableWebGL2: true, enableWebGL: true,
    manifest: { engines: [{ id: "gosx-engine-scene", component: "GoSXScene3D", kind: "surface", mountId: "scene",
      props: { width: 64, height: 64, requireWebGL: true, scene: { objects: [{ id: "box", kind: "box", size: 1, color: "#fff" }] } } }] },
  });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  assert.equal(mount.getAttribute("data-gosx-scene3d-backend"), "webgl");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-renderer"), "webgl2");
  const value = sharedSignalValue(env, "$gosx.renderer");
  assert.equal(value.renderer, "webgl2");
  assert.equal(value.source, "scene");
});
