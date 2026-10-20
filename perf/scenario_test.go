package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestPollSceneFrameSamplesInternalWindowReturnsPartialResult(t *testing.T) {
	parentCtx := context.Background()
	sampleCtx, cancel := context.WithTimeout(parentCtx, 30*time.Millisecond)
	defer cancel()

	observations := []int{1, 3}
	probeCount := 0
	result, err := pollSceneFrameSamples(parentCtx, sampleCtx, true, 10, time.Millisecond, func() (int, error) {
		index := probeCount
		probeCount++
		if index >= len(observations) {
			index = len(observations) - 1
		}
		return observations[index], nil
	})
	if err != nil {
		t.Fatalf("internal sample-window exhaustion should be best-effort: %v", err)
	}
	if !result.Exhausted || result.Target != 10 || result.Observed != 3 {
		t.Fatalf("unexpected partial sample result: %#v", result)
	}
}

func TestPollSceneFrameSamplesExternalCancellationRemainsFatal(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	sampleCtx, cancelSample := context.WithTimeout(parentCtx, time.Second)
	defer cancelSample()

	result, err := pollSceneFrameSamples(parentCtx, sampleCtx, true, 10, time.Millisecond, func() (int, error) {
		cancelParent()
		return 2, fmt.Errorf("evaluate: %w", context.Canceled)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("external cancellation should remain fatal, got result=%#v err=%v", result, err)
	}
	if result.Exhausted {
		t.Fatalf("external cancellation was misclassified as local exhaustion: %#v", result)
	}
}

func TestPollSceneFrameSamplesRouteDeadlineRemainsFatal(t *testing.T) {
	parentCtx, cancelParent := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelParent()
	sampleCtx, cancelSample := context.WithTimeout(parentCtx, time.Second)
	defer cancelSample()

	result, err := pollSceneFrameSamples(parentCtx, sampleCtx, false, 10, time.Millisecond, func() (int, error) {
		return 2, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("route deadline should remain fatal, got result=%#v err=%v", result, err)
	}
	if result.Exhausted {
		t.Fatalf("route deadline was misclassified as local exhaustion: %#v", result)
	}
}

func TestPollSceneFrameSamplesCDPErrorRemainsFatal(t *testing.T) {
	parentCtx := context.Background()
	sampleCtx, cancelSample := context.WithTimeout(parentCtx, time.Second)
	defer cancelSample()
	wantErr := errors.New("cdp evaluation failed")

	result, err := pollSceneFrameSamples(parentCtx, sampleCtx, true, 10, time.Millisecond, func() (int, error) {
		return 2, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("CDP error should remain fatal, got result=%#v err=%v", result, err)
	}
	if result.Exhausted {
		t.Fatalf("CDP error was misclassified as local exhaustion: %#v", result)
	}
}

func TestPollSceneFrameSamplesAlreadyExpiredInternalContextIsExhausted(t *testing.T) {
	parentCtx := context.Background()
	sampleCtx, cancelSample := context.WithDeadline(parentCtx, time.Now().Add(-time.Second))
	defer cancelSample()

	result, err := pollSceneFrameSamples(parentCtx, sampleCtx, true, 10, time.Millisecond, func() (int, error) {
		return 3, context.DeadlineExceeded
	})
	if err != nil {
		t.Fatalf("expired internal sample context should be best-effort: %v", err)
	}
	if !result.Exhausted || result.Observed != 3 {
		t.Fatalf("unexpected expired-context result: %#v", result)
	}
}

func TestPollSceneFrameSamplesConcurrentExpiryDoesNotSwallowCDPError(t *testing.T) {
	wantErr := errors.New("execution context destroyed")
	for _, tt := range []struct {
		name     string
		probeErr error
	}{
		{name: "sentinel", probeErr: wantErr},
		{name: "sentinel joined with context", probeErr: errors.Join(context.DeadlineExceeded, wantErr)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parentCtx := context.Background()
			sampleCtx, cancelSample := context.WithTimeout(parentCtx, 10*time.Millisecond)
			defer cancelSample()

			result, err := pollSceneFrameSamples(parentCtx, sampleCtx, true, 10, time.Millisecond, func() (int, error) {
				<-sampleCtx.Done()
				return 3, tt.probeErr
			})
			if !errors.Is(err, wantErr) {
				t.Fatalf("concurrent sample expiry swallowed CDP error: result=%#v err=%v", result, err)
			}
			if result.Exhausted {
				t.Fatalf("concurrent CDP error was misclassified as local exhaustion: %#v", result)
			}
		})
	}
}

func TestPollSceneFrameSamplesWrappedInternalContextErrorsAreExhausted(t *testing.T) {
	for _, tt := range []struct {
		name       string
		contextErr error
	}{
		{name: "deadline", contextErr: context.DeadlineExceeded},
		{name: "canceled", contextErr: context.Canceled},
		{name: "joined context errors", contextErr: errors.Join(context.DeadlineExceeded, context.Canceled)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parentCtx := context.Background()
			sampleCtx, cancelSample := context.WithDeadline(parentCtx, time.Now().Add(-time.Second))
			defer cancelSample()

			result, err := pollSceneFrameSamples(parentCtx, sampleCtx, true, 10, time.Millisecond, func() (int, error) {
				return 3, fmt.Errorf("evaluate: %w", tt.contextErr)
			})
			if err != nil {
				t.Fatalf("wrapped internal context error should be best-effort: %v", err)
			}
			if !result.Exhausted || result.Observed != 3 {
				t.Fatalf("unexpected wrapped-context result: %#v", result)
			}
		})
	}
}
