// Package schema defines portable telemetry snapshots and record views.
package schema

// TickHealth summarizes the lifetime of one meter, not a rolling window.
// Percentiles are 0.1 ms bin upper bounds; overflow uses the observed maximum.
// Instance percentiles cannot be averaged across meters.
type TickHealth struct {
	Available       bool    `json:"available"`
	Samples         uint64  `json:"samples"`
	P50MS           float64 `json:"p50_ms"`
	P99MS           float64 `json:"p99_ms"`
	MaxMS           float64 `json:"max_ms"`
	Overruns        uint64  `json:"overruns"`
	BudgetMS        float64 `json:"budget_ms"`
	OverflowSamples uint64  `json:"overflow_samples"`
}
