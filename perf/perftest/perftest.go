// Package perftest provides testing.T-integrated browser profiling helpers
// for use in .dmj test blocks and Go test functions.
//
// Usage in a .dmj file:
//
//	test "counter click perf" {
//	    report := perftest.Run(t, "http://localhost:3000",
//	        perftest.Click("[data-gosx-handler='increment']"),
//	    )
//	    expect report.TTFBMs < 500
//	    expect report.Interactions[0].DispatchMs < 10
//	}
//
// perftest.Run handles Chrome discovery, driver lifecycle, instrumentation
// injection, and cleanup. If Chrome is not found, the test is skipped
// (not failed) so CI without Chrome still passes.
package perftest

import (
	"testing"
	"time"

	"github.com/odvcencio/gosx/perf"
)

// RunOption configures a browser profiling scenario.
type RunOption func(*perf.Scenario)

// Click adds a click interaction on the given CSS selector.
func Click(selector string) RunOption {
	return func(s *perf.Scenario) {
		s.Interactions = append(s.Interactions, perf.Interaction{
			Kind: "click", Selector: selector,
		})
	}
}

// Type adds a type interaction on the given CSS selector with the given text.
func Type(selector, text string) RunOption {
	return func(s *perf.Scenario) {
		s.Interactions = append(s.Interactions, perf.Interaction{
			Kind: "type", Selector: selector, Value: text,
		})
	}
}

// Scroll adds a scroll interaction by the given number of pixels.
func Scroll(pixels int) RunOption {
	return func(s *perf.Scenario) {
		s.Interactions = append(s.Interactions, perf.Interaction{
			Kind: "scroll", Value: formatInt(pixels),
		})
	}
}

// Frames sets the number of Scene3D frames to sample (default 120).
func Frames(n int) RunOption {
	return func(s *perf.Scenario) {
		s.Frames = n
	}
}

// Timeout sets the max wait duration (default 30s).
func Timeout(d time.Duration) RunOption {
	return func(s *perf.Scenario) {
		s.Timeout = d
	}
}

// Record enables video recording to the given file path.
func Record(path string) RunOption {
	return func(s *perf.Scenario) {
		s.RecordPath = path
	}
}

// Headless controls headless mode (default true in tests).
func Headless(v bool) RunOption {
	return func(s *perf.Scenario) {
		s.Headless = v
	}
}

// Run executes a browser profiling scenario against the given URL and
// returns the page report. The test is skipped if Chrome is not found.
// The test fails if the scenario returns an error.
//
// Multiple URLs can be passed; each is profiled in sequence. The returned
// report contains per-page data in report.Pages plus the single-page
// shorthand in the embedded PageReport (for the common single-URL case).
func Run(t *testing.T, url string, opts ...RunOption) *perf.Report {
	t.Helper()

	// Check Chrome availability before doing any work.
	if _, err := perf.FindChrome(); err != nil {
		t.Skipf("perftest: %v", err)
	}

	s := &perf.Scenario{
		URLs:     []string{url},
		Frames:   120,
		Timeout:  30 * time.Second,
		Headless: true,
	}
	for _, opt := range opts {
		opt(s)
	}

	report, err := perf.RunScenario(s)
	if err != nil {
		t.Fatalf("perftest.Run: %v", err)
	}
	return report
}

// RunMulti profiles multiple URLs in sequence.
func RunMulti(t *testing.T, urls []string, opts ...RunOption) *perf.Report {
	t.Helper()

	if _, err := perf.FindChrome(); err != nil {
		t.Skipf("perftest: %v", err)
	}

	s := &perf.Scenario{
		URLs:     urls,
		Frames:   120,
		Timeout:  30 * time.Second,
		Headless: true,
	}
	for _, opt := range opts {
		opt(s)
	}

	report, err := perf.RunScenario(s)
	if err != nil {
		t.Fatalf("perftest.RunMulti: %v", err)
	}
	return report
}

func formatInt(v int) string {
	// Avoid importing strconv for one call.
	if v == 0 {
		return "0"
	}
	buf := make([]byte, 0, 8)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		buf = append(buf, byte('0'+v%10))
		v /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
