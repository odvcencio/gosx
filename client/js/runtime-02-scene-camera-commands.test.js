"use strict";
// Scene3D camera commands against built-in orbit controls: a dispatched
// SetCamera command must move the camera exactly like handle.setCamera.

const test = require("node:test");
const assert = require("node:assert/strict");

const { flushAsyncWork, runScript, bootstrapFeatureScene3DCommandSource } = require("./runtime-test-harness.js");
const { SET_CAMERA, mountScene, drag } = require("./runtime-camera-test-helpers.js");

for (const controls of ["", "orbit"]) {
  test(`dispatched SetCamera command moves the camera (controls ${controls || "none"})`, async () => {
    const { mounted, canvas } = await mountScene(controls ? { controls, controlTarget: { x: 0, y: 0.2, z: 0 } } : {});
    assert.ok(canvas);
    if (controls) drag(canvas, [320, 180], [380, 150]); // user has orbited: controls own the camera now
    await flushAsyncWork();
    const camera = { kind: "perspective", x: 30, y: 12, z: 40, rotationX: -0.2, rotationY: 0.6, rotationZ: 0, fov: 50, near: 0.5, far: 400, zoom: 1 };
    await mounted.handle.applyCommands([{ kind: SET_CAMERA, data: camera }]);
    await flushAsyncWork();
    const got = mounted.handle.getCamera();
    assert.ok(Math.abs(got.x - 30) < 0.001 && Math.abs(got.y - 12) < 0.001 && Math.abs(got.z - 40) < 0.001,
      "camera did not move: " + JSON.stringify(got));
    assert.equal(Math.round(got.fov), 50);
  });
}

test("a dispatched SetCamera command and handle.setCamera leave the same camera", async () => {
  const camera = { kind: "perspective", x: 9, y: 4, z: 11, rotationX: -0.1, rotationY: 0.4, rotationZ: 0, fov: 55, near: 0.1, far: 300, zoom: 1 };
  const props = { controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } };
  const viaHandle = await mountScene(props);
  viaHandle.mounted.handle.setCamera(camera);
  await flushAsyncWork();
  const viaCommand = await mountScene(props);
  await viaCommand.mounted.handle.applyCommands([{ kind: SET_CAMERA, data: camera }]);
  await flushAsyncWork();
  assert.equal(JSON.stringify(viaCommand.mounted.handle.getCamera()), JSON.stringify(viaHandle.mounted.handle.getCamera()));
  assert.equal(JSON.stringify(viaCommand.mounted.handle.getTelemetry().orbit), JSON.stringify(viaHandle.mounted.handle.getTelemetry().orbit));
});


test("the public dispatchCommands bridge a Go engine calls moves an orbiting camera", async () => {
  const { env, mount, mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } });
  runScript(bootstrapFeatureScene3DCommandSource, env.context, "bootstrap-feature-scene3d-command.js");
  drag(canvas, [320, 180], [380, 150]);
  await flushAsyncWork();
  const camera = { kind: "perspective", x: -20, y: 9, z: 25, rotationX: -0.2, rotationY: -0.6, rotationZ: 0, fov: 45, near: 0.5, far: 400, zoom: 1 };
  await env.context.__gosx.scene3d.dispatchCommands(mount, [{ kind: SET_CAMERA, data: camera }]);
  await flushAsyncWork();
  const got = mounted.handle.getCamera();
  assert.ok(Math.abs(got.x + 20) < 0.001 && Math.abs(got.y - 9) < 0.001 && Math.abs(got.z - 25) < 0.001, JSON.stringify(got));
});
