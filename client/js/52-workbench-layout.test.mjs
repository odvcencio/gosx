import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { bootWorkbench, FakeElement, flushAsyncWork, keydown, subscribe } from "./50-workbench-commands.test.mjs";
import { bootHandles, handle, pointer, INPUT_URL } from "./51-workbench-drag.test.mjs";

export function styled(tag = "div") {
  const el = new FakeElement(tag, null); el.properties = new Map();
  el.style = { setProperty: (key, value) => el.properties.set(key, value), removeProperty: key => el.properties.delete(key) };
  return el;
}
function refreshDOM(env) {
  const observer = env.mutationObservers.findLast(o => o.options.some(({ target, options }) => target === env.document.body && options.childList));
  assert.ok(observer, "page observer"); observer.trigger([{ type: "childList", target: env.document.body }]);
}

test("clearing a parent style signal removes CSS while an absent signal preserves fallback", async () => {
  const el = styled(); el.setAttribute("data-gosx-bind-style", "--width:$layout.sidebar.width:px");
  el.properties.set("--width", "240px");
  const env = bootWorkbench({ elements: [el] }); await flushAsyncWork();
  const set = env.context.__gosx.workbench.debug.setSignal;
  assert.equal(el.properties.get("--width"), "240px");
  for (const cleared of [null, { sidebar: null }, { sidebar: { width: null } }]) {
    set("$layout", { sidebar: { width: 280 } });
    assert.equal(el.properties.get("--width"), "280px");
    set("$layout", cleared); assert.equal(el.properties.has("--width"), false);
  }
});

test("removed tabs cannot select replacement panels before or after mutation refresh", async () => {
  const old = tabs(); old.root.setAttribute("data-gosx-tabs-signal", "$retiredView");
  const env = bootWorkbench({ elements: [old.root] }); await flushAsyncWork();
  const next = tabs(); old.root.remove(); env.document.body.appendChild(next.root);
  env.context.__gosx.workbench.debug.setSignal("$retiredView", "session");
  assert.equal(next.panels[0].hidden, false);
  refreshDOM(env); await flushAsyncWork();
  env.context.__gosx.workbench.debug.setSignal("$view", "session");
  env.context.__gosx.workbench.debug.setSignal("$retiredView", "arrange");
  assert.equal(next.panels[0].hidden, true); assert.equal(next.panels[1].hidden, false);
});

test("fifty region replacements keep every workbench binding bounded and leave detached controls untouched", async () => {
  function region(index) {
    const root = new FakeElement("div", null), drag = handle({ axis: "x", min: 160, max: 640, step: 8, scale: 1 });
    const style = styled(); style.setAttribute("data-gosx-bind-style", "--width:$layout.width:px,--peak:@data-peak"); style.setAttribute("data-peak", "0.5");
    const t = tabs(); t.root.setAttribute("data-gosx-tabs-signal", "$tabs." + index);
    const details = new FakeElement("details", null); details.setAttribute("data-gosx-collapsible", "$open"); details.open = false;
    const button = new FakeElement("button", null); button.setAttribute("data-gosx-command", "layout.save");
    for (const el of [drag, style, t.root, details, button]) root.appendChild(el);
    return { root, drag, style, t, details, button, elements: [drag, style, t.root, t.nav, ...t.links, ...t.panels, details, button] };
  }
  let live = region(0), detachedWrites = 0;
  const env = bootHandles([live.root], { manifest: { commands: [{ id: "layout.save", keys: ["Mod+S"], action: { signal: "$saved", value: true } }] } });
  await flushAsyncWork();
  const set = env.context.__gosx.workbench.debug.setSignal;
  function watch(r) {
    for (const el of r.elements) {
      Object.defineProperty(el, "isConnected", { get: () => env.document.body.contains(el) });
      const touch = () => { if (!el.isConnected) detachedWrites++; };
      for (const name of ["setAttribute", "removeAttribute"]) {
        const original = el[name].bind(el); el[name] = (...args) => { touch(); return original(...args); };
      }
      if (el === r.style) for (const name of ["setProperty", "removeProperty"]) {
        const original = el.style[name]; el.style[name] = (...args) => { touch(); return original(...args); };
      }
      if (el === r.details) {
        let open = el.open;
        Object.defineProperty(el, "open", { get: () => open, set: value => { touch(); open = value; } });
      }
    }
  }
  const subscriptions = () => [...env.context.__gosx.sharedSignals.subscribers.values()].reduce((n, callbacks) => n + callbacks.size, 0);
  const observers = () => env.mutationObservers.filter(o => o.targets.size).length;
  const listeners = () => [env.document.eventListeners, env.windowListeners].reduce((n, map) => n + [...map.values()].reduce((m, entries) => m + entries.length, 0), 0);
  const baseline = [subscriptions(), observers(), listeners()]; watch(live);
  for (let i = 1; i <= 50; i++) {
    const retired = live; live = region(i);
    // Exercise cancellation of an active gesture as well as its subscription.
    pointer(env, "pointerdown", retired.drag); await flushAsyncWork();
    pointer(env, "pointermove", retired.drag, { clientX: 28 });
    retired.root.remove(); env.document.body.appendChild(live.root); watch(live);
    // A signal may arrive before the MutationObserver delivers its records.
    set("$tabs." + (i - 1), "session"); set("$layout", { width: 280 }); set("$mix.vol", 280); set("$open", true);
    refreshDOM(env); await flushAsyncWork();
    assert.equal(retired.drag.capture, null);
    assert.equal([...retired.drag.listeners.values()].reduce((n, entries) => n + entries.length, 0), 0);
    assert.equal(env.mutationObservers.some(o => retired.elements.some(el => o.targets.has(el))), false);
    set("$tabs." + (i - 1), "arrange"); set("$tabs." + i, "session");
    set("$layout", { width: 288 }); set("$mix.vol", 288); set("$open", false);
    assert.equal(live.drag.getAttribute("aria-valuenow"), "288");
    assert.equal(live.style.properties.get("--width"), "288px"); assert.equal(live.details.open, false);
    assert.equal(live.t.panels[0].hidden, true); assert.equal(live.t.panels[1].hidden, false);
    assert.deepEqual([subscriptions(), observers(), listeners()], baseline, "replacement " + i);
    assert.equal(detachedWrites, 0, "callbacks never write to a removed element");
    env.document.dispatchEvent({ type: "click", target: live.button, preventDefault() {} });
    assert.equal(env.context.__gosx.sharedSignals.values.get("$saved"), true);
  }
  await env.context.__gosx_dispose_page();
  assert.equal(subscriptions(), 0);
  // The core's head observer has a document lifetime; workbench observers stop.
  assert.equal(env.mutationObservers.filter(o => o.targets.has(env.document.body) || live.elements.some(el => o.targets.has(el))).length, 0);
  assert.equal(env.fetchCalls.filter(c => String(c.url) === INPUT_URL).length, 1);
});

test("style bindings follow signal paths, attributes and region replacements", async () => {
  const el = styled(); el.setAttribute("data-gosx-bind-style", "--gsx-split-a:$layout.sidebar:px,--peak:@data-peak"); el.setAttribute("data-peak", "0.2");
  const env = bootWorkbench({ elements: [el] }); await flushAsyncWork();
  env.context.__gosx.workbench.debug.setSignal("$layout", { sidebar: 280 });
  assert.equal(el.properties.get("--gsx-split-a"), "280px"); assert.equal(el.properties.get("--peak"), "0.2");
  el.setAttribute("data-peak", "0.9");
  const observer = env.mutationObservers.find(o => o.targets.has(el));
  assert.ok(observer); observer.trigger([{ type: "attributes", target: el, attributeName: "data-peak" }]);
  assert.equal(el.properties.get("--peak"), "0.9");
  // The fake observer delivers records only through trigger(), not setAttribute().
  const replacement = styled(); replacement.setAttribute("data-gosx-bind-style", "--gsx-split-a:$layout.sidebar:px");
  el.remove(); env.document.body.appendChild(replacement); refreshDOM(env); await flushAsyncWork();
  assert.equal(replacement.properties.get("--gsx-split-a"), "280px");
  env.context.__gosx.workbench.debug.setSignal("$layout.sidebar", 328);
  assert.equal(replacement.properties.get("--gsx-split-a"), "328px");
  env.context.__gosx.workbench.debug.setSignal("$layout.sidebar", null); assert.equal(replacement.properties.has("--gsx-split-a"), false);
  await env.context.__gosx_dispose_page();
  env.context.__gosx.workbench.debug.setSignal("$layout", { sidebar: 400 }); assert.equal(replacement.properties.has("--gsx-split-a"), false);
});

export function tabs() {
  const root = new FakeElement("div", null); root.setAttribute("data-gosx-tabs", ""); root.setAttribute("data-gosx-tabs-signal", "$view");
  const nav = new FakeElement("nav", null); nav.setAttribute("class", "gsx-tabs__list"); nav.setAttribute("aria-label", "Views"); root.appendChild(nav);
  const links = [], panels = [];
  for (const name of ["arrange", "session"]) {
    const link = new FakeElement("a", null); link.setAttribute("href", "?tab=" + name); link.setAttribute("data-gosx-tab", name);
    link.setAttribute("data-gosx-tab-panel", "panel-" + name); if (name === "arrange") link.setAttribute("aria-current", "page"); nav.appendChild(link); links.push(link);
    const panel = new FakeElement("section", null); panel.id = "panel-" + name; panel.hidden = name !== "arrange";
    if (panel.hidden) panel.setAttribute("hidden", ""); root.appendChild(panel); panels.push(panel);
  }
  return { root, nav, links, panels };
}

test("tabs without workbench retain server links and selected panel", async () => {
  const t = tabs(), env = bootWorkbench({ elements: [t.root], manifest: { features: [] } }); await flushAsyncWork();
  assert.equal(t.nav.getAttribute("role"), null); assert.equal(t.links[0].getAttribute("role"), null);
  assert.equal(t.links[0].getAttribute("href"), "?tab=arrange"); assert.equal(t.links[0].getAttribute("aria-current"), "page"); assert.equal(t.panels[1].hidden, true);
});

test("upgraded tabs select, focus, persist and support orientation and region swaps", async () => {
  const t = tabs(), env = bootWorkbench({ elements: [t.root] }); await flushAsyncWork(); const values = subscribe(env, "$view");
  assert.equal(t.nav.getAttribute("role"), "tablist"); assert.equal(t.links[0].getAttribute("role"), "tab");
  assert.equal(t.links[0].getAttribute("aria-controls"), "panel-arrange"); assert.equal(t.links[0].getAttribute("aria-selected"), "true");
  assert.equal(t.links[0].getAttribute("tabindex"), "0"); assert.equal(t.links[1].getAttribute("tabindex"), "-1"); assert.equal(t.panels[0].getAttribute("role"), "tabpanel");
  assert.equal(keydown(env, { key: "ArrowRight", target: t.links[0] }).prevented, true);
  assert.equal(values.at(-1), "session"); assert.equal(env.document.activeElement, t.links[1]); assert.equal(t.panels[0].hidden, true);
  const click = { type: "click", target: t.links[0], preventDefault() { this.prevented = true; } }; env.document.dispatchEvent(click);
  assert.equal(click.prevented, true); assert.equal(values.at(-1), "arrange");
  keydown(env, { key: "End", target: t.links[0] }); assert.equal(values.at(-1), "session");
  keydown(env, { key: "Home", target: t.links[1] }); assert.equal(values.at(-1), "arrange");
  t.nav.setAttribute("aria-orientation", "vertical"); assert.equal(keydown(env, { key: "ArrowRight", target: t.links[0] }).prevented, false);
  keydown(env, { key: "ArrowUp", target: t.links[0] }); assert.equal(values.at(-1), "session");
  env.context.__gosx.workbench.debug.setSignal("$view", "arrange"); assert.equal(t.panels[0].hidden, false);
  const next = tabs(); t.root.remove(); env.document.body.appendChild(next.root); refreshDOM(env); await flushAsyncWork(); assert.equal(next.nav.getAttribute("role"), "tablist");
});

test("collapsible mirrors native toggle and signal updates after swaps", async () => {
  const details = new FakeElement("details", null); details.setAttribute("data-gosx-collapsible", "$layout.browserOpen"); details.open = true; details.setAttribute("open", "");
  const env = bootWorkbench({ elements: [details] }); await flushAsyncWork(); const values = subscribe(env, "$layout.browserOpen");
  assert.equal(details.open, true); details.open = false; env.document.dispatchEvent({ type: "toggle", target: details }); assert.deepEqual(values, [false]);
  env.context.__gosx.workbench.debug.setSignal("$layout.browserOpen", true); assert.equal(details.open, true); assert.equal(details.hasAttribute("open"), true);
});

test("handle keyboard update reaches controller storage with one input fetch", async () => {
  const el = handle({ axis: "x", min: 160, max: 640, step: 8 }); el.setAttribute("data-gosx-drag", "$layout.sidebar");
  const app = styled(); app.id = "app"; app.appendChild(el); app.setAttribute("data-gosx-bind-style", "--gsx-split-a:$layout.sidebar:px");
  const CONTROLLERS_URL = "/hashed/bootstrap-feature-controllers.js";
  const env = bootHandles([app], { manifest: { controllers: [{ id: "layout-0", config: { root: "#app", storage: { area: "local", namespace: "studio:layout", load: [{ key: "sidebar", signal: "$layout.sidebar" }], save: [{ key: "sidebar", signal: "$layout.sidebar" }] } } }] },
    contract: { bootstrapFeatureControllersPath: CONTROLLERS_URL }, fetchRoutes: { [CONTROLLERS_URL]: { text: fs.readFileSync(new URL("./bootstrap-feature-controllers.js", import.meta.url), "utf8") } } });
  const storage = new Map([["studio:layout:sidebar", "320"]]), writes = [];
  env.context.localStorage = { getItem: key => storage.get(key) ?? null, setItem: (key, value) => { storage.set(key, value); writes.push([key, value]); } };
  await flushAsyncWork(); assert.equal(el.getAttribute("aria-valuenow"), "320");
  keydown(env, { key: "ArrowRight", target: el }); assert.equal(storage.get("studio:layout:sidebar"), "328");
  assert.ok(writes.some(([key, value]) => key === "studio:layout:sidebar" && value === "328")); assert.equal(app.properties.get("--gsx-split-a"), "328px");
  assert.equal(env.fetchCalls.filter(c => String(c.url) === INPUT_URL).length, 1);
});
