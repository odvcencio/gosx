package perf

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatTable returns a human-readable table representation of the report.
// Sections with no data are omitted.
func FormatTable(r *Report) string {
	var b strings.Builder

	// Use the embedded PageReport for single-page reports, or first page
	p := &r.PageReport
	url := r.URL

	b.WriteString(fmt.Sprintf("gosx perf — %s\n", url))

	// Page Lifecycle
	b.WriteString("\n  Page Lifecycle\n")
	b.WriteString(fmt.Sprintf("    %-24s%.1fms\n", "TTFB", p.TTFBMs))
	b.WriteString(fmt.Sprintf("    %-24s%.1fms\n", "DOMContentLoaded", p.DCLMs))

	if len(p.Islands) > 0 {
		b.WriteString(fmt.Sprintf("    %-24s%d (hydrated in %.1fms total)\n",
			"Islands", len(p.Islands), p.IslandHydrationMs))
		for _, isl := range p.Islands {
			b.WriteString(fmt.Sprintf("      %-22s%.1fms\n", isl.ID, isl.HydrationMs))
		}
	}

	if p.JSHeapSizeMB > 0 {
		b.WriteString(fmt.Sprintf("    %-24s%.1f MB\n", "JS Heap", p.JSHeapSizeMB))
	}

	// Scene3D
	if p.Scene != nil {
		b.WriteString("\n  Scene3D\n")
		b.WriteString(fmt.Sprintf("    %-24s%.1fms\n", "First frame", p.Scene.FirstFrameMs))

		fs := p.Scene.FrameStats
		if fs.Count > 0 {
			b.WriteString(fmt.Sprintf("    Frame budget (%d frames)\n", fs.Count))
			b.WriteString(fmt.Sprintf("      p50    %.1fms    p95    %.1fms    p99    %.1fms    max    %.1fms\n",
				fs.P50, fs.P95, fs.P99, fs.Max))
		}

		b.WriteString(fmt.Sprintf("    %-24s%d\n", "Frame count", p.Scene.FrameCount))
	}

	// Interactions
	if len(p.Interactions) > 0 {
		b.WriteString("\n  Interactions\n")
		for _, ix := range p.Interactions {
			b.WriteString(fmt.Sprintf("    %s\n", ix.Action))
			b.WriteString(fmt.Sprintf("      dispatch    %.1fms    patches    %d\n",
				ix.DispatchMs, ix.PatchCount))
		}
	}

	return b.String()
}

// FormatJSON returns the report as indented JSON.
func FormatJSON(r *Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
