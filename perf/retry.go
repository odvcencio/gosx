package perf

import (
	"fmt"
	"io"
	"os"
)

// RetryRecoveredPages measures again, once, every page in report whose
// Scene3D renderer recovered mid-load (see RendererRecovery), and replaces the
// page with the new measurement. Each retry runs a new scenario, so it gets a
// fresh browser with a cold cache, like the first attempt.
//
// A renderer recovery on a CI software rasterizer (WebGPU device loss, then a
// swap to WebGL) re-fetches the fallback chunk and replaces the canvas, which
// makes LCP and byte counts describe the recovery instead of the page. One
// clean retry gives the budget gate a real measurement; if the retry recovers
// again, EvaluateBudget reports the page as inconclusive.
func RetryRecoveredPages(s *Scenario, report *Report) {
	retryRecoveredPages(s, report, RunScenario)
}

func retryRecoveredPages(s *Scenario, report *Report, run func(*Scenario) (*Report, error)) {
	if s == nil || report == nil {
		return
	}
	diagnostics := s.diagnostics
	if diagnostics == nil {
		diagnostics = os.Stderr
	}
	retried := false
	for i := range report.Pages {
		reason := RendererRecovery(report.Pages[i])
		if reason == "" {
			continue
		}
		url := report.Pages[i].URL
		fmt.Fprintf(diagnostics, "gosx perf: route %s renderer-recovery=%s during measurement (webgl renderer %q); measuring once more in a fresh browser\n",
			url, reason, webglRendererOf(report.Pages[i]))
		retry := *s
		retry.URLs = []string{url}
		// Keep the retry equivalent to the first attempt: coverage runs only
		// on the first route and interactions only on the last one.
		retry.Coverage = s.Coverage && i == 0
		if i != len(report.Pages)-1 {
			retry.Interactions = nil
		}
		retry.RecordPath, retry.TracePath, retry.HeapSnapshotPath = "", "", ""
		again, err := run(&retry)
		if err != nil || again == nil || len(again.Pages) == 0 {
			fmt.Fprintf(diagnostics, "gosx perf: route %s retry failed (%v); keeping the first measurement\n", url, err)
			continue
		}
		report.Pages[i] = again.Pages[0]
		retried = true
		logRetryOutcome(diagnostics, url, again.Pages[0])
	}
	if retried {
		finalizeScenarioReport(report)
	}
}

func logRetryOutcome(w io.Writer, url string, page PageReport) {
	if reason := RendererRecovery(page); reason != "" {
		fmt.Fprintf(w, "gosx perf: route %s retry recovered again (renderer-recovery=%s); its budget will be reported as inconclusive\n", url, reason)
		return
	}
	fmt.Fprintf(w, "gosx perf: route %s retry measured without a renderer recovery\n", url)
}

func webglRendererOf(page PageReport) string {
	if page.WebGL == nil {
		return ""
	}
	return page.WebGL.Renderer
}
