import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = fs.readFileSync(path.join(repoRoot, "client/runtime/scene3d/mount-controls.ts"), "utf8");

function createAdapterHarness() {
  let adapter = null;
  const invalidations = [];
  const window = { __gosx: { motion: { attachScene(_mount, next) { adapter = next; return () => {}; } } } };
  const context = vm.createContext({
    window,
    document: {},
    sceneNumber(value, fallback) { const number = Number(value); return Number.isFinite(number) ? number : fallback; },
  });
  vm.runInContext(source, context, { filename: "mount-controls.ts" });
  const state = { camera: { x: 0, y: 0, z: 5, fov: 75 }, objects: new Map() };
  context.attachSceneMotionBridge({}, state, (reason) => invalidations.push(reason));
  return { context, state, adapter: () => adapter, invalidations };
}

function bundle(cameraZ = 5) {
  return {
    camera: { x: 0, y: 0, z: cameraZ, fov: 75 },
    meshObjects: [{ id: "cube", x: 0, y: 0, z: 0, scaleX: 1, scaleY: 1, scaleZ: 1, customUniforms: { glow: 0 } }],
  };
}

test("Scene3D motion overrides survive rebuilt bundles and clear with their program", () => {
  const harness = createAdapterHarness();
  const adapter = harness.adapter();
  const token = { id: "page-a" };
  adapter.write({ target: "sceneNode", node: "cube", property: "scale.x" }, 2, token);
  adapter.write({ target: "materialUniform", node: "cube", property: "glow" }, 1.5, token);
  adapter.write({ target: "camera", property: "position.z" }, 3, token);

  const first = bundle();
  harness.context.applyMotionBindingsToRuntimeBundle(first, harness.state, null);
  assert.equal(first.meshObjects[0].scaleX, 2);
  assert.equal(first.meshObjects[0].customUniforms.glow, 1.5);
  assert.equal(first.camera.z, 3);

  // A new scene command/runtime frame creates fresh objects, but the active
  // program layer is applied over them again before render.
  const nextFrame = bundle(8);
  harness.context.applyMotionBindingsToRuntimeBundle(nextFrame, harness.state, null);
  assert.equal(nextFrame.meshObjects[0].scaleX, 2);
  assert.equal(nextFrame.meshObjects[0].customUniforms.glow, 1.5);
  assert.equal(nextFrame.camera.z, 3);

  adapter.disposeProgram(token);
  const reusedScene = bundle(8);
  harness.context.applyMotionBindingsToRuntimeBundle(reusedScene, harness.state, null);
  assert.equal(reusedScene.meshObjects[0].scaleX, 1);
  assert.equal(reusedScene.meshObjects[0].customUniforms.glow, 0);
  assert.equal(reusedScene.camera.z, 8);
  assert.equal(harness.state._gosxMotionRuntimeBindings.size, 0);
  assert.deepEqual(harness.invalidations, ["motion-dispose"]);
});

test("PinTo stores node positions in the same disposable program layer", () => {
  const harness = createAdapterHarness();
  const adapter = harness.adapter();
  const token = { id: "pin-page" };
  const changed = adapter.pin(
    { node: "cube" },
    { left: 10, top: 20, width: 20, height: 10 },
    { left: 0, top: 0, width: 100, height: 100 },
    token,
  );
  assert.equal(changed, true);
  const pinned = bundle();
  harness.context.applyMotionBindingsToRuntimeBundle(pinned, harness.state, null);
  assert.notEqual(pinned.meshObjects[0].x, 0);
  assert.notEqual(pinned.meshObjects[0].y, 0);

  adapter.disposeProgram(token);
  const cleared = bundle();
  harness.context.applyMotionBindingsToRuntimeBundle(cleared, harness.state, null);
  assert.equal(cleared.meshObjects[0].x, 0);
  assert.equal(cleared.meshObjects[0].y, 0);
});

test("disposing layered camera overrides never writes into the reused scene base camera", () => {
  const harness = createAdapterHarness();
  const adapter = harness.adapter();
  const pageA = { id: "page-a" };
  const pageB = { id: "page-b" };
  adapter.write({ target: "camera", property: "position.x" }, 2, pageA);
  adapter.write({ target: "camera", property: "position.z" }, 3, pageB);

  adapter.disposeProgram(pageA);
  assert.deepEqual(harness.state.camera, { x: 0, y: 0, z: 5, fov: 75 });
  const whileBActive = bundle();
  harness.context.applyMotionBindingsToRuntimeBundle(whileBActive, harness.state, null);
  assert.equal(whileBActive.camera.z, 3);

  adapter.disposeProgram(pageB);
  assert.deepEqual(harness.state.camera, { x: 0, y: 0, z: 5, fov: 75 });
  const afterDispose = bundle();
  harness.context.applyMotionBindingsToRuntimeBundle(afterDispose, harness.state, null);
  assert.equal(afterDispose.camera.x, 0);
  assert.equal(afterDispose.camera.z, 5);
  assert.equal(harness.state._gosxMotionRuntimeBindings.size, 0);
});
