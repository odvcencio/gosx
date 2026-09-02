"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  SIZE_BUDGET_POLICY,
  evaluateSizeBudget,
  sizeBudgetAllowance,
} = require("./size-budget-policy.js");

test("runtime size policy gives reviewed baselines bounded warning and error bands", () => {
  assert.equal(SIZE_BUDGET_POLICY.warningFraction, 0.02);
  assert.equal(SIZE_BUDGET_POLICY.errorFraction, 0.05);
  assert.equal(sizeBudgetAllowance(1_000_000, "raw", "warning"), 20_000);
  assert.equal(sizeBudgetAllowance(1_000_000, "raw", "error"), 50_000);
  assert.equal(sizeBudgetAllowance(1_000_000, "gzip", "error"), 16_384, "compressed error allowance stays capped");
  assert.equal(sizeBudgetAllowance(10_000, "brotli", "warning"), 256, "small chunks receive a warning noise floor");
  assert.equal(sizeBudgetAllowance(10_000, "brotli", "error"), 1_024, "small chunks receive a wider error floor");
  assert.throws(() => sizeBudgetAllowance(10_000, "unknown"), /unknown size-budget metric/);
});

test("runtime size policy stays quiet, warns, then fails at distinct thresholds", () => {
  const atTarget = evaluateSizeBudget(100_000, 100_000, "raw");
  assert.equal(atTarget.targetExceeded, false);
  assert.equal(atTarget.warningExceeded, false);
  assert.equal(atTarget.hardLimitExceeded, false);

  const reviewDrift = evaluateSizeBudget(100_700, 100_000, "raw");
  assert.equal(reviewDrift.targetExceeded, true);
  assert.equal(reviewDrift.warningExceeded, false);
  assert.equal(reviewDrift.hardLimitExceeded, false);

  const warning = evaluateSizeBudget(102_001, 100_000, "raw");
  assert.equal(warning.warningLimit, 102_000);
  assert.equal(warning.warningExceeded, true);
  assert.equal(warning.hardLimitExceeded, false);

  const regression = evaluateSizeBudget(105_001, 100_000, "raw");
  assert.equal(regression.hardLimit, 105_000);
  assert.equal(regression.hardLimitExceeded, true);
});
