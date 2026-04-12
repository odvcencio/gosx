package perf

import (
	"fmt"
	"sort"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/debugger"
	"github.com/chromedp/cdproto/profiler"
	"github.com/chromedp/chromedp"
)

// CoverageEntry is a per-script "how much of this JS was actually executed
// during the profiling window" summary. UsedBytes / TotalBytes gives the
// coverage ratio, and UnusedBytes is what could theoretically be removed
// by a smarter split or dead-code elimination.
type CoverageEntry struct {
	URL         string  `json:"url"`
	TotalBytes  int     `json:"totalBytes"`
	UsedBytes   int     `json:"usedBytes"`
	UnusedBytes int     `json:"unusedBytes"`
	UsedRatio   float64 `json:"usedRatio"`
}

// CaptureCoverage starts precise block-level coverage, runs during(), and
// returns per-script usage summaries. Scripts with an empty URL (inline
// <script> blocks, anonymous eval) are dropped — those aren't actionable.
//
// Block-level coverage is the same granularity Chrome DevTools' Coverage
// panel uses. It's accurate per byte range, not per function.
func CaptureCoverage(d *Driver, during func() error) ([]CoverageEntry, error) {
	if d == nil {
		return nil, nil
	}

	// Build an executor-scoped context — CDP commands with multi-return
	// Do signatures can't be passed through chromedp.Run, so we thread
	// the driver context through cdp.WithExecutor bound to the current
	// target (the active page).
	execCtx := cdp.WithExecutor(d.ctx, chromedp.FromContext(d.ctx).Target)

	// Enable the Debugger domain so we can resolve scriptID → source
	// length. Profiler coverage only gives us byte ranges, not absolute
	// sizes, and many scripts are streamed from the network without a
	// Content-Length we could trust, so the authoritative size is the
	// source string length Chrome records.
	if _, err := debugger.Enable().Do(execCtx); err != nil {
		return nil, fmt.Errorf("debugger.Enable: %w", err)
	}

	// Profiler has to be explicitly enabled before precise coverage can
	// start — otherwise Chrome returns "Profiler is not enabled".
	if err := profiler.Enable().Do(execCtx); err != nil {
		return nil, fmt.Errorf("profiler.Enable: %w", err)
	}

	startParams := profiler.StartPreciseCoverage().
		WithCallCount(false). // we don't need counts, just used/unused
		WithDetailed(true)    // block-level granularity
	if _, err := startParams.Do(execCtx); err != nil {
		return nil, fmt.Errorf("profiler.StartPreciseCoverage: %w", err)
	}

	// Run the measured work.
	cbErr := during()

	// Pull coverage data via the same executor-scoped context.
	scripts, _, err := profiler.TakePreciseCoverage().Do(execCtx)
	if err != nil {
		return nil, fmt.Errorf("profiler.TakePreciseCoverage: %w", err)
	}
	_ = profiler.StopPreciseCoverage().Do(execCtx)

	// Resolve per-script total size via Debugger.getScriptSource. Cache
	// by script id so duplicate references don't re-fetch.
	type agg struct {
		total int
		used  int
		url   string
	}
	sums := make(map[string]*agg)
	for _, sc := range scripts {
		if sc == nil || sc.URL == "" {
			continue
		}
		src, _, err := debugger.GetScriptSource(sc.ScriptID).Do(execCtx)
		if err != nil {
			// Script might have been GC'd or from a worker — skip.
			continue
		}
		total := len(src)
		// Correct coverage algorithm for Chrome's block format:
		//
		// Per function, range[0] is the whole function body with its
		// call count; inner ranges are conditional blocks within. With
		// CallCount disabled, count is 0 for unexecuted blocks and 1
		// for executed ones.
		//
		// Compute USED as (total script length) − (bytes covered by a
		// count=0 range). This works because Chrome's block ranges are
		// non-overlapping leaves when count is 0 (if a block never ran,
		// no child range was emitted inside it). It also correctly
		// handles whole-function count=0 ranges (never-called funcs).
		unused := 0
		for _, fn := range sc.Functions {
			for _, r := range fn.Ranges {
				if r.Count == 0 {
					unused += int(r.EndOffset - r.StartOffset)
				}
			}
		}
		used := total - unused
		if used < 0 {
			used = 0
		}
		a, ok := sums[sc.URL]
		if !ok {
			a = &agg{url: sc.URL}
			sums[sc.URL] = a
		}
		// In case the same URL shows up twice (module vs main), take the
		// larger total and the larger used.
		if total > a.total {
			a.total = total
		}
		if used > a.used {
			a.used = used
		}
	}

	out := make([]CoverageEntry, 0, len(sums))
	for _, a := range sums {
		if a.total == 0 {
			continue
		}
		ratio := float64(a.used) / float64(a.total)
		if ratio > 1 {
			ratio = 1
		}
		out = append(out, CoverageEntry{
			URL:         a.url,
			TotalBytes:  a.total,
			UsedBytes:   a.used,
			UnusedBytes: a.total - a.used,
			UsedRatio:   ratio,
		})
	}
	// Sort by unused bytes descending — largest fix opportunities first.
	sort.Slice(out, func(i, j int) bool {
		return out[i].UnusedBytes > out[j].UnusedBytes
	})
	return out, cbErr
}

// FormatCoverageSummary renders a coverage slice as a table fragment.
// Only entries above minBytes are printed — noise from tiny inline scripts
// isn't useful.
func FormatCoverageSummary(entries []CoverageEntry, minBytes int, topN int) string {
	var b strings.Builder
	b.WriteString("\n  JS Coverage (used / total)\n")
	if len(entries) == 0 {
		b.WriteString("    (no data)\n")
		return b.String()
	}
	shown := 0
	var totalUsed, totalAll int
	for _, e := range entries {
		totalUsed += e.UsedBytes
		totalAll += e.TotalBytes
		if e.TotalBytes < minBytes {
			continue
		}
		if topN > 0 && shown >= topN {
			continue
		}
		shown++
		pct := e.UsedRatio * 100
		label := shortenURL(e.URL, 48)
		b.WriteString(fmt.Sprintf("      %-48s%6.1f%%   %5.1f KB / %5.1f KB\n",
			label, pct,
			float64(e.UsedBytes)/1024,
			float64(e.TotalBytes)/1024))
	}
	if totalAll > 0 {
		overall := float64(totalUsed) / float64(totalAll) * 100
		b.WriteString(fmt.Sprintf("    %-24s%.1f%%  (%d scripts, %.1f KB used / %.1f KB total)\n",
			"Overall", overall, len(entries),
			float64(totalUsed)/1024, float64(totalAll)/1024))
	}
	return b.String()
}

func shortenURL(url string, max int) string {
	// Strip origin for readability — "https://m31labs.dev/…" is noise.
	if idx := strings.Index(url, "://"); idx >= 0 {
		if slash := strings.Index(url[idx+3:], "/"); slash >= 0 {
			url = url[idx+3+slash:]
		}
	}
	if len(url) > max {
		return "…" + url[len(url)-max+1:]
	}
	return url
}
