import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(__dirname, "bootstrap-src", "08-controllers.js"), "utf8");

async function drainMicrotasks(turns = 8) {
  for (let i = 0; i < turns; i += 1) {
    await Promise.resolve();
  }
}

class FakeTarget {
  constructor(tagName = "div") {
    this.tagName = tagName.toUpperCase();
    this.id = "";
    this.name = "";
    this.value = "";
    this.checked = false;
    this.textContent = "";
    this.isContentEditable = false;
    this.listeners = new Map();
  }

  addEventListener(type, listener) {
    const list = this.listeners.get(type) || [];
    list.push(listener);
    this.listeners.set(type, list);
  }

  removeEventListener(type, listener) {
    const list = this.listeners.get(type) || [];
    this.listeners.set(type, list.filter((item) => item !== listener));
  }

  dispatchEvent(event) {
    event.target = event.target || this;
    for (const listener of this.listeners.get(event.type) || []) {
      listener(event);
    }
  }

  closest(selector) {
    if (selector === "button" && this.tagName === "BUTTON") return this;
    if (selector.includes("contenteditable")) return null;
    return null;
  }
}

function createContext(options = {}) {
  const writes = [];
  const sharedValues = new Map([["$theme", "dark"]]);
  const subscribers = new Map();
  const timers = [];
  const document = new FakeTarget("document");
  const button = new FakeTarget("button");
  button.id = "save";
  button.value = "clicked";
  const storage = new Map(options.storageEntries || [["prefs:theme", JSON.stringify("light")]]);
  const aborts = [];
  class AbortController {
    constructor() {
      this.signal = { aborted: false };
      aborts.push(this);
    }
    abort() {
      this.signal.aborted = true;
    }
  }
  const window = {
    __gosx: {},
    location: { href: "https://example.test/app", origin: "https://example.test" },
    localStorage: {
      getItem(key) { return storage.has(key) ? storage.get(key) : null; },
      setItem(key, value) { storage.set(key, value); },
    },
  };
  const fetchImpl = options.fetch || (async (url, init) => {
    assert.equal(url, "https://example.test/api/settings");
    assert.equal(init.credentials, "same-origin");
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      headers: { get: () => "application/json" },
      text: async () => JSON.stringify({ ok: true }),
    };
  });
  const context = {
    window,
    document: Object.assign(document, {
      querySelector(selector) {
        if (selector === "#app") return document;
        if (selector === "button") return button;
        return null;
      },
      baseURI: "https://example.test/app",
    }),
    URL,
    Date,
    AbortController,
    console,
    fetch: fetchImpl,
    setInterval(fn) {
      timers.push(fn);
      return timers.length;
    },
    clearInterval() {},
    clearTimeout() {},
    setSharedSignalValue(signal, value) {
      writes.push({ signal, value });
      sharedValues.set(signal, value);
      const list = subscribers.get(signal) || [];
      for (const listener of list) listener(value, signal);
      return null;
    },
    gosxReadSharedSignal(signal, fallback) {
      return sharedValues.has(signal) ? sharedValues.get(signal) : fallback;
    },
    gosxSubscribeSharedSignal(signal, listener, options = {}) {
      const list = subscribers.get(signal) || [];
      list.push(listener);
      subscribers.set(signal, list);
      if (options.immediate !== false) listener(sharedValues.has(signal) ? sharedValues.get(signal) : null, signal);
      return () => {
        subscribers.set(signal, (subscribers.get(signal) || []).filter((item) => item !== listener));
      };
    },
  };
  vm.createContext(context);
  vm.runInContext(`(function(){${source}\nwindow.__test_mountAllControllers = mountAllControllers;})();`, context);
  return { context, writes, timers, document, button, storage, aborts, sharedValues };
}

test("declarative controller handles signals, keys, timers, fetch, storage, and dispose", async () => {
  const env = createContext();
  const manifest = {
    controllers: [{
      id: "gosx-controller-0",
      config: {
        name: "prefs",
        root: "#app",
        inputs: [{ name: "theme", signal: "$theme", output: "events" }],
        outputs: [{ name: "events", signal: "$events", initial: { ready: true } }],
        keys: [{ code: "KeyK", modifiers: ["meta"], output: "events", preventDefault: true }],
        timers: [{ name: "pulse", output: "events", everyMs: 10, payload: { n: 1 } }],
        resources: [{ name: "settings", url: "/api/settings", output: "events" }],
        storage: {
          namespace: "prefs",
          load: [
            { key: "theme", signal: "$storedTheme", output: "events" },
            { key: "theme", signal: "$storedThemeOnly" },
          ],
          save: [{ key: "theme", signal: "$theme" }],
        },
      },
    }],
  };

  await env.context.window.__test_mountAllControllers(manifest);
  await drainMicrotasks();

  assert.equal(env.context.window.__gosx.controllers.size, 1);
  assert.deepEqual(env.writes[0], { signal: "$events", value: { ready: true } });
  assert.equal(env.writes.some((entry) => entry.value.kind === "input" && entry.value.value === "dark"), true);
  assert.equal(env.writes.some((entry) => entry.value.kind === "resource" && entry.value.result.data.ok === true), true);
  assert.equal(env.writes.some((entry) => entry.value.kind === "storage" && entry.value.value === "light"), true);
  assert.equal(env.writes.some((entry) => entry.signal === "$storedTheme" && entry.value === "light"), true);
  assert.equal(env.writes.some((entry) => entry.signal === "$storedThemeOnly" && entry.value === "light"), true);

  const keyEvent = {
    type: "keydown",
    code: "KeyK",
    key: "k",
    metaKey: true,
    preventDefaultCalled: false,
    preventDefault() { this.preventDefaultCalled = true; },
  };
  env.document.dispatchEvent(keyEvent);
  assert.equal(keyEvent.preventDefaultCalled, true);
  assert.equal(env.writes.at(-1).value.kind, "key");

  env.timers[0]();
  assert.equal(env.writes.at(-1).value.kind, "timer");
  assert.equal(env.writes.at(-1).value.payload.n, 1);

  env.context.setSharedSignalValue("$theme", "blue");
  assert.equal(env.storage.get("prefs:theme"), JSON.stringify("blue"));

  env.context.window.__gosx_dispose_controller("gosx-controller-0");
  const count = env.writes.length;
  env.document.dispatchEvent({ type: "keydown", code: "KeyK", key: "k", metaKey: true });
  assert.equal(env.writes.length, count);
  assert.equal(env.aborts.every((controller) => controller.signal.aborted), false);
});

test("controller resource refresh and polling release completed AbortControllers", async () => {
  const pendingFetches = [];
  const env = createContext({
    fetch: (url, init) => {
      assert.equal(url, "https://example.test/api/settings");
      assert.equal(init.credentials, "same-origin");
      let resolveFetch;
      const promise = new Promise((resolve) => {
        resolveFetch = resolve;
      });
      pendingFetches.push({
        signal: init.signal,
        resolve() {
          resolveFetch({
            ok: true,
            status: 200,
            statusText: "OK",
            headers: { get: () => "application/json" },
            text: async () => JSON.stringify({ ok: true }),
          });
        },
      });
      return promise;
    },
  });
  const manifest = {
    controllers: [{
      id: "gosx-controller-0",
      config: {
        name: "prefs",
        resources: [{
          name: "settings",
          url: "/api/settings",
          output: "events",
          immediate: false,
          pollMs: 10,
          refreshSignal: "$refreshSettings",
        }],
        outputs: [{ name: "events", signal: "$events" }],
      },
    }],
  };

  await env.context.window.__test_mountAllControllers(manifest);
  const record = env.context.window.__gosx.controllers.get("gosx-controller-0");
  assert.equal(record.abortControllers.length, 0);

  env.timers[0]();
  env.context.setSharedSignalValue("$refreshSettings", 1);
  env.context.setSharedSignalValue("$refreshSettings", 2);
  assert.equal(pendingFetches.length, 3);
  assert.equal(pendingFetches[0].signal.aborted, true);
  assert.equal(pendingFetches[1].signal.aborted, true);
  assert.equal(record.abortControllers.length, 3);

  for (const pending of pendingFetches) {
    pending.resolve();
  }
  await drainMicrotasks();
  assert.equal(record.abortControllers.length, 0);
  assert.equal(Object.keys(record.resourceAbort).length, 0);
  assert.equal(env.writes.filter((entry) => entry.value.kind === "resource").length, 1);

  for (let i = 0; i < 5; i += 1) {
    env.timers[0]();
    pendingFetches.at(-1).resolve();
    await drainMicrotasks();
    assert.equal(record.abortControllers.length, 0);
    assert.equal(Object.keys(record.resourceAbort).length, 0);
  }
});

test("stored signal loads win across controller-island mount order while missing or invalid values preserve defaults", async () => {
  const manifest = {
    controllers: [{
      id: "gosx-controller-storage",
      config: {
        name: "storage",
        outputs: [{ name: "events", signal: "$events" }],
        storage: {
          namespace: "prefs",
          load: [{ key: "open", signal: "$open", output: "events" }],
        },
      },
    }],
  };
  const hydrateDefault = (env) => {
    if (!env.sharedValues.has("$open")) env.sharedValues.set("$open", false);
    return env.sharedValues.get("$open");
  };

  const controllerFirst = createContext({ storageEntries: [["prefs:open", "true"]] });
  await controllerFirst.context.window.__test_mountAllControllers(manifest);
  assert.equal(hydrateDefault(controllerFirst), true);

  const islandFirst = createContext({ storageEntries: [["prefs:open", "true"]] });
  assert.equal(hydrateDefault(islandFirst), false);
  await islandFirst.context.window.__test_mountAllControllers(manifest);
  assert.equal(islandFirst.sharedValues.get("$open"), true);

  for (const storageEntries of [[], [["prefs:open", ""]], [["prefs:open", "not-json"]]]) {
    const env = createContext({ storageEntries });
    await env.context.window.__test_mountAllControllers(manifest);
    assert.equal(env.sharedValues.has("$open"), false);
    assert.equal(hydrateDefault(env), false);
    const event = env.writes.find((entry) => entry.signal === "$events");
    assert.equal(event.value.kind, "storage");
    assert.equal(event.value.value, null);
  }
});
