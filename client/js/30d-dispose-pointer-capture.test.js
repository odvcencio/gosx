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

test("disposing an island releases pointers still captured by its handlers", () => {
  const handle = new FakeElement("div", null);
  const released = [];
  const release = handle.releasePointerCapture.bind(handle);
  handle.releasePointerCapture = (id) => { released.push(id); release(id); };
  const capturing = { value: true, pointerId: 5 };
  const { root, env } = mount("island-capture", handle, capturing);

  root.dispatchEvent({ type: "pointerdown", target: handle, pointerId: 5 });
  assert.ok(handle.hasPointerCapture(5));
  env.context.__gosx_test_dispose_island(root.id);
  assert.deepEqual(released, [5]);
  assert.equal(handle.hasPointerCapture(5), false);
  assert.equal(env.context.__gosx.islands.has(root.id), false);
});

test("disposal survives a release that throws for an ended pointer", () => {
  const handle = new FakeElement("div", null);
  handle.releasePointerCapture = () => { throw new Error("NotFoundError"); };
  const capturing = { value: true, pointerId: 2 };
  const { root, env } = mount("island-ended", handle, capturing);
  root.dispatchEvent({ type: "pointerdown", target: handle, pointerId: 2 });
  env.context.__gosx_test_dispose_island(root.id);
  assert.equal(env.context.__gosx.islands.has(root.id), false);
});

test("repeated gestures on a mounted island leave no capture records or detached elements", () => {
  const capturing = { value: false, pointerId: 0 };
  const handle = new FakeElement("div", null);
  const { root, record } = mount("island-repeat", handle, capturing);
  const size = () => (record.captures ? record.captures.size : 0);

  for (let id = 1; id <= 200; id++) {
    // A handler that never captures records nothing.
    capturing.value = false;
    root.dispatchEvent({ type: "pointerdown", target: handle, pointerId: id });
    assert.equal(size(), 0, `pointer ${id} without capture`);

    // A capturing gesture is recorded while it lasts and removed when it ends,
    // even though the island has no pointerup or lostpointercapture handler.
    const pointerId = 1000 + id;
    capturing.value = true;
    capturing.pointerId = pointerId;
    root.dispatchEvent({ type: "pointerdown", target: handle, pointerId });
    assert.equal(size(), 1, `pointer ${pointerId} while captured`);
    handle.releasePointerCapture(pointerId);
    handle.dispatchEvent({ type: "lostpointercapture", target: handle, pointerId });
    assert.equal(size(), 0, `pointer ${pointerId} after capture ended`);
    assert.equal(handle.listenerCount("lostpointercapture"), 0, "end-of-capture listener is one-shot");
  }
});
