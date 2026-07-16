import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(path.join(__dirname, "mdpp-diagrams.js"), "utf8");

test("Markdown++ diagrams register as a disposable GoSX surface", () => {
  let registered = null;
  const document = {
    readyState: "complete",
    body: { style: {} },
    addEventListener() {},
    removeEventListener() {},
    querySelectorAll() { return []; },
  };
  const window = {
    __gosx_register_runtime_surface(name, factory) {
      registered = { name, factory };
    },
  };
  const context = { window, document, console, Date, Promise, setTimeout };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  assert.equal(registered.name, "mdpp-diagrams");
  assert.equal(typeof registered.factory, "function");
  assert.equal(typeof window.M31Diagrams.render, "function");
  const root = {
    querySelectorAll() { return []; },
    contains() { return false; },
  };
  const handle = registered.factory({ root, signal: null });
  assert.equal(typeof handle.dispose, "function");
  assert.doesNotThrow(() => handle.dispose());
});

test("Markdown++ diagrams prefer the public runtime-surface registry", () => {
  let publicRegistration = null;
  const document = {
    readyState: "complete",
    body: { style: {} },
    addEventListener() {},
    querySelectorAll() { return []; },
  };
  const window = {
    __gosx: {
      runtimeSurfaceAPI: {
        register(name, factory) {
          publicRegistration = { name, factory };
        },
      },
    },
    __gosx_register_runtime_surface() {
      throw new Error("legacy registry should not win");
    },
  };
  const context = { window, document, console, Date, Promise, setTimeout };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  assert.equal(publicRegistration.name, "mdpp-diagrams");
  assert.equal(typeof publicRegistration.factory, "function");
});

test("diagram enhancement binds generated listeners through the surface DOM scope", async () => {
  let registered = null;
  const bound = [];
  const removed = [];
  const source = { textContent: "graph TD; A-->B" };
  const rendered = {
    setAttribute() {},
    addEventListener() {},
  };
  const figure = {
    dataset: {},
    innerHTML: "",
    querySelector(selector) {
      if (selector === "pre code") return source;
      if (selector === ".mdpp-diagram-rendered") return rendered;
      return null;
    },
    insertAdjacentHTML() {},
  };
  const root = {
    querySelectorAll() { return [figure]; },
    contains() { return true; },
  };
  const document = {
    readyState: "complete",
    body: { style: {} },
    addEventListener() {},
    removeEventListener() {},
    querySelectorAll() { return []; },
  };
  const window = {
    mermaid: {
      render: async () => ({ svg: "<svg></svg>" }),
    },
    __gosx_register_runtime_surface(name, factory) {
      registered = { name, factory };
    },
  };
  const context = { window, document, console, Date, Promise, setTimeout };
  vm.createContext(context);
  vm.runInContext(moduleSrc, context);

  const listen = (target, type, listener, options) => {
    bound.push({ target, type, listener, options });
    return () => removed.push({ target, type, listener });
  };
  registered.factory({ root, signal: null, listen });
  await new Promise((resolve) => setTimeout(resolve, 0));
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.deepEqual(bound.map((entry) => entry.type), ["click", "keydown"]);
  assert.equal(bound.every((entry) => entry.target === rendered), true);
  assert.equal(removed.length, 0);
});
