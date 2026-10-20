"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  bootstrapRuntimeSource, createContext, freshFeatureBundleSource, runScript, FakeElement,
} = require("./runtime-test-harness.js");

test("moving labels retain text presentation while appearance and layout changes stay live", () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  const runtime = env.context.__gosx_runtime_api;
  const original = runtime.applyTextLayoutPresentation;
  let presentations = 0;
  runtime.applyTextLayoutPresentation = (...args) => {
    presentations += 1;
    return original(...args);
  };
  const source = freshFeatureBundleSource("scene3d").replace(
    "window.__gosx_scene3d_available = true;",
    "window.__labelPresentationTest = { render: renderSceneLabelElement, key: sceneLabelLayoutKey }; window.__gosx_scene3d_available = true;",
  );
  runScript(source, env.context, "bootstrap-feature-scene3d.js");
  const api = env.context.__labelPresentationTest;
  const element = new FakeElement("div", null);
  const label = { id: "turn", text: "Your turn", font: "600 13px system-ui", maxWidth: 180, color: "#ffffff" };
  const layout = { lines: [{ text: label.text }], maxLineWidth: 64, height: 18, truncated: false };
  const metrics = { totalWidth: 84, totalHeight: 34, maxTotalWidth: 200 };
  const render = (x, hidden = false) => api.render(element, label, api.key(label), layout, metrics,
    { anchor: { x, y: 40 } }, hidden, false);
  render(20);
  assert.equal(presentations, 1);
  const line = element.children[0];
  label.color = "#ffd080";
  label.className = "active-turn";
  label.priority = 4;
  render(30, true);
  render(40);
  assert.equal(presentations, 1, "position and appearance changes do not reapply text layout");
  assert.equal(element.children[0], line, "moving text retains its DOM line");
  assert.equal(element.style["--gosx-scene-label-left"], "40px");
  assert.equal(element.style["--gosx-scene-label-color"], "#ffd080");
  assert.equal(element.getAttribute("class"), "gosx-scene-label active-turn");
  assert.equal(element.getAttribute("data-gosx-scene-label-visibility"), "visible");
  label.text = "Partner's turn";
  label.font = "700 16px system-ui";
  label.textAlign = "right";
  layout.lines = [{ text: label.text }];
  render(40);
  assert.equal(presentations, 2, "a new text/font/alignment layout reapplies presentation");
  assert.equal(element.children[0].textContent, label.text);
  assert.equal(element.style["--gosx-scene-label-font"], label.font);
});
