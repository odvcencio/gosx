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

const resync = { event: "resync", minIntervalMs: 1200, periodMs: 180000, offsetMs: 2200, initialDelayMs: 1200 };

test("asks the hub to resend state on visible, focus, online and pageshow, at most once per minIntervalMs", async () => {
  let now = 10000;
  const h = await boot({ resync }, { performanceNow: () => now });
  h.socket().readyState = 1; h.socket().onopen();
  h.timers.runDelay(1200);
  assert.deepEqual(h.socket().sent.at(-1), { event: "resync", data: { reason: "initial" } });
  now += 1300;
  h.env.context.document.visibilityState = "visible";
  h.env.context.document.dispatchEvent({ type: "visibilitychange" });
  assert.equal(h.socket().sent.filter((m) => m.event === "resync").length, 2);
  h.env.context.window.dispatchEvent({ type: "online" });
  assert.equal(h.socket().sent.filter((m) => m.event === "resync").length, 2, "second trigger inside 1.2 s is dropped");
  now += 1300;
  h.env.context.window.dispatchEvent({ type: "focus" });
  assert.equal(h.socket().sent.at(-1).data.reason, "focus");
  now += 1300;
  h.env.context.window.dispatchEvent({ type: "pageshow", persisted: true });
  assert.equal(h.socket().sent.at(-1).data.reason, "pageshow");
  now += 1300;
  h.env.context.document.visibilityState = "hidden";
  h.env.context.document.dispatchEvent({ type: "visibilitychange" });
  assert.equal(h.socket().sent.at(-1).data.reason, "pageshow", "hiding the tab never resyncs");
});

test("schedules a resync after each wall-clock period boundary plus the offset, only while visible", async () => {
  let now = 10000;
  const h = await boot({ resync }, { performanceNow: () => now });
  // 1 s after a period boundary. The vm context has its own Date.
  runScript("globalThis.__realDateNow = Date.now; globalThis.__realRandom = Math.random; Date.now = () => 180000 * 5 + 1000; Math.random = () => 0;", h.env.context, "date-stub.js");
  try {
    const before = h.timers.count();
    h.socket().readyState = 1; h.socket().onopen();
    assert.ok(h.timers.count() > before, "period and initial timers armed");
    now += 5000; h.timers.runDelay(2200 - 1000);
    assert.equal(h.socket().sent.at(-1).data.reason, "period");
  } finally { runScript("Date.now = globalThis.__realDateNow; Math.random = globalThis.__realRandom;", h.env.context, "date-restore.js"); }
});

test("a hub without resync never sends resync", async () => {
  const h = await boot();
  h.socket().readyState = 1; h.socket().onopen();
  h.timers.runDelay(5000);
  h.env.context.window.dispatchEvent({ type: "online" });
  assert.equal(h.socket().sent.filter((m) => m.event === "resync").length, 0);
});

test("the period slot in the current window still counts, and jitter spreads clients", async () => {
  const h = await boot({ resync }, { performanceNow: () => 10000 });
  runScript("globalThis.__realDateNow = Date.now; globalThis.__realRandom = Math.random; Date.now = () => 180000 * 5 + 3000; Math.random = () => 0.5;", h.env.context, "date-stub.js");
  try {
    h.socket().readyState = 1; h.socket().onopen();
    // Slot is boundary + 2200 ms, already past at +3000 ms, so the next one is one period later.
    // Jitter is 0.5 * min(5000, 18000) = 2500 ms.
    h.timers.runDelay(180000 - 3000 + 2200 + 2500);
    assert.equal(h.socket().sent.at(-1).data.reason, "period");
  } finally { runScript("Date.now = globalThis.__realDateNow; Math.random = globalThis.__realRandom;", h.env.context, "date-restore.js"); }
});
