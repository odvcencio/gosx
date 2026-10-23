"use strict";
// Shared test doubles and loaders for the Elio graph runner tests (not a test file).
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { createRequire } = require("node:module");
const ts = createRequire(path.join(__dirname, "..", "runtime", "package.json"))("typescript");

const scene3d = path.join(__dirname, "..", "runtime", "scene3d");
const transpile = (file) => ts.transpileModule(fs.readFileSync(path.join(scene3d, file), "utf8"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
function loadSources(context, files) {
  context.window ||= context;
  for (const file of files) vm.runInContext(transpile(file), context, { filename: file });
}
const RUNNER_FILES = ["elio-graph.ts", "elio-graph-gather.ts", "elio-graph-compute.ts"];
function runnerContext(extra) {
  const context = vm.createContext(Object.assign({ Math, Number, Object, Array, Float32Array, Uint32Array, Uint8Array, ArrayBuffer, DataView, Error, Set, Map, JSON, Promise, console }, extra));
  context.window = context;
  loadSources(context, ["ocean-fft-kernels.ts", ...RUNNER_FILES]);
  return context;
}

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
  framebufferTexture2D(target, attachment, texTarget, t, level) { this.drawFB.attachments[attachment - GLC.COLOR_ATTACHMENT0] = t; (this.attachmentLevels ||= {})[attachment - GLC.COLOR_ATTACHMENT0] = level; }
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
    if (kernel && ubo) { const v = new DataView(ubo.buffer.data.buffer, ubo.offset); record.u32 = [v.getUint32(0, true), v.getUint32(4, true), v.getUint32(8, true), v.getUint32(12, true)]; if (ubo.size >= 24) record.f32 = [v.getFloat32(16, true), v.getFloat32(20, true)]; }
    this.draws.push(record);
  }
  live(kind) { return this.created[kind].filter((o) => !o.deleted); }
}



const floatCaps = (gl) => ({ colorBufferFloat: Boolean(gl.getExtension("EXT_color_buffer_float")) });

// Hand-built graphs that follow the contract exactly, over kernels in the generated module. Sizes are for n = 64.
const PARAMS = [
  { name: "windSpeed", type: "f32", source: { kind: "value", value: 9 }, tween: 2 },
  { name: "lambda", type: "f32", source: { kind: "value", value: 0.6 } },
  { name: "mips", type: "bool", source: { kind: "value", value: 1 } },
  { name: "t", type: "f32", source: { kind: "frame", name: "time" } },
  { name: "dt", type: "f32", source: { kind: "frame", name: "dt" } },
  { name: "tmod", type: "f32", source: { kind: "expr", op: "mod", args: ["param:t", 1000] } },
];
function testGraph(backend) {
  const gl = backend === "webgl2";
  const buffer = (name, elementType) => gl ? { name, kind: "buffer", elementType, count: 4096, width: 64 } : { name, kind: "buffer", elementType, count: 4096 };
  const tex = (name) => ({ name, kind: "texture", format: "rgba16float", width: 64, height: 64, mipLevels: 5 });
  const target = { width: 64, height: 64, elioWidth: 64 };
  const where = gl ? { target } : { dispatch: [64, 1, 1] };
  const evolve = { name: "evolve", type: "kernel", kernel: "ocean_evolve", entry: "ocean_evolve", when: "changed:windSpeed", ...where,
    bindings: [{ name: "h0", resource: "h0" }, { name: "omega", resource: "omega" }, { name: "outA", resource: "a0" }, { name: "outB", resource: "b0" }],
    uniforms: [{ name: "ev", values: { n: 64, t: "param:tmod", tile: 250, loopPeriod: 1000 } }] };
  const assemble = { name: "assemble", type: "kernel", kernel: "ocean_assemble", entry: "ocean_assemble", when: "always", ...where,
    bindings: [{ name: "fieldA", resource: "a0" }, { name: "fieldB", resource: "b0" }, { name: "disp", resource: "disp", level: 0 }, { name: "slope", resource: "slope", level: 0 }],
    uniforms: [{ name: "assem", values: { n: 64, lambda: "param:lambda" } }] };
  const passes = [evolve];
  if (gl) {
    passes.push({ name: "fft-a", type: "kernel", kernel: "fft_gather_64", entry: "fft_stage", when: "always", target,
      bindings: [{ name: "src", resource: "a0" }, { name: "dst", resource: "a1" }], uniforms: [{ name: "fft", values: { n: 64, stage: 0, axis: 0 } }] });
    passes.push({ name: "fft-b", type: "kernel", kernel: "fft_gather_64", entry: "fft_stage", when: "always", target,
      bindings: [{ name: "src", resource: "a1" }, { name: "dst", resource: "a0" }], uniforms: [{ name: "fft", values: { n: 64, stage: 0, axis: 1 } }] });
  } else {
    passes.push({ name: "fft-rows", type: "kernel", kernel: "fft_compute_64", entry: "fft_rows", when: "always", dispatch: [64, 1, 1], bindings: [{ name: "data", resource: "a0" }], uniforms: [] });
    passes.push({ name: "fft-cols", type: "kernel", kernel: "fft_compute_64", entry: "fft_cols", when: "always", dispatch: [64, 1, 1], bindings: [{ name: "data", resource: "a0" }], uniforms: [] });
  }
  passes.push(assemble);
  if (gl) passes.push({ name: "mip-disp", type: "mipmap", resource: "disp", when: "param:mips" }, { name: "mip-slope", type: "mipmap", resource: "slope", when: "param:mips" });
  return { name: "test", backend, tier: "high", version: 1, params: PARAMS,
    resources: [buffer("h0", "vec4<f32>"), buffer("omega", "f32"), buffer("a0", "vec4<f32>"), buffer("a1", "vec4<f32>"), buffer("b0", "vec4<f32>"), tex("disp"), tex("slope")],
    passes, frameSwaps: [], exports: { disp0: "disp", slope0: "slope" } };
}
module.exports = { testGraph, PARAMS, RecordingGL, GLC, FORMAT, transpile, loadSources, runnerContext, floatCaps, RUNNER_FILES };
