"use strict";

// Declarative fixed-target transfer (data-gosx-transfer, gosx#250): stable
// source/target payloads, pointer/touch and keyboard interaction, source-
// specific eligibility, authoritative managed-action state, cancellation,
// and the in-flight double-submit guard. A transfer deliberately never
// relocates either node in the DOM.

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  navigationSource,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

function response(message, ok = true, status = 200) {
  return {
    ok,
    status,
    text: JSON.stringify(Object.assign({ ok }, message || {})),
  };
}

function buildTransfer(options) {
  const opts = options || {};
  const root = new FakeElement("section", null);
  root.setAttribute("data-gosx-transfer", opts.marker === undefined ? "true" : opts.marker);
  root.setAttribute(
    "data-gosx-transfer-action",
    opts.action === undefined ? "POST /team/actions/lineup-set" : opts.action,
  );
  if (opts.context) root.setAttribute("data-gosx-transfer-context", opts.context);
  if (opts.csrf) root.setAttribute("data-gosx-csrf-token", opts.csrf);
  if (opts.sourceField) root.setAttribute("data-gosx-transfer-source-field", opts.sourceField);
  if (opts.targetField) root.setAttribute("data-gosx-transfer-target-field", opts.targetField);

  const sources = [];
  for (const item of opts.sources || [{ id: "player-7", label: "Player 7", handle: true }]) {
    const source = new FakeElement("article", null);
    source.setAttribute("data-gosx-transfer-source", item.id);
    source.setAttribute("aria-label", item.label || item.id);
    let handle = source;
    if (item.handle !== false) {
      handle = new FakeElement("button", null);
      handle.setAttribute("type", "button");
      handle.setAttribute("data-gosx-transfer-handle", "true");
      handle.setAttribute("aria-label", item.label || item.id);
      source.appendChild(handle);
    }
    if (item.handleAttrs) {
      for (const [name, value] of Object.entries(item.handleAttrs)) handle.setAttribute(name, value);
    }
    source.textContent = item.label || item.id;
    if (handle !== source) source.appendChild(handle);
    source.__handle = handle;
    root.appendChild(source);
    sources.push(source);
  }

  const targets = [];
  for (const item of opts.targets || [
    { id: "QB", label: "Quarterback", rect: { left: 120, top: 0, width: 100, height: 60 } },
    { id: "RB", label: "Running back", rect: { left: 120, top: 70, width: 100, height: 60 } },
  ]) {
    const target = new FakeElement(item.tagName || "button", null);
    target.setAttribute("data-gosx-transfer-target", item.id);
    target.setAttribute("aria-label", item.label || item.id);
    if (item.attrs) {
      for (const [name, value] of Object.entries(item.attrs)) target.setAttribute(name, value);
    }
    const rect = item.rect || { left: 0, top: 0, width: 100, height: 50 };
    target.getBoundingClientRect = () => ({
      left: rect.left,
      top: rect.top,
      width: rect.width,
      height: rect.height,
      right: rect.right === undefined ? rect.left + rect.width : rect.right,
      bottom: rect.bottom === undefined ? rect.top + rect.height : rect.bottom,
    });
    target.textContent = item.label || item.id;
    root.appendChild(target);
    targets.push(target);
  }
  return { root, sources, targets };
}

function pointer(env, type, target, options) {
  const event = Object.assign({
    type,
    target,
    pointerId: 7,
    pointerType: "touch",
    isPrimary: true,
    button: 0,
    clientX: 0,
    clientY: 0,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  }, options || {});
  if (type === "pointerdown") env.document.dispatchEvent(event);
  else target.dispatchEvent(event);
  return event;
}

async function keydown(env, target, key, options) {
  const event = Object.assign({
    type: "keydown",
    target,
    key,
    shiftKey: false,
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  }, options || {});
  env.document.dispatchEvent(event);
  await flushAsyncWork();
  return event;
}

function lastAnnouncement(env) {
  const region = env.document.querySelector("[data-gosx-announcer]");
  return region ? region.textContent : "";
}

function bodyFields(call) {
  return new Map(call.init.body.values.map(([name, value]) => [name, value]));
}

test("pointer touch transfer posts stable identities and leaves DOM order authoritative", async () => {
  const built = buildTransfer({ context: "team_id=team-1&week=1", csrf: "csrf-7" });
  const env = createContext({
    elements: [built.root],
    maxTouchPoints: 5,
    fetchRoutes: {
      "http://localhost:3000/team/actions/lineup-set": response({ message: "Assigned" }),
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const sourceOrder = built.root.children.slice();
  const handle = built.sources[0].__handle;

  const down = pointer(env, "pointerdown", handle, { clientX: 10, clientY: 10 });
  assert.equal(down.defaultPrevented, true);
  assert.equal(handle.style.touchAction, "none");
  assert.equal(built.root.getAttribute("class"), "gosx-transfer--active");
  assert.equal(built.sources[0].getAttribute("class"), "gosx-transfer-source--active");

  pointer(env, "pointermove", handle, { clientX: 150, clientY: 20 });
  assert.equal(built.targets[0].getAttribute("class"), "gosx-transfer-target--over");
  assert.deepEqual(built.root.children, sourceOrder, "transfer never relocates source or target nodes");
  pointer(env, "pointerup", handle, { clientX: 150, clientY: 20 });
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  assert.equal(env.fetchCalls[0].url, "http://localhost:3000/team/actions/lineup-set");
  assert.equal(env.fetchCalls[0].init.method, "POST");
  assert.deepEqual(Object.fromEntries(bodyFields(env.fetchCalls[0])), {
    csrf_token: "csrf-7",
    team_id: "team-1",
    week: "1",
    player_id: "player-7",
    slot: "QB",
  });
  assert.equal(env.fetchCalls[0].init.headers["X-CSRF-Token"], "csrf-7");
  assert.equal(built.root.getAttribute("data-gosx-pending"), null);
  assert.equal(built.root.getAttribute("data-gosx-form-state"), "success");
  assert.equal(built.root.getAttribute("class"), "");
  assert.equal(handle.getAttribute("aria-grabbed"), "false");
  assert.equal(lastAnnouncement(env), "Assigned");
});

test("keyboard transfer cycles eligible targets with Tab direction and source-specific policy", async () => {
  const built = buildTransfer({
    sources: [{ id: "wr-7", label: "Wide receiver", handle: false }],
    targets: [
      { id: "WR", label: "WR slot", attrs: { "data-gosx-transfer-eligible-for": "wr-7" } },
      { id: "FLEX", label: "Flex slot", attrs: { "data-gosx-transfer-eligible-for": "wr-7" } },
      { id: "QB", label: "Quarterback", attrs: { "data-gosx-transfer-eligible-for": "qb-2" } },
    ],
  });
  const env = createContext({
    elements: [built.root],
    fetchRoutes: {
      "http://localhost:3000/team/actions/lineup-set": response({}),
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  const source = built.sources[0];
  const transfer = env.context.__gosx.transfer;
  assert.deepEqual(
    Array.from(transfer.eligibleTargets(built.root, source)).map((target) => target.getAttribute("data-gosx-transfer-target")),
    ["WR", "FLEX"],
  );

  await keydown(env, source, " ");
  assert.equal(built.targets[0].getAttribute("class"), "gosx-transfer-target--over");
  await keydown(env, source, "Tab");
  assert.equal(built.targets[1].getAttribute("class"), "gosx-transfer-target--over");
  await keydown(env, source, "Tab", { shiftKey: true });
  assert.equal(built.targets[0].getAttribute("class"), "gosx-transfer-target--over");
  await keydown(env, source, "ArrowDown");
  assert.equal(built.targets[1].getAttribute("class"), "gosx-transfer-target--over");
  await keydown(env, source, "Enter");
  await flushAsyncWork();

  assert.equal(env.fetchCalls.length, 1);
  const fields = bodyFields(env.fetchCalls[0]);
  assert.equal(fields.get("player_id"), "wr-7");
  assert.equal(fields.get("slot"), "FLEX");
  assert.deepEqual(built.root.children, [source, ...built.targets]);
});

test("disabled handles, locked targets, and opt-out roots never start a transfer", async () => {
  const disabledHandle = buildTransfer({
    sources: [{ id: "player-1", label: "Player 1", handle: true, handleAttrs: { "aria-disabled": "true" } }],
    targets: [{ id: "QB", label: "Quarterback" }],
  });
  const env = createContext({ elements: [disabledHandle.root], fetchRoutes: {} });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await keydown(env, disabledHandle.sources[0].__handle, "Enter");
  assert.equal(disabledHandle.root.getAttribute("class"), null);
  pointer(env, "pointerdown", disabledHandle.sources[0].__handle);
  assert.equal(env.fetchCalls.length, 0);

  const optOut = buildTransfer({ marker: "false" });
  const env2 = createContext({ elements: [optOut.root], fetchRoutes: {} });
  runScript(navigationSource, env2.context, "navigation_runtime.js");
  const handle = optOut.sources[0].__handle;
  await keydown(env2, handle, " ");
  pointer(env2, "pointerdown", handle);
  assert.equal(env2.fetchCalls.length, 0);
  assert.equal(handle.getAttribute("data-gosx-transfer-handle-ready"), null);

  const blocked = buildTransfer({
    targets: [
      { id: "LOCKED", label: "Locked", attrs: { "data-gosx-transfer-locked": "true" }, rect: { left: 0, top: 0, width: 100, height: 50 } },
      { id: "INELIGIBLE", label: "Ineligible", attrs: { "data-gosx-transfer-eligible": "false" }, rect: { left: 0, top: 60, width: 100, height: 50 } },
      { id: "OPEN", label: "Open", rect: { left: 0, top: 120, width: 100, height: 50 } },
    ],
  });
  const env3 = createContext({ elements: [blocked.root], fetchRoutes: {} });
  runScript(navigationSource, env3.context, "navigation_runtime.js");
  await keydown(env3, blocked.sources[0].__handle, " ");
  assert.equal(blocked.targets[2].getAttribute("class"), "gosx-transfer-target--over");
  await keydown(env3, blocked.sources[0].__handle, "Escape");
  pointer(env3, "pointerdown", blocked.sources[0].__handle, { clientX: 5, clientY: 10 });
  pointer(env3, "pointermove", blocked.sources[0].__handle, { clientX: 5, clientY: 10 });
  assert.equal(blocked.targets[0].getAttribute("class"), null);
  pointer(env3, "pointerup", blocked.sources[0].__handle, { clientX: 5, clientY: 10 });
  assert.equal(env3.fetchCalls.length, 0);
});

test("failure clears transient state and distinguishes an unknown server outcome", async () => {
  const built = buildTransfer({});
  const env = createContext({
    elements: [built.root],
    fetchRoutes: {
      "http://localhost:3000/team/actions/lineup-set": response({ message: "Lineup is locked" }, false, 409),
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  pointer(env, "pointerdown", built.sources[0].__handle);
  pointer(env, "pointermove", built.sources[0].__handle, { clientX: 150, clientY: 20 });
  pointer(env, "pointerup", built.sources[0].__handle, { clientX: 150, clientY: 20 });
  await flushAsyncWork();
  assert.equal(built.root.getAttribute("data-gosx-form-state"), "error");
  assert.equal(built.root.getAttribute("data-gosx-pending"), null);
  assert.equal(built.root.getAttribute("class"), "");
  assert.equal(lastAnnouncement(env), "Lineup is locked");
  assert.ok(env.document.dispatchedEvents.some((event) => event.type === "gosx:transfer:error"));

  const unknown = buildTransfer({});
  const env2 = createContext({
    elements: [unknown.root],
    fetchRoutes: {
      "http://localhost:3000/team/actions/lineup-set": { ok: false, status: 503, text: "" },
    },
  });
  runScript(navigationSource, env2.context, "navigation_runtime.js");
  await keydown(env2, unknown.sources[0].__handle, " ");
  await keydown(env2, unknown.sources[0].__handle, "Enter");
  await flushAsyncWork();
  assert.equal(unknown.root.getAttribute("data-gosx-form-state"), "error");
  assert.equal(lastAnnouncement(env2), "Could not confirm transfer; refresh and check the current assignment.");
});

test("pending transfer blocks a second source and soft navigation/cancel cleans up", async () => {
  let resolveRequest;
  const built = buildTransfer({
    sources: [
      { id: "player-1", label: "Player 1", handle: true },
      { id: "player-2", label: "Player 2", handle: true },
    ],
  });
  const env = createContext({
    elements: [built.root],
    fetchRoutes: {
      "http://localhost:3000/team/actions/lineup-set": () => new Promise((resolve) => { resolveRequest = resolve; }),
    },
  });
  runScript(navigationSource, env.context, "navigation_runtime.js");
  await keydown(env, built.sources[0].__handle, " ");
  await keydown(env, built.sources[0].__handle, " ");
  assert.equal(built.root.getAttribute("data-gosx-pending"), "true");
  assert.equal(env.fetchCalls.length, 1);
  await keydown(env, built.sources[1].__handle, " ");
  assert.equal(env.fetchCalls.length, 1, "pending root refuses a second source");
  resolveRequest(response({ message: "Done" }));
  await flushAsyncWork();
  assert.equal(built.root.getAttribute("data-gosx-pending"), null);
  assert.equal(built.root.getAttribute("data-gosx-form-state"), "success");

  const cancel = buildTransfer({});
  const env2 = createContext({ elements: [cancel.root], fetchRoutes: {} });
  runScript(navigationSource, env2.context, "navigation_runtime.js");
  const handle = cancel.sources[0].__handle;
  pointer(env2, "pointerdown", handle);
  assert.equal(cancel.root.getAttribute("class"), "gosx-transfer--active");
  env2.document.dispatchEvent({ type: "gosx:navigate", detail: {} });
  assert.equal(cancel.root.getAttribute("class"), "");
  assert.equal(handle.getAttribute("aria-grabbed"), "false");
  pointer(env2, "pointerup", handle);
  assert.equal(env2.fetchCalls.length, 0);
});
