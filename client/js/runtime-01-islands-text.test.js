"use strict";
// Island hydration, runtime diagnostics, browser text measurement and
// layout, managed text blocks, document environment state and surface engines.
//
// Split out of client/js/runtime.test.js. Every shared fake, sandbox builder
// and fixture factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");

const {
  bootstrapSource,
  bootstrapLiteSource,
  FakeElement,
  FakeFontSet,
  createContext,
  installManualRAF,
  runScript,
  flushAsyncWork,
  appendManagedHead,
} = require("./runtime-test-harness.js");

test("bootstrap hydrates, delegates click events, and disposes islands", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  const button = new FakeElement("button", null);

  wrapper.id = "gosx-island-1";
  button.setAttribute("data-gosx-on-click", "increment");
  componentRoot.appendChild(button);
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-1",
          component: "Counter",
          props: { initial: 2 },
          programRef: "/counter.json",
        },
      ],
    },
    onAction: () => 1,
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  assert.deepEqual(env.hydrateCalls[0].slice(0, 3), [
    "gosx-island-1",
    "Counter",
    '{"initial":2}',
  ]);
  assert.equal(typeof env.hydrateCalls[0][3], "string");
  assert.equal(env.hydrateCalls[0][4], "json");
  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.islands.size, 1);
  assert.equal(env.document.dispatchedEvents.at(-1).type, "gosx:ready");
  assert.equal(env.document.dispatchedEvents.some((event) => event.type === "gosx:ready"), true);

  const clickEntries = wrapper.listeners.get("click") || [];
  assert.equal(clickEntries.length, 1);
  clickEntries[0].listener({
    type: "click",
    target: button,
    preventDefault() {},
  });

  assert.deepEqual(env.actionCalls, [
    ["gosx-island-1", "increment", '{"type":"click"}'],
  ]);

  env.context.__gosx_dispose_island("gosx-island-1");
  assert.equal(env.context.__gosx.islands.size, 0);
  assert.equal(wrapper.listenerCount("click"), 0);
  assert.deepEqual(env.disposeCalls, [["gosx-island-1"]]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap hydrates compute islands without a DOM root", async () => {
  const env = createContext({
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/fight-controller.json": { text: '{"name":"FightController"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      computeIslands: [
        {
          id: "gosx-compute-0",
          component: "FightController",
          props: { match: "abc" },
          programRef: "/fight-controller.json",
          capabilities: ["keyboard"],
          requiredCapabilities: ["wasm"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 0);
  assert.equal(env.computeHydrateCalls.length, 1);
  assert.deepEqual(env.computeHydrateCalls[0].slice(0, 3), [
    "gosx-compute-0",
    "FightController",
    '{"match":"abc"}',
  ]);
  assert.equal(env.context.__gosx.islands.size, 0);
  assert.equal(env.context.__gosx.computeIslands.size, 1);
  assert.ok(env.context.__gosx.input.providers.keyboard);

  env.context.__gosx_dispose_compute_island("gosx-compute-0");
  assert.equal(env.context.__gosx.computeIslands.size, 0);
  assert.deepEqual(env.disposeCalls, [["gosx-compute-0"]]);
});

test("bootstrap records island hydration failures and keeps the server fallback active", async () => {
  const wrapper = new FakeElement("div", null);
  wrapper.id = "gosx-island-1";

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.json": { text: '{"name":"Counter"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-1",
          component: "Counter",
          props: { initial: 2 },
          programRef: "/counter.json",
        },
      ],
    },
    onHydrate: () => "hydrate failed",
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const issues = env.context.__gosx.listIssues();
  assert.equal(issues.length, 1);
  assert.equal(issues[0].scope, "island");
  assert.equal(issues[0].type, "hydrate");
  assert.equal(issues[0].component, "Counter");
  assert.equal(issues[0].elementID, "gosx-island-1");
  assert.equal(wrapper.getAttribute("data-gosx-runtime-state"), "error");
  assert.equal(wrapper.getAttribute("data-gosx-runtime-issue"), "hydrate");
  assert.equal(wrapper.getAttribute("data-gosx-fallback-active"), "server");
  assert.equal(env.context.__gosx.islands.size, 0);
  assert.equal(env.document.dispatchedEvents.some((event) => event.type === "gosx:error"), true);
});

test("bootstrap exposes subscribable diagnostics with scoped clearing", () => {
  const env = createContext({ elements: [] });
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const root = new FakeElement("section", env.document);
  const child = new FakeElement("div", env.document);
  root.appendChild(child);
  const changes = [];
  const unsubscribe = env.context.__gosx.diagnostics.subscribe((change) => changes.push(change));

  env.context.__gosx.diagnostics.report({
    scope: "surface",
    type: "preview",
    element: child,
    message: "preview failed",
  });
  env.context.__gosx.diagnostics.report({
    scope: "surface",
    type: "upload",
    element: root,
    message: "upload failed",
  });

  assert.equal(env.context.__gosx.diagnostics.list().length, 2);
  assert.equal(changes.map((change) => change.type).join(","), "report,report");
  assert.equal(child.getAttribute("data-gosx-runtime-state"), "error");

  const cleared = env.context.__gosx.diagnostics.clearFor(root);
  assert.equal(cleared.length, 2);
  assert.equal(env.context.__gosx.diagnostics.list().length, 0);
  assert.equal(child.getAttribute("data-gosx-runtime-state"), "ready");
  assert.equal(root.getAttribute("data-gosx-runtime-state"), "ready");
  assert.equal(changes.map((change) => change.type).join(","), "report,report,clear,clear");

  unsubscribe();
  env.context.__gosx.diagnostics.report({ scope: "surface", type: "later", element: root });
  assert.equal(changes.length, 4);
});

test("bootstrap shares request failure reporting across runtime features", () => {
  const env = createContext({ elements: [] });
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const root = new FakeElement("section", env.document);
  const issue = env.context.__gosx.reportFailure(
    "preview",
    new Error("preview unavailable"),
    {
      scope: "runtime-surface",
      component: "editor",
      element: root,
      telemetry: { name: "editor", mode: "preview" },
    }
  );

  assert.equal(issue.scope, "runtime-surface");
  assert.equal(issue.type, "request");
  assert.equal(issue.severity, "warning");
  assert.equal(issue.phase, "preview");
  assert.equal(issue.component, "editor");
  assert.equal(issue.elementID, "");
  assert.equal(env.context.__gosx.listIssues().length, 1);
  assert.equal(env.context.__gosx.reportFailure("preview", { name: "AbortError" }), null);
  assert.equal(env.context.__gosx.listIssues().length, 1);
});

test("bootstrap forwards click target value to delegated island handlers", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);
  const button = new FakeElement("button", null);

  wrapper.id = "gosx-island-value";
  button.setAttribute("data-gosx-on-click", "selectFile");
  button.value = "schema.arb";
  componentRoot.appendChild(button);
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/selector.json": { text: '{"name":"Selector"}' },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-value",
          component: "Selector",
          props: {},
          programRef: "/selector.json",
        },
      ],
    },
    onAction: () => 1,
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const clickEntries = wrapper.listeners.get("click") || [];
  assert.equal(clickEntries.length, 1);
  clickEntries[0].listener({
    type: "click",
    target: button,
    preventDefault() {},
  });

  assert.deepEqual(env.actionCalls, [
    ["gosx-island-value", "selectFile", '{"type":"click","value":"schema.arb"}'],
  ]);
});

test("bootstrap exposes a browser text measurement helper", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const raw = env.context.__gosx_measure_text_batch("600 16px serif", JSON.stringify(["hi", "there"]));
  assert.deepEqual(JSON.parse(raw), [16, 40]);
});

test("bootstrap text measurement helper handles invalid payloads defensively", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const raw = env.context.__gosx_measure_text_batch("600 16px serif", "{");
  assert.deepEqual(JSON.parse(raw), []);
  assert.equal(env.consoleLogs.error.length > 0, true);
});

test("bootstrap exposes a browser text layout helper without wasm runtime", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["hello world", "from gosx"]);
  assert.equal(layout.lineCount, 2);
  assert.equal(layout.height, 40);
  assert.equal(layout.maxLineWidth, 88);
  assert.equal(layout.byteLen, 21);
  assert.equal(layout.runeCount, 21);
  assert.equal(layout.lines[0].byteStart, 0);
  assert.equal(layout.lines[0].byteEnd, 12);
});

test("bootstrap exposes a text layout metrics helper without wasm runtime", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const metrics = env.context.__gosx_text_layout_metrics("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.equal(metrics.lineCount, 2);
  assert.equal(metrics.height, 40);
  assert.equal(metrics.maxLineWidth, 88);
  assert.equal(metrics.byteLen, 21);
  assert.equal(metrics.runeCount, 21);
});

test("bootstrap exposes a text layout ranges helper without wasm runtime", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const result = env.context.__gosx_text_layout_ranges("ab\u00adcd", "600 16px serif", 80, "normal", 20);
  assert.equal(result.lineCount, 1);
  assert.equal(result.lines.length, 1);
  assert.equal(result.lines[0].softBreak, false);
  assert.equal(result.lines[0].hardBreak, false);
});

test("bootstrap browser text layout helper preserves pre-wrap hard breaks", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hi\n", "600 16px serif", 200, "pre-wrap", 18);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["hi", ""]);
  assert.equal(layout.lineCount, 2);
  assert.equal(layout.height, 36);
  assert.equal(layout.lines[0].hardBreak, true);
  assert.equal(layout.lines[1].byteStart, 3);
  assert.equal(layout.lines[1].byteEnd, 3);
});

test("bootstrap browser text layout keeps normal trailing spaces out of max width", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hello ", "600 16px serif", 200, "normal", 18);
  assert.equal(layout.maxLineWidth, 40);
  assert.equal(layout.lines[0].text, "hello");
});

test("bootstrap browser text layout breaks long tokens at grapheme boundaries", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("abcdef", "600 16px serif", 4, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["abcd", "ef"]);
  assert.equal(layout.maxLineWidth, 4);
});

test("bootstrap browser text layout uses browser-style tab stops", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("a\tb", "600 16px serif", 99, "pre-wrap", 12);
  assert.equal(layout.lineCount, 1);
  assert.equal(layout.maxLineWidth, 9);
  assert.equal(layout.lines[0].text, "a\tb");
});

test("bootstrap browser text layout breaks at soft hyphens", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("ab\u00adcd", "600 16px serif", 3, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["ab-", "cd"]);
  assert.equal(layout.lines[0].width, 3);
});

test("bootstrap browser text layout breaks at zero-width spaces", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("foo\u200bbar", "600 16px serif", 3, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["foo", "bar"]);
});

test("bootstrap browser text layout prefers word boundaries inside punctuation-heavy runs", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hello,world", "600 16px serif", 7, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["hello,", "world"]);
});

test("bootstrap browser text layout uses Intl word boundaries for Thai runs", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("สวัสดีครับโลก", "600 16px serif", 6, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["สวัสดี", "ครับ", "โลก"]);
});

test("bootstrap browser text layout keeps CJK closing punctuation off line starts", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("あ。い", "600 16px serif", 1, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["あ。", "い"]);
  assert.equal(layout.lines[0].width, 2);
});

test("bootstrap browser text layout keeps opening punctuation with following glyphs", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("(a", "600 16px serif", 1, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["(a"]);
  assert.equal(layout.lines[0].width, 2);
});

test("bootstrap browser text layout keeps emoji grapheme clusters intact", () => {
  const graphemeSegmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
  const env = createContext({
    measureText(text) {
      let count = 0;
      for (const _entry of graphemeSegmenter.segment(String(text))) {
        count += 1;
      }
      return count;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("👨‍👩‍👧‍👦a", "600 16px serif", 1, "normal", 12);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["👨‍👩‍👧‍👦", "a"]);
  assert.equal(layout.lineCount, 2);
});

test("bootstrap browser text layout supports max-lines ellipsis clamp", () => {
  const env = createContext({
    measureText(text) {
      return Array.from(String(text)).length;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const layout = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 11, "normal", 12, {
    maxLines: 1,
    overflow: "ellipsis",
  });
  assert.equal(layout.lineCount, 1);
  assert.equal(layout.truncated, true);
  assert.equal(layout.lines[0].truncated, true);
  assert.equal(layout.lines[0].ellipsis, true);
  assert.equal(layout.lines[0].text, "hello worl…");
});

test("bootstrap browser text layout invalidates cached widths after font loading events", () => {
  let scale = 1;
  const fonts = new FakeFontSet();
  const env = createContext({
    fonts,
    measureText(text) {
      return String(text).length * 8 * scale;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");

  const before = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.deepEqual(Array.from(before.lines, (line) => line.text), ["hello world", "from gosx"]);

  scale = 2;
  fonts.dispatch("loadingdone");

  const after = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.deepEqual(Array.from(after.lines, (line) => line.text), ["hello", "world", "from", "gosx"]);
  assert.equal(after.lineCount, 4);
});

test("bootstrap mounts declarative text layout blocks as managed runtime state", async () => {
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-ready"), "true");
  assert.equal(block.getAttribute("data-gosx-text-layout-role"), "block");
  assert.equal(block.getAttribute("data-gosx-text-layout-surface"), "dom");
  assert.equal(block.getAttribute("data-gosx-text-layout-state"), "ready");
  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "2");
  assert.equal(block.getAttribute("data-gosx-text-layout-height"), "40");
  assert.equal(block.getAttribute("data-gosx-text-layout-byte-length"), "21");
  assert.equal(block.style["--gosx-text-layout-height"], "40px");
  assert.equal(env.context.__gosx.textLayouts.size, 1);

  const result = env.context.__gosx.textLayout.read(block);
  assert.equal(result.lineCount, 2);
  assert.equal(result.maxLineWidth, 88);
  assert.equal(env.document.dispatchedEvents.some((event) => event.type === "gosx:textlayout"), true);
});

test("bootstrap lite mounts managed text layout without a manifest", async () => {
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(block.getAttribute("data-gosx-text-layout-ready"), "true");
  assert.equal(env.context.__gosx.textLayouts.size, 1);
});

test("bootstrap lite mounts managed motion blocks and plays load presets", async () => {
  const block = new FakeElement("section", null);
  block.setAttribute("data-gosx-motion", "");
  block.setAttribute("data-gosx-motion-preset", "slide-up");
  block.setAttribute("data-gosx-motion-duration", "360");
  block.setAttribute("data-gosx-motion-delay", "40");
  block.setAttribute("data-gosx-motion-distance", "24");
  block.setAttribute("data-gosx-motion-easing", "ease-out");

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(block.getAttribute("data-gosx-motion-state"), "finished");
  assert.equal(block.animateCalls.length, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(block.animateCalls[0].keyframes)), [
    { opacity: 0, transform: "translate3d(0, 24px, 0)" },
    { opacity: 1, transform: "translate3d(0, 0, 0)" },
  ]);
  assert.deepEqual(JSON.parse(JSON.stringify(block.animateCalls[0].options)), {
    duration: 360,
    delay: 40,
    easing: "ease-out",
    fill: "both",
  });
});

test("bootstrap lite respects reduced motion on managed motion blocks", async () => {
  const block = new FakeElement("div", null);
  block.setAttribute("data-gosx-motion", "");

  const env = createContext({
    elements: [block],
    prefersReducedMotion: true,
  });

  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-motion-state"), "reduced");
  assert.equal(block.animateCalls.length, 0);
});

test("bootstrap lite defers managed motion view triggers until intersection", async () => {
  const block = new FakeElement("div", null);
  block.setAttribute("data-gosx-motion", "");
  block.setAttribute("data-gosx-motion-trigger", "view");
  block.setAttribute("data-gosx-motion-preset", "zoom-in");

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapLiteSource, env.context, "bootstrap-lite.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-motion-state"), "idle");
  assert.equal(block.animateCalls.length, 0);
  assert.equal(env.intersectionObservers.length, 1);

  env.intersectionObservers[0].trigger([
    { target: block, isIntersecting: true, intersectionRatio: 0.5 },
  ]);
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-motion-state"), "finished");
  assert.equal(block.animateCalls.length, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(block.animateCalls[0].keyframes)), [
    { opacity: 0, transform: "scale(0.91)" },
    { opacity: 1, transform: "scale(1)" },
  ]);
});

test("bootstrap mounts declarative text layout clamp options on managed blocks", async () => {
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.setAttribute("data-gosx-text-layout-white-space", "pre-wrap");
  block.setAttribute("data-gosx-text-layout-align", "center");
  block.setAttribute("data-gosx-text-layout-max-lines", "1");
  block.setAttribute("data-gosx-text-layout-overflow", "ellipsis");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-max-lines"), "1");
  assert.equal(block.getAttribute("data-gosx-text-layout-overflow"), "ellipsis");
  assert.equal(block.getAttribute("data-gosx-text-layout-state"), "truncated");
  assert.equal(block.getAttribute("data-gosx-text-layout-truncated"), "true");
  assert.equal(block.style["--gosx-text-layout-white-space-mode"], "pre-wrap");
  assert.equal(block.style["--gosx-text-layout-align"], "center");
  assert.equal(block.style["--gosx-text-layout-max-lines"], "1");
  assert.equal(env.context.__gosx.textLayout.read(block).truncated, true);
});

test("bootstrap installs a stronger CSS contract for managed text layout blocks", async () => {
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.setAttribute("data-gosx-text-layout-max-width", "88");
  block.setAttribute("align", "right");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const styleTag = env.document.head.children[0];
  assert.equal(styleTag.tagName, "STYLE");
  assert.ok(styleTag.textContent.includes('white-space: var(--gosx-text-layout-white-space-mode, normal);'));
  assert.ok(styleTag.textContent.includes('[data-gosx-text-layout-role="block"][data-gosx-text-layout-max-width]'));
  assert.equal(block.style["--gosx-text-layout-align"], "right");
  assert.equal(block.style["--gosx-text-layout-max-width"], "88px");
  assert.equal(block.getAttribute("data-gosx-text-layout-state"), "ready");
});

test("bootstrap derives managed text layout config from computed styles and CSS vars", async () => {
  const block = new FakeElement("div", null);
  block.setAttribute("data-gosx-text-layout", "");
  block.textContent = "hello world from gosx";
  block.computedStyle = {
    font: "600 16px serif",
    textAlign: "center",
    whiteSpace: "pre-wrap",
    lineHeight: "22px",
    maxWidth: "88px",
    textOverflow: "ellipsis",
    getPropertyValue(name) {
      switch (name) {
        case "--gosx-text-layout-max-lines":
          return "1";
        case "--gosx-text-layout-overflow":
          return "ellipsis";
        default:
          return this[name] || "";
      }
    },
  };

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-align"), "center");
  assert.equal(block.getAttribute("data-gosx-text-layout-white-space"), "pre-wrap");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-width"), "88");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-lines"), "1");
  assert.equal(block.getAttribute("data-gosx-text-layout-overflow"), "ellipsis");
  assert.equal(block.getAttribute("data-gosx-text-layout-state"), "truncated");
  assert.equal(block.style["--gosx-text-layout-line-height"], "22px");
  assert.equal(env.context.__gosx.textLayout.read(block).truncated, true);
});

test("bootstrap derives managed text layout from logical inline size and locale-aware presentation", async () => {
  const block = new FakeElement("div", null);
  block.width = 80;
  block.height = 160;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("lang", "th");
  block.textContent = "hello gosx app";
  block.computedStyle = {
    font: "600 16px serif",
    textAlign: "start",
    whiteSpace: "normal",
    lineHeight: "20px",
    writingMode: "vertical-rl",
    maxInlineSize: "none",
    getPropertyValue(name) {
      return this[name] || "";
    },
  };

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-locale"), "th");
  assert.equal(block.getAttribute("data-gosx-text-layout-writing-mode"), "vertical-rl");
  assert.equal(block.getAttribute("data-gosx-text-layout-inline-size"), "160");
  assert.equal(block.getAttribute("data-gosx-text-layout-block-size"), "80");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-width"), "160");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-inline-size"), "160");
  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "1");
});

test("bootstrap refreshes managed text layout blocks after computed style changes", async () => {
  const block = new FakeElement("div", null);
  block.setAttribute("data-gosx-text-layout", "");
  block.textContent = "hello world from gosx";
  block.computedStyle = {
    font: "600 16px serif",
    textAlign: "left",
    whiteSpace: "normal",
    lineHeight: "20px",
    maxWidth: "88px",
    getPropertyValue(name) {
      return this[name] || "";
    },
  };

  const env = createContext({
    elements: [block],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "2");
  assert.equal(block.getAttribute("data-gosx-text-layout-align"), "left");

  block.computedStyle.textAlign = "right";
  block.computedStyle.maxWidth = "200px";
  env.context.__gosx.textLayout.refresh(block);

  assert.equal(block.getAttribute("data-gosx-text-layout-align"), "right");
  assert.equal(block.getAttribute("data-gosx-text-layout-max-width"), "200");
  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "1");
});

test("bootstrap refreshes managed text layout after inherited locale and direction changes", async () => {
  const container = new FakeElement("section", null);
  container.setAttribute("lang", "en");
  container.setAttribute("dir", "ltr");
  const block = new FakeElement("div", null);
  block.width = 120;
  block.height = 40;
  block.setAttribute("data-gosx-text-layout", "");
  block.textContent = "hello world";
  block.computedStyle = {
    font: "600 16px serif",
    lineHeight: "20px",
    textAlign: "start",
    whiteSpace: "normal",
    getPropertyValue(name) {
      return this[name] || "";
    },
  };
  container.appendChild(block);

  const env = createContext({
    elements: [container],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-locale"), "en");
  assert.equal(block.getAttribute("data-gosx-text-layout-direction"), "ltr");

  container.setAttribute("lang", "th");
  container.setAttribute("dir", "rtl");
  const presentationObserver = env.mutationObservers.find((observer) => observer.targets.has(env.document.documentElement));
  assert.ok(presentationObserver);
  presentationObserver.trigger([
    { target: container, type: "attributes", attributeName: "lang" },
    { target: container, type: "attributes", attributeName: "dir" },
  ]);
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-locale"), "th");
  assert.equal(block.getAttribute("data-gosx-text-layout-direction"), "rtl");
});

test("bootstrap shares presentation observers across managed text layout blocks and tears them down", async () => {
  const container = new FakeElement("section", null);
  const first = new FakeElement("div", null);
  const second = new FakeElement("div", null);
  for (const block of [first, second]) {
    block.width = 88;
    block.setAttribute("data-gosx-text-layout", "");
    block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
    block.setAttribute("data-gosx-text-layout-line-height", "20");
    block.textContent = "hello world from gosx";
    container.appendChild(block);
  }

  const env = createContext({
    elements: [container],
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const presentationObserver = env.mutationObservers.filter((observer) => observer.targets.has(env.document.documentElement));
  assert.equal(presentationObserver.length, 1);
  assert.equal(env.resizeObservers.length >= 1, true);
  assert.equal(env.resizeObservers[0].targets.has(first), true);
  assert.equal(env.resizeObservers[0].targets.has(second), true);

  env.context.__gosx.textLayout.dispose(first);

  assert.equal(env.resizeObservers[0].targets.has(first), false);
  assert.equal(env.resizeObservers[0].targets.has(second), true);

  env.context.__gosx.textLayout.dispose(second);

  assert.equal(presentationObserver[0].targets.size, 0);
  assert.equal(env.resizeObservers[0].targets.size, 0);
});

test("bootstrap coalesces presentation-driven text layout refreshes into one frame", async () => {
  const container = new FakeElement("section", null);
  container.setAttribute("lang", "en");
  const block = new FakeElement("div", null);
  block.width = 120;
  block.height = 40;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.textContent = "hello world from gosx";
  container.appendChild(block);

  const env = createContext({
    elements: [container],
  });
  const raf = installManualRAF(env.context);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  let updates = 0;
  block.addEventListener("gosx:textlayout", () => {
    updates += 1;
  });

  container.setAttribute("lang", "th");
  container.setAttribute("dir", "rtl");
  const presentationObserver = env.mutationObservers.find((observer) => observer.targets.has(env.document.documentElement));
  assert.ok(presentationObserver);

  presentationObserver.trigger([
    { target: container, type: "attributes", attributeName: "lang" },
  ]);
  presentationObserver.trigger([
    { target: container, type: "attributes", attributeName: "dir" },
  ]);

  assert.equal(raf.count(), 1);
  assert.equal(updates, 0);

  raf.flush();
  await flushAsyncWork();

  assert.equal(updates, 1);
  assert.equal(block.getAttribute("data-gosx-text-layout-locale"), "th");
  assert.equal(block.getAttribute("data-gosx-text-layout-direction"), "rtl");
});

test("bootstrap exposes unified environment, document, and presentation state", async () => {
  const block = new FakeElement("div", null);
  block.width = 144;
  block.height = 48;
  block.computedStyle = {
    font: "600 16px serif",
    lineHeight: "24px",
    direction: "rtl",
    writingMode: "vertical-rl",
    whiteSpace: "pre-wrap",
    textAlign: "end",
    maxWidth: "144px",
    getPropertyValue(name) {
      return this[name] || "";
    },
  };

  const env = createContext({
    elements: [block],
    visibilityState: "hidden",
    prefersReducedMotion: true,
    devicePixelRatio: 1.75,
    deviceMemory: 4,
    hardwareConcurrency: 4,
    visualViewportWidth: 640,
    visualViewportHeight: 360,
    visualViewportOffsetTop: 12,
    matchMedia: {
      "(prefers-reduced-data: reduce)": true,
      "(pointer: coarse)": true,
      "(any-pointer: coarse)": true,
      "(hover: hover)": false,
      "(any-hover: hover)": false,
      "(prefers-contrast: more)": true,
      "(prefers-color-scheme: dark)": true,
    },
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-docs-home",
      pattern: "GET /docs",
      path: "/docs",
      title: "Docs",
      status: 200,
      requestID: "req-123",
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  const fileCSS = env.document.createElement("style");
  fileCSS.setAttribute("data-gosx-file-css", "docs.css");
  fileCSS.setAttribute("data-gosx-file-css-scope", "docs-scope");
  fileCSS.setAttribute("data-gosx-css-layer", "page");
  fileCSS.setAttribute("data-gosx-css-owner", "page-file");
  fileCSS.setAttribute("data-gosx-css-source", "docs.css");
  fileCSS.setAttribute("data-gosx-css-order", "0");
  const stylesheet = env.document.createElement("link");
  stylesheet.setAttribute("rel", "stylesheet");
  stylesheet.setAttribute("href", "/app.css");
  appendManagedHead(env.document, [contract, fileCSS, stylesheet]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const environmentState = env.context.__gosx.environment.get();
  assert.equal(environmentState.pageVisible, false);
  assert.equal(environmentState.coarsePointer, true);
  assert.equal(environmentState.reducedMotion, true);
  assert.equal(environmentState.reducedData, true);
  assert.equal(environmentState.lowPower, true);
  assert.equal(environmentState.colorScheme, "dark");
  assert.equal(environmentState.contrast, "more");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-env-reduced-motion"), "true");
  assert.equal(env.document.documentElement.style["--gosx-env-visual-viewport-height"], "360px");

  const documentState = env.context.__gosx.document.get();
  assert.equal(documentState.page.id, "gosx-doc-docs-home");
  assert.equal(documentState.page.pattern, "GET /docs");
  assert.equal(documentState.enhancement.layer, "bootstrap");
  assert.equal(documentState.enhancement.navigation, true);
  assert.equal(documentState.css.owned[0].file, "docs.css");
  assert.equal(documentState.css.owned[0].layer, "page");
  assert.equal(documentState.css.layers.page.count, 1);
  assert.equal(documentState.css.layers.page.owners[0], "page-file");
  assert.equal(documentState.css.owned.some((entry) => entry.kind === "stylesheet" && entry.href === "/app.css"), true);
  assert.equal(documentState.css.stylesheets[0].layer, "global");
  assert.equal(documentState.css.stylesheets[0].owner, "document-global");
  assert.equal(documentState.css.stylesheets[0].source, "/app.css");
  assert.equal(documentState.css.owned.some((entry) => entry.layer === "runtime"), true);
  assert.equal(documentState.css.layers.runtime.count, 1);
  assert.equal(env.context.__gosx.document.css("page").count, 1);
  assert.equal(env.document.documentElement.getAttribute("data-gosx-document-id"), "gosx-doc-docs-home");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-css-page-count"), "1");
  assert.equal(env.document.body.getAttribute("data-gosx-enhancement-layer"), "bootstrap");

  const presentation = env.context.__gosx.presentation.read(block);
  assert.equal(presentation.direction, "rtl");
  assert.equal(presentation.writingMode, "vertical-rl");
  assert.equal(presentation.lang, "");
  assert.equal(presentation.inlineSize, 48);
  assert.equal(presentation.blockSize, 144);
  assert.equal(presentation.maxWidth, 144);
  assert.equal(presentation.maxInlineSize, 144);
  assert.equal(presentation.environment.reducedData, true);
});

test("bootstrap exposes document assets and enhancement inventory", async () => {
  const link = new FakeElement("a", null);
  link.setAttribute("href", "/docs");
  link.setAttribute("data-gosx-link", "");
  link.setAttribute("data-gosx-enhance", "navigation");
  link.setAttribute("data-gosx-enhance-layer", "bootstrap");
  link.setAttribute("data-gosx-fallback", "native-link");

  const form = new FakeElement("form", null);
  form.setAttribute("data-gosx-form", "");
  form.setAttribute("data-gosx-enhance", "form");
  form.setAttribute("data-gosx-enhance-layer", "bootstrap");
  form.setAttribute("data-gosx-fallback", "native-form");

  const text = new FakeElement("div", null);
  text.setAttribute("data-gosx-text-layout", "");
  text.setAttribute("data-gosx-enhance", "text-layout");
  text.setAttribute("data-gosx-enhance-layer", "bootstrap");
  text.setAttribute("data-gosx-fallback", "html");

  const scene = new FakeElement("div", null);
  scene.id = "scene-runtime";
  scene.setAttribute("data-gosx-engine", "GoSXScene3D");
  scene.setAttribute("data-gosx-enhance", "scene3d");
  scene.setAttribute("data-gosx-enhance-layer", "runtime");
  scene.setAttribute("data-gosx-fallback", "server");

  const island = new FakeElement("div", null);
  island.id = "counter-island";
  island.setAttribute("data-gosx-island", "Counter");
  island.setAttribute("data-gosx-enhance", "island");
  island.setAttribute("data-gosx-enhance-layer", "runtime");
  island.setAttribute("data-gosx-fallback", "server");

  const env = createContext({
    elements: [link, form, text, scene, island],
  });

  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-docs-home",
      pattern: "GET /docs",
      path: "/docs",
      title: "Docs",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: true,
      navigation: true,
    },
    assets: {
      bootstrapMode: "full",
      manifest: true,
      runtimePath: "/runtime.wasm",
      wasmExecPath: "/wasm_exec.js",
      patchPath: "/patch.js",
      bootstrapPath: "/bootstrap.js",
      hlsPath: "/hls.min.js",
      islands: 1,
      engines: 1,
      hubs: 1,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const documentState = env.context.__gosx.document.get();
  assert.equal(documentState.assets.runtime.bootstrapMode, "full");
  assert.equal(documentState.assets.runtime.manifest, true);
  assert.equal(documentState.assets.runtime.runtimePath, "/runtime.wasm");
  assert.equal(documentState.assets.runtime.bootstrapPath, "/bootstrap.js");
  assert.equal(documentState.assets.runtime.hlsPath, "/hls.min.js");
  assert.equal(documentState.assets.runtime.islands, 1);
  assert.equal(documentState.assets.runtime.engines, 1);
  assert.equal(documentState.assets.runtime.hubs, 1);
  assert.equal(documentState.enhancement.bootstrap, true);
  assert.equal(documentState.enhancement.runtime, true);
  assert.equal(documentState.enhancements.count, 5);
  assert.equal(documentState.enhancements.layers.bootstrap.count, 3);
  assert.equal(documentState.enhancements.layers.runtime.count, 2);
  assert.equal(documentState.enhancements.kinds.navigation.count, 1);
  assert.equal(documentState.enhancements.kinds["text-layout"].count, 1);
  assert.equal(env.context.__gosx.document.enhancements("scene3d").count, 1);
  assert.equal(env.document.documentElement.getAttribute("data-gosx-bootstrap-mode"), "full");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-enhancement-count"), "5");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-enhancement-navigation-count"), "1");
  assert.equal(env.document.body.getAttribute("data-gosx-enhancement-runtime-count"), "2");
});

test("bootstrap refreshes document state after navigation events", async () => {
  const env = createContext({});
  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-home",
      pattern: "GET /",
      path: "/",
      title: "Home",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.document.get().page.id, "gosx-doc-home");

  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-docs",
      pattern: "GET /docs",
      path: "/docs",
      title: "Docs",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  env.document.dispatchEvent(new env.context.CustomEvent("gosx:navigate", {
    detail: { url: "/docs" },
  }));
  await flushAsyncWork();

  assert.equal(env.context.__gosx.document.get().page.id, "gosx-doc-docs");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-route-pattern"), "GET /docs");
});

test("bootstrap refreshes document CSS state after head mutations", async () => {
  const env = createContext({});
  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-home",
      pattern: "GET /",
      path: "/",
      title: "Home",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const stylesheet = env.document.createElement("link");
  stylesheet.setAttribute("rel", "stylesheet");
  stylesheet.setAttribute("href", "/layout.css");
  stylesheet.setAttribute("data-gosx-css-layer", "layout");
  stylesheet.setAttribute("data-gosx-css-owner", "document-layout");
  stylesheet.setAttribute("data-gosx-css-source", "layout.css");
  const managedEnd = env.document.head.childNodes.find((node) => node.getAttribute && node.getAttribute("name") === "gosx-head-end");
  env.document.head.insertBefore(stylesheet, managedEnd);

  const headObserver = env.mutationObservers.find((observer) => observer.targets.has(env.document.head));
  assert.ok(headObserver, "expected head mutation observer");
  headObserver.trigger([{ target: env.document.head, type: "childList" }]);
  await flushAsyncWork();
  env.context.__gosx.document.refresh("head-mutation");

  assert.equal(env.context.__gosx.document.get().css.layers.layout.count, 1);
  assert.equal(env.context.__gosx.document.css("layout").sources[0], "layout.css");
  assert.equal(env.document.documentElement.getAttribute("data-gosx-css-layout-count"), "1");
});

test("bootstrap coalesces head mutation refreshes into one document update turn", async () => {
  const env = createContext({});
  const contract = env.document.createElement("script");
  contract.id = "gosx-document";
  contract.setAttribute("type", "application/json");
  contract.setAttribute("data-gosx-document-contract", "");
  contract.textContent = JSON.stringify({
    version: 1,
    page: {
      id: "gosx-doc-home",
      pattern: "GET /",
      path: "/",
      title: "Home",
      status: 200,
    },
    enhancement: {
      bootstrap: true,
      runtime: false,
      navigation: true,
    },
  });
  appendManagedHead(env.document, [contract]);

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const initialEvents = env.document.dispatchedEvents.filter((event) => event.type === "gosx:document").length;
  const headObserver = env.mutationObservers.find((observer) => observer.targets.has(env.document.head));
  assert.ok(headObserver);

  const stylesheet = env.document.createElement("link");
  stylesheet.setAttribute("rel", "stylesheet");
  stylesheet.setAttribute("href", "/page.css");
  stylesheet.setAttribute("data-gosx-css-layer", "page");
  stylesheet.setAttribute("data-gosx-css-owner", "document-page");
  stylesheet.setAttribute("data-gosx-css-source", "page.css");
  const managedEnd = env.document.head.childNodes.find((node) => node.getAttribute && node.getAttribute("name") === "gosx-head-end");
  env.document.head.insertBefore(stylesheet, managedEnd);
  stylesheet.setAttribute("media", "screen");

  headObserver.trigger([{ target: env.document.head, type: "childList" }]);
  headObserver.trigger([{ target: stylesheet, type: "attributes", attributeName: "media" }]);
  await flushAsyncWork();

  const nextEvents = env.document.dispatchedEvents.filter((event) => event.type === "gosx:document").length;
  assert.equal(nextEvents, initialEvents + 1);
});

test("bootstrap refreshes managed text layout blocks after font metric invalidation", async () => {
  let scale = 1;
  const fonts = new FakeFontSet();
  const block = new FakeElement("div", null);
  block.width = 88;
  block.setAttribute("data-gosx-text-layout", "");
  block.setAttribute("data-gosx-text-layout-font", "600 16px serif");
  block.setAttribute("data-gosx-text-layout-line-height", "20");
  block.textContent = "hello world from gosx";

  const env = createContext({
    elements: [block],
    fonts,
    measureText(text) {
      return String(text).length * 8 * scale;
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "2");

  scale = 2;
  fonts.dispatch("loadingdone");
  await flushAsyncWork();

  assert.equal(block.getAttribute("data-gosx-text-layout-line-count"), "4");
  assert.equal(block.getAttribute("data-gosx-text-layout-revision"), "1");
  assert.equal(env.context.__gosx.textLayout.read(block).lineCount, 4);
});

test("bootstrap adopts and caches runtime-provided text layout implementations", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  let calls = 0;
  env.context.__gosx_text_layout = function(text, font, maxWidth, whiteSpace, lineHeight) {
    calls += 1;
    return {
      lines: [{ text: String(text) }],
      lineCount: 1,
      height: Number(lineHeight) || 1,
      maxLineWidth: Math.min(Number(maxWidth) || 0, 24),
      byteLen: String(text).length,
      runeCount: String(text).length,
      font,
      whiteSpace,
    };
  };

  env.context.__gosx_runtime_ready();

  const first = env.context.__gosx_text_layout("hi", "600 16px serif", 80, "normal", 18);
  const second = env.context.__gosx_text_layout("hi", "600 16px serif", 80, "normal", 18);
  assert.equal(calls, 1);
  assert.equal(first.lineCount, 1);
  assert.equal(second.lineCount, 1);
  assert.equal(first.height, 18);
  assert.equal(second.maxLineWidth, 24);
});

test("bootstrap adopts and caches runtime-provided text layout metrics implementations", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  let calls = 0;
  env.context.__gosx_text_layout_metrics = function(text, font, maxWidth, whiteSpace, lineHeight) {
    calls += 1;
    return {
      lineCount: 3,
      height: Number(lineHeight) * 3,
      maxLineWidth: Math.min(Number(maxWidth) || 0, 42),
      byteLen: String(text).length,
      runeCount: String(text).length,
      font,
      whiteSpace,
    };
  };

  env.context.__gosx_runtime_ready();

  const first = env.context.__gosx_text_layout_metrics("hi", "600 16px serif", 80, "normal", 18);
  const second = env.context.__gosx_text_layout_metrics("hi", "600 16px serif", 80, "normal", 18);
  assert.equal(calls, 1);
  assert.equal(first.lineCount, 3);
  assert.equal(second.height, 54);
  assert.equal(second.maxLineWidth, 42);
});

test("bootstrap adopts and caches runtime-provided text layout ranges implementations", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  let calls = 0;
  env.context.__gosx_text_layout_ranges = function(text) {
    calls += 1;
    return {
      lines: [{ start: 0, end: 1, byteStart: 0, byteEnd: 1, runeStart: 0, runeEnd: 1, width: 7, hardBreak: false, softBreak: true }],
      lineCount: 1,
      height: 18,
      maxLineWidth: 7,
      byteLen: String(text).length,
      runeCount: String(text).length,
    };
  };

  env.context.__gosx_runtime_ready();

  const first = env.context.__gosx_text_layout_ranges("x", "600 16px serif", 80, "normal", 18);
  const second = env.context.__gosx_text_layout_ranges("x", "600 16px serif", 80, "normal", 18);
  assert.equal(calls, 1);
  assert.equal(first.lines[0].softBreak, true);
  assert.equal(second.maxLineWidth, 7);
});

test("bootstrap falls back to browser layout when runtime text layout fails", () => {
  const env = createContext({});

  runScript(bootstrapSource, env.context, "bootstrap.js");

  env.context.__gosx_text_layout = function() {
    throw new Error("boom");
  };

  env.context.__gosx_runtime_ready();

  const layout = env.context.__gosx_text_layout("hello world from gosx", "600 16px serif", 88, "normal", 20);
  assert.deepEqual(Array.from(layout.lines, (line) => line.text), ["hello world", "from gosx"]);
  assert.equal(layout.lineCount, 2);
  assert.equal(env.consoleLogs.error.length > 0, true);
});

test("bootstrap rerenders static Scene3D labels after font loading changes metrics", async () => {
  let scale = 1;
  const fonts = new FakeFontSet();
  const mount = new FakeElement("div", null);
  mount.id = "scene-font-refresh";

  const env = createContext({
    elements: [mount],
    fonts,
    measureText(text) {
      return String(text).length * 8 * scale;
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-font-refresh",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-font-refresh",
          jsExport: "GoSXScene3D",
          props: {
            width: 520,
            height: 320,
            autoRotate: false,
            scene: {
              objects: [
                { kind: "box", width: 1.8, height: 1.2, depth: 1.1, x: -0.8, y: 0.1, z: 0.2, color: "#8de1ff" },
              ],
              labels: [
                {
                  id: "font-refresh-label",
                  text: "hello world from gosx",
                  x: 0,
                  y: 1.3,
                  z: 0.8,
                  maxWidth: 88,
                },
              ],
            },
          },
          capabilities: ["canvas"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const labelLayer = mount.children[1];
  assert.equal(labelLayer.children.length, 1);
  assert.equal(labelLayer.children[0].children.length, 2);

  scale = 2;
  fonts.dispatch("loadingdone");
  await flushAsyncWork();

  assert.equal(labelLayer.children[0].children.length, 4);
  assert.equal(labelLayer.children[0].textContent, "helloworldfromgosx");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap infers binary island programs from .gxi refs", async () => {
  const wrapper = new FakeElement("div", null);
  const componentRoot = new FakeElement("div", null);

  wrapper.id = "gosx-island-bin";
  componentRoot.appendChild(new FakeElement("span", null));
  wrapper.appendChild(componentRoot);

  const env = createContext({
    elements: [wrapper],
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
      "/counter.gxi": { bytes: [1, 2, 3, 4] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      islands: [
        {
          id: "gosx-island-bin",
          component: "Counter",
          props: {},
          programRef: "/counter.gxi",
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.hydrateCalls.length, 1);
  assert.equal(env.hydrateCalls[0][4], "bin");
  assert.ok(env.hydrateCalls[0][3] instanceof Uint8Array);
  assert.deepEqual(Array.from(env.hydrateCalls[0][3]), [1, 2, 3, 4]);
  assert.equal(env.consoleLogs.error.length, 0);
});

test("bootstrap mounts registered surface engines without escape-hatch scripts", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "board-root";

  const env = createContext({
    elements: [mount],
    engineFactories: {
      Whiteboard(ctx) {
        env.engineMounts.push({
          id: ctx.id,
          component: ctx.component,
          mountID: ctx.mount.id,
          props: ctx.props,
          capabilities: ctx.capabilities.slice(),
        });
        return {
          dispose() {
            env.engineDisposals.push(ctx.id);
          },
        };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-0",
          component: "Whiteboard",
          kind: "surface",
          mountId: "board-root",
          props: { room: "abc" },
          capabilities: ["canvas", "animation"],
          programRef: "/engines/Whiteboard.wasm",
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.ready, true);
  assert.equal(env.context.__gosx.engines.size, 1);
  assert.deepEqual(env.engineMounts, [
    {
      id: "gosx-engine-0",
      component: "Whiteboard",
      mountID: "board-root",
      props: { room: "abc" },
      capabilities: ["canvas", "animation"],
    },
  ]);

  env.context.__gosx_dispose_engine("gosx-engine-0");
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.deepEqual(env.engineDisposals, ["gosx-engine-0"]);
  assert.equal(env.consoleLogs.warn.length, 0);
});

test("bootstrap exposes managed pixel surfaces to surface engines", async () => {
  const mount = new FakeElement("div", null);
  const fallback = new FakeElement("p", null);
  fallback.textContent = "server fallback";
  mount.id = "pixel-root";
  mount.width = 320;
  mount.height = 288;
  mount.appendChild(fallback);

  const env = createContext({
    elements: [mount],
    engineFactories: {
      PixelBoard(ctx) {
        const frameFromContext = ctx.runtime.pixelSurface();
        const frameFromGlobal = env.context.__gosx_engine_frame(ctx.id);
        env.engineMounts.push({
          id: ctx.id,
          sameFrame: frameFromContext === frameFromGlobal,
          width: frameFromContext.width,
          height: frameFromContext.height,
          scaling: frameFromContext.scaling,
          inside: frameFromContext.toPixel(64, 72).inside,
          pixel: frameFromContext.toPixel(64, 72),
        });
        frameFromContext.pixels[0] = 17;
        frameFromContext.pixels[1] = 34;
        frameFromContext.pixels[2] = 51;
        frameFromContext.pixels[3] = 255;
        frameFromContext.present();
        return {
          dispose() {
            env.engineDisposals.push(ctx.id);
          },
        };
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-pixel",
          component: "PixelBoard",
          kind: "surface",
          mountId: "pixel-root",
          props: { mode: "retro" },
          capabilities: ["pixel-surface", "canvas"],
          pixelSurface: {
            width: 160,
            height: 144,
            scaling: "fill",
            clearColor: [3, 4, 5, 255],
            vsync: false,
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 1);
  assert.deepEqual(JSON.parse(JSON.stringify(env.engineMounts)), [
    {
      id: "gosx-engine-pixel",
      sameFrame: true,
      width: 160,
      height: 144,
      scaling: "fill",
      inside: true,
      pixel: { x: 32, y: 36, inside: true },
    },
  ]);
  assert.equal(mount.getAttribute("data-gosx-pixel-surface-mounted"), "true");
  assert.equal(mount.style.backgroundColor, "rgba(3, 4, 5, 1)");
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0].tagName, "CANVAS");
  assert.equal(mount.children[0].getAttribute("data-gosx-pixel-surface"), "true");
  assert.equal(mount.children[0].width, 160);
  assert.equal(mount.children[0].height, 144);
  assert.equal(mount.children[0].style.width, "320px");
  assert.equal(mount.children[0].style.height, "288px");
  const ctx2d = mount.children[0].getContext("2d");
  assert.ok(ctx2d.ops.some((entry) => entry[0] === "putImageData" && entry[1] === 0 && entry[2] === 0));
  assert.equal(Array.from(ctx2d.lastImageData.data.slice(0, 4)).join(","), "17,34,51,255");
  const frame = env.context.__gosx_engine_frame("gosx-engine-pixel");
  assert.equal(frame.width, 160);
  assert.deepEqual(JSON.parse(JSON.stringify(frame.toPixel(64, 72))), { x: 32, y: 36, inside: true });

  env.context.__gosx_dispose_engine("gosx-engine-pixel");
  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(env.context.__gosx_engine_frame("gosx-engine-pixel"), null);
  assert.deepEqual(env.engineDisposals, ["gosx-engine-pixel"]);
  assert.equal(mount.getAttribute("data-gosx-pixel-surface-mounted"), null);
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0], fallback);
  assert.equal(mount.children[0].textContent, "server fallback");
});

test("bootstrap restores server fallback when pixel-surface engine mount fails", async () => {
  const mount = new FakeElement("div", null);
  const fallback = new FakeElement("p", null);
  fallback.textContent = "loading";
  mount.id = "broken-pixel-root";
  mount.width = 320;
  mount.height = 288;
  mount.appendChild(fallback);

  const env = createContext({
    elements: [mount],
    engineFactories: {
      BrokenPixel(ctx) {
        const frame = ctx.runtime.frame();
        frame.present();
        throw new Error("boom");
      },
    },
    manifest: {
      engines: [
        {
          id: "gosx-engine-broken-pixel",
          component: "BrokenPixel",
          kind: "surface",
          mountId: "broken-pixel-root",
          capabilities: ["pixel-surface", "canvas"],
          pixelSurface: {
            width: 160,
            height: 144,
            scaling: "fill",
            vsync: false,
          },
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(env.context.__gosx.engines.size, 0);
  assert.equal(env.context.__gosx_engine_frame("gosx-engine-broken-pixel"), null);
  assert.equal(mount.getAttribute("data-gosx-runtime-state"), "error");
  assert.equal(mount.getAttribute("data-gosx-runtime-issue"), "mount");
  assert.equal(mount.getAttribute("data-gosx-fallback-active"), "server");
  assert.equal(mount.getAttribute("data-gosx-pixel-surface-mounted"), null);
  assert.equal(mount.children.length, 1);
  assert.equal(mount.children[0], fallback);
  assert.equal(mount.children[0].textContent, "loading");
  const issues = env.context.__gosx.listIssues();
  assert.equal(issues.some((issue) => issue.scope === "engine" && issue.type === "mount" && issue.source === "gosx-engine-broken-pixel"), true);
});

test("bootstrap batches keyboard and pointer input for capable engines", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "input-root";

  const env = createContext({
    elements: [mount],
    engineFactories: {
      InputSurface() {
        return {};
      },
    },
    fetchRoutes: {
      "/runtime.wasm": { bytes: [0, 97, 115, 109] },
    },
    manifest: {
      runtime: { path: "/runtime.wasm" },
      engines: [
        {
          id: "gosx-engine-input",
          component: "InputSurface",
          kind: "surface",
          mountId: "input-root",
          props: {},
          capabilities: ["pointer", "keyboard"],
        },
      ],
    },
  });

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  env.document.dispatchEvent({ type: "keydown", key: "W" });
  env.document.dispatchEvent({
    type: "pointermove",
    clientX: 40,
    clientY: 25,
    movementX: 3,
    movementY: -2,
    buttons: 1,
  });
  await flushAsyncWork();

  assert.equal(env.inputBatchCalls.length > 0, true);
  const firstBatch = JSON.parse(env.inputBatchCalls[0][0]);
  assert.equal(firstBatch["$input.key.w"], true);
  assert.equal(firstBatch["$input.pointer.x"], 40);
  assert.equal(firstBatch["$input.pointer.y"], 25);
  assert.equal(firstBatch["$input.pointer.deltaX"], 3);
  assert.equal(firstBatch["$input.pointer.deltaY"], -2);
  assert.equal(firstBatch["$input.pointer.buttons"], 1);

  env.context.dispatchEvent({ type: "blur" });
  await flushAsyncWork();

  const lastBatch = JSON.parse(env.inputBatchCalls[env.inputBatchCalls.length - 1][0]);
  assert.equal(lastBatch["$input.key.w"], false);
  assert.equal(lastBatch["$input.pointer.buttons"], 0);

  env.context.__gosx_dispose_engine("gosx-engine-input");
  assert.equal(env.document.eventListeners.get("keydown").length, 1, "framework declarative-action listener remains shared");
  assert.equal(env.document.eventListeners.get("pointermove").length, 0);
});
