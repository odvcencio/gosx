import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const budgets = [
  { file: "bootstrap.js", raw: 750_000, gzip: 205_000, brotli: 170_000 },
  { file: "bootstrap-runtime.js", raw: 120_000, gzip: 33_000, brotli: 30_000 },
  { file: "bootstrap-lite.js", raw: 100_000, gzip: 27_000, brotli: 24_000 },
  { file: "bootstrap-feature-scene3d.js", raw: 455_000, gzip: 125_000, brotli: 105_000 },
  { file: "bootstrap-feature-scene3d-webgpu.js", raw: 115_000, gzip: 32_000, brotli: 28_000 },
  { file: "bootstrap-feature-scene3d-gltf.js", raw: 22_000, gzip: 8_000, brotli: 7_000 },
  { file: "bootstrap-feature-scene3d-animation.js", raw: 8_000, gzip: 4_000, brotli: 4_000 },
  { file: "bootstrap-feature-engines.js", raw: 38_000, gzip: 13_000, brotli: 12_000 },
  { file: "bootstrap-feature-hubs.js", raw: 40_000, gzip: 14_000, brotli: 13_000 },
  { file: "bootstrap-feature-islands.js", raw: 10_000, gzip: 4_000, brotli: 4_000 },
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
