// Contract tests for the generic GoSX runtime-surface registry.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = [
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "request.ts"), "utf8"),
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "facade.ts"), "utf8"),
].join("\n");
const domModuleSrc = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "host", "dom.ts"),
  "utf8"
);

function makeElement(attrs = {}, children = []) {
  const listeners = new Map();
  const el = {
    _attrs: { ...attrs },
    children,
    listeners,
    parentNode: null,
    getAttribute(name) { return Object.prototype.hasOwnProperty.call(this._attrs, name) ? this._attrs[name] : null; },
    hasAttribute(name) { return Object.prototype.hasOwnProperty.call(this._attrs, name); },
    setAttribute(name, value) { this._attrs[name] = String(value); },
    removeAttribute(name) { delete this._attrs[name]; },
    addEventListener(type, listener) { listeners.set(type, listener); },
    removeEventListener(type, listener) { if (listeners.get(type) === listener) listeners.delete(type); },
    querySelectorAll(selector) {
      if (selector !== "[data-gosx-runtime-surface]") return [];
      const out = [];
      const walk = (node) => {
        for (const child of node.children || []) {
          child.parentNode = node;
          if (child.hasAttribute("data-gosx-runtime-surface")) out.push(child);
          walk(child);
        }
      };
      walk(this);
      return out;
    },
    contains(node) {
      if (node === this) return true;
      return (this.children || []).some((child) => child === node || child.contains(node));
    },
  };
  return el;
}

function runModule(body, options = {}) {
  const events = [];
  const telemetry = [];
  const document = {
    body,
    baseURI: options.baseURI || "https://app.example/",
    querySelectorAll: options.querySelectorAll || (() => []),
    documentElement: body,
    dispatchEvent(event) { events.push(event); },
    querySelector: options.querySelector || (() => null),
  };
  const window = {
    __gosx: {
      reportIssue() {},
      ...(options.reportFailure ? { reportFailure: options.reportFailure } : {}),
    },
    __gosx_emit(level, category, message, fields) {
      telemetry.push({ level, category, message, fields });
    },
    location: { href: "https://app.example/", origin: "https://app.example" },
    fetch: options.fetch || (() => Promise.resolve({})),
  };
  class CustomEvent {
    constructor(type, init = {}) {
      this.type = type;
      this.detail = init.detail;
    }
  }
  const context = { window, document, URL, CustomEvent, AbortController, console, setTimeout, clearTimeout };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);
  if (options.withDOM) vm.runInContext(domModuleSrc, context);
  return { context, events, telemetry };
}

test("runtime surfaces mount late registrations and expose the GoSX bridge", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor", "data-gosx-runtime-surface-version": "1" });
  const body = makeElement({}, [surface]);
  const { context, events, telemetry } = runModule(body, { withDOM: true });
  let mounted = 0;
  let disposed = 0;
  let receivedContext;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    mounted += 1;
    return { dispose: () => { disposed += 1; } };
  });

  assert.equal(mounted, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "ready");
  assert.equal(receivedContext.root, surface);
  assert.equal(receivedContext.name, "editor");
  assert.equal(receivedContext.version, "1");
  assert.equal(receivedContext.namespace, context.window.__gosx);
  assert.equal(typeof receivedContext.fetch, "function");
  assert.equal(typeof receivedContext.request, "function");
  assert.equal(typeof context.window.__gosx.dom.scope, "function");
  assert.equal(typeof context.window.__gosx.transport.scope, "function");
  assert.equal(typeof context.window.__gosx.transport.requestLatest, "function");
  assert.equal(typeof context.window.__gosx.scheduler.scope, "function");
  assert.equal(typeof receivedContext.listen, "function");
  assert.equal(receivedContext.on, receivedContext.listen);
  assert.equal(receivedContext.dom.root, surface);
  assert.equal(typeof receivedContext.dom.portal, "function");
  assert.equal(typeof receivedContext.transport.request, "function");
  assert.equal(typeof receivedContext.scheduler.schedule, "function");
  assert.equal(typeof receivedContext.scheduler.frame, "function");
  assert.equal(typeof receivedContext.reconcile, "function");
  assert.equal(receivedContext.navigation, null);
  assert.equal(receivedContext.diagnostics, null);
  assert.equal(receivedContext.telemetry, null);
  assert.equal(receivedContext.prose, null);
  assert.equal(receivedContext.actions, null);
  assert.equal(receivedContext.regions, null);
  assert.equal(receivedContext.stream, null);
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:mount"), true);
  assert.equal(telemetry.some((event) => event.message === "runtime surface registered"), true);
  assert.equal(telemetry.some((event) => event.message === "runtime surface mounted"), true);

  context.window.__gosx_dispose_runtime_surfaces(body);
  assert.equal(disposed, 1);
  assert.equal(surface.getAttribute("data-gosx-runtime-state"), "disposed");
  assert.equal(events.some((event) => event.type === "gosx:runtime-surface:unmount"), true);
  assert.equal(telemetry.some((event) => event.message === "runtime surface unmounted"), true);
});

test("surface contexts publish shared runtime services and navigate through core", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const navigation = {
    navigate(url, options) {
      return { url, options };
    },
  };
  const diagnostics = { report() {} };
  const telemetryEvents = [];
  const telemetry = { emit(...args) { telemetryEvents.push(args); } };
  const prose = { reconcileHTML() {} };
  const actions = { run() {} };
  const regions = { refresh() {} };
  const stream = { reconcileHTML() {} };
  const { context } = runModule(body, { withDOM: true });
  const issues = [];
  context.window.__gosx.reportIssue = (issue) => {
    issues.push(issue);
    return issue;
  };
  context.window.__gosx.navigation = navigation;
  context.window.__gosx.diagnostics = diagnostics;
  context.window.__gosx.telemetry = telemetry;
  context.window.__gosx.prose = prose;
  context.window.__gosx.actions = actions;
  context.window.__gosx.regions = regions;
  context.window.__gosx.stream = stream;

  let receivedContext;
  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });

  assert.equal(receivedContext.navigation, navigation);
  assert.equal(receivedContext.diagnostics, diagnostics);
  assert.equal(receivedContext.telemetry, telemetry);
  assert.equal(receivedContext.prose, prose);
  assert.equal(receivedContext.actions, actions);
  assert.equal(receivedContext.regions, regions);
  assert.equal(receivedContext.stream, stream);
  assert.deepEqual(await receivedContext.navigate("/after", { replace: true }), {
    url: "/after",
    options: { replace: true },
  });
  const reconciled = receivedContext.reconcile(surface, () => ({ changed: true }));
  assert.equal(reconciled.lifecycle, "mounted");
  const issue = receivedContext.reportFailure("preview", new Error("preview unavailable"), { mode: "preview" });
  assert.equal(issue.type, "request");
  assert.equal(issue.phase, "preview");
  assert.equal(issue.severity, "warning");
  assert.equal(issue.element, surface);
  assert.equal(issues.length, 1);
  assert.equal(telemetryEvents[0][0], "warn");
  assert.equal(telemetryEvents[0][1], "runtime-surface");
  assert.equal(telemetryEvents[0][3].mode, "preview");
  receivedContext.reportFailure("preview", { name: "AbortError", message: "stale" });
  assert.equal(issues.length, 1);
});

test("surface failure reporting delegates to the shared core policy", () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const failures = [];
  const { context } = runModule(body, {
    withDOM: true,
    reportFailure(operation, error, fields) {
      failures.push({ operation, error, fields });
      return { delegated: true };
    },
  });
  let receivedContext;
  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });

  const result = receivedContext.reportFailure("preview", new Error("unavailable"), { mode: "preview" });
  assert.deepEqual(result, { delegated: true });
  assert.equal(failures.length, 1);
  assert.equal(failures[0].operation, "preview");
  assert.equal(failures[0].fields.scope, "runtime-surface");
  assert.equal(failures[0].fields.component, "editor");
  assert.equal(failures[0].fields.element, surface);
  assert.equal(failures[0].fields.telemetry.mode, "preview");
});

test("runtime surfaces are remounted after disposal when the page changes", () => {
  const first = makeElement({ "data-gosx-runtime-surface": "editor" });
  const second = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [first]);
  const { context } = runModule(body);
  let mounted = 0;

  context.window.__gosx_register_runtime_surface("editor", () => {
    mounted += 1;
    return { dispose() {} };
  });
  assert.equal(mounted, 1);

  body.children = [second];
  context.window.__gosx_mount_runtime_surfaces(body);
  assert.equal(mounted, 2);
  assert.equal(second.getAttribute("data-gosx-runtime-state"), "ready");
});

test("core request transport owns CSRF defaults and preserves explicit headers", async () => {
  const calls = [];
  const body = makeElement();
  const meta = { getAttribute(name) { return name === "content" ? "csrf-from-core" : null; } };
  const { context } = runModule(body, {
    querySelector(selector) {
      return selector === 'meta[name="csrf-token"]' ? meta : null;
    },
    fetch(input, init) {
      calls.push({ input, init });
      return Promise.resolve({ json: () => Promise.resolve({ ok: true }) });
    },
  });

  assert.equal(typeof context.window.__gosx.request, "function");
  assert.equal(typeof context.window.__gosx.transport.json, "function");

  await context.window.__gosx.request("/save", { method: "POST" });
  assert.equal(calls[0].init.headers["X-CSRF-Token"], "csrf-from-core");

  await context.window.__gosx.request("/save", {
    method: "POST",
    headers: { "x-csrf-token": "csrf-from-form" },
  });
  assert.equal(calls[1].init.headers["x-csrf-token"], "csrf-from-form");
  assert.equal(calls[1].init.headers["X-CSRF-Token"], undefined);

  await context.window.__gosx.request("/read", { method: "GET" });
  assert.equal(calls[2].init.headers, undefined);
});

test("core request transport preserves HeadersInit forms and explicit CSRF headers", async () => {
  const calls = [];
  const { context } = runModule(makeElement(), {
    querySelector: () => ({ getAttribute: () => "automatic-token" }),
    fetch(input, init) { calls.push(init); return Promise.resolve({}); },
  });
  for (const explicit of [false, true]) {
    const entries = [["X-Trace", "preserved"]];
    if (explicit) entries.push(["x-CsRf-ToKeN", "explicit-token"]);
    for (const headers of [Object.fromEntries(entries), entries, new Headers(entries)]) {
      const before = JSON.stringify(headers instanceof Headers ? [...headers] : headers);
      await context.window.__gosx.request("/save", { method: "POST", headers });
      const sent = new Headers(calls.at(-1).headers);
      assert.equal(sent.get("x-trace"), "preserved");
      assert.equal(sent.get("x-csrf-token"), explicit ? "explicit-token" : "automatic-token");
      assert.equal(JSON.stringify(headers instanceof Headers ? [...headers] : headers), before);
    }
  }
});

test("surface requests inherit the surface abort signal through core transport", async () => {
  const calls = [];
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body, {
    fetch(input, init) {
      calls.push({ input, init });
      return Promise.resolve({});
    },
  });
  let receivedContext;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });
  await receivedContext.request("/preview", { method: "POST" });

  assert.equal(calls.length, 1);
  assert.equal(calls[0].init.signal, receivedContext.signal);
});

test("surface latest requests cancel stale work and expose shared response JSON", async () => {
  const calls = [];
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body, {
    fetch(input, init) {
      calls.push({ input, init });
      return new Promise((resolve, reject) => {
        const abort = () => {
          const error = new Error("request aborted");
          error.name = "AbortError";
          reject(error);
        };
        if (init.signal && init.signal.aborted) {
          abort();
          return;
        }
        if (init.signal) init.signal.addEventListener("abort", abort, { once: true });
        if (input === "https://app.example/second") resolve({ ok: true });
      });
    },
  });
  let receivedContext;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });

  const first = receivedContext.requestLatest("preview", "/first", { method: "POST" });
  const second = receivedContext.requestLatest("preview", "/second", { method: "POST" });
  await assert.rejects(first, (error) => error && error.name === "AbortError");
  await second;

  assert.equal(calls.length, 2);
  assert.equal(calls[0].init.signal.aborted, true);
  assert.notEqual(calls[0].init.signal, calls[1].init.signal);
  const decoded = await receivedContext.json({
    clone() {
      return { json: () => Promise.resolve({ html: "<p>ok</p>" }) };
    },
  });
  assert.deepEqual(decoded, { html: "<p>ok</p>" });
});

test("transport scopes isolate latest-request cancellation and inherit lifecycle aborts", async () => {
  const calls = [];
  const body = makeElement();
  const { context } = runModule(body, {
    fetch(input, init) {
      calls.push({ input, init });
      return new Promise((resolve, reject) => {
        const abort = () => {
          const error = new Error("request aborted");
          error.name = "AbortError";
          reject(error);
        };
        if (init.signal && init.signal.aborted) return abort();
        if (init.signal) init.signal.addEventListener("abort", abort, { once: true });
        if (input === "https://app.example/second") resolve({ ok: true });
      });
    },
  });
  const firstController = new AbortController();
  const first = context.window.__gosx.transport.scope(firstController.signal);
  const second = context.window.__gosx.transport.scope();
  const stale = first.requestLatest("preview", "/first");
  const current = second.requestLatest("preview", "/first");
  firstController.abort();
  await assert.rejects(stale, (error) => error && error.name === "AbortError");
  assert.equal(calls[0].init.signal.aborted, true);
  assert.equal(calls.length, 2);
  second.cancelRequest("preview");
  await assert.rejects(current, (error) => error && error.name === "AbortError");
});

test("surface scheduler coalesces keyed work and cancels it on unmount", async () => {
  const surface = makeElement({ "data-gosx-runtime-surface": "editor" });
  const body = makeElement({}, [surface]);
  const { context } = runModule(body, { withDOM: true });
  const calls = [];
  let receivedContext;

  context.window.__gosx_register_runtime_surface("editor", (surfaceContext) => {
    receivedContext = surfaceContext;
    return { dispose() {} };
  });

  receivedContext.schedule("preview", () => calls.push("stale"), 0);
  receivedContext.schedule("preview", () => calls.push("current"), 0);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(calls, ["current"]);

  receivedContext.schedule("after-unmount", () => calls.push("disposed"), 0);
  context.window.__gosx_dispose_runtime_surfaces(body);
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.deepEqual(calls, ["current"]);
});


test("core requests attach automatic CSRF headers only to the same origin", async () => {
  const calls = [];
  const { context } = runModule(makeElement(), {
    querySelector: () => ({ getAttribute: () => "response-token" }),
    fetch: (input, init) => { calls.push(init); return Promise.resolve({}); },
  });
  for (const input of ["/save", "https://app.example/save", { url: "https://app.example/save", method: "POST" }]) {
    await context.window.__gosx.request(input, { method: "POST" });
    assert.equal(calls.at(-1).headers["X-CSRF-Token"], "response-token");
  }
  for (const input of ["https://other.example/save", "//other.example/save", "http://app.example/save", { url: "https://other.example/save", method: "POST" }]) {
    await context.window.__gosx.request(input, { method: "POST" });
    assert.equal(calls.at(-1).headers?.["X-CSRF-Token"], undefined);
  }
});

test("core transport resolves document base and preserves Request inputs", async () => {
 const calls = [];
 const meta = {getAttribute: () => "token"};
 const { context } = runModule(makeElement(), {
  baseURI: "https://other.example/forms/",
  querySelector: () => meta,
  fetch(input, init) { calls.push({input,init}); return Promise.resolve({}); },
 });
 for (const [input, expected, token] of [["/save", "https://other.example/save", undefined], ["save", "https://other.example/forms/save", undefined], ["https://app.example/save", "https://app.example/save", "token"]]) {
  await context.window.__gosx.request(input, {method:"POST"});
  const call = calls.at(-1);
  assert.equal(call.input, expected);
  assert.equal(call.init.headers?.["X-CSRF-Token"], token);
 }
 const request = {url:"https://app.example/save",method:"POST",body:"preserved"};
 await context.window.__gosx.request(request);
 assert.equal(calls.at(-1).input, request);
 assert.equal(calls.at(-1).init.headers["X-CSRF-Token"], "token");
});

test("JSON sign-in refreshes meta and native hidden tokens before the next mutation", async () => {
 const calls = [];
 const meta = makeElement({content:"before-login"}); meta.tagName = "META";
 const hidden = makeElement({value:"before-login"}); hidden.tagName = "INPUT"; hidden.value = "before-login";
 const { context } = runModule(makeElement(), {
  querySelector: () => meta, querySelectorAll: () => [meta, hidden],
  fetch(input,init) { calls.push({input,init}); return Promise.resolve({ headers: { get: () => "after-login" } }); },
 });
 await context.window.__gosx.request("/login", {method:"POST"});
 assert.equal(meta.getAttribute("content"), "after-login");
 assert.equal(hidden.value, "after-login");
 assert.equal(hidden.getAttribute("value"), "after-login");
 await context.window.__gosx.request("/save", {method:"POST"});
 assert.equal(calls[0].init.headers["X-CSRF-Token"], "before-login");
 assert.equal(calls[1].init.headers["X-CSRF-Token"], "after-login");
});

test("cross-origin responses cannot replace document CSRF tokens", async () => {
 for (const [input, responseURL] of [["https://other.example/login","https://other.example/login"], ["/login", "https://other.example/redirect"]]) {
  const meta = makeElement({content:"local"}); meta.tagName = "META";
  const {context} = runModule(makeElement(), { querySelector: () => meta, querySelectorAll: () => [meta], fetch() {return Promise.resolve({url:responseURL,headers:{get:()=>"foreign"}});} });
  await context.window.__gosx.request(input,{method:"POST"});
  assert.equal(meta.getAttribute("content"), "local");
 }
});

test("anonymous JSON sign-in creates the browser token meta before the next mutation", async () => {
 const calls = [];
 let meta;
 const {context} = runModule(makeElement(), {
  querySelector: () => meta,
  querySelectorAll: () => meta ? [meta] : [],
  fetch(input,init) { calls.push({input,init}); return Promise.resolve({headers:{get:()=>"first-session"}}); },
 });
 context.document.createElement = () => { const el = makeElement(); el.tagName = "META"; return el; };
 context.document.head = {appendChild(el) { meta = el; }};
 await context.window.__gosx.request("/login", {method:"POST"});
 assert.equal(meta.getAttribute("name"), "csrf-token");
 assert.equal(meta.getAttribute("content"), "first-session");
 await context.window.__gosx.request("/save", {method:"POST"});
 assert.equal(calls[0].init.headers, undefined);
 assert.equal(calls[1].init.headers["X-CSRF-Token"], "first-session");
});
