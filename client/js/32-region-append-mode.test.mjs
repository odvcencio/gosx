// Unit tests for runtime/host/regions.ts's append/prepend region modes
// (gosx#217): data-gosx-region-mode="append|prepend" inserts a fetched
// fragment beside a region's existing children instead of replacing them,
// data-gosx-region-key dedupes an overlapping fragment, and
// data-gosx-region-cursor fills a "{cursor}" token in the URL from an
// already-present child. Copies makeRegion/runModule from
// 30-region-after-event.test.mjs (see that file's own doc comment) and
// extends the document double with a template-parsing createElement, since
// insertRegionFragment parses a fetched fragment through
// document.createElement("template").
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

function attrOf(tag, name) {
  const m = new RegExp("\\b" + name + "=\"([^\"]*)\"").exec(tag);
  return m ? m[1] : null;
}
// parseTopLevel splits html into top-level element records: {tagName,
// outerHTML, getAttribute}. A child with the same tag name stays inside its
// parent because the depth counter only closes at depth zero.
function parseTopLevel(html) {
  const nodes = [];
  const re = /<\/?([a-zA-Z][\w-]*)[^>]*>/g;
  let depth = 0, start = -1, m;
  while ((m = re.exec(html))) {
    const closing = m[0][1] === "/";
    const selfClosing = m[0].endsWith("/>");
    if (!closing) { if (depth === 0) start = m.index; if (!selfClosing) depth += 1; }
    else depth -= 1;
    if (depth === 0 && start >= 0) {
      const outer = html.slice(start, m.index + m[0].length);
      const open = /<[^>]+>/.exec(outer)[0];
      nodes.push({ tagName: m[1].toUpperCase(), outerHTML: outer, nodeType: 1, getAttribute: (n) => attrOf(open, n) });
      start = -1;
    }
  }
  return nodes;
}

// makeRegion is a trimmed copy of 07-declarative-regions.test.mjs's own
// helper of the same name — just enough element shape (getAttribute/
// setAttribute/hasAttribute/innerHTML/querySelectorAll) for regions.ts's
// bindRegion + fetchRegion to run against, extended with seed/inserted/
// insertAdjacentHTML/childNodes for the append/prepend assertions below.
function makeRegion(attrs) {
  const region = {
    _attrs: attrs, innerHTML: "", children: [], inserted: [],
    getAttribute(n) { return n in this._attrs ? this._attrs[n] : null; },
    setAttribute(n, v) { this._attrs[n] = String(v); },
    removeAttribute(n) { delete this._attrs[n]; },
    hasAttribute(n) { return n in this._attrs; },
    seed(html) { this.children = parseTopLevel(html); },
    // Answers "[data-tape-key]" and "[data-pick-number]" alike: any
    // attribute selector returns the children that carry that attribute.
    querySelectorAll(selector) { const name = /^\[([^\]]+)\]$/.exec(selector); return name ? this.children.filter((c) => c.getAttribute(name[1]) != null) : []; },
    get childNodes() { return this.children; },
    insertAdjacentHTML(position, html) {
      this.inserted.push({ position, html });
      const fresh = parseTopLevel(html);
      this.children = position === "afterbegin" ? fresh.concat(this.children) : this.children.concat(fresh);
    },
  };
  return region;
}

// runModule is a trimmed copy of 07-declarative-regions.test.mjs's own
// helper of the same name, dropped to the pieces this file's assertions
// need: a fetch fake, and a document.dispatchEvent spy. document.createElement
// answers a template double for "template" (so insertRegionFragment's own
// document.createElement("template") + .innerHTML parse works) and an inert
// object for anything else, matching 30-region-after-event.test.mjs's own
// stub for every other tag.
function runModule(regions, payload, opts) {
  opts = opts || {};
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
      createElement: (tag) => tag === "template" ? { set innerHTML(v) { this.content = { childNodes: parseTopLevel(v) }; } } : ({}),
      head: { appendChild: (script) => script },
    },
    window: {
      __gosx_subscribe_shared_signal: () => () => {},
      __gosx_emit: () => {},
      __gosx: { engines: new Map() },
      ...(opts.replaceRuntimeContent ? {
        __gosx_replace_runtime_content: opts.replaceRuntimeContent,
      } : {}),
    },
  };
  ctx.window.document = ctx.document;
  vm.createContext(ctx);
  vm.runInContext(scene3dBridgeSrc, ctx);
  vm.runInContext(moduleSrc, ctx);
  return { dispatchedEvents, fetches, context: ctx };
}

test("prepend mode inserts the fragment before existing children and keeps them", async () => {
  const region = makeRegion({ "data-gosx-region-url": "/tape?since={cursor}", "data-gosx-region-mode": "prepend", "data-gosx-region-key": "data-tape-key", "data-gosx-region-cursor": "data-pick-number" });
  region.seed('<div data-tape-key="pick-19" data-pick-number="19"></div>');
  const { context, fetches } = runModule([region], { text: '<div data-tape-key="pick-20" data-pick-number="20"></div>' });
  await context.window.__gosx.regions.refresh(region);
  assert.equal(fetches[0].u, "/tape?since=19", "the cursor fills from the first keyed child");
  assert.deepEqual(region.inserted.map((i) => i.position), ["afterbegin"]);
  assert.equal(region.children.length, 2);
  assert.equal(region.innerHTML, "", "prepend mode never rewrites innerHTML");
});

test("a keyed node already present is skipped and a nested same-tag child stays inside its parent", async () => {
  const region = makeRegion({ "data-gosx-region-url": "/tape", "data-gosx-region-mode": "prepend", "data-gosx-region-key": "data-tape-key" });
  region.seed('<div data-tape-key="pick-19"></div>');
  const { context, dispatchedEvents } = runModule([region], { text: '<div data-tape-key="round-3"><div class="idx">ROUND 3</div></div><div data-tape-key="pick-19"></div><div data-tape-key="pick-20"></div>' });
  await context.window.__gosx.regions.refresh(region);
  assert.equal(region.children.length, 3, "header and pick-20 inserted, pick-19 skipped");
  assert.ok(region.inserted[0].html.indexOf('class="idx"') >= 0, "the header keeps its nested div");
  assert.equal(dispatchedEvents.filter((e) => e.type === "gosx:region:after").length, 1);
});

test("a fully deduplicated fragment inserts nothing and emits no gosx:region:after", async () => {
  const region = makeRegion({ "data-gosx-region-url": "/tape", "data-gosx-region-mode": "prepend", "data-gosx-region-key": "data-tape-key" });
  region.seed('<div data-tape-key="pick-19"></div>');
  const { context, dispatchedEvents } = runModule([region], { text: '<div data-tape-key="pick-19"></div>' });
  await context.window.__gosx.regions.refresh(region);
  assert.equal(region.inserted.length, 0);
  assert.equal(dispatchedEvents.filter((e) => e.type === "gosx:region:after").length, 0);
});

test("append mode inserts at the end", async () => {
  const region = makeRegion({ "data-gosx-region-url": "/feed", "data-gosx-region-mode": "append" });
  const { context } = runModule([region], { text: "<p>new</p>" });
  await context.window.__gosx.regions.refresh(region);
  assert.deepEqual(region.inserted.map((i) => i.position), ["beforeend"]);
});
