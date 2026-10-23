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

const elevation = (camera, target) => Math.asin((camera.y - target.y) / distance(camera, target));
async function dragDown(mounted, canvas, times = 4) { // the camera goes down when the pointer moves up
  for (let i = 0; i < times; i++) { drag(canvas, [320, 700], [320, 100]); await flushAsyncWork(); }
  return mounted.handle.getCamera();
}
async function dragUp(mounted, canvas, times = 4) {
  for (let i = 0; i < times; i++) { drag(canvas, [320, 100], [320, 700]); await flushAsyncWork(); }
  return mounted.handle.getCamera();
}

test("MinPitch and MaxPitch bound the orbit separately, and zero is a valid MinPitch", async () => {
  const { mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400, controlPitchLimit: 1.4 });
  const target = { x: 0, y: 5, z: 0 };
  mounted.handle.setCamera({ kind: "perspective", x: 0, y: 25, z: 60, fov: 55, orbit: { target, minPitch: 0, maxPitch: 0.9 } });
  await flushAsyncWork();
  const low = await dragDown(mounted, canvas);
  assert.ok(elevation(low, target) >= -1e-6, "camera went below the target's horizon: " + elevation(low, target));
  assert.ok(elevation(low, target) < 0.05, "the drag did not reach MinPitch 0: " + elevation(low, target));
  const high = await dragUp(mounted, canvas);
  assert.ok(Math.abs(elevation(high, target) - 0.9) < 1e-6, "MaxPitch not reached or passed: " + elevation(high, target));
});

test("PitchLimit stays the symmetric shorthand when MinPitch and MaxPitch are absent", async () => {
  const { mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400 });
  const target = { x: 0, y: 5, z: 0 };
  mounted.handle.setCamera({ kind: "perspective", x: 0, y: 25, z: 60, fov: 55, orbit: { target, pitchLimit: 0.5 } });
  await flushAsyncWork();
  assert.ok(Math.abs(elevation(await dragDown(mounted, canvas), target) + 0.5) < 1e-6);
  assert.ok(Math.abs(elevation(await dragUp(mounted, canvas), target) - 0.5) < 1e-6);
});

test("MinHeight keeps the camera above a floor at any radius", async () => {
  const { mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 0, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400 });
  const target = { x: 0, y: 3, z: 0 };
  mounted.handle.setCamera({ kind: "perspective", x: 0, y: 30, z: 100, fov: 55, orbit: { target, minHeight: 2 } });
  await flushAsyncWork();
  let camera = await dragDown(mounted, canvas);
  assert.ok(camera.y >= 2 - 1e-6, "camera under the floor: y " + camera.y);
  assert.ok(camera.y < 2.5, "the drag did not reach the floor: y " + camera.y);
  assert.ok(Math.abs(distance(camera, target) - distance({ x: 0, y: 30, z: 100 }, target)) < 1e-6, "the floor changed the radius");
  for (const deltaY of [-300, 300, 800]) {
    canvas.dispatchEvent({ type: "wheel", deltaY, preventDefault() {}, stopPropagation() {} });
    await flushAsyncWork();
    camera = mounted.handle.getCamera();
    assert.ok(camera.y >= 2 - 1e-6, "zoom put the camera under the floor: y " + camera.y);
  }
  // A camera command that places the camera under the floor is lifted onto it.
  mounted.handle.setCamera({ kind: "perspective", x: 0, y: -50, z: 100, fov: 55, orbit: { target, minHeight: 2 } });
  await flushAsyncWork();
  assert.ok(mounted.handle.getCamera().y >= 2 - 1e-6);
});

test("a preset camera with an orbit lands exactly on its pose after a flick, and stays there", async () => {
  const { mounted, canvas } = await mountScene({ controls: "orbit", controlTarget: { x: 0, y: 3, z: 0 }, controlMinDistance: 1, controlMaxDistance: 400 });
  const preset = { kind: "perspective", x: 0, y: 27.4, z: -37.6, rotationX: -0.033, rotationY: 0, fov: 68, near: 0.5, far: 20000, orbit: { target: { x: 0, y: 20, z: -260 } } };
  mounted.handle.setCamera(preset);
  await flushAsyncWork();
  // A fast drag, released with momentum, then the preset at once.
  const base = { button: 0, pointerId: 6, preventDefault() {}, stopPropagation() {} };
  canvas.dispatchEvent({ ...base, type: "pointerdown", clientX: 320, clientY: 180, timeStamp: 1000 });
  canvas.dispatchEvent({ ...base, type: "pointermove", buttons: 1, clientX: 420, clientY: 190, timeStamp: 1008 });
  canvas.dispatchEvent({ ...base, type: "pointermove", buttons: 1, clientX: 560, clientY: 200, timeStamp: 1016 });
  canvas.dispatchEvent({ ...base, type: "pointerup", clientX: 560, clientY: 200, timeStamp: 1017 });
  mounted.handle.setCamera(Object.assign({}, preset, { x: 54, y: 4.8, z: 66, rotationY: 0.555 }, { orbit: { target: { x: -3, y: 6, z: -26 } } }));
  mounted.handle.setCamera(preset);
  for (let i = 0; i < 6; i++) await flushAsyncWork();
  const c = mounted.handle.getCamera();
  assert.ok(Math.abs(c.x - preset.x) < 1e-9 && Math.abs(c.y - preset.y) < 1e-9 && Math.abs(c.z - preset.z) < 1e-9, "not on the pose: " + JSON.stringify(c));
  assert.ok(aimError(c, preset.orbit.target) < 1e-9, "residual yaw after a flick: " + c.rotationY);
  assert.equal(JSON.stringify(mounted.handle.getCamera()), JSON.stringify(c), "the camera kept moving: drag momentum survived the camera command");
});
