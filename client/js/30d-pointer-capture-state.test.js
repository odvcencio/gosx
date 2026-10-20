"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  FakeElement,
  createContext,
  runScript,
} = require("./runtime-test-harness.js");

const host = (name) => fs.readFileSync(path.join(__dirname, "..", "runtime", "host", name), "utf8");
const source = [
  host("compatibility.ts"),
  host("events.ts"),
  host("disposal.ts"),
  "window.__gosx_test_setup_event_delegation = setupEventDelegation;",
  "window.__gosx_test_dispose_island = disposeIsland;",
].join("\n");

// Builds a mounted island whose pointerdown handler captures when capturing.value is true.
function mount(id, handle, capturing) {
  const root = new FakeElement("div", null);
  root.id = id;
  handle.setAttribute("data-gosx-on-pointerdown", "grab");
  root.appendChild(handle);
  const env = createContext({ elements: [root] });
  env.context.__gosx_action = () => {
    if (capturing.value) handle.setPointerCapture(capturing.pointerId);
    return 0;
  };
  env.context.__gosx = env.context.__gosx || {};
  env.context.__gosx.islands = new Map();
  runScript(source, env.context, "30d-dispose.js");
  const listeners = env.context.__gosx_test_setup_event_delegation(root, root.id, [{ eventType: "pointerdown" }]);
  const record = { root, listeners };
  env.context.__gosx.islands.set(root.id, record);
  return { root, env, record };
}

test("many captured and uncaptured gestures keep no per-pointer state on a mounted island", () => {
  const capturing = { value: false, pointerId: 0 };
  const handle = new FakeElement("div", null);
  const { root, record } = mount("island-repeat", handle, capturing);
  const keys = Object.keys(record).sort();
  const listenerCounts = () => [...handle.listeners.keys()].map((type) => handle.listenerCount(type)).join();
  const before = listenerCounts();

  for (let id = 1; id <= 200; id++) {
    capturing.value = id % 2 === 0;
    capturing.pointerId = id;
    root.dispatchEvent({ type: "pointerdown", target: handle, pointerId: id });
    handle.releasePointerCapture(id);
    root.dispatchEvent({ type: "pointerup", target: handle, pointerId: id });
    assert.deepEqual(Object.keys(record).sort(), keys, `pointer ${id} added state to the island record`);
    assert.equal(listenerCounts(), before, `pointer ${id} added a listener`);
  }
});

test("disposing an island with an active capture does not throw", () => {
  const handle = new FakeElement("div", null);
  const capturing = { value: true, pointerId: 5 };
  const { root, env } = mount("island-active", handle, capturing);
  root.dispatchEvent({ type: "pointerdown", target: handle, pointerId: 5 });
  assert.ok(handle.hasPointerCapture(5));
  env.context.__gosx_test_dispose_island(root.id);
  assert.equal(env.context.__gosx.islands.has(root.id), false);
  assert.equal(root.listenerCount("pointerdown"), 0, "delegated listeners are removed");
});

function wheelIsland(id) {
  const root = new FakeElement("div", null);
  const child = new FakeElement("div", null);
  root.id = id;
  root.appendChild(child);
  const env = createContext({ elements: [root] });
  const actions = [];
  const options = [];
  env.context.__gosx_action = (...args) => { actions.push(args[1]); return 0; };
  env.context.__gosx = env.context.__gosx || {};
  env.context.__gosx.islands = new Map();
  const add = root.addEventListener.bind(root);
  root.addEventListener = (type, listener, opts) => { options.push([type, opts]); add(type, listener, opts); };
  runScript(source, env.context, "30d-wheel.js");
  const wheelOptions = () => options.filter(([type]) => type === "wheel").map(([, opts]) => opts);
  const wheel = () => root.dispatchEvent({ type: "wheel", target: child, deltaY: -120, preventDefault() {} });
  const setup = (events) => {
    const listeners = env.context.__gosx_test_setup_event_delegation(root, root.id, events);
    env.context.__gosx.islands.set(root.id, { root, listeners });
  };
  return { root, child, env, actions, wheelOptions, wheel, setup };
}

test("legacy island without a wheel handler at hydration listens passively and still fires a late handler", () => {
  const { root, child, env, actions, wheelOptions, wheel, setup } = wheelIsland("island-legacy-late");
  setup();
  assert.equal(root.listenerCount("wheel"), 1);
  assert.equal(wheelOptions()[0].passive, true, "no handler yet: passive");

  child.setAttribute("data-gosx-on-wheel", "zoom"); // a patch adds the handler
  wheel();
  assert.deepEqual(actions, ["zoom"], "exactly one action");
  assert.equal(wheelOptions().length, 1, "no second listener is added");

  env.context.__gosx_test_dispose_island(root.id);
  assert.equal(root.listenerCount("wheel"), 0, "disposal removes the listener");
});

test("legacy island with a wheel handler at setup listens non-passively", () => {
  const { root, child, env, actions, wheelOptions, wheel, setup } = wheelIsland("island-legacy-present");
  child.setAttribute("data-gosx-on-wheel", "zoom");
  setup();
  assert.equal(wheelOptions()[0].passive, false);
  wheel();
  assert.deepEqual(actions, ["zoom"]);
  env.context.__gosx_test_dispose_island(root.id);
  assert.equal(root.listenerCount("wheel"), 0);
});

test("declared-events islands attach wheel only when declared, non-passively, and fire a late handler", () => {
  const without = wheelIsland("island-declared-none");
  without.setup([{ eventType: "click" }]);
  assert.equal(without.root.listenerCount("wheel"), 0);

  const { root, child, env, actions, wheelOptions, wheel, setup } = wheelIsland("island-declared-late");
  setup([{ eventType: "wheel" }]);
  assert.equal(wheelOptions()[0].passive, false);
  wheel();
  assert.deepEqual(actions, [], "no handler yet");
  child.setAttribute("data-gosx-on-wheel", "zoom");
  wheel();
  assert.deepEqual(actions, ["zoom"]);
  env.context.__gosx_test_dispose_island(root.id);
  assert.equal(root.listenerCount("wheel"), 0);
});
