"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { bootstrapSource, createContext, runScript, flushAsyncWork, installManualTimers, sharedSignalValue, FakeElement } = require("./runtime-test-harness.js");

const makeSocket = (url) => ({ url, readyState: 0, onmessage: null, onclose: null, onopen: null, onerror: null, sent: [], send(text) { this.sent.push(JSON.parse(text)); }, close() { this.readyState = 3; } });

async function boot(entryExtra = {}, contextExtra = {}) {
  const status = new FakeElement("span", null);
  status.setAttribute("data-gosx-hub-status", "live");
  const env = createContext(Object.assign({ elements: [status], createWebSocket: makeSocket, fetchRoutes: { "/runtime.wasm": { bytes: [0, 97, 115, 109] } },
    manifest: { hubs: [Object.assign({ id: "gosx-hub-0", name: "live", path: "/live/ws", bindings: [], connectionSignal: "$live.connection" }, entryExtra)] } }, contextExtra));
  const timers = installManualTimers(env.context);
  const events = [];
  env.context.document.addEventListener("gosx:hub:event", (e) => { if (e.detail.event === "__connection") events.push(e.detail.data); });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const signal = () => sharedSignalValue(env, "$live.connection");
  return { env, timers, events, status, signal, socket: () => env.sockets[env.sockets.length - 1] };
}

test("publishes connecting, open, error and reconnecting to the signal, the status attribute and the __connection event", async () => {
  const h = await boot();
  assert.equal(h.signal().state, "connecting");
  assert.equal(h.status.getAttribute("data-gosx-hub-connection"), "connecting");
  h.socket().readyState = 1; h.socket().onopen();
  assert.equal(h.signal().state, "open");
  assert.equal(h.status.getAttribute("data-gosx-hub-connection"), "open");
  h.socket().onerror(new Error("boom"));
  assert.equal(h.signal().state, "error");
  h.socket().onclose();
  assert.equal(h.signal().state, "reconnecting");
  assert.equal(h.signal().attempt, 1);
  assert.ok(h.signal().reconnectInMs >= 500);
  assert.deepEqual(h.events.map((e) => e.state), ["connecting", "open", "error", "reconnecting"]);
  h.timers.runDelay(h.signal().reconnectInMs);
  await flushAsyncWork();
  assert.equal(h.signal().state, "connecting", "a reconnect attempt publishes connecting again");
});
