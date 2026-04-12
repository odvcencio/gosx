package perf

import (
	"math"
	"testing"
)

func TestParseAssertion(t *testing.T) {
	a, err := ParseAssertion("p95 < 12")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Metric != "p95" {
		t.Fatalf("Metric: got %q, want %q", a.Metric, "p95")
	}
	if a.Op != "<" {
		t.Fatalf("Op: got %q, want %q", a.Op, "<")
	}
	if a.Value != 12 {
		t.Fatalf("Value: got %f, want 12", a.Value)
	}
}

func TestParseAssertionAllOps(t *testing.T) {
	for _, op := range []string{"<", ">", "<=", ">=", "=="} {
		a, err := ParseAssertion("ttfb " + op + " 100")
		if err != nil {
			t.Fatalf("op %q: unexpected error: %v", op, err)
		}
		if a.Op != op {
			t.Fatalf("op: got %q, want %q", a.Op, op)
		}
	}
}

func TestParseAssertionError(t *testing.T) {
	cases := []string{
		"garbage",
		"",
		"p95 < ",
		"p95 < foo",
		"p95 != 10",
	}
	for _, expr := range cases {
		_, err := ParseAssertion(expr)
		if err == nil {
			t.Fatalf("expected error for %q, got nil", expr)
		}
	}
}

func TestEvalAssertions(t *testing.T) {
	r := &Report{
		PageReport: PageReport{
			TTFBMs: 15,
		},
	}

	assertions := []Assertion{
		{Metric: "ttfb", Op: "<", Value: 20},
		{Metric: "ttfb", Op: "<", Value: 10},
	}

	results := EvalAssertions(assertions, r)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Passed {
		t.Fatalf("ttfb < 20 should pass (actual=15)")
	}
	if results[0].Actual != 15 {
		t.Fatalf("actual: got %f, want 15", results[0].Actual)
	}
	if results[1].Passed {
		t.Fatalf("ttfb < 10 should fail (actual=15)")
	}
}

func TestEvalAssertionsMissingScene(t *testing.T) {
	r := &Report{
		PageReport: PageReport{
			TTFBMs: 15,
		},
	}

	assertions := []Assertion{
		{Metric: "p95", Op: "<", Value: 20},
	}
	results := EvalAssertions(assertions, r)
	if results[0].Passed {
		t.Fatalf("p95 assertion should fail when Scene is nil")
	}
}

func TestResolveMetricScene(t *testing.T) {
	r := &Report{
		PageReport: PageReport{
			Scene: &SceneMetric{
				FirstFrameMs: 8.5,
				FrameStats: FrameStats{
					P50: 5.0,
					P95: 11.3,
					P99: 14.2,
					Max: 18.0,
				},
				DroppedFrames: 3,
				FrameCount:    120,
			},
		},
	}

	cases := []struct {
		name string
		want float64
	}{
		{"first_frame", 8.5},
		{"p50", 5.0},
		{"p95", 11.3},
		{"p99", 14.2},
		{"frame_max", 18.0},
		{"dropped_frames", 3},
		{"frame_count", 120},
	}

	for _, tc := range cases {
		val, ok := ResolveMetric(tc.name, r)
		if !ok {
			t.Fatalf("ResolveMetric(%q): expected ok=true", tc.name)
		}
		if math.Abs(val-tc.want) > 0.01 {
			t.Fatalf("ResolveMetric(%q): got %f, want %f", tc.name, val, tc.want)
		}
	}
}

func TestResolveMetricPage(t *testing.T) {
	r := &Report{
		PageReport: PageReport{
			TTFBMs:            42,
			DCLMs:             100,
			IslandHydrationMs: 25,
			JSHeapSizeMB:      8.5,
			Islands:           []IslandMetric{{ID: "a"}, {ID: "b"}},
		},
	}

	cases := []struct {
		name string
		want float64
	}{
		{"ttfb", 42},
		{"dcl", 100},
		{"hydration_total", 25},
		{"heap_mb", 8.5},
		{"island_count", 2},
	}

	for _, tc := range cases {
		val, ok := ResolveMetric(tc.name, r)
		if !ok {
			t.Fatalf("ResolveMetric(%q): expected ok=true", tc.name)
		}
		if math.Abs(val-tc.want) > 0.01 {
			t.Fatalf("ResolveMetric(%q): got %f, want %f", tc.name, val, tc.want)
		}
	}
}

func TestResolveMetricUnknown(t *testing.T) {
	r := &Report{}
	_, ok := ResolveMetric("nonexistent", r)
	if ok {
		t.Fatalf("expected ok=false for unknown metric")
	}
}
