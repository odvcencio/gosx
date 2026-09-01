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
  installManualRAF,
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

// plainCopy re-homes an object the runtime created inside its own vm
// context. assert/strict compares prototypes, and a cross-realm object has a
// different Object.prototype than this file does.
function plainCopy(value) {
  return Object.assign({}, value);
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

test("a dedicated handle is prepared once: tabindex, role, aria-pressed, touch-action", async () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 3, { handle: true });
  const handle = items[0].__handle;

  await keydown(env, handle, "Enter");

  assert.equal(handle.getAttribute("tabindex"), "0");
  assert.equal(handle.getAttribute("role"), "button");
  assert.equal(handle.getAttribute("aria-pressed"), "true");
  assert.equal(handle.getAttribute("data-gosx-reorder-grabbed"), "true");
  assert.equal(handle.getAttribute("aria-grabbed"), null, "aria-grabbed was removed in ARIA 1.2");
  assert.equal(handle.style.touchAction, "none");
  assert.equal(handle.getAttribute("data-gosx-reorder-handle-ready"), "true");
});

test("every handle is tabbable from page load, with no prior pointer or keydown", () => {
  // A keyboard-only user's first touch on a never-interacted-with handle is
  // Tab, not Space/Enter/pointerdown — none of which have fired yet. Every
  // handle must already be in the tab order (and carry its grab state and its
  // instructions) by the time the runtime finishes loading, not only after
  // prepareReorderHandle's own lazy, event-driven path runs.
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
    assert.equal(handle.getAttribute("aria-pressed"), "false");
    assert.equal(handle.getAttribute("data-gosx-reorder-grabbed"), "false");
    assert.equal(handle.style.touchAction, "none");
  }
});

test("a11y: every handle points aria-describedby at one shared instructions node", () => {
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 3, { handle: true });
  env.document.dispatchEvent({ type: "gosx:navigate", detail: {} });

  const instructions = env.document.querySelector("[data-gosx-reorder-instructions]");
  assert.ok(instructions, "the runtime creates a pre-interaction instructions node");
  assert.equal(instructions.getAttribute("id"), "gosx-reorder-instructions");
  assert.match(instructions.textContent, /arrow up and arrow down/i);

  for (const item of items) {
    assert.equal(item.__handle.getAttribute("aria-describedby"), "gosx-reorder-instructions");
  }

  // One node for the whole page, however many handles point at it.
  assert.equal(
    env.document.querySelectorAll("[data-gosx-reorder-instructions]").length,
    1,
  );
});

test("a11y: a whole-item handle gets no button role, no roledescription, and no touch-action", () => {
  // An item that acts as its own handle is not a button: stamping role and
  // aria-roledescription on it flattens everything inside the row into one
  // "Sortable item button". touch-action: none on the whole row would kill
  // native list scrolling for every finger that lands on it.
  const env = createContext({ elements: [] });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 3, {});
  env.document.dispatchEvent({ type: "gosx:navigate", detail: {} });

  for (const item of items) {
    assert.equal(item.getAttribute("tabindex"), "0", "still reachable by keyboard");
    assert.equal(item.getAttribute("aria-describedby"), "gosx-reorder-instructions");
    assert.equal(item.getAttribute("role"), null);
    assert.equal(item.getAttribute("aria-roledescription"), null);
    assert.equal(item.getAttribute("aria-pressed"), null);
    assert.equal(item.style.touchAction, undefined, "the row keeps native scrolling");
  }
});

test("a11y: the live region turns assertive for a gesture and polite again after it", async () => {
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/api/board/reorder": { text: JSON.stringify({ ok: true }) },
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 3, { handle: true });
  const handle = items[0].__handle;

  await keydown(env, handle, " ");
  const region = env.document.querySelector("[data-gosx-announcer]");
  assert.equal(region.getAttribute("aria-live"), "assertive", "move feedback must not queue behind other speech");

  await keydown(env, handle, "ArrowDown");
  assert.equal(region.getAttribute("aria-live"), "assertive");

  await keydown(env, handle, " ");
  assert.equal(region.getAttribute("aria-live"), "polite", "ordinary navigation announcements stay polite");
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
  assert.equal(handle.getAttribute("aria-pressed"), "false");

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
  assert.equal(handle.getAttribute("aria-pressed"), "false");
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
  assert.equal(second.getAttribute("aria-pressed"), "false", "the blocked grab never took hold");
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
  assert.equal(second.getAttribute("aria-pressed"), "true");
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

  // pointerdown alone is a PRESS, not a drag: nothing is lifted, no
  // placeholder exists, and the list is untouched until the pointer travels
  // the activation distance.
  assert.equal(container.getAttribute("class"), null, "a press does not lift anything");
  assert.equal(container.children.some((c) => c.hasAttribute("data-gosx-reorder-placeholder")), false);

  // Drag the pointer down past item[2]'s midpoint (100). This crosses the
  // activation distance, which starts the drag, and the transform is measured
  // from the original pointerdown position.
  handle.dispatchEvent({ type: "pointermove", pointerId, clientY: 105 });
  assert.equal(container.getAttribute("class"), "gosx-reorder--dragging");
  assert.equal(items[0].getAttribute("class"), "gosx-reorder-item--lifted");
  assert.equal(items[0].style.transform, "translateY(85px)");

  const placeholder = container.children.find((c) => c.hasAttribute("data-gosx-reorder-placeholder"));
  assert.ok(placeholder, "a placeholder clone marks the vacated slot");
  assert.equal(placeholder.getAttribute("aria-hidden"), "true");

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

test("pointer: Escape cancels an active drag the same way pointercancel does (gosx#223)", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });
  withRect(container, { top: 0, bottom: 400, height: 400 });
  withRect(items[0], { top: 0, height: 40, bottom: 40 });
  withRect(items[1], { top: 40, height: 40, bottom: 80 });
  withRect(items[2], { top: 80, height: 40, bottom: 120 });

  const handle = items[0].__handle;
  const pointerId = 9;
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
  assert.equal(handle._capturedPointerID, pointerId, "the drag captures the pointer on lift");

  // Drag past item[2]'s midpoint, same as the pointercancel test above, so
  // the placeholder has actually moved before Escape has to undo it.
  handle.dispatchEvent({ type: "pointermove", pointerId, clientY: 105 });
  assert.equal(container.getAttribute("class"), "gosx-reorder--dragging");
  assert.equal(items[0].getAttribute("class"), "gosx-reorder-item--lifted");
  assert.notEqual(items[0].style.transform, "");

  const escapeEvent = await keydown(env, handle, "Escape");
  assert.equal(escapeEvent.defaultPrevented, true, "Escape must not also trigger a browser default action");

  // Exactly pointercancel's own teardown: DOM reverted to the pre-drag
  // order, no placeholder left behind, lift class and inline transform
  // cleared, pointer capture released, and no action submitted.
  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-0", "item-1", "item-2"],
    "Escape discards the placeholder and never moves the real item",
  );
  assert.equal(container.children.some((c) => c.hasAttribute("data-gosx-reorder-placeholder")), false);
  assert.equal(container.getAttribute("class"), "", "the dragging class is cleared");
  assert.equal(items[0].getAttribute("class"), "", "the lifted class is cleared");
  assert.equal(items[0].style.transform, "", "the inline transform is cleared");
  assert.equal(handle._capturedPointerID, null, "pointer capture is released");

  await flushAsyncWork();
  assert.equal(lastAnnouncement(env), "Reorder cancelled.");
  assert.equal(env.fetchCalls.length, 0, "Escape never submits the managed action");

  // A later pointerup for the same gesture (a real browser can still
  // deliver one after Escape already tore the drag down) must be a no-op —
  // activeReorderDrag is already null, so endReorderPointerDrag's own
  // early-return guard is what this proves.
  handle.dispatchEvent({ type: "pointerup", pointerId, clientY: 105 });
  await flushAsyncWork();
  assert.equal(env.fetchCalls.length, 0, "a stale pointerup after Escape submits nothing");
});

test("pointer: Escape is ignored with no active pointer drag, so a keyboard grab elsewhere still starts normally", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 2, { handle: true });
  const handle = items[0].__handle;

  // No pointer drag and no keyboard grab active: Escape is simply not this
  // listener's concern (it never calls preventDefault, and nothing throws).
  const escapeEvent = await keydown(env, handle, "Escape");
  assert.equal(escapeEvent.defaultPrevented, false);

  // The handle is still perfectly usable afterward.
  await keydown(env, handle, " ");
  assert.equal(handle.getAttribute("aria-pressed"), "true");
});

// --- auto-scroll target selection -------------------------------------------

// pointerDown and pointerMove keep the touch and mouse pointer tests below
// symmetric: the ONLY difference between the two is pointerType, which is
// exactly the difference the runtime branches on.
function pointerDown(env, handle, options) {
  const opts = options || {};
  const event = {
    type: "pointerdown",
    target: handle,
    pointerId: opts.pointerId != null ? opts.pointerId : 1,
    pointerType: opts.pointerType || "mouse",
    button: 0,
    isPrimary: true,
    clientX: opts.clientX != null ? opts.clientX : 10,
    clientY: opts.clientY != null ? opts.clientY : 20,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  };
  env.document.dispatchEvent(event);
  return event;
}

function pointerMove(handle, options) {
  const opts = options || {};
  handle.dispatchEvent({
    type: "pointermove",
    pointerId: opts.pointerId != null ? opts.pointerId : 1,
    clientX: opts.clientX != null ? opts.clientX : 10,
    clientY: opts.clientY,
  });
}

function makeScrollable(element, clientHeight, scrollHeight) {
  element.computedStyle = { overflowY: "auto" };
  element.clientHeight = clientHeight;
  element.scrollHeight = scrollHeight;
  element.scrollTop = 0;
  return element;
}

test("auto-scroll: the target is the nearest scrollable ancestor, not the container", () => {
  const env = createContext({ elements: [] });
  env.context.innerHeight = 800;
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);

  const pane = new FakeElement("div", null);
  env.document.body.appendChild(pane);
  const { container } = buildListElements(3, { handle: true });
  pane.appendChild(container);
  makeScrollable(pane, 400, 4000);
  withRect(pane, { top: 120, bottom: 520, height: 400 });
  // The container itself is far taller than the screen — the shape that made
  // the old container-rect edge zones unreachable.
  withRect(container, { top: 120, bottom: 4120, height: 4000 });

  const target = reorder.scrollTargetForContainer(container);
  assert.equal(target.element, pane, "the scrolling pane is what a drag scrolls");
  assert.equal(target.page, false);

  const view = plainCopy(reorder.scrollViewRectForContainer(container));
  assert.deepEqual(view, { top: 120, bottom: 520, height: 400 }, "edge zones live on the pane's visible box");
});

test("auto-scroll: a pane that declares overflow but has nothing to scroll is skipped", () => {
  const env = createContext({ elements: [] });
  env.context.innerHeight = 800;
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);

  const pane = new FakeElement("div", null);
  env.document.body.appendChild(pane);
  const { container } = buildListElements(3, { handle: true });
  pane.appendChild(container);
  // overflow: auto, but the content fits: this element scrolls nothing.
  makeScrollable(pane, 400, 400);

  const target = reorder.scrollTargetForContainer(container);
  assert.equal(target.page, true, "the search keeps walking up to the page scroller");
  assert.equal(target.element, env.document.documentElement);
});

test("auto-scroll: with no scrollable ancestor the edge zones are the viewport", () => {
  // This is the phone case data-gosx-reorder exists for: a 100-row board in
  // the normal document flow, taller than the screen, scrolled by the window.
  // Measuring the edge zones against the CONTAINER put both of them off
  // screen, where no finger could ever reach them.
  const env = createContext({ elements: [] });
  env.context.innerHeight = 640;
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);

  const { container } = buildList(env, 3, { handle: true });
  withRect(container, { top: -2400, bottom: 3600, height: 6000 });

  const target = reorder.scrollTargetForContainer(container);
  assert.equal(target.page, true);

  const view = plainCopy(reorder.scrollViewRectForContainer(container));
  assert.deepEqual(view, { top: 0, bottom: 640, height: 640 });

  // A finger near the bottom of the SCREEN scrolls down, which the old
  // container-rect measurement could never report.
  assert.ok(reorder.autoScrollDeltaForPointer(630, view) > 0);
  assert.ok(reorder.autoScrollDeltaForPointer(10, view) < 0);
  assert.equal(reorder.autoScrollDeltaForPointer(320, view), 0, "the middle of the screen is a dead zone");
});

test("auto-scroll: a scrollable pane's rect is clipped to the viewport", () => {
  const env = createContext({ elements: [] });
  env.context.innerHeight = 600;
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);

  const pane = new FakeElement("div", null);
  env.document.body.appendChild(pane);
  const { container } = buildListElements(3, { handle: true });
  pane.appendChild(container);
  makeScrollable(pane, 1200, 9000);
  // The pane starts above the fold and ends below it: only the middle band is
  // reachable by a pointer, so only the middle band can hold an edge zone.
  withRect(pane, { top: -300, bottom: 900, height: 1200 });

  assert.deepEqual(
    plainCopy(reorder.scrollViewRectForContainer(container)),
    { top: 0, bottom: 600, height: 600 },
  );
});

test("auto-scroll: a drag near the bottom of the screen scrolls the page scroller", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  env.context.innerHeight = 600;
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");

  const { container, items } = buildList(env, 3, { handle: true });
  withRect(container, { top: 0, bottom: 4000, height: 4000 });
  withRect(items[0], { top: 0, height: 40, bottom: 40 });
  withRect(items[1], { top: 40, height: 40, bottom: 80 });
  withRect(items[2], { top: 80, height: 40, bottom: 120 });
  const scroller = env.document.documentElement;
  scroller.scrollTop = 0;

  const handle = items[0].__handle;
  pointerDown(env, handle, { pointerId: 5, clientY: 20 });
  pointerMove(handle, { pointerId: 5, clientY: 595 });

  timers.runInterval(16);
  assert.ok(scroller.scrollTop > 0, "the page scroller moved down");
  const afterOneTick = scroller.scrollTop;
  timers.runInterval(16);
  assert.ok(scroller.scrollTop > afterOneTick, "each tick keeps scrolling while the finger stays at the edge");

  // A finger back in the dead zone stops the scroll instead of drifting.
  pointerMove(handle, { pointerId: 5, clientY: 300 });
  const beforeIdleTick = scroller.scrollTop;
  timers.runInterval(16);
  assert.equal(scroller.scrollTop, beforeIdleTick);

  handle.dispatchEvent({ type: "pointercancel", pointerId: 5 });
  await flushAsyncWork();
});

test("auto-scroll: a container may widen or slow its own edge zones", () => {
  const env = createContext({ elements: [] });
  env.context.innerHeight = 600;
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const reorder = reorderRuntime(env);
  const view = { top: 0, bottom: 600, height: 600 };

  // The two overrides are read straight off the container by the drag; the
  // pure function takes them as arguments so both can be checked here.
  assert.equal(reorder.autoScrollDeltaForPointer(500, view), 0, "outside the default 48px zone");
  assert.ok(reorder.autoScrollDeltaForPointer(500, view, 200, 18) > 0, "inside a 200px zone");
  assert.equal(reorder.autoScrollDeltaForPointer(600, view, 200, 4), 4, "a slower cap is honored");
});

// --- activation constraint ---------------------------------------------------

test("activation: a tap never grabs and never submits", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });
  withRect(container, { top: 0, bottom: 400, height: 400 });
  const handle = items[0].__handle;

  pointerDown(env, handle, { pointerId: 2, clientY: 20 });
  // Two pixels of jitter is a tap, not a drag.
  pointerMove(handle, { pointerId: 2, clientY: 22 });
  handle.dispatchEvent({ type: "pointerup", pointerId: 2, clientY: 22 });
  await flushAsyncWork();

  assert.equal(container.getAttribute("class"), null, "nothing was ever lifted");
  assert.equal(container.children.some((c) => c.hasAttribute("data-gosx-reorder-placeholder")), false);
  assert.equal(items[0].getAttribute("data-gosx-reorder-grabbed"), null);
  assert.equal(env.fetchCalls.length, 0);
  assert.equal(handle._capturedPointerID, null, "the press released the pointer it captured");
});

test("touch: a dedicated handle drags on the same 5px activation as a mouse", async () => {
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
  const down = pointerDown(env, handle, { pointerId: 11, pointerType: "touch", clientY: 20 });
  assert.equal(down.defaultPrevented, true, "a dedicated handle owns the gesture from pointerdown");

  pointerMove(handle, { pointerId: 11, clientY: 105 });
  assert.equal(container.getAttribute("class"), "gosx-reorder--dragging", "no hold is required on a real handle");

  handle.dispatchEvent({ type: "pointerup", pointerId: 11, clientY: 105 });
  await flushAsyncWork();

  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-1", "item-2", "item-0"],
  );
  assert.equal(env.fetchCalls.length, 1);
});

test("touch: a whole-item row scrolls natively and only drags after a hold", async () => {
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/api/board/reorder": { text: JSON.stringify({ ok: true }) },
    },
  });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, {});
  withRect(container, { top: 0, bottom: 400, height: 400 });
  withRect(items[0], { top: 0, height: 40, bottom: 40 });
  withRect(items[1], { top: 40, height: 40, bottom: 80 });
  withRect(items[2], { top: 80, height: 40, bottom: 120 });

  const row = items[0];
  const down = pointerDown(env, row, { pointerId: 21, pointerType: "touch", clientY: 20 });
  assert.equal(down.defaultPrevented, false, "the browser keeps its scroll gesture on a whole-item row");
  assert.equal(row.style.touchAction, undefined);

  // A finger that moves before the hold elapses is scrolling the list.
  pointerMove(row, { pointerId: 21, clientY: 90 });
  assert.equal(container.getAttribute("class"), null, "a scroll never became a drag");
  assert.equal(row._capturedPointerID, null, "the pointer went back to the browser");

  timers.runDelay(250);
  assert.equal(container.getAttribute("class"), null, "an abandoned press cannot lift later");

  // A finger held still through the hold lifts the row instead.
  pointerDown(env, row, { pointerId: 22, pointerType: "touch", clientY: 20 });
  assert.equal(container.getAttribute("class"), null, "still only a press");
  timers.runDelay(250);
  await flushAsyncWork();
  assert.equal(container.getAttribute("class"), "gosx-reorder--dragging", "the hold lifted the row");
  assert.equal(row.getAttribute("data-gosx-reorder-grabbed"), "true");

  pointerMove(row, { pointerId: 22, clientY: 105 });
  row.dispatchEvent({ type: "pointerup", pointerId: 22, clientY: 105 });
  await flushAsyncWork();
  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-1", "item-2", "item-0"],
  );
});

test("touch: a whole-item row under a MOUSE needs no hold", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, {});
  withRect(container, { top: 0, bottom: 400, height: 400 });
  withRect(items[0], { top: 0, height: 40, bottom: 40 });
  withRect(items[1], { top: 40, height: 40, bottom: 80 });
  withRect(items[2], { top: 80, height: 40, bottom: 120 });

  const row = items[0];
  pointerDown(env, row, { pointerId: 31, pointerType: "mouse", clientY: 20 });
  pointerMove(row, { pointerId: 31, clientY: 105 });
  assert.equal(container.getAttribute("class"), "gosx-reorder--dragging");
  assert.equal(timers.count(), 1, "a mouse press arms no hold timer, only the scroll interval");

  row.dispatchEvent({ type: "pointercancel", pointerId: 31 });
  await flushAsyncWork();
});

test("touch: pointercancel during the hold gives the gesture back to the browser", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  const timers = installManualTimers(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, {});
  const row = items[0];

  pointerDown(env, row, { pointerId: 41, pointerType: "touch", clientY: 20 });
  row.dispatchEvent({ type: "pointercancel", pointerId: 41 });
  timers.runDelay(250);
  await flushAsyncWork();

  assert.equal(container.getAttribute("class"), null);
  assert.equal(row._capturedPointerID, null);
  assert.equal(lastAnnouncement(env), "", "a press that never lifted announces nothing");
});

// --- frame budget --------------------------------------------------------------

test("performance: pointermove never measures; a frame measures at most once", () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  const frames = installManualRAF(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });

  let measurements = 0;
  withRect(container, { top: 0, bottom: 400, height: 400 });
  const rects = [
    { top: 0, height: 40, bottom: 40 },
    { top: 40, height: 40, bottom: 80 },
    { top: 80, height: 40, bottom: 120 },
  ];
  items.forEach((item, index) => {
    item.getBoundingClientRect = () => {
      measurements += 1;
      return rects[index];
    };
  });

  const handle = items[0].__handle;
  pointerDown(env, handle, { pointerId: 51, clientY: 20 });
  pointerMove(handle, { pointerId: 51, clientY: 30 });
  const afterLift = measurements;
  assert.ok(afterLift > 0, "the drag measures once when it starts");

  // A finger delivers moves far faster than the display refreshes. None of
  // them may read layout.
  for (let y = 40; y <= 120; y += 10) {
    pointerMove(handle, { pointerId: 51, clientY: y });
  }
  assert.equal(measurements, afterLift, "no pointermove reads layout");
  assert.equal(items[0].style.transform, "translateY(100px)", "the lifted item still tracks every move");
  assert.equal(frames.count(), 1, "however many moves arrive, one frame is scheduled");

  frames.flush();
  assert.equal(measurements, afterLift, "the frame decides from the rects it already holds");
  const placeholder = container.children[container.children.length - 1];
  assert.ok(placeholder.hasAttribute("data-gosx-reorder-placeholder"), "the frame moved the placeholder to the end");

  // Moving the placeholder is the ONE event that shifts every sibling, so the
  // next frame — and only the next frame — re-reads layout.
  pointerMove(handle, { pointerId: 51, clientY: 121 });
  frames.flush();
  assert.ok(measurements > afterLift, "a placeholder move invalidates the cached rects");
  const afterInvalidation = measurements;

  // The placeholder did not move that time, so nothing is stale.
  pointerMove(handle, { pointerId: 51, clientY: 122 });
  frames.flush();
  assert.equal(measurements, afterInvalidation, "a frame with no placeholder move re-reads nothing");

  handle.dispatchEvent({ type: "pointercancel", pointerId: 51 });
});

test("performance: a drop lands where the finger was, even inside one frame", async () => {
  const env = createContext({
    elements: [],
    fetchRoutes: {
      "http://localhost:3000/api/board/reorder": { text: JSON.stringify({ ok: true }) },
    },
  });
  installManualRAF(env.context);
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { container, items } = buildList(env, 3, { handle: true });
  withRect(container, { top: 0, bottom: 400, height: 400 });
  withRect(items[0], { top: 0, height: 40, bottom: 40 });
  withRect(items[1], { top: 40, height: 40, bottom: 80 });
  withRect(items[2], { top: 80, height: 40, bottom: 120 });

  // Press, flick to the end, and release without ever letting a frame run.
  const handle = items[0].__handle;
  pointerDown(env, handle, { pointerId: 61, clientY: 20 });
  pointerMove(handle, { pointerId: 61, clientY: 105 });
  handle.dispatchEvent({ type: "pointerup", pointerId: 61, clientY: 105 });
  await flushAsyncWork();

  assert.deepEqual(
    container.children.map((c) => c.getAttribute("data-gosx-reorder-item")),
    ["item-1", "item-2", "item-0"],
  );
  const body = new URLSearchParams(env.fetchCalls[0].init.body.toString());
  assert.equal(body.get("index"), "2");
});

// --- keyboard visibility ---------------------------------------------------------

test("keyboard: every move scrolls the item into view and returns focus to the handle", async () => {
  const env = createContext({ elements: [], fetchRoutes: {} });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const { items } = buildList(env, 4, { handle: true });
  const item = items[0];
  const handle = item.__handle;

  await keydown(env, handle, " ");
  const focusesAfterGrab = handle.focusCalls.length;

  await keydown(env, handle, "ArrowDown");
  assert.equal(item.scrollIntoViewCalls.length, 1);
  assert.deepEqual(plainCopy(item.scrollIntoViewCalls[0][0]), { block: "nearest", inline: "nearest" });
  assert.equal(handle.focusCalls.length, focusesAfterGrab + 1, "moving the node dropped focus, so the move restores it");
  assert.deepEqual(plainCopy(handle.focusCalls[handle.focusCalls.length - 1][0]), { preventScroll: true });

  await keydown(env, handle, "ArrowDown");
  assert.equal(item.scrollIntoViewCalls.length, 2);
  assert.equal(handle.focusCalls.length, focusesAfterGrab + 2);

  // A refused move — already at the end of the list — scrolls nothing.
  await keydown(env, handle, "ArrowUp");
  await keydown(env, handle, "ArrowUp");
  const scrollsAtTop = item.scrollIntoViewCalls.length;
  await keydown(env, handle, "ArrowUp");
  assert.equal(item.scrollIntoViewCalls.length, scrollsAtTop);
});
