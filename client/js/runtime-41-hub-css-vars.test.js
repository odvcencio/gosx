"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { bootstrapRuntimeSource, createContext, runScript, flushAsyncWork, installManualTimers, installManualRAF, FakeElement } = require("./runtime-test-harness.js");

const chunkSource = (name) => fs.readFileSync(path.join(__dirname, `bootstrap-feature-${name}.js`), "utf8");
const makeSocket = (url) => ({ url, readyState: 1, onmessage: null, onclose: null, onopen: null, onerror: null, close() { this.readyState = 3; } });

// The harness FakeElement has style = {} and no classList; stub both per test (do not edit the harness).
function stubStyleAndClasses(element) {
  const props = new Map(); const classes = new Set();
  element.style = { setProperty: (n, v) => props.set(n, String(v)), getPropertyValue: (n) => props.get(n) || "", removeProperty: (n) => props.delete(n) };
  element.classList = { add: (c) => classes.add(c), remove: (c) => classes.delete(c), contains: (c) => classes.has(c), get length() { return classes.size; } };
  return element;
}
function manifest(cssVars) {
  return { features: ["hub-css-vars"], hubs: [{ id: "gosx-hub-0", name: "palette", path: "/palette/ws", bindings: [{ event: "tick", cssVars }] }] };
}
const vars = [
  { name: "--core", path: "coreInner" },
  { name: "--chrome-core", path: "coreInner" },
  { name: "--activity", path: "activity", digits: 3 },
  { name: "--hue", path: "hue", unit: "deg" },
];
async function boot(cssVars, options = {}, extraElements = []) {
  const contract = new FakeElement("script", null); contract.id = "gosx-document";
  contract.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true } });
  const env = createContext(Object.assign({
    elements: [contract].concat(extraElements),
    createWebSocket: makeSocket,
    fetchRoutes: {
      "/gosx/bootstrap-feature-hubs.js": { text: chunkSource("hubs") },
      "/gosx/bootstrap-feature-hub-css-vars.js": { text: chunkSource("hub-css-vars") },
    },
    manifest: manifest(cssVars),
  }, options.context || {}));
  if (options.search) env.context.location.search = options.search; // the harness location has no search; stub it
  const root = stubStyleAndClasses(env.context.document.documentElement);
  const timers = installManualTimers(env.context);
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  // Chunk fetches resolve through promise chains and zero-delay timers; drive both until the hub socket exists.
  for (let i = 0; i < 10 && env.sockets.length === 0; i++) { await flushAsyncWork(); timers.runDelay(0); }
  assert.equal(env.sockets.length, 1, "hubs chunk connected");
  const send = (data) => { env.sockets[0].onmessage({ data: JSON.stringify({ event: "tick", data }) }); };
  return { env, timers, raf, send, root };
}

test("writes mapped payload fields to :root custom properties, numbers rounded with a unit", async () => {
  const h = await boot({ root: "state", vars, flashClass: "arrival", flashMs: 300, liveClass: "live" });
  h.send({ frame: 1, state: { coreInner: "#ff8800", activity: 0.123456, hue: 12.5, ignored: { nested: true } } });
  assert.equal(h.root.style.getPropertyValue("--core"), "#ff8800");
  assert.equal(h.root.style.getPropertyValue("--chrome-core"), "#ff8800", "two names may read one path");
  assert.equal(h.root.style.getPropertyValue("--activity"), "0.123");
  assert.equal(h.root.style.getPropertyValue("--hue"), "12.5deg");
  assert.equal(h.root.classList.contains("arrival"), true, "flash class added after a changed apply");
  h.raf.flush();
  assert.equal(h.root.classList.contains("live"), true, "live class added once after the first apply");
  h.timers.runDelay(300);
  assert.equal(h.root.classList.contains("arrival"), false, "flash class removed after flashMs");
});

test("skips missing and non-scalar values and does not flash when nothing changed", async () => {
  const h = await boot({ root: "state", vars, flashClass: "arrival" });
  h.send({ state: { coreInner: "#111111" } });
  h.timers.runDelay(1000);
  h.send({ state: { coreInner: "#111111", activity: null, hue: "x" } });
  assert.equal(h.root.style.getPropertyValue("--core"), "#111111");
  assert.equal(h.root.style.getPropertyValue("--activity"), "");
  assert.equal(h.root.style.getPropertyValue("--hue"), "x", "strings pass through without the unit");
  assert.equal(h.root.classList.contains("arrival"), true, "the --hue change flashed");
});

test("flashWhen flashes only when the named value changes", async () => {
  const h = await boot({ root: "state", vars, flashClass: "arrival", flashWhen: "frame" });
  h.send({ frame: 3, state: { coreInner: "#1" } });
  assert.equal(h.root.classList.contains("arrival"), true);
  h.timers.runDelay(1000);
  h.send({ frame: 3, state: { coreInner: "#2" } });
  assert.equal(h.root.classList.contains("arrival"), false, "same frame: vars update, no flash");
});

for (const [label, options] of [
  ["reduced motion", { context: { prefersReducedMotion: true } }],
  ["hidden tab", { context: { visibilityState: "hidden" } }],
  ["pinned visual seed", { search: "?__gosx_visual_seed=night" }],
]) {
  test("no flash class under " + label, async () => {
    const h = await boot({ root: "state", vars, flashClass: "arrival" }, options);
    h.send({ state: { coreInner: "#abc" } });
    assert.equal(h.root.style.getPropertyValue("--core"), "#abc", "vars still apply");
    assert.equal(h.root.classList.contains("arrival"), false);
  });
}

test("a target selector other than :root, and a payload for another event, are handled; dispose keeps the values", async () => {
  const card = stubStyleAndClasses(new FakeElement("div", null)); card.id = "card";
  const h = await boot({ target: "#card", vars: [{ name: "--core", path: "coreInner" }], flashClass: "arrival" }, {}, [card]);
  h.env.sockets[0].onmessage({ data: JSON.stringify({ event: "other", data: { coreInner: "#no" } }) });
  assert.equal(card.style.getPropertyValue("--core"), "");
  h.send({ coreInner: "#yes" });
  assert.equal(card.style.getPropertyValue("--core"), "#yes");
  assert.equal(card.classList.contains("arrival"), true);
  await h.env.context.__gosx_dispose_page();
  assert.equal(card.style.getPropertyValue("--core"), "#yes", "dispose keeps the values");
  assert.equal(card.classList.contains("arrival"), false, "dispose clears the flash class and its timer");
});

const hostile = [
  ["url()", "url(https://evil.example/beacon)"],
  ["image-set()", 'image-set("https://evil.example/a.png" 1x)'],
  ["escaped url", "u\\72l(https://evil.example/beacon)"],
  ["escaped url without parenthesis", "u\\72l"],
  ["quote", 'a"b'],
  ["single quote", "a'b"],
  ["backtick", "a`b"],
  ["semicolon", "red;background:red"],
  ["control character", "red\u0007"],
  ["newline", "red\nblue"],
  ["200 KB string", "a".repeat(200 * 1024)],
  ["257 characters", "a".repeat(257)],
];

for (const [label, value] of hostile) {
  test("plain kind rejects " + label, async () => {
    const h = await boot({ vars: [{ name: "--v", path: "v" }] });
    h.send({ v: value });
    assert.equal(h.root.style.getPropertyValue("--v"), "");
  });
}

test("every kind accepts its grammar and rejects hostile values", async () => {
  const kinds = [
    { name: "--n", path: "n", kind: "number" },
    { name: "--l", path: "l", kind: "length" },
    { name: "--c", path: "c", kind: "color" },
    { name: "--i", path: "i", kind: "ident" },
    { name: "--s", path: "s", kind: "string" },
  ];
  const good = await boot({ vars: kinds });
  good.send({ n: 12.5, l: "12px", c: "rgb(1, 2, 3)", i: "slow-fade", s: "Hello" });
  assert.equal(good.root.style.getPropertyValue("--n"), "12.5");
  assert.equal(good.root.style.getPropertyValue("--l"), "12px");
  assert.equal(good.root.style.getPropertyValue("--c"), "rgb(1, 2, 3)");
  assert.equal(good.root.style.getPropertyValue("--i"), "slow-fade");
  assert.equal(good.root.style.getPropertyValue("--s"), '"Hello"');
  const bad = await boot({ vars: kinds });
  bad.send({ n: "url(x)", l: "1px url(x)", c: "url(https://evil.example/x)", i: "u\\72l", s: 'x"; background: url(y)' });
  for (const name of ["--n", "--l", "--c", "--i", "--s"]) assert.equal(bad.root.style.getPropertyValue(name), "", name);
  const nested = await boot({ vars: kinds });
  nested.send({ c: "rgb(url(x))" });
  assert.equal(nested.root.style.getPropertyValue("--c"), "");
});

test("the client re-checks the -- prefix, paths and kinds from the manifest", async () => {
  const h = await boot({ vars: [
    { name: "background", path: "v" },
    { name: "--p", path: "a[0]" },
    { name: "--k", path: "v", kind: "url" },
    { name: "--ok", path: "v" },
  ] });
  h.send({ v: "red", a: ["x"] });
  assert.equal(h.root.style.getPropertyValue("background"), "");
  assert.equal(h.root.style.getPropertyValue("--p"), "");
  assert.equal(h.root.style.getPropertyValue("--k"), "");
  assert.equal(h.root.style.getPropertyValue("--ok"), "red");
});

test("dispose removes the live class", async () => {
  const h = await boot({ vars: [{ name: "--v", path: "v" }], liveClass: "live" });
  h.send({ v: "red" });
  h.raf.flush();
  assert.equal(h.root.classList.contains("live"), true);
  await h.env.context.__gosx_dispose_page();
  assert.equal(h.root.classList.contains("live"), false);
});
