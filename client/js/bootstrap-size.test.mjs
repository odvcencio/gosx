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
  { file: "bootstrap.js", raw: 812_000, gzip: 222_000, brotli: 182_000 },
  { file: "bootstrap-runtime.js", raw: 120_000, gzip: 33_000, brotli: 30_000 },
  { file: "bootstrap-lite.js", raw: 100_000, gzip: 27_000, brotli: 24_000 },
  { file: "bootstrap-feature-scene3d.js", raw: 510_000, gzip: 140_000, brotli: 116_000 },
  { file: "bootstrap-feature-scene3d-webgpu.js", raw: 130_000, gzip: 32_000, brotli: 28_000 },
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
  { file: "bootstrap-feature-engines.js", raw: 66_000, gzip: 21_000, brotli: 19_000 },
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
    raw: 171_000,
    gzip: 49_000,
    brotli: 43_000,
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
