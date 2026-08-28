"use strict";
// Visibility-aware heartbeat ping (data-gosx-heartbeat, gosx#216): a
// same-origin GET on an interval while the document is visible, and a
// slower same-origin GET on a separate, configurable interval
// (data-gosx-heartbeat-hidden-interval) while the document is hidden, so a
// server can tell a backgrounded tab from a closed browser. A hidden ping
// carries the X-GoSX-Heartbeat-Visibility: hidden header; a visible ping
// carries no such header. Never more than one ping in flight at a time.
// The lifecycle deliberately mirrors periodic revalidation's own tests in
// runtime-14-navigation.test.js.

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
  assert.equal(
    env.fetchCalls[0].init.headers,
    undefined,
    "a visible tick must carry no hidden marker",
  );
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

test("declarative heartbeat does not fire a normal-interval tick while the document is hidden", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "4s");
  main.setAttribute("data-gosx-heartbeat-hidden-interval", "60s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "ok" } },
    visibilityState: "hidden",
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  // A hidden document must arm the hidden cadence, not the normal one — the
  // normal 4s delay has no interval registered against it at all.
  const fired = timers.runInterval(4000);
  await flushAsyncWork();

  assert.equal(fired, 0, "the normal interval must not be armed while hidden");
  assert.equal(env.fetchCalls.length, 0, "a hidden document must not tick at the normal interval");
});

test("declarative heartbeat fires at the hidden interval while the document is hidden, marked hidden", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "4s");
  main.setAttribute("data-gosx-heartbeat-hidden-interval", "60s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "ok" } },
    visibilityState: "hidden",
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  timers.runInterval(60000);
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1, "a hidden document still pings, at the slower hidden interval");
  assert.equal(env.fetchCalls[0].url, pingURL);
  assert.equal(env.fetchCalls[0].init.method, "GET");
  assert.equal(
    env.fetchCalls[0].init.headers && env.fetchCalls[0].init.headers["X-GoSX-Heartbeat-Visibility"],
    "hidden",
    "a hidden ping must carry the hidden marker header",
  );
});

test("declarative heartbeat defaults its hidden interval to 60s when the attribute is absent", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "4s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "ok" } },
    visibilityState: "hidden",
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  timers.runInterval(60000);
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1, "the hidden interval defaults to 60s with no attribute present");
});

test("declarative heartbeat resumes the normal interval promptly when the document becomes visible again", async () => {
  const pingURL = "http://localhost:3000/api/presence/ping";
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "4s");
  main.setAttribute("data-gosx-heartbeat-hidden-interval", "60s");
  const env = createContext({
    elements: [main],
    fetchRoutes: { [pingURL]: { text: "ok" } },
  });

  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  env.document.visibilityState = "hidden";
  env.document.dispatchEvent({ type: "visibilitychange" });

  // The hidden cadence is now armed; the normal 4s cadence must be torn
  // down, not merely left to finish counting down.
  assert.equal(timers.runInterval(4000), 0, "the normal interval must not survive a transition to hidden");

  env.document.visibilityState = "visible";
  env.document.dispatchEvent({ type: "visibilitychange" });

  // Visibility recovery must rearm the normal cadence immediately — a
  // browser throttles a hidden tab's timers independently of this
  // rearming, so the test never depends on the hidden timer firing on
  // schedule, only on the runtime's own rearm-on-visible behavior.
  const fired = timers.runInterval(4000);
  await flushAsyncWork();

  assert.equal(fired, 1, "visibility recovery must rearm the normal interval promptly");
  assert.equal(env.fetchCalls.length, 1);
  assert.equal(
    env.fetchCalls[0].init.headers,
    undefined,
    "a normal-interval tick after visibility recovery carries no hidden marker",
  );
});

test("declarative heartbeat logs one warning and stays disabled for an invalid hidden interval", () => {
  const main = new FakeElement("main", null);
  main.setAttribute("data-gosx-heartbeat", "/api/presence/ping");
  main.setAttribute("data-gosx-heartbeat-interval", "5s");
  main.setAttribute("data-gosx-heartbeat-hidden-interval", "not-a-duration");
  const env = createContext({ elements: [main] });
  const timers = installManualTimers(env.context);

  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(timers.count(), 0);
  assert.equal(env.consoleLogs.warn.length, 1);
  assert.match(env.consoleLogs.warn[0], /data-gosx-heartbeat-hidden-interval/);
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
