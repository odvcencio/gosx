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
