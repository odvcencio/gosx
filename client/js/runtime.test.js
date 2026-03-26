const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const bootstrapSource = fs.readFileSync(path.join(__dirname, "bootstrap.js"), "utf8");
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
  constructor() {
    this.fillStyle = "";
    this.strokeStyle = "";
    this.lineWidth = 1;
  }

  beginPath() {}
  clearRect() {}
  closePath() {}
  fill() {}
  fillRect() {}
  lineTo() {}
  moveTo() {}
  restore() {}
  save() {}
  scale() {}
  stroke() {}
  translate() {}
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
    this._canvasContext = null;
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

  focus() {
    this.ownerDocument.activeElement = this;
  }

  getContext(kind) {
    if (this.tagName !== "CANVAS" || kind !== "2d") {
      return null;
    }
    if (!this._canvasContext) {
      this._canvasContext = new FakeCanvasContext2D();
    }
    return this._canvasContext;
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

  async arrayBuffer() {
    return Uint8Array.from(this._bytes).buffer;
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

function createContext(options) {
  const document = new FakeDocument();
  const consoleSpy = createConsoleSpy();
  const hydrateCalls = [];
  const actionCalls = [];
  const disposeCalls = [];
  const engineMounts = [];
  const engineDisposals = [];
  const sharedSignalCalls = [];
  const sockets = [];
  const fetchCalls = [];
  const windowListeners = new Map();

  const routes = new Map();
  for (const [url, response] of Object.entries(options.fetchRoutes || {})) {
    routes.set(url, response);
  }

  const context = {
    Array,
    ArrayBuffer,
    JSON,
    Map,
    Promise,
    Set,
    Uint8Array,
    clearTimeout,
    console: consoleSpy.console,
    CustomEvent: class CustomEvent {
      constructor(type, init = {}) {
        this.type = type;
        this.detail = init.detail;
      }
    },
    document,
    fetch: async (url, init = {}) => {
      fetchCalls.push({ url, init });
      if (!routes.has(url)) {
        throw new Error("unexpected fetch: " + url);
      }
      return new FakeResponse(routes.get(url));
    },
    location: {
      protocol: "http:",
      host: "localhost:3000",
      href: "http://localhost:3000/",
      origin: "http://localhost:3000",
    },
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
        context.__gosx_set_shared_signal = (...args) => {
          sharedSignalCalls.push(args);
          if (typeof options.onSetSharedSignal === "function") {
            return options.onSetSharedSignal(...args);
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
    addEventListener(type, listener) {
      if (!windowListeners.has(type)) {
        windowListeners.set(type, []);
      }
      windowListeners.get(type).push(listener);
    },
    dispatchEvent(event) {
      const listeners = windowListeners.get(event.type) || [];
      for (const listener of listeners) {
        listener(event);
      }
      return true;
    },
    scrollTo() {},
    setTimeout,
    WebAssembly: {
      instantiate: async () => ({ instance: {} }),
      instantiateStreaming: async () => ({ instance: {} }),
    },
  };

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
    engineDisposals,
    engineMounts,
    fetchCalls,
    hydrateCalls,
    sharedSignalCalls,
    sockets,
    windowListeners,
  };
}

function runScript(source, context, filename) {
  vm.runInContext(source, context, { filename });
}

async function flushAsyncWork() {
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
  assert.deepEqual(env.document.dispatchedEvents.map((event) => event.type), ["gosx:ready"]);

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
  assert.equal(mount.children.length, 1);
  assert.equal(mount.firstElementChild.tagName, "CANVAS");
  assert.equal(mount.firstElementChild.getAttribute("width"), "640");
  assert.equal(mount.firstElementChild.getAttribute("height"), "360");

  env.context.__gosx_dispose_engine("gosx-engine-2");
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(mount.children.length, 0);
  assert.equal(env.consoleLogs.warn.length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
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
      { kind: 8, path: "1", text: "<strong>safe</strong>" },
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
  const nextBody = new FakeElement("div", null);
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
  assert.equal(env.document.body.textContent, "new-page");
  assert.equal(env.document.getElementById("new-page").textContent, "new-page");
  assert.equal(env.document.head.childNodes[1].getAttribute("content"), "new");
  assert.equal(env.fetchCalls[0].init.headers["X-GoSX-Navigation"], "1");
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:navigate");
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
