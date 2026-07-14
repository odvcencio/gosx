// Unit tests for bootstrap-src/06-declarative-actions.js — the declarative
// interaction primitives (data-gosx-action / -submit-on / -set). Runs the module
// in a node:vm with a minimal DOM stub and asserts the delegated handlers.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "06-declarative-actions.js"),
  "utf8"
);

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
  const dispatched = [];
  const telemetry = [];
  const metaToken = options.csrfToken;
  const ctx = {
    console,
    URLSearchParams,
    FormData: class { constructor() {} }, // unused shape; body identity is enough
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
      __gosx_set_shared_signal: (name, payload) => { signals.push({ name, payload }); },
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
  return { listeners, fetches, signals, dispatched, telemetry, ctx };
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
