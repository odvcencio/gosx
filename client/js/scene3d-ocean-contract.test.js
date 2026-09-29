"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { createBoardWebGPUHarness } = require("./runtime-test-harness.js");

test("ocean normalization applies defaults, clamps values, wraps direction, and validates bathymetry", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const plain = value => JSON.parse(JSON.stringify(value));
  assert.equal(api.normalizeSceneOcean(null), null);
  assert.equal(api.normalizeSceneOcean("ocean"), null);
  assert.deepEqual(plain(api.normalizeSceneOcean({})), {
    level: 0, windDirection: 0, waveHeight: 0.8, waveLength: 18, choppiness: 0.6, speed: 1,
    deepColor: "#03141f", shallowColor: "#1f6f78", scatterColor: "#2fa58f", foamColor: "#e9eef0",
    clarity: 4, roughness: 0.06, foam: 0.6, surf: 0.5, extent: 4000, bathymetry: null,
  });
  const clamped = api.normalizeSceneOcean({
    level: 3, windDirection: -725, waveHeight: 99, waveLength: -1, choppiness: -1, speed: 9,
    deepColor: "  #112233  ", shallowColor: " ", scatterColor: " #aabbcc ", foamColor: "#123456",
    clarity: 0, roughness: 2, foam: -2, surf: 3, extent: 0,
  });
  assert.deepEqual(plain(clamped), {
    level: 3, windDirection: 355, waveHeight: 6, waveLength: 2, choppiness: 0, speed: 4,
    deepColor: "#112233", shallowColor: "#1f6f78", scatterColor: "#aabbcc", foamColor: "#123456",
    clarity: 4, roughness: 0.5, foam: 0, surf: 1, extent: 4000, bathymetry: null,
  });
  assert.equal(api.normalizeSceneOcean({ windDirection: 1085 }).windDirection, 5);
  assert.equal(api.normalizeSceneOcean({ windDirection: 360 }).windDirection, 0);

  const validBathymetry = { src: " map.png ", minX: -10, minZ: -20, maxX: 10, maxZ: 20, minHeight: -5, maxHeight: 2 };
  assert.deepEqual(plain(api.normalizeSceneOcean({ bathymetry: validBathymetry }).bathymetry),
    { src: "map.png", minX: -10, minZ: -20, maxX: 10, maxZ: 20, minHeight: -5, maxHeight: 2 });
  for (const bathymetry of [
    { ...validBathymetry, src: " " },
    { ...validBathymetry, maxX: -10 },
    { ...validBathymetry, maxZ: -20 },
    { ...validBathymetry, maxHeight: -5 },
    null,
  ]) {
    assert.equal(api.normalizeSceneOcean({ bathymetry }).bathymetry, null);
  }

  const environment = api.normalizeSceneEnvironment({ ocean: { waveHeight: 2 } });
  assert.equal(environment.ocean.waveHeight, 2);
  const updated = api.normalizeSceneEnvironment({ exposure: 2 }, environment);
  assert.equal(updated.ocean.waveHeight, 2, "an unrelated environment update keeps the ocean");
  assert.equal(api.normalizeSceneEnvironment({ ocean: null }, updated).ocean, null,
    "an explicit null removes the ocean");
  h.renderer.dispose();
});
