"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { bootstrapSource, bootstrapRuntimeSource, bootstrapLiteSource, bootstrapFeatureEnginesSource,
  FakeElement, createContext, runScript, flushAsyncWork } = require("./runtime-test-harness.js");

for (const [name, source] of [["monolith", bootstrapSource], ["selective", bootstrapRuntimeSource], ["lite", bootstrapLiteSource]]) {
  test(`${name}: document requests stop before unload and get a fresh scope on restore`, async () => {
    const env = createContext({});
    runScript(source, env.context, `bootstrap-${name}.js`);
    const api = env.context.__gosx.host.lifecycle;
    const original = api.documentSignal();
    let canceled = 0;
    original.addEventListener("abort", () => canceled++);
    env.context.dispatchEvent({ type: "beforeunload" });
    assert.equal(api.documentActive(), false);
    assert.equal(api.documentSignal().aborted, true, "late callers must see an already canceled scope");
    assert.equal(canceled, 1);
    env.context.dispatchEvent({ type: "pageshow", persisted: false });
    assert.equal(api.documentActive(), false, "late initial load cannot revive an outgoing page");
    let resumed = false;
    api.whenDocumentActive().then(() => { resumed = true; });
    env.context.dispatchEvent({ type: "pagehide", persisted: true });
    await flushAsyncWork();
    assert.equal(resumed, false);
    env.context.dispatchEvent({ type: "pageshow", persisted: true });
    await flushAsyncWork();
    assert.equal(resumed, true);
    assert.equal(api.documentActive(), true);
    assert.equal(original.aborted, true);
    assert.equal(api.documentSignal().aborted, false);
    assert.notEqual(api.documentSignal(), original);
  });
}

test("canceled native navigation resumes, and a superseded abort cannot resume its replacement", async () => {
  const env = createContext({});
  const listeners = new Map();
  env.context.navigation = { addEventListener(name, fn) { listeners.set(name, fn); } };
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  const api = env.context.__gosx.host.lifecycle;
  const first = new env.context.AbortController();
  listeners.get("navigate")({ destination: { sameDocument: true }, signal: first.signal });
  assert.equal(api.documentActive(), true, "hash/history changes keep the document active");
  listeners.get("navigate")({ destination: { sameDocument: false }, signal: first.signal });
  assert.equal(api.documentActive(), false);
  first.abort();
  await flushAsyncWork();
  assert.equal(api.documentActive(), true);
  const old = new env.context.AbortController();
  const next = new env.context.AbortController();
  listeners.get("navigate")({ destination: { sameDocument: false }, signal: old.signal });
  old.abort();
  listeners.get("navigate")({ destination: { sameDocument: false }, signal: next.signal });
  await flushAsyncWork();
  assert.equal(api.documentActive(), false);
  listeners.get("navigateerror")();
  assert.equal(api.documentActive(), true, "failed/canceled navigation retains its usable document");
});

test("late engine feature/factory does not mount on an outgoing native document", async () => {
  const mount = new FakeElement("div", null); mount.id = "late-root";
  let calls = 0;
  const env = createContext({
    elements: [mount],
    engineFactories: { Reporter() { calls++; return { dispose() {} }; } },
    fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource } },
    manifest: { engines: [{ id: "late-reporter", component: "Reporter", kind: "surface", mountId: "late-root", jsExport: "Reporter" }] },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  // Same ordering as a cold WASM load: the browser begins a native form POST
  // while async bootstrap work is pending; its Set-Cookie may precede pagehide.
  env.context.dispatchEvent({ type: "beforeunload" });
  await flushAsyncWork();
  assert.equal(calls, 0);
  assert.equal(env.context.__gosx.engines.size, 0);
  env.context.dispatchEvent({ type: "pageshow", persisted: true });
  await flushAsyncWork();
  assert.equal(calls, 1);
  assert.equal(env.context.__gosx.engines.size, 1);
});

test("page disposal revokes a pending factory even when native navigation is later canceled", async () => {
  let calls = 0;
  const env = createContext({
    engineFactories: { Reporter() { calls++; return {}; } },
    fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource } },
    manifest: { engines: [{ id: "revoked-reporter", component: "Reporter", kind: "worker", jsExport: "Reporter" }] },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  env.context.dispatchEvent({ type: "beforeunload" });
  await flushAsyncWork();
  await env.context.__gosx_dispose_page();
  env.context.dispatchEvent({ type: "pageshow", persisted: true });
  await flushAsyncWork();
  assert.equal(calls, 0);
});
