import { createRequire } from "node:module";
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

// Timed pose frames (client/runtime/scene3d/pose-interp.ts): the page keeps
// the newest frames and applies interpolated poses from the mount's frame hook.

const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const transpile = (source) => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const directory = path.dirname(fileURLToPath(import.meta.url));
const read = (name) => fs.readFileSync(path.join(directory, "..", "runtime", "scene3d", name), "utf8");

function runtime() {
  const clock = { now: 0 };
  const context = {
    window: {}, document: {}, TextDecoder, Uint8Array, ArrayBuffer, DataView, Set, Map, WeakMap, Date, Promise, setTimeout, Math, Number,
    performance: { now: () => clock.now },
  };
  vm.createContext(context);
  vm.runInContext(transpile(read("pose-interp.ts")), context);
  vm.runInContext(transpile(read("command-runtime.ts")), context);
  return { clock, bridge: context.window.__gosx_scene3d_command_bridge, api: context.window.__gosx_scene3d_pose_interpolation };
}

// frame encodes one GSP2 frame: batch "units" with the given instances.
function frame(instances) {
  const bytes = [];
  const encoder = new TextEncoder();
  const id = (value) => { const chars = encoder.encode(value); bytes.push(chars.length & 255, chars.length >> 8, ...chars); };
  const u16 = (value) => bytes.push(value & 255, value >> 8);
  const f32 = (value) => { const data = new DataView(new ArrayBuffer(4)); data.setFloat32(0, value, true); bytes.push(...new Uint8Array(data.buffer)); };
  const clips = [...new Set(instances.map((i) => i.clip))];
  bytes.push(71, 83, 80, 50);
  u16(clips.length);
  clips.forEach(id);
  u16(1);
  id("units");
  u16(instances.length);
  for (const i of instances) {
    id(i.id);
    for (const v of [i.x, 0, 0, 0, i.ry || 0, 0, 1, 1, 1, i.t || 0]) f32(v);
    u16(clips.indexOf(i.clip) + 1);
    bytes.push(1);
  }
  return Uint8Array.from(bytes);
}

function mount() {
  const applied = [];
  const handle = {
    __gosxScene3DCommandReady: true,
    applyCommands() { throw new Error("no commands expected"); },
    applyPoseFrame(batches) { applied.push(JSON.parse(JSON.stringify(batches))); return { applied: true, binary: true }; },
    setFrameHook(fn) { handle.hook = fn; return () => { handle.hook = null; }; },
  };
  return { handle, applied };
}

test("timed frames install the frame hook and the hook interpolates between ticks", async () => {
  const { clock, bridge } = runtime();
  const { handle, applied } = mount();
  clock.now = 1000;
  const r0 = await bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: 0, clip: "Run", t: 1 }]), { at: 1000, step: 100 });
  assert.equal(r0.timed, true);
  assert.equal(typeof handle.hook, "function", "the first timed frame takes the mount's frame hook");
  clock.now = 1100;
  await bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: 10, clip: "Run", t: 2 }]), { at: 1100, step: 100 });
  assert.equal(applied.length, 0, "frames are not applied on arrival");
  // offset = min(arrival - at) = 0; delay = step + 2 = 102. Draw at 1152 -> renderAt 1050.
  const alpha = handle.hook(1152, 16);
  assert.ok(Math.abs(alpha - 0.5) < 1e-9, `alpha ${alpha}`);
  const pose = applied.at(-1)[0].instances[0];
  assert.ok(Math.abs(pose.x - 5) < 1e-6, `x ${pose.x}`);
  assert.ok(Math.abs(pose.animationTime - 1.5) < 1e-6, `clip time ${pose.animationTime}`);
  assert.equal(pose.animation, "Run");
  const stats = handle.__gosxTimedPoseStats;
  assert.equal(stats.frames, 2);
  assert.equal(stats.applied, 1);
  assert.equal(stats.delayMS, 102);
});

test("rotation takes the shortest arc and a clip change switches at once", async () => {
  const { clock, bridge } = runtime();
  const { handle, applied } = mount();
  clock.now = 0;
  await bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: 0, ry: 3.0, clip: "Run", t: 5 }]), { at: 0, step: 50 });
  clock.now = 50;
  await bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: 0, ry: -3.0, clip: "Idle", t: 0.2 }]), { at: 50, step: 50 });
  handle.hook(25 + 52, 16); // renderAt 25: halfway
  const pose = applied.at(-1)[0].instances[0];
  // 3.0 -> -3.0 the short way crosses pi: halfway is about +/-pi, not 0.
  assert.ok(Math.abs(Math.abs(pose.rotationY) - Math.PI) < 1e-3, `rotationY ${pose.rotationY}`);
  assert.equal(pose.animation, "Idle");
  assert.ok(Math.abs(pose.animationTime - 0.2) < 1e-6);
});

test("the hook holds the newest pose when frames stop, and a rewound frame replaces newer ones", async () => {
  const { clock, bridge } = runtime();
  const { handle, applied } = mount();
  for (let i = 0; i < 3; i++) {
    clock.now = i * 100;
    await bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: i, clip: "Run" }]), { at: i * 100, step: 100 });
  }
  handle.hook(900, 16);
  assert.equal(applied.at(-1)[0].instances[0].x, 2);
  assert.equal(handle.__gosxTimedPoseStats.holds, 1);
  clock.now = 300;
  await bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: 7, clip: "Run" }]), { at: 100, step: 100 });
  assert.equal(handle.__gosxTimedPoseStats.rewinds, 1);
  handle.hook(900, 16);
  assert.equal(applied.at(-1)[0].instances[0].x, 7, "frames after the rewound time are gone");
});

test("a released hook drops its frames, and bad times reject", async () => {
  const { bridge } = runtime();
  const { handle, applied } = mount();
  await bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: 1, clip: "Run" }]), { at: 0, step: 10 });
  handle.hook(0, 0, true);
  await bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: 2, clip: "Run" }]), { at: 5, step: 10 });
  handle.hook(1000, 16);
  assert.equal(applied.at(-1)[0].instances[0].x, 2);
  await assert.rejects(bridge.dispatchPoseFrame(handle, frame([{ id: "a", x: 1, clip: "Run" }]), { at: 1, step: 0 }), /step > 0/);
});

test("blending reuses its output, so a drawn frame allocates nothing", () => {
  const { api } = runtime();
  const a = [{ id: "units", instances: [{ id: "a", x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, scaleX: 1, scaleY: 1, scaleZ: 1, animation: "Run", animationTime: 0, animationLoop: true }] }];
  const b = [{ id: "units", instances: [{ ...a[0].instances[0], x: 4, animationTime: 1 }] }];
  const out = [];
  const first = api.blend(a, b, 0.25, out);
  const firstInstance = first[0].instances[0];
  const second = api.blend(a, b, 0.75, out);
  assert.equal(second, out);
  assert.equal(second[0].instances[0], firstInstance, "the same instance object is reused");
  assert.equal(second[0].instances[0].x, 3);
});
