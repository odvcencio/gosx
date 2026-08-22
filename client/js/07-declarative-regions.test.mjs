// Unit tests for runtime/host/regions.ts — declarative
// server-fragment regions (data-gosx-region). Runs the module in a node:vm with
// a minimal DOM stub and asserts signal-triggered and hub-event-triggered fetch+swap.
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
const scene3dBridgeSrc = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "command-bridge.ts"),
  "utf8"
);
const scene3dCommandRuntimeSrc = fs.readFileSync(
  path.join(__dirname, "..", "runtime", "scene3d", "command-runtime.ts"),
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
    setAttribute(n, value) { this._attrs[n] = String(value); },
    removeAttribute(n) { delete this._attrs[n]; },
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
// installManualTimers gives a test control over regions.ts's own
// setInterval(...) calls (gosx#217's periodic region polling) instead of a
// real background timer, the same manual-timer control
// client/js/runtime-test-harness.js's installManualTimers gives the
// navigation-runtime test suite. Always installed — a test that never
// polls simply never calls run(), and the returned handle map stays empty.
function installManualTimers(ctx) {
  let nextHandle = 1;
  const intervals = new Map();
  ctx.setInterval = (cb, delay) => {
    const handle = nextHandle++;
    intervals.set(handle, { cb, delay: Number(delay || 0) });
    return handle;
  };
  ctx.clearInterval = (handle) => { intervals.delete(handle); };
  return {
    count: () => intervals.size,
    run: (delay) => {
      const targetDelay = Number(delay || 0);
      const entries = Array.from(intervals.entries()).filter(([, timer]) => timer.delay === targetDelay);
      for (const [handle, timer] of entries) {
        if (intervals.has(handle)) timer.cb();
      }
      return entries.length;
    },
  };
}

function runModule(regions, payload, opts) {
  opts = opts || {};
  const subs = [];
  const hubListeners = [];
  const readyListeners = [];
  const pointerListeners = { pointerdown: [], pointerup: [], pointercancel: [] };
  const fetches = [];
  const warnings = [];
  const telemetry = [];
  const dispatchedEvents = [];
  const engines = opts.engines || new Map();
  const initialCommandScripts = opts.initialCommandScripts || [];
  const ctx = {
    console: { ...console, warn: (...args) => warnings.push(args) },
    CustomEvent: class CustomEvent {
      constructor(type, init) {
        this.type = type;
        this.detail = init && init.detail;
      }
    },
    encodeURIComponent,
    fetch: async (u, fetchOpts) => {
      fetches.push({ u, opts: fetchOpts });
      // `payload` is either the flat {json, text} shape every pre-gosx#217
      // test still passes, a function of (url, fetchOpts, callIndex) a
      // gosx#217 test uses to vary status/headers/body across repeated
      // polls (an ETag round-trip, a 304, a changing body), or — awaited
      // here — a Promise that function returns to hold a fetch open (the
      // "never overlapping" in-flight test below).
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
      hidden: !!opts.hidden,
      body: {},
      querySelectorAll: (selector) => {
        if (selector === "[data-gosx-region]") return regions;
        if (selector === SCENE_COMMANDS_SELECTOR) return initialCommandScripts;
        return [];
      },
      addEventListener: (type, fn) => {
        if (type === "gosx:hub:event") hubListeners.push(fn);
        if (type === "gosx:ready") readyListeners.push(fn);
        if (pointerListeners[type]) pointerListeners[type].push(fn);
      },
      removeEventListener: () => {},
      dispatchEvent: (event) => {
        dispatchedEvents.push(event);
        return true;
      },
      createElement: (tagName) => {
        const tag = String(tagName || "").toLowerCase();
        if (tag !== "script") return {};
        return {
          async: false,
          onload: null,
          onerror: null,
          src: "",
        };
      },
      head: {
        appendChild: (script) => {
          try {
            if (!script || script.src !== "/gosx/bootstrap-feature-scene3d-command.js") {
              throw new Error("script not found: " + (script && script.src));
            }
            vm.runInContext(scene3dCommandRuntimeSrc, ctx);
            if (typeof script.onload === "function") script.onload({});
          } catch (err) {
            if (script && typeof script.onerror === "function") script.onerror(err);
          }
          return script;
        },
      },
    },
    window: {
      __gosx_subscribe_shared_signal: (name, fn, opts) => { subs.push({ name, fn, opts }); return () => {}; },
      __gosx_emit: (level, category, message, fields) => telemetry.push({ level, category, message, fields }),
      __gosx: {
        engines,
        ...(opts.transport ? { transport: opts.transport } : {}),
        ...(opts.navigation ? { navigation: opts.navigation } : {}),
      },
      ...(opts.replaceRuntimeContent ? {
        __gosx_replace_runtime_content: opts.replaceRuntimeContent,
      } : {}),
    },
  };
  ctx.window.document = ctx.document;
  const timers = installManualTimers(ctx);
  vm.createContext(ctx);
  vm.runInContext(scene3dBridgeSrc, ctx);
  vm.runInContext(moduleSrc, ctx);
  const firePointer = (type, target) => {
    for (const fn of pointerListeners[type] || []) fn({ target });
  };
  return { subs, hubListeners, readyListeners, fetches, warnings, telemetry, dispatchedEvents, engines, timers, firePointer, context: ctx };
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

test("4xx and 5xx region responses retain SSR DOM, expose sanitized error state, and recover", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/tree",
    "data-gosx-region-on": "change",
  });
  region.innerHTML = "<p>server truth</p>";
  let bodyReads = 0;
  const responses = [
    {
      status: 404,
      ok: false,
      get text() {
        bodyReads += 1;
        return "private not-found body";
      },
    },
    {
      status: 503,
      ok: false,
      get text() {
        bodyReads += 1;
        return "private upstream body";
      },
    },
    {
      status: 200,
      ok: true,
      get text() {
        bodyReads += 1;
        return "<p>recovered</p>";
      },
    },
  ];
  const { hubListeners, dispatchedEvents, telemetry } = runModule(
    [region],
    () => responses.shift(),
  );

  assert.equal(region.getAttribute("data-gosx-region-state"), "ready");
  assert.equal(region.getAttribute("aria-busy"), null);
  for (const status of [404, 503]) {
    hubListeners[0]({ detail: { event: "change" } });
    assert.equal(region.getAttribute("data-gosx-region-state"), "pending");
    assert.equal(region.getAttribute("aria-busy"), "true");
    assert.equal(region.innerHTML, "<p>server truth</p>");
    await tick();
    await tick();

    assert.equal(region.getAttribute("data-gosx-region-state"), "error");
    assert.equal(region.getAttribute("aria-busy"), null);
    assert.equal(region.getAttribute("data-gosx-region-request"), null);
    assert.equal(region.innerHTML, "<p>server truth</p>");
    const errors = dispatchedEvents.filter((event) => event.type === "gosx:region:error");
    const detail = errors[errors.length - 1].detail;
    assert.deepEqual(Object.keys(detail).sort(), ["element", "status", "url"]);
    assert.equal(detail.status, status);
    assert.equal(detail.url, "/tree");
    assert.doesNotMatch(JSON.stringify(detail), /private not-found|private upstream/);
  }
  assert.equal(bodyReads, 0, "non-2xx bodies must never be read");
  assert.deepEqual(
    telemetry.filter((event) => event.message === "region refresh rejected").map((event) => event.fields.status),
    [404, 503],
  );

  hubListeners[0]({ detail: { event: "change" } });
  assert.equal(region.getAttribute("aria-busy"), "true");
  await tick();
  await tick();
  assert.equal(region.innerHTML, "<p>recovered</p>");
  assert.equal(region.getAttribute("data-gosx-region-state"), "ready");
  assert.equal(region.getAttribute("aria-busy"), null);
  assert.equal(bodyReads, 1, "only the successful response body is consumed");
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

test("declarative scene command broadcasts keep legacy mounted handles separate from public target readiness", async () => {
  const commands = [{ kind: 0, objectId: "legacy-pin", data: { kind: "label", props: { text: "legacy" } } }];
  const engine = makeEngineHandle();
  const engines = new Map([["legacy-engine", engine.record]]);
  const root = makeRegion({}, [makeCommandScript(JSON.stringify(commands))]);

  const { context } = runModule([], {}, { engines });
  assert.equal(engine.record.handle.__gosxScene3DCommandReady, undefined);

  await context.window.__gosx_apply_scene_command_scripts(root);
  assert.deepEqual(asJSON(engine.calls), [commands]);

  await assert.rejects(
    context.window.__gosx.scene3d.dispatchCommands(engine.record.handle, commands),
    /not ready and has no stable id/,
    "the public API still requires a ready target or a stable target id",
  );
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

test("initial-load data-gosx-scene-commands payloads apply once at scan time and again on gosx:ready", async () => {
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
  await tick();

  assert.deepEqual(asJSON(engine.calls), [commands], "the SSR-rendered payload must reach the now-mounted engine");
});

test("initial-load scan applies immediately when the engine is already mounted (no swap needed)", async () => {
  const commands = [{ kind: 0, objectId: "ssr-pin", data: { kind: "label", props: { text: "ssr" } } }];
  const engine = makeEngineHandle();
  const engines = new Map([["engine", engine.record]]);

  runModule([], {}, {
    initialCommandScripts: [makeCommandScript(JSON.stringify(commands))],
    engines,
  });
  await tick();

  assert.deepEqual(asJSON(engine.calls), [commands]);
});

// -----------------------------------------------------------------------
// gosx#217: periodic region polling (data-gosx-region-interval), the
// interaction guard it alone observes, and the ETag/scroll behavior every
// trigger kind now shares.
// -----------------------------------------------------------------------

test("data-gosx-region-interval fetches immediately at bind time and again on its own interval", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-interval": "20s",
  });
  const { fetches, timers } = runModule([region], { text: "<ul>wire</ul>" });
  await tick();
  await tick();

  assert.equal(fetches.length, 1, "the immediate first tick fetches without waiting a full interval");
  assert.equal(region.innerHTML, "<ul>wire</ul>");
  assert.equal(timers.count(), 1);

  timers.run(20000);
  await tick();
  await tick();
  assert.equal(fetches.length, 2);
});

test("an invalid data-gosx-region-interval warns once and disables only the interval trigger", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-signal": "$wire",
    "data-gosx-region-interval": "not-a-duration",
  });
  const { subs, fetches, warnings, timers } = runModule([region], { text: "<ul>wire</ul>" });

  assert.equal(timers.count(), 0, "no timer starts for an invalid interval");
  assert.match(String(warnings[0][0]), /data-gosx-region-interval/);
  // The region itself is not disabled — its other trigger still works.
  assert.equal(subs.length, 1);
  subs[0].fn("obj-1");
  await tick();
  await tick();
  assert.equal(fetches.length, 1);
});

test("a poll tick skips while the region contains the document's focused element, and resumes once it clears", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-interval": "20s",
  });
  const { fetches, timers, context } = runModule([region], { text: "<ul>wire</ul>" });
  await tick();
  await tick();
  assert.equal(fetches.length, 1, "the immediate first tick is unaffected by a focus that starts later");

  context.document.activeElement = region;
  timers.run(20000);
  await tick();
  await tick();
  assert.equal(fetches.length, 1, "a focused region blocks its own poll tick");

  context.document.activeElement = null;
  timers.run(20000);
  await tick();
  await tick();
  assert.equal(fetches.length, 2, "the tick resumes once focus clears");
});

test("a poll tick skips while a pointer is held down inside the region, and resumes after pointerup", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-interval": "20s",
  });
  const { fetches, timers, firePointer } = runModule([region], { text: "<ul>wire</ul>" });
  await tick();
  await tick();
  assert.equal(fetches.length, 1);

  firePointer("pointerdown", region);
  timers.run(20000);
  await tick();
  await tick();
  assert.equal(fetches.length, 1, "an active pointer inside the region blocks its poll tick");

  firePointer("pointerup", region);
  timers.run(20000);
  await tick();
  await tick();
  assert.equal(fetches.length, 2, "the tick resumes once the pointer releases");
});

test("a poll tick skips while the document is hidden", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-interval": "20s",
  });
  const { fetches } = runModule([region], { text: "<ul>wire</ul>" }, { hidden: true });
  await tick();
  await tick();

  assert.equal(fetches.length, 0, "a hidden document blocks even the immediate first tick");
});

test("a poll tick skips while a navigation is in flight", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-interval": "20s",
  });
  const navigation = { getState: () => ({ phase: "pending" }) };
  const { fetches } = runModule([region], { text: "<ul>wire</ul>" }, { navigation });
  await tick();
  await tick();

  assert.equal(fetches.length, 0, "an in-flight navigation blocks the poll tick");
});

test("a poll tick never starts a second fetch while one from a previous tick is still in flight", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-interval": "20s",
  });
  let resolveFetch;
  const { fetches, timers } = runModule([region], () => new Promise((resolve) => { resolveFetch = resolve; }));
  await tick();
  assert.equal(fetches.length, 1, "the immediate first tick started one fetch");

  timers.run(20000);
  await tick();
  assert.equal(fetches.length, 1, "a tick during an in-flight poll fetch starts no second one");

  resolveFetch({ text: "<ul>wire</ul>" });
  await tick();
  await tick();
});

test("a region sends If-None-Match once a response carries an ETag, and a 304 leaves its content untouched", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-interval": "20s",
  });
  let calls = 0;
  const { fetches, timers } = runModule([region], () => {
    calls += 1;
    if (calls === 1) return { text: "<ul>wire v1</ul>", headers: { ETag: '"v1"' } };
    return { status: 304, text: "" };
  });
  await tick();
  await tick();
  assert.equal(region.innerHTML, "<ul>wire v1</ul>");

  timers.run(20000);
  await tick();
  await tick();

  assert.equal(calls, 2);
  assert.equal(fetches[1].opts.headers["If-None-Match"], '"v1"');
  assert.equal(region.innerHTML, "<ul>wire v1</ul>", "a 304 leaves the previously-swapped content untouched");
});

test("a region restores its own scrollTop and scrollLeft across a swap", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-on": "change",
  });
  region.scrollTop = 120;
  region.scrollLeft = 4;
  const { hubListeners } = runModule([region], { text: "<ul>wire</ul>" });
  hubListeners[0]({ detail: { event: "change" } });
  await tick();
  await tick();

  assert.equal(region.innerHTML, "<ul>wire</ul>");
  assert.equal(region.scrollTop, 120);
  assert.equal(region.scrollLeft, 4);
});

test("a soft-navigation dispose clears a region's poll timer", async () => {
  const region = makeRegion({
    "data-gosx-region-url": "/api/wire/events",
    "data-gosx-region-interval": "20s",
  });
  const { timers, context } = runModule([region], { text: "<ul>wire</ul>" });
  await tick();
  assert.equal(timers.count(), 1);

  context.window.__gosx_dispose_declarative_regions(context.document);
  assert.equal(timers.count(), 0, "disposal clears the poll timer along with the rest of the region's bindings");
});
