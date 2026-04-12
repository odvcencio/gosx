package perf

import (
	"sort"
	"time"
)

// Report is the top-level profiling result.
type Report struct {
	URL       string       `json:"url"`
	Timestamp time.Time    `json:"timestamp"`
	Pages     []PageReport `json:"pages,omitempty"`
	PageReport             // embedded for single-page backward compat
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
