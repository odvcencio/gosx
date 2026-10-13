"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const H = require("./runtime-test-harness.js");

async function mountPost(extra = {}) {
  const mount = new H.FakeElement("div", null);
  mount.id = "post-canvas-msaa";
  const env = H.createContext({elements: [mount], enableWebGL2: true, disableCanvas2D: true,
    manifest: {engines: [{id: "post-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: {width: 640, height: 360, adaptiveQuality: false,
        scene: {objects: [{kind: "box"}], postEffects: [{kind: "bloom"}, {kind: "toneMapping", mode: "aces"}]}, ...extra}}]}});
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
  assert.equal(s.env.consoleLogs.error.length, 0);
});
