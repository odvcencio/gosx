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
  let now = 1000;
  let fetchEpoch = { started: 0, applied: 0 };
  let navigationPhase = "idle";
  const timers = new Map();
  const intervals = new Map();
  const signalCalls = [];
  const refreshCalls = [];
  const errors = [];
  const sceneObservers = [];
  const documentListeners = new Map();
  const mounts = new Map();
  const document = {
    documentElement: {},
    dispatchEvent() {},
    addEventListener(type, listener) {
      const listeners = documentListeners.get(type) || [];
      listeners.push(listener);
      documentListeners.set(type, listeners);
    },
    removeEventListener(type, listener) {
      documentListeners.set(type, (documentListeners.get(type) || []).filter((current) => current !== listener));
    },
    getElementById(id) { return mounts.get(id) || null; },
    dispatch(type, event) {
      for (const listener of documentListeners.get(type) || []) listener(event);
    },
  };
  class MutationObserver {
    constructor(callback) {
      this.callback = callback;
      this.disconnected = false;
      sceneObservers.push(this);
    }
    observe() {}
    disconnect() { this.disconnected = true; }
  }
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
    Date: { now() { return now; } },
    String,
    Uint8Array,
    URL,
    clearTimeout(id) { timers.delete(id); },
    console: { error(...args) { errors.push(args); } },
    document,
    MutationObserver,
    CustomEvent: class CustomEvent {
      constructor(type, init = {}) {
        this.type = type;
        this.detail = init.detail;
      }
    },
    setSharedSignalJSON(signal, value) {
      signalCalls.push([signal, value]);
      return "";
    },
    setTimeout(callback, delay) {
      const id = nextTimer++;
      timers.set(id, { callback, delay });
      return id;
    },
    setInterval(callback, delay) {
      const id = nextTimer++;
      intervals.set(id, { callback, delay });
      return id;
    },
    clearInterval(id) { intervals.delete(id); },
    window,
  };
  vm.createContext(context);
  vm.runInContext(`(function(){${connections}\n${disconnect}\nwindow.__test_applyHubBindings = applyHubBindings; window.__test_bindHubOutputs = bindHubOutputs; window.__test_startHubRoundTrip = startHubRoundTrip; window.__test_observeHubRoundTrip = observeHubRoundTrip; window.__test_stopHubRoundTrip = stopHubRoundTrip;})();`, context);
  return {
    context,
    document,
    errors,
    mounts,
    refreshCalls,
    sceneObservers,
    signalCalls,
    timers,
    intervals,
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
    setNow(value) { now = Number(value); },
    setNavigationPhase(phase) {
      navigationPhase = String(phase);
    },
};
}

test("hub RTT binding sends a bounded ping and publishes its matching pong time", () => {
  const env = createContext();
  const sent = [];
  const record = {
    entry: {
      id: "tabletop",
      roundTrip: { signal: "$tabletop.rtt", pingEvent: "room:ping", pongEvent: "room:pong", intervalMs: 250 },
    },
    socket: { readyState: 1, send(value) { sent.push(JSON.parse(value)); } },
  };

  env.context.window.__test_startHubRoundTrip(record);
  assert.equal(sent.length, 1, "the first ping should not wait for the interval");
  assert.equal(sent[0].event, "room:ping");
  assert.equal(env.intervals.get(record.roundTripTimer).delay, 250);
  assert.equal(env.context.window.__test_observeHubRoundTrip(record, {
    event: "room:pong", data: { sequence: sent[0].data.sequence + 1 },
  }), false, "an unmatched pong must not publish a measurement");
  env.setNow(1017);
  assert.equal(env.context.window.__test_observeHubRoundTrip(record, {
    event: "room:pong", data: sent[0].data,
  }), true);
  assert.deepEqual(env.signalCalls, [["$tabletop.rtt", "17"]]);
  env.context.window.__test_stopHubRoundTrip(record);
  assert.equal(env.intervals.size, 0, "stopping the binding clears its timer");
});

test("hub scene command bindings keep only the newest batch until the mount is ready", () => {
  const env = createContext();
  const events = [];
  const mount = {
    attributes: new Map(),
    getAttribute(name) { return this.attributes.get(name) || null; },
    setAttribute(name, value) { this.attributes.set(name, String(value)); },
    dispatchEvent(event) { events.push(event); },
  };
  env.mounts.set("tabletop-scene", mount);
  const record = {
    entry: {
      id: "tabletop",
      bindings: [{ event: "scene:update", sceneCommands: true, sceneMountId: "tabletop-scene" }],
    },
  };

  env.context.window.__test_applyHubBindings(record, {
    event: "scene:update",
    data: { revision: 3, commands: [{ kind: 0, objectId: "old" }] },
  });
  env.context.window.__test_applyHubBindings(record, {
    event: "scene:update",
    data: { revision: 4, commands: [{ kind: 0, objectId: "latest" }] },
  });
  assert.equal(events.length, 0);
  assert.equal(env.sceneObservers.length, 1);

  mount.setAttribute("data-gosx-scene3d-command-ready", "true");
  env.sceneObservers[0].callback([]);
  assert.equal(events.length, 1);
  assert.equal(events[0].type, "gosx:scene3d:commands");
  assert.equal(events[0].detail.revision, 4);
  assert.deepEqual(events[0].detail.commands, [{ kind: 0, objectId: "latest" }]);
  assert.equal(env.sceneObservers[0].disconnected, true);

  env.context.window.__test_applyHubBindings(record, {
    event: "scene:update",
    data: { revision: 3, commands: [{ kind: 0, objectId: "stale" }] },
  });
  assert.equal(events.length, 1, "a stale command revision must not roll back the mounted scene");
});

test("hub scene input bindings filter by event kind, mount, and a 15 Hz throttle", () => {
  const env = createContext();
  const sent = [];
  const mount = { id: "scene" };
  env.mounts.set("scene", mount);
  const record = {
    entry: {
      id: "tabletop",
      bindings: [{
        direction: "out",
        event: "scene:pick",
        throttleMs: 66,
        sceneInput: "pick",
        sceneInputKind: "tabletop",
        sceneMountId: "scene",
      }],
    },
    socket: { readyState: 1, send(raw) { sent.push(JSON.parse(raw)); } },
  };
  env.context.window.__test_bindHubOutputs(record);

  env.document.dispatch("gosx:scene3d:input", {
    target: mount,
    detail: { kind: "gizmo-commit", input: { objectId: "prop-1" } },
  });
  env.document.dispatch("gosx:scene3d:input", {
    target: { id: "other" },
    detail: { kind: "pick", input: { objectId: "prop-1" } },
  });
  env.document.dispatch("gosx:scene3d:input", {
    target: mount,
    detail: { kind: "pick", input: { objectId: "prop-1", point: { x: 0.5, y: 0, z: -0.25 } } },
  });
  env.setNow(1050);
  env.document.dispatch("gosx:scene3d:input", {
    target: mount,
    detail: { kind: "pick", input: { objectId: "too-soon" } },
  });
  env.setNow(1067);
  env.document.dispatch("gosx:scene3d:input", {
    target: mount,
    detail: { kind: "pick", input: { objectId: "after-interval" } },
  });

  assert.deepEqual(sent, [
    {
      event: "scene:pick",
      data: { kind: "tabletop", input: { objectId: "prop-1", point: { x: 0.5, y: 0, z: -0.25 } } },
    },
    { event: "scene:pick", data: { kind: "tabletop", input: { objectId: "after-interval" } } },
  ]);
  record.outputUnsubscribers.forEach((unsubscribe) => unsubscribe());
  env.document.dispatch("gosx:scene3d:input", {
    target: mount,
    detail: { kind: "pick", input: { objectId: "prop-2" } },
  });
  assert.equal(sent.length, 2);
});

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
