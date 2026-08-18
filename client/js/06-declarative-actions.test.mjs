// Unit tests for runtime/host/actions.ts — the declarative
// interaction primitives (data-gosx-action / -submit-on / -set). Runs the module
// in a node:vm with a minimal DOM stub and asserts the delegated handlers.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = [
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "actions.ts"), "utf8"),
].join("\n");

// integratedModuleSrc loads the REAL shared-signal store (00-textlayout.js)
// alongside actions.ts, in the same order every bundle ships them (see
// cmd/buildbootstrap/main.go's outputs). No window.__gosx_set_shared_signal
// is installed here — no WASM engine has run — so this exercises the
// gosx#233 fallback path end to end: setSignal falls through to
// window.__gosx_notify_shared_signal, which is the exact function backing
// window.__gosx_subscribe_shared_signal's store. A passing test here proves
// delivery, not just that a fallback function got called.
//
// 00-textlayout.js's own IIFE never closes itself — real bundles concatenate
// dozens more files into that same function scope before a tail file closes
// it (see cmd/buildbootstrap/main.go). This slice needs only its public
// window.* surface, so it closes the IIFE itself right after the file.
const textLayoutSourceStandalone = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "00-textlayout.js"), "utf8"
) + "\n})();\n";
const integratedModuleSrc = [
  textLayoutSourceStandalone,
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "actions.ts"), "utf8"),
].join("\n");

function runIntegratedActionsModule(options = {}) {
  const listeners = {};
  const ctx = {
    console,
    URLSearchParams,
    // 00-textlayout.js reads this bare name at module scope — it normally
    // comes from 05-document-env.ts, concatenated into the same bundle
    // scope in every real bootstrap (see cmd/buildbootstrap/main.go). This
    // slice carries neither the definition nor any caller of it.
    gosxLowEndHardware: false,
    fetch: () => Promise.resolve({
      ok: options.responseOK !== false,
      status: options.responseStatus || 200,
      json: () => Promise.resolve(options.responsePayload),
      clone() { return this; },
    }),
    document: {
      addEventListener: (type, fn) => { listeners[type] = fn; },
      dispatchEvent: () => {},
      querySelector: () => null,
      querySelectorAll: () => [],
      readyState: "complete",
    },
    window: {},
  };
  ctx.window.document = ctx.document;
  class CustomEvent {
    constructor(type, init = {}) { this.type = type; this.detail = init.detail; }
  }
  ctx.CustomEvent = CustomEvent;
  vm.createContext(ctx);
  vm.runInContext(integratedModuleSrc, ctx);
  return { listeners, ctx };
}

// Minimal selector matcher: tag, [attr], [attr='val'], tag[attr], tag[attr='val'].
function matchSel(el, sel) {
  const m = /^([a-zA-Z]*)(?:\[([a-zA-Z-]+)(?:=['"]?([^'"\]]*)['"]?)?\])?$/.exec(sel.trim());
  if (!m) return false;
  const [, tag, attr, val] = m;
  if (tag && el.tagName !== tag.toUpperCase()) return false;
  if (attr) {
    if (!el.hasAttribute(attr)) return false;
    if (val !== undefined && val !== "" && el.getAttribute(attr) !== val) return false;
  }
  return true;
}

function makeEl(attrs = {}, opts = {}) {
  const el = {
    _attrs: attrs,
    tagName: (opts.tag || "div").toUpperCase(),
    disabled: false,
    isConnected: opts.isConnected !== false,
    name: opts.name || attrs.name || "",
    value: opts.value || attrs.value || "",
    form: opts.form || null,
    getAttribute(n) { return n in this._attrs ? this._attrs[n] : null; },
    hasAttribute(n) { return n in this._attrs; },
    setAttribute(n, v) { this._attrs[n] = String(v); },
    removeAttribute(n) { delete this._attrs[n]; },
    matches(sel) { return matchSel(this, sel); },
    closest(sel) { let e = this; while (e) { if (matchSel(e, sel)) return e; e = e._parent || null; } return null; },
    querySelector(sel) { return opts.querySelector ? opts.querySelector(sel) : (opts.submitBtn || null); },
    querySelectorAll(sel) { return opts.querySelectorAll ? opts.querySelectorAll(sel) : (opts.textInputs || []); },
    focus() { this.focused = true; },
  };
  (opts.children || []).forEach((c) => { c._parent = el; });
  return el;
}

// runModule(options.csrfToken) stubs document.querySelector('meta[name="csrf-token"]')
// to return a fake <meta> element carrying options.csrfToken as its "content"
// attribute — mirrors the meta tag m31labs.dev/gosx/server's AddHeadDecorator
// hook renders when a page's session carries a CSRF token (see
// kiln/authwire/wire.go). Omitting csrfToken (or passing "") mimics a page
// with no Protect-backed token — the backward-compat case.
function runModule(options = {}) {
  const listeners = {};
  const fetches = [];
  const signals = [];
  const notifications = [];
  const dispatched = [];
  const telemetry = [];
  const metaToken = options.csrfToken;
  const ctx = {
    console,
    URLSearchParams,
    FormData: class {
      constructor(form) {
        this.entries = form && Array.isArray(form._formEntries) ? form._formEntries.slice() : [];
      }
      append(key, value) {
        this.entries.push([String(key), value == null ? "" : String(value)]);
      }
      has(key) {
        return this.entries.some((entry) => entry[0] === String(key));
      }
      [Symbol.iterator]() {
        return this.entries[Symbol.iterator]();
      }
    },
    fetch: (url, opts) => {
      fetches.push({ url, opts });
      const response = {
        ok: options.responseOK !== false,
        status: options.responseStatus || (options.responseOK === false ? 500 : 200),
      };
      if (options.responsePayload !== undefined) {
        response.json = () => Promise.resolve(options.responsePayload);
      }
      return Promise.resolve(response);
    },
    document: {
      addEventListener: (type, fn) => { listeners[type] = fn; },
      dispatchEvent: (event) => { dispatched.push(event); },
      querySelector: (sel) => {
        if (sel === 'meta[name="csrf-token"]' && metaToken !== undefined) {
          return { getAttribute: (n) => (n === "content" ? metaToken : null) };
        }
        if (sel === options.targetSelector) return options.target || null;
        if (options.queryMap && sel in options.queryMap) return options.queryMap[sel];
        return null;
      },
      querySelectorAll: (sel) => options.queryAll && sel in options.queryAll ? options.queryAll[sel] : [],
      activeElement: options.activeElement || null,
    },
    window: {
      __gosx: Object.assign(
        {},
        options.coreRequest ? { request: options.coreRequest } : {},
        options.reportFailure ? { reportFailure: options.reportFailure } : {}
      ),
      // A page with no WASM engine never installs __gosx_set_shared_signal
      // (gosx#233). options.noSharedSignalWriter models that page so tests
      // can assert on the window.__gosx_notify_shared_signal fallback below.
      ...(options.noSharedSignalWriter ? {} : {
        __gosx_set_shared_signal: options.sharedSignalWriter || ((name, payload) => { signals.push({ name, payload }); }),
      }),
      __gosx_notify_shared_signal: (name, payload) => { notifications.push({ name, payload }); },
      __gosx_emit: (level, category, message, fields) => telemetry.push({ level, category, message, fields }),
      ...(options.replaceRuntimeContent ? {
        __gosx_replace_runtime_content: options.replaceRuntimeContent,
      } : {}),
    },
  };
  class CustomEvent {
    constructor(type, init = {}) { this.type = type; this.detail = init.detail; }
  }
  ctx.window.document = ctx.document;
  ctx.CustomEvent = CustomEvent;
  vm.createContext(ctx);
  vm.runInContext(moduleSrc, ctx);
  return { listeners, fetches, signals, notifications, dispatched, telemetry, ctx };
}

function fire(listener, target) {
  let prevented = false;
  listener({ target, preventDefault: () => { prevented = true; } });
  return prevented;
}

test("data-gosx-set writes the shared signal on click", () => {
  const { listeners, signals } = runModule();
  const row = makeEl({ "data-gosx-set": "$sel", "data-gosx-set-value": "obj-7" }, { tag: "a" });
  const prevented = fire(listeners.click, row);
  assert.equal(prevented, true);
  assert.deepEqual(signals, [{ name: "$sel", payload: JSON.stringify("obj-7") }]);
});

// --- shared-signal write fallback with no WASM engine (gosx#233) -----------
//
// A page with no WASM engine never installs window.__gosx_set_shared_signal.
// Before gosx#233, setSignal simply did nothing in that case: an action
// declaring data-gosx-action-signal (or a data-gosx-set click) wrote
// nothing, and every subscriber stayed silent with no error. setSignal now
// falls back to window.__gosx_notify_shared_signal — the same JS-only
// writer 00-textlayout.js already uses for its own store — whenever the
// engine hook is absent or fails.

test("data-gosx-set falls back to window.__gosx_notify_shared_signal when no WASM engine is installed", () => {
  const { listeners, signals, notifications } = runModule({ noSharedSignalWriter: true });
  const row = makeEl({ "data-gosx-set": "$sel", "data-gosx-set-value": "obj-7" }, { tag: "a" });
  fire(listeners.click, row);
  assert.deepEqual(signals, [], "no engine writer means no engine call");
  assert.deepEqual(notifications, [{ name: "$sel", payload: JSON.stringify("obj-7") }]);
});

test("data-gosx-action-signal falls back to window.__gosx_notify_shared_signal when no WASM engine is installed", async () => {
  const { listeners, signals, notifications } = runModule({
    noSharedSignalWriter: true,
    responsePayload: { value: "saved" },
  });
  const btn = makeEl({
    "data-gosx-action": "POST /api/save",
    "data-gosx-action-signal": "$note.status",
  }, { tag: "button" });
  fire(listeners.click, btn);
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(signals, []);
  assert.deepEqual(notifications, [{ name: "$note.status", payload: JSON.stringify("saved") }]);
});

test("an installed WASM engine writer wins and setSignal does not also call the JS-only fallback", () => {
  const { listeners, signals, notifications } = runModule({
    sharedSignalWriter: (name, payload) => { signals.push({ name, payload }); return null; },
  });
  const row = makeEl({ "data-gosx-set": "$sel", "data-gosx-set-value": "obj-7" }, { tag: "a" });
  fire(listeners.click, row);
  assert.deepEqual(signals, [{ name: "$sel", payload: JSON.stringify("obj-7") }]);
  assert.deepEqual(notifications, [], "the engine succeeded, so the fallback must not also fire");
});

test("a WASM engine writer that reports an error still reaches subscribers through the fallback", () => {
  const { listeners, signals, notifications } = runModule({
    sharedSignalWriter: (name, payload) => { signals.push({ name, payload }); return "error: island disposed"; },
  });
  const row = makeEl({ "data-gosx-set": "$sel", "data-gosx-set-value": "obj-7" }, { tag: "a" });
  fire(listeners.click, row);
  assert.deepEqual(signals, [{ name: "$sel", payload: JSON.stringify("obj-7") }], "the engine writer still ran once");
  assert.deepEqual(notifications, [{ name: "$sel", payload: JSON.stringify("obj-7") }], "its error routes the write through the fallback too");
});

test("declarative actions publish a core API while retaining the delegated transport", () => {
  const { ctx } = runModule();
  assert.equal(typeof ctx.window.__gosx.actions.run, "function");
  assert.equal(typeof ctx.window.__gosx.actions.parse, "function");
  assert.equal(typeof ctx.window.__gosx.actions.applyResult, "function");
  assert.equal(typeof ctx.window.__gosx.actions.refreshBindings, "function");
  assert.equal(typeof ctx.window.__gosx.actions.openDisclosure, "function");
  assert.equal(ctx.window.__gosx.actions.parse("PUT /notes", "POST").method, "PUT");
  assert.equal(ctx.window.__gosx.actions.parse("PUT /notes", "POST").url, "/notes");
  assert.equal(ctx.window.__gosx_declarative_actions, ctx.window.__gosx.actions);
});

test("data-gosx-toggle-target owns attribute and aria state without page JS", () => {
  const drawer = makeEl();
  const trigger = makeEl({
    "data-gosx-toggle-target": "#drawer",
    "data-gosx-toggle-attribute": "data-open",
    "aria-expanded": "false",
  }, { tag: "button" });
  const { listeners } = runModule({ queryMap: { "#drawer": drawer } });
  fire(listeners.click, trigger);
  assert.equal(drawer.getAttribute("data-open"), "true");
  assert.equal(trigger.getAttribute("aria-expanded"), "true");
  fire(listeners.click, trigger);
  assert.equal(drawer.hasAttribute("data-open"), false);
  assert.equal(trigger.getAttribute("aria-expanded"), "false");
});

test("navigation context binding projects selected source data declaratively", () => {
  const source = makeEl({ "data-title": "Water Lab", "data-slug": "water" }, { tag: "a" });
  const title = makeEl({ "data-gosx-bind-text": "data-title" });
  const root = makeEl({
    "data-gosx-bind-source": ".active",
    "data-gosx-bind-attr": "data-active:data-slug",
  }, { querySelectorAll: () => [title] });
  const { ctx } = runModule({
    queryMap: { ".active": source },
    queryAll: { "[data-gosx-bind-source]": [root] },
  });
  ctx.window.__gosx.actions.refreshBindings();
  assert.equal(root.getAttribute("data-active"), "water");
  assert.equal(title.textContent, "Water Lab");
});

test("declarative disclosure manages visibility, aria, and focus restoration", () => {
  const close = makeEl({ "data-gosx-disclosure-initial-focus": "" }, { tag: "button" });
  const panel = makeEl({ id: "details", hidden: "" }, { querySelector: () => close });
  panel.hidden = true;
  const trigger = makeEl({ "data-gosx-disclosure-target": "#details", "aria-expanded": "false" }, { tag: "button" });
  const backdrop = makeEl({ "data-gosx-disclosure-backdrop": "#details", hidden: "" });
  backdrop.hidden = true;
  const { ctx } = runModule({
    activeElement: trigger,
    queryMap: {
      "#details": panel,
      '[data-gosx-disclosure-target="#details"]': trigger,
    },
    queryAll: { '[data-gosx-disclosure-backdrop="#details"]': [backdrop] },
  });
  ctx.window.__gosx.actions.openDisclosure(trigger);
  assert.equal(panel.hidden, false);
  assert.equal(backdrop.hidden, false);
  assert.equal(trigger.getAttribute("aria-expanded"), "true");
  assert.equal(close.focused, true);
  ctx.window.__gosx.actions.closeDisclosure(panel);
  assert.equal(panel.hidden, true);
  assert.equal(trigger.getAttribute("aria-expanded"), "false");
  assert.equal(trigger.focused, true);
});

test("data-gosx-action button POSTs, disables during flight, re-enables on settle", async () => {
  const { listeners, fetches } = runModule();
  const btn = makeEl({ "data-gosx-action": "POST /api/x/accept" }, { tag: "button" });
  fire(listeners.click, btn);
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].url, "/api/x/accept");
  assert.equal(fetches[0].opts.method, "POST");
  assert.equal(btn.disabled, true, "disabled during flight");
  // After the fetch settles (2xx), the button must be usable again so a
  // persistent submit (composer/comment/suggest) can be re-fired without reload.
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(btn.disabled, false, "re-enabled on settle");
});

test("data-gosx-action form submits via fetch and does not navigate", () => {
  const { listeners, fetches } = runModule();
  const form = makeEl({ "data-gosx-action": "" , method: "POST", action: "/api/x/agent" }, { tag: "form" });
  const prevented = fire(listeners.submit, form);
  assert.equal(prevented, true);
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].url, "/api/x/agent");
  assert.equal(fetches[0].opts.method, "POST");
});

test("data-gosx-action form includes the clicked submitter value", () => {
  const { listeners, fetches } = runModule();
  const submit = makeEl({ type: "submit", name: "post_action", value: "publish" }, { tag: "button" });
  const form = makeEl({ "data-gosx-action": "", method: "POST", action: "/api/x/post" }, {
    tag: "form",
    submitBtn: submit,
  });
  form._formEntries = [["title", "Hello"]];
  const prevented = listeners.submit({
    target: form,
    submitter: submit,
    preventDefault: () => true,
  });
  assert.equal(prevented, undefined);
  assert.equal(fetches.length, 1);
  assert.equal(String(fetches[0].opts.body), "title=Hello&post_action=publish");
});

test("data-gosx-action form honors submitter formaction and formmethod", () => {
  const { listeners, fetches } = runModule();
  const submit = makeEl({
    type: "submit",
    name: "post_action",
    value: "schedule",
    formaction: "/api/x/schedule",
    formmethod: "PATCH",
  }, { tag: "button" });
  const form = makeEl({ "data-gosx-action": "", method: "POST", action: "/api/x/update" }, {
    tag: "form",
    submitBtn: submit,
  });
  form._formEntries = [["publish_at", "2026-07-29T16:30"]];
  listeners.submit({ target: form, submitter: submit, preventDefault() {} });
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].url, "/api/x/schedule");
  assert.equal(fetches[0].opts.method, "PATCH");
  assert.equal(String(fetches[0].opts.body), "publish_at=2026-07-29T16%3A30&post_action=schedule");
});

test("data-gosx-action form lets the submitter own the patch target", async () => {
  const formTarget = { innerHTML: "<p>form</p>" };
  const buttonTarget = { innerHTML: "<p>button</p>" };
  const submit = makeEl({
    type: "submit",
    name: "post_action",
    value: "publish",
    "data-gosx-action-target": "#button-status",
  }, { tag: "button" });
  const form = makeEl({
    "data-gosx-action": "",
    "data-gosx-action-target": "#form-status",
    method: "POST",
    action: "/api/x/post",
  }, { tag: "form", submitBtn: submit });
  const { listeners } = runModule({
    queryMap: {
      "#form-status": formTarget,
      "#button-status": buttonTarget,
    },
    responsePayload: { html: "<p>Published</p>" },
  });
  listeners.submit({ target: form, submitter: submit, preventDefault() {} });
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(formTarget.innerHTML, "<p>form</p>");
  assert.equal(buttonTarget.innerHTML, "<p>Published</p>");
});

test("data-gosx-action form exposes pending state during request and restores it", async () => {
  let resolveFetch;
  const { listeners } = runModule({
    coreRequest() {
      return new Promise((resolve) => {
        resolveFetch = () => resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
      });
    },
  });
  const submit = makeEl({ type: "submit" }, { tag: "button" });
  const form = makeEl({
    "data-gosx-action": "",
    "data-gosx-form-state": "idle",
    method: "POST",
    action: "/api/x/post",
  }, { tag: "form", submitBtn: submit });
  listeners.submit({ target: form, submitter: submit, preventDefault() {} });
  assert.equal(form.getAttribute("data-gosx-pending"), "true");
  assert.equal(form.getAttribute("data-gosx-form-state"), "pending");
  resolveFetch();
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(form.hasAttribute("data-gosx-pending"), false);
  assert.equal(form.getAttribute("data-gosx-form-state"), "idle");
});

test("data-gosx-submit-on=change requests its form submit", () => {
  const { listeners } = runModule();
  let submitted = false;
  const form = { requestSubmit: () => { submitted = true; } };
  const input = makeEl({ "data-gosx-submit-on": "change" }, { tag: "input", form });
  fire(listeners.change, input);
  assert.equal(submitted, true);
});

// --- CSRF token attachment (mirrors m31labs.dev/gosx/session.Manager.Protect's
// expected X-CSRF-Token header) ---

test("data-gosx-action click POST attaches X-CSRF-Token when the page carries a csrf-token meta tag", () => {
  const { listeners, fetches } = runModule({ csrfToken: "tok-abc123" });
  const btn = makeEl({ "data-gosx-action": "POST /api/x/accept" }, { tag: "button" });
  fire(listeners.click, btn);
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].opts.headers["X-CSRF-Token"], "tok-abc123");
});

test("standard action requests delegate CSRF policy to the core transport", async () => {
  const requests = [];
  const { listeners } = runModule({
    csrfToken: "tok-core",
    coreRequest(url, opts) {
      requests.push({ url, opts });
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
    },
  });
  const btn = makeEl({ "data-gosx-action": "POST /api/core" }, { tag: "button" });
  fire(listeners.click, btn);
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(requests.length, 1);
  assert.equal(requests[0].opts.headers["X-CSRF-Token"], undefined);
});

test("data-gosx-action form submit attaches X-CSRF-Token when a csrf-token meta tag is present", () => {
  const { listeners, fetches } = runModule({ csrfToken: "tok-form-1" });
  const form = makeEl({ "data-gosx-action": "", method: "POST", action: "/api/x/agent" }, { tag: "form" });
  fire(listeners.submit, form);
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].opts.headers["X-CSRF-Token"], "tok-form-1");
});

test("data-gosx-action attaches X-CSRF-Token for PUT/PATCH/DELETE, not just POST", () => {
  for (const method of ["PUT", "PATCH", "DELETE"]) {
    const { listeners, fetches } = runModule({ csrfToken: "tok-mut" });
    const btn = makeEl({ "data-gosx-action": method + " /api/x/thing" }, { tag: "button" });
    fire(listeners.click, btn);
    assert.equal(fetches.length, 1, method + " should fetch once");
    assert.equal(fetches[0].opts.headers["X-CSRF-Token"], "tok-mut", method + " should carry the token");
  }
});

test("data-gosx-action does not attach X-CSRF-Token for a GET action, even with a token present", () => {
  const { listeners, fetches } = runModule({ csrfToken: "tok-get" });
  const btn = makeEl({ "data-gosx-action": "GET /api/x/refresh" }, { tag: "button" });
  fire(listeners.click, btn);
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].opts.method, "GET");
  assert.equal("X-CSRF-Token" in fetches[0].opts.headers, false, "GET must never carry a CSRF header");
});

test("data-gosx-action omits X-CSRF-Token when no csrf-token meta tag is present (backward compat)", () => {
  // No csrfToken option at all — document.querySelector('meta[name="csrf-token"]')
  // returns null, matching an app that never mounted session.Manager.Protect.
  const { listeners, fetches } = runModule();
  const btn = makeEl({ "data-gosx-action": "POST /api/x/accept" }, { tag: "button" });
  fire(listeners.click, btn);
  assert.equal(fetches.length, 1);
  assert.equal("X-CSRF-Token" in fetches[0].opts.headers, false);
});

test("data-gosx-action omits X-CSRF-Token when the csrf-token meta tag is present but empty", () => {
  const { listeners, fetches } = runModule({ csrfToken: "" });
  const btn = makeEl({ "data-gosx-action": "POST /api/x/accept" }, { tag: "button" });
  fire(listeners.click, btn);
  assert.equal(fetches.length, 1);
  assert.equal("X-CSRF-Token" in fetches[0].opts.headers, false);
});

test("data-gosx-action emits typed result and custom lifecycle events", async () => {
  const { listeners, dispatched, telemetry } = runModule();
  const btn = makeEl({
    "data-gosx-action": "POST /api/save",
    "data-gosx-action-event": "note:saved",
  }, { tag: "button" });
  fire(listeners.click, btn);
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(dispatched.map((event) => event.type), ["gosx:action:result", "note:saved"]);
  assert.equal(dispatched[0].detail.url, "/api/save");
  assert.equal(dispatched[0].detail.ok, true);
  assert.equal(telemetry.at(-1).category, "action");
  assert.equal(telemetry.at(-1).message, "action completed");
});

test("action failures delegate diagnostics and telemetry policy to core", async () => {
  const failures = [];
  const { listeners } = runModule({
    responseOK: false,
    responseStatus: 503,
    reportFailure(operation, error, fields) {
      failures.push({ operation, error, fields });
    },
  });
  const btn = makeEl({ "data-gosx-action": "POST /api/save" }, { tag: "button" });
  fire(listeners.click, btn);
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(failures.length, 1);
  assert.equal(failures[0].operation, "action response");
  assert.match(failures[0].error.message, /503/);
  assert.equal(JSON.stringify(failures[0].fields.telemetry), JSON.stringify({
    method: "POST",
    url: "/api/save",
    status: 503,
  }));
});

test("data-gosx-action transports response values to signals and HTML to a target", async () => {
  const target = { innerHTML: "<p>old</p>" };
  const { listeners, signals } = runModule({
    targetSelector: "#status",
    target,
    responsePayload: { value: "saved", html: "<p>Saved</p>" },
  });
  const btn = makeEl({
    "data-gosx-action": "POST /api/save",
    "data-gosx-action-signal": "$note.status",
    "data-gosx-action-target": "#status",
  }, { tag: "button" });
  fire(listeners.click, btn);
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(signals, [{ name: "$note.status", payload: JSON.stringify("saved") }]);
  assert.equal(target.innerHTML, "<p>Saved</p>");
});

test("data-gosx-action delegates target replacement to the core runtime DOM lifecycle", async () => {
  const target = { innerHTML: "<p>old</p>" };
  const replacements = [];
  const { listeners } = runModule({
    targetSelector: "#status",
    target,
    responsePayload: { html: "<p>Saved</p>" },
    replaceRuntimeContent(nextTarget, html) {
      replacements.push({ nextTarget, html });
      nextTarget.innerHTML = html;
      return true;
    },
  });
  const btn = makeEl({
    "data-gosx-action": "POST /api/save",
    "data-gosx-action-target": "#status",
  }, { tag: "button" });
  fire(listeners.click, btn);
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.deepEqual(replacements, [{ nextTarget: target, html: "<p>Saved</p>" }]);
});

test("data-gosx-action target patch restores focus without scrolling", async () => {
  const nextButton = makeEl({}, { tag: "button" });
  let focusOptions = null;
  nextButton.focus = (options) => { focusOptions = options; nextButton.focused = true; };
  const target = {
    innerHTML: "<p>old</p>",
    querySelector: () => nextButton,
  };
  const oldButton = makeEl({
    "data-gosx-action": "POST /api/save",
    "data-gosx-action-target": "#status",
  }, { tag: "button", isConnected: false });
  const { listeners } = runModule({
    targetSelector: "#status",
    target,
    responsePayload: { html: "<button>Saved</button>" },
  });
  fire(listeners.click, oldButton);
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(nextButton.focused, true);
  assert.equal(focusOptions && focusOptions.preventScroll, true);
});

// --- end-to-end shared-signal delivery against the real store (gosx#233) ---

test("data-gosx-action-signal delivers to a real window.__gosx_subscribe_shared_signal listener with no WASM engine installed", async () => {
  const { listeners, ctx } = runIntegratedActionsModule({
    responsePayload: { value: "saved-42" },
  });
  assert.equal(typeof ctx.window.__gosx_set_shared_signal, "undefined", "no engine writer is installed");
  assert.equal(typeof ctx.window.__gosx_subscribe_shared_signal, "function", "00-textlayout.js installed the real subscribe API");

  const received = [];
  ctx.window.__gosx_subscribe_shared_signal("$note.status", (value, name) => {
    received.push({ value, name });
  }, { immediate: false });

  const btn = makeEl({
    "data-gosx-action": "POST /api/save",
    "data-gosx-action-signal": "$note.status",
  }, { tag: "button" });
  fire(listeners.click, btn);
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));

  assert.deepEqual(received, [{ value: "saved-42", name: "$note.status" }]);
});

test("data-gosx-action-signal prefers an installed WASM engine writer and does not double-notify the real store", async () => {
  const { listeners, ctx } = runIntegratedActionsModule({
    responsePayload: { value: "engine-value" },
  });
  const engineCalls = [];
  // Mirrors client/wasm/main.go's sharedSignalRuntimeFunc: js.Null() (not a
  // string) on success.
  ctx.window.__gosx_set_shared_signal = (name, valueJSON) => {
    engineCalls.push({ name, valueJSON });
    return null;
  };

  const received = [];
  ctx.window.__gosx_subscribe_shared_signal("$note.status", (value, name) => {
    received.push({ value, name });
  }, { immediate: false });

  const btn = makeEl({
    "data-gosx-action": "POST /api/save",
    "data-gosx-action-signal": "$note.status",
  }, { tag: "button" });
  fire(listeners.click, btn);
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));

  assert.deepEqual(engineCalls, [{ name: "$note.status", valueJSON: JSON.stringify("engine-value") }]);
  // The engine already owns notifying its own subscribers; the JS-only
  // fallback store must stay untouched, or a subscriber on the engine's own
  // channel would see the write twice.
  assert.deepEqual(received, [], "the JS-only store does not also receive the write when the engine succeeds");
});
