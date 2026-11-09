"use strict";

// The baked shoreline (scene.OceanShoreline) rides in ocean uniform vec4 23,
// which no ocean shader reads as sky data, and both ocean passes use it for the
// waterline foam band and the surf-zone absorption.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { createBoardWebGPUHarness } = require("./runtime-test-harness.js");

function oceanRecord(bathymetry) {
  return { level: 0, windDirection: 0, waveHeight: 0.8, waveLength: 18, choppiness: 0.6, speed: 1,
    deepColor: "#03141f", shallowColor: "#1f6f78", scatterColor: "#2fa58f", foamColor: "#e9eef0",
    clarity: 4, roughness: 0.06, foam: 0.6, surf: 0.5, extent: 4000, bathymetry };
}

test("ocean uniform vec4 23 carries the shoreline distance, foam width and surf clarity", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const base = { src: "data:image/png;base64,AAAA", minX: -100, minZ: -100, maxX: 100, maxZ: 100, minHeight: -10, maxHeight: 10, encoding: "signed-sqrt" };
  const shore = api.sceneOceanUniformData(oceanRecord({ ...base, shoreDistance: 24, shoreFoam: 4, shoreTurbidity: 0.5 }), {}, {}, 0, true, "high");
  assert.deepEqual(Array.from(shore.slice(92, 96)), [24, 4, 0.5, 0]);
  const plain = api.sceneOceanUniformData(oceanRecord(base), {}, {}, 0, true, "high");
  assert.deepEqual(Array.from(plain.slice(92, 96)), [0, 0, 1, 0], "no shoreline: zero distance, unit clarity");
  assert.equal(plain[26], 2, "signed-sqrt bathymetry still selects its decoder");
});

test("both ocean passes read the shoreline only from vec4 23 and keep every spectral-surface anchor", () => {
  const read = (name) => fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", name), "utf8");
  const gl = read("webgl-ocean.ts"), gpu = read("webgpu-ocean.ts");
  assert.match(gl, /float oceanShore\(vec2 xz\)/);
  assert.match(gpu, /fn oceanShore\(xz: vec2f\) -> f32/);
  for (const source of [gl, gpu]) {
    assert.ok(!/u_ocean\[2[1-2]\]|ocean\.p\[2[1-2]\]|u_ocean\[27\]|ocean\.p\[27\]/.test(source), "the sky slots stay unread");
  }
  // Anchors the spectral (FFT) surface patches replace.
  for (const anchor of ["    foam = (1. - smoothstep(0.12, 0.35, v_j)) * lace * bu;\n", "  if (dist < 250.) {", "  color = mix(color, fl, foam); alpha = mix(alpha, 1., foam);"]) {
    assert.ok(gl.split('",\n  "').join("\n").includes(anchor.trimEnd()), anchor);
  }
});
