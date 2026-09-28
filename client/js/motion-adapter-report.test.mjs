// The motion docs demo reports which renderer and GPU adapter its Scene3D
// runs on. An earlier version lived in the core bootstrap, observed the whole
// document with childList, and rewrote the report's textContent on every
// callback. A textContent set always queues a childList record, so the
// observer re-triggered itself in an endless microtask chain once the scene
// published its renderer truth, and Chrome froze the page (PR #390).
//
// These tests run the report under a MutationObserver model that follows the
// DOM rules that matter here: every textContent set queues a childList record
// (even when the text is unchanged), every setAttribute queues an attributes
// record, and records queued during a callback are delivered in a new
// microtask. Delivery stops at a cap so a loop fails the test instead of
// hanging it.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const reportScript = fs.readFileSync(path.join(repoRoot, "examples/gosx-docs/public/motion-adapter-report.js"), "utf8");

const DELIVERY_CAP = 64;

function createObservedDocument() {
  const observers = [];
  const listeners = new Map();
  let deliveries = 0;

  function queueRecord(record) {
    for (const observer of observers) {
      if (!observer.root || !observer.root.contains(record.target)) continue;
      const options = observer.options;
      if (record.type === "childList" && !options.childList) continue;
      if (record.type === "attributes") {
        if (!options.attributes && !options.attributeFilter) continue;
        if (options.attributeFilter && !options.attributeFilter.includes(record.attributeName)) continue;
      }
      observer.records.push(record);
      if (observer.scheduled) continue;
      observer.scheduled = true;
      queueMicrotask(() => {
        observer.scheduled = false;
        const records = observer.records;
        observer.records = [];
        if (!observer.root || deliveries >= DELIVERY_CAP) return;
        deliveries++;
        observer.callback(records, observer);
      });
    }
  }

  function createElement(attributes, text) {
    const element = {
      nodeType: 1,
      parent: null,
      children: [],
      attributes: new Map(Object.entries(attributes || {})),
      text: text || "",
      get textContent() { return this.text; },
      set textContent(value) {
        this.text = String(value);
        queueRecord({ type: "childList", target: element });
      },
      hasAttribute(name) { return this.attributes.has(name); },
      getAttribute(name) { return this.attributes.has(name) ? this.attributes.get(name) : null; },
      setAttribute(name, value) {
        this.attributes.set(name, String(value));
        queueRecord({ type: "attributes", target: element, attributeName: name });
      },
      matches(selector) {
        const attr = /^\[([\w-]+)\]$/.exec(selector);
        if (attr) return this.hasAttribute(attr[1]);
        if (selector.startsWith("#")) return this.getAttribute("id") === selector.slice(1);
        return false;
      },
      querySelectorAll(selector) {
        const out = [];
        const visit = (node) => { for (const child of node.children) { if (child.matches(selector)) out.push(child); visit(child); } };
        visit(this);
        return out;
      },
      querySelector(selector) { return this.querySelectorAll(selector)[0] || null; },
      contains(node) {
        for (let current = node; current; current = current.parent) if (current === this) return true;
        return false;
      },
      appendChild(child) { child.parent = this; this.children.push(child); return child; },
    };
    return element;
  }

  const root = createElement({}, "");
  const scene = root.appendChild(createElement({ id: "motion-scene" }, ""));
  const report = root.appendChild(createElement({ "data-gosx-motion-adapter-report": "#motion-scene" }, "Waiting for the scene adapter"));

  class MutationObserver {
    constructor(callback) { this.callback = callback; this.root = null; this.options = {}; this.records = []; this.scheduled = false; observers.push(this); }
    observe(target, options) { this.root = target; this.options = options || {}; }
    disconnect() { this.root = null; this.records = []; }
    takeRecords() { const records = this.records; this.records = []; return records; }
  }

  const document = {
    documentElement: root,
    body: root,
    readyState: "complete",
    querySelector(selector) { return root.querySelector(selector); },
    querySelectorAll(selector) { return root.querySelectorAll(selector); },
    addEventListener(name, callback) {
      const set = listeners.get(name) || new Set();
      set.add(callback);
      listeners.set(name, set);
    },
  };
  const window = {};
  const context = vm.createContext({ window, document, MutationObserver, JSON, console });
  window.window = window;

  return {
    context,
    scene,
    report,
    deliveries: () => deliveries,
    run(source, filename) { vm.runInContext(source, context, { filename }); },
    // Scene3D publishes its truth the way sceneRenderBackendTruth does: the
    // attribute first, then the expando, in the same task.
    publishTruth(truth) {
      scene.setAttribute("data-gosx-scene3d-render-backend-truth", JSON.stringify(truth));
      scene.__gosxScene3DRenderBackendTruth = truth;
    },
  };
}

async function drainMicrotasks() {
  for (let i = 0; i < 4; i++) await new Promise((resolve) => setTimeout(resolve, 0));
}

const nvidiaTruth = { backend: "webgpu", gpu: true, adapterInfo: { vendor: "nvidia", architecture: "blackwell", device: "", description: "" } };

test("the motion adapter report settles after the scene publishes its renderer truth", async () => {
  const page = createObservedDocument();
  page.run(reportScript, "motion-adapter-report.js");
  await drainMicrotasks();
  assert.equal(page.report.textContent, "Waiting for the scene adapter", "no truth yet, so the report keeps its placeholder");

  const before = page.deliveries();
  page.publishTruth(nvidiaTruth);
  await drainMicrotasks();

  const settled = page.deliveries() - before;
  assert.ok(settled < DELIVERY_CAP, `the report observer re-triggered itself ${settled} times`);
  assert.ok(settled <= 2, `expected the report to settle within 2 observer deliveries, got ${settled}`);
  assert.equal(page.report.textContent, "webgpu adapter: nvidia blackwell");
  assert.equal(page.report.getAttribute("data-gosx-motion-renderer"), "webgpu");
  assert.equal(page.report.getAttribute("data-gosx-motion-adapter"), "nvidia blackwell");

  // A backend swap (for example WebGPU device loss to WebGL) updates the report.
  page.publishTruth({ backend: "webgl", gpu: true, adapterInfo: { vendor: "nvidia", device: "RTX 5070 Ti" } });
  await drainMicrotasks();
  assert.equal(page.report.textContent, "webgl adapter: nvidia RTX 5070 Ti");
  assert.ok(page.deliveries() - before < 6, "a backend swap should settle too");
});

test("the observer model catches a report that rewrites itself inside a childList observer", async () => {
  // The pre-fix pattern from PR #390, kept here so the model above is known to
  // detect the loop rather than pass by accident.
  const page = createObservedDocument();
  page.run(`
    (function () {
      function update() {
        for (const report of document.querySelectorAll("[data-gosx-motion-adapter-report]")) {
          const scene = document.querySelector(report.getAttribute("data-gosx-motion-adapter-report"));
          const truth = scene && scene.__gosxScene3DRenderBackendTruth;
          if (!truth) continue;
          report.textContent = truth.backend + " adapter";
        }
      }
      new MutationObserver(update).observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ["data-gosx-scene3d-render-backend-truth"] });
    })();
  `, "pre-fix-report.js");
  page.publishTruth(nvidiaTruth);
  await drainMicrotasks();
  assert.equal(page.deliveries(), DELIVERY_CAP, "the model must reproduce the endless microtask chain");
});

test("the adapter report is demo code and stays out of the core bootstrap", () => {
  const core = fs.readFileSync(path.join(repoRoot, "client/js/bootstrap-src/06-motion-core.ts"), "utf8");
  assert.ok(!core.includes("data-gosx-motion-adapter-report"), "06-motion-core.ts must not carry the docs adapter report");
  for (const bundle of ["bootstrap.js", "bootstrap-runtime.js", "bootstrap-lite.js"]) {
    const source = fs.readFileSync(path.join(repoRoot, "client/js", bundle), "utf8");
    assert.ok(!source.includes("data-gosx-motion-adapter-report"), `${bundle} must not carry the docs adapter report`);
  }
});
