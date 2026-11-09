package perf

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func timedPage(lcp, tbt float64, bytes int64) PageReport {
	return PageReport{
		URL:                      "http://127.0.0.1/demos/water",
		LargestContentfulPaintMs: lcp,
		TotalBlockingTimeMs:      tbt,
		TotalBytesTransferred:    bytes,
		MetricAvailability:       &MetricAvailability{Navigation: true, LCP: true, LongTasks: true, Network: true},
	}
}

func TestMedianTiming(t *testing.T) {
	got := MedianTiming([]TimingSample{{LCPMs: 2196}, {LCPMs: 876}, {LCPMs: 900}})
	if got.LCPMs != 900 {
		t.Fatalf("odd median = %v, want 900", got.LCPMs)
	}
	if got := MedianTiming([]TimingSample{{LCPMs: 100}, {LCPMs: 300}}); got.LCPMs != 200 {
		t.Fatalf("even median = %v, want 200", got.LCPMs)
	}
}

func TestSampleTimingsGatesOnMedianAndKeepsStrictMetricsSingleSample(t *testing.T) {
	report := &Report{Pages: []PageReport{timedPage(2196, 500, 1000)}}
	finalizeScenarioReport(report)
	later := []PageReport{timedPage(876, 300, 99999), timedPage(900, 400, 88888)}
	calls := 0
	var diag bytes.Buffer
	s := &Scenario{diagnostics: &diag, Coverage: true, Interactions: []Interaction{{Kind: "click"}}}
	sampleTimings(s, report, 3, func(sc *Scenario) (*Report, error) {
		if sc.Coverage || len(sc.Interactions) != 0 || len(sc.URLs) != 1 {
			t.Errorf("extra sample must be plain: %+v", sc)
		}
		p := later[calls]
		calls++
		return &Report{Pages: []PageReport{p}}, nil
	})
	if calls != 2 {
		t.Fatalf("ran %d extra samples, want 2", calls)
	}
	page := report.Pages[0]
	if page.LargestContentfulPaintMs != 900 || page.TotalBlockingTimeMs != 400 {
		t.Fatalf("timings not median: lcp %v tbt %v", page.LargestContentfulPaintMs, page.TotalBlockingTimeMs)
	}
	if page.TotalBytesTransferred != 1000 {
		t.Fatalf("bytes must stay the first sample, got %d", page.TotalBytesTransferred)
	}
	if report.PageReport.LargestContentfulPaintMs != 900 {
		t.Fatalf("embedded single-page report not refreshed")
	}
	budget := &BudgetFile{DefaultProfile: "p", Profiles: map[string]BudgetProfile{"p": {Assertions: []string{"lcp <= 2000", "network_kb <= 2"}}}}
	res, err := EvaluateBudget(report, budget, "")
	if err != nil || !res.Passed {
		t.Fatalf("median 900 must pass: %v %+v", err, res)
	}
	out := FormatBudgetResult(res)
	if !strings.Contains(out, "2196 / 876 / 900") || !strings.Contains(out, "median 900.00") {
		t.Fatalf("report lacks samples and median:\n%s", out)
	}
	if strings.Contains(out, "samples network_kb") {
		t.Fatalf("strict metric printed as sampled:\n%s", out)
	}
}

func TestSampleTimingsStrictMetricFailsOnFirstSample(t *testing.T) {
	report := &Report{Pages: []PageReport{timedPage(900, 1, 5000)}}
	sampleTimings(&Scenario{diagnostics: &bytes.Buffer{}}, report, 3, func(*Scenario) (*Report, error) {
		return &Report{Pages: []PageReport{timedPage(900, 1, 1)}}, nil
	})
	budget := &BudgetFile{DefaultProfile: "p", Profiles: map[string]BudgetProfile{"p": {Assertions: []string{"network_kb <= 2"}}}}
	res, _ := EvaluateBudget(report, budget, "")
	if res.Passed {
		t.Fatal("network_kb must be judged on the first sample (5000 bytes), not a later one")
	}
}

func TestSampleTimingsStillFailsOnRealRegression(t *testing.T) {
	report := &Report{Pages: []PageReport{timedPage(2500, 1, 1)}}
	sampleTimings(&Scenario{diagnostics: &bytes.Buffer{}}, report, 3, func(*Scenario) (*Report, error) {
		return &Report{Pages: []PageReport{timedPage(2400, 1, 1)}}, nil
	})
	res, _ := EvaluateBudget(report, waterBudget(), "")
	if res.Passed {
		t.Fatal("a median over the limit must fail")
	}
}

func TestSampleTimingsDropsFailedAndRecoveredSamples(t *testing.T) {
	report := &Report{Pages: []PageReport{timedPage(800, 1, 1)}}
	n := 0
	sampleTimings(&Scenario{diagnostics: &bytes.Buffer{}}, report, 3, func(*Scenario) (*Report, error) {
		n++
		if n == 1 {
			return nil, errors.New("chrome died")
		}
		return &Report{Pages: []PageReport{recoveredWaterPage("webgpu-device-lost")}}, nil
	})
	if got := report.Pages[0]; got.LargestContentfulPaintMs != 800 || len(got.TimingSamples) != 1 {
		t.Fatalf("only the first sample should remain: %+v", got.TimingSamples)
	}
}

func TestSampleTimingsSkipsRecoveredFirstPageAndSingleSample(t *testing.T) {
	report := &Report{Pages: []PageReport{recoveredWaterPage("webgpu-device-lost")}}
	run := func(*Scenario) (*Report, error) { t.Fatal("must not run"); return nil, nil }
	sampleTimings(&Scenario{diagnostics: &bytes.Buffer{}}, report, 3, run)
	ok := &Report{Pages: []PageReport{timedPage(1, 1, 1)}}
	sampleTimings(&Scenario{diagnostics: &bytes.Buffer{}}, ok, 1, run)
	if len(ok.Pages[0].TimingSamples) != 0 {
		t.Fatal("n=1 must record no samples")
	}
}

func TestIsTimingMetric(t *testing.T) {
	for _, m := range []string{"lcp", "tbt", "ttfb"} {
		if !IsTimingMetric(m) {
			t.Errorf("%s should be a timing metric", m)
		}
	}
	for _, m := range []string{"network_kb", "requests", "js_total_kb", "long_tasks", "scene_counter_x", "cls"} {
		if IsTimingMetric(m) {
			t.Errorf("%s must stay single-sample and strict", m)
		}
	}
}
