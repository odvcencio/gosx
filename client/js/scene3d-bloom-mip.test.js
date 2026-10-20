"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { createWebGLRendererForPost, createBoardWebGPUHarness, makePointsBundle } = require("./runtime-test-harness.js");

const sizes = [[320, 256], [160, 128], [80, 64], [40, 32], [20, 16], [10, 8]];
const order = ["prefilter", ...Array(5).fill("downsample"), ...Array(5).fill("upsample"), "composite"];

function bloomBundle(mode, extra = {}) {
  const bundle = makePointsBundle({ id: "bloom-probe", positions: new Float32Array([0, 0, 0]), colors: new Float32Array([1, 1, 1, 1]), count: 1, size: 1 });
  bundle.postEffects = [{ kind: "bloom", mode, threshold: 0.8, intensity: 0.5, radius: 4, ...extra }];
  return bundle;
}

function glBloomPass(source) {
  if (source.includes("float knee")) return "prefilter";
  if (source.includes("0.03125")) return "downsample";
  if (source.includes("color / 16.0")) return "upsample";
  if (source.includes("scene + bloom")) return "composite";
  if (source.includes("u_threshold")) return "bright";
  if (source.includes("u_direction")) return "blur";
  return "";
}

function recordGL(gl) {
  const getExtension = gl.getExtension.bind(gl);
  gl.getExtension = name => name === "EXT_color_buffer_float" ? {} : getExtension(name);
  gl.deleteRenderbuffer = buffer => gl.ops.push(["deleteRenderbuffer", buffer.id]);
  const allocations = new Map(), attachments = new Map(), bindings = new Map(), passes = [];
  let framebuffer = null, unit = gl.TEXTURE0, viewport;
  function wrap(name, record) {
    const original = gl[name].bind(gl);
    gl[name] = (...args) => { record(...args); return original(...args); };
  }
  wrap("activeTexture", value => { unit = value; });
  wrap("bindTexture", (_target, texture) => { bindings.set(unit, texture); });
  wrap("bindFramebuffer", (_target, value) => { framebuffer = value; });
  wrap("viewport", (_x, _y, w, h) => { viewport = [w, h]; });
  wrap("texImage2D", (...args) => { if (args.length === 9) allocations.set(bindings.get(unit), [args[3], args[4]]); });
  wrap("framebufferTexture2D", (_target, attachment, _kind, texture) => {
    if (attachment === gl.COLOR_ATTACHMENT0) attachments.set(framebuffer, texture);
  });
  wrap("drawArrays", () => {
    const kind = glBloomPass(gl.programShaderSources(gl._activeProgram));
    if (kind) passes.push({ kind, size: viewport, target: attachments.get(framebuffer), framebuffer,
      input: bindings.get(gl.TEXTURE0), bloom: bindings.get(gl.TEXTURE1) });
  });
  return { allocations, passes };
}

test("WebGL mip bloom builds six levels, downsamples then adds tent upsampling, and composites once", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2"), r = recordGL(gl);
  h.canvas.width = 640; h.canvas.height = 512;
  h.renderer.render(bloomBundle("mip"), { width: 640, height: 512 });
  assert.deepEqual(r.passes.map(p => p.kind), order);
  assert.deepEqual(r.passes.slice(0, 6).map(p => p.size), sizes);
  assert.deepEqual(r.passes.slice(6, 11).map(p => p.size), sizes.slice(0, -1).reverse());
  assert.deepEqual(r.passes.at(-1).size, [640, 512]);
  const levelTextures = new Set(r.passes.slice(0, -1).map(p => p.target));
  assert.equal(levelTextures.size, 11, "six base levels and five scratch targets");
  for (const pass of r.passes.slice(0, -1)) {
    assert.notEqual(pass.target, pass.input, "no attachment is sampled while being written");
    assert.notEqual(pass.target, pass.bloom);
    assert.deepEqual(r.allocations.get(pass.target), pass.size);
  }
  for (let i = 1; i < 6; i++) assert.equal(r.passes[i].input, r.passes[i - 1].target);
  for (let i = 6; i < 11; i++) {
    assert.equal(r.passes[i].input, r.passes[10 - i].target);
    assert.equal(r.passes[i].bloom, r.passes[i - 1].target);
  }
  assert.equal(r.passes.at(-1).input, r.passes[0].input);
  assert.equal(r.passes.at(-1).bloom, r.passes[10].target);
  const count = r.allocations.size;
  h.renderer.render(bloomBundle("mip"), { width: 640, height: 512 });
  assert.equal(r.allocations.size, count, "same size reuses all levels");
  h.canvas.width = 320; h.canvas.height = 180;
  r.passes.length = 0;
  h.renderer.render(bloomBundle("mip"), { width: 320, height: 180 });
  assert.deepEqual(r.passes.slice(0, 4).map(p => p.size), [[160, 90], [80, 45], [40, 22], [20, 11]]);
  for (const texture of levelTextures) assert.ok(gl.ops.some(op => op[0] === "deleteTexture" && op[1] === texture.id));
  h.renderer.dispose();
  for (const pass of r.passes.slice(0, -1)) {
    assert.ok(gl.ops.some(op => op[0] === "deleteTexture" && op[1] === pass.target.id));
    assert.ok(gl.ops.some(op => op[0] === "deleteFramebuffer" && op[1] === pass.framebuffer.id));
  }
  for (const op of gl.ops.filter(op => op[0] === "createRenderbuffer")) {
    assert.ok(gl.ops.some(deleted => deleted[0] === "deleteRenderbuffer" && deleted[1] === op[1]));
  }
  assert.deepEqual(h.warnLog, []);
});

test("WebGL empty and unknown bloom modes retain the four legacy passes", () => {
  for (const mode of ["", "unknown"]) {
    const h = createWebGLRendererForPost({ fresh: true });
    const gl = h.canvas.getContext("webgl2"), r = recordGL(gl);
    h.renderer.render(bloomBundle(mode), { width: 320, height: 180 });
    assert.deepEqual(r.passes.map(p => p.kind), ["bright", "blur", "blur", "composite"]);
    assert.deepEqual(r.passes.map(p => p.size), [[160, 90], [160, 90], [160, 90], [320, 180]]);
    assert.deepEqual(gl.ops.filter(op => op[0] === "uniform1f" && op[1] === "u_radius").map(op => op[2]), [4, 4]);
    h.renderer.dispose();
  }
});

test("WebGL mip scale and tiny targets preserve prefilter sizing and stop before sub-eight downsampling", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const r = recordGL(h.canvas.getContext("webgl2"));
  h.renderer.render(bloomBundle("mip", { scale: 0.25 }), { width: 320, height: 180 });
  assert.deepEqual(r.passes.slice(0, 3).map(p => p.size), [[80, 45], [40, 22], [20, 11]]);
  r.passes.length = 0; h.canvas.width = 12; h.canvas.height = 10;
  h.renderer.render(bloomBundle("mip"), { width: 12, height: 10 });
  assert.deepEqual(r.passes.map(p => p.kind), ["prefilter", "composite"]);
  assert.deepEqual(r.passes[0].size, [6, 5]);
  h.renderer.dispose();
});

test("browser bloom normalization accepts mip and drops unknown modes without changing tonemap modes", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const api = h.env.context.__gosx_scene3d_api;
  for (const [mode, expected] of [[" MIP ", "mip"], ["unknown", ""], ["", ""]]) {
    const state = api.createSceneState({ scene: { postEffects: [{ kind: "bloom", mode }] } });
    assert.equal(state.postEffects[0].mode, expected);
  }
  const state = api.createSceneState({ scene: { postEffects: [{ kind: "toneMapping", mode: "reinhard" }] } });
  assert.equal(state.postEffects[0].mode, "reinhard");
  h.renderer.dispose();
});

function gpuBloomPasses(h, start = 0) {
  return h.fake.state.renderPasses.slice(start).flatMap(pass => pass.draws.map(draw => {
    const label = draw.pipeline?.desc.fragment.module.label;
    const kinds = { "post-bloomMipPrefilter": "prefilter", "post-bloomMipDownsample": "downsample",
      "post-bloomMipUpsample": "upsample", "post-bloomComposite": "composite", "post-bloomBright": "bright", "post-blur": "blur" };
    if (!kinds[label]) return null;
    const view = pass.descriptor.colorAttachments[0].view;
    const texture = h.fake.state.textures.find(t => t.id === view.textureId);
    const group = pass.bindGroups.find(g => g.slot === 0).group;
    return { kind: kinds[label], size: texture ? Array.from(texture.desc.size).slice(0, 2) : undefined, target: view,
      texture, entries: group.desc.entries, pipeline: draw.pipeline };
  })).filter(Boolean);
}

test("WebGPU mip bloom matches level sizes, dependency order and HDR compositing, and frees every level", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  h.canvas.width = 640; h.canvas.height = 512;
  h.renderer.render(bloomBundle("mip"), { width: 640, height: 512 });
  const passes = gpuBloomPasses(h);
  const binding = (pass, index) => pass.entries.find(e => e.binding === index).resource;
  assert.deepEqual(passes.map(p => p.kind), order);
  assert.deepEqual(passes.slice(0, 6).map(p => p.size), sizes);
  assert.deepEqual(passes.slice(6, 11).map(p => p.size), sizes.slice(0, -1).reverse());
  const textures = passes.slice(0, -1).map(p => p.texture);
  assert.equal(new Set(textures).size, 11);
  for (const pass of passes) {
    assert.equal(pass.pipeline.desc.fragment.targets[0].format, "rgba16float");
    assert.notEqual(pass.target, binding(pass, 0), "no sampled attachment feedback");
    if (["upsample", "composite"].includes(pass.kind)) assert.notEqual(pass.target, binding(pass, 2));
  }
  for (let i = 1; i < 6; i++) assert.equal(binding(passes[i], 0), passes[i - 1].target);
  for (let i = 6; i < 11; i++) {
    assert.equal(binding(passes[i], 0), passes[10 - i].target);
    assert.equal(binding(passes[i], 2), passes[i - 1].target);
  }
  assert.equal(binding(passes.at(-1), 0), binding(passes[0], 0));
  assert.equal(binding(passes.at(-1), 2), passes[10].target);
  const count = h.fake.state.textures.length;
  h.renderer.render(bloomBundle("mip"), { width: 640, height: 512 });
  assert.equal(h.fake.state.textures.length, count, "same size retains the chain");
  const start = h.fake.state.renderPasses.length;
  h.canvas.width = 320; h.canvas.height = 180;
  h.renderer.render(bloomBundle("mip"), { width: 320, height: 180 });
  assert.ok(textures.every(t => t.destroyed), "resize destroys every old level");
  const resized = gpuBloomPasses(h, start);
  assert.deepEqual(resized.slice(0, 4).map(p => p.size), [[160, 90], [80, 45], [40, 22], [20, 11]]);
  h.renderer.dispose();
  assert.ok(resized.slice(0, -1).every(p => p.texture.destroyed));
  assert.ok(h.fake.state.buffers.every(b => b.destroyed), "post uniform buffers are also disposed");
});

test("WebGPU empty and unknown modes keep the legacy four-pass bloom", async () => {
  for (const mode of ["", "unknown"]) {
    const h = await createBoardWebGPUHarness({ fresh: true });
    h.canvas.width = 320; h.canvas.height = 180;
    h.renderer.render(bloomBundle(mode), { width: 320, height: 180 });
    const passes = gpuBloomPasses(h);
    assert.deepEqual(passes.map(p => p.kind), ["bright", "blur", "blur", "composite"]);
    assert.deepEqual(passes.map(p => p.size), [[160, 90], [160, 90], [160, 90], [320, 180]]);
    assert.equal(h.fake.state.textures.some(t => t.desc.label?.startsWith("gosx-bloom-mip")), false);
    h.renderer.dispose();
  }
});

test("WebGPU mip bloom honors scale and handles a single tiny level", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  h.canvas.width = 320; h.canvas.height = 180;
  h.renderer.render(bloomBundle("mip", { scale: 0.25 }), { width: 320, height: 180 });
  assert.deepEqual(gpuBloomPasses(h).slice(0, 3).map(p => p.size), [[80, 45], [40, 22], [20, 11]]);
  const start = h.fake.state.renderPasses.length;
  h.canvas.width = 12; h.canvas.height = 10;
  h.renderer.render(bloomBundle("mip"), { width: 12, height: 10 });
  assert.deepEqual(gpuBloomPasses(h, start).map(p => p.kind), ["prefilter", "composite"]);
  assert.deepEqual(gpuBloomPasses(h, start)[0].size, [6, 5]);
  h.renderer.dispose();
});

test("WebGPU multiple mip effects retain separate levels and uniform values until submission", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  h.canvas.width = 320; h.canvas.height = 180;
  const bundle = bloomBundle("mip", { threshold: 0.6, radius: 5 });
  bundle.postEffects.push({ kind: "bloom", mode: "mip", threshold: 1.2, radius: 10, scale: 0.25 });
  h.renderer.render(bundle, { width: 320, height: 180 });
  const passes = gpuBloomPasses(h);
  const prefilters = passes.filter(p => p.kind === "prefilter");
  assert.deepEqual(prefilters.map(p => p.size), [[160, 90], [80, 45]]);
  const params = p => p.entries.at(-1).resource.buffer;
  const lastValue = buffer => h.fake.state.writeBufferCalls.filter(w => w.buffer === buffer).at(-1).data[0];
  assert.notEqual(params(prefilters[0]), params(prefilters[1]));
  assert.ok(Math.abs(lastValue(params(prefilters[0])) - 0.6) < 1e-6);
  assert.ok(Math.abs(lastValue(params(prefilters[1])) - 1.2) < 1e-6);
  const upsampleBuffers = [...new Set(passes.filter(p => p.kind === "upsample").map(params))];
  assert.deepEqual(upsampleBuffers.map(lastValue), [1, 2]);
  assert.ok(passes.slice(0, -1).every(p => !p.texture.destroyed), "every encoded input remains alive until submission");
  h.renderer.dispose();
  assert.ok(passes.every(p => p.texture.destroyed));
});

test("both backends composite mip bloom into linear HDR before the following tone map", async () => {
  const glHarness = createWebGLRendererForPost({ fresh: true });
  const gl = glHarness.canvas.getContext("webgl2"), recorded = recordGL(gl);
  const bundle = bloomBundle("mip");
  bundle.postEffects.push({ kind: "toneMapping" });
  glHarness.renderer.render(bundle, { width: 320, height: 180 });
  const composite = gl.programMatching("scene + bloom");
  const tonemap = gl.programs.filter(p => gl.programShaderSources(p).includes("u_toneMapMode")).at(-1);
  const programs = gl.ops.filter(op => op[0] === "drawArrays").map(op => op[4]);
  assert.ok(programs.indexOf(composite.id) < programs.indexOf(tonemap.id));
  assert.ok(recorded.passes.at(-1).target, "composite stays in the HDR post target");
  glHarness.renderer.dispose();
  const h = await createBoardWebGPUHarness({ fresh: true });
  h.canvas.width = 320; h.canvas.height = 180;
  h.renderer.render(bundle, { width: 320, height: 180 });
  const labels = h.fake.state.renderPasses.flatMap(p => p.draws.map(d => d.pipeline?.desc.fragment.module.label));
  assert.ok(labels.indexOf("post-bloomComposite") < labels.indexOf("post-toneMapping"));
  assert.equal(gpuBloomPasses(h).at(-1).texture.desc.format, "rgba16float");
  h.renderer.dispose();
});
