package perf

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
)

// Comparison is the diff between two perf reports. Each Metric is a
// named pair of baseline + candidate values with a pre-computed delta.
// Direction tells the formatter which way is "good" (lower is better for
// timing, higher is better for coverage ratio).
type Comparison struct {
	Baseline  string           `json:"baselinePath"`
	Candidate string           `json:"candidatePath"`
	Metrics   []ComparedMetric `json:"metrics"`
}

// Direction encodes which way improvement points for a metric.
type Direction int

const (
	// LowerBetter means the candidate is better when its value is lower
	// than the baseline — e.g., TBT, long task count, DCL.
	LowerBetter Direction = iota
	// HigherBetter means the candidate is better when its value is higher
	// — e.g., JS coverage ratio.
	HigherBetter
)

// ComparedMetric is a single side-by-side comparison row.
type ComparedMetric struct {
	Name             string    `json:"name"`
	Unit             string    `json:"unit"`
	Baseline         float64   `json:"baseline"`
	Candidate        float64   `json:"candidate"`
	DeltaPct         float64   `json:"deltaPct"`
	Direction        Direction `json:"direction"`
	BaselineFound    bool      `json:"baselineFound"`
	CandidateFound   bool      `json:"candidateFound"`
	Incomplete       bool      `json:"incomplete,omitempty"`
	IncompleteReason string    `json:"incompleteReason,omitempty"`
}

func (m ComparedMetric) MarshalJSON() ([]byte, error) {
	type comparedMetricJSON struct {
		Name             string    `json:"name"`
		Unit             string    `json:"unit"`
		Baseline         float64   `json:"baseline"`
		Candidate        float64   `json:"candidate"`
		DeltaPct         *float64  `json:"deltaPct,omitempty"`
		Direction        Direction `json:"direction"`
		BaselineFound    bool      `json:"baselineFound"`
		CandidateFound   bool      `json:"candidateFound"`
		Incomplete       bool      `json:"incomplete,omitempty"`
		IncompleteReason string    `json:"incompleteReason,omitempty"`
	}
	row := comparedMetricJSON{
		Name:             m.Name,
		Unit:             m.Unit,
		Baseline:         m.Baseline,
		Candidate:        m.Candidate,
		Direction:        m.Direction,
		BaselineFound:    m.BaselineFound,
		CandidateFound:   m.CandidateFound,
		Incomplete:       m.Incomplete,
		IncompleteReason: m.IncompleteReason,
	}
	if !m.Incomplete {
		row.DeltaPct = &m.DeltaPct
	}
	return json.Marshal(row)
}

// IsRegression returns true if the delta moves the wrong way beyond the
// threshold (expressed as a percentage, e.g. 5 for 5%).
func (m ComparedMetric) IsRegression(threshold float64) bool {
	if m.Incomplete {
		return false
	}
	if m.Baseline == 0 && m.Candidate == 0 {
		return false
	}
	// Directional sign: positive DeltaPct means candidate is LARGER than
	// baseline. Regression is "larger and LowerBetter" OR "smaller and
	// HigherBetter". Anything under the threshold is noise.
	if math.Abs(m.DeltaPct) < threshold {
		return false
	}
	if m.Direction == LowerBetter {
		return m.DeltaPct > 0
	}
	return m.DeltaPct < 0
}

// LoadReport reads a perf report JSON file (produced by `gosx perf --json`).
func LoadReport(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// Backfill: single-page reports sometimes lack Pages but have the
	// embedded PageReport populated. Normalize so downstream code can
	// always read from Pages[0].
	if len(r.Pages) == 0 && (r.PageReport.URL != "" || r.URL != "") {
		if r.PageReport.URL == "" {
			r.PageReport.URL = r.URL
		}
		r.Pages = []PageReport{r.PageReport}
	}
	return &r, nil
}

// CompareReports diffs two loaded reports and returns the full comparison.
// Only metrics that appear in both reports' first page are included.
// Multi-page reports are compared page[0] vs page[0] — compare per URL by
// running gosx perf compare separately for each URL.
func CompareReports(baseline, candidate *Report) Comparison {
	cmp := Comparison{
		Baseline:  reportURL(baseline),
		Candidate: reportURL(candidate),
	}
	if baseline == nil || candidate == nil {
		cmp.Metrics = append(cmp.Metrics, incompleteComparisonRow("Report pages", "baseline or candidate report is nil"))
		return cmp
	}
	if len(baseline.Pages) == 0 || len(candidate.Pages) == 0 {
		cmp.Metrics = append(cmp.Metrics, incompleteComparisonRow("Report pages", "baseline or candidate report has no comparable pages"))
		return cmp
	}
	b := baseline.Pages[0]
	c := candidate.Pages[0]

	add := func(name, unit string, bv, cv float64, dir Direction) {
		cmp.Metrics = append(cmp.Metrics, ComparedMetric{
			Name:           name,
			Unit:           unit,
			Baseline:       bv,
			Candidate:      cv,
			DeltaPct:       percentDelta(bv, cv),
			Direction:      dir,
			BaselineFound:  true,
			CandidateFound: true,
		})
	}
	statusRecorded := func(p PageReport, metric string) bool {
		group := measurementGroupForMetric(metric)
		if group == "" || p.Measurements == nil || p.Measurements.Version != measurementStatusVersion {
			return false
		}
		_, ok := p.Measurements.Groups[group]
		return ok
	}
	shouldAddMetric := func(metric string, rawPresent bool) bool {
		return rawPresent || statusRecorded(b, metric) || statusRecorded(c, metric)
	}
	addMetric := func(label, metric, unit string, dir Direction) {
		bv, bok := ResolvePageMetric(metric, &b)
		cv, cok := ResolvePageMetric(metric, &c)
		row := ComparedMetric{
			Name:           label,
			Unit:           unit,
			Baseline:       bv,
			Candidate:      cv,
			Direction:      dir,
			BaselineFound:  bok,
			CandidateFound: cok,
		}
		if bok && cok {
			row.DeltaPct = percentDelta(bv, cv)
		} else {
			row.Incomplete = true
			switch {
			case !bok && !cok:
				row.IncompleteReason = "baseline and candidate unavailable: " + metricUnavailableReason(metric, &b)
			case !bok:
				row.IncompleteReason = "baseline unavailable: " + metricUnavailableReason(metric, &b)
			case !cok:
				row.IncompleteReason = "candidate unavailable: " + metricUnavailableReason(metric, &c)
			}
		}
		cmp.Metrics = append(cmp.Metrics, row)
	}

	// Lifecycle — lower is better across the board.
	add("TTFB", "ms", b.TTFBMs, c.TTFBMs, LowerBetter)
	add("DCL", "ms", b.DCLMs, c.DCLMs, LowerBetter)
	if b.JSHeapSizeMB > 0 || c.JSHeapSizeMB > 0 {
		add("JS heap", "MB", b.JSHeapSizeMB, c.JSHeapSizeMB, LowerBetter)
	}

	// Web vitals.
	if shouldAddMetric("lcp", b.LargestContentfulPaintMs > 0 || c.LargestContentfulPaintMs > 0) {
		addMetric("LCP", "lcp", "ms", LowerBetter)
	}
	if shouldAddMetric("cls", b.CumulativeLayoutShift > 0 || c.CumulativeLayoutShift > 0) {
		addMetric("CLS", "cls", "", LowerBetter)
	}
	if shouldAddMetric("fid", b.FirstInputDelayMs > 0 || c.FirstInputDelayMs > 0) {
		addMetric("FID", "fid", "ms", LowerBetter)
	}

	// Main-thread blocking.
	addMetric("Long tasks", "long_tasks", "", LowerBetter)
	addMetric("Long task total", "long_task_total", "ms", LowerBetter)
	addMetric("TBT", "tbt", "ms", LowerBetter)

	// Scene frame stats if both reports have them.
	if b.Scene != nil && c.Scene != nil {
		add("Scene first frame", "ms", b.Scene.FirstFrameMs, c.Scene.FirstFrameMs, LowerBetter)
		add("Scene p50", "ms", b.Scene.FrameStats.P50, c.Scene.FrameStats.P50, LowerBetter)
		add("Scene p95", "ms", b.Scene.FrameStats.P95, c.Scene.FrameStats.P95, LowerBetter)
		add("Scene p99", "ms", b.Scene.FrameStats.P99, c.Scene.FrameStats.P99, LowerBetter)
	}

	// Runtime throughput.
	if shouldAddMetric("hub_bytes", b.HubMessageBytes > 0 || c.HubMessageBytes > 0) {
		addMetric("Hub bytes", "hub_bytes", "B", LowerBetter)
	}

	// Network.
	addMetric("Network bytes", "network_kb", "KB", LowerBetter)
	addMetric("Requests", "requests", "", LowerBetter)

	// JS coverage (overall) — higher is better.
	if len(b.Coverage) > 0 && len(c.Coverage) > 0 {
		add("JS used", "KB", coverageUsedKB(b.Coverage), coverageUsedKB(c.Coverage), LowerBetter)
		add("JS total", "KB", coverageTotalKB(b.Coverage), coverageTotalKB(c.Coverage), LowerBetter)
		add("JS used ratio", "%", coverageRatio(b.Coverage)*100, coverageRatio(c.Coverage)*100, HigherBetter)
	}

	return cmp
}

func reportURL(r *Report) string {
	if r == nil {
		return ""
	}
	return r.URL
}

func incompleteComparisonRow(name, reason string) ComparedMetric {
	return ComparedMetric{Name: name, Incomplete: true, IncompleteReason: reason}
}

func percentDelta(baseline, candidate float64) float64 {
	if baseline == 0 {
		if candidate == 0 {
			return 0
		}
		return 100 // new metric — treat as +100%
	}
	return (candidate - baseline) / baseline * 100
}

func coverageUsedKB(entries []CoverageEntry) float64 {
	total := 0
	for _, e := range entries {
		total += e.UsedBytes
	}
	return float64(total) / 1024
}

func coverageTotalKB(entries []CoverageEntry) float64 {
	total := 0
	for _, e := range entries {
		total += e.TotalBytes
	}
	return float64(total) / 1024
}

func coverageRatio(entries []CoverageEntry) float64 {
	used := coverageUsedKB(entries)
	total := coverageTotalKB(entries)
	if total == 0 {
		return 0
	}
	return used / total
}

// FormatComparison renders a Comparison as a side-by-side table. Metrics
// that moved more than threshold% in the wrong direction are prefixed
// with "⚠" in the arrow column; improvements get "↓" or "↑".
func FormatComparison(cmp Comparison, threshold float64) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("gosx perf compare — baseline: %s  candidate: %s\n",
		cmp.Baseline, cmp.Candidate))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("    %-22s%14s%14s%12s  %s\n",
		"Metric", "Baseline", "Candidate", "Δ%", "Status"))
	b.WriteString("    " + strings.Repeat("-", 70) + "\n")
	for _, m := range cmp.Metrics {
		status := formatDeltaStatus(m, threshold)
		bv := formatMetricValue(m.Baseline, m.Unit)
		cv := formatMetricValue(m.Candidate, m.Unit)
		if !m.BaselineFound {
			bv = "unavailable"
		}
		if !m.CandidateFound {
			cv = "unavailable"
		}
		if m.Incomplete {
			b.WriteString(fmt.Sprintf("    %-22s%14s%14s%12s  %s: %s\n",
				m.Name, bv, cv, "n/a", status, m.IncompleteReason))
			continue
		}
		b.WriteString(fmt.Sprintf("    %-22s%14s%14s%11.1f%%  %s\n",
			m.Name, bv, cv, m.DeltaPct, status))
	}
	return b.String()
}

func formatMetricValue(v float64, unit string) string {
	if unit == "" {
		if v == math.Trunc(v) && v < 1e6 {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%.2f", v)
	}
	return fmt.Sprintf("%.1f%s", v, unit)
}

func formatDeltaStatus(m ComparedMetric, threshold float64) string {
	if m.Incomplete {
		return "inconclusive"
	}
	if math.Abs(m.DeltaPct) < threshold {
		return "~"
	}
	if m.IsRegression(threshold) {
		return "⚠ regression"
	}
	// Improvement
	if m.Direction == LowerBetter {
		return "↓ improved"
	}
	return "↑ improved"
}

// AnyRegression reports whether the comparison contains any metric that
// regressed beyond threshold. Useful for CI gating.
func AnyRegression(cmp Comparison, threshold float64) bool {
	for _, m := range cmp.Metrics {
		if m.IsRegression(threshold) {
			return true
		}
	}
	return false
}

func AnyIncomplete(cmp Comparison) bool {
	for _, m := range cmp.Metrics {
		if m.Incomplete {
			return true
		}
	}
	return false
}
