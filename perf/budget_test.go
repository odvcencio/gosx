package perf

import (
	"strings"
	"testing"
)

func TestEvaluateBudgetProfileForAllPages(t *testing.T) {
	report := &Report{
		Pages: []PageReport{
			{
				URL:                      "http://localhost:3000/",
				LargestContentfulPaintMs: 1200,
				LongTaskCount:            0,
				TotalBytesTransferred:    30 * 1024,
			},
			{
				URL:                      "http://localhost:3000/docs",
				LargestContentfulPaintMs: 1400,
				LongTaskCount:            0,
				TotalBytesTransferred:    31 * 1024,
			},
		},
	}
	budget := &BudgetFile{
		Profiles: map[string]BudgetProfile{
			"static": {
				Assertions: []string{
					"lcp <= 1500",
					"long_tasks == 0",
					"network_kb <= 35",
				},
			},
		},
	}

	result, err := EvaluateBudget(report, budget, "static")
	if err != nil {
		t.Fatalf("EvaluateBudget: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected budget to pass: %+v", result)
	}
	if len(result.Pages) != 2 {
		t.Fatalf("expected two page results, got %d", len(result.Pages))
	}
}

func TestEvaluateBudgetRouteProfiles(t *testing.T) {
	report := &Report{
		Pages: []PageReport{
			{
				URL:                      "http://localhost:3000/",
				LargestContentfulPaintMs: 1200,
				TotalBytesTransferred:    2 * 1024,
				Coverage: []CoverageEntry{
					{TotalBytes: 0, UsedBytes: 0, UnusedBytes: 0},
				},
			},
			{
				URL:                      "http://localhost:3000/scene",
				LargestContentfulPaintMs: 1600,
				TotalBytesTransferred:    80 * 1024,
				Scene: &SceneMetric{FrameStats: FrameStats{
					P95: 20,
				}},
			},
		},
	}
	budget := &BudgetFile{
		Profiles: map[string]BudgetProfile{
			"static": {
				Assertions: []string{
					"js_total_kb == 0",
					"lcp <= 1500",
				},
			},
			"scene3d": {
				Assertions: []string{
					"network_kb <= 100",
					"scene_p95 <= 33",
				},
			},
		},
		Routes: []BudgetRoute{
			{URL: "/", Profile: "static"},
			{URL: "/scene", Profile: "scene3d"},
		},
	}

	result, err := EvaluateBudget(report, budget, "")
	if err != nil {
		t.Fatalf("EvaluateBudget: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected budget to pass: %+v", result)
	}
	if result.Pages[0].Profile != "static" || result.Pages[1].Profile != "scene3d" {
		t.Fatalf("unexpected profiles: %+v", result.Pages)
	}
}

func TestEvaluateBudgetAllowsCapturedZeroJS(t *testing.T) {
	report := &Report{
		PageReport: PageReport{
			URL:              "http://localhost:3000/",
			CoverageCaptured: true,
		},
	}
	budget := &BudgetFile{
		DefaultProfile: "static",
		Profiles: map[string]BudgetProfile{
			"static": {Assertions: []string{"js_total_kb == 0"}},
		},
	}

	result, err := EvaluateBudget(report, budget, "")
	if err != nil {
		t.Fatalf("EvaluateBudget: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected captured zero-JS budget to pass: %+v", result)
	}
	if !result.Pages[0].Assertions[0].Found {
		t.Fatalf("expected js_total_kb metric to be found")
	}
}

func TestEvaluateBudgetRouteFullURLKeepsHostBoundary(t *testing.T) {
	report := &Report{
		PageReport: PageReport{
			URL:                      "http://127.0.0.1:3000/",
			LargestContentfulPaintMs: 1000,
		},
	}
	budget := &BudgetFile{
		DefaultProfile: "fallback",
		Profiles: map[string]BudgetProfile{
			"fallback": {Assertions: []string{"lcp <= 1500"}},
			"local":    {Assertions: []string{"lcp <= 1"}},
		},
		Routes: []BudgetRoute{
			{URL: "http://localhost:3000/", Profile: "local"},
		},
	}

	result, err := EvaluateBudget(report, budget, "")
	if err != nil {
		t.Fatalf("EvaluateBudget: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected fallback profile to pass: %+v", result)
	}
	if result.Pages[0].Profile != "fallback" {
		t.Fatalf("expected fallback profile, got %q", result.Pages[0].Profile)
	}
}

func TestEvaluateBudgetFailsOnMissingMetricAndRegression(t *testing.T) {
	report := &Report{
		PageReport: PageReport{
			URL:                      "http://localhost:3000/scene",
			LargestContentfulPaintMs: 2500,
		},
	}
	budget := &BudgetFile{
		DefaultProfile: "scene3d",
		Profiles: map[string]BudgetProfile{
			"scene3d": {
				Assertions: []string{
					"lcp <= 1500",
					"scene_p95 <= 33",
				},
			},
		},
	}

	result, err := EvaluateBudget(report, budget, "")
	if err != nil {
		t.Fatalf("EvaluateBudget: %v", err)
	}
	if result.Passed {
		t.Fatalf("expected budget to fail")
	}
	if len(result.Pages) != 1 || len(result.Pages[0].Assertions) != 2 {
		t.Fatalf("unexpected result shape: %+v", result)
	}
	if result.Pages[0].Assertions[0].Passed {
		t.Fatalf("lcp assertion should fail")
	}
	if result.Pages[0].Assertions[1].Found {
		t.Fatalf("scene_p95 should not be found without Scene metrics")
	}

	out := FormatBudgetResult(result)
	if !strings.Contains(out, "gosx perf budget") || !strings.Contains(out, "fail") {
		t.Fatalf("unexpected formatted result:\n%s", out)
	}
}
