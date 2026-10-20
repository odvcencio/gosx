import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { createContext, baseBundle, quad } from "./17-scene-input-pick.test.mjs";

const root = path.dirname(fileURLToPath(import.meta.url));

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName;
    this.attributes = new Map();
    this.style = {};
    this.listeners = new Map();
    this.events = [];
    this.children = [];
    this.parentNode = null;
  }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.has(name) ? this.attributes.get(name) : null; }
  removeAttribute(name) { this.attributes.delete(name); }
  addEventListener(name, listener) {
    if (!this.listeners.has(name)) this.listeners.set(name, []);
    this.listeners.get(name).push(listener);
  }
  removeEventListener(name, listener) {
    this.listeners.set(name, (this.listeners.get(name) || []).filter((item) => item !== listener));
  }
  dispatchEvent(event) {
    this.events.push(event.type);
    for (const listener of this.listeners.get(event.type) || []) listener(event);
    return true;
  }
  appendChild(child) {
    if (child.parentNode) child.parentNode.removeChild(child);
    child.parentNode = this;
    this.children.push(child);
    return child;
  }
  insertBefore(child, reference) {
    if (child.parentNode) child.parentNode.removeChild(child);
    const index = reference ? this.children.indexOf(reference) : -1;
    child.parentNode = this;
    if (index < 0) this.children.push(child);
    else this.children.splice(index, 0, child);
    return child;
  }
  removeChild(child) {
    this.children = this.children.filter((item) => item !== child);
    child.parentNode = null;
    return child;
  }
  focus() { for (const listener of this.listeners.get("focus") || []) listener({ type: "focus" }); }
  blur() { for (const listener of this.listeners.get("blur") || []) listener({ type: "blur" }); }
  key(key) {
    let prevented = false;
    const event = { type: "keydown", key, preventDefault() { prevented = true; } };
    for (const listener of this.listeners.get("keydown") || []) listener(event);
    return prevented;
  }
}

function loadFocusRuntime() {
  const host = new FakeElement("div");
  const context = vm.createContext({
    document: { createElement: (tag) => new FakeElement(tag) },
    sceneStateObjects: (state) => (state && state.objects) || [],
    Map,
    Set,
    Array,
    String,
    Boolean,
    Object,
  });
  vm.runInContext(fs.readFileSync(path.join(root, "../runtime/scene3d/mount-scene-focus.ts"), "utf8"), context, {
    filename: "mount-scene-focus.ts",
  });
  context.__host = host;
  // The focus layer is created lazily, so read it from the host after a sync.
  const layer = new Proxy({}, { get: (_, key) => (host.children[0] || { children: [] })[key] });
  return { context, host, layer };
}

test("a scene without interactive nodes adds no DOM", () => {
  const { context, host } = loadFocusRuntime();
  vm.runInContext(`
    __focus = setupSceneNodeFocusProxies(__host);
    syncSceneNodeFocusProxies(__focus, { meshObjects: [{ id: "plain", label: "Not interactive" }] });`, context);
  assert.equal(host.children.length, 0);
  assert.equal(vm.runInContext("sceneFocusEnabled({ objects: [{ id: 'plain' }] })", context), false);
  assert.equal(vm.runInContext("sceneFocusEnabled({ objects: [{ id: 'a', interactive: true, label: 'A' }] })", context), true);
});

test("scene focus proxies retain scene-node order and accessible names", () => {
  const { context, layer } = loadFocusRuntime();
  vm.runInContext(`
    __focus = setupSceneNodeFocusProxies(__host);
    syncSceneNodeFocusProxies(__focus, { meshObjects: [
      { id: "second", interactive: true, label: "Second node", interactiveOrder: 2 },
      { id: "ignored", label: "Not interactive" },
      { id: "first", interactive: true, label: "First node", interactiveOrder: 1 }
    ] });`, context);
  assert.equal(layer.children.length, 2);
  assert.deepEqual(layer.children.map((el) => el.getAttribute("data-gosx-scene-node")), ["first", "second"]);
  assert.deepEqual(layer.children.map((el) => el.getAttribute("aria-label")), ["First node", "Second node"]);
  assert.deepEqual(layer.children.map((el) => el.getAttribute("tabindex")), ["0", "0"]);
  assert.deepEqual(layer.children.map((el) => el.getAttribute("role")), ["button", "button"]);
});

test("picked pointer interactions reach the proxy in order and mirror state", () => {
  const { context, layer } = loadFocusRuntime();
  vm.runInContext(`
    __focus = setupSceneNodeFocusProxies(__host);
    syncSceneNodeFocusProxies(__focus, { objects: [{ id: "target", interactive: true, label: "Target" }] });
    dispatchSceneNodeFocusPointer(__focus, { type: "move", targetID: "target" });
    dispatchSceneNodeFocusPointer(__focus, { type: "down", targetID: "target" });`, context);
  const proxy = layer.children[0];
  assert.equal(proxy.getAttribute("data-hover"), "true");
  assert.equal(proxy.getAttribute("data-pressed"), "true");
  vm.runInContext(`
    dispatchSceneNodeFocusPointer(__focus, { type: "up", targetID: "target", clicked: true });
    dispatchSceneNodeFocusPointer(__focus, { type: "leave", targetID: "target" });`, context);
  assert.deepEqual(proxy.events, ["pointerenter", "pointerdown", "pointerup", "click", "pointerleave"]);
  assert.equal(proxy.getAttribute("data-hover"), null);
  assert.equal(proxy.getAttribute("data-pressed"), null);
});

test("the existing canvas picker forwards pointer phases to the matching proxy", () => {
  const context = createContext();
  const host = new FakeElement("div");
  const documentListeners = new Map();
  context.document = {
    createElement: (tag) => new FakeElement(tag),
    addEventListener(name, listener) { documentListeners.set(name, listener); },
    removeEventListener(name) { documentListeners.delete(name); },
  };
  context.sceneStateObjects = (state) => (state && state.objects) || [];
  context.__host = host;
  vm.runInContext(fs.readFileSync(path.join(root, "../runtime/scene3d/mount-scene-focus.ts"), "utf8"), context);
  context.__focus = vm.runInContext("setupSceneNodeFocusProxies(__host)", context);
  const pickGeometry = quad(-1);
  context.__bundle = baseBundle(Object.assign({}, pickGeometry, {
    objects: [{ id: "quad", interactive: true, label: "Picked quad", interactiveOrder: 1 }],
  }));
  context.__viewport = { cssWidth: 640, cssHeight: 360 };
  const canvas = {
    listeners: new Map(),
    addEventListener(name, listener) {
      if (!this.listeners.has(name)) this.listeners.set(name, []);
      this.listeners.get(name).push(listener);
    },
    removeEventListener(name, listener) {
      this.listeners.set(name, (this.listeners.get(name) || []).filter((item) => item !== listener));
    },
    getBoundingClientRect() { return { left: 0, top: 0, width: 640, height: 360 }; },
    setPointerCapture() {},
    releasePointerCapture() {},
    fire(name) {
      const event = {
        clientX: 320, clientY: 180, pointerId: 7, pointerType: "mouse",
        button: 0, isPrimary: true, preventDefault() {}, stopPropagation() {},
      };
      for (const listener of this.listeners.get(name) || []) listener(event);
    },
  };
  context.__canvas = canvas;
  vm.runInContext(`
    syncSceneNodeFocusProxies(__focus, __bundle);
    __pick = setupScenePickInteractions(__canvas, {}, function() { return __viewport; }, function() { return __bundle; }, function() {}, true,
      sceneFocusPointerHandler(__focus));`, context);
  canvas.fire("pointermove");
  canvas.fire("pointerdown");
  const proxy = host.children[0].children[0];
  assert.equal(proxy.getAttribute("data-hover"), "true");
  assert.equal(proxy.getAttribute("data-pressed"), "true");
  canvas.fire("pointerup");
  canvas.fire("pointerleave");
  assert.deepEqual(proxy.events, ["pointerenter", "pointerdown", "pointerup", "click", "pointerleave"]);
  assert.equal(proxy.getAttribute("data-hover"), null);
  assert.equal(proxy.getAttribute("data-pressed"), null);
  vm.runInContext("__pick.dispose(); disposeSceneNodeFocusProxies(__focus)", context);
});

test("keyboard focus mirrors hover, Enter and Space click, and disposal removes proxies", () => {
  const { context, host, layer } = loadFocusRuntime();
  vm.runInContext(`
    __focus = setupSceneNodeFocusProxies(__host);
    syncSceneNodeFocusProxies(__focus, { objects: [{ id: "keyboard", interactive: true, label: "Keyboard node" }] });`, context);
  const proxy = layer.children[0];
  proxy.focus();
  assert.equal(proxy.getAttribute("data-hover"), "true");
  assert.deepEqual(proxy.events, ["pointerenter"]);
  assert.equal(proxy.key("Enter"), true);
  assert.deepEqual(proxy.events, ["pointerenter", "click"]);
  assert.equal(proxy.key(" "), true);
  assert.deepEqual(proxy.events, ["pointerenter", "click", "click"]);
  vm.runInContext("disposeSceneNodeFocusProxies(__focus)", context);
  assert.equal(host.children.length, 0, "disposal removes the focus layer with its proxies");
});

test("removing a scene node removes its proxy", () => {
  const { context, layer } = loadFocusRuntime();
  vm.runInContext(`
    __focus = setupSceneNodeFocusProxies(__host);
    syncSceneNodeFocusProxies(__focus, { objects: [{ id: "gone", interactive: true, label: "Gone" }] });
    syncSceneNodeFocusProxies(__focus, { objects: [] });`, context);
  assert.equal(layer.children.length, 0);
});
