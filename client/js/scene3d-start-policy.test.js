"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "../runtime/scene3d/mount.ts"), "utf8");

function extractFunction(name, nextName) {
  let start = source.indexOf(`function ${name}(`);
  const end = source.indexOf(`\n  ${nextName}`, start);
  assert.notEqual(start, -1, `${name} must be present in the Scene3D runtime`);
  assert.notEqual(end, -1, `${nextName} must follow ${name}`);
  if (source.slice(start - 6, start) === "async ") start -= 6;
  return stripRuntimeTypes(source.slice(start, end).trim());
}

function stripRuntimeTypes(value) {
  return value
    .replaceAll("mount: HTMLElement", "mount")
    .replaceAll("isCurrent: () => boolean", "isCurrent")
    .replaceAll("props: Record<string, any>", "props")
    .replaceAll("): Promise<boolean> {", ") {")
    .replaceAll("): Promise<{ softwareWebGL: boolean; hardware: boolean }> {", ") {")
    .replaceAll("Promise<boolean>", "Promise")
    .replaceAll("): boolean {", ") {")
    .replaceAll("): void {", ") {")
    .replace(/(target): EventTarget \| null,\s*/g, "$1, ")
    .replace(/(type): string,\s*/g, "$1, ")
    .replace(/(callback): EventListener,\s*/g, "$1, ")
    .replaceAll("options: AddEventListenerOptions | boolean | undefined = undefined", "options = undefined")
    .replaceAll(": (IntersectionObserver & { __gosxIntersecting?: boolean }) | null", "")
    .replaceAll(": MutationObserver | null", "")
    .replaceAll(": ReturnType<typeof setTimeout> | null", "")
    .replaceAll(": Array<() => void>", "")
    .replaceAll("eligible: boolean", "eligible")
    .replaceAll("started: boolean", "started")
    .replaceAll("event: Event", "event")
    .replaceAll("button: HTMLButtonElement | null", "button")
    .replaceAll("note: HTMLElement | null", "note")
    .replaceAll("querySelectorAll<HTMLButtonElement>", "querySelectorAll")
    .replaceAll("querySelectorAll<HTMLElement>", "querySelectorAll")
    .replaceAll(" as any", "");
}

function sceneStartBackend(options) {
  const fn = extractFunction("sceneProbeStartBackend", "function sceneWaitForManualStart");
  const context = {
    window: {
      navigator: options.navigator || {},
      __gosx_scene3d_force_webgl: Boolean(options.forceWebGL),
    },
    sceneProbeWebGLRenderer: () => options.webgl || { available: false, software: false },
  };
  return vm.runInNewContext(`(async function() { ${fn}; return sceneProbeStartBackend; })()`, context);
}

function sceneIdleGate(options = {}) {
  const fn = extractFunction("sceneWaitForIdleVisible", "async function sceneProbeStartBackend");
  const events = new Map();
  const motion = {
    matches: Boolean(options.reducedMotion),
    addEventListener(type, callback) { events.set(`motion:${type}`, callback); },
    removeEventListener(type) { events.delete(`motion:${type}`); },
  };
  const connection = {
    saveData: Boolean(options.saveData),
    addEventListener(type, callback) { events.set(`connection:${type}`, callback); },
    removeEventListener(type) { events.delete(`connection:${type}`); },
  };
  const document = {
    visibilityState: options.visibilityState || "visible",
    documentElement: {},
    getElementById(id) { return id === mount.id ? mount : null; },
    addEventListener(type, callback) { events.set(`document:${type}`, callback); },
    removeEventListener(type) { events.delete(`document:${type}`); },
  };
  const window = {
    innerWidth: 100,
    innerHeight: 100,
    navigator: { connection },
    matchMedia() { return motion; },
    addEventListener(type, callback) { events.set(`window:${type}`, callback); },
    removeEventListener(type) { events.delete(`window:${type}`); },
    requestIdleCallback(callback) { window.idleCallback = callback; },
  };
  const mount = {
    id: "home-scene",
    getBoundingClientRect() {
      return options.offscreen
        ? { left: 150, top: 0, right: 200, bottom: 50, width: 50, height: 50 }
        : { left: 0, top: 0, right: 50, bottom: 50, width: 50, height: 50 };
    },
  };
  class TestIntersectionObserver {
    constructor(callback) { this.callback = callback; TestIntersectionObserver.last = this; }
    observe() {}
    disconnect() { this.disconnected = true; }
    trigger(isIntersecting) {
      this.callback([{ target: mount, isIntersecting, intersectionRatio: isIntersecting ? 1 : 0 }]);
    }
  }
  class TestMutationObserver {
    constructor(callback) { this.callback = callback; }
    observe() {}
    disconnect() {}
  }
  const context = {
    window,
    document,
    IntersectionObserver: TestIntersectionObserver,
    MutationObserver: TestMutationObserver,
    setTimeout,
    clearTimeout,
    Promise,
    Boolean,
    Number,
  };
  const gate = vm.runInNewContext(`(function() { ${fn}; return sceneWaitForIdleVisible; })()`, context);
  return { gate, mount, window, motion, connection, document, events, observer: TestIntersectionObserver };
}

test("the home start gate waits for idle, viewport, visible tab, motion, and Save-Data checks", async () => {
  const gate = sceneIdleGate({ reducedMotion: true, saveData: true });
  let settled = false;
  const result = gate.gate(gate.mount, () => true).then((value) => { settled = true; return value; });

  gate.window.idleCallback();
  gate.observer.last.trigger(true);
  gate.connection.saveData = false;
  gate.events.get("connection:change")();
  await Promise.resolve();
  assert.equal(settled, false, "reduced motion must keep the poster in place");

  gate.motion.matches = false;
  gate.events.get("motion:change")();
  assert.equal(await result, true);
  assert.equal(gate.observer.last.disconnected, true);
});

test("the home start gate rejects an offscreen scene", async () => {
  const gate = sceneIdleGate({ offscreen: true });
  let settled = false;
  const result = gate.gate(gate.mount, () => true).then((value) => { settled = true; return value; });
  gate.window.idleCallback();
  gate.observer.last.trigger(false);
  await Promise.resolve();
  assert.equal(settled, false);
  gate.observer.last.trigger(true);
  assert.equal(await result, true);
});

test("the home start gate waits while its tab is hidden", async () => {
  const gate = sceneIdleGate({ visibilityState: "hidden" });
  let settled = false;
  const result = gate.gate(gate.mount, () => true).then((value) => { settled = true; return value; });
  gate.window.idleCallback();
  gate.observer.last.trigger(true);
  await Promise.resolve();
  assert.equal(settled, false);
  gate.document.visibilityState = "visible";
  gate.events.get("document:visibilitychange")();
  assert.equal(await result, true);
});

test("a hardware WebGPU adapter allows startup even when WebGL is software", async () => {
  const get = await sceneStartBackend({
    navigator: { gpu: { requestAdapter: async () => ({ info: { vendor: "NVIDIA", device: "RTX 5070 Ti" } }) } },
    webgl: { available: true, software: true },
  });
  const result = await get({});
  assert.equal(result.hardware, true);
  assert.equal(result.softwareWebGL, true);
});

test("forced software WebGL waits for manual start", async () => {
  const get = await sceneStartBackend({
    forceWebGL: true,
    navigator: { gpu: { requestAdapter: async () => ({ info: { vendor: "NVIDIA", device: "RTX 5070 Ti" } }) } },
    webgl: { available: true, software: true },
  });
  const result = await get({ forceWebGL: true });
  assert.equal(result.hardware, false);
  assert.equal(result.softwareWebGL, true);
});

test("software WebGPU is rejected when the WebGL fallback is unavailable", async () => {
  const get = await sceneStartBackend({
    navigator: { gpu: { requestAdapter: async () => ({ info: { description: "SwiftShader software adapter" } }) } },
    webgl: { available: false, software: false },
  });
  const result = await get({});
  assert.equal(result.hardware, false);
  assert.equal(result.softwareWebGL, false);
});

test("software WebGL exposes and consumes the matching manual-start control", async () => {
  const mount = { id: "tabletop-scene" };
  const listeners = new Map();
  const attributes = new Set(["hidden"]);
  const button = {
    getAttribute(name) { return name === "data-gosx-scene3d-start" ? mount.id : null; },
    addEventListener(type, callback) { listeners.set(type, callback); },
    removeEventListener(type) { listeners.delete(type); },
    removeAttribute(name) { attributes.delete(name); },
    setAttribute(name) { attributes.add(name); },
  };
  const noteAttributes = new Set(["hidden"]);
  const note = {
    getAttribute(name) { return name === "data-gosx-scene3d-software-note" ? mount.id : null; },
    removeAttribute(name) { noteAttributes.delete(name); },
    setAttribute(name) { noteAttributes.add(name); },
  };
  const sourceStart = source.indexOf("function sceneWaitForManualStart(");
  const sourceEnd = source.indexOf("\n// Advance the capped animation clock", sourceStart);
  assert.notEqual(sourceStart, -1);
  assert.notEqual(sourceEnd, -1);
  const helper = stripRuntimeTypes(source.slice(sourceStart, sourceEnd).trim());
  class TestMutationObserver { observe() {} disconnect() {} }
  const context = {
    document: {
      documentElement: {},
      querySelectorAll(selector) { return selector.includes("software-note") ? [note] : [button]; },
      getElementById(id) { return id === mount.id ? mount : null; },
    },
    window: {
      addEventListener(type, callback) { listeners.set(`window:${type}`, callback); },
      removeEventListener(type) { listeners.delete(`window:${type}`); },
    },
    MutationObserver: TestMutationObserver,
    Promise,
    Boolean,
  };
  const waitForStart = vm.runInNewContext(`(function() { ${helper}; return sceneWaitForManualStart; })()`, context);
  const started = waitForStart(mount, () => true);
  assert.equal(attributes.has("hidden"), false);
  assert.equal(noteAttributes.has("hidden"), false);
  let prevented = false;
  listeners.get("click")({ preventDefault() { prevented = true; } });
  assert.equal(await started, true);
  assert.equal(prevented, true);
  assert.equal(attributes.has("hidden"), true);
  assert.equal(noteAttributes.has("hidden"), true);
});
