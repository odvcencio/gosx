const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const bootstrapSource = fs.readFileSync(path.join(__dirname, "bootstrap.js"), "utf8");
const bootstrapLiteSource = fs.readFileSync(path.join(__dirname, "bootstrap-lite.js"), "utf8");
const bootstrapRuntimeSource = fs.readFileSync(path.join(__dirname, "bootstrap-runtime.js"), "utf8");
const bootstrapFeatureIslandsSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-islands.js"), "utf8");
const bootstrapFeatureEnginesSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-engines.js"), "utf8");
const bootstrapFeatureHubsSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-hubs.js"), "utf8");
const bootstrapFeatureScene3DSource = fs.readFileSync(path.join(__dirname, "bootstrap-feature-scene3d.js"), "utf8");
const patchSource = fs.readFileSync(path.join(__dirname, "patch.js"), "utf8");
const navigationSource = fs.readFileSync(path.join(__dirname, "..", "..", "server", "navigation_runtime.js"), "utf8");

const ELEMENT_NODE = 1;
const TEXT_NODE = 3;
const DOCUMENT_FRAGMENT_NODE = 11;

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
    this.bufferUploads = new Map();
    this.textureUploads = new Map();
    this._nextBufferID = 1;
    this._nextTextureID = 1;
    this._boundArrayBuffer = null;
    this._boundTexture = null;
    this._vendor = typeof options.vendor === "string" ? options.vendor : "FakeGPU Inc.";
    this._renderer = typeof options.renderer === "string" ? options.renderer : "FakeGPU Renderer";
    this._disableDebugRendererInfo = options.debugRendererInfo === false;
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

  getShaderParameter(_shader, param) {
    return param === this.COMPILE_STATUS;
  }

  createProgram() {
    const program = { attached: [] };
    this.ops.push(["createProgram"]);
    return program;
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
    if (name === "a_joints") return 7;
    if (name === "a_weights") return 8;
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

  useProgram(_program) {
    this.ops.push(["useProgram"]);
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
    }
    this.ops.push(["bufferData", target, bufferID, data.length, usage]);
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

  vertexAttrib4f(location, x, y, z, w) {
    this.ops.push(["vertexAttrib4f", location, x, y, z, w]);
  }

  drawArrays(mode, first, count) {
    this.ops.push(["drawArrays", mode, first, count]);
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
    if (name === "WEBGL_debug_renderer_info" && !this._disableDebugRendererInfo) {
      return {
        UNMASKED_VENDOR_WEBGL: 0x9245,
        UNMASKED_RENDERER_WEBGL: 0x9246,
      };
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
    if (param === 0x9245 || param === this.VENDOR) {
      return this._vendor;
    }
    if (param === 0x9246 || param === this.RENDERER) {
      return this._renderer;
    }
    return null;
  }
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
  }

  clone() {
    return new FakeResponse({
      ok: this.ok,
      status: this.status,
      text: this._text,
      bytes: this._bytes.slice(),
      url: this.url,
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
  const consoleSpy = createConsoleSpy();
  const hydrateCalls = [];
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
  const visualViewport = options.visualViewport === false ? null : new FakeListenerTarget();
  if (visualViewport) {
    visualViewport.offsetLeft = numberOr(options.visualViewportOffsetLeft, 0);
    visualViewport.offsetTop = numberOr(options.visualViewportOffsetTop, 0);
    visualViewport.width = numberOr(options.visualViewportWidth, 0);
    visualViewport.height = numberOr(options.visualViewportHeight, 0);
  }

  const routes = new Map();
  for (const [url, response] of Object.entries(options.fetchRoutes || {})) {
    routes.set(url, response);
  }

  const context = {
    Array,
    ArrayBuffer,
    Intl,
    JSON,
    Map,
    Promise,
    Set,
    Uint8Array,
    Uint8ClampedArray,
    clearTimeout,
    clearInterval,
    console: consoleSpy.console,
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
      return new FakeResponse(routes.get(url));
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
    const manifestScript = document.createElement("script");
    manifestScript.id = "gosx-manifest";
    manifestScript.textContent = JSON.stringify(options.manifest);
    document.body.appendChild(manifestScript);
  }

  for (const element of options.elements || []) {
    document.body.appendChild(element);
  }

  return {
    actionCalls,
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

function installManualTimers(context) {
  let nextHandle = 1;
  const timers = new Map();
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
  return {
    count() {
      return timers.size;
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

function buildNavigatedDocument(options) {
  const doc = new FakeDocument();
  doc.title = options.title || "";
  appendManagedHead(doc, options.headNodes || []);
  for (const node of options.bodyNodes || []) {
    doc.body.appendChild(node);
  }
  return doc;
}

test("bootstrap hydrates, delegates click events, and disposes islands", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  const button = new FakeElement("button", null);

  wrapper.id = "gosx-island-1";
  button.setAttribute("data-gosx-on-click", "increment");
  componentRoot.appendChild(button);
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-1",
          component: "Counter",
          props: { initial: 2 },
          programRef: "/counter.json",
        },
      ],
    },
    onAction: () => 1,
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  assert.deepEqual(env.hydrateCalls[0].slice(0, 3), [
    "gosx-island-1",
    "Counter",
    '{"initial":2}',
  ]);
  assert.equal(typeof env.hydrateCalls[0][3], "string");
  assert.equal(env.hydrateCalls[0][4], "json");
  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.islands.size, 1);
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:ready");
  assert.equal(env.document.dispatchedEvents.some((event) => event.type === "gosx:ready"), true);

  const clickEntries = wrapper.listeners.get("click") || [];
  assert.equal(clickEntries.length, 1);
  clickEntries[0].listener({
    type: "click",
    target: button,
    preventDefault() {},
  });

  assert.deepEqual(env.actionCalls, [
    ["gosx-island-1", "increment", '{"type":"click"}'],
  ]);

  env.context.__gosx_dispose_island("gosx-island-1");
  assert.equal(env.context.__gosx.islands.size, 0);
  assert.equal(wrapper.listenerCount("click"), 0);
  assert.deepEqual(env.disposeCalls, [["gosx-island-1"]]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap records island hydration failures and keeps the server fallback active", async () => {
  const wrapper = new FakeElement("div", null);
  wrapper.id = "gosx-island-1";

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-1",
          component: "Counter",
          props: { initial: 2 },
          programRef: "/counter.json",
        },
      ],
    },
    onHydrate: () => "hydrate failed",
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const issues = env.context.__gosx.listIssues();
  assert.equal(issues.length, 1);
  assert.equal(issues[0].scope, "island");
  assert.equal(issues[0].type, "hydrate");
  assert.equal(issues[0].component, "Counter");
  assert.equal(issues[0].elementID, "gosx-island-1");
  assert.equal(wrapper.getAttribute("data-gosx-runtime-state"), "error");
  assert.equal(wrapper.getAttribute("data-gosx-runtime-issue"), "hydrate");
  assert.equal(wrapper.getAttribute("data-gosx-fallback-active"), "server");
  assert.equal(env.context.__gosx.islands.size, 0);
  assert.equal(env.document.dispatchedEvents.some((event) => event.type === "gosx:error"), true);
});

test("bootstrap forwards click target value to delegated island handlers", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  const button = new FakeElement("button", null);

  wrapper.id = "gosx-island-value";
  button.setAttribute("data-gosx-on-click", "selectFile");
  button.value = "schema.arb";
  componentRoot.appendChild(button);
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/selector.json": { text: '{"name":"Selector"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-value",
          component: "Selector",
          props: {},
          programRef: "/selector.json",
        },
      ],
    },
    onAction: () => 1,
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const clickEntries = wrapper.listeners.get("click") || [];
  assert.equal(clickEntries.length, 1);
  clickEntries[0].listener({
    type: "click",
    target: button,
    preventDefault() {},
  });

  assert.deepEqual(env.actionCalls, [
    ["gosx-island-value", "selectFile", '{"type":"click","value":"schema.arb"}'],
  ]);
});

test("bootstrap exposes a browser text measurement helper", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const raw = env.context.__gosx_measure_text_batch("600 16px serif", JSON.stringify(["hi", "there"]));
  assert.deepEqual(JSON.parse(raw), [16, 40]);
});

test("bootstrap text measurement helper handles invalid payloads defensively", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const raw = env.context.__gosx_measure_text_batch("600 16px serif", "{");
  assert.deepEqual(JSON.parse(raw), []);
  assert.equal(env.consoleLogs.error.length > 0, true);
});

test("bootstrap exposes a browser text layout helper without wasm runtime", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["hello world", "from gosx"]);
  assert.equal(layout.lineCount, 2);
  assert.equal(layout.height, 40);
  assert.equal(layout.maxLineWidth, 88);
  assert.equal(layout.byteLen, 21);
  assert.equal(layout.runeCount, 21);
  assert.equal(layout.lines[0].byteStart, 0);
  assert.equal(layout.lines[0].byteEnd, 12);
});

test("bootstrap exposes a text layout metrics helper without wasm runtime", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const metrics = env.context.__gosx_text_layout_metrics("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.equal(metrics.lineCount, 2);
  assert.equal(metrics.height, 40);
  assert.equal(metrics.maxLineWidth, 88);
  assert.equal(metrics.byteLen, 21);
  assert.equal(metrics.runeCount, 21);
});

test("bootstrap exposes a text layout ranges helper without wasm runtime", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const result = env.context.__gosx_text_layout_ranges("ab\u00adcd", "600 16px serif", 80, "normal", 20);
  assert.equal(result.lineCount, 1);
  assert.equal(result.lines.length, 1);
  assert.equal(result.lines[0].softBreak, false);
  assert.equal(result.lines[0].hardBreak, false);
});

test("bootstrap browser text layout helper preserves pre-wrap hard breaks", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hi\n", "600 16px serif", 200, "pre-wrap", 18);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["hi", ""]);
  assert.equal(layout.lineCount, 2);
  assert.equal(layout.height, 36);
  assert.equal(layout.lines[0].hardBreak, true);
  assert.equal(layout.lines[1].byteStart, 3);
  assert.equal(layout.lines[1].byteEnd, 3);
});

test("bootstrap browser text layout keeps normal trailing spaces out of max width", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hello ", "600 16px serif", 200, "normal", 18);
  assert.equal(layout.maxLineWidth, 40);
  assert.equal(layout.lines[0].text, "hello");
});

test("bootstrap browser text layout breaks long tokens at grapheme boundaries", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("abcdef", "600 16px serif", 4, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["abcd", "ef"]);
  assert.equal(layout.maxLineWidth, 4);
});

test("bootstrap browser text layout uses browser-style tab stops", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("a\tb", "600 16px serif", 99, "pre-wrap", 12);
  assert.equal(layout.lineCount, 1);
  assert.equal(layout.maxLineWidth, 9);
  assert.equal(layout.lines[0].text, "a\tb");
});

test("bootstrap browser text layout breaks at soft hyphens", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("ab\u00adcd", "600 16px serif", 3, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["ab-", "cd"]);
  assert.equal(layout.lines[0].width, 3);
});

test("bootstrap browser text layout breaks at zero-width spaces", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("foo\u200bbar", "600 16px serif", 3, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["foo", "bar"]);
});

test("bootstrap browser text layout prefers word boundaries inside punctuation-heavy runs", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hello,world", "600 16px serif", 7, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["hello,", "world"]);
});

test("bootstrap browser text layout uses Intl word boundaries for Thai runs", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("สวัสดีครับโลก", "600 16px serif", 6, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["สวัสดี", "ครับ", "โลก"]);
});

test("bootstrap browser text layout keeps CJK closing punctuation off line starts", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("あ。い", "600 16px serif", 1, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["あ。", "い"]);
  assert.equal(layout.lines[0].width, 2);
});

test("bootstrap browser text layout keeps opening punctuation with following glyphs", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("(a", "600 16px serif", 1, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["(a"]);
  assert.equal(layout.lines[0].width, 2);
});

test("bootstrap browser text layout keeps emoji grapheme clusters intact", () => {
  const graphemeSegmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
  const env = createContext({
    measureText(text) {
      let count = 0;
      for (const _entry of graphemeSegmenter.segment(String(text))) {
        count += 1;
      }
      return count;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("👨‍👩‍👧‍👦a", "600 16px serif", 1, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["👨‍👩‍👧‍👦", "a"]);
  assert.equal(layout.lineCount, 2);
});

test("bootstrap browser text layout supports max-lines ellipsis clamp", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 11, "normal", 12, {
    maxLines: 1,
    overflow: "ellipsis",
  });
  assert.equal(layout.lineCount, 1);
  assert.equal(layout.truncated, true);
  assert.equal(layout.lines[0].truncated, true);
  assert.equal(layout.lines[0].ellipsis, true);
  assert.equal(layout.lines[0].text, "hello worl…");
});

test("bootstrap browser text layout invalidates cached widths after font loading events", () => {
  let scale = 1;
  const fonts = new FakeFontSet();
  const env = createContext({
    fonts,
    measureText(text) {
      return String(text).length * 8 * scale;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const before = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.deepEqual(Array.from(before.lines, (line) => line.text), ["hello world", "from gosx"]);

  scale = 2;
  fonts.dispatch("loadingdone");

  const after = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.deepEqual(Array.from(after.lines, (line) => line.text), ["hello", "world", "from", "gosx"]);
  assert.equal(after.lineCount, 4);
});

test("bootstrap mounts declarative text layout blocks as managed runtime state", async () => {
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-ready"), "true");
  assert.equal(block.getAttribute("data-gosx-text-layout-role"), "block");
  assert.equal(block.getAttribute("data-gosx-text-layout-surface"), "dom");
  assert.equal(block.getAttribute("data-gosx-text-layout-state"), "ready");
  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "2");
  assert.equal(block.getAttribute("data-gosx-text-layout-height"), "40");
  assert.equal(block.getAttribute("data-gosx-text-layout-byte-length"), "21");
  assert.equal(block.style["--gosx-text-layout-height"], "40px");
  assert.equal(env.context.__gosx.textLayouts.size, 1);

  const result = env.context.__gosx.textLayout.read(block);
  assert.equal(result.lineCount, 2);
  assert.equal(result.maxLineWidth, 88);
  assert.equal(env.document.dispatchedEvents.some((event) => event.type === "gosx:textlayout"), true);
});

test("bootstrap lite mounts managed text layout without a manifest", async () => {
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(block.getAttribute("data-gosx-text-layout-ready"), "true");
  assert.equal(env.context.__gosx.textLayouts.size, 1);
});

test("bootstrap lite mounts managed motion blocks and plays load presets", async () => {
  const block = new FakeElement("section", null);
  block.setAttribute("data-gosx-motion", "");
  block.setAttribute("data-gosx-motion-preset", "slide-up");
  block.setAttribute("data-gosx-motion-duration", "360");
  block.setAttribute("data-gosx-motion-delay", "40");
  block.setAttribute("data-gosx-motion-distance", "24");
  block.setAttribute("data-gosx-motion-easing", "ease-out");

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(block.getAttribute("data-gosx-motion-state"), "finished");
  assert.equal(block.animateCalls.length, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(block.animateCalls[0].keyframes)), [
    { opacity: 0, transform: "translate3d(0, 24px, 0)" },
    { opacity: 1, transform: "translate3d(0, 0, 0)" },
  ]);
  assert.deepEqual(JSON.parse(JSON.stringify(block.animateCalls[0].options)), {
    duration: 360,
    delay: 40,
    easing: "ease-out",
    fill: "both",
  });
});

test("bootstrap lite respects reduced motion on managed motion blocks", async () => {
  const block = new FakeElement("div", null);
  block.setAttribute("data-gosx-motion", "");

  const env = createContext({
    elements: [block],
    prefersReducedMotion: true,
  });

  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-motion-state"), "reduced");
  assert.equal(block.animateCalls.length, 0);
});

test("bootstrap lite defers managed motion view triggers until intersection", async () => {
  const block = new FakeElement("div", null);
  block.setAttribute("data-gosx-motion", "");
  block.setAttribute("data-gosx-motion-trigger", "view");
  block.setAttribute("data-gosx-motion-preset", "zoom-in");

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-motion-state"), "idle");
  assert.equal(block.animateCalls.length, 0);
  assert.equal(env.intersectionObservers.length, 1);

  env.intersectionObservers[0].trigger([
    { target: block, isIntersecting: true, intersectionRatio: 0.5 },
  ]);
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-motion-state"), "finished");
  assert.equal(block.animateCalls.length, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(block.animateCalls[0].keyframes)), [
    { opacity: 0, transform: "scale(0.91)" },
    { opacity: 1, transform: "scale(1)" },
  ]);
});

test("bootstrap mounts declarative text layout clamp options on managed blocks", async () => {
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.setAttribute("data-gosx-text-layout-white-space", "pre-wrap");
  block.setAttribute("data-gosx-text-layout-align", "center");
  block.setAttribute("data-gosx-text-layout-max-lines", "1");
  block.setAttribute("data-gosx-text-layout-overflow", "ellipsis");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-max-lines"), "1");
  assert.equal(block.getAttribute("data-gosx-text-layout-overflow"), "ellipsis");
  assert.equal(block.getAttribute("data-gosx-text-layout-state"), "truncated");
  assert.equal(block.getAttribute("data-gosx-text-layout-truncated"), "true");
  assert.equal(block.style["--gosx-text-layout-white-space-mode"], "pre-wrap");
  assert.equal(block.style["--gosx-text-layout-align"], "center");
  assert.equal(block.style["--gosx-text-layout-max-lines"], "1");
  assert.equal(env.context.__gosx.textLayout.read(block).truncated, true);
});

test("bootstrap installs a stronger CSS contract for managed text layout blocks", async () => {
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.setAttribute("data-gosx-text-layout-max-width", "88");
  block.setAttribute("align", "right");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const styleTag = env.document.head.children[0];
  assert.equal(styleTag.tagName, "STYLE");
  assert.ok(styleTag.textContent.includes('white-space: var(--gosx-text-layout-white-space-mode, normal);'));
  assert.ok(styleTag.textContent.includes('[data-gosx-text-layout-role="block"][data-gosx-text-layout-max-width]'));
  assert.equal(block.style["--gosx-text-layout-align"], "right");
  assert.equal(block.style["--gosx-text-layout-max-width"], "88px");
  assert.equal(block.getAttribute("data-gosx-text-layout-state"), "ready");
});

test("bootstrap derives managed text layout config from computed styles and CSS vars", async () => {
  const block = new FakeElement("div", null);
  block.setAttribute("data-gosx-text-layout", "");
  block.textContent = "hello world from gosx";
  block.computedStyle = {
    font: "600 16px serif",
    textAlign: "center",
    whiteSpace: "pre-wrap",
    lineHeight: "22px",
    maxWidth: "88px",
    textOverflow: "ellipsis",
    getPropertyValue(name) {
      switch (name) {
        case "--gosx-text-layout-max-lines":
          return "1";
        case "--gosx-text-layout-overflow":
          return "ellipsis";
        default:
          return this[name] || "";
      }
    },
  };

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-align"), "center");
  assert.equal(block.getAttribute("data-gosx-text-layout-white-space"), "pre-wrap");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-width"), "88");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-lines"), "1");
  assert.equal(block.getAttribute("data-gosx-text-layout-overflow"), "ellipsis");
  assert.equal(block.getAttribute("data-gosx-text-layout-state"), "truncated");
  assert.equal(block.style["--gosx-text-layout-line-height"], "22px");
  assert.equal(env.context.__gosx.textLayout.read(block).truncated, true);
});

test("bootstrap derives managed text layout from logical inline size and locale-aware presentation", async () => {
  const block = new FakeElement("div", null);
  block.width = 80;
  block.height = 160;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("lang", "th");
  block.textContent = "hello gosx app";
  block.computedStyle = {
    font: "600 16px serif",
    textAlign: "start",
    whiteSpace: "normal",
    lineHeight: "20px",
    writingMode: "vertical-rl",
    maxInlineSize: "none",
    getPropertyValue(name) {
      return this[name] || "";
    },
  };

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-locale"), "th");
  assert.equal(block.getAttribute("data-gosx-text-layout-writing-mode"), "vertical-rl");
  assert.equal(block.getAttribute("data-gosx-text-layout-inline-size"), "160");
  assert.equal(block.getAttribute("data-gosx-text-layout-block-size"), "80");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-width"), "160");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-inline-size"), "160");
  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "1");
});

test("bootstrap refreshes managed text layout blocks after computed style changes", async () => {
  const block = new FakeElement("div", null);
  block.setAttribute("data-gosx-text-layout", "");
  block.textContent = "hello world from gosx";
  block.computedStyle = {
    font: "600 16px serif",
    textAlign: "left",
    whiteSpace: "normal",
    lineHeight: "20px",
    maxWidth: "88px",
    getPropertyValue(name) {
      return this[name] || "";
    },
  };

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "2");
  assert.equal(block.getAttribute("data-gosx-text-layout-align"), "left");

  block.computedStyle.textAlign = "right";
  block.computedStyle.maxWidth = "200px";
  env.context.__gosx.textLayout.refresh(block);

  assert.equal(block.getAttribute("data-gosx-text-layout-align"), "right");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-width"), "200");
  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "1");
});

test("bootstrap refreshes managed text layout after inherited locale and direction changes", async () => {
  const container = new FakeElement("section", null);
  container.setAttribute("lang", "en");
  container.setAttribute("dir", "ltr");
  const block = new FakeElement("div", null);
  block.width = 120;
  block.height = 40;
  block.setAttribute("data-gosx-text-layout", "");
  block.textContent = "hello world";
  block.computedStyle = {
    font: "600 16px serif",
    lineHeight: "20px",
    textAlign: "start",
    whiteSpace: "normal",
    getPropertyValue(name) {
      return this[name] || "";
    },
  };
  container.appendChild(block);

  const env = createContext({
    elements: [container],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-locale"), "en");
  assert.equal(block.getAttribute("data-gosx-text-layout-direction"), "ltr");

  container.setAttribute("lang", "th");
  container.setAttribute("dir", "rtl");
  const presentationObserver = env.mutationObservers.find((observer) => observer.targets.has(env.document.documentElement));
  assert.ok(presentationObserver);
  presentationObserver.trigger([
    { target: container, type: "attributes", attributeName: "lang" },
    { target: container, type: "attributes", attributeName: "dir" },
  ]);
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-locale"), "th");
  assert.equal(block.getAttribute("data-gosx-text-layout-direction"), "rtl");
});

test("bootstrap shares presentation observers across managed text layout blocks and tears them down", async () => {
  const container = new FakeElement("section", null);
  const first = new FakeElement("div", null);
  const second = new FakeElement("div", null);
  for (const block of [first, second]) {
    block.width = 88;
    block.setAttribute("data-gosx-text-layout", "");
    block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
    block.setAttribute("data-gosx-text-layout-line-height", "20");
    block.textContent = "hello world from gosx";
    container.appendChild(block);
  }

  const env = createContext({
    elements: [container],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const presentationObserver = env.mutationObservers.filter((observer) => observer.targets.has(env.document.documentElement));
  assert.equal(presentationObserver.length, 1);
  assert.equal(env.resizeObservers.length >= 1, true);
  assert.equal(env.resizeObservers[0].targets.has(first), true);
  assert.equal(env.resizeObservers[0].targets.has(second), true);

  env.context.__gosx.textLayout.dispose(first);

  assert.equal(env.resizeObservers[0].targets.has(first), false);
  assert.equal(env.resizeObservers[0].targets.has(second), true);

  env.context.__gosx.textLayout.dispose(second);

  assert.equal(presentationObserver[0].targets.size, 0);
  assert.equal(env.resizeObservers[0].targets.size, 0);
});

test("bootstrap coalesces presentation-driven text layout refreshes into one frame", async () => {
  const container = new FakeElement("section", null);
  container.setAttribute("lang", "en");
  const block = new FakeElement("div", null);
  block.width = 120;
  block.height = 40;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.textContent = "hello world from gosx";
  container.appendChild(block);

  const env = createContext({
    elements: [container],
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let updates = 0;
  block.addEventListener("gosx:textlayout", () => {
    updates += 1;
  });

  container.setAttribute("lang", "th");
  container.setAttribute("dir", "rtl");
  const presentationObserver = env.mutationObservers.find((observer) => observer.targets.has(env.document.documentElement));
  assert.ok(presentationObserver);

  presentationObserver.trigger([
    { target: container, type: "attributes", attributeName: "lang" },
  ]);
  presentationObserver.trigger([
    { target: container, type: "attributes", attributeName: "dir" },
  ]);

  assert.equal(raf.count(), 1);
  assert.equal(updates, 0);

  raf.flush();
  await flushAsyncWork();

  assert.equal(updates, 1);
  assert.equal(block.getAttribute("data-gosx-text-layout-locale"), "th");
  assert.equal(block.getAttribute("data-gosx-text-layout-direction"), "rtl");
});

test("bootstrap exposes unified environment, document, and presentation state", async () => {
  const block = new FakeElement("div", null);
  block.width = 144;
  block.height = 48;
  block.computedStyle = {
    font: "600 16px serif",
    lineHeight: "24px",
    direction: "rtl",
    writingMode: "vertical-rl",
    whiteSpace: "pre-wrap",
    textAlign: "end",
    maxWidth: "144px",
    getPropertyValue(name) {
      return this[name] || "";
    },
  };

  const env = createContext({
    elements: [block],
    visibilityState: "hidden",
    prefersReducedMotion: true,
    devicePixelRatio: 1.75,
    deviceMemory: 4,
    hardwareConcurrency: 4,
    visualViewportWidth: 640,
    visualViewportHeight: 360,
    visualViewportOffsetTop: 12,
    matchMedia: {
      "(prefers-reduced-data: reduce)": true,
      "(pointer: coarse)": true,
      "(any-pointer: coarse)": true,
      "(hover: hover)": false,
      "(any-hover: hover)": false,
      "(prefers-contrast: more)": true,
      "(prefers-color-scheme: dark)": true,
    },
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-docs-home",
      pattern: "GET /docs",
      path: "/docs",
      title: "Docs",
      status: 200,
      requestID: "req-123",
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  const fileCSS = env.document.createElement("style");
  fileCSS.setAttribute("data-gosx-file-css", "docs.css");
  fileCSS.setAttribute("data-gosx-file-css-scope", "docs-scope");
  fileCSS.setAttribute("data-gosx-css-layer", "page");
  fileCSS.setAttribute("data-gosx-css-owner", "page-file");
  fileCSS.setAttribute("data-gosx-css-source", "docs.css");
  fileCSS.setAttribute("data-gosx-css-order", "0");
  const stylesheet = env.document.createElement("link");
  stylesheet.setAttribute("rel", "stylesheet");
  stylesheet.setAttribute("href", "/app.css");
  appendManagedHead(env.document, [contract, fileCSS, stylesheet]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const environmentState = env.context.__gosx.environment.get();
  assert.equal(environmentState.pageVisible, false);
  assert.equal(environmentState.coarsePointer, true);
  assert.equal(environmentState.reducedMotion, true);
  assert.equal(environmentState.reducedData, true);
  assert.equal(environmentState.lowPower, true);
  assert.equal(environmentState.colorScheme, "dark");
  assert.equal(environmentState.contrast, "more");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-env-reduced-motion"), "true");
  assert.equal(env.document.documentElement.style["--gosx-env-visual-viewport-height"], "360px");

  const documentState = env.context.__gosx.document.get();
  assert.equal(documentState.page.id, "gosx-doc-docs-home");
  assert.equal(documentState.page.pattern, "GET /docs");
  assert.equal(documentState.enhancement.layer, "bootstrap");
  assert.equal(documentState.enhancement.navigation, true);
  assert.equal(documentState.css.owned[0].file, "docs.css");
  assert.equal(documentState.css.owned[0].layer, "page");
  assert.equal(documentState.css.layers.page.count, 1);
  assert.equal(documentState.css.layers.page.owners[0], "page-file");
  assert.equal(documentState.css.owned.some((entry) => entry.kind === "stylesheet" && entry.href === "/app.css"), true);
  assert.equal(documentState.css.stylesheets[0].layer, "global");
  assert.equal(documentState.css.stylesheets[0].owner, "document-global");
  assert.equal(documentState.css.stylesheets[0].source, "/app.css");
  assert.equal(documentState.css.owned.some((entry) => entry.layer === "runtime"), true);
  assert.equal(documentState.css.layers.runtime.count, 1);
  assert.equal(env.context.__gosx.document.css("page").count, 1);
  assert.equal(env.document.documentElement.getAttribute("data-gosx-document-id"), "gosx-doc-docs-home");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-css-page-count"), "1");
  assert.equal(env.document.body.getAttribute("data-gosx-enhancement-layer"), "bootstrap");

  const presentation = env.context.__gosx.presentation.read(block);
  assert.equal(presentation.direction, "rtl");
  assert.equal(presentation.writingMode, "vertical-rl");
  assert.equal(presentation.lang, "");
  assert.equal(presentation.inlineSize, 48);
  assert.equal(presentation.blockSize, 144);
  assert.equal(presentation.maxWidth, 144);
  assert.equal(presentation.maxInlineSize, 144);
  assert.equal(presentation.environment.reducedData, true);
});

test("bootstrap exposes document assets and enhancement inventory", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs");
  link.setAttribute("data-gosx-link", "");
  link.setAttribute("data-gosx-enhance", "navigation");
  link.setAttribute("data-gosx-enhance-layer", "bootstrap");
  link.setAttribute("data-gosx-fallback", "native-link");

  const form = new FakeElement("form", null);
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-enhance", "form");
  form.setAttribute("data-gosx-enhance-layer", "bootstrap");
  form.setAttribute("data-gosx-fallback", "native-form");

  const text = new FakeElement("div", null);
  text.setAttribute("data-gosx-text-layout", "");
  text.setAttribute("data-gosx-enhance", "text-layout");
  text.setAttribute("data-gosx-enhance-layer", "bootstrap");
  text.setAttribute("data-gosx-fallback", "html");

  const scene = new FakeElement("div", null);
  scene.id = "scene-runtime";
  scene.setAttribute("data-gosx-engine", "GoSXScene3D");
  scene.setAttribute("data-gosx-enhance", "scene3d");
  scene.setAttribute("data-gosx-enhance-layer", "runtime");
  scene.setAttribute("data-gosx-fallback", "server");

  const island = new FakeElement("div", null);
  island.id = "counter-island";
  island.setAttribute("data-gosx-island", "Counter");
  island.setAttribute("data-gosx-enhance", "island");
  island.setAttribute("data-gosx-enhance-layer", "runtime");
  island.setAttribute("data-gosx-fallback", "server");

  const env = createContext({
    elements: [link, form, text, scene, island],
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-docs-home",
      pattern: "GET /docs",
      path: "/docs",
      title: "Docs",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: true,
      navigation: true,
    },
    assets: {
      bootstrapMode: "full",
      manifest: true,
      runtimePath: "/runtime.wasm",
      wasmExecPath: "/wasm_exec.js",
      patchPath: "/patch.js",
      bootstrapPath: "/bootstrap.js",
      hlsPath: "/hls.min.js",
      islands: 1,
      engines: 1,
      hubs: 1,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const documentState = env.context.__gosx.document.get();
  assert.equal(documentState.assets.runtime.bootstrapMode, "full");
  assert.equal(documentState.assets.runtime.manifest, true);
  assert.equal(documentState.assets.runtime.runtimePath, "/runtime.wasm");
  assert.equal(documentState.assets.runtime.bootstrapPath, "/bootstrap.js");
  assert.equal(documentState.assets.runtime.hlsPath, "/hls.min.js");
  assert.equal(documentState.assets.runtime.islands, 1);
  assert.equal(documentState.assets.runtime.engines, 1);
  assert.equal(documentState.assets.runtime.hubs, 1);
  assert.equal(documentState.enhancement.bootstrap, true);
  assert.equal(documentState.enhancement.runtime, true);
  assert.equal(documentState.enhancements.count, 5);
  assert.equal(documentState.enhancements.layers.bootstrap.count, 3);
  assert.equal(documentState.enhancements.layers.runtime.count, 2);
  assert.equal(documentState.enhancements.kinds.navigation.count, 1);
  assert.equal(documentState.enhancements.kinds["text-layout"].count, 1);
  assert.equal(env.context.__gosx.document.enhancements("scene3d").count, 1);
  assert.equal(env.document.documentElement.getAttribute("data-gosx-bootstrap-mode"), "full");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-enhancement-count"), "5");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-enhancement-navigation-count"), "1");
  assert.equal(env.document.body.getAttribute("data-gosx-enhancement-runtime-count"), "2");
});

test("bootstrap refreshes document state after navigation events", async () => {
  const env = createContext({});
  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-home",
      pattern: "GET /",
      path: "/",
      title: "Home",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.document.get().page.id, "gosx-doc-home");

  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-docs",
      pattern: "GET /docs",
      path: "/docs",
      title: "Docs",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  env.document.dispatchEvent(new env.context.CustomEvent("gosx:navigate", {
    detail: { url: "/docs" },
  }));
  await flushAsyncWork();

  assert.equal(env.context.__gosx.document.get().page.id, "gosx-doc-docs");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-route-pattern"), "GET /docs");
});

test("bootstrap refreshes document CSS state after head mutations", async () => {
  const env = createContext({});
  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-home",
      pattern: "GET /",
      path: "/",
      title: "Home",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const stylesheet = env.document.createElement("link");
  stylesheet.setAttribute("rel", "stylesheet");
  stylesheet.setAttribute("href", "/layout.css");
  stylesheet.setAttribute("data-gosx-css-layer", "layout");
  stylesheet.setAttribute("data-gosx-css-owner", "document-layout");
  stylesheet.setAttribute("data-gosx-css-source", "layout.css");
  const managedEnd = env.document.head.childNodes.find((node) => node.getAttribute && node.getAttribute("name") === "gosx-head-end");
  env.document.head.insertBefore(stylesheet, managedEnd);

  const headObserver = env.mutationObservers.find((observer) => observer.targets.has(env.document.head));
  assert.ok(headObserver, "expected head mutation observer");
  headObserver.trigger([{ target: env.document.head, type: "childList" }]);
  await flushAsyncWork();
  env.context.__gosx.document.refresh("head-mutation");

  assert.equal(env.context.__gosx.document.get().css.layers.layout.count, 1);
  assert.equal(env.context.__gosx.document.css("layout").sources[0], "layout.css");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-css-layout-count"), "1");
});

test("bootstrap coalesces head mutation refreshes into one document update turn", async () => {
  const env = createContext({});
  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-home",
      pattern: "GET /",
      path: "/",
      title: "Home",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const initialEvents = env.document.dispatchedEvents.filter((event) => event.type === "gosx:document").length;
  const headObserver = env.mutationObservers.find((observer) => observer.targets.has(env.document.head));
  assert.ok(headObserver);

  const stylesheet = env.document.createElement("link");
  stylesheet.setAttribute("rel", "stylesheet");
  stylesheet.setAttribute("href", "/page.css");
  stylesheet.setAttribute("data-gosx-css-layer", "page");
  stylesheet.setAttribute("data-gosx-css-owner", "document-page");
  stylesheet.setAttribute("data-gosx-css-source", "page.css");
  const managedEnd = env.document.head.childNodes.find((node) => node.getAttribute && node.getAttribute("name") === "gosx-head-end");
  env.document.head.insertBefore(stylesheet, managedEnd);
  stylesheet.setAttribute("media", "screen");

  headObserver.trigger([{ target: env.document.head, type: "childList" }]);
  headObserver.trigger([{ target: stylesheet, type: "attributes", attributeName: "media" }]);
  await flushAsyncWork();

  const nextEvents = env.document.dispatchedEvents.filter((event) => event.type === "gosx:document").length;
  assert.equal(nextEvents, initialEvents + 1);
});

test("bootstrap refreshes managed text layout blocks after font metric invalidation", async () => {
  let scale = 1;
  const fonts = new FakeFontSet();
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
    fonts,
    measureText(text) {
      return String(text).length * 8 * scale;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "2");

  scale = 2;
  fonts.dispatch("loadingdone");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "4");
  assert.equal(block.getAttribute("data-gosx-text-layout-revision"), "1");
  assert.equal(env.context.__gosx.textLayout.read(block).lineCount, 4);
});

test("bootstrap adopts and caches runtime-provided text layout implementations", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  let calls = 0;
  env.context.__gosx_text_layout = function(text, font, maxWidth, whiteSpace, lineHeight) {
    calls += 1;
    return {
      lines: [{ text: String(text) }],
      lineCount: 1,
      height: Number(lineHeight) || 1,
      maxLineWidth: Math.min(Number(maxWidth) || 0, 24),
      byteLen: String(text).length,
      runeCount: String(text).length,
      font,
      whiteSpace,
    };
  };

  env.context.__gosx_runtime_ready();

  const first = env.context.__gosx_text_layout("hi", "600 16px serif", 80, "normal", 18);
  const second = env.context.__gosx_text_layout("hi", "600 16px serif", 80, "normal", 18);
  assert.equal(calls, 1);
  assert.equal(first.lineCount, 1);
  assert.equal(second.lineCount, 1);
  assert.equal(first.height, 18);
  assert.equal(second.maxLineWidth, 24);
});

test("bootstrap adopts and caches runtime-provided text layout metrics implementations", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  let calls = 0;
  env.context.__gosx_text_layout_metrics = function(text, font, maxWidth, whiteSpace, lineHeight) {
    calls += 1;
    return {
      lineCount: 3,
      height: Number(lineHeight) * 3,
      maxLineWidth: Math.min(Number(maxWidth) || 0, 42),
      byteLen: String(text).length,
      runeCount: String(text).length,
      font,
      whiteSpace,
    };
  };

  env.context.__gosx_runtime_ready();

  const first = env.context.__gosx_text_layout_metrics("hi", "600 16px serif", 80, "normal", 18);
  const second = env.context.__gosx_text_layout_metrics("hi", "600 16px serif", 80, "normal", 18);
  assert.equal(calls, 1);
  assert.equal(first.lineCount, 3);
  assert.equal(second.height, 54);
  assert.equal(second.maxLineWidth, 42);
});

test("bootstrap adopts and caches runtime-provided text layout ranges implementations", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  let calls = 0;
  env.context.__gosx_text_layout_ranges = function(text) {
    calls += 1;
    return {
      lines: [{ start: 0, end: 1, byteStart: 0, byteEnd: 1, runeStart: 0, runeEnd: 1, width: 7, hardBreak: false, softBreak: true }],
      lineCount: 1,
      height: 18,
      maxLineWidth: 7,
      byteLen: String(text).length,
      runeCount: String(text).length,
    };
  };

  env.context.__gosx_runtime_ready();

  const first = env.context.__gosx_text_layout_ranges("x", "600 16px serif", 80, "normal", 18);
  const second = env.context.__gosx_text_layout_ranges("x", "600 16px serif", 80, "normal", 18);
  assert.equal(calls, 1);
  assert.equal(first.lines[0].softBreak, true);
  assert.equal(second.maxLineWidth, 7);
});

test("bootstrap falls back to browser layout when runtime text layout fails", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  env.context.__gosx_text_layout = function() {
    throw new Error("boom");
  };

  env.context.__gosx_runtime_ready();

  const layout = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["hello world", "from gosx"]);
  assert.equal(layout.lineCount, 2);
  assert.equal(env.consoleLogs.error.length > 0, true);
});

test("bootstrap rerenders static Scene3D labels after font loading changes metrics", async () => {
  let scale = 1;
  const fonts = new FakeFontSet();
  const mount = new FakeElement("div", null);
  mount.id = "scene-font-refresh";

  const env = createContext({
    elements: [mount],
    fonts,
    measureText(text) {
      return String(text).length * 8 * scale;
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-font-refresh",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-font-refresh",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.8, height: 1.2, depth: 1.1, x: -0.8, y: 0.1, z: 0.2, color: "#8de1ff" },
              ],
              labels: [
                {
                  id: "font-refresh-label",
                  text: "hello world from gosx",
                  x: 0,
                  y: 1.3,
                  z: 0.8,
                  maxWidth: 88,
                },
              ],
            },
          },
          capabilities: ["canvas"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  assert.equal(labelLayer.children.length, 1);
  assert.equal(labelLayer.children[0].children.length, 2);

  scale = 2;
  fonts.dispatch("loadingdone");
  await flushAsyncWork();

  assert.equal(labelLayer.children[0].children.length, 4);
  assert.equal(labelLayer.children[0].textContent, "helloworldfromgosx");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap infers binary island programs from .gxi refs", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);

  wrapper.id = "gosx-island-bin";
  componentRoot.appendChild(new FakeElement("span", null));
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.gxi": { bytes: [1, 2, 3, 4] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-bin",
          component: "Counter",
          props: {},
          programRef: "/counter.gxi",
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  assert.equal(env.hydrateCalls[0][4], "bin");
  assert.ok(env.hydrateCalls[0][3] instanceof Uint8Array);
  assert.deepEqual(Array.from(env.hydrateCalls[0][3]), [1, 2, 3, 4]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap mounts registered surface engines without escape-hatch scripts", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "board-root";

  const env = createContext({
    elements: [mount],
    engineFactories: {
      Whiteboard(ctx) {
        env.engineMounts.push({
          id: ctx.id,
          component: ctx.component,
          mountID: ctx.mount.id,
          props: ctx.props,
          capabilities: ctx.capabilities.slice(),
        });
        return {
          dispose() {
            env.engineDisposals.push(ctx.id);
          },
        };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-0",
          component: "Whiteboard",
          kind: "surface",
          mountId: "board-root",
          props: { room: "abc" },
          capabilities: ["canvas", "animation"],
          programRef: "/engines/Whiteboard.wasm",
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.deepEqual(env.engineMounts, [
    {
      id: "gosx-engine-0",
      component: "Whiteboard",
      mountID: "board-root",
      props: { room: "abc" },
      capabilities: ["canvas", "animation"],
    },
  ]);

  env.context.__gosx_dispose_engine("gosx-engine-0");
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.deepEqual(env.engineDisposals, ["gosx-engine-0"]);
  assert.equal(env.consoleLogs.warn.length, 0);
});

test("bootstrap exposes managed pixel surfaces to surface engines", async () => {
  const mount = new FakeElement("div", null);
  const fallback = new FakeElement("p", null);
  fallback.textContent = "server fallback";
  mount.id = "pixel-root";
  mount.width = 320;
  mount.height = 288;
  mount.appendChild(fallback);

  const env = createContext({
    elements: [mount],
    engineFactories: {
      PixelBoard(ctx) {
        const frameFromContext = ctx.runtime.pixelSurface();
        const frameFromGlobal = env.context.__gosx_engine_frame(ctx.id);
        env.engineMounts.push({
          id: ctx.id,
          sameFrame: frameFromContext === frameFromGlobal,
          width: frameFromContext.width,
          height: frameFromContext.height,
          scaling: frameFromContext.scaling,
          inside: frameFromContext.toPixel(64, 72).inside,
          pixel: frameFromContext.toPixel(64, 72),
        });
        frameFromContext.pixels[0] = 17;
        frameFromContext.pixels[1] = 34;
        frameFromContext.pixels[2] = 51;
        frameFromContext.pixels[3] = 255;
        frameFromContext.present();
        return {
          dispose() {
            env.engineDisposals.push(ctx.id);
          },
        };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-pixel",
          component: "PixelBoard",
          kind: "surface",
          mountId: "pixel-root",
          props: { mode: "retro" },
          capabilities: ["pixel-surface", "canvas"],
          pixelSurface: {
            width: 160,
            height: 144,
            scaling: "fill",
            clearColor: [3, 4, 5, 255],
            vsync: false,
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(env.engineMounts)), [
    {
      id: "gosx-engine-pixel",
      sameFrame: true,
      width: 160,
      height: 144,
      scaling: "fill",
      inside: true,
      pixel: { x: 32, y: 36, inside: true },
    },
  ]);
  assert.equal(mount.getAttribute("data-gosx-pixel-surface-mounted"), "true");
  assert.equal(mount.style.backgroundColor, "rgba(3, 4, 5, 1)");
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0].tagName, "CANVAS");
  assert.equal(mount.children[0].getAttribute("data-gosx-pixel-surface"), "true");
  assert.equal(mount.children[0].width, 160);
  assert.equal(mount.children[0].height, 144);
  assert.equal(mount.children[0].style.width, "320px");
  assert.equal(mount.children[0].style.height, "288px");
  const ctx2d = mount.children[0].getContext("2d");
  assert.ok(ctx2d.ops.some((entry) => entry[0] === "putImageData" && entry[1] === 0 && entry[2] === 0));
  assert.equal(Array.from(ctx2d.lastImageData.data.slice(0, 4)).join(","), "17,34,51,255");
  const frame = env.context.__gosx_engine_frame("gosx-engine-pixel");
  assert.equal(frame.width, 160);
  assert.deepEqual(JSON.parse(JSON.stringify(frame.toPixel(64, 72))), { x: 32, y: 36, inside: true });

  env.context.__gosx_dispose_engine("gosx-engine-pixel");
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(env.context.__gosx_engine_frame("gosx-engine-pixel"), null);
  assert.deepEqual(env.engineDisposals, ["gosx-engine-pixel"]);
  assert.equal(mount.getAttribute("data-gosx-pixel-surface-mounted"), null);
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0], fallback);
  assert.equal(mount.children[0].textContent, "server fallback");
});

test("bootstrap restores server fallback when pixel-surface engine mount fails", async () => {
  const mount = new FakeElement("div", null);
  const fallback = new FakeElement("p", null);
  fallback.textContent = "loading";
  mount.id = "broken-pixel-root";
  mount.width = 320;
  mount.height = 288;
  mount.appendChild(fallback);

  const env = createContext({
    elements: [mount],
    engineFactories: {
      BrokenPixel(ctx) {
        const frame = ctx.runtime.frame();
        frame.present();
        throw new Error("boom");
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-broken-pixel",
          component: "BrokenPixel",
          kind: "surface",
          mountId: "broken-pixel-root",
          capabilities: ["pixel-surface", "canvas"],
          pixelSurface: {
            width: 160,
            height: 144,
            scaling: "fill",
            vsync: false,
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(env.context.__gosx_engine_frame("gosx-engine-broken-pixel"), null);
  assert.equal(mount.getAttribute("data-gosx-runtime-state"), "error");
  assert.equal(mount.getAttribute("data-gosx-runtime-issue"), "mount");
  assert.equal(mount.getAttribute("data-gosx-fallback-active"), "server");
  assert.equal(mount.getAttribute("data-gosx-pixel-surface-mounted"), null);
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0], fallback);
  assert.equal(mount.children[0].textContent, "loading");
  const issues = env.context.__gosx.listIssues();
  assert.equal(issues.some((issue) => issue.scope === "engine" && issue.type === "mount" && issue.source === "gosx-engine-broken-pixel"), true);
});

test("bootstrap batches keyboard and pointer input for capable engines", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "input-root";

  const env = createContext({
    elements: [mount],
    engineFactories: {
      InputSurface() {
        return {};
      },
    },
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-input",
          component: "InputSurface",
          kind: "surface",
          mountId: "input-root",
          props: {},
          capabilities: ["pointer", "keyboard"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.document.dispatchEvent({ type: "keydown", key: "W" });
  env.document.dispatchEvent({
    type: "pointermove",
    clientX: 40,
    clientY: 25,
    movementX: 3,
    movementY: -2,
    buttons: 1,
  });
  await flushAsyncWork();

  assert.equal(env.inputBatchCalls.length > 0, true);
  const firstBatch = JSON.parse(env.inputBatchCalls[0][0]);
  assert.equal(firstBatch["$input.key.w"], true);
  assert.equal(firstBatch["$input.pointer.x"], 40);
  assert.equal(firstBatch["$input.pointer.y"], 25);
  assert.equal(firstBatch["$input.pointer.deltaX"], 3);
  assert.equal(firstBatch["$input.pointer.deltaY"], -2);
  assert.equal(firstBatch["$input.pointer.buttons"], 1);

  env.context.dispatchEvent({ type: "blur" });
  await flushAsyncWork();

  const lastBatch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(lastBatch["$input.key.w"], false);
  assert.equal(lastBatch["$input.pointer.buttons"], 0);

  env.context.__gosx_dispose_engine("gosx-engine-input");
  assert.equal(env.document.eventListeners.get("keydown").length, 0);
  assert.equal(env.document.eventListeners.get("pointermove").length, 0);
});

test("bootstrap hydrates shared-runtime Scene3D programs", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-runtime-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-program.json": { text: '{"name":"GeometryZoo"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-rt",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-runtime-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-program.json",
        },
      ],
    },
    onHydrateEngine: () => JSON.stringify([
      { kind: 5, objectId: 0, data: { x: 0, y: 0, z: 6, fov: 75 } },
      {
        kind: 0,
        objectId: 1,
        data: {
          kind: "mesh",
          geometry: "sphere",
          material: "flat",
          props: { x: 0, y: 0, z: 0, radius: 1.2, color: "#8de1ff", spinY: 0.35 },
        },
      },
    ]),
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0.1, y: -0.05, z: 6.2, fov: 72 },
      lines: [
        {
          from: { x: 10, y: 12 },
          to: { x: 120, y: 96 },
          color: "#8de1ff",
          lineWidth: 1.8,
        },
      ],
      positions: [-0.9, 0.93, -0.2, 0.47],
      colors: [0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1],
      vertexCount: 2,
      worldPositions: [
        -2.4, -1.5, 0.1, 2.4, -1.5, 0.1,
        -0.8, 0.2, 0.5, 0.7, 0.9, 1.1,
        -1.2, -0.4, 0.2, 1.1, 0.6, 1.4,
      ],
      worldColors: [
        0.25, 0.33, 0.41, 1, 0.25, 0.33, 0.41, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
        0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1,
      ],
      worldVertexCount: 6,
      materials: [
        { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
        { kind: "glass", color: "#c7f0ff", opacity: 0.45, wireframe: true, blendMode: "alpha", emissive: 0.05 },
        { kind: "glow", color: "#8de1ff", opacity: 0.7, wireframe: true, blendMode: "additive", emissive: 0.4 },
      ],
      objects: [
        { id: "floor", kind: "plane", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: true },
        { id: "shield", kind: "box", materialIndex: 1, vertexOffset: 2, vertexCount: 2, static: false },
        { id: "orb", kind: "sphere", materialIndex: 2, vertexOffset: 4, vertexCount: 2, static: false },
      ],
      labels: [
        {
          id: "orb-label",
          text: "Orbit node\nShared runtime",
          position: { x: 318, y: 132 },
          depth: 7.2,
          maxWidth: 188,
          font: '600 13px "IBM Plex Sans", "Segoe UI", sans-serif',
          lineHeight: 18,
          whiteSpace: "pre-wrap",
          textAlign: "center",
        },
      ],
      objectCount: 3,
    }),
  });
  const textLayoutCalls = [];
  env.context.__gosx_text_layout = (...args) => {
    textLayoutCalls.push(args);
    return {
      lines: [{ text: "Orbit node" }, { text: "Shared runtime" }],
      lineCount: 2,
      height: 36,
      maxLineWidth: 94,
    };
  };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.engineHydrateCalls.length, 1);
  assert.deepEqual(env.engineHydrateCalls[0].slice(0, 3), [
    "gosx-engine-rt",
    "GoSXScene3D",
    '{"width":640,"height":360,"background":"#08151f"}',
  ]);
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/scene-program.json"),
    true,
  );
  assert.equal(mount.children[0].tagName, "CANVAS");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(env.engineRenderCalls.length > 0, true);
  assert.equal(env.engineTickCalls.length, 0);
  const gl = mount.children[0].getContext("webgl");
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform4f" && entry[1] === "u_camera"));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[2] === 3));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[1] === 2 && entry[2] === 3));
  assert.ok(gl.ops.filter((entry) => entry[0] === "drawArrays").length >= 2);
  assert.ok(gl.ops.some((entry) => entry[0] === "enable" && entry[1] === gl.BLEND));
  assert.ok(gl.ops.some((entry) => entry[0] === "enable" && entry[1] === gl.DEPTH_TEST));
  assert.ok(gl.ops.some((entry) => entry[0] === "clear" && entry[1] === (gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)));
  assert.ok(gl.ops.some((entry) => entry[0] === "depthMask" && entry[1] === true));
  assert.ok(gl.ops.some((entry) => entry[0] === "depthMask" && entry[1] === false));
  assert.ok(gl.ops.some((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW));
  assert.ok(gl.ops.some((entry) => entry[0] === "bufferData" && entry[4] === gl.DYNAMIC_DRAW));
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE_MINUS_SRC_ALPHA));
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE));
  assert.equal(mount.children.length, 2);
  assert.equal(mount.children[1].getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(mount.children[1].children.length, 1);
  assert.equal(mount.children[1].children[0].textContent, "Orbit nodeShared runtime");
  assert.equal(textLayoutCalls.length, 1);
  assert.equal(textLayoutCalls[0][0], "Orbit node\nShared runtime");
  assert.equal(textLayoutCalls[0][1], '600 13px "IBM Plex Sans", "Segoe UI", sans-serif');
  assert.equal(textLayoutCalls[0][2], 188);
  assert.equal(textLayoutCalls[0][3], "pre-wrap");
  assert.equal(textLayoutCalls[0][4], 18);
  assert.equal(textLayoutCalls[0][5].maxLines, 0);
  assert.equal(textLayoutCalls[0][5].overflow, "clip");

  env.context.__gosx_dispose_engine("gosx-engine-rt");
  assert.deepEqual(env.engineDisposeCalls, [["gosx-engine-rt"]]);
});

test("Scene3D drag only starts when the pointer lands on a shape in shared runtime mode", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-fallback-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-drag-program.json": { text: '{"name":"SceneDrag"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-fallback",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-fallback-root",
          runtime: "shared",
          programRef: "/scene-drag-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            autoRotate: false,
            dragToRotate: true,
            dragSignalNamespace: "$scene.test.drag",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -2.4, -1.5, 0.1, 2.4, -1.5, 0.1,
        -0.8, 0.2, 0.5, 0.7, 0.9, 1.1,
      ],
      worldColors: [
        0.25, 0.33, 0.41, 1, 0.25, 0.33, 0.41, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        {
          id: "floor",
          kind: "plane",
          materialIndex: 0,
          vertexOffset: 0,
          vertexCount: 2,
          static: true,
          bounds: { minX: -2.4, minY: -1.5, minZ: 0.1, maxX: 2.4, maxY: -1.5, maxZ: 0.1 },
        },
        {
          id: "shape",
          kind: "box",
          materialIndex: 1,
          vertexOffset: 2,
          vertexCount: 2,
          static: false,
          bounds: { minX: -0.8, minY: 0.2, minZ: 0.5, maxX: 0.7, maxY: 0.9, maxZ: 1.1 },
        },
      ],
      objectCount: 2,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  assert.equal(canvas.tagName, "CANVAS");
  assert.equal(canvas.style.cursor, "grab");
  assert.equal((env.document.eventListeners.get("pointermove") || []).length, 0);
  assert.equal((env.document.eventListeners.get("pointerup") || []).length, 0);
  assert.equal((env.document.eventListeners.get("pointercancel") || []).length, 0);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 1,
    clientX: 56,
    clientY: 320,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  assert.equal(canvas.style.cursor, "grab");
  assert.equal(canvas._capturedPointerID, null);
  assert.equal(env.inputBatchCalls.length, 0);
  assert.equal((env.document.eventListeners.get("pointermove") || []).length, 0);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 2,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  assert.equal(canvas.style.cursor, "grabbing");
  assert.equal(canvas._capturedPointerID, 2);
  assert.equal((env.document.eventListeners.get("pointermove") || []).length, 1);
  assert.equal((env.document.eventListeners.get("pointerup") || []).length, 1);
  assert.equal((env.document.eventListeners.get("pointercancel") || []).length, 1);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 3,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  assert.equal(canvas._capturedPointerID, 2);

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    buttons: 1,
    pointerId: 2,
    clientX: 360,
    clientY: 130,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  assert.equal(env.inputBatchCalls.length > 0, true);
  const dragBatch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(dragBatch["$scene.test.drag.active"], true);
  assert.equal(dragBatch["$scene.test.drag.x"] > 0, true);
  assert.equal(dragBatch["$scene.test.drag.y"] > 0, true);
  assert.equal(dragBatch["$scene.test.drag.targetIndex"], 1);

  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 2,
    clientX: 360,
    clientY: 130,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  assert.equal(canvas.style.cursor, "grab");
  assert.equal(canvas._capturedPointerID, null);
  assert.equal((env.document.eventListeners.get("pointermove") || []).length, 0);
  assert.equal((env.document.eventListeners.get("pointerup") || []).length, 0);
  assert.equal((env.document.eventListeners.get("pointercancel") || []).length, 0);
  const releaseBatch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(releaseBatch["$scene.test.drag.active"], false);
  assert.equal(releaseBatch["$scene.test.drag.targetIndex"], -1);
});

test("bootstrap drives shared-runtime Scene3D orbit controls without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-shared-orbit-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-orbit-program.json": { text: '{"name":"SceneOrbit"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-orbit",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-shared-orbit-root",
          runtime: "shared",
          programRef: "/scene-orbit-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            autoRotate: false,
            controls: "orbit",
            controlTarget: { x: 0, y: 0.2, z: 0.8 },
            camera: { x: 0, y: 0.2, z: 6, fov: 72, near: 0.05, far: 128 },
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0.2, z: 6, fov: 72, near: 0.05, far: 128 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -1.2, -0.7, 0.1, 1.2, -0.7, 0.1,
        0.1, -0.2, 0.1, 0.1, 1.4, 1.5,
      ],
      worldColors: [
        0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0 },
      ],
      objects: [
        {
          id: "frame",
          kind: "box",
          materialIndex: 0,
          renderPass: "opaque",
          vertexOffset: 0,
          vertexCount: 4,
          static: true,
          bounds: { minX: -1.2, minY: -0.7, minZ: 0.1, maxX: 1.2, maxY: 1.4, maxZ: 1.5 },
          depthNear: 6.1,
          depthFar: 7.5,
          depthCenter: 6.8,
        },
      ],
      passes: [
        {
          name: "staticOpaque",
          blend: "opaque",
          depth: "opaque",
          static: true,
          cacheKey: "orbit-static",
          positions: [
            -1.2, -0.7, 0.1, 1.2, -0.7, 0.1,
            0.1, -0.2, 0.1, 0.1, 1.4, 1.5,
          ],
          colors: [
            0.55, 0.88, 1, 1, 0.55, 0.88, 1, 1,
            0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
          ],
          materials: [
            0, 0, 1,
            0, 0, 1,
            0, 0, 1,
            0, 0, 1,
          ],
          vertexCount: 4,
        },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.firstElementChild;
  const gl = canvas.getContext("webgl");
  const initialRotation = gl.ops.filter((entry) => entry[0] === "uniform3f" && entry[1] === "u_camera_rotation").at(-1);
  const initialCamera = gl.ops.filter((entry) => entry[0] === "uniform4f" && entry[1] === "u_camera").at(-1);
  assert.ok(initialRotation);
  assert.equal(initialRotation[1], "u_camera_rotation");
  assert.ok(Math.abs(initialRotation[2]) < 0.0001);
  assert.ok(Math.abs(initialRotation[3]) < 0.0001);
  assert.ok(Math.abs(initialRotation[4]) < 0.0001);
  assert.ok(initialCamera);
  assert.equal(initialCamera[5], 72);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 9,
    clientX: 320,
    clientY: 180,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    buttons: 1,
    pointerId: 9,
    clientX: 410,
    clientY: 120,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 9,
    clientX: 410,
    clientY: 120,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  const draggedRotation = gl.ops.filter((entry) => entry[0] === "uniform3f" && entry[1] === "u_camera_rotation").at(-1);
  assert.ok(draggedRotation);
  assert.ok(Math.abs(draggedRotation[2]) > 0.01 || Math.abs(draggedRotation[3]) > 0.01);

  canvas.dispatchEvent({
    type: "wheel",
    deltaY: -120,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  const zoomedCamera = gl.ops.filter((entry) => entry[0] === "uniform4f" && entry[1] === "u_camera").at(-1);
  assert.ok(zoomedCamera);
  assert.equal(zoomedCamera[5], 72);
  assert.notEqual(zoomedCamera[4], initialCamera[4]);
  assert.equal(mount.getAttribute("data-gosx-scene3d-controls"), "orbit");
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap loads declarative Scene3D model assets without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/runner.gosx3d.json": {
        text: JSON.stringify({
          objects: [
            {
              id: "runner-frame",
              kind: "lines",
              points: [
                { x: -0.8, y: -0.3, z: 0 },
                { x: 0.9, y: -0.3, z: 0 },
                { x: 0.9, y: 0.35, z: 0.2 },
                { x: -0.8, y: 0.35, z: 0.2 },
                { x: -0.2, y: 0.65, z: 0.45 },
                { x: 0.25, y: 0.65, z: 0.45 },
              ],
              segments: [[0, 1], [1, 2], [2, 3], [3, 0], [2, 4], [3, 5], [4, 5]],
              material: {
                kind: "matte",
                color: "#5ca8ff",
              },
            },
          ],
          labels: [
            {
              id: "runner-label",
              text: "Model asset",
              x: 0,
              y: 1.05,
              z: 0.35,
              maxWidth: 160,
            },
          ],
          sprites: [
            {
              id: "runner-card",
              src: "../paper-card.png",
              x: 0,
              y: 0.62,
              z: 0.12,
              width: 1.5,
              height: 1,
              opacity: 0.92,
              priority: 3,
              anchorX: 0.5,
              anchorY: 0.5,
              fit: "cover",
              occlude: false,
            },
          ],
          lights: [
            {
              id: "runner-light",
              kind: "point",
              color: "#ffd48f",
              intensity: 1.15,
              x: 0.4,
              y: 0.9,
              z: 1.2,
              range: 4.8,
              decay: 1.35,
            },
          ],
        }),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            models: [
              {
                id: "runner",
                src: "/models/runner.gosx3d.json",
                x: 1.1,
                y: 0.2,
                z: -0.6,
                rotationY: 0.42,
                scaleX: 1.35,
                scaleY: 1.1,
                scaleZ: 1.2,
                materialKind: "glow",
                color: "#ffd48f",
                opacity: 0.74,
                emissive: 0.26,
                blendMode: "additive",
                renderPass: "additive",
                static: true,
              },
            ],
            scene: {
              objects: [
                {
                  id: "guide",
                  kind: "lines",
                  points: [
                    { x: -1, y: -0.8, z: 0 },
                    { x: 1, y: -0.8, z: 0 },
                    { x: 1, y: 0.8, z: 0 },
                    { x: -1, y: 0.8, z: 0 },
                  ],
                  segments: [[0, 1], [1, 2], [2, 3], [3, 0]],
                  color: "#8de1ff",
                },
              ],
            },
          },
        },
      ],
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/runner.gosx3d.json"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.children[0].tagName, "CANVAS");
  assert.equal(mount.children[1].getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(mount.children[1].children.length, 2);
  const modelLabel = mount.children[1].children.find((child) => child.getAttribute("data-gosx-scene-label") === "runner/runner-label");
  const modelSprite = mount.children[1].children.find((child) => child.getAttribute("data-gosx-scene-sprite") === "runner/runner-card");
  assert.ok(modelLabel);
  assert.equal(modelLabel.textContent, "Model asset");
  assert.ok(modelSprite);
  assert.equal(modelSprite.getAttribute("data-gosx-scene-sprite-fit"), "cover");
  assert.equal(modelSprite.firstChild.getAttribute("src"), "http://localhost:3000/paper-card.png");
  const ctx2d = mount.children[0].getContext("2d");
  assert.ok(ctx2d.ops.some((entry) => entry[0] === "lineTo"));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap loads declarative Scene3D GLB model assets through the native renderer path", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-glb-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/runner.glb": {
        bytes: buildMinimalGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-glb",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-glb-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            models: [
              {
                id: "runner",
                src: "/models/runner.glb",
                x: 0.35,
                y: 0.1,
                z: -0.4,
                rotationY: 0.2,
                scaleX: 1.1,
                scaleY: 1.1,
                scaleZ: 1.1,
                static: true,
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/runner.glb"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl);
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === gl.TRIANGLES && entry[3] >= 3));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap GLB loader extracts Scene3D POINTS and LINES primitives", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/points-lines.glb": {
        bytes: buildPointLineGLBBytes(),
      },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/points-lines.glb");

  assert.equal(scene.points.length, 1);
  assert.equal(scene.objects.length, 1);

  const points = scene.points[0];
  assert.equal(points.id, "sparks");
  assert.equal(points.count, 3);
  assert.equal(points.style, "glow");
  assert.equal(points.blendMode, "additive");
  assert.deepEqual(points.live, ["palette"]);
  assert.equal(ArrayBuffer.isView(points.positions), true);
  assert.equal(ArrayBuffer.isView(points.sizes), true);
  assert.equal(ArrayBuffer.isView(points.colors), true);
  assert.deepEqual(Array.from(points.sizes), [2, 3, 4]);
  assert.equal(points.positions[0], 1);
  assert.equal(points.positions[1], 0.5);
  assert.equal(points.colors[0], 1);
  assert.equal(points.colors[1], 0);
  assert.equal(points.colors[2], 0);
  assert.equal(points.colors[3], 1);
  assert.ok(Math.abs(points.colors[7] - (192 / 255)) < 0.00001);

  const lines = scene.objects[0];
  assert.equal(lines.id, "filament-lines");
  assert.equal(lines.kind, "lines");
  assert.equal(lines.points.length, 3);
  assert.equal(JSON.stringify(lines.lineSegments), JSON.stringify([[0, 1], [1, 2]]));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap GLB loader accepts query-stringed GLB model URLs", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/points-lines.glb?bucket=202604211430": {
        bytes: buildPointLineGLBBytes(),
      },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/points-lines.glb?bucket=202604211430");

  assert.equal(scene.points.length, 1);
  assert.equal(scene.points[0].id, "sparks");
  assert.equal(scene.objects.length, 1);
  assert.equal(env.fetchCalls.some((call) => call.url === "/models/points-lines.glb?bucket=202604211430"), true);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap hydrates Scene3D model POINTS from GLB assets", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-glb-points-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/points-lines.glb": {
        bytes: buildPointLineGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-glb-points",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-glb-points-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { z: 5 },
            models: [
              {
                id: "galaxy",
                src: "/models/points-lines.glb",
                scaleX: 1.25,
                scaleY: 1.25,
                scaleZ: 1.25,
                static: true,
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/points-lines.glb"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  const sentinelIDs = mount.__gosxScene3DSentinels
    ? Array.from(mount.__gosxScene3DSentinels.keys())
    : [];
  assert.equal(sentinelIDs.includes("galaxy/sparks"), true);
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === gl.LINES && entry[3] >= 4));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap restores GLB Scene3D point layers through the WebGL2 renderer", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-glb-points-restore-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    fetchRoutes: {
      "/models/points-lines.glb": {
        bytes: buildPointLineGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-glb-points-restore",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-glb-points-restore-root",
          props: {
            width: 640,
            height: 360,
            forceWebGL: true,
            autoRotate: false,
            background: "#08151f",
            camera: { z: 5 },
            models: [
              {
                id: "galaxy",
                src: "/models/points-lines.glb",
                static: true,
              },
            ],
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const initialGl = canvas.getContext("webgl2");
  assert.ok(
    initialGl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === initialGl.POINTS && entry[3] === 3),
    "initial WebGL2 renderer must draw GLB point layers",
  );

  canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
  await flushAsyncWork();
  canvas._webglContext = null;
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  await flushAsyncWork();

  const restoredGl = canvas.getContext("webgl2");
  assert.notEqual(restoredGl, initialGl);
  assert.ok(
    restoredGl.ops.some((entry) => entry[0] === "getUniformLocation" && entry[1] === "u_defaultSize"),
    "restored renderer must use the WebGL2 point shader path",
  );
  assert.ok(
    restoredGl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === restoredGl.POINTS && entry[3] === 3),
    "restored renderer must draw GLB point layers",
  );
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap requests opaque WebGL canvas for opaque Scene3D backgrounds", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-opaque-canvas-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-opaque-canvas",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-opaque-canvas-root",
          props: {
            width: 640,
            height: 360,
            forceWebGL: true,
            background: "#02030a",
            camera: { z: 8 },
            points: [
              {
                id: "stars",
                count: 1,
                positions: [{ x: 0, y: 0, z: 0 }],
                sizes: [2],
                colors: ["#ffffff"],
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const contextCall = canvas.contextCalls.find((call) => call.kind === "webgl2" && call.options && Object.prototype.hasOwnProperty.call(call.options, "alpha"));
  assert.ok(contextCall);
  assert.equal(contextCall.options.alpha, false);
  assert.equal(contextCall.options.premultipliedAlpha, false);
});

test("bootstrap preserves transparent WebGL canvas when Scene3D asks for alpha", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-alpha-canvas-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-alpha-canvas",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-alpha-canvas-root",
          props: {
            width: 640,
            height: 360,
            forceWebGL: true,
            canvasAlpha: true,
            background: "#02030a",
            camera: { z: 8 },
            points: [
              {
                id: "stars",
                count: 1,
                positions: [{ x: 0, y: 0, z: 0 }],
                sizes: [2],
                colors: ["#ffffff"],
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const contextCall = canvas.contextCalls.find((call) => call.kind === "webgl2" && call.options && Object.prototype.hasOwnProperty.call(call.options, "alpha"));
  assert.ok(contextCall);
  assert.equal(contextCall.options.alpha, true);
  assert.equal(contextCall.options.premultipliedAlpha, true);
});

test("bootstrap GLB loader extracts skin attributes and evaluates animation joint matrices", async () => {
  const env = createContext({
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  const scene = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/rig.glb");

  assert.equal(scene.objects.length, 1);
  assert.equal(scene.skins.length, 1);
  assert.equal(scene.nodes.length, 3);
  assert.equal(scene.animations.length, 1);

  const object = scene.objects[0];
  assert.equal(object.skin, scene.skins[0]);
  assert.equal(object.skinIndex, 0);
  assert.equal(object.vertices.count, 3);
  assert.equal(object.vertices.positions[0], 0);
  assert.equal(object.vertices.positions[1], 1);
  assert.deepEqual(Array.from(object.vertices.weights.slice(0, 8)), [1, 0, 0, 0, 0.75, 0.25, 0, 0]);
  assert.deepEqual(Array.from(object.vertices.joints.slice(0, 4)), [0, 1, 0, 0]);
  assert.equal(scene.animations[0].channels[0].targetID, 2);

  const animationApi = env.context.__gosx_scene3d_animation_api;
  const mixer = animationApi.createMixer();
  mixer.addClip(scene.animations[0].name, scene.animations[0]);
  mixer.play("bend", { loop: false, fadeIn: 0 });

  const animatedTransforms = new env.context.Map();
  mixer.update(0.5, function(targetNode, property, value) {
    animatedTransforms.set(targetNode, {
      [property]: Array.from(value),
    });
  });

  const nodeTransforms = animationApi.buildNodeTransforms(scene.nodes, animatedTransforms);
  const jointMatrices = animationApi.computeJointMatrices(scene.skins[0], nodeTransforms);
  assert.equal(jointMatrices.length, 32);
  assert.ok(Math.abs(jointMatrices[16 + 13] - 0.25) < 0.00001);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap starts Scene3D GLB model animation playback from model props", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-skinned-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-skinned",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-skinned-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [
              {
                id: "rig",
                src: "/models/rig.glb",
                animation: "bend",
                loop: true,
              },
            ],
          },
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/rig.glb"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(raf.count(), 1);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap uploads skinned GLB joint matrices through WebGL PBR", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-skinned-webgl-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/rig.glb": {
        bytes: buildSkinnedGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-skinned-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-skinned-webgl-root",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            models: [
              {
                id: "rig",
                src: "/models/rig.glb",
                animation: "bend",
                loop: true,
              },
            ],
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl.ops.some((entry) => entry[0] === "getAttribLocation" && entry[1] === "a_joints"));
  assert.ok(gl.ops.some((entry) => entry[0] === "getAttribLocation" && entry[1] === "a_weights"));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_hasSkin" && entry[2] === 1));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniformMatrix4fv" && entry[1] === "u_modelMatrix" && entry[3] === 16));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniformMatrix4fv" && entry[1] === "u_jointMatrices[0]" && entry[3] === 32));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[1] === 7 && entry[2] === 4));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[1] === 8 && entry[2] === 4));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap allocates Scene3D texture units without CSM and IBL collisions", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  assert.equal(typeof api.allocateTextureUnits, "function");

  const twoShadowLayout = api.allocateTextureUnits({ shadowCount: 2, ibl: true, maxUnits: 16 });
  assert.deepEqual(Array.from(twoShadowLayout.shadows), [5, 6]);
  assert.deepEqual({ ...twoShadowLayout.ibl }, { irradiance: 7, radiance: 8, brdfLUT: 9 });

  const defaultLayout = api.allocateTextureUnits({ shadowCount: 2, ibl: true });
  assert.deepEqual(Array.from(defaultLayout.shadows), [5, 6]);
  assert.deepEqual({ ...defaultLayout.ibl }, { irradiance: 7, radiance: 8, brdfLUT: 9 });

  const csmLayout = api.allocateTextureUnits({ shadowCount: 4, ibl: true, maxUnits: 16 });
  assert.deepEqual(Array.from(csmLayout.shadows), [5, 6, 7, 8]);
  assert.deepEqual({ ...csmLayout.ibl }, { irradiance: 9, radiance: 10, brdfLUT: 11 });

  const constrained = api.allocateTextureUnits({ shadowCount: 6, ibl: true, maxUnits: 10 });
  assert.deepEqual(Array.from(constrained.shadows), [5, 6]);
  assert.deepEqual({ ...constrained.ibl }, { irradiance: 7, radiance: 8, brdfLUT: 9 });
  assert.equal(constrained.warnings.length > 0, true);
});

test("bootstrap resolves Scene3D shadow and IBL resource budgets", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  const budget = api.resolveTextureMemoryBudget({
    shadowCount: 4,
    shadowSize: 1024,
    ibl: true,
  });

  assert.equal(budget.totalBytes <= 26 * 1024 * 1024, true);
  assert.equal(budget.ibl, true);
  assert.equal(budget.iblProfile.sourceFaceSize <= 256, true);
  assert.equal(budget.warnings.some((msg) => msg.includes("IBL profile downscaled")), true);

  const halfFloat = api.resolveIBLRenderTargetMode({
    getExtension(name) {
      return name === "EXT_color_buffer_half_float" ? { name } : null;
    },
  });
  assert.equal(halfFloat.mode, "half-float");

  const fallback = api.resolveIBLRenderTargetMode({ getExtension() { return null; } }, { lowPower: true });
  assert.equal(fallback.mode, "ldr-fallback");
  assert.equal(fallback.profile.sourceFaceSize, 128);

  const disabled = api.resolveIBLRenderTargetMode({ getExtension() { return null; } }, { allowLDRFallback: false });
  assert.equal(disabled.mode, "disabled");

  const rawHDR = Buffer.concat([
    Buffer.from("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n", "ascii"),
    Buffer.from([128, 64, 32, 129]),
  ]);
  const parsed = api.parseRadianceHDR(rawHDR.buffer.slice(rawHDR.byteOffset, rawHDR.byteOffset + rawHDR.byteLength));
  assert.equal(parsed.width, 1);
  assert.equal(parsed.height, 1);
  assert.ok(Math.abs(parsed.data[0] - 1) < 0.00001);
  assert.ok(Math.abs(parsed.data[1] - 0.5) < 0.00001);
  assert.ok(Math.abs(parsed.data[2] - 0.25) < 0.00001);
});

test("bootstrap hashes shadow passes with cascade-sensitive inputs", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  assert.equal(typeof api.shadowPassHash, "function");

  const matrix = new Float32Array(16);
  matrix[0] = 1;
  matrix[5] = 1;
  matrix[10] = 1;
  matrix[15] = 1;
  const casters = [
    { castShadow: true, vertexOffset: 0, vertexCount: 6, depthNear: 1, depthFar: 3 },
  ];
  const base = api.shadowPassHash(matrix, casters, {
    cascadeIndex: 0,
    splitNear: 0.1,
    splitFar: 10,
    shadowSize: 1024,
  });

  assert.notEqual(base, api.shadowPassHash(matrix, casters, {
    cascadeIndex: 1,
    splitNear: 0.1,
    splitFar: 10,
    shadowSize: 1024,
  }));
  assert.notEqual(base, api.shadowPassHash(matrix, casters, {
    cascadeIndex: 0,
    splitNear: 0.1,
    splitFar: 25,
    shadowSize: 1024,
  }));
  assert.notEqual(base, api.shadowPassHash(matrix, casters, {
    cascadeIndex: 0,
    splitNear: 0.1,
    splitFar: 10,
    shadowSize: 512,
  }));
});

test("bootstrap computes CSM cascade splits blending uniform and log", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  assert.equal(typeof api.computeCascadeSplits, "function");

  const uniform = api.computeCascadeSplits(1, 101, 4, 0);
  // Uniform scheme: splits at 26, 51, 76, 101.
  assert.ok(Math.abs(uniform[0] - 26) < 0.001, "uniform[0]=" + uniform[0]);
  assert.ok(Math.abs(uniform[3] - 101) < 0.001, "uniform[3]=" + uniform[3]);

  const log = api.computeCascadeSplits(1, 100, 4, 1);
  // Log scheme over [1,100] with 4 splits: 100^(1/4) ≈ 3.162 factor.
  assert.ok(Math.abs(log[0] - 3.162) < 0.01);
  assert.ok(Math.abs(log[3] - 100) < 0.001);

  const practical = api.computeCascadeSplits(1, 100, 4, 0.5);
  // Last split always equals far regardless of lambda.
  assert.ok(Math.abs(practical[3] - 100) < 0.001);
  // Practical splits should be monotonically increasing.
  for (let i = 1; i < practical.length; i++) {
    assert.ok(practical[i] > practical[i - 1], "splits must be increasing");
  }

  // Single cascade returns just far.
  const one = api.computeCascadeSplits(1, 50, 1, 0.5);
  assert.equal(one.length, 1);
  assert.ok(Math.abs(one[0] - 50) < 0.001);
});

test("bootstrap fits light-space ortho around a known frustum", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  assert.equal(typeof api.fitLightSpaceOrtho, "function");

  // Identity view: camera at origin, looking down -Z. Near=1, Far=10, fov=90, aspect=1
  // gives a symmetric frustum. Corners lie in world space since view is identity.
  const identity = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);
  const corners = api.frustumSubCorners(identity, 90, 1, 1, 10);
  assert.equal(corners.length, 24);
  // Near corners at z = -1, far at z = -10 (camera looks -Z).
  assert.ok(Math.abs(corners[2] - -1) < 0.001);    // near TL z
  assert.ok(Math.abs(corners[14] - -10) < 0.001);  // far TL z
  // Near half-extent at z=1, fov 90, aspect 1: tan(45)=1 → ±1 in x and y.
  assert.ok(Math.abs(Math.abs(corners[0]) - 1) < 0.001);
  assert.ok(Math.abs(Math.abs(corners[1]) - 1) < 0.001);

  // Fit a light looking straight down (light dir = (0,-1,0)) — the ortho
  // matrix should transform any world point inside the frustum to NDC within
  // [-1,1]^3.
  const lightMatrix = api.fitLightSpaceOrtho([0, -1, 0], corners, 0);
  // Centroid of the 8 corners — guaranteed to be inside the fit box, so
  // transformed NDC should be ~origin (within floating-point tolerance).
  let cx = 0, cy = 0, cz = 0;
  for (let i = 0; i < 8; i++) {
    cx += corners[i * 3];
    cy += corners[i * 3 + 1];
    cz += corners[i * 3 + 2];
  }
  cx /= 8; cy /= 8; cz /= 8;

  // Apply mat4 (column-major) * vec4(cx, cy, cz, 1).
  const w = lightMatrix[3] * cx + lightMatrix[7] * cy + lightMatrix[11] * cz + lightMatrix[15];
  const x = (lightMatrix[0] * cx + lightMatrix[4] * cy + lightMatrix[8] * cz + lightMatrix[12]) / w;
  const y = (lightMatrix[1] * cx + lightMatrix[5] * cy + lightMatrix[9] * cz + lightMatrix[13]) / w;
  const z = (lightMatrix[2] * cx + lightMatrix[6] * cy + lightMatrix[10] * cz + lightMatrix[14]) / w;
  assert.ok(Math.abs(x) < 0.2, "centroid x in NDC: " + x);
  assert.ok(Math.abs(y) < 0.2, "centroid y in NDC: " + y);
  assert.ok(z >= -1.01 && z <= 1.01, "centroid z in NDC [-1,1]: " + z);

  // All 8 corners should transform into NDC cube [-1,1]^3.
  for (let i = 0; i < 8; i++) {
    const px = corners[i * 3];
    const py = corners[i * 3 + 1];
    const pz = corners[i * 3 + 2];
    const ww = lightMatrix[3] * px + lightMatrix[7] * py + lightMatrix[11] * pz + lightMatrix[15];
    const nx = (lightMatrix[0] * px + lightMatrix[4] * py + lightMatrix[8] * pz + lightMatrix[12]) / ww;
    const ny = (lightMatrix[1] * px + lightMatrix[5] * py + lightMatrix[9] * pz + lightMatrix[13]) / ww;
    const nz = (lightMatrix[2] * px + lightMatrix[6] * py + lightMatrix[10] * pz + lightMatrix[14]) / ww;
    assert.ok(nx >= -1.01 && nx <= 1.01, "corner " + i + " x in NDC: " + nx);
    assert.ok(ny >= -1.01 && ny <= 1.01, "corner " + i + " y in NDC: " + ny);
    assert.ok(nz >= -1.01 && nz <= 1.01, "corner " + i + " z in NDC: " + nz);
  }
});

test("bootstrap snaps CSM light-space ortho to shadow texels", () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const api = env.context.window.__gosx_scene3d_resource_api;
  const identity = new Float32Array([
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 1, 0,
    0, 0, 0, 1,
  ]);
  const corners = api.frustumSubCorners(identity, 90, 1, 1, 10);
  const shifted = new Float32Array(corners);
  for (let i = 0; i < 8; i++) {
    shifted[i * 3] += 0.001;
  }

  const unsnappedA = api.fitLightSpaceOrtho([0, -1, 0], corners, 0);
  const unsnappedB = api.fitLightSpaceOrtho([0, -1, 0], shifted, 0);
  const snappedA = api.fitLightSpaceOrtho([0, -1, 0], corners, 0, 1024);
  const snappedB = api.fitLightSpaceOrtho([0, -1, 0], shifted, 0, 1024);

  let unsnappedDiff = 0;
  let snappedDiff = 0;
  for (let i = 0; i < 16; i++) {
    unsnappedDiff += Math.abs(unsnappedA[i] - unsnappedB[i]);
    snappedDiff += Math.abs(snappedA[i] - snappedB[i]);
  }
  assert.ok(unsnappedDiff > 0.00001, "unsnapped matrix should move with sub-texel camera shifts: " + unsnappedDiff);
  assert.ok(snappedDiff < 0.000001, "snapped matrix should stay stable below one texel: " + snappedDiff + " vs " + unsnappedDiff);
});

test("bootstrap binds Scene3D environment maps for WebGL PBR", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-envmap-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-envmap",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-envmap-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            environment: {
              envMap: "/hdri/studio.png",
              envIntensity: 1.25,
              envRotation: 0.5,
            },
            scene: {
              environment: {
                envMap: "/hdri/studio.png",
                envIntensity: 1.25,
                envRotation: 0.5,
              },
              objects: [
                {
                  id: "chrome-ball",
                  kind: "sphere",
                  radius: 1,
                  materialKind: "pbr",
                  color: "#ffffff",
                  metalness: 1,
                  roughness: 0.15,
                  vertices: {
                    count: 3,
                    positions: [0, 1, 0, -1, -1, 0, 1, -1, 0],
                    normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
                    uvs: [0.5, 1, 0, 0, 1, 0],
                  },
                },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.imageLoads.includes("/hdri/studio.png"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_hasEnvMap" && entry[2] === 1));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_envMap" && entry[2] === 7));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1f" && entry[1] === "u_envIntensity" && entry[2] === 1.25));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1f" && entry[1] === "u_envRotation" && entry[2] === 0.5));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap keeps Scene3D CSM shadow units ahead of IBL units", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-csm-ibl-root";

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-csm-ibl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-csm-ibl-root",
          props: {
            width: 640,
            height: 360,
            camera: { x: 0, y: 0, z: 6, near: 0.1, far: 100, fov: 72 },
            environment: {
              envMap: "/hdri/studio.png",
              envIntensity: 1,
            },
            scene: {
              lights: [
                {
                  id: "sun",
                  kind: "directional",
                  castShadow: true,
                  shadowCascades: 4,
                  shadowSize: 256,
                  shadowSoftness: 0.05,
                  directionX: 0.2,
                  directionY: -1,
                  directionZ: -0.35,
                },
              ],
              objects: [
                {
                  id: "shadow-triangle",
                  kind: "gltf-mesh",
                  materialKind: "pbr",
                  castShadow: true,
                  receiveShadow: true,
                  vertices: {
                    count: 3,
                    positions: [0, 1, 0, -1, -1, 0, 1, -1, 0],
                    normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
                    uvs: [0.5, 1, 0, 0, 1, 0],
                  },
                },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.imageLoads.includes("/hdri/studio.png"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  assert.equal(gl.ops.filter((entry) => entry[0] === "createFramebuffer").length, 4);
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowCascades0" && entry[2] === 4));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowMap0_0" && entry[2] === 5));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowMap0_1" && entry[2] === 6));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowMap0_2" && entry[2] === 7));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_shadowMap0_3" && entry[2] === 8));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_envMap" && entry[2] === 9));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1fv" && entry[1] === "u_shadowCascadeSplits0" && entry[2] === 4));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap fetches Radiance HDR Scene3D environment maps for WebGL PBR", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-hdr-envmap-root";
  const rawHDR = Buffer.concat([
    Buffer.from("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n", "ascii"),
    Buffer.from([128, 64, 32, 129]),
  ]);

  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/hdri/studio.hdr": {
        bytes: Array.from(rawHDR),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-hdr-envmap",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-hdr-envmap-root",
          props: {
            width: 320,
            height: 180,
            environment: {
              envMap: "/hdri/studio.hdr",
              envIntensity: 0.75,
            },
            objects: [
              {
                id: "hdr-triangle",
                kind: "gltf-mesh",
                materialKind: "pbr",
                vertices: {
                  count: 3,
                  positions: [0, 1, 0, -1, -1, 0, 1, -1, 0],
                  normals: [0, 0, 1, 0, 0, 1, 0, 0, 1],
                  uvs: [0.5, 1, 0, 0, 1, 0],
                },
              },
            ],
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/hdri/studio.hdr"), true);
  assert.equal(env.imageLoads.includes("/hdri/studio.hdr"), false);
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl.ops.filter((entry) => entry[0] === "texImage2D").length >= 2);
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_envMap" && entry[2] === 7));
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap keeps GLB Scene3D assets visible on canvas fallback", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-glb-canvas-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/models/runner.glb": {
        bytes: buildMinimalGLBBytes(),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-glb-canvas",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-glb-canvas-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            models: [
              {
                id: "runner",
                src: "/models/runner.glb",
                x: 0.35,
                y: 0.1,
                z: -0.4,
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((call) => call.url === "/models/runner.glb"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  const ctx2d = mount.children[0].getContext("2d");
  const lineCount = ctx2d.ops.filter((entry) => entry[0] === "lineTo").length;
  assert.ok(lineCount > 22);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders model-relative Scene3D textures without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-model-texture-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/models/panel.gosx3d.json": {
        text: JSON.stringify({
          objects: [
            {
              id: "panel",
              kind: "plane",
              width: 1.55,
              height: 1.02,
              x: 0,
              y: 0.6,
              z: 0.4,
              material: {
                kind: "flat",
                color: "#f7fbff",
                texture: "./paper-card.png",
                wireframe: false,
              },
            },
          ],
        }),
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-model-texture",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-texture-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
            models: [
              {
                id: "panel-asset",
                src: "/models/panel.gosx3d.json",
                x: 0.25,
                y: 0.1,
                z: -0.4,
                rotationY: 0.12,
              },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  console.log("scene-logs", JSON.stringify(env.consoleLogs.log));
  assert.equal(env.fetchCalls.some((call) => call.url === "/models/panel.gosx3d.json"), true);
  assert.equal(env.imageLoads.includes("http://localhost:3000/models/paper-card.png"), true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl");
  assert.ok(gl.ops.some((entry) => entry[0] === "createTexture"));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1i" && entry[1] === "u_texture" && entry[2] === 0));
  assert.ok(gl.ops.some((entry) => entry[0] === "vertexAttribPointer" && entry[1] === 3 && entry[2] === 2));
  assert.ok(gl.ops.some((entry) => entry[0] === "texImage2D" && entry[2] === 9));
  assert.ok(gl.ops.some((entry) => entry[0] === "texImage2D" && entry[2] === 6));
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === gl.TRIANGLES && entry[3] === 6));
  console.log("texture-draws", JSON.stringify(gl.ops.filter((entry) => entry[0] === "drawArrays")));
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders declarative Scene3D sprite billboards without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-sprite-root";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-sprite",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-sprite-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            scene: {
              objects: [
                {
                  id: "hero",
                  kind: "box",
                  width: 1.4,
                  height: 1.1,
                  depth: 0.9,
                  x: 0,
                  y: 0.2,
                  z: 0.3,
                  color: "#8de1ff",
                },
              ],
              sprites: [
                {
                  id: "card",
                  src: "/paper-card.png",
                  x: 0.15,
                  y: 1.3,
                  z: 0.5,
                  width: 1.55,
                  height: 1.02,
                  scale: 1,
                  opacity: 0.94,
                  priority: 3,
                  anchorX: 0.5,
                  anchorY: 0.5,
                  fit: "cover",
                  occlude: true,
                },
              ],
            },
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  assert.equal(labelLayer.getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(labelLayer.children.length, 1);
  const sprite = labelLayer.children[0];
  assert.equal(sprite.getAttribute("data-gosx-scene-sprite"), "card");
  assert.equal(sprite.getAttribute("data-gosx-scene-sprite-fit"), "cover");
  assert.equal(sprite.getAttribute("data-gosx-scene-sprite-occlude"), "true");
  assert.equal(sprite.firstChild.tagName, "IMG");
  assert.equal(sprite.firstChild.getAttribute("src"), "/paper-card.png");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders declarative Scene3D HTML overlays on the canvas backend", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-html-root";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-html",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-html-root",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            autoRotate: false,
            scene: {
              objects: [
                { id: "hero", kind: "box", width: 1.4, height: 1.1, depth: 0.9, x: 0, y: 0, z: 0.3, color: "#8de1ff" },
              ],
              html: [
                {
                  id: "hud-card",
                  className: "hud-card",
                  html: '<section class="hud"><strong>HTML</strong> <span>scene</span></section>',
                  x: 0,
                  y: 1.2,
                  z: 0.2,
                  width: 1.6,
                  height: 0.8,
                  pointerEvents: "auto",
                  priority: 4,
                },
              ],
            },
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
          capabilities: ["canvas"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  const html = labelLayer.children.find((child) => child.getAttribute("data-gosx-scene-html") === "hud-card");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(labelLayer.getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(labelLayer.getAttribute("aria-hidden"), "false");
  assert.ok(html);
  assert.equal(html.getAttribute("class"), "gosx-scene-html hud-card");
  assert.equal(html.getAttribute("data-gosx-scene-html-pointer-events"), "auto");
  assert.equal(html.getAttribute("aria-hidden"), "false");
  assert.equal(html.innerHTML, '<section class="hud"><strong>HTML</strong> <span>scene</span></section>');
  assert.equal(html.textContent, "HTML scene");
  assert.equal(html.style["--gosx-scene-html-pointer-events"], "auto");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap emits declarative Scene3D pick signals without authored JS", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pick-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-pick-program.json": { text: '{"name":"ScenePick"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-pick",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-pick-root",
          runtime: "shared",
          programRef: "/scene-pick-program.json",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            pickSignalNamespace: "$scene.pick",
            eventSignalNamespace: "$scene.event",
            camera: { x: 0, y: 0, z: 6, fov: 72 },
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -2.4, -1.5, 0.1, 2.4, -1.5, 0.1,
        -0.8, 0.2, 0.5, 0.7, 0.9, 1.1,
      ],
      worldColors: [
        0.25, 0.33, 0.41, 1, 0.25, 0.33, 0.41, 1,
        0.78, 0.92, 1, 1, 0.78, 0.92, 1, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        {
          id: "floor",
          kind: "plane",
          pickable: false,
          materialIndex: 0,
          vertexOffset: 0,
          vertexCount: 2,
          static: true,
          bounds: { minX: -2.4, minY: -1.5, minZ: 0.1, maxX: 2.4, maxY: -1.5, maxZ: 0.1 },
        },
        {
          id: "shape",
          kind: "box",
          pickable: true,
          materialIndex: 1,
          vertexOffset: 2,
          vertexCount: 2,
          static: false,
          bounds: { minX: -0.8, minY: 0.2, minZ: 0.5, maxX: 0.7, maxY: 0.9, maxZ: 1.1 },
        },
      ],
      objectCount: 2,
    }),
  });
  const interactionEvents = [];
  env.document.addEventListener("gosx:engine:scene-interaction", (event) => interactionEvents.push(event.detail));

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const canvas = mount.children[0];
  assert.equal(mount.getAttribute("data-gosx-scene3d-pick-signals"), "$scene.pick");
  assert.equal(mount.getAttribute("data-gosx-scene3d-event-signals"), "$scene.event");

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  await flushAsyncWork();

  let batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.hovered"], true);
  assert.equal(batch["$scene.pick.hoverIndex"], 1);
  assert.equal(batch["$scene.pick.hoverID"], "shape");
  assert.equal(batch["$scene.pick.selected"], false);
  assert.equal(batch["$scene.event.revision"], 1);
  assert.equal(batch["$scene.event.type"], "hover");
  assert.equal(batch["$scene.event.targetIndex"], 1);
  assert.equal(batch["$scene.event.targetID"], "shape");
  assert.equal(batch["$scene.event.targetKind"], "box");
  assert.equal(batch["$scene.event.hovered"], true);
  assert.equal(batch["$scene.event.hoverKind"], "box");
  assert.equal(batch["$scene.event.object.shape.hovered"], true);
  assert.deepEqual(JSON.parse(JSON.stringify(interactionEvents[0])), {
    engineID: "gosx-engine-pick",
    component: "GoSXScene3D",
    detail: {
      type: "hover",
      revision: 1,
      targetIndex: 1,
      targetID: "shape",
      targetKind: "box",
      hovered: true,
      hoverIndex: 1,
      hoverID: "shape",
      hoverKind: "box",
      down: false,
      downIndex: -1,
      downID: "",
      downKind: "",
      selected: false,
      selectedIndex: -1,
      selectedID: "",
      selectedKind: "",
      clickCount: 0,
      pointerX: 320,
      pointerY: 160,
    },
  });
  const hoverBatchCount = env.inputBatchCalls.length;

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.inputBatchCalls.length, hoverBatchCount);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.down"], true);
  assert.equal(batch["$scene.pick.downID"], "shape");
  assert.equal(batch["$scene.event.type"], "down");
  assert.equal(batch["$scene.event.down"], true);
  assert.equal(batch["$scene.event.downID"], "shape");
  assert.equal(batch["$scene.event.object.shape.down"], true);

  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 4,
    clientX: 320,
    clientY: 160,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.down"], false);
  assert.equal(batch["$scene.pick.selected"], true);
  assert.equal(batch["$scene.pick.selectedIndex"], 1);
  assert.equal(batch["$scene.pick.selectedID"], "shape");
  assert.equal(batch["$scene.pick.clickCount"], 1);
  assert.equal(batch["$scene.event.type"], "select");
  assert.equal(batch["$scene.event.selected"], true);
  assert.equal(batch["$scene.event.selectedID"], "shape");
  assert.equal(batch["$scene.event.object.shape.down"], false);
  assert.equal(batch["$scene.event.object.shape.selected"], true);
  assert.equal(batch["$scene.event.object.shape.clickCount"], 1);

  canvas.dispatchEvent({
    type: "pointermove",
    button: 0,
    pointerId: 5,
    clientX: 48,
    clientY: 332,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.hovered"], false);
  assert.equal(batch["$scene.pick.hoverIndex"], -1);
  assert.equal(batch["$scene.pick.hoverID"], "");
  assert.equal(batch["$scene.pick.selectedID"], "shape");
  assert.equal(batch["$scene.event.type"], "leave");
  assert.equal(batch["$scene.event.hovered"], false);
  assert.equal(batch["$scene.event.object.shape.hovered"], false);

  canvas.dispatchEvent({
    type: "pointerdown",
    button: 0,
    pointerId: 5,
    clientX: 48,
    clientY: 332,
    preventDefault() {},
    stopPropagation() {},
  });
  canvas.dispatchEvent({
    type: "pointerup",
    button: 0,
    pointerId: 5,
    clientX: 48,
    clientY: 332,
    preventDefault() {},
    stopPropagation() {},
  });
  await flushAsyncWork();

  batch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(batch["$scene.pick.selected"], false);
  assert.equal(batch["$scene.pick.selectedIndex"], -1);
  assert.equal(batch["$scene.pick.selectedID"], "");
  assert.equal(batch["$scene.pick.clickCount"], 1);
  assert.equal(batch["$scene.event.type"], "deselect");
  assert.equal(batch["$scene.event.selected"], false);
  assert.equal(batch["$scene.event.object.shape.selected"], false);
  assert.equal(interactionEvents.at(-1).detail.type, "deselect");
});

test("bootstrap stamps and validates Scene3D IR bundles", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(api.SCENE_IR_VERSION, 1);
  assert.equal(typeof api.validateSceneIR, "function");

  const bundle = api.createSceneRenderBundle(
    320,
    180,
    "#08151f",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [],
    [],
    [],
    [],
    [],
    { ambientColor: "#ffffff", ambientIntensity: 0.1 },
    0,
    [],
    [],
    [],
    [],
    921600,
  );
  assert.equal(bundle.bundleVersion, api.SCENE_RENDER_BUNDLE_VERSION);
  assert.equal(bundle.postFXMaxPixels, 921600);
  assert.equal(JSON.stringify(api.validateSceneIR(bundle)), JSON.stringify({ valid: true, errors: [] }));

  const invalid = api.validateSceneIR({
    version: 1,
    camera: { near: 2, far: 1 },
    environment: {},
    nodes: [{ kind: "points", points: { count: -1 } }],
  });
  assert.equal(invalid.valid, false);
  assert.ok(invalid.errors.some((entry) => entry.includes("camera.far")));
  assert.ok(invalid.errors.some((entry) => entry.includes("points.count")));

  const emptyCanonical = api.validateSceneIR({
    version: 1,
    camera: { near: 0.05, far: 128 },
    environment: { fogDensity: 0.001 },
    lights: [{ kind: "directional" }],
  });
  assert.equal(JSON.stringify(emptyCanonical), JSON.stringify({ valid: true, errors: [] }));

  const mismatched = api.validateSceneIR({
    version: 1,
    camera: { near: 0.05, far: 128 },
    nodes: [{ kind: "mesh", points: { count: 1 } }],
  });
  assert.equal(mismatched.valid, false);
  assert.ok(mismatched.errors.some((entry) => entry.includes(".mesh is required")));

  const negativeInstanced = api.validateSceneIR({
    version: 1,
    camera: { near: 0.05, far: 128 },
    nodes: [{ kind: "instanced-mesh", instancedMesh: { count: -1 } }],
  });
  assert.equal(negativeInstanced.valid, false);
  assert.ok(negativeInstanced.errors.some((entry) => entry.includes("instancedMesh.count")));
});

test("bootstrap prepares Scene3D pass plans and cached buffers through shared planner", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.prepareScene, "function");
  assert.equal(typeof api.sceneCachedBuffer, "function");

  const bundle = {
    bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
    camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    environment: {},
    materials: [
      { kind: "flat", opacity: 1, renderPass: "opaque" },
      { kind: "glass", opacity: 0.5, renderPass: "alpha" },
    ],
    meshObjects: [
      { id: "near", kind: "box", materialIndex: 1, vertexOffset: 0, vertexCount: 3, depthCenter: 4 },
      { id: "far", kind: "box", materialIndex: 1, vertexOffset: 3, vertexCount: 3, depthCenter: 8 },
      { id: "solid", kind: "box", materialIndex: 0, vertexOffset: 6, vertexCount: 3, depthCenter: 6 },
    ],
    objects: [],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(27),
    worldMeshNormals: new Float32Array(27),
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const prepared = api.prepareScene(bundle, bundle.camera, viewport, null);
  assert.equal(JSON.stringify(prepared.pbrPasses.opaque.map((entry) => entry.id)), JSON.stringify(["solid"]));
  assert.equal(JSON.stringify(prepared.pbrPasses.alpha.map((entry) => entry.id)), JSON.stringify(["far", "near"]));
  assert.equal(JSON.stringify(api.scenePreparedCommandSequence(prepared)), JSON.stringify([
    { op: "drawMesh", pass: "opaque", id: "solid", kind: "box", vertexOffset: 6, vertexCount: 3 },
    { op: "drawMesh", pass: "alpha", id: "far", kind: "box", vertexOffset: 3, vertexCount: 3 },
    { op: "drawMesh", pass: "alpha", id: "near", kind: "box", vertexOffset: 0, vertexCount: 3 },
  ]));
  assert.equal(prepared.shadowPassHash, api.prepareScene(bundle, bundle.camera, viewport, prepared).shadowPassHash);
  assert.equal(api.prepareScene(bundle, bundle.camera, viewport, prepared), prepared);

  bundle.points = [{ id: "stars", count: 5, color: "#ffffff", size: 1 }];
  const withPoints = api.prepareScene(bundle, bundle.camera, viewport, prepared);
  assert.notEqual(withPoints, prepared);
  assert.equal(api.scenePreparedCommandSequence(withPoints).at(-1).count, 5);
  bundle.points = [{ id: "stars", count: 8, color: "#5eead4", size: 1.5 }];
  const updatedPoints = api.prepareScene(bundle, bundle.camera, viewport, withPoints);
  assert.notEqual(updatedPoints, withPoints);
  assert.equal(api.scenePreparedCommandSequence(updatedPoints).at(-1).count, 8);

  bundle.environment = { fogColor: "#0b142a", fogDensity: 0.0003 };
  const withFog = api.prepareScene(bundle, bundle.camera, viewport, updatedPoints);
  assert.equal(withFog, updatedPoints);
  assert.equal(withFog.ir.environment.fogDensity, 0.0003);

  bundle.points = [{ id: "stars", count: 8, color: "#5eead4", size: 1.5, positions: [0, 0, 0, 1, 1, 1] }];
  const withPointGeometry = api.prepareScene(bundle, bundle.camera, viewport, withFog);
  assert.equal(withPointGeometry, withFog);
  assert.equal(withPointGeometry.ir.points[0].positions.length, 6);

  const cache = new WeakMap();
  const typed = new Float32Array([1, 2, 3]);
  let uploads = 0;
  const first = api.sceneCachedBuffer(cache, typed, () => ({ id: "buffer" }), () => { uploads += 1; });
  const second = api.sceneCachedBuffer(cache, typed, () => ({ id: "next" }), () => { uploads += 1; });
  assert.equal(first, second);
  assert.equal(uploads, 1);

  const owner = {};
  const small = new Float32Array([1]);
  const large = new Float32Array([1, 2, 3, 4]);
  const smallHandle = api.sceneCachedBuffer(
    owner,
    small,
    () => ({ id: "small", size: small.byteLength }),
    () => {},
    { slot: "gpuBuffer" }
  );
  const largeHandle = api.sceneCachedBuffer(
    owner,
    large,
    () => ({ id: "unused" }),
    (handle, data, state) => state.bytesChanged && data.byteLength > handle.size
      ? { id: "large", size: data.byteLength }
      : handle,
    { slot: "gpuBuffer" }
  );
  assert.equal(smallHandle.id, "small");
  assert.equal(largeHandle.id, "large");
  assert.equal(owner.gpuBuffer, largeHandle);
});

test("bootstrap resolves Scene3D CSS custom properties in the planner", async () => {
  let computedStyleCalls = 0;
  const env = createContext({
    getComputedStyle(element) {
      computedStyleCalls += 1;
      return element && element.computedStyle ? element.computedStyle : {};
    },
  });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const mount = new FakeElement("div", null);
  mount.computedStyle = {
    "--scene-core-color": "#5eead4",
    "--scene-core-roughness": "0.3",
    "--scene-ambient-intensity": "0.2",
    "--scene-filter": "bloom(threshold 0.8 intensity 1.1) vignette(intensity 0.5)",
  };
  const starsSentinel = new FakeElement("div", null);
  starsSentinel.computedStyle = {
    "--point-size": "2.5",
  };
  const sentinels = new Map([["stars", starsSentinel]]);
  const bundle = {
    bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
    camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    environment: { ambientIntensity: 0 },
    materials: [
      { name: "core", color: "var(--scene-core-color)", roughness: "var(--scene-core-roughness, 0.4)" },
    ],
    meshObjects: [
      { id: "hero", kind: "box", materialIndex: 0, vertexOffset: 0, vertexCount: 3, depthCenter: 4 },
    ],
    objects: [],
    points: [{ id: "stars", count: 3, color: "#ffffff", size: 1 }],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(9),
    worldMeshNormals: new Float32Array(9),
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const prepared = api.prepareScene(bundle, bundle.camera, viewport, null, { mount, sentinels, revision: 1 });
  const firstComputedStyleCalls = computedStyleCalls;

  assert.equal(prepared.ir.materials[0].color, "#5eead4");
  assert.equal(prepared.ir.materials[0].roughness, 0.3);
  assert.equal(prepared.ir.environment.ambientIntensity, 0.2);
  assert.equal(prepared.ir.points[0].size, 2.5);
  assert.equal(prepared.ir.postEffects.length, 2);
  assert.equal(JSON.stringify(prepared.ir.postEffects[0]), JSON.stringify({ kind: "bloom", threshold: 0.8, intensity: 1.1 }));

  const cached = api.prepareScene(bundle, bundle.camera, viewport, prepared, { mount, sentinels, revision: 1 });
  assert.equal(cached, prepared);
  assert.equal(computedStyleCalls, firstComputedStyleCalls);

  const cachedAgain = api.prepareScene(bundle, bundle.camera, viewport, cached, { mount, sentinels, revision: 1 });
  assert.equal(cachedAgain.ir.points[0], cached.ir.points[0]);
  assert.equal(computedStyleCalls, firstComputedStyleCalls);

  mount.computedStyle["--scene-core-color"] = "#1e3a8a";
  const staleRevision = api.prepareScene(bundle, bundle.camera, viewport, cachedAgain, { mount, sentinels, revision: 1 });
  assert.equal(staleRevision, cachedAgain);
  assert.equal(staleRevision.ir.materials[0].color, "#5eead4");
  assert.equal(computedStyleCalls, firstComputedStyleCalls);

  const updated = api.prepareScene(bundle, bundle.camera, viewport, staleRevision, { mount, sentinels, revision: 2 });
  assert.notEqual(updated, prepared);
  assert.equal(updated.ir.materials[0].color, "#1e3a8a");
  assert.ok(computedStyleCalls > firstComputedStyleCalls);
});

test("bootstrap keeps WebGPU Scene3D points on per-entry cached GPU buffers", () => {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16a-scene-webgpu.js"), "utf8");

  assert.match(source, /function ensurePointsUniformGPUBuffer\(owner, uniformData\)/);
  assert.match(source, /function ensurePointsParticleGPUBuffer\(entry, particleData\)/);
  assert.match(source, /sceneCachedBuffer\(owner,\s*typedArray/);
  assert.match(source, /ensurePointsUniformGPUBuffer\(entry,\s*puF\)/);
  assert.match(source, /ensurePointsParticleGPUBuffer\(entry,\s*particleData\)/);
  assert.doesNotMatch(source, /var\s+pointsUniformBuffer\s*=\s*device\.createBuffer/);
  assert.doesNotMatch(source, /device\.queue\.writeBuffer\(pointsUniformBuffer,\s*0/);
});

test("bootstrap bridges clamp01 into the WebGPU Scene3D sub-feature", () => {
  const prefix = fs.readFileSync(path.join(__dirname, "bootstrap-src", "26e-feature-scene3d-webgpu-prefix.js"), "utf8");
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.js"), "utf8");

  assert.match(prefix, /var clamp01 = sceneApi\.clamp01/);
  assert.match(prefix, /var SCENE_POST_TONE_MAPPING = sceneApi\.SCENE_POST_TONE_MAPPING/);
  assert.match(prefix, /var SCENE_POST_BLOOM = sceneApi\.SCENE_POST_BLOOM/);
  assert.match(prefix, /var SCENE_POST_VIGNETTE = sceneApi\.SCENE_POST_VIGNETTE/);
  assert.match(prefix, /var SCENE_POST_COLOR_GRADE = sceneApi\.SCENE_POST_COLOR_GRADE/);
  assert.match(core, /\n    clamp01,\n/);
  assert.match(core, /\n    SCENE_POST_TONE_MAPPING: "toneMapping",\n/);
  assert.match(core, /\n    SCENE_POST_BLOOM: "bloom",\n/);
  assert.match(core, /\n    SCENE_POST_VIGNETTE: "vignette",\n/);
  assert.match(core, /\n    SCENE_POST_COLOR_GRADE: "colorGrade",\n/);
});

test("bootstrap applies named Scene3D materials to point layers", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneStatePointsWithMaterials, "function");

  const state = api.createSceneState({
    scene: {
      materials: [
        { name: "core", color: "var(--galaxy-core-inner)", opacity: "var(--galaxy-core-opacity)" },
      ],
      points: [
        { id: "galaxy", count: 1, material: "core", color: "#ffffff", opacity: 0.1, blendMode: "additive" },
      ],
    },
  });
  const points = api.sceneStatePointsWithMaterials(state);
  const again = api.sceneStatePointsWithMaterials(state);

  assert.equal(points[0].color, "var(--galaxy-core-inner)");
  assert.equal(points[0].opacity, "var(--galaxy-core-opacity)");
  assert.equal(points[0].blendMode, "additive");
  assert.equal(again[0], points[0]);
});

test("bootstrap applies Scene3D live point buffers outside update tweens", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      points: [
        {
          id: "galaxy",
          count: 2,
          positions: [0, 0, 0, 1, 0, 0],
          sizes: [1, 1],
          colors: ["#000000", "#111111"],
          opacity: 0.5,
          live: ["galaxy:node:galaxy"],
          transition: { update: { duration: 1200, easing: "linear" } },
        },
      ],
    },
  });
  const entry = state.points[0];
  entry._cachedColors = new Float32Array([0, 0, 0, 1, 0.1, 0.1, 0.1, 1]);

  const changed = api.sceneApplyLiveEvent(state, "galaxy:node:galaxy", {
    colors: ["#ff0000", "#00ff00"],
  }, false, 10);

  assert.equal(changed, true);
  assert.deepEqual(entry.colors, ["#ff0000", "#00ff00"]);
  assert.equal(entry._cachedColors, null);
  assert.equal(state._transitions.length, 0);
});

test("bootstrap keeps Scene3D live point buffers out of scalar update transitions", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState({
    scene: {
      points: [
        {
          id: "galaxy",
          count: 2,
          positions: [0, 0, 0, 1, 0, 0],
          sizes: [1, 1],
          colors: ["#000000", "#111111"],
          opacity: 0.5,
          live: ["galaxy:node:galaxy"],
          transition: { update: { duration: 1200, easing: "linear" } },
        },
      ],
    },
  });
  const entry = state.points[0];
  entry._cachedColors = new Float32Array([0, 0, 0, 1, 0.1, 0.1, 0.1, 1]);
  const payload = {
    colors: ["#ff0000", "#00ff00"],
    opacity: 0.9,
  };

  const changed = api.sceneApplyLiveEvent(state, "galaxy:node:galaxy", payload, false, 10);

  assert.equal(changed, true);
  assert.deepEqual(entry.colors, ["#ff0000", "#00ff00"]);
  assert.equal(entry._cachedColors, null);
  assert.equal(entry.opacity, 0.5);
  assert.equal(Object.prototype.hasOwnProperty.call(payload, "__eventName"), false);
  assert.equal(state._transitions.length, 1);
  const transition = state._transitions[0];
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "colors"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "positions"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "sizes"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.target, "_cachedColors"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "colors"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "positions"), false);
  assert.equal(Object.prototype.hasOwnProperty.call(transition.delta, "sizes"), false);
  assert.equal(transition.delta.opacity.__from, 0.5);
  assert.equal(transition.delta.opacity.__to, 0.9);
  assert.equal(transition.delta.opacity.__key, "opacity");

  api.sceneAdvanceTransitions(state, 1210);
  assert.equal(entry.opacity, 0.9);
  assert.deepEqual(entry.colors, ["#ff0000", "#00ff00"]);
});

test("bootstrap keeps Scene3D CSS transition diagnostics opt-in", () => {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "15b-scene-planner.js"), "utf8");

  assert.match(source, /function sceneCSSDebugLog\(\)/);
  assert.match(source, /__gosx_scene3d_css_debug/);
  assert.doesNotMatch(source, /console\.log\("\[gosx:css-transition\]/);
});

test("bootstrap observes inherited root CSS var mutations for Scene3D", () => {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "20-scene-mount.js"), "utf8");

  assert.match(source, /observer\.observe\(document\.documentElement,\s*\{/);
  assert.match(source, /attributeOldValue:\s*true/);
  assert.match(source, /sceneCSSMutationShouldInvalidate\(records\)/);
  assert.match(source, /name\.indexOf\("--gosx-"\)\s*===\s*0/);
  assert.match(source, /sceneCSSTransitionWindowMillis\(document && document\.documentElement\)/);
});

test("bootstrap gates Scene3D viewport refreshes to viewport-shaped environment changes", () => {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "20-scene-mount.js"), "utf8");

  assert.match(source, /function sceneViewportEnvironmentSignature\(environment\)/);
  assert.match(source, /sceneNumber\(environment\.devicePixelRatio,\s*1\)/);
  assert.match(source, /Math\.round\(sceneNumber\(environment\.visualViewportHeight,\s*0\)\)/);
  assert.doesNotMatch(source, /environment\.visualViewportOffsetTop/);
  assert.match(source, /if \(environmentSignature === nextSignature\) \{\s*return;\s*\}/);
});

test("bootstrap skips redundant runtime style and attribute writes", () => {
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "00-textlayout.js"), "utf8");

  assert.match(source, /style\.getPropertyValue\(name\) === next/);
  assert.match(source, /style\.setProperty\(name,\s*next\)/);
  assert.match(source, /element\.getAttribute\(name\) === next/);
  assert.match(source, /element\.setAttribute\(name,\s*next\)/);
});

test("bootstrap keeps WebGL and WebGPU Scene3D command logs in parity", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  assert.equal(typeof api.sceneWebGLCommandSequence, "function");
  assert.equal(typeof api.sceneWebGPUCommandSequence, "function");

  const bundle = {
    bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
    camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    environment: {},
    materials: [
      { kind: "flat", opacity: 1, renderPass: "opaque" },
      { kind: "glass", opacity: 0.5, renderPass: "alpha" },
      { kind: "glow", opacity: 0.7, renderPass: "additive" },
    ],
    meshObjects: [
      { id: "near", kind: "box", materialIndex: 1, vertexOffset: 0, vertexCount: 3, depthCenter: 4 },
      { id: "far", kind: "box", materialIndex: 1, vertexOffset: 3, vertexCount: 3, depthCenter: 8 },
      { id: "solid", kind: "sphere", materialIndex: 0, vertexOffset: 6, vertexCount: 3, depthCenter: 6 },
      { id: "spark", kind: "plane", materialIndex: 2, vertexOffset: 9, vertexCount: 3, depthCenter: 7 },
    ],
    objects: [],
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldMeshPositions: new Float32Array(36),
    worldMeshNormals: new Float32Array(36),
    points: [
      { id: "stars", count: 5 },
    ],
    instancedMeshes: [
      { id: "debris", kind: "box", count: 3 },
    ],
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const expected = api.scenePreparedCommandSequence(api.prepareScene(bundle, bundle.camera, viewport, null));

  assert.deepEqual(api.sceneWebGLCommandSequence(bundle, viewport), expected);
  assert.deepEqual(api.sceneWebGPUCommandSequence(bundle, viewport), expected);
  assert.deepEqual(api.sceneWebGPUCommandSequence(bundle, viewport), api.sceneWebGLCommandSequence(bundle, viewport));
});

test("bootstrap registers and selects Scene3D backends through registry", async () => {
  const env = createContext({});
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const registry = env.context.__gosx_scene3d_api.sceneBackendRegistry;
  assert.equal(typeof registry.register, "function");
  assert.ok(registry.list().some((entry) => entry.kind === "webgl"));
  assert.ok(registry.list().some((entry) => entry.kind === "canvas2d"));

  const custom = registry.register("foo", {
    capabilities: ["foo"],
    create: () => ({ kind: "foo", render() {}, dispose() {} }),
  });
  assert.equal(registry.select({ foo: true, canvas2d: false, canvas: false, webgl: false, webgpu: false }).kind, custom.kind);
  registry.dispose("foo");
  assert.equal(registry.list().some((entry) => entry.kind === "foo"), false);
  assert.equal(registry.select({ canvas: false, canvas2d: false, webgl: false, webgl2: false, webgpu: false }), null);
});

test("selective Scene3D bootstrap prefers WebGPU before first renderer selection", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgpu-default";
  const env = createContext({
    elements: [mount],
    enableWebGPU: true,
    enableWebGL2: true,
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": {
        text: bootstrapFeatureEnginesSource,
      },
      "/gosx/bootstrap-feature-scene3d-webgpu.js": {
        text: `
          window.__gosx_scene3d_webgpu_api = {
            createRenderer: function() {
              return {
                kind: "webgpu",
                render: function() {},
                dispose: function() {}
              };
            }
          };
          window.__gosx_scene3d_webgpu_loaded = true;
        `,
      },
    },
    manifest: {
      runtime: { path: "/gosx/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-webgpu-default",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgpu-default",
          jsExport: "GoSXScene3D",
          props: {
            width: 360,
            height: 220,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1, height: 1, depth: 1, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(bootstrapFeatureScene3DSource, env.context, "bootstrap-feature-scene3d.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "prefer");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgpu", JSON.stringify({
    fetchCalls: env.fetchCalls,
    hasWebGPUAPI: Boolean(env.context.__gosx_scene3d_webgpu_api),
    webgpuProbe: env.context.__gosx_scene3d_webgpu_probe && env.context.__gosx_scene3d_webgpu_probe(),
    backends: env.context.__gosx_scene3d_api.sceneBackendRegistry.list().map((entry) => entry.kind),
  }));
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
  assert.equal(
    env.fetchCalls.some((call) => call.url === "/gosx/bootstrap-feature-scene3d-webgpu.js"),
    true,
  );
  assert.equal((mount.children[0].contextCalls || []).some((call) => call.kind === "webgl" || call.kind === "webgl2"), false);
});

test("bootstrap releases replaced static point WebGL buffers on live updates", async () => {
  const env = createContext({
    enableWebGL2: true,
    disableCanvas2D: true,
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const registry = api.sceneBackendRegistry;
  const backend = registry.select({
    webgl: true,
    webgl2: true,
    webgpu: false,
    canvas: false,
    canvas2d: false,
  });
  const canvas = env.document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 180;
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  assert.equal(renderer && renderer.type, "webgl-pbr");

  const point = {
    id: "stars",
    count: 2,
    positions: [0, 0, 0, 1, 0, 0],
    sizes: [1, 1],
    colors: ["#ffffff", "#88ccff"],
    style: "focus",
    size: 1,
    opacity: 1,
    blendMode: "additive",
    depthWrite: false,
    attenuation: true,
  };
  const bundle = {
    bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
    background: "#000000",
    camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    environment: {},
    points: [point],
    instancedMeshes: [],
    computeParticles: [],
    objects: [],
    meshObjects: [],
    materials: [],
    labels: [],
    sprites: [],
    lights: [],
    positions: new Float32Array(0),
    colors: new Float32Array(0),
    worldPositions: new Float32Array(0),
    worldColors: new Float32Array(0),
    worldLineWidths: new Float32Array(0),
    worldMeshPositions: new Float32Array(0),
    worldMeshColors: new Float32Array(0),
    worldMeshNormals: new Float32Array(0),
    worldMeshUVs: new Float32Array(0),
    worldMeshTangents: new Float32Array(0),
    vertexCount: 0,
    worldVertexCount: 0,
    postEffects: [],
  };
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  renderer.render(bundle, viewport);
  point.colors = ["#ff6677", "#66ffee"];
  point._cachedColors = null;
  renderer.render(bundle, viewport);

  const colorUploads = canvas.getContext("webgl2").ops.filter((entry) => (
    entry[0] === "bufferData" &&
    entry[3] === 8 &&
    entry[4] === canvas.getContext("webgl2").STATIC_DRAW
  ));
  const deletes = canvas.getContext("webgl2").ops.filter((entry) => entry[0] === "deleteBuffer");
  assert.equal(colorUploads.length, 2);
  assert.ok(deletes.some((entry) => entry[1] === colorUploads[0][2]));

  renderer.dispose();
});

test("bootstrap reuses static point WebGL buffers across transient point records", async () => {
  const env = createContext({
    enableWebGL2: true,
    disableCanvas2D: true,
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const registry = api.sceneBackendRegistry;
  const backend = registry.select({
    webgl: true,
    webgl2: true,
    webgpu: false,
    canvas: false,
    canvas2d: false,
  });
  const canvas = env.document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 180;
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  assert.equal(renderer && renderer.type, "webgl-pbr");

  const positions = new Float32Array([0, 0, 0, 1, 0, 0]);
  const sizes = new Float32Array([1, 1]);
  const colors = new Float32Array([1, 1, 1, 1, 0.5, 0.8, 1, 1]);
  const nextColors = new Float32Array([1, 0.4, 0.5, 1, 0.4, 1, 0.9, 1]);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

  function bundleWith(pointColors) {
    return {
      bundleVersion: api.SCENE_RENDER_BUNDLE_VERSION,
      background: "#000000",
      camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
      environment: {},
      points: [{
        id: "stars",
        count: 2,
        positions,
        sizes,
        colors: pointColors || colors,
        style: "focus",
        size: 1,
        opacity: 1,
        blendMode: "additive",
        depthWrite: false,
        attenuation: true,
      }],
      instancedMeshes: [],
      computeParticles: [],
      objects: [],
      meshObjects: [],
      materials: [],
      labels: [],
      sprites: [],
      lights: [],
      positions: new Float32Array(0),
      colors: new Float32Array(0),
      worldPositions: new Float32Array(0),
      worldColors: new Float32Array(0),
      worldLineWidths: new Float32Array(0),
      worldMeshPositions: new Float32Array(0),
      worldMeshColors: new Float32Array(0),
      worldMeshNormals: new Float32Array(0),
      worldMeshUVs: new Float32Array(0),
      worldMeshTangents: new Float32Array(0),
      vertexCount: 0,
      worldVertexCount: 0,
      postEffects: [],
    };
  }

  const gl = canvas.getContext("webgl2");
  const staticUploadCount = () => gl.ops.filter((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW).length;
  const colorUploads = () => gl.ops.filter((entry) => (
    entry[0] === "bufferData" &&
    entry[3] === 8 &&
    entry[4] === gl.STATIC_DRAW
  ));

  renderer.render(bundleWith(colors), viewport);
  const firstUploadCount = staticUploadCount();
  const firstColorBufferID = colorUploads()[0][2];

  renderer.render(bundleWith(colors), viewport);
  renderer.render(bundleWith(colors), viewport);
  assert.equal(staticUploadCount(), firstUploadCount);

  renderer.render(bundleWith(nextColors), viewport);
  assert.equal(staticUploadCount(), firstUploadCount + 1);
  assert.ok(gl.ops.some((entry) => entry[0] === "deleteBuffer" && entry[1] === firstColorBufferID));

  const afterPaletteUploadCount = staticUploadCount();
  renderer.render(bundleWith(nextColors), viewport);
  assert.equal(staticUploadCount(), afterPaletteUploadCount);

  renderer.dispose();
});

test("bootstrap reuses static opaque Scene3D buffers across dynamic-only runtime updates", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-static-cache-root";
  let renderIndex = 0;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-static-cache-program.json": { text: '{"name":"StaticCache"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-static-cache",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-static-cache-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-static-cache-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => {
      renderIndex += 1;
      const shieldZ = renderIndex === 1 ? 1 : 1.5;
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [
          -2, 0, 0, 2, 0, 0,
          -1, 0.5, shieldZ, 1, 0.5, shieldZ,
        ],
        worldColors: [
          0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1,
          0.8, 0.95, 1, 1, 0.8, 0.95, 1, 1,
        ],
        worldVertexCount: 4,
        materials: [
          { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
          { kind: "glass", color: "#c7f0ff", opacity: 0.45, wireframe: true, blendMode: "alpha", emissive: 0.05 },
        ],
        objects: [
          {
            id: "floor",
            kind: "plane",
            materialIndex: 0,
            vertexOffset: 0,
            vertexCount: 2,
            static: true,
            bounds: { minX: -2, minY: 0, minZ: 0, maxX: 2, maxY: 0, maxZ: 0 },
            depthNear: 6,
            depthFar: 6,
            depthCenter: 6,
            viewCulled: false,
          },
          {
            id: "shield",
            kind: "plane",
            materialIndex: 1,
            vertexOffset: 2,
            vertexCount: 2,
            static: false,
            bounds: { minX: -1, minY: 0.5, minZ: shieldZ, maxX: 1, maxY: 0.5, maxZ: shieldZ },
            depthNear: 6 + shieldZ,
            depthFar: 6 + shieldZ,
            depthCenter: 6 + shieldZ,
            viewCulled: false,
          },
        ],
        objectCount: 2,
      });
    },
  });

  let rafCount = 0;
  // Allow two frames so the test can observe buffer reuse across a
  // second render. The scene mount defers its first render to rAF for
  // LCP; bounding at one rAF here used to match an older sync-mount
  // path that no longer exists, leaving the test stuck at a single
  // engineRenderCalls entry while the assertion wants >= 2.
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 2) return 0;
    rafCount += 1;
    return setTimeout(() => callback(rafCount * 16), 0);
  };
  env.context.cancelAnimationFrame = (handle) => clearTimeout(handle);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(env.engineRenderCalls.length >= 2);
  assert.equal(gl.ops.filter((entry) => entry[0] === "bufferData" && entry[2] === 4).length, 1);
});

test("bootstrap invalidates static opaque Scene3D buffers when camera clip state changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-static-camera-root";
  let renderIndex = 0;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-static-camera-program.json": { text: '{"name":"StaticCamera"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-static-camera",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-static-camera-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-static-camera-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => {
      renderIndex += 1;
      const cameraZ = renderIndex === 1 ? 6 : 5.5;
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: cameraZ, fov: 72, near: 0.05, far: 128 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [
          -2, 0, 0, 2, 0, 0,
        ],
        worldColors: [
          0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1,
        ],
        worldVertexCount: 2,
        materials: [
          { key: "flat|#35556a|1.000|true|opaque|opaque|0.000", kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0 },
        ],
        objects: [
          {
            id: "floor",
            kind: "plane",
            materialIndex: 0,
            vertexOffset: 0,
            vertexCount: 2,
            static: true,
            bounds: { minX: -2, minY: 0, minZ: 0, maxX: 2, maxY: 0, maxZ: 0 },
            depthNear: cameraZ,
            depthFar: cameraZ,
            depthCenter: cameraZ,
            viewCulled: false,
          },
        ],
        objectCount: 1,
      });
    },
  });

  let rafCount = 0;
  // Allow two frames so the test can observe buffer reuse across a
  // second render. The scene mount defers its first render to rAF for
  // LCP; bounding at one rAF here used to match an older sync-mount
  // path that no longer exists, leaving the test stuck at a single
  // engineRenderCalls entry while the assertion wants >= 2.
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 2) return 0;
    rafCount += 1;
    return setTimeout(() => callback(rafCount * 16), 0);
  };
  env.context.cancelAnimationFrame = (handle) => clearTimeout(handle);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(env.engineRenderCalls.length >= 2);
  assert.equal(gl.ops.filter((entry) => entry[0] === "bufferData" && entry[2] === 4).length, 2);
});

test("bootstrap invalidates static opaque Scene3D buffers when shared-runtime lighting changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-static-lighting-root";
  let renderIndex = 0;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-static-lighting-program.json": { text: '{"name":"StaticLighting"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-static-lighting",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-static-lighting-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-static-lighting-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => {
      renderIndex += 1;
      const warm = renderIndex === 1 ? 0.35 : 0.92;
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [
          -2, 0, 0, 2, 0, 0,
        ],
        worldColors: [
          warm, 0.42, 0.5, 1, warm, 0.42, 0.5, 1,
        ],
        worldVertexCount: 2,
        materials: [
          { key: "flat|#808080|1.000|true|opaque|opaque|0.000", kind: "flat", color: "#808080", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0 },
        ],
        objects: [
          {
            id: "hero",
            kind: "box",
            materialIndex: 0,
            vertexOffset: 0,
            vertexCount: 2,
            static: true,
            bounds: { minX: -2, minY: 0, minZ: 0, maxX: 2, maxY: 0, maxZ: 0 },
            depthNear: 6,
            depthFar: 6,
            depthCenter: 6,
            viewCulled: false,
          },
        ],
        objectCount: 1,
      });
    },
  });

  let rafCount = 0;
  // Allow two frames so the test can observe buffer reuse across a
  // second render. The scene mount defers its first render to rAF for
  // LCP; bounding at one rAF here used to match an older sync-mount
  // path that no longer exists, leaving the test stuck at a single
  // engineRenderCalls entry while the assertion wants >= 2.
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 2) return 0;
    rafCount += 1;
    return setTimeout(() => callback(rafCount * 16), 0);
  };
  env.context.cancelAnimationFrame = (handle) => clearTimeout(handle);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(env.engineRenderCalls.length >= 2);
  assert.equal(gl.ops.filter((entry) => entry[0] === "bufferData" && entry[2] === 4).length, 2);
});

test("bootstrap prefers engine-batched Scene3D pass payloads when present", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pass-bundle-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-pass-bundle-program.json": { text: '{"name":"PassBundle"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-pass-bundle",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-pass-bundle-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-pass-bundle-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -9, 0, 0, -8, 0, 0,
      ],
      worldColors: [
        1, 0, 0, 1, 1, 0, 0, 1,
      ],
      worldVertexCount: 2,
      materials: [
        { key: "flat|#35556a|1.000|true|opaque|opaque|0.000", kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", renderPass: "opaque", emissive: 0, shaderData: [0, 0, 1] },
      ],
      objects: [
        { id: "floor", kind: "plane", materialIndex: 0, renderPass: "opaque", vertexOffset: 0, vertexCount: 2, static: true, depthCenter: 6, viewCulled: false },
      ],
      passes: [
        {
          name: "staticOpaque",
          blend: "opaque",
          depth: "opaque",
          static: true,
          cacheKey: "engine-pass-key",
          positions: [1, 0, 0, 2, 0, 0],
          colors: [0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1],
          materials: [0, 0, 1, 0, 0, 1],
          vertexCount: 2,
        },
      ],
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.deepEqual(gl.bufferUploads.get(4), [1, 0, 0, 2, 0, 0]);
});

test("bootstrap keeps static Scene3D bundle-pass caches isolated per pass", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pass-cache-root";
  let renderIndex = 0;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-pass-cache-program.json": { text: '{"name":"PassCache"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-pass-cache",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-pass-cache-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-pass-cache-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => {
      renderIndex += 1;
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [
          1, 0, 0, 2, 0, 0,
        ],
        worldColors: [
          0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1,
        ],
        worldVertexCount: 2,
        materials: [],
        objects: [],
        objectCount: 0,
        passes: [
          {
            name: "staticOpaque",
            blend: "opaque",
            depth: "opaque",
            static: true,
            cacheKey: "shared-engine-pass-key",
            positions: [1, 0, 0, 2, 0, 0],
            colors: [0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1],
            materials: [0, 0, 1, 0, 0, 1],
            vertexCount: 2,
          },
          {
            name: "alpha",
            blend: "alpha",
            depth: "translucent",
            static: true,
            cacheKey: "shared-engine-pass-key",
            positions: [-4, 0, 2, -3, 0, 2],
            colors: [0.9, 0.8, 0.5, 1, 0.9, 0.8, 0.5, 1],
            materials: [2, 0.05, 0.7, 2, 0.05, 0.7],
            vertexCount: 2,
          },
        ],
      });
    },
  });

  let rafCount = 0;
  // Allow two frames so the test can observe buffer reuse across a
  // second render. The scene mount defers its first render to rAF for
  // LCP; bounding at one rAF here used to match an older sync-mount
  // path that no longer exists, leaving the test stuck at a single
  // engineRenderCalls entry while the assertion wants >= 2.
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 2) return 0;
    rafCount += 1;
    return setTimeout(() => callback(rafCount * 16), 0);
  };
  env.context.cancelAnimationFrame = (handle) => clearTimeout(handle);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(env.engineRenderCalls.length >= 2);
  assert.deepEqual(gl.bufferUploads.get(4), [1, 0, 0, 2, 0, 0]);
  assert.deepEqual(gl.bufferUploads.get(7), [-4, 0, 2, -3, 0, 2]);
  assert.equal(
    gl.ops.filter((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW && (entry[2] === 4 || entry[2] === 7)).length,
    2,
  );
});

test("bootstrap clamps engine-batched Scene3D pass vertex counts to uploaded geometry", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-pass-clamp-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-pass-clamp-program.json": { text: '{"name":"PassClamp"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-pass-clamp",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-pass-clamp-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-pass-clamp-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        1, 0, 0, 2, 0, 0,
      ],
      worldColors: [
        0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1,
      ],
      worldVertexCount: 2,
      materials: [],
      objects: [],
      objectCount: 0,
      passes: [
        {
          name: "dynamicOpaque",
          blend: "opaque",
          depth: "opaque",
          positions: [1, 0, 0, 2, 0, 0],
          colors: [0.3, 0.4, 0.5, 1, 0.3, 0.4, 0.5, 1],
          materials: [0, 0, 1, 0, 0, 1],
          vertexCount: 99,
        },
      ],
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] === 2));
  assert.ok(!gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] === 99));
});

test("bootstrap reuses opaque Scene3D WebGL state transitions within a frame", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-opaque-state-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-opaque-state-program.json": { text: '{"name":"OpaqueState"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-opaque-state",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-opaque-state-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-opaque-state-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -2, 0, 0, 2, 0, 0,
      ],
      worldColors: [
        0.4, 0.5, 0.6, 1, 0.4, 0.5, 0.6, 1,
      ],
      worldVertexCount: 2,
      materials: [
        { kind: "flat", color: "#35556a", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        { id: "floor", kind: "plane", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: true, depthCenter: 6, viewCulled: false },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.equal(gl.ops.filter((entry) => entry[0] === "disable" && entry[1] === gl.BLEND).length, 1);
  assert.equal(gl.ops.filter((entry) => entry[0] === "enable" && entry[1] === gl.DEPTH_TEST).length, 1);
  assert.equal(gl.ops.filter((entry) => entry[0] === "depthMask" && entry[1] === true).length, 1);
});

test("bootstrap depth-sorts alpha Scene3D objects before upload", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-alpha-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-alpha-program.json": { text: '{"name":"AlphaDepth"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-alpha",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-alpha-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-alpha-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        4, 0, -2, 3, 0, -2,
        -4, 0, 2, -3, 0, 2,
      ],
      worldColors: [
        0.3, 0.6, 0.9, 1, 0.3, 0.6, 0.9, 1,
        0.9, 0.8, 0.5, 1, 0.9, 0.8, 0.5, 1,
      ],
      worldVertexCount: 4,
      materials: [
        { key: "glass|#c7f0ff|0.450|true|alpha|alpha|0.050", kind: "glass", color: "#c7f0ff", opacity: 0.45, wireframe: true, blendMode: "opaque", emissive: 0.05, shaderData: [2, 0.05, 0.7] },
      ],
      objects: [
        { id: "near-static", kind: "plane", materialIndex: 0, renderPass: "alpha", vertexOffset: 0, vertexCount: 2, static: true, depthCenter: 4 },
        { id: "far-dynamic", kind: "plane", materialIndex: 0, renderPass: "alpha", vertexOffset: 2, vertexCount: 2, static: false, depthCenter: 8 },
      ],
      objectCount: 2,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.deepEqual(gl.bufferUploads.get(7), [
    -4, 0, 2, -3, 0, 2,
    4, 0, -2, 3, 0, -2,
  ]);
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE_MINUS_SRC_ALPHA));
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] === 4));
});

test("bootstrap uploads engine-clipped Scene3D segments directly", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-clip-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-clip-program.json": { text: '{"name":"NearClip"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-clip",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-clip-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-clip-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -1.475, 0, -5.95, 2, 0, 1,
      ],
      worldColors: [
        0.7, 0.9, 1, 1, 0.7, 0.9, 1, 1,
      ],
      worldVertexCount: 2,
      materials: [
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        { id: "clip-line", kind: "line", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: true, depthCenter: 3.5 },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  const clipped = gl.bufferUploads.get(4);
  assert.equal(clipped.length, 6);
  assert.ok(Math.abs(clipped[0] + 1.475) < 0.001);
  assert.ok(Math.abs(clipped[1]) < 0.001);
  assert.ok(Math.abs(clipped[2] + 5.95) < 0.001);
  assert.deepEqual(clipped.slice(3), [2, 0, 1]);
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] === 2));
});

test("bootstrap honors engine-side Scene3D view-cull metadata", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-metadata-cull-root";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-metadata-cull-program.json": { text: '{"name":"MetadataCull"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-metadata-cull",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-metadata-cull-root",
          runtime: "shared",
          props: { width: 640, height: 360, background: "#08151f" },
          programRef: "/scene-metadata-cull-program.json",
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: () => JSON.stringify({
      background: "#08151f",
      camera: { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
      positions: [],
      colors: [],
      vertexCount: 0,
      worldPositions: [
        -1, 0, 0.5, 1, 0, 0.5,
      ],
      worldColors: [
        0.7, 0.9, 1, 1, 0.7, 0.9, 1, 1,
      ],
      worldVertexCount: 2,
      materials: [
        { kind: "flat", color: "#8de1ff", opacity: 1, wireframe: true, blendMode: "opaque", emissive: 0 },
      ],
      objects: [
        { id: "metadata-hidden", kind: "line", materialIndex: 0, vertexOffset: 0, vertexCount: 2, static: true, viewCulled: true, depthCenter: 6.5 },
      ],
      objectCount: 1,
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.deepEqual(gl.bufferUploads.get(4), []);
  assert.equal(gl.ops.some((entry) => entry[0] === "drawArrays"), false);
});

test("bootstrap mounts native Scene3D engines without extra scripts", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-root";
  mount.appendChild(new FakeElement("p", null));

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-2",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-root",
          jsExport: "GoSXScene3D",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "cube", size: 1.5, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(mount.children.length, 2);
  assert.equal(mount.firstElementChild.tagName, "CANVAS");
  assert.equal(mount.firstElementChild.getAttribute("width"), "640");
  assert.equal(mount.firstElementChild.getAttribute("height"), "360");
  assert.equal(mount.children[1].getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(mount.children[1].children.length, 0);

  env.context.__gosx_dispose_engine("gosx-engine-2");
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(mount.children.length, 0);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap renders mixed native Scene3D primitives", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-primitives";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-3",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-primitives",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.8, height: 1.2, depth: 1.1, x: -1.6, y: 0.1, z: -0.2, color: "#8de1ff" },
                { kind: "sphere", radius: 0.8, x: 0.2, y: 0.15, z: 0.6, color: "#ffd48f", segments: 10 },
                { kind: "pyramid", width: 1.4, height: 1.8, depth: 1.4, x: 1.9, y: -0.2, z: 0.4, color: "#b8ffb0" },
                { kind: "plane", width: 5.2, depth: 3.8, y: -1.6, z: 0.3, color: "#35556a" },
              ],
              labels: [
                {
                  id: "zoo-label",
                  text: "Geometry zoo\nBrowser-measured overlay copy",
                  x: 0.2,
                  y: 1.4,
                  z: 0.9,
                  maxWidth: 120,
                  whiteSpace: "pre-wrap",
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(mount.children.length, 2);
  const canvas = mount.firstElementChild;
  assert.equal(canvas.tagName, "CANVAS");

  const ctx2d = canvas.getContext("2d");
  const strokeCount = ctx2d.ops.filter((entry) => entry[0] === "stroke").length;
  const lineCount = ctx2d.ops.filter((entry) => entry[0] === "lineTo").length;
  const labelLayer = mount.children[1];
  assert.equal(canvas.getAttribute("width"), "520");
  assert.equal(canvas.getAttribute("height"), "320");
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(labelLayer.getAttribute("data-gosx-scene3d-label-layer"), "true");
  assert.equal(labelLayer.children.length, 1);
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-text-layout-role"), "label");
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-text-layout-surface"), "scene3d");
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-text-layout-state"), "ready");
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-scene-label-visibility"), "visible");
  assert.equal(labelLayer.children[0].children.length >= 2, true);
  assert.equal(labelLayer.children[0].textContent, "Geometry zooBrowser-measured overlay copy");
  assert.equal(env.context.__gosx.textLayout.read(labelLayer.children[0]).lineCount >= 2, true);
  assert.ok(lineCount >= 12);
  assert.ok(strokeCount >= 1);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap routes native Scene3D material profiles through WebGL pass planning", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-native-materials";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-native-materials",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-native-materials",
          jsExport: "GoSXScene3D",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { x: 0, y: 0, z: 6, near: 0.05, far: 128, fov: 72 },
            scene: {
              objects: [
                {
                  id: "floor",
                  kind: "plane",
                  width: 6.2,
                  depth: 4.8,
                  y: -1.8,
                  z: 0.3,
                  color: "#35556a",
                  materialKind: "flat",
                },
                {
                  id: "glass-orb",
                  kind: "sphere",
                  radius: 0.82,
                  x: -1.35,
                  y: 0.2,
                  z: 0.85,
                  color: "#c7f0ff",
                  materialKind: "glass",
                  opacity: 0.45,
                  emissive: 0.05,
                },
                {
                  id: "glow-orb",
                  kind: "sphere",
                  radius: 0.74,
                  x: 1.45,
                  y: 0.46,
                  z: 1.62,
                  color: "#8de1ff",
                  materialKind: "glow",
                  opacity: 0.72,
                  emissive: 0.4,
                },
              ],
            },
          },
          capabilities: ["webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  assert.ok(!gl.bufferUploads.has(1));
  assert.ok(Array.isArray(gl.bufferUploads.get(4)) && gl.bufferUploads.get(4).length > 0);
  assert.ok(Array.isArray(gl.bufferUploads.get(7)) && gl.bufferUploads.get(7).length > 0);
  assert.ok(Array.isArray(gl.bufferUploads.get(10)) && gl.bufferUploads.get(10).length > 0);
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE_MINUS_SRC_ALPHA));
  assert.ok(gl.ops.some((entry) => entry[0] === "blendFunc" && entry[1] === gl.SRC_ALPHA && entry[2] === gl.ONE));
  assert.ok(gl.ops.filter((entry) => entry[0] === "drawArrays" && entry[3] > 0).length >= 3);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap tints native Scene3D geometry with declarative lights and environment", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-native-lighting";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-native-lighting",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-native-lighting",
          jsExport: "GoSXScene3D",
          props: {
            width: 640,
            height: 360,
            background: "#08151f",
            camera: { x: 0, y: 0, z: 6, near: 0.05, far: 128, fov: 72 },
            scene: {
              environment: {
                ambientColor: "#f4fbff",
                ambientIntensity: 0.14,
                skyColor: "#b9deff",
                skyIntensity: 0.12,
                groundColor: "#102030",
                groundIntensity: 0.04,
              },
              lights: [
                {
                  id: "sun",
                  kind: "directional",
                  color: "#fff1d6",
                  intensity: 1.25,
                  directionX: 0.3,
                  directionY: -1,
                  directionZ: -0.35,
                },
              ],
              objects: [
                {
                  id: "hero",
                  kind: "box",
                  width: 1.8,
                  height: 1.2,
                  depth: 1.2,
                  color: "#808080",
                  materialKind: "flat",
                },
              ],
            },
          },
          capabilities: ["webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const gl = mount.children[0].getContext("webgl");
  const uploadedColors = gl.bufferUploads.get(5);
  assert.ok(Array.isArray(uploadedColors) && uploadedColors.length > 0);
  assert.ok(uploadedColors[0] > uploadedColors[2]);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap respects static Scene3D camera clip props for label projection", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-camera-clip";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-camera-clip",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-camera-clip",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            camera: { x: 0, y: 0, z: 6, fov: 72, near: 7, far: 8 },
            scene: {
              labels: [
                {
                  id: "clipped-label",
                  text: "Too near",
                  x: -0.5,
                  y: 0.3,
                  z: 0,
                  maxWidth: 96,
                },
                {
                  id: "visible-label",
                  text: "Visible depth",
                  x: 0.5,
                  y: 0.6,
                  z: 1.5,
                  maxWidth: 120,
                },
              ],
            },
          },
          capabilities: ["canvas"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  assert.equal(labelLayer.children.length, 1);
  assert.equal(labelLayer.children[0].getAttribute("data-gosx-scene-label"), "visible-label");
  assert.equal(labelLayer.children[0].textContent, "Visible depth");
});

test("bootstrap gives Scene3D labels a shared text-layout CSS contract and custom classes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-label-contract";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-label-contract",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-label-contract",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              labels: [
                {
                  id: "hero-chip",
                  className: "hero-chip tone-accent",
                  text: "supercalifragilisticgosx",
                  x: 0,
                  y: 0.8,
                  z: 0.2,
                  maxWidth: 72,
                  maxLines: 1,
                  overflow: "ellipsis",
                  priority: 3,
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const label = mount.children[1].children[0];
  assert.equal(label.getAttribute("class"), "gosx-scene-label hero-chip tone-accent");
  assert.equal(label.getAttribute("data-gosx-text-layout-role"), "label");
  assert.equal(label.getAttribute("data-gosx-text-layout-surface"), "scene3d");
  assert.equal(label.getAttribute("data-gosx-scene-label-priority"), "3");
  assert.equal(label.getAttribute("data-gosx-scene-label-collision"), "avoid");
  assert.equal(label.getAttribute("data-gosx-scene-label-visibility"), "visible");
  assert.equal(label.getAttribute("data-gosx-text-layout-overflow"), "ellipsis");
  assert.equal(typeof env.context.__gosx.textLayout.read(label).lineCount, "number");
});

test("bootstrap hides lower-priority Scene3D labels when collision avoidance overlaps", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-label-collision";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-label-collision",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-label-collision",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              labels: [
                {
                  id: "primary-label",
                  text: "Primary label",
                  x: 0,
                  y: 0.4,
                  z: 0.2,
                  maxWidth: 132,
                  priority: 5,
                },
                {
                  id: "secondary-label",
                  text: "Secondary label",
                  x: 0,
                  y: 0.4,
                  z: 0.2,
                  maxWidth: 132,
                  priority: 1,
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  assert.equal(labelLayer.children.length, 2);
  const primary = labelLayer.children[0].getAttribute("data-gosx-scene-label") === "primary-label" ? labelLayer.children[0] : labelLayer.children[1];
  const secondary = primary === labelLayer.children[0] ? labelLayer.children[1] : labelLayer.children[0];
  assert.equal(primary.getAttribute("data-gosx-scene-label-visibility"), "visible");
  assert.equal(secondary.getAttribute("data-gosx-scene-label-visibility"), "hidden");
});

test("bootstrap marks occluded Scene3D labels when scene geometry covers their anchor", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-label-occlusion";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-label-occlusion",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-label-occlusion",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 2.8, height: 2.2, depth: 2.2, x: 0, y: 0, z: 0.2, color: "#8de1ff" },
              ],
              labels: [
                {
                  id: "occluded-label",
                  text: "Covered label",
                  x: 0,
                  y: 0,
                  z: 0.2,
                  maxWidth: 140,
                  offsetY: 0,
                  occlude: true,
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const label = mount.children[1].children[0];
  assert.equal(label.getAttribute("data-gosx-scene-label-occluded"), "true");
  assert.equal(label.getAttribute("data-gosx-scene-label-visibility"), "hidden");
});

test("bootstrap prefers WebGL Scene3D rendering when available", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: -0.8, y: 0, z: 0, color: "#8de1ff" },
                { kind: "sphere", radius: 0.7, x: 1.1, y: 0.2, z: 0.8, color: "#ffd48f" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.firstElementChild;
  assert.equal(canvas.tagName, "CANVAS");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");

  const gl = canvas.getContext("webgl");
  assert.ok(gl.ops.some((entry) => entry[0] === "bufferData" && entry[3] > 0));
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] > 0));
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap prefers canvas Scene3D rendering on software WebGL backends", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-software-webgl";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-software-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-software-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
    createWebGLContext: () => new FakeWebGLContext({
      vendor: "Google Inc. (Google)",
      renderer: "ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver)",
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-capability-tier"), "constrained");
  assert.equal(mount.getAttribute("data-gosx-scene3d-low-power"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-software-webgl"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "avoid");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "environment-constrained");
});

test("bootstrap requires WebGL for Scene3D when requested", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-required-webgl";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-required-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-required-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            requireWebGL: true,
            unsupportedMessage: "Update your browser or enable hardware acceleration.",
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-require-webgl"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "unsupported");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgl-required");
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0].getAttribute("data-gosx-scene3d-unsupported"), "true");
  assert.equal(mount.children[0].textContent, "Update your browser or enable hardware acceleration.");
});

test("bootstrap honors required WebGL over software-renderer canvas preference", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-required-software-webgl";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-required-software-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-required-software-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            requireWebGL: true,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
    createWebGLContext: () => new FakeWebGLContext({
      vendor: "Google Inc. (Google)",
      renderer: "ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver)",
    }),
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-software-webgl"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-require-webgl"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "force");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
});

test("Scene3D defers postfx until idle delay", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-deferred-postfx";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl-deferred-postfx",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-deferred-postfx",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            deferPostFX: true,
            deferPostFXDelayMS: 40,
            scene: {
              postEffects: [
                { kind: "bloom", threshold: 0.7, intensity: 0.5 },
                { kind: "toneMapping", mode: "aces", exposure: 1 },
              ],
              points: [
                {
                  id: "stars",
                  count: 3,
                  positions: [0, 0, 0, 1, 1, 0, -1, 1, 0],
                  color: "#ffffff",
                  size: 1,
                },
              ],
            },
          },
          capabilities: ["canvas", "webgl", "animation"],
        },
      ],
    },
  });
  const timers = installManualTimers(env.context);
  env.context.requestIdleCallback = () => 1;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-postfx"), "deferred");

  assert.equal(timers.runDelay(40), 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-postfx"), "deferred");
  assert.equal(timers.runDelay(1200), 1);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-postfx"), "enabled");
});

test("bootstrap keeps Scene3D responsive across resize and DPR changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-responsive";
  mount.width = 520;

  const env = createContext({
    elements: [mount],
    devicePixelRatio: 1,
    manifest: {
      engines: [
        {
          id: "gosx-engine-responsive",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-responsive",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              labels: [
                {
                  id: "center-label",
                  text: "Center label",
                  x: 0,
                  y: 0,
                  z: 0.5,
                  offsetY: 0,
                  maxWidth: 140,
                },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.firstElementChild;
  const label = mount.children[1].children[0];
  const initialLeft = label.style["--gosx-scene-label-left"];
  assert.equal(canvas.getAttribute("width"), "520");
  assert.equal(canvas.style.width, "100%");
  assert.equal(mount.getAttribute("data-gosx-scene3d-pixel-ratio"), "1");

  mount.width = 260;
  env.context.devicePixelRatio = 2;
  env.resizeObservers[0].trigger([mount]);
  await flushAsyncWork();

  assert.equal(canvas.getAttribute("width"), "520");
  assert.equal(canvas.getAttribute("height"), "320");
  assert.equal(canvas.style.width, "100%");
  assert.equal(canvas.style.height, "auto");
  assert.equal(mount.getAttribute("data-gosx-scene3d-css-width"), "260");
  assert.equal(mount.getAttribute("data-gosx-scene3d-css-height"), "160");
  assert.equal(mount.getAttribute("data-gosx-scene3d-pixel-ratio"), "2");
  assert.notEqual(label.style["--gosx-scene-label-left"], initialLeft);
});

test("bootstrap prefers canvas Scene3D rendering on constrained coarse-pointer devices", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-constrained-mobile";

  const env = createContext({
    elements: [mount],
    devicePixelRatio: 3,
    deviceMemory: 4,
    hardwareConcurrency: 4,
    enableWebGL: true,
    matchMedia: {
      "(pointer: coarse)": true,
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-constrained-mobile",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-constrained-mobile",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-capability-tier"), "constrained");
  assert.equal(mount.getAttribute("data-gosx-scene3d-coarse-pointer"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-low-power"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "avoid");
  assert.equal(mount.getAttribute("data-gosx-scene3d-pixel-ratio"), "1.25");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "environment-constrained");
});

test("bootstrap reconfigures Scene3D renderer when environment constraints change", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-capability-reconfigure";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-capability-reconfigure",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-capability-reconfigure",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);

  env.matchMedia("(prefers-reduced-data: reduce)").dispatch(true);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-reduced-data"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "avoid");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "environment-constrained");

  env.matchMedia("(prefers-reduced-data: reduce)").dispatch(false);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-reduced-data"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-webgl-preference"), "prefer");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
});

test("bootstrap falls back from WebGL and restores Scene3D rendering after context events", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-webgl-fallback";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-webgl-fallback",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-webgl-fallback",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const ctx2d = canvas.getContext("2d");
  let prevented = false;

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  canvas.dispatchEvent({
    type: "webglcontextlost",
    preventDefault() {
      prevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, true);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "canvas");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgl-context-lost");
  assert.ok(ctx2d.ops.some((entry) => entry[0] === "fillRect"));

  const lostGl = canvas.getContext("webgl");
  canvas._webglContext = null;
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  const restoredGl = canvas.getContext("webgl");

  assert.notEqual(restoredGl, lostGl);
  assert.ok(
    restoredGl.ops.some((entry) => entry[0] === "bufferData" && entry[3] > 0),
    "restored renderer must upload geometry buffers to the new GL context",
  );
  assert.ok(
    restoredGl.ops.some((entry) => entry[0] === "drawArrays" && entry[3] > 0),
    "restored renderer must draw against the new GL context",
  );
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
});

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

test("bootstrap installs a client-event telemetry emitter that POSTs to /_gosx/client-events", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(typeof env.context.__gosx_emit, "function", "__gosx_emit should be installed");

  env.context.__gosx_emit("warn", "test", "hello world", { k: "v" });
  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  const events = telemetryEvents(env);
  assert.equal(events.length, 1, "expected one event, got: " + JSON.stringify(events));
  assert.equal(events[0].cat, "test");
  assert.equal(events[0].msg, "hello world");
  assert.equal(events[0].lvl, "warn");
  assert.deepEqual(events[0].fields, { k: "v" });
  assert.ok(events[0].ua, "first batch should include userAgent");

  const bodies = telemetryPostBodies(env);
  assert.ok(bodies[0].sid && bodies[0].sid.startsWith("s_"), "sid should be generated");
});

test("bootstrap telemetry flushes on scheduled timer", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 10 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.__gosx_emit("info", "timer-test", "tick", {});

  await new Promise((resolve) => setTimeout(resolve, 40));
  await flushAsyncWork();

  const events = telemetryEvents(env);
  assert.equal(events.length, 1, "expected one event after timer fired, got: " + JSON.stringify(events));
  assert.equal(events[0].cat, "timer-test");
});

test("bootstrap telemetry drops into no-op when disabled via config", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = { enabled: false, flushInterval: 0 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.__gosx_emit("warn", "x", "should-not-ship", {});
  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  assert.equal(telemetryEvents(env).length, 0, "disabled telemetry must not POST");
});

test("bootstrap telemetry captures uncaught window errors", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.dispatchEvent({
    type: "error",
    message: "Test uncaught",
    filename: "app.js",
    lineno: 7,
    colno: 3,
    error: { stack: "Error: Test uncaught\n    at app.js:7:3" },
  });
  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  const events = telemetryEvents(env);
  assert.equal(events.length, 1);
  assert.equal(events[0].cat, "runtime");
  assert.equal(events[0].lvl, "error");
  assert.equal(events[0].msg, "Test uncaught");
  assert.equal(events[0].fields.filename, "app.js");
  assert.equal(events[0].fields.lineno, 7);
});

test("bootstrap scene3d emits telemetry for webgl context-lost and context-restored", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-telemetry-ctx";

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-telemetry-ctx",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-telemetry-ctx",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
  await flushAsyncWork();
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  await flushAsyncWork();

  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  const events = telemetryEvents(env);
  const scene3dMsgs = events.filter((ev) => ev.cat === "scene3d").map((ev) => ev.msg);
  assert.ok(
    scene3dMsgs.some((msg) => msg === "webgl-context-lost"),
    "expected scene3d/webgl-context-lost telemetry, got: " + scene3dMsgs.join(", "),
  );
  assert.ok(
    scene3dMsgs.some((msg) => msg === "webgl-context-restored"),
    "expected scene3d/webgl-context-restored telemetry, got: " + scene3dMsgs.join(", "),
  );
  const restored = events.find((ev) => ev.cat === "scene3d" && ev.msg === "webgl-context-restored");
  assert.equal(restored && restored.fields && restored.fields.swapped, true, "context-restored should report swapped=true");

  const renderEmpty = events.find((ev) => ev.cat === "scene3d" && ev.msg === "render-empty");
  assert.equal(
    renderEmpty,
    undefined,
    "restored renderer must produce non-empty bundle (render-empty should not fire), got: " + JSON.stringify(renderEmpty),
  );

  const warmup = events.find((ev) => ev.cat === "scene3d" && ev.msg === "renderer-warmup");
  assert.ok(warmup, "renderer-warmup should fire after restore, events: " + JSON.stringify(events));
  assert.equal(warmup.fields.rendererKind, "webgl");
  assert.ok(warmup.fields.bundleMeshObjects >= 0, "warmup reports mesh object count");
});

test("bootstrap keeps hidden Scene3D WebGL contexts alive instead of voluntary loss", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-hidden-webgl";
  let canvas = null;
  let lost = false;
  let loseCalls = 0;
  let restoreCalls = 0;
  const extension = {
    loseContext() {
      loseCalls += 1;
      lost = true;
      canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
    },
    restoreContext() {
      restoreCalls += 1;
      lost = false;
      canvas._webglContext = null;
    },
  };

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    disableCanvas2D: true,
    createWebGLContext: () => {
      const gl = new FakeWebGLContext();
      gl.getExtension = (name) => {
        if (name !== "WEBGL_lose_context") {
          return null;
        }
        return lost ? null : extension;
      };
      return gl;
    },
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-hidden-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-hidden-webgl",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: true,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };
  const timers = installManualTimers(env.context);
  installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  canvas = mount.children[0];

  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();
  assert.equal(timers.runDelay(30000), 0, "hidden scenes should not schedule voluntary WebGL loss");
  await flushAsyncWork();

  assert.equal(loseCalls, 0);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");

  env.document.visibilityState = "visible";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(restoreCalls, 0);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();
  const requested = telemetryEvents(env).find((ev) => ev.cat === "scene3d" && ev.msg === "webgl-voluntary-restore-requested");
  assert.equal(requested, undefined);
});

test("scene3d render-empty does NOT fire on restore when bundle has meshObjects (modern PBR path)", async () => {
  // Regression: the pre-alpha.21 detector only inspected legacy vertex/surface
  // fields on the bundle. If the PBR path populated only meshObjects (no
  // legacy verts), the detector fell through, counted sceneState objects, and
  // fired a FALSE POSITIVE render-empty. After alpha.21 the early-return
  // also considers bundle.meshObjects and bundle.instancedMeshes.
  const mount = new FakeElement("div", null);
  mount.id = "scene-modern-pbr-probe";
  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-modern-pbr",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-modern-pbr-probe",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1, height: 1, depth: 1, x: 0, y: 0, z: 0, color: "#fff" },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.__gosx_telemetry_config = { flushInterval: 0 };
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  // Simulate the PBR-only bundle shape by stripping the legacy vertex fields
  // from every bundle the runtime hands the renderer. The bundle will still
  // carry meshObjects (populated from sceneState.objects). Post-restore the
  // detector must treat that as "geometry is present" and skip render-empty.
  const api = env.context.__gosx_scene3d_api;
  const origCreateBundle = api.createSceneRenderBundle;
  api.createSceneRenderBundle = function (...args) {
    const bundle = origCreateBundle.apply(this, args);
    return Object.assign({}, bundle, {
      vertexCount: 0,
      worldVertexCount: 0,
      surfaces: [],
      meshObjects: bundle.meshObjects && bundle.meshObjects.length > 0
        ? bundle.meshObjects
        : [{ id: "synthetic-pbr-box", material: "pbr", transform: null, geometry: { vertexCount: 36 } }],
    });
  };

  const canvas = mount.children[0];
  canvas.dispatchEvent({ type: "webglcontextlost", preventDefault() {} });
  await flushAsyncWork();
  canvas.dispatchEvent({ type: "webglcontextrestored" });
  await flushAsyncWork();

  env.context.__gosx_telemetry_flush();
  await flushAsyncWork();

  const events = telemetryEvents(env);
  const renderEmpty = events.find((ev) => ev.cat === "scene3d" && ev.msg === "render-empty");
  assert.equal(
    renderEmpty,
    undefined,
    "render-empty must not fire when bundle has meshObjects (modern PBR), got: " + JSON.stringify(renderEmpty),
  );
  const warmup = events.find((ev) => ev.cat === "scene3d" && ev.msg === "renderer-warmup");
  assert.ok(warmup, "renderer-warmup should fire after restore, events: " + JSON.stringify(events));
  assert.equal(warmup.fields.rendererKind, "webgl");
});

test("bootstrap telemetry flushes via sendBeacon on visibility hidden", async () => {
  const env = createContext({
    fetchRoutes: {
      "/_gosx/client-events": { status: 204, text: "" },
    },
  });
  const beaconCalls = [];
  env.context.navigator.sendBeacon = function (url, body) {
    beaconCalls.push({ url, body });
    return true;
  };
  env.context.__gosx_telemetry_config = { flushInterval: 30000 };

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.context.__gosx_emit("info", "visibility-test", "bye", {});
  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(beaconCalls.length, 1, "expected one beacon call, got: " + JSON.stringify(beaconCalls));
  assert.equal(beaconCalls[0].url, "/_gosx/client-events");
  const parsed = JSON.parse(beaconCalls[0].body);
  assert.equal(parsed.events[0].cat, "visibility-test");
  assert.equal(telemetryPostBodies(env).length, 0, "should prefer beacon over fetch when available");
});

test("bootstrap respects prefers-reduced-motion for Scene3D animation loops", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-reduced-motion";

  const env = createContext({
    elements: [mount],
    prefersReducedMotion: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-reduced-motion",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-reduced-motion",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: true,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-reduced-motion"), "true");
  assert.equal(raf.count(), 0);

  env.matchMedia("(prefers-reduced-motion: reduce)").dispatch(false);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-reduced-motion"), "false");
  assert.equal(raf.count(), 1);
});

test("animated Scene3D scroll camera renders immediately on scroll input", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-scroll-camera-active";
  mount.width = 640;
  mount.height = 360;

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    visualViewport: false,
    manifest: {
      engines: [
        {
          id: "gosx-engine-scroll-camera-active",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-scroll-camera-active",
          jsExport: "GoSXScene3D",
          props: {
            width: 640,
            height: 360,
            autoRotate: true,
            scrollCameraStart: 10,
            scrollCameraEnd: 4,
            scene: {
              objects: [
                { kind: "box", width: 1.4, height: 1.1, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });
  env.context.__gosx_scene3d_perf = true;
  env.context.innerHeight = 1000;
  env.document.documentElement.scrollHeight = 2000;
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const canvas = mount.children[0];
  const gl = canvas.getContext("webgl");
  const cameraZ = () => {
    const calls = gl.ops.filter((entry) => entry[0] === "uniform4f" && entry[1] === "u_camera");
    return calls[calls.length - 1][4];
  };
  assert.equal(cameraZ(), 10);

  env.context.scrollY = 900;
  env.context.dispatchEvent({ type: "scroll" });
  assert.equal(mount.__gosxScene3DScheduleCounts["schedule:scroll"], 1);

  raf.flush(32);
  await flushAsyncWork();

  assert.ok(cameraZ() < 5, "scroll camera should jump near target instead of easing slowly; z=" + cameraZ());
});

test("bootstrap rerenders shared-runtime Scene3D with responsive viewport dimensions", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-runtime-responsive";
  mount.width = 640;
  const renderArgs = [];

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-responsive-runtime.json": { text: '{"name":"ResponsiveScene"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-runtime-responsive",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-runtime-responsive",
          runtime: "shared",
          programRef: "/scene-responsive-runtime.json",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            background: "#08151f",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: (...args) => {
      renderArgs.push(args);
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [],
        worldColors: [],
        worldVertexCount: 0,
        objects: [],
        labels: [],
      });
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.deepEqual(renderArgs[0].slice(2, 4), [640, 360]);

  mount.width = 320;
  env.resizeObservers[0].trigger([mount]);
  await flushAsyncWork();

  const last = renderArgs[renderArgs.length - 1];
  assert.deepEqual(last.slice(2, 4), [320, 180]);
});

test("bootstrap rerenders shared-runtime Scene3D on visual viewport scroll changes", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-runtime-viewport-scroll";
  mount.width = 640;
  const renderArgs = [];

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-viewport-scroll.json": { text: '{"name":"ViewportScrollScene"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-viewport-scroll",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-runtime-viewport-scroll",
          runtime: "shared",
          programRef: "/scene-viewport-scroll.json",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            background: "#08151f",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: (...args) => {
      renderArgs.push(args);
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [],
        worldColors: [],
        worldVertexCount: 0,
        objects: [],
        labels: [],
      });
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const initialRenderCount = renderArgs.length;
  assert.equal(initialRenderCount > 0, true);
  assert.equal(env.visualViewport.listenerCount("scroll") >= 1, true);

  env.visualViewport.dispatchEvent({ type: "scroll" });
  await flushAsyncWork();

  assert.equal(renderArgs.length > initialRenderCount, true);
});

test("bootstrap pauses animated Scene3D when the page is hidden and resumes on visibilitychange", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-page-visibility";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-page-visibility",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-page-visibility",
          jsExport: "GoSXScene3D",
          props: {
            width: 480,
            height: 300,
            autoRotate: true,
            scene: {
              objects: [
                { kind: "box", width: 1.6, height: 1.2, depth: 1.2, x: 0, y: 0, z: 0, color: "#8de1ff" },
              ],
            },
          },
          capabilities: ["canvas", "animation"],
        },
      ],
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-page-visible"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "true");
  assert.equal(raf.count(), 1);

  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-page-visible"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "false");
  assert.equal(raf.count(), 0);

  env.document.visibilityState = "visible";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-page-visible"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "true");
  assert.equal(raf.count(), 1);

  raf.flush(16);
  assert.equal(raf.count(), 1);
});

test("bootstrap defers offscreen shared-runtime Scene3D rerenders until the mount re-enters the viewport", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-intersection-runtime";
  mount.width = 640;
  const renderArgs = [];

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/scene-intersection-runtime.json": { text: '{"name":"IntersectionScene"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-intersection-runtime",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-intersection-runtime",
          runtime: "shared",
          programRef: "/scene-intersection-runtime.json",
          props: {
            width: 640,
            height: 360,
            autoRotate: false,
            background: "#08151f",
          },
        },
      ],
    },
    onHydrateEngine: () => "[]",
    onRenderEngine: (...args) => {
      renderArgs.push(args);
      return JSON.stringify({
        background: "#08151f",
        camera: { x: 0, y: 0, z: 6, fov: 72 },
        positions: [],
        colors: [],
        vertexCount: 0,
        worldPositions: [],
        worldColors: [],
        worldVertexCount: 0,
        objects: [],
        labels: [],
      });
    },
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(renderArgs.length, 1);
  assert.equal(env.intersectionObservers.length, 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-in-viewport"), "true");
  assert.equal(raf.count(), 1);

  env.intersectionObservers[0].trigger([
    { target: mount, isIntersecting: false, intersectionRatio: 0 },
  ]);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-in-viewport"), "false");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "false");
  assert.equal(raf.count(), 0);

  mount.width = 320;
  env.resizeObservers[0].trigger([mount]);
  await flushAsyncWork();

  assert.equal(renderArgs.length, 1);

  env.intersectionObservers[0].trigger([
    { target: mount, isIntersecting: true, intersectionRatio: 1 },
  ]);
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-in-viewport"), "true");
  assert.equal(mount.getAttribute("data-gosx-scene3d-active"), "true");
  assert.equal(raf.count(), 1);

  raf.flush(16);
  await flushAsyncWork();

  const last = renderArgs[renderArgs.length - 1];
  assert.deepEqual(last.slice(2, 4), [320, 180]);
});

test("bootstrap does not load engine JS via jsRef (eval escape-hatch removed)", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "escape-root";

  const env = createContext({
    elements: [mount],
    manifest: {
      engines: [
        {
          id: "gosx-engine-1",
          component: "SpecialCanvas",
          kind: "surface",
          mountId: "escape-root",
          props: { mode: "escape" },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  // Engine does not mount because no factory is registered for "SpecialCanvas".
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.deepEqual(env.engineMounts, []);
});

test("bootstrap connects hubs and forwards events into shared signals", async () => {
  function makeSocket(url) {
    return {
      url,
      closeCalled: false,
      onmessage: null,
      onclose: null,
      onerror: null,
      close() {
        this.closeCalled = true;
      },
    };
  }

  const env = createContext({
    createWebSocket: makeSocket,
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      hubs: [
        {
          id: "gosx-hub-0",
          name: "presence",
          path: "/gosx/hub/presence",
          bindings: [
            { event: "snapshot", signal: "$presence" },
          ],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.hubs.size, 1);
  assert.equal(env.sockets.length, 1);
  assert.equal(env.sockets[0].url, "ws://localhost:3000/gosx/hub/presence");

  env.sockets[0].onmessage({
    data: JSON.stringify({ event: "snapshot", data: { count: 2 } }),
  });

  assert.deepEqual(env.sharedSignalCalls, [
    ["$presence", '{"count":2}'],
  ]);
  assert.equal(env.sockets[0].binaryType, "arraybuffer");

  env.sockets[0].onmessage({
    data: {
      text: async () => JSON.stringify({ event: "snapshot", data: { count: 3 } }),
    },
  });
  await flushAsyncWork();

  env.sockets[0].onmessage({
    data: new Uint8Array([1, 2, 3]).buffer,
  });
  await flushAsyncWork();

  assert.deepEqual(env.sharedSignalCalls, [
    ["$presence", '{"count":2}'],
    ["$presence", '{"count":3}'],
  ]);

  env.context.__gosx_disconnect_hub("gosx-hub-0");
  assert.equal(env.context.__gosx.hubs.size, 0);
  assert.equal(env.sockets[0].closeCalled, true);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("patch applier updates text nodes and treats setHTML as text", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  const counter = new FakeElement("span", null);
  const htmlSink = new FakeElement("pre", null);

  wrapper.id = "gosx-island-patch";
  counter.textContent = "0";
  htmlSink.textContent = "";
  componentRoot.appendChild(counter);
  componentRoot.appendChild(htmlSink);
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
  });

  runScript(patchSource, env.context, "patch.js");
  env.context.__gosx_apply_patches(
    "gosx-island-patch",
    JSON.stringify([
      { kind: 0, path: "0", text: "1" },
      { kind: 9, path: "1", text: "<strong>safe</strong>" },
    ]),
  );

  assert.equal(counter.textContent, "1");
  assert.equal(htmlSink.textContent, "<strong>safe</strong>");
  assert.equal(htmlSink.children.length, 0);
});

test("bootstrap exposes page lifecycle hooks and can re-bootstrap after disposal", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);

  wrapper.id = "gosx-island-2";
  componentRoot.appendChild(new FakeElement("span", null));
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-2",
          component: "Counter",
          props: {},
          programRef: "/counter.json",
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(typeof env.context.__gosx_bootstrap_page, "function");
  assert.equal(typeof env.context.__gosx_dispose_page, "function");
  assert.equal(env.context.__gosx.islands.size, 1);

  await env.context.__gosx_dispose_page();
  assert.equal(env.context.__gosx.islands.size, 0);

  await env.context.__gosx_bootstrap_page();
  await flushAsyncWork();
  assert.equal(env.hydrateCalls.length, 2);
  assert.equal(env.context.__gosx.islands.size, 1);
});

test("navigation runtime swaps managed head/body and calls page lifecycle hooks", async () => {
  const oldMeta = new FakeElement("meta", null);
  oldMeta.setAttribute("name", "description");
  oldMeta.setAttribute("content", "old");

  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Docs";

  const oldBody = new FakeElement("div", null);
  oldBody.id = "old-page";
  oldBody.textContent = "old-page";

  const disposeCalls = [];
  const bootstrapCalls = [];
  const parsedDocs = new Map();

  const env = createContext({
    elements: [link, oldBody],
    fetchRoutes: {
      "http://localhost:3000/docs": {
        text: "__PAGE_DOC__",
        url: "http://localhost:3000/docs",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.document.title = "Old";
  appendManagedHead(env.document, [oldMeta]);
  env.context.__gosx_dispose_page = async function() {
    disposeCalls.push("dispose");
  };
  env.context.__gosx_bootstrap_page = async function() {
    bootstrapCalls.push("bootstrap");
  };

  const nextMeta = new FakeElement("meta", null);
  nextMeta.setAttribute("name", "description");
  nextMeta.setAttribute("content", "new");
  const nextBody = new FakeElement("main", null);
  nextBody.id = "new-page";
  nextBody.textContent = "new-page";

  parsedDocs.set("__PAGE_DOC__", buildNavigatedDocument({
    title: "Docs",
    headNodes: [nextMeta],
    bodyNodes: [nextBody],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  const clickListener = env.document.eventListeners.get("click")[0];
  let prevented = false;
  clickListener({
    type: "click",
    button: 0,
    target: link,
    defaultPrevented: false,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, true);
  assert.deepEqual(disposeCalls, ["dispose"]);
  assert.deepEqual(bootstrapCalls, ["bootstrap"]);
  assert.equal(env.document.title, "Docs");
  assert.equal(env.context.location.href, "http://localhost:3000/docs");
  assert.equal(env.document.getElementById("new-page").textContent, "new-page");
  assert.equal(env.document.head.childNodes[1].getAttribute("content"), "new");
  assert.equal(env.fetchCalls[0].init.headers["X-GoSX-Navigation"], "1");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:navigate");
  assert.equal(env.document.activeElement, env.document.getElementById("new-page"));
  assert.equal(env.document.activeElement.getAttribute("tabindex"), "-1");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.focusTargetId, "new-page");
  assert.equal(env.document.body.childNodes.at(-1).textContent, "Docs");
  assert.equal(env.scrollCalls.length, 1);
  assert.equal(env.scrollCalls[0].length, 1);
  assert.equal(env.scrollCalls[0][0].top, 0);
  assert.equal(env.scrollCalls[0][0].left, 0);
  assert.equal(env.scrollCalls[0][0].behavior, "instant");
});

test("navigation runtime loads patch, lifecycle, and managed scripts before page bootstrap", async () => {
  const parsedDocs = new Map();
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs/runtime");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Runtime";

  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/docs/runtime": {
        text: "__SCRIPT_DOC__",
        url: "http://localhost:3000/docs/runtime",
      },
      "http://localhost:3000/patch.js": {
        text: "window.__scriptOrder.push('patch');",
        url: "http://localhost:3000/patch.js",
      },
      "http://localhost:3000/lifecycle.js": {
        text: "window.__scriptOrder.push('lifecycle');",
        url: "http://localhost:3000/lifecycle.js",
      },
      "http://localhost:3000/managed.js": {
        text: "window.__scriptOrder.push('managed');",
        url: "http://localhost:3000/managed.js",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.__scriptOrder = [];

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const originalBootstrap = env.context.__gosx_bootstrap_page;
  env.context.__gosx_bootstrap_page = async function() {
    env.context.__scriptOrder.push("bootstrap");
    return originalBootstrap();
  };
  env.context.__gosx_dispose_page = async function() {
    env.context.__scriptOrder.push("dispose");
  };

  const patchScript = new FakeElement("script", null);
  patchScript.setAttribute("data-gosx-script", "patch");
  patchScript.setAttribute("src", "/patch.js");

  const lifecycleScript = new FakeElement("script", null);
  lifecycleScript.setAttribute("data-gosx-script", "lifecycle");
  lifecycleScript.setAttribute("src", "/lifecycle.js");

  const managedScript = new FakeElement("script", null);
  managedScript.id = "managed-script";
  managedScript.setAttribute("data-gosx-script", "managed");
  managedScript.setAttribute("src", "/managed.js");

  const nextBody = new FakeElement("main", null);
  nextBody.id = "runtime-page";
  nextBody.textContent = "Runtime page";

  parsedDocs.set("__SCRIPT_DOC__", buildNavigatedDocument({
    title: "Runtime",
    headNodes: [patchScript, lifecycleScript],
    bodyNodes: [nextBody, managedScript],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/runtime");
  await flushAsyncWork();

  assert.deepEqual(env.context.__scriptOrder, [
    "dispose",
    "patch",
    "lifecycle",
    "managed",
    "bootstrap",
  ]);
  assert.equal(env.document.getElementById("runtime-page").textContent, "Runtime page");
  assert.equal(env.document.getElementById("managed-script"), null);
  assert.deepEqual(
    Array.from(env.context.__gosx.document.get().assets.scripts, (entry) => entry.role),
    ["patch", "lifecycle"],
  );
});

test("navigation runtime caches lifecycle scripts across page transitions", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/docs/a": {
        text: "__DOC_A__",
        url: "http://localhost:3000/docs/a",
      },
      "http://localhost:3000/docs/b": {
        text: "__DOC_B__",
        url: "http://localhost:3000/docs/b",
      },
      "http://localhost:3000/shared-lifecycle.js": {
        text: "window.__sharedLifecycleLoads = (window.__sharedLifecycleLoads || 0) + 1;",
        url: "http://localhost:3000/shared-lifecycle.js",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let bootstrapCount = 0;
  const originalBootstrap = env.context.__gosx_bootstrap_page;
  env.context.__gosx_bootstrap_page = async function() {
    bootstrapCount += 1;
    return originalBootstrap();
  };
  env.context.__gosx_dispose_page = async function() {};

  function lifecycleDoc(title, id) {
    const script = new FakeElement("script", null);
    script.setAttribute("data-gosx-script", "lifecycle");
    script.setAttribute("src", "/shared-lifecycle.js");

    const page = new FakeElement("main", null);
    page.id = id;
    page.textContent = title;

    return buildNavigatedDocument({
      title,
      headNodes: [script],
      bodyNodes: [page],
    });
  }

  parsedDocs.set("__DOC_A__", lifecycleDoc("Page A", "page-a"));
  parsedDocs.set("__DOC_B__", lifecycleDoc("Page B", "page-b"));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/a");
  await flushAsyncWork();
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/b");
  await flushAsyncWork();

  assert.equal(env.context.__sharedLifecycleLoads, 1);
  assert.equal(
    env.fetchCalls.filter((call) => call.url === "http://localhost:3000/shared-lifecycle.js").length,
    1,
  );
  assert.equal(bootstrapCount, 2);
  assert.equal(env.document.getElementById("page-b").textContent, "Page B");
});

test("navigation runtime marks current and ancestor links and exposes navigation state", async () => {
  const docsLink = new FakeElement("a", null);
  docsLink.setAttribute("href", "/docs");
  docsLink.setAttribute("data-gosx-link", "");
  docsLink.textContent = "Docs";

  const formsLink = new FakeElement("a", null);
  formsLink.setAttribute("href", "/docs/forms");
  formsLink.setAttribute("data-gosx-link", "");
  formsLink.textContent = "Forms";

  const blogLink = new FakeElement("a", null);
  blogLink.setAttribute("href", "/blog");
  blogLink.setAttribute("data-gosx-link", "");
  blogLink.textContent = "Blog";

  const env = createContext({
    elements: [docsLink, formsLink, blogLink],
  });
  env.context.location.href = "http://localhost:3000/docs/forms";

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(docsLink.getAttribute("data-gosx-link-current-policy"), "auto");
  assert.equal(docsLink.getAttribute("data-gosx-link-current"), "ancestor");
  assert.equal(formsLink.getAttribute("data-gosx-link-current-policy"), "auto");
  assert.equal(formsLink.getAttribute("data-gosx-link-current"), "page");
  assert.equal(formsLink.getAttribute("aria-current"), "page");
  assert.equal(blogLink.getAttribute("data-gosx-link-current-policy"), "auto");
  assert.equal(blogLink.getAttribute("data-gosx-link-current"), "none");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-navigation-state"), "idle");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-navigation-current-path"), "/docs/forms");
  assert.equal(env.context.__gosx_page_nav.getState().currentPath, "/docs/forms");
});

test("navigation runtime honors explicit link current policy", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs/forms");
  link.setAttribute("data-gosx-link", "");
  link.setAttribute("data-gosx-link-current-policy", "none");
  link.setAttribute("data-gosx-link-current", "none");
  link.textContent = "Forms";

  const env = createContext({
    elements: [link],
  });
  env.context.location.href = "http://localhost:3000/docs/forms";

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(link.getAttribute("data-gosx-link-current-policy"), "none");
  assert.equal(link.getAttribute("data-gosx-link-current"), "none");
  assert.equal(link.hasAttribute("aria-current"), false);
});

test("navigation runtime prefetches marked links and reuses cached HTML", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/prefetch");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Prefetch";

  const parsedDocs = new Map();
  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/prefetch": {
        text: "__PREFETCH_DOC__",
        url: "http://localhost:3000/prefetch",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  parsedDocs.set("__PREFETCH_DOC__", buildNavigatedDocument({
    title: "Prefetched",
    bodyNodes: [new FakeElement("div", null)],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const overListener = env.document.eventListeners.get("mouseover")[0];
  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1);
  assert.equal(link.getAttribute("data-gosx-prefetch-state"), "ready");

  const clickListener = env.document.eventListeners.get("click")[0];
  clickListener({
    type: "click",
    button: 0,
    target: link,
    defaultPrevented: false,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    preventDefault() {},
  });
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.document.title, "Prefetched");
});

test("navigation runtime eagerly prefetches render-marked links", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/prefetch");
  link.setAttribute("data-gosx-link", "");
  link.setAttribute("data-gosx-prefetch", "render");
  link.textContent = "Prefetch";

  const parsedDocs = new Map();
  const env = createContext({
    elements: [link],
    fetchRoutes: {
      "http://localhost:3000/prefetch": {
        text: "__PREFETCH_RENDER_DOC__",
        url: "http://localhost:3000/prefetch",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  parsedDocs.set("__PREFETCH_RENDER_DOC__", buildNavigatedDocument({
    title: "Prefetched",
    bodyNodes: [new FakeElement("div", null)],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(link.getAttribute("data-gosx-prefetch-state"), "ready");
});

test("navigation runtime skips intent prefetch under reduced-data conditions", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/prefetch");
  link.setAttribute("data-gosx-link", "");
  link.textContent = "Prefetch";

  const env = createContext({
    elements: [link],
    matchMedia: {
      "(prefers-reduced-data: reduce)": true,
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const overListener = env.document.eventListeners.get("mouseover")[0];
  overListener({ type: "mouseover", target: link });
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 0);
  assert.equal(link.getAttribute("data-gosx-prefetch-state"), "idle");
});

test("navigation runtime leaves non-interceptable links to native handling", async () => {
  const hashLink = new FakeElement("a", null);
  hashLink.setAttribute("href", "#details");
  hashLink.setAttribute("data-gosx-link", "");

  const externalLink = new FakeElement("a", null);
  externalLink.setAttribute("href", "https://example.com/docs");
  externalLink.setAttribute("data-gosx-link", "");

  const downloadLink = new FakeElement("a", null);
  downloadLink.setAttribute("href", "/download");
  downloadLink.setAttribute("data-gosx-link", "");
  downloadLink.setAttribute("download", "");

  const targetLink = new FakeElement("a", null);
  targetLink.setAttribute("href", "/target");
  targetLink.setAttribute("data-gosx-link", "");
  targetLink.setAttribute("target", "_blank");

  const modifiedLink = new FakeElement("a", null);
  modifiedLink.setAttribute("href", "/modified");
  modifiedLink.setAttribute("data-gosx-link", "");

  const env = createContext({
    elements: [hashLink, externalLink, downloadLink, targetLink, modifiedLink],
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const clickListener = env.document.eventListeners.get("click")[0];
  for (const [link, overrides] of [
    [hashLink, {}],
    [externalLink, {}],
    [downloadLink, {}],
    [targetLink, {}],
    [modifiedLink, { ctrlKey: true }],
  ]) {
    let prevented = false;
    clickListener({
      type: "click",
      target: link,
      button: 0,
      ctrlKey: false,
      metaKey: false,
      shiftKey: false,
      altKey: false,
      defaultPrevented: false,
      preventDefault() {
        prevented = true;
        this.defaultPrevented = true;
      },
      ...overrides,
    });
    await flushAsyncWork();
    assert.equal(prevented, false);
  }

  assert.equal(env.fetchCalls.length, 0);
});

test("navigation runtime absolutizes managed asset URLs during navigation", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/docs/runtime/index.html": {
        text: "__ASSET_DOC__",
        url: "http://localhost:3000/docs/runtime/index.html",
      },
      "http://localhost:3000/docs/runtime/runtime.js": {
        text: "window.__navScriptLoaded = (window.__navScriptLoaded || 0) + 1;",
        url: "http://localhost:3000/docs/runtime/runtime.js",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.location.href = "http://localhost:3000/docs/";
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const favicon = new FakeElement("link", null);
  favicon.setAttribute("rel", "icon");
  favicon.setAttribute("href", "./favicon.svg");

  const patchScript = new FakeElement("script", null);
  patchScript.setAttribute("data-gosx-script", "patch");
  patchScript.setAttribute("src", "./runtime.js");

  const image = new FakeElement("img", null);
  image.id = "hero";
  image.setAttribute("src", "./hero.png");
  image.setAttribute("srcset", "./hero.png 1x, ./hero@2x.png 2x");

  const form = new FakeElement("form", null);
  form.id = "signup";
  form.setAttribute("action", "./signup");

  const video = new FakeElement("video", null);
  video.id = "promo";
  video.setAttribute("poster", "./poster.jpg");

  parsedDocs.set("__ASSET_DOC__", buildNavigatedDocument({
    title: "Assets",
    headNodes: [favicon, patchScript],
    bodyNodes: [image, form, video],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/runtime/index.html");

  assert.equal(env.document.head.childNodes[1].getAttribute("href"), "http://localhost:3000/docs/runtime/favicon.svg");
  assert.equal(env.document.getElementById("hero").getAttribute("src"), "http://localhost:3000/docs/runtime/hero.png");
  assert.equal(
    env.document.getElementById("hero").getAttribute("srcset"),
    "http://localhost:3000/docs/runtime/hero.png 1x, http://localhost:3000/docs/runtime/hero@2x.png 2x",
  );
  assert.equal(env.document.getElementById("signup").getAttribute("action"), "http://localhost:3000/docs/runtime/signup");
  assert.equal(env.document.getElementById("promo").getAttribute("poster"), "http://localhost:3000/docs/runtime/poster.jpg");
  assert.equal(env.fetchCalls[1].url, "http://localhost:3000/docs/runtime/runtime.js");
  assert.equal(env.context.__navScriptLoaded, 1);
});

test("navigation runtime honors explicit a11y markers and hash targets", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/docs/a11y#details": {
        text: "__A11Y_DOC__",
        url: "http://localhost:3000/docs/a11y#details",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const main = new FakeElement("section", null);
  main.id = "main-shell";
  main.setAttribute("data-gosx-main", "");
  main.textContent = "Main shell";

  const announce = new FakeElement("p", null);
  announce.setAttribute("data-gosx-announce", "Accessibility docs");
  announce.textContent = "Ignored body copy";
  main.appendChild(announce);

  const target = new FakeElement("section", null);
  target.id = "details";
  target.textContent = "Deep details";
  main.appendChild(target);

  parsedDocs.set("__A11Y_DOC__", buildNavigatedDocument({
    title: "A11y",
    bodyNodes: [main],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/a11y#details");
  await flushAsyncWork();

  const renderedTarget = env.document.getElementById("details");
  assert.equal(env.document.activeElement, renderedTarget);
  assert.equal(renderedTarget.getAttribute("tabindex"), "-1");
  assert.equal(renderedTarget.scrollIntoViewCalls.length, 1);
  assert.equal(renderedTarget.scrollIntoViewCalls[0].length, 1);
  assert.equal(renderedTarget.scrollIntoViewCalls[0][0].behavior, "instant");
  assert.deepEqual(env.scrollCalls, []);
  assert.equal(env.document.body.childNodes.at(-1).textContent, "Accessibility docs");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.announcement, "Accessibility docs");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.focusTargetId, "details");
});

test("navigation runtime preserves scroll when requested and still focuses the target", async () => {
  const parsedDocs = new Map();
  const env = createContext({
    fetchRoutes: {
      "http://localhost:3000/docs/a11y#details": {
        text: "__PRESERVE_SCROLL_DOC__",
        url: "http://localhost:3000/docs/a11y#details",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });

  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const main = new FakeElement("section", null);
  main.id = "main-shell";
  main.setAttribute("data-gosx-main", "");
  main.textContent = "Main shell";

  const target = new FakeElement("section", null);
  target.id = "details";
  target.textContent = "Deep details";
  main.appendChild(target);

  parsedDocs.set("__PRESERVE_SCROLL_DOC__", buildNavigatedDocument({
    title: "Preserve Scroll",
    bodyNodes: [main],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");
  await env.context.__gosx_page_nav.navigate("http://localhost:3000/docs/a11y#details", {
    preserveScroll: true,
    replace: true,
  });
  await flushAsyncWork();

  const renderedTarget = env.document.getElementById("details");
  assert.equal(renderedTarget.scrollIntoViewCalls.length, 0);
  assert.deepEqual(env.scrollCalls, []);
  assert.equal(env.document.activeElement, renderedTarget);
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:navigate");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.replace, true);
  assert.equal(env.document.dispatchedEvents.at(-1).detail.focusTargetId, "details");
});

test("navigation runtime intercepts managed form submissions and forwards action data", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/save");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");

  const input = new FakeElement("input", null);
  input.setAttribute("name", "title");
  input.value = "hello";
  form.appendChild(input);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "intent");
  submitter.setAttribute("value", "publish");
  form.appendChild(submitter);

  const inputBatchCalls = [];
  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/save": {
        text: '{"data":{"$draft.title":"hello"},"redirect":"/done"}',
        url: "http://localhost:3000/save",
      },
      "http://localhost:3000/done": {
        text: "__DONE_DOC__",
        url: "http://localhost:3000/done",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_set_input_batch = function(payload) {
    inputBatchCalls.push(payload);
    return null;
  };
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const doneBody = new FakeElement("main", null);
  doneBody.id = "done";
  doneBody.textContent = "done";
  parsedDocs.set("__DONE_DOC__", buildNavigatedDocument({
    title: "Done",
    bodyNodes: [doneBody],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  let prevented = false;
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-form-mode"), "post");
  assert.equal(prevented, true);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/save");
  assert.equal(env.fetchCalls[0].init.method, "POST");
  assert.equal(env.fetchCalls[0].init.headers.Accept, "application/json");
  assert.equal(env.fetchCalls[0].init.body instanceof FakeFormData, true);
  assert.equal(env.fetchCalls[0].init.body.has("title"), true);
  assert.equal(env.fetchCalls[0].init.body.has("intent"), true);
  assert.deepEqual(inputBatchCalls, ['{"$draft.title":"hello"}']);
  assert.equal(env.context.location.href, "http://localhost:3000/done");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:result");
  assert.equal(form.getAttribute("data-gosx-pending"), null);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
});

test("navigation runtime exposes programmatic managed action submission", async () => {
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-main", "");
  main.setAttribute("data-gosx-csrf-token", "root-token");

  const env = createContext({
    elements: [main],
    fetchRoutes: {
      "http://localhost:3000/play/__actions/pilot": {
        text: '{"ok":true}',
        url: "http://localhost:3000/play/__actions/pilot",
      },
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(typeof env.context.__gosx_submit_action, "function");
  assert.equal(typeof env.context.__gosx_page_nav.submitAction, "function");

  const form = env.context.__gosx_submit_action("/play/__actions/pilot", {
    unitId: "robot-1",
    mode: "manual",
  }, { root: main, keepForm: true });
  await form.__gosxSubmitPromise;

  assert.equal(form.parentNode, main);
  assert.equal(form.getAttribute("action"), "/play/__actions/pilot");
  assert.equal(form.getAttribute("method"), "post");
  assert.equal(form.getAttribute("data-gosx-form"), "");
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/play/__actions/pilot");
  assert.equal(env.fetchCalls[0].init.method, "POST");
  assert.equal(env.fetchCalls[0].init.headers["X-CSRF-Token"], "root-token");
  assert.deepEqual(env.fetchCalls[0].init.body.values, [
    ["unitId", "robot-1"],
    ["mode", "manual"],
    ["csrf_token", "root-token"],
  ]);
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:result");
});

test("navigation runtime intercepts managed GET forms and navigates with query params", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/search");
  form.setAttribute("method", "get");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");

  const query = new FakeElement("input", null);
  query.setAttribute("name", "q");
  query.value = "scene labels";
  form.appendChild(query);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "view");
  submitter.setAttribute("value", "list");
  form.appendChild(submitter);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/search?q=scene+labels&view=list": {
        text: "__SEARCH_DOC__",
        url: "http://localhost:3000/search?q=scene+labels&view=list",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const results = new FakeElement("main", null);
  results.id = "results";
  results.textContent = "results";
  parsedDocs.set("__SEARCH_DOC__", buildNavigatedDocument({
    title: "Search",
    bodyNodes: [results],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  let prevented = false;
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-form-mode"), "get");
  assert.equal(prevented, true);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/search?q=scene+labels&view=list");
  assert.equal(env.fetchCalls[0].init.headers.Accept, "text/html");
  assert.equal(env.context.location.href, "http://localhost:3000/search?q=scene+labels&view=list");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:navigate");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.method, "GET");
  assert.equal(form.getAttribute("data-gosx-pending"), null);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
});

test("navigation runtime restores prior managed form lifecycle attrs after submit", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/search");
  form.setAttribute("method", "get");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "validating");
  form.setAttribute("data-gosx-pending", "queued");

  const query = new FakeElement("input", null);
  query.setAttribute("name", "q");
  query.value = "scene labels";
  form.appendChild(query);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "view");
  submitter.setAttribute("value", "list");
  form.appendChild(submitter);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/search?q=scene+labels&view=list": {
        text: "__RESTORE_FORM_DOC__",
        url: "http://localhost:3000/search?q=scene+labels&view=list",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const results = new FakeElement("main", null);
  results.id = "results";
  results.textContent = "results";
  parsedDocs.set("__RESTORE_FORM_DOC__", buildNavigatedDocument({
    title: "Search",
    bodyNodes: [results],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(form.getAttribute("data-gosx-pending"), "queued");
  assert.equal(form.getAttribute("data-gosx-form-state"), "validating");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:navigate");
});

test("navigation runtime honors submitter overrides and falls back with native semantics", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/save");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");

  const title = new FakeElement("input", null);
  title.setAttribute("name", "title");
  title.value = "hello";
  form.appendChild(title);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "intent");
  submitter.setAttribute("value", "preview");
  submitter.setAttribute("formaction", "/preview");
  submitter.setAttribute("formmethod", "get");
  submitter.formAction = "http://localhost:3000/preview";
  submitter.formMethod = "get";
  form.appendChild(submitter);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/preview?title=hello&intent=preview": {
        text: "__PREVIEW_DOC__",
        url: "http://localhost:3000/preview?title=hello&intent=preview",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const preview = new FakeElement("main", null);
  preview.id = "preview";
  preview.textContent = "preview";
  parsedDocs.set("__PREVIEW_DOC__", buildNavigatedDocument({
    title: "Preview",
    bodyNodes: [preview],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/preview?title=hello&intent=preview");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.method, "GET");

  env.fetchCalls.length = 0;
  submitter.setAttribute("formmethod", "post");
  submitter.setAttribute("formaction", "/missing");
  submitter.formMethod = "post";
  submitter.formAction = "http://localhost:3000/missing";

  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(form.requestSubmitCalls.length, 1);
  assert.equal(form.requestSubmitCalls[0][0], submitter);
  assert.equal(form.hasAttribute("data-gosx-form"), true);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
});

test("navigation runtime ignores default submitter action property when no override attribute exists", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/save");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");

  const title = new FakeElement("input", null);
  title.setAttribute("name", "title");
  title.value = "hello";
  form.appendChild(title);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "intent");
  submitter.setAttribute("value", "publish");
  submitter.formAction = "http://localhost:3000/";
  form.appendChild(submitter);

  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/save": {
        text: '{"ok":true,"message":"saved"}',
        url: "http://localhost:3000/save",
      },
    },
  });

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  let prevented = false;
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, true);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/save");
  assert.equal(env.fetchCalls[0].init.method, "POST");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:form:result");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.action, "http://localhost:3000/save");
});

test("navigation runtime honors submitter override attributes without reflected props", async () => {
  const form = new FakeElement("form", null);
  form.setAttribute("action", "/save");
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-form-state", "idle");

  const title = new FakeElement("input", null);
  title.setAttribute("name", "title");
  title.value = "hello";
  form.appendChild(title);

  const submitter = new FakeElement("button", null);
  submitter.setAttribute("name", "intent");
  submitter.setAttribute("value", "preview");
  submitter.setAttribute("formaction", "/preview-attr");
  submitter.setAttribute("formmethod", "get");
  form.appendChild(submitter);

  const parsedDocs = new Map();
  const env = createContext({
    elements: [form],
    fetchRoutes: {
      "http://localhost:3000/preview-attr?title=hello&intent=preview": {
        text: "__PREVIEW_ATTR_DOC__",
        url: "http://localhost:3000/preview-attr?title=hello&intent=preview",
      },
    },
    parseHTML(html) {
      return parsedDocs.get(html);
    },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const preview = new FakeElement("main", null);
  preview.id = "preview-attr";
  preview.textContent = "preview";
  parsedDocs.set("__PREVIEW_ATTR_DOC__", buildNavigatedDocument({
    title: "Preview Attr",
    bodyNodes: [preview],
  }));

  runScript(navigationSource, env.context, "navigation_runtime.js");

  const submitListener = env.document.eventListeners.get("submit")[0];
  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/preview-attr?title=hello&intent=preview");
  assert.equal(env.document.dispatchedEvents.at(-1).detail.method, "GET");

  env.fetchCalls.length = 0;
  submitter.setAttribute("formtarget", "_blank");
  let prevented = false;

  submitListener({
    type: "submit",
    target: form,
    submitter,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
      this.defaultPrevented = true;
    },
  });
  await flushAsyncWork();

  assert.equal(prevented, false);
  assert.equal(form.requestSubmitCalls.length, 0);
  assert.equal(env.fetchCalls.length, 0);
});

test("bootstrap mounts builtin video engines and bridges shared signals", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-root";
  mount.width = 640;
  mount.height = 360;

  let env;
  env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideo",
          kind: "video",
          mountId: "video-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.mp4",
            volume: 0.5,
            subtitleTracks: [{ id: "en", language: "en", title: "English" }],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.engines.size, 1);

  const video = mount.firstChild;
  assert.ok(video);
  assert.equal(video.tagName, "VIDEO");
  assert.equal(video.volume, 0.5);
  assert.ok(video.loadCalls.length >= 1);

  video.duration = 120;
  video.readyState = 4;
  video.currentTime = 10;
  video.buffered = {
    length: 1,
    start() {
      return 0;
    },
    end() {
      return 25;
    },
  };
  video.dispatchEvent({ type: "timeupdate", target: video });
  await flushAsyncWork();

  assert.equal(sharedSignalValue(env, "$video.position"), 10);
  assert.equal(sharedSignalValue(env, "$video.duration"), 120);
  assert.equal(sharedSignalValue(env, "$video.buffered"), 15);
  assert.deepEqual(sharedSignalValue(env, "$video.viewport"), [640, 360]);
  assert.deepEqual(sharedSignalValue(env, "$video.subtitleTracks"), [
    { id: "en", language: "en", srclang: "en", title: "English", kind: "subtitles", src: "", default: false, forced: false },
  ]);

  env.context.__gosx_notify_shared_signal("$video.command", JSON.stringify("play"));
  await flushAsyncWork();
  assert.equal(video.playCalls.length, 1);
  assert.equal(video.paused, false);

  env.context.__gosx_notify_shared_signal("$video.seek", JSON.stringify(42));
  await flushAsyncWork();
  assert.equal(video.currentTime, 42);
});

test("bootstrap mounts only the first video engine on a page", async () => {
  const firstMount = new FakeElement("div", null);
  firstMount.id = "video-root-a";
  const secondMount = new FakeElement("div", null);
  secondMount.id = "video-root-b";

  const env = createContext({
    elements: [firstMount, secondMount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideoA",
          kind: "video",
          mountId: "video-root-a",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/a.mp4" },
        },
        {
          id: "gosx-engine-1",
          component: "PromoVideoB",
          kind: "video",
          mountId: "video-root-b",
          capabilities: ["video", "fetch", "audio"],
          props: { src: "/media/b.mp4" },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(firstMount.firstChild && firstMount.firstChild.tagName, "VIDEO");
  assert.equal(secondMount.firstChild, null);
  assert.ok(env.consoleLogs.error.some((entry) => entry.includes("only one video engine is supported per page")));
  const issues = env.context.__gosx.listIssues();
  assert.equal(issues.some((issue) => issue.scope === "engine" && issue.source === "gosx-engine-1"), true);
});

test("bootstrap upgrades server-rendered video fallbacks in place and loads explicit subtitle track URLs", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-fallback-root";
  mount.width = 960;
  mount.height = 540;

  const fallback = new FakeElement("video", null);
  fallback.setAttribute("poster", "/media/poster.jpg");
  fallback.setCanPlayType("video/webm", "probably");

  const source = new FakeElement("source", null);
  source.setAttribute("src", "/media/promo.webm");
  source.setAttribute("type", "video/webm");
  fallback.appendChild(source);

  const track = new FakeElement("track", null);
  track.setAttribute("src", "/subs/en-custom.vtt");
  track.setAttribute("kind", "captions");
  track.setAttribute("srclang", "en");
  track.setAttribute("label", "English");
  fallback.appendChild(track);

  mount.appendChild(fallback);

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/subs/en-custom.vtt": {
        text: "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nHello from track",
      },
    },
    onSetSharedSignal(name, payload) {
      if (env && typeof env.context.__gosx_notify_shared_signal === "function") {
        env.context.__gosx_notify_shared_signal(name, payload);
      }
      return null;
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideo",
          kind: "video",
          mountId: "video-fallback-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            poster: "/media/poster.jpg",
            sources: [
              { src: "/media/promo.webm", type: "video/webm" },
              { src: "/media/promo.mp4", type: "video/mp4" },
            ],
            subtitleTrack: "en",
            subtitleTracks: [
              { id: "en", language: "en", title: "English", kind: "captions", src: "/subs/en-custom.vtt" },
            ],
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  const video = mount.firstChild;
  assert.equal(video, fallback);
  assert.equal(video.tagName, "VIDEO");
  assert.equal(video.getAttribute("data-gosx-video"), "true");
  assert.equal(video.getAttribute("poster"), "/media/poster.jpg");
  assert.equal(video.getAttribute("src"), null);
  assert.equal(video.children.length, 2);
  assert.equal(video.children[0], source);
  assert.equal(video.children[1], track);
  assert.ok(video.loadCalls.length >= 1);
  assert.ok(env.fetchCalls.some((call) => call.url === "/subs/en-custom.vtt"));
  assert.deepEqual(sharedSignalValue(env, "$video.subtitleTracks"), [
    {
      id: "en",
      language: "en",
      srclang: "en",
      title: "English",
      kind: "captions",
      src: "/subs/en-custom.vtt",
      default: false,
      forced: false,
    },
  ]);
  assert.equal(sharedSignalValue(env, "$video.subtitleStatus"), "ready");
});

test("bootstrap video engines load HLS.js from the document runtime contract", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "video-hls-root";
  mount.width = 1280;
  mount.height = 720;

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/runtime/vendor/hls.min.js": {
        text: `window.__hlsLoads = [];
window.Hls = function FakeHls() {
  this.attachMedia = function(video) { this.video = video; };
  this.loadSource = function(src) { window.__hlsLoads.push(src); };
  this.on = function() {};
  this.destroy = function() {};
};
window.Hls.isSupported = function() { return true; };
window.Hls.Events = {};`,
      },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-0",
          component: "PromoVideo",
          kind: "video",
          mountId: "video-hls-root",
          capabilities: ["video", "fetch", "audio"],
          props: {
            src: "/media/promo.m3u8",
          },
        },
      ],
    },
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-video",
      pattern: "GET /video",
      path: "/video",
      title: "Video",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: true,
      navigation: false,
    },
    assets: {
      bootstrapMode: "full",
      manifest: true,
      runtimePath: "/runtime.wasm",
      wasmExecPath: "/wasm_exec.js",
      bootstrapPath: "/bootstrap.js",
      hlsPath: "/runtime/vendor/hls.min.js",
      engines: 1,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.fetchCalls.some((call) => call.url === "/runtime/vendor/hls.min.js"), true);
  assert.deepEqual(Array.from(env.context.__hlsLoads || []), ["/media/promo.m3u8"]);

  const mounted = env.context.__gosx.engines.get("gosx-engine-0");
  assert.ok(mounted);
  assert.equal(mounted.handle.video.tagName, "VIDEO");
});

test("bootstrap decompresses compressedPositions for Scene3D points", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-decompress-root";

  // Create a compressedPositions payload matching Go's scalar quantization format.
  // 6 floats: [1.0, 2.0, 3.0, 4.0, 5.0, 6.0]
  // min=1.0, max=6.0, 2-bit quantization (4 levels: 0,1,2,3)
  // Indices: 0, floor((2-1)/(6-1)*3+0.5)=1, 1, 2, 2, 3
  // step = (6-1)/3 = 1.6667
  // Packed 2-bit: indices [0,1,1,2,2,3] → byte layout
  // byte 0: idx0(00) | idx1(01) | idx2(01) | idx3(10) = 0b10010100 = 0x94
  // byte 1: idx4(10) | idx5(11) | pad      | pad      = 0b00001110 = 0x0E
  const packed = Buffer.from([0x94, 0x0E]).toString("base64");

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-decompress",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-decompress-root",
          props: {
            width: 200,
            height: 200,
            background: "#000",
            camera: { x: 0, y: 0, z: 5, fov: 72 },
            scene: {
              points: [
                {
                  id: "compressed-cloud",
                  count: 2,
                  color: "#fff",
                  size: 2,
                  compressedPositions: [
                    {
                      packed: packed,
                      norm: 1.0,    // min value
                      maxVal: 6.0,  // max value
                      dim: 6,
                      bitWidth: 2,
                      count: 6,
                    },
                  ],
                },
              ],
            },
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  // The decompressor should have replaced compressedPositions with positions.
  // Verify the engine mounted and the scene rendered without errors.
  assert.equal(env.consoleLogs.error.length, 0, "expected no errors, got: " + JSON.stringify(env.consoleLogs.error));
  const engineState = env.context.__gosx.engines.get("gosx-engine-decompress");
  assert.ok(engineState, "expected engine to mount");
});

test("selective runtime loads islands feature and shared wasm only when islands are declared", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  wrapper.id = "gosx-island-runtime";
  componentRoot.appendChild(new FakeTextNode("0", null));
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
      "/gosx/bootstrap-feature-islands.js": { text: bootstrapFeatureIslandsSource },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-runtime",
          component: "Counter",
          props: { initial: 1 },
          programRef: "/counter.json",
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-islands.js"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-engines.js"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-hubs.js"), false);
  assert.equal(env.context.__gosx.islands.size, 1);
});

test("selective runtime mounts native JS engines without loading the shared wasm runtime", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "engine-root";

  const env = createContext({
    elements: [mount],
    engineFactories: {
      Painter(context) {
        context.mount.setAttribute("data-mounted", "true");
        return {
          dispose() {
            context.mount.setAttribute("data-disposed", "true");
          },
        };
      },
    },
    fetchRoutes: {
      "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-runtime",
          component: "Painter",
          kind: "surface",
          mountId: "engine-root",
          jsExport: "Painter",
          props: { color: "#8de1ff" },
        },
      ],
    },
  });

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-engines.js"), true);
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(mount.getAttribute("data-mounted"), "true");

  await env.context.__gosx_dispose_page();
  assert.equal(mount.getAttribute("data-disposed"), "true");
});

test("bootstrap blocks engines when required browser capabilities are missing", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "strict-engine-root";
  let factoryCalls = 0;

  const env = createContext({
    elements: [mount],
    engineFactories: {
      StrictRenderer() {
        factoryCalls += 1;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-strict",
          component: "StrictRenderer",
          kind: "surface",
          mountId: "strict-engine-root",
          props: {},
          capabilities: ["canvas", "webgl"],
          requiredCapabilities: ["webgl"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(factoryCalls, 0);
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(mount.getAttribute("data-gosx-engine-capability-state"), "unsupported");
  assert.equal(mount.getAttribute("data-gosx-engine-required-capabilities"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-engine-missing-capabilities"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-runtime-issue"), "capability");
  assert.equal(mount.getAttribute("data-gosx-fallback-active"), "unsupported");
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0].getAttribute("data-gosx-engine-unsupported"), "true");
  assert.ok(mount.children[0].textContent.includes("current browser"));

  const issues = env.context.__gosx.listIssues();
  assert.equal(issues.some((issue) => issue.scope === "engine" && issue.type === "capability" && issue.source === "gosx-engine-strict"), true);
});

test("bootstrap exposes required capability status to mounted engines", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "strict-ready-root";
  const captured = {};

  const env = createContext({
    elements: [mount],
    enableWebGL: true,
    engineFactories: {
      StrictReady(ctx) {
        captured.requiredCapabilities = ctx.requiredCapabilities.slice();
        captured.capabilityStatus = ctx.capabilityStatus;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-strict-ready",
          component: "StrictReady",
          kind: "surface",
          mountId: "strict-ready-root",
          props: {},
          capabilities: ["canvas", "webgl"],
          requiredCapabilities: ["webgl"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(mount.getAttribute("data-gosx-engine-capability-state"), "ready");
  assert.equal(mount.getAttribute("data-gosx-engine-supported-capabilities"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-engine-missing-capabilities"), null);
  assert.deepEqual(Array.from(captured.requiredCapabilities), ["webgl"]);
  assert.deepEqual(Array.from(captured.capabilityStatus.required), ["webgl"]);
  assert.deepEqual(Array.from(captured.capabilityStatus.missing), []);
  assert.deepEqual(Array.from(env.context.__gosx.engines.get("gosx-engine-strict-ready").requiredCapabilities), ["webgl"]);
});

test("selective runtime connects hubs without loading the shared wasm runtime", async () => {
  const sockets = [];
  const fetchRoutes = {
    "/gosx/assets/runtime/bootstrap-feature-hubs.hashed.js": { text: bootstrapFeatureHubsSource },
  };
  const env = createContext({
    createWebSocket(url) {
      const socket = {
        url,
        closeCalled: false,
        close() {
          this.closeCalled = true;
        },
      };
      sockets.push(socket);
      return socket;
    },
    fetchRoutes,
    manifest: {
      hubs: [
        {
          id: "gosx-hub-runtime",
          name: "presence",
          path: "/gosx/hub/presence",
          bindings: [{ event: "snapshot", signal: "$presence" }],
        },
      ],
    },
  });
  const preload = env.document.createElement("link");
  preload.setAttribute("rel", "preload");
  preload.setAttribute("as", "script");
  preload.setAttribute("href", "/gosx/assets/runtime/bootstrap-feature-hubs.hashed.js");
  env.document.head.appendChild(preload);

  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.some((entry) => entry.url === "/runtime.wasm"), false);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/assets/runtime/bootstrap-feature-hubs.hashed.js"), true);
  assert.equal(env.fetchCalls.some((entry) => entry.url === "/gosx/bootstrap-feature-hubs.js"), false);
  assert.equal(sockets.length, 1);
  assert.equal(String(sockets[0].url).includes("/gosx/hub/presence"), true);
});

test("engine factory context does not receive window or document", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scope-test-root";

  const capturedCtx = {};

  const env = createContext({
    elements: [mount],
    engineFactories: {
      ScopeTest(ctx) {
        capturedCtx.hasWindow = "window" in ctx;
        capturedCtx.hasDocument = "document" in ctx;
        capturedCtx.windowValue = ctx.window;
        capturedCtx.documentValue = ctx.document;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-scope",
          component: "ScopeTest",
          kind: "surface",
          mountId: "scope-test-root",
          capabilities: ["canvas"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(capturedCtx.hasWindow, false, "ctx must not expose window");
  assert.equal(capturedCtx.hasDocument, false, "ctx must not expose document");
  assert.equal(capturedCtx.windowValue, undefined, "ctx.window must be undefined");
  assert.equal(capturedCtx.documentValue, undefined, "ctx.document must be undefined");
});

test("engine factory context does not receive activateInputProviders", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "input-scope-root";

  const capturedCtx = {};

  const env = createContext({
    elements: [mount],
    engineFactories: {
      InputScopeTest(ctx) {
        capturedCtx.hasActivateInputProviders = "activateInputProviders" in ctx;
        capturedCtx.hasReleaseInputProviders = "releaseInputProviders" in ctx;
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-input-scope",
          component: "InputScopeTest",
          kind: "surface",
          mountId: "input-scope-root",
          capabilities: [],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.equal(capturedCtx.hasActivateInputProviders, false, "ctx must not expose activateInputProviders");
  assert.equal(capturedCtx.hasReleaseInputProviders, false, "ctx must not expose releaseInputProviders");
});

test("engine factory context does not receive activateInputProviders even with input capabilities", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "input-cap-root";

  const capturedCtx = {};

  const env = createContext({
    elements: [mount],
    engineFactories: {
      InputCapTest(ctx) {
        capturedCtx.hasActivateInputProviders = "activateInputProviders" in ctx;
        capturedCtx.capabilities = ctx.capabilities.slice();
        return { dispose() {} };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-input-cap",
          component: "InputCapTest",
          kind: "surface",
          mountId: "input-cap-root",
          capabilities: ["keyboard", "pointer", "gamepad"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.deepEqual(capturedCtx.capabilities, ["keyboard", "pointer", "gamepad"]);
  assert.equal(capturedCtx.hasActivateInputProviders, false, "ctx must not expose activateInputProviders even with input capabilities");
});
