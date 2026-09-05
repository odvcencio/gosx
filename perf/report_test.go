package perf

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFormatTable(t *testing.T) {
	r := &Report{
		URL:       "http://localhost:3000",
		Timestamp: time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		PageReport: PageReport{
			URL:               "http://localhost:3000",
			TTFBMs:            12.5,
			DCLMs:             45.3,
			FullyLoadedMs:     120.0,
			IslandHydrationMs: 8.2,
			JSHeapSizeMB:      4.5,
			Islands: []IslandMetric{
				{ID: "counter-1", HydrationMs: 3.1},
				{ID: "todo-list", HydrationMs: 5.1},
			},
			Scene: &SceneMetric{
				FirstFrameMs:  16.7,
				DroppedFrames: 2,
				FrameCount:    120,
				FrameStats: FrameStats{
					P50:   8.3,
					P95:   14.2,
					P99:   16.1,
					Max:   22.0,
					Mean:  9.1,
					Count: 120,
				},
				Presentation: &PresentationMetric{
					TelemetrySeries: TelemetrySeries{
						Stats: FrameStats{P50: 16.7, P95: 18.2, P99: 25, Max: 30, Count: 100},
						Cold:  FrameStats{P95: 20, Count: 30},
						Warm:  FrameStats{P95: 17, Count: 70},
					},
					EstimatedRefreshIntervalMs: 16.67,
					EstimatedMissedVsyncs:      2,
					HitchClusters:              []HitchCluster{{IntervalCount: 1}},
				},
				GPU: &SceneGPUTelemetry{
					Total: &TelemetrySeries{Stats: FrameStats{P50: 5, P95: 8, P99: 10, Count: 10}},
				},
			},
			Interactions: []InteractionMetric{
				{Action: "counter-1:increment", DispatchMs: 0.8, PatchCount: 1},
				{Action: "todo-list:add", DispatchMs: 2.3, PatchCount: 3},
			},
		},
	}

	out := FormatTable(r)

	// Verify expected strings are present
	checks := []string{
		"gosx perf — http://localhost:3000",
		"Page Lifecycle",
		"TTFB",
		"12.5ms",
		"DOMContentLoaded",
		"45.3ms",
		"Islands",
		"2 (hydrated in 8.2ms total)",
		"counter-1",
		"3.1ms",
		"todo-list",
		"5.1ms",
		"JS Heap",
		"4.5 MB",
		"Scene3D",
		"First CPU submit",
		"16.7ms",
		"CPU render/submit (120 measures)",
		"p50    8.3ms",
		"p95    14.2ms",
		"p99    16.1ms",
		"max    22.0ms",
		"rAF presentation cadence (100 intervals)",
		"missed-vsync estimate 2",
		"GPU total",
		"Frame count",
		"120",
		"Interactions",
		"counter-1:increment",
		"dispatch    0.8ms    patches    1",
		"todo-list:add",
		"dispatch    2.3ms    patches    3",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("FormatTable output missing %q\nGot:\n%s", want, out)
		}
	}
}

func TestFormatTableOmitsEmptySections(t *testing.T) {
	r := &Report{
		URL:       "http://localhost:3000",
		Timestamp: time.Now(),
		PageReport: PageReport{
			URL:    "http://localhost:3000",
			TTFBMs: 10,
			DCLMs:  20,
		},
	}

	out := FormatTable(r)

	if strings.Contains(out, "Scene3D") {
		t.Errorf("expected no Scene3D section when Scene is nil\nGot:\n%s", out)
	}
	if strings.Contains(out, "Interactions") {
		t.Errorf("expected no Interactions section when empty\nGot:\n%s", out)
	}
	if strings.Contains(out, "Islands") {
		t.Errorf("expected no Islands section when empty\nGot:\n%s", out)
	}
}

func TestFormatJSON(t *testing.T) {
	r := &Report{
		URL:       "http://localhost:3000",
		Timestamp: time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		Pages: []PageReport{{
			URL: "http://localhost:3000/nested",
			Measurements: &MeasurementStatus{Version: measurementStatusVersion, Groups: map[string]CollectorStatus{
				"resource-waterfall": {State: MeasurementOK},
			}},
		}},
		PageReport: PageReport{
			URL:    "http://localhost:3000",
			TTFBMs: 10.5,
			DCLMs:  40.2,
			Measurements: &MeasurementStatus{Version: measurementStatusVersion, Groups: map[string]CollectorStatus{
				"resource-waterfall": {State: MeasurementOK},
				"lcp":                {State: MeasurementUnobserved, Reason: "no paint"},
			}},
		},
	}

	data, err := FormatJSON(r)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}

	// Roundtrip: unmarshal back into a Report
	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.URL != r.URL {
		t.Errorf("URL: got %q, want %q", got.URL, r.URL)
	}
	if len(got.Pages) != 1 || got.Pages[0].URL != "http://localhost:3000/nested" {
		t.Fatalf("nested pages did not round trip: %+v", got.Pages)
	}
	if status := got.Pages[0].measurementStatus("resource-waterfall"); status.State != MeasurementOK {
		t.Fatalf("nested resource status = %+v", status)
	}
	if got.PageReport.TTFBMs != r.PageReport.TTFBMs {
		t.Errorf("TTFBMs: got %f, want %f", got.PageReport.TTFBMs, r.PageReport.TTFBMs)
	}
	if got.PageReport.DCLMs != r.PageReport.DCLMs {
		t.Errorf("DCLMs: got %f, want %f", got.PageReport.DCLMs, r.PageReport.DCLMs)
	}
	if !got.Timestamp.Equal(r.Timestamp) {
		t.Errorf("Timestamp: got %v, want %v", got.Timestamp, r.Timestamp)
	}
	if got.PageReport.Measurements == nil || got.PageReport.Measurements.Version != measurementStatusVersion {
		t.Fatalf("measurement status did not round trip: %+v", got.PageReport.Measurements)
	}
	if status := got.PageReport.measurementStatus("lcp"); status.State != MeasurementUnobserved || status.Reason != "no paint" {
		t.Fatalf("lcp status = %+v", status)
	}
}

func TestLoadLegacyReportLeavesOptionalMeasurementsUnknown(t *testing.T) {
	var got Report
	if err := json.Unmarshal([]byte(`{
		"url":"legacy",
		"timestamp":"2026-04-11T12:00:00Z",
		"pages":[{"url":"legacy","totalBytesTransferred":0,"lcpMs":0,"longTaskCount":0}]
	}`), &got); err != nil {
		t.Fatalf("Unmarshal legacy: %v", err)
	}
	if len(got.Pages) != 1 {
		t.Fatalf("legacy pages not preserved: %+v", got.Pages)
	}
	for _, metric := range []string{"network_kb", "lcp", "long_tasks"} {
		if value, ok := ResolvePageMetric(metric, &got.Pages[0]); ok {
			t.Fatalf("legacy %s resolved to %.2f without measurement status", metric, value)
		}
	}
}

func TestLoadFlattenedReportFallbackKeepsTopLevelURL(t *testing.T) {
	path := writeTempReportJSON(t, `{
		"url":"https://example.test/",
		"timestamp":"2026-04-11T12:00:00Z",
		"ttfbMs":10
	}`)
	report, err := LoadReport(path)
	if err != nil {
		t.Fatalf("LoadReport: %v", err)
	}
	if len(report.Pages) != 1 || report.Pages[0].URL != "https://example.test/" {
		t.Fatalf("flattened fallback lost top-level URL: %+v", report.Pages)
	}
}

func TestFormatTableAndWaterfallSurfaceOptionalStatus(t *testing.T) {
	r := &Report{
		URL: "http://localhost:3000",
		PageReport: PageReport{
			URL: "http://localhost:3000",
			Measurements: &MeasurementStatus{Version: measurementStatusVersion, Groups: map[string]CollectorStatus{
				"resource-waterfall": {State: MeasurementFailed, Reason: "cdp timeout"},
				"webgl-info":         {State: MeasurementUnsupported, Reason: "hook unavailable"},
			}},
		},
	}
	table := FormatTable(r)
	for _, want := range []string{"Optional Measurements", "Resource waterfall", "failed: cdp timeout", "WebGL info", "unsupported: hook unavailable"} {
		if !strings.Contains(table, want) {
			t.Fatalf("FormatTable missing %q\n%s", want, table)
		}
	}
	waterfall := FormatWaterfallTable(r)
	if !strings.Contains(waterfall, "Resource waterfall unavailable: cdp timeout") {
		t.Fatalf("FormatWaterfallTable did not surface unavailable status:\n%s", waterfall)
	}
}

func TestFormatTableDoesNotGreenwashLegacyOptionalScalars(t *testing.T) {
	r := &Report{URL: "legacy", PageReport: PageReport{
		URL:                      "legacy",
		LargestContentfulPaintMs: 1200,
		LongTaskCount:            1,
		LongTaskTotalMs:          60,
		TotalBlockingTimeMs:      10,
	}}
	table := FormatTable(r)
	if strings.Contains(table, "Core Web Vitals") || strings.Contains(table, "Main-Thread Blocking") || strings.Contains(table, "(good)") {
		t.Fatalf("legacy optional scalars should not print as measured green values:\n%s", table)
	}
	if !strings.Contains(table, "LCP") || !strings.Contains(table, "unknown") || !strings.Contains(table, "Long tasks") {
		t.Fatalf("legacy optional scalar availability should be visible:\n%s", table)
	}
}

func TestFormatTableShowsMeasuredZeroOptionalMetrics(t *testing.T) {
	r := &Report{URL: "measured", PageReport: PageReport{
		URL: "measured",
		Measurements: &MeasurementStatus{Version: measurementStatusVersion, Groups: map[string]CollectorStatus{
			"lcp":              {State: MeasurementOK},
			"cls":              {State: MeasurementOK},
			"long-tasks":       {State: MeasurementOK},
			"runtime-counters": {State: MeasurementOK},
		}},
	}}
	table := FormatTable(r)
	for _, want := range []string{"Core Web Vitals", "LCP", "0.0ms", "CLS", "0.000", "Main-Thread Blocking", "Long tasks", "0", "Runtime Throughput"} {
		if !strings.Contains(table, want) {
			t.Fatalf("measured zero optional metric missing %q\n%s", want, table)
		}
	}
}

func TestReportSchemaReferencesRootAndPageMeasurements(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	data, err := os.ReadFile("report.schema.json")
	if err != nil {
		t.Fatalf("Read schema: %v", err)
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema JSON: %v", err)
	}
	if !strings.Contains(string(schema.Properties["measurements"]), "#/$defs/measurements") {
		t.Fatalf("root measurements property missing schema ref: %s", schema.Properties["measurements"])
	}
	page, ok := schema.Defs["page"]
	if !ok {
		t.Fatalf("page definition missing")
	}
	if !strings.Contains(string(page.Properties["measurements"]), "#/$defs/measurements") {
		t.Fatalf("page measurements property missing schema ref: %s", page.Properties["measurements"])
	}
}

func writeTempReportJSON(t *testing.T, data string) string {
	t.Helper()
	path := t.TempDir() + "/report.json"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
