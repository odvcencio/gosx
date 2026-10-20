const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const {createContext, runScript, FakeElement, installManualTimers, flushAsyncWork} = require("../../js/runtime-test-harness.js");
const sourceCode = fs.readFileSync(path.join(__dirname, "hub-document.ts"), "utf8");

async function editor() {
  const root = new FakeElement("section"); root.id = "notes";
  const source = new FakeElement("textarea"); source.setAttribute("data-gosx-document-source", ""); source.value = "hello";
  source.scrollTop = 0; source.scrollLeft = 0;
  source.setSelectionRange = function(start, end, direction) { this.selectionStart = start; this.selectionEnd = end; this.selectionDirection = direction; };
  const nodes = {};
  for (const name of ["preview", "status", "version", "presence", "identity", "cursors"]) {
    nodes[name] = new FakeElement("div"); nodes[name].setAttribute("data-gosx-document-" + name, ""); root.appendChild(nodes[name]);
  }
  root.appendChild(source);
  const env = createContext({elements: [root]});
  const create = env.document.createElement.bind(env.document);
  env.document.createElement = tag => {
    const node = create(tag); node.style.setProperty = (name, value) => { node.style[name] = value; }; return node;
  };
  const timers = installManualTimers(env.context);
  let now = 100000;
  env.context.Date = class extends Date { static now() { return now; } };
  env.context.TextEncoder = TextEncoder;
  env.context.requestAnimationFrame = callback => env.context.setTimeout(() => callback(now), 16);
  env.context.cancelAnimationFrame = env.context.clearTimeout;
  const sent = [];
  const socket = {readyState: 1, send(raw) { sent.push(JSON.parse(raw)); }};
  const record = {socket};
  env.context.__gosx = {hubs: new Map([["notes-hub", record]])};
  let factory;
  env.context.__gosx_register_bootstrap_feature = (name, value) => { assert.equal(name, "hub-document"); factory = value; };
  runScript(sourceCode, env.context, "hub-document.ts");
  const feature = factory();
  const config = {rootId: "notes", version: 0, debounceMs: 100, cursorThrottleMs: 60, cursorStaleMs: 15000, maxTextBytes: 65536,
    events: {update: "doc:update", edit: "doc:edit", sync: "doc:sync", cursor: "cursor:update", roster: "cursor:roster", leave: "cursor:leave", presence: "presence:count", self: "presence:self"}};
  feature.runtimeReady({hubs: [{id: "notes-hub", document: config}]});
  await flushAsyncWork();
  const emit = (event, data, id = "notes-hub") => env.document.dispatchEvent({type: "gosx:hub:event", detail: {hubID: id, event, data}});
  const input = value => { source.value = value; source.dispatchEvent({type: "input", target: source}); };
  return {...env, root, source, nodes, timers, sent, record, socket, feature, emit, input,
    edits: () => sent.filter(value => value.event === "doc:edit"),
    advance: milliseconds => { now += milliseconds; return timers.runDelay(milliseconds); }};
}

test("document view coalesces typing and preserves input newer than a delayed acknowledgement", async () => {
  const e = await editor();
  assert.equal(e.sent.filter(value => value.event === "doc:sync").length, 1);
  e.input("first"); e.advance(100);
  assert.deepEqual(e.edits()[0].data, {text: "first", version: 0});
  e.input("second");
  e.emit("doc:update", {text: "first", version: 1, html: "first preview"});
  assert.equal(e.source.value, "second");
  e.advance(100);
  assert.deepEqual(e.edits()[1].data, {text: "second", version: 1});
  e.emit("doc:update", {text: "second", version: 2, html: "<p>second</p>"});
  e.emit("__welcome", {});
  assert.equal(e.nodes.preview.innerHTML, "<p>second</p>");
  assert.equal(e.nodes.status.textContent, "synced");
  e.feature.disposePage(); assert.equal(e.timers.count(), 0);
});

test("document view defers submission and replacement during composition input", async () => {
  const e = await editor();
  e.source.dispatchEvent({type: "compositionstart"}); e.input("かな"); e.advance(100);
  e.emit("doc:update", {text: "remote", version: 1});
  assert.equal(e.source.value, "かな"); assert.equal(e.edits().length, 0);
  e.source.dispatchEvent({type: "compositionend"}); e.advance(100);
  assert.deepEqual(e.edits()[0].data, {text: "かな", version: 1});
  e.feature.disposePage(); assert.equal(e.timers.count(), 0);
});

test("document view keeps an offline draft and rebases it after reconnect", async () => {
  const e = await editor();
  e.socket.readyState = 3; e.emit("__connection", {state: "reconnecting"});
  e.input("offline draft"); e.advance(100);
  assert.match(e.nodes.status.textContent, /kept locally/);
  assert.equal(e.edits().length, 0);
  e.record.socket = {readyState: 1, send: e.socket.send}; e.emit("__connection", {state: "open"});
  e.emit("doc:update", {text: "remote while offline", version: 4}); e.advance(100);
  assert.deepEqual(e.edits()[0].data, {text: "offline draft", version: 4});
  e.emit("doc:update", {text: "offline draft", version: 5});
  assert.equal(e.source.value, "offline draft"); assert.equal(e.nodes.status.textContent, "synced");
  e.feature.disposePage(); assert.equal(e.timers.count(), 0);
});

test("document view ignores unrelated, stale, inexact and oversized snapshots", async () => {
  const e = await editor();
  e.emit("doc:update", {text: "wrong room", version: 1}, "other-hub");
  e.emit("doc:update", {text: "invalid", version: Number.MAX_SAFE_INTEGER + 1});
  e.emit("doc:update", {text: "too large".repeat(10000), version: 1});
  assert.equal(e.source.value, "hello");
  e.emit("doc:update", {text: "new", version: 3});
  e.emit("doc:update", {text: "old", version: 2});
  assert.equal(e.source.value, "new");
  e.feature.disposePage(); assert.equal(e.timers.count(), 0);
});

test("document view maps native UTF-16 selections and preserves scroll", async () => {
  const e = await editor();
  e.emit("doc:update", {text: "ab🌊cd", version: 1});
  e.source.setSelectionRange(4, 5, "backward"); e.source.scrollTop = 73;
  e.emit("doc:update", {text: "Hi ab🌊cd", version: 2});
  assert.equal(e.source.selectionStart, 7); assert.equal(e.source.selectionEnd, 8);
  assert.equal(e.source.selectionDirection, "backward"); assert.equal(e.source.scrollTop, 73);
  e.feature.disposePage(); assert.equal(e.timers.count(), 0);
});

test("document presence and bounded carets dispose listeners and scheduled work", async () => {
  const e = await editor();
  e.emit("presence:self", {id: "me", name: "Blue Otter", color: "#12aaff"});
  e.emit("presence:count", {count: 2});
  e.emit("cursor:update", {id: "me", offset: 1});
  e.emit("cursor:update", {id: "peer", name: "Other", color: "#abc", offset: 2});
  assert.equal(e.nodes.cursors.children.length, 1);
  assert.equal(e.nodes.identity.textContent, "You: Blue Otter"); assert.equal(e.nodes.presence.textContent, "2 editors");
  e.advance(16); e.advance(15000);
  assert.equal(e.nodes.cursors.children.length, 0);
  e.emit("cursor:update", {id: "peer", offset: 2});
  e.input("last edit");
  e.feature.disposePage();
  assert.equal(e.nodes.cursors.children.length, 0); assert.equal(e.timers.count(), 0);
  e.emit("doc:update", {text: "late response", version: 7});
  assert.equal(e.source.value, "last edit");
  assert.deepEqual(e.consoleLogs.error, []);
});

test("document view commits a pending edit before navigation disposes the transport", async () => {
  const e = await editor();
  e.input("typed just before navigating");
  assert.equal(e.edits().length, 0);
  e.document.dispatchEvent({type: "gosx:navigation:state", detail: {state: {phase: "pending"}}});
  assert.deepEqual(e.edits()[0].data, {text: "typed just before navigating", version: 0});
  e.socket.readyState = 3; e.feature.disposePage();
  assert.equal(e.timers.count(), 0);
});

test("document view bounds roster DOM and coalesces caret layout into one frame", async () => {
  const e = await editor();
  e.emit("cursor:roster", Array.from({length: 1000}, (_, i) => ({id: "peer-" + i, offset: 2})));
  assert.equal(e.nodes.cursors.children.length, 128);
  assert.equal(e.timers.count(), 2, "one caret frame and one expiry timer");
  e.advance(16);
  assert.equal(e.timers.count(), 1);
  e.feature.disposePage(); assert.equal(e.timers.count(), 0);
});
