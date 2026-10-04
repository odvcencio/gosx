"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const { createRequire } = require("node:module");
const ts = createRequire(require.resolve("../runtime/package.json"))("typescript");
const { FakeResizeObserver } = require("./runtime-test-harness.js");

function harness() {
  const source = fs.readFileSync(require.resolve("../runtime/scene3d/mount-viewport.ts"), "utf8");
  const start = source.indexOf("function observeSceneViewport(");
  const end = source.indexOf("function initialSceneLifecycleState(", start);
  const timers = new Map();
  const observers = [];
  let next = 0;
  const context = {
    window: { __gosx: {} }, Promise,
    setTimeout(callback) { const id = ++next; timers.set(id, callback); return id; },
    clearTimeout(id) { timers.delete(id); },
    ResizeObserver: class extends FakeResizeObserver {
      constructor(callback) { super(callback); observers.push(this); }
    },
  };
  vm.createContext(context);
  vm.runInContext(ts.transpileModule(source.slice(start, end), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  return { observe: context.observeSceneViewport, timers, observers,
    flush() { const queue = timers; const callbacks = [...queue.values()]; queue.clear(); callbacks.forEach((fn) => fn()); } };
}

test("Scene3D defers ancestor observation beyond descendant resize delivery", async () => {
  const h = harness();
  const mount = {};
  const refreshes = [];
  const stop = h.observe(mount, (reason) => refreshes.push(reason));
  const observer = h.observers[0];
  assert.equal(observer.targets.size, 0, "no ancestor observation during descendant delivery");
  await Promise.resolve();
  assert.equal(observer.targets.size, 0, "microtasks still belong to the current delivery cycle");
  h.flush();
  assert.ok(observer.targets.has(mount));
  observer.trigger([mount]);
  observer.trigger([mount]);
  await Promise.resolve();
  assert.deepEqual(refreshes, ["resize"], "resize delivery remains coalesced");
  stop();
  assert.equal(observer.targets.size, 0);
});

test("Scene3D disposal cancels pending ancestor observation", () => {
  const h = harness();
  const stop = h.observe({}, () => assert.fail("disposed viewport refreshed"));
  const pending = [...h.timers.values()][0];
  stop();
  assert.equal(h.timers.size, 0);
  h.flush();
  pending();
  assert.equal(h.observers[0].targets.size, 0, "a late callback cannot reattach a disposed observer");
});

test("Scene3D disposal suppresses queued resize refreshes", async () => {
  const h = harness();
  const mount = {};
  const stop = h.observe(mount, () => assert.fail("disposed viewport refreshed"));
  h.flush();
  h.observers[0].trigger([mount]);
  stop();
  await Promise.resolve();
});
