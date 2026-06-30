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
    matches(sel) { return matchSel(this, sel); },
    closest(sel) { let e = this; while (e) { if (matchSel(e, sel)) return e; e = e._parent || null; } return null; },
    querySelector() { return opts.submitBtn || null; },
    querySelectorAll() { return opts.textInputs || []; },
  };
  (opts.children || []).forEach((c) => { c._parent = el; });
  return el;
}

function runModule() {
  const listeners = {};
  const fetches = [];
  const signals = [];
  const ctx = {
    console,
    URLSearchParams,
    FormData: class { constructor() {} }, // unused shape; body identity is enough
    fetch: (url, opts) => { fetches.push({ url, opts }); return Promise.resolve({ ok: true }); },
    document: {
      addEventListener: (type, fn) => { listeners[type] = fn; },
    },
    window: {
      __gosx_set_shared_signal: (name, payload) => { signals.push({ name, payload }); },
    },
  };
  ctx.window.document = ctx.document;
  vm.createContext(ctx);
  vm.runInContext(moduleSrc, ctx);
  return { listeners, fetches, signals, ctx };
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
