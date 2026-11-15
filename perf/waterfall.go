package perf

import (
	"context"
	"fmt"
	"time"
)

// ResourceEntry represents a resource loading event from the Performance API.
type ResourceEntry struct {
	Name          string  `json:"name"`
	InitiatorType string  `json:"initiatorType"`
	TransferSize  float64 `json:"transferSize"`
	Duration      float64 `json:"duration"`
	StartTime     float64 `json:"startTime"`
	ResponseEnd   float64 `json:"responseEnd"`
}

// QueryResourceWaterfall returns all resource entries sorted by start time.
func QueryResourceWaterfall(d *Driver) ([]ResourceEntry, error) {
	var entries []ResourceEntry
	err := d.Evaluate(`
		performance.getEntriesByType("resource").map(e => ({
			name: e.name,
			initiatorType: e.initiatorType,
			transferSize: e.transferSize,
			duration: e.duration,
			startTime: e.startTime,
			responseEnd: e.responseEnd
		})).sort((a, b) => a.startTime - b.startTime)
	`, &entries)
	return entries, err
}

// FormatWaterfall prints a human-readable resource waterfall.
func FormatWaterfall(entries []ResourceEntry) string {
	var out string
	out += fmt.Sprintf("  %-12s %-8s %8s %8s  %s\n", "type", "size", "start", "dur", "url")
	out += fmt.Sprintf("  %-12s %-8s %8s %8s  %s\n", "----", "----", "-----", "---", "---")
	for _, e := range entries {
		name := e.Name
		if len(name) > 60 {
			name = "..." + name[len(name)-57:]
		}
		size := fmt.Sprintf("%.0fKB", e.TransferSize/1024)
		if e.TransferSize == 0 {
			size = "cached"
		}
		out += fmt.Sprintf("  %-12s %-8s %7.0fms %7.0fms  %s\n",
			e.InitiatorType, size, e.StartTime, e.Duration, name)
	}
	return out
}

const (
	// networkQuietWindow is how long the resource list must stay unchanged
	// before a budget measurement reads it.
	networkQuietWindow = 750 * time.Millisecond
	// networkQuietMax bounds the wait on a page that never goes quiet.
	networkQuietMax  = 6 * time.Second
	networkQuietPoll = 150 * time.Millisecond
)

// waitForNetworkQuiet polls the number of finished resources until it stays
// unchanged for quiet, or max passes. The byte budget sums transfer sizes at
// one moment, and a resource that finishes after it (a lazy navigation script
// of 111 KB on /demos/water) made the total differ by more than 100 KB between
// runs of the same code. It returns how long it waited and whether the page
// went quiet.
func waitForNetworkQuiet(ctx context.Context, count func() (int, error), quiet, max, poll time.Duration) (time.Duration, bool, error) {
	start := time.Now()
	last, err := count()
	if err != nil {
		return 0, false, err
	}
	stableSince := time.Now()
	for time.Since(start) < max {
		select {
		case <-ctx.Done():
			return time.Since(start), false, ctx.Err()
		case <-time.After(poll):
		}
		n, err := count()
		if err != nil {
			return time.Since(start), false, err
		}
		if n != last {
			last, stableSince = n, time.Now()
			continue
		}
		if time.Since(stableSince) >= quiet {
			return time.Since(start), true, nil
		}
	}
	return time.Since(start), false, nil
}
