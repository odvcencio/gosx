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
// regions.ts dispatches gosx:region:after on every successful swap (see
// 30-region-after-event.test.mjs for that contract, unrelated to this
// file's own navigation-runtime harness). This file covers the
// navigation.ts side: the boot-time listener that re-runs
// setupPageCountdowns/setupPageWatchers/setupPageFilters whenever that
// event fires, so an element the swap just introduced registers, and a
// detached old element simply drops out of the rescan.

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

test("gosx:region:after re-registration is idempotent: no duplicate shared timer across repeated events", () => {
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

  // Fire the event several times in a row, exactly as several independent
  // regions on the same page would after each finishing its own swap. Every
  // firing tears down and rebuilds from the same still-attached countdown,
  // so the page must still end up with exactly one shared timer, not one
  // per event.
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/a" } });
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/b" } });
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: region, url: "/c" } });

  assert.equal(timers.count(), 1, "repeated rescans must never accumulate extra shared timers");

  clock.advance(1000);
  timers.runInterval(1000);
  assert.match(countdown.textContent, /^1:2[89]$/);
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
