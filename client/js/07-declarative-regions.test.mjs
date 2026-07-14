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

const SCENE_COMMANDS_SELECTOR = 'script[type="application/json"][data-gosx-scene-commands]';

// makeCommandScript builds a fake <script type="application/json"
// data-gosx-scene-commands> node — just enough shape (.textContent) for
// applySceneCommandScripts's querySelectorAll(...) + textContent read.
function makeCommandScript(textContent) {
  return { textContent };
}

// makeRegion optionally accepts `commandScripts` (an array of
// makeCommandScript(...) nodes) so a test can simulate the swapped-in
// fragment containing data-gosx-scene-commands payloads: after
// `el.innerHTML = html`, 07 calls el.querySelectorAll(SCENE_COMMANDS_SELECTOR).
function makeRegion(attrs, commandScripts) {
  return {
    _attrs: attrs,
    innerHTML: "",
    getAttribute(n) { return n in this._attrs ? this._attrs[n] : null; },
    hasAttribute(n) { return n in this._attrs; },
    querySelectorAll(selector) {
      return selector === SCENE_COMMANDS_SELECTOR ? (commandScripts || []) : [];
    },
  };
}

// runModule's `document.querySelectorAll` dispatches on the selector so the
// module's two independent scans — `[data-gosx-region]` (bindRegion) and
// SCENE_COMMANDS_SELECTOR (the initial-load pass) — each see the right fake
// nodes. `engines` is a real Map (engineID -> {component, handle}), mirroring
// window.__gosx.engines exactly, so sceneCommandEngineHandles()'s
// `.forEach` works unmodified.
function runModule(regions, payload, opts) {
  opts = opts || {};
  const subs = [];
  const hubListeners = [];
  const readyListeners = [];
  const fetches = [];
  const warnings = [];
  const telemetry = [];
  const engines = opts.engines || new Map();
  const initialCommandScripts = opts.initialCommandScripts || [];
  const ctx = {
    console: { ...console, warn: (...args) => warnings.push(args) },
    encodeURIComponent,
    fetch: (u, fetchOpts) => {
      fetches.push({ u, opts: fetchOpts });
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve(payload && payload.json),
        text: () => Promise.resolve(payload && payload.text),
      });
    },
    document: {
      readyState: "complete",
      querySelectorAll: (selector) => {
        if (selector === "[data-gosx-region]") return regions;
        if (selector === SCENE_COMMANDS_SELECTOR) return initialCommandScripts;
        return [];
      },
      addEventListener: (type, fn) => {
        if (type === "gosx:hub:event") hubListeners.push(fn);
        if (type === "gosx:ready") readyListeners.push(fn);
      },
      removeEventListener: () => {},
    },
    window: {
      __gosx_subscribe_shared_signal: (name, fn, opts) => { subs.push({ name, fn, opts }); return () => {}; },
      __gosx_emit: (level, category, message, fields) => telemetry.push({ level, category, message, fields }),
      __gosx: {
        engines,
        ...(opts.transport ? { transport: opts.transport } : {}),
      },
      ...(opts.replaceRuntimeContent ? {
        __gosx_replace_runtime_content: opts.replaceRuntimeContent,
      } : {}),
    },
  };
  ctx.window.document = ctx.document;
  vm.createContext(ctx);
  vm.runInContext(moduleSrc, ctx);
  return { subs, hubListeners, readyListeners, fetches, warnings, telemetry, engines, context: ctx };
}

// makeEngineHandle returns a fake mounted-engine record + its handle's
// applyCommands call log, in the exact {component, handle} shape
// window.__gosx.engines stores (30-tail.js: rememberMountedEngine).
function makeEngineHandle(component) {
  const calls = [];
  const handle = { applyCommands: (commands) => calls.push(commands) };
  return { record: { component: component === undefined ? "GoSXScene3D" : component, handle }, calls };
}

// asJSON round-trips through the test realm's own JSON so assert.deepEqual
// never has to compare an array/object parsed inside the module's separate
// vm context (a different realm — deepStrictEqual treats same-shaped
// cross-realm values as NOT reference-equal) against a plain literal here.
function asJSON(value) {
  return JSON.parse(JSON.stringify(value));
}

test("signal-triggered region fetches {value}-substituted URL and injects the JSON field", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/sel/{value}",
    "data-gosx-region-signal": "$sel",
    "data-gosx-region-field": "html_field",
  });
  const { subs, fetches, telemetry } = runModule([region], { json: { html_field: "<b>hi</b>" } });
  assert.equal(subs.length, 1);
  assert.equal(subs[0].name, "$sel");
  assert.equal(subs[0].opts.immediate, false);

  subs[0].fn("obj-9");
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].u, "/sel/obj-9");
  await tick();
  await tick();
  assert.equal(region.innerHTML, "<b>hi</b>");
  assert.equal(telemetry.some((event) => event.message === "region refresh started"), true);
  assert.equal(telemetry.some((event) => event.message === "region refresh completed"), true);
});

test("declarative regions publish a core refresh and lifecycle API", async () => {
  const region = makeRegion({ "data-gosx-region-url": "/tree" });
  const { context, fetches } = runModule([region], { text: "<p>tree</p>" });
  assert.equal(typeof context.window.__gosx.regions.mount, "function");
  assert.equal(typeof context.window.__gosx.regions.dispose, "function");
  assert.equal(typeof context.window.__gosx.regions.refresh, "function");
  assert.equal(context.window.__gosx.regions.bindings, context.window.__gosx_declarative_regions.bindings);
  await context.window.__gosx.regions.refresh(region);
  assert.equal(fetches.length, 1);
  assert.equal(region.innerHTML, "<p>tree</p>");
});

test("declarative regions delegate latest-request cancellation to the core transport scope", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/tree",
    "data-gosx-region-on": "change",
  });
  const requests = [];
  let disposed = 0;
  const transport = {
    scope() {
      return {
        requestLatest(key, url, init) {
          requests.push({ key, url, init });
          return Promise.resolve({ ok: true, text: () => Promise.resolve("<p>core transport</p>") });
        },
        dispose() {
          disposed += 1;
        },
      };
    },
  };
  const { context, hubListeners, fetches } = runModule([region], { text: "<p>fallback</p>" }, { transport });
  hubListeners[0]({ detail: { event: "change" } });
  await tick();
  await tick();

  assert.equal(fetches.length, 0, "core transport should replace the direct fetch path");
  assert.equal(requests.length, 1);
  assert.equal(requests[0].key, "refresh");
  assert.equal(requests[0].url, "/tree");
  assert.equal(region.innerHTML, "<p>core transport</p>");

  context.window.__gosx_dispose_declarative_regions(context.document);
  assert.equal(disposed, 1, "region disposal must dispose its transport scope");
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

test("regions delegate fragment replacement to the core runtime DOM lifecycle", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/tree",
    "data-gosx-region-on": "change",
  });
  const replacements = [];
  const { hubListeners } = runModule([region], { text: "<ul>tree</ul>" }, {
    replaceRuntimeContent(target, html) {
      replacements.push({ target, html });
      target.innerHTML = html;
      return true;
    },
  });
  hubListeners[0]({ detail: { event: "change" } });
  await tick();
  await tick();
  assert.deepEqual(replacements, [{ target: region, html: "<ul>tree</ul>" }]);
  assert.equal(region.innerHTML, "<ul>tree</ul>");
});

test("regions remount on a new page root and dispose their signal bindings", () => {
  const first = makeRegion({
    "data-gosx-region-url": "/first",
    "data-gosx-region-signal": "$page",
  });
  const second = makeRegion({
    "data-gosx-region-url": "/second",
    "data-gosx-region-signal": "$page",
  });
  const regions = [first];
  const { context, subs, fetches } = runModule(regions, { text: "<p>ok</p>" });
  assert.equal(subs.length, 1);

  regions.push(second);
  context.window.__gosx_mount_declarative_regions(context.document);
  assert.equal(subs.length, 2, "the second navigation root must bind exactly once");

  context.window.__gosx_dispose_declarative_regions(context.document);
  subs[0].fn("stale");
  subs[1].fn("stale");
  assert.equal(fetches.length, 0, "disposed regions must ignore late signal callbacks");
});

// -----------------------------------------------------------------------
// P6: declarative scene commands (data-gosx-scene-commands)
// -----------------------------------------------------------------------

test("region swap applies a data-gosx-scene-commands payload to every mounted GoSXScene3D engine", async () => {
  const commands = [{ kind: 0, objectId: "ws-comment-1", data: { kind: "label", props: { text: "hi" } } }];
  const region = makeRegion(
    { "data-gosx-region-url": "/tree", "data-gosx-region-on": "change" },
    [makeCommandScript(JSON.stringify(commands))],
  );
  const engineA = makeEngineHandle();
  const engineB = makeEngineHandle();
  const engines = new Map([["engine-a", engineA.record], ["engine-b", engineB.record]]);

  const { hubListeners } = runModule([region], { text: "<ul>tree</ul>" }, { engines });
  hubListeners[0]({ detail: { event: "change" } });
  await tick();
  await tick();

  assert.equal(region.innerHTML, "<ul>tree</ul>");
  assert.deepEqual(asJSON(engineA.calls), [commands], "every mounted GoSXScene3D engine must receive the commands");
  assert.deepEqual(asJSON(engineB.calls), [commands]);
});

test("region swap ignores engines that are not GoSXScene3D and engines without applyCommands", async () => {
  const commands = [{ kind: 0, objectId: "x", data: { kind: "label", props: { text: "hi" } } }];
  const region = makeRegion(
    { "data-gosx-region-url": "/tree", "data-gosx-region-on": "change" },
    [makeCommandScript(JSON.stringify(commands))],
  );
  const scene3d = makeEngineHandle();
  const otherComponent = makeEngineHandle("SomeOtherEngine");
  const noHandle = { component: "GoSXScene3D", handle: null };
  const engines = new Map([
    ["scene3d", scene3d.record],
    ["other", otherComponent.record],
    ["no-handle", noHandle],
  ]);

  const { hubListeners } = runModule([region], { text: "<ul>tree</ul>" }, { engines });
  hubListeners[0]({ detail: { event: "change" } });
  await tick();
  await tick();

  assert.deepEqual(asJSON(scene3d.calls), [commands]);
  assert.deepEqual(asJSON(otherComponent.calls), [], "non-Scene3D engines must not receive scene commands");
});

test("malformed data-gosx-scene-commands JSON warns and is skipped, never throws", async () => {
  const region = makeRegion(
    { "data-gosx-region-url": "/tree", "data-gosx-region-on": "change" },
    [makeCommandScript("{not valid json")],
  );
  const engine = makeEngineHandle();
  const engines = new Map([["engine", engine.record]]);

  const { hubListeners, warnings } = runModule([region], { text: "<ul>tree</ul>" }, { engines });
  assert.doesNotThrow(() => {
    hubListeners[0]({ detail: { event: "change" } });
  });
  await tick();
  await tick();

  assert.equal(region.innerHTML, "<ul>tree</ul>", "the region swap itself must still complete");
  assert.equal(engine.calls.length, 0, "malformed payload must not reach applyCommands");
  assert.equal(warnings.length, 1, "malformed JSON must warn exactly once");
  assert.match(String(warnings[0][0]), /scene command payload parse failed/);
});

test("a non-array data-gosx-scene-commands payload is silently skipped (no warn, no apply)", async () => {
  const region = makeRegion(
    { "data-gosx-region-url": "/tree", "data-gosx-region-on": "change" },
    [makeCommandScript(JSON.stringify({ not: "an array" }))],
  );
  const engine = makeEngineHandle();
  const engines = new Map([["engine", engine.record]]);

  const { hubListeners, warnings } = runModule([region], { text: "<ul>tree</ul>" }, { engines });
  hubListeners[0]({ detail: { event: "change" } });
  await tick();
  await tick();

  assert.equal(engine.calls.length, 0);
  assert.equal(warnings.length, 0, "a well-formed-but-non-array payload is not malformed JSON, so no warn");
});

test("multiple data-gosx-scene-commands tags in one swapped fragment are each applied, in document order", async () => {
  const first = [{ kind: 0, objectId: "a", data: { kind: "label", props: { text: "a" } } }];
  const second = [{ kind: 0, objectId: "b", data: { kind: "label", props: { text: "b" } } }];
  const region = makeRegion(
    { "data-gosx-region-url": "/tree", "data-gosx-region-on": "change" },
    [makeCommandScript(JSON.stringify(first)), makeCommandScript(JSON.stringify(second))],
  );
  const engine = makeEngineHandle();
  const engines = new Map([["engine", engine.record]]);

  const { hubListeners } = runModule([region], { text: "<ul>tree</ul>" }, { engines });
  hubListeners[0]({ detail: { event: "change" } });
  await tick();
  await tick();

  assert.deepEqual(asJSON(engine.calls), [first, second]);
});

test("initial-load data-gosx-scene-commands payloads apply once at scan time and again on gosx:ready", () => {
  const commands = [{ kind: 0, objectId: "ssr-pin", data: { kind: "label", props: { text: "ssr" } } }];
  const engine = makeEngineHandle();
  const engines = new Map(); // no engine mounted yet at synchronous scan time

  const { readyListeners } = runModule([], {}, {
    initialCommandScripts: [makeCommandScript(JSON.stringify(commands))],
    engines,
  });

  // Synchronous scan ran with zero mounted engines — a no-op, not an error.
  assert.equal(engine.calls.length, 0);

  // The engine finishes mounting asynchronously; the runtime dispatches
  // gosx:ready once every manifest engine is up.
  engines.set("engine", engine.record);
  assert.equal(readyListeners.length, 1, "07 must listen for gosx:ready exactly once");
  readyListeners[0]();

  assert.deepEqual(asJSON(engine.calls), [commands], "the SSR-rendered payload must reach the now-mounted engine");
});

test("initial-load scan applies immediately when the engine is already mounted (no swap needed)", () => {
  const commands = [{ kind: 0, objectId: "ssr-pin", data: { kind: "label", props: { text: "ssr" } } }];
  const engine = makeEngineHandle();
  const engines = new Map([["engine", engine.record]]);

  runModule([], {}, {
    initialCommandScripts: [makeCommandScript(JSON.stringify(commands))],
    engines,
  });

  assert.deepEqual(asJSON(engine.calls), [commands]);
});
