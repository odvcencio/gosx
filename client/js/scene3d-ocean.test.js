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
  assert.equal(out.length, 140, "both ocean passes allocate 140 floats");
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
  const sqrt = api.sceneOceanUniformData(oceanRecord({ bathymetry: { src: "/h.png", minX: -60, minZ: -40, maxX: 60, maxZ: 50, minHeight: -8, maxHeight: 4, encoding: "signed-sqrt" } }), {}, {}, 0, true, "high");
  assert.deepEqual(Array.from(sqrt.slice(24, 27)), [0, 8, 2], "signed-sqrt packs its symmetric range and flag 2");
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

test("WebGL restores the PBR shader for alpha and additive meshes after the ocean", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2");
  gl.isEnabled = () => false;
  gl.uniform4fv = () => {};
  gl.blendFuncSeparate = () => {};
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.worldMeshPositions = new Float32Array([-1, -1, 0, 1, -1, 0, 0, 1, 0]);
  bundle.worldMeshNormals = new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]);
  bundle.worldMeshColors = new Float32Array([1, 1, 1, 1, 1, 1, 1, 1, 1]);
  bundle.meshObjects = [
    { id: "alpha", vertexOffset: 0, vertexCount: 3, materialIndex: 0 },
    { id: "additive", vertexOffset: 0, vertexCount: 3, materialIndex: 1 },
  ];
  bundle.materials = [
    { kind: "standard", color: "#ffffff", opacity: 0.5, blendMode: "alpha" },
    { kind: "standard", color: "#ffffff", blendMode: "additive" },
  ];
  h.renderer.render(bundle, { width: 320, height: 180 });
  const pbrDraws = gl.ops.filter(op => op[0] === "drawArrays" && op[3] === 3);
  assert.equal(pbrDraws.length, 2);
  const pbrProgram = pbrDraws[0][4];
  gl.ops.length = 0;
  bundle.environment.ocean = oceanRecord();
  h.renderer.render(bundle, { width: 320, height: 180 });
  const draws = gl.ops.filter(op => op[0] === "drawArrays");
  const oceanDraw = draws.find(op => op[3] > 3);
  assert.ok(oceanDraw, "the ocean draws before transparent geometry");
  assert.notEqual(oceanDraw[4], pbrProgram);
  const transparentDraws = draws.filter(op => op[3] === 3);
  assert.equal(transparentDraws.length, 2);
  assert.ok(transparentDraws.every(op => op[4] === pbrProgram), "both transparent passes use PBR");
  assert.deepEqual(h.warnLog, []);
  h.renderer.dispose();
});

test("WebGL ocean draws and bathymetry uploads preserve cached mesh textures", async () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2");
  gl.isEnabled = () => false;
  gl.uniform4fv = () => {};
  gl.blendFuncSeparate = () => {};
  gl.ACTIVE_TEXTURE = 0x84e0; gl.TEXTURE_BINDING_2D = 0x8069;
  let active = gl.TEXTURE0;
  const bindings = new Map();
  const activeTexture = gl.activeTexture.bind(gl), bindTexture = gl.bindTexture.bind(gl);
  const getParameter = gl.getParameter.bind(gl), drawArrays = gl.drawArrays.bind(gl);
  gl.activeTexture = unit => { active = unit; activeTexture(unit); };
  gl.bindTexture = (target, texture) => {
    if (target === gl.TEXTURE_2D) bindings.set(active, texture);
    bindTexture(target, texture);
  };
  gl.getParameter = parameter => parameter === gl.ACTIVE_TEXTURE ? active
    : parameter === gl.TEXTURE_BINDING_2D ? bindings.get(active) || null : getParameter(parameter);
  const meshTextures = [];
  gl.drawArrays = (mode, first, count) => {
    if (count === 3) meshTextures.push(bindings.get(gl.TEXTURE0));
    drawArrays(mode, first, count);
  };
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.worldMeshPositions = new Float32Array([-1, -1, 0, 1, -1, 0, 0, 1, 0]);
  bundle.worldMeshNormals = new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]);
  bundle.worldMeshUVs = new Float32Array([0, 0, 1, 0, 0.5, 1]);
  bundle.worldMeshColors = new Float32Array(9).fill(1);
  bundle.meshObjects = [{ id: "textured", vertexOffset: 0, vertexCount: 3, materialIndex: 0 }];
  bundle.materials = [{ kind: "standard", color: "#ffffff", texture: "/albedo.png" }];
  const render = () => h.renderer.render(bundle, { width: 320, height: 180 });
  render();
  await new Promise(resolve => setTimeout(resolve, 20));
  render();
  const albedo = meshTextures.at(-1);
  assert.ok(albedo, "the loaded mesh texture is bound");
  meshTextures.length = 0;
  bundle.environment.ocean = oceanRecord();
  render(); render();
  assert.ok(meshTextures.every(texture => texture === albedo), "consecutive ocean frames retain the cached albedo");
  bundle.environment.ocean.bathymetry = { src: "/bathymetry.png", minX: -10, minZ: -10, maxX: 10, maxZ: 10, minHeight: -5, maxHeight: 2 };
  render();
  assert.equal(bindings.get(gl.TEXTURE0), albedo, "starting bathymetry loading preserves the mesh binding");
  const priorActive = active;
  await new Promise(resolve => setTimeout(resolve, 20));
  assert.equal(active, priorActive, "bathymetry upload preserves the active unit");
  assert.equal(bindings.get(gl.TEXTURE0), albedo, "the asynchronous bathymetry upload preserves the mesh binding");
  render(); render();
  bundle.environment.ocean = null;
  render();
  assert.ok(meshTextures.every(texture => texture === albedo), "loaded bathymetry and ocean removal retain cached albedo");
  h.renderer.dispose();
});

test("WebGL clears an ocean-only scene and resets its status after removal", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2");
  gl.isEnabled = () => false;
  gl.uniform4fv = () => {};
  gl.blendFuncSeparate = () => {};
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.environment.ocean = oceanRecord();
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  gl.ops.length = 0;
  bundle.environment.ocean = null;
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "none");
  assert.ok(gl.ops.some(op => op[0] === "clear" && (op[1] & gl.COLOR_BUFFER_BIT)), "the old ocean frame is cleared");
  assert.ok(!gl.ops.some(op => op[0] === "drawArrays"), "no ocean is drawn after removal");
  h.renderer.dispose();
});

test("WebGPU clears an ocean-only scene and resets its status after removal", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle(null); bundle.points = [];
  h.canvas.width = h.canvas.height = 64;
  bundle.environment.ocean = oceanRecord();
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  const start = h.fake.state.renderPasses.length;
  bundle.environment.ocean = null;
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-ocean"), "none");
  const passes = h.fake.state.renderPasses.slice(start);
  assert.ok(passes.some(p => p.descriptor.colorAttachments?.[0]?.loadOp === "clear"), "the old ocean frame is cleared");
  assert.ok(!passes.flatMap(p => p.draws).some(d => d.pipeline?.desc?.label === "gosx-ocean"));
  h.renderer.dispose();
});

for (const backend of ["WebGL", "WebGPU"]) {
  test(`${backend} ocean uniforms follow the scene clock across pause, resume, and reduced motion`, async () => {
    const h = backend === "WebGPU"
      ? await createBoardWebGPUHarness({ fresh: true })
      : createWebGLRendererForPost({ fresh: true });
    const bundle = makePointsBundle(null); bundle.points = [];
    bundle.environment.ocean = oceanRecord();
    let wallMS = 1000, oceanTime;
    h.env.context.performance.now = () => wallMS;
    if (backend === "WebGL") {
      const gl = h.canvas.getContext("webgl2");
      gl.isEnabled = () => false;
      gl.blendFuncSeparate = () => {};
      gl.uniform4fv = (_location, data) => { if (data.length === 140) oceanTime = data[2]; };
    }
    for (const [phase, wall, clock] of [
      ["playing", 1000, 1.25],
      ["paused interaction", 60000, 1.25],
      ["first resumed frame", 61000, 1.25],
      ["playing again", 61032, 1.282],
      ["reduced motion interaction", 120000, 1.282],
    ]) {
      wallMS = wall;
      bundle.timeSeconds = clock;
      h.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: wallMS });
      if (backend === "WebGPU") {
        const write = h.fake.state.writeBufferCalls.findLast(call => call.data?.length === 16 + 140);
        assert.ok(write, "the ocean uniform buffer is uploaded");
        oceanTime = write.data[18];
      }
      assert.equal(oceanTime, Math.fround(clock), `${phase} uses played time instead of wall time`);
    }
    h.renderer.dispose();
  });
}
