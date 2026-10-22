"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { bootstrapSource, createContext, runScript, flushAsyncWork, installManualTimers, FakeElement } = require("./runtime-test-harness.js");
const makeSocket = (url) => ({ url, readyState: 1, onmessage: null, onclose: null, onopen: null, onerror: null, close() {} });

async function boot(binding) {
  const mount = new FakeElement("div", null); mount.id = "scene";
  const env = createContext({ elements: [mount], createWebSocket: makeSocket, fetchRoutes: { "/runtime.wasm": { bytes: [0, 97, 115, 109] } },
    manifest: { hubs: [{ id: "gosx-hub-0", name: "scene", path: "/ws", bindings: [Object.assign({ event: "update", sceneMountId: "scene", sceneCommands: true }, binding)] }] } });
  const timers = installManualTimers(env.context);
  const applied = [];
  mount.addEventListener("gosx:scene3d:commands", (e) => applied.push(e.detail.revision));
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const send = (revision) => env.sockets[0].onmessage({ data: JSON.stringify({ event: "update", data: { revision, commands: [] } }) });
  // The harness MutationObserver fake does not watch attributes; run each observer after an attribute change.
  const settle = async () => { for (const observer of env.mutationObservers.slice()) { if (observer.targets.size) observer.trigger(); } await flushAsyncWork(); };
  return { env, mount, timers, applied, send, settle };
}

test("revealed gate holds a command-ready batch until the mount reports its first content frame", async () => {
  const h = await boot({ sceneCommandsWhen: "revealed" });
  h.mount.setAttribute("data-gosx-scene3d-command-ready", "true");
  h.send(1); h.send(2);
  await h.settle();
  assert.deepEqual(h.applied, [], "not revealed yet");
  h.mount.setAttribute("data-gosx-scene3d-revealed", "true");
  await h.settle();
  assert.deepEqual(h.applied, [2], "only the newest revision is applied");
  h.send(3);
  assert.deepEqual(h.applied, [2, 3], "later batches apply at once");
});

test("the timeout applies the pending batch when the scene never reveals", async () => {
  const h = await boot({ sceneCommandsWhen: "revealed", sceneCommandsWhenTimeoutMs: 500 });
  h.mount.setAttribute("data-gosx-scene3d-command-ready", "true");
  const warnings = []; h.env.context.console.warn = (m) => warnings.push(String(m));
  h.send(1);
  h.timers.runDelay(500);
  assert.deepEqual(h.applied, [1]);
  assert.ok(warnings.some((w) => w.includes("before reveal")), "a warning names the fallback");
  h.send(2);
  assert.deepEqual(h.applied, [1, 2], "after the timeout the gate is command-ready only");
});

test("without the gate the existing command-ready behaviour is unchanged", async () => {
  const h = await boot({});
  h.mount.setAttribute("data-gosx-scene3d-command-ready", "true");
  h.send(1);
  assert.deepEqual(h.applied, [1]);
});
