//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
)

type recordedChromeDecision struct{ fatal, skip string }

func (r *recordedChromeDecision) Helper() {}
func (r *recordedChromeDecision) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
}
func (r *recordedChromeDecision) Skipf(format string, args ...any) {
	r.skip = fmt.Sprintf(format, args...)
}

func TestChromeRequiredReadsEnvironment(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "false": false, " FALSE ": false, "1": true, "true": true, "yes": true} {
		t.Setenv(requireChromeEnv, value)
		if got := chromeRequired(); got != want {
			t.Errorf("%s=%q: chromeRequired() = %v, want %v", requireChromeEnv, value, got, want)
		}
	}
}

func TestSkipOrFailChromeDecision(t *testing.T) {
	var got recordedChromeDecision
	skipOrFailChrome(&got, true, "chrome failed to start: %v", "boom")
	if got.skip != "" || !strings.Contains(got.fatal, "boom") || !strings.Contains(got.fatal, requireChromeEnv) {
		t.Fatalf("required and missing must fail, got %+v", got)
	}
	got = recordedChromeDecision{}
	skipOrFailChrome(&got, false, "chrome failed to start: %v", "boom")
	if got.fatal != "" || !strings.Contains(got.skip, "boom") {
		t.Fatalf("not required and missing must skip, got %+v", got)
	}
}
