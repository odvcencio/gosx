"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  createBoardWebGPUHarness, createWebGLRendererForPost, makePointsBundle,
} = require("./runtime-test-harness.js");

test("sky fields survive normalization, bundle construction, updates, and removal", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  const raw = { sky: { mode: " environment ", topColor: "#0a2e4f", horizonColor: "#cfe6f2",
    bottomColor: "#1c2a33", blur: 0.15, intensity: 1.2 }, envRotation: 1.25 };
  const state = api.createSceneState({ scene: { environment: raw } });
  assert.equal(state.environment.specified, true);
  const bundle = api.createSceneRenderBundle(64, 64, "#000000", {}, [], [], [], [], [],
    state.environment, 0, [], [], [], [], [], 0, false);
  assert.deepEqual(JSON.parse(JSON.stringify(bundle.environment.sky)), { ...raw.sky, mode: "environment" });
  assert.equal(bundle.environment.envRotation, 1.25);
  const updated = api.normalizeSceneEnvironment({ exposure: 2 }, state.environment);
  assert.equal(updated.sky.intensity, 1.2, "an unrelated update retains the sky");
  assert.equal(api.normalizeSceneEnvironment({ sky: null }, updated).sky, null, "explicit null removes the sky");
  assert.equal(api.normalizeSceneEnvironment({ sky: { topColor: "#fff", blur: 3 } }).sky.mode, "gradient");
  assert.equal(api.normalizeSceneEnvironment({ sky: { mode: "environment", intensity: 0 } }).sky.intensity, 1);
  assert.equal(api.normalizeSceneEnvironment({ sky: { mode: "invalid" } }).sky, null);
  assert.equal(api.normalizeSceneEnvironment({}).sky, null);
  assert.equal(api.normalizeSceneEnvironment({ sky: { mode: "gradient" } }).specified, false,
    "a background alone does not replace default environment lighting");
  h.renderer.dispose();
});

test("WebGPU draws a sky-only scene, keeps depth, and caches format-specific pipelines", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle(null); bundle.points = [];
  h.canvas.width = h.canvas.height = 64;
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.equal(h.fake.state.shaderModules.filter(m => m.label === "gosx-sky").length, 0);
  bundle.environment.sky = { mode: "gradient", topColor: "#123456", horizonColor: "#abcdef", bottomColor: "#102030" };
  for (const post of [false, true, true, false]) {
    bundle.postEffects = post ? [{ kind: "toneMapping" }] : [];
    bundle.msaaSamples = post ? 4 : 1;
    const start = h.fake.state.renderPasses.length;
    h.renderer.render(bundle, { width: 64, height: 64 });
    const passes = h.fake.state.renderPasses.slice(start);
    const draw = passes.flatMap(p => p.draws).find(d => d.pipeline?.desc?.label === "gosx-sky");
    assert.ok(draw, "a sky with no geometry still draws");
    assert.equal(draw.pipeline.desc.depthStencil.depthWriteEnabled, false, "the sky leaves scene depth untouched");
    assert.equal(draw.pipeline.desc.fragment.targets[0].format, post ? "rgba16float" : h.configureCalls.at(-1).format);
  }
  assert.equal(h.fake.state.renderPipelines.filter(p => p.desc.label === "gosx-sky").length, 2);
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-sky"), "gradient");
  bundle.environment.sky = { mode: "environment", horizonColor: "#888888" };
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-sky"), "environment-unavailable");
  const start = h.fake.state.renderPasses.length;
  bundle.environment.sky = null;
  h.renderer.render(bundle, { width: 64, height: 64 });
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-sky"), "none");
  assert.ok(h.fake.state.renderPasses.slice(start).some(p => p.descriptor.colorAttachments?.[0]?.loadOp === "clear"));
  h.renderer.dispose();
  assert.ok(h.fake.state.buffers.filter(b => b.size === 112).every(b => b.destroyed));
});

test("WebGL draws sky-only frames and restores the flat background after removal", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2");
  const enabled = new Set([gl.CULL_FACE]);
  gl.isEnabled = cap => enabled.has(cap);
  gl.enable = cap => enabled.add(cap); gl.disable = cap => enabled.delete(cap);
  gl.uniform4fv = (location, data) => gl.ops.push(["uniform4fv", location.name, Array.from(data)]);
  gl.createSampler = () => ({}); gl.deleteSampler = () => {};
  gl.samplerParameteri = (...args) => gl.ops.push(["samplerParameteri", ...args]);
  gl.bindSampler = (...args) => gl.ops.push(["bindSampler", ...args]);
  const bundle = makePointsBundle(null); bundle.points = [];
  bundle.environment.sky = { mode: "gradient", topColor: "#fff", horizonColor: "#999", bottomColor: "#000" };
  assert.doesNotThrow(() => h.renderer.render(bundle, { width: 320, height: 180 }));
  assert.equal(mount.getAttribute("data-gosx-scene3d-sky"), "gradient");
  bundle.environment.sky = { mode: "environment" };
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-sky"), "environment-unavailable");
  bundle.environment.sky = null;
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-sky"), "none");
  assert.deepEqual(h.warnLog, []);
  assert.deepEqual(gl.ops.filter(op => op[0] === "bindSampler").at(-1), ["bindSampler", 0, null]);
  assert.equal(enabled.has(gl.CULL_FACE), true, "sky restores culling for world geometry");
  h.renderer.dispose();
});
