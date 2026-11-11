"use strict";
// A scene that starts on WebGPU and then falls back to WebGL2 (for example when
// a WebGPU pipeline is rejected) runs the WebGL renderer in a page where the
// WebGPU renderer has already installed window.__gosx_scene3d_asset_ext.uv1 with
// only its own gpu* hooks. The WebGL renderer must add its use, bind and
// dispose hooks to that object instead of keeping it as it is, or the first
// draw throws "uv1.use is not a function" and the canvas stays blank.
const test = require("node:test");
const assert = require("node:assert/strict");
const {
  bootstrapRuntimeSource, freshFeatureBundleSource,
  createContext, runScript, FakeWebGLContext,
} = require("./runtime-test-harness.js");

test("the WebGL renderer completes the uv1 hooks that a WebGPU stub left behind", () => {
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  env.context.__gosx_scene3d_asset_ext = { uv1: { gpuWanted() { return ""; } } };
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  runScript(freshFeatureBundleSource("scene3d-compute"), env.context, "bootstrap-feature-scene3d-compute.js");
  runScript(freshFeatureBundleSource("scene3d-webgl"), env.context, "bootstrap-feature-scene3d-webgl.js");
  const registry = env.context.__gosx_scene3d_api.sceneBackendRegistry;
  const backend = registry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
  const canvas = env.document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 180;
  backend.create(canvas, { background: "#000000" }, { tier: "full" });
  const uv1 = env.context.__gosx_scene3d_asset_ext.uv1;
  for (const name of ["use", "bind", "dispose"]) assert.equal(typeof uv1[name], "function", "uv1." + name);
  assert.equal(typeof uv1.gpuWanted, "function", "the WebGPU hook stays");
});
