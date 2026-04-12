package perf

import (
	"encoding/json"
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
		"First frame",
		"16.7ms",
		"Frame budget (120 frames)",
		"p50    8.3ms",
		"p95    14.2ms",
		"p99    16.1ms",
		"max    22.0ms",
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
		PageReport: PageReport{
			URL:    "http://localhost:3000",
			TTFBMs: 10.5,
			DCLMs:  40.2,
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
	if got.PageReport.TTFBMs != r.PageReport.TTFBMs {
		t.Errorf("TTFBMs: got %f, want %f", got.PageReport.TTFBMs, r.PageReport.TTFBMs)
	}
	if got.PageReport.DCLMs != r.PageReport.DCLMs {
		t.Errorf("DCLMs: got %f, want %f", got.PageReport.DCLMs, r.PageReport.DCLMs)
	}
	if !got.Timestamp.Equal(r.Timestamp) {
		t.Errorf("Timestamp: got %v, want %v", got.Timestamp, r.Timestamp)
	}
}
