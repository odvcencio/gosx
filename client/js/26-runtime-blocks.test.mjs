// Contract tests for the core keyed HTML stream reconciler.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "26-runtime-blocks.js"),
  "utf8"
);

function makeNode(tag, text = "") {
  const attrs = new Map();
  const node = {
    nodeType: 1,
    nodeValue: null,
    tagName: tag.toUpperCase(),
    textContent: text,
    parentNode: null,
    getAttribute(name) { return attrs.has(name) ? attrs.get(name) : null; },
    setAttribute(name, value) { attrs.set(name, String(value)); },
    removeAttribute(name) { attrs.delete(name); },
    cloneNode() {
      const clone = makeNode(tag, text);
      for (const [name, value] of attrs) clone.setAttribute(name, value);
      return clone;
    },
    remove() {
      if (this.parentNode) this.parentNode.removeChild(this);
    },
  };
  Object.defineProperty(node, "outerHTML", {
    get() {
      const attributes = Array.from(attrs.entries())
        .map(([name, value]) => ` ${name}="${value}"`)
        .join("");
      return `<${tag}${attributes}>${text}</${tag}>`;
    },
  });
  return node;
}

function makeRoot(nodes = []) {
  const root = {
    childNodes: [],
    get children() { return this.childNodes; },
    get lastElementChild() { return this.childNodes[this.childNodes.length - 1] || null; },
    appendChild(node) {
      node.parentNode = this;
      this.childNodes.push(node);
      return node;
    },
    removeChild(node) {
      const index = this.childNodes.indexOf(node);
      if (index >= 0) this.childNodes.splice(index, 1);
      node.parentNode = null;
      return node;
    },
    replaceChild(next, previous) {
      const index = this.childNodes.indexOf(previous);
      if (index >= 0) {
        next.parentNode = this;
        previous.parentNode = null;
        this.childNodes[index] = next;
      }
      return previous;
    },
    insertBefore(node, anchor) {
      const currentIndex = this.childNodes.indexOf(node);
      if (currentIndex >= 0) this.childNodes.splice(currentIndex, 1);
      const index = anchor ? this.childNodes.indexOf(anchor) : this.childNodes.length;
      node.parentNode = this;
      this.childNodes.splice(index < 0 ? this.childNodes.length : index, 0, node);
      return node;
    },
  };
  for (const node of nodes) root.appendChild(node);
  return root;
}

function runModule(options = {}) {
  const parse = (html) => {
    const match = /^<([a-z0-9-]+)>([\s\S]*)<\/\1>$/.exec(String(html || "").trim());
    return match ? makeNode(match[1], match[2]) : null;
  };
  const document = {
    createElement(tag) {
      assert.equal(tag, "template");
      let html = "";
      const content = {};
      Object.defineProperty(content, "childNodes", {
        get() {
          const node = parse(html);
          return node ? [node] : [];
        },
      });
      return {
        content,
        set innerHTML(value) { html = value; },
      };
    },
  };
  const window = {
    __gosx: { stream: {}, ...(options.gosx || {}) },
    ...(options.runtimeDOM ? { __gosx_runtime_dom: options.runtimeDOM } : {}),
    ...(options.dispose ? { __gosx_dispose_runtime_content: options.dispose } : {}),
    ...(options.mount ? { __gosx_mount_runtime_content: options.mount } : {}),
  };
  const context = { window, document, console };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);
  return context;
}

test("core stream reconciler keeps keyed blocks stable and supports clear", () => {
  const context = runModule();
  const root = makeRoot();
  const stream = context.window.__gosx.stream;
  assert.equal(context.window.GosxProse.reconcileBlocks, stream.reconcileBlocks);
  assert.equal(context.window.__gosx.prose.createBlockStream, stream.createBlockStream);

  const first = stream.reconcileBlocks(root, [
    { key: "a", html: "<p>A</p>" },
    { key: "b", html: "<p>B</p>" },
  ]);
  assert.equal(first.changed, true);
  assert.deepEqual(root.children.map((node) => node.getAttribute("data-gosx-stream-key")), ["a", "b"]);

  const existingA = root.children[0];
  const second = stream.reconcileBlocks(root, [
    { key: "b", html: "<p>B</p>" },
    { key: "a", html: "<p>A</p>" },
  ]);
  assert.equal(second.changed, true);
  assert.equal(root.children[1], existingA);

  const cleared = stream.createBlockStream(root).clear();
  assert.equal(cleared.changed, true);
  assert.equal(root.children.length, 0);
});

test("core stream reconciler accepts a domain signature and HTML updates", () => {
  const context = runModule();
  const root = makeRoot([makeNode("p", "old")]);
  const stream = context.window.__gosx.stream;
  const signatures = [];
  const result = stream.reconcileHTML(root, "<p>new</p>", {
    signature(node) {
      signatures.push(node.textContent);
      return node.tagName;
    },
  });
  assert.equal(result.changed, false);
  assert.equal(signatures.length, 2);
  assert.equal(root.children[0].textContent, "old");
});

test("core stream reconciliation disposes replaced nodes and mounts inserted nodes", () => {
  const disposed = [];
  const mounted = [];
  const context = runModule({
    runtimeDOM: { inTransaction: () => false },
    dispose(node) { disposed.push(node); },
    mount(node) { mounted.push(node); },
  });
  const old = makeNode("p", "old");
  const root = makeRoot([old]);
  const result = context.window.__gosx.stream.reconcileBlocks(root, [
    { key: "next", html: "<p>new</p>" },
  ]);

  assert.equal(result.changed, true);
  assert.deepEqual(disposed, [old]);
  assert.equal(mounted.length, 1);
  assert.equal(root.children[0].textContent, "new");
});

test("core stream reconciliation defers node lifecycle to an outer DOM transaction", () => {
  const calls = [];
  const context = runModule({
    runtimeDOM: { inTransaction: () => true },
    dispose(node) { calls.push(["dispose", node]); },
    mount(node) { calls.push(["mount", node]); },
  });
  const root = makeRoot([makeNode("p", "old")]);

  context.window.__gosx.stream.reconcileHTML(root, "<p>new</p>");
  assert.deepEqual(calls, []);
  assert.equal(root.children[0].textContent, "new");
});
