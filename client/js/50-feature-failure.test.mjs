// A failed or missing feature chunk disables only that feature. Every other
// feature still mounts, and the page finishes initialization.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const H = require("./runtime-test-harness.js");
const { bootstrapRuntimeSource, createContext, runScript, flushAsyncWork, FakeElement } = H;
const chunkSource = (name) => fs.readFileSync(path.join(here, `bootstrap-feature-${name}.js`), "utf8");

const url = (name) => `/gosx/bootstrap-feature-${name}.js`;
const NON_LEGACY = "probe-fixture";
const GOOD_NON_LEGACY = `(function(){ window.__gosx_register_bootstrap_feature("${NON_LEGACY}", function() { window.__probe_ran = true; return {}; }); })();`;

// A page with an ordinary engine (needs the engines chunk) and a hub (needs the
// hubs chunk), plus an explicit requirement for `extra` when given. `fail`
// maps feature name to "throw" (the chunk fails to evaluate) or "missing" (the
// build did not ship it).
function page({ extra, fail = {}, routes: extraRoutes = {} }) {
  const mount = new FakeElement("div", null);
  mount.id = "js-root";
  const contract = new FakeElement("script", null);
  contract.id = "gosx-document";
  contract.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  const sockets = [];
  const routes = {
    [url("engines")]: { text: chunkSource("engines") },
    [url("hubs")]: { text: chunkSource("hubs") },
    [url("islands")]: { text: chunkSource("islands") },
    [url("controllers")]: { text: chunkSource("controllers") },
    [url(NON_LEGACY)]: { text: GOOD_NON_LEGACY },
    ...extraRoutes,
  };
  for (const [name, mode] of Object.entries(fail)) {
    if (mode === "throw") routes[url(name)] = { text: "throw new Error('chunk evaluation failed');" };
    else if (name === "textlayout") routes[url(name)] = { text: "throw new Error('chunk missing');" };
    else delete routes[url(name)];
  }
  const manifest = {
    engines: [{ id: "js-engine", component: "JSFixture", kind: "surface", mountId: "js-root" }],
    hubs: [{ id: "hub-1", name: "presence", path: "/gosx/hub/presence", bindings: [{ event: "snapshot", signal: "$presence" }] }],
  };
  if (extra) manifest.features = [extra];
  const env = createContext({
    elements: [contract, mount],
    engineFactories: { JSFixture() { return { dispose() {} }; } },
    createWebSocket(u) {
      const socket = { url: u, close() {} };
      sockets.push(socket);
      return socket;
    },
    fetchRoutes: routes,
    manifest,
  });
  return { env, sockets };
}

async function boot(options) {
  const fixture = page(options);
  runScript(bootstrapRuntimeSource, fixture.env.context, "bootstrap-runtime.js");
  await flushAsyncWork();
  await new Promise((resolve) => setTimeout(resolve, 20));
  await flushAsyncWork();
  return fixture;
}

function failureLogs(env, name) {
  return env.consoleLogs.error.filter((line) => line.includes(name) && line.includes(url(name)));
}

test("all chunks load: behavior is unchanged", async () => {
  const { env, sockets } = await boot({ extra: NON_LEGACY });
  assert.equal(env.context.__gosx.ready, true);
  assert.ok(env.context.__gosx.engines.get("js-engine"), "engine mounts");
  assert.equal(sockets.length, 1, "hub connects");
  assert.equal(env.context.__probe_ran, true, "the non-legacy chunk runs");
  assert.equal(env.consoleLogs.error.length, 0, env.consoleLogs.error.join("\n"));
});

for (const mode of ["throw", "missing"]) {
  for (const name of ["islands", "controllers", "textlayout", NON_LEGACY]) {
    test(`explicit ${name} that fails (${mode}) disables only itself`, async () => {
      const { env, sockets } = await boot({ extra: name, fail: { [name]: mode } });
      assert.equal(env.context.__gosx.ready, true, "page finishes initialization");
      assert.ok(env.context.__gosx.engines.get("js-engine"), "engines still mount");
      assert.equal(sockets.length, 1, "hubs still connect");
      assert.equal(failureLogs(env, name).length, 1, `one error naming ${name} and its URL: ${env.consoleLogs.error.join(" | ")}`);
    });
  }

  test(`engines chunk that fails (${mode}) skips engine mounts but not hubs`, async () => {
    const { env, sockets } = await boot({ fail: { engines: mode } });
    assert.equal(env.context.__gosx.ready, true);
    assert.equal(env.context.__gosx.engines.get("js-engine"), undefined, "engine mounts are skipped");
    assert.equal(sockets.length, 1, "hubs still connect");
    assert.equal(failureLogs(env, "engines").length, 1, env.consoleLogs.error.join(" | "));
  });

  test(`hubs chunk that fails (${mode}) skips hub connections but not engines`, async () => {
    const { env, sockets } = await boot({ fail: { hubs: mode } });
    assert.equal(env.context.__gosx.ready, true);
    assert.ok(env.context.__gosx.engines.get("js-engine"), "engines still mount");
    assert.equal(sockets.length, 0, "hub connections are skipped");
    assert.equal(failureLogs(env, "hubs").length, 1, env.consoleLogs.error.join(" | "));
  });
}

// Feature chunks that RequireFeature names must register a factory, or the
// runtime logs "missing bootstrap feature" even though the chunk loaded.
for (const name of ["arcade-audio", "scene3d-animation-graph"]) {
  test(`explicit ${name} registers its feature factory`, async () => {
    const { env } = await boot({ extra: name, routes: { [url(name)]: { text: chunkSource(name) } } });
    assert.equal(env.context.__gosx.ready, true);
    assert.deepEqual(env.consoleLogs.error.filter((line) => line.includes("missing bootstrap feature")), []);
    assert.deepEqual(env.consoleLogs.error.filter((line) => line.includes(name)), [], env.consoleLogs.error.join("\n"));
  });
}

// The Scene3D chunk is a deferred <script> the server emits; the runtime waits
// for its readiness signal. A page with an island, an ordinary engine, a hub
// and a Scene3D engine whose chunk never signals.
function scenePage({ timeoutMS = 0 } = {}) {
  const island = new FakeElement("div", null);
  island.id = "gosx-island-1";
  const mount = new FakeElement("div", null);
  mount.id = "js-root";
  const sceneMount = new FakeElement("div", null);
  sceneMount.id = "scene-root";
  const sceneTag = new FakeElement("script", null);
  sceneTag.setAttribute("data-gosx-script", "feature-scene3d");
  sceneTag.setAttribute("src", "/gosx/assets/runtime/bootstrap-feature-scene3d.b1.js");
  const contract = new FakeElement("script", null);
  contract.id = "gosx-document";
  contract.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  const sockets = [];
  const env = createContext({
    elements: [contract, island, mount, sceneMount, sceneTag],
    engineFactories: { JSFixture() { return { dispose() {} }; } },
    createWebSocket(u) {
      const socket = { url: u, close() {} };
      sockets.push(socket);
      return socket;
    },
    fetchRoutes: {
      [url("engines")]: { text: chunkSource("engines") },
      [url("hubs")]: { text: chunkSource("hubs") },
      [url("islands")]: { text: chunkSource("islands") },
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [{ id: "gosx-island-1", component: "Counter", props: {}, programRef: "/counter.json" }],
      engines: [
        { id: "js-engine", component: "JSFixture", kind: "surface", mountId: "js-root" },
        { id: "scene", component: "GoSXScene3D", kind: "surface", mountId: "scene-root", props: {} },
      ],
      hubs: [{ id: "hub-1", name: "presence", path: "/gosx/hub/presence", bindings: [{ event: "snapshot", signal: "$presence" }] }],
    },
  });
  if (timeoutMS > 0) env.context.__gosx_runtime_ready_timeout_ms = timeoutMS;
  return { env, sockets, sceneTag, sceneMount };
}

async function settle(ms) {
  await flushAsyncWork();
  await new Promise((resolve) => setTimeout(resolve, ms));
  await flushAsyncWork();
}

// Every other feature still works, the page reaches gosx:ready, the failure is
// an issue naming the chunk, and the Scene3D mount shows its fallback state.
function assertSceneFailureIsContained(fixture, reason) {
  const { env, sockets, sceneMount } = fixture;
  assert.equal(env.context.__gosx.ready, true, "page finishes initialization");
  assert.ok(env.document.dispatchedEvents.some((event) => event.type === "gosx:ready"), "gosx:ready fires");
  assert.equal(env.hydrateCalls.length, 1, "the island hydrates");
  assert.ok(env.context.__gosx.engines.get("js-engine"), "other engines mount");
  assert.equal(sockets.length, 1, "hubs connect");
  assert.equal(env.context.__gosx.engines.get("scene"), undefined, "the scene does not mount");
  assert.equal(sceneMount.getAttribute("data-gosx-fallback-active"), "server", "the scene mount keeps its server fallback");
  const issues = env.context.__gosx.listIssues().filter((issue) => issue.type === "feature" && issue.ref === "scene3d");
  assert.equal(issues.length, 1, JSON.stringify(env.context.__gosx.listIssues()));
  assert.match(issues[0].message, reason);
  assert.match(issues[0].message, /bootstrap-feature-scene3d\.b1\.js/);
}

// The event-driven cases set a 2 s ready timeout: they settle in about 25 ms,
// so the failure comes from the script event, not the timer.
test("a Scene3D chunk whose script fails to load does not hold the page hostage", async () => {
  const fixture = scenePage({ timeoutMS: 2000 });
  runScript(bootstrapRuntimeSource, fixture.env.context, "bootstrap-runtime.js");
  await settle(5);
  assert.equal(fixture.env.context.__gosx.ready, false, "the page waits for the chunk while it loads");
  fixture.sceneTag.dispatchEvent({ type: "error" });
  await settle(20);
  assertSceneFailureIsContained(fixture, /Scene3D chunk failed to load/);
});

test("a Scene3D chunk that runs without signalling readiness fails at once", async () => {
  const fixture = scenePage({ timeoutMS: 2000 });
  runScript(bootstrapRuntimeSource, fixture.env.context, "bootstrap-runtime.js");
  await settle(5);
  fixture.sceneTag.dispatchEvent({ type: "load" });
  await settle(20);
  assertSceneFailureIsContained(fixture, /ran but did not signal readiness/);
});

test("a silent Scene3D chunk times out, and a late arrival still mounts the scene", async () => {
  const fixture = scenePage({ timeoutMS: 30 });
  runScript(bootstrapRuntimeSource, fixture.env.context, "bootstrap-runtime.js");
  await settle(80);
  assertSceneFailureIsContained(fixture, /did not signal readiness within 30 ms/);

  const { context } = fixture.env;
  context.console.info = () => {};
  context.__gosx_register_engine_factory("GoSXScene3D", () => ({ dispose() {} }));
  context.__gosx_scene3d_available = true;
  context.__gosx_scene3d_loaded();
  await settle(10);
  assert.ok(context.__gosx.engines.get("scene"), "the late chunk mounts the scene");
  const stale = context.__gosx.listIssues().filter((issue) => issue.ref === "scene3d" || issue.component === "GoSXScene3D");
  assert.equal(stale.length, 0, "the stale issues are cleared: " + JSON.stringify(stale));
});


test("deferred engine waits for every disclosure and mounts once", async () => {
  const outer = new FakeElement("details", null);
  const inner = new FakeElement("details", null);
  const mount = new FakeElement("div", null);
  mount.id = "deferred-root";
  outer.appendChild(inner); inner.appendChild(mount);
  outer.open = false; inner.open = false;
  let count = 0;
  const env = createContext({
    elements: [outer],
    engineFactories: { DeferredFixture() { count++; return { dispose() {} }; } },
    fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: [{ id: "deferred", component: "DeferredFixture", kind: "surface", mountId: "deferred-root", deferUntilOpen: true }] },
  });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork(); await new Promise(resolve => setTimeout(resolve, 20)); await flushAsyncWork();
  assert.equal(count, 0);
  outer.open = true; outer.dispatchEvent({ type: "toggle" }); await flushAsyncWork();
  assert.equal(count, 0);
  inner.open = true; inner.dispatchEvent({ type: "toggle" }); await flushAsyncWork();
  assert.equal(count, 1);
  inner.open = false; inner.dispatchEvent({ type: "toggle" }); inner.open = true; inner.dispatchEvent({ type: "toggle" }); await flushAsyncWork();
  assert.equal(count, 1);
  assert.equal(inner.listenerCount("toggle"), 0);
});

test("disposing the page cancels deferred mounts", async () => {
  const details = new FakeElement("details", null), mount = new FakeElement("div", null);
  mount.id = "deferred-root"; details.appendChild(mount); details.open = false;
  let count = 0;
  const env = createContext({ elements: [details], engineFactories: { DeferredFixture() { count++; return {}; } },
    fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: [{ id: "deferred", component: "DeferredFixture", kind: "surface", mountId: "deferred-root", deferUntilOpen: true }] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await flushAsyncWork(); await new Promise(resolve => setTimeout(resolve, 20)); await flushAsyncWork();
  env.context.__gosx.host.lifecycle.disposePage();
  details.open = true; details.dispatchEvent({ type: "toggle" }); await flushAsyncWork();
  assert.equal(count, 0); assert.equal(details.listenerCount("toggle"), 0);
});

test("deferred mount follows a removed then reinserted disclosure", async () => {
  const old = new FakeElement("details", null), first = new FakeElement("div", null);
  first.id = "deferred-root"; old.appendChild(first); old.open = false;
  let count = 0;
  const env = createContext({ elements: [old], engineFactories: { DeferredFixture() { count++; return {}; } },
    fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: [{ id: "deferred", component: "DeferredFixture", kind: "surface", mountId: "deferred-root", deferUntilOpen: true }] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  await settle(30);
  env.document.byID.delete("deferred-root");
  for (const observer of env.mutationObservers) observer.trigger();
  old.open = true; old.dispatchEvent({ type: "toggle" }); await flushAsyncWork();
  assert.equal(count, 0);
  const replacement = new FakeElement("details", env.document), second = new FakeElement("div", env.document);
  second.id = "deferred-root"; replacement.appendChild(second); replacement.open = false;
  env.document.byID.set("deferred-root", second);
  for (const observer of env.mutationObservers.slice()) observer.trigger();
  assert.equal(old.listenerCount("toggle"), 0);
  replacement.open = true; replacement.dispatchEvent({ type: "toggle" }); await flushAsyncWork();
  assert.equal(count, 1);
});

test("late factory honors closed disclosure and page disposal", async () => {
  const details = new FakeElement("details", null), mount = new FakeElement("div", null);
  mount.id = "deferred-root"; details.appendChild(mount); details.open = false;
  let count = 0;
  const env = createContext({ elements: [details], fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: [{ id: "deferred", component: "DeferredFixture", kind: "surface", mountId: "deferred-root", deferUntilOpen: true }] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js"); await settle(30);
  env.context.__gosx_register_engine_factory("DeferredFixture", () => { count++; return {}; });
  await flushAsyncWork(); assert.equal(count, 0);
  env.context.__gosx.host.lifecycle.disposePage();
  env.context.__gosx_mount_late_engine_factory("DeferredFixture");
  details.open = true; details.dispatchEvent({ type: "toggle" }); await flushAsyncWork();
  assert.equal(count, 0);
});

test("late factory waits again if disclosure closes after a failed start", async () => {
  const details = new FakeElement("details", null), mount = new FakeElement("div", null);
  mount.id = "deferred-root"; details.appendChild(mount); details.open = false;
  let count = 0;
  const env = createContext({ elements: [details], fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: [{ id: "deferred", component: "DeferredFixture", kind: "surface", mountId: "deferred-root", deferUntilOpen: true }] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js"); await settle(30);
  details.open = true; details.dispatchEvent({type:"toggle"}); await settle(10);
  details.open = false; details.dispatchEvent({type:"toggle"});
  env.context.__gosx_register_engine_factory("DeferredFixture", () => { count++; return {}; });
  env.context.__gosx_mount_late_engine_factory("DeferredFixture"); await flushAsyncWork();
  assert.equal(count, 0);
  details.open = true; details.dispatchEvent({type:"toggle"}); await settle(30);
  assert.equal(count, 1, JSON.stringify(env.consoleLogs));
});

test("deferred mount survives removing and reinserting the same node", async () => {
  const details = new FakeElement("details", null), mount = new FakeElement("div", null);
  mount.id = "deferred-root"; details.appendChild(mount); details.open = false;
  let count = 0;
  const env = createContext({ elements: [details], engineFactories: { DeferredFixture() { count++; return {}; } },
    fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: [{ id: "deferred", component: "DeferredFixture", kind: "surface", mountId: "deferred-root", deferUntilOpen: true }] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js"); await settle(30);
  env.document.byID.delete("deferred-root"); for (const observer of env.mutationObservers) observer.trigger();
  env.document.byID.set("deferred-root", mount); for (const observer of env.mutationObservers.slice()) observer.trigger();
  details.open = true; details.dispatchEvent({type:"toggle"}); await flushAsyncWork();
  assert.equal(count, 1);
});

test("deferred engine can be remounted after page bootstrap", async () => {
  const details = new FakeElement("details", null), mount = new FakeElement("div", null);
  mount.id = "deferred-root"; details.appendChild(mount); details.open = false;
  let count = 0;
  const env = createContext({ elements: [details], engineFactories: { DeferredFixture() { count++; return {dispose(){}}; } },
    fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: [{ id: "deferred", component: "DeferredFixture", kind: "surface", mountId: "deferred-root", deferUntilOpen: true }] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js"); await settle(30);
  env.context.__gosx.host.lifecycle.disposePage();
  await env.context.__gosx.host.lifecycle.bootstrapPage(); await settle(20);
  assert.equal(count, 0); assert.equal(details.listenerCount("toggle"), 1);
  details.open = true; details.dispatchEvent({type:"toggle"}); await settle(20);
  assert.equal(count, 1);
});

test("duplicate invalid engine cannot leak a deferred listener", async () => {
  const a = new FakeElement("details", null), b = new FakeElement("details", null);
  const first = new FakeElement("div", null), second = new FakeElement("div", null);
  first.id = "first-root"; second.id = "second-root"; a.appendChild(first); b.appendChild(second); a.open = b.open = false;
  let count = 0;
  const env = createContext({ elements: [a,b], engineFactories: { DeferredFixture() { count++; return {}; } },
    fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: ["first-root", "second-root"].map(mountId => ({ id: "deferred", component: "DeferredFixture", kind: "surface", mountId, deferUntilOpen: true })) } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js"); await settle(30);
  env.context.__gosx.host.lifecycle.disposePage();
  a.open = b.open = true; a.dispatchEvent({type:"toggle"}); b.dispatchEvent({type:"toggle"}); await flushAsyncWork();
  assert.equal(count, 0); assert.equal(a.listenerCount("toggle"), 0); assert.equal(b.listenerCount("toggle"), 0);
});

test("all deferred Scene3D entries do not request the scene feature at startup", async () => {
  const details = new FakeElement("details", null), mount = new FakeElement("div", null);
  mount.id = "scene-root"; details.appendChild(mount); details.open = false;
  const env = createContext({ elements: [details], fetchRoutes: { [url("engines")]: { text: chunkSource("engines") } },
    manifest: { engines: [{id:"scene", component:"GoSXScene3D", kind:"surface", mountId:"scene-root", deferUntilOpen:true}] } });
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js"); await settle(30);
  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.engines.has("scene"), false);
  assert.equal(env.context.__gosx.listIssues().filter(issue => issue.ref === "scene3d").length, 0);
});
