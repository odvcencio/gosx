package perf

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePageMetricRequiresRecordedEvidence(t *testing.T) {
	names := []string{"lcp", "lcp_ms", "cls", "fid", "fid_ms", "long_tasks", "long_task_total", "long_task_total_ms", "tbt", "tbt_ms", "network_kb", "requests", "hub_messages", "hub_bytes", "hub_sends", "first_frame", "scene_first_frame", "p50", "scene_p50", "p95", "scene_p95", "p99", "scene_p99", "frame_max", "scene_frame_max", "dropped_frames", "scene_dropped_frames", "frame_count", "scene_frame_count"}
	for _, metadata := range []*MetricAvailability{nil, {}} {
		p := PageReport{MetricAvailability: metadata, Scene: BuildSceneMetric(nil, SceneTelemetrySnapshot{}, 0)}
		for _, name := range names {
			if _, found := ResolvePageMetric(name, &p); found {
				t.Errorf("%s: an empty sample was reported as zero (metadata=%v)", name, metadata)
			}
		}
	}
}

func TestResolvePageMetricAcceptsRecordedZero(t *testing.T) {
	p := PageReport{MetricAvailability: &MetricAvailability{LCP: true, CLS: true, FID: true, LongTasks: true, Network: true, RuntimeCounters: true, SceneFrames: true}, Scene: &SceneMetric{FrameStats: FrameStats{Count: 1}, Presentation: &PresentationMetric{TelemetrySeries: TelemetrySeries{Stats: FrameStats{Count: 1}}}}}
	for _, name := range []string{"lcp", "cls", "fid", "long_tasks", "long_task_total", "tbt", "network_kb", "requests", "hub_messages", "hub_bytes", "hub_sends", "first_frame", "p50", "p95", "p99", "frame_max", "dropped_frames", "frame_count"} {
		value, found := ResolvePageMetric(name, &p)
		if !found || value != 0 {
			t.Errorf("%s: recorded zero = %v,%v", name, value, found)
		}
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PageReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if value, found := ResolvePageMetric("tbt", &decoded); !found || value != 0 {
		t.Fatalf("recorded zero lost in JSON: %v,%v", value, found)
	}
}

func TestResolvePageMetricLegacySamplesAndExplicitFailure(t *testing.T) {
	p := PageReport{LargestContentfulPaintMs: 12, FirstInputDelayMs: 3, CumulativeLayoutShift: .1, LongTasks: []LongTaskMetric{{DurationMs: 60}}, LongTaskCount: 1, TotalBytesTransferred: 1, HubMessageCount: 1, Scene: &SceneMetric{FrameCount: 2}}
	for _, name := range []string{"lcp", "cls", "fid", "long_tasks", "tbt", "network_kb", "hub_messages", "frame_count"} {
		if _, found := ResolvePageMetric(name, &p); !found {
			t.Errorf("lost legacy sample: %s", name)
		}
	}
	p.MetricAvailability = &MetricAvailability{}
	for _, name := range []string{"lcp", "cls", "fid", "long_tasks", "tbt", "network_kb", "hub_messages", "frame_count"} {
		if _, found := ResolvePageMetric(name, &p); found {
			t.Errorf("explicit collection failure ignored: %s", name)
		}
	}
}

func TestCollectPageReportRecordsOptionalMetricAvailability(t *testing.T) {
	for _, fail := range []string{"", "collect/vitals", "collect/long-tasks", "collect/resource-waterfall"} {
		t.Run(fail, func(t *testing.T) {
			queries := successfulPageReportQueries()
			queries.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) { return nil, nil }
			queries.evaluate = func(_ *Driver, expr string, dst interface{}) error {
				switch {
				case strings.Contains(expr, "largestContentfulPaint"):
					return json.Unmarshal([]byte(`{"availability":{"lcp":true,"cls":true,"fid":true},"instrumented":true,"lcp":12}`), dst)
				case strings.Contains(expr, "longTasks"):
					return json.Unmarshal([]byte(`{"available":true,"entries":[]}`), dst)
				default:
					return nil
				}
			}
			runner := func(phase string, _ bool, query func() error) error {
				if err := query(); err != nil {
					return err
				}
				if phase == fail {
					return errors.New("optional query failed after partial decoding")
				}
				return nil
			}
			p, err := collectPageReport(nil, "https://example.test", queries, runner)
			if err != nil {
				t.Fatal(err)
			}
			for name, failed := range map[string]bool{"lcp": fail == "collect/vitals", "cls": fail == "collect/vitals", "fid": fail == "collect/vitals", "hub_sends": fail == "collect/vitals", "long_tasks": fail == "collect/long-tasks", "tbt": fail == "collect/long-tasks", "network_kb": fail == "collect/resource-waterfall"} {
				if _, found := ResolvePageMetric(name, p); found == failed {
					t.Errorf("%s availability=%v after %s", name, found, fail)
				}
			}
		})
	}
}

func TestBudgetMissingEvidenceFailsDuringRendererRecovery(t *testing.T) {
	for _, recovery := range []string{"", "webgpu-device-lost"} {
		p := recoveredWaterPage(recovery)
		p.LargestContentfulPaintMs = 0
		p.MetricAvailability = &MetricAvailability{}
		budget := &BudgetFile{DefaultProfile: "proof", Profiles: map[string]BudgetProfile{"proof": {Assertions: []string{"lcp <= 2000", "tbt <= 200", "network_kb <= 1800", "scene_p95 <= 33"}}}}
		r, err := EvaluateBudget(&Report{PageReport: p}, budget, "")
		if err != nil {
			t.Fatal(err)
		}
		if r.Passed || r.Pages[0].Inconclusive {
			t.Fatalf("missing evidence was exempted: %+v", r)
		}
		for _, a := range r.Pages[0].Assertions {
			if a.Found || a.Passed || a.Inconclusive {
				t.Errorf("missing measurement passed: %+v", a)
			}
		}
		budget.Profiles["proof"] = BudgetProfile{Assertions: []string{"lcp? <= 2000", "scene_p95? <= 33"}}
		r, err = EvaluateBudget(&Report{PageReport: p}, budget, "")
		if err != nil || !r.Passed {
			t.Fatalf("explicit optional metrics: %+v %v", r, err)
		}
		for _, a := range r.Pages[0].Assertions {
			if !a.Skipped || a.Found {
				t.Errorf("optional missing measurement was not skipped: %+v", a)
			}
		}
	}
}

func TestCompareReportsDoesNotTurnMissingDataIntoImprovement(t *testing.T) {
	baseline := PageReport{LargestContentfulPaintMs: 100, TotalBlockingTimeMs: 10, LongTaskCount: 1, TotalBytesTransferred: 100, Scene: &SceneMetric{FirstFrameMs: 1, FrameStats: FrameStats{P50: 1, P95: 2, P99: 3, Count: 2}}}
	missing := PageReport{MetricAvailability: &MetricAvailability{}, Scene: &SceneMetric{}}
	cmp := CompareReports(&Report{Pages: []PageReport{baseline}}, &Report{Pages: []PageReport{missing}})
	for _, m := range cmp.Metrics {
		if m.Name == "LCP" || m.Name == "TBT" || m.Name == "Scene p95" || m.Name == "Network bytes" {
			t.Errorf("missing measurement compared as an improvement: %+v", m)
		}
	}
	for _, name := range []string{"LCP", "TBT", "Scene p95", "Network bytes"} {
		if !strings.Contains(strings.Join(cmp.Unavailable, ","), name) {
			t.Errorf("missing comparison was not disclosed: %s", name)
		}
	}
	if !strings.Contains(FormatComparison(cmp, 5), "Not compared (missing evidence in one report)") {
		t.Fatal("human comparison hid unavailable metrics")
	}
	missing.MetricAvailability = &MetricAvailability{LCP: true, LongTasks: true, Network: true}
	cmp = CompareReports(&Report{Pages: []PageReport{baseline}}, &Report{Pages: []PageReport{missing}})
	found := false
	for _, m := range cmp.Metrics {
		if m.Name == "TBT" {
			found = true
			if m.Candidate != 0 || m.DeltaPct != -100 {
				t.Fatalf("recorded zero improvement = %+v", m)
			}
		}
	}
	if !found {
		t.Fatal("recorded zero should be comparable")
	}
}

func TestFormatTableShowsUnavailableAndRecordedZeros(t *testing.T) {
	p := PageReport{MetricAvailability: &MetricAvailability{}, LargestContentfulPaintMs: 123, TotalBlockingTimeMs: 123, LongTaskCount: 1, TotalBytesTransferred: 123, Scene: &SceneMetric{}}
	out := FormatTable(&Report{PageReport: p})
	for _, line := range []string{"LCP                     unavailable", "Main-Thread Blocking\n    unavailable", "Network\n    unavailable", "Frame count             unavailable"} {
		if !strings.Contains(out, line) {
			t.Errorf("missing marker %q in:\n%s", line, out)
		}
	}
	if strings.Contains(out, "123.0ms") || strings.Contains(out, "good") {
		t.Fatalf("unavailable metrics appeared as measured: %s", out)
	}
	p = PageReport{MetricAvailability: &MetricAvailability{LCP: true, CLS: true, FID: true, LongTasks: true, Network: true}}
	out = FormatTable(&Report{PageReport: p})
	for _, line := range []string{"LCP                     0.0ms", "CLS                     0.000", "Total blocking time     0.0ms", "Total requests          0"} {
		if !strings.Contains(out, line) {
			t.Errorf("captured zero missing %q in:\n%s", line, out)
		}
	}
}

func TestLoadReportNormalizesLegacySinglePageAvailability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	for _, input := range []string{`{"url":"https://example.test","lcpMs":12}`, `{"url":"https://example.test","lcpMs":0,"metricAvailability":{"lcp":true}}`} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		r, err := LoadReport(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Pages) != 1 || r.Pages[0].URL != "https://example.test" {
			t.Fatalf("single-page URL lost during normalization: %+v", r)
		}
		if _, found := ResolvePageMetric("lcp", &r.Pages[0]); !found {
			t.Fatal("legacy measurement or explicit recorded zero lost during normalization")
		}
	}
}
