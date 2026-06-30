// Unit tests for bootstrap-src/07-declarative-regions.js — declarative
// server-fragment regions (data-gosx-region). Runs the module in a node:vm with
// a minimal DOM stub and asserts signal-triggered and hub-event-triggered fetch+swap.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const moduleSrc = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "07-declarative-regions.js"),
  "utf8"
);

const tick = () => new Promise((r) => setTimeout(r, 0));

function makeRegion(attrs) {
  return {
    _attrs: attrs,
    innerHTML: "",
    getAttribute(n) { return n in this._attrs ? this._attrs[n] : null; },
    hasAttribute(n) { return n in this._attrs; },
  };
}

function runModule(regions, payload) {
  const subs = [];
  const hubListeners = [];
  const fetches = [];
  const ctx = {
    console,
    encodeURIComponent,
    fetch: (u, opts) => {
      fetches.push({ u, opts });
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve(payload && payload.json),
        text: () => Promise.resolve(payload && payload.text),
      });
    },
    document: {
      readyState: "complete",
      querySelectorAll: () => regions,
      addEventListener: (type, fn) => { if (type === "gosx:hub:event") hubListeners.push(fn); },
    },
    window: {
      __gosx_subscribe_shared_signal: (name, fn, opts) => { subs.push({ name, fn, opts }); return () => {}; },
    },
  };
  ctx.window.document = ctx.document;
  vm.createContext(ctx);
  vm.runInContext(moduleSrc, ctx);
  return { subs, hubListeners, fetches };
}

test("signal-triggered region fetches {value}-substituted URL and injects the JSON field", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/sel/{value}",
    "data-gosx-region-signal": "$sel",
    "data-gosx-region-field": "html_field",
  });
  const { subs, fetches } = runModule([region], { json: { html_field: "<b>hi</b>" } });
  assert.equal(subs.length, 1);
  assert.equal(subs[0].name, "$sel");
  assert.equal(subs[0].opts.immediate, false);

  subs[0].fn("obj-9");
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].u, "/sel/obj-9");
  await tick();
  await tick();
  assert.equal(region.innerHTML, "<b>hi</b>");
});

test("empty signal value suppresses the {value} fetch", () => {
  const region = makeRegion({
    "data-gosx-region-url": "/sel/{value}",
    "data-gosx-region-signal": "$sel",
    "data-gosx-region-field": "html_field",
  });
  const { subs, fetches } = runModule([region], { json: {} });
  subs[0].fn("");
  assert.equal(fetches.length, 0);
});

test("data-gosx-region-allow-empty fetches with empty {value} substituted", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/tree?selected={value}",
    "data-gosx-region-signal": "$sel",
    "data-gosx-region-allow-empty": "",
    "data-gosx-region-field": "tree_html",
  });
  const { subs, fetches } = runModule([region], { json: { tree_html: "<ul/>" } });
  subs[0].fn(""); // empty selection — must STILL fetch (?selected=)
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].u, "/tree?selected=");
  subs[0].fn("obj-3");
  assert.equal(fetches.length, 2);
  assert.equal(fetches[1].u, "/tree?selected=obj-3");
});

test("hub-event region refetches static URL and injects raw body; ignores other events", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/tree",
    "data-gosx-region-on": "change",
  });
  const { hubListeners, fetches } = runModule([region], { text: "<ul>tree</ul>" });
  assert.equal(hubListeners.length, 1);

  hubListeners[0]({ detail: { event: "other" } });
  assert.equal(fetches.length, 0, "non-matching event must not refetch");

  hubListeners[0]({ detail: { event: "change" } });
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].u, "/tree");
  await tick();
  await tick();
  assert.equal(region.innerHTML, "<ul>tree</ul>");
});
