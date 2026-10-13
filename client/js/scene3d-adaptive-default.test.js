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
