import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const budgets = [
  // bootstrap.js raw bumped 806_000 -> 812_000 for 28-video-sync-fallback.js
  // (parity-locked JS drift engine on the brain-absent video path). gzip/brotli
  // headroom unchanged.
  // Bumped raw 812_000 -> 814_000 and gzip 222_000 -> 223_000 for Scene3D's
  // Selena shader descriptor transport (`shaderBackend` + `shaderLayout`).
  //
  // Bumped raw 814_000 -> 827_000, gzip 223_000 -> 226_000, brotli 182_000 ->
  // 184_000 for Scene3D's Selena shader executor: WebGL full-GLSL program
  // routing plus WebGPU descriptor-packed WGSL pipelines. This turns the prior
  // descriptor transport into executable shader backend support.
  //
  // Bumped raw 827_000 -> 835_000, gzip 226_000 -> 229_000, brotli 184_000 ->
  // 186_000 for WebGPU skeletal skinning: Elio stdlib.Skin compute kernel,
  // storage-buffer packing, per-frame bone palette upload, and computed vertex
  // buffer routing through PBR/shadow draws.
  //
  // Bumped raw 835_000 -> 837_000 for static GLB live model records and
  // transform reprojection, used by baked computed meshes without skinning.
  //
  // Bumped raw 837_000 -> 861_000, gzip 229_000 -> 235_000, brotli 186_000 ->
  // 191_000 — catch-up while re-arming this gate. `make test-js` (and so CI)
  // globbed only *.test.js and never ran this .mjs file, so breaches landed
  // silently: c75e7ee (WebGPU pipeline refresh) shipped bundles already over
  // the very budgets it bumped (raw 856_454 vs 837_000), then 6d4e3dc (Selena
  // custom materials on skinned meshes) added ~1KB more with no bump. The M1
  // ortho-2D board work adds the last ~1.5KB (ortho-2D camera branch in 16a,
  // board-bundle adapter, Selena BoardFill attach). New budgets = current
  // measured size (858_908 / 233_921 / 189_980) plus the customary sub-1%
  // rounding headroom; the Makefile glob now includes *.test.mjs.
  { file: "bootstrap.js", raw: 861_000, gzip: 235_000, brotli: 191_000 },
  { file: "bootstrap-runtime.js", raw: 120_000, gzip: 33_000, brotli: 30_000 },
  { file: "bootstrap-lite.js", raw: 100_000, gzip: 27_000, brotli: 24_000 },
  // Bumped raw 510_000 -> 512_000 for the WebGL Selena executor. Bumped gzip
  // 140_000 -> 140_500 for static GLB live model records and transform
  // reprojection used by baked computed meshes.
  //
  // Bumped raw 512_000 -> 525_000, gzip 140_500 -> 144_000, brotli 116_000 ->
  // 119_000: same gate-rot catch-up as bootstrap.js above — c75e7ee landed
  // this chunk at raw 522_576, over the 512_000 budget it set, while make
  // test-js skipped this file; the M1 ortho-2D board work adds ~0.9KB on top
  // (measured now: 523_496 / 143_391 / 118_066, plus rounding headroom).
  { file: "bootstrap-feature-scene3d.js", raw: 525_000, gzip: 144_000, brotli: 119_000 },
  // Bumped raw 130_000 -> 135_000, gzip 32_000 -> 33_500, brotli 28_000 ->
  // 29_000 for the WebGPU Selena executor. Bumped raw 135_000 -> 143_000,
  // gzip 33_500 -> 36_000, brotli 29_000 -> 31_000 for Elio compute skinning
  // and GPU-deformed PBR vertex buffers.
  //
  // Bumped raw 143_000 -> 155_000, gzip 36_000 -> 38_500, brotli 31_000 ->
  // 33_500: gate-rot catch-up — c75e7ee landed this chunk at raw 152_242,
  // over the 143_000 budget it set in the same commit; 6d4e3dc (Selena custom
  // materials on skinned meshes) added ~1KB more unbumped; and the M1 ortho-2D
  // board work (camera branch + board adapter + BoardFill attach) adds ~0.6KB
  // (measured now: 153_827 / 38_175 / 33_084, plus rounding headroom).
  { file: "bootstrap-feature-scene3d-webgpu.js", raw: 155_000, gzip: 38_500, brotli: 33_500 },
  { file: "bootstrap-feature-scene3d-gltf.js", raw: 22_000, gzip: 8_000, brotli: 7_000 },
  { file: "bootstrap-feature-scene3d-animation.js", raw: 8_000, gzip: 4_000, brotli: 4_000 },
  // bootstrap-feature-engines.js carries the video factory, so it now also
  // carries 28-video-sync-fallback.js (the JS drift engine): raw 52_000 ->
  // 58_000, gzip 16_000 -> 18_500, brotli 14_500 -> 16_500.
  //
  // Bumped again for the canvas2d paint loop (26b1-canvas2d-painter.js + the
  // _startCanvasSurfaceRAF render loop in 26b-feature-engines-prefix.js):
  // raw 58_000 -> 62_000, gzip 18_500 -> 20_000, brotli 16_500 -> 18_000.
  //
  // Bumped raw 62_000 -> 64_000 for the canvas2d INTERACTION loop
  // (_bridgeCanvasBoardEvents in 26b-feature-engines-prefix.js: drag-to-pan,
  // wheel-to-zoom, click-to-pick dispatching to __gosx_canvas_event). Compressed
  // headroom unchanged — gzip/brotli stay well under their budgets.
  //
  // Bumped raw 64_000 -> 66_000 (gzip 20_000 -> 21_000, brotli 18_000 -> 19_000)
  // for the canvas2d MARQUEE + KEYBOARD-NAV loop (shift-drag marquee overlay +
  // CANVAS_EVENT_MARQUEE, arrow-key CANVAS_EVENT_NAV, Escape-to-clear in
  // _bridgeCanvasBoardEvents) — interaction parity with the DOM site-map board.
  //
  // Bumped raw 66_000 -> 66_500 for video engine resilience: bitmap WebVTT cue
  // placement, muted autoplay fallback, and fatal HLS network/media recovery.
  // Direct compressed budgets still hold.
  //
  // Bumped raw 66_500 -> 70_000, gzip 21_000 -> 22_000, brotli 19_000 -> 19_700
  // for the DOM label overlay (26b2-canvas-board-labels.js): __gosx_canvas_board_labels_sync
  // and __gosx_canvas_board_labels_dispose. Measured: 69_141 / 21_901 / 19_540,
  // plus sub-1% rounding headroom.
  { file: "bootstrap-feature-engines.js", raw: 70_000, gzip: 22_000, brotli: 19_700 },
  { file: "bootstrap-feature-hubs.js", raw: 40_000, gzip: 14_000, brotli: 13_000 },
  { file: "bootstrap-feature-islands.js", raw: 10_000, gzip: 4_000, brotli: 4_000 },
];

const routeBudgets = [
  {
    name: "video selective runtime",
    files: ["bootstrap-runtime.js", "bootstrap-feature-engines.js"],
    // raw bumped 160_000 -> 164_000 for 28-video-sync-fallback.js folded into
    // the engines surface. Bumped again 164_000 -> 167_000 (gzip 46_000 ->
    // 48_000) for the canvas2d paint loop folded into the engines surface.
    // Bumped raw 167_000 -> 169_000 (brotli 42_000 -> 43_000) for the canvas2d
    // interaction loop (_bridgeCanvasBoardEvents); gzip headroom unchanged.
    // Bumped raw 169_000 -> 171_000 (gzip 48_000 -> 49_000) for the canvas2d
    // marquee + keyboard-nav loop (shift-drag marquee, arrow-key nav, Escape).
    // Bumped brotli 43_000 -> 43_100 for the video resilience additions above.
    // Bumped raw 171_000 -> 175_000, gzip 49_000 -> 50_000, brotli 43_100 -> 44_100
    // for the DOM label overlay (26b2-canvas-board-labels.js). Measured:
    // 173_758 / 49_610 / 43_879, plus sub-1% rounding headroom.
    raw: 175_000,
    gzip: 50_000,
    brotli: 44_100,
  },
];

function fileSize(relativePath) {
  const absolute = path.join(__dirname, relativePath);
  return fs.statSync(absolute).size;
}

test("generated bootstrap bundles stay within runtime size budgets", () => {
  for (const budget of budgets) {
    const raw = fileSize(budget.file);
    const gzip = fileSize(`${budget.file}.gz`);
    const brotli = fileSize(`${budget.file}.br`);

    assert.ok(raw <= budget.raw, `${budget.file} raw size ${raw} exceeds budget ${budget.raw}`);
    assert.ok(gzip <= budget.gzip, `${budget.file}.gz size ${gzip} exceeds budget ${budget.gzip}`);
    assert.ok(brotli <= budget.brotli, `${budget.file}.br size ${brotli} exceeds budget ${budget.brotli}`);
    assert.ok(gzip < raw, `${budget.file}.gz should be smaller than raw JS`);
    assert.ok(brotli <= gzip, `${budget.file}.br should be no larger than gzip`);
  }
});

test("selective runtime route surfaces stay within first-load budgets", () => {
  const monolithRaw = fileSize("bootstrap.js");
  for (const budget of routeBudgets) {
    const raw = budget.files.reduce((sum, file) => sum + fileSize(file), 0);
    const gzip = budget.files.reduce((sum, file) => sum + fileSize(`${file}.gz`), 0);
    const brotli = budget.files.reduce((sum, file) => sum + fileSize(`${file}.br`), 0);

    assert.ok(raw <= budget.raw, `${budget.name} raw size ${raw} exceeds budget ${budget.raw}`);
    assert.ok(gzip <= budget.gzip, `${budget.name}.gz size ${gzip} exceeds budget ${budget.gzip}`);
    assert.ok(brotli <= budget.brotli, `${budget.name}.br size ${brotli} exceeds budget ${budget.brotli}`);
    assert.ok(raw < monolithRaw * 0.25, `${budget.name} should stay below 25% of legacy monolith raw size`);
  }
});
