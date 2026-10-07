import test from "node:test";
import assert from "node:assert/strict";
import vm from "node:vm";
import { freshFeatureBundleSource } from "./runtime-test-harness.js";

const source = freshFeatureBundleSource("scene3d-command");
function runtime() {
  const context = vm.createContext({ window: {}, document: {}, TextDecoder, Uint8Array, ArrayBuffer, DataView, setTimeout });
  vm.runInContext(source, context);
  return context.window.__gosx_scene3d_command_bridge;
}

// Independently encode both wire layouts, keeping the frame envelope identical.
function frame(motion, options = {}) {
  const bytes = [71, 83, 80, motion ? 51 : 50], encoder = new TextEncoder();
  const u16 = value => bytes.push(value & 255, value >> 8);
  const id = value => { const data = typeof value === "string" ? encoder.encode(value) : value; u16(data.length); bytes.push(...data); };
  const number = value => { const data = new DataView(new ArrayBuffer(4)); data.setFloat32(0, value, true); bytes.push(...new Uint8Array(data.buffer)); };
  const clips = options.clips || ["run"];
  u16(clips.length); clips.forEach(id);
  const batches = options.batches || ["heroes"], instances = options.instances || ["first", "last"];
  u16(batches.length);
  for (const batch of batches) {
    id(batch); u16(instances.length);
    for (let index = 0; index < instances.length; index++) {
      id(instances[index]);
      number(index === instances.length - 1 ? (options.scalar ?? 1) : 1);
      for (const value of [2, 3, 0, 0.5, 0, 1, 1, 1]) number(value);
      if (motion) {
        number(10);
        for (const value of [4, 5, 6, 0, 1, 0, 2, 2, 2]) number(value);
        number(options.nextTime ?? 11);
      } else number(options.animationTime ?? 0.25);
      u16(options.clipIndex ?? 1);
      if (motion) number(options.clipStartTime ?? -2);
      bytes.push(options.loop ?? 1);
      if (motion) number(options.playbackRate ?? -1);
    }
  }
  return Uint8Array.from(bytes);
}

for (const motion of [false, true]) {
  const name = motion ? "Motion" : "Pose", version = motion ? "GSP3" : "GSP2";
  test(version + " validates the complete envelope before invoking a renderer", async () => {
    const bridge = runtime(), decode = bridge["decode" + name + "Frame"], valid = frame(motion);
    const malformed = [
      [frame(motion, { clips: ["run", "run"] }), /duplicate.*clip/],
      [frame(motion, { clips: [""] }), /empty.*ID/],
      [frame(motion, { clips: [[0xff]] }), /encoded data/],
      [frame(motion, { batches: ["heroes", "heroes"] }), /duplicate.*batch ID/],
      [frame(motion, { instances: ["first", "first"] }), /duplicate.*instance ID/],
      [frame(motion, { clipIndex: 2 }), /unknown.*clip index/],
      [frame(motion, { loop: 2 }), /loop flag/],
      [frame(motion, { scalar: NaN }), /non-finite/],
      [frame(motion, { scalar: Infinity }), /non-finite/],
      [Uint8Array.from([...valid, 0]), /trailing/],
      [frame(!motion), /frame version/],
    ];
    if (motion) malformed.push([frame(true, { nextTime: 10 }), /tNext after tPrev/], [frame(true, { clipStartTime: NaN }), /non-finite/], [frame(true, { playbackRate: Infinity }), /non-finite/]);
    else malformed.push([frame(false, { animationTime: -1 }), /negative.*animation time/]);
    for (let length = 0; length < valid.length; length++) assert.throws(() => decode(valid.subarray(0, length)), /truncated/);
    for (const input of [null, [], new Uint16Array(4)]) assert.throws(() => decode(input), /ArrayBuffer or Uint8Array/);
    let applications = 0;
    const handle = { __gosxScene3DCommandReady: true, applyCommands() { applications++; }, ["apply" + name + "Frame"]() { applications++; } };
    for (const [input, reason] of malformed) {
      assert.throws(() => decode(input), reason);
      await assert.rejects(bridge["dispatch" + name + "Frame"](handle, input), reason);
    }
    assert.equal(applications, 0, "even late malformed data must prevent all renderer calls");
  });

  test(version + " respects typed-array bounds and preserves its distinct instance fields", () => {
    const decode = runtime()["decode" + name + "Frame"], bytes = frame(motion), padded = new Uint8Array(bytes.length + 16);
    padded.fill(255); padded.set(bytes, 8);
    const expected = JSON.parse(JSON.stringify(decode(bytes.buffer)));
    assert.deepEqual(JSON.parse(JSON.stringify(decode(padded.subarray(8, 8 + bytes.length)))), expected);
    const instance = expected[0].instances[1];
    assert.equal(instance.id, "last"); assert.equal(instance.animation, "run"); assert.equal(instance.animationLoop, true);
    if (motion) {
      assert.equal(instance.prevY, 2); assert.equal(instance.nextY, 5);
      assert.equal(instance.clipStartTime, -2); assert.equal(instance.playbackRate, -1);
      assert.equal("animationTime" in instance, false);
    } else {
      assert.equal(instance.y, 2); assert.equal(instance.animationTime, 0.25);
      assert.equal("playbackRate" in instance, false);
    }
  });
}
