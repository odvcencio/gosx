"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { navigationSource, FakeElement, createContext, runScript, installManualTimers, flushAsyncWork } = require("./runtime-test-harness.js");

const url = "http://localhost:3000/draft/live.json";
function liveRoot(bindAttr) {
  const root = new FakeElement("div", null);
  root.setAttribute("data-gosx-live-src", url);
  root.setAttribute("data-gosx-live-interval", "10s");
  const node = new FakeElement("b", null);
  node.setAttribute("class", "pick-clock");
  node.setAttribute("data-gosx-countdown", "2026-09-06T17:00:00Z");
  node.setAttribute("data-gosx-live-bind-attr", bindAttr || "data-gosx-countdown:clock.deadline");
  node.setAttribute("data-gosx-live-bind-class", "pick-clock--warn:clock.warn,pick-clock--paused:clock.paused");
  node.textContent = "1:30";
  root.appendChild(node);
  return { root, node };
}
async function boot(root, payload) {
  const env = createContext({ elements: [root], fetchRoutes: { [url]: () => ({ text: JSON.stringify(payload()) }) } });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  return { env, timers };
}

test("an attribute bind sets the named attribute and leaves text and class alone", async () => {
  const { root, node } = liveRoot();
  await boot(root, () => ({ clock: { deadline: "2026-09-06T17:02:00Z", warn: false, paused: false } }));
  assert.equal(node.getAttribute("data-gosx-countdown"), "2026-09-06T17:02:00Z");
  assert.equal(node.textContent, "1:30");
  assert.equal(node.getAttribute("class"), "pick-clock");
});

test("a class bind toggles each named class from a boolean and ignores a non-boolean", async () => {
  const { root, node } = liveRoot();
  let payload = { clock: { deadline: "2026-09-06T17:00:00Z", warn: true, paused: "yes" } };
  const { timers } = await boot(root, () => payload);
  assert.equal(node.getAttribute("class"), "pick-clock pick-clock--warn");
  payload = { clock: { deadline: "2026-09-06T17:00:00Z", warn: false, paused: true } };
  timers.runInterval(10000);
  await flushAsyncWork();
  assert.equal(node.getAttribute("class"), "pick-clock pick-clock--paused");
});

// Security: an attribute bind is a server-controlled write into the DOM,
// so the runtime never writes an event handler, a script-capable URL, an
// inline style, or a runtime-owned attribute.
for (const [target, value] of [
  ["onclick", "alert(1)"], ["ONLOAD", "x"],
  ["href", "javascript:alert(1)"], ["href", "java\tscript:alert(1)"], ["href", "java\nscript:alert(1)"], ["href", "\u0001javascript:alert(1)"], ["href", "//evil.example"],
  ["src", "data:text/html,x"], ["formaction", "javascript:x"], ["action", "vbscript:x"], ["xlink:href", "javascript:x"],
  ["style", "color:red"], ["srcdoc", "<b>x</b>"], ["srcset", "https://evil.example/x 1x"], ["ping", "https://evil.example/p"], ["poster", "https://evil.example/p.png"], ["background", "x"], ["target", "_blank"], ["id", "gosx-hub-0"], ["name", "x"], ["class", "x"],
  ["data-gosx-region-url", "/evil"], ["data-gosx-live-src", "/evil"],
]) {
  test("an attribute bind refuses " + target + "=" + JSON.stringify(value), async () => {
    const { root, node } = liveRoot(target + ":v");
    node.setAttribute(target, "before");
    await boot(root, () => ({ v: value }));
    assert.equal(node.getAttribute(target), "before", "the refused target is untouched");
  });
}

test("an attribute bind refuses data-gosx-countdown on a node that declares -then", async () => {
  const { root, node } = liveRoot("data-gosx-countdown:deadline");
  node.setAttribute("data-gosx-countdown", "2026-09-06T17:00:00Z");
  node.setAttribute("data-gosx-countdown-then", "revalidate");
  await boot(root, () => ({ deadline: "2020-01-01T00:00:00Z" }));
  assert.equal(node.getAttribute("data-gosx-countdown"), "2026-09-06T17:00:00Z", "a payload never re-points a -then countdown");
});

test("an attribute bind accepts a relative or http(s) URL and data-gosx-countdown", async () => {
  const { root, node } = liveRoot("href:link,data-gosx-countdown:deadline");
  await boot(root, () => ({ link: "/players?q=x", deadline: "2026-09-06T17:05:00Z" }));
  assert.equal(node.getAttribute("href"), "/players?q=x");
  assert.equal(node.getAttribute("data-gosx-countdown"), "2026-09-06T17:05:00Z");
  const { root: r2, node: n2 } = liveRoot("href:link");
  await boot(r2, () => ({ link: "https://example.com/x" }));
  assert.equal(n2.getAttribute("href"), "https://example.com/x");
});
