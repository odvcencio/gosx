"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const {
 bootstrapRuntimeSource,
 createContext, runScript, flushAsyncWork,
} = require("./runtime-test-harness.js");

const bootstrapFeatureControllersSource = require("node:fs").readFileSync(require("node:path").join(__dirname, "bootstrap-feature-controllers.js"), "utf8");

test("lazy features use the configured prefix when document assets are absent", async () => {
 const env = createContext({
  manifest: { runtime: {}, islands: [], bundles: {}, controllers: [{id: "clock", config: {}}] },
  fetchRoutes: { "/.proxy/game/gosx/bootstrap-feature-controllers.js": { text: bootstrapFeatureControllersSource } },
 });
 const meta = env.document.createElement("meta");
 meta.setAttribute("name", "gosx-base-path");
 meta.setAttribute("content", "/.proxy/game");
 env.document.head.appendChild(meta);
 runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
 await flushAsyncWork();
 assert.ok(env.fetchCalls.some(call => call.url === "/.proxy/game/gosx/bootstrap-feature-controllers.js"));
 assert.ok(!env.fetchCalls.some(call => call.url.startsWith("/gosx/")));
});

test("client telemetry uses the configured public prefix", async () => {
 const env = createContext({fetchRoutes: {"/.proxy/game/_gosx/client-events": {status: 204, text: ""}}});
 const meta = env.document.createElement("meta");
 meta.setAttribute("name", "gosx-base-path");
 meta.setAttribute("content", "/.proxy/game");
 meta.content = "/.proxy/game";
 env.document.head.appendChild(meta);
 env.context.__gosx_telemetry_config = {flushInterval: 0};
 runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
 await flushAsyncWork();
 env.context.__gosx_emit("info", "test", "prefix");
 env.context.__gosx_telemetry_flush();
 await flushAsyncWork();
 assert.ok(env.fetchCalls.some(call => new URL(call.url, env.context.location.href).pathname === "/.proxy/game/_gosx/client-events"));
 assert.ok(!env.fetchCalls.some(call => new URL(call.url, env.context.location.href).pathname === "/_gosx/client-events"));
});
