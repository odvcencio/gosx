"use strict";
// Live-bound regions (data-gosx-live-*, gosx#217): the text-binding half of
// the last primitive blocking downstream deletion of bespoke score/wire
// polling JS. Region fragment refresh (the other half) extends the
// pre-existing data-gosx-region system — see
// client/js/07-declarative-regions.test.mjs.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  navigationSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  installManualTimers,
  installManualClock,
  buildNavigatedDocument,
} = require("./runtime-test-harness.js");

function buildLiveRegion(options) {
  const opts = options || {};
  const root = new FakeElement("div", null);
  root.setAttribute("data-gosx-live-src", opts.src || "/api/live/week");
  root.setAttribute("data-gosx-live-interval", opts.interval || "10s");
  const score = new FakeElement("span", null);
  score.setAttribute("data-gosx-live-bind", opts.bindKey || "score:t42");
  if (opts.flashClass) score.setAttribute("data-gosx-live-flash-class", opts.flashClass);
  score.textContent = opts.initialText !== undefined ? opts.initialText : "0.0";
  root.appendChild(score);
  return { root, score };
}

test("a live region applies its first tick immediately, without waiting a full interval", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url });
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [url]: { text: JSON.stringify({ "score:t42": 17.4 }) },
    },
  });

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(score.textContent, "17.4");
  assert.equal(env.fetchCalls.filter((call) => call.url === url).length, 1);
});

test("a live region reapplies on its own interval and skips a write when the value is unchanged", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "10s" });
  let payload = { "score:t42": 17.4 };
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [url]: () => ({ text: JSON.stringify(payload) }),
    },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  assert.equal(score.textContent, "17.4");
  assert.equal(timers.count(), 1);

  // Same value: the body-diff short-circuit skips the DOM write entirely,
  // but the element's text is already correct either way.
  timers.runInterval(10000);
  await flushAsyncWork();
  assert.equal(score.textContent, "17.4");

  payload = { "score:t42": 20.1 };
  timers.runInterval(10000);
  await flushAsyncWork();
  assert.equal(score.textContent, "20.1");

  assert.equal(env.fetchCalls.filter((call) => call.url === url).length, 3);
});

test("a live-bind flash class retriggers only when the resolved text actually changes", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, flashClass: "score-flash", initialText: "17.4" });
  let payload = { "score:t42": 17.4 };
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [url]: () => ({ text: JSON.stringify(payload) }),
    },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  // Server-rendered text already matched the first poll: no flash.
  assert.equal(score.getAttribute("class"), null);

  payload = { "score:t42": 24.5 };
  timers.runInterval(10000);
  await flushAsyncWork();
  assert.equal(score.textContent, "24.5");
  assert.equal(score.getAttribute("class"), "score-flash");
});

test("a live-bind key resolves one level of nested-object dot chain", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, bindKey: "status.mode" });
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [url]: { text: JSON.stringify({ status: { mode: "LIVE" } }) },
    },
  });

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(score.textContent, "LIVE");
});

test("a live-bind key missing from the payload, or resolving to null/object/array, leaves existing text untouched", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, bindKey: "score:missing", initialText: "0.0" });
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [url]: { text: JSON.stringify({ "score:missing": null, other: { nested: true } }) },
    },
  });

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(score.textContent, "0.0");
});

test("many independent live regions poll on their own src and interval", async () => {
  const scoreURL = "http://localhost:3000/api/live/week";
  const rosterURL = "http://localhost:3000/api/roster/notes";
  const { root: scoreRoot, score } = buildLiveRegion({ src: scoreURL, interval: "10s" });
  const rosterRoot = new FakeElement("div", null);
  rosterRoot.setAttribute("data-gosx-live-src", rosterURL);
  rosterRoot.setAttribute("data-gosx-live-interval", "30s");
  const note = new FakeElement("span", null);
  note.setAttribute("data-gosx-live-bind", "note");
  note.textContent = "";
  rosterRoot.appendChild(note);

  const env = createContext({
    elements: [scoreRoot, rosterRoot],
    fetchRoutes: {
      [scoreURL]: { text: JSON.stringify({ "score:t42": 3.5 }) },
      [rosterURL]: { text: JSON.stringify({ note: "Questionable" }) },
    },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(score.textContent, "3.5");
  assert.equal(note.textContent, "Questionable");
  assert.equal(timers.count(), 2, "each live region owns its own timer");
});

test("a live region skips a tick entirely while the document is hidden", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root } = buildLiveRegion({ src: url });
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: { text: JSON.stringify({ "score:t42": 9.9 }) } },
  });
  env.document.visibilityState = "hidden";

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 0);
});

test("a live region skips a tick while an input, textarea, or select inside it has focus", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root } = buildLiveRegion({ src: url });
  const input = new FakeElement("input", null);
  root.appendChild(input);
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: { text: JSON.stringify({ "score:t42": 9.9 }) } },
  });
  env.document.activeElement = input;

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 0, "a focused control inside the region blocks the tick");
});

test("a live region skips a tick while a pointer is held down inside it, and resumes after pointerup", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "10s" });
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: { text: JSON.stringify({ "score:t42": 9.9 }) } },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  // First tick already applied before the pointer goes down.
  assert.equal(score.textContent, "9.9");
  const beforeCount = env.fetchCalls.length;

  env.document.dispatchEvent({ type: "pointerdown", target: score });
  timers.runInterval(10000);
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, beforeCount, "an active pointer inside the region blocks the tick");

  env.document.dispatchEvent({ type: "pointerup", target: score });
  timers.runInterval(10000);
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, beforeCount + 1, "the tick resumes once the pointer releases");
});

test("a live region does not skip while an unrelated part of the page is focused or pointed at", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url });
  const sibling = new FakeElement("input", null);
  const env = createContext({
    elements: [root, sibling],
    fetchRoutes: { [url]: { text: JSON.stringify({ "score:t42": 9.9 }) } },
  });
  env.document.activeElement = sibling;

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(score.textContent, "9.9");
});

test("a live region sends If-None-Match once a response carries an ETag, and a 304 skips reapplying", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "10s" });
  let calls = 0;
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [url]: () => {
        calls += 1;
        if (calls === 1) {
          return { text: JSON.stringify({ "score:t42": 9.9 }), headers: { ETag: '"v1"' } };
        }
        return { status: 304, text: "" };
      },
    },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  assert.equal(score.textContent, "9.9");

  timers.runInterval(10000);
  await flushAsyncWork();

  assert.equal(calls, 2);
  const secondRequest = env.fetchCalls[1];
  assert.equal(secondRequest.init.headers["If-None-Match"], '"v1"');
  assert.equal(score.textContent, "9.9", "a 304 leaves the previously-applied text untouched");
});

test("a live region never starts a second fetch while one is already in flight", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root } = buildLiveRegion({ src: url, interval: "10s" });
  let resolveFetch;
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [url]: () => new Promise((resolve) => { resolveFetch = resolve; }),
    },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await Promise.resolve();
  assert.equal(env.fetchCalls.length, 1, "the immediate first tick started one fetch");

  timers.runInterval(10000);
  await Promise.resolve();
  assert.equal(env.fetchCalls.length, 1, "an interval tick during an in-flight fetch starts no second one");

  resolveFetch({ text: JSON.stringify({ "score:t42": 1.0 }) });
  await flushAsyncWork();
});

test("a live region logs one warning and stays disabled without data-gosx-live-src", async () => {
  const root = new FakeElement("div", null);
  root.setAttribute("data-gosx-live-interval", "10s");
  const env = createContext({ elements: [root] });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(timers.count(), 0);
  assert.match(env.consoleLogs.warn[0], /data-gosx-live-src/);
});

test("a live region logs one warning and stays disabled with an invalid interval", async () => {
  const root = new FakeElement("div", null);
  root.setAttribute("data-gosx-live-src", "/api/live/week");
  root.setAttribute("data-gosx-live-interval", "1h");
  const env = createContext({ elements: [root] });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(timers.count(), 0);
  assert.match(env.consoleLogs.warn[0], /data-gosx-live-interval/);
});

test("a live region logs one warning and stays disabled for a cross-origin src", async () => {
  const root = new FakeElement("div", null);
  root.setAttribute("data-gosx-live-src", "https://other.example/api/live/week");
  root.setAttribute("data-gosx-live-interval", "10s");
  const env = createContext({ elements: [root] });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(timers.count(), 0);
  assert.match(env.consoleLogs.warn[0], /data-gosx-live-src/);
});

test("malformed JSON leaves bound text untouched and the next tick still tries", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "10s", initialText: "0.0" });
  let body = "not json";
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: () => ({ text: body }) },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  assert.equal(score.textContent, "0.0");

  body = JSON.stringify({ "score:t42": 12.5 });
  timers.runInterval(10000);
  await flushAsyncWork();
  assert.equal(score.textContent, "12.5");
});

test("a soft navigation tears down the old page's live region timer and reads the new page's own attributes", async () => {
  const url = "http://localhost:3000/scoreboard";
  const liveURL = "http://localhost:3000/api/live/week";
  const { root } = buildLiveRegion({ src: liveURL, interval: "10s" });
  const parsedDocs = new Map();
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [liveURL]: { text: JSON.stringify({ "score:t42": 1.0 }) },
      [url]: { text: "__NEXT_PAGE__", url },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  parsedDocs.set("__NEXT_PAGE__", buildNavigatedDocument({ title: "Next", bodyNodes: [new FakeElement("main", null)] }));

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  assert.equal(timers.count(), 1);

  assert.equal(await env.context.__gosx.navigation.navigate(url, { replace: false }), true);
  await flushAsyncWork();
  assert.equal(timers.count(), 0, "the new page carries no live-bound attribute, so the timer is cleared");
});

test("a hidden tab catches up with one tick once it becomes visible again, past a full interval", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "10s" });
  let payload = { "score:t42": 1.5 };
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: () => ({ text: JSON.stringify(payload) }) },
  });

  const clock = installManualClock(env.context, 0);
  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  assert.equal(score.textContent, "1.5", "the immediate first tick applies before any hidden period");

  payload = { "score:t42": 5.5 };
  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });

  clock.advance(10000);
  env.document.visibilityState = "visible";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(score.textContent, "5.5", "one full interval elapsed while hidden, so visibility recovery ran an immediate tick");
});

// --- manual refresh: signal and hub-event triggers (gosx#228) --------------

test("data-gosx-live-signal refreshes on a shared-signal change, needs no interval, and starts no timer", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "" });
  root.removeAttribute("data-gosx-live-interval");
  root.setAttribute("data-gosx-live-signal", "$syncTick");
  const subs = [];
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: () => ({ text: JSON.stringify({ "score:t42": 8.1 }) }) },
  });
  env.context.__gosx_subscribe_shared_signal = (name, fn, opts) => {
    subs.push({ name, fn, opts });
    return () => {};
  };

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(subs.length, 1);
  assert.equal(subs[0].name, "$syncTick");
  assert.equal(subs[0].opts.immediate, false);
  assert.equal(timers.count(), 0, "a signal-only live region polls never");
  assert.equal(env.fetchCalls.length, 0, "no interval means no immediate first tick either");

  subs[0].fn("1");
  await flushAsyncWork();
  assert.equal(score.textContent, "8.1");
  assert.equal(env.fetchCalls.length, 1);
});

test("data-gosx-live-signal still refreshes while the document is hidden — a signal change is a user action, not a background poll", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "" });
  root.removeAttribute("data-gosx-live-interval");
  root.setAttribute("data-gosx-live-signal", "$syncTick");
  const subs = [];
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: () => ({ text: JSON.stringify({ "score:t42": 3.3 }) }) },
  });
  env.context.__gosx_subscribe_shared_signal = (name, fn, opts) => {
    subs.push({ name, fn, opts });
    return () => {};
  };
  env.document.visibilityState = "hidden";

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  subs[0].fn("1");
  await flushAsyncWork();
  assert.equal(score.textContent, "3.3", "the signal trigger lands even though the document is hidden");
});

test("data-gosx-live-on refreshes on a matched gosx:hub:event and ignores an unmatched one", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "" });
  root.removeAttribute("data-gosx-live-interval");
  root.setAttribute("data-gosx-live-on", "sync-now, other-event");
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: () => ({ text: JSON.stringify({ "score:t42": 6.6 }) }) },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  assert.equal(timers.count(), 0, "an on-event-only live region polls never");

  env.document.dispatchEvent({ type: "gosx:hub:event", detail: { event: "unrelated" } });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 0, "an unmatched hub event name triggers nothing");

  env.document.dispatchEvent({ type: "gosx:hub:event", detail: { event: "sync-now" } });
  await flushAsyncWork();
  assert.equal(score.textContent, "6.6");
  assert.equal(env.fetchCalls.length, 1);
});

test("data-gosx-live-on still refreshes while the document is hidden, the same as data-gosx-live-signal", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "" });
  root.removeAttribute("data-gosx-live-interval");
  root.setAttribute("data-gosx-live-on", "sync-now");
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: () => ({ text: JSON.stringify({ "score:t42": 4.4 }) }) },
  });
  env.document.visibilityState = "hidden";

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  env.document.dispatchEvent({ type: "gosx:hub:event", detail: { event: "sync-now" } });
  await flushAsyncWork();
  assert.equal(score.textContent, "4.4", "the hub-event trigger lands even though the document is hidden");
});

test("a manual trigger never overlaps an in-flight fetch, and the interval poll shares the same guard", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "10s" });
  root.setAttribute("data-gosx-live-on", "sync-now");
  let resolveFetch;
  const env = createContext({
    elements: [root],
    fetchRoutes: {
      [url]: () => new Promise((resolve) => { resolveFetch = resolve; }),
    },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await Promise.resolve();
  assert.equal(env.fetchCalls.length, 1, "the interval trigger's own immediate first tick started one fetch");

  // A manual trigger that lands while that fetch is still in flight is
  // dropped outright — the existing fetch is left to finish and apply,
  // not restarted — and the interval tick behind it is skipped by the
  // same record.inFlight guard.
  env.document.dispatchEvent({ type: "gosx:hub:event", detail: { event: "sync-now" } });
  timers.runInterval(10000);
  await Promise.resolve();
  assert.equal(env.fetchCalls.length, 1, "no second fetch starts while the first is still in flight");

  resolveFetch({ text: JSON.stringify({ "score:t42": 2.2 }) });
  await flushAsyncWork();
  assert.equal(score.textContent, "2.2", "the one in-flight fetch still applies once it settles");
});

test("window.__gosx.live.refresh(element) triggers a fetch directly and resolves true; an unknown element resolves false", async () => {
  const url = "http://localhost:3000/api/live/week";
  const { root, score } = buildLiveRegion({ src: url, interval: "" });
  root.removeAttribute("data-gosx-live-interval");
  root.setAttribute("data-gosx-live-signal", "$unused");
  const env = createContext({
    elements: [root],
    fetchRoutes: { [url]: () => ({ text: JSON.stringify({ "score:t42": 5.1 }) }) },
  });
  env.context.__gosx_subscribe_shared_signal = () => () => {};

  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  const notARoot = new FakeElement("div", null);
  assert.equal(await env.context.__gosx.live.refresh(notARoot), false);

  assert.equal(await env.context.__gosx.live.refresh(root), true);
  await flushAsyncWork();
  assert.equal(score.textContent, "5.1");
  assert.equal(env.fetchCalls.length, 1);
});
