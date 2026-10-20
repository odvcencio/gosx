"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  bootstrapSource, bootstrapRuntimeSource, bootstrapFeatureHubsSource,
  navigationSource, createContext, runScript, flushAsyncWork,
  buildNavigatedDocument, installManualTimers, sharedSignalValue,
} = require("./runtime-test-harness.js");

function hub(id = "hub-0", name = "live", path = "/hub/live", signal = "$old") {
  return { id, name, path, bindings: [{ event: "changed", signal }] };
}

async function mount(source, entries, state = 1) {
  const env = createContext({
    manifest: { runtime: { path: "/runtime.wasm" }, hubs: entries },
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/gosx/bootstrap-feature-hubs.js": { text: bootstrapFeatureHubsSource },
    },
    createWebSocket(url) {
      return {
        url, readyState: state, closeCount: 0, send() {},
        close() {
          this.closeCount++;
          this.readyState = 3;
          // Browsers can deliver an error before close when a connecting
          // socket is deliberately cancelled. Deliver both synchronously.
          if (this.onerror) this.onerror(new Error("socket cancelled"));
          if (this.onclose) this.onclose();
        },
      };
    },
  });
  // The harness indexes IDs without removing detached nodes. Match browser
  // lookup so a manifest removed by reconciliation is no longer visible.
  const getElementById = env.document.getElementById.bind(env.document);
  env.document.getElementById = function(id) {
    const el = getElementById(id);
    return el && this.documentElement.contains(el) ? el : null;
  };
  runScript(source, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();
  for (const socket of env.sockets) if (state === 1) socket.onopen();
  assert.deepEqual(env.consoleLogs.error, []);
  return env;
}

async function navigate(env, entries) {
  const nextDoc = buildNavigatedDocument({ bodyNodes: [] });
  if (entries !== null) {
    const manifest = JSON.parse(env.document.getElementById("gosx-manifest").textContent);
    manifest.hubs = entries;
    const script = nextDoc.createElement("script");
    script.id = "gosx-manifest";
    script.textContent = JSON.stringify(manifest);
    nextDoc.body.appendChild(script);
  }
  const fetch = env.context.fetch;
  env.context.fetch = async function(url, options) {
    if (String(url).endsWith("/next")) {
      return { ok: true, status: 200, url: String(url), headers: { get() { return null; } }, text: async () => "next-page" };
    }
    return fetch(url, options);
  };
  env.context.DOMParser = class { parseFromString() { return nextDoc; } };
  runScript(navigationSource, env.context, "navigation.js");
  await env.context.__gosx.navigation.navigate("/next");
  await flushAsyncWork();
}

for (const [bundle, source] of [["monolithic", bootstrapSource], ["selective", bootstrapRuntimeSource]]) {
  for (const state of [0, 1]) {
    test(`${bundle} navigation keeps the same ${state === 0 ? "connecting" : "open"} hub and replaces page bindings`, async () => {
      const env = await mount(source, [hub()], state);
      const socket = env.sockets[0];
      await navigate(env, [hub("hub-1", "live", "/hub/live", "$new")]);
      await env.context.__gosx_bootstrap_page();
      await flushAsyncWork();
      assert.equal(env.sockets.length, 1);
      assert.equal(socket.closeCount, 0);
      assert.equal(env.context.__gosx.hubs.get("hub-1").socket, socket);
      assert.equal(env.context.__gosx.hubs.has("hub-0"), false);
      socket.onmessage({ data: JSON.stringify({ event: "changed", data: 7 }) });
      assert.ok(sharedSignalValue(env, "$new") === 7 || env.sharedSignalCalls.some(([signal, value]) => signal === "$new" && value === "7"));
      assert.ok(!env.sharedSignalCalls.some(([signal]) => signal === "$old"));
      assert.deepEqual(env.consoleLogs.error, []);
    });
  }

  for (const entries of [[], null]) {
    test(`${bundle} navigation closes an unwanted hub cleanly ${entries === null ? "without a manifest" : "with an empty hub list"}`, async () => {
      const env = await mount(source, [hub()], 0);
      const socket = env.sockets[0];
      const lateClose = socket.onclose;
      const lateError = socket.onerror;
      await navigate(env, entries);
      const timers = installManualTimers(env.context);
      lateClose();
      lateError(new Error("late cancellation"));
      assert.equal(socket.closeCount, 1);
      assert.equal(env.sockets.length, 1);
      assert.equal(env.context.__gosx.hubs.size, 0);
      assert.equal(timers.count(), 0, "a deliberate close must never reconnect");
      assert.deepEqual(env.consoleLogs.error, []);
    });
  }

  test(`${bundle} navigation replaces a different hub cleanly`, async () => {
    const env = await mount(source, [hub()], 0);
    await navigate(env, [hub("hub-0", "other", "/hub/other", "$new")]);
    assert.equal(env.sockets[0].closeCount, 1);
    assert.equal(env.sockets.length, 2);
    assert.ok(env.sockets[1].url.endsWith("/hub/other"));
    assert.equal(env.context.__gosx.hubs.get("hub-0").socket, env.sockets[1]);
    assert.deepEqual(env.consoleLogs.error, []);
  });

  test(`${bundle} shared hub references release bindings and close only the last reference`, async () => {
    const env = await mount(source, [hub(), hub("hub-1", "live", "/hub/live", "$second")]);
    assert.equal(env.sockets.length, 1);
    const record = env.context.__gosx.hubs.get("hub-0");
    assert.equal(record.refCount, 2);
    assert.equal(env.context.__gosx.hubs.get("hub-1"), record);
    env.context.__gosx_disconnect_hub("hub-0");
    assert.equal(record.refCount, 1);
    assert.equal(env.sockets[0].closeCount, 0);
    env.sockets[0].onmessage({ data: JSON.stringify({ event: "changed", data: 8 }) });
    assert.ok(sharedSignalValue(env, "$second") === 8 || env.sharedSignalCalls.some(([signal, value]) => signal === "$second" && value === "8"));
    assert.ok(!env.sharedSignalCalls.some(([signal]) => signal === "$old"));
    env.context.__gosx_disconnect_hub("hub-1");
    assert.equal(record.refCount, 0);
    assert.equal(env.sockets[0].closeCount, 1);
    assert.equal(env.context.__gosx.hubConnections.size, 0);
    assert.deepEqual(env.consoleLogs.error, []);
  });

  test(`${bundle} reexecuting bootstrap reuses the connection registry`, async () => {
    const env = await mount(source, [hub()]);
    const record = env.context.__gosx.hubs.get("hub-0");
    runScript(source, env.context, "bootstrap.js");
    await flushAsyncWork();
    await flushAsyncWork();
    assert.equal(env.sockets.length, 1);
    assert.equal(env.sockets[0].closeCount, 0);
    assert.equal(env.context.__gosx.hubs.get("hub-0"), record);
    assert.equal(record.refCount, 1);
    assert.deepEqual(env.consoleLogs.error, []);
  });
}
