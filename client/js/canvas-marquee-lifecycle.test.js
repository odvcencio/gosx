"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const { FakeElement } = require("./runtime-test-harness.js");

function harness() {
  const source = fs.readFileSync(require.resolve("./bootstrap-src/26b-feature-engines-prefix.ts"), "utf8");
  const start = source.indexOf("const CANVAS_EVENT_PAN =");
  const end = source.indexOf("// mountAllSurfaceKinds", start);
  const parent = new FakeElement("div", null);
  const canvas = new FakeElement("canvas", null);
  canvas.clientWidth = 400;
  canvas.clientHeight = 300;
  canvas.getBoundingClientRect = () => ({ left: 10, top: 20, width: 400, height: 300 });
  parent.appendChild(canvas);
  const packets = [];
  const instance = { disposed: false, listeners: [], extraCleanup: [] };
  const context = {
    window: { devicePixelRatio: 1, __gosx_canvas_event(id, kind, values) { packets.push({ id, kind, values: Array.from(values) }); } },
    document: { createElement(tag) { return new FakeElement(tag, null); } },
    console,
  };
  vm.createContext(context);
  vm.runInContext(source.slice(start, end), context);
  context._bridgeCanvasBoardEvents("board", canvas, instance);
  return { parent, canvas, instance, packets };
}

test("marquee reuses its mounted overlay without structural changes during drag", () => {
  const h = harness();
  const overlay = h.parent.children.find((node) => node.hasAttribute("data-gosx-canvas-marquee"));
  assert.ok(overlay, "the hidden overlay mounts before pointer interaction");
  assert.match(overlay.style.cssText, /display:none/);
  let mutations = 0;
  const append = h.parent.appendChild.bind(h.parent);
  const remove = h.parent.removeChild.bind(h.parent);
  h.parent.appendChild = (node) => { mutations++; return append(node); };
  h.parent.removeChild = (node) => { mutations++; return remove(node); };
  for (let pointerId = 1; pointerId <= 2; pointerId++) {
    const event = { pointerId, button: 0, shiftKey: true, clientX: 40, clientY: 60, preventDefault() {} };
    h.canvas.dispatchEvent({ ...event, type: "pointerdown" });
    h.canvas.dispatchEvent({ ...event, type: "pointermove", clientX: 140, clientY: 160 });
    assert.equal(overlay.style.display, "block");
    h.canvas.dispatchEvent({ ...event, type: "pointerup", clientX: 140, clientY: 160 });
    assert.equal(overlay.style.display, "none");
  }
  assert.equal(mutations, 0, "overlay changes cannot shift the viewport between press and release");
  assert.deepEqual(h.packets, [1, 2].map(() => ({ id: "board", kind: 4, values: [30, 40, 130, 140, 400, 300] })));
  h.instance.extraCleanup.forEach((cleanup) => cleanup());
  assert.equal(overlay.parentNode, null, "surface disposal removes the overlay");
});

test("pointer cancellation hides the mounted marquee without selecting", () => {
  const h = harness();
  const event = { pointerId: 1, button: 0, shiftKey: true, clientX: 40, clientY: 60, preventDefault() {} };
  h.canvas.dispatchEvent({ ...event, type: "pointerdown" });
  h.canvas.dispatchEvent({ ...event, type: "pointermove", clientX: 140, clientY: 160 });
  h.canvas.dispatchEvent({ ...event, type: "pointercancel" });
  const overlay = h.parent.children.find((node) => node.hasAttribute("data-gosx-canvas-marquee"));
  assert.equal(overlay.style.display, "none");
  assert.equal(h.packets.length, 0);
});
