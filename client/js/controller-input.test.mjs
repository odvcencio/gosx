import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import { createContext, FakeTarget } from "./08-controllers.test.mjs";

const source = fs.readFileSync(new URL("../runtime/host/controller-input.ts", import.meta.url), "utf8");

function harness() {
  const env = createContext(), frames = new Map(), timeouts = new Map();
  let next = 0, physical = null;
  const { context, document } = env;
  context.window.requestAnimationFrame = fn => { const id = ++next; frames.set(id, fn); return id; };
  context.window.cancelAnimationFrame = id => frames.delete(id);
  context.setTimeout = fn => { const id = ++next; timeouts.set(id, fn); return id; };
  context.clearTimeout = id => timeouts.delete(id);
  document.elementFromPoint = () => physical;
  document.hidden = false;
  const elements = new Map();
  document.querySelector = selector => selector === "#app" ? document : elements.get(selector) || null;
  function element(selector, tag = "div", parent = null) {
    const el = new FakeTarget(tag);
    Object.assign(el, { selector, parentElement: parent, children: [], dataset: {}, attrs: {}, tabIndex: tag === "button" ? 0 : -1, inert: false });
    if (parent) parent.children.push(el);
    el.contains = target => { for (let p = target; p; p = p.parentElement) if (p === el) return true; return false; };
    el.closest = query => {
      for (let p = el; p; p = p.parentElement) {
        if (p.selector === query || p.tagName.toLowerCase() === query) return p;
        if (query === "[hidden],[inert]" && (p.inert || p.attrs.hidden != null)) return p;
      }
      return null;
    };
    el.querySelector = query => Array.from(elements.values()).find(node => node !== el && el.contains(node) && (node.selector === query || node.tagName.toLowerCase() === query)) || null;
    el.querySelectorAll = () => Array.from(elements.values()).filter(node => node !== el && el.contains(node) && node.tabIndex >= 0);
    el.hasAttribute = key => key in el.attrs;
    el.setAttribute = (key, value) => { el.attrs[key] = value; if (key === "tabindex") el.tabIndex = Number(value); };
    el.removeAttribute = key => { delete el.attrs[key]; if (key === "tabindex") el.tabIndex = -1; };
    el.getClientRects = () => [{}];
    el.focus = () => { document.activeElement = el; document.dispatchEvent({ type: "focusin", target: el }); };
    el.setPointerCapture = id => { el.capture = id; };
    el.releasePointerCapture = () => { el.capture = null; };
    elements.set(selector, el);
    return el;
  }
  const body = element("body"); document.body = body;
  context.loadScriptTag = async url => { env.loaded = url; vm.runInContext(source, context); };
  let currentConfig;
  context.window.__gosx.document = { get: () => ({ assets: { runtime: {
    bootstrapControllerInputPath: currentConfig && (currentConfig.drags?.length || currentConfig.focus?.length || currentConfig.storage ||
      ["events", "keys"].some(key => (currentConfig[key] || []).some(binding => binding.project))) ? "/gosx/bootstrap-controller-input.js" : "",
  } } }) };
  env.mount = async config => {
    currentConfig = config;
    return context.window.__test_mountAllControllers({ controllers: [{ id: "input-0", config: { root: "#app", ...config } }] });
  };
  env.dispose = () => context.window.__gosx_dispose_controller("input-0");
  env.tick = () => { const pending = Array.from(frames.values()); frames.clear(); for (const fn of pending) fn(); };
  env.timeout = () => { const pending = Array.from(timeouts.values()); timeouts.clear(); for (const fn of pending) fn(); };
  env.emit = (target, type, fields = {}) => {
    const event = { type, preventDefault() { this.prevented = true; }, stopImmediatePropagation() { this.stopped = true; }, ...fields };
    target.dispatchEvent(event); return event;
  };
  return Object.assign(env, { element, body, frames, timeouts, physical: value => { physical = value; } });
}

test("input runtime loads only for configured contracts and uses the hashed asset URL", async () => {
  const h = harness(); await h.mount({ events: [{ type: "click", output: "$click" }] });
  assert.equal(h.loaded, undefined); assert.equal(h.frames.size, 0);
  h.context.window.__gosx.document = { get: () => ({ assets: { runtime: { bootstrapControllerInputPath: "/cdn/input.hash.js" } } }) };
  await h.mount({ focus: [{ target: "#modal", openSignal: "$open" }] });
  assert.equal(h.loaded, "/cdn/input.hash.js"); h.dispose(); assert.equal((h.subscribers.get("$open") || []).length, 0);
});

test("event and key projection produce typed intents and named DOM outputs", async () => {
  const h = harness(), intents = [];
  h.sharedValues.set("$view", { revision: 8 });
  h.document.addEventListener("app:intent", e => intents.push(e.detail));
  const project = { value: { kind: "play", tile: "", revision: 0 }, fields: { tile: "event.detail.input.targetID", revision: "inputs.view.revision" }, when: { "event.detail.kind": "pick", "event.detail.input.type": "select" } };
  await h.mount({ inputs: [{ name: "view", signal: "$view" }], outputs: [{ name: "intent", signal: "$intent", event: "app:intent" }],
    events: [{ type: "gosx:scene3d:input", output: "intent", project }],
    keys: [{ code: "Enter", output: "intent", project: { value: { kind: "confirm" }, fields: { revision: "inputs.view.revision" } } }] });
  h.emit(h.document, "gosx:scene3d:input", { detail: { kind: "pick", input: { targetID: "tile-7", type: "hover" } } });
  assert.equal(intents.length, 0);
  h.emit(h.document, "gosx:scene3d:input", { detail: { kind: "pick", input: { targetID: "tile-7", type: "select" } } });
  assert.deepEqual(JSON.parse(JSON.stringify(intents[0])), { kind: "play", tile: "tile-7", revision: 8 });
  assert.equal(intents[0].controllerId, undefined);
  h.emit(h.document, "gosx:scene3d:input", { detail: {} }); assert.equal(intents.length, 1);
  h.emit(h.document, "keydown", { code: "Enter", key: "Enter" }); assert.equal(intents[1].kind, "confirm");
  h.dispose(); h.emit(h.document, "keydown", { code: "Enter" }); assert.equal(intents.length, 2);
});

test("projection rejects prototype paths and preserves false and zero payloads", async () => {
  const h = harness();
  await h.mount({ events: [{ type: "payload", output: "$intent", project: { fields: { value: "event.detail.value" } } }] });
  for (const value of [false, 0]) { h.emit(h.document, "payload", { detail: { value } }); assert.equal(h.writes.at(-1).value.value, value); }
  const before = h.writes.length;
  await h.mount({ events: [{ type: "payload", output: "$intent", project: { fields: { value: "event.detail.constructor.prototype" } } }] });
  h.emit(h.document, "payload", { detail: {} }); assert.equal(h.writes.length, before);
});


test("drag uses the physical target despite pointer capture and snapshots revision at start", async () => {
  const h = harness(), tile = h.element(".tile", "button", h.body), drop = h.element(".end", "div", h.body);
  tile.dataset.tile = "tile-1"; h.sharedValues.set("$view", { revision: 3 }); h.physical(drop);
  await h.mount({ inputs: [{ name: "view", signal: "$view" }], drags: [{ source: ".tile", output: "$intent", targets: [{ target: ".end" }],
    project: { value: { kind: "play" }, fields: { tile: "drag.source.dataset.tile", revision: "drag.inputs.view.revision" } } }] });
  h.emit(h.document, "pointerdown", { target: tile, pointerId: 7, clientX: 0, clientY: 0 }); assert.equal(tile.capture, 7);
  h.context.setSharedSignalValue("$view", { revision: 4 });
  h.emit(h.document, "pointerup", { target: tile, pointerId: 8, clientX: 10, clientY: 10 }); assert.equal(h.writes.some(x => x.signal === "$intent"), false);
  h.emit(h.document, "pointerup", { target: tile, pointerId: 7, clientX: 10, clientY: 10 });
  assert.deepEqual(JSON.parse(JSON.stringify(h.writes.at(-1).value)), { kind: "play", tile: "tile-1", revision: 3 }); assert.equal(tile.capture, null);
  const before = h.writes.length;
  h.emit(h.document, "pointerdown", { target: tile, pointerId: 9, clientX: 0, clientY: 0 });
  h.emit(h.document, "pointerup", { target: tile, pointerId: 9, clientX: 1, clientY: 1 }); assert.equal(h.writes.length, before);
  h.dispose();
});

test("a tap ends its published drag phase without dropping", async () => {
  const h = harness(), tile = h.element(".tile", "button", h.body), drop = h.element(".end", "div", h.body);
  h.physical(drop);
  await h.mount({ drags: [{ source: ".tile", output: "$drop", startOutput: "$start", cancelOutput: "$cancel", targets: [{ target: ".end" }] }] });
  for (const pointerId of [1, 2]) {
    h.emit(h.document, "pointerdown", { target: tile, pointerId, clientX: 0, clientY: 0 });
    assert.equal(h.writes.at(-1).value.drag.phase, "start");
    h.emit(h.document, "pointerup", { target: tile, pointerId, clientX: 2, clientY: 1 });
    assert.equal(h.writes.at(-1).signal, "$cancel");
    assert.equal(h.writes.at(-1).value.drag.phase, "cancel");
    assert.equal(h.writes.at(-1).value.drag.reason, "tap");
    assert.equal(tile.capture, null);
  }
  assert.equal(h.writes.some(write => write.signal === "$drop"), false);
  const count = h.writes.length;
  h.dispose();
  assert.equal(h.writes.length, count);
});

test("scene drag accepts correlated native hits and ignores stale, cancelled and timed-out results", async () => {
  const h = harness(), tile = h.element(".tile", "button", h.body), scene = h.element("#board", "div", h.body);
  const requests = [];
  scene.addEventListener("gosx:scene3d:pick-request", e => {
    requests.push(e.detail.requestId);
    h.emit(h.document, "gosx:scene3d:input", { target: scene, detail: { kind: "ray", input: { requestId: e.detail.requestId,
      ray: { origin: { z: 4 }, direction: { z: -1 } }, hit: { id: "browser" } } } });
  });
  h.physical(scene);
  const config = { drags: [{ source: ".tile", output: "$drop", cancelOutput: "$cancel", targets: [{ target: "#board", scene: true,
    requestOutput: "$ray", resultSignal: "$hit", hitIds: ["end"], timeoutMs: 50 }] }] };
  const release = pointerId => { h.emit(h.document, "pointerdown", { target: tile, pointerId, clientX: 0, clientY: 0 }); h.emit(h.document, "pointerup", { target: tile, pointerId, clientX: 10, clientY: 10 }); };
  await h.mount(config); release(1);
  assert.equal(h.writes.at(-1).signal, "$ray"); assert.equal(h.writes.at(-1).value.ray.direction.z, -1);
  h.context.setSharedSignalValue("$hit", { requestId: "stale", hit: { id: "end" } }); assert.equal(h.writes.some(x => x.signal === "$drop"), false);
  h.context.setSharedSignalValue("$hit", { requestId: requests[0], hit: { id: "end", distance: 3, point: { z: 1 } } });
  assert.equal(h.writes.at(-1).value.drag.hit.id, "end"); assert.equal(h.timeouts.size, 0);
  release(2); h.emit(h.context.window, "blur");
  h.context.setSharedSignalValue("$hit", { requestId: requests[1], hit: { id: "end" } });
  assert.equal(h.writes.filter(x => x.signal === "$drop").length, 1);
  release(3); h.timeout(); assert.equal(h.writes.at(-1).value.drag.reason, "timeout");
  await h.mount(config); release(4); assert.notEqual(requests[3], requests[0]);
  h.context.setSharedSignalValue("$hit", { requestId: requests[0], hit: { id: "end" } }); assert.equal(h.writes.filter(x => x.signal === "$drop").length, 1);
  h.context.setSharedSignalValue("$hit", { requestId: requests[3], hit: null }); assert.equal(h.writes.at(-1).value.drag.reason, "miss");
  h.dispose(); assert.equal(h.timeouts.size, 0); assert.equal((h.subscribers.get("$hit") || []).length, 0);
});

test("modal focus traps both Tab directions, restores focus and preserves background inert state", async () => {
  const h = harness(), background = h.element("#background", "div", h.body), open = h.element("#open", "button", background);
  const preserved = h.element("#preserved", "div", h.body); preserved.inert = true;
  const modal = h.element("#modal", "div", h.body), first = h.element("#first", "button", modal), last = h.element("#last", "button", modal);
  open.focus(); h.sharedValues.set("$open", false);
  await h.mount({ focus: [{ target: "#modal", openSignal: "$open", initialFocus: "#first" }] });
  h.context.setSharedSignalValue("$open", true); await Promise.resolve();
  assert.equal(h.document.activeElement, first); assert.equal(background.inert, true);
  last.focus(); const forward = h.emit(h.document, "keydown", { key: "Tab" }); assert.equal(forward.prevented, true); assert.equal(h.document.activeElement, first);
  const backward = h.emit(h.document, "keydown", { key: "Tab", shiftKey: true }); assert.equal(backward.prevented, true); assert.equal(h.document.activeElement, last);
  open.focus(); assert.equal(h.document.activeElement, first);
  h.emit(h.document, "keydown", { key: "Escape" }); assert.equal(h.sharedValues.get("$open"), false);
  assert.equal(h.document.activeElement, open); assert.equal(background.inert, false); assert.equal(preserved.inert, true); assert.equal(modal.hasAttribute("tabindex"), false);
  h.context.setSharedSignalValue("$open", true); await Promise.resolve(); h.dispose();
  assert.equal(h.document.activeElement, open); assert.equal(background.inert, false);
});

test("InitialFocus selects the last control only on opening, while Tab wraps across every control", async () => {
  const h = harness(), modal = h.element("#modal", "div", h.body);
  const first = h.element("#first", "button", modal), last = h.element("#last", "button", modal);
  await h.mount({ focus: [{ target: "#modal", openSignal: "$open", initialFocus: "#last" }] });
  h.context.setSharedSignalValue("$open", true); await Promise.resolve();
  assert.equal(h.document.activeElement, last);
  h.emit(h.document, "keydown", { key: "Tab" });
  assert.equal(h.document.activeElement, first);
  h.emit(h.document, "keydown", { key: "Tab", shiftKey: true });
  assert.equal(h.document.activeElement, last);
  h.context.setSharedSignalValue("$open", false);
  h.context.setSharedSignalValue("$open", true); await Promise.resolve();
  assert.equal(h.document.activeElement, last);
  h.dispose();
});

test("nested focus owners restore the previous modal and cancelled opens never steal focus", async () => {
  const h = harness(), open = h.element("#open", "button", h.body), outer = h.element("#outer", "div", h.body);
  const outerButton = h.element("#outerButton", "button", outer), inner = h.element("#inner", "div", h.body);
  const innerButton = h.element("#innerButton", "button", inner); open.focus();
  await h.mount({ focus: [{ target: "#outer", openSignal: "$outer" }, { target: "#inner", openSignal: "$inner" }] });
  h.context.setSharedSignalValue("$outer", true); await Promise.resolve(); assert.equal(h.document.activeElement, outerButton);
  h.context.setSharedSignalValue("$inner", true); await Promise.resolve(); assert.equal(h.document.activeElement, innerButton); assert.equal(inner.inert, false);
  h.context.setSharedSignalValue("$inner", false); assert.equal(h.document.activeElement, outerButton);
  h.context.setSharedSignalValue("$outer", false); assert.equal(h.document.activeElement, open);
  h.context.setSharedSignalValue("$inner", true); h.context.setSharedSignalValue("$inner", false); await Promise.resolve(); assert.equal(h.document.activeElement, open);
  h.dispose();
});
