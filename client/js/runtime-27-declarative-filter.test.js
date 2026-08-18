"use strict";
// Declarative list filter (data-gosx-filter, gosx#215): the debounced,
// case-insensitive substring match; the focus and pointer guards that keep
// the runtime from hiding a row mid-interaction; the "N of M shown"
// announcement hook; and the rescan lifecycle that re-applies an
// already-typed query after a soft navigation swap.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  navigationSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  installManualTimers,
  buildNavigatedDocument,
} = require("./runtime-test-harness.js");

const FILTER_DEBOUNCE_MS = 150;

function buildFilterFixture(rowTexts, options) {
  const opts = options || {};
  const targetId = opts.targetId || "pool-list";
  const input = new FakeElement("input", null);
  input.setAttribute("data-gosx-filter", opts.filterTarget !== undefined ? opts.filterTarget : targetId);
  if (opts.inputId) {
    input.id = opts.inputId;
  }
  if (opts.announce) {
    input.setAttribute("data-gosx-filter-announce", "true");
  }
  const container = new FakeElement("ul", null);
  container.id = targetId;
  const rows = rowTexts.map(function(text) {
    const row = new FakeElement("li", null);
    row.setAttribute("data-gosx-filter-text", text);
    row.textContent = text;
    container.appendChild(row);
    return row;
  });
  return { input, container, rows };
}

function isHidden(row) {
  return String(row.getAttribute("class") || "").split(/\s+/).includes("gosx-filter-row--hidden");
}

function dispatchInput(env, input, value) {
  input.value = value;
  env.document.dispatchEvent({ type: "input", target: input });
}

function lastAnnouncement(env) {
  const region = env.document.querySelector("[data-gosx-announcer]");
  return region ? region.textContent : "";
}

test("declarative filter hides rows failing a case-insensitive substring match, after the debounce window", () => {
  const { input, rows } = buildFilterFixture(["Patrick Mahomes", "Josh Allen", "Joe Burrow"]);
  const env = createContext({ elements: [input, rows[0].parentNode] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  dispatchInput(env, input, "Jo");
  assert.equal(isHidden(rows[0]), false, "nothing is hidden before the debounce timer fires");
  assert.equal(isHidden(rows[1]), false);

  assert.equal(timers.runDelay(FILTER_DEBOUNCE_MS), 1, "exactly one debounce timer is pending");

  assert.equal(isHidden(rows[0]), true, "\"Patrick Mahomes\" does not contain \"jo\"");
  assert.equal(isHidden(rows[1]), false, "\"Josh Allen\" contains \"jo\" case-insensitively");
  assert.equal(isHidden(rows[2]), false, "\"Joe Burrow\" contains \"jo\"");
});

test("declarative filter debounces rapid keystrokes into a single apply of only the latest value", () => {
  const { input, rows } = buildFilterFixture(["Patrick Mahomes", "Josh Allen"]);
  const env = createContext({ elements: [input, rows[0].parentNode] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  dispatchInput(env, input, "j");
  dispatchInput(env, input, "jo");
  dispatchInput(env, input, "josh");

  // Each keystroke replaces the pending timer rather than stacking a new
  // one alongside it.
  assert.equal(timers.count(), 1);
  timers.runDelay(FILTER_DEBOUNCE_MS);

  assert.equal(isHidden(rows[0]), true, "\"Patrick Mahomes\" does not contain \"josh\"");
  assert.equal(isHidden(rows[1]), false, "\"Josh Allen\" contains \"josh\"");
});

test("declarative filter shows every row again once the input is cleared", () => {
  const { input, rows } = buildFilterFixture(["Patrick Mahomes", "Josh Allen"]);
  const env = createContext({ elements: [input, rows[0].parentNode] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  dispatchInput(env, input, "josh");
  timers.runDelay(FILTER_DEBOUNCE_MS);
  assert.equal(isHidden(rows[0]), true);

  dispatchInput(env, input, "   ");
  timers.runDelay(FILTER_DEBOUNCE_MS);
  assert.equal(isHidden(rows[0]), false, "a blank (whitespace-only) query matches every row");
  assert.equal(isHidden(rows[1]), false);
});

test("declarative filter never hides a row containing the focused control", () => {
  const { input, rows } = buildFilterFixture(["Patrick Mahomes", "Josh Allen"]);
  const rowInput = new FakeElement("input", null);
  rows[0].appendChild(rowInput);
  const env = createContext({ elements: [input, rows[0].parentNode] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  env.document.activeElement = rowInput;

  dispatchInput(env, input, "josh");
  timers.runDelay(FILTER_DEBOUNCE_MS);

  assert.equal(isHidden(rows[0]), false, "the row holding the focused control is left alone");
  assert.equal(isHidden(rows[1]), false, "\"Josh Allen\" matches anyway");
});

test("declarative filter never hides a row currently under the pointer, and catches up once it leaves", () => {
  const { input, rows } = buildFilterFixture(["Patrick Mahomes", "Josh Allen"]);
  const env = createContext({ elements: [input, rows[0].parentNode] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  env.document.dispatchEvent({ type: "mouseover", target: rows[0] });
  dispatchInput(env, input, "josh");
  timers.runDelay(FILTER_DEBOUNCE_MS);
  assert.equal(isHidden(rows[0]), false, "the row under the pointer is left alone even though it does not match");

  // The pointer leaves the row entirely (no relatedTarget row) — the guard
  // clears, but only the NEXT apply (another keystroke here) hides it; the
  // runtime does not force a fresh apply the instant the pointer leaves.
  env.document.dispatchEvent({ type: "mouseout", target: rows[0], relatedTarget: null });
  assert.equal(isHidden(rows[0]), false, "leaving the row alone does not itself trigger a new apply");

  dispatchInput(env, input, "josh");
  timers.runDelay(FILTER_DEBOUNCE_MS);
  assert.equal(isHidden(rows[0]), true, "the next apply hides it now that the guard has cleared");
});

test("declarative filter moving the pointer between two descendants of the same row keeps it guarded", () => {
  const { input, rows } = buildFilterFixture(["Patrick Mahomes", "Josh Allen"]);
  const child = new FakeElement("span", null);
  rows[0].appendChild(child);
  const env = createContext({ elements: [input, rows[0].parentNode] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  env.document.dispatchEvent({ type: "mouseover", target: rows[0] });
  // Moving from the row itself onto its own child fires mouseout on the
  // row with relatedTarget the child — still inside the same row.
  env.document.dispatchEvent({ type: "mouseout", target: rows[0], relatedTarget: child });
  env.document.dispatchEvent({ type: "mouseover", target: child });

  dispatchInput(env, input, "josh");
  timers.runDelay(FILTER_DEBOUNCE_MS);
  assert.equal(isHidden(rows[0]), false, "the pointer never actually left the row");
});

test("declarative filter announces \"N of M shown\" only when data-gosx-filter-announce is set", async () => {
  const { input, rows } = buildFilterFixture(["Patrick Mahomes", "Josh Allen", "Joe Burrow"], { announce: true });
  const env = createContext({ elements: [input, rows[0].parentNode] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  dispatchInput(env, input, "jo");
  timers.runDelay(FILTER_DEBOUNCE_MS);
  await flushAsyncWork();

  assert.equal(lastAnnouncement(env), "2 of 3 shown");
});

test("declarative filter logs one warning and stays disabled for a target matching no element", () => {
  const { input, rows } = buildFilterFixture(["Patrick Mahomes"], { filterTarget: "no-such-list" });
  const env = createContext({ elements: [input, rows[0].parentNode] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  dispatchInput(env, input, "mahomes");
  timers.runDelay(FILTER_DEBOUNCE_MS);

  assert.equal(env.consoleLogs.warn.length, 1);
  assert.match(env.consoleLogs.warn[0], /data-gosx-filter/);
  assert.equal(isHidden(rows[0]), false, "a disabled filter never touches row visibility");
});

test("declarative filter falls back to a CSS selector when the target value matches no element id", () => {
  const input = new FakeElement("input", null);
  input.setAttribute("data-gosx-filter", ".draft-pool");
  const container = new FakeElement("ul", null);
  container.setAttribute("class", "draft-pool");
  const rows = ["Patrick Mahomes", "Josh Allen"].map(function(text) {
    const row = new FakeElement("li", null);
    row.setAttribute("data-gosx-filter-text", text);
    container.appendChild(row);
    return row;
  });
  const env = createContext({ elements: [input, container] });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  dispatchInput(env, input, "josh");
  timers.runDelay(FILTER_DEBOUNCE_MS);

  assert.equal(isHidden(rows[0]), true);
  assert.equal(isHidden(rows[1]), false);
  assert.equal(env.consoleLogs.warn.length, 0);
});

test("declarative filter re-applies its remembered query, and restores the input's value, after a revalidation swap", async () => {
  const url = "http://localhost:3000/draft-pool";
  const nextURL = "http://localhost:3000/draft-pool-refresh";
  const { input, rows } = buildFilterFixture(["Patrick Mahomes", "Josh Allen"], { inputId: "pool-search" });
  const parsedDocs = new Map();
  const env = createContext({
    elements: [input, rows[0].parentNode],
    fetchRoutes: {
      [nextURL]: { text: "__POOL_REFRESH__", url: nextURL },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.location.href = url;
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  dispatchInput(env, input, "josh");
  timers.runDelay(FILTER_DEBOUNCE_MS);
  assert.equal(isHidden(rows[0]), true);

  // The refreshed document renders a fresh, empty-valued input carrying the
  // same id, and three fresh rows the visitor never typed against.
  const freshInput = new FakeElement("input", null);
  freshInput.setAttribute("data-gosx-filter", "pool-list");
  freshInput.id = "pool-search";
  const freshContainer = new FakeElement("ul", null);
  freshContainer.id = "pool-list";
  const freshRows = ["Patrick Mahomes", "Josh Allen", "Joshua Dobbs"].map(function(text) {
    const row = new FakeElement("li", null);
    row.setAttribute("data-gosx-filter-text", text);
    freshContainer.appendChild(row);
    return row;
  });
  parsedDocs.set("__POOL_REFRESH__", buildNavigatedDocument({
    title: "Draft pool",
    bodyNodes: [freshInput, freshContainer],
  }));

  assert.equal(await env.context.__gosx.navigation.navigate(nextURL, { replace: false }), true);
  await flushAsyncWork();

  const swappedInput = env.document.getElementById("pool-search");
  assert.equal(swappedInput.value, "josh", "the visitor's typed query survives the swap");

  const swappedContainer = env.document.getElementById("pool-list");
  const swappedRows = swappedContainer.children;
  assert.equal(isHidden(swappedRows[0]), true, "\"Patrick Mahomes\" still fails \"josh\" on the fresh rows");
  assert.equal(isHidden(swappedRows[1]), false, "\"Josh Allen\" still matches");
  assert.equal(isHidden(swappedRows[2]), false, "\"Joshua Dobbs\" matches too, and is new since the swap");
});
