"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { FakeElement, createContext, installManualRAF, runScript, flushAsyncWork,
  bootstrapSource } = require("./runtime-test-harness.js");
const fs = require("node:fs");
const path = require("node:path");
const walkSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-walk.js"), "utf8");
const camera = { x: 0, y: 4, z: 0, rotationX: 0.2, rotationY: 0, rotationZ: 0.1, fov: 67, near: 0.1, far: 100 };
function ground(heights, cols = 2, rows = 2, sizeX = 1, sizeZ = 1) {
  const minHeight = Math.min(...heights), maxHeight = Math.max(...heights), bytes = Buffer.alloc(heights.length * 2);
  heights.forEach((h, i) => bytes.writeUInt16LE(maxHeight === minHeight ? 0 : Math.round((h - minHeight) / (maxHeight - minHeight) * 65535), i * 2));
  return { minX: 0, minZ: 0, sizeX, sizeZ, cols, rows, minHeight, maxHeight, heights: bytes.toString("base64") };
}
function model(config = {}, reduced = false, pose = camera) {
  const env = createContext({}); env.context.atob = atob;
  runScript(walkSource, env.context, "bootstrap-feature-scene3d-walk.js");
  const api = env.context.__gosx_scene3d_walk_api;
  return { env, api, state: api.createState(pose, config, reduced) };
}
function near(actual, expected, tolerance = 1e-5) { assert.ok(Math.abs(actual - expected) < tolerance, `${actual} != ${expected}`); }

test("walk ground bilinear sampling clamps edges and supports constant fields", () => {
  const { api } = model(), field = api.decodeGround(ground([0, 2, 4, 6]));
  near(api.sampleGround(field, 0.5, 0.5), 3); near(api.sampleGround(field, 0.25, 0.75), 3.5);
  near(api.sampleGround(field, -5, 3), 4); near(api.sampleGround(api.decodeGround(ground([3], 1, 1)), 5, 5), 3);
  assert.throws(() => api.decodeGround({ ...ground([0]), cols: 2, rows: 2 }), /sample count/);
});
test("walk slides along a cylinder and cannot tunnel on a long sprint step", () => {
  const config = { radius: 0.35, colliders: [{ kind: "cylinder", x: 0.6, y: 0, z: 0, radius: 0.2, height: 0 }] };
  const { api, state } = model(config);
  api.move(state, 0.3, 0.3); assert.ok(state.x < 0.15); assert.ok(state.z >= 0.3);
  const other = api.createState(camera, config, false); api.move(other, 3, 0); assert.ok(other.x < 0.15);
});
test("walk uses collider footprints at feet height, including rotated boxes and sphere slices", () => {
  const { api, state } = model({ colliders: [{ kind: "cylinder", x: 0.5, y: 2, radius: 1, height: 1 }] });
  assert.equal(api.candidateAllowed(state, 0.5, 0), true);
  state.config.colliders = [{ kind: "box", x: 0.8, y: 0, z: 0, sizeX: 1.2, sizeY: 2, sizeZ: 0.2, rotationY: Math.PI / 2 }];
  assert.equal(api.candidateAllowed(state, 0.8, 0), false); assert.equal(api.candidateAllowed(state, 0.1, 0), true);
  state.config.colliders = [{ kind: "sphere", x: 1, y: 1, z: 0, radius: 0.5 }];
  assert.equal(api.candidateAllowed(state, 1, 0), true);
  state.config.colliders[0].y = 0; assert.equal(api.candidateAllowed(state, 1, 0), false);
});
test("deep water blocks walking but 0.4 metre wading is allowed", () => {
  const { api, state } = model({ water: { level: 0.4 } });
  api.move(state, 0.2, 0); near(state.x, 0.2);
  state.config.water.level = 0.7; api.move(state, 0.2, 0); near(state.x, 0.2);
});
test("continuous steep slopes block even tiny frame steps while a 0.2 metre ledge is allowed", () => {
  const { api, state } = model({ ground: ground([0, 2, 0, 2]) });
  api.move(state, 0.02, 0); near(state.x, 0);
  const ledge = api.createState(camera, { ground: ground([0, 0.2, 0, 0.2], 2, 2, 0.1) }, false);
  api.move(ledge, 0.4, 0); near(ledge.x, 0.4);
});
test("bounds clamp travel to the world edge and preserve sliding", () => {
  const { api, state } = model({ bounds: { minX: -1, minZ: -1, maxX: 0.2, maxZ: 0.5 } });
  api.move(state, 1, 1); near(state.x, 0.2); near(state.z, 0.5);
});
test("first frame retains the entire authored camera; eye following starts on movement and eases", () => {
  const { api, state } = model({});
  assert.deepEqual({ ...state.camera }, camera);
  api.advance(state, 0.1, 0, 0, false); assert.deepEqual({ ...state.camera }, camera);
  api.advance(state, 0.016, 0, 1, false); assert.ok(state.camera.y < 4 && state.camera.y > 1.7);
  for (let i = 0; i < 100; i++) api.advance(state, 0.016, 0, 0, false);
  near(state.camera.y, 1.7); assert.equal(state.settling, false);
});
test("head bob is distance based, fades out, and is zero for reduced motion or amplitude zero", () => {
  for (const [config, reduced] of [[{}, true], [{ headBob: 0 }, false]]) {
    const { api, state } = model(config, reduced); api.advance(state, 0.1, 0, 1, true); assert.equal(state.bob, 0);
  }
  const { api, state } = model(); api.advance(state, 0.1, 0, 1, false); assert.notEqual(state.bob, 0);
  for (let i = 0; i < 20; i++) api.advance(state, 0.016, 0, 0, false);
  assert.equal(state.bobWeight, 0); assert.equal(state.bob, 0);
});
test("movement is horizontal, normalizes diagonals, caps delta, and scales sprint", () => {
  const { api, state } = model({ headBob: 0 });
  api.advance(state, 1, 1, 1, false); near(Math.hypot(state.x, state.z), 0.16);
  const sprint = api.createState({ ...camera, rotationX: 1.4 }, { headBob: 0 }, false);
  api.advance(sprint, 0.1, 0, 1, true); near(sprint.z, -0.352);
  const slow = api.createState(camera, { headBob: 0 }, false);
  for (let i = 0; i < 10; i++) api.advance(slow, 0.01, 0, 1, false);
  near(slow.z, -0.16);
});

function controls(config = {}, options = {}) {
  const mount = new FakeElement("div", null); mount.id = "walk-scene"; mount.setAttribute("data-gosx-engine", "GoSXScene3D");
  const env = createContext({ ...options, elements: [mount], performanceNow: () => 0 });
  env.context.atob = atob; runScript(walkSource, env.context, "walk.js");
  const canvas = env.document.createElement("canvas"); canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 600, height: 400 }); mount.appendChild(canvas);
  const raf = installManualRAF(env.context), reasons = [], sceneState = {};
  const handle = env.context.__gosx_scene3d_walk_api.setup(canvas, { controls: "first-person", walk: config, ...(options.props || {}) }, () => camera, (r) => reasons.push(r), sceneState, {
    camera: (c) => c, requestLock: (c) => { c.requestPointerLock(); return true; },
    exitLock: () => env.document.exitPointerLock(), locked: (c) => env.document.pointerLockElement === c,
    requestFrame: (cb) => env.context.requestAnimationFrame(cb), cancelFrame: (id) => env.context.cancelAnimationFrame(id), now: () => 0,
  });
  function event(target, type, props = {}) {
    const e = { type, ...props, preventDefault() { this.defaultPrevented = true; } }; target.dispatchEvent(e); return e;
  }
  return { env, mount, canvas, raf, handle, event, reasons, sceneState };
}
test("keyboard navigation is focus scoped, supports look and Home reset, and stops when idle", () => {
  const h = controls({ headBob: 0 });
  const off = h.event(h.env.document, "keydown", { code: "KeyW" }); assert.equal(off.defaultPrevented, undefined); assert.equal(h.raf.count(), 0);
  h.canvas.focus(); h.event(h.env.document, "keydown", { code: "KeyW" }); h.raf.flush(16);
  assert.ok(h.handle.controller.currentCamera().z < 0);
  h.event(h.env.document, "keydown", { code: "ArrowRight" }); h.event(h.env.document, "keydown", { code: "PageUp" }); h.raf.flush(32);
  const pose = h.handle.controller.currentCamera(); assert.ok(pose.rotationY < 0); assert.ok(pose.rotationX > camera.rotationX);
  h.event(h.env.document, "keydown", { code: "Home" }); assert.deepEqual({ ...pose }, camera); assert.equal(h.raf.count(), 0);
  h.event(h.env.document, "keydown", { code: "KeyW" }); h.raf.flush(48); h.event(h.env.document, "keyup", { code: "KeyW" });
  for (let t = 64; t < 1500; t += 16) h.raf.flush(t);
  assert.equal(h.raf.count(), 0); assert.equal(h.canvas.getAttribute("tabindex"), "0"); assert.match(h.canvas.getAttribute("aria-label"), /Home/);
  h.handle.dispose(); assert.equal(h.sceneState._gosxMotionController, null);
});
test("pointer lock looks with movementX/Y, clamps pitch, hides the hint and restores it on release", () => {
  const h = controls(); const hint = h.mount.querySelector(".gosx-scene3d-walk-hint");
  h.event(h.canvas, "click", { pointerType: "mouse" }); assert.equal(hint.hidden, true);
  h.event(h.env.document, "mousemove", { movementX: 100, movementY: -10000 });
  near(h.handle.controller.currentCamera().rotationY, -0.22); near(h.handle.controller.currentCamera().rotationX, 1.45);
  h.env.document.exitPointerLock(); assert.equal(hint.hidden, false); assert.equal(h.raf.count(), 0); h.handle.dispose();
});
test("touch move and drag-to-look use independent pointer IDs in portrait and landscape", () => {
  for (const [width, height] of [[600, 400], [300, 600]]) {
    const h = controls({ hint: "none" }); h.canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width, height });
    h.event(h.canvas, "pointerdown", { pointerType: "touch", pointerId: 1, clientX: 50, clientY: 200, defaultPrevented: true });
    h.event(h.canvas, "pointerdown", { pointerType: "touch", pointerId: 2, clientX: width - 50, clientY: 200 });
    const move = h.event(h.canvas, "pointermove", { pointerType: "touch", pointerId: 1, clientX: 50, clientY: 145 });
    h.event(h.canvas, "pointermove", { pointerType: "touch", pointerId: 2, clientX: width - 30, clientY: 200 }); h.raf.flush(100);
    const pose = h.handle.controller.currentCamera(); near(pose.z, -0.352 * Math.cos(0.044)); near(pose.rotationY, -0.044);
    assert.equal(move.defaultPrevented, true); assert.equal(h.canvas.style.touchAction, "none");
    h.event(h.canvas, "pointerup", { pointerType: "touch", pointerId: 2 }); assert.equal(h.mount.querySelector(".gosx-scene3d-walk-joystick").style.display, "block");
    h.event(h.canvas, "pointercancel", { pointerType: "touch", pointerId: 1 }); assert.equal(h.mount.querySelector(".gosx-scene3d-walk-joystick").style.display, "none");
    assert.equal(h.mount.querySelector(".gosx-scene3d-walk-hint"), null); h.handle.dispose();
  }
});
test("gamepads poll only while connected, use deadzones, and honor opt-out", () => {
  let calls = 0; const pad = { index: 0, connected: true, axes: [0.1, -1, 1, 0], buttons: [] };
  const h = controls({ headBob: 0 }, { getGamepads: () => { calls++; return [pad]; } });
  h.raf.flush(16); assert.equal(calls, 0);
  h.event(h.env.context, "gamepadconnected", { gamepad: pad }); h.raf.flush(100);
  assert.equal(calls, 1); assert.ok(h.handle.controller.currentCamera().z < 0); assert.equal(h.raf.count(), 1);
  h.event(h.env.context, "gamepaddisconnected", { gamepad: pad });
  for (let t = 116; t < 1400; t += 16) h.raf.flush(t);
  assert.equal(calls, 1); assert.equal(h.raf.count(), 0); h.handle.dispose();
  const off = controls({ gamepad: false }, { getGamepads: () => { calls++; return [pad]; } });
  off.event(off.env.context, "gamepadconnected", { gamepad: pad }); assert.equal(off.raf.count(), 0); off.handle.dispose();
});
test("reset works through a mount ID, the only-scene button, and the scene API", () => {
  const h = controls(); h.canvas.focus(); h.event(h.env.document, "keydown", { code: "KeyW" }); h.raf.flush(32);
  const button = h.env.document.createElement("button"); button.setAttribute("data-gosx-scene3d-reset", h.mount.id);
  h.event(h.env.document, "click", { target: button }); assert.deepEqual({ ...h.handle.controller.currentCamera() }, camera);
  button.setAttribute("data-gosx-scene3d-reset", ""); h.event(h.env.document, "keydown", { code: "KeyW" }); h.raf.flush(32);
  h.event(h.env.document, "click", { target: button }); assert.deepEqual({ ...h.handle.controller.currentCamera() }, camera);
  assert.equal(h.env.context.__gosx.scene3d.resetCamera("missing-mount"), false);
  assert.equal(h.env.context.__gosx.scene3d.resetCamera(h.mount.id), true); h.handle.dispose();
  assert.equal(h.env.context.__gosx.scene3d.resetCamera(h.mount.id), false);
});

test("scene without walk never fetches the walk chunk", async () => {
  const mount = new FakeElement("div", null); mount.id = "ordinary-scene";
  const env = createContext({ elements: [mount], manifest: { engines: [{ id: "plain", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
    props: { controls: "first-person", autoRotate: false, camera } }] } });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js"); env.document.dispatchEvent({ type: "DOMContentLoaded" });
  await flushAsyncWork(); raf.flush(16); await flushAsyncWork(); raf.flush(32); await flushAsyncWork();
  const mounted = env.context.__gosx.engines.get("plain"); assert.ok(mounted && mounted.handle);
  assert.equal(env.context.__gosx_scene3d_walk_api, undefined);
  assert.ok(!env.fetchCalls.some((c) => /scene3d-walk/.test(String(c.url || c))));
  assert.ok(!env.document.querySelector('script[data-gosx-script="feature-scene3d-walk"]'));
  mounted.handle.dispose();
});

test("walk scene fetches its advertised chunk, preserves the first pose, publishes camera output, and reuses shaders", async () => {
  const mount = new FakeElement("div", null); mount.id = "mounted-walk"; mount.setAttribute("data-gosx-engine", "GoSXScene3D");
  const env = createContext({ elements: [mount], enableWebGL: true, disableCanvas2D: true, performanceNow: () => 0,
    fetchRoutes: { "/walk-chunk.js": { text: walkSource } },
    manifest: { engines: [{ id: "walker", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: { controls: "first-person", walk: { headBob: 0 }, autoRotate: false, camera, cameraOutputSignal: "walk.camera",
        scene: { objects: [{ id: "box", kind: "box", geometry: "box", size: 1 }], camera } } }] } });
  env.context.atob = atob;
  const script = env.document.createElement("script"); script.setAttribute("data-gosx-script", "feature-scene3d");
  script.setAttribute("data-gosx-scene3d-walk-url", "/walk-chunk.js"); env.document.head.appendChild(script);
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js"); env.document.dispatchEvent({ type: "DOMContentLoaded" });
  await flushAsyncWork(); raf.flush(16); await flushAsyncWork(); raf.flush(32); await flushAsyncWork();
  const mounted = env.context.__gosx.engines.get("walker"); assert.ok(mounted && mounted.handle);
  assert.equal(env.fetchCalls.filter((c) => c.url === "/walk-chunk.js").length, 1);
  const output = [];
  const unsubscribe = env.context.__gosx_subscribe_shared_signal("walk.camera", (value) => output.push(value));
  const initial = { ...mounted.handle.getCamera() };
  for (const [key, value] of Object.entries(camera)) near(initial[key], value);
  const canvas = mount.querySelector("canvas"), gl = canvas.getContext("webgl"), programCount = gl.programs.length;
  assert.ok(programCount > 0, "fixture must render with a compiled shader");
  canvas.focus(); env.document.dispatchEvent({ type: "keydown", code: "KeyW", preventDefault() {} });
  raf.flush(48); raf.flush(64); await flushAsyncWork();
  assert.ok(mounted.handle.getCamera().z < initial.z);
  assert.equal(gl.programs.length, programCount, "movement must not compile another shader");
  near(mounted.handle.getTelemetry().camera.z, mounted.handle.getCamera().z);
  assert.ok(output.some((pose) => pose && pose.z < initial.z));
  unsubscribe();
  assert.equal(mounted.handle.resetCamera(), true);
  assert.deepEqual({ ...mounted.handle.getCamera() }, initial);
  mounted.handle.dispose();
});

test("walk renders honour MaxFPS on a fast display while movement still integrates every frame", () => {
  const h = controls({ headBob: 0 }, { props: { maxFPS: 60 } });
  h.canvas.focus(); h.event(h.env.document, "keydown", { code: "KeyW" });
  for (let t = 0; t <= 1000; t += 1000 / 120) h.raf.flush(t);
  const renders = h.reasons.filter((r) => r === "controls").length;
  assert.ok(renders >= 55 && renders <= 62, "renders in one second at 120 Hz with MaxFPS 60: " + renders);
  const z = h.handle.controller.currentCamera().z;
  h.event(h.env.document, "keyup", { code: "KeyW" });
  for (let t = 1008; t < 2500; t += 1000 / 120) h.raf.flush(t);
  assert.ok(h.handle.controller.currentCamera().z <= z, "the final pose after release is rendered");
  assert.equal(h.reasons.at(-1), "controls"); assert.equal(h.raf.count(), 0);
  h.handle.dispose();
});

test("hint and joystick defaults live in one zero-specificity stylesheet so pages can restyle them", () => {
  const h = controls({});
  const hint = h.mount.children.find((c) => c.getAttribute && c.getAttribute("class") === "gosx-scene3d-walk-hint");
  assert.ok(hint, "hint element");
  assert.equal(hint.getAttribute("style") || "", "", "no inline default style on the hint");
  const styles = (h.env.document.head.children || []).filter((c) => c.getAttribute && c.getAttribute("data-gosx-scene3d-walk-style") === "true");
  assert.equal(styles.length, 1);
  assert.match(styles[0].textContent, /:where\(\.gosx-scene3d-walk-hint\)/);
  controls({}).handle.dispose();
  h.handle.dispose();
});

test("walk leaves rendering to an animating scene's paced loop", () => {
  const h = controls({ headBob: 0 }, { props: { maxFPS: 60 } });
  h.mount.setAttribute("data-gosx-scene3d-render-loop", "active");
  h.mount.setAttribute("data-gosx-scene3d-render-loop-wants-animation", "true");
  h.canvas.focus(); h.event(h.env.document, "keydown", { code: "KeyW" });
  for (let t = 0; t <= 500; t += 1000 / 120) h.raf.flush(t);
  assert.equal(h.reasons.filter((r) => r === "controls").length, 0);
  assert.ok(h.handle.controller.currentCamera().z < 0, "the camera still moves for the animation loop to draw");
  h.event(h.env.document, "keyup", { code: "KeyW" }); h.handle.dispose();
});

test("optical zoom scales walking travel and every look input with the same projection ratio", () => {
  const full = model({ headBob: 0 }), zoomed = model({ headBob: 0 });
  zoomed.state.zoomScale = 0.4;
  full.api.advance(full.state, 0.1, 0, 1, false); zoomed.api.advance(zoomed.state, 0.1, 0, 1, false);
  near(zoomed.state.z, full.state.z * 0.4);
  full.api.look(full.state, 0.5, 0.3); zoomed.api.look(zoomed.state, 0.5, 0.3);
  near(zoomed.state.yaw, full.state.yaw * 0.4);
  near(zoomed.state.pitch - camera.rotationX, (full.state.pitch - camera.rotationX) * 0.4);
});
