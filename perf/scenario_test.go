package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRunScenarioRoutesGivesEachURLFreshTimeout(t *testing.T) {
	var diagnostics bytes.Buffer
	report := &Report{}
	scenario := &Scenario{
		URLs:        []string{"http://127.0.0.1/first", "http://127.0.0.1/second"},
		Timeout:     200 * time.Millisecond,
		diagnostics: &diagnostics,
	}

	err := runScenarioRoutes(scenario, report, func(ctx context.Context, url string, index int, total int) (*PageReport, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatalf("route %d did not receive a deadline", index)
		}
		remaining := time.Until(deadline)
		if index == 0 {
			time.Sleep(125 * time.Millisecond)
		}
		if index == 1 && remaining < 150*time.Millisecond {
			t.Fatalf("second route inherited an exhausted deadline: remaining=%s", remaining)
		}
		return &PageReport{URL: url, FullyLoadedMs: float64(index + 1)}, nil
	}, &diagnostics)
	if err != nil {
		t.Fatalf("runScenarioRoutes: %v", err)
	}
	if len(report.Pages) != 2 {
		t.Fatalf("expected 2 page reports, got %d", len(report.Pages))
	}
	log := diagnostics.String()
	if !strings.Contains(log, "route 1/2 http://127.0.0.1/first start timeout=200ms") ||
		!strings.Contains(log, "route 2/2 http://127.0.0.1/second start timeout=200ms") {
		t.Fatalf("expected per-route diagnostics, got:\n%s", log)
	}
}

func TestRunScenarioRoutesTimeoutIdentifiesURLAndKeepsPartialReport(t *testing.T) {
	var diagnostics bytes.Buffer
	report := &Report{}
	scenario := &Scenario{
		URLs:    []string{"http://127.0.0.1/fast", "http://127.0.0.1/slow"},
		Timeout: 25 * time.Millisecond,
	}

	err := runScenarioRoutes(scenario, report, func(ctx context.Context, url string, index int, total int) (*PageReport, error) {
		if index == 0 {
			return &PageReport{URL: url, FullyLoadedMs: 10}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}, &diagnostics)
	if err == nil {
		t.Fatal("expected route timeout failure")
	}
	text := err.Error()
	if !strings.Contains(text, "route 2/2 http://127.0.0.1/slow") ||
		!strings.Contains(text, "context deadline exceeded") {
		t.Fatalf("timeout error did not identify URL and cause: %v", err)
	}
	if len(report.Pages) != 1 || report.Pages[0].URL != "http://127.0.0.1/fast" {
		t.Fatalf("expected partial report for completed first route, got %#v", report.Pages)
	}
	finalizeScenarioReport(report)
	data, jsonErr := FormatJSON(report)
	if jsonErr != nil {
		t.Fatalf("FormatJSON partial report: %v", jsonErr)
	}
	var decoded Report
	if jsonErr := json.Unmarshal(data, &decoded); jsonErr != nil {
		t.Fatalf("partial report was not valid JSON: %v\n%s", jsonErr, string(data))
	}
	log := diagnostics.String()
	if !strings.Contains(log, "route 2/2 http://127.0.0.1/slow failed") {
		t.Fatalf("expected failure diagnostics for timed-out URL, got:\n%s", log)
	}
}

func TestCaptureCoverageForRouteStaysFirstRouteOnly(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		index   int
		want    bool
	}{
		{name: "disabled first route", enabled: false, index: 0, want: false},
		{name: "enabled first route", enabled: true, index: 0, want: true},
		{name: "enabled second route", enabled: true, index: 1, want: false},
		{name: "enabled later route", enabled: true, index: 4, want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := captureCoverageForRoute(tt.enabled, tt.index); got != tt.want {
				t.Fatalf("captureCoverageForRoute(%v, %d) = %v, want %v", tt.enabled, tt.index, got, tt.want)
			}
		})
	}
}

func TestPerfBudgetFailureRemainsAuthoritativeAfterRouteIsolation(t *testing.T) {
	report := &Report{Pages: []PageReport{{
		URL:                   "http://127.0.0.1/docs/getting-started",
		FullyLoadedMs:         2000,
		TotalBytesTransferred: 1,
	}}}
	budget := &BudgetFile{
		DefaultProfile: "strict",
		Profiles: map[string]BudgetProfile{
			"strict": {Assertions: []string{"fully_loaded <= 1"}},
		},
	}

	result, err := EvaluateBudget(report, budget, "")
	if err != nil {
		t.Fatalf("EvaluateBudget: %v", err)
	}
	if result.Passed {
		t.Fatal("expected failing budget to remain authoritative")
	}
	if len(result.Pages) != 1 || len(result.Pages[0].Assertions) != 1 || result.Pages[0].Assertions[0].Passed {
		t.Fatalf("unexpected budget result: %#v", result)
	}
}
