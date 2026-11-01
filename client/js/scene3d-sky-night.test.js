"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { createBoardWebGPUHarness, createWebGLRendererForPost, makePointsBundle } = require("./runtime-test-harness.js");

const NIGHT_SUN = { x: 0.3, y: -0.35, z: -0.9 };

async function api() {
  const h = await createBoardWebGPUHarness({ fresh: true });
  return h.env.context.__gosx_scene3d_api;
}

function env(a, sky) {
  return a.normalizeSceneEnvironment({ sky: Object.assign({ mode: "physical", sunDirection: NIGHT_SUN }, sky),
    ocean: { level: 0 } });
}

function skyBlock(a, environment) {
  const out = new Float32Array(56);
  const view = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
  a.sceneSkyUniformData(out, environment, view, { fov: 60 }, 1.6, true, true);
  return out;
}

test("night layer normalizes and keeps the sky unchanged when absent", async () => {
  const a = await api();
  assert.equal(env(a, {}).sky.night, undefined);
  const n = env(a, { night: { starDensity: 4, starBrightness: 9, starSeed: 12, moonPhase: 1.25, moonRadius: 9, moonIntensity: 7,
    moonDirection: { x: 0, y: 1, z: 0 } } }).sky.night;
  assert.equal(n.starDensity, 1);
  assert.equal(n.starBrightness, 4);
  assert.equal(n.starSeed, 12);
  assert.ok(Math.abs(n.moonPhase - 0.25) < 1e-9);
  assert.equal(n.moonRadius, 5);
  assert.equal(n.moonIntensity, 4);
  const gradient = a.normalizeSceneEnvironment({ sky: { mode: "gradient", topColor: "#123456", night: {} } }).sky;
  assert.equal(gradient.night, undefined, "only a physical sky carries a night layer");
});

test("sky block packs stars, moon direction and phase for both backends", async () => {
  const a = await api();
  const full = skyBlock(a, env(a, { night: { moonDirection: { x: 0, y: 0, z: -1 }, moonPhase: 0.5, starSeed: 9 } }));
  assert.deepEqual(Array.from(full.slice(44, 47)), [0, 0, -1]);
  assert.ok(Math.abs(full[47] - Math.cos(0.9 * Math.PI / 180)) < 1e-6, "default disc radius is 0.9 degrees");
  assert.ok(full[48] > 0.9, "full night: star gain near 1");
  assert.equal(full[49], 0.5, "zero density means 0.5");
  assert.equal(full[50], 9, "seed");
  assert.ok(Math.abs(full[52] - Math.sin(Math.PI)) < 1e-5 && Math.abs(full[53] + 1) < 1e-6, "full moon phase angle");
  assert.ok(full[54] > 0 && full[55] > 0.9, "disc radiance and halo");
  const day = skyBlock(a, env(a, { sunDirection: { x: 0, y: 0.8, z: -0.6 }, night: { moonPhase: 0.5 } }));
  assert.equal(day[48], 0, "no stars by day");
  assert.equal(day[55], 0, "no halo by day");
  const hidden = skyBlock(a, env(a, { night: { moonRadius: -1, moonPhase: 0.5, starDensity: -1 } }));
  assert.equal(hidden[47], 2, "negative radius hides the disc");
  assert.equal(hidden[48], 0, "negative density hides the stars");
  assert.equal(hidden[54] + hidden[55], 0);
  const off = skyBlock(a, env(a, {}));
  assert.ok(Array.from(off.slice(44, 56)).every(v => v === 0), "no night layer, no night block");
  const cloudy = skyBlock(a, env(a, { clouds: { coverage: 1, opacity: 1 }, night: { moonPhase: 0.5 } }));
  assert.ok(cloudy[48] < full[48] * 0.2, "cloud cover dims the stars");
});

test("the ocean takes the moon as its key light once the sun is down", async () => {
  const a = await api();
  const sea = e => a.sceneOceanUniformData(e.ocean, e, { x: 0, y: 6, z: 0 }, 0, true, "high");
  const moon = { x: 0, y: 0.5, z: -Math.sqrt(0.75) };
  const base = sea(env(a, {}));
  const full = sea(env(a, { night: { moonPhase: 0.5, moonDirection: moon } }));
  const quarter = sea(env(a, { night: { moonPhase: 0.25, moonDirection: moon } }));
  const fresh = sea(env(a, { night: { moonPhase: 0, moonDirection: moon } }));
  const overcast = sea(env(a, { clouds: { coverage: 1, opacity: 1 }, night: { moonPhase: 0.5, moonDirection: moon } }));
  const sun = o => o[132] + o[133] + o[134];
  assert.ok(sun(base) < 0.01, "no moon: the sun is below the horizon and the sea has no key light");
  assert.ok(sun(full) > 0.5, "full moon lights the sea");
  assert.deepEqual([full[136], full[137], full[138]].map(v => +v.toFixed(5)), [moon.x, moon.y, moon.z].map(v => +v.toFixed(5)), "key direction is the moon");
  assert.ok(sun(quarter) < sun(full) && sun(quarter) > sun(fresh), "key light follows the lit fraction");
  assert.ok(sun(fresh) < 0.01, "new moon leaves the sea dark");
  assert.ok(sun(overcast) < sun(full) * 0.3, "cloud cover dims the moon path");
  assert.ok(full[128] > base[128], "moonlit ambient");
  assert.ok(full[99] < base[99] * 0.2, "the reflected sky is dimmed to match the night sky pass");
  const set = sea(env(a, { night: { moonPhase: 0.5, moonDirection: { x: 0, y: -0.6, z: -0.8 } } }));
  assert.ok(sun(set) < 0.01, "a moon below the horizon casts no path");
  const noon = sea(env(a, { sunDirection: { x: 0, y: 0.8, z: -0.6 }, night: { moonPhase: 0.5, moonDirection: moon } }));
  const noonBase = sea(env(a, { sunDirection: { x: 0, y: 0.8, z: -0.6 } }));
  assert.deepEqual(Array.from(noon.slice(128, 139)), Array.from(noonBase.slice(128, 139)), "by day the night layer leaves the sea alone");
});

test("the WebGL sky shader carries the night function and no other shader does", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2");
  const enabled = new Set([gl.CULL_FACE]);
  gl.isEnabled = cap => enabled.has(cap);
  gl.enable = cap => enabled.add(cap); gl.disable = cap => enabled.delete(cap);
  gl.uniform4fv = () => {};
  gl.createSampler = () => ({}); gl.deleteSampler = () => {};
  gl.samplerParameteri = () => {}; gl.bindSampler = () => {};
  const sources = [], original = gl.shaderSource.bind(gl);
  gl.shaderSource = (shader, source) => { sources.push(source); return original(shader, source); };
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.environment.sky = { mode: "physical", sunDirection: NIGHT_SUN, night: { moonPhase: 0.5 } };
  h.renderer.render(bundle, { width: 320, height: 180 });
  const night = sources.filter(s => s.includes("gosxNightSky"));
  assert.equal(night.length, 1, "exactly one program carries the night function");
  assert.ok(night[0].includes("uniform vec4 u_sky[14]"), "it is the sky pass");
  assert.match(night[0], /precision highp int;/);
  for (const word of ["0x7feb352du", "0x846ca68bu", "0x68bc21ebu"]) assert.ok(night[0].includes(word), `integer hash constant ${word}`);
  assert.ok(!/sin\(dot\(/.test(night[0]), "the star hash is integer, not sin-based");
  h.renderer.dispose();
});
