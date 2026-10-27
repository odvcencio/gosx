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
  const { mounted, at, camera } = await mountEased();
  await mounted.handle.applyCommands([
    { kind: SET_CAMERA, data: { ...END, transitionMS: 1000 } },
    { kind: 0, objectId: "marker", data: { kind: "mesh", props: { geometry: "box", x: 1 } } },
  ]);
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
