"use strict";
// Adaptive frame pacing ("vsync-divisor").
//
// scheduleNextAnimationFrame's default gate skips a requestAnimationFrame
// tick by a fixed millisecond threshold (frameIntervalMS/MaxFrameRate/
// MaxFPS). A fixed threshold cannot divide every display's refresh rate
// evenly: a 100 Hz display capped at a 20ms interval (MaxFrameRate 50)
// accepts an uneven 40-48 fps instead of an exact 50. The opt-in
// props.framePacing === "vsync-divisor" policy renders on every k-th real
// display tick -- a tick COUNTER, not a millisecond threshold -- so the
// paced rate is an exact fraction of the display's own refresh rate.
//
// These tests drive the pure sceneFramePacing* decision helpers (see
// mount.ts) directly with a simulated rAF clock: no DOM, no renderer,
// mirroring the createQualityLadderRAFHarness pattern the sibling quality-
// ladder governor already uses (see runtime-11-scene-adaptive-quality.test.js).

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  loadSceneFramePacingAPI,
  readSceneMountSrc,
  bootstrapSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  installManualRAF,
} = require("./runtime-test-harness.js");

function initialFramePacingState() {
  return {
    t1: 0, t2: 0, t3: 0,
    vsyncEstimateMS: 0,
    costEstimateMS: 0,
    activeK: 1,
    pendingK: 1,
    pendingStreak: 0,
    ticksSinceRender: 0,
  };
}

// runFramePacingTicks replays `ticks` perfectly regular display ticks
// (1000 / refreshHz apart) through the same per-tick glue
// scheduleNextAnimationFrame runs in mount.ts: median-of-3 the raw deltas,
// advance the governor, and -- only on a tick that actually renders --
// fold costMS into the running cost estimate. Takes and returns the full
// state so a test can run a warm-up phase and then measure a settled tail.
function runFramePacingTicks(api, state, { refreshHz, costMS, minIntervalMS = 0 }, ticks) {
  const tickDeltaMS = 1000 / refreshHz;
  const next = Object.assign({}, state);
  let renderedTicks = 0;
  for (let i = 0; i < ticks; i += 1) {
    next.t3 = next.t2;
    next.t2 = next.t1;
    next.t1 = tickDeltaMS;
    const vsyncSampleMS = api.sceneFramePacingMedianOf3(next.t1, next.t2, next.t3);
    const result = api.sceneFramePacingAdvanceOnTick(
      vsyncSampleMS, next.vsyncEstimateMS, next.costEstimateMS, next.activeK,
      next.pendingK, next.pendingStreak, next.ticksSinceRender, minIntervalMS,
    );
    next.vsyncEstimateMS = result.vsyncEstimateMS;
    next.activeK = result.activeK;
    next.pendingK = result.pendingK;
    next.pendingStreak = result.pendingStreak;
    next.ticksSinceRender = result.ticksSinceRender;
    if (result.shouldRender) {
      renderedTicks += 1;
      next.costEstimateMS = api.sceneFramePacingBlendCost(next.costEstimateMS, costMS);
    }
  }
  return { state: next, renderedTicks, ticks };
}

test("with no vsync/cost data yet, k defaults to 1 and renders every tick", () => {
  const { api } = loadSceneFramePacingAPI();
  const result = api.sceneFramePacingAdvanceOnTick(0, 0, 0, 1, 1, 0, 0, 0);
  assert.equal(result.activeK, 1);
  assert.equal(result.shouldRender, true);
});

test("60 Hz display, 10 ms render cost settles at k=1 (an unpaced 60 fps)", () => {
  const { api } = loadSceneFramePacingAPI();
  const { state } = runFramePacingTicks(api, initialFramePacingState(), { refreshHz: 60, costMS: 10 }, 60);
  assert.equal(state.activeK, 1);
  assert.ok(Math.abs(state.vsyncEstimateMS - 1000 / 60) < 0.01, `vsync estimate ${state.vsyncEstimateMS} should read ~16.67ms`);
});

test("60 Hz display, 20 ms render cost settles at k=2 (an even 30 fps)", () => {
  const { api } = loadSceneFramePacingAPI();
  const { state } = runFramePacingTicks(api, initialFramePacingState(), { refreshHz: 60, costMS: 20 }, 60);
  assert.equal(state.activeK, 2);
  // Once k has committed, a further run of even length renders EXACTLY
  // half the ticks -- the "tick counter, not a millisecond threshold"
  // contract produces an exact ratio, not an approximation.
  const tail = runFramePacingTicks(api, state, { refreshHz: 60, costMS: 20 }, 40);
  assert.equal(tail.state.activeK, 2);
  assert.equal(tail.renderedTicks, 20);
});

test("100 Hz display, 12 ms render cost settles at k=2 (an even 50 fps -- the original report)", () => {
  const { api } = loadSceneFramePacingAPI();
  const { state } = runFramePacingTicks(api, initialFramePacingState(), { refreshHz: 100, costMS: 12 }, 60);
  assert.equal(state.activeK, 2);
  const tail = runFramePacingTicks(api, state, { refreshHz: 100, costMS: 12 }, 40);
  assert.equal(tail.renderedTicks, 20);
});

test("144 Hz display, 8 ms render cost settles at k=2 (72 fps)", () => {
  const { api } = loadSceneFramePacingAPI();
  const { state } = runFramePacingTicks(api, initialFramePacingState(), { refreshHz: 144, costMS: 8 }, 60);
  assert.equal(state.activeK, 2);
  const tail = runFramePacingTicks(api, state, { refreshHz: 144, costMS: 8 }, 40);
  assert.equal(tail.renderedTicks, 20);
});

test("hysteresis: a raw candidate alternating every tick never accumulates enough streak to commit", () => {
  const { api } = loadSceneFramePacingAPI();
  let activeK = 1;
  let pendingK = 1;
  let pendingStreak = 0;
  for (let i = 0; i < 40; i += 1) {
    const candidateK = i % 2 === 0 ? 1 : 2;
    const observed = api.sceneFramePacingObserveCandidate(pendingK, pendingStreak, candidateK);
    pendingK = observed.pendingK;
    pendingStreak = observed.pendingStreak;
    activeK = api.sceneFramePacingCommitK(activeK, pendingK, pendingStreak);
  }
  assert.equal(activeK, 1, "an alternating candidate must never flap the active k");
});

test("hysteresis: a new candidate that holds steady for the commit streak does take effect", () => {
  const { api } = loadSceneFramePacingAPI();
  let activeK = 1;
  let pendingK = 1;
  let pendingStreak = 0;
  for (let i = 0; i < 20; i += 1) {
    const observed = api.sceneFramePacingObserveCandidate(pendingK, pendingStreak, 2);
    pendingK = observed.pendingK;
    pendingStreak = observed.pendingStreak;
    activeK = api.sceneFramePacingCommitK(activeK, pendingK, pendingStreak);
  }
  assert.equal(activeK, 2, "a candidate stable past the commit streak must take effect");
});

test("end-to-end: a cost estimate hovering exactly on a k boundary never flaps the paced rate", () => {
  const { api } = loadSceneFramePacingAPI();
  const vsyncMS = 1000 / 60;
  let activeK = 1;
  let pendingK = 1;
  let pendingStreak = 0;
  let ticksSinceRender = 0;
  for (let i = 0; i < 60; i += 1) {
    // 14ms is comfortably inside k=1 (16.67 >= 14*1.1=15.4); 16ms is just
    // past the k=1/k=2 boundary (16.67 < 16*1.1=17.6, so k=2 there). Real
    // measurement noise this close to the boundary must not visibly change
    // the paced rate frame to frame.
    const costEstimateMS = i % 2 === 0 ? 14 : 16;
    const result = api.sceneFramePacingAdvanceOnTick(
      vsyncMS, vsyncMS, costEstimateMS, activeK, pendingK, pendingStreak, ticksSinceRender, 0,
    );
    activeK = result.activeK;
    pendingK = result.pendingK;
    pendingStreak = result.pendingStreak;
    ticksSinceRender = result.ticksSinceRender;
  }
  assert.equal(activeK, 1);
});

test("an authored MaxFrameRate/frameIntervalMS cap bounds k upward even when render cost is cheap", () => {
  const { api } = loadSceneFramePacingAPI();
  const minIntervalMS = 1000 / 30; // MaxFrameRate: 30, on a 60 Hz display.
  const { state } = runFramePacingTicks(
    api, initialFramePacingState(), { refreshHz: 60, costMS: 5, minIntervalMS }, 60,
  );
  // Render cost alone (5ms) settles at k=1 -- comfortably inside one
  // 16.67ms tick -- but the authored cap forces k=2 so the paced rate
  // never exceeds 30 fps.
  assert.equal(state.activeK, 2);
});

test("with no authored cap, the same cheap render settles back at k=1", () => {
  const { api } = loadSceneFramePacingAPI();
  const { state } = runFramePacingTicks(api, initialFramePacingState(), { refreshHz: 60, costMS: 5 }, 60);
  assert.equal(state.activeK, 1);
});

test("a low authored cap raises k past 4 instead of exceeding the cap", () => {
  // Regression: an earlier version capped the authored-interval floor at
  // k=4 (the same range as the render-cost decision), so a 60 Hz display
  // with MaxFrameRate 10 (needs 6 ticks per render) rendered every 4th
  // tick instead -- 15 fps, 50% over the authored maximum.
  const { api } = loadSceneFramePacingAPI();
  const vsyncMS = 1000 / 60;
  const minIntervalMS = 1000 / 10; // MaxFrameRate: 10.
  assert.equal(api.sceneFramePacingMinKForInterval(vsyncMS, minIntervalMS), 6);
  const { state } = runFramePacingTicks(
    api, initialFramePacingState(), { refreshHz: 60, costMS: 5, minIntervalMS }, 60,
  );
  assert.equal(state.activeK, 6);
  const tail = runFramePacingTicks(api, state, { refreshHz: 60, costMS: 5, minIntervalMS }, 60);
  assert.equal(tail.renderedTicks, 10, "60 ticks at k=6 must render exactly 10 times");
});

test("sceneFramePacingMinKForInterval is never bounded to 4 the way the render-cost decision is", () => {
  const { api } = loadSceneFramePacingAPI();
  const vsyncMS = 1000 / 60;
  // A pathologically low cap (MaxFrameRate 1) must still compute the
  // exact divisor, not silently clamp to whatever the cost-based range
  // supports.
  assert.equal(api.sceneFramePacingMinKForInterval(vsyncMS, 1000 / 1), 60);
});

test("the default frame cap and opt-in divisor use separate gates", () => {
  const source = readSceneMountSrc();
  assert.match(source, /sceneAnimationFrameGate\(now, lastAnimationFrameAt, interval, animationIntervalMS\)/);
  assert.match(source, /const framePacingEnabled = framePacingMode === "vsync-divisor";/);
});

function framePacingManifest(extraProps) {
  return {
    engines: [
      {
        id: "gosx-engine-frame-pacing",
        component: "GoSXScene3D",
        kind: "surface",
        mountId: "scene-frame-pacing-root",
        jsExport: "GoSXScene3D",
        props: Object.assign(
          {
            width: 480,
            height: 300,
            autoRotate: true,
            scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, x: 0, y: 0, z: 0, color: "#8de1ff" }] },
          },
          extraProps || {},
        ),
        capabilities: ["canvas", "animation"],
      },
    ],
  };
}

test("framePacing absent: the mount gains no data-gosx-scene3d-frame-pacing* attributes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-frame-pacing-root";
  const env = createContext({ elements: [mount], manifest: framePacingManifest() });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();
  raf.flush(32);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-frame-pacing"), null);
  assert.equal(mount.getAttribute("data-gosx-scene3d-frame-pacing-k"), null);
});

test("framePacing: vsync-divisor publishes the governor's k/vsync/cost telemetry", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-frame-pacing-root";
  const env = createContext({
    elements: [mount],
    manifest: framePacingManifest({ framePacing: "vsync-divisor" }),
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();
  raf.flush(32);
  await flushAsyncWork();

  // Drive a steady simulated 60 Hz cadence so the vsync estimate locks on.
  let t = 32;
  for (let i = 0; i < 20; i += 1) {
    t += 16.6667;
    raf.flush(t);
    await flushAsyncWork();
  }

  assert.equal(mount.getAttribute("data-gosx-scene3d-frame-pacing"), "vsync-divisor");
  const k = Number(mount.getAttribute("data-gosx-scene3d-frame-pacing-k"));
  assert.ok(Number.isInteger(k) && k >= 1 && k <= 4, `k attribute should be an integer in [1,4], got ${mount.getAttribute("data-gosx-scene3d-frame-pacing-k")}`);
  const vsyncMs = Number(mount.getAttribute("data-gosx-scene3d-frame-pacing-vsync-ms"));
  assert.ok(vsyncMs > 10 && vsyncMs < 25, `vsync-ms attribute should read close to 16.67ms, got ${vsyncMs}`);
});
