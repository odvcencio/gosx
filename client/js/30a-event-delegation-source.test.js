"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  FakeElement,
  createContext,
  runScript,
} = require("./runtime-test-harness.js");

const source = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "30a-tail-event-delegation.js"),
  "utf8",
) + "\nwindow.__gosx_test_setup_event_delegation = setupEventDelegation;";

test("delegation source carries typed pointer/dataset context and clears current event globals", () => {
  const root = new FakeElement("div", null);
  const button = new FakeElement("button", null);
  root.id = "island-pointer";
  button.id = "pointer-target";
  button.setAttribute("data-gosx-on-pointerdown", "select");
  button.setAttribute("data-record-id", "r-7");
  root.appendChild(button);
  let contextDuringAction;
  const calls = [];
  const env = createContext({ elements: [root] });
  env.context.__gosx_action = (...args) => {
    calls.push(args);
    contextDuringAction = [env.context.__gosx_current_event, env.context.__gosx_current_handler];
    return 0;
  };
  runScript(source, env.context, "30a-tail-event-delegation.js");
  env.context.__gosx_test_setup_event_delegation(root, root.id);

  const event = {
    type: "pointerdown",
    target: button,
    pointerId: 4,
    clientX: 10,
    clientY: 20,
    buttons: 1,
    button: 0,
    pressure: 0,
    width: 0,
    height: 0,
    isPrimary: false,
    metaKey: true,
    timeStamp: 125.5,
  };
  root.dispatchEvent(event);

  assert.deepEqual(contextDuringAction, [event, button]);
  assert.equal(env.context.__gosx_current_event, undefined);
  assert.equal(env.context.__gosx_current_handler, undefined);
  assert.deepEqual(JSON.parse(calls[0][2]), {
    type: "pointerdown",
    targetID: "pointer-target",
    currentTargetID: "pointer-target",
    metaKey: true,
    pointerID: 4,
    clientX: 10,
    clientY: 20,
    buttons: 1,
    data: { recordId: "r-7" },
  });
});

test("delegation omits typed defaults and type-gates pointer numbers while retaining timestamps", () => {
  const root = new FakeElement("div", null);
  const button = new FakeElement("button", null);
  root.id = "compact";
  button.setAttribute("data-gosx-on-click", "click");
  root.setAttribute("data-gosx-on-document-keydown", "key");
  root.appendChild(button);
  button.checked = false;
  button.selectedIndex = 0;
  const env = createContext({ elements: [root] });
  const calls = [];
  env.context.__gosx_action = (...args) => { calls.push(args); return 0; };
  runScript(source, env.context, "30a-tail-event-delegation.js");
  env.context.__gosx_test_setup_event_delegation(root, root.id, [
    { eventType: "click" },
    { eventType: "documentKeyDown" },
  ]);

  root.dispatchEvent({
    type: "click",
    target: button,
    timeStamp: 0,
    clientX: 0,
    clientY: 0,
    button: 0,
    buttons: 0,
  });
  env.document.dispatchEvent({
    type: "keydown",
    target: env.document.body,
    key: "g",
    timeStamp: 1400,
    pointerId: 9,
    clientX: 13,
    buttons: 1,
  });

  assert.deepEqual(JSON.parse(calls[0][2]), { type: "click" });
  assert.deepEqual(JSON.parse(calls[1][2]), {
    type: "keydown",
    key: "g",
    currentTargetID: "compact",
    timeStamp: 1400,
  });
});

test("delegation stringifies form values and omits the empty value field", () => {
  const root = new FakeElement("div", null);
  const input = new FakeElement("input", null);
  root.id = "value-contract";
  input.setAttribute("data-gosx-on-input", "update");
  input.setAttribute("data-gosx-on-change", "commit");
  root.appendChild(input);
  const env = createContext({ elements: [root] });
  const calls = [];
  env.context.__gosx_action = (...args) => { calls.push(args); return 0; };
  runScript(source, env.context, "30a-tail-event-delegation.js");
  env.context.__gosx_test_setup_event_delegation(root, root.id, [
    { eventType: "input" },
    { eventType: "change" },
  ]);

  input.value = 0;
  root.dispatchEvent({ type: "input", target: input });
  input.value = "";
  root.dispatchEvent({ type: "change", target: input });

  assert.deepEqual(JSON.parse(calls[0][2]), {
    type: "input",
    value: "0",
    editable: true,
  });
  assert.deepEqual(JSON.parse(calls[1][2]), {
    type: "change",
    editable: true,
  });
});

test("delegation source transfers drag text and prevents handled dragover/drop", () => {
  const root = new FakeElement("div", null);
  const card = new FakeElement("article", null);
  const lane = new FakeElement("section", null);
  root.id = "island-drag";
  card.setAttribute("data-gosx-on-dragstart", "start");
  card.setAttribute("data-gosx-event-value", "session-9");
  lane.setAttribute("data-gosx-on-dragover", "over");
  lane.setAttribute("data-gosx-on-drop", "drop");
  root.appendChild(card);
  root.appendChild(lane);
  const env = createContext({ elements: [root] });
  const calls = [];
  env.context.__gosx_action = (...args) => { calls.push(args); return 0; };
  runScript(source, env.context, "30a-tail-event-delegation.js");
  env.context.__gosx_test_setup_event_delegation(root, root.id);

  const transfer = {
    value: "",
    setData(_type, value) { this.value = value; },
    getData() { return this.value; },
  };
  root.dispatchEvent({ type: "dragstart", target: card, dataTransfer: transfer });
  let prevented = 0;
  root.dispatchEvent({ type: "dragover", target: lane, dataTransfer: transfer, preventDefault() { prevented++; } });
  root.dispatchEvent({ type: "drop", target: lane, dataTransfer: transfer, preventDefault() { prevented++; } });

  assert.equal(transfer.value, "session-9");
  assert.equal(prevented, 2);
  assert.deepEqual(calls.map((call) => [call[1], JSON.parse(call[2]).eventData]), [
    ["start", "session-9"],
    ["over", undefined],
    ["drop", "session-9"],
  ]);
});

test("delegation rejects oversized external drop text before JSON/WASM", () => {
  const root = new FakeElement("div", null);
  const authored = new FakeElement("section", null);
  const external = new FakeElement("section", null);
  root.id = "island-drop-limit";
  authored.setAttribute("data-gosx-on-drop", "authoredDrop");
  authored.setAttribute("data-gosx-event-value", "authored-value");
  external.setAttribute("data-gosx-on-drop", "externalDrop");
  root.appendChild(authored);
  root.appendChild(external);
  const env = createContext({ elements: [root] });
  const calls = [];
  env.context.__gosx_action = (...args) => { calls.push(args); return 0; };
  runScript(source, env.context, "30a-tail-event-delegation.js");
  env.context.__gosx_test_setup_event_delegation(root, root.id, [{ eventType: "drop" }]);

  const oversizedASCII = { getData() { return "x".repeat((64 * 1024) + 1); } };
  root.dispatchEvent({ type: "drop", target: authored, dataTransfer: oversizedASCII, preventDefault() {} });
  const oversizedUTF8 = { getData() { return "é".repeat(32769); } };
  root.dispatchEvent({ type: "drop", target: external, dataTransfer: oversizedUTF8, preventDefault() {} });
  const boundary = { getData() { return "x".repeat(64 * 1024); } };
  root.dispatchEvent({ type: "drop", target: external, dataTransfer: boundary, preventDefault() {} });

  assert.equal(JSON.parse(calls[0][2]).eventData, "authored-value");
  assert.equal(JSON.parse(calls[1][2]).eventData, undefined);
  assert.equal(JSON.parse(calls[2][2]).eventData.length, 64 * 1024);
});

test("delegation source fans global events out and records their actual listener targets", () => {
  const first = new FakeElement("div", null);
  const second = new FakeElement("div", null);
  first.id = "first";
  second.id = "second";
  first.setAttribute("data-gosx-on-document-keydown", "firstKey");
  second.setAttribute("data-gosx-on-document-keydown", "secondKey");
  const env = createContext({ elements: [first, second] });
  const calls = [];
  let stop = false;
  env.context.__gosx_action = (...args) => {
    calls.push(args);
    if (stop && args[1] === "firstKey") {
      env.context.__gosx_current_event.__gosx_stop_island_fanout = true;
    }
    return 0;
  };
  runScript(source, env.context, "30a-tail-event-delegation.js");
  const firstEntries = env.context.__gosx_test_setup_event_delegation(first, first.id);
  const secondEntries = env.context.__gosx_test_setup_event_delegation(second, second.id);

  assert.equal(firstEntries.find((entry) => entry.type === "keydown" && entry.target === env.document).target, env.document);
  assert.equal(secondEntries.find((entry) => entry.type === "keydown" && entry.target === env.document).target, env.document);
  env.document.dispatchEvent({ type: "keydown", target: env.document.body, key: "k" });
  assert.deepEqual(calls.map((call) => call[1]), ["firstKey", "secondKey"]);

  calls.length = 0;
  stop = true;
  env.document.dispatchEvent({ type: "keydown", target: env.document.body, key: "Escape" });
  assert.deepEqual(calls.map((call) => call[1]), ["firstKey"]);
});

test("delegation treats nested island roots as root and global ownership boundaries", () => {
  const outer = new FakeElement("div", null);
  outer.id = "outer";
  outer.setAttribute("data-gosx-island", "Outer");
  const outerButton = new FakeElement("button", null);
  outerButton.setAttribute("data-gosx-on-click", "outerClick");
  const nested = new FakeElement("section", null);
  nested.id = "nested";
  nested.setAttribute("data-gosx-island", "Nested");
  const nestedButton = new FakeElement("button", null);
  nestedButton.setAttribute("data-gosx-on-click", "nestedClick");
  const nestedGlobal = new FakeElement("span", null);
  nestedGlobal.setAttribute("data-gosx-on-document-keydown", "nestedGlobal");
  const ownedGlobal = new FakeElement("span", null);
  ownedGlobal.setAttribute("data-gosx-on-document-keydown", "outerGlobal");
  nested.appendChild(nestedButton);
  nested.appendChild(nestedGlobal);
  outer.appendChild(nested);
  outer.appendChild(outerButton);
  outer.appendChild(ownedGlobal);

  const env = createContext({ elements: [outer] });
  const calls = [];
  env.context.__gosx_action = (...args) => { calls.push(args); return 0; };
  runScript(source, env.context, "30a-tail-event-delegation.js");
  const entries = env.context.__gosx_test_setup_event_delegation(outer, outer.id, [
    { eventType: "click" },
    { eventType: "documentKeyDown" },
  ]);

  outer.dispatchEvent({ type: "click", target: nestedButton });
  assert.deepEqual(calls, []);
  outer.dispatchEvent({ type: "click", target: outerButton });
  assert.deepEqual(calls.map((call) => call[1]), ["outerClick"]);

  assert.equal(entries.filter((entry) => entry.target === env.document).length, 1);
  env.document.dispatchEvent({ type: "keydown", target: env.document.body, key: "k" });
  assert.deepEqual(calls.map((call) => call[1]), ["outerClick", "outerGlobal"]);
});

test("delegation source attaches only declared de-duplicated events with legacy fallback", () => {
  const selective = new FakeElement("div", null);
  selective.id = "selective";
  selective.setAttribute("data-gosx-on-document-keydown", "shortcut");
  const legacy = new FakeElement("div", null);
  legacy.id = "legacy";
  const explicitNone = new FakeElement("div", null);
  explicitNone.id = "none";
  const legacyNull = new FakeElement("div", null);
  legacyNull.id = "legacy-null";
  const env = createContext({ elements: [selective, legacy, explicitNone, legacyNull] });
  runScript(source, env.context, "30a-tail-event-delegation.js");

  const selectiveEntries = env.context.__gosx_test_setup_event_delegation(selective, selective.id, [
    { eventType: "pointerDown" },
    { eventType: "pointerdown" },
    { eventType: "documentKeyDown" },
  ]);
  const rootEntries = selectiveEntries.filter((entry) => entry.target === selective);
  const documentEntries = selectiveEntries.filter((entry) => entry.target === env.document);
  assert.deepEqual(Array.from(rootEntries, (entry) => entry.type), ["pointerdown"]);
  assert.deepEqual(Array.from(documentEntries, (entry) => entry.type), ["keydown"]);

  const legacyEntries = env.context.__gosx_test_setup_event_delegation(legacy, legacy.id);
  assert.equal(legacyEntries.filter((entry) => entry.target === legacy).length, 17);
  const modernEntry = JSON.parse('{"events":[]}');
  const oldNullEntry = JSON.parse('{"events":null}');
  assert.equal(env.context.__gosx_test_setup_event_delegation(explicitNone, explicitNone.id, modernEntry.events).length, 0);
  assert.equal(env.context.__gosx_test_setup_event_delegation(legacyNull, legacyNull.id, oldNullEntry.events).length, 17);
});
