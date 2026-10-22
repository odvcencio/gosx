"use strict";
// Spectral (FFT) ocean on WebGL2: the gather scheduler, the GLSL surface patch and the renderer hooks.
// Runs against a recording WebGL2 double (state is tracked, so restoration is checked, not assumed); the
// real-GPU frames come from scripts/scene3d-ocean-spectrum.mjs --backend webgl.
const test = require("node:test"), assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { execFileSync } = require("node:child_process");
const { createRequire } = require("node:module");
const ts = createRequire(path.join(__dirname, "..", "runtime", "package.json"))("typescript");
const { createWebGLRendererForPost, makePointsBundle, makeFakeGPUDevice } = require("./runtime-test-harness.js");

const scene3d = path.join(__dirname, "..", "runtime", "scene3d");
const WEBGPU_FILES = ["ocean-fft-kernels.ts", "ocean-fft-surface.ts", "ocean-fft.ts"];
const GL_FILES = ["ocean-fft-surface-gl.ts", "ocean-fft-gl.ts"];
const transpile = (file) => ts.transpileModule(fs.readFileSync(path.join(scene3d, file), "utf8"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;

// Numeric values of the WebGL2 constants the scheduler uses.
const GLC = {
  TEXTURE0: 0x84C0, TEXTURE_2D: 0x0DE1, FRAMEBUFFER: 0x8D40, DRAW_FRAMEBUFFER: 0x8CA9, READ_FRAMEBUFFER: 0x8CA8, DRAW_FRAMEBUFFER_BINDING: 0x8CA6,
  READ_FRAMEBUFFER_BINDING: 0x8CAA, COLOR_ATTACHMENT0: 0x8CE0, NONE: 0, FRAMEBUFFER_COMPLETE: 0x8CD5, FRAMEBUFFER_INCOMPLETE: 0x8CD6, VIEWPORT: 0x0BA2,
  CURRENT_PROGRAM: 0x8B8D, VERTEX_ARRAY_BINDING: 0x85B5, ACTIVE_TEXTURE: 0x84E0, BLEND: 0x0BE2, DEPTH_TEST: 0x0B71, CULL_FACE: 0x0B44, SCISSOR_TEST: 0x0C11,
  COLOR_WRITEMASK: 0x0C23, DEPTH_WRITEMASK: 0x0B72, UNIFORM_BUFFER: 0x8A11, PIXEL_PACK_BUFFER: 0x88EB, PIXEL_UNPACK_BUFFER: 0x88EC, ARRAY_BUFFER: 0x8892,
  DYNAMIC_DRAW: 0x88E8, STREAM_COPY: 0x88E2, TRIANGLES: 4, VERTEX_SHADER: 0x8B31, FRAGMENT_SHADER: 0x8B30, COMPILE_STATUS: 0x8B81, LINK_STATUS: 0x8B82,
  RGBA32F: 0x8814, R32F: 0x822E, RGBA16F: 0x881A, RGBA: 0x1908, FLOAT: 0x1406, NEAREST: 0x2600, LINEAR: 0x2601, LINEAR_MIPMAP_LINEAR: 0x2703, REPEAT: 0x2901,
  CLAMP_TO_EDGE: 0x812F, TEXTURE_MIN_FILTER: 0x2801, TEXTURE_MAG_FILTER: 0x2800, TEXTURE_WRAP_S: 0x2802, TEXTURE_WRAP_T: 0x2803,
  MAX_VERTEX_TEXTURE_IMAGE_UNITS: 0x8B4C, MAX_TEXTURE_IMAGE_UNITS: 0x8872, MAX_DRAW_BUFFERS: 0x8824, UNIFORM_BUFFER_OFFSET_ALIGNMENT: 0x8A34,
};
const FORMAT = { [GLC.RGBA32F]: "RGBA32F", [GLC.R32F]: "R32F", [GLC.RGBA16F]: "RGBA16F" };

class RecordingGL {
  constructor(options = {}) {
    Object.assign(this, GLC);
    this.extensions = new Set(options.extensions || ["EXT_color_buffer_float"]);
    this.limits = Object.assign({ [GLC.MAX_VERTEX_TEXTURE_IMAGE_UNITS]: 16, [GLC.MAX_TEXTURE_IMAGE_UNITS]: 16, [GLC.MAX_DRAW_BUFFERS]: 8, [GLC.UNIFORM_BUFFER_OFFSET_ALIGNMENT]: 256 }, options.limits);
    this.incompleteMultiTarget = Boolean(options.incompleteMultiTarget);
    this.ops = []; this.draws = []; this.calls = 0;
    this.created = { texture: [], framebuffer: [], program: [], shader: [], buffer: [], vao: [] };
    this.enabled = new Set([GLC.BLEND, GLC.DEPTH_TEST, GLC.CULL_FACE, GLC.SCISSOR_TEST]);
    this.drawFB = null; this.readFB = null; this.currentProgram = null; this.vao = null; this.unit = 0; this.viewportRect = [0, 0, 640, 360];
    this.colorMaskValue = [true, true, true, true]; this.depthMaskValue = true;
    this.buffers = { [GLC.UNIFORM_BUFFER]: null, [GLC.PIXEL_PACK_BUFFER]: null, [GLC.PIXEL_UNPACK_BUFFER]: null, [GLC.ARRAY_BUFFER]: null };
    this.uboBase = null; this.textureUnits = new Map(); this.boundTexture = null;
    this.mipmaps = [];
  }
  get lost() { return Boolean(this.contextLost); }
  isContextLost() { return Boolean(this.contextLost); }
  getExtension(name) { return this.extensions.has(name) ? {} : null; }
  getParameter(p) {
    switch (p) {
      case GLC.DRAW_FRAMEBUFFER_BINDING: return this.drawFB;
      case GLC.READ_FRAMEBUFFER_BINDING: return this.readFB;
      case GLC.VIEWPORT: return this.viewportRect.slice();
      case GLC.CURRENT_PROGRAM: return this.currentProgram;
      case GLC.VERTEX_ARRAY_BINDING: return this.vao;
      case GLC.ACTIVE_TEXTURE: return GLC.TEXTURE0 + this.unit;
      case GLC.COLOR_WRITEMASK: return this.colorMaskValue.slice();
      case GLC.DEPTH_WRITEMASK: return this.depthMaskValue;
      default: return p in this.limits ? this.limits[p] : null;
    }
  }
  isEnabled(c) { return this.enabled.has(c); }
  enable(c) { this.enabled.add(c); this.ops.push(["enable", c]); }
  disable(c) { this.enabled.delete(c); this.ops.push(["disable", c]); }
  colorMask(r, g, b, a) { this.colorMaskValue = [r, g, b, a]; }
  depthMask(v) { this.depthMaskValue = v; }
  viewport(x, y, w, h) { this.viewportRect = [x, y, w, h]; }
  make(kind, extra) { const o = Object.assign({ id: kind + this.created[kind].length, deleted: false }, extra); this.created[kind].push(o); return o; }
  del(o) { if (o) o.deleted = true; }
  createShader(type) { return this.make("shader", { type }); }
  shaderSource(s, src) { s.source = src; }
  compileShader() {}
  getShaderParameter(s, p) { return p === GLC.COMPILE_STATUS ? !(this.rejectShader && this.rejectShader(s.source)) : null; }
  getShaderInfoLog() { return "rejected by the test"; }
  deleteShader(s) { this.del(s); }
  createProgram() { return this.make("program", { shaders: [], uniforms: new Map(), blocks: new Map() }); }
  attachShader(p, s) { p.shaders.push(s); }
  linkProgram() {}
  getProgramParameter(p, name) { return name === GLC.LINK_STATUS ? true : null; }
  getProgramInfoLog() { return ""; }
  deleteProgram(p) { this.del(p); }
  useProgram(p) { this.currentProgram = p; }
  fragmentSource(p) { const s = p && p.shaders.find((x) => x.type === GLC.FRAGMENT_SHADER); return s ? s.source : ""; }
  getUniformLocation(p, name) { const base = name.replace(/\[0\]$/, ""); return new RegExp("\\b" + base + "\\b").test(p.shaders.map((s) => s.source).join("\n")) ? { program: p, name } : null; }
  getUniformBlockIndex(p, name) { return this.fragmentSource(p).includes(name) ? 0 : 0xFFFFFFFF; }
  uniformBlockBinding(p, index, binding) { p.blocks.set(index, binding); }
  uniform1i(loc, v) { if (loc) loc.program.uniforms.set(loc.name, v); }
  uniform1ui(loc, v) { if (loc) loc.program.uniforms.set(loc.name, v); }
  uniform4fv(loc, v) { if (loc) { loc.program.uniforms.set(loc.name, Float32Array.from(v)); this.ops.push(["uniform4fv", loc.name]); } }
  createBuffer() { return this.make("buffer", { data: new Uint8Array(0) }); }
  bindBuffer(target, b) { this.buffers[target] = b; }
  bindBufferBase(target, index, b) { if (target === GLC.UNIFORM_BUFFER) this.uboBase = b ? { buffer: b, offset: 0, size: b.data.length } : null; }
  bindBufferRange(target, index, b, offset, size) { if (target === GLC.UNIFORM_BUFFER) this.uboBase = { buffer: b, offset, size }; }
  bufferData(target, size) { const b = this.buffers[target]; b.data = new Uint8Array(typeof size === "number" ? size : size.byteLength); b.target = target; }
  bufferSubData(target, offset, data) { this.buffers[target].data.set(new Uint8Array(data.buffer || data, data.byteOffset || 0, data.byteLength), offset); }
  deleteBuffer(b) { this.del(b); }
  createVertexArray() { return this.make("vao"); }
  bindVertexArray(v) { this.vao = v; }
  deleteVertexArray(v) { this.del(v); }
  activeTexture(u) { this.unit = u - GLC.TEXTURE0; }
  createTexture() { return this.make("texture", { params: {} }); }
  bindTexture(target, t) { this.boundTexture = t; this.textureUnits.set(this.unit, t); }
  texStorage2D(target, levels, internal, w, h) { Object.assign(this.boundTexture, { levels, internal, format: FORMAT[internal], w, h }); }
  texParameteri(target, name, v) { this.boundTexture.params[name] = v; }
  texSubImage2D(target, level, x, y, w, h, format, type, offset) { this.ops.push(["texSubImage2D", this.boundTexture, w, h, offset, this.buffers[GLC.PIXEL_UNPACK_BUFFER]]); }
  readPixels(x, y, w, h, format, type, offset) { this.ops.push(["readPixels", this.readFB, w, h, offset, this.buffers[GLC.PIXEL_PACK_BUFFER]]); }
  generateMipmap() { this.mipmaps.push(this.boundTexture); this.draws.push({ kind: "mipmap", texture: this.boundTexture }); }
  deleteTexture(t) { this.del(t); }
  createFramebuffer() { return this.make("framebuffer", { attachments: {}, drawBuffers: [GLC.COLOR_ATTACHMENT0] }); }
  bindFramebuffer(target, f) {
    if (target === GLC.FRAMEBUFFER || target === GLC.DRAW_FRAMEBUFFER) this.drawFB = f;
    if (target === GLC.FRAMEBUFFER || target === GLC.READ_FRAMEBUFFER) this.readFB = f;
  }
  framebufferTexture2D(target, attachment, texTarget, t) { this.drawFB.attachments[attachment - GLC.COLOR_ATTACHMENT0] = t; }
  drawBuffers(list) { this.drawFB.drawBuffers = list.slice(); }
  checkFramebufferStatus() {
    const f = this.drawFB;
    return this.incompleteMultiTarget && Object.keys(f.attachments).length > 1 ? GLC.FRAMEBUFFER_INCOMPLETE : GLC.FRAMEBUFFER_COMPLETE;
  }
  deleteFramebuffer(f) { this.del(f); }
  drawArrays(mode, first, count) {
    const p = this.currentProgram, source = this.fragmentSource(p), ubo = this.uboBase;
    const kernel = (/uniform (\w+)_ubo/.exec(source) || [])[1];
    const names = { OceanSpectrumParams: "spectrum", OceanEvolveParams: "evolve", OceanAssembleParams: "assemble", OceanFoamParams: "foam", FFTStage: "fft" };
    const record = { kind: "draw", mode, count, program: p, kernel: names[kernel] || null, viewport: this.viewportRect.slice(), fbo: this.drawFB,
      attachments: this.drawFB ? Object.assign({}, this.drawFB.attachments) : null, drawBuffers: this.drawFB ? this.drawFB.drawBuffers.slice() : null,
      enabled: new Set(this.enabled), input: this.textureUnits.get(4) || null, inputs: [4, 5, 6, 7, 8].map((u) => this.textureUnits.get(u) || null),
      elioWidth: p && p.uniforms.get("elio_width"), vao: this.vao };
    if (kernel && ubo) { const v = new DataView(ubo.buffer.data.buffer, ubo.offset); record.u32 = [v.getUint32(0, true), v.getUint32(4, true), v.getUint32(8, true), v.getUint32(12, true)]; record.f32 = [v.getFloat32(16, true), v.getFloat32(20, true)]; }
    this.draws.push(record);
  }
  live(kind) { return this.created[kind].filter((o) => !o.deleted); }
}

function loadSources(context, files) {
  context.window ||= context;
  for (const file of files) vm.runInContext(transpile(file), context, { filename: file });
}
function glContext(extra) {
  const context = vm.createContext(Object.assign({ Math, Number, Object, Array, Float32Array, Uint32Array, Uint8Array, ArrayBuffer, DataView, Error, Set, Map, JSON, console }, extra));
  loadSources(context, [...WEBGPU_FILES, ...GL_FILES]);
  return context;
}
const floatCaps = (gl) => ({ colorBufferFloat: Boolean(gl.getExtension("EXT_color_buffer_float")) });
const ocean = (spectrum, extra) => Object.assign({ level: 0, choppiness: 0.6, windDirection: 0, spectrum }, extra);
function standalone(glOptions) {
  const context = glContext();
  const gl = new RecordingGL(glOptions);
  const api = context.window.__gosx_scene3d_ocean_fft_gl_api;
  const mount = { attrs: {}, setAttribute(k, v) { this.attrs[k] = v; } };
  const textureCache = { _sceneTextureEpoch: 0 };
  const run = (resources, env, extra) => api.frame(resources, gl, Object.assign({ ocean: env, tier: "high", time: 0, camera: { x: 0, z: 0 }, textureCache, mount, caps: floatCaps }, extra));
  return { context, gl, api, mount, textureCache, run };
}
const draws = (gl) => gl.draws.filter((d) => d.kind === "draw");
const cascadeSizes = (tier) => ({ high: [64, 256, 256, 256], medium: [64, 128, 128, 128], low: [64, 64, 64] })[tier];

// The pass list the scheduler must issue for one frame: per cascade evolve, both transforms, assemble; then foam.
function expectedPasses(tier, first) {
  const out = [];
  for (const n of cascadeSizes(tier)) {
    const L = Math.log2(n);
    if (first) out.push("spectrum");
    out.push("evolve");
    for (let t = 0; t < 2; t++) for (let axis = 0; axis < 2; axis++) for (let stage = 0; stage < L; stage++) out.push("fft:" + n + ":" + axis + ":" + stage);
    out.push("assemble", "mip");
  }
  out.push("foam");
  return out;
}
function observedPasses(gl) {
  return gl.draws.map((d) => d.kind === "mipmap" ? (d.texture.format === "RGBA16F" ? "mip" : "?") : d.kernel === "fft" ? "fft:" + d.u32.slice(0, 3).join(":").replace(/^(\d+):(\d+):(\d+)$/, "$1:$3:$2") : d.kernel)
    .filter((name, i, all) => !(name === "mip" && all[i - 1] === "mip"));
}

test("gate: without EXT_color_buffer_float the scheduler creates nothing and the caller keeps the Gerstner ocean", () => {
  const s = standalone({ extensions: [] }), resources = {};
  assert.equal(s.run(resources, ocean({})), null);
  assert.equal(resources.fftDisabled, "EXT_color_buffer_float");
  assert.equal(s.mount.attrs["data-gosx-scene3d-ocean-fft-fallback"], "EXT_color_buffer_float");
  for (const kind of Object.keys(s.gl.created)) assert.equal(s.gl.created[kind].length, 0, kind + " must not be created");
  assert.equal(s.textureCache._sceneTextureEpoch, 0, "no GL state touched, so no epoch bump");
  const before = s.gl.ops.length;
  assert.equal(s.run(resources, ocean({})), null);
  assert.equal(s.gl.ops.length, before, "the decision is remembered");
});

test("gate: too few texture units or a kernel set without the spectrum GLSL also fall back, with the reason", () => {
  const few = standalone({ limits: { [GLC.MAX_VERTEX_TEXTURE_IMAGE_UNITS]: 4 } }), resources = {};
  assert.equal(few.run(resources, ocean({})), null);
  assert.equal(resources.fftDisabled, "texture-units");
  // A kernel set generated before ocean_spectrum had a gather form carries no GLSL for it.
  const context = vm.createContext({ Math, Number, Object, Array, Float32Array, Uint32Array, Uint8Array, ArrayBuffer, DataView, Error, Set, Map, JSON, console });
  context.window = context;
  loadSources(context, ["ocean-fft-kernels.ts"]);
  const set = context.window.__gosx_scene3d_elio_ocean;
  const kernels = Object.assign({}, set.kernels, { oceanSpectrum: Object.assign({}, set.kernels.oceanSpectrum, { glsl: undefined }) });
  context.window.__gosx_scene3d_elio_ocean = { kernels };
  loadSources(context, ["ocean-fft-surface.ts", "ocean-fft.ts", ...GL_FILES]);
  const gl = new RecordingGL(), res = {};
  assert.equal(context.window.__gosx_scene3d_ocean_fft_gl_api.frame(res, gl, { ocean: ocean({}), tier: "high", time: 0, caps: floatCaps }), null);
  assert.equal(res.fftDisabled, "kernel:oceanSpectrum");
});

test("pass order and count: spectrum once, then evolve, two transforms of 2 log2 n stages, assemble, mips; foam last", () => {
  for (const tier of ["high", "medium", "low"]) {
    const s = standalone(), resources = {};
    assert.ok(s.run(resources, ocean({ windSpeed: 12 }), { tier }), tier + ": engine");
    assert.deepEqual(observedPasses(s.gl), expectedPasses(tier, true), tier + ": first frame");
    s.gl.draws.length = 0;
    s.run(resources, ocean({ windSpeed: 12 }), { tier, time: 1 });
    assert.deepEqual(observedPasses(s.gl), expectedPasses(tier, false), tier + ": later frames skip ocean_spectrum");
  }
  const s = standalone(), resources = {};
  s.run(resources, ocean({}));
  assert.equal(draws(s.gl).length, expectedPasses("high", true).filter((x) => x !== "mip").length, "one draw per pass");
});

test("every FFT pass reads the texture the previous pass wrote and never the one it writes", () => {
  const s = standalone(), resources = {};
  s.run(resources, ocean({}));
  const all = draws(s.gl);
  for (let i = 0; i < all.length; i++) {
    const d = all[i];
    if (d.kernel !== "fft") continue;
    const out = d.attachments[0];
    assert.notEqual(d.input, out, "a kernel never reads what it writes");
    assert.equal(d.input.format, "RGBA32F");
    const prev = all[i - 1];
    const stage = d.u32[1], axis = d.u32[2];
    if (stage > 0 || axis > 0) assert.equal(d.input, prev.attachments[0], "reads the previous stage's output");
  }
  // The transform ends in the texture the evolve pass wrote, so assemble reads evolve's targets.
  const evolve = all.find((d) => d.kernel === "evolve"), assemble = all.find((d) => d.kernel === "assemble");
  assert.equal(assemble.inputs[0], evolve.attachments[0]);
  assert.equal(assemble.inputs[1], evolve.attachments[1]);
});

test("multi-target setup: attachments, draw buffers, formats and viewports follow each kernel's outputs", () => {
  const s = standalone(), resources = {};
  s.run(resources, ocean({}));
  const by = (kernel) => draws(s.gl).filter((d) => d.kernel === kernel);
  const fmt = (d) => Object.keys(d.attachments).map((k) => d.attachments[k].format);
  assert.deepEqual(by("spectrum").map(fmt)[0], ["RGBA32F", "R32F"]);
  assert.deepEqual(by("evolve").map(fmt)[0], ["RGBA32F", "RGBA32F"]);
  assert.deepEqual(by("assemble").map(fmt)[0], ["RGBA16F", "RGBA16F"]);
  assert.deepEqual(by("foam").map(fmt)[0], ["R32F", "RGBA16F"]);
  assert.deepEqual(by("fft").map(fmt)[0], ["RGBA32F"]);
  for (const d of [...by("spectrum"), ...by("evolve"), ...by("assemble"), ...by("foam")]) assert.deepEqual([...d.drawBuffers], [GLC.COLOR_ATTACHMENT0, GLC.COLOR_ATTACHMENT0 + 1]);
  assert.ok(draws(s.gl).every((d) => Object.keys(d.attachments).length <= 2), "at most two outputs (32 bytes) per pass");
  assert.deepEqual(by("assemble").map((d) => d.viewport.slice(2).join("x")), ["64x64", "256x256", "256x256", "256x256"]);
  assert.deepEqual(by("foam").map((d) => d.viewport.slice(2).join("x")), ["1024x1024"]);
  for (const d of draws(s.gl)) assert.equal(d.elioWidth, d.viewport[2], "elio_width is the texture width of every pass");
  // The surface samples RGBA16F with mips; foam state is R32F because decay is multiplicative.
  const a = by("assemble")[1];
  assert.equal(a.attachments[0].levels, 7);
  assert.equal(a.attachments[0].params[GLC.TEXTURE_MIN_FILTER], GLC.LINEAR_MIPMAP_LINEAR);
  assert.equal(a.attachments[0].params[GLC.TEXTURE_WRAP_S], GLC.REPEAT);
  const evolveTexture = by("evolve")[0].attachments[0];
  assert.equal(evolveTexture.params[GLC.TEXTURE_MIN_FILTER], GLC.NEAREST, "float32 inputs are fetched, never filtered");
  // generateMipmap runs on both outputs, once per cascade, after that cascade's assemble pass.
  const order = s.gl.draws.map((d) => d.kind === "mipmap" ? "mip:" + d.texture.format : d.kernel);
  assert.equal(order.filter((x) => x === "mip:RGBA16F").length, 8);
  for (let i = 0; i < order.length; i++) if (order[i] === "assemble") assert.deepEqual(order.slice(i + 1, i + 3), ["mip:RGBA16F", "mip:RGBA16F"]);
  assert.equal(order[order.length - 1], "foam");
});

test("when the driver rejects a multi-target framebuffer, each output gets its own and the kernel runs once per output", () => {
  const s = standalone({ incompleteMultiTarget: true }), resources = {};
  const engine = s.run(resources, ocean({}));
  assert.ok(engine && engine.split, "split mode");
  const by = (kernel) => draws(s.gl).filter((d) => d.kernel === kernel);
  assert.equal(by("evolve").length, 2 * 4);
  assert.equal(by("foam").length, 2);
  const first = by("evolve").slice(0, 2);
  assert.deepEqual(first.map((d) => Object.keys(d.attachments).join()), ["0", "1"]);
  assert.deepEqual([...first[0].drawBuffers], [GLC.COLOR_ATTACHMENT0]);
  assert.deepEqual([...first[1].drawBuffers], [GLC.NONE, GLC.COLOR_ATTACHMENT0 + 1], "output 1 draws only to attachment 1");
  assert.equal(by("fft").length, 4 * 0 + (2 * 2 * (6 + 8 + 8 + 8)), "FFT passes have one output and are not repeated");
});

test("GL state is saved before the passes and restored after, whatever the host had bound", () => {
  const s = standalone(), resources = {}, gl = s.gl;
  const hostFB = gl.createFramebuffer(), hostRead = gl.createFramebuffer(), hostProgram = gl.createProgram(), hostVAO = gl.createVertexArray();
  gl.drawFB = hostFB; gl.readFB = hostRead; gl.currentProgram = hostProgram; gl.vao = hostVAO; gl.unit = 2; gl.viewportRect = [3, 4, 500, 300];
  gl.colorMaskValue = [true, false, true, true]; gl.depthMaskValue = false;
  gl.created.framebuffer.length = 0; gl.created.program.length = 0; gl.created.vao.length = 0;
  const want = { draw: hostFB, read: hostRead, program: hostProgram, vao: hostVAO, unit: 2, viewport: [3, 4, 500, 300] };
  s.run(resources, ocean({}));
  assert.equal(gl.drawFB, want.draw); assert.equal(gl.readFB, want.read); assert.equal(gl.currentProgram, want.program); assert.equal(gl.vao, want.vao);
  assert.equal(gl.unit, want.unit); assert.deepEqual(gl.viewportRect, want.viewport);
  assert.deepEqual([...gl.enabled].sort(), [GLC.BLEND, GLC.DEPTH_TEST, GLC.CULL_FACE, GLC.SCISSOR_TEST].sort(), "blend, depth, cull and scissor come back on");
  assert.deepEqual(gl.colorMaskValue, [true, false, true, true]); assert.equal(gl.depthMaskValue, false);
  for (const target of [GLC.PIXEL_PACK_BUFFER, GLC.PIXEL_UNPACK_BUFFER, GLC.UNIFORM_BUFFER]) assert.equal(gl.buffers[target], null);
  assert.equal(gl.uboBase, null);
  for (const d of draws(gl)) {
    assert.ok(![GLC.BLEND, GLC.DEPTH_TEST, GLC.CULL_FACE, GLC.SCISSOR_TEST].some((c) => d.enabled.has(c)), "passes run with blend, depth, cull and scissor off");
    assert.notEqual(d.vao, hostVAO, "passes use their own VAO");
  }
  // A second frame (no rebuild) restores too.
  gl.drawFB = hostFB; s.run(resources, ocean({}), { time: 1 });
  assert.equal(gl.drawFB, hostFB); assert.deepEqual(gl.viewportRect, want.viewport);
});

test("each frame bumps the texture epoch so material bindings are rebound", () => {
  const s = standalone(), resources = {};
  s.run(resources, ocean({}));
  const one = s.textureCache._sceneTextureEpoch;
  assert.ok(one >= 1);
  s.run(resources, ocean({}), { time: 1 });
  assert.ok(s.textureCache._sceneTextureEpoch > one);
});

test("the B field reaches the foam kernel in the foam layout through a pixel buffer, once per cascade", () => {
  const s = standalone(), resources = {};
  s.run(resources, ocean({}));
  const reads = s.gl.ops.filter((o) => o[0] === "readPixels"), writes = s.gl.ops.filter((o) => o[0] === "texSubImage2D");
  assert.equal(reads.length, 4); assert.equal(writes.length, 4);
  assert.deepEqual(reads.map((o) => o[2] + "x" + o[3]), ["64x64", "256x256", "256x256", "256x256"]);
  assert.deepEqual(writes.map((o) => o[2] + "x" + o[3]), ["1024x4", "1024x64", "1024x64", "1024x64"], "same texels, rows as wide as the foam state");
  assert.ok(reads.every((o, i) => o[5] && o[5] === writes[i][5]), "the same buffer is packed and unpacked");
  const foam = draws(s.gl).find((d) => d.kernel === "foam");
  assert.equal(foam.inputs[1].w, 1024, "the foam kernel's fieldB textures are foam-state wide");
});

test("scene time, tier and spectrum changes: evolve gets t modulo the loop, a changed spectrum reruns ocean_spectrum, a tier change rebuilds", () => {
  const s = standalone(), resources = {};
  s.run(resources, ocean({ windSpeed: 9 }), { time: 1234.5 });
  const evolve = draws(s.gl).find((d) => d.kernel === "evolve");
  assert.ok(Math.abs(evolve.f32[0] - 234.5) < 1e-3);
  const count = (k) => draws(s.gl).filter((d) => d.kernel === k).length;
  assert.equal(count("spectrum"), 4);
  s.run(resources, ocean({ windSpeed: 9 }), { time: 1235 });
  assert.equal(count("spectrum"), 4, "unchanged spectrum");
  const textures = s.gl.created.texture.length;
  s.run(resources, ocean({ windSpeed: 15 }), { time: 1236 });
  assert.equal(count("spectrum"), 8, "changed spectrum reruns once per cascade");
  assert.equal(s.gl.created.texture.length, textures, "no allocation");
  const old = s.gl.live("texture");
  const programs = s.gl.created.program.length;
  s.run(resources, ocean({ windSpeed: 15 }), { time: 1237, tier: "low" });
  assert.equal(resources.fft.buildCount, 2);
  assert.ok(old.every((t) => t.deleted), "the old textures are released");
  assert.equal(s.gl.created.program.length, programs + 0, "programs are kept across rebuilds");
});

test("dispose releases every GL object; a lost context does no work", () => {
  const s = standalone(), resources = {};
  s.run(resources, ocean({}));
  assert.ok(s.gl.live("texture").length > 0 && s.gl.live("framebuffer").length > 0);
  resources.fft.dispose();
  for (const kind of Object.keys(s.gl.created)) assert.equal(s.gl.live(kind).length, 0, kind + " leaked");
  const lost = standalone(), res = {};
  lost.gl.contextLost = true;
  assert.equal(lost.run(res, ocean({})), null);
  assert.equal(lost.gl.created.texture.length, 0);
});

test("a shader that does not compile disables the scheduler instead of throwing", () => {
  const s = standalone(), resources = {};
  s.gl.rejectShader = (source) => /OceanFoamParams_ubo/.test(source);
  assert.equal(s.run(resources, ocean({})), null);
  assert.equal(resources.fftDisabled, "error");
  assert.equal(resources.fft, null);
  for (const kind of Object.keys(s.gl.created)) assert.equal(s.gl.live(kind).length, 0, kind + " leaked after the failure");
});

function surfaceGLSL(context, vertex, fragment) { return context.window.__gosx_scene3d_ocean_fft_surface_gl.glsl(vertex, fragment); }

test("the GLSL surface patch names its anchors and fails loudly when one disappears", () => {
  const context = glContext();
  assert.throws(() => surfaceGLSL(context, "void main() {}", "void main() {}"), /anchor not found/);
});

// --- the full renderer: hook order, surface variant, fallback -----------------------------------------------------

function oceanRecord(extra) {
  return Object.assign({ level: 0, windDirection: 0, waveHeight: 0.8, waveLength: 18, choppiness: 0.6, speed: 1,
    deepColor: "#03141f", shallowColor: "#1f6f78", scatterColor: "#2fa58f", foamColor: "#e9eef0",
    clarity: 4, roughness: 0.06, foam: 0.6, surf: 0.5, extent: 4000, bathymetry: null }, extra || {});
}
// The repository's fake context is minimal; this adds what the spectral path calls, over the fake's own bookkeeping.
function rendererWithFFT(options = {}) {
  const h = createWebGLRendererForPost({ fresh: options.fresh !== false });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2");
  const extensions = new Set(options.extensions || ["EXT_color_buffer_float"]);
  const getExtension = gl.getExtension.bind(gl), getParameter = gl.getParameter.bind(gl);
  gl.getExtension = (name) => extensions.has(name) ? {} : getExtension(name);
  gl.getParameter = (p) => ({ [GLC.MAX_VERTEX_TEXTURE_IMAGE_UNITS]: 16, [GLC.MAX_TEXTURE_IMAGE_UNITS]: 16, [GLC.MAX_DRAW_BUFFERS]: 8, [GLC.UNIFORM_BUFFER_OFFSET_ALIGNMENT]: 256, [GLC.COLOR_WRITEMASK]: [true, true, true, true], [GLC.DEPTH_WRITEMASK]: true })[p] ?? getParameter(p);
  const enabled = new Set([gl.CULL_FACE]);
  gl.isEnabled = (c) => enabled.has(c); gl.enable = (c) => enabled.add(c); gl.disable = (c) => enabled.delete(c);
  const record = [];
  const stubs = { createSampler: () => ({}), deleteSampler() {}, samplerParameteri() {}, bindSampler() {}, uniform4fv() {}, uniform1i() {}, uniform1ui() {}, blendFuncSeparate() {},
    getUniformBlockIndex: () => 0, uniformBlockBinding() {}, bindBufferBase() {}, bindBufferRange() {}, colorMask() {}, readPixels() {}, texSubImage2D() {}, drawBuffers() {},
    bufferSubData() {}, bufferData() {}, texStorage2D() {}, framebufferTexture2D() {}, generateMipmap() {}, getUniformLocation: (p, n) => ({ n }), uniformMatrix4fv() {} };
  for (const [name, fn] of Object.entries(stubs)) if (!gl[name]) gl[name] = fn;
  Object.assign(gl, Object.fromEntries(Object.entries(GLC).filter(([k]) => gl[k] === undefined)));
  gl.uniform1ui = gl.uniform1ui || (() => {});
  const drawArrays = gl.drawArrays.bind(gl);
  gl.drawArrays = (mode, first, count) => { record.push({ count, program: gl.programMatching ? null : null }); return drawArrays(mode, first, count); };
  loadSources(h.env.context, [...WEBGPU_FILES, ...GL_FILES]);
  h.env.context.window ||= h.env.context;
  return { h, gl, mount, record, extensions };
}
function sceneBundle(spectrum) {
  const b = makePointsBundle(null);
  b.points = [];
  b.environment.sky = { mode: "physical" };
  b.environment.ocean = oceanRecord(spectrum ? { spectrum } : {});
  b.camera.y = 30; b.camera.z = 0;
  return b;
}
function oceanProgramSources(gl) {
  const program = gl.programMatching("u_ocean[35]");
  return program ? program.attached.map((shader) => String(shader.source || "")) : null;
}

test("the renderer runs the gather passes before the ocean draw and compiles the spectral surface", () => {
  const { h, gl, mount } = rendererWithFFT();
  h.renderer.render(sceneBundle({ windSpeed: 12 }), { width: 320, height: 180 });
  assert.deepEqual(h.warnLog, [], "no warnings");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface+spectrum");
  const sources = oceanProgramSources(gl);
  assert.ok(sources, "the spectral ocean program exists");
  const all = [].concat(sources).map((s) => (typeof s === "string" ? s : s.source || "")).join("\n");
  assert.match(all, /textureLod\(u_spDisp0/);
  assert.match(all, /uniform vec4 u_sp\[6\]/);
  assert.doesNotMatch(all, /for \(int i = 0; i < 6; i\+\+\) \{\n    if \(i >= waves\)/, "the Gerstner loop is gone");
  const grid = gl.ops.findIndex((op) => op[0] === "drawArrays" && op[3] === 192 * 256 * 6);
  const gather = gl.ops.findIndex((op) => op[0] === "drawArrays" && op[3] === 3);
  assert.ok(gather >= 0 && grid > gather, "gather passes are drawn before the ocean grid");
  h.renderer.dispose();
});

test("without EXT_color_buffer_float the renderer draws the Gerstner ocean and says why", () => {
  const { h, gl, mount } = rendererWithFFT({ extensions: [] });
  h.renderer.render(sceneBundle({ windSpeed: 12 }), { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean-fft-fallback"), "EXT_color_buffer_float");
  const sources = oceanProgramSources(gl);
  assert.ok(sources);
  assert.doesNotMatch([].concat(sources).map((s) => (typeof s === "string" ? s : s.source || "")).join("\n"), /u_spDisp0/);
  assert.ok(!gl.ops.some((op) => op[0] === "drawArrays" && op[3] === 3), "no gather pass");
  h.renderer.dispose();
});

test("a surface patch that cannot be applied falls back to the Gerstner ocean and disables the scheduler", () => {
  const { h, gl, mount } = rendererWithFFT();
  h.env.context.window.__gosx_scene3d_ocean_fft_surface_gl.glsl = () => { throw new Error("anchor not found: injected"); };
  h.renderer.render(sceneBundle({}), { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean-fft-fallback"), "surface-patch");
  h.renderer.render(sceneBundle({}), { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface", "stays on Gerstner");
  h.renderer.dispose();
});

test("scenes without a spectrum render as before: no FFT resources", () => {
  const { h, gl, mount } = rendererWithFFT();
  h.renderer.render(sceneBundle(null), { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean-fft-fallback"), null);
  assert.ok(!gl.ops.some((op) => op[0] === "drawArrays" && op[3] === 3));
  h.renderer.dispose();
});

test("renderer dispose (also the context-loss path) releases the gather resources", () => {
  const { h, gl } = rendererWithFFT();
  h.renderer.render(sceneBundle({}), { width: 320, height: 180 });
  const created = gl.ops.filter((op) => op[0] === "createFramebuffer").length;
  h.renderer.dispose();
  const deleted = gl.ops.filter((op) => op[0] === "deleteFramebuffer").length;
  assert.ok(deleted >= created - 1 && deleted > 0, `framebuffers deleted: ${deleted} of ${created}`);
});

function glslangPath() {
  try { execFileSync("glslangValidator", ["--version"], { stdio: "ignore" }); return "glslangValidator"; } catch (_e) { return ""; }
}
const glslang = glslangPath();
function validateGLSL(code, stage, name) {
  const dir = fs.mkdtempSync(path.join(require("node:os").tmpdir(), "ocean-fft-gl-"));
  const file = path.join(dir, name + "." + stage);
  fs.writeFileSync(file, code);
  try { execFileSync(glslang, [file], { stdio: "pipe" }); } catch (e) { throw new Error(name + ": " + String(e.stdout)); } finally { fs.rmSync(dir, { recursive: true, force: true }); }
}

test("every gather kernel and both patched ocean shaders pass glslangValidator as GLSL ES 3.00", { skip: glslang ? false : "glslangValidator is not available" }, () => {
  const context = glContext();
  const kernels = context.window.__gosx_scene3d_elio_ocean.kernels;
  for (const name of ["oceanSpectrum", "oceanEvolve", "oceanAssemble", "oceanFoam", "fftGather64", "fftGather128", "fftGather256"]) validateGLSL(kernels[name].glsl, "frag", name);
  const { h, gl } = rendererWithFFT();
  h.renderer.render(sceneBundle({}), { width: 320, height: 180 });
  const sources = [].concat(oceanProgramSources(gl)).map((s) => (typeof s === "string" ? s : s.source || ""));
  const [vertex, fragment] = [sources.find((s) => /gl_VertexID \/ 6/.test(s)), sources.find((s) => /fragColor/.test(s))];
  assert.ok(vertex && fragment);
  validateGLSL(vertex, "vert", "ocean-fft-vertex");
  validateGLSL(fragment, "frag", "ocean-fft-fragment");
  h.renderer.dispose();
});

test("the shipped (packed) WebGL bundle still accepts the spectral surface patch", () => {
  const { h, mount } = rendererWithFFT({ fresh: false });
  h.renderer.render(sceneBundle({ windSpeed: 9 }), { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface+spectrum");
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean-fft-fallback"), null);
  h.renderer.dispose();
});

test("the surface uniform matches the WebGPU scheduler's byte for byte", () => {
  const context = glContext({ GPUBufferUsage: { MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512 },
    GPUTextureUsage: { COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } });
  const fake = makeFakeGPUDevice(), validation = new Map();
  const host = { device: fake.device, validation, guard: { disabled: new Set(), fail() {} }, camera: { x: 0, z: 0 }, tier: "high", time: 0,
    create(device, kind, descriptor) { const handle = { label: descriptor.label }; validation.set(handle, { ready: true }); return handle; } };
  const env = ocean({ windSpeed: 13, seed: 5 });
  const api = context.window.__gosx_scene3d_ocean_fft_api;
  api.frame({}, Object.assign({ environment: { ocean: env }, encoder: fake.device.createCommandEncoder() }, host));
  const resources = {};
  api.frame(resources, Object.assign({ environment: { ocean: env }, encoder: fake.device.createCommandEncoder() }, host));
  const write = fake.state.writeBufferCalls.filter((w) => w.buffer.size === 96).pop();
  assert.ok(write, "the WebGPU scheduler wrote its surface uniform");
  const gpu = Array.from(new Float32Array(write.data.buffer || write.data, write.data.byteOffset || 0, 24));
  const gl = new RecordingGL(), res = {};
  const glApi = context.window.__gosx_scene3d_ocean_fft_gl_api;
  glApi.frame(res, gl, { ocean: env, tier: "high", time: 0, camera: { x: 0, z: 0 }, caps: floatCaps });
  const program = gl.createProgram();
  program.shaders.push({ source: "uniform vec4 u_sp[6]; uniform sampler2D u_spDisp0, u_spDisp1, u_spDisp2, u_spDisp3, u_spSlope0, u_spSlope1, u_spSlope2, u_spSlope3, u_spFoam;" });
  res.fft.bind(gl, program);
  assert.deepEqual(Array.from(program.uniforms.get("u_sp[0]")), gpu);
  assert.deepEqual([0, 1, 2, 3].map((i) => program.uniforms.get("u_spDisp" + i)), [4, 5, 6, 7]);
  assert.deepEqual([0, 1, 2, 3].map((i) => program.uniforms.get("u_spSlope" + i)), [8, 9, 10, 11]);
  assert.equal(program.uniforms.get("u_spFoam"), 12);
});
