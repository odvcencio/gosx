"use strict";
// Orbit target and limits after mount: a game moves between camera presets that
// look at different points and keeps mouse orbit.

const test = require("node:test");
const assert = require("node:assert/strict");

const { flushAsyncWork, bootstrapFeatureScene3DCommandSource, runScript } = require("./runtime-test-harness.js");
const { SET_CAMERA, mountScene, drag, distance, aimError } = require("./runtime-camera-test-helpers.js");

test("a camera with an orbit target is aimed at it and orbits around it", async () => {
  const { mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400 });
  const target = { x: 0, y: 20, z: -260 };
  mounted.handle.setCamera({ kind: "perspective", x: 0, y: 27, z: -37, fov: 68, near: 0.5, far: 20000, orbit: { target } });
  await flushAsyncWork();
  const before = mounted.handle.getCamera();
  assert.ok(aimError(before, target) < 1e-6, "not aimed at the new target: " + JSON.stringify(before));
  const radius = distance(before, target);
  drag(canvas, [320, 180], [420, 160]);
  await flushAsyncWork();
  const after = mounted.handle.getCamera();
  assert.ok(Math.abs(after.x - before.x) > 0.1 || Math.abs(after.z - before.z) > 0.1, "drag did not orbit");
  assert.ok(Math.abs(distance(after, target) - radius) < 1e-6, "orbit left the new target's sphere");
  assert.ok(aimError(after, target) < 1e-6, "orbit lost the new target");
  assert.equal(JSON.stringify(mounted.handle.getTelemetry().orbit.target), JSON.stringify(target));
});

test("orbit target survives a camera with no orbit and changes with the next one", async () => {
  const { mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400 });
  const a = { x: 0, y: 5, z: 0 }, b = { x: 40, y: 5, z: 40 };
  mounted.handle.setCamera({ kind: "perspective", x: 0, y: 5, z: 30, fov: 60, orbit: { target: a } });
  await flushAsyncWork();
  drag(canvas, [320, 180], [350, 180]);
  mounted.handle.setCamera({ kind: "perspective", x: 40, y: 15, z: 80, fov: 60, orbit: { target: b } });
  await flushAsyncWork();
  assert.ok(aimError(mounted.handle.getCamera(), b) < 1e-6);
  drag(canvas, [320, 180], [300, 190]);
  await flushAsyncWork();
  assert.ok(aimError(mounted.handle.getCamera(), b) < 1e-6, "second target lost after a drag");
});

test("distance and pitch limits change after mount", async () => {
  const { mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400 });
  const target = { x: 0, y: 0, z: 0 };
  mounted.handle.setCamera({ kind: "perspective", x: 0, y: 0, z: 30, fov: 60,
    orbit: { target, minDistance: 20, maxDistance: 40, pitchLimit: 0.3 } });
  await flushAsyncWork();
  for (const deltaY of [-100000, 100000]) {
    canvas.dispatchEvent({ type: "wheel", deltaY, preventDefault() {}, stopPropagation() {} });
    await flushAsyncWork();
    const d = distance(mounted.handle.getCamera(), target);
    assert.ok(d >= 19.999 && d <= 40.001, "distance " + d + " left [20, 40]");
  }
  drag(canvas, [320, 180], [320, 600]);
  drag(canvas, [320, 180], [320, 600]);
  await flushAsyncWork();
  const camera = mounted.handle.getCamera();
  assert.ok(Math.abs(Math.asin((camera.y - target.y) / distance(camera, target))) <= 0.3 + 1e-6, "pitch left its limit");
});

test("an orbit-only camera command keeps the pose and changes the target", async () => {
  const { mounted } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0, z: 0 } });
  const before = mounted.handle.getCamera();
  await mounted.handle.applyCommands([{ kind: SET_CAMERA, data: Object.assign({}, before, { orbit: { target: { x: 5, y: 0, z: 0 } } }) }]);
  await flushAsyncWork();
  assert.equal(JSON.stringify(mounted.handle.getTelemetry().orbit.target), JSON.stringify({ x: 5, y: 0, z: 0 }));
});

test("setOrbit keeps the camera position and turns it to the new target", async () => {
  const { env, mount, mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400 });
  const before = mounted.handle.getCamera();
  const target = { x: 10, y: 3, z: -20 };
  mounted.handle.setOrbit({ target });
  await flushAsyncWork();
  const after = mounted.handle.getCamera();
  assert.ok(Math.abs(after.x - before.x) < 1e-9 && Math.abs(after.y - before.y) < 1e-9 && Math.abs(after.z - before.z) < 1e-9);
  assert.ok(aimError(after, target) < 1e-6);
  drag(canvas, [320, 180], [380, 150]);
  await flushAsyncWork();
  assert.ok(aimError(mounted.handle.getCamera(), target) < 1e-6);
  // The page-side bridge a Go engine calls reaches the same handle.
  runScript(bootstrapFeatureScene3DCommandSource, env.context, "bootstrap-feature-scene3d-command.js");
  await env.context.__gosx.scene3d.setOrbit(mount, { target: { x: 0, y: 0, z: 0 } });
  await flushAsyncWork();
  assert.ok(aimError(mounted.handle.getCamera(), { x: 0, y: 0, z: 0 }) < 1e-6);
});

test("a dispatched camera command carries the orbit target like setCamera", async () => {
  const camera = { kind: "perspective", x: 9, y: 4, z: 11, rotationX: -0.1, rotationY: 0.4, rotationZ: 0, fov: 55, near: 0.1, far: 300, zoom: 1,
    orbit: { target: { x: 1, y: 2, z: 3 }, minDistance: 2, maxDistance: 50 } };
  const props = { controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } };
  const viaHandle = await mountScene(props);
  viaHandle.mounted.handle.setCamera(camera);
  const viaCommand = await mountScene(props);
  await viaCommand.mounted.handle.applyCommands([{ kind: SET_CAMERA, data: camera }]);
  await flushAsyncWork();
  assert.equal(JSON.stringify(viaCommand.mounted.handle.getCamera()), JSON.stringify(viaHandle.mounted.handle.getCamera()));
  assert.equal(JSON.stringify(viaCommand.mounted.handle.getTelemetry().orbit), JSON.stringify(viaHandle.mounted.handle.getTelemetry().orbit));
  assert.ok(aimError(viaCommand.mounted.handle.getCamera(), { x: 1, y: 2, z: 3 }) < 1e-6);
});

test("a zero limit in an orbit update leaves that limit as it is", async () => {
  const { mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400 });
  const target = { x: 0, y: 0, z: 0 };
  mounted.handle.setOrbit({ target, minDistance: 20, maxDistance: 40, pitchLimit: 0.3 });
  mounted.handle.setCamera({ kind: "perspective", x: 0, y: 0, z: 30, fov: 60, orbit: { target, minDistance: 0, maxDistance: 0, pitchLimit: 0 } });
  await flushAsyncWork();
  for (const deltaY of [-100000, 100000]) {
    canvas.dispatchEvent({ type: "wheel", deltaY, preventDefault() {}, stopPropagation() {} });
    await flushAsyncWork();
    const d = distance(mounted.handle.getCamera(), target);
    assert.ok(d >= 19.999 && d <= 40.001, "distance " + d + " left [20, 40] after a zero limit update");
  }
  drag(canvas, [320, 180], [320, 600]);
  drag(canvas, [320, 180], [320, 600]);
  await flushAsyncWork();
  const camera = mounted.handle.getCamera();
  assert.ok(Math.abs(Math.asin((camera.y - target.y) / distance(camera, target))) <= 0.3 + 1e-6, "pitch limit was reset by a zero");
});
