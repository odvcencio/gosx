import { createRequire } from 'node:module';
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const transpile = source => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;

const directory = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(directory, "..", "runtime", "scene3d", "command-runtime.ts"), "utf8");

function runtime(handle, extra = {}) {
  const context = { window: {}, document: {}, TextDecoder, Uint8Array, ArrayBuffer, DataView, Set, Map, Date, Promise, setTimeout, Object, ...extra };
  vm.createContext(context);
  vm.runInContext(transpile(source), context);
  return { context, bridge: context.window.__gosx_scene3d_command_bridge, handle };
}

// frame() builds a valid GSP3 byte buffer: one batch ("heroes") of `count`
// instances, each carrying a motion key from prev(1,0,0) at tPrev=10 to
// next(2,0,0) at tNext=10.1, animating clip "run" from clipStartTime=9.5.
function frame(count = 1, overrides = {}) {
  const bytes = [];
  const encoder = new TextEncoder();
  const id = (value) => { const chars = encoder.encode(value); bytes.push(chars.length, 0, ...chars); };
  const u16 = (value) => bytes.push(value & 255, value >> 8);
  const f32 = (value) => { const data = new DataView(new ArrayBuffer(4)); data.setFloat32(0, value, true); bytes.push(...new Uint8Array(data.buffer)); };
  const prev = overrides.prev || [1, 0, 0, 0, 0, 0, 1, 1, 1];
  const tPrev = overrides.tPrev != null ? overrides.tPrev : 10;
  const next = overrides.next || [2, 0, 0, 0, 0, 0, 1, 1, 1];
  const tNext = overrides.tNext != null ? overrides.tNext : 10.1;
  bytes.push(71, 83, 80, 51); // GSP3
  u16(1); id("run"); // clip dictionary
  u16(1); id("heroes"); u16(count);
  for (let index = 0; index < count; index++) {
    id(index === 0 ? "hero" : `actor-${index}`);
    for (const v of prev) f32(v);
    f32(tPrev);
    for (const v of next) f32(v);
    f32(tNext);
    u16(1); // clip index 1 == "run"
    f32(overrides.clipStartTime != null ? overrides.clipStartTime : 9.5);
    bytes.push(overrides.loop ? 1 : 0);
    f32(overrides.rate != null ? overrides.rate : 1);
  }
  return Uint8Array.from(bytes);
}

test("GSP3 decoder carries motion keys, timestamps, and animation state", () => {
  const { bridge } = runtime();
  const decoded = bridge.decodeMotionFrame(frame());
  assert.equal(decoded[0].id, "heroes");
  const instance = decoded[0].instances[0];
  assert.equal(instance.id, "hero");
  assert.equal(instance.prevX, 1);
  assert.equal(instance.nextX, 2);
  assert.equal(instance.tPrev, 10);
  assert.equal(instance.tNext, Math.fround(10.1));
  assert.equal(instance.animation, "run");
  assert.equal(instance.clipStartTime, 9.5);
  assert.equal(instance.animationLoop, false);
  assert.equal(instance.playbackRate, 1);
});

test("GSP3 decoder rejects truncated frames and unknown clip indices", () => {
  const { bridge } = runtime();
  assert.throws(() => bridge.decodeMotionFrame(frame().subarray(0, -1)), /truncated/);
});

test("GSP3 decoder rejects a frame where tNext does not exceed tPrev", () => {
  const { bridge } = runtime();
  assert.throws(() => bridge.decodeMotionFrame(frame(1, { tPrev: 10, tNext: 10 })), /tNext after tPrev/);
  assert.throws(() => bridge.decodeMotionFrame(frame(1, { tPrev: 10, tNext: 9 })), /tNext after tPrev/);
});

test("GSP3 dispatch calls the retained motion path", async () => {
  const received = [];
  const handle = { __gosxScene3DCommandReady: true, applyCommands() { throw new Error("JSON planner called"); }, applyMotionFrame(batches) { received.push(batches); return { applied: true }; } };
  const { bridge } = runtime(handle);
  const result = await bridge.dispatchMotionFrame(handle, frame());
  assert.equal(result.applied, true);
  assert.equal(received.length, 1);
  assert.equal(received[0][0].instances[0].id, "hero");
});

test("motion rejection falls back to fallbackPoseFrame, then fallbackCommands", async () => {
  const commands = [{ kind: 11, data: { instancedGLBMeshes: [] } }];
  const poseCalls = [];
  const handle = {
    __gosxScene3DCommandReady: true,
    applyMotionFrame() { throw new Error("retained-motion-unavailable"); },
    applyPoseFrame(batches) { poseCalls.push(batches); throw new Error("retained-pose-unavailable"); },
    applyCommands(value) { assert.equal(value, commands); return { applied: true }; },
  };
  const { bridge } = runtime(handle);
  // Encode a minimal valid GSP2 pose frame by hand for the pose fallback.
  const poseBytes = (() => {
    const b = [];
    const encoder = new TextEncoder();
    const id = (value) => { const chars = encoder.encode(value); b.push(chars.length, 0, ...chars); };
    const u16 = (value) => b.push(value & 255, value >> 8);
    const f32 = (value) => { const data = new DataView(new ArrayBuffer(4)); data.setFloat32(0, value, true); b.push(...new Uint8Array(data.buffer)); };
    b.push(71, 83, 80, 50); u16(0); u16(1); id("heroes"); u16(1); id("hero");
    for (const v of [1, 0, 0, 0, 0, 0, 1, 1, 1, 0]) f32(v);
    u16(0); b.push(0);
    return Uint8Array.from(b);
  })();
  const result = await bridge.dispatchMotionFrame(handle, frame(), { fallbackPoseFrame: poseBytes, fallbackCommands: commands });
  assert.equal(result.applied, true);
  assert.equal(result.binary, false);
  assert.match(result.fallbackReason, /retained-pose-unavailable/);
  assert.equal(poseCalls.length, 1);
});

test("motion rejection uses fallbackCommands directly when no fallbackPoseFrame is supplied", async () => {
  const commands = [{ kind: 11, data: { instancedGLBMeshes: [] } }];
  const handle = {
    __gosxScene3DCommandReady: true,
    applyMotionFrame() { throw new Error("membership-changed"); },
    applyCommands(value) { assert.equal(value, commands); return { applied: true }; },
  };
  const { bridge } = runtime(handle);
  const result = await bridge.dispatchMotionFrame(handle, frame(), { fallbackCommands: commands });
  assert.equal(result.binary, false);
  assert.match(result.fallbackReason, /membership-changed/);
});

test("per-target queue orders prerequisite JSON and motion across overlapping frames", async () => {
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
    applyMotionFrame() { order.push("motion"); return { applied: true, binary: true }; },
  };
  const { bridge } = runtime(handle);
  const first = bridge.dispatchMotionFrame(handle, frame(), { beforeCommands: [{ kind: 5 }] });
  const second = bridge.dispatchMotionFrame(handle, frame(), { beforeCommands: [{ kind: 5 }] });
  for (let i = 0; i < 4; i++) await Promise.resolve();
  assert.deepEqual(order, ["commands"]);
  releaseFirst();
  await Promise.all([first, second]);
  assert.deepEqual(order, ["commands", "motion", "commands", "motion"]);
});

test("slow first frame keeps only the latest pending declaration and motion", async () => {
  const order = [];
  let releaseFirst;
  let commandsSeen = 0;
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands(commands) {
      order.push(`commands-${commands[0].objectId}`);
      if (++commandsSeen === 1) return new Promise(resolve => { releaseFirst = resolve; });
    },
    applyMotionFrame() { order.push("motion"); return { applied: true, binary: true }; },
  };
  const { bridge } = runtime(handle);
  const submit = n => bridge.dispatchMotionFrame(handle, frame(), { beforeCommands: [{ kind: 5, objectId: n }] });
  const first = submit(1), stale = submit(2), latest = submit(3);
  assert.equal((await stale).superseded, true);
  assert.deepEqual(order, ["commands-1"]);
  releaseFirst();
  await Promise.all([first, latest]);
  assert.deepEqual(order, ["commands-1", "motion", "commands-3", "motion"]);
  assert.equal(handle.__gosxMotionFrameStats.superseded, 1);
});

test("superseding a queued declaration carries its membership into the latest motion frame", async () => {
  let releaseFirst;
  const seenCommands = [];
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands(commands) {
      seenCommands.push(commands);
      if (seenCommands.length === 1) return new Promise(resolve => { releaseFirst = resolve; });
    },
    applyMotionFrame() { return { applied: true, binary: true }; },
  };
  const { bridge } = runtime(handle);
  const first = bridge.dispatchMotionFrame(handle, frame(), { beforeCommands: [{ kind: 5 }] });
  const membership = { kind: 11, data: { instancedGLBMeshes: [{ id: "heroes" }] } };
  const stale = bridge.dispatchMotionFrame(handle, frame(), { beforeCommands: [membership] });
  const latest = bridge.dispatchMotionFrame(handle, frame());
  assert.equal((await stale).superseded, true);
  releaseFirst();
  await Promise.all([first, latest]);
  assert.equal(seenCommands.length, 2);
  assert.equal(seenCommands[1][0], membership);
});

test("asynchronous motion failures update telemetry and emit a resync event", async () => {
  const events = [];
  class FakeCustomEvent { constructor(type, options) { this.type = type; this.detail = options.detail; } }
  const handle = { __gosxScene3DCommandReady: true, applyCommands() {}, applyMotionFrame() { throw new Error("retained motion unavailable"); } };
  const { bridge } = runtime(handle, { window: { dispatchEvent(event) { events.push(event); } }, CustomEvent: FakeCustomEvent });
  await assert.rejects(bridge.dispatchMotionFrame(handle, frame()), /retained motion unavailable/);
  assert.equal(handle.__gosxMotionFrameStats.errors, 1);
  assert.equal(handle.__gosxMotionFrameStats.lastError, "retained motion unavailable");
  assert.equal(events[0].type, "gosx:scene3d:motion-frame-error");
  assert.equal(events[0].detail.reason, "retained motion unavailable");
});

test("applyMountedMotionFrame validates membership and order before calling updateRigidMotion", () => {
  const { bridge } = runtime();
  const state = { instancedGLBMeshes: [{ id: "heroes", instances: [{ id: "hero" }] }], _hydratedModelRecords: {} };
  const handle = {};
  const calls = [];
  let rendered = 0;
  const batches = bridge.decodeMotionFrame(frame());
  const result = bridge.applyMountedMotionFrame(state, batches, function(s, b) { calls.push([s, b]); return true; }, () => rendered++, handle);
  assert.equal(result.applied, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0][1], batches);
  assert.equal(rendered, 1);
  assert.equal(handle.__gosxMotionFrameStats.accepted, 1);

  // Membership changed: different instance count.
  const changedState = { instancedGLBMeshes: [{ id: "heroes", instances: [] }], _hydratedModelRecords: {} };
  assert.throws(() => bridge.applyMountedMotionFrame(changedState, batches, () => true, () => {}, handle), /membership-changed/);

  // Membership reordered: same ids, different order.
  const reorderedBatches = [{ id: "heroes", instances: [{ ...batches[0].instances[0], id: "other" }] }];
  const reorderedState = { instancedGLBMeshes: [{ id: "heroes", instances: [{ id: "hero" }] }], _hydratedModelRecords: {} };
  assert.throws(() => bridge.applyMountedMotionFrame(reorderedState, reorderedBatches, () => true, () => {}, handle), /membership-order-changed/);
});

test("applyMountedMotionFrame rejects when updateRigidMotion cannot retain, and never renders", () => {
  const { bridge } = runtime();
  const state = { instancedGLBMeshes: [{ id: "heroes", instances: [{ id: "hero" }] }], _hydratedModelRecords: {} };
  const handle = {};
  let rendered = 0;
  const batches = bridge.decodeMotionFrame(frame());
  assert.throws(() => bridge.applyMountedMotionFrame(state, batches, () => false, () => rendered++, handle), /retained-motion-unavailable/);
  assert.equal(rendered, 0);
  assert.equal(handle.__gosxMotionFrameStats.rejected["retained-motion-unavailable"], 1);

  // A throwing updateRigidMotion is treated the same as returning false.
  assert.throws(() => bridge.applyMountedMotionFrame(state, batches, () => { throw new Error("boom"); }, () => rendered++, handle), /retained-motion-unavailable/);
  assert.equal(rendered, 0);
});

test("applyMountedMotionFrame reaches sceneUpdateRigidInstanceMotion and writes a GPU motion record", () => {
  const { context, bridge } = runtime();
  const rendererSource = fs.readFileSync(path.join(directory, "..", "runtime", "scene3d", "mount-webgl.ts"), "utf8");
  const start = rendererSource.indexOf("function sceneUpdateRigidInstanceMotion(");
  const end = rendererSource.indexOf("function sceneRigidMembershipModelID(", start);
  assert.ok(start > 0 && end > start);
  vm.runInContext(rendererSource.slice(start, end), context);
  const writes = [];
  const clipTable = { count: 2 };
  const atlas = {};
  context.window.__gosx_scene3d_animation_api = {
    crowdMotionRecordFloats: 24,
    crowdMotionMaxClips: 32,
    crowdMotionClipTable() { return clipTable; },
    crowdMotionClipIndex(a, name) { return name === "run" ? 1 : 0; },
    crowdMotionWriteRecord(record, instance, clipRow) { writes.push({ record, instance, clipRow }); record[20] = clipRow; },
    crowdMotionLocalRadius() { return 2; },
    crowdMotionSweptBoundsInto(bounds) { bounds.minX = 1; bounds.maxX = 3; bounds.minY = bounds.minZ = -2; bounds.maxY = bounds.maxZ = 2; },
  };
  const object = { _crowdSkin: { atlas, bounds: { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 } } };
  const staged = { objects: [object] };
  const membership = { id: "heroes/hero", key: "hero-key", staged };
  const cache = new Map([["hero-key", staged]]);
  const rigidInstancesByID = new Map([["heroes/hero", membership]]);
  const crowdRenderer = { prepareCrowdMotionShaders: () => true };
  const state = {
    instancedGLBMeshes: [{ id: "heroes", instances: [{ id: "hero" }] }],
    _hydratedModelRecords: { rigidInstances: cache, rigidInstancesByID },
    _crowdRenderer: crowdRenderer,
  };
  const batches = bridge.decodeMotionFrame(frame());
  let rendered = 0;
  const result = bridge.applyMountedMotionFrame(state, batches, context.sceneUpdateRigidInstanceMotion, () => rendered++, {});
  assert.equal(result.applied, true);
  assert.equal(rendered, 1);
  assert.equal(writes.length, 1);
  assert.equal(writes[0].instance.id, "hero");
  assert.equal(writes[0].clipRow, 1);
  assert.ok(object._crowdMotion);
  assert.equal(object._crowdMotion.atlas, atlas);
  assert.equal(object._crowdMotion.clipTable, clipTable);
  assert.equal(object._crowdMotion.dirty, true);
  assert.equal(object._crowdMotion.record[20], 1);
  assert.equal(object._crowdMotion.bounds.maxX, 3);
  delete object._crowdMotion;
  clipTable.count = 33;
  assert.throws(() => bridge.applyMountedMotionFrame(state, batches, context.sceneUpdateRigidInstanceMotion, () => {}, {}), /retained-motion-unavailable/);
  assert.equal(object._crowdMotion, undefined, 'an unsupported clip table must not partially enable GPU motion');
});

test("sceneUpdateRigidInstanceMotion fails closed when the backend has no GPU-motion crowd path", () => {
  const { context, bridge } = runtime();
  const rendererSource = fs.readFileSync(path.join(directory, "..", "runtime", "scene3d", "mount-webgl.ts"), "utf8");
  const start = rendererSource.indexOf("function sceneUpdateRigidInstanceMotion(");
  const end = rendererSource.indexOf("function sceneRigidMembershipModelID(", start);
  vm.runInContext(rendererSource.slice(start, end), context);
  context.window.__gosx_scene3d_animation_api = { crowdMotionRecordFloats: 24, crowdMotionClipTable() { return {}; }, crowdMotionClipIndex() { return 0; }, crowdMotionWriteRecord() {} };
  const object = { _crowdSkin: { atlas: {} } };
  const staged = { objects: [object] };
  const membership = { id: "hero", key: "hero-key", staged };
  const cache = new Map([["hero-key", staged]]);
  const rigidInstancesByID = new Map([["hero", membership]]);
  const state = {
    instancedGLBMeshes: [{ id: "heroes", instances: [{ id: "hero" }] }],
    _hydratedModelRecords: { rigidInstances: cache, rigidInstancesByID },
    _crowdRenderer: { kind: "webgpu" }, // no prepareCrowdMotionShaders
  };
  const batches = bridge.decodeMotionFrame(frame());
  assert.throws(() => bridge.applyMountedMotionFrame(state, batches, context.sceneUpdateRigidInstanceMotion, () => {}, {}), /retained-motion-unavailable/);
  assert.equal(object._crowdMotion, undefined);
});

// ---- GSP4 partial motion frames ----

// The bytes scene.EncodePartialMotionFrame writes for one batch "crowd" of 300
// instances, with slots 2 and 257 carrying the same motion key. The Go golden
// test in scene/motion_frame_partial_test.go builds the identical buffer.
const GSP4_GOLDEN = Uint8Array.from(Buffer.from(
  "475350340100040077616c6b0100050063726f77642c01020002000000803f00000040000040400000003f0000803e000000000000803f0000803f0000803f00002041000080400000a0400000c040000000000000403f0000003e00000040000000400000004000002841010000001841010000c03f" +
  "01010000803f00000040000040400000003f0000803e000000000000803f0000803f0000803f00002041000080400000a0400000c040000000000000403f0000003e00000040000000400000004000002841010000001841010000c03f", "hex"));

// partialFrame builds a GSP4 buffer by hand: one batch of `size` instances that
// lists `slots`, each with the motion key prev(slot,0,0) -> next(slot+1,0,0).
function partialFrame(slots, size = 300, id = "crowd") {
  const bytes = [];
  const encoder = new TextEncoder();
  const text = value => { const chars = encoder.encode(value); bytes.push(chars.length, 0, ...chars); };
  const u16 = value => bytes.push(value & 255, value >> 8);
  const f32 = value => { const data = new DataView(new ArrayBuffer(4)); data.setFloat32(0, value, true); bytes.push(...new Uint8Array(data.buffer)); };
  bytes.push(71, 83, 80, 52);
  u16(1); text("run");
  u16(1); text(id); u16(size); u16(slots.length);
  for (const slot of slots) {
    u16(slot);
    for (const v of [slot, 0, 0, 0, 0, 0, 1, 1, 1, 10, slot + 1, 0, 0, 0, 0, 0, 1, 1, 1, 10.5]) f32(v);
    u16(1); f32(9.5); bytes.push(1); f32(1);
  }
  return Uint8Array.from(bytes);
}

test("GSP4 decoder reads the golden bytes the Go encoder writes", () => {
  const { bridge } = runtime();
  const [batch] = bridge.decodeMotionFrame(GSP4_GOLDEN);
  assert.equal(batch.id, "crowd");
  assert.equal(batch.partial, true);
  assert.equal(batch.size, 300);
  assert.deepEqual(Array.from(batch.instances, instance => instance.slot), [2, 257]);
  const instance = batch.instances[0];
  assert.equal(instance.id, undefined, "partial instances are addressed by slot, not ID");
  assert.deepEqual(Array.from([instance.prevX, instance.prevY, instance.prevZ, instance.prevRotationX, instance.prevRotationY, instance.prevRotationZ]), [1, 2, 3, .5, .25, 0]);
  assert.deepEqual(Array.from([instance.prevScaleX, instance.tPrev, instance.nextX, instance.nextRotationY, instance.nextRotationZ, instance.nextScaleX, instance.tNext]), [1, 10, 4, .75, .125, 2, 10.5]);
  assert.equal(instance.animation, "walk");
  assert.equal(instance.clipStartTime, 9.5);
  assert.equal(instance.animationLoop, true);
  assert.equal(instance.playbackRate, 1.5);
});

test("GSP4 decoder rejects descending slots, slots outside the batch, trailing bytes and truncation", () => {
  const { bridge } = runtime();
  assert.throws(() => bridge.decodeMotionFrame(partialFrame([5, 5])), /ascend strictly/);
  assert.throws(() => bridge.decodeMotionFrame(partialFrame([5, 3])), /ascend strictly/);
  assert.throws(() => bridge.decodeMotionFrame(partialFrame([4], 4)), /outside its batch/);
  assert.throws(() => bridge.decodeMotionFrame(partialFrame([0, 1, 2], 2)), /more instances/);
  assert.throws(() => bridge.decodeMotionFrame(Uint8Array.from([...partialFrame([1]), 0])), /trailing/);
  assert.throws(() => bridge.decodeMotionFrame(partialFrame([1]).subarray(0, -2)), /truncated/);
  assert.throws(() => bridge.decodePoseFrame(partialFrame([1])), /unsupported .* version/);
});

test("applyMountedMotionFrame resolves partial slots to mounted IDs and checks the batch size", () => {
  const { bridge } = runtime();
  const instances = Array.from({ length: 6 }, (_, i) => ({ id: i === 3 ? "" : `agent-${i}` }));
  const state = { instancedGLBMeshes: [{ id: "crowd", instances }], _hydratedModelRecords: {} };
  const batches = bridge.decodeMotionFrame(partialFrame([1, 3, 5], 6));
  const seen = [];
  const result = bridge.applyMountedMotionFrame(state, batches, (s, b) => { seen.push(b[0].instances.map(i => i.id)); return true; }, () => {}, {});
  assert.equal(result.applied, true);
  assert.deepEqual(Array.from(seen[0]), ["agent-1", "instance-3", "agent-5"], "an unnamed agent is addressed the way hydration names it");
  const stale = bridge.decodeMotionFrame(partialFrame([1], 7));
  assert.throws(() => bridge.applyMountedMotionFrame(state, stale, () => true, () => {}, {}), /membership-changed/);
  const missing = bridge.decodeMotionFrame(partialFrame([1], 6, "other"));
  assert.throws(() => bridge.applyMountedMotionFrame(state, missing, () => true, () => {}, {}), /membership-changed/);
});

test("queued partial frames merge so no changed agent is dropped", async () => {
  const applied = [];
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands() { return gate; },
    applyMotionFrame(batches) { applied.push(batches); return { applied: true, binary: true }; },
  };
  const { bridge } = runtime(handle);
  // The first frame is already running behind a gated beforeCommands job, so
  // later frames queue.
  const first = bridge.dispatchMotionFrame(handle, partialFrame([0]), { beforeCommands: [{ kind: 5 }] });
  const a = bridge.dispatchMotionFrame(handle, partialFrame([1, 4]));
  const b = bridge.dispatchMotionFrame(handle, partialFrame([4, 7]));
  const full = bridge.dispatchMotionFrame(handle, frame());
  const c = bridge.dispatchMotionFrame(handle, partialFrame([9]));
  release();
  const results = await Promise.all([first, a, b, full, c]);
  assert.equal(results[1].superseded, true, "the older partial frame is folded into the newer one");
  assert.equal(results[0].applied, true);
  assert.deepEqual(applied.map(batches => batches[0].partial ? Array.from(batches[0].instances, i => i.slot) : "full"),
    [[0], [1, 4, 7], "full", [9]]);
  // Slot 4 carries the newer frame's motion key.
  assert.equal(applied[1][0].instances[1].prevX, 4);
});

test("a full frame does not supersede a queued partial frame", async () => {
  const applied = [];
  let release;
  const gate = new Promise(resolve => { release = resolve; });
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands() { return gate; },
    applyMotionFrame(batches) { applied.push(batches[0].partial ? "partial" : "full"); return { applied: true, binary: true }; },
  };
  const { bridge } = runtime(handle);
  const first = bridge.dispatchMotionFrame(handle, frame(), { beforeCommands: [{ kind: 5 }] });
  const partial = bridge.dispatchMotionFrame(handle, partialFrame([2]));
  const full = bridge.dispatchMotionFrame(handle, frame());
  release();
  await Promise.all([first, partial, full]);
  assert.deepEqual(applied, ["full", "partial", "full"]);
});
