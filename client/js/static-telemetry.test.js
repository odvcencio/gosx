"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { bootstrapRuntimeSource, createContext, runScript, flushAsyncWork } = require("./runtime-test-harness.js");

function addMeta(env, name, content) {
 const meta = env.document.createElement("meta");
 meta.setAttribute("name", name);
 meta.setAttribute("content", content);
 meta.content = content;
 env.document.head.appendChild(meta);
}

test("static export marker keeps telemetry off and sends nothing", async () => {
 const env = createContext({fetchRoutes: {"/_gosx/client-events": {status: 204, text: ""}}});
 addMeta(env, "gosx-telemetry", "off");
 env.context.__gosx_telemetry_config = {flushInterval: 0};
 runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
 await flushAsyncWork();
 env.context.__gosx_emit("info", "test", "static");
 env.context.__gosx_telemetry_flush();
 await flushAsyncWork();
 assert.equal(env.context.__gosx_telemetry_config.enabled, false);
 assert.ok(!env.fetchCalls.some(call => call.url.includes("/_gosx/")), JSON.stringify(env.fetchCalls));
});

test("an app can opt back in to telemetry on a static page", async () => {
 const env = createContext({fetchRoutes: {"/collect": {status: 204, text: ""}}});
 addMeta(env, "gosx-telemetry", "off");
 env.context.__gosx_telemetry_config = {flushInterval: 0, enabled: true, endpoint: "/collect"};
 runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
 await flushAsyncWork();
 env.context.__gosx_emit("info", "test", "opt-in");
 env.context.__gosx_telemetry_flush();
 await flushAsyncWork();
 assert.ok(env.fetchCalls.some(call => new URL(call.url, env.context.location.href).pathname === "/collect"));
});
