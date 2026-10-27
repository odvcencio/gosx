"use strict";
// TransitionMS on a Scene3D camera: the page eases to the new pose over that
// many milliseconds instead of cutting, so a Go caller makes one call per move
// rather than one per frame. The mounted runtime runs here, on a clock the test
// owns.

const test = require("node:test");
const assert = require("node:assert/strict");

const { flushAsyncWork, runScript, bootstrapFeatureScene3DCommandSource } = require("./runtime-test-harness.js");
const { SET_CAMERA, mountScene, drag } = require("./runtime-camera-test-helpers.js");

const START = { kind: "perspective", x: 0, y: 0.2, z: 6, rotationX: -0.1, rotationY: 0, rotationZ: 0, fov: 72, near: 0.05, far: 512, zoom: 1 };
const END = { kind: "perspective", x: 30, y: 12, z: 40, rotationX: -0.4, rotationY: 0.8, rotationZ: 0.2, fov: 50, near: 0.5, far: 400, zoom: 1 };
const FIELDS = ["x", "y", "z", "rotationX", "rotationY", "rotationZ", "fov", "near", "far"];

// scene.CameraTransition.Sample's pose for START -> END over 1000 ms (cubic-bezier
// .4, 0, .2, 1), printed by Go and pinned in scene/camera_state_test.go
// (TestCameraTransitionSampleGolden). The page and a Go loop that steps
// SetCamera must produce the same move.
const GO_SAMPLES = {
  250: { x: 7.097620814033, y: 2.99173085352, z: 14.043970255905, rotationX: -0.17097620814, rotationY: 0.189269888374, rotationZ: 0.047317472094, fov: 66.795078069709, near: 0.156464312211, far: 485.502215627608 },
  500: { x: 23.266839333486, y: 9.351623471171, z: 32.369084577951, rotationX: -0.332668393335, rotationY: 0.620449048893, rotationZ: 0.155112262223, fov: 54.937651155444, near: 0.399002590002, far: 425.137133154986 },
  750: { x: 28.781032102181, y: 11.520539293525, z: 38.618503049139, rotationX: -0.387810321022, rotationY: 0.767494189392, rotationZ: 0.191873547348, fov: 50.893909791734, near: 0.481715481533, far: 404.550813485190 },
};

// mountEased mounts a scene on a manual clock. at(ms) moves the clock to ms
// after the mount and lets the page run its animation frames.
async function mountEased(props, envOptions) {
  const clock = { now: 5000 };
  const origin = clock.now;
  const scene = await mountScene(Object.assign({ camera: START }, props), Object.assign({ performanceNow: () => clock.now }, envOptions));
  const at = async (ms) => { clock.now = origin + ms; await flushAsyncWork(); };
  const camera = () => scene.mounted.handle.getCamera();
  return Object.assign(scene, { clock, at, camera });
}

function assertPose(got, want, label, tolerance = 1e-5) {
  for (const field of FIELDS) {
    assert.ok(Math.abs(got[field] - want[field]) <= tolerance,
      `${label}: ${field} is ${got[field]}, want ${want[field]} (camera ${JSON.stringify(got)})`);
  }
}

// Under orbit controls the camera always faces the orbit target, so a test that
// moves the target compares where the camera is, not where it looks.
function assertPosition(got, want, label, tolerance) {
  for (const field of ["x", "y", "z"]) {
    assert.ok(Math.abs(got[field] - want[field]) <= tolerance,
      `${label}: ${field} is ${got[field]}, want ${want[field]} (camera ${JSON.stringify(got)})`);
  }
}

test("handle.setCamera with TransitionMS eases to the pose instead of cutting", async () => {
  const { mounted, at, camera } = await mountEased();
  assert.equal(mounted.handle.setCamera({ ...END, transitionMS: 1000 }), true);
  await at(0);
  assertPose(camera(), START, "the move starts where the camera is");
  for (const ms of [250, 500, 750]) {
    await at(ms);
    assertPose(camera(), GO_SAMPLES[ms], `${ms} ms into the move`);
  }
  await at(1000);
  assertPose(camera(), END, "the move ends exactly on the pose", 1e-9);
  await at(5000);
  assertPose(camera(), END, "nothing moves the camera after the move", 1e-9);
});

test("a dispatched SetCamera command with TransitionMS eases the same way", async () => {
  const { mounted, at, camera } = await mountEased();
  await mounted.handle.applyCommands([{ kind: SET_CAMERA, data: { ...END, portraitFOV: 0, left: 0, right: 0, top: 0, bottom: 0, transitionMS: 1000 } }]);
  await at(0);
  assertPose(camera(), START, "the command does not cut");
  await at(500);
  assertPose(camera(), GO_SAMPLES[500], "500 ms into the move");
  await at(1000);
  assertPose(camera(), END, "the move ends exactly on the pose", 1e-9);
});

test("a camera command in a batch with other commands still eases, and the rest applies at once", async () => {
  const { mount, mounted, at, camera } = await mountEased();
  await mounted.handle.applyCommands([
    { kind: SET_CAMERA, data: { ...END, transitionMS: 1000 } },
    { kind: 0, objectId: "marker", data: { kind: "mesh", props: { geometry: "box", x: 1 } } },
  ]);
  assert.ok(mount.__gosxScene3DState.objects.has("marker"), "the rest of the batch applies before the move has run");
  assertPose(camera(), START, "the camera command did not cut");
  await at(500);
  assertPose(camera(), GO_SAMPLES[500], "500 ms into the move");
});

test("a camera without TransitionMS still cuts", async () => {
  const { mounted, at, camera } = await mountEased();
  mounted.handle.setCamera(END);
  await at(0);
  assertPose(camera(), END, "cut by setCamera", 1e-9);
  const other = { ...START, transitionMS: 0 };
  await mounted.handle.applyCommands([{ kind: SET_CAMERA, data: other }]);
  await at(0);
  assertPose(camera(), START, "cut by a command", 1e-9);
});

test("a new camera replaces a move from the pose on screen", async () => {
  const { mounted, at, camera } = await mountEased();
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(500);
  const midway = camera();
  assertPose(midway, GO_SAMPLES[500], "500 ms into the first move");
  const back = { ...START, x: -20, y: 3 };
  mounted.handle.setCamera({ ...back, transitionMS: 400 });
  await at(500);
  assertPose(camera(), midway, "the replacement starts on the pose on screen, not on a target", 1e-6);
  await at(900);
  assertPose(camera(), back, "the replacement ends on its own pose", 1e-9);
  await at(2500);
  assertPose(camera(), back, "the first move is gone", 1e-9);
});

test("a camera without TransitionMS ends a move in progress", async () => {
  const { mounted, at, camera } = await mountEased();
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(300);
  const cut = { ...START, x: -9, z: 12 };
  mounted.handle.setCamera(cut);
  await at(300);
  assertPose(camera(), cut, "the cut takes effect at once", 1e-9);
  await at(2000);
  assertPose(camera(), cut, "the move does not pull the camera back", 1e-9);
});

test("a camera command or a Live camera event without TransitionMS also ends a move", async () => {
  const { mounted, at, camera } = await mountEased();
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(300);
  const cut = { ...START, x: -9, z: 12 };
  await mounted.handle.applyCommands([{ kind: SET_CAMERA, data: cut }]);
  await at(300);
  assertPose(camera(), cut, "a camera command cuts at once", 1e-9);
  await at(2000);
  assertPose(camera(), cut, "the command's cut is not undone by the old move", 1e-9);

  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(2300);
  const live = { ...START, x: 14, y: 5 };
  await mounted.handle.applyLive("camera", { camera: live });
  await at(2300);
  assertPose(camera(), live, "a Live camera event cuts at once", 1e-9);
  await at(4000);
  assertPose(camera(), live, "the Live cut is not undone by the old move", 1e-9);
});

test("asking again for the pose a move is heading to does not restart it", async () => {
  const { mounted, at, camera } = await mountEased();
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(600);
  assert.equal(mounted.handle.setCamera({ ...END, transitionMS: 1000 }), false);
  await at(1000);
  assertPose(camera(), END, "the move keeps its first start time", 1e-9);
});

test("reduced motion cuts instead of easing", async () => {
  const { mounted, at, camera } = await mountEased({}, { prefersReducedMotion: true });
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(0);
  assertPose(camera(), END, "a viewer who asked for less motion gets the pose", 1e-9);
});

test("a change of camera kind cuts, as scene.CameraTransition does", async () => {
  const { mounted, at, camera } = await mountEased();
  mounted.handle.setCamera({ kind: "orthographic", x: 0, y: 10, z: 0, rotationX: -1.5, left: -4, right: 4, top: 3, bottom: -3, zoom: 2, near: 0.1, far: 100, transitionMS: 1000 });
  await at(0);
  assert.equal(camera().kind, "orthographic");
  assert.equal(camera().zoom, 2);
});

test("a move under orbit controls carries the orbit with it, after the player has orbited", async () => {
  const { mounted, canvas, at, camera } = await mountEased({ controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } });
  drag(canvas, [320, 180], [380, 150]); // the controls own the camera now
  await at(0);
  const dragged = camera();
  assert.ok(Math.abs(dragged.x - START.x) > 0.1, "the drag moved the orbit camera: " + JSON.stringify(dragged));
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(0);
  const first = camera();
  assert.ok(Math.abs(first.x - dragged.x) < 1e-3 && Math.abs(first.z - dragged.z) < 1e-3, "the move starts on the dragged pose: " + JSON.stringify(first));
  await at(500);
  const halfway = camera();
  assert.ok(halfway.x > Math.min(dragged.x, 20) && halfway.x < 29, "the orbit camera moved part of the way: " + JSON.stringify(halfway));
  assert.ok(halfway.z > Math.min(dragged.z, 20) && halfway.z < 39, "the orbit camera moved part of the way: " + JSON.stringify(halfway));
  await at(1000);
  const last = camera();
  assert.ok(Math.abs(last.x - END.x) < 1e-3 && Math.abs(last.y - END.y) < 1e-3 && Math.abs(last.z - END.z) < 1e-3, JSON.stringify(last));
});

test("the public setCamera a Go engine calls returns at once and the move runs in the page", async () => {
  const { env, mount, at, camera } = await mountEased();
  runScript(bootstrapFeatureScene3DCommandSource, env.context, "bootstrap-feature-scene3d-command.js");
  // The clock does not move, so a call that waited for the move would never return.
  const result = await Promise.race([
    env.context.__gosx.scene3d.setCamera(mount, { ...END, transitionMS: 1000 }),
    new Promise((resolve) => setTimeout(() => resolve("waited"), 500)),
  ]);
  assert.equal(result, true);
  await at(500);
  assertPose(camera(), GO_SAMPLES[500], "500 ms into the move");
  await at(1000);
  assertPose(camera(), END, "the move ends exactly on the pose", 1e-9);
});

// The orbit target the controls are using now, as a plain object of this realm
// (the page runs in its own vm context, so deepEqual would see another prototype).
const orbitTarget = (mounted) => {
  const { x, y, z } = mounted.handle.getTelemetry().orbit.target;
  return { x, y, z };
};

test("setOrbit during a move changes the orbit and lets the move finish", async () => {
  const { mounted, at, camera } = await mountEased({ controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } });
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(300);
  const midway = camera();
  assert.equal(mounted.handle.setOrbit({ target: { x: 2, y: 3, z: 4 } }), true);
  assert.deepEqual(orbitTarget(mounted), { x: 2, y: 3, z: 4 }, "the orbit target moved at once");
  assertPosition(camera(), midway, "setOrbit keeps the camera where it is", 1e-3);
  await at(1000);
  assertPosition(camera(), END, "the move still ends on its pose", 1e-6);
  assert.deepEqual(orbitTarget(mounted), { x: 2, y: 3, z: 4 }, "the move does not undo the new target");
});

test("setOrbit without orbit controls leaves a move running", async () => {
  const { mounted, at, camera } = await mountEased();
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(300);
  mounted.handle.setOrbit({ maxDistance: 500 });
  await at(500);
  assertPose(camera(), GO_SAMPLES[500], "500 ms into the move");
  await at(1000);
  assertPose(camera(), END, "the move ends on its pose", 1e-9);
});

test("asking again for the pose a move is heading to still applies the new orbit", async () => {
  const { mounted, at, camera } = await mountEased({ controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } });
  mounted.handle.setCamera({ ...END, transitionMS: 1000, orbit: { target: { x: 0, y: 0, z: 0 } } });
  await at(200);
  assert.deepEqual(orbitTarget(mounted), { x: 0, y: 0, z: 0 });
  assert.equal(mounted.handle.setCamera({ ...END, transitionMS: 1000, orbit: { target: { x: 5, y: 5, z: 5 } } }), true, "the orbit changed");
  assert.deepEqual(orbitTarget(mounted), { x: 5, y: 5, z: 5 }, "the orbit target is the second call's");
  await at(1000);
  assertPosition(camera(), END, "the move keeps its first start time", 1e-6);
});

test("a player's drag takes the camera from a move in progress", async () => {
  const { mounted, canvas, at, camera } = await mountEased({ controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } });
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(450);
  const before = camera();
  drag(canvas, [320, 180], [380, 150]);
  const dragged = camera();
  assert.ok(Math.hypot(dragged.x - before.x, dragged.y - before.y, dragged.z - before.z) > 1, "the drag moves the camera: " + JSON.stringify(dragged));
  await at(750);
  const move = GO_SAMPLES[750];
  assert.ok(Math.hypot(camera().x - move.x, camera().y - move.y, camera().z - move.z) > 1, "the move no longer pulls the camera: " + JSON.stringify(camera()));
  await at(2000);
  assert.ok(Math.hypot(camera().x - END.x, camera().y - END.y, camera().z - END.z) > 1, "the camera does not finish the move: " + JSON.stringify(camera()));
});

test("a player's wheel zoom takes the camera from a move in progress", async () => {
  const { mounted, canvas, at, camera } = await mountEased({ controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } });
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(450);
  canvas.dispatchEvent({ type: "wheel", deltaY: -400, preventDefault() {}, stopPropagation() {} });
  await at(750);
  const move = GO_SAMPLES[750];
  assert.ok(Math.hypot(camera().x - move.x, camera().y - move.y, camera().z - move.z) > 1, "the move no longer pulls the camera: " + JSON.stringify(camera()));
  await at(2000);
  assert.ok(Math.hypot(camera().x - END.x, camera().y - END.y, camera().z - END.z) > 1, "the camera does not finish the move: " + JSON.stringify(camera()));
});

test("a player's arrow key takes the camera from a move in progress", async () => {
  const { env, mounted, canvas, at, camera } = await mountEased({ controls: "orbit", controlTarget: { x: 0, y: 0.2, z: 0 } });
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(450);
  canvas.focus();
  env.document.dispatchEvent({ type: "keydown", code: "ArrowLeft", preventDefault() {} });
  await at(750);
  const move = GO_SAMPLES[750];
  assert.ok(Math.hypot(camera().x - move.x, camera().y - move.y, camera().z - move.z) > 1, "the move no longer pulls the camera: " + JSON.stringify(camera()));
  await at(2000);
  assert.ok(Math.hypot(camera().x - END.x, camera().y - END.y, camera().z - END.z) > 1, "the camera does not finish the move: " + JSON.stringify(camera()));
});

test("a move started from CameraInputSignal eases and leaves CameraOutputSignal alone", async () => {
  const { env, at, camera } = await mountEased({ cameraInputSignal: "$camin", cameraOutputSignal: "$camout" });
  const published = [];
  env.context.__gosx_subscribe_shared_signal("$camout", (value) => published.push(value), { immediate: false });
  await at(0);
  published.length = 0; // the mount's own first publish is not under test
  env.context.__gosx_notify_shared_signal("$camin", JSON.stringify({ camera: { ...END, transitionMS: 1000 } }));
  await at(0);
  assertPose(camera(), START, "the signal starts a move and does not cut");
  await at(500);
  assertPose(camera(), GO_SAMPLES[500], "500 ms into the move");
  await at(1000);
  assertPose(camera(), END, "the move ends exactly on the pose", 1e-9);
  await at(2000);
  assert.deepEqual(published, [], "the output signal is suppressed while the input signal drives the camera, move steps included");
});

test("one signal bound as input and output does not cancel a move its own input started", async () => {
  const { env, at, camera } = await mountEased({ cameraInputSignal: "$cam", cameraOutputSignal: "$cam" });
  await at(0);
  env.context.__gosx_notify_shared_signal("$cam", JSON.stringify({ camera: { ...END, transitionMS: 1000 } }));
  await at(500);
  assertPose(camera(), GO_SAMPLES[500], "500 ms into the move");
  await at(1000);
  assertPose(camera(), END, "the move ends exactly on the pose", 1e-9);
});

test("a move started by handle.setCamera publishes the camera as it moves", async () => {
  const { env, mounted, at } = await mountEased({ cameraOutputSignal: "$camout" });
  const published = [];
  env.context.__gosx_subscribe_shared_signal("$camout", (value) => published.push(value), { immediate: false });
  await at(0);
  published.length = 0;
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  for (const ms of [100, 300, 500, 700, 1000]) await at(ms);
  assert.ok(published.length >= 3, "the output signal follows a move the page started, as it follows a drag: " + published.length);
  assertPose(published.at(-1), END, "the last published camera is the pose the move ended on", 1e-9);
});

test("reduced motion turned on during a move completes it at once", async () => {
  const { env, mounted, at, camera } = await mountEased();
  mounted.handle.setCamera({ ...END, transitionMS: 1000 });
  await at(300);
  assert.ok(Math.abs(camera().x - END.x) > 5, "the move is still running");
  env.context.matchMedia("(prefers-reduced-motion: reduce)").dispatch(true);
  await at(300);
  assertPose(camera(), END, "a viewer who asks for less motion gets the pose now", 1e-9);
});
