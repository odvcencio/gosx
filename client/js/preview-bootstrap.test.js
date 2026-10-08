"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const {
  bootstrapSource,
  bootstrapRuntimeSource,
  bootstrapFeatureIslandsSource,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const relaySource = fs.readFileSync(path.join(__dirname, "relay.js"), "utf8");

for (const [name, source, selective] of [
  ["selective", bootstrapRuntimeSource, true],
  ["compatibility", bootstrapSource, false],
]) {
  test(`preview ${name} bootstrap starts WASM without islands and preserves relay delivery`, async () => {
    const env = createContext({
      manifest: { preview: true, runtime: { path: "/runtime.wasm" }, islands: [] },
      fetchRoutes: {
        "/runtime.wasm": { bytes: [0, 97, 115, 109] },
        "/gosx/bootstrap-feature-islands.js": { text: bootstrapFeatureIslandsSource },
      },
    });
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
    let runtimeStarts = 0;
    env.context.Go = function() {
      const go = new OriginalGo();
      const run = go.run;
      go.run = function() {
        runtimeStarts++;
        assert.equal(typeof env.context.__gosx_relay_send, "function", "relay must precede WASM startup");
        env.context.__gosx_relay_dispatch_inbound = (...args) => received.push(args);
        env.context.__gosx_relay_flush_inbound();
        return run();
      };
      return go;
    };

    runScript(source, env.context, `${name}-bootstrap.js`);
    await flushAsyncWork();
    await flushAsyncWork();

    assert.equal(runtimeStarts, 1);
    assert.equal(env.context.__gosx.ready, true);
    assert.equal(env.hydrateCalls.length, 0);
    assert.equal(env.computeHydrateCalls.length, 0);
    assert.equal(env.fetchCalls.filter(call => call.url === "/runtime.wasm").length, 1);
    assert.equal(env.fetchCalls.filter(call => call.url.includes("bootstrap-feature-islands")).length, selective ? 1 : 0);
    assert.deepEqual(received, [["$preview.visible", "true", "https://editor.example"]]);
    env.context.dispatchEvent(message);
    assert.equal(received.length, 2, "inbound delivery must continue after runtime readiness");
    assert.equal(env.consoleLogs.error.length, 0);
  });
}

test("preview accepts islands feature registration before selective bootstrap", async () => {
  const env = createContext({
    manifest: { preview: true, runtime: { path: "/runtime.wasm" }, islands: [] },
    fetchRoutes: { "/runtime.wasm": { bytes: [0, 97, 115, 109] } },
  });
  runScript(relaySource, env.context, "relay.js");
  runScript(bootstrapFeatureIslandsSource, env.context, "bootstrap-feature-islands.js");
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await flushAsyncWork();
  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.fetchCalls.filter(call => call.url.includes("bootstrap-feature-islands")).length, 0);
  assert.equal(env.consoleLogs.error.length, 0);
});
