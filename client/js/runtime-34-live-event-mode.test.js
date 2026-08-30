"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { navigationSource, FakeElement, createContext, runScript, installManualTimers, flushAsyncWork } = require("./runtime-test-harness.js");

const url = "http://localhost:3000/draft/live.json";
function eventRoot(withSrc) {
  const root = new FakeElement("div", null);
  root.setAttribute("data-gosx-live-on", "draft:pick draft:clock");
  root.setAttribute("data-gosx-live-mode", "event");
  if (withSrc) root.setAttribute("data-gosx-live-src", url);
  const name = new FakeElement("span", null);
  name.setAttribute("data-gosx-live-bind", "cell.3.4");
  name.textContent = "3.04";
  root.appendChild(name);
  return { root, name };
}
const hubEvent = (env, event, data) => env.document.dispatchEvent({ type: "gosx:hub:event", detail: { hubID: "h", hubName: "draft-live", event, data } });
async function boot(root, routes) {
  const env = createContext({ elements: [root], fetchRoutes: routes || {} });
  installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();
  return env;
}

test("event mode applies the hub payload to binds with no fetch and needs no data-gosx-live-src", async () => {
  const { root, name } = eventRoot(false);
  const env = await boot(root);
  hubEvent(env, "draft:pick", { cell: { "3": { "4": "Tee Higgins" } } });
  await flushAsyncWork();
  assert.equal(name.textContent, "Tee Higgins");
  assert.equal(env.fetchCalls.length, 0, "event mode must never fetch on an event");
});

test("event mode ignores an unmatched event and a non-object payload", async () => {
  const { root, name } = eventRoot(false);
  const env = await boot(root);
  hubEvent(env, "draft:seat", { cell: { "3": { "4": "Wrong" } } });
  hubEvent(env, "draft:pick", "not an object");
  await flushAsyncWork();
  assert.equal(name.textContent, "3.04");
});

test("event mode with a src still fetches on window.__gosx.live.refresh", async () => {
  const { root, name } = eventRoot(true);
  const env = await boot(root, { [url]: { text: JSON.stringify({ cell: { "3": { "4": "Repaired" } } }) } });
  assert.equal(env.fetchCalls.length, 0, "no fetch at setup without an interval");
  await env.context.__gosx.live.refresh(root);
  assert.equal(name.textContent, "Repaired");
  assert.equal(env.fetchCalls.filter((call) => call.url === url).length, 1);
});
