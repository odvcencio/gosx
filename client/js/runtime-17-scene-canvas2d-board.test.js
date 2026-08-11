"use strict";
// The canvas2d surface kind: placeholder discovery, the paint loop, the 2D
// painter and the orthographic 2D camera golden vectors.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  FakeDocument,
  loadCanvasPainter,
  makeFakeContext2D,
  callsOfType,
  nodeFillRects,
  loadScene3DApiContext,
  assertMat4Approx,
} = require("./runtime-test-harness.js");

// -----------------------------------------------------------------------------
// Canvas2D surface-kind discovery + dispatch (gosx.CanvasBoard hydration)
//
// The FakeDocument harness drives island/engine hydration through the manifest
// rather than DOM querySelectorAll, so DOM-discovery code paths (both the
// bytecode mountAllEngineSurfaces and the new mountAllSurfaceKinds) are guarded
// at the source level — mirroring the existing bootstrap-src/*.js source
// assertions. These lock the wiring contract: surface-kind placeholders are
// discovered separately from the bytecode path and dispatched through the
// unified Phase 1d __gosx_hydrate with a valid-empty program.
// -----------------------------------------------------------------------------

test("bootstrap discovers canvas2d surface-kind placeholders without touching the bytecode path", () => {
  const source = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "26b-feature-engines-prefix.js"),
    "utf8",
  );

  // The surface-kind discovery query must exclude the bytecode path so the two
  // never double-mount the same element. Manifest kind filtering happens in JS
  // after this broad query, so authored kinds are never injected into selectors.
  assert.match(
    source,
    /querySelectorAll\("\[data-gosx-surface-kind\]:not\(\[data-gosx-engine-bytecode\]\)"\)/,
  );
  assert.match(source, /function manifestSurfaceKinds\(manifest\)/);
  assert.match(source, /declaredKinds\.has\(kind\)/);
  assert.match(source, /el\.__gosxSurfaceMounting/);
  assert.match(source, /data-gosx-surface-id/);

  // Dispatch goes through the unified 6-arg __gosx_hydrate(surfaceKind, id,
  // componentName, propsJSON, programData, format) with a valid-empty program.
  assert.match(source, /window\.__gosx_hydrate;/);
  assert.match(
    source,
    /hydrateFn\(surfaceKind,\s*id,\s*component,\s*propsJSON,\s*"\{\}",\s*"json"\)/,
  );

  // Raw-JSON-first, base64-fallback props decoding (gosx.CanvasBoard emits raw
  // HTML-escaped JSON; the engine/surface renderer base64-encodes).
  assert.match(source, /function decodeSurfaceProps\(/);

  // The bytecode discovery query stays byte-for-byte unchanged.
  assert.match(source, /querySelectorAll\("\[data-gosx-engine-bytecode\]"\)/);
});

test("bootstrap engines feature runs canvas2d surface-kind mount on runtime ready", () => {
  const suffix = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "26b-feature-engines-suffix.js"),
    "utf8",
  );

  // runtimeReady must fan out to the surface-kind mount alongside the existing
  // engine + engine-surface mounts. isNavigationBootstrap threads through so
  // mountAllEngines' "engine-remounted" telemetry never fires on a first load.
  assert.match(suffix, /mountAllEngines\(manifest, reuseEngineIDs, isNavigationBootstrap\)/);
  assert.match(suffix, /mountAllEngineSurfaces\(\)/);
  assert.match(suffix, /mountAllSurfaceKinds\(manifest\)/);
});

test("bootstrap starts a canvas2d paint loop (tick + render + paint) only for the canvas2d surface kind", () => {
  const source = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "26b-feature-engines-prefix.js"),
    "utf8",
  );

  // The canvas2d branch starts a dedicated RAF render loop after a successful
  // hydrate, gated on surfaceKind === "canvas2d" so the bytecode/GPU
  // engine-surface path is untouched.
  assert.match(source, /surfaceKind === "canvas2d"/);
  assert.match(source, /function _startCanvasSurfaceRAF\(/);

  // The loop drives the three canvas WASM globals (dispose is routed by kind
  // through window[disposeName], so it appears as a bare global name).
  assert.match(source, /window\.__gosx_tick_canvas/);
  assert.match(source, /window\.__gosx_render_canvas/);
  assert.match(source, /__gosx_dispose_canvas/);

  // It paints through the shared painter on the canvas's 2D context.
  assert.match(source, /window\.__gosx_paint_canvas_bundle/);
  assert.match(source, /getContext\("2d"\)/);
});

test("paintCanvasBundle clears with the bundle background then paints a rect at the OrthoCamera2D origin (zoom 1, pan 0)", () => {
  const paint = loadCanvasPainter();
  const ctx = makeFakeContext2D();

  const bundle = {
    background: "#0f1720",
    camera: { mode: "ortho2d", x: 0, y: 0, z: 1 },
    materials: [{ color: "#ff8866" }],
    objects: [
      {
        kind: "rect",
        materialIndex: 0,
        bounds: { minX: -22, maxX: 22, minY: -16, maxY: 16 },
      },
    ],
    lines: [],
    labels: [],
  };

  paint(ctx, bundle, 800, 600, 1);

  // Background clear/fill happens.
  const clears = callsOfType(ctx, "clearRect");
  const fills = nodeFillRects(ctx, 800, 600);
  assert.ok(clears.length >= 1, "expected a clearRect for the frame");
  assert.equal(fills.length, 1, "expected exactly one node fillRect for the rect node");

  // World rect (-22..22, -16..16) at zoom 1, pan 0, viewport 800x600 maps to
  // screen x in [378, 422] and (Y flipped) screen y in [284, 316].
  // top-left screen corner = (minX→378, maxY→284); width=44, height=32.
  const rect = fills[0];
  assert.equal(rect.fillStyle, "#ff8866", "rect must use its material color");
  assert.ok(Math.abs(rect.x - 378) < 1e-6, `rect x = ${rect.x}, want 378`);
  assert.ok(Math.abs(rect.y - 284) < 1e-6, `rect y = ${rect.y}, want 284`);
  assert.ok(Math.abs(rect.w - 44) < 1e-6, `rect w = ${rect.w}, want 44`);
  assert.ok(Math.abs(rect.h - 32) < 1e-6, `rect h = ${rect.h}, want 32`);
});

test("paintCanvasBundle strokes a line and fills a label at OrthoCamera2D screen coords (zoom 1, pan 0)", () => {
  const paint = loadCanvasPainter();
  const ctx = makeFakeContext2D();

  const bundle = {
    background: "#000",
    camera: { mode: "ortho2d", x: 0, y: 0, z: 1 },
    objects: [],
    lines: [
      {
        from: { x: 0, y: 0 },
        to: { x: 100, y: 0 },
        color: "#88ddff",
        lineWidth: 3,
      },
    ],
    labels: [
      {
        text: "hello",
        color: "#ffffff",
        font: "14px system-ui, sans-serif",
        position: { x: 0, y: 0 },
      },
    ],
  };

  paint(ctx, bundle, 800, 600, 1);

  // Line endpoints: (0,0) → screen (400, 300); (100,0) → screen (500, 300).
  const moves = callsOfType(ctx, "moveTo");
  const linesTo = callsOfType(ctx, "lineTo");
  const strokes = callsOfType(ctx, "stroke");
  assert.equal(moves.length, 1, "expected one moveTo");
  assert.equal(linesTo.length, 1, "expected one lineTo");
  assert.equal(strokes.length, 1, "expected one stroke");
  assert.ok(Math.abs(moves[0].x - 400) < 1e-6, `moveTo x = ${moves[0].x}, want 400`);
  assert.ok(Math.abs(moves[0].y - 300) < 1e-6, `moveTo y = ${moves[0].y}, want 300`);
  assert.ok(Math.abs(linesTo[0].x - 500) < 1e-6, `lineTo x = ${linesTo[0].x}, want 500`);
  assert.ok(Math.abs(linesTo[0].y - 300) < 1e-6, `lineTo y = ${linesTo[0].y}, want 300`);
  assert.equal(strokes[0].strokeStyle, "#88ddff", "line must use its color");
  assert.equal(strokes[0].lineWidth, 3, "line must use its lineWidth");

  // Label at world (0,0) → screen (400, 300).
  const texts = callsOfType(ctx, "fillText");
  assert.equal(texts.length, 1, "expected one fillText");
  assert.equal(texts[0].text, "hello");
  assert.equal(texts[0].fillStyle, "#ffffff", "label must use its color");
  assert.ok(Math.abs(texts[0].x - 400) < 1e-6, `label x = ${texts[0].x}, want 400`);
  assert.ok(Math.abs(texts[0].y - 300) < 1e-6, `label y = ${texts[0].y}, want 300`);
});

test("paintCanvasBundle applies zoom and pan from the OrthoCamera2D camera", () => {
  const paint = loadCanvasPainter();
  const ctx = makeFakeContext2D();

  // zoom=2, pan=(10,20). World point (10,20) must land at the viewport center.
  // World rect top-left corner (minX=10, maxY=20):
  //   screenX = (10 - 10) * 2 + 400 = 400
  //   screenY = 300 - (20 - 20) * 2 = 300
  // width = (maxX-minX)*zoom = (30-10)*2 = 40; height = (maxY-minY)*zoom = (20-0)*2 = 40
  const bundle = {
    background: "#101010",
    camera: { mode: "ortho2d", x: 10, y: 20, z: 2 },
    materials: [{ color: "#a0ff88" }],
    objects: [
      {
        kind: "rect",
        materialIndex: 0,
        bounds: { minX: 10, maxX: 30, minY: 0, maxY: 20 },
      },
    ],
    lines: [],
    labels: [],
  };

  paint(ctx, bundle, 800, 600, 1);

  const fills = nodeFillRects(ctx, 800, 600);
  assert.equal(fills.length, 1, "expected one node fillRect");
  const rect = fills[0];
  assert.ok(Math.abs(rect.x - 400) < 1e-6, `rect x = ${rect.x}, want 400`);
  assert.ok(Math.abs(rect.y - 300) < 1e-6, `rect y = ${rect.y}, want 300`);
  assert.ok(Math.abs(rect.w - 40) < 1e-6, `rect w = ${rect.w}, want 40 (20 world * 2 zoom)`);
  assert.ok(Math.abs(rect.h - 40) < 1e-6, `rect h = ${rect.h}, want 40 (20 world * 2 zoom)`);
});

test("paintCanvasBundle tolerates missing/empty arrays and absent background", () => {
  const paint = loadCanvasPainter();
  const ctx = makeFakeContext2D();

  // No objects/lines/labels keys at all, no background — must not throw and
  // must still clear the frame.
  assert.doesNotThrow(() => {
    paint(ctx, { camera: { mode: "ortho2d", x: 0, y: 0, z: 1 } }, 320, 240, 1);
  });
  assert.ok(callsOfType(ctx, "clearRect").length >= 1, "frame must still be cleared");
  assert.equal(callsOfType(ctx, "fillRect").length, 0, "no rects to fill");
  assert.equal(callsOfType(ctx, "stroke").length, 0, "no lines to stroke");
  assert.equal(callsOfType(ctx, "fillText").length, 0, "no labels to fill");
});

test("bootstrap Scene3D ortho-2D viewProj matches the native computeOrthoCamera2DMVP golden", async () => {
  const api = await loadScene3DApiContext();
  const viewProj = api.sceneMat4Ortho2DViewProj;
  assert.equal(
    typeof viewProj,
    "function",
    "sceneMat4Ortho2DViewProj must be exported on __gosx_scene3d_api",
  );

  const out = new Float32Array(16);

  // zoom=1, pan=0, 200x100 → ortho scale 2/200, 2/100; identity translation.
  viewProj({ mode: "ortho2d", x: 0, y: 0, z: 1, near: -1, far: 1 }, 200, 100, out);
  assertMat4Approx(out, [
    [0, 0.01],
    [5, 0.02],
    [10, -1],
    [15, 1],
    [12, 0],
    [13, 0],
    [14, 0],
  ], "golden case 1");

  // pan=(10,20) → MVP translation = proj-scaled (-panX, -panY).
  viewProj({ mode: "ortho2d", x: 10, y: 20, z: 1, near: -1, far: 1 }, 200, 100, out);
  assertMat4Approx(out, [
    [0, 0.01],
    [5, 0.02],
    [12, -0.1],
    [13, -0.4],
  ], "golden case 2");

  // zoom=2 → half-extents halve → ortho scale doubles.
  viewProj({ mode: "ortho2d", x: 0, y: 0, z: 2, near: -1, far: 1 }, 200, 100, out);
  assertMat4Approx(out, [
    [0, 0.02],
    [5, 0.04],
  ], "golden case 3");
});

test("bootstrap Scene3D ortho-2D helpers apply the native defaults (zoom<=0→1, near/far 0→-1/1)", async () => {
  const api = await loadScene3DApiContext();
  const viewProj = api.sceneMat4Ortho2DViewProj;
  const view = api.sceneMat4Ortho2DView;
  const proj = api.sceneMat4Ortho2DProj;
  assert.equal(typeof viewProj, "function", "sceneMat4Ortho2DViewProj must be exported");
  assert.equal(typeof view, "function", "sceneMat4Ortho2DView must be exported");
  assert.equal(typeof proj, "function", "sceneMat4Ortho2DProj must be exported");

  const out = new Float32Array(16);

  // zoom 0 (and missing near/far) → zoom 1, near -1, far 1: identical to
  // golden case 1 (mirrors the native guards in computeOrthoCamera2DMVP).
  viewProj({ mode: "ortho2d", x: 0, y: 0, z: 0, near: 0, far: 0 }, 200, 100, out);
  assertMat4Approx(out, [
    [0, 0.01],
    [5, 0.02],
    [10, -1],
    [14, 0],
    [15, 1],
  ], "defaults");

  // View is a pure translation by (-panX, -panY, 0) — zoom never leaks in.
  view({ mode: "ortho2d", x: 7, y: -3, z: 5, near: -1, far: 1 }, out);
  assertMat4Approx(out, [
    [0, 1],
    [5, 1],
    [10, 1],
    [12, -7],
    [13, 3],
    [14, 0],
    [15, 1],
  ], "view");

  // Proj emits the WebGL depth convention (NDC z in [-1,1]) to match the
  // native golden — the WebGPU [0,1] remap happens in uploadFrameUniforms.
  proj({ mode: "ortho2d", x: 10, y: 20, z: 1, near: -1, far: 1 }, 200, 100, out);
  assertMat4Approx(out, [
    [0, 0.01],
    [5, 0.02],
    [10, -1],
    [12, 0],
    [13, 0],
    [14, 0],
    [15, 1],
  ], "proj");
});

test("bootstrap 16a uploadFrameUniforms takes the ortho-2D branch before the 3D camera normalizer", () => {
  const source = fs.readFileSync(
    path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"),
    "utf8",
  );
  const start = source.indexOf("function uploadFrameUniforms(");
  const end = source.indexOf("function uploadLights(");
  assert.notEqual(start, -1, "uploadFrameUniforms must exist in 16a");
  assert.notEqual(end, -1, "uploadLights anchor must exist in 16a");
  const body = source.slice(start, end);

  const orthoGate = body.indexOf('camera.mode === "ortho2d"');
  const normalizer = body.indexOf("sceneRenderCamera(camera)");
  const depthRemap = body.indexOf("scratchProjMatrix[2]");
  assert.notEqual(orthoGate, -1, "uploadFrameUniforms must gate on camera.mode === \"ortho2d\"");
  assert.notEqual(normalizer, -1, "the 3D path must still normalize through sceneRenderCamera");
  assert.ok(
    orthoGate < normalizer,
    "the ortho-2D branch must precede sceneRenderCamera — the normalizer strips mode and applies 3D defaults (z→6, near→0.05, far→128), silently mangling 2D cameras",
  );
  assert.match(body, /sceneMat4Ortho2DView\(camera,\s*scratchViewMatrix\)/);
  assert.match(body, /sceneMat4Ortho2DProj\(camera,\s*width,\s*height,\s*scratchProjMatrix\)/);
  // The ortho proj is emitted in WebGL convention (z in [-1,1], matching the
  // native golden) and must still flow through the WebGPU depth remap.
  assert.notEqual(depthRemap, -1, "WebGL→WebGPU depth remap must remain");
  assert.ok(depthRemap > orthoGate, "depth remap must stay downstream of the ortho-2D branch");

  // The sceneApi bridge: the chunked build's 26e prefix must import the
  // helpers, and 10-runtime-scene-core.js must export them.
  const prefix = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "26e-feature-scene3d-webgpu-prefix.js"),
    "utf8",
  );
  assert.match(prefix, /var sceneMat4Ortho2DView = sceneApi\.sceneMat4Ortho2DView;/);
  assert.match(prefix, /var sceneMat4Ortho2DProj = sceneApi\.sceneMat4Ortho2DProj;/);
  assert.match(prefix, /var sceneMat4Ortho2DViewProj = sceneApi\.sceneMat4Ortho2DViewProj;/);

  const core = fs.readFileSync(
    path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.js"),
    "utf8",
  );
  assert.match(core, /sceneMat4Ortho2DView:/);
  assert.match(core, /sceneMat4Ortho2DProj:/);
  assert.match(core, /sceneMat4Ortho2DViewProj:/);
});
