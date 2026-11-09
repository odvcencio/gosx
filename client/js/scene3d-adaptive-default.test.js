"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const H = require("./runtime-test-harness.js");

async function mountQuality(extra = {}) {
  let now = 1000;
  const mount = new H.FakeElement("div", null);
  mount.id = "adaptive-default";
  const env = H.createContext({elements: [mount], enableWebGL2: true, disableCanvas2D: true,
    devicePixelRatio: 2, performanceNow: () => now,
    manifest: {engines: [{id: "adaptive-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: {width: 800, height: 600, responsive: false, autoRotate: true, adaptiveWarmupFrames: 0, ...extra}}]}});
  const raf = H.installManualRAF(env.context);
  H.runScript(H.bootstrapSource, env.context, "bootstrap.js");
  for (let i = 0; i < 12; i++) await H.flushAsyncWork();
  raf.flush(now); await H.flushAsyncWork();
  return {mount, canvas: mount.children[0], tick(ms) {now += ms; raf.flush(now);}, env};
}

test("adaptive quality defaults on at capability DPR and publishes the rendered DPR", async () => {
  const s = await mountQuality();
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-adaptive-quality"), "true");
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-tier"), "full");
  assert.equal(s.canvas.width, 1600);
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-dpr-cap"), "2.000");
});

test("explicit adaptive opt-out stays fixed and reports the actual DPR", async () => {
  const s = await mountQuality({adaptiveQuality: false});
  for (let i = 0; i < 40; i++) s.tick(60);
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-adaptive-quality"), "false");
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-tier"), "fixed");
  assert.equal(s.canvas.width, 1600);
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-dpr-cap"), "2.000");
});

test("rAF-only timing demotes under load and recovers on sustained 60 Hz cadence", async () => {
  const s = await mountQuality({adaptiveQuality: true});
  for (let i = 0; i < 40; i++) s.tick(60);
  const demoted = s.mount.getAttribute("data-gosx-scene3d-quality-active");
  console.log(`slow rAF tier: ${demoted}, DPR ${s.mount.getAttribute("data-gosx-scene3d-pixel-ratio")}`);
  assert.equal(demoted, "balanced");
  assert.equal(s.canvas.width, 1000, "default governor may reduce resolution");
  for (let i = 0; i < 480; i++) s.tick(1000 / 60);
  const recovered = s.mount.getAttribute("data-gosx-scene3d-quality-active");
  console.log(`healthy rAF tier after 480 frames: ${recovered}`);
  assert.equal(recovered, "full");
  assert.equal(s.canvas.width, 1600);
  assert.equal(s.env.consoleLogs.error.length, 0);
});

for (const [refreshHz, maxFrameRate] of [[90, 60], [120, 60], [144, 60], [120, 30], [144, 30]]) {
  test(`rAF recovery respects a ${maxFrameRate} FPS scene on a ${refreshHz} Hz display`, async () => {
    const s = await mountQuality({ maxFrameRate });
    for (let i = 0; i < 40; i++) s.tick(60);
    assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-active"), "balanced");
    // A 30 FPS cap needs time for both cooldowns and two recovery steps.
    for (let i = 0; i < refreshHz * (maxFrameRate === 30 ? 28 : 16); i++) s.tick(1000 / refreshHz);
    const q = s.mount.__gosxScene3DQualityState;
    assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-active"), "full", JSON.stringify({refreshHz, maxFrameRate, quality:q, pacing:s.mount.getAttribute("data-gosx-scene3d-frame-pacing-k")}));
    assert.equal(s.canvas.width, 1600);
    assert.equal(s.env.consoleLogs.error.length, 0);
  });
}

test("the authored quality ladder also recovers through capped high-refresh cadence", async () => {
  const s = await mountQuality({ maxFrameRate: 60, qualityLadder: H.THREE_RUNG_LADDER });
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-rung"), "0");
  for (let i = 0; i < 90 * 16; i++) s.tick(1000 / 90);
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-rung"), "2");
  assert.equal(s.canvas.width, 1600, "the ladder must preserve authored resolution");
  assert.equal(s.env.consoleLogs.error.length, 0);
});

for (const [name, limits, width, dpr] of [
  ["authored DPR", {maxDevicePixelRatio: 1.5}, 1200, "1.500"],
  ["backing-pixel budget", {maxPixels: 480000}, 800, "1.000"],
]) {
  test(`adaptive defaults preserve the ${name} limit and report the rendered DPR`, async () => {
    const s = await mountQuality(limits);
    assert.equal(s.canvas.width, width);
    assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-dpr-cap"), dpr);
  });
}

// Ported from salvage/perf/scene3d-adaptive-default onto the shared cadence
// model in mount-quality.ts, which both governors now use.
test("on-demand refresh gaps do not demote a static scene", async () => {
  const s = await mountQuality({autoRotate: false});
  for (let i = 0; i < 12; i++) {
    s.mount.__gosxScene3DHandle.applyCommands([{kind: 7, data: {background: "#123456"}}]);
    s.tick(2000); await H.flushAsyncWork();
  }
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-active"), "full");
  assert.equal(s.canvas.width, 1600);
});

for (const hz of [50, 48, 30]) {
  test(`rAF-only quality stays full at ${hz} Hz`, async () => {
    const s = await mountQuality();
    for (let i = 0; i < 600; i++) s.tick(1000 / hz);
    assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-active"), "full");
  });
}

test("rAF-only quality follows a display changing from 60 to 30 Hz", async () => {
  const s = await mountQuality();
  for (let i = 0; i < 120; i++) s.tick(1000 / 60);
  for (let i = 0; i < 600; i++) s.tick(1000 / 30);
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-active"), "full");
});

test("reversed promotions back off instead of repeatedly flapping", () => {
  const h = H.createAdaptiveQualityHarness({qualityTier: "full"});
  let transitions = 0;
  let last = h.state.activeTier;
  for (let i = 0; i < 3600; i++) {
    h.sample(h.state.activeTier === "full" ? 24 : 5, 1000 / 60);
    if (last !== h.state.activeTier) {transitions++; last = h.state.activeTier;}
  }
  console.log(`tier transitions over 60 seconds: ${transitions}`);
  assert.ok(transitions <= 7, `expected promotion backoff, got ${transitions} transitions`);
  assert.ok(h.state.promotionBackoff >= 2, "each reversed promotion doubles the next window");
});

test("a clean window at the requested tier clears the promotion backoff", () => {
  const h = H.createAdaptiveQualityHarness({qualityTier: "full"});
  h.state.promotionBackoff = 8;
  for (let i = 0; i < 400; i++) h.sample(5, 1000 / 60);
  assert.equal(h.state.activeTier, "full");
  assert.equal(h.state.promotionBackoff, 1);
});

test("scroll interaction holds rAF-only tier decisions", () => {
  const h = H.createAdaptiveQualityHarness({qualityTier: "full"});
  h.sceneState._scrollCamera = {_activeInputUntil: 1e9};
  for (let i = 0; i < 600; i++) {
    h.clock.now += 60;
    h.api.sceneUpdateAdaptiveQuality(h.state, h.mount, h.sceneState, {}, h.clock.now, h.clock.now, null);
  }
  assert.equal(h.state.activeTier, "full");
});

test("CPU-bound ticks do not raise the measured display budget", () => {
  const h = H.createAdaptiveQualityHarness({qualityTier: "full"});
  for (let i = 0; i < 40; i++) {
    h.clock.now += 40;
    h.api.sceneObserveAdaptiveQualityRAF(h.state, h.clock.now);
    h.api.sceneUpdateAdaptiveQuality(h.state, h.mount, h.sceneState, {}, h.clock.now - 30, h.clock.now, null);
  }
  assert.equal(h.state.activeTier, "balanced");
  assert.equal(h.state.vsyncIntervalMS, 0, "heavy frames never feed the display estimate");
});

test("GPU timing uses the measured 30 Hz display budget", () => {
  const h = H.createAdaptiveQualityHarness({qualityTier: "full"});
  for (let i = 0; i < 600; i++) h.sample(22, 1000 / 30);
  assert.equal(h.state.activeTier, "full");
  assert.ok(Math.abs(h.state.vsyncIntervalMS - 1000 / 30) < 0.5);
});

test("a long stall is not a sample, so quality returns after it clears", () => {
  const h = H.createAdaptiveQualityHarness({qualityTier: "full"});
  for (let i = 0; i < 60; i++) h.sample(5, 1000 / 60);
  // A 1.2 s stall (GC, upload, tab switch) arrives as one rAF interval.
  h.clock.now += 1200;
  h.api.sceneObserveAdaptiveQualityRAF(h.state, h.clock.now);
  h.api.sceneUpdateAdaptiveQuality(h.state, h.mount, h.sceneState, {}, h.clock.now - 2, h.clock.now, null);
  assert.equal(h.state.resumePending, false, "the stall resets the window and is consumed as a resume");
  assert.equal(h.state.badFrames, 0);
  assert.equal(h.state.activeTier, "full");
});

test("a demotion caused by sustained misses recovers once frames are clean", () => {
  const h = H.createAdaptiveQualityHarness({qualityTier: "full"});
  for (let i = 0; i < 40; i++) h.sample(40, 1000 / 60);
  assert.equal(h.state.activeTier, "balanced");
  for (let i = 0; i < 1200; i++) h.sample(5, 1000 / 60);
  assert.equal(h.state.activeTier, "full", "quality comes back after the load clears");
});

test("the ladder governor floors its budget at the measured display interval", () => {
  const h = H.createQualityLadderHarness(H.THREE_RUNG_LADDER, {qualityStartRung: 2});
  for (let i = 0; i < 400; i++) h.sample(22, 1000 / 30);
  assert.equal(h.state.rungIndex, 2, "30 Hz GPU frames inside the display interval are not misses");
});
