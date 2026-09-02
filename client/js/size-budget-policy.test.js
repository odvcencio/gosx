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
  assert.throws(() => sizeBudgetAllowance(10_000, "unknown", "warning"), /unknown size-budget metric/);
  assert.throws(() => sizeBudgetAllowance(10_000, "raw", "warn"), /unknown size-budget severity/);
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

test("runtime size policy fails closed on invalid measurements and zero-byte invariants", () => {
  for (const invalid of [undefined, NaN, -1, Infinity, 1.5, Number.MAX_SAFE_INTEGER + 1, "100"]) {
    assert.throws(
      () => evaluateSizeBudget(invalid, 100, "raw"),
      /measured size must be a non-negative safe integer byte count/,
    );
    assert.throws(
      () => evaluateSizeBudget(100, invalid, "raw"),
      /size-budget target must be a non-negative safe integer byte count/,
    );
  }

  const exactZero = evaluateSizeBudget(0, 0, "raw");
  assert.equal(exactZero.warningAllowance, 0);
  assert.equal(exactZero.errorAllowance, 0);
  assert.equal(exactZero.hardLimitExceeded, false);
  assert.equal(evaluateSizeBudget(1, 0, "raw").hardLimitExceeded, true);
});
