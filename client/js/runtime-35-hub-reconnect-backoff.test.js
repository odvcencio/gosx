"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { bootstrapSource, createContext, runScript, flushAsyncWork, installManualTimers } = require("./runtime-test-harness.js");

const makeSocket = (url) => ({ url, readyState: 1, onmessage: null, onclose: null, onopen: null, onerror: null, close() { this.readyState = 3; } });

test("reconnect delay grows from 500 ms to a 15 s cap with bounded jitter and resets on open", async () => {
  const env = createContext({ createWebSocket: makeSocket, fetchRoutes: { "/runtime.wasm": { bytes: [0, 97, 115, 109] } }, manifest: { hubs: [{ id: "gosx-hub-0", name: "draft-live", path: "/draft/live", bindings: [] }] } });
  const timers = installManualTimers(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const hubs = env.context.__gosx.host.hubs;
  assert.equal(hubs.reconnectDelayMs(0, 0), 500);
  assert.equal(hubs.reconnectDelayMs(1, 0), 1000);
  assert.equal(hubs.reconnectDelayMs(5, 0), 15000, "the cap holds");
  assert.equal(hubs.reconnectDelayMs(2, 1), 2500, "jitter adds at most 25 percent");
  const record = () => env.context.__gosx.hubs.get("gosx-hub-0");
  env.sockets[0].onclose();
  env.sockets[0].onclose(); // a second close of the same socket schedules nothing new
  assert.equal(record().reconnectAttempt, 1);
  assert.equal(timers.count(), 1);
  timers.runDelay(record().reconnectDelay);
  await flushAsyncWork();
  assert.equal(env.sockets.length, 2);
  env.sockets[1].onclose();
  assert.equal(record().reconnectAttempt, 2);
  timers.runDelay(record().reconnectDelay);
  await flushAsyncWork();
  env.sockets[2].onopen();
  assert.equal(record().reconnectAttempt, 0, "a successful open resets the backoff");
});
