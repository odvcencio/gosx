"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  createBoardWebGPUHarness, createWebGLRendererForPost, makePointsBundle,
} = require("./runtime-test-harness.js");

// A normalized ocean record (normalizeSceneOcean applies these defaults).
function oceanRecord(extra) {
  return Object.assign({ level: 0, windDirection: 0, waveHeight: 0.8, waveLength: 18, choppiness: 0.6, speed: 1,
    deepColor: "#03141f", shallowColor: "#1f6f78", scatterColor: "#2fa58f", foamColor: "#e9eef0",
    clarity: 4, roughness: 0.06, foam: 0.6, surf: 0.5, extent: 4000, bathymetry: null }, extra || {});
}

test("ocean uniform block packs 35 vec4s with a Gerstner table sized by significant height", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const env = { sky: { mode: "physical", sunDirection: { x: 0, y: 0.1, z: -1 } } };
  const out = api.sceneOceanUniformData(oceanRecord({ waveHeight: 2 }), env, { x: 1, y: 2, z: 3 }, 5, true, "high");
  assert.equal(out.length, 140, "webgl-ocean.ts and webgpu-ocean.ts allocate 140 floats");
  assert.deepEqual(Array.from(out.slice(28, 32)), [1, 2, 3, 1], "camera and linear output");
  assert.equal(out[27], 6); assert.equal(out[32], 192); assert.equal(out[33], 256);
  let sumA2 = 0;
  for (let i = 0; i < 6; i++) sumA2 += out[36 + i * 8 + 4] ** 2;
  assert.ok(Math.abs(sumA2 - 4 / 8) < 1e-5, `sum(a^2) = Hs^2/8, got ${sumA2}`);
  for (let i = 0; i < 6; i++) {
    const k = out[36 + i * 8 + 2], qa = out[36 + i * 8 + 5];
    assert.ok(Math.abs(out[36 + i * 8 + 3] - Math.sqrt(9.81 * k)) < 1e-4, "deep-water dispersion");
    assert.ok(k * qa * 6 <= 0.6 + 1e-6, "Gerstner steepness never loops");
  }
  assert.equal(out[84 + 23], 4, "the physical sky block rides along");
  assert.ok(out[132] > 0 && out[132] > out[134], "a low sun is warm");
  assert.ok(out[128] > 0 && out[130] > 0, "ambient sky light is positive");
  const low = api.sceneOceanUniformData(oceanRecord(), {}, {}, 0, false, "low");
  assert.deepEqual([low[27], low[32], low[33]], [4, 96, 128], "low quality: four waves and a quarter of the grid");
  assert.deepEqual(Array.from(low.slice(132, 135)), [0, 0, 0], "no physical sky: no sun glint");
  const shore = api.sceneOceanUniformData(oceanRecord({ bathymetry: { src: "/h.png", minX: -60, minZ: -40, maxX: 60, maxZ: 50, minHeight: -8, maxHeight: 4 } }), {}, {}, 0, true, "high");
  assert.deepEqual(Array.from(shore.slice(20, 27)), [-60, -40, 60, 50, -8, 4, 1]);
  h.renderer.dispose();
});

test("the ocean survives scene state and per-frame lighting resolution into the render bundle", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const state = api.createSceneState({ scene: { environment: { ocean: { waveHeight: 1.4 } } } });
  assert.equal(state.environment.ocean.waveHeight, 1.4);
  const bundle = api.createSceneRenderBundle(64, 64, "#000000", {}, [], [], [], [], [],
    state.environment, 0, [], [], [], [], [], 0, false);
  assert.ok(bundle.environment.ocean, "the render bundle carries the ocean");
  assert.equal(bundle.environment.ocean.waveHeight, 1.4);
  h.renderer.dispose();
});

test("WebGPU draws the ocean in the direct path with premultiplied alpha and restores the frame group", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.environment.sky = { mode: "physical" };
  bundle.environment.ocean = oceanRecord();
  h.canvas.width = h.canvas.height = 64;
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  const module = h.fake.state.shaderModules.find(m => m.label === "gosx-ocean");
  assert.match(module.code, /fn gosxPhysicalSky\(/);
  const draw = h.fake.state.renderPasses.flatMap(p => p.draws).find(d => d.pipeline?.desc?.label === "gosx-ocean");
  assert.ok(draw, "the ocean draws even with no meshes");
  const target = draw.pipeline.desc.fragment.targets[0];
  assert.equal(target.blend.color.srcFactor, "one");
  assert.equal(target.blend.color.dstFactor, "one-minus-src-alpha");
  assert.equal(draw.pipeline.desc.depthStencil.depthWriteEnabled, true);
  bundle.environment.ocean = oceanRecord({ bathymetry: { src: "/missing.png", minX: 0, minZ: 0, maxX: 1, maxZ: 1, minHeight: 0, maxHeight: 1 } });
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.match(h.mount.getAttribute("data-gosx-scene3d-ocean"), /^bathymetry-/);
  bundle.environment.ocean = null;
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-ocean"), "none");
  h.renderer.dispose();
});

test("WebGL draws the ocean after opaque geometry from the gl_VertexID grid", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2");
  const enabled = new Set([gl.CULL_FACE]);
  gl.isEnabled = cap => enabled.has(cap);
  gl.enable = cap => enabled.add(cap); gl.disable = cap => enabled.delete(cap);
  gl.createSampler = () => ({}); gl.deleteSampler = () => {}; gl.samplerParameteri = () => {}; gl.bindSampler = () => {};
  gl.uniform4fv = gl.uniform4fv || (() => {});
  gl.blendFuncSeparate = gl.blendFuncSeparate || (() => {});
  const draws = [];
  const drawArrays = gl.drawArrays.bind(gl);
  gl.drawArrays = (mode, first, count) => { draws.push(count); return drawArrays(mode, first, count); };
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.environment.ocean = oceanRecord();
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  assert.ok(draws.includes(192 * 256 * 6) || draws.includes(96 * 128 * 6), `grid draw, got ${draws}`);
  assert.equal(enabled.has(gl.CULL_FACE), true, "culling is restored");
  assert.deepEqual(h.warnLog, []);
  h.renderer.dispose();
});
