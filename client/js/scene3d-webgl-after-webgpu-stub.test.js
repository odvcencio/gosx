"use strict";
// A scene that starts on WebGPU and then falls back to WebGL2 (for example when
// a WebGPU pipeline is rejected) runs the WebGL renderer in a page where the
// WebGPU renderer has already installed window.__gosx_scene3d_asset_ext.uv1 with
// only its own gpu* hooks. Without the glTF chunk nothing supplies use, bind or
// dispose, so the first draw threw "uv1.use is not a function" and the canvas
// stayed blank.
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const {
  bootstrapRuntimeSource, freshFeatureBundleSource, makeWebGLBundleWithCustomPost,
  createContext, runScript, FakeWebGLContext,
} = require("./runtime-test-harness.js");

test("the WebGL renderer draws and disposes when a WebGPU stub left uv1 without its hooks", () => {
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
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  assert.equal(typeof env.context.__gosx_scene3d_asset_ext.uv1.use, "undefined", "the stub keeps no WebGL hooks");
  // A lit mesh with a material takes the authored-program path that calls
  // uv1.use, and then uv1.bind for the draw. A bundle with only a point never
  // reaches either call.
  const F32 = vm.runInContext("Float32Array", env.context);
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = [];
  bundle.points = [];
  bundle.lights = [{ kind: "directional", color: "#ffffff", intensity: 1, directionX: 0, directionY: -1, directionZ: -1 }];
  bundle.materials = [{ kind: "standard", color: "#c0c0c0", roughness: 0.5, metalness: 0.1 }];
  bundle.meshObjects = [{
    id: "m", kind: "mesh", materialIndex: 0, renderPass: "opaque", static: false, castShadow: false, receiveShadow: false,
    bounds: { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 }, depthNear: 0, depthFar: 0, depthCenter: 0, viewCulled: false,
    vertices: { positions: new F32([0, 1, 0, -1, -1, 0, 1, -1, 0]), normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1]), uvs: new F32([0.5, 1, 0, 0, 1, 0]), count: 3, immutable: true },
    directVertices: true, retainedGeometry: true, geometryRevision: 1, resourceOwner: {},
    modelMatrix: new F32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]), vertexOffset: 0, vertexCount: 3,
  }];
  const gl = canvas.getContext("webgl2");
  renderer.render(bundle, { width: canvas.width, height: canvas.height });
  assert.ok(gl.ops.some((op) => op[0] === "drawArrays" || op[0] === "drawElements"), "the mesh draw reaches GL");
  renderer.dispose();
});
