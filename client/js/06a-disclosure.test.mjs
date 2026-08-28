import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));
const compatibility = fs.readFileSync(path.join(dirname, "..", "runtime", "host", "compatibility.ts"), "utf8");
const disclosure = fs.readFileSync(path.join(dirname, "..", "runtime", "host", "disclosure.ts"), "utf8");

function selectorPartMatches(element, selector) {
  selector = selector.trim();
  if (selector.startsWith("#")) return element.getAttribute("id") === selector.slice(1);
  const tag = /^([a-z]+)/i.exec(selector);
  if (tag && element.tagName !== tag[1].toUpperCase()) return false;
  const positiveSelector = selector.replace(/:not\(\[[^\]]+\]\)/g, "");
  for (const match of positiveSelector.matchAll(/\[([a-z0-9-]+)(?:=["']([^"']*)["'])?\]/gi)) {
    if (!element.hasAttribute(match[1])) return false;
    if (match[2] !== undefined && element.getAttribute(match[1]) !== match[2]) return false;
  }
  if (selector.includes(":not([disabled])") && element.disabled) return false;
  if (selector.includes(':not([tabindex="-1"])') && element.getAttribute("tabindex") === "-1") return false;
  return Boolean(tag || selector.startsWith("["));
}

function matches(element, selector) {
  return selector.split(",").some((part) => selectorPartMatches(element, part));
}

class FakeElement {
  constructor(tag = "div", attrs = {}) {
    this.tagName = tag.toUpperCase();
    this._attrs = { ...attrs };
    this.children = [];
    this.parentNode = null;
    this.ownerDocument = null;
    this.hidden = "hidden" in attrs;
    this.disabled = false;
    this.isConnected = true;
  }
  appendChild(child) {
    child.parentNode = this;
    child.ownerDocument = this.ownerDocument;
    this.children.push(child);
    return child;
  }
  getAttribute(name) { return name in this._attrs ? this._attrs[name] : null; }
  hasAttribute(name) { return name in this._attrs; }
  setAttribute(name, value) { this._attrs[name] = String(value); }
  removeAttribute(name) { delete this._attrs[name]; }
  contains(node) {
    if (node === this) return true;
    return this.children.some((child) => child.contains(node));
  }
  matches(selector) { return matches(this, selector); }
  closest(selector) {
    for (let node = this; node; node = node.parentNode) {
      if (matches(node, selector)) return node;
    }
    return null;
  }
  querySelectorAll(selector) {
    const found = [];
    const visit = (node) => {
      for (const child of node.children) {
        if (matches(child, selector)) found.push(child);
        visit(child);
      }
    };
    visit(this);
    return found;
  }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  focus() {
    this.focused = (this.focused || 0) + 1;
    if (this.ownerDocument) this.ownerDocument.activeElement = this;
  }
}

function environment({ evaluateTwice = false } = {}) {
  const listeners = new Map();
  const document = {
    activeElement: null,
    addEventListener(type, fn) {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push(fn);
    },
    querySelector(selector) { return this.body.querySelector(selector); },
    querySelectorAll(selector) { return this.body.querySelectorAll(selector); },
    contains(node) { return this.body.contains(node); },
  };
  document.body = new FakeElement("body");
  document.body.ownerDocument = document;
  const context = { window: {}, document, console, Map, setTimeout };
  context.window.document = document;
  vm.createContext(context);
  vm.runInContext(compatibility + "\n" + disclosure + (evaluateTwice ? "\n" + disclosure : ""), context);
  function append(element) {
    document.body.appendChild(element);
    const setOwner = (node) => {
      node.ownerDocument = document;
      node.children.forEach(setOwner);
    };
    setOwner(element);
    return element;
  }
  function dispatch(type, init = {}) {
    const event = {
      type,
      key: init.key || "",
      shiftKey: Boolean(init.shiftKey),
      target: init.target || document.activeElement,
      defaultPrevented: false,
      preventDefault() { this.defaultPrevented = true; },
    };
    for (const listener of listeners.get(type) || []) listener(event);
    return event;
  }
  return { context, document, listeners, append, dispatch };
}

function disclosureFixture(env, id, { modal = false, initial = true, focusable = true } = {}) {
  const trigger = env.append(new FakeElement("button", { "data-gosx-disclosure-target": `#${id}`, "aria-expanded": "false" }));
  const background = env.append(new FakeElement("main"));
  const backdrop = env.append(new FakeElement("div", { "data-gosx-disclosure-backdrop": `#${id}`, hidden: "" }));
  const panelAttrs = { id, "data-gosx-disclosure": "", hidden: "" };
  if (modal) panelAttrs["aria-modal"] = "true";
  const panel = env.append(new FakeElement("section", panelAttrs));
  let first = null;
  if (focusable) {
    first = panel.appendChild(new FakeElement("button", initial ? { "data-gosx-disclosure-initial-focus": "" } : {}));
    first.ownerDocument = env.document;
  }
  return { trigger, background, backdrop, panel, first };
}

test("disclosure authority installs exactly once across inline and bootstrap evaluation", () => {
  const env = environment({ evaluateTwice: true });
  assert.equal(env.listeners.get("click").length, 1);
  assert.equal(env.listeners.get("keydown").length, 1);
  assert.equal(env.listeners.get("gosx:navigate").length, 1);
  assert.strictEqual(env.context.window.__gosx.disclosure, env.context.window.__gosx.host.disclosure);
});

test("modal disclosure manages focus, inert background, exact restoration, and navigation cleanup", () => {
  const env = environment();
  const item = disclosureFixture(env, "modal", { modal: true });
  item.background.inert = false;
  item.background.setAttribute("inert", "authored");
  item.trigger.focus();

  assert.equal(env.context.window.__gosx.disclosure.open(item.trigger), true);
  assert.equal(item.panel.hidden, false);
  assert.equal(item.backdrop.hidden, false);
  assert.equal(item.trigger.getAttribute("aria-expanded"), "true");
  assert.strictEqual(env.document.activeElement, item.first);
  assert.equal(item.trigger.inert, true);
  assert.equal(item.background.inert, true);

  env.dispatch("gosx:navigate");
  assert.equal(item.panel.hidden, true);
  assert.equal(item.trigger.getAttribute("aria-expanded"), "false");
  assert.equal("inert" in item.trigger, false);
  assert.equal(item.background.inert, false);
  assert.equal(item.background.getAttribute("inert"), "authored");
  assert.equal(env.context.window.__gosx.disclosure.size(), 0);
  assert.equal(item.trigger.focused, 1, "navigation cleanup must not return focus");
});

test("Escape closes the topmost disclosure independent of focus and Tab stays trapped", () => {
  const env = environment();
  const lower = disclosureFixture(env, "lower", { initial: false });
  const upper = disclosureFixture(env, "upper", { initial: false });
  const last = upper.panel.appendChild(new FakeElement("button"));
  last.ownerDocument = env.document;
  env.context.window.__gosx.disclosure.open(lower.trigger);
  env.context.window.__gosx.disclosure.open(upper.trigger);

  last.focus();
  let event = env.dispatch("keydown", { key: "Tab", target: last });
  assert.equal(event.defaultPrevented, true);
  assert.strictEqual(env.document.activeElement, upper.first);

  lower.background.focus();
  event = env.dispatch("keydown", { key: "Tab", target: lower.background });
  assert.equal(event.defaultPrevented, true);
  assert.strictEqual(env.document.activeElement, upper.first);

  lower.background.focus();
  event = env.dispatch("keydown", { key: "Escape", target: lower.background });
  assert.equal(event.defaultPrevented, true);
  assert.equal(upper.panel.hidden, true);
  assert.equal(lower.panel.hidden, false);
  assert.strictEqual(env.context.window.__gosx.disclosure.top(), lower.panel);
});

test("focus falls back from initial to first to panel and returns only to connected owners", () => {
  const env = environment();
  const firstFallback = disclosureFixture(env, "first", { initial: false });
  firstFallback.trigger.focus();
  env.context.window.__gosx.disclosure.open(firstFallback.trigger);
  assert.strictEqual(env.document.activeElement, firstFallback.first);
  firstFallback.trigger.isConnected = false;
  env.context.window.__gosx.disclosure.close(firstFallback.panel);
  assert.equal(firstFallback.trigger.focused, 1, "disconnected trigger must not receive return focus");

  const panelFallback = disclosureFixture(env, "panel", { focusable: false });
  panelFallback.trigger.focus();
  env.context.window.__gosx.disclosure.open(panelFallback.trigger);
  assert.strictEqual(env.document.activeElement, panelFallback.panel);
  assert.equal(panelFallback.panel.getAttribute("tabindex"), "-1");
  env.context.window.__gosx.disclosure.close(panelFallback.panel);
  assert.equal(panelFallback.panel.hasAttribute("tabindex"), false);
  assert.equal(panelFallback.trigger.focused, 2);
});

test("a disconnected top panel is cleaned without focus return or inert leakage", () => {
  const env = environment();
  const item = disclosureFixture(env, "removed", { modal: true });
  item.trigger.focus();
  env.context.window.__gosx.disclosure.open(item.trigger);

  item.panel.isConnected = false;
  assert.equal(env.context.window.__gosx.disclosure.top(), null);
  assert.equal(env.context.window.__gosx.disclosure.size(), 0);
  assert.equal(item.trigger.getAttribute("aria-expanded"), "false");
  assert.equal("inert" in item.trigger, false);
  assert.equal(item.trigger.focused, 1, "disconnect cleanup must not return focus");
});
