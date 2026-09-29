"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  createBoardWebGPUHarness, createWebGLRendererForPost, makePointsBundle,
} = require("./runtime-test-harness.js");

// Same parameter block as TestPhysicalSkyParamsGolden in scene/sky_physical_test.go:
// sun at 8 degrees elevation, 20 degrees azimuth; turbidity 6; Rayleigh 1.5;
// Mie 0.004; g 0.85; disk radius 0.6 degrees.
const GOLDEN = [8.706814e-06, 2.034437e-05, 4.539885e-05, 1.130223e+02,
  3.833071e-06, 5.790884e-06, 8.497473e-06, 1, null, null, null, 9.999452e-01, 0.85, 0, 0, 0];

function goldenSky() {
  const el = 8 * Math.PI / 180, az = 20 * Math.PI / 180;
  return { mode: "physical", sunDirection: { x: Math.sin(az) * Math.cos(el), y: Math.sin(el), z: -Math.cos(az) * Math.cos(el) },
    turbidity: 6, rayleigh: 1.5, mieCoefficient: 0.004, mieDirectionalG: 0.85, sunDiskRadius: 0.6 };
}

test("physical sky normalizes, clamps, and packs the Go golden parameter block", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const sky = api.normalizeSceneEnvironment({ sky: goldenSky() }).sky;
  assert.equal(sky.mode, "physical");
  assert.equal(sky.turbidity, 6);
  const out = new Float32Array(16);
  api.sceneSkyPhysicalParams(sky, out, 0);
  GOLDEN.forEach((want, i) => {
    if (want === null) return;
    assert.ok(Math.abs(out[i] - want) <= 1e-5 * Math.max(1e-30, Math.abs(want)) + 1e-12, `param ${i}: ${out[i]} vs ${want}`);
  });
  assert.ok(Math.abs(Math.hypot(out[8], out[9], out[10]) - 1) < 1e-6, "sun direction is unit length");
  const clamped = api.normalizeSceneEnvironment({ sky: { mode: "physical", turbidity: 99, mieDirectionalG: 3, sunDiskRadius: 9 } }).sky;
  assert.deepEqual([clamped.turbidity, clamped.mieDirectionalG, clamped.sunDiskRadius], [20, 0.999, 5]);
  const defaults = new Float32Array(16);
  api.sceneSkyPhysicalParams(api.normalizeSceneEnvironment({ sky: { mode: "physical" } }).sky, defaults, 0);
  assert.ok(defaults[9] > 0.1 && defaults[10] < -0.9, "zero sun direction defaults to a low sun over -Z");
  assert.ok(Math.abs(defaults[12] - 0.8) < 1e-6, "zero g defaults to 0.8");
  const hidden = new Float32Array(16);
  api.sceneSkyPhysicalParams({ mode: "physical", sunDiskRadius: -1 }, hidden, 0);
  assert.equal(hidden[11], 2, "a negative disk radius hides the sun");
  const below = new Float32Array(16);
  for (const el of [-0.5, -2, -6]) {
    const r = el * Math.PI / 180;
    api.sceneSkyPhysicalParams({ mode: "physical", rayleigh: 0.001, sunDirection: { x: 0, y: Math.sin(r), z: -Math.cos(r) } }, below, 0);
    for (let i = 0; i < 8; i++) assert.ok(Number.isFinite(below[i]) && below[i] >= 0, `param ${i} at ${el} degrees: ${below[i]}`);
  }
  assert.match(api.sceneSkyPhysicalShaderSource("glsl"), /vec3 gosxPhysicalSky\(/);
  assert.match(api.sceneSkyPhysicalShaderSource("wgsl"), /fn gosxPhysicalSky\(/);
  h.renderer.dispose();
});

test("WebGPU draws the physical sky from one 176-byte uniform block", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.environment.sky = goldenSky();
  h.canvas.width = h.canvas.height = 64;
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-sky"), "physical");
  const module = h.fake.state.shaderModules.find(m => m.label === "gosx-sky");
  assert.match(module.desc ? module.desc.code : module.code, /fn gosxPhysicalSky\(/);
  const draw = h.fake.state.renderPasses.flatMap(p => p.draws).find(d => d.pipeline?.desc?.label === "gosx-sky");
  assert.ok(draw, "the physical sky draws with no geometry");
  assert.ok(h.fake.state.buffers.some(b => b.size === 176), "one 176-byte sky uniform buffer");
  h.renderer.dispose();
});

test("WebGL packs mode 4 and the physical parameters into u_sky[11]", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2");
  const enabled = new Set([gl.CULL_FACE]);
  gl.isEnabled = cap => enabled.has(cap);
  gl.enable = cap => enabled.add(cap); gl.disable = cap => enabled.delete(cap);
  gl.uniform4fv = (location, data) => gl.ops.push(["uniform4fv", location.name, Array.from(data)]);
  gl.createSampler = () => ({}); gl.deleteSampler = () => {};
  gl.samplerParameteri = () => {}; gl.bindSampler = () => {};
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.environment.sky = goldenSky();
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-sky"), "physical");
  const upload = gl.ops.filter(op => op[0] === "uniform4fv" && op[1] === "u_sky[0]").at(-1);
  assert.ok(upload, "the sky uniform block is uploaded");
  assert.equal(upload[2].length, 44);
  assert.equal(upload[2][23], 4, "mode 4 selects the physical branch");
  assert.ok(Math.abs(upload[2][31] - GOLDEN[3]) < 1e-3, "sunE reaches the shader");
  assert.deepEqual(h.warnLog, []);
  h.renderer.dispose();
});
