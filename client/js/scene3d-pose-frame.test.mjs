import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const directory = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(directory, "..", "runtime", "scene3d", "command-runtime.ts"), "utf8");

function runtime(handle) {
  const context = { window: {}, document: {}, TextDecoder, Uint8Array, ArrayBuffer, DataView, Set, Map, Date, Promise, setTimeout };
  vm.createContext(context);
  vm.runInContext(source, context);
  return { context, bridge: context.window.__gosx_scene3d_command_bridge, handle };
}

function frame() {
  const bytes = [];
  const encoder = new TextEncoder();
  const id = (value) => { const chars = encoder.encode(value); bytes.push(chars.length, 0, ...chars); };
  const u16 = (value) => bytes.push(value & 255, value >> 8);
  const f32 = (value) => { const data = new DataView(new ArrayBuffer(4)); data.setFloat32(0, value, true); bytes.push(...new Uint8Array(data.buffer)); };
  bytes.push(71, 83, 80, 50); // GSP2
  u16(1); id("run"); // clip dictionary
  u16(1); id("heroes"); u16(1); id("hero");
  for (const value of [1.5, 2, 3, 0, 0.5, 0, 1, 1, 1, 0.25]) f32(value);
  u16(1); bytes.push(1);
  return Uint8Array.from(bytes);
}

test("GSP2 decoder carries transform and animation and rejects truncated frames", () => {
  const { bridge } = runtime();
  const decoded = bridge.decodePoseFrame(frame());
  assert.equal(decoded[0].id, "heroes");
  assert.equal(decoded[0].instances[0].x, 1.5);
  assert.equal(decoded[0].instances[0].animation, "run");
  assert.equal(decoded[0].instances[0].animationTime, 0.25);
  assert.equal(decoded[0].instances[0].animationLoop, true);
  assert.throws(() => bridge.decodePoseFrame(frame().subarray(0, -1)), /truncated/);
});

test("GSP2 dispatch calls the retained pose path", async () => {
  const received = [];
  const handle = { __gosxScene3DCommandReady: true, applyCommands() { throw new Error("JSON planner called"); }, applyPoseFrame(batches) { received.push(batches); return { applied: true }; } };
  const { bridge } = runtime(handle);
  const result = await bridge.dispatchPoseFrame(handle, frame());
  assert.equal(result.applied, true);
  assert.equal(received.length, 1);
});

test("pose rejection uses existing JSON command path when supplied", async () => {
  const commands = [{ kind: 11, data: { instancedGLBMeshes: [] } }];
  const handle = { __gosxScene3DCommandReady: true, applyPoseFrame() { throw new Error("membership-changed"); }, applyCommands(value) { assert.equal(value, commands); return { applied: true }; } };
  const { bridge } = runtime(handle);
  const result = await bridge.dispatchPoseFrame(handle, frame(), { fallbackCommands: commands });
  assert.equal(result.binary, false);
  assert.match(result.fallbackReason, /membership-changed/);
});

test("retained application updates clip and transform, then rolls back failed frames", () => {
  const { bridge } = runtime();
  const instance = { id: "hero", x: 0, animation: "idle", animationTime: 0, animationLoop: false };
  const state = { instancedGLBMeshes: [{ id: "heroes", instances: [instance] }], _hydratedModelRecords: {} };
  const handle = {};
  let rendered = 0;
  const poses = bridge.decodePoseFrame(frame());
  assert.equal(bridge.applyMountedPoseFrame(state, poses, () => true, () => rendered++, handle).binary, true);
  assert.equal(instance.x, 1.5);
  assert.equal(instance.animation, "run");
  assert.equal(rendered, 1);
  assert.equal(handle.__gosxPoseFrameStats.plannerCallsSkipped, 1);
  assert.throws(() => bridge.applyMountedPoseFrame(state, [{ id: "heroes", instances: [{ ...poses[0].instances[0], x: 8 }] }], () => false, () => rendered++, handle), /retained-pose-unavailable/);
  assert.equal(instance.x, 1.5);
  assert.equal(rendered, 1);
  assert.equal(handle.__gosxPoseFrameStats.rejected["retained-pose-unavailable"], 1);
});
