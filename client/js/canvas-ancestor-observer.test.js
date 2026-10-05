"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const { FakeElement, FakeResizeObserver } = require("./runtime-test-harness.js");

function harness() {
  const source = fs.readFileSync(require.resolve("./bootstrap-src/26b-feature-engines-prefix.ts"), "utf8");
  const sizingStart = source.indexOf("function _canvasDeclaredSize(");
  const sizingEnd = source.indexOf("// _ensureSurfaceCanvas", sizingStart);
  const disposeStart = source.indexOf("function _disposeEngineSurface(");
  const disposeEnd = source.indexOf("function paintEngineSurfaceMissing(", disposeStart);
  const timers = new Map();
  let next = 0;
  const context = {
    window: { devicePixelRatio: 1 }, surfaceInstances: new Map(),
    setTimeout(callback) { const id = ++next; timers.set(id, callback); return id; },
    clearTimeout(id) { timers.delete(id); },
  };
  vm.createContext(context);
  vm.runInContext(source.slice(sizingStart, sizingEnd) + source.slice(disposeStart, disposeEnd), context);
  const ancestor = new FakeElement("div", null);
  ancestor.clientWidth = 800;
  ancestor.clientHeight = 600;
  const canvas = new FakeElement("canvas", null);
  canvas.setAttribute("width", "640");
  canvas.setAttribute("height", "400");
  ancestor.appendChild(canvas);
  canvas.getBoundingClientRect = () => ({ width: 0, height: 0 });
  const observer = new FakeResizeObserver(() => context._initEngineSurfaceCanvasSize(canvas));
  canvas.__gosxResizeObserver = observer;
  context.surfaceInstances.set("board", { canvas, kind: "canvas2d", listeners: [], resizeObserver: observer });
  return { context, timers, canvas, ancestor, observer,
    flush() { const callbacks = [...timers.values()]; timers.clear(); callbacks.forEach((fn) => fn()); } };
}

test("canvas recovery defers ancestor observation beyond resize delivery", async () => {
  const h = harness();
  h.observer.trigger([h.canvas]);
  assert.equal(h.canvas.width, 640, "fallback keeps the authored width cap");
  assert.equal(h.canvas.height, 400, "fallback keeps the authored height cap");
  assert.equal(h.observer.targets.size, 0, "no shallower target during descendant delivery");
  await Promise.resolve();
  assert.equal(h.observer.targets.size, 0, "microtasks still belong to the current delivery cycle");
  h.observer.trigger([h.canvas]);
  assert.equal(h.timers.size, 1, "repeated recovery coalesces ancestor registration");
  h.flush();
  assert.ok(h.observer.targets.has(h.ancestor));
});

test("canvas disposal cancels ancestor registration and rejects late callbacks", () => {
  const h = harness();
  h.observer.trigger([h.canvas]);
  const pending = [...h.timers.values()][0];
  h.context._disposeEngineSurface("board");
  assert.equal(h.timers.size, 0);
  pending();
  assert.equal(h.observer.targets.size, 0);
  assert.equal(h.context.surfaceInstances.size, 0);
});

test("a canvas with a real CSS box keeps its direct sizing path", () => {
  const h = harness();
  h.canvas.getBoundingClientRect = () => ({ width: 320, height: 200 });
  h.context.window.devicePixelRatio = 2;
  h.observer.trigger([h.canvas]);
  assert.equal(h.canvas.width, 640);
  assert.equal(h.canvas.height, 400);
  assert.equal(h.timers.size, 0);
});
