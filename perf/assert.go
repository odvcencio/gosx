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

	return Assertion{Metric: metric, Op: op, Value: val}, nil
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
	switch name {
	case "ttfb":
		return p.TTFBMs, true
	case "dcl":
		return p.DCLMs, true
	case "fully_loaded":
		return p.FullyLoadedMs, true
	case "lcp", "lcp_ms":
		return p.LargestContentfulPaintMs, true
	case "cls":
		return p.CumulativeLayoutShift, true
	case "fid", "fid_ms":
		return p.FirstInputDelayMs, true
	case "long_tasks":
		return float64(p.LongTaskCount), true
	case "long_task_total", "long_task_total_ms":
		return p.LongTaskTotalMs, true
	case "tbt", "tbt_ms":
		return p.TotalBlockingTimeMs, true
	case "hydration_total":
		return p.IslandHydrationMs, true
	case "heap_mb":
		return p.JSHeapSizeMB, true
	case "island_count":
		return float64(len(p.Islands)), true
	case "network_kb":
		return float64(p.TotalBytesTransferred) / 1024, true
	case "requests":
		return float64(len(p.Resources)), true
	case "hub_messages":
		return float64(p.HubMessageCount), true
	case "hub_bytes":
		return float64(p.HubMessageBytes), true
	case "hub_sends":
		return float64(p.HubSendCount), true
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
		return p.Scene.FirstFrameMs, true
	case "p50", "scene_p50":
		return p.Scene.FrameStats.P50, true
	case "p95", "scene_p95":
		return p.Scene.FrameStats.P95, true
	case "p99", "scene_p99":
		return p.Scene.FrameStats.P99, true
	case "frame_max", "scene_frame_max":
		return p.Scene.FrameStats.Max, true
	case "dropped_frames", "scene_dropped_frames":
		return float64(p.Scene.DroppedFrames), true
	case "frame_count", "scene_frame_count":
		return float64(p.Scene.FrameCount), true
	}

	return 0, false
}

func hasCoverageMetric(p *PageReport) bool {
	return p.CoverageCaptured || len(p.Coverage) > 0
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
