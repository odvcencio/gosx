// Keep enough intervals for the reported p95 to reflect more than one draw.
export const MIN_DRAW_CADENCE_SAMPLES = 50;

export function hasRepresentativeDrawCadence(cadence) {
  return Number.isInteger(cadence?.n) &&
    cadence.n >= MIN_DRAW_CADENCE_SAMPLES &&
    Number.isFinite(cadence.p50) && cadence.p50 > 0 &&
    Number.isFinite(cadence.p95) && cadence.p95 > 0;
}
