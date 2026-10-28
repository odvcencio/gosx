"use strict";
// Motion frames meet a real Scene3D mount while its models hydrate. The frame is
// held only while a hydration is in flight, and fails at once when none is, as
// it did before motion frames could wait. The fake-mount tests for the queue and
// for the hold itself are in scene3d-motion-frame.test.mjs; these run the
// bundles a page loads, with a model route the test controls.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapSource,
  bootstrapFeatureScene3DCommandSource,
  FakeElement,
  buildMinimalGLBBytes,
  buildSkinnedGLBBytes,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

// A GSP3 frame: one batch ("heroes") with one instance ("hero") moving along x.
function motionFrame() {
  const bytes = [];
  const encoder = new TextEncoder();
  const text = (value) => { const chars = encoder.encode(value); bytes.push(chars.length, 0, ...chars); };
  const u16 = (value) => bytes.push(value & 255, value >> 8);
  const f32 = (value) => { const data = new DataView(new ArrayBuffer(4)); data.setFloat32(0, value, true); bytes.push(...new Uint8Array(data.buffer)); };
  bytes.push(71, 83, 80, 51);
  u16(1); text("run");
  u16(1); text("heroes"); u16(1);
  text("hero");
  for (const value of [1, 0, 0, 0, 0, 0, 1, 1, 1]) f32(value);
  f32(10);
  for (const value of [2, 0, 0, 0, 0, 0, 1, 1, 1]) f32(value);
  f32(10.1);
  u16(1); f32(9.5); bytes.push(0); f32(1);
  return Uint8Array.from(bytes);
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise; });
  return { promise, resolve, reject };
}

// settled reports whether a promise finished, either way, within ms.
const settled = (promise, ms) => Promise.race([
  promise.then(() => true, () => true),
  new Promise((resolve) => setTimeout(() => resolve(false), ms)),
]);

// outcome waits for a frame and reports how it ended and how long that took.
async function outcome(promise) {
  const started = Date.now();
  try {
    return { ok: true, value: await promise, ms: Date.now() - started };
  } catch (error) {
    return { ok: false, error: String(error && error.message || error), ms: Date.now() - started };
  }
}

const CROWD = { id: "heroes", src: "/models/hero.glb", instances: [{ id: "hero" }] };

// mountScene mounts a scene with the given props and loads the command bridge
// the way a page does on its first Go call. The page's script context has no
// TextDecoder of its own, which the frame decoder needs. prepare(env) runs once
// the runtime is loaded and prepareMount(mount) before the page exists, for a
// test that changes how the mount behaves before the scene starts.
async function mountScene(props, fetchRoutes, prepare, prepareMount) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-motion-frame-mount";
  if (prepareMount) prepareMount(mount);
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes,
    manifest: { engines: [{ id: "gosx-engine-motion-frame", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: Object.assign({ width: 480, height: 300, autoRotate: false, camera: { x: 0, y: 0.2, z: 6, fov: 72 }, scene: { objects: [] } }, props) }] },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  env.context.TextDecoder = TextDecoder;
  await flushAsyncWork();
  if (prepare) prepare(env);
  for (let attempt = 0; attempt < 8; attempt += 1) await flushAsyncWork();
  runScript(bootstrapFeatureScene3DCommandSource, env.context, "bootstrap-feature-scene3d-command.js");
  const mounted = env.context.__gosx.engines.get("gosx-engine-motion-frame");
  return { env, mount, mounted, state: () => mount.__gosxScene3DState, bridge: env.context.__gosx_scene3d_command_bridge };
}

// failSkinStage makes the next model hydration fail the way the transactional
// skin-stage test in runtime-04-scene-materials-lighting.test.js does.
function failSkinStage(env) {
  const animationAPI = env.context.__gosx_scene3d_animation_api;
  animationAPI.computeJointMatrices = function throwingSkinStage() { throw new Error("transactional-skin-stage-boom"); };
  animationAPI.createMixer = function trackedMixer() {
    return { addClip() {}, play() {}, stop() {}, update() {}, isPlaying() { return false; }, dispose() {} };
  };
}

const BROKEN_RIG = { id: "broken-rig", src: "/models/transaction-rig.glb", animation: "bend" };

// A frame that reached the committed crowd either applies or is refused by the
// renderer's own checks. The fake WebGL context has no GPU crowd path, so these
// tests see the second; what they show is that it was not "renderer-not-ready".
function assertReachedCommittedCrowd(result, mounted) {
  assert.ok(result.ok || /retained-motion-unavailable/.test(result.error), JSON.stringify(result));
  assert.equal(mounted.handle.__gosxMotionFrameStats.rejected["renderer-not-ready"], undefined);
}

test("a motion frame sent after the first model hydration failed fails at once", async () => {
  const route = deferred();
  const { env, mount, bridge, state } = await mountScene(
    { scene: { objects: [{ id: "authored-baseline", kind: "box" }], models: [BROKEN_RIG] } },
    { "/models/transaction-rig.glb": () => route.promise },
    (page) => { failSkinStage(page); route.resolve({ bytes: buildSkinnedGLBBytes() }); });
  assert.equal(mount.getAttribute("data-gosx-scene3d-model-hydration-status"), "failed");
  assert.equal(state()._hydratedModelRecords, undefined, "nothing committed");
  assert.equal(state()._modelHydrationUncommitted, true, "the failed declarations stay uncommitted");

  const frame = await outcome(bridge.dispatchMotionFrame(mount, motionFrame(), { timeoutMS: 5000 }));
  assert.equal(frame.ok, false);
  assert.match(frame.error, /renderer-not-ready/, "not a wait that ends in 'still hydrating'");
  assert.ok(frame.ms < 1000, "a hydration that failed will not finish, so the frame must not wait for it: " + frame.ms + " ms");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("a failed model hydration lets a motion frame take its fallback at once", async () => {
  const route = deferred();
  const { mount, bridge } = await mountScene(
    { scene: { objects: [{ id: "authored-baseline", kind: "box" }], models: [BROKEN_RIG] } },
    { "/models/transaction-rig.glb": () => route.promise },
    (page) => { failSkinStage(page); route.resolve({ bytes: buildSkinnedGLBBytes() }); });
  const fallback = [{ kind: 0, objectId: "fallback-marker", data: { kind: "mesh", props: { geometry: "box" } } }];
  const frame = await outcome(bridge.dispatchMotionFrame(mount, motionFrame(), { timeoutMS: 5000, fallbackCommands: fallback }));
  assert.equal(frame.ok, true, JSON.stringify(frame));
  assert.equal(frame.value.binary, false);
  assert.match(frame.value.fallbackReason, /renderer-not-ready/);
  assert.ok(frame.ms < 1000, "the fallback is not delayed by the call's timeout: " + frame.ms + " ms");
  assert.ok(mount.__gosxScene3DState.objects.has("fallback-marker"), "the fallback commands applied");
});

test("a motion frame sent while a renderBeforeModels scene loads its crowd waits for the commit", async () => {
  const route = deferred();
  const { mount, mounted, bridge, state } = await mountScene(
    { renderBeforeModels: true, scene: { objects: [], instancedGLBMeshes: [CROWD] } },
    { "/models/hero.glb": () => route.promise });
  assert.equal(mounted.handle.__gosxScene3DCommandReady, true, "this scene takes commands before its models load");
  assert.equal(state()._hydratedModelRecords, undefined);
  assert.equal(state()._modelHydrationUncommitted, true);

  const sent = outcome(bridge.dispatchMotionFrame(mount, motionFrame(), { timeoutMS: 5000 }));
  assert.equal(await settled(sent, 150), false, "the frame was rejected instead of held");
  route.resolve({ bytes: buildMinimalGLBBytes() });
  const frame = await sent;
  assert.equal(state()._modelHydrationUncommitted, false);
  assertReachedCommittedCrowd(frame, mounted);
});

test("a motion frame sent while a command loads the first crowd waits for the commit", async () => {
  const route = deferred();
  const { mount, mounted, bridge, state } = await mountScene({}, { "/models/hero.glb": () => route.promise });
  assert.equal(state()._hydratedModelRecords == null, true, "the scene starts with no models");
  const command = bridge.dispatchCommands(mount, [{ kind: 11, data: { instancedGLBMeshes: [CROWD] } }]);
  await flushAsyncWork();
  assert.ok(state()._modelHydrationPromise, "the command's hydration is in flight");

  const sent = outcome(bridge.dispatchMotionFrame(mount, motionFrame(), { timeoutMS: 5000 }));
  assert.equal(await settled(sent, 150), false, "the frame was rejected instead of held");
  route.resolve({ bytes: buildMinimalGLBBytes() });
  await command;
  const frame = await sent;
  assertReachedCommittedCrowd(frame, mounted);
});

test("a held motion frame fails promptly when its hydration fails", async () => {
  const route = deferred();
  const { mount, bridge, state } = await mountScene(
    { renderBeforeModels: true, scene: { objects: [{ id: "authored-baseline", kind: "box" }], models: [BROKEN_RIG] } },
    { "/models/transaction-rig.glb": () => route.promise },
    failSkinStage);
  assert.equal(state()._modelHydrationUncommitted, true);

  const sent = outcome(bridge.dispatchMotionFrame(mount, motionFrame(), { timeoutMS: 5000 }));
  assert.equal(await settled(sent, 100), false, "the frame is held while the model loads");
  route.resolve({ bytes: buildSkinnedGLBBytes() });
  const frame = await sent;
  assert.equal(frame.ok, false);
  assert.match(frame.error, /renderer-not-ready/);
  assert.ok(frame.ms < 1000, "the frame must not wait for its timeout after the load failed: " + frame.ms + " ms");
});

test("a later hydration that succeeds lets motion frames wait and apply again", async () => {
  const failing = deferred();
  const crowd = deferred();
  const { mount, mounted, bridge, state } = await mountScene(
    { scene: { objects: [{ id: "authored-baseline", kind: "box" }], models: [BROKEN_RIG] } },
    { "/models/transaction-rig.glb": () => failing.promise, "/models/hero.glb": () => crowd.promise },
    (page) => { failSkinStage(page); failing.resolve({ bytes: buildSkinnedGLBBytes() }); });
  assert.equal(state()._modelHydrationFailed, true, "the first hydration failed");

  // A command replaces the declarations; its hydration is a new generation and
  // the earlier failure no longer speaks for it.
  const command = bridge.dispatchCommands(mount, [
    { kind: 10, data: { models: [] } },
    { kind: 11, data: { instancedGLBMeshes: [CROWD] } },
  ]);
  await flushAsyncWork();
  assert.equal(state()._modelHydrationFailed, false, "the new generation cleared the failure");
  const sent = outcome(bridge.dispatchMotionFrame(mount, motionFrame(), { timeoutMS: 5000 }));
  assert.equal(await settled(sent, 150), false, "the frame is held while the new hydration is in flight");
  crowd.resolve({ bytes: buildMinimalGLBBytes() });
  await command;
  assertReachedCommittedCrowd(await sent, mounted);
});

// throwOnHydrationStatus makes the mount refuse the hydration's status write, an
// unexpected failure of the kind the mount's own guard around the first
// hydration anticipates (a producer or listener that throws), not one of the
// structured failures a load or a stage reports.
function throwOnHydrationStatus(status) {
  return (mount) => {
    const setAttribute = mount.setAttribute.bind(mount);
    mount.setAttribute = (name, value) => {
      if (name === "data-gosx-scene3d-model-hydration-status" && value === status) throw new Error("unexpected-hydration-failure");
      return setAttribute(name, value);
    };
  };
}

test("a motion frame sent after a hydration threw unexpectedly fails at once", async () => {
  const route = deferred();
  const { env, mount, bridge, state } = await mountScene(
    { renderBeforeModels: true, scene: { objects: [], instancedGLBMeshes: [CROWD] } },
    { "/models/hero.glb": () => route.promise }, null, throwOnHydrationStatus("loading"));
  assert.ok(env.consoleLogs.warn.some((line) => /model hydration failed.*unexpected-hydration-failure/.test(line)), "the mount reported the exception: " + JSON.stringify(env.consoleLogs.warn));
  assert.equal(state()._hydratedModelRecords, undefined, "nothing committed");
  assert.equal(state()._modelHydrationUncommitted, true);
  assert.equal(state()._modelHydrationFailed, true, "an exception ends the generation as a failed load does");

  const frame = await outcome(bridge.dispatchMotionFrame(mount, motionFrame(), { timeoutMS: 5000 }));
  assert.equal(frame.ok, false);
  assert.match(frame.error, /renderer-not-ready/, "not a wait that ends in 'still hydrating'");
  assert.ok(frame.ms < 1000, "nothing is loading, so the frame must not wait for its timeout: " + frame.ms + " ms");
});

test("a hydration that throws while it commits ends the wait of a held motion frame", async () => {
  const route = deferred();
  const { env, mount, bridge, state } = await mountScene(
    { renderBeforeModels: true, scene: { objects: [], instancedGLBMeshes: [CROWD] } },
    { "/models/hero.glb": () => route.promise });
  const sent = outcome(bridge.dispatchMotionFrame(mount, motionFrame(), { timeoutMS: 5000 }));
  assert.equal(await settled(sent, 100), false, "the frame is held while the model loads");
  // The commit writes the crowd's objects into the scene; refuse that write.
  const objects = state().objects;
  const set = objects.set.bind(objects);
  objects.set = (id, object) => {
    if (!/authored/.test(String(id))) throw new Error("unexpected-commit-failure");
    return set(id, object);
  };
  route.resolve({ bytes: buildMinimalGLBBytes() });
  const frame = await sent;
  assert.ok(env.consoleLogs.warn.some((line) => /model hydration failed.*unexpected-commit-failure/.test(line)), "the mount reported the exception: " + JSON.stringify(env.consoleLogs.warn));
  assert.equal(state()._hydratedModelRecords == null, true, "nothing committed");
  assert.equal(frame.ok, false);
  assert.match(frame.error, /renderer-not-ready/);
  assert.ok(frame.ms < 1000, "the frame must not wait for its timeout after the commit failed: " + frame.ms + " ms");
});
