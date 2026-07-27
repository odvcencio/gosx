"use strict";
// The DOM label and HTML overlay layers above a canvas board, plus the
// canvas2d routing decision between the WebGPU path and the 2D painter.
//
// Split out of client/js/runtime.test.js. Every shared fake, sandbox builder
// and fixture factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");

const {
  flushAsyncWork,
  goBoardBundleRectsJSON,
  mainRenderPasses,
  loadBoardLabels,
  makeBoardHost,
  layer_childCount,
  createCanvasBoardRoutingHarness,
} = require("./runtime-test-harness.js");

test("boardLabels transform parity: zoom 1 pan 0 maps world origin to viewport center", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  const cssWidth = 800;
  const cssHeight = 600;
  const camera = { mode: "ortho2d", x: 0, y: 0, z: 1 };

  sync(host, [{ position: { x: 0, y: 0 }, text: "O", font: "20px monospace", color: "#fff" }],
    camera, cssWidth, cssHeight);

  const layer = host.__gosxBoardLabelLayer;
  assert.ok(layer, "layer created");
  assert.equal(layer.childNodes.length, 1, "one span");
  const span = layer.childNodes[0];
  // At world (0,0) with zoom 1, pan 0: screenX = 400, screenY = 300.
  // Font "20px monospace" → parseFontSizePx = 20, ascent fallback = 0.8 * 20 = 16.
  // Expected transform: translate(400px,284px)
  assert.match(span.style.transform, /translate\(400px,284px\)/, "origin maps to center minus ascent");
});

test("boardLabels transform parity: zoom 1 pan (10, 20) shifts labels left/down", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  const cssWidth = 800;
  const cssHeight = 600;
  const camera = { mode: "ortho2d", x: 10, y: 20, z: 1 };
  const font = "10px sans-serif";
  // ascent fallback = 0.8 * 10 = 8

  sync(host, [{ position: { x: 10, y: 20 }, text: "P", font, color: "#fff" }],
    camera, cssWidth, cssHeight);

  const span = host.__gosxBoardLabelLayer.childNodes[0];
  // world (10, 20) with pan (10, 20) zoom 1:
  //   screenX = (10 - 10) * 1 + 400 = 400
  //   screenY = 300 - (20 - 20) * 1 = 300
  // translate(400px, 292px) after ascent subtraction (300 - 8 = 292)
  assert.match(span.style.transform, /translate\(400px,292px\)/, "pan-aligned world point maps to center");
});

test("boardLabels transform parity: zoom 2 scales screen coordinates", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  const cssWidth = 800;
  const cssHeight = 600;
  const camera = { mode: "ortho2d", x: 0, y: 0, z: 2 };
  const font = "10px sans-serif";
  // ascent fallback = 0.8 * 10 = 8

  sync(host, [{ position: { x: 50, y: 50 }, text: "Z", font, color: "#fff" }],
    camera, cssWidth, cssHeight);

  const span = host.__gosxBoardLabelLayer.childNodes[0];
  // screenX = (50 - 0) * 2 + 400 = 500
  // screenY = 300 - (50 - 0) * 2 = 200
  // translate(500px, 192px) after ascent (200 - 8 = 192)
  assert.match(span.style.transform, /translate\(500px,192px\)/, "zoom 2 scales position correctly");
});

test("boardLabels ascent fallback: 0.8 * parsedFontSizePx for a known font size", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  // Use a large distinctive font size so the ascent is unambiguous.
  const font = "40px serif";
  // ascent fallback = 0.8 * 40 = 32
  sync(host, [{ position: { x: 0, y: 0 }, text: "A", font, color: "#fff" }],
    { mode: "ortho2d", x: 0, y: 0, z: 1 }, 800, 600);

  const span = host.__gosxBoardLabelLayer.childNodes[0];
  // screenY = 300 - 0 = 300; ascent = 32; translateY = 300 - 32 = 268
  assert.match(span.style.transform, /translate\(400px,268px\)/, "40px ascent fallback = 32");
});

test("boardLabels reconciliation: 3 labels produce 3 spans; shrink to 2 removes 1", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  const cam = { mode: "ortho2d", x: 0, y: 0, z: 1 };
  const mk = (t) => ({ position: { x: 0, y: 0 }, text: t, font: "12px sans-serif", color: "#fff" });

  sync(host, [mk("A"), mk("B"), mk("C")], cam, 400, 300);
  const layer = host.__gosxBoardLabelLayer;
  assert.equal(layer.childNodes.length, 3, "3 labels → 3 spans");

  sync(host, [mk("A"), mk("B")], cam, 400, 300);
  assert.equal(layer.childNodes.length, 2, "shrink to 2 removes last span");
});

test("boardLabels reconciliation: text/font/color changes propagate on re-sync", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  const cam = { mode: "ortho2d", x: 0, y: 0, z: 1 };

  sync(host, [{ position: { x: 0, y: 0 }, text: "hello", font: "12px sans-serif", color: "#aaa" }],
    cam, 400, 300);

  const span = host.__gosxBoardLabelLayer.childNodes[0];
  assert.equal(span._gosxText, "hello");
  assert.equal(span._gosxColor, "#aaa");
  assert.equal(span._gosxFont, "12px sans-serif");

  sync(host, [{ position: { x: 0, y: 0 }, text: "world", font: "16px serif", color: "#bbb" }],
    cam, 400, 300);

  assert.equal(span._gosxText, "world", "text updated");
  assert.equal(span._gosxColor, "#bbb", "color updated");
  assert.equal(span._gosxFont, "16px serif", "font updated");
});

test("boardLabels reconciliation: unchanged re-sync does not mutate span properties", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  const cam = { mode: "ortho2d", x: 0, y: 0, z: 1 };
  const label = { position: { x: 0, y: 0 }, text: "stable", font: "12px sans-serif", color: "#fff" };

  sync(host, [label], cam, 400, 300);
  const span = host.__gosxBoardLabelLayer.childNodes[0];

  // Capture all tracked values after the first sync.
  const afterFirst = {
    text: span._gosxText,
    color: span._gosxColor,
    font: span._gosxFont,
    transform: span._gosxTransform,
    styleTransform: span.style.transform,
    styleColor: span.style.color,
    styleFont: span.style.font,
  };

  // Instrument textContent setter to detect spurious writes.
  let textContentWrites = 0;
  Object.defineProperty(span, "textContent", {
    get() { return span._gosxText; },
    set(v) {
      textContentWrites++;
      span._gosxText = String(v);
    },
    configurable: true,
  });

  sync(host, [label], cam, 400, 300);

  assert.equal(textContentWrites, 0, "no textContent write on unchanged label");
  assert.equal(span._gosxTransform, afterFirst.transform, "transform unchanged");
  assert.equal(span.style.transform, afterFirst.styleTransform, "style.transform unchanged");
});

test("boardLabels culling: far-off label gets display:none; back in view becomes visible", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  const cam = { mode: "ortho2d", x: 0, y: 0, z: 1 };
  const cssWidth = 800;
  const cssHeight = 600;

  // Label at world (10000, 0) with zoom 1 → screenX = 10000 + 400 = 10400, well outside.
  sync(host, [{ position: { x: 10000, y: 0 }, text: "far", font: "12px sans-serif", color: "#fff" }],
    cam, cssWidth, cssHeight);

  const span = host.__gosxBoardLabelLayer.childNodes[0];
  assert.equal(span.style.display, "none", "culled label is display:none");

  // Move the camera to bring the label into view: pan to (10000, 0).
  sync(host, [{ position: { x: 10000, y: 0 }, text: "far", font: "12px sans-serif", color: "#fff" }],
    { mode: "ortho2d", x: 10000, y: 0, z: 1 }, cssWidth, cssHeight);

  assert.notEqual(span.style.display, "none", "label visible again after camera pans to it");
  assert.equal(layer_childCount(host), 1, "still exactly 1 span (no re-creation)");
});

test("boardLabels defaults: missing font uses 14px system-ui; missing color uses #e6edf3", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();

  sync(host, [{ position: { x: 0, y: 0 }, text: "default" }],
    { mode: "ortho2d", x: 0, y: 0, z: 1 }, 400, 300);

  const span = host.__gosxBoardLabelLayer.childNodes[0];
  assert.equal(span._gosxFont, "14px system-ui, sans-serif", "default font");
  assert.equal(span._gosxColor, "#e6edf3", "default color — parity with 26b1 label fallback");
});

test("boardLabels dispose: removes the layer div and clears host cache", () => {
  const { sync, dispose } = loadBoardLabels();
  const host = makeBoardHost();

  sync(host, [{ position: { x: 0, y: 0 }, text: "x" }],
    { mode: "ortho2d", x: 0, y: 0, z: 1 }, 400, 300);

  const layer = host.__gosxBoardLabelLayer;
  assert.ok(layer, "layer exists before dispose");
  assert.equal(layer.parentNode, host, "layer is child of host");

  dispose(host);

  assert.equal(layer.parentNode, null, "layer detached after dispose");
  assert.equal(host.__gosxBoardLabelLayer, undefined, "host cache cleared");
});

test("boardLabels layer invariants: pointer-events:none, overflow:hidden, single layer reused", () => {
  const { sync } = loadBoardLabels();
  const host = makeBoardHost();
  const cam = { mode: "ortho2d", x: 0, y: 0, z: 1 };
  const label = { position: { x: 0, y: 0 }, text: "t" };

  sync(host, [label], cam, 400, 300);
  const layer = host.__gosxBoardLabelLayer;

  assert.ok(layer, "layer created on first sync");
  assert.match(String(layer.style.cssText || ""), /pointer-events:none/, "layer has pointer-events:none");
  assert.match(String(layer.style.cssText || ""), /overflow:hidden/, "layer has overflow:hidden");

  // Second sync must reuse the same layer, not append another.
  sync(host, [label], cam, 400, 300);
  assert.equal(host.__gosxBoardLabelLayer, layer, "same layer instance reused across frames");

  // Host children should contain exactly one layer div.
  const layerCount = host.childNodes.filter((c) => c === layer).length;
  assert.equal(layerCount, 1, "only one label layer appended to host");
});

test("boardHTML sync maps CanvasBoard bottom-left bounds to DOM top-left transform", () => {
  const { htmlSync, doc } = loadBoardLabels();
  const host = makeBoardHost(doc);
  const markup = '<h1 contenteditable data-studio-field="home.hero.headline">Hi</h1>';

  htmlSync(host, [{
    id: "page:home",
    markup,
    x: 30,
    y: 40,
    width: 120,
    height: 50,
    pointerEvents: "auto",
  }], { mode: "ortho2d", x: 10, y: 20, z: 2 }, 800, 600);

  const layer = host.__gosxBoardHTMLLayer;
  assert.ok(layer, "HTML layer created");
  assert.match(String(layer.style.cssText || ""), /pointer-events:none/, "layer does not block the board");
  assert.equal(layer.childNodes.length, 1, "one HTML overlay");

  const overlay = layer.childNodes[0];
  assert.equal(overlay.getAttribute("data-gosx-canvas-html"), "page:home");
  assert.equal(overlay.getAttribute("data-gosx-canvas-html-pointer-events"), "auto");
  assert.equal(overlay.innerHTML, markup);
  assert.equal(overlay.textContent, "Hi");
  assert.equal(overlay.style.pointerEvents, "auto");
  // CanvasBoard x/y is the bottom-left of bounds. DOM overlays are top-left
  // positioned, so top subtracts scaled entry height:
  // left=(30-10)*2+400=440, top=300-(40-20)*2-(50*2)=160.
  assert.equal(overlay.style.transform, "translate(440px,160px)");
  assert.equal(overlay.style.width, "240px");
  assert.equal(overlay.style.height, "100px");
});

test("boardHTML sync does not clobber a focused editable overlay", () => {
  const { htmlSync, doc } = loadBoardLabels();
  const host = makeBoardHost(doc);
  const markup = '<h1 contenteditable data-studio-field="home.hero.headline">Hi</h1>';
  const entry = {
    id: "page:home",
    markup,
    position: { x: 0, y: 0 },
    width: 100,
    height: 40,
    pointerEvents: "auto",
  };

  htmlSync(host, [entry], { mode: "ortho2d", x: 0, y: 0, z: 1 }, 400, 300);
  const overlay = host.__gosxBoardHTMLLayer.childNodes[0];
  const focusedHeading = doc.createElement("h1");
  focusedHeading.setAttribute("contenteditable", "");
  focusedHeading.textContent = "User edit";
  overlay.innerHTML = "";
  overlay.appendChild(focusedHeading);
  overlay._gosxMarkup = "__stale_cache_for_focus_guard__";
  focusedHeading.focus();

  htmlSync(host, [entry], { mode: "ortho2d", x: 10, y: 20, z: 2 }, 400, 300);

  assert.equal(doc.activeElement, focusedHeading, "editable child remains focused");
  assert.equal(overlay.textContent, "User edit", "focused user edit is preserved");
  assert.equal(overlay.style.transform, "translate(180px,110px)", "transform still updates while focused");
  assert.equal(overlay.style.width, "200px", "width still updates while focused");
  assert.equal(overlay.style.height, "80px", "height still updates while focused");
});

test("canvas2d surface with the webgpu flag mounts through 16a: RAF drives render + populates DOM overlays", async () => {
  const h = await createCanvasBoardRoutingHarness();
  await h.mount();

  // The routed setup told the WASM board to emit GPU geometry.
  assert.deepEqual(h.setBackendCalls, [{ id: h.canvas.getAttribute("data-gosx-surface-id"), backend: "webgpu" }], "must call __gosx_canvas_set_backend(id, webgpu) exactly once at setup");
  // The canvas2d path must NOT have created a 2D context (WebGPU owns the canvas).
  assert.deepEqual(h.ctx2dCalls, [], "routed canvas must never get a 2d context (would taint it against webgpu)");

  // Pump one RAF frame → tick + render + 16a draw + DOM overlays.
  h.raf.flush(16);
  await flushAsyncWork();

  assert.ok(h.renderCalls.length >= 1, "render loop must call __gosx_render_canvas");
  assert.ok(h.tickCalls.length >= 1, "render loop must tick the board");

  // 16a recorded the four board quads (rect + 2 lines + sprite from the mixed
  // fixture) as 6-vertex draws on the main pass.
  const mains = mainRenderPasses(h.fake);
  assert.ok(mains.length >= 1, "16a must run a main render pass");
  assert.deepEqual(mains[mains.length - 1].draws.map((d) => d.vertexCount), [6, 6, 6, 6], "all four board quads draw through 16a");

  // The DOM label overlay was populated with the fixture's single label ("Board").
  const layer = h.labelLayer();
  assert.ok(layer, "label overlay layer must be created on the host");
  assert.equal(layer.childNodes.length, 1, "one label span for the fixture's one label");
  assert.equal(layer.childNodes[0].textContent, "Board", "label text comes from bundle.labels");

  // The WebGPU RAF path also mounts RenderBundle.HTML entries into the DOM.
  const htmlLayer = h.htmlLayer();
  assert.ok(htmlLayer, "HTML overlay layer must be created on the host");
  assert.equal(htmlLayer.childNodes.length, 1, "one HTML overlay for bundle.html");
  const html = htmlLayer.childNodes[0];
  assert.equal(html.getAttribute("data-gosx-canvas-html"), "page:home");
  assert.equal(html.getAttribute("data-gosx-canvas-html-pointer-events"), "auto");
  assert.equal(html.style.pointerEvents, "auto");
  assert.equal(html.innerHTML, '<h1 contenteditable data-studio-field="home.hero.headline">Hi</h1>');
  assert.equal(html.textContent, "Hi");
});

test("routed canvas2d skip-frame: an identical render JSON second frame does zero render work", async () => {
  const h = await createCanvasBoardRoutingHarness();
  await h.mount();

  // Frame 1: a full render (string differs from the null previous).
  h.raf.flush(16);
  await flushAsyncWork();
  const mainsAfter1 = mainRenderPasses(h.fake).length;
  const renderCallsAfter1 = h.renderCalls.length;
  assert.ok(mainsAfter1 >= 1, "frame 1 must render");

  // Frame 2: __gosx_render_canvas returns the SAME JSON string → the loop must
  // skip parse + 16a render entirely (the idle-board contract). tick + the
  // render-string fetch still happen (cheap); the GPU submit does not.
  h.raf.flush(16);
  await flushAsyncWork();
  assert.ok(h.renderCalls.length > renderCallsAfter1, "the loop still fetches the render string each frame (to detect change)");
  assert.equal(mainRenderPasses(h.fake).length, mainsAfter1, "identical-JSON frame must add NO render pass (zero GPU work)");

  // Frame 3: change the JSON (simulate a pan) → a full render runs again.
  h.setRenderJSON(() => goBoardBundleRectsJSON);
  h.raf.flush(16);
  await flushAsyncWork();
  assert.ok(mainRenderPasses(h.fake).length > mainsAfter1, "a changed JSON frame renders again (pan/zoom → full frame)");
});

test("routed canvas2d resize-frame: same JSON but new viewport always re-renders (camera JSON carries no viewport)", async () => {
  // The OrthoCamera2D serialisation intentionally drops width/height (see
  // render/bundle/ortho_camera_2d.go:44-45: `_ = width; _ = height`).  A pure
  // container resize therefore produces IDENTICAL bundle JSON — the skip-key
  // must include the viewport (cssW, cssH, dpr) so a resize still re-renders,
  // re-runs _initEngineSurfaceCanvasSize (swapchain resync), and re-syncs labels.
  const h = await createCanvasBoardRoutingHarness();
  await h.mount();

  // Frame 1: a full render (prevJSON was null).
  h.raf.flush(16);
  await flushAsyncWork();
  const mainsAfter1 = mainRenderPasses(h.fake).length;
  assert.ok(mainsAfter1 >= 1, "frame 1 must render");

  // Resize the canvas between frames: same JSON string, new CSS dimensions.
  h.canvas.clientWidth  = 800;
  h.canvas.clientHeight = 600;

  // Frame 2: JSON is byte-identical but viewport changed → must NOT skip.
  h.raf.flush(16);
  await flushAsyncWork();
  assert.ok(
    mainRenderPasses(h.fake).length > mainsAfter1,
    "resize frame must add a render pass even when JSON is identical (viewport changed)",
  );
  // The label overlay must also have been re-synced for the new viewport.
  const layer = h.labelLayer();
  assert.ok(layer && layer.childNodes.length >= 1, "labels sync ran for the new viewport");

  // Frame 3: JSON identical AND viewport unchanged → skip (existing contract preserved).
  const mainsAfter2 = mainRenderPasses(h.fake).length;
  h.raf.flush(16);
  await flushAsyncWork();
  assert.equal(
    mainRenderPasses(h.fake).length,
    mainsAfter2,
    "identical JSON + identical viewport must still skip (no regression on the idle-board fast path)",
  );
});

test("canvas2d WITHOUT the webgpu flag stays on the 26b1 painter (regression pin for the default path)", async () => {
  const h = await createCanvasBoardRoutingHarness({ flag: false });
  await h.mount();

  h.raf.flush(16);
  await flushAsyncWork();

  // Default path: the 2D context IS created and painted; 16a never runs; the
  // WASM board is never switched to the webgpu backend.
  assert.deepEqual(h.setBackendCalls, [], "painter path must never call __gosx_canvas_set_backend");
  assert.ok(h.ctx2dCalls.length >= 1, "painter path must create a 2d context");
  assert.equal(mainRenderPasses(h.fake).length, 0, "painter path must not run a 16a render pass");
  assert.ok(h.renderCalls.length >= 1, "painter path still drives __gosx_render_canvas");

  const htmlLayer = h.htmlLayer();
  assert.ok(htmlLayer, "painter path must create the CanvasBoard HTML overlay layer");
  assert.equal(htmlLayer.childNodes.length, 1, "painter path mounts one HTML overlay for bundle.html");
  const html = htmlLayer.childNodes[0];
  assert.equal(html.getAttribute("data-gosx-canvas-html"), "page:home");
  assert.equal(html.innerHTML, '<h1 contenteditable data-studio-field="home.hero.headline">Hi</h1>');
});

test("canvas2d webgpu flag but no navigator.gpu → falls back to the painter with exactly one warn", async () => {
  const h = await createCanvasBoardRoutingHarness({ webgpuUnavailable: true });
  await h.mount();

  h.raf.flush(16);
  await flushAsyncWork();

  // Complete fallback: the painter runs (2d context created + render loop),
  // 16a never runs, the backend switch never happens.
  assert.deepEqual(h.setBackendCalls, [], "fallback must not switch the WASM backend");
  assert.ok(h.ctx2dCalls.length >= 1, "fallback must create a 2d context (painter path)");
  assert.equal(mainRenderPasses(h.fake).length, 0, "fallback must not run a 16a render pass");
  assert.ok(h.renderCalls.length >= 1, "fallback painter still drives __gosx_render_canvas");
  const htmlLayer = h.htmlLayer();
  assert.ok(htmlLayer, "WebGPU fallback painter path must create the CanvasBoard HTML overlay layer");
  assert.equal(htmlLayer.childNodes.length, 1, "fallback painter path mounts one HTML overlay for bundle.html");
  assert.equal(htmlLayer.childNodes[0].getAttribute("data-gosx-canvas-html"), "page:home");
  assert.equal(htmlLayer.childNodes[0].innerHTML, '<h1 contenteditable data-studio-field="home.hero.headline">Hi</h1>');

  // Exactly one warn about the unavailable backend (the fallback's single
  // console.warn — not a per-frame log).
  const warns = h.env.consoleLogs.warn || [];
  const backendWarns = warns.filter((w) => /WebGPU backend unavailable/.test(String(w)));
  assert.equal(backendWarns.length, 1, "exactly one console.warn on fallback, got: " + JSON.stringify(warns));
});
