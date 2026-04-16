package perf

import (
	"sort"
	"strings"
	"time"
)

// Report is the top-level profiling result.
type Report struct {
	URL        string       `json:"url"`
	Timestamp  time.Time    `json:"timestamp"`
	Pages      []PageReport `json:"pages,omitempty"`
	PageReport              // embedded for single-page backward compat
}

// PageReport holds metrics collected from a single page load.
type PageReport struct {
	URL               string              `json:"url"`
	TTFBMs            float64             `json:"ttfbMs"`
	DCLMs             float64             `json:"dclMs"`
	FullyLoadedMs     float64             `json:"fullyLoadedMs"`
	Islands           []IslandMetric      `json:"islands,omitempty"`
	IslandHydrationMs float64             `json:"islandHydrationMs"`
	JSHeapSizeMB      float64             `json:"jsHeapSizeMb"`
	Scene             *SceneMetric        `json:"scene,omitempty"`
	Interactions      []InteractionMetric `json:"interactions,omitempty"`

	// Network resource waterfall (top-N slowest or largest)
	Resources             []ResourceEntry `json:"resources,omitempty"`
	TotalBytesTransferred int64           `json:"totalBytesTransferred"`
	BlockingResourceMs    float64         `json:"blockingResourceMs"`

	// Core Web Vitals
	LargestContentfulPaintMs float64 `json:"lcpMs"`
	CumulativeLayoutShift    float64 `json:"cls"`
	FirstInputDelayMs        float64 `json:"fidMs"`

	// Main-thread blocking
	LongTasks           []LongTaskMetric `json:"longTasks,omitempty"`
	LongTaskCount       int              `json:"longTaskCount"`
	LongTaskTotalMs     float64          `json:"longTaskTotalMs"`
	TotalBlockingTimeMs float64          `json:"totalBlockingTimeMs"`

	// GoSX runtime throughput
	SignalWrites    int `json:"signalWrites"`
	SignalReads     int `json:"signalReads"`
	HubMessageCount int `json:"hubMessageCount"`
	HubMessageBytes int `json:"hubMessageBytes"`
	HubSendCount    int `json:"hubSendCount"`

	// WebGL context info (nil if no canvas detected)
	WebGL *WebGLInfo `json:"webgl,omitempty"`

	// Console and uncaught exceptions captured during the page load, in
	// arrival order. Only warnings, errors, asserts, and uncaught
	// exceptions are kept by default — see StartConsoleCaptureAll for
	// the noisy variant.
	ConsoleEntries []ConsoleEntry `json:"consoleEntries,omitempty"`

	// JS coverage per script (populated only when --coverage is set).
	// Sorted by unused bytes descending — biggest split opportunities first.
	Coverage []CoverageEntry `json:"coverage,omitempty"`
}

// LongTaskMetric represents one main-thread blocking task (> 50ms).
type LongTaskMetric struct {
	Name       string  `json:"name"`
	StartTime  float64 `json:"startTime"`
	DurationMs float64 `json:"durationMs"`
}

// WebGLInfo holds GPU context introspection results.
// Tier is "webgpu" | "webgl2" | "webgl1" | "none".
type WebGLInfo struct {
	Tier                         string   `json:"tier"`
	Version                      string   `json:"version"`
	ShadingLanguageVersion       string   `json:"shadingLanguageVersion,omitempty"`
	Vendor                       string   `json:"vendor"`
	Renderer                     string   `json:"renderer"`
	MaxTextureSize               int      `json:"maxTextureSize"`
	MaxCubeMapSize               int      `json:"maxCubeMapSize,omitempty"`
	MaxRenderbufferSize          int      `json:"maxRenderbufferSize,omitempty"`
	MaxVertexAttribs             int      `json:"maxVertexAttribs,omitempty"`
	MaxCombinedTextureImageUnits int      `json:"maxCombinedTextureImageUnits,omitempty"`
	Antialiasing                 bool     `json:"antialiasing,omitempty"`
	PreserveDrawingBuffer        bool     `json:"preserveDrawingBuffer,omitempty"`
	Extensions                   []string `json:"extensions,omitempty"`
	Caps                         *GPUCaps `json:"caps,omitempty"`
}

// IsSoftwareRendered reports whether the WebGL context is backed by a
// software rasterizer (SwiftShader, Mesa llvmpipe, Mesa softpipe, Apple
// software renderer, etc.) rather than real GPU hardware.
//
// Perf numbers from software rendering — especially Scene3D frame budgets,
// main-thread blocking during shader compilation, and buffer upload times
// — do NOT reflect what real users on hardware GPUs will experience.
// Callers should surface a clear warning when this returns true so that
// automated perf gates or manual profiling sessions don't chase ghost
// regressions that are entirely artifacts of software emulation.
func (w *WebGLInfo) IsSoftwareRendered() bool {
	if w == nil {
		return false
	}
	hay := strings.ToLower(w.Renderer + " " + w.Vendor)
	// Known software renderer substrings.
	markers := []string{
		"swiftshader",
		"llvmpipe",
		"softpipe",
		"software rasterizer",
		"apple software renderer",
		"microsoft basic render driver",
		"google swiftshader",
	}
	for _, m := range markers {
		if strings.Contains(hay, m) {
			return true
		}
	}
	return false
}

// SoftwareRendererName returns a short human-readable name for the detected
// software renderer, or "" when none is detected.
func (w *WebGLInfo) SoftwareRendererName() string {
	if w == nil {
		return ""
	}
	hay := strings.ToLower(w.Renderer + " " + w.Vendor)
	switch {
	case strings.Contains(hay, "swiftshader"):
		return "SwiftShader"
	case strings.Contains(hay, "llvmpipe"):
		return "Mesa llvmpipe"
	case strings.Contains(hay, "softpipe"):
		return "Mesa softpipe"
	case strings.Contains(hay, "apple software"):
		return "Apple Software Renderer"
	case strings.Contains(hay, "microsoft basic"):
		return "Microsoft Basic Render Driver"
	case strings.Contains(hay, "software rasterizer"):
		return "Generic Software Rasterizer"
	}
	return ""
}

// GPUCaps reports browser-level GPU tier availability independent of what
// any particular canvas ended up selecting.
type GPUCaps struct {
	WebGPUAvailable bool `json:"webgpuAvailable"`
	WebGL2Available bool `json:"webgl2Available"`
	WebGL1Available bool `json:"webgl1Available"`
}

// IslandMetric holds per-island hydration timing.
type IslandMetric struct {
	ID          string  `json:"id"`
	HydrationMs float64 `json:"hydrationMs"`
}

// SceneMetric holds Scene3D rendering metrics.
type SceneMetric struct {
	FirstFrameMs  float64    `json:"firstFrameMs"`
	FrameStats    FrameStats `json:"frameStats"`
	DroppedFrames int        `json:"droppedFrames"`
	FrameCount    int        `json:"frameCount"`
}

// FrameStats holds percentile statistics for frame durations.
type FrameStats struct {
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
	Max   float64 `json:"max"`
	Mean  float64 `json:"mean"`
	Count int     `json:"count"`
}

// InteractionMetric holds a single dispatch interaction measurement.
type InteractionMetric struct {
	Action     string  `json:"action"`
	DispatchMs float64 `json:"dispatchMs"`
	PatchCount int     `json:"patchCount"`
}

// CollectPageReport queries all performance data from the driver and assembles
// a PageReport for the given URL.
func CollectPageReport(d *Driver, url string) (*PageReport, error) {
	pr := &PageReport{URL: url}

	// Navigation timing
	nav, err := QueryNavigationTiming(d)
	if err != nil {
		return nil, err
	}
	pr.TTFBMs = nav.TTFB
	pr.DCLMs = nav.DOMContentLoaded
	pr.FullyLoadedMs = nav.FullyLoaded

	// Heap size
	heap, err := QueryHeapSize(d)
	if err != nil {
		return nil, err
	}
	pr.JSHeapSizeMB = heap

	// Hydration log
	hydLog, err := QueryHydrationLog(d)
	if err != nil {
		return nil, err
	}
	var totalHydration float64
	for _, h := range hydLog {
		pr.Islands = append(pr.Islands, IslandMetric{
			ID:          h.ID,
			HydrationMs: h.Ms,
		})
		totalHydration += h.Ms
	}
	pr.IslandHydrationMs = totalHydration

	// Scene3D frames
	frames, err := QuerySceneFrames(d)
	if err != nil {
		return nil, err
	}
	if len(frames) > 0 {
		durations := make([]float64, len(frames))
		for i, f := range frames {
			durations[i] = f.Duration
		}
		stats := ComputeFrameStats(durations)

		// First frame timing
		var firstFrameMs float64
		if len(frames) > 0 {
			firstFrameMs = frames[0].Duration
		}

		// Dropped frames: those exceeding 16.67ms budget (60fps)
		var dropped int
		for _, dur := range durations {
			if dur > 16.67 {
				dropped++
			}
		}

		rs, err := QueryRuntimeState(d)
		if err != nil {
			return nil, err
		}

		pr.Scene = &SceneMetric{
			FirstFrameMs:  firstFrameMs,
			FrameStats:    stats,
			DroppedFrames: dropped,
			FrameCount:    rs.FrameCount,
		}
	}

	// Dispatch log → interactions
	dispLog, err := QueryDispatchLog(d)
	if err != nil {
		return nil, err
	}
	for _, dl := range dispLog {
		pr.Interactions = append(pr.Interactions, InteractionMetric{
			Action:     dl.Island + ":" + dl.Handler,
			DispatchMs: dl.Ms,
			PatchCount: dl.Patches,
		})
	}

	// Core Web Vitals + extended runtime counters
	var vitals struct {
		LCP             float64 `json:"lcp"`
		CLS             float64 `json:"cls"`
		FID             float64 `json:"fid"`
		SignalWrites    int     `json:"signalWrites"`
		SignalReads     int     `json:"signalReads"`
		HubMessageCount int     `json:"hubMessageCount"`
		HubMessageBytes int     `json:"hubMessageBytes"`
		HubSendCount    int     `json:"hubSendCount"`
	}
	_ = d.Evaluate(`(function() {
		var p = window.__gosx_perf || {};
		return {
			lcp: p.largestContentfulPaint || 0,
			cls: p.cumulativeLayoutShift || 0,
			fid: p.firstInputDelay || 0,
			signalWrites: p.signalWrites || 0,
			signalReads: p.signalReads || 0,
			hubMessageCount: p.hubMessageCount || 0,
			hubMessageBytes: p.hubMessageBytes || 0,
			hubSendCount: p.hubSendCount || 0
		};
	})()`, &vitals)
	pr.LargestContentfulPaintMs = vitals.LCP
	pr.CumulativeLayoutShift = vitals.CLS
	pr.FirstInputDelayMs = vitals.FID
	pr.SignalWrites = vitals.SignalWrites
	pr.SignalReads = vitals.SignalReads
	pr.HubMessageCount = vitals.HubMessageCount
	pr.HubMessageBytes = vitals.HubMessageBytes
	pr.HubSendCount = vitals.HubSendCount

	// Long tasks
	var longTasks []struct {
		Name      string  `json:"name"`
		StartTime float64 `json:"startTime"`
		Duration  float64 `json:"duration"`
	}
	_ = d.Evaluate(`(window.__gosx_perf && window.__gosx_perf.longTasks) || []`, &longTasks)
	for _, lt := range longTasks {
		pr.LongTasks = append(pr.LongTasks, LongTaskMetric{
			Name:       lt.Name,
			StartTime:  lt.StartTime,
			DurationMs: lt.Duration,
		})
		pr.LongTaskTotalMs += lt.Duration
		// Total Blocking Time = sum of (duration - 50ms) for tasks > 50ms
		if lt.Duration > 50 {
			pr.TotalBlockingTimeMs += lt.Duration - 50
		}
	}
	pr.LongTaskCount = len(longTasks)

	// GPU context info (tier + caps). Captured even when no canvas is
	// present so the report can show browser capabilities.
	var webgl WebGLInfo
	err = d.Evaluate(`(typeof window.__gosx_perf_webgl_info === "function") ? window.__gosx_perf_webgl_info() : null`, &webgl)
	if err == nil && webgl.Tier != "" {
		pr.WebGL = &webgl
	}

	// Resource waterfall
	resources, err := QueryResourceWaterfall(d)
	if err == nil {
		pr.Resources = resources
		for _, r := range resources {
			pr.TotalBytesTransferred += int64(r.TransferSize)
			// Blocking script/style resources that delay rendering
			if r.InitiatorType == "script" || r.InitiatorType == "link" {
				if r.ResponseEnd > pr.BlockingResourceMs {
					pr.BlockingResourceMs = r.ResponseEnd
				}
			}
		}
	}

	return pr, nil
}

// ComputeFrameStats sorts durations and computes percentile statistics.
// Empty input returns a zero FrameStats.
func ComputeFrameStats(durations []float64) FrameStats {
	n := len(durations)
	if n == 0 {
		return FrameStats{}
	}

	sorted := make([]float64, n)
	copy(sorted, durations)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}

	return FrameStats{
		P50:   percentile(sorted, 0.50),
		P95:   percentile(sorted, 0.95),
		P99:   percentile(sorted, 0.99),
		Max:   sorted[n-1],
		Mean:  sum / float64(n),
		Count: n,
	}
}

// percentile computes the p-th percentile using linear interpolation.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 1 {
		return sorted[0]
	}
	// Use the "exclusive" interpolation method (same as numpy default).
	// rank is 0-indexed fractional position
	rank := p * float64(n-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	frac := rank - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}
