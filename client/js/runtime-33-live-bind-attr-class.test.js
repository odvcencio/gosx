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

test("an attribute bind toggles a boolean attribute (hidden/disabled) by presence, in both directions", async () => {
  const { root, node } = liveRoot("hidden:clock.paused,disabled:clock.locked");
  let payload = { clock: { paused: true, locked: false } };
  const { timers } = await boot(root, () => payload);
  assert.equal(node.getAttribute("hidden"), "", "true sets the attribute present with an empty value, never the text \"true\"");
  assert.equal(node.hasAttribute("disabled"), false, "false leaves the attribute absent");
  payload = { clock: { paused: false, locked: true } };
  timers.runInterval(10000);
  await flushAsyncWork();
  assert.equal(node.hasAttribute("hidden"), false, "false removes a previously-set boolean attribute");
  assert.equal(node.getAttribute("disabled"), "", "true sets the attribute present");
});

test("an attribute bind treats the strings \"true\"/\"false\" and a JSON null as boolean for hidden/disabled", async () => {
  const { root, node } = liveRoot("hidden:v");
  await boot(root, () => ({ v: "true" }));
  assert.equal(node.getAttribute("hidden"), "");
  const { root: r2, node: n2 } = liveRoot("hidden:v");
  n2.setAttribute("hidden", "");
  await boot(r2, () => ({ v: "false" }));
  assert.equal(n2.hasAttribute("hidden"), false);
  const { root: r3, node: n3 } = liveRoot("hidden:v");
  n3.setAttribute("hidden", "");
  await boot(r3, () => ({ v: null }));
  assert.equal(n3.hasAttribute("hidden"), false, "a JSON null removes a boolean attribute the same as false");
});

test("an attribute bind ignores a non-boolean value for a boolean target", async () => {
  const { root, node } = liveRoot("hidden:v");
  node.setAttribute("hidden", "before");
  await boot(root, () => ({ v: "yes" }));
  assert.equal(node.getAttribute("hidden"), "before", "an unrecognized value leaves the attribute untouched");
});

// Security: an attribute bind is a server-controlled write into the DOM,
// so the runtime never writes an event handler, a script-capable URL, an
// inline style, or a runtime-owned attribute.
for (const [target, value] of [
  ["onclick", "alert(1)"], ["ONLOAD", "x"],
  ["href", "javascript:alert(1)"], ["href", "java\tscript:alert(1)"], ["href", "java\nscript:alert(1)"], ["href", "\u0001javascript:alert(1)"], ["href", "//evil.example"],
  // Browsers map a backslash to a forward slash while resolving a URL
  // with a special scheme, so each of these resolves exactly like
  // "//evil.example/x" — a protocol-relative, off-site URL — even though
  // none of them starts with two literal forward slashes.
  ["href", "/\\evil.example/x"], ["href", "\\\\evil.example/x"], ["href", "\\/evil.example/x"],
  ["src", "data:text/html,x"], ["formaction", "javascript:x"], ["action", "vbscript:x"],
  ["style", "color:red"], ["srcdoc", "<b>x</b>"], ["srcset", "https://evil.example/x 1x"], ["ping", "https://evil.example/p"], ["poster", "https://evil.example/p.png"], ["background", "x"], ["target", "_blank"], ["id", "gosx-hub-0"], ["name", "x"], ["class", "x"],
  ["data-gosx-region-url", "/evil"], ["data-gosx-live-src", "/evil"],
  // data-csrf-token and data-csrf carry no data-gosx- prefix, but
  // csrfTokenFromElement reads both for CSRF token resolution, so a bind
  // must never be able to rewrite either one.
  ["data-csrf-token", "evil-token"], ["data-csrf", "evil-token"],
  // Every target map liveBindAttrTargetAllowed reads is built with
  // Object.create(null): a plain object literal answers a lookup for any
  // one of these three names truthy through Object.prototype alone, with
  // no entry for the name ever having been added.
  ["constructor", "x"], ["__proto__", "x"], ["hasOwnProperty", "x"],
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

test("a colon-bearing target is unreachable: parseLiveBindPairs splits on the first colon, and the leftover target is refused anyway", async () => {
  const { root, node } = liveRoot("xlink:href:v");
  node.setAttribute("xlink", "before");
  const { env } = await boot(root, () => ({ "href:v": "javascript:alert(1)" }));
  // .map to primitive fields, not a bare deepEqual on the array of vm-realm
  // objects debugParseLiveBindPairs returns: the same cross-realm
  // Object.prototype identity issue debugCueLog's own doc comment
  // documents.
  const pairs = env.context.__gosx.navigation.debugParseLiveBindPairs("xlink:href:v");
  assert.equal(pairs.length, 1, "the grammar splits on the FIRST colon only");
  assert.equal(pairs[0].target, "xlink");
  assert.equal(pairs[0].key, "href:v");
  assert.equal(node.getAttribute("xlink"), "before", "the split-off target \"xlink\" is refused by the allowlist regardless of the value");
});

test("a class bind skips a target with embedded whitespace, since it could never toggle off", async () => {
  const { root, node } = liveRoot();
  node.setAttribute("data-gosx-live-bind-class", "pick clock:clock.warn");
  await boot(root, () => ({ clock: { warn: true } }));
  assert.equal(node.getAttribute("class"), "pick-clock", "a whitespace-bearing class target is never toggled on");
});
