package perf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type pageReportQueryInvocation struct {
	phase    string
	required bool
}

func successfulPageReportQueries() pageReportQueries {
	return pageReportQueries{
		navigationTiming: func(*Driver) (NavigationTiming, error) {
			return NavigationTiming{TTFB: 1, DOMContentLoaded: 2, FullyLoaded: 3}, nil
		},
		heapSize: func(*Driver) (float64, error) {
			return 4, nil
		},
		hydrationLog: func(*Driver) ([]HydrationEntry, error) {
			return []HydrationEntry{{ID: "island", Ms: 5}}, nil
		},
		performanceMeasures: func(*Driver, string) ([]PerfEntry, error) {
			return []PerfEntry{{Name: "scene3d-render", Duration: 6}}, nil
		},
		sceneTelemetry: func(*Driver) (SceneTelemetrySnapshot, error) {
			return SceneTelemetrySnapshot{Available: true}, nil
		},
		runtimeState: func(*Driver) (RuntimeState, error) {
			return RuntimeState{FrameCount: 1}, nil
		},
		dispatchLog: func(*Driver) ([]DispatchEntry, error) {
			return []DispatchEntry{{Island: "island", Handler: "click", Ms: 7, Patches: 1}}, nil
		},
		evaluate: func(*Driver, string, interface{}) error {
			return nil
		},
		resourceWaterfall: func(*Driver) ([]ResourceEntry, error) {
			return []ResourceEntry{{Name: "app.js", InitiatorType: "script", TransferSize: 8, ResponseEnd: 9}}, nil
		},
	}
}

func TestCollectPageReportQueryOrderAndOptionalFailures(t *testing.T) {
	queries := successfulPageReportQueries()
	optionalErr := errors.New("optional query unavailable")
	queries.evaluate = func(*Driver, string, interface{}) error { return optionalErr }
	queries.resourceWaterfall = func(*Driver) ([]ResourceEntry, error) { return nil, optionalErr }

	var got []pageReportQueryInvocation
	runner := func(phase string, required bool, query func() error) error {
		got = append(got, pageReportQueryInvocation{phase: phase, required: required})
		return query()
	}

	report, err := collectPageReport(nil, "https://example.test/", queries, runner)
	if err != nil {
		t.Fatalf("collectPageReport returned an optional error: %v", err)
	}
	want := []pageReportQueryInvocation{
		{phase: "collect/navigation-timing", required: true},
		{phase: "collect/heap-size", required: true},
		{phase: "collect/hydration-log", required: true},
		{phase: "collect/scene-measures", required: true},
		{phase: "collect/scene-telemetry", required: true},
		{phase: "collect/runtime-state", required: true},
		{phase: "collect/dispatch-log", required: true},
		{phase: "collect/vitals", required: false},
		{phase: "collect/long-tasks", required: false},
		{phase: "collect/webgl-info", required: false},
		{phase: "collect/resource-waterfall", required: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query order/requiredness mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	if report.URL != "https://example.test/" || report.TTFBMs != 1 || report.JSHeapSizeMB != 4 {
		t.Fatalf("required results were not retained: %+v", report)
	}
	if report.WebGL != nil || len(report.Resources) != 0 {
		t.Fatalf("failed optional results must remain absent: webgl=%+v resources=%+v", report.WebGL, report.Resources)
	}
}

func TestCollectPageReportRequiredFailureAttribution(t *testing.T) {
	requiredPhases := []string{
		"collect/navigation-timing",
		"collect/heap-size",
		"collect/hydration-log",
		"collect/scene-measures",
		"collect/scene-telemetry",
		"collect/runtime-state",
		"collect/dispatch-log",
	}
	for _, failPhase := range requiredPhases {
		t.Run(failPhase, func(t *testing.T) {
			sentinel := errors.New("sentinel")
			var phases []string
			runner := func(phase string, required bool, query func() error) error {
				phases = append(phases, phase)
				if phase == failPhase {
					if !required {
						t.Fatalf("%s unexpectedly marked optional", phase)
					}
					return sentinel
				}
				return query()
			}

			report, err := collectPageReport(nil, "https://example.test/", successfulPageReportQueries(), runner)
			if report != nil {
				t.Fatalf("required failure returned a report: %+v", report)
			}
			if !errors.Is(err, sentinel) {
				t.Fatalf("error lost sentinel: %v", err)
			}
			wantError := failPhase + ": sentinel"
			if err.Error() != wantError {
				t.Fatalf("error = %q, want %q", err, wantError)
			}
			if got := phases[len(phases)-1]; got != failPhase {
				t.Fatalf("last query = %q, want %q", got, failPhase)
			}
		})
	}
}

func TestCollectPageReportCancellationRemainsAttributedAndFatal(t *testing.T) {
	runner := func(phase string, _ bool, query func() error) error {
		if phase == "collect/scene-measures" {
			return fmt.Errorf("Runtime.evaluate: %w", context.Canceled)
		}
		return query()
	}

	report, err := collectPageReport(nil, "https://example.test/", successfulPageReportQueries(), runner)
	if report != nil {
		t.Fatalf("cancellation returned a report: %+v", report)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation cause was lost: %v", err)
	}
	want := "collect/scene-measures: Runtime.evaluate: context canceled"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestDiagnosticPageReportQueryRunnerLabelsOptionalFailure(t *testing.T) {
	var diagnostics bytes.Buffer
	sentinel := errors.New("optional unavailable")
	runner := diagnosticPageReportQueryRunner(&diagnostics, 1, 3, "https://example.test/", 45*time.Second)

	err := runner("collect/vitals", false, func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("runner lost optional error: %v", err)
	}
	log := diagnostics.String()
	if !strings.Contains(log, "route 2/3 https://example.test/ phase=collect/vitals start required=false timeout=45s") {
		t.Fatalf("missing stable optional start attribution:\n%s", log)
	}
	if !strings.Contains(log, "phase=collect/vitals failed") ||
		!strings.Contains(log, "required=false action=continue: optional unavailable") {
		t.Fatalf("missing stable optional failure attribution:\n%s", log)
	}
}
