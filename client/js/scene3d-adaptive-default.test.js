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
  assert.equal(h.state.activeTier, "balanced");
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

test("transition events publish the DPR applied for the new tier", async () => {
  const s = await mountQuality();
  const events = [];
  s.env.context.__gosx_emit = (level, source, message, fields) => {
    if (message === "adaptive-quality-transition") events.push({...fields});
  };
  for (let i = 0; i < 40; i++) s.tick(60);
  assert.equal(events.length, 1);
  assert.equal(events[0].activeTier, "balanced");
  assert.equal(events[0].dprCap, 1.25);
  assert.equal(events[0].dprCap, s.canvas.width / 800);
});

test("rAF-only quality follows a display changing from 60 to 30 Hz", async () => {
  const s = await mountQuality();
  for (let i = 0; i < 120; i++) s.tick(1000 / 60);
  for (let i = 0; i < 600; i++) s.tick(1000 / 30);
  assert.equal(s.mount.getAttribute("data-gosx-scene3d-quality-active"), "full");
});

test("CPU-bound ticks do not raise the measured display budget", () => {
  const h = H.createAdaptiveQualityHarness({qualityTier: "full"});
  for (let i = 0; i < 40; i++) {
    h.clock.now += 40;
    h.api.sceneObserveAdaptiveQualityRAF(h.state, h.clock.now);
    h.api.sceneUpdateAdaptiveQuality(h.state, h.mount, h.sceneState, {}, h.clock.now - 30, h.clock.now, null);
  }
  assert.equal(h.state.activeTier, "balanced");
});
