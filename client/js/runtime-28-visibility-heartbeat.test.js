"use strict";
// Visibility-aware heartbeat ping (data-gosx-heartbeat, gosx#216): the
// same-origin GET on an interval, paused while the document is hidden, a
// catch-up ping on visibility return, and never more than one ping in
// flight at a time. The lifecycle deliberately mirrors periodic
// revalidation's own tests in runtime-14-navigation.test.js.

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

test("declarative heartbeat pings its endpoint on the declared interval while the document is visible", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const env = createContext({
    elements: [],
    fetchRoutes: { [pingURL]: { text: "ok" } },
  });
  // The "or body" half of the contract: the attributes live directly on
  // document.body itself, not on a descendant.
  env.document.body.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  env.document.body.setAttribute("data-gosx-heartbeat-interval", "30s");

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  timers.runInterval(30000);
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.fetchCalls[0].url, pingURL);
  assert.equal(env.fetchCalls[0].init.method, "GET");
  assert.equal(env.fetchCalls[0].init.credentials, "same-origin");
});

test("declarative heartbeat pings an element other than body the same way", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "10s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "ok" } },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  timers.runInterval(10000);
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.fetchCalls[0].url, pingURL);
});

test("declarative heartbeat skips a tick while the document is hidden", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "30s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "ok" } },
    visibilityState: "hidden",
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  timers.runInterval(30000);
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 0, "a hidden document must skip the tick entirely");
});

test("declarative heartbeat runs one immediate catch-up ping when a full interval elapsed while hidden", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "30s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "ok" } },
  });

  const clock = installManualClock(env.context, 0);
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });

  clock.advance(30000);
  env.document.visibilityState = "visible";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1, "one full interval elapsed while hidden, so visibility recovery pings immediately");

  // A second visible event without an intervening hidden period must not
  // fire another catch-up ping.
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1, "the catch-up ping fires at most once per hidden period");
});

test("declarative heartbeat skips the catch-up ping when less than a full interval elapsed while hidden", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "30s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "ok" } },
  });

  const clock = installManualClock(env.context, 0);
  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });

  clock.advance(10000);
  env.document.visibilityState = "visible";
  env.document.dispatchEvent({ type: "visibilitychange" });
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 0, "under one interval elapsed while hidden, so no catch-up ping runs");
});

test("declarative heartbeat never starts a second overlapping ping while the previous one is in flight", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "5s");
  let pingCalls = 0;
  let resolvePing;
  const env = createContext({
    elements: [main],
    fetchRoutes: {
      [pingURL]: () => {
        pingCalls += 1;
        return new Promise((resolve) => { resolvePing = resolve; });
      },
    },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  timers.runInterval(5000);
  await flushAsyncWork();
  assert.equal(pingCalls, 1, "the first tick starts a ping this test holds open");

  timers.runInterval(5000);
  await flushAsyncWork();
  assert.equal(pingCalls, 1, "a second tick must not start an overlapping ping while the first is in flight");

  resolvePing({ text: "ok" });
  await flushAsyncWork();

  timers.runInterval(5000);
  await flushAsyncWork();
  assert.equal(pingCalls, 2, "once the first ping settles, the next tick may start a new one");
});

test("declarative heartbeat is silent on a fetch rejection and keeps ticking on the next interval", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "5s");
  let pingCalls = 0;
  const env = createContext({
    elements: [main],
    fetchRoutes: {
      [pingURL]: () => {
        pingCalls += 1;
        return Promise.reject(new Error("network down"));
      },
    },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  timers.runInterval(5000);
  await flushAsyncWork();
  assert.equal(pingCalls, 1);
  assert.equal(env.consoleLogs.error.length, 0, "a dropped ping must never surface a console error");

  timers.runInterval(5000);
  await flushAsyncWork();
  assert.equal(pingCalls, 2, "a failed ping does not wedge the in-flight guard open");
});

test("declarative heartbeat is silent on a non-2xx response", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "5s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "not found", status: 404, ok: false } },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  timers.runInterval(5000);
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.consoleLogs.error.length, 0);
  assert.equal(env.consoleLogs.warn.length, 0);
});

test("declarative heartbeat logs one warning and stays disabled for a cross-origin endpoint", () => {
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "https://elsewhere.example/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "5s");
  const env = createContext({ elements: [main] });
  const timers = installManualTimers(env.context);

  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(timers.count(), 0);
  assert.equal(env.consoleLogs.warn.length, 1);
  assert.match(env.consoleLogs.warn[0], /data-gosx-heartbeat/);
});

test("declarative heartbeat logs one warning and stays disabled for an invalid interval", () => {
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "not-a-duration");
  const env = createContext({ elements: [main] });
  const timers = installManualTimers(env.context);

  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(timers.count(), 0);
  assert.equal(env.consoleLogs.warn.length, 1);
  assert.match(env.consoleLogs.warn[0], /data-gosx-heartbeat-interval/);
});

test("declarative heartbeat tears down its interval when a soft navigation lands on a page without the attribute", async () => {
  const url = "http://localhost:3000/scoreboard";
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.id = "scoreboard";
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "5s");
  const parsedDocs = new Map();
  const env = createContext({
    elements: [main],
    fetchRoutes: {
      [pingURL]: { text: "ok" },
      [url]: { text: "__PLAIN_PAGE__", url },
    },
    parseHTML(html) { return parsedDocs.get(html); },
  });
  env.context.__gosx_dispose_page = async function() {};
  env.context.__gosx_bootstrap_page = async function() {};
  const plainMain = new FakeElement("main", null);
  plainMain.id = "plain-page";
  parsedDocs.set("__PLAIN_PAGE__", buildNavigatedDocument({
    title: "Plain page",
    bodyNodes: [plainMain],
  }));

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  assert.equal(timers.count(), 1);

  assert.equal(await env.context.__gosx.navigation.navigate(url, { replace: false }), true);
  await flushAsyncWork();

  assert.equal(timers.count(), 0, "the new page carries no heartbeat attribute, so the timer must be cleared");
});
