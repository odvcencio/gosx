"use strict";
// The gosx:region:after rescan (fix/region-rescan-countdowns).
//
// A declarative region (data-gosx-region, regions.ts) replaces its own
// subtree with gosxHost.dom.replace(el, html) — effectively el.innerHTML =
// html — on every refetch, entirely outside the soft-navigation lifecycle
// finalizeNavigation and the initial-document replay already re-scan
// through. Before this fix, a data-gosx-countdown (or -watch, or -filter)
// element swapped in by a region refetch was never registered at all: the
// production symptom is a draft pick clock, inside a region, that freezes
// at its server-rendered text the moment the region refetches.
//
// regions.ts dispatches gosx:region:after on every successful swap — it has
// since 0b1f558d, well before this fix (see 30-region-after-event.test.mjs
// for that contract, unrelated to this file's own navigation-runtime
// harness). This file covers the navigation.ts side: the boot-time listener
// that re-runs setupPageCountdowns/setupPageWatchers/setupPageFilters
// whenever that event fires, so an element the swap just introduced
// registers, and a detached old element simply drops out of the rescan —
// and setupPageCountdowns' own fix to keep the shared countdown interval's
// phase across a rescan instead of restarting it (see the dedicated
// "does not reset the shared countdown interval" test below).

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  navigationSource,
  FakeElement,
  createContext,
  runScript,
  installManualClock,
  installManualTimers,
} = require("./runtime-test-harness.js");

test("a countdown swapped in by a region refetch starts ticking; the detached old one never ticks again", () => {
  const region = new FakeElement("div", null);
  region.id = "r";
  const oldCountdown = new FakeElement("b", null);
  oldCountdown.id = "old";
  oldCountdown.setAttribute("data-gosx-countdown", "1970-01-01T00:01:30Z"); // now(0) + 90s
  oldCountdown.setAttribute("data-gosx-countdown-format", "mm:ss");
  oldCountdown.textContent = "1:30";
  region.appendChild(oldCountdown);

  const env = createContext({ elements: [region] });
  const clock = installManualClock(env.context, 0);
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(timers.count(), 1, "boot must find the old countdown and start the shared timer");

  // Simulate regions.ts's own swap: the old element is detached and a
  // brand-new element — with its own, different countdown target — takes
  // its place inside the SAME region container. FakeElement.innerHTML does
  // not parse markup into elements (see runtime-test-harness.js), so a
  // remove/append pair is this fake DOM's stand-in for gosxHost.dom.replace's
  // real innerHTML write: the structural before/after this rescan cares
  // about — old detached, new attached under the same region root — is
  // identical either way.
  region.removeChild(oldCountdown);
  const newCountdown = new FakeElement("b", null);
  newCountdown.id = "new";
  newCountdown.setAttribute("data-gosx-countdown", "1970-01-01T00:01:00Z"); // now(0) + 60s
  newCountdown.setAttribute("data-gosx-countdown-format", "mm:ss");
  newCountdown.textContent = "1:00";
  region.appendChild(newCountdown);

  env.document.dispatchEvent({
    type: "gosx:region:after",
    detail: { element: region, url: "/frag" },
  });

  clock.advance(1000);
  timers.runInterval(1000);

  assert.match(
    newCountdown.textContent,
    /^0:5[89]$/,
    "the newly swapped-in countdown must be registered and ticking",
  );
  assert.equal(
    oldCountdown.textContent,
    "1:30",
    "the detached old countdown must never update again",
  );
});

// A swapped-in countdown must never tick unless gosx:region:after actually
// fires — the control case for the test above. Without this, "the new
// element ticked" could in principle be explained by something other than
// the event listener (for example a rescan that runs unconditionally on
// every tick).
test("a countdown swapped into a region stays frozen at its server text when gosx:region:after never fires", () => {
  const region = new FakeElement("div", null);
  region.id = "r";
  const oldCountdown = new FakeElement("b", null);
  oldCountdown.id = "old";
  oldCountdown.setAttribute("data-gosx-countdown", "1970-01-01T00:01:30Z");
  oldCountdown.setAttribute("data-gosx-countdown-format", "mm:ss");
  oldCountdown.textContent = "1:30";
  region.appendChild(oldCountdown);

  const env = createContext({ elements: [region] });
  const clock = installManualClock(env.context, 0);
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  region.removeChild(oldCountdown);
  const newCountdown = new FakeElement("b", null);
  newCountdown.id = "new";
  newCountdown.setAttribute("data-gosx-countdown", "1970-01-01T00:01:00Z");
  newCountdown.setAttribute("data-gosx-countdown-format", "mm:ss");
  newCountdown.textContent = "1:00";
  region.appendChild(newCountdown);
  // No gosx:region:after dispatch here — the swap happens, but the event
  // that tells the runtime to rescan never fires.

  clock.advance(1000);
  timers.runInterval(1000);

  assert.equal(
    newCountdown.textContent,
    "1:00",
    "an unregistered element must never tick, no matter what the shared timer does",
  );
});

test("gosx:region:after re-registers a newly swapped-in element on each of several repeated swaps, without ever accumulating an extra shared timer", () => {
  const region = new FakeElement("div", null);
  region.id = "r";
  let current = new FakeElement("b", null);
  current.id = "c0";
  current.setAttribute("data-gosx-countdown", "1970-01-01T00:01:30Z"); // +90s
  current.setAttribute("data-gosx-countdown-format", "mm:ss");
  current.textContent = "1:30";
  region.appendChild(current);

  const env = createContext({ elements: [region] });
  const clock = installManualClock(env.context, 0);
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  assert.equal(timers.count(), 1);

  // Three swaps in a row, each detaching the previously-current element and
  // attaching a fresh one with its own target — exactly like three
  // independent regions on the same page, each finishing its own refetch.
  // Every firing must (a) register the newly-attached element and (b) never
  // grow the page beyond one shared timer.
  const targets = [
    ["1970-01-01T00:01:20Z", "1:20"], // +80s
    ["1970-01-01T00:01:10Z", "1:10"], // +70s
    ["1970-01-01T00:01:00Z", "1:00"], // +60s
  ];
  const stale = [];
  for (let i = 0; i < targets.length; i += 1) {
    stale.push(current);
    region.removeChild(current);
    const next = new FakeElement("b", null);
    next.id = "c" + (i + 1);
    next.setAttribute("data-gosx-countdown", targets[i][0]);
    next.setAttribute("data-gosx-countdown-format", "mm:ss");
    next.textContent = targets[i][1];
    region.appendChild(next);
    current = next;
    env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/swap" + i } });
  }

  assert.equal(timers.count(), 1, "repeated rescans must never accumulate extra shared timers");

  clock.advance(1000);
  timers.runInterval(1000);

  // The LATEST swapped-in element must be the one ticking now — proving
  // each dispatch actually re-registered the current element, not just
  // replayed the very first registration from boot.
  assert.match(
    current.textContent,
    /^0:5[89]$/,
    "the latest swapped-in element must be registered and ticking",
  );
  // Every earlier, now-detached element must be frozen exactly where this
  // loop left it. If the listener were missing (or only fired once), the
  // shared timer would still be ticking the ORIGINAL, still-referenced c0
  // record — which is stale[0] here — and this would catch that: c0's text
  // would keep advancing instead of staying at "1:20".
  assert.equal(stale[0].textContent, "1:30");
  assert.equal(stale[1].textContent, "1:20");
  assert.equal(stale[2].textContent, "1:10");
});

test("gosx:region:after restarts the shared countdown timer after a swap removes the last countdown and a later swap adds one back", () => {
  const region = new FakeElement("div", null);
  region.id = "r";
  const countdown = new FakeElement("b", null);
  countdown.id = "c";
  countdown.setAttribute("data-gosx-countdown", "1970-01-01T00:01:30Z");
  countdown.setAttribute("data-gosx-countdown-format", "mm:ss");
  countdown.textContent = "1:30";
  region.appendChild(countdown);

  const env = createContext({ elements: [region] });
  const clock = installManualClock(env.context, 0);
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  assert.equal(timers.count(), 1);

  // A swap that empties the region of every countdown must stop the shared
  // timer — nothing left on the page can tick.
  region.removeChild(countdown);
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/empty" } });
  assert.equal(timers.count(), 0, "a swap leaving zero countdown roots must clear the shared timer");

  // A later swap that reintroduces a countdown must restart it.
  const revived = new FakeElement("b", null);
  revived.id = "revived";
  revived.setAttribute("data-gosx-countdown", "1970-01-01T00:01:00Z");
  revived.setAttribute("data-gosx-countdown-format", "mm:ss");
  revived.textContent = "1:00";
  region.appendChild(revived);
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/revived" } });
  assert.equal(timers.count(), 1, "a later swap that adds a countdown back must restart the shared timer");

  clock.advance(1000);
  timers.runInterval(1000);
  assert.match(revived.textContent, /^0:5[89]$/);
});

test("gosx:region:after does not reset the shared countdown interval: three swaps under 1000ms apart never recreate it or drop a real tick", () => {
  const region = new FakeElement("div", null);
  region.id = "r";
  const countdown = new FakeElement("b", null);
  countdown.id = "c";
  countdown.setAttribute("data-gosx-countdown", "1970-01-01T00:01:30Z"); // +90s
  countdown.setAttribute("data-gosx-countdown-format", "mm:ss");
  countdown.textContent = "1:30";
  region.appendChild(countdown);

  const env = createContext({ elements: [region] });
  const clock = installManualClock(env.context, 0);
  const timers = installManualTimers(env.context);

  // installManualTimers' own runInterval(delay) is a purely logical
  // trigger: it fires every CURRENTLY live interval matching `delay`
  // whenever a test asks, with no notion of how much manual-clock time has
  // elapsed since that particular interval was (re)created. That makes it
  // blind to this bug's real shape on its own — a REAL setInterval(fn,
  // 1000) recreated by a rescan does not fire until a full real 1000ms
  // elapses from ITS OWN creation, so recreating it every ~300ms means the
  // callback never runs at all in a real browser. Counting the runtime's
  // own setInterval/clearInterval calls directly is what actually proves
  // the shared interval's identity — and therefore its phase — survives a
  // rescan; the tick assertion at the end is the literal "still advances by
  // ~1s" check on top of that.
  const originalSetInterval = env.context.setInterval;
  const originalClearInterval = env.context.clearInterval;
  let setIntervalCalls = 0;
  let clearIntervalCalls = 0;
  env.context.setInterval = function(cb, delay) {
    if (Number(delay) === 1000) setIntervalCalls += 1;
    return originalSetInterval.apply(this, arguments);
  };
  env.context.clearInterval = function(handle) {
    clearIntervalCalls += 1;
    return originalClearInterval.apply(this, arguments);
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  assert.equal(timers.count(), 1);
  assert.equal(setIntervalCalls, 1, "page boot must create exactly one shared interval");
  assert.equal(clearIntervalCalls, 0);

  // Three region swaps 300ms apart, well inside the same 1-second window —
  // exactly what an un-rate-limited signal- or hub-event-triggered region
  // can produce, and what a polled region's own 1000ms floor can land on.
  // The countdown element itself never leaves the document across any of
  // these three rescans, so every one of them still finds exactly one root.
  clock.advance(300);
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/a" } });
  clock.advance(300);
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/b" } });
  clock.advance(300);
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/c" } });

  assert.equal(
    setIntervalCalls,
    1,
    "three rescans, each still with a live root, must never recreate the shared interval — recreating it would starve every real tick",
  );
  assert.equal(
    clearIntervalCalls,
    0,
    "none of the three rescans found zero roots, so the interval must never have been cleared",
  );
  assert.equal(timers.count(), 1);

  // The remaining 100ms brings the manual clock to 1000ms total since
  // boot — the shared interval's own original phase, undisturbed by any of
  // the three swaps above.
  clock.advance(100);
  timers.runInterval(1000);

  assert.match(
    countdown.textContent,
    /^1:2[89]$/,
    "the countdown must have ticked by ~1 second total, not be starved indefinitely",
  );
});
