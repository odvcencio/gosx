import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { bootWorkbench, FakeElement, flushAsyncWork, keydown, subscribe } from "./50-workbench-commands.test.mjs";
import { bootHandles, handle, INPUT_URL } from "./51-workbench-drag.test.mjs";

export function styled(tag = "div") {
  const el = new FakeElement(tag, null); el.properties = new Map();
  el.style = { setProperty: (key, value) => el.properties.set(key, value), removeProperty: key => el.properties.delete(key) };
  return el;
}
function refreshDOM(env) {
  const observer = env.mutationObservers.findLast(o => o.options.some(({ target, options }) => target === env.document.body && options.childList));
  assert.ok(observer, "page observer"); observer.trigger([{ type: "childList", target: env.document.body }]);
}

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
