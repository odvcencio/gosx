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
    { src: "map.png", minX: -10, minZ: -20, maxX: 10, maxZ: 20, minHeight: -5, maxHeight: 2, encoding: "linear" });
  assert.equal(api.normalizeSceneOcean({ bathymetry: { ...validBathymetry, encoding: " Signed-Sqrt " } }).bathymetry.encoding, "signed-sqrt");
  assert.equal(api.normalizeSceneOcean({ bathymetry: { ...validBathymetry, encoding: "log" } }).bathymetry.encoding, "linear");
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
  const clampedEnvironment = api.normalizeSceneEnvironment({ ocean: { foam: -1, speed: -1, choppiness: -1, surf: -1 } });
  const preserved = api.normalizeSceneEnvironment({ exposure: 2 }, clampedEnvironment);
  for (const key of ["foam", "speed", "choppiness", "surf"]) {
    assert.equal(preserved.ocean[key], 0, `unrelated update preserves clamped ${key}`);
  }
  const state = api.createSceneState({ scene: { environment: { ocean: { foam: -1, speed: -1, choppiness: -1, surf: -1 } } } });
  api.applySceneCommands(state, [{ kind: 13, data: { exposure: 2 } }]);
  for (const key of ["foam", "speed", "choppiness", "surf"]) assert.equal(state.environment.ocean[key], 0);
  h.renderer.dispose();
});

test("ocean normalization carries the spectrum, its swell waves and the masks", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const plain = value => JSON.parse(JSON.stringify(value));
  const plainOcean = api.normalizeSceneOcean({ windDirection: 10 });
  assert.equal("spectrum" in plainOcean || "swellWaves" in plainOcean || "masks" in plainOcean, false, "no spectrum, no new fields");
  const wave = { dx: 0.6, dz: 0.8, k: 0.125, omega: 1.1, amplitude: 0.04, phase: -1.5 };
  const spectrum = { windSpeed: 12, fetch: 80, swell: 0.2, swellDirection: 270, seed: 4294967295, cascades: [{ size: 1600, kMin: 0.0039, kMax: 0.12 }] };
  const ocean = api.normalizeSceneOcean({ spectrum, swellWaves: [wave], masks: ["hull", "", 7, "dock"] });
  assert.deepEqual(plain(ocean.spectrum), spectrum);
  assert.notEqual(ocean.spectrum, spectrum, "the spectrum is copied");
  assert.deepEqual(plain(ocean.swellWaves), [wave]);
  assert.deepEqual(plain(ocean.masks), ["hull", "dock"]);
  assert.deepEqual(plain(api.normalizeSceneOcean({ spectrum: { seed: 0 } }).swellWaves), [], "waves default to none");
  h.renderer.dispose();
});

test("ocean normalization carries presentation with defaults and clamps, and omits it when absent", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const plain = value => JSON.parse(JSON.stringify(value));
  assert.equal("presentation" in plain(api.normalizeSceneOcean({})), false);
  assert.deepEqual(plain(api.normalizeSceneOcean({ presentation: {} }).presentation), { timeScale: 1, waterDriftX: 0, waterDriftZ: 0, transitionSeconds: 20 });
  assert.deepEqual(plain(api.normalizeSceneOcean({ presentation: { timeScale: 2, waterDriftX: 1.5, waterDriftZ: -3, transitionSeconds: 8 } }).presentation),
    { timeScale: 2, waterDriftX: 1.5, waterDriftZ: -3, transitionSeconds: 8 });
  assert.deepEqual(plain(api.normalizeSceneOcean({ presentation: { timeScale: 99, waterDriftX: 1e6, waterDriftZ: -1e6, transitionSeconds: 1e9 } }).presentation),
    { timeScale: 4, waterDriftX: 100, waterDriftZ: -100, transitionSeconds: 600 });
  assert.deepEqual(plain(api.normalizeSceneOcean({ presentation: { timeScale: "fast", waterDriftX: NaN, waterDriftZ: Infinity, transitionSeconds: -4 } }).presentation),
    { timeScale: 1, waterDriftX: 0, waterDriftZ: 0, transitionSeconds: 0.1 });
  assert.equal(api.normalizeSceneOcean({ presentation: { timeScale: -2 } }).presentation.timeScale, 1, "below the range reads as the default, like Go");
  assert.equal("presentation" in plain(api.normalizeSceneOcean({ presentation: "slow" })), false);
});

test("ocean normalization passes the Go-computed graph parameters through unchanged", async () => {
  const fs = require("node:fs"), path = require("node:path");
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const plain = value => JSON.parse(JSON.stringify(value));
  const golden = JSON.parse(fs.readFileSync(path.join(__dirname, "..", "..", "scene", "testdata", "ocean-graph-params.json"), "utf8"));
  for (const entry of golden) {
    const ocean = plain(api.normalizeSceneOcean({ spectrum: { windSpeed: 5 }, graphParams: entry.graphParams, graphTween: entry.graphTween }));
    assert.deepEqual(ocean.graphParams, entry.graphParams, entry.preset);
    assert.equal(ocean.graphTween, entry.graphTween);
  }
  assert.equal("graphParams" in plain(api.normalizeSceneOcean({ graphParams: { windSpeed: 9 } })), false, "no spectrum, no graph params");
  const cleaned = plain(api.normalizeSceneOcean({ spectrum: {}, graphParams: { windSpeed: 9, bad: "x", worse: NaN }, graphTween: "slow" }));
  assert.deepEqual(cleaned.graphParams, { windSpeed: 9 });
  assert.equal("graphTween" in cleaned, false);
});
