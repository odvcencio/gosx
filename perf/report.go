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

	// Core Web Vitals
	if p.LargestContentfulPaintMs > 0 || p.CumulativeLayoutShift > 0 || p.FirstInputDelayMs > 0 {
		b.WriteString("\n  Core Web Vitals\n")
		if p.LargestContentfulPaintMs > 0 {
			rating := " (good)"
			if p.LargestContentfulPaintMs > 4000 {
				rating = " (poor)"
			} else if p.LargestContentfulPaintMs > 2500 {
				rating = " (needs improvement)"
			}
			b.WriteString(fmt.Sprintf("    %-24s%.1fms%s\n", "LCP", p.LargestContentfulPaintMs, rating))
		}
		if p.CumulativeLayoutShift > 0 {
			rating := " (good)"
			if p.CumulativeLayoutShift > 0.25 {
				rating = " (poor)"
			} else if p.CumulativeLayoutShift > 0.1 {
				rating = " (needs improvement)"
			}
			b.WriteString(fmt.Sprintf("    %-24s%.3f%s\n", "CLS", p.CumulativeLayoutShift, rating))
		}
		if p.FirstInputDelayMs > 0 {
			rating := " (good)"
			if p.FirstInputDelayMs > 300 {
				rating = " (poor)"
			} else if p.FirstInputDelayMs > 100 {
				rating = " (needs improvement)"
			}
			b.WriteString(fmt.Sprintf("    %-24s%.1fms%s\n", "FID", p.FirstInputDelayMs, rating))
		}
	}

	// Long tasks — main thread blocking
	if p.LongTaskCount > 0 {
		b.WriteString("\n  Main-Thread Blocking\n")
		b.WriteString(fmt.Sprintf("    %-24s%d\n", "Long tasks", p.LongTaskCount))
		b.WriteString(fmt.Sprintf("    %-24s%.1fms\n", "Long task total", p.LongTaskTotalMs))
		b.WriteString(fmt.Sprintf("    %-24s%.1fms\n", "Total blocking time", p.TotalBlockingTimeMs))
		// Show top 5 longest
		show := len(p.LongTasks)
		if show > 5 {
			show = 5
		}
		for i := 0; i < show; i++ {
			lt := p.LongTasks[i]
			b.WriteString(fmt.Sprintf("      %-22s%.1fms at %.0fms\n", lt.Name, lt.DurationMs, lt.StartTime))
		}
		if len(p.LongTasks) > 5 {
			b.WriteString(fmt.Sprintf("      ... %d more\n", len(p.LongTasks)-5))
		}
	}

	// GoSX runtime throughput
	if p.SignalWrites > 0 || p.SignalReads > 0 || p.HubMessageCount > 0 {
		b.WriteString("\n  Runtime Throughput\n")
		if p.SignalWrites > 0 || p.SignalReads > 0 {
			b.WriteString(fmt.Sprintf("    %-24sw:%d  r:%d\n", "Signal ops", p.SignalWrites, p.SignalReads))
		}
		if p.HubMessageCount > 0 {
			b.WriteString(fmt.Sprintf("    %-24srecv:%d (%dB)  send:%d\n",
				"Hub messages", p.HubMessageCount, p.HubMessageBytes, p.HubSendCount))
		}
	}

	// Software-GPU warning banner — surface this above all GPU-dependent
	// sections so users immediately understand that Scene3D timings and
	// main-thread blocking numbers reflect software emulation, not the
	// real user experience on hardware GPUs.
	if p.WebGL != nil && p.WebGL.IsSoftwareRendered() {
		name := p.WebGL.SoftwareRendererName()
		if name == "" {
			name = "a software rasterizer"
		}
		b.WriteString("\n  ⚠  Software GPU detected (" + name + ")\n")
		b.WriteString("     Scene3D frame timings, shader-compile blocking, and main-thread\n")
		b.WriteString("     long tasks below are software-emulated and do NOT reflect what real\n")
		b.WriteString("     users on hardware GPUs experience. Run this profile against a browser\n")
		b.WriteString("     with a real GPU for accurate Scene3D numbers.\n")
	}

	// GPU context info — shows the tier actually in use and flags
	// whether the engine is running on the best available tier.
	if p.WebGL != nil {
		b.WriteString("\n  GPU Context\n")
		tierLabel := p.WebGL.Tier
		if p.WebGL.Caps != nil {
			// Annotate the tier with whether it's the best available.
			best := ""
			if p.WebGL.Caps.WebGPUAvailable {
				best = "webgpu"
			} else if p.WebGL.Caps.WebGL2Available {
				best = "webgl2"
			} else if p.WebGL.Caps.WebGL1Available {
				best = "webgl1"
			}
			if best != "" && best != tierLabel {
				tierLabel = tierLabel + " (best available: " + best + ")"
			} else if best != "" {
				tierLabel = tierLabel + " ✓"
			}
		}
		b.WriteString(fmt.Sprintf("    %-24s%s\n", "Tier", tierLabel))
		if p.WebGL.Version != "" {
			b.WriteString(fmt.Sprintf("    %-24s%s\n", "Version", p.WebGL.Version))
		}
		if p.WebGL.Vendor != "" {
			b.WriteString(fmt.Sprintf("    %-24s%s\n", "Vendor", p.WebGL.Vendor))
		}
		if p.WebGL.Renderer != "" {
			b.WriteString(fmt.Sprintf("    %-24s%s\n", "Renderer", p.WebGL.Renderer))
		}
		if p.WebGL.MaxTextureSize > 0 {
			b.WriteString(fmt.Sprintf("    %-24s%d\n", "Max texture size", p.WebGL.MaxTextureSize))
		}
		if len(p.WebGL.Extensions) > 0 {
			b.WriteString(fmt.Sprintf("    %-24s%d extensions\n", "Extensions", len(p.WebGL.Extensions)))
		}
		if p.WebGL.Caps != nil {
			var avail []string
			if p.WebGL.Caps.WebGPUAvailable {
				avail = append(avail, "WebGPU")
			}
			if p.WebGL.Caps.WebGL2Available {
				avail = append(avail, "WebGL2")
			}
			if p.WebGL.Caps.WebGL1Available {
				avail = append(avail, "WebGL1")
			}
			if len(avail) > 0 {
				b.WriteString(fmt.Sprintf("    %-24s%s\n", "Browser supports", strings.Join(avail, ", ")))
			}
		}
	}

	// Console entries — warnings, errors, and uncaught exceptions
	// captured during page load. These are the silent killers that
	// break features in prod without showing up in dev, so they get
	// prominent placement and truncated messages are marked with ….
	if len(p.ConsoleEntries) > 0 {
		b.WriteString("\n  Console\n")
		var nErr, nWarn, nExc int
		for _, c := range p.ConsoleEntries {
			switch c.Level {
			case "error", "assert":
				nErr++
			case "warning":
				nWarn++
			case "exception":
				nExc++
			}
		}
		b.WriteString(fmt.Sprintf("    %-24sexceptions:%d  errors:%d  warnings:%d\n",
			"Counts", nExc, nErr, nWarn))
		show := len(p.ConsoleEntries)
		if show > 6 {
			show = 6
		}
		for i := 0; i < show; i++ {
			c := p.ConsoleEntries[i]
			msg := strings.ReplaceAll(c.Text, "\n", " ⏎ ")
			if len(msg) > 80 {
				msg = msg[:79] + "…"
			}
			label := c.Level
			if label == "warning" {
				label = "warn"
			}
			b.WriteString(fmt.Sprintf("      %-8s%s\n", label, msg))
		}
		if len(p.ConsoleEntries) > 6 {
			b.WriteString(fmt.Sprintf("      … %d more\n", len(p.ConsoleEntries)-6))
		}
	}

	// Coverage summary — only printed when captured. Shows scripts
	// sorted by unused bytes (biggest split opportunities first).
	if len(p.Coverage) > 0 {
		b.WriteString(FormatCoverageSummary(p.Coverage, 1024, 8))
	}

	// Network summary (detail available via --waterfall)
	if len(p.Resources) > 0 {
		b.WriteString("\n  Network\n")
		b.WriteString(fmt.Sprintf("    %-24s%d\n", "Total requests", len(p.Resources)))
		b.WriteString(fmt.Sprintf("    %-24s%.1fKB\n", "Total transferred", float64(p.TotalBytesTransferred)/1024))
		b.WriteString(fmt.Sprintf("    %-24s%.0fms\n", "Blocking resource", p.BlockingResourceMs))
	}

	return b.String()
}

// FormatWaterfallTable formats the resource waterfall as a detailed table.
func FormatWaterfallTable(r *Report) string {
	p := &r.PageReport
	if len(p.Resources) == 0 {
		return "  No resources captured.\n"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n  Resource Waterfall (%d entries)\n", len(p.Resources)))
	b.WriteString("  " + FormatWaterfall(p.Resources))
	return b.String()
}

// FormatJSON returns the report as indented JSON.
func FormatJSON(r *Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
