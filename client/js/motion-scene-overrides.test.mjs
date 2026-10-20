import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = fs.readFileSync(path.join(repoRoot, "client/runtime/scene3d/mount-controls.ts"), "utf8");
const trackSource = fs.readFileSync(path.join(repoRoot, "client/runtime/motion/track.ts"), "utf8");

function createAdapterHarness() {
  let adapter = null;
  const invalidations = [];
  const window = { __gosx: { motion: { registerProgramFeature() {}, attachScene(_mount, next) { adapter = next; return () => {}; } } } };
  const context = vm.createContext({
    window,
    document: {},
    sceneNumber(value, fallback) { const number = Number(value); return Number.isFinite(number) ? number : fallback; },
  });
  vm.runInContext(fs.readFileSync(path.join(repoRoot, "client/js/bootstrap-src/11-scene-math.ts"), "utf8"), context);
  vm.runInContext(source, context, { filename: "mount-controls.ts" });
  vm.runInContext(trackSource, context, { filename: "track.ts" });
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

test("object layers apply before geometry baking without mutating scene or named material defaults", () => {
  const h = createAdapterHarness(), token = {};
  const source = {id: "cube", x: 0, scaleX: 1, customUniforms: {glow: 0, warmth: 0}};
  h.adapter().write({target: "sceneNode",node: "cube",property: "position.x"}, 3, token);
  h.adapter().write({target: "sceneNode",node: "cube",property: "scale.x"}, 2, token);
  h.adapter().write({target: "materialUniform",node: "cube",property: "warmth"}, .8, token);
  const [resolved] = h.context.sceneMotionObjects([source], h.state);
  assert.equal(resolved.x, 3); assert.equal(resolved.scaleX, 2); assert.equal(resolved.customUniforms.warmth, .8);
  assert.equal(resolved._rigidMaterialProfileStable, false);
  assert.equal(source.x, 0); assert.equal(source.scaleX, 1); assert.equal(source.customUniforms.warmth, 0);
  source.x = -4; // A scene command changes the base under the active program.
  h.adapter().disposeProgram(token);
  assert.equal(h.context.sceneMotionObjects([source], h.state)[0], source);
  assert.equal(source.x, -4);
});

test("runtime bundle uniforms reach the indexed renderer material and isolate shared materials", () => {
  const h = createAdapterHarness(), token = {}, shared = {customUniforms: {gain: .2, warmth: 0}};
  h.adapter().write({target: "materialUniform",node: "cube",property: "gain"}, .9, token);
  h.adapter().write({target: "materialUniform",node: "cube",property: "warmth"}, .8, token);
  const frame = {materials: [shared],meshObjects: [
    {id: "cube",materialIndex: 0,customUniforms: shared.customUniforms},
    {id: "other",materialIndex: 0,customUniforms: shared.customUniforms},
  ]};
  h.context.applyMotionBindingsToRuntimeBundle(frame, h.state, null);
  assert.equal(frame.materials.length, 2, "clone once per overridden object, not once per uniform");
  assert.equal(frame.meshObjects[0].materialIndex, 1);
  assert.equal(frame.materials[1].customUniforms.gain, .9); assert.equal(frame.materials[1].customUniforms.warmth, .8);
  assert.equal(frame.meshObjects[1].materialIndex, 0);
  assert.deepEqual(shared.customUniforms, {gain: .2,warmth: 0});
});

test("typed positions, scales and quaternion rotations share the renderer's component overrides", () => {
  const harness = createAdapterHarness(), adapter = harness.adapter(), token = {};
  adapter.write({target:"sceneNode",node:"cube",property:"position"},[1,2,3],token);
  adapter.write({target:"sceneNode",node:"cube",property:"scale"},[.5,1,2],token);
  adapter.write({target:"sceneNode",node:"cube",property:"rotation"},[Math.SQRT1_2,0,0,Math.SQRT1_2],token);
  adapter.write({target:"camera",property:"position"},[3,4,8],token);
  adapter.write({target:"camera",property:"rotation"},[Math.SQRT1_2,0,0,Math.SQRT1_2],token);
  adapter.write({target:"materialUniform",node:"cube",property:"tint"},[.2,.4,.6,1],token);
  const frame = bundle();harness.context.applyMotionBindingsToRuntimeBundle(frame,harness.state,null);
  const object = frame.meshObjects[0];
  assert.deepEqual([object.x,object.y,object.z],[1,2,3]);
  assert.deepEqual([object.scaleX,object.scaleY,object.scaleZ],[.5,1,2]);
  assert.ok(Math.abs(object.rotationX-Math.PI/2)<1e-7);
  assert.deepEqual([frame.camera.x,frame.camera.y,frame.camera.z],[3,4,8]);
  assert.ok(Math.abs(frame.camera.rotationX-Math.PI/2)<1e-7);
  assert.deepEqual(Array.from(object.customUniforms.tint),[.2,.4,.6,1]);
  const entries = harness.state._gosxMotionRuntimeBindings.get(token), count = entries.size;
  adapter.write({target:"sceneNode",node:"cube",property:"position"},[5,6,7],token);
  assert.equal(entries.size,count,"repeated vector samples reuse their component entries");
  const next = bundle();harness.context.applyMotionBindingsToRuntimeBundle(next,harness.state,null);
  assert.deepEqual([next.meshObjects[0].x,next.meshObjects[0].y,next.meshObjects[0].z],[5,6,7]);
  adapter.disposeProgram(token);
  const restored = bundle();harness.context.applyMotionBindingsToRuntimeBundle(restored,harness.state,null);
  assert.deepEqual([restored.meshObjects[0].x,restored.meshObjects[0].y,restored.meshObjects[0].z],[0,0,0]);
  assert.equal(restored.camera.z,5);assert.equal(harness.state._gosxMotionRuntimeBindings.size,0);
});
