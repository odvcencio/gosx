"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const {
  bootstrapSource,
  bootstrapRuntimeSource,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const relaySource = fs.readFileSync(path.join(__dirname, "relay.js"), "utf8");

function previewContext({ search = "", iframe = false, storage = new Map(), blockedStorage = false } = {}) {
  const env = createContext({
    manifest: { preview: true, runtime: { path: "/runtime.wasm" }, islands: [] },
    fetchRoutes: { "/runtime.wasm": { bytes: [0, 97, 115, 109] } },
  });
  env.context.location.search = search;
  env.context.parent = iframe ? {} : env.context;
  env.context.sessionStorage = {
    getItem(key) {
      if (blockedStorage) throw new Error("storage unavailable");
      return storage.get(key) || null;
    },
    setItem(key, value) {
      if (blockedStorage) throw new Error("storage unavailable");
      storage.set(key, value);
    },
  };
  const OriginalGo = env.context.Go;
  env.runtimeStarts = 0;
  env.context.Go = function() {
    const go = new OriginalGo();
    const run = go.run;
    go.run = function() {
      env.runtimeStarts++;
      return run();
    };
    return go;
  };
  return env;
}

async function boot(source, env) {
  runScript(relaySource, env.context, "relay.js");
  runScript(source, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushAsyncWork();
}

for (const [name, source] of [
  ["selective", bootstrapRuntimeSource],
  ["compatibility", bootstrapSource],
]) {
  for (const search of ["", "?gosx-preview=0", "?gosx-preview=10", "?other=gosx-preview=1"]) {
    test(`preview ${name} public visitor does not start WASM (${search || "no query"})`, async () => {
      const env = previewContext({ search });
      await boot(source, env);
      assert.equal(env.runtimeStarts, 0);
      assert.equal(env.fetchCalls.length, 0);
      assert.equal(env.context.__gosx.ready, true);
      assert.equal(env.consoleLogs.error.length, 0);
    });
  }

  test(`preview ${name} iframe starts WASM without islands and preserves relay delivery`, async () => {
    const env = previewContext({ iframe: true });
    runScript(relaySource, env.context, "relay.js");
    const peer = { postMessage() {} };
    env.context.__gosx_relay_configure([{ prefix: "$preview.", allowedOrigin: "https://editor.example" }]);
    env.context.__gosx_relay_register_peer(peer, "https://editor.example");
    assert.equal(env.windowListeners.get("message").length, 1);
    const message = {
      type: "message",
      origin: "https://editor.example",
      source: peer,
      data: { type: "gosx:shared-signal", name: "$preview.visible", valueJSON: "true" },
    };
    env.context.dispatchEvent(message);
    assert.equal(env.context.__gosx.relay.inboundBuffer.length, 1);

    const received = [];
    const OriginalGo = env.context.Go;
    env.context.Go = function() {
      const go = new OriginalGo();
      const run = go.run;
      go.run = function() {
        assert.equal(typeof env.context.__gosx_relay_send, "function", "relay must precede WASM startup");
        env.context.__gosx_relay_dispatch_inbound = (...args) => received.push(args);
        env.context.__gosx_relay_flush_inbound();
        return run();
      };
      return go;
    };
    runScript(source, env.context, "bootstrap.js");
    await flushAsyncWork();
    await flushAsyncWork();

    assert.equal(env.runtimeStarts, 1);
    assert.equal(env.context.__gosx.ready, true);
    assert.equal(env.hydrateCalls.length, 0);
    assert.equal(env.computeHydrateCalls.length, 0);
    assert.equal(env.fetchCalls.filter(call => call.url === "/runtime.wasm").length, 1);
    assert.equal(env.fetchCalls.filter(call => call.url.includes("bootstrap-feature-islands")).length, 0);
    assert.deepEqual(received, [["$preview.visible", "true", "https://editor.example"]]);
    env.context.dispatchEvent(message);
    assert.equal(received.length, 2, "inbound delivery must continue after runtime readiness");
    assert.equal(env.consoleLogs.error.length, 0);
  });

  for (const blockedStorage of [false, true]) {
    test(`preview ${name} soft navigation keeps preview after dropping its query (blocked storage: ${blockedStorage})`, async () => {
      const storage = new Map();
      const env = previewContext({ search: "?other=1&gosx-preview=1&last=2", storage, blockedStorage });
      await boot(source, env);
      assert.equal(env.runtimeStarts, 1);
      assert.equal(storage.get("gosx-preview"), blockedStorage ? undefined : "1");

      env.context.location.search = "";
      assert.equal(env.context.__gosx.relay.isPreview(), true, "preview context must stay sticky after the query disappears");
      await env.context.__gosx_dispose_page();
      const el = env.document.getElementById("gosx-manifest");
      const replacement = env.document.createElement("script");
      replacement.id = el.id;
      replacement.textContent = el.textContent;
      el.parentNode.insertBefore(replacement, el);
      el.remove();
      await env.context.__gosx_bootstrap_page();
      await flushAsyncWork();
      assert.equal(env.runtimeStarts, 1, "navigation must reuse the running bridge");
      assert.equal(env.context.__gosx.ready, true);
      assert.equal(env.consoleLogs.error.length, 0);
    });
  }

  test(`preview ${name} recognizes preview entered through soft navigation`, async () => {
    const env = previewContext();
    await boot(source, env);
    assert.equal(env.runtimeStarts, 0);
    env.context.location.search = "?gosx-preview=1";
    await env.context.__gosx_dispose_page();
    await env.context.__gosx_bootstrap_page();
    await flushAsyncWork();
    assert.equal(env.runtimeStarts, 1);
    assert.equal(env.consoleLogs.error.length, 0);
  });

  test(`preview ${name} restores context from session storage`, async () => {
    const env = previewContext({ storage: new Map([["gosx-preview", "1"]]) });
    await boot(source, env);
    assert.equal(env.runtimeStarts, 1);
    assert.equal(env.consoleLogs.error.length, 0);
  });
}
