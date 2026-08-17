"use strict";
// Declarative reorder (data-gosx-reorder, gosx#212): the pure pointer -> index
// function, the keyboard grab/move/drop/cancel path, the pointer path,
// managed-action submission (optimistic reorder, revert on failure, the
// in-flight BLOCK policy), and the revalidation pause the drag holds for its
// whole gesture.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  navigationSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  installManualTimers,
} = require("./runtime-test-harness.js");

function reorderRuntime(env) {
  return env.context.__gosx.reorder;
}

function buildListElements(itemCount, options) {
  const opts = options || {};
  const container = new FakeElement("ul", null);
  container.setAttribute("data-gosx-reorder", "true");
  container.setAttribute("data-gosx-reorder-action", opts.action !== undefined ? opts.action : "POST /api/board/reorder");
  if (opts.itemField) container.setAttribute("data-gosx-reorder-item-field", opts.itemField);
  if (opts.indexField) container.setAttribute("data-gosx-reorder-index-field", opts.indexField);

  const items = [];
  for (let i = 0; i < itemCount; i += 1) {
    const item = new FakeElement("li", null);
    item.setAttribute("data-gosx-reorder-item", "item-" + i);
    // aria-label gives reorderItemLabel a clean, deterministic string to
    // announce — item.textContent would otherwise concatenate the item's own
    // text with its handle's text with no separating space.
    item.setAttribute("aria-label", "Item " + i);
    item.textContent = "Item " + i;
    let handle = item;
    if (opts.handle) {
      handle = new FakeElement("span", null);
      handle.setAttribute("data-gosx-reorder-handle", "true");
      handle.textContent = "handle";
      item.appendChild(handle);
    }
    item.__handle = handle;
    container.appendChild(item);
    items.push(item);
  }
  return { container, items };
}

// buildList builds the list AFTER the runtime is already loaded — the common
// case for every test below except the revalidation-pause one, which needs
// its data-gosx-revalidate-interval element present in document.body BEFORE
// navigation.ts's own initial-load scan runs (see that test).
function buildList(env, itemCount, options) {
  const built = buildListElements(itemCount, options);
  env.document.body.appendChild(built.container);
  return built;
}

// keydown dispatches a synthetic keydown and flushes the microtask queue
// before returning: announceNavigation defers the actual live-region text
// write to a microtask (see navigation.ts), so a caller that wants to assert
// on the announcement must await this instead of reading it synchronously.
async function keydown(env, target, key) {
  const event = {
    type: "keydown",
    key,
    target,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  };
  env.document.dispatchEvent(event);
  await flushAsyncWork();
  return event;
}

function lastAnnouncement(env) {
  const region = env.document.querySelector("[data-gosx-announcer]");
  return region ? region.textContent : "";
}

// --- pure function -------------------------------------------------------

test("reorderTargetIndex: midpoint crossings, both directions", () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);

  const rects = [
    { top: 0, height: 40 }, // midpoint 20
    { top: 40, height: 40 }, // midpoint 60
    { top: 80, height: 40 }, // midpoint 100
  ];

  // Downward sweep.
  assert.equal(reorder.targetIndexForPointer(19, rects), 0);
  assert.equal(reorder.targetIndexForPointer(21, rects), 1);
  assert.equal(reorder.targetIndexForPointer(59, rects), 1);
  assert.equal(reorder.targetIndexForPointer(61, rects), 2);
  assert.equal(reorder.targetIndexForPointer(99, rects), 2);
  assert.equal(reorder.targetIndexForPointer(101, rects), 3);

  // Upward sweep across the same boundaries — the function is pure and
  // stateless, so it must land on exactly the same index at exactly the
  // same pointer position regardless of the direction of travel.
  assert.equal(reorder.targetIndexForPointer(101, rects), 3);
  assert.equal(reorder.targetIndexForPointer(61, rects), 2);
  assert.equal(reorder.targetIndexForPointer(59, rects), 1);
  assert.equal(reorder.targetIndexForPointer(21, rects), 1);
  assert.equal(reorder.targetIndexForPointer(19, rects), 0);
});

test("reorderTargetIndex: first and last positions", () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);
  const rects = [
    { top: 0, height: 50 },
    { top: 50, height: 50 },
    { top: 100, height: 50 },
    { top: 150, height: 50 },
  ];

  assert.equal(reorder.targetIndexForPointer(-1000, rects), 0, "far above the list");
  assert.equal(reorder.targetIndexForPointer(0, rects), 0, "exactly at the top");
  assert.equal(reorder.targetIndexForPointer(1000, rects), rects.length, "far below the list");
  assert.equal(reorder.targetIndexForPointer(200, rects), rects.length, "exactly at the bottom");
});

test("reorderTargetIndex: single-item list always returns 0", () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);

  // Dragging the only item in a list means every OTHER item's rect list is
  // empty — there is nowhere else to put it, at any pointer position.
  assert.equal(reorder.targetIndexForPointer(0, []), 0);
  assert.equal(reorder.targetIndexForPointer(500, []), 0);
  assert.equal(reorder.targetIndexForPointer(-500, []), 0);
});

test("reorderTargetIndex: scrolled container coordinates", () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);

  // A container scrolled far down produces large, offset viewport rects —
  // getBoundingClientRect() already folds scrollTop into `top`, so the pure
  // function needs no separate scroll-offset argument. Correctness must
  // hold at any coordinate magnitude, not just values near 0.
  const scrolledDown = [
    { top: 5200, height: 60 }, // midpoint 5230
    { top: 5260, height: 60 }, // midpoint 5290
    { top: 5320, height: 60 }, // midpoint 5350
  ];
  assert.equal(reorder.targetIndexForPointer(5000, scrolledDown), 0);
  assert.equal(reorder.targetIndexForPointer(5240, scrolledDown), 1);
  assert.equal(reorder.targetIndexForPointer(5300, scrolledDown), 2);
  assert.equal(reorder.targetIndexForPointer(5400, scrolledDown), 3);

  // A container scrolled so far that the first items sit above the
  // viewport (negative `top`) still resolves correctly against a pointer
  // that is necessarily inside the viewport (clientY >= 0).
  const scrolledPastTop = [
    { top: -360, height: 40 }, // midpoint -340
    { top: -320, height: 40 }, // midpoint -300
  ];
  assert.equal(reorder.targetIndexForPointer(0, scrolledPastTop), 2);
});

// --- setup / handle discovery --------------------------------------------

test("a handle is prepared once: tabindex, role, aria-grabbed, touch-action", async () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 3, { handle: true });
  const handle = items[0].__handle;

  await keydown(env, handle, "Enter");

  assert.equal(handle.getAttribute("tabindex"), "0");
  assert.equal(handle.getAttribute("role"), "button");
  assert.equal(handle.getAttribute("aria-grabbed"), "true");
  assert.equal(handle.style.touchAction, "none");
  assert.equal(handle.getAttribute("data-gosx-reorder-handle-ready"), "true");
});

test("every handle is tabbable from page load, with no prior pointer or keydown", () => {
  // A keyboard-only user's first touch on a never-interacted-with handle is
  // Tab, not Space/Enter/pointerdown — none of which have fired yet. Every
  // handle must already be in the tab order (and carry aria-grabbed) by the
  // time the runtime finishes loading, not only after prepareReorderHandle's
  // own lazy, event-driven path runs.
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 3, { handle: true });

  // buildList appends to document.body AFTER the runtime already loaded —
  // dispatching gosx:navigate is what a soft navigation does to hand the
  // runtime a freshly swapped body; it is the re-scan trigger this test
  // exercises (the module's own initial-load scan already ran with an empty
  // document.body seconds earlier, in runScript itself).
  env.document.dispatchEvent({ type: "gosx:navigate", detail: {} });

  for (const item of items) {
    const handle = item.__handle;
    assert.equal(handle.getAttribute("tabindex"), "0", "handle is in the tab order");
    assert.equal(handle.getAttribute("aria-grabbed"), "false");
    assert.equal(handle.style.touchAction, "none");
  }
});

// --- keyboard path ---------------------------------------------------------

test("keyboard: grab, move, drop announces each step and reorders the DOM", async () => {
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/api/board/reorder": { text: JSON.stringify({ ok: true }) },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });
  const handle = items[0].__handle;

  await keydown(env, handle, " ");
  assert.equal(
    lastAnnouncement(env),
    "Grabbed Item 0. Position 1 of 3. Use arrow keys to move, space to drop, escape to cancel.",
  );
  assert.equal(container.getAttribute("class"), "gosx-reorder--dragging");
  assert.equal(items[0].getAttribute("class"), "gosx-reorder-item--grabbed");

  await keydown(env, handle, "ArrowDown");
  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-1", "item-0", "item-2"],
  );
  assert.equal(lastAnnouncement(env), "Moved to position 2 of 3.");

  await keydown(env, handle, "ArrowDown");
  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-1", "item-2", "item-0"],
  );
  assert.equal(lastAnnouncement(env), "Moved to position 3 of 3.");

  // Arrow past the last position is a no-op, not an error.
  await keydown(env, handle, "ArrowDown");
  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-1", "item-2", "item-0"],
  );

  await keydown(env, handle, " ");
  assert.equal(lastAnnouncement(env), "Dropped Item 0 at position 3 of 3.");
  assert.equal(container.getAttribute("class"), "", "the dragging class is removed on drop");
  assert.equal(items[0].getAttribute("class"), "", "the grabbed class is removed on drop");
  assert.equal(handle.getAttribute("aria-grabbed"), "false");

  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/api/board/reorder");
  assert.equal(env.fetchCalls[0].init.method, "POST");
  const body = new URLSearchParams(env.fetchCalls[0].init.body.toString());
  assert.equal(body.get("item_id"), "item-0");
  assert.equal(body.get("index"), "2");
  assert.equal(container.getAttribute("data-gosx-pending"), null);
  assert.equal(container.getAttribute("data-gosx-form-state"), "success");
});

test("keyboard: escape cancels and restores the original order with no submission", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });
  const handle = items[1].__handle;

  await keydown(env, handle, "Enter");
  await keydown(env, handle, "ArrowDown");
  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-0", "item-2", "item-1"],
  );

  const escapeEvent = await keydown(env, handle, "Escape");
  assert.equal(escapeEvent.defaultPrevented, true);

  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-0", "item-1", "item-2"],
  );
  assert.equal(lastAnnouncement(env), "Reorder cancelled.");
  assert.equal(handle.getAttribute("aria-grabbed"), "false");
  assert.equal(container.getAttribute("class"), "");
  assert.equal(env.fetchCalls.length, 0, "cancel never submits the managed action");
});

test("keyboard: a failed submission reverts the optimistic reorder and surfaces the error", async () => {
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/api/board/reorder": { status: 500, ok: false, text: JSON.stringify({ ok: false }) },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });
  const handle = items[0].__handle;

  await keydown(env, handle, " ");
  await keydown(env, handle, "ArrowDown");
  await keydown(env, handle, " ");
  await flushAsyncWork();

  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-0", "item-1", "item-2"],
    "a failed action reverts the DOM to server truth",
  );
  assert.equal(container.getAttribute("data-gosx-form-state"), "error");
  assert.equal(container.getAttribute("data-gosx-pending"), null);
  assert.equal(lastAnnouncement(env), "Reorder failed. Order restored.");
});

test("keyboard: custom field names post under the configured names", async () => {
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/api/board/reorder": { text: JSON.stringify({ ok: true }) },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 2, {
    handle: true,
    itemField: "playerId",
    indexField: "slot",
  });
  const handle = items[1].__handle;

  await keydown(env, handle, " ");
  await keydown(env, handle, "ArrowUp");
  await keydown(env, handle, " ");
  await flushAsyncWork();

  const body = new URLSearchParams(env.fetchCalls[0].init.body.toString());
  assert.equal(body.get("playerId"), "item-1");
  assert.equal(body.get("slot"), "0");
  assert.equal(body.get("item_id"), null);
});

test("keyboard: a second grab is blocked while a submission is in flight", async () => {
  let resolveFetch;
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/api/board/reorder": () =>
        new Promise((resolve) => {
          resolveFetch = () => resolve({ text: JSON.stringify({ ok: true }) });
        }),
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });
  const first = items[0].__handle;
  const second = items[1].__handle;

  await keydown(env, first, " ");
  await keydown(env, first, "ArrowDown");
  await keydown(env, first, " "); // drop -> submission starts and stays pending
  await flushAsyncWork();
  assert.equal(container.getAttribute("data-gosx-pending"), "true");

  // A second grab attempt anywhere in the same container while the first
  // drop's submission is still pending must be refused outright (BLOCK, not
  // queue) — the list must not change and no second request is issued.
  const orderBeforeSecondAttempt = container.children.map((c) => c.getAttribute("data-gosx-reorder-item"));
  await keydown(env, second, " ");
  assert.equal(second.getAttribute("aria-grabbed"), "false", "the blocked grab never took hold");
  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    orderBeforeSecondAttempt,
  );

  resolveFetch();
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1, "the blocked grab never issued a request");
  assert.equal(container.getAttribute("data-gosx-pending"), null);

  // Now that the first submission has settled, a grab is accepted again.
  await keydown(env, second, " ");
  assert.equal(second.getAttribute("aria-grabbed"), "true");
});

test("missing data-gosx-reorder-action never throws and never submits", async () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 2, { handle: true, action: "" });
  const handle = items[0].__handle;

  await keydown(env, handle, " ");
  await keydown(env, handle, " ");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 0, "no URL to submit to, so no request is issued");
});

// --- revalidation pause ----------------------------------------------------

test("a keyboard grab suspends periodic revalidation for its whole gesture", async () => {
  const revalidateRoot = new FakeElement("main", null);
  revalidateRoot.setAttribute("data-gosx-revalidate-interval", "4s");
  revalidateRoot.setAttribute("data-gosx-revalidate-src", "/api/league/version");
  const { container, items } = buildListElements(3, { handle: true });

  const versionURL = "http://localhost:3000/api/league/version";
  const env = createContext({
    elements: [revalidateRoot, container],
    fetchRoutes: {
      // data-gosx-revalidate-src makes each tick a plain body-diff poll
      // (pollRevalidateSrc), never a full navigate()/DOMParser round trip —
      // exactly what this test needs to isolate the suspend gate.
      [versionURL]: { text: '{"version":1}' },
      "http://localhost:3000/api/board/reorder": { text: JSON.stringify({ ok: true }) },
    },
  });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  const handle = items[0].__handle;

  // The first tick only records a baseline (see pollRevalidateSrc); consume
  // it before grabbing so every later tick is a real, countable poll.
  timers.runInterval(4000);
  await flushAsyncWork();
  const fetchesBeforeGrab = env.fetchCalls.filter((call) => call.url === versionURL).length;

  await keydown(env, handle, " ");
  timers.runInterval(4000);
  await flushAsyncWork();
  assert.equal(
    env.fetchCalls.filter((call) => call.url === versionURL).length,
    fetchesBeforeGrab,
    "revalidation is held off while a reorder gesture is active",
  );

  // Drop, ending the gesture. The follow-up action submission is a separate,
  // unsuspended request — revalidation resumes immediately, not once that
  // submission settles.
  await keydown(env, handle, " ");
  await flushAsyncWork();

  timers.runInterval(4000);
  await flushAsyncWork();
  assert.equal(
    env.fetchCalls.filter((call) => call.url === versionURL).length,
    fetchesBeforeGrab + 1,
    "revalidation resumes once the gesture ends",
  );
});

// --- auto-scroll ------------------------------------------------------------

test("autoScrollDeltaForPointer: zero inside the dead zone, scaled inside each edge zone, capped past each edge", () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);
  // Edge zone is 48px deep (REORDER_AUTOSCROLL_EDGE_PX), max speed 18px/tick
  // (REORDER_AUTOSCROLL_MAX_PX) — see the constants next to REORDER_AUTOSCROLL_TICK_MS.
  const containerRect = { top: 100, bottom: 300, height: 200 };

  assert.equal(reorder.autoScrollDeltaForPointer(200, containerRect), 0, "dead zone: no scroll");
  assert.equal(reorder.autoScrollDeltaForPointer(148, containerRect), 0, "exactly at the top zone boundary: no scroll");
  assert.equal(reorder.autoScrollDeltaForPointer(252, containerRect), 0, "exactly at the bottom zone boundary: no scroll");

  const nearTop = reorder.autoScrollDeltaForPointer(124, containerRect); // 24px into the 48px top zone: half depth
  assert.ok(nearTop < 0, "inside the top zone scrolls up (negative)");
  assert.ok(Math.abs(nearTop) < 18, "shallower than the deepest point scrolls slower than the cap");

  const nearBottom = reorder.autoScrollDeltaForPointer(276, containerRect); // 24px into the bottom zone
  assert.ok(nearBottom > 0, "inside the bottom zone scrolls down (positive)");
  assert.ok(nearBottom < 18);

  assert.equal(reorder.autoScrollDeltaForPointer(100, containerRect), -18, "at the very top edge: capped speed");
  assert.equal(reorder.autoScrollDeltaForPointer(99, containerRect), -18, "above the container: still capped, never faster");
  assert.equal(reorder.autoScrollDeltaForPointer(300, containerRect), 18, "at the very bottom edge: capped speed");
  assert.equal(reorder.autoScrollDeltaForPointer(301, containerRect), 18, "below the container: still capped, never faster");
});

test("autoScrollDeltaForPointer: a container too short for both edge zones never throws", () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);
  const shortRect = { top: 100, bottom: 120, height: 20 };
  assert.doesNotThrow(() => reorder.autoScrollDeltaForPointer(110, shortRect));
});

// --- pointer path -----------------------------------------------------------

function withRect(element, rect) {
  element.getBoundingClientRect = () => rect;
  return element;
}

test("pointer: drag down past a sibling's midpoint reorders on drop and posts the target index", async () => {
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/api/board/reorder": { text: JSON.stringify({ ok: true }) },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });

  withRect(container, { top: 0, bottom: 400, height: 400 });
  withRect(items[0], { top: 0, height: 40, bottom: 40 });
  withRect(items[1], { top: 40, height: 40, bottom: 80 });
  withRect(items[2], { top: 80, height: 40, bottom: 120 });

  const handle = items[0].__handle;
  const pointerId = 7;

  env.document.dispatchEvent({
    type: "pointerdown",
    target: handle,
    pointerId,
    pointerType: "mouse",
    button: 0,
    isPrimary: true,
    clientY: 20,
    preventDefault() {},
  });

  assert.equal(container.getAttribute("class"), "gosx-reorder--dragging");
  assert.equal(items[0].getAttribute("class"), "gosx-reorder-item--lifted");

  const placeholder = container.children.find((c) => c.hasAttribute("data-gosx-reorder-placeholder"));
  assert.ok(placeholder, "a placeholder clone marks the vacated slot");
  assert.equal(placeholder.getAttribute("aria-hidden"), "true");

  // Drag the pointer down past item[2]'s midpoint (100).
  handle.dispatchEvent({ type: "pointermove", pointerId, clientY: 105 });
  assert.equal(items[0].style.transform, "translateY(85px)");

  handle.dispatchEvent({ type: "pointerup", pointerId, clientY: 105 });

  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-1", "item-2", "item-0"],
  );
  assert.equal(items[0].getAttribute("class"), "", "lift class cleared on drop");
  assert.equal(items[0].style.transform, "", "inline transform cleared on drop");

  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 1);
  const body = new URLSearchParams(env.fetchCalls[0].init.body.toString());
  assert.equal(body.get("item_id"), "item-0");
  assert.equal(body.get("index"), "2");
});

test("pointer: pointercancel cancels an active drag and leaves the order untouched", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });
  withRect(container, { top: 0, bottom: 400, height: 400 });
  withRect(items[0], { top: 0, height: 40, bottom: 40 });
  withRect(items[1], { top: 40, height: 40, bottom: 80 });
  withRect(items[2], { top: 80, height: 40, bottom: 120 });

  const handle = items[0].__handle;
  const pointerId = 3;
  env.document.dispatchEvent({
    type: "pointerdown",
    target: handle,
    pointerId,
    pointerType: "mouse",
    button: 0,
    isPrimary: true,
    clientY: 20,
    preventDefault() {},
  });
  handle.dispatchEvent({ type: "pointermove", pointerId, clientY: 105 });

  handle.dispatchEvent({ type: "pointercancel", pointerId });

  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-0", "item-1", "item-2"],
    "pointercancel discards the placeholder and never moves the real item",
  );
  assert.equal(container.children.some((c) => c.hasAttribute("data-gosx-reorder-placeholder")), false);

  await flushAsyncWork();
  assert.equal(lastAnnouncement(env), "Reorder cancelled.");
  assert.equal(env.fetchCalls.length, 0);
});
