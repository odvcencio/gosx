package perf

import (
	"fmt"
	"strconv"
	"strings"
)

// Assertion is a parsed perf gate expression.
type Assertion struct {
	Metric string // e.g. "p95", "ttfb", "dropped_frames"
	Op     string // "<", ">", "<=", ">=", "=="
	Value  float64
	// Optional makes an unavailable metric a skipped/pass result. It is
	// expressed by suffixing the metric with "?", e.g. gpu_total_p95?.
	Optional bool
}

// AssertionResult is the outcome of evaluating one assertion.
type AssertionResult struct {
	Assertion
	Actual float64
	Passed bool
}

// ParseAssertion parses "p95 < 12" into an Assertion.
func ParseAssertion(expr string) (Assertion, error) {
	parts := strings.Fields(expr)
	if len(parts) != 3 {
		return Assertion{}, fmt.Errorf("expected 3 parts (metric op value), got %d in %q", len(parts), expr)
	}

	metric := parts[0]
	optional := strings.HasSuffix(metric, "?")
	metric = strings.TrimSuffix(metric, "?")
	if metric == "" {
		return Assertion{}, fmt.Errorf("empty metric in %q", expr)
	}
	op := parts[1]

	switch op {
	case "<", ">", "<=", ">=", "==":
	default:
		return Assertion{}, fmt.Errorf("unsupported operator %q in %q", op, expr)
	}

	val, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return Assertion{}, fmt.Errorf("bad value %q in %q: %w", parts[2], expr, err)
	}

	return Assertion{Metric: metric, Op: op, Value: val, Optional: optional}, nil
}

// EvalAssertions evaluates all assertions against a Report.
// Returns results with Passed set for each.
func EvalAssertions(assertions []Assertion, r *Report) []AssertionResult {
	results := make([]AssertionResult, len(assertions))
	for i, a := range assertions {
		actual, ok := ResolveMetric(a.Metric, r)
		res := AssertionResult{
			Assertion: a,
			Actual:    actual,
		}
		if ok {
			res.Passed = compare(actual, a.Op, a.Value)
		} else if a.Optional {
			res.Passed = true
		}
		results[i] = res
	}
	return results
}

// ResolveMetric extracts a named metric value from a Report.
func ResolveMetric(name string, r *Report) (float64, bool) {
	p := &r.PageReport
	return ResolvePageMetric(name, p)
}

// ResolvePageMetric extracts a named metric value from one page report.
func ResolvePageMetric(name string, p *PageReport) (float64, bool) {
	available := pageMetricAvailability(p)
	switch name {
	case "ttfb":
		return p.TTFBMs, available.Navigation && (p.MetricAvailability != nil || p.TTFBMs > 0)
	case "dcl":
		return p.DCLMs, available.Navigation && (p.MetricAvailability != nil || p.DCLMs > 0)
	case "fully_loaded":
		return p.FullyLoadedMs, available.Navigation && (p.MetricAvailability != nil || p.FullyLoadedMs > 0)
	case "lcp", "lcp_ms":
		return p.LargestContentfulPaintMs, available.LCP
	case "cls":
		return p.CumulativeLayoutShift, available.CLS
	case "fid", "fid_ms":
		return p.FirstInputDelayMs, available.FID
	case "long_tasks":
		return float64(p.LongTaskCount), available.LongTasks && (p.MetricAvailability != nil || p.LongTaskCount > 0)
	case "long_task_total", "long_task_total_ms":
		return p.LongTaskTotalMs, available.LongTasks && (p.MetricAvailability != nil || p.LongTaskTotalMs > 0)
	case "tbt", "tbt_ms":
		return p.TotalBlockingTimeMs, available.LongTasks && (p.MetricAvailability != nil || p.TotalBlockingTimeMs > 0)
	case "hydration_total":
		return p.IslandHydrationMs, available.Hydration && (p.MetricAvailability != nil || p.IslandHydrationMs > 0)
	case "heap_mb":
		return p.JSHeapSizeMB, available.Heap
	case "island_count":
		return float64(len(p.Islands)), available.Hydration && (p.MetricAvailability != nil || len(p.Islands) > 0)
	case "network_kb":
		return float64(p.TotalBytesTransferred) / 1024, available.Network && (p.MetricAvailability != nil || p.TotalBytesTransferred > 0)
	case "requests":
		return float64(len(p.Resources)), available.Network && (p.MetricAvailability != nil || len(p.Resources) > 0)
	case "hub_messages":
		return float64(p.HubMessageCount), available.RuntimeCounters && (p.MetricAvailability != nil || p.HubMessageCount > 0)
	case "hub_bytes":
		return float64(p.HubMessageBytes), available.RuntimeCounters && (p.MetricAvailability != nil || p.HubMessageBytes > 0)
	case "hub_sends":
		return float64(p.HubSendCount), available.RuntimeCounters && (p.MetricAvailability != nil || p.HubSendCount > 0)
	case "js_used_kb":
		if !hasCoverageMetric(p) {
			return 0, false
		}
		return coverageUsedKB(p.Coverage), true
	case "js_total_kb":
		if !hasCoverageMetric(p) {
			return 0, false
		}
		return coverageTotalKB(p.Coverage), true
	case "js_unused_kb":
		if !hasCoverageMetric(p) {
			return 0, false
		}
		return coverageUnusedKB(p.Coverage), true
	case "js_used_ratio":
		if !hasCoverageMetric(p) {
			return 0, false
		}
		return coverageRatio(p.Coverage), true
	case "js_used_pct":
		if !hasCoverageMetric(p) {
			return 0, false
		}
		return coverageRatio(p.Coverage) * 100, true
	}

	// Scene-dependent metrics
	if p.Scene == nil {
		return 0, false
	}
	switch name {
	case "first_frame", "scene_first_frame":
		return p.Scene.FirstFrameMs, p.Scene.FrameStats.Count > 0
	case "p50", "scene_p50":
		return p.Scene.FrameStats.P50, p.Scene.FrameStats.Count > 0
	case "p95", "scene_p95":
		return p.Scene.FrameStats.P95, p.Scene.FrameStats.Count > 0
	case "p99", "scene_p99":
		return p.Scene.FrameStats.P99, p.Scene.FrameStats.Count > 0
	case "frame_max", "scene_frame_max":
		return p.Scene.FrameStats.Max, p.Scene.FrameStats.Count > 0
	case "dropped_frames", "scene_dropped_frames":
		return float64(p.Scene.DroppedFrames), p.Scene.Presentation != nil && p.Scene.Presentation.Stats.Count > 0
	case "frame_count", "scene_frame_count":
		return float64(p.Scene.FrameCount), available.SceneFrames
	case "cpu_submit_p50", "scene_cpu_submit_p50":
		return telemetryStat(p.Scene.CPURenderSubmit, "all", "p50")
	case "cpu_submit_p95", "scene_cpu_submit_p95":
		return telemetryStat(p.Scene.CPURenderSubmit, "all", "p95")
	case "cpu_submit_p99", "scene_cpu_submit_p99":
		return telemetryStat(p.Scene.CPURenderSubmit, "all", "p99")
	case "cpu_submit_warm_p95", "scene_cpu_submit_warm_p95":
		return telemetryStat(p.Scene.CPURenderSubmit, "warm", "p95")
	case "presented_p50", "presentation_p50", "scene_presented_p50":
		return presentationStat(p.Scene.Presentation, "all", "p50")
	case "presented_p95", "presentation_p95", "scene_presented_p95":
		return presentationStat(p.Scene.Presentation, "all", "p95")
	case "presented_p99", "presentation_p99", "scene_presented_p99":
		return presentationStat(p.Scene.Presentation, "all", "p99")
	case "presented_max", "presentation_max", "scene_presented_max":
		return presentationStat(p.Scene.Presentation, "all", "max")
	case "presented_cold_p95", "presentation_cold_p95":
		return presentationStat(p.Scene.Presentation, "cold", "p95")
	case "presented_warm_p95", "presentation_warm_p95":
		return presentationStat(p.Scene.Presentation, "warm", "p95")
	case "missed_vsyncs", "scene_missed_vsyncs":
		if p.Scene.Presentation == nil {
			return 0, false
		}
		return float64(p.Scene.Presentation.EstimatedMissedVsyncs), true
	case "hitch_intervals", "scene_hitch_intervals":
		if p.Scene.Presentation == nil {
			return 0, false
		}
		return float64(p.Scene.Presentation.HitchIntervals), true
	case "hitch_clusters", "scene_hitch_clusters":
		if p.Scene.Presentation == nil {
			return 0, false
		}
		return float64(len(p.Scene.Presentation.HitchClusters)), true
	case "gpu_total_p50", "scene_gpu_total_p50":
		return gpuTotalStat(p.Scene.GPU, "all", "p50")
	case "gpu_total_p95", "scene_gpu_total_p95":
		return gpuTotalStat(p.Scene.GPU, "all", "p95")
	case "gpu_total_p99", "scene_gpu_total_p99":
		return gpuTotalStat(p.Scene.GPU, "all", "p99")
	case "gpu_total_warm_p95", "scene_gpu_total_warm_p95":
		return gpuTotalStat(p.Scene.GPU, "warm", "p95")
	}

	if strings.HasPrefix(name, "gpu_pass_") {
		return resolveGPUPassMetric(strings.TrimPrefix(name, "gpu_pass_"), p.Scene.GPU)
	}
	if strings.HasPrefix(name, "scene_counter_") {
		key := strings.ReplaceAll(strings.TrimPrefix(name, "scene_counter_"), "_", "-")
		value, ok := p.Scene.Counters[key]
		return value, ok
	}
	return 0, false
}

func telemetryStat(series *TelemetrySeries, phase, stat string) (float64, bool) {
	if series == nil {
		return 0, false
	}
	selected := series.Stats
	switch phase {
	case "cold":
		selected = series.Cold
	case "warm":
		selected = series.Warm
	}
	if selected.Count == 0 {
		return 0, false
	}
	switch stat {
	case "p50":
		return selected.P50, true
	case "p95":
		return selected.P95, true
	case "p99":
		return selected.P99, true
	case "max":
		return selected.Max, true
	}
	return 0, false
}

func presentationStat(metric *PresentationMetric, phase, stat string) (float64, bool) {
	if metric == nil {
		return 0, false
	}
	return telemetryStat(&metric.TelemetrySeries, phase, stat)
}

func gpuTotalStat(gpu *SceneGPUTelemetry, phase, stat string) (float64, bool) {
	if gpu == nil {
		return 0, false
	}
	return telemetryStat(gpu.Total, phase, stat)
}

func resolveGPUPassMetric(metric string, gpu *SceneGPUTelemetry) (float64, bool) {
	if gpu == nil {
		return 0, false
	}
	for _, suffix := range []string{"_warm_p95", "_cold_p95", "_p50", "_p95", "_p99", "_max"} {
		if !strings.HasSuffix(metric, suffix) {
			continue
		}
		pass := strings.TrimSuffix(metric, suffix)
		series, ok := gpu.Passes[pass]
		if !ok {
			return 0, false
		}
		phase := "all"
		stat := strings.TrimPrefix(suffix, "_")
		if strings.HasPrefix(stat, "warm_") {
			phase, stat = "warm", strings.TrimPrefix(stat, "warm_")
		} else if strings.HasPrefix(stat, "cold_") {
			phase, stat = "cold", strings.TrimPrefix(stat, "cold_")
		}
		return telemetryStat(&series, phase, stat)
	}
	return 0, false
}

func hasCoverageMetric(p *PageReport) bool {
	return p.CoverageCaptured || len(p.Coverage) > 0
}

func pageMetricAvailability(p *PageReport) MetricAvailability {
	if p.MetricAvailability != nil {
		return *p.MetricAvailability
	}
	// Legacy reports have no capture metadata. A nonzero measurement or a
	// recorded sample is evidence; a default zero alone is not.
	return MetricAvailability{
		Navigation:      p.TTFBMs > 0 || p.DCLMs > 0 || p.FullyLoadedMs > 0,
		Heap:            p.JSHeapSizeMB > 0,
		Hydration:       p.IslandHydrationMs > 0 || len(p.Islands) > 0,
		LCP:             p.LargestContentfulPaintMs > 0,
		CLS:             p.CumulativeLayoutShift > 0,
		FID:             p.FirstInputDelayMs > 0,
		LongTasks:       len(p.LongTasks) > 0 || p.LongTaskCount > 0 || p.LongTaskTotalMs > 0 || p.TotalBlockingTimeMs > 0,
		Network:         len(p.Resources) > 0 || p.TotalBytesTransferred > 0,
		RuntimeCounters: p.SignalWrites > 0 || p.SignalReads > 0 || p.HubMessageCount > 0 || p.HubMessageBytes > 0 || p.HubSendCount > 0,
		SceneFrames:     p.Scene != nil && p.Scene.FrameCount > 0,
	}
}

func compare(actual float64, op string, target float64) bool {
	switch op {
	case "<":
		return actual < target
	case ">":
		return actual > target
	case "<=":
		return actual <= target
	case ">=":
		return actual >= target
	case "==":
		return actual == target
	}
	return false
}
