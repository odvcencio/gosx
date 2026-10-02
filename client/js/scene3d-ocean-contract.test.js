"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const path = require("node:path");
const { createBoardWebGPUHarness } = require("./runtime-test-harness.js");

test("Go environment replacement diffs remove oceans in both IR shapes", async () => {
  const wire = JSON.parse(execFileSync("go", ["run", "./client/js/testdata/ocean-go-writer-fixture"], {
    cwd: path.resolve(__dirname, "../.."), env: { ...process.env, GOWORK: "off" }, encoding: "utf8",
  }));
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    const api = h.env.context.__gosx_scene3d_api;
    for (const name of ["sceneIRRemoval", "canonicalIRRemoval"]) {
      const state = api.createSceneState(wire.previous);
      assert.ok(state.environment.ocean, `${name} starts with an ocean`);
      assert.equal(wire[name][0].data.environment.ocean, null, `${name} sends explicit removal`);
      api.applySceneCommands(state, wire[name]);
      assert.equal(state.environment.ocean, null, `${name} removes the ocean`);
      assert.equal(state.environment.exposure, 2, `${name} keeps other environment changes`);
    }
  } finally {
    h.renderer.dispose();
  }
});

test("Go ocean lowering retains disabled parameters through JSON and browser normalization", async () => {
  const wire = JSON.parse(execFileSync("go", ["run", "./client/js/testdata/ocean-go-writer-fixture"], {
    cwd: path.resolve(__dirname, "../.."), env: { ...process.env, GOWORK: "off" }, encoding: "utf8",
  }));
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  for (const name of ["stopped", "roundTrip"]) {
    const ocean = wire[name].scene.environment.ocean;
    const state = api.createSceneState(wire[name]);
    for (const key of ["choppiness", "speed", "foam", "surf"]) {
      assert.equal(ocean[key], 0, `${name} serializes the clamped ${key}`);
      assert.equal(state.environment.ocean[key], 0, `${name} keeps ${key} disabled in the browser`);
    }
  }
  const defaults = api.normalizeSceneOcean(wire.defaults.scene.environment.ocean);
  assert.deepEqual(wire.defaults.scene.environment.ocean, {}, "Go leaves authored defaults absent");
  assert.equal(defaults.choppiness, 0.6); assert.equal(defaults.speed, 1);
  assert.equal(defaults.foam, 0.6); assert.equal(defaults.surf, 0.5);
  h.renderer.dispose();
});

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
  const stopped = api.normalizeSceneEnvironment({ ocean: { speed: -1, choppiness: -1, foam: -1, surf: -1 } });
  const changed = api.normalizeSceneEnvironment({ exposure: 2 }, stopped);
  for (const key of ["speed", "choppiness", "foam", "surf"]) {
    assert.equal(stopped.ocean[key], 0);
    assert.equal(changed.ocean[key], 0, `${key} stays zero across an unrelated update`);
  }
  assert.deepEqual(plain(changed.ocean), plain(stopped.ocean));
  assert.notEqual(changed.ocean, stopped.ocean, "environment updates own their ocean settings");
  assert.equal(api.normalizeSceneEnvironment({ ocean: { speed: 0 } }, changed).ocean.speed, 1,
    "an authored zero still requests the default");
  assert.equal(api.normalizeSceneEnvironment({ ocean: null }, updated).ocean, null,
    "an explicit null removes the ocean");
  h.renderer.dispose();
});
