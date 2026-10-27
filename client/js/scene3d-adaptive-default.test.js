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
