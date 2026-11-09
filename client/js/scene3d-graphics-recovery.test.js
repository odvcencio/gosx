"use strict";
// Graphics resets: the mount rebuilds its renderer after a WebGL context loss
// or a WebGPU device loss, and reports each transition on the mount so a game
// can pause and resume (scene.Surface.OnGraphicsReset).
const test = require("node:test");
const assert = require("node:assert/strict");
const H = require("./runtime-test-harness.js");

function recoveryAPI() {
  return H.createAdaptiveQualityHarness().api;
}

function recordingMount() {
  const events = [];
  return { events, dispatchEvent(event) { events.push({ type: event.type, ...event.detail }); return true; } };
}

test("a WebGPU device loss may return to WebGPU twice per five minutes", () => {
  const api = recoveryAPI();
  const recovery = api.createSceneGraphicsRecovery();
  assert.equal(api.sceneWebGPULossReturnAllowed(recovery, 1000), true);
  assert.equal(api.sceneWebGPULossReturnAllowed(recovery, 2000), true);
  assert.equal(api.sceneWebGPULossReturnAllowed(recovery, 3000), false, "a third loss in the window stays on WebGL");
  assert.equal(api.sceneWebGPULossReturnAllowed(recovery, 1000 + 300000), true, "the window slides");
  assert.equal(api.sceneWebGPULossReturnAllowed(api.createSceneGraphicsRecovery(), 0, "destroyed"), false, "a page's own device.destroy() stays on WebGL");
  assert.equal(api.sceneWebGPULossReturnAllowed(api.createSceneGraphicsRecovery(), 0, "unknown"), true);
});

test("reset events pair each loss with the renderer that draws next", () => {
  const api = recoveryAPI();
  const recovery = api.createSceneGraphicsRecovery();
  const mount = recordingMount();
  api.sceneGraphicsResetSwap(mount, recovery, "webgpu", "webgpu-probe-recovered");
  assert.equal(mount.events.length, 0, "a first WebGPU arrival without a loss is not a reset");
  api.sceneDispatchGraphicsReset(mount, recovery, "lost", "webgpu", "webgpu-device-lost");
  api.sceneGraphicsResetSwap(mount, recovery, "canvas", "webgpu-device-lost");
  assert.equal(mount.events.length, 1, "a Canvas2D stand-in is not a restore");
  api.sceneGraphicsResetSwap(mount, recovery, "webgl", "webgpu-device-lost");
  api.sceneGraphicsResetSwap(mount, recovery, "webgl", "renderer-swap");
  api.sceneGraphicsResetSwap(mount, recovery, "webgpu", "webgpu-probe-recovered");
  assert.deepEqual(mount.events.map((e) => [e.type, e.phase, e.backend, e.reason, e.count]), [
    ["gosx:scene3d:graphics-reset", "lost", "webgpu", "webgpu-device-lost", 1],
    ["gosx:scene3d:graphics-reset", "restored", "webgl", "webgpu-device-lost", 1],
    ["gosx:scene3d:graphics-reset", "restored", "webgpu", "webgpu-probe-recovered", 1],
  ]);
});

test("a renderer swap resets adaptive sampling so recovery is not judged as load", () => {
  const h = H.createAdaptiveQualityHarness({ qualityTier: "full" });
  for (let i = 0; i < 30; i++) h.sample(40, 1000 / 60);
  assert.ok(h.state.badFrames > 0);
  h.api.sceneAdaptiveResumeSampling(h.state);
  assert.equal(h.state.badFrames, 0);
  assert.equal(h.state.sampleWindow.length, 0);
  assert.equal(h.state.resumePending, true);
});

test("a mounted scene reports WebGL context loss and restore on the mount", async () => {
  const mount = new H.FakeElement("div", null);
  mount.id = "scene-graphics-reset";
  const env = H.createContext({
    elements: [mount], enableWebGL: true,
    manifest: { engines: [{ id: "graphics-reset-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: { width: 320, height: 200, autoRotate: false,
        scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] } } }] },
  });
  const resets = [];
  mount.addEventListener("gosx:scene3d:graphics-reset", (event) => resets.push(event.detail));
  H.runScript(H.bootstrapSource, env.context, "bootstrap.js");
  await H.flushAsyncWork();
  const canvas = mount.children[0];
  canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
  await H.flushAsyncWork();
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  await H.flushAsyncWork();
  assert.deepEqual(resets.map((r) => [r.phase, r.backend, r.count]), [["lost", "webgl", 1], ["restored", "webgl", 1]]);
});
