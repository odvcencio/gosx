"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { measureAll, uploadBytes } = require("./scene3d-work-measure.js");
const budgetPath = path.join(__dirname, "../../perf/budgets/scene3d-work.json");

function check(actual, limits, name = "cases") {
  assert.deepEqual(Object.keys(actual).sort(), Object.keys(limits).sort(), `${name}: measurement coverage changed`);
  for (const key of Object.keys(actual)) {
    const metric = `${name}.${key}`, value = actual[key], limit = limits[key];
    if (typeof value === "number") {
      assert.ok(Number.isSafeInteger(limit) && limit >= 0, `${metric}: invalid limit`);
      assert.equal(value, limit, value > limit ? `${metric}: work rose from ${limit} to ${value}` : `${metric}: work fell from ${limit} to ${value}; lower the budget`);
    } else if (value && typeof value === "object") check(value, limit, metric);
    else assert.equal(value, limit, metric);
  }
}

test("Scene3D default frame work stays within its down-only budget on both backends", async () => {
  const first = await measureAll();
  if (process.env.UPDATE_SCENE3D_WORK_BUDGET === "1") {
    fs.writeFileSync(budgetPath, JSON.stringify({ schema: "gosx.scene3d-work/v1", note: "Exact maxima and totals over 120 rendered frames after eight warmup frames. Targets count live attachment pixels times samples; fake GL default MSAA is four samples. Pacing uses 120 RAF pulses at 60 Hz after settling. Lower every improved count; raises are forbidden by the base-branch ratchet.", cases: first }, null, 2) + "\n");
  }
  const budget = JSON.parse(fs.readFileSync(budgetPath));
  assert.equal(budget.schema, "gosx.scene3d-work/v1");
  check(first, budget.cases);
});

test("work gate rejects regression, stale limits, and missing coverage", () => {
  for (const [actual, limits] of [[{draws: 2}, {draws: 1}], [{draws: 1}, {draws: 2}], [{draws: 1}, {}]]) assert.throws(() => check(actual, limits));
  check({draws: 1}, {draws: 1});
  assert.equal(uploadBytes(new Float32Array(10), 2, 3), 12);
  assert.equal(uploadBytes(new Uint16Array(10), 2), 16);
  assert.equal(uploadBytes(new ArrayBuffer(12), 4, 5), 5);
});
