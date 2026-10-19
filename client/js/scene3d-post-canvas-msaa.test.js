"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const H = require("./runtime-test-harness.js");

async function mountPost(extra = {}) {
  const mount = new H.FakeElement("div", null);
  mount.id = "post-canvas-msaa";
  const env = H.createContext({elements: [mount], enableWebGL2: true, disableCanvas2D: true,
    createWebGL2Context() {
      const gl = new H.FakeWebGLContext();
      gl.textureSizes = [];
      const upload = gl.texImage2D.bind(gl);
      gl.texImage2D = (...args) => {if (args.length === 9) gl.textureSizes.push([args[3], args[4]]); upload(...args);};
      return gl;
    },
    manifest: {engines: [{id: "post-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: {width: 640, height: 360, adaptiveQuality: false,
        scene: {objects: [{id: "triangle", kind: "gltf-mesh", materialKind: "standard", wireframe: false,
          vertices: {positions: [-1, -1, 0, 1, -1, 0, 0, 1, 0], normals: [0, 0, 1, 0, 0, 1, 0, 0, 1]}}], postEffects: [{kind: "bloom"}, {kind: "toneMapping", mode: "aces"}]}, ...extra}}]}});
  env.context.WebGL2RenderingContext = H.FakeWebGLContext;
  const raf = H.installManualRAF(env.context);
  H.runScript(H.bootstrapSource, env.context, "bootstrap.js");
  for (let i = 0; i < 12; i++) await H.flushAsyncWork();
  await H.flushSceneInitialFrameBoundary(raf);
  const canvas = mount.children[0];
  return {mount, canvas, gl: canvas._webglContext, raf, env};
}

test("undeferred post scenes avoid canvas MSAA and color-only depth buffers", async () => {
  const s = await mountPost();
  const options = s.canvas.contextCalls.filter(c => c.kind === "webgl2").at(-1).options;
  const depthBuffers = s.gl.ops.filter(op => op[0] === "renderbufferStorage" && op[2] === s.gl.DEPTH_COMPONENT24);
  console.log(`post canvas antialias=${options.antialias}, intermediate depth buffers=${depthBuffers.length}`);
  assert.equal(options.antialias, false);
  assert.equal(depthBuffers.length, 0, "post intermediates only need color; scene depth is a texture");
});

test("explicit MSAA samples preserve canvas requests and deferred post keeps canvas MSAA", async () => {
  for (const [extra, expected] of [[{msaaSamples: 4}, true], [{msaaSamples: 1}, false], [{deferPostFX: true}, true]]) {
    const s = await mountPost(extra);
    assert.equal(s.canvas.contextCalls.filter(c => c.kind === "webgl2").at(-1).options.antialias, expected);
  }
});

test("turning initial post effects off retains antialiasing through FXAA", async () => {
  const s = await mountPost();
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-antialiasing"), "none");
  const state = s.mount.__gosxScene3DState;
  const handle = s.mount.__gosxScene3DHandle;
  handle.applyCommands([{kind: 7, data: {postEffects: []}}]);
  s.raf.flush(48);
  await H.flushAsyncWork();
  s.raf.flush(64);
  assert.equal(state.postEffects.length, 0, "fallback must not change authored effects");
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
  const outputModes = s.gl.ops.filter(op => op[0] === "uniform1i" && op[1] === "u_outputLinear");
  assert.ok(outputModes.length > 0);
  assert.equal(outputModes.at(-1)[2], 0, "FXAA fallback filters display colors without changing tone mapping or exposure");
  assert.equal(s.env.consoleLogs.error.length, 0);
});

test("unsupported specular-only bloom retains FXAA for points on the full tier", async () => {
  const s = await mountPost({scene: {points: [{positions: [0, 0, 0], count: 1, sizes: [3]}],
    postEffects: [{kind: "bloom", source: "specular"}]}});
  assert.equal(s.canvas.contextCalls.filter(c => c.kind === "webgl2").at(-1).options.antialias, false);
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
  assert.equal(s.mount.__gosxScene3DState.postEffects[0].source, "specular");
});

test("private FXAA uses the full canvas resolution above the post pixel default", async () => {
  const s = await mountPost({width: 2560, height: 1440, maxPixels: -1});
  s.mount.__gosxScene3DHandle.applyCommands([{kind: 7, data: {postEffects: []}}]);
  s.raf.flush(48); await H.flushAsyncWork(); s.raf.flush(64);
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
  const allocations = s.gl.textureSizes.filter(size => size[0] === 2560 && size[1] === 1440);
  assert.ok(allocations.length > 0, "private FXAA target must match the backing canvas");
});

test("fallback preparation does not copy the scene bundle on steady frames", () => {
  const h = H.createWebGLRendererForPost({fresh: true,
    props: {scene: {postEffects: [{kind: "bloom"}]}}});
  let enumerations = 0;
  const bundle = new Proxy(H.makeWebGLBundleWithCustomPost({kind: "bloom"}), {
    ownKeys(target) { enumerations++; return Reflect.ownKeys(target); },
  });
  try {
    h.renderer.render(bundle, {width: 320, height: 180});
    enumerations = 0;
    for (let frame = 0; frame < 30; frame++) {
      assert.notEqual(h.renderer.render(bundle, {width: 320, height: 180}), "pending");
    }
    assert.equal(enumerations, 0, "preparing private FXAA must not enumerate and copy the scene bundle");
    assert.equal(h.env.consoleLogs.error.length, 0);
  } finally {
    h.renderer.dispose();
  }
});
