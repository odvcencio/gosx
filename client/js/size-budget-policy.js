"use strict";

// Runtime-size targets are reviewed baselines, not byte-perfect tripwires.
// Small compression and minifier shifts stay quiet, material growth produces a
// diagnostic, and dependency-sized regressions still fail. Floors keep tiny
// chunks from failing on metadata noise, while caps keep large bundles from
// receiving an ever-growing allowance. Semantic invariants such as a static
// route shipping exactly zero JavaScript remain separate exact assertions.
const SIZE_BUDGET_POLICY = Object.freeze({
  warningFraction: 0.02,
  errorFraction: 0.05,
  warning: Object.freeze({
    minimumBytes: Object.freeze({ raw: 512, gzip: 256, brotli: 256 }),
    maximumBytes: Object.freeze({ raw: 32_768, gzip: 8_192, brotli: 8_192 }),
  }),
  error: Object.freeze({
    minimumBytes: Object.freeze({ raw: 2_048, gzip: 1_024, brotli: 1_024 }),
    maximumBytes: Object.freeze({ raw: 65_536, gzip: 16_384, brotli: 16_384 }),
  }),
});

function requireSizeBudgetBytes(value, label) {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    throw new TypeError(`${label} must be a non-negative safe integer byte count`);
  }
  return value;
}

function sizeBudgetAllowance(target, metric, severity) {
  const baseline = requireSizeBudgetBytes(target, "size-budget target");
  if (severity !== "warning" && severity !== "error") {
    throw new TypeError(`unknown size-budget severity ${String(severity)}`);
  }
  const tierName = severity;
  const tier = SIZE_BUDGET_POLICY[tierName];
  const fraction = SIZE_BUDGET_POLICY[`${tierName}Fraction`];
  const minimum = tier.minimumBytes[metric];
  const maximum = tier.maximumBytes[metric];
  if (!Number.isFinite(minimum) || !Number.isFinite(maximum)) {
    throw new TypeError(`unknown size-budget metric ${String(metric)}`);
  }
  // A zero target is a semantic invariant (for example, a static route that
  // must ship no JavaScript), not a small bundle entitled to a noise floor.
  if (baseline === 0) return 0;
  return Math.max(minimum, Math.min(maximum, Math.ceil(baseline * fraction)));
}

function evaluateSizeBudget(actual, target, metric) {
  const measured = requireSizeBudgetBytes(actual, "measured size");
  const baseline = requireSizeBudgetBytes(target, "size-budget target");
  const warningAllowance = sizeBudgetAllowance(baseline, metric, "warning");
  const errorAllowance = sizeBudgetAllowance(baseline, metric, "error");
  const warningLimit = baseline + warningAllowance;
  const hardLimit = baseline + errorAllowance;
  return {
    actual: measured,
    target: baseline,
    warningAllowance,
    errorAllowance,
    warningLimit,
    hardLimit,
    targetExceeded: measured > baseline,
    warningExceeded: measured > warningLimit,
    hardLimitExceeded: measured > hardLimit,
  };
}

module.exports = {
  SIZE_BUDGET_POLICY,
  evaluateSizeBudget,
  sizeBudgetAllowance,
};
