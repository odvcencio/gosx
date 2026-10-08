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

test("disposing an island releases pointers captured by its handlers", () => {
  const root = new FakeElement("div", null);
  const handle = new FakeElement("div", null);
  root.id = "island-capture";
  handle.setAttribute("data-gosx-on-pointerdown", "grab");
  root.appendChild(handle);
  const captured = new Set();
  const released = [];
  handle.setPointerCapture = (id) => { captured.add(id); };
  handle.hasPointerCapture = (id) => captured.has(id);
  handle.releasePointerCapture = (id) => { captured.delete(id); released.push(id); };

  const env = createContext({ elements: [root] });
  // The handler asks for capture the way browser.CapturePointer does.
  env.context.__gosx_action = () => { handle.setPointerCapture(5); return 0; };
  env.context.__gosx = env.context.__gosx || {};
  env.context.__gosx.islands = new Map();
  runScript(source, env.context, "30d-dispose.js");
  const listeners = env.context.__gosx_test_setup_event_delegation(root, root.id, [{ eventType: "pointerdown" }]);
  env.context.__gosx.islands.set(root.id, { root, listeners });

  root.dispatchEvent({ type: "pointerdown", target: handle, pointerId: 5 });
  assert.ok(captured.has(5));
  env.context.__gosx_test_dispose_island(root.id);
  assert.deepEqual(released, [5]);
  assert.equal(captured.size, 0);
  assert.equal(env.context.__gosx.islands.has(root.id), false);
});

test("disposal skips pointers the browser already released", () => {
  const root = new FakeElement("div", null);
  const handle = new FakeElement("div", null);
  root.id = "island-released";
  handle.setAttribute("data-gosx-on-pointerdown", "grab");
  root.appendChild(handle);
  let held = true;
  let releaseCalls = 0;
  handle.hasPointerCapture = () => held;
  handle.releasePointerCapture = () => { releaseCalls++; };
  const env = createContext({ elements: [root] });
  env.context.__gosx_action = () => 0;
  env.context.__gosx = env.context.__gosx || {};
  env.context.__gosx.islands = new Map();
  runScript(source, env.context, "30d-dispose.js");
  const listeners = env.context.__gosx_test_setup_event_delegation(root, root.id, [{ eventType: "pointerdown" }]);
  env.context.__gosx.islands.set(root.id, { root, listeners });
  root.dispatchEvent({ type: "pointerdown", target: handle, pointerId: 2 });
  held = false;
  env.context.__gosx_test_dispose_island(root.id);
  assert.equal(releaseCalls, 0);
});
