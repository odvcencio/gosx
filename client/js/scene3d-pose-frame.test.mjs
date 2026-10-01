import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const directory = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(directory, "..", "runtime", "scene3d", "command-runtime.ts"), "utf8");

function runtime(handle, extra = {}) {
  const context = { window: {}, document: {}, TextDecoder, Uint8Array, ArrayBuffer, DataView, Set, Map, Date, Promise, setTimeout, ...extra };
  vm.createContext(context);
  vm.runInContext(source, context);
  return { context, bridge: context.window.__gosx_scene3d_command_bridge, handle };
}

function frame(count = 1) {
  const bytes = [];
  const encoder = new TextEncoder();
  const id = (value) => { const chars = encoder.encode(value); bytes.push(chars.length, 0, ...chars); };
  const u16 = (value) => bytes.push(value & 255, value >> 8);
  const f32 = (value) => { const data = new DataView(new ArrayBuffer(4)); data.setFloat32(0, value, true); bytes.push(...new Uint8Array(data.buffer)); };
  bytes.push(71, 83, 80, 50); // GSP2
  u16(1); id("run"); // clip dictionary
  u16(1); id("heroes"); u16(count);
  for (let index = 0; index < count; index++) {
    id(index === 0 ? "hero" : `actor-${index}`);
    for (const value of [1.5, 2, 3, 0, 0.5, 0, 1, 1, 1, 0.25]) f32(value);
    u16(1); bytes.push(1);
  }
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

test("per-target queue orders prerequisite JSON and pose across overlapping frames", async () => {
  const order = [];
  let releaseFirst;
  let commandsSeen = 0;
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands() {
      order.push("commands");
      if (++commandsSeen === 1) return new Promise(resolve => { releaseFirst = resolve; });
      return Promise.resolve();
    },
    applyPoseFrame() { order.push("pose"); return { applied: true, binary: true }; },
  };
  const { bridge } = runtime(handle);
  const first = bridge.dispatchPoseFrame(handle, frame(), { beforeCommands: [{ kind: 5 }] });
  const second = bridge.dispatchPoseFrame(handle, frame(), { beforeCommands: [{ kind: 5 }] });
  for (let i = 0; i < 4; i++) await Promise.resolve();
  assert.deepEqual(order, ["commands"]);
  releaseFirst();
  await Promise.all([first, second]);
  assert.deepEqual(order, ["commands", "pose", "commands", "pose"]);
});

test("slow first frame keeps only the latest pending declaration and pose", async () => {
  const order = [];
  let releaseFirst;
  let commandsSeen = 0;
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands(commands) {
      order.push(`commands-${commands[0].objectId}`);
      if (++commandsSeen === 1) return new Promise(resolve => { releaseFirst = resolve; });
    },
    applyPoseFrame() { order.push("pose"); return { applied: true, binary: true }; },
  };
  const { bridge } = runtime(handle);
  const submit = n => bridge.dispatchPoseFrame(handle, frame(), { beforeCommands: [{ kind: 5, objectId: n }] });
  const first = submit(1), stale = submit(2), latest = submit(3);
  assert.equal((await stale).superseded, true);
  assert.deepEqual(order, ["commands-1"]);
  releaseFirst();
  await Promise.all([first, latest]);
  assert.deepEqual(order, ["commands-1", "pose", "commands-3", "pose"]);
  assert.equal(handle.__gosxPoseFrameStats.superseded, 1);
});

test("superseding a queued declaration carries its membership into the latest pose", async () => {
  let releaseFirst;
  const seenCommands = [];
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands(commands) {
      seenCommands.push(commands);
      if (seenCommands.length === 1) return new Promise(resolve => { releaseFirst = resolve; });
    },
    applyPoseFrame() { return { applied: true, binary: true }; },
  };
  const { bridge } = runtime(handle);
  const first = bridge.dispatchPoseFrame(handle, frame(), { beforeCommands: [{ kind: 5 }] });
  const membership = { kind: 11, data: { instancedGLBMeshes: [{ id: "heroes" }] } };
  const stale = bridge.dispatchPoseFrame(handle, frame(), { beforeCommands: [membership] });
  const latest = bridge.dispatchPoseFrame(handle, frame());
  assert.equal((await stale).superseded, true);
  releaseFirst();
  await Promise.all([first, latest]);
  assert.equal(seenCommands.length, 2);
  assert.equal(seenCommands[1][0], membership);
});

test("asynchronous pose failures update telemetry and emit a resync event", async () => {
  const events = [];
  class FakeCustomEvent { constructor(type, options) { this.type = type; this.detail = options.detail; } }
  const handle = { __gosxScene3DCommandReady: true, applyCommands() {}, applyPoseFrame() { throw new Error("retained pose unavailable"); } };
  const { bridge } = runtime(handle, { window: { dispatchEvent(event) { events.push(event); } }, CustomEvent: FakeCustomEvent });
  await assert.rejects(bridge.dispatchPoseFrame(handle, frame()), /retained pose unavailable/);
  assert.equal(handle.__gosxPoseFrameStats.errors, 1);
  assert.equal(handle.__gosxPoseFrameStats.lastError, "retained pose unavailable");
  assert.equal(events[0].type, "gosx:scene3d:pose-frame-error");
  assert.equal(events[0].detail.reason, "retained pose unavailable");
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

test("one decoder serves a dense frame and reordered membership falls back without mutation", () => {
  let decoders = 0;
  class CountingDecoder extends TextDecoder { constructor(...args) { super(...args); decoders++; } }
  const { bridge } = runtime(null, { TextDecoder: CountingDecoder });
  const bytes = frame(500);
  const decoded = bridge.decodePoseFrame(bytes);
  assert.equal(decoders, 1);
  assert.equal(decoded[0].instances.length, 500);
  const first = { id: "hero", x: 0 }, second = { id: "other", x: 0 };
  const state = { instancedGLBMeshes: [{ id: "heroes", instances: [second, first] }], _hydratedModelRecords: {} };
  const handle = {};
  const reordered = [{ id: "heroes", instances: [{ ...decoded[0].instances[0], id: "hero" }, { ...decoded[0].instances[0], id: "other" }] }];
  assert.throws(() => bridge.applyMountedPoseFrame(state, reordered, () => true, () => {}, handle), /membership-order-changed/);
  assert.equal(first.x, 0);
  assert.equal(second.x, 0);
});

test("retained pose application reaches the existing crowd animation rows", () => {
  const { context, bridge } = runtime();
  const rendererSource = fs.readFileSync(path.join(directory, "..", "runtime", "scene3d", "mount-webgl.ts"), "utf8");
  const start = rendererSource.indexOf("function sceneUpdateRigidInstancePoses(");
  const end = rendererSource.indexOf("function sceneRigidMembershipModelID(", start);
  assert.ok(start > 0 && end > start);
  context.sceneHydrationModels = state => state.instancedGLBMeshes[0].instances.map(instance => ({ id: instance.id, x: instance.x, _crowdPose: { animation: instance.animation, animationTime: instance.animationTime, animationLoop: instance.animationLoop } }));
  context.sceneModelTransformMatrix = model => new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, model.x, 0, 0, 1]);
  context.sceneRigidMembershipModelID = () => "";
  context.sceneRigidInstanceHydrationKey = (_state, model) => model.id;
  context.sceneReusableRigidInstance = () => true;
  context.sceneRigidMembershipScopeKey = () => "";
  context.sceneInstancedGLBHydrationTemplates = new WeakMap();
  vm.runInContext(rendererSource.slice(start, end), context);
  const atlas = { clips: new Map([["run", { start: 1, duration: 1, segments: 4 }]]), missingClips: new Set() };
  const rows = new Float32Array(3);
  const object = { _crowdMotion: { record: new Float32Array(24) }, _crowdSkin: { atlas, rows, poseRows(_atlas, pose, out) { out[0] = pose.animation === "run" ? 1 + pose.animationTime * 4 : 0; out[1] = pose.animationLoop ? 1 : 0; return out; } } };
  const staged = { objects: [object] };
  const state = { instancedGLBMeshes: [{ id: "heroes", instances: [{ id: "hero", x: 0, animation: "idle", animationTime: 0, animationLoop: false }] }], _hydratedModelRecords: { modelCount: 1, rigidInstances: new Map([["hero", staged]]) } };
  bridge.applyMountedPoseFrame(state, bridge.decodePoseFrame(frame()), context.sceneUpdateRigidInstancePoses, () => {}, {});
  assert.equal(object.parentMatrix[12], 1.5);
  assert.equal(object._crowdMotion, undefined, 'pose fallback returns the object to the legacy crowd shader');
  assert.deepEqual(Array.from(rows.slice(0, 2)), [2, 1]);
});
