"use strict";
// Shared setup for the Scene3D camera-command tests (not a test file).

const assert = require("node:assert/strict");

const {
  bootstrapSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const SET_CAMERA = 5;

async function mountScene(extraProps) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-camera-commands";
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: { engines: [{
      id: "gosx-engine-camera-commands",
      component: "GoSXScene3D",
      kind: "surface",
      mountId: mount.id,
      props: Object.assign({
        width: 640,
        height: 360,
        autoRotate: false,
        camera: { x: 0, y: 0.2, z: 6, fov: 72, near: 0.05, far: 512 },
        scene: { objects: [] },
      }, extraProps),
    }] },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  let mounted = env.context.__gosx.engines.get("gosx-engine-camera-commands");
  for (let attempt = 0; attempt < 5 && !mounted; attempt += 1) {
    await flushAsyncWork();
    mounted = env.context.__gosx.engines.get("gosx-engine-camera-commands");
  }
  assert.ok(mounted, "scene did not mount: " + JSON.stringify(env.consoleLogs.error));
  return { env, mount, mounted, canvas: mount.firstElementChild };
}

function drag(canvas, from, to) {
  const base = { button: 0, pointerId: 4, preventDefault() {}, stopPropagation() {} };
  canvas.dispatchEvent({ ...base, type: "pointerdown", clientX: from[0], clientY: from[1] });
  canvas.dispatchEvent({ ...base, type: "pointermove", buttons: 1, clientX: to[0], clientY: to[1] });
  canvas.dispatchEvent({ ...base, type: "pointerup", clientX: to[0], clientY: to[1] });
}

const distance = (camera, target) => Math.hypot(camera.x - target.x, camera.y - target.y, camera.z - target.z);

// forward is the camera's view direction for the runtime's Euler convention
// (rotationY turns about +y, rotationX pitches).
function forward(camera) {
  return {
    x: -Math.sin(camera.rotationY) * Math.cos(camera.rotationX),
    y: Math.sin(camera.rotationX),
    z: -Math.cos(camera.rotationY) * Math.cos(camera.rotationX),
  };
}

function aimError(camera, target) {
  const f = forward(camera);
  const d = distance(camera, target);
  const to = { x: (target.x - camera.x) / d, y: (target.y - camera.y) / d, z: (target.z - camera.z) / d };
  return Math.hypot(f.x - to.x, f.y - to.y, f.z - to.z);
}

module.exports = { SET_CAMERA, mountScene, drag, distance, aimError };
