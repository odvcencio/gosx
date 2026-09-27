package docs

import "testing"

func TestDebuggingGuideRendersLiveCPUReport(t *testing.T) {
	report := debuggingSceneReport()
	if report["status"] != "Passed" {
		t.Fatalf("CPU report status = %v", report["status"])
	}
	for _, key := range []string{"backend", "objects", "lights", "coverage", "visibleBounds", "uniqueColors"} {
		if report[key] == nil || report[key] == "" {
			t.Errorf("CPU report is missing %q: %#v", key, report)
		}
	}
}
