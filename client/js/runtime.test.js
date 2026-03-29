const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const bootstrapSource = fs.readFileSync(path.join(__dirname, "bootstrap.js"), "utf8");
const bootstrapLiteSource = fs.readFileSync(path.join(__dirname, "bootstrap-lite.js"), "utf8");
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
  constructor() {
    this.ops = [];
    this.bufferUploads = new Map();
    this._nextBufferID = 1;
    this._boundArrayBuffer = null;
    this.ARRAY_BUFFER = 0x8892;
    this.STATIC_DRAW = 0x88E4;
    this.DYNAMIC_DRAW = 0x88E8;
    this.FLOAT = 0x1406;
    this.LINES = 0x0001;
    this.COLOR_BUFFER_BIT = 0x4000;
    this.DEPTH_BUFFER_BIT = 0x0100;
    this.BLEND = 0x0BE2;
    this.DEPTH_TEST = 0x0B71;
    this.LEQUAL = 0x0203;
    this.ONE = 1;
    this.SRC_ALPHA = 0x0302;
    this.ONE_MINUS_SRC_ALPHA = 0x0303;
    this.VERTEX_SHADER = 0x8B31;
    this.FRAGMENT_SHADER = 0x8B30;
    this.COMPILE_STATUS = 0x8B81;
    this.LINK_STATUS = 0x8B82;
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

  getAttribLocation(_program, name) {
    this.ops.push(["getAttribLocation", name]);
    if (name === "a_position") return 0;
    if (name === "a_color") return 1;
    if (name === "a_material") return 2;
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

  bufferData(target, data, usage) {
    const bufferID = this._boundArrayBuffer && this._boundArrayBuffer.id;
    if (bufferID != null) {
      this.bufferUploads.set(bufferID, Array.from(data || []));
    }
    this.ops.push(["bufferData", target, bufferID, data.length, usage]);
  }

  enableVertexAttribArray(location) {
    this.ops.push(["enableVertexAttribArray", location]);
  }

  vertexAttribPointer(location, size, type, normalized, stride, offset) {
    this.ops.push(["vertexAttribPointer", location, size, type, normalized, stride, offset]);
  }

  drawArrays(mode, first, count) {
    this.ops.push(["drawArrays", mode, first, count]);
  }

  uniform4f(location, x, y, z, w) {
    this.ops.push(["uniform4f", location && location.name, x, y, z, w]);
  }

  uniform1f(location, value) {
    this.ops.push(["uniform1f", location && location.name, value]);
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

  deleteBuffer(_buffer) {
    this.ops.push(["deleteBuffer"]);
  }

  deleteProgram(_program) {
    this.ops.push(["deleteProgram"]);
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
    this.childNodes = [];
    const textNode = new FakeTextNode(value, this.ownerDocument);
    textNode.parentNode = this;
    this.childNodes.push(textNode);
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
    if (this.ownerDocument) {
      adoptNode(node, this.ownerDocument);
    }
    this.childNodes.push(node);

    if (node.nodeType === ELEMENT_NODE && this.ownerDocument) {
      this.ownerDocument.indexNode(node);
    }

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

  getContext(kind) {
    if (this.tagName !== "CANVAS") {
      return null;
    }
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

  cloneNode(deep) {
    const clone = new FakeElement(this.tagName.toLowerCase(), this.ownerDocument);
    for (const [name, value] of this.attributes.entries()) {
      clone.setAttribute(name, value);
    }
    clone.value = this.value;
    clone.selectionStart = this.selectionStart;
    clone.selectionEnd = this.selectionEnd;
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
    this.title = "";
    this.disableCanvas2D = false;
    this.createWebGLContext = null;
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
    console: consoleSpy.console,
    CustomEvent: class CustomEvent {
      constructor(type, init = {}) {
        this.type = type;
        this.detail = init.detail;
      }
    },
    document,
    FormData: FakeFormData,
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
    setTimeout,
    WebAssembly: {
      instantiate: async () => ({ instance: {} }),
      instantiateStreaming: async () => ({ instance: {} }),
    },
  };
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

function runScript(source, context, filename) {
  vm.runInContext(source, context, { filename });
}

async function flushAsyncWork() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));
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
  const releaseBatch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(releaseBatch["$scene.test.drag.active"], false);
  assert.equal(releaseBatch["$scene.test.drag.targetIndex"], -1);
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
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 1) return 0;
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
  env.context.requestAnimationFrame = (callback) => {
    if (rafCount >= 1) return 0;
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
  assert.ok(strokeCount >= 12);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
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

  canvas.dispatchEvent({ type: "webglcontextrestored" });
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), null);
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

test("bootstrap loads explicit JS escape-hatch engines only when configured", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "escape-root";

  const env = createContext({
    elements: [mount],
    fetchRoutes: {
      "/engines/special.js": {
        text: `
          window.__gosx_register_engine_factory("SpecialEngine", function(ctx) {
            window.__engineMounts.push({ id: ctx.id, mountID: ctx.mount.id, props: ctx.props });
            return {
              dispose: function() {
                window.__engineDisposals.push(ctx.id);
              }
            };
          });
        `,
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-1",
          component: "SpecialCanvas",
          kind: "surface",
          mountId: "escape-root",
          jsRef: "/engines/special.js",
          jsExport: "SpecialEngine",
          props: { mode: "escape" },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(env.engineMounts)), [
    {
      id: "gosx-engine-1",
      mountID: "escape-root",
      props: { mode: "escape" },
    },
  ]);

  env.context.__gosx_dispose_engine("gosx-engine-1");
  assert.deepEqual(env.engineDisposals, ["gosx-engine-1"]);
  assert.equal(env.consoleLogs.error.length, 0);
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
