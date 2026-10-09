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
  env.context.__gosx_action = (...args) => { actions.push(args[1]); return 0; };
  env.context.__gosx = env.context.__gosx || {};
  env.context.__gosx.islands = new Map();
  runScript(source, env.context, "30d-wheel.js");
  return { root, child, env, actions };
}

test("a wheel handler inserted after legacy hydration attaches once and disposal removes it", () => {
  const { root, child, env, actions } = wheelIsland("island-late-wheel");
  const listeners = env.context.__gosx_test_setup_event_delegation(root, root.id);
  env.context.__gosx.islands.set(root.id, { root, listeners });
  assert.equal(root.listenerCount("wheel"), 0, "no wheel listener while no handler exists");
  assert.equal(env.mutationObservers.length, 1);

  // A patch inserts the handler; the observer reports the change.
  child.setAttribute("data-gosx-on-wheel", "zoom");
  env.mutationObservers[0].trigger([{ target: child, type: "attributes", attributeName: "data-gosx-on-wheel" }]);
  assert.equal(root.listenerCount("wheel"), 1);
  assert.equal(env.mutationObservers[0].targets.size, 0, "observer stops once the listener exists");

  root.dispatchEvent({ type: "wheel", target: child, deltaY: -120, preventDefault() {} });
  assert.deepEqual(actions, ["zoom"]);

  env.context.__gosx_test_dispose_island(root.id);
  assert.equal(root.listenerCount("wheel"), 0, "disposal removes the late listener");
});

function root_count(island) { return island.root.listenerCount("wheel"); }

test("islands with no legacy gap and islands that already listen create no observer", () => {
  const present = wheelIsland("island-has-wheel");
  present.child.setAttribute("data-gosx-on-wheel", "zoom");
  present.env.context.__gosx_test_setup_event_delegation(present.root, present.root.id);
  assert.equal(root_count(present), 1, "wheel attached at setup");
  assert.ok(present.env.mutationObservers.every((observer) => observer.targets.size === 0), "nothing left to watch");

  const declared = wheelIsland("island-declared-wheel");
  declared.env.context.__gosx_test_setup_event_delegation(declared.root, declared.root.id, [{ eventType: "wheel" }]);
  assert.equal(declared.env.mutationObservers.length, 0);
});

test("a conditionally rendered wheel handler works on a declared-events island", () => {
  const { root, child, env, actions } = wheelIsland("island-declared-late");
  const listeners = env.context.__gosx_test_setup_event_delegation(root, root.id, [{ eventType: "wheel" }]);
  env.context.__gosx.islands.set(root.id, { root, listeners });
  root.dispatchEvent({ type: "wheel", target: child, deltaY: 1, preventDefault() {} });
  assert.deepEqual(actions, [], "no handler yet");
  child.setAttribute("data-gosx-on-wheel", "zoom");
  root.dispatchEvent({ type: "wheel", target: child, deltaY: 1, preventDefault() {} });
  assert.deepEqual(actions, ["zoom"]);
});
