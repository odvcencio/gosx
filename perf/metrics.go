package perf

import (
	"fmt"
	"math"
	"sort"
	"strconv"
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
	CoverageCaptured bool            `json:"coverageCaptured,omitempty"`
	Coverage         []CoverageEntry `json:"coverage,omitempty"`
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
	// Legacy CPU render/submit fields retained for JSON and budget
	// compatibility. They are not presentation or GPU timings.
	FirstFrameMs  float64    `json:"firstFrameMs"`
	FrameStats    FrameStats `json:"frameStats"`
	DroppedFrames int        `json:"droppedFrames"`
	FrameCount    int        `json:"frameCount"`

	FirstRenderStartMs float64                    `json:"firstRenderStartMs,omitempty"`
	CPURenderSubmit    *TelemetrySeries           `json:"cpuRenderSubmit,omitempty"`
	CPUTimings         map[string]TelemetrySeries `json:"cpuTimings,omitempty"`
	Presentation       *PresentationMetric        `json:"presentation,omitempty"`
	GPU                *SceneGPUTelemetry         `json:"gpu,omitempty"`
	Mounts             []SceneMountMetric         `json:"mounts,omitempty"`
	Counters           map[string]float64         `json:"counters,omitempty"`
	Geometry           map[string]float64         `json:"geometry,omitempty"`
	Pipeline           map[string]float64         `json:"pipeline,omitempty"`
	Status             map[string]string          `json:"status,omitempty"`
	Events             []SceneTelemetryEvent      `json:"events,omitempty"`
	Unavailable        map[string]string          `json:"unavailable,omitempty"`
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

// TelemetrySeries separates a timing distribution into all, cold, and warm
// samples. Empty phase stats are kept as zero-count values rather than being
// inferred from another source.
type TelemetrySeries struct {
	Source string     `json:"source"`
	Unit   string     `json:"unit"`
	Stats  FrameStats `json:"stats"`
	Cold   FrameStats `json:"cold"`
	Warm   FrameStats `json:"warm"`
}

// PresentationMetric describes requestAnimationFrame display-opportunity
// cadence. EstimatedMissedVsyncs is not a compositor dropped-frame count.
type PresentationMetric struct {
	TelemetrySeries
	EstimatedRefreshIntervalMs float64        `json:"estimatedRefreshIntervalMs"`
	EstimatedMissedVsyncs      int            `json:"estimatedMissedVsyncs"`
	HitchIntervals             int            `json:"hitchIntervals"`
	HitchClusters              []HitchCluster `json:"hitchClusters,omitempty"`
}

// HitchCluster is a consecutive run of rAF intervals that missed one or more
// estimated refresh opportunities.
type HitchCluster struct {
	StartTime             float64 `json:"startTime"`
	IntervalCount         int     `json:"intervalCount"`
	EstimatedMissedVsyncs int     `json:"estimatedMissedVsyncs"`
	DurationMs            float64 `json:"durationMs"`
	MaxIntervalMs         float64 `json:"maxIntervalMs"`
}

// SceneGPUTelemetry contains renderer-published timestamp-query timings.
type SceneGPUTelemetry struct {
	Total  *TelemetrySeries           `json:"total,omitempty"`
	Passes map[string]TelemetrySeries `json:"passes,omitempty"`
	Status string                     `json:"status,omitempty"`
}

// SceneMountMetric captures the runtime profile actually selected for one
// Scene3D mount, plus the original stable attributes for debugging.
type SceneMountMetric struct {
	Index               int                  `json:"index"`
	ID                  string               `json:"id,omitempty"`
	Backend             string               `json:"backend,omitempty"`
	Renderer            string               `json:"renderer,omitempty"`
	Fallback            string               `json:"fallback,omitempty"`
	Profile             string               `json:"profile,omitempty"`
	RenderGPU           string               `json:"renderGpu,omitempty"`
	PixelRatio          float64              `json:"pixelRatio,omitempty"`
	DevicePixelRatio    float64              `json:"devicePixelRatio,omitempty"`
	EffectivePixelRatio float64              `json:"effectivePixelRatio,omitempty"`
	Canvas              *SceneCanvasSnapshot `json:"canvas,omitempty"`
	Attributes          map[string]string    `json:"attributes,omitempty"`
}

// InteractionMetric holds a single dispatch interaction measurement.
type InteractionMetric struct {
	Action     string  `json:"action"`
	DispatchMs float64 `json:"dispatchMs"`
	PatchCount int     `json:"patchCount"`
}

type pageReportQueryRunner func(phase string, required bool, query func() error) error

type pageReportQueries struct {
	navigationTiming    func(*Driver) (NavigationTiming, error)
	heapSize            func(*Driver) (float64, error)
	hydrationLog        func(*Driver) ([]HydrationEntry, error)
	performanceMeasures func(*Driver, string) ([]PerfEntry, error)
	sceneTelemetry      func(*Driver) (SceneTelemetrySnapshot, error)
	runtimeState        func(*Driver) (RuntimeState, error)
	dispatchLog         func(*Driver) ([]DispatchEntry, error)
	evaluate            func(*Driver, string, interface{}) error
	resourceWaterfall   func(*Driver) ([]ResourceEntry, error)
}

func defaultPageReportQueries() pageReportQueries {
	return pageReportQueries{
		navigationTiming:    QueryNavigationTiming,
		heapSize:            QueryHeapSize,
		hydrationLog:        QueryHydrationLog,
		performanceMeasures: QueryPerformanceMeasures,
		sceneTelemetry:      QuerySceneTelemetry,
		runtimeState:        QueryRuntimeState,
		dispatchLog:         QueryDispatchLog,
		evaluate:            (*Driver).Evaluate,
		resourceWaterfall:   QueryResourceWaterfall,
	}
}

func runPageReportQuery(runner pageReportQueryRunner, phase string, required bool, query func() error) error {
	var err error
	if runner != nil {
		err = runner(phase, required, query)
	} else {
		err = query()
	}
	if err != nil {
		return fmt.Errorf("%s: %w", phase, err)
	}
	return nil
}

// CollectPageReport queries all performance data from the driver and assembles
// a PageReport for the given URL.
func CollectPageReport(d *Driver, url string) (*PageReport, error) {
	return collectPageReport(d, url, defaultPageReportQueries(), nil)
}

func collectPageReportWithQueryRunner(d *Driver, url string, runner pageReportQueryRunner) (*PageReport, error) {
	return collectPageReport(d, url, defaultPageReportQueries(), runner)
}

func collectPageReport(d *Driver, url string, queries pageReportQueries, runner pageReportQueryRunner) (*PageReport, error) {
	pr := &PageReport{URL: url}

	// Navigation timing
	var nav NavigationTiming
	if err := runPageReportQuery(runner, "collect/navigation-timing", true, func() error {
		var err error
		nav, err = queries.navigationTiming(d)
		return err
	}); err != nil {
		return nil, err
	}
	pr.TTFBMs = nav.TTFB
	pr.DCLMs = nav.DOMContentLoaded
	pr.FullyLoadedMs = nav.FullyLoaded

	// Heap size
	var heap float64
	if err := runPageReportQuery(runner, "collect/heap-size", true, func() error {
		var err error
		heap, err = queries.heapSize(d)
		return err
	}); err != nil {
		return nil, err
	}
	pr.JSHeapSizeMB = heap

	// Hydration log
	var hydLog []HydrationEntry
	if err := runPageReportQuery(runner, "collect/hydration-log", true, func() error {
		var err error
		hydLog, err = queries.hydrationLog(d)
		return err
	}); err != nil {
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

	// Scene3D CPU measures and renderer-published telemetry. scene3d-render
	// measures CPU command planning/submission; rAF and GPU timings remain
	// separate sources throughout the report.
	var sceneMeasures []PerfEntry
	if err := runPageReportQuery(runner, "collect/scene-measures", true, func() error {
		var err error
		sceneMeasures, err = queries.performanceMeasures(d, "scene3d-")
		return err
	}); err != nil {
		return nil, err
	}
	var snapshot SceneTelemetrySnapshot
	if err := runPageReportQuery(runner, "collect/scene-telemetry", true, func() error {
		var err error
		snapshot, err = queries.sceneTelemetry(d)
		return err
	}); err != nil {
		return nil, err
	}
	if snapshot.Available || len(sceneMeasures) > 0 {
		var rs RuntimeState
		if err := runPageReportQuery(runner, "collect/runtime-state", true, func() error {
			var err error
			rs, err = queries.runtimeState(d)
			return err
		}); err != nil {
			return nil, err
		}
		pr.Scene = BuildSceneMetric(sceneMeasures, snapshot, rs.FrameCount)
	}

	// Dispatch log → interactions
	var dispLog []DispatchEntry
	if err := runPageReportQuery(runner, "collect/dispatch-log", true, func() error {
		var err error
		dispLog, err = queries.dispatchLog(d)
		return err
	}); err != nil {
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
	_ = runPageReportQuery(runner, "collect/vitals", false, func() error {
		return queries.evaluate(d, `(function() {
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
	})
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
	_ = runPageReportQuery(runner, "collect/long-tasks", false, func() error {
		return queries.evaluate(d, `(window.__gosx_perf && window.__gosx_perf.longTasks) || []`, &longTasks)
	})
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
	webglErr := runPageReportQuery(runner, "collect/webgl-info", false, func() error {
		return queries.evaluate(d, `(typeof window.__gosx_perf_webgl_info === "function") ? window.__gosx_perf_webgl_info() : null`, &webgl)
	})
	if webglErr == nil && webgl.Tier != "" {
		pr.WebGL = &webgl
	}

	// Resource waterfall
	var resources []ResourceEntry
	resourcesErr := runPageReportQuery(runner, "collect/resource-waterfall", false, func() error {
		var err error
		resources, err = queries.resourceWaterfall(d)
		return err
	})
	if resourcesErr == nil {
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

// BuildSceneMetric aggregates strictly source-labelled Scene3D telemetry.
// It is exported so offline tooling and tests can build the same report shape
// without a live browser.
func BuildSceneMetric(measures []PerfEntry, snapshot SceneTelemetrySnapshot, frameCount int) *SceneMetric {
	scene := &SceneMetric{
		FrameCount:  frameCount,
		CPUTimings:  map[string]TelemetrySeries{},
		Counters:    map[string]float64{},
		Geometry:    map[string]float64{},
		Pipeline:    map[string]float64{},
		Status:      map[string]string{},
		Unavailable: map[string]string{},
		Events:      append([]SceneTelemetryEvent(nil), snapshot.TelemetryEvents...),
	}

	measureGroups := map[string][]SceneTelemetrySample{}
	var renderEntries []PerfEntry
	for _, entry := range measures {
		phase := telemetryPhase(entry.StartTime, snapshot.WarmAt)
		sample := SceneTelemetrySample{
			Name:         entry.Name,
			NumericValue: entry.Duration,
			StartTime:    entry.StartTime,
			Phase:        phase,
		}
		measureGroups[entry.Name] = append(measureGroups[entry.Name], sample)
		if entry.Name == "scene3d-render" {
			renderEntries = append(renderEntries, entry)
		}
	}
	if len(renderEntries) > 0 {
		renderSamples := measureGroups["scene3d-render"]
		series := buildTelemetrySeries("performance.measure:scene3d-render", "ms", renderSamples)
		scene.CPURenderSubmit = &series
		scene.FrameStats = series.Stats
		scene.FirstFrameMs = renderEntries[0].Duration
		scene.FirstRenderStartMs = renderEntries[0].StartTime
	}
	for name, samples := range measureGroups {
		if name == "scene3d-render" {
			continue
		}
		key := strings.TrimPrefix(name, "scene3d-")
		scene.CPUTimings[key] = buildTelemetrySeries("performance.measure:"+name, "ms", samples)
	}

	attributeGroups := map[string][]SceneTelemetrySample{}
	for _, sample := range snapshot.AttributeSamples {
		if validMetricNumber(sample.NumericValue) {
			attributeGroups[sample.Name] = append(attributeGroups[sample.Name], sample)
		}
	}

	// Ensure a latest mount value remains reportable even when the Mutation
	// Observer attached after a backend's one-time initialization write.
	for _, mount := range snapshot.Mounts {
		for name, raw := range mount.Attributes {
			value, ok := parseMetricNumber(raw)
			if !ok || !isTimingAttribute(name) || len(attributeGroups[name]) > 0 {
				continue
			}
			attributeGroups[name] = append(attributeGroups[name], SceneTelemetrySample{
				Mount:        mount.Index,
				Name:         name,
				Value:        raw,
				NumericValue: value,
				StartTime:    snapshot.CapturedAt,
				Phase:        telemetryPhase(snapshot.CapturedAt, snapshot.WarmAt),
			})
		}
	}

	gpu := &SceneGPUTelemetry{Passes: map[string]TelemetrySeries{}}
	for name, samples := range attributeGroups {
		key := sceneAttributeKey(name)
		switch {
		case strings.Contains(name, "-gpu-pass-") && strings.HasSuffix(name, "-ms"):
			gpu.Passes[gpuPassKey(name)] = buildTelemetrySeries("mount-attribute:"+name, "ms", samples)
		case strings.HasSuffix(name, "-gpu-ms"):
			series := buildTelemetrySeries("mount-attribute:"+name, "ms", samples)
			gpu.Total = &series
		case strings.HasSuffix(name, "-cpu-ms"):
			scene.CPUTimings[key] = buildTelemetrySeries("mount-attribute:"+name, "ms", samples)
		}
	}

	for _, mount := range snapshot.Mounts {
		metric := buildSceneMountMetric(mount, snapshot.DevicePixelRatio)
		scene.Mounts = append(scene.Mounts, metric)
		prefix := ""
		if len(snapshot.Mounts) > 1 {
			prefix = "mount" + strconv.Itoa(mount.Index) + "."
		}
		for name, raw := range mount.Attributes {
			key := prefix + sceneAttributeKey(name)
			if value, ok := parseMetricNumber(raw); ok && isCounterAttribute(name) {
				scene.Counters[key] = value
				if strings.Contains(name, "-retained-") {
					scene.Geometry[key] = value
				}
				if containsAny(name, "pipeline", "shader", "compile", "warmup", "cache") {
					scene.Pipeline[key] = value
				}
			}
			if isStatusAttribute(name) && raw != "" {
				scene.Status[key] = raw
			}
			if strings.HasSuffix(name, "-gpu-timing") {
				gpu.Status = raw
			}
		}
	}

	if gpu.Total != nil || len(gpu.Passes) > 0 || gpu.Status != "" {
		scene.GPU = gpu
	}

	var presentationSamples []AnimationFrameSample
	startAt := snapshot.SceneStartedAt
	if startAt == 0 && len(renderEntries) > 0 {
		startAt = renderEntries[0].StartTime
	}
	for _, sample := range snapshot.PresentedFrameIntervals {
		if startAt > 0 && sample.StartTime < startAt {
			continue
		}
		if validMetricNumber(sample.Duration) && sample.Duration > 0 {
			presentationSamples = append(presentationSamples, sample)
		}
	}
	if len(presentationSamples) > 0 {
		presentation := ComputePresentationMetric(presentationSamples)
		scene.Presentation = &presentation
		// Deprecated compatibility alias, now derived only from rAF cadence.
		scene.DroppedFrames = presentation.EstimatedMissedVsyncs
	}

	if scene.CPURenderSubmit == nil {
		scene.Unavailable["cpuRenderSubmit"] = "no scene3d-render performance measures were emitted"
	}
	if scene.Presentation == nil {
		reason := "no visible-tab requestAnimationFrame intervals were observed after Scene3D started"
		if !snapshot.RAFAvailable {
			reason = "requestAnimationFrame is unavailable"
		}
		scene.Unavailable["presentation"] = reason
	}
	if scene.GPU == nil || (scene.GPU.Total == nil && len(scene.GPU.Passes) == 0) {
		reason := "renderer did not publish GPU timestamp-query values"
		if gpu.Status != "" {
			reason = gpu.Status
		}
		scene.Unavailable["gpu"] = reason
	}
	if _, ok := scene.CPUTimings["planner"]; !ok {
		if _, ok := scene.CPUTimings["planner-cpu-ms"]; !ok {
			scene.Unavailable["planner"] = "renderer did not publish planner CPU timing"
		}
	}

	if len(scene.CPUTimings) == 0 {
		scene.CPUTimings = nil
	}
	if len(scene.Counters) == 0 {
		scene.Counters = nil
	}
	if len(scene.Geometry) == 0 {
		scene.Geometry = nil
	}
	if len(scene.Pipeline) == 0 {
		scene.Pipeline = nil
	}
	if len(scene.Status) == 0 {
		scene.Status = nil
	}
	return scene
}

// ComputePresentationMetric computes cadence percentiles and estimated
// missed-vsync clusters from rAF intervals. The refresh interval is estimated
// from the tenth percentile to resist occasional startup hitches.
func ComputePresentationMetric(samples []AnimationFrameSample) PresentationMetric {
	converted := make([]SceneTelemetrySample, 0, len(samples))
	durations := make([]float64, 0, len(samples))
	for _, sample := range samples {
		if !validMetricNumber(sample.Duration) || sample.Duration <= 0 {
			continue
		}
		converted = append(converted, SceneTelemetrySample{
			NumericValue: sample.Duration,
			StartTime:    sample.StartTime,
			Phase:        sample.Phase,
		})
		durations = append(durations, sample.Duration)
	}
	result := PresentationMetric{
		TelemetrySeries: buildTelemetrySeries(
			"requestAnimationFrame timestamp interval (display opportunity; not compositor proof)",
			"ms",
			converted,
		),
	}
	if len(durations) == 0 {
		return result
	}
	sort.Float64s(durations)
	baseline := percentile(durations, 0.10)
	if baseline <= 0 {
		return result
	}
	result.EstimatedRefreshIntervalMs = baseline
	hitchThreshold := math.Max(50, baseline*3)

	var active *HitchCluster
	for _, sample := range samples {
		ratio := sample.Duration / baseline
		missed := 0
		if ratio > 1.5 {
			missed = int(math.Round(ratio)) - 1
			if missed < 1 {
				missed = 1
			}
		}
		if sample.Duration >= hitchThreshold {
			result.HitchIntervals++
		}
		if missed == 0 {
			if active != nil {
				result.HitchClusters = append(result.HitchClusters, *active)
				active = nil
			}
			continue
		}
		result.EstimatedMissedVsyncs += missed
		if active == nil {
			active = &HitchCluster{StartTime: sample.StartTime}
		}
		active.IntervalCount++
		active.EstimatedMissedVsyncs += missed
		active.DurationMs += sample.Duration
		if sample.Duration > active.MaxIntervalMs {
			active.MaxIntervalMs = sample.Duration
		}
	}
	if active != nil {
		result.HitchClusters = append(result.HitchClusters, *active)
	}
	return result
}

func buildTelemetrySeries(source, unit string, samples []SceneTelemetrySample) TelemetrySeries {
	var all, cold, warm []float64
	for _, sample := range samples {
		if !validMetricNumber(sample.NumericValue) {
			continue
		}
		all = append(all, sample.NumericValue)
		if sample.Phase == "warm" {
			warm = append(warm, sample.NumericValue)
		} else {
			cold = append(cold, sample.NumericValue)
		}
	}
	return TelemetrySeries{
		Source: source,
		Unit:   unit,
		Stats:  ComputeFrameStats(all),
		Cold:   ComputeFrameStats(cold),
		Warm:   ComputeFrameStats(warm),
	}
}

func buildSceneMountMetric(mount SceneMountSnapshot, devicePixelRatio float64) SceneMountMetric {
	attrs := mount.Attributes
	metric := SceneMountMetric{
		Index:            mount.Index,
		ID:               mount.ID,
		Backend:          firstAttribute(attrs, "data-gosx-scene3d-backend", "data-gosx-scene3d-render-backend"),
		Renderer:         attrs["data-gosx-scene3d-renderer"],
		Fallback:         attrs["data-gosx-scene3d-renderer-fallback"],
		Profile:          firstAttribute(attrs, "data-gosx-scene3d-quality-active", "data-gosx-scene3d-quality-tier"),
		RenderGPU:        attrs["data-gosx-scene3d-render-gpu"],
		DevicePixelRatio: devicePixelRatio,
		Canvas:           mount.Canvas,
		Attributes:       attrs,
	}
	metric.PixelRatio, _ = parseMetricNumber(attrs["data-gosx-scene3d-pixel-ratio"])
	if mount.Canvas != nil && mount.Canvas.CSSWidth > 0 {
		metric.EffectivePixelRatio = mount.Canvas.Width / mount.Canvas.CSSWidth
	}
	return metric
}

func telemetryPhase(at, warmAt float64) string {
	if warmAt > 0 && at >= warmAt {
		return "warm"
	}
	return "cold"
}

func sceneAttributeKey(name string) string {
	return strings.TrimPrefix(name, "data-gosx-scene3d-")
}

func gpuPassKey(name string) string {
	parts := strings.SplitN(name, "-gpu-pass-", 2)
	if len(parts) != 2 {
		return sceneAttributeKey(name)
	}
	return strings.TrimSuffix(parts[1], "-ms")
}

func isTimingAttribute(name string) bool {
	return strings.HasSuffix(name, "-gpu-ms") ||
		(strings.Contains(name, "-gpu-pass-") && strings.HasSuffix(name, "-ms")) ||
		strings.HasSuffix(name, "-cpu-ms")
}

func isCounterAttribute(name string) bool {
	if isTimingAttribute(name) {
		return false
	}
	return containsAny(name,
		"pipeline", "shader", "compile", "warmup", "cache", "upload",
		"allocation", "retirement", "rebuild", "fallback", "draw",
		"dispatch", "pass", "bytes", "entries", "miss", "hit", "retained",
	)
}

func isStatusAttribute(name string) bool {
	return containsAny(name,
		"backend", "renderer", "fallback", "quality-active", "quality-tier",
		"quality-reason", "gpu-timing", "gpu-pass-timing", "pipeline-failed",
		"device-lost", "target-format", "sample-count",
	)
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func firstAttribute(attrs map[string]string, names ...string) string {
	for _, name := range names {
		if value := attrs[name]; value != "" {
			return value
		}
	}
	return ""
}

func parseMetricNumber(raw string) (float64, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	return value, err == nil && validMetricNumber(value)
}

func validMetricNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
