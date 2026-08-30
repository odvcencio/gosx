"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { navigationSource, FakeElement, createContext, runScript, installManualClock, installManualTimers } = require("./runtime-test-harness.js");

function countdown(target, text) {
  const el = new FakeElement("b", null);
  el.id = "clock";
  el.setAttribute("data-gosx-countdown", target);
  el.setAttribute("data-gosx-countdown-format", "mm:ss");
  el.textContent = text;
  return el;
}
const record = (el) => [{ target: el, type: "attributes", attributeName: "data-gosx-countdown" }];
// boot tracks every live 1000 ms interval handle by identity, the way
// runtime-30-region-rescan.test.js:283-299 does, so "the timer survived" is
// a handle comparison, not a count.
function boot(el) {
  const env = createContext({ elements: [el] });
  const clock = installManualClock(env.context, 0);
  const timers = installManualTimers(env.context);
  const live = new Set();
  const originalSet = env.context.setInterval, originalClear = env.context.clearInterval;
  env.context.setInterval = function(cb, delay) { const h = originalSet.apply(this, arguments); if (Number(delay) === 1000) live.add(h); return h; };
  env.context.clearInterval = function(h) { live.delete(h); return originalClear.apply(this, arguments); };
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const observer = env.mutationObservers.find((o) => o.targets.has(el));
  return { env, clock, timers, live, observer };
}

test("an attribute change retargets a registered countdown without re-registering it", () => {
  const el = countdown("1970-01-01T00:01:30Z", "1:30"); // now(0) + 90s
  const { env, clock, timers, live, observer } = boot(el);
  assert.ok(observer, "setup observes the countdown root");
  assert.equal(live.size, 1);
  const bootHandle = live.values().next().value;
  clock.advance(1000); timers.runInterval(1000);
  assert.equal(el.textContent, "1:29");
  el.setAttribute("data-gosx-countdown", "1970-01-01T00:02:01Z"); // now(1s) + 120s
  observer.trigger(record(el));
  clock.advance(1000); timers.runInterval(1000);
  assert.equal(el.textContent, "1:59", "the tick after the retarget counts from the new deadline");
  assert.deepEqual(Array.from(live), [bootHandle], "a retarget keeps the same shared interval");
  assert.equal(env.mutationObservers.filter((o) => o.targets.has(el)).length, 1, "no second observer");
});

test("an unchanged attribute write keeps the current target", () => {
  const el = countdown("1970-01-01T00:01:30Z", "1:30");
  const { clock, timers, observer } = boot(el);
  el.setAttribute("data-gosx-countdown", "1970-01-01T00:01:30Z");
  observer.trigger(record(el));
  clock.advance(1000); timers.runInterval(1000);
  assert.equal(el.textContent, "1:29");
});

test("expiry clears the shared timer (gosx#178 m8) and a retarget restarts it", () => {
  const el = countdown("1970-01-01T00:00:01Z", "0:01");
  const { env, clock, timers, live } = boot(el);
  clock.advance(2000); timers.runInterval(1000);
  assert.equal(el.textContent, "0:00");
  assert.equal(live.size, 0, "every root finished: the interval is cleared");
  assert.equal(env.context.__gosx.countdown.retarget(el, "1970-01-01T00:00:32Z"), true); // now(2s) + 30s
  assert.equal(live.size, 1, "a retarget restarts the interval");
  clock.advance(1000); timers.runInterval(1000);
  assert.equal(el.textContent, "0:29");
});
