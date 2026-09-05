"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function harness(onEmit) {
  function target() {
    const listeners = new Map();
    return { listeners, captures: [], releases: [],
      addEventListener(type, fn) { if (!listeners.has(type)) listeners.set(type, new Set()); listeners.get(type).add(fn); },
      removeEventListener(type, fn) { listeners.get(type)?.delete(fn); },
      fire(type, values = {}) { const event = { type, button: 0, pointerId: 7, clientX: 0.5, clientY: 0, preventDefault() {}, stopImmediatePropagation() { this.stopped = true; }, ...values }; for (const fn of [...(listeners.get(type) || [])]) { fn(event); if (event.stopped) break; } return event; },
      setPointerCapture(id) { this.captures.push(id); },
      releasePointerCapture(id) { this.releases.push(id); this.fire("lostpointercapture", { pointerId: id }); },
      getBoundingClientRect() { return { left: 0, top: 0, width: 100, height: 100 }; },
    };
  }
  const canvas = target(), document = target(), phases = [];
  let current = { targetID: "object", mode: "translate", anchor: { x: 0, y: 0, z: 0 } }, samples = 0;
  const context = { document, Math, sceneNumber: (v, fallback) => Number.isFinite(v) ? v : fallback,
    sceneDragViewportMetrics: () => ({ width: 100, height: 100 }),
    sceneScreenToRay(x, y) { samples++; return { origin: { x, y, z: 5 }, dir: { x: 0, y: 0, z: -1 } }; },
  };
  vm.createContext(context);
  const source = fs.readFileSync(path.join(__dirname, "bootstrap-src/17-scene-input.ts"), "utf8");
  vm.runInContext(source.slice(source.indexOf("  function sceneGizmoAxisParameter"), source.indexOf("  function setupSceneDragInteractions")), context);
  const handle = context.setupSceneGizmoDragInteractions(canvas, {}, () => ({}), () => ({ camera: {} }), () => current, p => { phases.push(JSON.parse(JSON.stringify(p))); if (onEmit) onEmit(p); });
  return { canvas, document, phases, handle, samples: () => samples, change: value => { current = value; } };
}

test("gizmo pointer cancel is not a commit and never samples cancellation coordinates", () => {
  const h = harness(); h.canvas.fire("pointerdown"); h.document.fire("pointermove", { clientX: 1 });
  const samples = h.samples(); h.document.fire("pointercancel", { clientX: 99 });
  assert.deepEqual(h.phases.map(p => p.phase), ["start", "move", "cancel"]);
  assert.equal(h.samples(), samples); assert.equal(h.phases.at(-1).position.x, 0.5);
  assert.deepEqual(h.canvas.releases, [7]); h.document.fire("pointerup"); h.handle.dispose();
  assert.equal(h.phases.length, 3); assert.deepEqual(h.canvas.releases, [7]);
});

for (const reason of ["lostpointercapture", "dispose"]) test(`gizmo ${reason} cancels exactly once`, () => {
  const h = harness(); h.canvas.fire("pointerdown");
  if (reason === "dispose") h.handle.dispose(); else h.canvas.fire(reason);
  h.document.fire("pointerup"); h.handle.dispose();
  assert.deepEqual(h.phases.map(p => p.phase), ["start", "cancel"]);
  assert.deepEqual(h.canvas.releases, [7]);
  for (const type of ["pointermove", "pointerup", "pointercancel"]) assert.equal(h.document.listeners.get(type)?.size || 0, 0);
});

test("gizmo only matching pointerup commits; captured down excludes camera", () => {
  const h = harness(); let camera = 0; h.canvas.addEventListener("pointerdown", () => camera++);
  h.canvas.fire("pointerdown"); h.document.fire("pointercancel", { pointerId: 99 }); h.document.fire("pointerup", { pointerId: 99 });
  assert.equal(h.phases.length, 1); h.document.fire("pointerup", { clientX: 1 }); h.document.fire("pointerup");
  assert.deepEqual(h.phases.map(p => p.phase), ["start", "end"]); assert.equal(camera, 0);
  h.canvas.fire("pointerdown"); h.document.fire("pointercancel");
  assert.deepEqual(h.phases.map(p => p.phase), ["start", "end", "start", "cancel"]);
  assert.deepEqual(h.canvas.captures, [7, 7]); assert.deepEqual(h.canvas.releases, [7, 7]);
});

test("gizmo invalidated selection cancels before another sample", () => {
  const h = harness(); h.canvas.fire("pointerdown"); const samples = h.samples(); h.change(null);
  h.document.fire("pointermove", { clientX: 1 });
  assert.deepEqual(h.phases.map(p => p.phase), ["start", "cancel"]); assert.equal(h.samples(), samples);
});

test("gizmo disposal rejects synchronous cancellation-listener pointer reentry", () => {
  const h = harness(p => { if (p.phase === "cancel") h.canvas.fire("pointerdown"); });
  h.canvas.fire("pointerdown"); h.handle.dispose(); h.canvas.fire("pointerdown");
  assert.deepEqual(h.phases.map(p => p.phase), ["start", "cancel"]);
  assert.deepEqual(h.canvas.captures, [7]); assert.deepEqual(h.canvas.releases, [7]);
  for (const type of ["pointermove", "pointerup", "pointercancel"]) assert.equal(h.document.listeners.get(type)?.size || 0, 0);
  assert.equal(h.canvas.listeners.get("pointerdown")?.size || 0, 0);
});

for (const terminal of ["pointerup", "pointercancel", "lostpointercapture"]) test(`gizmo ${terminal} rejects terminal callback reentry but permits subsequent gestures`, () => {
  const h = harness(p => { if (p.phase === "end" || p.phase === "cancel") h.canvas.fire("pointerdown"); });
  const release = h.canvas.releasePointerCapture;
  h.canvas.releasePointerCapture = function(id) { release.call(this, id); this.fire("pointerdown"); };
  h.canvas.fire("pointerdown");
  (terminal === "lostpointercapture" ? h.canvas : h.document).fire(terminal);
  assert.deepEqual(h.phases.map(p => p.phase), ["start", terminal === "pointerup" ? "end" : "cancel"]);
  assert.deepEqual(h.canvas.captures, [7]); assert.deepEqual(h.canvas.releases, [7]);
  for (const type of ["pointermove", "pointerup", "pointercancel"]) assert.equal(h.document.listeners.get(type)?.size || 0, 0);
  h.canvas.fire("pointerdown"); assert.equal(h.phases.at(-1).phase, "start");
  assert.deepEqual(h.canvas.captures, [7, 7]); h.handle.dispose();
});

const { FakeElement, createContext, runScript, bootstrapRuntimeSource, freshFeatureBundleSource, flushAsyncWork } = require("./runtime-test-harness.js");
for (const interruption of ["cancel", "external", "replacement", "selection", "mode", "dispose"]) test(`mounted gizmo ${interruption} preserves ownership of translation rollback`, async () => {
  const mount = new FakeElement("div", null); mount.id = "gizmo-lifecycle";
  const env = createContext({ elements: [mount], enableWebGL: true, disableCanvas2D: true,
    fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: freshFeatureBundleSource("engines") }, "/gosx/bootstrap-feature-scene3d-webgl.js": { text: freshFeatureBundleSource("scene3d-webgl") } },
    manifest: { engines: [{ id: "gizmo-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: { width: 640, height: 360, autoRotate: false, controls: "orbit", camera: { x: 0, y: 0, z: 6, fov: 72 },
        selectionInputSignal: "$selection", gizmoInputSignal: "$mode", gizmoOutputSignal: "$result",
        scene: { objects: [{ id: "cube", kind: "box", size: 1, x: 0, y: 0, z: 0 }] } } }] } });
  const phases = []; mount.addEventListener("gosx:scene3d:input", e => { if (e.detail.kind === "gizmo-commit") phases.push(e.detail.input); });
  // Observe real patch output; do not replace its implementation or selection logic.
  const original = freshFeatureBundleSource("scene3d");
  const source = original.replace("function applySceneObjectPatch(state, objectID, patch) {", "function applySceneObjectPatch(state, objectID, patch) { originalGizmoPatch(state, objectID, patch); window.__gizmoState = state; } function originalGizmoPatch(state, objectID, patch) {");
  assert.notEqual(source, original, "real source observation seam found");
  runScript(bootstrapRuntimeSource, env.context, "runtime.js"); runScript(source, env.context, "scene3d.js"); await flushAsyncWork();
  env.context.__gosx_notify_shared_signal("$selection", JSON.stringify("cube"));
  env.context.__gosx_notify_shared_signal("$mode", JSON.stringify("translate"));
  await flushAsyncWork();
  const canvas = mount.children.find(c => c.tagName === "CANVAS"); assert.ok(canvas);
  canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 640, height: 360 });
  const event = (type, x) => ({ type, button: 0, pointerId: 7, clientX: x, clientY: 180, preventDefault() { this.defaultPrevented = true; }, stopPropagation() {}, stopImmediatePropagation() {} });
  canvas.dispatchEvent(event("pointerdown", 340));
  env.context.document.dispatchEvent(event("pointermove", 360));
  assert.deepEqual(phases.map(p => p.phase), ["start", "move"]);
  const cube = () => env.context.__gizmoState.objects.get("cube");
  assert.ok(cube().x > 0.1, "real mount applied preview");
  if (interruption === "external") {
    mount.dispatchEvent(new env.context.CustomEvent("gosx:scene3d:commands", { detail: { revision: 1, commands: [{ kind: 2, objectId: "cube", data: { x: 9, y: 0, z: 0 } }] } }));
  }
  if (interruption === "replacement") mount.dispatchEvent(new env.context.CustomEvent("gosx:scene3d:commands", { detail: { revision: 1, commands: [{kind:1,objectId:"cube"},{kind:0,objectId:"cube",data:{kind:"box",props:{id:"cube",x:9,y:0,z:0,size:2}}}] } }));
  if (interruption === "selection") env.context.__gosx_notify_shared_signal("$selection", JSON.stringify(""));
  else if (interruption === "mode") env.context.__gosx_notify_shared_signal("$mode", JSON.stringify("rotate"));
  else if (interruption === "dispose") env.context.__gosx_dispose_engine("gizmo-engine");
  else env.context.document.dispatchEvent(event(interruption === "external" || interruption === "replacement" ? "pointerup" : "pointercancel", 999));
  await flushAsyncWork();
  assert.deepEqual(phases.map(p => p.phase), ["start", "move", "cancel"]);
  assert.equal(cube().x, interruption === "external" || interruption === "replacement" ? 9 : 0);
  env.context.document.dispatchEvent(event("pointerup", 360));
  assert.equal(phases.length, 3);
});
