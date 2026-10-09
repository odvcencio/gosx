// The M0 core seams are inert until a later chunk installs the hook they read.
// This file pins both halves: the seam exists in the source, and without the
// hook the behavior is the behavior before the seam existed.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const read = (...parts) => fs.readFileSync(path.join(here, ...parts), "utf8");
const { navigationSource, createContext, runScript, flushAsyncWork, FakeElement, installManualTimers, buildNavigatedDocument } = require("./runtime-test-harness.js");

const actionsSrc = read("..", "runtime", "host", "actions.ts");
const navigationSrc = read("..", "runtime", "host", "navigation.ts");
const mountingSrc = read("bootstrap-src", "30b-tail-engine-mounting.ts");

test("actions.ts routes through actionFetchNow behind the edit queue", () => {
  assert.match(actionsSrc, /function actionFetchNow\(/);
  assert.match(actionsSrc, /window\.__gosx\.editQueue/);
  assert.match(actionsSrc, /data-gosx-queue/);
});

test("navigation.ts keeps the queue dispatcher ahead of the pending guard", () => {
  assert.match(navigationSrc, /function submitFormWith\(/);
  const submitForm = navigationSrc.slice(navigationSrc.indexOf("async function submitForm("));
  const queueAt = submitForm.indexOf("window.__gosx.editQueue");
  const guardAt = submitForm.indexOf("pendingManagedForms.has(form)");
  assert.ok(queueAt >= 0 && guardAt >= 0 && queueAt < guardAt, "editQueue dispatch must precede the pending guard");
  assert.match(navigationSrc, /window\.__gosx\.editConflict/);
  const manage = navigationSrc.slice(navigationSrc.indexOf("async function submitManagedActionForm("));
  assert.ok(manage.indexOf("editConflict") < manage.indexOf("parseJSONResponse(response)"), "409 hook runs before the JSON parse");
  const submitAction = navigationSrc.slice(navigationSrc.indexOf("function submitAction("), navigationSrc.indexOf("function actionFormHost("));
  assert.match(submitAction, /opts\.queue/);
});

test("30b clears the boot token after go.run and picks the toolchain constructor", () => {
  const token = mountingSrc.indexOf("window.__gosx.goWASMBootToken = record.token");
  const run = mountingSrc.indexOf("runResult = go.run(result.instance);");
  const clear = mountingSrc.indexOf('window.__gosx.goWASMBootToken = ""');
  assert.ok(token >= 0 && token < run, "token is set before go.run");
  assert.ok(clear > mountingSrc.indexOf("runResult = go.run(result.instance);"), "token is cleared after go.run returns");
  assert.ok(mountingSrc.includes('record.toolchain === "tinygo"'));
  assert.ok(mountingSrc.includes("tinyGo ? window.__gosx.tinyGoWASMCtor : window.__gosx_standard_go_wasm_ctor"));
});

function managedForm(attrs, actionURL) {
  const form = new FakeElement("form", null);
  form.setAttribute("action", actionURL);
  form.setAttribute("method", "post");
  form.setAttribute("data-gosx-form", "");
  for (const [key, value] of Object.entries(attrs)) form.setAttribute(key, value);
  const input = new FakeElement("input", null);
  input.setAttribute("name", "title");
  input.value = "first";
  form.appendChild(input);
  const status = new FakeElement("p", null);
  status.setAttribute("class", "form-status");
  form.appendChild(status);
  return { form, input, status };
}

function submitEvent(form) {
  return {
    type: "submit",
    target: form,
    defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true; },
  };
}

test("a data-gosx-queue form with no queue installed submits once with its current values", async () => {
  const actionURL = "http://localhost:3000/save";
  const { form, input, status } = managedForm({ "data-gosx-queue": "serial" }, actionURL);
  const env = createContext({
    elements: [form],
    fetchRoutes: { [actionURL]: { text: '{"ok":true,"message":"Saved."}', url: actionURL } },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const submit = env.document.eventListeners.get("submit")[0];
  input.value = "second";
  submit(submitEvent(form));
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1);
  const body = env.fetchCalls[0].init.body;
  assert.equal(body.get("title"), "second");
  assert.equal(status.textContent, "Saved.");
  assert.equal(form.getAttribute("data-gosx-form-state"), "success");
});

test("a 409 with no editConflict hook takes the ordinary failure projection", async () => {
  const actionURL = "http://localhost:3000/save";
  const { form, status } = managedForm({}, actionURL);
  const env = createContext({
    elements: [form],
    fetchRoutes: { [actionURL]: { status: 409, ok: false, text: '{"ok":false,"message":"Edit conflict."}', url: actionURL } },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.eventListeners.get("submit")[0](submitEvent(form));
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1);
  assert.equal(status.textContent, "Edit conflict.");
  assert.equal(form.getAttribute("data-gosx-form-state"), "error");
});

test("the 409 hook, once installed, receives the form and response", async () => {
  const actionURL = "http://localhost:3000/save";
  const { form } = managedForm({}, actionURL);
  const env = createContext({
    elements: [form],
    fetchRoutes: { [actionURL]: { status: 409, ok: false, text: "{}", url: actionURL } },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const seen = [];
  env.context.__gosx.editConflict = (f, response, url, method) => {
    seen.push([f === form, response.status, url, method]);
    return { response, result: null, redirected: false };
  };
  env.document.eventListeners.get("submit")[0](submitEvent(form));
  await flushAsyncWork();
  assert.deepEqual(seen, [[true, 409, actionURL, "POST"]]);
});

test("an installed edit queue receives a snapshot taken at enqueue", async () => {
  const actionURL = "http://localhost:3000/save";
  const { form, input } = managedForm({ "data-gosx-queue": "serial" }, actionURL);
  const env = createContext({
    elements: [form],
    fetchRoutes: { [actionURL]: { text: '{"ok":true}', url: actionURL } },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const queued = [];
  env.context.__gosx.editQueue = {
    submit(f, submitter, snapshot, send) {
      queued.push({ f, snapshot });
      return send(f, submitter, snapshot);
    },
  };
  input.value = "at-enqueue";
  env.document.eventListeners.get("submit")[0](submitEvent(form));
  input.value = "later";
  await flushAsyncWork();
  assert.equal(queued.length, 1);
  assert.equal(queued[0].snapshot.get("title"), "at-enqueue");
  assert.equal(env.fetchCalls.length, 1);
});

function runActions({ queue } = {}) {
  const listeners = {};
  const fetches = [];
  const ctx = {
    console,
    URL,
    URLSearchParams,
    fetch: (url, opts) => {
      fetches.push({ url, opts });
      return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, json: () => Promise.resolve({}) });
    },
    document: {
      baseURI: "https://app.example/",
      addEventListener: (type, fn) => { listeners[type] = fn; },
      dispatchEvent() {},
      querySelector: () => null,
      querySelectorAll: () => [],
      readyState: "complete",
    },
    window: { location: { href: "https://app.example/", origin: "https://app.example" }, __gosx: queue ? { editQueue: queue } : {} },
  };
  ctx.window.document = ctx.document;
  ctx.window.fetch = ctx.fetch;
  ctx.CustomEvent = class { constructor(type, init = {}) { this.type = type; this.detail = init.detail; } };
  vm.createContext(ctx);
  vm.runInContext([
    read("..", "runtime", "host", "compatibility.ts"),
    read("..", "runtime", "host", "request.ts"),
    actionsSrc,
  ].join("\n"), ctx);
  return { listeners, fetches };
}

function actionButton() {
  const attrs = { "data-gosx-action": "POST /act", "data-gosx-queue": "" };
  const el = {
    _attrs: attrs,
    tagName: "BUTTON",
    disabled: false,
    getAttribute(n) { return n in attrs ? attrs[n] : null; },
    hasAttribute(n) { return n in attrs; },
    closest(sel) { return /data-gosx-action\]$/.test(sel) ? el : null; },
    matches() { return false; },
  };
  return el;
}

test("a data-gosx-action element with data-gosx-queue and no queue sends one request", async () => {
  const { listeners, fetches } = runActions();
  const button = actionButton();
  listeners.click({ target: button, preventDefault() {} });
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].opts.method, "POST");
});

test("a data-gosx-action element with data-gosx-queue runs through an installed queue", async () => {
  const runs = [];
  const { listeners, fetches } = runActions({ queue: { run(fn) { runs.push(1); return fn(); } } });
  const button = actionButton();
  listeners.click({ target: button, preventDefault() {} });
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(runs.length, 1);
  assert.equal(fetches.length, 1);
});

test("the host authority guard still passes", () => {
  execFileSync("go", ["test", "-count=1", "-run", "TestHostAmbientCompatibilityDoesNotIncrease", "./..."], {
    cwd: path.join(here, "..", "..", "cmd", "buildbootstrap"),
    env: { ...process.env, GOWORK: "off" },
    stdio: "pipe",
  });
});

test("a queued submit holds the form in the in-flight set, so refresh ticks skip", async () => {
  const url = "http://localhost:3000/scoreboard";
  const actionURL = "http://localhost:3000/scoreboard/__actions/save";
  const main = new FakeElement("main", null);
  main.id = "scoreboard";
  main.setAttribute("data-gosx-revalidate-interval", "4s");
  const { form } = managedForm({ "data-gosx-queue": "serial" }, actionURL);
  let resolveAction;
  const parsedDocs = new Map();
  const env = createContext({
    elements: [main, form],
    fetchRoutes: {
      [actionURL]: () => new Promise((resolve) => { resolveAction = resolve; }),
      [url]: { text: "__REFRESH__", url },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.location.href = url;
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  const freshMain = new FakeElement("main", null);
  freshMain.id = "scoreboard";
  freshMain.setAttribute("data-gosx-revalidate-interval", "4s");
  parsedDocs.set("__REFRESH__", buildNavigatedDocument({ title: "Scoreboard", bodyNodes: [freshMain] }));
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.context.__gosx.editQueue = { submit(f, submitter, snapshot, send) { return send(f, submitter, snapshot); } };

  env.document.eventListeners.get("submit")[0](submitEvent(form));
  await flushAsyncWork();
  timers.runInterval(4000);
  await flushAsyncWork();
  assert.equal(env.fetchCalls.filter((call) => call.url === url).length, 0, "a queued edit in flight must skip the refresh tick");

  resolveAction({ text: "{}" });
  await flushAsyncWork();
  timers.runInterval(4000);
  await flushAsyncWork();
  assert.equal(env.fetchCalls.filter((call) => call.url === url).length, 1, "the next tick runs once the queued send settles");
});

function queuedRefreshFixture() {
  const url = "http://localhost:3000/scoreboard";
  const actionURL = "http://localhost:3000/scoreboard/__actions/save";
  const main = new FakeElement("main", null);
  main.id = "scoreboard";
  main.setAttribute("data-gosx-revalidate-interval", "4s");
  const { form } = managedForm({ "data-gosx-queue": "serial" }, actionURL);
  const parsedDocs = new Map();
  const env = createContext({
    elements: [main, form],
    fetchRoutes: { [actionURL]: { text: "{}", url: actionURL }, [url]: { text: "__REFRESH__", url } },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.location.href = url;
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  const freshMain = new FakeElement("main", null);
  freshMain.id = "scoreboard";
  freshMain.setAttribute("data-gosx-revalidate-interval", "4s");
  parsedDocs.set("__REFRESH__", buildNavigatedDocument({ title: "Scoreboard", bodyNodes: [freshMain] }));
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  // A queue that holds every submission until the test releases it.
  const gates = [];
  env.context.__gosx.editQueue = {
    submit(f, submitter, snapshot, send) {
      return new Promise((resolve) => gates.push(() => resolve(send(f, submitter, snapshot))));
    },
  };
  const refreshes = () => env.fetchCalls.filter((call) => call.url === url).length;
  return { env, form, timers, gates, refreshes };
}

test("a queued edit that is still waiting protects the form from refresh ticks", async () => {
  const { env, form, timers, gates, refreshes } = queuedRefreshFixture();
  env.document.eventListeners.get("submit")[0](submitEvent(form));
  await flushAsyncWork();
  assert.equal(env.fetchCalls.filter((c) => c.url.includes("__actions")).length, 0, "the queue is still holding the edit");
  timers.runInterval(4000);
  await flushAsyncWork();
  assert.equal(refreshes(), 0, "a waiting queued edit must skip the refresh tick");
  gates.shift()();
  await flushAsyncWork();
  timers.runInterval(4000);
  await flushAsyncWork();
  assert.equal(refreshes(), 1, "the tick runs once the queued edit settles");
});

test("two queued edits on one form: the first settling does not release the second", async () => {
  const { env, form, timers, gates, refreshes } = queuedRefreshFixture();
  const submit = env.document.eventListeners.get("submit")[0];
  submit(submitEvent(form));
  submit(submitEvent(form));
  await flushAsyncWork();
  assert.equal(gates.length, 2, "both submissions reach the queue");
  gates.shift()();
  await flushAsyncWork();
  timers.runInterval(4000);
  await flushAsyncWork();
  assert.equal(refreshes(), 0, "the second queued edit still protects the form");
  gates.shift()();
  await flushAsyncWork();
  timers.runInterval(4000);
  await flushAsyncWork();
  assert.equal(refreshes(), 1, "all queued edits settled, so the tick runs");
});

test("a synchronous go.run throw clears the boot token", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "a-root";
  const contract = new FakeElement("script", null);
  contract.id = "gosx-document";
  contract.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  const { bootstrapRuntimeSource, bootstrapFeatureEnginesSource } = require("./runtime-test-harness.js");
  const env = createContext({
    elements: [contract, mount],
    fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: bootstrapFeatureEnginesSource }, "/engines/a.wasm": { text: "a" } },
    manifest: { engines: [{ id: "a", component: "A", kind: "surface", runtime: "go-wasm", programRef: "/engines/a.wasm", mountId: "a-root" }] },
  });
  const ctx = env.context;
  ctx.__gosx_standard_go_wasm_ctor = function FakeGo() { this.env = {}; this.importObject = {}; this.run = () => { throw new Error("sync trap"); }; };
  // The engine boot failure rejects the page bootstrap, which is the expected
  // outcome of a trapping module; keep the test runner from counting it.
  const listeners = process.listeners("unhandledRejection");
  process.removeAllListeners("unhandledRejection");
  const rejections = [];
  process.on("unhandledRejection", (reason) => rejections.push(reason));
  try {
    runScript(bootstrapRuntimeSource, ctx, "bootstrap-runtime.js");
    await flushAsyncWork();
    await new Promise((r) => setTimeout(r, 20));
    await flushAsyncWork();
  } finally {
    process.removeAllListeners("unhandledRejection");
    for (const fn of listeners) process.on("unhandledRejection", fn);
  }
  assert.ok(rejections.length >= 1, "the trapping module must fail the boot");
  assert.equal(ctx.__gosx.goWASMBootToken, "");
});
