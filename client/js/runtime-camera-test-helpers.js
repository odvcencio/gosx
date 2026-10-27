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

// envOptions reach createContext, for a test that owns the clock
// (performanceNow) or the reduced-motion preference (prefersReducedMotion).
// prepare(env) runs after the runtime script and before the mount starts, for
// a test that loads a lazy chunk (the zoom chunk, which a controlZoom scene
// needs) the way a page that has already fetched it would.
async function mountScene(extraProps, envOptions, prepare) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-camera-commands";
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    ...envOptions,
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
  if (prepare) prepare(env);
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

// browserEvents makes FakeElement.dispatchEvent follow the DOM for the events a
// test sends: capture listeners run from the root down to the target, the
// target's capture listeners run before its others, stopImmediatePropagation
// ends the dispatch, and an event that bubbles goes back up through the parents.
// The harness calls only the target's listeners, in registration order, and
// honors none of this, which hides a capture-phase handler that stops an event
// before the handler a test expects to see it (ControlZoom's wheel and key
// handlers do this). A page's CustomEvent keeps its bubbles flag. The returned
// function undoes both changes.
function browserEvents(env) {
  const original = FakeElement.prototype.dispatchEvent;
  const BaseCustomEvent = env.context.CustomEvent;
  env.context.CustomEvent = class CustomEvent extends BaseCustomEvent {
    constructor(type, init = {}) { super(type, init); this.bubbles = init.bubbles === true; }
  };
  FakeElement.prototype.dispatchEvent = function dispatchEvent(event) {
    const stopped = { immediate: false, propagation: false };
    const stopImmediate = event.stopImmediatePropagation, stop = event.stopPropagation;
    event.stopImmediatePropagation = function() { stopped.immediate = stopped.propagation = true; if (typeof stopImmediate === "function") stopImmediate.call(this); };
    event.stopPropagation = function() { stopped.propagation = true; if (typeof stop === "function") stop.call(this); };
    if (!event.target) event.target = this;
    const path = [];
    for (let node = this; node && node.listeners; node = node.parentNode) path.push(node);
    const run = (node, capture) => {
      for (const entry of (node.listeners.get(event.type) || []).slice()) {
        if (stopped.immediate) return;
        if (entry.capture !== capture) continue;
        if (entry.once) node.removeEventListener(event.type, entry.listener, entry.capture);
        entry.listener(event);
      }
    };
    const visit = (node, capture) => { if (!stopped.propagation) run(node, capture); };
    for (let index = path.length - 1; index > 0; index--) visit(path[index], true);
    visit(path[0], true);
    run(path[0], false);
    if (event.bubbles) for (let index = 1; index < path.length; index++) visit(path[index], false);
    return true;
  };
  return () => { FakeElement.prototype.dispatchEvent = original; env.context.CustomEvent = BaseCustomEvent; };
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

module.exports = { SET_CAMERA, mountScene, drag, browserEvents, distance, aimError };
