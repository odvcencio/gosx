import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));
const connections = [
  fs.readFileSync(path.join(dirname, "..", "runtime", "host", "compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(dirname, "..", "runtime", "host", "hubs.ts"), "utf8"),
].join("\n");
const disconnect = fs.readFileSync(path.join(dirname, "..", "runtime", "host", "hub-disposal.ts"), "utf8");

function createContext() {
  let nextTimer = 1;
  let fetchEpoch = { started: 0, applied: 0 };
  let navigationPhase = "idle";
  const timers = new Map();
  const signalCalls = [];
  const refreshCalls = [];
  const errors = [];
  const window = {
    __gosx: {
      hubs: new Map(),
      navigation: {
        refresh() {
          return { phase: "idle" };
        },
        revalidate(options) {
          refreshCalls.push(options);
          return Promise.resolve(true);
        },
        getFetchEpoch() {
          return { started: fetchEpoch.started, applied: fetchEpoch.applied };
        },
        getState() {
          return { phase: navigationPhase };
        },
      },
    },
    location: { protocol: "https:", host: "example.test" },
  };
  const context = {
    ArrayBuffer,
    JSON,
    Map,
    Number,
    Promise,
    String,
    Uint8Array,
    URL,
    clearTimeout(id) { timers.delete(id); },
    console: { error(...args) { errors.push(args); } },
    document: { dispatchEvent() {} },
    CustomEvent: class CustomEvent {},
    setSharedSignalJSON(signal, value) {
      signalCalls.push([signal, value]);
      return "";
    },
    setTimeout(callback, delay) {
      const id = nextTimer++;
      timers.set(id, { callback, delay });
      return id;
    },
    window,
  };
  vm.createContext(context);
  vm.runInContext(`(function(){${connections}\n${disconnect}\nwindow.__test_applyHubBindings = applyHubBindings;})();`, context);
  return {
    context,
    errors,
    refreshCalls,
    signalCalls,
    timers,
    flushTimers() {
      const callbacks = Array.from(timers.values(), (timer) => timer.callback);
      timers.clear();
      callbacks.forEach((callback) => callback());
    },
    runTimer(id) {
      const timer = timers.get(id);
      assert.ok(timer, `missing timer ${id}`);
      timers.delete(id);
      timer.callback();
    },
    setFetchEpoch(started, applied) {
      fetchEpoch = { started: Number(started), applied: Number(applied) };
    },
    setNavigationPhase(phase) {
      navigationPhase = String(phase);
    },
  };
}

test("different hub bindings share one rearmed refresh timer and false scroll policy wins", async () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-0",
      bindings: [
        {
          event: "agenda.changed",
          signal: "$agenda",
          refresh: true,
          refreshDebounceMs: 10,
          refreshPreserveScroll: false,
        },
        {
          event: "presence.changed",
          refresh: true,
          refreshDebounceMs: 40,
          refreshPreserveScroll: true,
        },
      ],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(record.entry.id, record);

  env.context.window.__test_applyHubBindings(record, { event: "welcome", data: { ready: true } });
  assert.deepEqual(env.signalCalls, []);
  assert.equal(env.timers.size, 0);

  env.context.window.__test_applyHubBindings(record, { event: "agenda.changed", data: { revision: 1 } });
  const firstTimer = Array.from(env.timers.keys())[0];
  assert.equal(env.timers.size, 1);
  assert.equal(Array.from(env.timers.values())[0].delay, 10);

  env.context.window.__test_applyHubBindings(record, { event: "presence.changed", data: { online: 2 } });
  assert.equal(env.timers.size, 1);
  const secondTimer = Array.from(env.timers.keys())[0];
  assert.notEqual(secondTimer, firstTimer);
  assert.equal(Array.from(env.timers.values())[0].delay, 40);
  assert.deepEqual(env.signalCalls, [["$agenda", '{"revision":1}']]);

  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(env.refreshCalls.length, 1);
  assert.equal(env.refreshCalls[0].preserveScroll, false);
  assert.equal(record.refreshTimer, null);
  assert.equal(record.refreshPreserveScroll, null);
  assert.equal(record.refreshFetchEpoch, null);
  assert.deepEqual(env.errors, []);
});

test("repeated matching hub events extend the same connection debounce window", () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-1",
      bindings: [{ event: "changed", refresh: true, refreshDebounceMs: 25 }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(record.entry.id, record);

  env.context.window.__test_applyHubBindings(record, { event: "changed", data: 1 });
  const firstTimer = Array.from(env.timers.keys())[0];
  env.context.window.__test_applyHubBindings(record, { event: "changed", data: 2 });
  const secondTimer = Array.from(env.timers.keys())[0];

  assert.notEqual(secondTimer, firstTimer);
  assert.equal(env.timers.size, 1);
  assert.equal(Array.from(env.timers.values())[0].delay, 25);
});

test("hub records debounce independently", async () => {
  const env = createContext();
  const first = {
    entry: {
      id: "gosx-hub-a",
      bindings: [{ event: "changed", refresh: true, refreshPreserveScroll: false }],
    },
    socket: { close() {} },
  };
  const second = {
    entry: {
      id: "gosx-hub-b",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(first.entry.id, first);
  env.context.window.__gosx.hubs.set(second.entry.id, second);

  env.context.window.__test_applyHubBindings(first, { event: "changed", data: null });
  const firstTimer = Array.from(env.timers.keys())[0];
  env.context.window.__test_applyHubBindings(second, { event: "changed", data: null });
  const secondTimer = Array.from(env.timers.keys()).find((id) => id !== firstTimer);
  assert.equal(env.timers.size, 2);

  env.runTimer(firstTimer);
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [false]);
  assert.equal(env.timers.size, 1);

  env.runTimer(secondTimer);
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [false, true]);
});

test("explicit welcome refreshes are supported and disconnect clears only that record", async () => {
  const env = createContext();
  let firstClosed = false;
  const first = {
    entry: {
      id: "gosx-hub-welcome",
      bindings: [{ event: "welcome", refresh: true }],
    },
    socket: { close() { firstClosed = true; } },
  };
  const second = {
    entry: {
      id: "gosx-hub-live",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(first.entry.id, first);
  env.context.window.__gosx.hubs.set(second.entry.id, second);

  env.context.window.__test_applyHubBindings(first, { event: "welcome", data: null });
  const firstTimer = Array.from(env.timers.keys())[0];
  env.context.window.__test_applyHubBindings(second, { event: "changed", data: null });
  const secondTimer = Array.from(env.timers.keys()).find((id) => id !== firstTimer);
  assert.equal(env.timers.size, 2);

  env.context.window.__gosx_disconnect_hub(first.entry.id);
  assert.equal(firstClosed, true);
  assert.equal(first.refreshTimer, null);
  assert.equal(first.refreshPreserveScroll, null);
  assert.equal(env.timers.has(firstTimer), false);
  assert.equal(env.timers.has(secondTimer), true);

  env.runTimer(secondTimer);
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [true]);
});

test("hub refresh catches synchronous navigation throws", async () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-throws",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(record.entry.id, record);
  env.context.window.__gosx.navigation.revalidate = function() {
    throw new Error("synchronous refresh failure");
  };

  env.context.window.__test_applyHubBindings(record, { event: "changed", data: null });
  assert.doesNotThrow(() => env.flushTimers());
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(env.errors.length, 1);
  assert.match(String(env.errors[0][0]), /gosx-hub-throws\/changed/);
  assert.match(String(env.errors[0][1]), /synchronous refresh failure/);
  assert.equal(record.refreshTimer, null);
  assert.equal(record.refreshPreserveScroll, null);
});

test("hub refresh suppresses an event covered by a newer applied fetch but not a later event", async () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-epoch",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(record.entry.id, record);

  env.context.window.__test_applyHubBindings(record, { event: "changed", data: { revision: 1 } });
  env.setFetchEpoch(1, 1);
  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls, []);

  env.context.window.__test_applyHubBindings(record, { event: "changed", data: { revision: 2 } });
  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [true]);
});

test("a newer started but unapplied fetch never consumes a hub refresh", async () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-unapplied",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(record.entry.id, record);

  env.context.window.__test_applyHubBindings(record, { event: "changed", data: null });
  env.setFetchEpoch(1, 0);
  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [true]);
});

test("an older inflight fetch that applies after the event does not suppress it", async () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-older-fetch",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(record.entry.id, record);
  env.setFetchEpoch(1, 0);

  env.context.window.__test_applyHubBindings(record, { event: "changed", data: null });
  env.setFetchEpoch(1, 1);
  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [true]);
});

test("hub refresh waits for pending navigation and rechecks applied freshness after settlement", async () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-pending-nav",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(record.entry.id, record);

  // A fetch that starts after this event covers it only after a successful
  // page apply. The Hub must not interrupt it while pending.
  env.context.window.__test_applyHubBindings(record, { event: "changed", data: 1 });
  env.setFetchEpoch(1, 0);
  env.setNavigationPhase("pending");
  env.flushTimers();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls, []);
  assert.equal(env.timers.size, 1);

  env.setFetchEpoch(1, 1);
  env.setNavigationPhase("idle");
  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls, []);
  assert.equal(env.timers.size, 0);

  // A fetch already in flight when the event arrives is older than the
  // event, so its completion cannot consume the refresh.
  env.setFetchEpoch(2, 1);
  env.setNavigationPhase("pending");
  env.context.window.__test_applyHubBindings(record, { event: "changed", data: 2 });
  env.flushTimers();
  assert.deepEqual(env.refreshCalls, []);
  assert.equal(env.timers.size, 1);

  env.setFetchEpoch(2, 2);
  env.setNavigationPhase("idle");
  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [true]);

  // A newer failed navigation also leaves the Hub event uncovered.
  env.context.window.__test_applyHubBindings(record, { event: "changed", data: 3 });
  env.setFetchEpoch(3, 2);
  env.setNavigationPhase("pending");
  env.flushTimers();
  assert.equal(env.timers.size, 1);
  env.setNavigationPhase("idle");
  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [true, true]);
});

test("disconnect cancels a Hub refresh waiting on pending navigation", () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-pending-dispose",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  env.context.window.__gosx.hubs.set(record.entry.id, record);
  env.setNavigationPhase("pending");

  env.context.window.__test_applyHubBindings(record, { event: "changed", data: null });
  env.flushTimers();
  assert.equal(env.timers.size, 1);
  env.context.window.__gosx_disconnect_hub(record.entry.id);

  assert.equal(env.timers.size, 0);
  assert.equal(record.refreshTimer, null);
  assert.equal(record.refreshFetchEpoch, null);
});

test("missing fetch epoch API conservatively keeps hub revalidation", async () => {
  const env = createContext();
  const record = {
    entry: {
      id: "gosx-hub-no-epoch",
      bindings: [{ event: "changed", refresh: true }],
    },
    socket: { close() {} },
  };
  delete env.context.window.__gosx.navigation.getFetchEpoch;
  env.context.window.__gosx.hubs.set(record.entry.id, record);

  env.context.window.__test_applyHubBindings(record, { event: "changed", data: null });
  env.flushTimers();
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(env.refreshCalls.map((options) => options.preserveScroll), [true]);
});
