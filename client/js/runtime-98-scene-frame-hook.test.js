"use strict";
// The Scene3D frame hook: one engine callback inside the mount's animation
// frame (docs/engine-frame-loop.md). A Go game registers it through
// game/host.SceneFrameSource so the renderer's requestAnimationFrame is the
// page's only frame loop. These tests pin the contract the Go side relies on:
//
//   1. A registered hook keeps a static scene drawing every frame and runs
//      once per frame, before the draw, with the frame time and the delta
//      since the previous hook frame.
//   2. The hook's return value is the frame's interpolation alpha, kept on
//      the mount's telemetry.
//   3. Work the hook schedules (a command, a stream write) draws in the same
//      frame and does not add a second frame request.
//   4. Removing the hook lets a static scene stop; replacing it or disposing
//      the mount tells the old hook it was let go.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  bootstrapSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  installManualRAF,
  flushSceneInitialFrameBoundary,
} = require("./runtime-test-harness.js");

function staticManifest() {
  return {
    engines: [
      {
        id: "gosx-engine-frame-hook",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: "scene-frame-hook-root",
        jsExport: "GoSXScene3D",
        props: {
          width: 320,
          height: 200,
          autoRotate: false,
          scene: { points: [{ id: "still", count: 2, positions: [0, 0, 0, 1, 0, 0], sizes: [2, 2], color: "#ffffff" }] },
        },
        capabilities: ["canvas", "animation"],
      },
    ],
  };
}

async function mountStaticScene() {
  const mount = new FakeElement("div", null);
  mount.id = "scene-frame-hook-root";
  const env = createContext({ elements: [mount], manifest: staticManifest() });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  raf.flush(48);
  await flushAsyncWork();
  return { mount, raf, handle: mount.__gosxScene3DHandle };
}

test("a frame hook drives a static scene once per frame, before the draw", async () => {
  const { mount, raf, handle } = await mountStaticScene();
  assert.ok(handle && typeof handle.setFrameHook === "function", "the mount handle exposes setFrameHook");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped", "a static scene parks its loop");

  const calls = [];
  const remove = handle.setFrameHook(function(now, delta, detached) {
    calls.push({ now, delta, detached: detached === true });
    // A command from inside the hook must draw in this frame.
    handle.setAnimationClock({ timeSeconds: calls.length });
    return 0.25;
  });
  assert.equal(typeof remove, "function");
  for (const time of [100, 116, 133]) {
    raf.flush(time);
    await flushAsyncWork();
    assert.equal(raf.count(), 1, "exactly one frame request after a hook frame at " + time);
  }
  assert.deepEqual(calls.map(c => c.now), [100, 116, 133]);
  assert.deepEqual(calls.map(c => c.delta), [0, 16, 17]);
  assert.ok(calls.every(c => !c.detached));
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop-reason"), "frame-hook");
  assert.equal(handle.getTelemetry().frameHook.alpha, 0.25);
  assert.equal(mount.getAttribute("data-gosx-scene3d-animation-clock"), "3.000", "the hook's clock command drew in the hook's frame");

  remove();
  raf.flush(150);
  await flushAsyncWork();
  raf.flush(166);
  await flushAsyncWork();
  assert.equal(calls.length, 3, "a removed hook does not run");
  assert.equal(handle.getTelemetry().frameHook, null);
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "stopped", "without a hook the static scene stops again");
  assert.equal(raf.count(), 0);
});

test("a replaced hook and a disposed mount release the hook", async () => {
  const { raf, handle } = await mountStaticScene();
  const released = [];
  handle.setFrameHook(function(now, delta, detached) { if (detached) released.push("first"); return 0; });
  handle.setFrameHook(function(now, delta, detached) { if (detached) released.push("second"); return 1; });
  assert.deepEqual(released, ["first"], "replacing a hook lets the old one go");
  raf.flush(16);
  await flushAsyncWork();
  assert.equal(handle.getTelemetry().frameHook.alpha, 1);
  assert.throws(() => handle.setFrameHook(42), /function/);
  handle.dispose();
  assert.deepEqual(released, ["first", "second"], "disposing the mount lets the hook go");
  assert.throws(() => handle.setFrameHook(function() {}), /disposed/);
});

test("a hook that throws does not stop the frame loop", async () => {
  const { raf, handle, mount } = await mountStaticScene();
  let calls = 0;
  const original = console.error;
  console.error = function() {};
  try {
    handle.setFrameHook(function() { calls++; throw new Error("boom"); });
    raf.flush(16);
    await flushAsyncWork();
    raf.flush(32);
    await flushAsyncWork();
  } finally {
    console.error = original;
  }
  assert.equal(calls, 2);
  assert.equal(mount.getAttribute("data-gosx-scene3d-render-loop"), "active");
  assert.equal(handle.getTelemetry().frameHook.alpha, 0);
});

test("hook frames that change nothing draw the retained bundle", async () => {
  const { raf, handle } = await mountStaticScene();
  let write = false;
  handle.setFrameHook(function() {
    if (write) handle.setAnimationClock({ timeSeconds: 1 });
    return 0;
  });
  raf.flush(16);
  await flushAsyncWork();
  const start = Object.assign({}, handle.getTelemetry().bundle);
  for (const time of [32, 48, 64, 80]) {
    raf.flush(time);
    await flushAsyncWork();
  }
  const idle = handle.getTelemetry().bundle;
  assert.equal(idle.builds, start.builds, "an idle hook frame must not rebuild the bundle");
  assert.equal(idle.reuses, start.reuses + 4);
  write = true;
  raf.flush(96);
  await flushAsyncWork();
  assert.equal(handle.getTelemetry().bundle.builds, idle.builds + 1, "a hook that changes the scene rebuilds the bundle");
});

function bakedScene(api, x) {
  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#8de1ff", roughness: 0.4, metalness: 0.1 }],
      objects: [
        { id: "box", kind: "box", width: 1, height: 1, depth: 1, x: x, y: 0, z: 0, material: "m", wireframe: false },
        { id: "edge", kind: "box", width: 1, height: 1, depth: 1, x: -2, y: 0, z: 0, material: "m", wireframe: true },
      ],
    },
  }, { tier: "full" });
  return api.sceneStateObjectsWithMaterials(state);
}

function buildBaked(api, objects, retention) {
  return api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 6, fov: 60, near: 0.05, far: 128 },
    objects, [], [], [], [], {}, 0, [], [], [], [], [], 0, false, undefined, undefined, retention,
  );
}

test("the bundle build reuses a typed array only when its values are unchanged", async () => {
  const env = createContext({ elements: [] });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const api = env.context.__gosx_scene3d_api;
  const keys = ["worldPositions", "worldColors", "worldMeshPositions", "worldMeshNormals"];
  const retention = {};
  const first = buildBaked(api, bakedScene(api, 0), retention);
  for (const key of keys) assert.ok(first[key].length > 0, key + " must hold data for this test");
  const same = buildBaked(api, bakedScene(api, 0), retention);
  for (const key of keys) assert.equal(same[key], first[key], key + ": unchanged values keep the array");
  const moved = buildBaked(api, bakedScene(api, 1), retention);
  assert.notEqual(moved.worldMeshPositions, first.worldMeshPositions, "changed values get a new array");
  assert.notDeepEqual(Array.from(moved.worldMeshPositions), Array.from(first.worldMeshPositions));
  assert.deepEqual(Array.from(first.worldMeshPositions), Array.from(same.worldMeshPositions), "a handed-out array is never rewritten");
  const back = buildBaked(api, bakedScene(api, 0), retention);
  assert.notEqual(back.worldMeshPositions, first.worldMeshPositions, "an older array is not handed out again");
  assert.deepEqual(Array.from(back.worldMeshPositions), Array.from(first.worldMeshPositions));
  // Without retention every build gets new arrays, as before.
  const a = buildBaked(api, bakedScene(api, 0));
  const b = buildBaked(api, bakedScene(api, 0));
  assert.notEqual(a.worldMeshPositions, b.worldMeshPositions);
});
