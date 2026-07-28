package perf

import (
	"math"
	"strings"
	"testing"
)

func TestComputeFrameStats(t *testing.T) {
	durations := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	fs := ComputeFrameStats(durations)

	if fs.Count != 10 {
		t.Fatalf("Count: got %d, want 10", fs.Count)
	}
	if fs.Max != 10 {
		t.Fatalf("Max: got %f, want 10", fs.Max)
	}
	// Mean = 55/10 = 5.5
	if math.Abs(fs.Mean-5.5) > 0.01 {
		t.Fatalf("Mean: got %f, want 5.5", fs.Mean)
	}
	// P50 = median of [1..10] with linear interpolation at index 4.5 → 5.5
	if math.Abs(fs.P50-5.5) > 0.01 {
		t.Fatalf("P50: got %f, want 5.5", fs.P50)
	}
	// P95 at rank = 0.95*9 = 8.55 → sorted[8] + 0.55*(sorted[9]-sorted[8]) = 9 + 0.55 = 9.55
	if math.Abs(fs.P95-9.55) > 0.1 {
		t.Fatalf("P95: got %f, want ~9.55", fs.P95)
	}
	// P99 at rank = 0.99*9 = 8.91 → sorted[8] + 0.91*(sorted[9]-sorted[8]) = 9 + 0.91 = 9.91
	if math.Abs(fs.P99-9.91) > 0.1 {
		t.Fatalf("P99: got %f, want ~9.91", fs.P99)
	}
}

func TestComputeFrameStatsEmpty(t *testing.T) {
	fs := ComputeFrameStats(nil)
	if fs.Count != 0 {
		t.Fatalf("Count: got %d, want 0", fs.Count)
	}
	if fs.P50 != 0 || fs.P95 != 0 || fs.P99 != 0 || fs.Max != 0 || fs.Mean != 0 {
		t.Fatalf("expected zero FrameStats, got %+v", fs)
	}
}

func TestComputeFrameStatsSingle(t *testing.T) {
	fs := ComputeFrameStats([]float64{42.0})
	if fs.Count != 1 {
		t.Fatalf("Count: got %d, want 1", fs.Count)
	}
	if fs.P50 != 42.0 {
		t.Fatalf("P50: got %f, want 42.0", fs.P50)
	}
	if fs.P95 != 42.0 {
		t.Fatalf("P95: got %f, want 42.0", fs.P95)
	}
	if fs.P99 != 42.0 {
		t.Fatalf("P99: got %f, want 42.0", fs.P99)
	}
	if fs.Max != 42.0 {
		t.Fatalf("Max: got %f, want 42.0", fs.Max)
	}
	if fs.Mean != 42.0 {
		t.Fatalf("Mean: got %f, want 42.0", fs.Mean)
	}
}

func TestComputePresentationMetricSeparatesCadenceFromMissedVsyncEstimate(t *testing.T) {
	samples := []AnimationFrameSample{
		{Duration: 16.7, StartTime: 100, Phase: "cold"},
		{Duration: 16.7, StartTime: 116.7, Phase: "cold"},
		{Duration: 51, StartTime: 133.4, Phase: "warm"},
		{Duration: 16.7, StartTime: 184.4, Phase: "warm"},
	}
	got := ComputePresentationMetric(samples)

	if got.Stats.Count != 4 || got.Cold.Count != 2 || got.Warm.Count != 2 {
		t.Fatalf("unexpected phase stats: %+v", got.TelemetrySeries)
	}
	if math.Abs(got.EstimatedRefreshIntervalMs-16.7) > 0.01 {
		t.Fatalf("refresh estimate: got %.3f, want 16.7", got.EstimatedRefreshIntervalMs)
	}
	if got.EstimatedMissedVsyncs != 2 {
		t.Fatalf("missed vsyncs: got %d, want 2", got.EstimatedMissedVsyncs)
	}
	if got.HitchIntervals != 1 || len(got.HitchClusters) != 1 {
		t.Fatalf("unexpected hitch summary: %+v", got)
	}
	if !strings.Contains(got.Source, "requestAnimationFrame") {
		t.Fatalf("source must identify rAF, got %q", got.Source)
	}
}

func TestBuildSceneMetricCapturesRendererTelemetry(t *testing.T) {
	snapshot := SceneTelemetrySnapshot{
		Available:        true,
		RAFAvailable:     true,
		SceneStartedAt:   100,
		WarmAt:           150,
		DevicePixelRatio: 2,
		PresentedFrameIntervals: []AnimationFrameSample{
			{Duration: 16.7, StartTime: 110, Phase: "cold"},
			{Duration: 16.6, StartTime: 160, Phase: "warm"},
		},
		AttributeSamples: []SceneTelemetrySample{
			{Name: "data-gosx-scene3d-webgpu-gpu-ms", NumericValue: 8, StartTime: 120, Phase: "cold"},
			{Name: "data-gosx-scene3d-webgpu-gpu-ms", NumericValue: 5, StartTime: 170, Phase: "warm"},
			{Name: "data-gosx-scene3d-webgpu-gpu-pass-shadow-ms", NumericValue: 1.5, StartTime: 170, Phase: "warm"},
			{Name: "data-gosx-scene3d-planner-cpu-ms", NumericValue: 0.4, StartTime: 170, Phase: "warm"},
		},
		Mounts: []SceneMountSnapshot{{
			Index: 0,
			ID:    "hero",
			Attributes: map[string]string{
				"data-gosx-scene3d-backend":                  "webgpu",
				"data-gosx-scene3d-renderer":                 "webgpu",
				"data-gosx-scene3d-quality-active":           "high",
				"data-gosx-scene3d-pixel-ratio":              "1.5",
				"data-gosx-scene3d-webgpu-gpu-timing":        "measured",
				"data-gosx-scene3d-retained-cache-hits":      "7",
				"data-gosx-scene3d-retained-allocations":     "2",
				"data-gosx-scene3d-render-pipeline-failures": "0",
			},
			Canvas: &SceneCanvasSnapshot{Width: 1200, Height: 600, CSSWidth: 800, CSSHeight: 400},
		}},
	}
	measures := []PerfEntry{
		{Name: "scene3d-render", StartTime: 105, Duration: 3},
		{Name: "scene3d-render", StartTime: 165, Duration: 2},
		{Name: "scene3d-bundle", StartTime: 106, Duration: 1},
	}

	got := BuildSceneMetric(measures, snapshot, 2)
	if got.CPURenderSubmit == nil || got.CPURenderSubmit.Cold.Count != 1 || got.CPURenderSubmit.Warm.Count != 1 {
		t.Fatalf("CPU submit phases not captured: %+v", got.CPURenderSubmit)
	}
	if got.Presentation == nil || got.DroppedFrames != 0 {
		t.Fatalf("presentation cadence not captured honestly: %+v", got.Presentation)
	}
	if got.GPU == nil || got.GPU.Total == nil || got.GPU.Total.Warm.P50 != 5 {
		t.Fatalf("GPU total not captured: %+v", got.GPU)
	}
	if got.GPU.Passes["shadow"].Stats.P50 != 1.5 {
		t.Fatalf("GPU pass not captured: %+v", got.GPU.Passes)
	}
	if got.Mounts[0].Backend != "webgpu" || got.Mounts[0].Profile != "high" ||
		math.Abs(got.Mounts[0].EffectivePixelRatio-1.5) > 0.01 {
		t.Fatalf("runtime profile not captured: %+v", got.Mounts[0])
	}
	if got.Geometry["retained-cache-hits"] != 7 || got.Pipeline["render-pipeline-failures"] != 0 {
		t.Fatalf("geometry/pipeline telemetry missing: geometry=%v pipeline=%v", got.Geometry, got.Pipeline)
	}
}
