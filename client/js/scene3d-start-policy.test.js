"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const source = fs.readFileSync(path.join(__dirname, "../runtime/scene3d/start-policy.ts"), "utf8");
const mountSource = fs.readFileSync(path.join(__dirname, "../runtime/scene3d/mount.ts"), "utf8");

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
    .replaceAll("deadline?: IdleDeadline", "deadline")
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
  const fn = extractFunction("sceneProbeStartBackend", "async function sceneRunStartPolicy");
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
    requestIdleCallback(callback) { window.idleCallback = callback; window.idleRequests = (window.idleRequests || 0) + 1; },
    requestAnimationFrame(callback) { window.paintCallbacks = window.paintCallbacks || []; window.paintCallbacks.push(callback); },
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

function paintPosterTwice(window) {
  const first = window.paintCallbacks.shift();
  assert.equal(typeof first, "function", "first paint frame is scheduled");
  first();
  const second = window.paintCallbacks.shift();
  assert.equal(typeof second, "function", "second frame follows the poster paint");
  second();
}

test("the Scene3D factory applies declarative start policy before creating scene state", () => {
  const gate = mountSource.indexOf('if (props.startPolicy === "idle-visible-hardware" && !(await window.__gosx_scene3d_start(mount, props, ctx, sceneProbeWebGLRenderer, setAttrValue))) return {};');
  const sceneState = mountSource.indexOf("const runtimeScene =", gate);
  assert.notEqual(gate, -1);
  assert.ok(sceneState > gate);
  assert.match(source, /async function sceneRunStartPolicy\(/);
  assert.match(source, /if \(!\(await sceneWaitForIdleVisible\(mount, current\)\)\)/);
});

test("the scene start gate keeps the poster for reduced motion and Save-Data", async () => {
  const gate = sceneIdleGate({ reducedMotion: true, saveData: true });
  assert.equal(await gate.gate(gate.mount, () => true), false);
  assert.equal(gate.observer.last.disconnected, true);
});

test("the scene start gate rejects an offscreen scene", async () => {
  const gate = sceneIdleGate({ offscreen: true });
  let settled = false;
  const result = gate.gate(gate.mount, () => true).then((value) => { settled = true; return value; });
  gate.window.idleCallback({ didTimeout: false });
  paintPosterTwice(gate.window);
  gate.observer.last.trigger(false);
  await Promise.resolve();
  assert.equal(settled, false);
  gate.observer.last.trigger(true);
  assert.equal(await result, true);
});

test("the scene start gate waits while its tab is hidden", async () => {
  const gate = sceneIdleGate({ visibilityState: "hidden" });
  let settled = false;
  const result = gate.gate(gate.mount, () => true).then((value) => { settled = true; return value; });
  gate.window.idleCallback({ didTimeout: false });
  paintPosterTwice(gate.window);
  gate.observer.last.trigger(true);
  await Promise.resolve();
  assert.equal(settled, false);
  gate.document.visibilityState = "visible";
  gate.events.get("document:visibilitychange")();
  assert.equal(await result, true);
});

test("the scene start gate waits for the poster to paint before resolving an idle slice", async () => {
  const gate = sceneIdleGate();
  let settled = false;
  const result = gate.gate(gate.mount, () => true).then((value) => { settled = true; return value; });
  gate.window.idleCallback({ didTimeout: false });
  await Promise.resolve();
  assert.equal(settled, false, "the live renderer waits for the poster's first paint");
  paintPosterTwice(gate.window);
  assert.equal(await result, true);
});

test("a timed-out idle callback keeps the poster until the browser grants an idle slice", async () => {
  const gate = sceneIdleGate();
  let settled = false;
  const result = gate.gate(gate.mount, () => true).then((value) => { settled = true; return value; });

  gate.window.idleCallback({ didTimeout: true });
  await Promise.resolve();
  assert.equal(settled, false);
  assert.equal(gate.window.idleRequests, 2, "a timed-out callback must request another idle slice");

  gate.window.idleCallback({ didTimeout: false });
  paintPosterTwice(gate.window);
  assert.equal(await result, true);
});

test("a hardware WebGPU adapter allows startup even when WebGL is software", async () => {
  let adapterCalls = 0;
  const get = await sceneStartBackend({
    navigator: { gpu: { requestAdapter: async () => { adapterCalls += 1; return { info: { vendor: "NVIDIA", device: "RTX 5070 Ti" } }; } } },
    webgl: { available: true, software: true },
  });
  const result = await get({});
  assert.equal(result.hardware, true);
  assert.equal(result.softwareWebGL, true);
  assert.equal(adapterCalls, 1);
});

test("hardware WebGL skips the duplicate WebGPU adapter preflight", async () => {
  let adapterCalls = 0;
  const get = await sceneStartBackend({
    navigator: { gpu: { requestAdapter: async () => { adapterCalls += 1; return null; } } },
    webgl: { available: true, software: false },
  });
  const result = await get({});
  assert.equal(result.hardware, true);
  assert.equal(result.softwareWebGL, false);
  assert.equal(adapterCalls, 0);
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

test("idle-visible-hardware leaves a poster when only software rendering is available", async () => {
  const sourceStart = source.indexOf("async function sceneRunStartPolicy(");
  assert.notEqual(sourceStart, -1);
  const helper = stripRuntimeTypes(source.slice(sourceStart).trim());
  const attributes = new Map();
  const mount = {
    setAttribute(name, value) { attributes.set(name, String(value)); },
  };
  const context = {
    sceneWaitForIdleVisible: async () => true,
    sceneProbeStartBackend: async () => ({ hardware: false, softwareWebGL: true }),
    setAttrValue(_mount, name, value) { attributes.set(name, String(value)); },
    Boolean,
    Promise,
    String,
  };
  const runPolicy = vm.runInNewContext(`(function() { ${helper}; return sceneRunStartPolicy; })()`, context);

  const shouldStart = await runPolicy(mount, { startPolicy: "idle-visible-hardware" }, { isCurrent: () => true });

  assert.equal(shouldStart, false);
  assert.equal(attributes.get("data-gosx-scene3d-start-state"), "poster");
});
