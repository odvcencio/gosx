// Unit tests for runtime/host/regions.ts — the gosx:region:after event a
// declarative region (data-gosx-region) dispatches on every successful
// swap. 07-declarative-regions.test.mjs already covers the wider fetch and
// swap contract; this file isolates just the rescan-trigger event, whose
// contract fix/region-rescan-countdowns adds a navigation.ts listener for
// (see runtime-30-region-rescan.test.js for that listener's own coverage).
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = [
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "compatibility.ts"), "utf8"),
  fs.readFileSync(path.join(__dirname, "..", "runtime", "host", "regions.ts"), "utf8"),
].join("\n");
// regions.ts calls applySceneCommandScripts(el) unconditionally after every
// swap (the "P6" declarative scene commands pass) — command-bridge.ts is
// where that function lives, so it must load first, exactly as
// 07-declarative-regions.test.mjs's own moduleSrc does.
const scene3dBridgeSrc = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "command-bridge.ts"),
  "utf8"
);

// makeRegion is a trimmed copy of 07-declarative-regions.test.mjs's own
// helper of the same name — just enough element shape (getAttribute/
// setAttribute/hasAttribute/innerHTML/querySelectorAll) for regions.ts's
// bindRegion + fetchRegion to run against.
function makeRegion(attrs) {
  return {
    _attrs: attrs,
    innerHTML: "",
    getAttribute(n) { return n in this._attrs ? this._attrs[n] : null; },
    setAttribute(n, value) { this._attrs[n] = String(value); },
    removeAttribute(n) { delete this._attrs[n]; },
    hasAttribute(n) { return n in this._attrs; },
    querySelectorAll() { return []; },
  };
}

// runModule is a trimmed copy of 07-declarative-regions.test.mjs's own
// helper of the same name, dropped to the pieces this file's assertions
// need: a fetch fake, and a document.dispatchEvent spy.
function runModule(regions, payload) {
  const dispatchedEvents = [];
  const fetches = [];
  const ctx = {
    console,
    CustomEvent: class CustomEvent {
      constructor(type, init) {
        this.type = type;
        this.detail = init && init.detail;
      }
    },
    encodeURIComponent,
    fetch: async (u, fetchOpts) => {
      fetches.push({ u, opts: fetchOpts });
      const resolved = (await (typeof payload === "function" ? payload(u, fetchOpts, fetches.length) : payload)) || {};
      const status = resolved.status != null ? resolved.status : 200;
      const ok = resolved.ok != null ? resolved.ok : (status >= 200 && status < 300);
      const headerMap = resolved.headers || {};
      return {
        ok,
        status,
        headers: { get: (name) => (headerMap[name] !== undefined ? headerMap[name] : null) },
        json: () => Promise.resolve(resolved.json),
        text: () => Promise.resolve(resolved.text),
      };
    },
    document: {
      readyState: "complete",
      activeElement: null,
      hidden: false,
      body: {},
      querySelectorAll: (selector) => (selector === "[data-gosx-region]" ? regions : []),
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: (event) => {
        dispatchedEvents.push(event);
        return true;
      },
      createElement: () => ({}),
      head: { appendChild: (script) => script },
    },
    window: {
      __gosx_subscribe_shared_signal: () => () => {},
      __gosx_emit: () => {},
      __gosx: { engines: new Map() },
    },
  };
  ctx.window.document = ctx.document;
  vm.createContext(ctx);
  vm.runInContext(scene3dBridgeSrc, ctx);
  vm.runInContext(moduleSrc, ctx);
  return { dispatchedEvents, fetches, context: ctx };
}

test("a successful region swap dispatches gosx:region:after with the swapped element and the fetched URL", async () => {
  const region = makeRegion({ "data-gosx-region-url": "/tree" });
  const { context, dispatchedEvents } = runModule([region], { text: "<p>tree</p>" });

  await context.window.__gosx.regions.refresh(region);

  assert.equal(region.innerHTML, "<p>tree</p>", "the swap itself must have happened before the event fires");
  const events = dispatchedEvents.filter((event) => event.type === "gosx:region:after");
  assert.equal(events.length, 1);
  assert.equal(events[0].detail.element, region);
  assert.equal(events[0].detail.url, "/tree");
});

test("gosx:region:after fires again on every subsequent swap, once per swap", async () => {
  const region = makeRegion({ "data-gosx-region-url": "/tree" });
  const { context, dispatchedEvents } = runModule([region], { text: "<p>tree</p>" });

  await context.window.__gosx.regions.refresh(region);
  await context.window.__gosx.regions.refresh(region);

  const events = dispatchedEvents.filter((event) => event.type === "gosx:region:after");
  assert.equal(events.length, 2, "each swap must dispatch its own event, not zero and not a merged one");
});

test("a 304 not-modified response never dispatches gosx:region:after — nothing was actually swapped", async () => {
  const region = makeRegion({ "data-gosx-region-url": "/tree" });
  const { context, dispatchedEvents } = runModule([region], { status: 304 });

  await context.window.__gosx.regions.refresh(region);

  const events = dispatchedEvents.filter((event) => event.type === "gosx:region:after");
  assert.equal(events.length, 0);
});
