"use strict";
// The frame meter (frame-owner.ts): the mount's last 120 drawn frames, read by
// handle.getFrameStats and Go's scene.Surface.FrameStats for a readout a
// player can turn on. These tests run the source as plain JavaScript; the
// last one runs the built bootstrap (regenerate it with cmd/buildbootstrap
// after a source change).
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const {
  bootstrapSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  installManualRAF,
  flushSceneInitialFrameBoundary,
} = require("./runtime-test-harness.js");

// The fields getFrameStats returns; Go's scene.FrameStats decodes the same
// names (scene/surface_framestats_test.go).
const FIELDS = ["backend", "adapter", "fallbackReason", "tier", "governed", "frames", "fps", "frameMS", "frameP95MS",
  "cpuMS", "cpuP95MS", "gpuMS", "gpuP95MS", "gpuSamples", "gpuTimer", "pixelRatio", "width", "height"];

function meter() {
  const context = vm.createContext({});
  vm.runInContext(fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "frame-owner.ts"), "utf8"), context,
    { filename: "frame-owner.ts" });
  return { context, owner: context.createSceneFrameOwner() };
}

const truth = { backend: "webgpu", adapter: "Example GPU", fallbackReason: "" };
const viewport = { devicePixelRatio: 1.35, cssWidth: 1920, cssHeight: 1080 };

test("the meter reports frames a second, main-thread work and GPU time over its window", () => {
  const { context, owner } = meter();
  const quality = { enabled: true, activeTier: "balanced", rendererSample: null };
  for (let i = 0; i < 200; i++) {
    const at = 1000 + i * (1000 / 60);
    // The governor polled a GPU timestamp on every third frame.
    quality.rendererSample = i % 3 === 0 ? { source: "gpu-timestamp", rendererDurationMS: 6 + (i % 2) } : null;
    context.sceneFrameMeterRecord(owner, at, at, at + 3, quality);
    assert.equal(quality.rendererSample, null, "the meter takes the governor's sample");
  }
  const stats = context.sceneFrameMeterStats(owner, truth, quality, viewport);
  assert.deepEqual(Object.keys(stats), FIELDS);
  assert.equal(stats.frames, 120, "the window holds the last 120 frames");
  assert.ok(Math.abs(stats.fps - 60) < 0.01, "60 frames a second, got " + stats.fps);
  assert.ok(Math.abs(stats.frameMS - 1000 / 60) < 0.01);
  assert.equal(stats.cpuMS, 3);
  assert.equal(stats.gpuSamples, 40);
  assert.equal(stats.gpuMS, 6.5);
  assert.equal(stats.gpuP95MS, 7);
  assert.equal(stats.gpuTimer, "gpu-timestamp");
  assert.equal(stats.tier, "balanced");
  assert.equal(stats.governed, true);
  assert.equal(stats.backend, "webgpu");
  assert.equal(stats.adapter, "Example GPU");
  assert.equal(stats.pixelRatio, 1.35);
  assert.equal(stats.width, 1920);
  assert.equal(stats.height, 1080);
});

test("a slow frame shows in the 95th percentile, and only GPU timers count as GPU time", () => {
  const { context, owner } = meter();
  const quality = { mode: "ladder", rungName: "low", enabled: true, rendererSample: null };
  let at = 0;
  for (let i = 0; i < 100; i++) {
    at += i % 20 === 19 ? 50 : 1000 / 60;
    // A CPU-bound frame keeps the GPU's own time (cpu-work+); the governor's
    // rAF fallback (cpu-raf) is no GPU time.
    quality.rendererSample = i % 2 === 0 ? { source: "cpu-work+webgl-timer", rendererDurationMS: 9 } : { source: "cpu-raf", rendererDurationMS: 0, durationMS: 16 };
    context.sceneFrameMeterRecord(owner, at, at - 2, at + 10, quality);
  }
  const stats = context.sceneFrameMeterStats(owner, { backend: "webgl", adapter: "", fallbackReason: "webgpu-unavailable" }, quality, {});
  assert.equal(stats.frames, 100);
  assert.equal(stats.frameP95MS, 50, "the 50 ms frames are the slowest 5 %");
  assert.ok(stats.fps < 60 && stats.fps > 45, "fps counts the slow frames, got " + stats.fps);
  assert.equal(stats.cpuMS, 12);
  assert.equal(stats.gpuSamples, 50);
  assert.equal(stats.gpuMS, 9);
  assert.equal(stats.gpuTimer, "webgl-timer");
  assert.equal(stats.tier, "low", "a ladder reports its rung");
  assert.equal(stats.fallbackReason, "webgpu-unavailable");
});

test("an empty meter reads zeros", () => {
  const { context, owner } = meter();
  const stats = context.sceneFrameMeterStats(owner, null, null, null);
  assert.deepEqual(Object.keys(stats), FIELDS);
  assert.equal(stats.frames, 0);
  assert.equal(stats.fps, 0);
  assert.equal(stats.gpuTimer, "");
  assert.equal(stats.tier, "");
  assert.equal(stats.governed, false);
});

test("the mount handle's getFrameStats counts the frames an engine hook drives", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-frame-meter-root";
  const env = createContext({ elements: [mount], manifest: { engines: [{
    id: "gosx-engine-frame-meter", component: "GoSXScene3D", kind: "surface", mountId: "scene-frame-meter-root", jsExport: "GoSXScene3D",
    props: { width: 320, height: 200, autoRotate: false, scene: { points: [{ id: "still", count: 2, positions: [0, 0, 0, 1, 0, 0], sizes: [2, 2], color: "#ffffff" }] } },
    capabilities: ["canvas", "animation"],
  }] } });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  const handle = mount.__gosxScene3DHandle;
  assert.equal(typeof handle.getFrameStats, "function");
  const before = handle.getFrameStats().frames;
  handle.setFrameHook(function() { return 0; });
  for (let i = 1; i <= 30; i++) {
    raf.flush(1000 + i * 20);
    await flushAsyncWork();
  }
  const stats = JSON.parse(JSON.stringify(handle.getFrameStats()));
  assert.deepEqual(Object.keys(stats), FIELDS);
  assert.equal(stats.frames, Math.min(120, before + 30), "every hook frame is counted");
  assert.ok(stats.fps > 0 && stats.fps <= 50, "frames 20 ms apart, got " + stats.fps);
  assert.ok(stats.cpuMS >= 0);
});
