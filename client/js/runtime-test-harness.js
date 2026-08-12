"use strict";
// Shared harness for the runtime-NN-*.test.js files.
//
// client/js/runtime.test.js used to hold 562 tests in one 33,000-line file.
// Nobody could run a subset, and a diff against it showed no subsystem. The
// tests now live in runtime-NN-*.test.js files, and every piece of setup they
// share lives here exactly once. Copies of a fake DOM or a sandbox builder
// drift apart, so this module is the single definition of each one.
//
// The module holds four kinds of setup:
//   1. Bundle and source readers, so bundle paths are resolved in one place.
//   2. Fake DOM, canvas, WebGL and WebGPU classes.
//   3. createContext(), the sandbox builder every runtime test mounts into.
//   4. Subsystem fixture factories for scenes, water, boards and video.
//
// Requiring this module registers a root afterEach hook that disposes every
// sandbox a test opened. Each test file runs in its own process, so the hook
// is registered once per process.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const bootstrapSource = fs.readFileSync(path.join(__dirname, "bootstrap.js"), "utf8");
const bootstrapLiteSource = fs.readFileSync(path.join(__dirname, "bootstrap-lite.js"), "utf8");
const bootstrapRuntimeSource = fs.readFileSync(path.join(__dirname, "bootstrap-runtime.js"), "utf8");
const bootstrapFeatureTextLayoutSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-textlayout.js"), "utf8");
const bootstrapFeatureIslandsSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-islands.js"), "utf8");
const bootstrapFeatureEnginesSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-engines.js"), "utf8");
const bootstrapFeatureHubsSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-hubs.js"), "utf8");
const bootstrapFeatureScene3DSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d.js"), "utf8");
const bootstrapFeatureScene3DCommandSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-command.js"), "utf8");
const bootstrapFeatureScene3DComputeSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-compute.js"), "utf8");
const bootstrapFeatureScene3DDecompressSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-decompress.js"), "utf8");
const bootstrapFeatureScene3DWebGLSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-webgl.js"), "utf8");
const bootstrapFeatureScene3DWebGPUSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d-webgpu.js"), "utf8");
const bootstrapScene3DWebGPUSourceFile = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
const bootstrapScene3DInputSourceFile = fs.readFileSync(path.join(__dirname, "bootstrap-src", "17-scene-input.js"), "utf8");
const bootstrapScene3DMountSourceFile = readSceneMountSrc();
const bootstrapScene3DDOMRegionsSourceFile = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "dom-regions.ts"), "utf8");
const hostCompatibilitySource = fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "compatibility.ts"), "utf8");
const patchSource = [
  hostCompatibilitySource,
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "patch.ts"), "utf8"),
].join("\n");
const stripeBridgeSource = [
  hostCompatibilitySource,
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "stripe-bridge.ts"), "utf8"),
].join("\n");
const navigationSource = [
  hostCompatibilitySource,
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "navigation.ts"), "utf8"),
].join("\n");

function bootstrapSourceMapSource(mapName, sourceName) {
  const sourceMap = JSON.parse(fs.readFileSync(path.join(__dirname, mapName), "utf8"));
  const index = sourceMap.sources.indexOf(sourceName);
  assert.notEqual(index, -1, `${sourceName} missing from ${mapName}`);
  return String(sourceMap.sourcesContent[index] || "");
}

const ELEMENT_NODE = 1;
const TEXT_NODE = 3;
const DOCUMENT_FRAGMENT_NODE = 11;
const activeTestContexts = new Set();

function scene3DCommandFetchRoutes(extra = {}) {
  return Object.assign({
    "/gosx/bootstrap-feature-scene3d-command.js": { text: bootstrapFeatureScene3DCommandSource },
  }, extra);
}

test.afterEach(async () => {
  const contexts = Array.from(activeTestContexts);
  activeTestContexts.clear();
  for (const env of contexts) {
    await disposeRuntimeTestContext(env);
  }
});

async function disposeRuntimeTestContext(env) {
  const context = env && env.context;
  if (!context) {
    return;
  }
  if (typeof context.__gosx_dispose_page === "function") {
    await context.__gosx_dispose_page();
    return;
  }
  if (context.__gosx && context.__gosx.engines && typeof context.__gosx_dispose_engine === "function") {
    for (const engineID of Array.from(context.__gosx.engines.keys())) {
      context.__gosx_dispose_engine(engineID);
    }
  }
}

class FakeTextNode {
  constructor(text, ownerDocument) {
    this.nodeType = TEXT_NODE;
    this.parentNode = null;
    this.ownerDocument = ownerDocument;
    this._text = String(text == null ? "" : text);
  }

  get textContent() {
    return this._text;
  }

  set textContent(value) {
    this._text = String(value == null ? "" : value);
  }

  cloneNode() {
    return new FakeTextNode(this._text, this.ownerDocument);
  }
}

class FakeDocumentFragment {
  constructor(ownerDocument) {
    this.nodeType = DOCUMENT_FRAGMENT_NODE;
    this.ownerDocument = ownerDocument;
    this.parentNode = null;
    this.childNodes = [];
  }

  get firstChild() {
    return this.childNodes[0] || null;
  }

  appendChild(node) {
    if (node.parentNode) {
      node.parentNode.removeChild(node);
    }
    node.parentNode = this;
    this.childNodes.push(node);
    return node;
  }

  removeChild(node) {
    const idx = this.childNodes.indexOf(node);
    if (idx >= 0) {
      this.childNodes.splice(idx, 1);
      node.parentNode = null;
    }
    return node;
  }

  cloneNode(deep) {
    const clone = new FakeDocumentFragment(this.ownerDocument);
    if (deep) {
      for (const child of this.childNodes) {
        clone.appendChild(child.cloneNode(true));
      }
    }
    return clone;
  }
}

function fakeHTMLText(value) {
  return String(value == null ? "" : value)
    .replace(/<script\b[^<]*(?:(?!<\/script>)<[^<]*)*<\/script>/gi, "")
    .replace(/<style\b[^<]*(?:(?!<\/style>)<[^<]*)*<\/style>/gi, "")
    .replace(/<[^>]*>/g, "")
    .replace(/&nbsp;/g, "\u00a0")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'");
}

class FakeCanvasContext2D {
  constructor(ownerDocument) {
    this.ownerDocument = ownerDocument;
    this.font = "10px sans-serif";
    this.fillStyle = "";
    this.strokeStyle = "";
    this.lineWidth = 1;
    this.imageSmoothingEnabled = true;
    this.lastImageData = null;
    this.ops = [];
  }

  beginPath() { this.ops.push(["beginPath"]); }
  arc(x, y, radius, startAngle, endAngle) {
    this.ops.push(["arc", x, y, radius, startAngle, endAngle]);
  }
  clearRect(x, y, width, height) { this.ops.push(["clearRect", x, y, width, height]); }
  closePath() { this.ops.push(["closePath"]); }
  fill() { this.ops.push(["fill"]); }
  fillRect(x, y, width, height) { this.ops.push(["fillRect", x, y, width, height]); }
  lineTo(x, y) { this.ops.push(["lineTo", x, y]); }
  moveTo(x, y) { this.ops.push(["moveTo", x, y]); }
  restore() { this.ops.push(["restore"]); }
  save() { this.ops.push(["save"]); }
  scale(x, y) { this.ops.push(["scale", x, y]); }
  stroke() { this.ops.push(["stroke"]); }
  translate(x, y) { this.ops.push(["translate", x, y]); }
  createImageData(width, height) {
    const data = new Uint8ClampedArray(Math.max(0, width * height * 4));
    const imageData = { width, height, data };
    this.ops.push(["createImageData", width, height]);
    return imageData;
  }
  putImageData(imageData, x, y) {
    this.lastImageData = {
      width: imageData && imageData.width,
      height: imageData && imageData.height,
      data: Uint8ClampedArray.from(imageData && imageData.data ? imageData.data : []),
      x,
      y,
    };
    this.ops.push(["putImageData", x, y, this.lastImageData.width, this.lastImageData.height]);
  }
  measureText(text) {
    const value = String(text == null ? "" : text);
    this.ops.push(["measureText", this.font, value]);
    if (this.ownerDocument && typeof this.ownerDocument.measureText === "function") {
      return { width: this.ownerDocument.measureText(value, this.font) };
    }
    return { width: value.length * 8 };
  }
}

class FakeWebGLContext {
  constructor(options = {}) {
    this.ops = [];
    this.programs = [];
    this.bufferUploads = new Map();
    this.bufferByteSizes = new Map();
    this.textureUploads = new Map();
    this._nextBufferID = 1;
    this._nextTextureID = 1;
    this._nextProgramID = 1;
    this._boundArrayBuffer = null;
    this._boundTexture = null;
    this._activeProgram = null;
    this._rejectShaderSources = Array.isArray(options.rejectShaderSources) ? options.rejectShaderSources : [];
    this._vendor = typeof options.vendor === "string" ? options.vendor : "FakeGPU Inc.";
    this._renderer = typeof options.renderer === "string" ? options.renderer : "FakeGPU Renderer";
    // unmaskedVendor/unmaskedRenderer let a test simulate a browser that
    // masks the PLAIN gl.VENDOR/gl.RENDERER query (vendor/renderer above)
    // but reveals the real string through WEBGL_debug_renderer_info's
    // UNMASKED_* parameters — distinct from vendor/renderer so a test can
    // assert which path a caller actually used. Default to the same value
    // (no masking) when not given.
    this._unmaskedVendor = typeof options.unmaskedVendor === "string" ? options.unmaskedVendor : this._vendor;
    this._unmaskedRenderer = typeof options.unmaskedRenderer === "string" ? options.unmaskedRenderer : this._renderer;
    this._disableDebugRendererInfo = options.debugRendererInfo === false;
    this.debugRendererInfoRequests = 0;
    this.POINTS = 0x0000;
    this.ARRAY_BUFFER = 0x8892;
    this.STATIC_DRAW = 0x88E4;
    this.DYNAMIC_DRAW = 0x88E8;
    this.FLOAT = 0x1406;
    this.LINES = 0x0001;
    this.TRIANGLES = 0x0004;
    this.COLOR_BUFFER_BIT = 0x4000;
    this.DEPTH_BUFFER_BIT = 0x0100;
    this.BLEND = 0x0BE2;
    this.DEPTH_TEST = 0x0B71;
    this.CULL_FACE = 0x0B44;
    this.LEQUAL = 0x0203;
    this.FRONT = 0x0404;
    this.BACK = 0x0405;
    this.ONE = 1;
    this.SRC_ALPHA = 0x0302;
    this.ONE_MINUS_SRC_ALPHA = 0x0303;
    this.TEXTURE_2D = 0x0DE1;
    this.TEXTURE0 = 0x84C0;
    this.TEXTURE_MIN_FILTER = 0x2801;
    this.TEXTURE_MAG_FILTER = 0x2800;
    this.TEXTURE_WRAP_S = 0x2802;
    this.TEXTURE_WRAP_T = 0x2803;
    this.CLAMP_TO_EDGE = 0x812F;
    this.LINEAR = 0x2601;
    this.RGBA = 0x1908;
    this.UNSIGNED_BYTE = 0x1401;
    this.UNSIGNED_INT = 0x1405;
    this.FRAMEBUFFER = 0x8D40;
    this.DEPTH_ATTACHMENT = 0x8D00;
    this.DEPTH_COMPONENT = 0x1902;
    this.DEPTH_COMPONENT24 = 0x81A6;
    this.VERTEX_SHADER = 0x8B31;
    this.FRAGMENT_SHADER = 0x8B30;
    this.COMPILE_STATUS = 0x8B81;
    this.LINK_STATUS = 0x8B82;
    this.VENDOR = 0x1F00;
    this.RENDERER = 0x1F01;
    // WebGL2 additional constants
    this.RENDERBUFFER = 0x8D41;
    this.COLOR_ATTACHMENT0 = 0x8CE0;
    this.RGBA16F = 0x881A;
    this.RGBA8 = 0x8058;
    this.NEAREST = 0x2600;
    this.TRIANGLE_STRIP = 0x0005;
    this.RED = 0x1903;
    this.R32F = 0x822E;
    this.RGBA32F = 0x8814;
    this.TEXTURE_CUBE_MAP = 0x8513;
    this.DEPTH_COMPONENT16 = 0x81A5;
    this.UNSIGNED_SHORT = 0x1403;
    this.INT = 0x1404;
    this.COLOR_ATTACHMENT1 = 0x8CE1;
    this.DRAW_FRAMEBUFFER = 0x8CA9;
    this.READ_FRAMEBUFFER = 0x8CA8;
    this.COLOR = 0x1800;
    this.BUFFER_SIZE = 0x8764;
  }

  createShader(type) {
    const shader = { type };
    this.ops.push(["createShader", type]);
    return shader;
  }

  shaderSource(shader, source) {
    shader.source = source;
    this.ops.push(["shaderSource", shader.type, source.length]);
  }

  compileShader(shader) {
    shader.compiled = true;
    this.ops.push(["compileShader", shader.type]);
  }

  // rejectShaderSources lets a test drive the REAL compile-failure path: any
  // shader whose source contains one of the listed substrings reports a failed
  // compile, exactly as a driver that rejects the GLSL would.
  getShaderParameter(shader, param) {
    if (param !== this.COMPILE_STATUS) {
      return false;
    }
    const source = String(shader && shader.source || "");
    for (const needle of this._rejectShaderSources) {
      if (needle && source.includes(needle)) {
        return false;
      }
    }
    return true;
  }

  createProgram() {
    const program = { id: this._nextProgramID++, attached: [] };
    this.programs.push(program);
    this.ops.push(["createProgram", program.id]);
    return program;
  }

  // programShaderSources returns the concatenated GLSL of every shader ever
  // attached to `program`. Tests use it to name a program by what it draws
  // ("the one carrying the Selena fragment body") rather than by creation
  // order, which shifts whenever the renderer gains another built-in program.
  programShaderSources(program) {
    if (!program || !Array.isArray(program.attached)) {
      return "";
    }
    return program.attached.map((shader) => String(shader && shader.source || "")).join("\n");
  }

  // programMatching finds the single created program whose attached shader
  // sources contain `needle`.
  programMatching(needle) {
    return this.programs.find((program) => this.programShaderSources(program).includes(needle)) || null;
  }

  attachShader(program, shader) {
    program.attached.push(shader);
    this.ops.push(["attachShader", shader.type]);
  }

  linkProgram(program) {
    program.linked = true;
    this.ops.push(["linkProgram", program.attached.length]);
  }

  getProgramParameter(_program, param) {
    return param === this.LINK_STATUS;
  }

  getProgramInfoLog(_program) {
    this.ops.push(["getProgramInfoLog"]);
    return "";
  }

  getShaderInfoLog(_shader) {
    this.ops.push(["getShaderInfoLog"]);
    return "";
  }

  createBuffer() {
    const buffer = { id: this._nextBufferID++ };
    this.ops.push(["createBuffer", buffer.id]);
    return buffer;
  }

  createTexture() {
    const texture = { id: this._nextTextureID++ };
    this.ops.push(["createTexture", texture.id]);
    return texture;
  }

  createFramebuffer() {
    const framebuffer = { id: "fb-" + this.ops.length };
    this.ops.push(["createFramebuffer", framebuffer.id]);
    return framebuffer;
  }

  getAttribLocation(_program, name) {
    this.ops.push(["getAttribLocation", name]);
    if (name === "a_position") return 0;
    if (name === "a_color") return 1;
    if (name === "a_material") return 2;
    if (name === "a_uv") return 3;
    if (name === "a_instanceMatrix") return 4;
    if (name === "a_joints") return 7;
    if (name === "a_weights") return 8;
    if (name === "a_normal") return 5;
    if (name === "a_instanceColor") return 9;
    if (name === "a_size") return 10;
    return -1;
  }

  getUniformLocation(_program, name) {
    this.ops.push(["getUniformLocation", name]);
    return { name };
  }

  viewport(x, y, width, height) {
    this.ops.push(["viewport", x, y, width, height]);
  }

  clearColor(r, g, b, a) {
    this.ops.push(["clearColor", r, g, b, a]);
  }

  clear(mask) {
    this.ops.push(["clear", mask]);
  }

  clearDepth(value) {
    this.ops.push(["clearDepth", value]);
  }

  useProgram(program) {
    this._activeProgram = program || null;
    this.ops.push(["useProgram", program && program.id]);
  }

  bindBuffer(target, buffer) {
    if (target === this.ARRAY_BUFFER) {
      this._boundArrayBuffer = buffer || null;
    }
    this.ops.push(["bindBuffer", target, buffer && buffer.id]);
  }

  bindTexture(target, texture) {
    if (target === this.TEXTURE_2D) {
      this._boundTexture = texture || null;
    }
    this.ops.push(["bindTexture", target, texture && texture.id]);
  }

  bindFramebuffer(target, framebuffer) {
    this.ops.push(["bindFramebuffer", target, framebuffer && framebuffer.id]);
  }

  framebufferTexture2D(target, attachment, textarget, texture, level) {
    this.ops.push(["framebufferTexture2D", target, attachment, textarget, texture && texture.id, level]);
  }

  activeTexture(unit) {
    this.ops.push(["activeTexture", unit]);
  }

  bufferData(target, data, usage) {
    const bufferID = this._boundArrayBuffer && this._boundArrayBuffer.id;
    if (bufferID != null) {
      this.bufferUploads.set(bufferID, Array.from(data || []));
      this.bufferByteSizes.set(bufferID, data && Number.isFinite(data.byteLength) ? data.byteLength : 0);
    }
    this.ops.push(["bufferData", target, bufferID, data.length, usage]);
  }

  getBufferParameter(target, param) {
    if (target === this.ARRAY_BUFFER && param === this.BUFFER_SIZE) {
      const bufferID = this._boundArrayBuffer && this._boundArrayBuffer.id;
      return bufferID == null ? 0 : this.bufferByteSizes.get(bufferID) || 0;
    }
    return 0;
  }

  bufferSubData(target, offset, data) {
    const bufferID = this._boundArrayBuffer && this._boundArrayBuffer.id;
    this.ops.push(["bufferSubData", target, bufferID, offset, data && data.length]);
  }

  texParameteri(target, pname, param) {
    this.ops.push(["texParameteri", target, pname, param]);
  }

  texImage2D(...args) {
    const textureID = this._boundTexture && this._boundTexture.id;
    this.textureUploads.set(textureID, args.length);
    this.ops.push(["texImage2D", textureID, args.length]);
  }

  enableVertexAttribArray(location) {
    this.ops.push(["enableVertexAttribArray", location]);
  }

  vertexAttribPointer(location, size, type, normalized, stride, offset) {
    this.ops.push(["vertexAttribPointer", location, size, type, normalized, stride, offset]);
  }

  disableVertexAttribArray(location) {
    this.ops.push(["disableVertexAttribArray", location]);
  }

  vertexAttrib2f(location, x, y) {
    this.ops.push(["vertexAttrib2f", location, x, y]);
  }

  vertexAttrib1f(location, x) {
    this.ops.push(["vertexAttrib1f", location, x]);
  }

  vertexAttrib4f(location, x, y, z, w) {
    this.ops.push(["vertexAttrib4f", location, x, y, z, w]);
  }

  drawArrays(mode, first, count) {
    // Element 4 records WHICH program was bound at draw time. Without it a
    // test can only prove that a draw happened, not that the intended shader
    // ran -- the exact blind spot that let a Selena mesh draw through the
    // built-in PBR program undetected.
    this.ops.push(["drawArrays", mode, first, count, this._activeProgram && this._activeProgram.id]);
  }

  // The thick-line world pass (10-runtime-scene-core.js
  // renderSceneWebGLWorldBundle) issues indexed draws. Without this method any
  // scene carrying a LinesGeometry with an explicit width > 1 threw here
  // instead of rendering, so no test could cover a mixed lines+mesh frame.
  drawElements(mode, count, type, offset) {
    this.ops.push(["drawElements", mode, count, type, offset, this._activeProgram && this._activeProgram.id]);
  }

  uniform4f(location, x, y, z, w) {
    this.ops.push(["uniform4f", location && location.name, x, y, z, w]);
  }

  uniform3f(location, x, y, z) {
    this.ops.push(["uniform3f", location && location.name, x, y, z]);
  }

  uniform1f(location, value) {
    this.ops.push(["uniform1f", location && location.name, value]);
  }

  uniform1i(location, value) {
    this.ops.push(["uniform1i", location && location.name, value]);
  }

  uniform2f(location, x, y) {
    this.ops.push(["uniform2f", location && location.name, x, y]);
  }

  uniform1fv(location, value) {
    this.ops.push(["uniform1fv", location && location.name, value && value.length, value && Array.from(value).slice(0, 4)]);
  }

  uniformMatrix4fv(location, transpose, value) {
    this.ops.push(["uniformMatrix4fv", location && location.name, Boolean(transpose), value && value.length]);
  }

  uniformMatrix3fv(location, transpose, value) {
    this.ops.push(["uniformMatrix3fv", location && location.name, Boolean(transpose), value && value.length]);
  }

  enable(capability) {
    this.ops.push(["enable", capability]);
  }

  disable(capability) {
    this.ops.push(["disable", capability]);
  }

  blendFunc(src, dst) {
    this.ops.push(["blendFunc", src, dst]);
  }

  depthFunc(mode) {
    this.ops.push(["depthFunc", mode]);
  }

  depthMask(flag) {
    this.ops.push(["depthMask", flag]);
  }

  cullFace(face) {
    this.ops.push(["cullFace", face]);
  }

  deleteBuffer(buffer) {
    if (buffer && buffer.id != null) {
      this.bufferUploads.delete(buffer.id);
    }
    this.ops.push(["deleteBuffer", buffer && buffer.id]);
  }

  deleteProgram(_program) {
    this.ops.push(["deleteProgram"]);
  }

  deleteShader(shader) {
    this.ops.push(["deleteShader", shader && shader.type]);
  }

  deleteFramebuffer(framebuffer) {
    this.ops.push(["deleteFramebuffer", framebuffer && framebuffer.id]);
  }

  deleteTexture(texture) {
    this.ops.push(["deleteTexture", texture && texture.id]);
  }

  getExtension(name) {
    if (name === "WEBGL_debug_renderer_info") {
      // Track every request regardless of availability — real browsers
      // (Firefox) log the deprecation warning on the mere getExtension()
      // call, so tests assert on this counter to prove callers only reach
      // for the extension when the plain gl.VENDOR/gl.RENDERER query came
      // back masked/empty.
      this.debugRendererInfoRequests += 1;
      if (!this._disableDebugRendererInfo) {
        return {
          UNMASKED_VENDOR_WEBGL: 0x9245,
          UNMASKED_RENDERER_WEBGL: 0x9246,
        };
      }
      return null;
    }
    if (name === "WEBGL_lose_context") {
      return {
        loseContext: () => {
          this.ops.push(["loseContext"]);
        },
      };
    }
    return null;
  }

  getParameter(param) {
    if (param === 0x9245) {
      return this._unmaskedVendor;
    }
    if (param === 0x9246) {
      return this._unmaskedRenderer;
    }
    if (param === this.VENDOR) {
      return this._vendor;
    }
    if (param === this.RENDERER) {
      return this._renderer;
    }
    return null;
  }

  // WebGL2 VAO methods (used by PBR post-processing fullscreen quad setup)
  createVertexArray() {
    const vao = { id: "vao-" + this.ops.length };
    this.ops.push(["createVertexArray", vao.id]);
    return vao;
  }
  bindVertexArray(vao) {
    this.ops.push(["bindVertexArray", vao && vao.id]);
  }
  deleteVertexArray(vao) {
    this.ops.push(["deleteVertexArray", vao && vao.id]);
  }

  // WebGL2 renderbuffer methods (used by post-processing FBO setup)
  createRenderbuffer() {
    const rb = { id: "rb-" + this.ops.length };
    this.ops.push(["createRenderbuffer", rb.id]);
    return rb;
  }
  bindRenderbuffer(target, rb) {
    this.ops.push(["bindRenderbuffer", target, rb && rb.id]);
  }
  renderbufferStorage(target, internalFormat, width, height) {
    this.ops.push(["renderbufferStorage", target, internalFormat, width, height]);
  }
  framebufferRenderbuffer(target, attachment, rbTarget, rb) {
    this.ops.push(["framebufferRenderbuffer", target, attachment, rbTarget, rb && rb.id]);
  }

  // WebGL2 instancing (used by instanced mesh path)
  vertexAttribDivisor(location, divisor) {
    this.ops.push(["vertexAttribDivisor", location, divisor]);
  }
  drawArraysInstanced(mode, first, count, instances) {
    this.ops.push(["drawArraysInstanced", mode, first, count, instances]);
  }
  drawElementsInstanced(mode, count, type, offset, instances) {
    this.ops.push(["drawElementsInstanced", mode, count, type, offset, instances]);
  }

  // WebGL2 draw buffers
  drawBuffers(buffers) {
    this.ops.push(["drawBuffers", buffers && buffers.length]);
  }

  // WebGL2 additional methods
  renderbufferStorageMultisample(target, samples, internalFormat, width, height) {
    this.ops.push(["renderbufferStorageMultisample", target, samples, internalFormat, width, height]);
  }
  blitFramebuffer(sx0, sy0, sx1, sy1, dx0, dy0, dx1, dy1, mask, filter) {
    this.ops.push(["blitFramebuffer", mask]);
  }
  vertexAttribIPointer(location, size, type, stride, offset) {
    this.ops.push(["vertexAttribIPointer", location, size, type, stride, offset]);
  }
  texStorage2D(target, levels, internalFormat, width, height) {
    this.ops.push(["texStorage2D", target, levels, internalFormat, width, height]);
  }
}

// FakeWebGPUCanvasContext is a minimal double for the GPUCanvasContext
// returned by canvas.getContext("webgpu"). It covers exactly what
// sceneWebGPUProbeCanvasContext (16z-scene-webgpu-probe.js) and the real
// WebGPU renderer (16a-scene-webgpu.js) call on a canvas context: configure(),
// getCurrentTexture()/createView(), and unconfigure().
class FakeWebGPUCanvasContext {
  constructor() {
    this.ops = [];
    this.configured = null;
  }

  configure(descriptor) {
    this.configured = descriptor || null;
    this.ops.push(["configure", descriptor || null]);
  }

  unconfigure() {
    this.configured = null;
    this.ops.push(["unconfigure"]);
  }

  getCurrentTexture() {
    this.ops.push(["getCurrentTexture"]);
    return {
      createView() {
        return { __kind: "canvasTextureView" };
      },
    };
  }
}

function fakeElementMatchesSelector(element, selector) {
  if (!element || element.nodeType !== ELEMENT_NODE) {
    return false;
  }
  const groups = String(selector || "").split(",").map((part) => part.trim()).filter(Boolean);
  return groups.some((group) => fakeElementMatchesSelectorGroup(element, group));
}

function fakeElementMatchesSelectorGroup(element, selector) {
  let source = String(selector || "").trim();
  if (!source) {
    return false;
  }
  let rejectedByNot = false;
  source = source.replace(/:not\(([^()]*)\)/g, (_match, inner) => {
    if (fakeElementMatchesSelectorGroup(element, inner)) {
      rejectedByNot = true;
    }
    return "";
  }).trim();
  if (rejectedByNot || !source) {
    return false;
  }

  const tagMatch = source.match(/^[a-zA-Z][a-zA-Z0-9-]*/);
  if (tagMatch && element.tagName.toLowerCase() !== tagMatch[0].toLowerCase()) {
    return false;
  }
  for (const idMatch of source.matchAll(/#([a-zA-Z0-9_-]+)/g)) {
    if (element.id !== idMatch[1]) {
      return false;
    }
  }
  const classAttr = element.getAttribute("class") || "";
  const classes = classAttr.split(/\s+/).filter(Boolean);
  for (const classMatch of source.matchAll(/\.([a-zA-Z0-9_-]+)/g)) {
    if (!classes.includes(classMatch[1])) {
      return false;
    }
  }
  for (const attrMatch of source.matchAll(/\[\s*([^\]\s=]+)\s*(?:=\s*(?:"([^"]*)"|'([^']*)'|([^\]\s]+)))?\s*\]/g)) {
    const name = attrMatch[1];
    const expected = attrMatch[2] ?? attrMatch[3] ?? attrMatch[4];
    if (!element.hasAttribute(name)) {
      return false;
    }
    if (expected != null && element.getAttribute(name) !== expected) {
      return false;
    }
  }
  return true;
}

function fakeElementQuerySelectorAll(root, selector, includeSelf = false) {
  const matches = [];
  const visit = (node, includeNode) => {
    if (!node || node.nodeType !== ELEMENT_NODE) {
      return;
    }
    if (includeNode && fakeElementMatchesSelector(node, selector)) {
      matches.push(node);
    }
    for (const child of node.childNodes || []) {
      visit(child, true);
    }
  };
  visit(root, includeSelf);
  return matches;
}

class FakeElement {
  constructor(tagName, ownerDocument) {
    this.nodeType = ELEMENT_NODE;
    this.tagName = String(tagName || "div").toUpperCase();
    this.ownerDocument = ownerDocument;
    this.parentNode = null;
    this.childNodes = [];
    this.attributes = new Map();
    this.dataset = {};
    this.listeners = new Map();
    this.value = "";
    this.selectionStart = 0;
    this.selectionEnd = 0;
    this.width = 0;
    this.height = 0;
    this.style = {};
    this._canvasContext = null;
    this._webglContext = null;
    this._capturedPointerID = null;
    this.focusCalls = [];
    this.scrollIntoViewCalls = [];
    this.clickCalls = [];
    this.requestSubmitCalls = [];
    this.submitCalls = [];
    this.loadCalls = [];
    this.playCalls = [];
    this.pauseCalls = [];
    this.fullscreenCalls = [];
    this.pointerLockCalls = [];
    this.animateCalls = [];
    this._innerHTML = null;
    this.paused = true;
    this.ended = false;
    this.muted = false;
    this.volume = 1;
    this.playbackRate = 1;
    this.currentTime = 0;
    this.duration = 0;
    this.readyState = 0;
    this.error = null;
    this.buffered = {
      length: 0,
      start() {
        return 0;
      },
      end() {
        return 0;
      },
    };
    this._canPlayTypes = Object.create(null);
  }

  get id() {
    return this.getAttribute("id") || "";
  }

  set id(value) {
    this.setAttribute("id", value);
  }

  get firstChild() {
    return this.childNodes[0] || null;
  }

  get children() {
    return this.childNodes.filter((child) => child.nodeType === ELEMENT_NODE);
  }

  get firstElementChild() {
    return this.children[0] || null;
  }

  get textContent() {
    return this.childNodes.map((child) => child.textContent).join("");
  }

  set textContent(value) {
    this._innerHTML = null;
    this.childNodes = [];
    const textNode = new FakeTextNode(value, this.ownerDocument);
    textNode.parentNode = this;
    this.childNodes.push(textNode);
  }

  get innerHTML() {
    if (this._innerHTML != null) {
      return this._innerHTML;
    }
    return this.childNodes.map((child) => child.textContent).join("");
  }

  set innerHTML(value) {
    this._innerHTML = String(value == null ? "" : value);
    this.childNodes = [];
    const text = fakeHTMLText(this._innerHTML);
    if (text !== "") {
      const textNode = new FakeTextNode(text, this.ownerDocument);
      textNode.parentNode = this;
      this.childNodes.push(textNode);
    }
  }

  appendChild(node) {
    if (node.nodeType === DOCUMENT_FRAGMENT_NODE) {
      while (node.firstChild) {
        this.appendChild(node.firstChild);
      }
      return node;
    }

    if (node.parentNode) {
      node.parentNode.removeChild(node);
    }

    node.parentNode = this;
    this._innerHTML = null;
    if (this.ownerDocument) {
      adoptNode(node, this.ownerDocument);
    }
    this.childNodes.push(node);

    if (node.nodeType === ELEMENT_NODE && this.ownerDocument) {
      this.ownerDocument.indexNode(node);
    }

    if (
      node.nodeType === ELEMENT_NODE &&
      node.tagName === "SCRIPT" &&
      (this.tagName === "HEAD" || this.tagName === "HTML") &&
      this.ownerDocument &&
      typeof this.ownerDocument.scriptLoader === "function"
    ) {
      const src = node.src || node.getAttribute("src");
      if (src) {
        this.ownerDocument.scriptLoader(src, node);
      }
    }

    if (
      node.nodeType === ELEMENT_NODE &&
      node.tagName === "SCRIPT" &&
      this.ownerDocument &&
      typeof this.ownerDocument.inlineScriptLoader === "function" &&
      !node.getAttribute("src") &&
      node.getAttribute("data-gosx-navigation-replayed") === "true"
    ) {
      this.ownerDocument.inlineScriptLoader(node);
    }

    return node;
  }

  removeChild(node) {
    const idx = this.childNodes.indexOf(node);
    if (idx >= 0) {
      this.childNodes.splice(idx, 1);
      node.parentNode = null;
      this._innerHTML = null;
    }
    return node;
  }

  insertBefore(node, before) {
    if (!before) {
      return this.appendChild(node);
    }
    if (node.parentNode) {
      node.parentNode.removeChild(node);
    }
    const idx = this.childNodes.indexOf(before);
    if (idx < 0) {
      return this.appendChild(node);
    }
    node.parentNode = this;
    this._innerHTML = null;
    if (this.ownerDocument) {
      adoptNode(node, this.ownerDocument);
    }
    this.childNodes.splice(idx, 0, node);
    if (node.nodeType === ELEMENT_NODE && this.ownerDocument) {
      this.ownerDocument.indexNode(node);
    }
    if (
      node.nodeType === ELEMENT_NODE &&
      node.tagName === "SCRIPT" &&
      this.ownerDocument &&
      typeof this.ownerDocument.inlineScriptLoader === "function" &&
      !node.getAttribute("src") &&
      node.getAttribute("data-gosx-navigation-replayed") === "true"
    ) {
      this.ownerDocument.inlineScriptLoader(node);
    }
    return node;
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
    if (String(name).startsWith("data-")) {
      const datasetKey = String(name)
        .slice(5)
        .replace(/-([a-z])/g, (_match, letter) => letter.toUpperCase());
      this.dataset[datasetKey] = String(value);
    }
    if (name === "id" && this.ownerDocument) {
      this.ownerDocument.indexNode(this);
    }
  }

  getAttribute(name) {
    return this.attributes.has(name) ? this.attributes.get(name) : null;
  }

  hasAttribute(name) {
    return this.attributes.has(name);
  }

  matches(selector) {
    return fakeElementMatchesSelector(this, selector);
  }

  querySelectorAll(selector) {
    return fakeElementQuerySelectorAll(this, selector, false);
  }

  querySelector(selector) {
    return this.querySelectorAll(selector)[0] || null;
  }

  removeAttribute(name) {
    this.attributes.delete(name);
  }

  addEventListener(type, listener, capture) {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, []);
    }
    this.listeners.get(type).push({ listener, capture: Boolean(capture) });
  }

  removeEventListener(type, listener, capture) {
    const current = this.listeners.get(type) || [];
    this.listeners.set(
      type,
      current.filter((entry) => entry.listener !== listener || entry.capture !== Boolean(capture)),
    );
  }

  listenerCount(type) {
    return (this.listeners.get(type) || []).length;
  }

  contains(node) {
    let current = node;
    while (current) {
      if (current === this) {
        return true;
      }
      current = current.parentNode;
    }
    return false;
  }

  focus(...args) {
    this.focusCalls.push(args);
    this.ownerDocument.activeElement = this;
  }

  scrollIntoView(...args) {
    this.scrollIntoViewCalls.push(args);
  }

  getBoundingClientRect() {
    return {
      left: 0,
      top: 0,
      width: this.width,
      height: this.height,
      right: this.width,
      bottom: this.height,
    };
  }

  getContext(kind, options) {
    if (this.tagName !== "CANVAS") {
      return null;
    }
    this.lastContextKind = kind;
    this.lastContextOptions = options || null;
    this.contextCalls = this.contextCalls || [];
    this.contextCalls.push({ kind, options: options || null });
    if (kind === "2d") {
      if (this.ownerDocument && this.ownerDocument.disableCanvas2D) {
        return null;
      }
      if (!this._canvasContext) {
        this._canvasContext = new FakeCanvasContext2D(this.ownerDocument);
      }
      return this._canvasContext;
    }
    if ((kind === "webgl" || kind === "experimental-webgl") && this.ownerDocument && typeof this.ownerDocument.createWebGLContext === "function") {
      if (!this._webglContext) {
        this._webglContext = this.ownerDocument.createWebGLContext();
      }
      return this._webglContext;
    }
    if (kind === "webgl2" && this.ownerDocument && typeof this.ownerDocument.createWebGL2Context === "function") {
      if (!this._webglContext) {
        this._webglContext = this.ownerDocument.createWebGL2Context();
      }
      return this._webglContext;
    }
    if (kind === "webgpu" && this.ownerDocument && typeof this.ownerDocument.createWebGPUContext === "function") {
      if (!this._webgpuContext) {
        this._webgpuContext = this.ownerDocument.createWebGPUContext();
      }
      return this._webgpuContext;
    }
    return null;
  }

  setPointerCapture(pointerID) {
    this._capturedPointerID = pointerID;
  }

  releasePointerCapture(pointerID) {
    if (this._capturedPointerID === pointerID) {
      this._capturedPointerID = null;
    }
  }

  dispatchEvent(event) {
    const listeners = this.listeners.get(event.type) || [];
    for (const entry of listeners) {
      entry.listener(event);
    }
    return true;
  }

  click() {
    this.clickCalls.push([]);
  }

  requestSubmit(submitter) {
    this.requestSubmitCalls.push([submitter || null]);
  }

  submit() {
    this.submitCalls.push([]);
  }

  load() {
    this.loadCalls.push([]);
  }

  play() {
    this.playCalls.push([]);
    this.paused = false;
    return Promise.resolve();
  }

  pause() {
    this.pauseCalls.push([]);
    this.paused = true;
  }

  animate(keyframes, options) {
    const animation = {
      keyframes,
      options,
      cancelled: false,
      cancel() {
        this.cancelled = true;
      },
      finished: Promise.resolve(),
    };
    this.animateCalls.push(animation);
    return animation;
  }

  canPlayType(type) {
    return this._canPlayTypes[String(type)] || "";
  }

  setCanPlayType(type, value) {
    this._canPlayTypes[String(type)] = String(value == null ? "" : value);
  }

  requestFullscreen() {
    this.fullscreenCalls.push([]);
    if (this.ownerDocument) {
      this.ownerDocument.fullscreenElement = this;
      this.ownerDocument.dispatchEvent({ type: "fullscreenchange", target: this });
    }
    return Promise.resolve();
  }

  requestPointerLock(options) {
    this.pointerLockCalls.push([options || null]);
    if (this.ownerDocument) {
      this.ownerDocument.pointerLockElement = this;
      this.ownerDocument.dispatchEvent({ type: "pointerlockchange", target: this });
    }
    return undefined;
  }

  cloneNode(deep) {
    const clone = new FakeElement(this.tagName.toLowerCase(), this.ownerDocument);
    for (const [name, value] of this.attributes.entries()) {
      clone.setAttribute(name, value);
    }
    clone.value = this.value;
    clone.selectionStart = this.selectionStart;
    clone.selectionEnd = this.selectionEnd;
    clone.paused = this.paused;
    clone.ended = this.ended;
    clone.muted = this.muted;
    clone.volume = this.volume;
    clone.playbackRate = this.playbackRate;
    clone.currentTime = this.currentTime;
    clone.duration = this.duration;
    clone.readyState = this.readyState;
    clone.error = this.error;
    clone.buffered = this.buffered;
    clone._canPlayTypes = Object.assign(Object.create(null), this._canPlayTypes);
    if (deep) {
      for (const child of this.childNodes) {
        clone.appendChild(child.cloneNode(true));
      }
    }
    return clone;
  }
}

function adoptNode(node, ownerDocument) {
  node.ownerDocument = ownerDocument;
  if (node.nodeType === ELEMENT_NODE) {
    for (const child of node.childNodes) {
      adoptNode(child, ownerDocument);
    }
  }
}

class FakeDocument {
  constructor() {
    this.readyState = "complete";
    this.visibilityState = "visible";
    this.byID = new Map();
    this.eventListeners = new Map();
    this.dispatchedEvents = [];
    this.documentElement = new FakeElement("html", this);
    this.head = new FakeElement("head", this);
    this.body = new FakeElement("body", this);
    this.documentElement.appendChild(this.head);
    this.documentElement.appendChild(this.body);
    this.activeElement = this.body;
    this.fullscreenElement = null;
    this.pointerLockElement = null;
    this.title = "";
    this.disableCanvas2D = false;
    this.createWebGLContext = null;
    // Set by createContext to simulate <script src> loading from fetchRoutes.
    this.scriptLoader = null;
  }

  createElement(tagName) {
    return new FakeElement(tagName, this);
  }

  createTextNode(text) {
    return new FakeTextNode(text, this);
  }

  createDocumentFragment() {
    return new FakeDocumentFragment(this);
  }

  getElementById(id) {
    return this.byID.get(id) || null;
  }

  querySelectorAll(selector) {
    return fakeElementQuerySelectorAll(this.documentElement, selector, true);
  }

  querySelector(selector) {
    return this.querySelectorAll(selector)[0] || null;
  }

  addEventListener(type, listener) {
    if (!this.eventListeners.has(type)) {
      this.eventListeners.set(type, []);
    }
    this.eventListeners.get(type).push(listener);
  }

  removeEventListener(type, listener) {
    const current = this.eventListeners.get(type) || [];
    this.eventListeners.set(
      type,
      current.filter((entry) => entry !== listener),
    );
  }

  dispatchEvent(event) {
    this.dispatchedEvents.push(event);
    const listeners = this.eventListeners.get(event.type) || [];
    for (const listener of listeners) {
      listener(event);
    }
    return true;
  }

  exitFullscreen() {
    this.fullscreenElement = null;
    this.dispatchEvent({ type: "fullscreenchange", target: this });
    return Promise.resolve();
  }

  exitPointerLock() {
    this.pointerLockElement = null;
    this.dispatchEvent({ type: "pointerlockchange", target: this });
  }

  indexNode(node) {
    if (node.nodeType !== ELEMENT_NODE) {
      return;
    }
    if (node.id) {
      this.byID.set(node.id, node);
    }
    for (const child of node.children) {
      this.indexNode(child);
    }
  }
}

class FakeFontSet {
  constructor() {
    this.listeners = new Map();
    this.ready = null;
  }

  addEventListener(type, listener) {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, []);
    }
    this.listeners.get(type).push(listener);
  }

  dispatch(type) {
    const listeners = this.listeners.get(type) || [];
    for (const listener of listeners) {
      listener({ type });
    }
  }
}

class FakeResizeObserver {
  constructor(callback) {
    this.callback = callback;
    this.targets = new Set();
  }

  observe(target) {
    this.targets.add(target);
  }

  unobserve(target) {
    this.targets.delete(target);
  }

  disconnect() {
    this.targets.clear();
  }

  trigger(targets) {
    const list = Array.isArray(targets) && targets.length > 0 ? targets : Array.from(this.targets);
    this.callback(list.map((target) => ({
      target,
      contentRect: target && typeof target.getBoundingClientRect === "function"
        ? target.getBoundingClientRect()
        : { width: 0, height: 0 },
    })));
  }
}

class FakeIntersectionObserver {
  constructor(callback, options = {}) {
    this.callback = callback;
    this.options = options;
    this.targets = new Set();
  }

  observe(target) {
    this.targets.add(target);
  }

  disconnect() {
    this.targets.clear();
  }

  trigger(entries) {
    let list = entries;
    if (!Array.isArray(list) || list.length === 0) {
      list = Array.from(this.targets).map((target) => ({
        target,
        isIntersecting: true,
        intersectionRatio: 1,
      }));
    } else if (list[0] && !Object.prototype.hasOwnProperty.call(list[0], "target")) {
      list = list.map((target) => ({
        target,
        isIntersecting: true,
        intersectionRatio: 1,
      }));
    }
    this.callback(list);
  }
}

class FakeMutationObserver {
  constructor(callback) {
    this.callback = callback;
    this.targets = new Set();
    this.options = [];
  }

  observe(target, options = {}) {
    this.targets.add(target);
    this.options.push({ target, options });
  }

  disconnect() {
    this.targets.clear();
    this.options = [];
  }

  trigger(records) {
    const list = Array.isArray(records) && records.length > 0 ? records : Array.from(this.targets).map((target) => ({
      target,
      type: "attributes",
      attributeName: "class",
    }));
    this.callback(list);
  }
}

class FakeListenerTarget {
  constructor() {
    this.listeners = new Map();
  }

  addEventListener(type, listener) {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, []);
    }
    this.listeners.get(type).push(listener);
  }

  removeEventListener(type, listener) {
    const current = this.listeners.get(type) || [];
    this.listeners.set(
      type,
      current.filter((entry) => entry !== listener),
    );
  }

  dispatchEvent(event) {
    const current = this.listeners.get(event.type) || [];
    for (const listener of current) {
      listener(event);
    }
    return true;
  }

  listenerCount(type) {
    return (this.listeners.get(type) || []).length;
  }
}

class FakeMediaQueryList extends FakeListenerTarget {
  constructor(query, matches) {
    super();
    this.media = String(query);
    this.matches = Boolean(matches);
  }

  addListener(listener) {
    this.addEventListener("change", listener);
  }

  removeListener(listener) {
    this.removeEventListener("change", listener);
  }

  dispatch(matches) {
    if (typeof matches === "boolean") {
      this.matches = matches;
    }
    this.dispatchEvent({
      type: "change",
      media: this.media,
      matches: this.matches,
    });
  }
}

class FakeResponse {
  constructor(options) {
    this.ok = options.ok !== false;
    this.status = options.status || 200;
    this._text = options.text || "";
    this._bytes = options.bytes || [];
    this.url = options.url || "";
    const headerEntries = Object.entries(options.headers || {}).map(([key, value]) => [String(key).toLowerCase(), String(value)]);
    const headerMap = new Map(headerEntries);
    this._headers = Object.fromEntries(headerEntries);
    this.headers = {
      get(name) {
        return headerMap.get(String(name || "").toLowerCase()) || null;
      },
    };
  }

  clone() {
    return new FakeResponse({
      ok: this.ok,
      status: this.status,
      text: this._text,
      bytes: this._bytes.slice(),
      url: this.url,
      headers: this._headers,
    });
  }

  async text() {
    return this._text;
  }

  async json() {
    return JSON.parse(this._text || "null");
  }

  async arrayBuffer() {
    return Uint8Array.from(this._bytes).buffer;
  }
}

function buildMinimalGLBBytes() {
  const positions = new Float32Array([
    0, 0.75, 0,
    -0.65, -0.45, 0.3,
    0.7, -0.35, -0.2,
  ]);
  const normals = new Float32Array([
    0, 0, 1,
    0, 0, 1,
    0, 0, 1,
  ]);
  const indices = new Uint16Array([0, 1, 2]);
  const bin = Buffer.alloc(80);
  Buffer.from(positions.buffer).copy(bin, 0);
  Buffer.from(normals.buffer).copy(bin, 36);
  Buffer.from(indices.buffer).copy(bin, 72);

  const gltf = {
    asset: { version: "2.0", generator: "runtime-test" },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{
      name: "runner",
      primitives: [{
        attributes: { POSITION: 0, NORMAL: 1 },
        indices: 2,
        material: 0,
      }],
    }],
    materials: [{
      pbrMetallicRoughness: {
        baseColorFactor: [0.49, 0.78, 1, 1],
        metallicFactor: 0.08,
        roughnessFactor: 0.72,
      },
    }],
    accessors: [
      {
        bufferView: 0,
        componentType: 5126,
        count: 3,
        type: "VEC3",
        min: [-0.65, -0.45, -0.2],
        max: [0.7, 0.75, 0.3],
      },
      {
        bufferView: 1,
        componentType: 5126,
        count: 3,
        type: "VEC3",
      },
      {
        bufferView: 2,
        componentType: 5123,
        count: 3,
        type: "SCALAR",
      },
    ],
    bufferViews: [
      { buffer: 0, byteOffset: 0, byteLength: 36, target: 34962 },
      { buffer: 0, byteOffset: 36, byteLength: 36, target: 34962 },
      { buffer: 0, byteOffset: 72, byteLength: 8, target: 34963 },
    ],
    buffers: [{ byteLength: 80 }],
  };

  let json = Buffer.from(JSON.stringify(gltf), "utf8");
  while (json.length % 4 !== 0) {
    json = Buffer.concat([json, Buffer.from(" ")]);
  }

  const totalLength = 12 + 8 + json.length + 8 + bin.length;
  const glb = Buffer.alloc(totalLength);
  let offset = 0;
  glb.writeUInt32LE(0x46546c67, offset); offset += 4;
  glb.writeUInt32LE(2, offset); offset += 4;
  glb.writeUInt32LE(totalLength, offset); offset += 4;
  glb.writeUInt32LE(json.length, offset); offset += 4;
  glb.writeUInt32LE(0x4E4F534A, offset); offset += 4;
  json.copy(glb, offset); offset += json.length;
  glb.writeUInt32LE(bin.length, offset); offset += 4;
  glb.writeUInt32LE(0x004E4942, offset); offset += 4;
  bin.copy(glb, offset);
  return Array.from(glb);
}

function buildPointLineGLBBytes() {
  const chunks = [];
  const bufferViews = [];
  let byteOffset = 0;

  function alignBuffer() {
    const pad = (4 - (byteOffset % 4)) % 4;
    if (pad > 0) {
      chunks.push(Buffer.alloc(pad));
      byteOffset += pad;
    }
  }

  function appendTypedArray(typed, target) {
    alignBuffer();
    const bytes = Buffer.from(typed.buffer, typed.byteOffset, typed.byteLength);
    const view = { buffer: 0, byteOffset, byteLength: bytes.length };
    if (target) {
      view.target = target;
    }
    const viewIndex = bufferViews.length;
    bufferViews.push(view);
    chunks.push(bytes);
    byteOffset += bytes.length;
    return viewIndex;
  }

  const pointPositions = new Float32Array([
    0, 0, 0,
    1, 0, 0,
    0, 1, 0,
  ]);
  const pointColors = new Uint8Array([
    255, 0, 0, 255,
    0, 255, 0, 192,
    0, 0, 255, 128,
  ]);
  const pointSizes = new Float32Array([2, 3, 4]);
  const linePositions = new Float32Array([
    -1, -1, 0,
    0, 1, 0,
    1, -1, 0,
  ]);
  const lineIndices = new Uint16Array([0, 1, 1, 2]);

  const pointPositionView = appendTypedArray(pointPositions, 34962);
  const pointColorView = appendTypedArray(pointColors, 34962);
  const pointSizeView = appendTypedArray(pointSizes, 34962);
  const linePositionView = appendTypedArray(linePositions, 34962);
  const lineIndexView = appendTypedArray(lineIndices, 34963);
  alignBuffer();

  const bin = Buffer.concat(chunks);
  const gltf = {
    asset: { version: "2.0", generator: "runtime-test-points-lines" },
    scene: 0,
    scenes: [{ nodes: [0, 1] }],
    nodes: [
      { name: "point-node", mesh: 0, translation: [1, 0.5, 0] },
      { name: "line-node", mesh: 1 },
    ],
    meshes: [
      {
        name: "spark-field",
        primitives: [{
          mode: 0,
          attributes: { POSITION: 0, COLOR_0: 1, _POINT_SIZE: 2 },
          material: 0,
          extras: {
            gosx: {
              id: "sparks",
              style: "glow",
              opacity: 0.75,
              blendMode: "additive",
              live: ["palette"],
            },
          },
        }],
      },
      {
        name: "filament",
        primitives: [{
          mode: 1,
          attributes: { POSITION: 3 },
          indices: 4,
          material: 0,
          extras: { gosx: { id: "filament-lines" } },
        }],
      },
    ],
    materials: [{
      pbrMetallicRoughness: {
        baseColorFactor: [0.5, 0.75, 1, 0.8],
        metallicFactor: 0,
        roughnessFactor: 0.6,
      },
      alphaMode: "BLEND",
    }],
    accessors: [
      { bufferView: pointPositionView, componentType: 5126, count: 3, type: "VEC3" },
      { bufferView: pointColorView, componentType: 5121, count: 3, type: "VEC4", normalized: true },
      { bufferView: pointSizeView, componentType: 5126, count: 3, type: "SCALAR" },
      { bufferView: linePositionView, componentType: 5126, count: 3, type: "VEC3" },
      { bufferView: lineIndexView, componentType: 5123, count: 4, type: "SCALAR" },
    ],
    bufferViews,
    buffers: [{ byteLength: bin.length }],
  };

  let json = Buffer.from(JSON.stringify(gltf), "utf8");
  while (json.length % 4 !== 0) {
    json = Buffer.concat([json, Buffer.from(" ")]);
  }

  const totalLength = 12 + 8 + json.length + 8 + bin.length;
  const glb = Buffer.alloc(totalLength);
  let offset = 0;
  glb.writeUInt32LE(0x46546c67, offset); offset += 4;
  glb.writeUInt32LE(2, offset); offset += 4;
  glb.writeUInt32LE(totalLength, offset); offset += 4;
  glb.writeUInt32LE(json.length, offset); offset += 4;
  glb.writeUInt32LE(0x4E4F534A, offset); offset += 4;
  json.copy(glb, offset); offset += json.length;
  glb.writeUInt32LE(bin.length, offset); offset += 4;
  glb.writeUInt32LE(0x004E4942, offset); offset += 4;
  bin.copy(glb, offset);

  return Array.from(glb);
}

function buildSkinnedGLBBytes() {
  const chunks = [];
  const bufferViews = [];
  let byteOffset = 0;

  function alignBuffer() {
    const pad = (4 - (byteOffset % 4)) % 4;
    if (pad > 0) {
      chunks.push(Buffer.alloc(pad));
      byteOffset += pad;
    }
  }

  function appendTypedArray(typed, target) {
    alignBuffer();
    const viewIndex = bufferViews.length;
    const bytes = Buffer.from(typed.buffer, typed.byteOffset, typed.byteLength);
    const view = { buffer: 0, byteOffset, byteLength: bytes.length };
    if (target) {
      view.target = target;
    }
    bufferViews.push(view);
    chunks.push(bytes);
    byteOffset += bytes.length;
    return viewIndex;
  }

  const positions = new Float32Array([
    0, 0, 0,
    1, 0, 0,
    0, 1, 0,
  ]);
  const normals = new Float32Array([
    0, 0, 1,
    0, 0, 1,
    0, 0, 1,
  ]);
  const joints = new Uint8Array([
    0, 1, 0, 0,
    0, 1, 0, 0,
    0, 1, 0, 0,
  ]);
  const weights = new Float32Array([
    0.75, 0.25, 0, 0,
    0.5, 0.5, 0, 0,
    1, 0, 0, 0,
  ]);
  const indices = new Uint16Array([2, 0, 1]);
  const inverseBindMatrices = new Float32Array(32);
  inverseBindMatrices[0] = 1;
  inverseBindMatrices[5] = 1;
  inverseBindMatrices[10] = 1;
  inverseBindMatrices[15] = 1;
  inverseBindMatrices[16] = 1;
  inverseBindMatrices[21] = 1;
  inverseBindMatrices[26] = 1;
  inverseBindMatrices[31] = 1;
  const times = new Float32Array([0, 1]);
  const translations = new Float32Array([
    0, 0, 0,
    0, 0.5, 0,
  ]);

  const positionView = appendTypedArray(positions, 34962);
  const normalView = appendTypedArray(normals, 34962);
  const jointView = appendTypedArray(joints, 34962);
  const weightView = appendTypedArray(weights, 34962);
  const indexView = appendTypedArray(indices, 34963);
  const inverseBindView = appendTypedArray(inverseBindMatrices);
  const timeView = appendTypedArray(times);
  const translationView = appendTypedArray(translations);
  alignBuffer();

  const bin = Buffer.concat(chunks);
  const gltf = {
    asset: { version: "2.0", generator: "runtime-test-skinned" },
    scene: 0,
    scenes: [{ nodes: [0, 1] }],
    nodes: [
      { name: "skinned-mesh", mesh: 0, skin: 0, translation: [2, 0, 0] },
      { name: "root-joint", children: [2] },
      { name: "tip-joint" },
    ],
    meshes: [{
      name: "rig",
      primitives: [{
        attributes: {
          POSITION: 0,
          NORMAL: 1,
          JOINTS_0: 2,
          WEIGHTS_0: 3,
        },
        indices: 4,
        material: 0,
      }],
    }],
    skins: [{
      name: "rig-skin",
      joints: [1, 2],
      inverseBindMatrices: 5,
      skeleton: 1,
    }],
    animations: [{
      name: "bend",
      samplers: [{
        input: 6,
        output: 7,
        interpolation: "LINEAR",
      }],
      channels: [{
        sampler: 0,
        target: { node: 2, path: "translation" },
      }],
    }],
    materials: [{
      pbrMetallicRoughness: {
        baseColorFactor: [0.8, 0.4, 0.2, 1],
        metallicFactor: 0,
        roughnessFactor: 0.9,
      },
    }],
    accessors: [
      { bufferView: positionView, componentType: 5126, count: 3, type: "VEC3" },
      { bufferView: normalView, componentType: 5126, count: 3, type: "VEC3" },
      { bufferView: jointView, componentType: 5121, count: 3, type: "VEC4" },
      { bufferView: weightView, componentType: 5126, count: 3, type: "VEC4" },
      { bufferView: indexView, componentType: 5123, count: 3, type: "SCALAR" },
      { bufferView: inverseBindView, componentType: 5126, count: 2, type: "MAT4" },
      { bufferView: timeView, componentType: 5126, count: 2, type: "SCALAR" },
      { bufferView: translationView, componentType: 5126, count: 2, type: "VEC3" },
    ],
    bufferViews,
    buffers: [{ byteLength: bin.length }],
  };

  let json = Buffer.from(JSON.stringify(gltf), "utf8");
  while (json.length % 4 !== 0) {
    json = Buffer.concat([json, Buffer.from(" ")]);
  }

  const totalLength = 12 + 8 + json.length + 8 + bin.length;
  const glb = Buffer.alloc(totalLength);
  let offset = 0;
  glb.writeUInt32LE(0x46546c67, offset); offset += 4;
  glb.writeUInt32LE(2, offset); offset += 4;
  glb.writeUInt32LE(totalLength, offset); offset += 4;
  glb.writeUInt32LE(json.length, offset); offset += 4;
  glb.writeUInt32LE(0x4E4F534A, offset); offset += 4;
  json.copy(glb, offset); offset += json.length;
  glb.writeUInt32LE(bin.length, offset); offset += 4;
  glb.writeUInt32LE(0x004E4942, offset); offset += 4;
  bin.copy(glb, offset);

  return Array.from(glb);
}

class FakeFormData {
  constructor(form) {
    this.values = [];
    if (form) {
      this._collect(form);
    }
  }

  append(name, value) {
    this.values.push([String(name), String(value == null ? "" : value)]);
  }

  has(name) {
    return this.values.some((entry) => entry[0] === name);
  }

  get(name) {
    const found = this.values.find((entry) => entry[0] === name);
    return found ? found[1] : null;
  }

  forEach(callback, thisArg) {
    for (const [name, value] of this.values) {
      callback.call(thisArg, value, name, this);
    }
  }

  _collect(node) {
    if (!node || node.nodeType !== ELEMENT_NODE) {
      return;
    }
    const tag = node.tagName;
    if ((tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") && node.hasAttribute("name")) {
      this.append(node.getAttribute("name"), node.value || node.getAttribute("value") || "");
    }
    for (const child of node.children) {
      this._collect(child);
    }
  }
}

function createConsoleSpy() {
  const logs = { error: [], warn: [], log: [] };
  return {
    logs,
    console: {
      error: (...args) => logs.error.push(args.map(String).join(" ")),
      warn: (...args) => logs.warn.push(args.map(String).join(" ")),
      log: (...args) => logs.log.push(args.map(String).join(" ")),
    },
  };
}

function numberOr(value, fallback) {
  const num = Number(value);
  return Number.isFinite(num) ? num : fallback;
}

function createComputedStyleSnapshot(element, options) {
  const fromOption = typeof options.getComputedStyle === "function" ? options.getComputedStyle(element) : null;
  const source = Object.assign({}, element && element.computedStyle ? element.computedStyle : {}, fromOption || {});
  if (typeof source.getPropertyValue !== "function") {
    source.getPropertyValue = function getPropertyValue(name) {
      if (Object.prototype.hasOwnProperty.call(source, name)) {
        return source[name];
      }
      const camel = String(name || "").replace(/-([a-z])/g, (_match, letter) => String(letter || "").toUpperCase());
      return Object.prototype.hasOwnProperty.call(source, camel) ? source[camel] : "";
    };
  }
  return source;
}

function createContext(options) {
  const document = new FakeDocument();
  if (options.visibilityState) {
    document.visibilityState = String(options.visibilityState);
  }
  document.disableCanvas2D = Boolean(options.disableCanvas2D);
  if (typeof options.measureText === "function") {
    document.measureText = options.measureText;
  }
  if (options.fonts) {
    document.fonts = options.fonts;
  }
  if (typeof options.createWebGLContext === "function") {
    document.createWebGLContext = options.createWebGLContext;
  } else if (options.enableWebGL) {
    document.createWebGLContext = () => new FakeWebGLContext();
  }
  if (typeof options.createWebGL2Context === "function") {
    document.createWebGL2Context = options.createWebGL2Context;
  } else if (options.enableWebGL2) {
    document.createWebGL2Context = () => new FakeWebGLContext();
  }
  if (typeof options.createWebGPUContext === "function") {
    document.createWebGPUContext = options.createWebGPUContext;
  } else if (options.enableWebGPU) {
    document.createWebGPUContext = () => new FakeWebGPUCanvasContext();
  }
  const consoleSpy = createConsoleSpy();
  const hydrateCalls = [];
  const computeHydrateCalls = [];
  const actionCalls = [];
  const disposeCalls = [];
  const engineHydrateCalls = [];
  const engineRenderCalls = [];
  const engineTickCalls = [];
  const engineDisposeCalls = [];
  const engineMounts = [];
  const engineDisposals = [];
  const sharedSignalCalls = [];
  const inputBatchCalls = [];
  const sockets = [];
  const fetchCalls = [];
  const imageLoads = [];
  const scrollCalls = [];
  const windowListeners = new Map();
  const resizeObservers = [];
  const intersectionObservers = [];
  const mutationObservers = [];
  const mediaQueries = new Map();
  let cryptoWord = 1;
  const visualViewport = options.visualViewport === false ? null : new FakeListenerTarget();
  if (visualViewport) {
    visualViewport.offsetLeft = numberOr(options.visualViewportOffsetLeft, 0);
    visualViewport.offsetTop = numberOr(options.visualViewportOffsetTop, 0);
    visualViewport.width = numberOr(options.visualViewportWidth, 0);
    visualViewport.height = numberOr(options.visualViewportHeight, 0);
  }

  const routes = new Map();
  // The text-layout engine now ships as a lazily fetched chunk instead of
  // riding in every bundle (see bootstrap-src/00-textlayout.js). Serve it by
  // default: any page with a data-gosx-text-layout element, and any page whose
  // manifest mounts a Scene3D engine, asks for it. A test can still override
  // the route through options.fetchRoutes.
  routes.set("/gosx/bootstrap-feature-textlayout.js", { text: bootstrapFeatureTextLayoutSource });
  // The WebGL2 renderer ships as a lazily fetched chunk too, so a WebGPU-capable
  // browser never downloads it (see bootstrap-src/16-scene-webgl.js). Serve it
  // by default: every Scene3D page that draws with WebGL asks for it, and so
  // does a WebGPU page whose device is lost. A test can still override the
  // route through options.fetchRoutes, or drop it to prove the chunk is absent.
  routes.set("/gosx/bootstrap-feature-scene3d-webgl.js", { text: bootstrapFeatureScene3DWebGLSource });
  for (const [url, response] of Object.entries(options.fetchRoutes || {})) {
    routes.set(url, response);
  }

  const context = {
    Array,
    ArrayBuffer,
    DataView,
    Intl,
    JSON,
    Map,
    Promise,
    Set,
    Uint8Array,
    Uint8ClampedArray,
    Uint32Array,
    AbortController: class AbortController {
      constructor() {
        this.signal = { aborted: false };
      }

      abort() {
        this.signal.aborted = true;
      }
    },
    clearTimeout,
    clearInterval,
    console: consoleSpy.console,
    crypto: {
      getRandomValues(words) {
        for (let i = 0; i < words.length; i += 1) {
          words[i] = cryptoWord++;
        }
        return words;
      },
      subtle: {
        async digest(_algorithm, _bytes) {
          return Uint8Array.from([
            0xcd, 0x5d, 0x49, 0x35, 0xa4, 0x8c, 0x06, 0x72,
            0xcb, 0x06, 0x40, 0x7b, 0xb4, 0x43, 0xbc, 0x00,
            0x87, 0xaf, 0xf9, 0x47, 0xc6, 0xb8, 0x64, 0xba,
            0xc8, 0x86, 0x98, 0x2c, 0x73, 0xb3, 0x02, 0x7f,
          ]).buffer;
        },
      },
    },
    CustomEvent: class CustomEvent {
      constructor(type, init = {}) {
        this.type = type;
        this.detail = init.detail;
      }
    },
    document,
    FormData: FakeFormData,
    Image: class FakeImage {
      constructor() {
        this.onload = null;
        this.onerror = null;
        this.complete = false;
        this.naturalWidth = 1;
        this.naturalHeight = 1;
        this._src = "";
      }

      set src(value) {
        this._src = String(value == null ? "" : value);
        this.complete = true;
        imageLoads.push(this._src);
        setTimeout(() => {
          if (typeof this.onload === "function") {
            this.onload({ type: "load", target: this });
          }
        }, 0);
      }

      get src() {
        return this._src;
      }
    },
    fetch: async (url, init = {}) => {
      fetchCalls.push({ url, init });
      if (!routes.has(url)) {
        throw new Error("unexpected fetch: " + url);
      }
      const route = routes.get(url);
      const response = typeof route === "function" ? await route(url, init, fetchCalls.length) : route;
      return new FakeResponse(Object.assign({ url }, response || {}));
    },
    getComputedStyle(element) {
      return createComputedStyleSnapshot(element, options);
    },
    location: {
      protocol: "http:",
      host: "localhost:3000",
      href: "http://localhost:3000/",
      origin: "http://localhost:3000",
    },
    navigator: {
      deviceMemory: numberOr(options.deviceMemory, 8),
      hardwareConcurrency: Math.max(1, Math.floor(numberOr(options.hardwareConcurrency, 8))),
      maxTouchPoints: Math.max(0, Math.floor(numberOr(options.maxTouchPoints, 0))),
      userAgent: String(options.userAgent || "FakeBrowser/1.0"),
      getGamepads: typeof options.getGamepads === "function" ? options.getGamepads : () => [],
    },
    performance: {
      now: typeof options.performanceNow === "function" ? options.performanceNow : () => Date.now(),
      mark() {},
      measure() {},
      clearMarks() {},
    },
    matchMedia(query) {
      const key = String(query);
      if (!mediaQueries.has(key)) {
        const matches = key === "(prefers-reduced-motion: reduce)"
          ? Boolean(options.prefersReducedMotion)
          : Boolean(options.matchMedia && options.matchMedia[key]);
        mediaQueries.set(key, new FakeMediaQueryList(key, matches));
      }
      return mediaQueries.get(key);
    },
    devicePixelRatio: numberOr(options.devicePixelRatio, 1),
    history: {
      pushState(_state, _title, url) {
        context.location.href = String(url);
      },
      replaceState(_state, _title, url) {
        context.location.href = String(url);
      },
    },
    requestAnimationFrame(callback) {
      return setTimeout(() => callback(Date.now()), 0);
    },
    cancelAnimationFrame(handle) {
      clearTimeout(handle);
    },
    ResizeObserver: class ResizeObserver extends FakeResizeObserver {
      constructor(callback) {
        super(callback);
        resizeObservers.push(this);
      }
    },
    MutationObserver: class MutationObserver extends FakeMutationObserver {
      constructor(callback) {
        super(callback);
        mutationObservers.push(this);
      }
    },
    IntersectionObserver: class IntersectionObserver extends FakeIntersectionObserver {
      constructor(callback, observerOptions) {
        super(callback, observerOptions);
        intersectionObservers.push(this);
      }
    },
    Go: function Go() {
      this.importObject = {};
      this.run = () => {
        context.__gosx_hydrate = (...args) => {
          hydrateCalls.push(args);
          if (typeof options.onHydrate === "function") {
            return options.onHydrate(...args);
          }
          return null;
        };
        context.__gosx_hydrate_compute = (...args) => {
          computeHydrateCalls.push(args);
          if (typeof options.onHydrateCompute === "function") {
            return options.onHydrateCompute(...args);
          }
          return null;
        };
        context.__gosx_action = (...args) => {
          actionCalls.push(args);
          if (typeof options.onAction === "function") {
            return options.onAction(...args);
          }
          return 0;
        };
        context.__gosx_dispose = (...args) => {
          disposeCalls.push(args);
          if (typeof options.onDispose === "function") {
            return options.onDispose(...args);
          }
          return null;
        };
        context.__gosx_hydrate_engine = (...args) => {
          engineHydrateCalls.push(args);
          if (typeof options.onHydrateEngine === "function") {
            return options.onHydrateEngine(...args);
          }
          return "[]";
        };
        context.__gosx_tick_engine = (...args) => {
          engineTickCalls.push(args);
          if (typeof options.onTickEngine === "function") {
            return options.onTickEngine(...args);
          }
          return "[]";
        };
        context.__gosx_render_engine = (...args) => {
          engineRenderCalls.push(args);
          if (typeof options.onRenderEngine === "function") {
            return options.onRenderEngine(...args);
          }
          return "";
        };
        context.__gosx_engine_dispose = (...args) => {
          engineDisposeCalls.push(args);
          if (typeof options.onDisposeEngine === "function") {
            return options.onDisposeEngine(...args);
          }
          return null;
        };
        context.__gosx_set_shared_signal = (...args) => {
          sharedSignalCalls.push(args);
          if (typeof options.onSetSharedSignal === "function") {
            return options.onSetSharedSignal(...args);
          }
          return null;
        };
        context.__gosx_set_input_batch = (...args) => {
          inputBatchCalls.push(args);
          if (typeof options.onSetInputBatch === "function") {
            return options.onSetInputBatch(...args);
          }
          return null;
        };
        if (typeof context.__gosx_runtime_ready === "function") {
          context.__gosx_runtime_ready();
        }
      };
    },
    Node: {
      ELEMENT_NODE,
      TEXT_NODE,
    },
    URL,
    URLSearchParams,
    addEventListener(type, listener) {
      if (!windowListeners.has(type)) {
        windowListeners.set(type, []);
      }
      windowListeners.get(type).push(listener);
    },
    removeEventListener(type, listener) {
      const current = windowListeners.get(type) || [];
      windowListeners.set(
        type,
        current.filter((entry) => entry !== listener),
      );
    },
    dispatchEvent(event) {
      const listeners = windowListeners.get(event.type) || [];
      for (const listener of listeners) {
        listener(event);
      }
      return true;
    },
    scrollTo(...args) {
      scrollCalls.push(args);
    },
    queueMicrotask,
    setInterval,
    setTimeout,
    WebAssembly: {
      instantiate: async () => ({ instance: {} }),
      instantiateStreaming: async () => ({ instance: {} }),
    },
  };
  if (options.enableWebGPU) {
    const webgpuDevice = options.webgpuDevice || {
      lost: new Promise(() => {}),
    };
    const webgpuAdapter = options.webgpuAdapter || {
      requestDevice: async () => webgpuDevice,
    };
    context.navigator.gpu = options.navigatorGPU || {
      requestAdapter: async () => webgpuAdapter,
      getPreferredCanvasFormat: () => "rgba8unorm",
    };
  }
  if (visualViewport) {
    context.visualViewport = visualViewport;
  }
  if (typeof options.AudioContext === "function") {
    context.AudioContext = options.AudioContext;
  }
  if (typeof options.webkitAudioContext === "function") {
    context.webkitAudioContext = options.webkitAudioContext;
  }

  if (typeof options.parseHTML === "function") {
    context.DOMParser = class DOMParser {
      parseFromString(html) {
        return options.parseHTML(html);
      }
    };
  }

  if (typeof options.createWebSocket === "function") {
    context.WebSocket = function WebSocket(url) {
      const socket = options.createWebSocket(url);
      sockets.push(socket);
      return socket;
    };
  }

  context.window = context;
	context.__gosx_runtime_abi = {
		handshake() {
			const contract = context.__gosx_runtime_contract;
			return contract ? {
				abiVersion: contract.abiVersion,
				mailboxVersion: contract.mailboxVersion,
				manifestHash: contract.manifestHash,
				variant: "core",
				featureMask: contract.variants.core,
			} : null;
		},
	};
  context.__gosx_engine_factories = Object.assign({}, options.engineFactories || {});
  context.__engineMounts = engineMounts;
  context.__engineDisposals = engineDisposals;
  vm.createContext(context);

  document.scriptLoader = function(src, scriptElement) {
    fetchCalls.push({ url: src, init: {} });
    if (!routes.has(src)) {
      setTimeout(() => {
        if (typeof scriptElement.onerror === "function") {
          scriptElement.onerror(new Error("script not found: " + src));
        }
      }, 0);
      return;
    }
    const route = routes.get(src);
    const source = route.text || "";
    setTimeout(() => {
      try {
        vm.runInContext(source, context, { filename: src });
      } catch (e) {
        if (typeof scriptElement.onerror === "function") {
          scriptElement.onerror(e);
          return;
        }
      }
      if (typeof scriptElement.onload === "function") {
        scriptElement.onload({});
      }
    }, 0);
  };

  if (options.manifest) {
	const runtime = options.manifest.runtime;
	if (runtime && runtime.path && !runtime.hash) {
		runtime.hash = "cd5d4935a48c0672";
		runtime.manifestHash = "850ff5e72dc872437bf568a7486f0ed08ad0fa046dfaa5f8956243b70182bc10";
		runtime.variant = "core";
		runtime.featureMask = 17;
	}
    const manifestScript = document.createElement("script");
    manifestScript.id = "gosx-manifest";
    manifestScript.textContent = JSON.stringify(options.manifest);
    document.body.appendChild(manifestScript);
  }

  for (const element of options.elements || []) {
    document.body.appendChild(element);
  }

  const env = {
    actionCalls,
    computeHydrateCalls,
    consoleLogs: consoleSpy.logs,
    context,
    disposeCalls,
    document,
    engineDisposeCalls,
    engineDisposals,
    engineHydrateCalls,
    engineRenderCalls,
    engineMounts,
    engineTickCalls,
    fetchCalls,
    hydrateCalls,
    imageLoads,
    inputBatchCalls,
    intersectionObservers,
    matchMedia(query) {
      return context.matchMedia(query);
    },
    mediaQueries,
    mutationObservers,
    resizeObservers,
    sharedSignalCalls,
    scrollCalls,
    sockets,
    visualViewport,
    windowListeners,
  };
  activeTestContexts.add(env);
  return env;
}

function installManualRAF(context) {
  let nextHandle = 1;
  const callbacks = new Map();
  context.requestAnimationFrame = (callback) => {
    const handle = nextHandle++;
    callbacks.set(handle, callback);
    return handle;
  };
  context.cancelAnimationFrame = (handle) => {
    callbacks.delete(handle);
  };
  return {
    count() {
      return callbacks.size;
    },
    flush(time) {
      const entries = Array.from(callbacks.entries());
      callbacks.clear();
      for (const [, callback] of entries) {
        callback(typeof time === "number" ? time : 16);
      }
    },
  };
}

async function flushSceneInitialFrameBoundary(raf, firstTime = 16, secondTime = 32) {
  raf.flush(firstTime);
  await flushAsyncWork();
  raf.flush(secondTime);
  await flushAsyncWork();
}

function installManualTimers(context) {
  let nextHandle = 1;
  const timers = new Map();
  const intervals = new Map();
  context.setTimeout = (callback, delay, ...args) => {
    const handle = nextHandle++;
    timers.set(handle, {
      callback,
      delay: Number(delay || 0),
      args,
    });
    return handle;
  };
  context.clearTimeout = (handle) => {
    timers.delete(handle);
  };
  context.setInterval = (callback, delay, ...args) => {
    const handle = nextHandle++;
    intervals.set(handle, {
      callback,
      delay: Number(delay || 0),
      args,
    });
    return handle;
  };
  context.clearInterval = (handle) => {
    intervals.delete(handle);
  };
  return {
    count() {
      return timers.size + intervals.size;
    },
    runDelay(delay) {
      const targetDelay = Number(delay || 0);
      const entries = Array.from(timers.entries())
        .filter(([, timer]) => timer.delay === targetDelay);
      for (const [handle, timer] of entries) {
        if (!timers.has(handle)) {
          continue;
        }
        timers.delete(handle);
        timer.callback(...timer.args);
      }
      return entries.length;
    },
    runInterval(delay) {
      const targetDelay = Number(delay || 0);
      const entries = Array.from(intervals.entries())
        .filter(([, timer]) => timer.delay === targetDelay);
      for (const [handle, timer] of entries) {
        if (!intervals.has(handle)) {
          continue;
        }
        timer.callback(...timer.args);
      }
      return entries.length;
    },
  };
}

// installManualClock replaces context.Date with a minimal double exposing
// only Date.now() — the one clock entry point server/navigation_runtime.js
// reads (see PAGE_CACHE_TTL_MS). A test advances it explicitly instead of
// waiting on the real clock.
function installManualClock(context, startAt) {
  let current = typeof startAt === "number" ? startAt : Date.now();
  context.Date = {
    now() {
      return current;
    },
  };
  return {
    now() {
      return current;
    },
    advance(ms) {
      current += Number(ms) || 0;
      return current;
    },
  };
}

function runScript(source, context, filename) {
  vm.runInContext(source, context, { filename });
}

async function flushAsyncWork() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));
}

function sharedSignalValue(env, name) {
  const store = env && env.context && env.context.__gosx && env.context.__gosx.sharedSignals;
  if (!store || !store.values || typeof store.values.get !== "function") {
    return undefined;
  }
  return store.values.get(name);
}

function appendManagedHead(document, nodes) {
  const start = document.createElement("meta");
  start.setAttribute("name", "gosx-head-start");
  start.setAttribute("content", "");
  const end = document.createElement("meta");
  end.setAttribute("name", "gosx-head-end");
  end.setAttribute("content", "");
  document.head.appendChild(start);
  for (const node of nodes) {
    document.head.appendChild(node);
  }
  document.head.appendChild(end);
}

function theatreSyncHeartbeat({ serverTime = 0, position = 0, playing = false, viewerCount = 0 } = {}) {
  const buffer = new ArrayBuffer(16);
  const bytes = new Uint8Array(buffer);
  const view = new DataView(buffer);
  const timestamp = Math.max(0, Math.floor(Number(serverTime) || 0));
  bytes[0] = 0x01;
  view.setUint32(1, Math.floor(timestamp / 4294967296), false);
  view.setUint32(5, timestamp % 4294967296, false);
  view.setFloat32(9, Number(position) || 0, false);
  bytes[13] = playing ? 1 : 0;
  view.setUint16(14, Math.max(0, Math.min(65535, Math.floor(Number(viewerCount) || 0))), false);
  return buffer;
}

function theatrePing(timestamp = 0) {
  const buffer = new ArrayBuffer(9);
  const bytes = new Uint8Array(buffer);
  const view = new DataView(buffer);
  const value = Math.max(0, Math.floor(Number(timestamp) || 0));
  bytes[0] = 0x05;
  view.setUint32(1, Math.floor(value / 4294967296), false);
  view.setUint32(5, value % 4294967296, false);
  return buffer;
}

function buildNavigatedDocument(options) {
  const doc = new FakeDocument();
  doc.title = options.title || "";
  appendManagedHead(doc, options.headNodes || []);
  for (const node of options.bodyNodes || []) {
    doc.body.appendChild(node);
  }
  return doc;
}

// Mounts a shared-runtime Scene3D whose program creates a long box ("cube") and
// drives one frame. The scene IR carries a motionProgram so the P2.4b WASM
// motion seam (applyWasmMotionFrame) can lazy-load + tick + decode. Returns the
// FakeWebGLContext so the caller can read uploaded world-mesh positions.
async function mountMotionSeamScene(motionFlag, tickFn) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-motion-seam-root";
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-motion.json": { text: '{"name":"MotionSeam"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-motion",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-motion-seam-root",
          runtime: "shared",
          programRef: "/scene-motion.json",
          // The scene IR rides under props.scene and carries motionProgram as
          // base64; the load stub ignores the bytes, so any non-empty string works.
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            scene: { motionProgram: "AAEC" },
          },
        },
      ],
    },
    // Camera + a single long box keyed "cube" (width 3, depth 0.4) so a 90°
    // rotation about Y swaps the X/Z extents — a deterministic, camera-
    // independent world-space signal.
    onHydrateEngine: () => JSON.stringify([
      { kind: 5, objectId: 0, data: { x: 0, y: 0, z: 8, fov: 75 } },
      {
        kind: 0,
        objectId: "cube",
        data: {
          kind: "box",
          geometry: "box",
          material: "flat",
          props: { x: 0, y: 0, z: 0, width: 3, height: 0.4, depth: 0.4, color: "#8de1ff" },
        },
      },
    ]),
    // Empty render bundle forces renderFrame to fall through to the tick branch
    // (where the motion seam runs).
    onRenderEngine: () => "",
  });

  if (motionFlag) {
    env.context.__gosx_motion_wasm = true;
    env.context.__gosx_motion_load = () => 1;
    env.context.__gosx_motion_refs = () => ({ target: ["cube"], prop: ["rotation"] });
    env.context.__gosx_motion_tick = tickFn;
    env.context.__gosx_motion_unload = () => {};
  }

  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();
  raf.flush(32);
  await flushAsyncWork();

  const gl = mount.children[0] && typeof mount.children[0].getContext === "function"
    ? mount.children[0].getContext("webgl")
    : null;
  return { env, mount, gl };
}

// Locates the box position buffer among all uploads and returns its world-space
// |x| / |z| extents. The box (width 3, height/depth 0.4) is the only mesh whose
// vertices are exactly the 8 corners at {±1.5, ±0.2, ±0.2}: every triple's
// components fall in {0.2, 1.5} (up to rotation, which only permutes the axes),
// and the peak extent is 1.5. That signature uniquely identifies the position
// buffer and excludes normals (unit), colors (0..1), and the lit-mesh buffer
// (which carries view-space / non-axis-aligned values).
function motionMeshExtents(gl) {
  let best = null;
  if (!gl || !gl.bufferUploads) return { maxAbsX: 0, maxAbsZ: 0 };
  for (const data of gl.bufferUploads.values()) {
    if (!Array.isArray(data) || data.length < 9 || data.length % 3 !== 0) continue;
    let maxAbsX = 0, maxAbsZ = 0, isBox = true;
    for (let i = 0; i + 2 < data.length && isBox; i += 3) {
      const comps = [Math.abs(data[i]), Math.abs(data[i + 1]), Math.abs(data[i + 2])];
      for (const c of comps) {
        // Box corner magnitudes are 0.2 or 1.5 (within fp tolerance).
        if (Math.abs(c - 0.2) > 0.02 && Math.abs(c - 1.5) > 0.02) { isBox = false; break; }
      }
      if (comps[0] > maxAbsX) maxAbsX = comps[0];
      if (comps[2] > maxAbsZ) maxAbsZ = comps[2];
    }
    if (isBox && Math.max(maxAbsX, maxAbsZ) > 1.4) {
      best = { maxAbsX, maxAbsZ };
      break;
    }
  }
  return best || { maxAbsX: 0, maxAbsZ: 0 };
}

// C3: mounts a JS-sceneState scene whose IR carries
// props.scene.materialMotionProgram (base64). A stubbed motion handle reports a
// single TargetMaterial track (mesh "glow-cube", uniform "emissive"); the tick
// stub writes one packed Color record. The seam must decode it and write the
// color into the mesh's customUniforms so selena's per-frame re-pack sees it.
// motionFlag toggles whether window.__gosx_motion_wasm is set, proving the
// seam is fully inert (tick never called, customUniforms untouched) when off.
async function mountMaterialMotionScene(motionFlag) {
  const mount = new FakeElement("div", null);
  mount.id = "scene-material-motion-root";
  let tickCalls = 0;
  // The decoded color the stub emits (r,g,b,a). arity 5 (Color) → width 4.
  const color = [0.2, 0.6, 0.9, 1.0];
  const tick = (handle, t, reduced, outU8) => {
    tickCalls += 1;
    const f = new Float64Array(outU8.buffer, outU8.byteOffset, outU8.byteLength / 8);
    // packed: [targetID 0, propID 0, arity 5 (Color), r, g, b, a]
    f[0] = 0; f[1] = 0; f[2] = 5;
    f[3] = color[0]; f[4] = color[1]; f[5] = color[2]; f[6] = color[3];
    return 7;
  };
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-mat-motion.json": { text: '{"name":"MatMotion"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-mat-motion",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-material-motion-root",
          runtime: "shared",
          programRef: "/scene-mat-motion.json",
          // materialMotionProgram rides under props.scene as base64; the load
          // stub ignores the bytes, so any non-empty string works.
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            scene: { materialMotionProgram: "AAEC" },
          },
        },
      ],
    },
    // A selena/custom box keyed "glow-cube" carrying an inline customUniforms
    // bag — so the resolved write target is the object record itself (no named
    // material lookup), which the seam mutates in place.
    onHydrateEngine: () => JSON.stringify([
      { kind: 5, objectId: 0, data: { x: 0, y: 0, z: 8, fov: 75 } },
      {
        kind: 0,
        objectId: "glow-cube",
        data: {
          kind: "box",
          geometry: "box",
          props: {
            x: 0, y: 0, z: 0, size: 1, color: "#8de1ff",
            materialKind: "custom",
            shaderBackend: "selena",
            customFragmentWGSL: "fn gosx_fragment() -> vec4f { return vec4f(1.0); }",
            customUniforms: { emissive: [0, 0, 0, 0] },
          },
        },
      },
    ]),
    onRenderEngine: () => "",
  });

  if (motionFlag) {
    env.context.__gosx_motion_wasm = true;
    env.context.__gosx_motion_load = () => 1;
    env.context.__gosx_motion_refs = () => ({ target: ["glow-cube"], prop: ["emissive"] });
    env.context.__gosx_motion_tick = tick;
    env.context.__gosx_motion_unload = () => {};
  } else {
    // Exports present but the opt-in flag deliberately unset.
    env.context.__gosx_motion_load = () => 1;
    env.context.__gosx_motion_refs = () => ({ target: ["glow-cube"], prop: ["emissive"] });
    env.context.__gosx_motion_tick = tick;
    env.context.__gosx_motion_unload = () => {};
  }

  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  raf.flush(16);
  await flushAsyncWork();
  raf.flush(32);
  await flushAsyncWork();

  const state = mount.__gosxScene3DState;
  return { env, mount, state, color, tickCalls: () => tickCalls };
}

// Fixture: byte-identical to the GLSL m31labs.dev/selena emits for a default
// (no vertex()) mesh material with a `worldNormal`-reading surface() — the
// same shape as the game's FighterLit material (fight/materials/fighter.sel).
// mvp is model-free (view*proj only) and normalMatrix is hardcoded identity
// by the WebGL renderer's selenaUniformValue; both conventions assume
// `position`/`normal` arrive already in world space.
const SELENA_SKINNABLE_VERTEX_GLSL_FIXTURE = [
  "attribute vec3 position;",
  "attribute vec3 normal;",
  "uniform mat4 mvp;",
  "uniform mat3 normalMatrix;",
  "uniform vec3 baseColor;",
  "uniform vec3 rimColor;",
  "uniform float rimGain;",
  "uniform float gain;",
  "uniform float light_ambient;",
  "uniform vec3 light_dir;",
  "varying vec3 vWorldNormal;",
  "",
  "void main() {",
  "  vWorldNormal = normalize((normalMatrix * normal));",
  "  gl_Position = (mvp * vec4(position, 1.0));",
  "}",
].join("\n");

const SELENA_SKINNABLE_FRAGMENT_GLSL_FIXTURE = [
  "precision mediump float;",
  "uniform mat4 mvp;",
  "uniform mat3 normalMatrix;",
  "uniform vec3 baseColor;",
  "uniform vec3 rimColor;",
  "uniform float rimGain;",
  "uniform float gain;",
  "uniform float light_ambient;",
  "uniform vec3 light_dir;",
  "varying vec3 vWorldNormal;",
  "",
  "void main() {",
  "  vec3 n = normalize(vWorldNormal);",
  "  float ndl = max(dot(n, light_dir), 0.0);",
  "  float lit = (light_ambient + (ndl * gain));",
  "  float rim = pow((1.0 - ndl), 3.0);",
  "  gl_FragColor = vec4(((baseColor * lit) + (rimColor * (rim * rimGain))), 1.0);",
  "}",
].join("\n");

const SELENA_SKINNABLE_SHADER_LAYOUT_FIXTURE = {
  schemaVersion: "selena.descriptor.v1",
  languageVersion: "selena.lang.v1",
  material: "FighterLit",
  kind: "mesh",
  entryPoints: { vertex: "vertexMain", fragment: "fragmentMain" },
  attributes: [
    { location: 0, name: "position", type: "vec3" },
    { location: 1, name: "normal", type: "vec3" },
  ],
  textures: [],
  uniformBlock: {
    size: 176,
    fields: [
      { name: "mvp", type: "mat4", offset: 0, size: 64 },
      { name: "normalMatrix", type: "mat3", offset: 64, size: 48 },
      { name: "baseColor", type: "vec3", offset: 112, size: 12 },
      { name: "rimColor", type: "vec3", offset: 128, size: 12 },
      { name: "rimGain", type: "float", offset: 140, size: 4 },
      { name: "gain", type: "float", offset: 144, size: 4 },
      { name: "light_ambient", type: "float", offset: 148, size: 4 },
      { name: "light_dir", type: "vec3", offset: 160, size: 12 },
    ],
    defaults: [
      { name: "baseColor", type: "vec3", values: [0.62, 0.66, 0.74] },
      { name: "rimColor", type: "vec3", values: [0.4, 0.85, 1] },
      { name: "rimGain", type: "float", values: [1.1] },
      { name: "gain", type: "float", values: [1] },
    ],
  },
  wgsl: { group: 0, binding: 0 },
  metal: { buffer: 0 },
};

function loadSceneWaterClockAPI() {
  // sceneNumber sits in the runtime-utils file and the water clock sits in the
  // scene core file. The bundles load them next to each other, so join them.
  const core = readBootstrapSrc("10-runtime-scene-utils.js", "10-runtime-scene-core.js");
  const start = core.indexOf("function sceneNumber(value, fallback)");
  const end = core.indexOf("function sceneNumberOrCSSVar", start);
  assert.notEqual(start, -1, "sceneNumber anchor missing from scene core");
  assert.notEqual(end, -1, "sceneNumberOrCSSVar anchor missing from scene core");
  const context = {};
  vm.runInNewContext(
    '"use strict";\n' + core.slice(start, end) +
      "\nglobalThis.clockAPI = { sceneWaterAdvanceClock, sceneWaterResetClock };",
    context,
    { filename: "scene-water-clock.js" },
  );
  return context.clockAPI;
}

// --- WebGL chunk split (bootstrap-feature-scene3d-webgl.js) ------------------
//
// 16-scene-webgl.js left the base scene3d chunk. These tests pin the three
// behaviours the split must not break:
//   1. A WebGL page fetches the chunk and draws with WebGL.
//   2. A WebGPU page does NOT fetch the chunk at mount.
//   3. A page that cannot reach the chunk keeps walking the ladder to canvas2d
//      instead of stalling on a dead renderer.

function scene3dWebGLSplitManifest(mountID, props) {
  return {
    runtime: { path: "/gosx/runtime.wasm" },
    engines: [
      {
        id: "gosx-engine-" + mountID,
        component: "GoSXScene3D",
        kind: "surface",
        mountId: mountID,
        jsExport: "GoSXScene3D",
        props: Object.assign({
          width: 320,
          height: 180,
          scene: { objects: [{ kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" }] },
        }, props || {}),
      },
    ],
  };
}

// --- WebGL customPost reserved auto-uniforms ---
//
// v0.35.9 repaired customPost DISPATCH: post effect kinds were lowercased, so
// "customPost" matched no backend case and the pass never ran. Once the pass
// started to run, a second defect became reachable. applyCustomPost read ONLY
// effect.uniforms, so RESERVED auto-uniforms — time above all — were never
// resolved. Any time-driven WebGL post effect therefore read time == 0 on
// every frame and stayed inert.
//
// The WebGPU post path (ensureCustomPostUniformBuffer -> sceneSelenaUniformData
// -> sceneSelenaUniformValue) and the WebGL mesh path (uploadSelenaUniforms ->
// selenaUniformValue) both resolve reserved uniforms. Only WebGL customPost did
// not. The bug class survived because every earlier test asserted that the pass
// DISPATCHED and none asserted what the pass RECEIVED. This test asserts
// uniform CONTENT.
const CUSTOM_POST_TIME_LAYOUT_FIXTURE = {
  schemaVersion: "selena.descriptor.v1",
  languageVersion: "selena.lang.v1",
  material: "TimedLens",
  kind: "post",
  entryPoints: { vertex: "vertexMain", fragment: "fragmentMain" },
  attributes: [{ location: 0, name: "a_position", type: "vec2" }],
  textures: [],
  uniformBlock: {
    size: 32,
    fields: [
      { name: "time", type: "float", offset: 0, size: 4 },
      { name: "amount", type: "float", offset: 4, size: 4 },
      { name: "tint", type: "vec3", offset: 16, size: 12 },
    ],
    defaults: [
      // A compiled `param time` ships its default of 0 inside customUniforms.
      // It must NOT shadow the engine clock — same precedence the mesh path
      // and the WebGPU post path apply (reserved names resolve first).
      { name: "time", type: "float", values: [0] },
      { name: "tint", type: "vec3", values: [0.25, 0.5, 0.75] },
    ],
  },
  wgsl: { group: 0, binding: 0 },
};

// sceneCoreSourceRange extracts a [startAnchor, endAnchor) slice from
// 10-runtime-scene-core.js — used to pull the REAL G2 QualityLadder
// normalizer (sceneQualityLadder et al.) and its two small dependencies
// into the adaptive-quality VM harness below, instead of stubbing them out,
// so ladder-driven harness tests exercise the exact same normalization the
// full bootstrap bundle runs.
function sceneCoreSourceRange(startAnchor, endAnchor) {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.js"), "utf8");
  const start = source.indexOf(startAnchor);
  const end = source.indexOf(endAnchor, start);
  assert.notEqual(start, -1, "10-runtime-scene-core.js anchor missing: " + startAnchor);
  assert.notEqual(end, -1, "10-runtime-scene-core.js anchor missing: " + endAnchor);
  return source.slice(start, end);
}

function loadSceneAdaptiveQualityAPI() {
  const source = readSceneMountSrc();
  const start = source.indexOf("function sceneStatusBindingLabel");
  const end = source.indexOf("function applyScenePostFXState", start);
  assert.notEqual(start, -1, "adaptive controller start anchor missing");
  assert.notEqual(end, -1, "adaptive controller end anchor missing");
  const sceneProps = sceneCoreSourceRange("function sceneProps(props)", "function sceneObjectList");
  const sceneIsPlainObject = sceneCoreSourceRange("function sceneIsPlainObject(value)", "function normalizeSceneMaterialCapabilityTier");
  const qualityLadderCore = sceneCoreSourceRange(
    "// G2 QualityLadder — bidirectional work-based ABR",
    "function sceneCamera(props)",
  );
  const clock = { now: 0 };
  const context = { __clock: clock };
  vm.runInNewContext(`
    function sceneNumber(value, fallback) { const n = Number(value); return Number.isFinite(n) ? n : fallback; }
    function sceneBool(value, fallback) { return value == null ? fallback : (value === false || value === "false" ? false : Boolean(value)); }
    function setAttrValue(mount, name, value) { const next = String(value == null ? "" : value); if (mount.getAttribute(name) !== next) mount.setAttribute(name, next); }
    function applyScenePostFXState() {}
    function gosxSceneEmit() {}
    const performance = { now: () => __clock.now };
  ` + sceneProps + sceneIsPlainObject + qualityLadderCore + source.slice(start, end) + `
    globalThis.adaptiveAPI = {
      createSceneAdaptiveQualityState, applySceneAdaptiveQualityState, sceneUpdateAdaptiveQuality,
      sceneApplyQualityLadderRung, sceneQualityLadderAdmittedGroups, sceneFilterObjectsByQualityGroups,
      sceneEffectivePointQualityGroup, sceneFilterPointsByQualityGroups,
      sceneQualityLadderPointBudgetScale, sceneApplyPointBudgetScale,
      scenePrimeAdaptiveQuality,
      sceneSyncStatusBindings,
    };
  `, context, { filename: "scene-adaptive-quality.js" });
  return { api: context.adaptiveAPI, clock };
}

function loadSceneViewportAPI(options = {}) {
  const mountSource = readSceneMountSrc();
  const start = mountSource.indexOf("function sceneViewportDevicePixelRatio");
  const end = mountSource.indexOf("function observeSceneViewport", start);
  assert.notEqual(start, -1, "viewport start anchor missing");
  assert.notEqual(end, -1, "viewport end anchor missing");
  const baseStart = mountSource.indexOf("function sceneViewportBase");
  const baseEnd = mountSource.indexOf("function scheduleSceneIdleTask", baseStart);
  assert.notEqual(baseStart, -1, "viewport base start anchor missing");
  assert.notEqual(baseEnd, -1, "viewport base end anchor missing");
  const devicePixelRatio = Number.isFinite(Number(options.devicePixelRatio))
    ? Number(options.devicePixelRatio)
    : 1;
  const environment = Object.assign({ devicePixelRatio }, options.environment || {});
  const context = {
    window: { devicePixelRatio },
    __environment: environment,
  };
  vm.runInNewContext(`
    function sceneNumber(value, fallback) { const n = Number(value); return Number.isFinite(n) ? n : fallback; }
    function sceneBool(value, fallback) { return value == null ? fallback : (value === false || value === "false" ? false : Boolean(value)); }
    function sceneEnvironmentState() { return __environment; }
    function defaultSceneMaxDevicePixelRatio(capability) {
      if (capability && (capability.reducedData || capability.lowPower)) {
        switch (capability.tier) {
          case "constrained": return 1.25;
          case "balanced": return 1.5;
          default: return 1.75;
        }
      }
      switch (capability && capability.tier) {
        case "constrained": return 1.5;
        case "balanced": return 1.75;
        default: return 2;
      }
    }
  ` + mountSource.slice(baseStart, baseEnd) + mountSource.slice(start, end) + `
    globalThis.viewportAPI = {
      sceneViewportBase,
      sceneViewportDevicePixelRatio,
      sceneViewportFromMount,
    };
  `, context, { filename: "scene-viewport.js" });
  return context.viewportAPI;
}

function resolveSceneViewportForTest(props, options = {}) {
  const api = loadSceneViewportAPI(options);
  const width = Number.isFinite(Number(options.measuredWidth))
    ? Number(options.measuredWidth)
    : sceneNumberForTest(props && props.width, 390);
  const height = Number.isFinite(Number(options.measuredHeight))
    ? Number(options.measuredHeight)
    : sceneNumberForTest(props && props.height, 844);
  const base = api.sceneViewportBase(Object.assign({ responsive: false }, props || {}));
  if (options.base) Object.assign(base, options.base);
  const rect = { width, height, left: 0, top: 0 };
  const mount = { getBoundingClientRect() { return rect; } };
  const canvas = { getBoundingClientRect() { return rect; } };
  return api.sceneViewportFromMount(
    mount,
    Object.assign({ responsive: false }, props || {}),
    base,
    canvas,
    options.capability || { tier: "full" },
    options.adaptiveQuality || null,
  );
}

function sceneNumberForTest(value, fallback) {
  const n = Number(value);
  return Number.isFinite(n) ? n : fallback;
}

function createAdaptiveQualityHarness(extraProps) {
  const loaded = loadSceneAdaptiveQualityAPI();
  const props = Object.assign({
    adaptiveQuality: true,
    qualityTier: "balanced",
    adaptiveTargetFrameMS: 16,
    adaptiveWarmupFrames: 0,
    adaptiveCooldownMS: 5000,
    adaptivePostFX: true,
  }, extraProps || {});
  const state = loaded.api.createSceneAdaptiveQualityState(props, { explicitMaxDevicePixelRatio: 1.6 }, { tier: "full" });
  const mount = new FakeElement("div", null);
  const bloom = { kind: "bloom" };
  const sceneState = { _adaptiveSourcePostEffects: [bloom], postEffects: [bloom] };
  const renderer = {
    sample: null,
    pollPerformanceSample() { const sample = this.sample; this.sample = null; return sample; },
  };
  let frameNowMS = 0;
  function sample(durationMS, advanceMS) {
    const advance = advanceMS == null ? 16 : advanceMS;
    loaded.clock.now += advance;
    frameNowMS += advance;
    renderer.sample = { source: "gpu-test", gpuMS: durationMS, atMS: loaded.clock.now };
    return loaded.api.sceneUpdateAdaptiveQuality(state, mount, sceneState, {}, loaded.clock.now - 1, frameNowMS, renderer);
  }
  loaded.api.applySceneAdaptiveQualityState(mount, state, 0, true);
  sample(1); // resume/initial anchor is deliberately excluded
  return { ...loaded, state, mount, sceneState, renderer, sample };
}

// --- G2 QualityLadder governor (bidirectional work-based ABR) ---
//
// Same fake-clock/renderer-sample harness pattern as createAdaptiveQualityHarness
// above, but authors a scene.qualityLadder prop (the Go-lowered wire shape —
// see scene/quality_ladder.go) so createSceneAdaptiveQualityState takes the
// mode: "ladder" branch. loadSceneAdaptiveQualityAPI splices in the REAL
// sceneQualityLadder normalizer from 10-runtime-scene-core.js (not a stub),
// so this exercises the exact same rung normalization the full bootstrap
// bundle runs.
function createQualityLadderHarness(qualityLadder, extraProps) {
  const loaded = loadSceneAdaptiveQualityAPI();
  const props = Object.assign({
    scene: { qualityLadder },
    adaptiveTargetFrameMS: 16,
    adaptiveWarmupFrames: 0,
    adaptiveCooldownMS: 5000,
  }, extraProps || {});
  const state = loaded.api.createSceneAdaptiveQualityState(props, { explicitMaxDevicePixelRatio: 1.6 }, { tier: "full" });
  const mount = new FakeElement("div", null);
  const bloom = { kind: "bloom" };
  const tonemap = { kind: "toneMapping" };
  const customLens = { kind: "customPost", name: "test-lens", vertexGLSL: "attribute vec2 a;", fragmentGLSL: "void main(){}" };
  const sourceEffects = [bloom, tonemap, customLens];
  const sceneState = { _adaptiveSourcePostEffects: sourceEffects, postEffects: sourceEffects.slice() };
  const renderer = {
    sample: null,
    pollPerformanceSample() { const sample = this.sample; this.sample = null; return sample; },
  };
  let frameNowMS = 0;
  function sample(durationMS, advanceMS) {
    const advance = advanceMS == null ? 16 : advanceMS;
    loaded.clock.now += advance;
    frameNowMS += advance;
    renderer.sample = { source: "gpu-test", gpuMS: durationMS, atMS: loaded.clock.now };
    return loaded.api.sceneUpdateAdaptiveQuality(state, mount, sceneState, {}, loaded.clock.now - 1, frameNowMS, renderer);
  }
  loaded.api.scenePrimeAdaptiveQuality(state, {}, mount, sceneState);
  sample(1); // resume/initial anchor is deliberately excluded, mirrors createAdaptiveQualityHarness
  return { ...loaded, state, mount, sceneState, renderer, sample, bloom, tonemap, customLens, sourceEffects };
}

const THREE_RUNG_LADDER = [
  { name: "raw" },
  { name: "glow", postEffects: ["bloom"], layerGroups: ["particles"], pointBudgetScale: 0.5 },
  { name: "full", postEffects: ["bloom", "toneMapping", "test-lens"], layerGroups: ["particles", "far-decor"] },
];

// createQualityLadderRAFHarness is createQualityLadderHarness's cpu-raf
// counterpart: renderer.pollPerformanceSample() always returns null (no
// GPU timestamp-query support — the common case on regular Chrome stable)
// and there is no getPerformanceTimingStatus, so sceneUpdateQualityLadder
// falls through to the cpu-raf sample built from the rAF interval, exactly
// like a real un-timed browser session. sample(intervalMS) drives both the
// wall clock and the rAF timestamp by the same amount, so a constant
// intervalMS simulates a steady vsync-locked frame cadence.
function createQualityLadderRAFHarness(qualityLadder, extraProps) {
  const loaded = loadSceneAdaptiveQualityAPI();
  const props = Object.assign({
    scene: { qualityLadder },
    adaptiveTargetFrameMS: 16,
    adaptiveWarmupFrames: 0,
    adaptiveCooldownMS: 5000,
  }, extraProps || {});
  const state = loaded.api.createSceneAdaptiveQualityState(props, { explicitMaxDevicePixelRatio: 1.6 }, { tier: "full" });
  const mount = new FakeElement("div", null);
  const sceneState = { _adaptiveSourcePostEffects: [], postEffects: [] };
  const renderer = {
    pollPerformanceSample() { return null; }, // no GPU timing available
  };
  let frameNowMS = 0;
  function sample(intervalMS) {
    const advance = intervalMS == null ? 16.7 : intervalMS;
    loaded.clock.now += advance;
    frameNowMS += advance;
    return loaded.api.sceneUpdateAdaptiveQuality(state, mount, sceneState, {}, loaded.clock.now - 1, frameNowMS, renderer);
  }
  loaded.api.scenePrimeAdaptiveQuality(state, {}, mount, sceneState);
  sample(1); // resume/initial anchor is deliberately excluded, mirrors createQualityLadderHarness
  return { ...loaded, state, mount, sceneState, renderer, sample };
}

// --- v0.33.1: empty/absent LayerGroups at the QualityStartRung must admit
// everything from frame one, exactly like it does when the same empty-
// LayerGroups rung is reached later via promotion/demotion ---
//
// Bug: sceneQualityLadderAdmittedGroups returned the active rung's
// normalized `layerGroups` array verbatim. normalizeSceneQualityRung always
// materializes that field as an array — `[]` for a rung with none authored,
// never undefined — and `[]` is truthy in JS. The filter functions'
// `!admittedGroups` back-compat check therefore saw "filtering is active"
// instead of "no filtering", and since `[].indexOf(anything)` is always -1,
// every TAGGED (non-empty qualityGroup) entry was rejected while untagged
// entries still passed — so a scene whose QualityStartRung pointed straight
// at an empty-LayerGroups rung lost every tagged mesh/points entry from the
// very first frame, before any promotion/demotion had a chance to run.
const RAW_TO_GLOW_LADDER = [
  { name: "raw" }, // no LayerGroups authored -> must admit everything
  { name: "glow", layerGroups: ["particles"] },
];

function telemetryPostBodies(env) {
  return env.fetchCalls
    .filter((call) => call.url === "/_gosx/client-events" && call.init && call.init.method === "POST")
    .map((call) => JSON.parse(call.init.body));
}

function telemetryEvents(env) {
  const bodies = telemetryPostBodies(env);
  const events = [];
  for (const body of bodies) {
    if (body && Array.isArray(body.events)) {
      for (const event of body.events) {
        events.push(event);
      }
    }
  }
  return events;
}

const VIDEO_PRIMITIVES_FAKE_HLS_SCRIPT = `window.__hlsInstances = [];
window.Hls = function FakeHls(config) {
  const self = this;
  self.config = config;
  self.handlers = {};
  self.audioTracks = [];
  self.levels = [];
  self.audioTrackSets = [];
  self.nextLevelSets = [];
  self._audioTrack = -1;
  self._currentLevel = -1;
  self._nextLevel = -1;
  self.attachMedia = function(video) { self.video = video; };
  self.loadSource = function(src) { self.src = src; };
  self.on = function(event, handler) { self.handlers[event] = handler; };
  self.destroy = function() {};
  Object.defineProperty(self, "audioTrack", {
    get: function() { return self._audioTrack; },
    set: function(value) { self._audioTrack = value; self.audioTrackSets.push(value); },
  });
  Object.defineProperty(self, "currentLevel", {
    get: function() { return self._currentLevel; },
    set: function(value) { self._currentLevel = value; },
  });
  Object.defineProperty(self, "nextLevel", {
    get: function() { return self._nextLevel; },
    set: function(value) { self._nextLevel = value; self.nextLevelSets.push(value); },
  });
  window.__hlsInstances.push(self);
};
window.Hls.isSupported = function() { return true; };
window.Hls.Events = {
  MANIFEST_PARSED: "hlsManifestParsed",
  SUBTITLE_TRACKS_UPDATED: "hlsSubtitleTracksUpdated",
  ERROR: "hlsError",
  AUDIO_TRACKS_UPDATED: "hlsAudioTracksUpdated",
  AUDIO_TRACK_SWITCHED: "hlsAudioTrackSwitched",
  LEVELS_UPDATED: "hlsLevelsUpdated",
  LEVEL_SWITCHED: "hlsLevelSwitched",
  LEVEL_LOADED: "hlsLevelLoaded",
};`;

// ---------------------------------------------------------------------------
// Video drift engine: JS fallback ↔ Go golden parity.
//
// 28-video-sync-fallback.js is a pure-JS port of the Go videosync engine
// (client/videosync). The committed golden vector — produced by the Go
// Engine — is replayed through a fresh JS engine; the decision stream MUST
// match. kind/preloadPhase/ready/stalled/resetRate are exact; rate/seekTo/
// actualRate within 1e-3. If this diverges, the JS port is wrong (the Go side
// is the source of truth) — do NOT edit the golden to fit the JS.
// ---------------------------------------------------------------------------
function loadVideoSyncJSEngineFactory() {
  // Extract the factory from the UNMINIFIED 28- source (mirrors how the other
  // source-extraction tests read bootstrap-src/*.js directly), eval it in an
  // isolated context whose only global is `window`, and pull the factory off.
  const source = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "28-video-sync-fallback.js"),
    "utf8",
  );
  const sandbox = { window: {} };
  vm.createContext(sandbox);
  vm.runInContext(source, sandbox, { filename: "28-video-sync-fallback.js" });
  const factory = sandbox.window.__gosx_video_sync_js_create;
  assert.equal(
    typeof factory,
    "function",
    "28-video-sync-fallback.js must install window.__gosx_video_sync_js_create",
  );
  return factory;
}

// -----------------------------------------------------------------------------
// Canvas2D painter — paintCanvasBundle(ctx, bundle, cssWidth, cssHeight, dpr)
//
// The painter is the JS half of the canvas2d paint loop: it takes a
// RenderBundle (as emitted by __gosx_render_canvas), computes the screen
// transform from the OrthoCamera2D contract (mode "ortho2d"; camera.x = panX,
// camera.y = panY, camera.z = zoom), and replays the bundle's objects/lines/
// labels onto a 2D context. The transform it must replicate is:
//
//     screenX = (worldX - panX) * zoom + cssWidth / 2
//     screenY = (cssHeight / 2) - (worldY - panY) * zoom   (NDC +Y up → canvas +Y down)
//
// These tests feed a hand-built bundle to a FAKE 2D context that records every
// draw call and assert the resulting screen coordinates.
// -----------------------------------------------------------------------------

// loadCanvasPainter evaluates the standalone painter fragment in an isolated
// sandbox and returns the exposed window.__gosx_paint_canvas_bundle function.
function loadCanvasPainter() {
  const source = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "26b1-canvas2d-painter.js"),
    "utf8",
  );
  const sandbox = {};
  sandbox.window = sandbox;
  vm.createContext(sandbox);
  vm.runInContext(source, sandbox, { filename: "26b1-canvas2d-painter.js" });
  const fn = sandbox.window.__gosx_paint_canvas_bundle;
  assert.equal(typeof fn, "function", "painter must expose window.__gosx_paint_canvas_bundle");
  return fn;
}

// makeFakeContext2D returns a CanvasRenderingContext2D-like recorder. It tracks
// fillStyle/strokeStyle/lineWidth/font assignments and records each draw call
// (with the style in effect at call time) into a flat `calls` array.
function makeFakeContext2D() {
  const ctx = {
    fillStyle: "",
    strokeStyle: "",
    lineWidth: 1,
    font: "",
    textBaseline: "",
    textAlign: "",
    calls: [],
  };
  ctx.clearRect = (x, y, w, h) => ctx.calls.push({ op: "clearRect", x, y, w, h });
  ctx.fillRect = (x, y, w, h) =>
    ctx.calls.push({ op: "fillRect", x, y, w, h, fillStyle: ctx.fillStyle });
  ctx.beginPath = () => ctx.calls.push({ op: "beginPath" });
  ctx.moveTo = (x, y) => ctx.calls.push({ op: "moveTo", x, y });
  ctx.lineTo = (x, y) => ctx.calls.push({ op: "lineTo", x, y });
  ctx.stroke = () =>
    ctx.calls.push({ op: "stroke", strokeStyle: ctx.strokeStyle, lineWidth: ctx.lineWidth });
  ctx.fillText = (text, x, y) =>
    ctx.calls.push({ op: "fillText", text, x, y, fillStyle: ctx.fillStyle, font: ctx.font });
  ctx.save = () => ctx.calls.push({ op: "save" });
  ctx.restore = () => ctx.calls.push({ op: "restore" });
  ctx.setTransform = (a, b, c, d, e, f) =>
    ctx.calls.push({ op: "setTransform", a, b, c, d, e, f });
  ctx.scale = (x, y) => ctx.calls.push({ op: "scale", x, y });
  return ctx;
}

function callsOfType(ctx, op) {
  return ctx.calls.filter((c) => c.op === op);
}

// nodeFillRects returns fillRect calls that are NOT the full-viewport background
// fill (which the painter emits at the origin in CSS-logical pixels; the caller
// pre-scales the context for the device pixel ratio, so dpr does not change the
// logical fill region).
function nodeFillRects(ctx, cssWidth, cssHeight) {
  return callsOfType(ctx, "fillRect").filter(
    (c) => !(c.x === 0 && c.y === 0 && c.w === cssWidth && c.h === cssHeight),
  );
}

// -----------------------------------------------------------------------------
// Ortho-2D WebGPU camera math — sceneMat4Ortho2DView/Proj/ViewProj
// (bootstrap-src/11-scene-math.js, exported through window.__gosx_scene3d_api).
//
// sceneMat4Ortho2DViewProj is the JS half of the pinned cross-language golden
// contract with the native 2D board camera: render/bundle/math.go
// computeOrthoCamera2DMVP, guarded by TestComputeOrthoCamera2DMVP_Golden
// (render/bundle/ortho_camera_2d_golden_test.go). The values asserted below are
// copied verbatim from that Go test — if either side drifts, both suites fail.
//
// The helpers read the RAW engine.RenderCamera wire fields (mode, x/y = pan,
// z = zoom, near/far) and are deliberately NOT routed through sceneRenderCamera,
// whose 3D defaults (z→6, near→0.05, far→128) would silently mangle them.
// -----------------------------------------------------------------------------

// loadScene3DApiContext boots the full monolithic bundle in a fake DOM and
// returns window.__gosx_scene3d_api — the same object the chunked
// bootstrap-feature-scene3d-webgpu.js prefix destructures at load time.
async function loadScene3DApiContext() {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const api = env.context.__gosx_scene3d_api;
  assert.ok(api, "bootstrap must publish window.__gosx_scene3d_api");
  return api;
}

function assertMat4Approx(out, expectations, label) {
  for (const [index, want] of expectations) {
    const got = out[index];
    assert.ok(
      Math.abs(got - want) <= 1e-5,
      `${label} m[${index}] = ${got}, want ${want}`,
    );
  }
}

// -----------------------------------------------------------------------------
// Ortho-2D board bundle → 16a WebGPU renderer (adaptOrtho2DBoardBundle seam)
//
// The Go board pipeline (render/bundle2d.ComputeCanvasGPUBundle) marshals an
// engine.RenderBundle in the NATIVE renderer vocabulary: rect quads live in
// bundle.objects (vertexOffset/vertexCount/materialIndex) sliced into
// bundle.worldPositions/worldNormals/worldUVs. 16a draws from the JS
// scene-core vocabulary instead (meshObjects + worldMeshPositions/...). The
// adaptOrtho2DBoardBundle seam at the top of 16a's render() bridges the two by
// ZERO-COPY aliasing — these tests drive the REAL chunked renderer
// (bootstrap-feature-scene3d.js + bootstrap-feature-scene3d-webgpu.js) against
// a fake GPUDevice and assert observable draw behavior, not source shape.
//
// Fixtures are the SHARED Go↔JS goldens in render/bundle2d/testdata/ —
// bundle2d's TestBoardGPUBundleGolden asserts
// MarshalCanvasBundle(ComputeCanvasGPUBundleWithBackground(nodes, "#102030",
// 640, 480, 0.5, 0, 0)) equals these bytes byte-for-byte (the node lists live
// next to that test), so what renders here is EXACTLY what Go emits.
// Regenerate with GOSX_UPDATE_BOARD_FIXTURES=1 go test ./render/bundle2d/.
// Zoom 0.5 is deliberate: at zoom < 1 the board objects are NOT bounds-culled
// by sceneBoundsViewCulled, so the fixtures also pin the regression where
// scene-core's buildSceneWorldDrawPlan misread native board objects (which
// have worldPositions but no worldColors) as world-line records and threw.
// -----------------------------------------------------------------------------

function readBoardFixture(name) {
  return fs.readFileSync(
    path.join(__dirname, "..", "..", "render", "bundle2d", "testdata", name),
    "utf8",
  );
}

// Two-rect board ({ID:"card-a",Kind:"rect",X:16,Y:24,Width:200,Height:120,
// Color:"#3a86ff"}, {ID:"card-b",Kind:"rect",X:280,Y:60,Width:160,Height:90,
// Color:"#ffbe0b"}). NOTE: the first object's vertexOffset/materialIndex are
// ABSENT — Go's `omitempty` elides zero values on the wire; the seam restores
// both. Materials carry the Selena BoardFill attach (customVertexWGSL/
// customFragmentWGSL/shaderBackend/shaderLayout + customUniforms.baseColor).
const goBoardBundleRectsJSON = readBoardFixture("board_fixture_rects.json");

// Mixed board (1 rect + 2 lines + 1 label + 1 sprite). Since M1 slice 2A the
// Go attach (AttachBoardGPUGeometry) expands lines and sprites into z=0
// quads appended to objects/worldPositions in painter z-order (rect → lines
// → sprite), so the fixture carries FOUR drawable objects: the rect and the
// two lines on flat+Selena BoardFill materials, the sprite on a bare
// {kind:"sprite", color:"#ffffff", texture, unlit} material that draws
// unlit-textured through 16a's default PBR object path (no Selena attach). The
// lines/labels/sprites wire arrays still ride unchanged
// ({from:{x,y},to:{x,y},color,lineWidth} etc.); labels stay undrawn here
// (slice 2C renders them as a DOM overlay).
const goBoardBundleMixedJSON = readBoardFixture("board_fixture_mixed.json");
const goBoardBundleMixedWithHTMLJSON = JSON.stringify(Object.assign(
  {},
  JSON.parse(goBoardBundleMixedJSON),
  {
    html: [{
      id: "page:home",
      markup: '<h1 contenteditable data-studio-field="home.hero.headline">Hi</h1>',
      x: 20,
      y: 20,
      width: 240,
      height: 80,
      pointerEvents: "auto",
    }],
  },
));

// parseWGSLBindingKinds extracts every `@group(G) @binding(B) var ...`
// declaration from a WGSL source string into a Map keyed "G:B" -> kind, where
// kind is one of "uniform", "storage-read", "storage-read_write", "texture",
// or "sampler". This is a lightweight scan (mirroring the same small set of
// regexes the Go host-binding-descriptor test uses --
// examples/gosx-docs/.../water/selena_wgsl_binding_test.go -- not a full WGSL
// parser), used ONLY by makeFakeGPUDevice's opt-in `validateBindings` mode.
function parseWGSLBindingKinds(source) {
  const kinds = new Map();
  const src = typeof source === "string" ? source : "";
  const textureOrSamplerRE = /@group\(\s*(\d+)\s*\)\s*@binding\(\s*(\d+)\s*\)\s*var\s+\w+\s*:\s*(texture_\w+(?:<[^>]*>)?|sampler)\s*;/g;
  const storageRE = /@group\(\s*(\d+)\s*\)\s*@binding\(\s*(\d+)\s*\)\s*var<storage,\s*(read_write|read)>\s*\w+\s*:/g;
  const uniformRE = /@group\(\s*(\d+)\s*\)\s*@binding\(\s*(\d+)\s*\)\s*var<uniform>\s*\w+\s*:/g;
  let m;
  while ((m = textureOrSamplerRE.exec(src))) {
    kinds.set(m[1] + ":" + m[2], m[3] === "sampler" ? "sampler" : "texture");
  }
  while ((m = storageRE.exec(src))) {
    kinds.set(m[1] + ":" + m[2], m[3] === "read_write" ? "storage-read_write" : "storage-read");
  }
  while ((m = uniformRE.exec(src))) {
    kinds.set(m[1] + ":" + m[2], "uniform");
  }
  return kinds;
}

// gpuBindGroupLayoutEntryKind classifies one GPUBindGroupLayoutEntry the same
// way parseWGSLBindingKinds classifies a WGSL declaration, so the two can be
// compared directly by makeFakeGPUDevice's validator.
function gpuBindGroupLayoutEntryKind(entry) {
  if (!entry) return null;
  if (entry.texture) return "texture";
  if (entry.sampler) return "sampler";
  if (entry.buffer) {
    if (entry.buffer.type === "uniform") return "uniform";
    if (entry.buffer.type === "storage") return "storage-read_write";
    if (entry.buffer.type === "read-only-storage") return "storage-read";
  }
  return null;
}

// makeFakeGPUDevice builds a recording GPUDevice double that satisfies every
// device call 16a's synchronous init + render() issue on the board path:
// buffers (incl. mappedAtCreation), textures/views, samplers, bind groups,
// pipelines, shader modules, command encoders with render/compute passes, the
// queue, and validation error scopes.
//
// Pass { validateBindings: true } to opt into a structural binding-mismatch
// gate (used by the pool-pass Selena routing test): createShaderModule
// already records the WGSL `code` on the returned module; when validation is
// enabled, createBindGroupLayout rejects malformed entries,
// createRenderPipeline cross-checks every bind group layout entry the
// pipeline references (group index + binding number + resource kind) against
// `@group(G) @binding(B)` declarations actually present in the pipeline's
// vertex/fragment WGSL, and createBindGroup cross-checks the bind group's
// actual entries against its OWN bind group layout's declared entries (catches
// a bind-group builder that drifts from the layout it was built against, the
// exact class of bug a copy-paste error between sceneSelenaBindGroupLayout and
// createSelenaBindGroup would produce). Every check throws loudly on mismatch.
// Defaulting to false/undefined keeps every existing makeFakeGPUDevice() call
// site (there are hundreds across this file) byte-for-byte unaffected.
function makeFakeGPUDevice(options) {
  const validateBindings = Boolean(options && options.validateBindings);
  const timestampQuery = Boolean(options && options.timestampQuery);
  const timestampEncoder = !options || options.timestampEncoder !== false;
  // writeTimestamp is a finer switch than timestampEncoder. Chromium removed
  // encoder.writeTimestamp but kept resolveQuerySet and copyBufferToBuffer, so a
  // test can model that exact implementation.
  const encoderWriteTimestamp = !options || options.writeTimestamp !== false;
  const renderBundles = Boolean(options && options.renderBundles);
  function validateBindGroupLayoutDesc(desc) {
    if (!validateBindings) return;
    const entries = (desc && desc.entries) || [];
    for (const entry of entries) {
      if (typeof entry.binding !== "number" || entry.binding < 0) {
        throw new Error("[gosx-test fake GPUDevice] createBindGroupLayout entry has an invalid binding: " + JSON.stringify(entry));
      }
      const kindCount = [entry.buffer, entry.texture, entry.sampler].filter(Boolean).length;
      if (kindCount !== 1) {
        throw new Error(
          "[gosx-test fake GPUDevice] createBindGroupLayout @binding(" + entry.binding + ") must declare exactly one of buffer/texture/sampler, got " + kindCount,
        );
      }
    }
  }
  function validateRenderPipelineDesc(desc) {
    if (!validateBindings) return;
    // Scope the strict "every layout entry must be declared in the WGSL"
    // check to the generic Selena material path ONLY (its pipelines are
    // always labeled "gosx-selena-..." by getSelenaPipeline, and its bind
    // group layout is built PER-MATERIAL from the same descriptor that also
    // drove the WGSL, so the two are always meant to match 1:1). Every other
    // pipeline in this renderer (PBR, shadow, points, the hand-written water
    // passes, ...) intentionally shares wide, reusable bind group layouts
    // (e.g. frameBindGroupLayout carries the shadow-map binding used by SOME
    // but not all consumers of group 0) -- checking those against one
    // specific shader's WGSL would be validating the wrong subset direction
    // and produces false positives unrelated to this task's pool-routing
    // change.
    if (typeof desc.label !== "string" || desc.label.indexOf("gosx-selena-") !== 0) return;
    const vertexCode = desc && desc.vertex && desc.vertex.module && desc.vertex.module.code;
    const fragmentCode = desc && desc.fragment && desc.fragment.module && desc.fragment.module.code;
    const combined = [vertexCode, fragmentCode].filter((s) => typeof s === "string" && s).join("\n");
    if (!combined) return; // no shader text recorded for this pipeline -- nothing to check.
    const declared = parseWGSLBindingKinds(combined);
    if (declared.size === 0) return; // shader carries no @group/@binding annotations -- skip.
    const pipelineLayoutDesc = desc && desc.layout && desc.layout.desc;
    const bindGroupLayouts = (pipelineLayoutDesc && pipelineLayoutDesc.bindGroupLayouts) || [];
    for (let g = 0; g < bindGroupLayouts.length; g += 1) {
      const bglEntries = (bindGroupLayouts[g] && bindGroupLayouts[g].desc && bindGroupLayouts[g].desc.entries) || [];
      for (const entry of bglEntries) {
        const key = g + ":" + entry.binding;
        const declaredKind = declared.get(key);
        if (!declaredKind) {
          throw new Error(
            "[gosx-test fake GPUDevice] createRenderPipeline '" + (desc.label || "") + "': bind group layout references @group(" + g + ") @binding(" + entry.binding +
            "), but no such binding is declared in the pipeline's WGSL",
          );
        }
        const entryKind = gpuBindGroupLayoutEntryKind(entry);
        if (entryKind && entryKind !== declaredKind) {
          throw new Error(
            "[gosx-test fake GPUDevice] createRenderPipeline '" + (desc.label || "") + "': @group(" + g + ") @binding(" + entry.binding + ") is declared as \"" +
            declaredKind + "\" in WGSL, but the bind group layout entry is \"" + entryKind + "\"",
          );
        }
      }
    }
  }
  // validateComputePipelineDesc mirrors validateRenderPipelineDesc for the
  // Selena feedback-compute path (getSelenaComputePipeline in
  // 16a-scene-webgpu.js): cross-checks every bind group layout entry the
  // compute pipeline references (group index + binding number + resource
  // kind) against `@group(G) @binding(B)` declarations actually present in
  // the pipeline's compute-stage WGSL. Scoped to "gosx-selena-" labeled
  // pipelines exactly like the render check (every OTHER compute pipeline in
  // this renderer -- elioSkin, computedMorph, the hardcoded water
  // seed/drop/displacement/step/normal kernels -- keeps its wide, reusable
  // bind group layout unchecked here).
  function validateComputePipelineDesc(desc) {
    if (!validateBindings) return;
    if (typeof desc.label !== "string" || desc.label.indexOf("gosx-selena-") !== 0) return;
    const code = desc && desc.compute && desc.compute.module && desc.compute.module.code;
    if (typeof code !== "string" || !code) return; // no shader text recorded -- nothing to check.
    const declared = parseWGSLBindingKinds(code);
    if (declared.size === 0) return; // shader carries no @group/@binding annotations -- skip.
    const pipelineLayoutDesc = desc && desc.layout && desc.layout.desc;
    const bindGroupLayouts = (pipelineLayoutDesc && pipelineLayoutDesc.bindGroupLayouts) || [];
    for (let g = 0; g < bindGroupLayouts.length; g += 1) {
      const bglEntries = (bindGroupLayouts[g] && bindGroupLayouts[g].desc && bindGroupLayouts[g].desc.entries) || [];
      for (const entry of bglEntries) {
        const key = g + ":" + entry.binding;
        const declaredKind = declared.get(key);
        if (!declaredKind) {
          throw new Error(
            "[gosx-test fake GPUDevice] createComputePipeline '" + (desc.label || "") + "': bind group layout references @group(" + g + ") @binding(" + entry.binding +
            "), but no such binding is declared in the pipeline's compute-stage WGSL",
          );
        }
        const entryKind = gpuBindGroupLayoutEntryKind(entry);
        if (entryKind && entryKind !== declaredKind) {
          throw new Error(
            "[gosx-test fake GPUDevice] createComputePipeline '" + (desc.label || "") + "': @group(" + g + ") @binding(" + entry.binding + ") is declared as \"" +
            declaredKind + "\" in WGSL, but the bind group layout entry is \"" + entryKind + "\"",
          );
        }
      }
    }
  }
  function validateBindGroupDesc(desc) {
    if (!validateBindings) return;
    const layoutDesc = desc && desc.layout && desc.layout.desc;
    if (!layoutDesc || !Array.isArray(layoutDesc.entries)) return; // nothing to cross-check against.
    const entries = Array.isArray(desc.entries) ? desc.entries : [];
    const byBinding = new Map(entries.map((e) => [e.binding, e]));
    for (const layoutEntry of layoutDesc.entries) {
      const actual = byBinding.get(layoutEntry.binding);
      if (!actual) {
        throw new Error(
          "[gosx-test fake GPUDevice] createBindGroup is missing an entry for @binding(" + layoutEntry.binding + "), which its bind group layout declares",
        );
      }
      const hasBufferResource = !!(actual.resource && typeof actual.resource === "object" && actual.resource.buffer);
      if (layoutEntry.buffer && !hasBufferResource) {
        throw new Error(
          "[gosx-test fake GPUDevice] createBindGroup @binding(" + layoutEntry.binding + ") must supply a {buffer} resource per its layout, got " + JSON.stringify(actual.resource),
        );
      }
      if ((layoutEntry.texture || layoutEntry.sampler) && hasBufferResource) {
        throw new Error(
          "[gosx-test fake GPUDevice] createBindGroup @binding(" + layoutEntry.binding + ") must supply a texture/sampler resource per its layout, got a buffer",
        );
      }
    }
    if (entries.length !== layoutDesc.entries.length) {
      throw new Error(
        "[gosx-test fake GPUDevice] createBindGroup entry count " + entries.length + " does not match its bind group layout's entry count " + layoutDesc.entries.length,
      );
    }
  }
  const state = {
    writeBufferCalls: [],
    submitCount: 0,
    renderPasses: [],
    computePasses: [],
    renderPipelines: [],
    computePipelines: [],
    shaderModules: [],
    bindGroups: [],
    buffers: [],
    // Texture lifecycle recording for the sprite path: every createTexture
    // (placeholder + the post-load upload target), writeTexture (placeholder
    // pixel), and copyExternalImageToTexture (the resolved image upload).
    textures: [],
    writeTextureCalls: [],
    copyExternalCalls: [],
    copyBufferToTextureCalls: [],
    // Render-bundle recording. renderBundleEncoders holds every encoder the
    // renderer opened; renderBundles holds every finished bundle.
    renderBundleEncoders: [],
    renderBundles: [],
  };
  var textureSeq = 0;
  function makePass(descriptor, kind) {
    const pass = {
      kind,
      descriptor,
      draws: [],
      drawIndirects: [],
      drawIndexeds: [],
      indexBuffers: [],
      pipelines: [],
      bindGroups: [],
      vertexBuffers: [],
      ended: false,
      setPipeline(pipeline) {
        pass.pipelines.push(pipeline);
      },
      setBindGroup(slot, group) {
        pass.bindGroups.push({ slot, group });
      },
      setVertexBuffer(slot, buffer, offset, size) {
        pass.vertexBuffers.push({ slot, buffer, offset, size });
      },
      setIndexBuffer(buffer, format, offset, size) {
        pass.indexBuffers.push({ buffer, format, offset, size });
      },
      drawIndexed(indexCount, instanceCount, firstIndex, baseVertex, firstInstance) {
        pass.drawIndexeds.push({
          indexCount,
          instanceCount: instanceCount == null ? 1 : instanceCount,
          firstIndex: firstIndex == null ? 0 : firstIndex,
          baseVertex: baseVertex == null ? 0 : baseVertex,
          firstInstance: firstInstance == null ? 0 : firstInstance,
          pipeline: pass.pipelines.length ? pass.pipelines[pass.pipelines.length - 1] : null,
        });
      },
      draw(vertexCount, instanceCount, firstVertex, firstInstance) {
        const values = [
          ["vertexCount", vertexCount],
          ["instanceCount", instanceCount == null ? 1 : instanceCount],
          ["firstVertex", firstVertex == null ? 0 : firstVertex],
          ["firstInstance", firstInstance == null ? 0 : firstInstance],
        ];
        for (const [name, value] of values) {
          if (!Number.isFinite(value) || !Number.isInteger(value) || value < 0 || value > 0xffffffff) {
            throw new TypeError("[gosx-test fake GPURenderPassEncoder] draw " + name + " must be an unsigned long, got " + String(value));
          }
        }
        pass.draws.push({
          vertexCount,
          instanceCount: instanceCount == null ? 1 : instanceCount,
          // The pipeline bound when the draw was issued — lets tests assert
          // WHICH path drew without coupling to setPipeline call ordering.
          pipeline: pass.pipelines.length ? pass.pipelines[pass.pipelines.length - 1] : null,
        });
      },
      // drawIndirect: mirrors draw() recording for the indirect path. Records
      // the buffer + offset + the currently-bound pipeline so tests can assert
      // WHICH path was taken (indirect vs direct) without GPU execution.
      drawIndirect(buffer, offset) {
        pass.drawIndirects.push({
          buffer,
          offset: offset == null ? 0 : offset,
          pipeline: pass.pipelines.length ? pass.pipelines[pass.pipelines.length - 1] : null,
        });
      },
      dispatchWorkgroups() {},
      // executeBundles records a render-bundle replay. Tests assert on
      // executedBundles to prove the renderer replayed rather than re-encoded.
      executeBundles(bundles) {
        const list = Array.isArray(bundles) ? bundles : [bundles];
        for (const item of list) pass.executedBundles.push(item);
      },
      end() {
        pass.ended = true;
      },
    };
    pass.executedBundles = [];
    return pass;
  }
  const device = {
    lost: options && options.lost ? options.lost : new Promise(() => {}),
    features: new Set(timestampQuery ? ["timestamp-query"] : []),
    limits: timestampQuery ? { timestampPeriod: 1 } : {},
    queue: {
      writeBuffer(buffer, offset, data) {
        state.writeBufferCalls.push({
          buffer,
          offset,
          data: data && typeof data.slice === "function" ? data.slice(0) : data,
        });
      },
      writeTexture(destination, data, layout, size) {
        state.writeTextureCalls.push({
          texture: destination && destination.texture,
          data: data && typeof data.slice === "function" ? data.slice(0) : data,
          size,
        });
      },
      copyExternalImageToTexture(source, destination, copySize) {
        state.copyExternalCalls.push({
          source: source && source.source,
          texture: destination && destination.texture,
          copySize,
        });
      },
      submit() {
        state.submitCount += 1;
      },
    },
    createBindGroupLayout(desc) {
      validateBindGroupLayoutDesc(desc);
      return { __kind: "bindGroupLayout", desc };
    },
    createPipelineLayout(desc) {
      return { __kind: "pipelineLayout", desc };
    },
    createShaderModule(desc) {
      const module = { __kind: "shaderModule", label: desc && desc.label, code: desc && desc.code };
      state.shaderModules.push(module);
      return module;
    },
    createComputePipeline(desc) {
      validateComputePipelineDesc(desc);
      const pipeline = { __kind: "computePipeline", label: desc && desc.label, desc };
      state.computePipelines.push(pipeline);
      return pipeline;
    },
    createComputePipelineAsync(desc) {
      return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
    },
    createRenderPipeline(desc) {
      validateRenderPipelineDesc(desc);
      const pipeline = { __kind: "renderPipeline", desc };
      state.renderPipelines.push(pipeline);
      return pipeline;
    },
    createBindGroup(desc) {
      validateBindGroupDesc(desc);
      const bindGroup = { __kind: "bindGroup", desc };
      state.bindGroups.push(bindGroup);
      return bindGroup;
    },
    createSampler(desc) {
      return { __kind: "sampler", desc };
    },
    createQuerySet(desc) {
      if (!timestampQuery) throw new Error("timestamp-query unsupported");
      return { __kind: "querySet", desc, destroy() {} };
    },
    createTexture(desc) {
      textureSeq += 1;
      const texture = {
        __kind: "texture",
        id: textureSeq,
        desc,
        createView() {
          return { __kind: "textureView", textureId: textureSeq };
        },
        destroy() { texture.destroyed = true; },
      };
      state.textures.push(texture);
      return texture;
    },
    createBuffer(desc) {
      const buffer = {
        __kind: "buffer",
        size: desc && desc.size || 0,
        usage: desc && desc.usage,
        destroy() { buffer.destroyed = true; },
      };
      if (timestampQuery) {
        buffer._backing = new ArrayBuffer(buffer.size);
        buffer.mapAsync = () => Promise.resolve();
        buffer.getMappedRange = () => buffer._backing;
        buffer.unmap = () => {};
      }
      if (desc && desc.mappedAtCreation) {
        const backing = new ArrayBuffer(buffer.size);
        buffer.getMappedRange = () => backing;
        buffer.unmap = () => {};
      }
      state.buffers.push(buffer);
      return buffer;
    },
    createCommandEncoder() {
      const encoder = {
        writeTimestamp() {},
        resolveQuerySet() {},
        copyBufferToBuffer(_source, _sourceOffset, destination) {
          if (timestampQuery && destination && destination._backing) {
            const values = new BigUint64Array(destination._backing);
            if (values.length >= 4) {
              // The per-pass ring resolves four stamps: shadow begin/end then
              // main begin/end. Give them a coherent ramp so the renderer's
              // millisecond arithmetic can be asserted: shadow 1 ms, main
              // 2.5 ms, whole scene 4 ms.
              values[0] = 0n;
              values[1] = 1_000_000n;
              values[2] = 1_500_000n;
              values[3] = 4_000_000n;
            } else {
              values[0] = 0n;
              values[1] = 4_000_000n;
            }
          }
        },
        copyBufferToTexture(source, destination, size) {
          state.copyBufferToTextureCalls.push({ source, destination, size });
        },
        beginRenderPass(descriptor) {
          const pass = makePass(descriptor, "render");
          state.renderPasses.push(pass);
          return pass;
        },
        beginComputePass(descriptor) {
          const pass = makePass(descriptor, "compute");
          state.computePasses.push(pass);
          return pass;
        },
        finish() {
          return { __kind: "commandBuffer" };
        },
      };
      if (!timestampEncoder) {
        delete encoder.writeTimestamp;
        delete encoder.resolveQuerySet;
        delete encoder.copyBufferToBuffer;
      } else if (!encoderWriteTimestamp) {
        delete encoder.writeTimestamp;
      }
      return encoder;
    },
    pushErrorScope() {},
    popErrorScope() {
      return Promise.resolve(null);
    },
  };
  // Render bundles are opt-in. Without createRenderBundleEncoder the renderer
  // takes the direct path, which is what every pre-existing harness test
  // asserts on. options.renderBundles: true turns the bundled path on so a test
  // can prove the encode, the replay and the invalidation.
  if (renderBundles) {
    device.createRenderBundleEncoder = function(descriptor) {
      const bundleEncoder = makePass(descriptor, "bundle");
      state.renderBundleEncoders.push(bundleEncoder);
      bundleEncoder.finish = function(finishDescriptor) {
        const bundle = {
          __kind: "renderBundle",
          label: (finishDescriptor && finishDescriptor.label) || (descriptor && descriptor.label) || "",
          descriptor,
          recorded: bundleEncoder,
        };
        state.renderBundles.push(bundle);
        return bundle;
      };
      return bundleEncoder;
    };
  }
  return { device, state };
}

// bootstrapChunkManifest is the generated chunk manifest that
// cmd/buildbootstrap writes on every build. It is the single source of truth
// for which bootstrap-src files each bundle carries, and in what order. This
// file used to repeat those lists by hand, so a manifest edit silently made
// the fresh-bundle helper below build a different bundle from the one that
// ships. `go run ./cmd/buildbootstrap --check` fails when chunks.json is stale.
const bootstrapChunkManifest = JSON.parse(
  fs.readFileSync(path.join(__dirname, "bootstrap-src", "chunks.json"), "utf8"),
);

// bootstrapChunkSources returns the bootstrap-src paths for one bundle, in
// build order. The paths in chunks.json are relative to client/js.
function bootstrapChunkSources(bundleName) {
  const chunk = bootstrapChunkManifest.chunks.find((c) => c.name === bundleName);
  if (!chunk) {
    throw new Error("bootstrapChunkSources: unknown bundle " + bundleName);
  }
  return chunk.sources;
}

// readBootstrapSrc joins the named bootstrap-src files in the given order. Use
// it when a test inspects source text that one file no longer holds alone.
function readBootstrapSrc(...names) {
  return names
    .map((name) => fs.readFileSync(
      name.startsWith("../") ? path.join(__dirname, name) : path.join(__dirname, "bootstrap-src", name),
      "utf8",
    ))
    .join("\n");
}

// readSceneMountSrc joins every 20x-scene-mount*.js file in build order. The
// old single 20-scene-mount.js was 10_127 lines and 43 percent of the base
// Scene3D chunk; it is now nine files. A source assertion about the mount path
// must read them all.
function readSceneMountSrc() {
  return readBootstrapSrc(
    "../runtime/scene3d/mount-backend.ts",
    "../runtime/scene3d/mount-webgl.ts",
    "../runtime/scene3d/mount-quality.ts",
    "../runtime/scene3d/overlays.ts",
    "../runtime/scene3d/mount-viewport.ts",
    "../runtime/scene3d/overlay-dom.ts",
    "../runtime/scene3d/mount-controls.ts",
    "../runtime/scene3d/mount-telemetry.ts",
    "../runtime/scene3d/mount.ts",
  );
}

// readWebGPUBackendSrc joins the WebGPU backend source files. The Selena
// uniform packer moved out of createSceneWebGPURenderer into 16a1, so a source
// assertion about the backend must read both files.
function readWebGPUBackendSrc() {
  return readBootstrapSrc("../runtime/scene3d/webgpu.ts", "16a1-scene-webgpu-selena-uniforms.js");
}

// readBootstrapTailSrc joins every 30x-tail-*.js file in build order. The old
// single 30-tail.js is now that file set.
function readBootstrapTailSrc() {
  const srcDir = path.join(__dirname, "bootstrap-src");
  return readBootstrapSrc(
    ...fs.readdirSync(srcDir).filter((n) => /^30[a-z]-tail-.*\.js$/.test(n)).sort(),
  );
}

// freshFeatureBundleSource concatenates the CURRENT bootstrap-src/*.js files
// for a given feature bundle, using the generated chunk manifest, but WITHOUT
// invoking esbuild/minification or writing anything to disk. The committed
// bootstrap-feature-*.js bundle artifacts (bootstrapFeatureScene3DSource /
// bootstrapFeatureScene3DWebGPUSource above) are prebuilt snapshots that go
// stale the moment a bootstrap-src file changes; most tests intentionally
// exercise that committed snapshot (it's what ships), but a test that needs to
// exercise a bootstrap-src edit BEFORE the bundles are regenerated (for
// example this file's pool-pass Selena-routing test) can opt into a bundle
// built fresh from bootstrap-src via this helper.
function freshFeatureBundleSource(name, options) {
  const clientJS = __dirname;
  const opts = options || {};
  function read(rel) {
    const source = fs.readFileSync(path.join(clientJS, rel), "utf8");
    if (rel.endsWith("webgl.ts") && opts.exportWaterRendererForTest) {
      return source + "\nwindow.__gosx_test_create_water_webgl = createSceneWaterRendererWebGL;\n";
    }
    return source;
  }
  return bootstrapChunkSources("bootstrap-feature-" + name + ".js").map(read).join("\n");
}

// createBoardWebGPUHarness boots the runtime + scene3d + scene3d-webgpu chunks
// (the production load order for 16a), points the 16z probe bridge at a ready
// fake device, and constructs the REAL createSceneWebGPURenderer over a fake
// canvas. Returns the renderer plus the recording device state and mount.
//
// options.fresh: when true, the scene3d + scene3d-webgpu chunks are built
// fresh from bootstrap-src/*.js (see freshFeatureBundleSource) instead of
// reading the committed bootstrap-feature-*.js bundle snapshots, so the
// harness exercises the CURRENT bootstrap-src source. Defaults to false
// (every existing call site keeps reading the committed bundles unchanged).
// options.fakeDeviceOptions is forwarded to makeFakeGPUDevice().
async function createBoardWebGPUHarness(options) {
  const opts = options || {};
  const env = createContext({ enableWebGPU: true, performanceNow: opts.performanceNow });
  // Most renderer harnesses assert the complete diagnostic attribute surface.
  // Production defaults to throttled telemetry; tests opt out only when they
  // are specifically verifying that production behavior.
  env.context.__gosx_scene3d_webgpu_telemetry = opts.verboseTelemetry !== false;
  // WebGPU usage-flag globals the renderer reads when creating resources.
  env.context.GPUBufferUsage = {
    MAP_READ: 0x1, MAP_WRITE: 0x2, COPY_SRC: 0x4, COPY_DST: 0x8,
    INDEX: 0x10, VERTEX: 0x20, UNIFORM: 0x40, STORAGE: 0x80,
    INDIRECT: 0x100, QUERY_RESOLVE: 0x200,
  };
  env.context.GPUTextureUsage = {
    COPY_SRC: 0x1, COPY_DST: 0x2, TEXTURE_BINDING: 0x4,
    STORAGE_BINDING: 0x8, RENDER_ATTACHMENT: 0x10,
  };
  env.context.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
  // The sprite texture path resolves images via Image.onload (createContext's
  // FakeImage fires it async) then createImageBitmap →
  // copyExternalImageToTexture. createContext has no createImageBitmap, so add
  // a minimal resolver that hands back a bitmap double carrying the source size
  // (1×1 per FakeImage) — without it wgpuLoadTexture marks the record failed
  // and never uploads.
  env.context.createImageBitmap = function(image) {
    return Promise.resolve({ __kind: "imageBitmap", width: image && image.width || 1, height: image && image.height || 1, close() {} });
  };

  const scene3DSource = opts.fresh ? freshFeatureBundleSource("scene3d") : bootstrapFeatureScene3DSource;
  const scene3DWebGPUSource = opts.fresh ? freshFeatureBundleSource("scene3d-webgpu") : bootstrapFeatureScene3DWebGPUSource;

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(scene3DSource, env.context, "bootstrap-feature-scene3d.js");
  // The GPU instanced cull moved to bootstrap-feature-scene3d-compute.js. Any
  // board scene here carries instanced meshes, so the mount would fetch the
  // chunk in a browser. Load it the same way.
  runScript(
    opts.fresh ? freshFeatureBundleSource("scene3d-compute") : bootstrapFeatureScene3DComputeSource,
    env.context,
    "bootstrap-feature-scene3d-compute.js",
  );
  await flushAsyncWork();
  assert.ok(env.context.__gosx_scene3d_api, "scene3d chunk must publish __gosx_scene3d_api");

  const fake = makeFakeGPUDevice(opts.fakeDeviceOptions);
  // The 16a factory consumes the probed adapter+device through the 16z
  // bridge global — point it at the fake before loading the webgpu chunk.
  env.context.__gosx_scene3d_webgpu_probe = function() {
    return { adapter: { __kind: "adapter" }, device: fake.device, ready: true };
  };

  runScript(scene3DWebGPUSource, env.context, "bootstrap-feature-scene3d-webgpu.js");
  const api = env.context.__gosx_scene3d_webgpu_api;
  assert.ok(api && typeof api.createRenderer === "function", "webgpu chunk must publish createRenderer");

  const mount = new FakeElement("div", null);
  const configureCalls = [];
  const gpuCtx = {
    configure(config) {
      configureCalls.push(config);
    },
    getCurrentTexture() {
      return {
        createView() {
          return { __kind: "canvasTextureView" };
        },
      };
    },
  };
  const canvas = {
    width: 640,
    height: 480,
    isConnected: true,
    childNodes: [],
    parentNode: mount,
    getBoundingClientRect() {
      return { width: 640, height: 480 };
    },
    getContext(kind) {
      return kind === "webgpu" ? gpuCtx : null;
    },
  };

  const renderer = api.createRenderer(canvas, {});
  assert.ok(renderer, "createRenderer must succeed against the fake device (got factory failure: " + String(env.context.__gosx_scene3d_webgpu_factory_error || "") + ")");
  return { env, renderer, fake, mount, canvas, configureCalls };
}

// mainRenderPasses filters out the init-time dummy-shadow clear pass (which
// has no color attachments) leaving the frame's main passes.
function mainRenderPasses(fake) {
  return fake.state.renderPasses.filter(
    (pass) => pass.descriptor && Array.isArray(pass.descriptor.colorAttachments) && pass.descriptor.colorAttachments.length > 0,
  );
}

// waterPoolSelenaFixture is the REAL Selena-compiled pool.sel WGSL + host
// binding descriptor (bindings.Layout), generated once via
// `selena.Compile(pool.sel, {Targets:[selena.TargetWGSL]})` -- the exact call
// examples/gosx-docs/.../water/selena_glsl.go's waterPoolSelenaWGSLData makes.
// See client/js/testdata/water-pool-selena.json for regeneration instructions.
const waterPoolSelenaFixture = JSON.parse(
  fs.readFileSync(path.join(__dirname, "testdata", "water-pool-selena.json"), "utf8"),
);

// waterSelenaFixture loads the REAL Selena-compiled WGSL + host binding
// descriptor for one of the remaining migrated water render passes
// (surface/surface-below/caustics/object-material/duck-material/object-shadow/
// compound-shadow/object-mesh-shadow), mirroring waterPoolSelenaFixture above.
// Each fixture is generated the same way: `selena.Compile(<file>, {Targets:
// [selena.TargetWGSL]})`, the exact call waterSelenaRenderWGSLData
// (selena_glsl.go) makes for every non-compute entry in waterSelenaShaders.
function waterSelenaFixture(slug) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, "testdata", "water-" + slug + "-selena.json"), "utf8"));
}
const waterSurfaceSelenaFixture = waterSelenaFixture("surface");
const waterSurfaceBelowSelenaFixture = waterSelenaFixture("surface-below");
const waterCausticsSelenaFixture = waterSelenaFixture("caustics");
const waterObjectMaterialSelenaFixture = waterSelenaFixture("object-material");
const waterDuckMaterialSelenaFixture = waterSelenaFixture("duck-material");
const waterObjectShadowSelenaFixture = waterSelenaFixture("object-shadow");
const waterCompoundShadowSelenaFixture = waterSelenaFixture("compound-shadow");
const waterObjectMeshShadowSelenaFixture = waterSelenaFixture("object-mesh-shadow");

// waterSelenaFixture also loads the five feedback-COMPUTE kernel fixtures
// (seed/drop/displacement/simulation/normal), generated the same way via
// `selena.Compile(<file>, {Targets:[selena.TargetWGSL]})` -- the exact call
// waterSelenaComputeWGSLData (selena_glsl.go) makes for each compute entry in
// waterSelenaShaders. Used by the Selena feedback-compute path test below.
const waterSeedSelenaFixture = waterSelenaFixture("seed");
const waterDropSelenaFixture = waterSelenaFixture("drop");
const waterDisplacementSelenaFixture = waterSelenaFixture("displacement");
const waterSimulationSelenaFixture = waterSelenaFixture("simulation");
const waterNormalSelenaFixture = waterSelenaFixture("normal");

// waterSelenaFrameEntry builds a minimal <WaterSystem>-entry-shaped object
// (mirroring the pool test's waterEntry) carrying every additive
// "<pass>SelenaWGSL" slot + shaderDescriptors key this task wires, so any
// combination of the newly-migrated passes can be exercised without repeating
// the whole field list per test.
function waterSelenaFrameEntry(overrides) {
  return Object.assign({
    id: "water-main",
    resolution: 4,
    poolShape: "Box",
    poolWidth: 1,
    poolLength: 1,
    poolHeight: 1,
    cornerRadius: 0,
    tileTexture: "",
    cubeMap: "",
    causticsResolution: 4,
    objectShadowResolution: 4,
    lightDirectionX: 2,
    lightDirectionY: 2,
    lightDirectionZ: -1,
    caustics: true,
    reflection: true,
    refraction: true,
    materialBackend: "selena",
    surfaceSelenaWGSL: waterSurfaceSelenaFixture.wgsl,
    surfaceBelowSelenaWGSL: waterSurfaceBelowSelenaFixture.wgsl,
    causticsSelenaWGSL: waterCausticsSelenaFixture.wgsl,
    objectShadowSelenaWGSL: waterObjectShadowSelenaFixture.wgsl,
    compoundShadowSelenaWGSL: waterCompoundShadowSelenaFixture.wgsl,
    objectMeshShadowSelenaWGSL: waterObjectMeshShadowSelenaFixture.wgsl,
    shaderDescriptors: {
      surface: waterSurfaceSelenaFixture.layout,
      surfaceBelow: waterSurfaceBelowSelenaFixture.layout,
      caustics: waterCausticsSelenaFixture.layout,
      objectShadow: waterObjectShadowSelenaFixture.layout,
      compoundShadow: waterCompoundShadowSelenaFixture.layout,
      objectMeshShadow: waterObjectMeshShadowSelenaFixture.layout,
    },
  }, overrides || {});
}

// waterSelenaFieldFloats reads the descriptor's declared byte `offset` for
// `fieldName` (layout.uniformBlock.fields, the SAME field list
// sceneSelenaUniformData/sceneSelenaWriteUniformField walk) and slices `count`
// floats out of an already-captured Float32Array uniform write -- lets value
// assertions below address fields by NAME (matching the .sel source) instead
// of hand-computed/hardcoded byte offsets, so they stay correct if the
// compiled layout ever shifts.
function waterSelenaFieldFloats(layout, floats, fieldName, count) {
  const field = layout.uniformBlock.fields.find((f) => f.name === fieldName);
  assert.ok(field, "expected a uniformBlock field named " + fieldName);
  const base = field.offset / 4;
  return Array.from(floats.slice(base, base + count));
}

// waterSelenaLastUniformWrite finds the LAST queue.writeBuffer call the fake
// device recorded against the uniform buffer bound at binding 0 of
// `bindGroup` -- the actual bytes the renderer sent the GPU for this
// material's draw this frame.
function waterSelenaLastUniformWrite(fake, bindGroup) {
  const uniformEntry = bindGroup.desc.entries.find((e) => e.binding === 0);
  assert.ok(uniformEntry, "expected a binding-0 (uniform block) entry in the bind group");
  const buffer = uniformEntry.resource.buffer;
  const writes = fake.state.writeBufferCalls.filter((c) => c.buffer === buffer);
  assert.ok(writes.length > 0, "expected at least one writeBuffer call against the uniform buffer");
  return writes[writes.length - 1].data;
}

// waterPerfShapeEntry builds a full <WaterSystem> entry (every render-pass
// Selena slot from waterSelenaFrameEntry PLUS the 5 feedback-compute kernel
// slots) so a single rendered frame exercises the complete migrated pass set:
// pool/surface/surface-below/caustics/object-shadow/compound-shadow/
// object-mesh-shadow render passes + seed/drop/displacement/simulation/normal
// compute kernels. `duck: true` switches the active object to a kind:3
// (compound) subject matching the water demo's Rubber Duck config (objectKind
// "compound"/objectSubtype "duck"), which is the ONLY water-object kind that
// engages waterSystemHasObjectTextureSubject's object-texture RTT passes
// (refraction/reflection/clipped-reflection) and the projected mesh-shadow
// pass (see waterSystemUsesProjectedObjectTextures, kind===3 gate) -- `duck:
// false` (kind 1, Sphere) exercises the cheaper projected-sphere shadow path
// instead, so diffing the two isolates the passes the task says the duck adds
// from any incidental per-frame churn shared by both.
function waterPerfShapeEntry(duck) {
  const entry = waterSelenaFrameEntry({
    activeObject: duck ? "Rubber Duck" : "Sphere",
    objectKind: duck ? "compound" : "sphere",
    objectSubtype: duck ? "duck" : undefined,
    objectX: duck ? 0.4 : -0.4,
    objectY: -0.7,
    objectZ: duck ? -0.2 : 0.2,
    objectRadius: duck ? 0.1 : 0.25,
    objectDisplacementScale: 1,
    objectDisplacementSpheres: duck ? [
      { offsetX: 0.2, offsetY: 0.05, offsetZ: -0.1, radius: 0.08 },
      { offsetX: -0.15, offsetY: 0.02, offsetZ: 0.12, radius: 0.05 },
    ] : undefined,
    seedDrops: 0,
    dropEventID: 0,
  });
  entry.seedSelenaWGSL = waterSeedSelenaFixture.wgsl;
  entry.dropSelenaWGSL = waterDropSelenaFixture.wgsl;
  entry.displacementSelenaWGSL = waterDisplacementSelenaFixture.wgsl;
  entry.simulationSelenaWGSL = waterSimulationSelenaFixture.wgsl;
  entry.normalSelenaWGSL = waterNormalSelenaFixture.wgsl;
  entry.shaderDescriptors = Object.assign({}, entry.shaderDescriptors, {
    seed: waterSeedSelenaFixture.layout,
    drop: waterDropSelenaFixture.layout,
    displacement: waterDisplacementSelenaFixture.layout,
    simulation: waterSimulationSelenaFixture.layout,
    normal: waterNormalSelenaFixture.layout,
  });
  return entry;
}

// waterPerfShapeScene builds the material + object list for one perf-shape
// scenario against a caller-supplied `api` (from a fresh harness), mirroring
// the object-material/duck-material test above but parameterized on `duck`.
function waterPerfShapeScene(api, duck, resolution) {
  const materialName = duck ? "water-duck-material" : "water-object-material";
  const fixture = duck ? waterDuckMaterialSelenaFixture : waterObjectMaterialSelenaFixture;
  const objectID = duck ? "float-duck" : "float-sphere";
  const customUniforms = {
    poolHeight: 1,
    baseColor: duck ? [1, 1, 1, 1] : [0.5, 0.5, 0.5, 1],
    isTexturePass: 0,
    texturePassMode: 0,
    lightDir: [2, 3, -1],
    grid: 4,
    water: "gosx:water:water-main:state",
  };
  if (duck) customUniforms.modelTexture = "/water/models/duck/DuckCM.png";
  const waterEntry = waterPerfShapeEntry(duck);
  if (resolution) waterEntry.resolution = resolution;
  const state = api.createSceneState({
    scene: {
      materials: [{
        name: materialName,
        kind: "custom",
        shaderBackend: "selena",
        customVertexWGSL: fixture.wgsl,
        customFragmentWGSL: fixture.wgsl,
        shaderLayout: fixture.layout,
        customUniforms,
      }],
      objects: [
        { id: objectID, kind: "sphere", radius: duck ? 0.1 : 0.25, x: duck ? 0.4 : -0.4, y: -0.7, z: duck ? -0.2 : 0.2, material: materialName, castShadow: true, receiveShadow: true, wireframe: false },
      ],
      waterSystems: [waterEntry],
    },
  });
  const objects = api.sceneStateObjectsWithMaterials(state);
  return { state, objects };
}

// deviceCallSnapshot captures the cumulative device-call counters the fake
// GPUDevice records that a steady-state (post-warmup) frame must NOT keep
// growing: pipeline/shader-module compiles (memoized, should be 0 after frame
// 1), bind groups (pooled, should plateau), and queue.writeBuffer call count +
// total byte volume (uniforms only once warm -- no per-frame mesh re-upload).
function deviceCallSnapshot(fake) {
  const bytes = fake.state.writeBufferCalls.reduce((sum, c) => sum + (c.data && c.data.byteLength || 0), 0);
  return {
    renderPipelines: fake.state.renderPipelines.length,
    computePipelines: fake.state.computePipelines.length,
    shaderModules: fake.state.shaderModules.length,
    bindGroups: fake.state.bindGroups.length,
    renderPasses: fake.state.renderPasses.length,
    computePasses: fake.state.computePasses.length,
    writeBufferCalls: fake.state.writeBufferCalls.length,
    writeBufferBytes: bytes,
  };
}

function deviceCallDelta(before, after) {
  return {
    renderPipelines: after.renderPipelines - before.renderPipelines,
    computePipelines: after.computePipelines - before.computePipelines,
    shaderModules: after.shaderModules - before.shaderModules,
    bindGroups: after.bindGroups - before.bindGroups,
    renderPasses: after.renderPasses - before.renderPasses,
    computePasses: after.computePasses - before.computePasses,
    writeBufferCalls: after.writeBufferCalls - before.writeBufferCalls,
    writeBufferBytes: after.writeBufferBytes - before.writeBufferBytes,
  };
}

// renderWaterPerfShapeFrames renders N consecutive frames of one scenario
// (`duck` true/false) against a FRESH harness (fresh device + renderer), and
// returns the per-frame device-call delta array. Each frame rebuilds the
// bundle via api.createSceneRenderBundle -- exactly like a real animation
// loop re-marshaling scene state every tick -- so meshObjects/materials
// entries are BRAND-NEW JS objects each frame (see createSceneRenderBundle's
// appendSceneObjectToBundle, which always pushes a fresh object), the same
// identity churn a live Go-driven frame loop produces. Any cache keyed on
// per-frame object identity rather than a stable owner (the WaterSystem
// instance, the material) will show up here as non-zero createBindGroup/
// createBuffer(writeBuffer) growth on every single frame, not just frame 1.
async function renderWaterPerfShapeFrames(duck, frameCount, resolution) {
  const harness = await createBoardWebGPUHarness({
    fresh: true,
    fakeDeviceOptions: { validateBindings: true },
  });
  const api = harness.env.context.__gosx_scene3d_api;
  const { state, objects } = waterPerfShapeScene(api, duck, resolution);
  harness.canvas.width = 64;
  harness.canvas.height = 64;

  const deltas = [];
  for (let frame = 0; frame < frameCount; frame += 1) {
    const bundle = api.createSceneRenderBundle(
      64, 64, "#000000",
      { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
      objects, [], [], [], [], {}, frame * 0.016, [], [], [], state.waterSystems, [], 0, false,
    );
    const before = deviceCallSnapshot(harness.fake);
    harness.renderer.render(bundle, { width: 64, height: 64 }, {
      nowMS: frame * 17,
      displayDeltaMS: frame === 0 ? 0 : 17,
      active: true,
      qualityTier: resolution === 128 ? "survival" : (resolution === 192 ? "balanced" : "full"),
      qualityRevision: 0,
    });
    const after = deviceCallSnapshot(harness.fake);
    deltas.push(deviceCallDelta(before, after));
  }
  return { harness, deltas };
}

// waterComputeKernelPipeline finds the compiled gosx-selena-compute-<Material>
// pipeline for one feedback kernel among every createComputePipeline call the
// fake device recorded this test.
function waterComputeKernelPipeline(fake, materialName) {
  return fake.state.computePipelines.find(
    (p) => p.label && String(p.label).indexOf("gosx-selena-compute-" + materialName) >= 0,
  );
}

// assertWaterComputeKernelBindings resolves kernel's compiled pipeline (by
// Selena material name), asserts its bind group layout carries exactly
// wantBindings, and that a bind group was actually built against that exact
// layout with the same binding set -- the compute analogue of the render
// tests' pool/caustics/object-material bind-group corroboration above.
function assertWaterComputeKernelBindings(fake, materialName, wantBindings) {
  const pipeline = waterComputeKernelPipeline(fake, materialName);
  assert.ok(pipeline, "expected a compiled gosx-selena-compute-" + materialName + " pipeline");
  const bgl = pipeline.desc.layout.desc.bindGroupLayouts[0];
  const bindings = Array.from(bgl.desc.entries, (e) => e.binding).sort((a, b) => a - b);
  assert.deepEqual(bindings, wantBindings, materialName + " compute bind group layout bindings");
  const boundGroup = fake.state.bindGroups.find((bg) => bg.desc && bg.desc.layout === bgl);
  assert.ok(boundGroup, "expected a bind group built against the " + materialName + " compute bind group layout");
  const boundBindings = Array.from(boundGroup.desc.entries, (e) => e.binding).sort((a, b) => a - b);
  assert.deepEqual(boundBindings, bindings, materialName + " bind group entries must match its layout's declared bindings exactly");
  return pipeline;
}

// -----------------------------------------------------------------------------
// DOM CanvasBoard overlays — labels/html sync + dispose
//
// 26b2-canvas-board-labels.js positions real HTML <span> elements over the
// canvas board so text renders in the DOM rather than via GPU fillText. The
// tests below exercise the OrthoCamera2D transform parity, index-keyed label
// reconciliation, keyed HTML reconciliation, culling, defaults, dispose, and
// layer invariants.
//
// The module runs in an isolated VM sandbox with a minimal FakeDocument so
// createElement / appendChild work without a real browser. The offscreen-canvas
// probe finds no canvas support in this environment and falls back to the
// 0.8 * fontSize ascent approximation, which is what the assertions target.
// -----------------------------------------------------------------------------

// loadBoardLabels evaluates 26b2 in an isolated sandbox and returns the exposed
// globals. The sandbox wires a FakeDocument for createElement so overlay
// elements can be created.
function loadBoardLabels() {
  const source = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "26b2-canvas-board-labels.js"),
    "utf8",
  );
  const fakeDoc = new FakeDocument();
  const sandbox = {
    document: fakeDoc,
  };
  sandbox.window = sandbox;
  vm.createContext(sandbox);
  vm.runInContext(source, sandbox, { filename: "26b2-canvas-board-labels.js" });
  assert.equal(typeof sandbox.window.__gosx_canvas_board_labels_sync, "function",
    "26b2 must expose __gosx_canvas_board_labels_sync");
  assert.equal(typeof sandbox.window.__gosx_canvas_board_labels_dispose, "function",
    "26b2 must expose __gosx_canvas_board_labels_dispose");
  assert.equal(typeof sandbox.window.__gosx_canvas_board_html_sync, "function",
    "26b2 must expose __gosx_canvas_board_html_sync");
  assert.equal(typeof sandbox.window.__gosx_canvas_board_html_dispose, "function",
    "26b2 must expose __gosx_canvas_board_html_dispose");
  return {
    sync: sandbox.window.__gosx_canvas_board_labels_sync,
    dispose: sandbox.window.__gosx_canvas_board_labels_dispose,
    htmlSync: sandbox.window.__gosx_canvas_board_html_sync,
    htmlDispose: sandbox.window.__gosx_canvas_board_html_dispose,
    doc: fakeDoc,
  };
}

// makeBoardHost creates a FakeElement to serve as the canvas's parent (host).
function makeBoardHost(doc = new FakeDocument()) {
  const host = doc.createElement("div");
  doc.body.appendChild(host);
  return host;
}

function layer_childCount(host) {
  return host.__gosxBoardLabelLayer ? host.__gosxBoardLabelLayer.childNodes.length : 0;
}

// -----------------------------------------------------------------------------
// M1 slice 4: canvas2d surface → 16a WebGPU renderer routing (behind the flag).
//
// These drive the REAL routed-mount flow in the engines feature: a canvas2d
// surface-kind placeholder carrying data-gosx-canvas-backend="webgpu", with the
// scene3d-webgpu chunk loaded (16a factory live against the fake GPU device) and
// the WASM canvas globals faked, mounts through mountAllSurfaceKinds →
// mountSurfaceKind → _startCanvasSurfaceWebGPURAF. The RAF loop then drives
// 16a's render() (recorded draws) + the DOM label/html overlays. They also pin the
// skip-frame contract, the painter default (flag absent), and the complete
// fallback (flag on but no navigator.gpu → painter path + one warn).
// -----------------------------------------------------------------------------

// createCanvasBoardRoutingHarness loads runtime + scene3d + scene3d-webgpu +
// engines into one context, wires a fake GPU device + 16z probe so the 16a
// factory is live, fakes the four WASM canvas globals, installs manual RAF, and
// returns handles to mount a canvas2d board and pump frames. The WASM
// __gosx_render_canvas double returns a caller-supplied bundle JSON (so a test
// controls the skip-frame string), records every call, and the set-backend
// double records (id, backend) so a test asserts the routing handshake.
async function createCanvasBoardRoutingHarness(options = {}) {
  const env = createContext({ enableWebGPU: true });
  env.context.GPUBufferUsage = {
    MAP_READ: 0x1, MAP_WRITE: 0x2, COPY_SRC: 0x4, COPY_DST: 0x8,
    INDEX: 0x10, VERTEX: 0x20, UNIFORM: 0x40, STORAGE: 0x80,
    INDIRECT: 0x100, QUERY_RESOLVE: 0x200,
  };
  env.context.GPUTextureUsage = {
    COPY_SRC: 0x1, COPY_DST: 0x2, TEXTURE_BINDING: 0x4,
    STORAGE_BINDING: 0x8, RENDER_ATTACHMENT: 0x10,
  };
  env.context.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
  env.context.createImageBitmap = function(image) {
    return Promise.resolve({ __kind: "imageBitmap", width: image && image.width || 1, height: image && image.height || 1, close() {} });
  };

  const raf = installManualRAF(env.context);

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  const fake = makeFakeGPUDevice();
  // Point the 16z probe bridge at the fake device UNLESS the test wants the
  // GPU path to be unavailable (the no-navigator.gpu fallback case).
  if (options.webgpuUnavailable) {
    // Simulate "no navigator.gpu": the routed path's _canvasSurfaceWebGPUUsable
    // returns false and must fall back to the painter.
    env.context.navigator.gpu = undefined;
  } else {
    env.context.__gosx_scene3d_webgpu_probe = function() {
      return { adapter: { __kind: "adapter" }, device: fake.device, ready: true };
    };
    runScript(bootstrapFeatureScene3DWebGPUSource, env.context, "bootstrap-feature-scene3d-webgpu.js");
    assert.ok(
      env.context.__gosx_scene3d_webgpu_api && typeof env.context.__gosx_scene3d_webgpu_api.createRenderer === "function",
      "webgpu chunk must publish createRenderer",
    );
  }

  // Register the engines feature factory (it calls __gosx_register_bootstrap_feature).
  runScript(bootstrapFeatureEnginesSource, env.context, "bootstrap-feature-engines.js");
  const enginesFactory = env.context.__gosx_bootstrap_features && env.context.__gosx_bootstrap_features.engines;
  assert.equal(typeof enginesFactory, "function", "engines feature factory must be registered");

  // The canvas2d/webgpu path uses none of the engine-mount api.* helpers, so a
  // minimal stub API suffices to construct the feature (mountEngineSurface etc.
  // are never reached in this flow). engineFactories is the one field read at
  // construction time (assigned to a local), so provide an empty object.
  const feature = enginesFactory({
    engineFactories: {},
    sceneNumber: (v, d) => (typeof v === "number" ? v : d),
    sceneBool: (v, d) => (typeof v === "boolean" ? v : d),
  });
  assert.ok(feature && typeof feature.runtimeReady === "function", "engines feature must expose runtimeReady");

  // Fake the four WASM canvas globals. render returns the controllable bundle
  // JSON; tick/set_backend/dispose record calls.
  const renderCalls = [];
  const setBackendCalls = [];
  const tickCalls = [];
  const disposeCalls = [];
  let renderJSONProvider = options.renderJSON || (() => goBoardBundleMixedWithHTMLJSON);
  env.context.__gosx_render_canvas = function(id, w, h, t) {
    renderCalls.push({ id, w, h, t });
    return typeof renderJSONProvider === "function" ? renderJSONProvider({ id, w, h, t }) : renderJSONProvider;
  };
  env.context.__gosx_tick_canvas = function(id) { tickCalls.push(id); return null; };
  env.context.__gosx_canvas_set_backend = function(id, backend) { setBackendCalls.push({ id, backend }); return null; };
  env.context.__gosx_dispose_canvas = function(id) { disposeCalls.push(id); return null; };
  // The WASM hydrate global the surface-kind mount calls before the paint loop —
  // a board with no program; return "" (success).
  env.context.__gosx_hydrate = function() { return ""; };

  // A canvas2d surface-kind placeholder (already a <canvas>) carrying the
  // backend opt-in. getContext("webgpu") returns a configurable context double;
  // getContext("2d") returns a painter context double (used only on fallback).
  const mountParent = new FakeElement("div", env.context.document);
  const ctx2dCalls = [];
  const ctx2d = makeFakeContext2D();
  const gpuCtx = {
    configure() {},
    getCurrentTexture() {
      return { createView() { return { __kind: "canvasTextureView" }; } };
    },
  };
  const canvas = new FakeElement("canvas", env.context.document);
  canvas.setAttribute("data-gosx-surface-kind", "canvas2d");
  if (options.flag !== false) {
    canvas.setAttribute("data-gosx-canvas-backend", "webgpu");
  }
  canvas.width = 640;
  canvas.height = 480;
  canvas.clientWidth = 640;
  canvas.clientHeight = 480;
  canvas.isConnected = true;
  canvas.getBoundingClientRect = () => ({ width: 640, height: 480, left: 0, top: 0 });
  canvas.getContext = (kind) => {
    if (kind === "webgpu") return gpuCtx;
    if (kind === "2d") { ctx2dCalls.push("2d"); return ctx2d; }
    return null;
  };
  canvas.focus = () => {};
  mountParent.appendChild(canvas);
  env.context.document.body.appendChild(mountParent);

  // mountAllSurfaceKinds discovers placeholders via document.querySelectorAll.
  // FakeDocument has none; install a one-off matcher on THIS document instance
  // (zero risk to other tests) that returns our canvas for the surface-kind
  // query and nothing for the bytecode query.
  env.context.document.querySelectorAll = function(selector) {
    if (selector === "[data-gosx-surface-kind]:not([data-gosx-engine-bytecode])") {
      return canvas.hasAttribute("data-gosx-engine-bytecode") ? [] : [canvas];
    }
    return [];
  };

  return {
    env, fake, feature, raf, canvas, mountParent, ctx2d, ctx2dCalls,
    renderCalls, setBackendCalls, tickCalls, disposeCalls,
    setRenderJSON(fn) { renderJSONProvider = fn; },
    async mount() {
      await feature.runtimeReady({});
      await flushAsyncWork();
    },
    labelLayer() {
      return mountParent.__gosxBoardLabelLayer || null;
    },
    htmlLayer() {
      return mountParent.__gosxBoardHTMLLayer || null;
    },
  };
}

// -----------------------------------------------------------------------------
// M1 slice 4 perf: getSelenaPipeline memoizes the resolved pipeline+key on the
// material (_gosxWGPUSelenaResource), so the ~1.2KB content key is built ONCE
// PER MATERIAL per frame instead of once per OBJECT. Board frames are
// fresh-parsed (a material object lives one frame) but a material is shared by
// every object that references it, so this is the difference between a handful
// of key-builds and hundreds.
// -----------------------------------------------------------------------------

// boardBundleManyObjectsOneMaterial builds a GPU board bundle where N rect
// objects all reference materials[0] (one BoardFill Selena material) — the
// shape AttachBoardGPUGeometry produces when N same-color rects dedupe onto one
// material. Each object points at its own 6-vertex quad in the shared World*
// streams. Mirrors the fixture wire shape (camera ortho2d, flat+Selena material).
function boardBundleManyObjectsOneMaterial(n) {
  const selena = JSON.parse(goBoardBundleRectsJSON).materials[0]; // a real BoardFill Selena material (WGSL + layout)
  const objects = [];
  const positions = [];
  const normals = [];
  const uvs = [];
  for (let i = 0; i < n; i++) {
    const x0 = i * 12, y0 = 0, x1 = x0 + 10, y1 = 10;
    objects.push({ kind: "rect", materialIndex: 0, vertexOffset: i * 6, vertexCount: 6, bounds: { minX: x0, minY: y0, maxX: x1, maxY: y1 } });
    positions.push(x0, y0, 0, x1, y0, 0, x1, y1, 0, x0, y0, 0, x1, y1, 0, x0, y1, 0);
    normals.push(0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1);
    uvs.push(0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1);
  }
  return {
    camera: { mode: "ortho2d", x: 0, y: 0, z: 1, near: -1, far: 1 },
    background: "#102030",
    objects,
    objectCount: n,
    materials: [selena],
    worldPositions: positions,
    worldNormals: normals,
    worldUVs: uvs,
  };
}

// makeFakeGPUDeviceForCompute extends the board fake with controllable async
// pipeline behaviour, per-module compilation info, and error-scope control,
// needed for the payload-kernel validation tests.  pipelineAsyncBehavior is a
// function called with the pipeline descriptor on createComputePipelineAsync;
// it should return either a resolved or rejected Promise.
// compilationInfoBehavior is a function called with the shader-module
// descriptor; it should return an array of GPUCompilationMessage-like objects
// for that module, or nothing for a clean module.
//
// errorScopeBehavior remains only so the older tests keep their shape. The
// renderer no longer validates pipelines through the device error scope: that
// stack is DEVICE-GLOBAL, and popping it from six overlapping async builds
// attributed one build's error to another. Per-pipeline verdicts come from
// create*PipelineAsync and per-module reasons from getCompilationInfo, both of
// which are keyed to the object they describe.
function makeFakeGPUDeviceForCompute(options) {
  const base = makeFakeGPUDevice();
  const device = base.device;
  const opts = options || {};
  const pendingScopes = [];
  const computePipelineAsyncCalls = [];
  const innerCreateShaderModule = device.createShaderModule;
  device.createShaderModule = function(desc) {
    const module = innerCreateShaderModule.call(device, desc);
    module.getCompilationInfo = function() {
      const messages = typeof opts.compilationInfoBehavior === "function"
        ? (opts.compilationInfoBehavior(desc) || [])
        : [];
      return Promise.resolve({ messages });
    };
    return module;
  };
  device.pushErrorScope = function(filter) {
    pendingScopes.push(filter);
  };
  device.popErrorScope = function() {
    pendingScopes.pop();
    if (typeof opts.errorScopeBehavior === "function") {
      return opts.errorScopeBehavior();
    }
    return Promise.resolve(null);
  };
  device.createComputePipelineAsync = function(desc) {
    computePipelineAsyncCalls.push(desc);
    if (typeof opts.pipelineAsyncBehavior === "function") {
      return opts.pipelineAsyncBehavior(desc);
    }
    return Promise.resolve({ __kind: "computePipeline", label: desc && desc.label });
  };
  return { device, state: base.state, computePipelineAsyncCalls };
}

// createComputeParticleHarness boots the same chunk stack as
// createBoardWebGPUHarness but injects a caller-supplied fake device instead
// of the default makeFakeGPUDevice(), enabling per-test async pipeline control.
async function createComputeParticleHarness(fakeDevice, options) {
  const opts = options || {};
  const env = createContext({
    enableWebGPU: true,
    performanceNow: opts.performanceNow,
  });
  env.context.GPUBufferUsage = {
    MAP_READ: 0x1, MAP_WRITE: 0x2, COPY_SRC: 0x4, COPY_DST: 0x8,
    INDEX: 0x10, VERTEX: 0x20, UNIFORM: 0x40, STORAGE: 0x80,
    INDIRECT: 0x100, QUERY_RESOLVE: 0x200,
  };
  env.context.GPUTextureUsage = {
    COPY_SRC: 0x1, COPY_DST: 0x2, TEXTURE_BINDING: 0x4,
    STORAGE_BINDING: 0x8, RENDER_ATTACHMENT: 0x10,
  };
  env.context.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
  env.context.createImageBitmap = function(image) {
    return Promise.resolve({ __kind: "imageBitmap", width: image && image.width || 1, height: image && image.height || 1, close() {} });
  };

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  // The particle systems and the GPU instanced cull left the base chunk for
  // bootstrap-feature-scene3d-compute.js. The mount fetches it for a scene
  // that declares particles or instanced meshes, which is exactly what these
  // harnesses build, so load it here the way the browser would.
  runScript(bootstrapFeatureScene3DComputeSource, env.context, "bootstrap-feature-scene3d-compute.js");
  await flushAsyncWork();

  env.context.__gosx_scene3d_webgpu_probe = function() {
    return { adapter: { __kind: "adapter" }, device: fakeDevice, ready: true };
  };
  runScript(bootstrapFeatureScene3DWebGPUSource, env.context, "bootstrap-feature-scene3d-webgpu.js");

  const api = env.context.__gosx_scene3d_webgpu_api;
  const mount = new FakeElement("div", null);
  const gpuCtx = {
    configure() {},
    getCurrentTexture() {
      return { createView() { return { __kind: "canvasTextureView" }; } };
    },
  };
  const canvas = {
    width: 640, height: 480, isConnected: true, childNodes: [],
    parentNode: mount,
    getBoundingClientRect() { return { width: 640, height: 480 }; },
    getContext(kind) { return kind === "webgpu" ? gpuCtx : null; },
  };

  // Capture console.warn calls for assertion.
  const warnLog = [];
  const origWarn = env.context.console.warn;
  env.context.console.warn = function() {
    warnLog.push(Array.from(arguments).join(" "));
    if (typeof origWarn === "function") origWarn.apply(env.context.console, arguments);
  };

  const renderer = api.createRenderer(canvas, {});
  return { env, renderer, warnLog };
}

// Minimal valid Scene3D bundle with one compute particle entry.
function makeComputeParticleBundle(particleEntry) {
  return {
    camera: { x: 0, y: 0, z: 5, fov: 72, near: 0.05, far: 128 },
    environment: {},
    points: [], instancedMeshes: [], objects: [], meshObjects: [],
    materials: [], labels: [], sprites: [], lights: [], postEffects: [],
    computeParticles: [particleEntry],
    positions: new Float32Array(0), colors: new Float32Array(0),
    worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
    worldLineWidths: new Float32Array(0),
    worldMeshPositions: new Float32Array(0), worldMeshColors: new Float32Array(0),
    worldMeshNormals: new Float32Array(0), worldMeshUVs: new Float32Array(0),
    worldMeshTangents: new Float32Array(0),
    vertexCount: 0, worldVertexCount: 0,
  };
}

// -------------------------------------------------------------------------
// Points authored shader: WebGPU async pipeline tests (S2 rungs)
// -------------------------------------------------------------------------

// makeFakeGPUDeviceForPoints extends makeFakeGPUDevice with a controllable
// createRenderPipelineAsync — needed for points authored-pipeline tests.
function makeFakeGPUDeviceForPoints(options) {
  const base = makeFakeGPUDevice();
  const device = base.device;
  const opts = options || {};
  const pendingScopes = [];
  const renderPipelineAsyncCalls = [];
  device.pushErrorScope = function(filter) {
    pendingScopes.push(filter);
  };
  device.popErrorScope = function() {
    pendingScopes.pop();
    if (typeof opts.errorScopeBehavior === "function") {
      return opts.errorScopeBehavior();
    }
    return Promise.resolve(null);
  };
  device.createRenderPipelineAsync = function(desc) {
    renderPipelineAsyncCalls.push(desc);
    if (typeof opts.pipelineAsyncBehavior === "function") {
      return opts.pipelineAsyncBehavior(desc);
    }
    return Promise.resolve({ __kind: "renderPipeline", label: desc && desc.label });
  };
  return { device, state: base.state, renderPipelineAsyncCalls };
}

// Minimal valid bundle with one points entry (no positions — count-only path).
function makePointsBundle(pointsEntry) {
  return {
    camera: { x: 0, y: 0, z: 5, fov: 72, near: 0.05, far: 128 },
    environment: {},
    points: [pointsEntry],
    instancedMeshes: [], objects: [], meshObjects: [],
    materials: [], labels: [], sprites: [], lights: [], postEffects: [],
    computeParticles: [],
    positions: new Float32Array(0), colors: new Float32Array(0),
    worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
    worldLineWidths: new Float32Array(0),
    worldMeshPositions: new Float32Array(0), worldMeshColors: new Float32Array(0),
    worldMeshNormals: new Float32Array(0), worldMeshUVs: new Float32Array(0),
    worldMeshTangents: new Float32Array(0),
    vertexCount: 0, worldVertexCount: 0,
  };
}

// -------------------------------------------------------------------------
// ComputeParticles authored render: WebGPU async pipeline tests (S3 rungs)
// -------------------------------------------------------------------------

// Minimal valid bundle with one compute particle entry using an authored render.
function makeComputeParticleBundleWithRender(particleEntry) {
  return makeComputeParticleBundle(particleEntry);
}

// -------------------------------------------------------------------------
// shaderLib hydrate: inflate refs back to inline fields before WASM hydration
// -------------------------------------------------------------------------

// Build a minimal manifest entry with a shaderLib-deduplicated scene.
function makeShaderLibManifestEntry(lib, computeParticles) {
  return {
    islands: [
      {
        id: "gosx-island-shader-test",
        component: "Galaxy",
        bundleId: "test-bundle",
        props: {
          scene: {
            computeParticles: computeParticles,
            shaderLib: lib,
          },
        },
        programRef: "/test.json",
        programFormat: "json",
      },
    ],
    bundles: { "test-bundle": { path: "/test.wasm" } },
    runtime: { path: "/test-runtime.wasm" },
  };
}

// -------------------------------------------------------------------------
// shaderLib hydrate: inflate for Points authored WGSL ref fields (S2 rungs)
// -------------------------------------------------------------------------

// Build a minimal manifest entry with a shaderLib containing points entries.
function makeShaderLibManifestEntryForPoints(lib, points) {
  return {
    islands: [
      {
        id: "gosx-island-points-shader-test",
        component: "GalaxyPoints",
        bundleId: "test-bundle",
        props: {
          scene: {
            points: points,
            shaderLib: lib,
          },
        },
        programRef: "/test.json",
        programFormat: "json",
      },
    ],
    bundles: { "test-bundle": { path: "/test.wasm" } },
    runtime: { path: "/test-runtime.wasm" },
  };
}

// Build a manifest with computeParticles having renderVertexWGSLRef fields.
function makeShaderLibManifestEntryForParticleRender(lib, computeParticles) {
  return {
    islands: [
      {
        id: "gosx-island-cprender-shader-test",
        component: "GalaxyParticleRender",
        bundleId: "test-bundle",
        props: {
          scene: {
            computeParticles: computeParticles,
            shaderLib: lib,
          },
        },
        programRef: "/test.json",
        programFormat: "json",
      },
    ],
    bundles: { "test-bundle": { path: "/test.wasm" } },
    runtime: { path: "/test-runtime.wasm" },
  };
}

// -------------------------------------------------------------------------
// Task 4: shaderLib hydrate for instancedMeshes/cullKernelWGSL
// -------------------------------------------------------------------------

// Build a minimal manifest entry with an instancedMeshes scene using a shaderLib.
function makeShaderLibManifestEntryForInstancedMeshes(lib, instancedMeshes) {
  return {
    islands: [
      {
        id: "gosx-island-instanced-cull-test",
        component: "MeteorRing",
        bundleId: "test-bundle",
        props: {
          scene: {
            instancedMeshes: instancedMeshes,
            shaderLib: lib,
          },
        },
        programRef: "/test.json",
        programFormat: "json",
      },
    ],
    bundles: { "test-bundle": { path: "/test.wasm" } },
    runtime: { path: "/test-runtime.wasm" },
  };
}

// -------------------------------------------------------------------------
// Custom post-effect (Selena kind:"post") — WebGPU path (16a)
// -------------------------------------------------------------------------

// makeBundleWithCustomPost returns a minimal Scene3D bundle that carries one
// customPost effect in postEffects. Callers may supply fragmentWGSL/vertexWGSL
// to exercise the authored WGSL path, or omit them for the absent/no-op path.
//
// A minimal compute particle (no authored render WGSL) is included so the
// WebGPU renderer's early-return guard — which short-circuits when there is no
// renderable geometry — does not fire before reaching the post-processing code.
// The particle has no renderVertexWGSL, so it will NOT trigger a second
// createRenderPipelineAsync call; only the custom post effect triggers one.
function makeBundleWithCustomPost(options) {
  const effect = Object.assign({ kind: "customPost", name: "test-lens" }, options || {});
  return {
    camera: { x: 0, y: 0, z: 5, fov: 72, near: 0.05, far: 128 },
    environment: {},
    points: [], instancedMeshes: [], objects: [], meshObjects: [],
    materials: [], labels: [], sprites: [], lights: [],
    postEffects: [effect],
    // Minimal compute particle: bypasses the early-return guard without
    // supplying renderVertexWGSL, so no authored render pipeline is built.
    computeParticles: [{ id: "post-test-cp", count: 4, emitter: { kind: "point" }, material: { color: "#fff" } }],
    positions: new Float32Array(0), colors: new Float32Array(0),
    worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
    worldLineWidths: new Float32Array(0),
    worldMeshPositions: new Float32Array(0), worldMeshColors: new Float32Array(0),
    worldMeshNormals: new Float32Array(0), worldMeshUVs: new Float32Array(0),
    worldMeshTangents: new Float32Array(0),
    vertexCount: 0, worldVertexCount: 0,
  };
}

// -------------------------------------------------------------------------
// Custom post-effect — WebGL2 path (16)
// -------------------------------------------------------------------------

// createWebGLRendererForPost: helper that boots the WebGL2 backend (same
// pattern as other WebGL2 renderer tests in this file).
function createWebGLRendererForPost(options) {
  const opts = options || {};
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  if (opts.fresh) {
    runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
    runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
    runScript(freshFeatureBundleSource("scene3d-compute"), env.context, "bootstrap-feature-scene3d-compute.js");
    runScript(freshFeatureBundleSource("scene3d-webgl"), env.context, "bootstrap-feature-scene3d-webgl.js");
  } else {
    runScript(bootstrapSource, env.context, "bootstrap.js");
  }
  const api = env.context.__gosx_scene3d_api;
  const registry = api.sceneBackendRegistry;
  const backend = registry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
  const canvas = env.document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 180;
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  const warnLog = [];
  const orig = env.context.console.warn;
  env.context.console.warn = function() {
    warnLog.push(Array.from(arguments).join(" "));
    if (orig) orig.apply(env.context.console, arguments);
  };
  return { env, renderer, canvas, warnLog };
}

function makeWebGLBundleWithCustomPost(overrides) {
  const effect = Object.assign({ kind: "customPost", name: "gl-lens" }, overrides || {});
  return {
    bundleVersion: 1,
    camera: { x: 0, y: 0, z: 5, fov: 72, near: 0.05, far: 128 },
    environment: {},
    // A minimal point bypasses the early-return guard in the PBR renderer
    // (which skips rendering when there is no geometry, never reaching the
    // post-processing path). The point has no special material requirements.
    points: [{ id: "gl-post-test-p", count: 1, positions: new Float32Array([0, 0, 0]), color: "#ffffff" }],
    instancedMeshes: [], computeParticles: [],
    objects: [], meshObjects: [], materials: [], labels: [], sprites: [], lights: [],
    postEffects: [effect],
    positions: new Float32Array(0), colors: new Float32Array(0),
    worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
    worldLineWidths: new Float32Array(0),
    worldMeshPositions: new Float32Array(0), worldMeshColors: new Float32Array(0),
    worldMeshNormals: new Float32Array(0), worldMeshUVs: new Float32Array(0),
    worldMeshTangents: new Float32Array(0),
    vertexCount: 0, worldVertexCount: 0,
  };
}

// countDefaultFramebufferDraws counts fullscreen draws issued while the DEFAULT
// framebuffer (id undefined) is bound — i.e. draws that actually land on the
// canvas the user sees.
function countDefaultFramebufferDraws(gl) {
  let bound = "initial";
  let count = 0;
  for (const op of gl.ops) {
    if (op[0] === "bindFramebuffer") {
      bound = op[2];
    } else if ((op[0] === "drawArrays" || op[0] === "drawElements") && bound == null && bound !== "initial") {
      count += 1;
    }
  }
  return count;
}

// -------------------------------------------------------------------------
// GLB-style point layer with named material authored profile (S4 rungs)
// -------------------------------------------------------------------------

// makeSceneApiEnv creates a minimal context with bootstrap loaded and returns
// the __gosx_scene3d_api plus helper for createSceneState.
async function makeSceneApiEnv() {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  return env.context.__gosx_scene3d_api;
}

// =============================================================================
// Slice 2: browser GPU cull — Tasks 1-5
// =============================================================================

// Minimal valid cull kernel WGSL (matches native render/bundle/cull.go contract:
// 4 bindings, atomic drawArgs[1], InstanceRecord layout).
const MINIMAL_CULL_WGSL = [
  "struct CullUniforms { planes: array<vec4<f32>,6>, vertexCount: u32, radius: f32, _pad0: vec2<f32>, };",
  "struct InstanceRecord { model: mat4x4<f32>, pickData: vec4<u32>, };",
  "@group(0) @binding(0) var<uniform> cull: CullUniforms;",
  "@group(0) @binding(1) var<storage, read> input: array<InstanceRecord>;",
  "@group(0) @binding(2) var<storage, read_write> output: array<InstanceRecord>;",
  "@group(0) @binding(3) var<storage, read_write> drawArgs: array<atomic<u32>, 4>;",
  "@compute @workgroup_size(64)",
  "fn main(@builtin(global_invocation_id) gid: vec3<u32>) {",
  "  let i = gid.x;",
  "  if (i >= arrayLength(&input)) { return; }",
  "  let slot = atomicAdd(&drawArgs[1], 1u);",
  "  output[slot] = input[i];",
  "}",
].join("\n");

// Helper: create a compute particle harness (reused from existing helper) but
// return the __gosx_scene3d_api so we can call createSceneInstancedCullSystem.
async function createCullSystemHarness(fakeDevice) {
  const env = createContext({ enableWebGPU: true });
  env.context.GPUBufferUsage = {
    MAP_READ: 0x1, MAP_WRITE: 0x2, COPY_SRC: 0x4, COPY_DST: 0x8,
    INDEX: 0x10, VERTEX: 0x20, UNIFORM: 0x40, STORAGE: 0x80,
    INDIRECT: 0x100, QUERY_RESOLVE: 0x200,
  };
  env.context.GPUTextureUsage = {
    COPY_SRC: 0x1, COPY_DST: 0x2, TEXTURE_BINDING: 0x4,
    STORAGE_BINDING: 0x8, RENDER_ATTACHMENT: 0x10,
  };
  env.context.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
  env.context.createImageBitmap = function(image) {
    return Promise.resolve({ __kind: "imageBitmap", width: image && image.width || 1, height: image && image.height || 1, close() {} });
  };
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  // The particle systems and the GPU instanced cull left the base chunk for
  // bootstrap-feature-scene3d-compute.js. The mount fetches it for a scene
  // that declares particles or instanced meshes, which is exactly what these
  // harnesses build, so load it here the way the browser would.
  runScript(bootstrapFeatureScene3DComputeSource, env.context, "bootstrap-feature-scene3d-compute.js");
  await flushAsyncWork();

  env.context.__gosx_scene3d_webgpu_probe = function() {
    return { adapter: { __kind: "adapter" }, device: fakeDevice, ready: true };
  };
  runScript(bootstrapFeatureScene3DWebGPUSource, env.context, "bootstrap-feature-scene3d-webgpu.js");

  const api = env.context.__gosx_scene3d_api;
  return { env, api };
}

// -------------------------------------------------------------------------
// Render bundles, executed end to end against the recording fake device.
//
// The unit tests for the recorder, the token stream and the invalidation live in
// 16a-scene-webgpu-bundle.test.mjs. These tests drive the REAL renderer through
// real frames and assert on what the device saw: one bundle encode, then
// replays, then a fresh encode when the draw set moves.
// -------------------------------------------------------------------------

// bundleMeshScene builds a mesh-only scene. Mesh-only matters: water, points,
// labels, screen lines, surfaces and world lines all keep the direct path.
function bundleMeshScene(api, objects) {
  const state = api.createSceneState({
    scene: {
      materials: [{ id: "m", color: "#8de1ff", roughness: 0.4, metalness: 0.1 }],
      objects: objects,
    },
  }, { tier: "full" });
  const withMaterials = api.sceneStateObjectsWithMaterials(state);
  return api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 0, z: 4, fov: 60, near: 0.05, far: 128 },
    withMaterials, [], [], [], [], {}, 0, [], [], [], [], [], 0, false,
  );
}

function bundleBoxes(count, scale) {
  const out = [];
  for (let i = 0; i < count; i += 1) {
    out.push({ id: "box-" + i, kind: "box", width: scale, height: scale, depth: scale, x: i * 0.5, y: 0, z: 0, material: "m", wireframe: false });
  }
  return out;
}

// bundleInstancedMesh builds one instanced mesh with `count` identity-scaled
// transforms spread along a grid, and no per-instance colours.
//
// The transforms are a PLAIN Array, not a Float32Array. instancedMeshTransformData
// accepts either, but it tests `instanceof Float32Array`, and a typed array built
// in this host realm is not an instance of the sandbox realm's constructor.
// Array.isArray works across realms, so a plain Array is the correct shape for a
// vm-hosted harness. A real page hands over a Float32Array from its own realm and
// takes the other branch.
function bundleInstancedMesh(count, scale) {
  const transforms = new Array(count * 16).fill(0);
  for (let i = 0; i < count; i += 1) {
    const b = i * 16;
    transforms[b + 0] = scale;
    transforms[b + 5] = scale;
    transforms[b + 10] = scale;
    transforms[b + 15] = 1;
    transforms[b + 12] = (i % 32) * 0.5;
    transforms[b + 13] = Math.floor(i / 32) * 0.5;
  }
  return {
    id: "ring",
    kind: "box",
    width: 1,
    height: 1,
    depth: 1,
    count,
    instanceCount: count,
    transforms,
    materialIndex: 0,
    castShadow: false,
    receiveShadow: false,
  };
}

// -------------------------------------------------------------------------
// Task 4: indirect-draw branch in drawInstancedMeshes
// -------------------------------------------------------------------------

// Minimal instanced mesh bundle for render() testing.
function makeInstancedBundle(meshOverrides) {
  const identity16 = new Float32Array([1,0,0,0, 0,1,0,0, 0,0,1,0, 0,0,0,1]);
  return {
    camera: { x: 0, y: 0, z: 5, fov: 72, near: 0.05, far: 128 },
    environment: {},
    instancedMeshes: [Object.assign({
      id: "test-meteor",
      kind: "box",
      instanceCount: 1,
      transforms: identity16,
    }, meshOverrides)],
    points: [], objects: [], meshObjects: [],
    materials: [], labels: [], sprites: [], lights: [], postEffects: [],
    computeParticles: [],
    positions: new Float32Array(0), colors: new Float32Array(0),
    worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
    worldLineWidths: new Float32Array(0),
    worldMeshPositions: new Float32Array(0), worldMeshColors: new Float32Array(0),
    worldMeshNormals: new Float32Array(0), worldMeshUVs: new Float32Array(0),
    worldMeshTangents: new Float32Array(0),
    vertexCount: 0, worldVertexCount: 0,
  };
}

// =========================================================================
// Slice 3: WebGL2 CPU-cull fallback tests
// =========================================================================

// Helper: extract and compile extractFrustumPlanesJS + instancePassesCullTest
// from 11-scene-math.js for headless unit testing.
function loadCullFunctions() {
  const mathSrc = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "11-scene-math.js"), "utf8");

  // Extract extractFrustumPlanesJS (indented 2 spaces inside the IIFE).
  const extractMatch = mathSrc.match(
    /function extractFrustumPlanesJS\(vp\)\s*\{([\s\S]*?)\n  \}/);
  assert.ok(extractMatch, "extractFrustumPlanesJS must be extractable from 11-scene-math.js");
  const extractFn = new Function(
    "return (function extractFrustumPlanesJS(vp) {" + extractMatch[1] + "\n  })")();

  // Extract instancePassesCullTest (indented 2 spaces).
  const passMatch = mathSrc.match(
    /function instancePassesCullTest\(transforms, instanceIndex, planes, radius\)\s*\{([\s\S]*?)\n  \}/);
  assert.ok(passMatch, "instancePassesCullTest must be extractable from 11-scene-math.js");
  const passFn = new Function(
    "return (function instancePassesCullTest(transforms, instanceIndex, planes, radius) {" +
    passMatch[1] + "\n  })")();

  return { extractFrustumPlanesJS: extractFn, instancePassesCullTest: passFn };
}

// P4-M3: route glTF MODEL animation through the WASM motion mixer
// (behind window.__gosx_motion_wasm; JS mixer stays the default).
// -------------------------------------------------------------------------

// Installs a deterministic WASM motion-mixer stub onto the sandbox context and
// records every interaction so a test can assert the bridge wiring. The update
// stub writes one packed rotation TRS for `rotationNode` so the decode →
// animatedTransforms → skinning path can be verified through the renderer.
function installWasmMotionMixerStub(context, rotationNode, quat) {
  const calls = { create: 0, addClip: [], play: [], stop: [], update: 0, isPlaying: 0, destroy: [] };
  let nextHandle = 1;
  context.__gosx_motion_wasm = true;
  context.__gosx_motion_mixer_create = () => {
    calls.create += 1;
    return nextHandle++;
  };
  context.__gosx_motion_mixer_add_clip = (handle, name, clipJSON) => {
    calls.addClip.push({ handle, name, clipJSON });
    return true;
  };
  context.__gosx_motion_mixer_play = (handle, name, fadeIn, loop, speed, weight) => {
    calls.play.push({ handle, name, fadeIn, loop, speed, weight });
  };
  context.__gosx_motion_mixer_stop = (handle, name, fadeOut) => {
    calls.stop.push({ handle, name, fadeOut });
  };
  context.__gosx_motion_mixer_is_playing = () => {
    calls.isPlaying += 1;
    return true;
  };
  context.__gosx_motion_mixer_update = (handle, dt, reduced, outU8) => {
    calls.update += 1;
    // packed: [targetID, propID=1(rotation), arity=4, qx, qy, qz, qw]
    const f = new Float64Array(outU8.buffer, outU8.byteOffset, Math.floor(outU8.byteLength / 8));
    f[0] = rotationNode;
    f[1] = 1;
    f[2] = 4;
    f[3] = quat[0];
    f[4] = quat[1];
    f[5] = quat[2];
    f[6] = quat[3];
    return 7;
  };
  context.__gosx_motion_mixer_destroy = (handle) => {
    calls.destroy.push(handle);
  };
  return calls;
}

module.exports = {
  bootstrapSource,
  bootstrapLiteSource,
  bootstrapRuntimeSource,
  bootstrapFeatureTextLayoutSource,
  bootstrapFeatureIslandsSource,
  bootstrapFeatureEnginesSource,
  bootstrapFeatureHubsSource,
  bootstrapFeatureScene3DSource,
  bootstrapFeatureScene3DCommandSource,
  bootstrapFeatureScene3DComputeSource,
  bootstrapFeatureScene3DDecompressSource,
  bootstrapFeatureScene3DWebGLSource,
  bootstrapFeatureScene3DWebGPUSource,
  bootstrapScene3DWebGPUSourceFile,
  bootstrapScene3DInputSourceFile,
  bootstrapScene3DMountSourceFile,
  bootstrapScene3DDOMRegionsSourceFile,
  patchSource,
  stripeBridgeSource,
  navigationSource,
  bootstrapSourceMapSource,
  ELEMENT_NODE,
  TEXT_NODE,
  DOCUMENT_FRAGMENT_NODE,
  activeTestContexts,
  scene3DCommandFetchRoutes,
  disposeRuntimeTestContext,
  FakeTextNode,
  FakeDocumentFragment,
  fakeHTMLText,
  FakeCanvasContext2D,
  FakeWebGLContext,
  FakeWebGPUCanvasContext,
  fakeElementMatchesSelector,
  fakeElementMatchesSelectorGroup,
  fakeElementQuerySelectorAll,
  FakeElement,
  adoptNode,
  FakeDocument,
  FakeFontSet,
  FakeResizeObserver,
  FakeIntersectionObserver,
  FakeMutationObserver,
  FakeListenerTarget,
  FakeMediaQueryList,
  FakeResponse,
  buildMinimalGLBBytes,
  buildPointLineGLBBytes,
  buildSkinnedGLBBytes,
  FakeFormData,
  createConsoleSpy,
  numberOr,
  createComputedStyleSnapshot,
  createContext,
  installManualRAF,
  flushSceneInitialFrameBoundary,
  installManualTimers,
  installManualClock,
  runScript,
  flushAsyncWork,
  sharedSignalValue,
  appendManagedHead,
  theatreSyncHeartbeat,
  theatrePing,
  buildNavigatedDocument,
  mountMotionSeamScene,
  motionMeshExtents,
  mountMaterialMotionScene,
  SELENA_SKINNABLE_VERTEX_GLSL_FIXTURE,
  SELENA_SKINNABLE_FRAGMENT_GLSL_FIXTURE,
  SELENA_SKINNABLE_SHADER_LAYOUT_FIXTURE,
  loadSceneWaterClockAPI,
  scene3dWebGLSplitManifest,
  CUSTOM_POST_TIME_LAYOUT_FIXTURE,
  sceneCoreSourceRange,
  loadSceneAdaptiveQualityAPI,
  loadSceneViewportAPI,
  resolveSceneViewportForTest,
  createAdaptiveQualityHarness,
  createQualityLadderHarness,
  THREE_RUNG_LADDER,
  createQualityLadderRAFHarness,
  RAW_TO_GLOW_LADDER,
  telemetryPostBodies,
  telemetryEvents,
  VIDEO_PRIMITIVES_FAKE_HLS_SCRIPT,
  loadVideoSyncJSEngineFactory,
  loadCanvasPainter,
  makeFakeContext2D,
  callsOfType,
  nodeFillRects,
  loadScene3DApiContext,
  assertMat4Approx,
  readBoardFixture,
  goBoardBundleRectsJSON,
  goBoardBundleMixedJSON,
  goBoardBundleMixedWithHTMLJSON,
  parseWGSLBindingKinds,
  gpuBindGroupLayoutEntryKind,
  makeFakeGPUDevice,
  bootstrapChunkManifest,
  bootstrapChunkSources,
  readBootstrapSrc,
  readSceneMountSrc,
  readWebGPUBackendSrc,
  readBootstrapTailSrc,
  freshFeatureBundleSource,
  createBoardWebGPUHarness,
  mainRenderPasses,
  waterPoolSelenaFixture,
  waterSelenaFixture,
  waterSurfaceSelenaFixture,
  waterSurfaceBelowSelenaFixture,
  waterCausticsSelenaFixture,
  waterObjectMaterialSelenaFixture,
  waterDuckMaterialSelenaFixture,
  waterObjectShadowSelenaFixture,
  waterCompoundShadowSelenaFixture,
  waterObjectMeshShadowSelenaFixture,
  waterSeedSelenaFixture,
  waterDropSelenaFixture,
  waterDisplacementSelenaFixture,
  waterSimulationSelenaFixture,
  waterNormalSelenaFixture,
  waterSelenaFrameEntry,
  waterSelenaFieldFloats,
  waterSelenaLastUniformWrite,
  waterPerfShapeEntry,
  waterPerfShapeScene,
  deviceCallSnapshot,
  deviceCallDelta,
  renderWaterPerfShapeFrames,
  waterComputeKernelPipeline,
  assertWaterComputeKernelBindings,
  loadBoardLabels,
  makeBoardHost,
  layer_childCount,
  createCanvasBoardRoutingHarness,
  boardBundleManyObjectsOneMaterial,
  makeFakeGPUDeviceForCompute,
  createComputeParticleHarness,
  makeComputeParticleBundle,
  makeFakeGPUDeviceForPoints,
  makePointsBundle,
  makeComputeParticleBundleWithRender,
  makeShaderLibManifestEntry,
  makeShaderLibManifestEntryForPoints,
  makeShaderLibManifestEntryForParticleRender,
  makeShaderLibManifestEntryForInstancedMeshes,
  makeBundleWithCustomPost,
  createWebGLRendererForPost,
  makeWebGLBundleWithCustomPost,
  countDefaultFramebufferDraws,
  makeSceneApiEnv,
  MINIMAL_CULL_WGSL,
  createCullSystemHarness,
  bundleMeshScene,
  bundleBoxes,
  bundleInstancedMesh,
  makeInstancedBundle,
  loadCullFunctions,
  installWasmMotionMixerStub,
};
