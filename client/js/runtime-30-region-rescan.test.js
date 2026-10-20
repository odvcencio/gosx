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
// registers, and a detached old element simply drops out of the rescan; and
// three side effects that rescan would otherwise cause on every region swap
// (a polled region can rescan every second) if the listener simply replayed
// each setup function's usual boot/soft-navigation behavior unchanged:
//
//   - setupPageCountdowns' shared 1-second interval keeping its own phase
//     across a rescan instead of being torn down and recreated (see "does
//     not reset the shared countdown interval" below).
//   - a title flash (data-gosx-watch-effect="title") keeping whatever
//     on/off phase it currently shows, instead of being stomped back to
//     its "on" phase on every rescan (see "does not reset an active title
//     flash's off phase" below).
//   - setupPageFilters never re-announcing an unchanged "N of M shown"
//     count to the shared aria-live region on a rescan (see "does not
//     re-announce" below); a real soft navigation still announces exactly
//     as before.
//
// Deliberately excluded from the rescan entirely: setupPageRevalidation,
// setupPageHeartbeat, and setupLiveRegions (see the listener's own doc
// comment in navigation.ts for why).

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  navigationSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  installManualClock,
  installManualTimers,
} = require("./runtime-test-harness.js");

function lastAnnouncement(env) {
  const region = env.document.querySelector("[data-gosx-announcer]");
  return region ? region.textContent : "";
}

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

  assert.equal(
    newCountdown.textContent,
    "0:59",
    "the newly swapped-in countdown must be registered and ticking (60s target - 1s elapsed = 59s = \"0:59\", exact under the manual clock)",
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
  assert.equal(
    current.textContent,
    "0:59",
    "the latest swapped-in element must be registered and ticking (exact under the manual clock)",
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
  assert.equal(revived.textContent, "0:59", "exact under the manual clock: 60s target - 1s elapsed = 59s");
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
  // callback never runs at all in a real browser.
  //
  // Tracking every LIVE 1000ms-interval HANDLE directly — not merely
  // counting create/clear calls — is what actually proves the shared
  // interval's IDENTITY, and therefore its phase, survives a rescan:
  // liveHandles must still contain the exact same handle setInterval
  // returned at boot, and nothing else. Scoping to delay===1000 (rather
  // than every interval the runtime creates) means an unrelated 1000ms
  // interval elsewhere in the runtime — for example the title-flash
  // effect, which shares this same cadence — could never be mistaken for
  // the countdown's own.
  const originalSetInterval = env.context.setInterval;
  const originalClearInterval = env.context.clearInterval;
  const liveHandles = new Set();
  env.context.setInterval = function(cb, delay) {
    const handle = originalSetInterval.apply(this, arguments);
    if (Number(delay) === 1000) liveHandles.add(handle);
    return handle;
  };
  env.context.clearInterval = function(handle) {
    liveHandles.delete(handle);
    return originalClearInterval.apply(this, arguments);
  };

  runScript(navigationSource, env.context, "navigation_runtime.js");
  assert.equal(timers.count(), 1);
  assert.equal(liveHandles.size, 1, "page boot must create exactly one live 1000ms countdown interval");
  const bootHandle = liveHandles.values().next().value;

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

  assert.deepEqual(
    Array.from(liveHandles),
    [bootHandle],
    "three rescans, each still with a live root, must never recreate the shared interval (a new handle) or clear the original one — recreating it would starve every real tick",
  );
  assert.equal(timers.count(), 1);

  // The remaining 100ms brings the manual clock to 1000ms total since
  // boot — the shared interval's own original phase, undisturbed by any of
  // the three swaps above.
  clock.advance(100);
  timers.runInterval(1000);

  assert.equal(
    countdown.textContent,
    "1:29",
    "exact under the manual clock: 90s target - 1s elapsed = 89s = \"1:29\"; not starved indefinitely",
  );
});

test("gosx:region:after does not reset an active title flash's off phase", () => {
  const el = new FakeElement("div", null);
  el.id = "on-clock-panel";
  el.setAttribute("data-on-clock", "true");
  el.setAttribute("data-gosx-watch", "data-on-clock=true");
  el.setAttribute("data-gosx-watch-effect", "title");
  el.setAttribute("data-gosx-watch-title", "It's your pick!");
  const env = createContext({ elements: [el] });
  // Deliberately distinct from the flash message, so a bug that ignores
  // the flash's own on/off phase (and, say, always shows one or the
  // other) cannot accidentally read as correct.
  env.document.title = "Home";
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  assert.equal(
    env.document.title,
    "It's your pick!",
    "an already-true condition fires immediately at boot, in the flash's \"on\" phase",
  );

  // The flash's own first toggle (WATCH_TITLE_FLASH_INTERVAL_MS === 1000,
  // the same cadence as the countdown tick) enters the "off" phase.
  timers.runInterval(1000);
  assert.equal(env.document.title, "Home", "the flash's own toggle enters the \"off\" phase");

  // A region swap elsewhere on the page rescans every watcher via
  // gosx:region:after — including this one, whose condition has not
  // changed. Unlike a real soft navigation, a region swap never touches
  // document.title, so nothing here should force it back to the message.
  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: el, url: "/frag" } });

  assert.equal(
    env.document.title,
    "Home",
    "a gosx:region:after rescan of an unchanged, still-active title flash must not reset its current off phase",
  );

  // The toggle itself must still be running normally afterward — the fix
  // must preserve the phase, not freeze the flash outright.
  timers.runInterval(1000);
  assert.equal(env.document.title, "It's your pick!", "the toggle keeps alternating normally after the rescan");
});

test("gosx:region:after rescan does not re-announce a filter's shown count; boot still announces once, exactly as before", async () => {
  const input = new FakeElement("input", null);
  input.setAttribute("data-gosx-filter", "pool-list");
  input.setAttribute("data-gosx-filter-announce", "true");
  const container = new FakeElement("ul", null);
  container.id = "pool-list";
  ["Patrick Mahomes", "Josh Allen", "Joe Burrow"].forEach(function(text) {
    const row = new FakeElement("li", null);
    row.setAttribute("data-gosx-filter-text", text);
    row.textContent = text;
    container.appendChild(row);
  });

  const env = createContext({ elements: [input, container] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await flushAsyncWork();

  assert.equal(
    lastAnnouncement(env),
    "3 of 3 shown",
    "boot's own setupPageFilters() call must still announce, exactly as before this fix",
  );

  // announceNavigation clears the live region's own textContent before
  // (asynchronously) re-setting it — clearing it here too, between boot's
  // announcement and the rescan, is what lets this test tell "no new
  // announcement happened" apart from "the same text got announced
  // again", which would otherwise look identical.
  const announcer = env.document.querySelector("[data-gosx-announcer]");
  announcer.textContent = "";

  env.document.dispatchEvent({ type: "gosx:region:after", detail: { element: container, url: "/frag" } });
  await flushAsyncWork();

  assert.equal(
    lastAnnouncement(env),
    "",
    "a gosx:region:after rescan must never re-announce the filter's shown count",
  );
});
