package perf

import "fmt"

// ResourceEntry represents a resource loading event from the Performance API.
type ResourceEntry struct {
	Name         string  `json:"name"`
	InitiatorType string `json:"initiatorType"`
	TransferSize float64 `json:"transferSize"`
	Duration     float64 `json:"duration"`
	StartTime    float64 `json:"startTime"`
	ResponseEnd  float64 `json:"responseEnd"`
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
