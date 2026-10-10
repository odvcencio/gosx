import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { bootstrapRuntimeSource, createContext, runScript, flushAsyncWork, FakeElement } = require("./runtime-test-harness.js");
const here = path.dirname(fileURLToPath(import.meta.url));
const workbenchSource = fs.readFileSync(path.join(here, "bootstrap-feature-workbench.js"), "utf8");
export const WORKBENCH_URL = "/hashed/bootstrap-feature-workbench.abcd1234.js";
export { FakeElement, flushAsyncWork };

export function contractElement(assets) {
  const el = new FakeElement("script", null);
  el.id = "gosx-document";
  el.textContent = JSON.stringify({ version: 1, assets: { bootstrapMode: "full", manifest: true, ...assets } });
  return el;
}

export function bootWorkbench(extra = {}) {
  const env = createContext({
    elements: [contractElement({ bootstrapFeatureWorkbenchPath: WORKBENCH_URL, ...(extra.contract || {}) }), ...(extra.elements || [])],
    fetchRoutes: { [WORKBENCH_URL]: { text: workbenchSource }, ...(extra.fetchRoutes || {}) },
    manifest: { features: ["workbench"], commands: [], ...(extra.manifest || {}) },
  });
  if (extra.platform) env.context.navigator.platform = extra.platform;
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  return env;
}

export function keydown(env, fields) {
  const event = { type: "keydown", target: env.document, prevented: false,
    preventDefault() { this.prevented = true; }, stopPropagation() {}, ...fields };
  env.document.dispatchEvent(event);
  return event;
}

export const cmds = [
  { id: "edit.undo", title: "Undo", keys: ["Mod+Z"], action: { signal: "$edit", value: "undo" } },
  { id: "edit.redo", title: "Redo", keys: ["Mod+Shift+Z"], action: { signal: "$edit", value: "redo" } },
  { id: "transport.play", title: "Play", keys: ["Space"], when: { "$view": "arrange" }, action: { signal: "$transport", value: "toggle" } },
  { id: "transport.resume", title: "Resume", keys: ["Shift+Space"], action: { signal: "$transport", value: "resume" } },
  { id: "zoom.in", title: "Zoom in", keys: ["Plus"], action: { signal: "$zoom", value: 1 } },
  { id: "clip.consolidate", title: "Consolidate", keys: ["Mod+J"], reserved: true },
  { id: "edit.type", title: "Type", keys: ["Mod+T"], allowEditable: true, action: { signal: "$edit", value: "type" } },
];

export function subscribe(env, name) {
  const seen = [];
  env.context.__gosx_subscribe_shared_signal(name, v => seen.push(v), { immediate: false });
  return seen;
}

test("workbench loads through the hashed contract and installs the commands API", async () => {
  const env = bootWorkbench();
  await flushAsyncWork();
  assert.deepEqual(env.fetchCalls.map(c => String(c.url)), [WORKBENCH_URL]);
  assert.equal(env.context.__gosx.workbench.version, 1);
  for (const key of ["list", "run", "available", "platformMod", "chord"]) {
    assert.equal(typeof env.context.__gosx.commands[key], "function", key);
  }
  assert.equal(env.context.__gosx.ready, true);
});

test("exact chords, symbol Shift, When, and reserved commands dispatch once", async () => {
  const env = bootWorkbench({ manifest: { commands: cmds }, platform: "Win32" });
  await flushAsyncWork();
  const edits = subscribe(env, "$edit"), transport = subscribe(env, "$transport"), zoom = subscribe(env, "$zoom");
  const fired = [], unavailable = [];
  env.document.addEventListener("gosx:command", e => { fired.push(e.detail.id); assert.equal(typeof e.detail.at, "number"); });
  env.document.addEventListener("gosx:command:unavailable", e => unavailable.push(e.detail.id));
  assert.equal(keydown(env, { key: "z", ctrlKey: true }).prevented, true);
  keydown(env, { key: "z", ctrlKey: true, shiftKey: true });
  assert.deepEqual(edits, ["undo", "redo"]);
  for (const fields of [{ key: "z", metaKey: true }, { key: "z", ctrlKey: true, altKey: true }, { key: "z", ctrlKey: true, metaKey: true }]) {
    assert.equal(keydown(env, fields).prevented, false);
  }
  assert.equal(keydown(env, { key: " " }).prevented, false);
  env.context.__gosx.workbench.debug.setSignal("$view", "arrange");
  keydown(env, { key: " " });
  keydown(env, { key: " ", shiftKey: true });
  assert.deepEqual(transport, ["toggle", "resume"]);
  keydown(env, { key: "+", shiftKey: true, code: "Equal" });
  assert.deepEqual(zoom, [1]);
  assert.equal(keydown(env, { key: "j", ctrlKey: true }).prevented, true);
  assert.deepEqual(unavailable, ["clip.consolidate"]);
  assert.deepEqual(fired, ["edit.undo", "edit.redo", "transport.play", "transport.resume", "zoom.in"]);
  assert.equal(env.context.__gosx.commands.available("clip.consolidate"), false);
  assert.equal(env.context.__gosx.commands.run("clip.consolidate"), false);
  assert.equal(env.context.__gosx.commands.available("missing"), false);
});

test("Mod follows macOS and userAgentData takes precedence", async () => {
  const env = bootWorkbench({ manifest: { commands: cmds }, platform: "MacIntel" });
  await flushAsyncWork();
  const edits = subscribe(env, "$edit");
  keydown(env, { key: "z", metaKey: true });
  keydown(env, { key: "z", ctrlKey: true });
  assert.deepEqual(edits, ["undo"]);
  assert.equal(env.context.__gosx.commands.platformMod(), "meta");
  const phone = bootWorkbench({ manifest: { commands: cmds }, platform: "Win32" });
  phone.context.navigator.userAgentData = { platform: "iPad" };
  await flushAsyncWork();
  assert.equal(phone.context.__gosx.commands.platformMod(), "meta");
});

test("editable targets and already handled events preserve their keys", async () => {
  const env = bootWorkbench({ manifest: { commands: cmds }, platform: "Win32" });
  await flushAsyncWork();
  const edits = subscribe(env, "$edit");
  for (const tag of ["input", "textarea", "select", "div"]) {
    const target = new FakeElement(tag, null);
    if (tag === "div") target.setAttribute("contenteditable", "true");
    assert.equal(keydown(env, { key: "z", ctrlKey: true, target }).prevented, false);
    keydown(env, { key: "t", ctrlKey: true, target });
  }
  for (const fields of [{ defaultPrevented: true }, { __gosx_stop_island_fanout: true }]) {
    assert.equal(keydown(env, { key: "z", ctrlKey: true, ...fields }).prevented, false);
  }
  assert.deepEqual(edits, ["type", "type", "type", "type"]);
});

test("buttons narrow Mod ARIA shortcuts and reserved buttons stay disabled", async () => {
  const button = new FakeElement("button", null), reserved = new FakeElement("button", null);
  button.setAttribute("data-gosx-command", "edit.undo");
  button.setAttribute("aria-keyshortcuts", "Control+Z Meta+Z");
  reserved.setAttribute("data-gosx-command", "clip.consolidate");
  const child = new FakeElement("span", null);
  button.appendChild(child);
  const env = bootWorkbench({ elements: [button, reserved], manifest: { commands: cmds }, platform: "MacIntel" });
  await flushAsyncWork();
  assert.equal(button.getAttribute("aria-keyshortcuts"), "Meta+Z");
  assert.equal(reserved.getAttribute("aria-disabled"), "true");
  const edits = subscribe(env, "$edit");
  env.document.dispatchEvent({ type: "click", target: child, preventDefault() {} });
  env.document.dispatchEvent({ type: "click", target: reserved, preventDefault() {} });
  assert.deepEqual(edits, ["undo"]);
});

test("programmatic commands use deep When equality, defaults, and disclosures", async () => {
  const panel = new FakeElement("div", null);
  panel.id = "panel";
  const conditional = { id: "panel.open", title: "Open", group: "Panels", keys: ["Option+F9"], when: { "$state": { selected: [1, 2] } }, action: { signal: "$intent", open: "#panel" } };
  const env = bootWorkbench({ elements: [panel], manifest: { commands: [conditional, { id: "panel.close", title: "Close", action: { close: "#panel" } }] } });
  await flushAsyncWork();
  const api = env.context.__gosx.commands, intents = subscribe(env, "$intent"), calls = [];
  env.context.__gosx.disclosure = { open: el => calls.push(["open", el]), close: el => calls.push(["close", el]) };
  assert.equal(api.run("panel.open"), false);
  env.context.__gosx.workbench.debug.setSignal("$state", { selected: [1, 2] });
  assert.equal(api.available("panel.open"), true);
  assert.equal(api.list()[0].group, "Panels");
  assert.equal(api.list()[0].available, true);
  assert.equal(keydown(env, { key: "F9", altKey: true }).prevented, true);
  assert.equal(intents[0].id, "panel.open");
  assert.equal(typeof intents[0].at, "number");
  assert.equal(api.run("panel.close"), true);
  assert.deepEqual(calls, [["open", panel], ["close", panel]]);
});

test("page disposal removes command listeners and rebinding does not duplicate them", async () => {
  const env = bootWorkbench({ manifest: { commands: cmds }, platform: "Win32" });
  await flushAsyncWork();
  const edits = subscribe(env, "$edit");
  await env.context.__gosx_dispose_page();
  assert.equal(env.context.__gosx.commands.list().length, 0);
  assert.equal(keydown(env, { key: "z", ctrlKey: true }).prevented, false);
  await env.context.__gosx_bootstrap_page();
  await flushAsyncWork();
  keydown(env, { key: "z", ctrlKey: true });
  assert.deepEqual(edits, ["undo"]);
});

test("browser chord grammar mirrors the Go grammar", async () => {
  const env = bootWorkbench({ platform: "Win32" });
  await flushAsyncWork();
  const parse = env.context.__gosx.workbench.debug.parseChord;
  for (const [text, key] of [["Mod+Z", "z"], ["Cmd+K", "k"], ["Plus", "+"], ["Shift+Space", " "], ["Control+Up", "ArrowUp"], ["F9", "F9"], ["Shift+É", "é"]]) {
    assert.equal(parse(text).key, key, text);
  }
  for (const text of ["", "Mod", "Mod+", "Ctrl+Mod+Z", "Meta+Mod+Z", "Laser+Z", "Z+Mod", "Ctrl++", "Fno", "Shift+word"]) {
    assert.equal(parse(text), null, text);
  }
});
