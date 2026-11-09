package perf

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// DefaultBudgetTimingSamples is how many times a budget gate measures each
// route's load timings. One sample on a loaded hosted runner can read 2.5x
// the quiet value (an LCP of 2196 ms against 876 ms on the same code), so the
// gate judges the median of three.
const DefaultBudgetTimingSamples = 3

// TimingSample holds the load-dependent timing metrics of one page
// measurement. Only these metrics are replaced by their median across
// samples. Byte counts, request counts, coverage, renderer counters and
// frame statistics describe the page, not the runner's load, and always come
// from the first measurement.
type TimingSample struct {
	TTFBMs        float64 `json:"ttfbMs"`
	DCLMs         float64 `json:"dclMs"`
	FullyLoadedMs float64 `json:"fullyLoadedMs"`
	LCPMs         float64 `json:"lcpMs"`
	TBTMs         float64 `json:"tbtMs"`
	HydrationMs   float64 `json:"islandHydrationMs"`
}

// timingMetrics maps assertion metric names to the timing field they read.
// A metric absent from this table is strict: it is never resampled.
var timingMetrics = map[string]func(TimingSample) float64{
	"ttfb":            func(s TimingSample) float64 { return s.TTFBMs },
	"dcl":             func(s TimingSample) float64 { return s.DCLMs },
	"fully_loaded":    func(s TimingSample) float64 { return s.FullyLoadedMs },
	"lcp":             func(s TimingSample) float64 { return s.LCPMs },
	"lcp_ms":          func(s TimingSample) float64 { return s.LCPMs },
	"tbt":             func(s TimingSample) float64 { return s.TBTMs },
	"tbt_ms":          func(s TimingSample) float64 { return s.TBTMs },
	"hydration_total": func(s TimingSample) float64 { return s.HydrationMs },
}

// IsTimingMetric reports whether a budget metric depends on runner load and
// is therefore gated on the median of several samples.
func IsTimingMetric(name string) bool {
	_, ok := timingMetrics[name]
	return ok
}

// TimingSampleOf extracts the timing metrics of a page.
func TimingSampleOf(p PageReport) TimingSample {
	return TimingSample{
		TTFBMs:        p.TTFBMs,
		DCLMs:         p.DCLMs,
		FullyLoadedMs: p.FullyLoadedMs,
		LCPMs:         p.LargestContentfulPaintMs,
		TBTMs:         p.TotalBlockingTimeMs,
		HydrationMs:   p.IslandHydrationMs,
	}
}

// MedianTiming returns the per-metric median of samples. Each metric is
// ordered on its own, so the result may combine values from different
// samples. For an even count the median is the mean of the two middle values.
func MedianTiming(samples []TimingSample) TimingSample {
	// A zero load timing means the browser reported nothing (a hosted run
	// recorded an LCP of 0 for a page that had painted), not a fast page. Drop
	// zeros from those metrics when any sample has a real value. Blocking time
	// and hydration time can be a true zero and keep every sample.
	pick := func(get func(TimingSample) float64) float64 {
		values := make([]float64, 0, len(samples))
		for _, s := range samples {
			if v := get(s); v > 0 {
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			return 0
		}
		return median(values)
	}
	pickAll := func(get func(TimingSample) float64) float64 {
		values := make([]float64, len(samples))
		for i, s := range samples {
			values[i] = get(s)
		}
		return median(values)
	}
	return TimingSample{
		TTFBMs:        pick(func(s TimingSample) float64 { return s.TTFBMs }),
		DCLMs:         pick(func(s TimingSample) float64 { return s.DCLMs }),
		FullyLoadedMs: pick(func(s TimingSample) float64 { return s.FullyLoadedMs }),
		LCPMs:         pick(func(s TimingSample) float64 { return s.LCPMs }),
		TBTMs:         pickAll(func(s TimingSample) float64 { return s.TBTMs }),
		HydrationMs:   pickAll(func(s TimingSample) float64 { return s.HydrationMs }),
	}
}

func median(values []float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// applyMedianTiming records samples on the page and replaces only its timing
// fields with the median. Every other field keeps the first measurement.
func applyMedianTiming(page *PageReport, samples []TimingSample) {
	m := MedianTiming(samples)
	page.TimingSamples = samples
	page.TTFBMs = m.TTFBMs
	page.DCLMs = m.DCLMs
	page.FullyLoadedMs = m.FullyLoadedMs
	page.LargestContentfulPaintMs = m.LCPMs
	page.TotalBlockingTimeMs = m.TBTMs
	page.IslandHydrationMs = m.HydrationMs
}

// SampleTimings measures every page of report n-1 more times, each in a fresh
// browser with a cold cache, and replaces the page's load timings with the
// median over all n samples. A sample taken while the renderer recovered is
// dropped (it describes the recovery) and so is a failed run. A page whose
// first measurement recovered is left alone: EvaluateBudget reports it as
// inconclusive. With n <= 1 nothing runs.
func SampleTimings(s *Scenario, report *Report, n int) {
	sampleTimings(s, report, n, RunScenario)
}

func sampleTimings(s *Scenario, report *Report, n int, run func(*Scenario) (*Report, error)) {
	if s == nil || report == nil || n <= 1 {
		return
	}
	diagnostics := s.diagnostics
	if diagnostics == nil {
		diagnostics = os.Stderr
	}
	sampled := false
	for i := range report.Pages {
		page := &report.Pages[i]
		if RendererRecovery(*page) != "" {
			continue
		}
		samples := []TimingSample{TimingSampleOf(*page)}
		for k := 1; k < n; k++ {
			extra := *s
			extra.URLs = []string{page.URL}
			extra.Coverage = false
			extra.Interactions = nil
			extra.RecordPath, extra.TracePath, extra.HeapSnapshotPath = "", "", ""
			again, err := run(&extra)
			if err != nil || again == nil || len(again.Pages) == 0 {
				fmt.Fprintf(diagnostics, "gosx perf: route %s timing sample %d/%d failed (%v); dropped\n", page.URL, k+1, n, err)
				continue
			}
			if reason := RendererRecovery(again.Pages[0]); reason != "" {
				fmt.Fprintf(diagnostics, "gosx perf: route %s timing sample %d/%d recovered the renderer (%s); dropped\n", page.URL, k+1, n, reason)
				continue
			}
			samples = append(samples, TimingSampleOf(again.Pages[0]))
		}
		applyMedianTiming(page, samples)
		sampled = true
		fmt.Fprintf(diagnostics, "gosx perf: route %s %d timing sample(s): lcp %s ms, median %.0f ms\n",
			page.URL, len(samples), joinSamples(samples, func(s TimingSample) float64 { return s.LCPMs }), page.LargestContentfulPaintMs)
	}
	if sampled {
		finalizeScenarioReport(report)
	}
}

func joinSamples(samples []TimingSample, get func(TimingSample) float64) string {
	parts := make([]string, len(samples))
	for i, s := range samples {
		parts[i] = fmt.Sprintf("%.0f", get(s))
	}
	return strings.Join(parts, " / ")
}

// writeTimingSampleLines prints, for each timing metric the page's assertions
// gate, every sample and the median.
func writeTimingSampleLines(w io.Writer, page BudgetPageResult) {
	if len(page.Samples) < 2 {
		return
	}
	seen := map[string]bool{}
	for _, a := range page.Assertions {
		get, ok := timingMetrics[a.Metric]
		if !ok || seen[a.Metric] {
			continue
		}
		seen[a.Metric] = true
		fmt.Fprintf(w, "    samples %-14s %s  median %.2f (gated on the median of %d)\n",
			a.Metric, joinSamples(page.Samples, get), get(MedianTiming(page.Samples)), len(page.Samples))
	}
}
