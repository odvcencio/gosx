import assert from "node:assert/strict";
import test from "node:test";

import {
  hasRepresentativeDrawCadence,
  MIN_DRAW_CADENCE_SAMPLES,
} from "./showcase-gpu-cadence.mjs";

test("GPU cadence needs enough draw intervals to report percentiles", () => {
  assert.equal(hasRepresentativeDrawCadence({ n: 1, p50: 16.67, p95: 16.67 }), false);
  assert.equal(hasRepresentativeDrawCadence({
    n: MIN_DRAW_CADENCE_SAMPLES - 1,
    p50: 16.67,
    p95: 20,
  }), false);
  assert.equal(hasRepresentativeDrawCadence({
    n: MIN_DRAW_CADENCE_SAMPLES,
    p50: 16.67,
    p95: 20,
  }), true);
  assert.equal(hasRepresentativeDrawCadence({
    n: MIN_DRAW_CADENCE_SAMPLES,
    p50: 0,
    p95: 20,
  }), false);
  assert.equal(hasRepresentativeDrawCadence(null), false);
});
