import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { bootWorkbench, FakeElement, flushAsyncWork, keydown, subscribe } from "./50-workbench-commands.test.mjs";

export const INPUT_URL = "/hashed/bootstrap-controller-input.ef01.js";
export const inputSource = fs.readFileSync(new URL("./bootstrap-controller-input.js", import.meta.url), "utf8");
export function handle(attrs = {}) {
  const el = new FakeElement("div", null);
  el.setAttribute("data-gosx-drag", "$mix.vol");
  el.setAttribute("role", "slider"); el.setAttribute("tabindex", "0");
  for (const [key, value] of Object.entries(attrs)) el.setAttribute("data-gosx-drag-" + key, value);
  el.clientWidth = 40; el.clientHeight = 200;
  el.setPointerCapture = id => { el.capture = id; };
  el.releasePointerCapture = () => { el.capture = null; };
  return el;
}
export function bootHandles(elements, extra = {}) {
  return bootWorkbench({ ...extra, elements, contract: { bootstrapControllerInputPath: INPUT_URL, ...extra.contract },
    fetchRoutes: { [INPUT_URL]: { text: inputSource }, ...extra.fetchRoutes } });
}
export function pointer(env, type, target, fields = {}) {
  const event = { type, target, pointerId: 1, clientX: 20, clientY: 100,
    preventDefault() { this.prevented = true; }, ...fields };
  env.document.dispatchEvent(event); return event;
}
function ends(env, el) {
  const seen = [];
  el.addEventListener("gosx:drag:end", e => seen.push(JSON.parse(JSON.stringify(e.detail))));
  return seen;
}

test("recipe pixel handles move eight units across their eight pixel thickness", async () => {
  const recipe = fs.readFileSync(new URL("../../internal/uirecipe/recipes/v1/splitpane/splitpane.gsx", import.meta.url), "utf8");
  for (const axis of ["x", "y"]) {
    const component = recipe.split(`component SplitHandle${axis.toUpperCase()}`)[1].split("component ")[0];
    const scale = component.match(/data-gosx-drag-scale="([^"]+)"/)?.[1];
    const el = handle({ axis, min: 160, max: 640, step: 8, ...(scale == null ? {} : { scale }) });
    el.clientWidth = el.clientHeight = 8;
    const env = bootHandles([el]); await flushAsyncWork();
    env.context.__gosx.workbench.debug.setSignal("$mix.vol", 280);
    pointer(env, "pointerdown", el); await flushAsyncWork();
    const move = axis === "x" ? { clientX: 28 } : { clientY: 92 };
    pointer(env, "pointermove", el, move); pointer(env, "pointerup", el, move);
    assert.equal(el.getAttribute("aria-valuenow"), "288", axis);
  }
});

test("handle defaults and bounded value math", async () => {
  const env = bootWorkbench(); await flushAsyncWork();
  const { parseDragHandle, applyDelta } = env.context.__gosx.workbench.debug;
  const h = parseDragHandle(handle({ min: 0, max: 1, step: 0.01, fine: 0.1, reset: 0.8 }));
  for (const [key, value] of Object.entries({ signal: "$mix.vol", axis: "y", min: 0, max: 1, step: 0.01, fine: 0.1, reset: 0.8, scale: 1 / 200 })) assert.equal(h[key], value, key);
  const bare = parseDragHandle(handle({ axis: "x" }));
  for (const [key, value] of Object.entries({ min: -Infinity, max: Infinity, step: 1, scale: 1, fine: 0.1, reset: undefined })) assert.equal(bare[key], value, key);
  const math = { min: 0, max: 1, step: 0.05, scale: 1 / 200, fine: 0.1 };
  for (const [px, fine, expected] of [[100, false, 1], [-300, false, 0], [40, false, 0.7], [40, true, 0.5], [60, true, 0.55]]) assert.equal(applyDelta(math, 0.5, px, fine), expected);
});

test("pointer moves, fine adjustment, commit, Escape and reset use the hashed input chunk", async () => {
  const el = handle({ min: 0, max: 1, step: 0.01, reset: 0.75 });
  const env = bootHandles([el]); await flushAsyncWork();
  assert.equal(env.fetchCalls.filter(c => String(c.url) === INPUT_URL).length, 1);
  env.context.__gosx.workbench.debug.setSignal("$mix.vol", 0.5);
  const values = subscribe(env, "$mix.vol"), finished = ends(env, el);
  pointer(env, "pointerdown", el, { button: 0, isPrimary: true }); await flushAsyncWork();
  assert.equal(el.capture, 1);
  pointer(env, "pointermove", el, { clientY: 60 }); pointer(env, "pointerup", el, { clientY: 60 });
  assert.deepEqual(values, [0.7]); assert.deepEqual(finished, [{ signal: "$mix.vol", value: 0.7, commit: true }]);
  assert.equal(el.getAttribute("aria-valuenow"), "0.7"); assert.equal(el.capture, null);
  pointer(env, "pointerdown", el, { pointerId: 2, clientY: 60 }); await flushAsyncWork();
  pointer(env, "pointermove", el, { pointerId: 2, clientY: 80, altKey: true });
  assert.equal(values.at(-1), 0.69); keydown(env, { key: "Escape" });
  assert.equal(values.at(-1), 0.7); assert.deepEqual(finished.at(-1), { signal: "$mix.vol", value: 0.7, commit: false });
  pointer(env, "dblclick", el); assert.equal(values.at(-1), 0.75); assert.equal(finished.at(-1).commit, true);
});

for (const reason of ["pointercancel", "lostpointercapture", "blur", "dispose"]) test(`pointer ${reason} restores the start and releases listeners`, async () => {
  const el = handle({ axis: "x", min: 0, max: 10, step: 1, scale: 1, end: "$mix.commit" });
  const env = bootHandles([el]); await flushAsyncWork();
  env.context.__gosx.workbench.debug.setSignal("$mix.vol", 3);
  const values = subscribe(env, "$mix.vol"), finished = ends(env, el), commits = subscribe(env, "$mix.commit");
  pointer(env, "pointerdown", el); await flushAsyncWork(); pointer(env, "pointermove", el, { clientX: 22 });
  if (reason === "dispose") await env.context.__gosx_dispose_page();
  else if (reason === "blur") env.context.dispatchEvent({ type: "blur" });
  else el.dispatchEvent({ type: reason, pointerId: 1 });
  assert.deepEqual(values, [5, 3]); assert.equal(el.capture, null);
  assert.equal(finished.length, 1); assert.equal(finished[0].commit, false);
  assert.equal(commits.length, 1);
  pointer(env, "pointermove", el, { clientX: 25 }); assert.deepEqual(values, [5, 3]);
});

test("keyboard commits support bounds, fine steps, reset and axis filtering", async () => {
  const el = handle({ axis: "x", min: 0, max: 1, step: 0.05, reset: 0.5 });
  el.setAttribute("role", "separator");
  const env = bootHandles([el]); await flushAsyncWork(); env.context.__gosx.workbench.debug.setSignal("$mix.vol", 0.5);
  const values = subscribe(env, "$mix.vol"), finished = ends(env, el);
  for (const [fields, value] of [[{ key: "ArrowRight" }, 0.55], [{ key: "ArrowLeft", shiftKey: true }, 0.05],
    [{ key: "ArrowRight", altKey: true }, 0.055], [{ key: "PageUp" }, 0.555], [{ key: "End" }, 1], [{ key: "Home" }, 0], [{ key: "Backspace" }, 0.5], [{ key: "Delete" }, 0.5]]) {
    assert.equal(keydown(env, { ...fields, target: el }).prevented, true); assert.equal(values.at(-1), value);
  }
  assert.equal(keydown(env, { key: "ArrowUp", target: el }).prevented, false);
  assert.ok(finished.every(e => e.commit)); assert.equal(finished.length, 8);
  assert.equal(el.getAttribute("aria-valuenow"), "0.5"); assert.equal(el.getAttribute("aria-valuetext"), "0.5");
  env.context.__gosx.workbench.debug.setSignal("$mix.vol", 0.8); assert.equal(el.getAttribute("aria-valuenow"), "0.8");
});
